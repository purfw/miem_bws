package subscriptions

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"strings"
	"time"

	"github.com/confhub/engagement-svc/internal/client"
	"github.com/confhub/engagement-svc/internal/mailer"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type localeResolver interface {
	Locale(context.Context, uuid.UUID) (string, error)
}
type conferenceResolver interface {
	Get(context.Context, uuid.UUID) (*client.Conference, error)
}

type Delivery struct {
	pool        *pgxpool.Pool
	mailer      *mailer.Mailer
	identity    localeResolver
	conferences conferenceResolver
	baseURL     string
}

func NewDelivery(pool *pgxpool.Pool, m *mailer.Mailer, identity localeResolver, conferences conferenceResolver, baseURL string) *Delivery {
	return &Delivery{pool: pool, mailer: m, identity: identity, conferences: conferences, baseURL: strings.TrimRight(baseURL, "/")}
}

var messageTemplate = template.Must(template.New("subscription").Parse(`<html><body><h2>{{.Subject}}</h2><p>{{.Title}}</p><p>{{.Message}}</p><p><a href="{{.URL}}">{{.Open}}</a></p><p><a href="{{.UnsubscribeURL}}">{{.Unsubscribe}}</a></p></body></html>`))

func (d *Delivery) Queue(ctx context.Context, conferenceID uuid.UUID, title, slug, criterion, occurrence string) error {
	tx := mailer.Transaction(ctx)
	if tx == nil {
		return errors.New("subscription delivery requires a transaction")
	}
	var hasCriteria bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM engagement_svc.subscriptions WHERE conference_id IS NULL AND active AND $1=ANY(notify_on))`, criterion).Scan(&hasCriteria); err != nil {
		return err
	}
	var conference *client.Conference
	var err error
	if hasCriteria {
		conference, err = d.conferences.Get(ctx, conferenceID)
		if err != nil {
			return err
		}
		if conference != nil && slug == "" {
			slug = conference.Slug
		}
	}
	rows, err := tx.Query(ctx, `SELECT id,user_id,user_email,COALESCE(unsubscribe_token,''),conference_id IS NULL,criteria FROM engagement_svc.subscriptions
		WHERE (conference_id=$1 OR conference_id IS NULL) AND active AND $2=ANY(notify_on) AND 'email'=ANY(channels) ORDER BY id FOR UPDATE`, conferenceID, criterion)
	if err != nil {
		return err
	}
	type recipient struct {
		id, userID   uuid.UUID
		email, token string
		filtered     bool
		criteria     Criteria
	}
	var recipients []recipient
	for rows.Next() {
		var r recipient
		if err := rows.Scan(&r.id, &r.userID, &r.email, &r.token, &r.filtered, &r.criteria); err != nil {
			rows.Close()
			return err
		}
		if !r.filtered || r.criteria.Matches(conference) {
			recipients = append(recipients, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range recipients {
		tag, err := tx.Exec(ctx, `INSERT INTO engagement_svc.subscription_deliveries(subscription_id,criterion,occurrence) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, r.id, criterion, conferenceID.String()+":"+occurrence)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		locale, err := d.identity.Locale(ctx, r.userID)
		if errors.Is(err, client.ErrUserNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if r.token == "" {
			var hash []byte
			r.token, hash, err = newToken()
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE engagement_svc.subscriptions SET unsubscribe_token=$2,unsubscribe_token_hash=$3 WHERE id=$1`, r.id, r.token, hash); err != nil {
				return err
			}
		}
		subject, text := messageText(locale, criterion)
		open, unsubscribe := "Открыть конференцию", "Отписаться"
		if locale == "en" {
			open, unsubscribe = "Open conference", "Unsubscribe"
		}
		var body bytes.Buffer
		url := d.baseURL + "/conferences/" + slug
		if slug == "" {
			url = d.baseURL + "/conferences"
		}
		if err := messageTemplate.Execute(&body, map[string]string{"Subject": subject, "Title": title, "Message": text, "URL": url, "Open": open, "Unsubscribe": unsubscribe, "UnsubscribeURL": d.baseURL + "/api/v1/subscriptions/unsubscribe/" + r.token}); err != nil {
			return err
		}
		if err := d.mailer.Enqueue(ctx, r.email, subject, body.String(), nil, &r.userID, "subscription."+criterion, &r.id); err != nil {
			return err
		}
	}
	return nil
}

func messageText(locale, criterion string) (string, string) {
	if locale == "en" {
		switch criterion {
		case "conference_published":
			return "New conference", "Registration has opened for a conference matching your filters."
		case "deadline_approaching":
			return "Registration closes soon", "Registration closes within 24 hours."
		case "speaker_added":
			return "A speaker was added", "A new speaker has been added to the programme."
		case "conference_cancelled":
			return "Conference cancelled", "The organizer has cancelled this conference."
		default:
			return "Programme updated", "The conference programme has changed."
		}
	}
	switch criterion {
	case "conference_published":
		return "Новая конференция", "Открыта регистрация на конференцию, подходящую под ваши фильтры."
	case "deadline_approaching":
		return "Регистрация скоро закончится", "До окончания регистрации осталось менее 24 часов."
	case "speaker_added":
		return "Добавлен спикер", "В программе конференции появился новый спикер."
	case "conference_cancelled":
		return "Конференция отменена", "Организатор отменил конференцию."
	default:
		return "Программа обновлена", "Программа конференции изменилась."
	}
}

func (d *Delivery) RunReminders(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := d.reminders(ctx); err != nil {
			slog.Error("subscription reminders", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Delivery) reminders(ctx context.Context) error {
	var filtered bool
	if err := d.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM engagement_svc.subscriptions WHERE conference_id IS NULL AND active AND 'deadline_approaching'=ANY(notify_on))`).Scan(&filtered); err != nil {
		return err
	}
	if source, ok := d.conferences.(interface {
		Due(context.Context, string) ([]client.Conference, string, error)
	}); ok && filtered {
		cursor := ""
		for page := 0; page < 100; page++ {
			items, next, err := source.Due(ctx, cursor)
			if err != nil {
				return err
			}
			for i := range items {
				if err := d.queueReminder(ctx, &items[i]); err != nil {
					return err
				}
			}
			if next == "" {
				break
			}
			if next == cursor {
				return errors.New("reminder cursor did not advance")
			}
			cursor = next
		}
	}
	rows, err := d.pool.Query(ctx, `SELECT DISTINCT conference_id FROM engagement_svc.subscriptions WHERE conference_id IS NOT NULL AND active AND 'deadline_approaching'=ANY(notify_on) AND 'email'=ANY(channels)`)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		conference, err := d.conferences.Get(ctx, id)
		if err != nil {
			slog.Warn("reminder conference", "conference_id", id, "err", err)
			continue
		}
		if conference == nil || conference.Status != "registration_open" || !deadlineDue(conference.Deadline, time.Now()) {
			continue
		}
		if err := d.queueReminder(ctx, conference); err != nil {
			slog.Error("queue reminder", "conference_id", id, "err", err)
		}
	}
	return nil
}

func deadlineDue(deadline *time.Time, now time.Time) bool {
	return deadline != nil && deadline.After(now) && !deadline.After(now.Add(24*time.Hour))
}

func (d *Delivery) queueReminder(ctx context.Context, conference *client.Conference) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	ctx = mailer.WithTransaction(ctx, tx, uuid.Nil)
	if err := d.Queue(ctx, conference.ID, conference.Title, conference.Slug, "deadline_approaching", conference.Deadline.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
