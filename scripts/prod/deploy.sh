#!/bin/bash
# =============================================================================
# ADDP Production Deployment Script
# =============================================================================
# Description: Complete production release workflow
#              Compile → Build Images → Push → Deploy
# Usage: ./deploy.sh [VERSION] [OPTIONS]
# =============================================================================

set -euo pipefail

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
REGISTRY="${REGISTRY:-localhost:5001}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
SKIP_COMPILE=false
SKIP_BUILD=false
SKIP_PUSH=false
SKIP_DEPLOY=false
VERSION="latest"

# Check for help first
if [[ "${1:-}" == "-h" ]] || [[ "${1:-}" == "--help" ]]; then
    echo "Usage: $0 [VERSION] [OPTIONS]"
    echo ""
    echo "Arguments:"
    echo "  VERSION              Image version tag (default: latest)"
    echo ""
    echo "Options:"
    echo "  --registry URL       Registry URL (default: localhost:5001)"
    echo "  --skip-compile       Skip binary compilation"
    echo "  --skip-build         Skip image building"
    echo "  --skip-push          Skip image push to registry"
    echo "  --skip-deploy        Skip deployment"
    echo "  -h, --help           Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 v1.0.0                                    # Full release v1.0.0"
    echo "  $0 v1.0.0 --registry hub.docker.com/myorg   # Custom registry"
    echo "  $0 latest --skip-compile --skip-build       # Only push & deploy"
    exit 0
fi

# Set version from first argument if provided and not an option
if [[ $# -gt 0 ]] && [[ ! "$1" =~ ^-- ]]; then
    VERSION="$1"
    shift
fi

# Parse remaining arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --skip-compile)
            SKIP_COMPILE=true
            shift
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --skip-push)
            SKIP_PUSH=true
            shift
            ;;
        --skip-deploy)
            SKIP_DEPLOY=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Production Deployment${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "Version: ${GREEN}${VERSION}${NC}"
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
echo ""

# Step 1: Compile multi-arch binaries
if [ "$SKIP_COMPILE" = false ]; then
    echo -e "${YELLOW}[1/4] Compiling multi-arch binaries...${NC}"
    cd "$ROOT_DIR"
    if ! ./scripts/build/compile.sh --arch both; then
        echo -e "${RED}✗ Compilation failed${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Compilation completed${NC}"
    echo ""
else
    echo -e "${YELLOW}[1/4] Skipping compilation${NC}"
    echo ""
fi

# Step 2: Build multi-arch images
if [ "$SKIP_BUILD" = false ]; then
    echo -e "${YELLOW}[2/4] Building multi-arch images...${NC}"
    cd "$ROOT_DIR"
    if ! ./scripts/build/build-images.sh \
        --multi-arch \
        --tag "${VERSION}" \
        --registry "${REGISTRY}"; then
        echo -e "${RED}✗ Image build failed${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Images built${NC}"
    echo ""
else
    echo -e "${YELLOW}[2/4] Skipping image build${NC}"
    echo ""
fi

# Step 3: Push images to registry
if [ "$SKIP_PUSH" = false ]; then
    echo -e "${YELLOW}[3/4] Pushing images to registry...${NC}"
    
    # List of all services to push
    SERVICES=(
        "system-backend"
        "manager-backend"
        "manager-worker"
        "meta-backend"
        "meta-worker"
        "transfer-backend"
        "transfer-worker"
        "orchestrator-backend"
        "develop-backend"
        "service-backend"
        "gateway"
        "portal-frontend"
        "system-frontend"
        "manager-frontend"
        "meta-frontend"
        "transfer-frontend"
        "orchestrator-frontend"
        "develop-frontend"
    )
    
    failed_pushes=()
    for service in "${SERVICES[@]}"; do
        echo -e "  Pushing ${BLUE}addp-${service}:${VERSION}${NC}..."
        if ! docker push "${REGISTRY}/addp-${service}:${VERSION}" 2>/dev/null; then
            echo -e "  ${YELLOW}⚠ Failed to push addp-${service}${NC}"
            failed_pushes+=("$service")
        else
            echo -e "  ${GREEN}✓ addp-${service}${NC}"
        fi
    done
    
    if [ ${#failed_pushes[@]} -gt 0 ]; then
        echo -e "${YELLOW}⚠ Some images failed to push:${NC}"
        for service in "${failed_pushes[@]}"; do
            echo -e "  - ${service}"
        done
        echo -e "${YELLOW}Note: Images may not exist locally. Use --skip-push if already pushed.${NC}"
    else
        echo -e "${GREEN}✓ All images pushed${NC}"
    fi
    echo ""
else
    echo -e "${YELLOW}[3/4] Skipping image push${NC}"
    echo ""
fi

# Step 4: Deploy to production
if [ "$SKIP_DEPLOY" = false ]; then
    echo -e "${YELLOW}[4/4] Deploying to production...${NC}"
    
    # Detect deployment method
    if command -v kubectl &> /dev/null && kubectl cluster-info &> /dev/null; then
        echo -e "${BLUE}Detected Kubernetes cluster${NC}"
        echo -e "${YELLOW}Kubernetes deployment not yet implemented${NC}"
        echo -e "${YELLOW}Please deploy manually using kubectl${NC}"
        
    elif docker info 2>/dev/null | grep -q "Swarm: active"; then
        echo -e "${BLUE}Detected Docker Swarm${NC}"
        cd "$ROOT_DIR"
        
        # Set environment variables for deployment
        export IMAGE_TAG="${VERSION}"
        export REGISTRY="${REGISTRY}"
        
        if docker stack deploy -c docker-compose.yml addp; then
            echo -e "${GREEN}✓ Swarm stack deployed${NC}"
            
            # Wait for services to be running
            echo -e "${YELLOW}Waiting for services to start...${NC}"
            sleep 10
            docker stack services addp
        else
            echo -e "${RED}✗ Swarm deployment failed${NC}"
            exit 1
        fi
        
    else
        echo -e "${BLUE}Using Docker Compose${NC}"
        cd "$ROOT_DIR"
        
        # Set environment variables for deployment
        export IMAGE_TAG="${VERSION}"
        export REGISTRY="${REGISTRY}"
        
        if docker-compose -f docker-compose.infra.yml -f docker-compose.yml up -d; then
            echo -e "${GREEN}✓ Services started${NC}"
        else
            echo -e "${RED}✗ Deployment failed${NC}"
            exit 1
        fi
    fi
    echo ""
else
    echo -e "${YELLOW}[4/4] Skipping deployment${NC}"
    echo ""
fi

# Deployment summary
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Deployment Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Version: ${BLUE}${VERSION}${NC}"
echo -e "Registry: ${BLUE}${REGISTRY}${NC}"
echo ""
echo -e "Verification commands:"
echo -e "  ${BLUE}docker images | grep ${VERSION}${NC}"
echo -e "  ${BLUE}curl http://localhost:8180/health${NC}"
echo -e "  ${BLUE}./scripts/prod/health-check.sh${NC}"
echo ""
echo -e "Access URLs:"
echo -e "  Portal: ${BLUE}http://localhost${NC} (Nginx)"
echo -e "  Portal: ${BLUE}http://localhost:5170${NC} (Direct)"
echo -e "  Gateway: ${BLUE}http://localhost:8000${NC}"
echo ""
