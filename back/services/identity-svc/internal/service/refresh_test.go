package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func isolatedRedis(t *testing.T) *redis.Client {
	t.Helper()
	address := os.Getenv("TEST_REDIS_URL")
	if address == "" {
		t.Skip("TEST_REDIS_URL is not configured")
	}
	options, err := redis.ParseURL(address)
	if err != nil || !strings.HasPrefix(options.Addr, "confhub-test-redis-") {
		t.Fatal("refusing a non-test Redis")
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRefreshRotationReplayAndLogoutKeepOtherSessions(t *testing.T) {
	client := isolatedRedis(t)
	store := NewRefreshStore(client, time.Hour)
	ctx := context.Background()
	user := uuid.New()
	old, err := store.Issue(ctx, user, "browser", 7)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Issue(ctx, user, "other browser", 7)
	if err != nil {
		t.Fatal(err)
	}
	next, id, err := store.Rotate(ctx, old, "browser")
	if err != nil || id != user || next == old {
		t.Fatalf("rotation id=%s err=%v", id, err)
	}
	payload, err := store.lookup(ctx, tokenKey(next))
	if err != nil || payload.AuthVersion != 7 || payload.UserAgent != "browser" {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
	if _, _, err := store.Rotate(ctx, old, "replay"); !errors.Is(err, ErrRefreshReplay) {
		t.Fatalf("replay=%v", err)
	}
	if _, err := store.Lookup(ctx, next); !errors.Is(err, ErrRefreshNotFound) {
		t.Fatal("replay did not revoke replacement")
	}
	if id, err := store.Lookup(ctx, other); err != nil || id != user {
		t.Fatal("unrelated session revoked")
	}
	newOther, _, err := store.Rotate(ctx, other, "other browser")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(ctx, newOther); !errors.Is(err, ErrRefreshNotFound) {
		t.Fatal("logout using rotated cookie left the session alive")
	}
	if err := store.Revoke(ctx, uuid.NewString()); err != nil {
		t.Fatal("missing-token logout is not idempotent")
	}
}

func TestRefreshRevocationIsAtomicWithConcurrentRotation(t *testing.T) {
	client := isolatedRedis(t)
	store := NewRefreshStore(client, time.Hour)
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		user := uuid.New()
		old, err := store.Issue(ctx, user, "browser", 3)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := store.lookup(ctx, tokenKey(old))
		if err != nil {
			t.Fatal(err)
		}
		ready := make(chan struct{})
		var group sync.WaitGroup
		var next string
		group.Go(func() { <-ready; next, _, _ = store.Rotate(ctx, old, "browser") })
		group.Go(func() {
			<-ready
			if err := store.RevokeFamily(ctx, user, payload.FamilyID); err != nil {
				t.Error(err)
			}
		})
		close(ready)
		group.Wait()
		if next != "" {
			if _, err := store.Lookup(ctx, next); !errors.Is(err, ErrRefreshNotFound) {
				t.Fatal("concurrent rotation survived family revocation")
			}
		}
		if _, err := store.Lookup(ctx, old); !errors.Is(err, ErrRefreshNotFound) {
			t.Fatal("old token survived family revocation")
		}
	}
}

func TestRevokeAllCleansLegacyTokensAndBlocksUser(t *testing.T) {
	client := isolatedRedis(t)
	store := NewRefreshStore(client, time.Hour)
	ctx := context.Background()
	user, otherUser := uuid.New(), uuid.New()
	first, err := store.Issue(ctx, user, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Issue(ctx, user, "second")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Issue(ctx, otherUser, "other")
	if err != nil {
		t.Fatal(err)
	}
	legacy := tokenKey(uuid.NewString())
	if err := client.Set(ctx, legacy, `{"legacy":true}`, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.SAdd(ctx, legacyUserIndexKey(user), legacy, "unrelated-key").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "unrelated-key", "keep", time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.BlockUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{first, second} {
		if _, err := store.Lookup(ctx, token); !errors.Is(err, ErrRefreshNotFound) {
			t.Fatal("user token survived global revocation")
		}
	}
	if count, err := client.Exists(ctx, legacy).Result(); err != nil || count != 0 {
		t.Fatal("legacy token survived revocation")
	}
	if value, err := client.Get(ctx, "unrelated-key").Result(); err != nil || value != "keep" {
		t.Fatal("legacy index deleted unrelated data")
	}
	if _, err := store.Lookup(ctx, other); err != nil {
		t.Fatal("other user's token revoked")
	}
	if count, err := client.Exists(ctx, "auth:blocked:"+user.String()).Result(); err != nil || count != 1 {
		t.Fatal("block marker missing")
	}
	if err := store.UnblockUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if count, err := client.Exists(ctx, "auth:blocked:"+user.String()).Result(); err != nil || count != 0 {
		t.Fatal("block marker remains")
	}
}

func TestLoginRateLimitIsPerAddressAndHasExpiry(t *testing.T) {
	client := isolatedRedis(t)
	ctx := context.Background()
	limiter := NewLoginRateLimiter(client, 10, time.Minute)
	ip := uuid.NewString()
	for i := 0; i < 10; i++ {
		if err := limiter.Check(ctx, ip, uuid.NewString()+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := limiter.Check(ctx, ip, "another@example.test"); !errors.Is(err, ErrTooMany) {
		t.Fatal("changing email bypassed address limit")
	}
	if ttl, err := client.TTL(ctx, key(ip, "")).Result(); err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("limiter ttl=%v err=%v", ttl, err)
	}
	if strings.Contains(key(ip, ""), ip) {
		t.Fatal("address was stored in plaintext")
	}
	limiter.Reset(ctx, ip, "")
	if err := limiter.Check(ctx, ip, ""); err != nil {
		t.Fatal("limiter reset failed")
	}
}
