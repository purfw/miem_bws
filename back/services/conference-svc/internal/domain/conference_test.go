package domain

import "testing"

func TestConferenceLifecycle(t *testing.T) {
	states := []Status{StatusDraft, StatusRegistrationOpen, StatusRegistrationClosed, StatusInProgress, StatusCompleted, StatusCancelled, StatusArchived, Status("unknown")}
	edges := map[[2]Status]bool{{StatusDraft, StatusRegistrationOpen}: true, {StatusDraft, StatusCancelled}: true, {StatusRegistrationOpen, StatusRegistrationClosed}: true, {StatusRegistrationOpen, StatusCancelled}: true, {StatusRegistrationClosed, StatusRegistrationOpen}: true, {StatusRegistrationClosed, StatusInProgress}: true, {StatusRegistrationClosed, StatusCancelled}: true, {StatusInProgress, StatusCompleted}: true, {StatusCompleted, StatusArchived}: true}
	for _, from := range states {
		for _, to := range states {
			want := from == to || edges[[2]Status{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("%s->%s=%v want=%v", from, to, got, want)
			}
		}
	}
}

func TestFormatNormalization(t *testing.T) {
	for input, want := range map[string]Format{"offline": FormatOffline, "in_person": FormatOffline, "in-person": FormatOffline, "hybrid": FormatHybrid, "online": FormatOnline} {
		if got, ok := NormalizeFormat(input); !ok || got != want {
			t.Errorf("%s=%s %v", input, got, ok)
		}
	}
	if _, ok := NormalizeFormat("unsupported"); ok {
		t.Fatal("unsupported format accepted")
	}
}
