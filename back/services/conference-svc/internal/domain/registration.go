package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type RegistrationStatus string

const (
	RegistrationConfirmed  RegistrationStatus = "confirmed"
	RegistrationWaitlisted RegistrationStatus = "waitlisted"
	RegistrationCancelled  RegistrationStatus = "cancelled"
)

type Registration struct {
	ID                uuid.UUID
	ConferenceID      uuid.UUID
	UserID            uuid.UUID
	UserEmail         string
	UserName          string
	RoleAtConference  string
	Status            RegistrationStatus
	CancellationCount int
	RegisteredAt      time.Time
	ConfirmedAt       *time.Time
	WaitlistedAt      *time.Time
	CancelledAt       *time.Time
	UpdatedAt         time.Time
	WaitlistPosition  int

	ConferenceSlug      string
	ConferenceTitle     string
	ConferenceStartDate time.Time
	ConferenceEndDate   time.Time
	ConferenceStatus    Status
	ConferenceLocation  string
}

type RegistrationError struct {
	Code     string
	Message  string
	Sentinel error
}

func (e *RegistrationError) Error() string       { return e.Message }
func (e *RegistrationError) Unwrap() error       { return e.Sentinel }
func (e *RegistrationError) ProblemCode() string { return e.Code }

func IsRegistrationError(err error, code string) bool {
	var target *RegistrationError
	return errors.As(err, &target) && target.Code == code
}
