package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jung-kurt/gofpdf"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/outbox"

	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/application-svc/internal/repository"
)

func randomToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func (s *Applications) GenerateParticipationDocuments(ctx context.Context, conferenceID uuid.UUID, title string) error {
	registrations, err := s.regs.Confirmed(ctx, conferenceID)
	if err != nil {
		return err
	}
	for _, registration := range registrations {
		verificationCode, verificationHash, err := randomToken()
		if err != nil {
			return err
		}
		downloadToken, downloadHash, err := randomToken()
		if err != nil {
			return err
		}
		documentID := uuid.New()
		verifyURL := fmt.Sprintf("%s/verify-document?code=%s", s.baseURL, verificationCode)
		pdfBytes, err := s.renderParticipationPDF(registration.UserName, title, verifyURL, verificationCode)
		if err != nil {
			return err
		}
		key, err := s.putTracked(ctx, ".pdf", "application/pdf", int64(len(pdfBytes)), bytes.NewReader(pdfBytes))
		if err != nil {
			return err
		}
		doc := &domain.ParticipationDocument{ID: documentID, RegistrationID: registration.ID, UserID: registration.UserID,
			ConferenceID: conferenceID, ConferenceTitle: title, ParticipantName: registration.UserName, StoredName: key,
			Bucket: s.store.Bucket(), VerificationHash: verificationHash, DownloadTokenHash: downloadHash,
			DownloadExpiresAt: time.Now().Add(24 * time.Hour)}
		tx, err := s.repo.Pool().Begin(ctx)
		if err != nil {
			s.expediteCleanup(ctx, key)
			return err
		}
		if err := s.repo.CreateDocumentInTx(ctx, tx, doc); err != nil {
			_ = tx.Rollback(ctx)
			s.expediteCleanup(ctx, key)
			if errors.Is(err, repository.ErrDuplicate) {
				continue
			}
			return err
		}
		if err := outbox.Insert(ctx, tx, schemaName, sharedevents.SubjectDocumentReady, "document", doc.ID.String(), sharedevents.DocumentReady{
			DocumentID: doc.ID, UserID: doc.UserID, UserEmail: registration.UserEmail, UserName: registration.UserName,
			ConferenceID: conferenceID, ConferenceTitle: title, DownloadToken: downloadToken, DownloadExpiresAt: doc.DownloadExpiresAt,
		}); err != nil {
			_ = tx.Rollback(ctx)
			s.expediteCleanup(ctx, key)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			s.expediteCleanup(ctx, key)
			return err
		}
	}
	return nil
}

func (s *Applications) renderParticipationPDF(name, title, verifyURL, code string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	font := "Arial"
	if fontBytes, err := os.ReadFile(s.fontPath); err == nil {
		// Read bytes directly: gofpdf resolves filenames relative to its font directory.
		pdf.AddUTF8FontFromBytes("DejaVu", "", fontBytes)
		font = "DejaVu"
	}
	pdf.AddPage()
	pdf.SetFont(font, "", 24)
	pdf.CellFormat(190, 20, "Документ об участии", "", 1, "C", false, 0, "")
	pdf.Ln(12)
	pdf.SetFont(font, "", 14)
	pdf.MultiCell(190, 9, fmt.Sprintf("Настоящим подтверждается участие\n%s\nв мероприятии «%s».", name, title), "", "C", false)
	pdf.Ln(18)
	pdf.SetFont(font, "", 9)
	pdf.MultiCell(190, 5, "Проверить подлинность: "+verifyURL, "", "L", false)
	pdf.MultiCell(190, 5, "Проверочный код: "+code, "", "L", false)
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *Applications) ListParticipationDocuments(ctx context.Context, userID uuid.UUID) ([]domain.ParticipationDocument, error) {
	return s.repo.ListDocuments(ctx, userID)
}

func (s *Applications) ParticipationDocumentURL(ctx context.Context, id, userID uuid.UUID) (string, error) {
	doc, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		return "", fmt.Errorf("document not found: %w", httputil.ErrNotFound)
	}
	if doc.UserID != userID {
		return "", fmt.Errorf("not your document: %w", httputil.ErrForbidden)
	}
	return s.store.Presign(ctx, doc.StoredName, "participation-"+doc.ID.String()+".pdf")
}

func (s *Applications) VerifyParticipationDocument(ctx context.Context, code string) (*domain.ParticipationDocument, error) {
	code = strings.TrimSpace(code)
	raw, err := hex.DecodeString(code)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("invalid verification code: %w", httputil.ErrNotFound)
	}
	hash := sha256.Sum256([]byte(code))
	doc, err := s.repo.VerifyDocument(ctx, hash[:])
	if err != nil {
		return nil, fmt.Errorf("document not found: %w", httputil.ErrNotFound)
	}
	return doc, nil
}

func (s *Applications) DownloadByTokenURL(ctx context.Context, token string) (string, error) {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("invalid or expired download token: %w", httputil.ErrNotFound)
	}
	hash := sha256.Sum256([]byte(token))
	doc, err := s.repo.DocumentByDownloadToken(ctx, hash[:])
	if err != nil {
		return "", fmt.Errorf("invalid or expired download token: %w", httputil.ErrNotFound)
	}
	return s.store.Presign(ctx, doc.StoredName, "participation-"+doc.ID.String()+".pdf")
}
