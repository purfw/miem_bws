ALTER TABLE identity_svc.users
    ALTER COLUMN role SET DEFAULT 'user'::identity_svc.user_role,
    ADD COLUMN IF NOT EXISTS locale varchar(5) NOT NULL DEFAULT 'ru';

UPDATE identity_svc.users SET role = 'user' WHERE role::text = 'student';
UPDATE identity_svc.users SET role = 'admin' WHERE role::text = 'moderator';

DO $$ BEGIN
    ALTER TABLE identity_svc.users
        ADD CONSTRAINT chk_users_locale CHECK (locale IN ('ru', 'en'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE INDEX IF NOT EXISTS idx_evt_user_created
    ON identity_svc.email_verification_tokens (user_id, created_at DESC);
