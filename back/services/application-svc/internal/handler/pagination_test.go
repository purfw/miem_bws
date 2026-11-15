package handler

import (
	"net/http/httptest"
	"testing"
)

func TestQueuePagination(t *testing.T) {
	for _, tc := range []struct {
		query       string
		page, limit int
		valid       bool
	}{
		{"", 1, 100, true}, {"?page=2&per_page=50", 2, 50, true},
		{"?page=0", 0, 0, false}, {"?per_page=201", 0, 0, false}, {"?page=abc", 0, 0, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			page, limit, err := queuePage(httptest.NewRequest("GET", "/internal/applications"+tc.query, nil))
			if (err == nil) != tc.valid || page != tc.page || limit != tc.limit {
				t.Fatalf("page=%d limit=%d err=%v", page, limit, err)
			}
		})
	}
}
