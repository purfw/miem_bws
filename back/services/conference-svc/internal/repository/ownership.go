package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/domain"
)

func (r *Conferences) CanManagePermission(ctx context.Context, conferenceID, userID uuid.UUID, permission string) (bool, error) {
	var allowed bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM conference_svc.conferences c
		WHERE c.id=$1 AND c.deleted_at IS NULL AND (
			c.created_by=$2 OR EXISTS(SELECT 1 FROM conference_svc.conference_members m
				WHERE m.conference_id=c.id AND m.user_id=$2 AND ($3='' OR $3=ANY(m.permissions)))
		))`, conferenceID, userID, permission).Scan(&allowed)
	return allowed, err
}

func (r *Conferences) ListOwned(ctx context.Context, userID uuid.UUID) ([]domain.Conference, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+conferenceCols+`
		FROM conference_svc.conferences c
		WHERE c.deleted_at IS NULL AND (c.created_by=$1 OR EXISTS(
			SELECT 1 FROM conference_svc.conference_members m WHERE m.conference_id=c.id AND m.user_id=$1))
		ORDER BY c.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conference
	for rows.Next() {
		var c domain.Conference
		if err := scanConference(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
