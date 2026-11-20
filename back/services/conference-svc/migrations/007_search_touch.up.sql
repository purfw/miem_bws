-- The search indexer picks up conferences by updated_at. Sections, schedule
-- slots and organizers are part of the indexed document, so their changes
-- must move the parent conference forward as well.
CREATE FUNCTION conference_svc.touch_parent_conference() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        UPDATE conference_svc.conferences SET updated_at = now()
        WHERE id = OLD.conference_id AND updated_at < now();
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        UPDATE conference_svc.conferences SET updated_at = now()
        WHERE id = NEW.conference_id AND updated_at < now();
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_sections_touch_conference
    AFTER INSERT OR UPDATE OR DELETE ON conference_svc.sections
    FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_parent_conference();

CREATE TRIGGER trg_slots_touch_conference
    AFTER INSERT OR UPDATE OR DELETE ON conference_svc.schedule_slots
    FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_parent_conference();

CREATE TRIGGER trg_organizers_touch_conference
    AFTER INSERT OR UPDATE OR DELETE ON conference_svc.organizers
    FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_parent_conference();
