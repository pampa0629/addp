# Build Speed Optimization

## 问题背景

在多架构镜像构建过程中,发现每个构建步骤耗时都很长:

```bash
#5 [1/3] FROM docker.io/library/alpine:latest...
#5 resolve docker.io/library/alpine:latest... 61.3s done
```

**原因分析**:
- Docker 需要从 Docker Hub 解析 alpine:latest 镜像的 digest
- 网络延迟导致每次 resolve 操作耗时 61 秒
- 24 个服务镜像构建 × 61秒 = 约 25 分钟

## 优化方案

将所有基础镜像提前推送到本地私有仓库 (localhost:5001),并修改 Dockerfile 使用本地镜像:

### 1. 基础镜像列表

| 镜像类型 | Docker Hub 镜像 | 本地仓库镜像 | 用途 |
|---------|----------------|-------------|------|
| Alpine | `alpine:latest` | `localhost:5001/alpine:latest` | 后端服务基础镜像 |
| Redis | `redis:7-alpine` | `localhost:5001/addp-infra-redis:7-alpine` | 缓存和队列 |
| MinIO | `minio/minio:latest` | `localhost:5001/addp-infra-minio:latest` | 对象存储 |
| Elasticsearch | `elasticsearch:8.11.0` | `localhost:5001/addp-infra-elasticsearch:8.11.0` | 搜索引擎 |
| Nginx | `nginx:alpine` | `localhost:5001/nginx:alpine` | 前端 Web 服务器 |
| Node | `node:18-alpine` | `localhost:5001/node:18-alpine` | 前端构建 |

### 2. 自动化集成

基础设施镜像设置已**自动集成到 `deploy-all.sh`** 脚本中:

1. **首次运行检测**: 脚本会自动检查本地仓库中是否已有基础镜像
2. **一键设置**: 如果缺失,自动从 Docker Hub 拉取、标记、推送到本地仓库
3. **创建 Multi-Arch Manifest**: 自动为 AMD64 和 ARM64 架构创建统一的 manifest list
4. **后续运行跳过**: 一旦设置完成,后续运行会跳过这一步

### 3. Dockerfile 修改

**后端服务** (system/manager/meta/transfer/gateway):
```dockerfile
# Before
FROM alpine:latest

# After
FROM localhost:5001/alpine:latest
```

**前端服务** (portal/system/manager/meta/transfer):
```dockerfile
# Before
FROM node:18-alpine AS builder
FROM nginx:alpine

# After
FROM localhost:5001/node:18-alpine AS builder
FROM localhost:5001/nginx:alpine
```

## 性能提升

### 单次构建对比

| 指标 | 优化前 | 优化后 | 提升 |
|-----|--------|--------|------|
| FROM resolve | 61.3s | 0.6s | **100x** |
| 总构建时间 | 62.22s | 1.123s | **55x** |

### 完整部署对比

| 场景 | 优化前 | 优化后 | 节省时间 |
|-----|--------|--------|---------|
| 24 服务构建 (AMD64) | ~25 分钟 | ~24 秒 | **约 24 分钟** |
| 双架构构建 (AMD64+ARM64) | ~50 分钟 | ~48 秒 | **约 49 分钟** |

## 使用方法

### 一键部署 (推荐)

```bash
# 自动处理所有步骤,包括基础镜像设置
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

**脚本流程**:
1. 检查基础镜像是否存在于本地仓库
2. 如缺失,自动设置多架构基础镜像 (首次运行约 5-10 分钟)
3. 编译 Go 二进制文件 (AMD64 + ARM64)
4. 构建 Docker 镜像 (极速,约 48 秒)
5. 打包并传输到目标服务器
6. 在服务器上启动所有服务

### 手动验证

```bash
# 验证基础镜像是否存在
docker buildx imagetools inspect localhost:5001/alpine:latest
docker buildx imagetools inspect localhost:5001/nginx:alpine
docker buildx imagetools inspect localhost:5001/node:18-alpine

# 测试构建速度
time docker build --platform linux/amd64 \
    -t localhost:5001/addp-gateway:test \
    -f gateway/Dockerfile.prebuilt .
```

## 技术细节

### Multi-Arch Manifest 创建

```bash
# 1. 拉取两个架构的镜像
docker pull --platform linux/amd64 alpine:latest
docker pull --platform linux/arm64 alpine:latest

# 2. 标记并推送到本地仓库
docker tag alpine:latest localhost:5001/alpine:latest-amd64
docker tag alpine:latest localhost:5001/alpine:latest-arm64
docker push localhost:5001/alpine:latest-amd64
docker push localhost:5001/alpine:latest-arm64

# 3. 创建 Multi-Arch Manifest (关键步骤)
docker buildx imagetools create \
    --tag localhost:5001/alpine:latest \
    localhost:5001/alpine:latest-amd64 \
    localhost:5001/alpine:latest-arm64
```

### Manifest List 原理

- **OCI Image Index**: 单个标签 (如 `alpine:latest`) 指向多个架构的镜像
- **自动架构选择**: Docker 根据目标平台 (--platform) 自动选择正确的镜像
- **兼容性**: 在 AMD64 服务器上拉取 `localhost:5001/alpine:latest` 自动获取 AMD64 版本

### 为什么这么快?

1. **本地网络**: localhost:5001 → 本机,无网络延迟
2. **已缓存**: Docker 本地已有镜像层,无需下载
3. **无 digest 查询**: 不需要向 Docker Hub 查询最新 digest

## 部署约束

### 服务器环境要求

由于服务器**只能访问本机的私有仓库** (localhost:5001),无法连接 Docker Hub,因此:

✅ **必须**:
- 在本机运行 Docker Registry (localhost:5001)
- 将所有基础镜像推送到本机仓库
- 修改所有 Dockerfile 使用本机仓库镜像

❌ **不可行**:
- 在 Dockerfile 中使用 `FROM alpine:latest` (无法从 Docker Hub 拉取)
- 依赖外部镜像仓库 (如 Docker Hub, 阿里云)

### 网络架构

```
┌─────────────────────────────────────────────────┐
│  Build Machine (Mac ARM64)                     │
│                                                 │
│  ┌──────────────────┐    ┌──────────────────┐ │
│  │ Docker Registry  │    │ Docker Engine    │ │
│  │ localhost:5001   │◄───│ Build Images     │ │
│  └────────┬─────────┘    └──────────────────┘ │
│           │                                     │
└───────────┼─────────────────────────────────────┘
            │ Package + Transfer
            ▼
┌─────────────────────────────────────────────────┐
│  Target Server (AMD64)                         │
│                                                 │
│  ┌──────────────────┐    ┌──────────────────┐ │
│  │ Docker Registry  │    │ Docker Compose   │ │
│  │ localhost:5001   │◄───│ Pull Images      │ │
│  └──────────────────┘    └──────────────────┘ │
│                                                 │
│  ⚠️  No external network access                │
└─────────────────────────────────────────────────┘
```

## 故障排查

### 问题 1: 镜像架构不匹配

```
Error: The requested image's platform (linux/arm64) does not match
       the detected host platform (linux/amd64/v3)
```

**原因**: 本地仓库中的镜像只有一个架构 (例如只有 ARM64)

**解决方案**:
```bash
# 重新运行 deploy-all.sh,会自动创建 multi-arch manifest
./scripts/deploy/deploy-all.sh --server your-server
```

### 问题 2: 基础镜像缺失

```
Error: manifest for localhost:5001/alpine:latest not found
```

**解决方案**:
```bash
# 让 deploy-all.sh 自动设置,或手动运行:
docker buildx imagetools inspect localhost:5001/alpine:latest
# 如果不存在,脚本会自动设置
```

### 问题 3: Registry 连接失败

```
Error: Get "http://localhost:5001/v2/": dial tcp 127.0.0.1:5001: connect: connection refused
```

**解决方案**:
```bash
# 启动本地 Docker Registry
docker run -d -p 5001:5000 --name registry registry:2
```

## 总结

通过将基础镜像从 Docker Hub 迁移到本地仓库,实现了:

1. ✅ **100 倍速度提升**: FROM resolve 从 61s 降至 0.6s
2. ✅ **完全离线构建**: 无需访问 Docker Hub
3. ✅ **自动化集成**: 一键脚本搞定所有步骤
4. ✅ **多架构支持**: AMD64 + ARM64 统一 manifest
5. ✅ **缓存友好**: Docker layer cache 完全生效

**关键命令**:
```bash
# 一键部署到远程服务器 (包含所有优化)
./scripts/deploy/deploy-all.sh --server user@host

# 验证基础镜像
docker buildx imagetools inspect localhost:5001/alpine:latest
```
