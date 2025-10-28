# Infrastructure Credentials Reference

基础设施服务访问凭据参考文档

**⚠️ 安全提醒**:
- 本文档列出的是**默认开发环境**凭据
- **生产环境必须修改所有默认密码**
- 切勿将生产环境凭据提交到代码仓库

---

## ADDP 系统基础设施

### PostgreSQL (ADDP 系统数据库)

**连接信息**:
```
Host:     localhost
Port:     5432
Database: addp
User:     addp
Password: addp_password
```

**连接字符串**:
```bash
# 从主机连接
postgresql://addp:addp_password@localhost:5432/addp

# 从 Docker 容器内连接
postgresql://addp:addp_password@postgres:5432/addp
```

**命令行访问**:
```bash
# 使用 psql 连接
psql -h localhost -p 5432 -U addp -d addp

# 使用 Docker
docker exec -it addp-postgres psql -U addp -d addp
```

**配置文件**: `.env`
```bash
POSTGRES_USER=addp
POSTGRES_PASSWORD=addp_password
POSTGRES_DB=addp
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
```

---

### Redis (缓存和任务队列)

**连接信息**:
```
Host:     localhost
Port:     6379
Password: addp_redis
Database: 0 (默认)
```

**连接字符串**:
```bash
# 标准格式
redis://:addp_redis@localhost:6379/0

# 从 Docker 容器内连接
redis://:addp_redis@redis:6379/0
```

**命令行访问**:
```bash
# 使用 redis-cli 连接
redis-cli -h localhost -p 6379 -a addp_redis

# 使用 Docker
docker exec -it addp-redis redis-cli -a addp_redis
```

**配置文件**: `.env`
```bash
REDIS_PASSWORD=addp_redis
REDIS_HOST=redis
REDIS_PORT=6379
```

---

### MinIO (ADDP 系统文件存储)

**连接信息**:
```
API Endpoint:      http://localhost:9002
Console URL:       http://localhost:9003
Access Key:        minioadmin
Secret Key:        minioadmin
Default Bucket:    addp-data
```

**Web 控制台访问**:
```
URL:      http://localhost:9003
Username: minioadmin
Password: minioadmin
```

**S3 兼容配置**:
```bash
# AWS CLI 格式
export AWS_ACCESS_KEY_ID=minioadmin
export AWS_SECRET_ACCESS_KEY=minioadmin
export AWS_ENDPOINT_URL=http://localhost:9002

# MinIO Client (mc)
mc alias set addp-minio http://localhost:9002 minioadmin minioadmin
```

**配置文件**: `.env`
```bash
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
MINIO_BUCKET=addp-data
```

---

### Elasticsearch (全文检索)

**连接信息**:
```
HTTP Endpoint: http://localhost:9200
Transport:     localhost:9300
Security:      Disabled (开发环境)
Username:      N/A (未启用认证)
Password:      N/A (未启用认证)
```

**API 访问**:
```bash
# 健康检查
curl http://localhost:9200/_cluster/health

# 查看所有索引
curl http://localhost:9200/_cat/indices?v

# 查看节点信息
curl http://localhost:9200/_nodes
```

**配置文件**: `.env`
```bash
ELASTICSEARCH_URL=http://elasticsearch:9200
ELASTICSEARCH_INDEX=metadata
MANAGER_ELASTICSEARCH_INDEX=manager-resources
```

---

## Business 业务基础设施

### PostgreSQL (业务数据库)

**连接信息**:
```
Host:     localhost
Port:     5433  (⚠️ 注意：不是 5432)
Database: business
User:     business
Password: business_password
```

**连接字符串**:
```bash
# 从主机连接
postgresql://business:business_password@localhost:5433/business

# 从 Docker 容器内连接 (ADDP 服务访问)
postgresql://business:business_password@host.docker.internal:5433/business
```

**命令行访问**:
```bash
# 使用 psql 连接
psql -h localhost -p 5433 -U business -d business

# 使用 Docker
docker exec -it business-postgres psql -U business -d business
```

**数据库 Schemas**:
```
business_data  - 业务数据主存储
staging        - 临时暂存区
archive        - 归档数据
public         - 系统默认 schema
```

**配置文件**: `business/.env`
```bash
BUSINESS_POSTGRES_USER=business
BUSINESS_POSTGRES_PASSWORD=business_password
BUSINESS_POSTGRES_DB=business
BUSINESS_POSTGRES_PORT=5433
```

---

### MinIO (业务文件存储)

**连接信息**:
```
API Endpoint:      http://localhost:9000  (⚠️ 注意：不是 9002)
Console URL:       http://localhost:9001  (⚠️ 注意：不是 9003)
Access Key:        minioadmin
Secret Key:        minioadmin
Default Bucket:    addp-data
```

**Web 控制台访问**:
```
URL:      http://localhost:9001
Username: minioadmin
Password: minioadmin
```

**S3 兼容配置**:
```bash
# AWS CLI 格式
export AWS_ACCESS_KEY_ID=minioadmin
export AWS_SECRET_ACCESS_KEY=minioadmin
export AWS_ENDPOINT_URL=http://localhost:9000

# MinIO Client (mc)
mc alias set business-minio http://localhost:9000 minioadmin minioadmin
```

**从 ADDP 服务访问** (Docker 容器内):
```bash
# Endpoint
http://host.docker.internal:9000

# 配置示例
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9000
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin
```

**配置文件**: `business/.env`
```bash
BUSINESS_MINIO_ROOT_USER=minioadmin
BUSINESS_MINIO_ROOT_PASSWORD=minioadmin
BUSINESS_MINIO_API_PORT=9000
BUSINESS_MINIO_CONSOLE_PORT=9001
```

---

## 端口分配总览

| 服务 | ADDP 系统 | Business 业务 |
|------|-----------|---------------|
| PostgreSQL | 5432 | 5433 |
| Redis | 6379 | N/A |
| MinIO API | 9002 | 9000 |
| MinIO Console | 9003 | 9001 |
| Elasticsearch | 9200/9300 | N/A |

---

## 快速访问命令

### 查看所有容器状态
```bash
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

### 连接到数据库
```bash
# ADDP 系统数据库
docker exec -it addp-postgres psql -U addp -d addp

# Business 业务数据库
docker exec -it business-postgres psql -U business -d business
```

### 连接到 Redis
```bash
docker exec -it addp-redis redis-cli -a addp_redis
```

### 访问 MinIO 控制台
```bash
# ADDP 系统 MinIO
open http://localhost:9003

# Business 业务 MinIO
open http://localhost:9001
```

### 测试 Elasticsearch
```bash
curl http://localhost:9200/_cluster/health?pretty
```

---

## 生产环境安全加固

### 1. 修改所有默认密码

**PostgreSQL**:
```bash
# 生成强密码
openssl rand -base64 32

# 修改 .env 文件
POSTGRES_PASSWORD=<生成的强密码>
BUSINESS_POSTGRES_PASSWORD=<生成的强密码>
```

**Redis**:
```bash
# 生成强密码
openssl rand -base64 32

# 修改 .env 文件
REDIS_PASSWORD=<生成的强密码>
```

**MinIO**:
```bash
# 生成 Access Key 和 Secret Key
openssl rand -hex 20  # Access Key
openssl rand -base64 40  # Secret Key

# 修改 .env 文件
MINIO_ROOT_USER=<生成的 Access Key>
MINIO_ROOT_PASSWORD=<生成的 Secret Key>
BUSINESS_MINIO_ROOT_USER=<生成的 Access Key>
BUSINESS_MINIO_ROOT_PASSWORD=<生成的 Secret Key>
```

### 2. 启用 SSL/TLS

**PostgreSQL**:
- 配置 SSL 证书
- 修改连接字符串: `sslmode=require`

**Redis**:
- 使用 Redis TLS 支持
- 配置证书和密钥

**MinIO**:
- 配置 TLS 证书
- 使用 HTTPS endpoint

**Elasticsearch**:
- 启用 `xpack.security.enabled=true`
- 配置 HTTPS 和认证

### 3. 网络隔离

```bash
# 仅允许必要的端口对外开放
# 其他端口仅在 Docker 内部网络访问

# 示例：仅开放应用服务端口，数据库端口不对外
# 修改 docker-compose.yml:
ports:
  - "127.0.0.1:5432:5432"  # 仅本地访问
```

### 4. 访问控制

**PostgreSQL**:
- 创建专用用户，限制权限
- 禁用超级用户远程登录

**MinIO**:
- 创建 IAM policies
- 为不同应用创建不同的 Access Key
- 启用 Bucket Policy

**Elasticsearch**:
- 启用认证
- 配置角色和权限
- 限制索引访问

---

## 故障排查

### 无法连接数据库

```bash
# 检查容器状态
docker ps | grep postgres

# 查看日志
docker logs addp-postgres
docker logs business-postgres

# 测试连接
docker exec addp-postgres pg_isready -U addp
docker exec business-postgres pg_isready -U business
```

### 无法连接 Redis

```bash
# 检查容器状态
docker ps | grep redis

# 查看日志
docker logs addp-redis

# 测试连接
docker exec addp-redis redis-cli -a addp_redis ping
```

### MinIO 无法访问

```bash
# 检查容器状态
docker ps | grep minio

# 查看日志
docker logs addp-minio
docker logs business-minio

# 测试健康检查
curl http://localhost:9002/minio/health/live  # ADDP
curl http://localhost:9000/minio/health/live  # Business
```

### Elasticsearch 无法连接

```bash
# 检查容器状态
docker ps | grep elasticsearch

# 查看日志
docker logs addp-elasticsearch

# 测试连接
curl http://localhost:9200/_cluster/health
```

---

## 备份和恢复

### PostgreSQL 备份

```bash
# ADDP 系统数据库
docker exec addp-postgres pg_dump -U addp -d addp > addp_backup_$(date +%Y%m%d).sql

# Business 业务数据库
docker exec business-postgres pg_dump -U business -d business > business_backup_$(date +%Y%m%d).sql
```

### PostgreSQL 恢复

```bash
# ADDP 系统数据库
docker exec -i addp-postgres psql -U addp -d addp < addp_backup.sql

# Business 业务数据库
docker exec -i business-postgres psql -U business -d business < business_backup.sql
```

### Redis 备份

```bash
# 触发 RDB 快照
docker exec addp-redis redis-cli -a addp_redis SAVE

# 复制 dump.rdb
docker cp addp-redis:/data/dump.rdb ./redis_backup_$(date +%Y%m%d).rdb
```

### MinIO 备份

```bash
# 使用 mc mirror 命令
mc mirror addp-minio/addp-data ./minio_backup/addp/
mc mirror business-minio/addp-data ./minio_backup/business/
```

---

## 相关文档

- [业务基础设施使用文档](business/README.md)
- [业务基础设施架构说明](business/ARCHITECTURE.md)
- [快速参考](business/QUICK_REFERENCE.md)
- [主项目文档](CLAUDE.md)
