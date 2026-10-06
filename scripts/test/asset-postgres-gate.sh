#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ASSET_POSTGRES_TEST_DSN
# asset-postgres-gate.sh - Run Asset authorization and schema tests against addp_test.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-asset-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${ASSET_POSTGRES_TEST_DSN:-}" ]; then
    echo "ASSET_POSTGRES_TEST_DSN must reference addp_test or an isolated disposable PostgreSQL database" >&2
    exit 1
fi
dsn_without_query=${ASSET_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$ASSET_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "ASSET_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
case "$database" in addp_test|*disposable*|*test*) ;; *) echo "ASSET_POSTGRES_TEST_DSN must use addp_test or an isolated disposable database" >&2; exit 1 ;; esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$ASSET_POSTGRES_TEST_DSN"

cd "$ROOT_DIR/asset/backend"
go test ./internal/repository -run '^TestAssetSchemaMigrationAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/asset.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/asset.log"; then
    echo "Asset PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
go test ./internal/service -run '^Test(DashboardStats|AssetKeywordSearchCurrentFacts)AgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/service.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/service.log"; then
    echo "Asset PostgreSQL service gate refuses skipped tests" >&2
    exit 1
fi
go test ./internal/service -run '^TestCategoryServiceSubtreeAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/category-subtree.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/category-subtree.log"; then
    echo "Asset PostgreSQL category subtree gate refuses skipped tests" >&2
    exit 1
fi
