package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedevents "github.com/confhub/shared/events"
	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/outbox"

	"github.com/confhub/identity-svc/internal/domain"
	"github.com/confhub/identity-svc/internal/repository"
)

const (
	verifyTokenTTL = 24 * time.Hour
	resetTokenTTL  = time.Hour
	schemaName     = "identity_svc"
)

type Auth struct {
	users   *repository.Users
	jwt     *jwtauth.Issuer
	refresh *RefreshStore
	rl      *LoginRateLimiter
	atTTL   time.Duration
	checker PasswordChecker
}

func NewAuth(users *repository.Users, jwt *jwtauth.Issuer, refresh *RefreshStore, rl *LoginRateLimiter, atTTL time.Duration, checker PasswordChecker) *Auth {
	return &Auth{users: users, jwt: jwt, refresh: refresh, rl: rl, atTTL: atTTL, checker: checker}
}

type RegisterInput struct {
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	Patronymic  *string `json:"patronymic,omitempty"`
	Affiliation *string `json:"affiliation,omitempty"`
	Locale      string  `json:"locale,omitempty"`
}

func (in *RegisterInput) Validate() error {
	var fe []httputil.FieldError
	address, emailErr := mail.ParseAddress(strings.TrimSpace(in.Email))
	if emailErr != nil || address.Address != strings.TrimSpace(in.Email) || strings.ContainsAny(in.Email, "\r\n") || len(in.Email) > 255 {
		fe = append(fe, httputil.FieldError{Field: "email", Message: "invalid email"})
	}
	if utf8.RuneCountInString(in.Password) < 12 || len(in.Password) > 1024 {
		fe = append(fe, httputil.FieldError{Field: "password", Message: "min 12 chars"})
	}
	if strings.TrimSpace(in.FirstName) == "" {
		fe = append(fe, httputil.FieldError{Field: "first_name", Message: "required"})
	}
	if strings.TrimSpace(in.LastName) == "" {
		fe = append(fe, httputil.FieldError{Field: "last_name", Message: "required"})
	}
	if in.Locale == "" {
		in.Locale = "ru"
	}
	if in.Locale != "ru" && in.Locale != "en" {
		fe = append(fe, httputil.FieldError{Field: "locale", Message: "must be ru or en"})
	}
	if len(fe) > 0 {
		return &httputil.ValidationError{Fields: fe}
	}
	return nil
}

func (a *Auth) Register(ctx context.Context, in *RegisterInput) (*domain.User, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if err := validatePassword(ctx, a.checker, in.Password); err != nil {
		return nil, err
	}
	hash, err := HashPasswordContext(ctx, in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	u := &domain.User{
		Email:        strings.ToLower(strings.TrimSpace(in.Email)),
		PasswordHash: hash,
		FirstName:    strings.TrimSpace(in.FirstName),
		LastName:     strings.TrimSpace(in.LastName),
		Patronymic:   in.Patronymic,
		Affiliation:  in.Affiliation,
		Role:         domain.RoleUser,
		Locale:       in.Locale,
	}

	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := a.users.CreateInTx(ctx, tx, u); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, fmt.Errorf("email already registered: %w", httputil.ErrConflict)
		}
		return nil, err
	}
	verifyTok, err := a.users.CreateVerificationTokenInTx(ctx, tx, u.ID, verifyTokenTTL)
	if err != nil {
		return nil, err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectUserRegistered, "user", u.ID.String(),
		sharedevents.UserRegistered{
			UserID:            u.ID,
			Email:             u.Email,
			FirstName:         u.FirstName,
			LastName:          u.LastName,
			VerificationToken: verifyTok.String(),
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

type LoginOutput struct {
	AccessToken  string
	AccessExpiry time.Time
	RefreshToken string
	User         *domain.User
}

func (a *Auth) Login(ctx context.Context, ip, email, password, ua string) (*LoginOutput, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := a.rl.Check(ctx, ip, email); err != nil {
		if errors.Is(err, ErrTooMany) {
			return nil, fmt.Errorf("rate limit: %w", httputil.ErrTooMany)
		}
		return nil, err
	}
	u, err := a.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			if _, err := VerifyPasswordContext(ctx, password, dummyHash); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("invalid credentials: %w", httputil.ErrUnauthorized)
		}
		return nil, err
	}
	if !u.IsActive {
		return nil, fmt.Errorf("account disabled: %w", httputil.ErrForbidden)
	}
	ok, err := VerifyPasswordContext(ctx, password, u.PasswordHash)
	if errors.Is(err, httputil.ErrUnavailable) {
		return nil, err
	}
	if err != nil || !ok {
		return nil, fmt.Errorf("invalid credentials: %w", httputil.ErrUnauthorized)
	}
	return a.issueLogin(ctx, u, ua)
}

func (a *Auth) issueLogin(ctx context.Context, u *domain.User, ua string) (*LoginOutput, error) {
	if !u.IsActive {
		return nil, fmt.Errorf("account disabled: %w", httputil.ErrForbidden)
	}
	at, exp, err := a.jwt.Issue(u.ID.String(), jwtauth.Role(u.Role), u.Email, u.FullName(), u.EmailVerified, u.AuthVersion)
	if err != nil {
		return nil, err
	}
	rt, err := a.refresh.Issue(ctx, u.ID, ua, u.AuthVersion)
	if err != nil {
		return nil, err
	}
	return &LoginOutput{AccessToken: at, AccessExpiry: exp, RefreshToken: rt, User: u}, nil
}

func (a *Auth) LoginExternal(ctx context.Context, provider, subject, displayName, firstName, lastName string, publicationCount int, ua string) (*LoginOutput, error) {
	u, err := a.users.UpsertExternalIdentity(ctx, provider, subject, displayName, firstName, lastName, publicationCount)
	if err != nil {
		return nil, err
	}
	return a.issueLogin(ctx, u, ua)
}

// Refresh rotates the refresh token and reloads the user's role.
func (a *Auth) Refresh(ctx context.Context, oldRT, ua string) (*LoginOutput, error) {
	previous, lookupErr := a.refresh.lookup(ctx, tokenKey(oldRT))
	if lookupErr != nil && !errors.Is(lookupErr, ErrRefreshNotFound) {
		return nil, lookupErr
	}
	rt, uid, err := a.refresh.Rotate(ctx, oldRT, ua)
	if err != nil {
		if errors.Is(err, ErrRefreshNotFound) || errors.Is(err, ErrRefreshReplay) {
			return nil, fmt.Errorf("invalid refresh: %w", httputil.ErrUnauthorized)
		}
		return nil, err
	}
	u, err := a.users.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if previous.AuthVersion != u.AuthVersion {
		_ = a.refresh.Revoke(ctx, rt)
		return nil, fmt.Errorf("session revoked: %w", httputil.ErrUnauthorized)
	}
	if !u.IsActive {
		_ = a.refresh.RevokeAllForUser(ctx, uid)
		return nil, fmt.Errorf("account disabled: %w", httputil.ErrForbidden)
	}
	at, exp, err := a.jwt.Issue(u.ID.String(), jwtauth.Role(u.Role), u.Email, u.FullName(), u.EmailVerified, u.AuthVersion)
	if err != nil {
		return nil, err
	}
	return &LoginOutput{AccessToken: at, AccessExpiry: exp, RefreshToken: rt, User: u}, nil
}

func (a *Auth) Logout(ctx context.Context, rt string) error {
	return a.refresh.Revoke(ctx, rt)
}

func (a *Auth) Verify(ctx context.Context, tok uuid.UUID) error {
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	uid, err := a.users.UseVerificationTokenInTx(ctx, tx, tok)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("token invalid or expired: %w", httputil.ErrNotFound)
		}
		return err
	}
	if err := a.users.MarkEmailVerified(ctx, tx, uid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ForgotPassword hides whether the email exists.
func (a *Auth) ForgotPassword(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := a.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tok, err := a.users.CreatePasswordResetTokenInTx(ctx, tx, u.ID, resetTokenTTL)
	if err != nil {
		return err
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		"user.password_reset_requested", "user", u.ID.String(),
		map[string]any{
			"user_id":     u.ID,
			"user_email":  u.Email,
			"user_name":   u.FullName(),
			"reset_token": tok.String(),
		}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *Auth) ResetPassword(ctx context.Context, tok uuid.UUID, newPassword string) error {
	if err := validatePassword(ctx, a.checker, newPassword); err != nil {
		return err
	}
	hash, err := HashPasswordContext(ctx, newPassword)
	if err != nil {
		return err
	}
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	uid, err := a.users.UsePasswordResetTokenInTx(ctx, tx, tok)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("token invalid or expired: %w", httputil.ErrNotFound)
		}
		return err
	}
	if err := a.users.UpdatePasswordHash(ctx, tx, uid, hash); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = a.refresh.RevokeAllForUser(ctx, uid)
	return nil
}

type ProfileInput struct {
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	Patronymic  *string `json:"patronymic,omitempty"`
	Affiliation *string `json:"affiliation,omitempty"`
	Locale      string  `json:"locale"`
}

func (a *Auth) UpdateProfile(ctx context.Context, id uuid.UUID, in ProfileInput) (*domain.User, error) {
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	if in.Locale == "" {
		in.Locale = "ru"
	}
	var fields []httputil.FieldError
	if in.FirstName == "" || len([]rune(in.FirstName)) > 100 {
		fields = append(fields, httputil.FieldError{Field: "first_name", Message: "required, max 100 chars"})
	}
	if in.LastName == "" || len([]rune(in.LastName)) > 100 {
		fields = append(fields, httputil.FieldError{Field: "last_name", Message: "required, max 100 chars"})
	}
	if in.Locale != "ru" && in.Locale != "en" {
		fields = append(fields, httputil.FieldError{Field: "locale", Message: "must be ru or en"})
	}
	if len(fields) > 0 {
		return nil, &httputil.ValidationError{Fields: fields}
	}
	return a.users.UpdateProfile(ctx, id, repository.ProfileUpdate{
		FirstName: in.FirstName, LastName: in.LastName, Patronymic: in.Patronymic,
		Affiliation: in.Affiliation, Locale: in.Locale,
	})
}

func (a *Auth) ChangePassword(ctx context.Context, id uuid.UUID, currentPassword, newPassword string) error {
	u, err := a.users.GetByID(ctx, id)
	if err != nil {
		return err
	}
	ok, err := VerifyPasswordContext(ctx, currentPassword, u.PasswordHash)
	if err != nil || !ok {
		return fmt.Errorf("current password invalid: %w", httputil.ErrUnauthorized)
	}
	if err := validatePassword(ctx, a.checker, newPassword); err != nil {
		return err
	}
	hash, err := HashPasswordContext(ctx, newPassword)
	if err != nil {
		return err
	}
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := a.users.UpdatePasswordHash(ctx, tx, id, hash); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return a.refresh.RevokeAllForUser(ctx, id)
}

func (a *Auth) DeleteAccount(ctx context.Context, id uuid.UUID, currentPassword string) error {
	u, err := a.users.GetByID(ctx, id)
	if err != nil {
		return err
	}
	ok, err := VerifyPasswordContext(ctx, currentPassword, u.PasswordHash)
	if err != nil || !ok {
		return fmt.Errorf("current password invalid: %w", httputil.ErrUnauthorized)
	}
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := a.users.SoftDeleteInTx(ctx, tx, id); err != nil {
		return err
	}
	if err := outbox.Insert(ctx, tx, schemaName, sharedevents.SubjectUserDeleted, "user", id.String(), map[string]any{"user_id": id}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := a.refresh.BlockUser(ctx, id); err != nil {
		return err
	}
	return nil
}

func (a *Auth) AssignRole(ctx context.Context, target uuid.UUID, newRole domain.Role, changedBy uuid.UUID, preserveAdmin ...bool) error {
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if len(preserveAdmin) > 0 && preserveAdmin[0] && newRole == domain.RoleVerifiedOrganizer {
		var current string
		if err := tx.QueryRow(ctx, `SELECT role::text FROM identity_svc.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, target).Scan(&current); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httputil.ErrNotFound
			}
			return err
		}
		if current == string(domain.RoleAdmin) {
			return tx.Commit(ctx)
		}
	}
	old, err := a.users.UpdateRole(ctx, tx, target, newRole)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("user not found: %w", httputil.ErrNotFound)
		}
		return err
	}
	if old == newRole {
		return tx.Commit(ctx)
	}
	if err := outbox.Insert(ctx, tx, schemaName,
		sharedevents.SubjectUserRoleChanged, "user", target.String(),
		sharedevents.UserRoleChanged{
			UserID: target, OldRole: string(old), NewRole: string(newRole), ChangedBy: changedBy,
		}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *Auth) SetAccountActive(ctx context.Context, target uuid.UUID, active bool, changedBy uuid.UUID) error {
	user, err := a.users.GetByID(ctx, target)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("user not found: %w", httputil.ErrNotFound)
		}
		return err
	}
	tx, err := a.users.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := a.users.SetActiveInTx(ctx, tx, target, active); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("user not found: %w", httputil.ErrNotFound)
		}
		return err
	}
	if err := outbox.Insert(ctx, tx, schemaName, sharedevents.SubjectUserActiveChanged, "user", target.String(),
		sharedevents.UserActiveChanged{UserID: target, Active: active, Role: string(user.Role), ChangedBy: changedBy}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if active {
		return a.refresh.UnblockUser(ctx, target)
	}
	return a.refresh.BlockUser(ctx, target)
}
