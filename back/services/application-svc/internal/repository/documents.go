package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/confhub/application-svc/internal/domain"
)

const documentCols = `id,registration_id,user_id,conference_id,conference_title,participant_name,
	stored_name,bucket,verification_hash,download_token_hash,download_expires_at,issued_at`

func scanDocument(row pgx.Row, d *domain.ParticipationDocument) error {
	return row.Scan(&d.ID, &d.RegistrationID, &d.UserID, &d.ConferenceID, &d.ConferenceTitle,
		&d.ParticipantName, &d.StoredName, &d.Bucket, &d.VerificationHash, &d.DownloadTokenHash,
		&d.DownloadExpiresAt, &d.IssuedAt)
}

type documentQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *Applications) CreateDocumentInTx(ctx context.Context, q documentQuerier, d *domain.ParticipationDocument) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	err := scanDocument(q.QueryRow(ctx, `INSERT INTO application_svc.participation_documents
		(id,registration_id,user_id,conference_id,conference_title,participant_name,stored_name,bucket,
		 verification_hash,download_token_hash,download_expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+documentCols,
		d.ID, d.RegistrationID, d.UserID, d.ConferenceID, d.ConferenceTitle, d.ParticipantName,
		d.StoredName, d.Bucket, d.VerificationHash, d.DownloadTokenHash, d.DownloadExpiresAt), d)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
	}
	return err
}

func (r *Applications) ListDocuments(ctx context.Context, userID uuid.UUID) ([]domain.ParticipationDocument, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+documentCols+` FROM application_svc.participation_documents WHERE user_id=$1 ORDER BY issued_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ParticipationDocument
	for rows.Next() {
		var d domain.ParticipationDocument
		if err := scanDocument(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Applications) GetDocument(ctx context.Context, id uuid.UUID) (*domain.ParticipationDocument, error) {
	var d domain.ParticipationDocument
	err := scanDocument(r.pool.QueryRow(ctx, `SELECT `+documentCols+` FROM application_svc.participation_documents WHERE id=$1`, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (r *Applications) VerifyDocument(ctx context.Context, hash []byte) (*domain.ParticipationDocument, error) {
	var d domain.ParticipationDocument
	err := scanDocument(r.pool.QueryRow(ctx, `SELECT `+documentCols+` FROM application_svc.participation_documents WHERE verification_hash=$1`, hash), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (r *Applications) DocumentByDownloadToken(ctx context.Context, hash []byte) (*domain.ParticipationDocument, error) {
	var d domain.ParticipationDocument
	err := scanDocument(r.pool.QueryRow(ctx, `SELECT `+documentCols+` FROM application_svc.participation_documents WHERE download_token_hash=$1 AND download_expires_at>now()`, hash), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}
