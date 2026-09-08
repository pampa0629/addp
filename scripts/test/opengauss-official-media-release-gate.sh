#!/usr/bin/env bash
# opengauss-official-media-release-gate.sh - Certify the pinned official openGauss media on Linux x86_64.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
MEDIA_URL=https://opengauss.obs.cn-south-1.myhuaweicloud.com/6.0.6/openGauss-Docker-6.0.6-x86_64.tar
MEDIA_SHA256=4c50bd0f8884f3c0716872ee7d277bad99cdee3d75193a948c7ae9d4b7dfd14d
IMAGE_REFERENCE=opengauss:6.0.6
CONTAINER_NAME=addp-opengauss-official-media-certification
DATABASE_NAME=addp_opengauss_disposable
DATABASE_USER=gaussdb
DATABASE_PASSWORD='AddpGauss606@'
REPORT_PATH=${ADDP_OPENGAUSS_CERTIFICATION_REPORT:?ADDP_OPENGAUSS_CERTIFICATION_REPORT is required}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-opengauss-certification.XXXXXX")
MEDIA_PATH=$WORK_DIR/openGauss-Docker-6.0.6-x86_64.tar
CONTAINER_OWNED=false
IMAGE_OWNED=false

cleanup() {
    status=$?
    cleanup_failed=false
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "${REPORT_PATH%.json}-container.log" 2>&1
        fi
        if ! docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1; then
            echo "failed to remove disposable openGauss container: $CONTAINER_NAME" >&2
            cleanup_failed=true
        fi
    fi
    if [ "$IMAGE_OWNED" = true ] && docker image inspect "$IMAGE_REFERENCE" >/dev/null 2>&1; then
        if ! docker image rm "$IMAGE_REFERENCE" >/dev/null 2>&1; then
            echo "failed to remove disposable openGauss image: $IMAGE_REFERENCE" >&2
            cleanup_failed=true
        fi
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-opengauss-certification.*)
            if ! rm -rf "$WORK_DIR"; then
                echo "failed to remove openGauss certification work directory: $WORK_DIR" >&2
                cleanup_failed=true
            fi
            ;;
    esac
    if [ "$status" -eq 0 ] && [ "$cleanup_failed" = true ]; then
        status=1
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
    echo "openGauss official-media certification requires Linux x86_64" >&2
    exit 1
fi

for command in curl docker go python3 sha256sum; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "$command is required for openGauss official-media certification" >&2
        exit 1
    fi
done

if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    echo "refusing to replace existing container: $CONTAINER_NAME" >&2
    exit 1
fi
if docker image inspect "$IMAGE_REFERENCE" >/dev/null 2>&1; then
    echo "refusing to replace existing image: $IMAGE_REFERENCE" >&2
    exit 1
fi

echo "Downloading pinned openGauss 6.0.6 x86_64 official media"
curl --fail --location --retry 3 --retry-delay 5 --output "$MEDIA_PATH" "$MEDIA_URL"
printf '%s  %s\n' "$MEDIA_SHA256" "$MEDIA_PATH" | sha256sum --check --status

IMAGE_OWNED=true
docker load --input "$MEDIA_PATH"
image_architecture=$(docker image inspect --format '{{.Architecture}}' "$IMAGE_REFERENCE")
if [ "$image_architecture" != "amd64" ]; then
    echo "loaded openGauss image architecture is $image_architecture, want amd64" >&2
    exit 1
fi
image_id=$(docker image inspect --format '{{.Id}}' "$IMAGE_REFERENCE")

export GS_PASSWORD=$DATABASE_PASSWORD
CONTAINER_OWNED=true
docker run --detach \
    --name "$CONTAINER_NAME" \
    --privileged=true \
    --publish 127.0.0.1::5432 \
    --env GS_PASSWORD \
    "$IMAGE_REFERENCE" >/dev/null
unset GS_PASSWORD

ready=false
for _ in $(seq 1 120); do
    if docker exec --user omm "$CONTAINER_NAME" \
        gsql -At -d postgres -p 5432 -c 'SELECT 1' 2>/dev/null | grep -Fxq '1'; then
        ready=true
        break
    fi
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "openGauss container exited before readiness" >&2
        exit 1
    fi
    sleep 5
done
if [ "$ready" != true ]; then
    echo "openGauss did not become ready within 600 seconds" >&2
    exit 1
fi

docker exec --user omm "$CONTAINER_NAME" \
    gsql -d postgres -p 5432 -c "DROP DATABASE IF EXISTS $DATABASE_NAME"
docker exec --user omm "$CONTAINER_NAME" \
    gsql -d postgres -p 5432 -c "CREATE DATABASE $DATABASE_NAME DBCOMPATIBILITY 'PG'"

host_mapping=$(docker port "$CONTAINER_NAME" 5432/tcp)
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*)
        echo "cannot determine disposable openGauss host port from: $host_mapping" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/common"
ADDP_OPENGAUSS_CERTIFICATION=1 \
ADDP_TEST_OPENGAUSS_DSN="host=127.0.0.1 port=$host_port user=$DATABASE_USER password=$DATABASE_PASSWORD dbname=$DATABASE_NAME sslmode=disable connect_timeout=10" \
    go test ./engine/certification/opengauss \
    -run '^TestOpenGaussOfficialMediaCertification$' \
    -count=1 -v 2>&1 | tee "${REPORT_PATH%.json}-go-test.log"

if grep -q -- '--- SKIP:' "${REPORT_PATH%.json}-go-test.log"; then
    echo "openGauss official-media certification refuses skipped tests" >&2
    exit 1
fi

python3 - "$REPORT_PATH" "$MEDIA_URL" "$MEDIA_SHA256" "$IMAGE_REFERENCE" "$image_id" <<'PY'
import json
import sys
from pathlib import Path

report_path, media_url, media_sha256, image_reference, image_id = sys.argv[1:]
Path(report_path).write_text(
    json.dumps(
        {
            "schema_version": "addp.opengauss-official-media-certification/v1",
            "result": "passed",
            "media_url": media_url,
            "media_sha256": media_sha256,
            "image_reference": image_reference,
            "image_id": image_id,
            "runner": {"os": "linux", "architecture": "x86_64"},
            "database_compatibility": "PG",
        },
        sort_keys=True,
    )
    + "\n",
    encoding="utf-8",
)
PY
