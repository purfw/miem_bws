package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/confhub/shared/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/confhub/application-svc/internal/domain"
)

func (r *Applications) IsTrustedDomain(ctx context.Context, email string) (string, bool, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(email)), "@")
	if len(parts) != 2 {
		return "", false, nil
	}
	domainPart := parts[1]
	var organization string
	err := r.pool.QueryRow(ctx, `SELECT organization FROM application_svc.trusted_domains
		WHERE domain=$1 AND is_active=true`, domainPart).Scan(&organization)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return organization, err == nil, err
}

func (r *Applications) CreateVerification(ctx context.Context, v *domain.OrganizerVerification) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(ctx, `INSERT INTO application_svc.organizer_verifications
		(id,user_id,user_email,user_name,organization,method,status,role_applied)
		VALUES ($1,$2,$3,$4,$5,$6,$7::application_svc.verification_status,false)
		RETURNING created_at,updated_at`, v.ID, v.UserID, v.UserEmail, v.UserName,
		v.Organization, v.Method, string(v.Status)).Scan(&v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
	}
	if err != nil {
		return err
	}
	if err := queueVerificationRole(ctx, tx, v); err != nil {
		return err
	}
	if err := outbox.Insert(ctx, tx, "application_svc", "verification.created", "verification", v.ID.String(), map[string]any{"verification_id": v.ID, "user_id": v.UserID, "status": v.Status, "method": v.Method}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanVerification(row pgx.Row, v *domain.OrganizerVerification) error {
	var status string
	err := row.Scan(&v.ID, &v.UserID, &v.UserEmail, &v.UserName, &v.Organization,
		&v.Method, &status, &v.RejectionReason, &v.ReviewedBy, &v.ReviewedAt,
		&v.CreatedAt, &v.UpdatedAt, &v.RoleApplied)
	v.Status = domain.VerificationStatus(status)
	return err
}

const verificationCols = `id,user_id,user_email,user_name,organization,method,status::text,
	rejection_reason,reviewed_by,reviewed_at,created_at,updated_at,role_applied`

func (r *Applications) GetVerification(ctx context.Context, id uuid.UUID) (*domain.OrganizerVerification, error) {
	var v domain.OrganizerVerification
	err := scanVerification(r.pool.QueryRow(ctx, `SELECT `+verificationCols+`
		FROM application_svc.organizer_verifications WHERE id=$1`, id), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (r *Applications) ListVerifications(ctx context.Context, userID *uuid.UUID, status string) ([]domain.OrganizerVerification, error) {
	return r.ListVerificationsPage(ctx, userID, status, 500, 0)
}

func (r *Applications) ListVerificationsPage(ctx context.Context, userID *uuid.UUID, status string, limit, offset int) ([]domain.OrganizerVerification, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+verificationCols+`
		FROM application_svc.organizer_verifications
		WHERE ($1::uuid IS NULL OR user_id=$1) AND ($2='' OR status::text=$2)
		ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, userID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OrganizerVerification
	for rows.Next() {
		var v domain.OrganizerVerification
		if err := scanVerification(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Applications) AddVerificationDocument(ctx context.Context, d *domain.VerificationDocument) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var status string
	err = tx.QueryRow(ctx, `SELECT status::text FROM application_svc.organizer_verifications WHERE id=$1 FOR UPDATE`, d.VerificationID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrStateConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO application_svc.verification_documents
		(id,verification_id,original_name,stored_name,mime_type,size_bytes,bucket)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING uploaded_at`,
		d.ID, d.VerificationID, d.OriginalName, d.StoredName, d.MimeType, d.SizeBytes, d.Bucket).Scan(&d.UploadedAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Applications) ListVerificationDocuments(ctx context.Context, verificationID uuid.UUID) ([]domain.VerificationDocument, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,verification_id,original_name,stored_name,mime_type,size_bytes,bucket,uploaded_at
		FROM application_svc.verification_documents WHERE verification_id=$1 ORDER BY uploaded_at`, verificationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.VerificationDocument
	for rows.Next() {
		var d domain.VerificationDocument
		if err := rows.Scan(&d.ID, &d.VerificationID, &d.OriginalName, &d.StoredName, &d.MimeType, &d.SizeBytes, &d.Bucket, &d.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Applications) GetVerificationDocument(ctx context.Context, id uuid.UUID) (*domain.VerificationDocument, error) {
	var d domain.VerificationDocument
	err := r.pool.QueryRow(ctx, `SELECT id,verification_id,original_name,stored_name,mime_type,size_bytes,bucket,uploaded_at
		FROM application_svc.verification_documents WHERE id=$1`, id).Scan(
		&d.ID, &d.VerificationID, &d.OriginalName, &d.StoredName, &d.MimeType, &d.SizeBytes, &d.Bucket, &d.UploadedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (r *Applications) ModerateVerification(ctx context.Context, id, reviewer uuid.UUID, status domain.VerificationStatus, reason *string) (*domain.OrganizerVerification, error) {
	var v domain.OrganizerVerification
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	err = scanVerification(tx.QueryRow(ctx, `UPDATE application_svc.organizer_verifications
		SET status=$2::application_svc.verification_status,rejection_reason=$3,
		    reviewed_by=$4,reviewed_at=now(),updated_at=now()
		WHERE id=$1 AND status='pending'
		RETURNING `+verificationCols, id, string(status), reason, reviewer), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := queueVerificationRole(ctx, tx, &v); err != nil {
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, "application_svc", "verification.decided", "verification", id.String(), map[string]any{"verification_id": id, "user_id": v.UserID, "status": status, "changed_by": reviewer}); err != nil {
		return nil, err
	}
	return &v, tx.Commit(ctx)
}

func queueVerificationRole(ctx context.Context, tx pgx.Tx, v *domain.OrganizerVerification) error {
	if v.Status != domain.VerificationApproved {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO application_svc.verification_role_jobs(verification_id) VALUES ($1) ON CONFLICT DO NOTHING`, v.ID)
	return err
}
