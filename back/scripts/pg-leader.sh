#!/usr/bin/env bash
# Печатает контейнер узла PostgreSQL, который принимает запись. Работает и с
# одиночным PostgreSQL, и с кластером Patroni, где лидером может быть любой узел.
set -euo pipefail

project="${COMPOSE_PROJECT:-confhub}"
[[ "$project" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Unsafe compose project" >&2; exit 2; }

for node in postgres postgres-2; do
    container="${project}-${node}-1"
    if [[ "$(docker exec "$container" psql -X -U postgres -Atc 'SELECT NOT pg_is_in_recovery()' 2>/dev/null)" == t ]]; then
        echo "$container"
        exit 0
    fi
done
echo "No writable PostgreSQL node in project ${project}" >&2
exit 1
