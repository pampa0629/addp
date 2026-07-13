# Transfer 模块基本概念及配置说明

更新时间：2026-07-12

本文档定义 Transfer 稳定任务配置、执行状态、bounded snapshot 主链路、PostgreSQL watermark bounded incremental 第一版规则，以及 continuous/Kafka 契约与当前实现边界。旧版顶层 `mode`、`target.policy.write_mode`、`connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段不再兼容。

## 一、核心对象

### 1.1 传输任务

任务表：`transfer.transfer_tasks`。

| 字段 | 说明 |
|---|---|
| `id` | 任务 ID。 |
| `tenant_id` | 租户 ID。 |
| `name` | 任务名称。 |
| `description` | 任务描述。 |
| `task_type` | 当前固定为 `sync`；执行主链路统一由 `config` 决定。 |
| `config` | source / target endpoint 任务配置。 |
| `schedule` | Cron 表达式；为空表示手动任务。 |
| `batch_size` | 批大小；config 内未声明时作为 planner 默认值。 |
| `enabled` | 定时任务是否启用。 |
| `auto_scan_metadata` | 成功后是否触发 Meta deep scan，默认 true。 |
| `status` | 任务状态：`idle`、`running`。 |
| `progress` | 任务进度百分比。 |

任务状态只表达当前任务是否正在运行；成功或失败等结果在统一执行记录中查看。

### 1.2 执行记录

Transfer 执行记录使用统一表 `common.task_executions`。Transfer API 会将统一执行记录投影为模块 DTO。

| 字段 | 当前语义 |
|---|---|
| `records_read` / `records_written` | table Transfer 的行数指标；raw copy 第一版固定为 `1/1`。 |
| `bytes_read` / `bytes_written` | 字节指标；table Transfer 当前通常不作为主指标，raw copy 第一版会写入。 |
| `checkpoint_offset` | 从 execution metadata 投影出来的观测偏移，table Transfer 当前为累计读取记录数，raw copy 第一版完成后为 `1`。 |
| `checkpoint_state` | 从 execution metadata 投影出来的 checkpoint JSON，包含 batch、进度和 provider marker。 |
| `logs` | 从 error details 中投影出来的简短执行日志。 |

snapshot checkpoint 用于 progress / diagnostics，不表示可从 checkpoint 自动续写。watermark incremental 使用独立同步主状态 resume。失败 snapshot retry 走 restartable：创建新 execution 并从头重新执行；append 任务 retry 会被拒绝。

## 二、任务 Config JSON

`config` 必须包含 `source` 和 `target` endpoint：

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
    "name": "roads.csv",
    "data_type": "table",
    "representation": "encoded",
    "format": "csv",
    "options": {"header": true},
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [],
  "batch_size": 10000
}
```

### 2.1 顶层字段

| 字段 | 必填 | 说明 |
|---|---:|---|
| `runtime` | 是 | 执行边界；当前 worker 只支持 `boundary=bounded`。 |
| `load` | 是 | 装载方式；支持 `mode=snapshot` 和 PostgreSQL native table 的 `mode=incremental + change_detection.type=watermark`。 |
| `source` | 是 | 源 endpoint。 |
| `target` | 是 | 目标 endpoint。 |
| `transforms` | 否 | table batch transform 列表。 |
| `batch_size` | 否 | 本任务批大小；为空时使用任务表 `batch_size`。 |

### 2.2 Endpoint 字段

| 字段 | 必填 | 说明 |
|---|---:|---|
| `locator` | source 必填 | source 使用的 ResourceLocator URI，指向已存在资源。 |
| `parent_locator` | target 必填 | target 父 node 的 ResourceLocator URI，指向已存在 schema / directory / bucket / prefix 等父节点。 |
| `name` | target 必填 | 父 node 下待创建或待覆盖的目标资源名。 |
| `data_type` | 是 | table Transfer 使用 `table`；raw copy 第一版支持 `document`、`media`、`cad`、`unknown`。 |
| `representation` | 是 | `native` 或 `encoded`。 |
| `format` | encoded 必填 | encoded endpoint 的格式，如 `csv`、`json`、`geojson`、`parquet`、`shapefile`。 |
| `options` | 否 | 格式或读取写入选项。 |
| `policy` | target 必填 | 目标应用策略；必须声明 `apply_mode`，upsert 还必须声明 `keys`。 |

`locator` 示例：

| 类型 | 示例 |
|---|---|
| `table` | `addp://engine/1/path/public/roads?type=table` |
| `file` | `addp://engine/2/path/exports/roads.csv?type=file` |
| `object` | `addp://engine/3/path/bucket/exports/roads.csv?type=object` |
| 已入库 source item | `addp://engine/3/path/bucket/roads.shp?type=object&item_id=12` |

## 三、table Transfer 支持范围

### 3.1 Endpoint 组合

| source | target | 状态 |
|---|---|---|
| native table | native table | 已接入统一 table reader / writer 链路。 |
| native table | encoded file/object | 已接入。 |
| encoded file/object | native table | 已接入。 |
| encoded file/object | encoded file/object | 已接入。 |
| encoded whole scope | encoded / native table target | Parquet dataset 已接入。 |
| encoded multi refs | encoded / native table target | Shapefile 已接入。 |

Transfer 不为 PostgreSQL -> PostgreSQL、NFS -> MinIO、MinIO -> NFS 等具体引擎组合维护专用链路。

### 3.2 格式

| 格式 | 支持形态 |
|---|---|
| CSV / TSV | single table read / write。 |
| JSON / JSONL | single table read / write；JSONL 是 `json` 的用户侧编码变体。 |
| GeoJSON | single table read / write；独立 `format=geojson`，空间事实由解析结果表达为 `capabilities.spatial`。 |
| Parquet | single table read / write；whole scope dataset read；支持 field_selection 下推。 |
| Shapefile | multi table read / write；完整 refs 由 format specs 或 Meta attributes 提供。 |

### 3.3 Native table target

| 引擎 | 当前写侧 |
|---|---|
| PostgreSQL | create / ensure table、schema evolution、COPY session、batch insert、空间字段写入。 |
| MySQL | create database/table、安全缺失字段追加、事务内批量 insert。 |
| Doris | DUPLICATE KEY 明细模型、MySQL 协议批量 insert；空间字段暂拒绝。 |
| ClickHouse | MergeTree 普通表、批量 insert；生成列写入跳过，空间字段暂拒绝。 |

### 3.4 空间字段与 CRS 边界

Transfer 在 table 链路中只传递空间事实，不提供通用 CRS transform 能力。

规则：

- 表结构中的空间字段由 `datatype.FieldInfo` 表达。
- 空间字段、几何类型、SRID / CRS、dimension、extent 等横切事实由 `datatype.SpatialInfo` 表达。
- encoded Shapefile / GeoJSON 等空间源写入 native table target 时，Transfer 将 `SpatialInfo` 传给目标 writer / preparer，用于创建 geometry column、typmod、SRID 等目标结构事实。
- `ewkb` 行值可以携带 SRID，但不能替代 `SpatialInfo` 中的 schema 级 CRS 事实。
- Transfer 不在普通 table copy / import / export 链路中隐式执行 CRS transform。
- 批量 CRS transform 属于计算 / ETL 能力，应由 PostGIS、Python/Spark 工作流或后续明确的空间转换算子承担。

Shapefile 写出 `.prj` 时，`WriteOptions.ExtraParams["crs_definition"]` 必须是 CRS 定义文本，例如 WKT、ESRI WKT 或 proj4 文本；不得传入裸 `EPSG:<code>`。CRS ID 应进入 `SpatialInfo` / `capabilities.spatial.crs_ref`，定义文本应进入 `crs_definitions[].definition`。

## 四、raw copy 支持范围

raw copy 是 non-table encoded single content 的原始字节复制。它不调用 `common/format` 的 table reader / writer，不解析文档、不抽取媒体元数据，也不做格式转换。

第一版只支持以下 endpoint：

| 维度 | 支持范围 |
|---|---|
| source / target `data_type` | `document`、`media`、`cad`、`unknown` |
| source / target `representation` | `encoded` |
| source locator `type` | `file`、`object` |
| target `parent_locator` type | `directory`、`root`、`bucket`、`prefix`、`service` |
| source format layout | `single` |
| target `data_type` / `format` | 可省略并继承 source；显式声明时必须一致 |
| target path | 必须是完整 file / object 路径 |
| target `policy.apply_mode` | 只支持 `replace` |

配置示例：

```json
{
  "runtime": {"boundary": "bounded"},
  "load": {"mode": "snapshot"},
  "source": {
    "locator": "addp://engine/1/path/raw/docs/report.pdf?type=object",
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/archive?type=directory",
    "name": "report.pdf",
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf",
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [],
  "batch_size": 1
}
```

## 五、field_mapping transform

字段映射写入 `config.transforms[]`：

```json
{
  "type": "field_mapping",
  "version": "v1",
  "mode": "project",
  "fields": [
    {"source": "name", "target": "road_name", "target_type": "string"},
    {"source": "geom", "target": "geometry", "target_type": "geometry", "nullable": false},
    {"target": "created_by", "target_type": "string", "default": "transfer"}
  ]
}
```

| 字段 | 必填 | 说明 |
|---|---:|---|
| `source` | 否 | 源字段名；为空表示常量 / 默认字段。 |
| `target` | 是 | 目标字段名。 |
| `target_type` | 否 | 目标字段类型。 |
| `nullable` | 否 | 是否可空；默认 true。 |
| `default` | 否 | 源字段缺失或值为 nil 时使用的默认值。 |
| `format` | 否 | 日期、时间、数字等简单解析 / 格式化提示。 |

`mode`：

| mode | 语义 |
|---|---|
| `project` | 只输出 fields 声明的目标字段；可推导 source `field_selection`。 |
| `passthrough` | 保留源 row 全字段，再应用字段映射覆盖 / 新增目标字段；不下推 `field_selection`。 |

旧任务外层 `mappings` / `field_mappings` 不作为新执行主链路输入；相关表和独立 mappings API 已删除，不能成为新的配置来源。

## 六、写入策略

`policy.apply_mode`：

| 值 | 说明 |
|---|---|
| `replace` | Transfer 写入前清理目标资源或让 prepare 重建目标。 |
| `append` | 追加写入；失败 retry 当前拒绝 append，避免重复写入。 |
| `upsert` | 按稳定键幂等新增或更新；第一版只支持 PostgreSQL native table 目标。 |
| `upsert_delete` | 为后续完整 CDC 保留；当前拒绝。 |

apply mode 是 Transfer policy；真实 upsert/delete 能力必须由目标 engine Provider 和 capability 声明。raw copy 第一版只支持 `replace`，并要求目标 engine 提供删除资源能力。

## 七、PostgreSQL watermark bounded incremental

第一版唯一支持组合为：

```text
PostgreSQL native table -> PostgreSQL native table
bounded + incremental + watermark + upsert
```

配置必须声明 `load.change_detection.field`、非空 `tie_breaker`、`start=committed`、`end=execution_upper_bound`，并在 `target.policy.keys` 声明稳定目标键。watermark 字段不得为 NULL；tie breaker 必须唯一、稳定且不可变。每次 execution 在 PostgreSQL 一致性只读事务内冻结复合上界，只读取 `(committed_position, execution_upper_bound]` 并稳定排序。

同步主状态存储在 `transfer.sync_states`。position 使用 `type=watermark`、`version=v1` 的 JSON；目标批次提交成功后才允许携带 `state_version` 和本次 fencing token 做 CAS 更新。重复应用必须由 PostgreSQL `ON CONFLICT ... DO UPDATE` 幂等吸收。

第一版只支持 resume：新 execution 从 committed position 继续并在成功后推进主状态。不提供 replay，不发现物理删除，也不支持只读副本 lookback。源表所有 insert/update 必须可靠更新 watermark；时间回拨或未更新 watermark 的变化不在保证范围内。

## 八、Continuous/Kafka v1 契约与当前实现边界

第一条 continuous 实现路径固定为业务 Kafka keyed JSON record -> PostgreSQL native table upsert。业务 Kafka 作为 System Engine 暴露 `service -> topic` catalog；用户选择 `type=topic` locator，partition 不进入资源树或 locator。Infra Kafka 是后续 CDC 的内部实现，不进入公开任务配置。

Kafka Provider 通过 `ChangeStreamReaderProvider` 返回原始 ChangeRecord 和 per-partition provider position；Transfer adapter 只接受 JSON object，从 value 的显式非空字段提取稳定 key，并归一化为 `operation=upsert` ChangeEvent。任务必须提供完整 `field_mapping` 固定目标 schema，source key 映射后必须与目标 keys 一一对应。

目标只允许 PostgreSQL `PartitionedTableChangeApplyProvider`。每个 task 由服务端生成不可变 `apply_identity`；Provider 在业务目标库维护 `addp_transfer.apply_positions`，把单 partition 的目标 upsert 与 `next_offset` 在同一事务提交。poll batch 必须先按 partition 拆分；同一批中相同目标 key 的有效记录保留最高 offset 的最后状态。普通 `TableUpsertProvider`、Infra state CAS 和 runtime lease 都不能替代该目标侧原子应用契约。

每个 partition 的主状态继续存储在 `transfer.sync_states`，position 固定为 `type=kafka_offset`、`version=v1`、`next_offset`。consumer auto commit 禁用；目标提交后才允许以 runtime fencing token + state version 做 CAS。首次无状态 partition 必须显式选择 `earliest|latest`。

continuous worker 是 Transfer 独立进程角色，不使用 Asynq。`desired_state`、原子 start/pause/resume/stop、session owner/lease/heartbeat/fencing、supervisor 和 Kafka JSON -> PostgreSQL 生产数据循环已经实现；每次启动或恢复创建新 execution。pause/stop 会先取消 runtime，等待 source/target 关闭，再结束 execution 和释放 lease。consumer group 只负责 partition assignment，Kafka auto commit 禁用；交付保证是 at-least-once + 目标 monotonic 幂等应用，不宣称分布式 exactly-once。Console Wizard 已支持 topic、JSON 字段、记录唯一标识、首次读取范围和 PostgreSQL 目标配置；execution 详情展示 owner、heartbeat、lease、fencing token、每个 partition 的 committed next offset 与最近提交时间。第一版不支持无 key append、Debezium、CDC、DLQ、Schema Registry、Avro、Protobuf、Kafka target、replay 和物理删除。

resume 前必须确认 committed `next_offset` 仍在 Kafka partition 的保留范围内。若 retention 已删除该位置，任务明确失败并要求人工决定如何处理，不能自动跳到 earliest。PostgreSQL 目标锁等待必须响应 context 取消；取消事务不得留下业务行或目标 ledger 的半提交状态。未知 JSON 字段、缺失必填字段和类型不兼容继续按严格 schema drift 策略使当前 execution 失败。

continuous worker 每隔 `TRANSFER_CONTINUOUS_DIAGNOSTICS_INTERVAL`（默认 `15s`）读取每分区 earliest/latest offset，用目标已成功应用的 committed `next_offset` 计算 lag 和 retention 恢复余量。时间余量使用连续 latest 样本的增长率估算；冷启动、无 committed position 或写入速率为零时显示 unknown。默认 degraded/critical 阈值由 `TRANSFER_CONTINUOUS_RETENTION_DEGRADED_HORIZON=6h` 和 `TRANSFER_CONTINUOUS_RETENTION_CRITICAL_HORIZON=1h` 统一控制，不进入任务 JSON。诊断结果写入 `common.task_executions.metadata.continuous.diagnostics`，Monitor 只读取 execution metadata，不直连业务 Kafka。

## 九、Checkpoint、进度和重试

当前 checkpoint 语义：

- 每个成功写入 batch 后更新执行进度。
- `checkpoint_offset` 等于累计 `records_read`；raw copy 第一版完成后为 `1`。
- `checkpoint_state` 可包含 `batch_index`、`source_offset`、`records_read`、`records_written`、`resume_marker`、`commit_marker` 等。
- marker 由 provider 生成并解释；Transfer 只保存和展示，不解析 marker 内部字段。

恢复分级：

| 等级 | 当前状态 |
|---|---|
| observable | 已支持，用于进度展示和故障定位。 |
| restartable | 已支持 retry 从头重跑；append 拒绝。 |
| resumable | PostgreSQL watermark incremental 通过 `transfer.sync_states` 支持 execution 间 resume；snapshot checkpoint 仍仅可观测。 |

## 十、写后 Meta 扫描

成功写入后，如果 `auto_scan_metadata=true`，Transfer 触发 Meta deep scan。

Transfer 不直接推导目标文件 attributes。GeoJSON 导出目标使用独立 `format=geojson` 写出；写后扫描由 Meta 按统一格式探测和 GeoJSON provider 解析目标内容，负责写入 `type_info.table`、`format_info.geojson` 和实际存在的 `capabilities.spatial`。

Transfer 只提交本次实际写出的目标边界；encoded/raw content 目标使用 `ref_groups`，不扩大为父目录扫描：

| 目标类型 | 扫描目标 |
|---|---|
| native table | schema 或 database。 |
| NFS encoded/raw file | 单文件 `ref_groups`。 |
| MinIO / S3 encoded/raw object | 单对象 `ref_groups`。 |
| Shapefile refs | 本次实际生成的 refs group，不补不存在的 sidecar refs。 |

Transfer 不直接写目标 Meta attributes。
