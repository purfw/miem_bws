package repository

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/confhub/conference-svc/internal/domain"
)

type SessionSecure struct {
	domain.ScheduleSlot
	Ciphertext []byte
	Nonce      []byte
	KeyVersion *int16
	PublicAt   *time.Time
}

func (r *Conferences) CreateSession(ctx context.Context, slot *domain.ScheduleSlot) error {
	if slot.ID == uuid.Nil {
		slot.ID = uuid.New()
	}
	return r.mutateSession(ctx, slot, "created", func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO conference_svc.schedule_slots
		(id,conference_id,section_id,title,speaker_name,speaker_affiliation,room,
		 slot_date,start_time,end_time,description,session_type)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING created_at`, slot.ID, slot.ConferenceID, slot.SectionID, slot.Title,
			slot.SpeakerName, slot.SpeakerAff, slot.Room, slot.SlotDate, slot.StartTime,
			slot.EndTime, slot.Description, slot.SessionType).Scan(new(time.Time))
	})
}

func (r *Conferences) GetSessionSecure(ctx context.Context, id uuid.UUID) (*SessionSecure, error) {
	var s SessionSecure
	err := r.pool.QueryRow(ctx, `SELECT id,conference_id,section_id,title,speaker_name,
		speaker_affiliation,room,slot_date,start_time::text,end_time::text,description,
		session_type,join_url_ciphertext,join_url_nonce,join_url_key_version,join_url_public_at
		FROM conference_svc.schedule_slots WHERE id=$1`, id).Scan(
		&s.ID, &s.ConferenceID, &s.SectionID, &s.Title, &s.SpeakerName, &s.SpeakerAff,
		&s.Room, &s.SlotDate, &s.StartTime, &s.EndTime, &s.Description, &s.SessionType,
		&s.Ciphertext, &s.Nonce, &s.KeyVersion, &s.PublicAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *Conferences) UpdateSession(ctx context.Context, slot *domain.ScheduleSlot) error {
	return r.mutateSession(ctx, slot, "updated", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE conference_svc.schedule_slots SET
		section_id=$2,title=$3,speaker_name=$4,speaker_affiliation=$5,room=$6,
		slot_date=$7,start_time=$8,end_time=$9,description=$10,session_type=$11
		WHERE id=$1`, slot.ID, slot.SectionID, slot.Title, slot.SpeakerName, slot.SpeakerAff,
			slot.Room, slot.SlotDate, slot.StartTime, slot.EndTime, slot.Description, slot.SessionType)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *Conferences) DeleteSession(ctx context.Context, id uuid.UUID) error {
	slot, err := r.GetSessionSecure(ctx, id)
	if err != nil {
		return err
	}
	return r.mutateSession(ctx, &slot.ScheduleSlot, "deleted", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM conference_svc.schedule_slots WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *Conferences) SetSessionLink(ctx context.Context, id uuid.UUID, ciphertext, nonce []byte, keyVersion int16, publicAt *time.Time) error {
	slot, err := r.GetSessionSecure(ctx, id)
	if err != nil {
		return err
	}
	return r.mutateSession(ctx, &slot.ScheduleSlot, "link_updated", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE conference_svc.schedule_slots SET
		join_url_ciphertext=$2,join_url_nonce=$3,join_url_key_version=$4,join_url_public_at=$5
		WHERE id=$1`, id, ciphertext, nonce, keyVersion, publicAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *Conferences) mutateSession(ctx context.Context, slot *domain.ScheduleSlot, action string, mutate func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	conference, err := r.GetByIDTx(ctx, tx, slot.ConferenceID)
	if err != nil {
		return err
	}
	if action == "created" || action == "updated" {
		if err := validateScheduleSlot(ctx, tx, conference, slot); err != nil {
			return err
		}
	}
	speakerAdded := action == "created" && slot.SpeakerName != nil && strings.TrimSpace(*slot.SpeakerName) != ""
	if action == "updated" {
		var previous *string
		if err := tx.QueryRow(ctx, `SELECT speaker_name FROM conference_svc.schedule_slots WHERE id=$1`, slot.ID).Scan(&previous); err != nil {
			return err
		}
		speakerAdded = slot.SpeakerName != nil && strings.TrimSpace(*slot.SpeakerName) != "" && (previous == nil || *previous != *slot.SpeakerName)
	}
	if err := mutate(tx); err != nil {
		return err
	}
	var actorID *uuid.UUID
	if claims := jwtauth.ClaimsFromContext(ctx); claims != nil {
		if id, err := uuid.Parse(claims.UserID); err == nil {
			actorID = &id
		}
	}
	if err := outbox.Insert(ctx, tx, "conference_svc", sharedevents.SubjectScheduleUpdated, "conference", slot.ConferenceID.String(), sharedevents.ScheduleUpdated{
		ConferenceID: slot.ConferenceID, SessionID: slot.ID, Title: conference.Title, Slug: conference.Slug, Action: action, SpeakerAdded: speakerAdded, ActorID: actorID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateScheduleSlot(ctx context.Context, tx pgx.Tx, conference *domain.Conference, slot *domain.ScheduleSlot) error {
	invalid := func(field, message string) error {
		return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: field, Message: message}}}
	}
	slot.Title = strings.TrimSpace(slot.Title)
	if slot.Title == "" || utf8.RuneCountInString(slot.Title) > 500 {
		return invalid("title", "required, maximum 500 characters")
	}
	slot.SessionType = strings.TrimSpace(slot.SessionType)
	if slot.SessionType == "" {
		slot.SessionType = "talk"
	}
	if utf8.RuneCountInString(slot.SessionType) > 50 {
		return invalid("session_type", "maximum 50 characters")
	}
	for _, field := range []struct {
		name  string
		value *string
		limit int
	}{
		{"speaker_name", slot.SpeakerName, 255}, {"speaker_affiliation", slot.SpeakerAff, 255}, {"room", slot.Room, 100},
	} {
		if field.value != nil && utf8.RuneCountInString(*field.value) > field.limit {
			return invalid(field.name, "value is too long")
		}
	}
	parseTime := func(value string) (time.Time, error) {
		parsed, err := time.Parse("15:04:05", value)
		if err != nil {
			return time.Parse("15:04", value)
		}
		return parsed, nil
	}
	start, err := parseTime(slot.StartTime)
	if err != nil {
		return invalid("start_time", "HH:MM or HH:MM:SS required")
	}
	end, err := parseTime(slot.EndTime)
	if err != nil || !end.After(start) {
		return invalid("end_time", "must be after start_time")
	}
	slot.StartTime, slot.EndTime = start.Format("15:04:05"), end.Format("15:04:05")
	if slot.SlotDate.Before(conference.StartDate) || slot.SlotDate.After(conference.EndDate) {
		return invalid("slot_date", "must be within conference dates")
	}
	if slot.SectionID != nil {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conference_svc.sections WHERE id=$1 AND conference_id=$2)`, slot.SectionID, slot.ConferenceID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return invalid("section_id", "section belongs to another conference")
		}
	}
	return nil
}

func (r *Conferences) LogJoinAccess(ctx context.Context, slotID, userID uuid.UUID, requestID, ipHash string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO conference_svc.join_link_access_log
		(slot_id,user_id,request_id,ip_hash) VALUES ($1,$2,$3,$4)`, slotID, userID, requestID, ipHash)
	return err
}
