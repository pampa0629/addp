#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=sql-pool-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=sql-pool-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-sql-pool-postgresql addp-sql-pool-mysql)

before_online_application_stop() {
  [ ! -f "$ADDP_ONLINE_SECRET_DIR/pool-grants.json" ] ||
    run_logged bash -c 'source "$1"; python3 -m scripts.test.sql-pool-consumer-flow-online cleanup' _ "$IDENTITY_ENV"
}

stop_online_fixture() {
  run_logged python3 -m scripts.test.sql-pool-consumer-flow-online stop
}

source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170 SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_TEST_RUN_ID="run-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
infra_owned=1
hosted_start_platform_infra
fixture_owned=1
run_logged python3 -m scripts.test.sql-pool-consumer-flow-online start
application_owned=1
hosted_start_native_modules -system -security -meta -manager -develop
# This representative deployment records only the role and revision. Check the
# environment passed to both Security launchers without logging its values.
run_logged python3 - <<'PYRUNTIME'
import os
from pathlib import Path

for unit in ("security", "security-worker"):
    pid = int(Path(f".dev-pids/{unit}.pid").read_text())
    entries = Path(f"/proc/{pid}/environ").read_bytes().split(b"\0")
    env = dict(entry.split(b"=", 1) for entry in entries if b"=" in entry)
    if any(key in env for key in (b"ADDP_DATABASE_DEPLOY_USER", b"ADDP_DATABASE_DEPLOY_PASSWORD",
                                   b"hosted_deploy_password", b"POSTGRES_USER", b"POSTGRES_PASSWORD")):
        raise SystemExit(f"{unit} received installation or bootstrap credentials")
    if (env.get(b"ADDP_DATABASE_RUNTIME_USER") != b"platform_runtime"
            or env.get(b"ADDP_DATABASE_RUNTIME_PASSWORD") != os.environb[b"ADDP_DATABASE_RUNTIME_PASSWORD"]):
        raise SystemExit(f"{unit} is missing the Hosted runtime identity")
    print(f"{unit} uses the restricted Hosted runtime identity")
PYRUNTIME
PGPASSWORD="$ADDP_DATABASE_RUNTIME_PASSWORD" run_logged docker exec -e PGPASSWORD addp-postgres \
  psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -U platform_runtime -d addp_online \
    -c 'SELECT current_user, version AS security_schema_version FROM security.startup_schema_revision WHERE singleton'
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite sql-pool-consumer-flow --wait-protection-owners manager,develop --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 -m scripts.test.sql-pool-consumer-flow-online prepare
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
