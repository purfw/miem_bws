#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/.env"

rand() { openssl rand -hex "${1:-24}"; }

if [[ -f "$ENV_FILE" ]]; then
    chmod 600 "$ENV_FILE"
    if ! grep -q '^DJANGO_SECRET_KEY=.' "$ENV_FILE"; then
        printf '\nDJANGO_SECRET_KEY=%s\n' "$(rand 32)" >> "$ENV_FILE"
    fi
    if [[ -f "$ROOT/secrets/admin-auth/credentials.enc" ]] || grep -q '^ADMIN_USERNAME=' "$ENV_FILE"; then
        bash "$ROOT/scripts/admin-auth.sh" migrate .env
    else
        bash "$ROOT/scripts/admin-auth.sh" setup .env
    fi
    echo ".env exists at $ENV_FILE. Existing service secrets were kept."
    exit 0
fi

umask 077
cat > "$ENV_FILE" <<EOF
POSTGRES_PASSWORD=$(rand 24)
SVC_DB_PASSWORD=$(rand 24)
SERVICE_TOKEN_SECRET=$(rand 32)
JOIN_URL_ENCRYPTION_KEY=$(openssl rand -base64 32)

MINIO_ROOT_USER=confhub
MINIO_ROOT_PASSWORD=$(rand 24)

SUPPORT_EMAIL=support@confhub.example.com

DJANGO_SECRET_KEY=$(rand 32)
EOF

bash "$ROOT/scripts/admin-auth.sh" setup .env

echo "Wrote $ENV_FILE with generated secrets."
echo "They are applied to the database roles only on a fresh volume:"
echo "changing SVC_DB_PASSWORD later requires ALTER ROLE ... PASSWORD."
echo "Administrator authentication is stored encrypted; no plaintext password file is created."
