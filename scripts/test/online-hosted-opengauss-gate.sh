#!/usr/bin/env bash
# GitHub Hosted Linux lifecycle for the openGauss cross-module T4 Online gate.
# ADDP_ONLINE_SUITES=opengauss-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
ONLINE_SUITE=opengauss-consumer-flow
MODE=run
# shellcheck source=../lib/opengauss-official-media.sh
source "$ROOT_DIR/scripts/lib/opengauss-official-media.sh"

fail() {
  echo "Hosted openGauss Online gate failed: $*" >&2
  exit 1
}

case "${1:-}" in
  "") ;;
  --check-only) MODE=check-only ;;
  *) fail "usage: bash scripts/test/online-hosted-opengauss-gate.sh [--check-only]" ;;
esac
[ "$#" -le 1 ] || fail "only one option is supported"

resolve_path() {
  python3 - "$1" <<'PY'
import os
import sys
print(os.path.realpath(sys.argv[1]))
PY
}

require_external_path() {
  local label=$1
  local path=$2
  case "$path" in
    "$ROOT_DIR"|"$ROOT_DIR"/*) fail "$label must be outside the repository" ;;
  esac
}

[ "${GITHUB_ACTIONS:-}" = "true" ] || fail "GITHUB_ACTIONS must be true"
[ "${RUNNER_OS:-}" = "Linux" ] || fail "RUNNER_OS must be Linux"
[ "$(uname -s)" = "Linux" ] && [ "$(uname -m)" = "x86_64" ] ||
  fail "openGauss Hosted Online requires Linux x86_64"
[ "${ADDP_ONLINE_HOSTED:-}" = "1" ] || fail "ADDP_ONLINE_HOSTED must be exactly 1"
[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "${ONLINE_SUITE_INPUT:-$ONLINE_SUITE}" = "$ONLINE_SUITE" ] ||
  fail "this lifecycle only owns $ONLINE_SUITE"
[ ! -e "$ROOT_DIR/.env" ] || fail "Hosted Online forbids a repository root .env"
[ -z "$(git -C "$ROOT_DIR" status --porcelain)" ] || fail "Hosted Online requires a clean checkout"

for variable in ADDP_ONLINE_ARTIFACT_DIR ADDP_ONLINE_SECRET_DIR; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
  case "${!variable}" in
    /*) ;;
    *) fail "$variable must be absolute" ;;
  esac
  resolved=$(resolve_path "${!variable}") || fail "cannot resolve $variable"
  printf -v "$variable" '%s' "$resolved"
  export "$variable"
  require_external_path "$variable" "$resolved"
done
[ "$ADDP_ONLINE_ARTIFACT_DIR" != "$ADDP_ONLINE_SECRET_DIR" ] ||
  fail "artifact and secret directories must be different"

for command in bash curl docker git go make node npm python3; do
  command -v "$command" >/dev/null 2>&1 || fail "missing required command: $command"
done
docker compose version >/dev/null 2>&1 || fail "docker compose is required"

for container in addp-postgres addp-redis addp-minio addp-meilisearch addp-redpanda addp-opengauss-online-disposable; do
  if docker container inspect "$container" >/dev/null 2>&1; then
    fail "refusing to reuse existing container: $container"
  fi
done
if docker image inspect "$OPENGAUSS_OFFICIAL_IMAGE" >/dev/null 2>&1; then
  fail "refusing to reuse existing image: $OPENGAUSS_OFFICIAL_IMAGE"
fi

mkdir -p "$ADDP_ONLINE_ARTIFACT_DIR"
READINESS="$ADDP_ONLINE_ARTIFACT_DIR/readiness.txt"
SUMMARY="$ADDP_ONLINE_ARTIFACT_DIR/summary.txt"
GATE_LOG="$ADDP_ONLINE_ARTIFACT_DIR/online-gate.log"
{
  printf 'schema_version=addp.online-host-readiness/v1\n'
  printf 'suite=%s\n' "$ONLINE_SUITE"
  printf 'runner=github-hosted-linux-x86_64\n'
  printf 'database=addp_online\n'
  printf 'result=passed\n'
  printf 'repository_clean=true\n'
  printf 'artifact_dir_external=true\n'
  printf 'secret_dir_external=true\n'
  printf 'lifecycle=not-started\n'
} > "$READINESS"

if [ "$MODE" = "check-only" ]; then
  echo "Hosted openGauss Online readiness check passed"
  exit 0
fi

: > "$GATE_LOG"
infra_owned=0
application_owned=0
fixture_owned=0

run_logged() {
  "$@" 2>&1 | tee -a "$GATE_LOG"
}

finish() {
  local status=$?
  local cleanup=passed
  trap - EXIT INT TERM
  set +e
  if [ "$application_owned" -eq 1 ]; then
    run_logged bash scripts/dev/stop.sh || cleanup=failed
  fi
  if [ "$fixture_owned" -eq 1 ]; then
    run_logged bash business/scripts/online-opengauss-consumer-fixture.sh stop || cleanup=failed
  fi
  if [ "$infra_owned" -eq 1 ]; then
    run_logged bash scripts/infra/down.sh --volumes --force || cleanup=failed
  fi
  case "$ADDP_ONLINE_SECRET_DIR" in
    "${RUNNER_TEMP:-/tmp}"/addp-online-secret-*) rm -rf "$ADDP_ONLINE_SECRET_DIR" ;;
    *) cleanup=failed ;;
  esac
  if [ "$cleanup" != "passed" ]; then
    status=1
  fi
  if [ "$status" -eq 0 ]; then
    result=passed
  else
    result=failed
  fi
  {
    printf 'schema_version=addp.online-host-gate/v1\n'
    printf 'suite=%s\n' "$ONLINE_SUITE"
    printf 'runner=github-hosted-linux-x86_64\n'
    printf 'database=addp_online\n'
    printf 'result=%s\n' "$result"
    printf 'cleanup=%s\n' "$cleanup"
  } > "$SUMMARY"
  exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd "$ROOT_DIR"
set -a
# The checked-in example provides non-production disposable defaults. Hosted
# control values below replace database identity and all runtime addresses.
# shellcheck disable=SC1091
source "$ROOT_DIR/.env.example"
set +a
export ADDP_ONLINE_HOST=1
export ADDP_ONLINE_HOSTED=1
export ADDP_ONLINE_TEST=1
export ONLINE_SUITE
export POSTGRES_DB=addp_online
export SERVICE_HOST=127.0.0.1
export SYSTEM_URL=http://127.0.0.1:8180
export GATEWAY_URL=http://127.0.0.1:8000
export META_URL=http://127.0.0.1:8082
export MANAGER_URL=http://127.0.0.1:8081
export TRANSFER_URL=http://127.0.0.1:8083
export DEVELOP_URL=http://127.0.0.1:8185
export SERVICE_URL=http://127.0.0.1:8086
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=900
export ADDP_OPENGAUSS_MEDIA_CACHE="${RUNNER_TEMP:-/tmp}/addp-opengauss-media-cache"
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/opengauss-engine.json"
IDENTITY_ENV="$ADDP_ONLINE_SECRET_DIR/identity.env"
ENGINE_RESULT_ENV="$ADDP_ONLINE_SECRET_DIR/engine.env"
unset ADDP_TEST_OPENGAUSS_DSN ADDP_TEST_OPENGAUSS_HOST ADDP_TEST_OPENGAUSS_PORT
unset ADDP_TEST_OPENGAUSS_DATABASE ADDP_TEST_OPENGAUSS_USER ADDP_TEST_OPENGAUSS_PASSWORD
mkdir -p "$ADDP_ONLINE_SECRET_DIR"
chmod 700 "$ADDP_ONLINE_SECRET_DIR"

infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-opengauss-consumer-fixture.sh start
application_owned=1
for start_target in -manager -develop -service; do
  run_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done

run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --output "$1"' _ "$IDENTITY_ENV"
# shellcheck disable=SC1090
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" \
  --output "$ENGINE_RESULT_ENV"
# shellcheck disable=SC1090
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN

export ADDP_ONLINE_CONSUMER_ENGINE_TYPE=opengauss
export ADDP_ONLINE_CONSUMER_NAMESPACE=public
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
