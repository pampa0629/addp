#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_SYSTEM_POSTGRES_TEST_DSN
# system-iam-postgres-gate.sh - Run destructive System IAM tests against a disposable PostgreSQL database.

set -euo pipefail

PACKAGE_FILTER=""
TEST_FILTER=""
SOURCE_ROOT=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --source-root)
            SOURCE_ROOT="${2:?--source-root requires a checkout path}"
            shift 2
            ;;
        --package)
            PACKAGE_FILTER="${2:-}"
            shift 2
            ;;
        --test)
            TEST_FILTER="${2:-}"
            shift 2
            ;;
        *)
            echo "Engine deployment declarations/publication: --package repository|migration --test engine-deployments" >&2
            echo "Host inventory type/state filters: --package repository --test host-nodes" >&2
            echo "Engine credential/JSONB address regression: --package repository --test engine-address" >&2
            echo "Standard platform units: --package migration --test standard-platform-units" >&2
            echo "Platform alert publication: --package migration --test platform-alert-management" >&2
            echo "Focused authorization-relation migration: --package migration --test source-grant-relations" >&2
            echo "Authorization facts: --package migration|api --test authorization-facts" >&2
            echo "Graph protection runtime: --package migration --test graph-protection-runtime" >&2
            echo "Transfer production receipt: --package migration --test production-receipt-runtime" >&2
            echo "PG production source observation: --package engineaccess --test production-sources" >&2
            echo "FTP file composition Grant/current read: --package engineaccess --test file-composition" >&2
            echo "Unified human data authorizer: --package migration --test unified-data-authorizer" >&2
            echo "usage: $0 [--source-root checkout] [--package iam|oauth|api|migration|engineaccess|repository|online-fixture] [--test oauth-client-credentials|prometheus-credential|credential-context|engine-access-coordination|service-account|tenant-invitation|catalog-reference-candidates|catalog-integrity|standard-collection-removal|invitation-enrollment-removal|execution-audience|duckdb-runtime-catalog|security-module-repair|security-access-request-repair|execution-authorization-lease-boundary|internal-task-authorization|portal-runtime-removal|service-execution-audit|develop-execution-audit|workbench-runtime|workbench-data-application|workbench-catalog-read|workbench-resource-grant|model-catalog-read|standard-catalog-read|service-catalog-read|develop-catalog-read|develop-transfer-execution|quality-catalog-read|quality-plans|model-writer-decoupling|catalog-engine-descriptor-read|catalog-project-group-read|transfer-task-provider|transfer-task-create|export-execution-provenance]" >&2
            exit 2
            ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-system-iam-postgres.XXXXXX")

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

if [ -z "${ADDP_SYSTEM_POSTGRES_TEST_DSN:-}" ]; then
    echo "ADDP_SYSTEM_POSTGRES_TEST_DSN must reference a disposable PostgreSQL 15+ database" >&2
    exit 1
fi

SOURCE_ROOT="${SOURCE_ROOT:-$ROOT_DIR}"
SOURCE_ROOT=$(cd "$SOURCE_ROOT" && pwd -P)
source_checkout=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel)
source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD)
if [ "$source_checkout" != "$SOURCE_ROOT" ] ||
   ! git -C "$ROOT_DIR" merge-base --is-ancestor "$source_commit" HEAD ||
   ! [ -f "$SOURCE_ROOT/system/backend/go.mod" ] ||
   ! grep -qx 'module github.com/addp/system' "$SOURCE_ROOT/system/backend/go.mod"; then
    echo "--source-root must be an ADDP checkout whose commit belongs to this repository" >&2
    exit 1
fi

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$ADDP_SYSTEM_POSTGRES_TEST_DSN"

# All checkouts share the same local IAM test database. Keep the open file
# description alive in this shell and its children until the entire gate exits.
# Never unlink the file: another process could otherwise lock a new inode.
exec 9>>/tmp/addp-system-iam-postgres-gate.lock
python3 - <<'PY'
import fcntl
import sys

try:
    fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    sys.exit("System IAM PostgreSQL gate is already running; run gates serially")
PY

cd "$SOURCE_ROOT/system/backend"

run_without_skips() {
    package=$1
    pattern=$2
    log_name=$(printf '%s' "$package" | tr '/.' '__')
    log_path="$WORK_DIR/$log_name.log"

    # Full forward-migration replay can exceed Go's default 10-minute package
    # budget. Keep every package bounded; CI separately bounds the whole job.
    go test "$package" -run "$pattern" -count=1 -timeout=20m -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "PostgreSQL release gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

run_without_skips ./internal/testsupport '^TestResetDisposablePostgresForGate$'
case "$PACKAGE_FILTER" in
    "") packages=(./internal/iam ./internal/iam/oauth ./internal/api ./internal/migration ./internal/engineaccess ./internal/repository ./cmd/online-test-fixture) ;;
    iam) packages=(./internal/iam) ;;
    oauth) packages=(./internal/iam/oauth) ;;
    api) packages=(./internal/api) ;;
    migration) packages=(./internal/migration) ;;
    repository) packages=(./internal/repository) ;;
    engineaccess) packages=(./internal/engineaccess) ;;
    online-fixture) packages=(./cmd/online-test-fixture) ;;
    *)
        echo "unsupported System IAM PostgreSQL gate package: $PACKAGE_FILTER" >&2
        exit 2
        ;;
esac
test_pattern='AgainstPostgres$'
case "$TEST_FILTER" in
    administrator-governance)
        case "$PACKAGE_FILTER" in
            iam) test_pattern='^TestAdministratorGovernanceAgainstPostgres$' ;;
            api) test_pattern='^TestAdministratorGovernanceAPIAuthenticationAgainstPostgres$' ;;
            migration) test_pattern='^Test(AdministratorGovernanceForwardMigration|Runner)AgainstPostgres$' ;;
            *) echo "administrator-governance requires --package iam, api or migration" >&2; exit 2 ;;
        esac
        ;;
    tenant-functional-roles)
        case "$PACKAGE_FILTER" in
            migration) test_pattern='^Test(TenantFunctionalRolesForwardMigration|Runner)AgainstPostgres$' ;;
            iam) test_pattern='^Test(TenantAdministrationClosure|AuthContextService)AgainstPostgres$' ;;
            *) echo "tenant-functional-roles requires --package iam or migration" >&2; exit 2 ;;
        esac
        ;;
    production-sources)
        [ "$PACKAGE_FILTER" = engineaccess ] || { echo "production-sources requires --package engineaccess" >&2; exit 2; }
        test_pattern='^TestFulfillmentArbitrationAgainstPostgres$/^production_first_acceptance_and_historical_retry$/^(initial_production_observes_actual_sources_and_persistent_target_history_without_issuing|initial_production_grant_terminal_lifecycle)$'
        ;;
    file-composition)
        [ "$PACKAGE_FILTER" = engineaccess ] || { echo "file-composition requires --package engineaccess" >&2; exit 2; }
        test_pattern='^TestFulfillmentArbitrationAgainstPostgres$/^production_first_acceptance_and_historical_retry$/^file_composition_source_grants$'
        ;;
    manager-source)
        [ "$PACKAGE_FILTER" = engineaccess ] || { echo "manager-source requires --package engineaccess" >&2; exit 2; }
        test_pattern='^TestFulfillmentArbitrationAgainstPostgres$/^production_first_acceptance_and_historical_retry$/^(current_complete_source_read_rules|file_composition_source_grants)$/^(Manager_complete_source_execution_authorization|Manager_FTP_model_source_execution_authorization)$'
        ;;
    engine-deployments)
        case "$PACKAGE_FILTER" in
            repository) test_pattern='^TestEngineDeploymentDeclarationsAgainstPostgres$' ;;
            api) test_pattern='^TestEngineDeploymentHTTPAuditAgainstPostgres$' ;;
            migration) test_pattern='^Test(EngineDeploymentPublication|Runner)AgainstPostgres$' ;;
            *) echo "engine-deployments requires --package repository, api or migration" >&2; exit 2 ;;
        esac
        ;;
    host-nodes)
        [ "$PACKAGE_FILTER" = repository ] || { echo "host-nodes requires --package repository" >&2; exit 2; }
        test_pattern='^TestHostNodeIdentityVersionAndBindingAgainstPostgres$'
        ;;
    unified-data-authorizer)
        [ "$PACKAGE_FILTER" = migration ] || { echo "unified-data-authorizer requires --package migration" >&2; exit 2; }
        test_pattern='^Test(UnifiedDataAuthorizer(AssignedSessionRefresh|ForwardMigration)|Runner)AgainstPostgres$'
        ;;
    task-authorization)
        case "$PACKAGE_FILTER" in
            iam) test_pattern='^TestTaskAuthorizationSubjectAgainstPostgres$' ;;
            migration) test_pattern='^Test(TransferTaskAuthorizationForwardMigration|Runner)AgainstPostgres$' ;;
            *) echo "task-authorization requires --package iam or migration" >&2; exit 2 ;;
        esac
        ;;
    standard-platform-units)
        [ "$PACKAGE_FILTER" = migration ] || { echo "standard-platform-units requires --package migration" >&2; exit 2; }
        test_pattern='^TestStandardPlatformUnitsForwardMigrationAgainstPostgres$'
        ;;
    resource-alert-rules)
        [ "$PACKAGE_FILTER" = migration ] || { echo "resource-alert-rules requires --package migration" >&2; exit 2; }
        test_pattern='^Test(PlatformResourceAlertRulesForwardMigration|Runner)AgainstPostgres$'
        ;;
    platform-alert-management)
        [ "$PACKAGE_FILTER" = migration ] || { echo "platform-alert-management requires --package migration" >&2; exit 2; }
        test_pattern='^Test(PlatformAlertManagementForwardMigration|PlatformHTTPQualityForwardMigration|PlatformTraceForwardMigration|Runner)AgainstPostgres$'
        ;;
    engine-address)
        [ "$PACKAGE_FILTER" = repository ] || { echo "engine-address requires --package repository" >&2; exit 2; }
        test_pattern='^TestEngineCredentialUpdateAfterJSONBAddressAgainstPostgres$'
        ;;
    authorization-facts)
        case "$PACKAGE_FILTER" in
            migration) test_pattern='^Test(Meta|Catalog)AuthorizationFactsForwardMigrationAgainstPostgres$' ;;
            api) test_pattern='^TestEngineAccessDelegationAgainstPostgres$' ;;
            *) echo "authorization-facts test requires --package migration or api" >&2; exit 2 ;;
        esac
        ;;
    source-grant-relations)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "source-grant-relations test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^Test(SourceGrantRelations|SourceDataItemBoundary(Identity)?|SourceSingleRecordTargets)ForwardMigrationAgainstPostgres$'
        ;;
    oauth-client-credentials)
        if [ "$PACKAGE_FILTER" != "api" ]; then
            echo "oauth-client-credentials test requires --package api" >&2
            exit 2
        fi
        test_pattern='^TestIAMOAuthClientCredentialsAuthContextAgainstPostgres$'
        ;;
    prometheus-credential)
        if [ "$PACKAGE_FILTER" != "iam" ]; then
            echo "prometheus-credential test requires --package iam" >&2
            exit 2
        fi
        test_pattern='^TestPrometheusCredentialAgainstPostgres$'
        ;;
    export-execution-provenance)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "export-execution-provenance test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestExportExecutionProvenanceMigrationAgainstPostgres$'
        ;;
    "") ;;
    credential-context)
        if [ "$PACKAGE_FILTER" != "iam" ]; then
            echo "credential-context test requires --package iam" >&2
            exit 2
        fi
        test_pattern='^Test(DelegationService|ExecutionAuthorizationService|NotebookSessionAuthorizationService|CredentialValidationEvidence|BrowserAuthenticationClock)AgainstPostgres$'
        ;;
    engine-access-coordination)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "engine-access-coordination test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^Test(SourceDeny|SourceDenyRelease|FulfillmentOutcome|FulfillmentGrant|FulfillmentGrantRevocation|FulfillmentGrantRevocationExpiry|FulfillmentRecoveryPermission|FulfillmentHandlingPermission|FulfillmentBasis|ApprovalRequirement|SharingExpiry|SourceGrantRelations)ForwardMigrationAgainstPostgres$'
        ;;
    ontology-backend)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "ontology-backend test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^Test(Ontology(Runtime|Backend)|PlatformOntologyInspection)ForwardMigrationAgainstPostgres$'
        ;;
    internal-task-authorization)
        if [ "$PACKAGE_FILTER" != "iam" ]; then
            echo "internal-task-authorization test requires --package iam" >&2
            exit 2
        fi
        test_pattern='^Test(InternalTaskAuthorization|ExecutionAuthorizationService)AgainstPostgres$'
        ;;
    quality-plans)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "quality-plans test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestQuality(Plans|Rules|StandardReference)ForwardMigrationAgainstPostgres$'
        ;;
    service-account)
        if [ "$PACKAGE_FILTER" != "iam" ]; then
            echo "service-account test requires --package iam" >&2
            exit 2
        fi
        test_pattern='^TestTenantServiceAccountServiceAgainstPostgres$'
        ;;
    tenant-invitation)
        if [ "$PACKAGE_FILTER" != "iam" ]; then
            echo "tenant-invitation test requires --package iam" >&2
            exit 2
        fi
        test_pattern='^TestTenantInvitationServiceAgainstPostgres$'
        ;;
    catalog-reference-candidates)
        if [ "$PACKAGE_FILTER" != "iam" ]; then
            echo "catalog-reference-candidates test requires --package iam" >&2
            exit 2
        fi
        test_pattern='^TestCatalogReferenceCandidatesAgainstPostgres$'
        ;;
    catalog-integrity)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "catalog-integrity test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^Test(Runner|EngineAccessDelegationRoleForwardMigration|SourceDataAuthorizerRoleForwardMigration)AgainstPostgres$'
        ;;
    standard-collection-removal)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "standard-collection-removal test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestStandardCollectionRemovalForwardMigrationAgainstPostgres$'
        ;;
    execution-audience)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "execution-audience test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestExecutionAudienceForwardMigrationAgainstPostgres$'
        ;;
    duckdb-runtime-catalog)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "duckdb-runtime-catalog test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestDuckDBRuntimeCatalogForwardMigrationAgainstPostgres$'
        ;;
    security-module-repair)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "security-module-repair test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestSecurityModuleDirtyMigrationRepairAgainstPostgres$'
        ;;
    security-access-request-repair)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "security-access-request-repair test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestSecurityProtectionAccessRequestDirtyMigrationRepairAgainstPostgres$'
        ;;
    invitation-enrollment-removal)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "invitation-enrollment-removal test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestInvitationEnrollmentTicketRemovalForwardMigrationAgainstPostgres$'
        ;;
    execution-authorization-lease-boundary)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "execution-authorization-lease-boundary test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestExecutionAuthorizationLeaseBoundaryForwardMigrationAgainstPostgres$'
        ;;
    portal-runtime-removal)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "portal-runtime-removal test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestPortalTenantRuntimeRemovalForwardMigrationAgainstPostgres$'
        ;;
    service-execution-audit)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "service-execution-audit test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestServiceExecutionAuditForwardMigrationAgainstPostgres$'
        ;;
    develop-execution-audit)
        if [ "$PACKAGE_FILTER" != "api" ]; then
            echo "develop-execution-audit test requires --package api" >&2
            exit 2
        fi
        test_pattern='^TestDevelopExecutionServiceAuditPersistsAgainstPostgres$'
        ;;
    workbench-runtime)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "workbench-runtime test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestWorkbenchRuntimeForwardMigrationAgainstPostgres$'
        ;;
    workbench-data-application)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "workbench-data-application test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestWorkbenchDataApplicationForwardMigrationAgainstPostgres$'
        ;;
    workbench-catalog-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "workbench-catalog-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestWorkbenchCatalogReadForwardMigrationAgainstPostgres$'
        ;;
    workbench-resource-grant)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "workbench-resource-grant test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestWorkbenchResourceGrantForwardMigrationAgainstPostgres$'
        ;;
    model-catalog-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "model-catalog-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestModelCatalogReadForwardMigrationAgainstPostgres$'
        ;;
    standard-catalog-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "standard-catalog-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestStandardCatalogReadForwardMigrationAgainstPostgres$'
        ;;
    service-catalog-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "service-catalog-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestServiceCatalogReadForwardMigrationAgainstPostgres$'
        ;;
    develop-catalog-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "develop-catalog-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestDevelopCatalogReadForwardMigrationAgainstPostgres$'
        ;;
    develop-transfer-execution)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "develop-transfer-execution test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestDevelopTransferExecutionForwardMigrationAgainstPostgres$'
        ;;
    quality-catalog-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "quality-catalog-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestQualityCatalogReadForwardMigrationAgainstPostgres$'
        ;;
    model-writer-decoupling)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "model-writer-decoupling test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestModelWriterDecouplingForwardMigrationAgainstPostgres$'
        ;;
    catalog-engine-descriptor-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "catalog-engine-descriptor-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestCatalogEngineDescriptorReadForwardMigrationAgainstPostgres$'
        ;;
    catalog-project-group-read)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "catalog-project-group-read test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestCatalogProjectGroupReadForwardMigrationAgainstPostgres$'
        ;;
    transfer-task-provider)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "transfer-task-provider test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestTransferTaskProviderForwardMigrationAgainstPostgres$'
        ;;
    production-receipt-runtime)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "production-receipt-runtime requires --package migration" >&2; exit 2
        fi
        test_pattern='^TestProductionReceiptForwardMigrationAgainstPostgres$'
        ;;
    graph-protection-runtime)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "graph-protection-runtime requires --package migration" >&2; exit 2
        fi
        test_pattern='^TestGraphProtectionRuntimeForwardMigrationAgainstPostgres$'
        ;;
    transfer-task-create)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "transfer-task-create test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestTransferTaskCreateDelegationMigrationAgainstPostgres$'
        ;;
    *)
        echo "unsupported System IAM PostgreSQL gate test: $TEST_FILTER" >&2
        exit 2
        ;;
esac
for package in "${packages[@]}"; do
    run_without_skips "$package" "$test_pattern"
done
