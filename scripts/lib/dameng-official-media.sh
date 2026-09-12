#!/usr/bin/env bash
# dameng-official-media.sh - Canonical DM8 ARM64 official-media facts and local image builder.

DAMENG_OFFICIAL_VERSION=20260708
DAMENG_OFFICIAL_BUILD_ID=03134284604-20260707-335949-20228
DAMENG_OFFICIAL_MEDIA_URL=https://download.dameng.com/eco/adapter/DM8/202607/dm8_20260708_HWarm920_kylin10_sp1_64.zip
DAMENG_OFFICIAL_MEDIA_FILENAME=dm8_20260708_HWarm920_kylin10_sp1_64.zip
DAMENG_OFFICIAL_MEDIA_SHA256=d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824
DAMENG_OFFICIAL_ISO_SHA256=2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e
DAMENG_LIBDODBC_SHA256=5cd15db0f0f19cf985565615114cf6242c0257b2df566e5b81eb9cb62832e921
DAMENG_LIBDMDPI_SHA256=f80e1472517f148f4aa7f7c0e9feb22c939349d1c464079481e869700e77f873
DAMENG_LIBDMFLDR_SHA256=09e911c8cb5015f3c17cd1e834cdddc12b994c4bf417d77b87b7440e2296ff3b
DAMENG_OFFICIAL_IMAGE=addp/dameng:dm8-20260708-arm64
DAMENG_DATABASE_PORT=5236
DAMENG_HOME=/opt/dmdbms
DAMENG_DATABASE_PATH=/opt/dmdata/DAMENG/dm.ini
DAMENG_DATABASE_USER=SYSDBA
DAMENG_DATABASE_PASSWORD='AddpDameng8@'

dameng_verify_sha256() {
    local expected=$1
    local file_path=$2
    if command -v sha256sum >/dev/null 2>&1; then
        [ "$(sha256sum "$file_path" | awk '{print $1}')" = "$expected" ]
        return
    fi
    if command -v shasum >/dev/null 2>&1; then
        [ "$(shasum -a 256 "$file_path" | awk '{print $1}')" = "$expected" ]
        return
    fi
    echo "sha256sum or shasum is required to verify DM8 media" >&2
    return 1
}

dameng_verify_iso_sha256() {
    local media_path=$1
    local iso_entries
    local actual

    iso_entries=$(unzip -Z1 "$media_path" | grep -E '\.iso$' || true)
    [ "$(printf '%s\n' "$iso_entries" | sed '/^$/d' | wc -l | tr -d ' ')" = "1" ] || {
        echo "DM8 official ZIP must contain exactly one ISO" >&2
        return 1
    }
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(unzip -p "$media_path" "$iso_entries" | sha256sum | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(unzip -p "$media_path" "$iso_entries" | shasum -a 256 | awk '{print $1}')
    else
        echo "sha256sum or shasum is required to verify the DM8 ISO" >&2
        return 1
    fi
    [ "$actual" = "$DAMENG_OFFICIAL_ISO_SHA256" ] || {
        echo "DM8 official ISO SHA-256 mismatch" >&2
        return 1
    }
}

dameng_verify_official_media() {
    local media_path=$1
    dameng_verify_sha256 "$DAMENG_OFFICIAL_MEDIA_SHA256" "$media_path" || {
        echo "DM8 official ZIP SHA-256 mismatch" >&2
        return 1
    }
    dameng_verify_iso_sha256 "$media_path"
}

dameng_download_official_media() {
    local destination=$1
    local partial=${destination}.partial

    mkdir -p "$(dirname "$destination")"
    if [ -f "$destination" ] && dameng_verify_official_media "$destination"; then
        return 0
    fi
    curl --fail --location --retry 3 --retry-delay 5 \
        --output "$partial" "$DAMENG_OFFICIAL_MEDIA_URL"
    dameng_verify_official_media "$partial"
    mv "$partial" "$destination"
}

dameng_require_arm64_docker() {
    local server_os
    local server_arch

    server_os=$(docker version --format '{{.Server.Os}}')
    server_arch=$(docker version --format '{{.Server.Arch}}')
    [ "$server_os" = "linux" ] || {
        echo "DM8 technical fixture requires a Linux Docker Server" >&2
        return 1
    }
    case "$server_arch" in
        arm64|aarch64) ;;
        *)
            echo "DM8 technical fixture requires a native ARM64 Docker Server, got $server_arch" >&2
            return 1
            ;;
    esac
}

dameng_validate_local_image() {
    local image_arch
    local media_sha256
    local iso_sha256

    image_arch=$(docker image inspect --format '{{.Architecture}}' "$DAMENG_OFFICIAL_IMAGE")
    media_sha256=$(docker image inspect --format '{{ index .Config.Labels "com.addp.dameng.media.sha256" }}' "$DAMENG_OFFICIAL_IMAGE")
    iso_sha256=$(docker image inspect --format '{{ index .Config.Labels "com.addp.dameng.iso.sha256" }}' "$DAMENG_OFFICIAL_IMAGE")
    [ "$image_arch" = "arm64" ] || return 1
    [ "$media_sha256" = "$DAMENG_OFFICIAL_MEDIA_SHA256" ] || return 1
    [ "$iso_sha256" = "$DAMENG_OFFICIAL_ISO_SHA256" ]
}

dameng_build_official_image() {
    local repository=$1
    local media_path=$2
    local cache_dir

    cache_dir=$(cd "$(dirname "$media_path")" && pwd -P)
    docker build \
        --platform linux/arm64 \
        --build-context "dm-media=$cache_dir" \
        --file "$repository/business/dameng/Containerfile" \
        --tag "$DAMENG_OFFICIAL_IMAGE" \
        "$repository/business/dameng"
    dameng_validate_local_image
}

dameng_ensure_official_image() {
    local repository=$1
    local cache_dir=${ADDP_DAMENG_MEDIA_CACHE:-${TMPDIR:-/tmp}/addp-dameng-official-media}
    local media_path=$cache_dir/$DAMENG_OFFICIAL_MEDIA_FILENAME

    for command in curl docker unzip; do
        command -v "$command" >/dev/null 2>&1 || {
            echo "$command is required to build the DM8 technical fixture" >&2
            return 1
        }
    done
    dameng_require_arm64_docker
    if docker image inspect "$DAMENG_OFFICIAL_IMAGE" >/dev/null 2>&1; then
        dameng_validate_local_image || {
            echo "existing $DAMENG_OFFICIAL_IMAGE does not match the pinned DM8 media" >&2
            return 1
        }
        return 0
    fi
    dameng_download_official_media "$media_path"
    dameng_build_official_image "$repository" "$media_path"
}

dameng_disql() {
    local container_name=$1
    shift
    docker exec --interactive "$container_name" timeout 30 "$DAMENG_HOME/bin/disql" \
        "$DAMENG_DATABASE_USER/\"$DAMENG_DATABASE_PASSWORD\"@127.0.0.1:$DAMENG_DATABASE_PORT" "$@"
}

dameng_container_ready() {
    local container_name=$1
    printf 'SET HEADING OFF\nSET FEEDBACK OFF\nSELECT 1;\nEXIT\n' | \
        dameng_disql "$container_name" 2>/dev/null | grep -Eq '(^|[[:space:]])1([[:space:]]|$)'
}
