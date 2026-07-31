#!/usr/bin/env bash
# common-python-cli-release-gate.sh - Build and verify the installable ADDP CLI product.
#
# Usage: bash scripts/test/common-python-cli-release-gate.sh

set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
    echo "ADDP CLI release gate currently requires macOS with a real Keychain" >&2
    exit 1
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_DIR="$ROOT_DIR/common-python"
PYTHON=${PYTHON:-python3}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-cli-release.XXXXXX")

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

"$PYTHON" -m pip wheel --disable-pip-version-check --no-deps --wheel-dir "$WORK_DIR/dist" "$SOURCE_DIR"
WHEEL=$(find "$WORK_DIR/dist" -maxdepth 1 -type f -name 'addp_common-*.whl' -print -quit)
if [ -z "$WHEEL" ]; then
    echo "addp-common wheel was not built" >&2
    exit 1
fi

"$PYTHON" -m venv "$WORK_DIR/venv"
VENV_PYTHON="$WORK_DIR/venv/bin/python"
VENV_ADDP="$WORK_DIR/venv/bin/addp"

"$VENV_PYTHON" -m pip install --disable-pip-version-check "$WHEEL" pytest pytest-asyncio twine
"$VENV_PYTHON" -m twine check "$WHEEL"
"$VENV_PYTHON" -c '
from pathlib import Path
import addp_common
import sys

package_path = Path(addp_common.__file__).resolve()
environment_path = Path(sys.prefix).resolve()
if not package_path.is_relative_to(environment_path):
    raise SystemExit(f"addp_common was not imported from the fresh environment: {package_path}")
'
(
    cd "$WORK_DIR"
    PYTHONPATH= "$VENV_PYTHON" -m pytest --rootdir "$WORK_DIR" -q "$SOURCE_DIR/tests"
)

VERSION_JSON=$(cd "$WORK_DIR" && "$VENV_ADDP" --version)
"$VENV_PYTHON" -c '
import importlib.metadata
import json
import sys

payload = json.loads(sys.argv[1])
installed = importlib.metadata.version("addp-common")
if payload != {"name": "addp", "version": installed}:
    raise SystemExit(f"entry point version mismatch: {payload!r} != {installed!r}")
' "$VERSION_JSON"

(
    cd "$WORK_DIR"
    PYTHONPATH= "$VENV_PYTHON" "$SOURCE_DIR/tests/cli_product_e2e.py" --addp "$VENV_ADDP"
)

if [ -n "${ADDP_CLI_RELEASE_DIST:-}" ]; then
    mkdir -p "$ADDP_CLI_RELEASE_DIST"
    RELEASE_FILENAME=$(basename "$WHEEL")
    RELEASE_WHEEL="$ADDP_CLI_RELEASE_DIST/$RELEASE_FILENAME"
    cp "$WHEEL" "$RELEASE_WHEEL"
    (
        cd "$ADDP_CLI_RELEASE_DIST"
        shasum -a 256 "$RELEASE_FILENAME" > "$RELEASE_FILENAME.sha256"
    )
fi
