#!/usr/bin/env bash
# ADDP_T2_SERVICES=mysql
# common-mysql-data-protection-gate.sh - Verify the MySQL read contract and all four data-protection owners.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-mysql-data-protection.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${ADDP_TEST_MYSQL_PASSWORD:-}" ]; then
    echo "ADDP_TEST_MYSQL_PASSWORD is required for the disposable MySQL protection gate" >&2
    exit 1
fi

cd "$ROOT_DIR/common"
ADDP_MYSQL_INTEGRATION=1 \
    go test ./engine/plugins/mysql \
    -run '^TestIntegrationMySQLDataProtectionReadContracts$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-mysql-data-protection.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-mysql-data-protection.log"; then
    echo "MySQL data protection provider gate refuses skipped tests" >&2
    exit 1
fi

cd "$ROOT_DIR/manager/backend"
go test ./internal/api ./internal/protection \
    -run '^(TestPreviewProtection|TestProtectProfile)' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/manager-protection-contract.log"

cd "$ROOT_DIR/develop/backend"
go test ./internal/protection \
    -run '^(TestGate|TestExecutionBarrier)' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/develop-protection-contract.log"

cd "$ROOT_DIR/service/backend"
go test ./internal/protection ./internal/service \
    -run '^(TestGate|TestAcknowledgementBarrier|TestTableQueryPlanUsesPublishedColumnsWithoutWildcard)' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/service-protection-contract.log"

cd "$ROOT_DIR/transfer/backend"
go test ./internal/protection ./internal/executor ./internal/service \
    -run '^(TestGate|TestProtectedTableBatchReader|TestPrepareBoundedTableSourceProtection)' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer-protection-contract.log"
