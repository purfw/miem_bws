-- Схема базы данных ConfHub (PostgreSQL 16).
--
-- Файл получен командой pg_dump --schema-only и нужен для чтения и проверки.
-- При развёртывании роли и схемы создаёт scripts/postgres-init.sql, а таблицы —
-- миграции сервисов (services/*/migrations), которые выполняются при старте.
-- Обновление файла: make schema

--
-- PostgreSQL database dump
--


-- Dumped from database version 16.15
-- Dumped by pg_dump version 16.15

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: application_svc; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA application_svc;


--
-- Name: conference_svc; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA conference_svc;


--
-- Name: engagement_svc; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA engagement_svc;


--
-- Name: identity_svc; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA identity_svc;


--
-- Name: pg_trgm; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;


--
-- Name: EXTENSION pg_trgm; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pg_trgm IS 'text similarity measurement and index searching based on trigrams';


--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: application_status; Type: TYPE; Schema: application_svc; Owner: -
--

CREATE TYPE application_svc.application_status AS ENUM (
    'submitted',
    'under_review',
    'revision_requested',
    'accepted',
    'rejected',
    'withdrawn'
);


--
-- Name: participation_form; Type: TYPE; Schema: application_svc; Owner: -
--

CREATE TYPE application_svc.participation_form AS ENUM (
    'speaker',
    'poster',
    'listener'
);


--
-- Name: review_recommendation; Type: TYPE; Schema: application_svc; Owner: -
--

CREATE TYPE application_svc.review_recommendation AS ENUM (
    'accept',
    'minor_revision',
    'major_revision',
    'reject'
);


--
-- Name: verification_status; Type: TYPE; Schema: application_svc; Owner: -
--

CREATE TYPE application_svc.verification_status AS ENUM (
    'pending',
    'approved',
    'rejected'
);


--
-- Name: conference_format; Type: TYPE; Schema: conference_svc; Owner: -
--

CREATE TYPE conference_svc.conference_format AS ENUM (
    'offline',
    'hybrid',
    'online'
);


--
-- Name: conference_status; Type: TYPE; Schema: conference_svc; Owner: -
--

CREATE TYPE conference_svc.conference_status AS ENUM (
    'draft',
    'registration_open',
    'registration_closed',
    'in_progress',
    'completed',
    'cancelled',
    'archived'
);


--
-- Name: registration_status; Type: TYPE; Schema: conference_svc; Owner: -
--

CREATE TYPE conference_svc.registration_status AS ENUM (
    'confirmed',
    'waitlisted',
    'cancelled'
);


--
-- Name: user_role; Type: TYPE; Schema: identity_svc; Owner: -
--

CREATE TYPE identity_svc.user_role AS ENUM (
    'student',
    'moderator',
    'admin',
    'user',
    'verified_organizer',
    'organizer_member'
);


--
-- Name: touch_updated_at(); Type: FUNCTION; Schema: application_svc; Owner: -
--

CREATE FUNCTION application_svc.touch_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


--
-- Name: touch_registration_updated_at(); Type: FUNCTION; Schema: conference_svc; Owner: -
--

CREATE FUNCTION conference_svc.touch_registration_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


--
-- Name: touch_parent_conference(); Type: FUNCTION; Schema: conference_svc; Owner: -
--

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


--
-- Name: touch_updated_at(); Type: FUNCTION; Schema: conference_svc; Owner: -
--

CREATE FUNCTION conference_svc.touch_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


--
-- Name: touch_updated_at(); Type: FUNCTION; Schema: identity_svc; Owner: -
--

CREATE FUNCTION identity_svc.touch_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: application_files; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.application_files (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    application_id uuid NOT NULL,
    original_name character varying(500) NOT NULL,
    stored_name character varying(500) NOT NULL,
    mime_type character varying(100) NOT NULL,
    size_bytes bigint NOT NULL,
    bucket character varying(100) NOT NULL,
    uploaded_by uuid NOT NULL,
    uploaded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_size CHECK (((size_bytes > 0) AND (size_bytes <= ((21 * 1024) * 1024))))
);


--
-- Name: applications; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.applications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conference_id uuid NOT NULL,
    section_id uuid,
    user_id uuid NOT NULL,
    cached_user_email character varying(255) NOT NULL,
    cached_user_name character varying(255) NOT NULL,
    cached_conf_title character varying(500) NOT NULL,
    form application_svc.participation_form NOT NULL,
    title character varying(500) NOT NULL,
    abstract text,
    co_authors jsonb DEFAULT '[]'::jsonb NOT NULL,
    keywords text[] DEFAULT '{}'::text[] NOT NULL,
    status application_svc.application_status DEFAULT 'submitted'::application_svc.application_status NOT NULL,
    status_changed_at timestamp with time zone DEFAULT now() NOT NULL,
    status_changed_by uuid,
    moderator_comment text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: event_failures; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.event_failures (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consumer text NOT NULL,
    stream_sequence bigint NOT NULL,
    subject text NOT NULL,
    payload bytea NOT NULL,
    status text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT event_failures_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT event_failures_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'dead'::text, 'completed'::text]))),
    CONSTRAINT event_failures_stream_sequence_check CHECK ((stream_sequence > 0))
);


--
-- Name: organizer_verifications; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.organizer_verifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    user_email character varying(255) NOT NULL,
    user_name character varying(255) NOT NULL,
    organization character varying(500) NOT NULL,
    method character varying(30) DEFAULT 'documents'::character varying NOT NULL,
    status application_svc.verification_status DEFAULT 'pending'::application_svc.verification_status NOT NULL,
    rejection_reason text,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    role_applied boolean DEFAULT false NOT NULL,
    CONSTRAINT chk_verification_method CHECK (((method)::text = ANY ((ARRAY['documents'::character varying, 'trusted_domain'::character varying, 'orcid'::character varying])::text[])))
);


--
-- Name: outbox_events; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.outbox_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type character varying(100) NOT NULL,
    aggregate_type character varying(50),
    aggregate_id uuid,
    payload jsonb NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    next_retry_at timestamp with time zone,
    last_error text,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: participation_documents; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.participation_documents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    registration_id uuid NOT NULL,
    user_id uuid NOT NULL,
    conference_id uuid NOT NULL,
    conference_title character varying(500) NOT NULL,
    participant_name character varying(255) NOT NULL,
    stored_name character varying(1000) NOT NULL,
    bucket character varying(255) NOT NULL,
    verification_hash bytea NOT NULL,
    download_token_hash bytea NOT NULL,
    download_expires_at timestamp with time zone NOT NULL,
    issued_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: reviews; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.reviews (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    application_id uuid NOT NULL,
    reviewer_id uuid NOT NULL,
    cached_reviewer_name character varying(255) NOT NULL,
    score smallint NOT NULL,
    summary text NOT NULL,
    strengths text,
    weaknesses text,
    recommendation application_svc.review_recommendation NOT NULL,
    is_visible_to_author boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT reviews_score_check CHECK (((score >= 1) AND (score <= 10)))
);


--
-- Name: schema_migrations; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: storage_deletions; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.storage_deletions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    bucket character varying(255) NOT NULL,
    stored_name character varying(1000) NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT storage_deletions_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'dead'::character varying, 'completed'::character varying])::text[])))
);


--
-- Name: trusted_domains; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.trusted_domains (
    domain character varying(255) NOT NULL,
    organization character varying(500) NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: verification_documents; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.verification_documents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    verification_id uuid NOT NULL,
    original_name character varying(500) NOT NULL,
    stored_name character varying(1000) NOT NULL,
    mime_type character varying(255) NOT NULL,
    size_bytes bigint NOT NULL,
    bucket character varying(255) NOT NULL,
    uploaded_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: verification_role_jobs; Type: TABLE; Schema: application_svc; Owner: -
--

CREATE TABLE application_svc.verification_role_jobs (
    verification_id uuid NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    last_error text,
    completed_at timestamp with time zone
);


--
-- Name: conference_counters; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.conference_counters (
    conference_id uuid NOT NULL,
    participants integer DEFAULT 0 NOT NULL,
    accepted integer DEFAULT 0 NOT NULL,
    rejected integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_counters_nonneg CHECK (((participants >= 0) AND (accepted >= 0) AND (rejected >= 0)))
);


--
-- Name: conference_members; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.conference_members (
    conference_id uuid NOT NULL,
    user_id uuid NOT NULL,
    permissions text[] DEFAULT ARRAY['edit'::text, 'schedule'::text, 'participants'::text] NOT NULL,
    added_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: conferences; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.conferences (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    title character varying(500) NOT NULL,
    title_en character varying(500),
    slug character varying(200) NOT NULL,
    description text,
    description_en text,
    status conference_svc.conference_status DEFAULT 'draft'::conference_svc.conference_status NOT NULL,
    start_date date NOT NULL,
    end_date date NOT NULL,
    registration_deadline timestamp with time zone,
    max_participants integer,
    location character varying(500),
    topics text[] DEFAULT '{}'::text[] NOT NULL,
    created_by uuid NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS ((((setweight(to_tsvector('russian'::regconfig, (COALESCE(title, ''::character varying))::text), 'A'::"char") || setweight(to_tsvector('english'::regconfig, (COALESCE(title_en, ''::character varying))::text), 'A'::"char")) || setweight(to_tsvector('russian'::regconfig, COALESCE(description, ''::text)), 'B'::"char")) || setweight(to_tsvector('english'::regconfig, COALESCE(description_en, ''::text)), 'B'::"char"))) STORED,
    format conference_svc.conference_format DEFAULT 'offline'::conference_svc.conference_format NOT NULL,
    city character varying(200),
    cover_image_url character varying(1000),
    is_featured boolean DEFAULT false NOT NULL,
    publication_allowed boolean DEFAULT false NOT NULL,
    CONSTRAINT chk_dates CHECK ((end_date >= start_date))
);


--
-- Name: COLUMN conferences.location; Type: COMMENT; Schema: conference_svc; Owner: -
--

COMMENT ON COLUMN conference_svc.conferences.location IS 'Адрес площадки внутри города (primary_venue.address)';


--
-- Name: COLUMN conferences.city; Type: COMMENT; Schema: conference_svc; Owner: -
--

COMMENT ON COLUMN conference_svc.conferences.city IS 'Город проведения; NULL для формата online';


--
-- Name: event_failures; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.event_failures (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consumer text NOT NULL,
    stream_sequence bigint NOT NULL,
    subject text NOT NULL,
    payload bytea NOT NULL,
    status text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT event_failures_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT event_failures_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'dead'::text, 'completed'::text]))),
    CONSTRAINT event_failures_stream_sequence_check CHECK ((stream_sequence > 0))
);


--
-- Name: favorites; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.favorites (
    user_id uuid NOT NULL,
    conference_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: join_link_access_log; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.join_link_access_log (
    id bigint NOT NULL,
    slot_id uuid NOT NULL,
    user_id uuid NOT NULL,
    accessed_at timestamp with time zone DEFAULT now() NOT NULL,
    request_id character varying(100),
    ip_hash character varying(64)
);


--
-- Name: join_link_access_log_id_seq; Type: SEQUENCE; Schema: conference_svc; Owner: -
--

CREATE SEQUENCE conference_svc.join_link_access_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: join_link_access_log_id_seq; Type: SEQUENCE OWNED BY; Schema: conference_svc; Owner: -
--

ALTER SEQUENCE conference_svc.join_link_access_log_id_seq OWNED BY conference_svc.join_link_access_log.id;


--
-- Name: organizers; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.organizers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conference_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    email character varying(255),
    affiliation character varying(255),
    role character varying(100) DEFAULT 'organizer'::character varying NOT NULL,
    user_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    kind character varying(20) DEFAULT 'organizer'::character varying NOT NULL,
    job_title character varying(255),
    photo_url character varying(1000),
    sort_order integer DEFAULT 0 NOT NULL,
    CONSTRAINT chk_organizer_kind CHECK (((kind)::text = ANY ((ARRAY['organizer'::character varying, 'committee'::character varying])::text[])))
);


--
-- Name: outbox_events; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.outbox_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type character varying(100) NOT NULL,
    aggregate_type character varying(50),
    aggregate_id uuid,
    payload jsonb NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    next_retry_at timestamp with time zone,
    last_error text,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: partners; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.partners (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conference_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    logo_url character varying(1000),
    website_url character varying(1000),
    tier character varying(50),
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: processed_events; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.processed_events (
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: registration_counters; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.registration_counters (
    conference_id uuid NOT NULL,
    confirmed_count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT registration_counters_confirmed_count_check CHECK ((confirmed_count >= 0))
);


--
-- Name: registration_history; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.registration_history (
    id bigint NOT NULL,
    registration_id uuid NOT NULL,
    old_status conference_svc.registration_status,
    new_status conference_svc.registration_status NOT NULL,
    reason character varying(100) NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: registration_history_id_seq; Type: SEQUENCE; Schema: conference_svc; Owner: -
--

CREATE SEQUENCE conference_svc.registration_history_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: registration_history_id_seq; Type: SEQUENCE OWNED BY; Schema: conference_svc; Owner: -
--

ALTER SEQUENCE conference_svc.registration_history_id_seq OWNED BY conference_svc.registration_history.id;


--
-- Name: registrations; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.registrations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conference_id uuid NOT NULL,
    user_id uuid NOT NULL,
    user_email character varying(255) NOT NULL,
    user_name character varying(255) NOT NULL,
    role_at_conf character varying(30) DEFAULT 'attendee'::character varying NOT NULL,
    status conference_svc.registration_status NOT NULL,
    cancellation_count smallint DEFAULT 0 NOT NULL,
    registered_at timestamp with time zone DEFAULT now() NOT NULL,
    confirmed_at timestamp with time zone,
    waitlisted_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_registration_cancellations CHECK (((cancellation_count >= 0) AND (cancellation_count <= 3))),
    CONSTRAINT chk_registration_role CHECK (((role_at_conf)::text = ANY ((ARRAY['attendee'::character varying, 'speaker'::character varying, 'reviewer'::character varying])::text[])))
);


--
-- Name: schedule_slots; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.schedule_slots (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conference_id uuid NOT NULL,
    section_id uuid,
    title character varying(500) NOT NULL,
    speaker_name character varying(255),
    speaker_affiliation character varying(255),
    room character varying(100),
    slot_date date NOT NULL,
    start_time time without time zone NOT NULL,
    end_time time without time zone NOT NULL,
    description text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    session_type character varying(50) DEFAULT 'talk'::character varying NOT NULL,
    join_url_ciphertext bytea,
    join_url_nonce bytea,
    join_url_key_version smallint,
    join_url_public_at timestamp with time zone,
    CONSTRAINT chk_time CHECK ((end_time > start_time))
);


--
-- Name: schema_migrations; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: sections; Type: TABLE; Schema: conference_svc; Owner: -
--

CREATE TABLE conference_svc.sections (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conference_id uuid NOT NULL,
    title character varying(500) NOT NULL,
    title_en character varying(500),
    description text,
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    moderator_name character varying(255),
    moderator_affiliation character varying(255),
    moderator_user_id uuid
);


--
-- Name: audit_log; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log (
    id bigint NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (occurred_at);


--
-- Name: audit_log_id_seq; Type: SEQUENCE; Schema: engagement_svc; Owner: -
--

CREATE SEQUENCE engagement_svc.audit_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: audit_log_id_seq; Type: SEQUENCE OWNED BY; Schema: engagement_svc; Owner: -
--

ALTER SEQUENCE engagement_svc.audit_log_id_seq OWNED BY engagement_svc.audit_log.id;


--
-- Name: audit_log_2026_07; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2026_07 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2026_08; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2026_08 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2026_09; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2026_09 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2026_10; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2026_10 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2026_11; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2026_11 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2026_12; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2026_12 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2027_01; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2027_01 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_2027_02; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_2027_02 (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_default; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.audit_log_default (
    id bigint DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass) NOT NULL,
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    actor_id uuid,
    entity_type character varying(50),
    entity_id uuid,
    payload jsonb NOT NULL,
    ip_address inet,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: event_failures; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.event_failures (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    consumer text NOT NULL,
    stream_sequence bigint NOT NULL,
    subject text NOT NULL,
    payload bytea NOT NULL,
    status text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT event_failures_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT event_failures_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'dead'::text, 'completed'::text]))),
    CONSTRAINT event_failures_stream_sequence_check CHECK ((stream_sequence > 0))
);


--
-- Name: notifications; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.notifications (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid,
    channel character varying(20) DEFAULT 'email'::character varying NOT NULL,
    type character varying(100) NOT NULL,
    recipient character varying(255) NOT NULL,
    subject character varying(500),
    body text,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    attempts smallint DEFAULT 0 NOT NULL,
    last_error text,
    sent_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    message bytea,
    dedup_key text,
    subscription_id uuid,
    trace_parent character varying(55) DEFAULT ''::character varying NOT NULL,
    trace_state character varying(512) DEFAULT ''::character varying NOT NULL
);


--
-- Name: processed_events; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.processed_events (
    event_id uuid NOT NULL,
    event_type character varying(100) NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: schema_migrations; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: subscription_deliveries; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.subscription_deliveries (
    subscription_id uuid NOT NULL,
    criterion character varying(50) NOT NULL,
    occurrence text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: subscriptions; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    user_email character varying(255) NOT NULL,
    conference_id uuid,
    notify_on text[] NOT NULL,
    channels text[] NOT NULL,
    unsubscribe_token_hash bytea NOT NULL,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    unsubscribe_token text,
    criteria jsonb DEFAULT '{}'::jsonb NOT NULL,
    criteria_hash character varying(64),
    CONSTRAINT chk_subscription_channels CHECK ((cardinality(channels) > 0)),
    CONSTRAINT chk_subscription_notify_on CHECK ((cardinality(notify_on) > 0)),
    CONSTRAINT subscription_target CHECK ((((conference_id IS NOT NULL) AND (criteria_hash IS NULL)) OR ((conference_id IS NULL) AND (criteria_hash IS NOT NULL) AND (criteria <> '{}'::jsonb))))
);


--
-- Name: support_messages; Type: TABLE; Schema: engagement_svc; Owner: -
--

CREATE TABLE engagement_svc.support_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid,
    name character varying(255) NOT NULL,
    email character varying(255) NOT NULL,
    topic character varying(50) DEFAULT 'other'::character varying NOT NULL,
    subject character varying(500),
    message text NOT NULL,
    conference_id uuid,
    ip_address inet,
    status character varying(20) DEFAULT 'new'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_support_message CHECK (((length(message) >= 10) AND (length(message) <= 5000))),
    CONSTRAINT chk_support_status CHECK (((status)::text = ANY ((ARRAY['new'::character varying, 'in_progress'::character varying, 'closed'::character varying])::text[])))
);


--
-- Name: email_verification_tokens; Type: TABLE; Schema: identity_svc; Owner: -
--

CREATE TABLE identity_svc.email_verification_tokens (
    token uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: external_identities; Type: TABLE; Schema: identity_svc; Owner: -
--

CREATE TABLE identity_svc.external_identities (
    provider character varying(50) NOT NULL,
    subject character varying(255) NOT NULL,
    user_id uuid NOT NULL,
    display_name character varying(255),
    publication_count integer DEFAULT 0 NOT NULL,
    linked_at timestamp with time zone DEFAULT now() NOT NULL,
    last_synced_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT external_identities_publication_count_check CHECK ((publication_count >= 0))
);


--
-- Name: outbox_events; Type: TABLE; Schema: identity_svc; Owner: -
--

CREATE TABLE identity_svc.outbox_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type character varying(100) NOT NULL,
    aggregate_type character varying(50),
    aggregate_id uuid,
    payload jsonb NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    next_retry_at timestamp with time zone,
    last_error text,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: password_reset_tokens; Type: TABLE; Schema: identity_svc; Owner: -
--

CREATE TABLE identity_svc.password_reset_tokens (
    token uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: schema_migrations; Type: TABLE; Schema: identity_svc; Owner: -
--

CREATE TABLE identity_svc.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: identity_svc; Owner: -
--

CREATE TABLE identity_svc.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email character varying(255) NOT NULL,
    password_hash character varying(255) NOT NULL,
    first_name character varying(100) NOT NULL,
    last_name character varying(100) NOT NULL,
    patronymic character varying(100),
    affiliation character varying(255),
    role identity_svc.user_role DEFAULT 'user'::identity_svc.user_role NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    email_verified boolean DEFAULT false NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    locale character varying(5) DEFAULT 'ru'::character varying NOT NULL,
    auth_version bigint DEFAULT 0 NOT NULL,
    CONSTRAINT chk_users_locale CHECK (((locale)::text = ANY ((ARRAY['ru'::character varying, 'en'::character varying])::text[])))
);


--
-- Name: audit_log_2026_07; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2026_07 FOR VALUES FROM ('2026-07-01 00:00:00+00') TO ('2026-08-01 00:00:00+00');


--
-- Name: audit_log_2026_08; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2026_08 FOR VALUES FROM ('2026-08-01 00:00:00+00') TO ('2026-09-01 00:00:00+00');


--
-- Name: audit_log_2026_09; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2026_09 FOR VALUES FROM ('2026-09-01 00:00:00+00') TO ('2026-10-01 00:00:00+00');


--
-- Name: audit_log_2026_10; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2026_10 FOR VALUES FROM ('2026-10-01 00:00:00+00') TO ('2026-11-01 00:00:00+00');


--
-- Name: audit_log_2026_11; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2026_11 FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');


--
-- Name: audit_log_2026_12; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2026_12 FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');


--
-- Name: audit_log_2027_01; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2027_01 FOR VALUES FROM ('2027-01-01 00:00:00+00') TO ('2027-02-01 00:00:00+00');


--
-- Name: audit_log_2027_02; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_2027_02 FOR VALUES FROM ('2027-02-01 00:00:00+00') TO ('2027-03-01 00:00:00+00');


--
-- Name: audit_log_default; Type: TABLE ATTACH; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ATTACH PARTITION engagement_svc.audit_log_default DEFAULT;


--
-- Name: join_link_access_log id; Type: DEFAULT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.join_link_access_log ALTER COLUMN id SET DEFAULT nextval('conference_svc.join_link_access_log_id_seq'::regclass);


--
-- Name: registration_history id; Type: DEFAULT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registration_history ALTER COLUMN id SET DEFAULT nextval('conference_svc.registration_history_id_seq'::regclass);


--
-- Name: audit_log id; Type: DEFAULT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log ALTER COLUMN id SET DEFAULT nextval('engagement_svc.audit_log_id_seq'::regclass);


--
-- Name: application_files application_files_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.application_files
    ADD CONSTRAINT application_files_pkey PRIMARY KEY (id);


--
-- Name: applications applications_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.applications
    ADD CONSTRAINT applications_pkey PRIMARY KEY (id);


--
-- Name: event_failures event_failures_consumer_stream_sequence_key; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.event_failures
    ADD CONSTRAINT event_failures_consumer_stream_sequence_key UNIQUE (consumer, stream_sequence);


--
-- Name: event_failures event_failures_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.event_failures
    ADD CONSTRAINT event_failures_pkey PRIMARY KEY (id);


--
-- Name: organizer_verifications organizer_verifications_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.organizer_verifications
    ADD CONSTRAINT organizer_verifications_pkey PRIMARY KEY (id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);


--
-- Name: participation_documents participation_documents_download_token_hash_key; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.participation_documents
    ADD CONSTRAINT participation_documents_download_token_hash_key UNIQUE (download_token_hash);


--
-- Name: participation_documents participation_documents_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.participation_documents
    ADD CONSTRAINT participation_documents_pkey PRIMARY KEY (id);


--
-- Name: participation_documents participation_documents_registration_id_key; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.participation_documents
    ADD CONSTRAINT participation_documents_registration_id_key UNIQUE (registration_id);


--
-- Name: participation_documents participation_documents_verification_hash_key; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.participation_documents
    ADD CONSTRAINT participation_documents_verification_hash_key UNIQUE (verification_hash);


--
-- Name: reviews reviews_application_id_reviewer_id_key; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.reviews
    ADD CONSTRAINT reviews_application_id_reviewer_id_key UNIQUE (application_id, reviewer_id);


--
-- Name: reviews reviews_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.reviews
    ADD CONSTRAINT reviews_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: storage_deletions storage_deletions_bucket_stored_name_key; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.storage_deletions
    ADD CONSTRAINT storage_deletions_bucket_stored_name_key UNIQUE (bucket, stored_name);


--
-- Name: storage_deletions storage_deletions_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.storage_deletions
    ADD CONSTRAINT storage_deletions_pkey PRIMARY KEY (id);


--
-- Name: trusted_domains trusted_domains_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.trusted_domains
    ADD CONSTRAINT trusted_domains_pkey PRIMARY KEY (domain);


--
-- Name: verification_documents verification_documents_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.verification_documents
    ADD CONSTRAINT verification_documents_pkey PRIMARY KEY (id);


--
-- Name: verification_role_jobs verification_role_jobs_pkey; Type: CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.verification_role_jobs
    ADD CONSTRAINT verification_role_jobs_pkey PRIMARY KEY (verification_id);


--
-- Name: conference_counters conference_counters_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.conference_counters
    ADD CONSTRAINT conference_counters_pkey PRIMARY KEY (conference_id);


--
-- Name: conference_members conference_members_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.conference_members
    ADD CONSTRAINT conference_members_pkey PRIMARY KEY (conference_id, user_id);


--
-- Name: conferences conferences_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.conferences
    ADD CONSTRAINT conferences_pkey PRIMARY KEY (id);


--
-- Name: event_failures event_failures_consumer_stream_sequence_key; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.event_failures
    ADD CONSTRAINT event_failures_consumer_stream_sequence_key UNIQUE (consumer, stream_sequence);


--
-- Name: event_failures event_failures_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.event_failures
    ADD CONSTRAINT event_failures_pkey PRIMARY KEY (id);


--
-- Name: favorites favorites_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.favorites
    ADD CONSTRAINT favorites_pkey PRIMARY KEY (user_id, conference_id);


--
-- Name: join_link_access_log join_link_access_log_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.join_link_access_log
    ADD CONSTRAINT join_link_access_log_pkey PRIMARY KEY (id);


--
-- Name: organizers organizers_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.organizers
    ADD CONSTRAINT organizers_pkey PRIMARY KEY (id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);


--
-- Name: partners partners_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.partners
    ADD CONSTRAINT partners_pkey PRIMARY KEY (id);


--
-- Name: processed_events processed_events_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.processed_events
    ADD CONSTRAINT processed_events_pkey PRIMARY KEY (event_id);


--
-- Name: registration_counters registration_counters_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registration_counters
    ADD CONSTRAINT registration_counters_pkey PRIMARY KEY (conference_id);


--
-- Name: registration_history registration_history_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registration_history
    ADD CONSTRAINT registration_history_pkey PRIMARY KEY (id);


--
-- Name: registrations registrations_conference_id_user_id_key; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registrations
    ADD CONSTRAINT registrations_conference_id_user_id_key UNIQUE (conference_id, user_id);


--
-- Name: registrations registrations_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registrations
    ADD CONSTRAINT registrations_pkey PRIMARY KEY (id);


--
-- Name: schedule_slots schedule_slots_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.schedule_slots
    ADD CONSTRAINT schedule_slots_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: sections sections_conference_id_sort_order_key; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.sections
    ADD CONSTRAINT sections_conference_id_sort_order_key UNIQUE (conference_id, sort_order);


--
-- Name: sections sections_pkey; Type: CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.sections
    ADD CONSTRAINT sections_pkey PRIMARY KEY (id);


--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2026_07 audit_log_2026_07_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2026_07
    ADD CONSTRAINT audit_log_2026_07_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2026_08 audit_log_2026_08_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2026_08
    ADD CONSTRAINT audit_log_2026_08_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2026_09 audit_log_2026_09_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2026_09
    ADD CONSTRAINT audit_log_2026_09_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2026_10 audit_log_2026_10_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2026_10
    ADD CONSTRAINT audit_log_2026_10_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2026_11 audit_log_2026_11_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2026_11
    ADD CONSTRAINT audit_log_2026_11_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2026_12 audit_log_2026_12_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2026_12
    ADD CONSTRAINT audit_log_2026_12_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2027_01 audit_log_2027_01_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2027_01
    ADD CONSTRAINT audit_log_2027_01_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_2027_02 audit_log_2027_02_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_2027_02
    ADD CONSTRAINT audit_log_2027_02_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: audit_log_default audit_log_default_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.audit_log_default
    ADD CONSTRAINT audit_log_default_pkey PRIMARY KEY (id, occurred_at);


--
-- Name: event_failures event_failures_consumer_stream_sequence_key; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.event_failures
    ADD CONSTRAINT event_failures_consumer_stream_sequence_key UNIQUE (consumer, stream_sequence);


--
-- Name: event_failures event_failures_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.event_failures
    ADD CONSTRAINT event_failures_pkey PRIMARY KEY (id);


--
-- Name: notifications notifications_dedup_key_key; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.notifications
    ADD CONSTRAINT notifications_dedup_key_key UNIQUE (dedup_key);


--
-- Name: notifications notifications_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);


--
-- Name: processed_events processed_events_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.processed_events
    ADD CONSTRAINT processed_events_pkey PRIMARY KEY (event_id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: subscription_deliveries subscription_deliveries_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.subscription_deliveries
    ADD CONSTRAINT subscription_deliveries_pkey PRIMARY KEY (subscription_id, criterion, occurrence);


--
-- Name: subscriptions subscriptions_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);


--
-- Name: subscriptions subscriptions_unsubscribe_token_hash_key; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.subscriptions
    ADD CONSTRAINT subscriptions_unsubscribe_token_hash_key UNIQUE (unsubscribe_token_hash);


--
-- Name: support_messages support_messages_pkey; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.support_messages
    ADD CONSTRAINT support_messages_pkey PRIMARY KEY (id);


--
-- Name: subscriptions uq_subscription_user_conference; Type: CONSTRAINT; Schema: engagement_svc; Owner: -
--

ALTER TABLE ONLY engagement_svc.subscriptions
    ADD CONSTRAINT uq_subscription_user_conference UNIQUE (user_id, conference_id);


--
-- Name: email_verification_tokens email_verification_tokens_pkey; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.email_verification_tokens
    ADD CONSTRAINT email_verification_tokens_pkey PRIMARY KEY (token);


--
-- Name: external_identities external_identities_pkey; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.external_identities
    ADD CONSTRAINT external_identities_pkey PRIMARY KEY (provider, subject);


--
-- Name: external_identities external_identities_provider_user_id_key; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.external_identities
    ADD CONSTRAINT external_identities_provider_user_id_key UNIQUE (provider, user_id);


--
-- Name: outbox_events outbox_events_pkey; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);


--
-- Name: password_reset_tokens password_reset_tokens_pkey; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.password_reset_tokens
    ADD CONSTRAINT password_reset_tokens_pkey PRIMARY KEY (token);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: event_failures_pending; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX event_failures_pending ON application_svc.event_failures USING btree (next_attempt_at) WHERE (status = 'pending'::text);


--
-- Name: idx_app_outbox_pending; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_app_outbox_pending ON application_svc.outbox_events USING btree (created_at) WHERE ((status)::text = 'pending'::text);


--
-- Name: idx_applications_conf; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_applications_conf ON application_svc.applications USING btree (conference_id, status);


--
-- Name: idx_applications_pipeline; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_applications_pipeline ON application_svc.applications USING btree (conference_id, status, status_changed_at) WHERE (status = ANY (ARRAY['submitted'::application_svc.application_status, 'under_review'::application_svc.application_status, 'revision_requested'::application_svc.application_status]));


--
-- Name: idx_applications_user; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_applications_user ON application_svc.applications USING btree (user_id, created_at DESC);


--
-- Name: idx_files_app; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_files_app ON application_svc.application_files USING btree (application_id);


--
-- Name: idx_participation_documents_conference; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_participation_documents_conference ON application_svc.participation_documents USING btree (conference_id);


--
-- Name: idx_participation_documents_user; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_participation_documents_user ON application_svc.participation_documents USING btree (user_id, issued_at DESC);


--
-- Name: idx_reviews_app; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_reviews_app ON application_svc.reviews USING btree (application_id);


--
-- Name: idx_verification_documents_verification; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_verification_documents_verification ON application_svc.verification_documents USING btree (verification_id, uploaded_at);


--
-- Name: idx_verification_queue; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX idx_verification_queue ON application_svc.organizer_verifications USING btree (status, created_at);


--
-- Name: storage_deletions_pending; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE INDEX storage_deletions_pending ON application_svc.storage_deletions USING btree (next_attempt_at) WHERE ((status)::text = 'pending'::text);


--
-- Name: uq_active_organizer_verification; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE UNIQUE INDEX uq_active_organizer_verification ON application_svc.organizer_verifications USING btree (user_id) WHERE (status = 'pending'::application_svc.verification_status);


--
-- Name: uq_app_inflight; Type: INDEX; Schema: application_svc; Owner: -
--

CREATE UNIQUE INDEX uq_app_inflight ON application_svc.applications USING btree (conference_id, user_id, form) WHERE (status = ANY (ARRAY['submitted'::application_svc.application_status, 'under_review'::application_svc.application_status, 'revision_requested'::application_svc.application_status, 'accepted'::application_svc.application_status]));


--
-- Name: event_failures_pending; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX event_failures_pending ON conference_svc.event_failures USING btree (next_attempt_at) WHERE (status = 'pending'::text);


--
-- Name: idx_conf_outbox_pending; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conf_outbox_pending ON conference_svc.outbox_events USING btree (created_at) WHERE ((status)::text = 'pending'::text);


--
-- Name: idx_conf_processed_at; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conf_processed_at ON conference_svc.processed_events USING btree (processed_at);


--
-- Name: idx_conference_members_user; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conference_members_user ON conference_svc.conference_members USING btree (user_id, conference_id);


--
-- Name: idx_conferences_city; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_city ON conference_svc.conferences USING btree (city) WHERE ((deleted_at IS NULL) AND (city IS NOT NULL));


--
-- Name: idx_conferences_format; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_format ON conference_svc.conferences USING btree (format) WHERE (deleted_at IS NULL);


--
-- Name: idx_conferences_fts; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_fts ON conference_svc.conferences USING gin (search_vector);


--
-- Name: idx_conferences_slug; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE UNIQUE INDEX idx_conferences_slug ON conference_svc.conferences USING btree (slug) WHERE (deleted_at IS NULL);


--
-- Name: idx_conferences_start_date; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_start_date ON conference_svc.conferences USING btree (start_date DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_conferences_status; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_status ON conference_svc.conferences USING btree (status) WHERE (deleted_at IS NULL);


--
-- Name: idx_conferences_title_trgm; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_title_trgm ON conference_svc.conferences USING gin (title public.gin_trgm_ops);


--
-- Name: idx_conferences_topics; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_conferences_topics ON conference_svc.conferences USING gin (topics);


--
-- Name: idx_favorites_conf; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_favorites_conf ON conference_svc.favorites USING btree (conference_id);


--
-- Name: idx_favorites_user_created; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_favorites_user_created ON conference_svc.favorites USING btree (user_id, created_at DESC);


--
-- Name: idx_join_access_slot_time; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_join_access_slot_time ON conference_svc.join_link_access_log USING btree (slot_id, accessed_at DESC);


--
-- Name: idx_organizers_conf; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_organizers_conf ON conference_svc.organizers USING btree (conference_id);


--
-- Name: idx_organizers_conf_kind; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_organizers_conf_kind ON conference_svc.organizers USING btree (conference_id, kind, sort_order);


--
-- Name: idx_organizers_user; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_organizers_user ON conference_svc.organizers USING btree (user_id) WHERE (user_id IS NOT NULL);


--
-- Name: idx_partners_conf; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_partners_conf ON conference_svc.partners USING btree (conference_id);


--
-- Name: idx_registration_history_registration; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_registration_history_registration ON conference_svc.registration_history USING btree (registration_id, occurred_at DESC);


--
-- Name: idx_registrations_confirmed; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_registrations_confirmed ON conference_svc.registrations USING btree (conference_id) WHERE (status = 'confirmed'::conference_svc.registration_status);


--
-- Name: idx_registrations_user; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_registrations_user ON conference_svc.registrations USING btree (user_id, registered_at DESC);


--
-- Name: idx_registrations_waitlist; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_registrations_waitlist ON conference_svc.registrations USING btree (conference_id, waitlisted_at, id) WHERE (status = 'waitlisted'::conference_svc.registration_status);


--
-- Name: idx_schedule_conf_date; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_schedule_conf_date ON conference_svc.schedule_slots USING btree (conference_id, slot_date, start_time);


--
-- Name: idx_schedule_section; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_schedule_section ON conference_svc.schedule_slots USING btree (section_id) WHERE (section_id IS NOT NULL);


--
-- Name: idx_sections_conf; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE INDEX idx_sections_conf ON conference_svc.sections USING btree (conference_id);


--
-- Name: uq_conferences_featured; Type: INDEX; Schema: conference_svc; Owner: -
--

CREATE UNIQUE INDEX uq_conferences_featured ON conference_svc.conferences USING btree (is_featured) WHERE (is_featured AND (deleted_at IS NULL));


--
-- Name: uq_audit_event_id; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX uq_audit_event_id ON ONLY engagement_svc.audit_log USING btree (event_id, occurred_at);


--
-- Name: audit_log_2026_07_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2026_07_event_id_occurred_at_idx ON engagement_svc.audit_log_2026_07 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2026_08_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2026_08_event_id_occurred_at_idx ON engagement_svc.audit_log_2026_08 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2026_09_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2026_09_event_id_occurred_at_idx ON engagement_svc.audit_log_2026_09 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2026_10_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2026_10_event_id_occurred_at_idx ON engagement_svc.audit_log_2026_10 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2026_11_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2026_11_event_id_occurred_at_idx ON engagement_svc.audit_log_2026_11 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2026_12_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2026_12_event_id_occurred_at_idx ON engagement_svc.audit_log_2026_12 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2027_01_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2027_01_event_id_occurred_at_idx ON engagement_svc.audit_log_2027_01 USING btree (event_id, occurred_at);


--
-- Name: audit_log_2027_02_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_2027_02_event_id_occurred_at_idx ON engagement_svc.audit_log_2027_02 USING btree (event_id, occurred_at);


--
-- Name: audit_log_default_event_id_occurred_at_idx; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX audit_log_default_event_id_occurred_at_idx ON engagement_svc.audit_log_default USING btree (event_id, occurred_at);


--
-- Name: event_failures_pending; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX event_failures_pending ON engagement_svc.event_failures USING btree (next_attempt_at) WHERE (status = 'pending'::text);


--
-- Name: idx_notif_status; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX idx_notif_status ON engagement_svc.notifications USING btree (status, created_at) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'failed'::character varying])::text[]));


--
-- Name: idx_notif_user; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX idx_notif_user ON engagement_svc.notifications USING btree (user_id);


--
-- Name: idx_notifications_retry; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX idx_notifications_retry ON engagement_svc.notifications USING btree (next_attempt_at) WHERE ((status)::text = ANY ((ARRAY['pending'::character varying, 'failed'::character varying, 'sending'::character varying])::text[]));


--
-- Name: idx_subscriptions_conference_active; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX idx_subscriptions_conference_active ON engagement_svc.subscriptions USING btree (conference_id) WHERE active;


--
-- Name: idx_support_ip_created; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX idx_support_ip_created ON engagement_svc.support_messages USING btree (ip_address, created_at DESC);


--
-- Name: idx_support_status; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE INDEX idx_support_status ON engagement_svc.support_messages USING btree (status, created_at DESC);


--
-- Name: subscription_criteria_unique; Type: INDEX; Schema: engagement_svc; Owner: -
--

CREATE UNIQUE INDEX subscription_criteria_unique ON engagement_svc.subscriptions USING btree (user_id, criteria_hash) WHERE (conference_id IS NULL);


--
-- Name: idx_evt_user; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE INDEX idx_evt_user ON identity_svc.email_verification_tokens USING btree (user_id);


--
-- Name: idx_evt_user_created; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE INDEX idx_evt_user_created ON identity_svc.email_verification_tokens USING btree (user_id, created_at DESC);


--
-- Name: idx_external_identities_user; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE INDEX idx_external_identities_user ON identity_svc.external_identities USING btree (user_id);


--
-- Name: idx_outbox_pending; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE INDEX idx_outbox_pending ON identity_svc.outbox_events USING btree (created_at) WHERE ((status)::text = 'pending'::text);


--
-- Name: idx_prt_user; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE INDEX idx_prt_user ON identity_svc.password_reset_tokens USING btree (user_id);


--
-- Name: idx_users_email_lower; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE UNIQUE INDEX idx_users_email_lower ON identity_svc.users USING btree (lower((email)::text)) WHERE (deleted_at IS NULL);


--
-- Name: idx_users_role; Type: INDEX; Schema: identity_svc; Owner: -
--

CREATE INDEX idx_users_role ON identity_svc.users USING btree (role) WHERE (deleted_at IS NULL);


--
-- Name: audit_log_2026_07_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2026_07_event_id_occurred_at_idx;


--
-- Name: audit_log_2026_07_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2026_07_pkey;


--
-- Name: audit_log_2026_08_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2026_08_event_id_occurred_at_idx;


--
-- Name: audit_log_2026_08_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2026_08_pkey;


--
-- Name: audit_log_2026_09_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2026_09_event_id_occurred_at_idx;


--
-- Name: audit_log_2026_09_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2026_09_pkey;


--
-- Name: audit_log_2026_10_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2026_10_event_id_occurred_at_idx;


--
-- Name: audit_log_2026_10_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2026_10_pkey;


--
-- Name: audit_log_2026_11_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2026_11_event_id_occurred_at_idx;


--
-- Name: audit_log_2026_11_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2026_11_pkey;


--
-- Name: audit_log_2026_12_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2026_12_event_id_occurred_at_idx;


--
-- Name: audit_log_2026_12_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2026_12_pkey;


--
-- Name: audit_log_2027_01_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2027_01_event_id_occurred_at_idx;


--
-- Name: audit_log_2027_01_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2027_01_pkey;


--
-- Name: audit_log_2027_02_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_2027_02_event_id_occurred_at_idx;


--
-- Name: audit_log_2027_02_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_2027_02_pkey;


--
-- Name: audit_log_default_event_id_occurred_at_idx; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.uq_audit_event_id ATTACH PARTITION engagement_svc.audit_log_default_event_id_occurred_at_idx;


--
-- Name: audit_log_default_pkey; Type: INDEX ATTACH; Schema: engagement_svc; Owner: -
--

ALTER INDEX engagement_svc.audit_log_pkey ATTACH PARTITION engagement_svc.audit_log_default_pkey;


--
-- Name: audit_log no_audit_delete; Type: RULE; Schema: engagement_svc; Owner: -
--

CREATE RULE no_audit_delete AS
    ON DELETE TO engagement_svc.audit_log DO INSTEAD NOTHING;


--
-- Name: audit_log no_audit_update; Type: RULE; Schema: engagement_svc; Owner: -
--

CREATE RULE no_audit_update AS
    ON UPDATE TO engagement_svc.audit_log DO INSTEAD NOTHING;


--
-- Name: applications trg_app_touch; Type: TRIGGER; Schema: application_svc; Owner: -
--

CREATE TRIGGER trg_app_touch BEFORE UPDATE ON application_svc.applications FOR EACH ROW EXECUTE FUNCTION application_svc.touch_updated_at();


--
-- Name: reviews trg_review_touch; Type: TRIGGER; Schema: application_svc; Owner: -
--

CREATE TRIGGER trg_review_touch BEFORE UPDATE ON application_svc.reviews FOR EACH ROW EXECUTE FUNCTION application_svc.touch_updated_at();


--
-- Name: conferences trg_conf_touch; Type: TRIGGER; Schema: conference_svc; Owner: -
--

CREATE TRIGGER trg_conf_touch BEFORE UPDATE ON conference_svc.conferences FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_updated_at();


--
-- Name: organizers trg_organizers_touch_conference; Type: TRIGGER; Schema: conference_svc; Owner: -
--

CREATE TRIGGER trg_organizers_touch_conference AFTER INSERT OR DELETE OR UPDATE ON conference_svc.organizers FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_parent_conference();


--
-- Name: schedule_slots trg_slots_touch_conference; Type: TRIGGER; Schema: conference_svc; Owner: -
--

CREATE TRIGGER trg_slots_touch_conference AFTER INSERT OR DELETE OR UPDATE ON conference_svc.schedule_slots FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_parent_conference();


--
-- Name: sections trg_sections_touch_conference; Type: TRIGGER; Schema: conference_svc; Owner: -
--

CREATE TRIGGER trg_sections_touch_conference AFTER INSERT OR DELETE OR UPDATE ON conference_svc.sections FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_parent_conference();


--
-- Name: registrations trg_registration_touch; Type: TRIGGER; Schema: conference_svc; Owner: -
--

CREATE TRIGGER trg_registration_touch BEFORE UPDATE ON conference_svc.registrations FOR EACH ROW EXECUTE FUNCTION conference_svc.touch_registration_updated_at();


--
-- Name: users trg_users_touch; Type: TRIGGER; Schema: identity_svc; Owner: -
--

CREATE TRIGGER trg_users_touch BEFORE UPDATE ON identity_svc.users FOR EACH ROW EXECUTE FUNCTION identity_svc.touch_updated_at();


--
-- Name: application_files application_files_application_id_fkey; Type: FK CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.application_files
    ADD CONSTRAINT application_files_application_id_fkey FOREIGN KEY (application_id) REFERENCES application_svc.applications(id) ON DELETE CASCADE;


--
-- Name: reviews reviews_application_id_fkey; Type: FK CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.reviews
    ADD CONSTRAINT reviews_application_id_fkey FOREIGN KEY (application_id) REFERENCES application_svc.applications(id) ON DELETE CASCADE;


--
-- Name: verification_documents verification_documents_verification_id_fkey; Type: FK CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.verification_documents
    ADD CONSTRAINT verification_documents_verification_id_fkey FOREIGN KEY (verification_id) REFERENCES application_svc.organizer_verifications(id) ON DELETE CASCADE;


--
-- Name: verification_role_jobs verification_role_jobs_verification_id_fkey; Type: FK CONSTRAINT; Schema: application_svc; Owner: -
--

ALTER TABLE ONLY application_svc.verification_role_jobs
    ADD CONSTRAINT verification_role_jobs_verification_id_fkey FOREIGN KEY (verification_id) REFERENCES application_svc.organizer_verifications(id);


--
-- Name: conference_counters conference_counters_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.conference_counters
    ADD CONSTRAINT conference_counters_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: conference_members conference_members_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.conference_members
    ADD CONSTRAINT conference_members_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: favorites favorites_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.favorites
    ADD CONSTRAINT favorites_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: join_link_access_log join_link_access_log_slot_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.join_link_access_log
    ADD CONSTRAINT join_link_access_log_slot_id_fkey FOREIGN KEY (slot_id) REFERENCES conference_svc.schedule_slots(id) ON DELETE CASCADE;


--
-- Name: organizers organizers_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.organizers
    ADD CONSTRAINT organizers_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: partners partners_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.partners
    ADD CONSTRAINT partners_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: registration_counters registration_counters_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registration_counters
    ADD CONSTRAINT registration_counters_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: registration_history registration_history_registration_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registration_history
    ADD CONSTRAINT registration_history_registration_id_fkey FOREIGN KEY (registration_id) REFERENCES conference_svc.registrations(id) ON DELETE CASCADE;


--
-- Name: registrations registrations_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.registrations
    ADD CONSTRAINT registrations_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: schedule_slots schedule_slots_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.schedule_slots
    ADD CONSTRAINT schedule_slots_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: schedule_slots schedule_slots_section_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.schedule_slots
    ADD CONSTRAINT schedule_slots_section_id_fkey FOREIGN KEY (section_id) REFERENCES conference_svc.sections(id) ON DELETE SET NULL;


--
-- Name: sections sections_conference_id_fkey; Type: FK CONSTRAINT; Schema: conference_svc; Owner: -
--

ALTER TABLE ONLY conference_svc.sections
    ADD CONSTRAINT sections_conference_id_fkey FOREIGN KEY (conference_id) REFERENCES conference_svc.conferences(id) ON DELETE CASCADE;


--
-- Name: email_verification_tokens email_verification_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.email_verification_tokens
    ADD CONSTRAINT email_verification_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES identity_svc.users(id) ON DELETE CASCADE;


--
-- Name: external_identities external_identities_user_id_fkey; Type: FK CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.external_identities
    ADD CONSTRAINT external_identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES identity_svc.users(id) ON DELETE CASCADE;


--
-- Name: password_reset_tokens password_reset_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: identity_svc; Owner: -
--

ALTER TABLE ONLY identity_svc.password_reset_tokens
    ADD CONSTRAINT password_reset_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES identity_svc.users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


