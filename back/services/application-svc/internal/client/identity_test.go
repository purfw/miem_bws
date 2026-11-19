package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSetRoleDeletedUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	identity := NewIdentity(server.URL, "test-secret")
	if err := identity.SetRole(context.Background(), uuid.New(), "verified_organizer", uuid.New()); !errors.Is(err, ErrUserMissing) {
		t.Fatalf("deleted user error: %v", err)
	}
}

func TestProfileCacheEvictsExpiredEntries(t *testing.T) {
	identity := NewIdentity("http://identity.invalid", "test-secret")
	start := time.Now()
	stale, fresh := uuid.New(), uuid.New()
	identity.store(stale, Profile{fetchedAt: start}, start)
	identity.store(fresh, Profile{fetchedAt: start.Add(profileTTL)}, start.Add(profileTTL))
	if _, ok := identity.cache[stale]; ok {
		t.Fatal("expired profile kept")
	}
	if _, ok := identity.cache[fresh]; !ok || len(identity.cache) != 1 {
		t.Fatalf("cache=%v", identity.cache)
	}
}
