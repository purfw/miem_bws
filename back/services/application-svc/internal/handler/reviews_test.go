package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/application-svc/internal/repository"
	"github.com/confhub/application-svc/internal/service"
	"github.com/confhub/shared/testutil"
)

func TestInternalReviewRequestValidation(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/internal", NewInternal(&service.Applications{}).Routes)
	valid := `"reviewer_name":"Программный комитет","score":7,"summary":"Сильная работа","recommendation":"accept"`
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		field                    string
	}{
		{"invalid application id", "POST", "/internal/applications/not-a-uuid/reviews", `{"reviewer_id":"` + uuid.NewString() + `",` + valid + `}`, 422, "id"},
		{"missing reviewer", "POST", "/internal/applications/" + uuid.NewString() + "/reviews", `{` + valid + `}`, 422, "reviewer_id"},
		{"nil reviewer", "POST", "/internal/applications/" + uuid.NewString() + "/reviews", `{"reviewer_id":"` + uuid.Nil.String() + `",` + valid + `}`, 422, "reviewer_id"},
		{"score out of range", "POST", "/internal/applications/" + uuid.NewString() + "/reviews", `{"reviewer_id":"` + uuid.NewString() + `","reviewer_name":"A","score":11,"summary":"x","recommendation":"accept"}`, 422, "score"},
		{"unknown recommendation", "POST", "/internal/applications/" + uuid.NewString() + "/reviews", `{"reviewer_id":"` + uuid.NewString() + `","reviewer_name":"A","score":5,"summary":"x","recommendation":"maybe"}`, 422, "recommendation"},
		{"blank summary", "POST", "/internal/applications/" + uuid.NewString() + "/reviews", `{"reviewer_id":"` + uuid.NewString() + `","reviewer_name":"A","score":5,"summary":"  ","recommendation":"reject"}`, 422, "summary"},
		{"unknown field", "POST", "/internal/applications/" + uuid.NewString() + "/reviews", `{"reviewer_id":"` + uuid.NewString() + `",` + valid + `,"extra":1}`, 422, ""},
		{"invalid list id", "GET", "/internal/applications/not-a-uuid/reviews", "", 422, "id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.field != "" && !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
				t.Fatalf("expected field %q in %s", tc.field, rec.Body.String())
			}
		})
	}
}

func TestInternalReviewsCreateAndListHiddenReviews(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	var appID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO application_svc.applications
		(conference_id, user_id, cached_user_email, cached_user_name, cached_conf_title, form, title)
		VALUES ($1, $2, 'author@example.test', 'Author', 'Test conference', 'speaker', 'Test talk')
		RETURNING id`, uuid.New(), uuid.New()).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM application_svc.applications WHERE id=$1`, appID)
	})
	r := chi.NewRouter()
	svc := service.New(repository.NewApplications(pool), nil, nil, nil, nil, 0, "", "", 0)
	r.Route("/internal", NewInternal(svc).Routes)
	path := "/internal/applications/" + appID.String() + "/reviews"
	body := `{"reviewer_id":"` + uuid.NewString() + `","reviewer_name":" Программный комитет ","score":8,` +
		`"summary":" Хорошо структурированный доклад ","strengths":"  ","weaknesses":"Мало данных",` +
		`"recommendation":"minor_revision","is_visible_to_author":false}`
	serve := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	rec := serve("POST", path, body)
	var created reviewDTO
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM application_svc.outbox_events WHERE aggregate_id=$1`, created.ID)
	})
	if created.Summary != "Хорошо структурированный доклад" || created.ReviewerName != "Программный комитет" || created.Strengths != nil {
		t.Fatalf("review text was not normalized: %+v", created)
	}
	if rec := serve("POST", path, body); rec.Code != 409 {
		t.Fatalf("duplicate review HTTP %d", rec.Code)
	}
	rec = serve("GET", path, "")
	var listed internalReviewsResp
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &listed) != nil {
		t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
	}
	if listed.Application.ID != appID.String() || len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("hidden review missing from console listing: %+v", listed)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM application_svc.outbox_events WHERE aggregate_id=$1`, created.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("outbox events=%d error=%v", events, err)
	}
	if rec := serve("GET", "/internal/applications/"+uuid.NewString()+"/reviews", ""); rec.Code != 404 {
		t.Fatalf("missing application HTTP %d", rec.Code)
	}
}
