#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_INPUT_FILES=graph/backend/
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_POSTGRES_PORT,ADDP_TEST_POSTGRES_PASSWORD
# common-postgres-gate.sh - Run Common PostgreSQL Engine Provider integration tests.

set -euo pipefail

test_group=all
highgo_native=false
source_root=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --test)
            case "${2:-}" in
                all|query-read-set|typed-write|highgo-write|graph|projection-store) test_group="$2"; shift 2 ;;
                *) echo "usage: $0 [--test all|query-read-set|typed-write|highgo-write|graph|projection-store] [--highgo-native] [--source-root checkout]" >&2; exit 2 ;;
            esac
            ;;
        --highgo-native) highgo_native=true; shift ;;
        --source-root)
            [ -n "${2:-}" ] || { echo '--source-root requires a checkout path' >&2; exit 2; }
            source_root="$2"; shift 2 ;;
        *) echo "usage: $0 [--test all|query-read-set|typed-write|highgo-write|graph|projection-store] [--highgo-native] [--source-root checkout]" >&2; exit 2 ;;
    esac
done

if [ "$highgo_native" = true ]; then
    [ "$test_group" = highgo-write ] || { echo "--highgo-native requires --test highgo-write" >&2; exit 2; }
    for required in ADDP_TEST_HIGHGO_HOST ADDP_TEST_HIGHGO_PORT ADDP_TEST_HIGHGO_DATABASE ADDP_TEST_HIGHGO_USER ADDP_TEST_HIGHGO_PASSWORD; do
        [ -n "${!required:-}" ] || { echo "$required is required for native HighGo verification" >&2; exit 2; }
    done
else
    : "${ADDP_TEST_POSTGRES_PORT:?Set ADDP_TEST_POSTGRES_PORT to the verified PostgreSQL test service mapping}"
    : "${ADDP_TEST_POSTGRES_PASSWORD:?Set ADDP_TEST_POSTGRES_PASSWORD for the PostgreSQL test service}"
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_ROOT="$ROOT_DIR"
if [ -n "$source_root" ]; then
    SOURCE_ROOT=$(cd "$source_root" && pwd -P) || exit 1
    source_checkout=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel 2>/dev/null) || { echo '--source-root must be an ADDP checkout' >&2; exit 1; }
    source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD 2>/dev/null) || { echo '--source-root must have a source commit' >&2; exit 1; }
    if [ "$source_checkout" != "$SOURCE_ROOT" ] ||
       ! git -C "$ROOT_DIR" merge-base --is-ancestor "$source_commit" HEAD ||
       ! [ -f "$SOURCE_ROOT/common/go.mod" ] ||
       ! grep -qx 'module github.com/addp/common' "$SOURCE_ROOT/common/go.mod"; then
        echo '--source-root must be an ADDP checkout whose commit belongs to this repository' >&2
        exit 1
    fi
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ "$highgo_native" = true ]; then
    cd "$SOURCE_ROOT/common"
    ADDP_HIGHGO_INTEGRATION=1 ADDP_POSTGRES_INTEGRATION=0 \
        go test ./engine/plugins/highgo -run '^TestIntegrationHighGoTableWrite$' \
        -count=1 -v 2>&1 | tee "$WORK_DIR/highgo-native.log"
    if grep -q -- '--- SKIP:' "$WORK_DIR/highgo-native.log"; then
        echo "Native HighGo verification refuses skipped tests" >&2
        exit 1
    fi
    grep -q -- '--- PASS: TestIntegrationHighGoTableWrite ' "$WORK_DIR/highgo-native.log" || {
        echo "Native HighGo verification requires the real write contract test" >&2
        exit 1
    }
    echo "Native HighGo isolated write group passed (not the full PostgreSQL gate)"
    exit 0
fi

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

postgres_host=${ADDP_TEST_POSTGRES_HOST:-localhost}
postgres_port=$ADDP_TEST_POSTGRES_PORT
postgres_user=${ADDP_TEST_POSTGRES_USER:-addp}
postgres_password=$ADDP_TEST_POSTGRES_PASSWORD
postgres_sslmode=${ADDP_TEST_POSTGRES_SSLMODE:-disable}
execution_dsn=${ADDP_TEST_EXECUTION_POSTGRES_DSN:-postgres://${postgres_user}:${postgres_password}@${postgres_host}:${postgres_port}/${database}?sslmode=${postgres_sslmode}}

run_projection_store_postgres() {
    (cd "$SOURCE_ROOT/common" && ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN="$execution_dsn" \
        go test ./dataprotection/projectionstore \
        -run '^TestProjectionStore.*AgainstPostgres$' \
        -count=1 -v) 2>&1 | tee "$WORK_DIR/common-projectionstore-postgres.log"
    if grep -q -- '--- SKIP:' "$WORK_DIR/common-projectionstore-postgres.log"; then
        echo "Common protection projection store PostgreSQL gate refuses skipped tests" >&2
        exit 1
    fi
    grep -q -- '--- PASS: TestProjectionStoreSchemaContractAgainstPostgres ' "$WORK_DIR/common-projectionstore-postgres.log" || {
        echo 'Common protection projection store gate requires the real schema and migration contract test' >&2
        exit 1
    }
}
if [ "$test_group" = projection-store ]; then
    run_projection_store_postgres
    echo 'Common protection projection store group passed (not the full PostgreSQL gate)'
    exit 0
fi

run_graph_postgres() {
    (cd "$SOURCE_ROOT/graph/backend" && ADDP_POSTGRES_INTEGRATION=1 GOWORK=off go test ./internal/repository -run '^TestIntegrationPostgresGraph' -count=1 -v) 2>&1 | tee "$WORK_DIR/graph-postgres.log"
    if grep -q -- '--- SKIP:' "$WORK_DIR/graph-postgres.log"; then echo 'Graph PostgreSQL gate refuses skipped tests' >&2; exit 1; fi
    grep -q -- '--- PASS: TestIntegrationPostgresGraphTargetContract' "$WORK_DIR/graph-postgres.log" || {
        echo 'Graph PostgreSQL gate requires the real target contract test' >&2
        exit 1
    }
}
if [ "$test_group" = graph ]; then
    run_graph_postgres
    echo 'Graph PostgreSQL target, schema and execution contracts passed'
    exit 0
fi

cd "$SOURCE_ROOT/common"
test_pattern='^(TestIntegrationPostgresCatalogPreciseReadOnly|TestIntegrationPostgresPrepareTableWrite|TestIntegrationPostgresTableResult|TestIntegrationPostgresAnalyticalInstance|TestIntegrationPostgresAnalyticalText|TestIntegrationPostgresAnalyticalDateBuckets|TestIntegrationPostgresAnalyticalScan|TestIntegrationPostgresAnalyticalRelations|TestIntegrationPostgresAnalyticalCalendar|TestIntegrationPostgresAnalyticalExpressions|TestIntegrationPostgresAnalyticalArithmetic|TestIntegrationPostgresLosslessAnalyticalInteger|TestIntegrationPostgresAnalyticalResultAssertions|TestIntegrationPostgresBoundedWatermarkResumeAndIdempotentUpsert|TestIntegrationPostgresReadBatchHonorsSpatialEncoding|TestIntegrationPostgresQueryReadSession|TestIntegrationResolvePostgresQuery(ReadSet|OutputLineage|OutputRewrite))'
if [ "$test_group" = query-read-set ]; then
    test_pattern='^(TestIntegrationResolvePostgresQuery(ReadSet|OutputLineage|OutputRewrite)|TestIntegrationPostgresQueryReadSession)'
else
    test_pattern="$test_pattern|^TestIntegrationPostgresTyped(JSON|Array)Writes$"
fi
if [ "$test_group" = typed-write ]; then
    test_pattern='^TestIntegrationPostgresTyped(JSON|Array)Writes$'
fi
test_packages=(./engine/plugins/postgresql)
if [ "$test_group" = all ] || [ "$test_group" = highgo-write ]; then
    test_packages+=(./engine/plugins/highgo)
    test_pattern="$test_pattern|^TestIntegrationHighGoTableWrite$"
fi
if [ "$test_group" = highgo-write ]; then
    test_packages=(./engine/plugins/highgo)
    test_pattern='^TestIntegrationHighGoTableWrite$'
fi
ADDP_POSTGRES_INTEGRATION=1 \
ADDP_HIGHGO_INTEGRATION=0 \
    go test "${test_packages[@]}" \
    -run "$test_pattern" \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-postgres.log"; then
    echo "Common PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
if [ "$test_group" = all ] || [ "$test_group" = query-read-set ]; then
    grep -q -- '--- PASS: TestIntegrationResolvePostgresQueryOutputRewrite ' "$WORK_DIR/common-postgres.log" || {
        echo "Common PostgreSQL gate requires the real output rewrite equivalence test" >&2
        exit 1
    }
fi
if [ "$test_group" = all ] || [ "$test_group" = highgo-write ]; then
    grep -q -- '--- PASS: TestIntegrationHighGoTableWrite ' "$WORK_DIR/common-postgres.log" || {
        echo "Common PostgreSQL gate requires the HighGo write protocol contract test" >&2
        exit 1
    }
fi

if [ "$test_group" != all ]; then
    echo "Common PostgreSQL $test_group group passed (not the full gate)"
    exit 0
fi


ADDP_TEST_EXECUTION_POSTGRES_DSN="$execution_dsn" \
    go test ./execution \
    -run '^TestExecution(AuthorizationFactsMigration|Events)AgainstPostgres$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-execution-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-execution-postgres.log"; then
    echo "Common execution PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

ADDP_TEST_EXECUTION_POSTGRES_DSN="$execution_dsn" \
    go test ./exportartifact -run '^TestExportExecutionSourceAgainstPostgres$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-export-source-postgres.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/common-export-source-postgres.log"; then
    echo "Common export source PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

run_projection_store_postgres

ADDP_TEST_TABLE_RESULT_POSTGRES_DSN="$execution_dsn" \
    go test ./dbbridge -run '^TestTableResult.*AgainstPostgres$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-table-result-postgres.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/common-table-result-postgres.log"; then
    echo "Common table result PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

run_graph_postgres
