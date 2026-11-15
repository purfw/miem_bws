package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type Health struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
	nc   *nats.Conn
}

func NewHealth(pool *pgxpool.Pool, rdb *redis.Client, nc *nats.Conn) *Health {
	return &Health{pool: pool, rdb: rdb, nc: nc}
}

func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.pool.Ping(ctx); err != nil {
		http.Error(w, "pg down", http.StatusServiceUnavailable)
		return
	}
	if h.rdb != nil {
		if err := h.rdb.Ping(ctx).Err(); err != nil {
			http.Error(w, "redis down", http.StatusServiceUnavailable)
			return
		}
	}
	if h.nc != nil && !h.nc.IsConnected() {
		http.Error(w, "nats down", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
