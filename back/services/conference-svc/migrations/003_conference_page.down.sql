DROP TABLE IF EXISTS conference_svc.favorites;

DROP INDEX IF EXISTS conference_svc.idx_schedule_section;

ALTER TABLE conference_svc.sections
    DROP COLUMN IF EXISTS moderator_name,
    DROP COLUMN IF EXISTS moderator_affiliation,
    DROP COLUMN IF EXISTS moderator_user_id;

DROP INDEX IF EXISTS conference_svc.idx_organizers_conf_kind;

ALTER TABLE conference_svc.organizers
    DROP CONSTRAINT IF EXISTS chk_organizer_kind;

ALTER TABLE conference_svc.organizers
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS job_title,
    DROP COLUMN IF EXISTS photo_url,
    DROP COLUMN IF EXISTS sort_order;
