package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/outbox"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
)

const schemaName = "conference_svc"

type Service struct {
	repo *repository.Conferences
}

func New(repo *repository.Conferences) *Service { return &Service{repo: repo} }

type CreateInput struct {
	Title                string
	TitleEn              *string
	Slug                 string
	Description          *string
	DescriptionEn        *string
	Format               domain.Format
	StartDate            time.Time
	EndDate              time.Time
	RegistrationDeadline *time.Time
	MaxParticipants      *int
	Location             *string
	City                 *string
	CoverImageURL        *string
	Topics               []string
	CreatedBy            uuid.UUID
	Sections             []domain.Section
	Organizers           []domain.Organizer
	Partners             []domain.Partner
	PublicationAllowed   bool
}

func (s *Service) Create(ctx context.Context, in *CreateInput) (*domain.Conference, error) {
	if err := domain.ValidateStartDate(in.StartDate); err != nil {
		return nil, err
	}
	if in.Topics == nil {
		in.Topics = []string{}
	}
	if in.Title == "" || in.Slug == "" {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "title/slug", Message: "required"}}}
	}
	if in.EndDate.Before(in.StartDate) {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "end_date", Message: "must be >= start_date"}}}
	}
	format := in.Format
	if format == "" {
		format = domain.FormatOffline
	}
	c := &domain.Conference{
		Title:                in.Title,
		TitleEn:              in.TitleEn,
		Slug:                 in.Slug,
		Description:          in.Description,
		DescriptionEn:        in.DescriptionEn,
		Status:               domain.StatusDraft,
		Format:               format,
		StartDate:            in.StartDate,
		EndDate:              in.EndDate,
		RegistrationDeadline: in.RegistrationDeadline,
		MaxParticipants:      in.MaxParticipants,
		Location:             in.Location,
		City:                 in.City,
		CoverImageURL:        in.CoverImageURL,
		Topics:               in.Topics,
		CreatedBy:            in.CreatedBy,
		PublicationAllowed:   in.PublicationAllowed,
	}

	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, in.CreatedBy.String()); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM conference_svc.conferences WHERE created_by=$1 AND created_at >= now()-interval '24 hours'`, in.CreatedBy).Scan(&count); err != nil {
		return nil, err
	}
	if count >= 10 {
		return nil, fmt.Errorf("maximum 10 conferences in 24 hours: %w", httputil.ErrTooMany)
	}
	if in.MaxParticipants != nil && *in.MaxParticipants < 1 {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "max_participants", Message: "must be positive"}}}
	}

	if err := s.repo.CreateInTx(ctx, tx, c, in.Sections, in.Organizers, in.Partners); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, fmt.Errorf("slug already taken: %w", httputil.ErrConflict)
		}
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectConferenceCreated, "conference", c.ID.String(),
		sharedevents.ConferenceCreated{
			ConferenceID: c.ID, Title: c.Title, Slug: c.Slug,
			StartDate: c.StartDate.Format("2006-01-02"), CreatedBy: c.CreatedBy,
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, patch repository.ConferencePatch) (*domain.Conference, error) {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	cur, err := s.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("conference not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if patch.Status != nil && *patch.Status != cur.Status && !domain.CanTransition(cur.Status, *patch.Status) {
		return nil, fmt.Errorf("%s -> %s: %w", cur.Status, *patch.Status, httputil.ErrConflict)
	}
	if !patch.StatusOnly() {
		if err := s.validatePatch(ctx, tx, cur, patch); err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.UpdateInTx(ctx, tx, id, patch)
	if err != nil {
		if errors.Is(err, repository.ErrFeaturedTaken) {
			return nil, fmt.Errorf("another conference is already featured: %w", httputil.ErrConflict)
		}
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, fmt.Errorf("slug already taken: %w", httputil.ErrConflict)
		}
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("conference not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if err := s.emitChangeEvents(ctx, tx, cur, updated, patch.Status != nil && *patch.Status != cur.Status); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

// emitChangeEvents writes the outbox events for an updated conference: a
// status change event (and cancellation notices), or a plain update event.
func (s *Service) emitChangeEvents(ctx context.Context, tx pgx.Tx, cur, updated *domain.Conference, statusChanged bool) error {
	if statusChanged {
		var deadline *string
		if updated.RegistrationDeadline != nil {
			d := updated.RegistrationDeadline.Format(time.RFC3339)
			deadline = &d
		}
		if err := outbox.Insert(ctx, tx, schemaName,
			sharedevents.SubjectConferenceStatus, "conference", updated.ID.String(),
			sharedevents.ConferenceStatusChanged{
				ConferenceID: updated.ID, Title: updated.Title,
				OldStatus: string(cur.Status), NewStatus: string(updated.Status),
				RegistrationDeadline: deadline,
			}); err != nil {
			return err
		}
		if updated.Status == domain.StatusCancelled {
			rows, err := tx.Query(ctx, `SELECT r.id,r.user_id,r.user_email,r.user_name,r.status::text
			FROM conference_svc.registrations r
			WHERE r.conference_id=$1 AND r.status IN ('confirmed','waitlisted')`, updated.ID)
			if err != nil {
				return err
			}
			type recipient struct {
				id, userID         uuid.UUID
				email, name, state string
			}
			var recipients []recipient
			for rows.Next() {
				var item recipient
				if err := rows.Scan(&item.id, &item.userID, &item.email, &item.name, &item.state); err != nil {
					rows.Close()
					return err
				}
				recipients = append(recipients, item)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			for _, item := range recipients {
				if err := outbox.Insert(ctx, tx, schemaName,
					sharedevents.SubjectConferenceCancelled, "registration", item.id.String(),
					sharedevents.RegistrationNotification{
						RegistrationID: item.id, ConferenceID: updated.ID, ConferenceTitle: updated.Title,
						ConferenceSlug: updated.Slug, StartDate: updated.StartDate.Format("2006-01-02"),
						EndDate: updated.EndDate.Format("2006-01-02"), UserID: item.userID,
						UserEmail: item.email, UserName: item.name, Status: item.state,
					}); err != nil {
					return err
				}
			}
		}
	} else {
		if err := outbox.Insert(ctx, tx, schemaName,
			sharedevents.SubjectConferenceUpdated, "conference", updated.ID.String(),
			sharedevents.ConferenceUpdated{ConferenceID: updated.ID, Title: updated.Title}); err != nil {
			return err
		}
	}
	return nil
}

// AdvanceLifecycle moves conferences through their status lifecycle by date:
// registration closes after the deadline, the event starts on its start date
// and completes after its end date. It returns the number of transitions.
func (s *Service) AdvanceLifecycle(ctx context.Context, now time.Time) (int, error) {
	due, err := s.repo.LifecycleDue(ctx, domain.LifecycleToday(now))
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, c := range due {
		from := c.Status
		for _, to := range domain.LifecycleSteps(c.Status, c.StartDate, c.EndDate, c.RegistrationDeadline, now) {
			ok, err := s.transition(ctx, c.ID, from, to)
			if err != nil {
				return applied, fmt.Errorf("conference %s %s -> %s: %w", c.ID, from, to, err)
			}
			if !ok {
				break // changed concurrently; the next run starts from the new status
			}
			applied++
			from = to
		}
	}
	return applied, nil
}

// transition changes the status only if it is still from, so concurrent
// replicas running the lifecycle never apply a step twice.
func (s *Service) transition(ctx context.Context, id uuid.UUID, from, to domain.Status) (bool, error) {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	cur, err := s.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if cur.Status != from || !domain.CanTransition(from, to) {
		return false, nil
	}
	updated, err := s.repo.UpdateInTx(ctx, tx, id, repository.ConferencePatch{Status: &to})
	if err != nil {
		return false, err
	}
	if err := s.emitChangeEvents(ctx, tx, cur, updated, true); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Service) validatePatch(ctx context.Context, tx pgx.Tx, cur *domain.Conference, patch repository.ConferencePatch) error {
	effStart, effEnd := cur.StartDate, cur.EndDate
	if patch.StartDate != nil {
		effStart = *patch.StartDate
	}
	if patch.EndDate != nil {
		effEnd = *patch.EndDate
	}
	if err := domain.ValidateStartDate(effStart); err != nil {
		return err
	}
	if effEnd.Before(effStart) {
		return &httputil.ValidationError{Fields: []httputil.FieldError{
			{Field: "end_date", Message: "must be >= start_date"}}}
	}
	if patch.MaxParticipants != nil {
		var confirmed int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM conference_svc.registrations WHERE conference_id=$1 AND status='confirmed'`, cur.ID).Scan(&confirmed); err != nil {
			return err
		}
		if *patch.MaxParticipants < 1 || *patch.MaxParticipants < confirmed {
			return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "max_participants", Message: "cannot be below the number of confirmed participants"}}}
		}
	}
	var outOfRange bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conference_svc.schedule_slots WHERE conference_id=$1 AND (slot_date < $2 OR slot_date > $3))`, cur.ID, effStart, effEnd).Scan(&outOfRange); err != nil {
		return err
	}
	if outOfRange {
		return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "start_date/end_date", Message: "existing sessions must remain within conference dates"}}}
	}
	return nil
}

func (s *Service) ReplaceSchedule(ctx context.Context, conferenceID uuid.UUID, slots []domain.ScheduleSlot) error {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	conference, err := s.repo.GetByIDTx(ctx, tx, conferenceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("conference not found: %w", httputil.ErrNotFound)
		}
		return err
	}
	if err := s.repo.ReplaceScheduleInTx(ctx, tx, conferenceID, slots); err != nil {
		return err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectScheduleUpdated, "conference", conferenceID.String(),
		sharedevents.ScheduleUpdated{ConferenceID: conferenceID, Title: conference.Title, Slug: conference.Slug, Action: "replaced"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReplaceContent runs replace in a transaction that locks the conference and
// records a conference update event, so the change reaches the audit log and
// the search index.
func (s *Service) ReplaceContent(ctx context.Context, conferenceID uuid.UUID, replace func(pgx.Tx) error) error {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := s.repo.GetByIDTx(ctx, tx, conferenceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("conference not found: %w", httputil.ErrNotFound)
		}
		return err
	}
	if err := replace(tx); err != nil {
		return err
	}
	if err := outbox.Insert(ctx, tx, schemaName, sharedevents.SubjectConferenceUpdated, "conference", c.ID.String(),
		sharedevents.ConferenceUpdated{ConferenceID: c.ID, Title: c.Title}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
