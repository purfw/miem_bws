package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestEventRequestCorrelation(t *testing.T) {
	payload, requestID, err := eventPayload([]byte(`{"_request_id":"request-123","_traceparent":"00-0123456789abcdef0123456789abcdef-0123456789abcdef-01","_tracestate":"test=1","conference_id":"test"}`))
	if err != nil || requestID != "request-123" {
		t.Fatalf("request=%q err=%v", requestID, err)
	}
	var result map[string]string
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result["conference_id"] != "test" {
		t.Fatalf("business payload changed: %s", payload)
	}
}

type fakeTx struct {
	pgx.Tx
	fail    map[string]error
	applied []string
	pending []string
}

func (f *fakeTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	for match, err := range f.fail {
		if strings.Contains(sql, match) {
			return pgconn.CommandTag{}, err
		}
	}
	f.pending = append(f.pending, sql)
	return pgconn.CommandTag{}, nil
}
func (f *fakeTx) Commit(context.Context) error {
	f.applied = append(f.applied, f.pending...)
	f.pending = nil
	return nil
}
func (f *fakeTx) Rollback(context.Context) error { f.pending = nil; return nil }

func TestProcessIsolatesBadRows(t *testing.T) {
	w := New(nil, nil, Config{Schema: "test_svc"})
	tx := &fakeTx{fail: map[string]error{"status='published'": errors.New("update failed")}}
	var ids []string
	publish := func(_ string, _ []byte, msgID string) error {
		ids = append(ids, msgID)
		return nil
	}
	ctx := context.Background()
	if w.process(ctx, tx, row{ID: "bad", EventType: "user.test", Payload: []byte(`[1]`)}, publish) {
		t.Fatal("malformed payload published")
	}
	if len(ids) != 0 || len(tx.applied) != 1 || !strings.Contains(tx.applied[0], "status='failed'") {
		t.Fatalf("malformed row not parked: ids=%v applied=%v", ids, tx.applied)
	}
	if w.process(ctx, tx, row{ID: "e1", EventType: "user.test", Payload: []byte(`{}`)}, publish) {
		t.Fatal("unrecorded publish counted")
	}
	if len(ids) != 1 || ids[0] != "e1" {
		t.Fatalf("publish must carry the event id as message id: %v", ids)
	}
	delete(tx.fail, "status='published'")
	if !w.process(ctx, tx, row{ID: "e2", EventType: "user.test", Payload: []byte(`{}`)}, publish) {
		t.Fatal("row after failed update not processed")
	}
	if w.process(ctx, tx, row{ID: "e3", EventType: "user.test", Payload: []byte(`{}`), RetryCount: maxRetries}, func(string, []byte, string) error {
		return errors.New("broker down")
	}) {
		t.Fatal("failed publish counted")
	}
	if !strings.Contains(tx.applied[len(tx.applied)-1], "status='failed'") {
		t.Fatalf("exhausted row not parked: %v", tx.applied)
	}
}
