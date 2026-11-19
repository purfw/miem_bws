package service

import (
	"bufio"
	"context"
	"crypto/sha1" // #nosec G505 -- mandated k-anonymity lookup, not password storage
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/confhub/shared/httputil"
)

const weakPasswordMessage = "Пароль обнаружен в перечне скомпрометированных учётных данных. Укажите другой пароль"

type WeakPasswordError struct{}

func (WeakPasswordError) Error() string { return weakPasswordMessage }
func (WeakPasswordError) Unwrap() error {
	return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "password", Message: weakPasswordMessage}}}
}
func (WeakPasswordError) ProblemCode() string { return "WEAK_PASSWORD" }

type PasswordChecker interface {
	Compromised(context.Context, string) (bool, error)
}

type PwnedChecker struct {
	baseURL string
	client  *http.Client
}

func NewPwnedChecker(baseURL string, timeout time.Duration) *PwnedChecker {
	return &PwnedChecker{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *PwnedChecker) Compromised(ctx context.Context, password string) (bool, error) {
	sum := sha1.Sum([]byte(password)) // #nosec G401 -- required by Pwned Passwords range protocol
	hexHash := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hexHash[:5], hexHash[5:]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/range/"+prefix, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "ConfHub-Password-Checker/1.0")
	resp, err := p.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("pwned passwords lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("pwned passwords lookup: status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	for scanner.Scan() {
		candidate, frequency, ok := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		if ok && strings.EqualFold(candidate, suffix) {
			count, err := strconv.ParseUint(frequency, 10, 64)
			if err != nil {
				return false, errors.New("invalid password range response")
			}
			return count > 0, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func validatePassword(ctx context.Context, checker PasswordChecker, password string) error {
	if utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "password", Message: "min 12 chars"}}}
	}
	compromised, err := checker.Compromised(ctx, password)
	if err != nil {
		// Reject passwords if the breach check is unavailable.
		return fmt.Errorf("password reputation service: %w", httputil.ErrUnavailable)
	}
	if compromised {
		return WeakPasswordError{}
	}
	return nil
}
