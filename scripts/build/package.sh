#!/bin/bash
# =============================================================================
# ADDP Deployment Packager
# =============================================================================
# Description: Package deployment files for offline or registry-based deployment
# Usage: ./package.sh [OPTIONS]
# Options:
#   --mode MODE               Deployment mode: offline|registry (default: offline)
#   --registry REGISTRY_URL   Registry URL (default: localhost:5001)
#   --server SERVER_USER@IP   Automatically transfer to server via rsync
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default values
MODE="images"
REGISTRY="${REGISTRY:-localhost:5001}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
SERVER=""
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --mode)
            MODE="$2"
            if [[ "$MODE" != "images" && "$MODE" != "registry" ]]; then
                echo -e "${RED}Error: Invalid mode '$MODE'. Must be 'images' or 'registry'${NC}"
                exit 1
            fi
            shift 2
            ;;
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --server)
            SERVER="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Usage: $0 [--mode images|registry] [--registry URL] [--server user@host]"
            exit 1
            ;;
    esac
done

cd "$PROJECT_ROOT"

# Set output directory based on mode
OUTPUT_DIR="dist/package-${MODE}-${TIMESTAMP}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Deployment Packager${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Mode: ${GREEN}${MODE}${NC}"
echo -e "Output: ${GREEN}${OUTPUT_DIR}${NC}"
echo -e "Registry: ${GREEN}${REGISTRY}${NC}"
if [ -n "$SERVER" ]; then
    echo -e "Target Server: ${GREEN}${SERVER}${NC}"
fi
echo ""

# Function: Copy common files (needed by both modes)
copy_common_files() {
    echo -e "${YELLOW}Copying common files...${NC}"

    # Create base directory structure
    mkdir -p "$OUTPUT_DIR"

    # 1. Copy docker-compose files
    if [ -f "docker-compose.infra.yml" ]; then
        cp docker-compose.infra.yml "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ docker-compose.infra.yml copied${NC}"
    else
        echo -e "${RED}Error: docker-compose.infra.yml not found${NC}"
        exit 1
    fi

    if [ -f "docker-compose.yml" ]; then
        cp docker-compose.yml "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ docker-compose.yml copied${NC}"
    else
        echo -e "${RED}Error: docker-compose.yml not found${NC}"
        exit 1
    fi

    # 2. Copy environment template
    if [ -f ".env.example" ]; then
        cp .env.example "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ .env.example copied${NC}"
    else
        echo -e "${RED}Error: .env.example not found${NC}"
        exit 1
    fi

    # For local packaging/deployment, preserve the active environment when present.
    # This avoids generating a new database password against an existing Docker volume.
    if [ -f ".env" ]; then
        cp .env "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ .env copied (reuse current deployment secrets)${NC}"
    else
        echo -e "${YELLOW}ℹ .env not found; package will generate one on first start${NC}"
    fi

    # 3. Copy scripts/infra/ (CORE: database initialization)
    if [ -d "scripts/infra" ]; then
        cp -r scripts/infra "$OUTPUT_DIR/scripts/"
        echo -e "${GREEN}✓ scripts/infra/ copied (including init-db.sql)${NC}"
    else
        echo -e "${RED}Error: scripts/infra/ directory not found${NC}"
        exit 1
    fi

    # 4. Copy scripts/prod/ (start/stop scripts)
    if [ -d "scripts/prod" ]; then
        cp -r scripts/prod "$OUTPUT_DIR/scripts/"
        chmod +x "$OUTPUT_DIR/scripts/prod"/*.sh 2>/dev/null || true
        echo -e "${GREEN}✓ scripts/prod/ copied${NC}"
    else
        echo -e "${YELLOW}Warning: scripts/prod/ not found${NC}"
    fi

    # 5. Copy business infrastructure (recommended)
    if [ -d "business" ]; then
        cp -r business "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ business/ copied (business infrastructure)${NC}"
    else
        echo -e "${YELLOW}Warning: business/ directory not found${NC}"
    fi

    # 6. Copy nginx configuration (if exists)
    if [ -f "nginx/nginx.prod.conf" ]; then
        mkdir -p "$OUTPUT_DIR/nginx"
        cp nginx/nginx.prod.conf "$OUTPUT_DIR/nginx/"
        echo -e "${GREEN}✓ nginx/nginx.prod.conf copied${NC}"
    else
        echo -e "${YELLOW}Warning: nginx/nginx.prod.conf not found${NC}"
    fi
}

# Function: Export Docker images to tar files
export_images() {
    echo -e "${YELLOW}Exporting Docker images...${NC}"
    echo ""

    local images_dir="$OUTPUT_DIR/images"
    mkdir -p "$images_dir"

    # List of deployable images — keep in sync with build-images.sh and runtime sidecars.
    local services=(
        # Backend services
        "system-backend" "manager-backend" "meta-backend"
        "transfer-backend" "orchestrator-backend" "develop-backend"
        "service-backend" "monitor-backend" "standard-backend" "copilot-backend"
        "agent-backend" "model-backend" "quality-backend" "asset-backend" "portal-backend" "graph-backend"
        "gateway"
        # Worker services
        "meta-worker" "transfer-worker"
        # Engine/runtime services
        "python-workflow-engine" "raster-mosaic-runtime" "model3d-workflow-engine"
        "pointcloud-workflow-engine" "spark-workflow-engine" "jupyter-engine"
        # Frontend services
        "system-frontend" "manager-frontend" "meta-frontend"
        "transfer-frontend" "orchestrator-frontend" "develop-frontend"
        "service-frontend" "monitor-frontend" "standard-frontend"
        "agent-frontend" "model-frontend" "quality-frontend" "asset-frontend" "portal-frontend" "graph-frontend"
        # Infrastructure
        "console" "nginx"
    )

    local exported_count=0
    local failed_services=()

    # Export each image
    for service in "${services[@]}"; do
        local image_name="${REGISTRY}/addp-${service}:${IMAGE_TAG}"
        local tar_file="${images_dir}/${service}.tar"

        echo -e "${YELLOW}Exporting ${service}...${NC}"

        # Check if image exists
        if ! docker image inspect "${image_name}" > /dev/null 2>&1; then
            echo -e "${RED}⚠ Warning: Image ${image_name} not found, skipping${NC}"
            failed_services+=("$service")
            continue
        fi

        # Export image
        if docker save "${image_name}" -o "${tar_file}"; then
            local size=$(du -h "${tar_file}" | cut -f1)
            echo -e "${GREEN}✓ Exported ${service} (${size})${NC}"
            exported_count=$((exported_count + 1))
        else
            echo -e "${RED}✗ Failed to export ${service}${NC}"
            failed_services+=("$service")
        fi
    done

    echo ""
    echo -e "${GREEN}✓ Exported ${exported_count}/${#services[@]} images${NC}"

    if [ ${#failed_services[@]} -gt 0 ]; then
        echo -e "${YELLOW}⚠ Failed/skipped services:${NC}"
        for service in "${failed_services[@]}"; do
            echo -e "  - ${service}"
        done
        echo ""
        echo -e "${YELLOW}Hint: Run './scripts/build/build-images.sh' to build missing images${NC}"
    fi
    echo ""
}

# Function: Create load-images.sh script for server
create_load_script() {
    echo -e "${YELLOW}Creating image load script...${NC}"

    cat > "$OUTPUT_DIR/load-images.sh" << 'EOF'
#!/bin/bash
# =============================================================================
# ADDP Image Loader
# =============================================================================
# Description: Load all Docker images from images/ directory
# Usage: ./load-images.sh
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Image Loader${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Count total images
TOTAL=$(ls images/*.tar 2>/dev/null | wc -l | tr -d ' ')

if [ "$TOTAL" -eq 0 ]; then
    echo -e "${RED}Error: No image tar files found in images/${NC}"
    exit 1
fi

echo -e "Found ${GREEN}${TOTAL}${NC} image(s) to load"
echo ""

# Load each image
LOADED=0
FAILED=0

for tar in images/*.tar; do
    IMAGE_NAME=$(basename "$tar" .tar)
    echo -e "${YELLOW}Loading ${IMAGE_NAME}...${NC}"

    if docker load -i "$tar"; then
        echo -e "${GREEN}✓ Loaded ${IMAGE_NAME}${NC}"
        LOADED=$((LOADED + 1))
    else
        echo -e "${RED}✗ Failed to load ${IMAGE_NAME}${NC}"
        FAILED=$((FAILED + 1))
    fi
    echo ""
done

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "Total: ${TOTAL}"
echo -e "Loaded: ${GREEN}${LOADED}${NC}"
echo -e "Failed: ${RED}${FAILED}${NC}"
echo ""

if [ "$FAILED" -eq 0 ]; then
    echo -e "${GREEN}✓ All images loaded successfully!${NC}"
    echo ""
    echo -e "${YELLOW}Next steps:${NC}"
    echo "1. Configure environment: cp .env.example .env.prod && vim .env.prod"
    echo "2. Start infrastructure: bash scripts/infra/up.sh"
    echo "3. Start services: docker compose -f docker-compose.yml --env-file .env.prod up -d"
    exit 0
else
    echo -e "${RED}✗ Some images failed to load${NC}"
    exit 1
fi
EOF

    chmod +x "$OUTPUT_DIR/load-images.sh"
    echo -e "${GREEN}✓ load-images.sh created${NC}"
    echo ""
}

# Function: Update registry URLs in docker-compose files
update_registry_url() {
    echo -e "${YELLOW}Updating registry URLs to ${REGISTRY}...${NC}"

    # Replace localhost:5001 with actual registry URL
    if [ -f "$OUTPUT_DIR/docker-compose.yml" ]; then
        # Use perl for cross-platform compatibility (macOS + Linux)
        if command -v perl &> /dev/null; then
            perl -pi -e "s|localhost:5001|${REGISTRY}|g" "$OUTPUT_DIR/docker-compose.yml"
        else
            # Fallback to sed with backup
            sed -i.bak "s|localhost:5001|${REGISTRY}|g" "$OUTPUT_DIR/docker-compose.yml"
            rm -f "$OUTPUT_DIR/docker-compose.yml.bak"
        fi
        echo -e "${GREEN}✓ Registry URLs updated in docker-compose.yml${NC}"
    fi
}

# Function: Generate mode-specific README
generate_readme() {
    echo -e "${YELLOW}Generating README for ${MODE} mode...${NC}"

    if [ "$MODE" = "images" ]; then
        cat > "$OUTPUT_DIR/README.md" << 'EOF'
# ADDP Images Deployment Package

This package contains **pre-built Docker images** for offline deployment (no registry or compilation required).

## Package Contents

- `images/` - Pre-built Docker images (40 tar files, ~5-10GB total)
- `load-images.sh` - Script to load all images into Docker
- `docker-compose.infra.yml` - Infrastructure services configuration
- `docker-compose.yml` - Application services configuration
- `.env.example` - Environment variables template
- `scripts/infra/` - Infrastructure initialization scripts
- `scripts/prod/` - Production startup/stop scripts
- `business/` - Business infrastructure (optional)
- `nginx/` - Nginx configuration

## Server Requirements

- ✅ **Docker only** (no Go, Node.js, or build tools required)
- ✅ ~10GB disk space for images and containers
- ✅ No internet access required

## Deployment Steps

### 1. Transfer Package to Server

```bash
# Option A: Transfer tarball (recommended for large packages)
scp addp-deploy-images-*.tar.gz user@server:~/
ssh user@server
cd ~
tar -xzf addp-deploy-images-*.tar.gz
cd package-images-*/

# Option B: Transfer directory (faster if on same network)
rsync -avz --progress package-images-*/ user@server:~/addp-deploy/
```

### 2. Load Docker Images

```bash
cd ~/addp-deploy

# Load all images (takes 5-10 minutes depending on disk speed)
./load-images.sh
```

**Expected output**:
```
========================================
ADDP Image Loader
========================================

Found 40 image(s) to load

Loading system-backend...
✓ Loaded system-backend

...

========================================
Summary
========================================
Total: 40
Loaded: 40
Failed: 0

✓ All images loaded successfully!
```

### 3. Configure Environment

```bash
cp .env.example .env.prod
vim .env.prod

# MUST configure:
# - JWT_SECRET (generate with: openssl rand -base64 32)
# - ENCRYPTION_KEY (generate with: openssl rand -base64 32)
# - POSTGRES_PASSWORD
# - REDIS_PASSWORD
# - MINIO_ROOT_PASSWORD
```

### 4. Start Infrastructure

```bash
# Start PostgreSQL, Redis, MinIO, Meilisearch
bash scripts/infra/up.sh

# Wait for infrastructure to be ready (30-60 seconds)
```

### 5. Start Application Services

```bash
# Start all ADDP services
docker compose -f docker-compose.yml --env-file .env.prod up -d
```

### 6. Verify Deployment

```bash
# Check service status
docker compose -f docker-compose.yml ps

# Check logs
docker compose -f docker-compose.yml logs -f

# Access application
# Console: http://server-ip:80 (via Nginx, recommended)
# Direct: http://server-ip:5170 (Console)
# API Gateway: http://server-ip:8000
```

## Default Login

- **Super Admin**: `SuperAdmin` / `20251001#SuperAdmin`
- **IMPORTANT**: Change password after first login!

## Advantages

- ✅ **No compilation required** - pre-built images ready to use
- ✅ **Fast deployment** - just load and start (~10 minutes total)
- ✅ **Consistent environment** - images match development machine exactly
- ✅ **Offline capable** - no internet access needed on server
- ✅ **Simple requirements** - only Docker needed

## Package Size

- **Images**: ~5-10GB (40 Docker images)
- **Total**: ~5-10GB compressed tarball

## Troubleshooting

### Load Images Fails

```bash
# Check Docker is running
docker info

# Check disk space
df -h

# Manually load a specific image
docker load -i images/system-backend.tar
```

### Services Won't Start

```bash
# Check all images are loaded
docker images | grep addp

# Check infrastructure is running
docker ps | grep -E "postgres|redis|minio"

# Check logs
docker compose -f docker-compose.yml logs SERVICE_NAME

# Restart infrastructure
bash scripts/infra/down.sh
bash scripts/infra/up.sh
```

## Updating Deployment

To update to a new version:

```bash
# 1. Stop services
docker compose -f docker-compose.yml down

# 2. Transfer new package with updated images
# 3. Load new images (will overwrite old versions)
./load-images.sh

# 4. Restart services
docker compose -f docker-compose.yml --env-file .env.prod up -d
```

## Architecture

**Images deployment workflow**:
1. Build on development machine
2. Export all images using `docker save`
3. Package images + configs
4. Transfer to server
5. Load images using `docker load`
6. Start services

**Why this approach**:
- ✅ Server doesn't need build tools
- ✅ Guarantees identical images
- ✅ Works offline
- ✅ Faster than compiling on server
EOF
    else
        # Registry mode README
        cat > "$OUTPUT_DIR/README.md" << EOF
# ADDP Registry Deployment Package

This package is for **registry-based deployment** (requires access to Docker registry).

## Package Contents

- \`docker-compose.infra.yml\` - Infrastructure services
- \`docker-compose.yml\` - Application services (configured for registry: **${REGISTRY}**)
- \`.env.example\` - Environment variables template
- \`scripts/infra/\` - Infrastructure initialization scripts
- \`scripts/prod/\` - Production startup/stop scripts
- \`business/\` - Business infrastructure (optional)
- \`nginx/\` - Nginx configuration

## Pre-requisites

Images must be **pre-built and pushed** to registry: \`${REGISTRY}\`

Required images:
- \`${REGISTRY}/addp-system-backend:latest\`
- \`${REGISTRY}/addp-manager-backend:latest\`
- \`${REGISTRY}/addp-meta-backend:latest\`
- \`${REGISTRY}/addp-transfer-backend:latest\`
- \`${REGISTRY}/addp-orchestrator-backend:latest\`
- \`${REGISTRY}/addp-develop-backend:latest\`
- \`${REGISTRY}/addp-gateway:latest\`
- \`${REGISTRY}/addp-*-worker:latest\` (workers)
- \`${REGISTRY}/addp-*-frontend:latest\` (frontends)
- \`${REGISTRY}/addp-console:latest\`
- \`${REGISTRY}/addp-nginx:latest\`

## Deployment Steps

### 1. Transfer Package to Server

\`\`\`bash
# Transfer deployment package
rsync -avz package-registry-*/ user@server:~/addp-deploy/

# Or use tarball if generated
scp addp-deploy-registry-*.tar.gz user@server:~/
\`\`\`

### 2. Configure Environment

\`\`\`bash
cd ~/addp-deploy
cp .env.example .env.prod
vim .env.prod

# MUST configure:
# - JWT_SECRET
# - ENCRYPTION_KEY
# - POSTGRES_PASSWORD
# - REDIS_PASSWORD
# - MINIO_ROOT_PASSWORD
\`\`\`

### 3. Configure Registry Access (If Private)

\`\`\`bash
# Login to private registry
docker login ${REGISTRY}

# Or configure insecure registry (if HTTP)
sudo vim /etc/docker/daemon.json
{
  "insecure-registries": ["${REGISTRY}"]
}
sudo systemctl restart docker
\`\`\`

### 4. Pull Images

\`\`\`bash
# Pull all images from registry
docker compose -f docker-compose.yml pull
\`\`\`

### 5. Start Services

\`\`\`bash
# Start infrastructure
bash scripts/infra/up.sh

# Start application services
docker compose -f docker-compose.yml --env-file .env.prod up -d
\`\`\`

### 6. Verify Deployment

\`\`\`bash
# Check service status
docker compose -f docker-compose.yml ps

# Check logs
docker compose -f docker-compose.yml logs -f
\`\`\`

## Updating Deployment

\`\`\`bash
# Pull new images
docker compose -f docker-compose.yml pull

# Restart services (zero downtime)
docker compose -f docker-compose.yml up -d
\`\`\`

## Architecture

**Registry deployment workflow**:
1. Images pre-built on build machine
2. Images pushed to registry (${REGISTRY})
3. Server pulls images from registry
4. Deploy immediately

**Advantages**:
- Fast deployment (no compilation)
- No build tools required on server
- Consistent builds across environments

**Disadvantages**:
- Requires registry access
- Registry must be maintained separately
EOF
    fi

    echo -e "${GREEN}✓ README.md generated${NC}"
}

# Function: Create DEPLOY_INFO.txt
create_deploy_info() {
    cat > "$OUTPUT_DIR/DEPLOY_INFO.txt" << EOF
ADDP Deployment Package
========================

Mode: ${MODE}
Generated: $(date)
Registry: ${REGISTRY}
Builder: $(whoami)
Host: $(hostname)
Git Branch: $(git branch --show-current 2>/dev/null || echo "unknown")
Git Commit: $(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

Package Contents:
- docker-compose.infra.yml (infrastructure)
- docker-compose.yml (application)
- .env.example (environment template)
- scripts/infra/ (initialization scripts)
- scripts/prod/ (production scripts)
- business/ (business infrastructure)
$(if [ "$MODE" = "images" ]; then echo "- images/ (pre-built Docker images, ~2-5GB)"; echo "- load-images.sh (image loader script)"; fi)

Deployment Instructions:
See README.md for detailed deployment steps.
EOF
    echo -e "${GREEN}✓ DEPLOY_INFO.txt created${NC}"
}

# Function: Create tarball (images mode only)
create_tarball() {
    if [ "$MODE" = "images" ]; then
        echo -e "${YELLOW}Creating tarball...${NC}"
        TARBALL="dist/addp-deploy-${MODE}-${TIMESTAMP}.tar.gz"
        tar -czf "$TARBALL" -C "dist/" "$(basename $OUTPUT_DIR)"
        echo -e "${GREEN}✓ Tarball created: ${TARBALL}${NC}"
        echo ""
        echo -e "Tarball location: ${GREEN}${TARBALL}${NC}"

        # Display tarball size
        local size=$(du -h "$TARBALL" | cut -f1)
        echo -e "Tarball size: ${GREEN}${size}${NC}"
    else
        echo -e "${YELLOW}Skipping tarball creation (registry mode)${NC}"
    fi
}

# Function: Display package contents
display_contents() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}Package Contents${NC}"
    echo -e "${BLUE}========================================${NC}"

    if command -v tree &> /dev/null; then
        tree -L 3 "$OUTPUT_DIR"
    else
        find "$OUTPUT_DIR" -type f -o -type d | sort | sed "s|$OUTPUT_DIR|.|"
    fi
}

# Function: Transfer to server (if specified)
transfer_to_server() {
    if [ -z "$SERVER" ]; then
        return 0
    fi

    echo ""
    echo -e "${YELLOW}Transferring to server ${SERVER}...${NC}"

    # Detect local IP for registry access (images mode only)
    if [ "$MODE" = "images" ]; then
        LOCAL_IP=$(ip addr show 2>/dev/null | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | cut -d/ -f1 | head -1)

        if [ -z "$LOCAL_IP" ]; then
            # Fallback for macOS
            LOCAL_IP=$(ifconfig 2>/dev/null | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | head -1)
        fi

        if [ -n "$LOCAL_IP" ]; then
            echo -e "${GREEN}✓ Detected local IP: ${LOCAL_IP}${NC}"
            echo "REGISTRY_HOST=${LOCAL_IP}" > "$OUTPUT_DIR/.registry_config"
        fi
    fi

    # Transfer via rsync or scp
    echo -e "${YELLOW}Transferring files...${NC}"
    if command -v rsync &> /dev/null; then
        if rsync -avz --progress "$OUTPUT_DIR/" "$SERVER:~/addp-deploy/"; then
            echo -e "${GREEN}✓ Files transferred successfully (rsync)${NC}"
        else
            echo -e "${RED}✗ Transfer failed${NC}"
            exit 1
        fi
    else
        if scp -r "$OUTPUT_DIR" "$SERVER:~/addp-deploy"; then
            echo -e "${GREEN}✓ Files transferred successfully (scp)${NC}"
        else
            echo -e "${RED}✗ Transfer failed${NC}"
            exit 1
        fi
    fi

    echo ""
    echo -e "${GREEN}Next steps:${NC}"
    echo "1. SSH into server: ssh $SERVER"
    echo "2. Follow deployment instructions in README.md"
    if [ "$MODE" = "images" ]; then
        echo "3. Load images: ./load-images.sh"
        echo "4. Start infrastructure: bash scripts/infra/up.sh"
    else
        echo "3. Pull images: docker compose -f docker-compose.yml pull"
    fi
    echo "5. Start services: docker compose -f docker-compose.yml --env-file .env.prod up -d"
}

# Main execution
main() {
    # Clean and create dist directory
    mkdir -p dist

    # Step 1: Copy common files
    copy_common_files

    # Step 2: Mode-specific operations
    if [ "$MODE" = "images" ]; then
        # Export images and create load script
        export_images
        create_load_script
    fi

    # Step 3: Update registry URLs (both modes)
    update_registry_url

    # Step 4: Generate README
    generate_readme

    # Step 5: Create DEPLOY_INFO
    create_deploy_info

    # Step 6: Create tarball (images mode only)
    create_tarball

    # Step 7: Display contents
    display_contents

    # Step 8: Transfer to server (if specified)
    transfer_to_server

    # Summary
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}✓ Packaging Complete${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "Mode: ${BLUE}${MODE}${NC}"
    echo -e "Output: ${BLUE}${OUTPUT_DIR}${NC}"
    if [ "$MODE" = "images" ]; then
        echo -e "Tarball: ${BLUE}dist/addp-deploy-${MODE}-${TIMESTAMP}.tar.gz${NC}"
    fi
    echo ""

    if [ -z "$SERVER" ]; then
        echo -e "${YELLOW}Next steps:${NC}"
        echo "1. Transfer package to server:"
        if [ "$MODE" = "images" ]; then
            echo "   scp dist/addp-deploy-${MODE}-${TIMESTAMP}.tar.gz user@server:~/"
        else
            echo "   rsync -avz ${OUTPUT_DIR}/ user@server:~/addp-deploy/"
        fi
        echo "2. Follow README.md for deployment instructions"
        echo ""
        echo "Or run with --server option to transfer automatically:"
        echo "  ./scripts/build/package.sh --mode ${MODE} --server user@host"
    fi
}

# Run main function
main
