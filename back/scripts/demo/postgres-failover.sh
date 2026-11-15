#!/usr/bin/env bash
# Отказ лидера PostgreSQL: работа с лидером, аварийное завершение (SIGKILL),
# автоматическое переключение на standby, возврат старого лидера репликой.
#
# Во время сценария каждую секунду регистрируется пользователь (запись в БД
# через приложение). В конце подтверждённые записи сверяются с базой.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_lab

run_id="failover-$(date +%s)"
ack_file="$(mktemp)"
trap 'rm -f -- "$ack_file"' EXIT
seq_no=0

write_once() {
    seq_no=$((seq_no + 1))
    local email="${run_id}-${seq_no}@demo.example.com" code
    code="$(register_user "$email")"
    if [[ "$code" == 201 ]]; then echo "$email" >> "$ack_file"; fi
    printf '  %s  запись #%-3s HTTP %s\n' "$(stamp)" "$seq_no" "${code:-000}"
    [[ "$code" == 201 ]]
}

step "1. Исходное состояние кластера"
pg_cluster
old_leader="$(pg_member leader)"
[[ -n "$old_leader" ]] || { echo "Кластер не готов" >&2; exit 1; }
echo "Лидер: ${old_leader}"

step "2. Работа с лидером: записи проходят"
for _ in 1 2 3; do write_once || true; sleep 1; done

step "3. Аварийное завершение лидера (${old_leader}, SIGKILL)"
docker kill -s KILL "$(container "$old_leader")" >/dev/null
killed_at="$(now)"
echo "  $(stamp)  контейнер $(container "$old_leader") убит"

step "4. Записи во время переключения"
recovered_after=""
for _ in $(seq 1 90); do
    if write_once; then
        recovered_after="$(elapsed "$killed_at")"
        break
    fi
    sleep 1
done
[[ -n "$recovered_after" ]] || { echo "Запись не восстановилась за 90 секунд" >&2; pg_cluster || true; exit 1; }
new_leader="$(pg_member leader)"
echo
echo "Новый лидер: ${new_leader}. Запись восстановилась через ${recovered_after} с после отказа."
for _ in 1 2 3; do write_once || true; sleep 1; done
pg_cluster

step "5. Потерянных данных нет"
acknowledged="$(wc -l < "$ack_file")"
stored="$(pg_sql -c "SELECT count(*) FROM identity_svc.users WHERE email LIKE '${run_id}-%'")"
echo "Подтверждено клиенту: ${acknowledged}, найдено в базе на новом лидере: ${stored}"
if (( stored < acknowledged )); then echo "ПОТЕРЯ ДАННЫХ" >&2; exit 1; fi

step "6. Возврат старого лидера (${old_leader}) в кластер"
docker start "$(container "$old_leader")" >/dev/null
for _ in $(seq 1 60); do
    if pg_cluster -f json | grep -q '"State": "streaming"'; then break; fi
    sleep 2
done
pg_cluster
echo
echo "Итог: лидер ${old_leader} -> ${new_leader}, простой записи ${recovered_after} с, потерь нет."
