# Business 业务数据基础设施说明

## 模块定位

`business/` 提供 ADDP 业务数据基础设施的本地部署样例，与系统元数据库隔离。它用于承载用户业务数据、对象文件和外部数据源测试环境，不是 ADDP 系统元数据存储。

## 包含组件

- PostgreSQL/PostGIS：业务关系库。
- MinIO：业务对象存储。
- ClickHouse、MongoDB、Doris、Spark：可选业务数据源和分析组件。
- Neo4j：图业务数据测试环境。

## 重要目录

```text
business/
├── docker-compose.yml
├── .env.example
├── scripts/            # start、stop、restart
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
- 生产部署前必须修改 `.env` 默认密码并限制网络访问。

## 启动与验证

```bash
cd business
bash scripts/start.sh
bash scripts/start.sh -all
bash scripts/stop.sh
```

详细命令见 `business/README.md`。

## 相关文档

- `business/README.md`
- `business/docs/QUICKSTART-CLICKHOUSE-MONGODB.md`
- `docs/spec/addp配置介绍.md`
- `docs/spec/addp端口分配.md`
