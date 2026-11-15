package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusDraft              Status = "draft"
	StatusRegistrationOpen   Status = "registration_open"
	StatusRegistrationClosed Status = "registration_closed"
	StatusInProgress         Status = "in_progress"
	StatusCompleted          Status = "completed"
	StatusCancelled          Status = "cancelled"
	StatusArchived           Status = "archived"
)

var allowedTransitions = map[Status]map[Status]struct{}{
	StatusDraft:              {StatusRegistrationOpen: {}, StatusCancelled: {}},
	StatusRegistrationOpen:   {StatusRegistrationClosed: {}, StatusCancelled: {}},
	StatusRegistrationClosed: {StatusRegistrationOpen: {}, StatusInProgress: {}, StatusCancelled: {}},
	StatusInProgress:         {StatusCompleted: {}},
	StatusCompleted:          {StatusArchived: {}},
}

func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	m, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = m[to]
	return ok
}

var ErrBadTransition = errors.New("invalid status transition")

type Format string

const (
	FormatOffline Format = "offline"
	FormatHybrid  Format = "hybrid"
	FormatOnline  Format = "online"
)

func NormalizeFormat(s string) (Format, bool) {
	switch s {
	case "offline", "in_person", "in-person":
		return FormatOffline, true
	case "hybrid":
		return FormatHybrid, true
	case "online":
		return FormatOnline, true
	}
	return "", false
}

type Conference struct {
	ID                   uuid.UUID
	Title                string
	TitleEn              *string
	Slug                 string
	Description          *string
	DescriptionEn        *string
	Status               Status
	Format               Format
	StartDate            time.Time
	EndDate              time.Time
	RegistrationDeadline *time.Time
	MaxParticipants      *int
	Location             *string
	City                 *string
	CoverImageURL        *string
	IsFeatured           bool
	PublicationAllowed   bool
	Topics               []string
	CreatedBy            uuid.UUID
	CreatedAt            time.Time
	UpdatedAt            time.Time

	Participants int
	SessionCount int

	IsFavorite bool
}

type Counters struct {
	Participants int
	Accepted     int
	Rejected     int
}

type PlatformStats struct {
	Participants int
	Conferences  int
	ApprovalRate float64
}

type Section struct {
	ID           uuid.UUID
	ConferenceID uuid.UUID
	Title        string
	TitleEn      *string
	Description  *string
	SortOrder    int

	ModeratorName        *string
	ModeratorAffiliation *string
	ModeratorUserID      *uuid.UUID

	TalksCount int
}

const (
	OrganizerKindOrganizer = "organizer"
	OrganizerKindCommittee = "committee"
)

func NormalizeOrganizerKind(s string) (string, bool) {
	switch s {
	case "", OrganizerKindOrganizer:
		return OrganizerKindOrganizer, true
	case OrganizerKindCommittee, "program_committee":
		return OrganizerKindCommittee, true
	}
	return "", false
}

type Organizer struct {
	ID           uuid.UUID
	ConferenceID uuid.UUID
	Name         string
	Email        *string
	Affiliation  *string
	Role         string
	Kind         string
	JobTitle     *string
	PhotoURL     *string
	SortOrder    int
	UserID       *uuid.UUID
}

type Partner struct {
	ID           uuid.UUID
	ConferenceID uuid.UUID
	Name         string
	LogoURL      *string
	WebsiteURL   *string
	Tier         *string
	SortOrder    int
}

type ScheduleSlot struct {
	ID           uuid.UUID
	ConferenceID uuid.UUID
	SectionID    *uuid.UUID
	Title        string
	SpeakerName  *string
	SpeakerAff   *string
	Room         *string
	SlotDate     time.Time
	StartTime    string // HH:MM:SS
	EndTime      string
	Description  *string
	SessionType  string

	SectionTitle *string
}
