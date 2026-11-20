package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/application-svc/internal/repository"
	"github.com/confhub/application-svc/internal/service"
)

type Internal struct {
	svc *service.Applications
}

func NewInternal(svc *service.Applications) *Internal { return &Internal{svc: svc} }

func (h *Internal) Routes(r chi.Router) {
	r.Patch("/applications/{id}/moderate", h.moderate)
	r.Get("/applications", h.list)
	r.Get("/applications/{id}/reviews", h.listReviews)
	r.Post("/applications/{id}/reviews", h.createReview)
	r.Get("/verifications", h.verificationQueue)
	r.Patch("/verifications/{id}", h.moderateVerification)
	r.Get("/verification-documents/{id}", h.internalVerificationDocument)
}

type moderateReq struct {
	Status    string  `json:"status"`
	Comment   *string `json:"comment,omitempty"`
	ChangedBy string  `json:"changed_by"`
}

func (h *Internal) moderate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in moderateReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	by, err := uuid.Parse(in.ChangedBy)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "changed_by", Message: "invalid uuid"}}})
		return
	}
	app, err := h.svc.Moderate(r.Context(), id, domain.Status(in.Status), in.Comment, by)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toApplicationDTO(app))
}

func (h *Internal) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, limit, err := queuePage(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	f := repository.ListFilter{Limit: limit + 1, Offset: (page - 1) * limit}
	if v := q.Get("conference_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.ConferenceID = &id
		}
	}
	if v := q.Get("user_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.UserID = &id
		}
	}
	if v := q.Get("status"); v != "" {
		f.Statuses = strings.Split(v, ",")
	}
	apps, err := h.svc.List(r.Context(), f)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	hasMore := len(apps) > limit
	if hasMore {
		apps = apps[:limit]
	}
	out := make([]applicationDTO, 0, len(apps))
	for i := range apps {
		out = append(out, toApplicationDTO(&apps[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": out, "page": page, "has_more": hasMore})
}

func queuePage(r *http.Request) (int, int, error) {
	page, limit := 1, 100
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
