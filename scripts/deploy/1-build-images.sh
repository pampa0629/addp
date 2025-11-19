#!/bin/bash
# =============================================================================
# ADDP Image Builder
# =============================================================================
# Description: Build Docker images for native or multi-architecture deployment
# Usage: ./1-build-images.sh [OPTIONS]
# Options:
#   --registry REGISTRY_URL   Registry URL (default: localhost:5001)
#   --skip-cache              Force rebuild without cache
#   --services SERVICE_LIST   Build specific services (comma-separated)
#   --multi-arch              Build for both ARM64 and AMD64 (default: native only)
#
# Default behavior: Builds for native platform only (faster, no Docker Hub needed)
# Use --multi-arch for cross-platform deployment (requires Docker Hub access)
# =============================================================================

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
REGISTRY="${REGISTRY:-localhost:5001}"
USE_CACHE="--cache-from"
SERVICES_TO_BUILD="all"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUILD_PLATFORMS="linux/$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"  # Native platform by default
MULTI_ARCH=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --skip-cache)
            USE_CACHE=""
            shift
            ;;
        --services)
            SERVICES_TO_BUILD="$2"
            shift 2
            ;;
        --multi-arch)
            MULTI_ARCH=true
            BUILD_PLATFORMS="linux/amd64,linux/arm64"
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
echo -e "${BLUE}ADDP Image Builder${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
echo -e "Platforms: ${GREEN}${BUILD_PLATFORMS}${NC}"
echo -e "Cache: ${GREEN}$([ -z "$USE_CACHE" ] && echo "disabled" || echo "enabled")${NC}"
echo -e "Services: ${GREEN}${SERVICES_TO_BUILD}${NC}"
echo ""

# Function to configure Docker registry mirrors automatically
configure_docker_mirrors() {
    local OS="$(uname -s)"
    local PLATFORM=""

    case "${OS}" in
        Linux*)     PLATFORM="Linux";;
        Darwin*)    PLATFORM="Mac";;
        *)          return 1;;
    esac

    echo -e "${YELLOW}Configuring Docker registry mirrors...${NC}"

    local DAEMON_JSON='{
  "registry-mirrors": [
    "https://docker.mirrors.sjtug.sjtu.edu.cn"
  ],
  "insecure-registries": ["localhost:5001"],
  "max-concurrent-downloads": 10,
  "max-concurrent-uploads": 5
}'

    if [ "$PLATFORM" = "Mac" ]; then
        local DOCKER_CONFIG="$HOME/.docker/daemon.json"

        # Backup existing config
        if [ -f "$DOCKER_CONFIG" ]; then
            cp "$DOCKER_CONFIG" "${DOCKER_CONFIG}.backup.$(date +%Y%m%d_%H%M%S)"
        fi

        mkdir -p "$HOME/.docker"
        echo "$DAEMON_JSON" > "$DOCKER_CONFIG"

        echo -e "${GREEN}✓ Configuration written${NC}"
        echo -e "${YELLOW}Restarting Docker Desktop...${NC}"

        osascript -e 'quit app "Docker"' 2>/dev/null || true
        sleep 3
        open -a Docker

        echo -e "${YELLOW}Waiting for Docker to restart (30 seconds)...${NC}"
        sleep 30

        # Wait for Docker to be ready
        local timeout=60
        local elapsed=0
        while ! docker info &> /dev/null; do
            if [ $elapsed -ge $timeout ]; then
                echo -e "${RED}Docker failed to start within ${timeout}s${NC}"
                return 1
            fi
            sleep 2
            elapsed=$((elapsed + 2))
        done

        echo -e "${GREEN}✓ Docker Desktop restarted${NC}"
        return 0

    elif [ "$PLATFORM" = "Linux" ]; then
        local DOCKER_CONFIG="/etc/docker/daemon.json"

        if [ "$EUID" -ne 0 ]; then
            echo -e "${RED}Error: Root privileges required on Linux${NC}"
            echo -e "${YELLOW}Please run: sudo $0${NC}"
            return 1
        fi

        # Backup existing config
        if [ -f "$DOCKER_CONFIG" ]; then
            cp "$DOCKER_CONFIG" "${DOCKER_CONFIG}.backup.$(date +%Y%m%d_%H%M%S)"
        fi

        mkdir -p /etc/docker
        echo "$DAEMON_JSON" > "$DOCKER_CONFIG"

        systemctl daemon-reload
        systemctl restart docker
        sleep 5

        if systemctl is-active --quiet docker; then
            echo -e "${GREEN}✓ Docker daemon restarted${NC}"
            return 0
        else
            echo -e "${RED}Failed to restart Docker${NC}"
            return 1
        fi
    fi

    return 1
}

# Function to check Docker daemon configuration
check_docker_config() {
    echo -e "${YELLOW}Checking Docker configuration...${NC}"

    # Check if daemon.json exists
    if [ -f "$HOME/.docker/daemon.json" ] || [ -f "/etc/docker/daemon.json" ]; then
        echo -e "${GREEN}✓ Docker daemon.json found${NC}"
        return 0
    fi

    echo -e "${YELLOW}Warning: No Docker daemon.json found${NC}"
    echo -e "${YELLOW}Docker Hub pulls may timeout without registry mirrors${NC}"
    echo ""
    echo -e "${BLUE}Options:${NC}"
    echo -e "  ${GREEN}1${NC} - Auto-configure mirrors (recommended)"
    echo -e "  ${GREEN}2${NC} - Continue without mirrors"
    echo -e "  ${GREEN}3${NC} - Exit and configure manually"
    echo ""
    read -p "Choose option (1-3): " -n 1 -r
    echo

    case $REPLY in
        1)
            if configure_docker_mirrors; then
                echo -e "${GREEN}✓ Mirrors configured successfully${NC}"
                return 0
            else
                echo -e "${RED}Failed to configure mirrors${NC}"
                echo -e "${YELLOW}Continuing anyway...${NC}"
                return 0
            fi
            ;;
        2)
            echo -e "${YELLOW}Continuing without mirrors...${NC}"
            return 0
            ;;
        3)
            echo ""
            echo -e "${BLUE}Manual configuration:${NC}"
            echo ""
            echo -e "${YELLOW}For macOS:${NC}"
            echo -e "  mkdir -p ~/.docker"
            echo -e "  cat > ~/.docker/daemon.json <<'EOF'"
            echo -e '  {'
            echo -e '    "registry-mirrors": ['
            echo -e '      "https://docker.mirrors.sjtug.sjtu.edu.cn",'
            echo -e '      "https://docker.nju.edu.cn"'
            echo -e '    ],'
            echo -e '    "insecure-registries": ["localhost:5001"]'
            echo -e '  }'
            echo -e '  EOF'
            echo -e "  # Then restart Docker Desktop"
            echo ""
            echo -e "${YELLOW}For Linux:${NC}"
            echo -e "  sudo mkdir -p /etc/docker"
            echo -e "  sudo tee /etc/docker/daemon.json > /dev/null <<'EOF'"
            echo -e '  {'
            echo -e '    "registry-mirrors": ['
            echo -e '      "https://docker.mirrors.sjtug.sjtu.edu.cn",'
            echo -e '      "https://docker.nju.edu.cn"'
            echo -e '    ],'
            echo -e '    "insecure-registries": ["localhost:5001"]'
            echo -e '  }'
            echo -e '  EOF'
            echo -e "  sudo systemctl daemon-reload"
            echo -e "  sudo systemctl restart docker"
            echo ""
            exit 1
            ;;
        *)
            echo -e "${RED}Invalid option${NC}"
            exit 1
            ;;
    esac
}

# Function to check if docker buildx is available
check_buildx() {
    echo -e "${YELLOW}Checking Docker Buildx...${NC}"

    if [ "$MULTI_ARCH" = false ]; then
        echo -e "${GREEN}✓ Using native platform build (no buildx needed)${NC}"
        return 0
    fi

    if ! docker buildx version &> /dev/null; then
        echo -e "${RED}Error: Docker Buildx is not available${NC}"
        echo "Please install Docker Desktop or enable buildx"
        exit 1
    fi

    # Remove existing builder if it exists
    if docker buildx inspect addp-builder &> /dev/null; then
        echo -e "${YELLOW}Removing existing builder instance...${NC}"
        docker buildx rm addp-builder || true
    fi

    echo -e "${YELLOW}Creating buildx builder instance for multi-arch...${NC}"

    # Create buildkitd config with Aliyun registry mirror (best for China)
    local buildkit_config="/tmp/buildkitd.toml"
    cat > "$buildkit_config" <<'EOF'
# BuildKit configuration with Aliyun registry mirror

[registry."docker.io"]
  mirrors = ["registry.cn-hangzhou.aliyuncs.com"]
EOF

    echo -e "${YELLOW}Using Aliyun registry mirror for faster image pulls...${NC}"

    docker buildx create \
        --name addp-builder \
        --driver docker-container \
        --driver-opt network=host \
        --config "$buildkit_config" \
        --use \
        --bootstrap 2>&1

    # Clean up config file
    rm -f "$buildkit_config"

    echo -e "${GREEN}✓ Docker Buildx ready for multi-arch builds${NC}"
    echo -e "${YELLOW}Note: Using Aliyun mirror for base images${NC}"
}

# Function to check if registry is accessible
check_registry() {
    echo -e "${YELLOW}Checking registry accessibility...${NC}"

    if ! curl -sf "http://${REGISTRY}/v2/" > /dev/null 2>&1; then
        echo -e "${RED}Error: Registry ${REGISTRY} is not accessible${NC}"
        echo "Please start the local registry:"
        echo "  docker run -d -p 5001:5000 --restart=always --name registry registry:2"
        exit 1
    fi

    echo -e "${GREEN}✓ Registry is accessible${NC}"
}

# Function to check if service code has changed
check_service_changed() {
    local service=$1
    local service_dir=$2
    local image_name="${REGISTRY}/addp-${service}:latest"

    # Check if image exists in registry using tags API
    local tags_response=$(curl -s "http://${REGISTRY}/v2/addp-${service}/tags/list")

    if ! echo "$tags_response" | grep -q '"latest"'; then
        echo -e "${YELLOW}Image doesn't exist in registry, building...${NC}"
        return 1  # Need to build
    fi

    # Image exists, now check if source code has changed
    # Get the most recent modification time of source files
    local latest_source_time=$(find "$service_dir" -type f -name "*.go" -o -name "*.mod" -o -name "*.sum" -o -name "Dockerfile" | xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)

    if [ -z "$latest_source_time" ]; then
        # Can't determine source modification time, rebuild to be safe
        echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
        return 1
    fi

    # Get image creation time from local Docker (if exists)
    local image_time=$(docker image inspect "${REGISTRY}/addp-${service}:latest" 2>/dev/null | jq -r '.[0].Created' | date -j -f "%Y-%m-%dT%H:%M:%S" +%s 2>/dev/null || echo "0")

    if [ "$latest_source_time" -gt "$image_time" ]; then
        echo -e "${YELLOW}Source code changed, rebuilding...${NC}"
        return 1  # Need to rebuild
    else
        echo -e "${GREEN}✓ Image exists and up-to-date, skipping build${NC}"
        return 0  # No need to rebuild
    fi
}

# Function to build a service image
build_service() {
    local service=$1
    local service_dir=$2
    local image_name="${REGISTRY}/addp-${service}:latest"

    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Building: ${service}${NC}"
    echo -e "${BLUE}========================================${NC}"

    echo -e "${YELLOW}Building image for ${service}...${NC}"

    # Determine build context and Dockerfile based on service type
    local build_context="."
    local dockerfile_path=""

    case "$service" in
        transfer-worker|meta-worker)
            # Worker services: special case with worker binary
            dockerfile_path="${service_dir}/Dockerfile.prebuilt.worker"
            build_context="."

            # Check if prebuilt Dockerfile exists
            if [ ! -f "$dockerfile_path" ]; then
                echo -e "${RED}Error: $dockerfile_path not found${NC}"
                echo -e "${YELLOW}Hint: Dockerfile.prebuilt.worker should be created for worker binary${NC}"
                return 1
            fi

            # Check if worker binary exists
            local binary_path="${service_dir}/worker"
            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Worker binary not found at ${binary_path}${NC}"
                echo -e "${YELLOW}Hint: Run ./scripts/deploy/0-compile-binaries.sh first${NC}"
                return 1
            fi
            ;;

        *-backend|gateway)
            # Backends: use prebuilt binary Dockerfile (requires Step 0 compilation)
            dockerfile_path="${service_dir}/Dockerfile.prebuilt"
            build_context="."

            # Check if prebuilt Dockerfile exists
            if [ ! -f "$dockerfile_path" ]; then
                echo -e "${RED}Error: $dockerfile_path not found${NC}"
                echo -e "${YELLOW}Hint: Dockerfile.prebuilt should be created for prebuilt binaries${NC}"
                return 1
            fi

            # Check if binary exists
            local binary_path="${service_dir}/server"
            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Binary not found at ${binary_path}${NC}"
                echo -e "${YELLOW}Hint: Run ./scripts/deploy/0-compile-binaries.sh first${NC}"
                return 1
            fi
            ;;

        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;
        *-frontend|portal|nginx)
            # Frontends: system-frontend and transfer-frontend need root context for common-frontend
            if [ "$service" = "system-frontend" ] || [ "$service" = "transfer-frontend" ]; then
                build_context="."
                dockerfile_path="${service_dir}/Dockerfile"
            else
                build_context="${service_dir}"
                dockerfile_path="${service_dir}/Dockerfile"
            fi

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;

        *)
            echo -e "${RED}Error: Unknown service type: ${service}${NC}"
            return 1
            ;;
    esac

    # Build command differs for native vs multi-arch
    local build_cmd
    if [ "$MULTI_ARCH" = true ]; then
        # Multi-arch build with buildx (requires push to registry)
        build_cmd="docker buildx build \
            --platform ${BUILD_PLATFORMS} \
            --tag ${image_name} \
            --push \
            -f ${dockerfile_path}"

        if [ -n "$USE_CACHE" ]; then
            build_cmd="$build_cmd --cache-from type=registry,ref=${image_name}"
        fi

        build_cmd="$build_cmd ${build_context}"
    else
        # Native platform build with regular docker (load to local)
        build_cmd="docker build \
            --tag ${image_name} \
            --platform ${BUILD_PLATFORMS} \
            -f ${dockerfile_path}"

        if [ -n "$USE_CACHE" ]; then
            build_cmd="$build_cmd --cache-from ${image_name}"
        fi

        build_cmd="$build_cmd ${build_context}"
    fi

    echo -e "${YELLOW}Executing: ${build_cmd}${NC}"

    if eval "$build_cmd"; then
        # Push to registry for native builds
        if [ "$MULTI_ARCH" = false ]; then
            echo -e "${YELLOW}Pushing ${image_name} to registry...${NC}"
            if docker push "${image_name}"; then
                echo -e "${GREEN}✓ Successfully built and pushed ${service}${NC}"
                return 0
            else
                echo -e "${RED}✗ Failed to push ${service}${NC}"
                return 1
            fi
        else
            echo -e "${GREEN}✓ Successfully built and pushed ${service}${NC}"
            return 0
        fi
    else
        echo -e "${RED}✗ Failed to build ${service}${NC}"
        return 1
    fi
}

# Main build process
main() {
    check_docker_config
    check_buildx
    check_registry

    # Define services to build
    local services=(
        "system-backend:system/backend"
        "manager-backend:manager/backend"
        "meta-backend:meta/backend"
        "transfer-backend:transfer/backend"
        "transfer-worker:transfer/backend"
        "meta-worker:meta/backend"
        "gateway:gateway"
        "portal:portal/frontend"
        "system-frontend:system/frontend"
        "manager-frontend:manager/frontend"
        "meta-frontend:meta/frontend"
        "transfer-frontend:transfer/frontend"
        "nginx:nginx"
    )

    # Filter services if specified
    if [ "$SERVICES_TO_BUILD" != "all" ]; then
        IFS=',' read -ra SELECTED_SERVICES <<< "$SERVICES_TO_BUILD"
        local filtered_services=()
        for service_def in "${services[@]}"; do
            local service_name="${service_def%%:*}"
            for selected in "${SELECTED_SERVICES[@]}"; do
                if [ "$service_name" == "$selected" ]; then
                    filtered_services+=("$service_def")
                    break
                fi
            done
        done
        services=("${filtered_services[@]}")
    fi

    echo -e "${YELLOW}Building ${#services[@]} services...${NC}"
    echo ""

    local failed_services=()

    # Build each service
    for service_def in "${services[@]}"; do
        IFS=':' read -r service_name service_dir <<< "$service_def"

        if ! build_service "$service_name" "$service_dir"; then
            failed_services+=("$service_name")
        fi
    done

    # Summary
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Build Summary${NC}"
    echo -e "${BLUE}========================================${NC}"

    if [ ${#failed_services[@]} -eq 0 ]; then
        echo -e "${GREEN}✓ All services built successfully${NC}"
        echo ""
        echo "Images pushed to: ${REGISTRY}"
        echo "Platforms: ${BUILD_PLATFORMS}"
        return 0
    else
        echo -e "${RED}✗ Failed services:${NC}"
        for failed in "${failed_services[@]}"; do
            echo -e "  - ${failed}"
        done
        return 1
    fi
}

# Run main function
main
