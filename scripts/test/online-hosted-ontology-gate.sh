#!/usr/bin/env bash
# Disposable Ontology revision/projection acceptance through System and Gateway.
# ADDP_ONLINE_SUITES=ontology-revision-lifecycle
# ADDP_ONLINE_SUITES=ontology-agent-protection
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Usage: bash scripts/test/online-hosted-ontology-gate.sh [--check-only]
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=${ONLINE_SUITE_INPUT:-ontology-revision-lifecycle}
case "$ONLINE_SUITE" in
  ontology-revision-lifecycle|ontology-agent-protection) ;;
  *) echo 'Unsupported Hosted Ontology suite' >&2; exit 1 ;;
esac
HOSTED_FIXTURE_CONTAINERS=()
HOSTED_FIXTURE_IMAGES=()
if [ "$ONLINE_SUITE" = ontology-agent-protection ]; then
  HOSTED_FIXTURE_CONTAINERS=(addp-security-online-postgres addp-security-online-mysql addp-security-online-mongodb)
  stop_online_fixture() {
    run_logged bash business/scripts/online-security-owner-fixture.sh stop
  }
fi
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
if [ "$ONLINE_SUITE" = ontology-agent-protection ]; then
  # All owners share this deployment key before encrypting any fixture data.
  export ENCRYPTION_KEY="$(python3 -c 'import base64,secrets; print(base64.b64encode(secrets.token_bytes(32)).decode())')"
fi
export ONTOLOGY_URL=http://127.0.0.1:8195
export SERVICE_URL=http://127.0.0.1:8086 SECURITY_URL=http://127.0.0.1:8194
export META_URL=http://127.0.0.1:8082 MANAGER_URL=http://127.0.0.1:8081
export AGENT_URL=http://127.0.0.1:8190 INFERENCE_URL=http://127.0.0.1:8191
infra_owned=1
run_logged bash scripts/infra/up.sh
if [ "$ONLINE_SUITE" = ontology-agent-protection ]; then
  fixture_owned=1
  export ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR="$ADDP_ONLINE_SECRET_DIR/ontology-pg.json"
  export ADDP_ONLINE_SECURITY_MYSQL_DESCRIPTOR="$ADDP_ONLINE_SECRET_DIR/unused-mysql.json"
  export ADDP_ONLINE_SECURITY_MONGODB_DESCRIPTOR="$ADDP_ONLINE_SECRET_DIR/unused-mongodb.json"
  run_logged bash business/scripts/online-security-owner-fixture.sh start
fi
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -ontology
if [ "$ONLINE_SUITE" = ontology-agent-protection ]; then
  for target in -meta -manager -security -develop -graph -service -inference -agent; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$target"
  done
fi
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$1" --output "$2"' _ "$ONLINE_SUITE" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
if [ "$ONLINE_SUITE" = ontology-agent-protection ]; then
  run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600
fi
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
