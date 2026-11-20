package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/testutil"

	"github.com/confhub/conference-svc/internal/repository"
)

func TestRecommendationEndpoints(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	topic := "Рек-" + uuid.NewString()[:8]
	day := time.Now().AddDate(0, 4, 0)
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	slugs := []string{"rec-a-" + uuid.NewString()[:8], "rec-b-" + uuid.NewString()[:8]}
	for i, id := range ids {
		if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences
			(id,title,slug,start_date,end_date,created_by,topics,status,publication_allowed)
			VALUES($1,'Рекомендация',$2,$3,$3,$4,$5,'registration_open',true)`, id, slugs[i], day, uuid.New(), []string{topic}); err != nil {
			t.Fatal(err)
		}
	}
	h := NewPublic(repository.NewConferences(pool), nil, nil)
	r := chi.NewRouter()
	r.Route("/api/v1/home", h.RecommendationRoutes)
	r.Route("/api/v1/conferences", h.Routes)

	get := func(path string) recommendationsResp {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body.String())
		}
		var out recommendationsResp
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	home := get("/api/v1/home/recommendations?limit=50")
	if home.Personalised || len(home.Data) == 0 {
		t.Fatalf("anonymous list: personalised=%v items=%d", home.Personalised, len(home.Data))
	}
	similar := get("/api/v1/conferences/" + slugs[0] + "/similar?limit=24")
	if !similar.Personalised || len(similar.Data) == 0 || similar.Data[0].ID != ids[1].String() {
		t.Fatalf("similar must start with the same-topic conference: %+v", similar)
	}
	if similar.Data[0].Reasons[0].Code != "topic" || similar.Data[0].Reasons[0].Value != topic {
		t.Fatalf("reasons: %+v", similar.Data[0].Reasons)
	}
	for _, item := range similar.Data {
		if item.ID == ids[0].String() {
			t.Fatal("a conference is not similar to itself")
		}
	}
}
