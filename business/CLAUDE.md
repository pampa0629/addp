# Business 业务数据基础设施说明

## 模块定位

`business/` 提供 ADDP 业务数据基础设施的本地部署样例，与系统元数据库隔离。它用于承载用户业务数据、对象文件和外部数据源测试环境，不是 ADDP 系统元数据存储。

## 包含组件

- FTP／显式 FTPS：官方 SFTPGo v2.7.6 固定摘要，独立 `-ftp` profile，不进入 `-all`；唯一 `ftp/entrypoint.sh`、只读账号、独立文件和状态卷、回环2121及50000–50009，按需补齐中文 CSV／JSON／Parquet 示范文件，不调用 System API。协议准入、正式插件与平台 T4 分别认定。

- PostgreSQL/PostGIS、MySQL：业务关系库与 CDC 测试源。
- OceanBase Community Edition：国产分布式关系数据库的 MySQL 模式测试源，以独立 `engine_type=oceanbase` 注册；启动时幂等初始化探针及普通关系业务样例，支持非空间普通表的 bounded watermark source 与 prepare/session/delete/upsert 集成验证，不包含空间、CDC 或 Oracle 模式能力。
- TiDB 8.5.8：固定 PingCAP 官方 PD/TiKV/TiDB 三组件镜像与 OCI digest，以独立 `engine_type=tidb` 注册；启动时幂等初始化普通关系样例，首版只声明真实 T2 验证的非空间目录、查询、bounded watermark 与 prepare/session/delete/upsert，不包含 MySQL replication、TiCDC、空间或分区变化应用能力。
- openGauss 6.0.6 LTS：在具备 NUMA 的 Linux x86_64 主机从固定 SHA-256 的官方 Docker tar 加载，以独立 `engine_type=opengauss` 注册；macOS 不启动其含 MOT 的官方容器，实库门禁由 GitHub Actions hosted-only T2 承担；启动时幂等初始化普通关系业务样例，首版只声明经门禁验证的 PG 兼容非空间目录、查询、读取、COPY 写会话、bounded watermark 与 upsert，不包含 PostGIS、CDC 或 PostgreSQL 扩展能力。
- KingbaseES V9R1C10 `V009R001C010B0004`：Business 按宿主 CPU 只选择固定 SHA-256 的官方 x86_64 或 ARM64 Docker tar，统一加载为仓库定义的中性本地镜像引用，并以独立 `engine_type=kingbase` 注册；它作为 `business/docker-compose.yml` 的独立 `kingbase` profile，由 `business/scripts/kingbase.sh` 按 `docker compose create`、License 注入、`docker compose start` 的单一路径管理无卷 disposable 容器与幂等普通关系样例，不进入日常 `start.sh -all`。正式 T5/T2/T4 仍只在 owner-managed Linux x86_64 主机使用 x86_64 介质和 owner 正规 License。首版只声明 PG 模式下经真实门禁验证的非空间目录、查询、读取、COPY 写会话、bounded watermark 与 `ON CONFLICT` upsert，不包含 PostGIS、CDC、PostgreSQL 扩展或 openGauss `MERGE`。
- 瀚高 V4.5：固定已确认的社区镜像与 OCI digest，独立 `highgo` profile，仅通过 `scripts/start.sh -highgo` 启用，不进入 `-all`。仓库初始化直接注入配置密码、保留 SM3/TLS 和默认安全策略；独立数据卷保存 `highgo/addp_business` 探针及客户／商品／订单／明细样例，普通关系种子由单一事务幂等写入，System 使用普通业务账号 `business`，管理员与业务密码分别配置；首次建卷和标准启动复用唯一幂等初始化，业务密码变化由 SYSSSO 轮换，管理员密码不自动轮换。Business 不调用 System API，停止不删除卷；详见 README 的瀚高节。
- 瀚高跨模块 T4 只由 `scripts/online-highgo-consumer-fixture.py` 管理 GitHub Hosted Linux x86_64 一次性 HighGo/PostgreSQL 容器；复用 Business 固定摘要与唯一初始化，不读取个人 `.env`，不调用 System API。普通消费身份、正式 Engine 注册及 PG 源表授权由对应 owner 准备，退出核对当次容器及匿名卷零残留。
- 达梦 DM8 ARM64：只使用固定 SHA-256 的鲲鹏 920/麒麟 10 SP1 官方安装介质构建本地、不可推送的无卷 disposable 数据库；作为独立 `dameng` profile 由 `business/scripts/dameng.sh` 管理，不进入日常 `start.sh -all`。macOS ARM64 Docker Desktop 仅承担 Linux ARM64 兼容层技术验证；Business 只管理数据库与幂等样例，不调用 System API。`engine_type=dameng` 的注册由 System/测试 owner 完成，官方 Go 驱动只从同版本介质注入 Linux ARM64 T2/T4 执行容器，不进入仓库或 Artifact。
- OceanBase 跨模块 T4 只由 `scripts/online-oceanbase-consumer-fixture.sh` 管理固定源/目标表；Fixture 不创建 Engine Instance，并必须在退出路径恢复 5 行源基线和空目标表。
- TiDB 跨模块 T4 只由 `scripts/online-tidb-consumer-fixture.sh` 管理无卷三组件集群和固定源/目标表；Fixture 不创建 Engine Instance，退出路径必须删除本次 Compose project、volumes 与 orphans 并验证容器零残留。
- openGauss 跨模块 T4 的数据库生命周期只由 `scripts/online-opengauss-consumer-fixture.sh` 在 GitHub Hosted Linux x86_64 管理；Fixture 复用固定官方介质，创建当次 disposable database 和源/目标表，并输出 owner-only Engine 描述。System IAM 身份和 Engine API 注册分别由 `system/backend/cmd/online-test-fixture` 与 `scripts/test/online-engine-registration.py` 负责；Business 不调用 System API。退出时删除 owner 容器，不读取根 `.env` 或 Business `.env`。
- Security 四出口 T4 的物理夹具只由 `scripts/online-security-owner-fixture.sh` 在 GitHub Hosted Linux x86_64 管理：每轮建立独立 MySQL/PostGIS 容器，复用 MySQL 样例生成器，输出 owner-only Engine 描述，并核对容器归属与退出零残留。Business 不调用 System/Security API；身份、引擎登记、治理初始化由各 owner 正式服务负责。
- KingbaseES 跨模块 T4 的数据库生命周期只由 `scripts/online-kingbase-consumer-fixture.sh` 在受保护 owner-managed Linux x86_64 Runner 管理；Fixture 使用固定官方介质与 owner License 创建当次无卷容器、源/目标表和 owner-only Engine 描述，增量使用 `ON CONFLICT`。System IAM 身份和 Engine API 注册继续由通用 owner 负责；Business 不调用 System API。退出时删除容器、镜像缓存与凭据目录，并验证零残留。
- Oracle Free 23ai：普通表、Schema、Oracle Spatial、只读快照、普通表 CDC 与 Oracle Spatial CDC 测试源；ArcGIS SDE 作为后续独立能力路线预留。
- Redpanda：独立业务 Kafka API 消息流，不承载 ADDP Infra Kafka topic。
- MinIO：业务对象存储。
- HDFS 3.5.0：独立 NameNode/DataNode，Simple 开发实验模式，`/addp/samples` 下 CSV、JSON、Parquet 各 20 条订单，金额合计 2100。通过 `bash scripts/start.sh -hdfs` 启动和幂等初始化；只创建样例，不登记 System 引擎。本机 Docker Desktop 实例的四个端口均绑定 `127.0.0.1`，DataNode 广播稳定主机名 `host.docker.internal`；宿主机须配置对应回环映射，启动前验证。WebHDFS 使用回环端点，System RPC 登记 `hdfs://host.docker.internal:<实际端口>`，换网无需修改连接。不再检测局域网 IP 或使用 `HDFS_SHARED_HOST`。端口为显式部署配置，冲突直接失败，不自动漂移。
- ClickHouse、MongoDB、Doris、Spark：可选业务数据源和分析组件。
- MySQL/Doris 的 Spark 页面 T4 扩展既有 `hdfs-spark-consumer-flow`，`online-hdfs-spark-fixture.sh` 从真实 Worker T2 读取固定镜像并复用 Doris 唯一初始化。`online-spark-table-oracle.py` 只访问本轮有 owner 标签的业务容器，核对精确类型、原生表身份、DDL 和暂存表；目标名绑定 Run ID，删除后确认不存在。普通 User、Engine 登记和精确表读取 Grant 仍由 System owner 的现有准备流程负责，Business 不访问平台 API。
- 本机 Doris all-in-one 只发布回环 FE 9030/8030 与 BE 8040/9050/9060/8060，BE 公布 `host.docker.internal`，仅 Doris 容器将其解析到自身回环地址，其他容器使用宿主网关解析。`doris/init.sh` 是唯一初始化与 FE/BE 健康检查入口，复用官方启停；已有回环 BE 必须以原生 HOSTNAME 修改保留 Backend ID 和数据，不删除后重建。确定性迁移与反例归 `make test-business-config`；新库、重复登记保留 Backend ID 和真实 Worker 读写归 Hosted `make test-common-spark-table`。
- Neo4j：图业务数据测试环境。
- FalkorDB 4.20.6：独立 `business-falkordb`，通过 `start.sh -falkordb` 管理，回环首选 6381、独立密码和持久卷；用户明确要求的 Infra 快照只在首次启动前一次性恢复，不自动复制或持续同步。
- Redis 7.2.13：独立 Business 单机键值样例，不复用 Infra Redis。使用固定官方多架构镜像、单独数据卷和 ACL 管理/只读账号；ADDP Redis Engine Plugin 提供 ACL 连接登记、认证检测、server → keyspace 目录、Meta 键值数据集身份扫描与 Manager 原生有限样本预览；Develop 查询尚未实现。

GaussDB 混合云开发者版 V2.0-10.0.0 使用独立 `gaussdb` profile，`scripts/start.sh -gaussdb` 必须单独调用，不进入 `-all`。`gaussdb/image.py`、Dockerfile 与 entrypoint 是 Business 和原生 Linux 双架构 Release 认证唯一共用的构建／初始化路径。首选回环 SQL 5437，独立持久卷；仅空数据目录初始化，缺少完成标记的旧库直接失败，停止不删除卷，重启只补缺失样例。管理员和普通账号密码分别配置，不自动轮换，不调用 System API。开发者版只用于非商业学习、适配及测试。独立 `engine_type=gaussdb` 只读插件已接入；Linux ARM64／amd64 Provider T2 与六权限普通 User 的 System—Meta—Manager—Console Hosted T4 已分别通过，精确源码、运行及清理证据见 README 的 GaussDB 节。写入、空间、CDC 和 Spark 接入不在已验证范围。

StarRocks 固定3.5.21官方多架构镜像由 Compose 唯一持有，`starrocks/config.py` 供准入和持久化门禁读取。独立 `-starrocks` profile 不进入 `-all`，使用回环9031/8031/8041和两个持久卷。`starrocks/entrypoint.sh` 在原生 FE 就绪后设置首次 root 密码，再启动官方 director 注册 BE；不替换 FE/BE，不自动重置已有密码，不接管无初始化标记的元数据。初始化仅补缺失样例和普通只读账号，Business 不登记平台 Engine。`init.sql` 唯一维护两行精度探针和客户5／商品6／订单10行业务示范，固定主键只补缺失记录，不覆盖用户修改。持久化、Backend ID、用户修改、缺失样例恢复、三表关联金额／日期／布尔／空值及只读反例由现有双架构 `make test-common-starrocks` 的 Business 阶段验证，平台消费另走 T4。

## 重要目录

```text
business/
├── docker-compose.yml
├── .env.example
├── scripts/            # start、stop、restart
├── mysql/              # MySQL 测试数据与 CDC 用户初始化
├── oceanbase/          # OceanBase CE 幂等样例数据初始化
├── tidb/               # TiDB 幂等样例数据初始化
├── opengauss/          # openGauss 幂等样例数据初始化
├── gaussdb/            # GaussDB 官方介质镜像、持久化启动和幂等样例
├── kingbase/           # KingbaseES 幂等样例数据初始化
├── dameng/             # DM8 ARM64 官方介质技术夹具
├── oracle/             # Oracle 普通表与 Spatial 样例数据
├── postgres/
├── minio/
├── clickhouse/
├── mongodb/
├── doris/
├── spark/
└── neo4j/
```

## 开发规则

- 系统库和业务库必须保持隔离，不要把 ADDP 元数据表写入业务库。
- 修改业务库端口、账号或容器名时，同步检查 `docs/spec/addp配置介绍.md`、`docs/spec/addp端口分配.md` 和依赖该业务源的测试数据说明。
- 业务库脚本应保持幂等，可重复启动、停止和重启。
- MySQL CDC 必须使用 `MYSQL_CDC_USER` 专用账号；`scripts/start.sh -mysql` 每次在数据库 ready 后执行 `mysql/init-cdc.sh`，确保已有 volume 也能补齐账号、轮换密码并收敛最小权限。
- Oracle 必须使用保留 Spatial/Locator 的常规镜像，不得使用会卸载 Spatial 的 `-slim` 镜像；`scripts/start.sh -oracle` 必须幂等收敛 ARCHIVELOG、force/minimal supplemental logging、专用 common CDC 用户，以及 `CUSTOMERS`、`CUSTOMER_LOCATIONS`、`SPATIAL_FEATURES` 的 `ALL COLUMN LOGGING`。Oracle Spatial CDC 通过 Transfer-owned WKB 镜像表验证；ArcGIS SDE 不得伪装成已启用能力。
- MySQL 普通表和全二维几何族样例只通过 `mysql/test-data.sh` 显式重建；不得挂接到 `scripts/start.sh -mysql`，避免启动时破坏已有业务数据。
- Business Redpanda 必须与 Infra Kafka 分离；System Engine 使用 `BUSINESS_KAFKA_READER_USERNAME` 只读账号，不能登记 admin 或 Infra principal。
- 生产部署前必须修改 `.env` 默认密码并限制网络访问。

## 启动与验证

```bash
cd business
bash scripts/start.sh
bash scripts/start.sh -all
bash mysql/test-data.sh
bash scripts/stop.sh
```

详细命令见 `business/README.md`。

## 相关文档

- `business/README.md`
- `business/docs/QUICKSTART-CLICKHOUSE-MONGODB.md`
- `docs/spec/addp配置介绍.md`
- `docs/spec/addp端口分配.md`

Redis T4 使用 `scripts/test/online-hosted-redis-gate.sh`，物理夹具由 `business/scripts/online-redis-consumer-fixture.sh` 唯一拥有。仅接受 GitHub Hosted Linux x86_64，复用 Business 官方镜像、ACL 启动和九个样例初始化，随机回环端口及密码、tmpfs 数据，不连接已有 Redis。Business 仅输出 secret 分区内的 owner-only descriptor；System 使用独立身份经正式 API 登记，引擎注册凭据在验收前移除。Hosted T4 已通过；对应提交、运行和归档范围见 `business/README.md` 的 Business Redis 节，后续消费链路变更需重新验收。入口保持人工触发，不登记 schedule。

Elasticsearch T4 的物理夹具由 `scripts/online-elasticsearch-consumer-fixture.sh` 唯一拥有，使用 Hosted Linux x86_64、固定官方镜像、随机回环端口/密码及 tmpfs，复用 `elasticsearch/init.py`。Business 只输出 owner-only Engine descriptor；System owner 负责临时身份，正式 Engine API 负责登记。成功、失败和中断均删除本轮容器并验证零残留，不访问已有 Business 实例。Hosted T4 已通过；对应提交、运行和归档范围见 `business/README.md` 的 Elasticsearch 节，后续消费链路变更需重新验收。入口保持人工触发，不登记 schedule。

完整栅格容量 T4 的数据与物理 oracle 归 `scripts/online-raster-capacity-fixture.py`：固定公开 Sentinel-2 四个 10 米波段长度／SHA-256，完整组合像元与 mask 校验；源／目标 MinIO 使用当次 owner-only 磁盘目录，避免 tmpfs 占用干扰受限 Runtime 测量。只接受固定格式的当组目标名，验证全部 COG 像元、mask、金字塔与源哈希，逐组删除目标并检查零残留；容器与凭据随 Hosted 生命周期清理。此夹具不调用平台 API，不接管个人服务，不把样本保存到仓库；生命周期和协议回归由 `make test-raster-online-runner`，真实 GDAL oracle 由 `make test-geopython-workflow` 覆盖。
