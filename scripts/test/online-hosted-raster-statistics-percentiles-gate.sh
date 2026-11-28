#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=raster-statistics-percentiles-workflow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Independent read-only full-raster percentile workflow; the original Raster T4 and budget are unchanged.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=raster-statistics-percentiles-workflow
RUNTIME_CONTAINER=addp-raster-statistics-percentiles-runtime
RUNTIME_TAG=raster-statistics-percentiles
RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
HOSTED_FIXTURE_CONTAINERS=(addp-raster-statistics-percentiles-registry "$RUNTIME_CONTAINER")
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim)
registry_owned=0
stop_online_fixture() {
  local status=0 image
  run_logged python3 business/scripts/online-raster-statistics-percentiles-fixture.py stop || status=1
  if [ "$registry_owned" -eq 1 ]; then
    run_logged docker rm -fv addp-raster-statistics-percentiles-registry || status=1
    if docker container inspect addp-raster-statistics-percentiles-registry >/dev/null 2>&1; then status=1; fi
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
export ADDP_ONLINE_TEST_RUN_ID="statistics-percentiles-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export MONITOR_URL=http://127.0.0.1:8100 SECURITY_URL=http://127.0.0.1:8194
fixture_owned=1
registry_owned=1
run_logged docker run -d --name addp-raster-statistics-percentiles-registry --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'statistics-percentiles registry readiness timed out'
  sleep 1
done
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
hosted_start_platform_infra
run_logged python3 business/scripts/online-raster-statistics-percentiles-fixture.py start
STATISTICS_PERCENTILES_ROOT=$(cat "$ADDP_ONLINE_SECRET_DIR/statistics-percentiles-root")
application_owned=1
hosted_start_native_modules -security -develop -manager -monitor
start_online_geopython_runtime "$RUNTIME_TAG" --mount "type=bind,source=$STATISTICS_PERCENTILES_ROOT,target=$STATISTICS_PERCENTILES_ROOT"
export ADDP_ONLINE_STATISTICS_PERCENTILES_STARTED
ADDP_ONLINE_STATISTICS_PERCENTILES_STARTED=$(python3 -c 'import time; print(time.monotonic())')
run_business() {
  local remaining
  remaining=$(python3 - <<'CLOCK'
import os
import time

started = float(os.environ['ADDP_ONLINE_STATISTICS_PERCENTILES_STARTED'])
remaining = 600 - (time.monotonic() - started)
if remaining <= 0:
    raise SystemExit('statistics business budget exhausted')
print(remaining)
CLOCK
  )
  run_logged timeout --kill-after=5 "$remaining" "$@"
}
run_business python3 business/scripts/online-raster-statistics-percentiles-fixture.py seed
# Reuse the existing ordinary raster identity and its exact twelve permissions.
run_business bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite raster-workflow --wait-protection-owners develop,manager --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_business python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/statistics-percentiles-nfs-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_STATISTICS_PERCENTILES_NFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_business env -u POSTGRES_PASSWORD make test-online "ONLINE_SUITE=$ONLINE_SUITE"
