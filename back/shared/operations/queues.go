package operations

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/outbox"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auditFunc func(context.Context, pgx.Tx, uuid.UUID, string, uuid.UUID, string) error
type queue struct{ list, retry string }
type Handler struct {
	pool   *pgxpool.Pool
	schema string
	queues map[string]queue
	audit  auditFunc
}
type Item struct {
	ID        uuid.UUID `json:"id"`
	Queue     string    `json:"queue"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}

func New(pool *pgxpool.Pool, schema string, audit auditFunc) *Handler {
	h := &Handler{pool: pool, schema: schema, audit: audit, queues: map[string]queue{}}
	switch schema {
	case "identity_svc", "conference_svc", "application_svc":
		h.queues["outbox"] = queue{
			fmt.Sprintf(`SELECT id,status,retry_count,created_at FROM %s.outbox_events WHERE status='failed' ORDER BY created_at LIMIT 100`, schema),
			fmt.Sprintf(`UPDATE %s.outbox_events SET status='pending',retry_count=0,next_retry_at=now(),last_error=NULL WHERE id=$1 AND status='failed'`, schema)}
	case "engagement_svc":
		h.queues["notifications"] = queue{
			`SELECT id,status,attempts,created_at FROM engagement_svc.notifications WHERE status IN ('failed','dead') ORDER BY created_at LIMIT 100`,
			`UPDATE engagement_svc.notifications SET status='pending',attempts=0,next_attempt_at=now(),locked_at=NULL,last_error=NULL WHERE id=$1 AND status IN ('failed','dead')`}
	default:
		panic("unsupported operations schema")
	}
	if schema == "application_svc" {
		h.queues["storage_deletions"] = queue{
			`SELECT id,status,attempts,created_at FROM application_svc.storage_deletions WHERE status='dead' ORDER BY created_at LIMIT 100`,
			`UPDATE application_svc.storage_deletions SET status='pending',attempts=0,next_attempt_at=now(),last_error=NULL WHERE id=$1 AND status='dead'`}
	}
	if schema != "identity_svc" {
		h.queues["events"] = queue{
			fmt.Sprintf(`SELECT id,status,attempts,created_at FROM %s.event_failures WHERE status='dead' ORDER BY created_at LIMIT 100`, schema),
			fmt.Sprintf(`UPDATE %s.event_failures SET status='pending',attempts=0,next_attempt_at=now(),updated_at=now() WHERE id=$1 AND status='dead'`, schema)}
	}
	return h
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/operations/queues", h.list)
	r.Post("/operations/queues/{queue}/{id}/retry", h.retry)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items := make([]Item, 0)
	for name, q := range h.queues {
		rows, err := h.pool.Query(r.Context(), q.list)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			item := Item{Queue: name}
			if err := rows.Scan(&item.ID, &item.Status, &item.Attempts, &item.CreatedAt); err != nil {
				rows.Close()
				httputil.WriteError(w, r, err)
				return
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "queue")
	q, ok := h.queues[name]
	if !ok {
		httputil.WriteError(w, r, httputil.ErrNotFound)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, httputil.ErrValidation)
		return
	}
	var in struct {
		Actor uuid.UUID `json:"changed_by"`
	}
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if in.Actor == uuid.Nil {
		httputil.WriteError(w, r, httputil.ErrValidation)
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(r.Context()))
	tag, err := tx.Exec(r.Context(), q.retry, id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() != 1 {
		httputil.WriteError(w, r, httputil.ErrConflict)
		return
	}
	if h.audit != nil {
		err = h.audit(r.Context(), tx, in.Actor, "admin.queue_retried", id, name)
	} else {
		err = outbox.Insert(r.Context(), tx, h.schema, "user.admin_queue_retried", "queue", id.String(), map[string]any{"queue_id": id, "queue": name, "service": h.schema, "changed_by": in.Actor})
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
