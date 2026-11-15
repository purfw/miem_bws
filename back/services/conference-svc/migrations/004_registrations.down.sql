DROP TRIGGER IF EXISTS trg_registration_touch ON conference_svc.registrations;
DROP FUNCTION IF EXISTS conference_svc.touch_registration_updated_at();
DROP TABLE IF EXISTS conference_svc.registration_history;
DROP TABLE IF EXISTS conference_svc.registration_counters;
DROP TABLE IF EXISTS conference_svc.registrations;
DROP TYPE IF EXISTS conference_svc.registration_status;
