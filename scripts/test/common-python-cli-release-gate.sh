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

"$VENV_PYTHON" -m pip install --disable-pip-version-check "$WHEEL[dev]" twine pipx==1.16.5
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

export PIPX_HOME="$WORK_DIR/pipx/home"
export PIPX_BIN_DIR="$WORK_DIR/pipx/bin"
export PIPX_MAN_DIR="$WORK_DIR/pipx/man"
export PIPX_DEFAULT_BACKEND=pip
PIPX_ADDP="$PIPX_BIN_DIR/addp"

"$VENV_PYTHON" -m pipx install "$WHEEL"
"$VENV_PYTHON" -m pipx install --force "$WHEEL"
PIPX_VERSION_JSON=$(cd "$WORK_DIR" && "$PIPX_ADDP" --version)
PIPX_LIST_JSON=$("$VENV_PYTHON" -m pipx list --json)
"$VENV_PYTHON" -c '
from pathlib import Path
import importlib.metadata
import json
import sys

version_payload = json.loads(sys.argv[1])
pipx_payload = json.loads(sys.argv[2])
wheel = Path(sys.argv[3]).resolve()
installed = importlib.metadata.version("addp-common")
main_package = pipx_payload["venvs"]["addp-common"]["metadata"]["main_package"]
if version_payload != {"name": "addp", "version": installed}:
    raise SystemExit(f"pipx entry point version mismatch: {version_payload!r} != {installed!r}")
if main_package["package"] != "addp-common" or main_package["package_version"] != installed:
    raise SystemExit(f"unexpected pipx package metadata: {main_package!r}")
apps = main_package["apps"]
if apps != ["addp"]:
    raise SystemExit(f"unexpected pipx apps: {apps!r}")
package_or_url = main_package["package_or_url"]
if Path(package_or_url).resolve() != wheel:
    raise SystemExit(f"pipx did not install the verified wheel: {package_or_url!r}")
' "$PIPX_VERSION_JSON" "$PIPX_LIST_JSON" "$WHEEL"

(
    cd "$WORK_DIR"
    PYTHONPATH= "$VENV_PYTHON" "$SOURCE_DIR/tests/cli_product_e2e.py" --addp "$PIPX_ADDP"
)

"$VENV_PYTHON" -m pipx uninstall addp-common
PIPX_LIST_JSON=$("$VENV_PYTHON" -m pipx list --json)
"$VENV_PYTHON" -c '
import json
import sys

payload = json.loads(sys.argv[1])
if payload.get("venvs") != {}:
    raise SystemExit(f"pipx environment was not removed: {payload!r}")
' "$PIPX_LIST_JSON"
if [ -e "$PIPX_ADDP" ]; then
    echo "pipx addp entry point remains after uninstall" >&2
    exit 1
fi

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
