#!/usr/bin/env bash
# Canonical official openGauss Docker media facts and loader shared by ADDP gates and Business.

OPENGAUSS_OFFICIAL_VERSION=6.0.6
OPENGAUSS_OFFICIAL_IMAGE=opengauss:6.0.6

opengauss_select_official_media() {
    local machine_arch=${1:-$(uname -m)}
    case "$machine_arch" in
        x86_64|amd64)
            OPENGAUSS_OFFICIAL_MACHINE_ARCH=x86_64
            OPENGAUSS_OFFICIAL_IMAGE_ARCH=amd64
            OPENGAUSS_OFFICIAL_MEDIA_URL=https://opengauss.obs.cn-south-1.myhuaweicloud.com/6.0.6/openGauss-Docker-6.0.6-x86_64.tar
            OPENGAUSS_OFFICIAL_MEDIA_SHA256=4c50bd0f8884f3c0716872ee7d277bad99cdee3d75193a948c7ae9d4b7dfd14d
            ;;
        aarch64|arm64)
            OPENGAUSS_OFFICIAL_MACHINE_ARCH=aarch64
            OPENGAUSS_OFFICIAL_IMAGE_ARCH=arm64
            OPENGAUSS_OFFICIAL_MEDIA_URL=https://opengauss.obs.cn-south-1.myhuaweicloud.com/6.0.6/openGauss-Docker-6.0.6-aarch64.tar
            OPENGAUSS_OFFICIAL_MEDIA_SHA256=00ad2206ac93cf28c7702cd624b7c59dc1a146f9dca1f81ed19eeac430416c0b
            ;;
        *)
            echo "unsupported openGauss official-media architecture: $machine_arch" >&2
            return 1
            ;;
    esac
    OPENGAUSS_OFFICIAL_MEDIA_FILENAME=${OPENGAUSS_OFFICIAL_MEDIA_URL##*/}
    export OPENGAUSS_OFFICIAL_MACHINE_ARCH OPENGAUSS_OFFICIAL_IMAGE_ARCH
    export OPENGAUSS_OFFICIAL_MEDIA_URL OPENGAUSS_OFFICIAL_MEDIA_SHA256 OPENGAUSS_OFFICIAL_MEDIA_FILENAME
}

opengauss_verify_sha256() {
    local expected=$1
    local file_path=$2
    if [ "$(uname -s)" = "Darwin" ] && command -v shasum >/dev/null 2>&1; then
        [ "$(shasum -a 256 "$file_path" | awk '{print $1}')" = "$expected" ]
        return
    fi
    if command -v sha256sum >/dev/null 2>&1; then
        printf '%s  %s\n' "$expected" "$file_path" | sha256sum --check --status
        return
    fi
    if command -v shasum >/dev/null 2>&1; then
        [ "$(shasum -a 256 "$file_path" | awk '{print $1}')" = "$expected" ]
        return
    fi
    echo "sha256sum or shasum is required to verify openGauss media" >&2
    return 1
}

opengauss_ensure_official_image() {
    opengauss_select_official_media "${1:-$(uname -m)}" || return 1
    for command in curl docker; do
        if ! command -v "$command" >/dev/null 2>&1; then
            echo "$command is required to load openGauss official media" >&2
            return 1
        fi
    done

    if docker image inspect "$OPENGAUSS_OFFICIAL_IMAGE" >/dev/null 2>&1; then
        local loaded_arch
        loaded_arch=$(docker image inspect --format '{{.Architecture}}' "$OPENGAUSS_OFFICIAL_IMAGE")
        if [ "$loaded_arch" != "$OPENGAUSS_OFFICIAL_IMAGE_ARCH" ]; then
            echo "existing $OPENGAUSS_OFFICIAL_IMAGE architecture is $loaded_arch, want $OPENGAUSS_OFFICIAL_IMAGE_ARCH" >&2
            return 1
        fi
        return 0
    fi

    local cache_dir=${ADDP_OPENGAUSS_MEDIA_CACHE:-${TMPDIR:-/tmp}/addp-opengauss-official-media}
    local media_path=$cache_dir/$OPENGAUSS_OFFICIAL_MEDIA_FILENAME
    mkdir -p "$cache_dir"
    if [ ! -f "$media_path" ] && [ -f "$media_path.partial" ] && opengauss_verify_sha256 "$OPENGAUSS_OFFICIAL_MEDIA_SHA256" "$media_path.partial"; then
        mv "$media_path.partial" "$media_path"
    fi
    if [ ! -f "$media_path" ] || ! opengauss_verify_sha256 "$OPENGAUSS_OFFICIAL_MEDIA_SHA256" "$media_path"; then
        rm -f "$media_path.partial"
        echo "Downloading pinned openGauss $OPENGAUSS_OFFICIAL_VERSION $OPENGAUSS_OFFICIAL_MACHINE_ARCH official media"
        curl --fail --location --retry 3 --retry-delay 5 --output "$media_path.partial" "$OPENGAUSS_OFFICIAL_MEDIA_URL"
        opengauss_verify_sha256 "$OPENGAUSS_OFFICIAL_MEDIA_SHA256" "$media_path.partial"
        mv "$media_path.partial" "$media_path"
    fi
    docker load --input "$media_path"
    local loaded_arch
    loaded_arch=$(docker image inspect --format '{{.Architecture}}' "$OPENGAUSS_OFFICIAL_IMAGE")
    if [ "$loaded_arch" != "$OPENGAUSS_OFFICIAL_IMAGE_ARCH" ]; then
        echo "loaded $OPENGAUSS_OFFICIAL_IMAGE architecture is $loaded_arch, want $OPENGAUSS_OFFICIAL_IMAGE_ARCH" >&2
        return 1
    fi
}
