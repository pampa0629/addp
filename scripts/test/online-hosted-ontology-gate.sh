#!/usr/bin/env bash
# Disposable Ontology revision/projection acceptance through System and Gateway.
# ADDP_ONLINE_SUITES=ontology-revision-lifecycle
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Usage: bash scripts/test/online-hosted-ontology-gate.sh [--check-only]
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=ontology-revision-lifecycle
HOSTED_FIXTURE_CONTAINERS=()
HOSTED_FIXTURE_IMAGES=()
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ONTOLOGY_URL=http://127.0.0.1:8195
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -ontology
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite ontology-revision-lifecycle --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
