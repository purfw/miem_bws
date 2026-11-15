DROP TABLE IF EXISTS conference_svc.processed_events;
DROP TABLE IF EXISTS conference_svc.conference_counters;

DROP INDEX IF EXISTS conference_svc.uq_conferences_featured;
DROP INDEX IF EXISTS conference_svc.idx_conferences_city;
DROP INDEX IF EXISTS conference_svc.idx_conferences_format;

ALTER TABLE conference_svc.conferences
    DROP COLUMN IF EXISTS is_featured,
    DROP COLUMN IF EXISTS cover_image_url,
    DROP COLUMN IF EXISTS city,
    DROP COLUMN IF EXISTS format;

DROP TYPE IF EXISTS conference_svc.conference_format;
