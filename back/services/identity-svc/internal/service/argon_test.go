package service

import (
	"context"
	"errors"
	"github.com/confhub/shared/httputil"
	"testing"
	"time"
)

func TestHashContextCancellation(t *testing.T) {
	for i := 0; i < cap(hashSlots); i++ {
		hashSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(hashSlots); i++ {
			<-hashSlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := HashPasswordContext(ctx, "long-password-123"); !errors.Is(err, httputil.ErrUnavailable) {
		t.Fatalf("error %v", err)
	}
}

func TestRejectUnsafeHashParameters(t *testing.T) {
	for _, hash := range []string{"$argon2id$v=19$m=4294967295,t=3,p=4$YWJj$YWJj", "$argon2id$v=19$m=65536,t=0,p=0$YWJj$YWJj"} {
		if _, err := VerifyPassword("password", hash); err == nil {
			t.Fatal("unsafe hash accepted")
		}
	}
}
