// Package recommend ranks conferences for a user or for a conference page.
//
// The ranking is a hybrid of content-based and collaborative signals computed
// from a set of seed conferences (the ones a user registered for, spoke at or
// added to favourites, or the conference being viewed):
//
//   - topics: weighted overlap between the candidate's topics and the seeds';
//   - text: full-text similarity between the candidate and the seed
//     descriptions (PostgreSQL ts_rank over the seeds' lexemes);
//   - co-attendance: other users who interacted with the seeds also
//     registered for or favourited the candidate;
//   - city: an in-person seed took place in the same city;
//   - popularity: confirmed participants, log-scaled.
//
// Without seeds only popularity and open registration remain, which gives a
// reasonable cold-start list for anonymous visitors and new users.
package recommend

import (
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Candidate holds the raw signals for one conference.
type Candidate struct {
	ID            uuid.UUID
	MatchedTopics []string
	TopicWeight   float64
	TextRank      float64
	CoScore       float64
	SameCity      bool
	City          string
	Participants  int
	Open          bool
	StartDate     time.Time
}

// Reason codes returned to clients; labels are localised on the frontend.
const (
	ReasonTopic        = "topic"
	ReasonSimilarText  = "similar_text"
	ReasonCoAttendance = "co_attendance"
	ReasonCity         = "city"
	ReasonPopular      = "popular"
	ReasonOpen         = "registration_open"
)

type Reason struct {
	Code  string `json:"code"`
	Value string `json:"value,omitempty"`
}

type Scored struct {
	ID      uuid.UUID
	Score   float64
	Reasons []Reason
}

// Weights of the normalised signals; they sum to 1.
const (
	weightTopic      = 0.40
	weightText       = 0.20
	weightCo         = 0.20
	weightCity       = 0.10
	weightPopularity = 0.10
	// bonusOpen lifts conferences that still accept registrations.
	bonusOpen = 0.05
)

// Rank scores candidates and returns at most limit of them, best first.
func Rank(candidates []Candidate, limit int) []Scored {
	var maxTopic, maxText, maxCo float64
	maxParticipants := 0
	for _, c := range candidates {
		maxTopic = math.Max(maxTopic, c.TopicWeight)
		maxText = math.Max(maxText, c.TextRank)
		maxCo = math.Max(maxCo, c.CoScore)
		maxParticipants = max(maxParticipants, c.Participants)
	}
	out := make([]Scored, 0, len(candidates))
	starts := make(map[uuid.UUID]time.Time, len(candidates))
	for _, c := range candidates {
		topic, text, co := norm(c.TopicWeight, maxTopic), norm(c.TextRank, maxText), norm(c.CoScore, maxCo)
		popularity := 0.0
		if maxParticipants > 0 {
			popularity = math.Log1p(float64(c.Participants)) / math.Log1p(float64(maxParticipants))
		}
		score := weightTopic*topic + weightText*text + weightCo*co + weightPopularity*popularity
		if c.SameCity {
			score += weightCity
		}
		if c.Open {
			score += bonusOpen
		}

		var reasons []Reason
		for _, t := range c.MatchedTopics {
			reasons = append(reasons, Reason{Code: ReasonTopic, Value: t})
			if len(reasons) == 2 {
				break
			}
		}
		if co >= 0.2 {
			reasons = append(reasons, Reason{Code: ReasonCoAttendance})
		}
		if text >= 0.3 && len(c.MatchedTopics) == 0 {
			reasons = append(reasons, Reason{Code: ReasonSimilarText})
		}
		if c.SameCity && c.City != "" {
			reasons = append(reasons, Reason{Code: ReasonCity, Value: c.City})
		}
		if len(reasons) == 0 {
			if popularity >= 0.5 {
				reasons = append(reasons, Reason{Code: ReasonPopular})
			} else if c.Open {
				reasons = append(reasons, Reason{Code: ReasonOpen})
			}
		}
		out = append(out, Scored{ID: c.ID, Score: score, Reasons: reasons})
		starts[c.ID] = c.StartDate
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if !starts[out[i].ID].Equal(starts[out[j].ID]) {
			return starts[out[i].ID].Before(starts[out[j].ID])
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func norm(v, maxV float64) float64 {
	if maxV <= 0 || v <= 0 {
		return 0
	}
	return v / maxV
}

// Personalised reports whether a ranking used any of the user's seeds.
func Personalised(items []Scored) bool {
	for _, item := range items {
		for _, r := range item.Reasons {
			if r.Code != ReasonPopular && r.Code != ReasonOpen {
				return true
			}
		}
	}
	return false
}
