package domain

import "testing"

func TestApplicationLifecycle(t *testing.T) {
	states := []Status{StatusSubmitted, StatusUnderReview, StatusRevisionRequested, StatusAccepted, StatusRejected, StatusWithdrawn, Status("unknown")}
	edges := map[[2]Status]bool{{StatusSubmitted, StatusUnderReview}: true, {StatusSubmitted, StatusWithdrawn}: true, {StatusUnderReview, StatusRevisionRequested}: true, {StatusUnderReview, StatusAccepted}: true, {StatusUnderReview, StatusRejected}: true, {StatusUnderReview, StatusWithdrawn}: true, {StatusRevisionRequested, StatusSubmitted}: true, {StatusRevisionRequested, StatusWithdrawn}: true}
	for _, from := range states {
		for _, to := range states {
			want := from == to || edges[[2]Status{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("%s->%s=%v want=%v", from, to, got, want)
			}
		}
	}
}

func TestFormAndRecommendationValidation(t *testing.T) {
	for _, form := range []Form{FormSpeaker, FormPoster, FormListener} {
		if !ValidForm(form) {
			t.Error("valid form rejected")
		}
	}
	if ValidForm("unsupported") {
		t.Fatal("unsupported form accepted")
	}
	for _, recommendation := range []Recommendation{RecAccept, RecMinor, RecMajor, RecReject} {
		if !ValidRecommendation(recommendation) {
			t.Error("valid recommendation rejected")
		}
	}
	if ValidRecommendation("unsupported") {
		t.Fatal("unsupported recommendation accepted")
	}
}
