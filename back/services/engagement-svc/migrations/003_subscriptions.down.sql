DROP TABLE IF EXISTS engagement_svc.subscriptions;
DROP INDEX IF EXISTS engagement_svc.idx_notifications_retry;
ALTER TABLE engagement_svc.notifications DROP COLUMN IF EXISTS locked_at;
ALTER TABLE engagement_svc.notifications DROP COLUMN IF EXISTS next_attempt_at;
