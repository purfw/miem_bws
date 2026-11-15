// Package servicetoken signs internal requests and rejects replayed tokens.
package servicetoken

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/confhub/shared/httputil"
)

const (
	HeaderName    = "X-Service-Token"
	maxClockSkew  = 5 * time.Minute
	nonceTTL      = 10 * time.Minute
	nonceRedisKey = "svc_nonce:"
)

func SignRequest(secret string, r *http.Request, body []byte) string {
	if id := httputil.RequestIDFromContext(r.Context()); id != "" {
		r.Header.Set("X-Request-ID", id)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.NewString()
	return "v2." + ts + "." + nonce + "." + signature(secret, r.Method, r.URL.RequestURI(), ts, nonce, body)
}

func signature(secret, method, target, ts, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "v2\n%s\n%s\n%s\n%s\n%x", method, target, ts, nonce, digest)
	return hex.EncodeToString(mac.Sum(nil))
}

type Verifier struct {
	secret string
	rdb    *redis.Client
}

func NewVerifier(secret string, rdb *redis.Client) *Verifier {
	return &Verifier{secret: secret, rdb: rdb}
}

// Verify returns the consumed body for downstream handlers.
func (v *Verifier) Verify(ctx context.Context, r *http.Request) ([]byte, error) {
	tok := r.Header.Get(HeaderName)
	if tok == "" {
		return nil, fmt.Errorf("missing service token: %w", httputil.ErrUnauthorized)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 4 || parts[0] != "v2" || len(parts[3]) != 64 {
		return nil, fmt.Errorf("malformed token: %w", httputil.ErrUnauthorized)
	}
	if _, err := uuid.Parse(parts[2]); err != nil {
		return nil, httputil.ErrUnauthorized
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad ts: %w", httputil.ErrUnauthorized)
	}
	now := time.Now().Unix()
	if now-ts > int64(maxClockSkew.Seconds()) || ts-now > int64(maxClockSkew.Seconds()) {
		return nil, fmt.Errorf("token expired: %w", httputil.ErrUnauthorized)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", httputil.ErrInternal)
	}
	if len(body) > 1<<20 {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "body", Message: "maximum 1 MiB"}}}
	}
	expected := signature(v.secret, r.Method, r.URL.RequestURI(), parts[1], parts[2], body)
	if !hmac.Equal([]byte(expected), []byte(parts[3])) {
		return nil, fmt.Errorf("bad signature: %w", httputil.ErrUnauthorized)
	}
	ok, err := v.rdb.SetNX(ctx, nonceRedisKey+parts[2], "1", nonceTTL).Result()
	if err != nil {
		return nil, fmt.Errorf("nonce check: %w", httputil.ErrInternal)
	}
	if !ok {
		return nil, fmt.Errorf("nonce reused: %w", httputil.ErrUnauthorized)
	}
	return body, nil
}

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := v.Verify(r.Context(), r)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}
