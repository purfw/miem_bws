package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/shared/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestAdminUserSearchIsPaginatedLiteralAndDoesNotExposeCredentials(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	prefix := uuid.NewString()
	for i := 0; i < 52; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO identity_svc.users(id,email,password_hash,first_name,last_name,role,deleted_at)
		VALUES($1,$2,'private-password-hash','Search','Person','user',CASE WHEN $3 THEN now() ELSE NULL END)`, uuid.New(), prefix+uuid.NewString()+"@example.test", i == 51); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM identity_svc.users WHERE strpos(email,$1)>0`, prefix) })
	router := chi.NewRouter()
	router.Route("/internal", NewInternal(nil, repository.NewUsers(pool)).Routes)
	for page, want := range map[string]int{"1": 50, "2": 1} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/internal/users?q="+prefix+"&page="+page, nil))
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		var body adminUsersResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Data) != want || body.HasMore != (page == "1") {
			t.Fatalf("page %s: %+v", page, body)
		}
		if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "auth_version") {
			t.Fatal("credential fields exposed")
		}
	}
	for _, query := range []string{"%25", "%27%20OR%201%3D1--"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/internal/users?q="+query, nil))
		var body adminUsersResponse
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Data) != 0 {
			t.Fatal("search input interpreted as SQL")
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/internal/users?page=-1", nil))
	if response.Code != 422 && response.Code != 400 {
		t.Fatal("invalid page accepted", response.Code)
	}
}

func TestAdminUserSearchLimitsUnicodeCharacters(t *testing.T) {
	pool := testutil.Database(t)
	router := chi.NewRouter()
	router.Route("/internal", NewInternal(nil, repository.NewUsers(pool)).Routes)
	for _, test := range []struct {
		length int
		status int
	}{{255, 200}, {256, 422}} {
		response := httptest.NewRecorder()
		query := url.Values{"q": {strings.Repeat("Я", test.length)}}
		router.ServeHTTP(response, httptest.NewRequest("GET", "/internal/users?"+query.Encode(), nil))
		if response.Code != test.status {
			t.Fatalf("%d Unicode characters: status=%d, want=%d", test.length, response.Code, test.status)
		}
	}
}
