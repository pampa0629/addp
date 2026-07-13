# Transfer 数据传输模块

Transfer 是 ADDP 的数据传输中枢，负责传输任务配置、执行编排、字段映射、异步 worker、checkpoint 观测、执行日志、指标和写后 Meta 扫描触发。

当前已实现 bounded snapshot、PostgreSQL bounded watermark incremental，以及业务 Kafka keyed JSON -> PostgreSQL 的 continuous v1。continuous worker 已接通严格 JSON/字段映射、partitioned monotonic upsert、业务库 apply ledger、Infra `transfer.sync_states` CAS、lease/fencing 与真实 pause/resume/stop。Console Wizard 尚未开放 continuous 配置，当前需通过严格任务 JSON/API 创建；不得在 UI 中提前展示未完成的配置能力。

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

当前已经接入的 table 格式包括 CSV / TSV、JSON / JSONL、Parquet、Shapefile。native table 写侧已经接入 PostgreSQL、MySQL、Doris、ClickHouse 第一版。

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

`target.policy.apply_mode` 支持 snapshot 的 `replace` / `append`，以及 PostgreSQL watermark incremental 的幂等 `upsert`。旧 `write_mode` 不兼容。snapshot replace 失败执行可按 restartable 从头 retry；watermark upsert 从 `transfer.sync_states` 的 committed position resume；append retry 被拒绝。

continuous 第一版唯一实现路径是：业务 Kafka keyed JSON record -> Transfer continuous worker -> PostgreSQL `PartitionedTableChangeApplyProvider`。Provider 在业务目标库将 native table upsert 与 `addp_transfer.apply_positions` 的 partition `next_offset` 原子提交，避免失效 worker 写回旧状态；Infra PostgreSQL 的 `transfer.sync_states` 仍是任务 committed position 事实源。交付保证是 at-least-once + 目标 monotonic 幂等应用，不宣称分布式 exactly-once。consumer group 只负责 partition assignment，Kafka auto commit 不作为事实源。无 key append、Debezium、CDC、DLQ、Schema Registry、Avro、Protobuf、Kafka target、replay 和物理删除均后置。Infra Kafka 不进入 System engines 或用户任务配置。

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
- `GET /task-definitions/:id/executions`
- `GET /executions`
- `GET /executions/:execution_id`
- `POST /executions/:execution_id/retry`
- `GET /executions/:execution_id/progress`
- `GET /executions/:execution_id/logs`

`GET /executions/:execution_id` 是 TaskProvider 标准执行详情入口，按统一 `common.task_executions.execution_id` 查询。重试、进度和日志入口也按 `execution_id` 定位执行记录。私有 task-definition `stop` 只控制 continuous runtime；bounded worker 仍不支持真实中断，因此 TaskProvider 保持 `supports_cancel=false`，不注册标准 execution cancel endpoint。

continuous worker 是独立进程角色 `cmd/continuous-worker`，用 Infra PostgreSQL 管理任务状态，并通过 System Engine Resolver 连接业务 Kafka 与业务 PostgreSQL，不使用 Asynq。相关配置为 `TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID`、`TRANSFER_CONTINUOUS_WORKER_CAPACITY`、`TRANSFER_CONTINUOUS_LEASE_DURATION`、`TRANSFER_CONTINUOUS_HEARTBEAT_INTERVAL`、`TRANSFER_CONTINUOUS_CLAIM_INTERVAL`、`TRANSFER_CONTINUOUS_POLL_TIMEOUT` 和 `TRANSFER_CONTINUOUS_FETCH_MAX_BYTES`；heartbeat 必须小于 lease duration 的一半。worker 还要求 `SYSTEM_URL` 与 `INTERNAL_API_KEY` 可用。

Orchestrator v1 的 TaskProvider 注册使用只返回 bounded task 的 `/provider-tasks`，因此不会发现 continuous task；即使调用方持有 continuous task ID，标准 Provider execute 入口也会拒绝执行。用户侧 `/tasks` 仍查询全部任务，并可显式使用 `runtime_boundary` 过滤。

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
curl http://localhost:8083/health
```

常用验证：

```bash
cd transfer/backend
go test ./internal/planner ./internal/executor -run 'TableTransfer|Native|Encoded|Shapefile|Parquet|FieldSelection|Checkpoint|Retry' -count=1
```

## 相关文档

- [Transfer 模块说明](./CLAUDE.md)
- [Transfer 当前架构设计](./docs/design.md)
- [Transfer 基本概念及配置说明](./docs/transfer-基本概念及配置说明.md)
- [Transfer 数据库架构](./docs/数据库架构.md)
- [transfer_tasks 表](./docs/tables/tasks表.md)
- [task_executions 表](./docs/tables/task_executions表.md)
