# System Backend 启动失败修复指南

## 🔍 问题诊断

**错误现象**:
```
✘ Container addp-system-backend    Error
dependency failed to start: container addp-system-backend is unhealthy
```

## 🐛 根本原因

`docker-compose.prod.yml` 中 `system-backend` 的配置存在错误：

### 错误配置（已修复）:
```yaml
system-backend:
  environment:
    - PORT=8080
    - JWT_SECRET=${JWT_SECRET}
    - DB_PATH=/app/data/addp.db     # ❌ 错误：使用 SQLite
    - ENCRYPTION_KEY=${ENCRYPTION_KEY}
  volumes:
    - system_data:/app/data          # ❌ 错误：挂载 SQLite 数据目录
```

**问题分析**:
1. System 模块在生产环境应该使用 **PostgreSQL**，而不是 SQLite
2. 配置文件中使用了 `DB_PATH` 指向 SQLite 数据库文件
3. 缺少 PostgreSQL 连接信息（`POSTGRES_HOST`, `POSTGRES_PORT` 等）
4. 容器启动后无法连接到数据库，导致健康检查失败

### 正确配置:
```yaml
system-backend:
  environment:
    - PORT=8080
    - JWT_SECRET=${JWT_SECRET}
    - ENCRYPTION_KEY=${ENCRYPTION_KEY}
    - POSTGRES_HOST=postgres         # ✅ PostgreSQL 主机
    - POSTGRES_PORT=5432             # ✅ PostgreSQL 端口
    - POSTGRES_USER=${POSTGRES_USER:-addp}
    - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-addp_password}
    - POSTGRES_DB=${POSTGRES_DB:-addp}
    - GIN_MODE=release
  # 不再需要 volumes 挂载
```

---

## ✅ 修复内容

### 1. 修复 `docker-compose.prod.yml`

**变更内容**:
- ✅ 移除 `DB_PATH` 环境变量
- ✅ 添加 PostgreSQL 连接环境变量
- ✅ 移除 `system_data` volume 挂载
- ✅ 移除 volumes 定义中的 `system_data`

### 2. 修复 `.env.example`

**变更内容**:
- ✅ 添加 `ENCRYPTION_KEY` 配置说明
- ✅ 确保所有 PostgreSQL 配置变量存在

### 3. 修复 `scripts/generate-env.sh`

**变更内容**:
- ✅ 修正 MinIO 环境变量名称：`MINIO_SYSTEM_ROOT_PASSWORD` → `MINIO_ROOT_PASSWORD`
- ✅ 确保自动生成 `ENCRYPTION_KEY`

---

## 🚀 完整部署流程

### 在开发机上操作

#### 步骤 1: 确保本地文件是最新的

```bash
cd ~/code/addp

# 检查关键文件是否已修复
git status

# 应该看到这些文件的修改:
#   modified:   docker-compose.prod.yml
#   modified:   .env.example
#   modified:   scripts/generate-env.sh
```

#### 步骤 2: 推送所有镜像到私有 Registry

```bash
# 2.1 推送基础设施镜像（包含 PostgreSQL init）
./scripts/push-infrastructure-images.sh 5001

# 2.2 推送应用镜像
./scripts/push-to-local-registry-multiarch-cached.sh 5001
```

#### 步骤 3: 传输文件到服务器

```bash
# 传输关键配置文件
scp docker-compose.prod.yml pampa@192.168.31.174:~/addp/
scp .env.example pampa@192.168.31.174:~/addp/
scp scripts/generate-env.sh pampa@192.168.31.174:~/addp/scripts/
scp scripts/clean-deploy.sh pampa@192.168.31.174:~/addp/scripts/
scp scripts/diagnose-startup.sh pampa@192.168.31.174:~/addp/scripts/
```

---

### 在服务器上操作

#### 步骤 1: 生成生产环境配置

```bash
ssh pampa@192.168.31.174
cd ~/addp

# 生成安全的 .env 文件
chmod +x scripts/generate-env.sh
./scripts/generate-env.sh 192.168.31.238:5001

# ⚠️ 重要: 脚本会生成随机密钥并保存到 .env.secrets.TIMESTAMP.txt
# 请妥善保管这个文件，里面包含所有密钥和密码
```

#### 步骤 2: 检查生成的 .env 文件

```bash
cat .env

# 确认以下配置存在且正确:
# - REGISTRY=192.168.31.238:5001
# - JWT_SECRET=<随机生成的密钥>
# - ENCRYPTION_KEY=<随机生成的密钥>
# - POSTGRES_PASSWORD=<随机生成的密码>
# - REDIS_PASSWORD=<随机生成的密码>
# - MINIO_ROOT_PASSWORD=<随机生成的密码>
```

#### 步骤 3: 完全清理并重新部署

```bash
# 使用清理部署脚本
chmod +x scripts/clean-deploy.sh
REGISTRY=192.168.31.238:5001 ./scripts/clean-deploy.sh

# 脚本会执行以下操作:
# 1. 停止并删除所有旧容器
# 2. 验证 docker-compose.prod.yml 配置
# 3. 拉取最新镜像
# 4. 检查或创建 .env 文件
# 5. 启动所有服务
```

#### 步骤 4: 验证部署结果

```bash
# 查看所有容器状态
docker-compose -f docker-compose.prod.yml ps

# 应该看到所有容器状态为 Up (healthy)
# NAME                     STATUS
# addp-postgres            Up (healthy)
# addp-redis               Up (healthy)
# addp-minio-system        Up (healthy)
# addp-elasticsearch       Up (healthy)
# addp-system-backend      Up (healthy)   ✅ 这个应该是健康的
# addp-system-frontend     Up
```

#### 步骤 5: 检查 system-backend 日志

```bash
# 查看启动日志
docker logs addp-system-backend

# 应该看到类似输出:
# [GIN-debug] Listening and serving HTTP on :8080
# Successfully connected to database
# Server started successfully

# 测试健康检查端点
curl http://localhost:8080/health

# 应该返回:
# {"status":"ok"}
```

---

## 🔧 故障排查

### 如果 system-backend 仍然 unhealthy

运行诊断脚本：

```bash
chmod +x scripts/diagnose-startup.sh
./scripts/diagnose-startup.sh
```

### 常见问题

#### 问题 1: 数据库连接失败

**错误日志**:
```
Error connecting to database: dial tcp 172.18.0.2:5432: connect: connection refused
```

**解决方法**:
```bash
# 检查 PostgreSQL 容器状态
docker logs addp-postgres

# 确保 PostgreSQL 已完全启动
docker exec addp-postgres pg_isready -U addp

# 如果返回 "accepting connections"，说明数据库正常
```

#### 问题 2: 环境变量未设置

**错误日志**:
```
JWT_SECRET not set or too short
```

**解决方法**:
```bash
# 检查 .env 文件
cat .env | grep JWT_SECRET

# 如果为空或太短，重新生成
./scripts/generate-env.sh 192.168.31.238:5001
```

#### 问题 3: 端口冲突（9001）

**错误信息**:
```
Bind for 0.0.0.0:9001 failed: port is already allocated
```

**解决方法**:

参考 [FIX_PORT_CONFLICT.md](FIX_PORT_CONFLICT.md)

---

## 📋 验证清单

部署成功后，验证以下内容：

- [ ] 所有基础设施容器状态为 `Up (healthy)`
  - [ ] PostgreSQL (port 5432)
  - [ ] Redis (port 6379)
  - [ ] MinIO System (port 9000-9001)
  - [ ] Elasticsearch (port 9200)

- [ ] system-backend 容器状态为 `Up (healthy)`
  - [ ] 日志中无错误信息
  - [ ] 健康检查端点返回 200: `curl http://localhost:8080/health`
  - [ ] 能连接到 PostgreSQL

- [ ] system-frontend 容器正常运行
  - [ ] 能访问前端: `curl http://localhost:8090`

- [ ] 如果启动了完整服务（`--profile full`）
  - [ ] manager-backend: `curl http://localhost:8081/health`
  - [ ] meta-backend: `curl http://localhost:8082/health`
  - [ ] gateway: `curl http://localhost:8000/health`

---

## 🎯 核心要点

1. **生产环境使用 PostgreSQL**
   - System 模块在生产环境必须使用 PostgreSQL
   - 不要使用 SQLite（`DB_PATH`）

2. **环境变量完整性**
   - 必须配置所有 PostgreSQL 连接参数
   - 必须设置 `JWT_SECRET` 和 `ENCRYPTION_KEY`
   - 使用 `generate-env.sh` 自动生成安全密钥

3. **部署顺序**
   - 先部署 Business 基础设施（如果需要）
   - 再部署 ADDP 系统
   - 确保端口不冲突

4. **使用清理部署脚本**
   - `clean-deploy.sh` 会完全清理旧部署
   - 避免旧容器配置缓存问题

---

## 📚 相关文档

- [完整部署指南](DEPLOY_TO_SERVER.md)
- [端口冲突解决](FIX_PORT_CONFLICT.md)
- [init-db.sql 错误修复](FIX_INITDB_ERROR.md)
- [脚本修复说明](SCRIPT_FIXES.md)

---

现在系统应该能够正常启动了！🚀
