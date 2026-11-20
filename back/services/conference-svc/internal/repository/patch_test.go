package repository

import (
	"slices"
	"testing"

	"github.com/confhub/conference-svc/internal/domain"
)

func TestPatchClearsOptionalFields(t *testing.T) {
	empty, title := " ", "Title"
	set, args := ConferencePatch{
		Title: &title, Description: &empty, City: &empty,
		ClearRegistrationDeadline: true, ClearMaxParticipants: true,
	}.Build()
	want := []string{"title = $1", "description = $2", "registration_deadline = NULL", "max_participants = NULL", "city = $3"}
	if !slices.Equal(set, want) {
		t.Fatalf("set=%v", set)
	}
	if args[0] != "Title" || args[1] != nil || args[2] != nil {
		t.Fatalf("args=%v", args)
	}
}

func TestPatchStatusOnly(t *testing.T) {
	status := domain.StatusCompleted
	title := "x"
	if !(ConferencePatch{Status: &status}).StatusOnly() {
		t.Fatal("status patch")
	}
	if (ConferencePatch{Status: &status, Title: &title}).StatusOnly() || (ConferencePatch{}).StatusOnly() {
		t.Fatal("mixed or empty patch is not status-only")
	}
	if (ConferencePatch{Status: &status, ClearMaxParticipants: true}).StatusOnly() {
		t.Fatal("clearing a field is not status-only")
	}
}
