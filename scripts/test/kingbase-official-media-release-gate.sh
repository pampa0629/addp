#!/usr/bin/env bash
# kingbase-official-media-release-gate.sh - Certify pinned KingbaseES media and owner License on Linux x86_64.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=../lib/kingbase-official-media.sh
source "$ROOT_DIR/scripts/lib/kingbase-official-media.sh"

CONTAINER_NAME=addp-kingbase-official-media-certification
DATABASE_NAME=addp_kingbase_disposable
DATABASE_USER=system
DATABASE_PASSWORD='AddpKingbaseV9@'
REPORT_PATH=${ADDP_KINGBASE_CERTIFICATION_REPORT:?ADDP_KINGBASE_CERTIFICATION_REPORT is required}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-kingbase-certification.XXXXXX")
MEDIA_PATH=$WORK_DIR/$KINGBASE_OFFICIAL_MEDIA_FILENAME
CONTAINER_OWNED=false
IMAGE_OWNED=false

cleanup() {
    local status=$?
    local cleanup_failed=false
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "${REPORT_PATH%.json}-container.log" 2>&1
        fi
        if ! docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1; then
            echo "failed to remove disposable KingbaseES container: $CONTAINER_NAME" >&2
            cleanup_failed=true
        fi
    fi
    if [ "$IMAGE_OWNED" = true ] && docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1; then
        if ! docker image rm "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1; then
            echo "failed to remove disposable KingbaseES image: $KINGBASE_OFFICIAL_IMAGE" >&2
            cleanup_failed=true
        fi
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-kingbase-certification.*)
            chmod -R u+rwX "$WORK_DIR" 2>/dev/null || true
            rm -rf "$WORK_DIR" || cleanup_failed=true
            ;;
    esac
    if docker ps -a --filter "label=com.addp.certification=kingbase" --format '{{.Names}}' | grep -q .; then
        echo "KingbaseES certification container residual detected" >&2
        cleanup_failed=true
    fi
    if [ "$status" -eq 0 ] && [ "$cleanup_failed" = true ]; then
        status=1
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
    echo "KingbaseES official-media certification requires Linux x86_64" >&2
    exit 1
fi

for command in curl docker go python3; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "$command is required for KingbaseES official-media certification" >&2
        exit 1
    fi
done
kingbase_validate_license_input "$ROOT_DIR"

if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    echo "refusing to replace existing container: $CONTAINER_NAME" >&2
    exit 1
fi
if docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1; then
    echo "refusing to replace existing image: $KINGBASE_OFFICIAL_IMAGE" >&2
    exit 1
fi

echo "Downloading pinned KingbaseES $KINGBASE_OFFICIAL_VERSION x86_64 official media"
kingbase_download_official_media "$MEDIA_PATH"
IMAGE_OWNED=true
docker load --input "$MEDIA_PATH"
image_architecture=$(docker image inspect --format '{{.Architecture}}' "$KINGBASE_OFFICIAL_IMAGE")
if [ "$image_architecture" != "$KINGBASE_OFFICIAL_IMAGE_ARCH" ]; then
    echo "loaded KingbaseES image architecture is $image_architecture, want $KINGBASE_OFFICIAL_IMAGE_ARCH" >&2
    exit 1
fi
image_id=$(docker image inspect --format '{{.Id}}' "$KINGBASE_OFFICIAL_IMAGE")

CONTAINER_OWNED=true
docker create \
    --name "$CONTAINER_NAME" \
    --label com.addp.certification=kingbase \
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
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "KingbaseES container exited before readiness" >&2
        exit 1
    fi
    sleep 5
done
if [ "$ready" != true ]; then
    echo "KingbaseES did not become ready within 600 seconds" >&2
    exit 1
fi

version=$(kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -At -c 'SELECT version()')
database_mode=$(kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -At -c 'SHOW database_mode')
if [ "$version" != "KingbaseES V009R001C010" ] || [ "$database_mode" != "pg" ]; then
    echo "unexpected KingbaseES identity: version=$version database_mode=$database_mode" >&2
    exit 1
fi
kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $DATABASE_NAME"
kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 -c "CREATE DATABASE $DATABASE_NAME"

host_mapping=$(docker port "$CONTAINER_NAME" "$KINGBASE_DATABASE_PORT/tcp")
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*) echo "cannot determine disposable KingbaseES host port from: $host_mapping" >&2; exit 1 ;;
esac

cd "$ROOT_DIR/common"
ADDP_KINGBASE_CERTIFICATION=1 \
ADDP_TEST_KINGBASE_DSN="host=127.0.0.1 port=$host_port user=$DATABASE_USER password=$DATABASE_PASSWORD dbname=$DATABASE_NAME sslmode=disable connect_timeout=10" \
    go test ./engine/certification/kingbase \
    -run '^TestKingbaseOfficialMediaCertification$' \
    -count=1 -v 2>&1 | tee "${REPORT_PATH%.json}-go-test.log"

if grep -q -- '--- SKIP:' "${REPORT_PATH%.json}-go-test.log"; then
    echo "KingbaseES official-media certification refuses skipped tests" >&2
    exit 1
fi

python3 - "$REPORT_PATH" "$KINGBASE_OFFICIAL_MEDIA_URL" "$KINGBASE_OFFICIAL_MEDIA_SHA256" "$KINGBASE_OFFICIAL_MEDIA_MD5" "$KINGBASE_OFFICIAL_IMAGE" "$image_id" "$ADDP_KINGBASE_LICENSE_SHA256" <<'PY'
import json
import sys
from pathlib import Path

report_path, media_url, media_sha256, media_md5, image_reference, image_id, license_sha256 = sys.argv[1:]
Path(report_path).write_text(
    json.dumps(
        {
            "schema_version": "addp.kingbase-official-media-certification/v1",
            "result": "passed",
            "media_url": media_url,
            "media_sha256": media_sha256,
            "media_md5": media_md5,
            "image_reference": image_reference,
            "image_id": image_id,
            "license_sha256": license_sha256,
            "license_source": "owner_managed",
            "runner": {"os": "linux", "architecture": "x86_64"},
            "database_mode": "pg",
            "upsert_syntax": "on_conflict",
        },
        sort_keys=True,
    )
    + "\n",
    encoding="utf-8",
)
PY
