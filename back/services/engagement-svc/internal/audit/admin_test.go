package audit

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/confhub/shared/testutil"
	"github.com/google/uuid"
)

func TestAdministratorManagementRequestsAreAudited(t *testing.T) {
	pool := testutil.Database(t)
	handler := LoginHandler(pool)
	for _, kind := range []string{"admin.invitation_created", "admin.access_change_requested", "admin.activation_requested"} {
		actor := uuid.New()
		payload, _ := json.Marshal(LoginRequest{EventID: uuid.New(), EventType: kind, ActorID: &actor, UsernameHash: strings.Repeat("a", 64)})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", "/internal/admin/auth-audit", bytes.NewReader(payload)))
		if response.Code != 204 {
			t.Fatal(kind, response.Code, response.Body.String())
		}
	}
}
