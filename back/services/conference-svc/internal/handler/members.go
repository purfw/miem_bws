package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/conference-svc/internal/identity"
	"github.com/confhub/conference-svc/internal/repository"
)

// Users resolves accounts for the team editor.
type Users interface {
	ByEmail(ctx context.Context, email string) (identity.User, error)
	ByID(ctx context.Context, id string) (identity.User, error)
}

// Members lets the owner of a conference manage its team. Members get the
// organizer workspace for this conference with the granted permissions.
type Members struct {
	repo  *repository.Conferences
	users Users
}

func NewMembers(repo *repository.Conferences, users Users) *Members {
	return &Members{repo: repo, users: users}
}

func (h *Members) Routes(r chi.Router) {
	r.Get("/{id}/members", h.list)
	r.Put("/{id}/members", h.save)
	r.Delete("/{id}/members/{userId}", h.remove)
}

type memberDTO struct {
	UserID      string   `json:"user_id"`
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"created_at"`
}

type memberReq struct {
	Email       string   `json:"email"`
	Permissions []string `json:"permissions"`
}

// requireOwner allows the conference creator and administrators.
func (h *Members) requireOwner(r *http.Request) (uuid.UUID, uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}}
	}
	claims, userID, err := organizerClaims(r)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if claims.Role == jwtauth.RoleAdmin {
		return id, userID, nil
	}
	owner, err := h.repo.IsOwner(r.Context(), id, userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if !owner {
		return uuid.Nil, uuid.Nil, fmt.Errorf("only the conference owner manages its team: %w", httputil.ErrForbidden)
	}
	return id, userID, nil
}

func (h *Members) list(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.requireOwner(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	members, err := h.repo.Members(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]memberDTO, 0, len(members))
	for _, m := range members {
		dto := memberDTO{UserID: m.UserID.String(), Permissions: m.Permissions, CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339)}
		// Names are looked up live; a deleted or unreachable account still lists its id.
		if u, err := h.users.ByID(r.Context(), dto.UserID); err == nil {
			dto.Name, dto.Email = u.Name(), u.Email
		}
		data = append(data, dto)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Members) save(w http.ResponseWriter, r *http.Request) {
	id, actor, err := h.requireOwner(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in memberReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	email := strings.TrimSpace(in.Email)
	if email == "" || len(email) > 255 {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "email", Message: "required"}}})
		return
	}
	permissions := make([]string, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		if !slices.Contains(repository.MemberPermissions, p) {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "permissions", Message: "edit, schedule or participants"}}})
			return
		}
		if !slices.Contains(permissions, p) {
			permissions = append(permissions, p)
		}
	}
	if len(permissions) == 0 {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "permissions", Message: "at least one permission"}}})
		return
	}
	user, err := h.users.ByEmail(r.Context(), email)
	if errors.Is(err, identity.ErrNotFound) {
		httputil.WriteError(w, r, fmt.Errorf("no active account with this email: %w", httputil.ErrNotFound))
		return
	}
	if err != nil {
		httputil.WriteError(w, r, errors.Join(httputil.ErrUnavailable, err))
		return
	}
	// Only organizer accounts can open the workspace; the role is granted by
	// an administrator after verification.
	switch jwtauth.Role(user.Role) {
	case jwtauth.RoleOrganizerMember, jwtauth.RoleVerifiedOrganizer, jwtauth.RoleAdmin:
	default:
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "email", Message: "the account needs the organizer team role; ask an administrator to grant it"}}})
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil || userID == actor {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "email", Message: "cannot add yourself"}}})
		return
	}
	if err := h.repo.SaveMember(r.Context(), id, userID, actor, permissions); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, memberDTO{UserID: user.ID, Name: user.Name(), Email: user.Email, Permissions: permissions, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
}

func (h *Members) remove(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.requireOwner(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "userId", Message: "invalid uuid"}}})
		return
	}
	removed, err := h.repo.RemoveMember(r.Context(), id, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if !removed {
		httputil.WriteError(w, r, httputil.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
