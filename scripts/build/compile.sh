#!/bin/bash
# Binary Compiler with Smart Cache
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
MULTI_ARCH=false
FORCE_REBUILD=false
LOCAL_BUILD=false
SELECTED_SERVICES="all"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${PROJECT_ROOT}/scripts/dev/build-identity.sh"

# Build configuration
BUILD_TYPE="${BUILD_TYPE:-release}"
GOOS="${GOOS:-linux}"  # Default to Linux for container builds
DIST_DIR="${PROJECT_ROOT}/dist"

# Local Go caches to avoid writing system GOPATH and avoid toolchain fetches
LOCAL_GOMODCACHE="${ADDP_BUILD_GOMODCACHE:-${PROJECT_ROOT}/.gomodcache}"
LOCAL_GOPATH="${ADDP_BUILD_GOPATH:-${PROJECT_ROOT}/.gopath}"
LOCAL_GOCACHE="${ADDP_BUILD_GOCACHE:-${PROJECT_ROOT}/.cache/go-build}"
export GOMODCACHE="${LOCAL_GOMODCACHE}"
export GOPATH="${LOCAL_GOPATH}"
export GOCACHE="${LOCAL_GOCACHE}"
export GOTOOLCHAIN="local"

mkdir -p "$LOCAL_GOMODCACHE" "$LOCAL_GOPATH" "$LOCAL_GOCACHE"

while [[ $# -gt 0 ]]; do
    case $1 in
        --arch)
            ARCH="$2"
            [[ "$ARCH" == "both" ]] && MULTI_ARCH=true && ARCH="amd64,arm64"
            shift 2
            ;;
        --force)
            FORCE_REBUILD=true
            shift
            ;;
        --local)
            LOCAL_BUILD=true
            shift
            ;;
        --services)
            [ "$#" -ge 2 ] || { echo "--services requires a comma-separated service list" >&2; exit 1; }
            SELECTED_SERVICES="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown: $1${NC}"
            exit 1
            ;;
    esac
done

# Override GOOS for local builds (detects native OS)
if [[ "$LOCAL_BUILD" == true ]]; then
    GOOS="$(go env GOOS)"
    echo -e "${YELLOW}Local build mode enabled: Compiling for ${GOOS}${NC}"
fi

echo -e "${BLUE}ADDP Binary Compiler${NC}"
echo -e "Architecture: ${GREEN}${ARCH}${NC}"
echo -e "Smart Cache: ${GREEN}Enabled${NC}\n"

# The cache is keyed by the real Go dependency graph, including common/ and
# build settings. An mtime check would reuse stale production binaries.
service_entry_point() {
    local name=$1 dir=$2
    if [[ "$name" == *-worker ]]; then
        if [ "$name" = "transfer-continuous-worker" ]; then
            printf './cmd/continuous-worker\n'
        else
            printf './cmd/worker\n'
        fi
    elif [ ! -d "$dir/cmd/server" ] && [ -d "$dir/cmd/gateway" ]; then
        printf './cmd/gateway\n'
    else
        printf './cmd/server\n'
    fi
}

needs_recompile() {
    local name=$1 dir=$2 arch=$3 binary_name=$4
    [ "$FORCE_REBUILD" = false ] || return 0
    local relative_output="dist/${BUILD_TYPE}-${GOOS}-${arch}/${binary_name}"
    local entry_point
    entry_point=$(service_entry_point "$name" "$dir")
    [ -x "${PROJECT_ROOT}/${relative_output}" ] || return 0
    local fingerprint_path current_fingerprint recorded_fingerprint
    fingerprint_path=$(addp_build_fingerprint_path "$relative_output")
    [ -f "$fingerprint_path" ] || return 0
    current_fingerprint=$(compile_source_fingerprint "$dir" "$arch" "$entry_point") || return 0
    recorded_fingerprint=$(sed -n '1p' "$fingerprint_path")
    [ "$current_fingerprint" = "$recorded_fingerprint" ] && return 1
    return 0
}

NATIVE_GOOS="$(go env GOOS)"
NATIVE_GOARCH="$(go env GOARCH)"
GO_DOCKER_IMAGE="golang:1.24"

compile_source_fingerprint() {
    local dir=$1 arch=$2 entry_point=$3
    if [[ "$GOOS" != "$NATIVE_GOOS" ]]; then
        docker run --rm \
            --platform "${GOOS}/${arch}" \
            -v "${PROJECT_ROOT}:/workspace" \
            -v "${LOCAL_GOMODCACHE}:/go/pkg/mod" \
            -v "${LOCAL_GOCACHE}:/root/.cache/go-build" \
            -e GOMODCACHE=/go/pkg/mod \
            -e GOCACHE=/root/.cache/go-build \
            -e GOTOOLCHAIN=local \
            -e CGO_ENABLED=1 \
            -w "/workspace/${dir}" \
            "${GO_DOCKER_IMAGE}" \
            bash -c 'PROJECT_ROOT=/workspace; source /workspace/scripts/dev/build-identity.sh; addp_source_fingerprint "/workspace/$1" "$2"' \
            _ "$dir" "$entry_point"
    else
        (export GOOS GOARCH="$arch" CGO_ENABLED=1;
          addp_source_fingerprint "${PROJECT_ROOT}/${dir}" "$entry_point")
    fi
}

compile_service() {
    local name=$1 dir=$2 arch=$3
    COMPILE_WAS_BUILT=false

    local entry_point
    entry_point=$(service_entry_point "$name" "$dir")

    # Calculate output path: dist/{build}-{os}-{arch}/{binary_name}
    local output_dir="${DIST_DIR}/${BUILD_TYPE}-${GOOS}-${arch}"
    local binary_name="${name%-backend}"
    binary_name="${binary_name%-worker}"

    if [[ "$name" == *"-worker" ]]; then
        binary_name="${binary_name}-worker"
    fi

    local output_path="${output_dir}/${binary_name}"

    mkdir -p "$output_dir"

    if needs_recompile "$name" "$dir" "$arch" "$binary_name"; then
        echo -e "${YELLOW}Compiling ${name} for ${GOOS}/${arch}...${NC}"

        local build_ok=false
        local fingerprint_before fingerprint_after git_commit built_at build_id ldflags
        fingerprint_before=$(compile_source_fingerprint "$dir" "$arch" "$entry_point") || return 1
        git_commit=$(git -C "$PROJECT_ROOT" rev-parse HEAD)
        built_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
        build_id="$(date -u '+%Y%m%dT%H%M%SZ')-${name}-$$"
        ldflags="-s -w -X github.com/addp/common/buildinfo.BuildID=${build_id} -X github.com/addp/common/buildinfo.GitCommit=${git_commit} -X github.com/addp/common/buildinfo.SourceFingerprint=${fingerprint_before} -X github.com/addp/common/buildinfo.BuiltAt=${built_at}"
        local temporary_output="${output_path}.tmp.$$"

        if [[ "$GOOS" != "$NATIVE_GOOS" ]]; then
            # Cross-OS compilation: use Docker to handle CGO dependencies (e.g. go-duckdb)
            local rel_output="${temporary_output#$PROJECT_ROOT/}"
            if docker run --rm \
                --platform "${GOOS}/${arch}" \
                -v "${PROJECT_ROOT}:/workspace" \
                -v "${LOCAL_GOMODCACHE}:/go/pkg/mod" \
                -v "${LOCAL_GOCACHE}:/root/.cache/go-build" \
                -e GOMODCACHE=/go/pkg/mod \
                -e GOCACHE=/root/.cache/go-build \
                -e GOTOOLCHAIN=local \
                -e CGO_ENABLED=1 \
                -w "/workspace/${dir}" \
                "${GO_DOCKER_IMAGE}" \
                go build -ldflags="$ldflags" -o "/workspace/${rel_output}" "$entry_point"; then
                build_ok=true
            fi
        else
            # Native build
            cd "$dir"
            if CGO_ENABLED=1 GOOS="${GOOS}" GOARCH="$arch" go build -ldflags="$ldflags" -o "$temporary_output" "$entry_point"; then
                build_ok=true
            fi
            cd - > /dev/null
        fi

        fingerprint_after=$(compile_source_fingerprint "$dir" "$arch" "$entry_point") || build_ok=false
        if [[ "$build_ok" == true && "$fingerprint_after" == "$fingerprint_before" ]]; then
            mv -f "$temporary_output" "$output_path"
            printf '%s\n' "$fingerprint_after" > "$(addp_build_fingerprint_path "dist/${BUILD_TYPE}-${GOOS}-${arch}/${binary_name}")"
            COMPILE_WAS_BUILT=true
            echo -e "${GREEN}✓ Compiled ${binary_name} → ${output_path} ($(du -h $output_path | cut -f1))${NC}"
            return 0
        else
            rm -f "$temporary_output"
            echo -e "${RED}✗ Failed ${name}${NC}"
            return 1
        fi
    else
        echo -e "${CYAN}⚡ Using cached ${binary_name} for ${GOOS}/${arch} (no source changes)${NC}"
        return 0
    fi
}

SERVICES=(
    "system-backend:system/backend"
    "manager-backend:manager/backend"
    "meta-backend:meta/backend"
    "meta-worker:meta/backend"
    "transfer-backend:transfer/backend"
    "transfer-bounded-worker:transfer/backend"
    "transfer-continuous-worker:transfer/backend"
    "orchestrator-backend:orchestrator/backend"
    "develop-backend:develop/backend"
    "service-backend:service/backend"
    "monitor-backend:monitor/backend"
    "standard-backend:standard/backend"
    "model-backend:model/backend"
    "quality-backend:quality/backend"
    "quality-worker:quality/backend"
    "security-backend:security/backend"
    "security-worker:security/backend"
    "asset-backend:asset/backend"
    "catalog-backend:catalog/backend"
    "ontology-backend:ontology/backend"
    "workbench-backend:workbench/backend"
    "portal-backend:portal/backend"
    "graph-backend:graph/backend"
    "inference-backend:inference/backend"
    "gateway:gateway"
)
if [ "$SELECTED_SERVICES" != all ]; then
    IFS=',' read -ra selected <<< "$SELECTED_SERVICES"
    [ "${#selected[@]}" -gt 0 ] || { echo "--services must name at least one service" >&2; exit 1; }
    filtered=()
    for name in "${selected[@]}"; do
        found=false
        for svc in "${SERVICES[@]}"; do
            if [ "${svc%%:*}" = "$name" ]; then
                filtered+=("$svc")
                found=true
                break
            fi
        done
        [ "$found" = true ] || { echo "unknown --services entry: $name" >&2; exit 1; }
    done
    [ "${#filtered[@]}" -eq "${#selected[@]}" ] &&
        [ "$(printf '%s\n' "${selected[@]}" | LC_ALL=C sort -u | wc -l | tr -d ' ')" -eq "${#selected[@]}" ] ||
        { echo "--services contains duplicates" >&2; exit 1; }
    SERVICES=("${filtered[@]}")
fi
failed=()
compiled=0
cached=0

for svc in "${SERVICES[@]}"; do
    name="${svc%%:*}"
    dir="${svc#*:}"
    echo -e "\n${BLUE}Building: ${name}${NC}"

    if [[ "$MULTI_ARCH" == true ]]; then
        IFS=',' read -ra ARCHS <<< "$ARCH"
        for a in "${ARCHS[@]}"; do
            # Calculate binary name for checking
            binary_name="${name%-backend}"
            binary_name="${binary_name%-worker}"
            [[ "$name" == *"-worker" ]] && binary_name="${binary_name}-worker"

            if compile_service "$name" "$dir" "$a"; then
                if [ "$COMPILE_WAS_BUILT" = true ]; then
                    ((compiled++)) || true
                else
                    ((cached++)) || true
                fi
            else
                failed+=("$name ($a)")
            fi
        done
    else
        # Calculate binary name for checking
        binary_name="${name%-backend}"
        binary_name="${binary_name%-worker}"
        [[ "$name" == *"-worker" ]] && binary_name="${binary_name}-worker"

        if compile_service "$name" "$dir" "$ARCH"; then
            if [ "$COMPILE_WAS_BUILT" = true ]; then
                ((compiled++)) || true
            else
                ((cached++)) || true
            fi
        else
            failed+=("$name")
        fi
    fi
done

echo -e "\n${BLUE}Summary${NC}"
echo -e "${GREEN}Compiled: ${compiled}${NC}"
echo -e "${CYAN}Cached: ${cached}${NC}"

if [ ${#failed[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All binaries ready${NC}"
    exit 0
else
    echo -e "${RED}✗ Failed: ${failed[*]}${NC}"
    exit 1
fi
