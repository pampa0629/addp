#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=standard-document-storage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/standard-http-product"
[[ "${STANDARD_HTTP_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$STANDARD_HTTP_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=standard-document-storage
objectstore_provider=${ADDP_INFRA_OBJECTSTORE_PROVIDER:?}
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=(addp-rustfs)
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
source "$HARNESS/scripts/test/hosted-platform-database.sh"

export ADDP_INFRA_OBJECTSTORE_PROVIDER="$objectstore_provider"
export ADDP_INFRA_OBJECTSTORE_BINDINGS=standard=standard-http-documents
export STANDARD_URL=http://127.0.0.1:8110
export STANDARD_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
infra_owned=1
hosted_start_selected_platform_infra
objectstore_containers=$(docker ps -a --filter label=com.docker.compose.project=addp-infra \
  --filter 'name=^/(addp-minio|addp-rustfs)$' --format '{{.Names}}')
[[ "$objectstore_containers" == "addp-$objectstore_provider" ]] || fail "expected only the selected $objectstore_provider container"
addp_infra_verify_container "$objectstore_provider" "addp-$objectstore_provider"
objectstore_port=$(addp_infra_mapped_port "addp-$objectstore_provider" 9000)
export ADDP_ONLINE_OBJECTSTORE_ENDPOINT="127.0.0.1:$objectstore_port"
application_owned=1
hosted_prepare_selected_native_database -standard
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -standard
# The fixture uses the pinned product IAM and a scenario-only command patch.
run_logged bash -c 'cd "$1/system/backend" && go run ./cmd/online-test-fixture --suite standard-document-storage --output "$2"' _ "$RUNNER_TEMP/standard-http-identity" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
observer_env="$ADDP_ONLINE_SECRET_DIR/standard-object-observer"
run_logged python3 -m venv "$observer_env"
run_logged "$observer_env/bin/python" -m pip install "$ROOT_DIR/common-python[infra-minio]"
PATH="$observer_env/bin:$PATH" run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
