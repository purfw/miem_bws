-- conference_id and user_id are copied UUIDs without cross-schema FKs.
-- Cascade-on-cancel is handled via NATS compensation events.

CREATE SCHEMA IF NOT EXISTS application_svc;

DO $$ BEGIN
    CREATE TYPE application_svc.participation_form AS ENUM (
        'speaker',
        'poster',
        'listener'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE application_svc.application_status AS ENUM (
        'submitted',
        'under_review',
        'revision_requested',
        'accepted',
        'rejected',
        'withdrawn'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE application_svc.review_recommendation AS ENUM (
        'accept',
        'minor_revision',
        'major_revision',
        'reject'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS application_svc.applications (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conference_id       uuid NOT NULL,
    section_id          uuid,
    user_id             uuid NOT NULL,
    -- Snapshots so listings don't need to talk to other svcs.
    cached_user_email   varchar(255) NOT NULL,
    cached_user_name    varchar(255) NOT NULL,
    cached_conf_title   varchar(500) NOT NULL,
    form                application_svc.participation_form NOT NULL,
    title               varchar(500) NOT NULL,
    abstract            text,
    -- co_authors: [{name, email, affiliation}]
    co_authors          jsonb NOT NULL DEFAULT '[]'::jsonb,
    keywords            text[] NOT NULL DEFAULT '{}',
    status              application_svc.application_status NOT NULL DEFAULT 'submitted',
    status_changed_at   timestamptz NOT NULL DEFAULT now(),
    status_changed_by   uuid,
    moderator_comment   text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- Partial unique so a user can re-apply after a rejected/withdrawn attempt.
CREATE UNIQUE INDEX IF NOT EXISTS uq_app_inflight
    ON application_svc.applications (conference_id, user_id, form)
    WHERE status IN ('submitted', 'under_review', 'revision_requested', 'accepted');

CREATE INDEX IF NOT EXISTS idx_applications_user
    ON application_svc.applications (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_applications_conf
    ON application_svc.applications (conference_id, status);
CREATE INDEX IF NOT EXISTS idx_applications_pipeline
    ON application_svc.applications (conference_id, status, status_changed_at)
    WHERE status IN ('submitted', 'under_review', 'revision_requested');

CREATE TABLE IF NOT EXISTS application_svc.application_files (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  uuid NOT NULL REFERENCES application_svc.applications(id) ON DELETE CASCADE,
    original_name   varchar(500) NOT NULL,
    stored_name     varchar(500) NOT NULL,
    mime_type       varchar(100) NOT NULL,
    size_bytes      bigint NOT NULL,
    bucket          varchar(100) NOT NULL,
    uploaded_by     uuid NOT NULL,
    uploaded_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_size CHECK (size_bytes > 0 AND size_bytes <= 21 * 1024 * 1024)
);
CREATE INDEX IF NOT EXISTS idx_files_app ON application_svc.application_files (application_id);

CREATE TABLE IF NOT EXISTS application_svc.reviews (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id          uuid NOT NULL REFERENCES application_svc.applications(id) ON DELETE CASCADE,
    reviewer_id             uuid NOT NULL,
    cached_reviewer_name    varchar(255) NOT NULL,
    score                   smallint NOT NULL CHECK (score BETWEEN 1 AND 10),
    summary                 text NOT NULL,
    strengths               text,
    weaknesses              text,
    recommendation          application_svc.review_recommendation NOT NULL,
    is_visible_to_author    boolean NOT NULL DEFAULT false,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, reviewer_id)
);
CREATE INDEX IF NOT EXISTS idx_reviews_app ON application_svc.reviews (application_id);

CREATE TABLE IF NOT EXISTS application_svc.outbox_events (
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
CREATE INDEX IF NOT EXISTS idx_app_outbox_pending
    ON application_svc.outbox_events (created_at)
    WHERE status = 'pending';

CREATE OR REPLACE FUNCTION application_svc.touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_app_touch ON application_svc.applications;
CREATE TRIGGER trg_app_touch BEFORE UPDATE ON application_svc.applications
    FOR EACH ROW EXECUTE FUNCTION application_svc.touch_updated_at();

DROP TRIGGER IF EXISTS trg_review_touch ON application_svc.reviews;
CREATE TRIGGER trg_review_touch BEFORE UPDATE ON application_svc.reviews
    FOR EACH ROW EXECUTE FUNCTION application_svc.touch_updated_at();
