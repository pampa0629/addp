# Registry 镜像拉取 500 错误排查指南

## 问题现象

```
unable to get image '192.168.1.92:5001/addp-system-backend:latest':
request returned 500 Internal Server Error
```

---

## 诊断步骤

### 步骤 1: 在服务器上检查 Registry 连接

```bash
# 在服务器执行
curl http://192.168.1.92:5001/v2/

# 预期输出: {}
# 如果失败，说明无法连接到 Registry
```

### 步骤 2: 检查 Registry 中的镜像

```bash
# 查看所有镜像
curl http://192.168.1.92:5001/v2/_catalog

# 查看特定镜像的标签
curl http://192.168.1.92:5001/v2/addp-system-backend/tags/list
```

### 步骤 3: 测试手动拉取

```bash
# 手动拉取镜像
docker pull 192.168.1.92:5001/addp-system-backend:latest

# 查看详细错误
docker pull 192.168.1.92:5001/addp-system-backend:latest --debug
```

---

## 常见原因和解决方案

### 原因 1: Docker 未配置信任私有 Registry（最可能）

#### macOS 服务器解决方案

**Docker Desktop 配置：**

1. 打开 **Docker Desktop**
2. **Preferences/Settings** → **Docker Engine**
3. 添加配置：

```json
{
  "insecure-registries": ["192.168.1.92:5001"]
}
```

4. **Apply & Restart**

**验证配置：**

```bash
# 查看 Docker 配置
docker info | grep -A 5 "Insecure Registries"

# 应该看到
# Insecure Registries:
#  192.168.1.92:5001
```

#### Linux 服务器解决方案

```bash
# 编辑 daemon.json
sudo vim /etc/docker/daemon.json

# 添加或修改
{
  "insecure-registries": ["192.168.1.92:5001"]
}

# 重启 Docker
sudo systemctl restart docker

# 验证
docker info | grep "Insecure Registries"
```

---

### 原因 2: Registry 未运行或不可访问

#### 在开发机检查 Registry 状态

```bash
# 检查 Registry 容器
docker ps | grep addp-registry

# 预期输出
# addp-registry   ...   Up   0.0.0.0:5001->5000/tcp

# 查看 Registry 日志
docker logs addp-registry

# 测试访问
curl http://localhost:5001/v2/
```

#### 检查防火墙

**开发机（macOS）：**

```bash
# 临时关闭防火墙
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --setglobalstate off

# 或添加允许规则（需要重新打开防火墙后配置）
```

**服务器（macOS）：**

同样需要确保防火墙不阻止对开发机的访问。

---

### 原因 3: 镜像不存在或未推送

#### 在开发机验证镜像

```bash
# 查看 Registry 中的镜像
curl http://localhost:5001/v2/_catalog

# 应该看到所有镜像
{
  "repositories": [
    "addp-gateway",
    "addp-manager-backend",
    "addp-manager-frontend",
    "addp-meta-backend",
    "addp-portal",
    "addp-system-backend",
    "addp-system-frontend"
  ]
}

# 查看特定镜像的标签
curl http://localhost:5001/v2/addp-system-backend/tags/list

# 应该看到
{
  "name": "addp-system-backend",
  "tags": ["latest"]
}
```

#### 如果镜像不存在，重新推送

```bash
# 在开发机执行
cd /Users/pampa/code/addp

# 重新推送镜像
./scripts/push-to-local-registry-multiarch.sh 5001
```

---

### 原因 4: 网络问题

#### 测试网络连通性

```bash
# 在服务器测试连接开发机的 Registry
telnet 192.168.1.92 5001

# 或
nc -zv 192.168.1.92 5001

# 或
curl -v http://192.168.1.92:5001/v2/
```

#### 检查 IP 地址是否正确

```bash
# 在开发机查看当前 IP
ipconfig getifaddr en0  # WiFi
ipconfig getifaddr en1  # 以太网

# 确认是否是 192.168.1.92
```

---

### 原因 5: Docker API 版本不兼容

错误中提到 `v1.51`，可能是 Docker 版本问题。

```bash
# 检查 Docker 版本
docker version

# 客户端和服务器版本应该兼容
# Client: Docker Engine - Community
#  Version:           24.0.7
# Server: Docker Desktop
#  Version:           24.0.7
```

---

## 完整修复流程

### 步骤 1: 在开发机确认 Registry 正常

```bash
# 在开发机执行
docker ps | grep addp-registry

# 查看镜像列表
curl http://localhost:5001/v2/_catalog | jq

# 获取当前 IP
ipconfig getifaddr en0
# 假设输出: 192.168.1.92
```

### 步骤 2: 在服务器配置信任 Registry

**macOS 服务器：**

```bash
# Docker Desktop → Settings → Docker Engine
# 添加:
{
  "insecure-registries": ["192.168.1.92:5001"]
}

# 重启 Docker Desktop
```

**或使用配置文件：**

```bash
# 在服务器执行
mkdir -p ~/.docker

cat > ~/.docker/daemon.json <<EOF
{
  "insecure-registries": ["192.168.1.92:5001"]
}
EOF

# 重启 Docker Desktop
```

### 步骤 3: 在服务器测试连接

```bash
# 测试 Registry API
curl http://192.168.1.92:5001/v2/

# 测试镜像列表
curl http://192.168.1.92:5001/v2/_catalog

# 测试手动拉取
docker pull 192.168.1.92:5001/addp-system-backend:latest
```

### 步骤 4: 重新运行部署脚本

```bash
# 在服务器执行
cd /opt/addp
REGISTRY=192.168.1.92:5001 ./scripts/deploy-from-registry.sh
```

---

## 修复 docker-compose.yml 警告

### 删除 version 字段

错误提示：
```
WARN[0000] the attribute `version` is obsolete
```

这是 Docker Compose v2 的警告，不影响功能，但可以修复：

```bash
# 在服务器上编辑文件
vim /opt/addp/docker-compose.prod.yml

# 删除第一行
# version: '3.8'  ← 删除这一行

# 或使用 sed
sed -i '' '1d' /opt/addp/docker-compose.prod.yml  # macOS
sed -i '1d' /opt/addp/docker-compose.prod.yml     # Linux
```

**或者在开发机修复后重新传输：**

```bash
# 在开发机修改
vim /Users/pampa/code/addp/docker-compose.prod.yml
# 删除第一行 version: '3.8'

# 重新传输
scp docker-compose.prod.yml pampa@192.168.1.182:/opt/addp/
```

---

## 快速诊断脚本

创建一个诊断脚本帮助排查：

```bash
#!/bin/bash
# 在服务器上运行

REGISTRY="192.168.1.92:5001"

echo "=== Registry 连接测试 ==="
curl -s http://$REGISTRY/v2/ && echo "✅ Registry API 可访问" || echo "❌ Registry API 不可访问"

echo ""
echo "=== 镜像列表 ==="
curl -s http://$REGISTRY/v2/_catalog | jq

echo ""
echo "=== Docker 配置 ==="
docker info | grep -A 5 "Insecure Registries"

echo ""
echo "=== 测试拉取镜像 ==="
docker pull $REGISTRY/addp-system-backend:latest
```

---

## 常见错误和解决

### 错误 1: curl 连接失败

```
curl: (7) Failed to connect to 192.168.1.92 port 5001: Connection refused
```

**原因**：
- 开发机 Registry 未运行
- 网络不通
- 防火墙阻止

**解决**：
```bash
# 在开发机检查 Registry
docker ps | grep addp-registry

# 如果未运行，启动
./scripts/setup-local-registry.sh 5001
```

---

### 错误 2: http: server gave HTTP response to HTTPS client

```
Error response from daemon: Get "https://192.168.1.92:5001/v2/":
http: server gave HTTP response to HTTPS client
```

**原因**：Docker 未配置信任 insecure registry

**解决**：添加 `"insecure-registries": ["192.168.1.92:5001"]` 到 Docker 配置

---

### 错误 3: manifest unknown

```
Error response from daemon: manifest for 192.168.1.92:5001/addp-system-backend:latest not found
```

**原因**：镜像未推送或标签错误

**解决**：
```bash
# 在开发机重新推送
./scripts/push-to-local-registry-multiarch.sh 5001
```

---

## 验证成功

部署成功的标志：

```bash
# 服务器上
docker-compose -f /opt/addp/docker-compose.prod.yml ps

# 所有服务 State 为 Up
NAME                    STATE     PORTS
addp-gateway            Up        0.0.0.0:8000->8000/tcp
addp-system-backend     Up        0.0.0.0:8080->8080/tcp
...
```

---

## 下一步

部署成功后：

```bash
# 访问服务
open http://192.168.1.182:5170  # Portal
open http://192.168.1.182:8000  # Gateway

# 查看日志
docker-compose -f /opt/addp/docker-compose.prod.yml logs -f
```
