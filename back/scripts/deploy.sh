#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

bash scripts/mtls.sh
docker volume create confhub_jaegerdata >/dev/null
docker run --rm --network none -v confhub_jaegerdata:/traces alpine:3.21 chown 10001:10001 /traces

COMPOSE=(docker compose -p confhub -f docker-compose.prod.yml -f docker-compose.build.yml)

if [[ -n "${HOST_PROXY_PORT:-}" ]]; then
    COMPOSE+=(-f docker-compose.host.yml)
fi

# CONFHUB_HA=1 включает отказоустойчивый контур (Patroni + etcd, кластер NATS).
# Перенос данных одиночного PostgreSQL описан в docs/RELIABILITY.md.
if [[ "${CONFHUB_HA:-}" == 1 ]]; then
    COMPOSE+=(-f docker-compose.ha.yml)
fi

if [[ ! -f secrets/jwt_ed25519.key ]]; then
    mkdir -p secrets
    openssl genpkey -algorithm ED25519 -out secrets/jwt_ed25519.key
    openssl pkey -in secrets/jwt_ed25519.key -pubout -out secrets/jwt_ed25519.pub
    chmod 600 secrets/jwt_ed25519.key
    echo "JWT keys generated."
fi

# Container users read the key through supplementary group 0.
chgrp 0 secrets/jwt_ed25519.key
chmod 640 secrets/jwt_ed25519.key
chmod 644 secrets/jwt_ed25519.pub

if [[ ! -f secrets/certs/fullchain.pem ]]; then
    mkdir -p secrets/certs
    CN="${PUBLIC_HOST:-91.132.58.253}"
    if [[ "$CN" =~ ^[0-9a-fA-F:.]+$ ]]; then
        SAN="IP:${CN}"
    else
        SAN="DNS:${CN}"
    fi
    openssl req -x509 -newkey rsa:2048 -days 365 -nodes \
        -keyout secrets/certs/privkey.pem \
        -out secrets/certs/fullchain.pem \
        -subj "/CN=${CN}" \
        -addext "subjectAltName=${SAN}" 2>/dev/null || \
    openssl req -x509 -newkey rsa:2048 -days 365 -nodes \
        -keyout secrets/certs/privkey.pem \
        -out secrets/certs/fullchain.pem \
        -subj "/CN=${CN}"
    echo "Self-signed certificate for ${CN} generated."
fi

if [[ ! -f .env.prod ]]; then
    rand() { openssl rand -hex "${1:-24}"; }
    HOST="${PUBLIC_HOST:-91.132.58.253}"
    umask 077
    cat > .env.prod <<EOF
POSTGRES_DB=confhub
POSTGRES_USER=postgres
POSTGRES_PASSWORD=$(rand 24)
SVC_DB_PASSWORD=$(rand 24)
SERVICE_TOKEN_SECRET=$(rand 32)
JOIN_URL_ENCRYPTION_KEY=$(openssl rand -base64 32)

MINIO_ROOT_USER=confhub
MINIO_ROOT_PASSWORD=$(rand 24)
MINIO_BUCKET=application-files
MAX_FILE_SIZE_BYTES=20971520

JWT_ISSUER=confhub
ACCESS_TOKEN_TTL=15m
REFRESH_TOKEN_TTL=168h
JWT_PRIVATE_KEY_FILE=./secrets/jwt_ed25519.key
JWT_PUBLIC_KEY_FILE=./secrets/jwt_ed25519.pub
TLS_CERTS_DIR=./secrets/certs

BASE_URL=https://${HOST}
LOG_LEVEL=info

SMTP_HOST=mailpit
SMTP_PORT=1025
SMTP_FROM=noreply@confhub.example.com
SUPPORT_EMAIL=support@confhub.example.com

CI_REGISTRY_IMAGE=local
IMAGE_TAG=latest

DJANGO_SETTINGS_MODULE=conf_admin.settings
ALLOWED_HOSTS=${HOST}
DJANGO_SECRET_KEY=$(rand 32)
EOF
    echo ".env.prod generated."
fi

if [[ -f secrets/admin-auth/credentials.enc ]] || grep -q '^ADMIN_USERNAME=' .env.prod; then
    bash scripts/admin-auth.sh migrate .env.prod
else
    bash scripts/admin-auth.sh setup .env.prod
fi

if [[ "${CONFHUB_HA:-}" == 1 ]] && ! grep -q '^PG_REPLICATION_PASSWORD=' .env.prod; then
    echo "PG_REPLICATION_PASSWORD=$(openssl rand -hex 24)" >> .env.prod
fi

"${COMPOSE[@]}" --env-file .env.prod config >/dev/null
echo "NOTE: mailpit is the default mail sink; replace SMTP_* in .env.prod for real mail." >&2

"${COMPOSE[@]}" --env-file .env.prod up -d --build

# Межсетевой экран между контейнерами: default deny + правила firewall/policy.conf.
if [[ "$(id -u)" == "0" ]] && command -v nft >/dev/null 2>&1; then
    bash scripts/firewall.sh apply
else
    echo "WARNING: nftables is not available; network policy was NOT applied (apt install nftables)." >&2
fi

# Refresh read-only grants for tables added by migrations.
docker exec -i "$(COMPOSE_PROJECT=confhub bash scripts/pg-leader.sh)" sh -c '
psql -X -v ON_ERROR_STOP=1 -U "${POSTGRES_USER:-postgres}" -d "$POSTGRES_DB" <<SQL
GRANT USAGE ON SCHEMA identity_svc, conference_svc, application_svc, engagement_svc TO django_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA identity_svc, conference_svc, application_svc, engagement_svc TO django_reader;
SQL
' >/dev/null

if [[ "$(id -u)" == "0" ]] && command -v systemctl >/dev/null 2>&1; then
    install -m 0644 systemd/confhub-backup.service systemd/confhub-backup.timer \
        systemd/confhub-audit-maintenance.service systemd/confhub-audit-maintenance.timer \
        systemd/confhub-weekly-review.service systemd/confhub-weekly-review.timer \
        systemd/confhub-key-rotation.service systemd/confhub-key-rotation.timer \
        systemd/confhub-service-keys.service systemd/confhub-service-keys.timer \
        systemd/confhub-firewall.service \
        /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable confhub-firewall.service >/dev/null
    systemctl enable --now confhub-backup.timer confhub-audit-maintenance.timer \
        confhub-weekly-review.timer confhub-key-rotation.timer confhub-service-keys.timer >/dev/null
fi

echo
echo "Stack is up. Internal API operations: python3 scripts/internal-call.py --help"
if [[ -n "${HOST_PROXY_PORT:-}" ]]; then
    echo "Gateway is listening on 127.0.0.1:${HOST_PROXY_PORT}; configure the host reverse proxy for HTTPS."
else
    echo "Open https://${PUBLIC_HOST:-91.132.58.253}/ (self-signed cert warning is expected)."
fi
