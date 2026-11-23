#!/usr/bin/env bash
# ADDP_T2_SERVICES=mysql
# ADDP_T2_INPUT_FILES=common/domainreference/ scripts/test/inference-platform-startup.py scripts/test/mysql-gateway-login.py scripts/test/mysql-gateway-install-contract.sh scripts/test/mysql-meta-install-contract.sh scripts/test/mysql-source-grant-boundary.sh system/backend/internal/migration/mysql_source_grant/persist_source_grant.sql system/backend/internal/migration/mysql_source_grant_catalog/003_initial_production_results.sql system/backend/internal/migration/mysql_source_grant_catalog/004_initial_upload_results.sql scripts/test/mysql-common-install-contract.sh scripts/test/mysql-system-install-contract.sh scripts/test/mysql-system-startup.py scripts/test/docker-compose.system-infra-configuration-t2.yml system/backend/cmd/iam-bootstrap/testdata/mysql_cli.py
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_MYSQL_PASSWORD|ADDP_LOCAL_CI_MYSQL
# common-mysql-data-protection-gate.sh - Verify the MySQL read contract and all four data-protection owners.

set -euo pipefail

test_group=all
local_disposable=false
recovery_mode=
recovery_names=()
while [ "$#" -gt 0 ]; do
    case "$1" in
        --local-disposable)
            [ "$local_disposable" = false ] || { echo 'duplicate --local-disposable' >&2; exit 2; }
            local_disposable=true
            shift
            ;;
        --test)
            [ "$#" -eq 2 ] || { echo 'expected --test all|shared-sql-pool|typed-write|guarded-spatial-read|platform-database|platform-execution|platform-common-install|platform-system-request|platform-source-grant-boundary|platform-system-iam-foundation|platform-system-registry|platform-system-install|platform-gateway-install|platform-inference-install|platform-workbench-install|platform-meta-scan' >&2; exit 2; }
            case "$2" in all|shared-sql-pool|typed-write|guarded-spatial-read|platform-database|platform-execution|platform-common-install|platform-system-request|platform-source-grant-boundary|platform-system-iam-foundation|platform-system-registry|platform-system-install|platform-gateway-install|platform-inference-install|platform-workbench-install|platform-meta-scan) test_group="$2" ;; *) echo 'unknown MySQL test group' >&2; exit 2 ;; esac
            shift 2
            ;;
        --inspect-owned-databases)
            [ "$#" -eq 1 ] || { echo 'inspection takes no names' >&2; exit 2; }
            recovery_mode=inspect
            shift
            ;;
        --cleanup-owned-databases)
            shift
            [ "$#" -gt 0 ] || { echo 'explicit owned fixture names required' >&2; exit 2; }
            for name in "$@"; do
                [[ "$name" =~ ^addp_common_mysql_it_[0-9]{19}$ ]] || { echo 'invalid owned MySQL fixture name' >&2; exit 2; }
                recovery_names+=("$name")
            done
            recovery_mode=cleanup
            shift "$#"
            ;;
        *) echo 'unknown Common MySQL gate argument' >&2; exit 2 ;;
    esac
done
if [ "$local_disposable" = true ] && [ -n "$recovery_mode" ]; then
    echo 'fixture inspection or recovery requires an explicit test server' >&2
    exit 2
fi

case "$test_group" in
    platform-execution|platform-common-install|platform-system-request|platform-source-grant-boundary|platform-system-iam-foundation|platform-system-registry|platform-system-install|platform-gateway-install|platform-inference-install|platform-workbench-install|platform-meta-scan)
        if [ "$local_disposable" != true ]; then
            echo "$test_group requires a gate-owned instance (--local-disposable)" >&2
            exit 2
        fi
        ;;
esac

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
    python3 - "$WORK_DIR/base.json" "$WORK_DIR/mysql.json" "$MYSQL_PROJECT" "$test_group" <<'PY'
import json, sys
source, destination, project, group = sys.argv[1:]
with open(source) as handle:
    mysql = json.load(handle)['services']['mysql']
mysql['container_name'] = project
mysql['ports'] = [{'target': 3306, 'published': '0', 'host_ip': '127.0.0.1', 'protocol': 'tcp'}]
mysql['networks'] = {'default': None}
mysql['environment']['MYSQL_DATABASE'] = {
    'platform-execution': 'common',
    'platform-common-install': 'common',
    'platform-system-request': 'system',
    'platform-source-grant-boundary': 'system',
    'platform-system-iam-foundation': 'system',
    'platform-system-registry': 'system',
    'platform-system-install': 'system',
    'platform-meta-scan': 'meta',
    'platform-gateway-install': 'gateway',
    'platform-inference-install': 'inference',
    'platform-workbench-install': 'workbench',
}.get(group, 'addp_platform_mysql_test')

with open(destination, 'w') as handle:
    json.dump({'services': {'mysql': mysql}}, handle)
PY
    if [ "$test_group" = platform-system-install ]; then
        export REDIS_PASSWORD=$(openssl rand -hex 24)
        ADDP_INFRA_GATE_PASSWORD="$REDIS_PASSWORD" ADDP_INFRA_GATE_MANAGER_PASSWORD="$(openssl rand -hex 24)" \
            docker compose --env-file /dev/null -f "$ROOT_DIR/scripts/test/docker-compose.system-infra-configuration-t2.yml" \
            config --format json > "$WORK_DIR/redis-base.json"
        python3 - "$WORK_DIR/mysql.json" "$WORK_DIR/redis-base.json" <<'PYTHON'
import json, sys
from pathlib import Path
config = Path(sys.argv[1])
spec = json.loads(config.read_text())
redis = json.loads(Path(sys.argv[2]).read_text())["services"]["redis"]
spec["services"]["redis"] = redis
spec["volumes"] = {"redis_config_data": {}}
config.write_text(json.dumps(spec))
PYTHON
    fi
    existing=$(docker ps -aq --filter "label=com.docker.compose.project=$MYSQL_PROJECT")
    if [ -n "$existing" ]; then
        echo "MySQL gate refuses to reuse an existing project: $MYSQL_PROJECT" >&2
        exit 1
    fi
    mysql_owned=true
    mysql_compose up -d --wait --wait-timeout 120 mysql
    if [ "$test_group" = platform-system-install ]; then
        mysql_compose up -d --wait --wait-timeout 90 redis
        redis_binding=$(mysql_compose port redis 6379)
        [[ "$redis_binding" =~ ^127\.0\.0\.1:[0-9]+$ ]]
        export REDIS_PORT=${redis_binding##*:}
    fi
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

if [ "$test_group" = platform-execution ]; then
    (cd "$ROOT_DIR/common" && ADDP_TEST_MYSQL_COMMON_EXECUTION=1 GOWORK=off go test -p 1 ./schema ./execution \
        -run '^(TestPlatformMySQLExecutionLifecycle|TestExecutionRetentionAssessmentAgainstMySQL|TestCleanupScanReceiptAgainstMySQL)$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/platform-execution.log"
    grep -q '^--- PASS: TestPlatformMySQLExecutionLifecycle ' "$WORK_DIR/platform-execution.log"
    grep -q '^--- PASS: TestCleanupScanReceiptAgainstMySQL ' "$WORK_DIR/platform-execution.log"
    grep -q '^--- PASS: TestExecutionRetentionAssessmentAgainstMySQL ' "$WORK_DIR/platform-execution.log"
    exit 0
fi

if [ "$test_group" = platform-common-install ]; then
    (cd "$ROOT_DIR/system/backend" && GOWORK=off go build -o "$WORK_DIR/system-server" ./cmd/server &&
        GOWORK=off go test -c -o "$WORK_DIR/common-startup.test" ./internal/repository)
    bash "$ROOT_DIR/scripts/test/mysql-common-install-contract.sh" "$WORK_DIR" "$MYSQL_PROJECT" "$mysql_port"
    exit 0
fi

if [ "$test_group" = platform-system-install ] || [ "$test_group" = platform-source-grant-boundary ]; then
    (cd "$ROOT_DIR/system/backend" && GOWORK=off go build -o "$WORK_DIR/system-server" ./cmd/server)
fi

if [ "$test_group" = platform-system-install ]; then
    (cd "$ROOT_DIR/system/backend" && GOWORK=off go build -o "$WORK_DIR/iam-bootstrap" ./cmd/iam-bootstrap)
    (cd "$ROOT_DIR/gateway" && GOWORK=off go build -o "$WORK_DIR/gateway" ./cmd/gateway)
    bash "$ROOT_DIR/scripts/test/mysql-system-install-contract.sh" "$WORK_DIR" "$MYSQL_PROJECT" "$mysql_port"
    exit 0
fi

if [[ "$test_group" == platform-inference-install || "$test_group" == platform-workbench-install ]]; then
    owner=inference
    if [[ "$test_group" == platform-workbench-install ]]; then owner=workbench; fi
    export ADDP_INFRA_DATABASE_PROVIDER=mysql ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$mysql_port"
    export ADDP_DATABASE_DEPLOY_USER=platform_deploy ADDP_DATABASE_RUNTIME_USER=platform_runtime
    ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
    ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
    export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD
    docker exec -i "$MYSQL_PROJECT" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql -uroot' <<SQL
CREATE USER 'platform_deploy'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'platform_runtime'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT ALL ON ${owner}.* TO 'platform_deploy'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON ${owner}.* TO 'platform_runtime'@'%';
SQL
    if [[ "$owner" == workbench ]]; then
        cd "$ROOT_DIR/workbench/backend"
        GOWORK=off go build -o "$WORK_DIR/workbench" ./cmd/server
        status=0
        "$WORK_DIR/workbench" prepare-database > "$WORK_DIR/triggers.sql" || status=$?
        case "$status" in
            0) ;;
            3) docker exec -i "$MYSQL_PROJECT" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql -uroot workbench' < "$WORK_DIR/triggers.sql" ;;
            *) exit "$status" ;;
        esac
        "$WORK_DIR/workbench" prepare-database
        unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
        ADDP_TEST_WORKBENCH_PLATFORM=1 GOWORK=off go test ./internal/repository -run '^TestWorkbenchDataApplicationRepositoryAgainstMySQL$' -count=1 -v | tee "$WORK_DIR/workbench.log"
        grep -q '^--- PASS: TestWorkbenchDataApplicationRepositoryAgainstMySQL ' "$WORK_DIR/workbench.log"
        exit 0
    fi
    cd "$ROOT_DIR/inference/backend"
    GOWORK=off go build -o "$WORK_DIR/inference" ./cmd/server
    python3 "$ROOT_DIR/scripts/test/inference-platform-startup.py" "$WORK_DIR/inference" unprepared
    "$WORK_DIR/inference" prepare-database
    "$WORK_DIR/inference" prepare-database
    unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
    python3 "$ROOT_DIR/scripts/test/inference-platform-startup.py" "$WORK_DIR/inference"
    ADDP_TEST_INFERENCE_PLATFORM=1 GOWORK=off go test ./internal/repository -run '^TestInferencePlatformStore$' -count=1 -v | tee "$WORK_DIR/inference.log"
    grep -q '^--- PASS: TestInferencePlatformStore ' "$WORK_DIR/inference.log"
    exit 0
fi

if [ "$test_group" = platform-gateway-install ]; then
    export ADDP_DATABASE_DEPLOY_USER=platform_deploy ADDP_DATABASE_RUNTIME_USER=platform_runtime
    ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
    ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
    export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD
    docker exec -i "$MYSQL_PROJECT" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot' <<SQL
CREATE USER 'platform_deploy'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'platform_runtime'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT ALL ON gateway.* TO 'platform_deploy'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON gateway.* TO 'platform_runtime'@'%';
SQL
    (cd "$ROOT_DIR/gateway" && GOWORK=off go build -o "$WORK_DIR/gateway" ./cmd/gateway)
    bash "$ROOT_DIR/scripts/test/mysql-gateway-install-contract.sh" "$WORK_DIR" "$MYSQL_PROJECT" "$mysql_port"
    exit 0
fi

if [ "$test_group" = platform-system-registry ]; then
    (cd "$ROOT_DIR/system/backend" && ADDP_TEST_MYSQL_SYSTEM_REGISTRY=1 GOWORK=off go test ./internal/repository \
        -run '^TestModuleRegistryAgainstMySQL$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/platform-system-registry.log"
    grep -q '^--- PASS: TestModuleRegistryAgainstMySQL ' "$WORK_DIR/platform-system-registry.log"
    exit 0
fi

if [ "$test_group" = platform-system-iam-foundation ]; then
    (cd "$ROOT_DIR/system/backend" && ADDP_TEST_MYSQL_SYSTEM_IAM_FOUNDATION=1 GOWORK=off go test ./internal/migration \
        -run '^TestMySQLIAMFoundation$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/platform-system-iam-foundation.log"
    grep -q '^--- PASS: TestMySQLIAMFoundation ' "$WORK_DIR/platform-system-iam-foundation.log"
    exit 0
fi

if [ "$test_group" = platform-source-grant-boundary ]; then
    bash "$ROOT_DIR/scripts/test/mysql-source-grant-boundary.sh" "$MYSQL_PROJECT" "$WORK_DIR"
    exit 0
fi

if [ "$test_group" = platform-system-request ]; then
    (cd "$ROOT_DIR/system/backend" && ADDP_TEST_MYSQL_SYSTEM_REQUEST_LOCK=1 GOWORK=off go test ./internal/engineaccess \
        -run '^TestSystemAccess(Request|Target)LockAgainstMySQL$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/platform-system-request.log"
    grep -q '^--- PASS: TestSystemAccessRequestLockAgainstMySQL ' "$WORK_DIR/platform-system-request.log"
    grep -q '^--- PASS: TestSystemAccessTargetLockAgainstMySQL ' "$WORK_DIR/platform-system-request.log"
    exit 0
fi

if [ "$test_group" = platform-meta-scan ]; then
    mysql_compose exec -T mysql sh -ec 'exec mysql --user=root --password="$MYSQL_ROOT_PASSWORD" --execute="CREATE DATABASE common"'
    (cd "$ROOT_DIR/system/backend" && GOWORK=off go build -o "$WORK_DIR/system-server" ./cmd/server)
    (cd "$ROOT_DIR/meta/backend" && GOWORK=off go build -o "$WORK_DIR/meta-server" ./cmd/server)
    export ADDP_TEST_META_SOURCE_PASSWORD
    ADDP_TEST_META_SOURCE_PASSWORD=$(openssl rand -hex 24)
    export ADDP_DATABASE_RUNTIME_USER=worker_gate
    ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
    export ADDP_DATABASE_RUNTIME_PASSWORD
    bash "$ROOT_DIR/scripts/test/mysql-meta-install-contract.sh" "$WORK_DIR" "$MYSQL_PROJECT" "$mysql_port"
    (cd "$ROOT_DIR/meta/backend" && ADDP_TEST_MYSQL_META_SCAN=1 GOWORK=off go test ./internal/service \
        -run '^TestMetaClaimAndTaskSummaryAgainstMySQL$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/platform-meta-scan.log"
    grep -q '^--- PASS: TestMetaClaimAndTaskSummaryAgainstMySQL ' "$WORK_DIR/platform-meta-scan.log"
    (cd "$ROOT_DIR/meta/backend" && GOWORK=off go build -o "$WORK_DIR/meta-worker" ./cmd/worker)
    mysql_admin() {
        mysql_compose exec -T mysql sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql -h127.0.0.1 -uroot -NB meta'
    }
    mysql_admin <<SQL
GRANT SELECT, INSERT, UPDATE, DELETE ON meta.* TO 'worker_gate'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE ON common.* TO 'worker_gate'@'%';
SQL
    (cd "$ROOT_DIR/meta/backend" && ADDP_TEST_META_MYSQL_CATALOG=1 GOWORK=off go test ./internal/scanruntime \
        -run '^TestMySQLScanCatalog$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/meta-catalog.log"
    grep -q '^--- PASS: TestMySQLScanCatalog ' "$WORK_DIR/meta-catalog.log"
    (cd "$ROOT_DIR/system/backend" && ADDP_INFRA_DATABASE_PROVIDER=mysql ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$mysql_port" ADDP_TEST_HEARTBEAT_RETENTION=1 GOWORK=off go test ./internal/service -run '^TestSystemRuntimeHeartbeatRetention$' -count=1 -v) | tee "$WORK_DIR/retention.log"
    grep -q '^--- PASS: TestSystemRuntimeHeartbeatRetention ' "$WORK_DIR/retention.log"
    mysql_admin <<'SQL'
SET GLOBAL log_output='TABLE';
SET GLOBAL general_log=ON;
SQL
    restart_status=0
    python3 "$ROOT_DIR/scripts/test/meta-database-restart-contract.py" "$WORK_DIR" mysql || restart_status=$?
    echo "SET GLOBAL general_log=OFF" | mysql_admin
    test "$(echo "SELECT COUNT(*) FROM mysql.general_log WHERE SUBSTRING_INDEX(user_host,'[',1) = 'worker_gate' AND command_type IN ('Query','Execute') AND CAST(argument AS CHAR CHARACTER SET utf8mb4) REGEXP '^[[:space:]]*(CREATE|DROP|ALTER|TRUNCATE|RENAME)[[:space:]]'" | mysql_admin)" = 0
    test "$(echo "SELECT COUNT(DISTINCT SUBSTRING_INDEX(user_host,'[',1)) FROM mysql.general_log WHERE SUBSTRING_INDEX(user_host,'[',1) = 'worker_gate' AND command_type IN ('Query','Execute')" | mysql_admin)" = 1
    test "$(echo "SELECT COUNT(*) FROM common.background_runtime_heartbeats WHERE module='meta' AND runtime_name='scan' AND stopped_at IS NOT NULL" | mysql_admin)" = 2
    echo 'MySQL: no runtime DDL in the general query log during two starts and stops'
    test "$restart_status" = 0
    test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner='meta' AND version>0 AND pending_version=0" | mysql_admin)" = 1
    echo "DELETE FROM startup_schema_revision WHERE owner='meta'" | mysql_admin
    (cd "$ROOT_DIR/common" && ADDP_TEST_STARTUP_BINARY="$WORK_DIR/meta-server" ADDP_TEST_STARTUP_REASON='schema meta version mismatch: database=0' GOWORK=off go test ./infra/bootstrap -run '^TestMySQLRuntimeStartupRefusal$' -count=1 -v)
    export ADDP_TEST_STARTUP_BINARY="$WORK_DIR/meta-worker"
    (cd "$ROOT_DIR/common" && ADDP_TEST_STARTUP_REASON='schema meta version mismatch: database=0' GOWORK=off go test ./infra/bootstrap -run '^TestMySQLRuntimeStartupRefusal$' -count=1 -v)
    echo "UPDATE common.startup_schema_revision SET pending_version=version+1 WHERE owner='common'" | mysql_admin
    (cd "$ROOT_DIR/common" && ADDP_TEST_STARTUP_REASON='schema common has unfinished MySQL migration' GOWORK=off go test ./infra/bootstrap -run '^TestMySQLRuntimeStartupRefusal$' -count=1 -v)
    exit 0
fi

if [ "$test_group" = platform-database ]; then
    if [ "$local_disposable" = true ]; then
        export ADDP_TEST_MYSQL_PLATFORM_DATABASE=addp_platform_mysql_test
    fi
    : "${ADDP_TEST_MYSQL_PLATFORM_DATABASE:?Use a pre-created disposable platform test database}"
    case "$ADDP_TEST_MYSQL_PLATFORM_DATABASE" in
        addp_ci|addp_platform_mysql_test) ;;
        *) echo 'unsupported platform test database name' >&2; exit 2 ;;
    esac
    (cd "$ROOT_DIR/common" && GOWORK=off go test ./infra/database/mysql -run '^TestPlatformConnectionAgainstMySQL$' -count=1 -timeout=45s -v) \
        | tee "$WORK_DIR/platform-mysql.log"
    grep -q '^--- PASS: TestPlatformConnectionAgainstMySQL ' "$WORK_DIR/platform-mysql.log"
    (cd "$ROOT_DIR/common" && GOWORK=off go test ./domainreference -run '^TestReferencePageAgainstMySQL$' -count=1 -timeout=45s -v) | tee "$WORK_DIR/platform-references.log"
    grep -q '^--- PASS: TestReferencePageAgainstMySQL ' "$WORK_DIR/platform-references.log"
    (cd "$ROOT_DIR/common" && GOWORK=off go test ./schema -run '^TestPlatformMySQLSchemaMigration$' -count=1 -timeout=60s -v) \
        | tee "$WORK_DIR/platform-migration.log"
    grep -q '^--- PASS: TestPlatformMySQLSchemaMigration ' "$WORK_DIR/platform-migration.log"
    (cd "$ROOT_DIR/gateway" && GOWORK=off go test ./internal/middleware -run '^TestGatewayAccessLogAgainstMySQL$' -count=1 -timeout=60s -v) \
        | tee "$WORK_DIR/platform-gateway.log"
    grep -q '^--- PASS: TestGatewayAccessLogAgainstMySQL ' "$WORK_DIR/platform-gateway.log"
    exit 0
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

test_pattern='^TestIntegrationMySQL(TypedJSONWrites|AnalyticalInstance|AnalyticalText|AnalyticalDateBuckets|AnalyticalScan|AnalyticalRelations|AnalyticalCalendar|AnalyticalExpressions|AnalyticalArithmetic|LosslessAnalyticalInteger|BoundedWatermarkResumeAndIdempotentUpsert|DataProtectionReadContracts|GuardedSpatialTableRead|FileGDBSourceShapes|ResultSchema|SharedSQLPool|OutputRewrite|QueryReadSession)$'
if [ "$test_group" = shared-sql-pool ]; then
    test_pattern='^TestIntegrationMySQLSharedSQLPool$'
fi
if [ "$test_group" = typed-write ]; then
    test_pattern='^TestIntegrationMySQLTypedJSONWrites$'
fi
if [ "$test_group" = guarded-spatial-read ]; then
    test_pattern='^TestIntegrationMySQL(GuardedSpatialTableRead|FileGDBSourceShapes)$'
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

if [ "$test_group" = all ] || [ "$test_group" = shared-sql-pool ]; then
    grep -q -- '--- PASS: TestIntegrationMySQLSharedSQLPool ' "$WORK_DIR/common-mysql-data-protection.log" || {
        echo 'MySQL gate requires the native shared SQL pool test' >&2
        exit 1
    }
fi

if [ "$test_group" = all ] || [ "$test_group" = guarded-spatial-read ]; then
    grep -q -- '--- PASS: TestIntegrationMySQLGuardedSpatialTableRead ' "$WORK_DIR/common-mysql-data-protection.log" || {
        echo 'MySQL guarded spatial table read test did not pass' >&2
        exit 1
    }
    grep -q -- '--- PASS: TestIntegrationMySQLFileGDBSourceShapes ' "$WORK_DIR/common-mysql-data-protection.log" || {
        echo 'MySQL FileGDB source shapes test did not pass' >&2
        exit 1
    }
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
