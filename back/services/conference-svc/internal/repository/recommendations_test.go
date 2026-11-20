package repository

import (
	"context"
	"testing"
	"time"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/recommend"
)

func TestRecommendationCandidates(t *testing.T) {
	pool := testutil.Database(t)
	repo := NewConferences(pool)
	ctx := context.Background()
	user, neighbour := uuid.New(), uuid.New()
	topic := "Тема-" + uuid.NewString()[:8]
	future := time.Now().AddDate(0, 1, 0)
	past := time.Now().AddDate(0, -1, 0)

	insert := func(title string, topics []string, createdBy uuid.UUID, day time.Time) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences
			(id,title,slug,start_date,end_date,created_by,topics,status,publication_allowed,format,city)
			VALUES($1,$2,$3,$4,$4,$5,$6,'registration_open',true,'offline','Казань')`,
			id, title, uuid.NewString(), day, createdBy, topics); err != nil {
			t.Fatal(err)
		}
		return id
	}
	register := func(userID, conferenceID uuid.UUID) {
		if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.registrations
			(conference_id,user_id,user_email,user_name,status) VALUES($1,$2,'u@example.test','U','confirmed')`,
			conferenceID, userID); err != nil {
			t.Fatal(err)
		}
	}
	attended := insert("Палеогенетика древних популяций", []string{topic}, uuid.New(), future)
	sameTopic := insert("Другая конференция", []string{topic, "Прочее"}, uuid.New(), future)
	coAttended := insert("Без общих тем", []string{"Астрономия"}, uuid.New(), future)
	ownConference := insert("Своя конференция", []string{topic}, user, future)
	finished := insert("Прошедшая", []string{topic}, uuid.New(), past)
	register(user, attended)
	register(neighbour, attended)
	register(neighbour, coAttended)

	seeds, err := repo.UserSeeds(ctx, user)
	if err != nil || len(seeds) != 1 || seeds[0].ConferenceID != attended || seeds[0].Weight != seedAttendee {
		t.Fatalf("seeds=%+v err=%v", seeds, err)
	}
	candidates, err := repo.RecommendationCandidates(ctx, seeds, &user)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]recommend.Candidate{}
	for _, c := range candidates {
		byID[c.ID] = c
	}
	for name, id := range map[string]uuid.UUID{"seed": attended, "own": ownConference, "finished": finished} {
		if _, ok := byID[id]; ok {
			t.Errorf("%s conference must not be recommended", name)
		}
	}
	if c := byID[sameTopic]; c.TopicWeight != seedAttendee || len(c.MatchedTopics) != 1 || c.MatchedTopics[0] != topic || !c.SameCity {
		t.Errorf("same topic candidate: %+v", c)
	}
	if c := byID[coAttended]; c.CoScore <= 0 || c.TopicWeight != 0 {
		t.Errorf("co-attended candidate: %+v", c)
	}

	ranked := recommend.Rank(candidates, 0)
	position := map[uuid.UUID]int{}
	for i, item := range ranked {
		position[item.ID] = i
	}
	if position[sameTopic] > position[coAttended] {
		t.Errorf("topic match should outrank co-attendance alone")
	}

	anonymous, err := repo.RecommendationCandidates(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range anonymous {
		if c.TopicWeight != 0 || c.CoScore != 0 || c.TextRank != 0 {
			t.Fatalf("cold start must have no personal signals: %+v", c)
		}
	}
}

func TestProgramChangesTouchConference(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	id := uuid.New()
	day := time.Now().AddDate(0, 1, 0)
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,created_by)
		VALUES($1,'Touch',$2,$3,$3,$4)`, id, uuid.NewString(), day, uuid.New()); err != nil {
		t.Fatal(err)
	}
	var before, after time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM conference_svc.conferences WHERE id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.sections(conference_id,title) VALUES($1,'Секция')`, id); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM conference_svc.conferences WHERE id=$1`, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Fatalf("section insert must move updated_at forward: %v -> %v", before, after)
	}
}
