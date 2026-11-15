package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"

	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/application-svc/internal/service"
)

type reviewReq struct {
	Score             int16   `json:"score"`
	Summary           string  `json:"summary"`
	Strengths         *string `json:"strengths,omitempty"`
	Weaknesses        *string `json:"weaknesses,omitempty"`
	Recommendation    string  `json:"recommendation"`
	IsVisibleToAuthor bool    `json:"is_visible_to_author"`
}

type reviewDTO struct {
	ID                string  `json:"id"`
	ApplicationID     string  `json:"application_id"`
	ReviewerID        string  `json:"reviewer_id"`
	ReviewerName      string  `json:"reviewer_name"`
	Score             int16   `json:"score"`
	Summary           string  `json:"summary"`
	Strengths         *string `json:"strengths,omitempty"`
	Weaknesses        *string `json:"weaknesses,omitempty"`
	Recommendation    string  `json:"recommendation"`
	IsVisibleToAuthor bool    `json:"is_visible_to_author"`
	CreatedAt         string  `json:"created_at"`
}

func toReviewDTO(r *domain.Review) reviewDTO {
	return reviewDTO{
		ID: r.ID.String(), ApplicationID: r.ApplicationID.String(),
		ReviewerID: r.ReviewerID.String(), ReviewerName: r.CachedReviewerName,
		Score: r.Score, Summary: r.Summary,
		Strengths: r.Strengths, Weaknesses: r.Weaknesses,
		Recommendation: string(r.Recommendation), IsVisibleToAuthor: r.IsVisibleToAuthor,
		CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func (h *Applications) listReviews(w http.ResponseWriter, r *http.Request) {
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
	rvs, err := h.svc.ListReviews(r.Context(), id, uid, c.Role.AtLeast(jwtauth.RoleModerator))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	out := make([]reviewDTO, 0, len(rvs))
	for i := range rvs {
		out = append(out, toReviewDTO(&rvs[i]))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Applications) createReview(w http.ResponseWriter, r *http.Request) {
	c := jwtauth.ClaimsFromContext(r.Context())
	if c == nil || !c.Role.AtLeast(jwtauth.RoleModerator) {
		httputil.WriteError(w, r, errForbidden())
		return
	}
	uid, _ := uuid.Parse(c.UserID)
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	var in reviewReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	_, name := profileFromContext(r)
	rv, err := h.svc.CreateReview(r.Context(), &service.ReviewInput{
		ApplicationID: appID, ReviewerID: uid, ReviewerName: name,
		Score: in.Score, Summary: in.Summary,
		Strengths: in.Strengths, Weaknesses: in.Weaknesses,
		Recommendation:    domain.Recommendation(in.Recommendation),
		IsVisibleToAuthor: in.IsVisibleToAuthor,
	})
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, toReviewDTO(rv))
}
