package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/domain"
)

// SearchIndexBatch returns conferences changed after the cursor, including
// rows that must be removed from the index. Changes to sections, schedule
// slots and organizers bump conferences.updated_at (migration 007).
func (r *Conferences) SearchIndexBatch(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]domain.SearchDocument, error) {
	if limit < 1 || limit > 2000 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.id,c.title,c.title_en,c.slug,c.description,c.description_en,
		       c.format::text,c.status::text,c.start_date,c.end_date,c.city,c.topics,
		       COALESCE((SELECT array_agg(o.name ORDER BY o.sort_order,o.name)
		                 FROM conference_svc.organizers o WHERE o.conference_id=c.id),'{}'::text[]),
		       COALESCE((SELECT array_agg(DISTINCT n) FROM (
		                   SELECT sl.speaker_name AS n FROM conference_svc.schedule_slots sl
		                   WHERE sl.conference_id=c.id AND COALESCE(sl.speaker_name,'')<>''
		                   UNION
		                   SELECT s.moderator_name FROM conference_svc.sections s
		                   WHERE s.conference_id=c.id AND COALESCE(s.moderator_name,'')<>'') names),'{}'::text[]),
		       COALESCE((SELECT array_agg(DISTINCT t) FROM (
		                   SELECT sl.title AS t FROM conference_svc.schedule_slots sl WHERE sl.conference_id=c.id
		                   UNION
		                   SELECT s.title FROM conference_svc.sections s WHERE s.conference_id=c.id
		                   UNION
		                   SELECT s.title_en FROM conference_svc.sections s
		                   WHERE s.conference_id=c.id AND COALESCE(s.title_en,'')<>'') titles),'{}'::text[]),
		       c.publication_allowed,c.deleted_at IS NOT NULL,c.created_at,c.updated_at
		FROM conference_svc.conferences c
		WHERE (c.updated_at,c.id) > ($1,$2)
		ORDER BY c.updated_at,c.id
		LIMIT $3`, after, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.SearchDocument, 0, limit)
	for rows.Next() {
		var d domain.SearchDocument
		if err := rows.Scan(&d.ID, &d.Title, &d.TitleEn, &d.Slug, &d.Description, &d.DescriptionEn,
			&d.Format, &d.Status, &d.StartDate, &d.EndDate, &d.City, &d.Topics,
			&d.OrganizerNames, &d.Speakers, &d.Program, &d.PublicationAllowed, &d.Deleted, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
