DROP TABLE IF EXISTS engagement_svc.subscription_deliveries;
ALTER TABLE engagement_svc.subscriptions DROP COLUMN IF EXISTS unsubscribe_token;
ALTER TABLE engagement_svc.notifications
    DROP COLUMN IF EXISTS subscription_id,
    DROP COLUMN IF EXISTS dedup_key,
    DROP COLUMN IF EXISTS message;
