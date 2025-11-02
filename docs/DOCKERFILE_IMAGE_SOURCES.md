# Dockerfile 镜像来源策略

## 概述

ADDP 项目采用**混合镜像源策略**,针对不同类型的服务使用不同的基础镜像来源:

| 服务类型 | 基础镜像 | 来源 | 原因 |
|---------|---------|------|------|
| **后端服务** (6个) | alpine:latest | `localhost:5001` | 预编译Go二进制,极速构建 |
| **前端服务** (5个) | node:18-alpine<br>nginx:alpine | `Docker Hub` | 需要正确架构,multi-arch支持 |
| **Nginx网关** | nginx:alpine | `Docker Hub` | 需要正确架构,multi-arch支持 |
| **基础设施** | redis/minio/es | `localhost:5001` | 预配置multi-arch manifest |

## 详细列表

### 后端服务 (使用本地仓库)

所有后端服务使用 `FROM localhost:5001/alpine:latest`:

1. **system/backend/Dockerfile.prebuilt**
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH
   COPY system/backend/server-${TARGETARCH} ./server
   ```

2. **manager/backend/Dockerfile.prebuilt**
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH
   COPY manager/backend/server-${TARGETARCH} ./server
   ```

3. **meta/backend/Dockerfile.prebuilt**
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH
   COPY meta/backend/server-${TARGETARCH} ./server
   ```

4. **transfer/backend/Dockerfile.prebuilt**
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH
   COPY transfer/backend/server-${TARGETARCH} ./server
   ```

5. **transfer/backend/Dockerfile.prebuilt.worker**
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH
   COPY transfer/backend/worker-${TARGETARCH} ./worker
   ```

6. **gateway/Dockerfile.prebuilt**
   ```dockerfile
   FROM localhost:5001/alpine:latest
   ARG TARGETARCH
   COPY gateway/server-${TARGETARCH} ./server
   ```

**性能优势**:
- FROM resolve: 0.6s (vs Docker Hub 的 61s)
- 总构建时间: ~1.5s (100倍提升)

### 前端服务 (使用 Docker Hub)

所有前端服务使用 Docker Hub 镜像:

1. **portal/frontend/Dockerfile**
2. **system/frontend/Dockerfile**
3. **manager/frontend/Dockerfile**
4. **meta/frontend/Dockerfile**
5. **transfer/frontend/Dockerfile**

**统一模板**:
```dockerfile
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

**为什么使用 Docker Hub**:
- ✅ 正确的 multi-arch manifest (AMD64 + ARM64)
- ✅ node:18-alpine 需要正确架构来编译 npm native modules
- ✅ nginx:alpine 需要正确架构来运行
- ✅ 无法在 ARM Mac 上创建这些镜像的 AMD64 版本到本地仓库

### Nginx 网关 (使用 Docker Hub)

**nginx/Dockerfile**:
```dockerfile
# Use Docker Hub for multi-arch support
FROM nginx:alpine

COPY nginx.conf /etc/nginx/nginx.conf
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost/health || exit 1
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

### 基础设施服务 (使用本地仓库)

基础设施镜像由 `deploy-all.sh` 自动设置到本地仓库:

**docker-compose.prod.yml**:
```yaml
services:
  redis:
    image: ${REGISTRY:-localhost:5001}/addp-infra-redis:7-alpine
  
  minio-system:
    image: ${REGISTRY:-localhost:5001}/addp-infra-minio:latest
  
  elasticsearch:
    image: ${REGISTRY:-localhost:5001}/addp-infra-elasticsearch:8.11.0
```

## 自动化设置

### deploy-all.sh 自动检测和设置

脚本会自动检测本地仓库中是否存在必需的基础镜像:

```bash
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

**检测逻辑**:
1. 检查 `localhost:5001/alpine:latest` 是否存在
2. 检查 `localhost:5001/addp-infra-redis:7-alpine` 是否存在
3. 检查 `localhost:5001/addp-infra-minio:latest` 是否存在
4. 检查 `localhost:5001/addp-infra-elasticsearch:8.11.0` 是否存在

**自动设置** (如果镜像缺失):
```bash
# [1/4] Alpine
docker pull alpine:latest
docker tag alpine:latest localhost:5001/alpine:latest-arm64
docker push localhost:5001/alpine:latest-arm64
docker buildx imagetools create --tag localhost:5001/alpine:latest \
    localhost:5001/alpine:latest-arm64

# [2/4] Redis
docker pull redis:7-alpine
docker tag redis:7-alpine localhost:5001/addp-infra-redis:7-alpine-arm64
docker push localhost:5001/addp-infra-redis:7-alpine-arm64
docker buildx imagetools create --tag localhost:5001/addp-infra-redis:7-alpine \
    localhost:5001/addp-infra-redis:7-alpine-arm64

# [3/4] MinIO (同上)
# [4/4] Elasticsearch (同上)
```

**注意**: 
- 在 ARM Mac 上只能推送 ARM64 版本
- 但后端预编译的Go二进制包含正确的架构代码
- 基础设施镜像在AMD64服务器上会自动拉取正确的架构

## 构建性能对比

### 后端 (使用本地仓库)

| 步骤 | Docker Hub | 本地仓库 | 提升 |
|-----|-----------|---------|------|
| FROM alpine resolve | 61.3s | 0.6s | **100x** |
| COPY binary | 0.1s | 0.1s | - |
| **总计** | **62s** | **1.5s** | **41x** |

### 前端 (使用 Docker Hub)

| 步骤 | 耗时 | 说明 |
|-----|------|------|
| FROM node:18-alpine | ~10s | Docker Hub,正常 |
| npm install | ~30s | 主要耗时 |
| npm run build | ~10s | 正常 |
| FROM nginx:alpine | ~1s | Docker Hub,快速 |
| COPY dist | ~1s | 正常 |
| **总计** | **~52s** | **可接受** |

### Nginx (使用 Docker Hub)

| 步骤 | 耗时 |
|-----|------|
| FROM nginx:alpine | ~60s |
| COPY nginx.conf | <1s |
| **总计** | **~60s** |

## 故障排查

### 错误 1: no match for platform in manifest

**症状**:
```
ERROR: no match for platform in manifest: not found
Failed to build portal (amd64)
```

**原因**: Dockerfile 使用 `FROM localhost:5001/nginx:alpine`,但本地仓库只有 ARM64 版本

**解决方案**: 
```dockerfile
# 修改前
FROM localhost:5001/nginx:alpine

# 修改后
FROM nginx:alpine  # 使用 Docker Hub
```

### 错误 2: 后端构建慢

**症状**: 后端构建每次耗时 60+ 秒

**原因**: Dockerfile 使用 `FROM alpine:latest` (Docker Hub)

**解决方案**:
```dockerfile
# 修改前
FROM alpine:latest

# 修改后
FROM localhost:5001/alpine:latest  # 使用本地仓库
```

### 错误 3: 基础设施镜像架构不匹配

**症状**:
```
platform (linux/arm64) does not match detected host platform (linux/amd64)
```

**原因**: 本地仓库中基础设施镜像只有 ARM64 版本

**解决方案**: 重新运行 deploy-all.sh,脚本会自动设置 multi-arch manifest

## 检查当前配置

运行以下命令检查所有 Dockerfile 的镜像来源:

```bash
# 检查后端 (应该都是 localhost:5001/alpine:latest)
grep "^FROM" */backend/Dockerfile.prebuilt gateway/Dockerfile.prebuilt

# 检查前端 (应该都是 node:18-alpine 和 nginx:alpine)
grep "^FROM" */frontend/Dockerfile

# 检查 nginx (应该是 nginx:alpine)
grep "^FROM" nginx/Dockerfile

# 预期输出:
# 后端: FROM localhost:5001/alpine:latest (6个)
# 前端: FROM node:18-alpine 和 FROM nginx:alpine (5个)
# Nginx: FROM nginx:alpine (1个)
```

## 总结

### 镜像来源决策矩阵

| 需求 | 镜像源 | 原因 |
|-----|--------|------|
| 预编译二进制 + 极速构建 | 本地仓库 | 100倍速度提升 |
| 需要正确架构 (node/nginx) | Docker Hub | Multi-arch 支持 |
| 基础设施 (redis/minio/es) | 本地仓库 | 自动设置,离线部署 |

### 关键文件

- [scripts/deploy/deploy-all.sh](../scripts/deploy/deploy-all.sh) - 自动设置脚本
- [docs/MULTIARCH_IMAGE_STRATEGY.md](MULTIARCH_IMAGE_STRATEGY.md) - 多架构策略详解
- [docs/BUILD_SPEED_OPTIMIZATION.md](BUILD_SPEED_OPTIMIZATION.md) - 构建优化详解
