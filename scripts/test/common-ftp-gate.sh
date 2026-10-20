#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=ftp
# ADDP_T2_INPUT_FILES=business/ftp/ business/docker-compose.yml common/engine/certification/ftp/ common/engine/internal/ftpclient/
# Business owner: disposable docker run / docker rm resources, never personal services.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ADDP_FTP_ADMISSION_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-ftp-admission.XXXXXX")
export ADDP_FTP_ADMISSION_DIR
ADDP_FTP_ADMISSION_DIR=$(cd "$ADDP_FTP_ADMISSION_DIR" && pwd -P)
ARTIFACT_DIR=${ADDP_FTP_ARTIFACT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/addp-ftp-evidence.XXXXXX")}
mkdir -p "$ARTIFACT_DIR"
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    python3 "$ROOT_DIR/business/ftp/admission.py" stop || status=1
    for evidence in facts.json cleanup.json; do
        [ ! -f "$ADDP_FTP_ADMISSION_DIR/$evidence" ] || cp "$ADDP_FTP_ADMISSION_DIR/$evidence" "$ARTIFACT_DIR/$evidence"
    done
    printf 'exit_code=%s\n' "$status" > "$ARTIFACT_DIR/result.txt"
    if [ ! -f "$ADDP_FTP_ADMISSION_DIR/state.json" ]; then
        rm -rf "$ADDP_FTP_ADMISSION_DIR"
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$ROOT_DIR/business/ftp/admission.py" start
export ADDP_FTP_INTEGRATION=1
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/certification/ftp ./engine/internal/ftpclient -run '^(TestAdmission|TestIntegration|TestStrict)' -count=1 -v) 2>&1 | tee "$ARTIFACT_DIR/admission.log"
! grep -q -- '--- SKIP:' "$ARTIFACT_DIR/admission.log"
for required in CatalogTLSAndReadOnly RESTRangeAndConcurrentIsolation; do
    grep -q -- "--- PASS: TestAdmission${required} " "$ARTIFACT_DIR/admission.log" || exit 1
done

grep -q -- "--- PASS: TestIntegrationPASVUsesControlPeer " "$ARTIFACT_DIR/admission.log"
grep -q -- "--- PASS: TestStrictMLSTFacts " "$ARTIFACT_DIR/admission.log"
