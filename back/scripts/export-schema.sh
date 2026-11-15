#!/usr/bin/env bash
# Выгружает актуальную схему БД из запущенного стенда в db/schema.sql.
set -euo pipefail
cd "$(dirname "$0")/.."

PROJECT="${COMPOSE_PROJECT:-confhub}"
[[ "$PROJECT" =~ ^[a-zA-Z0-9_-]+$ ]] || { echo "Unsafe compose project" >&2; exit 2; }
target=db/schema.sql
mkdir -p db

{
    cat <<'HEADER'
-- Схема базы данных ConfHub (PostgreSQL 16).
--
-- Файл получен командой pg_dump --schema-only и нужен для чтения и проверки.
-- При развёртывании роли и схемы создаёт scripts/postgres-init.sql, а таблицы —
-- миграции сервисов (services/*/migrations), которые выполняются при старте.
-- Обновление файла: make schema

HEADER
    docker exec "$(COMPOSE_PROJECT="$PROJECT" scripts/pg-leader.sh)" pg_dump -U postgres -d confhub \
        --schema-only --no-owner --no-privileges | grep -Ev '^\\(un)?restrict '
} > "${target}.new"
mv "${target}.new" "$target"
echo "Schema written to ${target}: $(grep -c '^CREATE TABLE' "$target") tables"
