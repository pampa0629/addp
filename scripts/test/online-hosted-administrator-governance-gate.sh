#!/usr/bin/env bash
# Disposable platform officer identity governance and real Console recovery.
# ADDP_ONLINE_SUITES=platform-administrator-governance
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ONLINE_SUITE=platform-administrator-governance
HOSTED_FIXTURE_CONTAINERS=()
HOSTED_FIXTURE_IMAGES=()
stop_online_fixture() { return 0; } # IAM history is owned by this run's Infra volume.
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -system
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite platform-administrator-governance --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged make build-iam-recovery build-iam-bootstrap GOOS=linux GOARCH=amd64 OUT_DIR=dist BUILD_TYPE=release
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
