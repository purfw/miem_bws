package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSensitivePayloadRedaction(t *testing.T) {
	raw, err := sanitizedPayload(map[string]any{"user_id": "id", "reset_token": "reset-secret", "nested": []any{map[string]any{"VerificationToken": "verify-secret", "password_hash": "hash-secret", "status": "approved"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("secrets retained: %s", raw)
	}
	if !strings.Contains(string(raw), "approved") || !strings.Contains(string(raw), "user_id") {
		t.Fatalf("audit fields lost: %s", raw)
	}
	if !json.Valid(raw) {
		t.Fatal("invalid JSON")
	}
}
