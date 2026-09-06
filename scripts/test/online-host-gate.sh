#!/usr/bin/env bash
# Dedicated-host readiness and lifecycle wrapper for the single ADDP T4 Online gate.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)

fail() {
  echo "Online host gate failed: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: bash scripts/test/online-host-gate.sh [--check-only]

  --check-only  Validate the dedicated Runner boundary without changing services.
EOF
}

MODE=run
case "${1:-}" in
  "") ;;
  --check-only)
    MODE=check-only
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    usage >&2
    fail "unsupported argument: $1"
    ;;
esac
[ "$#" -le 1 ] || fail "only one option is supported"

resolve_path() {
  local path=$1
  python3 - "$path" <<'PY'
import os
import sys

print(os.path.realpath(sys.argv[1]))
PY
}

require_external_path() {
  local label=$1
  local path=$2
  case "$path" in
    "$ROOT_DIR"|"$ROOT_DIR"/*)
      fail "$label 必须位于仓库外"
      ;;
  esac
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] ||
  fail "ADDP_ONLINE_HOST must be exactly 1 on a dedicated runner"
[ "$(uname -s)" = "Darwin" ] || fail "dedicated Online Runner must use macOS"

[ -n "${ADDP_ONLINE_ENV_FILE:-}" ] || fail "ADDP_ONLINE_ENV_FILE is required"
case "$ADDP_ONLINE_ENV_FILE" in
  /*) ;;
  *) fail "ADDP_ONLINE_ENV_FILE must be an absolute path" ;;
esac
[ -f "$ADDP_ONLINE_ENV_FILE" ] || fail "ADDP_ONLINE_ENV_FILE does not exist"
ONLINE_ENV_FILE=$(resolve_path "$ADDP_ONLINE_ENV_FILE") ||
  fail "cannot resolve ADDP_ONLINE_ENV_FILE"
require_external_path "ADDP_ONLINE_ENV_FILE" "$ONLINE_ENV_FILE"

# Lifecycle control belongs to the workflow or direct caller. The external env
# file only supplies deployment configuration and secrets; stale control values
# in that file must never change the selected suite or evidence destination.
REQUESTED_ONLINE_SUITE=${ONLINE_SUITE:-}
REQUESTED_ARTIFACT_DIR=${ADDP_ONLINE_ARTIFACT_DIR:-}
[ -n "$REQUESTED_ONLINE_SUITE" ] || fail "ONLINE_SUITE is required from the caller"
[ -n "$REQUESTED_ARTIFACT_DIR" ] || fail "ADDP_ONLINE_ARTIFACT_DIR is required from the caller"

[ ! -e "$ROOT_DIR/.env" ] ||
  fail "专用 Online Runner 禁止使用仓库根 .env；请使用仓库外 ADDP_ONLINE_ENV_FILE"

set -a
# shellcheck disable=SC1090
source "$ONLINE_ENV_FILE"
set +a

ONLINE_SUITE=$REQUESTED_ONLINE_SUITE
ADDP_ONLINE_ARTIFACT_DIR=$REQUESTED_ARTIFACT_DIR
export ONLINE_SUITE ADDP_ONLINE_ARTIFACT_DIR

case "$ONLINE_SUITE" in
  consumer-engine-recovery)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL MANAGER_URL SERVICE_URL CONSOLE_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_USER_USERNAME
      ADDP_ONLINE_TEST_USER_PASSWORD ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_TEST_ENGINE_ID ADDP_ONLINE_TEST_ENGINE_NAME
      ADDP_ONLINE_TEST_ENGINE_PORT ADDP_ONLINE_TEST_ENGINE_USER
      ADDP_ONLINE_TEST_ENGINE_PASSWORD ADDP_ONLINE_TEST_ENGINE_DATABASE
    )
    ;;
  module-registry-recovery)
    START_TARGET=-system
    REQUIRED_SUITE_ENV=(SYSTEM_URL GATEWAY_URL MANAGER_URL MANAGER_SERVICE_CLIENT_SECRET)
    ;;
  oceanbase-consumer-flow)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL META_URL TRANSFER_URL DEVELOP_URL SERVICE_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_OCEANBASE_ENGINE_ID ADDP_ONLINE_OCEANBASE_PORT
      ADDP_ONLINE_OCEANBASE_DATABASE ADDP_ONLINE_OCEANBASE_USER
      ADDP_ONLINE_OCEANBASE_PASSWORD
    )
    ;;
  manager-internal-artifact-lineage)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL MONITOR_URL CONSOLE_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_USER_USERNAME
      ADDP_ONLINE_TEST_USER_PASSWORD ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID ADDP_ONLINE_MANAGER_MINIO_PORT
      ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY
      ADDP_ONLINE_MANAGER_MINIO_BUCKET ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT
      ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT
    )
    ;;
  security-transfer-protection)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL TRANSFER_URL MANAGER_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_TEST_ENGINE_ID ADDP_ONLINE_TEST_ENGINE_PORT
      ADDP_ONLINE_TEST_ENGINE_USER ADDP_ONLINE_TEST_ENGINE_PASSWORD
      ADDP_ONLINE_TEST_ENGINE_DATABASE
      ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID ADDP_ONLINE_SECURITY_MONGODB_PORT
      ADDP_ONLINE_SECURITY_MONGODB_DATABASE ADDP_ONLINE_SECURITY_MONGODB_USER
      ADDP_ONLINE_SECURITY_MONGODB_PASSWORD ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER
      ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD
    )
    ;;
  security-plaintext-access)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL MANAGER_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN
      ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_TEST_ENGINE_ID ADDP_ONLINE_TEST_ENGINE_PORT
      ADDP_ONLINE_TEST_ENGINE_USER ADDP_ONLINE_TEST_ENGINE_PASSWORD
      ADDP_ONLINE_TEST_ENGINE_DATABASE
      ADDP_ONLINE_SECURITY_MONGODB_PORT ADDP_ONLINE_SECURITY_MONGODB_DATABASE
      ADDP_ONLINE_SECURITY_MONGODB_USER ADDP_ONLINE_SECURITY_MONGODB_PASSWORD
      ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER
      ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD
    )
    ;;
  security-mysql-owner-protection)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL MANAGER_URL DEVELOP_URL
      SERVICE_URL TRANSFER_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_TEST_ENGINE_ID ADDP_ONLINE_TEST_ENGINE_PORT
      ADDP_ONLINE_TEST_ENGINE_USER ADDP_ONLINE_TEST_ENGINE_PASSWORD
      ADDP_ONLINE_TEST_ENGINE_DATABASE
      ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID ADDP_ONLINE_WORKBENCH_MYSQL_PORT
      ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE ADDP_ONLINE_WORKBENCH_MYSQL_USER
      ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD
    )
    ;;
  standard-model-reference-deletion)
    START_TARGET=-model
    REQUIRED_SUITE_ENV=(SYSTEM_URL GATEWAY_URL STANDARD_URL MODEL_URL ADDP_ONLINE_TEST_USER_ACCESS_TOKEN)
    ;;
  enterprise-catalog-publishing)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL META_URL CATALOG_URL ASSET_URL PORTAL_URL CONSOLE_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_USER_USERNAME
      ADDP_ONLINE_TEST_USER_PASSWORD ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_TEST_ENGINE_ID ADDP_ONLINE_TEST_ENGINE_PORT
      ADDP_ONLINE_TEST_ENGINE_USER ADDP_ONLINE_TEST_ENGINE_PASSWORD
      ADDP_ONLINE_TEST_ENGINE_DATABASE ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID
      ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID
    )
    ;;
  workbench-service-consumption)
    START_TARGET=-all
    REQUIRED_SUITE_ENV=(
      SYSTEM_URL GATEWAY_URL SERVICE_URL WORKBENCH_URL CONSOLE_URL
      ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_USER_USERNAME
      ADDP_ONLINE_TEST_USER_PASSWORD ADDP_ONLINE_TEST_TENANT_ID
      ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID ADDP_ONLINE_WORKBENCH_MYSQL_PORT
      ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE ADDP_ONLINE_WORKBENCH_MYSQL_USER
      ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD
    )
    ;;
  *)
    fail "ONLINE_SUITE has no dedicated deployment profile: $ONLINE_SUITE"
    ;;
esac

for variable in "${REQUIRED_SUITE_ENV[@]}"; do
  [ -n "${!variable:-}" ] || fail "$ONLINE_SUITE requires $variable in ADDP_ONLINE_ENV_FILE"
done

case "$ADDP_ONLINE_ARTIFACT_DIR" in
  /*) ;;
  *) fail "ADDP_ONLINE_ARTIFACT_DIR must be an absolute path" ;;
esac
ADDP_ONLINE_ARTIFACT_DIR=$(resolve_path "$ADDP_ONLINE_ARTIFACT_DIR") ||
  fail "cannot resolve ADDP_ONLINE_ARTIFACT_DIR"
export ADDP_ONLINE_ARTIFACT_DIR
require_external_path "ADDP_ONLINE_ARTIFACT_DIR" "$ADDP_ONLINE_ARTIFACT_DIR"

cd "$ROOT_DIR"
[ -z "$(git status --porcelain)" ] ||
  fail "dedicated Online deployment requires a clean repository"

for required_command in bash git python3 make docker go node npm curl lsof nc; do
  command -v "$required_command" >/dev/null 2>&1 ||
    fail "dedicated Online Runner is missing required command: $required_command"
done

python3 scripts/test/online-preflight.py --environment-only >/dev/null ||
  fail "dedicated Online environment boundary validation failed"

mkdir -p "$ADDP_ONLINE_ARTIFACT_DIR"
GATE_LOG="$ADDP_ONLINE_ARTIFACT_DIR/online-gate.log"
SUMMARY="$ADDP_ONLINE_ARTIFACT_DIR/summary.txt"
READINESS="$ADDP_ONLINE_ARTIFACT_DIR/readiness.txt"
: > "$GATE_LOG"

{
  printf 'schema_version=addp.online-host-readiness/v1\n'
  printf 'suite=%s\n' "$ONLINE_SUITE"
  printf 'start_target=%s\n' "$START_TARGET"
  printf 'database=%s\n' "$POSTGRES_DB"
  printf 'result=passed\n'
  printf 'host_os=macOS\n'
  printf 'repository_clean=true\n'
  printf 'env_file_external=true\n'
  printf 'artifact_dir_external=true\n'
  printf 'lifecycle=not-started\n'
} > "$READINESS"

if [ "$MODE" = "check-only" ]; then
  echo "Online dedicated Runner readiness check passed: $ONLINE_SUITE"
  exit 0
fi

cleanup_required=0
process_lifecycle=not-applicable
engine_fixture_cleanup_required=0
engine_restore_required=0
workbench_mysql_cleanup_required=0
oceanbase_consumer_cleanup_required=0
manager_minio_cleanup_required=0
security_transfer_fixture_cleanup_required=0

run_logged() {
  "$@" 2>&1 | tee -a "$GATE_LOG"
}

observe_module_lifecycle() {
  local phase=$1
  local timeout=$2
  local expected_instance_id=${3:-}
  local command=(
    python3 scripts/test/module-lifecycle-process-online.py
    --phase "$phase"
    --manager-url "$MANAGER_URL"
    --system-url "$SYSTEM_URL"
    --gateway-url "$GATEWAY_URL"
    --repository "$ROOT_DIR"
    --timeout "$timeout"
    --output "$ADDP_ONLINE_ARTIFACT_DIR/module-lifecycle-${phase}.json"
  )
  if [ -n "$expected_instance_id" ]; then
    command+=(--expected-instance-id "$expected_instance_id")
  fi
  run_logged "${command[@]}"
}

finish() {
  local gate_status=$?
  local result
  local cleanup=not-required
  trap - EXIT INT TERM

  if [ "$engine_fixture_cleanup_required" -eq 1 ]; then
    if [ "$engine_restore_required" -eq 1 ]; then
      if ! run_logged bash business/scripts/online-engine-fixture.sh start; then
        cleanup=failed
        gate_status=1
      elif ! run_logged python3 scripts/test/consumer-engine-recovery-online.py --restore-only; then
        cleanup=failed
        gate_status=1
      fi
    fi
    if ! run_logged bash business/scripts/online-engine-fixture.sh stop; then
      cleanup=failed
      gate_status=1
    fi
  fi

  if [ "$workbench_mysql_cleanup_required" -eq 1 ]; then
    if ! run_logged bash business/scripts/online-workbench-mysql-fixture.sh stop; then
      cleanup=failed
      gate_status=1
    fi
  fi

  if [ "$manager_minio_cleanup_required" -eq 1 ]; then
    if ! run_logged bash business/scripts/online-manager-minio-fixture.sh stop; then
      cleanup=failed
      gate_status=1
    fi
  fi

  if [ "$security_transfer_fixture_cleanup_required" -eq 1 ]; then
    if ! run_logged bash business/scripts/online-security-transfer-fixture.sh stop; then
      cleanup=failed
      gate_status=1
    fi
  fi

  if [ "$cleanup_required" -eq 1 ]; then
    [ "$cleanup" = "failed" ] || cleanup=passed
    if ! run_logged bash scripts/dev/stop.sh; then
      cleanup=failed
      gate_status=1
    fi
  fi
  if [ "$oceanbase_consumer_cleanup_required" -eq 1 ]; then
    if ! run_logged bash business/scripts/online-oceanbase-consumer-fixture.sh stop; then
      cleanup=failed
      gate_status=1
    fi
  fi
  if [ "$gate_status" -eq 0 ]; then
    result=passed
  else
    result=failed
  fi

  {
    printf 'schema_version=addp.online-host-gate/v1\n'
    printf 'suite=%s\n' "$ONLINE_SUITE"
    printf 'start_target=%s\n' "$START_TARGET"
    printf 'database=%s\n' "$POSTGRES_DB"
    printf 'result=%s\n' "$result"
    printf 'cleanup=%s\n' "$cleanup"
    printf 'process_lifecycle=%s\n' "$process_lifecycle"
  } > "$SUMMARY"
  exit "$gate_status"
}

trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# This is intentionally broad only after the dedicated-host marker and external
# environment boundary have been validated. It removes leftovers from a prior T4 run.
cleanup_required=1
run_logged bash scripts/dev/stop.sh
run_logged bash scripts/infra/up.sh

if [ "$ONLINE_SUITE" = "module-registry-recovery" ]; then
  process_lifecycle=not-run
  process_timeout=${ADDP_ONLINE_PROCESS_TIMEOUT_SECONDS:-60}
  lease_timeout=${ADDP_ONLINE_LEASE_TIMEOUT_SECONDS:-60}

  run_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process --wait-live -manager
  observe_module_lifecycle business-before-system "$process_timeout"
  manager_instance_id=$(python3 - "$ADDP_ONLINE_ARTIFACT_DIR/module-lifecycle-business-before-system.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as evidence:
    print(json.load(evidence)["manager"]["instance_id"])
PY
  )

  run_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  observe_module_lifecycle manager-registered "$process_timeout" "$manager_instance_id"

  run_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
  observe_module_lifecycle gateway-established "$process_timeout" "$manager_instance_id"

  run_logged bash scripts/dev/stop-exact-process.sh -system
  observe_module_lifecycle system-interrupted "$lease_timeout" "$manager_instance_id"

  run_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  observe_module_lifecycle system-recovered "$process_timeout" "$manager_instance_id"

  # Release the real Manager route before the existing two-probe lease and
  # multi-instance scenario claims the same module route prefix.
  run_logged bash scripts/dev/stop-exact-process.sh -manager
  process_lifecycle=passed
elif [ "$ONLINE_SUITE" = "consumer-engine-recovery" ]; then
  process_lifecycle=not-run
  engine_fixture_cleanup_required=1
  engine_restore_required=1
  run_logged bash business/scripts/online-engine-fixture.sh stop
  run_logged bash business/scripts/online-engine-fixture.sh start
  run_logged bash scripts/dev/start.sh
  run_logged npm --prefix console/frontend exec -- playwright install chromium
  run_logged python3 scripts/test/consumer-process-stability-online.py \
    --capture \
    --repository "$ROOT_DIR" \
    --output "$ADDP_ONLINE_ARTIFACT_DIR/consumer-process-stability.json"
elif [ "$ONLINE_SUITE" = "enterprise-catalog-publishing" ]; then
  engine_fixture_cleanup_required=1
  run_logged bash business/scripts/online-engine-fixture.sh stop
  run_logged bash business/scripts/online-engine-fixture.sh start
  run_logged bash scripts/dev/start.sh "$START_TARGET"
  run_logged npm --prefix console/frontend exec -- playwright install chromium
elif [ "$ONLINE_SUITE" = "workbench-service-consumption" ]; then
  workbench_mysql_cleanup_required=1
  run_logged bash business/scripts/online-workbench-mysql-fixture.sh stop
  run_logged bash business/scripts/online-workbench-mysql-fixture.sh start
  run_logged bash scripts/dev/start.sh "$START_TARGET"
  run_logged npm --prefix console/frontend exec -- playwright install chromium
elif [ "$ONLINE_SUITE" = "oceanbase-consumer-flow" ]; then
  oceanbase_consumer_cleanup_required=1
  run_logged bash business/scripts/online-oceanbase-consumer-fixture.sh stop
  run_logged bash business/scripts/online-oceanbase-consumer-fixture.sh start
  run_logged bash scripts/dev/start.sh "$START_TARGET"
elif [ "$ONLINE_SUITE" = "security-mysql-owner-protection" ]; then
  engine_fixture_cleanup_required=1
  workbench_mysql_cleanup_required=1
  run_logged bash business/scripts/online-engine-fixture.sh stop
  run_logged bash business/scripts/online-workbench-mysql-fixture.sh stop
  run_logged bash business/scripts/online-engine-fixture.sh start
  run_logged bash business/scripts/online-workbench-mysql-fixture.sh start
  run_logged bash scripts/dev/start.sh "$START_TARGET"
elif [ "$ONLINE_SUITE" = "manager-internal-artifact-lineage" ]; then
  manager_minio_cleanup_required=1
  run_logged bash business/scripts/online-manager-minio-fixture.sh stop
  run_logged bash business/scripts/online-manager-minio-fixture.sh start
  run_logged bash scripts/dev/start.sh "$START_TARGET"
  run_logged npm --prefix console/frontend exec -- playwright install chromium
elif [ "$ONLINE_SUITE" = "security-transfer-protection" ] ||
  [ "$ONLINE_SUITE" = "security-plaintext-access" ]; then
  security_transfer_fixture_cleanup_required=1
  run_logged bash business/scripts/online-security-transfer-fixture.sh stop
  run_logged bash business/scripts/online-security-transfer-fixture.sh start
  run_logged bash scripts/dev/start.sh "$START_TARGET"
else
  run_logged bash scripts/dev/start.sh "$START_TARGET"
fi

run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"

if [ "$ONLINE_SUITE" = "consumer-engine-recovery" ]; then
  run_logged python3 scripts/test/consumer-process-stability-online.py \
    --verify \
    --repository "$ROOT_DIR" \
    --output "$ADDP_ONLINE_ARTIFACT_DIR/consumer-process-stability.json"
  process_lifecycle=passed
fi
