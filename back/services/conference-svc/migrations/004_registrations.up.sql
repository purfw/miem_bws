DO $$ BEGIN
    CREATE TYPE conference_svc.registration_status AS ENUM (
        'confirmed', 'waitlisted', 'cancelled'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS conference_svc.registrations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conference_id       uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    user_id             uuid NOT NULL,
    user_email          varchar(255) NOT NULL,
    user_name           varchar(255) NOT NULL,
    role_at_conf        varchar(30) NOT NULL DEFAULT 'attendee',
    status              conference_svc.registration_status NOT NULL,
    cancellation_count  smallint NOT NULL DEFAULT 0,
    registered_at       timestamptz NOT NULL DEFAULT now(),
    confirmed_at        timestamptz,
    waitlisted_at       timestamptz,
    cancelled_at        timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conference_id, user_id),
    CONSTRAINT chk_registration_role CHECK (role_at_conf IN ('attendee', 'speaker', 'reviewer')),
    CONSTRAINT chk_registration_cancellations CHECK (cancellation_count BETWEEN 0 AND 3)
);

CREATE INDEX IF NOT EXISTS idx_registrations_user
    ON conference_svc.registrations (user_id, registered_at DESC);
CREATE INDEX IF NOT EXISTS idx_registrations_waitlist
    ON conference_svc.registrations (conference_id, waitlisted_at, id)
    WHERE status = 'waitlisted';
CREATE INDEX IF NOT EXISTS idx_registrations_confirmed
    ON conference_svc.registrations (conference_id)
    WHERE status = 'confirmed';

CREATE TABLE IF NOT EXISTS conference_svc.registration_counters (
    conference_id   uuid PRIMARY KEY REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    confirmed_count int NOT NULL DEFAULT 0 CHECK (confirmed_count >= 0),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

INSERT INTO conference_svc.registration_counters (conference_id, confirmed_count)
SELECT id, 0 FROM conference_svc.conferences
ON CONFLICT (conference_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS conference_svc.registration_history (
    id              bigserial PRIMARY KEY,
    registration_id uuid NOT NULL REFERENCES conference_svc.registrations(id) ON DELETE CASCADE,
    old_status      conference_svc.registration_status,
    new_status      conference_svc.registration_status NOT NULL,
    reason          varchar(100) NOT NULL,
    occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_registration_history_registration
    ON conference_svc.registration_history (registration_id, occurred_at DESC);

CREATE OR REPLACE FUNCTION conference_svc.touch_registration_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_registration_touch ON conference_svc.registrations;
CREATE TRIGGER trg_registration_touch BEFORE UPDATE ON conference_svc.registrations
    FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_registration_updated_at();
