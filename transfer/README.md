# Transfer 数据传输模块

Transfer 是 ADDP 的数据传输中枢，负责传输任务配置、执行编排、字段映射、异步 worker、checkpoint 观测、执行日志、指标和写后 Meta 扫描触发。

当前主路径采用 clean break：Transfer 不再维护私有 reader / writer 插件体系，不再兼容旧 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type` 等任务 JSON 字段。具体读写能力来自 `common/engine`、`common/format`、`common/contentio` 和 `common/engine/contentadapter`。

## 模块边界

| 层 | 职责 |
|---|---|
| Transfer | 任务 JSON、planner、policy、transform、worker、checkpoint、logs、metrics、写后 Meta scan |
| `common/engine` | 引擎连接、catalog、metadata、content read/write、table batch/session read/write、DeleteResource |
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

non-table raw copy 已形成第一版最小闭环：`document`、`media`、`unknown` 的 encoded single file/object 可按原始字节复制。raw copy 不进入 `common/format` table reader / writer，不解析正文、不抽取媒体元数据，也不做格式转换；目标写入第一版只支持 `overwrite`。

## 任务配置

任务配置存放在 `transfer.transfer_tasks.config` 中，必须使用 source / target endpoint 结构：

```json
{
  "mode": "batch",
  "source": {
    "locator": "addp://engine/1/path/public/roads?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "locator": "addp://engine/2/path/exports/roads.parquet?type=file",
    "data_type": "table",
    "representation": "encoded",
    "format": "parquet",
    "policy": {"write_mode": "overwrite"}
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

`policy.write_mode` 当前只保留 `overwrite` 和 `append`。overwrite 是 Transfer policy；删除指定资源由 `common/engine` 的 DeleteResource 能力提供。失败执行 retry 当前按 restartable 处理，从头重新入队执行；append 任务重试会被拒绝，避免重复写入。

## API

路由前缀：`/api/v1/transfer`。

常用接口：

- `GET /ping`
- `GET /engines`
- `GET /engines/:engine_id/tree`
- `GET /nodes/:node_id/children`
- `GET /tables/metadata`
- `GET /capabilities`
- `POST /tasks`
- `GET /tasks`
- `GET /tasks/:id`
- `PUT /tasks/:id`
- `DELETE /tasks/:id`
- `POST /tasks/:id/start`
- `POST /tasks/:id/stop`
- `POST /tasks/:id/pause`
- `POST /tasks/:id/resume`
- `GET /tasks/:id/executions`
- `GET /executions`
- `GET /executions/:execution_id`
- `POST /executions/:id/cancel`
- `POST /executions/:id/retry`
- `GET /executions/:id/progress`
- `GET /executions/:id/logs`

`GET /executions/:execution_id` 是 TaskProvider 标准执行详情入口，按统一 `common.task_executions.execution_id` 查询。取消、重试、进度和日志接口仍属于 Transfer 私有执行管理入口，当前按内部执行记录自增 ID 工作；后续由 Transfer 专题统一收敛。

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
