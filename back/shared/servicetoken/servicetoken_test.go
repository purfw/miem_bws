package servicetoken

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignatureBindsRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "http://identity/internal/users/id/role?mode=one", nil)
	token := SignRequest("secret", req, []byte(`{"role":"user"}`))
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v2" {
		t.Fatal("invalid protocol")
	}
	base := signature("secret", req.Method, req.URL.RequestURI(), parts[1], parts[2], []byte(`{"role":"user"}`))
	if parts[3] != base {
		t.Fatal("signature mismatch")
	}
	for _, test := range []struct{ method, path, body string }{{"GET", req.URL.RequestURI(), `{"role":"user"}`}, {req.Method, "/internal/users/id/active?mode=one", `{"role":"user"}`}, {req.Method, "/internal/users/id/role?mode=two", `{"role":"user"}`}, {req.Method, req.URL.RequestURI(), `{"role":"admin"}`}} {
		if base == signature("secret", test.method, test.path, parts[1], parts[2], []byte(test.body)) {
			t.Fatal("request mutation did not invalidate signature")
		}
	}
}
