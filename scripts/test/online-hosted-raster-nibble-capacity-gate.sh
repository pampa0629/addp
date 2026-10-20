#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=raster-nibble-capacity
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Independent nibble capacity; the original Raster T4 and budget are unchanged.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=raster-nibble-capacity
RUNTIME_CONTAINER=addp-raster-nibble-capacity-runtime
RUNTIME_TAG=raster-nibble-capacity
RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
HOSTED_FIXTURE_CONTAINERS=(addp-raster-nibble-capacity-registry "$RUNTIME_CONTAINER")
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim)
export ADDP_ONLINE_NIBBLE_CAPACITY_RUNTIME_IMAGE="$RUNTIME_IMAGE"
registry_owned=0
stop_online_fixture() {
  local status=0 image
  run_logged python3 business/scripts/online-raster-nibble-capacity-fixture.py stop || status=1
  if [ "$registry_owned" -eq 1 ]; then
    run_logged docker rm -fv addp-raster-nibble-capacity-registry || status=1
    if docker container inspect addp-raster-nibble-capacity-registry >/dev/null 2>&1; then status=1; fi
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
  [ "$#" -eq 1 ] && [ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] && [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] && [ "${ADDP_ONLINE_TEST:-}" = 1 ] || exit 1
  python3 business/scripts/online-raster-nibble-capacity-fixture.py check-root >/dev/null
  [ -n "${ADDP_ONLINE_ARTIFACT_DIR:-}" ] && [ -n "${GEOPYTHON_WORKFLOW_PORT:-}" ]
  python3 - <<'CHECK_PATHS'
import os
from pathlib import Path
artifact = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
secret = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
runner = Path(os.environ['RUNNER_TEMP'])
assert artifact.is_absolute() and artifact.resolve() == artifact and artifact.is_dir()
assert artifact.stat().st_uid == os.getuid() and runner in artifact.parents
assert secret != artifact and secret not in artifact.parents and artifact not in secret.parents
assert os.environ['ADDP_ONLINE_TEST_RUN_ID'] == f"nibble-capacity-{os.environ['GITHUB_RUN_ID']}-{os.environ['GITHUB_RUN_ATTEMPT']}"
CHECK_PATHS
  if docker container inspect "$RUNTIME_CONTAINER" >/dev/null 2>&1; then exit 1; fi
  NIBBLE_CAPACITY_ROOT=$(cat "$ADDP_ONLINE_SECRET_DIR/nibble-capacity-root")
  run_logged() { "$@" >> "$ADDP_ONLINE_ARTIFACT_DIR/nibble-capacity-runtime-start.log" 2>&1; }
  fail() { echo 'Nibble capacity Runtime launch verification failed' >&2; exit 1; }
  source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"
  launch_online_geopython_runtime --cpus 4 --memory 2g --memory-swap 2g \
    --mount "type=bind,source=$NIBBLE_CAPACITY_ROOT,target=$NIBBLE_CAPACITY_ROOT" \
    -e "TMPDIR=$NIBBLE_CAPACITY_ROOT/runtime-tmp"
  exit 0
fi
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800
export ADDP_ONLINE_TEST_RUN_ID="nibble-capacity-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export MONITOR_URL=http://127.0.0.1:8100 SECURITY_URL=http://127.0.0.1:8194
fixture_owned=1
registry_owned=1
run_logged docker run -d --name addp-raster-nibble-capacity-registry --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'nibble registry readiness timed out'
  sleep 1
done
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged python3 business/scripts/online-raster-nibble-capacity-fixture.py start
NIBBLE_CAPACITY_ROOT=$(cat "$ADDP_ONLINE_SECRET_DIR/nibble-capacity-root")
application_owned=1
for start_target in -security -develop -manager -monitor; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_geopython_runtime "$RUNTIME_TAG" --cpus 4 --memory 2g --memory-swap 2g \
  --mount "type=bind,source=$NIBBLE_CAPACITY_ROOT,target=$NIBBLE_CAPACITY_ROOT" \
  -e "TMPDIR=$NIBBLE_CAPACITY_ROOT/runtime-tmp"
run_logged python3 business/scripts/online-raster-nibble-capacity-fixture.py seed
# Reuse the existing ordinary raster identity and its exact twelve permissions.
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite raster-workflow --wait-protection-owners develop,manager --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/nibble-capacity-nfs-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_NIBBLE_CAPACITY_NFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged env -u POSTGRES_PASSWORD make test-online "ONLINE_SUITE=$ONLINE_SUITE"
