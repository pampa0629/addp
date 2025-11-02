# ADDP 智能部署系统完整指南

## 概述

ADDP 智能部署系统支持**跨网络环境的自动化部署**，可以从 ARM Mac 开发机部署到不同网络中的 AMD64/ARM64 服务器，具备以下特性：

- ✅ **智能缓存** - 基于文件修改时间追踪，避免无谓的重复构建
- ✅ **自动清理** - 维护健康的构建环境，自动清除旧文件
- ✅ **动态IP检测** - 适应不同网络环境，自动检测开发机IP
- ✅ **多架构支持** - AMD64 + ARM64 自动构建和部署
- ✅ **中心化Registry** - 开发机作为镜像仓库，支持远程服务器拉取

## 架构说明

### 中心化 Registry 模式

```
开发机 (动态IP，例如 192.168.1.92)        目标服务器 (192.168.1.182)
├── Docker Registry (0.0.0.0:5001)       ├── docker-compose.prod.yml:
│   存储所有 ADDP 多架构镜像               │   REGISTRY=192.168.1.92:5001
│                                       │   image: 192.168.1.92:5001/addp-xxx
├── 智能构建系统                         │
│   - 自动检测代码变更                    ├── 从开发机 Registry 拉取镜像
│   - 只重建变更的服务                    │   docker pull 192.168.1.92:5001/...
│   - 自动清理旧文件                      │
│                                       └── ✅ 启动服务
├── IP自动检测
│   - 检测当前网络环境
│   - 与目标服务器子网匹配
│   - 自动设置 REGISTRY
│
└── 部署脚本
    - 本地部署: REGISTRY=localhost:5001
    - 远程部署: REGISTRY=<自动检测IP>:5001
```

### 网络环境自适应

| 场景 | 开发机IP | 目标服务器 | REGISTRY (自动检测) |
|------|---------|-----------|-------------------|
| 本地测试 | 192.168.1.92 | localhost | localhost:5001 |
| 办公室部署 | 192.168.1.92 | 192.168.1.182 | 192.168.1.92:5001 |
| 家里网络 | 192.168.31.100 | 192.168.31.200 | 192.168.31.100:5001 |
| 客户现场 | 10.0.0.50 | 10.0.0.100 | 10.0.0.50:5001 |

**关键特性**：无论在哪个网络，脚本都能自动检测并配置正确的 REGISTRY！

## 使用方法

### 1. 本地部署（开发测试）

```bash
./scripts/deploy/deploy-all.sh --server localhost
```

**行为**：
- ✅ 自动使用 `REGISTRY=localhost:5001`
- ✅ 智能检测代码变更
- ✅ 自动清理旧部署包和Docker镜像
- ✅ 部署到 `~/addp` 目录

**输出示例**：
```
Mode: Local Deployment
Target Directory: /Users/pampa/addp
Registry: localhost:5001

Checking for old deployment packages...
✓ Cleaned 2 old packages

Cleaning 5 dangling Docker image(s)...
✓ Cleaned dangling images

Building services...
Cache efficiency: 11 cached, 1 rebuilt  (⚡ Portal重建，其他缓存)
```

### 2. 远程部署（生产服务器）

```bash
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

**行为**：
- ✅ **自动检测开发机IP** (例如 192.168.1.92)
- ✅ 自动设置 `REGISTRY=192.168.1.92:5001`
- ✅ 检测与目标服务器是否在同一子网
- ✅ 编译、构建、传输、部署一键完成

**输出示例**：
```
✓ Auto-detected development machine IP: 192.168.1.92
  Using REGISTRY=192.168.1.92:5001 for remote deployment

Mode: Remote Deployment
Target Server: pampa@192.168.1.182
Registry: 192.168.1.92:5001

Building services...
✓ Transferred to server
✓ Services started successfully
```

### 3. 手动指定 Registry（特殊场景）

```bash
# 开发机和目标服务器不在同一子网，但有路由可达
./scripts/deploy/deploy-all.sh --server user@10.0.0.100 --registry 192.168.1.92:5001
```

## 智能特性详解

### 1. 智能缓存系统

**原理**：追踪每个服务源代码的最新修改时间，与上次构建时间比较。

**行为**：
```bash
# 场景：修改了 Portal.vue 后部署

Building portal for amd64... (source changed)     # ← 检测到修改
✓ Built and pushed portal (amd64) - REBUILT

Building system-backend for amd64... (using cache - no source changes)
✓ Built and pushed system-backend (amd64) - CACHED  # ← 未修改，使用缓存

Cache efficiency: 11 cached, 1 rebuilt  # ← 总结：只重建了1个服务
```

**速度提升**：
- 全部重建：15-30分钟
- 修改1个服务：3-5分钟（**5-10倍提升**）
- 无修改重复部署：1-2分钟（**10-15倍提升**）

### 2. 自动清理机制

**清理内容**：
1. **旧部署包** - 保留最新3个，删除更早的（每个约150MB）
2. **Docker悬空镜像** - 清理 `<none>` 标签的镜像（每次最多20个）

**时机**：每次构建开始前自动执行

**输出**：
```bash
Checking for old deployment packages...
Found 5 old package(s), removing...
✓ Cleaned old packages  # ← 释放 750MB 磁盘空间

Cleaning 3 dangling Docker image(s)...
✓ Cleaned dangling images  # ← 释放 2GB 磁盘空间
```

### 3. 动态IP检测

**检测策略**：
1. 扫描所有网络接口，过滤出私有IP（192.168.x.x, 10.x.x.x, 172.16-31.x.x）
2. 如果指定了目标服务器，优先选择同一子网的IP
3. 如果有多个IP，优先级：192.168.x.x > 10.x.x.x > 172.x.x.x

**使用工具**：
```bash
# 单独测试IP检测
./scripts/deploy/detect-dev-ip.sh --verbose

# 输出：
[DEBUG] Detected OS: macOS
[DEBUG] All detected IPs: 192.168.1.92
[DEBUG] Found private IP: 192.168.1.92
[DEBUG] Selected 192.168.x.x IP: 192.168.1.92
192.168.1.92
✓ Detected development machine IP: 192.168.1.92

# 指定目标服务器（会优先选择同子网IP）
./scripts/deploy/detect-dev-ip.sh --target-server 192.168.1.182
192.168.1.92  # ← 同一 192.168.1.x 子网
```

## 网络切换场景

### 场景1：办公室网络 → 家里网络

**办公室网络**：
```bash
$ ifconfig | grep "inet 192"
inet 192.168.1.92

$ ./scripts/deploy/deploy-all.sh --server admin@192.168.1.182
✓ Auto-detected development machine IP: 192.168.1.92
  Using REGISTRY=192.168.1.92:5001
```

**回家后切换到家里网络**：
```bash
$ ifconfig | grep "inet 192"
inet 192.168.31.100  # ← IP已变化

$ ./scripts/deploy/deploy-all.sh --server admin@192.168.31.200
✓ Auto-detected development machine IP: 192.168.31.100  # ← 自动检测新IP
  Using REGISTRY=192.168.31.100:5001
```

**无需任何配置修改！脚本自动适应新网络环境。**

### 场景2：跨子网部署

开发机和服务器在不同子网，但有路由可达：

```bash
# 开发机: 192.168.1.92 (办公网络)
# 服务器: 10.0.0.100 (生产网络)
# 两个网络通过路由器连接

$ ./scripts/deploy/deploy-all.sh --server admin@10.0.0.100
Warning: Dev machine (192.168.1.92) and target (10.0.0.100) are in different subnets
Registry may not be accessible from target server
✓ Using REGISTRY=192.168.1.92:5001

# 如果不可达，手动指定可达的IP或使用VPN IP
$ ./scripts/deploy/deploy-all.sh --server admin@10.0.0.100 --registry 10.0.0.99:5001
```

## 故障排查

### 问题1：IP检测失败

**错误信息**：
```
Error: Failed to detect development machine IP
Please specify registry manually: --registry <dev-machine-ip>:5001
```

**原因**：未连接到网络或没有私有IP地址

**解决**：
```bash
# 1. 检查网络连接
ifconfig | grep "inet " | grep -v 127.0.0.1

# 2. 手动指定IP
./scripts/deploy/deploy-all.sh --server <target> --registry <your-ip>:5001
```

### 问题2：Registry不可访问

**错误信息**（在目标服务器上）：
```
Error response from daemon: Get "http://192.168.1.92:5001/v2/": dial tcp 192.168.1.92:5001: connect: no route to host
```

**原因**：开发机防火墙阻止或网络不可达

**解决**：
```bash
# 1. 检查开发机Registry是否运行
docker ps | grep registry
# 应该看到: addp-registry   0.0.0.0:5001->5000/tcp

# 2. 检查防火墙（macOS）
sudo pfctl -s all  # 查看防火墙规则

# 3. 测试连通性（从目标服务器）
curl http://192.168.1.92:5001/v2/_catalog

# 4. 如果不可达，检查路由
ping 192.168.1.92
traceroute 192.168.1.92
```

### 问题3：缓存导致旧代码被部署

**现象**：明明修改了代码，但部署后还是旧版本

**原因**：智能缓存系统未检测到文件修改时间变化

**解决**：
```bash
# 方法1：强制重建所有服务
./scripts/deploy/1-build-images-multiarch.sh --force

# 方法2：清除构建缓存
rm -rf .build-cache/

# 方法3：检查文件修改时间
stat -f "%m %N" portal/frontend/src/views/Portal.vue
cat .build-cache/portal-amd64.timestamp

# 如果源文件时间戳 > 缓存时间戳，应该会重建
```

### 问题4：多网卡情况下选择错误的IP

**现象**：检测到的IP不是期望的网络接口

**原因**：有多个网络接口（WiFi + 以太网 + VPN等）

**解决**：
```bash
# 1. 查看所有IP
./scripts/deploy/detect-dev-ip.sh --verbose

# 2. 手动指定正确的IP
./scripts/deploy/deploy-all.sh --server <target> --registry <correct-ip>:5001

# 3. 或修改 detect-dev-ip.sh 的优先级逻辑
```

## 高级用法

### 只构建，不部署

```bash
./scripts/deploy/1-build-images-multiarch.sh
```

### 跳过构建，只打包部署

```bash
./scripts/deploy/deploy-all.sh --server <target> --skip-build
```

### 分步执行

```bash
# Step 1: 编译 Go 二进制
./scripts/deploy/0-compile-binaries.sh --arch both

# Step 2: 构建Docker镜像
./scripts/deploy/1-build-images-multiarch.sh

# Step 3: 打包并传输
./scripts/deploy/2-package-deploy.sh --server <target>

# Step 4: 在服务器上执行（通过SSH）
ssh <target>
cd ~/addp
./scripts/3-server-setup.sh
```

## 最佳实践

### ✅ 推荐做法

1. **日常开发** - 使用智能缓存，快速迭代
   ```bash
   ./scripts/deploy/deploy-all.sh --server localhost
   ```

2. **版本发布** - 使用 `--force` 确保全部重建
   ```bash
   ./scripts/deploy/1-build-images-multiarch.sh --force
   ./scripts/deploy/deploy-all.sh --server <production>
   ```

3. **切换网络** - 无需任何操作，脚本自动适应

4. **多环境部署** - 使用相同命令，脚本自动检测环境
   ```bash
   # 开发环境
   ./scripts/deploy/deploy-all.sh --server dev-server

   # 测试环境
   ./scripts/deploy/deploy-all.sh --server test-server

   # 生产环境
   ./scripts/deploy/deploy-all.sh --server prod-server
   ```

### ❌ 避免做法

1. **不要**手动修改 `.build-cache/` 文件
2. **不要**在不同网络环境使用硬编码的REGISTRY
3. **不要**忘记启动开发机的 Docker Registry
4. **不要**依赖缓存进行关键发布（使用 `--force`）

## 相关文档

- [智能构建系统详解](./SMART_BUILD_SYSTEM.md)
- [多架构镜像构建](./MULTI_ARCH_IMAGES.md)
- [部署脚本总结](./DEPLOY_SUMMARY.md)
- [Registry快速参考](./REGISTRY_QUICK_REFERENCE.md)

## 总结

ADDP 智能部署系统通过以下技术实现了**跨网络环境的自动化部署**：

1. **智能缓存** - 5-10倍构建速度提升
2. **自动清理** - 维护健康的构建环境
3. **动态IP检测** - 适应不同网络环境
4. **中心化Registry** - 统一镜像管理
5. **多架构支持** - AMD64 + ARM64 无缝部署

**核心优势**：
- ✅ **无需配置** - 自动检测和适应
- ✅ **跨网络部署** - 办公室、家里、客户现场
- ✅ **快速迭代** - 智能缓存大幅提速
- ✅ **磁盘优化** - 自动清理旧文件
- ✅ **幂等性** - 可重复执行

**一个命令，随处部署！** 🚀
