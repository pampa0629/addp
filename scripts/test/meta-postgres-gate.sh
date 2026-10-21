#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=META_POSTGRES_TEST_DSN
# meta-postgres-gate.sh - Run Meta PostgreSQL integration tests against a disposable database.

set -euo pipefail

test_group=all
source_root=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --test)
            case "${2:-}" in
                all|authorization-facts) test_group="$2"; shift 2 ;;
                *) echo 'usage: meta-postgres-gate.sh [--test all|authorization-facts] [--source-root checkout]' >&2; exit 2 ;;
            esac
            ;;
        --source-root)
            [ -n "${2:-}" ] || { echo '--source-root requires a checkout path' >&2; exit 2; }
            source_root="$2"; shift 2 ;;
        *) echo 'usage: meta-postgres-gate.sh [--test all|authorization-facts] [--source-root checkout]' >&2; exit 2 ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_ROOT="$ROOT_DIR"
if [ -n "$source_root" ]; then
    SOURCE_ROOT=$(cd "$source_root" && pwd -P) || exit 1
    source_checkout=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel 2>/dev/null) || { echo '--source-root must be an ADDP checkout' >&2; exit 1; }
    source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD 2>/dev/null) || { echo '--source-root must have a source commit' >&2; exit 1; }
    if [ "$source_checkout" != "$SOURCE_ROOT" ] ||
       ! git -C "$ROOT_DIR" merge-base --is-ancestor "$source_commit" HEAD ||
       ! [ -f "$SOURCE_ROOT/meta/backend/go.mod" ] ||
       ! grep -qx 'module github.com/addp/meta' "$SOURCE_ROOT/meta/backend/go.mod"; then
        echo '--source-root must be an ADDP checkout whose commit belongs to this repository' >&2
        exit 1
    fi
fi
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

# Meta fixtures share one database across checkouts. Hold the same open file
# description through the entire gate; never unlink this persistent lock file.
exec 9>>/tmp/addp-meta-postgres-gate.lock
python3 - <<'PY'
import fcntl
import sys

try:
    fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    sys.exit("Meta PostgreSQL gate is already running; run gates serially")
PY

cd "$SOURCE_ROOT/meta/backend"
if [ "$test_group" = all ]; then
    go test ./internal/repository -run '^(TestClaimedComponentsAgainstPostgres|TestDataItemChangeMigrationAgainstPostgres|TestLineageLifecycleMigrationAgainstPostgres|TestNodeStatisticsMigrationAgainstPostgres|TestNodeScanTimeMigrationAgainstPostgres)$' -count=1 -v 2>&1 | tee "$WORK_DIR/meta.log"
fi
service_tests='^(TestDataItemAuthorizationFactsAgainstPostgres|TestScanPlanFactsAgainstPostgres)$'
if [ "$test_group" = all ]; then
    service_tests='^(TestProductionBasicScanAgainstPostgres|TestMosaicScanWholeResultAgainstPostgres|TestDataItemAuthorizationFactsAgainstPostgres|TestScanPlanFactsAgainstPostgres|TestFieldLineage(Evidence|MultiSourceWriteFacts|WideOverview)AgainstPostgres|TestLineageCollector(RetriesUnresolvedTarget|ContinuesAfterInvalidFacts|DefersNewExecutions|PreservesClosedFieldRelations)AgainstPostgres)$'
fi
go test ./internal/service -run "$service_tests" -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
if [ "$test_group" = all ]; then
    go test ./internal/scanadapter -run '^(TestPrecise(Catalog|NativePostgres)ScanAgainstPostgres|TestPreciseContentStorageFactsAgainstPostgres)$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
    go test ./internal/scanprocessor -run '^(TestDelimitedEncodingRefreshAgainstPostgres|TestDelimitedSourceVersionRefreshAgainstPostgres|TestBasicRediscoveryPreservesDeepJSONTableSemanticsAgainstPostgres)$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
    go test ./internal/scanruntime -run '^(TestObjectModelRelatedRefsAgainstPostgres|TestFTPBasicCompositionAgainstPostgres)$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
    go test ./internal/api -run '^TestMetaScanExecutionReadAgainstPostgres$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/meta.log"
fi
if grep -q -- '--- SKIP:' "$WORK_DIR/meta.log"; then
    echo "Meta PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
grep -q -- '--- PASS: TestDataItemAuthorizationFactsAgainstPostgres ' "$WORK_DIR/meta.log" || {
    echo 'Meta PostgreSQL gate requires the real authorization facts test' >&2
    exit 1
}
grep -q -- '--- PASS: TestScanPlanFactsAgainstPostgres ' "$WORK_DIR/meta.log" || {
    echo 'Meta PostgreSQL gate requires the real scan plan facts test' >&2
    exit 1
}
