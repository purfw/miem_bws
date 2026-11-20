package domain

import (
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/confhub/shared/httputil"
)

const MaxCoAuthors = 20

// NormalizeCoAuthors trims the fields and validates the list: at most
// MaxCoAuthors entries, a name for each, a valid e-mail when one is given.
func NormalizeCoAuthors(items []CoAuthor) ([]CoAuthor, error) {
	if len(items) > MaxCoAuthors {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "co_authors", Message: "at most 20 co-authors"}}}
	}
	var errs []httputil.FieldError
	out := make([]CoAuthor, 0, len(items))
	for i, item := range items {
		field := "co_authors[" + strconv.Itoa(i) + "]."
		item.Name = strings.TrimSpace(item.Name)
		item.Email = strings.TrimSpace(item.Email)
		item.Affiliation = strings.TrimSpace(item.Affiliation)
		if item.Name == "" || utf8.RuneCountInString(item.Name) > 255 {
			errs = append(errs, httputil.FieldError{Field: field + "name", Message: "required, up to 255 characters"})
		}
		if item.Email != "" {
			if addr, err := mail.ParseAddress(item.Email); err != nil || addr.Address != item.Email || len(item.Email) > 255 {
				errs = append(errs, httputil.FieldError{Field: field + "email", Message: "invalid email"})
			}
		}
		if utf8.RuneCountInString(item.Affiliation) > 255 {
			errs = append(errs, httputil.FieldError{Field: field + "affiliation", Message: "up to 255 characters"})
		}
		out = append(out, item)
	}
	if len(errs) > 0 {
		return nil, &httputil.ValidationError{Fields: errs}
	}
	return out, nil
}
