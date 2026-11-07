#!/usr/bin/env bash
# Disposable production Compose entry topology on Hosted Linux.
# ADDP_ONLINE_SUITES=compose-public-origin
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=compose-public-origin
HOSTED_FIXTURE_CONTAINERS=(registry addp-online-public-origin-upstreams system-backend security-backend gateway console system-frontend meta-backend meta-frontend manager-backend manager-frontend transfer-backend transfer-frontend orchestrator-backend orchestrator-frontend workbench-backend workbench-frontend asset-backend portal-backend portal-frontend addp-nginx geopython-workflow-engine business-minio)
HOSTED_FIXTURE_COMPOSE_PROJECTS=(addp-platform addp-runtimes business)
HOSTED_FIXTURE_IMAGES=()
COMPOSE_OVERLAY="$ROOT_DIR/scripts/test/docker-compose.public-origin-t4.yml"
published_workbench="${ADDP_ONLINE_PUBLIC_ORIGIN_PUBLISHED_WORKBENCH:-0}"
case "$published_workbench" in
  0) ;;
  1) HOSTED_FIXTURE_CONTAINERS+=(service-backend standard-backend catalog-backend addp-metric-online-disposable) ;;
  *) echo "invalid published Workbench case selector" >&2; exit 1 ;;
esac

compose_app() {
  docker compose -f "$ROOT_DIR/docker-compose.yml" -f "$COMPOSE_OVERLAY" "$@"
}

compose_runtimes() {
  docker compose -f "$ROOT_DIR/docker-compose.runtimes.yml" "$@"
}

compose_business() {
  MINIO_BIND_HOST=127.0.0.1 MINIO_API_PORT=19002 MINIO_CONSOLE_PORT=19003 \
    docker compose --env-file /dev/null -f "$ROOT_DIR/business/docker-compose.yml" "$@"
}

verify_empty_project() {
  local project=$1
  local remaining kind
  remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$project") || return 1
  [ -z "$remaining" ] || return 1
  for kind in network volume; do
    remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project") || return 1
    [ -z "$remaining" ] || return 1
  done
}

stop_online_fixture() {
  local status=0
  if [ "$published_workbench" = 1 ]; then
    run_logged bash business/scripts/online-metric-postgres-fixture.sh stop || status=1
  fi
  run_logged docker rm -fv addp-online-public-origin-upstreams registry || true
  for container in addp-online-public-origin-upstreams registry; do
    docker container inspect "$container" >/dev/null 2>&1 && return 1
  done
  return "$status"
}

source "$ROOT_DIR/scripts/utils/hosted-online.sh"

stop_online_application() {
  local status=0
  run_logged compose_runtimes down --remove-orphans --volumes || status=1
  run_logged compose_business down --remove-orphans --volumes || status=1
  run_logged compose_app down --remove-orphans --volumes || status=1
  for project in addp-runtimes business addp-platform; do
    verify_empty_project "$project" || status=1
  done
  return "$status"
}

export NGINX_BIND_HOST=127.0.0.1 NGINX_PORT=18080
export ADDP_PUBLIC_ORIGIN=http://127.0.0.1:18080
export ADDP_ONLINE_PUBLIC_ORIGIN="$ADDP_PUBLIC_ORIGIN"
export ALLOWED_ORIGINS="$ADDP_PUBLIC_ORIGIN"
export REGISTRY=localhost:5001 IMAGE_TAG=latest
run_logged compose_business config --quiet
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged docker run -d --name registry -p 127.0.0.1:5001:5000 registry:2
registry_ready=0
for _ in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:5001/v2/ >/dev/null 2>&1; then
    registry_ready=1
    break
  fi
  sleep 1
done
[ "$registry_ready" -eq 1 ] || fail "disposable image registry did not become ready"
backends=system-backend,security-backend,gateway,meta-backend,manager-backend,transfer-backend,orchestrator-backend,workbench-backend,asset-backend,portal-backend
frontends=console,system-frontend,meta-frontend,manager-frontend,transfer-frontend,orchestrator-frontend,workbench-frontend,portal-frontend
protection_owners=manager,transfer
if [ "$published_workbench" = 1 ]; then
  backends="$backends,service-backend,standard-backend,catalog-backend"
  protection_owners="$protection_owners,service"
  export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/public-origin-postgres.json"
  run_logged bash business/scripts/online-metric-postgres-fixture.sh start
fi
run_logged make build BUILD_ARGS="--arch amd64 --services $backends"
run_logged make build-images IMAGE_BUILD_ARGS="--verify --services $backends,$frontends,nginx,geopython-workflow-engine"

application_owned=1
run_logged compose_app up -d --no-deps --wait system-backend
run_logged compose_app up -d --no-deps --wait security-backend
run_logged compose_app up -d --no-deps --wait gateway console system-frontend
run_logged compose_app up -d --no-deps --wait meta-backend
run_logged compose_app up -d --no-deps --wait meta-frontend
run_logged compose_app up -d --no-deps --wait manager-backend
run_logged compose_app up -d --no-deps --wait manager-frontend
run_logged compose_app up -d --no-deps --wait transfer-backend
run_logged compose_app up -d --no-deps --wait transfer-frontend
run_logged compose_app up -d --no-deps --wait orchestrator-backend
run_logged compose_app up -d --no-deps --wait orchestrator-frontend
if [ "$published_workbench" = 1 ]; then
  run_logged compose_app up -d --no-deps --wait service-backend standard-backend
fi
run_logged compose_app up -d --no-deps --wait workbench-backend asset-backend
if [ "$published_workbench" = 1 ]; then
  run_logged compose_app up -d --no-deps --wait catalog-backend
fi
run_logged compose_app up -d --no-deps --wait portal-backend
run_logged compose_app up -d --no-deps --wait workbench-frontend portal-frontend

# Nginx resolves its legacy static upstream names at startup. These aliases
# occupy no host ports and do not replace any service under test.
run_logged docker run -d --name addp-online-public-origin-upstreams \
  --network addp-network \
  --network-alias develop-frontend --network-alias service-frontend nginx:alpine
run_logged compose_app up -d --no-deps --wait nginx
run_logged compose_runtimes up -d --no-deps --wait --wait-timeout 180 geopython-workflow-engine
run_logged compose_business up -d --no-deps --wait --wait-timeout 180 minio

run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite compose-public-origin --wait-protection-owners "$2" --output "$1"' _ "$IDENTITY_ENV" "$protection_owners"
# shellcheck disable=SC1090
source "$IDENTITY_ENV"
if [ "$published_workbench" = 1 ]; then
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  export ADDP_ONLINE_CONSUMER_ENGINE_ID
  unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
fi
run_logged npm --prefix console/frontend ci
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"

run_logged compose_business down --remove-orphans --volumes
verify_empty_project business || fail "Business Compose project has residual resources"
[ "$(docker inspect -f '{{.State.Running}}' geopython-workflow-engine)" = true ] ||
  fail "stopping Business also stopped the Runtime"
[ "$(docker inspect -f '{{.State.Running}}' system-backend)" = true ] ||
  fail "stopping Business also stopped the platform"

run_logged compose_runtimes down --remove-orphans --volumes
verify_empty_project addp-runtimes || fail "Runtime Compose project has residual resources"
[ "$(docker inspect -f '{{.State.Running}}' system-backend)" = true ] ||
  fail "stopping Runtime also stopped the platform"
