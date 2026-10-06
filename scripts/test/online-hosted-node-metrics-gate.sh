#!/usr/bin/env bash
# Real platform node admission, native OAuth discovery and resource sampling.
# ADDP_ONLINE_SUITES=platform-node-metrics
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ONLINE_SUITE=platform-node-metrics
HOSTED_FIXTURE_CONTAINERS=()
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_COMPOSE_PROJECTS=(addp-node-metrics addp-metrics-online)
node_owned=0
stop_online_fixture() {
  local status=0
  run_logged python3 scripts/test/platform-node-metrics-fixture.py down || status=1
  if [ "$node_owned" -eq 1 ]; then
    run_logged python3 scripts/infra/node-metrics.py down || status=1
    for kind in container network volume; do
      case "$kind" in
        container) remaining=$(docker ps -aq --filter label=com.docker.compose.project=addp-node-metrics) || status=1 ;;
        *) remaining=$(docker "$kind" ls -q --filter label=com.docker.compose.project=addp-node-metrics) || status=1 ;;
      esac
      [ -z "$remaining" ] || status=1
    done
  fi
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export MONITOR_URL=http://127.0.0.1:8100
# Core Infra is created independently; metrics preparation and startup follow.
infra_owned=1
run_logged env ADDP_OBSERVABILITY_METRICS_ENABLED=false ADDP_OBSERVABILITY_LOGS_ENABLED=false bash scripts/infra/up.sh
fixture_owned=1
run_logged python3 scripts/test/platform-node-metrics-fixture.py prepare
source "$ADDP_ONLINE_SECRET_DIR/metrics.env"
node_owned=1
run_logged python3 scripts/infra/node-metrics.py up
run_logged python3 scripts/test/platform-node-metrics-fixture.py up
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -monitor
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite platform-node-metrics --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
