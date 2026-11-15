package mailer

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func smtpFixture(t *testing.T, reject bool) Config {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = fmt.Fprint(conn, "220 test SMTP\r\n")
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "DATA"):
						_, _ = fmt.Fprint(conn, "354 send message\r\n")
						for {
							line, err = reader.ReadString('\n')
							if err != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
						}
						_, _ = fmt.Fprint(conn, "250 queued\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = fmt.Fprint(conn, "221 bye\r\n")
						return
					case strings.HasPrefix(line, "RCPT") && reject:
						_, _ = fmt.Fprint(conn, "451 temporarily unavailable\r\n")
					default:
						_, _ = fmt.Fprint(conn, "250 ok\r\n")
					}
				}
			}()
		}
	}()
	address := listener.Addr().(*net.TCPAddr)
	return Config{Host: "127.0.0.1", Port: address.Port, From: "from@example.test", TLSMode: "none"}
}

func TestQueuePersistsTraceAndSuppressesDuplicateEvent(t *testing.T) {
	pool := testutil.Database(t)
	m := &Mailer{pool: pool, cfg: Config{From: "from@example.test"}}
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	ctx = WithTransaction(ctx, tx, uuid.New())
	kind := "test.trace." + uuid.NewString()
	for i := 0; i < 2; i++ {
		if err := m.Enqueue(ctx, "to@example.test", "subject", "body", nil, nil, kind, nil); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var parent string
	if err := tx.QueryRow(ctx, `SELECT count(*),max(trace_parent) FROM engagement_svc.notifications WHERE type=$1`, kind).Scan(&count, &parent); err != nil || count != 1 || !strings.Contains(parent, sc.TraceID().String()) {
		t.Fatalf("count=%d parent=%s err=%v", count, parent, err)
	}
}

func TestQueueSuccessFailureAndStaleLastAttempt(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	for _, reject := range []bool{false, true} {
		m := &Mailer{pool: pool, cfg: smtpFixture(t, reject)}
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO engagement_svc.notifications(id,type,recipient,status,attempts,next_attempt_at) VALUES ($1,'test.smtp','to@example.test','pending',0,'1970-01-01')`, id); err != nil {
			t.Fatal(err)
		}
		if processed, err := m.retryOne(ctx); err != nil || !processed {
			t.Fatalf("processed=%v err=%v", processed, err)
		}
		var status string
		var attempts int
		if err := pool.QueryRow(ctx, `SELECT status,attempts FROM engagement_svc.notifications WHERE id=$1`, id).Scan(&status, &attempts); err != nil {
			t.Fatal(err)
		}
		want := "sent"
		if reject {
			want = "failed"
		}
		if status != want || attempts != 1 {
			t.Fatalf("status=%s attempts=%d", status, attempts)
		}
		if _, err := pool.Exec(ctx, `UPDATE engagement_svc.notifications SET status='sending',attempts=10,locked_at=now()-interval '2 minutes' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := m.retryOne(ctx); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT status FROM engagement_svc.notifications WHERE id=$1`, id).Scan(&status); err != nil || status != "dead" {
			t.Fatalf("last attempt remained stuck: %s %v", status, err)
		}
	}
}

func TestQueueCancelsInactiveSubscription(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	user, conference, subscription, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO engagement_svc.subscriptions(id,user_id,user_email,conference_id,notify_on,channels,unsubscribe_token_hash,active) VALUES ($1,$2,'to@example.test',$3,'{conference_cancelled}','{email}',$4,false)`, subscription, user, conference, []byte(uuid.NewString())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO engagement_svc.notifications(id,type,recipient,status,subscription_id,next_attempt_at) VALUES ($1,'test.inactive','to@example.test','pending',$2,'1970-01-01')`, id, subscription); err != nil {
		t.Fatal(err)
	}
	m := &Mailer{pool: pool, cfg: Config{From: "from@example.test"}}
	if processed, err := m.retryOne(ctx); err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM engagement_svc.notifications WHERE id=$1`, id).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("inactive subscription delivery: %s %v", status, err)
	}
}
