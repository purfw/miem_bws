package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/metrics"
	sharedmigrate "github.com/confhub/shared/migrate"
	"github.com/confhub/shared/natsx"
	"github.com/confhub/shared/openapi"
	"github.com/confhub/shared/operations"
	"github.com/confhub/shared/outbox"
	sharedpgx "github.com/confhub/shared/pgx"
	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/slogx"
	"github.com/confhub/shared/telemetry"
	"github.com/confhub/shared/transport"

	"github.com/confhub/identity-svc/internal/config"
	"github.com/confhub/identity-svc/internal/handler"
	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/identity-svc/internal/service"

	"github.com/redis/go-redis/v9"
)

const serviceName = "identity-svc"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	slogx.Init(serviceName, cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	flushTraces, err := telemetry.Init(ctx, serviceName)
	if err != nil {
		slog.Error("tracing configuration", "err", err)
		os.Exit(1)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = flushTraces(flushCtx)
	}()

	if err := sharedmigrate.Up(cfg.DatabaseURL, "/app/migrations"); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}

	pool, err := sharedpgx.Connect(ctx, cfg.DatabaseURL, 15)
	if err != nil {
		slog.Error("pg connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	redisOpt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("redis url", "err", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpt)
	defer rdb.Close()

	nc, err := natsx.Connect(cfg.NATSURL, serviceName)
	if err != nil {
		slog.Error("nats connect", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	jwtIssuer, err := jwtauth.NewIssuer(cfg.JWTPrivateKeyPath, cfg.AccessTokenTTL, cfg.JWTIssuer)
	if err != nil {
		slog.Error("jwt issuer", "err", err)
		os.Exit(1)
	}
	jwtVerifier, err := jwtauth.NewVerifier(cfg.JWTPublicKeyPath, cfg.JWTIssuer)
	if err != nil {
		slog.Error("jwt verifier", "err", err)
		os.Exit(1)
	}
	jwtVerifier.WithRevocationStore(rdb)

	users := repository.NewUsers(pool)
	jwtVerifier.WithStateResolver(users.AuthState)
	refresh := service.NewRefreshStore(rdb, cfg.RefreshTokenTTL)
	rl := service.NewLoginRateLimiter(rdb, cfg.LoginRateLimit, cfg.LoginRateWindow)
	passwordChecker := service.NewPwnedChecker(cfg.PwnedPasswordsURL, cfg.PwnedPasswordsTimeout)
	auth := service.NewAuth(users, jwtIssuer, refresh, rl, cfg.AccessTokenTTL, passwordChecker)
	orcid := service.NewORCIDOAuth(service.ORCIDConfig{
		ClientID: cfg.ORCIDClientID, ClientSecret: cfg.ORCIDClientSecret,
		AuthorizeURL: cfg.ORCIDAuthorizeURL, TokenURL: cfg.ORCIDTokenURL,
		UserInfoURL: cfg.ORCIDUserInfoURL, APIURL: cfg.ORCIDAPIURL,
		BaseURL: cfg.BaseURL, StateSecret: cfg.ServiceTokenSecret,
	}, rdb, auth)

	r := chi.NewRouter()
	stats := metrics.New("identity-svc", pool)
	r.Use(httputil.RequestID, telemetry.Middleware, stats.Middleware, httputil.Recover, httputil.AccessLog)
	r.Handle("/metrics", stats.Handler())
	r.Get("/openapi.json", openapi.Handler(r, serviceName, handler.APIModels()))

	authH := handler.NewAuth(auth, cfg.RefreshTokenTTL, cfg.CookieSecure, jwtVerifier.JWKSet(), orcid, cfg.BaseURL)
	meH := handler.NewMe(users, auth)
	internalH := handler.NewInternal(auth, users)
	healthH := handler.NewHealth(pool, rdb, nc)

	r.Route("/api/v1/auth", func(r chi.Router) {
		authH.Routes(r)
		r.Group(func(r chi.Router) {
			r.Use(jwtVerifier.Middleware(false))
			authH.AuthenticatedRoutes(r)
			meH.Routes(r)
		})
	})

	stVerifier := servicetoken.NewVerifier(cfg.ServiceTokenSecret, rdb)
	r.Route("/internal", func(r chi.Router) {
		r.Use(stVerifier.Middleware)
		operations.New(pool, "identity_svc", nil).Routes(r)
		internalH.Routes(r)
	})

	r.Get("/healthz", healthH.Live)
	r.Get("/readyz", healthH.Ready)
	if err := openapi.ValidateRoutes(r, serviceName, handler.APIModels()); err != nil {
		slog.Error("API contract", "err", err)
		os.Exit(1)
	}

	worker := outbox.New(pool, nc, outbox.Config{Schema: "identity_svc"})
	go func() {
		if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("outbox worker", "err", err)
		}
	}()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		slog.Info("listening", "port", cfg.Port)
		if err := transport.ListenAndServe(srv); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdown, sc := context.WithTimeout(context.Background(), 15*time.Second)
	defer sc()
	_ = transport.Shutdown(shutdown, srv)
}
