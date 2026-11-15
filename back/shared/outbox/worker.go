// Package outbox publishes pending database events to NATS.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/natsx"
	"github.com/confhub/shared/telemetry"
)

const (
	defaultBatch = 50
	defaultPoll  = 500 * time.Millisecond
	maxRetries   = 5
)

type Config struct {
	Schema  string
	Batch   int
	Poll    time.Duration
	Subject func(eventType string) string
}

type Worker struct {
	pool    *pgxpool.Pool
	nc      *nats.Conn
	js      nats.JetStreamContext
	cfg     Config
	subject func(string) string
}

func New(pool *pgxpool.Pool, nc *nats.Conn, cfg Config) *Worker {
	if cfg.Batch == 0 {
		cfg.Batch = defaultBatch
	}
	if cfg.Poll == 0 {
		cfg.Poll = defaultPoll
	}
	if cfg.Subject == nil {
		cfg.Subject = func(et string) string { return et }
	}
	return &Worker{pool: pool, nc: nc, cfg: cfg, subject: cfg.Subject}
}

func (w *Worker) Run(ctx context.Context) error {
	for w.js == nil {
		js, err := natsx.EnsureEventStream(w.nc)
		if err == nil {
			w.js = js
			break
		}
		slog.ErrorContext(ctx, "ensure event stream", "err", err, "schema", w.cfg.Schema)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	t := time.NewTicker(w.cfg.Poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			n, err := w.tick(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "outbox tick", "err", err, "schema", w.cfg.Schema)
				continue
			}
			if n > 0 {
				slog.DebugContext(ctx, "outbox published", "n", n, "schema", w.cfg.Schema)
			}
		}
	}
}

func (w *Worker) tick(ctx context.Context) (int, error) {
	if !w.nc.IsConnected() {
		return 0, nats.ErrDisconnected
	}
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, fmt.Sprintf(`
		SELECT id, event_type, payload, retry_count
		  FROM %s.outbox_events
		 WHERE status = 'pending'
		   AND (next_retry_at IS NULL OR next_retry_at <= now())
		 ORDER BY created_at
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`, w.cfg.Schema), w.cfg.Batch)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}

	type row struct {
		ID         string
		EventType  string
		Payload    []byte
		RetryCount int
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.EventType, &r.Payload, &r.RetryCount); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(batch) == 0 {
		return 0, tx.Commit(ctx)
	}

	published := 0
	for _, r := range batch {
		subj := w.subject(r.EventType)
		payload, requestID, err := eventPayload(r.Payload)
		if err != nil {
			return published, err
		}
		envelope, _ := json.Marshal(map[string]any{
			"event_id":    r.ID,
			"event_type":  r.EventType,
			"payload":     json.RawMessage(payload),
			"request_id":  requestID,
			"traceparent": traceField(r.Payload, "_traceparent"),
			"tracestate":  traceField(r.Payload, "_tracestate"),
			"published":   time.Now().UTC().Format(time.RFC3339Nano),
		})
		if _, err := w.js.Publish(subj, envelope); err != nil {
			retry := r.RetryCount + 1
			if retry >= maxRetries {
				_, _ = tx.Exec(ctx,
					fmt.Sprintf(`UPDATE %s.outbox_events SET status='failed', retry_count=$1, last_error=$2 WHERE id=$3`, w.cfg.Schema),
					retry, err.Error(), r.ID)
				slog.ErrorContext(ctx, "outbox event parked", "id", r.ID, "type", r.EventType, "err", err)
				continue
			}
			backoff := time.Duration(1<<retry) * 5 * time.Second
			_, _ = tx.Exec(ctx,
				fmt.Sprintf(`UPDATE %s.outbox_events SET retry_count=$1, last_error=$2, next_retry_at=now()+$3::interval WHERE id=$4`, w.cfg.Schema),
				retry, err.Error(), backoff.String(), r.ID)
			continue
		}
		// JetStream Publish returns only after the server persisted the message.
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`UPDATE %s.outbox_events SET status='published', published_at=now() WHERE id=$1`, w.cfg.Schema),
			r.ID); err != nil {
			return published, fmt.Errorf("mark published: %w", err)
		}
		published++
		slog.InfoContext(ctx, "outbox published", "event_id", r.ID, "event_type", r.EventType, "request_id", requestID)
	}

	return published, tx.Commit(ctx)
}

func Insert(ctx context.Context, tx pgx.Tx, schema, eventType, aggregateType, aggregateID string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if requestID := httputil.RequestIDFromContext(ctx); requestID != "" || len(telemetry.Carrier(ctx)) > 0 {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(b, &object); err != nil {
			return err
		}
		if object != nil {
			object["_request_id"], _ = json.Marshal(requestID)
			for key, value := range telemetry.Carrier(ctx) {
				object["_"+key], _ = json.Marshal(value)
			}
			b, err = json.Marshal(object)
			if err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.outbox_events (event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, $3, $4)`, schema),
		eventType, aggregateType, aggregateID, b)
	return err
}

func eventPayload(raw []byte) ([]byte, string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, "", err
	}
	var requestID string
	if id, ok := object["_request_id"]; ok {
		if err := json.Unmarshal(id, &requestID); err != nil {
			return nil, "", err
		}
		delete(object, "_request_id")
	}
	delete(object, "_traceparent")
	delete(object, "_tracestate")
	payload, err := json.Marshal(object)
	return payload, requestID, err
}

func traceField(raw []byte, key string) string {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	var value string
	_ = json.Unmarshal(object[key], &value)
	return value
}
