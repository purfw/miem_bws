package projection

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/natsx"
	"github.com/confhub/shared/telemetry"

	"github.com/confhub/conference-svc/internal/repository"
)

const queueGroup = "conference-counters"

type Counters struct {
	repo *repository.Conferences
}

func NewCounters(repo *repository.Conferences) *Counters { return &Counters{repo: repo} }

type envelope struct {
	TraceParent string          `json:"traceparent"`
	TraceState  string          `json:"tracestate"`
	EventID     uuid.UUID       `json:"event_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
}

func (c *Counters) Subscribe(ctx context.Context, nc *nats.Conn, failures ...*natsx.FailureStore) error {
	subs := []struct {
		subject string
		fn      func(context.Context, envelope) error
	}{
		{sharedevents.SubjectAppSubmitted, c.onSubmitted},
		{sharedevents.SubjectAppStatusChanged, c.onStatusChanged},
		{sharedevents.SubjectAppModerated, c.onModerated},
	}
	for _, s := range subs {
		fn := s.fn
		subject := s.subject
		durable := queueGroup + "-" + strings.ReplaceAll(subject, ".", "-")
		if err := natsx.SubscribeDurable(nc, subject, durable, func(m *nats.Msg) error {
			var env envelope
			if err := json.Unmarshal(m.Data, &env); err != nil {
				slog.Warn("counters: bad envelope", "subject", subject, "err", err)
				return err
			}
			eventCtx, span := telemetry.Consumer(ctx, durable, env.TraceParent, env.TraceState)
			defer span.End()
			if err := fn(eventCtx, env); err != nil {
				return fmt.Errorf("apply counter event %s: %w", env.EventID, err)
			}
			return nil
		}, failures...); err != nil {
			return err
		}
	}
	return nil
}

func (c *Counters) apply(ctx context.Context, env envelope, confID uuid.UUID, d repository.CounterDelta) error {
	if confID == uuid.Nil || d == (repository.CounterDelta{}) {
		return nil
	}
	tx, err := c.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	claimed, err := c.repo.ClaimEvent(ctx, tx, env.EventID, env.EventType)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if err := c.repo.ApplyCounterDelta(ctx, tx, confID, d); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *Counters) onSubmitted(ctx context.Context, env envelope) error {
	var p sharedevents.ApplicationSubmitted
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	return c.apply(ctx, env, p.ConferenceID, repository.CounterDelta{Participants: 1})
}

func (c *Counters) onStatusChanged(ctx context.Context, env envelope) error {
	var p sharedevents.ApplicationStatusChanged
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if p.NewStatus != "withdrawn" {
		return nil
	}
	return c.apply(ctx, env, p.ConferenceID, repository.CounterDelta{Participants: -1})
}

func (c *Counters) onModerated(ctx context.Context, env envelope) error {
	var p sharedevents.ApplicationModerated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	var d repository.CounterDelta
	switch p.NewStatus {
	case "accepted":
		d.Accepted = 1
	case "rejected":
		d.Rejected = 1
	case "withdrawn":
		d.Participants = -1
	default:
		return nil
	}
	return c.apply(ctx, env, p.ConferenceID, d)
}
