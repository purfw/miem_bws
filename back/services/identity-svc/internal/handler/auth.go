package handler

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/identity-svc/internal/domain"
	"github.com/confhub/identity-svc/internal/service"
)

type Auth struct {
	svc           *service.Auth
	refreshCookie string
	cookiePath    string
	cookieSecure  bool
	refreshTTL    time.Duration
	jwks          jwtauth.JWKSet
	orcid         *service.ORCIDOAuth
	baseURL       string
}

func NewAuth(svc *service.Auth, refreshTTL time.Duration, cookieSecure bool, jwks jwtauth.JWKSet, orcid *service.ORCIDOAuth, baseURL string) *Auth {
	return &Auth{
		svc:           svc,
		refreshCookie: "refresh_token",
		cookiePath:    "/api/v1/auth",
		cookieSecure:  cookieSecure,
		refreshTTL:    refreshTTL,
		jwks:          jwks,
		orcid:         orcid,
		baseURL:       strings.TrimRight(baseURL, "/"),
	}
}

func (h *Auth) Routes(r chi.Router) {
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	r.Post("/forgot-password", h.forgotPassword)
	r.Post("/reset-password", h.resetPassword)
	r.Get("/verify", h.verifyEmail)
	r.Get("/jwks.json", h.jwkSet)
	r.Get("/.well-known/jwks.json", h.jwkSet)
	r.Get("/providers", h.providers)
	r.Get("/orcid/start", h.orcidStart)
	r.Get("/orcid/callback", h.orcidCallback)
}

func (h *Auth) AuthenticatedRoutes(r chi.Router) {
	r.Post("/resend-verification", h.resendVerification)
}

func (h *Auth) providers(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.WriteJSON(w, http.StatusOK, map[string]bool{"orcid": h.orcid != nil && h.orcid.Enabled()})
}

func (h *Auth) orcidStart(w http.ResponseWriter, r *http.Request) {
	if h.orcid == nil {
		httputil.WriteError(w, r, errors.Join(httputil.ErrUnavailable, errors.New("ORCID login is not configured")))
		return
	}
	target, err := h.orcid.Start(r.Context())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Auth) orcidCallback(w http.ResponseWriter, r *http.Request) {
	if h.orcid == nil || r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, h.baseURL+"/login?orcid_error=1", http.StatusFound)
		return
	}
	out, err := h.orcid.Callback(r.Context(), r.URL.Query().Get("code"), r.URL.Query().Get("state"), r.UserAgent())
	if err != nil {
		http.Redirect(w, r, h.baseURL+"/login?orcid_error=1", http.StatusFound)
		return
	}
	h.setRefreshCookie(w, out.RefreshToken)
	http.Redirect(w, r, h.baseURL+"/profile", http.StatusFound)
}

func (h *Auth) jwkSet(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	httputil.WriteJSON(w, http.StatusOK, h.jwks)
}

func (h *Auth) setRefreshCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.refreshCookie,
		Value:    tok,
		Path:     h.cookiePath,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.refreshTTL.Seconds()),
	})
}

func (h *Auth) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.refreshCookie,
		Value:    "",
		Path:     h.cookiePath,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

type registerResp struct {
	UserID string `json:"user_id"`
}

func (h *Auth) register(w http.ResponseWriter, r *http.Request) {
	var in service.RegisterInput
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	u, err := h.svc.Register(r.Context(), &in)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, registerResp{UserID: u.ID.String()})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResp struct {
	AccessToken string  `json:"access_token"`
	ExpiresAt   string  `json:"expires_at"`
	User        userDTO `json:"user"`
}

func (h *Auth) login(w http.ResponseWriter, r *http.Request) {
	var in loginReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	out, err := h.svc.Login(r.Context(), clientIP(r), in.Email, in.Password, r.UserAgent())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	h.setRefreshCookie(w, out.RefreshToken)
	httputil.WriteJSON(w, http.StatusOK, loginResp{
		AccessToken: out.AccessToken,
		ExpiresAt:   out.AccessExpiry.UTC().Format(time.RFC3339),
		User:        toUserDTO(out.User),
	})
}

func (h *Auth) refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(h.refreshCookie)
	if err != nil || c.Value == "" {
		httputil.WriteError(w, r, errUnauth("missing refresh"))
		return
	}
	out, err := h.svc.Refresh(r.Context(), c.Value, r.UserAgent())
	if err != nil {
		h.clearRefreshCookie(w)
		httputil.WriteError(w, r, err)
		return
	}
	h.setRefreshCookie(w, out.RefreshToken)
	httputil.WriteJSON(w, http.StatusOK, loginResp{
		AccessToken: out.AccessToken,
		ExpiresAt:   out.AccessExpiry.UTC().Format(time.RFC3339),
		User:        toUserDTO(out.User),
	})
}

func (h *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(h.refreshCookie); err == nil && c.Value != "" {
		_ = h.svc.Logout(r.Context(), c.Value)
	}
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type forgotReq struct {
	Email string `json:"email"`
}

func (h *Auth) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in forgotReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.svc.ForgotPassword(r.Context(), in.Email); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Auth) resendVerification(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.svc.ResendVerification(r.Context(), userID); err != nil {
		var limit *service.VerificationRateError
		if errors.As(err, &limit) {
			w.Header().Set("Retry-After", strconv.Itoa(limit.RetryAfterSeconds()))
		}
		httputil.WriteError(w, r, err)
		return
	}
	w.Header().Set("Retry-After", "60")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

type resetReq struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (h *Auth) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in resetReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	tok, err := uuid.Parse(in.Token)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "token", Message: "invalid"}}})
		return
	}
	if err := h.svc.ResetPassword(r.Context(), tok, in.NewPassword); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Auth) verifyEmail(w http.ResponseWriter, r *http.Request) {
	tok, err := uuid.Parse(r.URL.Query().Get("token"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "token", Message: "invalid"}}})
		return
	}
	if err := h.svc.Verify(r.Context(), tok); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(v) != nil {
		return v
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && net.ParseIP(host) != nil {
		return host
	}
	return r.RemoteAddr
}

func errUnauth(msg string) error {
	return wrap(msg, httputil.ErrUnauthorized)
}

func wrap(msg string, sentinel error) error {
	return errors.Join(sentinel, errors.New(msg))
}

type userDTO struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	Patronymic    *string `json:"patronymic,omitempty"`
	Affiliation   *string `json:"affiliation,omitempty"`
	Role          string  `json:"role"`
	EmailVerified bool    `json:"email_verified"`
	Locale        string  `json:"locale"`
}

func toUserDTO(u *domain.User) userDTO {
	return userDTO{
		ID:            u.ID.String(),
		Email:         u.Email,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		Patronymic:    u.Patronymic,
		Affiliation:   u.Affiliation,
		Role:          string(u.Role),
		EmailVerified: u.EmailVerified,
		Locale:        u.Locale,
	}
}
