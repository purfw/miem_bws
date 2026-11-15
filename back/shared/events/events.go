// Package events defines shared NATS payloads.
package events

import (
	"time"

	"github.com/google/uuid"
)

const (
	SubjectUserRegistered            = "user.registered"
	SubjectUserVerificationRequested = "user.verification_requested"
	SubjectUserRoleChanged           = "user.role_changed"
	SubjectUserDeleted               = "user.deleted"
	SubjectUserActiveChanged         = "user.active_changed"
	SubjectConferenceCreated         = "conference.created"
	SubjectConferenceUpdated         = "conference.updated"
	SubjectConferenceStatus          = "conference.status_changed"
	SubjectScheduleUpdated           = "schedule.updated"
	SubjectAppSubmitted              = "application.submitted"
	SubjectAppStatusChanged          = "application.status_changed"
	SubjectAppModerated              = "application.moderated"
	SubjectReviewCreated             = "review.created"
	SubjectRegistrationConfirmed     = "registration.confirmed"
	SubjectRegistrationWaitlisted    = "registration.waitlisted"
	SubjectRegistrationPromoted      = "registration.promoted"
	SubjectRegistrationCancelled     = "registration.cancelled"
	SubjectConferenceCancelled       = "conference.cancelled.participant"
	SubjectDocumentReady             = "document.ready"
)

type UserRegistered struct {
	UserID            uuid.UUID `json:"user_id"`
	Email             string    `json:"user_email"`
	FirstName         string    `json:"first_name"`
	LastName          string    `json:"last_name"`
	VerificationToken string    `json:"verification_token"`
}

type UserVerificationRequested struct {
	UserID            uuid.UUID `json:"user_id"`
	Email             string    `json:"user_email"`
	FirstName         string    `json:"first_name"`
	VerificationToken string    `json:"verification_token"`
}

type UserRoleChanged struct {
	UserID    uuid.UUID `json:"user_id"`
	OldRole   string    `json:"old_role"`
	NewRole   string    `json:"new_role"`
	ChangedBy uuid.UUID `json:"changed_by"`
}

type UserActiveChanged struct {
	UserID    uuid.UUID `json:"user_id"`
	Active    bool      `json:"active"`
	Role      string    `json:"role"`
	ChangedBy uuid.UUID `json:"changed_by"`
}

type ConferenceCreated struct {
	ConferenceID uuid.UUID `json:"conference_id"`
	Title        string    `json:"title"`
	Slug         string    `json:"slug"`
	StartDate    string    `json:"start_date"`
	CreatedBy    uuid.UUID `json:"created_by"`
}

type ConferenceUpdated struct {
	ConferenceID uuid.UUID `json:"conference_id"`
	Title        string    `json:"title"`
}

type ConferenceStatusChanged struct {
	ConferenceID         uuid.UUID `json:"conference_id"`
	Title                string    `json:"title"`
	OldStatus            string    `json:"old_status"`
	NewStatus            string    `json:"new_status"`
	RegistrationDeadline *string   `json:"registration_deadline,omitempty"`
}

type ScheduleUpdated struct {
	ConferenceID uuid.UUID  `json:"conference_id"`
	SessionID    uuid.UUID  `json:"session_id,omitempty"`
	Title        string     `json:"title"`
	Slug         string     `json:"slug"`
	Action       string     `json:"action"`
	SpeakerAdded bool       `json:"speaker_added"`
	ActorID      *uuid.UUID `json:"actor_id,omitempty"`
}

// Email snapshots avoid cross-schema reads.

type ApplicationSubmitted struct {
	ApplicationID   uuid.UUID `json:"application_id"`
	ConferenceID    uuid.UUID `json:"conference_id"`
	ConferenceTitle string    `json:"conference_title"`
	UserID          uuid.UUID `json:"user_id"`
	UserEmail       string    `json:"user_email"`
	UserName        string    `json:"user_name"`
	Title           string    `json:"title"`
	Form            string    `json:"form"`
}

type ApplicationStatusChanged struct {
	ApplicationID   uuid.UUID `json:"application_id"`
	ConferenceID    uuid.UUID `json:"conference_id"`
	ConferenceTitle string    `json:"conference_title"`
	UserID          uuid.UUID `json:"user_id"`
	UserEmail       string    `json:"user_email"`
	UserName        string    `json:"user_name"`
	OldStatus       string    `json:"old_status"`
	NewStatus       string    `json:"new_status"`
}

type ApplicationModerated struct {
	ApplicationID   uuid.UUID `json:"application_id"`
	ConferenceID    uuid.UUID `json:"conference_id"`
	ConferenceTitle string    `json:"conference_title"`
	UserID          uuid.UUID `json:"user_id"`
	UserEmail       string    `json:"user_email"`
	UserName        string    `json:"user_name"`
	NewStatus       string    `json:"new_status"`
	Comment         string    `json:"comment,omitempty"`
	ChangedBy       uuid.UUID `json:"changed_by"`
}

type ReviewCreated struct {
	ReviewID          uuid.UUID `json:"review_id"`
	ApplicationID     uuid.UUID `json:"application_id"`
	UserID            uuid.UUID `json:"user_id"`
	UserEmail         string    `json:"user_email"`
	UserName          string    `json:"user_name"`
	Recommendation    string    `json:"recommendation"`
	IsVisibleToAuthor bool      `json:"is_visible_to_author"`
}

type RegistrationNotification struct {
	RegistrationID   uuid.UUID `json:"registration_id"`
	ConferenceID     uuid.UUID `json:"conference_id"`
	ConferenceTitle  string    `json:"conference_title"`
	ConferenceSlug   string    `json:"conference_slug"`
	StartDate        string    `json:"start_date"`
	EndDate          string    `json:"end_date"`
	Location         string    `json:"location,omitempty"`
	UserID           uuid.UUID `json:"user_id"`
	UserEmail        string    `json:"user_email"`
	UserName         string    `json:"user_name"`
	Status           string    `json:"status"`
	WaitlistPosition int       `json:"waitlist_position,omitempty"`
}

type DocumentReady struct {
	DocumentID        uuid.UUID `json:"document_id"`
	UserID            uuid.UUID `json:"user_id"`
	UserEmail         string    `json:"user_email"`
	UserName          string    `json:"user_name"`
	ConferenceID      uuid.UUID `json:"conference_id"`
	ConferenceTitle   string    `json:"conference_title"`
	DownloadToken     string    `json:"download_token"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
}
