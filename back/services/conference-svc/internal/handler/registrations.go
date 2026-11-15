package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
)

type Registrations struct {
	repo  *repository.Registrations
	redis *redis.Client
}

func NewRegistrations(repo *repository.Registrations, rdb *redis.Client) *Registrations {
	return &Registrations{repo: repo, redis: rdb}
}

func (h *Registrations) ConferenceRoutes(r chi.Router) {
	r.Post("/{id}/register", h.register)
	r.Get("/{id}/registration", h.current)
}
func (h *Registrations) RegistrationRoutes(r chi.Router) { r.Delete("/{id}", h.cancel) }
func (h *Registrations) MyRoutes(r chi.Router)           { r.Get("/registrations", h.listMine) }

type registerReq struct {
	RoleAtConference string `json:"role_at_conf"`
}

type conferenceSummaryDTO struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Status    string `json:"status"`
}

type registrationDTO struct {
	ID                string                `json:"id"`
	ConferenceID      string                `json:"conference_id"`
	UserID            string                `json:"user_id"`
	Status            string                `json:"status"`
	RoleAtConference  string                `json:"role_at_conf"`
	WaitlistPosition  *int                  `json:"waitlist_position,omitempty"`
	CancellationCount int                   `json:"cancellation_count"`
	RegisteredAt      string                `json:"registered_at"`
	Conference        *conferenceSummaryDTO `json:"conference,omitempty"`
}

func toRegistrationDTO(reg *domain.Registration, withConference bool) registrationDTO {
	dto := registrationDTO{
		ID: reg.ID.String(), ConferenceID: reg.ConferenceID.String(), UserID: reg.UserID.String(),
		Status: string(reg.Status), RoleAtConference: reg.RoleAtConference,
		CancellationCount: reg.CancellationCount, RegisteredAt: reg.RegisteredAt.UTC().Format(time.RFC3339),
	}
	if reg.Status == domain.RegistrationWaitlisted {
		position := reg.WaitlistPosition
		dto.WaitlistPosition = &position
	}
	if withConference {
		dto.Conference = &conferenceSummaryDTO{
			ID: reg.ConferenceID.String(), Slug: reg.ConferenceSlug, Title: reg.ConferenceTitle,
			StartDate: reg.ConferenceStartDate.Format("2006-01-02"), EndDate: reg.ConferenceEndDate.Format("2006-01-02"),
			Status: string(reg.ConferenceStatus),
		}
	}
	return dto
}

func claimsAndUserID(r *http.Request) (*jwtauth.Claims, uuid.UUID, error) {
	claims := jwtauth.ClaimsFromContext(r.Context())
	if claims == nil {
		return nil, uuid.Nil, fmt.Errorf("authentication required: %w", httputil.ErrUnauthorized)
	}
	id, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("invalid subject: %w", httputil.ErrUnauthorized)
	}
	return claims, id, nil
}

func (h *Registrations) checkRegisterRate(ctx context.Context, userID uuid.UUID) error {
	hash := sha256.Sum256([]byte(userID.String()))
	key := "rate:conference_register:" + hex.EncodeToString(hash[:8]) + ":" + time.Now().UTC().Format("200601021504")
	count, err := h.redis.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("rate limiter unavailable: %w", httputil.ErrUnavailable)
	}
	if count == 1 {
		_ = h.redis.Expire(ctx, key, 2*time.Minute).Err()
	}
	if count > 20 {
		return fmt.Errorf("registration rate limit: %w", httputil.ErrTooMany)
	}
	return nil
}

func (h *Registrations) register(w http.ResponseWriter, r *http.Request) {
	claims, userID, err := claimsAndUserID(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if !claims.EmailVerified {
		httputil.WriteError(w, r, &domain.RegistrationError{Code: "EMAIL_NOT_VERIFIED", Message: "email must be verified", Sentinel: httputil.ErrForbidden})
		return
	}
	if err := h.checkRegisterRate(r.Context(), userID); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	conferenceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in registerReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if in.RoleAtConference == "" {
		in.RoleAtConference = "attendee"
	}
	if in.RoleAtConference != "attendee" && in.RoleAtConference != "speaker" && in.RoleAtConference != "reviewer" {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "role_at_conf", Message: "must be attendee, speaker or reviewer"}}})
		return
	}
	reg, err := h.repo.Register(r.Context(), conferenceID, userID, claims.Email, claims.Name, in.RoleAtConference)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{"data": toRegistrationDTO(reg, false)})
}

func (h *Registrations) current(w http.ResponseWriter, r *http.Request) {
	_, userID, err := claimsAndUserID(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	conferenceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	registration, err := h.repo.CurrentForUser(r.Context(), conferenceID, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var data *registrationDTO
	if registration != nil {
		dto := toRegistrationDTO(registration, false)
		data = &dto
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Registrations) cancel(w http.ResponseWriter, r *http.Request) {
	_, userID, err := claimsAndUserID(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	registrationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	reg, _, err := h.repo.Cancel(r.Context(), registrationID, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": toRegistrationDTO(reg, false)})
}

func (h *Registrations) listMine(w http.ResponseWriter, r *http.Request) {
	_, userID, err := claimsAndUserID(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	page, perPage, err := parseRegistrationPage(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "confirmed" && status != "waitlisted" && status != "cancelled" {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "status", Message: "invalid status"}}})
		return
	}
	items, total, err := h.repo.ListMine(r.Context(), userID, status, page, perPage)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]registrationDTO, 0, len(items))
	for i := range items {
		data = append(data, toRegistrationDTO(&items[i], true))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"data":       data,
		"pagination": map[string]any{"page": page, "per_page": perPage, "total": total, "total_pages": int(math.Ceil(float64(total) / float64(perPage)))},
	})
}

func parseRegistrationPage(r *http.Request) (int, int, error) {
	page, perPage := 1, 20
	var err error
	if value := r.URL.Query().Get("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 {
			return 0, 0, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "page", Message: "must be a positive integer"}}}
		}
	}
	if value := r.URL.Query().Get("per_page"); value != "" {
		perPage, err = strconv.Atoi(value)
		if err != nil || perPage < 1 || perPage > 100 {
			return 0, 0, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "per_page", Message: "must be between 1 and 100"}}}
		}
	}
	return page, perPage, nil
}
