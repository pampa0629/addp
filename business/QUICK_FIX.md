# 快速修复指南

## 问题原因

Docker 缓存中保留了旧的 AMD64 架构镜像，导致与新的 ARM64 配置冲突。

错误信息：
```
Error response from daemon: image with reference postgis/postgis:15-3.4 was found
but its platform (linux/amd64) does not match the specified platform (linux/arm64)
```

## 已修复

修改了 `scripts/restart.sh`，新增功能：

1. ✅ **先停止服务** - 避免容器占用镜像
2. ✅ **清理旧镜像** - 检测架构不匹配时自动删除
3. ✅ **重新拉取** - 拉取正确架构的镜像
4. ✅ **启动服务** - 使用新镜像启动

## 现在运行

```bash
cd /Users/pampa/code/addp/business
./scripts/restart.sh
```

## 预期流程

```
🔍 Detecting CPU architecture: arm64
✓ Target platform: ARM64 (linux/arm64)

📦 Current PostgreSQL image: postgis/postgis:15-3.4 (amd64)
⚠️  Architecture mismatch detected!
   Current: amd64, Target: arm64
   Will pull and restart with correct architecture...

🛑 Stopping services...
[停止确认] y

🧹 Cleaning up old architecture images...
   Removing old PostGIS image (amd64)...  ← 删除旧镜像
   ✓ Cleanup complete

📥 Pulling images for ARM64 architecture...
   Pulling postgis/postgis:15-3.4 for linux/arm64...
   [下载进度...]
   ✓ Images pulled successfully

🚀 Starting services with correct architecture...
   ✓ PostgreSQL ready
   ✓ MinIO ready
   ✓ PostGIS extensions installed

✅ Restart Complete!
Architecture: ARM64 (linux/arm64)
```

## 验证

运行后验证：

```bash
./scripts/verify-arch.sh
```

期望输出：
```
🖥️  System Architecture: arm64
🎯 Expected Docker Arch: arm64

📦 Checking running containers...
  PostgreSQL:
    Arch: arm64
    Status: ✓ MATCH  ← 修复成功！
```

## 如果还有问题

手动清理（备用方案）：

```bash
# 1. 停止服务
cd business
docker-compose down

# 2. 强制删除镜像
docker rmi -f postgis/postgis:15-3.4
docker rmi -f minio/minio:latest

# 3. 手动拉取 ARM64 镜像
docker pull --platform=linux/arm64 postgis/postgis:15-3.4
docker pull --platform=linux/arm64 minio/minio:latest

# 4. 启动服务
export DOCKER_DEFAULT_PLATFORM=linux/arm64
docker-compose up -d
```

## 修改说明

### 修改前（导致错误）
- 先拉取镜像 → 但缓存有旧的 AMD64 镜像
- docker-compose up → 发现架构不匹配，报错

### 修改后（正确流程）
- 先停止服务 → 释放镜像占用
- 清理旧镜像 → 删除架构不匹配的镜像
- 拉取新镜像 → 拉取正确的 ARM64 镜像
- 启动服务 → 成功使用新镜像

现在可以安全运行了！🚀
