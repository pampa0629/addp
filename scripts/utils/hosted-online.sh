#!/usr/bin/env bash
# Shared Hosted Online lifecycle. Source after declaring suite and fixture cleanup.
MODE=run

fail() {
  echo "Hosted Online gate failed: $*" >&2
  exit 1
}

case "${1:-}" in
  "") ;;
  --check-only) MODE=check-only ;;
  *) fail "usage: bash Hosted Online gate [--check-only]" ;;
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
  fail "Hosted Online requires Linux x86_64"
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
case "$ADDP_ONLINE_SECRET_DIR" in
  "${RUNNER_TEMP:?RUNNER_TEMP is required}"/addp-online-secret-*) ;;
  *) fail "secret directory must use the Runner temporary addp-online-secret- prefix" ;;
esac
case "$ADDP_ONLINE_ARTIFACT_DIR/" in
  "$ADDP_ONLINE_SECRET_DIR/"*) fail "artifact directory must not be inside secrets" ;;
esac
case "$ADDP_ONLINE_SECRET_DIR/" in
  "$ADDP_ONLINE_ARTIFACT_DIR/"*) fail "secret directory must not be inside artifacts" ;;
esac
[ ! -e "$ADDP_ONLINE_SECRET_DIR" ] || fail "secret directory must be new for this run"
[ "${ADDP_ONLINE_OWNER_MANAGED:-0}" != "1" ] || fail "Hosted and owner-managed profiles are mutually exclusive"


for command in bash curl docker git go make node npm python3; do
  command -v "$command" >/dev/null 2>&1 || fail "missing required command: $command"
done
docker compose version >/dev/null 2>&1 || fail "docker compose is required"
[ -f "$ROOT_DIR/scripts/infra/Dockerfile.postgres" ] ||
  fail "missing repository Infra PostgreSQL Dockerfile"

for container in addp-postgres addp-redis addp-minio addp-meilisearch addp-redpanda "${HOSTED_FIXTURE_CONTAINERS[@]}"; do
  if docker container inspect "$container" >/dev/null 2>&1; then
    fail "refusing to reuse existing container: $container"
  fi
done
for fixture_image in "${HOSTED_FIXTURE_IMAGES[@]:-}"; do
  [ -n "$fixture_image" ] || continue
  if docker image inspect "$fixture_image" >/dev/null 2>&1; then
    fail "refusing to reuse existing image: $fixture_image"
  fi
done

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
  echo "Hosted Online readiness check passed"
  exit 0
fi

: > "$GATE_LOG"
infra_owned=0
application_owned=0
fixture_owned=0

run_logged() {
  "$@" 2>&1 | tee -a "$GATE_LOG"
}

run_daemon_launcher_logged() {
  local previous_size command_status=0
  previous_size=$(wc -c < "$GATE_LOG")
  "$@" >> "$GATE_LOG" 2>&1 || command_status=$?
  tail -c "+$((previous_size + 1))" "$GATE_LOG"
  return "$command_status"
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
    stop_online_fixture || cleanup=failed
  fi
  if [ "$infra_owned" -eq 1 ]; then
    run_logged bash scripts/infra/down.sh --volumes --force || cleanup=failed
  fi
  case "$ADDP_ONLINE_SECRET_DIR" in
    "${RUNNER_TEMP:-/tmp}"/addp-online-secret-*) rm -rf "$ADDP_ONLINE_SECRET_DIR" ;;
    *) cleanup=failed ;;
  esac
  [ ! -e "$ADDP_ONLINE_SECRET_DIR" ] || cleanup=failed
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
# Non-production defaults, only inside a verified disposable Hosted deployment.
source "$ROOT_DIR/.env.example"
set +a
export ADDP_ONLINE_HOST=1 ADDP_ONLINE_HOSTED=1 ADDP_ONLINE_TEST=1
export ONLINE_SUITE POSTGRES_DB=addp_online SERVICE_HOST=127.0.0.1
export SYSTEM_URL=http://127.0.0.1:8180 GATEWAY_URL=http://127.0.0.1:8000
export META_URL=http://127.0.0.1:8082 MANAGER_URL=http://127.0.0.1:8081
export TRANSFER_URL=http://127.0.0.1:8083 DEVELOP_URL=http://127.0.0.1:8185
export SERVICE_URL=http://127.0.0.1:8086
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=900
IDENTITY_ENV="$ADDP_ONLINE_SECRET_DIR/identity.env"
ENGINE_RESULT_ENV="$ADDP_ONLINE_SECRET_DIR/engine.env"
mkdir -p "$ADDP_ONLINE_SECRET_DIR"
chmod 700 "$ADDP_ONLINE_SECRET_DIR"
