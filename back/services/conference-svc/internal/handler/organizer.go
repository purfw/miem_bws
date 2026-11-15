package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/conference-svc/internal/service"
)

type Organizer struct {
	svc           *service.Service
	repo          *repository.Conferences
	registrations *repository.Registrations
	links         *service.LinkCipher
}

func NewOrganizer(svc *service.Service, repo *repository.Conferences, registrations *repository.Registrations, links *service.LinkCipher) *Organizer {
	return &Organizer{svc: svc, repo: repo, registrations: registrations, links: links}
}

func (h *Organizer) ConferenceRoutes(r chi.Router) {
	r.Post("/", h.create)
	r.Post("/{id}/sessions", h.createSession)
	r.Patch("/{id}", h.update)
	r.Delete("/{id}", h.cancel)
}

func (h *Organizer) MyRoutes(r chi.Router) {
	r.Get("/conferences", h.mine)
	r.Get("/conferences/{id}/sessions", h.listManagedSessions)
}

func (h *Organizer) SessionRoutes(r chi.Router) {
	r.Put("/{id}", h.updateSession)
	r.Delete("/{id}", h.deleteSession)
	r.Put("/{id}/link", h.setSessionLink)
	r.Get("/{id}/join", h.joinSession)
}

func organizerClaims(r *http.Request) (*jwtauth.Claims, uuid.UUID, error) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		return nil, uuid.Nil, fmt.Errorf("authentication required: %w", httputil.ErrUnauthorized)
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("invalid subject: %w", httputil.ErrUnauthorized)
	}
	return c, id, nil
}

func canCreateConference(role jwtauth.Role) bool {
	return role == jwtauth.RoleVerifiedOrganizer || role == jwtauth.RoleAdmin
}

func (h *Organizer) requireManage(r *http.Request, conferenceID uuid.UUID, permissions ...string) (uuid.UUID, error) {
	c, userID, err := organizerClaims(r)
	if err != nil {
		return uuid.Nil, err
	}
	if c.Role == jwtauth.RoleAdmin || c.Role == jwtauth.RoleModerator {
		return userID, nil
	}
	if c.Role != jwtauth.RoleVerifiedOrganizer && c.Role != jwtauth.RoleOrganizerMember {
		return uuid.Nil, fmt.Errorf("organizer role required: %w", httputil.ErrForbidden)
	}
	permission := "edit"
	if len(permissions) > 0 {
		permission = permissions[0]
	}
	allowed, err := h.repo.CanManagePermission(r.Context(), conferenceID, userID, permission)
	if err != nil {
		return uuid.Nil, err
	}
	if !allowed {
		return uuid.Nil, fmt.Errorf("not a conference owner or member: %w", httputil.ErrForbidden)
	}
	return userID, nil
}

type organizerCreateReq struct {
	Title                string   `json:"title"`
	TitleEn              *string  `json:"title_en,omitempty"`
	Slug                 string   `json:"slug"`
	Description          *string  `json:"description,omitempty"`
	DescriptionEn        *string  `json:"description_en,omitempty"`
	Format               string   `json:"format"`
	StartDate            string   `json:"start_date"`
	EndDate              string   `json:"end_date"`
	RegistrationDeadline *string  `json:"registration_deadline,omitempty"`
	MaxParticipants      *int     `json:"max_participants,omitempty"`
	Location             *string  `json:"location,omitempty"`
	City                 *string  `json:"city,omitempty"`
	Topics               []string `json:"topics"`
}

func (h *Organizer) create(w http.ResponseWriter, r *http.Request) {
	c, userID, err := organizerClaims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if !canCreateConference(c.Role) {
		httputil.WriteError(w, r, fmt.Errorf("verified organizer role required: %w", httputil.ErrForbidden))
		return
	}
	var in organizerCreateReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	start, err := time.Parse("2006-01-02", in.StartDate)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "start_date", Message: "YYYY-MM-DD required"}}})
		return
	}
	end, err := time.Parse("2006-01-02", in.EndDate)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "end_date", Message: "YYYY-MM-DD required"}}})
		return
	}
	format, ok := domain.NormalizeFormat(in.Format)
	if !ok {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "format", Message: "offline, hybrid or online required"}}})
		return
	}
	var deadline *time.Time
	if in.RegistrationDeadline != nil && strings.TrimSpace(*in.RegistrationDeadline) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, *in.RegistrationDeadline)
		if parseErr != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "registration_deadline", Message: "RFC3339 required"}}})
			return
		}
		deadline = &parsed
	}
	conference, err := h.svc.Create(r.Context(), &service.CreateInput{
		Title: in.Title, TitleEn: in.TitleEn, Slug: in.Slug, Description: in.Description,
		DescriptionEn: in.DescriptionEn, Format: format, StartDate: start, EndDate: end,
		RegistrationDeadline: deadline, MaxParticipants: in.MaxParticipants, Location: in.Location,
		City: in.City, Topics: in.Topics, CreatedBy: userID, PublicationAllowed: true,
		Organizers: []domain.Organizer{{Name: c.Name, Email: &c.Email, Role: "owner", Kind: domain.OrganizerKindOrganizer, UserID: &userID}},
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{"id": conference.ID.String(), "slug": conference.Slug, "status": conference.Status})
}

func (h *Organizer) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if _, err := h.requireManage(r, id); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in updateReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	patch, err := organizerPatch(in)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	updated, err := h.svc.Update(r.Context(), id, patch)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"id": updated.ID.String(), "slug": updated.Slug, "status": updated.Status})
}

func organizerPatch(in updateReq) (repository.ConferencePatch, error) {
	patch := repository.ConferencePatch{Title: in.Title, TitleEn: in.TitleEn, Slug: in.Slug,
		Description: in.Description, DescriptionEn: in.DescriptionEn, MaxParticipants: in.MaxParticipants,
		Location: in.Location, City: in.City, CoverImageURL: in.CoverImageURL, Topics: nil}
	if in.Topics != nil {
		patch.Topics = &in.Topics
	}
	if in.Status != nil {
		status := domain.Status(*in.Status)
		patch.Status = &status
	}
	if in.Format != nil {
		format, ok := domain.NormalizeFormat(*in.Format)
		if !ok {
			return patch, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "format", Message: "invalid"}}}
		}
		patch.Format = &format
	}
	if in.StartDate != nil {
		value, err := time.Parse("2006-01-02", *in.StartDate)
		if err != nil {
			return patch, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "start_date", Message: "YYYY-MM-DD"}}}
		}
		patch.StartDate = &value
	}
	if in.EndDate != nil {
		value, err := time.Parse("2006-01-02", *in.EndDate)
		if err != nil {
			return patch, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "end_date", Message: "YYYY-MM-DD"}}}
		}
		patch.EndDate = &value
	}
	if in.RegistrationDeadline != nil {
		value, err := time.Parse(time.RFC3339, *in.RegistrationDeadline)
		if err != nil {
			return patch, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "registration_deadline", Message: "RFC3339"}}}
		}
		patch.RegistrationDeadline = &value
	}
	return patch, nil
}

func (h *Organizer) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if _, err := h.requireManage(r, id); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	status := domain.StatusCancelled
	if _, err := h.svc.Update(r.Context(), id, repository.ConferencePatch{Status: &status}); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Organizer) mine(w http.ResponseWriter, r *http.Request) {
	_, userID, err := organizerClaims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	items, err := h.repo.ListOwned(r.Context(), userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]cardDTO, 0, len(items))
	for i := range items {
		data = append(data, toCard(&items[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}
