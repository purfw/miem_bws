package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/application-svc/internal/repository"
	"github.com/confhub/application-svc/internal/service"
)

type Applications struct {
	svc *service.Applications
}

func NewApplications(svc *service.Applications) *Applications { return &Applications{svc: svc} }

func (h *Applications) OpenRoutes(r chi.Router) {
	h.openDocumentRoutes(r)
	r.Get("/conferences/{confId}/participants", h.listParticipants)
}

func (h *Applications) PublicRoutes(r chi.Router) {
	h.documentRoutes(r)
	h.verificationRoutes(r)
	r.Post("/conferences/{confId}/applications", h.submit)
	r.Route("/applications", func(r chi.Router) {
		r.Get("/my", h.listMine)
		r.Get("/{id}", h.detail)
		r.Patch("/{id}", h.updateMeta)
		r.Post("/{id}/withdraw", h.withdraw)
		r.Post("/{id}/resubmit", h.resubmit)
		r.Post("/{id}/files", h.uploadFile)
		r.Get("/{id}/files", h.listFiles)
		r.Get("/{id}/reviews", h.listReviews)
		r.Post("/{id}/reviews", h.createReview)
	})
}

type submitReq struct {
	SectionID *string           `json:"section_id,omitempty"`
	Form      string            `json:"form"`
	Title     string            `json:"title"`
	Abstract  *string           `json:"abstract,omitempty"`
	CoAuthors []domain.CoAuthor `json:"co_authors,omitempty"`
	Keywords  []string          `json:"keywords,omitempty"`
}

func (h *Applications) submit(w http.ResponseWriter, r *http.Request) {
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
	confID, err := uuid.Parse(chi.URLParam(r, "confId"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "confId", Message: "invalid uuid"}}})
		return
	}
	var in submitReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var sec *uuid.UUID
	if in.SectionID != nil {
		s, err := uuid.Parse(*in.SectionID)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "section_id", Message: "invalid uuid"}}})
			return
		}
		sec = &s
	}
	email, name := profileFromContext(r)
	app, err := h.svc.Submit(r.Context(), &service.SubmitInput{
		ConferenceID: confID, SectionID: sec, UserID: uid,
		UserEmail: email, UserName: name,
		Form: domain.Form(in.Form), Title: in.Title, Abstract: in.Abstract,
		CoAuthors: in.CoAuthors, Keywords: in.Keywords,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, toApplicationDTO(app))
}

func (h *Applications) listMine(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, _ := uuid.Parse(c.UserID)
	apps, err := h.svc.List(r.Context(), repository.ListFilter{UserID: &uid, Limit: 100})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	out := make([]applicationDTO, 0, len(apps))
	for i := range apps {
		out = append(out, toApplicationDTO(&apps[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Applications) detail(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, _ := uuid.Parse(c.UserID)
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
	dto := toApplicationDTO(app)
	dto.Files = make([]fileDTO, 0, len(files))
	for i := range files {
		dto.Files = append(dto.Files, toFileDTO(&files[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, dto)
}

type updateMetaReq struct {
	Title     *string            `json:"title,omitempty"`
	Abstract  *string            `json:"abstract,omitempty"`
	CoAuthors *[]domain.CoAuthor `json:"co_authors,omitempty"`
	Keywords  *[]string          `json:"keywords,omitempty"`
	SectionID *string            `json:"section_id,omitempty"`
}

func (h *Applications) updateMeta(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, _ := uuid.Parse(c.UserID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in updateMetaReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	patch := repository.ApplicationPatch{
		Title: in.Title, Abstract: in.Abstract, CoAuthors: in.CoAuthors, Keywords: in.Keywords,
	}
	if in.SectionID != nil {
		s, err := uuid.Parse(*in.SectionID)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "section_id", Message: "invalid uuid"}}})
			return
		}
		patch.SectionID = &s
	}
	app, err := h.svc.UpdateMeta(r.Context(), id, uid, patch)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toApplicationDTO(app))
}

func (h *Applications) withdraw(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		httputil.WriteError(w, r, errUnauth())
		return
	}
	uid, _ := uuid.Parse(c.UserID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	app, err := h.svc.SetStatus(r.Context(), id, uid, domain.StatusWithdrawn)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toApplicationDTO(app))
}

func (h *Applications) resubmit(w http.ResponseWriter, r *http.Request) {
	claims := jwtauth.ClaimsFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if claims == nil || err != nil {
		httputil.WriteError(w, r, httputil.ErrUnauthorized)
		return
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		httputil.WriteError(w, r, httputil.ErrUnauthorized)
		return
	}
	app, err := h.svc.SetStatus(r.Context(), id, userID, domain.StatusSubmitted)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toApplicationDTO(app))
}

type participantDTO struct {
	Name      string  `json:"name"`
	Form      string  `json:"form"`
	SectionID *string `json:"section_id,omitempty"`
	Title     *string `json:"title,omitempty"`
}

type participantsResp struct {
	Data       []participantDTO `json:"data"`
	Pagination paginationDTO    `json:"pagination"`
}

type paginationDTO struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func (h *Applications) listParticipants(w http.ResponseWriter, r *http.Request) {
	confID, err := uuid.Parse(chi.URLParam(r, "confId"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{
			{Field: "confId", Message: "invalid uuid"}}})
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}

	list, total, err := h.svc.Participants(r.Context(), confID, perPage, (page-1)*perPage)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	resp := participantsResp{
		Data: make([]participantDTO, 0, len(list)),
		Pagination: paginationDTO{
			Page: page, PerPage: perPage, Total: total,
			TotalPages: totalPages(total, perPage),
		},
	}
	for _, p := range list {
		dto := participantDTO{Name: p.Name, Form: p.Form}
		if p.SectionID != nil {
			s := p.SectionID.String()
			dto.SectionID = &s
		}
		if p.Form != string(domain.FormListener) {
			t := p.Title
			dto.Title = &t
		}
		resp.Data = append(resp.Data, dto)
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

func totalPages(total, perPage int) int {
	if perPage <= 0 || total <= 0 {
		return 0
	}
	return (total + perPage - 1) / perPage
}

// Context profile fields are a fallback when Identity Svc is unavailable.
func profileFromContext(r *http.Request) (email, name string) {
	email = r.Header.Get("X-User-Email")
	name = r.Header.Get("X-User-Name")
	if email == "" {
		email = "unknown@example.com"
	}
	if name == "" {
		name = "User"
	}
	return
}

func errUnauth() error    { return errors.Join(httputil.ErrUnauthorized, errors.New("unauthorized")) }
func errForbidden() error { return errors.Join(httputil.ErrForbidden, errors.New("forbidden")) }

func mapErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return errors.Join(httputil.ErrNotFound, errors.New("not found"))
	case errors.Is(err, repository.ErrDuplicate):
		return errors.Join(httputil.ErrConflict, errors.New("duplicate"))
	default:
		return err
	}
}

type applicationDTO struct {
	ID               string            `json:"id"`
	ConferenceID     string            `json:"conference_id"`
	ConferenceTitle  string            `json:"conference_title"`
	SectionID        *string           `json:"section_id,omitempty"`
	UserID           string            `json:"user_id"`
	Form             string            `json:"form"`
	Title            string            `json:"title"`
	Abstract         *string           `json:"abstract,omitempty"`
	CoAuthors        []domain.CoAuthor `json:"co_authors"`
	Keywords         []string          `json:"keywords"`
	Status           string            `json:"status"`
	StatusChangedAt  string            `json:"status_changed_at"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	Files            []fileDTO         `json:"files,omitempty"`
	ModeratorComment *string           `json:"moderator_comment,omitempty"`
}

func toApplicationDTO(a *domain.Application) applicationDTO {
	dto := applicationDTO{
		ID: a.ID.String(), ConferenceID: a.ConferenceID.String(),
		ConferenceTitle:  a.CachedConfTitle,
		UserID:           a.UserID.String(),
		Form:             string(a.Form),
		Title:            a.Title,
		Abstract:         a.Abstract,
		ModeratorComment: a.ModeratorComment,
		CoAuthors:        a.CoAuthors,
		Keywords:         a.Keywords,
		Status:           string(a.Status),
		StatusChangedAt:  a.StatusChangedAt.UTC().Format("2006-01-02T15:04:05Z"),
		CreatedAt:        a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        a.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if a.SectionID != nil {
		s := a.SectionID.String()
		dto.SectionID = &s
	}
	return dto
}

type fileDTO struct {
	ID           string `json:"id"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	UploadedAt   string `json:"uploaded_at"`
}

func toFileDTO(f *domain.ApplicationFile) fileDTO {
	return fileDTO{
		ID:           f.ID.String(),
		OriginalName: f.OriginalName,
		MimeType:     f.MimeType,
		SizeBytes:    f.SizeBytes,
		UploadedAt:   f.UploadedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
