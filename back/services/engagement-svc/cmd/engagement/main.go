package main

import (
	"context"
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
	sharedpgx "github.com/confhub/shared/pgx"
	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/slogx"
	"github.com/confhub/shared/telemetry"
	"github.com/confhub/shared/transport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/confhub/engagement-svc/internal/audit"
	"github.com/confhub/engagement-svc/internal/client"
	"github.com/confhub/engagement-svc/internal/config"
	"github.com/confhub/engagement-svc/internal/consumer"
	"github.com/confhub/engagement-svc/internal/mailer"
	"github.com/confhub/engagement-svc/internal/subscriptions"
	"github.com/confhub/engagement-svc/internal/support"
)

const serviceName = "engagement-svc"

func main() {
	cfg := config.Load()
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

	pool, err := sharedpgx.Connect(ctx, cfg.DatabaseURL, 10)
	if err != nil {
		slog.Error("pg", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	redisOpt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("redis config", "err", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpt)
	defer rdb.Close()
	jwtVerifier, err := jwtauth.NewVerifier(cfg.JWTPublicKey, cfg.JWTIssuer)
	if err != nil {
		slog.Error("jwt verifier", "err", err)
		os.Exit(1)
	}
	jwtVerifier.WithRevocationStore(rdb)
	jwtVerifier.WithStateResolver(jwtauth.RemoteStateResolver(cfg.IdentitySvcURL, cfg.ServiceTokenSecret))

	nc, err := natsx.Connect(cfg.NATSURL, serviceName)
	if err != nil {
		slog.Error("nats", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	m, err := mailer.New(mailer.Config{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.SMTPFrom, TLSMode: cfg.SMTPTLSMode,
	}, pool, "/app/templates/*.html")
	if err != nil {
		slog.Error("mailer", "err", err)
		os.Exit(1)
	}
	go m.RunRetry(ctx)

	identity := client.NewIdentity(cfg.IdentitySvcURL, cfg.ServiceTokenSecret)
	conferences := client.NewConferences(cfg.ConferenceSvcURL, cfg.ServiceTokenSecret)
	delivery := subscriptions.NewDelivery(pool, m, identity, conferences, cfg.BaseURL)
	go delivery.RunReminders(ctx)
	cns := consumer.New(pool, m, identity, cfg.BaseURL).WithSubscriptions(delivery)
	failures := natsx.NewFailureStore(pool, "engagement_svc")
	go failures.Run(ctx)
	if err := cns.Subscribe(ctx, nc, failures); err != nil {
		slog.Error("subscribe", "err", err)
		os.Exit(1)
	}

	mux := chi.NewRouter()
	stats := metrics.New("engagement-svc", pool)
	mux.Use(httputil.RequestID, telemetry.Middleware, stats.Middleware, httputil.Recover, httputil.AccessLog)
	mux.Handle("/metrics", stats.Handler())
	models := subscriptions.APIModels()
	for name, model := range support.APIModels() {
		models[name] = model
	}
	models["POST /internal/admin/auth-audit"] = openapi.Models{Request: audit.LoginRequest{}, Status: 204}
	mux.Get("/openapi.json", openapi.Handler(mux, serviceName, models))
	mux.Route("/internal", func(r chi.Router) {
		r.Use(servicetoken.NewVerifier(cfg.ServiceTokenSecret, rdb).Middleware)
		r.Post("/admin/auth-audit", audit.LoginHandler(pool))
		operations.New(pool, "engagement_svc", func(ctx context.Context, tx pgx.Tx, actor uuid.UUID, event string, id uuid.UUID, queue string) error {
			return audit.Insert(ctx, tx, audit.Entry{EventID: uuid.New(), EventType: event, ActorID: &actor, EntityType: "queue", EntityID: &id, Payload: map[string]string{"queue": queue}})
		}).Routes(r)
	})

	supportH := support.New(pool, m, cfg.SupportEmail)
	mux.Post("/api/v1/support/messages", supportH.Create)
	subscriptionH := subscriptions.New(pool)
	mux.Route("/api/v1/subscriptions", func(r chi.Router) {
		subscriptionH.OpenRoutes(r)
		r.Group(func(r chi.Router) {
			r.Use(jwtVerifier.Middleware(false))
			subscriptionH.AuthRoutes(r)
		})
	})

	mux.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		c, c2 := context.WithTimeout(r.Context(), 2*time.Second)
		defer c2()
		if err := pool.Ping(c); err != nil {
			http.Error(w, "pg down", http.StatusServiceUnavailable)
			return
		}
		if !nc.IsConnected() {
			http.Error(w, "nats down", http.StatusServiceUnavailable)
			return
		}
		if err := rdb.Ping(c).Err(); err != nil {
			http.Error(w, "redis down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := openapi.ValidateRoutes(mux, serviceName, models); err != nil {
		slog.Error("API contract", "err", err)
		os.Exit(1)
	}
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		slog.Info("listening", "port", cfg.Port)
		if err := transport.ListenAndServe(srv); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	c2, cc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cc()
	_ = transport.Shutdown(c2, srv)
}
