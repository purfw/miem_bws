
ALTER TABLE conference_svc.organizers
    ADD COLUMN IF NOT EXISTS kind       varchar(20)  NOT NULL DEFAULT 'organizer',
    ADD COLUMN IF NOT EXISTS job_title  varchar(255),
    ADD COLUMN IF NOT EXISTS photo_url  varchar(1000),
    ADD COLUMN IF NOT EXISTS sort_order int          NOT NULL DEFAULT 0;

DO $$ BEGIN
    ALTER TABLE conference_svc.organizers
        ADD CONSTRAINT chk_organizer_kind CHECK (kind IN ('organizer', 'committee'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE INDEX IF NOT EXISTS idx_organizers_conf_kind
    ON conference_svc.organizers (conference_id, kind, sort_order);

ALTER TABLE conference_svc.sections
    ADD COLUMN IF NOT EXISTS moderator_name        varchar(255),
    ADD COLUMN IF NOT EXISTS moderator_affiliation varchar(255),
    ADD COLUMN IF NOT EXISTS moderator_user_id     uuid;

CREATE INDEX IF NOT EXISTS idx_schedule_section
    ON conference_svc.schedule_slots (section_id)
    WHERE section_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS conference_svc.favorites (
    user_id       uuid NOT NULL,
    conference_id uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, conference_id)
);

CREATE INDEX IF NOT EXISTS idx_favorites_conf
    ON conference_svc.favorites (conference_id);

CREATE INDEX IF NOT EXISTS idx_favorites_user_created
    ON conference_svc.favorites (user_id, created_at DESC);
