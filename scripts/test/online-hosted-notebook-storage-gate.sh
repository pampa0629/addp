#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=develop-notebook-storage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/notebook-http-product"
[[ "${NOTEBOOK_HTTP_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$NOTEBOOK_HTTP_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=develop-notebook-storage
objectstore_provider=${ADDP_INFRA_OBJECTSTORE_PROVIDER:?}
HOSTED_FIXTURE_IMAGES=(localhost:5001/addp-jupyter-engine:jupyter-dev localhost:5001/python:3.12-slim localhost:5001/node:20-alpine)
HOSTED_SPARK_REGISTRY=addp-notebook-online-registry
stop_online_fixture() {
  local status=0 volume labels
  for volume in notebook-launcher notebook-jobs; do
    if docker volume inspect "addp-runtimes_$volume" >/dev/null 2>&1; then
      labels=$(docker volume inspect -f '{{index .Labels "com.docker.compose.project"}}|{{index .Labels "com.docker.compose.volume"}}' "addp-runtimes_$volume")
      if [ "$labels" = "addp-runtimes|$volume" ]; then
        run_logged docker volume rm "addp-runtimes_$volume" || status=1
      else
        status=1
      fi
    fi
  done
  stop_online_spark_image_registry || status=1
  return "$status"
}
HOSTED_FIXTURE_CONTAINERS=("$HOSTED_SPARK_REGISTRY" jupyter-engine jupyter-launcher addp-rustfs)
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
source "$HARNESS/scripts/test/hosted-platform-database.sh"

export ADDP_INFRA_OBJECTSTORE_PROVIDER="$objectstore_provider"
export ADDP_INFRA_OBJECTSTORE_BINDINGS=develop=notebook-http-documents
export JUPYTER_SESSION_TTL_SECONDS=300 JUPYTER_API_PORT=8097
observer_env="$ADDP_ONLINE_SECRET_DIR/notebook-object-observer"
run_logged python3 -m venv "$observer_env"
export NOTEBOOK_STORAGE_OBSERVER_PYTHON="$observer_env/bin/python"
run_logged "$NOTEBOOK_STORAGE_OBSERVER_PYTHON" -m pip install "$ROOT_DIR/common-python[infra-minio]" 'websocket-client==1.9.2'
export DEVELOP_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export MANAGER_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
infra_owned=1
hosted_start_selected_platform_infra
objectstore_containers=$(docker ps -a --filter label=com.docker.compose.project=addp-infra \
  --filter 'name=^/(addp-minio|addp-rustfs)$' --format '{{.Names}}')
[[ "$objectstore_containers" == "addp-$objectstore_provider" ]] || fail "expected only the selected $objectstore_provider container"
addp_infra_verify_container "$objectstore_provider" "addp-$objectstore_provider"
objectstore_port=$(addp_infra_mapped_port "addp-$objectstore_provider" 9000)
export ADDP_ONLINE_OBJECTSTORE_ENDPOINT="127.0.0.1:$objectstore_port"
fixture_owned=1
start_online_spark_image_registry
application_owned=1
hosted_prepare_selected_native_database -develop
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -jupyter
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -develop
run_logged bash -c 'cd "$1/system/backend" && go run ./cmd/online-test-fixture --suite develop-notebook-storage --output "$2"' _ "$RUNNER_TEMP/notebook-http-identity" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
