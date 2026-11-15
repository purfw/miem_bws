package domain

import (
	"time"

	"github.com/google/uuid"
)

type ParticipationDocument struct {
	ID                uuid.UUID
	RegistrationID    uuid.UUID
	UserID            uuid.UUID
	ConferenceID      uuid.UUID
	ConferenceTitle   string
	ParticipantName   string
	StoredName        string
	Bucket            string
	VerificationHash  []byte
	DownloadTokenHash []byte
	DownloadExpiresAt time.Time
	IssuedAt          time.Time
}
