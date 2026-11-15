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

	"github.com/confhub/application-svc/internal/client"
	"github.com/confhub/application-svc/internal/config"
	"github.com/confhub/application-svc/internal/handler"
	"github.com/confhub/application-svc/internal/queue"
	"github.com/confhub/application-svc/internal/repository"
	"github.com/confhub/application-svc/internal/service"
	"github.com/confhub/application-svc/internal/storage"
)

const serviceName = "application-svc"

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
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}

	pool, err := sharedpgx.Connect(ctx, cfg.DatabaseURL, 15)
	if err != nil {
		slog.Error("pg", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	redisOpt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("redis", "err", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpt)
	defer rdb.Close()

	nc, err := natsx.Connect(cfg.NATSURL, serviceName)
	if err != nil {
		slog.Error("nats", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	jwtVerifier, err := jwtauth.NewVerifier(cfg.JWTPublicKeyPath, cfg.JWTIssuer)
	if err != nil {
		slog.Error("jwt", "err", err)
		os.Exit(1)
	}
	jwtVerifier.WithRevocationStore(rdb)
	jwtVerifier.WithStateResolver(jwtauth.RemoteStateResolver(cfg.IdentitySvcURL, cfg.ServiceTokenSecret))

	store, err := storage.New(storage.Config{
		Endpoint:       cfg.MinIOEndpoint,
		PublicEndpoint: cfg.MinIOPublicEndpoint,
		AccessKey:      cfg.MinIOAccessKey,
		SecretKey:      cfg.MinIOSecretKey,
		UseSSL:         cfg.MinIOUseSSL,
		PublicUseSSL:   cfg.MinIOPublicUseSSL,
		Bucket:         cfg.MinIOBucket,
		PresignTTL:     cfg.PresignTTL,
	})
	if err != nil {
		slog.Error("minio", "err", err)
		os.Exit(1)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		slog.Error("ensure bucket", "err", err)
		os.Exit(1)
	}

	confCache := client.NewConferenceCache(cfg.ConferenceSvcURL, cfg.ServiceTokenSecret, cfg.ConferenceCacheTTL)
	if err := confCache.Subscribe(nc); err != nil {
		slog.Error("subscribe conference cache", "err", err)
		os.Exit(1)
	}
	confCache.StartSweeper(ctx)

	identity := client.NewIdentity(cfg.IdentitySvcURL, cfg.ServiceTokenSecret)
	registrations := client.NewRegistrationClient(cfg.ConferenceSvcURL, cfg.ServiceTokenSecret)

	repo := repository.NewApplications(pool)
	svc := service.New(repo, store, confCache, identity, registrations, cfg.MaxFileSizeBytes, cfg.BaseURL, cfg.PDFFontPath, cfg.ORCIDPublicationThreshold)
	go svc.RunVerificationRoles(ctx)
	go svc.RunStorageCleanup(ctx)

	failures := natsx.NewFailureStore(pool, "application_svc")
	go failures.Run(ctx)
	if err := queue.SubscribeConferenceCancellation(ctx, nc, svc, failures); err != nil {
		slog.Error("subscribe compensation", "err", err)
		os.Exit(1)
	}
	if err := queue.SubscribeDocumentGeneration(ctx, nc, svc, failures); err != nil {
		slog.Error("subscribe document generation", "err", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	stats := metrics.New("application-svc", pool)
	r.Use(httputil.RequestID, telemetry.Middleware, stats.Middleware, httputil.Recover, httputil.AccessLog)
	r.Handle("/metrics", stats.Handler())
	r.Get("/openapi.json", openapi.Handler(r, serviceName, handler.APIModels()))

	apps := handler.NewApplications(svc)
	files := handler.NewFiles(apps)
	internal := handler.NewInternal(svc)
	healthH := handler.NewHealth(pool, rdb, nc)

	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			apps.OpenRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(jwtVerifier.Middleware(false))
			apps.PublicRoutes(r)
			r.Route("/files", func(r chi.Router) {
				files.Routes(r)
			})
		})
	})

	stVerifier := servicetoken.NewVerifier(cfg.ServiceTokenSecret, rdb)
	r.Route("/internal", func(r chi.Router) {
		r.Use(stVerifier.Middleware)
		operations.New(pool, "application_svc", nil).Routes(r)
		internal.Routes(r)
	})

	r.Get("/healthz", healthH.Live)
	r.Get("/readyz", healthH.Ready)
	if err := openapi.ValidateRoutes(r, serviceName, handler.APIModels()); err != nil {
		slog.Error("API contract", "err", err)
		os.Exit(1)
	}

	worker := outbox.New(pool, nc, outbox.Config{Schema: "application_svc"})
	go func() {
		if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("outbox", "err", err)
		}
	}()

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
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
	shutdown, sc := context.WithTimeout(context.Background(), 30*time.Second)
	defer sc()
	_ = transport.Shutdown(shutdown, srv)
}
