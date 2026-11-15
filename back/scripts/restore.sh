#!/usr/bin/env bash
set -euo pipefail
umask 077

mode="${1:---check}"
archive="${2:-}"
[[ "$mode" == --check || "$mode" == --verify-db || "$mode" == --verify-stack ]] || { echo "Usage: $0 --check|--verify-db|--verify-stack /path/confhub-archive.tar.gz[.age]" >&2; exit 2; }
[[ -f "$archive" && ! -L "$archive" ]] || { echo "Regular backup archive required" >&2; exit 2; }
COMPOSE_PROJECT="${COMPOSE_PROJECT:-confhub}"
[[ "$COMPOSE_PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Unsafe compose project" >&2; exit 2; }
pg_container="$(COMPOSE_PROJECT="$COMPOSE_PROJECT" "$(dirname "$0")/pg-leader.sh")"

work_dir="$(mktemp -d /tmp/confhub-restore-check.XXXXXX)"
test_db=""
cleanup() {
    if [[ "$test_db" =~ ^confhub_restore_[0-9]+_[0-9]+$ ]]; then
        docker exec "$pg_container" dropdb -U postgres --force --if-exists "$test_db" >/dev/null
    fi
    case "$work_dir" in /tmp/confhub-restore-check.*) rm -rf -- "$work_dir" ;; esac
}
trap cleanup EXIT

if [[ "$archive" == *.age ]]; then
    age -d -i "${BACKUP_IDENTITY_FILE:-/srv/confhub-backup-keys/identity.key}" -o "$work_dir/decrypted.tar.gz" "$archive"
    archive="$work_dir/decrypted.tar.gz"
fi

tar -tzf "$archive" > "$work_dir/members"
while IFS= read -r member; do
    [[ "$member" != /* && "/$member/" != *"/../"* ]] || { echo "Unsafe archive path" >&2; exit 1; }
done < "$work_dir/members"

for member in postgres.dump deployment-config.tar.gz "${COMPOSE_PROJECT}_miniodata.tar.gz" "${COMPOSE_PROJECT}_natsdata.tar.gz"; do
    if grep -Fxq "./$member" "$work_dir/members"; then
        tar -xOzf "$archive" "./$member" > "$work_dir/$member"
    elif grep -Fxq "$member" "$work_dir/members"; then
        tar -xOzf "$archive" "$member" > "$work_dir/$member"
    else
        echo "Missing archive member: $member" >&2; exit 1
    fi
    if [[ "$member" == *.tar.gz ]]; then tar -tzf "$work_dir/$member" >/dev/null; fi
done
for member in letsencrypt-config.tar.gz host-nginx-config.tar.gz; do
    if grep -Fxq "./$member" "$work_dir/members"; then
        tar -xOzf "$archive" "./$member" > "$work_dir/$member"
        tar -tzf "$work_dir/$member" >/dev/null
    elif grep -Fxq "$member" "$work_dir/members"; then
        tar -xOzf "$archive" "$member" > "$work_dir/$member"
        tar -tzf "$work_dir/$member" >/dev/null
    fi
done
[[ "$(head -c 5 "$work_dir/postgres.dump")" == PGDMP ]] || { echo "Invalid PostgreSQL dump" >&2; exit 1; }
docker exec -i "$pg_container" pg_restore --list < "$work_dir/postgres.dump" >/dev/null
echo "Backup archive structure verified"

if [[ "$mode" == --verify-stack ]]; then
    python3 "$(dirname "$0")/verify-stack.py" "$archive" "$COMPOSE_PROJECT"
fi

if [[ "$mode" == --verify-db ]]; then
    test_db="confhub_restore_$(date -u +%Y%m%d%H%M%S)_$$"
    docker exec "$pg_container" createdb -U postgres -T template0 "$test_db"
    docker exec -i "$pg_container" pg_restore -U postgres -d "$test_db" --exit-on-error --no-owner --no-privileges < "$work_dir/postgres.dump"
    docker exec "$pg_container" psql -X -U postgres -d "$test_db" -v ON_ERROR_STOP=1 -c \
        "SELECT schemaname,count(*) AS tables FROM pg_tables WHERE schemaname IN ('identity_svc','conference_svc','application_svc','engagement_svc') GROUP BY schemaname ORDER BY schemaname"
    echo "PostgreSQL restored successfully into a temporary verification database; production was not changed"
fi
