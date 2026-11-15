#!/usr/bin/env bash
# Этап 4. Ошибки при запрете нужного (легитимного) правила.
#
# Снимаем разрешение conference-svc -> PostgreSQL (оба узла). Каталог перестает
# отвечать, проверка готовности сервиса сообщает об ошибке, остальные сервисы
# работают. После возврата правила каталог снова отвечает.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_lab
firewall="$(dirname "$0")/../firewall.sh"
export COMPOSE_PROJECT="$PROJECT"
trap '"$firewall" restore >/dev/null' EXIT

ready() {
    local answer
    if answer="$(docker exec "$(container conference-svc)" wget -qO /dev/null -T 5 http://127.0.0.1:8082/readyz 2>&1)"; then
        echo 200
    else
        grep -o '[0-9][0-9][0-9] .*' <<<"$answer" || echo "нет ответа"
    fi
}
state() {
    echo "  HTTP $(api_code /api/v1/conferences 75)  каталог, GET /api/v1/conferences"
    echo "  HTTP $(ready)  готовность conference-svc, GET /readyz"
    echo "  HTTP $(api_code /api/v1/auth/me)  identity-svc, GET /api/v1/auth/me без токена"
}

step "1. Полная политика: сервис работает"
state

step "2. Снимаем правило conference-svc -> postgres и postgres-2"
"$firewall" revoke conference-svc postgres >/dev/null
"$firewall" revoke conference-svc postgres-2 | tail -3

step "3. Без правила: ошибки"
state

step "4. Счетчики отброшенных пакетов"
"$firewall" rules | grep revoked

step "5. Возвращаем правило"
"$firewall" restore | head -1
for _ in $(seq 1 30); do
    [[ "$(api_code /api/v1/conferences 5)" == 200 ]] && break
    sleep 1
done
state
