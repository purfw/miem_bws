ALTER TABLE engagement_svc.notifications
    ADD COLUMN IF NOT EXISTS message bytea,
    ADD COLUMN IF NOT EXISTS dedup_key text UNIQUE,
    ADD COLUMN IF NOT EXISTS subscription_id uuid;

ALTER TABLE engagement_svc.subscriptions
    ADD COLUMN IF NOT EXISTS unsubscribe_token text;

CREATE TABLE IF NOT EXISTS engagement_svc.subscription_deliveries (
    subscription_id uuid NOT NULL,
    criterion varchar(50) NOT NULL,
    occurrence text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subscription_id, criterion, occurrence)
);
