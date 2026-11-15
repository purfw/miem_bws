// Package pagination implements stable cursor pagination over (created_at, id).
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type Cursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func (c Cursor) Encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func Decode(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, errors.New("invalid cursor")
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cursor{}, errors.New("invalid cursor")
	}
	return c, nil
}

func ClampLimit(req, max int) int {
	if req <= 0 {
		return 20
	}
	if req > max {
		return max
	}
	return req
}
