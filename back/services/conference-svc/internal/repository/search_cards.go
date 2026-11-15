package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/domain"
)

// CardsByIDs loads database cards in Elasticsearch relevance order.
func (r *Conferences) CardsByIDs(ctx context.Context, ids []uuid.UUID, viewer *uuid.UUID) ([]domain.Conference, error) {
	if len(ids) == 0 {
		return []domain.Conference{}, nil
	}
	fav, args := favoriteExpr(viewer, nil)
	args = append(args, ids)
	idsPos := len(args)
	q := fmt.Sprintf(`SELECT %s, %s, %s, 0
		FROM conference_svc.conferences c %s
		WHERE c.id = ANY($%d::uuid[]) AND c.deleted_at IS NULL AND c.publication_allowed=true
		ORDER BY array_position($%d::uuid[],c.id)`,
		conferenceCols, cardExtras, fav, cardJoins, idsPos, idsPos)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Conference, 0, len(ids))
	for rows.Next() {
		var c domain.Conference
		var ignored int
		if err := scanCard(rows, &c, &ignored); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
