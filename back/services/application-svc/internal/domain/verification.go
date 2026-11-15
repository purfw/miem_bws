package domain

import (
	"time"

	"github.com/google/uuid"
)

type VerificationStatus string

const (
	VerificationPending  VerificationStatus = "pending"
	VerificationApproved VerificationStatus = "approved"
	VerificationRejected VerificationStatus = "rejected"
)

type OrganizerVerification struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	UserEmail       string
	UserName        string
	Organization    string
	Method          string
	Status          VerificationStatus
	RoleApplied     bool
	RejectionReason *string
	ReviewedBy      *uuid.UUID
	ReviewedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Documents       []VerificationDocument
}

type VerificationDocument struct {
	ID             uuid.UUID
	VerificationID uuid.UUID
	OriginalName   string
	StoredName     string
	MimeType       string
	SizeBytes      int64
	Bucket         string
	UploadedAt     time.Time
}
