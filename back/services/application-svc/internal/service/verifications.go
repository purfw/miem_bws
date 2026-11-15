package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/application-svc/internal/repository"
)

func (s *Applications) CreateVerification(ctx context.Context, userID uuid.UUID, email, name, organization string, emailVerified bool) (*domain.OrganizerVerification, error) {
	if !emailVerified {
		return nil, fmt.Errorf("email must be verified: %w", httputil.ErrForbidden)
	}
	organization = strings.TrimSpace(organization)
	trustedOrganization, trusted, err := s.repo.IsTrustedDomain(ctx, email)
	if err != nil {
		return nil, err
	}
	if organization == "" {
		organization = trustedOrganization
	}
	if organization == "" {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "organization", Message: "required"}}}
	}
	if utf8.RuneCountInString(organization) > 500 {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "organization", Message: "maximum 500 characters"}}}
	}
	v := &domain.OrganizerVerification{
		UserID: userID, UserEmail: email, UserName: name, Organization: organization,
		Method: "documents", Status: domain.VerificationPending,
	}
	if trusted {
		v.Method = "trusted_domain"
		v.Status = domain.VerificationApproved
	} else if identity, identityErr := s.ident.ResearchIdentity(ctx, userID); identityErr == nil &&
		identity.Provider == "orcid" && identity.PublicationCount >= s.orcidPublicationThreshold {
		v.Method = "orcid"
		v.Status = domain.VerificationApproved
	}
	if err := s.repo.CreateVerification(ctx, v); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, fmt.Errorf("an active verification already exists: %w", httputil.ErrConflict)
		}
		return nil, err
	}
	return v, nil
}

func (s *Applications) ListMyVerifications(ctx context.Context, userID uuid.UUID) ([]domain.OrganizerVerification, error) {
	items, err := s.repo.ListVerifications(ctx, &userID, "")
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Documents, _ = s.repo.ListVerificationDocuments(ctx, items[i].ID)
	}
	return items, nil
}

func (s *Applications) AttachVerificationDocument(ctx context.Context, verificationID, userID uuid.UUID, originalName string, declaredSize int64, body io.Reader) (*domain.VerificationDocument, error) {
	originalName = strings.TrimSpace(originalName)
	if originalName == "" || utf8.RuneCountInString(originalName) > 500 || strings.ContainsAny(originalName, "\x00\r\n") {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "document", Message: "invalid file name"}}}
	}
	v, err := s.repo.GetVerification(ctx, verificationID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("verification not found: %w", httputil.ErrNotFound)
		}
		return nil, err
	}
	if v.UserID != userID {
		return nil, fmt.Errorf("not your verification: %w", httputil.ErrForbidden)
	}
	if v.Status != domain.VerificationPending {
		return nil, fmt.Errorf("verification is already decided: %w", httputil.ErrConflict)
	}
	if declaredSize <= 0 || declaredSize > s.max {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "document", Message: "invalid file size"}}}
	}
	mimeType, ext, restored, err := DetectFileKind(body)
	if err != nil || (mimeType != "application/pdf" && mimeType != "image/png" && !strings.Contains(mimeType, "wordprocessingml")) {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "document", Message: "PDF, DOCX or PNG required"}}}
	}
	limited := &countingReader{r: io.LimitReader(restored, s.max+1)}
	key, err := s.putTracked(ctx, ext, mimeType, declaredSize, limited)
	if err != nil {
		return nil, err
	}
	if limited.n > s.max {
		s.expediteCleanup(ctx, key)
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "document", Message: "file too large"}}}
	}
	d := &domain.VerificationDocument{
		VerificationID: verificationID, OriginalName: originalName, StoredName: key,
		MimeType: mimeType, SizeBytes: limited.n, Bucket: s.store.Bucket(),
	}
	if err := s.repo.AddVerificationDocument(ctx, d); err != nil {
		s.expediteCleanup(ctx, key)
		if errors.Is(err, repository.ErrStateConflict) {
			return nil, httputil.ErrConflict
		}
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httputil.ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func (s *Applications) VerificationDocumentURL(ctx context.Context, documentID, userID uuid.UUID, admin bool) (string, error) {
	d, err := s.repo.GetVerificationDocument(ctx, documentID)
	if err != nil {
		return "", fmt.Errorf("document not found: %w", httputil.ErrNotFound)
	}
	v, err := s.repo.GetVerification(ctx, d.VerificationID)
	if err != nil {
		return "", err
	}
	if !admin && v.UserID != userID {
		return "", fmt.Errorf("not your document: %w", httputil.ErrForbidden)
	}
	return s.store.Presign(ctx, d.StoredName, d.OriginalName)
}

func (s *Applications) ListVerificationQueue(ctx context.Context, status string, limit, offset int) ([]domain.OrganizerVerification, error) {
	items, err := s.repo.ListVerificationsPage(ctx, nil, status, limit, offset)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Documents, _ = s.repo.ListVerificationDocuments(ctx, items[i].ID)
	}
	return items, nil
}

func (s *Applications) ModerateVerification(ctx context.Context, id, reviewer uuid.UUID, status domain.VerificationStatus, reason *string) (*domain.OrganizerVerification, error) {
	if status != domain.VerificationApproved && status != domain.VerificationRejected {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "status", Message: "approved or rejected required"}}}
	}
	if status == domain.VerificationRejected && (reason == nil || strings.TrimSpace(*reason) == "") {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "reason", Message: "required for rejection"}}}
	}
	v, err := s.repo.ModerateVerification(ctx, id, reviewer, status, reason)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("verification missing or already decided: %w", httputil.ErrConflict)
	}
	return v, err
}
