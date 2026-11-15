package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/confhub/engagement-svc/internal/audit"
	"github.com/confhub/engagement-svc/internal/mailer"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"testing"
)

func TestEventTransactionRollbackAndDeduplication(t *testing.T) {
	pool := testutil.Database(t)
	m, err := mailer.New(mailer.Config{From: "from@example.test"}, pool, "../../templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	c := New(pool, m, nil, "https://example.test")
	id := uuid.New()
	body, _ := json.Marshal(map[string]any{"event_id": id, "event_type": "test.transaction", "payload": map[string]any{"reset_token": "must-not-be-audited", "status": "test"}})
	message := &nats.Msg{Subject: "test.transaction", Data: body}
	sentinel := errors.New("simulated crash before commit")
	writes := func(ctx context.Context, env envelope) error {
		if err := c.auditTx(ctx, audit.Entry{EventID: env.EventID, EventType: env.EventType, Payload: json.RawMessage(env.Payload)}); err != nil {
			return err
		}
		return m.Enqueue(ctx, "to@example.test", "subject", "body", []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR"), nil, "test.transaction", nil)
	}
	if err := c.handle(context.Background(), func(ctx context.Context, env envelope) error {
		if err := writes(ctx, env); err != nil {
			return err
		}
		return sentinel
	})(message); !errors.Is(err, sentinel) {
		t.Fatalf("rollback error %v", err)
	}
	for _, table := range []string{"processed_events", "audit_log"} {
		var count int
		if err := pool.QueryRow(context.Background(), fmt.Sprintf("SELECT count(*) FROM engagement_svc.%s WHERE event_id=$1", table), id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s persisted before commit", table)
		}
	}
	var queued int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM engagement_svc.notifications WHERE type='test.transaction'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatal("notification persisted before event commit")
	}
	handler := c.handle(context.Background(), writes)
	if err := handler(message); err != nil {
		t.Fatal(err)
	}
	if err := handler(message); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM engagement_svc.notifications WHERE type='test.transaction'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("notification count=%d", count)
	}
	var payload string
	if err := pool.QueryRow(context.Background(), `SELECT payload::text FROM engagement_svc.audit_log WHERE event_id=$1`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"status": "test"}` {
		t.Fatalf("unsafe audit %s", payload)
	}
}
