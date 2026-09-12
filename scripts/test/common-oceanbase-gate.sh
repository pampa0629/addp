#!/usr/bin/env bash
# ADDP_T2_SERVICES=oceanbase
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_OCEANBASE_PASSWORD|ADDP_LOCAL_CI_OCEANBASE
# common-oceanbase-gate.sh - Run OceanBase Engine Provider integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-oceanbase.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

# 本地巡检只向聚合门禁传递作用域标记；真实连接参数在本 owner 门禁内生成，
# 并覆盖任何继承的外部值，避免误连 Business 或生产 OceanBase。
if [ "${ADDP_LOCAL_CI_OCEANBASE:-0}" = "1" ]; then
    export ADDP_TEST_OCEANBASE_HOST=127.0.0.1
    export ADDP_TEST_OCEANBASE_PORT=12881
    export ADDP_TEST_OCEANBASE_TENANT=test
    export ADDP_TEST_OCEANBASE_USER=root@test
    export ADDP_TEST_OCEANBASE_PASSWORD=addp_local_ci_oceanbase_password
    export ADDP_TEST_OCEANBASE_DATABASE=addp_oceanbase_disposable
fi

if [ -z "${ADDP_TEST_OCEANBASE_PASSWORD:-}" ]; then
    echo "ADDP_TEST_OCEANBASE_PASSWORD is required for the disposable OceanBase gate" >&2
    exit 1
fi

database=${ADDP_TEST_OCEANBASE_DATABASE:-addp_oceanbase_disposable}
case "$database" in
    *disposable*) ;;
    *)
        echo "ADDP_TEST_OCEANBASE_DATABASE must identify an isolated disposable database" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/common"
ADDP_TEST_OCEANBASE_DATABASE="$database" \
ADDP_OCEANBASE_INTEGRATION=1 \
    go test ./engine/plugins/oceanbase \
    -run '^TestIntegrationOceanBase' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-oceanbase.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-oceanbase.log"; then
    echo "Common OceanBase gate refuses skipped tests" >&2
    exit 1
fi
