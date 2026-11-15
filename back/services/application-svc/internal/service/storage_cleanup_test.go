package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/confhub/application-svc/internal/repository"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

type fakeObjectStore struct {
	fail    bool
	deleted []string
}

func (s *fakeObjectStore) PutAt(context.Context, string, string, int64, io.Reader) error { return nil }
func (s *fakeObjectStore) Delete(_ context.Context, key string) error {
	if s.fail {
		return errors.New("storage unavailable")
	}
	s.deleted = append(s.deleted, key)
	return nil
}
func (s *fakeObjectStore) Presign(context.Context, string, string) (string, error) { return "", nil }
func (s *fakeObjectStore) Bucket() string                                          { return "test-bucket" }

func TestStorageCleanupRetriesAndKeepsReferencedFiles(t *testing.T) {
	pool := testutil.Database(t)
	store := &fakeObjectStore{}
	svc := &Applications{repo: repository.NewApplications(pool), store: store}
	ctx := context.Background()
	key, err := svc.prepareObject(ctx, ".pdf")
	if err != nil {
		t.Fatal(err)
	}
	var scheduled bool
	if err := pool.QueryRow(ctx, `SELECT next_attempt_at>now()+interval '59 minutes' FROM application_svc.storage_deletions WHERE stored_name=$1`, key).Scan(&scheduled); err != nil || !scheduled {
		t.Fatalf("intent not durable: %v", err)
	}
	svc.expediteCleanup(ctx, key)
	store.fail = true
	for i := 0; i < 10; i++ {
		if _, err := pool.Exec(ctx, `UPDATE application_svc.storage_deletions SET next_attempt_at=now() WHERE stored_name=$1`, key); err != nil {
			t.Fatal(err)
		}
		if worked, err := svc.cleanupObject(ctx); err != nil || !worked {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status,attempts FROM application_svc.storage_deletions WHERE stored_name=$1`, key).Scan(&status, &attempts); err != nil || status != "dead" || attempts != 10 {
		t.Fatalf("dead letter: %s %d %v", status, attempts, err)
	}
	store.fail = false
	if _, err := pool.Exec(ctx, `UPDATE application_svc.storage_deletions SET status='pending',attempts=0,next_attempt_at=now() WHERE stored_name=$1`, key); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.cleanupObject(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != key {
		t.Fatal("orphan was not removed")
	}
	kept, err := svc.prepareObject(ctx, ".pdf")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(uuid.NewString()))
	if _, err := pool.Exec(ctx, `INSERT INTO application_svc.participation_documents(registration_id,user_id,conference_id,conference_title,participant_name,stored_name,bucket,verification_hash,download_token_hash,download_expires_at)
        VALUES($1,$2,$3,'Test','Test',$4,$5,$6,$6,now()+interval '1 day')`, uuid.New(), uuid.New(), uuid.New(), kept, store.Bucket(), hash[:]); err != nil {
		t.Fatal(err)
	}
	svc.expediteCleanup(ctx, kept)
	if _, err := svc.cleanupObject(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != 1 {
		t.Fatal("referenced object was deleted")
	}
}
