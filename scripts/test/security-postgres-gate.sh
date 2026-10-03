#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=SECURITY_POSTGRES_TEST_DSN
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-security-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

: "${SECURITY_POSTGRES_TEST_DSN:?SECURITY_POSTGRES_TEST_DSN must reference addp_test or an isolated disposable PostgreSQL database}"
export SECURITY_POSTGRES_TEST_DSN
case "$SECURITY_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "SECURITY_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
dsn_without_query=${SECURITY_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in addp_test|*disposable*) ;; *) echo "SECURITY_POSTGRES_TEST_DSN must use addp_test or an isolated disposable database" >&2; exit 1 ;; esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$SECURITY_POSTGRES_TEST_DSN"

# Shared startup ownership/locking is part of the existing PostgreSQL gate.
(cd "$ROOT_DIR/common" && SCHEMA_POSTGRES_TEST_DSN="$SECURITY_POSTGRES_TEST_DSN" go test ./schema -run '^TestPostgresStartupSchemaOwnership$' -count=1 -v) 2>&1 | tee "$WORK_DIR/startup-schema.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/startup-schema.log"; then exit 1; fi

cd "$ROOT_DIR/security/backend"
go test ./internal/repository -run '^TestSecurityMigrateAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/repository.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/repository.log"; then
    echo "Security PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

go test ./internal/service -run '^Test(DefinitionImpact|EnrollmentLifecycle|ProtectionExemptionAssessmentRevision|FieldAlgorithms)AgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/service.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/service.log"; then
    echo "Security PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

go test ./internal/api -run '^TestDefaultProtectionHTTPAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/api.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/api.log"; then
    echo "Security PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
