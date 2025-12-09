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
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPILE_CACHE_DIR="${PROJECT_ROOT}/.compile-cache"

# Build configuration
BUILD_TYPE="${BUILD_TYPE:-release}"
GOOS="${GOOS:-linux}"  # Default to Linux for container builds
DIST_DIR="${PROJECT_ROOT}/dist"

# Local Go caches to avoid writing system GOPATH and avoid toolchain fetches
LOCAL_GOMODCACHE="${PROJECT_ROOT}/.gomodcache"
LOCAL_GOPATH="${PROJECT_ROOT}/.gopath"
LOCAL_GOCACHE="${PROJECT_ROOT}/.cache/go-build"
export GOMODCACHE="${LOCAL_GOMODCACHE}"
export GOPATH="${LOCAL_GOPATH}"
export GOCACHE="${LOCAL_GOCACHE}"
export GOTOOLCHAIN="local"

mkdir -p "$COMPILE_CACHE_DIR" "$LOCAL_GOMODCACHE" "$LOCAL_GOPATH" "$LOCAL_GOCACHE"

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

# Get latest source file modification time for a service
get_source_mtime() {
    local dir=$1
    local latest=0

    # Find all .go files and get the latest modification time
    while IFS= read -r file; do
        if [[ "$OSTYPE" == "darwin"* ]]; then
            mtime=$(stat -f %m "$file" 2>/dev/null || echo 0)
        else
            mtime=$(stat -c %Y "$file" 2>/dev/null || echo 0)
        fi
        [[ $mtime -gt $latest ]] && latest=$mtime
    done < <(find "$dir" -name "*.go" -type f 2>/dev/null)

    echo "$latest"
}

# Check if binary needs recompilation
needs_recompile() {
    local name=$1 dir=$2 arch=$3 binary_name=$4

    # Force rebuild if --force flag is set
    if [[ "$FORCE_REBUILD" == true ]]; then
        return 0  # needs recompile
    fi

    # Calculate output path
    local output_dir="${DIST_DIR}/${BUILD_TYPE}-${GOOS}-${arch}"
    local binary_path="${output_dir}/${binary_name}"

    # If binary doesn't exist, needs recompile
    if [ ! -f "$binary_path" ]; then
        return 0  # needs recompile
    fi

    # Get binary modification time
    if [[ "$OSTYPE" == "darwin"* ]]; then
        binary_mtime=$(stat -f %m "$binary_path" 2>/dev/null || echo 0)
    else
        binary_mtime=$(stat -c %Y "$binary_path" 2>/dev/null || echo 0)
    fi

    # Check cache file (include build context in cache name)
    local cache_file="${COMPILE_CACHE_DIR}/${name}-${BUILD_TYPE}-${GOOS}-${arch}.cache"
    if [ -f "$cache_file" ]; then
        cached_mtime=$(cat "$cache_file")
    else
        cached_mtime=0
    fi

    # Get latest source modification time
    source_mtime=$(get_source_mtime "$dir")

    # If source is newer than binary or cache, needs recompile
    if [[ $source_mtime -gt $binary_mtime ]] || [[ $source_mtime -gt $cached_mtime ]]; then
        return 0  # needs recompile
    fi

    return 1  # no need to recompile
}

# Update compile cache
update_compile_cache() {
    local name=$1 arch=$2
    local cache_file="${COMPILE_CACHE_DIR}/${name}-${BUILD_TYPE}-${GOOS}-${arch}.cache"
    date +%s > "$cache_file"
}

compile_service() {
    local name=$1 dir=$2 arch=$3

    # Detect entry point
    local entry_point="./cmd/server"

    if [[ "$name" == *"-worker" ]]; then
        # Special case for workers (transfer-worker, meta-worker, manager-worker)
        entry_point="./cmd/worker"
    elif [ ! -d "$dir/cmd/server" ] && [ -d "$dir/cmd/gateway" ]; then
        entry_point="./cmd/gateway"
    fi

    # Calculate output path: dist/{build}-{os}-{arch}/{binary_name}
    local output_dir="${DIST_DIR}/${BUILD_TYPE}-${GOOS}-${arch}"
    local binary_name="${name%-backend}"  # Remove -backend suffix (system-backend → system)
    binary_name="${binary_name%-worker}"  # Keep -worker suffix for workers (transfer-worker → transfer-worker)

    # Special handling: restore -worker suffix if it was in original name
    if [[ "$name" == *"-worker" ]]; then
        binary_name="${binary_name}-worker"
    fi

    local output_path="${output_dir}/${binary_name}"

    # Create output directory
    mkdir -p "$output_dir"

    # Check if recompilation is needed
    if needs_recompile "$name" "$dir" "$arch" "$binary_name"; then
        echo -e "${YELLOW}Compiling ${name} for ${GOOS}/${arch}...${NC}"
        cd "$dir"

        CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="$arch" go build -ldflags="-s -w" -o "$output_path" $entry_point

        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ Compiled ${binary_name} → ${output_path} ($(du -h $output_path | cut -f1))${NC}"
            update_compile_cache "$name" "$arch"
            cd - > /dev/null
            return 0
        else
            echo -e "${RED}✗ Failed ${name}${NC}"
            cd - > /dev/null
            return 1
        fi
    else
        # Use cached binary
        echo -e "${CYAN}⚡ Using cached ${binary_name} for ${GOOS}/${arch} (no source changes)${NC}"
        return 0
    fi
}

SERVICES=(
    "system-backend:system/backend"
    "manager-backend:manager/backend"
    "manager-worker:manager/backend"
    "meta-backend:meta/backend"
    "meta-worker:meta/backend"
    "transfer-backend:transfer/backend"
    "transfer-worker:transfer/backend"
    "orchestrator-backend:orchestrator/backend"
    "develop-backend:develop/backend"
    "gateway:gateway"
)
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
                if needs_recompile "$name" "$dir" "$a" "$binary_name" 2>/dev/null; then
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
            if needs_recompile "$name" "$dir" "$ARCH" "$binary_name" 2>/dev/null; then
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
