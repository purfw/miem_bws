package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/confhub/conference-svc/internal/domain"
)

const cardExtras = `COALESCE(rc.confirmed_count, 0), COALESCE(sc.n, 0)`

const cardJoins = `
	LEFT JOIN conference_svc.registration_counters rc ON rc.conference_id = c.id
	LEFT JOIN LATERAL (
		SELECT count(*) AS n FROM conference_svc.schedule_slots s WHERE s.conference_id = c.id
	) sc ON true`

func sortExpr(sort string) string {
	switch sort {
	case "start_date":
		return "c.start_date ASC, c.id ASC"
	case "-start_date":
		return "c.start_date DESC, c.id DESC"
	case "created_at":
		return "c.created_at ASC, c.id ASC"
	case "title":
		return "c.title ASC, c.id ASC"
	case "-created_at", "":
		return "c.created_at DESC, c.id DESC"
	default:
		return "c.created_at DESC, c.id DESC"
	}
}

func scanCard(rows pgx.Row, c *domain.Conference, total *int) error {
	var status, format string
	if err := rows.Scan(&c.ID, &c.Title, &c.TitleEn, &c.Slug, &c.Description, &c.DescriptionEn, &status,
		&format, &c.StartDate, &c.EndDate, &c.RegistrationDeadline, &c.MaxParticipants,
		&c.Location, &c.City, &c.CoverImageURL, &c.IsFeatured, &c.PublicationAllowed,
		&c.Topics, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		&c.Participants, &c.SessionCount, &c.IsFavorite, total); err != nil {
		return err
	}
	c.Status = domain.Status(status)
	c.Format = domain.Format(format)
	return nil
}

func favoriteExpr(viewer *uuid.UUID, args []any) (string, []any) {
	if viewer == nil {
		return "false", args
	}
	args = append(args, *viewer)
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM conference_svc.favorites fv
		WHERE fv.conference_id = c.id AND fv.user_id = $%d)`, len(args)), args
}

func (r *Conferences) ListPage(ctx context.Context, f ListFilter) ([]domain.Conference, int, error) {
	fav, args := favoriteExpr(f.ViewerID, nil)
	where, args := f.buildWhere(args)
	if f.Limit <= 0 {
		f.Limit = 20
	}
	args = append(args, f.Limit)
	limitPos := len(args)
	args = append(args, f.Offset)

	q := fmt.Sprintf(`SELECT %s, %s, %s, count(*) OVER() AS total
		FROM conference_svc.conferences c %s
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		conferenceCols, cardExtras, fav, cardJoins,
		strings.Join(where, " AND "), sortExpr(f.Sort), limitPos, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
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
		total, err = r.count(ctx, f)
		if err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (r *Conferences) count(ctx context.Context, f ListFilter) (int, error) {
	where, args := f.buildWhere(nil)
	q := fmt.Sprintf(`SELECT count(*) FROM conference_svc.conferences c WHERE %s`,
		strings.Join(where, " AND "))
	var n int
	err := r.pool.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

type FacetBucket struct {
	Value string
	Count int
}

func (r *Conferences) Facets(ctx context.Context, f ListFilter) (topics, formats, cities []FacetBucket, err error) {
	topicFilter := f
	topicFilter.Topics = nil
	topics, err = r.facet(ctx, topicFilter,
		`SELECT t, count(*) FROM conference_svc.conferences c, unnest(c.topics) t
		 WHERE %s GROUP BY t ORDER BY count(*) DESC, t ASC LIMIT 50`)
	if err != nil {
		return nil, nil, nil, err
	}

	formatFilter := f
	formatFilter.Formats = nil
	formats, err = r.facet(ctx, formatFilter,
		`SELECT c.format::text, count(*) FROM conference_svc.conferences c
		 WHERE %s GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return nil, nil, nil, err
	}

	cityFilter := f
	cityFilter.Cities = nil
	cities, err = r.facet(ctx, cityFilter,
		`SELECT c.city, count(*) FROM conference_svc.conferences c
		 WHERE %s AND c.city IS NOT NULL AND c.city <> ''
		 GROUP BY 1 ORDER BY count(*) DESC, 1 ASC LIMIT 50`)
	if err != nil {
		return nil, nil, nil, err
	}
	return topics, formats, cities, nil
}

func (r *Conferences) facet(ctx context.Context, f ListFilter, tmpl string) ([]FacetBucket, error) {
	where, args := f.buildWhere(nil)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(tmpl, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FacetBucket, 0, 16)
	for rows.Next() {
		var b FacetBucket
		if err := rows.Scan(&b.Value, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Conferences) Hero(ctx context.Context) (*domain.Conference, error) {
	q := fmt.Sprintf(`SELECT %s, %s, false, 0
		FROM conference_svc.conferences c %s
		WHERE c.deleted_at IS NULL
		  AND c.publication_allowed=true
		  AND c.status IN ('registration_open','registration_closed','in_progress')
		  AND c.end_date >= current_date
		ORDER BY c.is_featured DESC,
		         (c.start_date >= current_date) DESC,
		         c.start_date ASC,
		         c.id ASC
		LIMIT 1`, conferenceCols, cardExtras, cardJoins)

	var c domain.Conference
	var ignored int
	if err := scanCard(r.pool.QueryRow(ctx, q), &c, &ignored); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *Conferences) PlatformStats(ctx context.Context, includeApprovalRate bool) (domain.PlatformStats, error) {
	q := `
		SELECT
			count(*), COALESCE(sum(rc.confirmed_count), 0)`
	var conferences, participants, accepted, rejected int
	targets := []any{&conferences, &participants}
	if includeApprovalRate {
		q += `, COALESCE(sum(cc.accepted), 0), COALESCE(sum(cc.rejected), 0)`
		targets = append(targets, &accepted, &rejected)
	}
	q += ` FROM conference_svc.conferences c
		LEFT JOIN conference_svc.registration_counters rc ON rc.conference_id=c.id`
	if includeApprovalRate {
		q += ` LEFT JOIN conference_svc.conference_counters cc ON cc.conference_id=c.id`
	}
	q += ` WHERE c.deleted_at IS NULL AND c.publication_allowed=true AND c.status <> 'draft'`
	if err := r.pool.QueryRow(ctx, q).Scan(targets...); err != nil {
		return domain.PlatformStats{}, err
	}
	st := domain.PlatformStats{Conferences: conferences, Participants: participants}
	if decided := accepted + rejected; decided > 0 {
		st.ApprovalRate = float64(accepted) / float64(decided)
	}
	return st, nil
}

func (r *Conferences) ClaimEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID, eventType string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO conference_svc.processed_events (event_id, event_type)
		VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, eventType)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Conferences) SetPublicationAllowedForCreator(ctx context.Context, tx pgx.Tx, creator uuid.UUID, allowed bool) error {
	_, err := tx.Exec(ctx, `UPDATE conference_svc.conferences
		SET publication_allowed=$2 WHERE created_by=$1 AND deleted_at IS NULL`, creator, allowed)
	return err
}

type CounterDelta struct {
	Participants int
	Accepted     int
	Rejected     int
}

func (r *Conferences) ApplyCounterDelta(ctx context.Context, tx pgx.Tx, conferenceID uuid.UUID, d CounterDelta) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO conference_svc.conference_counters
			(conference_id, participants, accepted, rejected, updated_at)
		VALUES ($1, GREATEST($2,0), GREATEST($3,0), GREATEST($4,0), now())
		ON CONFLICT (conference_id) DO UPDATE SET
			participants = GREATEST(conference_svc.conference_counters.participants + $2, 0),
			accepted     = GREATEST(conference_svc.conference_counters.accepted     + $3, 0),
			rejected     = GREATEST(conference_svc.conference_counters.rejected     + $4, 0),
			updated_at   = now()`,
		conferenceID, d.Participants, d.Accepted, d.Rejected)
	return err
}

func (r *Conferences) Counters(ctx context.Context, conferenceID uuid.UUID) (domain.Counters, error) {
	var c domain.Counters
	err := r.pool.QueryRow(ctx,
		`SELECT participants, accepted, rejected FROM conference_svc.conference_counters WHERE conference_id=$1`,
		conferenceID).Scan(&c.Participants, &c.Accepted, &c.Rejected)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Counters{}, nil
	}
	return c, err
}

func (r *Conferences) SessionCount(ctx context.Context, conferenceID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM conference_svc.schedule_slots WHERE conference_id=$1`,
		conferenceID).Scan(&n)
	return n, err
}
