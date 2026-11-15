DROP TABLE IF EXISTS conference_svc.join_link_access_log;
ALTER TABLE conference_svc.schedule_slots
    DROP COLUMN IF EXISTS join_url_public_at,
    DROP COLUMN IF EXISTS join_url_key_version,
    DROP COLUMN IF EXISTS join_url_nonce,
    DROP COLUMN IF EXISTS join_url_ciphertext,
    DROP COLUMN IF EXISTS session_type;
DROP TABLE IF EXISTS conference_svc.conference_members;
ALTER TABLE conference_svc.conferences DROP COLUMN IF EXISTS publication_allowed;
