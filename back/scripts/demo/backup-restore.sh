#!/usr/bin/env bash
# Резервное копирование и восстановление PostgreSQL:
# дамп -> потеря данных (удаление схемы) -> восстановление -> сверка.
#
# Ежедневную полную копию стенда делает scripts/backup.sh, проверку архива —
# scripts/restore.sh. Здесь тот же pg_dump/pg_restore показан на одной базе.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_lab
umask 077

dump="$(mktemp --suffix=.dump)"
stopped=()
cleanup() {
    # Сервисы запускаются обратно даже при ошибке восстановления.
    if (( ${#stopped[@]} )); then docker start "${stopped[@]}" >/dev/null || true; fi
    rm -f -- "$dump"
}
trap cleanup EXIT

inventory() {
    pg_sql <<'SQL'
SELECT 'identity_svc.users        ' || count(*) FROM identity_svc.users
UNION ALL SELECT 'conference_svc.conferences ' || count(*) FROM conference_svc.conferences
UNION ALL SELECT 'таблиц в схемах сервисов   ' || count(*) FROM pg_tables
    WHERE schemaname IN ('identity_svc', 'conference_svc', 'application_svc', 'engagement_svc');
SQL
}

step "1. Данные до копирования"
register_user "backup-$(date +%s)@demo.example.com" >/dev/null
before="$(inventory)"
echo "$before"

step "2. Резервная копия (pg_dump, custom format)"
leader="$(pg_member leader)"
started="$(now)"
docker exec "$(container "$leader")" pg_dump -d confhub -Fc > "$dump"
echo "Лидер: ${leader}; файл: $(du -h "$dump" | cut -f1), объектов: $(docker exec -i "$(container "$leader")" pg_restore --list < "$dump" | grep -vc '^;'), время: $(elapsed "$started") с"

step "3. Потеря данных: удаление схемы identity_svc"
pg_sql -c 'DROP SCHEMA identity_svc CASCADE' 2>&1 | tail -1
echo "Регистрация пользователя: HTTP $(register_user "lost-$(date +%s)@demo.example.com")"
echo "Таблиц в identity_svc: $(pg_sql -c "SELECT count(*) FROM pg_tables WHERE schemaname = 'identity_svc'")"

step "4. Восстановление (pg_restore)"
started="$(now)"
echo "Останавливаем сервисы: базу нельзя пересоздать при открытых соединениях"
for service in "${APP_SERVICES[@]}"; do
    docker stop "$(container "$service")" >/dev/null
    stopped+=("$(container "$service")")
done
# --create пересоздаёт базу целиком вместе с её правами доступа из копии.
docker exec -i "$(container "$leader")" pg_restore -d postgres --clean --if-exists --create --exit-on-error < "$dump"
docker start "${stopped[@]}" >/dev/null
stopped=()
for _ in $(seq 1 60); do
    if [[ "$(api_code /api/v1/conferences)" == 200 ]]; then break; fi
    sleep 1
done
restore_time="$(elapsed "$started")"
echo "Восстановлено и сервисы запущены за ${restore_time} с"

step "5. Сверка"
after="$(inventory)"
echo "$after"
if [[ "$before" != "$after" ]]; then echo "ДАННЫЕ ОТЛИЧАЮТСЯ" >&2; exit 1; fi
echo "Регистрация пользователя: HTTP $(register_user "restored-$(date +%s)@demo.example.com" 10)"
pg_cluster
echo
echo "Итог: данные совпадают с копией, восстановление заняло ${restore_time} с; standby получил восстановленные данные репликацией."
