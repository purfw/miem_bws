package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/confhub/application-svc/internal/storage"
	"github.com/jackc/pgx/v5"
)

type objectStore interface {
	PutAt(context.Context, string, string, int64, io.Reader) error
	Delete(context.Context, string) error
	Presign(context.Context, string, string) (string, error)
	Bucket() string
}

func (s *Applications) prepareObject(ctx context.Context, ext string) (string, error) {
	key := storage.NewObjectKey(ext)
	_, err := s.repo.Pool().Exec(ctx, `INSERT INTO application_svc.storage_deletions (bucket,stored_name,next_attempt_at) VALUES ($1,$2,now()+interval '1 hour')`, s.store.Bucket(), key)
	return key, err
}

func (s *Applications) putTracked(ctx context.Context, ext, mime string, size int64, body io.Reader) (string, error) {
	key, err := s.prepareObject(ctx, ext)
	if err != nil {
		return "", err
	}
	err = s.store.PutAt(ctx, key, mime, size, body)
	if err != nil {
		s.expediteCleanup(ctx, key)
	}
	return key, err
}

func (s *Applications) expediteCleanup(ctx context.Context, key string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := s.repo.Pool().Exec(cleanup, `UPDATE application_svc.storage_deletions SET next_attempt_at=now() WHERE bucket=$1 AND stored_name=$2 AND status='pending'`, s.store.Bucket(), key); err != nil {
		slog.Warn("storage cleanup remains scheduled", "err", err)
	}
}

func (s *Applications) RunStorageCleanup(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 20; i++ {
			worked, err := s.cleanupObject(ctx)
			if err != nil {
				slog.Error("storage cleanup", "err", err)
				break
			}
			if !worked {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Applications) cleanupObject(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var id, bucket, key string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,bucket,stored_name,attempts FROM application_svc.storage_deletions WHERE status='pending' AND next_attempt_at<=now() ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &bucket, &key, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var referenced bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM application_svc.application_files WHERE bucket=$1 AND stored_name=$2
        UNION ALL SELECT 1 FROM application_svc.verification_documents WHERE bucket=$1 AND stored_name=$2
        UNION ALL SELECT 1 FROM application_svc.participation_documents WHERE bucket=$1 AND stored_name=$2
    )`, bucket, key).Scan(&referenced)
	if err != nil {
		return false, err
	}
	if !referenced {
		if bucket != s.store.Bucket() {
			err = errors.New("cleanup bucket does not match configured storage")
		} else {
			err = s.store.Delete(ctx, key)
		}
		if err != nil {
			attempts++
			status := "pending"
			if attempts >= 10 {
				status = "dead"
			}
			lastError := err.Error()
			if len(lastError) > 2000 {
				lastError = lastError[:2000]
			}
			_, err = tx.Exec(ctx, `UPDATE application_svc.storage_deletions SET status=$2,attempts=$3,last_error=$4,next_attempt_at=now()+make_interval(secs=>$5) WHERE id=$1`, id, status, attempts, lastError, min(3600, 5*(1<<min(attempts, 10))))
			if err != nil {
				return false, err
			}
			return true, tx.Commit(ctx)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE application_svc.storage_deletions SET status='completed',completed_at=now(),last_error=NULL WHERE id=$1`, id)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
