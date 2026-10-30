#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=cleanup-native-effects
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Each explicit case creates and destroys one independent deployment.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=cleanup-native-effects
case "${ADDP_ONLINE_NATIVE_CASE:-}" in
  manager-success|manager-object-unknown|manager-index-unknown|manager-late-writer|transfer-engine|transfer-dlq-unknown|transfer-capture-unknown) ;;
  *) echo 'An explicit bounded native Cleanup case is required' >&2; exit 1 ;;
esac
export ADDP_ONLINE_TEST_RUN_ID="native-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
HOSTED_FIXTURE_CONTAINERS=(addp-cleanup-native-registry business-minio addp-continuous-pg-online-disposable addp-continuous-kafka-online-disposable addp-cleanup-native-object-proxy addp-cleanup-native-index-proxy addp-cleanup-native-dlq-proxy addp-cleanup-native-capture-proxy addp-cleanup-native-kafka-probe)
HOSTED_FIXTURE_COMPOSE_PROJECTS=(addp-platform business)
compose_native() {
  docker compose -f "$ROOT_DIR/docker-compose.yml" -f "$ROOT_DIR/docker-compose.runtimes.yml" \
    -f "$ROOT_DIR/scripts/test/docker-compose.cleanup-native-t4.yml" "$@"
}
stop_online_fixture() {
  local status=0
  python3 scripts/test/cleanup-native-infra.py stop || status=1
  if [[ "$ADDP_ONLINE_NATIVE_CASE" = manager-* ]]; then
    run_logged bash business/scripts/online-manager-minio-fixture.sh stop || status=1
  else
    run_logged bash business/scripts/online-continuous-fixture.sh stop || status=1
  fi
  if docker container inspect addp-cleanup-native-registry >/dev/null 2>&1; then
    if [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-native-run" }}' addp-cleanup-native-registry)" = "$ADDP_ONLINE_TEST_RUN_ID" ]; then
      run_logged docker rm -fv addp-cleanup-native-registry || status=1
    else
      status=1
    fi
  fi
  if docker container inspect addp-cleanup-native-registry >/dev/null 2>&1; then status=1; fi
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
# The canonical Compose service already forwards these product controls.
# Only this explicit case changes the default 2m / 30s supervisor timings.
if [ "$ADDP_ONLINE_NATIVE_CASE" = manager-late-writer ]; then
  export MANAGER_EXECUTION_LEASE_DURATION=30s MANAGER_EXECUTION_HEARTBEAT_INTERVAL=5s
fi
stop_online_application() {
  local status=0 remaining kind
  run_logged compose_native down --remove-orphans --volumes || status=1
  remaining=$(docker ps -aq --filter label=com.docker.compose.project=addp-platform) || return 1
  [ -z "$remaining" ] || status=1
  for kind in network volume; do
    remaining=$(docker "$kind" ls -q --filter label=com.docker.compose.project=addp-platform) || return 1
    [ -z "$remaining" ] || status=1
  done
  return "$status"
}
before_online_application_stop() {
  # Reuse bounded recovery before destroying the deployment; never force-delete unknown business effects.
  local status=0
  run_logged python3 -m scripts.test.execution-online-support resume || status=1
  if [ "$ADDP_ONLINE_NATIVE_CASE" = manager-late-writer ]; then
    python3 scripts/test/cleanup-native-infra.py recover-writer || status=1
  fi
  printf 'control_recovery=%s\n' "$([ "$status" -eq 0 ] && echo passed || echo failed)" > "$ADDP_ONLINE_ARTIFACT_DIR/execution-recovery.txt"
  return "$status"
}
export SECURITY_URL=http://127.0.0.1:8194 MONITOR_URL=http://127.0.0.1:8100 GRAPH_URL=http://127.0.0.1:8186
export QUALITY_URL=http://127.0.0.1:8182 ASSET_URL=http://127.0.0.1:8183 MODEL_URL=http://127.0.0.1:8181 STANDARD_URL=http://127.0.0.1:8110
export REGISTRY=localhost:5001 IMAGE_TAG=latest
export ADDP_ONLINE_MANAGER_MINIO_PORT=9002 ADDP_ONLINE_MANAGER_MINIO_CONSOLE_PORT=9003
export ADDP_ONLINE_MANAGER_MINIO_BUCKET=addp-online ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY=online-manager
export ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/manager-engine.json"
export GOWORK="$ADDP_ONLINE_SECRET_DIR/go.work" GOMODCACHE="$ROOT_DIR/.gomodcache"
cp "$ROOT_DIR/go.work.sum" "${GOWORK}.sum"
go_modules=()
while IFS= read -r module; do go_modules+=("$ROOT_DIR/$(dirname "$module")"); done < <(git ls-files -- 'go.mod' '**/go.mod')
[ "${#go_modules[@]}" -gt 0 ] || fail 'No tracked Go modules'
run_logged go work init "${go_modules[@]}"
# Canonical pinned SDK, private interpreter; no product-client or dependency fallback.
run_logged python3 -m venv "$ADDP_ONLINE_SECRET_DIR/native-python"
export PATH="$ADDP_ONLINE_SECRET_DIR/native-python/bin:$PATH"
minio_requirement=$(python3 - <<'PY'
import re
from pathlib import Path
matches = re.findall(r'^minio==[0-9]+\.[0-9]+\.[0-9]+$', Path('engines/model3d-workflow/requirements.txt').read_text(), re.M)
assert len(matches) == 1
print(matches[0])
PY
)
run_logged python3 -m pip install "$minio_requirement"
run_logged compose_native config --quiet
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
# No app exists while the fresh broker's internal advertised route is prepared.
python3 scripts/test/cleanup-native-infra.py prepare
if [[ "$ADDP_ONLINE_NATIVE_CASE" = manager-* ]]; then
  run_logged bash business/scripts/online-manager-minio-fixture.sh start-obj
else
  run_logged bash business/scripts/online-continuous-fixture.sh start
fi
python3 scripts/test/cleanup-native-infra.py manifest
run_logged docker run -d --name addp-cleanup-native-registry --label "com.addp.online-native-run=$ADDP_ONLINE_TEST_RUN_ID" -p 127.0.0.1:5001:5000 registry:2
registry_ready=0
for _ in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:5001/v2/ >/dev/null 2>&1; then registry_ready=1; break; fi
  sleep 1
done
[ "$registry_ready" -eq 1 ] || fail 'Native image registry readiness timed out'
backend_images=system-backend,gateway,meta-backend,meta-worker,security-backend,manager-backend,transfer-backend,develop-backend,service-backend,quality-backend,graph-backend,asset-backend,model-backend,standard-backend,monitor-backend
if [[ "$ADDP_ONLINE_NATIVE_CASE" = transfer-* ]]; then backend_images+=,transfer-continuous-worker; fi
run_logged make build BUILD_ARGS="--arch amd64 --services $backend_images"
image_services=$backend_images
if [[ "$ADDP_ONLINE_NATIVE_CASE" = manager-* ]]; then image_services+=,model3d-workflow-engine; fi
run_logged make build-images IMAGE_BUILD_ARGS="--verify --services $image_services"
application_owned=1
for service in system-backend security-backend gateway meta-backend manager-backend transfer-backend develop-backend service-backend quality-backend graph-backend asset-backend model-backend standard-backend monitor-backend meta-worker; do
  run_logged compose_native up -d --no-deps --wait "$service"
done
if [[ "$ADDP_ONLINE_NATIVE_CASE" = manager-* ]]; then
  run_logged compose_native up -d --no-deps --wait model3d-workflow-engine
else
  run_logged compose_native up -d --no-deps --wait transfer-continuous-worker
fi
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$1" --output "$2"' _ "$ONLINE_SUITE" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
# Keep the existing exact source initializer for domain Grants; no IAM/revocation actor is used.
run_logged make test-online ONLINE_SUITE="$ONLINE_SUITE"
