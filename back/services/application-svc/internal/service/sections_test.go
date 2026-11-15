package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/confhub/application-svc/internal/client"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/servicetoken"
)

func TestApplicationSectionValidation(t *testing.T) {
	conferenceID, sectionID := uuid.New(), uuid.New()
	for _, test := range []struct {
		name, body string
		status     int
		expected   error
	}{
		{"own section", `{"belongs":true}`, 200, nil},
		{"foreign or missing", `{"belongs":false}`, 200, httputil.ErrValidation},
		{"malformed", `{}`, 200, httputil.ErrUnavailable},
		{"unavailable", `{}`, 503, httputil.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.Header.Get(servicetoken.HeaderName), "v2.") {
					t.Error("unsigned internal section check")
				}
				if r.URL.Path != "/internal/conferences/"+conferenceID.String()+"/sections/"+sectionID.String()+"/check" {
					t.Error("incorrect section check route")
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			application := &Applications{conf: client.NewConferenceCache(server.URL, "test-secret", 0)}
			if err := application.validateSection(context.Background(), conferenceID, &sectionID); !errors.Is(err, test.expected) {
				t.Fatalf("error=%v, expected=%v", err, test.expected)
			}
		})
	}
	if err := (&Applications{}).validateSection(context.Background(), conferenceID, nil); err != nil {
		t.Fatal(err)
	}
}
