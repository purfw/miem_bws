// Package identity looks up accounts in identity-svc over its internal API.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
)

var ErrNotFound = errors.New("user not found")

type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Role      string `json:"role"`
	Active    bool   `json:"is_active"`
}

func (u User) Name() string { return strings.TrimSpace(u.FirstName + " " + u.LastName) }

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(baseURL, secret string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), secret: secret, http: transport.NewClient(3 * time.Second)}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, nil))
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("identity-svc %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ByEmail returns the active account with exactly this e-mail address.
func (c *Client) ByEmail(ctx context.Context, email string) (User, error) {
	email = strings.TrimSpace(email)
	var page struct {
		Data []User `json:"data"`
	}
	if err := c.get(ctx, "/internal/users?q="+url.QueryEscape(email), &page); err != nil {
		return User{}, err
	}
	for _, u := range page.Data {
		if strings.EqualFold(u.Email, email) && u.Active {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

// ByID returns the account with this id; Active is not reported by this call.
func (c *Client) ByID(ctx context.Context, id string) (User, error) {
	var u User
	err := c.get(ctx, "/internal/users/"+url.PathEscape(id), &u)
	return u, err
}
