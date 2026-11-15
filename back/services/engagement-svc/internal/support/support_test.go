package support

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/confhub/engagement-svc/internal/mailer"
	"github.com/confhub/shared/testutil"
)

func supportRequest(email, ip string) *http.Request {
	body, _ := json.Marshal(createReq{Name: "Test Sender", Email: email, Message: "An integration test support message", Topic: "bug"})
	r := httptest.NewRequest(http.MethodPost, "/support", bytes.NewReader(body))
	r.Header.Set("X-Real-IP", ip)
	return r
}

func TestSupportAtomicQueueAndConcurrentLimit(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	mail, err := mailer.New(mailer.Config{From: "from@example.test"}, pool, "../../templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	recipient, sender := uuid.NewString()+"@example.test", uuid.NewString()+"@example.test"
	ip := fmt.Sprintf("2001:db8:%s:%s::1", uuid.NewString()[:4], uuid.NewString()[:4])
	handler := New(pool, mail, recipient)
	statuses := make(chan int, 12)
	var workers sync.WaitGroup
	for i := 0; i < cap(statuses); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			response := httptest.NewRecorder()
			handler.Create(response, supportRequest(sender, ip))
			statuses <- response.Code
		}()
	}
	workers.Wait()
	close(statuses)
	accepted := 0
	for status := range statuses {
		if status == http.StatusAccepted {
			accepted++
		} else if status != http.StatusTooManyRequests {
			t.Fatalf("unexpected HTTP %d", status)
		}
	}
	if accepted != perIPHourly {
		t.Fatalf("accepted=%d, limit=%d", accepted, perIPHourly)
	}
	var messages, notifications int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.support_messages WHERE email=$1`, sender).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engagement_svc.notifications WHERE recipient=$1`, recipient).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if messages != perIPHourly || notifications != messages {
		t.Fatalf("messages=%d, queued=%d", messages, notifications)
	}
}

func TestSupportQueueFailureRollsBackMessage(t *testing.T) {
	pool := testutil.Database(t)
	mail, err := mailer.New(mailer.Config{From: "from@example.test"}, pool, "../../templates/welcome.html")
	if err != nil {
		t.Fatal(err)
	}
	sender := uuid.NewString() + "@example.test"
	response := httptest.NewRecorder()
	New(pool, mail, "support@example.test").Create(response, supportRequest(sender, "192.0.2.200"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("HTTP %d", response.Code)
	}
	var messages int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM engagement_svc.support_messages WHERE email=$1`, sender).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 0 {
		t.Fatalf("persisted %d messages despite queue failure", messages)
	}
}

func TestSupportRejectsDisplayNameEmail(t *testing.T) {
	input := createReq{Name: "Test Sender", Email: "Test <test@example.test>", Message: "This is a valid message", Topic: "other"}
	for _, field := range validate(&input) {
		if field.Field == "email" {
			return
		}
	}
	t.Fatal("display-name address accepted as email")
}
