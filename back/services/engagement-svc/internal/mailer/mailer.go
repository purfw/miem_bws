package mailer

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	TLSMode  string
}

type Mailer struct {
	cfg  Config
	tpl  *template.Template
	pool *pgxpool.Pool
}

func New(cfg Config, pool *pgxpool.Pool, tplGlob string) (*Mailer, error) {
	if strings.ContainsAny(cfg.From, "\r\n") {
		return nil, fmt.Errorf("invalid sender")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("invalid sender: %w", err)
	}
	cfg.From = from.Address
	if cfg.TLSMode != "" && cfg.TLSMode != "starttls" && cfg.TLSMode != "implicit" && cfg.TLSMode != "none" {
		return nil, fmt.Errorf("invalid SMTP TLS mode")
	}
	if cfg.User != "" && cfg.TLSMode == "none" {
		return nil, fmt.Errorf("SMTP authentication requires TLS")
	}
	tpl, err := template.ParseGlob(tplGlob)
	if err != nil {
		return nil, fmt.Errorf("parse email templates: %w", err)
	}
	return &Mailer{cfg: cfg, tpl: tpl, pool: pool}, nil
}

func (m *Mailer) Send(ctx context.Context, recipient, subject, templateName string, data any, userID *uuid.UUID, notifType string) error {
	return m.SendWithCalendar(ctx, recipient, subject, templateName, data, nil, userID, notifType)
}

func (m *Mailer) SendWithCalendar(ctx context.Context, recipient, subject, templateName string, data any, calendar []byte, userID *uuid.UUID, notifType string) error {
	var body bytes.Buffer
	if err := m.tpl.ExecuteTemplate(&body, templateName, data); err != nil {
		return fmt.Errorf("render email: %w", err)
	}
	return m.Enqueue(ctx, recipient, subject, body.String(), calendar, userID, notifType, nil)
}
