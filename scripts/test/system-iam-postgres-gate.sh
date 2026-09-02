#!/usr/bin/env bash
# system-iam-postgres-gate.sh - Run destructive System IAM tests against a disposable PostgreSQL database.

set -euo pipefail

PACKAGE_FILTER=""
TEST_FILTER=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --package)
            PACKAGE_FILTER="${2:-}"
            shift 2
            ;;
        --test)
            TEST_FILTER="${2:-}"
            shift 2
            ;;
        *)
            echo "usage: $0 [--package iam|oauth|api|migration] [--test tenant-invitation|catalog-reference-candidates|catalog-integrity|invitation-enrollment-removal|execution-audience|security-module-repair|execution-authorization-lease-boundary|portal-runtime-removal|service-execution-audit|workbench-runtime|workbench-data-application|workbench-catalog-read|workbench-resource-grant|model-catalog-read|standard-catalog-read|service-catalog-read|develop-catalog-read|quality-catalog-read|model-writer-decoupling|catalog-engine-descriptor-read|catalog-project-group-read|transfer-task-provider]" >&2
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

cd "$ROOT_DIR/system/backend"

run_without_skips() {
    package=$1
    pattern=$2
    log_name=$(printf '%s' "$package" | tr '/.' '__')
    log_path="$WORK_DIR/$log_name.log"

    go test "$package" -run "$pattern" -count=1 -v 2>&1 | tee "$log_path"
    if grep -q -- '--- SKIP:' "$log_path"; then
        echo "PostgreSQL release gate refuses skipped tests in $package" >&2
        exit 1
    fi
}

run_without_skips ./internal/testsupport '^TestResetDisposablePostgresForGate$'
case "$PACKAGE_FILTER" in
    "") packages=(./internal/iam ./internal/iam/oauth ./internal/api ./internal/migration) ;;
    iam) packages=(./internal/iam) ;;
    oauth) packages=(./internal/iam/oauth) ;;
    api) packages=(./internal/api) ;;
    migration) packages=(./internal/migration) ;;
    *)
        echo "unsupported System IAM PostgreSQL gate package: $PACKAGE_FILTER" >&2
        exit 2
        ;;
esac
test_pattern='AgainstPostgres$'
case "$TEST_FILTER" in
    "") ;;
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
        test_pattern='^TestRunnerAgainstPostgres$'
        ;;
    execution-audience)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "execution-audience test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestExecutionAudienceForwardMigrationAgainstPostgres$'
        ;;
    security-module-repair)
        if [ "$PACKAGE_FILTER" != "migration" ]; then
            echo "security-module-repair test requires --package migration" >&2
            exit 2
        fi
        test_pattern='^TestSecurityModuleDirtyMigrationRepairAgainstPostgres$'
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
    *)
        echo "unsupported System IAM PostgreSQL gate test: $TEST_FILTER" >&2
        exit 2
        ;;
esac
for package in "${packages[@]}"; do
    run_without_skips "$package" "$test_pattern"
done
