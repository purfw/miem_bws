// Package jwtauth signs and verifies Ed25519 JWTs.
package jwtauth

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/confhub/shared/httputil"
)

type Role string

const (
	RoleAnonymous         Role = ""
	RoleUser              Role = "user"
	RoleVerifiedOrganizer Role = "verified_organizer"
	RoleOrganizerMember   Role = "organizer_member"
	RoleAdmin             Role = "admin"
)

var roleRank = map[Role]int{
	RoleAnonymous:         0,
	RoleUser:              1,
	RoleOrganizerMember:   2,
	RoleVerifiedOrganizer: 2,
	RoleAdmin:             3,
}

func (r Role) AtLeast(min Role) bool { return roleRank[r] >= roleRank[min] }

type Claims struct {
	UserID        string `json:"sub"`
	Role          Role   `json:"role"`
	Email         string `json:"email,omitempty"`
	Name          string `json:"name,omitempty"`
	EmailVerified bool   `json:"email_verified"`
	AuthVersion   int64  `json:"auth_version"`
	jwt.RegisteredClaims
}

type Issuer struct {
	priv ed25519.PrivateKey
	kid  string
	ttl  time.Duration
	iss  string
}

type Verifier struct {
	pub           ed25519.PublicKey
	kid           string
	iss           string
	redis         *redis.Client
	stateResolver func(context.Context, string) (AuthState, error)
}

type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	X   string `json:"x"`
}

type JWKSet struct {
	Keys []JWK `json:"keys"`
}

func keyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

func NewIssuer(path string, ttl time.Duration, iss string) (*Issuer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("invalid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an ed25519 private key")
	}
	return &Issuer{priv: priv, kid: keyID(priv.Public().(ed25519.PublicKey)), ttl: ttl, iss: iss}, nil
}

func NewVerifier(path, iss string) (*Verifier, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("invalid PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an ed25519 public key")
	}
	return &Verifier{pub: pub, kid: keyID(pub), iss: iss}, nil
}

// WithRevocationStore enables Redis-based revocation checks. They apply only
// when no state resolver is set; WithStateResolver takes precedence.
func (v *Verifier) WithRevocationStore(rdb *redis.Client) *Verifier {
	v.redis = rdb
	return v
}

func (v *Verifier) JWKSet() JWKSet {
	return JWKSet{Keys: []JWK{{
		Kty: "OKP", Crv: "Ed25519", Use: "sig", Alg: "EdDSA", Kid: v.kid,
		X: base64.RawURLEncoding.EncodeToString(v.pub),
	}}}
}

func (i *Issuer) Issue(userID string, role Role, email, name string, emailVerified bool, versions ...int64) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.ttl)
	claims := Claims{
		UserID:        userID,
		Role:          role,
		Email:         email,
		Name:          name,
		EmailVerified: emailVerified,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.iss,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	if len(versions) > 0 {
		claims.AuthVersion = versions[0]
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = i.kid
	signed, err := tok.SignedString(i.priv)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

func (v *Verifier) Verify(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, errors.New("unexpected alg")
		}
		return v.pub, nil
	}, jwt.WithIssuer(v.iss), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	c, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

type ctxKey int

const (
	ctxClaims ctxKey = iota
)

func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(ctxClaims).(*Claims)
	return c
}

func (v *Verifier) Middleware(optional bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ah := r.Header.Get("Authorization")
			if ah == "" {
				if optional {
					next.ServeHTTP(w, r)
					return
				}
				httputil.WriteError(w, r, fmt.Errorf("missing bearer: %w", httputil.ErrUnauthorized))
				return
			}
			if !strings.HasPrefix(ah, "Bearer ") {
				httputil.WriteError(w, r, fmt.Errorf("bad auth header: %w", httputil.ErrUnauthorized))
				return
			}
			tok := strings.TrimPrefix(ah, "Bearer ")
			claims, err := v.Verify(tok)
			if err != nil {
				httputil.WriteError(w, r, fmt.Errorf("invalid token: %w", httputil.ErrUnauthorized))
				return
			}
			if v.stateResolver != nil {
				state, err := v.stateResolver(r.Context(), claims.UserID)
				if err != nil || !state.Active || state.Version != claims.AuthVersion {
					if err != nil && !errors.Is(err, httputil.ErrUnauthorized) {
						if optional {
							next.ServeHTTP(w, r)
							return
						}
						httputil.WriteError(w, r, fmt.Errorf("account state unavailable: %w", httputil.ErrUnavailable))
					} else {
						httputil.WriteError(w, r, httputil.ErrUnauthorized)
					}
					return
				}
			}
			if v.redis != nil && v.stateResolver == nil {
				blocked, redisErr := v.redis.Exists(r.Context(), "auth:blocked:"+claims.UserID).Result()
				if redisErr != nil {
					httputil.WriteError(w, r, fmt.Errorf("revocation store unavailable: %w", httputil.ErrUnavailable))
					return
				}
				if blocked > 0 {
					httputil.WriteError(w, r, fmt.Errorf("account blocked: %w", httputil.ErrUnauthorized))
					return
				}
				if revokedAt, redisErr := v.redis.Get(r.Context(), "auth:revoked_before:"+claims.UserID).Int64(); redisErr == nil && claims.IssuedAt != nil && claims.IssuedAt.Unix() < revokedAt {
					httputil.WriteError(w, r, fmt.Errorf("session revoked: %w", httputil.ErrUnauthorized))
					return
				} else if redisErr != nil && !errors.Is(redisErr, redis.Nil) {
					httputil.WriteError(w, r, fmt.Errorf("revocation store unavailable: %w", httputil.ErrUnavailable))
					return
				}
			}
			ctx := context.WithValue(r.Context(), ctxClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
