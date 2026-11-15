#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
command_name="${1:-setup}"
env_file="${2:-.env.prod}"
case "$command_name" in
    setup|migrate|add|list|role|disable|enable) ;;
    *) echo "Usage: $0 setup|migrate|add|list|role|disable|enable .env.prod|.env [--username NAME] [--roles ROLES]" >&2; exit 1 ;;
esac
case "$env_file" in
    .env|.env.prod) ;;
    *) echo "The environment file must be .env or .env.prod in back/." >&2; exit 1 ;;
esac
[[ -f "$env_file" ]] || { echo "$env_file does not exist." >&2; exit 1; }

docker build -q -t confhub-admin-setup ./services/django-admin >/dev/null
docker_args=(run --rm --user 0 --network none -v "$PWD:/config")
if [[ "$command_name" == "setup" || "$command_name" == "add" ]]; then
    [[ -t 0 && -t 1 ]] || { echo "Setup requires an interactive terminal." >&2; exit 1; }
    docker_args+=(-it)
fi
docker "${docker_args[@]}" --entrypoint python confhub-admin-setup \
    -m console.credentials_tool "$command_name" --env-file "/config/$env_file" "${@:3}"
