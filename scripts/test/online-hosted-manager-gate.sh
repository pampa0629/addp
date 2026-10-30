#!/usr/bin/env bash
# Disposable Manager artifact acceptance with real converters and Console.
# ADDP_ONLINE_SUITES=manager-internal-artifact-lineage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=manager-internal-artifact-lineage
export ADDP_ONLINE_MANAGER_CASE=${ADDP_ONLINE_MANAGER_CASE-full}
case "$ADDP_ONLINE_MANAGER_CASE" in
  full|native-raster-media|native-table-orc|kml-export|filegdb-authorization) ;;
  *) echo 'ADDP_ONLINE_MANAGER_CASE must be full, native-raster-media, native-table-orc, kml-export or filegdb-authorization' >&2; exit 1 ;;
esac
HOSTED_FIXTURE_CONTAINERS=(addp-manager-model3d-runtime business-minio addp-manager-online-registry addp-manager-raster-runtime addp-manager-raster-verifier)
if [ "$ADDP_ONLINE_MANAGER_CASE" = kml-export ]; then
  HOSTED_FIXTURE_CONTAINERS=(addp-metric-online-disposable)
elif [ "$ADDP_ONLINE_MANAGER_CASE" = filegdb-authorization ]; then
  HOSTED_FIXTURE_CONTAINERS=(addp-manager-online-registry addp-manager-raster-runtime)
fi
HOSTED_FIXTURE_COMPOSE_PROJECTS=(business)
RUNTIME_CONTAINER=addp-manager-raster-runtime
RUNTIME_TAG=manager-raster-online
RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
export ADDP_ONLINE_RASTER_RUNTIME_IMAGE="$RUNTIME_IMAGE"
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim 127.0.0.1:5001/addp-model3d-converter:online 127.0.0.1:5001/addp-model3d-workflow-engine:online)
if [ "$ADDP_ONLINE_MANAGER_CASE" = filegdb-authorization ]; then
  HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE")
fi
stop_online_fixture() {
  local cleanup_status=0 image
  if [ "$ADDP_ONLINE_MANAGER_CASE" = kml-export ]; then
    run_logged bash business/scripts/online-metric-postgres-fixture.sh stop || cleanup_status=1
    return "$cleanup_status"
  fi
  if [ "$ADDP_ONLINE_MANAGER_CASE" = filegdb-authorization ]; then
    if [ -f "$ADDP_ONLINE_SECRET_DIR/filegdb-nfs-mount" ]; then
      run_logged python3 -m scripts.test.manager-filegdb-physical unmount || cleanup_status=1
    fi
    run_logged bash business/scripts/online-manager-nfs-fixture.sh stop || cleanup_status=1
    remove_online_product_container addp-manager-online-registry || cleanup_status=1
    for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
      if docker image inspect "$image" >/dev/null 2>&1; then
        run_logged docker image rm "$image" || cleanup_status=1
        if docker image inspect "$image" >/dev/null 2>&1; then cleanup_status=1; fi
      fi
    done
    return "$cleanup_status"
  fi
  run_logged bash business/scripts/online-manager-nfs-fixture.sh stop || cleanup_status=1
  if docker container inspect addp-manager-raster-verifier >/dev/null 2>&1; then
    if [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' addp-manager-raster-verifier)" = "$ONLINE_SUITE" ]; then
      run_logged docker rm -fv addp-manager-raster-verifier || cleanup_status=1
    else
      cleanup_status=1
    fi
  fi
  if docker container inspect addp-manager-model3d-runtime >/dev/null 2>&1 &&
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-runtime" }}' addp-manager-model3d-runtime)" = "$ONLINE_SUITE" ]; then
    run_logged docker logs addp-manager-model3d-runtime > "$ADDP_ONLINE_ARTIFACT_DIR/model3d-runtime.log" || cleanup_status=1
  fi
  remove_online_product_container addp-manager-model3d-runtime || cleanup_status=1
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
python3 - <<'PYCASE'
import json, os
from pathlib import Path
(Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'manager-case.json').write_text(json.dumps({
    'schema_version': 'addp.manager-acceptance-case/v1',
    'suite': 'manager-internal-artifact-lineage', 'case': os.environ['ADDP_ONLINE_MANAGER_CASE'],
}, indent=2) + '\n')
PYCASE
[ -n "${GITHUB_RUN_ID:-}" ] && [ -n "${GITHUB_RUN_ATTEMPT:-}" ] || fail 'GitHub run ID and attempt are required before Runtime startup'
export ADDP_ONLINE_TEST_RUN_ID="manager-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export MONITOR_URL=http://127.0.0.1:8100 CONSOLE_URL=http://127.0.0.1:5170
export SECURITY_URL=http://127.0.0.1:8194
if [ "$ADDP_ONLINE_MANAGER_CASE" = kml-export ]; then
  export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/manager-kml-engine.json"
  infra_owned=1
  run_logged bash scripts/infra/up.sh
  fixture_owned=1
  run_logged bash business/scripts/online-metric-postgres-fixture.sh start
  application_owned=1
  for start_target in -security -meta -manager -monitor -transfer; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
  done
  run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
  run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage --wait-protection-owners manager,transfer --output "$1"' _ "$IDENTITY_ENV"
  source "$IDENTITY_ENV"
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  export ADDP_ONLINE_MANAGER_PG_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
  run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
  exit 0
fi
if [ "$ADDP_ONLINE_MANAGER_CASE" = filegdb-authorization ]; then
  export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/manager-nfs-engine.json"
  fixture_owned=1
  run_logged docker run -d --name addp-manager-online-registry --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
  for attempt in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
    [ "$attempt" -lt 30 ] || fail 'temporary registry not ready'
    sleep 1
  done
  infra_owned=1
  run_logged bash scripts/infra/up.sh
  run_logged bash business/scripts/online-manager-nfs-fixture.sh start
  run_logged python3 -m scripts.test.manager-filegdb-physical mount
  nfs_root=$(cat "$ADDP_ONLINE_SECRET_DIR/nfs-root")
  nfs_read_mount=$(cat "$ADDP_ONLINE_SECRET_DIR/filegdb-nfs-mount")
  application_owned=1
  for start_target in -security -meta -manager -monitor; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
  done
  start_online_geopython_runtime "$RUNTIME_TAG" --mount "type=bind,src=$nfs_read_mount,dst=$nfs_root,readonly"
  run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage --wait-protection-owners manager --output "$1"' _ "$IDENTITY_ENV"
  source "$IDENTITY_ENV"
  run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  export ADDP_ONLINE_MANAGER_NFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
  run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
  exit 0
fi
export MODEL3D_DOCKER_PLATFORM=linux/amd64
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
if [ "$ADDP_ONLINE_MANAGER_CASE" != native-table-orc ]; then
run_logged bash scripts/build/build-images.sh --verify --jobs 1 \
  --registry 127.0.0.1:5001 --tag online --services model3d-workflow-engine
fi
run_logged docker pull python:3.11-slim
run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
run_logged docker push localhost:5001/python:3.11-slim
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged bash business/scripts/online-manager-minio-fixture.sh start
run_logged bash business/scripts/online-manager-nfs-fixture.sh start
application_owned=1
# Manager must install its initial Security projection before protected reads.
if [ "$ADDP_ONLINE_MANAGER_CASE" = native-table-orc ]; then
  for start_target in -security -meta -manager -monitor -transfer; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
  done
  nfs_root=$(cat "$ADDP_ONLINE_SECRET_DIR/nfs-root")
  start_online_geopython_runtime "$RUNTIME_TAG" --mount "type=bind,src=$nfs_root,dst=$nfs_root,readonly"
else
for start_target in -security -meta -manager -monitor -pointcloud-workflow -document-workflow; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_model3d_runtime addp-manager-model3d-runtime 127.0.0.1:5001/addp-model3d-workflow-engine:online
start_online_geopython_runtime "$RUNTIME_TAG"
fi
export ADDP_ONLINE_MANAGER_MINIO_RASTER_OBJECT=raster/source.tif
if [ "$ADDP_ONLINE_MANAGER_CASE" != native-table-orc ]; then
  run_logged python3 business/scripts/online-raster-minio-fixture.py manager-seed
fi
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
protection_owners=manager
if [ "$ADDP_ONLINE_MANAGER_CASE" = native-table-orc ]; then protection_owners=manager,transfer; fi
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage --wait-protection-owners "$2" --output "$1"' _ "$IDENTITY_ENV" "$protection_owners"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/manager-nfs-engine.json" --output "$ADDP_ONLINE_SECRET_DIR/nfs-engine.env"
source "$ADDP_ONLINE_SECRET_DIR/nfs-engine.env"
export ADDP_ONLINE_MANAGER_NFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
