#!/usr/bin/env bash
# HighGo native catalog and PG snapshot replace on disposable GitHub Hosted Linux.
# ADDP_ONLINE_SUITES=highgo-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=highgo-consumer-flow
export ADDP_ONLINE_HIGHGO_CASE=${ADDP_ONLINE_HIGHGO_CASE:-business}
case "$ADDP_ONLINE_HIGHGO_CASE" in
  business|sql-timeout) ;;
  *) echo 'unknown HighGo acceptance case' >&2; exit 2 ;;
esac
HOSTED_FIXTURE_CONTAINERS=(addp-highgo-online-disposable addp-highgo-pg-online-disposable)
before_online_application_stop() {
  if [ -f "$ADDP_ONLINE_SECRET_DIR/highgo-business-grants.json" ]; then
    run_logged bash -c 'source "$1"; python3 -m scripts.test.highgo-consumer-flow-online cleanup' _ "$IDENTITY_ENV" || return 1
  fi
  [ ! -f "$ADDP_ONLINE_SECRET_DIR/highgo-timeout-grant.json" ] ||
    run_logged bash -c 'source "$1"; python3 -m scripts.test.highgo-sql-timeout-online cleanup' _ "$IDENTITY_ENV"
}
stop_online_fixture() {
  run_logged python3 business/scripts/online-highgo-consumer-fixture.py stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170 SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_TEST_RUN_ID="run-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
targets=(-system -security -meta -transfer -manager)
identity_command=(bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite highgo-consumer-flow --output "$1" "${@:2}"' _ "$IDENTITY_ENV" --wait-protection-owners manager,transfer)
if [ "$ADDP_ONLINE_HIGHGO_CASE" = sql-timeout ]; then
  export SECURITY_URL=http://127.0.0.1:8194
  targets=(-system -security -meta -manager -develop)
  identity_command=(bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite highgo-consumer-flow --output "$1" "${@:2}"' _ "$IDENTITY_ENV" --wait-protection-owners manager,develop)
fi
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged python3 business/scripts/online-highgo-consumer-fixture.py start
application_owned=1
for start_target in "${targets[@]}"; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged "${identity_command[@]}"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/highgo-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_HIGHGO_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
if [ "$ADDP_ONLINE_HIGHGO_CASE" = sql-timeout ]; then
  run_logged python3 -m scripts.test.highgo-sql-timeout-online prepare
  unset ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
else
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/highgo-pg-engine.json" --output "$ADDP_ONLINE_SECRET_DIR/highgo-pg-engine.env"
  source "$ADDP_ONLINE_SECRET_DIR/highgo-pg-engine.env"
  export ADDP_ONLINE_HIGHGO_PG_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  run_logged python3 -m scripts.test.highgo-consumer-flow-online prepare
  unset ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
fi
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
run_logged python3 business/scripts/online-highgo-consumer-fixture.py verify
