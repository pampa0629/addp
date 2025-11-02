# System Backend 健康检查失败 - 快速修复指南

## 问题描述

部署后 System Backend 健康检查失败,导致服务不可用。

## 立即排查步骤

### 步骤 1: 传输诊断脚本到服务器

```bash
# 在本机运行
scp scripts/check-system-backend.sh pampa@192.168.1.182:~/
```

### 步骤 2: 在服务器上运行诊断

```bash
# SSH 到服务器
ssh pampa@192.168.1.182

# 运行诊断脚本
bash ~/check-system-backend.sh
```

这个脚本会自动检查:
- System Backend 容器状态
- 依赖服务 (PostgreSQL, Redis) 状态
- 最近的日志和错误
- 健康检查端点
- 环境变量配置
- 网络连接

## 常见问题和快速修复

### 问题 1: 容器未启动

**症状**: `docker compose ps system-backend` 显示容器不存在或已退出

**修复**:
```bash
cd ~/addp
docker compose -f docker-compose.prod.yml up -d system-backend
docker compose -f docker-compose.prod.yml logs -f system-backend
```

### 问题 2: 数据库连接失败

**症状**: 日志显示 "failed to connect to database" 或 "connection refused"

**可能原因**:
- PostgreSQL 未启动
- 密码不匹配
- 数据库未初始化

**修复**:
```bash
cd ~/addp

# 检查 PostgreSQL 状态
docker compose -f docker-compose.prod.yml ps postgres

# 查看 PostgreSQL 日志
docker compose -f docker-compose.prod.yml logs postgres

# 重启 PostgreSQL
docker compose -f docker-compose.prod.yml restart postgres

# 等待 PostgreSQL 启动
sleep 10

# 重启 System Backend
docker compose -f docker-compose.prod.yml restart system-backend
```

### 问题 3: .env.prod 文件缺失或配置错误

**症状**: 日志显示环境变量未设置

**修复**:
```bash
cd ~/addp

# 检查 .env.prod 文件是否存在
ls -la .env.prod

# 如果不存在,从示例创建
cp .env.prod.example .env.prod

# 编辑配置
vi .env.prod

# 必须设置的变量:
# JWT_SECRET=your-secret-key-change-this
# POSTGRES_PASSWORD=addp_password
# POSTGRES_USER=addp
# POSTGRES_DB=addp
# REDIS_PASSWORD=addp_redis

# 重启所有服务以应用新配置
docker compose -f docker-compose.prod.yml down
docker compose -f docker-compose.prod.yml up -d
```

### 问题 4: 端口冲突

**症状**: 日志显示 "address already in use"

**修复**:
```bash
# 检查端口占用
netstat -tlnp | grep 8080

# 如果有其他进程占用,杀掉它
kill <PID>

# 或者修改 docker-compose.prod.yml 中的端口映射
```

### 问题 5: 镜像架构不匹配

**症状**: 日志显示 "exec format error" 或 "platform mismatch"

**修复**:
```bash
cd ~/addp

# 检查镜像架构
docker inspect localhost:5001/addp-system-backend:latest | grep Architecture

# 重新拉取正确架构的镜像
docker compose -f docker-compose.prod.yml pull system-backend
docker compose -f docker-compose.prod.yml up -d system-backend
```

## 完整重启流程

如果上述方法都不奏效,执行完整重启:

```bash
cd ~/addp

# 1. 停止所有服务
docker compose -f docker-compose.prod.yml down

# 2. 清理旧数据 (可选,会丢失数据)
# docker volume rm addp_postgres_data addp_redis_data

# 3. 重新启动所有服务
docker compose -f docker-compose.prod.yml up -d

# 4. 监控启动日志
docker compose -f docker-compose.prod.yml logs -f
```

### 启动顺序验证

服务应该按以下顺序启动并变为健康:

1. **基础设施** (30-60秒):
   - postgres → healthy
   - redis → healthy
   - minio-system → healthy

2. **后端服务** (10-20秒):
   - system-backend → healthy (等待 postgres 和 redis)
   - manager-backend → healthy
   - meta-backend → healthy
   - transfer-backend → healthy

3. **网关和前端** (5-10秒):
   - gateway → healthy
   - portal → up
   - system-frontend → up
   - nginx → up

```bash
# 检查所有服务状态
docker compose -f docker-compose.prod.yml ps

# 等待所有服务健康
watch -n 2 'docker compose -f docker-compose.prod.yml ps | grep -E "(system-backend|postgres|redis)"'
```

## 手动测试 System Backend

### 1. 测试健康检查端点

```bash
# 从宿主机
curl http://localhost:8080/health

# 从容器内部
docker compose -f docker-compose.prod.yml exec system-backend wget -qO- http://localhost:8080/health
```

**期望输出**: `healthy`

### 2. 测试登录 API

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"SuperAdmin","password":"20251001#SuperAdmin"}'
```

**期望输出**: JSON 包含 token

### 3. 检查数据库连接

```bash
# 进入容器
docker compose -f docker-compose.prod.yml exec system-backend sh

# 测试数据库连接 (在容器内)
nc -zv postgres 5432
nc -zv redis 6379
```

## 查看详细日志

```bash
cd ~/addp

# 查看所有错误
docker compose -f docker-compose.prod.yml logs | grep -i "error\|fatal\|panic"

# 实时跟踪 System Backend 日志
docker compose -f docker-compose.prod.yml logs -f system-backend

# 查看最近 100 行
docker compose -f docker-compose.prod.yml logs --tail=100 system-backend

# 查看特定时间段的日志
docker compose -f docker-compose.prod.yml logs --since 5m system-backend
```

## 环境变量检查清单

确保 `.env.prod` 包含以下必需变量:

```bash
# 安全配置 (必须修改)
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production
ENCRYPTION_KEY=your-32-char-encryption-key-change-this

# PostgreSQL
POSTGRES_PASSWORD=addp_password
POSTGRES_USER=addp
POSTGRES_DB=addp
POSTGRES_HOST=postgres
POSTGRES_PORT=5432

# Redis
REDIS_PASSWORD=addp_redis
REDIS_HOST=redis
REDIS_PORT=6379

# MinIO
MINIO_SYSTEM_ROOT_USER=minioadmin
MINIO_SYSTEM_ROOT_PASSWORD=minioadmin
MINIO_SYSTEM_ENDPOINT=minio-system:9000

# Registry
REGISTRY=localhost:5001
```

## 网络诊断

```bash
# 检查 Docker 网络
docker network ls | grep addp
docker network inspect addp-network

# 检查容器是否在同一网络
docker compose -f docker-compose.prod.yml ps -q | xargs docker inspect --format='{{.Name}}: {{range .NetworkSettings.Networks}}{{.NetworkID}}{{end}}'

# 测试容器间连接
docker compose -f docker-compose.prod.yml exec system-backend ping -c 3 postgres
docker compose -f docker-compose.prod.yml exec system-backend ping -c 3 redis
```

## 常见错误模式

### 错误 1: "connection refused"
- PostgreSQL 或 Redis 未启动
- 网络配置问题
- 端口映射错误

### 错误 2: "exec format error"
- 镜像架构与服务器 CPU 架构不匹配
- 需要重新构建正确架构的镜像

### 错误 3: "database does not exist"
- 数据库未初始化
- init-db.sql 未执行
- 需要手动初始化数据库

### 错误 4: "password authentication failed"
- .env.prod 中的密码与实际不匹配
- 需要统一密码配置

## 获取帮助

如果问题仍未解决,请收集以下信息:

```bash
cd ~/addp

# 1. 服务状态
docker compose -f docker-compose.prod.yml ps > /tmp/addp-status.txt

# 2. System Backend 日志
docker compose -f docker-compose.prod.yml logs --tail=200 system-backend > /tmp/addp-backend-logs.txt

# 3. 基础设施日志
docker compose -f docker-compose.prod.yml logs postgres redis > /tmp/addp-infra-logs.txt

# 4. 环境配置
cat .env.prod | grep -v "PASSWORD\|SECRET" > /tmp/addp-env.txt

# 5. 系统信息
uname -a > /tmp/addp-system.txt
docker version >> /tmp/addp-system.txt

# 打包
tar -czf ~/addp-debug-$(date +%Y%m%d-%H%M%S).tar.gz /tmp/addp-*.txt
```

将 `addp-debug-*.tar.gz` 文件发送给技术支持。
