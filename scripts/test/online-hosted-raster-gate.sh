#!/usr/bin/env bash
# Disposable raster workflow, automatic metadata/lineage and Console acceptance.
# ADDP_ONLINE_SUITES=raster-workflow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=raster-workflow
HOSTED_FIXTURE_CONTAINERS=(addp-raster-source addp-raster-target addp-raster-fixture-worker addp-raster-registry geopython-workflow-engine)
HOSTED_FIXTURE_IMAGES=(addp-geopython-workflow-engine:dev localhost:5001/python:3.11-slim)
registry_owned=0
stop_online_fixture() {
  local status=0 image
  run_logged python3 business/scripts/online-raster-minio-fixture.py stop || status=1
  if [ "$registry_owned" -eq 1 ]; then
    run_logged docker rm -fv addp-raster-registry || status=1
    if docker container inspect addp-raster-registry >/dev/null 2>&1; then status=1; fi
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
# This manual raster scene verifies 20 sequential browsers plus byte/lineage checks.
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=1200
export MONITOR_URL=http://127.0.0.1:8100 CONSOLE_URL=http://127.0.0.1:5170
export ADDP_ONLINE_TEST_RUN_ID="raster-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
# Base-image mirror only; Runtime build and startup remain owned by start.sh.
fixture_owned=1
registry_owned=1
run_logged docker run -d --name addp-raster-registry -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail "raster image registry readiness timed out"
  sleep 1
done
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged python3 business/scripts/online-raster-minio-fixture.py start
application_owned=1
for start_target in -develop -manager -monitor -geopython-workflow; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged python3 business/scripts/online-raster-minio-fixture.py seed
# Prove the running container uses the image built from this checkout.
image_id=$(docker image inspect -f '{{.Id}}' addp-geopython-workflow-engine:dev)
[ "$(docker inspect -f '{{.Image}}' geopython-workflow-engine)" = "$image_id" ] ||
  fail "GeoPython Runtime build identity mismatch"
source_fingerprint=$(docker image inspect -f '{{ index .Config.Labels "addp.geopython.source-fingerprint" }}' addp-geopython-workflow-engine:dev)
[ -n "$source_fingerprint" ] && [ "$source_fingerprint" != '<no value>' ] ||
  fail "GeoPython Runtime source fingerprint is missing"
printf 'image_id=%s\nsource_fingerprint=%s\ngit_commit=%s\n' "$image_id" "$source_fingerprint" "$(git rev-parse HEAD)" > "$ADDP_ONLINE_ARTIFACT_DIR/raster-runtime-build.txt"
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite raster-workflow --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
for role in source target; do
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/raster-$role-engine.json" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  if [ "$role" = source ]; then
    export ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  else
    export ADDP_ONLINE_RASTER_TARGET_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  fi
done
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged env -u POSTGRES_PASSWORD make test-online "ONLINE_SUITE=$ONLINE_SUITE"
