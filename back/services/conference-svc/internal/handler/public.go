package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
	searchsvc "github.com/confhub/conference-svc/internal/search"
)

const (
	defaultPerPage = 20
	maxPerPage     = 100
)

type Public struct {
	repo    *repository.Conferences
	search  *searchsvc.Client
	limiter *searchsvc.RateLimiter
}

func NewPublic(repo *repository.Conferences, search *searchsvc.Client, limiter *searchsvc.RateLimiter) *Public {
	return &Public{repo: repo, search: search, limiter: limiter}
}

func (h *Public) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/search", h.list)
	r.Get("/{id}", h.detail)
	r.Get("/{id}/calendar.ics", h.calendar)
	r.Get("/{id}/similar", h.similar)

	r.Get("/{id}/program", h.program)
	r.Get("/{id}/sessions", h.program)
	r.Get("/{id}/talks", h.talks)
	r.Get("/{id}/organizers", h.organizers)
	r.Get("/{id}/partners", h.partners)

	r.Put("/{id}/favorite", h.addFavorite)
	r.Delete("/{id}/favorite", h.removeFavorite)
}

func (h *Public) SearchRoutes(r chi.Router) {
	r.Get("/", h.list)
	r.Get("/autocomplete", h.autocomplete)
}

func (h *Public) autocomplete(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < 2 {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "q", Message: "minimum 2 characters"}}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if err := h.checkSearchRate(r); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	items, err := h.search.Autocomplete(r.Context(), query, limit)
	if err != nil {
		httputil.WriteError(w, r, errors.Join(httputil.ErrUnavailable, err))
		return
	}
	suggestions := make([]map[string]string, 0, len(items))
	for _, item := range items {
		suggestions = append(suggestions, map[string]string{"text": item.Text, "type": "conference", "slug": item.Slug})
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"suggestions": suggestions}})
}

func (h *Public) checkSearchRate(r *http.Request) error {
	ip := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if net.ParseIP(ip) == nil {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	allowed, err := h.limiter.Allow(r.Context(), ip)
	if err != nil {
		return errors.Join(httputil.ErrUnavailable, err)
	}
	if !allowed {
		return errors.Join(httputil.ErrTooMany, errors.New("search limit is 200 requests per minute"))
	}
	return nil
}

func (h *Public) FavoritesRoutes(r chi.Router) {
	r.Get("/", h.listFavorites)
}

func (h *Public) HomeRoutes(r chi.Router) {
	r.Get("/", h.home)
}

// RecommendationRoutes is mounted under /api/v1/home with optional auth.
func (h *Public) RecommendationRoutes(r chi.Router) {
	r.Get("/recommendations", h.recommendations)
}

type venueDTO struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city,omitempty"`
}

type cardDTO struct {
	ID                   string    `json:"id"`
	Slug                 string    `json:"slug"`
	Title                string    `json:"title"`
	TitleEn              *string   `json:"title_en,omitempty"`
	Description          *string   `json:"description,omitempty"`
	DescriptionEn        *string   `json:"description_en,omitempty"`
	Format               string    `json:"format"`
	Status               string    `json:"status"`
	RegistrationOpen     bool      `json:"registration_open"`
	StartDate            string    `json:"start_date"`
	EndDate              string    `json:"end_date"`
	RegistrationDeadline *string   `json:"registration_deadline,omitempty"`
	City                 *string   `json:"city,omitempty"`
	PrimaryVenue         *venueDTO `json:"primary_venue,omitempty"`
	CoverImageURL        *string   `json:"cover_image_url,omitempty"`
	Topics               []string  `json:"topics"`
	ParticipantsCount    int       `json:"participants_count"`
	MaxParticipants      *int      `json:"max_participants,omitempty"`
	SessionsCount        int       `json:"sessions_count"`
	IsFavorite           *bool     `json:"is_favorite,omitempty"`
	IsFeatured           bool      `json:"is_featured"`
	CreatedAt            string    `json:"created_at"`
}

func toCard(c *domain.Conference) cardDTO {
	card := cardDTO{
		ID:                c.ID.String(),
		Slug:              c.Slug,
		Title:             c.Title,
		TitleEn:           c.TitleEn,
		Description:       c.Description,
		DescriptionEn:     c.DescriptionEn,
		Format:            string(c.Format),
		Status:            string(c.Status),
		RegistrationOpen:  c.Status == domain.StatusRegistrationOpen && (c.RegistrationDeadline == nil || time.Now().Before(*c.RegistrationDeadline)),
		StartDate:         c.StartDate.Format("2006-01-02"),
		EndDate:           c.EndDate.Format("2006-01-02"),
		City:              c.City,
		CoverImageURL:     c.CoverImageURL,
		Topics:            c.Topics,
		ParticipantsCount: c.Participants,
		MaxParticipants:   c.MaxParticipants,
		SessionsCount:     c.SessionCount,
		IsFeatured:        c.IsFeatured,
		CreatedAt:         c.CreatedAt.UTC().Format(time.RFC3339),
	}
	if c.Topics == nil {
		card.Topics = []string{}
	}
	if c.RegistrationDeadline != nil {
		s := c.RegistrationDeadline.UTC().Format(time.RFC3339)
		card.RegistrationDeadline = &s
	}
	if c.Location != nil || c.City != nil {
		v := &venueDTO{}
		if c.Location != nil {
			v.Address = *c.Location
			v.Name = *c.Location
		}
		if c.City != nil {
			v.City = *c.City
		}
		card.PrimaryVenue = v
	}
	return card
}

func cardFor(c *domain.Conference, viewer *uuid.UUID) cardDTO {
	card := toCard(c)
	if viewer != nil {
		fav := c.IsFavorite
		card.IsFavorite = &fav
	}
	return card
}

func viewerID(r *http.Request) *uuid.UUID {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil {
		return nil
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return nil
	}
	return &id
}

func requireViewer(r *http.Request) (uuid.UUID, error) {
	if id := viewerID(r); id != nil {
		return *id, nil
	}
	return uuid.Nil, errors.Join(httputil.ErrUnauthorized, errors.New("authentication required"))
}

type paginationDTO struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type filterDTO struct {
	Title string `json:"title"`
	Key   string `json:"key"`
	Type  string `json:"type"`

	Values   []filterOption `json:"values,omitempty"`
	Options  []string       `json:"options,omitempty"`
	Selected string         `json:"selected,omitempty"`
	Value    string         `json:"value,omitempty"`
	From     string         `json:"from,omitempty"`
	To       string         `json:"to,omitempty"`
}

type filterOption struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Selected bool   `json:"selected"`
}

type statsDTO struct {
	Participants int  `json:"participants"`
	Conferences  int  `json:"conferences"`
	ApprovalRate *int `json:"approvalRate,omitempty"`
}

type heroInfoDTO struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Stats       statsDTO `json:"stats"`
}

type listResp struct {
	Data           []cardDTO     `json:"data"`
	Filters        []filterDTO   `json:"filters"`
	Pagination     paginationDTO `json:"pagination"`
	HeroConference *cardDTO      `json:"heroConference,omitempty"`
	HeroInfo       *heroInfoDTO  `json:"heroInfo,omitempty"`
}

func tabToStatuses(tab string) ([]string, string) {
	switch tab {
	case "upcoming":
		return []string{"registration_closed"}, "upcoming"
	case "past":
		return []string{"completed", "archived"}, "past"
	case "active", "":
		return []string{"registration_open", "in_progress"}, "active"
	default:
		return nil, ""
	}
}

var publicStatuses = []string{
	"registration_open", "registration_closed", "in_progress", "completed", "cancelled", "archived",
}

type listQuery struct {
	filter  repository.ListFilter
	page    int
	perPage int
	tab     string
	search  string
	city    string
	from    string
	to      string
	hero    bool
}

func parseListQuery(r *http.Request) (listQuery, error) {
	q := r.URL.Query()
	out := listQuery{}

	out.page, out.perPage = pageParams(q.Get("page"), q.Get("per_page"))

	out.search = strings.TrimSpace(firstNonEmpty(q.Get("searchText"), q.Get("q")))

	f := repository.ListFilter{
		Query:  out.search,
		Limit:  out.perPage,
		Offset: (out.page - 1) * out.perPage,
		Sort:   q.Get("sort"),
	}

	if v := csv(q.Get("topics")); len(v) > 0 {
		f.Topics = v
	}
	for _, raw := range csv(q.Get("format")) {
		fm, ok := domain.NormalizeFormat(raw)
		if !ok {
			return out, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "format", Message: "offline|hybrid|online"}}}
		}
		f.Formats = append(f.Formats, string(fm))
	}
	out.city = strings.TrimSpace(q.Get("city"))
	if out.city != "" {
		f.Cities = csv(out.city)
	}

	statuses, tab := tabToStatuses(q.Get("tab"))
	if tab == "" {
		return out, &httputil.ValidationError{Fields: []httputil.FieldError{
			{Field: "tab", Message: "active|upcoming|past"}}}
	}
	out.tab = tab
	if explicit := csv(q.Get("status")); len(explicit) > 0 {
		f.Statuses = intersect(explicit, publicStatuses)
		if len(f.Statuses) == 0 {
			return out, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "status", Message: "unknown or non-public status"}}}
		}
	} else {
		f.Statuses = statuses
	}

	out.from = q.Get("from_date")
	if out.from != "" {
		t, err := time.Parse("2006-01-02", out.from)
		if err != nil {
			return out, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "from_date", Message: "YYYY-MM-DD"}}}
		}
		f.FromDate = &t
	}
	out.to = q.Get("to_date")
	if out.to != "" {
		t, err := time.Parse("2006-01-02", out.to)
		if err != nil {
			return out, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "to_date", Message: "YYYY-MM-DD"}}}
		}
		f.ToDate = &t
	}
	if f.FromDate != nil && f.ToDate != nil && f.ToDate.Before(*f.FromDate) {
		return out, &httputil.ValidationError{Fields: []httputil.FieldError{
			{Field: "to_date", Message: "must be >= from_date"}}}
	}

	for _, inc := range csv(q.Get("include")) {
		if inc == "hero" {
			out.hero = true
		}
	}

	out.filter = f
	return out, nil
}

func csv(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func intersect(got, allowed []string) []string {
	set := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		set[a] = struct{}{}
	}
	var out []string
	for _, g := range got {
		if _, ok := set[g]; ok {
			out = append(out, g)
		}
	}
	return out
}

func (h *Public) list(w http.ResponseWriter, r *http.Request) {
	lq, err := parseListQuery(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	viewer := viewerID(r)
	lq.filter.ViewerID = viewer

	var confs []domain.Conference
	var total int
	var topics, formats, cities []repository.FacetBucket
	searched := false
	if lq.search != "" {
		if err := h.checkSearchRate(r); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		result, searchErr := h.search.Search(r.Context(), searchsvc.Filter{
			Query: lq.search, Topics: lq.filter.Topics, Statuses: lq.filter.Statuses,
			Formats: lq.filter.Formats, Cities: lq.filter.Cities,
			FromDate: lq.filter.FromDate, ToDate: lq.filter.ToDate, Sort: lq.filter.Sort,
			Page: lq.page, PerPage: lq.perPage,
		})
		switch {
		case searchErr == nil:
			searched = true
			confs, err = h.repo.CardsByIDs(r.Context(), result.IDs, viewer)
			total = result.Total
			topics = searchBuckets(result.Topics)
			formats = searchBuckets(result.Formats)
			cities = searchBuckets(result.Cities)
		case errors.Is(searchErr, searchsvc.ErrUnavailable):
			// Elasticsearch is down or still building the index: fall back to
			// PostgreSQL full-text search below.
			slog.WarnContext(r.Context(), "search unavailable, using database", "err", searchErr)
		default:
			httputil.WriteError(w, r, searchErr)
			return
		}
	}
	if !searched {
		confs, total, err = h.repo.ListPage(r.Context(), lq.filter)
		if err == nil {
			topics, formats, cities, err = h.repo.Facets(r.Context(), lq.filter)
		}
	}
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	data := make([]cardDTO, 0, len(confs))
	for i := range confs {
		data = append(data, cardFor(&confs[i], viewer))
	}

	resp := listResp{
		Data:    data,
		Filters: buildFilters(lq, topics, formats, cities),
		Pagination: paginationDTO{
			Page:       lq.page,
			PerPage:    lq.perPage,
			Total:      total,
			TotalPages: totalPages(total, lq.perPage),
		},
	}

	if lq.hero {
		hero, info, err := h.heroPayload(r)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		resp.HeroConference = hero
		resp.HeroInfo = info
	}

	w.Header().Set("Vary", "Authorization")
	if viewer == nil {
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

func searchBuckets(items []searchsvc.Bucket) []repository.FacetBucket {
	out := make([]repository.FacetBucket, 0, len(items))
	for _, item := range items {
		out = append(out, repository.FacetBucket{Value: item.Value, Count: item.Count})
	}
	return out
}

type homeResp struct {
	HeroConference *cardDTO    `json:"heroConference,omitempty"`
	HeroInfo       heroInfoDTO `json:"heroInfo"`
}

func (h *Public) home(w http.ResponseWriter, r *http.Request) {
	hero, info, err := h.heroPayload(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httputil.WriteJSON(w, http.StatusOK, homeResp{HeroConference: hero, HeroInfo: *info})
}

func (h *Public) heroPayload(r *http.Request) (*cardDTO, *heroInfoDTO, error) {
	countsOnly := r.URL.Query().Get("stats") == "counts"
	stats, err := h.repo.PlatformStats(r.Context(), !countsOnly)
	if err != nil {
		return nil, nil, err
	}
	info := &heroInfoDTO{
		Title:       "Конференции для исследователей и учёных",
		Description: "Единое окно для подачи заявок, рецензирования, программ и сборников трудов.",
		Stats: statsDTO{
			Participants: stats.Participants,
			Conferences:  stats.Conferences,
		},
	}
	if !countsOnly {
		rate := int(math.Round(stats.ApprovalRate * 100))
		info.Stats.ApprovalRate = &rate
	}
	c, err := h.repo.Hero(r.Context())
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, info, nil
		}
		return nil, nil, err
	}
	card := toCard(c)
	return &card, info, nil
}

func totalPages(total, perPage int) int {
	if perPage <= 0 || total <= 0 {
		return 0
	}
	return (total + perPage - 1) / perPage
}

var formatLabels = map[string]string{
	"offline": "Очно",
	"hybrid":  "Гибрид",
	"online":  "Заочно",
}

func buildFilters(lq listQuery, topics, formats, cities []repository.FacetBucket) []filterDTO {
	selectedTopics := toSet(lq.filter.Topics)
	topicOpts := make([]filterOption, 0, len(topics))
	for _, b := range topics {
		_, sel := selectedTopics[b.Value]
		topicOpts = append(topicOpts, filterOption{
			Value: b.Value, Label: b.Value, Count: b.Count, Selected: sel,
		})
	}

	selectedFormats := toSet(lq.filter.Formats)
	formatOpts := make([]filterOption, 0, len(formats))
	for _, b := range formats {
		label := formatLabels[b.Value]
		if label == "" {
			label = b.Value
		}
		_, sel := selectedFormats[b.Value]
		formatOpts = append(formatOpts, filterOption{
			Value: b.Value, Label: label, Count: b.Count, Selected: sel,
		})
	}

	cityOpts := make([]filterOption, 0, len(cities))
	for _, b := range cities {
		cityOpts = append(cityOpts, filterOption{
			Value: b.Value, Label: b.Value, Count: b.Count,
			Selected: containsFold(lq.filter.Cities, b.Value),
		})
	}

	return []filterDTO{
		{Title: "Тематика", Key: "topics", Type: "multi", Values: topicOpts},
		{Title: "Формат", Key: "format", Type: "multi", Values: formatOpts},
		{Title: "Город", Key: "city", Type: "text", Value: lq.city},
		{Title: "Города", Key: "city_options", Type: "multi", Values: cityOpts},
		{Title: "Статус", Key: "tab", Type: "radio",
			Options: []string{"active", "upcoming", "past"}, Selected: lq.tab},
		{Title: "Даты", Key: "dates", Type: "range", From: lq.from, To: lq.to},
	}
}

func toSet(v []string) map[string]struct{} {
	m := make(map[string]struct{}, len(v))
	for _, s := range v {
		m[s] = struct{}{}
	}
	return m
}

func containsFold(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}

type detailResp struct {
	cardDTO
	DescriptionEn    *string        `json:"description_en,omitempty"`
	FavoritesCount   int            `json:"favorites_count"`
	Organizers       []organizerDTO `json:"organizers"`
	ProgramCommittee []organizerDTO `json:"program_committee"`
	Sections         []sectionDTO   `json:"sections"`
	Schedule         []scheduleDTO  `json:"schedule"`
	Partners         []partnerDTO   `json:"partners"`
}

type organizerDTO struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Affiliation *string `json:"affiliation,omitempty"`
	JobTitle    *string `json:"job_title,omitempty"`
	PhotoURL    *string `json:"photo_url,omitempty"`
	Role        string  `json:"role"`
	Kind        string  `json:"kind"`
	SortOrder   int     `json:"sort_order"`
}

func toOrganizer(o *domain.Organizer) organizerDTO {
	return organizerDTO{
		ID: o.ID.String(), Name: o.Name, Affiliation: o.Affiliation,
		JobTitle: o.JobTitle, PhotoURL: o.PhotoURL,
		Role: o.Role, Kind: o.Kind, SortOrder: o.SortOrder,
	}
}

func splitOrganizers(list []domain.Organizer) (organizers, committee []organizerDTO) {
	organizers = make([]organizerDTO, 0, len(list))
	committee = make([]organizerDTO, 0)
	for i := range list {
		dto := toOrganizer(&list[i])
		if list[i].Kind == domain.OrganizerKindCommittee {
			committee = append(committee, dto)
			continue
		}
		organizers = append(organizers, dto)
	}
	return organizers, committee
}

type personDTO struct {
	Name        string  `json:"name"`
	Affiliation *string `json:"affiliation,omitempty"`
	UserID      *string `json:"user_id,omitempty"`
}

type sectionDTO struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	TitleEn     *string    `json:"title_en,omitempty"`
	Description *string    `json:"description,omitempty"`
	SortOrder   int        `json:"sort_order"`
	Moderator   *personDTO `json:"moderator,omitempty"`
	TalksCount  int        `json:"talks_count"`
}

func toSection(s *domain.Section) sectionDTO {
	dto := sectionDTO{
		ID: s.ID.String(), Title: s.Title, TitleEn: s.TitleEn,
		Description: s.Description, SortOrder: s.SortOrder, TalksCount: s.TalksCount,
	}
	if s.ModeratorName != nil && *s.ModeratorName != "" {
		m := &personDTO{Name: *s.ModeratorName, Affiliation: s.ModeratorAffiliation}
		if s.ModeratorUserID != nil {
			u := s.ModeratorUserID.String()
			m.UserID = &u
		}
		dto.Moderator = m
	}
	return dto
}

type scheduleDTO struct {
	ID                 string  `json:"id"`
	SectionID          *string `json:"section_id,omitempty"`
	SectionTitle       *string `json:"section_title,omitempty"`
	Title              string  `json:"title"`
	SpeakerName        *string `json:"speaker_name,omitempty"`
	SpeakerAffiliation *string `json:"speaker_affiliation,omitempty"`
	Description        *string `json:"description,omitempty"`
	Room               *string `json:"room,omitempty"`
	SlotDate           string  `json:"slot_date"`
	StartTime          string  `json:"start_time"`
	EndTime            string  `json:"end_time"`
	SessionType        string  `json:"session_type"`
}

func toSlot(s *domain.ScheduleSlot) scheduleDTO {
	dto := scheduleDTO{
		ID: s.ID.String(), SectionTitle: s.SectionTitle, Title: s.Title,
		SpeakerName: s.SpeakerName, SpeakerAffiliation: s.SpeakerAff,
		Description: s.Description, Room: s.Room,
		SlotDate:  s.SlotDate.Format("2006-01-02"),
		StartTime: s.StartTime, EndTime: s.EndTime, SessionType: s.SessionType,
	}
	if s.SectionID != nil {
		v := s.SectionID.String()
		dto.SectionID = &v
	}
	return dto
}

type partnerDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	LogoURL    *string `json:"logo_url,omitempty"`
	WebsiteURL *string `json:"website_url,omitempty"`
	Tier       *string `json:"tier,omitempty"`
	SortOrder  int     `json:"sort_order"`
}

func toPartner(p *domain.Partner) partnerDTO {
	return partnerDTO{
		ID: p.ID.String(), Name: p.Name, LogoURL: p.LogoURL,
		WebsiteURL: p.WebsiteURL, Tier: p.Tier, SortOrder: p.SortOrder,
	}
}

func (h *Public) resolve(r *http.Request) (*domain.Conference, error) {
	idStr := chi.URLParam(r, "id")
	var c *domain.Conference
	var err error
	if id, parseErr := uuid.Parse(idStr); parseErr == nil {
		c, err = h.repo.GetByID(r.Context(), id)
	} else {
		c, err = h.repo.GetBySlug(r.Context(), idStr)
	}
	if err != nil {
		return nil, mapErr(err)
	}
	if c.Status == domain.StatusDraft || !c.PublicationAllowed {
		return nil, mapErr(repository.ErrNotFound)
	}
	return c, nil
}

func (h *Public) detail(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	ctx := r.Context()

	counters, err := h.repo.Counters(ctx, c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	c.Participants = counters.Participants

	sections, err := h.repo.Sections(ctx, c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slots, err := h.repo.Schedule(ctx, c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	c.SessionCount = len(slots)

	organizers, err := h.repo.Organizers(ctx, c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	partners, err := h.repo.Partners(ctx, c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	favCount, err := h.repo.FavoritesCount(ctx, c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	viewer := viewerID(r)
	if viewer != nil {
		fav, err := h.repo.IsFavorite(ctx, *viewer, c.ID)
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		c.IsFavorite = fav
	}

	orgs, committee := splitOrganizers(organizers)
	resp := detailResp{
		cardDTO:          cardFor(c, viewer),
		DescriptionEn:    c.DescriptionEn,
		FavoritesCount:   favCount,
		Organizers:       orgs,
		ProgramCommittee: committee,
		Sections:         make([]sectionDTO, 0, len(sections)),
		Schedule:         make([]scheduleDTO, 0, len(slots)),
		Partners:         make([]partnerDTO, 0, len(partners)),
	}
	for i := range sections {
		resp.Sections = append(resp.Sections, toSection(&sections[i]))
	}
	for i := range slots {
		resp.Schedule = append(resp.Schedule, toSlot(&slots[i]))
	}
	for i := range partners {
		resp.Partners = append(resp.Partners, toPartner(&partners[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

func icsEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ";", `\;`)
	value = strings.ReplaceAll(value, ",", `\,`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func (h *Public) calendar(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	end := c.EndDate.AddDate(0, 0, 1)
	location := ""
	if c.Location != nil {
		location = *c.Location
	}
	description := ""
	if c.Description != nil {
		description = *c.Description
	}
	body := fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//ConfHub//RU\r\nCALSCALE:GREGORIAN\r\nMETHOD:PUBLISH\r\nBEGIN:VEVENT\r\nUID:%s@confhub\r\nDTSTAMP:%s\r\nDTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\nSUMMARY:%s\r\nDESCRIPTION:%s\r\nLOCATION:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		c.ID, time.Now().UTC().Format("20060102T150405Z"), c.StartDate.Format("20060102"), end.Format("20060102"),
		icsEscape(c.Title), icsEscape(description), icsEscape(location))
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="conference.ics"`)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

type programDayDTO struct {
	Date  string        `json:"date"`
	Slots []scheduleDTO `json:"slots"`
}

type programResp struct {
	Days     []programDayDTO `json:"days"`
	Sections []sectionDTO    `json:"sections"`
}

func (h *Public) program(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slots, err := h.repo.Schedule(r.Context(), c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	sections, err := h.repo.Sections(r.Context(), c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}

	resp := programResp{Days: make([]programDayDTO, 0), Sections: make([]sectionDTO, 0, len(sections))}
	for i := range slots {
		day := slots[i].SlotDate.Format("2006-01-02")
		if n := len(resp.Days); n == 0 || resp.Days[n-1].Date != day {
			resp.Days = append(resp.Days, programDayDTO{Date: day, Slots: []scheduleDTO{}})
		}
		last := &resp.Days[len(resp.Days)-1]
		last.Slots = append(last.Slots, toSlot(&slots[i]))
	}
	for i := range sections {
		resp.Sections = append(resp.Sections, toSection(&sections[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

type talksResp struct {
	Data       []scheduleDTO `json:"data"`
	Pagination paginationDTO `json:"pagination"`
}

func (h *Public) talks(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	page, perPage := pageParams(q.Get("page"), q.Get("per_page"))

	f := repository.TalkFilter{
		ConferenceID: c.ID,
		Query:        strings.TrimSpace(firstNonEmpty(q.Get("searchText"), q.Get("q"))),
		Limit:        perPage,
		Offset:       (page - 1) * perPage,
	}
	if v := q.Get("section_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "section_id", Message: "invalid uuid"}}})
			return
		}
		f.SectionID = &id
	}
	if v := q.Get("day"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{
				{Field: "day", Message: "YYYY-MM-DD"}}})
			return
		}
		f.Day = &d
	}

	slots, total, err := h.repo.Talks(r.Context(), f)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	resp := talksResp{
		Data: make([]scheduleDTO, 0, len(slots)),
		Pagination: paginationDTO{
			Page: page, PerPage: perPage, Total: total, TotalPages: totalPages(total, perPage),
		},
	}
	for i := range slots {
		resp.Data = append(resp.Data, toSlot(&slots[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

type organizersResp struct {
	Organizers       []organizerDTO `json:"organizers"`
	ProgramCommittee []organizerDTO `json:"program_committee"`
}

func (h *Public) organizers(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	list, err := h.repo.Organizers(r.Context(), c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	orgs, committee := splitOrganizers(list)
	httputil.WriteJSON(w, http.StatusOK, organizersResp{Organizers: orgs, ProgramCommittee: committee})
}

func (h *Public) partners(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	list, err := h.repo.Partners(r.Context(), c.ID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	out := make([]partnerDTO, 0, len(list))
	for i := range list {
		out = append(out, toPartner(&list[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Public) addFavorite(w http.ResponseWriter, r *http.Request) {
	uid, err := requireViewer(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.repo.AddFavorite(r.Context(), uid, c.ID); err != nil {
		httputil.WriteError(w, r, mapErr(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Public) removeFavorite(w http.ResponseWriter, r *http.Request) {
	uid, err := requireViewer(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.repo.RemoveFavorite(r.Context(), uid, c.ID); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type favoritesResp struct {
	Data       []cardDTO     `json:"data"`
	Pagination paginationDTO `json:"pagination"`
}

func (h *Public) listFavorites(w http.ResponseWriter, r *http.Request) {
	uid, err := requireViewer(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	page, perPage := pageParams(q.Get("page"), q.Get("per_page"))

	confs, total, err := h.repo.FavoritesPage(r.Context(), uid, perPage, (page-1)*perPage)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	resp := favoritesResp{
		Data: make([]cardDTO, 0, len(confs)),
		Pagination: paginationDTO{
			Page: page, PerPage: perPage, Total: total, TotalPages: totalPages(total, perPage),
		},
	}
	for i := range confs {
		resp.Data = append(resp.Data, cardFor(&confs[i], &uid))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httputil.WriteJSON(w, http.StatusOK, resp)
}

func pageParams(pageRaw, perPageRaw string) (page, perPage int) {
	page, _ = strconv.Atoi(pageRaw)
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(perPageRaw)
	if perPage <= 0 {
		perPage = defaultPerPage
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return page, perPage
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
