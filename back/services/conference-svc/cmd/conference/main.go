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
	"github.com/redis/go-redis/v9"

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

	"github.com/confhub/conference-svc/internal/config"
	"github.com/confhub/conference-svc/internal/handler"
	"github.com/confhub/conference-svc/internal/projection"
	"github.com/confhub/conference-svc/internal/repository"
	searchsvc "github.com/confhub/conference-svc/internal/search"
	"github.com/confhub/conference-svc/internal/service"
)

const serviceName = "conference-svc"

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
	cacheOptions, err := redis.ParseURL(cfg.CacheRedisURL)
	if err != nil {
		slog.Error("cache redis", "err", err)
		os.Exit(1)
	}
	cacheRedis := redis.NewClient(cacheOptions)
	defer cacheRedis.Close()

	nc, err := natsx.Connect(cfg.NATSURL, serviceName)
	if err != nil {
		slog.Error("nats connect", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	jwtVerifier, err := jwtauth.NewVerifier(cfg.JWTPublicKeyPath, cfg.JWTIssuer)
	if err != nil {
		slog.Error("jwt verifier", "err", err)
		os.Exit(1)
	}
	jwtVerifier.WithRevocationStore(rdb)
	jwtVerifier.WithStateResolver(jwtauth.RemoteStateResolver(cfg.IdentitySvcURL, cfg.ServiceTokenSecret))

	repo := repository.NewConferences(pool)
	searchClient := searchsvc.NewClient(cfg.ElasticsearchURL, cfg.ElasticsearchIndex, cacheRedis, cfg.CacheTTL)
	searchLimiter := searchsvc.NewRateLimiter(rdb, cfg.SearchRateLimit)
	registrationRepo := repository.NewRegistrations(pool)
	svc := service.New(repo)
	linkCipher, err := service.NewLinkCipher(cfg.JoinURLKeys, cfg.JoinURLKeyVersion)
	if err != nil {
		slog.Error("join URL encryption", "err", err)
		os.Exit(1)
	}

	failures := natsx.NewFailureStore(pool, "conference_svc")
	go failures.Run(ctx)
	if err := projection.NewCounters(repo).Subscribe(ctx, nc, failures); err != nil {
		slog.Error("counters projection", "err", err)
		os.Exit(1)
	}
	if err := projection.NewOrganizerAccess(repo).Subscribe(ctx, nc, failures); err != nil {
		slog.Error("organizer access projection", "err", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	stats := metrics.New("conference-svc", pool)
	r.Use(httputil.RequestID, telemetry.Middleware, stats.Middleware, httputil.Recover, httputil.AccessLog)
	r.Handle("/metrics", stats.Handler())
	r.Get("/openapi.json", openapi.Handler(r, serviceName, handler.APIModels()))

	pub := handler.NewPublic(repo, searchClient, searchLimiter)
	registrationH := handler.NewRegistrations(registrationRepo, rdb)
	organizerH := handler.NewOrganizer(svc, repo, registrationRepo, linkCipher)
	internal := handler.NewInternal(svc, repo, registrationRepo)
	healthH := handler.NewHealth(pool, rdb, nc)

	r.Route("/api/v1/conferences", func(r chi.Router) {
		r.Use(jwtVerifier.Middleware(true))
		pub.Routes(r)
		registrationH.ConferenceRoutes(r)
		organizerH.ConferenceRoutes(r)
	})
	r.Route("/api/v1/registrations", func(r chi.Router) {
		r.Use(jwtVerifier.Middleware(false))
		registrationH.RegistrationRoutes(r)
	})
	r.Route("/api/v1/sessions", func(r chi.Router) {
		r.Use(jwtVerifier.Middleware(false))
		organizerH.SessionRoutes(r)
	})
	r.Route("/api/v1/my", func(r chi.Router) {
		r.Use(jwtVerifier.Middleware(false))
		registrationH.MyRoutes(r)
		organizerH.MyRoutes(r)
	})
	r.Route("/api/v1/home", func(r chi.Router) {
		pub.HomeRoutes(r)
	})
	r.Route("/api/v1/search", func(r chi.Router) {
		r.Use(jwtVerifier.Middleware(true))
		pub.SearchRoutes(r)
	})
	r.Route("/api/v1/favorites", func(r chi.Router) {
		r.Use(jwtVerifier.Middleware(false))
		pub.FavoritesRoutes(r)
	})

	stVerifier := servicetoken.NewVerifier(cfg.ServiceTokenSecret, rdb)
	r.Route("/internal", func(r chi.Router) {
		r.Use(stVerifier.Middleware)
		operations.New(pool, "conference_svc", nil).Routes(r)
		internal.Routes(r)
	})

	r.Get("/healthz", healthH.Live)
	r.Get("/readyz", healthH.Ready)
	if err := openapi.ValidateRoutes(r, serviceName, handler.APIModels()); err != nil {
		slog.Error("API contract", "err", err)
		os.Exit(1)
	}

	worker := outbox.New(pool, nc, outbox.Config{Schema: "conference_svc"})
	go func() {
		if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("outbox worker", "err", err)
		}
	}()
	go func() {
		if err := searchsvc.NewIndexer(searchClient, repo).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("search indexer", "err", err)
		}
	}()

	// Reconcile counters after manual edits or interrupted migrations.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := registrationRepo.ReconcileCounters(ctx); err != nil {
					slog.Error("registration counter reconciliation", "err", err)
				}
			}
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
