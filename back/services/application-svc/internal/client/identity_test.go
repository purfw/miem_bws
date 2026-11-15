package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
