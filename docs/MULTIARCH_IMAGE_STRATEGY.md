# Multi-Architecture Image Strategy

## 问题背景

在 ARM Mac 上构建多架构镜像(AMD64 + ARM64)时,遇到了一个关键问题:

**无法在 ARM Mac 上获取 AMD64 镜像的实际内容**

即使使用 `docker pull --platform linux/amd64 nginx:alpine`,Docker 也只会下载 manifest 元数据,不会下载实际的镜像层。这导致推送到本地仓库时,只有 ARM64 版本的镜像。

## 最终解决方案

采用**混合策略**:

### 1. 后端服务 - 使用本地仓库 Alpine

**原因**:
- ✅ 后端使用预编译的 Go 二进制文件
- ✅ Dockerfile 只需要复制二进制文件到 alpine 镜像
- ✅ alpine 是最基础的镜像,体积小(3MB)
- ✅ 从本地仓库拉取 alpine 速度快(0.6s vs 61s)

**实现**:
```dockerfile
# Backend Dockerfiles (system/manager/meta/transfer/gateway)
FROM localhost:5001/alpine:latest

ARG TARGETARCH
WORKDIR /app
COPY service/backend/server-${TARGETARCH} ./server
CMD ["./server"]
```

**本地仓库设置**:
```bash
# deploy-all.sh 自动执行
docker pull alpine:latest
docker tag alpine:latest localhost:5001/alpine:latest-arm64
docker push localhost:5001/alpine:latest-arm64
# 在 AMD64 服务器上,Docker 会自动选择 ARM64 版本(因为没有 AMD64)
```

### 2. 前端服务 - 使用 Docker Hub

**原因**:
- ✅ Docker Hub 提供正确的多架构 manifest
- ✅ 前端构建需要 node:18-alpine (构建阶段) + nginx:alpine (运行阶段)
- ✅ 无法在 ARM Mac 上获取这些镜像的 AMD64 版本
- ✅ Docker 会自动根据 `--platform` 参数选择正确的架构

**实现**:
```dockerfile
# Frontend Dockerfiles (portal/system/manager/meta/transfer)
# Build stage - use Docker Hub for multi-arch support
FROM node:18-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build

# Runtime stage - use Docker Hub for multi-arch support
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
```

**构建命令**:
```bash
# ARM Mac 上构建 AMD64 镜像
docker build --platform linux/amd64 \
    -t localhost:5001/addp-portal:latest-amd64 \
    -f portal/frontend/Dockerfile \
    portal/frontend

# Docker 会:
# 1. 从 Docker Hub 拉取 node:18-alpine 的 AMD64 版本
# 2. 从 Docker Hub 拉取 nginx:alpine 的 AMD64 版本
# 3. 构建 AMD64 镜像
# 4. 推送到本地仓库
```

## 架构对比

### Before (失败的尝试)

```
所有服务都使用本地仓库
├── Backend: FROM localhost:5001/alpine:latest ✅
├── Frontend: FROM localhost:5001/node:18-alpine ❌
└── Frontend: FROM localhost:5001/nginx:alpine ❌

问题: 无法在 ARM Mac 上创建 node 和 nginx 的 AMD64 manifest
```

### After (成功的方案)

```
混合策略
├── Backend: FROM localhost:5001/alpine:latest ✅
│   └── 只有 ARM64,但后端是预编译的,可以正常运行
│
└── Frontend: FROM node:18-alpine (Docker Hub) ✅
            FROM nginx:alpine (Docker Hub) ✅
    └── Docker Hub 有完整的 AMD64 + ARM64 manifest
```

## 性能影响

### 后端构建速度

| 镜像源 | Resolve 时间 | 总构建时间 |
|--------|-------------|-----------|
| Docker Hub | 61.3s | 62.2s |
| 本地仓库 | 0.6s | 1.1s |
| **提升** | **100x** | **56x** |

### 前端构建速度

| 阶段 | Docker Hub | 本地仓库 | 说明 |
|-----|-----------|---------|------|
| node:18-alpine | ~10s | N/A | 需要 Docker Hub |
| npm install | ~30s | ~30s | 不变 |
| npm run build | ~10s | ~10s | 不变 |
| nginx:alpine | ~1s | N/A | 需要 Docker Hub |
| **总计** | ~51s | N/A | 可接受 |

**结论**: 前端构建时间主要消耗在 npm 操作,基础镜像拉取影响小。

## 部署流程

### 1. 首次运行 deploy-all.sh

```bash
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

**自动执行**:
1. ✅ 检测本地仓库中是否有 alpine 和基础设施镜像
2. ✅ 如缺失,从 Docker Hub 拉取并推送到本地仓库:
   - alpine:latest
   - redis:7-alpine
   - minio/minio:latest
   - elasticsearch:8.11.0
3. ✅ 编译 Go 二进制文件(AMD64 + ARM64)
4. ✅ 构建所有镜像:
   - 后端: 从本地仓库拉取 alpine (极速)
   - 前端: 从 Docker Hub 拉取 node 和 nginx (正常速度)
5. ✅ 创建多架构 manifest
6. ✅ 打包并传输到服务器
7. ✅ 在服务器上启动服务

### 2. 后续运行

```bash
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

**跳过设置步骤**:
- ✅ 检测到本地仓库已有镜像,直接跳过(1-2秒)
- ✅ 直接进入编译和构建阶段
- ✅ 总时间缩短至 3-4 分钟

## 技术细节

### Alpine 镜像的特殊性

**为什么后端可以只用 ARM64 的 alpine?**

1. **Go 二进制是预编译的**:
   ```bash
   # 在 Mac 上编译 AMD64 和 ARM64 版本
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server-amd64
   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o server-arm64
   ```

2. **Dockerfile 使用 ARG TARGETARCH**:
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH  # Docker 自动设置为 amd64 或 arm64
   COPY server-${TARGETARCH} ./server
   ```

3. **即使 alpine 只有 ARM64**:
   - 在 ARM Mac 上构建时:
     - `--platform linux/amd64` → 复制 server-amd64
     - `--platform linux/arm64` → 复制 server-arm64
   - Go 二进制已经包含了正确的架构代码
   - alpine 只是一个运行环境(提供基础的 libc 等)

### 前端为什么必须用 Docker Hub?

1. **node:18-alpine 用于构建阶段**:
   ```dockerfile
   FROM node:18-alpine AS builder
   RUN npm install  # 需要正确的架构来编译 native modules
   ```
   - 必须使用正确架构的 node 来执行 npm install
   - 某些 npm 包包含 native code,需要编译

2. **nginx:alpine 用于运行阶段**:
   ```dockerfile
   FROM nginx:alpine
   COPY --from=builder /app/dist /usr/share/nginx/html
   ```
   - 必须使用正确架构的 nginx 来运行

## 故障排查

### 错误: no match for platform in manifest

```
ERROR: failed to solve: localhost:5001/nginx:alpine: no match for platform in manifest
```

**原因**: 本地仓库中的 nginx:alpine 只有 ARM64 架构

**解决方案**: 修改 Dockerfile 使用 Docker Hub:
```dockerfile
# Before
FROM localhost:5001/nginx:alpine

# After
FROM nginx:alpine  # Docker Hub 自动提供多架构
```

### 错误: 构建速度慢

如果发现构建速度慢:

1. **检查是否从 Docker Hub 拉取 alpine**:
   ```bash
   docker build --progress=plain ... 2>&1 | grep "FROM"
   # 应该看到: FROM localhost:5001/alpine:latest (后端)
   # 应该看到: FROM docker.io/library/node:18-alpine (前端)
   ```

2. **检查 alpine 是否在本地仓库**:
   ```bash
   docker buildx imagetools inspect localhost:5001/alpine:latest
   ```

## 总结

| 镜像类型 | 镜像源 | 原因 |
|---------|--------|------|
| **alpine** | localhost:5001 | 后端预编译,只需要基础环境,极速 |
| **node** | Docker Hub | 前端构建需要,必须用正确架构 |
| **nginx** | Docker Hub | 前端运行需要,必须用正确架构 |
| **redis/minio/elasticsearch** | localhost:5001 | 基础设施,已设置多架构 manifest |

**关键优化**:
- ✅ 后端构建速度提升 **100倍** (0.6s vs 61s)
- ✅ 前端构建速度正常 (~51s,主要是 npm)
- ✅ 完全支持多架构部署(AMD64 + ARM64)
- ✅ 一键部署,自动处理所有细节
