## 最终架构方案



### 架构原则遵循确认

**1. 单一职责** ✅:

**2. 适应性（OS/架构）** ✅:

3. 清晰明了** ✅:

*4. 幂等性** ✅:

**5. 易用性** ✅:

**6. 分散配置，集中脚本** ✅:

**7. 敢于删除** ✅:


## 修订后的 scripts/ 目录结构

```
scripts/
│
├── infra/              【基础设施】✅ 已完善
│   ├── up.sh
│   ├── down.sh
│   ├── status.sh
│   └── init-*.sh
│
├── dev/                【开发环境】⚠️ 需删除 upgrade-go.sh
│   ├── start.sh
│   ├── stop.sh
│   ├── restart.sh
│   ├── modtidy.sh
│   └── ❌ upgrade-go.sh  # 删除
│
├── build/              【编译和镜像构建】⭐ 核心变更
│   ├── compile.sh      # 编译二进制（已实现参数化）✅
│   ├── build-images.sh # 构建镜像（新建，参数化）⭐
│   └── README.md       # 文档
│
├── local/              【本地部署】✅ 已完善
│   ├── start.sh
│   ├── stop.sh
│   ├── restart.sh
│   └── status.sh
│
├── prod/               【生产部署】⚠️ 简化职责
│   ├── deploy.sh       # 完整发布流程（新建）⭐
│   ├── start.sh        # 启动生产环境 ✅
│   ├── stop.sh
│   ├── health-check.sh
│   └── swarm/          # Docker Swarm 部署 ✅
│
├── setup/              【初始化】保留
├── test/               【测试】保留
├── debug/              【调试】保留
└── utils/              【工具】保留
    └── check/          # 从 build/ 移动过来
```

**需要删除的脚本**:

```bash
scripts/dev/upgrade-go.sh
scripts/build/1-build-images-multiarch.sh
scripts/build/deploy-all.sh
scripts/build/3-server-setup.sh
scripts/build/detect-dev-ip.sh
```

**需要移动的脚本**:

```bash
scripts/build/check/ → scripts/utils/check/
```

---

## build-images.sh 详细设计

### 参数定义

```bash
Usage:
  ./scripts/build/build-images.sh [OPTIONS]

Options:
  --arch <amd64|arm64|both>   架构选择（默认：自动检测本地架构）
  --tag <version>             版本标签（默认：local）
  --push                      构建后推送到 Registry
  --registry <url>            Registry 地址（默认：localhost:5001）
  --service <name>            仅构建指定服务（默认：all）
  --build-type <debug|release> 构建类型（默认：release）

Examples:
  # 场景 1: 本地开发测试（默认）
  ./build-images.sh
  # 效果: 构建本地架构镜像，标签 :local，不推送

  # 场景 2: 生产单架构发布
  ./build-images.sh --arch amd64 --tag v1.0.0 --push

  # 场景 3: 生产多架构发布
  ./build-images.sh --arch both --tag v1.0.0 --push --registry hub.docker.com/myorg

  # 场景 4: 仅构建特定服务
  ./build-images.sh --service system --tag test
```

### 核心实现逻辑

```bash
#!/bin/bash
# ADDP Docker Image Builder
# 功能：构建单架构或多架构 Docker 镜像
# 依赖：需要先运行 compile.sh 编译二进制

set -euo pipefail

# 默认值
ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
TAG="local"
PUSH=false
REGISTRY="localhost:5001"
SERVICE="all"
BUILD_TYPE="${BUILD_TYPE:-release}"
GOOS="linux"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 参数解析
while [[ $# -gt 0 ]]; do
    case $1 in
        --arch)
            ARCH="$2"
            shift 2
            ;;
        --tag)
            TAG="$2"
            shift 2
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --registry)
            REGISTRY="$2"
            shift 2
            ;;
        --service)
            SERVICE="$2"
            shift 2
            ;;
        --build-type)
            BUILD_TYPE="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# 检查二进制是否存在
check_binaries() {
    local arch=$1
    local bin_dir="${PROJECT_ROOT}/dist/${BUILD_TYPE}-${GOOS}-${arch}"
  
    if [ ! -d "$bin_dir" ]; then
        echo "错误: ${arch} 架构的二进制文件不存在"
        echo "请先运行: ./scripts/build/compile.sh --arch ${arch}"
        exit 1
    fi
}

if [[ "$ARCH" == "both" ]]; then
    check_binaries "amd64"
    check_binaries "arm64"
else
    check_binaries "$ARCH"
fi

# 构建镜像
SERVICES=(system manager meta transfer gateway orchestrator develop)
WORKERS=(meta-worker transfer-worker)

[[ "$SERVICE" != "all" ]] && SERVICES=("$SERVICE") && WORKERS=()

cd "$PROJECT_ROOT"

echo "构建 ADDP Docker 镜像"
echo "  架构: ${ARCH}"
echo "  标签: ${TAG}"
echo "  Registry: ${REGISTRY}"
echo "  推送: ${PUSH}"
echo ""

# 构建后端服务镜像
for svc in "${SERVICES[@]}"; do
    echo "构建 ${svc}..."
  
    if [[ "$ARCH" == "both" ]]; then
        # 多架构构建（使用 docker buildx）
        docker buildx build \
            --platform linux/amd64,linux/arm64 \
            --build-arg BUILD_TYPE=${BUILD_TYPE} \
            --build-arg GOOS=${GOOS} \
            -f ${svc}/backend/Dockerfile.prebuilt \
            -t ${REGISTRY}/addp-${svc}:${TAG} \
            $([ "$PUSH" = true ] && echo "--push" || echo "--load") \
            .
    else
        # 单架构构建（普通 docker build）
        docker build \
            --build-arg BUILD_ARCH=${ARCH} \
            --build-arg BUILD_TYPE=${BUILD_TYPE} \
            --build-arg GOOS=${GOOS} \
            -f ${svc}/backend/Dockerfile.prebuilt \
            -t ${REGISTRY}/addp-${svc}:${TAG} \
            .
      
        # 推送（如果需要）
        if [ "$PUSH" = true ]; then
            docker push ${REGISTRY}/addp-${svc}:${TAG}
        fi
    fi
done

# 构建 Worker 镜像
for worker in "${WORKERS[@]}"; do
    svc_name="${worker%-worker}"
    echo "构建 ${worker}..."
  
    if [[ "$ARCH" == "both" ]]; then
        docker buildx build \
            --platform linux/amd64,linux/arm64 \
            --build-arg BUILD_TYPE=${BUILD_TYPE} \
            --build-arg GOOS=${GOOS} \
            -f ${svc_name}/backend/Dockerfile.prebuilt.worker \
            -t ${REGISTRY}/addp-${worker}:${TAG} \
            $([ "$PUSH" = true ] && echo "--push" || echo "--load") \
            .
    else
        docker build \
            --build-arg BUILD_ARCH=${ARCH} \
            --build-arg BUILD_TYPE=${BUILD_TYPE} \
            --build-arg GOOS=${GOOS} \
            -f ${svc_name}/backend/Dockerfile.prebuilt.worker \
            -t ${REGISTRY}/addp-${worker}:${TAG} \
            .
      
        if [ "$PUSH" = true ]; then
            docker push ${REGISTRY}/addp-${worker}:${TAG}
        fi
    fi
done

echo ""
echo "✅ 镜像构建完成"
docker images | grep "addp.*:${TAG}" | head -20
```

---

## prod/deploy.sh 设计

**职责**: 编排完整的生产发布流程（调用 build/ 脚本）

```bash
#!/bin/bash
# ADDP Production Deployment Script
# 完整的生产发布流程：编译 → 镜像 → 推送 → 部署

set -euo pipefail

VERSION="${1:-latest}"
REGISTRY="${REGISTRY:-hub.docker.com/myorg}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "========================================="
echo "ADDP 生产发布流程"
echo "========================================="
echo "版本: ${VERSION}"
echo "Registry: ${REGISTRY}"
echo ""

# 步骤 1: 编译多架构二进制
echo "步骤 1/3: 编译多架构二进制..."
cd "$ROOT_DIR"
./scripts/build/compile.sh --arch both

# 步骤 2: 构建并推送多架构镜像
echo ""
echo "步骤 2/3: 构建并推送多架构镜像..."
./scripts/build/build-images.sh \
    --arch both \
    --tag ${VERSION} \
    --push \
    --registry ${REGISTRY}

# 步骤 3: 部署到生产服务器
echo ""
echo "步骤 3/3: 部署到生产服务器..."

# 这里可以是多种部署方式，根据实际情况选择
if command -v kubectl &> /dev/null; then
    # Kubernetes 部署
    kubectl set image deployment/addp-system addp-system=${REGISTRY}/addp-system:${VERSION}
elif docker info | grep -q "Swarm: active"; then
    # Docker Swarm 部署
    docker stack deploy -c docker-compose.prod.yml addp
else
    # Docker Compose 部署
    VERSION=${VERSION} REGISTRY=${REGISTRY} docker-compose -f docker-compose.prod.yml up -d
fi

echo ""
echo "✅ 生产发布完成！"
echo ""
echo "验证部署:"
echo "  docker images | grep ${VERSION}"
echo "  curl http://localhost:8080/health"
```

**使用方式**:

```bash
# 发布 v1.0.0
./scripts/prod/deploy.sh v1.0.0

# 发布 latest
./scripts/prod/deploy.sh

# 使用自定义 Registry
REGISTRY=myregistry.com ./scripts/prod/deploy.sh v1.0.0
```

---

## 完整的使用流程示例

### 场景 1: 开发者本地测试（最常用）

```bash
cd /path/to/addp

# 1. 编译本地架构二进制
./scripts/build/compile.sh

# 2. 构建本地测试镜像
./scripts/build/build-images.sh

# 3. 启动测试
docker run -p 8080:8080 localhost:5001/addp-system:local

# 4. 验证
curl http://localhost:8080/health
```

**特点**: 快速，单架构，仅本地使用

---

### 场景 2: CI/CD 自动化发布

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
    
      - name: Compile multi-arch binaries
        run: ./scripts/build/compile.sh --arch both
    
      - name: Build and push multi-arch images
        env:
          REGISTRY: ghcr.io/${{ github.repository }}
        run: |
          ./scripts/build/build-images.sh \
            --arch both \
            --tag ${{ github.ref_name }} \
            --push \
            --registry $REGISTRY
    
      - name: Deploy to production
        run: ./scripts/prod/deploy.sh ${{ github.ref_name }}
```

---

### 场景 3: 手动生产发布

```bash
# 方式 1: 使用 deploy.sh 一键完成
./scripts/prod/deploy.sh v1.0.0

# 方式 2: 分步执行（更灵活）
# 步骤 1: 编译
./scripts/build/compile.sh --arch both

# 步骤 2: 构建并推送镜像
./scripts/build/build-images.sh \
  --arch both \
  --tag v1.0.0 \
  --push \
  --registry hub.docker.com/myorg

# 步骤 3: 在生产服务器上部署
ssh production-server
docker pull hub.docker.com/myorg/addp-system:v1.0.0
docker-compose up -d
```

---

## 与 Makefile 的集成

```makefile
# Makefile - 简化为调用 scripts/

# 编译
build-backend:
	@./scripts/build/compile.sh

build-backend-multiarch:
	@./scripts/build/compile.sh --arch both

# 镜像构建
build-images:
	@./scripts/build/build-images.sh

build-images-prod:
	@./scripts/build/build-images.sh --arch both --tag latest

# 生产发布
prod-release:
	@./scripts/prod/deploy.sh $(VERSION)

# 快速本地测试
local-test: build-backend build-images
	@docker run -p 8080:8080 localhost:5001/addp-system:local
```

**使用**:

```bash
# 本地开发
make local-test

# 生产发布
make prod-release VERSION=v1.0.0
```

---

## 架构优势总结

### ✅ 1. 遵循单一职责原则


| 职责       | 位置                           | 实现方式                     |
| ---------- | ------------------------------ | ---------------------------- |
| 编译二进制 | `build/compile.sh`             | 单一脚本，参数化             |
| 构建镜像   | `build/build-images.sh`        | 单一脚本，参数化             |
| 推送镜像   | `build/build-images.sh --push` | 集成到构建脚本               |
| 编排发布   | `prod/deploy.sh`               | 调用 build/ 脚本，不重复实现 |

**没有重复逻辑！**

### ✅ 2. 一致的用户体验

开发者只需记住两个脚本，使用完全相同的参数模式：

```bash
./scripts/build/compile.sh [--arch <arch>] [--force]
./scripts/build/build-images.sh [--arch <arch>] [--tag <tag>] [--push]
```

### ✅ 3. 灵活性

* **开发模式**: 不带参数 → 本地架构，快速迭代
* **生产模式**: `--arch both --push` → 多架构，推送 Registry
* **调试模式**: `--service system` → 仅构建单个服务

### ✅ 4. 避免代码重复

* prod/ 不再重复实现镜像构建逻辑
* 所有镜像构建通过 `build-images.sh` 完成
* prod/ 仅负责高层编排

---

## 实施计划

### 阶段 1: 清理冗余脚本（30 分钟）

```bash
cd /path/to/addp

# 删除冗余脚本
rm scripts/dev/upgrade-go.sh
rm scripts/build/1-build-images-multiarch.sh
rm scripts/build/deploy-all.sh
rm scripts/build/3-server-setup.sh
rm scripts/build/detect-dev-ip.sh

# 移动错放的脚本
mkdir -p scripts/utils/check
mv scripts/build/check/* scripts/utils/check/ || true
rmdir scripts/build/check 2>/dev/null || true

git status
```

### 阶段 2: 创建 build-images.sh（2 小时）

* 实现上述参数化逻辑
* 支持单架构和多架构
* 支持推送到 Registry
* 添加完善的错误处理和日志

### 阶段 3: 创建 prod/deploy.sh（1 小时）

* 编排完整发布流程
* 调用 build/ 脚本
* 支持多种部署方式（Compose/Swarm/K8s）

### 阶段 4: 更新文档（1 小时）

* 创建 `scripts/build/README.md`
* 更新 `CLAUDE.md` 中的 scripts/ 架构说明
* 添加使用示例

### 阶段 5: 测试验证（1 小时）

```bash
# 测试 1: 本地单架构构建
./scripts/build/compile.sh
./scripts/build/build-images.sh
docker images | grep ":local"

# 测试 2: 多架构构建（不推送）
./scripts/build/compile.sh --arch both
./scripts/build/build-images.sh --arch both --tag test
docker images | grep ":test"

# 测试 3: 完整发布流程（使用本地 Registry）
./scripts/prod/deploy.sh test-v1.0.0
```

**总工作量**: 约 5-6 小时（1 个工作日）

---

## 关键文件清单

### 需要删除的文件

```
scripts/dev/upgrade-go.sh
scripts/build/1-build-images-multiarch.sh
scripts/build/deploy-all.sh
scripts/build/3-server-setup.sh
scripts/build/detect-dev-ip.sh
scripts/build/check/          (目录移动到 utils/)
```

### 需要创建的文件

```
scripts/build/build-images.sh      (新建，核心脚本)
scripts/build/README.md             (新建，文档)
scripts/prod/deploy.sh              (新建，发布编排)
```

### 需要移动的文件

```
scripts/build/check/*  →  scripts/utils/check/
```

---

## 最终确认

用户确认的方案：

1. ✅ **删除 upgrade-go.sh** - 同意删除，以后需要再说
2. ✅ **统一镜像构建到 build/** - 默认本地架构，参数支持多架构
3. ✅ **与 compile.sh 一致的参数模式** - 确认编译也是这样操作的
4. ✅ **其他内容按建议实施** - setup/, test/, debug/, utils/ 保持现状

**核心优势**:

* 单一职责（镜像构建只在一处）
* 零重复（prod 调用 build，不重复实现）
* 一致体验（compile 和 build-images 参数模式相同）
