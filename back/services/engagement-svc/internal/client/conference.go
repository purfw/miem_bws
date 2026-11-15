package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
	"github.com/google/uuid"
)

type Conference struct {
	ID        uuid.UUID  `json:"id"`
	Title     string     `json:"title"`
	Slug      string     `json:"slug"`
	Status    string     `json:"status"`
	Topics    []string   `json:"topics"`
	Format    string     `json:"format"`
	City      string     `json:"city"`
	StartDate string     `json:"start_date"`
	Deadline  *time.Time `json:"registration_deadline"`
}

type Conferences struct {
	url, secret string
	hc          *http.Client
}

func NewConferences(url, secret string) *Conferences {
	return &Conferences{url: strings.TrimRight(url, "/"), secret: secret, hc: transport.NewClient(5 * time.Second)}
}

func (c *Conferences) Due(ctx context.Context, cursor string) ([]Conference, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/internal/conferences/reminders?"+url.Values{"cursor": {cursor}}.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, nil))
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("reminders: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Items []Conference `json:"items"`
		Next  string       `json:"next_cursor"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result.Items, result.Next, err
}

func (c *Conferences) Get(ctx context.Context, id uuid.UUID) (*Conference, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/internal/conferences/"+id.String()+"/status", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, nil))
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("conference status: %d", resp.StatusCode)
	}
	var conference Conference
	if err := json.NewDecoder(resp.Body).Decode(&conference); err != nil {
		return nil, err
	}
	return &conference, nil
}
