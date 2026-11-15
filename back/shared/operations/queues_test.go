package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/confhub/shared/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestRetryIsAtomicAndCannotReplaySuccessfulJobs(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	id, actor := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO identity_svc.outbox_events(id,event_type,payload,status,retry_count) VALUES ($1,'user.test','{}','failed',5)`, id); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	New(pool, "identity_svc", nil).Routes(router)
	for _, expected := range []int{204, 409} {
		req := httptest.NewRequest("POST", "/operations/queues/outbox/"+id.String()+"/retry", strings.NewReader(`{"changed_by":"`+actor.String()+`"}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != expected {
			t.Fatalf("retry=%d want=%d body=%s", response.Code, expected, response.Body.String())
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity_svc.outbox_events WHERE event_type='user.admin_queue_retried' AND aggregate_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit=%d error=%v", count, err)
	}
	notification := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO engagement_svc.notifications(id,type,recipient,status,attempts) VALUES ($1,'test','test@example.test','dead',10)`, notification); err != nil {
		t.Fatal(err)
	}
	router = chi.NewRouter()
	New(pool, "engagement_svc", func(context.Context, pgx.Tx, uuid.UUID, string, uuid.UUID, string) error {
		return errors.New("audit unavailable")
	}).Routes(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/operations/queues/notifications/"+notification.String()+"/retry", strings.NewReader(`{"changed_by":"`+actor.String()+`"}`)))
	if response.Code != 500 {
		t.Fatalf("failed audit=%d", response.Code)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM engagement_svc.notifications WHERE id=$1`, notification).Scan(&status); err != nil || status != "dead" {
		t.Fatalf("retry survived rollback: %s, %v", status, err)
	}
}

func TestEventReplayIsAuditedAndPayloadStaysPrivate(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	id, actor := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.event_failures(id,consumer,stream_sequence,subject,payload,status,attempts) VALUES ($1,'test-operations',123,'user.test',$2,'dead',10)`, id, []byte("private-event-payload")); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	New(pool, "conference_svc", nil).Routes(r)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/operations/queues", nil))
	if response.Code != 200 || strings.Contains(response.Body.String(), "private-event-payload") {
		t.Fatal("queue exposed event payload")
	}
	var body struct {
		Data []Item `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range body.Data {
		if item.ID == id && item.Queue == "events" {
			found = true
		}
	}
	if !found {
		t.Fatal("dead event missing from queue")
	}
	for _, status := range []int{204, 409} {
		response = httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("POST", "/operations/queues/events/"+id.String()+"/retry", strings.NewReader(`{"changed_by":"`+actor.String()+`"}`)))
		if response.Code != status {
			t.Fatalf("retry=%d want=%d", response.Code, status)
		}
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conference_svc.outbox_events WHERE event_type='user.admin_queue_retried' AND aggregate_id=$1`, id).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
}
