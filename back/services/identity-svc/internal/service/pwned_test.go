package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/confhub/shared/httputil"
)

func TestPasswordLookupDisclosesOnlyPrefixAndIgnoresPadding(t *testing.T) {
	password := "test-only-password-never-used"
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	for _, tc := range []struct {
		name, count string
		status      int
		want        bool
		wantError   bool
	}{
		{"compromised", "42", 200, true, false},
		{"padding", "0", 200, false, false},
		{"malformed", "invalid", 200, false, true},
		{"unavailable", "42", 503, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/range/"+hash[:5] || r.URL.RawQuery != "" || r.Header.Get("Add-Padding") != "true" || r.ContentLength > 0 {
					t.Error("lookup revealed more than the hash prefix")
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA:1\r\n%s:%s\r\n", hash[5:], tc.count)
			}))
			defer server.Close()
			checker := NewPwnedChecker(server.URL, time.Second)
			got, err := checker.Compromised(context.Background(), password)
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("compromised=%v err=%v", got, err)
			}
		})
	}
}

type passwordCheckStub struct {
	compromised bool
	err         error
	calls       int
}

func (p *passwordCheckStub) Compromised(context.Context, string) (bool, error) {
	p.calls++
	return p.compromised, p.err
}

func TestPasswordValidationFailsClosedBeforeHashing(t *testing.T) {
	for _, tc := range []struct {
		name, password string
		breached       bool
		failure        error
		want           error
		calls          int
	}{
		{"short", "short", false, nil, httputil.ErrValidation, 0},
		{"oversized", strings.Repeat("x", 1025), false, nil, httputil.ErrValidation, 0},
		{"strong", "sufficiently-long-password", false, nil, nil, 1},
		{"compromised", "sufficiently-long-password", true, nil, httputil.ErrValidation, 1},
		{"outage", "sufficiently-long-password", false, errors.New("provider down"), httputil.ErrUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := &passwordCheckStub{compromised: tc.breached, err: tc.failure}
			err := validatePassword(context.Background(), checker, tc.password)
			if !errors.Is(err, tc.want) || checker.calls != tc.calls {
				t.Fatalf("err=%v calls=%d", err, checker.calls)
			}
			if tc.breached {
				if code, ok := err.(interface{ ProblemCode() string }); !ok || code.ProblemCode() != "WEAK_PASSWORD" {
					t.Fatal("weak password field error code missing")
				}
			}
		})
	}
}
