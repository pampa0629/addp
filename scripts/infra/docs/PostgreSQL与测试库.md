# ADDP Infra：PostgreSQL与测试库

[返回 Infra 指南](../README.md)

## 项目隔离

## PostgreSQL database 清单

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

Meta 的授权资源事实可以通过 `make test-meta-postgres META_POSTGRES_TEST_ARGS='--test authorization-facts --source-root <冻结源码目录>'` 验证同一组 PostgreSQL 用例。可选源码目录必须是本仓库提交的 Git checkout，且具有正确的 Meta Go module；Infra 身份和真实映射仍由调用入口所属工作区核验，不将临时 checkout 伪装成容器 owner，也不设置 CI 环境变量绕过检查。默认不传 `--source-root` 时行为不变。该窄入口只在 `addp_test` 的回滚事务中验证事实，不提供新 database、额外 schema 或跳过失败用例的路线。

文件组成的真实扫描与落库替换可用 `make test-meta-postgres META_POSTGRES_TEST_ARGS='--test file-composition'` 复验，覆盖已登记的对象关联组成、FTP／NFS basic 结构观察及失败清除旧证明。仍使用同一 DSN 核验、互斥锁和回滚清理；默认完整 Meta 门禁及 CI 继续执行这些原用例，窄分组不替代授权事实或完整模块回归。

System IAM 标准门禁在首次重置 Schema 前获取宿主机级文件锁 `/tmp/addp-system-iam-postgres-gate.lock`，覆盖完整运行及其子进程，跨 checkout 和 `--package` 选择互斥。另一轮仍在运行时立即失败，不开始数据库操作；进程退出后由操作系统释放锁，锁文件保持原位，不能通过删除锁文件解除占用。该边界同样用于独占 CI Runner；分包验证也必须串行运行。

Cleanup 执行授权聚焦入口为 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS='--package iam --test cleanup-authorization'`，精确选择 `system/backend/internal/iam` 的 `TestCleanupAuthorizationAgainstPostgres`；默认 IAM 全集仍按 `AgainstPostgres$` 自动包含该用例。聚焦入口继续使用 `ADDP_SYSTEM_POSTGRES_TEST_DSN`、同一互斥锁及标准 Schema 重置，本地只能连接 `addp_iam_test`，不会执行独占容器崩溃恢复分组。缺少主测试 PASS、无测试、跳过或失败均阻断，业务测试未落地时不得把入口存在计为授权验收通过。该入口仅验证 IAM 授权；System Cleanup 父子协调的独立 T2 门禁须在业务测试到位后另行登记。

账号与权限的崩溃恢复回归使用 `make test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS='--test database-crash-recovery'`。该分组自行创建带唯一归属标记的一次性 PostgreSQL 15 容器、回环临时端口和 `addp_iam_test`，不消费外部 DSN，也不连接共享 `addp-postgres`。测试分别向自有数据库后端发送 SIGQUIT、对自有容器发送 SIGKILL 后重新启动；对账已提交的 IAM 全表内容，验证未提交变更回滚、原登录凭据和授权上下文可继续使用、迁移重入保留事实。成功及失败路径均删除自有容器与数据卷并核验零残留。默认完整 IAM 门禁同时运行该分组，既有 System IAM PostgreSQL CI Job 覆盖；这是模块 T2，不代替跨模块 Online 或宿主机断电／磁盘损坏认证。

该门禁对每个 Go 测试包显式设置 20 分钟上限。完整 Migration 回归会反复重放历史版本，累计运行可能超过 Go 默认的 10 分钟包级超时；单个迁移阶段仍保留自身期限，CI Job 仍以 30 分钟限制整轮运行，不通过拆包、跳过场景或取消超时规避完整验收。

Manager 剖析执行授权的 PostgreSQL 夹具从同一数据库的 `clock_timestamp()` 读取认证时间，与正式 Browser 会话的墙钟校验一致，避免主机与 Docker VM 时钟差触发“认证时间在未来”；正式拒绝未来认证时间的规则保持不变。

需要一次验证全部已登记基础设施集成门禁时，先显式配置各 owner 门禁要求的安全连接变量，再运行 `make test-integration`。该入口严格串行调用 PostgreSQL 和 MongoDB 模块级门禁，避免 `addp_test` 或 `addp_iam_test` 被并发重置；PostgreSQL 门禁不会创建新 database，也不会连接 `addp` 开发业务库。Manager MongoDB 门禁读取 `Outdoor/Persons`；目标为空时只创建一条确定性夹具，并在退出时恢复原状态。

Manager 统一派生任务表、语义唯一约束、资源绑定与资源回收生命周期使用 `addp_test` 验证：

```bash
MANAGER_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-manager-postgres
```

GitHub Actions 使用每个 Job 独占、随 Job 销毁的 PostgreSQL 15 Service，不连接本地开发环境 Infra；workflow 可以为 Job 创建专用测试 database，但必须由 workflow 声明并由隔离实例生命周期回收。

Ontology 修订/发布门禁使用 `ONTOLOGY_POSTGRES_TEST_DSN` 连接本地 `addp_test`，标准入口为 `make test-ontology-postgres`，并已纳入 `make test-module MODULE=ontology`、串行 `test-integration` 和辅助 macOS 巡检。门禁仅接管此前不存在的 ontology schema，用随机 Run ID 标记所有权；退出时核对标记并删除本轮执行记录和 schema，验证零残留，不删除或重建 common。CI 对应独占 `addp_ontology_test`，不访问开发库。具体语义范围见 [Ontology 模块说明](../../../ontology/CLAUDE.md)。

不可捕获的中断若遗留测试 schema，先只读核对 `ontology` 的 schema comment 为 `addp.ontology-test:<Run ID>`，再显式运行 `make test-ontology-postgres-cleanup ONTOLOGY_POSTGRES_CLEANUP_RUN_ID=<已核实的 UUID>`，同时提供上述安全测试 DSN。恢复入口复用正常退出的唯一 owner 清理函数，取得同一 PostgreSQL advisory lock 后才检查标记；锁占用、标记不符、无标记或非测试库均拒绝。仅删除指定轮次 schema 及其引用的 Ontology 执行记录，保留 common 和其他 owner 的数据；已无 schema 时不执行删除。恢复命令不运行集成测试，成功后另行运行正常门禁；正常门禁仍拒绝接管任何既有 schema，不自动恢复旧轮次。CLI 拒绝策略由 `make test-ontology-postgres-policy`（平台 T0 聚合）覆盖，真实锁与标记拒绝由既有 Ontology PG T2/CI 覆盖，无新增服务或 CI Job。

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

## PostgreSQL Schema 隔离

```
addp (database)
├── system       → System 模块（用户、租户、日志、资源）
├── manager      → Manager 模块（数据源、目录、快显）
├── meta         → Meta 模块（元数据节点、元数据项、字典）
├── transfer     → Transfer 模块（任务、执行记录、检查点）
├── orchestrator → Orchestrator 模块（编排定义、执行实例）
└── develop      → Develop 模块（SQL 脚本管理）
```

## init-postgresql.sh

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

Common PostgreSQL 门禁同时验证正式表结果的覆盖事务：重复覆盖、失败回滚、超时取消和并发写入。Model PostgreSQL 门禁验证正式表创建幂等、归属检查、结构漂移拒绝，以及物化任务排队、父子执行授权、固定版本和过期租约终态；Quality PostgreSQL 门禁验证物理表断言。均复用既有测试库与标准入口。

`make test-security-postgres` 同时执行 `common/schema` 的真实 PostgreSQL 启动所有权回归：同 schema 并发迁移一次执行、只读 Worker 校验、失败 DDL 和版本回滚及拒绝降级。使用 `addp_test` 内专用测试 schema，并在测试结束清理；不创建额外 database。
