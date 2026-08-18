#!/usr/bin/env bash
# quality-postgres-gate.sh - Run Quality PostgreSQL integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-quality-postgres.XXXXXX")

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

database=${ADDP_TEST_POSTGRES_DATABASE:-addp_test}
case "$database" in
    *test*|*disposable*) ;;
    *)
        echo "ADDP_TEST_POSTGRES_DATABASE must identify a disposable test database" >&2
        exit 1
        ;;
esac

run_without_skips() {
    local package=$1
    local log_name
    log_name=$(printf '%s' "$package" | tr '/.' '__')
    local log_path="$WORK_DIR/$log_name.log"

    ADDP_POSTGRES_INTEGRATION=1 go test "$package" -run '^TestIntegrationPostgres' -count=1 -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "Quality PostgreSQL gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

cd "$ROOT_DIR/quality/backend"
for package in ./internal/migration ./internal/repository ./internal/service; do
    run_without_skips "$package"
done
