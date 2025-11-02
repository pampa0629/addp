# macOS 服务器 chown 错误快速修复指南

## 问题

在 macOS 服务器上运行 `deploy-from-registry.sh` 时报错：

```
chown: illegal group name
```

## 解决方案（3 种方法）

---

### 方法一：重新传输最新版本的脚本（推荐）

本地脚本已经修复，只需重新传输到服务器：

```bash
# 从开发机传输最新脚本到服务器
scp /Users/pampa/code/addp/scripts/deploy-from-registry.sh user@server:/opt/addp/scripts/

# 在服务器上运行
ssh user@server
cd /opt/addp
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

---

### 方法二：在服务器上使用自动修复脚本

```bash
# 从开发机传输修复脚本到服务器
scp /Users/pampa/code/addp/scripts/fix-macos-chown.sh user@server:/tmp/

# 在服务器上运行修复脚本
ssh user@server
chmod +x /tmp/fix-macos-chown.sh
/tmp/fix-macos-chown.sh

# 然后运行部署
cd /opt/addp
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

---

### 方法三：手动修复服务器上的脚本

如果无法传输文件，可以手动编辑：

```bash
# 在服务器上
ssh user@server
vim /opt/addp/scripts/deploy-from-registry.sh

# 找到这一行（大约在第 140-150 行）：
sudo mkdir -p "$DEPLOY_DIR"
sudo chown -R $USER:$USER "$DEPLOY_DIR"

# 替换为：
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

# 保存并退出（:wq）
```

---

## 验证修复

运行以下命令检查脚本是否已修复：

```bash
# 在服务器上
grep -A 5 'if \[\[ "\$OSTYPE" == "darwin"' /opt/addp/scripts/deploy-from-registry.sh

# 如果看到类似输出，说明已修复：
# if [[ "$OSTYPE" == "darwin"* ]]; then
#   # macOS
#   mkdir -p "$DEPLOY_DIR" 2>/dev/null || sudo mkdir -p "$DEPLOY_DIR"
#   sudo chown -R $USER "$DEPLOY_DIR" 2>/dev/null || true
# else
```

---

## 完整部署流程（macOS 服务器）

### 前提条件

1. ✅ 开发机已搭建 Registry 并推送镜像
2. ✅ 服务器已安装 Docker Desktop
3. ✅ 服务器已配置信任 Registry

### 步骤 1: 传输部署文件

```bash
# 从开发机执行
cd /Users/pampa/code/addp

# 传输 docker-compose 配置
scp docker-compose.prod.yml user@server:/opt/addp/

# 传输 .env 模板
scp .env.example user@server:/opt/addp/

# 传输脚本（最新版本）
scp scripts/deploy-from-registry.sh user@server:/opt/addp/scripts/
scp scripts/init-db.sql user@server:/opt/addp/scripts/

# 或使用 rsync
rsync -avz --exclude 'node_modules' --exclude 'bin' \
  docker-compose.prod.yml \
  .env.example \
  scripts/ \
  user@server:/opt/addp/
```

### 步骤 2: 在服务器上配置环境

```bash
# SSH 登录服务器
ssh user@server
cd /opt/addp

# 创建 .env 文件
cp .env.example .env
vim .env

# 修改以下配置：
# JWT_SECRET=<强随机密钥>
# POSTGRES_PASSWORD=<强密码>
# REDIS_PASSWORD=<强密码>
# MINIO_SYSTEM_ROOT_PASSWORD=<强密码>
# ENCRYPTION_KEY=<强随机密钥>

# 生成随机密钥
openssl rand -base64 32
```

### 步骤 3: 配置 Docker 信任 Registry

**Docker Desktop 方式（macOS）：**

1. 打开 Docker Desktop
2. Preferences → Docker Engine
3. 添加配置：

```json
{
  "insecure-registries": ["192.168.1.100:5001"]
}
```

4. Apply & Restart

**或者命令行方式：**

```bash
# 检查配置文件
cat ~/.docker/daemon.json

# 如果不存在，创建
mkdir -p ~/.docker
cat > ~/.docker/daemon.json <<EOF
{
  "insecure-registries": ["192.168.1.100:5001"]
}
EOF

# 重启 Docker Desktop
```

### 步骤 4: 运行部署脚本

```bash
cd /opt/addp

# 设置可执行权限
chmod +x scripts/deploy-from-registry.sh

# 运行部署（替换为你的 Registry 地址）
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

### 步骤 5: 验证部署

```bash
# 查看服务状态
docker-compose -f docker-compose.prod.yml ps

# 查看日志
docker-compose -f docker-compose.prod.yml logs -f

# 测试健康检查
curl http://localhost:8080/health  # System backend
curl http://localhost:8000/health  # Gateway
```

---

## 常见问题

### Q1: 为什么 macOS 和 Linux 的 chown 不同？

**A:**

- **Linux**: 用户和组可以同名，`chown user:user` 有效
- **macOS**: 用户的主组通常是 `staff`，不是用户名本身

```bash
# macOS 查看用户组
id
# 输出: uid=501(pampa) gid=20(staff) groups=20(staff),...

# Linux 查看用户组
id
# 输出: uid=1000(pampa) gid=1000(pampa) groups=1000(pampa),...
```

### Q2: 如果仍然报错怎么办？

**A:** 检查以下几点：

1. **确认运行的是正确的脚本**
   ```bash
   which deploy-from-registry.sh
   # 或
   ls -la /opt/addp/scripts/deploy-from-registry.sh
   ```

2. **确认脚本已修复**
   ```bash
   grep "darwin" /opt/addp/scripts/deploy-from-registry.sh
   # 应该能看到 macOS 兼容性代码
   ```

3. **检查脚本版本**
   ```bash
   head -20 /opt/addp/scripts/deploy-from-registry.sh
   # 查看是否有最新的注释
   ```

### Q3: 可以不使用 sudo 吗？

**A:** 在 macOS 上，如果部署目录是 `/opt/addp`，通常需要 sudo。

**替代方案**：使用用户目录

```bash
# 修改部署目录
export DEPLOY_DIR="$HOME/addp-deploy"
./scripts/deploy-from-registry.sh
```

---

## 相关文档

- [macOS 部署兼容性修复](MACOS_DEPLOYMENT_FIX.md)
- [完整部署指南](DEPLOY_WITH_LOCAL_REGISTRY.md)
- [端口 5000 故障排查](TROUBLESHOOT_PORT_5000.md)

---

## 快速参考

### 一键修复命令（复制粘贴）

```bash
# 在开发机执行
scp /Users/pampa/code/addp/scripts/deploy-from-registry.sh user@server:/opt/addp/scripts/

# 在服务器执行
ssh user@server "chmod +x /opt/addp/scripts/deploy-from-registry.sh"
```

### 部署命令（在服务器执行）

```bash
cd /opt/addp
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```
