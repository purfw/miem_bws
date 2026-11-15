package jwtauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDurableSessionVersion(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &Issuer{priv: private, iss: "test", ttl: time.Hour}
	verifier := &Verifier{pub: public, iss: "test"}
	version := int64(3)
	active := true
	verifier.WithStateResolver(func(context.Context, string) (AuthState, error) {
		return AuthState{Version: version, Active: active}, nil
	})
	token, _, err := issuer.Issue(uuid.NewString(), RoleUser, "user@example.test", "User", true, version)
	if err != nil {
		t.Fatal(err)
	}
	call := func() int {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/private", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		verifier.Middleware(false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(out, req)
		return out.Code
	}
	if got := call(); got != http.StatusNoContent {
		t.Fatalf("active HTTP %d", got)
	}
	version++
	if got := call(); got != http.StatusUnauthorized {
		t.Fatalf("old version HTTP %d", got)
	}
	version--
	active = false
	if got := call(); got != http.StatusUnauthorized {
		t.Fatalf("blocked HTTP %d", got)
	}
}

func TestPublicFallbackDoesNotKeepAuthentication(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	issuer := &Issuer{priv: private, iss: "test", ttl: time.Hour}
	verifier := &Verifier{pub: public, iss: "test"}
	verifier.WithStateResolver(func(context.Context, string) (AuthState, error) {
		return AuthState{}, errors.New("identity unavailable")
	})
	token, _, _ := issuer.Issue(uuid.NewString(), RoleAdmin, "admin@example.test", "Admin", true)
	request := httptest.NewRequest(http.MethodGet, "http://example.test/public", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	verifier.Middleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ClaimsFromContext(r.Context()) != nil {
			t.Fatal("authentication survived failed verification")
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), request)
}
