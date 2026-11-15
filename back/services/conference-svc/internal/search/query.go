package search

import "encoding/json"

func buildSearchPayload(f Filter) []byte {
	must := []any{map[string]any{"multi_match": map[string]any{
		"query": f.Query, "fields": []string{
			"title^6", "title_en^6", "description^2", "description_en^2", "topics^3", "organizer_names^2",
		}, "fuzziness": "AUTO", "prefix_length": 1,
	}}}
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
		filters = append(filters, map[string]any{"terms": map[string]any{"city.raw": f.Cities}})
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
	switch f.Sort {
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
