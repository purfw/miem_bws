#!/usr/bin/env bash
# Выполняется Patroni один раз на первом лидере: создаёт базу, роли и схемы
# тем же SQL-файлом, что и одиночный PostgreSQL.
set -euo pipefail
: "${POSTGRES_DB:?POSTGRES_DB is required}"
[[ "$POSTGRES_DB" =~ ^[a-zA-Z0-9_]+$ ]] || { echo "Unsafe database name" >&2; exit 2; }
psql -X -v ON_ERROR_STOP=1 -d postgres -c "CREATE DATABASE ${POSTGRES_DB}"
psql -X -v ON_ERROR_STOP=1 -d "$POSTGRES_DB" -f /etc/confhub/postgres-init.sql
