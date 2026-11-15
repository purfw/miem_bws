package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/identity-svc/internal/service"
)

type Me struct {
	users *repository.Users
	auth  *service.Auth
}

func NewMe(users *repository.Users, auth *service.Auth) *Me {
	return &Me{users: users, auth: auth}
}

func (h *Me) Routes(r chi.Router) {
	r.Get("/me", h.get)
	r.Patch("/me", h.update)
	r.Put("/me", h.update)
	r.Put("/me/password", h.changePassword)
	r.Delete("/me", h.delete)
}

func userIDFromRequest(r *http.Request) (uuid.UUID, error) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		return uuid.Nil, errUnauth("no claims")
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return uuid.Nil, errUnauth("bad sub")
	}
	return id, nil
}

func (h *Me) get(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromRequest(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	u, err := h.users.GetByID(r.Context(), uid)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toUserDTO(u))
}

func (h *Me) update(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromRequest(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in service.ProfileInput
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	u, err := h.auth.UpdateProfile(r.Context(), uid, in)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toUserDTO(u))
}

type changePasswordReq struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Me) changePassword(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromRequest(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in changePasswordReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.auth.ChangePassword(r.Context(), uid, in.CurrentPassword, in.NewPassword); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deleteAccountReq struct {
	CurrentPassword string `json:"current_password"`
}

func (h *Me) delete(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromRequest(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in deleteAccountReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(in.CurrentPassword) == "" {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "current_password", Message: "required"}}})
		return
	}
	if err := h.auth.DeleteAccount(r.Context(), uid, in.CurrentPassword); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
