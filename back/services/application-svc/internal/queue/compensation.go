// Package queue holds NATS subscribers for cross-service compensation.
package queue

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/natsx"
	"github.com/confhub/shared/telemetry"

	"github.com/confhub/application-svc/internal/service"
)

const compensationGroup = "application-compensation"

// SubscribeConferenceCancellation withdraws in-flight applications idempotently.
func SubscribeConferenceCancellation(ctx context.Context, nc *nats.Conn, svc *service.Applications, failures ...*natsx.FailureStore) error {
	err := natsx.SubscribeDurable(nc, sharedevents.SubjectConferenceStatus, compensationGroup, func(m *nats.Msg) error {
		var env struct {
			TraceParent string                               `json:"traceparent"`
			TraceState  string                               `json:"tracestate"`
			Payload     sharedevents.ConferenceStatusChanged `json:"payload"`
		}
		if err := json.Unmarshal(m.Data, &env); err != nil {
			slog.Warn("compensation: bad event", "err", err)
			return natsx.Permanent(err)
		}
		if env.Payload.NewStatus != "cancelled" {
			return nil
		}
		eventCtx, span := telemetry.Consumer(ctx, "application-compensation", env.TraceParent, env.TraceState)
		defer span.End()
		if err := svc.HandleConferenceCancelled(eventCtx, env.Payload.ConferenceID, env.Payload.Title); err != nil {
			return err
		}
		return nil
	}, failures...)
	return err
}
