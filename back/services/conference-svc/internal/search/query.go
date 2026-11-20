package search

import (
	"encoding/json"
	"strings"
)

// Field boosts shared by the whole-word and prefix clauses.
var searchFields = []struct {
	name  string
	boost string
}{
	{"title", "6"}, {"title_en", "6"}, {"topics", "4"}, {"speakers", "3"},
	{"organizer_names", "2"}, {"program", "2"}, {"description", "1.5"},
	{"description_en", "1.5"}, {"city", "1"},
}

func fieldList(suffix string) []string {
	out := make([]string, 0, len(searchFields))
	for _, f := range searchFields {
		out = append(out, f.name+suffix+"^"+f.boost)
	}
	return out
}

// textQuery matches every query word somewhere in the document. A word
// matches as a whole word (stemmed, with typo tolerance) or as the start of
// a word, so "нейро" finds "нейронауке" and "геномика" finds "геномике".
func textQuery(query string) map[string]any {
	return map[string]any{"bool": map[string]any{
		"should": []any{
			map[string]any{"multi_match": map[string]any{
				"query": query, "type": "phrase", "fields": []string{"title^3", "title_en^3"}, "boost": 4,
			}},
			map[string]any{"multi_match": map[string]any{
				"query": query, "type": "best_fields", "operator": "and", "fields": fieldList(""),
				"fuzziness": "AUTO", "prefix_length": 1, "boost": 2,
			}},
			map[string]any{"multi_match": map[string]any{
				"query": query, "type": "cross_fields", "operator": "and", "fields": fieldList(".prefix"),
			}},
		},
		"minimum_should_match": 1,
	}}
}

func buildSearchPayload(f Filter) []byte {
	must := []any{textQuery(f.Query)}
	filters := []any{map[string]any{"term": map[string]any{"publication_allowed": true}}}
	if len(f.Statuses) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"status": f.Statuses}})
	} else {
		filters = append(filters, map[string]any{"bool": map[string]any{"must_not": []any{
			map[string]any{"terms": map[string]any{"status": []string{"draft", "cancelled"}}},
		}}})
	}
	if len(f.Topics) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"topics.keyword": f.Topics}})
	}
	if len(f.Formats) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"format": f.Formats}})
	}
	if len(f.Cities) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"city.lower": lowerAll(f.Cities)}})
	}
	dateRange := map[string]any{}
	if f.FromDate != nil {
		dateRange["gte"] = f.FromDate.Format("2006-01-02")
	}
	if f.ToDate != nil {
		dateRange["lte"] = f.ToDate.Format("2006-01-02")
	}
	if len(dateRange) > 0 {
		filters = append(filters, map[string]any{"range": map[string]any{"start_date": dateRange}})
	}
	sort := []any{map[string]any{"_score": "desc"}, map[string]any{"start_date": "asc"}, map[string]any{"slug": "asc"}}
	switch f.Sort { // "" and "relevance" keep the score order
	case "start_date":
		sort = []any{map[string]any{"start_date": "asc"}, map[string]any{"slug": "asc"}}
	case "-start_date":
		sort = []any{map[string]any{"start_date": "desc"}, map[string]any{"slug": "desc"}}
	case "created_at":
		sort = []any{map[string]any{"created_at": "asc"}, map[string]any{"slug": "asc"}}
	case "-created_at":
		sort = []any{map[string]any{"created_at": "desc"}, map[string]any{"slug": "desc"}}
	case "title":
		sort = []any{map[string]any{"title.raw": "asc"}, map[string]any{"slug": "asc"}}
	}
	payload, _ := json.Marshal(map[string]any{
		"from": (f.Page - 1) * f.PerPage, "size": f.PerPage, "track_total_hits": true, "_source": false,
		"query": map[string]any{"bool": map[string]any{"must": must, "filter": filters}},
		"sort":  sort,
		"aggs": map[string]any{
			"topics":  map[string]any{"terms": map[string]any{"field": "topics.keyword", "size": 50}},
			"formats": map[string]any{"terms": map[string]any{"field": "format", "size": 10}},
			"cities":  map[string]any{"terms": map[string]any{"field": "city.raw", "size": 50}},
		},
	})
	return payload
}

func lowerAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strings.ToLower(strings.ReplaceAll(v, "ё", "е")))
	}
	return out
}
