package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/confhub/conference-svc/internal/domain"
)

func (r *Conferences) AddFavorite(ctx context.Context, userID, conferenceID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO conference_svc.favorites (user_id, conference_id)
		VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, conferenceID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (r *Conferences) RemoveFavorite(ctx context.Context, userID, conferenceID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM conference_svc.favorites WHERE user_id=$1 AND conference_id=$2`,
		userID, conferenceID)
	return err
}

func (r *Conferences) IsFavorite(ctx context.Context, userID, conferenceID uuid.UUID) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conference_svc.favorites
		WHERE user_id=$1 AND conference_id=$2)`, userID, conferenceID).Scan(&ok)
	return ok, err
}

func (r *Conferences) FavoritesPage(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Conference, int, error) {
	if limit <= 0 {
		limit = 20
	}
	q := fmt.Sprintf(`SELECT %s, %s, true, count(*) OVER() AS total
		FROM conference_svc.favorites f
		JOIN conference_svc.conferences c ON c.id = f.conference_id
		%s
		WHERE f.user_id = $1 AND c.deleted_at IS NULL AND c.status <> 'draft'
		ORDER BY f.created_at DESC, c.id DESC
		LIMIT $2 OFFSET $3`, conferenceCols, cardExtras, cardJoins)

	rows, err := r.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.Conference
	total := 0
	for rows.Next() {
		var c domain.Conference
		var t int
		if err := scanCard(rows, &c, &t); err != nil {
			return nil, 0, err
		}
		total = t
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		if err := r.pool.QueryRow(ctx, `SELECT count(*)
			FROM conference_svc.favorites f
			JOIN conference_svc.conferences c ON c.id = f.conference_id
			WHERE f.user_id = $1 AND c.deleted_at IS NULL AND c.status <> 'draft'`,
			userID).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (r *Conferences) FavoritesCount(ctx context.Context, conferenceID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM conference_svc.favorites WHERE conference_id=$1`,
		conferenceID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
