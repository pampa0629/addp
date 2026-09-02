#!/usr/bin/env bash
# service-postgres-gate.sh - Run Service PostgreSQL integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-service-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${SERVICE_POSTGRES_TEST_DSN:-}" ]; then
    echo "SERVICE_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi
dsn_without_query=${SERVICE_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$SERVICE_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "SERVICE_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
case "$database" in *test*|*disposable*) ;; *) echo "SERVICE_POSTGRES_TEST_DSN must identify a disposable test database" >&2; exit 1 ;; esac

cd "$ROOT_DIR/service/backend"
go test ./internal/protection ./internal/repository ./internal/service -run '^(TestServiceExecuteProtectionAgainstPostgres|TestConsumerCatalogAgainstPostgres|TestCatalogQueryServiceChangeFeedAgainstPostgres|TestQueryServiceConsumerContractMigrationAgainstPostgres)$' -count=1 -v 2>&1 | tee "$WORK_DIR/service.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/service.log"; then
    echo "Service PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
