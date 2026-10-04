#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_POSTGRES_PORT,ADDP_TEST_POSTGRES_PASSWORD
# common-postgres-gate.sh - Run Common PostgreSQL Engine Provider integration tests.

set -euo pipefail

test_group=all
while [ "$#" -gt 0 ]; do
    case "$1" in
        --test)
            case "${2:-}" in
                all|query-read-set) test_group="$2"; shift 2 ;;
                *) echo "usage: $0 [--test all|query-read-set]" >&2; exit 2 ;;
            esac
            ;;
        *) echo "usage: $0 [--test all|query-read-set]" >&2; exit 2 ;;
    esac
done

: "${ADDP_TEST_POSTGRES_PORT:?Set ADDP_TEST_POSTGRES_PORT to the verified PostgreSQL test service mapping}"
: "${ADDP_TEST_POSTGRES_PASSWORD:?Set ADDP_TEST_POSTGRES_PASSWORD for the PostgreSQL test service}"

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

database=${ADDP_TEST_POSTGRES_DATABASE:-addp_test}
case "$database" in
    *test*|*disposable*) ;;
    *)
        echo "ADDP_TEST_POSTGRES_DATABASE must identify addp_test or an isolated disposable database" >&2
        exit 1
        ;;
esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_target "${ADDP_TEST_POSTGRES_HOST:-localhost}" "$ADDP_TEST_POSTGRES_PORT" "$database"
if [ -n "${ADDP_TEST_EXECUTION_POSTGRES_DSN:-}" ]; then
    addp_infra_verify_test_postgres_dsn "$ADDP_TEST_EXECUTION_POSTGRES_DSN"
fi

cd "$ROOT_DIR/common"
test_pattern='^(TestIntegrationPostgresCatalogReadOnlyPrimaryKey|TestIntegrationPostgresPrepareTableWrite|TestIntegrationPostgresTableResult|TestIntegrationPostgresAnalyticalInstance|TestIntegrationPostgresAnalyticalText|TestIntegrationPostgresAnalyticalDateBuckets|TestIntegrationPostgresAnalyticalScan|TestIntegrationPostgresAnalyticalRelations|TestIntegrationPostgresAnalyticalCalendar|TestIntegrationPostgresAnalyticalExpressions|TestIntegrationPostgresAnalyticalArithmetic|TestIntegrationPostgresLosslessAnalyticalInteger|TestIntegrationPostgresAnalyticalResultAssertions|TestIntegrationPostgresBoundedWatermarkResumeAndIdempotentUpsert|TestIntegrationPostgresReadBatchHonorsSpatialEncoding|TestIntegrationPostgresQueryReadSessionBindsExactNumericText|TestIntegrationResolvePostgresQuery(ReadSet|OutputLineage))'
if [ "$test_group" = query-read-set ]; then
    test_pattern='^TestIntegrationResolvePostgresQuery(ReadSet|OutputLineage)'
fi
ADDP_POSTGRES_INTEGRATION=1 \
    go test ./engine/plugins/postgresql \
    -run "$test_pattern" \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-postgres.log"; then
    echo "Common PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

if [ "$test_group" = query-read-set ]; then
    echo "Common PostgreSQL query-read-set group passed (not the full gate)"
    exit 0
fi

postgres_host=${ADDP_TEST_POSTGRES_HOST:-localhost}
postgres_port=$ADDP_TEST_POSTGRES_PORT
postgres_user=${ADDP_TEST_POSTGRES_USER:-addp}
postgres_password=$ADDP_TEST_POSTGRES_PASSWORD
postgres_sslmode=${ADDP_TEST_POSTGRES_SSLMODE:-disable}
execution_dsn=${ADDP_TEST_EXECUTION_POSTGRES_DSN:-postgres://${postgres_user}:${postgres_password}@${postgres_host}:${postgres_port}/${database}?sslmode=${postgres_sslmode}}

ADDP_TEST_EXECUTION_POSTGRES_DSN="$execution_dsn" \
    go test ./execution \
    -run '^TestExecution(AuthorizationFactsMigration|Events)AgainstPostgres$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-execution-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-execution-postgres.log"; then
    echo "Common execution PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN="$execution_dsn" \
    go test ./dataprotection/projectionstore \
    -run '^TestProjectionStore.*AgainstPostgres$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-projectionstore-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-projectionstore-postgres.log"; then
    echo "Common protection projection store PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

ADDP_TEST_TABLE_RESULT_POSTGRES_DSN="$execution_dsn" \
    go test ./dbbridge -run '^TestTableResult.*AgainstPostgres$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-table-result-postgres.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/common-table-result-postgres.log"; then
    echo "Common table result PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
