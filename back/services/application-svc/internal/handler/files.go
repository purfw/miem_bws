package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"
)

const maxMultipartMemory = 32 << 20

func (h *Applications) uploadFile(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "file", Message: "multipart parse failed"}}})
		return
	}
	file, fh, err := r.FormFile("file")
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "file", Message: "missing"}}})
		return
	}
	defer file.Close()

	out, err := h.svc.AttachFile(r.Context(), appID, uid, fh.Filename, fh.Size, file)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, toFileDTO(out))
}

func (h *Applications) listFiles(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	app, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, mapErr(err))
		return
	}
	if app.UserID != uid && !c.Role.AtLeast(jwtauth.RoleAdmin) {
		httputil.WriteError(w, r, errForbidden())
		return
	}
	files, err := h.svc.ListFiles(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	out := make([]fileDTO, 0, len(files))
	for i := range files {
		out = append(out, toFileDTO(&files[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

type Files struct {
	apps *Applications
}

func NewFiles(apps *Applications) *Files { return &Files{apps: apps} }

func (h *Files) Routes(r chi.Router) {
	r.Get("/{id}", h.download)
	r.Delete("/{id}", h.delete)
}

func (h *Files) download(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	url, _, err := h.apps.svc.PresignFile(r.Context(), id, uid, c.Role.AtLeast(jwtauth.RoleAdmin))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("redirect") == "false" {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"url": url})
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Files) delete(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if err := h.apps.svc.DeleteFile(r.Context(), id, uid); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
