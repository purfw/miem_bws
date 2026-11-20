package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/outbox"

	"github.com/confhub/application-svc/internal/client"
	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/application-svc/internal/repository"
)

const schemaName = "application_svc"

type Applications struct {
	repo                      *repository.Applications
	ident                     *client.Identity
	store                     objectStore
	conf                      *client.ConferenceCache
	max                       int64
	regs                      *client.RegistrationClient
	baseURL                   string
	fontPath                  string
	orcidPublicationThreshold int
}

func New(repo *repository.Applications, store objectStore, conf *client.ConferenceCache, ident *client.Identity, regs *client.RegistrationClient, maxBytes int64, baseURL, fontPath string, orcidPublicationThreshold int) *Applications {
	return &Applications{repo: repo, store: store, conf: conf, ident: ident, regs: regs, max: maxBytes, baseURL: baseURL, fontPath: fontPath, orcidPublicationThreshold: orcidPublicationThreshold}
}

type SubmitInput struct {
	ConferenceID uuid.UUID
	SectionID    *uuid.UUID
	UserID       uuid.UUID
	UserEmail    string
	UserName     string
	Form         domain.Form
	Title        string
	Abstract     *string
	CoAuthors    []domain.CoAuthor
	Keywords     []string
}

func (s *Applications) Submit(ctx context.Context, in *SubmitInput) (*domain.Application, error) {
	if !domain.ValidForm(in.Form) {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "form", Message: "speaker|poster|listener"}}}
	}
	if strings.TrimSpace(in.Title) == "" || utf8.RuneCountInString(in.Title) > 500 {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "title", Message: "required, maximum 500 characters"}}}
	}
	conf, err := s.conf.Get(ctx, in.ConferenceID)
	if err != nil {
		if errors.Is(err, client.ErrConferenceNotFound) {
			return nil, fmt.Errorf("conference not found: %w", httputil.ErrNotFound)
		}
		return nil, fmt.Errorf("conference status: %w", err)
	}
	if conf.Status != "registration_open" {
		return nil, fmt.Errorf("registration is %s: %w", conf.Status, httputil.ErrConflict)
	}
	if conf.RegistrationDeadline != nil && time.Now().After(*conf.RegistrationDeadline) {
		return nil, fmt.Errorf("deadline passed: %w", httputil.ErrConflict)
	}
	if err := s.validateSection(ctx, in.ConferenceID, in.SectionID); err != nil {
		return nil, err
	}

	email, name := in.UserEmail, in.UserName
	if p, err := s.ident.Profile(ctx, in.UserID); err == nil {
		email, name = p.Email, p.Name
	} else {
		slog.WarnContext(ctx, "identity profile unavailable, falling back to headers",
			"err", err, "user_id", in.UserID)
	}

	a := &domain.Application{
		ConferenceID:    in.ConferenceID,
		SectionID:       in.SectionID,
		UserID:          in.UserID,
		CachedUserEmail: email,
		CachedUserName:  name,
		CachedConfTitle: conf.Title,
		Form:            in.Form,
		Title:           in.Title,
		Abstract:        in.Abstract,
		CoAuthors:       in.CoAuthors,
		Keywords:        in.Keywords,
		Status:          domain.StatusSubmitted,
	}

	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.CreateInTx(ctx, tx, a); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, fmt.Errorf("already applied to this conference with this form: %w", httputil.ErrConflict)
		}
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectAppSubmitted, "application", a.ID.String(),
		sharedevents.ApplicationSubmitted{
			ApplicationID:   a.ID,
			ConferenceID:    a.ConferenceID,
			ConferenceTitle: a.CachedConfTitle,
			UserID:          a.UserID,
			UserEmail:       a.CachedUserEmail,
			UserName:        a.CachedUserName,
			Title:           a.Title,
			Form:            string(a.Form),
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// SetStatus applies user transitions; Moderate handles administrative decisions.
func (s *Applications) SetStatus(ctx context.Context, id uuid.UUID, requester uuid.UUID, target domain.Status) (*domain.Application, error) {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	cur, err := s.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if cur.UserID != requester {
		return nil, fmt.Errorf("not your application: %w", httputil.ErrForbidden)
	}
	if !domain.CanTransition(cur.Status, target) {
		return nil, fmt.Errorf("%s -> %s: %w", cur.Status, target, httputil.ErrConflict)
	}
	if cur.Status == target {
		return cur, nil
	}
	by := requester
	if err := s.repo.SetStatusInTx(ctx, tx, id, target, &by, nil); err != nil {
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectAppStatusChanged, "application", id.String(),
		sharedevents.ApplicationStatusChanged{
			ApplicationID:   id,
			ConferenceID:    cur.ConferenceID,
			ConferenceTitle: cur.CachedConfTitle,
			UserID:          cur.UserID,
			UserEmail:       cur.CachedUserEmail,
			UserName:        cur.CachedUserName,
			OldStatus:       string(cur.Status),
			NewStatus:       string(target),
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cur.Status = target
	return cur, nil
}

func (s *Applications) Moderate(ctx context.Context, id uuid.UUID, target domain.Status, comment *string, by uuid.UUID) (*domain.Application, error) {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	cur, err := s.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if !domain.CanTransition(cur.Status, target) {
		return nil, fmt.Errorf("%s -> %s: %w", cur.Status, target, httputil.ErrConflict)
	}
	if err := s.repo.SetStatusInTx(ctx, tx, id, target, &by, comment); err != nil {
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectAppModerated, "application", id.String(),
		sharedevents.ApplicationModerated{
			ApplicationID:   id,
			ConferenceID:    cur.ConferenceID,
			ConferenceTitle: cur.CachedConfTitle,
			UserID:          cur.UserID,
			UserEmail:       cur.CachedUserEmail,
			UserName:        cur.CachedUserName,
			NewStatus:       string(target),
			Comment:         deref(comment),
			ChangedBy:       by,
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cur.Status = target
	return cur, nil
}

func (s *Applications) UpdateMeta(ctx context.Context, id uuid.UUID, requester uuid.UUID, p repository.ApplicationPatch) (*domain.Application, error) {
	if p.Title != nil && (strings.TrimSpace(*p.Title) == "" || utf8.RuneCountInString(*p.Title) > 500) {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "title", Message: "required, maximum 500 characters"}}}
	}
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	cur, err := s.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if cur.UserID != requester {
		return nil, fmt.Errorf("not your application: %w", httputil.ErrForbidden)
	}
	switch cur.Status {
	case domain.StatusSubmitted, domain.StatusRevisionRequested:
	default:
		return nil, fmt.Errorf("not editable in %s: %w", cur.Status, httputil.ErrConflict)
	}
	if err := s.validateSection(ctx, cur.ConferenceID, p.SectionID); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateMetaInTx(ctx, tx, id, p); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Applications) validateSection(ctx context.Context, conferenceID uuid.UUID, sectionID *uuid.UUID) error {
	if sectionID == nil {
		return nil
	}
	belongs, err := s.conf.CheckSection(ctx, conferenceID, *sectionID)
	if err != nil {
		return err
	}
	if !belongs {
		return &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "section_id", Message: "section must belong to this conference"}}}
	}
	return nil
}

func (s *Applications) AttachFile(ctx context.Context, applicationID, requester uuid.UUID, originalName string, declaredSize int64, body io.Reader) (*domain.ApplicationFile, error) {
	preflight, err := s.repo.GetByID(ctx, applicationID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, httputil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if preflight.UserID != requester {
		return nil, httputil.ErrForbidden
	}
	if preflight.Status != domain.StatusSubmitted && preflight.Status != domain.StatusRevisionRequested {
		return nil, httputil.ErrConflict
	}
	if utf8.RuneCountInString(originalName) > 500 || strings.TrimSpace(originalName) == "" || declaredSize <= 0 || declaredSize > s.max {
		return nil, httputil.ErrValidation
	}
	mime, ext, restored, err := DetectFileKind(body)
	if err != nil {
		return nil, httputil.ErrValidation
	}
	key, err := s.prepareObject(ctx, ext)
	if err != nil {
		return nil, err
	}
	saved := false
	defer func() {
		if !saved {
			s.expediteCleanup(ctx, key)
		}
	}()
	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	app, err := s.repo.GetByIDTx(ctx, tx, applicationID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if app.UserID != requester {
		return nil, fmt.Errorf("not your application: %w", httputil.ErrForbidden)
	}
	if app.Status != domain.StatusSubmitted && app.Status != domain.StatusRevisionRequested {
		return nil, httputil.ErrConflict
	}
	// Cap the stream even if Content-Length lied.
	limited := &countingReader{r: io.LimitReader(restored, s.max+1)}
	err = s.store.PutAt(ctx, key, mime, declaredSize, limited)
	if err != nil {
		return nil, err
	}
	if limited.n > s.max {
		return nil, fmt.Errorf("file exceeds max: %w", httputil.ErrValidation)
	}
	f := &domain.ApplicationFile{
		ApplicationID: applicationID,
		OriginalName:  originalName,
		StoredName:    key,
		MimeType:      mime,
		SizeBytes:     limited.n,
		Bucket:        s.store.Bucket(),
		UploadedBy:    requester,
	}
	if err := s.repo.InsertFileInTx(ctx, tx, f); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	saved = true
	return f, nil
}

func (s *Applications) PresignFile(ctx context.Context, fileID, requester uuid.UUID, isModerator bool) (string, *domain.ApplicationFile, error) {
	f, err := s.repo.GetFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil, fmt.Errorf("file not found: %w", httputil.ErrNotFound)
		}
		return "", nil, err
	}
	app, err := s.repo.GetByID(ctx, f.ApplicationID)
	if err != nil {
		return "", nil, err
	}
	if app.UserID != requester && !isModerator {
		return "", nil, fmt.Errorf("forbidden: %w", httputil.ErrForbidden)
	}
	url, err := s.store.Presign(ctx, f.StoredName, f.OriginalName)
	if err != nil {
		return "", nil, err
	}
	return url, f, nil
}

func (s *Applications) DeleteFile(ctx context.Context, fileID, requester uuid.UUID) error {
	f, err := s.repo.GetFile(ctx, fileID)
	if err != nil {
		return err
	}
	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	app, err := s.repo.GetByIDTx(ctx, tx, f.ApplicationID)
	if err != nil {
		return err
	}
	if app.UserID != requester {
		return fmt.Errorf("forbidden: %w", httputil.ErrForbidden)
	}
	if app.Status != domain.StatusSubmitted && app.Status != domain.StatusRevisionRequested {
		return httputil.ErrConflict
	}
	if err := s.repo.DeleteFileInTx(ctx, tx, fileID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO application_svc.storage_deletions (bucket,stored_name) VALUES ($1,$2)
        ON CONFLICT (bucket,stored_name) DO UPDATE SET status='pending',attempts=0,next_attempt_at=now(),last_error=NULL,completed_at=NULL`, f.Bucket, f.StoredName); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

type ReviewInput struct {
	ApplicationID     uuid.UUID
	ReviewerID        uuid.UUID
	ReviewerName      string
	Score             int16
	Summary           string
	Strengths         *string
	Weaknesses        *string
	Recommendation    domain.Recommendation
	IsVisibleToAuthor bool
}

func (s *Applications) CreateReview(ctx context.Context, in *ReviewInput) (*domain.Review, error) {
	if in.Score < 1 || in.Score > 10 {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "score", Message: "1..10"}}}
	}
	if !domain.ValidRecommendation(in.Recommendation) {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "recommendation", Message: "invalid"}}}
	}
	if fields := validateReviewText(in); len(fields) > 0 {
		return nil, &httputil.ValidationError{Fields: fields}
	}
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	app, err := s.repo.GetByIDTx(ctx, tx, in.ApplicationID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	rv := &domain.Review{
		ApplicationID:      in.ApplicationID,
		ReviewerID:         in.ReviewerID,
		CachedReviewerName: in.ReviewerName,
		Score:              in.Score,
		Summary:            in.Summary,
		Strengths:          in.Strengths,
		Weaknesses:         in.Weaknesses,
		Recommendation:     in.Recommendation,
		IsVisibleToAuthor:  in.IsVisibleToAuthor,
	}
	if err := s.repo.InsertReviewInTx(ctx, tx, rv); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, fmt.Errorf("already reviewed: %w", httputil.ErrConflict)
		}
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectReviewCreated, "review", rv.ID.String(),
		sharedevents.ReviewCreated{
			ReviewID:          rv.ID,
			ApplicationID:     rv.ApplicationID,
			UserID:            app.UserID,
			UserEmail:         app.CachedUserEmail,
			UserName:          app.CachedUserName,
			Recommendation:    string(rv.Recommendation),
			IsVisibleToAuthor: rv.IsVisibleToAuthor,
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return rv, nil
}

// maxReviewText bounds every free-text review field.
const maxReviewText = 5000

func validateReviewText(in *ReviewInput) []httputil.FieldError {
	var fields []httputil.FieldError
	in.Summary = strings.TrimSpace(in.Summary)
	if in.Summary == "" || utf8.RuneCountInString(in.Summary) > maxReviewText {
		fields = append(fields, httputil.FieldError{Field: "summary", Message: "required, maximum 5000 characters"})
	}
	for _, optional := range []struct {
		name  string
		value **string
	}{{"strengths", &in.Strengths}, {"weaknesses", &in.Weaknesses}} {
		if *optional.value == nil {
			continue
		}
		trimmed := strings.TrimSpace(**optional.value)
		if trimmed == "" {
			*optional.value = nil
			continue
		}
		if utf8.RuneCountInString(trimmed) > maxReviewText {
			fields = append(fields, httputil.FieldError{Field: optional.name, Message: "maximum 5000 characters"})
			continue
		}
		*optional.value = &trimmed
	}
	in.ReviewerName = strings.TrimSpace(in.ReviewerName)
	if in.ReviewerName == "" || utf8.RuneCountInString(in.ReviewerName) > 255 {
		fields = append(fields, httputil.FieldError{Field: "reviewer_name", Message: "required, maximum 255 characters"})
	}
	return fields
}

func (s *Applications) ListReviews(ctx context.Context, appID, requester uuid.UUID, isModerator bool) ([]domain.Review, error) {
	app, err := s.repo.GetByID(ctx, appID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	visibleOnly := !isModerator
	if app.UserID == requester {
		visibleOnly = true
	}
	if app.UserID != requester && !isModerator {
		return nil, fmt.Errorf("forbidden: %w", httputil.ErrForbidden)
	}
	return s.repo.ListReviews(ctx, appID, !visibleOnly)
}

// HandleConferenceCancelled withdraws in-flight applications and emits their updates.
func (s *Applications) HandleConferenceCancelled(ctx context.Context, conferenceID uuid.UUID, conferenceTitle string) error {
	tx, err := s.repo.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ids, err := s.repo.WithdrawByConference(ctx, tx, conferenceID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		a, err := s.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := outbox.Insert(ctx, tx, schemaName,
			sharedevents.SubjectAppStatusChanged, "application", id.String(),
			sharedevents.ApplicationStatusChanged{
				ApplicationID:   id,
				ConferenceID:    conferenceID,
				ConferenceTitle: conferenceTitle,
				UserID:          a.UserID,
				UserEmail:       a.CachedUserEmail,
				UserName:        a.CachedUserName,
				OldStatus:       "in_flight",
				NewStatus:       string(domain.StatusWithdrawn),
			}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Applications) GetByID(ctx context.Context, id uuid.UUID) (*domain.Application, error) {
	return s.repo.GetByID(ctx, id)
}

// ApplicationReviews returns an application with every review, including
// those hidden from its author, for the administrator console.
func (s *Applications) ApplicationReviews(ctx context.Context, id uuid.UUID) (*domain.Application, []domain.Review, error) {
	app, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, fmt.Errorf("application not found: %w", httputil.ErrNotFound)
		}
		return nil, nil, err
	}
	reviews, err := s.repo.ListReviews(ctx, id, true)
	if err != nil {
		return nil, nil, err
	}
	return app, reviews, nil
}
func (s *Applications) ListFiles(ctx context.Context, appID uuid.UUID) ([]domain.ApplicationFile, error) {
	return s.repo.ListFiles(ctx, appID)
}
func (s *Applications) List(ctx context.Context, f repository.ListFilter) ([]domain.Application, error) {
	return s.repo.List(ctx, f)
}

func (s *Applications) Participants(ctx context.Context, conferenceID uuid.UUID, limit, offset int) ([]repository.Participant, int, error) {
	return s.repo.Participants(ctx, conferenceID, limit, offset)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
