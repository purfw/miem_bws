-- Runs once on an empty data directory. Django receives read-only access.
-- Role passwords come from SVC_DB_PASSWORD; later rotation requires ALTER ROLE.

\getenv svc_db_password SVC_DB_PASSWORD
\if :{?svc_db_password}
\else
\echo 'SVC_DB_PASSWORD is not set'
\quit 1
\endif

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

SELECT set_config('confhub.svc_db_password', :'svc_db_password', false);

DO $bootstrap$
DECLARE
    pw text := current_setting('confhub.svc_db_password');
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'identity_svc')    THEN EXECUTE format('CREATE ROLE identity_svc    LOGIN PASSWORD %L', pw); END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'conference_svc')  THEN EXECUTE format('CREATE ROLE conference_svc  LOGIN PASSWORD %L', pw); END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'application_svc') THEN EXECUTE format('CREATE ROLE application_svc LOGIN PASSWORD %L', pw); END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'engagement_svc')  THEN EXECUTE format('CREATE ROLE engagement_svc  LOGIN PASSWORD %L', pw); END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'django_reader')   THEN EXECUTE format('CREATE ROLE django_reader   LOGIN PASSWORD %L', pw); END IF;
END
$bootstrap$;

CREATE SCHEMA IF NOT EXISTS identity_svc    AUTHORIZATION identity_svc;
CREATE SCHEMA IF NOT EXISTS conference_svc  AUTHORIZATION conference_svc;
CREATE SCHEMA IF NOT EXISTS application_svc AUTHORIZATION application_svc;
CREATE SCHEMA IF NOT EXISTS engagement_svc  AUTHORIZATION engagement_svc;

ALTER ROLE identity_svc    SET search_path = identity_svc, public;
ALTER ROLE conference_svc  SET search_path = conference_svc, public;
ALTER ROLE application_svc SET search_path = application_svc, public;
ALTER ROLE engagement_svc  SET search_path = engagement_svc, public;
ALTER ROLE django_reader   SET search_path = identity_svc, conference_svc, application_svc, engagement_svc, public;

DO $grants$ BEGIN
    EXECUTE format('GRANT CREATE ON DATABASE %I TO identity_svc, conference_svc, application_svc, engagement_svc',
                   current_database());
END $grants$;

GRANT USAGE ON SCHEMA identity_svc, conference_svc, application_svc, engagement_svc TO django_reader;

ALTER DEFAULT PRIVILEGES FOR ROLE identity_svc    IN SCHEMA identity_svc    GRANT SELECT ON TABLES TO django_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE conference_svc  IN SCHEMA conference_svc  GRANT SELECT ON TABLES TO django_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE application_svc IN SCHEMA application_svc GRANT SELECT ON TABLES TO django_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE engagement_svc  IN SCHEMA engagement_svc  GRANT SELECT ON TABLES TO django_reader;
