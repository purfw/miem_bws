#!/usr/bin/env bash
set -euo pipefail
umask 077

BACKUP_ROOT="${BACKUP_ROOT:-/srv/confhub-backups}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
PROJECT_ROOT="${PROJECT_ROOT:-/srv/confhub}"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-confhub}"
BACKUP_KEY_DIR="${BACKUP_KEY_DIR:-/srv/confhub-backup-keys}"
[[ "$COMPOSE_PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Unsafe compose project" >&2; exit 2; }
pg_container="$(COMPOSE_PROJECT="$COMPOSE_PROJECT" "$(dirname "$0")/pg-leader.sh")"
command -v age >/dev/null || { echo "Install age before creating backups" >&2; exit 2; }
command -v flock >/dev/null || { echo "flock is required" >&2; exit 2; }

mkdir -p "$BACKUP_ROOT"
chmod 700 "$BACKUP_ROOT"
exec 9>"${BACKUP_ROOT}/.backup.lock"
flock -n 9 || { echo "Another backup is running" >&2; exit 1; }
mkdir -p "$BACKUP_KEY_DIR"
chmod 700 "$BACKUP_KEY_DIR"
if [[ ! -f "$BACKUP_KEY_DIR/identity.key" ]]; then
    [[ ! -f "$BACKUP_KEY_DIR/recipients.txt" ]] || { echo "Backup identity missing; refusing key replacement" >&2; exit 1; }
    age-keygen -o "$BACKUP_KEY_DIR/identity.key" 2>/dev/null
fi
chmod 600 "$BACKUP_KEY_DIR/identity.key"
age-keygen -y "$BACKUP_KEY_DIR/identity.key" > "$BACKUP_KEY_DIR/recipients.txt"
chmod 600 "$BACKUP_KEY_DIR/recipients.txt"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive_name="confhub-${timestamp}.tar.gz.age"
work_dir="$(mktemp -d "${BACKUP_ROOT}/.confhub-backup.XXXXXX")"
archive_tmp="${BACKUP_ROOT}/.${archive_name}.tmp"
stopped=()
image_pins=()
restart_services() {
    if (( ${#stopped[@]} )); then
        docker start "${stopped[@]}" >/dev/null
        stopped=()
    fi
}

cleanup() {
    restart_services
    for pin in "${image_pins[@]}"; do
        if [[ "$pin" == "${COMPOSE_PROJECT}-backup:${timestamp}-"* ]]; then
            docker image rm "$pin" >/dev/null 2>&1 || true
        fi
    done
    case "$work_dir" in
        "${BACKUP_ROOT}"/.confhub-backup.*) rm -rf -- "$work_dir" ;;
    esac
    case "$archive_tmp" in
        "${BACKUP_ROOT}"/.confhub-*.tmp) rm -f -- "$archive_tmp" ;;
    esac
}
trap cleanup EXIT

# Optional services (mail sink, tracing) may be absent; the rest must exist.
optional_services=" mailpit jaeger "
for service in identity-svc conference-svc application-svc engagement-svc django-admin frontend postgres minio nats redis-state redis elasticsearch nginx mailpit jaeger; do
    if ! image_id="$(docker inspect -f '{{.Image}}' "${COMPOSE_PROJECT}-${service}-1" 2>/dev/null)"; then
        if [[ "$optional_services" == *" $service "* ]]; then
            echo "Warning: optional container ${COMPOSE_PROJECT}-${service}-1 not found; its image is not pinned" >&2
            continue
        fi
        echo "Container ${COMPOSE_PROJECT}-${service}-1 not found" >&2
        exit 1
    fi
    [[ "$image_id" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "Invalid image for $service" >&2; exit 1; }
    pin_tag="${COMPOSE_PROJECT}-backup:${timestamp}-${service}"
    docker image tag "$image_id" "$pin_tag"
    image_pins+=("$pin_tag")
    printf '%s %s\n' "$service" "$image_id" >> "$work_dir/images.txt"
done

for service in identity-svc conference-svc application-svc engagement-svc django-admin minio nats redis-state jaeger; do
    container="${COMPOSE_PROJECT}-${service}-1"
    if [[ "$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true)" == true ]]; then
        stopped+=("$container")
    fi
done
(( ${#stopped[@]} )) && docker stop --time 30 "${stopped[@]}" >/dev/null

docker exec "$pg_container" \
    pg_dump -U postgres -d confhub -Fc > "${work_dir}/postgres.dump"

for volume_name in "${COMPOSE_PROJECT}_miniodata" "${COMPOSE_PROJECT}_natsdata" "${COMPOSE_PROJECT}_redisstatedata" "${COMPOSE_PROJECT}_jaegerdata"; do
    if [[ "$volume_name" == *redisstatedata || "$volume_name" == *jaegerdata ]] && ! docker volume inspect "$volume_name" >/dev/null 2>&1; then continue; fi
    docker volume inspect "$volume_name" >/dev/null
    docker run --rm \
        -v "${volume_name}:/source:ro" \
        -v "${work_dir}:/backup" \
        alpine:3.21 \
        tar -czf "/backup/${volume_name}.tar.gz" -C /source .
done

tar -czf "${work_dir}/deployment-config.tar.gz" \
    --exclude=back/secrets/admin-credentials.txt \
    --exclude='*/__pycache__' \
    --exclude='*/.pytest_cache' \
    --exclude='*/.ruff_cache' \
    -C "$PROJECT_ROOT" \
    back/docker-compose.prod.yml \
    back/docker-compose.build.yml \
    back/docker-compose.host.yml \
    back/docker-compose.ha.yml \
    back/postgres-ha \
    back/firewall \
    back/nginx \
    back/observability \
    back/systemd \
    back/scripts \
    back/.env.prod \
    back/secrets

restart_services

docker image save "${image_pins[@]}" | gzip -1 > "$work_dir/images.tar.gz"

if [[ -d /etc/letsencrypt ]]; then
    tar -czf "${work_dir}/letsencrypt-config.tar.gz" -C / etc/letsencrypt
fi

if [[ -d /etc/nginx ]]; then
    tar -czf "${work_dir}/host-nginx-config.tar.gz" -C / etc/nginx
fi

archive_members=(postgres.dump deployment-config.tar.gz images.txt images.tar.gz)
for member in "${COMPOSE_PROJECT}_miniodata.tar.gz" "${COMPOSE_PROJECT}_natsdata.tar.gz" "${COMPOSE_PROJECT}_redisstatedata.tar.gz" "${COMPOSE_PROJECT}_jaegerdata.tar.gz" letsencrypt-config.tar.gz host-nginx-config.tar.gz; do
    if [[ -f "$work_dir/$member" ]]; then archive_members+=("$member"); fi
done
tar -czf "$work_dir/archive.tar.gz" -C "$work_dir" -- "${archive_members[@]}"
tar -tzf "$work_dir/archive.tar.gz" >/dev/null
age -R "$BACKUP_KEY_DIR/recipients.txt" -o "$archive_tmp" "$work_dir/archive.tar.gz"
chmod 600 "$archive_tmp"
age -d -i "$BACKUP_KEY_DIR/identity.key" "$archive_tmp" | tar -tzf - >/dev/null
mv "$archive_tmp" "${BACKUP_ROOT}/${archive_name}"

find "$BACKUP_ROOT" -maxdepth 1 -type f -name 'confhub-*.tar.gz.age' \
    -mtime "+${RETENTION_DAYS}" -delete

echo "$(date -Is) backup created: ${BACKUP_ROOT}/${archive_name}"
