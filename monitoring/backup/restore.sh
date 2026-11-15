#!/usr/bin/env bash
set -euo pipefail
project_root="${PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
exec bash "$project_root/back/scripts/restore.sh" "$@"
