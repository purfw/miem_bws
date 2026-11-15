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

type OrganizerAccess struct{ repo *repository.Conferences }

func NewOrganizerAccess(repo *repository.Conferences) *OrganizerAccess {
	return &OrganizerAccess{repo: repo}
}

func (p *OrganizerAccess) Subscribe(ctx context.Context, nc *nats.Conn, failures ...*natsx.FailureStore) error {
	for _, subject := range []string{sharedevents.SubjectUserRoleChanged, sharedevents.SubjectUserActiveChanged, sharedevents.SubjectUserDeleted} {
		subject := subject
		durable := "conference-organizer-access-" + strings.ReplaceAll(subject, ".", "-")
		if err := natsx.SubscribeDurable(nc, subject, durable, func(msg *nats.Msg) error {
			var env envelope
			if err := json.Unmarshal(msg.Data, &env); err != nil {
				slog.Warn("organizer access: bad envelope", "subject", subject, "err", err)
				return err
			}
			eventCtx, span := telemetry.Consumer(ctx, durable, env.TraceParent, env.TraceState)
			defer span.End()
			if err := p.apply(eventCtx, env); err != nil {
				return fmt.Errorf("apply organizer event %s: %w", env.EventID, err)
			}
			return nil
		}, failures...); err != nil {
			return err
		}
	}
	return nil
}

func (p *OrganizerAccess) apply(ctx context.Context, env envelope) error {
	var userID uuid.UUID
	allowed := false
	switch env.EventType {
	case sharedevents.SubjectUserRoleChanged:
		var payload sharedevents.UserRoleChanged
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return err
		}
		userID = payload.UserID
		allowed = payload.NewRole == "verified_organizer" || payload.NewRole == "admin"
	case sharedevents.SubjectUserActiveChanged:
		var payload sharedevents.UserActiveChanged
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return err
		}
		userID = payload.UserID
		allowed = payload.Active && (payload.Role == "verified_organizer" || payload.Role == "admin")
	case sharedevents.SubjectUserDeleted:
		var payload struct {
			UserID uuid.UUID `json:"user_id"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return err
		}
		userID = payload.UserID
	default:
		return nil
	}
	if userID == uuid.Nil {
		return nil
	}
	tx, err := p.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	claimed, err := p.repo.ClaimEvent(ctx, tx, env.EventID, env.EventType)
	if err != nil || !claimed {
		return err
	}
	if err := p.repo.SetPublicationAllowedForCreator(ctx, tx, userID, allowed); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
