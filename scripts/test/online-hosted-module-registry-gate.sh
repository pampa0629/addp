#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=module-registry-recovery
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ONLINE_SUITE=module-registry-recovery
HOSTED_FIXTURE_CONTAINERS=()
stop_online_fixture() { return 0; }
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export MANAGER_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export GATEWAY_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export ADDP_HOST_NODE_IPS=127.0.0.1
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
# Bootstrap identities before the Manager-first scenario, then stop System.
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite module-registry-recovery --output "$1"' _ "$IDENTITY_ENV"
run_logged bash scripts/dev/stop-exact-process.sh -system
observe_module_lifecycle() {
  local phase=$1
  local timeout=$2
  local expected_instance_id=${3:-}
  local baseline_phase=${4:-}
  local command=(
    python3 scripts/test/module-lifecycle-process-online.py
    --phase "$phase"
    --manager-url "$MANAGER_URL"
    --system-url "$SYSTEM_URL"
    --gateway-url "$GATEWAY_URL"
    --repository "$ROOT_DIR"
    --timeout "$timeout"
    --output "$ADDP_ONLINE_ARTIFACT_DIR/module-lifecycle-${phase}.json"
  )
  if [ -n "$expected_instance_id" ]; then
    command+=(--expected-instance-id "$expected_instance_id")
  fi
  if [ -n "$baseline_phase" ]; then
    command+=(--baseline "$ADDP_ONLINE_ARTIFACT_DIR/module-lifecycle-${baseline_phase}.json")
  fi
  run_logged "${command[@]}"
}

module_lifecycle_instance_id() {
  python3 - "$ADDP_ONLINE_ARTIFACT_DIR/module-lifecycle-${1}.json" <<'PYID'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as evidence:
    print(json.load(evidence)["manager"]["instance_id"])
PYID
}

# User credentials exist only in observers, never in the product environment.
observe_with_identity() (
  source "$IDENTITY_ENV"
  observe_module_lifecycle "$@"
)
  process_timeout=${ADDP_ONLINE_PROCESS_TIMEOUT_SECONDS:-60}
  lease_timeout=${ADDP_ONLINE_LEASE_TIMEOUT_SECONDS:-60}

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process --wait-live -manager
  observe_with_identity business-before-system "$process_timeout"
  manager_instance_id=$(module_lifecycle_instance_id business-before-system)

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  observe_with_identity manager-registered "$process_timeout" "$manager_instance_id"

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
  observe_with_identity gateway-established "$process_timeout" "$manager_instance_id"

  run_logged bash scripts/dev/stop-exact-process.sh -system
  observe_with_identity system-interrupted "$lease_timeout" "$manager_instance_id"

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  observe_with_identity system-recovered "$process_timeout" "$manager_instance_id" gateway-established

  run_logged bash scripts/dev/stop-exact-process.sh --force -manager
  observe_with_identity manager-abnormally-stopped "$lease_timeout" "$manager_instance_id" system-recovered

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -manager
  observe_with_identity manager-restarted "$process_timeout" "" system-recovered
  restarted_instance_id=$(module_lifecycle_instance_id manager-restarted)

  # Release the real Manager route before the existing two-probe lease and
  # multi-instance scenario claims the same module route prefix.
  run_logged bash scripts/dev/stop-exact-process.sh -manager
  observe_with_identity manager-gracefully-stopped "$process_timeout" "$restarted_instance_id" manager-restarted

(source "$IDENTITY_ENV"; run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE")
