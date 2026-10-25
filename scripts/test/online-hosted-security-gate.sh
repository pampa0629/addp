#!/usr/bin/env bash
# Disposable Security masking and owner protection acceptance on GitHub Hosted.
# ADDP_ONLINE_SUITES=security-mysql-owner-protection
# ADDP_ONLINE_SUITES=security-plaintext-access
# ADDP_ONLINE_SUITES=graph-protection
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=${ONLINE_SUITE_INPUT:-}
case "$ONLINE_SUITE" in
  security-mysql-owner-protection) INITIALIZER_SCRIPT=security-mysql-owner-protection-online.py ;;
  security-plaintext-access) INITIALIZER_SCRIPT=security-plaintext-access-online.py ;;
  graph-protection) INITIALIZER_SCRIPT= ;;
  *) echo 'Unsupported Hosted Security suite' >&2; exit 1 ;;
esac
HOSTED_FIXTURE_CONTAINERS=(addp-security-online-postgres addp-security-online-mysql addp-security-online-mongodb)
if [ "$ONLINE_SUITE" = graph-protection ]; then
  case "${ADDP_ONLINE_GRAPH_ENGINE:-}" in
    falkordb|neo4j) ;;
    *) echo 'Graph acceptance requires ADDP_ONLINE_GRAPH_ENGINE=falkordb|neo4j' >&2; exit 1 ;;
  esac
  export ADDP_ONLINE_GRAPH_CASE=${ADDP_ONLINE_GRAPH_CASE-full}
  case "$ADDP_ONLINE_GRAPH_CASE" in
    full|endpoint-geometry) ;;
    *) echo 'Unsupported Graph acceptance case' >&2; exit 1 ;;
  esac
  HOSTED_FIXTURE_CONTAINERS=(addp-graph-online-review-extractor addp-graph-online-neo4j-review addp-graph-online-neo4j addp-graph-online-neo4j-control addp-graph-online-falkordb registry addp-graph-online-upstreams system-backend gateway console meta-backend security-backend manager-backend develop-backend service-backend service-frontend transfer-backend monitor-backend graph-backend graph-frontend develop-frontend addp-nginx)
  HOSTED_FIXTURE_COMPOSE_PROJECTS=(addp-graph-online addp-platform)
fi
compose_graph() {
  docker compose -f "$ROOT_DIR/docker-compose.yml" -f "$ROOT_DIR/scripts/test/docker-compose.graph-protection-t4.yml" "$@"
}
stop_online_fixture() {
  if [ "$ONLINE_SUITE" = graph-protection ]; then
    local status=0 container
    for container in addp-graph-online-upstreams registry; do
      if docker container inspect "$container" >/dev/null 2>&1; then
        run_logged docker rm -fv "$container" || status=1
      fi
      if docker container inspect "$container" >/dev/null 2>&1; then status=1; fi
    done
    run_logged python3 business/scripts/online-graph-fixture.py stop || status=1
    return "$status"
  else
    run_logged bash business/scripts/online-security-owner-fixture.sh stop
  fi
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
if [ "$ONLINE_SUITE" = graph-protection ]; then
  stop_online_application() {
    local status=0 remaining kind
    run_logged compose_graph down --remove-orphans --volumes || status=1
    remaining=$(docker ps -aq --filter label=com.docker.compose.project=addp-platform) || return 1
    [ -z "$remaining" ] || status=1
    for kind in network volume; do
      remaining=$(docker "$kind" ls -q --filter label=com.docker.compose.project=addp-platform) || return 1
      [ -z "$remaining" ] || status=1
    done
    return "$status"
  }
  export NGINX_BIND_HOST=127.0.0.1 NGINX_PORT=18080
  export ADDP_PUBLIC_ORIGIN=http://127.0.0.1:18080
  export ALLOWED_ORIGINS="$ADDP_PUBLIC_ORIGIN"
  export REGISTRY=localhost:5001 IMAGE_TAG=latest
fi
# The v11 suite adds discovery, required Owner acknowledgements and a real bank-card
# transfer to the existing serial algorithm/outage/index-convergence acceptance.
if [ "$ONLINE_SUITE" = security-mysql-owner-protection ]; then
  export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=1200
fi
export SECURITY_URL=http://127.0.0.1:8194
export GRAPH_URL=http://127.0.0.1:8186
export MONITOR_URL=http://127.0.0.1:8100 CONSOLE_URL=http://127.0.0.1:5170
export ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE=security_fixture
# Swagger generation requires an explicit workspace in a clean checkout.
# Discover the same tracked modules as test-go; the Hosted exit trap owns it.
export GOWORK="$ADDP_ONLINE_SECRET_DIR/go.work"
export GOMODCACHE="$ROOT_DIR/.gomodcache"
cp "$ROOT_DIR/go.work.sum" "${GOWORK}.sum"
go_modules=()
while IFS= read -r module; do
  go_modules+=("$ROOT_DIR/$(dirname "$module")")
done < <(git ls-files -- 'go.mod' '**/go.mod')
[ "${#go_modules[@]}" -gt 0 ] || fail 'Security Hosted checkout has no tracked Go modules'
run_logged go work init "${go_modules[@]}"
if [ "$ONLINE_SUITE" = graph-protection ]; then
  run_logged compose_graph config --quiet
fi
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
if [ "$ONLINE_SUITE" = graph-protection ]; then
  run_logged python3 business/scripts/online-graph-fixture.py start
else
  run_logged bash business/scripts/online-security-owner-fixture.sh start
fi
application_owned=1
if [ "$ONLINE_SUITE" = graph-protection ]; then
  export CONSOLE_URL="$ADDP_PUBLIC_ORIGIN"
  run_logged docker run -d --name registry -p 127.0.0.1:5001:5000 registry:2
  registry_ready=0
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 2 http://127.0.0.1:5001/v2/ >/dev/null 2>&1; then registry_ready=1; break; fi
    sleep 1
  done
  [ "$registry_ready" -eq 1 ] || fail 'Graph image registry readiness timed out'
  backend_images=system-backend,gateway,meta-backend,meta-worker,security-backend,security-worker,manager-backend,develop-backend,service-backend,transfer-backend,graph-backend,monitor-backend
  run_logged make build BUILD_ARGS="--arch amd64 --services $backend_images"
  run_logged make build-images IMAGE_BUILD_ARGS="--verify --services $backend_images,console,graph-frontend,develop-frontend,service-frontend,nginx"
  run_logged compose_graph up -d --no-deps --wait graph-review-extractor
  for service in system-backend security-backend gateway meta-backend manager-backend transfer-backend service-backend graph-backend monitor-backend develop-backend meta-worker security-worker console graph-frontend develop-frontend service-frontend; do
    run_logged compose_graph up -d --no-deps --wait "$service"
  done
  run_logged docker exec graph-frontend sh -ec 'wget -qO- http://127.0.0.1/graphs/1/browse | grep -q "id=\"app\""; if wget -qO- http://127.0.0.1/assets/nonexistent-acceptance-chunk.js >/dev/null 2>&1; then exit 1; fi'
  # Unvisited static upstreams need DNS names at Nginx startup, as in the
  # existing production-entry suite. Graph and Develop use real frontends.
  run_logged docker run -d --name addp-graph-online-upstreams --network addp-network \
    --network-alias system-frontend --network-alias manager-frontend --network-alias meta-frontend \
    --network-alias transfer-frontend --network-alias orchestrator-frontend \
    localhost:5001/nginx:alpine
  run_logged compose_graph up -d --no-deps --wait nginx
  run_logged npm --prefix console/frontend ci
else
  for start_target in -meta -security -manager -develop -service -transfer -graph -monitor; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
  done
fi
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
if [ "$ONLINE_SUITE" = graph-protection ]; then
  run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$1" --output "$2" --wait-protection-owners develop,graph,manager,service,transfer' _ "$ONLINE_SUITE" "$IDENTITY_ENV"
else
  run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$1" --output "$2"' _ "$ONLINE_SUITE" "$IDENTITY_ENV"
fi
source "$IDENTITY_ENV"
if [ "$ONLINE_SUITE" = graph-protection ]; then
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/graph-target.json" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  if [ "$ADDP_ONLINE_GRAPH_ENGINE" = neo4j ]; then
    run_logged python3 scripts/test/online-engine-registration.py \
      --descriptor "$ADDP_ONLINE_SECRET_DIR/graph-control.json" --output "$ADDP_ONLINE_SECRET_DIR/graph-control.env"
    source "$ADDP_ONLINE_SECRET_DIR/graph-control.env"
    export ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
    [ "$ADDP_ONLINE_TEST_ENGINE_ID" != "$ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID" ] || fail 'Neo4j requires independent engine instances'
    run_logged python3 scripts/test/online-engine-registration.py \
      --descriptor "$ADDP_ONLINE_SECRET_DIR/graph-review.json" --output "$ADDP_ONLINE_SECRET_DIR/graph-review.env"
    source "$ADDP_ONLINE_SECRET_DIR/graph-review.env"
    export ADDP_ONLINE_GRAPH_REVIEW_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
    [ "$ADDP_ONLINE_GRAPH_REVIEW_ENGINE_ID" != "$ADDP_ONLINE_TEST_ENGINE_ID" ] && [ "$ADDP_ONLINE_GRAPH_REVIEW_ENGINE_ID" != "$ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID" ] || fail 'Neo4j review requires an independent engine'

  fi
else
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/security-postgres.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/security-mysql.json" --output "$ADDP_ONLINE_SECRET_DIR/mysql-engine.env"
source "$ADDP_ONLINE_SECRET_DIR/mysql-engine.env"
export ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
if [ "$ONLINE_SUITE" = security-mysql-owner-protection ]; then
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/security-mongodb.json" --output "$ADDP_ONLINE_SECRET_DIR/mongodb-engine.env"
  source "$ADDP_ONLINE_SECRET_DIR/mongodb-engine.env"
  export ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
fi
run_logged python3 "scripts/test/$INITIALIZER_SCRIPT" --initialize
fi
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
