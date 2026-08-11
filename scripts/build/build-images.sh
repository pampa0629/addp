#!/bin/bash
# =============================================================================
# ADDP Image Builder
# =============================================================================
# Description: Build Docker images for native or multi-architecture deployment
# Usage: ./build-images.sh [OPTIONS]
# Options:
#   --registry REGISTRY_URL   Registry URL (default: localhost:5001)
#   --tag VERSION             Image tag (default: latest)
#   --skip-cache              Force rebuild without cache
#   --services SERVICE_LIST   Build specific services (comma-separated)
#   --multi-arch              Build for both ARM64 and AMD64 (default: native only)
#   --force                   Force rebuild all images (skip smart cache check)
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
IMAGE_TAG="${IMAGE_TAG:-latest}"
USE_CACHE="--cache-from"
SERVICES_TO_BUILD="all"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUILD_PLATFORMS="linux/$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"  # Native platform by default
MULTI_ARCH=false
FORCE_BUILD=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --tag)
            IMAGE_TAG="$2"
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
        --force)
            FORCE_BUILD=true
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
echo -e "Tag: ${GREEN}${IMAGE_TAG}${NC}"
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

    if ! curl -sf --max-time 5 "http://${REGISTRY}/v2/" > /dev/null 2>&1; then
        echo -e "${RED}Error: Registry ${REGISTRY} is not accessible${NC}"
        echo ""
        echo -e "${YELLOW}Troubleshooting steps:${NC}"
        echo "  ${BLUE}1.${NC} Check if registry container is running:"
        echo "     docker ps | grep registry"
        echo ""
        echo "  ${BLUE}2.${NC} If not running, start registry container:"
        echo "     docker run -d -p 5001:5000 --restart=always --name registry registry:2"
        echo ""
        echo "  ${BLUE}3.${NC} If running but not accessible, restart it:"
        echo "     docker rm -f registry"
        echo "     docker run -d -p 5001:5000 --restart=always --name registry registry:2"
        echo ""
        echo "  ${BLUE}4.${NC} Verify registry health:"
        echo "     curl http://localhost:5001/v2/"
        echo "     # Should return: {}"
        echo ""
        echo -e "${YELLOW}Alternatively, use Makefile command:${NC}"
        echo "     make registry-start"
        echo ""
        exit 1
    fi

    echo -e "${GREEN}✓ Registry is accessible${NC}"
}

# Return the newest modification time in the common-python package payload.
common_python_latest_time() {
    {
        printf '%s\n' common-python/README.md common-python/pyproject.toml
        find common-python/addp_common -type f \
            -not -path "*/__pycache__/*" \
            -not -name "*.pyc"
    } | xargs stat -f "%m" 2>/dev/null | sort -rn | head -1
}

# PointCloud packages only the runtime subset listed here in its Dockerfile.
pointcloud_common_python_latest_time() {
    {
        printf '%s\n' \
            common-python/README.md \
            common-python/pyproject.toml \
            common-python/addp_common/__init__.py \
            common-python/addp_common/workflow_access.py
        find common-python/addp_common/client common-python/addp_common/workflow_runtime \
            -type f ! -path '*/__pycache__/*' ! -name '*.pyc'
    } | xargs stat -f "%m" 2>/dev/null | sort -rn | head -1
}

# Function to check if service needs rebuild
check_service_changed() {
    local service=$1
    local service_dir=$2
    local image_name="${REGISTRY}/addp-${service}:${IMAGE_TAG}"
    local cache_dir=".build-cache"
    local cache_file="${cache_dir}/${service}-${IMAGE_TAG}.timestamp"

    # Check if image exists in registry using tags API
    local tags_response=$(curl -s "http://${REGISTRY}/v2/addp-${service}/tags/list")

    if ! echo "$tags_response" | grep -q "\"${IMAGE_TAG}\""; then
        echo -e "${YELLOW}Image doesn't exist in registry, building...${NC}"
        return 1  # Need to build
    fi

    # Create cache directory if it doesn't exist
    mkdir -p "$cache_dir"

    # Get last build time from cache file (0 if doesn't exist)
    local last_build_time=0
    if [ -f "$cache_file" ]; then
        last_build_time=$(cat "$cache_file" 2>/dev/null || echo "0")
    fi

    # Determine what to compare based on service type
    local comparison_time=0

    case "$service" in
        copilot-backend|agent-backend)
            # Python 后端依赖 common-python，两者都需要检查变更
            local python_backend_time
            python_backend_time=$(find "$service_dir" -type f '(' -name "*.py" -o -name "requirements.txt" -o -name "Dockerfile" -o -name "SKILL.md" ')' \
                -not -path "*/venv/*" -not -path "*/__pycache__/*" 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
            local common_time
            common_time=$(common_python_latest_time)
            comparison_time=$(( python_backend_time > common_time ? python_backend_time : common_time ))

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        model3d-workflow-engine)
            # Model3D runtime packages Python source plus converter Dockerfiles/patches/scripts.
            local model3d_time
            model3d_time=$(find "$service_dir" -type f '(' -name "*.py" -o -name "requirements.txt" -o -name "Dockerfile" -o -name "*.patch" -o -name "*.sh" ')' \
                -not -path "*/venv/*" -not -path "*/__pycache__/*" 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
            local common_time
            common_time=$(common_python_latest_time)
            comparison_time=$(( model3d_time > common_time ? model3d_time : common_time ))

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        pointcloud-workflow-engine)
            local pointcloud_time
            pointcloud_time=$(find "$service_dir" -type f '(' -name "*.py" -o -name "requirements.txt" -o -name "Dockerfile" ')' \
                -not -path "*/venv/*" -not -path "*/__pycache__/*" 2>/dev/null | xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
            local common_time
            common_time=$(pointcloud_common_python_latest_time)
            comparison_time=$(( pointcloud_time > common_time ? pointcloud_time : common_time ))
            ;;

        python-workflow-engine|jupyter-engine)
            # These Dockerfiles package common-python; compare both source trees.
            comparison_time=$(find "$service_dir" -type f '(' -name "*.py" -o -name "requirements.txt" -o -name "Dockerfile" ')' \
                -not -path "*/venv/*" -not -path "*/__pycache__/*" 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
            local common_time
            common_time=$(common_python_latest_time)
            comparison_time=$(( comparison_time > common_time ? comparison_time : common_time ))

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        spark-workflow-engine|raster-mosaic-runtime)
            # These Dockerfiles do not package common-python.
            comparison_time=$(find "$service_dir" -type f '(' -name "*.py" -o -name "requirements.txt" -o -name "Dockerfile" ')' \
                -not -path "*/venv/*" -not -path "*/__pycache__/*" 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        duckdb-engine)
            local duckdb_time
            duckdb_time=$(find "$service_dir" common -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" -o -name "Dockerfile" \) 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
            comparison_time="${duckdb_time:-0}"
            ;;

        supermap-workflow-engine)
            comparison_time=$(find "$service_dir" -type f '(' -name "*.java" -o -name "Dockerfile" -o -name "run.sh" ')' \
                -not -path "*/target/*" 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        *-backend|gateway|*-worker)
            # Backend/Worker services: compare binary file time
            local arch=$(echo "$BUILD_PLATFORMS" | sed 's|linux/||' | cut -d',' -f1)
            local binary_name

            if [[ "$service" == *-worker ]]; then
                binary_name="${service%-worker}-worker"  # transfer-worker → transfer-worker
            elif [ "$service" = "gateway" ]; then
                binary_name="gateway"
            else
                binary_name="${service%-backend}"  # system-backend → system
            fi

            local binary_path="dist/${BUILD_TYPE:-release}-linux-${arch}/${binary_name}"

            if [ ! -f "$binary_path" ]; then
                echo -e "${YELLOW}Binary not found at ${binary_path}, rebuilding...${NC}"
                return 1
            fi

            comparison_time=$(stat -f "%m" "$binary_path" 2>/dev/null || echo "0")

            # Manager backend image also packages declarative preview/content plugins.
            # Rebuild the image when plugin JSON/Dockerfile changes even if the Go binary is cached.
            if [ "$service" = "manager-backend" ]; then
                local manager_packaged_time
                manager_packaged_time=$(find "$service_dir" -type f \( -name "Dockerfile" -o -name "Dockerfile.prebuilt" -o -path "*/plugins/*" \) 2>/dev/null | \
                    xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)
                if [ -n "$manager_packaged_time" ] && [ "$manager_packaged_time" -gt "$comparison_time" ]; then
                    comparison_time="$manager_packaged_time"
                fi
            fi
            ;;

        nginx)
            # Nginx: compare config and Dockerfile
            comparison_time=$(find "$service_dir" -type f \( -name "*.conf" -o -name "Dockerfile" \) 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        *-frontend|console)
            # Frontend services: compare source file time (Vue/JS/TS/HTML + package.json)
            comparison_time=$(find "$service_dir" -type f \( -name "*.vue" -o -name "*.js" -o -name "*.ts" -o -name "*.html" -o -name "package.json" -o -name "Dockerfile" \) \
                -not -path "*/node_modules/*" -not -path "*/dist/*" 2>/dev/null | \
                xargs stat -f "%m" 2>/dev/null | sort -rn | head -1)

            if [ -z "$comparison_time" ] || [ "$comparison_time" = "0" ]; then
                echo -e "${YELLOW}Cannot determine source modification time, rebuilding...${NC}"
                return 1
            fi
            ;;

        *)
            echo -e "${YELLOW}Unknown service type, rebuilding...${NC}"
            return 1
            ;;
    esac

    # Compare with last build time
    if [ "$comparison_time" -gt "$last_build_time" ]; then
        echo -e "${YELLOW}Changes detected (source newer than last build), rebuilding...${NC}"
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
    local image_name="${REGISTRY}/addp-${service}:${IMAGE_TAG}"

    # Check if service directory exists
    if [ ! -d "$service_dir" ]; then
        echo -e "${YELLOW}Warning: Service directory $service_dir not found, skipping${NC}"
        return 0  # Not an error, just skip
    fi

    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Building: ${service}${NC}"
    echo -e "${BLUE}========================================${NC}"

    echo -e "${YELLOW}Building image for ${service}...${NC}"

    if [ "$service" = "model3d-workflow-engine" ]; then
        if [[ "$BUILD_PLATFORMS" == *,* ]]; then
            echo -e "${RED}Error: model3d-workflow-engine currently supports one Linux platform per build${NC}"
            echo -e "${YELLOW}Hint: build linux/arm64 on Apple Silicon with the default native build path${NC}"
            return 1
        fi

        local converter_image="${REGISTRY}/addp-model3d-converter:${IMAGE_TAG}"
        if MODEL3D_DOCKER_PLATFORM="${BUILD_PLATFORMS}" \
            MODEL3D_CONVERTER_IMAGE="${converter_image}" \
            MODEL3D_RUNTIME_IMAGE="${image_name}" \
            "${service_dir}/scripts/build-linux-arm64-images.sh"; then
            if [ "$MULTI_ARCH" = false ]; then
                echo -e "${YELLOW}Pushing ${converter_image} to registry...${NC}"
                docker push "${converter_image}"
                echo -e "${YELLOW}Pushing ${image_name} to registry...${NC}"
                docker push "${image_name}"
            fi
            mkdir -p ".build-cache"
            date +%s > ".build-cache/${service}-${IMAGE_TAG}.timestamp"
            echo -e "${GREEN}✓ Successfully built and pushed ${service}${NC}"
            return 0
        fi

        echo -e "${RED}✗ Failed to build ${service}${NC}"
        return 1
    fi

    if [ "$service" = "supermap-workflow-engine" ]; then
        if [[ "$BUILD_PLATFORMS" == *,* ]]; then
            echo -e "${RED}Error: supermap-workflow-engine currently supports one Linux platform per build${NC}"
            echo -e "${YELLOW}Hint: build linux/arm64 with SuperMap Linux arm64 SDK${NC}"
            return 1
        fi
        if [[ "$BUILD_PLATFORMS" != "linux/arm64" ]]; then
            echo -e "${RED}Error: supermap-workflow-engine requires linux/arm64, got ${BUILD_PLATFORMS}${NC}"
            return 1
        fi

        local supermap_base_image="${SUPERMAP_WORKFLOW_BASE_IMAGE:-addp-supermap-workflow-base:local}"
        if [ "$(docker image inspect -f '{{ index .Config.Labels "addp.supermap.base" }}' "${supermap_base_image}" 2>/dev/null || true)" != "true" ]; then
            echo -e "${RED}Error: missing SuperMap Workflow base image ${supermap_base_image}${NC}"
            echo -e "${YELLOW}Run: bash scripts/build/build-supermap-workflow-base.sh${NC}"
            return 1
        fi
        if docker build \
            --no-cache \
            --platform "${BUILD_PLATFORMS}" \
            --build-arg "BASE_IMAGE=${supermap_base_image}" \
            -f "${PROJECT_ROOT}/engines/supermap-workflow/Dockerfile" \
            -t "${image_name}" \
            "${PROJECT_ROOT}/engines/supermap-workflow" \
            && docker push "${image_name}"; then
            mkdir -p ".build-cache"
            date +%s > ".build-cache/${service}-${IMAGE_TAG}.timestamp"
            echo -e "${GREEN}✓ Successfully built and pushed ${service}${NC}"
            return 0
        fi

        echo -e "${RED}✗ Failed to build ${service}${NC}"
        return 1
    fi

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

            # Check if worker binary exists in dist/ directory
            local arch=$(echo "$BUILD_PLATFORMS" | sed 's|linux/||' | cut -d',' -f1)
            local binary_name="${service%-worker}-worker"  # e.g., transfer-worker
            local binary_path="dist/${BUILD_TYPE:-release}-linux-${arch}/${binary_name}"

            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Worker binary not found at ${binary_path}${NC}"
                echo -e "${YELLOW}Hint: Run ./scripts/build/compile.sh --arch ${arch} first${NC}"
                return 1
            fi
            ;;

        duckdb-engine)
            build_context="."
            dockerfile_path="${service_dir}/Dockerfile"
            ;;

        copilot-backend|agent-backend)
            # Python 后端需要访问项目根目录的 common-python，使用根目录作为构建上下文
            build_context="."
            dockerfile_path="${service_dir}/Dockerfile"

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;

        python-workflow-engine|raster-mosaic-runtime)
            # GeoPython Workflow 依赖 common-python，共享 schema/client 需要仓库根作为构建上下文
            build_context="."
            dockerfile_path="${service_dir}/Dockerfile"

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;

        pointcloud-workflow-engine)
            # PointCloud Workflow 依赖 common-python，使用仓库根作为构建上下文。
            build_context="."
            dockerfile_path="${service_dir}/Dockerfile"
            ;;

        spark-workflow-engine)
            # Python Engine: Python service built from source
            build_context="${service_dir}"
            dockerfile_path="${service_dir}/Dockerfile"

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;

        jupyter-engine)
            # Jupyter Dockerfile references common-python and engines/jupyter from the repository root.
            build_context="."
            dockerfile_path="${service_dir}/Dockerfile"

            if [ ! -f "${service_dir}/Dockerfile" ]; then
                echo -e "${RED}Error: Dockerfile not found in ${service_dir}${NC}"
                return 1
            fi
            ;;

        *-backend|gateway)
            # Backends: use prebuilt binary Dockerfile (requires compile.sh first)
            dockerfile_path="${service_dir}/Dockerfile.prebuilt"
            build_context="."

            # Check if prebuilt Dockerfile exists
            if [ ! -f "$dockerfile_path" ]; then
                echo -e "${RED}Error: $dockerfile_path not found${NC}"
                echo -e "${YELLOW}Hint: Dockerfile.prebuilt should be created for prebuilt binaries${NC}"
                return 1
            fi

            # Check if binary exists in dist/ directory
            local arch=$(echo "$BUILD_PLATFORMS" | sed 's|linux/||' | cut -d',' -f1)
            local binary_name="${service%-backend}"  # system-backend → system
            if [ "$service" = "gateway" ]; then
                binary_name="gateway"
            fi
            local binary_path="dist/${BUILD_TYPE:-release}-linux-${arch}/${binary_name}"

            if [ ! -f "$binary_path" ]; then
                echo -e "${RED}Error: Binary not found at ${binary_path}${NC}"
                echo -e "${YELLOW}Hint: Run ./scripts/build/compile.sh --arch ${arch} first${NC}"
                return 1
            fi
            ;;

        nginx)
            # Nginx Dockerfile copies files relative to nginx/ directory.
            build_context="nginx"
            dockerfile_path="nginx/Dockerfile"

            if [ ! -f "$dockerfile_path" ]; then
                echo -e "${RED}Error: Dockerfile not found in nginx${NC}"
                return 1
            fi
            ;;

        *-frontend|console)
            # All frontends use root context to access common-frontend
            build_context="."
            dockerfile_path="${service_dir}/Dockerfile"

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

    # Extract architecture for BUILD_ARG (first platform if multiple)
    local arch=$(echo "$BUILD_PLATFORMS" | sed 's|linux/||' | cut -d',' -f1)

    if [ "$MULTI_ARCH" = true ]; then
        # Multi-arch build with buildx (requires push to registry)
        build_cmd="docker buildx build \
            --build-arg BUILD_ARCH=${arch} \
            --build-arg GOOS=linux \
            --build-arg BUILD_TYPE=${BUILD_TYPE:-release} \
            --platform ${BUILD_PLATFORMS} \
            --tag ${image_name} \
            --push \
            -f ${dockerfile_path}"

        if [ -n "$USE_CACHE" ]; then
            if docker manifest inspect "${image_name}" > /dev/null 2>&1; then
                build_cmd="$build_cmd --cache-from type=registry,ref=${image_name}"
            else
                echo -e "${YELLOW}Cache image not found for ${service}, building without cache-from${NC}"
            fi
        fi

        build_cmd="$build_cmd ${build_context}"
    else
        # Native platform build with regular docker (load to local)
        build_cmd="docker build \
            --build-arg BUILD_ARCH=${arch} \
            --build-arg GOOS=linux \
            --build-arg BUILD_TYPE=${BUILD_TYPE:-release} \
            --tag ${image_name} \
            --platform ${BUILD_PLATFORMS} \
            -f ${dockerfile_path}"

        if [ -n "$USE_CACHE" ]; then
            if docker image inspect "${image_name}" > /dev/null 2>&1; then
                build_cmd="$build_cmd --cache-from ${image_name}"
            else
                echo -e "${YELLOW}Cache image not found for ${service}, building without cache-from${NC}"
            fi
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
                # Update build cache timestamp
                mkdir -p ".build-cache"
                date +%s > ".build-cache/${service}-${IMAGE_TAG}.timestamp"
                return 0
            else
                echo -e "${RED}✗ Failed to push ${service}${NC}"
                return 1
            fi
        else
            echo -e "${GREEN}✓ Successfully built and pushed ${service}${NC}"
            # Update build cache timestamp
            mkdir -p ".build-cache"
            date +%s > ".build-cache/${service}-${IMAGE_TAG}.timestamp"
            return 0
        fi
    else
        echo -e "${RED}✗ Failed to build ${service}${NC}"
        return 1
    fi
}

# Pull base images and push to local registry so Dockerfiles can use localhost:5001/ prefix
seed_base_images() {
    echo -e "${YELLOW}Seeding base images to local registry...${NC}"

    local arch
    arch=$(echo "$BUILD_PLATFORMS" | sed 's|linux/||' | cut -d',' -f1)

    local base_images=(
        "alpine:latest"
        "nginx:alpine"
        "node:18.20.5-alpine"
        "node:20-alpine"
        "python:3.11-slim"
        "python:3.12-slim"
        "python:3.11-bullseye"
        "golang:1.24=golang:1.24"
        "golang:1.24-bookworm=golang:1.24-bookworm"
        "debian:bookworm-slim=debian-slim:latest"
        "debian:bookworm-slim=debian-bookworm-slim:latest"
        "ubuntu:24.04=ubuntu24:latest"
    )

    local any_failed=false
    for img in "${base_images[@]}"; do
        local source_img="${img%%=*}"
        local target_img="${img#*=}"
        if [ "$source_img" = "$target_img" ]; then
            target_img="$source_img"
        fi

        local img_name="${target_img%%:*}"
        local img_tag="${target_img#*:}"
        local local_img="${REGISTRY}/${target_img}"

        local tags_response
        tags_response=$(curl -s "http://${REGISTRY}/v2/${img_name}/tags/list" 2>/dev/null)
        if echo "$tags_response" | grep -q "\"${img_tag}\""; then
            echo -e "${GREEN}✓ ${target_img} already in registry${NC}"
            continue
        fi

        echo -e "${YELLOW}Pulling ${source_img} (linux/${arch})...${NC}"
        if docker pull --platform "linux/${arch}" "${source_img}" \
            && docker tag "${source_img}" "${local_img}" \
            && docker push "${local_img}"; then
            echo -e "${GREEN}✓ Seeded ${target_img}${NC}"
        else
            echo -e "${RED}✗ Failed to seed ${target_img} — builds using this image will fail${NC}"
            any_failed=true
        fi
    done

    if [ "$any_failed" = true ]; then
        echo -e "${RED}Some base images could not be seeded. Check network/mirror config.${NC}"
        return 1
    fi
    echo -e "${GREEN}✓ All base images ready${NC}"
}

# Main build process
main() {
    check_docker_config
    check_buildx
    check_registry
    seed_base_images

    # Define services to build
    local services=(
        "system-backend:system/backend"
        "manager-backend:manager/backend"
        "meta-backend:meta/backend"
        "transfer-backend:transfer/backend"
        "orchestrator-backend:orchestrator/backend"
        "develop-backend:develop/backend"
        "service-backend:service/backend"
        "monitor-backend:monitor/backend"
        "standard-backend:standard/backend"
        "copilot-backend:copilot"
        "agent-backend:agent/backend"
        "model-backend:model/backend"
        "quality-backend:quality/backend"
        "asset-backend:asset/backend"
        "portal-backend:portal/backend"
        "graph-backend:graph/backend"
        "inference-backend:inference/backend"
        "python-workflow-engine:engines/python-workflow"
        "raster-mosaic-runtime:manager/raster-mosaic-runtime"
        "model3d-workflow-engine:engines/model3d-workflow"
        "pointcloud-workflow-engine:engines/pointcloud-workflow"
        "supermap-workflow-engine:engines/supermap-workflow"
        "spark-workflow-engine:engines/spark-workflow"
        "jupyter-engine:engines/jupyter"
        "duckdb-engine:engines/duckdb"
        "transfer-worker:transfer/backend"
        "meta-worker:meta/backend"
        "gateway:gateway"
        "console:console/frontend"
        "system-frontend:system/frontend"
        "manager-frontend:manager/frontend"
        "meta-frontend:meta/frontend"
        "transfer-frontend:transfer/frontend"
        "orchestrator-frontend:orchestrator/frontend"
        "develop-frontend:develop/frontend"
        "service-frontend:service/frontend"
        "monitor-frontend:monitor/frontend"
        "standard-frontend:standard/frontend"
        "agent-frontend:agent/frontend"
        "model-frontend:model/frontend"
        "quality-frontend:quality/frontend"
        "asset-frontend:asset/frontend"
        "portal-frontend:portal/frontend"
        "graph-frontend:graph/frontend"
        "inference-frontend:inference/frontend"
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
    local skipped_services=()

    # Build each service
    for service_def in "${services[@]}"; do
        IFS=':' read -r service_name service_dir <<< "$service_def"

        # Check if service needs rebuild (unless --force is specified)
        if [ "$FORCE_BUILD" = false ] && check_service_changed "$service_name" "$service_dir"; then
            echo -e "${GREEN}✓ Skipping ${service_name} (unchanged)${NC}"
            skipped_services+=("$service_name")
            continue
        fi

        if ! build_service "$service_name" "$service_dir"; then
            failed_services+=("$service_name")
        fi
    done

    # Summary
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Build Summary${NC}"
    echo -e "${BLUE}========================================${NC}"

    # Show skipped services if any
    if [ ${#skipped_services[@]} -gt 0 ]; then
        echo -e "${GREEN}✓ Skipped ${#skipped_services[@]} unchanged service(s)${NC}"
    fi

    if [ ${#failed_services[@]} -eq 0 ]; then
        local built_count=$((${#services[@]} - ${#skipped_services[@]}))
        echo -e "${GREEN}✓ Successfully built ${built_count} service(s)${NC}"
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
