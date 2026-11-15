package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/confhub/conference-svc/internal/service"
	"github.com/confhub/shared/jwtauth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestPublicAndInternalCreateRejectDatesBeforeMinimum(t *testing.T) {
	issuer, verifier := testJWT(t)
	token, _, err := issuer.Issue(uuid.NewString(), jwtauth.RoleVerifiedOrganizer, "owner@example.test", "Owner", true)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1/conferences", func(r chi.Router) {
		r.Use(verifier.Middleware(false))
		NewOrganizer(service.New(nil), nil, nil, nil).ConferenceRoutes(r)
	})
	r.Route("/internal", NewInternal(service.New(nil), nil, nil).Routes)
	for _, path := range []string{"/api/v1/conferences/", "/internal/conferences"} {
		body := `{"title":"Date test","slug":"date-test","format":"online","start_date":"2026-12-31","end_date":"2027-01-02"}`
		if strings.HasPrefix(path, "/internal/") {
			body = strings.TrimSuffix(body, "}") + `,"created_by":"` + uuid.NewString() + `"}`
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		var problem struct {
			Code   string `json:"code"`
			Errors []struct {
				Field string `json:"field"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if response.Code != 422 || problem.Code != "START_DATE_TOO_EARLY" || len(problem.Errors) != 1 || problem.Errors[0].Field != "start_date" {
			t.Fatalf("%s: status=%d code=%s", path, response.Code, problem.Code)
		}
	}
}
