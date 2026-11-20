package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/conference-svc/internal/domain"
)

const maxContentItems = 100

// Request bodies for the organizer content editors. Each PUT replaces the
// whole list; items keep their id between saves.

type sectionEdit struct {
	ID                   string  `json:"id,omitempty"`
	Title                string  `json:"title"`
	TitleEn              *string `json:"title_en,omitempty"`
	Description          *string `json:"description,omitempty"`
	ModeratorName        *string `json:"moderator_name,omitempty"`
	ModeratorAffiliation *string `json:"moderator_affiliation,omitempty"`
}

type personEdit struct {
	ID          string  `json:"id,omitempty"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Role        string  `json:"role,omitempty"`
	Affiliation *string `json:"affiliation,omitempty"`
	JobTitle    *string `json:"job_title,omitempty"`
	PhotoURL    *string `json:"photo_url,omitempty"`
}

type partnerEdit struct {
	ID         string  `json:"id,omitempty"`
	Name       string  `json:"name"`
	LogoURL    *string `json:"logo_url,omitempty"`
	WebsiteURL *string `json:"website_url,omitempty"`
	Tier       *string `json:"tier,omitempty"`
}

type sectionsReq struct {
	Data []sectionEdit `json:"data"`
}
type peopleReq struct {
	Data []personEdit `json:"data"`
}
type partnersReq struct {
	Data []partnerEdit `json:"data"`
}

func (h *Organizer) ContentRoutes(r chi.Router) {
	r.Put("/{id}/sections", h.replaceSections)
	r.Put("/{id}/organizers", h.replaceOrganizers)
	r.Put("/{id}/partners", h.replacePartners)
}

// contentErrors collects field errors with the item index in the field name.
type contentErrors []httputil.FieldError

func (e *contentErrors) text(field string, value string, required bool, limit int) {
	value = strings.TrimSpace(value)
	if required && value == "" {
		*e = append(*e, httputil.FieldError{Field: field, Message: "required"})
	} else if utf8.RuneCountInString(value) > limit {
		*e = append(*e, httputil.FieldError{Field: field, Message: "too long"})
	}
}

func (e *contentErrors) optional(field string, value *string, limit int) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	v := strings.TrimSpace(*value)
	e.text(field, v, false, limit)
	return &v
}

func (e *contentErrors) link(field string, value *string) *string {
	v := e.optional(field, value, 1000)
	if v == nil {
		return nil
	}
	if u, err := url.Parse(*v); err != nil || u.Scheme != "https" || u.Host == "" {
		*e = append(*e, httputil.FieldError{Field: field, Message: "must be an https URL"})
	}
	return v
}

func (e *contentErrors) err() error {
	if len(*e) == 0 {
		return nil
	}
	return &httputil.ValidationError{Fields: *e}
}

func itemID(raw string) uuid.UUID {
	id, _ := uuid.Parse(raw)
	return id // an unknown or empty id inserts a new row
}

func trimmed(s string) string { return strings.TrimSpace(s) }

func (h *Organizer) contentTarget(w http.ResponseWriter, r *http.Request, body any) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return uuid.Nil, false
	}
	if _, err := h.requireManage(r, id, "edit"); err != nil {
		httputil.WriteError(w, r, err)
		return uuid.Nil, false
	}
	if err := httputil.DecodeJSON(r, body); err != nil {
		httputil.WriteError(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}

func tooMany(w http.ResponseWriter, r *http.Request, n int) bool {
	if n <= maxContentItems {
		return false
	}
	httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "data", Message: "at most 100 items"}}})
	return true
}

func (h *Organizer) replaceSections(w http.ResponseWriter, r *http.Request) {
	var in sectionsReq
	id, ok := h.contentTarget(w, r, &in)
	if !ok || tooMany(w, r, len(in.Data)) {
		return
	}
	var errs contentErrors
	items := make([]domain.Section, 0, len(in.Data))
	for i, item := range in.Data {
		p := func(f string) string { return "data[" + strconv.Itoa(i) + "]." + f }
		errs.text(p("title"), item.Title, true, 500)
		items = append(items, domain.Section{
			ID: itemID(item.ID), Title: trimmed(item.Title), SortOrder: i,
			TitleEn:              errs.optional(p("title_en"), item.TitleEn, 500),
			Description:          errs.optional(p("description"), item.Description, 5000),
			ModeratorName:        errs.optional(p("moderator_name"), item.ModeratorName, 255),
			ModeratorAffiliation: errs.optional(p("moderator_affiliation"), item.ModeratorAffiliation, 255),
		})
	}
	if err := errs.err(); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	h.saveContent(w, r, id, func(tx pgx.Tx) error { return h.repo.ReplaceSectionsInTx(r.Context(), tx, id, items) })
}

func (h *Organizer) replaceOrganizers(w http.ResponseWriter, r *http.Request) {
	var in peopleReq
	id, ok := h.contentTarget(w, r, &in)
	if !ok || tooMany(w, r, len(in.Data)) {
		return
	}
	var errs contentErrors
	items := make([]domain.Organizer, 0, len(in.Data))
	for i, item := range in.Data {
		p := func(f string) string { return "data[" + strconv.Itoa(i) + "]." + f }
		errs.text(p("name"), item.Name, true, 255)
		kind, valid := domain.NormalizeOrganizerKind(item.Kind)
		if !valid {
			errs = append(errs, httputil.FieldError{Field: p("kind"), Message: "organizer|committee"})
		}
		role := trimmed(item.Role)
		if role == "" {
			role = "organizer"
		}
		errs.text(p("role"), role, false, 100)
		items = append(items, domain.Organizer{
			ID: itemID(item.ID), Name: trimmed(item.Name), Kind: kind, Role: role, SortOrder: i,
			Affiliation: errs.optional(p("affiliation"), item.Affiliation, 255),
			JobTitle:    errs.optional(p("job_title"), item.JobTitle, 255),
			PhotoURL:    errs.link(p("photo_url"), item.PhotoURL),
		})
	}
	if err := errs.err(); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	h.saveContent(w, r, id, func(tx pgx.Tx) error { return h.repo.ReplaceOrganizersInTx(r.Context(), tx, id, items) })
}

func (h *Organizer) replacePartners(w http.ResponseWriter, r *http.Request) {
	var in partnersReq
	id, ok := h.contentTarget(w, r, &in)
	if !ok || tooMany(w, r, len(in.Data)) {
		return
	}
	var errs contentErrors
	items := make([]domain.Partner, 0, len(in.Data))
	for i, item := range in.Data {
		p := func(f string) string { return "data[" + strconv.Itoa(i) + "]." + f }
		errs.text(p("name"), item.Name, true, 255)
		items = append(items, domain.Partner{
			ID: itemID(item.ID), Name: trimmed(item.Name), SortOrder: i,
			LogoURL:    errs.link(p("logo_url"), item.LogoURL),
			WebsiteURL: errs.link(p("website_url"), item.WebsiteURL),
			Tier:       errs.optional(p("tier"), item.Tier, 50),
		})
	}
	if err := errs.err(); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	h.saveContent(w, r, id, func(tx pgx.Tx) error { return h.repo.ReplacePartnersInTx(r.Context(), tx, id, items) })
}

func (h *Organizer) saveContent(w http.ResponseWriter, r *http.Request, id uuid.UUID, replace func(pgx.Tx) error) {
	if err := h.svc.ReplaceContent(r.Context(), id, replace); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type contentResp struct {
	Sections   []sectionDTO   `json:"sections"`
	Organizers []organizerDTO `json:"organizers"`
	Partners   []partnerDTO   `json:"partners"`
}

// content returns the editable lists of a managed conference, drafts included.
func (h *Organizer) content(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if _, err := h.requireManage(r, id, ""); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	sections, err := h.repo.Sections(ctx, id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	people, err := h.repo.Organizers(ctx, id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	partners, err := h.repo.Partners(ctx, id)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	resp := contentResp{
		Sections:   make([]sectionDTO, 0, len(sections)),
		Organizers: make([]organizerDTO, 0, len(people)),
		Partners:   make([]partnerDTO, 0, len(partners)),
	}
	for i := range sections {
		resp.Sections = append(resp.Sections, toSection(&sections[i]))
	}
	for i := range people {
		resp.Organizers = append(resp.Organizers, toOrganizer(&people[i]))
	}
	for i := range partners {
		resp.Partners = append(resp.Partners, toPartner(&partners[i]))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httputil.WriteJSON(w, http.StatusOK, resp)
}
