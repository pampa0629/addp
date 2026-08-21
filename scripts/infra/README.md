# ADDP 基础设施管理脚本

## 职责范围

本目录脚本**仅管理 ADDP 系统基础设施容器**，不涉及 business 容器。

### 管理的容器（addp-*）

| 容器名称 | 服务 | 用途 |
|---------|------|------|
| addp-postgres | PostgreSQL 15 + PostGIS | ADDP 系统元数据 |
| addp-redis | Redis 7 | 缓存、事件和分布式锁 |
| addp-minio | MinIO | 系统文件存储 |
| addp-meilisearch | Meilisearch | 全文搜索 |
| addp-redpanda | Redpanda v24.3.18 | 唯一内部 Kafka API CDC 总线 |
| addp-redpanda-init | Redpanda `rpk` 一次性任务 | SCRAM 用户、Connect internal topics 和 ACL 幂等初始化 |
| addp-kafka-connect | Debezium Connect 3.6.0.Final | 数据库日志捕获运行时 |

Infra Kafka 固定使用 Redpanda，不提供 Apache Kafka/Redpanda 运行时选择。broker service/container/DNS 分别固定为 `redpanda`、`addp-redpanda` 和 `redpanda:29092`；`addp-redpanda-init` 使用同一 Redpanda 镜像内置的 `rpk`，不是第二个 broker 或数据面。

### 不管理的容器（business-*）

business 容器由 `business/` 目录独立管理，可脱离 ADDP 部署。

用户部署 business 数据库后，通过 ADDP System 模块注册引擎，即可在 ADDP 中使用。

## 脚本说明

### 核心管理

- **up.sh** - 启动 ADDP 基础设施
  ```bash
  bash scripts/infra/up.sh
  ```

- **down.sh** - 停止 ADDP 基础设施（默认保留数据）
  ```bash
  bash scripts/infra/down.sh           # 保留数据卷
  bash scripts/infra/down.sh -v        # 删除数据卷
  bash scripts/infra/down.sh --force   # 跳过确认
  ```

- **status.sh** - 查看基础设施状态
  ```bash
  bash scripts/infra/status.sh
  ```

### 初始化（由 up.sh 自动调用）

- **init-postgresql.sh** - 初始化 PostgreSQL（扩展、Schema）
- **init-redis.sh** - 初始化 Redis 配置
- **init-minio.sh** - 初始化 MinIO buckets
- **init-meilisearch.sh** - 初始化 Meilisearch 索引
- **init-redpanda.sh** - 初始化 SCRAM 用户、Kafka Connect internal topics 和 infra principal ACL；`VERIFY_ACL=1` 时执行临时 topic 权限验收

### Infra Kafka 认证

默认 `docker-compose.infra.yml` 使用唯一 `redpanda_data` 卷，不向 Transfer 任务、API、数据库或 System Engine 引入发行版字段。完整认证会重建 `addp-redpanda` 和 `addp-kafka-connect`，存在活动 connector 时脚本会拒绝执行：

```bash
bash scripts/test/certify-infra-kafka.sh
```

生产拓扑认证使用 `docker-compose.infra.ha.yml`，在同一 Kafka API 数据面中启动 3 broker、2 个同组 Connect worker、SASL_SSL、RF=3 和 Raft majority；宿主机 listener 为 `19092`、`19093`、`19094`：

```bash
bash scripts/test/certify-infra-kafka-ha.sh
```

脚本会生成临时 CA/证书，验证 `acks=all`、关闭 write caching、单 broker `SIGKILL`、双 broker quorum、Connect owner failover、PostgreSQL/MySQL CDC、DLQ、retention、Stop cleanup、性能和资源，并在所有退出路径恢复默认单机 Redpanda profile。认证证据写入操作系统临时目录。

## 项目隔离

### PostgreSQL database 清单

`addp-postgres` 实例只长期保留以下非模板 database：

| database | 用途 | 生命周期与约束 |
|----------|------|----------------|
| `addp` | ADDP 开发环境系统数据库 | 必须保留；应用服务只连接该库，禁止运行破坏性测试门禁。 |
| `addp_test` | Transfer、Manager、Quality、Graph 和 Infra Kafka 等共享集成测试库 | 必须保留；只允许测试使用，各测试负责清理自己拥有的 Schema 或事实。 |
| `addp_iam_test` | System IAM、Fosite、API 与 Migration PostgreSQL 发布门禁库 | 必须保留；只允许 `make test-system-iam-postgres` 串行使用，门禁会重建 `system` 和 `common` Schema。 |
| `postgres` | PostgreSQL 默认维护连接库 | 必须保留；只用于管理操作，不存放 ADDP 业务表。 |

`template0` 和 `template1` 是 PostgreSQL 内置模板库；`template_postgis` 是当前 PostGIS 镜像提供的空间数据库模板。三者都不属于 ADDP 业务清单，也不得删除。临时测试 database 必须使用带独立 `test` 或 `disposable` 段的名称，并在任务结束后删除；不得长期保留 `addp_iam_test` 之外的 `addp_iam_*` database。CI 使用每个 Job 独占的临时 PostgreSQL 15 实例和 `addp_iam_test`，不连接开发环境 Infra。

本地 IAM 发布门禁使用：

```bash
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://addp:addp_password@localhost:15432/addp_iam_test?sslmode=disable' \
  make test-system-iam-postgres
```

### Docker Compose 项目

- `addp-infra` - 本目录管理（`docker-compose.infra.yml`）
- `addp-app` - 应用服务（`docker-compose.yml`）
- `business` - 业务数据库（`business/docker-compose.yml`）

### 网络隔离

- `addp-network` - ADDP 系统和应用共享
- `business-network` - business 数据库独立网络

business 容器不连接 `addp-network`，确保隔离性。

## 常见问题

### Q: 为什么 down.sh 删除了 business 容器？

**可能原因**：
1. `business/docker-compose.yml` 缺少 `name: business` 定义
2. Docker Compose 项目隔离配置错误

**解决**：
1. 确保 `business/docker-compose.yml` 顶部有 `name: business`
2. 执行前先用 `bash scripts/infra/status.sh` 检查容器列表
3. 如看到 business 容器，立即取消并检查配置

### Q: 如何确认只操作 addp 容器？

```bash
bash scripts/infra/status.sh
```

应该只显示：
- addp-postgres
- addp-redis
- addp-minio
- addp-meilisearch

如果出现 `business-*` 容器，说明配置有误，请勿继续操作。

### Q: 如何完全清理并重建？

```bash
# 1. 停止并删除所有数据（警告：数据丢失）
bash scripts/infra/down.sh -v

# 2. 重新启动
bash scripts/infra/up.sh
```

删除数据卷会丢失：
- PostgreSQL 所有数据库和表
- Redis 所有缓存和临时协调状态
- MinIO 所有文件
- Meilisearch 所有索引

### Q: 如何单独重启某个服务？

```bash
# 重启 PostgreSQL
docker compose -f docker-compose.infra.yml restart postgres

# 重启 Redis
docker compose -f docker-compose.infra.yml restart redis

# 重启 MinIO
docker compose -f docker-compose.infra.yml restart minio

# 重启 Meilisearch
docker compose -f docker-compose.infra.yml restart meilisearch
```

## 端口映射

| 服务 | 容器端口 | 主机端口 |
|-----|---------|---------|
| PostgreSQL | 5432 | 15432 |
| Redis | 6379 | 16379 |
| MinIO API | 9000 | 19000 |
| MinIO Console | 9001 | 19001 |
| Meilisearch | 7700 | 17700 |

## 开发提示

- **查看日志**：`docker compose -f docker-compose.infra.yml logs -f <service>`
- **进入容器**：`docker exec -it <container-name> bash`
- **修改配置**：编辑 `.env` 后需重启服务

## 相关文档

- [配置说明](../../docs/spec/addp配置介绍.md)
- [端口分配](../../docs/spec/addp端口分配.md)
- [部署步骤](../../docs/guide/addp部署和开发步骤.md)

1. **单一职责**: 同样功能只在一处实现,其他地方调用
2. **适应性**: 适应不同环境(OS、CPU架构),脚本自动适配
3. **清晰明了**: 一看就懂的结构和命名
4. **可重复执行**: 幂等性,多次执行不会破坏系统
5. **易用性**: 用户无需了解技术细节,按顺序执行即可
6. **分散和集中**: 模块相关配置分散,整体管理脚本集中
7. **敢于删除**: 删除重复或无用的内容,避免违反单一职责原则

## 目录结构

```
scripts/infra/
├── README.md                         # 本文件
├── up.sh                             # 启动基础设施服务
├── down.sh                           # 停止基础设施服务
├── status.sh                         # 查看服务状态
├── init-db.sh                        # 初始化数据库（执行所有 SQL 文件）
├── init-db.sql                       # 主数据库 schema 初始化（所有模块）
├── init-minio.sh                     # 初始化 MinIO buckets（模块化）
├── init-redis.sh                     # 初始化 Redis 配置
├── init-postgresql.sh                # 初始化 PostgreSQL 扩展（PostGIS + pgvector）
└── init-meilisearch.sh               # 初始化 Meilisearch 索引（模块化）
```

## 使用方法

### 快速启动（推荐）

```bash
# 1. 启动基础设施（自动完成所有初始化）
./scripts/infra/up.sh

# 2. 查看状态
./scripts/infra/status.sh

# 3. 停止基础设施
./scripts/infra/down.sh
```

`up.sh` 会自动执行以下操作:
- ✅ 检查并拉取 Docker 镜像
- ✅ 启动容器（PostgreSQL, Redis, MinIO, Meilisearch）
- ✅ 等待服务健康检查
- ✅ 初始化数据库 schema
- ✅ 安装 PostgreSQL 扩展（PostGIS + pgvector）
- ✅ 初始化 MinIO buckets
- ✅ 初始化 Meilisearch 索引

### 通过 Makefile（推荐）

```bash
# 启动基础设施
make infra-up

# 查看状态
make infra-status

# 停止基础设施
make infra-down

# 初始化数据库
make db-migrate

# 初始化 MinIO
make init-minio

# 初始化 Redis
make init-redis
```

### 手动初始化脚本

如果需要单独运行某个初始化脚本:

```bash
# 初始化数据库（支持清理选项）
./scripts/infra/init-db.sh                     # 正常初始化（幂等）
./scripts/infra/init-db.sh --drop-schema meta  # 重建 meta schema
./scripts/infra/init-db.sh --drop-all          # 清空所有 ADDP schema（慎用！）

# 初始化 MinIO buckets（模块化 buckets）
./scripts/infra/init-minio.sh

# 初始化 PostgreSQL 扩展（PostGIS + pgvector）
./scripts/infra/init-postgresql.sh

# 初始化 Meilisearch 索引
./scripts/infra/init-meilisearch.sh

# 检查 Redis 缓存、事件和分布式锁
./scripts/infra/init-redis.sh

# 查看所有服务状态
./scripts/infra/status.sh

# 停止所有基础设施服务
./scripts/infra/down.sh
```

## 模块化资源隔离架构

ADDP 采用**模块化资源隔离**架构,每个模块拥有独立的命名空间:

### PostgreSQL Schema 隔离

```
addp (database)
├── system       → System 模块（用户、租户、日志、资源）
├── manager      → Manager 模块（数据源、目录、快显）
├── meta         → Meta 模块（元数据节点、元数据项、字典）
├── transfer     → Transfer 模块（任务、执行记录、检查点）
├── orchestrator → Orchestrator 模块（编排定义、执行实例）
└── develop      → Develop 模块（SQL 脚本管理）
```

### MinIO Bucket 隔离

```
MinIO
├── system/             → System 模块（用户头像、系统配置、审计日志归档）
│   └── tenant_<id>/audit-logs/ → 审计日志归档对象；平台级日志使用 tenant_0
├── manager/            → Manager 模块（预览缓存、瓦片缓存对象）
│   └── tenant_<id>/vector-tile-cache/ → PMTiles 快显缓存对象，由 Manager API 按 storage_ref 访问
├── meta/               → Meta 模块（元数据相关文件）
├── transfer/           → Transfer 模块（传输临时文件）
├── orchestrator/       → Orchestrator 模块（编排文件）
└── develop/            → Develop 模块（查询结果导出）
```

**访问策略**: `manager` bucket 保持私有。预览缓存和瓦片缓存对象统一通过 Manager API 访问，前端不直接依赖 MinIO bucket 路径。

### Redis Key 命名规范

格式: `{module}:{middleware}:{function}:{id}`

示例:
```
meta:cache:scan_last_time:123                  → Meta 模块上次扫描时间
meta:scan:lock:tenant:1:engine:123:namespace:* → Meta 扫描范围锁
manager:tenant_1:cache:mvt:spatial:*           → Manager 空间瓦片缓存
cleanup:tasks:<task-id>                        → Cleanup 临时协调状态
```

Redis 不承载 ADDP bounded execution 队列。Quality、Meta 和 Transfer bounded
统一从 PostgreSQL `common.task_executions` claim execution；Redis 仅保存可重建的
缓存、事件、分布式锁、限流和临时协调状态。

### Meilisearch Index 命名规范

格式: `{module}:{resource_type}`

示例:
```
meta:assets       → Meta 模块资产索引
manager:files     → Manager 模块文件索引
develop:results   → Develop 模块查询结果索引
```

## 脚本详细说明

### up.sh

**功能**: 启动基础设施容器并完成所有初始化

**特性**:
- ✅ 自动检测 CPU 架构（x86_64/ARM64）并选择合适的 PostgreSQL 镜像
- ✅ 自动检查并拉取缺失的 Docker 镜像
- ✅ 端口占用检查（5432, 6379, 9000-9001, 7700）
- ✅ 服务运行状态检查（幂等操作,已运行服务不会重启）
- ✅ 健康检查等待（确保服务完全就绪）
- ✅ 自动调用所有初始化脚本

**环境变量**:
- `SKIP_INFRA_DB_INIT=1` - 跳过数据库初始化
- `SKIP_POSTGRESQL_INIT=1` - 跳过 PostgreSQL 扩展安装
- `SKIP_MEILISEARCH_INIT=1` - 跳过 Meilisearch 索引初始化

**使用示例**:
```bash
# 正常启动（完整初始化）
./scripts/infra/up.sh

# 跳过数据库初始化（仅启动容器）
SKIP_INFRA_DB_INIT=1 ./scripts/infra/up.sh

# ARM64 架构强制使用特定镜像
POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4 ./scripts/infra/up.sh
```

### down.sh

**功能**: 停止基础设施容器

**特性**:
- ✅ 安全停止所有服务
- ✅ 保留数据卷（数据不丢失）
- ✅ 显式指定 docker-compose.infra.yml

**使用示例**:
```bash
./scripts/infra/down.sh
```

### status.sh

**功能**: 查看服务状态和健康检查

**输出信息**:
- 容器运行状态
- PostgreSQL 连接状态
- Redis 连接状态
- MinIO 连接状态
- Meilisearch 连接状态
- 访问地址和端口

**使用示例**:
```bash
./scripts/infra/status.sh
```

### init-db.sh

**功能**: 初始化数据库 schema

**特性**:
- ✅ 幂等性（使用 `CREATE ... IF NOT EXISTS`）
- ✅ 支持选择性清理（`--drop-schema`, `--drop-all`）
- ✅ 执行 `init-db.sql`（包含所有模块 schema）
- ✅ 自动检查 PostgreSQL 容器是否运行

**使用示例**:
```bash
# 正常初始化（幂等,不会删除数据）
./scripts/infra/init-db.sh

# 重建 meta schema（删除并重新创建）
./scripts/infra/init-db.sh --drop-schema meta

# 清空所有 ADDP schema（慎用！会删除所有数据）
./scripts/infra/init-db.sh --drop-all
```

### init-minio.sh

**功能**: 初始化 MinIO buckets（模块化组织）

**创建的 Buckets**:
- `system` - System 模块（用户头像、系统配置、审计日志归档）
- `manager` - Manager 模块（预览缓存、瓦片缓存对象）- **私有**
- `meta` - Meta 模块（元数据相关文件）
- `transfer` - Transfer 模块（传输临时文件）
- `orchestrator` - Orchestrator 模块（编排文件）
- `develop` - Develop 模块（查询结果导出）

**访问地址**:
- API: http://localhost:19000
- Console: http://localhost:19001

**使用示例**:
```bash
./scripts/infra/init-minio.sh
```

### init-postgresql.sh

**功能**: 安装 PostgreSQL 扩展（PostGIS + pgvector）

**安装的扩展**:
- **PostGIS 3.4** - 空间数据操作支持
  - postgis - 核心空间功能
- **pgvector 0.7.0** - 向量检索支持
  - 从源码编译安装
  - 支持向量嵌入和相似度搜索

**特性**:
- ✅ 幂等性（已安装扩展不会重复安装）
- ✅ 版本检测和显示
- ✅ 自动处理依赖包安装
- ✅ 编译后自动清理构建依赖

**使用示例**:
```bash
./scripts/infra/init-postgresql.sh
```

### init-meilisearch.sh

**功能**: 初始化 Meilisearch 索引（模块化命名）

**创建的索引**:
- `meta:assets` - Meta 模块统一索引（元数据资产）
- `manager:files` - Manager 模块文件索引（目录文件）
- `develop:results` - Develop 模块查询结果索引

**访问地址**: http://localhost:17700

**使用示例**:
```bash
./scripts/infra/init-meilisearch.sh
```

### init-redis.sh

**功能**: 验证 Redis 连接并显示缓存、事件和临时协调状态

**特性**:
- ✅ 连接验证
- ✅ 显示 Redis 统计信息
- ✅ 支持可选清理（`--clean` 参数）

**使用示例**:
```bash
# 验证连接和显示统计
./scripts/infra/init-redis.sh

# 清空所有 Redis 缓存和临时协调数据（慎用！）
./scripts/infra/init-redis.sh --clean
```

## SQL 初始化文件

### init-db.sql

**包含所有模块的 schema**（单一职责原则）:

1. **System Schema**
   - users - 用户账户
   - tenants - 租户信息
   - audit_logs - 审计日志
   - engines - 引擎配置（数据源连接信息）

2. **Manager Schema**
   - data_sources - 数据源连接
   - directories - 目录组织
   - permissions - 访问控制
   - quick_view - 快显配置

3. **Metadata Schema**
   - meta_node - 元数据节点（层次结构）
   - meta_item - 元数据项（叶子节点）
   - meta_dictionary - 节点类型和规则定义
   - meta_change_log - 变更追踪

4. **Transfer Schema**
   - tasks - 传输任务定义
   - task_executions - 任务执行历史
   - data_mappings - 字段映射配置
   - checkpoints - 任务检查点

5. **Orchestrator Schema**
   - orchestrations - 编排定义
   - orchestration_instances - 编排执行实例

6. **Develop Schema**
   - sql_scripts - SQL 脚本管理

**设计原则**:
- ✅ 所有模块 schema 统一管理（遵循单一职责原则）
- ✅ 使用 `CREATE ... IF NOT EXISTS`（幂等性）
- ✅ 外键约束确保数据完整性
- ✅ 自动时间戳（created_at, updated_at）
- ✅ 软删除支持（deleted_at）

## 跨平台支持

### CPU 架构自动检测

`up.sh` 自动检测 CPU 架构并选择合适的 PostgreSQL 镜像:

**x86_64**（默认）:
```bash
POSTGRES_IMAGE=postgis/postgis:15-3.4
```

**ARM64**（macOS M1/M2, ARM64 Linux）:
```bash
POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4
```

**手动覆盖**:
```bash
# 在 .env 文件中设置
POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4

# 或运行时指定
POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4 ./scripts/infra/up.sh
```

## 环境变量配置

### 配置文件说明

- **.env.example** - 配置模板（提交到 Git）
  - 包含所有默认值
  - 详细注释说明
  - 安全提示（生产环境必须修改的字段）

- **.env** - 实际配置（不提交到 Git）
  - 从 .env.example 复制
  - 填写实际密码和配置
  - 本地开发或生产部署使用

**遵循原则**:
- ✅ 单一来源: 默认值只在 .env.example 定义
- ✅ docker-compose.infra.yml 不提供默认值（强制从 .env 读取）
- ✅ 开发环境可使用默认值
- ✅ 生产环境必须修改敏感配置

### 关键环境变量

```bash
# PostgreSQL（必填）
POSTGRES_USER=addp
POSTGRES_PASSWORD=addp_password          # ⚠️ 生产环境必须修改
POSTGRES_DB=addp

# Redis（必填）
REDIS_PASSWORD=addp_redis                # ⚠️ 生产环境必须修改

# MinIO（必填）
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin           # ⚠️ 生产环境必须修改
MINIO_API_PORT=19000
MINIO_CONSOLE_PORT=19001

# Meilisearch（必填）
MEILISEARCH_MASTER_KEY=your-master-key   # ⚠️ 生产环境必须修改
MEILISEARCH_URL_LOCAL=http://localhost:17700

# Infra Kafka / Kafka Connect（CDC 内部基础设施）
REDPANDA_IMAGE=docker.redpanda.com/redpandadata/redpanda:v24.3.18
REDPANDA_MEMORY=1G
INFRA_KAFKA_PORT=19092
INFRA_KAFKA_ADMIN_PASSWORD=change-in-production
INFRA_KAFKA_CONNECT_PASSWORD=change-in-production
INFRA_KAFKA_TRANSFER_PASSWORD=change-in-production
INFRA_KAFKA_SASL_MECHANISM=scram-sha-256
INFRA_KAFKA_INTERNAL_REPLICATION_FACTOR=1
REDPANDA_HA_MEMORY=1G
REDPANDA_HA_KAFKA_2_PORT=19093
REDPANDA_HA_KAFKA_3_PORT=19094
KAFKA_CONNECT_IMAGE=quay.io/debezium/connect:3.6.0.Final
KAFKA_CONNECT_PORT=18083
KAFKA_CONNECT_BOOTSTRAP_SERVERS=redpanda:29092

# PostgreSQL 镜像（可选,默认 x86_64）
POSTGRES_IMAGE=postgis/postgis:15-3.4
# ARM64: POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4

# 跳过初始化（可选）
SKIP_INFRA_DB_INIT=0
SKIP_POSTGRESQL_INIT=0
SKIP_MEILISEARCH_INIT=0
```

## 注意事项

### 执行顺序

1. ✅ **先启动基础设施**: `./scripts/infra/up.sh`
2. ✅ **再启动应用服务**: `make dev-start` 或 `make up-full`

`up.sh` 会自动完成所有初始化,无需手动执行其他脚本。

### 数据持久化

所有数据存储在 Docker volumes 中:
- `postgres_data` - PostgreSQL 数据
- `redis_data` - Redis 数据
- `minio_data` - MinIO 对象存储
- `meilisearch_data` - Meilisearch 索引
- `redpanda_data` - Infra Kafka 日志、Connect internal topics 和 CDC topic

**停止容器不会丢失数据**,除非显式删除 volumes:
```bash
# 危险操作！会删除所有数据
docker compose -f docker-compose.infra.yml down -v
```

### 端口冲突检查

`up.sh` 启动前会自动检查以下端口:
- 15432 (PostgreSQL)
- 16379 (Redis)
- 19000 (MinIO API)
- 19001 (MinIO Console)
- 17700 (Meilisearch)

如果端口被占用,脚本会报错并提示处理方法。

### 健康检查

所有初始化脚本都会:
- ✅ 检查容器是否运行
- ✅ 等待服务完全就绪
- ✅ 验证连接成功
- ✅ 显示详细状态信息

### 幂等性保证

所有脚本和 SQL 都是幂等的:
- ✅ 可以多次执行
- ✅ 不会重复创建资源
- ✅ 不会删除已有数据（除非显式指定 --drop 参数）

## 容器命名规范

**Docker Compose 项目名**: `addp-infra`

所有基础设施容器使用统一的 `addp-` 前缀:
- `addp-postgres` - PostgreSQL 容器
- `addp-redis` - Redis 容器
- `addp-minio` - MinIO 容器
- `addp-meilisearch` - Meilisearch 容器

**命名优势**:
- ✅ 清晰标识: 一眼看出容器属于 ADDP 系统基础设施
- ✅ 避免冲突: 与其他项目(如 business-postgres)明确区分
- ✅ 批量操作: `docker ps --filter "name=addp-"` 精确过滤
- ✅ 便于管理: `docker compose -f docker-compose.infra.yml ps` 查看所有服务

## 故障排查

### PostgreSQL 连接失败

```bash
# 检查容器状态
docker ps | grep postgres

# 查看日志
docker logs postgres

# 手动测试连接
docker compose -f docker-compose.infra.yml exec postgres psql -U addp -d addp
```

### Redis 连接失败

```bash
# 检查容器状态
docker ps | grep redis

# 查看日志
docker logs redis

# 手动测试连接（注意替换密码）
docker compose -f docker-compose.infra.yml exec redis redis-cli -a 'addp_redis' ping
```

### MinIO 连接失败

```bash
# 检查容器状态
docker ps | grep minio

# 访问控制台
open http://localhost:19001

# 查看日志
docker logs minio
```

### Meilisearch 连接失败

```bash
# 检查容器状态
docker ps | grep meilisearch

# 测试连接
curl http://localhost:17700/health

# 查看日志
docker logs meilisearch
```

### 端口被占用

```bash
# 查看端口占用情况（macOS/Linux）
lsof -i :15432
lsof -i :16379
lsof -i :19000

# 杀掉占用端口的进程
kill -9 <PID>

# 或在 .env 中修改端口
MINIO_API_PORT=19010
MINIO_CONSOLE_PORT=19011
```

## 相关文档

- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构说明
- [docker-compose.infra.yml](../../docker-compose.infra.yml) - 基础设施容器配置
- [.env.example](../../.env.example) - 环境变量配置模板
- [docs/spec/addp配置介绍.md](../../docs/spec/addp配置介绍.md) - 配置分层与管理能力规范
- [scripts/infra/README.md](README.md) - 基础设施启动、检测和排障说明

## 更新日志

### v0.0.12 (2024-12-08)

**优化基础设施脚本（遵循7个核心原则）**:

1. ✅ **单一职责原则**:
   - 删除 `init-orchestrator.sql`（内容已合并到 `init-db.sql`）
   - 删除 `restart.sh`（功能重复）
   - 删除 `pull-images.sh`（功能已集成到 `up.sh`）
   - 删除 `fix-collation.sh`（临时修复脚本）
   - 合并 `init-postgis.sh` 和 `init-pgvector.sh` 为 `init-postgresql.sh`

2. ✅ **模块化资源隔离**:
   - MinIO buckets: `system/`, `manager/`, `meta/`, `transfer/`, `orchestrator/`, `develop/`
   - Redis keys: `{module}:{middleware}:{function}:{id}` 命名规范
   - Meilisearch indexes: `{module}:{resource_type}` 命名规范
   - 容器命名: 使用简洁名称,由 Docker Compose 项目名 `addp-infra` 统一管理

3. ✅ **环境变量管理**:
   - docker-compose.infra.yml 移除所有默认值（单一来源原则）
   - .env.example 作为唯一的默认值定义位置
   - 添加详细的安全提示注释

4. ✅ **跨平台支持**:
   - 自动检测 x86_64/ARM64 架构
   - 支持 PostgreSQL 镜像自动选择

5. ✅ **新增功能**:
   - Meilisearch 索引自动初始化
   - PostgreSQL 扩展统一安装脚本
   - 镜像自动检查和拉取
