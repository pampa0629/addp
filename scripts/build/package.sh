#!/bin/bash
# =============================================================================
# ADDP Deployment Packager
# =============================================================================
# Description: Package deployment files for offline or registry-based deployment
# Usage: ./package.sh [OPTIONS]
# Options:
#   --mode MODE               Deployment mode: images|registry (default: images)
#   --server SERVER_USER@IP   Automatically transfer to server via rsync
# =============================================================================

set -euo pipefail

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default values
MODE="images"
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
        --server)
            SERVER="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Usage: $0 [--mode images|registry] [--server user@host]"
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
echo "Release: dist/compose-release/manifest.json"
if [ -n "$SERVER" ]; then
    echo -e "Target Server: ${GREEN}${SERVER}${NC}"
fi
echo ""

# Export deployment files from the source of the prepared release, independently
# of local configuration, ignored files and changes to the build workspace.
copy_common_files() {
    local release_dir="dist/compose-release" path
    if [ ! -f "$release_dir/manifest.json" ]; then
        echo "Prepare a Compose release with scripts/prod/deploy.sh --skip-deploy before packaging" >&2
        exit 1
    fi

    local identity
    identity=$(python3 - "$release_dir/manifest.json" <<'PYCOMMIT'
import json, re, sys

with open(sys.argv[1]) as stream:
    manifest = json.load(stream)
commit = manifest.get("source_commit", "")
if manifest.get("schema") != "addp.compose-release/v1" or not re.fullmatch(r"[0-9a-f]{40}", commit):
    sys.exit("Invalid Compose release manifest or source commit")
platform = manifest.get("platform", "")
if platform not in ("linux/amd64", "linux/arm64"):
    sys.exit("Invalid Compose release platform")
print(commit, platform.split("/")[1])
PYCOMMIT
)
    read -r SOURCE_COMMIT TARGET_ARCH <<< "$identity"
    git cat-file -e "$SOURCE_COMMIT^{commit}"

    # The collector image fingerprints include these build inputs even on
    # installation hosts, where the release only consumes prebuilt images.
    local paths=(
        .env.example docker-compose.infra.yml docker-compose.yml docker-compose.runtimes.yml
        scripts/infra scripts/prod scripts/dev/lifecycle-lock.sh scripts/dev/process-metrics-launch.py
        scripts/utils/observability-env.sh scripts/utils/deployment-inventory.py
        scripts/utils/container-proof.py scripts/utils/deployment-docker.py
        scripts/utils/metrics-project.py scripts/utils/metrics-source.py scripts/utils/private-state.py
        common gateway/go.mod monitor/backend/go.sum
        monitor/backend/cmd/minio-metrics-source monitor/backend/cmd/redis-metrics-source
        monitor/backend/cmd/cdc-metrics-source monitor/backend/internal/cdcsource
        monitor/backend/internal/miniosource monitor/backend/internal/redissource monitor/backend/internal/sourceserver
        business nginx/nginx.conf "docs/guide/addp IAM三员初始化操作指南.md"
    )
    while IFS= read -r path; do paths+=("$path"); done < <(
        git ls-tree -r --name-only "$SOURCE_COMMIT" | awk -F/ 'NF == 3 && $2 == "backend" && $3 == "go.mod"'
    )
    git archive --format=tar "$SOURCE_COMMIT" -- "${paths[@]}" | tar -xf - -C "$OUTPUT_DIR"

    mkdir -p "$OUTPUT_DIR/$release_dir"
    cp "$release_dir/"{manifest.json,infra-images.yml,platform-images.yml,runtimes-images.yml} "$OUTPUT_DIR/$release_dir/"
    python3 - "$OUTPUT_DIR" <<'PYINPUTS'
import hashlib, json, sys
from pathlib import Path

root = Path(sys.argv[1])
manifest = json.loads((root / "dist/compose-release/manifest.json").read_text())
for name, digest in manifest["inputs"].items():
    path = (root / name).resolve()
    if root.resolve() not in path.parents or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
        sys.exit(f"Packaged input differs from the prepared release: {name}")
PYINPUTS
    echo "Exported deployment files from $SOURCE_COMMIT"
}

build_iam_bootstrap() (
    local build_dir
    build_dir=$(mktemp -d)
    trap 'rm -rf -- "$build_dir"' EXIT
    git archive --format=tar "$SOURCE_COMMIT" -- Makefile common system/backend | tar -xf - -C "$build_dir"
    GOWORK=off make -C "$build_dir" build-iam-bootstrap GOOS=linux GOARCH="$TARGET_ARCH" \
        LOCAL_GOMODCACHE="$PROJECT_ROOT/.gomodcache" LOCAL_GOPATH="$PROJECT_ROOT/.gopath" \
        LOCAL_GOCACHE="$PROJECT_ROOT/.cache/go-build"
    mkdir "$OUTPUT_DIR/tools"
    install -m 0755 "$build_dir/dist/release-linux-$TARGET_ARCH/addp-iam-bootstrap" "$OUTPUT_DIR/tools/"
)

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
assert all(re.fullmatch(r'sha256:[0-9a-f]{64}', ref) for ref in references)
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
    echo "2. Start the prepared release: bash scripts/prod/start.sh"
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

本包包含固定提交的部署文件、镜像清单和 Compose 覆盖文件。源提交、目标架构、能力选择及镜像 digest 见 `dist/compose-release/manifest.json`。站点配置、凭据和备份单独交付。

目标 Linux 主机准备 Docker Engine、Docker Compose、Bash、Python 3、OpenSSL、curl 和 sha256sum。现场无需源码检出或编译工具。

首次安装：

1. 有归档外的 `.sha256` 时先校验归档。解包后进入含本 README 的部署根目录，执行 `sha256sum -c SHA256SUMS`。
2. 镜像归档模式执行 `bash load-images.sh`。仓库模式准备清单中仓库的访问权限和 CA，启动预检将按 digest 拉取。
3. 安装站点独立提供的根 `.env`（权限 0600），或在空库首次安装时执行 `bash scripts/prod/setup-env.sh` 生成配置和随机密钥，再填写站点参数。核对 `ADDP_PUBLIC_ORIGIN`、监听地址及与发布清单一致的能力选择。启用集中日志时须显式设置本节点的 `ADDP_HOST_NODE_NAME`，并配置独立的 `LOG_OBSERVER_SERVICE_CLIENT_SECRET`；首次配置入口在模板启用日志时生成该凭据。保管此文件和启动后生成的模块声明发布凭据。
4. 在部署根目录执行 `bash scripts/prod/start.sh` 和 `bash scripts/prod/health-check.sh`。选中集中日志时，另用 `docker inspect --format '{{.State.Status}}' addp-runtime-log-observer` 确认观测器为 `running`。重复启动仍使用同一入口和站点配置。
5. 全新站点首次登录前，使用包内 `tools/addp-iam-bootstrap prepare` 和 `tools/addp-iam-bootstrap apply --manifest /站点私有目录/三员清单.json` 完成一次性三员初始化。从部署根目录在直接终端交互执行，凭据和验证码按 [IAM 指南](docs/guide/addp%20IAM三员初始化操作指南.md) 准备。包内已提供 CLI，跳过指南中的源码构建步骤。部署根 `.env` 中的 PostgreSQL 宿主机地址和端口须对应实际 Infra 映射。
6. 从实际入口登录并核对项目业务。

带数据升级：

1. 在独立暂存目录校验下一发布并导入新包镜像。核对目标版本的迁移说明，保存原发布包、站点配置与部署身份。
2. 从原部署根目录执行 `bash scripts/prod/stop.sh`，保留 Infra 和数据卷。将已校验的新包文件解入**同一部署根目录**，保留 `.env`、`.dev-state` 和已有站点数据路径。按新版本要求核对配置，沿用原密钥。
3. 在新版启动及任何新迁移之前，按下文完成备份和恢复演练。随后执行新包的生产启动、健康检查及业务验收。应用镜像切回旧版不能撤销数据库迁移；失败时按已验证的备份和恢复计划处理。

备份与恢复演练：

备份前须在宿主机准备可读取来源 PostgreSQL dump 格式的客户端 `pg_restore`。在部署根目录执行 `python3 scripts/infra/backup.py create --output-root /绝对路径/私有备份目录`，记录输出的备份路径。执行 `python3 scripts/infra/backup.py drill --backup-dir /绝对路径/本次备份` 验证 PostgreSQL 和 Infra MinIO 在独占临时容器中的真实恢复。该演练不回写现有服务或卷。实际灾备范围还包括单独保存的部署密钥、外接 Business 引擎和其他有状态依赖。
EOF
}

# Function: Record package information
create_deploy_info() {
    cat > "$OUTPUT_DIR/DEPLOY_INFO.txt" << EOF
ADDP Deployment Package
========================

Mode: ${MODE}
Generated: $(date)
Release: dist/compose-release/manifest.json
$(python3 - "$OUTPUT_DIR/dist/compose-release/manifest.json" <<'PYINFO'
import json, sys
manifest = json.load(open(sys.argv[1]))
for key in ("version", "registry", "platform", "source_commit"):
    print(key + ": " + manifest[key])
PYINFO
)
Builder: $(whoami)
Host: $(hostname)

Package Contents:
- docker-compose.infra.yml (infrastructure)
- docker-compose.yml (platform)
- docker-compose.runtimes.yml (built-in runtimes)
- .env.example (environment template)
- scripts/infra/ (initialization scripts)
- scripts/prod/ (production scripts)
- business/ (business infrastructure)
$(if [ "$MODE" = "images" ]; then echo "- images/release.tar (all selected immutable image IDs)"; echo "- load-images.sh (image loader script)"; fi)

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
        tar -cf - -C dist "$(basename "$OUTPUT_DIR")" | gzip -1 > "$TARBALL.partial"
        mv "$TARBALL.partial" "$TARBALL"
        (cd dist && sha256sum "$(basename "$TARBALL")" > "$(basename "$TARBALL").sha256")
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
    mkdir "$OUTPUT_DIR"

    # Step 1: Copy common files
    copy_common_files
    build_iam_bootstrap

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
    (cd "$OUTPUT_DIR" && find . -type f ! -path './SHA256SUMS' -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS)

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
