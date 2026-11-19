-- PostgreSQL does not allow a freshly-added enum value to be used until the
-- transaction which added it has committed. Keep this migration separate
-- from the data migration in 003_account_controls.
ALTER TYPE identity_svc.user_role ADD VALUE IF NOT EXISTS 'user';
ALTER TYPE identity_svc.user_role ADD VALUE IF NOT EXISTS 'verified_organizer';
ALTER TYPE identity_svc.user_role ADD VALUE IF NOT EXISTS 'organizer_member';
