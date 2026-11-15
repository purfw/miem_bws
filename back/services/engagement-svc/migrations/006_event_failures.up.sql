CREATE TABLE engagement_svc.event_failures (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer text NOT NULL,
    stream_sequence bigint NOT NULL CHECK (stream_sequence > 0),
    subject text NOT NULL,
    payload bytea NOT NULL,
    status text NOT NULL CHECK (status IN ('pending','dead','completed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (consumer,stream_sequence)
);
CREATE INDEX event_failures_pending ON engagement_svc.event_failures(next_attempt_at) WHERE status='pending';
