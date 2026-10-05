#!/usr/bin/env bash
# Delegate to the repository's single development lifecycle.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec bash "${ROOT_DIR}/scripts/dev/start.sh" -spark-workflow "$@"
