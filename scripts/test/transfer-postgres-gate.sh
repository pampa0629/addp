#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_POSTGRES_PORT,ADDP_TEST_POSTGRES_PASSWORD
# transfer-postgres-gate.sh - Run non-spatial Transfer PostgreSQL schema, protected export, and target override integration tests (no PostGIS required).

set -euo pipefail

: "${ADDP_TEST_POSTGRES_PORT:?Set ADDP_TEST_POSTGRES_PORT to the verified PostgreSQL test service mapping}"
: "${ADDP_TEST_POSTGRES_PASSWORD:?Set ADDP_TEST_POSTGRES_PASSWORD for the PostgreSQL test service}"

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-transfer-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

database=${ADDP_TEST_POSTGRES_DATABASE:-addp_test}
case "$database" in
    *test*|*disposable*) ;;
    *)
        echo "ADDP_TEST_POSTGRES_DATABASE must identify a disposable test database" >&2
        exit 1
        ;;
esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_target "${ADDP_TEST_POSTGRES_HOST:-localhost}" "$ADDP_TEST_POSTGRES_PORT" "$database"

cd "$ROOT_DIR/transfer/backend"
ADDP_POSTGRES_INTEGRATION=1 \
    ADDP_TEST_POSTGRES_DATABASE="$database" \
    go test ./internal/repository ./internal/protection ./internal/planner \
    -run '^(TestIntegrationPostgresExecutionEventsRemoveRetiredText|TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite|TestIntegrationPlannerTargetOverrideAppendsOnlyToExistingPostgresTable)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/transfer.log"; then
    echo "Transfer PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
