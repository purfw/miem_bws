DROP TRIGGER IF EXISTS trg_organizers_touch_conference ON conference_svc.organizers;
DROP TRIGGER IF EXISTS trg_slots_touch_conference ON conference_svc.schedule_slots;
DROP TRIGGER IF EXISTS trg_sections_touch_conference ON conference_svc.sections;
DROP FUNCTION IF EXISTS conference_svc.touch_parent_conference();
