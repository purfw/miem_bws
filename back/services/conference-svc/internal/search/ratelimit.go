package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	rdb   *redis.Client
	limit int64
}

func NewRateLimiter(rdb *redis.Client, limit int) *RateLimiter {
	if limit < 1 {
		limit = 200
	}
	return &RateLimiter{rdb: rdb, limit: int64(limit)}
}

func (l *RateLimiter) Allow(ctx context.Context, ip string) (bool, error) {
	sum := sha256.Sum256([]byte(ip))
	key := "rate:search:" + hex.EncodeToString(sum[:8]) + ":" + time.Now().UTC().Format("200601021504")
	pipe := l.rdb.TxPipeline()
	n := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 2*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return n.Val() <= l.limit, nil
}
