package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/confhub/identity-svc/internal/repository"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestVerificationRateError(t *testing.T) {
	for _, item := range []struct {
		wait    time.Duration
		seconds int
	}{{0, 1}, {time.Millisecond, 1}, {time.Second + time.Millisecond, 2}, {time.Minute, 60}} {
		err := &VerificationRateError{RetryAfter: item.wait}
		if err.RetryAfterSeconds() != item.seconds || !errors.Is(err, httputil.ErrTooMany) || err.ProblemCode() != "VERIFICATION_RATE_LIMITED" {
			t.Fatalf("invalid cooldown: %v", item)
		}
	}
}

func TestVerificationResendRecipientRateAndConcurrency(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, other} {
		if _, err := pool.Exec(ctx, `INSERT INTO identity_svc.users(id,email,password_hash,first_name,last_name) VALUES($1,$2,'test-hash','Test','User')`, id, id.String()+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	auth := &Auth{users: repository.NewUsers(pool)}
	var success, limited atomic.Int32
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			err := auth.ResendVerification(ctx, owner)
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, httputil.ErrTooMany) {
				limited.Add(1)
			} else {
				t.Errorf("resend failed: %v", err)
			}
		})
	}
	group.Wait()
	if success.Load() != 1 || limited.Load() != 11 {
		t.Fatalf("success=%d limited=%d", success.Load(), limited.Load())
	}
	var recipient string
	var tokens int
	if err := pool.QueryRow(ctx, `SELECT payload->>'user_email' FROM identity_svc.outbox_events WHERE aggregate_id=$1 AND event_type='user.verification_requested'`, owner).Scan(&recipient); err != nil {
		t.Fatal(err)
	}
	if recipient != owner.String()+"@example.test" {
		t.Fatal("verification sent to another recipient")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity_svc.email_verification_tokens WHERE user_id=$1`, other).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatal("another account was changed")
	}
	if _, err := pool.Exec(ctx, `UPDATE identity_svc.email_verification_tokens SET created_at=now()-interval '2 minutes' WHERE user_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if err := auth.ResendVerification(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE identity_svc.email_verification_tokens SET created_at=now()-interval '2 minutes' WHERE user_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if err := auth.ResendVerification(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE identity_svc.email_verification_tokens SET created_at=now()-interval '2 minutes' WHERE user_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	var limit *VerificationRateError
	if err := auth.ResendVerification(ctx, owner); !errors.As(err, &limit) || limit.RetryAfterSeconds() < 3400 {
		t.Fatal("hourly limit missing")
	}
	if _, err := pool.Exec(ctx, `UPDATE identity_svc.users SET email_verified=true WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if err := auth.ResendVerification(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := auth.ResendVerification(ctx, uuid.New()); !errors.Is(err, httputil.ErrUnauthorized) {
		t.Fatal("missing user accepted")
	}
}
