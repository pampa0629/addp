#!/usr/bin/env bash
# standard-postgres-gate.sh - Run Standard PostgreSQL integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-standard-postgres.XXXXXX")

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

if [ -z "${STANDARD_POSTGRES_TEST_DSN:-}" ]; then
    echo "STANDARD_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi

case "$STANDARD_POSTGRES_TEST_DSN" in
    postgres://*/*|postgresql://*/*) ;;
    *)
        echo "STANDARD_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2
        exit 1
        ;;
esac
dsn_without_query=${STANDARD_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in
    *test*|*disposable*) ;;
    *)
        echo "STANDARD_POSTGRES_TEST_DSN must identify a disposable test database" >&2
        exit 1
        ;;
esac

run_without_skips() {
    local package=$1
    local pattern=$2
    local log_name
    log_name=$(printf '%s' "$package" | tr '/.' '__')
    local log_path="$WORK_DIR/$log_name.log"

    go test "$package" -run "$pattern" -count=1 -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "Standard PostgreSQL gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

cd "$ROOT_DIR/standard/backend"
run_without_skips ./internal/repository '^(TestMigrateAgainstPostgres|TestMigrateRenamesLegacyDocumentVersion|TestPostgresDeletePolicies|TestPostgresCodeSetScopeConstraint|TestPostgresElementScopeConstraint|TestPostgresStandardCollectionGovernanceConstraints|TestPostgresCatalogMetricChangeFeedCapturesOwnerLifecycle|TestPostgresReferenceCandidatesFilterAndPaginateOwnerFacts)$'
run_without_skips ./internal/service '^TestPostgres(StandardReferenceDeletion|MetricProfessionalRelations)'
