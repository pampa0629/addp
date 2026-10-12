#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ONTOLOGY_POSTGRES_TEST_DSN
# A local run uses addp_test; hosted CI owns addp_ontology_test as a disposable service.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
RECOVERY_RUN_ID=''
if [ "$#" -ne 0 ]; then
    if [ "$#" -ne 2 ] || [ "$1" != --cleanup-run ] ||
        ! [[ "$2" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
        echo "Usage: ontology-postgres-gate.sh [--cleanup-run <canonical Run ID UUID>]" >&2
        exit 1
    fi
    RECOVERY_RUN_ID="$2"
fi
if [ -z "${ONTOLOGY_POSTGRES_TEST_DSN:-}" ]; then
    echo "ONTOLOGY_POSTGRES_TEST_DSN must select local addp_test or the dedicated CI test database" >&2
    exit 1
fi
# The shared Infra check accepts CI-owned databases generally. This owner
# still permits only its dedicated test database, including in recovery mode.
python3 -c '
import os
from urllib.parse import unquote, urlsplit

value = urlsplit(os.environ["ONTOLOGY_POSTGRES_TEST_DSN"])
allowed = {"addp_test"}
if os.environ.get("GITHUB_ACTIONS") == "true":
    allowed.add("addp_ontology_test")
if value.hostname not in {"localhost", "127.0.0.1", "::1"} or unquote(value.path.lstrip("/")) not in allowed:
    raise SystemExit("Unsafe Ontology PostgreSQL test target")
'
source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$ONTOLOGY_POSTGRES_TEST_DSN"

export GOWORK=off
if [ -n "$RECOVERY_RUN_ID" ]; then
    export ONTOLOGY_POSTGRES_TEST_RUN_ID="$RECOVERY_RUN_ID"
    cd "$ROOT_DIR/ontology/backend"
    # No fresh fixture or second exit cleanup: the same owner function holds
    # the same advisory lock and refuses any marker other than this exact run.
    exec go test ./internal/service -run '^TestPostgresGateCleanup$' -count=1 -timeout=30s -v
fi

WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-ontology-postgres.XXXXXX")
export ONTOLOGY_POSTGRES_TEST_RUN_ID
ONTOLOGY_POSTGRES_TEST_RUN_ID=$(python3 -c 'import uuid; print(uuid.uuid4())')
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
go test ./internal/service -run '^TestPostgres(CleanupSafety|RevisionLifecycle|MigrationUpgrade)$' -count=1 -timeout=120s -v 2>&1 | tee "$WORK_DIR/tests.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then
    echo "Ontology PostgreSQL gate refuses skipped integration tests" >&2
    exit 1
fi
