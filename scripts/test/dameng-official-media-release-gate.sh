#!/usr/bin/env bash
# dameng-official-media-release-gate.sh - Verify pinned DM8 ARM64 media inside Linux ARM64 containers.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
# shellcheck source=../lib/dameng-official-media.sh
source "$ROOT_DIR/scripts/lib/dameng-official-media.sh"

REPORT_PATH=${ADDP_DAMENG_CERTIFICATION_REPORT:?ADDP_DAMENG_CERTIFICATION_REPORT is required}
CONTAINER_NAME=addp-dameng-official-media-certification
TEST_CONTAINER_NAME=addp-dameng-odbc-go-certification
NETWORK_NAME=addp-dameng-official-media-certification
GO_TEST_IMAGE=golang:1.24.2-bookworm@sha256:79390b5e5af9ee6e7b1173ee3eac7fadf6751a545297672916b59bfa0ecf6f71
UBUNTU_BASE_IMAGE=ubuntu:22.04@sha256:829f6df217bcbae2b371026e81711d1a787c61b2967ad09d015063663ebafbf7
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-dameng-certification.XXXXXX")
MEDIA_DIR=$WORK_DIR/media
MEDIA_PATH=$MEDIA_DIR/$DAMENG_OFFICIAL_MEDIA_FILENAME
DRIVER_DIR=$WORK_DIR/dm-bin
CONTAINER_OWNED=false
TEST_CONTAINER_OWNED=false
NETWORK_OWNED=false
IMAGE_OWNED=false

cleanup_resources() {
    local cleanup_failed=false

    if [ "$TEST_CONTAINER_OWNED" = true ] && docker container inspect "$TEST_CONTAINER_NAME" >/dev/null 2>&1; then
        docker rm --force "$TEST_CONTAINER_NAME" >/dev/null 2>&1 || cleanup_failed=true
    fi
    TEST_CONTAINER_OWNED=false
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || cleanup_failed=true
    fi
    CONTAINER_OWNED=false
    if [ "$NETWORK_OWNED" = true ] && docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
        docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || cleanup_failed=true
    fi
    NETWORK_OWNED=false
    if [ "$IMAGE_OWNED" = true ] && docker image inspect "$DAMENG_OFFICIAL_IMAGE" >/dev/null 2>&1; then
        docker image rm "$DAMENG_OFFICIAL_IMAGE" >/dev/null 2>&1 || cleanup_failed=true
    fi
    IMAGE_OWNED=false
    [ "$cleanup_failed" = false ]
}

cleanup() {
    local status=$?
    local cleanup_failed=false
    trap - EXIT INT TERM
    set +e
    if [ "$status" -ne 0 ] && [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        docker logs "$CONTAINER_NAME" > "${REPORT_PATH%.json}-container.log" 2>&1
    fi
    cleanup_resources || cleanup_failed=true
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-dameng-certification.*)
            rm -rf "$WORK_DIR" || cleanup_failed=true
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

for command in curl docker python3 unzip; do
    command -v "$command" >/dev/null 2>&1 || {
        echo "$command is required for DM8 official-media certification" >&2
        exit 1
    }
done
dameng_require_arm64_docker

for resource in "$CONTAINER_NAME" "$TEST_CONTAINER_NAME"; do
    if docker container inspect "$resource" >/dev/null 2>&1; then
        echo "refusing to replace existing container: $resource" >&2
        exit 1
    fi
done
if docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
    echo "refusing to replace existing network: $NETWORK_NAME" >&2
    exit 1
fi
if docker image inspect "$DAMENG_OFFICIAL_IMAGE" >/dev/null 2>&1; then
    echo "refusing to replace existing image: $DAMENG_OFFICIAL_IMAGE" >&2
    exit 1
fi

mkdir -p "$MEDIA_DIR" "$DRIVER_DIR"
echo "Downloading pinned DM8 20260708 ARM64 official media"
dameng_download_official_media "$MEDIA_PATH"

IMAGE_OWNED=true
dameng_build_official_image "$ROOT_DIR" "$MEDIA_PATH" 2>&1 | tee "${REPORT_PATH%.json}-image-build.log"
image_id=$(docker image inspect --format '{{.Id}}' "$DAMENG_OFFICIAL_IMAGE")

docker network create --label com.addp.certification=dameng "$NETWORK_NAME" >/dev/null
NETWORK_OWNED=true
docker run --detach \
    --name "$CONTAINER_NAME" \
    --network "$NETWORK_NAME" \
    --network-alias dameng \
    --label com.addp.certification=dameng \
    "$DAMENG_OFFICIAL_IMAGE" >/dev/null
CONTAINER_OWNED=true

ready=false
for _ in $(seq 1 120); do
    if dameng_container_ready "$CONTAINER_NAME"; then
        ready=true
        break
    fi
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "DM8 container exited before readiness" >&2
        exit 1
    fi
    sleep 2
done
[ "$ready" = true ] || {
    echo "DM8 did not become ready within 240 seconds" >&2
    exit 1
}

fixture_output=$(dameng_disql "$CONTAINER_NAME" < "$ROOT_DIR/business/dameng/init.sql" 2>&1)
if printf '%s\n' "$fixture_output" | grep -Eiq 'error\[|\[-[0-9]+\]'; then
    printf '%s\n' "$fixture_output" >&2
    exit 1
fi
probe_output=$(printf 'SELECT ENGINE_NAME FROM ADDP_BUSINESS.ADDP_ENGINE_PROBE WHERE ID = 1;\nEXIT\n' | dameng_disql "$CONTAINER_NAME" 2>&1)
printf '%s\n' "$probe_output" | grep -Fq 'DM8 ARM64 technical fixture' || {
    echo "DM8 Business sample probe is missing" >&2
    exit 1
}

identity_output=$(printf "SELECT BANNER FROM V\$VERSION WHERE BANNER LIKE 'DM Database Server%%';\nSELECT BUILD_VERSION FROM V\$INSTANCE;\nSELECT SERVER_TYPE, TO_CHAR(EXPIRED_DATE, 'YYYY-MM-DD') FROM V\$LICENSE;\nEXIT\n" | dameng_disql "$CONTAINER_NAME" 2>&1)
printf '%s\n' "$identity_output" | grep -Fq 'DM Database Server 64 V8' || {
    printf '%s\n' "$identity_output" >&2
    echo "DM8 version banner mismatch" >&2
    exit 1
}
printf '%s\n' "$identity_output" | grep -Fq "$DAMENG_OFFICIAL_BUILD_ID" || {
    printf '%s\n' "$identity_output" >&2
    echo "DM8 build ID mismatch" >&2
    exit 1
}
printf '%s\n' "$identity_output" | grep -Fq '2027-07-07' || {
    printf '%s\n' "$identity_output" >&2
    echo "DM8 trial expiration mismatch" >&2
    exit 1
}

docker cp "$CONTAINER_NAME:$DAMENG_HOME/bin/." "$DRIVER_DIR"
for library in libdodbc.so libdmdpi.so libdmfldr.so; do
    [ -f "$DRIVER_DIR/$library" ] || {
        echo "DM8 official ODBC dependency is missing: $library" >&2
        exit 1
    }
done
libdodbc_sha256=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "$DRIVER_DIR/libdodbc.so")
libdmdpi_sha256=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "$DRIVER_DIR/libdmdpi.so")
libdmfldr_sha256=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "$DRIVER_DIR/libdmfldr.so")
[ "$libdodbc_sha256" = "$DAMENG_LIBDODBC_SHA256" ] || {
    echo "DM8 libdodbc.so SHA-256 mismatch" >&2
    exit 1
}
[ "$libdmdpi_sha256" = "$DAMENG_LIBDMDPI_SHA256" ] || {
    echo "DM8 libdmdpi.so SHA-256 mismatch" >&2
    exit 1
}
[ "$libdmfldr_sha256" = "$DAMENG_LIBDMFLDR_SHA256" ] || {
    echo "DM8 libdmfldr.so SHA-256 mismatch" >&2
    exit 1
}

TEST_CONTAINER_OWNED=true
docker run --rm \
    --name "$TEST_CONTAINER_NAME" \
    --platform linux/arm64 \
    --network "$NETWORK_NAME" \
    --volume "$ROOT_DIR/common:/workspace/common:ro" \
    --volume "$DRIVER_DIR:/opt/dmclient:ro" \
    --env ADDP_DAMENG_CERTIFICATION=1 \
    --env "ADDP_DAMENG_EXPECTED_BUILD_ID=$DAMENG_OFFICIAL_BUILD_ID" \
    --env "ADDP_TEST_DAMENG_DSN=DRIVER={DM8 ODBC DRIVER};SERVER=dameng;PORT=5236;UID=$DAMENG_DATABASE_USER;PWD=$DAMENG_DATABASE_PASSWORD;CHARACTER_CODE=PG_UTF8" \
    --env GOTOOLCHAIN=local \
    --env CGO_ENABLED=1 \
    --env LD_LIBRARY_PATH=/opt/dmclient \
    --workdir /workspace/common \
    "$GO_TEST_IMAGE" \
    bash -ceu '
        apt-get update >/dev/null
        apt-get install --yes --no-install-recommends unixodbc-dev >/dev/null
        cat > /etc/odbcinst.ini <<"EOF"
[DM8 ODBC DRIVER]
Description=DM8 official ODBC driver
Driver=/opt/dmclient/libdodbc.so
Threading=0
EOF
        go test -mod=readonly -tags dameng_odbc ./engine/certification/dameng \
            -run "^TestDamengOfficialMediaCertification$" -count=1 -v
    ' 2>&1 | tee "${REPORT_PATH%.json}-go-test.log"
TEST_CONTAINER_OWNED=false

if grep -q -- '--- SKIP:' "${REPORT_PATH%.json}-go-test.log"; then
    echo "DM8 official-media certification refuses skipped tests" >&2
    exit 1
fi

docker rm --force "$CONTAINER_NAME" >/dev/null
CONTAINER_OWNED=false
docker network rm "$NETWORK_NAME" >/dev/null
NETWORK_OWNED=false
docker image rm "$DAMENG_OFFICIAL_IMAGE" >/dev/null
IMAGE_OWNED=false

for resource in "$CONTAINER_NAME" "$TEST_CONTAINER_NAME"; do
    ! docker container inspect "$resource" >/dev/null 2>&1 || {
        echo "DM8 certification container residue remains: $resource" >&2
        exit 1
    }
done
! docker network inspect "$NETWORK_NAME" >/dev/null 2>&1 || {
    echo "DM8 certification network residue remains: $NETWORK_NAME" >&2
    exit 1
}
! docker image inspect "$DAMENG_OFFICIAL_IMAGE" >/dev/null 2>&1 || {
    echo "DM8 certification image residue remains: $DAMENG_OFFICIAL_IMAGE" >&2
    exit 1
}

python3 - "$REPORT_PATH" "$image_id" "$libdodbc_sha256" "$libdmdpi_sha256" "$libdmfldr_sha256" <<'PY'
import json
import sys
from pathlib import Path

report_path, image_id, libdodbc_sha256, libdmdpi_sha256, libdmfldr_sha256 = sys.argv[1:]
Path(report_path).write_text(
    json.dumps(
        {
            "schema_version": "addp.dameng-official-media-certification/v1",
            "result": "passed",
            "route": "dm8-20260708-kunpeng920-kylin10-sp1-arm64",
            "media_url": "https://download.dameng.com/eco/adapter/DM8/202607/dm8_20260708_HWarm920_kylin10_sp1_64.zip",
            "media_sha256": "d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824",
            "iso_sha256": "2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e",
            "image_reference": "addp/dameng:dm8-20260708-arm64",
            "image_id": image_id,
            "base_image": "ubuntu:22.04@sha256:829f6df217bcbae2b371026e81711d1a787c61b2967ad09d015063663ebafbf7",
            "go_test_image": "golang:1.24.2-bookworm@sha256:79390b5e5af9ee6e7b1173ee3eac7fadf6751a545297672916b59bfa0ecf6f71",
            "server": {
                "banner": "DM Database Server 64 V8",
                "build_id": "03134284604-20260707-335949-20228",
            },
            "license": {"server_type": 3, "expires_on": "2027-07-07"},
            "driver_sha256": {
                "libdodbc.so": libdodbc_sha256,
                "libdmdpi.so": libdmdpi_sha256,
                "libdmfldr.so": libdmfldr_sha256,
            },
            "runner": {
                "docker_os": "linux",
                "docker_architecture": "arm64",
                "classification": "non-certified Ubuntu ARM64 ABI technical validation",
            },
            "engine_type_registered": False,
            "zero_residue": True,
        },
        sort_keys=True,
    )
    + "\n",
    encoding="utf-8",
)
PY
