# Business 基础设施独立部署指南

Business 基础设施是 ADDP 系统的**业务数据层**，与 ADDP 系统基础设施**完全独立**。它包含：

- **PostGIS** (端口 5433) - 存储用户实际业务数据，支持空间数据
- **MinIO** (端口 9002-9003) - 存储用户上传的文件

**独立部署特性**：
- ✅ 使用官方 Docker Hub 镜像，无需自建镜像仓库
- ✅ 无需与 ADDP 系统同步部署，可先于或后于 ADDP 系统启动
- ✅ 通过网络连接与 ADDP 通信，无容器依赖关系

## 部署架构

```
┌─────────────────────────────────────────────────────────┐
│  服务器                                                  │
│                                                         │
│  ┌──────────────────────────────────────────────────┐   │
│  │  Business 基础设施 (独立部署)                       │   │
│  │  - PostgreSQL (5433)                             │   │
│  │  - MinIO (9002-9003)                             │   │
│  └──────────────────────────────────────────────────┘   │
│                                                         │
│  ┌──────────────────────────────────────────────────┐   │
│  │  ADDP 系统 (独立部署)                              │   │
│  │  - System Backend  ──→ 连接 Business 基础设施      │   │
│  │  - Manager Backend ──→ 连接 Business 基础设施      │   │
│  │  - Meta Backend ──→ 连接 Business 基础设施         │   │
│  │  - Gateway                                       │   │
│  │  - Portal                                        │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

**为什么分离部署？**
- ✅ 数据隔离：系统元数据与业务数据物理分离
- ✅ 独立扩展：业务数据量增长时可单独扩容
- ✅ 安全性：业务数据库可配置更严格的访问控制
- ✅ 可替换性：可替换为云服务（RDS、OSS）
- ✅ 简化部署：官方镜像无需构建，直接拉取即用

---

## 部署方法

### 方法一：使用部署包（推荐）

#### 1. 在开发机准备部署包

```bash
# 在项目根目录执行
cd /Users/pampa/code/addp/business

# 运行准备脚本（可选本地测试）
./scripts/prepare-for-deploy.sh

# 生成的部署包位于项目根目录
# 例如: ../business-deploy-20240101_120000.tar.gz
```

#### 2. 传输部署包到服务器

```bash
# 从开发机传输
scp ../business-deploy-*.tar.gz user@server-ip:/opt/

# 或使用 rsync
rsync -avz --progress ../business-deploy-*.tar.gz user@server-ip:/opt/
```

#### 3. 在服务器上部署

```bash
# SSH 登录服务器
ssh user@server-ip

# 解压部署包
cd /opt
tar -xzf business-deploy-*.tar.gz
cd business-deploy-*/

# 配置环境变量（重要！）
cp .env.example .env
vim .env

# 修改以下密码为强密码：
# - BUSINESS_POSTGRES_PASSWORD
# - BUSINESS_MINIO_ROOT_PASSWORD

# 生成强密码
openssl rand -base64 32

# 启动服务
docker-compose -f docker-compose.prod.yml up -d

# 查看状态
docker-compose -f docker-compose.prod.yml ps
docker-compose -f docker-compose.prod.yml logs -f
```

---

### 方法二：直接传输文件

```bash
# 从开发机传输必要文件
cd /Users/pampa/code/addp

scp business/docker-compose.prod.yml user@server-ip:/opt/addp-business/
scp business/.env.prod.example user@server-ip:/opt/addp-business/.env.example
scp business/scripts/deploy-business.sh user@server-ip:/opt/addp-business/scripts/

# 在服务器上部署
ssh user@server-ip
cd /opt/addp-business
chmod +x scripts/deploy-business.sh
./scripts/deploy-business.sh
```

---

## 配置说明

### 环境变量配置 (.env)

```bash
# PostgreSQL 配置
BUSINESS_POSTGRES_USER=business
BUSINESS_POSTGRES_PASSWORD=<强密码>  # ⚠️  必须修改
BUSINESS_POSTGRES_DB=business
BUSINESS_POSTGRES_PORT=5433  # 避免与 ADDP 系统冲突（5432）

# MinIO 配置
BUSINESS_MINIO_ROOT_USER=admin
BUSINESS_MINIO_ROOT_PASSWORD=<强密码>  # ⚠️  必须修改
BUSINESS_MINIO_API_PORT=9002      # 避免与 ADDP 系统冲突（9000）
BUSINESS_MINIO_CONSOLE_PORT=9003  # 避免与 ADDP 系统冲突（9001）
```

**密码要求**：
- 至少 16 位字符
- 包含大小写字母、数字、特殊字符
- 不要使用默认密码 `business_password` 或 `minioadmin`

**生成强密码**：
```bash
openssl rand -base64 32
```

---

## 验证部署

### 1. 检查服务状态

```bash
docker-compose -f docker-compose.prod.yml ps

# 预期输出（所有服务 State 为 Up）
NAME                STATE     PORTS
business-postgres   Up        0.0.0.0:5433->5432/tcp
business-minio      Up        0.0.0.0:9002->9000/tcp, 0.0.0.0:9003->9001/tcp
```

### 2. 测试 PostgreSQL 连接

```bash
# 测试 PostgreSQL (PostGIS) 连接
docker exec -it business-postgres psql -U business -d business

# 验证 PostGIS 扩展
business=# SELECT PostGIS_version();
business=# \dx  # 查看已安装的扩展

# 从远程连接（需要配置防火墙）
psql -h server-ip -p 5433 -U business -d business
```

### 3. 访问 MinIO Console

浏览器访问：`http://server-ip:9003`

- 用户名：见 `.env` 文件 `BUSINESS_MINIO_ROOT_USER`
- 密码：见 `.env` 文件 `BUSINESS_MINIO_ROOT_PASSWORD`

---

## 常用管理命令

### 服务管理

```bash
# 查看服务状态
docker-compose -f docker-compose.prod.yml ps

# 查看实时日志
docker-compose -f docker-compose.prod.yml logs -f

# 查看特定服务日志
docker-compose -f docker-compose.prod.yml logs -f postgres
docker-compose -f docker-compose.prod.yml logs -f minio

# 重启服务
docker-compose -f docker-compose.prod.yml restart

# 停止服务
docker-compose -f docker-compose.prod.yml down

# 停止并删除数据卷（⚠️  谨慎！会丢失所有数据）
docker-compose -f docker-compose.prod.yml down -v
```

---

### 数据备份

#### PostgreSQL 备份

```bash
# 备份所有数据库
docker exec business-postgres pg_dumpall -U business > business-backup-$(date +%Y%m%d).sql

# 备份单个数据库
docker exec business-postgres pg_dump -U business -d business > business-db-backup-$(date +%Y%m%d).sql

# 恢复数据库
docker exec -i business-postgres psql -U business < business-backup-20240101.sql
```

#### MinIO 数据备份

```bash
# 备份整个 MinIO 数据卷
docker run --rm \
  -v business_minio_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/minio-backup-$(date +%Y%m%d).tar.gz /data

# 恢复 MinIO 数据
docker run --rm \
  -v business_minio_data:/data \
  -v $(pwd):/backup \
  alpine tar xzf /backup/minio-backup-20240101.tar.gz -C /
```

---

### 数据迁移

#### 导出现有数据

```bash
# 导出 PostgreSQL 数据
docker exec business-postgres pg_dump -U business -d business -F c -f /tmp/business.dump
docker cp business-postgres:/tmp/business.dump ./business.dump

# 导出 MinIO 数据（使用 mc 客户端）
docker run --rm --network business-network \
  minio/mc alias set minio http://business-minio:9000 admin password
docker run --rm --network business-network \
  minio/mc mirror minio/bucket ./backup/
```

#### 导入到新服务器

```bash
# 导入 PostgreSQL 数据
docker cp business.dump business-postgres:/tmp/
docker exec business-postgres pg_restore -U business -d business /tmp/business.dump

# 导入 MinIO 数据
docker run --rm --network business-network \
  -v $(pwd)/backup:/data \
  minio/mc mirror /data minio/bucket
```

---

## 安全建议

### 1. 密码管理

- ✅ 使用强随机密码（至少 16 位）
- ✅ 定期更新密码（每 90 天）
- ✅ 不要在代码中硬编码密码
- ✅ 使用环境变量或密钥管理服务

### 2. 网络访问控制

```bash
# 仅允许 ADDP 系统访问（推荐）
# 不要将端口暴露到公网

# 如需外部访问，配置防火墙规则
sudo ufw allow from addp-server-ip to any port 5433 proto tcp
sudo ufw allow from addp-server-ip to any port 9002 proto tcp
```

### 3. 数据加密

```bash
# PostgreSQL - 启用 SSL（可选）
# 在 docker-compose.prod.yml 中添加：
environment:
  POSTGRES_SSL: "on"
volumes:
  - ./certs/server.crt:/var/lib/postgresql/server.crt
  - ./certs/server.key:/var/lib/postgresql/server.key
```

### 4. 定期备份

```bash
# 创建定时备份任务
crontab -e

# 每天凌晨 2 点备份 PostgreSQL
0 2 * * * docker exec business-postgres pg_dumpall -U business > /backup/business-$(date +\%Y\%m\%d).sql

# 每周日凌晨 3 点备份 MinIO
0 3 * * 0 docker run --rm -v business_minio_data:/data -v /backup:/backup alpine tar czf /backup/minio-$(date +\%Y\%m\%d).tar.gz /data
```

---

## 性能优化

### PostgreSQL 优化

```bash
# 在 docker-compose.prod.yml 中添加配置
environment:
  # 内存配置（根据服务器配置调整）
  POSTGRES_SHARED_BUFFERS: "2GB"
  POSTGRES_EFFECTIVE_CACHE_SIZE: "6GB"
  POSTGRES_WORK_MEM: "64MB"
  POSTGRES_MAINTENANCE_WORK_MEM: "512MB"

# 或者挂载自定义配置文件
volumes:
  - ./postgresql.conf:/etc/postgresql/postgresql.conf
```

### MinIO 优化

```bash
# 在 docker-compose.prod.yml 中添加环境变量
environment:
  # 启用性能监控
  MINIO_PROMETHEUS_AUTH_TYPE: "public"

  # 调整缓存大小
  MINIO_CACHE_SIZE: "8GB"
```

---

## 故障排查

### 问题 1: PostgreSQL 无法启动

**症状**：
```
business-postgres | FATAL: data directory "/var/lib/postgresql/data" has wrong ownership
```

**解决方法**：
```bash
# 修复数据目录权限
docker-compose -f docker-compose.prod.yml down
docker volume rm business_postgres_data
docker-compose -f docker-compose.prod.yml up -d
```

---

### 问题 2: MinIO 健康检查失败

**症状**：
```
business-minio | unhealthy
```

**解决方法**：
```bash
# 查看 MinIO 日志
docker-compose -f docker-compose.prod.yml logs minio

# 手动测试健康接口
curl http://localhost:9002/minio/health/live

# 重启 MinIO
docker-compose -f docker-compose.prod.yml restart minio
```

---

### 问题 3: 端口冲突

**症状**：
```
Error starting userland proxy: listen tcp 0.0.0.0:5433: bind: address already in use
```

**解决方法**：
```bash
# 检查端口占用
lsof -i :5433
netstat -tuln | grep 5433

# 修改 .env 中的端口配置
BUSINESS_POSTGRES_PORT=5434
BUSINESS_MINIO_API_PORT=9004
BUSINESS_MINIO_CONSOLE_PORT=9005

# 重启服务
docker-compose -f docker-compose.prod.yml up -d
```

---

### 问题 4: 连接被拒绝

**症状**：
```
psql: error: connection to server at "server-ip", port 5433 failed: Connection refused
```

**解决方法**：
```bash
# 1. 检查服务是否运行
docker-compose -f docker-compose.prod.yml ps

# 2. 检查防火墙
sudo ufw status
sudo ufw allow 5433/tcp

# 3. 检查 PostgreSQL 配置
docker exec business-postgres cat /var/lib/postgresql/data/pg_hba.conf
```

---

## 监控和日志

### 实时监控

```bash
# 查看容器资源使用
docker stats business-postgres business-minio

# 查看数据卷大小
docker system df -v | grep business
```

### 日志管理

```bash
# 配置日志大小限制（在 docker-compose.prod.yml 中）
logging:
  driver: "json-file"
  options:
    max-size: "100m"
    max-file: "3"

# 查看日志
docker-compose -f docker-compose.prod.yml logs --tail=100 -f
```

---

## 与 ADDP 系统集成

Business 基础设施部署完成后，ADDP 系统通过以下方式连接：

### 1. 在 ADDP System 中配置资源

登录 ADDP Portal → System 模块 → Resources 管理

添加 Business PostgreSQL 资源：
```json
{
  "name": "Business PostgreSQL",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "server-ip",
    "port": 5433,
    "database": "business",
    "schema": "public",
    "username": "business",
    "password": "<加密存储>",
    "sslmode": "prefer"
  },
  "description": "业务数据库（PostGIS 支持）"
}
```

添加 Business MinIO 资源：
```json
{
  "name": "Business MinIO",
  "resource_type": "minio",
  "connection_info": {
    "endpoint": "server-ip:9002",
    "access_key": "admin",
    "secret_key": "<加密存储>",
    "use_ssl": false
  }
}
```

### 2. Manager 和 Meta 模块自动发现

Manager 和 Meta 模块会自动从 System 模块同步资源配置，无需额外配置。

---

## 升级和维护

### 升级 PostgreSQL

```bash
# 1. 备份数据
docker exec business-postgres pg_dumpall -U business > backup-before-upgrade.sql

# 2. 停止服务
docker-compose -f docker-compose.prod.yml down

# 3. 修改 docker-compose.prod.yml 中的镜像版本
# image: postgres:16-alpine  # 从 15 升级到 16

# 4. 启动新版本
docker-compose -f docker-compose.prod.yml up -d

# 5. 验证
docker-compose -f docker-compose.prod.yml logs postgres
```

### 升级 MinIO

```bash
# MinIO 支持热升级
docker-compose -f docker-compose.prod.yml pull minio
docker-compose -f docker-compose.prod.yml up -d minio
```

---

## 卸载

```bash
# ⚠️  警告：以下操作会删除所有数据！

# 停止服务并删除容器
docker-compose -f docker-compose.prod.yml down

# 删除数据卷（不可恢复）
docker volume rm business_postgres_data
docker volume rm business_minio_data

# 删除网络
docker network rm business-network

# 删除部署目录
sudo rm -rf /opt/addp-business
```

---

## 附录

### A. 端口配置对照表

| 服务 | 开发环境端口 | 生产环境端口 | 说明 |
|------|------------|------------|------|
| PostgreSQL | 5433 | 5433 | 避免与 ADDP 系统冲突 |
| MinIO API | 9000 | 9002 | 避免与 ADDP 系统冲突 |
| MinIO Console | 9001 | 9003 | 避免与 ADDP 系统冲突 |

### B. 数据卷位置

```bash
# 查看数据卷实际存储位置
docker volume inspect business_postgres_data
docker volume inspect business_minio_data

# 通常位于
# /var/lib/docker/volumes/business_postgres_data/_data
# /var/lib/docker/volumes/business_minio_data/_data
```

### C. 相关文档

- [ADDP 系统部署指南](DEPLOY_WITH_LOCAL_REGISTRY.md)
- [PostgreSQL 官方文档](https://www.postgresql.org/docs/)
- [MinIO 官方文档](https://min.io/docs/)

---

**部署完成后，继续部署 ADDP 系统：**
请参考 [ADDP 局域网私有镜像仓库部署指南](DEPLOY_WITH_LOCAL_REGISTRY.md)
