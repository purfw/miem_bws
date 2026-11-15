package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/confhub/application-svc/internal/domain"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicate     = errors.New("duplicate")
	ErrStateConflict = errors.New("invalid state")
)

type Applications struct {
	pool *pgxpool.Pool
}

func NewApplications(pool *pgxpool.Pool) *Applications { return &Applications{pool: pool} }
func (r *Applications) Pool() *pgxpool.Pool            { return r.pool }

const appCols = `id, conference_id, section_id, user_id, cached_user_email, cached_user_name,
	cached_conf_title, form::text, title, abstract, co_authors, keywords, status::text,
	status_changed_at, status_changed_by, moderator_comment, created_at, updated_at`

func scanApp(row pgx.Row, a *domain.Application) error {
	var form, status string
	var coRaw []byte
	if err := row.Scan(&a.ID, &a.ConferenceID, &a.SectionID, &a.UserID, &a.CachedUserEmail, &a.CachedUserName,
		&a.CachedConfTitle, &form, &a.Title, &a.Abstract, &coRaw, &a.Keywords, &status,
		&a.StatusChangedAt, &a.StatusChangedBy, &a.ModeratorComment, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return err
	}
	a.Form = domain.Form(form)
	a.Status = domain.Status(status)
	if len(coRaw) > 0 {
		_ = json.Unmarshal(coRaw, &a.CoAuthors)
	}
	return nil
}

func (r *Applications) GetByID(ctx context.Context, id uuid.UUID) (*domain.Application, error) {
	q := fmt.Sprintf(`SELECT %s FROM application_svc.applications WHERE id=$1`, appCols)
	var a domain.Application
	if err := scanApp(r.pool.QueryRow(ctx, q, id), &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *Applications) GetByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Application, error) {
	q := fmt.Sprintf(`SELECT %s FROM application_svc.applications WHERE id=$1 FOR UPDATE`, appCols)
	var a domain.Application
	if err := scanApp(tx.QueryRow(ctx, q, id), &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

type ListFilter struct {
	UserID       *uuid.UUID
	ConferenceID *uuid.UUID
	Statuses     []string
	Limit        int
	Offset       int
}

func (r *Applications) List(ctx context.Context, f ListFilter) ([]domain.Application, error) {
	var args []any
	var where []string
	if f.UserID != nil {
		args = append(args, *f.UserID)
		where = append(where, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if f.ConferenceID != nil {
		args = append(args, *f.ConferenceID)
		where = append(where, fmt.Sprintf("conference_id = $%d", len(args)))
	}
	if len(f.Statuses) > 0 {
		args = append(args, f.Statuses)
		where = append(where, fmt.Sprintf("status::text = ANY($%d)", len(args)))
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	args = append(args, f.Limit)
	if f.Offset < 0 {
		f.Offset = 0
	}
	args = append(args, f.Offset)
	q := fmt.Sprintf(`SELECT %s FROM application_svc.applications WHERE %s
		ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d`,
		appCols, joinWhere(where), len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Application
	for rows.Next() {
		var a domain.Application
		if err := scanApp(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func joinWhere(parts []string) string {
	if len(parts) == 0 {
		return "TRUE"
	}
	return strings.Join(parts, " AND ")
}

func (r *Applications) CreateInTx(ctx context.Context, tx pgx.Tx, a *domain.Application) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.CoAuthors == nil {
		a.CoAuthors = []domain.CoAuthor{}
	}
	co, _ := json.Marshal(a.CoAuthors)
	if a.Keywords == nil {
		a.Keywords = []string{}
	}
	const q = `INSERT INTO application_svc.applications
		(id, conference_id, section_id, user_id, cached_user_email, cached_user_name,
		 cached_conf_title, form, title, abstract, co_authors, keywords, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::application_svc.participation_form,$9,$10,$11::jsonb,$12,$13::application_svc.application_status)
		RETURNING status_changed_at, created_at, updated_at`
	err := tx.QueryRow(ctx, q,
		a.ID, a.ConferenceID, a.SectionID, a.UserID, a.CachedUserEmail, a.CachedUserName,
		a.CachedConfTitle, string(a.Form), a.Title, a.Abstract, co, a.Keywords, string(a.Status),
	).Scan(&a.StatusChangedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

func (r *Applications) SetStatusInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status domain.Status, by *uuid.UUID, comment *string) error {
	tag, err := tx.Exec(ctx, `UPDATE application_svc.applications
		SET status=$1::application_svc.application_status,
		    status_changed_at=now(),
		    status_changed_by=$2,
		    moderator_comment=COALESCE($3, moderator_comment)
		WHERE id=$4`,
		string(status), by, comment, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type ApplicationPatch struct {
	Title     *string
	Abstract  *string
	CoAuthors *[]domain.CoAuthor
	Keywords  *[]string
	SectionID *uuid.UUID
}

func (r *Applications) UpdateMetaInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, p ApplicationPatch) error {
	var args []any
	var set []string
	if p.Title != nil {
		args = append(args, *p.Title)
		set = append(set, fmt.Sprintf("title=$%d", len(args)))
	}
	if p.Abstract != nil {
		args = append(args, *p.Abstract)
		set = append(set, fmt.Sprintf("abstract=$%d", len(args)))
	}
	if p.CoAuthors != nil {
		b, _ := json.Marshal(*p.CoAuthors)
		args = append(args, b)
		set = append(set, fmt.Sprintf("co_authors=$%d::jsonb", len(args)))
	}
	if p.Keywords != nil {
		args = append(args, *p.Keywords)
		set = append(set, fmt.Sprintf("keywords=$%d", len(args)))
	}
	if p.SectionID != nil {
		args = append(args, *p.SectionID)
		set = append(set, fmt.Sprintf("section_id=$%d", len(args)))
	}
	if len(set) == 0 {
		return nil
	}
	args = append(args, id)
	q := fmt.Sprintf(`UPDATE application_svc.applications SET %s WHERE id=$%d`,
		strings.Join(set, ", "), len(args))
	tag, err := tx.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// WithdrawByConference returns changed IDs for status_changed events.
func (r *Applications) WithdrawByConference(ctx context.Context, tx pgx.Tx, conferenceID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `UPDATE application_svc.applications
		SET status='withdrawn'::application_svc.application_status,
		    status_changed_at=now()
		WHERE conference_id=$1
		  AND status IN ('submitted','under_review','revision_requested','accepted')
		RETURNING id`, conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
