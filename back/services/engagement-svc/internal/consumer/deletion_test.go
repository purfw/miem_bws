package consumer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/confhub/shared/events"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

func TestDeletionCancelsSubscriptionsAndQueuedMail(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	user, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other} {
		if _, err := pool.Exec(ctx, `INSERT INTO engagement_svc.subscriptions(user_id,user_email,conference_id,notify_on,channels,unsubscribe_token_hash) VALUES($1,'test@example.test',$2,ARRAY['session_updated'],ARRAY['email'],$3)`, id, uuid.New(), []byte(id.String())); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"pending", "failed", "sent"} {
			if _, err := pool.Exec(ctx, `INSERT INTO engagement_svc.notifications(user_id,type,recipient,status) VALUES($1,'test.deletion','test@example.test',$2)`, id, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	body, _ := json.Marshal(map[string]any{"event_id": uuid.New(), "event_type": events.SubjectUserDeleted, "payload": map[string]any{"user_id": user}})
	consumer := New(pool, nil, nil, "https://example.test")
	handler := consumer.handle(ctx, consumer.onUserDeleted)
	message := &nats.Msg{Subject: events.SubjectUserDeleted, Data: body}
	for range 2 {
		if err := handler(message); err != nil {
			t.Fatal(err)
		}
	}
	var active, cancelled, otherActive, otherCancelled int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.subscriptions WHERE user_id=$1 AND active`, user).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.notifications WHERE user_id=$1 AND status='cancelled'`, user).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.subscriptions WHERE user_id=$1 AND active`, other).Scan(&otherActive); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.notifications WHERE user_id=$1 AND status='cancelled'`, other).Scan(&otherCancelled); err != nil {
		t.Fatal(err)
	}
	if active != 0 || cancelled != 2 || otherActive != 1 || otherCancelled != 0 {
		t.Fatalf("active=%d cancelled=%d other active=%d other cancelled=%d", active, cancelled, otherActive, otherCancelled)
	}
}
