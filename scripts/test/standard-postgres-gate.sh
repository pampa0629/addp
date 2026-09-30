#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_OWNED_SERVICES=minio
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.standard-minio-t2.yml
# ADDP_T2_INPUT_FILES=scripts/infra/Dockerfile.minio
# ADDP_T2_REQUIRED_ENV=STANDARD_POSTGRES_TEST_DSN
# standard-postgres-gate.sh - PostgreSQL and gate-owned disposable MinIO integration tests.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-standard-postgres.XXXXXX")
RUN_ID=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
STANDARD_T2_PROJECT="addp-standard-minio-t2-$RUN_ID"
STANDARD_T2_COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.standard-minio-t2.yml"
export STANDARD_MINIO_TEST_IMAGE="addp-standard-minio-t2-$RUN_ID:RELEASE.2025-10-15T17-29-55Z"
export STANDARD_MINIO_TEST_ACCESS_KEY="standard_t2_$RUN_ID"
export STANDARD_MINIO_TEST_SECRET_KEY
STANDARD_MINIO_TEST_SECRET_KEY=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MINIO_STARTED=0

compose() {
    docker compose --env-file /dev/null -p "$STANDARD_T2_PROJECT" -f "$STANDARD_T2_COMPOSE_FILE" "$@"
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    if [ "$MINIO_STARTED" = 1 ]; then
        if [ "$status" -ne 0 ]; then
            compose logs --no-color --tail=100 >&2
        fi
        compose down --volumes --remove-orphans > "$WORK_DIR/cleanup.log" 2>&1 || {
            cat "$WORK_DIR/cleanup.log" >&2
            status=1
        }
        local remaining
        remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$STANDARD_T2_PROJECT") || status=1
        if [ -n "$remaining" ]; then status=1; fi
        remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$STANDARD_T2_PROJECT") || status=1
        if [ -n "$remaining" ]; then status=1; fi
        remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$STANDARD_T2_PROJECT") || status=1
        if [ -n "$remaining" ]; then status=1; fi
        if docker image inspect "$STANDARD_MINIO_TEST_IMAGE" >/dev/null 2>&1; then
            docker image rm --no-prune "$STANDARD_MINIO_TEST_IMAGE" >/dev/null 2>&1 || status=1
        fi
        if docker image inspect "$STANDARD_MINIO_TEST_IMAGE" >/dev/null 2>&1; then status=1; fi
        if [ "$status" -eq 0 ]; then
            echo "Standard temporary MinIO containers, networks, volumes and image: zero residue"
        fi
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$STANDARD_POSTGRES_TEST_DSN"

MINIO_STARTED=1
compose build minio > "$WORK_DIR/build.log" 2>&1 || {
    cat "$WORK_DIR/build.log" >&2
    exit 1
}
compose up -d --wait --wait-timeout 90 minio
export STANDARD_MINIO_TEST_ENDPOINT
STANDARD_MINIO_TEST_ENDPOINT=$(compose port minio 9000)
case "$STANDARD_MINIO_TEST_ENDPOINT" in
    127.0.0.1:*) ;;
    *) echo "Disposable MinIO must bind only loopback" >&2; exit 1 ;;
esac
export ADDP_STANDARD_MINIO_INTEGRATION=1

run_without_skips() {
    local package=$1
    local pattern=$2
    local log_path
    log_path=$(mktemp "$WORK_DIR/tests.XXXXXX")

    go test "$package" -run "$pattern" -count=1 -timeout=180s -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "Standard PostgreSQL gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

cd "$ROOT_DIR/standard/backend"
run_without_skips ./internal/repository '^(TestMigrateAgainstPostgres|TestMigrateConvertsLegacyDocumentToStableIdentityAndRevision|TestPostgresDeletePolicies|TestPostgresCodeSetScopeConstraint|TestPostgresElementScopeConstraint|TestPostgresExactElementRevisionResolutionKeepsPublishedHistory|TestPostgresGlossaryScopeConstraint|TestPostgresDocumentRevisionAndExtractionConstraints|TestPostgresMetricRevisionEffectiveIntervals|TestPostgresRemovesRetiredCollectionTables|TestPostgresCatalogMetricChangeFeedCapturesOwnerLifecycle|TestPostgresReferenceCandidatesFilterAndPaginateOwnerFacts|TestPostgresDocumentCandidateComparisonTargets|TestPostgresDocumentCandidateFormalization|TestPostgresDocumentCandidateFamilyDecision)$'
run_without_skips ./internal/repository '^TestPostgres(RemovesElementExtraQualityRules|SimplifiesStandardDefinitions|GlossaryMappingsPreserveIdentityLifecycle)$'
run_without_skips ./internal/service '^TestPostgres(StandardReferenceDeletion|MetricProfessionalRelations|DocumentCandidateGroups|StandardRevisionLifecycle)'
run_without_skips ./internal/service '^TestDocumentFileLifecycleAgainstPostgresAndMinIO$'
