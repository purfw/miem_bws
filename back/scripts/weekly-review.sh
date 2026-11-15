#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="${PROJECT_ROOT:-/srv/confhub/back}"
ENV_FILE="$PROJECT_ROOT/.env.prod"
REPORT_DIR="${REPORT_DIR:-/var/log/confhub-audit}"

env_value() {
    sed -n "s/^${1}=//p" "$ENV_FILE" | tail -n 1
}

postgres_user="$(env_value POSTGRES_USER)"
postgres_db="$(env_value POSTGRES_DB)"
postgres_container="$(COMPOSE_PROJECT=confhub "$(dirname "$0")/pg-leader.sh")"
[[ -n "$postgres_container" ]] || { echo "PostgreSQL container is not running" >&2; exit 1; }

mkdir -p "$REPORT_DIR"
chmod 700 "$REPORT_DIR"
report="$REPORT_DIR/review-$(date -u +%Y%m%dT%H%M%SZ).txt"

{
    echo "ConfHub weekly operational review"
    echo "generated_at=$(date -Is)"
    echo
    echo "Audit events by type during the last 7 days:"
    docker exec "$postgres_container" psql -X -U "$postgres_user" -d "$postgres_db" -P pager=off -c \
        "SELECT event_type,count(*) FROM engagement_svc.audit_log WHERE occurred_at>=now()-interval '7 days' GROUP BY event_type ORDER BY count(*) DESC,event_type"
    echo
    echo "Actors with more than 100 changes during the last 7 days:"
    docker exec "$postgres_container" psql -X -U "$postgres_user" -d "$postgres_db" -P pager=off -c \
        "SELECT actor_id,count(*) FROM engagement_svc.audit_log WHERE occurred_at>=now()-interval '7 days' AND actor_id IS NOT NULL GROUP BY actor_id HAVING count(*)>100 ORDER BY count(*) DESC"
    echo
    echo "Pending/failed notifications:"
    docker exec "$postgres_container" psql -X -U "$postgres_user" -d "$postgres_db" -P pager=off -c \
        "SELECT status,count(*),min(created_at) AS oldest FROM engagement_svc.notifications WHERE status<>'sent' GROUP BY status ORDER BY status"
    echo
    echo "Unpublished/failed outbox events:"
    docker exec "$postgres_container" psql -X -U "$postgres_user" -d "$postgres_db" -P pager=off -c \
        "SELECT 'identity_svc' service,status,count(*) FROM identity_svc.outbox_events WHERE status<>'published' GROUP BY status UNION ALL SELECT 'conference_svc',status,count(*) FROM conference_svc.outbox_events WHERE status<>'published' GROUP BY status UNION ALL SELECT 'application_svc',status,count(*) FROM application_svc.outbox_events WHERE status<>'published' GROUP BY status ORDER BY service,status"
    echo
    echo "Long-running database queries over 30 seconds:"
    docker exec "$postgres_container" psql -X -U "$postgres_user" -d "$postgres_db" -P pager=off -c \
        "SELECT pid,usename,now()-query_start AS duration,left(query,200) AS query FROM pg_stat_activity WHERE state<>'idle' AND query_start<now()-interval '30 seconds' AND pid<>pg_backend_pid() ORDER BY query_start"
    echo
    echo "Filesystem usage:"
    df -h / /var/lib/docker 2>/dev/null || df -h /
} > "$report"

chmod 600 "$report"
find "$REPORT_DIR" -maxdepth 1 -type f -name 'review-*.txt' -mtime +400 -delete
logger -t confhub-weekly-review "Weekly review completed: $report"
echo "$report"
