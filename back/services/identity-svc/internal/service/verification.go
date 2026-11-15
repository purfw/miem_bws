package service

import (
	"context"
	"errors"
	"time"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type VerificationRateError struct{ RetryAfter time.Duration }

func (e *VerificationRateError) Error() string       { return "verification resend rate limit" }
func (e *VerificationRateError) Unwrap() error       { return httputil.ErrTooMany }
func (e *VerificationRateError) ProblemCode() string { return "VERIFICATION_RATE_LIMITED" }
func (e *VerificationRateError) RetryAfterSeconds() int {
	return max(1, int((e.RetryAfter+time.Second-1)/time.Second))
}

func (a *Auth) ResendVerification(ctx context.Context, userID uuid.UUID) error {
	tx, err := a.users.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var email, firstName string
	var verified, active bool
	err = tx.QueryRow(ctx, `SELECT email,first_name,email_verified,is_active FROM identity_svc.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&email, &firstName, &verified, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return httputil.ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if !active {
		return httputil.ErrForbidden
	}
	if verified {
		return tx.Commit(ctx)
	}
	count, oldest, latest, err := a.users.VerificationWindowInTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	now := time.Now()
	if count >= 3 && oldest != nil {
		return &VerificationRateError{RetryAfter: oldest.Add(time.Hour).Sub(now)}
	}
	if latest != nil && latest.Add(time.Minute).After(now) {
		return &VerificationRateError{RetryAfter: latest.Add(time.Minute).Sub(now)}
	}
	token, err := a.users.CreateVerificationTokenInTx(ctx, tx, userID, verifyTokenTTL)
	if err != nil {
		return err
	}
	if err := outbox.Insert(ctx, tx, schemaName, sharedevents.SubjectUserVerificationRequested, "user", userID.String(), sharedevents.UserVerificationRequested{UserID: userID, Email: email, FirstName: firstName, VerificationToken: token.String()}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
