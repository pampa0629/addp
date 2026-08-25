#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLED_PYTHON="${HOME}/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3"

if [[ -x "${BUNDLED_PYTHON}" ]]; then
  PYTHON_BIN="${BUNDLED_PYTHON}"
elif command -v python3 >/dev/null 2>&1; then
  PYTHON_BIN="$(command -v python3)"
else
  echo "错误：未找到 Python 3。" >&2
  exit 1
fi

exec "${PYTHON_BIN}" "${SCRIPT_DIR}/publish.py" "$@"
