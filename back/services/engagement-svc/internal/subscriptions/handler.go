package subscriptions

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"
)

var allowedNotifications = map[string]bool{
	"deadline_approaching": true, "session_updated": true,
	"speaker_added": true, "conference_cancelled": true,
	"conference_published": true,
}

type Handler struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) AuthRoutes(r chi.Router) {
	r.Post("/", h.create)
	r.Post("/criteria", h.createCriteria)
	r.Get("/", h.list)
	r.Delete("/{id}", h.delete)
}

func (h *Handler) OpenRoutes(r chi.Router) {
	r.Get("/unsubscribe/{token}", h.unsubscribe)
}

type createRequest struct {
	ConferenceID string   `json:"conference_id"`
	NotifyOn     []string `json:"notify_on"`
	Channels     []string `json:"channels"`
}

type item struct {
	ID           uuid.UUID  `json:"id"`
	ConferenceID *uuid.UUID `json:"conference_id"`
	Criteria     Criteria   `json:"criteria"`
	NotifyOn     []string   `json:"notify_on"`
	Channels     []string   `json:"channels"`
	CreatedAt    time.Time  `json:"created_at"`
}

func claims(r *http.Request) (*jwtauth.Claims, uuid.UUID, error) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		return nil, uuid.Nil, httputil.ErrUnauthorized
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return nil, uuid.Nil, httputil.ErrUnauthorized
	}
	return c, id, nil
}

func validate(in createRequest) (uuid.UUID, error) {
	conferenceID, err := uuid.Parse(in.ConferenceID)
	if err != nil {
		return uuid.Nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "conference_id", Message: "invalid uuid"}}}
	}
	if len(in.NotifyOn) == 0 || len(in.NotifyOn) > len(allowedNotifications) {
		return uuid.Nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "notify_on", Message: "at least one criterion required"}}}
	}
	for _, value := range in.NotifyOn {
		if !allowedNotifications[value] {
			return uuid.Nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "notify_on", Message: "unsupported criterion"}}}
		}
	}
	if len(in.Channels) != 1 || in.Channels[0] != "email" {
		return uuid.Nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "channels", Message: "only email is currently supported"}}}
	}
	return conferenceID, nil
}

func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	c, userID, err := claims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in createRequest
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	conferenceID, err := validate(in)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	token, tokenHash, err := newToken()
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var out item
	err = h.pool.QueryRow(r.Context(), `INSERT INTO engagement_svc.subscriptions
		(user_id,user_email,conference_id,notify_on,channels,unsubscribe_token_hash,unsubscribe_token)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (user_id,conference_id) DO UPDATE SET
			user_email=excluded.user_email,notify_on=excluded.notify_on,channels=excluded.channels,
			unsubscribe_token_hash=excluded.unsubscribe_token_hash,unsubscribe_token=excluded.unsubscribe_token,active=true,updated_at=now()
		RETURNING id,conference_id,notify_on,channels,created_at`,
		userID, strings.ToLower(c.Email), conferenceID, in.NotifyOn, in.Channels, tokenHash, token).
		Scan(&out.ID, &out.ConferenceID, &out.NotifyOn, &out.Channels, &out.CreatedAt)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"id": out.ID, "conference_id": out.ConferenceID, "notify_on": out.NotifyOn,
		"channels": out.Channels, "created_at": out.CreatedAt, "unsubscribe_token": token,
	}})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	_, userID, err := claims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,conference_id,notify_on,channels,created_at,criteria
		FROM engagement_svc.subscriptions WHERE user_id=$1 AND active ORDER BY created_at DESC`, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	data := make([]item, 0)
	for rows.Next() {
		var out item
		if err := rows.Scan(&out.ID, &out.ConferenceID, &out.NotifyOn, &out.Channels, &out.CreatedAt, &out.Criteria); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		data = append(data, out)
	}
	if err := rows.Err(); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	_, userID, err := claims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE engagement_svc.subscriptions SET active=false,updated_at=now()
		WHERE id=$1 AND user_id=$2 AND active`, id, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httputil.WriteError(w, r, httputil.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if raw, err := base64.RawURLEncoding.DecodeString(token); err != nil || len(raw) != 32 {
		http.Redirect(w, r, "/unsubscribed?success=false", http.StatusFound)
		return
	}
	hash := sha256.Sum256([]byte(token))
	_, err := h.pool.Exec(r.Context(), `UPDATE engagement_svc.subscriptions
		SET active=false,updated_at=now() WHERE unsubscribe_token_hash=$1`, hash[:])
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Redirect(w, r, "/unsubscribed?success=false", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/unsubscribed?success=true", http.StatusFound)
}
