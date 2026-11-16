#!/bin/bash
# =============================================================================
# ADDP Deployment Packager
# =============================================================================
# Description: Package deployment files for offline or registry-based deployment
# Usage: ./package.sh [OPTIONS]
# Options:
#   --mode MODE               Deployment mode: images|registry (default: images)
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
    mkdir -p "$OUTPUT_DIR/scripts"

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
    if [ -f "docker-compose.runtimes.yml" ]; then
        cp docker-compose.runtimes.yml "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ docker-compose.runtimes.yml copied${NC}"
    else
        echo -e "${RED}Error: docker-compose.runtimes.yml not found${NC}"
        exit 1
    fi

    # A deployment package carries the selected immutable release, independent of
    # the build checkout's historical release directories and current symlink.
    if [ ! -f dist/compose-release/manifest.json ]; then
        echo "Prepare a Compose release with scripts/prod/deploy.sh --skip-deploy before packaging" >&2
        exit 1
    fi
    mkdir -p "$OUTPUT_DIR/dist/compose-release"
    cp -R dist/compose-release/. "$OUTPUT_DIR/dist/compose-release/"

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

    # 3. Copy scripts/infra/ (CORE: infrastructure initialization)
    if [ -d "scripts/infra" ]; then
        cp -r scripts/infra "$OUTPUT_DIR/scripts/"
        echo -e "${GREEN}✓ scripts/infra/ copied${NC}"
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

    # Production helpers retain deployment proofs and source identity inputs.
    # Runtime images are consumed from the fixed release manifest.
    local path parent
    for path in \
        scripts/dev/lifecycle-lock.sh scripts/dev/process-metrics-launch.py \
        scripts/utils/observability-env.sh scripts/utils/deployment-inventory.py \
        scripts/utils/container-proof.py scripts/utils/deployment-docker.py \
        scripts/utils/metrics-project.py scripts/utils/metrics-source.py scripts/utils/private-state.py \
        common gateway/go.mod */backend/go.mod monitor/backend/go.sum \
        monitor/backend/cmd/minio-metrics-source monitor/backend/cmd/redis-metrics-source \
        monitor/backend/cmd/cdc-metrics-source monitor/backend/internal/cdcsource \
        monitor/backend/internal/miniosource monitor/backend/internal/redissource monitor/backend/internal/sourceserver; do
        parent="$OUTPUT_DIR/$(dirname "$path")"
        mkdir -p "$parent"
        cp -R "$path" "$parent/"
    done
    echo -e "${GREEN}✓ Production lifecycle and collector build inputs copied${NC}"

    # 5. Copy business infrastructure (recommended)
    if [ -d "business" ]; then
        cp -r business "$OUTPUT_DIR/"
        echo -e "${GREEN}✓ business/ copied (business infrastructure)${NC}"
    else
        echo -e "${YELLOW}Warning: business/ directory not found${NC}"
    fi

    # 6. Copy nginx configuration (if exists)
    if [ -f "nginx/nginx.conf" ]; then
        mkdir -p "$OUTPUT_DIR/nginx"
        cp nginx/nginx.conf "$OUTPUT_DIR/nginx/"
        echo -e "${GREEN}✓ nginx/nginx.conf copied${NC}"
    else
        echo -e "${YELLOW}Warning: nginx/nginx.conf not found${NC}"
    fi
}

# Function: Export Docker images to tar files
export_images() {
    local images_dir="$OUTPUT_DIR/images"
    mkdir -p "$images_dir"
    python3 - "$OUTPUT_DIR/dist/compose-release/manifest.json" > "$images_dir/references.txt" <<'PYIMAGES'
import json, re, sys
manifest = json.load(open(sys.argv[1]))
assert manifest['schema'] == 'addp.compose-release/v1'
references = set(manifest['image_ids'].values())
assert references
assert all(re.fullmatch(r'.+@sha256:[0-9a-f]{64}', ref) for ref in references)
print('\n'.join(sorted(references)))
PYIMAGES
    local images=() reference
    while IFS= read -r reference; do images+=("$reference"); done < "$images_dir/references.txt"
    docker save -o "$images_dir/release.tar.partial" "${images[@]}"
    mv "$images_dir/release.tar.partial" "$images_dir/release.tar"
    echo "Exported ${#images[@]} fixed release images"
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
    echo "1. Configure environment: bash scripts/prod/setup-env.sh && vim .env"
    echo "2. Start infrastructure: bash scripts/infra/up.sh"
    echo "3. Start services: bash scripts/prod/start.sh"
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

# Function: Describe the prepared deployment package
generate_readme() {
    cat > "$OUTPUT_DIR/README.md" <<'EOF'
# ADDP Compose 发布包

本包包含固定镜像清单和 Compose 覆盖文件。源提交、目标架构及镜像 digest 见 dist/compose-release/manifest.json。

目标机器需要可用的 Docker Engine、Docker Compose 和 Python 3。已有部署沿用目标环境的 .env 和数据卷，首次部署由标准入口准备配置。

镜像归档模式先在包目录执行 ./load-images.sh。仓库模式由生产预检按清单 digest 取得镜像，需要能够访问清单中的仓库。

随后执行 bash scripts/prod/start.sh。入口核对所选能力和 Compose 输入，按固定镜像启动 Infra、System、发布模块声明，再启动平台与 Runtime。缺失制品时停止启动。

本包可能保留打包时已有的 .env，请按部署凭据管理。镜像清单不包含密码或令牌。

停止入口为 bash scripts/prod/stop.sh，保留持久数据卷。状态和业务验收按 scripts/prod/README.md 与平台测试规范执行。
EOF
}

# Function: Record package information
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
- docker-compose.yml (platform)
- docker-compose.runtimes.yml (built-in runtimes)
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
    fi
    echo "4. Start the prepared release: bash scripts/prod/start.sh"
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

    # Fixed registry references are supplied by the release manifest.

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
