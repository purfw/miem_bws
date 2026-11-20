package domain

import "time"

// platformZone is the time zone in which conference dates are interpreted.
var platformZone = time.FixedZone("MSK", 3*60*60)

// LifecycleToday returns the current calendar date in the platform time zone
// as a UTC midnight, the same representation as stored conference dates.
func LifecycleToday(now time.Time) time.Time {
	y, m, d := now.In(platformZone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// LifecycleSteps returns the status transitions a conference is due for at
// now, in order. Drafts, cancelled, completed and archived conferences never
// move automatically; archiving stays a manual decision.
func LifecycleSteps(status Status, start, end time.Time, deadline *time.Time, now time.Time) []Status {
	today := LifecycleToday(now)
	var target Status
	switch {
	case end.Before(today):
		target = StatusCompleted
	case !start.After(today):
		target = StatusInProgress
	case deadline != nil && !deadline.After(now):
		target = StatusRegistrationClosed
	default:
		return nil
	}
	path := []Status{StatusRegistrationClosed, StatusInProgress, StatusCompleted}
	var from int
	switch status {
	case StatusRegistrationOpen:
		from = 0
	case StatusRegistrationClosed:
		from = 1
	case StatusInProgress:
		from = 2
	default:
		return nil
	}
	var steps []Status
	for _, step := range path[from:] {
		steps = append(steps, step)
		if step == target {
			return steps
		}
	}
	return nil // already past the target
}
