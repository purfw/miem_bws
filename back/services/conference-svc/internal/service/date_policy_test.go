package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestCreateRejectsOldDatesBeforeDatabaseAccess(t *testing.T) {
	start := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	_, err := New(nil).Create(context.Background(), &CreateInput{Title: "Test", Slug: "test", StartDate: start, EndDate: start.AddDate(0, 0, 1)})
	if !errors.Is(err, httputil.ErrValidation) {
		t.Fatalf("create error=%v", err)
	}
}

func TestCreateAndUpdateRespectDatePolicyWithoutChangingLegacyEvents(t *testing.T) {
	pool := testutil.Database(t)
	svc := New(repository.NewConferences(pool))
	ctx := context.Background()
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, &CreateInput{Title: "Date boundary", Slug: uuid.NewString(), StartDate: start, EndDate: start.AddDate(0, 0, 1), CreatedBy: uuid.New(), Format: domain.FormatOnline})
	if err != nil {
		t.Fatal(err)
	}
	old := start.AddDate(0, 0, -1)
	if _, err := svc.Update(ctx, event.ID, repository.ConferencePatch{StartDate: &old}); !errors.Is(err, httputil.ErrValidation) {
		t.Fatalf("old PATCH=%v", err)
	}
	var stored time.Time
	if err := pool.QueryRow(ctx, `SELECT start_date FROM conference_svc.conferences WHERE id=$1`, event.ID).Scan(&stored); err != nil || !stored.Equal(start) {
		t.Fatal("rejected edit changed stored date")
	}
	if _, err := svc.Update(ctx, event.ID, repository.ConferencePatch{StartDate: &start}); err != nil {
		t.Fatal("exact boundary update failed", err)
	}
	legacy := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,created_by) VALUES ($1,'Legacy',$2,'2026-12-31','2027-01-02',$3)`, legacy, uuid.NewString(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	title := "Edited legacy"
	if _, err := svc.Update(ctx, legacy, repository.ConferencePatch{Title: &title}); !errors.Is(err, httputil.ErrValidation) {
		t.Fatal("partial update bypassed effective start-date validation")
	}
	if err := pool.QueryRow(ctx, `SELECT start_date FROM conference_svc.conferences WHERE id=$1`, legacy).Scan(&stored); err != nil || !stored.Equal(old) {
		t.Fatal("legacy date was changed automatically")
	}
	if _, err := svc.Update(ctx, legacy, repository.ConferencePatch{StartDate: &start}); err != nil {
		t.Fatal("explicit correction to a valid date failed", err)
	}
}
