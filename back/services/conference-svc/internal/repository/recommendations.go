package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/recommend"
)

// Seed is a conference that describes what a user is interested in.
type Seed struct {
	ConferenceID uuid.UUID
	Weight       float64
}

// Seed weights: speaking shows the strongest interest, a favourite the weakest.
const (
	seedSpeaker  = 4
	seedAttendee = 3
	seedFavorite = 2
	maxSeeds     = 20
)

// UserSeeds returns the user's most recent registrations and favourites.
func (r *Conferences) UserSeeds(ctx context.Context, userID uuid.UUID) ([]Seed, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT conference_id, max(w) FROM (
			SELECT conference_id, CASE WHEN role_at_conf='speaker' THEN $2::float8 ELSE $3::float8 END AS w, registered_at AS at
			FROM conference_svc.registrations WHERE user_id=$1 AND status<>'cancelled'
			UNION ALL
			SELECT conference_id, $4::float8, created_at FROM conference_svc.favorites WHERE user_id=$1
		) s
		GROUP BY conference_id
		ORDER BY max(at) DESC
		LIMIT $5`, userID, float64(seedSpeaker), float64(seedAttendee), float64(seedFavorite), maxSeeds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Seed
	for rows.Next() {
		var s Seed
		if err := rows.Scan(&s.ConferenceID, &s.Weight); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecommendationCandidates collects ranking signals for every upcoming public
// conference that is not a seed. With a viewer, conferences the viewer
// organizes or is registered for are excluded and the viewer's own activity
// does not count towards co-attendance.
func (r *Conferences) RecommendationCandidates(ctx context.Context, seeds []Seed, viewer *uuid.UUID) ([]recommend.Candidate, error) {
	ids := make([]uuid.UUID, 0, len(seeds))
	weights := make([]float64, 0, len(seeds))
	for _, s := range seeds {
		ids = append(ids, s.ConferenceID)
		weights = append(weights, s.Weight)
	}
	rows, err := r.pool.Query(ctx, `
		WITH seed AS (
			SELECT * FROM unnest($1::uuid[], $2::float8[]) AS s(conference_id, w)
		),
		seed_conf AS (
			SELECT c.topics, c.city, c.format, c.search_vector, s.w
			FROM seed s JOIN conference_svc.conferences c ON c.id = s.conference_id
		),
		interest AS (
			SELECT lower(t) AS topic, sum(w) AS w FROM seed_conf, unnest(topics) t GROUP BY 1
		),
		seed_cities AS (
			SELECT DISTINCT city FROM seed_conf WHERE city IS NOT NULL AND format <> 'online'
		),
		terms AS (
			SELECT to_tsquery('simple', string_agg(l, ' | ')) AS q FROM (
				SELECT DISTINCT l FROM seed_conf, unnest(tsvector_to_array(search_vector)) l
				WHERE l ~ '^[[:alnum:]]+$' AND length(l) > 2
				LIMIT 300) lexemes
		),
		interactions AS (
			SELECT user_id, conference_id FROM conference_svc.registrations WHERE status <> 'cancelled'
			UNION
			SELECT user_id, conference_id FROM conference_svc.favorites
		),
		neighbours AS (
			SELECT i.user_id, sum(s.w) AS affinity
			FROM interactions i JOIN seed s USING (conference_id)
			WHERE $3::uuid IS NULL OR i.user_id <> $3
			GROUP BY i.user_id
		),
		co AS (
			SELECT i.conference_id, sum(n.affinity) AS score
			FROM interactions i JOIN neighbours n USING (user_id)
			GROUP BY i.conference_id
		)
		SELECT c.id,
		       ARRAY(SELECT t FROM unnest(c.topics) t WHERE lower(t) IN (SELECT topic FROM interest)),
		       COALESCE((SELECT sum(i.w) FROM interest i
		                 WHERE i.topic IN (SELECT lower(t) FROM unnest(c.topics) t)), 0)::float8,
		       COALESCE(ts_rank(c.search_vector, terms.q), 0)::float8,
		       COALESCE(co.score, 0)::float8,
		       c.format <> 'online' AND c.city IN (SELECT city FROM seed_cities),
		       COALESCE(c.city, ''),
		       COALESCE(rc.confirmed_count, 0),
		       c.status = 'registration_open' AND (c.registration_deadline IS NULL OR c.registration_deadline > now()),
		       c.start_date
		FROM conference_svc.conferences c
		CROSS JOIN terms
		LEFT JOIN co ON co.conference_id = c.id
		LEFT JOIN conference_svc.registration_counters rc ON rc.conference_id = c.id
		WHERE c.deleted_at IS NULL AND c.publication_allowed
		  AND c.status IN ('registration_open', 'registration_closed', 'in_progress')
		  AND c.end_date >= current_date
		  AND c.id <> ALL($1::uuid[])
		  AND ($3::uuid IS NULL OR (
		        c.created_by <> $3
		        AND NOT EXISTS (SELECT 1 FROM conference_svc.conference_members m
		                        WHERE m.conference_id = c.id AND m.user_id = $3)
		        AND NOT EXISTS (SELECT 1 FROM conference_svc.registrations rg
		                        WHERE rg.conference_id = c.id AND rg.user_id = $3 AND rg.status <> 'cancelled')))
		LIMIT 1000`, ids, weights, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recommend.Candidate
	for rows.Next() {
		var c recommend.Candidate
		if err := rows.Scan(&c.ID, &c.MatchedTopics, &c.TopicWeight, &c.TextRank, &c.CoScore,
			&c.SameCity, &c.City, &c.Participants, &c.Open, &c.StartDate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
