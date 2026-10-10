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
- **达梦 DM8 ARM64**：固定鲲鹏 920/麒麟 10 SP1 官方安装 ZIP、服务端/驱动 SHA-256，本机只构建不可推送的 disposable 镜像，端口 5236；以独立 `engine_type=dameng` 登记，真实 Provider 只在 Linux ARM64 Docker 中执行。
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

本地启动 PostgreSQL、MySQL、MinIO 时，脚本会检查首选宿主机端口；首次冲突时自动选择空闲端口，并把成功启动的实际映射写入忽略版本控制的 `.business-state/ports.env`。后续重启沿用该端口，避免已登记的 Engine Instance 身份漂移；固定端口再次被占用时启动会明确失败。容器内部端口保持不变。本机开发注册引擎时使用 `127.0.0.1:实际宿主机端口`，同一 Docker 网络中的服务使用 `business-*` 服务名与固定内部端口。

Business bridge 网络由标准启动脚本创建并保留，网段记录在 `.business-state/network.env`。OceanBase 单节点的旧数据依赖首次部署时的容器内网 IP；标准启动脚本只读其 OBD 配置，并在重建容器前恢复同一地址。地址冲突或网段丢失时会拒绝启动，不迁移或重建旧数据；其他引擎仍使用服务名，不依赖容器 IP。

OceanBase 的 `observer/run` 使用 tmpfs 保存 PID 与 socket；它们不进入持久数据卷，容器重建不会误用上次进程的 PID。

`-nfs` 使用 macOS 系统 NFS。已有正确导出且服务正在运行时可直接复用；首次配置仍需使用 `sudo bash scripts/start.sh -nfs`。

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
│   ├── online-raster-minio-fixture.py # 栅格 T4 两套隔离 MinIO、原生像元与 COG 校验
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
| 达梦 DM8 | - | 端口 5236；ARM64 官方介质 disposable 数据库 |
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

达梦 DM8 只使用 `dm8_20260708_HWarm920_kylin10_sp1_64.zip`，外层 ZIP SHA-256 为 `d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824`，内层 ISO SHA-256 为 `2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e`，同介质官方 Go 驱动 ZIP SHA-256 为 `96516ecd7c9ec3c405109b81135f2c9d87ed0631632e99cd2de0e06f03b1392e`。`bash scripts/start.sh -dameng` 仅接受 Docker Server `linux/arm64`，在本机从官方介质构建 `addp/dameng:dm8-20260708-arm64` 并启动无卷 `dameng` profile；不会上传、归档或重分发介质、驱动与镜像。该介质面向鲲鹏 920/麒麟 10 SP1，本机 Ubuntu ARM64 容器只属于非认证 ABI 技术验证；内置试用授权固定于 2027-07-07 到期，不能作为长期门禁或通过重建重置。System 可在 macOS 开发模式登记 `engine_type=dameng`，但宿主进程不执行连接测试；真实 Provider T2/T4 从运行中的 Business 镜像提取并校验官方驱动后，在 Linux ARM64 Docker 内执行。

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

`bash scripts/start.sh -dameng` 在 ARM64 Docker Server 上构建并启动 DM8 官方介质数据库，反复执行 `business/dameng/init.sql` 收敛 `ADDP_ENGINE_PROBE` 与 `ADDP_RELATIONAL_SAMPLE`。宿主机连接为 `127.0.0.1:${DAMENG_PORT:-5236}`，固定 disposable 测试用户由脚本创建；System 以 `engine_type=dameng` 登记该端点。停止使用 `bash scripts/stop.sh -dameng`，状态检查使用 `bash scripts/dameng.sh status`；停止会删除本轮容器并验证零残留，本地镜像与官方介质缓存由显式清理或 T5 owner 门禁回收。

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

该入口要求 `ADDP_ONLINE_HOST=1`：Manager 内部产物套件由 GitHub Hosted Linux x86_64 运行，混合检索套件仍由 macOS 专用 Runner 运行。它通过 `business/minio` Compose service 管理独立 Business MinIO，用 Python 标准库在临时目录确定性生成 LAS 1.2 三点 EPSG:3857 夹具，并发布仓库已跟踪的 3 页 PPTX；Hosted 套件无需本机 NFS 样例或永久 Engine Instance，退出时销毁业务容器、网络和数据卷。只有混合检索套件额外发布专用部署中的 JPG。模型夹具由脚本在操作系统临时目录用 Python 标准库确定性生成：`model3d/dae/model.dae` 和 `model3d/3ds/model.3ds` 各包含一个带 UV 的静态三角形，并引用同目录的 `texture.png`；两者不引入外部下载。IFC 与单体 OSGB 使用 `business/fixtures/manager/addp_online_model_fixture.*` 自有夹具：IFC 来源于 Model3D 原生预检的 IFC4 米制立方体；OSGB 由现有 `docker/converter/tests/osgb_compression_smoke.cpp` 的 `generate ... dxt5` 生成，保留 zlib 压缩、8 × 8 红色 DXT5 贴图和带 UV 的三角形，不使用第三方数据或开发容器。SKP 自有夹具 `addp_online_model_fixture.skp` 由固定 OpenSKP 1.3.0 的 `create()` 生成：同一组件中的三角形坐标为 `(0,0,0)`、`(1,0,0)`、`(0,2,0)`（英寸），正背面使用同一内嵌 4 × 4 彩色 PNG；放置两个组件实例，第二个沿 X 位移 2 英寸。生成的 GLB 共享一个网格，场景米制尺寸为 `[0.0762, 0, 0.0508]`，贴图 SHA-256 为 `3168c3aa8cd3338d29b9ca74ee6794a2290b69d4da04d0869788187cb6ac4cf3`。夹具直接随仓库复制到 `model3d/skp/model.skp`，Business 不安装解析器或生成解析元数据。MAX 夹具 `addp_online_model_fixture.max` 是未经修改的 0 A.D. 项目贡献者马模型，由 Stanislas Dolcini（StanleySweet）分享，许可为 [CC-BY-SA-3.0](https://creativecommons.org/licenses/by-sa/3.0/legalcode)，来源为 [io_scene_max issue 1](https://github.com/nrgsille76/io_scene_max/issues/1) 和其中的 [horses.zip](https://github.com/user-attachments/files/15525512/horses.zip)。模型 SHA-256 为 `916714595bf7a2953264b8afaafe339fa60bdb4944b4664f6802ccb288190653`；不声明动画或骨架保真。ADDP 自建红色 4 × 4 测试 PNG `addp_online_max_texture.png` 并非原始 0 A.D. 贴图，复制为同目录 `max/texture.png`，由验收显式声明源 bitmap 引用映射。DAE 使用厘米单位，生成后的两类模型均为 1 × 2 的三角形。模型 object 路径固定为该专用夹具的协议，不增加部署配置。脚本只接受仓库外 `ADDP_ONLINE_MANAGER_MINIO_*` 变量，不读取或生成 `business/.env`，也不接触 Manager infra MinIO。个人开发环境不得调用该脚本。

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
- **MinIO**: `RELEASE.2025-10-15T17-29-55Z`（由 `scripts/infra/Dockerfile.minio` 从固定官方源码修订构建，包含 `mc`）
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

## Elasticsearch

通过 `bash business/scripts/start.sh -elasticsearch` 按需启动官方 Elasticsearch 9.5.4 单节点，支持 ARM64 和 AMD64。停止和重启使用同目录 `stop.sh -elasticsearch`、`restart.sh -elasticsearch`，数据卷保留。镜像固定 manifest digest，内存上限 2 GiB。宿主机只开放回环 HTTP，启用 Basic 认证；远程环境使用 HTTPS，插件允许提供 PEM CA，验证服务器证书。

配置位于 `business/.env` 的 `ELASTICSEARCH_PORT`、`ELASTICSEARCH_PASSWORD`、`ELASTICSEARCH_READER_USER`、`ELASTICSEARCH_READER_PASSWORD`；实际端口查看 `business/.business-state/ports.env`。初始化使用管理员，仅创建/更新固定 ID 的样例记录，不删除已有索引；创建 `addp_orders.v1` 和空索引 `addp_empty.v1`，内容包括对象、nested、multi-field、数组、null、缺失字段和超出安全整数范围的 long。注册 ADDP 引擎时使用只读用户，该角色仅有 `addp_*` 的 `read` 与 `view_index_metadata`，以及 Spark 官方连接器版本与健康检查需要的 `cluster:monitor/main`、`cluster:monitor/health` 两项集群动作；没有写入、节点发现及集群配置权限。

本机标准 `scripts/utils/register-business.sh` 在 ES 容器运行时会读取实际端口并注册只读接入。System 新增 `elasticsearch` 引擎填写 `endpoint`、`user`、`password`，按需填写 `tls_ca_cert`。目录仅展示普通、打开、非隐藏的具体索引；Meta 获取 Mapping；Manager 读取文档；Develop 在选定索引后使用 `es_dsl`。首版不提供聚合、脚本、别名、数据流、跨索引查询或 Transfer 写入/同步。

Spark Workflow 的 `load` 通过具体索引 locator 读取，Spark 计算集群独立选择。公开 `array_fields=["tags"]` 声明普通数组字段，nested 自动映射为对象数组；源连接与索引名在执行时派生。首版固定官方 ES Spark 9.5.4 连接器，仅 HTTP Basic 开发实验接入，HTTPS 明确拒绝；不支持别名、data stream、隐藏索引、DSL、写回、流式或全 DAG 统一 PIT。JSON 摘要中超出 JavaScript 安全整数范围的 bigint 使用十进制字符串，计算内部保留原类型。

验证入口：`make test-business-config`、`make test-common-elasticsearch-unit`、`make test-common-elasticsearch`。后者创建独占容器、认证用户及样例索引，验证 Common、Manager、Meta 及真实 Spark Worker 的多分片读取、空索引、数组声明和大整数后删除容器、卷和网络，不使用现有 Business 实例。

正式 T4 使用 `make test-online ONLINE_SUITE=elasticsearch-consumer-flow`，由 `online-hosted-elasticsearch-gate.sh` 在 Hosted Ubuntu 临时部署运行；物理夹具复用官方镜像、只读账号与 25 条文档/空索引初始化。System 独立身份经正式 API 登记引擎，普通用户从 Console 完成 Meta 重扫、Manager 预览和 Develop DSL 查询，并查看通过正式 Develop API 提交的 ES → Spark 工作流结果；该场景通过根产品构建入口生成 Spark Runtime 镜像，核对实际 image ID 与 `python api_server.py` 默认入口，使用独立 Master/Worker，核对四个节点、八次 Runtime 状态查询及实际 Executor 任务，归档同一身份的报告及五张截图。所有退出路径销毁临时业务容器、Infra 和凭据，核对零残留；仅人工触发，不登记 schedule。

产品容器默认入口已于 2026-10-10（北京时间）复验通过：[ES T4 37954898673](https://github.com/pampa0629/addp/actions/runs/37954898673)，提交 `ddaa982600608c94db56e04ac5ed130765cd77c1`。归档报告确认同一租户与普通用户完成 Meta 页面重扫、Manager 25 条文档/空索引预览、Develop 25 条 DSL 查询及正式 ES → Spark 四节点工作流，保留五张 Console 截图。工作流读取 25 行、金额合计 562.5，大整数 `9007199254740993` 在摘要中保持十进制字符串；实际 Worker 完成 14 个任务，连续八次 Runtime 状态查询一致。产品镜像实际 image ID 与默认入口核验通过，最终 `result=passed`、`cleanup=passed`、`infra_cleanup=zero_residuals`。该记录只证明对应提交的 HTTP Basic 只读消费范围，不包含 Spark HTTPS 接入，也不替代后续变更的重新验收。

## Business Redis

`bash business/scripts/start.sh -redis` 按需启动 Redis 7.2.13 单机；停止和重启使用 `stop.sh -redis`、`restart.sh -redis`，保留独立 `redis_data` 卷。镜像统一使用 Docker 官方发布的 ECR Public 多架构镜像并固定 OCI digest，支持 AMD64 和 ARM64，不复用 Infra Redis 容器、账号或数据。Business 首选回环端口为 `6380`，首次启动解析空闲端口，后续沿用 `business/.business-state/ports.env` 中的实际映射。

配置项为 `BUSINESS_REDIS_PORT`、`BUSINESS_REDIS_ADMIN_PASSWORD`、`BUSINESS_REDIS_READER_PASSWORD`，避免与 Infra `REDIS_*` 混淆。关闭匿名 default 用户，管理员为 `addp_business_admin`，只读用户为 `addp_business_reader`；角色使用显式命令白名单。该实例只开放 DB 0，读取账号可读取实例内全部业务 key，但不能执行写入、脚本、CONFIG、ACL 或 FLUSHALL。ACL 文件在容器 tmpfs 中由密码哈希生成；数据使用 AOF 持久化。

启动后仅补齐缺失的 `addp:sample:*` 固定样例，包含 string、大整数计数、hash、list、set、zset、stream、TTL 和带非 UTF-8 字节的 key/value。重复初始化不重复追加 list/stream、不重置仍存在的 TTL、不删除或覆盖既有 key；样例出现不符的原生类型时先整体拒绝初始化。初始化脚本只能由管理员执行，不属于读取账号的查询能力。

已实现独立 Business 部署、原生数据夹具、Redis Engine Plugin、System 连接注册、Meta 键值数据集身份扫描与 Manager 原生有限样本预览。System 通用表单按 ConnectionSpec 配置 ACL 用户、数据库编号与 TLS，注册脚本读取真实容器映射并使用只读账号登记数据库 0；连接检测执行 SELECT 和 DBSIZE，不读写业务 key。Catalog 为 `server -> keyspace`，Meta 只登记一个 `data_type=key_value` 数据集，内容键使用 `k:` 加无填充 Base64URL 令牌，原生类型、长度和 TTL 实时读取，不能伪装为 document collection。SCAN 是有预算的弱一致遍历，COUNT 不保证返回条数；扫描失败保留既有目录。Develop 查询、写入、迁移及 Cluster/Sentinel 尚未实现。消费链路验证使用 `make test-common-redis-unit` 与 `make test-common-redis`。

验证入口为 `make test-business-config` 和 `make test-business-redis`。后者创建独占 Compose project、随机回环端口和随机密码，核验认证拒绝、只读权限、原生类型、精确字节、初始化幂等和重启持久化，退出后检查容器、数据卷和网络零残留。该入口已纳入 `make test-integration`、模块/变更自动发现和 `release-and-t2-gates.yml`，不连接 Infra 或现有 Business Redis。

Redis 插件与消费契约验证入口：`make test-common-redis-unit`、`make test-common-redis`。后者复用同一独占夹具生命周期，验证真实 ACL 凭据、数据库选择、权限不足、System 密码加密/脱敏与在线状态更新，以及 Meta 键值数据集扫描和 Manager 原生预览，并已登记 Common/System/Meta/Manager/Business 变更触发的 CI 门禁。

Manager 内部产物验收 `manager-internal-artifact-lineage` 使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `scripts/test/online-hosted-manager-gate.sh` 从零建立最小权限身份、Business MinIO 与真实 Runtime，复用 `make test-online` 业务断言并在退出时销毁环境；无需永久账号或自备 Runner。首次真实运行成功前只登记手工触发，不计为 T4 通过。

Transfer SQL ETL 与字段血缘验收 `transfer-relational-sql-etl` 唯一使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `scripts/test/online-hosted-transfer-gate.sh` 编排。Business owner `scripts/online-transfer-relational-sql-etl-fixture.sh` 从零创建独占 tmpfs PostgreSQL 容器、5 行固定源表及只读取源表/创建目标表的数据库用户，输出 owner-only Engine 描述；System 负责临时身份和正式 Engine API 注册。业务断言沿用 `make test-online`，owner 核对 SQL 投影过滤、原生 replace/两跳结果和 decimal 精度，退出删除容器并验证零残留，不接管本地 `business-postgres` 或读取 Business `.env`。

Redis 正式 Online 入口为 `make test-online ONLINE_SUITE=redis-consumer-flow`，由 `online-t4-gates.yml` 人工触发 `redis-hosted-t4` Job，经 `scripts/test/online-hosted-redis-gate.sh` 准备一次性 Ubuntu x86_64 部署。物理夹具复用上面的固定镜像、ACL 与九个样例，使用随机回环端口、随机密码和 tmpfs；System helper 建立非默认 Tenant、最小权限普通用户与独立引擎登记身份。断言覆盖 System 实时目录/事实、Meta keyspace 身份及重扫稳定性、Manager 九个原生预览、大整数/二进制/TTL，并实际登录 Console 从 Meta 页面重扫及打开 Manager 预览，归档截图和同一身份的报告。成功、失败及中断都销毁当次业务容器、平台 Infra 和凭据目录；不接管个人开发环境。脚本确定性验证纳入 `make test-online-runner`，仅人工触发，不登记 schedule。

Hosted T4 已于 2026-10-04（北京时间）复验通过：[运行 37169297656](https://github.com/pampa0629/addp/actions/runs/37169297656)，提交 `7398d24d743b96316a338dce4a4b39675b979d7b`。归档报告确认同一租户、普通用户和引擎完成 Meta 页面重扫及九个原生预览，并保留重扫和各样例共十张 Console 截图。该记录只证明对应提交的消费链路，不替代后续变更的重新验收。


## HDFS Simple 实验环境

在仓库根运行 `bash business/scripts/start.sh -hdfs`，启动固定官方 Hadoop 3.5.0 NameNode 与 DataNode，并使用固定 Spark 3.5.0 容器幂等生成样例。数据保留在两个独立 volume；`stop.sh -hdfs`、`restart.sh -hdfs` 沿用同一标准生命周期，不重新格式化已有 NameNode。初始化只更新 `/addp/samples/orders.csv`、`orders.json`、`orders.parquet` 和根文件 `/addp/订单 100%.csv`；每份 20 条订单，金额总和 2100，其他业务文件不变。

WebHDFS 首选回环端口 9870，原生 RPC 首选 8020。DataNode 数据端口 9866 与 HTTP 端口 9864 在容器内外保持一致，并广播 `HDFS_SHARED_HOST` 指定的宿主机地址；RPC 与 DataNode 只绑定该地址，不能只开放 NameNode。该地址必须由宿主机 Go 服务、Spark Driver 和容器 Worker 同时访问；标准入口在配置留空时检测宿主机可达 IPv4，配置四个端口发生冲突时直接失败。换网后检查地址，并在 System 编辑同一引擎实例的连接事实。

System 中选择独立 `hdfs` 引擎：填写启动输出的 `webhdfs_endpoint` 和 `rpc_uri`，`root_path=/addp`、`authentication=simple`、`user=addp_business_reader`。根目录不进入 locator；例如 `samples/orders.parquet` 保持 `type=file`。Spark 计算集群单独选择，源文件通过已授权 locator 在执行时派生原生 HDFS URI；任务不保存连接参数。Spark Workflow 部署设置 `HADOOP_USER_NAME=addp_business_reader`，Worker 使用同一应用用户；身份不符或 HDFS 保存请求会被拒绝。Simple 是开发实验的主体声明，首版不提供 Kerberos、HA、YARN、MapReduce 或 HDFS 写回。

`make test-common-hdfs-unit` 与 `make test-spark-workflow` 验证确定性契约；`make test-common-hdfs` 创建专属无宿主端口集群，验证 WebHDFS、共享格式解析、样例幂等、真实 Worker 的 CSV/JSON/Parquet 读取聚合、PostgreSQL 普通和空间结果事务保存、已有表结构及权限保留、失败回滚、实际行数与结果再次读取，以及退出后的容器、卷、网络零残留。正式消费链路使用手动 Hosted T4 `hdfs-spark-consumer-flow`；`scripts/online-hdfs-spark-fixture.sh` 只在干净 Linux x86_64 Runner 管理当次宿主网络 Hadoop/Spark/PostgreSQL 容器及现有样例，输出 owner-only 描述，由 System 独立登记。独立准备 User 通过 System 正式独立批准命令，仅为普通消费 User 授予 `results.hdfs_totals` 的精确读取 Grant，并核对授予前拒绝、授予后通过；消费 User 不拥有签发 Grant 或管理委派的 Permission，准备凭据在业务验收前撤出环境。T4 在原三格式读取基础上验证该 User 的结果保存、自动扫描、血缘、Manager 预览和下游读取；执行或写入权限不替代当前读取 Grant。PostgreSQL 凭据及 seed SQL 只保存在当次 secret 目录，不归档。Spark Master 与 Worker 固定同一官方 digest。`make test-hdfs-online-runner` 验证隔离生命周期与断言；真实 System/Meta/Manager/Develop 和 Console 仍须由该 T4 实际运行通过，不能仅以 T1/T2 声明平台验收完成。

Hosted T4 已于 2026-10-05（北京时间）通过：[运行 37217772982](https://github.com/pampa0629/addp/actions/runs/37217772982)，提交 `11bbacfd577cb568f22c47a0a9fe1a39fd3a70bc`。报告确认同一租户与普通用户完成 Meta 目录重扫、四个文件预览和 Develop 正式工作流详情；三格式各 20 行、金额合计 2100，Standalone Application 对应的真实 Worker 完成 37 个 Executor 任务，归档六张 Console 截图。应用、业务夹具和 Infra 清理通过且零残留。该记录证明此提交在标准开发入口下的 Simple 只读消费链路，容器部署入口另须验收；仍只登记手动触发。

产品容器默认入口已于 2026-10-10（北京时间）复验通过：[HDFS T4 37954899129](https://github.com/pampa0629/addp/actions/runs/37954899129)，提交 `ddaa982600608c94db56e04ac5ed130765cd77c1`。Spark Workflow 产品镜像自动登记后，同一租户与普通用户完成扫描、四文件预览及八节点正式工作流，保留六张 Console 截图；三格式各 20 行、金额合计各 2100，实际 Standalone Worker 完成 37 个任务，连续八次 Runtime 状态查询一致。产品镜像实际 image ID 与 `python api_server.py` 默认入口核验通过；Business 容器与 Runtime/临时镜像仓库分别按自身所有权清理，最终 `result=passed`、`cleanup=passed`、`infra_cleanup=zero_residuals`。该记录只证明对应提交的 Simple 只读消费范围，仍只登记手动 T4。
