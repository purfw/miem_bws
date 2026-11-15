package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestAttendeeRegistrationIsUniqueUnderConcurrencyAndScopedToOwner(t *testing.T) {
	pool := testutil.Database(t)
	repo := NewRegistrations(pool)
	ctx := context.Background()
	conference, user, other := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,status,created_by,max_participants) VALUES ($1,'Registration test',$2,'2027-02-01','2027-02-02','registration_open',$3,1)`, conference, uuid.NewString(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	var successful, duplicates atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Go(func() {
			_, err := repo.Register(ctx, conference, user, "attendee@example.test", "Attendee", "attendee")
			if err == nil {
				successful.Add(1)
			} else if domain.IsRegistrationError(err, "ALREADY_REGISTERED") {
				duplicates.Add(1)
			} else {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if successful.Load() != 1 || duplicates.Load() != 19 {
		t.Fatalf("successful=%d duplicates=%d", successful.Load(), duplicates.Load())
	}
	registration, err := repo.CurrentForUser(ctx, conference, user)
	if err != nil || registration == nil || registration.Status != domain.RegistrationConfirmed || registration.RoleAtConference != "attendee" {
		t.Fatalf("registration=%+v err=%v", registration, err)
	}
	foreign, err := repo.CurrentForUser(ctx, conference, other)
	if err != nil || foreign != nil {
		t.Fatal("registration exposed to another user")
	}
	var applications, registrations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM application_svc.applications WHERE conference_id=$1 AND user_id=$2`, conference, user).Scan(&applications); err != nil || applications != 0 {
		t.Fatal("attendee registration created a presentation application")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conference_svc.registrations WHERE conference_id=$1 AND user_id=$2`, conference, user).Scan(&registrations); err != nil || registrations != 1 {
		t.Fatal("duplicate registration rows")
	}
	waitlisted, err := repo.Register(ctx, conference, other, "other@example.test", "Other", "attendee")
	if err != nil || waitlisted.Status != domain.RegistrationWaitlisted || waitlisted.WaitlistPosition != 1 {
		t.Fatalf("waitlist=%+v err=%v", waitlisted, err)
	}
	if _, _, err := repo.Cancel(ctx, registration.ID, other); !domain.IsRegistrationError(err, "REGISTRATION_NOT_FOUND") {
		t.Fatal("another user cancelled participation")
	}
	cancelled, promoted, err := repo.Cancel(ctx, registration.ID, user)
	if err != nil || cancelled.Status != domain.RegistrationCancelled || promoted == nil || promoted.UserID != other {
		t.Fatalf("cancel=%+v promote=%+v err=%v", cancelled, promoted, err)
	}
	current, err := repo.CurrentForUser(ctx, conference, user)
	if err != nil || current == nil || current.Status != domain.RegistrationCancelled {
		t.Fatal("cancelled status was not persisted")
	}
	current, err = repo.CurrentForUser(ctx, conference, other)
	if err != nil || current == nil || current.Status != domain.RegistrationConfirmed {
		t.Fatal("waitlisted attendee was not promoted")
	}
}
