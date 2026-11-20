package support

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/engagement-svc/internal/mailer"
)

const maxBodyBytes = 64 << 10

const perIPHourly = 5

var topics = map[string]string{
	"account":     "Учётная запись",
	"application": "Заявка на участие",
	"conference":  "Организация конференции",
	"bug":         "Ошибка на сайте",
	"other":       "Другое",
}

type Handler struct {
	pool         *pgxpool.Pool
	mailer       *mailer.Mailer
	supportEmail string
}

func New(pool *pgxpool.Pool, m *mailer.Mailer, supportEmail string) *Handler {
	return &Handler{pool: pool, mailer: m, supportEmail: supportEmail}
}

type createReq struct {
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Topic        string  `json:"topic"`
	Subject      *string `json:"subject,omitempty"`
	Message      string  `json:"message"`
	ConferenceID *string `json:"conference_id,omitempty"`
}

type createResp struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var in createReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	fields := validate(&in)
	var confID *uuid.UUID
	if in.ConferenceID != nil && *in.ConferenceID != "" {
		id, err := uuid.Parse(*in.ConferenceID)
		if err != nil {
			fields = append(fields, httputil.FieldError{Field: "conference_id", Message: "invalid uuid"})
		} else {
			confID = &id
		}
	}
	if len(fields) > 0 {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: fields})
		return
	}

	ip := clientIP(r)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if ip != "" {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "support:"+ip); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	over, err := h.overLimit(ctx, tx, ip)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if over {
		httputil.WriteError(w, r, errors.Join(httputil.ErrTooMany,
			errors.New("too many support requests from this address")))
		return
	}

	id := uuid.New()
	subject := ""
	if in.Subject != nil {
		subject = strings.TrimSpace(*in.Subject)
	}
	if subject == "" {
		subject = topics[in.Topic]
	}

	var ipArg *string
	if ip != "" {
		ipArg = &ip
	}
	if _, err := tx.Exec(ctx, `INSERT INTO engagement_svc.support_messages
		(id, name, email, topic, subject, message, conference_id, ip_address)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), in.Topic,
		subject, strings.TrimSpace(in.Message), confID, ipArg); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	if err := h.notify(mailer.WithTransaction(ctx, tx, id), id, &in, subject); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusAccepted, createResp{ID: id.String(), Status: "accepted"})
}

func validate(in *createReq) []httputil.FieldError {
	var fields []httputil.FieldError

	if n := len([]rune(strings.TrimSpace(in.Name))); n < 2 || n > 255 {
		fields = append(fields, httputil.FieldError{Field: "name", Message: "2..255 characters"})
	}
	address, err := mail.ParseAddress(strings.TrimSpace(in.Email))
	if err != nil || address.Address != strings.TrimSpace(in.Email) || len(in.Email) > 255 {
		fields = append(fields, httputil.FieldError{Field: "email", Message: "valid email required"})
	}
	if in.Topic == "" {
		in.Topic = "other"
	}
	if _, ok := topics[in.Topic]; !ok {
		fields = append(fields, httputil.FieldError{Field: "topic", Message: "unknown topic"})
	}
	if n := len([]rune(strings.TrimSpace(in.Message))); n < 10 || n > 5000 {
		fields = append(fields, httputil.FieldError{Field: "message", Message: "10..5000 characters"})
	}
	if in.Subject != nil && len([]rune(*in.Subject)) > 500 {
		fields = append(fields, httputil.FieldError{Field: "subject", Message: "max 500 characters"})
	}
	return fields
}

func (h *Handler) overLimit(ctx context.Context, tx pgx.Tx, ip string) (bool, error) {
	if ip == "" {
		return false, nil
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.support_messages
		WHERE ip_address = $1 AND created_at > now() - interval '1 hour'`, ip).Scan(&n); err != nil {
		return false, err
	}
	return n >= perIPHourly, nil
}

func (h *Handler) notify(ctx context.Context, id uuid.UUID, in *createReq, subject string) error {
	if h.mailer == nil || h.supportEmail == "" {
		return nil
	}

	data := map[string]any{
		"ID":      id.String(),
		"Name":    strings.TrimSpace(in.Name),
		"Email":   strings.TrimSpace(in.Email),
		"Topic":   topics[in.Topic],
		"Subject": subject,
		"Message": strings.TrimSpace(in.Message),
	}
	return h.mailer.Send(ctx, h.supportEmail, "Обращение в поддержку: "+subject,
		"support_message.html", data, nil, "support_message")
}

func clientIP(r *http.Request) string {
	candidates := []string{r.Header.Get("X-Real-IP")}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		first, _, _ := strings.Cut(v, ",")
		candidates = append(candidates, first)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		candidates = append(candidates, host)
	}
	for _, c := range candidates {
		if ip := net.ParseIP(strings.TrimSpace(c)); ip != nil {
			return ip.String()
		}
	}
	return ""
}
