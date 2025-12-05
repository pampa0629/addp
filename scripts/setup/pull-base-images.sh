#!/bin/bash
# =============================================================================
# Pull Base Images to Local Registry
# =============================================================================
# Description: Pull official base images and push to localhost:5001 registry
# Usage: ./pull-base-images.sh
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REGISTRY="${REGISTRY:-localhost:5001}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Pulling Base Images to Local Registry${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Base images needed by ADDP
BASE_IMAGES=(
    "alpine:latest"
    "node:18.20.5-alpine"
    "node:20-alpine"
    "nginx:alpine"
)

# Function to check if registry is accessible
check_registry() {
    echo -e "${YELLOW}Checking registry accessibility...${NC}"

    if ! curl -sf --max-time 5 "http://${REGISTRY}/v2/" > /dev/null 2>&1; then
        echo -e "${RED}Error: Registry ${REGISTRY} is not accessible${NC}"
        echo ""
        echo -e "${YELLOW}Starting local registry...${NC}"

        # Try to start existing registry container
        if docker ps -a | grep -q "registry"; then
            docker start registry 2>/dev/null || {
                # Remove and recreate if start fails
                docker rm -f registry 2>/dev/null || true
                docker run -d -p 5001:5000 --restart=always --name registry registry:2
            }
        else
            # Create new registry container
            docker run -d -p 5001:5000 --restart=always --name registry registry:2
        fi

        # Wait for registry to be ready
        local timeout=30
        local elapsed=0
        while ! curl -sf --max-time 5 "http://${REGISTRY}/v2/" > /dev/null 2>&1; do
            if [ $elapsed -ge $timeout ]; then
                echo -e "${RED}Registry failed to start within ${timeout}s${NC}"
                exit 1
            fi
            sleep 2
            elapsed=$((elapsed + 2))
        done

        echo -e "${GREEN}✓ Registry started${NC}"
    else
        echo -e "${GREEN}✓ Registry is accessible${NC}"
    fi
}

# Function to pull and push a base image
process_image() {
    local image=$1
    local registry_image="${REGISTRY}/${image}"

    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Processing: ${image}${NC}"
    echo -e "${BLUE}========================================${NC}"

    # Check if image already exists in registry
    local tags_response=$(curl -s "http://${REGISTRY}/v2/${image%%:*}/tags/list")
    local tag="${image##*:}"

    if echo "$tags_response" | grep -q "\"${tag}\""; then
        echo -e "${GREEN}✓ Image already exists in registry, skipping${NC}"
        return 0
    fi

    echo -e "${YELLOW}Pulling ${image} from Docker Hub...${NC}"
    if ! docker pull "$image"; then
        echo -e "${RED}✗ Failed to pull ${image}${NC}"
        return 1
    fi

    echo -e "${YELLOW}Tagging as ${registry_image}...${NC}"
    if ! docker tag "$image" "$registry_image"; then
        echo -e "${RED}✗ Failed to tag ${image}${NC}"
        return 1
    fi

    echo -e "${YELLOW}Pushing to local registry...${NC}"
    if ! docker push "$registry_image"; then
        echo -e "${RED}✗ Failed to push ${image}${NC}"
        return 1
    fi

    echo -e "${GREEN}✓ Successfully processed ${image}${NC}"
}

# Main process
main() {
    check_registry

    local failed_images=()

    for image in "${BASE_IMAGES[@]}"; do
        if ! process_image "$image"; then
            failed_images+=("$image")
        fi
    done

    # Summary
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Summary${NC}"
    echo -e "${BLUE}========================================${NC}"

    if [ ${#failed_images[@]} -eq 0 ]; then
        echo -e "${GREEN}✓ All base images processed successfully${NC}"
        echo ""
        echo "Registry: ${REGISTRY}"
        echo "Images available:"
        for image in "${BASE_IMAGES[@]}"; do
            echo "  - ${REGISTRY}/${image}"
        done
        return 0
    else
        echo -e "${RED}✗ Failed images:${NC}"
        for failed in "${failed_images[@]}"; do
            echo "  - ${failed}"
        done
        return 1
    fi
}

# Run main function
main
