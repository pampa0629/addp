#!/usr/bin/env bash
# system-iam-postgres-gate.sh - Run destructive System IAM tests against a disposable PostgreSQL database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-system-iam-postgres.XXXXXX")

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

if [ -z "${ADDP_SYSTEM_POSTGRES_TEST_DSN:-}" ]; then
    echo "ADDP_SYSTEM_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi

cd "$ROOT_DIR/system/backend"

run_without_skips() {
    package=$1
    pattern=$2
    log_name=$(printf '%s' "$package" | tr '/.' '__')
    log_path="$WORK_DIR/$log_name.log"

    go test "$package" -run "$pattern" -count=1 -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "PostgreSQL release gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

run_without_skips ./internal/testsupport '^TestResetDisposablePostgresForGate$'
for package in ./internal/iam ./internal/iam/oauth ./internal/api ./internal/migration; do
    run_without_skips "$package" 'AgainstPostgres$'
done
