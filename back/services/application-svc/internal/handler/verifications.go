package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/application-svc/internal/domain"
)

type verificationCreateReq struct {
	Organization string `json:"organization"`
}

func (h *Applications) verificationRoutes(r chi.Router) {
	r.Route("/verifications", func(r chi.Router) {
		r.Post("/", h.createVerification)
		r.Get("/my", h.myVerifications)
		r.Post("/{id}/documents", h.uploadVerificationDocument)
	})
	r.Get("/verification-documents/{id}", h.downloadVerificationDocument)
}

func (h *Applications) createVerification(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	var in verificationCreateReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	v, err := h.svc.CreateVerification(r.Context(), uid, c.Email, c.Name, in.Organization, c.EmailVerified)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, verificationDTO(v))
}

func (h *Applications) myVerifications(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	uid, err := uuid.Parse(c.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	items, err := h.svc.ListMyVerifications(r.Context(), uid)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]map[string]any, 0, len(items))
	for i := range items {
		data = append(data, verificationDTO(&items[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Applications) uploadVerificationDocument(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
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
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "document", Message: "multipart parse failed"}}})
		return
	}
	file, header, err := r.FormFile("document")
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "document", Message: "required"}}})
		return
	}
	defer file.Close()
	doc, err := h.svc.AttachVerificationDocument(r.Context(), id, uid, header.Filename, header.Size, file)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, verificationDocumentDTO(doc))
}

func (h *Applications) downloadVerificationDocument(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
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
	admin := c.Role == jwtauth.RoleAdmin
	url, err := h.svc.VerificationDocumentURL(r.Context(), id, uid, admin)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if r.URL.Query().Get("redirect") == "false" {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"url": url})
		return
	}
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(60))
	http.Redirect(w, r, url, http.StatusFound)
}

func verificationDTO(v *domain.OrganizerVerification) map[string]any {
	docs := make([]map[string]any, 0, len(v.Documents))
	for i := range v.Documents {
		docs = append(docs, verificationDocumentDTO(&v.Documents[i]))
	}
	return map[string]any{
		"id": v.ID.String(), "organization": v.Organization, "method": v.Method,
		"status": string(v.Status), "rejection_reason": v.RejectionReason,
		"role_applied": v.RoleApplied,
		"created_at":   v.CreatedAt, "reviewed_at": v.ReviewedAt, "documents": docs,
	}
}

func verificationDocumentDTO(d *domain.VerificationDocument) map[string]any {
	return map[string]any{
		"id": d.ID.String(), "name": d.OriginalName, "mime_type": d.MimeType,
		"size_bytes": d.SizeBytes, "uploaded_at": d.UploadedAt,
	}
}

type verificationModerateReq struct {
	Status    string  `json:"status"`
	Reason    *string `json:"reason,omitempty"`
	ChangedBy string  `json:"changed_by"`
}

func (h *Internal) verificationQueue(w http.ResponseWriter, r *http.Request) {
	page, limit, err := queuePage(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	items, err := h.svc.ListVerificationQueue(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), limit+1, (page-1)*limit)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]map[string]any, 0, len(items))
	for i := range items {
		entry := verificationDTO(&items[i])
		entry["user_id"] = items[i].UserID.String()
		entry["user_email"] = items[i].UserEmail
		entry["user_name"] = items[i].UserName
		data = append(data, entry)
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data, "page": page, "has_more": hasMore})
}

func (h *Internal) moderateVerification(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in verificationModerateReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	by, err := uuid.Parse(in.ChangedBy)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "changed_by", Message: "invalid uuid"}}})
		return
	}
	v, err := h.svc.ModerateVerification(r.Context(), id, by, domain.VerificationStatus(in.Status), in.Reason)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, verificationDTO(v))
}

func (h *Internal) internalVerificationDocument(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	url, err := h.svc.VerificationDocumentURL(r.Context(), id, uuid.Nil, true)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"url": url})
}
