package jwtauth

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/servicetoken"
	"github.com/confhub/shared/transport"
	"net/http"
	"strings"
	"time"
)

type AuthState struct {
	Version int64 `json:"version"`
	Active  bool  `json:"active"`
}

func (v *Verifier) WithStateResolver(resolve func(context.Context, string) (AuthState, error)) *Verifier {
	v.stateResolver = resolve
	return v
}

func RemoteStateResolver(baseURL, secret string) func(context.Context, string) (AuthState, error) {
	client := transport.NewClient(3 * time.Second)
	return func(ctx context.Context, userID string) (AuthState, error) {
		var state AuthState
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/internal/users/"+userID+"/auth-state", nil)
		if err != nil {
			return state, err
		}
		req.Header.Set(servicetoken.HeaderName, servicetoken.SignRequest(secret, req, nil))
		resp, err := client.Do(req)
		if err != nil {
			return state, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
			return state, httputil.ErrUnauthorized
		}
		if resp.StatusCode != http.StatusOK {
			return state, fmt.Errorf("auth state: HTTP %d", resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(&state)
		return state, err
	}
}
