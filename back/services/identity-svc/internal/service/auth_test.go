package service

import "testing"

func TestRegisterInputLocale(t *testing.T) {
	valid := RegisterInput{Email: "user@example.com", Password: "long-password-123", FirstName: "Ada", LastName: "Lovelace", Locale: "en"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid locale rejected: %v", err)
	}

	defaultLocale := RegisterInput{Email: "user@example.com", Password: "long-password-123", FirstName: "Ada", LastName: "Lovelace"}
	if err := defaultLocale.Validate(); err != nil {
		t.Fatalf("default locale rejected: %v", err)
	}
	if defaultLocale.Locale != "ru" {
		t.Fatalf("default locale = %q, want ru", defaultLocale.Locale)
	}

	invalid := RegisterInput{Email: "user@example.com", Password: "long-password-123", FirstName: "Ada", LastName: "Lovelace", Locale: "de"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid locale accepted")
	}
}
