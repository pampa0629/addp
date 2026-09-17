#!/usr/bin/env bash
# GitHub Hosted Linux lifecycle for the openGauss cross-module T4 Online gate.
# ADDP_ONLINE_SUITES=opengauss-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
ONLINE_SUITE=opengauss-consumer-flow
source "$ROOT_DIR/scripts/lib/opengauss-official-media.sh"
HOSTED_FIXTURE_CONTAINERS=(addp-opengauss-online-disposable)
HOSTED_FIXTURE_IMAGES=("$OPENGAUSS_OFFICIAL_IMAGE")
stop_online_fixture() {
  run_logged bash business/scripts/online-opengauss-consumer-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_OPENGAUSS_MEDIA_CACHE="${RUNNER_TEMP}/addp-opengauss-media-cache"
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/opengauss-engine.json"
unset ADDP_TEST_OPENGAUSS_DSN ADDP_TEST_OPENGAUSS_HOST ADDP_TEST_OPENGAUSS_PORT
unset ADDP_TEST_OPENGAUSS_DATABASE ADDP_TEST_OPENGAUSS_USER ADDP_TEST_OPENGAUSS_PASSWORD

infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-opengauss-consumer-fixture.sh start
application_owned=1
for start_target in -manager -develop -service; do
  # start.sh deliberately leaves application processes running. Writing its
  # output straight to the gate log prevents those children from retaining a
  # tee pipe and blocking the Hosted lifecycle after the launcher exits.
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done

run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite opengauss-consumer-flow --output "$1"' _ "$IDENTITY_ENV"
# shellcheck disable=SC1090
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" \
  --output "$ENGINE_RESULT_ENV"
# shellcheck disable=SC1090
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN

export ADDP_ONLINE_CONSUMER_ENGINE_TYPE=opengauss
export ADDP_ONLINE_CONSUMER_NAMESPACE=public
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
