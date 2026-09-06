#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# model-postgres-gate.sh - Run Model PostgreSQL integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-model-postgres.XXXXXX")

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

if [ -z "${ADDP_TEST_MODEL_POSTGRES_DSN:-}" ]; then
    echo "ADDP_TEST_MODEL_POSTGRES_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi

case "$ADDP_TEST_MODEL_POSTGRES_DSN" in
    postgres://*/*|postgresql://*/*) ;;
    *)
        echo "ADDP_TEST_MODEL_POSTGRES_DSN must use a PostgreSQL URL" >&2
        exit 1
        ;;
esac
dsn_without_query=${ADDP_TEST_MODEL_POSTGRES_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in
    addp_test|*disposable*) ;;
    *)
        echo "ADDP_TEST_MODEL_POSTGRES_DSN must use addp_test or an isolated disposable database" >&2
        exit 1
        ;;
esac

run_without_skips() {
    local package=$1
    local pattern=${2:-^TestPostgres}
    local log_name
    log_name=$(printf '%s' "$package" | tr '/.' '__')
    local log_path="$WORK_DIR/$log_name.log"

    go test "$package" -run "$pattern" -count=1 -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "Model PostgreSQL gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

cd "$ROOT_DIR/common"
export ADDP_TEST_EXECUTION_POSTGRES_DSN="$ADDP_TEST_MODEL_POSTGRES_DSN"
run_without_skips ./execution '^TestExecutionAuthorizationFactsMigrationAgainstPostgres$'

cd "$ROOT_DIR/model/backend"
run_without_skips ./internal/repository
run_without_skips ./internal/service
