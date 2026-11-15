package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func testJWT(t *testing.T) (*jwtauth.Issuer, *jwtauth.Verifier) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	privatePath, publicPath := filepath.Join(directory, "private.pem"), filepath.Join(directory, "public.pem")
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0600); err != nil {
		t.Fatal(err)
	}
	issuer, err := jwtauth.NewIssuer(privatePath, time.Hour, "test")
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := jwtauth.NewVerifier(publicPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	return issuer, verifier
}

func registrationRouter(pool *pgxpool.Pool, rdb *redis.Client, verifier *jwtauth.Verifier) chi.Router {
	r := chi.NewRouter()
	r.Use(verifier.Middleware(true))
	h := NewRegistrations(repository.NewRegistrations(pool), rdb)
	r.Route("/api/v1/conferences", h.ConferenceRoutes)
	r.Route("/api/v1/registrations", h.RegistrationRoutes)
	return r
}

func registrationRequest(t *testing.T, r chi.Router, issuer *jwtauth.Issuer, method, path, body string, user uuid.UUID, verified bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != uuid.Nil {
		token, _, err := issuer.Issue(user.String(), jwtauth.RoleUser, "test@example.test", "Test User", verified)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	return response
}

func TestRegistrationForbiddenIsEmailVerificationNotAccountRegistration(t *testing.T) {
	issuer, verifier := testJWT(t)
	r := registrationRouter(nil, nil, verifier)
	path := "/api/v1/conferences/" + uuid.NewString() + "/register"
	response := registrationRequest(t, r, issuer, "POST", path, `{"role_at_conf":"attendee"}`, uuid.New(), false)
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusForbidden || problem.Code != "EMAIL_NOT_VERIFIED" {
		t.Fatalf("actual gate: status=%d code=%s", response.Code, problem.Code)
	}
	response = registrationRequest(t, r, issuer, "POST", path, `{"role_at_conf":"attendee"}`, uuid.Nil, false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous registration=%d", response.Code)
	}
	response = registrationRequest(t, r, issuer, "GET", strings.TrimSuffix(path, "/register")+"/registration", "", uuid.Nil, false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status lookup=%d", response.Code)
	}
}

func TestAuthenticatedAttendeeRegistrationStatusAndCancellation(t *testing.T) {
	pool := testutil.Database(t)
	address := os.Getenv("TEST_REDIS_URL")
	if address == "" {
		t.Skip("TEST_REDIS_URL is not configured")
	}
	options, err := redis.ParseURL(address)
	if err != nil || !strings.HasPrefix(options.Addr, "confhub-test-redis-") {
		t.Fatal("refusing non-test Redis")
	}
	rdb := redis.NewClient(options)
	defer rdb.Close()
	issuer, verifier := testJWT(t)
	r := registrationRouter(pool, rdb, verifier)
	conference, user, other := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,status,created_by) VALUES ($1,'HTTP test',$2,'2027-03-01','2027-03-02','registration_open',$3)`, conference, uuid.NewString(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/conferences/" + conference.String()
	response := registrationRequest(t, r, issuer, "POST", path+"/register", `{"role_at_conf":"attendee"}`, user, true)
	if response.Code != 201 {
		t.Fatalf("attendee registration: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data *registrationDTO `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data == nil || body.Data.RoleAtConference != "attendee" {
		t.Fatalf("attendee response=%+v err=%v", body, err)
	}
	id := body.Data.ID
	response = registrationRequest(t, r, issuer, "POST", path+"/register", `{"role_at_conf":"attendee"}`, user, true)
	if response.Code != 409 {
		t.Fatalf("duplicate registration=%d", response.Code)
	}
	response = registrationRequest(t, r, issuer, "GET", path+"/registration", "", user, true)
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Data == nil || body.Data.ID != id {
		t.Fatal("own registration status missing")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("registration state can be cached")
	}
	response = registrationRequest(t, r, issuer, "GET", path+"/registration", "", other, true)
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Data != nil {
		t.Fatal("another user's registration was exposed")
	}
	response = registrationRequest(t, r, issuer, "DELETE", "/api/v1/registrations/"+id, "", other, true)
	if response.Code != 404 {
		t.Fatalf("foreign cancellation=%d", response.Code)
	}
	response = registrationRequest(t, r, issuer, "DELETE", "/api/v1/registrations/"+id, "", user, true)
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Data == nil || body.Data.Status != "cancelled" {
		t.Fatal("own cancellation failed")
	}
}
