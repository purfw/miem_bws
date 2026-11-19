package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLoginRateLimiterAlwaysSetsFixedWindow(t *testing.T) {
	client := isolatedRedis(t)
	limiter := NewLoginRateLimiter(client, 2, time.Minute)
	ctx := context.Background()
	ip := uuid.NewString()
	t.Cleanup(func() { limiter.Reset(ctx, ip, "") })
	for i := 0; i < 2; i++ {
		if err := limiter.Check(ctx, ip, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Expire(ctx, key(ip, ""), 10*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Check(ctx, ip, ""); !errors.Is(err, ErrTooMany) {
		t.Fatalf("limit not enforced: %v", err)
	}
	ttl, err := client.TTL(ctx, key(ip, "")).Result()
	if err != nil || ttl <= 0 || ttl > 10*time.Second {
		t.Fatalf("window extended or missing: ttl=%v err=%v", ttl, err)
	}
}
