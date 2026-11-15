package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/confhub/shared/httputil"
)

func TestMinimumStartDate(t *testing.T) {
	for _, tc := range []struct {
		date     string
		accepted bool
	}{
		{"2026-12-31", false}, {"2027-01-01", true}, {"2027-01-02", true}, {"2026-01-01", false},
	} {
		t.Run(tc.date, func(t *testing.T) {
			value, err := time.Parse("2006-01-02", tc.date)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateStartDate(value)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
			if !tc.accepted {
				var validation *httputil.ValidationError
				if !errors.As(err, &validation) || len(validation.Fields) != 1 || validation.Fields[0].Field != "start_date" {
					t.Fatal("date field error missing")
				}
				if err.(interface{ ProblemCode() string }).ProblemCode() != "START_DATE_TOO_EARLY" {
					t.Fatal("date error code missing")
				}
			}
		})
	}
	if err := ValidateStartDate(time.Time{}); err == nil {
		t.Fatal("zero date accepted")
	}
	moscow := time.FixedZone("MSK", 3*60*60)
	if err := ValidateStartDate(time.Date(2027, 1, 1, 0, 0, 0, 0, moscow)); err != nil {
		t.Fatal("valid local calendar date rejected")
	}
}
