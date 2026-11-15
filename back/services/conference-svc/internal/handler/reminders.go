package handler

import (
	"github.com/confhub/shared/httputil"
	"github.com/google/uuid"
	"net/http"
	"time"
)

func (h *Internal) reminderCandidates(w http.ResponseWriter, r *http.Request) {
	cursor := uuid.Nil
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httputil.WriteError(w, r, httputil.ErrValidation)
			return
		}
		cursor = id
	}
	rows, err := h.repo.Pool().Query(r.Context(), `SELECT id,title,slug,status::text,topics,format::text,city,start_date,registration_deadline
        FROM conference_svc.conferences WHERE id>$1 AND deleted_at IS NULL AND publication_allowed AND status='registration_open'
        AND registration_deadline>now() AND registration_deadline<=now()+interval '24 hours' ORDER BY id LIMIT 500`, cursor)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	var last uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var title, slug, status, format string
		var city *string
		var topics []string
		var start, deadline time.Time
		if err := rows.Scan(&id, &title, &slug, &status, &topics, &format, &city, &start, &deadline); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		last = id
		items = append(items, map[string]any{"id": id, "title": title, "slug": slug, "status": status, "topics": topics, "format": format, "city": city, "start_date": start.Format("2006-01-02"), "registration_deadline": deadline})
	}
	if err := rows.Err(); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	next := ""
	if len(items) == 500 {
		next = last.String()
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}
