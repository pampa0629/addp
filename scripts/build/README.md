# Build Scripts 使用指南

本目录包含 ADDP 项目的编译和镜像构建脚本，遵循单一职责和参数化原则。

## 目录

- [compile.sh](#compilesh) - 二进制编译器
- [build-images.sh](#build-imagessh) - Docker 镜像构建器
- [push-images.sh](#push-imagessh) - 镜像推送工具
- [package.sh](#packagesh) - 部署包生成器

---

## compile.sh

**用途**: 编译所有 ADDP 后端服务的二进制文件（带智能缓存）

### 使用方法

```bash
# 容器构建（默认，编译 Linux 二进制用于 Docker）
./scripts/build/compile.sh

# 本地开发（编译本机操作系统的可执行文件）
./scripts/build/compile.sh --local

# 编译多架构（amd64 + arm64，用于容器）
./scripts/build/compile.sh --arch both

# 强制重新编译（忽略缓存）
./scripts/build/compile.sh --force

# 组合使用
./scripts/build/compile.sh --local --force
```

### 参数说明

| 参数 | 值 | 说明 |
|------|------|------|
| `--arch` | `amd64` \| `arm64` \| `both` | 目标架构（默认：自动检测本机架构） |
| `--local` | - | 本地开发模式，编译本机 OS 可执行文件（macOS/Linux/Windows） |
| `--force` | - | 强制重新编译，忽略缓存 |

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `BUILD_TYPE` | `release` | 构建类型（`release` 或 `debug`） |
| `GOOS` | `linux` | 目标操作系统（使用 `--local` 时自动检测） |

### 输出目录结构

```
dist/
├── release-linux-amd64/        # 容器构建（Linux + AMD64）
│   ├── system                   # System 后端
│   ├── gateway                  # API Gateway
│   ├── manager-backend          # Manager 后端
│   ├── manager-worker           # Manager Worker
│   ├── meta-backend             # Meta 后端
│   ├── meta-worker              # Meta Worker
│   ├── transfer-backend         # Transfer 后端
│   ├── transfer-worker          # Transfer Worker
│   ├── orchestrator-backend     # Orchestrator 后端
│   └── develop-backend          # Develop 后端
│
├── release-linux-arm64/        # 容器构建（Linux + ARM64）
│   └── (同上)
│
└── release-darwin-arm64/       # 本地构建（macOS Apple Silicon）
    └── (同上，使用 --local 时生成)
```

### 智能缓存机制

- 自动检测源代码修改时间
- 仅重新编译变更的服务
- 使用 `.compile-cache/` 存储缓存元数据
- 使用 `--force` 可跳过缓存检查

### 示例

```bash
# 场景 1: 容器构建（默认，用于 Docker）
./scripts/build/compile.sh

# 场景 2: 本地开发调试（macOS 上直接运行）
./scripts/build/compile.sh --local
# 输出: dist/release-darwin-arm64/system (可直接运行)

# 场景 3: 生产发布（多架构容器）
./scripts/build/compile.sh --arch both

# 场景 4: 调试构建（本地 + 调试符号）
BUILD_TYPE=debug ./scripts/build/compile.sh --local

# 场景 5: 清理缓存重新编译
./scripts/build/compile.sh --force
```

### 使用场景对比

| 场景 | 命令 | 输出 | 用途 |
|------|------|------|------|
| **容器构建** | `./scripts/build/compile.sh` | `dist/release-linux-{arch}/` | 用于 Docker 镜像构建 |
| **本地开发** | `./scripts/build/compile.sh --local` | `dist/release-{os}-{arch}/` | macOS/Linux 上直接运行调试 |
| **多架构发布** | `./scripts/build/compile.sh --arch both` | Linux amd64 + arm64 | 生产环境跨平台部署 |

---

## build-images.sh

**用途**: 构建 ADDP 服务的 Docker 镜像（支持单架构和多架构）

### 使用方法

```bash
# 基本用法（构建本机架构镜像）
./scripts/build/build-images.sh

# 构建多架构镜像（amd64 + arm64）
./scripts/build/build-images.sh --multi-arch

# 指定 Registry
./scripts/build/build-images.sh --registry harbor.example.com:5001

# 禁用构建缓存
./scripts/build/build-images.sh --skip-cache

# 仅构建特定服务
./scripts/build/build-images.sh --services system-backend,manager-backend

# 组合使用（生产场景）
IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh \
  --multi-arch \
  --registry myregistry.com:5001
```

### 参数说明

| 参数 | 值 | 说明 |
|------|------|------|
| `--registry` | URL | Registry 地址（默认：`localhost:5001`） |
| `--multi-arch` | - | 构建多架构镜像（amd64 + arm64） |
| `--skip-cache` | - | 禁用 Docker 构建缓存 |
| `--services` | 服务列表 | 逗号分隔的服务名（默认：`all`） |

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `REGISTRY` | `localhost:5001` | Registry 地址 |
| `IMAGE_TAG` | `latest` | 镜像标签 |
| `BUILD_TYPE` | `release` | 构建类型（需与 compile.sh 一致） |

### 构建的服务列表

**后端服务**:
- `system-backend` - System 后端
- `manager-backend` - Manager 后端
- `meta-backend` - Meta 后端
- `transfer-backend` - Transfer 后端
- `orchestrator-backend` - Orchestrator 后端
- `develop-backend` - Develop 后端
- `gateway` - API Gateway

**Worker 服务**:
- `meta-worker` - Meta Worker
- `transfer-worker` - Transfer Worker
- `manager-worker` - Manager Worker

**前端服务**:
- `portal-frontend` - Portal 前端
- `system-frontend` - System 前端
- `manager-frontend` - Manager 前端
- `meta-frontend` - Meta 前端
- `transfer-frontend` - Transfer 前端
- `orchestrator-frontend` - Orchestrator 前端
- `develop-frontend` - Develop 前端

### 前置条件

**必须先运行 compile.sh**，否则会报错：

```bash
# 错误示例
./scripts/build/build-images.sh
# ❌ 错误: amd64 架构的二进制文件不存在

# 正确流程
./scripts/build/compile.sh           # 1. 先编译
./scripts/build/build-images.sh      # 2. 再构建镜像
```

### 镜像命名规范

```
{REGISTRY}/addp-{SERVICE}:{TAG}

示例:
localhost:5001/addp-system-backend:latest
localhost:5001/addp-manager-backend:v1.0.0
localhost:5001/addp-meta-worker:latest
```

### 多架构构建说明

使用 `--multi-arch` 时：
- 需要 Docker Buildx 支持
- 构建平台：`linux/amd64,linux/arm64`
- 自动检测并使用 buildx builder
- 镜像会同时包含两个架构

### 示例

```bash
# 场景 1: 本地开发测试
./scripts/build/compile.sh
./scripts/build/build-images.sh

# 场景 2: 构建特定服务
./scripts/build/compile.sh
./scripts/build/build-images.sh --services system-backend

# 场景 3: 生产单架构发布
./scripts/build/compile.sh --arch amd64
IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh \
  --registry hub.docker.com/myorg

# 场景 4: 生产多架构发布
./scripts/build/compile.sh --arch both
IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh \
  --multi-arch \
  --registry hub.docker.com/myorg

# 场景 5: 推送到私有 Registry
./scripts/build/compile.sh --arch both
IMAGE_TAG=v1.2.0 ./scripts/build/build-images.sh \
  --multi-arch \
  --registry harbor.example.com:5001
docker push harbor.example.com:5001/addp-system-backend:v1.2.0
```

---

## push-images.sh

**用途**: 推送 Docker 镜像到远程镜像仓库（Docker Hub、Harbor、私有 Registry）

### 使用方法

```bash
# 推送到 Docker Hub
./scripts/build/push-images.sh --registry docker.io/myusername

# 推送到阿里云 ACR
./scripts/build/push-images.sh \
  --registry crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp

# 推送到 Harbor
./scripts/build/push-images.sh --registry harbor.example.com:5001/project

# 指定镜像标签
./scripts/build/push-images.sh --registry docker.io/myusername --tag v1.0.0

# 仅推送特定服务
./scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --services system-backend,manager-backend,gateway

# 干运行（不实际推送，仅显示将推送的镜像）
./scripts/build/push-images.sh --registry docker.io/myusername --dry-run
```

### 参数说明

| 参数 | 值 | 说明 |
|------|------|------|
| `--registry` | URL | **必填**，目标 Registry 地址（如 `docker.io/USERNAME`、`registry.com/namespace`） |
| `--tag` | 版本号 | 镜像标签（默认：`latest`） |
| `--services` | 服务列表 | 逗号分隔的服务名（默认：`all`，推送所有 18 个服务） |
| `--dry-run` | - | 干运行模式，仅显示将推送的镜像，不实际推送 |
| `--source-registry` | URL | 源 Registry 地址（默认：`localhost:5001`） |

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `IMAGE_TAG` | `latest` | 镜像标签 |
| `SOURCE_REGISTRY` | `localhost:5001` | 源 Registry |

### 镜像命名规则

脚本使用标准 Docker 命名格式：

**格式**: `{REGISTRY}/addp-{service}:{TAG}`

**适用场景**:
- Docker Hub
- Harbor
- 阿里云 ACR（个人版和企业版）
- 私有 Registry

**示例**:
```bash
# Docker Hub
docker.io/myusername/addp-system-backend:latest
docker.io/myusername/addp-manager-backend:latest

# 阿里云 ACR
crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp/addp-system-backend:latest
crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp/addp-manager-backend:latest

# Harbor
harbor.example.com:5001/project/addp-system-backend:latest
harbor.example.com:5001/project/addp-manager-backend:latest
```

**特点**:
- 每个服务对应一个独立的镜像仓库（repository）
- 版本通过 tag 管理（`latest`, `v1.0.0` 等）
- 符合 Docker 标准规范

### 推送的镜像列表

脚本会推送以下 18 个镜像（当使用 `--services all` 或不指定时）：

**后端服务** (7 个):
- `addp-system-backend`
- `addp-manager-backend`
- `addp-meta-backend`
- `addp-transfer-backend`
- `addp-orchestrator-backend`
- `addp-develop-backend`
- `addp-gateway`

**Worker 服务** (3 个):
- `addp-meta-worker`
- `addp-transfer-worker`
- `addp-manager-worker`

**前端服务** (6 个):
- `addp-system-frontend`
- `addp-manager-frontend`
- `addp-meta-frontend`
- `addp-transfer-frontend`
- `addp-orchestrator-frontend`
- `addp-develop-frontend`

**Portal + Nginx** (2 个):
- `addp-portal`
- `addp-nginx`

### 前置条件

1. **必须先构建镜像**（运行 `build-images.sh`）
2. **必须登录目标 Registry**：
   ```bash
   # Docker Hub
   docker login

   # Harbor 或私有 Registry
   docker login harbor.example.com:5001
   ```

### 工作原理

脚本执行以下步骤：

1. **验证参数** - 检查 `--registry` 是否提供
2. **检查 Docker 状态** - 验证 Docker 是否运行
3. **检查登录状态** - 提示用户是否已登录 Registry
4. **遍历服务列表**：
   - 检查源镜像是否存在（`localhost:5001/addp-{service}:{tag}`）
   - Tag 镜像到目标 Registry（`{target-registry}/addp-{service}:{tag}`）
   - 推送镜像到目标 Registry
   - 记录成功/失败/跳过的服务
5. **显示推送摘要** - 成功数、失败数、跳过数

### 示例

```bash
# 场景 1: 推送所有镜像到 Docker Hub
docker login
./scripts/build/push-images.sh --registry docker.io/myusername

# 场景 2: 推送到阿里云 ACR
docker login crpi-xxx.cn-beijing.personal.cr.aliyuncs.com
./scripts/build/push-images.sh \
  --registry crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp

# 场景 3: 推送特定版本到 Docker Hub
./scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --tag v1.0.0

# 场景 4: 仅推送核心服务
./scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --services system-backend,manager-backend,meta-backend,gateway

# 场景 5: 推送到私有 Harbor
docker login harbor.example.com:5001
./scripts/build/push-images.sh \
  --registry harbor.example.com:5001/addp \
  --tag v2.0.0

# 场景 6: 干运行测试（不实际推送）
./scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --dry-run
```

### Registry 地址格式

不同 Registry 的地址格式和说明：

| Registry 类型 | 格式示例 | 说明 |
|--------------|---------|------|
| **Docker Hub** | `docker.io/USERNAME` | 最常用的公共镜像仓库 |
| **Harbor** | `harbor.example.com:5001/PROJECT` | 企业级私有镜像仓库 |
| **私有 Registry** | `registry.company.com:5000` | 自建 Docker Registry |
| **GitHub Container Registry** | `ghcr.io/USERNAME` | GitHub 提供的容器镜像服务 |
| **阿里云 ACR 个人版** | `crpi-xxx.cn-region.personal.cr.aliyuncs.com/NAMESPACE` | 个人版限制 300 个仓库，ADDP 18 个服务完全够用 |
| **阿里云 ACR 企业版** | `registry.cn-hangzhou.aliyuncs.com/NAMESPACE` | 企业版无仓库数量限制 |

**说明**:
- 所有 Registry 均使用标准 Docker 命名格式：`{REGISTRY}/addp-{service}:{TAG}`
- 每个服务对应一个独立的镜像仓库（repository）
- 版本通过 tag 管理（`latest`, `v1.0.0` 等）

### 推送结果示例

```
========================================
ADDP Image Pusher
========================================

Source Registry: localhost:5001
Target Registry: docker.io/myusername
Tag: latest
Services: all
Dry Run: false

Services to push (18 total):
  - system-backend
  - manager-backend
  - meta-backend
  ...

========================================
Pushing Images
========================================

Processing system-backend...
  Tagging: localhost:5001/addp-system-backend:latest → docker.io/myusername/addp-system-backend:latest
  Pushing: docker.io/myusername/addp-system-backend:latest
✓ Pushed system-backend (245MB)

...

========================================
Summary
========================================
Total services: 18
Successfully pushed: 18
Failed: 0
Skipped (not found): 0

✓ All images pushed successfully!

Next steps:
1. Verify images in registry: docker.io/myusername
   Visit: https://hub.docker.com/r/myusername/repositories
2. Generate deployment package:
   ./scripts/build/package.sh --mode registry --registry docker.io/myusername
3. Deploy on server:
   docker compose -f docker-compose.yml pull
   bash scripts/prod/start.sh
```

**阿里云 ACR 推送示例**:

```bash
# 推送到阿里云 ACR
./scripts/build/push-images.sh \
  --registry crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp

# 结果：18 个独立仓库
# crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp/addp-system-backend:latest
# crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp/addp-manager-backend:latest
# crpi-xxx.cn-beijing.personal.cr.aliyuncs.com/addp/addp-meta-backend:latest
# ... (共 18 个仓库)
```

### 注意事项

1. **登录验证**: 推送前必须登录目标 Registry（`docker login`）
2. **网络依赖**: 推送所有 18 个镜像需要稳定的网络（总大小约 2-5GB）
3. **权限要求**: 确保 Docker Hub 或 Registry 账号有推送权限
4. **命名规范**: 镜像名称保持 `addp-` 前缀不变
5. **版本管理**: 生产环境建议使用明确的版本标签（如 `v1.0.0`），避免 `latest`
6. **推送时间**: 首次推送所有镜像可能需要 10-30 分钟（取决于网络速度）

### 故障排查

#### 问题 1: 源镜像不存在

**错误信息**:
```
⚠ Warning: Source image not found: localhost:5001/addp-system-backend:latest
```

**解决方法**:
```bash
# 先构建镜像
./scripts/build/compile.sh
./scripts/build/build-images.sh
```

#### 问题 2: 未登录 Registry

**错误信息**:
```
denied: requested access to the resource is denied
```

**解决方法**:
```bash
# Docker Hub
docker login

# Harbor 或私有 Registry
docker login harbor.example.com:5001
```

#### 问题 3: 推送超时

**解决方法**:
```bash
# 增加 Docker daemon 超时时间（在 Docker Desktop 设置中）
# 或者仅推送部分服务
./scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --services system-backend,manager-backend
```

#### 问题 4: Registry 磁盘空间不足

**错误信息**:
```
error writing blob: insufficient_storage
```

**解决方法**:
- 清理 Registry 中的旧镜像
- 联系 Registry 管理员增加存储配额

---

## package.sh

**用途**: 生成部署包，支持离线部署和镜像仓库部署两种模式

### 使用方法

```bash
# 离线部署包（默认，包含构建脚本）
./scripts/build/package.sh

# 镜像仓库部署包（轻量，仅配置文件）
./scripts/build/package.sh --mode registry

# 指定 Registry URL
./scripts/build/package.sh --registry harbor.example.com:5001

# 自动传输到服务器
./scripts/build/package.sh --server ubuntu@192.168.1.100

# 组合使用
./scripts/build/package.sh \
  --mode offline \
  --registry localhost:5001 \
  --server ubuntu@production-server
```

### 参数说明

| 参数 | 值 | 说明 |
|------|------|------|
| `--mode` | `offline` \| `registry` | 部署模式（默认：`offline`） |
| `--registry` | URL | Registry 地址（默认：`localhost:5001`） |
| `--server` | `user@host` | 自动传输到远程服务器 |

### 两种部署模式

#### Offline Mode（离线部署）

**适用场景**：服务器无法访问镜像仓库，需要在服务器上本地构建

**包含内容**：
- 所有配置文件（docker-compose, .env, nginx）
- 基础设施脚本（scripts/infra/）
- 生产脚本（scripts/prod/）
- **构建脚本**（scripts/build/compile.sh, build-images.sh）
- 业务基础设施（business/）
- **Tarball 打包**（dist/addp-deploy-offline-{timestamp}.tar.gz）

**部署流程**：
1. 传输完整包到服务器
2. 在服务器上编译二进制（compile.sh）
3. 在服务器上构建镜像（build-images.sh）
4. 从本地 registry 部署

**优点**：
- ✅ 无需网络访问镜像仓库
- ✅ 可针对服务器架构优化编译
- ✅ 完全自包含

**缺点**：
- ⚠️ 部署时间较长（需要编译）
- ⚠️ 服务器需要 Go 和 Docker 构建环境

#### Registry Mode（镜像仓库部署）

**适用场景**：服务器可访问镜像仓库，镜像已预先构建并推送

**包含内容**：
- 所有配置文件（docker-compose, .env, nginx）
- 基础设施脚本（scripts/infra/）
- 生产脚本（scripts/prod/）
- 业务基础设施（business/）
- **不包含**构建脚本
- **不生成** tarball（轻量包）

**部署流程**：
1. 传输配置包到服务器
2. 配置 Registry 访问
3. 从 Registry 拉取镜像
4. 立即部署

**优点**：
- ✅ 部署速度快（无需编译）
- ✅ 服务器无需构建环境
- ✅ 镜像构建与部署分离

**缺点**：
- ⚠️ 需要维护镜像仓库
- ⚠️ 依赖网络访问 Registry

### 生成的包结构

**Offline Mode**:
```
dist/package-offline-{timestamp}/
├── docker-compose.infra.yml       # 基础设施配置
├── docker-compose.yml        # 应用服务配置
├── .env.example                   # 环境变量模板
├── scripts/
│   ├── infra/                     # 基础设施初始化（含 init-db.sql）
│   ├── prod/                      # 生产启动脚本
│   └── build/                     # 构建脚本（仅离线模式）
│       ├── compile.sh
│       └── build-images.sh
├── business/                      # 业务基础设施
├── nginx/                         # Nginx 配置
├── README.md                      # 离线部署说明
└── DEPLOY_INFO.txt                # 部署包元信息

Tarball: dist/addp-deploy-offline-{timestamp}.tar.gz
```

**Registry Mode**:
```
dist/package-registry-{timestamp}/
├── docker-compose.infra.yml       # 基础设施配置
├── docker-compose.yml        # 应用服务配置（Registry URLs 已更新）
├── .env.example                   # 环境变量模板
├── scripts/
│   ├── infra/                     # 基础设施初始化
│   └── prod/                      # 生产启动脚本
├── business/                      # 业务基础设施
├── nginx/                         # Nginx 配置
├── README.md                      # 镜像仓库部署说明
└── DEPLOY_INFO.txt                # 部署包元信息

（无 tarball，直接 rsync 目录）
```

### 示例

```bash
# 场景 1: 离线部署打包
./scripts/build/package.sh --mode offline

# 场景 2: 镜像仓库部署打包
./scripts/build/package.sh --mode registry --registry harbor.example.com:5001

# 场景 3: 离线部署并传输到服务器
./scripts/build/package.sh \
  --mode offline \
  --server ubuntu@192.168.1.100

# 场景 4: 私有仓库部署
./scripts/build/package.sh \
  --mode registry \
  --registry myregistry.com:5001 \
  --server ubuntu@production-server
```

---

## 完整的构建流程

### 本地开发测试流程

```bash
# 1. 编译本机架构
./scripts/build/compile.sh

# 2. 构建本地镜像
./scripts/build/build-images.sh

# 3. 启动测试
docker-compose -f docker-compose.infra.yml up -d
docker-compose -f docker-compose.yml up -d

# 4. 验证
curl http://localhost:8080/health
```

### 生产发布流程

```bash
# 1. 编译多架构二进制
./scripts/build/compile.sh --arch both

# 2. 构建多架构镜像
IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh \
  --multi-arch \
  --registry localhost:5001

# 3. 推送镜像到 Registry（使用 push-images.sh）
docker login  # Docker Hub
# 或者: docker login harbor.example.com:5001  # Harbor

./scripts/build/push-images.sh \
  --registry docker.io/myorg \
  --tag v1.0.0

# 4. 生成部署包
./scripts/build/package.sh \
  --mode registry \
  --registry docker.io/myorg

# 5. 传输到生产服务器
scp -r dist/package-registry-*/ ubuntu@production-server:/opt/addp/

# 6. 在生产服务器上部署
ssh ubuntu@production-server
cd /opt/addp/package-registry-*/
bash scripts/prod/start.sh
```

### CI/CD 自动化流程

```yaml
# .github/workflows/release.yml 示例
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

      - name: Compile binaries
        run: ./scripts/build/compile.sh --arch both

      - name: Build images
        env:
          REGISTRY: ghcr.io/${{ github.repository }}
          IMAGE_TAG: ${{ github.ref_name }}
        run: |
          ./scripts/build/build-images.sh \
            --multi-arch \
            --registry $REGISTRY

      - name: Package deployment
        run: |
          ./scripts/build/package.sh \
            --output ./release-${{ github.ref_name }} \
            --registry ghcr.io/${{ github.repository }}

      - name: Upload artifact
        uses: actions/upload-artifact@v3
        with:
          name: deployment-package
          path: ./release-${{ github.ref_name }}
```

---

## 与设计文档的对应关系

| 设计文档功能 | 当前实现 | 状态 |
|-------------|---------|------|
| `compile.sh --arch both` | ✅ 完全实现 | 完全匹配 |
| `build-images.sh --arch both` | `--multi-arch` | 功能等价 |
| `--tag <version>` | 环境变量 `IMAGE_TAG` | 功能等价 |
| `--push` | 需手动 `docker push` | 可手动操作 |
| `--registry <url>` | ✅ 完全实现 | 完全匹配 |
| `--service <name>` | `--services <list>` | 功能增强 |

---

## 故障排查

### 问题 1: 二进制文件不存在

**错误信息**:
```
错误: amd64 架构的二进制文件不存在
请先运行: ./scripts/build/compile.sh --arch amd64
```

**解决方法**:
```bash
# 先编译，再构建镜像
./scripts/build/compile.sh
./scripts/build/build-images.sh
```

### 问题 2: Docker Buildx 不可用

**错误信息**:
```
ERROR: multiple platforms feature is currently not supported for docker driver
```

**解决方法**:
```bash
# 创建 buildx builder
docker buildx create --use --name addp-builder

# 验证
docker buildx ls
```

### 问题 3: Registry 连接失败

**错误信息**:
```
error: failed to push manifest to localhost:5001
```

**解决方法**:
```bash
# 检查 Registry 是否运行
docker ps | grep registry

# 启动本地 Registry
docker run -d -p 5001:5000 --name registry registry:2

# 或者使用远程 Registry
./scripts/build/build-images.sh --registry hub.docker.com/myorg
```

### 问题 4: 缓存导致构建问题

**解决方法**:
```bash
# 清理编译缓存
rm -rf .compile-cache/

# 清理 Docker 缓存
docker builder prune -af

# 强制重新编译和构建
./scripts/build/compile.sh --force
./scripts/build/build-images.sh --skip-cache
```

---

## 相关文档

- [scripts/prod/README.md](../prod/README.md) - 生产部署脚本文档
- [scripts/dev/README.md](../dev/README.md) - 开发环境脚本文档
- [scripts/design.md](../design.md) - Scripts 架构设计文档
- [CLAUDE.md](../../CLAUDE.md) - 项目总体架构文档

---

## 最佳实践

1. **本地开发**: 使用默认参数，快速迭代
   ```bash
   ./scripts/build/compile.sh && ./scripts/build/build-images.sh
   ```

2. **生产发布**: 始终使用多架构构建和明确的版本标签
   ```bash
   ./scripts/build/compile.sh --arch both
   IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh --multi-arch
   ```

3. **CI/CD**: 使用环境变量传递配置
   ```bash
   BUILD_TYPE=release IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh
   ```

4. **调试**: 使用 debug 构建类型
   ```bash
   BUILD_TYPE=debug ./scripts/build/compile.sh
   BUILD_TYPE=debug ./scripts/build/build-images.sh
   ```

5. **缓存管理**: 定期清理缓存以避免问题
   ```bash
   # 每周一次
   rm -rf .compile-cache/ .gomodcache/ .cache/
   docker builder prune -af
   ```
