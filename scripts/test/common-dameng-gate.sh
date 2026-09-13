#!/usr/bin/env bash
# common-dameng-gate.sh - Run the local Linux ARM64 Docker DM8 Provider contract.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
# shellcheck source=../lib/dameng-official-media.sh
source "$ROOT_DIR/scripts/lib/dameng-official-media.sh"

DATABASE_CONTAINER=business-dameng
TEST_CONTAINER=addp-dameng-provider-t2
GO_TEST_IMAGE=golang:1.24.2-bookworm@sha256:79390b5e5af9ee6e7b1173ee3eac7fadf6751a545297672916b59bfa0ecf6f71
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-dameng.XXXXXX")
DRIVER_ZIP=$WORK_DIR/dm-go-driver.zip
DRIVER_DIR=$WORK_DIR/driver

fail() {
    echo "Common DM8 gate failed: $*" >&2
    exit 1
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    docker rm --force "$TEST_CONTAINER" >/dev/null 2>&1 || true
    chmod -R u+rwX "$WORK_DIR" >/dev/null 2>&1 || true
    rm -rf "$WORK_DIR"
    docker container inspect "$TEST_CONTAINER" >/dev/null 2>&1 && status=1
    exit "$status"
}
trap cleanup EXIT INT TERM

for command in docker unzip; do
    command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done

dameng_require_arm64_docker
docker container inspect "$DATABASE_CONTAINER" >/dev/null 2>&1 || fail "start DM8 with: bash business/scripts/start.sh -dameng"
[ "$(docker inspect --format '{{.State.Running}}' "$DATABASE_CONTAINER")" = true ] || fail "$DATABASE_CONTAINER is not running"
[ "$(docker inspect --format '{{ index .Config.Labels "com.addp.business-fixture" }}' "$DATABASE_CONTAINER")" = dameng ] || fail "$DATABASE_CONTAINER is not the ADDP Business fixture"
dameng_validate_local_image || fail "$DAMENG_OFFICIAL_IMAGE does not match the pinned official media"
dameng_container_ready "$DATABASE_CONTAINER" || fail "$DATABASE_CONTAINER is not ready"
docker container inspect "$TEST_CONTAINER" >/dev/null 2>&1 && fail "refusing to replace existing container: $TEST_CONTAINER"

docker cp "$DATABASE_CONTAINER:$DAMENG_HOME/drivers/go/dm-go-driver.zip" "$DRIVER_ZIP" >/dev/null
dameng_verify_sha256 "$DAMENG_OFFICIAL_GO_DRIVER_SHA256" "$DRIVER_ZIP" || fail "official Go driver SHA-256 mismatch"
mkdir -p "$DRIVER_DIR"
unzip -q "$DRIVER_ZIP" -d "$DRIVER_DIR"
[ -f "$DRIVER_DIR/dm/go.mod" ] || fail "official Go driver module is missing"

docker run --rm \
    --name "$TEST_CONTAINER" \
    --platform linux/arm64 \
    --network business_business-network \
    --volume "$ROOT_DIR/common:/workspace/common:ro" \
    --volume "$DRIVER_DIR/dm:/opt/dm-driver:ro" \
    --env ADDP_DAMENG_INTEGRATION=1 \
    --env ADDP_TEST_DAMENG_HOST=business-dameng \
    --env ADDP_TEST_DAMENG_PORT="$DAMENG_DATABASE_PORT" \
    --env ADDP_TEST_DAMENG_USER="$DAMENG_BUSINESS_USER" \
    --env ADDP_TEST_DAMENG_PASSWORD="$DAMENG_BUSINESS_PASSWORD" \
    --env GOTOOLCHAIN=local \
    --workdir /workspace/common \
    "$GO_TEST_IMAGE" \
    bash -ceu '
        cp -a /workspace/common /tmp/common
        cd /tmp/common
        go mod edit -require=dm@v0.0.0 -replace=dm=/opt/dm-driver
        go test -tags dameng_official ./engine/plugins/dameng ./engine/plugins/builtin/general \
            -run "TestIntegrationDM8ProviderContract|TestGeneralBuiltinPluginsRegistered" \
            -count=1 -v
    '

residual=$(printf "SET HEADING OFF\nSET FEEDBACK OFF\nSELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE = 'TABLE' AND OBJECT_NAME = 'ADDP_DM8_PROVIDER_CONTRACT';\nEXIT\n" | dameng_business_disql "$DATABASE_CONTAINER" 2>/dev/null)
printf '%s\n' "$residual" | grep -Eq '(^|[[:space:]])0([[:space:]]|$)' || fail "DM8 Provider test table residue remains"
docker container inspect "$TEST_CONTAINER" >/dev/null 2>&1 && fail "DM8 Provider test container residue remains"

echo "DM8 Provider T2 passed in Linux ARM64 Docker; the Business database remains running by design."
