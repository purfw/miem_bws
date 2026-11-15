package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
)

type ConfirmedRegistration struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	UserEmail    string    `json:"user_email"`
	UserName     string    `json:"user_name"`
	RegisteredAt time.Time `json:"registered_at"`
}

type RegistrationClient struct {
	url    string
	secret string
	hc     *http.Client
}

func NewRegistrationClient(conferenceURL, secret string) *RegistrationClient {
	return &RegistrationClient{url: strings.TrimRight(conferenceURL, "/"), secret: secret, hc: transport.NewClient(15 * time.Second)}
}

func (c *RegistrationClient) Confirmed(ctx context.Context, conferenceID uuid.UUID) ([]ConfirmedRegistration, error) {
	query := url.Values{"conference_id": {conferenceID.String()}, "status": {"confirmed"}}
	body := []byte{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/internal/registrations?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(c.secret, req, body))
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("conference registrations: %d %s", resp.StatusCode, payload)
	}
	var result struct {
		Data []ConfirmedRegistration `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}
