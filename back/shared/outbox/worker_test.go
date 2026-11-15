package outbox

import (
	"encoding/json"
	"testing"
)

func TestEventRequestCorrelation(t *testing.T) {
	payload, requestID, err := eventPayload([]byte(`{"_request_id":"request-123","_traceparent":"00-0123456789abcdef0123456789abcdef-0123456789abcdef-01","_tracestate":"test=1","conference_id":"test"}`))
	if err != nil || requestID != "request-123" {
		t.Fatalf("request=%q err=%v", requestID, err)
	}
	var result map[string]string
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result["conference_id"] != "test" {
		t.Fatalf("business payload changed: %s", payload)
	}
}
