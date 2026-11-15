package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMetricsUseRoutesNotUserIdentifiers(t *testing.T) {
	metrics := New("test-svc", nil)
	router := chi.NewRouter()
	router.Use(metrics.Middleware)
	router.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	router.Handle("/metrics", metrics.Handler())
	for _, path := range []string{"/users/secret-user-one", "/users/secret-user-two"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	text := response.Body.String()
	if response.Code != 200 || strings.Contains(text, "secret-user") || !strings.Contains(text, `route="/users/{id}"`) || !strings.Contains(text, `status="403"} 2`) {
		t.Fatalf("unexpected metrics: %s", text)
	}
}
