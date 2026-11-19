// Package consumer dispatches NATS events with database-backed deduplication.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/natsx"
	"github.com/confhub/shared/telemetry"

	"github.com/confhub/engagement-svc/internal/audit"
	"github.com/confhub/engagement-svc/internal/client"
	"github.com/confhub/engagement-svc/internal/mailer"
	"github.com/confhub/engagement-svc/internal/subscriptions"
)

type localeResolver interface {
	Locale(context.Context, uuid.UUID) (string, error)
}

type Consumer struct {
	pool     *pgxpool.Pool
	mailer   *mailer.Mailer
	identity localeResolver
	baseURL  string
	delivery *subscriptions.Delivery
}

func (c *Consumer) WithSubscriptions(delivery *subscriptions.Delivery) *Consumer {
	c.delivery = delivery
	return c
}

func New(pool *pgxpool.Pool, m *mailer.Mailer, identity localeResolver, baseURL string) *Consumer {
	return &Consumer{pool: pool, mailer: m, identity: identity, baseURL: baseURL}
}

// Subscribe registers durable consumers for domain events.
func (c *Consumer) Subscribe(ctx context.Context, nc *nats.Conn, failures ...*natsx.FailureStore) error {
	subs := []struct {
		subject string
		handler func(*nats.Msg) error
	}{
		{sharedevents.SubjectUserRegistered, c.handle(ctx, c.onUserRegistered)},
		{sharedevents.SubjectUserVerificationRequested, c.handle(ctx, c.onVerificationRequested)},
		{sharedevents.SubjectAppSubmitted, c.handle(ctx, c.onApplicationSubmitted)},
		{sharedevents.SubjectAppStatusChanged, c.handle(ctx, c.onApplicationStatusChanged)},
		{sharedevents.SubjectAppModerated, c.handle(ctx, c.onApplicationModerated)},
		{sharedevents.SubjectReviewCreated, c.handle(ctx, c.onReviewCreated)},
		{sharedevents.SubjectRegistrationConfirmed, c.handle(ctx, c.onRegistrationConfirmed)},
		{sharedevents.SubjectRegistrationWaitlisted, c.handle(ctx, c.onRegistrationWaitlisted)},
		{sharedevents.SubjectRegistrationPromoted, c.handle(ctx, c.onRegistrationPromoted)},
		{sharedevents.SubjectRegistrationCancelled, c.handle(ctx, c.onRegistrationCancelled)},
		{sharedevents.SubjectConferenceCancelled, c.handle(ctx, c.onConferenceCancelled)},
		{sharedevents.SubjectDocumentReady, c.handle(ctx, c.onDocumentReady)},
		{sharedevents.SubjectConferenceCreated, c.handle(ctx, c.onAuditOnly("conference"))},
		{sharedevents.SubjectConferenceUpdated, c.handle(ctx, c.onAuditOnly("conference"))},
		{sharedevents.SubjectConferenceStatus, c.handle(ctx, c.onConferenceStatus)},
		{sharedevents.SubjectScheduleUpdated, c.handle(ctx, c.onScheduleUpdated)},
		{sharedevents.SubjectUserRoleChanged, c.handle(ctx, c.onAuditOnly("user"))},
		{sharedevents.SubjectUserDeleted, c.handle(ctx, c.onUserDeleted)},
		{sharedevents.SubjectUserActiveChanged, c.handle(ctx, c.onAuditOnly("user"))},
		{"user.password_reset_requested", c.handle(ctx, c.onPasswordReset)},
		{"verification.created", c.handle(ctx, c.onAuditOnly("verification"))},
		{"verification.decided", c.handle(ctx, c.onAuditOnly("verification"))},
		{"user.admin_queue_retried", c.handle(ctx, c.onAuditOnly("queue"))},
	}
	for _, s := range subs {
		durable := "engagement-" + strings.NewReplacer(".", "-", "_", "-").Replace(s.subject)
		if err := natsx.SubscribeDurable(nc, s.subject, durable, s.handler, failures...); err != nil {
			return fmt.Errorf("subscribe %s: %w", s.subject, err)
		}
	}
	return nil
}

func (c *Consumer) onConferenceStatus(ctx context.Context, env envelope) error {
	var p sharedevents.ConferenceStatusChanged
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.onAuditOnly("conference")(ctx, env); err != nil {
		return err
	}
	if c.delivery != nil && p.NewStatus == "cancelled" {
		return c.delivery.Queue(ctx, p.ConferenceID, p.Title, "", "conference_cancelled", env.EventID.String())
	}
	if c.delivery != nil && p.NewStatus == "registration_open" && p.OldStatus == "draft" {
		return c.delivery.Queue(ctx, p.ConferenceID, p.Title, "", "conference_published", env.EventID.String())
	}
	return nil
}

func (c *Consumer) onUserDeleted(ctx context.Context, env envelope) error {
	var payload struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return err
	}
	if payload.UserID == uuid.Nil {
		return errors.New("deleted user ID required")
	}
	tx := mailer.Transaction(ctx)
	if tx == nil {
		return errors.New("account deletion requires a transaction")
	}
	if err := c.onAuditOnly("user")(ctx, env); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE engagement_svc.subscriptions SET active=false,updated_at=now() WHERE user_id=$1 AND active`, payload.UserID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE engagement_svc.notifications SET status='cancelled',locked_at=NULL WHERE user_id=$1 AND status IN ('pending','failed')`, payload.UserID)
	return err
}

func (c *Consumer) onScheduleUpdated(ctx context.Context, env envelope) error {
	var p sharedevents.ScheduleUpdated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{EventID: env.EventID, EventType: env.EventType, EntityType: "conference", EntityID: ptrUUID(p.ConferenceID), ActorID: p.ActorID, Payload: p}); err != nil {
		return err
	}
	if c.delivery == nil {
		return nil
	}
	if err := c.delivery.Queue(ctx, p.ConferenceID, p.Title, p.Slug, "session_updated", env.EventID.String()); err != nil {
		return err
	}
	if p.SpeakerAdded {
		return c.delivery.Queue(ctx, p.ConferenceID, p.Title, p.Slug, "speaker_added", env.EventID.String())
	}
	return nil
}

func (c *Consumer) onDocumentReady(ctx context.Context, env envelope) error {
	var p sharedevents.DocumentReady
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "document", EntityID: ptrUUID(p.DocumentID), Payload: p,
	}); err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	expiresAt := p.DownloadExpiresAt.In(time.FixedZone("MSK", 3*60*60)).Format("02.01.2006 15:04 МСК")
	if locale == "en" {
		expiresAt = p.DownloadExpiresAt.UTC().Format("January 2, 2006 15:04 UTC")
	}
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Документ участника готов", "Participation document ready"), localizedTemplate(locale, "document_ready.html"),
		map[string]any{
			"UserName": p.UserName, "ConferenceTitle": p.ConferenceTitle,
			"DownloadURL": c.baseURL + "/api/v1/documents/download?token=" + p.DownloadToken,
			"ExpiresAt":   expiresAt,
		}, &uid, "document.ready")
}

func (c *Consumer) registrationPayload(ctx context.Context, env envelope) (sharedevents.RegistrationNotification, error) {
	var p sharedevents.RegistrationNotification
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return p, err
	}
	err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "registration", EntityID: ptrUUID(p.RegistrationID), Payload: p,
	})
	return p, err
}

func (c *Consumer) onRegistrationConfirmed(ctx context.Context, env envelope) error {
	p, err := c.registrationPayload(ctx, env)
	if err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	calendar := buildICS(p)
	return c.mailer.SendWithCalendar(ctx, p.UserEmail, localized(locale, "Регистрация подтверждена", "Registration confirmed"), localizedTemplate(locale, "registration_confirmed.html"),
		map[string]any{"UserName": p.UserName, "ConferenceTitle": p.ConferenceTitle, "StartDate": p.StartDate, "ConferenceURL": c.baseURL + "/conferences/" + p.ConferenceSlug},
		calendar, &uid, "registration.confirmed")
}

func (c *Consumer) onRegistrationWaitlisted(ctx context.Context, env envelope) error {
	p, err := c.registrationPayload(ctx, env)
	if err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Вы в очереди ожидания", "You are on the waitlist"), localizedTemplate(locale, "registration_waitlisted.html"),
		map[string]any{"UserName": p.UserName, "ConferenceTitle": p.ConferenceTitle, "Position": p.WaitlistPosition},
		&uid, "registration.waitlisted")
}

func (c *Consumer) onRegistrationPromoted(ctx context.Context, env envelope) error {
	p, err := c.registrationPayload(ctx, env)
	if err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.SendWithCalendar(ctx, p.UserEmail, localized(locale, "Место освободилось — регистрация подтверждена", "A place opened — registration confirmed"), localizedTemplate(locale, "registration_promoted.html"),
		map[string]any{"UserName": p.UserName, "ConferenceTitle": p.ConferenceTitle, "ConferenceURL": c.baseURL + "/conferences/" + p.ConferenceSlug},
		buildICS(p), &uid, "registration.promoted")
}

func (c *Consumer) onRegistrationCancelled(ctx context.Context, env envelope) error {
	p, err := c.registrationPayload(ctx, env)
	if err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Регистрация отменена", "Registration cancelled"), localizedTemplate(locale, "registration_cancelled.html"),
		map[string]any{"UserName": p.UserName, "ConferenceTitle": p.ConferenceTitle},
		&uid, "registration.cancelled")
}

func (c *Consumer) onConferenceCancelled(ctx context.Context, env envelope) error {
	p, err := c.registrationPayload(ctx, env)
	if err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Мероприятие отменено", "Event cancelled"), localizedTemplate(locale, "conference_cancelled.html"),
		map[string]any{"UserName": p.UserName, "ConferenceTitle": p.ConferenceTitle},
		&uid, "conference.cancelled")
}

func buildICS(p sharedevents.RegistrationNotification) []byte {
	start, startErr := time.Parse("2006-01-02", p.StartDate)
	end, endErr := time.Parse("2006-01-02", p.EndDate)
	if startErr != nil || endErr != nil {
		return nil
	}
	end = end.AddDate(0, 0, 1) // DTEND is exclusive for all-day events.
	escape := func(value string) string {
		value = strings.ReplaceAll(value, "\\", "\\\\")
		value = strings.ReplaceAll(value, ";", "\\;")
		value = strings.ReplaceAll(value, ",", "\\,")
		return strings.ReplaceAll(value, "\n", "\\n")
	}
	return []byte(fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//ConfHub//ConferenceHub//\r\nCALSCALE:GREGORIAN\r\nMETHOD:PUBLISH\r\nBEGIN:VEVENT\r\nUID:%s@confhub\r\nDTSTAMP:%s\r\nDTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\nSUMMARY:%s\r\nLOCATION:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		p.RegistrationID, time.Now().UTC().Format("20060102T150405Z"), start.Format("20060102"), end.Format("20060102"), escape(p.ConferenceTitle), escape(p.Location)))
}

func (c *Consumer) onVerificationRequested(ctx context.Context, env envelope) error {
	var p sharedevents.UserVerificationRequested
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "user", EntityID: ptrUUID(p.UserID), Payload: p,
	}); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/verify?token=%s", c.baseURL, p.VerificationToken)
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.Email, localized(locale, "Подтвердите email", "Confirm your email"), localizedTemplate(locale, "welcome.html"),
		map[string]any{"FirstName": p.FirstName, "VerifyURL": url}, &uid, "user.verification")
}

type envelope struct {
	TraceParent string          `json:"traceparent"`
	TraceState  string          `json:"tracestate"`
	RequestID   string          `json:"request_id"`
	EventID     uuid.UUID       `json:"event_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
}

func (c *Consumer) handle(ctx context.Context, h func(ctx context.Context, env envelope) error) func(*nats.Msg) error {
	return func(m *nats.Msg) error {
		var env envelope
		if err := json.Unmarshal(m.Data, &env); err != nil {
			slog.Warn("bad envelope", "subject", m.Subject, "err", err)
			return natsx.Permanent(err)
		}
		if env.EventID == uuid.Nil || env.EventType != m.Subject {
			return natsx.Permanent(errors.New("invalid event identity"))
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ctx = httputil.WithRequestID(ctx, env.RequestID)
		ctx, span := telemetry.Consumer(ctx, env.EventType, env.TraceParent, env.TraceState)
		defer span.End()
		tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		tag, err := tx.Exec(ctx, `INSERT INTO engagement_svc.processed_events (event_id, event_type)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, env.EventID, env.EventType)
		if err != nil {
			return fmt.Errorf("claim event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		ctx = mailer.WithTransaction(ctx, tx, env.EventID)
		if err := h(ctx, env); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.InfoContext(ctx, "event processed", "event_id", env.EventID, "event_type", env.EventType, "request_id", httputil.RequestIDFromContext(ctx))
		return nil
	}
}

func (c *Consumer) auditTx(ctx context.Context, e audit.Entry) error {
	if tx := mailer.Transaction(ctx); tx != nil {
		return audit.Insert(ctx, tx, e)
	}
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := audit.Insert(ctx, tx, e); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *Consumer) onUserRegistered(ctx context.Context, env envelope) error {
	var p sharedevents.UserRegistered
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "user", EntityID: ptrUUID(p.UserID), Payload: p,
	}); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/verify?token=%s", c.baseURL, p.VerificationToken)
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.Email, localized(locale, "Подтвердите email", "Confirm your email"), localizedTemplate(locale, "welcome.html"),
		map[string]any{"FirstName": p.FirstName, "VerifyURL": url}, &uid, "user.welcome")
}

func (c *Consumer) onApplicationSubmitted(ctx context.Context, env envelope) error {
	var p sharedevents.ApplicationSubmitted
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "application", EntityID: ptrUUID(p.ApplicationID), Payload: p,
	}); err != nil {
		return err
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Заявка получена", "Application received"), localizedTemplate(locale, "application_submitted.html"),
		map[string]any{
			"UserName":        p.UserName,
			"ConferenceTitle": p.ConferenceTitle,
			"Title":           p.Title,
			"Form":            applicationLabel(locale, p.Form),
		}, &uid, "application.submitted")
}

func (c *Consumer) onApplicationStatusChanged(ctx context.Context, env envelope) error {
	var p sharedevents.ApplicationStatusChanged
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	// User and compensation withdrawals do not send email.
	return c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "application", EntityID: ptrUUID(p.ApplicationID), Payload: p,
	})
}

func (c *Consumer) onApplicationModerated(ctx context.Context, env envelope) error {
	var p sharedevents.ApplicationModerated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "application", EntityID: ptrUUID(p.ApplicationID), Payload: p,
	}); err != nil {
		return err
	}
	if p.UserEmail == "" {
		return nil
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Решение по заявке", "Application decision"), localizedTemplate(locale, "application_moderated.html"),
		map[string]any{
			"UserName":        p.UserName,
			"ConferenceTitle": p.ConferenceTitle,
			"NewStatus":       applicationLabel(locale, p.NewStatus),
			"Comment":         p.Comment,
		}, &uid, "application.moderated")
}

func (c *Consumer) onReviewCreated(ctx context.Context, env envelope) error {
	var p sharedevents.ReviewCreated
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "review", EntityID: ptrUUID(p.ReviewID), Payload: p,
	}); err != nil {
		return err
	}
	if !p.IsVisibleToAuthor || p.UserEmail == "" {
		return nil
	}
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Получено ревью", "Review received"),
		localizedTemplate(locale, "review_created.html"),
		map[string]any{
			"UserName":       p.UserName,
			"Recommendation": applicationLabel(locale, p.Recommendation),
		}, &uid, "review.created")
}

func (c *Consumer) onPasswordReset(ctx context.Context, env envelope) error {
	var p struct {
		UserID     uuid.UUID `json:"user_id"`
		UserEmail  string    `json:"user_email"`
		UserName   string    `json:"user_name"`
		ResetToken string    `json:"reset_token"`
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	if err := c.auditTx(ctx, audit.Entry{
		EventID: env.EventID, EventType: env.EventType,
		EntityType: "user", EntityID: ptrUUID(p.UserID), Payload: p,
	}); err != nil {
		return err
	}
	if p.UserEmail == "" || p.ResetToken == "" {
		return errors.New("password_reset: missing fields")
	}
	url := fmt.Sprintf("%s/reset-password?token=%s", c.baseURL, p.ResetToken)
	locale, send, err := c.messageLocale(ctx, p.UserID)
	if err != nil || !send {
		return err
	}
	uid := p.UserID
	return c.mailer.Send(ctx, p.UserEmail, localized(locale, "Сброс пароля", "Reset password"), localizedTemplate(locale, "password_reset.html"),
		map[string]any{"UserName": p.UserName, "ResetURL": url}, &uid, "user.password_reset")
}

func (c *Consumer) onAuditOnly(entity string) func(ctx context.Context, env envelope) error {
	return func(ctx context.Context, env envelope) error {
		var p map[string]any
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return err
		}
		var eid *uuid.UUID
		if v, ok := p[entity+"_id"].(string); ok {
			if u, err := uuid.Parse(v); err == nil {
				eid = &u
			}
		}
		var actor *uuid.UUID
		if value, ok := p["changed_by"].(string); ok {
			if id, err := uuid.Parse(value); err == nil {
				actor = &id
			}
		}
		return c.auditTx(ctx, audit.Entry{
			EventID: env.EventID, EventType: env.EventType,
			EntityType: entity, EntityID: eid, ActorID: actor, Payload: p,
		})
	}
}

func ptrUUID(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func (c *Consumer) messageLocale(ctx context.Context, userID uuid.UUID) (locale string, send bool, err error) {
	if userID == uuid.Nil || c.identity == nil {
		return "ru", true, nil
	}
	locale, err = c.identity.Locale(ctx, userID)
	if errors.Is(err, client.ErrUserNotFound) {
		// Keep the audit, but do not send queued email to a deleted account.
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if locale != "en" {
		locale = "ru"
	}
	return locale, true, nil
}

func localized(locale, ru, en string) string {
	if locale == "en" {
		return en
	}
	return ru
}

func localizedTemplate(locale, name string) string {
	if locale != "en" {
		return name
	}
	return strings.TrimSuffix(name, ".html") + ".en.html"
}

func applicationLabel(locale, value string) string {
	labels := map[string][2]string{
		"speaker":            {"доклад", "talk"},
		"poster":             {"постер", "poster"},
		"listener":           {"слушатель", "listener"},
		"submitted":          {"подана", "submitted"},
		"under_review":       {"на рассмотрении", "under review"},
		"revision_requested": {"требуется доработка", "revision requested"},
		"accepted":           {"принята", "accepted"},
		"rejected":           {"отклонена", "rejected"},
		"withdrawn":          {"отозвана", "withdrawn"},
		"accept":             {"принять", "accept"},
		"minor_revision":     {"небольшая доработка", "minor revision"},
		"major_revision":     {"существенная доработка", "major revision"},
		"reject":             {"отклонить", "reject"},
	}
	label, ok := labels[value]
	if !ok {
		return value
	}
	if locale == "en" {
		return label[1]
	}
	return label[0]
}
