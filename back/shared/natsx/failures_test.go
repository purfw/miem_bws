package natsx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

func TestDeliveryAcknowledgmentRequiresSuccessOrDurableSave(t *testing.T) {
	failed := errors.New("handler unavailable")
	storageFailed := errors.New("database unavailable")
	for _, tc := range []struct {
		name                string
		attempts            uint64
		handlerErr, saveErr error
		wantSave            bool
		want                error
	}{
		{"success", 1, nil, nil, false, nil},
		{"retry", 9, failed, nil, false, failed},
		{"quarantine", 10, failed, nil, true, nil},
		{"keep_unacknowledged", 10, failed, storageFailed, true, storageFailed},
		{"late_quarantine", 50, failed, nil, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			err := deliveryResult(func() error { return tc.handlerErr }, tc.attempts, func() error { called = true; return tc.saveErr })
			if called != tc.wantSave || !errors.Is(err, tc.want) {
				t.Fatalf("saved=%v err=%v", called, err)
			}
		})
	}
	if err := deliveryResult(func() error { return failed }, 100, nil); !errors.Is(err, failed) {
		t.Fatal("failure dropped without store")
	}
}

func TestFailureQuarantineAndTargetedReplay(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	store := NewFailureStore(pool, "engagement_svc")
	consumer := "test-" + uuid.NewString()
	msg := &nats.Msg{Sub: &nats.Subscription{}, Subject: "user.test", Data: []byte(`{"event_id":"test"}`), Reply: "$JS.ACK.CONFHUB_EVENTS." + consumer + ".10.123.123.1700000000000000000.0"}
	for i := 0; i < 2; i++ {
		if err := store.save(ctx, consumer, msg, 10); err != nil {
			t.Fatal(err)
		}
	}
	var id uuid.UUID
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.event_failures WHERE consumer=$1`, consumer).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedup=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM engagement_svc.event_failures WHERE consumer=$1`, consumer).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := func(m *nats.Msg) error {
		if m.Subject != msg.Subject || string(m.Data) != string(msg.Data) {
			return errors.New("event changed")
		}
		calls.Add(1)
		return nil
	}
	store.register(consumer, handler)
	if done, err := store.replayOne(ctx); done || err != nil {
		t.Fatalf("dead event replayed automatically: %v %v", done, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE engagement_svc.event_failures SET status='pending',attempts=0 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	other := NewFailureStore(pool, "engagement_svc")
	other.register(consumer, handler)
	var group sync.WaitGroup
	for _, instance := range []*FailureStore{store, other} {
		group.Go(func() {
			if _, err := instance.replayOne(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent replay calls=%d", calls.Load())
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM engagement_svc.event_failures WHERE id=$1`, id).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	store.register(consumer, func(*nats.Msg) error { return errors.New("still unavailable") })
	if _, err := pool.Exec(ctx, `UPDATE engagement_svc.event_failures SET status='pending',attempts=9,next_attempt_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.replayOne(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM engagement_svc.event_failures WHERE id=$1`, id).Scan(&status); err != nil || status != "dead" {
		t.Fatalf("exhausted=%s err=%v", status, err)
	}
	if err := store.save(ctx, consumer, &nats.Msg{}, 10); err == nil {
		t.Fatal("missing broker sequence accepted")
	}
}

func TestRejectsUntrustedSchema(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("untrusted SQL schema accepted")
		}
	}()
	_ = NewFailureStore(nil, fmt.Sprintf("%s; DROP SCHEMA public", "engagement_svc"))
}
