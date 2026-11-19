#!/usr/bin/env bash
# Отдельный стенд для демонстраций отказов: тот же compose, что и в production,
# плюс HA-оверлей, но свой проект, свои тома, свои подсети и свой .env.lab.
# Рабочий стенд (проект confhub) не затрагивается. CA для mTLS и ключ JWT
# общие с рабочим стендом (secrets/mtls, secrets/jwt_ed25519.key).
#
#   lab.sh up            собрать и запустить стенд, применить межсетевой экран
#   lab.sh down          остановить стенд (тома сохраняются)
#   lab.sh destroy       остановить стенд и удалить его тома
#   lab.sh compose ...   выполнить docker compose в контексте стенда
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT="${COMPOSE_PROJECT:-confhub-lab}"
export CONFHUB_NET_PREFIX="${CONFHUB_NET_PREFIX:-172.29}"
export CONFHUB_BR_PREFIX="${CONFHUB_BR_PREFIX:-chl}"
export HOST_PROXY_PORT="${HOST_PROXY_PORT:-18081}"
export JAEGER_UI_PORT="${JAEGER_UI_PORT:-16687}"
export MTLS_CERT_ROOT=secrets/mtls
ENV_FILE=.env.lab
COMPOSE=(docker compose -p "$COMPOSE_PROJECT" --env-file "$ENV_FILE"
    -f docker-compose.prod.yml -f docker-compose.build.yml -f docker-compose.host.yml -f docker-compose.ha.yml)

prepare() {
    bash scripts/mtls.sh
    if [[ ! -f secrets/jwt_ed25519.key ]]; then
        openssl genpkey -algorithm ED25519 -out secrets/jwt_ed25519.key
        openssl pkey -in secrets/jwt_ed25519.key -pubout -out secrets/jwt_ed25519.pub
    fi
    chgrp 0 secrets/jwt_ed25519.key
    chmod 640 secrets/jwt_ed25519.key
    chmod 644 secrets/jwt_ed25519.pub
    mkdir -p secrets/admin-auth secrets/certs
    if [[ ! -f "$ENV_FILE" ]]; then
        rand() { openssl rand -hex "${1:-24}"; }
        ( umask 077; cat > "$ENV_FILE" <<ENV
POSTGRES_DB=confhub
POSTGRES_USER=postgres
POSTGRES_PASSWORD=$(rand)
SVC_DB_PASSWORD=$(rand)
PG_REPLICATION_PASSWORD=$(rand)
SERVICE_TOKEN_SECRET=$(rand 32)
JOIN_URL_ENCRYPTION_KEY=$(openssl rand -base64 32)
MINIO_ROOT_USER=confhub
MINIO_ROOT_PASSWORD=$(rand)
MINIO_BUCKET=application-files
MAX_FILE_SIZE_BYTES=20971520
JWT_ISSUER=confhub
ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=168h
JWT_PRIVATE_KEY_FILE=./secrets/jwt_ed25519.key
JWT_PUBLIC_KEY_FILE=./secrets/jwt_ed25519.pub
TLS_CERTS_DIR=./secrets/certs
BASE_URL=http://127.0.0.1:${HOST_PROXY_PORT}
LOG_LEVEL=info
SMTP_HOST=mailpit
SMTP_PORT=1025
SMTP_FROM=noreply@confhub.example.com
SUPPORT_EMAIL=support@confhub.example.com
CI_REGISTRY_IMAGE=local
IMAGE_TAG=lab
ALLOWED_HOSTS=127.0.0.1
DJANGO_SECRET_KEY=$(rand 32)
ENV
        )
    fi
    docker volume create "${COMPOSE_PROJECT}_jaegerdata" >/dev/null
    docker run --rm --network none -v "${COMPOSE_PROJECT}_jaegerdata:/traces" alpine:3.21 chown 10001:10001 /traces
}

case "${1:-}" in
    up)
        prepare
        # Консоль администратора требует интерактивной настройки пароля и TOTP
        # (scripts/admin-auth.sh) и в сценариях отказов не участвует.
        "${COMPOSE[@]}" up -d --build --scale django-admin=0
        bash scripts/firewall.sh apply
        echo "Lab gateway: http://127.0.0.1:${HOST_PROXY_PORT}/"
        ;;
    down) "${COMPOSE[@]}" down ;;
    destroy)
        bash scripts/firewall.sh down || true
        "${COMPOSE[@]}" down -v
        ;;
    compose) shift; "${COMPOSE[@]}" "$@" ;;
    *) sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2 ;;
esac
