CREATE SCHEMA IF NOT EXISTS conference_svc;

DO $$ BEGIN
    CREATE TYPE conference_svc.conference_status AS ENUM (
        'draft',
        'registration_open',
        'registration_closed',
        'in_progress',
        'completed',
        'cancelled',
        'archived'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS conference_svc.conferences (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title                    varchar(500) NOT NULL,
    title_en                 varchar(500),
    slug                     varchar(200) NOT NULL,
    description              text,
    description_en           text,
    status                   conference_svc.conference_status NOT NULL DEFAULT 'draft',
    start_date               date NOT NULL,
    end_date                 date NOT NULL,
    registration_deadline    timestamptz,
    max_participants         int,
    location                 varchar(500),
    topics                   text[] NOT NULL DEFAULT '{}',
    -- created_by is copied from identity_svc.users; cross-schema FKs are forbidden.
    created_by               uuid NOT NULL,
    deleted_at               timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    search_vector            tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('russian', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(title_en, '')), 'A') ||
        setweight(to_tsvector('russian', coalesce(description, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(description_en, '')), 'B')
    ) STORED,
    CONSTRAINT chk_dates CHECK (end_date >= start_date)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_conferences_slug
    ON conference_svc.conferences (slug)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_conferences_status
    ON conference_svc.conferences (status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_conferences_start_date
    ON conference_svc.conferences (start_date DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_conferences_fts
    ON conference_svc.conferences USING GIN (search_vector);

CREATE INDEX IF NOT EXISTS idx_conferences_title_trgm
    ON conference_svc.conferences USING GIN (title gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_conferences_topics
    ON conference_svc.conferences USING GIN (topics);

CREATE TABLE IF NOT EXISTS conference_svc.sections (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conference_id   uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    title           varchar(500) NOT NULL,
    title_en        varchar(500),
    description     text,
    sort_order      int NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conference_id, sort_order)
);
CREATE INDEX IF NOT EXISTS idx_sections_conf ON conference_svc.sections (conference_id);

CREATE TABLE IF NOT EXISTS conference_svc.organizers (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conference_id   uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    name            varchar(255) NOT NULL,
    email           varchar(255),
    affiliation     varchar(255),
    role            varchar(100) NOT NULL DEFAULT 'organizer',
    user_id         uuid,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_organizers_conf ON conference_svc.organizers (conference_id);
CREATE INDEX IF NOT EXISTS idx_organizers_user ON conference_svc.organizers (user_id) WHERE user_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS conference_svc.partners (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conference_id   uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    name            varchar(255) NOT NULL,
    logo_url        varchar(1000),
    website_url     varchar(1000),
    tier            varchar(50),
    sort_order      int NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_partners_conf ON conference_svc.partners (conference_id);

CREATE TABLE IF NOT EXISTS conference_svc.schedule_slots (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conference_id   uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    section_id      uuid REFERENCES conference_svc.sections(id) ON DELETE SET NULL,
    title           varchar(500) NOT NULL,
    speaker_name    varchar(255),
    speaker_affiliation varchar(255),
    room            varchar(100),
    slot_date       date NOT NULL,
    start_time      time NOT NULL,
    end_time        time NOT NULL,
    description     text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_time CHECK (end_time > start_time)
);
CREATE INDEX IF NOT EXISTS idx_schedule_conf_date
    ON conference_svc.schedule_slots (conference_id, slot_date, start_time);

CREATE TABLE IF NOT EXISTS conference_svc.outbox_events (
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
CREATE INDEX IF NOT EXISTS idx_conf_outbox_pending
    ON conference_svc.outbox_events (created_at)
    WHERE status = 'pending';

CREATE OR REPLACE FUNCTION conference_svc.touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_conf_touch ON conference_svc.conferences;
CREATE TRIGGER trg_conf_touch BEFORE UPDATE ON conference_svc.conferences
    FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_updated_at();
