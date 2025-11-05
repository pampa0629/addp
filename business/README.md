# Business Infrastructure Services

## 概述

本目录包含**业务数据基础设施**的独立容器编排配置，与 ADDP 系统本身的基础设施分离部署。

## 架构设计

### 为什么分离业务基础设施？

ADDP 平台采用**系统与业务数据分离**的架构设计：

```
┌─────────────────────────────────────────────────────────────┐
│  ADDP 系统 (主 docker-compose.yml)                          │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ postgres     │  │ redis        │  │ minio-system │     │
│  │ (系统元数据) │  │ (缓存/队列)  │  │ (系统文件)   │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Application Services                                 │  │
│  │ system / manager / meta / transfer                   │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  Business Infrastructure (business/docker-compose.yml)      │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐                        │
│  │ postgres     │  │ minio        │                        │
│  │ (业务数据库) │  │ (业务文件)   │                        │
│  └──────────────┘  └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

### 职责划分

| 组件 | 位置 | 用途 | 示例数据 |
|------|------|------|----------|
| **postgres (ADDP)** | 主编排 | 存储 ADDP 系统的元数据 | 用户账号、资源配置、元数据索引、任务定义 |
| **postgres (Business)** | 业务编排 | 存储用户通过 ADDP 管理的实际业务数据 | 用户上传的 PostgreSQL 数据、Shapefile 数据表 |
| **minio-system (ADDP)** | 主编排 | 存储 ADDP 系统文件 | 用户头像、系统配置文件、日志归档 |
| **minio (Business)** | 业务编排 | 存储用户上传的业务文件 | Shapefile、GeoJSON、图片、视频、PDF |

### 优势

1. **数据隔离**: 系统数据与业务数据物理分离，互不影响
2. **独立扩展**: 业务数据量增长时，可单独扩展业务基础设施
3. **安全性**: 业务数据库可配置更严格的访问控制
4. **备份策略**: 系统数据和业务数据可采用不同的备份频率
5. **可替换性**: 业务基础设施可替换为云服务（如 RDS、OSS）而不影响 ADDP 系统

## 快速开始

### 1. 配置环境变量

```bash
cd business
cp .env.example .env
# 编辑 .env 修改数据库密码等配置
```

### 2. 启动业务基础设施

**推荐方式（使用脚本）**:
```bash
# 启动所有服务并自动安装 PostGIS 扩展
./scripts/start.sh

# 或单独安装 PostGIS（如果之前已启动）
./scripts/install-postgis.sh
```

**手动方式**:
```bash
# 启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f
```

### 3. 验证服务

```bash
# 测试 PostgreSQL 连接
docker exec -it business-postgres psql -U business -d business

# 验证 PostGIS 扩展
docker exec -it business-postgres psql -U business -d business -c "\dx"
# 应该看到 postgis 和 postgis_topology 扩展

# 访问 MinIO 控制台
open http://localhost:9001
# 默认账号: minioadmin / minioadmin
```

## PostGIS 空间数据支持

业务 PostgreSQL 数据库已配置 PostGIS 扩展以支持空间数据（Shapefile、GeoJSON 等）。

### 自动安装

使用 `./scripts/start.sh` 启动时会自动安装 PostGIS 扩展。

### 手动安装

如果需要手动安装或重新安装 PostGIS：

```bash
cd business
./scripts/install-postgis.sh
```

该脚本会：
1. 检查 PostGIS 是否已安装
2. 安装 `postgis` 扩展（空间数据核心）
3. 安装 `postgis_topology` 扩展（拓扑分析，可选）
4. 显示已安装的扩展列表

### 使用空间数据

在 Transfer 模块导入空间数据时：

1. **选择目标 schema**（推荐使用 `business_data`）：
   - 目标表名填写：`business_data.spatial_points`
   - 系统会自动创建 schema 和表

2. **空间数据类型支持**：
   - `GEOMETRY` - 通用几何类型
   - `POINT` - 点
   - `LINESTRING` - 线
   - `POLYGON` - 面
   - `MULTIPOINT`, `MULTILINESTRING`, `MULTIPOLYGON` - 多部件类型

3. **示例导入 Shapefile**：
   ```
   数据源: MinIO bucket (已上传 Shapefile)
   目标数据源: Business PostgreSQL
   目标表: business_data.beijing_boundaries
   ```

### 4. 连接到 ADDP

业务基础设施启动后，在 ADDP 系统中配置数据源时使用以下连接信息：

**PostgreSQL 数据源**:
- Host: `host.docker.internal` (Docker内) 或 `localhost` (本地)
- Port: `5433`
- Database: `business`
- Username: `business`
- Password: `business_password` (根据 .env 配置)

**MinIO 数据源**:
- Endpoint: `host.docker.internal:9000` (Docker内) 或 `localhost:9000` (本地)
- Access Key: `minioadmin`
- Secret Key: `minioadmin`

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| PostgreSQL | 5433 | 业务数据库（避免与 ADDP 系统的 5432 冲突）|
| MinIO API | 9000 | 对象存储 API（避免与 ADDP 系统的 9002 冲突）|
| MinIO Console | 9001 | MinIO Web 控制台（避免与 ADDP 系统的 9003 冲突）|

## 数据管理

### 备份业务数据

```bash
# 备份 PostgreSQL
docker exec business-postgres pg_dump -U business -d business > backup.sql

# 备份 MinIO（使用 mc 命令行工具）
mc mirror business-minio/addp-data ./minio-backup
```

### 恢复业务数据

```bash
# 恢复 PostgreSQL
docker exec -i business-postgres psql -U business -d business < backup.sql

# 恢复 MinIO
mc mirror ./minio-backup business-minio/addp-data
```

### 数据迁移

如果需要将业务数据迁移到云服务：

1. **迁移到云数据库 (如 AWS RDS, 阿里云 RDS)**:
   ```bash
   # 导出数据
   pg_dump -h localhost -p 5433 -U business -d business > export.sql

   # 导入到云数据库
   psql -h <云数据库地址> -U <用户名> -d <数据库名> < export.sql
   ```

2. **迁移到云对象存储 (如 AWS S3, 阿里云 OSS)**:
   ```bash
   # 使用云服务提供的同步工具
   aws s3 sync s3://business-bucket/ ./local-backup/
   # 或
   ossutil sync oss://business-bucket/ ./local-backup/
   ```

## 运维命令

```bash
# 查看容器状态
docker-compose ps

# 查看日志
docker-compose logs -f postgres
docker-compose logs -f minio

# 重启服务
docker-compose restart

# 停止服务
docker-compose down

# 停止服务并删除数据（⚠️ 危险操作）
docker-compose down -v

# 进入 PostgreSQL 容器
docker exec -it business-postgres bash

# 进入 MinIO 容器
docker exec -it business-minio sh
```

## 安全建议

1. **生产环境必须修改默认密码**:
   - PostgreSQL: `POSTGRES_PASSWORD`
   - MinIO: `MINIO_ROOT_USER` 和 `MINIO_ROOT_PASSWORD`

2. **限制网络访问**:
   - 使用防火墙限制端口访问
   - 考虑只允许 Docker 内部网络访问

3. **定期备份**:
   - 建议每天自动备份业务数据
   - 备份文件存储在异地

4. **监控**:
   - 监控磁盘使用率
   - 监控数据库连接数
   - 设置告警阈值

## 故障排查

### PostgreSQL 无法连接

```bash
# 检查容器是否运行
docker-compose ps

# 查看日志
docker-compose logs postgres

# 测试连接
docker exec business-postgres pg_isready -U business
```

### MinIO 无法访问

```bash
# 检查容器状态
docker-compose ps

# 查看日志
docker-compose logs minio

# 测试健康检查
curl http://localhost:9000/minio/health/live
```

### 数据持久化问题

```bash
# 检查 volume
docker volume ls | grep business

# 查看 volume 详情
docker volume inspect business_postgres_data
docker volume inspect business_minio_data
```

## 相关文档

- [ADDP 主文档](../CLAUDE.md)
- [Docker Compose 官方文档](https://docs.docker.com/compose/)
- [PostgreSQL 文档](https://www.postgresql.org/docs/)
- [MinIO 文档](https://min.io/docs/)
