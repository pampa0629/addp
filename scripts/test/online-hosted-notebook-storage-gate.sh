#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=develop-notebook-storage
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/notebook-http-product"
[[ "${NOTEBOOK_HTTP_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$NOTEBOOK_HTTP_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=develop-notebook-storage
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=()
source "$ROOT_DIR/scripts/utils/hosted-online.sh"

export ADDP_INFRA_OBJECTSTORE_PROVIDER=minio
export ADDP_INFRA_OBJECTSTORE_BINDINGS=develop=notebook-http-documents
export JUPYTER_SESSION_TTL_SECONDS=300 JUPYTER_API_PORT=8097
export WORKSPACE_DIR="$ADDP_ONLINE_SECRET_DIR/notebooks"
export DEVELOP_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export MANAGER_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
infra_owned=1
run_logged bash scripts/infra/up.sh
# The product creates only the deployment-bound bucket. The default bucket is
# an independent private sentinel owned by this disposable acceptance fixture.
run_logged docker exec addp-minio mc mb local/develop
run_logged docker exec addp-minio mc anonymous set private local/develop
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -jupyter
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -develop
run_logged bash -c 'cd "$1/system/backend" && go run ./cmd/online-test-fixture --suite develop-notebook-storage --output "$2"' _ "$RUNNER_TEMP/notebook-http-identity" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
export NOTEBOOK_STORAGE_OBSERVER_PYTHON="$ROOT_DIR/engines/jupyter/venv/bin/python"
run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
