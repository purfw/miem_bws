package subscriptions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/confhub/engagement-svc/internal/client"
	"github.com/confhub/shared/httputil"
)

type Criteria struct {
	Topics  []string `json:"topics,omitempty"`
	Formats []string `json:"formats,omitempty"`
	City    string   `json:"city,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
}

func (c *Criteria) normalize() error {
	c.City = strings.TrimSpace(c.City)
	if len(c.Topics) > 20 || len(c.Formats) > 3 || utf8.RuneCountInString(c.City) > 100 {
		return httputil.ErrValidation
	}
	for i, topic := range c.Topics {
		c.Topics[i] = strings.TrimSpace(topic)
		if c.Topics[i] == "" || utf8.RuneCountInString(c.Topics[i]) > 100 {
			return httputil.ErrValidation
		}
	}
	for _, format := range c.Formats {
		if format != "offline" && format != "hybrid" && format != "online" {
			return httputil.ErrValidation
		}
	}
	for _, date := range []string{c.From, c.To} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return httputil.ErrValidation
			}
		}
	}
	if c.From != "" && c.To != "" && c.From > c.To {
		return httputil.ErrValidation
	}
	slices.Sort(c.Topics)
	c.Topics = slices.Compact(c.Topics)
	slices.Sort(c.Formats)
	c.Formats = slices.Compact(c.Formats)
	if len(c.Topics) == 0 && len(c.Formats) == 0 && c.City == "" && c.From == "" && c.To == "" {
		return httputil.ErrValidation
	}
	return nil
}

func (c Criteria) Matches(conference *client.Conference) bool {
	if conference == nil {
		return false
	}
	if len(c.Formats) > 0 && !slices.Contains(c.Formats, conference.Format) {
		return false
	}
	if c.City != "" && !strings.EqualFold(c.City, conference.City) {
		return false
	}
	if c.From != "" && conference.StartDate < c.From || c.To != "" && conference.StartDate > c.To {
		return false
	}
	if len(c.Topics) > 0 && !slices.ContainsFunc(c.Topics, func(topic string) bool { return slices.Contains(conference.Topics, topic) }) {
		return false
	}
	return true
}

type criteriaRequest struct {
	Criteria Criteria `json:"criteria"`
	NotifyOn []string `json:"notify_on"`
	Channels []string `json:"channels"`
}

func (h *Handler) createCriteria(w http.ResponseWriter, r *http.Request) {
	c, userID, err := claims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in criteriaRequest
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := in.Criteria.normalize(); err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "criteria", Message: "non-empty valid topic, format, city or date filters required"}}})
		return
	}
	if _, err := validate(createRequest{ConferenceID: userID.String(), NotifyOn: in.NotifyOn, Channels: in.Channels}); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	raw, _ := json.Marshal(in.Criteria)
	digest := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(digest[:])
	token, hash, err := newToken()
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, userID.String()); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var limit bool
	err = tx.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM engagement_svc.subscriptions WHERE user_id=$1 AND active)>=100 AND NOT EXISTS(SELECT 1 FROM engagement_svc.subscriptions WHERE user_id=$1 AND criteria_hash=$2 AND active)`, userID, fingerprint).Scan(&limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if limit {
		httputil.WriteError(w, r, httputil.ErrConflict)
		return
	}
	var out item
	err = tx.QueryRow(r.Context(), `INSERT INTO engagement_svc.subscriptions (user_id,user_email,criteria,criteria_hash,notify_on,channels,unsubscribe_token_hash,unsubscribe_token)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT (user_id,criteria_hash) WHERE conference_id IS NULL DO UPDATE SET user_email=excluded.user_email,notify_on=excluded.notify_on,channels=excluded.channels,unsubscribe_token_hash=excluded.unsubscribe_token_hash,unsubscribe_token=excluded.unsubscribe_token,active=true,updated_at=now()
        RETURNING id,conference_id,notify_on,channels,created_at,criteria`, userID, strings.ToLower(c.Email), raw, fingerprint, in.NotifyOn, in.Channels, hash, token).
		Scan(&out.ID, &out.ConferenceID, &out.NotifyOn, &out.Channels, &out.CreatedAt, &out.Criteria)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{"data": out})
}
