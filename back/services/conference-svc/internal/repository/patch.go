package repository

import (
	"fmt"
	"strings"
	"time"

	"github.com/confhub/conference-svc/internal/domain"
)

// ConferencePatch is a sparse update DTO. Only non-nil fields are written.
type ConferencePatch struct {
	Title                *string
	TitleEn              *string
	Slug                 *string
	Description          *string
	DescriptionEn        *string
	Status               *domain.Status
	Format               *domain.Format
	StartDate            *time.Time
	EndDate              *time.Time
	RegistrationDeadline *time.Time
	MaxParticipants      *int
	Location             *string
	City                 *string
	CoverImageURL        *string
	IsFeatured           *bool
	Topics               *[]string

	// ClearRegistrationDeadline and ClearMaxParticipants set the column to
	// NULL. For optional text fields an empty string clears the value.
	ClearRegistrationDeadline bool
	ClearMaxParticipants      bool
}

// nullIfBlank stores an empty optional text field as NULL.
func nullIfBlank(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// StatusOnly reports whether the patch changes nothing but the status. Status
// changes skip date validation so that events created before the current
// date policy can still move through their lifecycle.
func (p ConferencePatch) StatusOnly() bool {
	status := p.Status
	p.Status = nil
	return status != nil && p == ConferencePatch{}
}

func (p ConferencePatch) Build() ([]string, []any) {
	var set []string
	var args []any
	add := func(col string, val any) {
		args = append(args, val)
		set = append(set, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Title != nil {
		add("title", *p.Title)
	}
	if p.TitleEn != nil {
		add("title_en", nullIfBlank(*p.TitleEn))
	}
	if p.Slug != nil {
		add("slug", *p.Slug)
	}
	if p.Description != nil {
		add("description", nullIfBlank(*p.Description))
	}
	if p.DescriptionEn != nil {
		add("description_en", nullIfBlank(*p.DescriptionEn))
	}
	if p.Status != nil {
		args = append(args, string(*p.Status))
		set = append(set, fmt.Sprintf("status = $%d::conference_svc.conference_status", len(args)))
	}
	if p.Format != nil {
		args = append(args, string(*p.Format))
		set = append(set, fmt.Sprintf("format = $%d::conference_svc.conference_format", len(args)))
	}
	if p.StartDate != nil {
		add("start_date", *p.StartDate)
	}
	if p.EndDate != nil {
		add("end_date", *p.EndDate)
	}
	if p.ClearRegistrationDeadline {
		set = append(set, "registration_deadline = NULL")
	} else if p.RegistrationDeadline != nil {
		add("registration_deadline", *p.RegistrationDeadline)
	}
	if p.ClearMaxParticipants {
		set = append(set, "max_participants = NULL")
	} else if p.MaxParticipants != nil {
		add("max_participants", *p.MaxParticipants)
	}
	if p.Location != nil {
		add("location", nullIfBlank(*p.Location))
	}
	if p.City != nil {
		add("city", nullIfBlank(*p.City))
	}
	if p.CoverImageURL != nil {
		add("cover_image_url", nullIfBlank(*p.CoverImageURL))
	}
	if p.IsFeatured != nil {
		add("is_featured", *p.IsFeatured)
	}
	if p.Topics != nil {
		add("topics", *p.Topics)
	}
	return set, args
}
