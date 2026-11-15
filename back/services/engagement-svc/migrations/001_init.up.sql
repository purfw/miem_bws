-- Monthly audit partitions reject UPDATE and DELETE.

CREATE SCHEMA IF NOT EXISTS engagement_svc;

-- Idempotency for the NATS consumer.
CREATE TABLE IF NOT EXISTS engagement_svc.processed_events (
    event_id     uuid PRIMARY KEY,
    event_type   varchar(100) NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS engagement_svc.notifications (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid,
    channel       varchar(20) NOT NULL DEFAULT 'email',
    type          varchar(100) NOT NULL,
    recipient     varchar(255) NOT NULL,
    subject       varchar(500),
    body          text,
    status        varchar(20) NOT NULL DEFAULT 'pending',
    attempts      smallint NOT NULL DEFAULT 0,
    last_error    text,
    sent_at       timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notif_user ON engagement_svc.notifications (user_id);
CREATE INDEX IF NOT EXISTS idx_notif_status ON engagement_svc.notifications (status, created_at)
    WHERE status IN ('pending', 'failed');

CREATE TABLE IF NOT EXISTS engagement_svc.audit_log (
    id            bigserial,
    event_id      uuid NOT NULL,
    event_type    varchar(100) NOT NULL,
    actor_id      uuid,
    entity_type   varchar(50),
    entity_id     uuid,
    payload       jsonb NOT NULL,
    ip_address    inet,
    occurred_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);

CREATE UNIQUE INDEX IF NOT EXISTS uq_audit_event_id
    ON engagement_svc.audit_log (event_id, occurred_at);

-- The default partition keeps writes working if partition creation is delayed.
DO $$
DECLARE
    cur date := date_trunc('month', now())::date;
    prv date := (cur - interval '1 month')::date;
    nxt date := (cur + interval '1 month')::date;
    nx2 date := (cur + interval '2 month')::date;
BEGIN
    EXECUTE format($f$ CREATE TABLE IF NOT EXISTS engagement_svc.audit_log_%s
        PARTITION OF engagement_svc.audit_log
        FOR VALUES FROM ('%s') TO ('%s') $f$,
        to_char(prv, 'YYYY_MM'), prv, cur);
    EXECUTE format($f$ CREATE TABLE IF NOT EXISTS engagement_svc.audit_log_%s
        PARTITION OF engagement_svc.audit_log
        FOR VALUES FROM ('%s') TO ('%s') $f$,
        to_char(cur, 'YYYY_MM'), cur, nxt);
    EXECUTE format($f$ CREATE TABLE IF NOT EXISTS engagement_svc.audit_log_%s
        PARTITION OF engagement_svc.audit_log
        FOR VALUES FROM ('%s') TO ('%s') $f$,
        to_char(nxt, 'YYYY_MM'), nxt, nx2);
END $$;

CREATE TABLE IF NOT EXISTS engagement_svc.audit_log_default
    PARTITION OF engagement_svc.audit_log DEFAULT;

CREATE OR REPLACE RULE no_audit_update AS
    ON UPDATE TO engagement_svc.audit_log DO INSTEAD NOTHING;
CREATE OR REPLACE RULE no_audit_delete AS
    ON DELETE TO engagement_svc.audit_log DO INSTEAD NOTHING;
