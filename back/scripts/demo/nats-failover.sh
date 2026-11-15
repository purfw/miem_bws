#!/usr/bin/env bash
# Отказ узла NATS JetStream: работа с лидером стрима, аварийное завершение
# (SIGKILL), выбор нового лидера среди реплик, возврат узла в кластер.
#
# Регистрация пользователя публикует событие user.registered: счётчик сообщений
# в стриме CONFHUB_EVENTS должен расти и до, и после отказа.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_lab

run_id="broker-$(date +%s)"
seq_no=0

show_stream() {
    local leader replicas messages current
    read -r leader replicas messages current < <(nats_stream)
    printf '  %s  лидер стрима: %-7s реплик: %s  сообщений: %-5s синхронные реплики: %s\n' \
        "$(stamp)" "$leader" "$replicas" "$messages" "$current"
}

publish_once() {
    seq_no=$((seq_no + 1))
    local code
    code="$(register_user "${run_id}-${seq_no}@demo.example.com")"
    printf '  %s  событие #%-3s (регистрация) HTTP %s\n' "$(stamp)" "$seq_no" "${code:-000}"
    [[ "$code" == 201 ]]
}

messages() { nats_stream | cut -d' ' -f3; }

step "1. Исходное состояние кластера NATS"
show_stream
read -r old_leader _ before _ < <(nats_stream)
[[ "$old_leader" != - ]] || { echo "У стрима нет лидера" >&2; exit 1; }

step "2. Работа с лидером: события публикуются и доставляются"
for _ in 1 2 3; do publish_once || true; sleep 1; done
sleep 2
show_stream

step "3. Аварийное завершение лидера стрима (${old_leader}, SIGKILL)"
victim="$(nats_service "$old_leader")"
docker kill -s KILL "$(container "$victim")" >/dev/null
killed_at="$(now)"
echo "  $(stamp)  контейнер $(container "$victim") убит"

step "4. Выбор нового лидера"
new_leader=-
for _ in $(seq 1 60); do
    new_leader="$(nats_stream | cut -d' ' -f1 || true)"
    if [[ -n "$new_leader" && "$new_leader" != - && "$new_leader" != "$old_leader" ]]; then break; fi
    sleep 0.5
done
[[ "$new_leader" != - && "$new_leader" != "$old_leader" ]] || { echo "Новый лидер не выбран" >&2; exit 1; }
echo "Новый лидер: ${new_leader}, выбран через $(elapsed "$killed_at") с после отказа."

step "5. Публикация после отказа"
at_failover="$(messages)"
for _ in 1 2 3 4 5; do publish_once || true; sleep 1; done
sleep 3
show_stream
after="$(messages)"
echo "Сообщений в стриме: до сценария ${before}, в момент отказа ${at_failover}, сейчас ${after}"
if (( after <= at_failover )); then echo "События после отказа не опубликованы" >&2; exit 1; fi

step "6. Возврат узла ${old_leader} в кластер"
docker start "$(container "$victim")" >/dev/null
for _ in $(seq 1 60); do
    current="$(nats_stream | cut -d' ' -f4 || true)"
    if [[ "$current" == *,* ]]; then break; fi
    sleep 1
done
show_stream
echo
echo "Итог: лидер стрима ${old_leader} -> ${new_leader}, публикация и доставка событий продолжились."
