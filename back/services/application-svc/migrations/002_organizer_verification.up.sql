DO $$ BEGIN
    CREATE TYPE application_svc.verification_status AS ENUM ('pending','approved','rejected');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS application_svc.trusted_domains (
    domain      varchar(255) PRIMARY KEY,
    organization varchar(500) NOT NULL,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO application_svc.trusted_domains (domain, organization) VALUES
    ('hse.ru', 'Национальный исследовательский университет «Высшая школа экономики»'),
    ('miem.hse.ru', 'МИЭМ НИУ ВШЭ')
ON CONFLICT (domain) DO NOTHING;

CREATE TABLE IF NOT EXISTS application_svc.organizer_verifications (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL,
    user_email      varchar(255) NOT NULL,
    user_name       varchar(255) NOT NULL,
    organization    varchar(500) NOT NULL,
    method          varchar(30) NOT NULL DEFAULT 'documents',
    status          application_svc.verification_status NOT NULL DEFAULT 'pending',
    rejection_reason text,
    reviewed_by     uuid,
    reviewed_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_verification_method CHECK (method IN ('documents','trusted_domain','orcid'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_active_organizer_verification
    ON application_svc.organizer_verifications (user_id)
    WHERE status='pending';
CREATE INDEX IF NOT EXISTS idx_verification_queue
    ON application_svc.organizer_verifications (status, created_at);

CREATE TABLE IF NOT EXISTS application_svc.verification_documents (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    verification_id uuid NOT NULL REFERENCES application_svc.organizer_verifications(id) ON DELETE CASCADE,
    original_name   varchar(500) NOT NULL,
    stored_name     varchar(1000) NOT NULL,
    mime_type       varchar(255) NOT NULL,
    size_bytes      bigint NOT NULL,
    bucket          varchar(255) NOT NULL,
    uploaded_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_verification_documents_verification
    ON application_svc.verification_documents (verification_id, uploaded_at);
