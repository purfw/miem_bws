package domain

import (
	"slices"
	"testing"
	"time"
)

func TestLifecycleSteps(t *testing.T) {
	// 2027-03-10 01:00 MSK is still 2027-03-09 in UTC.
	now := time.Date(2027, 3, 9, 22, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2027, 3, d, 0, 0, 0, 0, time.UTC) }
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	cases := []struct {
		name       string
		status     Status
		start, end time.Time
		deadline   *time.Time
		want       []Status
	}{
		{"nothing due", StatusRegistrationOpen, day(20), day(21), &future, nil},
		{"deadline passed", StatusRegistrationOpen, day(20), day(21), &past, []Status{StatusRegistrationClosed}},
		{"closed waits for start", StatusRegistrationClosed, day(20), day(21), &past, nil},
		{"starts today in MSK", StatusRegistrationOpen, day(10), day(12), nil, []Status{StatusRegistrationClosed, StatusInProgress}},
		{"closed starts", StatusRegistrationClosed, day(8), day(12), nil, []Status{StatusInProgress}},
		{"ended long ago", StatusRegistrationOpen, day(1), day(2), nil, []Status{StatusRegistrationClosed, StatusInProgress, StatusCompleted}},
		{"in progress ends", StatusInProgress, day(1), day(9), nil, []Status{StatusCompleted}},
		{"last day is still running", StatusInProgress, day(1), day(10), nil, nil},
		{"draft never moves", StatusDraft, day(1), day(2), nil, nil},
		{"cancelled never moves", StatusCancelled, day(1), day(2), nil, nil},
		{"completed stays", StatusCompleted, day(1), day(2), nil, nil},
	}
	for _, tc := range cases {
		got := LifecycleSteps(tc.status, tc.start, tc.end, tc.deadline, now)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
		from := tc.status
		for _, step := range got {
			if !CanTransition(from, step) {
				t.Errorf("%s: %s -> %s is not an allowed transition", tc.name, from, step)
			}
			from = step
		}
	}
}
