#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=META_POSTGRES_TEST_DSN
# meta-postgres-gate.sh - Run Meta PostgreSQL integration tests against a disposable database.

set -euo pipefail

test_group=all
while [ "$#" -gt 0 ]; do
    case "$1" in
        --test)
            case "${2:-}" in
                all|authorization-facts) test_group="$2"; shift 2 ;;
                *) echo 'usage: meta-postgres-gate.sh [--test all|authorization-facts]' >&2; exit 2 ;;
            esac
            ;;
        *) echo 'usage: meta-postgres-gate.sh [--test all|authorization-facts]' >&2; exit 2 ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-meta-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${META_POSTGRES_TEST_DSN:-}" ]; then
    echo "META_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi
dsn_without_query=${META_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$META_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "META_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
case "$database" in *test*|*disposable*) ;; *) echo "META_POSTGRES_TEST_DSN must identify a disposable test database" >&2; exit 1 ;; esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$META_POSTGRES_TEST_DSN"

cd "$ROOT_DIR/meta/backend"
if [ "$test_group" = all ]; then
    go test ./internal/repository -run '^(TestClaimedComponentsAgainstPostgres|TestDataItemChangeMigrationAgainstPostgres|TestLineageLifecycleMigrationAgainstPostgres|TestNodeStatisticsMigrationAgainstPostgres|TestNodeScanTimeMigrationAgainstPostgres)$' -count=1 -v 2>&1 | tee "$WORK_DIR/meta.log"
fi
service_tests='^TestDataItemAuthorizationFactsAgainstPostgres$'
if [ "$test_group" = all ]; then
    service_tests='^(TestMosaicScanWholeResultAgainstPostgres|TestDataItemAuthorizationFactsAgainstPostgres|TestFieldLineage(Evidence|MultiSourceWriteFacts|WideOverview)AgainstPostgres|TestLineageCollector(RetriesUnresolvedTarget|ContinuesAfterInvalidFacts|DefersNewExecutions|PreservesClosedFieldRelations)AgainstPostgres)$'
fi
go test ./internal/service -run "$service_tests" -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
if [ "$test_group" = all ]; then
    go test ./internal/scanadapter -run '^(TestPrecise(Catalog|NativePostgres)ScanAgainstPostgres|TestPreciseContentStorageFactsAgainstPostgres)$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
    go test ./internal/scanprocessor -run '^(TestDelimitedEncodingRefreshAgainstPostgres|TestDelimitedSourceVersionRefreshAgainstPostgres|TestBasicRediscoveryPreservesDeepJSONTableSemanticsAgainstPostgres)$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
    go test ./internal/api -run '^TestMetaScanExecutionReadAgainstPostgres$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
fi
if grep -q -- '--- SKIP:' "$WORK_DIR/meta.log"; then
    echo "Meta PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
