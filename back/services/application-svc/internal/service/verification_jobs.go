package service

import (
	"context"
	"errors"
	"github.com/confhub/application-svc/internal/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"time"
)

func (s *Applications) RunVerificationRoles(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 20; i++ {
			processed, err := s.applyVerificationRole(ctx)
			if err != nil {
				slog.Error("verification role delivery", "err", err)
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

func (s *Applications) applyVerificationRole(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())
	var verificationID, userID uuid.UUID
	var reviewer *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT j.verification_id,v.user_id,v.reviewed_by
		FROM application_svc.verification_role_jobs j JOIN application_svc.organizer_verifications v ON v.id=j.verification_id
		WHERE j.completed_at IS NULL AND j.next_attempt_at<=now() AND v.status='approved'
		ORDER BY j.next_attempt_at FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(&verificationID, &userID, &reviewer)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var actor uuid.UUID
	if reviewer != nil {
		actor = *reviewer
	}
	if err := s.ident.SetRole(ctx, userID, "verified_organizer", actor); err != nil {
		if errors.Is(err, client.ErrUserMissing) {
			if _, updateErr := tx.Exec(ctx, `UPDATE application_svc.verification_role_jobs SET completed_at=now(),last_error='user deleted' WHERE verification_id=$1`, verificationID); updateErr != nil {
				return true, updateErr
			}
			return true, tx.Commit(ctx)
		}
		message := err.Error()
		if len(message) > 500 {
			message = message[:500]
		}
		if _, updateErr := tx.Exec(ctx, `UPDATE application_svc.verification_role_jobs SET attempts=attempts+1,last_error=$2,next_attempt_at=now()+interval '30 seconds' WHERE verification_id=$1`, verificationID, message); updateErr != nil {
			return true, updateErr
		}
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE application_svc.verification_role_jobs SET completed_at=now(),last_error=NULL WHERE verification_id=$1`, verificationID); err != nil {
		return true, err
	}
	if _, err := tx.Exec(ctx, `UPDATE application_svc.organizer_verifications SET role_applied=true,updated_at=now() WHERE id=$1`, verificationID); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
