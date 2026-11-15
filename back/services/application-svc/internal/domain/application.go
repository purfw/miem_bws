package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Form string

const (
	FormSpeaker  Form = "speaker"
	FormPoster   Form = "poster"
	FormListener Form = "listener"
)

func ValidForm(f Form) bool {
	switch f {
	case FormSpeaker, FormPoster, FormListener:
		return true
	}
	return false
}

type Status string

const (
	StatusSubmitted         Status = "submitted"
	StatusUnderReview       Status = "under_review"
	StatusRevisionRequested Status = "revision_requested"
	StatusAccepted          Status = "accepted"
	StatusRejected          Status = "rejected"
	StatusWithdrawn         Status = "withdrawn"
)

// Terminal states (accepted/rejected/withdrawn) are absent from the map.
var allowed = map[Status]map[Status]struct{}{
	StatusSubmitted: {
		StatusUnderReview: {},
		StatusWithdrawn:   {},
	},
	StatusUnderReview: {
		StatusRevisionRequested: {},
		StatusAccepted:          {},
		StatusRejected:          {},
		StatusWithdrawn:         {},
	},
	StatusRevisionRequested: {
		StatusSubmitted: {},
		StatusWithdrawn: {},
	},
}

func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	m, ok := allowed[from]
	if !ok {
		return false
	}
	_, ok = m[to]
	return ok
}

var ErrBadTransition = errors.New("invalid status transition")

type CoAuthor struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Affiliation string `json:"affiliation,omitempty"`
}

type Application struct {
	ID               uuid.UUID
	ConferenceID     uuid.UUID
	SectionID        *uuid.UUID
	UserID           uuid.UUID
	CachedUserEmail  string
	CachedUserName   string
	CachedConfTitle  string
	Form             Form
	Title            string
	Abstract         *string
	CoAuthors        []CoAuthor
	Keywords         []string
	Status           Status
	StatusChangedAt  time.Time
	StatusChangedBy  *uuid.UUID
	ModeratorComment *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ApplicationFile struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	OriginalName  string
	StoredName    string
	MimeType      string
	SizeBytes     int64
	Bucket        string
	UploadedBy    uuid.UUID
	UploadedAt    time.Time
}

type Recommendation string

const (
	RecAccept Recommendation = "accept"
	RecMinor  Recommendation = "minor_revision"
	RecMajor  Recommendation = "major_revision"
	RecReject Recommendation = "reject"
)

func ValidRecommendation(r Recommendation) bool {
	switch r {
	case RecAccept, RecMinor, RecMajor, RecReject:
		return true
	}
	return false
}

type Review struct {
	ID                 uuid.UUID
	ApplicationID      uuid.UUID
	ReviewerID         uuid.UUID
	CachedReviewerName string
	Score              int16
	Summary            string
	Strengths          *string
	Weaknesses         *string
	Recommendation     Recommendation
	IsVisibleToAuthor  bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
