#!/usr/bin/env bash
# Disposable Security masking and owner protection acceptance on GitHub Hosted.
# ADDP_ONLINE_SUITES=security-mysql-owner-protection
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=security-mysql-owner-protection
HOSTED_FIXTURE_CONTAINERS=(addp-security-online-postgres addp-security-online-mysql)
stop_online_fixture() {
  run_logged bash business/scripts/online-security-owner-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE=security_fixture
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-security-owner-fixture.sh start
application_owned=1
for start_target in -meta -security -manager -develop -service -transfer; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite security-mysql-owner-protection --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/security-postgres.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/security-mysql.json" --output "$ADDP_ONLINE_SECRET_DIR/mysql-engine.env"
source "$ADDP_ONLINE_SECRET_DIR/mysql-engine.env"
export ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/security-mysql-owner-protection-online.py --initialize
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
