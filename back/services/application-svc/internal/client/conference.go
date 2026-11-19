// Package client holds signed mTLS clients for the Identity and Conference
// internal APIs. Conference status is cached from NATS events with an HTTP
// fallback.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
)

type ConfStatus struct {
	Status               string
	RegistrationDeadline *time.Time
	Title                string
	FetchedAt            time.Time
}

const defaultTTL = 60 * time.Second

type ConferenceCache struct {
	mu     sync.RWMutex
	data   map[uuid.UUID]ConfStatus
	hc     *http.Client
	url    string
	secret string
	ttl    time.Duration
}

func NewConferenceCache(svcURL, secret string, ttl time.Duration) *ConferenceCache {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &ConferenceCache{
		data:   map[uuid.UUID]ConfStatus{},
		hc:     transport.NewClient(5 * time.Second),
		url:    svcURL,
		secret: secret,
		ttl:    ttl,
	}
}

func (c *ConferenceCache) StartSweeper(ctx context.Context) {
	go func() {
		t := time.NewTicker(c.ttl)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cutoff := time.Now().Add(-2 * c.ttl)
				c.mu.Lock()
				for k, v := range c.data {
					if v.FetchedAt.Before(cutoff) {
						delete(c.data, k)
					}
				}
				c.mu.Unlock()
			}
		}
	}()
}

func (c *ConferenceCache) Subscribe(nc *nats.Conn) error {
	_, err := nc.Subscribe(sharedevents.SubjectConferenceStatus, func(m *nats.Msg) {
		var env struct {
			EventID   uuid.UUID                            `json:"event_id"`
			EventType string                               `json:"event_type"`
			Payload   sharedevents.ConferenceStatusChanged `json:"payload"`
		}
		if err := json.Unmarshal(m.Data, &env); err != nil {
			slog.Warn("conference cache: bad event", "err", err)
			return
		}
		var dl *time.Time
		if env.Payload.RegistrationDeadline != nil {
			t, err := time.Parse(time.RFC3339, *env.Payload.RegistrationDeadline)
			if err == nil {
				dl = &t
			}
		}
		c.mu.Lock()
		c.data[env.Payload.ConferenceID] = ConfStatus{
			Status:               env.Payload.NewStatus,
			RegistrationDeadline: dl,
			Title:                env.Payload.Title,
			FetchedAt:            time.Now(),
		}
		c.mu.Unlock()
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", sharedevents.SubjectConferenceStatus, err)
	}
	_, err = nc.Subscribe(sharedevents.SubjectConferenceUpdated, func(m *nats.Msg) {
		var env struct {
			Payload sharedevents.ConferenceUpdated `json:"payload"`
		}
		if err := json.Unmarshal(m.Data, &env); err != nil {
			return
		}
		c.mu.Lock()
		if cur, ok := c.data[env.Payload.ConferenceID]; ok {
			cur.Title = env.Payload.Title
			c.data[env.Payload.ConferenceID] = cur
		}
		c.mu.Unlock()
	})
	return err
}

func (c *ConferenceCache) Get(ctx context.Context, id uuid.UUID) (ConfStatus, error) {
	c.mu.RLock()
	v, ok := c.data[id]
	c.mu.RUnlock()
	if ok && time.Since(v.FetchedAt) < c.ttl {
		return v, nil
	}
	v, err := c.fetch(ctx, id)
	if err != nil {
		return ConfStatus{}, err
	}
	c.mu.Lock()
	c.data[id] = v
	c.mu.Unlock()
	return v, nil
}

type confStatusResp struct {
	ID                   string  `json:"id"`
	Title                string  `json:"title"`
	Status               string  `json:"status"`
	RegistrationDeadline *string `json:"registration_deadline,omitempty"`
}

func (c *ConferenceCache) fetch(ctx context.Context, id uuid.UUID) (ConfStatus, error) {
	url := c.url + "/internal/conferences/" + id.String() + "/status"
	body := []byte{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, bytes.NewReader(body))
	if err != nil {
		return ConfStatus{}, err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, body))
	resp, err := c.hc.Do(req)
	if err != nil {
		return ConfStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ConfStatus{}, ErrConferenceNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return ConfStatus{}, fmt.Errorf("conference svc: %d %s", resp.StatusCode, string(b))
	}
	var r confStatusResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return ConfStatus{}, err
	}
	out := ConfStatus{Status: r.Status, Title: r.Title, FetchedAt: time.Now()}
	if r.RegistrationDeadline != nil {
		t, err := time.Parse(time.RFC3339, *r.RegistrationDeadline)
		if err == nil {
			out.RegistrationDeadline = &t
		}
	}
	return out, nil
}

var ErrConferenceNotFound = errors.New("conference not found")

func (c *ConferenceCache) CheckSection(ctx context.Context, conferenceID, sectionID uuid.UUID) (bool, error) {
	url := fmt.Sprintf("%s/internal/conferences/%s/sections/%s/check", strings.TrimRight(c.url, "/"), conferenceID, sectionID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, errors.Join(httputil.ErrUnavailable, err)
	}
	request.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, request, nil))
	response, err := c.hc.Do(request)
	if err != nil {
		return false, errors.Join(httputil.ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("section check HTTP %d: %w", response.StatusCode, httputil.ErrUnavailable)
	}
	var result struct {
		Belongs *bool `json:"belongs"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil {
		return false, errors.Join(httputil.ErrUnavailable, err)
	}
	if result.Belongs == nil {
		return false, fmt.Errorf("invalid section check response: %w", httputil.ErrUnavailable)
	}
	return *result.Belongs, nil
}
