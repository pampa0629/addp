# 跨平台架构部署指南

本文档解决 **ARM Mac (Apple Silicon) 构建镜像部署到 Intel Mac/Linux 服务器** 的问题。

---

## 问题背景

### CPU 架构差异

| 设备 | CPU 架构 | Docker 平台标识 |
|------|---------|----------------|
| MacBook M1/M2/M3 | ARM64 | `linux/arm64` |
| MacBook Intel | x86_64 | `linux/amd64` |
| Linux 服务器 (Intel/AMD) | x86_64 | `linux/amd64` |

### 问题现象

在 ARM Mac 上构建的镜像，直接在 Intel 服务器上运行会报错：

```
WARNING: The requested image's platform (linux/arm64) does not match
the detected host platform (linux/amd64)

exec user process caused: exec format error
```

**原因**：ARM 架构的二进制文件无法在 x86 CPU 上直接执行。

---

## 解决方案对比

| 方案 | 优点 | 缺点 | 推荐度 |
|------|------|------|--------|
| **方案一: 多平台构建 (Buildx)** | 一次构建支持多架构 | 首次配置稍复杂 | ⭐⭐⭐⭐⭐ 推荐 |
| 方案二: 在服务器上构建 | 无架构问题 | 需要服务器有开发环境 | ⭐⭐⭐ |
| 方案三: Docker Desktop 模拟 | 简单 | 构建速度慢，不稳定 | ⭐⭐ |

---

## ⭐ 方案一：使用 Docker Buildx 多平台构建（推荐）

### 原理

Docker Buildx 使用 QEMU 模拟器，可以在 ARM Mac 上构建 x86 镜像。

### 步骤

#### 1. 检查 Docker Buildx

```bash
# 检查是否已安装
docker buildx version

# 预期输出
# github.com/docker/buildx v0.12.0
```

**如果未安装：**
- Docker Desktop >= 19.03 已内置 Buildx
- 更新 Docker Desktop 到最新版本

#### 2. 使用多平台构建脚本（最简单）

```bash
# 自动构建 x86 镜像（适用于 Intel 服务器）
./scripts/push-to-local-registry-multiarch.sh 5001

# 脚本会自动：
# 1. 创建支持多平台的 builder
# 2. 构建 linux/amd64 (x86) 镜像
# 3. 推送到本地 Registry
```

#### 3. 指定目标平台

```bash
# 仅构建 x86 (默认)
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001

# 仅构建 ARM
TARGET_PLATFORM=linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001

# 同时构建两种架构（用逗号分隔）
TARGET_PLATFORM=linux/amd64,linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

#### 4. 验证镜像架构

```bash
# 查看镜像支持的平台
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest

# 预期输出
# Name:      localhost:5001/addp-system-backend:latest
# MediaType: application/vnd.docker.distribution.manifest.list.v2+json
# Digest:    sha256:xxxxx
#
# Manifests:
#   Name:      localhost:5001/addp-system-backend:latest@sha256:xxxxx
#   MediaType: application/vnd.docker.distribution.manifest.v2+json
#   Platform:  linux/amd64  ← 支持 x86
```

---

## 方案二：在服务器上直接构建

如果服务器配置较高，可以直接在服务器上构建镜像。

### 步骤

```bash
# ========== 开发机 ==========
# 传输源码到服务器
rsync -avz --exclude 'node_modules' --exclude 'bin' \
  /Users/pampa/code/addp/ user@server-ip:/opt/addp-build/

# ========== 服务器 ==========
ssh user@server-ip
cd /opt/addp-build

# 在服务器上构建镜像
make docker-build-all

# 推送到本地 Registry（如果需要）
./scripts/push-to-local-registry.sh localhost:5000
```

**优点：**
- 无架构兼容问题
- 构建速度快（本地架构）

**缺点：**
- 服务器需要安装 Go、Node.js 等开发工具
- 首次构建慢（需要下载依赖）
- 占用服务器资源

---

## 方案三：Docker Desktop 设置默认平台

### 配置方法

```bash
# 在 ~/.docker/config.json 中添加
{
  "buildPlatform": "linux/amd64"
}

# 或者设置环境变量
export DOCKER_DEFAULT_PLATFORM=linux/amd64

# 然后使用原始脚本
./scripts/push-to-local-registry.sh 5001
```

**缺点：**
- 使用 QEMU 模拟，构建速度慢（约 2-5 倍）
- 某些镜像构建可能失败
- 不如 Buildx 稳定

---

## 完整部署流程（ARM Mac → Intel 服务器）

### 开发机操作（ARM Mac）

```bash
cd /Users/pampa/code/addp

# 1. 搭建本地 Registry
./scripts/setup-local-registry.sh 5001

# 2. 使用多平台构建脚本（自动构建 x86 镜像）
./scripts/push-to-local-registry-multiarch.sh 5001

# 输出会显示
# ✅ 构建并推送成功
# 目标平台: linux/amd64

# 3. 获取本机 IP（脚本会自动显示）
# 例如: 192.168.1.100
```

### 服务器操作（Intel Mac）

```bash
# 1. 传输部署文件
scp docker-compose.prod.yml user@server:/opt/addp/
scp -r scripts user@server:/opt/addp/

# 2. SSH 登录服务器
ssh user@server
cd /opt/addp

# 3. 配置 Docker 信任私有 Registry
sudo vim /etc/docker/daemon.json
# 添加: {"insecure-registries": ["192.168.1.100:5001"]}

sudo systemctl restart docker  # Linux
# 或 Docker Desktop 重启 (macOS)

# 4. 部署
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh

# 5. 验证镜像架构
docker inspect addp-system-backend:latest | grep Architecture
# 输出: "Architecture": "amd64"  ← 正确！
```

---

## 故障排查

### 问题 1: Buildx 构建失败

```
ERROR: failed to solve: process "/bin/sh -c ..." did not complete successfully
```

**解决方法：**

```bash
# 1. 更新 Docker Desktop
# 确保版本 >= 20.10

# 2. 重新创建 builder
docker buildx rm addp-multiarch-builder
./scripts/push-to-local-registry-multiarch.sh 5001

# 3. 启用实验性功能
# Docker Desktop -> Settings -> Docker Engine
# 添加: "experimental": true
```

---

### 问题 2: 推送到 insecure registry 失败

```
http: server gave HTTP response to HTTPS client
```

**解决方法：**

脚本已自动配置，如果仍然失败，手动配置：

```bash
# 创建 buildkitd 配置
mkdir -p ~/.docker/buildx

cat > ~/.docker/buildx/buildkitd.toml <<EOF
[registry."localhost:5001"]
  http = true
  insecure = true
EOF

# 重新运行脚本
./scripts/push-to-local-registry-multiarch.sh 5001
```

---

### 问题 3: 服务器拉取后仍然报架构错误

```
WARNING: The requested image's platform (linux/arm64) does not match...
```

**可能原因：**
1. 构建时未指定正确平台
2. Registry 中有旧的 ARM 镜像

**解决方法：**

```bash
# 1. 在开发机检查镜像架构
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest

# 2. 如果是 ARM 架构，重新构建
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001

# 3. 在服务器清除旧镜像
docker rmi localhost:5001/addp-system-backend:latest
docker pull localhost:5001/addp-system-backend:latest

# 4. 验证架构
docker inspect localhost:5001/addp-system-backend:latest | grep Architecture
# 应该输出: "Architecture": "amd64"
```

---

### 问题 4: 构建速度很慢

**原因**：跨平台构建需要使用 QEMU 模拟器。

**优化方法：**

```bash
# 1. 使用本地缓存
docker buildx prune  # 清理旧缓存
docker buildx build --cache-from type=local,src=/tmp/buildx-cache ...

# 2. 减少构建层数
# 优化 Dockerfile，合并 RUN 命令

# 3. 使用多阶段构建
# 已在现有 Dockerfile 中使用

# 4. 考虑使用方案二（服务器本地构建）
```

---

## 性能对比

测试环境：MacBook Pro M1, 16GB RAM

| 构建方式 | 耗时 | 说明 |
|---------|------|------|
| 本地架构 (ARM → ARM) | ~120s | 最快 |
| 跨架构 (ARM → x86) | ~300s | Buildx + QEMU |
| 服务器本地构建 (x86 → x86) | ~150s | 推荐生产环境 |

**建议：**
- **开发测试**：使用多平台构建（方案一）
- **生产部署**：在服务器本地构建（方案二）

---

## 常见架构组合

### 组合 1: ARM Mac → Intel Linux 服务器 ⭐ 最常见

```bash
# 开发机
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

### 组合 2: ARM Mac → ARM Linux 服务器 (树莓派、云服务器)

```bash
# 开发机（无需跨平台）
./scripts/push-to-local-registry.sh 5001
```

### 组合 3: Intel Mac → Intel Linux 服务器

```bash
# 开发机（无需跨平台）
./scripts/push-to-local-registry.sh 5001
```

### 组合 4: Intel Mac → ARM 服务器

```bash
# 开发机
TARGET_PLATFORM=linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

---

## 快速决策树

```
你的开发机架构是？
├─ ARM (M1/M2/M3)
│   ├─ 服务器是 Intel/AMD → 使用多平台脚本 (linux/amd64)
│   └─ 服务器是 ARM → 使用普通脚本
│
└─ Intel
    ├─ 服务器是 Intel/AMD → 使用普通脚本
    └─ 服务器是 ARM → 使用多平台脚本 (linux/arm64)
```

---

## 验证清单

部署前检查：

- [ ] 确认开发机 CPU 架构：`uname -m`
- [ ] 确认服务器 CPU 架构：`ssh user@server uname -m`
- [ ] 选择正确的构建脚本
- [ ] 验证镜像架构：`docker buildx imagetools inspect ...`
- [ ] 在服务器验证：`docker inspect ... | grep Architecture`

---

## 参考资料

- [Docker Buildx 官方文档](https://docs.docker.com/buildx/working-with-buildx/)
- [多平台镜像构建指南](https://docs.docker.com/build/building/multi-platform/)
- [QEMU 用户模式](https://www.qemu.org/docs/master/user/index.html)

---

**总结：对于你的场景（ARM Mac → Intel Mac），使用 `push-to-local-registry-multiarch.sh` 脚本构建 `linux/amd64` 镜像。**
