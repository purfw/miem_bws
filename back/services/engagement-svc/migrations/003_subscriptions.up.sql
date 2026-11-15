CREATE TABLE IF NOT EXISTS engagement_svc.subscriptions (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                uuid NOT NULL,
    user_email             varchar(255) NOT NULL,
    conference_id          uuid NOT NULL,
    notify_on              text[] NOT NULL,
    channels               text[] NOT NULL,
    unsubscribe_token_hash bytea NOT NULL UNIQUE,
    active                 boolean NOT NULL DEFAULT true,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_subscription_user_conference UNIQUE (user_id, conference_id),
    CONSTRAINT chk_subscription_notify_on CHECK (cardinality(notify_on) > 0),
    CONSTRAINT chk_subscription_channels CHECK (cardinality(channels) > 0)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_conference_active
    ON engagement_svc.subscriptions (conference_id) WHERE active;

ALTER TABLE engagement_svc.notifications
    ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS locked_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_notifications_retry
    ON engagement_svc.notifications (next_attempt_at)
    WHERE status IN ('pending','failed','sending');
