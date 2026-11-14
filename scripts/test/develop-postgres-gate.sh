#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=DEVELOP_POSTGRES_TEST_DSN
# develop-postgres-gate.sh - Run Develop owner-local PostgreSQL schema and catalog tests.

set -euo pipefail

source_root=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --source-root) source_root="${2:?--source-root requires a checkout path}"; shift 2 ;;
        *) echo 'usage: develop-postgres-gate.sh [--source-root checkout]' >&2; exit 2 ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_ROOT=$(cd "${source_root:-$ROOT_DIR}" && pwd -P)
source_checkout=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel)
source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD)
if [ "$source_checkout" != "$SOURCE_ROOT" ] ||
   ! git -C "$ROOT_DIR" merge-base --is-ancestor "$source_commit" HEAD ||
   ! [ -f "$SOURCE_ROOT/develop/backend/go.mod" ] ||
   ! grep -qx 'module github.com/addp/develop/backend' "$SOURCE_ROOT/develop/backend/go.mod"; then
    echo '--source-root must be an ADDP checkout whose commit belongs to this repository' >&2
    exit 2
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-develop-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${DEVELOP_POSTGRES_TEST_DSN:-}" ]; then
    echo "DEVELOP_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi
dsn_without_query=${DEVELOP_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$DEVELOP_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "DEVELOP_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
case "$database" in *test*|*disposable*) ;; *) echo "DEVELOP_POSTGRES_TEST_DSN must identify a disposable test database" >&2; exit 1 ;; esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$DEVELOP_POSTGRES_TEST_DSN"

cd "$SOURCE_ROOT/develop/backend"
go test ./internal/repository \
    -run '^(TestFileExecutionAgainstPostgres|TestCatalogDevTaskChangeFeedAgainstPostgres|TestExportSessionScopeAgainstPostgres|TestQueryExecutionQueueAgainstPostgres|TestQueryExecutionEventsAgainstPostgres|TestExecutionPolicyMigrationAgainstPostgres|TestQueryResultMigrationAgainstPostgres)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/develop.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/develop.log"; then
    echo "Develop PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

go test ./internal/service \
    -run '^(TestTableResultOwner.*AgainstPostgres|TestQueryFailureDiagnosticsAgainstPostgres|TestQueryResultPersistenceAgainstPostgres|TestWorkflowDisplayLanguageAgainstPostgres|TestWorkflowCommittedEffectsAgainstPostgres|TestQueryTraceAdmissionAgainstPostgres)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/develop-table-result.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/develop-table-result.log"; then
    echo "Develop table result PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
