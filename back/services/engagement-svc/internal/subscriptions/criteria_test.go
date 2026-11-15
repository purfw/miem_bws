package subscriptions

import (
	"github.com/confhub/engagement-svc/internal/client"
	"testing"
)

func TestCriteriaValidationAndMatching(t *testing.T) {
	for _, criteria := range []Criteria{{}, {Formats: []string{"invalid"}}, {From: "invalid"}, {From: "2026-11-01", To: "2026-10-01"}, {Topics: []string{" "}}} {
		if err := criteria.normalize(); err == nil {
			t.Fatalf("invalid criteria accepted: %+v", criteria)
		}
	}
	criteria := Criteria{Topics: []string{"Физика", "Физика"}, Formats: []string{"online"}, City: " Москва ", From: "2026-10-01", To: "2026-12-31"}
	if err := criteria.normalize(); err != nil {
		t.Fatal(err)
	}
	conference := &client.Conference{Topics: []string{"Физика"}, Format: "online", City: "МОСКВА", StartDate: "2026-10-05"}
	if len(criteria.Topics) != 1 || !criteria.Matches(conference) || criteria.Matches(nil) {
		t.Fatal("normalization or positive match failed")
	}
	for _, mutate := range []func(*client.Conference){func(c *client.Conference) { c.Topics = []string{"Иное"} }, func(c *client.Conference) { c.Format = "offline" }, func(c *client.Conference) { c.City = "Казань" }, func(c *client.Conference) { c.StartDate = "2027-01-01" }} {
		copy := *conference
		mutate(&copy)
		if criteria.Matches(&copy) {
			t.Fatalf("incorrect match: %+v", copy)
		}
	}
}
