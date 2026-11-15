package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/shared/testutil"
	"github.com/go-chi/chi/v5"
)

func TestHomeCountsProjectionOmitsAcceptanceMetric(t *testing.T) {
	pool := testutil.Database(t)
	router := chi.NewRouter()
	router.Route("/home", NewPublic(repository.NewConferences(pool), nil, nil).HomeRoutes)
	for _, test := range []struct {
		path        string
		includeRate bool
	}{{"/home?stats=counts", false}, {"/home", true}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", test.path, nil))
		if response.Code != 200 {
			t.Fatalf("%s status=%d", test.path, response.Code)
		}
		var body struct {
			Info struct {
				Stats map[string]any `json:"stats"`
			} `json:"heroInfo"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		_, found := body.Info.Stats["approvalRate"]
		if found != test.includeRate {
			t.Fatalf("%s approvalRate present=%v", test.path, found)
		}
		if _, ok := body.Info.Stats["conferences"]; !ok {
			t.Fatal("conference count missing")
		}
		if _, ok := body.Info.Stats["participants"]; !ok {
			t.Fatal("participant count missing")
		}
	}
}
