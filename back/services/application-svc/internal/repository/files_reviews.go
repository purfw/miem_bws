package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/confhub/application-svc/internal/domain"
)

func (r *Applications) InsertFileInTx(ctx context.Context, tx pgx.Tx, f *domain.ApplicationFile) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	const q = `INSERT INTO application_svc.application_files
		(id, application_id, original_name, stored_name, mime_type, size_bytes, bucket, uploaded_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING uploaded_at`
	return tx.QueryRow(ctx, q, f.ID, f.ApplicationID, f.OriginalName, f.StoredName,
		f.MimeType, f.SizeBytes, f.Bucket, f.UploadedBy).Scan(&f.UploadedAt)
}

func (r *Applications) ListFiles(ctx context.Context, applicationID uuid.UUID) ([]domain.ApplicationFile, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, application_id, original_name, stored_name, mime_type,
		size_bytes, bucket, uploaded_by, uploaded_at FROM application_svc.application_files
		WHERE application_id=$1 ORDER BY uploaded_at DESC`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ApplicationFile
	for rows.Next() {
		var f domain.ApplicationFile
		if err := rows.Scan(&f.ID, &f.ApplicationID, &f.OriginalName, &f.StoredName, &f.MimeType,
			&f.SizeBytes, &f.Bucket, &f.UploadedBy, &f.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *Applications) GetFile(ctx context.Context, id uuid.UUID) (*domain.ApplicationFile, error) {
	var f domain.ApplicationFile
	err := r.pool.QueryRow(ctx, `SELECT id, application_id, original_name, stored_name, mime_type,
		size_bytes, bucket, uploaded_by, uploaded_at FROM application_svc.application_files WHERE id=$1`, id).
		Scan(&f.ID, &f.ApplicationID, &f.OriginalName, &f.StoredName, &f.MimeType,
			&f.SizeBytes, &f.Bucket, &f.UploadedBy, &f.UploadedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (r *Applications) DeleteFileInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM application_svc.application_files WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Applications) InsertReviewInTx(ctx context.Context, tx pgx.Tx, rv *domain.Review) error {
	if rv.ID == uuid.Nil {
		rv.ID = uuid.New()
	}
	const q = `INSERT INTO application_svc.reviews
		(id, application_id, reviewer_id, cached_reviewer_name, score, summary, strengths, weaknesses,
		 recommendation, is_visible_to_author)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::application_svc.review_recommendation,$10)
		RETURNING created_at, updated_at`
	err := tx.QueryRow(ctx, q, rv.ID, rv.ApplicationID, rv.ReviewerID, rv.CachedReviewerName,
		rv.Score, rv.Summary, rv.Strengths, rv.Weaknesses, string(rv.Recommendation), rv.IsVisibleToAuthor).
		Scan(&rv.CreatedAt, &rv.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

func (r *Applications) ListReviews(ctx context.Context, applicationID uuid.UUID, includeHidden bool) ([]domain.Review, error) {
	q := `SELECT id, application_id, reviewer_id, cached_reviewer_name, score, summary, strengths,
		weaknesses, recommendation::text, is_visible_to_author, created_at, updated_at
		FROM application_svc.reviews WHERE application_id=$1`
	if !includeHidden {
		q += ` AND is_visible_to_author = true`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Review
	for rows.Next() {
		var rv domain.Review
		var rec string
		if err := rows.Scan(&rv.ID, &rv.ApplicationID, &rv.ReviewerID, &rv.CachedReviewerName,
			&rv.Score, &rv.Summary, &rv.Strengths, &rv.Weaknesses, &rec, &rv.IsVisibleToAuthor,
			&rv.CreatedAt, &rv.UpdatedAt); err != nil {
			return nil, err
		}
		rv.Recommendation = domain.Recommendation(rec)
		out = append(out, rv)
	}
	return out, rows.Err()
}
