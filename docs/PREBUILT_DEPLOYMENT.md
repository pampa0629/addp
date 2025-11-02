# ADDP 预编译部署方案

## 概述

ADDP 采用**本地预编译 + 简化镜像**的部署方案，解决了网络依赖和构建速度问题。

## 架构对比

### 旧方案：容器内编译
```dockerfile
FROM golang:1.24-alpine AS builder
# 下载 Go 模块（需要网络）
RUN go mod download
# 编译代码（耗时）
RUN go build -o server
# 最终镜像大小：300MB+
```

**问题**：
- ❌ 每次构建都需要下载依赖（网络超时风险）
- ❌ 编译时间长（3-5分钟）
- ❌ 镜像体积大（包含编译工具链）

### 新方案：本地预编译
```dockerfile
FROM alpine:latest
WORKDIR /app
COPY backend/server .  # 只复制编译好的二进制文件
CMD ["./server"]
```

**优势**：
- ✅ 无网络依赖（本地编译完成）
- ✅ 构建速度快（1-2秒打包完成）
- ✅ 镜像体积小（52MB vs 300MB+）
- ✅ 支持跨平台编译（Go原生能力）

## 交叉编译能力

Go 支持零配置跨平台编译：

```bash
# 当前机器：Mac ARM64
# 目标服务器：Linux AMD64

# 一条命令编译双平台
./scripts/deploy/0-compile-binaries.sh --arch both

# 输出：
# ✓ system-backend-amd64 (28MB)
# ✓ system-backend-arm64 (28MB)
# ✓ manager-backend-amd64 (40MB)
# ✓ manager-backend-arm64 (40MB)
# ... 其他服务
```

## 部署流程

### 完整自动化部署

```bash
# 一键完成所有步骤
./scripts/deploy/deploy-all.sh --server user@host

# 执行的步骤：
# Step 0/3: 编译 Go 二进制文件
#   - 检测系统架构（ARM64/AMD64）
#   - 本地交叉编译所有后端服务
#
# Step 1/3: 构建 Docker 镜像
#   - 使用简化 Dockerfile
#   - 只打包编译好的二进制文件
#   - 无需网络连接
#
# Step 2/3: 传输和部署
#   - 打包部署文件
#   - 传输到目标服务器
#   - 拉取镜像并启动服务
```

### 分步执行（调试时）

```bash
# 步骤 0: 编译二进制文件
./scripts/deploy/0-compile-binaries.sh

# 步骤 1: 构建镜像
./scripts/deploy/1-build-images.sh

# 步骤 2: 部署
./scripts/deploy/deploy-all.sh --skip-build --server user@host
```

## 文件结构

```
addp/
├── scripts/deploy/
│   ├── 0-compile-binaries.sh  # 编译脚本（新增）
│   ├── 1-build-images.sh       # 镜像构建
│   └── deploy-all.sh           # 一键部署
│
├── system/backend/
│   ├── Dockerfile              # 旧的（容器内编译）
│   ├── Dockerfile.prebuilt     # 新的（预编译版本）
│   └── server                  # 编译好的二进制文件
│
├── manager/backend/
│   ├── Dockerfile.prebuilt
│   └── server
│
├── meta/backend/
│   ├── Dockerfile.prebuilt
│   └── server
│
└── gateway/
    ├── Dockerfile.prebuilt
    └── server
```

## 性能对比

| 指标 | 旧方案 | 新方案 | 提升 |
|------|--------|--------|------|
| **构建时间** | 3-5分钟 | 10-30秒 | **10x** |
| **镜像大小** | 300MB+ | 52MB | **6x** |
| **网络依赖** | 必需 | 无 | ✅ |
| **失败率** | 高（网络超时） | 低 | ✅ |

## 技术细节

### Go 编译参数

```bash
CGO_ENABLED=0         # 禁用 CGO，生成纯静态二进制
GOOS=linux            # 目标操作系统
GOARCH=arm64          # 目标架构（arm64 或 amd64）
go build -ldflags="-s -w"  # 去除调试信息，减小体积
```

### 编译产物

```bash
$ ls -lh */backend/server gateway/server
-rwxr-xr-x  1 user  staff   28M  system/backend/server
-rwxr-xr-x  1 user  staff   40M  manager/backend/server
-rwxr-xr-x  1 user  staff   42M  meta/backend/server
-rwxr-xr-x  1 user  staff   13M  gateway/server
```

### Docker 镜像层

```bash
$ docker history localhost:5001/addp-system-backend:latest
IMAGE          CREATED         SIZE      COMMENT
08b0386a37e4   2 minutes ago   28MB      COPY system/backend/server
4b7ce07002c6   2 weeks ago     24.5MB    FROM alpine:latest
```

## 常见问题

### Q: 为什么不在容器内编译？
A: 容器内编译每次都需要下载 Go 模块，受网络影响大且速度慢。本地编译可以利用 Go 模块缓存，速度更快且稳定。

### Q: 跨平台编译会影响性能吗？
A: 不会。Go 的交叉编译生成的是原生二进制文件，性能与在目标平台上编译完全相同。

### Q: 如何支持多架构部署？
A: 使用 `--arch both` 参数编译双平台二进制文件，然后在打包镜像时选择对应架构的文件。

### Q: 前端服务呢？
A: 前端（Vue + Vite）仍然在容器内构建，因为 npm install 速度较快且依赖稳定。可以后续优化。

## 下一步优化

- [ ] 前端预构建（`npm run build` 本地完成）
- [ ] 多阶段缓存（复用 node_modules 层）
- [ ] 镜像压缩（使用 UPX 压缩二进制文件）
- [ ] 版本管理（为二进制文件添加版本标签）

## 参考资料

- [Go 交叉编译文档](https://go.dev/doc/install/source#environment)
- [Docker Multi-Stage Builds](https://docs.docker.com/build/building/multi-stage/)
- [Alpine Linux](https://alpinelinux.org/)
