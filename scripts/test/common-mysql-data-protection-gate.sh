#!/usr/bin/env bash
# ADDP_T2_SERVICES=mysql
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_MYSQL_PASSWORD|ADDP_LOCAL_CI_MYSQL
# common-mysql-data-protection-gate.sh - Verify the MySQL read contract and all four data-protection owners.

set -euo pipefail

test_group=all
local_disposable=false
recovery_mode=
recovery_names=()
if [ "$#" -gt 0 ]; then
    case "$1" in
        --local-disposable)
            [ "$#" -eq 1 ] || { echo '--local-disposable takes no additional arguments' >&2; exit 2; }
            local_disposable=true
            ;;
        --test)
            [ "$#" -eq 2 ] || { echo 'expected --test all|typed-write' >&2; exit 2; }
            case "$2" in all|typed-write) test_group="$2" ;; *) echo 'unknown MySQL test group' >&2; exit 2 ;; esac
            ;;
        --inspect-owned-databases)
            [ "$#" -eq 1 ] || { echo 'inspection takes no names' >&2; exit 2; }
            recovery_mode=inspect
            ;;
        --cleanup-owned-databases)
            shift
            [ "$#" -gt 0 ] || { echo 'explicit owned fixture names required' >&2; exit 2; }
            for name in "$@"; do
                [[ "$name" =~ ^addp_common_mysql_it_[0-9]{19}$ ]] || { echo 'invalid owned MySQL fixture name' >&2; exit 2; }
                recovery_names+=("$name")
            done
            recovery_mode=cleanup
            ;;
        *) echo 'unknown Common MySQL gate argument' >&2; exit 2 ;;
    esac
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-mysql-data-protection.XXXXXX")
export ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST="$WORK_DIR/owned-databases"
: > "$ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST"
chmod 600 "$ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST"
MYSQL_PROJECT="addp-common-mysql-t2-${PPID}-$$"
mysql_owned=false
mysql_compose() {
    docker compose --env-file /dev/null -p "$MYSQL_PROJECT" -f "$WORK_DIR/mysql.json" "$@"
}
cleanup() {
    local status=$?
    local cleanup_failed=false
    trap - EXIT INT TERM
    if [ -s "$ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST" ]; then
        if ! (cd "$ROOT_DIR/common" && ADDP_MYSQL_INTEGRATION=1 go test ./engine/plugins/mysql \
            -run '^TestIntegrationMySQLProviderFixtureCleanup$' -count=1 -v); then
            echo "MySQL fixture cleanup failed; exact ownership manifest retained: $ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST" >&2
            status=1
            cleanup_failed=true
        fi
    fi
    if [ "$mysql_owned" = true ]; then
        mysql_compose down --volumes --remove-orphans >/dev/null 2>&1 || { status=1; cleanup_failed=true; }
        local remaining
        remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$MYSQL_PROJECT") || { status=1; cleanup_failed=true; remaining=unknown; }
        if [ -n "$remaining" ]; then
            echo "MySQL gate left owned containers: $MYSQL_PROJECT" >&2
            status=1
            cleanup_failed=true
        fi
    fi
    if [ "$cleanup_failed" = false ]; then rm -rf "$WORK_DIR"; else echo "MySQL gate evidence retained: $WORK_DIR" >&2; fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# 本地巡检只向聚合门禁传递作用域标记；真实连接参数在本 owner 门禁内生成，
# 避免其他集成门禁继承 MySQL 凭据或意外连接外部 MySQL。
if [ "${ADDP_LOCAL_CI_MYSQL:-0}" = "1" ]; then
    export ADDP_TEST_MYSQL_HOST=127.0.0.1
    export ADDP_TEST_MYSQL_PORT=13306
    export ADDP_TEST_MYSQL_USER=root
    export ADDP_TEST_MYSQL_PASSWORD=addp_local_ci_mysql_password
fi

if [ "$local_disposable" = true ]; then
    # Reuse the registered fixed MySQL fixture image and native healthcheck,
    # with a unique project/container and an OS-assigned loopback-only port.
    docker compose --env-file /dev/null -f "$ROOT_DIR/scripts/test/docker-compose.local-macos-ci.yml" config --format json > "$WORK_DIR/base.json"
    python3 - "$WORK_DIR/base.json" "$WORK_DIR/mysql.json" "$MYSQL_PROJECT" <<'PY'
import json, sys
source, destination, project = sys.argv[1:]
with open(source) as handle:
    mysql = json.load(handle)['services']['mysql']
mysql['container_name'] = project
mysql['ports'] = [{'target': 3306, 'published': '0', 'host_ip': '127.0.0.1', 'protocol': 'tcp'}]
mysql['networks'] = {'default': None}
with open(destination, 'w') as handle:
    json.dump({'services': {'mysql': mysql}}, handle)
PY
    existing=$(docker ps -aq --filter "label=com.docker.compose.project=$MYSQL_PROJECT")
    if [ -n "$existing" ]; then
        echo "MySQL gate refuses to reuse an existing project: $MYSQL_PROJECT" >&2
        exit 1
    fi
    mysql_owned=true
    mysql_compose up -d --wait --wait-timeout 120 mysql
    mysql_binding=$(docker inspect --format '{{with index (index .NetworkSettings.Ports "3306/tcp") 0}}{{.HostIp}}:{{.HostPort}}{{end}}' "$MYSQL_PROJECT")
    [[ "$mysql_binding" =~ ^127\.0\.0\.1:[0-9]+$ ]] || { echo 'MySQL gate requires a mapped loopback port' >&2; exit 1; }
    mysql_port=${mysql_binding##*:}
    [ "$mysql_port" -gt 0 ] && [ "$mysql_port" -le 65535 ] || { echo 'MySQL gate received an invalid mapped port' >&2; exit 1; }
    export ADDP_TEST_MYSQL_HOST=127.0.0.1
    export ADDP_TEST_MYSQL_PORT="$mysql_port"
    export ADDP_TEST_MYSQL_USER=root
    export ADDP_TEST_MYSQL_PASSWORD=addp_local_ci_mysql_password
fi

if [ -z "${ADDP_TEST_MYSQL_PASSWORD:-}" ]; then
    echo "ADDP_TEST_MYSQL_PASSWORD is required for the disposable MySQL protection gate" >&2
    exit 1
fi

if [ "$recovery_mode" = inspect ]; then
    (cd "$ROOT_DIR/common" && ADDP_MYSQL_INTEGRATION=1 ADDP_MYSQL_PROVIDER_FIXTURE_INSPECT=1 \
        go test ./engine/plugins/mysql -run '^TestIntegrationMySQLProviderFixtureInspect$' -count=1 -v)
    echo 'Read-only test catalog inspection finished (not a Provider T2 pass)'
    exit 0
fi
if [ "$recovery_mode" = cleanup ]; then
    printf '%s\n' "${recovery_names[@]}" > "$ADDP_MYSQL_PROVIDER_FIXTURE_MANIFEST"
    echo 'Explicit test fixture recovery requested (not a Provider T2 pass)'
    exit 0
fi

test_pattern='^TestIntegrationMySQL(TypedJSONWrites|AnalyticalInstance|AnalyticalText|AnalyticalDateBuckets|AnalyticalScan|AnalyticalRelations|AnalyticalCalendar|AnalyticalExpressions|AnalyticalArithmetic|LosslessAnalyticalInteger|BoundedWatermarkResumeAndIdempotentUpsert|DataProtectionReadContracts|ResultSchema|OutputRewrite|QueryReadSession)$'
if [ "$test_group" = typed-write ]; then
    test_pattern='^TestIntegrationMySQLTypedJSONWrites$'
fi

cd "$ROOT_DIR/common"
ADDP_MYSQL_INTEGRATION=1 \
    go test ./engine/plugins/mysql \
    -run "$test_pattern" \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-mysql-data-protection.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-mysql-data-protection.log"; then
    echo "MySQL data protection provider gate refuses skipped tests" >&2
    exit 1
fi

if [ "$test_group" != all ]; then
    echo "Common MySQL $test_group group passed (not the full gate)"
    exit 0
fi

grep -q -- '--- PASS: TestIntegrationMySQLOutputRewrite ' "$WORK_DIR/common-mysql-data-protection.log" || {
    echo 'MySQL output rewrite equivalence test did not pass' >&2
    exit 1
}

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
