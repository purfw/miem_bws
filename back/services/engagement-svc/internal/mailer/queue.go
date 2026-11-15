package mailer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/confhub/shared/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/codes"
)

type transactionKey struct{}
type transaction struct {
	tx      pgx.Tx
	eventID uuid.UUID
}

func WithTransaction(ctx context.Context, tx pgx.Tx, eventID uuid.UUID) context.Context {
	return context.WithValue(ctx, transactionKey{}, transaction{tx: tx, eventID: eventID})
}

func Transaction(ctx context.Context) pgx.Tx {
	value, _ := ctx.Value(transactionKey{}).(transaction)
	return value.tx
}

func (m *Mailer) Enqueue(ctx context.Context, recipient, subject, body string, calendar []byte, userID *uuid.UUID, notifType string, subscriptionID *uuid.UUID) error {
	id := uuid.New()
	wire, err := buildMessage(m.cfg.From, recipient, subject, body, calendar, id)
	if err != nil {
		return err
	}
	value, _ := ctx.Value(transactionKey{}).(transaction)
	var dedupKey *string
	if value.eventID != uuid.Nil {
		digest := sha256.Sum256([]byte(value.eventID.String() + ":" + notifType + ":" + strings.ToLower(recipient)))
		key := hex.EncodeToString(digest[:])
		dedupKey = &key
	}
	query := `INSERT INTO engagement_svc.notifications
		(id,user_id,channel,type,recipient,subject,body,status,message,dedup_key,subscription_id,trace_parent,trace_state)
		VALUES ($1,$2,'email',$3,$4,$5,$6,'pending',$7,$8,$9,$10,$11)
		ON CONFLICT (dedup_key) DO NOTHING`
	carrier := telemetry.Carrier(ctx)
	args := []any{id, userID, notifType, recipient, subject, body, wire, dedupKey, subscriptionID, carrier["traceparent"], carrier["tracestate"]}
	if value.tx != nil {
		_, err = value.tx.Exec(ctx, query, args...)
	} else {
		_, err = m.pool.Exec(ctx, query, args...)
	}
	return err
}

func (m *Mailer) RunRetry(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 100; i++ {
			processed, err := m.retryOne(ctx)
			if err != nil {
				slog.Error("notification retry", "err", err)
				break
			}
			if !processed {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Mailer) retryOne(ctx context.Context) (bool, error) {
	if _, err := m.pool.Exec(ctx, `UPDATE engagement_svc.notifications SET status='dead',locked_at=NULL
		WHERE status='sending' AND attempts>=10 AND locked_at<now()-interval '1 minute'`); err != nil {
		return false, err
	}
	var id uuid.UUID
	var subscriptionID *uuid.UUID
	var recipient, subject, body string
	var wire []byte
	var attempts int
	var parent, state string
	err := m.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT id FROM engagement_svc.notifications WHERE attempts < 10 AND (
			(status IN ('pending','failed') AND next_attempt_at <= now()) OR
			(status='sending' AND locked_at < now() - interval '1 minute'))
		ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE engagement_svc.notifications n SET status='sending',locked_at=now(),attempts=attempts+1
		FROM candidate WHERE n.id=candidate.id
		RETURNING n.id,n.recipient,COALESCE(n.subject,''),COALESCE(n.body,''),n.message,n.attempts,n.subscription_id,n.trace_parent,n.trace_state`).
		Scan(&id, &recipient, &subject, &body, &wire, &attempts, &subscriptionID, &parent, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, span := telemetry.Consumer(ctx, "email-delivery", parent, state)
	defer span.End()
	if subscriptionID != nil {
		var active bool
		err = m.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM engagement_svc.subscriptions WHERE id=$1 AND active)`, subscriptionID).Scan(&active)
		if err != nil {
			return true, err
		}
		if !active {
			_, err = m.pool.Exec(ctx, `UPDATE engagement_svc.notifications SET status='cancelled',locked_at=NULL WHERE id=$1`, id)
			return true, err
		}
	}
	if len(wire) == 0 {
		wire, err = buildMessage(m.cfg.From, recipient, subject, body, nil, id)
	}
	if err == nil {
		err = m.dispatch(ctx, recipient, wire)
	}
	if err != nil {
		span.SetStatus(codes.Error, "SMTP delivery failed")
		_, updateErr := m.pool.Exec(ctx, `UPDATE engagement_svc.notifications SET
			status=CASE WHEN attempts>=10 THEN 'dead' ELSE 'failed' END,last_error=$2,locked_at=NULL,
			next_attempt_at=now()+($3*interval '1 second') WHERE id=$1`, id, safeSMTPError(err), retryDelay(attempts).Seconds())
		return true, updateErr
	}
	_, err = m.pool.Exec(ctx, `UPDATE engagement_svc.notifications SET status='sent',sent_at=now(),last_error=NULL,locked_at=NULL WHERE id=$1`, id)
	return true, err
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(15*(1<<uint(attempt-1))) * time.Second
}

func safeSMTPError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}
