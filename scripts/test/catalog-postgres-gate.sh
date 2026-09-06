#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# catalog-postgres-gate.sh - Run Catalog PostgreSQL integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-catalog-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${CATALOG_POSTGRES_TEST_DSN:-}" ]; then
    echo "CATALOG_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi
dsn_without_query=${CATALOG_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$CATALOG_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "CATALOG_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
case "$database" in *test*|*disposable*) ;; *) echo "CATALOG_POSTGRES_TEST_DSN must identify a disposable test database" >&2; exit 1 ;; esac

cd "$ROOT_DIR/catalog/backend"
go test ./internal/repository -run '^TestCatalogMigrateAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/catalog.log"
go test ./internal/service -run '^TestPostgres(RecommendedSuccessorUsesCatalogAggregateAndTenantBoundary|EntryGovernanceCertificationLifecycle|GovernanceCoverageAndSourceResolution|BatchGovernanceUsesPerEntryVersionsAndRollsBackAtomically)$' -count=1 -v 2>&1 | tee -a "$WORK_DIR/catalog.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/catalog.log"; then
    echo "Catalog PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
