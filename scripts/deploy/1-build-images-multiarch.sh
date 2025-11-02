#!/bin/bash
# =============================================================================
# ADDP Multi-Architecture Image Builder with Smart Cache
# =============================================================================
# Description: Build multi-arch images with intelligent cache invalidation
# Features:
#   - Detects source code changes by comparing modification timestamps
#   - Automatically rebuilds only changed services (--no-cache)
#   - Uses Docker cache for unchanged services (fast builds)
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Default values
REGISTRY="${REGISTRY:-localhost:5001}"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARCHITECTURES=("amd64" "arm64")
FORCE_REBUILD=false
BUILD_CACHE_DIR="${PROJECT_ROOT}/.build-cache"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --force)
            FORCE_REBUILD=true
            shift
            ;;
        --skip-cache)
            # Legacy support
            FORCE_REBUILD=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

cd "$PROJECT_ROOT"

# Create build cache directory
mkdir -p "$BUILD_CACHE_DIR"

# =============================================================================
# Auto-cleanup: Clean old deployment packages (keep latest 3)
# =============================================================================

echo -e "${CYAN}Checking for old deployment packages...${NC}"
old_packages=$(ls -t addp-deploy-*.tar.gz 2>/dev/null | tail -n +4 || true)
if [ -n "$old_packages" ]; then
    count=$(echo "$old_packages" | wc -l | tr -d ' ')
    echo -e "${YELLOW}Found $count old package(s), removing...${NC}"
    echo "$old_packages" | xargs rm -f
    echo -e "${GREEN}✓ Cleaned old packages${NC}"
else
    echo -e "${GREEN}✓ No old packages to clean${NC}"
fi
echo ""

# =============================================================================
# Auto-cleanup: Clean dangling Docker images silently
# =============================================================================

dangling=$(docker images -f "dangling=true" -q 2>/dev/null | head -20 || true)
if [ -n "$dangling" ]; then
    count=$(echo "$dangling" | wc -l | tr -d ' ')
    echo -e "${YELLOW}Cleaning $count dangling Docker image(s)...${NC}"
    echo "$dangling" | xargs docker rmi >/dev/null 2>&1 || true
    echo -e "${GREEN}✓ Cleaned dangling images${NC}"
    echo ""
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Smart Multi-Arch Builder${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
echo -e "Architectures: ${GREEN}amd64, arm64${NC}"
echo -e "Smart Cache: ${GREEN}Enabled${NC}"
if [ "$FORCE_REBUILD" = true ]; then
    echo -e "Force Rebuild: ${YELLOW}Yes (--force)${NC}"
fi
echo ""

# Service list: name:directory:source_dirs (comma-separated directories to monitor)
SERVICES=(
    "system-backend:system/backend:system/backend/cmd,system/backend/internal,system/backend/pkg"
    "manager-backend:manager/backend:manager/backend/cmd,manager/backend/internal,manager/backend/pkg"
    "meta-backend:meta/backend:meta/backend/cmd,meta/backend/internal,meta/backend/pkg"
    "transfer-backend:transfer/backend:transfer/backend/cmd,transfer/backend/internal,transfer/backend/pkg"
    "transfer-worker:transfer/backend:transfer/backend/cmd,transfer/backend/internal,transfer/backend/pkg"
    "gateway:gateway:gateway/cmd,gateway/internal,gateway/pkg"
    "portal:portal/frontend:portal/frontend/src,portal/frontend/public,portal/frontend/package.json,portal/frontend/vite.config.js"
    "system-frontend:system/frontend:system/frontend/src,system/frontend/public,system/frontend/package.json,system/frontend/vite.config.js,common-frontend"
    "manager-frontend:manager/frontend:manager/frontend/src,manager/frontend/public,manager/frontend/package.json,manager/frontend/vite.config.js"
    "meta-frontend:meta/frontend:meta/frontend/src,meta/frontend/public,meta/frontend/package.json,meta/frontend/vite.config.js"
    "transfer-frontend:transfer/frontend:transfer/frontend/src,transfer/frontend/public,transfer/frontend/package.json,transfer/frontend/vite.config.js,common-frontend"
    "nginx:nginx:nginx/nginx.conf,nginx/Dockerfile"
)

# Function to get latest modification time of source files
get_source_mtime() {
    local service_dir=$1
    local source_dirs=$2
    local max_mtime=0

    # Split source_dirs by comma
    IFS=',' read -ra DIRS <<< "$source_dirs"

    for dir in "${DIRS[@]}"; do
        local full_path="${PROJECT_ROOT}/${dir}"

        if [ ! -e "$full_path" ]; then
            continue
        fi

        # Find all source files and get their modification times
        # For frontends: *.vue, *.js, *.ts, *.json, *.css, *.html
        # For backends: *.go
        # For nginx: nginx.conf, Dockerfile
        local files_mtime
        if [[ "$service_dir" == *"frontend"* ]]; then
            files_mtime=$(find "$full_path" -type f \( -name "*.vue" -o -name "*.js" -o -name "*.ts" -o -name "*.json" -o -name "*.css" -o -name "*.html" \) -exec stat -f "%m" {} \; 2>/dev/null | sort -rn | head -1)
        elif [[ "$service_dir" == "nginx" ]]; then
            files_mtime=$(find "$full_path" -type f \( -name "*.conf" -o -name "Dockerfile" \) -exec stat -f "%m" {} \; 2>/dev/null | sort -rn | head -1)
        else
            # Backend Go files
            files_mtime=$(find "$full_path" -type f -name "*.go" -exec stat -f "%m" {} \; 2>/dev/null | sort -rn | head -1)
        fi

        if [ -n "$files_mtime" ] && [ "$files_mtime" -gt "$max_mtime" ]; then
            max_mtime=$files_mtime
        fi
    done

    echo "$max_mtime"
}

# Function to check if service needs rebuild
needs_rebuild() {
    local service=$1
    local arch=$2
    local source_mtime=$3
    local cache_file="${BUILD_CACHE_DIR}/${service}-${arch}.timestamp"

    # Force rebuild if requested
    if [ "$FORCE_REBUILD" = true ]; then
        return 0  # true - needs rebuild
    fi

    # No cache file = first build
    if [ ! -f "$cache_file" ]; then
        return 0  # true - needs rebuild
    fi

    # Get last build time
    local last_build_time=$(cat "$cache_file")

    # Compare timestamps
    if [ "$source_mtime" -gt "$last_build_time" ]; then
        return 0  # true - source is newer, needs rebuild
    fi

    return 1  # false - cache is valid, skip rebuild
}

# Function to update build cache
update_build_cache() {
    local service=$1
    local arch=$2
    local cache_file="${BUILD_CACHE_DIR}/${service}-${arch}.timestamp"

    # Record current timestamp
    date +%s > "$cache_file"
}

# Function to build image for a specific architecture
build_arch_image() {
    local service=$1
    local service_dir=$2
    local source_dirs=$3
    local arch=$4
    local image_name="${REGISTRY}/addp-${service}:latest-${arch}"

    # Get source modification time
    local source_mtime=$(get_source_mtime "$service_dir" "$source_dirs")

    # Check if rebuild is needed
    local use_cache=true
    local rebuild_reason=""

    if needs_rebuild "$service" "$arch" "$source_mtime"; then
        use_cache=false
        if [ "$FORCE_REBUILD" = true ]; then
            rebuild_reason="(forced rebuild)"
        elif [ ! -f "${BUILD_CACHE_DIR}/${service}-${arch}.timestamp" ]; then
            rebuild_reason="(first build)"
        else
            rebuild_reason="(source changed)"
        fi
    else
        rebuild_reason="(using cache - no source changes)"
    fi

    echo -e "${YELLOW}Building ${service} for ${arch}... ${CYAN}${rebuild_reason}${NC}"

    # Determine Dockerfile and build context
    local dockerfile_path=""
    local build_context=""

    case "$service" in
        *-backend)
            # Backend services use prebuilt binaries
            dockerfile_path="${service_dir}/Dockerfile.prebuilt"
            build_context="."

            # Check if binary exists
            local binary_path="${service_dir}/server-${arch}"
            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Binary not found at ${binary_path}${NC}"
                echo -e "${YELLOW}Hint: Run ./scripts/deploy/0-compile-binaries.sh --arch both first${NC}"
                return 1
            fi
            ;;
        transfer-worker)
            # Transfer worker special case
            dockerfile_path="${service_dir}/Dockerfile.prebuilt.worker"
            build_context="."

            local binary_path="${service_dir}/worker-${arch}"
            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Worker binary not found at ${binary_path}${NC}"
                return 1
            fi
            ;;
        gateway)
            # Gateway
            dockerfile_path="${service_dir}/Dockerfile.prebuilt"
            build_context="."

            local binary_path="${service_dir}/server-${arch}"
            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Gateway binary not found at ${binary_path}${NC}"
                return 1
            fi
            ;;
        system-frontend|transfer-frontend)
            # Frontends that need common-frontend
            dockerfile_path="${service_dir}/Dockerfile"
            build_context="."
            ;;
        *)
            # Other services (portal, manager-frontend, meta-frontend, nginx)
            dockerfile_path="${service_dir}/Dockerfile"
            build_context="${service_dir}"
            ;;
    esac

    # Build command - add --no-cache only if rebuild is needed
    local cache_flag=""
    if [ "$use_cache" = false ]; then
        cache_flag="--no-cache"
    fi

    local build_cmd="docker build \
        --platform linux/${arch} \
        --tag ${image_name} \
        ${cache_flag} \
        -f ${dockerfile_path} \
        ${build_context}"

    # Build the image (suppress output for cleaner logs)
    if eval "$build_cmd" >/dev/null 2>&1; then
        # Push to registry
        if docker push "${image_name}" >/dev/null 2>&1; then
            # Update build cache on success
            update_build_cache "$service" "$arch"

            if [ "$use_cache" = false ]; then
                echo -e "${GREEN}✓ Built and pushed ${service} (${arch}) - REBUILT${NC}"
            else
                echo -e "${GREEN}✓ Built and pushed ${service} (${arch}) - CACHED${NC}"
            fi
            return 0
        else
            echo -e "${RED}✗ Failed to push ${service} (${arch})${NC}"
            return 1
        fi
    else
        echo -e "${RED}✗ Failed to build ${service} (${arch})${NC}"
        return 1
    fi
}

# Function to create and push manifest list
create_manifest() {
    local service=$1
    local image_base="${REGISTRY}/addp-${service}:latest"

    echo -e "${YELLOW}Creating manifest list for ${service}...${NC}"

    # Use buildx imagetools to create multi-platform manifest directly from registry
    if docker buildx imagetools create \
        --tag "${image_base}" \
        "${image_base}-amd64" \
        "${image_base}-arm64" >/dev/null 2>&1; then

        echo -e "${GREEN}✓ Manifest created for ${service}${NC}"
        return 0
    else
        echo -e "${RED}✗ Failed to create manifest for ${service}${NC}"
        return 1
    fi
}

# Main build loop
failed_services=()
rebuilt_services=()
cached_services=()
total_services=${#SERVICES[@]}
current=0

for svc in "${SERVICES[@]}"; do
    current=$((current + 1))
    service_name="${svc%%:*}"
    rest="${svc#*:}"
    service_dir="${rest%%:*}"
    source_dirs="${rest#*:}"

    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}[${current}/${total_services}] ${service_name}${NC}"
    echo -e "${BLUE}========================================${NC}"

    # Track if this service was rebuilt
    service_rebuilt=false

    # Build for each architecture
    build_success=true
    for arch in "${ARCHITECTURES[@]}"; do
        # Check if rebuild needed (before build)
        source_mtime=$(get_source_mtime "$service_dir" "$source_dirs")
        if needs_rebuild "$service_name" "$arch" "$source_mtime"; then
            service_rebuilt=true
        fi

        if ! build_arch_image "$service_name" "$service_dir" "$source_dirs" "$arch"; then
            build_success=false
            failed_services+=("${service_name} (${arch})")
        fi
    done

    # Track rebuild status
    if [ "$service_rebuilt" = true ]; then
        rebuilt_services+=("$service_name")
    else
        cached_services+=("$service_name")
    fi

    # Create manifest list if both architectures succeeded
    if [ "$build_success" = true ]; then
        if ! create_manifest "$service_name"; then
            failed_services+=("${service_name} (manifest)")
        fi
    else
        echo -e "${RED}✗ Skipping manifest creation for ${service_name} (build failed)${NC}"
        failed_services+=("${service_name} (manifest)")
    fi
done

# Summary
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Build Summary${NC}"
echo -e "${BLUE}========================================${NC}"

if [ ${#rebuilt_services[@]} -gt 0 ]; then
    echo -e "${YELLOW}Rebuilt (source changed):${NC}"
    for service in "${rebuilt_services[@]}"; do
        echo -e "  ${YELLOW}↻${NC} ${service}"
    done
    echo ""
fi

if [ ${#cached_services[@]} -gt 0 ]; then
    echo -e "${CYAN}Cached (no changes):${NC}"
    for service in "${cached_services[@]}"; do
        echo -e "  ${CYAN}⚡${NC} ${service}"
    done
    echo ""
fi

if [ ${#failed_services[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All services built successfully for both architectures${NC}"
    echo ""
    echo -e "${CYAN}Cache efficiency: ${#cached_services[@]} cached, ${#rebuilt_services[@]} rebuilt${NC}"
    echo ""
    echo -e "${YELLOW}Verify multi-arch images:${NC}"
    echo -e "  docker buildx imagetools inspect ${REGISTRY}/addp-system-backend:latest"
    echo ""
    echo -e "${YELLOW}To force rebuild all services:${NC}"
    echo -e "  $0 --force"
    echo ""
    exit 0
else
    echo -e "${RED}✗ Failed services:${NC}"
    for service in "${failed_services[@]}"; do
        echo -e "  - ${service}"
    done
    exit 1
fi
