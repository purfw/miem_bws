#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="${PROJECT_ROOT:-/srv/confhub/back}"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-confhub}"
ENV_FILE="$PROJECT_ROOT/.env.prod"
LOCK_FILE="${LOCK_FILE:-/run/lock/confhub-audit-maintenance.lock}"

[[ -f "$ENV_FILE" ]] || { echo "Missing $ENV_FILE" >&2; exit 1; }
exec 9>"$LOCK_FILE"
flock -n 9 || { echo "Audit maintenance is already running" >&2; exit 1; }

env_value() {
    sed -n "s/^${1}=//p" "$ENV_FILE" | tail -n 1
}

postgres_user="$(env_value POSTGRES_USER)"
postgres_db="$(env_value POSTGRES_DB)"
minio_user="$(env_value MINIO_ROOT_USER)"
minio_password="$(env_value MINIO_ROOT_PASSWORD)"
postgres_container="$(COMPOSE_PROJECT="$COMPOSE_PROJECT" "$(dirname "$0")/pg-leader.sh")"
[[ -n "$postgres_container" ]] || { echo "PostgreSQL container is not running" >&2; exit 1; }

psql_cmd=(docker exec -i "$postgres_container" psql -X -v ON_ERROR_STOP=1 -U "$postgres_user" -d "$postgres_db")

# Prepare partitions ahead of writes; the default partition covers missed runs.
"${psql_cmd[@]}" >/dev/null <<'SQL'
DO $$
DECLARE
    month_start date;
    month_end date;
    partition_name text;
BEGIN
    FOR offset_month IN 0..3 LOOP
        month_start := (date_trunc('month', current_date) + make_interval(months => offset_month))::date;
        month_end := (month_start + interval '1 month')::date;
        partition_name := 'audit_log_' || to_char(month_start, 'YYYY_MM');
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS engagement_svc.%I PARTITION OF engagement_svc.audit_log FOR VALUES FROM (%L) TO (%L)',
            partition_name, month_start, month_end
        );
    END LOOP;
END $$;
SQL

mapfile -t old_partitions < <("${psql_cmd[@]}" -Atc "
SELECT child.relname
FROM pg_inherits i
JOIN pg_class parent ON parent.oid=i.inhparent
JOIN pg_namespace pn ON pn.oid=parent.relnamespace
JOIN pg_class child ON child.oid=i.inhrelid
WHERE pn.nspname='engagement_svc'
  AND parent.relname='audit_log'
  AND child.relname ~ '^audit_log_[0-9]{4}_[0-9]{2}$'
  AND to_date(substring(child.relname from 11), 'YYYY_MM') + interval '1 month'
      <= date_trunc('month', current_date) - interval '12 months'
ORDER BY child.relname")

if (( ${#old_partitions[@]} == 0 )); then
    echo "Audit partitions are current; no archive candidates"
    exit 0
fi

# Running container of a compose service of this project.
service_container() {
    docker ps -q --filter status=running \
        --filter "label=com.docker.compose.project=${COMPOSE_PROJECT}" \
        --filter "label=com.docker.compose.service=$1" | head -n 1
}

archive_dir="$(mktemp -d /tmp/confhub-audit-archive.XXXXXX)"
cleanup() {
    case "$archive_dir" in
        /tmp/confhub-audit-archive.*) rm -rf -- "$archive_dir" ;;
    esac
}
trap cleanup EXIT

# MinIO is reachable only from the application segment, so mc joins the
# network namespace of application-svc.
app_container="$(service_container application-svc)"
[[ -n "$app_container" ]] || { echo "application-svc container is not running" >&2; exit 1; }
# mc ships in the MinIO server image: by default use exactly the image the
# running MinIO uses, so nothing floating is pulled.
mc_image="${MINIO_MC_IMAGE:-}"
if [[ -z "$mc_image" ]]; then
    minio_container="$(service_container minio)"
    [[ -n "$minio_container" ]] || { echo "minio container is not running; set MINIO_MC_IMAGE" >&2; exit 1; }
    mc_image="$(docker inspect -f '{{.Image}}' "$minio_container")"
fi

# Credentials go into an mc config file in the private temp directory instead of
# the command line or container environment (visible in ps / docker inspect).
json_escape() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    printf '%s' "$value"
}
mc_config_dir="$archive_dir/mc"
(
    umask 077
    mkdir -p "$mc_config_dir"
    printf '{"version":"10","aliases":{"confhub":{"url":"http://minio:9000","accessKey":"%s","secretKey":"%s","api":"s3v4","path":"auto"}}}\n' \
        "$(json_escape "$minio_user")" "$(json_escape "$minio_password")" > "$mc_config_dir/config.json"
)

mc() {
    docker run --rm --network "container:$app_container" \
        -v "$mc_config_dir:/mc-config" -v "$archive_dir:/archive:ro" \
        --entrypoint mc "$mc_image" --config-dir /mc-config "$@"
}

mc mb --ignore-existing confhub/audit-archive >/dev/null

for partition in "${old_partitions[@]}"; do
    [[ "$partition" =~ ^audit_log_[0-9]{4}_[0-9]{2}$ ]] || { echo "Unsafe partition name: $partition" >&2; exit 1; }
    archive_file="${partition}.csv.gz"
    docker exec "$postgres_container" psql -X -v ON_ERROR_STOP=1 -U "$postgres_user" -d "$postgres_db" \
        -c "\\copy (SELECT id,NULL::uuid AS event_id,event_type,NULL::uuid AS actor_id,entity_type,NULL::uuid AS entity_id,'{}'::jsonb AS payload,NULL::text AS ip_address,occurred_at FROM engagement_svc.${partition} ORDER BY occurred_at,id) TO STDOUT WITH CSV HEADER" \
        | gzip -9 > "$archive_dir/$archive_file"
    gzip -t "$archive_dir/$archive_file"

    year="${partition:10:4}"
    mc cp "/archive/$archive_file" "confhub/audit-archive/$year/$archive_file" >/dev/null
    mc stat "confhub/audit-archive/$year/$archive_file" >/dev/null

    # Drop only this partition after verifying its archive.
    "${psql_cmd[@]}" -v partition="$partition" >/dev/null <<'SQL'
SELECT CASE WHEN :'partition' ~ '^audit_log_[0-9]{4}_[0-9]{2}$' THEN 1 ELSE 1/0 END;
SELECT format('ALTER TABLE engagement_svc.audit_log DETACH PARTITION engagement_svc.%I', :'partition') \gexec
SELECT format('DROP TABLE engagement_svc.%I', :'partition') \gexec
SQL
    echo "Archived and detached $partition"
done
