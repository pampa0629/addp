#!/usr/bin/env bash
# Manual complete-scene Raster capacity; preserves the original correctness suite.
# ADDP_ONLINE_SUITES=raster-workflow-capacity
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=raster-workflow-capacity
RUNTIME_CONTAINER=addp-raster-capacity-runtime
RUNTIME_TAG=raster-capacity
RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
export ADDP_ONLINE_RASTER_RUNTIME_IMAGE="$RUNTIME_IMAGE"
HOSTED_FIXTURE_CONTAINERS=(addp-raster-capacity-source addp-raster-capacity-target addp-raster-capacity-worker addp-raster-capacity-registry "$RUNTIME_CONTAINER")
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim)
registry_owned=0
stop_online_fixture() {
  local status=0 image
  run_logged python3 business/scripts/online-raster-capacity-fixture.py stop || status=1
  if [ "$registry_owned" -eq 1 ]; then
    run_logged docker rm -fv addp-raster-capacity-registry || status=1
    if docker container inspect addp-raster-capacity-registry >/dev/null 2>&1; then status=1; fi
  fi
  for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      run_logged docker image rm "$image" || status=1
      if docker image inspect "$image" >/dev/null 2>&1; then status=1; fi
    fi
  done
  return "$status"
}
if [ "${1:-}" = --start-runtime ]; then
  [ "$#" -eq 1 ] && [ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] && [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] && [ "${ADDP_ONLINE_TEST:-}" = 1 ] && [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || exit 1
  [ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] && [ -d "${ADDP_ONLINE_ARTIFACT_DIR:?}" ] && [ -n "${ADDP_ONLINE_TEST_RUN_ID:?}" ]
  case "$ADDP_ONLINE_SECRET_DIR" in "${RUNNER_TEMP:?}"/addp-online-secret-*) ;; *) exit 1 ;; esac
  python3 - <<'PY'
import os, pathlib, stat
secret=pathlib.Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
artifacts=pathlib.Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
assert secret.is_absolute() and artifacts.is_absolute()
assert secret.resolve()==secret and artifacts.resolve()==artifacts
assert secret.stat().st_uid==os.getuid() and stat.S_IMODE(secret.stat().st_mode)==0o700
assert secret!=artifacts and secret not in artifacts.parents and artifacts not in secret.parents
assert (secret/'capacity-fixture.json').is_file()
assert os.environ['ADDP_ONLINE_TEST_RUN_ID']==f"raster-capacity-{os.environ['GITHUB_RUN_ID']}-{os.environ['GITHUB_RUN_ATTEMPT']}"
PY
  if docker container inspect "$RUNTIME_CONTAINER" >/dev/null 2>&1; then exit 1; fi
  run_logged() { "$@" >> "$ADDP_ONLINE_ARTIFACT_DIR/capacity-runtime-start.log" 2>&1; }
  fail() { echo 'Capacity Runtime launch verification failed' >&2; exit 1; }
  source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"
  launch_online_geopython_runtime --cpus 4 --memory 2g --memory-swap 2g
  exit 0
fi
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800
export ADDP_ONLINE_TEST_RUN_ID="raster-capacity-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export MONITOR_URL=http://127.0.0.1:8100 CONSOLE_URL=http://127.0.0.1:5170
fixture_owned=1
registry_owned=1
run_logged docker run -d --name addp-raster-capacity-registry --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'capacity registry readiness timed out'
  sleep 1
done
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged python3 business/scripts/online-raster-capacity-fixture.py start
application_owned=1
for start_target in -develop -manager -monitor; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_geopython_runtime "$RUNTIME_TAG" --cpus 4 --memory 2g --memory-swap 2g
run_logged python3 business/scripts/online-raster-capacity-fixture.py seed
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite raster-workflow-capacity --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
for role in source target; do
  run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/raster-$role-engine.json" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  if [ "$role" = source ]; then export ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID";
  else export ADDP_ONLINE_RASTER_TARGET_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"; fi
done
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged env -u POSTGRES_PASSWORD make test-online "ONLINE_SUITE=$ONLINE_SUITE"
