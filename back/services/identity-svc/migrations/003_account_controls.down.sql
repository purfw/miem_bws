ALTER TABLE identity_svc.users ALTER COLUMN role SET DEFAULT 'student'::identity_svc.user_role;
ALTER TABLE identity_svc.users DROP CONSTRAINT IF EXISTS chk_users_locale;
ALTER TABLE identity_svc.users DROP COLUMN IF EXISTS locale;
DROP INDEX IF EXISTS identity_svc.idx_evt_user_created;
