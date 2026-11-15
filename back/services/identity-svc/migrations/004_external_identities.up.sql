CREATE TABLE IF NOT EXISTS identity_svc.external_identities (
    provider          varchar(50) NOT NULL,
    subject           varchar(255) NOT NULL,
    user_id           uuid NOT NULL REFERENCES identity_svc.users(id) ON DELETE CASCADE,
    display_name      varchar(255),
    publication_count integer NOT NULL DEFAULT 0 CHECK (publication_count >= 0),
    linked_at         timestamptz NOT NULL DEFAULT now(),
    last_synced_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject),
    UNIQUE (provider, user_id)
);

CREATE INDEX IF NOT EXISTS idx_external_identities_user
    ON identity_svc.external_identities (user_id);
