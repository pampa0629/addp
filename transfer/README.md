# Transfer 数据传输模块

Transfer 是 ADDP 的数据传输中枢，负责传输任务配置、执行编排、字段映射、异步 worker、checkpoint 观测、执行日志、指标和写后 Meta 扫描触发。

当前已实现 bounded snapshot、PostgreSQL/MySQL bounded watermark incremental、业务 Kafka keyed JSON continuous upsert + DLQ、业务 Kafka bounded replay，以及 PostgreSQL/MySQL/Oracle 单表 CDC initial snapshot + upsert/delete。continuous worker 已接通严格 record/Debezium adapter、业务库 apply ledger、Infra `transfer.sync_states` CAS、lease/fencing、真实 pause/resume/stop，以及分区 latest offset、lag、retention horizon 和 degraded/critical 告警；Console Wizard 已开放业务 Kafka和数据库 CDC 两类 continuous 路线，数据库 CDC 目标支持 PostgreSQL/MySQL/Oracle。Oracle CDC 包含 generation-owned Spatial 镜像：XY 使用 WKB BLOB，XYZ 使用保留 Z 值的 GeoJSON CLOB，Transfer 统一归一化为标准 EWKB；Oracle 目标当前只接受 XY geometry，不支持独立 `TIME`，仍不包含普通业务 LOB、RAC 和 ArcGIS SDE。

当前主路径采用 clean break：Transfer 不再维护私有 reader / writer 插件体系，不再兼容旧 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type` 等任务 JSON 字段。具体读写能力来自 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`。

## 模块边界

| 层 | 职责 |
|---|---|
| Transfer | 任务 JSON、planner、policy、transform、worker、checkpoint、logs、metrics、写后 Meta scan |
| `common/engine` | 引擎连接、catalog、metadata、content read/write、table batch/session read/write、change stream read、DeleteResource |
| `common/format` | table reader / writer、multi table reader / writer、scope table reader、格式编码解码 |
| `common/contentio` | content `Ref`、Reader、Writer、Lister、RangeReader |
| `common/engine/contentadapter` | engine content provider 到 contentio 的适配 |

## 已稳定的 table Transfer 主链路

table 类型 Transfer 已形成统一主路径：

```text
source endpoint
  -> table reader
  -> table batch
  -> field_mapping transform
  -> table writer
  -> target endpoint
```

endpoint 只决定 reader / writer 来源：

| endpoint | 读写来源 |
|---|---|
| native table | `common/engine` table read / write session 或 batch provider |
| encoded single file/object | engine content provider + contentio + `common/format` table reader / writer |
| encoded multi file/object | contentio + `[]format.RelatedRef` + `common/format` multi table reader / writer |
| encoded whole scope | contentio reader/lister + `common/format` scope table reader |

当前已经接入的 table 格式包括 CSV / TSV、JSON / JSONL、Parquet / GeoParquet、Shapefile。Parquet writer 默认使用 ZSTD 列式压缩，可选 codec 由 `GET /capabilities` 动态声明。native table 写侧已经接入 PostgreSQL、MySQL、Doris、ClickHouse 第一版。

non-table raw copy 已形成第一版最小闭环：`document`、`media`、`cad`、`unknown` 的 encoded single file/object 可按原始字节复制。raw copy 不进入 `common/format` table reader / writer，不解析正文、不抽取媒体元数据，也不做格式转换；目标应用只支持 `replace`。

## 任务配置

任务配置存放在 `transfer.transfer_tasks.config` 中，必须使用 source / target endpoint 结构：

```json
{
  "runtime": {"boundary": "bounded"},
  "load": {"mode": "snapshot"},
  "source": {
    "locator": "addp://engine/1/path/public/roads?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/exports?type=directory",
    "name": "roads.parquet",
    "data_type": "table",
    "representation": "encoded",
    "format": "parquet",
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "name", "target": "road_name", "target_type": "string"},
        {"source": "geom", "target": "geometry", "target_type": "geometry"}
      ]
    }
  ],
  "batch_size": 10000
}
```

`target.policy.apply_mode` 支持 snapshot 的 `replace` / `append`、PostgreSQL/MySQL watermark 和业务 Kafka 的幂等 `upsert`，以及数据库 CDC 的 `upsert_delete`。旧 `write_mode` 不兼容。snapshot replace 失败执行可按 restartable 从头 retry；watermark 和未阻塞的 continuous/CDC 从 `transfer.sync_states` committed position resume；append retry 被拒绝。数据库 CDC schema drift 会把任务置为 `status=blocked` 且不推进当前 offset。PostgreSQL/MySQL 中只有当前消息新增的 nullable 非 geometry 字段可由用户逐字段审批，平台复用目标 Provider 幂等加列并把任务置为 paused；Oracle 第一期的任何 schema drift 和其他不可审批变化均只能 Stop 后创建新任务和新目标表。

continuous 当前有两类实现路径：业务 Kafka keyed JSON record -> PostgreSQL/MySQL upsert，以及 PostgreSQL/MySQL/Oracle 单表 -> Debezium -> Infra Kafka -> PostgreSQL/MySQL/Oracle snapshot/upsert/delete。Oracle Spatial 在 connector 前由 capture Provider 将源行同步到 generation-owned 镜像表：XY 使用 WKB BLOB，XYZ 使用 Oracle `TO_GEOJSON` CLOB，adapter 先校验冻结的空间事实，再输出 EWKB；之后完全复用同一条 CDC 主路径。它们复用同一个 Transfer continuous worker 和 `PartitionedTableChangeApplyProvider`；Provider 在业务目标库将目标变化与目标 apply ledger 的 `next_offset` 原子提交，Infra PostgreSQL 的 `transfer.sync_states` 保存任务 committed position。Oracle 目标 ledger 位于连接用户 schema 的 `_ADDP_TRANSFER_APPLY_POSITIONS`，并通过 ownership comment 从 Catalog 隐藏；只开放 CDC apply，不开放普通 bounded table write。业务 Kafka已支持显式 `block|dead_letter` 和显式 offset ranges 到新 PostgreSQL 隔离表的 bounded replay。交付保证是 at-least-once + 目标 monotonic 幂等应用，不宣称分布式 exactly-once。当前仍不支持无 key append、Schema Registry、Avro、Protobuf、Kafka target、数据库 CDC replay/DLQ、普通 Oracle LOB/RAC、ArcGIS SDE 或自动 schema evolution。Infra Kafka 不进入 System engines 或用户任务配置。

恢复 continuous task 时，Kafka Provider 会先验证 committed `next_offset` 是否仍处于 topic partition 的 earliest/latest 范围。位置已被 retention 清除时 execution 明确失败，不允许静默重置。PostgreSQL/MySQL/Oracle 目标锁等待响应 runtime context 取消，未完成事务会同时回滚业务表和对应目标 apply ledger。

## API

路由前缀：`/api/v1/transfer`。

常用接口：

- `GET /ping`
- 资源选择、资源树和表字段读取统一走 Meta resource-tree / item API，Transfer 不提供私有数据源树代理。
- `GET /capabilities`
- `GET /tasks`
- `GET /tasks/:task_type/:id`
- `POST /tasks/:task_type/:id/execute`
- `POST /task-definitions`
- `GET /task-definitions/statistics`
- `GET /task-definitions/:id`
- `PUT /task-definitions/:id`
- `DELETE /task-definitions/:id`
- `POST /task-definitions/:id/start`
- `POST /task-definitions/:id/pause`
- `POST /task-definitions/:id/resume`
- `POST /task-definitions/:id/stop`
- `POST /task-definitions/:id/replay`
- `GET /task-definitions/:id/schema-change`
- `POST /task-definitions/:id/schema-change/approve`
- `GET /task-definitions/:id/dead-letters`
- `GET /task-definitions/:id/dead-letters/:identity`
- `GET /task-definitions/:id/executions`
- `GET /executions`
- `GET /executions/:execution_id`
- `POST /executions/:execution_id/retry`
- `GET /executions/:execution_id/progress`
- `GET /executions/:execution_id/logs`

`GET /executions/:execution_id` 是 TaskProvider 标准执行详情入口，按统一 `common.task_executions.execution_id` 查询。重试、进度和日志入口也按 `execution_id` 定位执行记录。私有 task-definition `stop` 只控制 continuous runtime；bounded worker 仍不支持真实中断，因此 TaskProvider 保持 `supports_cancel=false`，不注册标准 execution cancel endpoint。

continuous worker 是独立进程角色 `cmd/continuous-worker`，用 Infra PostgreSQL 管理任务状态，通过 System Engine Resolver 连接业务 Engine；CDC 内部 Kafka 使用部署配置和独立 `transfer` principal，不注册为 System Engine。除 continuous runtime 配置外，CDC consumer 还使用 `INFRA_KAFKA_BOOTSTRAP_SERVERS`、`INFRA_KAFKA_TRANSFER_PASSWORD`、`INFRA_KAFKA_SECURITY_PROTOCOL` 和 TLS 配置。worker 要求 `SYSTEM_URL` 与 `TRANSFER_SERVICE_CLIENT_SECRET` 可用，并按任务 Tenant 获取短期 Service Access Token。

Orchestrator v1 的 TaskProvider 声明使用唯一标准 `/tasks` 路由；带 `task_type=sync` 的标准发现请求由服务端强制只返回 bounded task，因此不会发现 continuous task。Console 不带 `task_type` 查询全部任务，并可显式使用 `runtime_boundary` 过滤。即使调用方持有 continuous task ID，标准 Provider execute 入口也会拒绝执行；不保留 `/provider-tasks` 私有旁路。

## 启动与验证

开发启动：

```bash
bash scripts/dev/start.sh -transfer
```

后端修改后重启：

```bash
./scripts/dev/restart.sh -transfer
```

健康检查：

```bash
curl http://localhost:8083/health/ready
```

常用验证：

```bash
cd transfer/backend
go test ./internal/planner ./internal/executor -run 'TableTransfer|Native|Encoded|Shapefile|Parquet|FieldSelection|Checkpoint|Retry' -count=1
```

真实 Access/PGeo 样本识别、PGeo -> Oracle Spatial bounded 导入，以及 Oracle Spatial -> FileGDB round-trip：

```bash
make test-arcgis-open-formats
```

该门禁由仓库根目录执行，要求 GeoPython 容器和 Business Oracle 已启动，不会自行重启服务。它断言普通 Access 不误判、PGeo catalog/行读取、PGeo 写入 Oracle 的行数/Point/MultiPolygon geometry/SRID（含无 SRID 源数据），以及 Oracle Spatial 写入 FileGDB 后回读的 123 行 MultiPolygon、非空 geometry 和 SRID 0。测试只使用临时 Oracle 表和临时 `.gdb` 目录，结束自动清理。

Oracle Spatial CDC 数据面矩阵（需要 Business Oracle、Kafka Connect、Infra Kafka 和 `addp_test`）使用显式开关运行：

```bash
cd transfer/backend
ADDP_TEST_POSTGRES_DATABASE=addp_test \
ADDP_ORACLE_SPATIAL_CDC_DATA_E2E=1 \
go test ./internal/continuous -run TestIntegrationOracleSpatialCDCGeometryMatrixAndRecovery -count=1 -v
```

PostgreSQL/MySQL/Oracle 三类源到 Oracle 目标的生命周期矩阵：

```bash
cd transfer/backend
ADDP_TEST_POSTGRES_DATABASE=addp_test \
ADDP_DATABASE_CDC_ORACLE_TARGET_E2E=1 \
go test ./internal/continuous -run TestIntegrationDatabaseCDCToOracleTargetLifecycle -count=1 -v
```

追加 `ADDP_ORACLE_SPATIAL_CDC_CONTAINER_FAULT=1` 才会暂停并恢复 `business-oracle` 容器验证 Oracle 中断；测试结束会自动 unpause，普通回归不启用该开关。

## 相关文档

- [Transfer 模块说明](./CLAUDE.md)
- [Transfer 当前架构设计](./docs/design.md)
- [Transfer 基本概念及配置说明](./docs/transfer-基本概念及配置说明.md)
- [Transfer 数据库架构](./docs/数据库架构.md)
- [transfer_tasks 表](./docs/tables/tasks表.md)
- [task_executions 表](./docs/tables/task_executions表.md)
