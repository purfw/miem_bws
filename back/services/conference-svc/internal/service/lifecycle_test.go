package service

import (
	"context"
	"testing"
	"time"

	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestAdvanceLifecycleCompletesFinishedConference(t *testing.T) {
	pool := testutil.Database(t)
	svc := New(repository.NewConferences(pool))
	ctx := context.Background()
	id := uuid.New()
	// A legacy event: its start date predates the current date policy, which
	// must not block automatic status changes.
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences
		(id,title,slug,start_date,end_date,created_by,status,publication_allowed)
		VALUES($1,'Finished',$2,'2026-09-10','2026-09-12',$3,'registration_open',true)`,
		id, uuid.NewString(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if _, err := svc.AdvanceLifecycle(ctx, now); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM conference_svc.conferences WHERE id=$1`, id).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("status=%q err=%v", status, err)
	}
	rows, err := pool.Query(ctx, `SELECT payload->>'new_status' FROM conference_svc.outbox_events
		WHERE aggregate_id=$1 AND event_type='conference.status_changed' ORDER BY created_at, payload->>'new_status' DESC`, id)
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		steps = append(steps, s)
	}
	rows.Close()
	if len(steps) != 3 {
		t.Fatalf("want one status event per step, got %v", steps)
	}
	// A second run is a no-op.
	if _, err := svc.AdvanceLifecycle(ctx, now); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conference_svc.outbox_events WHERE aggregate_id=$1`, id).Scan(&events); err != nil || events != 3 {
		t.Fatalf("events=%d err=%v", events, err)
	}
}
