#!/usr/bin/env bash
# Disposable Manager artifact acceptance with real converters and Console.
# ADDP_ONLINE_SUITES=manager-internal-artifact-lineage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=manager-internal-artifact-lineage
HOSTED_FIXTURE_CONTAINERS=(business-minio addp-manager-online-registry pointcloud-workflow-engine document-workflow-engine addp-manager-raster-runtime addp-manager-raster-verifier)
HOSTED_FIXTURE_COMPOSE_PROJECTS=(business)
RUNTIME_CONTAINER=addp-manager-raster-runtime
RUNTIME_TAG=manager-raster-online
RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
export ADDP_ONLINE_RASTER_RUNTIME_IMAGE="$RUNTIME_IMAGE"
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim 127.0.0.1:5001/addp-model3d-converter:online 127.0.0.1:5001/addp-model3d-workflow-engine:online)
stop_online_fixture() {
  local cleanup_status=0 image
  if docker container inspect addp-manager-raster-verifier >/dev/null 2>&1; then
    if [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' addp-manager-raster-verifier)" = "$ONLINE_SUITE" ]; then
      run_logged docker rm -fv addp-manager-raster-verifier || cleanup_status=1
    else
      cleanup_status=1
    fi
  fi
  run_logged bash business/scripts/online-manager-minio-fixture.sh stop || cleanup_status=1
  run_logged docker rm -fv addp-manager-online-registry || cleanup_status=1
  if docker container inspect addp-manager-online-registry >/dev/null 2>&1; then
    cleanup_status=1
  fi
  for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      run_logged docker image rm "$image" || cleanup_status=1
    fi
  done
  return "$cleanup_status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export MONITOR_URL=http://127.0.0.1:8100 CONSOLE_URL=http://127.0.0.1:5170
export MODEL3D_CONVERTER_PLATFORM=linux/amd64
export MODEL3D_CONVERTER_IMAGE=127.0.0.1:5001/addp-model3d-converter:online
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/manager-engine.json"
export ADDP_ONLINE_MANAGER_MINIO_PORT=59002 ADDP_ONLINE_MANAGER_MINIO_CONSOLE_PORT=59003
export ADDP_ONLINE_MANAGER_MINIO_BUCKET=addp-online
export ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT=pointcloud/pdal_las12_format0.las
export ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT=document/addp_online_preview_fixture.pptx
export ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY=online-manager
ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
export ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY
fixture_owned=1
run_logged docker run -d --name addp-manager-online-registry -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail "temporary image registry did not become ready"
  sleep 1
done
run_logged bash scripts/build/build-images.sh --verify --jobs 1 \
  --registry 127.0.0.1:5001 --tag online --services model3d-workflow-engine
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged bash business/scripts/online-manager-minio-fixture.sh start
application_owned=1
for start_target in -meta -manager -monitor -pointcloud-workflow -document-workflow -model3d-workflow; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_geopython_runtime "$RUNTIME_TAG"
export ADDP_ONLINE_MANAGER_MINIO_RASTER_OBJECT=raster/source.tif
run_logged python3 business/scripts/online-raster-minio-fixture.py manager-seed
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
