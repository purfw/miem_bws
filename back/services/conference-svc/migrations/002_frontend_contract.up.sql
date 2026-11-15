
DO $$ BEGIN
    CREATE TYPE conference_svc.conference_format AS ENUM (
        'offline',
        'hybrid',
        'online'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

ALTER TABLE conference_svc.conferences
    ADD COLUMN IF NOT EXISTS format          conference_svc.conference_format NOT NULL DEFAULT 'offline',
    ADD COLUMN IF NOT EXISTS city            varchar(200),
    ADD COLUMN IF NOT EXISTS cover_image_url varchar(1000),
    ADD COLUMN IF NOT EXISTS is_featured     boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN conference_svc.conferences.location IS 'Адрес площадки внутри города (primary_venue.address)';
COMMENT ON COLUMN conference_svc.conferences.city     IS 'Город проведения; NULL для формата online';

CREATE INDEX IF NOT EXISTS idx_conferences_format
    ON conference_svc.conferences (format)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_conferences_city
    ON conference_svc.conferences (city)
    WHERE deleted_at IS NULL AND city IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_conferences_featured
    ON conference_svc.conferences ((is_featured))
    WHERE is_featured AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS conference_svc.conference_counters (
    conference_id    uuid PRIMARY KEY REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    participants     int NOT NULL DEFAULT 0,
    accepted         int NOT NULL DEFAULT 0,
    rejected         int NOT NULL DEFAULT 0,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_counters_nonneg CHECK (participants >= 0 AND accepted >= 0 AND rejected >= 0)
);

CREATE TABLE IF NOT EXISTS conference_svc.processed_events (
    event_id     uuid PRIMARY KEY,
    event_type   varchar(100) NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_conf_processed_at
    ON conference_svc.processed_events (processed_at);
