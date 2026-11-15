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
	n, err := l.rdb.Incr(ctx, k).Result()
	if err != nil {
		return err
	}
	if n == 1 {
		_ = l.rdb.Expire(ctx, k, l.window).Err()
	}
	if int(n) > l.limit {
		return ErrTooMany
	}
	return nil
}

func (l *LoginRateLimiter) Reset(ctx context.Context, ip, email string) {
	_ = l.rdb.Del(ctx, key(ip, email)).Err()
}

var ErrTooMany = errors.New("too many login attempts")
