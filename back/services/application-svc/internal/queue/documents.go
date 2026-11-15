package queue

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/confhub/application-svc/internal/service"
	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/natsx"
	"github.com/confhub/shared/telemetry"
)

func SubscribeDocumentGeneration(ctx context.Context, nc *nats.Conn, svc *service.Applications, failures ...*natsx.FailureStore) error {
	err := natsx.SubscribeDurable(nc, sharedevents.SubjectConferenceStatus, "document-generation", func(m *nats.Msg) error {
		var env struct {
			TraceParent string                               `json:"traceparent"`
			TraceState  string                               `json:"tracestate"`
			Payload     sharedevents.ConferenceStatusChanged `json:"payload"`
		}
		if err := json.Unmarshal(m.Data, &env); err != nil {
			slog.Warn("documents: bad event", "err", err)
			return err
		}
		if env.Payload.NewStatus != "completed" {
			return nil
		}
		eventCtx, span := telemetry.Consumer(ctx, "document-generation", env.TraceParent, env.TraceState)
		defer span.End()
		if err := svc.GenerateParticipationDocuments(eventCtx, env.Payload.ConferenceID, env.Payload.Title); err != nil {
			return err
		}
		return nil
	}, failures...)
	return err
}
