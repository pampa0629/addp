#!/usr/bin/env bash
# meta-postgres-gate.sh - Run Meta PostgreSQL integration tests against a disposable database.

set -euo pipefail

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

cd "$ROOT_DIR/meta/backend"
go test ./internal/repository -run '^(TestDataItemChangeMigrationAgainstPostgres|TestLineageLifecycleMigrationAgainstPostgres)$' -count=1 -v 2>&1 | tee "$WORK_DIR/meta.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/meta.log"; then
    echo "Meta PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
