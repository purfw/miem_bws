CREATE TABLE IF NOT EXISTS application_svc.participation_documents (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    registration_id     uuid NOT NULL UNIQUE,
    user_id             uuid NOT NULL,
    conference_id       uuid NOT NULL,
    conference_title    varchar(500) NOT NULL,
    participant_name    varchar(255) NOT NULL,
    stored_name         varchar(1000) NOT NULL,
    bucket              varchar(255) NOT NULL,
    verification_hash   bytea NOT NULL UNIQUE,
    download_token_hash bytea NOT NULL UNIQUE,
    download_expires_at timestamptz NOT NULL,
    issued_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_participation_documents_user
    ON application_svc.participation_documents (user_id,issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_participation_documents_conference
    ON application_svc.participation_documents (conference_id);
