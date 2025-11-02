# 跨平台部署快速参考

## 🎯 你的场景：ARM Mac → Intel Mac

### 问题
- **开发机**: macOS ARM64 (M1/M2/M3)
- **服务器**: macOS x86_64 (Intel)

### 解决方案

使用 **多平台构建脚本** 构建 x86 镜像：

```bash
# 1. 搭建 Registry（避免 AirPlay 冲突）
./scripts/setup-local-registry.sh 5001

# 2. 构建 x86 镜像并推送（重要！）
./scripts/push-to-local-registry-multiarch.sh 5001

# 3. 在服务器上拉取部署
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

---

## 📋 所有脚本对比

### 1. `setup-local-registry.sh` - 搭建 Registry

**功能**：在本机启动私有 Docker Registry

**使用**：
```bash
./scripts/setup-local-registry.sh          # 默认 5000 端口
./scripts/setup-local-registry.sh 5001     # 自定义端口
```

**特性**：
- ✅ 自动检测端口冲突
- ✅ 显示局域网 IP
- ✅ 支持自定义端口

---

### 2. `push-to-local-registry.sh` - 普通推送（同架构）

**功能**：构建并推送镜像（本机架构）

**适用场景**：
- ✅ ARM Mac → ARM 服务器
- ✅ Intel Mac → Intel 服务器
- ❌ ARM Mac → Intel 服务器（**不适用**）

**使用**：
```bash
./scripts/push-to-local-registry.sh                    # 自动检测
./scripts/push-to-local-registry.sh 5001               # 指定端口
./scripts/push-to-local-registry.sh 192.168.1.100:5001 # 局域网 IP
```

**特性**：
- ✅ 智能参数解析
- ✅ 自动检测 Registry 端口
- ✅ 构建速度快
- ❌ 仅支持本机架构

---

### 3. `push-to-local-registry-multiarch.sh` - 多平台推送（跨架构）⭐

**功能**：构建并推送指定平台的镜像

**适用场景**：
- ✅ ARM Mac → Intel 服务器（**你的场景**）
- ✅ Intel Mac → ARM 服务器
- ✅ 需要同时支持多种架构

**使用**：
```bash
# 默认构建并推送双架构镜像（推荐）
./scripts/push-to-local-registry-multiarch.sh 5001

# 指定目标平台
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001  # 仅 x86
TARGET_PLATFORM=linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001  # 仅 ARM
TARGET_PLATFORM=linux/amd64,linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001  # 双架构（默认）
```

**特性**：
- ✅ 支持跨平台构建
- ✅ 使用 Docker Buildx
- ✅ 自动配置 insecure registry
- ✅ **默认推送双架构镜像**（ARM + x86 都可用）
- ⚠️  构建速度较慢（使用 QEMU 模拟）

**多架构镜像说明**：
- 一个镜像标签可以同时包含 ARM 和 x86 两种架构
- Docker 拉取时自动选择匹配的架构
- 存储空间约为单架构的 1.7-2 倍
- 详见：[docs/MULTI_ARCH_IMAGES.md](MULTI_ARCH_IMAGES.md)

---

### 4. `deploy-from-registry.sh` - 服务器部署

**功能**：从 Registry 拉取镜像并部署

**使用**（在服务器上）：
```bash
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

**特性**：
- ✅ 自动配置 Docker 信任 Registry
- ✅ 健康检查
- ✅ 完整的部署流程

---

## 🔍 如何选择脚本？

### 决策树

```
1. 检查架构
   开发机: uname -m  (arm64 或 x86_64)
   服务器: uname -m  (x86_64 或 aarch64)

2. 架构是否相同？
   ├─ 是 → 使用 push-to-local-registry.sh
   └─ 否 → 使用 push-to-local-registry-multiarch.sh ⭐
```

### 快速判断表

| 开发机 CPU | 服务器 CPU | 使用脚本 |
|-----------|-----------|---------|
| M1/M2/M3 (ARM) | Intel x86 | `push-to-local-registry-multiarch.sh` ⭐ |
| M1/M2/M3 (ARM) | ARM | `push-to-local-registry.sh` |
| Intel x86 | Intel x86 | `push-to-local-registry.sh` |
| Intel x86 | ARM | `push-to-local-registry-multiarch.sh` |

---

## ⚡ 完整部署流程（你的场景）

### 开发机（ARM Mac）

```bash
cd /Users/pampa/code/addp

# 步骤 1: 搭建 Registry
./scripts/setup-local-registry.sh 5001

# 步骤 2: 构建 x86 镜像（重要！）
./scripts/push-to-local-registry-multiarch.sh 5001

# 步骤 3: 验证镜像架构
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest
# 应该看到: Platform:  linux/amd64

# 步骤 4: 获取本机 IP（脚本会显示）
# 例如: 192.168.1.100
```

### 服务器（Intel Mac）

```bash
# 步骤 1: 传输部署文件
scp docker-compose.prod.yml user@server:/opt/addp/
scp -r scripts user@server:/opt/addp/

# 步骤 2: SSH 登录
ssh user@server
cd /opt/addp

# 步骤 3: 部署
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh

# 步骤 4: 验证镜像架构
docker inspect addp-system-backend | grep Architecture
# 应该输出: "Architecture": "amd64" ✅
```

---

## 🛠️ 故障排查

### 问题: 服务器上报架构错误

```
exec format error
```

**原因**：使用了错误的脚本，构建了 ARM 镜像

**解决**：
```bash
# 在开发机重新构建 x86 镜像
./scripts/push-to-local-registry-multiarch.sh 5001

# 在服务器重新拉取
docker-compose -f docker-compose.prod.yml pull
docker-compose -f docker-compose.prod.yml up -d
```

---

### 问题: Buildx 构建失败

```
ERROR: failed to solve...
```

**解决**：
```bash
# 更新 Docker Desktop 到最新版本
# 或手动安装 Buildx

# 检查版本
docker buildx version

# 重试
./scripts/push-to-local-registry-multiarch.sh 5001
```

---

## 📊 性能对比

| 构建方式 | 耗时 | 适用场景 |
|---------|------|---------|
| 普通构建（同架构） | ~120s | 开发机和服务器架构相同 |
| 多平台构建（跨架构） | ~300s | 开发机和服务器架构不同 ⭐ |
| 服务器本地构建 | ~150s | 服务器配置较高 |

**建议**：
- 首次部署：使用多平台构建
- 频繁迭代：考虑在服务器本地构建

---

## 📚 相关文档

- [完整跨平台部署指南](CROSS_PLATFORM_DEPLOYMENT.md)
- [端口 5000 故障排查](TROUBLESHOOT_PORT_5000.md)
- [推送脚本详细指南](PUSH_TO_REGISTRY_GUIDE.md)
- [完整部署指南](DEPLOY_WITH_LOCAL_REGISTRY.md)

---

## ✅ 验证清单

部署前确认：

- [ ] 已检查开发机架构：`uname -m`
- [ ] 已检查服务器架构：`ssh user@server uname -m`
- [ ] 选择了正确的推送脚本
- [ ] Registry 使用非冲突端口（推荐 5001）
- [ ] 镜像架构已验证：`docker buildx imagetools inspect ...`
- [ ] 服务器上镜像架构正确：`docker inspect ... | grep Architecture`

**你的场景总结：使用 `push-to-local-registry-multiarch.sh` 构建 x86 镜像部署到 Intel 服务器。**
