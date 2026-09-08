#!/usr/bin/env bash
# ADDP_T2_SERVICES=mysql
# common-mysql-data-protection-gate.sh - Verify the MySQL read contract and all four data-protection owners.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-mysql-data-protection.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

# 本地巡检只向聚合门禁传递作用域标记；真实连接参数在本 owner 门禁内生成，
# 避免其他集成门禁继承 MySQL 凭据或意外连接外部 MySQL。
if [ "${ADDP_LOCAL_CI_MYSQL:-0}" = "1" ]; then
    export ADDP_TEST_MYSQL_HOST=127.0.0.1
    export ADDP_TEST_MYSQL_PORT=13306
    export ADDP_TEST_MYSQL_USER=root
    export ADDP_TEST_MYSQL_PASSWORD=addp_local_ci_mysql_password
fi

if [ -z "${ADDP_TEST_MYSQL_PASSWORD:-}" ]; then
    echo "ADDP_TEST_MYSQL_PASSWORD is required for the disposable MySQL protection gate" >&2
    exit 1
fi

cd "$ROOT_DIR/common"
ADDP_MYSQL_INTEGRATION=1 \
    go test ./engine/plugins/mysql \
    -run '^TestIntegrationMySQL(BoundedWatermarkResumeAndIdempotentUpsert|DataProtectionReadContracts)$' \
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
