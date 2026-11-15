package consumer

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/confhub/engagement-svc/internal/client"
)

type localeStub struct {
	locale string
	err    error
}

func (s localeStub) Locale(context.Context, uuid.UUID) (string, error) {
	return s.locale, s.err
}

func TestMessageLocale(t *testing.T) {
	tests := []struct {
		name       string
		resolver   localeResolver
		wantLocale string
		wantSend   bool
		wantErr    bool
	}{
		{name: "english", resolver: localeStub{locale: "en"}, wantLocale: "en", wantSend: true},
		{name: "unknown falls back to russian", resolver: localeStub{locale: "de"}, wantLocale: "ru", wantSend: true},
		{name: "deleted user is skipped", resolver: localeStub{err: client.ErrUserNotFound}, wantSend: false},
		{name: "temporary failure is retried", resolver: localeStub{err: errors.New("down")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer := &Consumer{identity: tt.resolver}
			locale, send, err := consumer.messageLocale(context.Background(), uuid.New())
			if locale != tt.wantLocale || send != tt.wantSend || (err != nil) != tt.wantErr {
				t.Fatalf("got locale=%q send=%v err=%v", locale, send, err)
			}
		})
	}
}

func TestLocalizedContent(t *testing.T) {
	if got := localized("en", "RU", "EN"); got != "EN" {
		t.Fatalf("localized = %q", got)
	}
	if got := localizedTemplate("en", "welcome.html"); got != "welcome.en.html" {
		t.Fatalf("template = %q", got)
	}
	if got := applicationLabel("ru", "accepted"); got != "принята" {
		t.Fatalf("label = %q", got)
	}
}
