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
  bash scripts/infra/down.sh -v        # 交互输入确认短语后删除数据卷
  bash scripts/infra/down.sh --force   # 仅跳过非 ADDP 容器确认
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

`make infra-up` 调用 `init-postgresql.sh`，幂等确保 `addp_test` 与 `addp_iam_test` 存在，并在 `addp_test` 中安装 PostGIS。Common PostgreSQL 门禁依赖该扩展验证空间类型和可信空间函数；Manager 的 `make test-manager-postgres` 同时验证物化视图的 SRID／2D／Z／M／ZM 声明和无业务 SELECT 权限的目录核验，自建测试 Schema 与事务内角色在退出时清理并核对零残留。`addp_iam_test` 不安装 PostGIS；测试库也不执行开发库的模块 Schema 初始化 SQL。

本地共享 `addp-postgres` 禁止创建清单之外的测试 database；所有非 IAM 测试复用 `addp_test`，System IAM、Fosite、API 与 Migration 测试复用 `addp_iam_test`。本地测试必须调用根 `Makefile` 或 `scripts/test/` 的标准门禁，由门禁重建并清理自己拥有的 Schema 或测试事实；禁止为了单次验证直接执行 `createdb`、`CREATE DATABASE`、`dropdb` 或 `DROP DATABASE`。如果现有门禁不能提供所需隔离，应先修正门禁的重置和清理能力，不能用新增 database 绕过问题。

下文 DSN 中的 `15432` 仅为 ADDP 首选宿主机端口示例。每次从新会话执行本地门禁前，先运行 `bash scripts/infra/status.sh`，确认 `addp-postgres` 的实际映射和健康状态，再把该端口显式填入 `*_POSTGRES_TEST_DSN` 或 `ADDP_TEST_POSTGRES_PORT`。根 `.env` 记录首选值，不记录动态运行结果；不要根据 `.env` 或端口号猜测测试库身份。标准 PostgreSQL T2 门禁会在执行测试前核对本地回环目标与当前工作区容器的实际映射，并限制本地 database 为 `addp_test` 或 `addp_iam_test`；映射不符或无法核实即停止。若实际映射不是示例端口，必须替换下文命令中的端口，不得把占用首选端口的其他 PostgreSQL 实例当作 ADDP 测试库。

只需机器可读的 PostgreSQL 宿主机端口时，在仓库根目录执行以下只读命令；`addp_infra_verify_container` 会先核对容器属于当前工作区的 ADDP Infra：

```bash
bash -c 'source scripts/infra/ports.sh && addp_infra_verify_container postgres addp-postgres && addp_infra_mapped_port addp-postgres 5432'
```

System IAM 标准门禁在首次重置 Schema 前获取宿主机级文件锁 `/tmp/addp-system-iam-postgres-gate.lock`，覆盖完整运行及其子进程，跨 checkout 和 `--package` 选择互斥。另一轮仍在运行时立即失败，不开始数据库操作；进程退出后由操作系统释放锁，锁文件保持原位，不能通过删除锁文件解除占用。该边界同样用于独占 CI Runner；分包验证也必须串行运行。

该门禁对每个 Go 测试包显式设置 20 分钟上限。完整 Migration 回归会反复重放历史版本，累计运行可能超过 Go 默认的 10 分钟包级超时；单个迁移阶段仍保留自身期限，CI Job 仍以 30 分钟限制整轮运行，不通过拆包、跳过场景或取消超时规避完整验收。

Manager 剖析执行授权的 PostgreSQL 夹具从同一数据库的 `clock_timestamp()` 读取认证时间，与正式 Browser 会话的墙钟校验一致，避免主机与 Docker VM 时钟差触发“认证时间在未来”；正式拒绝未来认证时间的规则保持不变。

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

Security Schema 门禁必须显式提供 `SECURITY_POSTGRES_TEST_DSN`，本地仅使用 `addp_test`，CI 可使用 Job 独占的 disposable database。宿主机端口发生自动调整时，DSN 必须填写 ADDP PostgreSQL 的实际映射端口：

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
- `addp-platform` - 平台服务（`docker-compose.yml`）
- `addp-runtimes` - 内置计算与 Notebook Runtime（`docker-compose.runtimes.yml`）
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

`down.sh -v` 在本地必须由交互终端输入 `DELETE addp-infra VOLUMES`，`--force` 不会跳过这项确认；非交互删除仅供已完成独立准入的 GitHub Online 一次性环境使用。脚本还会核对容器的 Compose 工作目录，拒绝从另一个 worktree 停止本工作区的容器。

### 本地 Infra 备份与隔离恢复演练

`make infra-backup BACKUP_ROOT=/绝对路径/ADDP-backups` 从当前工作区拥有的运行中 Infra PostgreSQL 和 MinIO 只读导出 `addp` 数据库、Bucket 元数据/IAM 与当前对象，并复制根 `.env`。输出目录必须在仓库外，权限为当前用户独占；完成时生成逐文件 SHA-256 清单。备份中含凭据，不能提交到 Git、共享给他人或放入无加密的云同步目录。

`make infra-restore-drill BACKUP_DIR=/绝对路径/ADDP-backups/<备份目录>` 先校验清单，再使用与来源相同的镜像创建两个没有宿主机端口、没有持久卷的一次性容器。PostgreSQL 在隔离容器内真实恢复并核对关键表行数；MinIO 在隔离容器内导入 Bucket/IAM 并逐 Bucket 恢复对象，下载后比较文件哈希。演练结束仅清理本轮带所有权标签的一次性容器。该入口不连接或重置当前 `addp`、`addp_test`、`addp_iam_test`，不停止服务，不修改 Infra/Business/SCP 卷。

默认本地备份覆盖当前 System 数据库和 Infra MinIO 的当前对象版本，不覆盖 Business 引擎、Redis、FalkorDB、Meilisearch、Kafka 的持久事实，也不保存 MinIO 对象历史版本。显式收录 Meilisearch dump 的范围见下文；它仍不是原始搜索卷副本。正式平台备份还需界定这些状态的恢复来源、使用独立存储与加密保留策略，并做定期演练；本地单份备份不等于灾备。

已确认的 Meilisearch 扩展边界：普通备份仍不包含搜索卷，不自动调用 `POST /dumps`。仅当用户显式提供 `MEILISEARCH_DUMP=/仓库外私有目录/<文件>.dump` 和 `MEILISEARCH_DUMP_TASK=<已成功导出任务UID>` 时，标准入口收录该既有文件。必须核对本工作区运行中搜索容器的所有权、成功 dump 回执及其容器内文件 SHA-256；归属、版本、终态或摘要不符均拒绝收录。文件须为当前用户拥有的 0600 普通文件，父目录为 0700，不能是软链接。只接受完整 V6 dump，且其中任务全部已终结；收录不代表该快照与稍后导出的 PostgreSQL 具有共同事务时间点，迁移的一致性冻结仍由用户负责。

包含搜索 dump 的备份沿用 `make infra-restore-drill`：只读挂载 dump 到带本次所有权标签、无网络、无宿主端口、无持久卷的第三个一次性容器；使用来源镜像导入，核对索引身份、主键、文档、原显式设置与任务回执，再核实容器清理。版本新增的默认设置允许存在；原设置为 unset 的空值不强制覆盖新版本默认。`MEILISEARCH_RESTORE_IMAGE` 仅用于显式验证目标版本，须为 `getmeili/meilisearch:v<完整版本>@sha256:<digest>`；不接受浮动标签，不自动切换开发实例。恢复使用新的一次性 Master Key，不复用开发凭据。该演练不证明 Catalog／Asset 可见性、Manager 当前保护状态或故障恢复 T4 已通过，也不替代旧卷备份。

任务历史按官方 [`/tasks` 游标分页契约](https://www.meilisearch.com/docs/reference/api/async-task-management/list-tasks)读取，每页最多 1000 条，以 `next` 作为下一页的 `from`；UID `0` 不能被当成结束标记。仍逐条核对完整身份、终态、纳秒入队时间与关联标记，并拒绝重复／未知 UID、数量漂移、提前结束或游标不前进。不抽样、不删除历史，也不保留逐 UID 发起 Docker 查询的第二条核验路线。

默认任何文档内容摘要差异都会使恢复失败。用户明确接受搜索文档内容差异时，可仅对本次手动演练传入 `MEILISEARCH_ACCEPT_CONTENT_DRIFT=1`：仍读取全部文档并计算摘要，但内容差异输出带索引、数量、预期／实际摘要的 `WARN`，继续核验全部任务；索引、主键、数量、设置、任务和备份文件校验仍是必需门禁。完成输出明确注明已接受内容差异，不宣称逐文档无损恢复。此选项不进入环境模板、定时云备份或默认 CI，不改变业务权限，不替代正式切卷所需的 Owner 权限／故障验证与旧卷回滚准备，也不删除／改写原索引。未传入 `1` 时保持严格检查。

先在用户确认的导出窗口获得成功 dump，并将文件置于仓库外私有目录，再使用以下入口。尖括号为需要替换的实际值，UID `0` 有效；这些命令本身不会创建 dump：

```bash
make infra-backup BACKUP_ROOT=/绝对路径/ADDP-backups \
  MEILISEARCH_DUMP=/绝对路径/私有导出目录/实际文件.dump MEILISEARCH_DUMP_TASK=实际成功任务UID
make infra-restore-drill BACKUP_DIR=/绝对路径/ADDP-backups/实际备份目录
# 仅在用户明确接受搜索文档内容差异时，对本次手动演练显式选择
make infra-restore-drill BACKUP_DIR=/绝对路径/ADDP-backups/实际备份目录 \
  MEILISEARCH_ACCEPT_CONTENT_DRIFT=1
# 仅在核验目标镜像 digest 后，显式验证跨版本导入
make infra-restore-drill BACKUP_DIR=/绝对路径/ADDP-backups/实际备份目录 \
  MEILISEARCH_RESTORE_IMAGE='getmeili/meilisearch:v1.54.3@sha256:<已核验的64位摘要>'
```

`make test-infra-backup` 是不连接开发服务的 T1 校验逻辑测试，已由 `make test-platform` 和 Platform CI 覆盖；真实恢复必须另行执行 `make infra-restore-drill`，不能用 T1 通过代替导入成功。演练失败后会尝试清理全部本轮容器；任何清理失败均报告失败，不输出零残留成功。

#### 旧 Meilisearch 卷迁移与回滚

已有低版本搜索卷采用唯一流程：旧实例导出、独立新卷导入验证、正式切换；原卷保留作回滚，不同时提供新旧两套服务。dump 创建、停服、切卷及索引重建分别需要明确操作范围和窗口，普通 `up.sh` 不承担迁移。ADDP 应用重启由用户在自己的终端操作。

| 阶段 | 必须完成的检查 |
| --- | --- |
| 冻结与盘点 | 停止 Manager、Catalog、Asset 等索引生产者，暂停相关保护变化；重新核对端点、镜像、卷归属、各索引主键与设置、文档身份、Manager 持久出口及投递。完整分页证明外部任务已终结，不能删历史或取消任务绕过。 |
| 备份 | 核验旧实例 dump 任务成功，按上述标准入口收录产物与摘要；停止旧搜索实例后保全一致性卷副本和可启动的旧镜像，在隔离环境验证同版本恢复。新版本不得挂载原卷。 |
| 独立导入 | 核验目标镜像的完整版本、架构和 digest，在独立空卷导入，备份只读挂载，不接入开发 Owner 自动投递。核对全部索引、主键、设置、文档及任务历史；内容差异仅按上述显式选项处理，不能称为无损恢复。 |
| 分层验收 | 逐项核对原任务 UID、完整入队时间、种类、索引、状态及关联标记；不为旧任务补造标记。三个 Owner 分别核验租户、目录可见性、资产上架及当前内容保护；健康检查或搜索命中不能替代这些负例。 |
| 切换与恢复 | 在确认窗口只切换 Meilisearch 服务，先保持生产者冻结核对新端点和卷，再由 Owner 正常恢复清理及投递。Manager 核清历史和保护清理后才开放搜索；任何不确定结果继续隔离，不手改记录解封。 |
| 保留与收口 | 保留旧镜像、旧卷、dump、摘要和切换后回执，明确备份保留期；只清理本次拥有的演练资源。真实响应丢失、进程退出和同一原任务恢复须另由标准故障验收证明，导入成功不替代 T4。 |

应用恢复会结束生产者冻结。已有 dump 只代表其导出时点；恢复后若产生新任务，正式切换前须重新冻结并更新快照，不能把旧 dump 当作最新完整历史。

切换前仍处于冻结窗口时失败，可以恢复旧镜像及未经新版本改写的旧卷；新源码不退回无标记发送，Manager 搜索继续隔离。切换后已有新投递时，旧卷缺少这些更新，直接回滚不能称为无损：先冻结、保存新卷及新回执，保持出口隔离，再依据当前专业事实和保护规则恢复投影；重建须另行确认范围，不盲目重发未决请求。回滚搜索不得回退当前 IAM 或删除投递证据，旧索引可启动也不代表内容符合当前保护状态。

本机网盘备份使用 `make infra-cloud-backup BACKUP_ROOT=/Users/pampa/addp-backups EXPORT_ROOT=/Users/pampa/addp-cloud-encrypted PRIVATE_ROOT=/Users/pampa/.config/addp/backup`。入口先只读生成上述 Infra 快照，在无宿主机端口和持久卷的隔离容器中恢复核对，再用 age 收件人公钥生成 `.tar.age`，解密流并逐文件核对 SHA-256，最后才原子移入网盘客户端监视的本地目录；脚本本身不将明文快照或私钥放入该目录。`.sha256` 是加密文件的校验值，`last-run.json` 仅记在私有目录。失败时保留本地快照供检查，不上传不完整归档，也不自动删除已有备份。

当前这台 Mac 的用户级 `launchd` 任务 `/Users/pampa/Library/LaunchAgents/com.addp.infra-backup.plist` 每天本地时间 02:00 执行上述 Make 入口，日志在 `/Users/pampa/.config/addp/backup/`。百度网盘“文件夹自动备份”将本地 `/Users/pampa/addp-cloud-encrypted` 自动上传至云端 `/addp备份/addp-cloud-encrypted/`。任务需要该用户已登录、Docker/Infra 正在运行且网盘客户端可上传；电脑关机时不会补跑。原始 age 私钥保存在 `/Users/pampa/.config/addp/backup/identity.txt`（0600）；应用户要求，无独立恢复密码的副本已放在同步目录的 `addp-recovery-identity.txt`，并在网盘目录中确认可见。持有该网盘目录访问权的人因此也能解密其中的归档；如需恢复网盘侧的保密性，应删除云端及同步目录中的私钥副本，改由独立于网盘的安全介质保管。网盘客户端当前提示每月文件夹备份额度 10 GB；超额、离线或登录过期时，本地成功不代表云端成功，需核对传输记录和云端文件。此流程仍仅覆盖上一段列出的 Infra 数据。

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
bash scripts/infra/down.sh --volumes
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

当前源码固定 Meilisearch `1.54.3`、Go SDK `0.36.3`。Manager 新文档投递必须使用 `customMetadata` 并关闭自动重试；旧服务仍可运行其他模块，但 Manager 搜索保持隔离，不退回无标记写入。

已有 `1.7.6` 持久卷**不得只改镜像后重启**。[官方迁移指南](https://www.meilisearch.com/docs/resources/migration/updating) 明确 `--upgrade-db` 不支持低于 `1.12` 的数据库，因此需先确认 dump 迁移及恢复步骤：停止各 Owner 写入，保留旧版本可启动的卷备份与任务历史，核查 Manager 持久投递；备份验证成功后才在独立新卷导入并验证三个 Owner。任务身份／历史必须逐项验证，不能假定 dump 保留原回执或把编号复用当作成功。尚无编号的旧提交若无法证明收敛，保持隔离；受控重建另行确认。普通启动入口不得代替显式授权的迁移操作。

当前 Compose 固定 `1.54.3` 的多架构 registry digest，唯一挂载命名卷 `meilisearch_data_v1543`（默认物理名称 `addp-infra_meilisearch_data_v1543`）。旧物理卷 `addp-infra_meilisearch_data` 不再被当前 Compose 引用；它和原镜像保留作回滚，不删除、不改名，也不由新版本打开。正式迁移只操作 Meilisearch 服务，不启动或重启 ADDP 应用。尚待完成的 Owner 与故障验收见[企业资源目录能力专题](../../docs/next/ADDP企业资源目录能力专题.md)。

普通 `infra/up.sh` 在构建／拉取／启动容器之前，必须核对当前工作区已有 Meilisearch 容器与 Compose 所选镜像的实际 Image ID；不同、归属不明或身份无法核实均拒绝自动替换，不能把 `docker compose up -d` 的幂等性当作数据库可升级证明。该检查保护仍保留原容器的环境，不证明脱离容器的历史卷可以直接使用；删除旧容器不能代替迁移。应用 `restart.sh` 可能间接调用该入口，因此目标隔离导入成功后仍须先完成正式新卷导入、验收与切换，不能直接全量重启完成升级。

应用恢复与搜索迁移分开：当前核心 Infra 健康时，开发启动入口直接复用现有容器，不调用 Infra 构建或 Compose 更新，也不应用搜索镜像变化；核心 Infra 未就绪时才调用 `up.sh`，若上述替换被拒绝但核心已健康，仍可继续启动应用。旧搜索容器／卷不动；Manager 搜索继续隔离，不退回无标记投递。此行为只允许恢复其他功能，不表示新版本搜索已可用，也不替代正式迁移、当前保护核验或完整应用启动验收。

新建无历史数据的部署直接使用固定版本；Owner 初始化和恢复单测通过，不等于现有开发卷已迁移，也不等于真实故障 T4 通过。

具体执行阶段、冻结条件、任务身份对账与回滚边界见[旧 Meilisearch 卷迁移与回滚](#旧-meilisearch-卷迁移与回滚)。`make infra-backup` **默认不包含 Meilisearch**；仅显式传入既有成功 dump 和任务 UID 时收录，不能以 PostgreSQL／MinIO 备份成功代替搜索恢复验证。实际 dump、停服、迁移导入及卷切换须另行确认操作窗口；普通 `up.sh` 不是旧索引卷迁移入口。

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

## 模块实例运行日志

集中日志由根模板 `ADDP_OBSERVABILITY_LOGS_ENABLED=true` 选择，缺省保持启用。设置 `false` 后运行标准 `bash scripts/infra/up.sh` 会停止属于当前工作区的远端日志组件并保留卷，独立本地输出清理器继续运行；不能直接用全量 `docker compose up` 替代生命周期入口。`observability-logs` Profile 只组织可选设施，不作为业务依赖。日志未启用、配置无效或启动失败均不改变核心 Infra Ready；所选日志失败会使整体命令返回非零，调用者按核心服务实际状态决定继续。`status.sh` 对关闭/配置无效分别报告 Disabled/Unconfigured，关闭时不运行远端探针。System 与 Monitor 的部署选择随其下一次标准启动生效。

运行日志由共享 `runtime-log` 启动工具捕获，使用“节点分段文件 → Alloy → Loki → Infra MinIO”单一路径。`bash scripts/infra/up.sh` 管理日志设施，开发环境首次启动仅生成缺失的独立读写令牌和 S3 密钥；生产初始化仅为所选日志能力生成凭据，已有环境不自动补凭据；日志独立预检由 `infra/up.sh` 在核心服务就绪后执行。读、写令牌不得复用，应用只输出日志，不携带 Loki 写入凭据。

Online 启动只使用仓库外的 `ADDP_ONLINE_ENV_FILE`；Hosted 在不归档的秘密目录内初始化同一组凭据，退出时删除，禁止生成根 `.env`。标准宿主启动在 Compose 前创建日志父目录，并将观察器和清理器的 `ADDP_RUNTIME_LOG_OWNER` 设为调用用户的 UID/GID；root 容器中的独立接收器使用受控源目录所有者身份写入。源目录和文件继续保持 `0700`／`0600`，不通过放宽权限或递归 chown 修复历史目录；个人数据卷删除保护保持不变。

- 源目录 `ADDP_RUNTIME_LOG_ROOT` 默认 `./logs/runtime`，按模块和进程实例保存；相对 bind 路径必须以 `./` 开头。
- Loki 使用独立 bucket `addp-runtime-logs` 和最小权限账号；初始化容器同时为 UID 10001 准备 `loki_data` 卷权限。
- `loki_data` 保存 WAL、索引缓存和 Compactor 状态；`alloy_data` 保存读取位置；MinIO 数据卷保存集中日志。普通应用停止、重启和 `infra/down.sh` 不删除这些卷。
- 只在回环地址发布查询代理和 Alloy 健康端口；实际端口由 `ports.sh` 分配并由 `status.sh` 读取，Loki 本体没有宿主机端口。
- `bash scripts/infra/status.sh` 分别报告组件健康，并运行一次从源文件到查询结果的独立探针。成功只证明当前节点这一条测试消息送达，不能证明历史日志完整。
- 接收器的 `status.json` 提供接收、写入、解析失败、截断、写失败及丢弃计数；独立清理器的 `housekeeping-status.json` 提供清理结果；接收器与清理器另记录源文件提前清理数量，不把该风险直接当作 Loki 已丢失正文。Alloy 的 `/-/ready` 和 `/metrics` 提供采集器健康及发送重试、丢弃指标。进程输出普通文本标为 `unknown`，stderr 不自动标为 ERROR。

节点源默认保留最长 48 小时，段/实例/节点数据额度默认 50 MiB/1 GiB/10 GiB。清理器每分钟检查关闭的分段，活动分段由锁保护；额度不足时丢弃新输出并计数。Loki 保留默认 7 天，Compactor 异步删除，不给 bucket 配置粗粒度生命周期过期。源文件、positions、探针结果都不是远端完整持久化回执。

本地源码输出和 Docker 输出共用同一受控源目录。首版只部署一个应用节点；跨节点认证与 TLS 接入另行验收。日志属于可丢失的运维资料；没有自动备份和零丢失归档承诺。若需要灾备，必须协调备份 MinIO 日志 bucket 和 Loki WAL/Compactor 状态，不能只复制活动段文件或 Alloy positions。

集成验证入口：`make test-system-runtime-log`。该入口创建自己的 disposable MinIO/Alloy/Loki，使用随机回环端口、临时凭据和源目录；退出时销毁自建容器、网络、数据卷并检查零残留，不接管开发 Infra。

平台日志链路由独立 `runtime-log-observer` 每 30 秒观测并使用最小 Platform 服务凭据上报 Monitor；它不登记成业务模块。节点绑定、控制面地址、Secret 与通知配置见 [配置规范](../../docs/spec/addp配置介绍.md#平台日志链路观测与通知)。Monitor 持久化告警及通知，System 模块管理“日志链路”展示结果；Loki 不承担告警事实存储。`runtime-log observe --once` 运行单次真实采样并上报，失败返回非零，不是完整日志归档证明。常驻观察进程复用一个探针接收器，每轮使用独立消息标记验证投递，日志序号连续；技术探针段按最后写入时间保留一小时并按小时轮转，清理仅处理已关闭段及过期空来源，业务源仍按配置保留。这样避免每 30 秒创建一个目录撑满来源扫描和采集器指标预算。T2 门禁同时验证实际观测器 OAuth 上报、日志接口中断及恢复，并清理其自建 HTTP 夹具。


## 可选指标中心

根模板 `ADDP_OBSERVABILITY_METRICS_ENABLED=false` 默认关闭 Prometheus。设置 `true` 时，标准 `bash scripts/infra/up.sh` 在核心 Infra Ready 后独立预检、校验配置并启动 `observability-metrics` Profile。中心失败返回非零，开发/生产入口依据核心实际健康继续启动业务；日志与指标独立选择。关闭后再运行标准入口会停止当前工作区的中心并保留时序卷，不停止其他工作区容器。不要用全量 Compose 启动替代生命周期入口。

启用前将 `ADDP_METRICS_TLS_DIR` 设置为部署方管理的绝对证书目录，内含 `ca.crt`、`server.crt`、`server.key`、`health.crt`、`health.key`。此目录仅存放这五项输入，CA 私钥和其他客户端私钥由各自部署 Owner 在目录外保管。CA 只信任本中心部署客户端；`server.crt` 包含 `localhost` 和 `prometheus` DNS SAN，`health.crt` 具有客户端认证用途。目录和文件须可被容器 UID/GID `65534:65534` 读取，私钥不得通过仓库或 Artifact 分发；标准脚本不生成部署 CA 或证书。错误、过期、不匹配或容器不可读证书由真实 mTLS 启动/探针拒绝。

中心的节点作业通过原生 OAuth2 和 HTTP SD 连接已有 System/Monitor API。部署另外设置 `PROMETHEUS_SYSTEM_URL` 和 `PROMETHEUS_MONITOR_URL`，值为容器可达的规范 HTTPS origin，必须有显式端口，不含路径或尾部斜杠；由受控 TLS 入口分别转发固定 `/api/v1/system/oauth/token` 和 `/api/v1/monitor/platform/metrics_discovery`。不按本机 Backend 首选端口或模块注册 URL 拼接地址，不将发现凭据用于节点指标抓取。

`ADDP_METRICS_DEPLOYMENT_DIR` 指向仓库外的绝对目录，只存放 `control-ca.crt`、`source-ca.crt`、`collector.crt`、`collector.key`、`prometheus-client-secret` 和生成的 `prometheus.yml`。控制面 CA 用于验证 Token/发现服务，来源 CA 用于验证节点 IP SAN；独立 collector 证书只访问来源，不能复用中心健康证书。Secret 文件内容不带尾部换行，须精确匹配 System 部署中的 `PROMETHEUS_SERVICE_CLIENT_SECRET`，不得与日志或业务服务 Secret 相同。部署负责把目录设为容器可遍历、私钥和 Secret 仅向需要的 UID/GID 开放（如目录 `0750`、Secret `0640`，由部署正确设置所有者/组）；生命周期脚本不修改现有私钥权限。

预检从唯一版本化模板和部署输入原子生成 `prometheus.yml`，该文件不含 Secret 值，不得手工编辑。更换控制面地址、信任根或证书后由部署方重新创建中心容器应用，不承诺热更新；目标启停通过成功的 30 秒发现刷新生效，不要求重启业务。发现失败时中心保留上次成功目标列表，紧急阻断依赖凭据撤销和网络边界，不能承诺控制面故障期间即时停采。未填写完整接线输入时只报告可选指标未配置，核心启动仍继续。

中心 API/UI 强制 mTLS，宿主只发布 `127.0.0.1:${PROMETHEUS_PORT:-19090}`。本地冲突沿用端口解析和现有映射，实际值用 `bash scripts/infra/status.sh` 查看；容器间使用 `https://prometheus:9090`。管理员写入、远端写入和 HTTP 重载不开启。独立卷 `prometheus_data` 保留时序块 7 天或 10 GiB，体积淘汰可能缩短历史窗口；WAL 与压缩空间需要额外磁盘容量。镜像版本及 digest、资源预算和固定采集限制统一定义在 `metrics.yml` 及 `prometheus*.yml`。

中心自身作业与节点 HTTP SD 作业已分别接线，Ready 不代表节点或业务引擎已接入。正式目标只由 Monitor HTTP SD 与 System 当前对象绑定提供，不支持手工生产静态目标文件。`make test-monitor-metrics` 使用独占临时项目、临时证书及固定镜像，验证原生 OAuth/HTTP SD、真实受控采样和故障恢复，退出清理容器、网络、卷及文件；控制面为协议夹具，不证明真实部署全链路、生产规模或完整 7 天保留窗口。

生产平台通过根 Compose 向 System 传递可选指标开关及独立发现 Secret，向 Monitor 传递指标开关与明确 CIDR/端口集合。`scripts/prod/start.sh` 只在选中指标且三个 `MONITOR_METRICS_*_FILE` 是可读绝对文件时合并 `scripts/prod/metrics-platform.yml`，仅向 Monitor 挂载来源准入 CA/客户端证书/私钥，不重复定义服务。未选中或证书缺失不挂载不存在的文件、不创建空证书目录；缺失时报告可选能力未配置并继续业务启动。Monitor 准入证书可独立于 Prometheus collector 管理，两者均不使用中心健康证书。


### 独立 Linux 主机指标来源

中心和业务启动不调用节点采集入口。部署方在明确纳管的 Linux 节点导出根 `.env.example` 所定义的三个输入后，主动执行：

```bash
python3 scripts/infra/node-metrics.py up
python3 scripts/infra/node-metrics.py status
python3 scripts/infra/node-metrics.py down
```

入口只消费当前环境，不自动读取或改写 `.env`。`ADDP_NODE_METRICS_ENABLED=false` 时 `up` 不调用 Docker；选择 `true` 后要求 `ADDP_NODE_METRICS_LISTEN` 为明确本机 IP:端口（IPv6 使用 `[IP]:端口`），不能通配监听、使用 DNS 或自动避让。`ADDP_NODE_METRICS_TLS_DIR` 为仓库外绝对目录，只含 `ca.crt/server.crt/server.key`，不能混入签发私钥或客户端私钥；文件须可由 UID/GID 65534 读取。来源 CA 仅信任独立 Monitor 准入与 Prometheus 采集客户端，不信任中心健康身份；服务器证书包含 Monitor 发现使用的 IP SAN。文件存在不代表证书已通过验证。

同一入口根据本地 Engine 事实选择部署层，不新增 `.env` 开关。原生 Linux 要求内核 5.12+、Docker Engine 与当前宿主内核一致；采用 host 网络/PID 和宿主根目录只读 rslave 挂载，显式从 `/host/proc`、`/host/sys` 读取，启用十个资源采集器。macOS 的 Docker Desktop Linux VM 采用 `node-metrics-desktop.yml`：独立 bridge 网络，按显式 IP:端口发布来源；只启用 cpu、meminfo、loadavg、diskstats、stat、uname、time 七个内核全局采集器，使用 `/proc`、`/sys`，不挂载宿主根、Docker Socket，不用 host 网络/PID。不发布 filesystem、netdev、netstat；文件系统容量/inode API 对该来源明确无数据。diskstats 读取 Linux VM 内核块设备统计，提供五项磁盘 IO 观测，不代表 Mac 物理磁盘或单个容器的消耗，也不跨设备层求总和。仍拒绝远程 Docker Endpoint、非 Linux Engine、旧内核和未知环境。两层共用同一固定官方镜像 tag/digest、原生 mTLS、0.25 CPU/256 MiB、非 root、只读容器文件系统与全部 capability 移除。节点源能读取宿主资源属于明确部署权限，不由中心开关授予。证书替换和配置变更通过再次执行节点 `up` 重建生效；密钥文件 inode 替换不会自动更新既有文件 bind mount，不能承诺热轮换。生产宿主子挂载的实际只读状态与传播仍需 Linux 验收；配置声明和内核门槛不能替代运行检查。

Docker Desktop 主机登记必须使用 `virtual` 类型并明确命名为 Linux VM；指标反映 VM 中所有工作负载的合计，不能当作 Mac 本机或某个引擎消耗。在 Mac 直接运行的模块不归该节点。`ADDP_NODE_METRICS_LISTEN` 在 Desktop 模式表示宿主发布 IP:端口，来源内部固定监听 9100；正式监测目标必须同时对 Monitor 和 Prometheus 可达，不能把 Mac 回环地址直接当作中心容器的回环地址。服务器 IP SAN 对应正式目标准入解析出的地址，端口不自动避让。

macOS 本地验证需要避免 DHCP 地址变化时，可显式将 `ADDP_NODE_METRICS_LISTEN=127.0.0.1:19091`、`MONITOR_METRICS_ALLOWED_CIDRS=127.0.0.1/32` 与 `ADDP_METRICS_DESKTOP_LOOPBACK_PORTS=19091` 配为同一端口。正式目标使用 `https://127.0.0.1:19091/metrics`；端口列表最多 64 个规范非零端口，逗号分隔，不允许重复、空项或空格。中心唯一 Job 将列表内每个显式回环地址转换为同端口 `host.docker.internal`（此例为 `host.docker.internal:19091`），同时保留原地址作为 `instance`，确保宿主准入与中心查询沿同一作用域。Go 进程来源可以追加自己的显式端口；不转换列表外的端口或非回环地址。仅 macOS、本地 unix Docker Desktop Engine 接受该输入，缺省不启用转换；来源证书必须同时含 `127.0.0.1` IP SAN 与 `host.docker.internal` DNS SAN，仍使用独立 mTLS，不设置证书名称覆盖。此地址表示采集访问入口，不能作为 VM 物理网卡地址或身份依据。端口冲突必须处理原占用，不自动避让。配置与证书准备完成后，由部署方分别执行节点 `up`、Monitor 标准重启及中心 `up.sh --metrics`，再更新已有目标；保留原节点和目标身份，不建立重复记录。

固定项目 `addp-node-metrics` 使用仓库绝对路径 Owner 标签，异工作区同名容器存在时拒绝操作。`down/status` 不校验已失效的来源证书或当前开关，不依赖中心，不删除业务容器、网络或卷。启动成功只说明来源容器运行；部署方仍需通过独立 mTLS 验证来源，再用 Monitor 正式目标接口完成准入，来源部署不会自动创建 System 节点、目标或发现标签。端口与来源错误只影响这个独立入口。

`make test-monitor-metrics` 还以相同基础/Web 模板运行真实 node_exporter，验证客户端认证、资源样本进入唯一原生 HTTP SD 作业、来源中断与恢复。独占 T2 使用 bridge、随机回环端口和临时证书，不挂载宿主根、不使用 host 网络/PID。同一临时来源再部署为 Desktop 受限层，实际对照 Engine 核数、内存和内核，检查仅七采集器、不同命名空间容器的原始磁盘计数对照、被禁用指标族缺失、mTLS、唯一 HTTP SD 磁盘即时/趋势查询及恢复。macOS Docker Desktop 上的执行证明 VM 内核全局基础资源，Linux CI 只证明受限部署层；完整文件系统、cAdvisor、真实 System/Monitor 控制面和生产权限仍按各自验收范围验证，不以 T2 协议夹具替代。


### Monitor 资源查询身份

指标中心的 query/query_range 由 Monitor 使用独立 mTLS 客户端访问。显式部署 `MONITOR_PROMETHEUS_URL`（HTTPS origin、端口，无路径）以及 `MONITOR_PROMETHEUS_CA_FILE/CLIENT_CERT_FILE/CLIENT_KEY_FILE`（绝对文件路径）。CA 信任中心服务器证书；中心的客户端 CA 需信任该独立查询证书。不要复用 health、collector 或节点准入私钥。Prometheus 原生 mTLS 验证客户端身份，不提供按证书区分 HTTP 路径的 RBAC；查询客户端只调用固定读取端点，部署网络只允许 Monitor 访问中心，中心不得开启 admin/lifecycle API。

原生开发由标准环境加载传给 Monitor；生产通过唯一 `scripts/prod/start.sh` 在输入完整时添加 `metrics-query.yml` 的三项只读挂载，不增加中心启动依赖。未填写查询 origin 时不挂载，资源 API 报能力未配置；填写 origin 但证书缺失时生产脚本报告可选观测失败，业务仍继续启动。仅恢复同一中心的连接不重启业务；修改端点或证书须按服务生命周期由运维生效。

首批读取 8 个节点基础量的即时值及趋势，预算位于模块级 `/settings/resource-query-policy`；查询参数、权限和验收边界见平台运行监控设计 10.21。CPU 忙碌率、磁盘和网络速率尚未发布。


### 指标控制面的可选私有 TLS 转发

本地独立 TLS 入口由同一标准 `bash scripts/infra/up.sh --metrics` 管理，仅调和指标服务，使用既有 `addp-network`；不会启动、构建、初始化或重建核心和日志设施，也不生成日志凭据。默认不带参数仍为全量标准入口。再次执行指标入口会重新创建所选指标容器以应用生成配置和证书；保留中心时序卷，不能视为热更新。设置 `ADDP_OBSERVABILITY_METRICS_ENABLED=true`、`ADDP_METRICS_CONTROL_ENABLED=true`、外部 `ADDP_METRICS_CONTROL_DIR` 和明确 `ADDP_METRICS_CONTROL_GATEWAY_URL=http://host.docker.internal:实际Gateway端口`；两个发现 origin 均使用 `https://metrics-control:9444`。入口只在 `addp-network` 提供 TLS，不发布宿主端口；其 CA 放入中心 deployment/control-ca.crt。目录仅含 `server.crt/server.key/nginx.conf`，标准脚本不签发证书。版本化模板仅转发 Token POST 和发现 GET，拒绝其他路径、方法及查询参数；Authorization 保留，访问日志关闭，资源为 0.25 CPU/128 MiB，UID/GID 65534、只读文件系统、移除全部 capability。上游 HTTP 属于明确的受控本地网络边界，不能替代生产 TLS 验收。

关闭入口时不校验入口输入，停止当前 Owner 的入口容器；关闭中心也停止入口并保留中心数据卷。状态 Running 只证明容器运行，Token/发现接通须经真实请求验证；业务仍不依赖该入口。现有外部 HTTPS 控制面部署不需要选择此转发容器。

个人 macOS Docker Desktop 的新建凭据可以采用完整路径隔离：Owner 私有 0700 外层目录，容器仅只读挂载各自 0755 子目录，所需新建叶子文件设为容器 UID 65534 可读，其他本机用户无法穿过外层目录。CA 私钥与原生 Monitor 查询/准入私钥另存 0700 目录及 0600 文件，不挂载到中心或入口；不得递归放宽已有目录与私钥权限。此布局仅用于个人开发验证，生产密钥 Owner/组与权限按独立 T5 验收。来源使用明确且同时可达的 Mac IP，IP 改变后需重新签发 SAN 并通过正式目标准入更新，不能自动沿用旧绑定。
