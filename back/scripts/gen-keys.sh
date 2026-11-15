#!/usr/bin/env bash
# Ed25519 keypair for JWTs. Run once per environment.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)/secrets"
mkdir -p "$DIR"

if [[ -f "$DIR/jwt_ed25519.key" && -f "$DIR/jwt_ed25519.pub" ]]; then
    echo "Keys already exist at $DIR. Skipping. Delete to regenerate."
    exit 0
fi

openssl genpkey -algorithm ED25519 -out "$DIR/jwt_ed25519.key"
openssl pkey -in "$DIR/jwt_ed25519.key" -pubout -out "$DIR/jwt_ed25519.pub"
chmod 600 "$DIR/jwt_ed25519.key"
echo "Wrote $DIR/jwt_ed25519.{key,pub}"
