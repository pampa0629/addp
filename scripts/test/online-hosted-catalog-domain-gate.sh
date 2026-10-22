#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=catalog-domain-overview
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Disposable Catalog domain acceptance; no personal deployment or credentials.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=catalog-domain-overview
HOSTED_FIXTURE_CONTAINERS=()
HOSTED_FIXTURE_IMAGES=()
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export STANDARD_URL=http://127.0.0.1:8110 MODEL_URL=http://127.0.0.1:8181 CATALOG_URL=http://127.0.0.1:8192 QUALITY_URL=http://127.0.0.1:8182
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -model -catalog -quality
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite catalog-domain-overview --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
