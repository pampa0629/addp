#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=infra-object-storage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/infra-object-product"
[[ "${INFRA_OBJECT_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$INFRA_OBJECT_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=infra-object-storage
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=(addp-object-online-disposable)
stop_online_fixture() {
  run_logged bash "$HARNESS/scripts/test/infra-object-source-fixture.sh" stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"

export ADDP_INFRA_OBJECTSTORE_PROVIDER=minio
export ADDP_INFRA_OBJECTSTORE_BINDINGS=
export SECURITY_URL=http://127.0.0.1:8194
# One real 30-minute cleanup tick must fit inside the observer deadline.
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=2400
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash "$HARNESS/scripts/test/infra-object-source-fixture.sh" start
application_owned=1
for target in -security -meta -transfer -manager -develop; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$target"
done
# Only this command checkout receives the scenario IAM patch. Product services
# and all their source/build identity remain at the clean, selected commit.
run_logged bash -c 'cd "$1/system/backend" && go run ./cmd/online-test-fixture --suite infra-object-storage --wait-protection-owners manager,transfer,develop --output "$2"' _ "$RUNNER_TEMP/infra-object-identity" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
