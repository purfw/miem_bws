#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="${PROJECT_ROOT:-/srv/confhub/back}"
cd "$PROJECT_ROOT"

ENV_FILE="$PROJECT_ROOT/.env.prod"
SECRETS_DIR="$PROJECT_ROOT/secrets"
LOCK_FILE="${LOCK_FILE:-/run/lock/confhub-key-rotation.lock}"
rotation_mode="${1:---all}"
[[ "$rotation_mode" == --all || "$rotation_mode" == --service-only ]] || { echo "Usage: rotate-keys.sh [--all|--service-only]" >&2; exit 2; }

if [[ ! -f "$ENV_FILE" || ! -f "$SECRETS_DIR/jwt_ed25519.key" || ! -f "$SECRETS_DIR/jwt_ed25519.pub" ]]; then
    echo "ConfHub production environment or JWT keys are missing" >&2
    exit 1
fi

exec 9>"$LOCK_FILE"
flock -n 9 || { echo "ConfHub key rotation is already running" >&2; exit 1; }

COMPOSE=(docker compose -p confhub --env-file "$ENV_FILE" -f docker-compose.prod.yml -f docker-compose.build.yml)
SERVICES=(identity-svc conference-svc application-svc engagement-svc django-admin)
work_dir="$(mktemp -d "$SECRETS_DIR/.rotation.XXXXXX")"
cp "$ENV_FILE" "$work_dir/env.backup"
cp "$SECRETS_DIR/jwt_ed25519.key" "$work_dir/jwt.key.backup"
cp "$SECRETS_DIR/jwt_ed25519.pub" "$work_dir/jwt.pub.backup"

cleanup() {
    case "$work_dir" in
        "$SECRETS_DIR"/.rotation.*) rm -rf -- "$work_dir" ;;
    esac
}
trap cleanup EXIT

env_value() {
    sed -n "s/^${1}=//p" "$ENV_FILE" | tail -n 1
}

set_env() {
    local key="$1" value="$2" tmp
    tmp="$(mktemp "$PROJECT_ROOT/.env.prod.XXXXXX")"
    awk -v key="$key" -v value="$value" '
        BEGIN { found=0 }
        index($0, key "=") == 1 {
            if (!found) print key "=" value
            found=1
            next
        }
        { print }
        END { if (!found) print key "=" value }
    ' "$ENV_FILE" > "$tmp"
    chmod 600 "$tmp"
    mv "$tmp" "$ENV_FILE"
}

rollback() {
    local status=$?
    trap - ERR
    set +e
    cp "$work_dir/env.backup" "$ENV_FILE"
    cp "$work_dir/jwt.key.backup" "$SECRETS_DIR/jwt_ed25519.key"
    cp "$work_dir/jwt.pub.backup" "$SECRETS_DIR/jwt_ed25519.pub"
    chgrp 0 "$SECRETS_DIR/jwt_ed25519.key"
    chmod 640 "$SECRETS_DIR/jwt_ed25519.key"
    chmod 644 "$SECRETS_DIR/jwt_ed25519.pub"
	"${COMPOSE[@]}" up -d --no-deps --force-recreate "${SERVICES[@]}"
    echo "Key rotation failed and previous keys were restored" >&2
    exit "$status"
}
trap rollback ERR

bash scripts/mtls.sh
"${COMPOSE[@]}" stop "${SERVICES[@]}"
set_env SERVICE_TOKEN_SECRET "$(openssl rand -hex 32)"

if [[ "$rotation_mode" == --all ]]; then
legacy_key="$(env_value JOIN_URL_ENCRYPTION_KEY)"
keyring="$(env_value JOIN_URL_ENCRYPTION_KEYS)"
active_version="$(env_value JOIN_URL_ACTIVE_KEY_VERSION)"
active_version="${active_version:-1}"
if [[ ! "$active_version" =~ ^[0-9]+$ ]] || (( active_version < 1 || active_version >= 32767 )); then
    echo "Invalid JOIN_URL_ACTIVE_KEY_VERSION" >&2
    exit 1
fi
if [[ -z "$keyring" ]]; then
    [[ -n "$legacy_key" ]] || { echo "JOIN_URL_ENCRYPTION_KEY is empty" >&2; exit 1; }
    keyring="1:${legacy_key}"
fi
new_version=$((active_version + 1))
new_join_key="$(openssl rand -base64 32 | tr -d '\n')"
set_env JOIN_URL_ENCRYPTION_KEYS "${keyring},${new_version}:${new_join_key}"
set_env JOIN_URL_ACTIVE_KEY_VERSION "$new_version"

openssl genpkey -algorithm ED25519 -out "$work_dir/jwt_ed25519.key"
openssl pkey -in "$work_dir/jwt_ed25519.key" -pubout -out "$work_dir/jwt_ed25519.pub"
mkdir -p "$SECRETS_DIR/jwt-history"
cp "$SECRETS_DIR/jwt_ed25519.pub" "$SECRETS_DIR/jwt-history/jwt-$(date -u +%Y%m%dT%H%M%SZ).pub"
mv "$work_dir/jwt_ed25519.key" "$SECRETS_DIR/jwt_ed25519.key"
mv "$work_dir/jwt_ed25519.pub" "$SECRETS_DIR/jwt_ed25519.pub"
chgrp 0 "$SECRETS_DIR/jwt_ed25519.key"
chmod 640 "$SECRETS_DIR/jwt_ed25519.key"
chmod 644 "$SECRETS_DIR/jwt_ed25519.pub"
fi

"${COMPOSE[@]}" config >/dev/null
"${COMPOSE[@]}" up -d --no-deps --force-recreate "${SERVICES[@]}"

for service in "${SERVICES[@]}"; do
    container_id="$("${COMPOSE[@]}" ps -q "$service")"
    [[ -n "$container_id" ]] || { echo "$service container is missing" >&2; false; }
    healthy=false
    for _ in $(seq 1 30); do
        status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id")"
        if [[ "$status" == "healthy" || "$status" == "running" ]]; then
            healthy=true
            break
        fi
        sleep 2
    done
    [[ "$healthy" == true ]] || { echo "$service did not become healthy" >&2; false; }
done

trap - ERR
if [[ "$rotation_mode" == --all ]]; then
    echo "ConfHub JWT, interservice HMAC and join-link keys rotated; join key version ${new_version}"
else
    echo "ConfHub interservice HMAC rotated and mTLS certificates checked"
fi
