package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestVerificationDocumentRechecksDecision(t *testing.T) {
	pool := testutil.Database(t)
	repo := NewApplications(pool)
	ctx := context.Background()
	verification := &domain.OrganizerVerification{UserID: uuid.New(), UserEmail: "test@example.test", UserName: "Test", Organization: "Test", Method: "documents", Status: domain.VerificationPending}
	if err := repo.CreateVerification(ctx, verification); err != nil {
		t.Fatal(err)
	}
	document := &domain.VerificationDocument{VerificationID: verification.ID, OriginalName: "test.pdf", StoredName: uuid.NewString(), MimeType: "application/pdf", SizeBytes: 100, Bucket: "test"}
	if err := repo.AddVerificationDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ModerateVerification(ctx, verification.ID, uuid.New(), domain.VerificationApproved, nil); err != nil {
		t.Fatal(err)
	}
	document.ID = uuid.New()
	if err := repo.AddVerificationDocument(ctx, document); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("late upload accepted: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM application_svc.verification_documents WHERE verification_id=$1`, verification.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("documents=%d error=%v", count, err)
	}
}
