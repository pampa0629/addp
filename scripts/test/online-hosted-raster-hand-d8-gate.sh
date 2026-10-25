#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=raster-hand-d8-persistence
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Independent hand-d8 persistence; the original Raster T4 and budget are unchanged.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=raster-hand-d8-persistence
RUNTIME_CONTAINER=addp-raster-hand-d8-runtime
RUNTIME_TAG=raster-hand-d8
RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
HOSTED_FIXTURE_CONTAINERS=(addp-raster-hand-d8-registry "$RUNTIME_CONTAINER")
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim)
registry_owned=0
stop_online_fixture() {
  local status=0 image
  run_logged python3 business/scripts/online-raster-hand-d8-fixture.py stop || status=1
  if [ "$registry_owned" -eq 1 ]; then
    run_logged docker rm -fv addp-raster-hand-d8-registry || status=1
    if docker container inspect addp-raster-hand-d8-registry >/dev/null 2>&1; then status=1; fi
  fi
  for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      run_logged docker image rm "$image" || status=1
      if docker image inspect "$image" >/dev/null 2>&1; then status=1; fi
    fi
  done
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600
export ADDP_ONLINE_TEST_RUN_ID="hand-d8-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export MONITOR_URL=http://127.0.0.1:8100 SECURITY_URL=http://127.0.0.1:8194
fixture_owned=1
registry_owned=1
run_logged docker run -d --name addp-raster-hand-d8-registry --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'hand-d8 registry readiness timed out'
  sleep 1
done
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged python3 business/scripts/online-raster-hand-d8-fixture.py start
HAND_D8_ROOT=$(cat "$ADDP_ONLINE_SECRET_DIR/hand-d8-root")
application_owned=1
for start_target in -security -develop -manager -monitor; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_geopython_runtime "$RUNTIME_TAG" --mount "type=bind,source=$HAND_D8_ROOT,target=$HAND_D8_ROOT"
run_logged python3 business/scripts/online-raster-hand-d8-fixture.py seed
# Reuse the existing ordinary raster identity and its exact twelve permissions.
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite raster-workflow --wait-protection-owners develop,manager --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/hand-d8-nfs-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_HAND_D8_NFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged env -u POSTGRES_PASSWORD make test-online "ONLINE_SUITE=$ONLINE_SUITE"
