package subscriptions

import (
	"testing"
	"time"
)

func TestDeadlineDue(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		offset time.Duration
		due    bool
	}{{-time.Second, false}, {0, false}, {time.Hour, true}, {24 * time.Hour, true}, {25 * time.Hour, false}} {
		deadline := now.Add(test.offset)
		if deadlineDue(&deadline, now) != test.due {
			t.Fatalf("offset %s", test.offset)
		}
	}
	if deadlineDue(nil, now) {
		t.Fatal("nil deadline")
	}
}

func TestUnsubscribeToken(t *testing.T) {
	a, ah, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	b, bh, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 43 || len(ah) != 32 || string(ah) == string(bh) {
		t.Fatal("invalid unsubscribe tokens")
	}
}
