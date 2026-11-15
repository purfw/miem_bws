#!/usr/bin/env bash
# Этап 3. Запись в кеш и чтение из кеша (Redis).
#
# conference-svc кеширует ответы поиска. Для новой строки поиска готового
# ответа в Redis нет: сервис берет данные из БД и сохраняет ответ в Redis.
# Повторный такой же запрос получает сохраненный ответ из Redis.
# Счетчики Redis: keyspace_misses - ключ искали и не нашли, keyspace_hits - ключ прочитан.
# Сценарий создает только ключ кеша с коротким сроком жизни, поэтому его можно
# запускать и на рабочем стенде: COMPOSE_PROJECT=confhub ./scripts/demo/cache.sh
set -euo pipefail
source "$(dirname "$0")/lib.sh"

redis() { docker exec "$(container redis)" redis-cli "$@" | tr -d '\r'; }
stat() { redis info stats | awk -F: -v key="$1" '$1 == key { print $2 }'; }
show() { echo "  ключей в Redis: $(redis dbsize); ключ прочитан (keyspace_hits): $(stat keyspace_hits); ключ не найден (keyspace_misses): $(stat keyspace_misses)"; }

query="demo$(date +%s)"
path="/api/v1/search?q=${query}"

step "1. Redis до запросов"
show
hits0="$(stat keyspace_hits)"; misses0="$(stat keyspace_misses)"

step "2. Первый запрос GET ${path}"
read -r code seconds < <(api_timed "$path")
echo "  HTTP ${code}, время ответа ${seconds} с"
hits1="$(stat keyspace_hits)"; misses1="$(stat keyspace_misses)"
echo "  ключ не найден: keyspace_misses +$((misses1 - misses0)), keyspace_hits +$((hits1 - hits0)). Ответ взят из БД и сохранен в Redis"

step "3. Сохраненный ответ в Redis"
redis --scan --pattern 'search:*' | while read -r key; do
    echo "  ${key}  срок жизни $(redis ttl "$key") с, размер $(redis strlen "$key") байт"
done

step "4. Повторный такой же запрос"
read -r code seconds < <(api_timed "$path")
echo "  HTTP ${code}, время ответа ${seconds} с"
hits2="$(stat keyspace_hits)"; misses2="$(stat keyspace_misses)"
echo "  ключ прочитан: keyspace_hits +$((hits2 - hits1)), keyspace_misses +$((misses2 - misses1)). Ответ получен из Redis, без запроса к БД"

step "5. Redis после запросов"
show
echo "  второй экземпляр Redis (сессии и токены), ключей: $(docker exec "$(container redis-state)" redis-cli dbsize | tr -d '\r')"
