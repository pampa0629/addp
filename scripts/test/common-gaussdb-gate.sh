#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=gaussdb
# Verified-media disposable docker run / docker rm lifecycle is delegated to the shared Business fixture.
# ADDP_T2_INPUT_FILES=business/gaussdb/ business/scripts/online-gaussdb-consumer-fixture.py scripts/lib/gaussdb-official-media.sh common/engine/plugins/gaussdb/
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export ADDP_GAUSSDB_FIXTURE_DIR
ADDP_GAUSSDB_FIXTURE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-gaussdb-t2.XXXXXX")
# macOS /var is a symlink; the fixture accepts only canonical private paths.
ADDP_GAUSSDB_FIXTURE_DIR=$(cd "$ADDP_GAUSSDB_FIXTURE_DIR" && pwd -P)
ARTIFACT_DIR=${ADDP_GAUSSDB_T2_ARTIFACT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/addp-gaussdb-t2-evidence.XXXXXX")}
mkdir -p "$ARTIFACT_DIR"
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  python3 "$ROOT_DIR/business/scripts/online-gaussdb-consumer-fixture.py" stop || status=1
  if [[ -f "$ADDP_GAUSSDB_FIXTURE_DIR/gaussdb-cleanup.json" ]]; then
    cp "$ADDP_GAUSSDB_FIXTURE_DIR/gaussdb-cleanup.json" "$ARTIFACT_DIR/cleanup.json"
  fi
  printf 'exit_code=%s\n' "$status" > "$ARTIFACT_DIR/result.txt"
  if [[ -f "$ADDP_GAUSSDB_FIXTURE_DIR/gaussdb-state.json" ]]; then
    echo "GaussDB T2 cleanup failed; private ownership directory retained" >&2
  else
    rm -rf "$ADDP_GAUSSDB_FIXTURE_DIR"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$ROOT_DIR/business/scripts/online-gaussdb-consumer-fixture.py" start
export ADDP_GAUSSDB_INTEGRATION=1
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/gaussdb -run '^TestIntegrationGaussDB' -count=1 -v) 2>&1 | tee "$ARTIFACT_DIR/provider.log"
! grep -q -- '--- SKIP:' "$ARTIFACT_DIR/provider.log"
grep -q -- '--- PASS: TestIntegrationGaussDB' "$ARTIFACT_DIR/provider.log"
