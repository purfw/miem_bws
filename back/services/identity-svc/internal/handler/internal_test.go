package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/shared/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestInternalUserDeletedAndMissingReturnNotFound(t *testing.T) {
	pool := testutil.Database(t)
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO identity_svc.users(id,email,password_hash,first_name,last_name) VALUES($1,$2,'test-hash','Test','User')`, id, id.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewInternal(nil, repository.NewUsers(pool)).Routes(router)
	check := func(target uuid.UUID, status int) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/users/"+target.String(), nil))
		if response.Code != status {
			t.Fatalf("status=%d, want %d", response.Code, status)
		}
		if strings.Contains(response.Body.String(), "test-hash") {
			t.Fatal("password hash exposed")
		}
	}
	check(id, 200)
	check(uuid.New(), 404)
	if _, err := pool.Exec(context.Background(), `UPDATE identity_svc.users SET deleted_at=now(),is_active=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	check(id, 404)
}
