package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/confhub/conference-svc/internal/domain"
)

// The Replace* methods make the stored list equal to the given one. Items
// with a known ID are updated in place, so references survive: schedule slots
// keep their section, and organizer rows keep the e-mail and account link set
// by an administrator. Items with an unknown or empty ID are inserted, and
// stored items missing from the list are deleted.

func keepIDs(ctx context.Context, tx pgx.Tx, table string, conferenceID uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT id FROM conference_svc.%s WHERE conference_id=$1 FOR UPDATE`, table), conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func deleteMissing(ctx context.Context, tx pgx.Tx, table string, conferenceID uuid.UUID, keep []uuid.UUID) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM conference_svc.%s WHERE conference_id=$1 AND id <> ALL($2::uuid[])`, table), conferenceID, keep)
	return err
}

func (r *Conferences) ReplaceSectionsInTx(ctx context.Context, tx pgx.Tx, conferenceID uuid.UUID, items []domain.Section) error {
	existing, err := keepIDs(ctx, tx, "sections", conferenceID)
	if err != nil {
		return err
	}
	// sort_order is unique per conference: move stored rows out of the way
	// so that reordering cannot collide halfway through.
	if _, err := tx.Exec(ctx, `UPDATE conference_svc.sections SET sort_order = -1 - sort_order - (SELECT max(abs(sort_order)) + 1 FROM conference_svc.sections WHERE conference_id=$1) WHERE conference_id=$1`, conferenceID); err != nil {
		return err
	}
	keep := make([]uuid.UUID, 0, len(items))
	for i := range items {
		s := &items[i]
		if existing[s.ID] {
			if _, err := tx.Exec(ctx, `UPDATE conference_svc.sections SET title=$2,title_en=$3,description=$4,
				sort_order=$5,moderator_name=$6,moderator_affiliation=$7 WHERE id=$1`,
				s.ID, s.Title, s.TitleEn, s.Description, s.SortOrder, s.ModeratorName, s.ModeratorAffiliation); err != nil {
				return err
			}
		} else {
			s.ID = uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.sections
				(id,conference_id,title,title_en,description,sort_order,moderator_name,moderator_affiliation)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, s.ID, conferenceID, s.Title, s.TitleEn, s.Description,
				s.SortOrder, s.ModeratorName, s.ModeratorAffiliation); err != nil {
				return err
			}
		}
		keep = append(keep, s.ID)
	}
	return deleteMissing(ctx, tx, "sections", conferenceID, keep)
}

func (r *Conferences) ReplaceOrganizersInTx(ctx context.Context, tx pgx.Tx, conferenceID uuid.UUID, items []domain.Organizer) error {
	existing, err := keepIDs(ctx, tx, "organizers", conferenceID)
	if err != nil {
		return err
	}
	keep := make([]uuid.UUID, 0, len(items))
	for i := range items {
		o := &items[i]
		if o.Kind == "" {
			o.Kind = domain.OrganizerKindOrganizer
		}
		if existing[o.ID] {
			if _, err := tx.Exec(ctx, `UPDATE conference_svc.organizers SET name=$2,affiliation=$3,role=$4,kind=$5,
				job_title=$6,photo_url=$7,sort_order=$8 WHERE id=$1`,
				o.ID, o.Name, o.Affiliation, o.Role, o.Kind, o.JobTitle, o.PhotoURL, o.SortOrder); err != nil {
				return err
			}
		} else {
			o.ID = uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.organizers
				(id,conference_id,name,affiliation,role,kind,job_title,photo_url,sort_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, o.ID, conferenceID, o.Name, o.Affiliation, o.Role,
				o.Kind, o.JobTitle, o.PhotoURL, o.SortOrder); err != nil {
				return err
			}
		}
		keep = append(keep, o.ID)
	}
	return deleteMissing(ctx, tx, "organizers", conferenceID, keep)
}

func (r *Conferences) ReplacePartnersInTx(ctx context.Context, tx pgx.Tx, conferenceID uuid.UUID, items []domain.Partner) error {
	existing, err := keepIDs(ctx, tx, "partners", conferenceID)
	if err != nil {
		return err
	}
	keep := make([]uuid.UUID, 0, len(items))
	for i := range items {
		p := &items[i]
		if existing[p.ID] {
			if _, err := tx.Exec(ctx, `UPDATE conference_svc.partners SET name=$2,logo_url=$3,website_url=$4,
				tier=$5,sort_order=$6 WHERE id=$1`, p.ID, p.Name, p.LogoURL, p.WebsiteURL, p.Tier, p.SortOrder); err != nil {
				return err
			}
		} else {
			p.ID = uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO conference_svc.partners
				(id,conference_id,name,logo_url,website_url,tier,sort_order) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				p.ID, conferenceID, p.Name, p.LogoURL, p.WebsiteURL, p.Tier, p.SortOrder); err != nil {
				return err
			}
		}
		keep = append(keep, p.ID)
	}
	return deleteMissing(ctx, tx, "partners", conferenceID, keep)
}
