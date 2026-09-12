#!/usr/bin/env bash
# kingbase-official-media.sh - Canonical KingbaseES V9R1C10 official-media facts and loader.

KINGBASE_OFFICIAL_VERSION=V009R001C010B0004
KINGBASE_OFFICIAL_IMAGE=kingbase_v009r001c010b0004_single_x86:v1
KINGBASE_OFFICIAL_MEDIA_URL=https://kingbase.oss-cn-beijing.aliyuncs.com/upload/KESV9-baseline/allmode/V009R001C010/docker/KingbaseES_V009R001C010B0004_x86_64_Docker.tar
KINGBASE_OFFICIAL_MEDIA_FILENAME=KingbaseES_V009R001C010B0004_x86_64_Docker.tar
KINGBASE_OFFICIAL_MEDIA_SHA256=16a436608cc204349e510cb136b8fc1fcbdf6874aee7b204cdac20a3522282da
KINGBASE_OFFICIAL_MEDIA_MD5=26bb99891becc52f533488aac5fabfb6
KINGBASE_OFFICIAL_IMAGE_ARCH=amd64
KINGBASE_DATABASE_PORT=54321
KINGBASE_HOME=/home/kingbase/install/kingbase

kingbase_verify_sha256() {
    local expected=$1
    local file_path=$2
    if command -v sha256sum >/dev/null 2>&1; then
        printf '%s  %s\n' "$expected" "$file_path" | sha256sum --check --status
        return
    fi
    if command -v shasum >/dev/null 2>&1; then
        [ "$(shasum -a 256 "$file_path" | awk '{print $1}')" = "$expected" ]
        return
    fi
    echo "sha256sum or shasum is required to verify KingbaseES media" >&2
    return 1
}

kingbase_download_official_media() {
    local destination=$1
    local partial=${destination}.partial

    mkdir -p "$(dirname "$destination")"
    if [ -f "$destination" ] && kingbase_verify_sha256 "$KINGBASE_OFFICIAL_MEDIA_SHA256" "$destination"; then
        return 0
    fi
    : > "$partial"
    curl --user-agent 'Mozilla/5.0' \
        --referer 'https://www.kingbase.com.cn/download.html' \
        --header 'Range: bytes=0-' \
        --fail --location --retry 3 --retry-delay 5 \
        --output "$partial" "$KINGBASE_OFFICIAL_MEDIA_URL"
    kingbase_verify_sha256 "$KINGBASE_OFFICIAL_MEDIA_SHA256" "$partial"
    mv "$partial" "$destination"
}

kingbase_ensure_official_image() {
    if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
        echo "KingbaseES official gates require Linux x86_64" >&2
        return 1
    fi
    for command in curl docker; do
        if ! command -v "$command" >/dev/null 2>&1; then
            echo "$command is required to load KingbaseES official media" >&2
            return 1
        fi
    done

    if docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1; then
        local loaded_arch
        loaded_arch=$(docker image inspect --format '{{.Architecture}}' "$KINGBASE_OFFICIAL_IMAGE")
        if [ "$loaded_arch" != "$KINGBASE_OFFICIAL_IMAGE_ARCH" ]; then
            echo "existing $KINGBASE_OFFICIAL_IMAGE architecture is $loaded_arch, want $KINGBASE_OFFICIAL_IMAGE_ARCH" >&2
            return 1
        fi
        return 0
    fi

    local cache_dir=${ADDP_KINGBASE_MEDIA_CACHE:-${TMPDIR:-/tmp}/addp-kingbase-official-media}
    local media_path=$cache_dir/$KINGBASE_OFFICIAL_MEDIA_FILENAME
    kingbase_download_official_media "$media_path"
    docker load --input "$media_path"
    local loaded_arch
    loaded_arch=$(docker image inspect --format '{{.Architecture}}' "$KINGBASE_OFFICIAL_IMAGE")
    if [ "$loaded_arch" != "$KINGBASE_OFFICIAL_IMAGE_ARCH" ]; then
        echo "loaded $KINGBASE_OFFICIAL_IMAGE architecture is $loaded_arch, want $KINGBASE_OFFICIAL_IMAGE_ARCH" >&2
        return 1
    fi
}

kingbase_ksql() {
    local container_name=$1
    shift
    docker exec "$container_name" "$KINGBASE_HOME/bin/ksql" "$@"
}

kingbase_container_ready() {
    local container_name=$1
    local database_user=$2
    kingbase_ksql "$container_name" -U "$database_user" -d kingbase -p "$KINGBASE_DATABASE_PORT" -At -c 'SELECT 1' 2>/dev/null | grep -Fxq '1'
}

kingbase_validate_license_input() {
    local repository=$1
    local license_file=${ADDP_KINGBASE_LICENSE_FILE:-}
    local license_sha256=${ADDP_KINGBASE_LICENSE_SHA256:-}

    if [ -z "$license_file" ] || [ "${license_file#/}" = "$license_file" ]; then
        echo "ADDP_KINGBASE_LICENSE_FILE must be an absolute owner-managed path" >&2
        return 1
    fi
    if [ ! -f "$license_file" ]; then
        echo "ADDP_KINGBASE_LICENSE_FILE does not identify a regular file" >&2
        return 1
    fi
    case "$(cd "$(dirname "$license_file")" && pwd)/$(basename "$license_file")" in
        "$repository"/*)
            echo "ADDP_KINGBASE_LICENSE_FILE must be outside the repository" >&2
            return 1
            ;;
    esac
    case "$license_sha256" in
        [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
        *) echo "ADDP_KINGBASE_LICENSE_SHA256 must be a lowercase SHA-256" >&2; return 1 ;;
    esac
    if ! kingbase_verify_sha256 "$license_sha256" "$license_file"; then
        echo "KingbaseES License SHA-256 mismatch" >&2
        return 1
    fi
}

kingbase_install_license_into_created_container() {
    local container_name=$1
    local license_file=$2
    local work_dir=$3
    local staged_license=$work_dir/license.dat

    cp "$license_file" "$staged_license"
    chmod 0644 "$staged_license"
    docker cp "$staged_license" "$container_name:$KINGBASE_HOME/bin/license.dat"
    chmod 0600 "$staged_license"
}
