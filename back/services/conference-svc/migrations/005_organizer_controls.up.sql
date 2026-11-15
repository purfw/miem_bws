ALTER TABLE conference_svc.conferences
    ADD COLUMN IF NOT EXISTS publication_allowed boolean NOT NULL DEFAULT false;

-- Existing records were created by the trusted deployment seed/internal API.
UPDATE conference_svc.conferences SET publication_allowed=true;

CREATE TABLE IF NOT EXISTS conference_svc.conference_members (
    conference_id uuid NOT NULL REFERENCES conference_svc.conferences(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL,
    permissions   text[] NOT NULL DEFAULT ARRAY['edit','schedule','participants'],
    added_by      uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conference_id,user_id)
);
CREATE INDEX IF NOT EXISTS idx_conference_members_user
    ON conference_svc.conference_members (user_id,conference_id);

ALTER TABLE conference_svc.schedule_slots
    ADD COLUMN IF NOT EXISTS session_type varchar(50) NOT NULL DEFAULT 'talk',
    ADD COLUMN IF NOT EXISTS join_url_ciphertext bytea,
    ADD COLUMN IF NOT EXISTS join_url_nonce bytea,
    ADD COLUMN IF NOT EXISTS join_url_key_version smallint,
    ADD COLUMN IF NOT EXISTS join_url_public_at timestamptz;

CREATE TABLE IF NOT EXISTS conference_svc.join_link_access_log (
    id          bigserial PRIMARY KEY,
    slot_id     uuid NOT NULL REFERENCES conference_svc.schedule_slots(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL,
    accessed_at timestamptz NOT NULL DEFAULT now(),
    request_id  varchar(100),
    ip_hash     varchar(64)
);
CREATE INDEX IF NOT EXISTS idx_join_access_slot_time
    ON conference_svc.join_link_access_log (slot_id,accessed_at DESC);
