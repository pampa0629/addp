#!/usr/bin/env bash
# develop-postgres-gate.sh - Run Develop owner-local catalog change tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
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

cd "$ROOT_DIR/develop/backend"
go test ./internal/repository -run '^TestCatalogDevTaskChangeFeedAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/develop.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/develop.log"; then
    echo "Develop PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
