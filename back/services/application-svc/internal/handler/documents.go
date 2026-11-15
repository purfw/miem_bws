package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/application-svc/internal/domain"
)

func (h *Applications) openDocumentRoutes(r chi.Router) {
	r.Get("/documents/verify/{code}", h.verifyParticipationDocument)
	r.Get("/documents/download", h.downloadParticipationDocumentByToken)
}

func (h *Applications) documentRoutes(r chi.Router) {
	r.Get("/documents/my", h.listParticipationDocuments)
	r.Get("/documents/{id}/download", h.downloadParticipationDocument)
}

func participationDocumentDTO(d *domain.ParticipationDocument) map[string]any {
	return map[string]any{
		"id": d.ID.String(), "conference_id": d.ConferenceID.String(),
		"conference_title": d.ConferenceTitle, "participant_name": d.ParticipantName,
		"issued_at": d.IssuedAt,
	}
}

func (h *Applications) listParticipationDocuments(w http.ResponseWriter, r *http.Request) {
	claims := jwtauth.ClaimsFromContext(r.Context())
	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	items, err := h.svc.ListParticipationDocuments(r.Context(), uid)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]map[string]any, 0, len(items))
	for i := range items {
		data = append(data, participationDocumentDTO(&items[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Applications) downloadParticipationDocument(w http.ResponseWriter, r *http.Request) {
	claims := jwtauth.ClaimsFromContext(r.Context())
	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	url, err := h.svc.ParticipationDocumentURL(r.Context(), id, uid)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (h *Applications) verifyParticipationDocument(w http.ResponseWriter, r *http.Request) {
	document, err := h.svc.VerifyParticipationDocument(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, participationDocumentDTO(document))
}

func (h *Applications) downloadParticipationDocumentByToken(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	url, err := h.svc.DownloadByTokenURL(r.Context(), token)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, url, http.StatusFound)
}
