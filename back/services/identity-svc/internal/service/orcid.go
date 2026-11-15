package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/confhub/shared/httputil"
)

const orcidStateTTL = 10 * time.Minute

type ORCIDConfig struct {
	ClientID, ClientSecret       string
	AuthorizeURL, TokenURL       string
	UserInfoURL, APIURL, BaseURL string
	StateSecret                  string
}

type ORCIDOAuth struct {
	cfg  ORCIDConfig
	rdb  *redis.Client
	auth *Auth
	http *http.Client
}

func NewORCIDOAuth(cfg ORCIDConfig, rdb *redis.Client, auth *Auth) *ORCIDOAuth {
	return &ORCIDOAuth{cfg: cfg, rdb: rdb, auth: auth, http: &http.Client{Timeout: 10 * time.Second}}
}

func (o *ORCIDOAuth) Enabled() bool {
	return strings.TrimSpace(o.cfg.ClientID) != "" && strings.TrimSpace(o.cfg.ClientSecret) != ""
}

type oauthState struct {
	Verifier string `json:"verifier"`
}

func (o *ORCIDOAuth) Start(ctx context.Context) (string, error) {
	if !o.Enabled() {
		return "", errors.Join(httputil.ErrUnavailable, errors.New("ORCID login is not configured"))
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return "", err
	}
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])
	state, err := o.signedState()
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(oauthState{Verifier: verifier})
	if err := o.rdb.Set(ctx, orcidStateKey(state), raw, orcidStateTTL).Err(); err != nil {
		return "", errors.Join(httputil.ErrUnavailable, err)
	}
	params := url.Values{
		"client_id":             {o.cfg.ClientID},
		"response_type":         {"code"},
		"scope":                 {"openid /read-limited"},
		"redirect_uri":          {o.redirectURI()},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return o.cfg.AuthorizeURL + "?" + params.Encode(), nil
}

func (o *ORCIDOAuth) Callback(ctx context.Context, code, state, ua string) (*LoginOutput, error) {
	if !o.Enabled() {
		return nil, errors.Join(httputil.ErrUnavailable, errors.New("ORCID login is not configured"))
	}
	if !o.validState(state) || strings.TrimSpace(code) == "" {
		return nil, errors.Join(httputil.ErrUnauthorized, errors.New("invalid OAuth callback"))
	}
	raw, err := o.rdb.GetDel(ctx, orcidStateKey(state)).Bytes()
	if err != nil {
		return nil, errors.Join(httputil.ErrUnauthorized, errors.New("OAuth state expired or already used"))
	}
	var saved oauthState
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Verifier == "" {
		return nil, errors.Join(httputil.ErrUnauthorized, errors.New("invalid OAuth state"))
	}
	token, err := o.exchange(ctx, code, saved.Verifier)
	if err != nil {
		return nil, err
	}
	if token.ORCID == "" {
		return nil, errors.Join(httputil.ErrUnauthorized, errors.New("ORCID subject missing"))
	}
	firstName, lastName := splitDisplayName(token.Name)
	if info, err := o.userInfo(ctx, token.AccessToken); err == nil {
		if info.GivenName != "" {
			firstName = info.GivenName
		}
		if info.FamilyName != "" {
			lastName = info.FamilyName
		}
		if token.Name == "" {
			token.Name = info.Name
		}
	}
	publicationCount := o.publicationCount(ctx, token.ORCID, token.AccessToken)
	return o.auth.LoginExternal(ctx, "orcid", token.ORCID, token.Name, firstName, lastName, publicationCount, ua)
}

type orcidToken struct {
	AccessToken string `json:"access_token"`
	ORCID       string `json:"orcid"`
	Name        string `json:"name"`
}

func (o *ORCIDOAuth) exchange(ctx context.Context, code, verifier string) (orcidToken, error) {
	form := url.Values{
		"client_id":     {o.cfg.ClientID},
		"client_secret": {o.cfg.ClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {o.redirectURI()},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return orcidToken{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := o.http.Do(req)
	if err != nil {
		return orcidToken{}, errors.Join(httputil.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return orcidToken{}, errors.Join(httputil.ErrUnauthorized, fmt.Errorf("ORCID token exchange returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}
	var token orcidToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return orcidToken{}, err
	}
	return token, nil
}

type orcidUserInfo struct {
	Name       string `json:"name"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
}

func (o *ORCIDOAuth) userInfo(ctx context.Context, accessToken string) (orcidUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.cfg.UserInfoURL, nil)
	if err != nil {
		return orcidUserInfo{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := o.http.Do(req)
	if err != nil {
		return orcidUserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return orcidUserInfo{}, fmt.Errorf("ORCID userinfo returned %d", resp.StatusCode)
	}
	var out orcidUserInfo
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (o *ORCIDOAuth) publicationCount(ctx context.Context, orcid, accessToken string) int {
	endpoint := strings.TrimRight(o.cfg.APIURL, "/") + "/" + url.PathEscape(orcid) + "/works"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Accept", "application/vnd.orcid+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := o.http.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var works struct {
		Group []json.RawMessage `json:"group"`
	}
	if json.NewDecoder(resp.Body).Decode(&works) != nil {
		return 0
	}
	return len(works.Group)
}

func (o *ORCIDOAuth) redirectURI() string {
	return strings.TrimRight(o.cfg.BaseURL, "/") + "/api/v1/auth/orcid/callback"
}

func (o *ORCIDOAuth) signedState() (string, error) {
	random, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	payload := strconv.FormatInt(time.Now().Unix(), 10) + "." + random
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, []byte(o.cfg.StateSecret))
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (o *ORCIDOAuth) validState(state string) bool {
	parts := strings.Split(state, ".")
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(o.cfg.StateSecret))
	mac.Write([]byte(parts[0]))
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	issuedRaw, _, ok := strings.Cut(string(payload), ".")
	issued, err := strconv.ParseInt(issuedRaw, 10, 64)
	return ok && err == nil && time.Since(time.Unix(issued, 0)) >= 0 && time.Since(time.Unix(issued, 0)) <= orcidStateTTL
}

func orcidStateKey(state string) string {
	sum := sha256.Sum256([]byte(state))
	return "oauth:orcid:" + hex.EncodeToString(sum[:])
}

func randomURLToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func splitDisplayName(name string) (string, string) {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return "ORCID", "Researcher"
	case 1:
		return parts[0], "Researcher"
	default:
		return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
	}
}
