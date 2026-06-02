# Transfer 当前架构设计

更新时间：2026-05-31

本文记录 Transfer 模块当前稳定架构。旧的 Transfer 私有 Reader / Writer 插件体系、ConnectorRegistry、`pkg/pipeline` 和旧任务 JSON 不再作为主路径保留。

## 一、模块定位

Transfer 负责一次数据传输任务的业务编排：

- 任务创建、更新、启动、停止、重试和执行历史查询。
- source / target endpoint 配置解析和校验。
- planner 将 endpoint 转换为可执行计划。
- executor 调用 common engine / format / contentio 完成读写。
- transform 编排，当前稳定类型为 `field_mapping`。
- checkpoint、进度、日志和指标回写。
- 写入成功后触发 Meta deep scan。

Transfer 不负责具体引擎连接、格式解析或数据类型 reader / writer 实现。

## 二、架构分层

```text
Transfer API / Frontend
  -> TaskService / ExecutionService
  -> Asynq Worker
  -> Planner
  -> Executor
  -> common/engine + common/format + common/contentio
  -> Source / Target engine
```

| 层 | 主要代码 | 职责 |
|---|---|---|
| API | `backend/internal/api` | REST API、认证后的租户上下文、请求 DTO |
| Service | `backend/internal/service` | 任务 CRUD、执行记录、重试、队列入队、Meta scan 触发 |
| Worker | `backend/internal/worker` | 从 Asynq 队列取任务并调用执行服务 |
| Planner | `backend/internal/planner` | 解析新 endpoint JSON，解析 System engine，生成 table transfer plan 或 raw copy plan |
| Executor | `backend/internal/executor` | 按计划执行 table reader / transform / writer，或执行 encoded single content 原样复制 |
| Common | `common/engine`、`common/format`、`common/contentio` | 引擎、格式、内容 I/O 和数据类型能力 |

## 三、任务配置模型

任务配置存放在 `transfer.transfer_tasks.config` 中。新配置以 source / target endpoint 为核心：

```json
{
  "mode": "batch",
  "source": {
    "engine": {"scope": "system", "id": 1},
    "resource": {
      "kind": "native_table",
      "path": {"schema": "public", "table": "roads"}
    },
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "engine": {"scope": "system", "id": 2},
    "resource": {
      "kind": "object",
      "path": {"path": "bucket/exports/roads.shp"}
    },
    "data_type": "table",
    "representation": "encoded",
    "format": "shapefile",
    "policy": {"write_mode": "overwrite"}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "name", "target": "name", "target_type": "string"},
        {"source": "geom", "target": "geometry", "target_type": "geometry"}
      ]
    }
  ],
  "batch_size": 10000
}
```

关键规则：

- `engine.id` 指向 System engine；生产链路不依赖上游硬编码 `engine.type`。
- `resource.kind` 表示资源形态，例如 `native_table`、`file`、`object`。
- `data_type` 稳定支持 `table`；`document`、`media`、`unknown` 已支持 encoded single raw copy。
- `representation` 支持 `native` 和 `encoded`。
- `format` 只用于 encoded endpoint。
- `policy.write_mode` 在 table Transfer 支持 `overwrite` 和 `append`；raw copy 第一版只支持 `overwrite`。
- 旧 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段出现即拒绝。

## 四、table Transfer 主路径

Transfer 不按具体引擎组合分叉，而是按 data type / representation / layout 选择统一链路。

```text
source endpoint
  -> table reader
  -> table batch
  -> transform
  -> table writer
  -> target endpoint
```

| source / target 形态 | 读取 / 写入来源 |
|---|---|
| native table | `common/engine` `TableReadSessionProvider`、`BatchReadableProvider`、`TableWritePreparer`、`TableWriteSessionProvider`、`BatchWritableProvider` |
| encoded single file/object | engine content provider + contentadapter + `common/format` `TableReaderProvider` / `TableWriterProvider` |
| encoded multi file/object | contentio + `[]format.RelatedRef` + `MultiTableReaderProvider` / `MultiTableWriterProvider` |
| encoded whole scope | contentio Reader / Lister + `ScopeTableReaderProvider` |

当前已接入的 table 格式：

| 格式 | Transfer 能力 |
|---|---|
| CSV / TSV | single table read / write |
| JSON / JSONL | single table read / write |
| Parquet | single table read / write；whole scope dataset read；field_selection 下推 |
| Shapefile | multi table read / write；range source 可利用 `.shx` / `.dbf` 窗口 |

当前已接入的 native table 写侧：

| 引擎 | 写侧能力 |
|---|---|
| PostgreSQL | table prepare、COPY session、batch insert、空间字段写入 |
| MySQL | table prepare、事务内批量 insert、write session |
| Doris | table prepare、DUPLICATE KEY 明细表、MySQL 协议 insert |
| ClickHouse | table prepare、MergeTree 普通表、批量 insert |

## 五、Transform

当前稳定 transform 为 `field_mapping`。它只处理 table batch 的字段级变换：

- 字段投影。
- 字段重命名。
- 默认值。
- 目标字段类型声明。
- geometry 字段 schema 同步。

`mode` 支持：

| mode | 语义 |
|---|---|
| `project` | 只输出 `fields` 声明的目标字段；planner 可由此推导 source `field_selection`。 |
| `passthrough` | 保留源 row 全字段，再应用字段映射覆盖 / 新增字段；不下推 `field_selection`。 |

过滤、表达式、聚合、空间坐标转换等 ETL 能力尚未进入稳定主链路。

## 六、raw copy 主路径

raw copy 用于 non-table encoded single content 的原始字节复制，不进入 `common/format` table reader / writer，也不做文档解析、媒体转码或格式转换。

```text
source encoded file/object
  -> engine content reader
  -> byte stream
  -> engine content writer
  -> target encoded file/object
```

第一版支持范围：

| 维度 | 支持范围 |
|---|---|
| `data_type` | `document`、`media`、`unknown` |
| `representation` | `encoded` |
| `layout` | `single` |
| `resource.kind` | `file`、`object` |
| target `data_type` / `format` | 可省略并继承 source；显式声明时必须和 source 一致 |
| target path | 必须是完整 file / object 路径 |
| `policy.write_mode` | 只支持 `overwrite` |

raw copy 的覆盖语义由 Transfer 执行：目标引擎必须提供 `ResourceDeleteProvider`，先删除目标资源再创建写入，不依赖 create 的隐式覆盖行为。

## 七、写入策略

overwrite / append 是 Transfer policy，不进入 common engine。

| 模式 | 当前语义 |
|---|---|
| `overwrite` | 写入前由 Transfer 规划删除目标资源或重建目标，再执行写入。 |
| `append` | 追加写入；失败 retry 当前拒绝 append，避免重复写。 |

`TableWritePreparer` 只做 ensure / create table / schema evolution，不承载 overwrite / append 策略。DeleteResource 是 common engine 提供的原子删除能力。

## 八、Checkpoint 和重试

当前 checkpoint 是观测点，不是可恢复提交点：

- batch 写入成功后回写 `records_read`、`records_written`、`checkpoint_offset`、`checkpoint_state` 和简短日志。
- raw copy 成功后回写 `records_read=1`、`records_written=1`、`bytes_read` 和 `bytes_written`；`checkpoint_offset=1` 表示该单个 content 已复制完成。
- `checkpoint_offset` 表示累计读取记录数，用于展示和故障定位。
- reader / writer 如实现 marker provider，Transfer 可保存 `resume_marker` / `commit_marker`。
- 保存 marker 不表示已经启用 checkpoint resumable。

失败执行 retry 当前按 restartable 处理：

1. 创建新的 execution。
2. 重新入队。
3. overwrite / 默认写入模式从头执行。
4. append 写入模式拒绝 retry。

真正的 checkpoint resumable 需要 source seek、target 幂等提交和 provider marker 消费同时满足，尚未进入主链路。

## 九、写后 Meta 扫描

任务成功后，如果 `auto_scan_metadata=true`，Transfer 触发 Meta deep scan。

| 目标 | 扫描范围 |
|---|---|
| PostgreSQL native table | 所在 schema |
| MySQL / Doris / ClickHouse native table | 所在 database |
| NFS file | 文件父目录 |
| NFS Shapefile refs | refs 所在目录 |
| MinIO / S3 object | 对象所在 bucket/prefix |
| MinIO / S3 Shapefile refs | refs 所在 bucket/prefix |

Transfer 不直接写目标 Meta attributes。目标 item 的 `data_type`、`format`、`layout`、`type_info`、`format_info` 和 capabilities 由 Meta scan 重新识别。

## 十、后续方向

table Transfer 主链路已经稳定。后续增强包括：

- `row_filter` / predicate 统一语义。
- 分区并行读取和多 worker 协调。
- checkpoint resumable。
- Doris Stream Load。
- ClickHouse 排序键 / 分区键 / 原生批量接口。
- raw copy 端到端样例任务和更完整的执行展示。
- container child table transfer。
