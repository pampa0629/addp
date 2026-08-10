# Business 业务数据基础设施说明

## 模块定位

`business/` 提供 ADDP 业务数据基础设施的本地部署样例，与系统元数据库隔离。它用于承载用户业务数据、对象文件和外部数据源测试环境，不是 ADDP 系统元数据存储。

## 包含组件

- PostgreSQL/PostGIS、MySQL：业务关系库与 CDC 测试源。
- Oracle Free 23ai：普通表、Schema、只读快照读取测试源；CDC 和 ArcGIS SDE 作为后续独立能力路线预留。
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
├── oracle/             # Oracle 普通表样例数据
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
- Oracle 第一期只创建普通业务用户和只读样例表；不得把 Oracle CDC、LogMiner、ArcGIS SDE 或 `SDO_GEOMETRY` 伪装成已启用能力。
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
