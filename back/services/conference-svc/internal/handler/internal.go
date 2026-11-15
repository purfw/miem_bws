package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/pagination"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/conference-svc/internal/service"
)

const exportMaxPageSize = 500

type Internal struct {
	svc           *service.Service
	repo          *repository.Conferences
	registrations *repository.Registrations
}

func NewInternal(svc *service.Service, repo *repository.Conferences, registrations *repository.Registrations) *Internal {
	return &Internal{svc: svc, repo: repo, registrations: registrations}
}

func (h *Internal) Routes(r chi.Router) {
	r.Post("/conferences", h.create)
	r.Get("/conferences/export", h.export)
	r.Get("/conferences/reminders", h.reminderCandidates)
	r.Patch("/conferences/{id}", h.update)
	r.Delete("/conferences/{id}", h.softDelete)
	r.Get("/conferences/{id}/status", h.statusFallback)
	r.Get("/conferences/{id}/sections/{sectionId}/check", h.checkSection)
	r.Post("/conferences/{id}/schedule", h.replaceSchedule)
	r.Get("/registrations", h.listRegistrations)
	r.Get("/registrations/check", h.checkRegistration)
}

func (h *Internal) checkSection(w http.ResponseWriter, r *http.Request) {
	conferenceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "conference_id", Message: "invalid uuid"}}})
		return
	}
	sectionID, err := uuid.Parse(chi.URLParam(r, "sectionId"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "section_id", Message: "invalid uuid"}}})
		return
	}
	belongs, err := h.repo.SectionBelongsToConference(r.Context(), conferenceID, sectionID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]bool{"belongs": belongs})
}

type registrationCheckDTO struct {
	Confirmed bool    `json:"has_confirmed_registration"`
	ID        *string `json:"registration_id,omitempty"`
}

func (h *Internal) checkRegistration(w http.ResponseWriter, r *http.Request) {
	conferenceID, err := uuid.Parse(r.URL.Query().Get("conference_id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "conference_id", Message: "invalid uuid"}}})
		return
	}
	userID, err := uuid.Parse(r.URL.Query().Get("user_id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "user_id", Message: "invalid uuid"}}})
		return
	}
	id, confirmed, err := h.registrations.HasConfirmed(r.Context(), conferenceID, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := registrationCheckDTO{Confirmed: confirmed}
	if confirmed {
		registrationID := id.String()
		data.ID = &registrationID
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Internal) listRegistrations(w http.ResponseWriter, r *http.Request) {
	conferenceID, err := uuid.Parse(r.URL.Query().Get("conference_id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "conference_id", Message: "invalid uuid"}}})
		return
	}
	status := r.URL.Query().Get("status")
	items, err := h.registrations.ListByConference(r.Context(), conferenceID, status)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]map[string]any, 0, len(items))
	for _, item := range items {
		data = append(data, map[string]any{
			"id": item.ID.String(), "user_id": item.UserID.String(), "user_email": item.UserEmail,
			"user_name": item.UserName, "status": string(item.Status), "role_at_conf": item.RoleAtConference,
			"registered_at": item.RegisteredAt.UTC().Format(time.RFC3339),
		})
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

type createReq struct {
	Title                string           `json:"title"`
	TitleEn              *string          `json:"title_en,omitempty"`
	Slug                 string           `json:"slug"`
	Description          *string          `json:"description,omitempty"`
	DescriptionEn        *string          `json:"description_en,omitempty"`
	Format               string           `json:"format"`
	StartDate            string           `json:"start_date"` // YYYY-MM-DD
	EndDate              string           `json:"end_date"`
	RegistrationDeadline *string          `json:"registration_deadline,omitempty"` // RFC3339
	MaxParticipants      *int             `json:"max_participants,omitempty"`
	Location             *string          `json:"location,omitempty"`
	City                 *string          `json:"city,omitempty"`
	CoverImageURL        *string          `json:"cover_image_url,omitempty"`
	Topics               []string         `json:"topics"`
	CreatedBy            string           `json:"created_by"` // uuid string
	Sections             []sectionInput   `json:"sections"`
	Organizers           []organizerInput `json:"organizers"`
	Partners             []partnerInput   `json:"partners"`
}

type sectionInput struct {
	Title                string  `json:"title"`
	TitleEn              *string `json:"title_en,omitempty"`
	Description          *string `json:"description,omitempty"`
	SortOrder            int     `json:"sort_order"`
	ModeratorName        *string `json:"moderator_name,omitempty"`
	ModeratorAffiliation *string `json:"moderator_affiliation,omitempty"`
	ModeratorUserID      *string `json:"moderator_user_id,omitempty"`
}

type organizerInput struct {
	Name        string  `json:"name"`
	Email       *string `json:"email,omitempty"`
	Affiliation *string `json:"affiliation,omitempty"`
	Role        string  `json:"role"`
	Kind        string  `json:"kind,omitempty"`
	JobTitle    *string `json:"job_title,omitempty"`
	PhotoURL    *string `json:"photo_url,omitempty"`
	SortOrder   int     `json:"sort_order"`
	UserID      *string `json:"user_id,omitempty"`
}

type partnerInput struct {
	Name       string  `json:"name"`
	LogoURL    *string `json:"logo_url,omitempty"`
	WebsiteURL *string `json:"website_url,omitempty"`
	Tier       *string `json:"tier,omitempty"`
	SortOrder  int     `json:"sort_order"`
}

func (h *Internal) create(w http.ResponseWriter, r *http.Request) {
	var in createReq
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
	createdBy, err := uuid.Parse(in.CreatedBy)
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "created_by", Message: "invalid uuid"}}})
		return
	}
	var deadline *time.Time
	if in.RegistrationDeadline != nil {
		t, err := time.Parse(time.RFC3339, *in.RegistrationDeadline)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "registration_deadline", Message: "RFC3339 required"}}})
			return
		}
		deadline = &t
	}

	format := domain.FormatOffline
	if in.Format != "" {
		f, ok := domain.NormalizeFormat(in.Format)
		if !ok {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "format", Message: "offline|hybrid|online"}}})
			return
		}
		format = f
	}

	createIn := &service.CreateInput{
		Title: in.Title, TitleEn: in.TitleEn, Slug: in.Slug,
		Description: in.Description, DescriptionEn: in.DescriptionEn,
		Format:    format,
		StartDate: start, EndDate: end, RegistrationDeadline: deadline,
		MaxParticipants: in.MaxParticipants, Location: in.Location,
		City: in.City, CoverImageURL: in.CoverImageURL,
		Topics: in.Topics, CreatedBy: createdBy,
		PublicationAllowed: true,
	}
	for _, s := range in.Sections {
		sec := domain.Section{
			Title: s.Title, TitleEn: s.TitleEn, Description: s.Description, SortOrder: s.SortOrder,
			ModeratorName: s.ModeratorName, ModeratorAffiliation: s.ModeratorAffiliation,
		}
		if s.ModeratorUserID != nil {
			u, err := uuid.Parse(*s.ModeratorUserID)
			if err != nil {
				httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{
					{Field: "sections.moderator_user_id", Message: "invalid uuid"}}})
				return
			}
			sec.ModeratorUserID = &u
		}
		createIn.Sections = append(createIn.Sections, sec)
	}
	for _, o := range in.Organizers {
		var uid *uuid.UUID
		if o.UserID != nil {
			if u, err := uuid.Parse(*o.UserID); err == nil {
				uid = &u
			}
		}
		role := o.Role
		if role == "" {
			role = "organizer"
		}
		kind, ok := domain.NormalizeOrganizerKind(o.Kind)
		if !ok {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "organizers.kind", Message: "organizer|committee"}}})
			return
		}
		createIn.Organizers = append(createIn.Organizers, domain.Organizer{
			Name: o.Name, Email: o.Email, Affiliation: o.Affiliation, Role: role,
			Kind: kind, JobTitle: o.JobTitle, PhotoURL: o.PhotoURL, SortOrder: o.SortOrder,
			UserID: uid,
		})
	}
	for _, p := range in.Partners {
		createIn.Partners = append(createIn.Partners, domain.Partner{
			Name: p.Name, LogoURL: p.LogoURL, WebsiteURL: p.WebsiteURL, Tier: p.Tier, SortOrder: p.SortOrder,
		})
	}

	c, err := h.svc.Create(r.Context(), createIn)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"id": c.ID.String(), "slug": c.Slug})
}

type updateReq struct {
	Title                *string  `json:"title,omitempty"`
	TitleEn              *string  `json:"title_en,omitempty"`
	Slug                 *string  `json:"slug,omitempty"`
	Description          *string  `json:"description,omitempty"`
	DescriptionEn        *string  `json:"description_en,omitempty"`
	Status               *string  `json:"status,omitempty"`
	Format               *string  `json:"format,omitempty"`
	StartDate            *string  `json:"start_date,omitempty"`
	EndDate              *string  `json:"end_date,omitempty"`
	RegistrationDeadline *string  `json:"registration_deadline,omitempty"`
	MaxParticipants      *int     `json:"max_participants,omitempty"`
	Location             *string  `json:"location,omitempty"`
	City                 *string  `json:"city,omitempty"`
	CoverImageURL        *string  `json:"cover_image_url,omitempty"`
	IsFeatured           *bool    `json:"is_featured,omitempty"`
	Topics               []string `json:"topics,omitempty"`
}

func (h *Internal) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in updateReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	patch := repository.ConferencePatch{
		Title: in.Title, TitleEn: in.TitleEn, Slug: in.Slug,
		Description: in.Description, DescriptionEn: in.DescriptionEn,
		MaxParticipants: in.MaxParticipants, Location: in.Location,
		City: in.City, CoverImageURL: in.CoverImageURL, IsFeatured: in.IsFeatured,
	}
	if in.Status != nil {
		s := domain.Status(*in.Status)
		patch.Status = &s
	}
	if in.Format != nil {
		f, ok := domain.NormalizeFormat(*in.Format)
		if !ok {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "format", Message: "offline|hybrid|online"}}})
			return
		}
		patch.Format = &f
	}
	if in.StartDate != nil {
		t, err := time.Parse("2006-01-02", *in.StartDate)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "start_date", Message: "YYYY-MM-DD"}}})
			return
		}
		patch.StartDate = &t
	}
	if in.EndDate != nil {
		t, err := time.Parse("2006-01-02", *in.EndDate)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "end_date", Message: "YYYY-MM-DD"}}})
			return
		}
		patch.EndDate = &t
	}
	if in.RegistrationDeadline != nil {
		t, err := time.Parse(time.RFC3339, *in.RegistrationDeadline)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "registration_deadline", Message: "RFC3339"}}})
			return
		}
		patch.RegistrationDeadline = &t
	}
	if in.Topics != nil {
		patch.Topics = &in.Topics
	}

	updated, err := h.svc.Update(r.Context(), id, patch)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"id": updated.ID.String(), "status": string(updated.Status)})
}

// Cancel before deletion so Application Svc receives the compensation event.
func (h *Internal) softDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	cancelled := domain.StatusCancelled
	if _, err := h.svc.Update(r.Context(), id, repository.ConferencePatch{Status: &cancelled}); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type statusDTO struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Slug                 string   `json:"slug"`
	Status               string   `json:"status"`
	Topics               []string `json:"topics"`
	Format               string   `json:"format"`
	City                 *string  `json:"city"`
	StartDate            string   `json:"start_date"`
	RegistrationDeadline *string  `json:"registration_deadline,omitempty"`
}

func (h *Internal) statusFallback(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	c, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, mapErr(err))
		return
	}
	resp := statusDTO{ID: c.ID.String(), Title: c.Title, Slug: c.Slug, Status: string(c.Status), Topics: c.Topics, Format: string(c.Format), City: c.City, StartDate: c.StartDate.Format("2006-01-02")}
	if c.RegistrationDeadline != nil {
		deadline := c.RegistrationDeadline.UTC().Format(time.RFC3339)
		resp.RegistrationDeadline = &deadline
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

type scheduleReq struct {
	Slots []scheduleSlot `json:"slots"`
}

type scheduleSlot struct {
	SectionID   *string `json:"section_id,omitempty"`
	Title       string  `json:"title"`
	SpeakerName *string `json:"speaker_name,omitempty"`
	SpeakerAff  *string `json:"speaker_affiliation,omitempty"`
	Room        *string `json:"room,omitempty"`
	SlotDate    string  `json:"slot_date"`
	StartTime   string  `json:"start_time"`
	EndTime     string  `json:"end_time"`
	Description *string `json:"description,omitempty"`
}

func (h *Internal) replaceSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in scheduleReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slots := make([]domain.ScheduleSlot, 0, len(in.Slots))
	for _, s := range in.Slots {
		t, err := time.Parse("2006-01-02", s.SlotDate)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "slot_date", Message: "YYYY-MM-DD"}}})
			return
		}
		var sec *uuid.UUID
		if s.SectionID != nil {
			u, err := uuid.Parse(*s.SectionID)
			if err == nil {
				sec = &u
			}
		}
		slots = append(slots, domain.ScheduleSlot{
			SectionID: sec, Title: s.Title, SpeakerName: s.SpeakerName, SpeakerAff: s.SpeakerAff,
			Room: s.Room, SlotDate: t, StartTime: s.StartTime, EndTime: s.EndTime, Description: s.Description,
		})
	}
	if err := h.svc.ReplaceSchedule(r.Context(), id, slots); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type exportItem struct {
	ID                   string   `json:"id"`
	Slug                 string   `json:"slug"`
	Title                string   `json:"title"`
	TitleEn              *string  `json:"title_en,omitempty"`
	Description          *string  `json:"description,omitempty"`
	DescriptionEn        *string  `json:"description_en,omitempty"`
	Status               string   `json:"status"`
	Format               string   `json:"format"`
	StartDate            string   `json:"start_date"`
	EndDate              string   `json:"end_date"`
	RegistrationDeadline *string  `json:"registration_deadline,omitempty"`
	City                 *string  `json:"city,omitempty"`
	Location             *string  `json:"location,omitempty"`
	CoverImageURL        *string  `json:"cover_image_url,omitempty"`
	Topics               []string `json:"topics"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
}

type exportResp struct {
	Items      []exportItem `json:"items"`
	NextCursor *string      `json:"next_cursor,omitempty"`
}

func (h *Internal) export(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	limit = pagination.ClampLimit(limit, exportMaxPageSize)

	cur, err := pagination.Decode(q.Get("cursor"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "cursor", Message: "invalid"}}})
		return
	}
	f := repository.ListFilter{Limit: limit}
	if !cur.CreatedAt.IsZero() {
		ca := cur.CreatedAt
		f.After = &ca
		id, err := uuid.Parse(cur.ID)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "cursor", Message: "invalid"}}})
			return
		}
		f.AfterID = &id
	}

	confs, err := h.repo.List(r.Context(), f)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	items := make([]exportItem, 0, len(confs))
	for i := range confs {
		c := &confs[i]
		it := exportItem{
			ID: c.ID.String(), Slug: c.Slug, Title: c.Title, TitleEn: c.TitleEn,
			Description: c.Description, DescriptionEn: c.DescriptionEn,
			Status: string(c.Status), Format: string(c.Format),
			StartDate: c.StartDate.Format("2006-01-02"),
			EndDate:   c.EndDate.Format("2006-01-02"),
			City:      c.City, Location: c.Location, CoverImageURL: c.CoverImageURL,
			Topics:    c.Topics,
			CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if it.Topics == nil {
			it.Topics = []string{}
		}
		if c.RegistrationDeadline != nil {
			s := c.RegistrationDeadline.UTC().Format(time.RFC3339)
			it.RegistrationDeadline = &s
		}
		items = append(items, it)
	}
	resp := exportResp{Items: items}
	if len(confs) == limit && limit > 0 {
		last := confs[len(confs)-1]
		next := pagination.Cursor{CreatedAt: last.CreatedAt, ID: last.ID.String()}.Encode()
		resp.NextCursor = &next
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return wrap("not found", httputil.ErrNotFound)
	case errors.Is(err, repository.ErrDuplicate):
		return wrap("duplicate", httputil.ErrConflict)
	default:
		return err
	}
}

func wrap(msg string, sentinel error) error {
	return errors.Join(sentinel, errors.New(msg))
}
