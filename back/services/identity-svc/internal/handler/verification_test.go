package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/identity-svc/internal/service"
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestResendRequiresAuthenticatedAccountWithoutReadingBody(t *testing.T) {
	r := chi.NewRouter()
	NewAuth(nil, 0, false, jwtauth.JWKSet{}, nil, "").AuthenticatedRoutes(r)
	for _, body := range []string{`{"email":"other@example.test"}`, "broken JSON", ""} {
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("POST", "/resend-verification", strings.NewReader(body)))
		if response.Code != 401 {
			t.Fatalf("anonymous resend=%d", response.Code)
		}
	}
}

func TestResendIgnoresBodyEmailAndUsesVerifiedJWTSubject(t *testing.T) {
	pool := testutil.Database(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(private)
	publicDER, _ := x509.MarshalPKIXPublicKey(public)
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
	owner, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, other} {
		if _, err := pool.Exec(context.Background(), `INSERT INTO identity_svc.users(id,email,password_hash,first_name,last_name) VALUES($1,$2,'test-hash','Test','User')`, id, id.String()+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	svc := service.NewAuth(repository.NewUsers(pool), nil, nil, nil, time.Hour, nil)
	r := chi.NewRouter()
	r.Use(verifier.Middleware(false))
	NewAuth(svc, 0, false, jwtauth.JWKSet{}, nil, "").AuthenticatedRoutes(r)
	token, _, err := issuer.Issue(owner.String(), jwtauth.RoleUser, "stale-claim@example.test", "Test User", false)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/resend-verification", strings.NewReader(`{"email":"`+other.String()+`@example.test"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	if response.Code != 202 || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("resend=%d", response.Code)
	}
	var recipient string
	if err := pool.QueryRow(context.Background(), `SELECT payload->>'user_email' FROM identity_svc.outbox_events WHERE aggregate_id=$1 AND event_type='user.verification_requested'`, owner).Scan(&recipient); err != nil {
		t.Fatal(err)
	}
	if recipient != owner.String()+"@example.test" {
		t.Fatal("request body or stale JWT email was trusted")
	}
	response = httptest.NewRecorder()
	r.ServeHTTP(response, req.Clone(context.Background()))
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit=%d", response.Code)
	}
}
