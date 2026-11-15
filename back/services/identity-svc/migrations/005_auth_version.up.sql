ALTER TABLE identity_svc.users ADD COLUMN IF NOT EXISTS auth_version bigint NOT NULL DEFAULT 0;
