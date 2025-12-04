s# ADDP 局域网私有镜像仓库部署指南

本文档介绍如何使用**本机私有 Docker Registry** 将 ADDP 系统部署到局域网服务器。

## 部署架构

```
┌─────────────────────────────────────┐
│  开发机 (MacBook)                   │
│  IP: 192.168.1.100 (示例)          │
│                                     │
│  ┌──────────────────────────────┐  │
│  │  Docker Registry (port 5000) │  │
│  │  - addp-system-backend       │  │
│  │  - addp-manager-backend      │  │
│  │  - addp-meta-backend         │  │
│  │  - addp-gateway              │  │
│  │  - addp-portal               │  │
│  │  - 所有 frontend 镜像        │  │
│  └──────────────────────────────┘  │
└─────────────────┬───────────────────┘
                  │
        局域网 (WiFi/有线)
                  │
┌─────────────────▼───────────────────┐
│  生产服务器                         │
│  IP: 192.168.1.200 (示例)          │
│                                     │
│  docker-compose pull from Registry │
│  docker-compose up -d               │
└─────────────────────────────────────┘
```

## 前置条件

### 开发机 (MacBook)
- ✅ Docker Desktop 已安装并运行
- ✅ 已完成 ADDP 代码开发
- ✅ 连接到局域网（WiFi 或有线）
- ⚠️  **注意 CPU 架构兼容性**（见下方说明）

### 服务器
- ✅ Ubuntu 20.04+ / CentOS 7+ / Debian 10+ / macOS
- ✅ Docker 20.10+ 和 Docker Compose 1.29+ 已安装
- ✅ 至少 4GB RAM，20GB 磁盘空间
- ✅ 连接到与开发机相同的局域网

### ⚠️ CPU 架构兼容性检查（重要！）

**检查你的设备架构：**

```bash
# 开发机检查
uname -m
# arm64 = Apple Silicon (M1/M2/M3)
# x86_64 = Intel Mac

# 服务器检查（SSH 登录后）
uname -m
# x86_64 = Intel/AMD 服务器
# aarch64 或 arm64 = ARM 服务器
```

**架构不匹配需要使用多平台构建：**

| 开发机 | 服务器 | 需要多平台构建？ | 使用脚本 |
|--------|-------|----------------|---------|
| ARM (M1/M2/M3) | Intel/AMD x86 | ✅ **是** | `scripts/deploy/1-build-images-multiarch.sh` |
| ARM | ARM | ❌ 否 | `scripts/deploy/1-build-images.sh` |
| Intel | Intel/AMD x86 | ❌ 否 | `scripts/deploy/1-build-images.sh` |
| Intel | ARM | ✅ 是 | `scripts/deploy/1-build-images-multiarch.sh` |

**详细的跨平台部署指南：** [docs/CROSS_PLATFORM_DEPLOYMENT.md](CROSS_PLATFORM_DEPLOYMENT.md)

---

## 部署概览

ADDP 系统采用**分层部署架构**，分为两个独立部分：

### 1️⃣ Business 基础设施（优先部署）
- PostgreSQL (5433) - 业务数据库
- MinIO (9002-9003) - 业务对象存储
- **部署方式**：直接拉取官方镜像，无需私有 Registry
- **详细文档**：[business/DEPLOY.md](../business/DEPLOY.md)

### 2️⃣ ADDP 系统（依赖 Business 基础设施）
- System Backend + Frontend
- Manager Backend + Frontend
- Meta Backend
- Gateway
- Portal
- **部署方式**：从本机私有 Registry 拉取自定义镜像

**部署顺序：**
```
Business 基础设施 → ADDP 系统
```

---

## 阶段 0: 部署 Business 基础设施（必选）

⚠️ **重要：Business 基础设施必须先于 ADDP 系统部署！**

### 快速部署 Business 基础设施

```bash
# ========== 开发机操作 ==========
cd /Users/pampa/code/addp/business

# 准备部署包（可选本地测试）
./scripts/prepare-for-deploy.sh

# 传输部署包到服务器
scp ../business-deploy-*.tar.gz user@192.168.1.200:/opt/

# ========== 服务器操作 ==========
ssh user@192.168.1.200
cd /opt
tar -xzf business-deploy-*.tar.gz
cd business-deploy-*/

# 配置环境变量（修改密码！）
cp .env.example .env
vim .env

# 启动 Business 基础设施
docker-compose -f docker-compose.prod.yml up -d

# 验证部署
docker-compose -f docker-compose.prod.yml ps
```

**或者使用一键部署脚本：**
```bash
# 传输脚本到服务器
scp business/scripts/deploy-business.sh user@server:/opt/

# 在服务器上执行
./deploy-business.sh
```

✅ **完成后，继续部署 ADDP 系统（阶段 1）**

详细的 Business 基础设施部署说明，请参考：[business/DEPLOY.md](../business/DEPLOY.md)

---

## 阶段 1: 开发机准备（本机操作）

### 1.1 搭建本地私有 Registry

```bash
# 在项目根目录执行
cd /Users/pampa/code/addp

# 运行 Registry 搭建脚本
./scripts/setup/local-registry.sh
```

**脚本会自动完成：**
- ✅ 创建 Registry 数据目录 (`~/.addp-registry/`)
- ✅ 启动 Docker Registry 容器 (端口 5000)
- ✅ 检测本机局域网 IP 地址
- ✅ 显示 Registry 访问信息

**输出示例：**
```
==========================================
  ✅ Registry 搭建成功！
==========================================

Registry 信息：
  - 容器名称: addp-registry
  - 端口: 5000
  - 本机访问: http://localhost:5000
  - 局域网访问: http://192.168.1.100:5000
```

**记录下你的局域网 IP**（例如 `192.168.1.100`），后续部署会用到。

---

### 1.2 构建并推送镜像到本地 Registry

```bash
# 推送到 Registry（使用你的局域网 IP）
./scripts/deploy/1-build-images.sh --registry 192.168.1.100:5000

# 或者推送到 localhost（然后在服务器上用局域网 IP 访问）
./scripts/deploy/1-build-images.sh --registry localhost:5000
```

**脚本会自动完成：**
- ✅ 检查 Registry 连接
- ✅ 构建所有 ADDP 服务镜像（7 个服务）
- ✅ 标记镜像为 Registry 格式
- ✅ 推送所有镜像到 Registry
- ✅ 验证推送结果

**推送过程示例：**
```
==========================================
  步骤 1/3: 构建镜像
==========================================
==> 开始构建所有 ADDP 镜像...
✅ 所有镜像构建完成

==========================================
  步骤 2/3: 标记并推送镜像
==========================================
[1/7] 处理: system-backend
  - 标记镜像: 192.168.1.100:5000/addp-system-backend:latest
  - 推送镜像...
  ✅ 推送成功

[2/7] 处理: system-frontend
  ...

✅ 推送完成！
统计信息：
  - 成功推送: 7 个镜像
  - 总耗时: 120s
```

---

### 1.3 验证镜像推送成功

```bash
# 查看 Registry 中的所有镜像
curl http://localhost:5000/v2/_catalog

# 预期输出（JSON 格式）
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
```

---

### 1.4 准备服务器部署文件

```bash
# 传输必要文件到服务器
# 替换 user@server-ip 为你的服务器信息

# 方法 A: 使用 SCP（适合首次部署）
scp docker-compose.prod.yml user@192.168.1.200:/opt/addp/
scp .env.example user@192.168.1.200:/opt/addp/
# 推荐使用一键部署脚本（无需单独拷贝部署脚本）
scp scripts/init-db.sql user@192.168.1.200:/opt/addp/scripts/

# 方法 B: 使用 rsync（支持断点续传，推荐）
rsync -avz --exclude 'node_modules' --exclude 'bin' --exclude '.git' \
  docker-compose.prod.yml \
  .env.example \
  scripts/ \
  user@192.168.1.200:/opt/addp/
```

---

## 阶段 2: 服务器部署（服务器操作）

### 2.1 SSH 登录服务器

```bash
ssh user@192.168.1.200
cd /opt/addp
```

---

### 2.2 配置服务器 Docker 信任本地 Registry

**方法一：自动配置（推荐）**

部署脚本会自动检测并配置 `/etc/docker/daemon.json`。

**方法二：手动配置**

```bash
# 编辑 Docker daemon 配置
sudo vim /etc/docker/daemon.json

# 添加以下内容（替换为你的开发机 IP）
{
  "insecure-registries": ["192.168.1.100:5000"]
}

# 重启 Docker
sudo systemctl restart docker

# 验证配置生效
docker info | grep "Insecure Registries"
# 应该显示: Insecure Registries: 192.168.1.100:5000
```

---

### 2.3 测试 Registry 连接

```bash
# 测试是否能访问开发机的 Registry
curl http://192.168.1.100:5000/v2/

# 预期输出
{}

# 查看可用镜像
curl http://192.168.1.100:5000/v2/_catalog
```

**如果连接失败，排查方法：**

1. **检查开发机 Registry 是否运行**
   ```bash
   # 在开发机执行
   docker ps | grep addp-registry
   ```

2. **检查网络连通性**
   ```bash
   # 在服务器执行
   ping 192.168.1.100
   telnet 192.168.1.100 5000
   ```

3. **检查开发机防火墙**
   ```bash
   # macOS: 系统偏好设置 -> 安全性与隐私 -> 防火墙
   # 或暂时关闭防火墙测试
   ```

4. **检查服务器防火墙**
   ```bash
   # Ubuntu/Debian
   sudo ufw status
   sudo ufw allow 5000

   # CentOS/RHEL
   sudo firewall-cmd --list-all
   sudo firewall-cmd --add-port=5000/tcp --permanent
   sudo firewall-cmd --reload
   ```

---

### 2.4 配置生产环境变量

```bash
# 复制配置模板
cp .env.example .env

# 编辑配置（重要！）
vim .env
```

**必须修改的配置项：**

```bash
# 安全密钥（必须生成强随机值）
JWT_SECRET=<使用 openssl rand -base64 32 生成>
ENCRYPTION_KEY=<使用 openssl rand -base64 32 生成>

# 数据库密码
POSTGRES_PASSWORD=<强密码>
POSTGRES_USER=addp
POSTGRES_DB=addp

# Redis 密码
REDIS_PASSWORD=<强密码>

# MinIO 密码
MINIO_SYSTEM_ROOT_USER=admin
MINIO_SYSTEM_ROOT_PASSWORD=<强密码>

# 业务基础设施
BUSINESS_MINIO_ENDPOINT=localhost:9002
BUSINESS_MINIO_ACCESS_KEY=admin
BUSINESS_MINIO_SECRET_KEY=<强密码>

# 服务集成
ENABLE_SERVICE_INTEGRATION=true
```

**生成随机密钥示例：**
```bash
# 生成 JWT_SECRET
openssl rand -base64 32
# 输出: kX7nJ9mP2qR5sT8vW1yZ3aB4cD6eF0gH1iJ2kL3mN4o=

# 生成 ENCRYPTION_KEY
openssl rand -base64 32
# 输出: pQ9rS2tU5vX8yZ1aB3cD4eF6gH7iJ9kL0mN2oP3qR5s=
```

---

### 2.5 运行部署脚本

```bash
# 设置 Registry 地址并执行部署
./scripts/deploy/deploy-all.sh --server user@192.168.1.200 --registry 192.168.1.100:5000 --skip-build
```

**脚本会自动完成：**
- ✅ 检查 Docker 和 Docker Compose 安装
- ✅ 配置 Docker 信任 Registry
- ✅ 测试 Registry 连接
- ✅ 准备部署目录和配置文件
- ✅ 拉取所有镜像
- ✅ 启动所有服务
- ✅ 健康检查

**部署过程示例：**
```
==========================================
  步骤 1/6: 检查环境
==========================================
✅ Docker 版本: Docker version 24.0.7
✅ Docker Compose 版本: docker-compose version 1.29.2

==========================================
  步骤 2/6: 配置 Docker Registry
==========================================
✅ Registry 192.168.1.100:5000 已在信任列表中

==========================================
  步骤 3/6: 测试 Registry 连接
==========================================
✅ Registry 连接成功
可用镜像:
  - addp-gateway
  - addp-manager-backend
  - addp-meta-backend
  ...

==========================================
  步骤 5/6: 拉取镜像
==========================================
==> 开始拉取所有镜像...
Pulling postgres       ... done
Pulling redis          ... done
Pulling system-backend ... done
...
✅ 所有镜像拉取成功

==========================================
  步骤 6/6: 启动服务
==========================================
✅ 服务启动成功

==========================================
  ✅ 部署完成！
==========================================
服务访问地址：
  - Portal (统一门户): http://192.168.1.200:5170
  - Gateway (API): http://192.168.1.200:8000
```

---

### 2.6 验证部署成功

```bash
# 查看所有服务状态
docker-compose -f docker-compose.prod.yml ps

# 预期输出（所有服务 State 为 Up）
NAME                    STATE     PORTS
addp-gateway            Up        0.0.0.0:8000->8000/tcp
addp-manager-backend    Up        0.0.0.0:8081->8081/tcp
addp-meta-backend       Up        0.0.0.0:8082->8082/tcp
addp-portal             Up        0.0.0.0:5170->80/tcp
addp-postgres           Up        0.0.0.0:5432->5432/tcp
addp-system-backend     Up        0.0.0.0:8080->8080/tcp
...

# 健康检查
curl http://localhost:8080/health  # System backend
curl http://localhost:8000/health  # Gateway

# 查看日志
docker-compose -f docker-compose.prod.yml logs -f
```

---

## 阶段 3: 访问和验证

### 3.1 浏览器访问

```
统一门户: http://192.168.1.200:5170
API 网关: http://192.168.1.200:8000
```

### 3.2 首次登录

1. 注册管理员账号（首次启动时）
2. 或使用默认账号（如果在 System module 中配置了）

### 3.3 验证功能

- ✅ 用户登录和认证
- ✅ System 模块（用户管理、日志、资源）
- ✅ Manager 模块（数据源、目录、预览）
- ✅ Meta 模块（元数据扫描、查询）

---

## 常用管理命令

### 服务管理

```bash
# 查看服务状态
docker-compose -f docker-compose.prod.yml ps

# 查看实时日志
docker-compose -f docker-compose.prod.yml logs -f

# 查看特定服务日志
docker-compose -f docker-compose.prod.yml logs -f system-backend

# 重启所有服务
docker-compose -f docker-compose.prod.yml --profile full restart

# 重启单个服务
docker-compose -f docker-compose.prod.yml restart system-backend

# 停止所有服务
docker-compose -f docker-compose.prod.yml --profile full down

# 停止并删除数据卷（谨慎！）
docker-compose -f docker-compose.prod.yml --profile full down -v
```

---

### 镜像管理

```bash
# 查看已拉取的镜像
docker images | grep addp

# 更新镜像（当开发机推送新版本后）
REGISTRY=192.168.1.100:5000 docker-compose -f docker-compose.prod.yml pull
docker-compose -f docker-compose.prod.yml --profile full up -d

# 清理未使用的镜像
docker image prune -a
```

---

### 数据备份

```bash
# 备份 PostgreSQL 数据库
docker exec addp-postgres pg_dumpall -U addp > backup-$(date +%Y%m%d).sql

# 备份数据卷
docker run --rm -v addp_postgres_data:/data -v $(pwd):/backup \
  alpine tar czf /backup/postgres-backup-$(date +%Y%m%d).tar.gz /data

# 恢复数据库
docker exec -i addp-postgres psql -U addp < backup-20240101.sql
```

---

## 故障排查

### 问题 1: 无法连接到 Registry

**症状：**
```
Error response from daemon: Get "http://192.168.1.100:5000/v2/": dial tcp: connection refused
```

**解决方法：**
1. 确认开发机 Registry 运行中：`docker ps | grep addp-registry`
2. 确认服务器和开发机在同一局域网
3. 测试网络连接：`ping 192.168.1.100`
4. 检查防火墙设置

---

### 问题 2: 镜像拉取失败

**症状：**
```
manifest for 192.168.1.100:5000/addp-system-backend:latest not found
```

**解决方法：**
1. 在开发机检查镜像是否已推送：
   ```bash
   curl http://localhost:5000/v2/_catalog
   curl http://localhost:5000/v2/addp-system-backend/tags/list
   ```
2. 重新推送镜像：
   ```bash
   ./scripts/deploy/1-build-images.sh --registry 192.168.1.100:5000
   ```

---

### 问题 3: 服务启动失败

**症状：**
```
addp-system-backend | Error: database connection failed
```

**解决方法：**
1. 查看详细日志：
   ```bash
   docker-compose -f docker-compose.prod.yml logs system-backend
   ```
2. 检查 `.env` 配置是否正确
3. 确认 PostgreSQL 服务健康：
   ```bash
   docker-compose -f docker-compose.prod.yml ps postgres
   ```
4. 重启服务：
   ```bash
   docker-compose -f docker-compose.prod.yml restart system-backend
   ```

---

### 问题 4: 健康检查失败

**症状：**
```
❌ 检查 system-backend... 不健康
```

**解决方法：**
1. 等待更长时间（服务可能需要更长启动时间）
2. 手动测试健康接口：
   ```bash
   curl -v http://localhost:8080/health
   ```
3. 查看服务日志排查错误
4. 检查依赖服务（PostgreSQL, Redis）是否就绪

---

## 更新和升级

### 更新流程（开发机 → 服务器）

```bash
# ========== 开发机 ==========
# 1. 修改代码后重新构建并推送
cd /Users/pampa/code/addp
./scripts/deploy/1-build-images.sh --registry 192.168.1.100:5000

# ========== 服务器 ==========
# 2. SSH 到服务器
ssh user@192.168.1.200
cd /opt/addp

# 3. 拉取新镜像
REGISTRY=192.168.1.100:5000 docker-compose -f docker-compose.prod.yml pull

# 4. 滚动更新（不停机）
docker-compose -f docker-compose.prod.yml --profile full up -d

# 5. 查看更新后的服务
docker-compose -f docker-compose.prod.yml ps
```

---

## 注意事项

### 开发机要求
- ⚠️ **部署期间必须保持开机**：服务器拉取镜像时需要访问开发机 Registry
- ⚠️ **网络稳定性**：建议使用有线连接，避免 WiFi 不稳定导致拉取中断
- ⚠️ **磁盘空间**：Registry 会占用 ~2-5GB 存储空间

### 安全建议
- 🔒 生产环境必须修改所有默认密码
- 🔒 使用强随机密钥（JWT_SECRET, ENCRYPTION_KEY）
- 🔒 定期备份数据库和数据卷
- 🔒 开发机 Registry 仅用于局域网部署，不要暴露到公网

### 性能优化
- 📊 首次拉取镜像较慢（~5-10分钟），后续更新仅拉取变化层
- 📊 服务器配置建议：4 核 CPU + 8GB RAM + 50GB 存储
- 📊 可配置 Docker 资源限制防止单个服务占用过多资源

---

## 脚本文件清单

| 文件名 | 位置 | 用途 |
|--------|------|------|
| `setup-local-registry.sh` | `scripts/` | 在开发机搭建 Registry |
| `scripts/deploy/1-build-images.sh` | `scripts/deploy/` | 本机构建并推送镜像 |
| `scripts/deploy/deploy-all.sh` | `scripts/deploy/` | 一键部署（支持指定 registry 与 server） |
| `docker-compose.prod.yml` | 项目根目录 | 生产环境 Compose 配置 |
| `.env.example` | 项目根目录 | 环境变量模板 |

---

## 支持

如果遇到问题，请：
1. 查看本文档的故障排查部分
2. 检查服务日志：`docker-compose -f docker-compose.prod.yml logs`
3. 提交 Issue 到项目仓库

---

**祝部署顺利！🎉**
