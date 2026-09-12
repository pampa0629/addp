#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=MANAGER_POSTGRES_TEST_DSN
# manager-postgres-gate.sh - Verify Manager unified task persistence and cleanup against PostgreSQL.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-manager-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${MANAGER_POSTGRES_TEST_DSN:-}" ]; then
    echo "MANAGER_POSTGRES_TEST_DSN must reference addp_test or a disposable PostgreSQL database" >&2
    exit 1
fi
case "$MANAGER_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "MANAGER_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
dsn_without_query=${MANAGER_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in addp_test|*disposable*) ;; *) echo "MANAGER_POSTGRES_TEST_DSN must identify addp_test or a disposable database" >&2; exit 1 ;; esac

cd "$ROOT_DIR/manager/backend"
ADDP_POSTGRES_INTEGRATION=1 \
    go test ./internal/repository \
    -run '^TestIntegrationPostgresManager' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/manager-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/manager-postgres.log"; then
    echo "Manager PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
