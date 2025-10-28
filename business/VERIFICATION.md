# Business Infrastructure Verification

## 验证时间
2025-01-23

## 容器分离验证

### ✅ 容器独立性

所有容器已成功分离为两个独立的编排组：

**Business Infrastructure** (business 编排组):
```
Container ID: 13e78561bbe7
Name: business-postgres
Image: postgres:15-alpine
Ports: 5433 -> 5432
Project: business
Status: ✅ Healthy

Container ID: 28c54cb01cc4
Name: business-minio
Image: minio/minio:latest
Ports: 9000 -> 9000 (API), 9001 -> 9001 (Console)
Project: business
Status: ✅ Healthy
```

**ADDP System** (addp 编排组):
```
Container ID: 51fb0c7ccbc0
Name: addp-postgres
Image: postgres:15-alpine
Ports: 5432 -> 5432
Project: addp
Status: ✅ Healthy

Container ID: 8eeb214f7e29
Name: addp-minio-system
Image: minio/minio:latest
Ports: 9002 -> 9000 (API), 9003 -> 9001 (Console)
Project: addp
Status: ✅ Healthy
```

### ✅ 端口分配

所有端口已正确分配，无冲突：

| 服务 | 端口 | 状态 |
|------|------|------|
| ADDP PostgreSQL | 5432 | ✅ |
| Business PostgreSQL | 5433 | ✅ |
| ADDP MinIO API | 9002 | ✅ |
| ADDP MinIO Console | 9003 | ✅ |
| Business MinIO API | 9000 | ✅ |
| Business MinIO Console | 9001 | ✅ |

### ✅ 数据库验证

**Business PostgreSQL**:
- 用户: `business`
- 数据库: `business`
- Schemas:
  - ✅ `business_data` - 业务数据存储
  - ✅ `staging` - 临时暂存区
  - ✅ `archive` - 归档数据
- 表:
  - ✅ `business_data.data_source_registry` - 数据源注册表

**ADDP PostgreSQL**:
- 用户: `addp`
- 数据库: `addp`
- Schemas:
  - ✅ `system` - 系统元数据
  - ✅ `manager` - 管理器数据
  - ✅ `metadata` - 元数据索引
  - ✅ `transfer` - 传输任务

### ✅ MinIO 验证

两个 MinIO 实例独立运行，健康检查正常：

```bash
# Business MinIO
$ curl http://localhost:9000/minio/health/live
✅ OK

# ADDP System MinIO
$ curl http://localhost:9002/minio/health/live
✅ OK
```

### ✅ 网络隔离

- Business containers: `business-network`
- ADDP containers: `addp_addp-network`
- 两个网络完全隔离，容器间通过 `host.docker.internal` 通信

### ✅ 配置文件验证

**变量名冲突解决**:

原问题：business/.env 中的变量被父目录 .env 覆盖

解决方案：使用 `BUSINESS_` 前缀

| 原变量名 | 新变量名 | 值 |
|---------|---------|-----|
| POSTGRES_USER | BUSINESS_POSTGRES_USER | business |
| POSTGRES_PASSWORD | BUSINESS_POSTGRES_PASSWORD | business_password |
| POSTGRES_DB | BUSINESS_POSTGRES_DB | business |
| POSTGRES_PORT | BUSINESS_POSTGRES_PORT | 5433 |
| MINIO_ROOT_USER | BUSINESS_MINIO_ROOT_USER | minioadmin |
| MINIO_ROOT_PASSWORD | BUSINESS_MINIO_ROOT_PASSWORD | minioadmin |
| MINIO_API_PORT | BUSINESS_MINIO_API_PORT | 9000 |
| MINIO_CONSOLE_PORT | BUSINESS_MINIO_CONSOLE_PORT | 9001 |

## 测试命令

### 查看所有容器
```bash
docker ps --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"
```

### 查看编排组
```bash
# Business
cd business && docker compose ps

# ADDP
cd .. && docker compose ps
```

### 测试数据库连接
```bash
# Business
docker exec business-postgres psql -U business -d business -c "SELECT version();"

# ADDP
docker exec addp-postgres psql -U addp -d addp -c "SELECT version();"
```

### 测试 MinIO 连接
```bash
# Business MinIO Console
open http://localhost:9001

# ADDP MinIO Console
open http://localhost:9003
```

## 启动顺序

### 推荐启动流程
```bash
# 1. 启动业务基础设施（必须先启动）
cd business
docker compose up -d

# 2. 等待健康检查通过
docker compose ps

# 3. 启动 ADDP 系统
cd ..
docker compose up -d

# 4. 验证所有服务
docker ps
```

### 快速启动脚本
```bash
# 使用 business/scripts/start.sh
cd business
./scripts/start.sh

# 返回并启动 ADDP
cd ..
make dev-start
```

## 故障排查记录

### 问题 1: 端口冲突
**症状**: business-postgres 绑定到 5432 而非 5433

**原因**: Docker Compose 继承父目录的 .env 文件，导致变量冲突

**解决**:
1. 修改 business/docker-compose.yml 使用 `BUSINESS_` 前缀
2. 更新 business/.env 使用新变量名
3. 重新创建容器

### 问题 2: MinIO 容器共享
**症状**: addp 和 business 使用同一个 MinIO 容器

**原因**: 旧容器未正确清理

**解决**:
1. 停止所有容器: `docker compose down`
2. 删除旧容器: `docker rm addp-minio postgres-business`
3. 分别启动两个编排组

### 问题 3: init-db.sql 变量替换
**症状**: SQL 脚本中 `${POSTGRES_USER:-business}` 无法替换

**原因**: PostgreSQL 的 init 脚本不支持 shell 变量语法

**解决**: 使用 SQL 标准的 `CURRENT_USER` 关键字

## 验证清单

- [x] business-postgres 在端口 5433
- [x] addp-postgres 在端口 5432
- [x] business-minio 在端口 9000-9001
- [x] addp-minio-system 在端口 9000-9001
- [x] 所有容器 ID 不同
- [x] 容器分属不同的 compose project
- [x] business-postgres 有正确的 schema
- [x] business-postgres 有 data_source_registry 表
- [x] 两个 MinIO 实例健康检查通过
- [x] 网络隔离正确
- [x] 配置文件变量名不冲突

## 后续工作

### Manager 模块配置更新
Manager 模块已配置为使用业务 MinIO：
```yaml
environment:
  - MINIO_ENDPOINT=${BUSINESS_MINIO_ENDPOINT:-host.docker.internal:9000}
  - MINIO_ACCESS_KEY=${BUSINESS_MINIO_ACCESS_KEY:-minioadmin}
  - MINIO_SECRET_KEY=${BUSINESS_MINIO_SECRET_KEY:-minioadmin}
```

### 待验证
- [ ] Manager 能否正确连接 business-minio
- [ ] Meta 能否正确扫描 business-postgres 中的数据
- [ ] Transfer 能否在两个存储间传输数据

## 结论

✅ **所有容器已成功分离为两个独立的编排组**
✅ **端口分配正确，无冲突**
✅ **数据库和存储独立运行，健康状态良好**
✅ **配置文件变量名已修复，避免冲突**

业务基础设施分离工作已完成，可以开始下一阶段的开发和测试。
