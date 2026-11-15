#!/usr/bin/env bash
# Этап 3. Публикация сообщения в брокер и его потребление (NATS JetStream).
#
# identity-svc при регистрации пишет событие user.registered в таблицу outbox
# в той же транзакции, что и пользователя. Фоновый воркер публикует событие в
# стрим CONFHUB_EVENTS. engagement-svc получает его через своего потребителя (durable consumer),
# отмечает обработанным и отправляет письмо (на стенде письма ловит Mailpit).
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_lab

email="broker-$(date +%s)@demo.example.com"
consumer=engagement-user-registered

# Печатает: <сообщений в стриме> <потребителей> <доставлено потребителю> <подтверждено потребителем>
stream_state() {
    local node
    for node in "${NATS_NODES[@]}"; do
        running "$node" || continue
        docker exec "$(container "$node")" wget -qO- -T 3 'http://127.0.0.1:8222/jsz?streams=true&consumers=true' 2>/dev/null | python3 -c '
import json, sys
data = json.load(sys.stdin)
for account in data.get("account_details", []):
    for stream in account.get("stream_detail", []):
        for consumer in stream.get("consumer_detail") or []:
            if consumer["name"] == sys.argv[1]:
                print(stream["state"]["messages"], stream["state"]["consumer_count"],
                      consumer["delivered"]["consumer_seq"], consumer["ack_floor"]["consumer_seq"])
                sys.exit(0)
sys.exit(1)
' "$consumer" 2>/dev/null && return 0
    done
    return 1
}
mails() { docker exec "$(container mailpit)" wget -qO- "http://127.0.0.1:8025/api/v1/search?query=to:${email}" \
    | python3 -c 'import json, sys; print(json.load(sys.stdin)["messages_count"])'; }
show() {
    local messages consumers delivered acked
    read -r messages consumers delivered acked < <(stream_state)
    echo "  стрим CONFHUB_EVENTS: сообщений ${messages}, потребителей ${consumers}"
    echo "  потребитель ${consumer}: доставлено ${delivered}, подтверждено (ack) ${acked}"
    echo "  обработано событий в engagement-svc: $(pg_sql -c "SELECT count(*) FROM engagement_svc.processed_events")"
}

step "1. Состояние до публикации"
show

step "2. Публикация: регистрация пользователя ${email}"
echo "  POST /api/v1/auth/register: HTTP $(register_user "$email")"
sleep 3
pg_sql -c "SELECT format('  outbox identity-svc: событие %s, статус %s, опубликовано %s', event_type, status, to_char(published_at, 'HH24:MI:SS'))
    FROM identity_svc.outbox_events WHERE payload::text LIKE '%${email}%' ORDER BY created_at"

step "3. Потребление: engagement-svc получил событие"
show
pg_sql -c "SELECT format('  уведомление: %s, получатель %s, статус %s', type, recipient, status)
    FROM engagement_svc.notifications WHERE recipient='${email}'"
echo "  писем для ${email} в Mailpit: $(mails)"
