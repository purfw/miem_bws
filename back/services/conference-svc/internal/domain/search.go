package domain

import (
	"time"

	"github.com/google/uuid"
)

// SearchDocument is an Elasticsearch projection rebuilt from PostgreSQL.
type SearchDocument struct {
	ID                 uuid.UUID `json:"-"`
	Title              string    `json:"title"`
	TitleEn            *string   `json:"title_en,omitempty"`
	Slug               string    `json:"slug"`
	Description        *string   `json:"description,omitempty"`
	DescriptionEn      *string   `json:"description_en,omitempty"`
	Format             string    `json:"format"`
	Status             string    `json:"status"`
	StartDate          time.Time `json:"start_date"`
	EndDate            time.Time `json:"end_date"`
	City               *string   `json:"city,omitempty"`
	Topics             []string  `json:"topics"`
	OrganizerNames     []string  `json:"organizer_names"`
	PublicationAllowed bool      `json:"publication_allowed"`
	Deleted            bool      `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (d SearchDocument) Searchable() bool {
	return !d.Deleted && d.PublicationAllowed && d.Status != "draft" && d.Status != "cancelled"
}
