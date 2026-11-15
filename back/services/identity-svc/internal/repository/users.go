package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/confhub/shared/httputil"
	"github.com/confhub/shared/jwtauth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/confhub/identity-svc/internal/domain"
)

var ErrDuplicate = errors.New("duplicate")
var ErrNotFound = errors.New("not found")

type ResearchIdentity struct {
	Provider         string
	Subject          string
	DisplayName      string
	PublicationCount int
}

type Users struct {
	pool *pgxpool.Pool
}

func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool: pool} }

func (u *Users) Pool() *pgxpool.Pool { return u.pool }

func (u *Users) ListForAdministration(ctx context.Context, query string, limit, offset int) ([]domain.User, bool, error) {
	rows, err := u.pool.Query(ctx, `SELECT id,email,first_name,last_name,role::text,is_active,email_verified,created_at
		FROM identity_svc.users WHERE deleted_at IS NULL
		AND ($1='' OR strpos(lower(email),lower($1))>0 OR strpos(lower(first_name || ' ' || last_name),lower($1))>0)
		ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, strings.TrimSpace(query), limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	users := make([]domain.User, 0, limit)
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(&user.ID, &user.Email, &user.FirstName, &user.LastName, &user.Role, &user.IsActive, &user.EmailVerified, &user.CreatedAt); err != nil {
			return nil, false, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(users) > limit
	if more {
		users = users[:limit]
	}
	return users, more, nil
}

func (u *Users) AuthState(ctx context.Context, userID string) (jwtauth.AuthState, error) {
	var state jwtauth.AuthState
	err := u.pool.QueryRow(ctx, `SELECT auth_version,is_active FROM identity_svc.users WHERE id=$1 AND deleted_at IS NULL`, userID).Scan(&state.Version, &state.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, httputil.ErrUnauthorized
	}
	return state, err
}

func (u *Users) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	const q = `
		SELECT id, email, password_hash, first_name, last_name, patronymic, affiliation,
		       role::text, is_active, email_verified, locale, created_at, updated_at, auth_version
          FROM identity_svc.users
         WHERE lower(email) = lower($1)
           AND deleted_at IS NULL`
	var usr domain.User
	var role string
	err := u.pool.QueryRow(ctx, q, strings.TrimSpace(email)).
		Scan(&usr.ID, &usr.Email, &usr.PasswordHash, &usr.FirstName, &usr.LastName,
			&usr.Patronymic, &usr.Affiliation, &role, &usr.IsActive, &usr.EmailVerified, &usr.Locale,
			&usr.CreatedAt, &usr.UpdatedAt, &usr.AuthVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	usr.Role = domain.Role(role)
	return &usr, nil
}

func (u *Users) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	const q = `
		SELECT id, email, password_hash, first_name, last_name, patronymic, affiliation,
		       role::text, is_active, email_verified, locale, created_at, updated_at, auth_version
          FROM identity_svc.users
         WHERE id = $1 AND deleted_at IS NULL`
	var usr domain.User
	var role string
	err := u.pool.QueryRow(ctx, q, id).
		Scan(&usr.ID, &usr.Email, &usr.PasswordHash, &usr.FirstName, &usr.LastName,
			&usr.Patronymic, &usr.Affiliation, &role, &usr.IsActive, &usr.EmailVerified, &usr.Locale,
			&usr.CreatedAt, &usr.UpdatedAt, &usr.AuthVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	usr.Role = domain.Role(role)
	return &usr, nil
}

func (u *Users) CreateInTx(ctx context.Context, tx pgx.Tx, in *domain.User) error {
	const q = `
        INSERT INTO identity_svc.users
            (id, email, password_hash, first_name, last_name, patronymic, affiliation, role, locale)
        VALUES ($1, lower($2), $3, $4, $5, $6, $7, $8::identity_svc.user_role, $9)
        RETURNING created_at, updated_at`
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	err := tx.QueryRow(ctx, q,
		in.ID, in.Email, in.PasswordHash, in.FirstName, in.LastName, in.Patronymic, in.Affiliation, string(in.Role), in.Locale,
	).Scan(&in.CreatedAt, &in.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// UpsertExternalIdentity serializes callbacks for the same provider subject.
func (u *Users) UpsertExternalIdentity(ctx context.Context, provider, subject, displayName, firstName, lastName string, publicationCount int) (*domain.User, error) {
	tx, err := u.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	lockKey := provider + ":" + subject
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return nil, err
	}
	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM identity_svc.external_identities
		WHERE provider=$1 AND subject=$2`, provider, subject).Scan(&userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if strings.TrimSpace(firstName) == "" {
			firstName = "ORCID"
		}
		if strings.TrimSpace(lastName) == "" {
			lastName = "Researcher"
		}
		emailSubject := strings.ToLower(strings.ReplaceAll(subject, "-", ""))
		usr := &domain.User{
			Email: "orcid+" + emailSubject + "@identity.confhub.invalid",
			// External-only accounts cannot authenticate through the password path.
			PasswordHash: "external-only", FirstName: firstName, LastName: lastName,
			Role: domain.RoleUser, EmailVerified: true, IsActive: true, Locale: "ru",
		}
		if err := u.CreateInTx(ctx, tx, usr); err != nil {
			return nil, err
		}
		if err := u.MarkEmailVerified(ctx, tx, usr.ID); err != nil {
			return nil, err
		}
		userID = usr.ID
		if _, err := tx.Exec(ctx, `INSERT INTO identity_svc.external_identities
			(provider,subject,user_id,display_name,publication_count)
			VALUES ($1,$2,$3,$4,GREATEST($5,0))`, provider, subject, userID, displayName, publicationCount); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE identity_svc.external_identities
			SET display_name=$3,publication_count=GREATEST($4,0),last_synced_at=now()
			WHERE provider=$1 AND subject=$2`, provider, subject, displayName, publicationCount); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return u.GetByID(ctx, userID)
}

func (u *Users) ResearchIdentityByUser(ctx context.Context, userID uuid.UUID) (*ResearchIdentity, error) {
	var out ResearchIdentity
	err := u.pool.QueryRow(ctx, `SELECT provider,subject,COALESCE(display_name,''),publication_count
		FROM identity_svc.external_identities WHERE user_id=$1 ORDER BY linked_at LIMIT 1`, userID).Scan(
		&out.Provider, &out.Subject, &out.DisplayName, &out.PublicationCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &out, err
}

func (u *Users) UpdateRole(ctx context.Context, tx pgx.Tx, id uuid.UUID, role domain.Role) (oldRole domain.Role, err error) {
	const q = `
        UPDATE identity_svc.users
           SET role = $1::identity_svc.user_role, auth_version=auth_version+CASE WHEN role=$1::identity_svc.user_role THEN 0 ELSE 1 END
         WHERE id = $2 AND deleted_at IS NULL
        `
	var oldStr string
	if err := tx.QueryRow(ctx, `SELECT role::text FROM identity_svc.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&oldStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if _, err := tx.Exec(ctx, q, string(role), id); err != nil {
		return "", err
	}
	return domain.Role(oldStr), nil
}

func (u *Users) SetActiveInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, active bool) error {
	tag, err := tx.Exec(ctx, `UPDATE identity_svc.users SET is_active=$2,auth_version=auth_version+CASE WHEN is_active=$2 THEN 0 ELSE 1 END
		WHERE id=$1 AND deleted_at IS NULL`, id, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (u *Users) MarkEmailVerified(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE identity_svc.users SET email_verified=true WHERE id=$1`, id)
	return err
}

func (u *Users) UpdatePasswordHash(ctx context.Context, tx pgx.Tx, id uuid.UUID, hash string) error {
	_, err := tx.Exec(ctx, `UPDATE identity_svc.users SET password_hash=$1,auth_version=auth_version+1 WHERE id=$2`, hash, id)
	return err
}

func (u *Users) CreateVerificationTokenInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ttl time.Duration) (uuid.UUID, error) {
	tok := uuid.New()
	_, err := tx.Exec(ctx, `
        INSERT INTO identity_svc.email_verification_tokens (token, user_id, expires_at)
        VALUES ($1, $2, now() + $3::interval)`,
		tok, userID, ttl.String())
	return tok, err
}

func (u *Users) VerificationWindowInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (int, *time.Time, *time.Time, error) {
	var count int
	var oldest, latest *time.Time
	err := tx.QueryRow(ctx, `
		SELECT count(*),min(created_at),max(created_at)
		  FROM identity_svc.email_verification_tokens
		 WHERE user_id=$1 AND created_at > now() - interval '1 hour'`,
		userID).Scan(&count, &oldest, &latest)
	return count, oldest, latest, err
}

type ProfileUpdate struct {
	FirstName   string
	LastName    string
	Patronymic  *string
	Affiliation *string
	Locale      string
}

func (u *Users) UpdateProfile(ctx context.Context, id uuid.UUID, in ProfileUpdate) (*domain.User, error) {
	const q = `
		UPDATE identity_svc.users
		   SET first_name=$2, last_name=$3, patronymic=$4, affiliation=$5, locale=$6
		 WHERE id=$1 AND deleted_at IS NULL AND is_active=true
		 RETURNING id, email, password_hash, first_name, last_name, patronymic,
		           affiliation, role::text, is_active, email_verified, locale,
		           created_at, updated_at, auth_version`
	var usr domain.User
	var role string
	err := u.pool.QueryRow(ctx, q, id, in.FirstName, in.LastName, in.Patronymic, in.Affiliation, in.Locale).Scan(
		&usr.ID, &usr.Email, &usr.PasswordHash, &usr.FirstName, &usr.LastName,
		&usr.Patronymic, &usr.Affiliation, &role, &usr.IsActive, &usr.EmailVerified,
		&usr.Locale, &usr.CreatedAt, &usr.UpdatedAt, &usr.AuthVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	usr.Role = domain.Role(role)
	return &usr, nil
}

// SoftDelete anonymizes the profile while preserving existing references.
func (u *Users) SoftDeleteInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE identity_svc.users
		   SET email = concat('deleted+', id::text, '@invalid.local'),
		       first_name = 'Удалённый', last_name = 'пользователь',
		       patronymic = NULL, affiliation = NULL,
		       password_hash = 'deleted', is_active = false, deleted_at = now()
		 WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (u *Users) UseVerificationTokenInTx(ctx context.Context, tx pgx.Tx, tok uuid.UUID) (uuid.UUID, error) {
	var uid uuid.UUID
	err := tx.QueryRow(ctx, `
        UPDATE identity_svc.email_verification_tokens
           SET used = true
         WHERE token = $1 AND used = false AND expires_at > now()
        RETURNING user_id`, tok).Scan(&uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return uid, nil
}

func (u *Users) CreatePasswordResetTokenInTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, ttl time.Duration) (uuid.UUID, error) {
	tok := uuid.New()
	_, err := tx.Exec(ctx, `
        INSERT INTO identity_svc.password_reset_tokens (token, user_id, expires_at)
        VALUES ($1, $2, now() + $3::interval)`,
		tok, userID, ttl.String())
	return tok, err
}

func (u *Users) UsePasswordResetTokenInTx(ctx context.Context, tx pgx.Tx, tok uuid.UUID) (uuid.UUID, error) {
	var uid uuid.UUID
	err := tx.QueryRow(ctx, `
        UPDATE identity_svc.password_reset_tokens
           SET used = true
         WHERE token = $1 AND used = false AND expires_at > now()
        RETURNING user_id`, tok).Scan(&uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return uid, nil
}
