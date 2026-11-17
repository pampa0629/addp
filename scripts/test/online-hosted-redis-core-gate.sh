#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=infra-redis-core
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Prepare and run the pinned Redis R1 HTTP suite only after explicit dispatch.
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/redis-http-product"
PINNED=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["product_sha"])' "$HARNESS/scripts/test/redis-core-http-contract.json")
[[ "${REDIS_HTTP_SOURCE_COMMIT:-}" == "$PINNED" ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$PINNED" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=infra-redis-core
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=(addp-redis-core-source)
HOSTED_FIXTURE_COMPOSE_PROJECTS=(addp-redis-core-fixture)
before_online_application_stop() {
  run_logged python3 "$HARNESS/scripts/test/redis-core-http-preparation.py" recover
}
stop_online_fixture() {
  run_logged python3 "$HARNESS/scripts/test/redis-core-http-preparation.py" stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_TEST_RUN_ID="redis-r1-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export ONLINE_SUITE=infra-redis-core
# Only this suite's private launcher varies the selected Redis endpoint and
# published Service address. Platform compiler/receiver/deployment CLI unchanged.
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
application_owned=1
run_logged python3 "$HARNESS/scripts/test/redis-core-http-preparation.py" start
python3 - "$ADDP_ONLINE_SECRET_DIR/identity.json" "$ADDP_ONLINE_SECRET_DIR/observer.env" <<'PY'
import json,shlex,sys
from pathlib import Path
values=json.loads(Path(sys.argv[1]).read_text())
Path(sys.argv[2]).write_text(''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in values.items() if key in {'ADDP_ONLINE_TEST_TENANT_ID','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'}))
Path(sys.argv[2]).chmod(0o600)
PY
source "$ADDP_ONLINE_SECRET_DIR/observer.env"
run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
