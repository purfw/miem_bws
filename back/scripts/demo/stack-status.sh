#!/usr/bin/env bash
# Этап 2. Состав стенда: контейнеры из docker compose, версии установленных
# БД, кеша и брокера, ответы фронта и сервисов через шлюз.
# Сценарий только читает состояние, его можно запускать и на рабочем стенде:
#   COMPOSE_PROJECT=confhub ./scripts/demo/stack-status.sh
set -euo pipefail
source "$(dirname "$0")/lib.sh"

step "1. Контейнеры проекта ${PROJECT} (docker compose)"
docker ps --filter "label=com.docker.compose.project=${PROJECT}" \
    --format '{{.Label "com.docker.compose.service"}}\t{{.Image}}\t{{.Status}}' | sort | column -t -s $'\t' | sed 's/^/  /'
total="$(docker ps -q --filter "label=com.docker.compose.project=${PROJECT}" | wc -l)"
healthy="$(docker ps -q --filter "label=com.docker.compose.project=${PROJECT}" --filter health=healthy | wc -l)"
echo "Всего контейнеров: ${total}, из них с проверкой состояния healthy: ${healthy}"

step "2. Установленные БД, кеш и брокер"
echo "  БД:     $(docker exec "$(container postgres)" psql -V)"
echo "  кеш:    $(docker exec "$(container redis)" redis-server -v | cut -d' ' -f1-3)"
echo "  брокер: $(docker exec "$(container nats)" nats-server -v)"

step "3. Ответы через шлюз ${GATEWAY}"
check() { echo "  HTTP $(api_code "$2")  $1"; }
check "фронтенд, GET /" /
check "identity-svc, GET /healthz/identity" /healthz/identity
check "conference-svc, GET /healthz/conference" /healthz/conference
check "application-svc, GET /healthz/application" /healthz/application
check "engagement-svc, GET /healthz/engagement" /healthz/engagement
check "каталог, GET /api/v1/conferences" /api/v1/conferences
