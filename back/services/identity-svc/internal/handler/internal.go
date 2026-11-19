package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/identity-svc/internal/domain"
	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/identity-svc/internal/service"
)

type Internal struct {
	auth  *service.Auth
	users *repository.Users
}

func NewInternal(auth *service.Auth, users *repository.Users) *Internal {
	return &Internal{auth: auth, users: users}
}

func (h *Internal) Routes(r chi.Router) {
	r.Get("/users", h.listUsers)
	r.Get("/users/{id}", h.getUser)
	r.Get("/users/{id}/auth-state", h.getAuthState)
	r.Get("/users/{id}/research-identity", h.getResearchIdentity)
	r.Patch("/users/{id}/role", h.updateRole)
	r.Put("/users/{id}/active", h.setActive)
}

type adminUserDTO struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Role          string `json:"role"`
	Active        bool   `json:"is_active"`
	EmailVerified bool   `json:"email_verified"`
	CreatedAt     string `json:"created_at"`
}

type adminUsersResponse struct {
	Data    []adminUserDTO `json:"data"`
	HasMore bool           `json:"has_more"`
	Page    int            `json:"page"`
}

func (h *Internal) listUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if r.URL.Query().Get("page") == "" {
		page = 1
		err = nil
	}
	if err != nil || page < 1 || page > 100000 || utf8.RuneCountInString(query) > 255 {
		httputil.WriteError(w, r, httputil.ErrValidation)
		return
	}
	users, more, err := h.users.ListForAdministration(r.Context(), query, 50, (page-1)*50)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]adminUserDTO, 0, len(users))
	for _, u := range users {
		data = append(data, adminUserDTO{u.ID.String(), u.Email, u.FirstName, u.LastName, string(u.Role), u.IsActive, u.EmailVerified, u.CreatedAt.UTC().Format(time.RFC3339)})
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.WriteJSON(w, http.StatusOK, adminUsersResponse{data, more, page})
}

func (h *Internal) getAuthState(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, httputil.ErrUnauthorized)
		return
	}
	state, err := h.users.AuthState(r.Context(), id.String())
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, state)
}

func (h *Internal) getResearchIdentity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	identity, err := h.users.ResearchIdentityByUser(r.Context(), id)
	if err != nil {
		if err == repository.ErrNotFound {
			err = httputil.ErrNotFound
		}
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"provider": identity.Provider, "identifier": identity.Subject,
		"display_name": identity.DisplayName, "publication_count": identity.PublicationCount,
	})
}

type setActiveReq struct {
	Active    bool   `json:"active"`
	ChangedBy string `json:"changed_by"`
}

func (h *Internal) setActive(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in setActiveReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	changedBy, err := uuid.Parse(in.ChangedBy)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "changed_by", Message: "invalid uuid"}}})
		return
	}
	if err := h.auth.SetAccountActive(r.Context(), id, in.Active, changedBy); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Internal) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	u, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			err = httputil.ErrNotFound
		}
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toUserDTO(u))
}

type updateRoleReq struct {
	PreserveAdmin bool   `json:"preserve_admin"`
	Role          string `json:"role"`
	ChangedBy     string `json:"changed_by"`
}

func (h *Internal) updateRole(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in updateRoleReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	role := domain.Role(in.Role)
	if role != domain.RoleUser && role != domain.RoleVerifiedOrganizer && role != domain.RoleOrganizerMember && role != domain.RoleAdmin {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "role", Message: "must be user/verified_organizer/organizer_member/admin"}}})
		return
	}
	by, err := uuid.Parse(in.ChangedBy)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "changed_by", Message: "invalid uuid"}}})
		return
	}
	if err := h.auth.AssignRole(r.Context(), id, role, by, in.PreserveAdmin); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
