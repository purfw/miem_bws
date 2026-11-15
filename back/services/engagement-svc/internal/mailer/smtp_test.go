package mailer

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"testing"
	"time"
)

func TestCalendarMessage(t *testing.T) {
	calendar := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")
	wire, err := buildMessage("from@example.test", "to@example.test", "Регистрация подтверждена", "<p>test</p>", calendar, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(wire))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(part)
	if string(body) != "<p>test</p>" {
		t.Fatalf("body %q", body)
	}
	part, err = reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(part)
	if !bytes.Equal(body, calendar) {
		t.Fatalf("calendar %q", body)
	}
	if part.Header.Get("Content-Disposition") != `attachment; filename="conference.ics"` {
		t.Fatal("missing attachment filename")
	}
}

func TestRejectHeaderInjection(t *testing.T) {
	for _, field := range []int{0, 1, 2} {
		values := []string{"from@example.test", "to@example.test", "subject"}
		values[field] += "\r\nBcc: leak@example.test"
		if _, err := buildMessage(values[0], values[1], values[2], "body", nil, uuid.New()); err == nil {
			t.Fatalf("field %d accepted", field)
		}
	}
}

func TestSMTPHonorsCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	address := listener.Addr().(*net.TCPAddr)
	m := &Mailer{cfg: Config{Host: "127.0.0.1", Port: address.Port, From: "from@example.test"}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := m.dispatch(ctx, "to@example.test", []byte("test")); err == nil {
		t.Fatal("expected cancellation error")
	}
	if time.Since(started) > time.Second {
		t.Fatal("SMTP ignored cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection not closed")
	}
}

func TestRetryDelayBounded(t *testing.T) {
	if retryDelay(1) != 15*time.Second || retryDelay(2) != 30*time.Second || retryDelay(100) != 32*time.Minute {
		t.Fatal("incorrect backoff")
	}
}
