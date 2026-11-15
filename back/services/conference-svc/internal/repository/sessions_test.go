package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestSessionValidationAndEvents(t *testing.T) {
	pool := testutil.Database(t)
	repo := NewConferences(pool)
	ctx := context.Background()
	var first, second uuid.UUID
	date, _ := time.Parse("2006-01-02", "2026-11-01")
	for _, id := range []*uuid.UUID{&first, &second} {
		*id = uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,created_by) VALUES($1,'Test',$2,$3,$3,$4)`, *id, uuid.NewString(), date, uuid.New())
		if err != nil {
			t.Fatal(err)
		}
	}
	section := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.sections(id,conference_id,title) VALUES($1,$2,'Other section')`, section, second); err != nil {
		t.Fatal(err)
	}
	for conferenceID, expected := range map[uuid.UUID]bool{first: false, second: true, uuid.New(): false} {
		belongs, err := repo.SectionBelongsToConference(ctx, conferenceID, section)
		if err != nil || belongs != expected {
			t.Fatalf("section belongs=%v, expected=%v, error=%v", belongs, expected, err)
		}
	}
	valid := domain.ScheduleSlot{ConferenceID: first, Title: "Valid session", SlotDate: date, StartTime: "09:00", EndTime: "10:00"}
	for name, mutate := range map[string]func(*domain.ScheduleSlot){
		"foreign section":  func(s *domain.ScheduleSlot) { s.SectionID = &section },
		"out of range":     func(s *domain.ScheduleSlot) { s.SlotDate = date.AddDate(0, 0, 1) },
		"long type":        func(s *domain.ScheduleSlot) { s.SessionType = strings.Repeat("x", 51) },
		"reverse interval": func(s *domain.ScheduleSlot) { s.EndTime = "08:00" },
	} {
		t.Run(name, func(t *testing.T) {
			slot := valid
			mutate(&slot)
			if err := repo.CreateSession(ctx, &slot); !errors.Is(err, httputil.ErrValidation) {
				t.Fatalf("create: %v", err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err := repo.ReplaceScheduleInTx(ctx, tx, first, []domain.ScheduleSlot{slot}); !errors.Is(err, httputil.ErrValidation) {
				t.Fatalf("replace: %v", err)
			}
		})
	}
	if err := repo.CreateSession(ctx, &valid); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conference_svc.outbox_events WHERE aggregate_id=$1 AND event_type='schedule.updated'`, first.String()).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("events %d, want one successful mutation", events)
	}
}
