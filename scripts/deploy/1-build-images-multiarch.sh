#!/bin/bash
# =============================================================================
# ADDP Multi-Architecture Image Builder (Offline Mode)
# =============================================================================
# Description: Build multi-arch images without external network dependency
# Strategy: Build each architecture separately, then create manifest list
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default values
REGISTRY="${REGISTRY:-localhost:5001}"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ARCHITECTURES=("amd64" "arm64")
SKIP_CACHE=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --skip-cache)
            SKIP_CACHE=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Multi-Arch Image Builder (Offline)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
echo -e "Architectures: ${GREEN}amd64, arm64${NC}"
echo ""

# Service list: name:directory
SERVICES=(
    "system-backend:system/backend"
    "manager-backend:manager/backend"
    "meta-backend:meta/backend"
    "transfer-backend:transfer/backend"
    "transfer-worker:transfer/backend"
    "gateway:gateway"
    "portal:portal/frontend"
    "system-frontend:system/frontend"
    "manager-frontend:manager/frontend"
    "meta-frontend:meta/frontend"
    "transfer-frontend:transfer/frontend"
    "nginx:nginx"
)

# Function to build image for a specific architecture
build_arch_image() {
    local service=$1
    local service_dir=$2
    local arch=$3
    local image_name="${REGISTRY}/addp-${service}:latest-${arch}"

    echo -e "${YELLOW}Building ${service} for ${arch}...${NC}"

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

    # Build command - use native docker build with --platform
    # Note: Docker automatically uses local layer cache, no need for --cache-from
    local build_cmd="docker build \
        --platform linux/${arch} \
        --tag ${image_name} \
        -f ${dockerfile_path} \
        ${build_context}"

    echo -e "${YELLOW}Executing: ${build_cmd}${NC}"

    if eval "$build_cmd" >/dev/null 2>&1; then
        # Push to registry
        echo -e "${YELLOW}Pushing ${image_name}...${NC}"
        if docker push "${image_name}" >/dev/null 2>&1; then
            echo -e "${GREEN}✓ Built and pushed ${service} (${arch})${NC}"
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
    # This works with already-pushed images without needing them in local cache
    if docker buildx imagetools create \
        --tag "${image_base}" \
        "${image_base}-amd64" \
        "${image_base}-arm64"; then

        echo -e "${GREEN}✓ Manifest created for ${service}${NC}"
        return 0
    else
        echo -e "${RED}✗ Failed to create manifest for ${service}${NC}"
        return 1
    fi
}

# Main build loop
failed_services=()
total_services=${#SERVICES[@]}
current=0

for svc in "${SERVICES[@]}"; do
    current=$((current + 1))
    service_name="${svc%%:*}"
    service_dir="${svc#*:}"

    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}[${current}/${total_services}] Building: ${service_name}${NC}"
    echo -e "${BLUE}========================================${NC}"

    # Build for each architecture
    build_success=true
    for arch in "${ARCHITECTURES[@]}"; do
        if ! build_arch_image "$service_name" "$service_dir" "$arch"; then
            build_success=false
            failed_services+=("${service_name} (${arch})")
        fi
    done

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

if [ ${#failed_services[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All services built successfully for both architectures${NC}"
    echo ""
    echo -e "${YELLOW}Verify multi-arch images:${NC}"
    echo -e "  docker buildx imagetools inspect ${REGISTRY}/addp-system-backend:latest"
    echo ""
    exit 0
else
    echo -e "${RED}✗ Failed services:${NC}"
    for service in "${failed_services[@]}"; do
        echo -e "  - ${service}"
    done
    exit 1
fi
