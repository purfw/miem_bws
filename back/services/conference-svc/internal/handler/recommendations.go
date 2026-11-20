package handler

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/conference-svc/internal/recommend"
	"github.com/confhub/conference-svc/internal/repository"
)

const (
	defaultRecommendations = 6
	maxRecommendations     = 24
)

type recommendationDTO struct {
	cardDTO
	Reasons []recommend.Reason `json:"reasons"`
}

type recommendationsResp struct {
	Data []recommendationDTO `json:"data"`
	// Personalised is false when the list is the cold-start popularity ranking.
	Personalised bool `json:"personalised"`
}

func recommendationLimit(r *http.Request) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		return defaultRecommendations
	}
	return min(limit, maxRecommendations)
}

// recommendations ranks conferences from the viewer's registrations and
// favourites; anonymous visitors and new users get the most popular ones.
func (h *Public) recommendations(w http.ResponseWriter, r *http.Request) {
	viewer := viewerID(r)
	var seeds []repository.Seed
	if viewer != nil {
		var err error
		if seeds, err = h.repo.UserSeeds(r.Context(), *viewer); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
	}
	h.writeRecommendations(w, r, seeds, viewer)
}

// similar ranks conferences against the one being viewed.
func (h *Public) similar(w http.ResponseWriter, r *http.Request) {
	c, err := h.resolve(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	h.writeRecommendations(w, r, []repository.Seed{{ConferenceID: c.ID, Weight: 1}}, viewerID(r))
}

func (h *Public) writeRecommendations(w http.ResponseWriter, r *http.Request, seeds []repository.Seed, viewer *uuid.UUID) {
	candidates, err := h.repo.RecommendationCandidates(r.Context(), seeds, viewer)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	ranked := recommend.Rank(candidates, recommendationLimit(r))
	ids := make([]uuid.UUID, 0, len(ranked))
	for _, item := range ranked {
		ids = append(ids, item.ID)
	}
	cards, err := h.repo.CardsByIDs(r.Context(), ids, viewer)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	reasons := make(map[uuid.UUID][]recommend.Reason, len(ranked))
	for _, item := range ranked {
		reasons[item.ID] = item.Reasons
	}
	resp := recommendationsResp{Data: make([]recommendationDTO, 0, len(cards)), Personalised: recommend.Personalised(ranked)}
	for i := range cards {
		item := recommendationDTO{cardDTO: cardFor(&cards[i], viewer), Reasons: reasons[cards[i].ID]}
		if item.Reasons == nil {
			item.Reasons = []recommend.Reason{}
		}
		resp.Data = append(resp.Data, item)
	}
	w.Header().Set("Vary", "Authorization")
	if viewer == nil {
		w.Header().Set("Cache-Control", "public, max-age=60")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}
