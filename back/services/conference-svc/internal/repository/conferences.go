package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/confhub/conference-svc/internal/domain"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicate     = errors.New("duplicate slug")
	ErrFeaturedTaken = errors.New("another conference is already featured")
)

const featuredConstraint = "uq_conferences_featured"

func uniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil
	}
	if pgErr.ConstraintName == featuredConstraint {
		return ErrFeaturedTaken
	}
	return ErrDuplicate
}

type Conferences struct {
	pool *pgxpool.Pool
}

func NewConferences(pool *pgxpool.Pool) *Conferences { return &Conferences{pool: pool} }

func (r *Conferences) Pool() *pgxpool.Pool { return r.pool }

func (r *Conferences) SectionBelongsToConference(ctx context.Context, conferenceID, sectionID uuid.UUID) (bool, error) {
	var belongs bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conference_svc.sections s
		JOIN conference_svc.conferences c ON c.id=s.conference_id
		WHERE s.id=$1 AND s.conference_id=$2 AND c.deleted_at IS NULL)`, sectionID, conferenceID).Scan(&belongs)
	return belongs, err
}

const conferenceCols = `c.id, c.title, c.title_en, c.slug, c.description, c.description_en, c.status::text,
	c.format::text, c.start_date, c.end_date, c.registration_deadline, c.max_participants,
	c.location, c.city, c.cover_image_url, c.is_featured, c.publication_allowed, c.topics, c.created_by, c.created_at, c.updated_at`

func scanConference(row pgx.Row, c *domain.Conference) error {
	var status, format string
	if err := row.Scan(&c.ID, &c.Title, &c.TitleEn, &c.Slug, &c.Description, &c.DescriptionEn, &status,
		&format, &c.StartDate, &c.EndDate, &c.RegistrationDeadline, &c.MaxParticipants,
		&c.Location, &c.City, &c.CoverImageURL, &c.IsFeatured, &c.PublicationAllowed,
		&c.Topics, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return err
	}
	c.Status = domain.Status(status)
	c.Format = domain.Format(format)
	return nil
}

func (r *Conferences) GetByID(ctx context.Context, id uuid.UUID) (*domain.Conference, error) {
	q := fmt.Sprintf(`SELECT %s FROM conference_svc.conferences c WHERE c.id=$1 AND c.deleted_at IS NULL`, conferenceCols)
	var c domain.Conference
	if err := scanConference(r.pool.QueryRow(ctx, q, id), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *Conferences) GetBySlug(ctx context.Context, slug string) (*domain.Conference, error) {
	q := fmt.Sprintf(`SELECT %s FROM conference_svc.conferences c WHERE c.slug=$1 AND c.deleted_at IS NULL`, conferenceCols)
	var c domain.Conference
	if err := scanConference(r.pool.QueryRow(ctx, q, slug), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

type ListFilter struct {
	Query    string
	Topics   []string
	Status   string
	Statuses []string
	Formats  []string
	Cities   []string
	FromDate *time.Time
	ToDate   *time.Time
	Sort     string

	ViewerID *uuid.UUID

	After   *time.Time
	AfterID *uuid.UUID

	Limit  int
	Offset int
}

func (f ListFilter) buildWhere(args []any) ([]string, []any) {
	where := []string{"c.deleted_at IS NULL", "c.publication_allowed=true"}

	if f.Query != "" {
		args = append(args, f.Query)
		i := len(args)
		clause := fmt.Sprintf(`(
			c.search_vector @@ plainto_tsquery('russian', $%d)
			OR c.search_vector @@ plainto_tsquery('english', $%d)
			OR c.title %% $%d`, i, i, i)
		if len([]rune(f.Query)) >= 3 {
			args = append(args, "%"+likeEscape(f.Query)+"%")
			clause += fmt.Sprintf("\n\t\t\tOR c.title ILIKE $%d", len(args))
		}
		where = append(where, clause+")")
	}
	if len(f.Topics) > 0 {
		args = append(args, f.Topics)
		where = append(where, fmt.Sprintf(`c.topics && $%d`, len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf(`c.status = $%d::conference_svc.conference_status`, len(args)))
	}
	if len(f.Statuses) > 0 {
		args = append(args, f.Statuses)
		where = append(where, fmt.Sprintf(`c.status::text = ANY($%d)`, len(args)))
	}
	if len(f.Formats) > 0 {
		args = append(args, f.Formats)
		where = append(where, fmt.Sprintf(`c.format::text = ANY($%d)`, len(args)))
	}
	if len(f.Cities) > 0 {
		args = append(args, f.Cities)
		where = append(where, fmt.Sprintf(`c.city = ANY($%d)`, len(args)))
	}
	if f.FromDate != nil {
		args = append(args, *f.FromDate)
		where = append(where, fmt.Sprintf(`c.start_date >= $%d`, len(args)))
	}
	if f.ToDate != nil {
		args = append(args, *f.ToDate)
		where = append(where, fmt.Sprintf(`c.start_date <= $%d`, len(args)))
	}
	return where, args
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func likeEscape(s string) string { return likeEscaper.Replace(s) }

func (r *Conferences) List(ctx context.Context, f ListFilter) ([]domain.Conference, error) {
	where, args := f.buildWhere(nil)
	if f.After != nil && f.AfterID != nil {
		args = append(args, *f.After, *f.AfterID)
		where = append(where, fmt.Sprintf(`(c.created_at, c.id) < ($%d, $%d)`, len(args)-1, len(args)))
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	args = append(args, f.Limit)

	q := fmt.Sprintf(`SELECT %s FROM conference_svc.conferences c WHERE %s
		ORDER BY c.created_at DESC, c.id DESC LIMIT $%d`,
		conferenceCols, strings.Join(where, " AND "), len(args))

	rows, err := r.pool.Query(ctx, q, args...)
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

func (r *Conferences) CreateInTx(ctx context.Context, tx pgx.Tx, c *domain.Conference, sections []domain.Section, organizers []domain.Organizer, partners []domain.Partner) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	const q = `INSERT INTO conference_svc.conferences
		(id, title, title_en, slug, description, description_en, status, format, start_date, end_date,
		 registration_deadline, max_participants, location, city, cover_image_url, topics, created_by, publication_allowed)
		VALUES ($1,$2,$3,$4,$5,$6,$7::conference_svc.conference_status,
		        $8::conference_svc.conference_format,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING created_at, updated_at`
	err := tx.QueryRow(ctx, q,
		c.ID, c.Title, c.TitleEn, c.Slug, c.Description, c.DescriptionEn, string(c.Status), string(c.Format),
		c.StartDate, c.EndDate, c.RegistrationDeadline, c.MaxParticipants,
		c.Location, c.City, c.CoverImageURL, c.Topics, c.CreatedBy, c.PublicationAllowed,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if v := uniqueViolation(err); v != nil {
			return v
		}
		return err
	}
	for i := range sections {
		s := &sections[i]
		if s.ID == uuid.Nil {
			s.ID = uuid.New()
		}
		s.ConferenceID = c.ID
		if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.sections
			(id, conference_id, title, title_en, description, sort_order,
			 moderator_name, moderator_affiliation, moderator_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			s.ID, s.ConferenceID, s.Title, s.TitleEn, s.Description, s.SortOrder,
			s.ModeratorName, s.ModeratorAffiliation, s.ModeratorUserID); err != nil {
			return fmt.Errorf("insert section: %w", err)
		}
	}
	for i := range organizers {
		o := &organizers[i]
		if o.ID == uuid.Nil {
			o.ID = uuid.New()
		}
		o.ConferenceID = c.ID
		if o.Kind == "" {
			o.Kind = domain.OrganizerKindOrganizer
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.organizers
			(id, conference_id, name, email, affiliation, role, kind, job_title, photo_url, sort_order, user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			o.ID, o.ConferenceID, o.Name, o.Email, o.Affiliation, o.Role,
			o.Kind, o.JobTitle, o.PhotoURL, o.SortOrder, o.UserID); err != nil {
			return fmt.Errorf("insert organizer: %w", err)
		}
	}
	for i := range partners {
		p := &partners[i]
		if p.ID == uuid.Nil {
			p.ID = uuid.New()
		}
		p.ConferenceID = c.ID
		if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.partners
			(id, conference_id, name, logo_url, website_url, tier, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			p.ID, p.ConferenceID, p.Name, p.LogoURL, p.WebsiteURL, p.Tier, p.SortOrder); err != nil {
			return fmt.Errorf("insert partner: %w", err)
		}
	}
	return nil
}

func (r *Conferences) UpdateInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, patch ConferencePatch) (*domain.Conference, error) {
	set, args := patch.Build()
	if len(set) == 0 {
		return r.GetByIDTx(ctx, tx, id)
	}
	args = append(args, id)
	q := fmt.Sprintf(`UPDATE conference_svc.conferences SET %s WHERE id=$%d AND deleted_at IS NULL`,
		strings.Join(set, ", "), len(args))
	tag, err := tx.Exec(ctx, q, args...)
	if err != nil {
		if v := uniqueViolation(err); v != nil {
			return nil, v
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetByIDTx(ctx, tx, id)
}

func (r *Conferences) GetByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Conference, error) {
	q := fmt.Sprintf(`SELECT %s FROM conference_svc.conferences c WHERE c.id=$1 AND c.deleted_at IS NULL FOR UPDATE`, conferenceCols)
	var c domain.Conference
	if err := scanConference(tx.QueryRow(ctx, q, id), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *Conferences) Sections(ctx context.Context, conferenceID uuid.UUID) ([]domain.Section, error) {
	rows, err := r.pool.Query(ctx, `SELECT s.id, s.conference_id, s.title, s.title_en, s.description,
			s.sort_order, s.moderator_name, s.moderator_affiliation, s.moderator_user_id,
			COALESCE(t.n, 0)
		FROM conference_svc.sections s
		LEFT JOIN LATERAL (
			SELECT count(*) AS n FROM conference_svc.schedule_slots sl WHERE sl.section_id = s.id
		) t ON true
		WHERE s.conference_id=$1 ORDER BY s.sort_order ASC, s.title ASC`, conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Section
	for rows.Next() {
		var s domain.Section
		if err := rows.Scan(&s.ID, &s.ConferenceID, &s.Title, &s.TitleEn, &s.Description, &s.SortOrder,
			&s.ModeratorName, &s.ModeratorAffiliation, &s.ModeratorUserID, &s.TalksCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Conferences) Organizers(ctx context.Context, conferenceID uuid.UUID) ([]domain.Organizer, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, conference_id, name, email, affiliation, role,
			kind, job_title, photo_url, sort_order, user_id
		FROM conference_svc.organizers WHERE conference_id=$1
		ORDER BY sort_order ASC, name ASC`, conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Organizer
	for rows.Next() {
		var o domain.Organizer
		if err := rows.Scan(&o.ID, &o.ConferenceID, &o.Name, &o.Email, &o.Affiliation, &o.Role,
			&o.Kind, &o.JobTitle, &o.PhotoURL, &o.SortOrder, &o.UserID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *Conferences) Partners(ctx context.Context, conferenceID uuid.UUID) ([]domain.Partner, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, conference_id, name, logo_url, website_url, tier, sort_order
		FROM conference_svc.partners WHERE conference_id=$1
		ORDER BY sort_order ASC, name ASC`, conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Partner
	for rows.Next() {
		var p domain.Partner
		if err := rows.Scan(&p.ID, &p.ConferenceID, &p.Name, &p.LogoURL, &p.WebsiteURL, &p.Tier, &p.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const slotCols = `sl.id, sl.conference_id, sl.section_id, sl.title, sl.speaker_name,
	sl.speaker_affiliation, sl.room, sl.slot_date, sl.start_time::text, sl.end_time::text,
	sl.description, s.title`

func scanSlot(rows pgx.Rows, s *domain.ScheduleSlot) error {
	return rows.Scan(&s.ID, &s.ConferenceID, &s.SectionID, &s.Title, &s.SpeakerName,
		&s.SpeakerAff, &s.Room, &s.SlotDate, &s.StartTime, &s.EndTime, &s.Description, &s.SectionTitle)
}

func (r *Conferences) Schedule(ctx context.Context, conferenceID uuid.UUID) ([]domain.ScheduleSlot, error) {
	q := fmt.Sprintf(`SELECT %s FROM conference_svc.schedule_slots sl
		LEFT JOIN conference_svc.sections s ON s.id = sl.section_id
		WHERE sl.conference_id=$1
		ORDER BY sl.slot_date, sl.start_time, sl.title`, slotCols)
	rows, err := r.pool.Query(ctx, q, conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ScheduleSlot
	for rows.Next() {
		var s domain.ScheduleSlot
		if err := scanSlot(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type TalkFilter struct {
	ConferenceID uuid.UUID
	SectionID    *uuid.UUID
	Day          *time.Time
	Query        string
	Limit        int
	Offset       int
}

func (r *Conferences) Talks(ctx context.Context, f TalkFilter) ([]domain.ScheduleSlot, int, error) {
	args := []any{f.ConferenceID}
	where := []string{"sl.conference_id = $1"}
	if f.SectionID != nil {
		args = append(args, *f.SectionID)
		where = append(where, fmt.Sprintf("sl.section_id = $%d", len(args)))
	}
	if f.Day != nil {
		args = append(args, *f.Day)
		where = append(where, fmt.Sprintf("sl.slot_date = $%d", len(args)))
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		where = append(where, fmt.Sprintf(
			"(sl.title ILIKE $%d OR COALESCE(sl.speaker_name,'') ILIKE $%d)", len(args), len(args)))
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	args = append(args, f.Limit)
	limitPos := len(args)
	args = append(args, f.Offset)

	q := fmt.Sprintf(`SELECT %s, count(*) OVER() AS total
		FROM conference_svc.schedule_slots sl
		LEFT JOIN conference_svc.sections s ON s.id = sl.section_id
		WHERE %s
		ORDER BY sl.slot_date, sl.start_time, sl.title
		LIMIT $%d OFFSET $%d`,
		slotCols, strings.Join(where, " AND "), limitPos, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.ScheduleSlot
	total := 0
	for rows.Next() {
		var s domain.ScheduleSlot
		var t int
		if err := rows.Scan(&s.ID, &s.ConferenceID, &s.SectionID, &s.Title, &s.SpeakerName,
			&s.SpeakerAff, &s.Room, &s.SlotDate, &s.StartTime, &s.EndTime, &s.Description,
			&s.SectionTitle, &t); err != nil {
			return nil, 0, err
		}
		total = t
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		cq := fmt.Sprintf(`SELECT count(*) FROM conference_svc.schedule_slots sl WHERE %s`,
			strings.Join(where, " AND "))
		if err := r.pool.QueryRow(ctx, cq, args[:limitPos-1]...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (r *Conferences) ReplaceScheduleInTx(ctx context.Context, tx pgx.Tx, conferenceID uuid.UUID, slots []domain.ScheduleSlot) error {
	conference, err := r.GetByIDTx(ctx, tx, conferenceID)
	if err != nil {
		return err
	}
	for i := range slots {
		if err := validateScheduleSlot(ctx, tx, conference, &slots[i]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM conference_svc.schedule_slots WHERE conference_id=$1`, conferenceID); err != nil {
		return err
	}
	for i := range slots {
		s := &slots[i]
		if s.ID == uuid.Nil {
			s.ID = uuid.New()
		}
		s.ConferenceID = conferenceID
		if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.schedule_slots
			(id, conference_id, section_id, title, speaker_name, speaker_affiliation, room, slot_date,
			 start_time, end_time, description, session_type)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::time,$10::time,$11,$12)`,
			s.ID, s.ConferenceID, s.SectionID, s.Title, s.SpeakerName, s.SpeakerAff, s.Room,
			s.SlotDate, s.StartTime, s.EndTime, s.Description, s.SessionType); err != nil {
			return err
		}
	}
	return nil
}
