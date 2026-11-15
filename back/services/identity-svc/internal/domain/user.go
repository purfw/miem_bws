package domain

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser              Role = "user"
	RoleVerifiedOrganizer Role = "verified_organizer"
	RoleOrganizerMember   Role = "organizer_member"
	RoleAdmin             Role = "admin"

	// Accept legacy roles until pre-migration access tokens expire.
	RoleStudent   Role = "student"
	RoleModerator Role = "moderator"
)

type User struct {
	ID            uuid.UUID
	Email         string
	PasswordHash  string
	AuthVersion   int64
	FirstName     string
	LastName      string
	Patronymic    *string
	Affiliation   *string
	Role          Role
	IsActive      bool
	EmailVerified bool
	Locale        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (u *User) FullName() string {
	if u.Patronymic != nil && *u.Patronymic != "" {
		return u.FirstName + " " + *u.Patronymic + " " + u.LastName
	}
	return u.FirstName + " " + u.LastName
}
