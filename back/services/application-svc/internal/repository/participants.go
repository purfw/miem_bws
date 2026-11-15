package repository

import (
	"context"

	"github.com/google/uuid"
)

type Participant struct {
	Name      string
	Form      string
	SectionID *uuid.UUID
	Title     string
}

func (r *Applications) Participants(ctx context.Context, conferenceID uuid.UUID, limit, offset int) ([]Participant, int, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `SELECT cached_user_name, form::text, section_id, title, count(*) OVER() AS total
		FROM application_svc.applications
		WHERE conference_id = $1 AND status = 'accepted'
		ORDER BY cached_user_name ASC, created_at ASC
		LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, q, conferenceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]Participant, 0, limit)
	total := 0
	for rows.Next() {
		var p Participant
		var t int
		if err := rows.Scan(&p.Name, &p.Form, &p.SectionID, &p.Title, &t); err != nil {
			return nil, 0, err
		}
		total = t
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM application_svc.applications
			WHERE conference_id = $1 AND status = 'accepted'`, conferenceID).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}
