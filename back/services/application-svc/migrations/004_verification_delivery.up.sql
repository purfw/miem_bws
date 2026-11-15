ALTER TABLE application_svc.organizer_verifications ADD COLUMN role_applied boolean NOT NULL DEFAULT true;
ALTER TABLE application_svc.organizer_verifications ALTER COLUMN role_applied SET DEFAULT false;

CREATE TABLE application_svc.verification_role_jobs (
    verification_id uuid PRIMARY KEY REFERENCES application_svc.organizer_verifications(id),
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text,
    completed_at timestamptz
);
