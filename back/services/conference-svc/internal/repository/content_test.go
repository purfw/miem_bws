package repository

import (
	"context"
	"testing"
	"time"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/domain"
)

func TestReplaceSectionsKeepsTalkLinks(t *testing.T) {
	pool := testutil.Database(t)
	repo := NewConferences(pool)
	ctx := context.Background()
	conference, kept, dropped, slot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	day := time.Now().AddDate(0, 2, 0)
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,created_by)
		VALUES($1,'Content',$2,$3,$3,$4)`, conference, uuid.NewString(), day, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.sections(id,conference_id,title,sort_order) VALUES ($1,$3,'A',0),($2,$3,'B',1)`, kept, dropped, conference); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.schedule_slots(id,conference_id,section_id,title,slot_date,start_time,end_time)
		VALUES ($1,$2,$3,'Talk',$4,'10:00','11:00')`, slot, conference, kept, day); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	foreign := uuid.New() // an id from another conference must not be updated
	if err := repo.ReplaceSectionsInTx(ctx, tx, conference, []domain.Section{
		{ID: kept, Title: "A renamed", SortOrder: 1},
		{ID: foreign, Title: "New"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	sections, err := repo.Sections(ctx, conference)
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for _, s := range sections {
		titles[s.Title] = true
		if s.ID == dropped || s.ID == foreign {
			t.Fatalf("unexpected section id %v", s.ID)
		}
	}
	if len(sections) != 2 || !titles["A renamed"] || !titles["New"] {
		t.Fatalf("sections=%+v", sections)
	}
	var section *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT section_id FROM conference_svc.schedule_slots WHERE id=$1`, slot).Scan(&section); err != nil || section == nil || *section != kept {
		t.Fatalf("talk lost its section: %v %v", section, err)
	}
}
