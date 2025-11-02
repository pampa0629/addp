#!/bin/bash
# =============================================================================
# ADDP One-Click Deployment Script
# =============================================================================
# Description: Orchestrate complete deployment from build to server startup
# Usage: ./deploy-all.sh [OPTIONS]
# Options:
#   --server SERVER_USER@IP   Target server for deployment
#   --registry REGISTRY_URL   Registry URL (default: localhost:5001)
#   --skip-build              Skip image building step
#   --skip-transfer           Skip file transfer step
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default values
SERVER=""
REGISTRY="localhost:5001"
SKIP_BUILD=false
SKIP_TRANSFER=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_ARCH="auto"  # auto, arm64, amd64, or both

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --server)
            SERVER="$2"
            shift 2
            ;;
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --arch)
            BUILD_ARCH="$2"
            shift 2
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --skip-transfer)
            SKIP_TRANSFER=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --server USER@HOST    Target server for deployment (required)"
            echo "  --registry URL        Registry URL (default: localhost:5001)"
            echo "  --arch ARCH           Build architecture: auto (default), arm64, amd64, or both"
            echo "  --skip-build          Skip image building"
            echo "  --skip-transfer       Skip file transfer"
            echo "  -h, --help            Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Validation and detect deployment mode
IS_LOCAL=false

if [ -z "$SERVER" ]; then
    echo -e "${RED}Error: --server option is required${NC}"
    echo "Usage: $0 --server user@host [OPTIONS]"
    echo "For local deployment: --server localhost"
    exit 1
fi

# Extract host from SERVER (handle user@host or just host)
if [[ "$SERVER" == *"@"* ]]; then
    HOST="${SERVER#*@}"
else
    HOST="$SERVER"
fi

# Check if target is localhost
if [[ "$HOST" == "localhost" ]] || [[ "$HOST" == "127.0.0.1" ]] || [[ "$HOST" == "::1" ]]; then
    IS_LOCAL=true
else
    # Check if host is a local IP by trying to ping it
    LOCAL_IPS=$(ifconfig 2>/dev/null | grep 'inet ' | awk '{print $2}' || ip addr 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)

    for local_ip in $LOCAL_IPS; do
        if [[ "$HOST" == "$local_ip" ]]; then
            IS_LOCAL=true
            break
        fi
    done
fi

# Auto-detect architecture if set to "auto"
if [ "$BUILD_ARCH" = "auto" ]; then
    DETECTED_ARCH="$(uname -m)"
    case "$DETECTED_ARCH" in
        x86_64)
            BUILD_ARCH="amd64"
            ;;
        aarch64|arm64)
            BUILD_ARCH="arm64"
            ;;
        *)
            echo -e "${YELLOW}Warning: Unknown architecture $DETECTED_ARCH, defaulting to amd64${NC}"
            BUILD_ARCH="amd64"
            ;;
    esac
    echo -e "${GREEN}✓ Auto-detected CPU architecture: ${BUILD_ARCH}${NC}"
    echo ""
fi

# Validate BUILD_ARCH
case "$BUILD_ARCH" in
    arm64|amd64|both)
        ;;
    *)
        echo -e "${RED}Error: Invalid architecture '${BUILD_ARCH}'${NC}"
        echo -e "${YELLOW}Valid options: auto, arm64, amd64, both${NC}"
        exit 1
        ;;
esac

# Auto-detect REGISTRY based on deployment mode
if [ "$REGISTRY" = "localhost:5001" ]; then
    # Default REGISTRY not changed by user, auto-detect
    if [ "$IS_LOCAL" = true ]; then
        # Local deployment - use localhost
        REGISTRY="localhost:5001"
    else
        # Remote deployment - use development machine's IP
        DETECT_IP_SCRIPT="$SCRIPT_DIR/detect-dev-ip.sh"

        if [ -f "$DETECT_IP_SCRIPT" ]; then
            # Use the dedicated IP detection script
            DEV_MACHINE_IP=$("$DETECT_IP_SCRIPT" --target-server "$HOST" 2>/dev/null)
            DETECT_EXIT_CODE=$?

            if [ $DETECT_EXIT_CODE -eq 0 ] && [ -n "$DEV_MACHINE_IP" ] && [ "$DEV_MACHINE_IP" != "localhost" ]; then
                REGISTRY="${DEV_MACHINE_IP}:5001"
                echo -e "${GREEN}✓ Auto-detected development machine IP: ${DEV_MACHINE_IP}${NC}"
                echo -e "${CYAN}  Using REGISTRY=${REGISTRY} for remote deployment${NC}"
                echo ""
            else
                echo -e "${RED}Error: Failed to detect development machine IP${NC}"
                echo -e "${YELLOW}Please specify registry manually: --registry <dev-machine-ip>:5001${NC}"
                echo -e "${YELLOW}Or ensure you're connected to a network${NC}"
                exit 1
            fi
        else
            # Fallback to inline detection if script not found
            echo -e "${YELLOW}Warning: IP detection script not found, using fallback method${NC}"
            DEV_MACHINE_IP=$(ifconfig 2>/dev/null | grep 'inet ' | grep -v 127.0.0.1 | awk '{print $2}' | grep -E '^(192\.168\.|10\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)' | head -1 || echo "localhost")

            if [ "$DEV_MACHINE_IP" != "localhost" ]; then
                REGISTRY="${DEV_MACHINE_IP}:5001"
                echo -e "${GREEN}✓ Detected development machine IP: ${DEV_MACHINE_IP}${NC}"
                echo -e "${CYAN}  Using REGISTRY=${REGISTRY}${NC}"
                echo ""
            else
                echo -e "${RED}Error: Could not detect development machine IP${NC}"
                echo -e "${YELLOW}Please specify registry manually: --registry <dev-machine-ip>:5001${NC}"
                exit 1
            fi
        fi
    fi
fi

DEPLOY_DIR="$HOME/addp"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP One-Click Deployment${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
if [ "$IS_LOCAL" = true ]; then
    echo -e "Mode: ${GREEN}Local Deployment${NC}"
    echo -e "Target Directory: ${GREEN}${DEPLOY_DIR}${NC}"
else
    echo -e "Mode: ${GREEN}Remote Deployment${NC}"
    echo -e "Target Server: ${GREEN}${SERVER}${NC}"
fi
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
echo -e "Architecture: ${GREEN}${BUILD_ARCH}${NC}"
echo ""

# =============================================================================
# Step -1: Setup Multi-Arch Infrastructure Images (One-time setup)
# =============================================================================

# Check if infrastructure images already exist in registry
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Checking Infrastructure Images${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Function to check if a multi-arch manifest exists
check_image_exists() {
    local image=$1
    if docker buildx imagetools inspect "$image" >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

INFRA_MISSING=false

echo -e "${YELLOW}Checking required infrastructure images in registry...${NC}"

if ! check_image_exists "${REGISTRY}/alpine:latest"; then
    echo -e "${RED}✗ ${REGISTRY}/alpine:latest not found${NC}"
    INFRA_MISSING=true
else
    echo -e "${GREEN}✓ ${REGISTRY}/alpine:latest${NC}"
fi



if ! check_image_exists "${REGISTRY}/addp-infra-redis:7-alpine"; then
    echo -e "${RED}✗ ${REGISTRY}/addp-infra-redis:7-alpine not found${NC}"
    INFRA_MISSING=true
else
    echo -e "${GREEN}✓ ${REGISTRY}/addp-infra-redis:7-alpine${NC}"
fi

if ! check_image_exists "${REGISTRY}/addp-infra-minio:latest"; then
    echo -e "${RED}✗ ${REGISTRY}/addp-infra-minio:latest not found${NC}"
    INFRA_MISSING=true
else
    echo -e "${GREEN}✓ ${REGISTRY}/addp-infra-minio:latest${NC}"
fi

if ! check_image_exists "${REGISTRY}/addp-infra-elasticsearch:8.11.0"; then
    echo -e "${RED}✗ ${REGISTRY}/addp-infra-elasticsearch:8.11.0 not found${NC}"
    INFRA_MISSING=true
else
    echo -e "${GREEN}✓ ${REGISTRY}/addp-infra-elasticsearch:8.11.0${NC}"
fi

echo ""

if [ "$INFRA_MISSING" = true ]; then
    echo -e "${YELLOW}========================================${NC}"
    echo -e "${YELLOW}Setting up Multi-Arch Infrastructure${NC}"
    echo -e "${YELLOW}========================================${NC}"
    echo ""
    echo -e "${YELLOW}This is a one-time setup that will:${NC}"
    echo -e "  1. Pull official images from Docker Hub (alpine, redis, minio, elasticsearch)"
    echo -e "  2. Tag and push to local registry (${REGISTRY})"
    echo -e "  3. Create multi-arch manifests (AMD64 + ARM64)"
    echo ""
    echo -e "${YELLOW}This may take a few minutes depending on your network speed...${NC}"
    echo ""

    # Pull and push Alpine
    echo -e "${BLUE}[1/4] Alpine (backend base image)${NC}"
    docker pull --platform linux/amd64 alpine:latest 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker pull --platform linux/arm64 alpine:latest 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker tag alpine:latest ${REGISTRY}/alpine:latest-amd64
    docker tag alpine:latest ${REGISTRY}/alpine:latest-arm64
    docker push ${REGISTRY}/alpine:latest-amd64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker push ${REGISTRY}/alpine:latest-arm64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker buildx imagetools create --tag ${REGISTRY}/alpine:latest \
        ${REGISTRY}/alpine:latest-amd64 ${REGISTRY}/alpine:latest-arm64
    echo -e "${GREEN}✓ Alpine ready${NC}"
    echo ""

    # Pull and push Redis
    echo -e "${BLUE}[2/4] Redis (cache & queue)${NC}"
    docker pull --platform linux/amd64 redis:7-alpine 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker pull --platform linux/arm64 redis:7-alpine 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker tag redis:7-alpine ${REGISTRY}/addp-infra-redis:7-alpine-amd64
    docker tag redis:7-alpine ${REGISTRY}/addp-infra-redis:7-alpine-arm64
    docker push ${REGISTRY}/addp-infra-redis:7-alpine-amd64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker push ${REGISTRY}/addp-infra-redis:7-alpine-arm64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker buildx imagetools create --tag ${REGISTRY}/addp-infra-redis:7-alpine \
        ${REGISTRY}/addp-infra-redis:7-alpine-amd64 ${REGISTRY}/addp-infra-redis:7-alpine-arm64
    echo -e "${GREEN}✓ Redis ready${NC}"
    echo ""

    # Pull and push MinIO
    echo -e "${BLUE}[3/4] MinIO (object storage)${NC}"
    docker pull --platform linux/amd64 minio/minio:latest 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker pull --platform linux/arm64 minio/minio:latest 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker tag minio/minio:latest ${REGISTRY}/addp-infra-minio:latest-amd64
    docker tag minio/minio:latest ${REGISTRY}/addp-infra-minio:latest-arm64
    docker push ${REGISTRY}/addp-infra-minio:latest-amd64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker push ${REGISTRY}/addp-infra-minio:latest-arm64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker buildx imagetools create --tag ${REGISTRY}/addp-infra-minio:latest \
        ${REGISTRY}/addp-infra-minio:latest-amd64 ${REGISTRY}/addp-infra-minio:latest-arm64
    echo -e "${GREEN}✓ MinIO ready${NC}"
    echo ""

    # Pull and push Elasticsearch
    echo -e "${BLUE}[4/4] Elasticsearch (search engine)${NC}"
    docker pull --platform linux/amd64 elasticsearch:8.11.0 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker pull --platform linux/arm64 elasticsearch:8.11.0 2>&1 | grep -v "Pulling\|pull\|layer\|Waiting\|Download" || true
    docker tag elasticsearch:8.11.0 ${REGISTRY}/addp-infra-elasticsearch:8.11.0-amd64
    docker tag elasticsearch:8.11.0 ${REGISTRY}/addp-infra-elasticsearch:8.11.0-arm64
    docker push ${REGISTRY}/addp-infra-elasticsearch:8.11.0-amd64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker push ${REGISTRY}/addp-infra-elasticsearch:8.11.0-arm64 2>&1 | grep -v "Unavailable\|Layer already exists" || true
    docker buildx imagetools create --tag ${REGISTRY}/addp-infra-elasticsearch:8.11.0 \
        ${REGISTRY}/addp-infra-elasticsearch:8.11.0-amd64 ${REGISTRY}/addp-infra-elasticsearch:8.11.0-arm64
    echo -e "${GREEN}✓ Elasticsearch ready${NC}"
    echo ""



    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}✓ Infrastructure Setup Complete${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
else
    echo -e "${GREEN}✓ All infrastructure images found in registry${NC}"
    echo ""
fi

# =============================================================================
# Step 0: Compile Go Binaries Locally
# =============================================================================

if [ "$SKIP_BUILD" = false ]; then
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Step 0/3: Compiling Go Binaries${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""

    if [ ! -f "$SCRIPT_DIR/0-compile-binaries.sh" ]; then
        echo -e "${RED}Error: 0-compile-binaries.sh not found${NC}"
        exit 1
    fi

    # Determine compilation architecture
    if [ "$BUILD_ARCH" = "both" ]; then
        COMPILE_ARCH="both"
        echo -e "${YELLOW}Compiling binaries for multi-arch deployment (amd64 + arm64)...${NC}"
    else
        COMPILE_ARCH="$BUILD_ARCH"
        echo -e "${YELLOW}Compiling binaries for ${BUILD_ARCH} architecture...${NC}"
    fi

    if "$SCRIPT_DIR/0-compile-binaries.sh" --arch "$COMPILE_ARCH"; then
        echo -e "${GREEN}✓ Binaries compiled successfully${NC}"
    else
        echo -e "${RED}✗ Compilation failed${NC}"
        exit 1
    fi
fi

# =============================================================================
# Step 1: Build Images
# =============================================================================

if [ "$SKIP_BUILD" = false ]; then
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Step 1/3: Building Docker Images${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""

    # Choose build script based on architecture
    if [ "$BUILD_ARCH" = "both" ]; then
        # Multi-arch build
        if [ ! -f "$SCRIPT_DIR/1-build-images-multiarch.sh" ]; then
            echo -e "${RED}Error: 1-build-images-multiarch.sh not found${NC}"
            exit 1
        fi

        echo -e "${YELLOW}Building multi-arch images (amd64 + arm64) - Offline mode...${NC}"
        echo -e "${YELLOW}This builds each architecture separately then creates manifest lists${NC}"
        echo ""

        if "$SCRIPT_DIR/1-build-images-multiarch.sh" --registry "$REGISTRY"; then
            echo -e "${GREEN}✓ Multi-arch images built successfully${NC}"
        else
            echo -e "${RED}✗ Image build failed${NC}"
            exit 1
        fi
    else
        # Single-arch build
        if [ ! -f "$SCRIPT_DIR/1-build-images.sh" ]; then
            echo -e "${RED}Error: 1-build-images.sh not found${NC}"
            exit 1
        fi

        echo -e "${YELLOW}Building ${BUILD_ARCH} images...${NC}"
        echo ""

        if "$SCRIPT_DIR/1-build-images.sh" --registry "$REGISTRY"; then
            echo -e "${GREEN}✓ ${BUILD_ARCH} images built successfully${NC}"
        else
            echo -e "${RED}✗ Image build failed${NC}"
            exit 1
        fi
    fi
else
    echo -e "${YELLOW}Skipping build (--skip-build)${NC}"
fi

# =============================================================================
# Step 2: Package and Transfer/Extract
# =============================================================================

if [ "$SKIP_TRANSFER" = false ]; then
    echo ""
    echo -e "${BLUE}========================================${NC}"
    if [ "$IS_LOCAL" = true ]; then
        echo -e "${BLUE}Step 2/3: Packaging and Local Setup${NC}"
    else
        echo -e "${BLUE}Step 2/3: Packaging and Transfer${NC}"
    fi
    echo -e "${BLUE}========================================${NC}"
    echo ""

    if [ ! -f "$SCRIPT_DIR/2-package-deploy.sh" ]; then
        echo -e "${RED}Error: 2-package-deploy.sh not found${NC}"
        exit 1
    fi

    if [ "$IS_LOCAL" = true ]; then
        # Local deployment: create package and extract locally
        echo -e "${YELLOW}Creating deployment package...${NC}"
        PACKAGE_OUTPUT=$("$SCRIPT_DIR/2-package-deploy.sh" --registry "$REGISTRY" 2>&1)
        PACKAGE_FILE=$(echo "$PACKAGE_OUTPUT" | grep -o 'addp-deploy-[0-9_]*.tar.gz' | head -1)

        if [ -z "$PACKAGE_FILE" ] || [ ! -f "$PACKAGE_FILE" ]; then
            echo -e "${RED}✗ Failed to create package${NC}"
            exit 1
        fi

        echo -e "${GREEN}✓ Package created: $PACKAGE_FILE${NC}"
        echo ""

        # Create deployment directory
        echo -e "${YELLOW}Preparing deployment directory: $DEPLOY_DIR${NC}"
        rm -rf "$DEPLOY_DIR"
        mkdir -p "$DEPLOY_DIR"

        # Extract package
        echo -e "${YELLOW}Extracting package...${NC}"
        tar -xzf "$PACKAGE_FILE" -C "$DEPLOY_DIR" --strip-components=1

        echo -e "${GREEN}✓ Package extracted to $DEPLOY_DIR${NC}"
    else
        # Remote deployment: package and transfer via SSH
        if "$SCRIPT_DIR/2-package-deploy.sh" --registry "$REGISTRY" --server "$SERVER"; then
            echo -e "${GREEN}✓ Files transferred successfully${NC}"
        else
            echo -e "${RED}✗ File transfer failed${NC}"
            exit 1
        fi
    fi
else
    echo -e "${YELLOW}Skipping file transfer (--skip-transfer)${NC}"
fi

# =============================================================================
# Step 3: Server Setup (Local or Remote)
# =============================================================================

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Step 3/3: Server Setup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ "$IS_LOCAL" = true ]; then
    # Local deployment: run setup script directly
    echo -e "${YELLOW}Running local setup...${NC}"
    echo ""

    cd "$DEPLOY_DIR"

    if [ ! -f "scripts/3-server-setup.sh" ]; then
        echo -e "${RED}Error: 3-server-setup.sh not found in $DEPLOY_DIR${NC}"
        echo "Files in $DEPLOY_DIR:"
        ls -la
        exit 1
    fi

    chmod +x scripts/3-server-setup.sh

    # Run setup script with --force and --registry parameters
    if ./scripts/3-server-setup.sh --force --registry "$REGISTRY"; then
        SETUP_SUCCESS=true
    else
        SETUP_SUCCESS=false
    fi

else
    # Remote deployment: run setup via SSH
    echo -e "${YELLOW}Connecting to ${SERVER}...${NC}"

    # Execute server setup script remotely
    ssh "$SERVER" << 'REMOTE_SCRIPT'
#!/bin/bash
set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

cd ~/addp

echo -e "${YELLOW}Running server setup script...${NC}"

if [ ! -f "scripts/3-server-setup.sh" ]; then
    echo -e "${RED}Error: 3-server-setup.sh not found${NC}"
    echo "Files in ~/addp:"
    ls -la
    exit 1
fi

chmod +x scripts/3-server-setup.sh

# Run setup script with --force and --skip-docker-restart for remote deployment
if ./scripts/3-server-setup.sh --force --skip-docker-restart; then
    echo -e "${GREEN}✓ Server setup completed${NC}"
else
    echo -e "${RED}✗ Server setup failed${NC}"
    exit 1
fi
REMOTE_SCRIPT

    if [ $? -eq 0 ]; then
        SETUP_SUCCESS=true
    else
        SETUP_SUCCESS=false
    fi
fi

# Display results
if [ "$SETUP_SUCCESS" = true ]; then
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Deployment Completed Successfully!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "Access your ADDP installation at:"
    if [ "$IS_LOCAL" = true ]; then
        echo -e "  ${BLUE}http://localhost:8000${NC} (Nginx统一网关)"
    else
        echo -e "  ${BLUE}http://${HOST}:8000${NC} (Nginx统一网关)"
    fi
    echo ""
    echo -e "All services are accessible through port 8000:"
    echo -e "  • ${BLUE}/${NC} → Portal (统一门户)"
    echo -e "  • ${BLUE}/api/${NC} → API Gateway"
    echo -e "  • ${BLUE}/system/${NC} → System Frontend"
    echo -e "  • ${BLUE}/manager/${NC} → Manager Frontend"
    echo ""
    echo -e "Default Super Admin:"
    echo -e "  Username: ${GREEN}SuperAdmin${NC}"
    echo -e "  Password: ${GREEN}20251001#SuperAdmin${NC}"
    echo ""
    echo -e "${RED}IMPORTANT: Change the default password after first login!${NC}"
    echo ""
    if [ "$IS_LOCAL" = true ]; then
        echo -e "${YELLOW}Deployment directory: ${BLUE}$DEPLOY_DIR${NC}"
        echo -e "${YELLOW}Useful commands:${NC}"
        echo -e "  View logs:    ${BLUE}cd $DEPLOY_DIR && docker compose -f docker-compose.prod.yml logs -f${NC}"
        echo -e "  Stop:         ${BLUE}cd $DEPLOY_DIR && docker compose -f docker-compose.prod.yml down${NC}"
        echo -e "  Restart:      ${BLUE}cd $DEPLOY_DIR && docker compose -f docker-compose.prod.yml restart${NC}"
    fi
else
    echo ""
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}Deployment Failed${NC}"
    echo -e "${RED}========================================${NC}"
    echo ""
    echo "Check the error messages above for details"
    if [ "$IS_LOCAL" = true ]; then
        echo "Check logs:"
        echo "  cd $DEPLOY_DIR"
        echo "  docker compose -f docker-compose.prod.yml logs"
    else
        echo "You can also SSH into the server and check logs:"
        echo "  ssh $SERVER"
        echo "  cd ~/addp"
        echo "  docker compose -f docker-compose.prod.yml logs"
    fi
    exit 1
fi
