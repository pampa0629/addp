# macOS 部署脚本兼容性修复

## 问题

在 macOS 上运行部署脚本时出现错误：

```
chown: pampa: illegal group name
```

## 原因

macOS 和 Linux 的 `chown` 命令语法不同：

- **Linux**: `chown user:group file`
- **macOS**: `chown user file` 或 `chown user:staff file`

脚本中使用了 `$USER:$USER`，在 macOS 上 `$USER` 不是有效的组名。

## 解决方案

已修复以下脚本以兼容 macOS 和 Linux：

1. ✅ `business/scripts/deploy-business.sh`
2. ✅ `scripts/deploy-from-registry.sh`

### 修复内容

**修改前（仅支持 Linux）：**
```bash
sudo mkdir -p "$DEPLOY_DIR"
sudo chown -R $USER:$USER "$DEPLOY_DIR"
```

**修改后（支持 macOS + Linux）：**
```bash
# 检测操作系统
if [[ "$OSTYPE" == "darwin"* ]]; then
  # macOS
  mkdir -p "$DEPLOY_DIR" 2>/dev/null || sudo mkdir -p "$DEPLOY_DIR"
  sudo chown -R $USER "$DEPLOY_DIR" 2>/dev/null || true
else
  # Linux
  sudo mkdir -p "$DEPLOY_DIR"
  sudo chown -R $USER:$USER "$DEPLOY_DIR"
fi
```

## 使用方法

### 在 macOS 上部署

现在可以直接运行脚本，无需 sudo：

```bash
# Business 基础设施部署
./business/scripts/deploy-business.sh

# ADDP 系统部署
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

### 在 Linux 上部署

使用方法不变：

```bash
# Business 基础设施部署
./business/scripts/deploy-business.sh

# ADDP 系统部署
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

## macOS 特别说明

### 1. 部署目录权限

在 macOS 上，脚本会尝试：
1. 先以当前用户创建目录
2. 如果失败（目录需要 sudo），则使用 sudo 创建
3. 设置正确的所有者

### 2. Docker Desktop 配置

macOS 使用 Docker Desktop，配置略有不同：

**配置 insecure registry：**
- Docker Desktop → Preferences → Docker Engine
- 添加配置：
  ```json
  {
    "insecure-registries": ["192.168.1.100:5001"]
  }
  ```

### 3. 端口说明

macOS 上某些端口可能被系统服务占用：

| 端口 | macOS 默认占用 | 建议替代端口 |
|------|---------------|-------------|
| 5000 | AirPlay Receiver | 5001 |
| 7000 | AirPlay | 7001 |

## 验证修复

运行脚本应该顺利通过目录创建步骤：

```bash
./business/scripts/deploy-business.sh

# 预期输出
==========================================
  步骤 2/6: 准备部署目录
==========================================

==> 创建部署目录...
✅ 部署目录: /opt/addp-business  ← 成功！
```

## 其他 macOS 兼容性注意事项

### 1. lsof vs netstat

脚本已自动处理端口检测的兼容性：

```bash
# 优先使用 lsof（macOS 和 Linux 都支持）
if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
  echo "端口被占用"
fi

# 备用 netstat（某些系统可能没有 lsof）
if netstat -tuln 2>/dev/null | grep -q ":$port "; then
  echo "端口被占用"
fi
```

### 2. Docker Compose 版本

macOS 通常使用 Docker Desktop 内置的 `docker compose`（v2）：

```bash
# 检测可用的命令
if command -v docker-compose &> /dev/null; then
  COMPOSE_CMD="docker-compose"
elif docker compose version &> /dev/null; then
  COMPOSE_CMD="docker compose"
fi
```

### 3. 主机名解析

macOS 的 `hostname -I` 不支持，使用替代方法：

```bash
# macOS
ipconfig getifaddr en0  # WiFi
ipconfig getifaddr en1  # 以太网

# Linux
hostname -I | awk '{print $1}'
```

## 完整部署流程（macOS）

### 1. Business 基础设施

```bash
cd /Users/pampa/code/addp

# 部署 Business 基础设施
./business/scripts/deploy-business.sh
```

### 2. ADDP 系统

```bash
# 搭建 Registry（使用 5001 避免 AirPlay）
./scripts/setup-local-registry.sh 5001

# 推送多架构镜像
./scripts/push-to-local-registry-multiarch.sh 5001

# 如果在本机部署（测试），使用 localhost
REGISTRY=localhost:5001 ./scripts/deploy-from-registry.sh
```

## 相关文档

- [端口 5000 故障排查](TROUBLESHOOT_PORT_5000.md)
- [跨平台部署指南](CROSS_PLATFORM_DEPLOYMENT.md)
- [Business 基础设施部署](../business/DEPLOY.md)
