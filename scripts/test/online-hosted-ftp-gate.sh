#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=ftp-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=ftp-consumer-flow
HOSTED_FIXTURE_CONTAINERS=()
stop_online_fixture() {
  run_logged python3 business/ftp/admission.py stop
  local evidence
  for evidence in facts cleanup readiness-initial ftp-samples; do
    [ ! -f "$ADDP_ONLINE_SECRET_DIR/$evidence.json" ] || cp "$ADDP_ONLINE_SECRET_DIR/$evidence.json" "$ADDP_ONLINE_ARTIFACT_DIR/ftp-$evidence.json"
  done
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_FTP_ADMISSION_DIR="$ADDP_ONLINE_SECRET_DIR"
export CONSOLE_URL=http://127.0.0.1:5170
export SECURITY_URL=http://127.0.0.1:8194
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged python3 business/ftp/admission.py start
application_owned=1
for start_target in -security -meta -manager; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite ftp-consumer-flow --wait-protection-owners manager --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/ftp-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_FTP_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
