package repository

import (
	"context"
	"testing"

	"github.com/confhub/identity-svc/internal/domain"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestAuthorizationVersionChanges(t *testing.T) {
	pool := testutil.Database(t)
	repo := NewUsers(pool)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	user := &domain.User{Email: uuid.NewString() + "@example.test", PasswordHash: "test-hash", FirstName: "Test", LastName: "User", Role: domain.RoleUser, Locale: "en"}
	if err := repo.CreateInTx(ctx, tx, user); err != nil {
		t.Fatal(err)
	}
	assertVersion := func(want int64) {
		t.Helper()
		var got int64
		if err := tx.QueryRow(ctx, `SELECT auth_version FROM identity_svc.users WHERE id=$1`, user.ID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("authorization version %d, want %d", got, want)
		}
	}
	assertVersion(0)
	for _, wantOld := range []domain.Role{domain.RoleUser, domain.RoleVerifiedOrganizer} {
		old, err := repo.UpdateRole(ctx, tx, user.ID, domain.RoleVerifiedOrganizer)
		if err != nil || old != wantOld {
			t.Fatalf("role update: old=%q, err=%v", old, err)
		}
		assertVersion(1)
	}
	for _, active := range []bool{false, false} {
		if err := repo.SetActiveInTx(ctx, tx, user.ID, active); err != nil {
			t.Fatal(err)
		}
		assertVersion(2)
	}
	if err := repo.SetActiveInTx(ctx, tx, user.ID, true); err != nil {
		t.Fatal(err)
	}
	assertVersion(3)
	if err := repo.UpdatePasswordHash(ctx, tx, user.ID, "changed-hash"); err != nil {
		t.Fatal(err)
	}
	assertVersion(4)
}
