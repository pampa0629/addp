# ADDP 服务器端部署指南（简化版）

## 概述

由于多架构镜像构建存在网络连接问题，推荐使用**服务器本地构建**的方式部署。

服务器会直接构建本地架构（Intel x86_64）的镜像，无需依赖镜像仓库。

---

## 部署步骤

### 步骤 1: 准备服务器环境

确保服务器已安装：
- Docker 20.10+
- Docker Compose v2

```bash
# 检查 Docker
docker --version
docker-compose --version
```

### 步骤 2: 传输代码到服务器

#### 方法 A: 使用 SCP

```bash
# 在开发机执行
cd /Users/pampa/code
tar czf addp.tar.gz addp/
scp addp.tar.gz pampa@192.168.1.182:/tmp/

# 在服务器执行
ssh pampa@192.168.1.182
cd /opt
sudo tar xzf /tmp/addp.tar.gz
sudo chown -R $USER:$USER addp  # Linux
# 或
sudo chown -R $USER addp  # macOS
```

#### 方法 B: 使用 rsync

```bash
# 在开发机执行
rsync -avz --exclude 'node_modules' --exclude 'bin' --exclude 'dist' --exclude '.git' \
  /Users/pampa/code/addp/ \
  pampa@192.168.1.182:/opt/addp/
```

#### 方法 C: 使用 Git

```bash
# 在服务器执行
cd /opt
git clone https://github.com/your-org/addp.git
cd addp
git checkout main
```

### 步骤 3: 部署 Business 基础设施

```bash
# 在服务器执行
cd /opt/addp/business

# 配置环境变量
cp .env.prod.example .env
vim .env

# 修改以下配置:
# POSTGRES_PASSWORD=<强密码>
# MINIO_ROOT_PASSWORD=<强密码>

# 启动 Business 基础设施
docker-compose -f docker-compose.yml up -d

# 验证
docker-compose -f docker-compose.yml ps
```

预期输出:
```
NAME                          STATE     PORTS
business-postgres             Up        0.0.0.0:5433->5432/tcp
business-minio                Up        0.0.0.0:9002-9003->9000-9001/tcp
```

### 步骤 4: 构建并部署 ADDP 系统

```bash
## 4.1 准备生产环境变量
# 在服务器执行
cd /opt/addp
cp .env.prod.example .env.prod
vim .env.prod

# 关键项（至少修改以下几项）：
# JWT_SECRET=<强随机密钥>
# ENCRYPTION_KEY=<强随机密钥>
# INTERNAL_API_KEY=<强随机密钥>
# POSTGRES_PASSWORD=<系统数据库密码>
# REDIS_PASSWORD=<Redis密码>
# MINIO_ROOT_PASSWORD=<MinIO密码>

# 可选：一键生成并写入（也可使用 scripts/prod/start.sh 自动生成）
openssl rand -base64 32  # 手动生成备用密钥

## 4.2 启动本地镜像仓库（如未运行）
docker ps | grep registry || \
  docker run -d -p 5001:5000 --restart=always --name registry registry:2

## 4.3 预编译二进制（用于 Docker 预构建镜像路径）
# 根据 CPU 架构选择，常见为 amd64；多架构可使用 both
./scripts/build/compile.sh --arch amd64

## 4.4 本地构建并推送镜像到注册表
# 构建并推送所有服务镜像到 localhost:5001
./scripts/build/build-images.sh --registry localhost:5001

## 4.5 使用 Compose 启动全部服务
docker compose -f docker-compose.yml --env-file .env.prod up -d --remove-orphans
```

脚本会：
1. 检查所有 Dockerfile
2. 本地构建所有镜像（自动为 AMD64 架构）
3. 使用 docker-compose 启动服务

### 步骤 5: 验证部署

```bash
# 查看服务状态
docker compose -f docker-compose.yml --env-file .env.prod ps

# 查看日志
docker compose -f docker-compose.yml --env-file .env.prod logs -f

# 测试健康检查
curl http://localhost:8080/health  # System backend
curl http://localhost:8000/health  # Gateway
curl http://localhost:5170         # Portal
```

---

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| Portal | 5170 | 统一入口 |
| Gateway | 8000 | API 网关 |
| System Backend | 8080 | 系统后端 |
| System Frontend | 8090 | 系统前端（独立访问） |
| Manager Backend | 8081 | 管理后端 |
| Manager Frontend | 8091 | 管理前端（独立访问） |
| Meta Backend | 8082 | 元数据后端 |
| PostgreSQL (System) | 5432 | ADDP 系统数据库 |
| PostgreSQL (Business) | 5433 | 业务数据库 |
| Redis | 6379 | 缓存和队列 |
| MinIO System | 9000-9001 | 系统文件存储 |
| MinIO Business | 9002-9003 | 业务文件存储 |

---

## 常见问题

### Q1: 构建失败 - "go: requires go >= 1.24.0"

**解决**: 更新 Dockerfile 使用 golang:1.24-alpine

```bash
# 在所有 backend Dockerfile 中修改:
FROM golang:1.24-alpine AS builder
```

### Q2: 构建失败 - "context: not found"

**原因**: backend 服务需要从项目根目录构建（因为依赖 common/ 模块）

**解决**: 使用 `scripts/build/compile.sh` 统一处理，backend 使用 CONTEXT="."

### Q3: 服务启动失败 - "connection refused"

**检查**:
```bash
# 确认 Business 基础设施已启动
docker-compose -f business/docker-compose.yml ps

# 确认 PostgreSQL 可访问
docker exec -it business-postgres psql -U business_user -d business_db -c "SELECT 1"
```

### Q4: Portal 访问显示空白

**检查 nginx 配置**:
```bash
# 查看 Portal 日志
docker logs addp-portal

# 确认 Portal Dockerfile 和 nginx.conf 存在
ls -la portal/frontend/Dockerfile
ls -la portal/frontend/nginx.conf
```

---

## 更新部署

当代码更新后：

```bash
# 在服务器执行
cd /opt/addp

# 拉取最新代码（如果使用 Git）
git pull origin main

# 或重新传输文件（如果使用 scp/rsync）

# 重新构建镜像并部署
./scripts/build/compile.sh --arch amd64
./scripts/build/build-images.sh --registry localhost:5001
docker compose -f docker-compose.yml --env-file .env.prod up -d
```

---

## 回滚

如果新版本有问题：

```bash
# 停止服务
docker compose -f docker-compose.yml --env-file .env.prod down

# 恢复旧版本代码
git checkout <previous-commit>

# 重新构建
./scripts/build/compile.sh --arch amd64
./scripts/build/build-images.sh --registry localhost:5001
```

---

## 清理

完全清理并重新开始：

```bash
# 停止并删除所有容器
docker compose -f docker-compose.yml --env-file .env.prod down -v

# 删除所有 ADDP 镜像
docker images | grep "^addp-" | awk '{print $3}' | xargs docker rmi -f

# 重新构建
./scripts/build/compile.sh --arch amd64
./scripts/build/build-images.sh --registry localhost:5001
```

---

## 生产环境建议

1. **备份数据库**:
   ```bash
   # 备份 ADDP 系统数据库
   docker exec addp-postgres pg_dump -U addp addp > backup-system-$(date +%Y%m%d).sql

   # 备份业务数据库
   docker exec business-postgres pg_dump -U business_user business_db > backup-business-$(date +%Y%m%d).sql

---

## 可选：非容器化运行（本地二进制）

在无需容器的环境下，可以直接构建并运行二进制（默认输出到统一目录 `dist/`）。

```bash
# 在服务器执行（或开发机）
cd /opt/addp

# 构建 release 产物（统一输出到 dist/release）
make build-release

# 按平台运行（示例为 linux-amd64）
./dist/release/backend/system/linux-amd64/system      &
./dist/release/backend/manager/linux-amd64/manager    &
./dist/release/backend/meta/linux-amd64/meta          &
./dist/release/backend/transfer/linux-amd64/transfer  &
./dist/release/backend/gateway/linux-amd64/gateway    &

# 前端静态资源位于
# dist/release/frontend/system/
# dist/release/frontend/portal/
```

注意：非容器化运行需要自行准备 PostgreSQL、Redis、MinIO 等依赖，并在 `.env` 中配置对应连接信息。
   ```

2. **监控服务**:
   ```bash
   # 使用 cron 定期检查服务状态
   */5 * * * * cd /opt/addp && docker-compose -f docker-compose.yml ps | grep -q "Up" || /opt/addp/scripts/restart-services.sh
   ```

3. **日志轮转**:
   ```bash
   # 配置 Docker 日志大小限制
   vim /etc/docker/daemon.json
   {
     "log-driver": "json-file",
     "log-opts": {
       "max-size": "10m",
       "max-file": "3"
     }
   }
   ```

4. **使用域名**:
   - 配置 Nginx 反向代理
   - 申请 SSL 证书（Let's Encrypt）
   - 修改 docker-compose.yml 端口映射

---

## 下一步

部署完成后：

1. 访问 Portal: http://服务器IP:5170
2. 使用默认账号登录（如果有）
3. 修改默认密码
4. 配置数据源和业务数据

---

## 相关文档

- [DEPLOY_WITH_LOCAL_REGISTRY.md](DEPLOY_WITH_LOCAL_REGISTRY.md) - 使用镜像仓库部署（网络环境良好时）
- [TROUBLESHOOT_REGISTRY_500.md](TROUBLESHOOT_REGISTRY_500.md) - 镜像仓库故障排查
- [TROUBLESHOOT_SSH.md](TROUBLESHOOT_SSH.md) - SSH 连接问题
- [FIX_MACOS_SERVER_CHOWN.md](FIX_MACOS_SERVER_CHOWN.md) - macOS 服务器兼容性

---

## 技术支持

如遇问题，请提供以下信息：

```bash
# 系统信息
uname -a
docker --version
docker-compose --version

# 服务状态
docker-compose -f docker-compose.yml ps

# 最近日志
docker-compose -f docker-compose.yml logs --tail=100

# 镜像列表
docker images | grep addp
```
