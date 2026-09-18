#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ONTOLOGY_POSTGRES_TEST_DSN
# A local run uses addp_test; hosted CI owns addp_ontology_test as a disposable service.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
if [ -z "${ONTOLOGY_POSTGRES_TEST_DSN:-}" ]; then
    echo "ONTOLOGY_POSTGRES_TEST_DSN must select local addp_test or the dedicated CI test database" >&2
    exit 1
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-ontology-postgres.XXXXXX")
export ONTOLOGY_POSTGRES_TEST_RUN_ID
ONTOLOGY_POSTGRES_TEST_RUN_ID=$(python3 -c 'import uuid; print(uuid.uuid4())')
export GOWORK=off
cleanup() {
    local result=$?
    trap - EXIT INT TERM
    if ! go test ./internal/service -run '^TestPostgresGateCleanup$' -count=1 -timeout=30s -v > "$WORK_DIR/cleanup.log" 2>&1; then
        cat "$WORK_DIR/cleanup.log"
        result=1
    fi
    rm -rf "$WORK_DIR"
    exit "$result"
}
cd "$ROOT_DIR/ontology/backend"
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
go test ./internal/service -run '^TestPostgres(RevisionLifecycle|MigrationUpgrade)$' -count=1 -timeout=120s -v 2>&1 | tee "$WORK_DIR/tests.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then
    echo "Ontology PostgreSQL gate refuses skipped integration tests" >&2
    exit 1
fi
