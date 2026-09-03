#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-security-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

SECURITY_POSTGRES_TEST_DSN=${SECURITY_POSTGRES_TEST_DSN:-postgres://addp:addp_password@localhost:15432/addp_test?sslmode=disable}
export SECURITY_POSTGRES_TEST_DSN
case "$SECURITY_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "SECURITY_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
dsn_without_query=${SECURITY_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in addp_test|*disposable*) ;; *) echo "SECURITY_POSTGRES_TEST_DSN must use addp_test or an isolated disposable database" >&2; exit 1 ;; esac

cd "$ROOT_DIR/security/backend"
go test ./internal/repository -run '^TestSecurityMigrateAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/repository.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/repository.log"; then
    echo "Security PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

go test ./internal/service -run '^Test(DefinitionImpact|EnrollmentLifecycle)AgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/service.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/service.log"; then
    echo "Security PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
