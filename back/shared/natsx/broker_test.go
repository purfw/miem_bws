package natsx

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

func TestBrokerQuarantineSurvivesConsumerRestart(t *testing.T) {
	address := os.Getenv("TEST_NATS_URL")
	if address == "" {
		t.Skip("TEST_NATS_URL is not configured")
	}
	parsed, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(parsed.Hostname(), "confhub-test-nats-") {
		t.Fatal("refusing a non-test broker")
	}
	pool := testutil.Database(t)
	ctx := context.Background()
	nc, err := Connect(address, "failure-integration-test")
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := EnsureEventStream(nc)
	if err != nil {
		t.Fatal(err)
	}
	consumer := "failure-test-" + uuid.NewString()
	subject := "user.failure_test." + uuid.NewString()
	_, err = js.AddConsumer(EventStream, &nats.ConsumerConfig{Durable: consumer, DeliverSubject: nats.NewInbox(), DeliverGroup: consumer, FilterSubject: subject, AckPolicy: nats.AckExplicitPolicy, MaxDeliver: 10, AckWait: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer js.DeleteConsumer(EventStream, consumer)
	store := NewFailureStore(pool, "engagement_svc")
	handler := func(*nats.Msg) error { return errors.New("temporary handler failure") }
	if err := SubscribeDurable(nc, subject, consumer, handler, store); err != nil {
		t.Fatal(err)
	}
	info, err := js.ConsumerInfo(EventStream, consumer)
	if err != nil || info.Config.MaxDeliver != -1 {
		t.Fatalf("unbounded broker retry=%v %v", info, err)
	}
	published, err := js.Publish(subject, []byte(`{"test":true}`))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(65 * time.Second)
	var id uuid.UUID
	for time.Now().Before(deadline) {
		err := pool.QueryRow(ctx, `SELECT id FROM engagement_svc.event_failures WHERE consumer=$1 AND stream_sequence=$2 AND status='dead'`, consumer, published.Sequence).Scan(&id)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if id == uuid.Nil {
		t.Fatal("failed event was not quarantined")
	}
	for time.Now().Before(deadline) {
		info, err = js.ConsumerInfo(EventStream, consumer)
		if err == nil && info.NumAckPending == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || info.NumAckPending != 0 {
		t.Fatal("persisted failure was not acknowledged")
	}
	restarted := NewFailureStore(pool, "engagement_svc")
	restarted.register(consumer, func(msg *nats.Msg) error {
		if msg.Subject != subject || string(msg.Data) != `{"test":true}` {
			return errors.New("replay changed the event")
		}
		return nil
	})
	if _, err := pool.Exec(ctx, `UPDATE engagement_svc.event_failures SET status='pending',attempts=0 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if done, err := restarted.replayOne(ctx); err != nil || !done {
		t.Fatalf("restart replay=%v %v", done, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM engagement_svc.event_failures WHERE id=$1`, id).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("replayed status=%s %v", status, err)
	}
}
