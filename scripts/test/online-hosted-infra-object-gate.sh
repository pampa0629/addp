#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=infra-object-storage
# ADDP_ONLINE_SUITES=infra-object-csv-export
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/infra-object-product"
[[ "${INFRA_OBJECT_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$INFRA_OBJECT_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=${ONLINE_SUITE_INPUT:-infra-object-storage}
objectstore_provider=${ADDP_INFRA_OBJECTSTORE_PROVIDER:-minio}
case "$ONLINE_SUITE:$objectstore_provider" in
  infra-object-storage:minio|infra-object-csv-export:rustfs|infra-object-csv-export:minio) ;;
  *) echo 'Unsupported Hosted object storage scenario or provider' >&2; exit 2 ;;
esac
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=(addp-object-online-disposable addp-rustfs)
stop_online_fixture() {
  run_logged bash "$HARNESS/scripts/test/infra-object-source-fixture.sh" stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
source "$HARNESS/scripts/test/hosted-platform-database.sh"

export ADDP_INFRA_OBJECTSTORE_PROVIDER="$objectstore_provider"
export ADDP_INFRA_OBJECTSTORE_BINDINGS=
export SECURITY_URL=http://127.0.0.1:8194
# One real 30-minute cleanup tick must fit inside the observer deadline.
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=2400
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/engine.json"
infra_owned=1
hosted_start_selected_platform_infra
source "$ROOT_DIR/scripts/infra/ports.sh"
objectstore_containers=$(docker ps -a --filter label=com.docker.compose.project=addp-infra \
  --filter 'name=^/(addp-minio|addp-rustfs)$' --format '{{.Names}}')
[[ "$objectstore_containers" == "addp-$objectstore_provider" ]] || fail "expected only the selected $objectstore_provider container"
addp_infra_verify_container "$objectstore_provider" "addp-$objectstore_provider"
objectstore_port=$(addp_infra_mapped_port "addp-$objectstore_provider" 9000)
export ADDP_ONLINE_OBJECTSTORE_ENDPOINT="127.0.0.1:$objectstore_port"
run_logged make -C "$HARNESS" test-infra-object-barrier-postgres
fixture_owned=1
run_logged bash "$HARNESS/scripts/test/infra-object-source-fixture.sh" start
application_owned=1
modules=(-security -meta -transfer -manager -develop)
hosted_prepare_selected_native_database "${modules[@]}"
for target in "${modules[@]}"; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$target"
done
# Only this command checkout receives the scenario IAM patch. Product services
# and all their source/build identity remain at the clean, selected commit.
run_logged bash -c 'cd "$1/system/backend" && go run ./cmd/online-test-fixture --suite "$2" --wait-protection-owners manager,transfer,develop --output "$3"' _ "$RUNNER_TEMP/infra-object-identity" "$ONLINE_SUITE" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
observer_env="$ADDP_ONLINE_SECRET_DIR/object-observer"
run_logged python3 -m venv "$observer_env"
run_logged "$observer_env/bin/python" -m pip install "$HARNESS/common-python[infra-minio]"
PATH="$observer_env/bin:$PATH" run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
