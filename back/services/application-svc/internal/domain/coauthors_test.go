package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/confhub/shared/httputil"
)

func TestNormalizeCoAuthors(t *testing.T) {
	got, err := NormalizeCoAuthors([]CoAuthor{{Name: "  Анна Петрова ", Email: " anna@example.org ", Affiliation: " НИУ ВШЭ "}})
	if err != nil || got[0].Name != "Анна Петрова" || got[0].Email != "anna@example.org" || got[0].Affiliation != "НИУ ВШЭ" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for name, items := range map[string][]CoAuthor{
		"missing name": {{Name: " "}},
		"bad email":    {{Name: "A", Email: "not-an-email"}},
		"named email":  {{Name: "A", Email: "A <a@example.org>"}},
		"long name":    {{Name: strings.Repeat("я", 256)}},
		"too many":     make([]CoAuthor, MaxCoAuthors+1),
	} {
		if _, err := NormalizeCoAuthors(items); !errors.Is(err, httputil.ErrValidation) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}
