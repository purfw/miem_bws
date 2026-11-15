CREATE SCHEMA IF NOT EXISTS identity_svc;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$ BEGIN
    CREATE TYPE identity_svc.user_role AS ENUM ('student', 'moderator', 'admin');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS identity_svc.users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           varchar(255) NOT NULL,
    password_hash   varchar(255) NOT NULL,
    first_name      varchar(100) NOT NULL,
    last_name       varchar(100) NOT NULL,
    patronymic      varchar(100),
    affiliation     varchar(255),
    role            identity_svc.user_role NOT NULL DEFAULT 'student',
    is_active       boolean NOT NULL DEFAULT true,
    email_verified  boolean NOT NULL DEFAULT false,
    deleted_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower
    ON identity_svc.users (lower(email))
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_role
    ON identity_svc.users (role)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_svc.email_verification_tokens (
    token       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES identity_svc.users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    used        boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_evt_user ON identity_svc.email_verification_tokens(user_id);

CREATE TABLE IF NOT EXISTS identity_svc.password_reset_tokens (
    token       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES identity_svc.users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    used        boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_prt_user ON identity_svc.password_reset_tokens(user_id);

CREATE TABLE IF NOT EXISTS identity_svc.outbox_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      varchar(100) NOT NULL,
    aggregate_type  varchar(50),
    aggregate_id    uuid,
    payload         jsonb NOT NULL,
    status          varchar(20) NOT NULL DEFAULT 'pending',
    retry_count     int NOT NULL DEFAULT 0,
    next_retry_at   timestamptz,
    last_error      text,
    published_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_outbox_pending
    ON identity_svc.outbox_events (created_at)
    WHERE status = 'pending';

CREATE OR REPLACE FUNCTION identity_svc.touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_users_touch ON identity_svc.users;
CREATE TRIGGER trg_users_touch BEFORE UPDATE ON identity_svc.users
    FOR EACH ROW EXECUTE FUNCTION identity_svc.touch_updated_at();
