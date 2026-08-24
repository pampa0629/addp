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
  module-registry-recovery)
    START_TARGET=-system
    REQUIRED_SUITE_ENV=(SYSTEM_URL GATEWAY_URL MANAGER_SERVICE_CLIENT_SECRET)
    ;;
  standard-model-reference-deletion)
    START_TARGET=-model
    REQUIRED_SUITE_ENV=(SYSTEM_URL GATEWAY_URL STANDARD_URL MODEL_URL ADDP_ONLINE_TEST_USER_ACCESS_TOKEN)
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

for required_command in bash git python3 make docker go node npm curl; do
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

run_logged() {
  "$@" 2>&1 | tee -a "$GATE_LOG"
}

finish() {
  local gate_status=$?
  local result
  local cleanup=not-required
  trap - EXIT INT TERM

  if [ "$cleanup_required" -eq 1 ]; then
    cleanup=passed
    if ! run_logged bash scripts/dev/stop.sh; then
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
run_logged bash scripts/dev/start.sh "$START_TARGET"
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
