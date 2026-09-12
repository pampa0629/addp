#!/usr/bin/env bash
# common-kingbase-gate.sh - Run the owner-managed KingbaseES Provider contract.
# ADDP_T2_OWNER_MANAGED=kingbase

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=../lib/kingbase-official-media.sh
source "$ROOT_DIR/scripts/lib/kingbase-official-media.sh"

CONTAINER_NAME=addp-kingbase-provider-disposable
DATABASE_NAME=addp_kingbase_disposable
DATABASE_USER=system
DATABASE_PASSWORD=${ADDP_TEST_KINGBASE_PASSWORD:-}
ARTIFACT_DIR=${ADDP_KINGBASE_T2_ARTIFACT_DIR:-}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-kingbase.XXXXXX")
MEDIA_PATH=$WORK_DIR/$KINGBASE_OFFICIAL_MEDIA_FILENAME
CONTAINER_OWNED=false
IMAGE_OWNED=false

fail() {
    echo "Common KingbaseES gate failed: $*" >&2
    exit 1
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "$ARTIFACT_DIR/kingbase-container.log" 2>&1
        fi
        docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
    fi
    if [ "$IMAGE_OWNED" = true ] && docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1; then
        docker image rm "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1 || status=1
    fi
    chmod -R u+rwX "$WORK_DIR" 2>/dev/null || true
    rm -rf "$WORK_DIR"
    if docker ps -a --filter "label=com.addp.t2=kingbase" --format '{{.Names}}' | grep -q .; then
        echo "KingbaseES T2 container residual detected" >&2
        status=1
    fi
    exit "$status"
}
trap cleanup EXIT INT TERM

kingbase_load_owner_environment "$ROOT_DIR"
DATABASE_PASSWORD=${ADDP_TEST_KINGBASE_PASSWORD:-}
ARTIFACT_DIR=${ADDP_KINGBASE_T2_ARTIFACT_DIR:-}

[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail "KingbaseES T2 requires an owner-managed Linux x86_64 runner"
[ -n "$DATABASE_PASSWORD" ] || fail "ADDP_TEST_KINGBASE_PASSWORD is required"
[ -n "$ARTIFACT_DIR" ] && [ "${ARTIFACT_DIR#/}" != "$ARTIFACT_DIR" ] || fail "ADDP_KINGBASE_T2_ARTIFACT_DIR must be an absolute path"
mkdir -p "$ARTIFACT_DIR"

for command in curl docker go python3; do
    command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done
kingbase_validate_license_input "$ROOT_DIR"
docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1 && fail "refusing to replace existing container: $CONTAINER_NAME"
docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1 && fail "refusing to replace existing image: $KINGBASE_OFFICIAL_IMAGE"

kingbase_download_official_media "$MEDIA_PATH"
IMAGE_OWNED=true
kingbase_load_official_image "$MEDIA_PATH"
image_architecture=$(docker image inspect --format '{{.Architecture}}' "$KINGBASE_OFFICIAL_IMAGE")
[ "$image_architecture" = "$KINGBASE_OFFICIAL_IMAGE_ARCH" ] || fail "loaded image architecture is $image_architecture"
image_id=$(docker image inspect --format '{{.Id}}' "$KINGBASE_OFFICIAL_IMAGE")

CONTAINER_OWNED=true
docker create \
    --name "$CONTAINER_NAME" \
    --label com.addp.t2=kingbase \
    --publish 127.0.0.1::"$KINGBASE_DATABASE_PORT" \
    --env DB_MODE=pg \
    --env DB_USER="$DATABASE_USER" \
    --env DB_PASSWORD="$DATABASE_PASSWORD" \
    "$KINGBASE_OFFICIAL_IMAGE" >/dev/null
kingbase_install_license_into_created_container "$CONTAINER_NAME" "$ADDP_KINGBASE_LICENSE_FILE" "$WORK_DIR"
docker start "$CONTAINER_NAME" >/dev/null

ready=false
for _ in $(seq 1 120); do
    if kingbase_container_ready "$CONTAINER_NAME" "$DATABASE_USER"; then
        ready=true
        break
    fi
    [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" = true ] || fail "container exited before readiness"
    sleep 5
done
[ "$ready" = true ] || fail "container did not become ready within 600 seconds"

version=$(kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -At -c 'SELECT version()')
database_mode=$(kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -At -c 'SHOW database_mode')
[ "$version" = "KingbaseES V009R001C010" ] && [ "$database_mode" = pg ] || fail "unexpected identity: version=$version mode=$database_mode"
kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 -c "CREATE DATABASE $DATABASE_NAME"

host_mapping=$(docker port "$CONTAINER_NAME" "$KINGBASE_DATABASE_PORT/tcp")
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*) fail "cannot determine disposable host port from: $host_mapping" ;;
esac

cd "$ROOT_DIR/common"
ADDP_KINGBASE_INTEGRATION=1 \
ADDP_TEST_KINGBASE_HOST=127.0.0.1 \
ADDP_TEST_KINGBASE_PORT="$host_port" \
ADDP_TEST_KINGBASE_DATABASE="$DATABASE_NAME" \
ADDP_TEST_KINGBASE_USER="$DATABASE_USER" \
ADDP_TEST_KINGBASE_PASSWORD="$DATABASE_PASSWORD" \
    go test ./engine/plugins/kingbase \
    -run '^TestIntegrationKingbase' \
    -count=1 -v 2>&1 | tee "$ARTIFACT_DIR/common-kingbase.log"

grep -q -- '--- SKIP:' "$ARTIFACT_DIR/common-kingbase.log" && fail "skipped tests are not accepted"

docker rm --force "$CONTAINER_NAME" >/dev/null
CONTAINER_OWNED=false
docker image rm "$KINGBASE_OFFICIAL_IMAGE" >/dev/null
IMAGE_OWNED=false
docker ps -a --filter "label=com.addp.t2=kingbase" --format '{{.Names}}' | grep -q . && fail "container residual detected"

python3 - "$ARTIFACT_DIR/common-kingbase.json" "$KINGBASE_OFFICIAL_MEDIA_URL" "$KINGBASE_OFFICIAL_MEDIA_SHA256" "$image_id" "$ADDP_KINGBASE_LICENSE_SHA256" <<'PY'
import json
import sys
from pathlib import Path

report_path, media_url, media_sha256, image_id, license_sha256 = sys.argv[1:]
Path(report_path).write_text(
    json.dumps(
        {
            "schema_version": "addp.kingbase-provider-t2/v1",
            "result": "passed",
            "media_url": media_url,
            "media_sha256": media_sha256,
            "image_id": image_id,
            "license_sha256": license_sha256,
            "license_source": "owner_managed",
            "database_mode": "pg",
            "runner": {"os": "linux", "architecture": "x86_64"},
            "zero_residue": True,
        },
        sort_keys=True,
    ) + "\n",
    encoding="utf-8",
)
PY

cat > "$ARTIFACT_DIR/t2-summary.md" <<EOF
### KingbaseES Provider T2

| Version | Mode | Runner | Result | Zero residue |
| --- | --- | --- | --- | --- |
| $KINGBASE_OFFICIAL_VERSION | pg | owner-managed Linux x86_64 | passed | true |
EOF
