# ADDP Scripts 使用指南

本目录包含 ADDP 项目的所有脚本工具，涵盖基础设施管理、开发调试、编译构建、本地部署和生产部署等全流程。

## 核心原则

所有脚本遵循以下 7 个核心原则：

1. **单一职责**: 同样功能只在一处实现，其他地方调用
2. **适应性**: 适应不同环境（OS、CPU 架构），脚本自动适配
3. **清晰明了**: 一看就懂的结构和命名
4. **可重复执行**: 幂等性，多次执行不会破坏系统
5. **易用性**: 用户无需了解技术细节，按顺序执行即可
6. **分散和集中**: 模块相关配置分散，整体管理脚本集中
7. **敢于删除**: 删除重复或无用的内容，避免违反单一职责原则

---

## 目录结构

```
scripts/
├── infra/          # 一、基础设施管理
├── dev/            # 二、本地开发使用
├── build/          # 三、编译和构建
│   ├── compile.sh      # 1) 编译 - go build 生成二进制文件
│   ├── build-images.sh # 2) 构建 - docker build 生成镜像
│   └── package.sh      # 3) 打包 - docker save/push 到磁盘或镜像仓库
├── local/          # 四、本地 Docker 部署
├── prod/           # 五、生产服务器部署
├── registry/       # Docker Registry 管理
└── utils/          # 通用工具脚本
```

---

## 一、基础设施管理 (infra/)

**用途**: 管理 ADDP 的基础设施容器（PostgreSQL、Redis、MinIO、Meilisearch）

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `up.sh` | 启动基础设施 + 自动初始化 | 首次启动、重启基础设施 |
| `down.sh` | 停止基础设施 | 维护、完全停止 |
| `status.sh` | 查看服务状态 | 健康检查、故障排查 |
| `init-db.sh` | 初始化数据库 schema | 数据库重置、清理 |
| `init-minio.sh` | 初始化 MinIO buckets | MinIO 重置 |
| `init-postgresql.sh` | 安装 PostgreSQL 扩展 | PostGIS + pgvector 安装 |
| `init-meilisearch.sh` | 初始化 Meilisearch 索引 | 搜索引擎初始化 |
| `init-redis.sh` | 初始化 Redis 配置 | Redis 验证、清理 |

### 快速使用

```bash
# 启动基础设施（自动完成所有初始化）
bash scripts/infra/up.sh

# 查看状态
bash scripts/infra/status.sh

# 停止基础设施
bash scripts/infra/down.sh
```

### 特性

- ✅ **一键启动**: `up.sh` 自动完成镜像拉取、容器启动、健康检查、所有初始化
- ✅ **跨平台支持**: 自动检测 x86_64/ARM64 架构并选择合适的 PostgreSQL 镜像
- ✅ **模块化资源隔离**: PostgreSQL schema、MinIO bucket、Redis key、Meilisearch index 均按模块命名
- ✅ **幂等性**: 所有脚本可重复执行，不会破坏已有数据

详见: [infra/README.md](infra/README.md)

---

## 二、本地开发使用 (dev/)

**用途**: 用于日常开发调试，直接运行 Go 和 npm 进程（非容器化）

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `start.sh` | 启动完整开发环境（后台运行） | 日常开发启动 |
| `stop.sh` | 停止所有开发服务 | 开发结束、切换分支 |
| `restart.sh` | 重启所有服务 | 代码修改后重启 |
| `modtidy.sh` | 清理 Go 模块依赖 | 依赖冲突、切换分支 |

### 快速使用

```bash
# 一键启动开发环境（基础设施 + 后端 + 前端）
bash scripts/dev/start.sh

# 跳过 Go 依赖检查（节省 5-10 秒）
SKIP_MODTIDY=1 bash scripts/dev/start.sh

# 修改代码后重启
bash scripts/dev/restart.sh

# 停止所有服务
bash scripts/dev/stop.sh
```

### 启动流程

```
[Step 0] Go 依赖检查 (go mod tidy，可跳过)
  ↓
[Step 1] 启动基础设施 (调用 scripts/infra/up.sh)
  ↓
[Step 2-7] 启动后端服务（按依赖顺序）
  - System Backend (8180)
  - Manager Backend (8081) + Meta Backend (8082)
  - Transfer Backend (8083) + Workers
  - Orchestrator Backend (8084)
  - Gateway (8000)
  ↓
[Step 8] 启动前端服务
  - Portal (5170), System (5173), Manager (5174), etc.
```

### 特性

- ✅ **自动依赖启动**: 自动调用 `scripts/infra/up.sh` 启动基础设施
- ✅ **智能跳过**: 基础设施已运行时自动跳过，避免 pgvector 重新编译
- ✅ **健康检查**: 等待每个服务的 `/health` 端点返回 200
- ✅ **日志管理**: 所有日志存储在 `logs/*.log`
- ✅ **PID 追踪**: 存储进程 PID，支持优雅停止

详见: [dev/README.md](dev/README.md)

---

## 三、编译和构建 (build/)

**用途**: 编译二进制文件、构建 Docker 镜像、打包部署

### 核心脚本

| 脚本 | 功能 | 输入 | 输出 |
|------|------|------|------|
| `compile.sh` | 编译二进制文件 | Go 源码 | `dist/release-{os}-{arch}/` |
| `build-images.sh` | 构建 Docker 镜像 | 二进制文件 | Docker 镜像 |
| `push-images.sh` | 推送镜像到 Registry | Docker 镜像 | 远程 Registry |
| `package.sh` | 打包部署包 | 镜像 + 配置 | 部署包 tarball |

### 1. compile.sh - 编译

```bash
# 容器构建（默认，Linux 二进制用于 Docker）
bash scripts/build/compile.sh

# 本地开发（编译本机 OS 可执行文件）
bash scripts/build/compile.sh --local

# 多架构编译（amd64 + arm64）
bash scripts/build/compile.sh --arch both

# 强制重新编译（忽略缓存）
bash scripts/build/compile.sh --force
```

**输出**: `dist/release-{os}-{arch}/` 目录下的二进制文件

### 2. build-images.sh - 构建镜像

```bash
# ⚠️ 必须先运行 compile.sh

# 单架构构建
bash scripts/build/build-images.sh

# 多架构构建（amd64 + arm64）
bash scripts/build/build-images.sh --multi-arch

# 指定 Registry
bash scripts/build/build-images.sh --registry harbor.example.com:5001

# 指定镜像标签
IMAGE_TAG=v1.0.0 bash scripts/build/build-images.sh
```

**输出**: Docker 镜像 `{REGISTRY}/addp-{service}:{TAG}`

### 3. push-images.sh - 推送镜像

```bash
# ⚠️ 必须先登录 Registry
docker login  # Docker Hub
# 或: docker login harbor.example.com:5001

# 推送所有镜像到 Docker Hub
bash scripts/build/push-images.sh --registry docker.io/myusername

# 推送指定版本
bash scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --tag v1.0.0

# 仅推送部分服务
bash scripts/build/push-images.sh \
  --registry docker.io/myusername \
  --services system-backend,manager-backend,gateway

# 干运行测试（不实际推送）
bash scripts/build/push-images.sh --registry docker.io/myusername --dry-run
```

**输出**: 镜像推送到远程 Registry（Docker Hub、Harbor 等）

### 4. package.sh - 打包

```bash
# 离线部署包（包含构建脚本，用于服务器上编译）
bash scripts/build/package.sh --mode offline

# 镜像仓库部署包（轻量，仅配置文件）
bash scripts/build/package.sh --mode registry --registry harbor.example.com:5001

# 自动传输到服务器
bash scripts/build/package.sh --server ubuntu@192.168.1.100
```

**输出**:
- Offline Mode: `dist/addp-deploy-offline-{timestamp}.tar.gz`
- Registry Mode: `dist/package-registry-{timestamp}/` 目录

### 完整构建流程

```bash
# 生产发布示例（完整流程）
# 1. 编译多架构二进制
./scripts/build/compile.sh --arch both

# 2. 构建多架构镜像
IMAGE_TAG=v1.0.0 ./scripts/build/build-images.sh --multi-arch --registry localhost:5001

# 3. 推送镜像到 Registry
docker login  # 或: docker login harbor.example.com:5001
./scripts/build/push-images.sh --registry docker.io/myorg --tag v1.0.0

# 4. 生成部署包
./scripts/build/package.sh --mode registry --registry docker.io/myorg
```

详见: [build/README.md](build/README.md)

---

## 四、本地 Docker 部署 (local/)

**用途**: 在本地使用 Docker Compose 部署完整 ADDP 平台进行测试

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `start.sh` | 启动完整 Docker 环境 | 本地容器化测试 |
| `stop.sh` | 停止 Docker 服务 | 停止测试 |
| `status.sh` | 查看容器状态 | 健康检查、资源使用 |
| `restart.sh` | 重启服务 | 更新配置后重启 |

### 前置条件

```bash
# 1. 确保 Docker 运行
open -a Docker  # macOS

# 2. 构建镜像
bash scripts/build/compile.sh
bash scripts/build/build-images.sh
```

### 快速使用

```bash
# 启动完整平台
bash scripts/local/start.sh

# 访问服务
# - Portal (推荐): http://localhost:80
# - Gateway:        http://localhost:8000
# - System Backend: http://localhost:8180

# 查看状态和资源使用
bash scripts/local/status.sh

# 停止服务（保留基础设施）
bash scripts/local/stop.sh

# 停止所有服务（包括基础设施）
bash scripts/local/stop.sh --all
```

### 特性

- ✅ **镜像验证**: 自动检查所有必需镜像是否存在
- ✅ **智能启动**: 基础设施已运行时跳过，应用层使用 `docker compose up -d` 确保幂等
- ✅ **健康检查**: 等待关键服务健康检查通过
- ✅ **资源监控**: `status.sh` 显示 CPU、内存使用 Top 5

详见: [local/README.md](local/README.md)

---

## 五、生产服务器部署 (prod/)

**用途**: 在生产服务器上部署和管理 ADDP 平台

### 核心脚本

| 脚本 | 功能 | 使用场景 |
|------|------|---------|
| `start.sh` | 启动生产环境（分步启动） | 生产部署启动 |
| `stop.sh` | 停止生产环境 | 维护、停止服务 |
| `health-check.sh` | 健康检查 | 监控、故障排查 |
| `wait-infra.sh` | 等待基础设施就绪 | 启动流程中的依赖检查 |
| `swarm/` | Docker Swarm 高可用部署 | 生产高可用需求 |

### 快速使用

```bash
# 启动完整生产环境
bash scripts/prod/start.sh

# 访问地址
# - ✨ 推荐: http://localhost （Nginx 统一入口）
# - Portal: http://localhost:5170
# - Gateway: http://localhost:8000

# 健康检查
bash scripts/prod/health-check.sh

# 停止服务（保留数据）
bash scripts/prod/stop.sh

# 停止并删除容器
bash scripts/prod/stop.sh --remove
```

### 启动流程

```
[1/5] 基础设施层
  - PostgreSQL, Redis, MinIO, Meilisearch
  - 等待就绪（调用 wait-infra.sh）
  ↓
[2/5] System Backend (8180)
  - 配置中心、认证服务
  - 等待健康检查通过
  ↓
[3/5] 业务后端服务
  - Manager, Meta, Transfer, Orchestrator, Develop
  - Gateway
  ↓
[4/5] 后端健康检查
  - 最多等待 90 秒
  ↓
[5/5] 前端服务
  - Portal, 各模块前端, Nginx
```

### Docker Swarm 高可用

```bash
# 1. 初始化 Swarm（首次）
bash scripts/prod/swarm/init.sh

# 2. 部署服务栈
bash scripts/prod/swarm/deploy.sh

# 3. 查看状态
bash scripts/prod/swarm/status.sh

# 4. 手动扩容
docker service scale addp_transfer-worker=3
```

**Swarm 优势**:
- ✅ 自动重启（容器崩溃自动恢复）
- ✅ 多副本负载均衡（如 Transfer Worker x2）
- ✅ 滚动更新零停机
- ✅ 资源限制和预留

详见: [prod/README.md](prod/README.md), [prod/swarm/README.md](prod/swarm/README.md)

---

## 其他辅助目录

### registry/ - Docker Registry 管理

```bash
scripts/registry/
├── init.sh         # 初始化本地 Docker Registry
├── start.sh        # 启动已存在的 Registry
├── check.sh        # 检查 Registry 状态和镜像列表
└── configure.sh    # 配置 Docker daemon 信任 Registry
```

**用途**: 本地镜像仓库管理、离线部署准备

详见: [registry/README.md](registry/README.md)

### utils/ - 通用工具

```bash
scripts/utils/
├── go-mod-tidy-all.sh               # 批量清理 Go 依赖
├── ports-validate.sh                # 端口规范验证
├── test-tile-api.sh                 # MVT 瓦片 API 测试
└── standardize-frontend-docker.sh   # 前端 Docker 配置标准化
```

**用途**: 通用工具函数、批量操作、验证检查

---

## 使用场景对比

| 场景 | 使用脚本 | 运行方式 | 镜像需求 |
|------|---------|---------|---------|
| **日常开发** | `scripts/dev/` | Go + npm 直接运行 | ❌ 不需要 |
| **本地测试** | `scripts/local/` | Docker Compose | ✅ 需要先构建 |
| **生产部署** | `scripts/prod/` | Docker Compose/Swarm | ✅ 从 Registry 拉取 |
| **构建发布** | `scripts/build/` | 编译 + 构建 + 打包 | ✅ 生成镜像 |

---

## 典型工作流

### 场景 1: 日常开发

```bash
# 1. 启动开发环境
bash scripts/dev/start.sh

# 2. 修改代码
vim system/backend/internal/service/user_service.go

# 3. 重启服务
bash scripts/dev/restart.sh

# 4. 查看日志
tail -f logs/system-backend.log

# 5. 停止环境
bash scripts/dev/stop.sh
```

### 场景 2: 本地容器化测试

```bash
# 1. 构建镜像
bash scripts/build/compile.sh
bash scripts/build/build-images.sh

# 2. 启动 Docker 环境
bash scripts/local/start.sh

# 3. 测试功能
curl http://localhost:8180/health

# 4. 查看状态
bash scripts/local/status.sh

# 5. 停止环境
bash scripts/local/stop.sh
```

### 场景 3: 生产发布

```bash
# 1. 编译多架构二进制
bash scripts/build/compile.sh --arch both

# 2. 构建多架构镜像
IMAGE_TAG=v1.0.0 bash scripts/build/build-images.sh \
  --multi-arch \
  --registry localhost:5001

# 3. 登录并推送镜像到 Registry
docker login  # Docker Hub
# 或: docker login harbor.example.com:5001  # Harbor

bash scripts/build/push-images.sh \
  --registry docker.io/myorg \
  --tag v1.0.0

# 4. 生成部署包
bash scripts/build/package.sh \
  --mode registry \
  --registry docker.io/myorg \
  --server ubuntu@production-server

# 5. 在生产服务器上部署
ssh ubuntu@production-server
cd /opt/addp
bash scripts/prod/start.sh

# 6. 健康检查
bash scripts/prod/health-check.sh
```

---

## 常见问题

### Q1: 端口冲突怎么办？

```bash
# 检查端口占用
lsof -i :8180
lsof -i :5433

# 杀死占用进程或修改 .env 配置
```

### Q2: 基础设施启动失败？

```bash
# 查看基础设施状态
bash scripts/infra/status.sh

# 查看日志
docker logs postgres
docker logs redis
```

### Q3: 服务健康检查超时？

```bash
# 查看服务日志
tail -f logs/system-backend.log

# 手动测试健康端点
curl http://localhost:8180/health
```

### Q4: Docker 镜像不存在？

```bash
# 重新构建镜像
bash scripts/build/compile.sh
bash scripts/build/build-images.sh

# 验证镜像
docker images | grep addp
```

### Q5: 如何清理所有数据？

```bash
# ⚠️ 危险操作：会删除所有数据

# 开发模式
bash scripts/dev/stop.sh

# 本地 Docker
bash scripts/local/stop.sh --all --volumes

# 生产环境
bash scripts/prod/stop.sh --volumes
```

---

## 最佳实践

1. **开发环境**: 始终使用 `scripts/dev/` 进行日常开发，重启速度快
2. **容器测试**: 使用 `scripts/local/` 验证 Docker 配置和部署流程
3. **生产部署**: 使用 `scripts/prod/` 部署，启用 Docker Swarm 提高可用性
4. **定期备份**: 生产环境定期备份 PostgreSQL 和 MinIO 数据
5. **版本管理**: 生产镜像使用明确的版本标签（如 `v1.0.0`），避免使用 `latest`
6. **日志监控**: 定期查看日志文件和 `health-check.sh` 输出
7. **资源限制**: 在 docker-compose.yml 中配置 CPU 和内存限制

---

## 相关文档

- [CLAUDE.md](../CLAUDE.md) - 项目总体架构文档
- [Makefile](../Makefile) - Make 命令封装
- [docker-compose.infra.yml](../docker-compose.infra.yml) - 基础设施配置
- [docker-compose.yml](../docker-compose.yml) - 应用服务配置
- [docs/DEPLOYMENT.md](../docs/DEPLOYMENT.md) - 详细部署指南
- [docs/DOCKER_SWARM.md](../docs/DOCKER_SWARM.md) - Swarm 高可用部署

---

## 贡献指南

添加新脚本时，请遵循以下规范：

1. **命名规范**: 使用 `kebab-case`（小写 + 连字符）
2. **Shebang**: 始终使用 `#!/usr/bin/env bash`
3. **错误处理**: 使用 `set -e` 或显式错误检查
4. **颜色输出**: 使用统一的颜色变量（GREEN, RED, YELLOW）
5. **幂等性**: 脚本应支持重复执行
6. **文档**: 添加脚本时更新对应的 README.md

---

**Version**: 0.0.13
**Last Updated**: 2025-12-10
