# Transfer 模块基本概念及配置说明

更新时间：2026-05-31

本文档定义 Transfer 当前任务配置、执行状态、table Transfer 主链路和 non-table raw copy 最小闭环规则。旧版 `connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段不再兼容。

## 一、核心对象

### 1.1 传输任务

任务表：`transfer.transfer_tasks`。

| 字段 | 说明 |
|---|---|
| `id` | 任务 ID。 |
| `tenant_id` | 租户 ID。 |
| `name` | 任务名称。 |
| `description` | 任务描述。 |
| `task_type` | 当前固定为 `import`；执行主链路统一由 `config` 决定。 |
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

checkpoint 当前用于 progress / diagnostics，不表示可从 checkpoint 自动续写。失败执行 retry 走 restartable：创建新 execution 并从头重新执行；append 任务 retry 会被拒绝。

## 二、任务 Config JSON

`config` 必须包含 `source` 和 `target` endpoint：

```json
{
  "mode": "batch",
  "source": {
    "locator": "addp://engine/1/path/public/roads?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "locator": "addp://engine/2/path/exports/roads.csv?type=file",
    "data_type": "table",
    "representation": "encoded",
    "format": "csv",
    "options": {"header": true},
    "policy": {"write_mode": "overwrite"}
  },
  "transforms": [],
  "batch_size": 10000
}
```

### 2.1 顶层字段

| 字段 | 必填 | 说明 |
|---|---:|---|
| `mode` | 否 | 当前稳定值为 `batch`；为空时按 batch 处理。 |
| `source` | 是 | 源 endpoint。 |
| `target` | 是 | 目标 endpoint。 |
| `transforms` | 否 | table batch transform 列表。 |
| `batch_size` | 否 | 本任务批大小；为空时使用任务表 `batch_size`。 |

### 2.2 Endpoint 字段

| 字段 | 必填 | 说明 |
|---|---:|---|
| `locator` | 是 | common resource tree 使用的 ResourceLocator URI，包含 engine id、catalog path 和 `type`。 |
| `data_type` | 是 | table Transfer 使用 `table`；raw copy 第一版支持 `document`、`media`、`unknown`。 |
| `representation` | 是 | `native` 或 `encoded`。 |
| `format` | encoded 必填 | encoded endpoint 的格式，如 `csv`、`json`、`geojson`、`parquet`、`shapefile`。 |
| `options` | 否 | 格式或读取写入选项。 |
| `policy` | target 可选 | 目标写入策略。 |

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
| source / target `data_type` | `document`、`media`、`unknown` |
| source / target `representation` | `encoded` |
| source / target locator `type` | `file`、`object` |
| source format layout | `single` |
| target `data_type` / `format` | 可省略并继承 source；显式声明时必须一致 |
| target path | 必须是完整 file / object 路径 |
| target `policy.write_mode` | 只支持 `overwrite` |

配置示例：

```json
{
  "mode": "batch",
  "source": {
    "locator": "addp://engine/1/path/raw/docs/report.pdf?type=object",
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf"
  },
  "target": {
    "locator": "addp://engine/2/path/archive/report.pdf?type=file",
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf",
    "policy": {"write_mode": "overwrite"}
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

`policy.write_mode`：

| 值 | 说明 |
|---|---|
| `overwrite` | 默认策略。Transfer 写入前清理目标资源或让 prepare 重建目标。 |
| `append` | 追加写入；失败 retry 当前拒绝 append，避免重复写入。 |

overwrite / append 是 Transfer policy，不进入 common engine。`TableWritePreparer` 只负责 ensure / create table / schema evolution。raw copy 第一版只支持 `overwrite`，并要求目标 engine 提供删除资源能力。

## 七、Checkpoint、进度和重试

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
| resumable | 尚未进入主链路。 |

## 八、写后 Meta 扫描

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
