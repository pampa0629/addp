#!/usr/bin/env bash
# Shared Hosted Online lifecycle. Source after declaring suite and fixture cleanup.
MODE=run

# The two distributed Spark suites use the product builder's owned mirror.
spark_registry_owned=0
start_online_spark_image_registry() {
  spark_registry_owned=1
  run_logged docker run -d --name "$HOSTED_SPARK_REGISTRY" --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
  local attempt
  for attempt in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then return 0; fi
    [ "$attempt" -lt 30 ] || fail 'image registry readiness timed out'
    sleep 1
  done
}

stop_online_spark_image_registry() {
  [ "$spark_registry_owned" -eq 1 ] || return 0
  local status=0 image
  if docker container inspect "$HOSTED_SPARK_REGISTRY" >/dev/null 2>&1; then
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-runtime" }}' "$HOSTED_SPARK_REGISTRY")" = "$ONLINE_SUITE" ] || return 1
    run_logged docker rm -fv "$HOSTED_SPARK_REGISTRY" || status=1
    if docker container inspect "$HOSTED_SPARK_REGISTRY" >/dev/null 2>&1; then status=1; fi
  fi
  for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      run_logged docker image rm "$image" || status=1
      if docker image inspect "$image" >/dev/null 2>&1; then status=1; fi
    fi
  done
  return "$status"
}

runtime_owned=0
remove_online_product_container() {
  local container=$1 status=0
  if docker container inspect "$container" >/dev/null 2>&1; then
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-runtime" }}' "$container")" = "$ONLINE_SUITE" ] || return 1
    run_logged docker rm -fv "$container" || status=1
    if docker container inspect "$container" >/dev/null 2>&1; then status=1; fi
  fi
  return "$status"
}
stop_online_product_runtime() {
  local status=0
  if [ "$runtime_owned" -eq 1 ]; then
    if docker container inspect "$RUNTIME_CONTAINER" >/dev/null 2>&1 &&
      [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-runtime" }}' "$RUNTIME_CONTAINER")" = "$ONLINE_SUITE" ]; then
      run_logged docker logs "$RUNTIME_CONTAINER" > "$ADDP_ONLINE_ARTIFACT_DIR/${RUNTIME_REPORT}-runtime.log" || status=1
    fi
    remove_online_product_container "$RUNTIME_CONTAINER" || status=1
  fi
  return "$status"
}

start_online_spark_runtime() {
  local tag=$1
  RUNTIME_REPORT=$2
  run_logged make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --tag $tag --verify --jobs 1"
  runtime_owned=1
  export ADDP_ONLINE_SPARK_RUNTIME_URL="http://127.0.0.1:$SPARK_WORKFLOW_PORT"
  # No command override: exercise exactly the product image's default entry.
  run_logged docker run -d --name "$RUNTIME_CONTAINER" --network host \
    --label "com.addp.online-runtime=$ONLINE_SUITE" \
    -e "PORT=$SPARK_WORKFLOW_PORT" -e WORKFLOW_BIND_HOST=127.0.0.1 -e SYSTEM_URL -e SPARK_WORKFLOW_SERVICE_CLIENT_SECRET \
    -e HADOOP_USER_NAME -e SPARK_WORKFLOW_SHARED_HOST -e RUNTIME_HOST=127.0.0.1 "$RUNTIME_IMAGE"
  verify_online_product_runtime '["python","api_server.py"]' "$ADDP_ONLINE_SPARK_RUNTIME_URL/health"
}

start_online_geopython_runtime() {
  local tag=$1
  RUNTIME_REPORT=raster
  run_logged make build-images IMAGE_BUILD_ARGS="--services geopython-workflow-engine --tag $tag --verify --jobs 1"
  runtime_owned=1
  run_logged docker run -d --name "$RUNTIME_CONTAINER" --network host \
    --label "com.addp.online-runtime=$ONLINE_SUITE" \
    -e "PORT=$GEOPYTHON_WORKFLOW_PORT" -e WORKFLOW_BIND_HOST=127.0.0.1 \
    -e SYSTEM_URL -e GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET -e RUNTIME_HOST=127.0.0.1 \
    -e GEOPYTHON_WORKFLOW_LOOPBACK_HOST=127.0.0.1 "$RUNTIME_IMAGE"
  verify_online_product_runtime '["sh","container_entrypoint.sh"]' "http://127.0.0.1:$GEOPYTHON_WORKFLOW_PORT/health"
}

verify_online_product_runtime() {
  local command=$1 health_url=$2 image_id attempt default_entry
  image_id=$(docker image inspect -f '{{.Id}}' "$RUNTIME_IMAGE")
  [ "$(docker inspect -f '{{.Image}}' "$RUNTIME_CONTAINER")" = "$image_id" ] || fail 'Runtime image identity mismatch'
  [ "$(docker inspect -f '{{json .Config.Cmd}}' "$RUNTIME_CONTAINER")" = "$command" ] || fail 'Runtime default entry mismatch'
  default_entry=$(python3 -c 'import json,sys; print(" ".join(json.loads(sys.argv[1])))' "$command")
  printf 'image_id=%s\ngit_commit=%s\ndefault_entry=%s\n' "$image_id" "$(git rev-parse HEAD)" "$default_entry" > "$ADDP_ONLINE_ARTIFACT_DIR/${RUNTIME_REPORT}-runtime-build.txt"
  for attempt in $(seq 1 120); do
    if curl -fsS "$health_url" >/dev/null; then break; fi
    [ "$attempt" -lt 120 ] || fail 'Runtime readiness timed out'
    sleep 1
  done
  [ "$(docker inspect -f '{{.State.Running}}' "$RUNTIME_CONTAINER")" = true ] || fail 'Runtime container exited'
}

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
[ -z "${COMPOSE_PROJECT_NAME:-}" ] || fail "Hosted Online uses the Infra Compose project name without overrides"

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


for command in bash curl docker git go lsof make node npm python3 sudo; do
  command -v "$command" >/dev/null 2>&1 || fail "missing required command: $command"
done
docker compose version >/dev/null 2>&1 || fail "docker compose is required"
[ -f "$ROOT_DIR/scripts/infra/Dockerfile.postgres" ] ||
  fail "missing repository Infra PostgreSQL Dockerfile"

for container in addp-postgres addp-redis addp-falkordb addp-minio addp-meilisearch addp-redpanda addp-redpanda-init addp-kafka-connect "${HOSTED_FIXTURE_CONTAINERS[@]:-}"; do
  [ -n "$container" ] || continue
  if docker container inspect "$container" >/dev/null 2>&1; then
    fail "refusing to reuse existing container: $container"
  fi
done

# A fresh container set is insufficient: a retained PG/graph volume would reuse
# another run's identities and history, then be deleted by our exit trap.
verify_empty_infra() {
  local remaining kind
  remaining=$(docker ps -aq --filter label=com.docker.compose.project=addp-infra) || return 1
  [ -z "$remaining" ] || return 1
  for kind in network volume; do
    remaining=$(docker "$kind" ls -q --filter label=com.docker.compose.project=addp-infra) || return 1
    [ -z "$remaining" ] || return 1
  done
}
verify_empty_infra || fail "refusing existing or unverifiable addp-infra containers, networks or volumes"
for hosted_project in "${HOSTED_FIXTURE_COMPOSE_PROJECTS[@]:-}"; do
  [ -n "$hosted_project" ] || continue
  hosted_remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$hosted_project") ||
    fail "cannot inspect Compose project: $hosted_project"
  [ -z "$hosted_remaining" ] ||
    fail "refusing existing Compose project: $hosted_project"
  for kind in network volume; do
    hosted_remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$hosted_project") ||
      fail "cannot inspect Compose project $kind: $hosted_project"
    [ -z "$hosted_remaining" ] ||
      fail "refusing existing Compose project $kind: $hosted_project"
  done
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

stop_online_application() {
  local status=0
  stop_online_product_runtime || status=1
  run_logged bash scripts/dev/stop.sh || status=1
  return "$status"
}

finish() {
  local status=$?
  local cleanup=passed
  local infra_cleanup=not_started
  trap - EXIT INT TERM
  set +e
  if [ "$application_owned" -eq 1 ]; then
    stop_online_application || cleanup=failed
  fi
  if [ "$fixture_owned" -eq 1 ]; then
    stop_online_fixture || cleanup=failed
  fi
  if [ "$infra_owned" -eq 1 ]; then
    run_logged bash scripts/infra/down.sh --volumes --force || cleanup=failed
    if verify_empty_infra; then
      infra_cleanup=zero_residuals
    else
      infra_cleanup=failed
      cleanup=failed
    fi
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
    printf 'infra_cleanup=%s\n' "$infra_cleanup"
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
export INFRA_BIND_HOST=127.0.0.1
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

export PROJECT_ROOT="$ROOT_DIR"
export ADDP_ONLINE_ENV_FILE="$ADDP_ONLINE_SECRET_DIR/runtime.env"
source "$ROOT_DIR/scripts/utils/observability-env.sh"
addp_prepare_observability_env "$ADDP_ONLINE_ENV_FILE"
