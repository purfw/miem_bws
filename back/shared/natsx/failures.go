package natsx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

const maxHandlerAttempts = 10

type FailureStore struct {
	pool     *pgxpool.Pool
	schema   string
	mu       sync.RWMutex
	handlers map[string]func(*nats.Msg) error
}

func NewFailureStore(pool *pgxpool.Pool, schema string) *FailureStore {
	switch schema {
	case "conference_svc", "application_svc", "engagement_svc":
	default:
		panic("unsupported delivery failure schema")
	}
	return &FailureStore{pool: pool, schema: schema, handlers: make(map[string]func(*nats.Msg) error)}
}

func (s *FailureStore) register(consumer string, handler func(*nats.Msg) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[consumer] = handler
}

func (s *FailureStore) save(ctx context.Context, consumer string, msg *nats.Msg, attempts uint64) error {
	meta, err := msg.Metadata()
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.event_failures
		(consumer,stream_sequence,subject,payload,status,attempts)
		VALUES ($1,$2,$3,$4,'dead',$5) ON CONFLICT (consumer,stream_sequence) DO NOTHING`, s.schema),
		consumer, meta.Sequence.Stream, msg.Subject, msg.Data, attempts)
	return err
}

// ErrPermanent marks a handler failure that no redelivery can fix, such as an
// undecodable payload. Handlers wrap it so the message is quarantined (or
// terminated without a FailureStore) on the first attempt.
var ErrPermanent = errors.New("permanent event failure")

// Permanent wraps err with ErrPermanent.
func Permanent(err error) error {
	return fmt.Errorf("%w: %w", ErrPermanent, err)
}

// A broker message is acknowledged only after processing or durable quarantine.
func deliveryResult(handler func() error, attempts uint64, save func() error) error {
	err := handler()
	if err == nil || save == nil || (attempts < maxHandlerAttempts && !errors.Is(err, ErrPermanent)) {
		return err
	}
	return save()
}

func (s *FailureStore) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 20; i++ {
				done, err := s.replayOne(ctx)
				if err != nil {
					slog.Error("event replay failed", "schema", s.schema, "err", err)
					break
				}
				if !done {
					break
				}
			}
		}
	}
}

func (s *FailureStore) replayOne(ctx context.Context) (bool, error) {
	s.mu.RLock()
	consumers := make([]string, 0, len(s.handlers))
	for name := range s.handlers {
		consumers = append(consumers, name)
	}
	s.mu.RUnlock()
	if len(consumers) == 0 {
		return false, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var id uuid.UUID
	var consumer, subject string
	var payload []byte
	var attempts int
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT id,consumer,subject,payload,attempts FROM %s.event_failures
		WHERE status='pending' AND next_attempt_at<=now() AND consumer=ANY($1)
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, s.schema), consumers).
		Scan(&id, &consumer, &subject, &payload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.mu.RLock()
	handler := s.handlers[consumer]
	s.mu.RUnlock()
	status := "completed"
	if err := handler(&nats.Msg{Subject: subject, Data: payload}); err != nil {
		status = "pending"
		if attempts+1 >= maxHandlerAttempts || errors.Is(err, ErrPermanent) {
			status = "dead"
		}
	}
	delay := time.Duration(1<<min(attempts, 10)) * time.Second
	_, err = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s.event_failures SET status=$2,attempts=attempts+1,
		next_attempt_at=now()+$3::interval,updated_at=now() WHERE id=$1`, s.schema), id, status, delay.String())
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
