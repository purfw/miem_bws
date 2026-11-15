#!/usr/bin/env bash
# Этап 4. Сегменты сети: подсети, мосты, состав и изоляция.
# Сценарий только читает состояние, его можно запускать и на рабочем стенде:
#   COMPOSE_PROJECT=confhub ./scripts/demo/segments.sh
set -euo pipefail
source "$(dirname "$0")/lib.sh"

SEGMENTS=(edge app data broker storage)
network() { echo "${PROJECT}_$1"; }

step "1. Сегменты проекта ${PROJECT}"
printf '  %-8s %-16s %-11s %-9s %s\n' segment subnet bridge internal containers
for segment in "${SEGMENTS[@]}"; do
    docker network inspect "$(network "$segment")" --format \
        "{{(index .IPAM.Config 0).Subnet}} {{index .Options \"com.docker.network.bridge.name\"}} {{.Internal}} {{len .Containers}}" \
        | { read -r subnet bridge internal count; printf '  %-8s %-16s %-11s %-9s %s\n' "$segment" "$subnet" "$bridge" "$internal" "$count"; }
done

step "2. Кто подключен к сегменту"
for segment in "${SEGMENTS[@]}"; do
    members="$(docker network inspect "$(network "$segment")" --format '{{range .Containers}}{{.Name}} {{end}}' \
        | tr ' ' '\n' | sed -e "s/^${PROJECT}-//" -e 's/-1$//' | sort | xargs)"
    printf '  %-8s %s\n' "$segment" "$members" | fold -s -w 110 | sed '2,$s/^/           /'
done

step "3. Изоляция: адреса и маршрут по умолчанию"
for service in nginx conference-svc postgres nats minio; do
    addresses="$(netns "$service" ip -4 -o addr show scope global | awk '{ print $4 }' | xargs)"
    route="$(netns "$service" ip route show default | awk '{ print "есть, через " $3 }')"
    printf '  %-15s адреса: %-52s маршрут наружу: %s\n' "$service" "$addresses" "${route:-нет}"
done
echo "  nginx не имеет адреса в data и broker; у БД, брокера и хранилища файлов нет маршрута наружу."
