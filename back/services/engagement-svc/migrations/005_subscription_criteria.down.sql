DELETE FROM engagement_svc.subscription_deliveries WHERE subscription_id IN (SELECT id FROM engagement_svc.subscriptions WHERE conference_id IS NULL);
UPDATE engagement_svc.notifications SET subscription_id=NULL WHERE subscription_id IN (SELECT id FROM engagement_svc.subscriptions WHERE conference_id IS NULL);
DELETE FROM engagement_svc.subscriptions WHERE conference_id IS NULL;
DROP INDEX IF EXISTS engagement_svc.subscription_criteria_unique;
ALTER TABLE engagement_svc.subscriptions DROP CONSTRAINT subscription_target;
ALTER TABLE engagement_svc.subscriptions DROP COLUMN criteria, DROP COLUMN criteria_hash;
ALTER TABLE engagement_svc.subscriptions ALTER COLUMN conference_id SET NOT NULL;
