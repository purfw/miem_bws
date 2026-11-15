package handler

import (
	"testing"
	"time"

	"github.com/confhub/conference-svc/internal/domain"
)

func TestCardRegistrationAvailability(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	for _, test := range []struct {
		name     string
		status   domain.Status
		deadline *time.Time
		open     bool
	}{
		{"open without deadline", domain.StatusRegistrationOpen, nil, true},
		{"open before deadline", domain.StatusRegistrationOpen, &future, true},
		{"expired deadline", domain.StatusRegistrationOpen, &past, false},
		{"closed before deadline", domain.StatusRegistrationClosed, &future, false},
		{"draft", domain.StatusDraft, nil, false},
		{"cancelled", domain.StatusCancelled, nil, false},
		{"completed", domain.StatusCompleted, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			card := toCard(&domain.Conference{Status: test.status, RegistrationDeadline: test.deadline})
			if card.RegistrationOpen != test.open {
				t.Fatalf("registration_open=%v, expected %v", card.RegistrationOpen, test.open)
			}
		})
	}
}
