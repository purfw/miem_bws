package search

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/confhub/conference-svc/internal/domain"
)

func ptr(s string) *string { return &s }

var fixtures = []domain.SearchDocument{
	{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Slug: "neuro",
		Title:       "VI Всероссийская конференция по когнитивной нейронауке",
		Description: ptr("Эмпирические исследования мозга и поведения, нейровизуализация и вычислительные модели."),
		Topics:      []string{"Нейронауки", "Когнитивные науки"}, Speakers: []string{"Анна Петрова"},
		Program: []string{"Нейронные корреляты внимания"},
	},
	{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Slug: "bio",
		Title:       "Биоинформатика: вычислительные подходы к геномике",
		Description: ptr("Анализ геномных данных, машинное обучение для биологии и открытые наборы данных."),
		Topics:      []string{"Биоинформатика", "Геномика"}, Speakers: []string{"Иван Сидоров"},
	},
	{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), Slug: "physics",
		Title:       "Физика конденсированных сред: новые материалы",
		Description: ptr("Сверхпроводники, наноматериалы и квантовые эффекты в твёрдых телах."),
		Topics:      []string{"Физика", "Новые материалы"}, City: ptr("Новосибирск"),
	},
	{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000004"), Slug: "archaeology",
		Title:       "Цифровая археология и культурное наследие",
		Description: ptr("Методы 3D-реконструкции, ГИС в археологии и цифровые архивы артефактов."),
		Topics:      []string{"Археология", "Цифровое наследие"},
	},
	{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000005"), Slug: "nlp",
		Title: "Машинное обучение и обработка естественного языка", TitleEn: ptr("Machine Learning and NLP"),
		Description: ptr("Прикладные методы NLP, мультимодальные модели и retrieval-augmented generation."),
		Topics:      []string{"Машинное обучение", "Лингвистика"},
	},
	{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000006"), Slug: "socio",
		Title:       "Социолингвистика и языковая политика регионов",
		Description: ptr("Языковые контакты, многоязычие и корпусные исследования живой речи."),
		Topics:      []string{"Лингвистика", "Социология"},
	},
}

func TestSearchMatchesWordsAndPrefixes(t *testing.T) {
	endpoint := os.Getenv("TEST_ELASTICSEARCH_URL")
	if endpoint == "" {
		t.Skip("TEST_ELASTICSEARCH_URL is not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.HasPrefix(parsed.Hostname(), "confhub-test-elasticsearch") {
		t.Fatal("refusing a non-test Elasticsearch")
	}
	ctx := context.Background()
	client := NewClient(endpoint, "confhub_test_"+strings.ReplaceAll(uuid.NewString(), "-", ""), nil, time.Minute)
	t.Cleanup(func() {
		if resp, err := client.do(context.Background(), http.MethodDelete, "/"+client.index, nil); err == nil {
			resp.Body.Close()
		}
	})
	if created, err := client.ensureIndex(ctx); err != nil || !created {
		t.Fatalf("create index: created=%v err=%v", created, err)
	}
	docs := slices.Clone(fixtures)
	for i := range docs {
		docs[i].Format, docs[i].Status, docs[i].PublicationAllowed = "offline", "registration_open", true
		docs[i].StartDate = time.Date(2027, 1, 10+i, 0, 0, 0, 0, time.UTC)
		docs[i].EndDate = docs[i].StartDate
		if docs[i].OrganizerNames == nil {
			docs[i].OrganizerNames = []string{}
		}
	}
	if err := client.bulk(ctx, docs); err != nil {
		t.Fatal(err)
	}
	if resp, err := client.do(ctx, http.MethodPost, "/"+client.index+"/_refresh", nil); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	client.setReady(true)

	slugByID := map[uuid.UUID]string{}
	for _, d := range docs {
		slugByID[d.ID] = d.Slug
	}
	cases := []struct {
		query string
		want  []string // expected slugs; the first one must rank first
	}{
		{"нейро", []string{"neuro"}},
		{"нейронауке", []string{"neuro"}},
		{"когнитив", []string{"neuro"}},
		{"геном", []string{"bio"}},
		{"геномика", []string{"bio"}},
		{"био", []string{"bio"}},
		{"физ", []string{"physics"}},
		{"твердых", []string{"physics"}},
		{"Физика конденсированных сред: новые материалы", []string{"physics"}},
		{"цифров", []string{"archaeology"}},
		{"линг", []string{"socio", "nlp"}},
		{"машин", []string{"nlp", "bio"}},
		{"machine learn", []string{"nlp"}},
		{"Петров", []string{"neuro"}},
		{"внимания", []string{"neuro"}},
		{"новосиб", []string{"physics"}},
		{"археолоия", []string{"archaeology"}}, // typo
		{"геном машин", []string{"bio"}},
		{"квантовая хромодинамика", nil},
	}
	for _, tc := range cases {
		result, err := client.Search(ctx, Filter{Query: tc.query, Statuses: []string{"registration_open"}})
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		got := make([]string, 0, len(result.IDs))
		for _, id := range result.IDs {
			got = append(got, slugByID[id])
		}
		if len(tc.want) == 0 {
			if len(got) != 0 {
				t.Errorf("%q: want no results, got %v", tc.query, got)
			}
			continue
		}
		if len(got) == 0 || got[0] != tc.want[0] {
			t.Errorf("%q: want %v first, got %v", tc.query, tc.want[0], got)
			continue
		}
		for _, slug := range tc.want {
			if !slices.Contains(got, slug) {
				t.Errorf("%q: want %s in %v", tc.query, slug, got)
			}
		}
	}

	byCity, err := client.Search(ctx, Filter{Query: "физ", Cities: []string{"новосибирск"}, Statuses: []string{"registration_open"}})
	if err != nil || byCity.Total != 1 {
		t.Errorf("city filter must ignore case: total=%d err=%v", byCity.Total, err)
	}

	suggestions, err := client.Autocomplete(ctx, "нейр", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) == 0 || suggestions[0].Slug != "neuro" {
		t.Errorf("autocomplete нейр: got %+v", suggestions)
	}
}
