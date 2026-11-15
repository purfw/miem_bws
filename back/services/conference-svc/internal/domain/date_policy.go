package domain

import (
	"time"

	"github.com/confhub/shared/httputil"
)

const MinimumStartDate = "2027-01-01"

type StartDateError struct{}

func (StartDateError) Error() string {
	return "start_date must not be earlier than " + MinimumStartDate
}
func (StartDateError) ProblemCode() string { return "START_DATE_TOO_EARLY" }
func (StartDateError) Unwrap() error {
	return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "start_date", Message: "must not be earlier than " + MinimumStartDate}}}
}

func ValidateStartDate(value time.Time) error {
	if value.IsZero() || value.Year() < 2027 {
		return StartDateError{}
	}
	return nil
}
