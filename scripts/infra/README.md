# ADDP 基础设施管理脚本

## 职责范围

本目录脚本**仅管理 ADDP 系统基础设施容器**，不涉及 business 容器。

### 管理的容器（addp-*）

| 容器名称 | 服务 | 用途 |
|---------|------|------|
| addp-postgres | PostgreSQL 15 + PostGIS | ADDP 系统元数据 |
| addp-redis | Redis 7 | 缓存、事件和分布式锁 |
| addp-falkordb | FalkorDB 4.20.6（固定 digest） | Ontology 可重建语义关系投影 |
| addp-minio | MinIO | 系统文件存储 |
| addp-meilisearch | Meilisearch | 全文搜索 |
| addp-redpanda | Redpanda v24.3.18 | 唯一内部 Kafka API CDC 总线 |
| addp-redpanda-init | Redpanda `rpk` 一次性任务 | SCRAM 用户、Connect internal topics 和 ACL 幂等初始化 |
| addp-kafka-connect | Debezium Connect 3.6.0.Final | 数据库日志捕获运行时 |

Infra MinIO 镜像由 `scripts/infra/Dockerfile.minio` 从固定的 MinIO Server 和 `mc` 官方源码修订构建；首次执行 `scripts/infra/up.sh` 时会构建，后续复用本地镜像。此构建不依赖已撤下的公开 MinIO 容器镜像或预编译二进制。

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

- **init-postgresql.sh** - 初始化 PostgreSQL（保留测试库、扩展、开发库 Schema）
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
| `addp_test` | 所有非 IAM 模块及 Infra Kafka 的共享集成测试库 | 必须保留；只允许测试使用，各测试负责清理自己拥有的 Schema 或事实。 |
| `addp_iam_test` | System IAM、Fosite、API 与 Migration PostgreSQL 发布门禁库 | 必须保留；只允许 `make test-system-iam-postgres` 串行使用，门禁会重建 `system` 和 `common` Schema。 |
| `postgres` | PostgreSQL 默认维护连接库 | 必须保留；只用于管理操作，不存放 ADDP 业务表。 |

`template0` 和 `template1` 是 PostgreSQL 内置模板库；`template_postgis` 是当前 PostGIS 镜像提供的空间数据库模板。三者都不属于 ADDP 业务清单，也不得删除。

`make infra-up` 调用 `init-postgresql.sh`，幂等确保 `addp_test` 与 `addp_iam_test` 存在，并在 `addp_test` 中安装 PostGIS。Common PostgreSQL 门禁依赖该扩展验证空间类型和可信空间函数。`addp_iam_test` 不安装 PostGIS；测试库也不执行开发库的模块 Schema 初始化 SQL。

本地共享 `addp-postgres` 禁止创建清单之外的测试 database；所有非 IAM 测试复用 `addp_test`，System IAM、Fosite、API 与 Migration 测试复用 `addp_iam_test`。本地测试必须调用根 `Makefile` 或 `scripts/test/` 的标准门禁，由门禁重建并清理自己拥有的 Schema 或测试事实；禁止为了单次验证直接执行 `createdb`、`CREATE DATABASE`、`dropdb` 或 `DROP DATABASE`。如果现有门禁不能提供所需隔离，应先修正门禁的重置和清理能力，不能用新增 database 绕过问题。

下文 DSN 中的 `15432` 仅为 ADDP 首选宿主机端口示例。端口发生自动调整时，先用 `bash scripts/infra/status.sh` 查询 `addp-postgres` 的实际映射，再向标准门禁提供对应 DSN；不得把占用首选端口的其他 PostgreSQL 实例当作 ADDP 测试库。

System IAM 标准门禁在首次重置 Schema 前获取宿主机级文件锁 `/tmp/addp-system-iam-postgres-gate.lock`，覆盖完整运行及其子进程，跨 checkout 和 `--package` 选择互斥。另一轮仍在运行时立即失败，不开始数据库操作；进程退出后由操作系统释放锁，锁文件保持原位，不能通过删除锁文件解除占用。该边界同样用于独占 CI Runner；分包验证也必须串行运行。

需要一次验证全部已登记基础设施集成门禁时，先显式配置各 owner 门禁要求的安全连接变量，再运行 `make test-integration`。该入口严格串行调用 PostgreSQL 和 MongoDB 模块级门禁，避免 `addp_test` 或 `addp_iam_test` 被并发重置；PostgreSQL 门禁不会创建新 database，也不会连接 `addp` 开发业务库。Manager MongoDB 门禁读取 `Outdoor/Persons`；目标为空时只创建一条确定性夹具，并在退出时恢复原状态。

Manager 统一派生任务表、语义唯一约束、资源绑定与资源回收生命周期使用 `addp_test` 验证：

```bash
MANAGER_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-manager-postgres
```

GitHub Actions 使用每个 Job 独占、随 Job 销毁的 PostgreSQL 15 Service，不连接本地开发环境 Infra；workflow 可以为 Job 创建专用测试 database，但必须由 workflow 声明并由隔离实例生命周期回收。

Ontology 修订/发布门禁使用 `ONTOLOGY_POSTGRES_TEST_DSN` 连接本地 `addp_test`，标准入口为 `make test-ontology-postgres`，并已纳入 `make test-module MODULE=ontology`、串行 `test-integration` 和辅助 macOS 巡检。门禁仅接管此前不存在的 ontology schema，用随机 Run ID 标记所有权；退出时核对标记并删除本轮执行记录和 schema，验证零残留，不删除或重建 common。CI 对应独占 `addp_ontology_test`，不访问开发库。具体语义范围见 [Ontology 模块说明](../../ontology/CLAUDE.md)。

Ontology 的 FalkorDB 已纳入 `docker-compose.infra.yml`，使用独立 `falkordb_data` 卷、RDB 快照与 `INFRA_FALKORDB_PASSWORD`。已有开发环境须在根 `.env` 补充独立密码，再通过标准 `up.sh` 启动；生产 `setup-env.sh` 为新环境生成随机 Secret，对已有环境只校验不轮换。宿主机仅绑定 `127.0.0.1`，首选端口为 `16479`，冲突时自动选择空闲端口；不开放 Browser、不复用 `addp-redis`、不注册为业务 Engine。该单机部署不等于生产 HA/TLS 已认证，也不代表 Ontology 发布运行时已接通。

正式部署与 `make test-ontology-falkor` 共用 `scripts/infra/falkordb.yml` 的固定镜像、非零超时、资源限制和带认证图健康检查。T2 使用独占 Compose Project、随机密码、动态回环端口及可销毁卷，验证保存快照并重建容器后的图恢复；不接受个人图数据库地址、不读取 `.env`，退出时删除并核验自有容器/卷/网络。该入口还要求 `ONTOLOGY_POSTGRES_TEST_DSN`，复用上述 PG owner 夹具验证首次投影执行、激活与失败保留旧版本，并清理本轮 schema/执行记录。PG 与 FalkorDB 门禁均被标准模块门禁自动发现及串行集成聚合；已有 `ontology-falkor` CI Job 同时提供独占 PostgreSQL Service，执行同一入口。

门禁通过 `ADDP_T2_INPUT_FILES` 声明共享服务定义、根 Compose、环境模板及相关生命周期脚本，供本地与 CI 共用的影响计算选择 Ontology。静态测试验证正式/T2 配置一致、独立回环端口、密码必填、生产密码拒绝和退出清理。

本地 IAM 发布门禁使用：

```bash
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://addp:addp_password@localhost:15432/addp_iam_test?sslmode=disable' \
  make test-system-iam-postgres
```

Common 的 PostgreSQL 和 MySQL Provider 门禁同时运行无损整数转换矩阵：小数非零、正负边界、NULL 和超过浮点精确范围的整数；并通过 PreparedQuery 验证业务结果被过滤为空时仍返回求值错误。MySQL 门禁额外检查原生转换没有警告。同一已登记的整数集成测试还运行 CASE/COALESCE 条件组合矩阵：true/false/NULL、条件自身失败、嵌套与首个非 NULL 值停止；有结果和空结果各验证一次，未选分支不报错，已求值错误不能被后续补值隐藏。 新增 AnalyticalArithmetic 集成测试同步登记到两个 Common 门禁，使用 math/big 有理数独立验证四则运算、舍入、溢出、NULL 与空结果检查；MySQL 在 div_precision_increment=0/4/30 下分别运行原生数值矩阵并拒绝警告。 AnalyticalExpressions 门禁验证通用表达式入口的列／参数／常量、文字精确比较、布尔三值语义及错误传播；旧数值和条件矩阵同样使用该入口，不保留 fixture 私有递归路径。

Common PostgreSQL Engine Provider 与 execution store 门禁使用（包含分析结果检查协议：空结果、过滤、分页不得跳过断言，以及读取集合和输出血缘；此项不代表完整分析编译器已认证）：

```bash
ADDP_TEST_POSTGRES_HOST=localhost \
ADDP_TEST_POSTGRES_PORT=15432 \
ADDP_TEST_POSTGRES_USER=addp \
ADDP_TEST_POSTGRES_PASSWORD=addp_password \
ADDP_TEST_POSTGRES_DATABASE=addp_test \
ADDP_TEST_POSTGRES_SSLMODE=disable \
  make test-common-postgres
```

MySQL Provider 与 Manager、Develop、Service、Transfer 四个数据保护 Owner 的组合门禁使用一次性 `addp_common_mysql_it_*` database，测试结束时自动删除；`ADDP_TEST_MYSQL_*` 必须指向可丢弃的 MySQL 8 测试实例，不得指向生产实例：

```bash
ADDP_TEST_MYSQL_HOST=localhost \
ADDP_TEST_MYSQL_PORT=3306 \
ADDP_TEST_MYSQL_USER=root \
ADDP_TEST_MYSQL_PASSWORD=password \
  make test-common-mysql-data-protection
```

指标编译的 MySQL 8 金样沿用同一组 `ADDP_TEST_MYSQL_*`，通过 `make test-model-mysql` 执行；该门禁经原生 Provider 完成读取集合、输出血缘和真实执行，使用独立且自动删除的 `addp_model_mysql_it_*` database。它与 Model 元数据的 PostgreSQL 门禁职责不同，不能相互替代。现有 MySQL T2 作业和模块门禁自动发现均已登记此入口。

直接运行上述 owner 门禁时，调用方必须提供 disposable MySQL。辅助 macOS 巡检的 `make local-ci` 不读取这些外部变量，也不依赖 Business MySQL；它通过 `scripts/test/docker-compose.local-macos-ci.yml` 启动固定 digest、无数据卷的专属 MySQL 8，在健康检查通过后才执行确定性门禁，并仅在 MySQL owner 与 Model MySQL 门禁进程内使用固定测试连接参数。

OceanBase owner 门禁使用同一个本地 CI Compose 项目中固定 digest、无数据卷的 OceanBase CE 4.4.2 LTS，宿主机端口为 `12881`。`make local-ci` 会清除继承的 `ADDP_TEST_OCEANBASE_*`，只向聚合门禁传递本地作用域标记；`common-oceanbase-gate.sh` 再为自身生成固定本地连接参数和 `addp_oceanbase_disposable` database。MySQL 与 OceanBase 服务都在巡检的所有退出路径中统一删除。

Model 物化与事务门禁使用：

```bash
ADDP_TEST_MODEL_POSTGRES_DSN='postgres://addp:addp_password@localhost:15432/addp_test?sslmode=disable' \
  make test-model-postgres
```

Asset 授权履约 Schema 门禁使用：

```bash
ASSET_POSTGRES_TEST_DSN='postgres://addp:addp_password@localhost:15432/addp_test?sslmode=disable' \
  make test-asset-postgres
```

Security Schema 门禁默认使用本地 `addp_test`；也可以显式覆盖为独占 disposable database：

```bash
SECURITY_POSTGRES_TEST_DSN='postgres://addp:addp_password@localhost:15432/addp_test?sslmode=disable' \
  make test-security-postgres
```

Transfer PostgreSQL schema、受保护 bounded snapshot 导出与既有表目标覆盖门禁使用：

```bash
ADDP_TEST_POSTGRES_HOST=localhost \
ADDP_TEST_POSTGRES_PORT=15432 \
ADDP_TEST_POSTGRES_USER=addp \
ADDP_TEST_POSTGRES_PASSWORD=addp_password \
ADDP_TEST_POSTGRES_DATABASE=addp_test \
ADDP_TEST_POSTGRES_SSLMODE=disable \
  make test-transfer-postgres
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

宿主机端口为本地首选值。若发生冲突，`scripts/infra/up.sh` 选择空闲端口；使用 `bash scripts/infra/status.sh` 查看实际映射。

| 服务 | 容器端口 | 主机端口 |
|-----|---------|---------|
| PostgreSQL | 5432 | 15432 |
| Redis | 6379 | 16379 |
| FalkorDB | 6379 | 16479（仅回环） |
| MinIO API | 9000 | 19000 |
| MinIO Console | 9001 | 19001 |
| Meilisearch | 7700 | 17700 |
| Infra Kafka | 9092 | 19092 |
| Kafka Connect | 8083 | 18083 |

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

# 初始化 MinIO
make init-minio

# 初始化 Redis
make init-redis
```

### 手动初始化脚本

如果需要单独运行某个初始化脚本:

```bash
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
- ✅ x86_64 自动构建仓库 PostgreSQL 镜像，其他缺失镜像自动拉取
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

**功能**: 准备保留测试库，安装 PostgreSQL 扩展，并初始化开发库 Schema

**准备的测试库**:
- `addp_test` - 幂等创建并安装 PostGIS，供非 IAM PostgreSQL 集成门禁串行复用
- `addp_iam_test` - 幂等创建，供 System IAM 发布门禁独占串行复用

**安装的扩展**:
- **PostGIS 3** - 通过 PostgreSQL 官方 PGDG bookworm 软件源安装
  - 在开发库和 `addp_test` 中创建 postgis 扩展
- **pgvector 0.8.0** - 向量检索支持
  - 从源码编译安装
  - 仅在开发库的 `manager` Schema 中支持向量嵌入和相似度搜索

**特性**:
- ✅ 幂等性（已安装扩展不会重复安装）
- ✅ 只创建规范允许长期保留的两个本地测试 database
- ✅ 不向测试 database 写入开发环境模块 Schema
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

## 跨平台支持

### CPU 架构自动检测

`up.sh` 自动检测 CPU 架构并选择合适的 PostgreSQL 镜像:

**x86_64**（默认）:
```bash
POSTGRES_IMAGE=addp-postgres-pgvector:latest
```

镜像缺失时，`up.sh` 自动从 `scripts/infra/Dockerfile.postgres` 构建，不依赖外部同名镜像仓库。

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

# PostgreSQL 镜像（可选；留空时按架构选择）
POSTGRES_IMAGE=
# ARM64: POSTGRES_IMAGE=imresamu/postgis-arm64:15-3.4

# 跳过初始化（可选）
SKIP_INFRA_DB_INIT=0
SKIP_POSTGRESQL_INIT=0
SKIP_MEILISEARCH_INIT=0
```

## 注意事项

### 执行顺序

1. ✅ **先启动基础设施**: `./scripts/infra/up.sh`
2. ✅ **再启动应用服务**: `make dev-start` 或 `bash scripts/dev/start.sh`

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
   - 删除重复的旧版数据库初始化脚本
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

Common PostgreSQL 门禁同时验证正式表结果的覆盖事务：重复覆盖、失败回滚、超时取消和并发写入。Model PostgreSQL 门禁验证正式表创建幂等、归属检查、结构漂移拒绝，以及物化任务排队、父子执行授权、固定版本和过期租约终态；Quality PostgreSQL 门禁验证物理表断言。均复用既有测试库与标准入口。

`make test-security-postgres` 同时执行 `common/schema` 的真实 PostgreSQL 启动所有权回归：同 schema 并发迁移一次执行、只读 Worker 校验、失败 DDL 和版本回滚及拒绝降级。使用 `addp_test` 内专用测试 schema，并在测试结束清理；不创建额外 database。
