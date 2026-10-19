#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=starrocks
# ADDP_T2_INPUT_FILES=business/starrocks/ common/engine/certification/starrocks/
# The Business owner manages disposable docker run / docker rm resources.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ADDP_STARROCKS_ADMISSION_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-starrocks-admission.XXXXXX")
export ADDP_STARROCKS_ADMISSION_DIR
ADDP_STARROCKS_ADMISSION_DIR=$(cd "$ADDP_STARROCKS_ADMISSION_DIR" && pwd -P)
ARTIFACT_DIR=${ADDP_STARROCKS_ARTIFACT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/addp-starrocks-evidence.XXXXXX")}
mkdir -p "$ARTIFACT_DIR"
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    python3 "$ROOT_DIR/business/starrocks/admission.py" stop || status=1
    for evidence in facts.json cleanup.json image-build.json schema-initialization.json; do
        [ ! -f "$ADDP_STARROCKS_ADMISSION_DIR/$evidence" ] || cp "$ADDP_STARROCKS_ADMISSION_DIR/$evidence" "$ARTIFACT_DIR/$evidence"
    done
    printf 'exit_code=%s\n' "$status" > "$ARTIFACT_DIR/result.txt"
    if [ ! -f "$ADDP_STARROCKS_ADMISSION_DIR/state.json" ]; then
        rm -rf "$ADDP_STARROCKS_ADMISSION_DIR"
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$ROOT_DIR/business/starrocks/admission.py" start
export ADDP_STARROCKS_INTEGRATION=1
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/certification/starrocks -run '^TestAdmission' -count=1 -v) 2>&1 | tee "$ARTIFACT_DIR/admission.log"
! grep -q -- '--- SKIP:' "$ARTIFACT_DIR/admission.log"
for required in PreparedReadPrecisionAndLeastPrivilege ValidatedStatementAndNativeTransactionBoundary NativeVisibleCatalogAndFieldFacts NativeDecimalProtocolLengths; do
    grep -q -- "--- PASS: TestAdmission${required} " "$ARTIFACT_DIR/admission.log" || {
        echo "StarRocks admission requires the actual ${required} test" >&2
        exit 1
    }
done
python3 - "$ARTIFACT_DIR/admission-policy.json" <<'POLICY'
import json, sys
from pathlib import Path
Path(sys.argv[1]).write_text(json.dumps({
    'result': 'passed',
    'read_only_boundary': 'validated_statement',
    'database_read_only_transaction': False,
    'provider_implemented': False,
    'scope': 'native protocol, shared SQL guard, ordinary read transaction and visible catalog',
}, indent=2))
POLICY
