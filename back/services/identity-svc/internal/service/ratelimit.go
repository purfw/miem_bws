package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type LoginRateLimiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func NewLoginRateLimiter(rdb *redis.Client, limit int, window time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{rdb: rdb, limit: limit, window: window}
}

func key(ip, _ string) string {
	h := sha256.Sum256([]byte(ip))
	return "auth:login_attempts:" + hex.EncodeToString(h[:8])
}

func (l *LoginRateLimiter) Check(ctx context.Context, ip, email string) error {
	k := key(ip, email)
	// NX keeps the window fixed from the first attempt; running it in the same
	// transaction guarantees the counter never exists without a TTL.
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.ExpireNX(ctx, k, l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	if int(incr.Val()) > l.limit {
		return ErrTooMany
	}
	return nil
}

func (l *LoginRateLimiter) Reset(ctx context.Context, ip, email string) {
	_ = l.rdb.Del(ctx, key(ip, email)).Err()
}

var ErrTooMany = errors.New("too many login attempts")
