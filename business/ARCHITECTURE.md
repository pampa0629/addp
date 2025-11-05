# Business Infrastructure Architecture

## 架构变更说明

本文档说明 ADDP 平台业务基础设施分离的架构设计和实现。

**核心理念**: 业务基础设施与 ADDP 系统基础设施**完全独立**，使用官方 Docker Hub 镜像，无需自建仓库，简化部署流程。

## 变更概览

### 之前（单一基础设施）

```
docker-compose.yml
├── postgres (5432)       → 混合存储：系统元数据 + 业务数据
├── redis (6379)          → 缓存和队列
└── minio (9000-9001)     → 混合存储：系统文件 + 业务文件
```

**问题**：
- ❌ 系统数据和业务数据混在一起，难以独立管理
- ❌ 业务数据增长会影响系统性能
- ❌ 备份策略无法区分系统和业务数据
- ❌ 无法灵活替换业务存储为云服务

### 之后（分离架构）

```
主 docker-compose.yml (ADDP System)
├── postgres (5432)           → 仅系统元数据
├── redis (6379)              → 缓存和队列
├── minio-system (9002-9003)  → 仅系统文件
└── elasticsearch (9200)      → 全文检索

business/docker-compose.yml (Business Infrastructure - 完全独立)
├── postgis (5433)            → 仅业务数据 (官方镜像: postgis/postgis:15-3.4-alpine)
└── minio (9000-9001)         → 仅业务文件 (官方镜像: minio/minio:latest)
```

**优势**：
- ✅ 系统与业务数据物理隔离
- ✅ 可独立扩展业务基础设施
- ✅ 可针对业务数据配置更严格的安全策略
- ✅ 业务存储可灵活替换为云服务（RDS、OSS）
- ✅ 备份恢复策略更灵活
- ✅ **无需自建镜像仓库**，直接使用官方镜像
- ✅ **支持空间数据**，PostGIS 扩展开箱即用
- ✅ **无容器依赖**，可独立于 ADDP 系统部署

## 数据职责划分

### ADDP System Infrastructure

**PostgreSQL (系统数据库 - 端口 5432)**:
- `system` schema: 用户账号、租户信息、审计日志
- `manager` schema: 数据源配置（连接信息）、目录结构、权限
- `metadata` schema: 元数据索引、节点树、字典定义
- `transfer` schema: 传输任务定义、执行历史、映射规则

**MinIO System (系统文件存储 - 端口 9002-9003)**:
- 用户头像
- 系统配置文件
- 日志归档文件
- 系统生成的临时文件

### Business Infrastructure

**PostGIS (业务数据库 - 端口 5433)**:
- 用户通过 ADDP 管理的实际业务数据
- 例如：用户上传的 PostgreSQL 数据源中的表数据
- 例如：Shapefile 转换后的空间数据
- 动态创建的业务表（默认使用 public schema）
- **PostGIS 扩展支持**: 空间数据类型、空间索引、空间查询函数
- **官方镜像**: `postgis/postgis:15-3.4-alpine`

**MinIO Business (业务文件存储 - 端口 9000-9001)**:
- 用户上传的文件
- Shapefile 文件及其组成部分（.shp, .dbf, .shx, .prj）
- GeoJSON、KML、GML 等空间数据文件
- 图片、视频、PDF 等媒体文件
- 用户导出的数据文件
- **官方镜像**: `minio/minio:latest`

## 文件结构

```
addp/
├── docker-compose.yml              # ADDP 系统容器编排
├── .env                            # ADDP 系统环境配置
├── .env.example                    # 配置模板（已更新）
│
├── business/                       # 业务基础设施目录（新增）
│   ├── docker-compose.yml          # 业务基础设施编排
│   ├── .env                        # 业务基础设施配置（需自行创建）
│   ├── .env.example                # 业务配置模板
│   ├── init-db.sql                 # 业务数据库初始化脚本
│   ├── README.md                   # 业务基础设施使用文档
│   ├── ARCHITECTURE.md             # 本文档
│   └── scripts/
│       ├── start.sh                # 启动脚本
│       └── stop.sh                 # 停止脚本
│
├── system/                         # System 模块
├── manager/                        # Manager 模块（已更新配置）
├── meta/                           # Meta 模块
└── transfer/                       # Transfer 模块
```

## 配置变更

### 主 .env 文件变更

**之前**:
```bash
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
```

**之后**:
```bash
# ADDP系统MinIO
MINIO_SYSTEM_ROOT_USER=minioadmin
MINIO_SYSTEM_ROOT_PASSWORD=minioadmin

# 业务MinIO（在 business/docker-compose.yml 中部署）
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9000
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin
```

### Manager Backend 配置变更

**之前**:
```yaml
environment:
  - MINIO_ENDPOINT=minio:9000
  - MINIO_ACCESS_KEY=minioadmin
  - MINIO_SECRET_KEY=minioadmin
depends_on:
  - minio
```

**之后**:
```yaml
environment:
  - MINIO_ENDPOINT=${BUSINESS_MINIO_ENDPOINT:-host.docker.internal:9000}
  - MINIO_ACCESS_KEY=${BUSINESS_MINIO_ACCESS_KEY:-minioadmin}
  - MINIO_SECRET_KEY=${BUSINESS_MINIO_SECRET_KEY:-minioadmin}
depends_on:
  - minio-system  # 仍然依赖系统MinIO（用于系统功能）
```

### docker-compose.yml 变更

**服务名变更**:
- `minio` → `minio-system`
- `postgres` → 保持不变（但职责变为仅系统数据）

**Volume 变更**:
- `minio_data` → `minio_system_data`

## 部署流程

### 开发环境

```bash
# 1. 启动业务基础设施（必须先启动）
cd business
cp .env.example .env
./scripts/start.sh

# 2. 启动 ADDP 系统
cd ..
make dev-start
```

### Docker 生产环境

```bash
# 1. 部署业务基础设施
cd business
cp .env.example .env
# 编辑 .env 配置密码
docker-compose up -d

# 2. 部署 ADDP 系统
cd ..
make up-full
```

## 网络连接

### Docker 内部服务

ADDP 服务访问业务基础设施使用 `host.docker.internal`:
```bash
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9000
```

### 本地开发环境

本地运行的 ADDP 服务访问业务基础设施使用 `localhost`:
```bash
BUSINESS_MINIO_ENDPOINT=localhost:9000
```

## 数据迁移

### 从旧架构迁移到新架构

如果你有已存在的 ADDP 部署，需要执行以下迁移步骤：

#### 1. 备份现有数据

```bash
# 备份 PostgreSQL 业务数据
docker exec addp-postgres pg_dump -U addp -d addp --schema=业务schema > business_backup.sql

# 备份 MinIO 业务文件
mc mirror addp-minio/addp-data ./business_files_backup
```

#### 2. 部署业务基础设施

```bash
cd business
cp .env.example .env
docker-compose up -d
```

#### 3. 恢复业务数据

```bash
# 恢复 PostgreSQL 数据
docker exec -i business-postgres psql -U business -d business < business_backup.sql

# 恢复 MinIO 文件
mc mirror ./business_files_backup business-minio/addp-data
```

#### 4. 更新配置并重启 ADDP 系统

```bash
cd ..
# 更新 .env 文件
docker-compose down
docker-compose pull
docker-compose up -d
```

## 监控和维护

### 健康检查

```bash
# 检查业务基础设施状态
cd business
docker-compose ps

# 测试 PostgreSQL 连接
docker exec business-postgres pg_isready -U business

# 测试 MinIO API
curl http://localhost:9000/minio/health/live
```

### 日志查看

```bash
# 业务基础设施日志
cd business
docker-compose logs -f

# 单独查看 PostgreSQL 日志
docker-compose logs -f postgres

# 单独查看 MinIO 日志
docker-compose logs -f minio
```

### 备份策略建议

**业务数据备份（高频）**:
- PostgreSQL: 每天自动备份
- MinIO: 每天增量备份，每周全量备份

**系统数据备份（低频）**:
- PostgreSQL: 每周自动备份
- MinIO System: 按需备份

## 故障恢复

### 业务数据库故障

```bash
# 1. 停止 ADDP 系统
docker-compose down

# 2. 恢复业务数据库
cd business
docker-compose down
docker volume rm business_postgres_data
docker-compose up -d postgres
docker exec -i business-postgres psql -U business -d business < backup.sql

# 3. 重启 ADDP 系统
cd ..
docker-compose up -d
```

### 业务文件存储故障

```bash
# 1. 停止 ADDP 系统（可选，建议停止以避免数据不一致）
docker-compose down

# 2. 恢复 MinIO 数据
cd business
docker-compose down
docker volume rm business_minio_data
docker-compose up -d minio
mc mirror ./backup/minio-data business-minio/

# 3. 重启 ADDP 系统
cd ..
docker-compose up -d
```

## 云服务迁移

### 迁移到云数据库（如 RDS）

1. 导出业务数据：
   ```bash
   pg_dump -h localhost -p 5433 -U business -d business > export.sql
   ```

2. 导入到云数据库：
   ```bash
   psql -h <rds-endpoint> -U <username> -d <database> < export.sql
   ```

3. 更新 Manager/Meta 配置，指向云数据库

4. 停止本地业务 PostgreSQL：
   ```bash
   cd business
   docker-compose stop postgres
   ```

### 迁移到云对象存储（如 OSS）

1. 使用云服务 CLI 同步数据：
   ```bash
   # 阿里云 OSS
   ossutil sync oss://your-bucket/ ./backup/

   # AWS S3
   aws s3 sync s3://your-bucket/ ./backup/
   ```

2. 更新 Manager 配置，指向云存储

3. 停止本地业务 MinIO：
   ```bash
   cd business
   docker-compose stop minio
   ```

## 安全加固建议

### 生产环境清单

- [ ] 修改所有默认密码
- [ ] 配置防火墙规则（仅允许必要端口）
- [ ] 启用 PostgreSQL SSL 连接
- [ ] 启用 MinIO TLS 加密
- [ ] 配置业务数据定期自动备份
- [ ] 设置磁盘使用率告警
- [ ] 启用审计日志
- [ ] 限制 MinIO 控制台访问 IP

### 最小权限原则

为 ADDP 服务创建专用数据库用户，仅授予必要权限：

```sql
-- 在业务 PostgreSQL 中创建只读用户（用于查询）
CREATE USER addp_reader WITH PASSWORD 'secure_password';
GRANT CONNECT ON DATABASE business TO addp_reader;
GRANT USAGE ON SCHEMA business_data TO addp_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA business_data TO addp_reader;

-- 创建读写用户（用于数据导入）
CREATE USER addp_writer WITH PASSWORD 'secure_password';
GRANT CONNECT ON DATABASE business TO addp_writer;
GRANT USAGE, CREATE ON SCHEMA business_data TO addp_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA business_data TO addp_writer;
```

## 常见问题

### Q1: Manager 无法连接业务 MinIO

**症状**: Manager 日志显示 MinIO 连接超时

**解决**:
1. 检查业务基础设施是否运行：`cd business && docker-compose ps`
2. 检查端口是否正确：确认 `BUSINESS_MINIO_ENDPOINT` 指向 `9000` 而非 `9002`
3. 检查网络连接：`curl http://localhost:9000/minio/health/live`

### Q2: 业务 PostgreSQL 端口冲突

**症状**: 启动业务 PostgreSQL 失败，提示端口 5433 已被占用

**解决**:
1. 修改 `business/.env` 中的 `POSTGRES_PORT` 为其他端口
2. 重启：`docker-compose down && docker-compose up -d`

### Q3: 数据卷占用空间过大

**症状**: 磁盘空间不足

**解决**:
1. 清理旧数据：定期归档或删除不需要的业务数据
2. 配置 MinIO 生命周期策略，自动删除过期文件
3. 考虑迁移到云存储以获得弹性扩展能力

## 相关文档

- [业务基础设施使用文档](README.md)
- [ADDP 主文档](../CLAUDE.md)
- [Manager 模块文档](../manager/CLAUDE.md)
- [Meta 模块文档](../meta/CLAUDE.md)
