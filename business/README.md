# Business Database

## 概述

业务库为 ADDP 平台提供业务数据存储，与 ADDP 系统库完全独立部署。

**包含服务**：
- **PostgreSQL (PostGIS)**：业务数据库，端口 5433
- **Oracle Free 23ai**：普通表与 Oracle Spatial 测试源，端口 15210，service name `FREEPDB1`；固定使用保留 Spatial/Locator 的常规镜像 `gvenzl/oracle-free:23`，不得使用 `-slim` 镜像。
- **SuperMap SDX+ for PostgreSQL**：独立原生 PostgreSQL 实例，端口 5434；不安装 PostGIS
- **MinIO**：业务对象存储，端口 9002-9003
- **ClickHouse** 🆕：高性能列式存储 OLAP，端口 9000, 8123
- **MongoDB** 🆕：文档型 NoSQL 数据库，端口 27017
- **MySQL 8.0**：支持 Spatial 与 CDC 的业务关系库测试源，端口 3306
- **OceanBase Community Edition 4.4.2 LTS**：Apache 2.0 授权、MySQL 模式的国产分布式关系数据库测试源，端口 2881；固定使用 `oceanbase/oceanbase-ce:4.4.2-lts`。
- **TiDB 8.5.8**：Apache 2.0 的国产分布式 SQL 数据库测试源，端口 4000；固定使用 PingCAP 官方 PD/TiKV/TiDB 三组件镜像及 OCI digest，不提供镜像覆盖入口。
- **openGauss 6.0.6 LTS**：Apache 2.0 授权、PG 兼容模式的国产关系数据库测试源，端口 5435；当前只在具备 NUMA 的 Linux x86_64 主机使用并校验官方 Docker tar，统一加载为 `opengauss:6.0.6`。
- **KingbaseES V9R1C10**：固定 `V009R001C010B0004` 官方 Docker tar 与 SHA-256、使用 owner 正规 License 的 PG 模式国产关系数据库测试源，端口 5436；只通过 owner-managed Linux x86_64 disposable 生命周期启动。
- **达梦 DM8 ARM64 技术夹具**：固定鲲鹏 920/麒麟 10 SP1 官方安装 ZIP 与双层 SHA-256，本机只构建不可推送的 disposable 镜像，端口 5236；不代表 ADDP 已注册 DM8 Engine。
- **Apache Doris**：实时分析数据库，端口 9030, 8030
- **Apache Spark**：分布式计算引擎，主机端口 7077、18088、11000；默认 Worker 为 Thrift 查询和工作流执行分别保留执行资源
- **Redpanda**：兼容 Kafka API 的业务消息流，端口 29092

Business 的 Doris all-in-one 服务是固定单 FE、单 BE 的本地开发拓扑，FE 因此固定使用 `force_olap_table_replication_num=1`。生产 Doris 集群不复用该单节点配置，应按实际 BE 数量和容灾策略设置副本数。

**关键特性**：
- ✅ 独立部署，无依赖
- ✅ CPU 架构自适应（ARM64/AMD64）
- ✅ PostGIS 空间数据支持
- ✅ MySQL 全二维几何族、SRID 与空间索引测试数据
- ✅ Oracle `MDSYS.SDO_GEOMETRY`、完整二维几何族、SRID、空间索引与 EWKB 读取测试数据
- ✅ Oracle ARCHIVELOG、LogMiner 专用 common user、普通表与 Oracle Spatial CDC readiness
- ✅ 幂等启动脚本
- ✅ 模块化启动（按需启动服务）
- ✅ Business Kafka 与 ADDP Infra Kafka 物理隔离

## 快速开始

### 默认启动（PostgreSQL + MinIO）

```bash
# 1. 配置环境变量
cp .env.example .env
# 编辑 .env 修改密码

# 2. 启动服务
bash scripts/start.sh

# 3. 验证服务
docker-compose ps
```

### 按需启动特定服务

```bash
# 启动所有服务
bash scripts/start.sh -all

# 只启动 ClickHouse
bash scripts/start.sh -clickhouse

# 只启动 SuperMap SDX+ for PostgreSQL 专用实例
bash scripts/start.sh -supermap-postgresql

# 只启动 MongoDB
bash scripts/start.sh -mongodb

# 只启动 MySQL，并幂等初始化专用 CDC 用户
bash scripts/start.sh -mysql

# 只启动 OceanBase CE，并幂等初始化可查询样例
bash scripts/start.sh -oceanbase

# 只启动 TiDB 三组件，并幂等初始化可查询样例
bash scripts/start.sh -tidb

# 只启动 openGauss，并幂等初始化可查询样例
bash scripts/start.sh -opengauss

# 启动 License 受控的 disposable KingbaseES 样例；必须独立使用且不进入 start.sh -all
bash scripts/start.sh -kingbase

# 在 ARM64 Docker 中启动 DM8 官方介质技术夹具；必须独立使用且不进入 start.sh -all
bash scripts/start.sh -dameng

# 只启动 Oracle，并幂等初始化普通表与 Spatial 样例
bash scripts/start.sh -oracle

# 只启动业务 Redpanda，并幂等初始化只读 Engine 账号
bash scripts/start.sh -redpanda

# 启动 ClickHouse + MongoDB
bash scripts/start.sh -clickhouse -mongodb

# 启动 PostgreSQL + MinIO + ClickHouse
bash scripts/start.sh -postgres -minio -clickhouse
```

## 目录结构

```
business/
├── docker-compose.yml              # Docker Compose 配置
├── .env / .env.example             # 环境变量
├── README.md                       # 本文档
│
├── scripts/                        # 管理脚本
│   ├── start.sh                    # 启动服务
│   ├── stop.sh                     # 停止服务
│   ├── restart.sh                  # 重启服务
│   ├── kingbase.sh                 # owner-managed KingbaseES disposable 样例
│   ├── dameng.sh                   # DM8 ARM64 官方介质 disposable 技术夹具
│   ├── online-engine-fixture.sh    # T4 专用 PostgreSQL Fixture 生命周期
│   ├── online-workbench-mysql-fixture.sh # Workbench T4 专用只读 MySQL Fixture
│   ├── online-tidb-consumer-fixture.sh # TiDB 消费链路 T4 专用三组件 Fixture
│   ├── online-kingbase-consumer-fixture.sh # KingbaseES 消费链路 T4 专用许可受控 Fixture
│   ├── online-manager-minio-fixture.sh # Manager 血缘与文档快显 T4 专用 MinIO Fixture
│   └── online-security-transfer-fixture.sh # Security/Transfer T4 复合 Fixture
│
├── postgres/                       # PostgreSQL 配置
│   ├── init.sql                    # 数据库初始化脚本
│   └── pg_hba.conf                 # 访问控制配置
│
├── clickhouse/                     # ClickHouse 配置
│   └── init.sh                     # ClickHouse 初始化脚本
│
├── mongodb/                        # MongoDB 配置
│   └── init.sh                     # MongoDB 初始化脚本
│
├── mysql/                          # MySQL 配置与测试数据
│   ├── init-cdc.sh                 # 专用 CDC 用户幂等初始化
│   └── test-data.sh                # 普通表与全二维几何族显式测试数据
├── oceanbase/                      # OceanBase CE 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── tidb/                           # TiDB 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── opengauss/                      # openGauss 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── kingbase/                       # KingbaseES 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── dameng/                         # DM8 ARM64 官方介质技术夹具
│   ├── Containerfile               # 固定 ARM64 基础镜像与静默安装
│   ├── install.xml                 # 静默安装与初始化参数
│   ├── startup.sh                  # 前台启动 dmserver
│   └── init.sql                    # 幂等探针与普通关系样例
├── oracle/                         # Oracle 普通表与 Spatial 测试数据
│   ├── init.sql                     # 幂等初始化 SQL
│   ├── init-cdc.sh                  # ARCHIVELOG、LogMiner 账号与权限幂等初始化
│   └── test-data.sh                 # 容器内执行初始化并验证
│
├── doris/                          # Apache Doris 配置
│   └── init.sh                     # Doris 集群初始化
│
├── spark/                          # Apache Spark 配置
│   ├── spark-defaults.conf         # Spark 默认配置
│   └── init-test-data.sh           # Spark 测试数据初始化
│
└── minio/                          # MinIO 配置（预留）
```

## 架构说明

### 与 ADDP 系统库的区别

| 组件 | ADDP 系统库 | Business 业务库 |
|------|------------|----------------|
| PostgreSQL | 端口 5432 | 端口 5433 |
| SuperMap SDX+ for PostgreSQL | - | 独立实例，端口 5434 |
| MinIO | 端口 9000-9001 | 端口 9002-9003 |
| ClickHouse | - | 端口 9000, 8123 |
| MongoDB | - | 端口 27017 |
| TiDB | - | 端口 4000 |
| openGauss | - | 端口 5435 |
| KingbaseES | - | 端口 5436；仅 owner-managed disposable 样例 |
| 达梦 DM8 | - | 端口 5236；仅 ARM64 官方介质技术夹具 |
| 用途 | ADDP 元数据（用户、资源配置、任务定义） | 用户业务数据（上传的数据、文件） |
| 示例数据 | 用户账号、资源配置表 | Shapefile 空间数据表、用户上传文件 |

### CPU 架构自适应

启动脚本自动检测架构并选择最优镜像：

| CPU 架构 | PostgreSQL 镜像 | 性能 |
|---------|----------------|------|
| **ARM64** (Apple Silicon) | `imresamu/postgis-arm64:15-3.4` | ⚡ 原生性能 |
| **AMD64** (Intel/AMD) | `postgis/postgis:15-3.4` | ⚡ 原生性能 |

openGauss 不使用 Docker Hub 第三方镜像。`scripts/lib/opengauss-official-media.sh` 固定维护 6.0.6 官方 x86_64/aarch64 tar URL 与 SHA-256；当前 Business 和 T2 只使用已通过发布认证的 x86_64 介质。6.0.6 官方 Docker 构建包含 MOT，MOT 需要有效 NUMA 拓扑；Docker Desktop for macOS 不提供该拓扑，官方 ARM64 与 x86_64 镜像都会在启动期失败。因此 macOS 本地巡检不启动 openGauss，Provider 实库门禁由 GitHub Actions 的 Linux x86_64 Job 承担，不能用跳过或第三方镜像伪装本地通过。

KingbaseES 不使用第三方镜像，也不把官方介质或 License 写入仓库、Compose、日志或 Artifact。`scripts/lib/kingbase-official-media.sh` 固定 V9R1C10 `V009R001C010B0004` 官方 x86_64 与 ARM64 tar、官方 MD5 和 ADDP 校验的 SHA-256，并按宿主 CPU 选择后统一加载为固定中性本地镜像；`business/docker-compose.yml` 只声明一个独立 `kingbase` profile 和无卷容器结构。`business/scripts/kingbase.sh` 要求仓库外 `ADDP_KINGBASE_LICENSE_FILE` 及其 `ADDP_KINGBASE_LICENSE_SHA256`，按 `docker compose create`、License 注入、`docker compose start` 的唯一顺序启动，容器因此显示在 Docker Desktop 的 `business` Compose 分组中。macOS ARM64 可使用官方 ARM64 介质做本地功能评估，但内置 90 天试用 License 不作为可重复门禁路线；正式 T5/T2/T4 仍只在 owner-managed Linux x86_64 Runner 使用正规 License，GitHub Hosted 与 macOS Docker Desktop 不承担正式验证。

达梦 DM8 只使用 `dm8_20260708_HWarm920_kylin10_sp1_64.zip`，外层 ZIP SHA-256 为 `d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824`，内层 ISO SHA-256 为 `2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e`。`bash scripts/start.sh -dameng` 仅接受 Docker Server `linux/arm64`，在本机从官方介质构建 `addp/dameng:dm8-20260708-arm64` 并启动无卷 `dameng` profile；不会上传、归档或重分发介质与镜像。该介质面向鲲鹏 920/麒麟 10 SP1，本机 Ubuntu ARM64 容器只属于非认证 ABI 技术验证；内置试用授权固定于 2027-07-07 到期，不能作为长期门禁或通过重建重置。官方 ODBC 和 Go 连接验证在 Linux ARM64 容器内执行，macOS 宿主 ADDP 进程不加载 Linux `.so`，所以当前不注册 `engine_type=dameng`，也不进入 Provider、T2 或 T4。

## 脚本说明

### scripts/start.sh - 启动服务

```bash
bash scripts/start.sh
```

**功能**：检查配置、检测架构、启动服务、验证健康状态、安装 PostGIS
**特性**：幂等执行（可重复运行）

`-supermap-postgresql` 启动的 `business-supermap-postgresql` 使用独立 volume 和原生 PostgreSQL 15 镜像。启动脚本会拒绝已安装 PostGIS 的实例；SuperMap `sm*` 系统表只能在 System 中通过 `SuperMap SDX+ for PostgreSQL` 高危启用入口，由 `supermap_workflow` 的 SDK 算子创建。

启用 MySQL 时，脚本还会在数据库 ready 后执行 `mysql/init-cdc.sh`。该脚本每次都创建或更新 `${MYSQL_CDC_USER:-addp_cdc}@%`，并将权限收敛为 Debezium 所需的最小权限集，因此已有数据卷也会生效。连接 MySQL CDC Engine 时使用 `.env` 中的 `MYSQL_CDC_USER` 和 `MYSQL_CDC_PASSWORD`，不要使用 root。

`bash scripts/start.sh -oceanbase` 启动官方 OceanBase CE 固定 LTS 镜像，并通过纯 SQL 脚本幂等初始化 `${OCEANBASE_DATABASE:-business}`。样例包含连接探针表，以及与 MySQL 普通业务样例对齐的 `customers`、`products`、`orders`、`order_items` 四张关联表，覆盖主外键、唯一约束、索引、Decimal、JSON、布尔和微秒时间字段；当前支持非空间 InnoDB 基表的 bounded watermark 一致性读取、安全建表、可空列增量演进、事务性分批 insert、覆盖策略所需的精确目标表删除，以及按显式非空稳定键执行的事务性幂等 upsert。该容器不初始化或声明空间、CDC 或 Oracle 模式能力，是 `MODE=mini` 的单机测试形态，建议预留至少 2 CPU / 8 GB 内存，不用于生产部署。System 中必须注册为 `engine_type=oceanbase`，容器内连接地址为 `business-oceanbase:2881`，默认账号为 `root@test`；不要登记为 MySQL。

OceanBase Provider 的唯一 T2 入口是仓库根 `make test-common-oceanbase`。对当前 Business 容器验证时，显式传入 `ADDP_TEST_OCEANBASE_HOST`、`ADDP_TEST_OCEANBASE_PORT`、`ADDP_TEST_OCEANBASE_USER` 和 `ADDP_TEST_OCEANBASE_PASSWORD`；门禁固定使用名称含 `disposable` 的专用 database，覆盖连接、实时目录、字段与统计 Facts、BatchRead、可执行查询样例、命名参数、受控只读事务，以及非空间普通表的 bounded watermark resume 和 prepare/session/delete/upsert 写入契约。测试生命周期创建并删除该 database，各用例只清理自己拥有的 gate 表，不读取或修改上述固定业务样例。

`bash scripts/start.sh -tidb` 启动 PingCAP 官方 TiDB 8.5.8 三组件，并幂等初始化 `${TIDB_DATABASE:-business}`。样例包含连接探针及 `customers`、`orders` 普通关系表；System 中必须注册为 `engine_type=tidb`，宿主机连接 `localhost:${TIDB_PORT:-4000}`，容器网络连接 `business-tidb:4000`，默认账号 `root`、空密码。镜像在 Compose 中直接固定版本和 digest，不接受环境变量覆盖。首版只开放经真实 T2 验证的非空间目录、查询、bounded watermark 和普通表写入能力，不声明 MySQL replication、TiCDC、空间或分区变化应用。

TiDB Provider 的唯一 T2 入口是仓库根 `make test-common-tidb`。门禁拥有独立的无卷三组件 Compose project，创建并删除名称含 `disposable` 的 database，覆盖真实目录/Facts、参数查询、BatchRead、Snapshot Isolation 下的 bounded watermark resume，以及 prepare/session/delete/upsert；成功、失败和中断均执行 `down --volumes --remove-orphans` 并验证容器零残留。同一 owner 脚本可在 Linux x86_64 GitHub Hosted 和 macOS Docker Desktop 执行。

`bash scripts/start.sh -opengauss` 启动官方 openGauss 6.0.6 LTS 介质并幂等初始化 `${OPENGAUSS_DATABASE:-business}`。样例与 MySQL/OceanBase 的普通业务域一致，包含 `customers`、`products`、`orders`、`order_items` 和连接探针，覆盖主外键、唯一约束、复合索引、Decimal、Boolean 与时间字段。System 中必须注册为 `engine_type=opengauss`；宿主机连接 `localhost:${OPENGAUSS_PORT:-5435}`，容器网络连接 `business-opengauss:5432`，固定账号 `gaussdb`。不得登记为 PostgreSQL，也不得据 PG 兼容性宣称 PostGIS、CDC 或 PostgreSQL 扩展能力。

`bash scripts/start.sh -kingbase` 在 owner-managed Linux x86_64 主机启动 `kingbase` Compose profile 下的无卷 KingbaseES PG 模式容器，并反复执行 `business/kingbase/init.sql` 收敛探针、`customers`、`products`、`orders` 与 `order_items` 样例。调用前必须从 owner-only 当前 shell 注入 `KINGBASE_PASSWORD`、`ADDP_KINGBASE_LICENSE_FILE` 与 `ADDP_KINGBASE_LICENSE_SHA256`；可选覆盖 `KINGBASE_DATABASE`、`KINGBASE_USER` 与 `KINGBASE_PORT`。该选项必须独立使用，且不属于 `-all`。System 中必须注册为 `engine_type=kingbase`，宿主机连接 `127.0.0.1:${KINGBASE_PORT:-5436}`。停止和重启分别使用 `bash scripts/stop.sh -kingbase`、`bash scripts/restart.sh -kingbase`；状态检查使用 `bash scripts/kingbase.sh status`。停止会删除本轮容器并验证零残留。

`bash scripts/start.sh -dameng` 在 ARM64 Docker Server 上构建并启动 DM8 官方介质技术夹具，反复执行 `business/dameng/init.sql` 收敛 `ADDP_ENGINE_PROBE` 与 `ADDP_RELATIONAL_SAMPLE`。宿主机连接为 `127.0.0.1:${DAMENG_PORT:-5236}`，固定测试用户为 `SYSDBA`；该连接只用于手工技术观察，ADDP 当前不能注册它。停止使用 `bash scripts/stop.sh -dameng`，状态检查使用 `bash scripts/dameng.sh status`；停止会删除本轮容器并验证零残留，本地镜像与官方介质缓存由显式清理或 T5 owner 门禁回收。

### scripts/online-workbench-mysql-fixture.sh - Workbench T4 MySQL Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用，且只接受仓库外 `ADDP_ONLINE_WORKBENCH_MYSQL_*` 环境变量。它不读取或生成 `business/.env`，只操作由 `business/mysql` Compose service 拥有的 `business-mysql`，重建确定性测试数据，并将预置 Engine Instance 使用的账号收敛为仅有 `SELECT` 权限。个人开发环境不得调用该脚本。

```bash
bash scripts/online-workbench-mysql-fixture.sh start
bash scripts/online-workbench-mysql-fixture.sh status
bash scripts/online-workbench-mysql-fixture.sh stop
```

### scripts/online-oceanbase-consumer-fixture.sh - OceanBase 消费链路 T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用，且只接受仓库外 `ADDP_ONLINE_OCEANBASE_*` 环境变量。它固定使用 `oceanbase/oceanbase-ce:4.4.2-lts` 和 `business/oceanbase` Compose service，不读取或生成 `business/.env`。`start` 将 `addp_online_consumer_source` 恢复为 5 行 watermark 基线并将同构目标表清空；`advance` 只更新 1 行并新增 1 行；`stop` 在移除容器前再次恢复基线，因此成功、失败和中断后的物理数据边界一致。永久 Engine Instance 必须以 `engine_type=oceanbase` 指向同一端点，Fixture 不创建、修改或删除 Engine Instance；正式 T4 通过 Meta 定位目标后，分别由 Manager、Develop 和 Service 读取 Transfer 结果并比较一致性。个人开发环境不得调用该脚本。

```bash
bash scripts/online-oceanbase-consumer-fixture.sh start
bash scripts/online-oceanbase-consumer-fixture.sh advance
bash scripts/online-oceanbase-consumer-fixture.sh status
bash scripts/online-oceanbase-consumer-fixture.sh stop
```

### scripts/online-tidb-consumer-fixture.sh - TiDB 消费链路 T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用，只接受仓库外 `ADDP_ONLINE_TIDB_*` 环境变量，并复用 `scripts/test/docker-compose.tidb-t2.yml` 中固定 digest、无数据卷的 PD/TiKV/TiDB 三组件拓扑。`start` 重建 5 行 watermark 源表和同构空目标表，`advance` 更新 1 行并新增 1 行，`stop` 执行 `down --volumes --remove-orphans` 并验证本次 Compose project 容器零残留。永久 Engine Instance 必须以 `engine_type=tidb` 指向 `localhost:${ADDP_ONLINE_TIDB_PORT}`；Fixture 不创建、修改或删除 Engine Instance。个人开发环境不得调用该脚本。

```bash
bash scripts/online-tidb-consumer-fixture.sh start
bash scripts/online-tidb-consumer-fixture.sh advance
bash scripts/online-tidb-consumer-fixture.sh status
bash scripts/online-tidb-consumer-fixture.sh stop
```

### scripts/online-manager-minio-fixture.sh - Manager 内部产物血缘 T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用。它通过 `business/minio` Compose service 管理独立 Business MinIO，将仓库已有的小型 `pdal_las12_format0.las` 与确定性的 3 页 `addp_online_preview_fixture.pptx` 幂等写入配置的两个 object，供同一个永久 MinIO Engine Instance 扫描。脚本只接受仓库外 `ADDP_ONLINE_MANAGER_MINIO_*` 变量，不读取或生成 `business/.env`，也不接触 Manager infra MinIO。个人开发环境不得调用该脚本。

```bash
bash scripts/online-manager-minio-fixture.sh start
bash scripts/online-manager-minio-fixture.sh status
bash scripts/online-manager-minio-fixture.sh stop
```

### scripts/online-security-transfer-fixture.sh - Security/Transfer T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用。它复用 PostgreSQL Online Fixture，并管理独立的 `business-mongodb`：幂等建立 3 条合成文档和只读 MongoDB 账号，同时准备固定 PostgreSQL 目标表。平台长期预置的 MongoDB Engine Instance 只能使用只读账号；root 凭据仅由 Fixture 生命周期使用。脚本不读取或生成 `business/.env`，个人开发环境不得调用。

```bash
bash scripts/online-security-transfer-fixture.sh start
bash scripts/online-security-transfer-fixture.sh status
bash scripts/online-security-transfer-fixture.sh stop
```

### mysql/test-data.sh - MySQL Spatial 测试数据

```bash
bash scripts/start.sh -mysql
bash mysql/test-data.sh
```

该脚本幂等重建普通业务表，以及 `POINT`、`LINESTRING`、`POLYGON`、`MULTIPOINT`、`MULTILINESTRING`、`MULTIPOLYGON`、`GEOMETRYCOLLECTION`、通用 `GEOMETRY`、多几何列和 3857 样例，并校验几何有效性与空间索引。测试数据只允许显式执行，不挂接 `scripts/start.sh -mysql`，避免启动业务数据库时破坏已有数据。

### Oracle Spatial 测试源

Oracle 必须使用常规镜像 `gvenzl/oracle-free:23`。官方 `-slim` 镜像会卸载 Oracle Spatial 和 Oracle Locator，不能通过用户授权或普通初始化 SQL 恢复。

`bash scripts/start.sh -oracle` 会幂等初始化普通表、`CUSTOMER_LOCATIONS` 点要素表和 `SPATIAL_FEATURES` 综合空间表，并验证：

- `MDSYS.SDO_GEOMETRY` 可用；
- `Point`、`LineString`、`Polygon`、`MultiPoint`、`MultiLineString`、`MultiPolygon` 和 `GeometryCollection` 均可转换为标准 WKB；三维 `SDO_GEOMETRY` 由 Oracle `TO_GEOJSON` 保留 Z 后再由 Transfer 归一化为 EWKB；
- `USER_SDO_GEOM_METADATA` 中存在 SRID 4326 元数据；
- 两张空间表均具有 Oracle Domain Spatial Index。

已有 `business_oracle_data` 如果由 `-slim` 镜像创建，推荐重建该 Business Oracle volume，避免在已裁剪的数据库字典上手工补装组件。该操作会删除现有 Business Oracle 数据，执行前必须确认其中没有需保留的数据：

```bash
docker compose -f business/docker-compose.yml stop oracle
docker compose -f business/docker-compose.yml rm -f oracle
docker volume rm business_oracle_data
cd business && bash scripts/start.sh -oracle
```

### Oracle CDC 测试源

`bash scripts/start.sh -oracle` 会在数据库 ready 后执行 `oracle/init-cdc.sh`，幂等启用 `ARCHIVELOG`、force logging、minimal supplemental logging，并创建 `${ORACLE_CDC_USER:-C##ADDP_CDC}` LogMiner 专用 common user。`CUSTOMERS`、`CUSTOMER_LOCATIONS` 和 `SPATIAL_FEATURES` 由 `init.sql` 幂等启用 `SUPPLEMENTAL LOG DATA (ALL) COLUMNS`，分别作为普通字段、单一 Point 和混合二维几何族 CDC 样例。Oracle Engine 使用 schema owner 的 business 主账号做 Catalog/读取，并供 Transfer 创建 generation-owned Spatial WKB/GeoJSON 镜像表、行级触发器和 DDL guard；XYZ 场景使用 GeoJSON CLOB 保留 Z，Transfer 最终仍输出 EWKB。System 的 `connection_info` 另存 `cdc_database_name`、`cdc_user` 和加密的 `cdc_password`，LogMiner 不复用业务账号或 SYS。

启用 Redpanda 时，脚本会创建或轮换 `${BUSINESS_KAFKA_READER_USERNAME:-addp_transfer}` 的 SCRAM-SHA-256 密码，并只授予读取 Topic、消费组和描述集群所需权限。System 中统一注册为 `engine_type=kafka`，连接 `localhost:${BUSINESS_KAFKA_PORT:-29092}`；不要注册 `addp-redpanda` 的 Infra Kafka 地址。

### scripts/stop.sh - 停止服务

```bash
bash scripts/stop.sh
```

停止所有业务库服务。

### scripts/restart.sh - 重启服务

```bash
bash scripts/restart.sh
```

检测架构、清理旧镜像、重启服务（幂等）。

### spark/init-test-data.sh - Spark 测试数据

```bash
bash spark/init-test-data.sh
```

幂等重建并真实查询验证 `default.addp_sample_orders`。Spark Master 启动时会自动执行该脚本，因此 Develop 查询工作台可以从 Spark 实时 Catalog 动态生成可执行样例。

## 常用操作

### 查看日志

```bash
docker-compose logs -f postgres    # PostgreSQL 日志
docker-compose logs -f supermap-postgresql # SuperMap SDX+ for PostgreSQL 专用实例日志
docker-compose logs -f minio       # MinIO 日志
docker-compose logs -f clickhouse  # ClickHouse 日志
docker-compose logs -f mongodb     # MongoDB 日志
docker-compose logs -f oceanbase   # OceanBase CE 日志
docker-compose logs -f tidb        # TiDB SQL 节点日志
docker-compose logs -f business-redpanda # Business Redpanda 日志
docker-compose logs -f doris-fe    # Doris 日志
docker-compose logs -f spark-master  # Spark 日志
```

### 数据备份

```bash
# 备份
docker exec business-postgres pg_dump -U business business > backup.sql

# 恢复
docker exec -i business-postgres psql -U business business < backup.sql
```

### PostGIS 验证

```bash
docker exec business-postgres psql -U business -d business -c "SELECT PostGIS_Version();"
```

## 生产部署

### 1. 部署到服务器

```bash
# 复制文件
scp -r business/ user@server:/opt/addp-business/

# 登录并启动
ssh user@server
cd /opt/addp-business
cp .env.example .env
vim .env  # ⚠️ 修改密码（必须！）
bash scripts/start.sh
```

### 2. 安全加固（必须！）

1. **修改默认密码**：使用 `openssl rand -base64 32` 生成强密码
2. **限制网络访问**：仅允许 ADDP 系统访问
3. **定期备份**：配置 crontab 自动备份
4. **监控磁盘**：定期检查数据卷大小
5. **更新镜像**：定期运行 `docker-compose pull && ./restart.sh`

## 故障排查

### 端口冲突

```bash
lsof -nP -i :5433           # 查看占用
# 修改 .env 中的 POSTGRES_PORT
```

### 架构不匹配

```bash
bash scripts/restart.sh  # 自动检测并使用正确架构
```

### 数据恢复

```bash
bash scripts/stop.sh
docker volume rm business_postgres_data
bash scripts/start.sh
docker exec -i business-postgres psql -U business business < backup.sql
```

## 技术细节

- **PostgreSQL**: 15.x + PostGIS 3.4.x
- **MinIO**: latest
- **ClickHouse**: 23.8
- **MongoDB**: 7.0
- **OceanBase CE**: 4.4.2 LTS (MySQL mode)
- **Apache Doris**: 2.1.0 (all-in-one)
- **Apache Spark**: 3.5.0
- **网络**: business-network (bridge)
- **持久化**: Docker volumes
- **架构**: ARM64, AMD64

## 常见问题

**Q: 业务库和系统库有什么区别？**  
A: 系统库存储 ADDP 平台元数据，业务库存储用户实际业务数据。

**Q: 为什么需要 PostGIS？**  
A: 支持 Shapefile、GeoJSON 等空间数据的存储和查询。

**Q: 可以替换为云服务吗？**  
A: 可以！支持 AWS RDS/S3、阿里云 RDS/OSS 等云服务。

**Q: 脚本可以重复执行吗？**  
A: 可以！所有脚本都是幂等的。
