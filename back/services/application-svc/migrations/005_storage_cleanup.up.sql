CREATE TABLE application_svc.storage_deletions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket varchar(255) NOT NULL,
    stored_name varchar(1000) NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','dead','completed')),
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (bucket,stored_name)
);
CREATE INDEX storage_deletions_pending ON application_svc.storage_deletions (next_attempt_at) WHERE status='pending';
