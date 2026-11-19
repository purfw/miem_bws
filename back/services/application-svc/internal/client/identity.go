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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
)

const profileTTL = 10 * time.Minute

var ErrUserMissing = errors.New("identity user no longer exists")

type Profile struct {
	Email     string
	Name      string
	fetchedAt time.Time
}

type ResearchIdentity struct {
	Provider         string `json:"provider"`
	Identifier       string `json:"identifier"`
	DisplayName      string `json:"display_name"`
	PublicationCount int    `json:"publication_count"`
}

type Identity struct {
	mu     sync.RWMutex
	cache  map[uuid.UUID]Profile
	swept  time.Time
	hc     *http.Client
	url    string
	secret string
}

func NewIdentity(svcURL, secret string) *Identity {
	return &Identity{
		cache:  map[uuid.UUID]Profile{},
		hc:     transport.NewClient(5 * time.Second),
		url:    strings.TrimRight(svcURL, "/"),
		secret: secret,
	}
}

func (c *Identity) Profile(ctx context.Context, id uuid.UUID) (Profile, error) {
	c.mu.RLock()
	p, ok := c.cache[id]
	c.mu.RUnlock()
	if ok && time.Since(p.fetchedAt) < profileTTL {
		return p, nil
	}
	p, err := c.fetch(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	c.store(id, p, time.Now())
	return p, nil
}

// store drops expired profiles at most once per TTL so the cache stays
// bounded by the users seen in one TTL window.
func (c *Identity) store(id uuid.UUID, p Profile, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.swept) >= profileTTL {
		for k, v := range c.cache {
			if now.Sub(v.fetchedAt) >= profileTTL {
				delete(c.cache, k)
			}
		}
		c.swept = now
	}
	c.cache[id] = p
}

type userResp struct {
	Email      string  `json:"email"`
	FirstName  string  `json:"first_name"`
	LastName   string  `json:"last_name"`
	Patronymic *string `json:"patronymic"`
}

func (c *Identity) fetch(ctx context.Context, id uuid.UUID) (Profile, error) {
	url := c.url + "/internal/users/" + id.String()
	body := []byte{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, bytes.NewReader(body))
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, body))
	resp, err := c.hc.Do(req)
	if err != nil {
		return Profile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return Profile{}, fmt.Errorf("identity svc: %d %s", resp.StatusCode, string(b))
	}
	var u userResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return Profile{}, err
	}
	return Profile{
		Email:     u.Email,
		Name:      strings.TrimSpace(u.LastName + " " + u.FirstName),
		fetchedAt: time.Now(),
	}, nil
}

func (c *Identity) SetRole(ctx context.Context, id uuid.UUID, role string, changedBy uuid.UUID) error {
	body, err := json.Marshal(map[string]any{"role": role, "changed_by": changedBy.String(), "preserve_admin": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.url+"/internal/users/"+id.String()+"/role", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, body))
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrUserMissing
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("identity role update: %d %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *Identity) ResearchIdentity(ctx context.Context, id uuid.UUID) (ResearchIdentity, error) {
	body := []byte{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/internal/users/"+id.String()+"/research-identity", bytes.NewReader(body))
	if err != nil {
		return ResearchIdentity{}, err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, body))
	resp, err := c.hc.Do(req)
	if err != nil {
		return ResearchIdentity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ResearchIdentity{}, fmt.Errorf("identity research identity: %d", resp.StatusCode)
	}
	var out ResearchIdentity
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}
