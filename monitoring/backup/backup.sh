#!/usr/bin/env bash
set -euo pipefail
export PROJECT_ROOT="${PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
export BACKUP_ROOT="${BACKUP_ROOT:-/srv/confhub-backups}"
exec bash "$PROJECT_ROOT/back/scripts/backup.sh" "$@"
