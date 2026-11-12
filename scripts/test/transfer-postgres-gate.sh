#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_POSTGRES_PORT,ADDP_TEST_POSTGRES_PASSWORD
# transfer-postgres-gate.sh - Run Transfer execution isolation, continuous events/fencing/recovery, protected export and target override integration (no PostGIS required).

set -euo pipefail

source_root=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --source-root)
            source_root="${2:?--source-root requires a checkout path}"
            shift 2
            ;;
        *) echo "usage: $0 [--source-root checkout]" >&2; exit 2 ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_ROOT=$(cd "${source_root:-$ROOT_DIR}" && pwd -P)
source_checkout=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel)
source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD)
if [ "$source_checkout" != "$SOURCE_ROOT" ] ||
   ! git -C "$ROOT_DIR" merge-base --is-ancestor "$source_commit" HEAD ||
   ! [ -f "$SOURCE_ROOT/transfer/backend/go.mod" ] ||
   ! grep -qx 'module github.com/addp/transfer' "$SOURCE_ROOT/transfer/backend/go.mod"; then
    echo "--source-root must be an ADDP checkout whose commit belongs to this repository" >&2
    exit 2
fi

: "${ADDP_TEST_POSTGRES_PORT:?Set ADDP_TEST_POSTGRES_PORT to the verified PostgreSQL test service mapping}"
: "${ADDP_TEST_POSTGRES_PASSWORD:?Set ADDP_TEST_POSTGRES_PASSWORD for the PostgreSQL test service}"

WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-transfer-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

database=${ADDP_TEST_POSTGRES_DATABASE:-addp_test}
case "$database" in
    *test*|*disposable*) ;;
    *)
        echo "ADDP_TEST_POSTGRES_DATABASE must identify a disposable test database" >&2
        exit 1
        ;;
esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_target "${ADDP_TEST_POSTGRES_HOST:-localhost}" "$ADDP_TEST_POSTGRES_PORT" "$database"

cd "$SOURCE_ROOT/transfer/backend"
ADDP_POSTGRES_INTEGRATION=1 \
    ADDP_TEST_POSTGRES_DATABASE="$database" \
    go test -p 1 ./internal/repository ./internal/protection ./internal/planner ./internal/service \
    -run '^(TestIntegrationPostgresCDCTaskList|TestIntegrationPostgresRunApprovalAtomicity|TestIntegrationPostgresTransferCleanupRuntimeAtomicity|TestIntegrationPostgresScheduledAuthorizationClaim|TestIntegrationPostgresFileGDBClaimWaitsForSourceAuthorization|TestIntegrationPostgresExecutionEventsRemoveRetiredText|TestIntegrationPostgresContinuousExecutionEvents|TestIntegrationPostgresContinuousLeaseFencingAndCancellation|TestIntegrationPostgresContinuousAutomaticRecoveryCreatesNewExecution|TestIntegrationPostgresContinuousRecoveryBackoffAndCircuitBreaker|TestIntegrationPostgresPendingSchemaChangePreventsLeaseExpiryRecovery|TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite|TestIntegrationPostgresExportArtifactProtection|TestIntegrationPlannerTargetOverrideAppendsOnlyToExistingPostgresTable|TestIntegrationPlannerMultiSourceQueryFieldLineage|TestIntegrationPlannerWriteDefinitionBeforeReplace|TestIntegrationPlannerTargetPreparationOutcomes|TestIntegrationPostgresTargetProductionReceipts|TestIntegrationPostgresProductionMetadataSerialization|TestIntegrationPostgresInitialAuthorizationContinuation)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/transfer.log"; then
    echo "Transfer PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

ADDP_POSTGRES_INTEGRATION=1 ADDP_TEST_POSTGRES_DATABASE="$database" \
    go test ./internal/api -run '^(TestIntegrationPostgresTransferExecutionReadIsolation|TestIntegrationPostgresProductionReceiptRead)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer-api.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/transfer-api.log"; then
    echo "Transfer PostgreSQL API gate refuses skipped tests" >&2
    exit 1
fi
