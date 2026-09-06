#!/usr/bin/env bash
# Lifecycle owner for the dedicated Business MinIO fixtures used by Manager T4 acceptance.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)
REPOSITORY_DIR=$(cd "${BUSINESS_DIR}/.." && pwd -P)
POINTCLOUD_FIXTURE_SOURCE="${REPOSITORY_DIR}/business/nfs/data/点云/pdal_las12_format0.las"
PPTX_FIXTURE_SOURCE="${REPOSITORY_DIR}/business/fixtures/manager/addp_online_preview_fixture.pptx"
MC_IMAGE=${ADDP_ONLINE_MANAGER_MC_IMAGE:-minio/mc:latest}

fail() {
  echo "Online Manager MinIO fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online Manager MinIO fixture requires macOS"

action=${1:-}
case "$action" in
  start|stop|status) ;;
  *) fail "usage: bash business/scripts/online-manager-minio-fixture.sh start|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_MANAGER_MINIO_PORT
  ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY
  ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY
  ADDP_ONLINE_MANAGER_MINIO_BUCKET
  ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT
  ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done
[[ "$ADDP_ONLINE_MANAGER_MINIO_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_MANAGER_MINIO_PORT must be numeric"
[ "$ADDP_ONLINE_MANAGER_MINIO_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_MANAGER_MINIO_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY" =~ ^[A-Za-z0-9_.-]{3,64}$ ]] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY must contain 3-64 URL-safe characters"
[[ "$ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY" =~ ^[A-Za-z0-9_.-]{16,128}$ ]] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY must contain 16-128 URL-safe characters"
[[ "$ADDP_ONLINE_MANAGER_MINIO_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_BUCKET must be a valid S3 bucket name"

validate_object_key() {
  local variable=$1
  local expected_extension=$2
  local value=${!variable}
  local normalized
  [[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$ ]] ||
    fail "$variable must be a safe object key"
  case "/$value/" in
    *"/../"*|*"/./"*) fail "$variable must not contain dot segments" ;;
  esac
  normalized=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')
  case "$normalized" in
    *".$expected_extension") ;;
    *) fail "$variable must end with .$expected_extension" ;;
  esac
}

validate_object_key ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT las
validate_object_key ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT pptx
[ "$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT" != "$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT" ] ||
  fail "Manager fixture object keys must be distinct"
[ -f "$POINTCLOUD_FIXTURE_SOURCE" ] || fail "fixture source is missing: $POINTCLOUD_FIXTURE_SOURCE"
[ -f "$PPTX_FIXTURE_SOURCE" ] || fail "fixture source is missing: $PPTX_FIXTURE_SOURCE"

docker_fixture() {
  env \
    MINIO_API_PORT="$ADDP_ONLINE_MANAGER_MINIO_PORT" \
    MINIO_ROOT_USER="$ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY" \
    MINIO_ROOT_PASSWORD="$ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY" \
    docker "$@"
}

compose() {
  docker_fixture compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

container_running() {
  [ "$(docker_fixture inspect --format '{{.State.Running}}' business-minio 2>/dev/null || true)" = "true" ]
}

validate_container_ownership() {
  if ! docker_fixture inspect business-minio >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_fixture inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-minio)
  [ "$ownership" = "business/minio" ] ||
    fail "business-minio is not owned by the business/minio Compose service"
}

minio_network() {
  docker_fixture inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' business-minio
}

mc() {
  local network
  network=$(minio_network)
  [ -n "$network" ] || fail "business-minio has no Docker network"
  docker_fixture run --rm \
    --network "$network" \
    -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
    "$MC_IMAGE" "$@"
}

seed_fixture() {
  mc mb --ignore-existing "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET" >/dev/null
  local network
  network=$(minio_network)
  docker_fixture run --rm \
    --network "$network" \
    -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
    -v "$POINTCLOUD_FIXTURE_SOURCE:/fixture/source.las:ro" \
    -v "$PPTX_FIXTURE_SOURCE:/fixture/source.pptx:ro" \
    "$MC_IMAGE" cp --quiet /fixture/source.las \
    "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT"
  docker_fixture run --rm \
    --network "$network" \
    -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
    -v "$POINTCLOUD_FIXTURE_SOURCE:/fixture/source.las:ro" \
    -v "$PPTX_FIXTURE_SOURCE:/fixture/source.pptx:ro" \
    "$MC_IMAGE" cp --quiet /fixture/source.pptx \
    "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT"
  mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT" >/dev/null
  mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT" >/dev/null
}

validate_container_ownership

case "$action" in
  start)
    compose up -d minio
    for _ in $(seq 1 60); do
      if container_running && curl -fsS "http://127.0.0.1:${ADDP_ONLINE_MANAGER_MINIO_PORT}/minio/health/live" >/dev/null 2>&1; then
        seed_fixture
        echo "Online Manager MinIO Fixture is ready on port $ADDP_ONLINE_MANAGER_MINIO_PORT"
        exit 0
      fi
      sleep 1
    done
    fail "business-minio did not become ready"
    ;;
  stop)
    compose rm -sf minio
    if container_running; then
      fail "business-minio is still running"
    fi
    echo "Online Manager MinIO Fixture is stopped"
    ;;
  status)
    container_running || fail "business-minio is not running"
    curl -fsS "http://127.0.0.1:${ADDP_ONLINE_MANAGER_MINIO_PORT}/minio/health/live" >/dev/null ||
      fail "business-minio is not ready"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT" >/dev/null ||
      fail "point-cloud fixture object is missing"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT" >/dev/null ||
      fail "PPTX fixture object is missing"
    echo "Online Manager MinIO Fixture is ready"
    ;;
esac
