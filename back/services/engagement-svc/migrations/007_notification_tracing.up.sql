ALTER TABLE engagement_svc.notifications ADD COLUMN trace_parent varchar(55) NOT NULL DEFAULT '';
ALTER TABLE engagement_svc.notifications ADD COLUMN trace_state varchar(512) NOT NULL DEFAULT '';
