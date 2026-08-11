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
│   └── restart.sh                  # 重启服务
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
| 用途 | ADDP 元数据（用户、资源配置、任务定义） | 用户业务数据（上传的数据、文件） |
| 示例数据 | 用户账号、资源配置表 | Shapefile 空间数据表、用户上传文件 |

### CPU 架构自适应

启动脚本自动检测架构并选择最优镜像：

| CPU 架构 | PostgreSQL 镜像 | 性能 |
|---------|----------------|------|
| **ARM64** (Apple Silicon) | `imresamu/postgis-arm64:15-3.4` | ⚡ 原生性能 |
| **AMD64** (Intel/AMD) | `postgis/postgis:15-3.4` | ⚡ 原生性能 |

## 脚本说明

### scripts/start.sh - 启动服务

```bash
bash scripts/start.sh
```

**功能**：检查配置、检测架构、启动服务、验证健康状态、安装 PostGIS
**特性**：幂等执行（可重复运行）

`-supermap-postgresql` 启动的 `business-supermap-postgresql` 使用独立 volume 和原生 PostgreSQL 15 镜像。启动脚本会拒绝已安装 PostGIS 的实例；SuperMap `sm*` 系统表只能在 System 中通过 `SuperMap SDX+ for PostgreSQL` 高危启用入口，由 `supermap_workflow` 的 SDK 算子创建。

启用 MySQL 时，脚本还会在数据库 ready 后执行 `mysql/init-cdc.sh`。该脚本每次都创建或更新 `${MYSQL_CDC_USER:-addp_cdc}@%`，并将权限收敛为 Debezium 所需的最小权限集，因此已有数据卷也会生效。连接 MySQL CDC Engine 时使用 `.env` 中的 `MYSQL_CDC_USER` 和 `MYSQL_CDC_PASSWORD`，不要使用 root。

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
- `Point`、`LineString`、`Polygon`、`MultiPoint`、`MultiLineString`、`MultiPolygon` 和 `GeometryCollection` 均可转换为标准 WKB；
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

`bash scripts/start.sh -oracle` 会在数据库 ready 后执行 `oracle/init-cdc.sh`，幂等启用 `ARCHIVELOG`、force logging、minimal supplemental logging，并创建 `${ORACLE_CDC_USER:-C##ADDP_CDC}` LogMiner 专用 common user。`CUSTOMERS`、`CUSTOMER_LOCATIONS` 和 `SPATIAL_FEATURES` 由 `init.sql` 幂等启用 `SUPPLEMENTAL LOG DATA (ALL) COLUMNS`，分别作为普通字段、单一 Point 和混合二维几何族 CDC 样例。Oracle Engine 使用 schema owner 的 business 主账号做 Catalog/读取，并供 Transfer 创建 generation-owned Spatial WKB 镜像表、行级触发器和 DDL guard；System 的 `connection_info` 另存 `cdc_database_name`、`cdc_user` 和加密的 `cdc_password`，LogMiner 不复用业务账号或 SYS。

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
