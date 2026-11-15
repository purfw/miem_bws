#!/usr/bin/env bash
set -euo pipefail
COMPOSE_PROJECT="${COMPOSE_PROJECT:-confhub}"
[[ "$COMPOSE_PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || exit 2
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
database="confhub_test_$(date -u +%Y%m%d%H%M%S)_$$"
postgres_container="${COMPOSE_PROJECT}-postgres-1"
nats_container="confhub-test-nats-$(date -u +%Y%m%d%H%M%S)-$$"
redis_container="confhub-test-redis-$(date -u +%Y%m%d%H%M%S)-$$"
cleanup() {
    [[ "$database" =~ ^confhub_test_[0-9]+_[0-9]+$ ]] || return
    docker exec "$postgres_container" dropdb -U postgres --force --if-exists "$database" >/dev/null
    if [[ "$nats_container" =~ ^confhub-test-nats-[0-9]+-[0-9]+$ ]]; then
        docker rm -f "$nats_container" >/dev/null 2>&1 || true
    fi
    if [[ "$redis_container" =~ ^confhub-test-redis-[0-9]+-[0-9]+$ ]]; then
        docker rm -f "$redis_container" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT
docker exec "$postgres_container" createdb -U postgres -T template0 "$database"
docker exec "$postgres_container" psql -X -U postgres -d "$database" -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto; CREATE EXTENSION IF NOT EXISTS pg_trgm;' >/dev/null
for service in identity-svc conference-svc application-svc engagement-svc; do
    for migration in "$project_root/services/$service/migrations/"*.up.sql; do
        docker exec -i "$postgres_container" psql -X -U postgres -d "$database" -v ON_ERROR_STOP=1 < "$migration" >/dev/null
    done
done
postgres_password="$(docker exec "$postgres_container" sh -c 'printf "%s" "$POSTGRES_PASSWORD"')"
encoded_password="$(printf '%s' "$postgres_password" | python3 -c 'import sys; from urllib.parse import quote; print(quote(sys.stdin.read(), safe=""))')"
export TEST_DATABASE_URL="postgres://postgres:${encoded_password}@postgres:5432/${database}?sslmode=disable"
nats_image="$(docker inspect --format '{{.Image}}' "${COMPOSE_PROJECT}-nats-1")"
docker run -d --name "$nats_container" --network "${COMPOSE_PROJECT}_default" "$nats_image" -js >/dev/null
export TEST_NATS_URL="nats://${nats_container}:4222"
redis_image="$(docker inspect --format '{{.Image}}' "${COMPOSE_PROJECT}-redis-state-1")"
docker run -d --name "$redis_container" --network "${COMPOSE_PROJECT}_default" "$redis_image" >/dev/null
export TEST_REDIS_URL="redis://${redis_container}:6379/0"
docker run --rm --network "${COMPOSE_PROJECT}_default" \
    --env TEST_DATABASE_URL \
    --env TEST_NATS_URL \
    --env TEST_REDIS_URL \
    -v "$project_root:/workspace:ro" \
    -v "${COMPOSE_PROJECT}_integration_go_cache:/root/.cache/go-build" \
    -v "${COMPOSE_PROJECT}_integration_go_modules:/go/pkg/mod" \
    -w /workspace golang:1.27.3-alpine \
    sh -c 'go test -coverprofile=/tmp/confhub.cover ./shared/... ./services/identity-svc/... ./services/conference-svc/... ./services/application-svc/... ./services/engagement-svc/... && go tool cover -func=/tmp/confhub.cover | tail -n 1'
echo "Integration tests passed; the temporary test database is removed on exit"
