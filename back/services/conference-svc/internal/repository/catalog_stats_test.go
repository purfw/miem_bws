package repository

import (
	"context"
	"testing"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestPlatformStatsOnlyCountPublicConferences(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	repo := NewConferences(pool)
	before, err := repo.PlatformStats(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM conference_svc.conferences WHERE id=ANY($1::uuid[])`, ids) })
	for i, id := range ids {
		status := "registration_open"
		if i == 2 {
			status = "draft"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences
			(id,title,slug,start_date,end_date,status,created_by,publication_allowed,deleted_at)
			VALUES ($1,'Statistics test',$2,'2027-06-01','2027-06-02',$3,$4,$5,
			CASE WHEN $6 THEN now() ELSE NULL END)`, id, uuid.NewString(), status, uuid.New(), i != 1, i == 3); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.registration_counters(conference_id,confirmed_count) VALUES ($1,$2)`, id, (i+1)*3); err != nil {
			t.Fatal(err)
		}
	}
	for _, includeRate := range []bool{false, true} {
		after, err := repo.PlatformStats(ctx, includeRate)
		if err != nil {
			t.Fatal(err)
		}
		if after.Conferences != before.Conferences+1 || after.Participants != before.Participants+3 {
			t.Fatalf("private, deleted or draft registrations leaked into statistics: before=%+v after=%+v", before, after)
		}
	}
}
