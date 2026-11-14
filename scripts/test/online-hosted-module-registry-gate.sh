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
export CATALOG_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export COPILOT_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export GATEWAY_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export ADDP_HOST_NODE_IPS=127.0.0.1
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
# Bootstrap identities before the Manager-first scenario, then stop System.
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite module-registry-recovery --output "$1"' _ "$IDENTITY_ENV"
declaration_phase() (
  source "$IDENTITY_ENV"
  run_logged python3 scripts/test/module-declaration-online.py "$@"
)
run_logged python3 scripts/prod/publish-module-declarations.py --provision --credential-dir "$ADDP_ONLINE_SECRET_DIR/publishers" system gateway catalog manager copilot
declaration_phase system-cold system
declaration_phase catalog-prebuilt catalog
run_logged python3 scripts/prod/publish-module-declarations.py --credential-dir "$ADDP_ONLINE_SECRET_DIR/publishers" gateway
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -catalog
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
declaration_phase catalog-running catalog
run_logged bash scripts/dev/stop-exact-process.sh -catalog
run_logged bash scripts/dev/stop-exact-process.sh -gateway
run_logged bash scripts/dev/stop-exact-process.sh -system
observe_module_lifecycle() {
  local phase=$1
  local timeout=$2
  local expected_instance_id=${3:-}
  local baseline_phase=${4:-}
  local command=(
    python3 scripts/test/module-lifecycle-process-online.py
    --phase "$phase"
    --module "$backend_module"
    --backend-url "$backend_url"
    --system-url "$SYSTEM_URL"
    --gateway-url "$GATEWAY_URL"
    --repository "$ROOT_DIR"
    --timeout "$timeout"
    --output "$ADDP_ONLINE_ARTIFACT_DIR/${backend_module}-lifecycle-${phase}.json"
  )
  if [ -n "$expected_instance_id" ]; then
    command+=(--expected-instance-id "$expected_instance_id")
  fi
  if [ -n "$baseline_phase" ]; then
    command+=(--baseline "$ADDP_ONLINE_ARTIFACT_DIR/${backend_module}-lifecycle-${baseline_phase}.json")
  fi
  run_logged "${command[@]}"
}

module_lifecycle_instance_id() {
  python3 - "$ADDP_ONLINE_ARTIFACT_DIR/${backend_module}-lifecycle-${1}.json" <<'PYID'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as evidence:
    print(json.load(evidence)["backend"]["instance_id"])
PYID
}

# User credentials exist only in observers, never in the product environment.
observe_with_identity() (
  source "$IDENTITY_ENV"
  observe_module_lifecycle "$@"
)
start_observed_backend() {
  if [ "$backend_module" = copilot ]; then
    # Deployment supplies the interpreter application's source identity.
    local commit fingerprint built_at
    commit=$(git rev-parse HEAD)
    fingerprint=$(git archive HEAD copilot/backend common-python | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())')
    built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    run_daemon_launcher_logged env SKIP_MODTIDY=1 ADDP_GIT_COMMIT="$commit" ADDP_SOURCE_FINGERPRINT="sha256:$fingerprint" ADDP_BUILT_AT="$built_at" ADDP_BUILD_ID="${GITHUB_RUN_ID}-copilot-$built_at" bash scripts/dev/start.sh --exact-process -copilot
  else
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -manager
  fi
}
run_backend_lifecycle() {
  local backend_module=$1 backend_url=$2
  process_timeout=${ADDP_ONLINE_PROCESS_TIMEOUT_SECONDS:-60}
  lease_timeout=${ADDP_ONLINE_LEASE_TIMEOUT_SECONDS:-60}

  start_observed_backend
  observe_with_identity business-before-system "$process_timeout"
  backend_instance_id=$(module_lifecycle_instance_id business-before-system)

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  declaration_phase waiting "$backend_module"
  observe_with_identity backend-registered "$process_timeout" "$backend_instance_id"

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
  observe_with_identity gateway-established "$process_timeout" "$backend_instance_id"

  run_logged bash scripts/dev/stop-exact-process.sh -system
  observe_with_identity system-interrupted "$lease_timeout" "$backend_instance_id"
  if [ "$backend_module" = manager ]; then
    run_logged bash scripts/dev/stop-exact-process.sh --pause -manager
  fi

  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  if [ "$backend_module" = manager ]; then
    declaration_phase manager-upgrade manager
    run_logged bash scripts/dev/stop-exact-process.sh --resume -manager
  fi
  observe_with_identity system-recovered "$process_timeout" "$backend_instance_id" gateway-established

  if [ "$backend_module" = manager ]; then declaration_phase manager-reconnected manager; fi
  run_logged bash scripts/dev/stop-exact-process.sh --force -"$backend_module"
  observe_with_identity backend-abnormally-stopped "$lease_timeout" "$backend_instance_id" system-recovered

  start_observed_backend
  observe_with_identity backend-restarted "$process_timeout" "" system-recovered
  restarted_instance_id=$(module_lifecycle_instance_id backend-restarted)

  # Observe normal deregistration before leaving this Backend lifecycle.
  run_logged bash scripts/dev/stop-exact-process.sh -"$backend_module"
  observe_with_identity backend-gracefully-stopped "$process_timeout" "$restarted_instance_id" backend-restarted

}
run_backend_lifecycle manager "$MANAGER_URL"

(source "$IDENTITY_ENV"; run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE")

# Repeat the identical lifecycle against a real Python Backend.
run_logged bash scripts/dev/stop-exact-process.sh -gateway
run_logged bash scripts/dev/stop-exact-process.sh -system
run_backend_lifecycle copilot "http://127.0.0.1:${COPILOT_BACKEND_PORT}"
