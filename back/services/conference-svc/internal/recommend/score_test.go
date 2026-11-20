package recommend

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func id(n byte) uuid.UUID { return uuid.UUID{15: n} }

func TestRankPrefersTopicAndCoAttendance(t *testing.T) {
	day := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	got := Rank([]Candidate{
		{ID: id(1), Participants: 500, Open: true, StartDate: day},
		{ID: id(2), MatchedTopics: []string{"Лингвистика"}, TopicWeight: 3, TextRank: 0.1, StartDate: day},
		{ID: id(3), CoScore: 4, Participants: 10, StartDate: day},
		{ID: id(4), MatchedTopics: []string{"Лингвистика", "Социология", "NLP"}, TopicWeight: 5, CoScore: 2, StartDate: day},
	}, 3)
	if len(got) != 3 {
		t.Fatalf("limit not applied: %d", len(got))
	}
	if got[0].ID != id(4) {
		t.Fatalf("best match should rank first, got %v", got[0].ID)
	}
	if n := len(got[0].Reasons); n != 3 || got[0].Reasons[0].Code != ReasonTopic || got[0].Reasons[2].Code != ReasonCoAttendance {
		t.Fatalf("reasons: %+v", got[0].Reasons)
	}
	if !Personalised(got) {
		t.Fatal("ranking with topic matches must be personalised")
	}
}

func TestRankColdStartUsesPopularity(t *testing.T) {
	early := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.AddDate(0, 1, 0)
	got := Rank([]Candidate{
		{ID: id(1), Participants: 2, StartDate: early},
		{ID: id(2), Participants: 120, StartDate: late},
		{ID: id(3), Open: true, StartDate: late},
		{ID: id(4), Participants: 2, StartDate: late},
	}, 0)
	if got[0].ID != id(2) || got[0].Reasons[0].Code != ReasonPopular {
		t.Fatalf("most popular first: %+v", got[0])
	}
	if got[1].ID != id(3) {
		t.Fatalf("open registration outweighs a couple of participants: %v", got[1].ID)
	}
	if got[2].ID != id(1) || got[3].ID != id(4) {
		t.Fatalf("equal scores must be ordered by start date: %v %v", got[2].ID, got[3].ID)
	}
	if Personalised(got) {
		t.Fatal("cold start must not be reported as personalised")
	}
	for _, item := range got {
		if item.ID == id(3) && (len(item.Reasons) != 1 || item.Reasons[0].Code != ReasonOpen) {
			t.Fatalf("open registration reason: %+v", item.Reasons)
		}
	}
}

func TestRankEmpty(t *testing.T) {
	if got := Rank(nil, 5); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
