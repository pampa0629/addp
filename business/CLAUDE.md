# Business 业务数据基础设施说明

## 模块定位

`business/` 提供 ADDP 业务数据基础设施的本地部署样例，与系统元数据库隔离。它用于承载用户业务数据、对象文件和外部数据源测试环境，不是 ADDP 系统元数据存储。

## 包含组件

- PostgreSQL/PostGIS、MySQL：业务关系库与 CDC 测试源。
- OceanBase Community Edition：国产分布式关系数据库的 MySQL 模式测试源，以独立 `engine_type=oceanbase` 注册；启动时幂等初始化探针及普通关系业务样例，支持非空间普通表的 bounded watermark source 与 prepare/session/delete/upsert 集成验证，不包含空间、CDC 或 Oracle 模式能力。
- TiDB 8.5.8：固定 PingCAP 官方 PD/TiKV/TiDB 三组件镜像与 OCI digest，以独立 `engine_type=tidb` 注册；启动时幂等初始化普通关系样例，首版只声明真实 T2 验证的非空间目录、查询、bounded watermark 与 prepare/session/delete/upsert，不包含 MySQL replication、TiCDC、空间或分区变化应用能力。
- openGauss 6.0.6 LTS：在具备 NUMA 的 Linux x86_64 主机从固定 SHA-256 的官方 Docker tar 加载，以独立 `engine_type=opengauss` 注册；macOS 不启动其含 MOT 的官方容器，实库门禁由 GitHub Actions hosted-only T2 承担；启动时幂等初始化普通关系业务样例，首版只声明经门禁验证的 PG 兼容非空间目录、查询、读取、COPY 写会话、bounded watermark 与 upsert，不包含 PostGIS、CDC 或 PostgreSQL 扩展能力。
- KingbaseES V9R1C10 `V009R001C010B0004`：Business 按宿主 CPU 只选择固定 SHA-256 的官方 x86_64 或 ARM64 Docker tar，统一加载为仓库定义的中性本地镜像引用，并以独立 `engine_type=kingbase` 注册；它作为 `business/docker-compose.yml` 的独立 `kingbase` profile，由 `business/scripts/kingbase.sh` 按 `docker compose create`、License 注入、`docker compose start` 的单一路径管理无卷 disposable 容器与幂等普通关系样例，不进入日常 `start.sh -all`。正式 T5/T2/T4 仍只在 owner-managed Linux x86_64 主机使用 x86_64 介质和 owner 正规 License。首版只声明 PG 模式下经真实门禁验证的非空间目录、查询、读取、COPY 写会话、bounded watermark 与 `ON CONFLICT` upsert，不包含 PostGIS、CDC、PostgreSQL 扩展或 openGauss `MERGE`。
- 达梦 DM8 ARM64：只使用固定 SHA-256 的鲲鹏 920/麒麟 10 SP1 官方安装介质构建本地、不可推送的无卷 disposable 技术夹具；作为独立 `dameng` profile 由 `business/scripts/dameng.sh` 管理，不进入日常 `start.sh -all`。macOS ARM64 Docker Desktop 仅承担 Linux ARM64 兼容层技术验证，官方 ODBC 与 Go 协议断言必须在容器内运行；当前不登记 `engine_type`、不创建 Engine Instance、不声明 capability，也不进入 T2/T4。
- OceanBase 跨模块 T4 只由 `scripts/online-oceanbase-consumer-fixture.sh` 管理固定源/目标表；Fixture 不创建 Engine Instance，并必须在退出路径恢复 5 行源基线和空目标表。
- TiDB 跨模块 T4 只由 `scripts/online-tidb-consumer-fixture.sh` 管理无卷三组件集群和固定源/目标表；Fixture 不创建 Engine Instance，退出路径必须删除本次 Compose project、volumes 与 orphans 并验证容器零残留。
- openGauss 跨模块 T4 的数据库生命周期只由 `scripts/online-opengauss-consumer-fixture.sh` 在 GitHub Hosted Linux x86_64 管理；Fixture 复用固定官方介质，创建当次 disposable database 和源/目标表，并输出 owner-only Engine 描述。System IAM 身份和 Engine API 注册分别由 `system/backend/cmd/online-test-fixture` 与 `scripts/test/online-engine-registration.py` 负责；Business 不调用 System API。退出时删除 owner 容器，不读取根 `.env` 或 Business `.env`。
- KingbaseES 跨模块 T4 的数据库生命周期只由 `scripts/online-kingbase-consumer-fixture.sh` 在受保护 owner-managed Linux x86_64 Runner 管理；Fixture 使用固定官方介质与 owner License 创建当次无卷容器、源/目标表和 owner-only Engine 描述，增量使用 `ON CONFLICT`。System IAM 身份和 Engine API 注册继续由通用 owner 负责；Business 不调用 System API。退出时删除容器、镜像缓存与凭据目录，并验证零残留。
- Oracle Free 23ai：普通表、Schema、Oracle Spatial、只读快照、普通表 CDC 与 Oracle Spatial CDC 测试源；ArcGIS SDE 作为后续独立能力路线预留。
- Redpanda：独立业务 Kafka API 消息流，不承载 ADDP Infra Kafka topic。
- MinIO：业务对象存储。
- ClickHouse、MongoDB、Doris、Spark：可选业务数据源和分析组件。
- Neo4j：图业务数据测试环境。

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
