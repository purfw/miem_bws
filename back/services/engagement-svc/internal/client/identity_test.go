package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/confhub/shared/servicetoken"
)

func TestIdentityLocale(t *testing.T) {
	userID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/users/"+userID.String() {
			http.NotFound(w, r)
			return
		}
		if parts := strings.Split(r.Header.Get(servicetoken.HeaderName), "."); len(parts) != 4 {
			t.Fatalf("service token has %d parts", len(parts))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"locale":"en"}`))
	}))
	defer server.Close()

	locale, err := NewIdentity(server.URL, "test-secret").Locale(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if locale != "en" {
		t.Fatalf("locale = %q, want en", locale)
	}
}

func TestIdentityLocaleNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := NewIdentity(server.URL, "test-secret").Locale(context.Background(), uuid.New())
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}
