package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
)

var ErrUserNotFound = errors.New("user not found")

// Identity resolves locale without caching so the next message uses profile changes.
type Identity struct {
	hc     *http.Client
	url    string
	secret string
}

func NewIdentity(svcURL, secret string) *Identity {
	return &Identity{
		hc:     transport.NewClient(5 * time.Second),
		url:    strings.TrimRight(svcURL, "/"),
		secret: secret,
	}
}

func (c *Identity) Locale(ctx context.Context, id uuid.UUID) (string, error) {
	body := []byte{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/internal/users/"+id.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, body))
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrUserNotFound
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return "", fmt.Errorf("identity locale: %d %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var user struct {
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("decode identity locale: %w", err)
	}
	if user.Locale == "en" {
		return "en", nil
	}
	return "ru", nil
}
