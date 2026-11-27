#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=starrocks-consumer-flow
# ADDP_ONLINE_SUITES=doris-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=${ONLINE_SUITE_INPUT:?}
case "$ONLINE_SUITE" in
  starrocks-consumer-flow) engine_type=starrocks; HOSTED_FIXTURE_CONTAINERS=() ;;
  doris-consumer-flow) engine_type=doris; HOSTED_FIXTURE_CONTAINERS=(addp-doris-source-online) ;;
  *) exit 2 ;;
esac
grants_cleaned=0
before_online_application_stop() {
  if [ "$grants_cleaned" -eq 0 ] && [ -f "$ADDP_ONLINE_SECRET_DIR/readonly-sql-grants.json" ]; then
    run_logged bash -c 'source "$1"; python3 -m scripts.test.readonly-sql-consumer-flow-online --engine-type "$2" cleanup' _ "$IDENTITY_ENV" "$engine_type" || return
    grants_cleaned=1
  fi
}
stop_online_fixture() {
  if [ "$engine_type" = doris ]; then
    run_logged bash scripts/test/common-doris-decimal-gate.sh online-stop
    return
  fi
  run_logged python3 business/starrocks/admission.py stop
  local evidence
  for evidence in facts backend-readiness business-lifecycle business-launch cleanup; do
    [ ! -f "$ADDP_ONLINE_SECRET_DIR/$evidence.json" ] || cp "$ADDP_ONLINE_SECRET_DIR/$evidence.json" "$ADDP_ONLINE_ARTIFACT_DIR/starrocks-$evidence.json"
  done
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_STARROCKS_ADMISSION_DIR="$ADDP_ONLINE_SECRET_DIR"
export CONSOLE_URL=http://127.0.0.1:5170
export DEVELOP_URL=http://127.0.0.1:8185
export SECURITY_URL=http://127.0.0.1:8194
infra_owned=1
hosted_start_platform_infra
fixture_owned=1
if [ "$engine_type" = doris ]; then
  run_logged bash scripts/test/common-doris-decimal-gate.sh online-start
else
  run_logged python3 business/starrocks/admission.py start
  run_logged python3 business/starrocks/admission.py persistence
fi
application_owned=1
hosted_start_native_modules -security -manager -develop
if [ "$engine_type" = starrocks ]; then
  run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
fi
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$2" --wait-protection-owners manager,develop --output "$1"' _ "$IDENTITY_ENV" "$ONLINE_SUITE"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/$engine_type-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged python3 -m scripts.test.readonly-sql-consumer-flow-online --engine-type "$engine_type" prepare
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
before_online_application_stop
run_logged python3 -m scripts.test.readonly-sql-consumer-flow-online --engine-type "$engine_type" verify-revoked
