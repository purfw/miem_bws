ALTER TABLE engagement_svc.subscriptions ALTER COLUMN conference_id DROP NOT NULL;
ALTER TABLE engagement_svc.subscriptions ADD COLUMN criteria jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE engagement_svc.subscriptions ADD COLUMN criteria_hash varchar(64);
ALTER TABLE engagement_svc.subscriptions ADD CONSTRAINT subscription_target CHECK (
    (conference_id IS NOT NULL AND criteria_hash IS NULL) OR
    (conference_id IS NULL AND criteria_hash IS NOT NULL AND criteria <> '{}'::jsonb)
);
CREATE UNIQUE INDEX subscription_criteria_unique ON engagement_svc.subscriptions (user_id,criteria_hash) WHERE conference_id IS NULL;
