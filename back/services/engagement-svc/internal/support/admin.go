package support

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/engagement-svc/internal/audit"
)

// Statuses mirror the chk_support_status constraint on support_messages.
var statuses = map[string]bool{"new": true, "in_progress": true, "closed": true}

// AdminRoutes are mounted under /internal and protected by the service-token
// verifier; they back the support inbox of the administrator console.
func (h *Handler) AdminRoutes(r chi.Router) {
	r.Get("/support/messages", h.AdminList)
	r.Patch("/support/messages/{id}", h.AdminSetStatus)
}

type messageDTO struct {
	ID           string  `json:"id"`
	UserID       *string `json:"user_id,omitempty"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Topic        string  `json:"topic"`
	TopicLabel   string  `json:"topic_label"`
	Subject      string  `json:"subject"`
	Message      string  `json:"message"`
	ConferenceID *string `json:"conference_id,omitempty"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
}

type listResp struct {
	Data    []messageDTO `json:"data"`
	Page    int          `json:"page"`
	HasMore bool         `json:"has_more"`
}

type statusReq struct {
	Status    string `json:"status"`
	ChangedBy string `json:"changed_by"`
}

const messageColumns = `id, user_id, name, email, topic, COALESCE(subject, ''), message,
	conference_id, status, created_at`

func scanMessage(row pgx.Row) (messageDTO, error) {
	var (
		m            messageDTO
		id           uuid.UUID
		userID, conf *uuid.UUID
		created      time.Time
	)
	if err := row.Scan(&id, &userID, &m.Name, &m.Email, &m.Topic, &m.Subject, &m.Message,
		&conf, &m.Status, &created); err != nil {
		return m, err
	}
	m.ID = id.String()
	if userID != nil {
		s := userID.String()
		m.UserID = &s
	}
	if conf != nil {
		s := conf.String()
		m.ConferenceID = &s
	}
	m.TopicLabel = topics[m.Topic]
	if m.TopicLabel == "" {
		m.TopicLabel = m.Topic
	}
	m.CreatedAt = created.UTC().Format(time.RFC3339)
	return m, nil
}

func adminPage(r *http.Request) (int, int, error) {
	page, limit := 1, 50
	for _, field := range []struct {
		name  string
		value *int
		max   int
	}{
		{"page", &page, 100000}, {"per_page", &limit, 200},
	} {
		if raw := r.URL.Query().Get(field.name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > field.max {
				return 0, 0, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: field.name, Message: "invalid pagination value"}}}
			}
			*field.value = value
		}
	}
	return page, limit, nil
}

// AdminList returns support messages, newest first, optionally by status.
func (h *Handler) AdminList(w http.ResponseWriter, r *http.Request) {
	page, limit, err := adminPage(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !statuses[status] {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "status", Message: "new, in_progress or closed"}}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := h.pool.Query(ctx, `SELECT `+messageColumns+`
		FROM engagement_svc.support_messages
		WHERE $1::text = '' OR status = $1::text
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		status, limit+1, (page-1)*limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := listResp{Data: make([]messageDTO, 0, limit), Page: page}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		out.Data = append(out.Data, m)
	}
	if err := rows.Err(); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if len(out.Data) > limit {
		out.Data, out.HasMore = out.Data[:limit], true
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// AdminSetStatus moves a support message between new, in_progress and closed
// and records the administrator's decision in the audit log atomically.
func (h *Handler) AdminSetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in statusReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var fields []httputil.FieldError
	if !statuses[in.Status] {
		fields = append(fields, httputil.FieldError{Field: "status", Message: "new, in_progress or closed"})
	}
	actor, err := uuid.Parse(in.ChangedBy)
	if err != nil || actor == uuid.Nil {
		fields = append(fields, httputil.FieldError{Field: "changed_by", Message: "invalid uuid"})
	}
	if len(fields) > 0 {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: fields})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var previous string
	if err := tx.QueryRow(ctx, `SELECT status FROM engagement_svc.support_messages
		WHERE id = $1 FOR UPDATE`, id).Scan(&previous); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("support message not found: %w", httputil.ErrNotFound)
		}
		httputil.WriteError(w, r, err)
		return
	}
	m, err := scanMessage(tx.QueryRow(ctx, `UPDATE engagement_svc.support_messages
		SET status = $2 WHERE id = $1 RETURNING `+messageColumns, id, in.Status))
	if err == nil && previous != in.Status {
		err = audit.Insert(ctx, tx, audit.Entry{
			EventID: uuid.New(), EventType: "support.status_changed", ActorID: &actor,
			EntityType: "support_message", EntityID: &id,
			Payload: map[string]string{"old_status": previous, "new_status": in.Status},
		})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, m)
}
