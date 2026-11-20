package support

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/testutil"
)

func adminRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Route("/internal", h.AdminRoutes)
	return r
}

func serve(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestSupportAdminValidationPrecedesDatabase(t *testing.T) {
	r := adminRouter(New(nil, nil, ""))
	actor := uuid.NewString()
	for _, tc := range []struct {
		name, method, path, body, field string
	}{
		{"unknown status filter", "GET", "/internal/support/messages?status=resolved", "", "status"},
		{"page below range", "GET", "/internal/support/messages?page=0", "", "page"},
		{"page size above range", "GET", "/internal/support/messages?per_page=201", "", "per_page"},
		{"invalid id", "PATCH", "/internal/support/messages/nope", `{"status":"closed","changed_by":"` + actor + `"}`, "id"},
		{"unknown status", "PATCH", "/internal/support/messages/" + uuid.NewString(), `{"status":"resolved","changed_by":"` + actor + `"}`, "status"},
		{"missing actor", "PATCH", "/internal/support/messages/" + uuid.NewString(), `{"status":"closed"}`, "changed_by"},
		{"nil actor", "PATCH", "/internal/support/messages/" + uuid.NewString(), `{"status":"closed","changed_by":"` + uuid.Nil.String() + `"}`, "changed_by"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(r, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
				t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSupportAdminInboxAndAuditedStatusChange(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	r := adminRouter(New(pool, nil, ""))
	sender := uuid.NewString() + "@example.test"
	var older, newer uuid.UUID
	for _, target := range []*uuid.UUID{&older, &newer} {
		if err := pool.QueryRow(ctx, `INSERT INTO engagement_svc.support_messages
			(name, email, topic, subject, message, created_at)
			VALUES ('Test Sender', $1, 'bug', 'Тема', 'An integration test support message',
			        now() - CASE WHEN $2 THEN interval '0' ELSE interval '1 minute' END)
			RETURNING id`, sender, target == &newer).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM engagement_svc.support_messages WHERE email=$1`, sender)
		_, _ = pool.Exec(ctx, `DELETE FROM engagement_svc.audit_log WHERE entity_id = ANY($1)`, []uuid.UUID{older, newer})
	})

	actor := uuid.New()
	rec := serve(r, "PATCH", "/internal/support/messages/"+older.String(),
		`{"status":"closed","changed_by":"`+actor.String()+`"}`)
	var changed messageDTO
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &changed) != nil || changed.Status != "closed" || changed.TopicLabel != "Ошибка на сайте" {
		t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
	}
	var audited int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.audit_log
		WHERE event_type='support.status_changed' AND entity_id=$1 AND actor_id=$2`, older, actor).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 1 {
		t.Fatalf("audit entries=%d", audited)
	}
	if rec := serve(r, "PATCH", "/internal/support/messages/"+uuid.NewString(),
		`{"status":"closed","changed_by":"`+actor.String()+`"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing message HTTP %d", rec.Code)
	}

	found := map[string]string{}
	var order []string
	for page := 1; page <= 100 && len(found) < 2; page++ {
		rec := serve(r, "GET", "/internal/support/messages?per_page=200&page="+strconv.Itoa(page), "")
		var out listResp
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("HTTP %d %s", rec.Code, rec.Body.String())
		}
		for _, m := range out.Data {
			if m.Email == sender {
				found[m.ID] = m.Status
				order = append(order, m.ID)
			}
		}
		if !out.HasMore {
			break
		}
	}
	if len(order) != 2 || order[0] != newer.String() || found[older.String()] != "closed" {
		t.Fatalf("unexpected inbox order or status: %v %v", order, found)
	}
	rec = serve(r, "GET", "/internal/support/messages?status=closed&per_page=200", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), newer.String()) {
		t.Fatalf("status filter leaked new message: HTTP %d", rec.Code)
	}
}
