package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/outbox"

	"github.com/confhub/conference-svc/internal/domain"
)

const registrationSchema = "conference_svc"

type Registrations struct{ pool *pgxpool.Pool }

func NewRegistrations(pool *pgxpool.Pool) *Registrations { return &Registrations{pool: pool} }

type conferenceForRegistration struct {
	ID                   uuid.UUID
	Title                string
	Slug                 string
	Status               string
	StartDate            time.Time
	EndDate              time.Time
	RegistrationDeadline *time.Time
	MaxParticipants      *int
	Location             string
}

func registrationError(code, message string, sentinel error) error {
	return &domain.RegistrationError{Code: code, Message: message, Sentinel: sentinel}
}

func loadConferenceForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (conferenceForRegistration, error) {
	var c conferenceForRegistration
	err := tx.QueryRow(ctx, `
		SELECT id, title, slug, status::text, start_date, end_date,
		       registration_deadline, max_participants,
		       concat_ws(', ', nullif(city,''), nullif(location,''))
		  FROM conference_svc.conferences
		 WHERE id=$1 AND deleted_at IS NULL
		 FOR UPDATE`, id).Scan(
		&c.ID, &c.Title, &c.Slug, &c.Status, &c.StartDate, &c.EndDate,
		&c.RegistrationDeadline, &c.MaxParticipants, &c.Location,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, registrationError("CONFERENCE_NOT_FOUND", "conference not found", httputil.ErrNotFound)
	}
	return c, err
}

func validateRegistrationWindow(c conferenceForRegistration) error {
	if c.Status != string(domain.StatusRegistrationOpen) {
		return registrationError("REGISTRATION_CLOSED", "registration is closed", httputil.ErrConflict)
	}
	if c.RegistrationDeadline != nil && !time.Now().Before(*c.RegistrationDeadline) {
		return registrationError("REGISTRATION_CLOSED", "registration deadline has passed", httputil.ErrConflict)
	}
	return nil
}

func (r *Registrations) Register(ctx context.Context, conferenceID, userID uuid.UUID, email, name, role string) (*domain.Registration, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	c, err := loadConferenceForUpdate(ctx, tx, conferenceID)
	if err != nil {
		return nil, err
	}
	if err := validateRegistrationWindow(c); err != nil {
		return nil, err
	}

	var existing domain.Registration
	var existingStatus string
	err = tx.QueryRow(ctx, `
		SELECT id, status::text, cancellation_count, registered_at,
		       confirmed_at, waitlisted_at, cancelled_at, updated_at
		  FROM conference_svc.registrations
		 WHERE conference_id=$1 AND user_id=$2
		 FOR UPDATE`, conferenceID, userID).Scan(
		&existing.ID, &existingStatus, &existing.CancellationCount, &existing.RegisteredAt,
		&existing.ConfirmedAt, &existing.WaitlistedAt, &existing.CancelledAt, &existing.UpdatedAt,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil && existingStatus != string(domain.RegistrationCancelled) {
		return nil, registrationError("ALREADY_REGISTERED", "user is already registered", httputil.ErrConflict)
	}
	if err == nil && existing.CancellationCount >= 3 {
		return nil, registrationError("TOO_MANY_CANCELLATIONS", "maximum of three cancellations reached", httputil.ErrConflict)
	}

	_, err = tx.Exec(ctx, `INSERT INTO conference_svc.registration_counters (conference_id)
		VALUES ($1) ON CONFLICT (conference_id) DO NOTHING`, conferenceID)
	if err != nil {
		return nil, err
	}
	var confirmed int
	if err := tx.QueryRow(ctx, `SELECT confirmed_count FROM conference_svc.registration_counters
		WHERE conference_id=$1 FOR UPDATE`, conferenceID).Scan(&confirmed); err != nil {
		return nil, err
	}

	status := domain.RegistrationConfirmed
	if c.MaxParticipants != nil && confirmed >= *c.MaxParticipants {
		status = domain.RegistrationWaitlisted
	}

	reg := &domain.Registration{
		ConferenceID: conferenceID, UserID: userID, UserEmail: email, UserName: name,
		RoleAtConference: role, Status: status, CancellationCount: existing.CancellationCount,
		ConferenceSlug: c.Slug, ConferenceTitle: c.Title, ConferenceStartDate: c.StartDate,
		ConferenceEndDate: c.EndDate, ConferenceStatus: domain.Status(c.Status), ConferenceLocation: c.Location,
	}
	if existing.ID == uuid.Nil {
		err = tx.QueryRow(ctx, `
			INSERT INTO conference_svc.registrations
				(conference_id,user_id,user_email,user_name,role_at_conf,status,confirmed_at,waitlisted_at)
			VALUES ($1,$2,$3,$4,$5,$6::conference_svc.registration_status,
			        CASE WHEN $6='confirmed' THEN now() END,
			        CASE WHEN $6='waitlisted' THEN now() END)
			RETURNING id, registered_at, confirmed_at, waitlisted_at, cancelled_at, updated_at`,
			conferenceID, userID, email, name, role, string(status)).Scan(
			&reg.ID, &reg.RegisteredAt, &reg.ConfirmedAt, &reg.WaitlistedAt, &reg.CancelledAt, &reg.UpdatedAt,
		)
	} else {
		reg.ID = existing.ID
		err = tx.QueryRow(ctx, `
			UPDATE conference_svc.registrations
			   SET user_email=$2, user_name=$3, role_at_conf=$4,
			       status=$5::conference_svc.registration_status,
			       registered_at=now(), confirmed_at=CASE WHEN $5='confirmed' THEN now() END,
			       waitlisted_at=CASE WHEN $5='waitlisted' THEN now() END,
			       cancelled_at=NULL
			 WHERE id=$1
			RETURNING registered_at, confirmed_at, waitlisted_at, cancelled_at, updated_at`,
			reg.ID, email, name, role, string(status)).Scan(
			&reg.RegisteredAt, &reg.ConfirmedAt, &reg.WaitlistedAt, &reg.CancelledAt, &reg.UpdatedAt,
		)
	}
	if err != nil {
		return nil, err
	}

	if status == domain.RegistrationConfirmed {
		_, err = tx.Exec(ctx, `UPDATE conference_svc.registration_counters
			SET confirmed_count=confirmed_count+1, updated_at=now() WHERE conference_id=$1`, conferenceID)
	} else {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM conference_svc.registrations
			WHERE conference_id=$1 AND status='waitlisted'
			  AND (waitlisted_at, id) <= ($2, $3)`, conferenceID, reg.WaitlistedAt, reg.ID).Scan(&reg.WaitlistPosition)
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.registration_history
		(registration_id,old_status,new_status,reason) VALUES ($1,$2::conference_svc.registration_status,$3,'user_register')`,
		reg.ID, nullableOldStatus(existingStatus), string(status)); err != nil {
		return nil, err
	}
	subject := sharedevents.SubjectRegistrationConfirmed
	if status == domain.RegistrationWaitlisted {
		subject = sharedevents.SubjectRegistrationWaitlisted
	}
	if err := insertRegistrationEvent(ctx, tx, subject, reg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return reg, nil
}

func nullableOldStatus(status string) any {
	if status == "" {
		return nil
	}
	return status
}

func insertRegistrationEvent(ctx context.Context, tx pgx.Tx, subject string, reg *domain.Registration) error {
	return outbox.Insert(ctx, tx, registrationSchema, subject, "registration", reg.ID.String(),
		sharedevents.RegistrationNotification{
			RegistrationID: reg.ID, ConferenceID: reg.ConferenceID,
			ConferenceTitle: reg.ConferenceTitle, ConferenceSlug: reg.ConferenceSlug,
			StartDate: reg.ConferenceStartDate.Format("2006-01-02"), EndDate: reg.ConferenceEndDate.Format("2006-01-02"),
			Location: reg.ConferenceLocation, UserID: reg.UserID, UserEmail: reg.UserEmail,
			UserName: reg.UserName, Status: string(reg.Status), WaitlistPosition: reg.WaitlistPosition,
		})
}

func (r *Registrations) Cancel(ctx context.Context, registrationID, userID uuid.UUID) (*domain.Registration, *domain.Registration, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	var reg domain.Registration
	var status string
	err = tx.QueryRow(ctx, `
		SELECT r.id,r.conference_id,r.user_id,r.user_email,r.user_name,r.role_at_conf,
		       r.status::text,r.cancellation_count,r.registered_at,r.confirmed_at,
		       r.waitlisted_at,r.cancelled_at,r.updated_at,
		       c.slug,c.title,c.start_date,c.end_date,c.status::text,
		       concat_ws(', ', nullif(c.city,''), nullif(c.location,''))
		  FROM conference_svc.registrations r
		  JOIN conference_svc.conferences c ON c.id=r.conference_id
		 WHERE r.id=$1 AND r.user_id=$2
		 FOR UPDATE OF r,c`, registrationID, userID).Scan(
		&reg.ID, &reg.ConferenceID, &reg.UserID, &reg.UserEmail, &reg.UserName, &reg.RoleAtConference,
		&status, &reg.CancellationCount, &reg.RegisteredAt, &reg.ConfirmedAt,
		&reg.WaitlistedAt, &reg.CancelledAt, &reg.UpdatedAt,
		&reg.ConferenceSlug, &reg.ConferenceTitle, &reg.ConferenceStartDate, &reg.ConferenceEndDate,
		&reg.ConferenceStatus, &reg.ConferenceLocation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, registrationError("REGISTRATION_NOT_FOUND", "registration not found", httputil.ErrNotFound)
	}
	if err != nil {
		return nil, nil, err
	}
	reg.Status = domain.RegistrationStatus(status)
	if reg.Status == domain.RegistrationCancelled {
		return nil, nil, registrationError("CANNOT_CANCEL", "registration is already cancelled", httputil.ErrConflict)
	}
	if reg.CancellationCount >= 3 {
		return nil, nil, registrationError("TOO_MANY_CANCELLATIONS", "maximum of three cancellations reached", httputil.ErrConflict)
	}
	startOfToday := time.Now().UTC().Truncate(24 * time.Hour)
	if !reg.ConferenceStartDate.After(startOfToday) {
		return nil, nil, registrationError("CANNOT_CANCEL", "conference has already started", httputil.ErrConflict)
	}

	oldStatus := reg.Status
	err = tx.QueryRow(ctx, `UPDATE conference_svc.registrations
		SET status='cancelled', cancellation_count=cancellation_count+1, cancelled_at=now()
		WHERE id=$1 RETURNING cancellation_count,cancelled_at,updated_at`, reg.ID).Scan(
		&reg.CancellationCount, &reg.CancelledAt, &reg.UpdatedAt)
	if err != nil {
		return nil, nil, err
	}
	reg.Status = domain.RegistrationCancelled
	if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.registration_history
		(registration_id,old_status,new_status,reason) VALUES ($1,$2,'cancelled','user_cancel')`, reg.ID, string(oldStatus)); err != nil {
		return nil, nil, err
	}
	if err := insertRegistrationEvent(ctx, tx, sharedevents.SubjectRegistrationCancelled, &reg); err != nil {
		return nil, nil, err
	}

	var promoted *domain.Registration
	if oldStatus == domain.RegistrationConfirmed {
		_, err = tx.Exec(ctx, `INSERT INTO conference_svc.registration_counters (conference_id,confirmed_count)
			VALUES ($1,0) ON CONFLICT (conference_id) DO UPDATE
			SET confirmed_count=greatest(conference_svc.registration_counters.confirmed_count-1,0), updated_at=now()`, reg.ConferenceID)
		if err != nil {
			return nil, nil, err
		}
		promoted, err = promoteFirstWaitlisted(ctx, tx, reg)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &reg, promoted, nil
}

func promoteFirstWaitlisted(ctx context.Context, tx pgx.Tx, conference domain.Registration) (*domain.Registration, error) {
	var p domain.Registration
	err := tx.QueryRow(ctx, `
		SELECT id,conference_id,user_id,user_email,user_name,role_at_conf,
		       cancellation_count,registered_at,waitlisted_at,updated_at
		  FROM conference_svc.registrations
		 WHERE conference_id=$1 AND status='waitlisted'
		 ORDER BY waitlisted_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, conference.ConferenceID).Scan(
		&p.ID, &p.ConferenceID, &p.UserID, &p.UserEmail, &p.UserName, &p.RoleAtConference,
		&p.CancellationCount, &p.RegisteredAt, &p.WaitlistedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Status = domain.RegistrationConfirmed
	p.ConferenceSlug = conference.ConferenceSlug
	p.ConferenceTitle = conference.ConferenceTitle
	p.ConferenceStartDate = conference.ConferenceStartDate
	p.ConferenceEndDate = conference.ConferenceEndDate
	p.ConferenceStatus = conference.ConferenceStatus
	p.ConferenceLocation = conference.ConferenceLocation
	if err := tx.QueryRow(ctx, `UPDATE conference_svc.registrations
		SET status='confirmed',confirmed_at=now(),waitlisted_at=NULL
		WHERE id=$1 RETURNING confirmed_at,updated_at`, p.ID).Scan(&p.ConfirmedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE conference_svc.registration_counters
		SET confirmed_count=confirmed_count+1,updated_at=now() WHERE conference_id=$1`, p.ConferenceID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.registration_history
		(registration_id,old_status,new_status,reason) VALUES ($1,'waitlisted','confirmed','capacity_released')`, p.ID); err != nil {
		return nil, err
	}
	if err := insertRegistrationEvent(ctx, tx, sharedevents.SubjectRegistrationPromoted, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Registrations) ListMine(ctx context.Context, userID uuid.UUID, status string, page, perPage int) ([]domain.Registration, int, error) {
	whereStatus := ""
	args := []any{userID, perPage, (page - 1) * perPage}
	if status != "" {
		whereStatus = " AND r.status::text=$4"
		args = append(args, status)
	}
	q := fmt.Sprintf(`
		SELECT r.id,r.conference_id,r.user_id,r.user_email,r.user_name,r.role_at_conf,
		       r.status::text,r.cancellation_count,r.registered_at,r.confirmed_at,
		       r.waitlisted_at,r.cancelled_at,r.updated_at,
		       CASE WHEN r.status='waitlisted' THEN (
		         SELECT count(*) FROM conference_svc.registrations w
		          WHERE w.conference_id=r.conference_id AND w.status='waitlisted'
		            AND (w.waitlisted_at,w.id) <= (r.waitlisted_at,r.id)
		       ) ELSE 0 END,
		       c.slug,c.title,c.start_date,c.end_date,c.status::text,
		       concat_ws(', ', nullif(c.city,''), nullif(c.location,'')), count(*) OVER()
		  FROM conference_svc.registrations r
		  JOIN conference_svc.conferences c ON c.id=r.conference_id
		 WHERE r.user_id=$1%s
		 ORDER BY r.registered_at DESC LIMIT $2 OFFSET $3`, whereStatus)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.Registration, 0)
	total := 0
	for rows.Next() {
		var item domain.Registration
		var registrationStatus, conferenceStatus string
		if err := rows.Scan(
			&item.ID, &item.ConferenceID, &item.UserID, &item.UserEmail, &item.UserName, &item.RoleAtConference,
			&registrationStatus, &item.CancellationCount, &item.RegisteredAt, &item.ConfirmedAt,
			&item.WaitlistedAt, &item.CancelledAt, &item.UpdatedAt, &item.WaitlistPosition,
			&item.ConferenceSlug, &item.ConferenceTitle, &item.ConferenceStartDate, &item.ConferenceEndDate,
			&conferenceStatus, &item.ConferenceLocation, &total,
		); err != nil {
			return nil, 0, err
		}
		item.Status = domain.RegistrationStatus(registrationStatus)
		item.ConferenceStatus = domain.Status(conferenceStatus)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Registrations) CurrentForUser(ctx context.Context, conferenceID, userID uuid.UUID) (*domain.Registration, error) {
	var registration domain.Registration
	var status string
	err := r.pool.QueryRow(ctx, `SELECT r.id,r.conference_id,r.user_id,r.user_email,r.user_name,r.role_at_conf,
		r.status::text,r.cancellation_count,r.registered_at,r.confirmed_at,r.waitlisted_at,r.cancelled_at,r.updated_at,
		CASE WHEN r.status='waitlisted' THEN (SELECT count(*) FROM conference_svc.registrations w
			WHERE w.conference_id=r.conference_id AND w.status='waitlisted' AND (w.waitlisted_at,w.id)<=(r.waitlisted_at,r.id)) ELSE 0 END
		FROM conference_svc.registrations r WHERE r.conference_id=$1 AND r.user_id=$2`, conferenceID, userID).
		Scan(&registration.ID, &registration.ConferenceID, &registration.UserID, &registration.UserEmail, &registration.UserName, &registration.RoleAtConference,
			&status, &registration.CancellationCount, &registration.RegisteredAt, &registration.ConfirmedAt, &registration.WaitlistedAt, &registration.CancelledAt, &registration.UpdatedAt, &registration.WaitlistPosition)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	registration.Status = domain.RegistrationStatus(status)
	return &registration, nil
}

func (r *Registrations) HasConfirmed(ctx context.Context, conferenceID, userID uuid.UUID) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM conference_svc.registrations
		WHERE conference_id=$1 AND user_id=$2 AND status='confirmed'`, conferenceID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

func (r *Registrations) ListByConference(ctx context.Context, conferenceID uuid.UUID, status string) ([]domain.Registration, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,conference_id,user_id,user_email,user_name,role_at_conf,status::text,
		       cancellation_count,registered_at,confirmed_at,waitlisted_at,cancelled_at,updated_at
		  FROM conference_svc.registrations
		 WHERE conference_id=$1 AND ($2='' OR status::text=$2)
		 ORDER BY CASE WHEN status='waitlisted' THEN waitlisted_at ELSE registered_at END,id`, conferenceID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.Registration
	for rows.Next() {
		var item domain.Registration
		var itemStatus string
		if err := rows.Scan(&item.ID, &item.ConferenceID, &item.UserID, &item.UserEmail, &item.UserName,
			&item.RoleAtConference, &itemStatus, &item.CancellationCount, &item.RegisteredAt,
			&item.ConfirmedAt, &item.WaitlistedAt, &item.CancelledAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Status = domain.RegistrationStatus(itemStatus)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Registrations) ReconcileCounters(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conference_svc.registration_counters (conference_id,confirmed_count,updated_at)
		SELECT c.id,count(r.id)::int,now()
		  FROM conference_svc.conferences c
		  LEFT JOIN conference_svc.registrations r ON r.conference_id=c.id AND r.status='confirmed'
		 GROUP BY c.id
		ON CONFLICT (conference_id) DO UPDATE
		SET confirmed_count=excluded.confirmed_count,updated_at=excluded.updated_at`)
	return err
}
