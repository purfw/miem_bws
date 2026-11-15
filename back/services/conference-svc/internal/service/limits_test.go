package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/conference-svc/internal/repository"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentDailyLimit(t *testing.T) {
	pool := testutil.Database(t)
	svc := New(repository.NewConferences(pool))
	owner := uuid.New()
	var successful, limited atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 15; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err := svc.Create(context.Background(), &CreateInput{Title: fmt.Sprintf("Test %d", i), Slug: fmt.Sprintf("test-%s-%d", owner, i), StartDate: start, EndDate: start.Add(24 * time.Hour), Format: domain.FormatOnline, CreatedBy: owner})
			if err == nil {
				successful.Add(1)
			} else if errors.Is(err, httputil.ErrTooMany) {
				limited.Add(1)
			} else {
				t.Errorf("create: %v", err)
			}
		}(i)
	}
	wait.Wait()
	if successful.Load() != 10 || limited.Load() != 5 {
		t.Fatalf("successful=%d limited=%d", successful.Load(), limited.Load())
	}
}
