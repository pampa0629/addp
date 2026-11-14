#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=standard-document-storage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/standard-http-product"
[[ "${STANDARD_HTTP_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$STANDARD_HTTP_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=standard-document-storage
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=()
source "$ROOT_DIR/scripts/utils/hosted-online.sh"

export ADDP_INFRA_OBJECTSTORE_PROVIDER=minio
export ADDP_INFRA_OBJECTSTORE_BINDINGS=standard=standard-http-documents
export STANDARD_URL=http://127.0.0.1:8110
export STANDARD_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -standard
# The fixture uses the pinned product IAM and a scenario-only command patch.
run_logged bash -c 'cd "$1/system/backend" && go run ./cmd/online-test-fixture --suite standard-document-storage --output "$2"' _ "$RUNNER_TEMP/standard-http-identity" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
