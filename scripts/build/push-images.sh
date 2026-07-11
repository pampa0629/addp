#!/bin/bash
# =============================================================================
# ADDP Image Pusher
# =============================================================================
# Description: Push Docker images to a remote registry (Docker Hub, Harbor, etc.)
# Usage: ./push-images.sh [OPTIONS]
# Options:
#   --registry REGISTRY_URL   Target registry (e.g., docker.io/USERNAME, harbor.example.com:5001)
#   --tag VERSION             Image tag (default: latest)
#   --services SERVICE_LIST   Specific services to push (comma-separated, default: all)
#   --dry-run                 Show what would be pushed without actually pushing
#   --source-registry URL     Source registry (default: localhost:5001)
#
# Examples:
#   # Push all images to Docker Hub
#   ./push-images.sh --registry docker.io/myusername
#
#   # Push to Aliyun ACR
#   ./push-images.sh --registry crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp
#
#   # Push specific services with version tag
#   ./push-images.sh --registry docker.io/myusername --tag v1.0.0 --services system-backend,gateway
#
#   # Dry-run to see what would be pushed
#   ./push-images.sh --registry docker.io/myusername --dry-run
# =============================================================================

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
SOURCE_REGISTRY="${SOURCE_REGISTRY:-localhost:5001}"
TARGET_REGISTRY=""
IMAGE_TAG="${IMAGE_TAG:-latest}"
SERVICES_TO_PUSH="all"
DRY_RUN=false
FORCE_PUSH=false
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --registry)
            TARGET_REGISTRY="$2"
            shift 2
            ;;
        --tag)
            IMAGE_TAG="$2"
            shift 2
            ;;
        --services)
            SERVICES_TO_PUSH="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --force)
            FORCE_PUSH=true
            shift
            ;;
        --source-registry)
            SOURCE_REGISTRY="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Usage: $0 --registry REGISTRY_URL [--tag VERSION] [--services SERVICE_LIST] [--dry-run]"
            exit 1
            ;;
    esac
done

# Validate required arguments
if [ -z "$TARGET_REGISTRY" ]; then
    echo -e "${RED}Error: --registry is required${NC}"
    echo ""
    echo "Usage: $0 --registry REGISTRY_URL [OPTIONS]"
    echo ""
    echo "Examples:"
    echo "  # Push to Docker Hub"
    echo "  $0 --registry docker.io/myusername"
    echo ""
    echo "  # Push to Harbor"
    echo "  $0 --registry harbor.example.com:5001/addp"
    echo ""
    exit 1
fi

cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Image Pusher${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Source Registry: ${GREEN}${SOURCE_REGISTRY}${NC}"
echo -e "Target Registry: ${GREEN}${TARGET_REGISTRY}${NC}"
echo -e "Tag: ${GREEN}${IMAGE_TAG}${NC}"
echo -e "Services: ${GREEN}${SERVICES_TO_PUSH}${NC}"
echo -e "Dry Run: ${GREEN}${DRY_RUN}${NC}"
echo ""

# All ADDP services - must stay in sync with build-images.sh
ALL_SERVICES=(
    "system-backend" "manager-backend" "meta-backend"
    "transfer-backend" "orchestrator-backend" "develop-backend" "service-backend"
    "monitor-backend" "standard-backend" "copilot-backend" "agent-backend" "model-backend" "quality-backend" "asset-backend" "portal-backend" "graph-backend"
    "python-workflow-engine" "raster-mosaic-runtime" "model3d-workflow-engine" "pointcloud-workflow-engine" "supermap-workflow-engine" "spark-workflow-engine" "jupyter-engine"
    "gateway"
    "meta-worker" "transfer-worker"
    "system-frontend" "manager-frontend" "meta-frontend"
    "transfer-frontend" "orchestrator-frontend" "develop-frontend"
    "service-frontend" "monitor-frontend" "standard-frontend"
    "agent-frontend" "model-frontend" "quality-frontend" "asset-frontend" "portal-frontend" "graph-frontend"
    "console" "nginx"
    "postgres"
)

# Determine which services to push
if [ "$SERVICES_TO_PUSH" = "all" ]; then
    SERVICES_LIST=("${ALL_SERVICES[@]}")
else
    IFS=',' read -ra SERVICES_LIST <<< "$SERVICES_TO_PUSH"
fi

echo -e "${YELLOW}Services to push (${#SERVICES_LIST[@]} total):${NC}"
for service in "${SERVICES_LIST[@]}"; do
    echo -e "  - ${service}"
done
echo ""

# Verify Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running${NC}"
    echo "Please start Docker and try again"
    exit 1
fi

# Check Docker login status (only if not dry-run)
if [ "$DRY_RUN" = false ]; then
    echo -e "${YELLOW}Checking Docker login status...${NC}"

    # Extract registry host from TARGET_REGISTRY
    REGISTRY_HOST=$(echo "$TARGET_REGISTRY" | cut -d'/' -f1)

    # Check credentials: support both inline auth and external credential stores (e.g. Docker Desktop)
    REGISTRY_AUTH_HOST="$REGISTRY_HOST"
    if [ "$REGISTRY_HOST" = "docker.io" ] || [ "$REGISTRY_HOST" = "registry-1.docker.io" ]; then
        REGISTRY_AUTH_HOST="https://index.docker.io/v1/"
    fi
    HAS_CREDS=false
    if docker-credential-desktop list 2>/dev/null | grep -q "$REGISTRY_AUTH_HOST"; then
        HAS_CREDS=true
    elif cat ~/.docker/config.json 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); exit(0 if any('$REGISTRY_AUTH_HOST' in k or '$REGISTRY_HOST' in k for k in d.get('auths',{})) else 1)" 2>/dev/null; then
        HAS_CREDS=true
    elif docker info 2>&1 | grep -q "Username"; then
        HAS_CREDS=true
    fi
    if [ "$HAS_CREDS" = false ]; then
        echo -e "${YELLOW}Warning: No Docker credentials found${NC}"
        echo -e "${YELLOW}You may need to run: docker login${NC}"
        echo ""
        read -p "Continue anyway? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo -e "${RED}Aborted${NC}"
            exit 1
        fi
    fi
    echo ""
fi

# Function to check if image exists locally
check_image_exists() {
    local image_name=$1
    if docker image inspect "$image_name" > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Push cache: records "target_image -> source_image_id" after each successful push.
# On next run, if the source image ID hasn't changed, skip the push.
PUSH_CACHE_FILE="${PROJECT_ROOT}/.push-cache"

# Returns 0 if up-to-date (skip), 1 if needs push
check_cache_up_to_date() {
    local source_image=$1
    local target_image=$2

    [ ! -f "$PUSH_CACHE_FILE" ] && return 1

    local current_id
    current_id=$(docker inspect --format='{{.Id}}' "$source_image" 2>/dev/null)
    [ -z "$current_id" ] && return 1

    local cached_id
    cached_id=$(grep "^${target_image}=" "$PUSH_CACHE_FILE" 2>/dev/null | cut -d'=' -f2)
    [ -z "$cached_id" ] && return 1

    [ "$current_id" = "$cached_id" ] && return 0
    return 1
}

update_cache() {
    local source_image=$1
    local target_image=$2

    local image_id
    image_id=$(docker inspect --format='{{.Id}}' "$source_image" 2>/dev/null)
    [ -z "$image_id" ] && return

    # Update or insert the entry
    if [ -f "$PUSH_CACHE_FILE" ] && grep -q "^${target_image}=" "$PUSH_CACHE_FILE" 2>/dev/null; then
        sed -i '' "s|^${target_image}=.*|${target_image}=${image_id}|" "$PUSH_CACHE_FILE"
    else
        echo "${target_image}=${image_id}" >> "$PUSH_CACHE_FILE"
    fi
}

# Global variable to communicate skip reason from push_image to caller
PUSH_SKIP_REASON=""

# Function to push a single image with retry
push_image() {
    local service=$1
    # postgres uses a fixed tag and its own local name (not from SOURCE_REGISTRY)
    local tag="${IMAGE_TAG}"
    local source_image
    if [ "$service" = "postgres" ]; then
        tag="15-arm64"
        source_image="pampa0629/addp-${service}:${tag}"
    else
        source_image="${SOURCE_REGISTRY}/addp-${service}:${tag}"
    fi
    local target_image="${TARGET_REGISTRY}/addp-${service}:${tag}"
    local max_retries=5
    local retry_delay=15

    PUSH_SKIP_REASON=""
    echo -e "${YELLOW}Processing ${service}...${NC}"

    # Check if source image exists
    if ! check_image_exists "$source_image"; then
        echo -e "${RED}⚠ Warning: Source image not found: ${source_image}${NC}"
        echo -e "${RED}  Skipping...${NC}"
        echo ""
        PUSH_SKIP_REASON="not_found"
        return 1
    fi

    # Check if remote already has the same image (skip if up-to-date)
    if [ "$FORCE_PUSH" = false ] && [ "$DRY_RUN" = false ]; then
        echo -e "  Checking cache..."
        if check_cache_up_to_date "$source_image" "$target_image"; then
            echo -e "${GREEN}✓ ${service} already up-to-date, skipping${NC}"
            echo ""
            PUSH_SKIP_REASON="up_to_date"
            return 0
        fi
    fi

    # Tag image for target registry (if different from source)
    if [ "$SOURCE_REGISTRY" != "$TARGET_REGISTRY" ]; then
        echo -e "  Tagging: ${source_image} → ${target_image}"
        if [ "$DRY_RUN" = false ]; then
            if ! docker tag "$source_image" "$target_image"; then
                echo -e "${RED}✗ Failed to tag ${service}${NC}"
                echo ""
                return 1
            fi
        fi
    fi

    # Push image
    if [ "$DRY_RUN" = true ]; then
        echo -e "  ${BLUE}[DRY RUN]${NC} Would push: ${target_image}"
        echo -e "${GREEN}✓ ${service} (dry-run)${NC}"
    else
        echo -e "  Pushing: ${target_image}"

        # Retry logic for push
        local retry_count=0
        local push_success=false

        while [ $retry_count -lt $max_retries ]; do
            if [ $retry_count -gt 0 ]; then
                echo -e "${YELLOW}  ⚠ Push failed, retrying ($retry_count/$max_retries) in ${retry_delay}s...${NC}"
                sleep $retry_delay
            fi

            if docker push "$target_image" > /tmp/docker-push-$$.log 2>&1; then
                push_success=true
                rm -f /tmp/docker-push-$$.log
                break
            else
                retry_count=$((retry_count + 1))
                if [ $retry_count -ge $max_retries ]; then
                    echo -e "${RED}  Last error log:${NC}"
                    tail -n 10 /tmp/docker-push-$$.log
                    rm -f /tmp/docker-push-$$.log
                fi
            fi
        done

        if [ "$push_success" = true ]; then
            local size=$(docker image inspect "$target_image" --format='{{.Size}}' 2>/dev/null || echo "0")
            local size_mb=$((size / 1024 / 1024))
            echo -e "${GREEN}✓ Pushed ${service} (${size_mb}MB)${NC}"
            update_cache "$source_image" "$target_image"
        else
            echo -e "${RED}✗ Failed to push ${service} after $max_retries attempts${NC}"
            echo -e "${YELLOW}  Hint: Check network connection or try again later${NC}"
            echo ""
            return 1
        fi
    fi
    echo ""
    return 0
}

# Push all images
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Pushing Images${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

pushed_count=0
failed_count=0
skipped_count=0
uptodate_count=0
failed_services=()
skipped_services=()
uptodate_services=()

for service in "${SERVICES_LIST[@]}"; do
    if push_image "$service"; then
        if [ "$PUSH_SKIP_REASON" = "up_to_date" ]; then
            uptodate_count=$((uptodate_count + 1))
            uptodate_services+=("$service")
        else
            pushed_count=$((pushed_count + 1))
        fi
    else
        if [ "$PUSH_SKIP_REASON" = "not_found" ]; then
            skipped_count=$((skipped_count + 1))
            skipped_services+=("$service")
        else
            failed_count=$((failed_count + 1))
            failed_services+=("$service")
        fi
    fi
done

# Summary
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "Total services: ${#SERVICES_LIST[@]}"
echo -e "Successfully pushed: ${GREEN}${pushed_count}${NC}"
echo -e "Already up-to-date (skipped): ${BLUE}${uptodate_count}${NC}"
echo -e "Failed: ${RED}${failed_count}${NC}"
echo -e "Skipped (not found): ${YELLOW}${skipped_count}${NC}"
echo ""

if [ ${#failed_services[@]} -gt 0 ]; then
    echo -e "${RED}Failed services:${NC}"
    for service in "${failed_services[@]}"; do
        echo -e "  - ${service}"
    done
    echo ""
fi

if [ ${#uptodate_services[@]} -gt 0 ]; then
    echo -e "${BLUE}Already up-to-date (skipped):${NC}"
    for service in "${uptodate_services[@]}"; do
        echo -e "  - ${service}"
    done
    echo ""
fi

if [ ${#skipped_services[@]} -gt 0 ]; then
    echo -e "${YELLOW}Skipped services (image not found):${NC}"
    for service in "${skipped_services[@]}"; do
        echo -e "  - ${service}"
    done
    echo ""
    echo -e "${YELLOW}Hint: Run './scripts/build/build-images.sh' to build missing images${NC}"
    echo ""
fi

if [ "$DRY_RUN" = true ]; then
    echo -e "${BLUE}This was a dry run. No images were actually pushed.${NC}"
    echo -e "Remove --dry-run flag to push for real."
    echo ""
fi

# Exit with appropriate code
if [ ${#failed_services[@]} -gt 0 ]; then
    echo -e "${RED}✗ Some images failed to push${NC}"
    exit 1
elif [ "$pushed_count" -eq 0 ] && [ "$uptodate_count" -eq 0 ]; then
    echo -e "${YELLOW}⚠ No images were pushed${NC}"
    exit 1
elif [ "$pushed_count" -eq 0 ] && [ "$uptodate_count" -gt 0 ]; then
    echo -e "${GREEN}✓ All images already up-to-date, nothing to push${NC}"
    echo ""
    exit 0
else
    echo -e "${GREEN}✓ All images pushed successfully!${NC}"
    echo ""
    echo -e "${YELLOW}Next steps:${NC}"
    echo "1. Verify images in registry: $TARGET_REGISTRY"
    if [[ "$TARGET_REGISTRY" == *"docker.io"* ]] || [[ "$TARGET_REGISTRY" == docker.io* ]]; then
        echo "   Visit: https://hub.docker.com/r/${TARGET_REGISTRY#docker.io/}/repositories"
    fi
    echo "2. Generate deployment package:"
    echo "   ./scripts/build/package.sh --mode registry --registry $TARGET_REGISTRY"
    echo "3. Deploy on server:"
    echo "   docker compose -f docker-compose.yml pull"
    echo "   bash scripts/prod/start.sh"
    exit 0
fi
