package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type Problem struct {
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Status   int            `json:"status"`
	Code     string         `json:"code,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Instance string         `json:"instance,omitempty"`
	Errors   []FieldError   `json:"errors,omitempty"`
	Extra    map[string]any `json:"-"`
}

type codedError interface {
	ProblemCode() string
}

func withCode(p Problem, err error) Problem {
	var coded codedError
	if errors.As(err, &coded) {
		p.Code = coded.ProblemCode()
	}
	return p
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

var (
	ErrNotFound     = errors.New("not_found")
	ErrForbidden    = errors.New("forbidden")
	ErrUnauthorized = errors.New("unauthorized")
	ErrConflict     = errors.New("conflict")
	ErrValidation   = errors.New("validation")
	ErrTooMany      = errors.New("too_many")
	ErrUnavailable  = errors.New("unavailable")
	ErrInternal     = errors.New("internal")
)

type ValidationError struct {
	Fields []FieldError
}

func (v *ValidationError) Error() string { return "validation failed" }
func (v *ValidationError) Unwrap() error { return ErrValidation }

func problemFor(err error) Problem {
	switch {
	case errors.Is(err, ErrNotFound):
		return Problem{Type: "https://confhub.example.com/errors/not_found", Title: "Not Found", Status: http.StatusNotFound, Detail: err.Error()}
	case errors.Is(err, ErrForbidden):
		return Problem{Type: "https://confhub.example.com/errors/forbidden", Title: "Forbidden", Status: http.StatusForbidden, Detail: err.Error()}
	case errors.Is(err, ErrUnauthorized):
		return Problem{Type: "https://confhub.example.com/errors/unauthorized", Title: "Unauthorized", Status: http.StatusUnauthorized, Detail: err.Error()}
	case errors.Is(err, ErrConflict):
		return Problem{Type: "https://confhub.example.com/errors/conflict", Title: "Conflict", Status: http.StatusConflict, Detail: err.Error()}
	case errors.Is(err, ErrTooMany):
		return Problem{Type: "https://confhub.example.com/errors/too_many", Title: "Too Many Requests", Status: http.StatusTooManyRequests, Detail: err.Error()}
	case errors.Is(err, ErrUnavailable):
		return Problem{Type: "https://confhub.example.com/errors/unavailable", Title: "Service Unavailable", Status: http.StatusServiceUnavailable, Detail: "service temporarily unavailable"}
	case errors.Is(err, ErrValidation):
		var v *ValidationError
		if errors.As(err, &v) {
			return withCode(Problem{Type: "https://confhub.example.com/errors/validation", Title: "Validation Failed", Status: http.StatusUnprocessableEntity, Detail: err.Error(), Errors: v.Fields}, err)
		}
		return withCode(Problem{Type: "https://confhub.example.com/errors/validation", Title: "Validation Failed", Status: http.StatusUnprocessableEntity, Detail: err.Error()}, err)
	default:
		return Problem{Type: "https://confhub.example.com/errors/internal", Title: "Internal Server Error", Status: http.StatusInternalServerError, Detail: "internal error"}
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	p := withCode(problemFor(err), err)
	if rid := r.Header.Get("X-Request-ID"); rid != "" {
		p.Instance = rid
	}
	if p.Status >= 500 {
		slog.ErrorContext(r.Context(), "request failed",
			"err", err.Error(),
			"path", r.URL.Path,
			"method", r.Method,
			"request_id", r.Header.Get("X-Request-ID"),
		)
	} else if p.Status >= 400 {
		slog.WarnContext(r.Context(), "client error",
			"err", err.Error(),
			"path", r.URL.Path,
			"method", r.Method,
			"status", p.Status,
		)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &ValidationError{Fields: []FieldError{{Field: "body", Message: err.Error()}}}
	}
	return nil
}
