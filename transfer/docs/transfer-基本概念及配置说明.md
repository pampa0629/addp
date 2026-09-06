# Transfer 模块基本概念及配置说明

更新时间：2026-08-31

本文档定义 Transfer 稳定任务配置、执行状态、bounded snapshot 主链路、watermark bounded incremental 规则，以及 continuous/Kafka 契约与当前实现边界。旧版顶层 `mode`、`target.policy.write_mode`、`connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段不再兼容。

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
| `status` | 任务实际状态：`idle`、`running`、`blocked`；`blocked` 当前用于数据库 CDC schema drift，审批成功后收敛为 `idle`。 |
| `progress` | 任务进度百分比。 |

任务状态只表达当前任务是否正在运行；成功或失败等结果在统一执行记录中查看。

### 1.2 执行记录

Transfer 执行记录使用统一表 `common.task_executions`。Transfer API 会将统一执行记录投影为模块 DTO。

可复用同步配置使用 `transfer.transfer_tasks`；Manager 数据导出、Develop 查询完整结果导出等一次性动作通过 `POST /api/v1/transfer/executions` 直接创建 bounded `sync` ad-hoc execution，`source_task_id` 为空。该入口不创建临时任务定义，调用方只持有统一 `execution_id`。模块 Runtime 使用 `GET /api/v1/transfer/executions/{execution_id}/result` 回查自己创建的一次性 execution；Transfer 从 Service Client 身份再次推导来源模块并与 execution `source` 精确匹配，其他来源一律按不存在处理。请求使用强类型 source / target / fields 契约，Transfer 在内部生成唯一 planner 配置并继续使用相同 worker、checkpoint、保护和输出主链路。查询 source 可在 `query.inputs[]` 提交本次执行已经解析完成的关系输入 `{name, locator}`；Transfer 只接受与 source locator 同 Engine 的标准 ResourceLocator，并使用这些输入生成完整执行血缘，不解析查询文本反推资源。Develop 的 CSV 查询导出必须按参数名稳定排序提交全部有效关系输入，并把冻结结果的有序列名提交为同名 `field_mapping` 投影；`target_type=unknown` 表示保留 Provider 返回值且不做类型转换，不能用有界预览样本猜测类型。

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
| `load` | 是 | 装载方式；支持 `mode=snapshot` 和 PostgreSQL/MySQL native table 的 `mode=incremental + change_detection.type=watermark`。 |
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
| `data_type` | 是 | table Transfer 使用 `table`；动态 schema collection 原始记录导出使用 `unknown`；raw copy 第一版支持 `document`、`media`、`cad`、`unknown`。 |
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

### 2.3 Bounded query source 与 MongoDB 结构整形

`bounded + native table` source 可以声明 `source.query`，由源引擎的 `QueryReadSessionProvider` 执行只读查询，Transfer 仍通过统一 table reader / writer 主链路分批搬运查询结果：

```json
{
  "source": {
    "locator": "addp://engine/11/path/sales/orders?type=table",
    "data_type": "table",
    "representation": "native",
    "query": {
      "language": "mql",
      "statement": "{\"aggregate\":\"orders\",\"pipeline\":[{\"$unwind\":{\"path\":\"$items\",\"includeArrayIndex\":\"items__index\",\"preserveNullAndEmptyArrays\":false}},{\"$project\":{\"_id\":\"$_id\",\"items__sku\":{\"$ifNull\":[\"$items.sku\",null]},\"items__index\":1}}]}"
    }
  }
}
```

MongoDB 控制台提供一个通用结构整形构建器，当前覆盖两类基数模型：

- 一条源文档生成一行：自动携带 Meta 识别出的文档标识，并选择需要进入关系行的文档字段。
- 一个数组元素生成一行：只能选择一个 Meta 识别出的数组字段，可以选择该数组下的多个元素叶子字段，也可以选择多个不位于任何数组下的父文档叶子字段随每个元素行重复携带；父文档标识自动携带，并可选输出数组序号。空数组和缺失数组固定生成零行。

构建器只是一种 MQL 编写方式，不是新的任务 DSL 或执行路径。保存到任务中的唯一事实仍是 `source.query.language=mql` 和标准 MQL command object；基础构建器只生成 `可选单次 $unwind -> $project`，不生成筛选和排序。编辑已有任务时，只有严格属于该子集且使用系统确定性查询输出名的语句才反向显示为结构化表单，其他合法 MQL 统一进入高级编辑器。字段候选来自 Meta 已扫描的字段路径和数组类型，数组字段禁止手工输入；构建器不得内置业务库、collection、字段名或目标 schema。

基础构建器不暴露 MQL 的 `$match`、`$sort`、`$ifNull`、投影别名和 `preserveNullAndEmptyArrays` 等实现细节，不提供递归自动摊平，不猜测多个数组之间的业务粒度，也不开放 `$group`、`$lookup`、`$unionWith` 等业务计算。父文档随行字段只是同一文档 `$project` 的上下文复制，不是关联、聚合或跨 Collection 读取。需要其他只读 MQL 能力时使用高级编辑器；指标、标准化、维度和事实加工仍属于 Develop。

结构整形与 PostgreSQL 字段映射的职责必须分离：结构整形只决定“一行代表什么”和“哪些 MongoDB 源字段进入查询结果”；编译器为查询结果自动生成确定性、无点号的内部字段名。下一步 `field_mapping` 只展示实际查询输出，不展示其余 MongoDB 原始字段，并负责 PostgreSQL 目标字段名、类型、可空性等目标定义。数组展开的父文档标识属于关系行必需来源，基础模式自动携带且不得从映射中删除；`activity_id` 等业务目标名只在 `field_mapping` 中声明。

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
| native dynamic records | encoded file/object | 已接入 `mongodb_extended_jsonl` 原始记录导出；不经过 table pipeline。 |

Transfer 不为 PostgreSQL -> PostgreSQL、NFS -> MinIO、MinIO -> NFS 等具体引擎组合维护专用链路。

### 3.2 动态记录原始导出

动态 schema collection 的原始记录导出使用 `source representation=native + data_type=unknown` 到 `target representation=encoded + format=mongodb_extended_jsonl` 的唯一任务形态。Transfer 消费源引擎 `EncodedRecordReadSessionProvider` 返回的类型保真批次，再通过目标 `ContentWritableProvider` 流式写出；不得复用 `raw copy`，因为源 collection 没有可直接复制的单一内容字节流。

MongoDB 第一版固定输出 Canonical Extended JSON v2、UTF-8、每行一个紧凑文档，目标扩展名为 `.ejsonl`。该格式用于记录交换，不是 BSON archive 或数据库备份。任务不接受 transforms、query、筛选、排序或用户可选编码模式；表格化导出后续复用 query source 到 encoded table target 的独立主路径。

### 3.3 格式

| 格式 | 支持形态 |
|---|---|
| CSV / TSV | single table read / write。 |
| JSON / JSONL | single table read / write；JSONL 是 `json` 的用户侧编码变体。 |
| GeoJSON | single table read / write；独立 `format=geojson`，空间事实由解析结果表达为 `capabilities.spatial`。 |
| Parquet | single table read / write；whole scope dataset read；支持 field_selection 下推；普通 Parquet 与 GeoParquet 共用列式压缩配置。 |
| Shapefile | multi table read / write；完整 refs 由 format specs 或 Meta attributes 提供。 |

Parquet writer 的列式压缩通过 `target.options.compression` 配置，该值作用于所有列的 Column Chunk / Data Page，不是文件封装压缩，也不是 Parquet value encoding。当前契约为：

- 默认：`zstd`。
- 支持：`zstd`、`snappy`、`lz4_raw`、`brotli`、`gzip`、`uncompressed`。
- 普通 Parquet 和 GeoParquet 使用同一份契约；当源数据含空间事实时，压缩后的 WKB 几何列和 `geo` metadata 一起写出。
- 不开放压缩级别和逐列 codec；未声明的 codec 直接报错。

`GET /api/v1/transfer/capabilities` 的 `table_formats[].columnar_compression` 是唯一能力事实源，包含 `default` 和 canonical `codecs`。控制台必须消费该声明，不按格式名硬编码列表。

```json
{
  "target": {
    "format": "parquet",
    "options": {
      "compression": "zstd"
    }
  }
}
```

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
    {"source": "amount", "target": "amount", "target_type": "decimal", "precision": 18, "scale": 4},
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
| `precision` | 否 | decimal 目标的总有效位数；必须为正整数。 |
| `scale` | 否 | decimal 目标的小数位数；必须为非负整数且不大于 `precision`。 |
| `nullable` | 否 | 是否可空；默认 true。 |
| `default` | 否 | 源字段缺失或值为 nil 时使用的默认值。 |
| `format` | 否 | 日期、时间、数字等简单解析 / 格式化提示。 |

`precision` / `scale` 只属于 decimal 目标字段，两者必须同时出现或同时省略。源字段已声明有限精度时，向导默认继承该事实；PostgreSQL 无精度限制的 `numeric` 写入 MySQL 这类只支持有界 decimal 的目标时，必须在映射中显式填写。MySQL 要求 `1 <= precision <= 65`、`0 <= scale <= 30` 且 `scale <= precision`。系统不扫描数据猜测表结构，也不把无界 decimal 静默收缩为默认精度。

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
| `upsert` | 按稳定键幂等新增或更新；当前支持声明幂等 upsert 能力的 PostgreSQL、MySQL 与非空间 OceanBase native table 目标。 |
| `upsert_delete` | 数据库 CDC v1 按稳定键新增、更新和物理删除；与目标 partition ledger 原子提交。 |

apply mode 是 Transfer policy；真实 upsert/delete 能力必须由目标 engine Provider 和 capability 声明。raw copy 第一版只支持 `replace`，并要求目标 engine 提供删除资源能力。

## 七、Watermark bounded incremental

当前支持组合为：

```text
PostgreSQL/MySQL native table -> PostgreSQL/MySQL/OceanBase native table
bounded + incremental + watermark + upsert
```

配置必须声明 `load.change_detection.field`、非空 `tie_breaker`、`start=committed`、`end=execution_upper_bound`，并在 `target.policy.keys` 声明稳定目标键。watermark 字段不得为 NULL；tie breaker 必须精确匹配非空主键或唯一约束，并且稳定、不可变。每次 execution 在源数据库的一致性快照内冻结复合上界，只读取 `(committed_position, execution_upper_bound]` 并稳定排序；MySQL 源必须是 InnoDB 基表，且当前不得包含空间字段。

同步主状态存储在 `transfer.sync_states`。position 使用 `type=watermark`、`version=v1` 的 JSON；目标批次提交成功后才允许携带 `state_version` 和本次 fencing token 做 CAS 更新。重复应用必须由目标 `TableUpsertProvider` 幂等吸收：PostgreSQL 使用 `ON CONFLICT ... DO UPDATE`，MySQL 及 MySQL 模式 OceanBase 使用 InnoDB 事务内的 `ON DUPLICATE KEY UPDATE`。MySQL-compatible 目标的配置 keys 必须精确匹配非空主键或唯一约束，且目标表不得存在与配置 keys 不同的唯一约束；OceanBase 当前只支持非空间目标。

第一版只支持 resume：新 execution 从 committed position 继续并在成功后推进主状态。不提供 replay，不发现物理删除，也不支持只读副本 lookback。源表所有 insert/update 必须可靠更新 watermark；时间回拨或未更新 watermark 的变化不在保证范围内。

## 八、Continuous/Kafka v1 契约与当前实现边界

第一条 continuous 实现路径为业务 Kafka keyed JSON record -> PostgreSQL/MySQL native table upsert。业务 Kafka 作为 System Engine 暴露 `service -> topic` catalog；用户选择 `type=topic` locator，partition 不进入资源树或 locator。Infra Kafka 是数据库 CDC 的内部实现，不进入公开任务配置。

Kafka Provider 通过 `ChangeStreamReaderProvider` 返回原始 ChangeRecord 和 per-partition provider position；Transfer adapter 只接受 JSON object，从 value 的显式非空字段提取稳定 key，并归一化为 `operation=upsert` ChangeEvent。任务必须提供完整 `field_mapping` 固定目标 schema，source key 映射后必须与目标 keys 一一对应，并显式提交 `runtime.record_failure.mode=block|dead_letter`，不得依赖字段省略猜默认值。

任务创建或编辑时可以显式请求 Manager 对 Topic 当前保留范围尾部做一次有界预览，并从返回的 JSON object 样本生成顶层字段、目标类型和可空性的候选建议。样本建议不是 Topic schema，不写入 Meta attributes，也不能自动进入任务配置；用户必须在确认对话框中检查建议，确认后才合并为正式 `field_mapping`。已有手工映射始终保留，样本未覆盖的字段不能据此删除。运行时仍只认已确认的完整映射，并继续按未知字段、缺失必填字段和类型不匹配的严格规则处理记录。

目标必须声明原子、单调且覆盖任务所需 operation 的 `PartitionedTableChangeApplyProvider`，当前支持 PostgreSQL、MySQL 与 Oracle。每个 task 由服务端生成不可变 `apply_identity`；PostgreSQL Provider 在业务目标库维护 `addp_transfer.apply_positions`，MySQL Provider 在目标业务数据库维护 `_addp_transfer_apply_positions` InnoDB 私有表，Oracle Provider 在连接用户 schema 内维护 `_ADDP_TRANSFER_APPLY_POSITIONS`，并把单 partition 的目标变更与 `next_offset` 在同一事务提交。MySQL 不跨数据库创建私有账本；Oracle 目标 schema 必须等于连接用户，ledger 通过 ownership comment 从 Catalog 隐藏。任一同名 ledger 结构不符合唯一规范时直接失败。poll batch 必须先按 partition 拆分；同一批中相同目标 key 的有效记录保留最高 offset 的最后状态。普通 `TableUpsertProvider`、Infra state CAS 和 runtime lease 都不能替代该目标侧原子应用契约。

每个 partition 的主状态继续存储在 `transfer.sync_states`，position 固定为 `type=kafka_offset`、`version=v1`、`next_offset`。consumer auto commit 禁用；目标提交后才允许以 runtime fencing token + state version 做 CAS。首次无状态 partition 必须显式选择 `earliest|latest`。

continuous worker 是 Transfer 独立进程角色，不使用 Asynq。`desired_state`、原子 start/pause/resume/stop、session owner/lease/heartbeat/fencing、supervisor、业务 Kafka JSON upsert + DLQ 和 PostgreSQL/MySQL/Oracle CDC snapshot/upsert/delete 已实现。业务 Kafka与数据库 CDC 共用同一个 consumer/apply/CAS 主循环；Oracle Spatial 由 capture Provider 维护 generation-owned WKB 镜像表后进入相同 Debezium 数据面。consumer group 只负责 partition assignment，Kafka auto commit 禁用。交付保证是 at-least-once + 目标 monotonic 幂等应用，不宣称分布式 exactly-once。Console Wizard 已支持业务 Kafka 以及 PostgreSQL/MySQL/Oracle CDC 配置，业务 Kafka 默认显式发送 `block`；公开 API 可显式选择 `dead_letter`。execution 详情展示 owner、heartbeat、lease、fencing token、每个 partition 的 committed next offset 与最近提交时间。当前仍不支持无 key append、Schema Registry、Avro、Protobuf、Kafka target、数据库 CDC replay/DLQ、普通 Oracle LOB/RAC、ArcGIS SDE 或自动 schema evolution。

`dead_letter` 只处理业务 Kafka JSON 解码、字段/key 校验和类型转换阶段的确定性记录错误。处理顺序固定为 DLQ payload -> `transfer.dead_letters` 控制索引 -> 目标 `skip` ledger -> `transfer.sync_states` CAS；source/poll、目标、fencing、retention 和 Infra 故障仍严格阻塞。DLQ topic 不是 replay source。

业务 Kafka bounded replay 使用唯一 `POST /task-definitions/:id/replay`。请求只能提交显式 per-partition `[start_offset,end_offset)` 与新 PostgreSQL 目标的 `parent_locator + name`；source、mapping、key、policy 和原目标全部继承 owner task 且不可覆盖。API 在创建 execution 前冻结并校验请求时 retention、拒绝原目标或已有目标；bounded worker 再次校验 retention 和目标不存在后，以 execution-scoped apply identity 写入隔离表。replay 不读写 `transfer.sync_states`、主 lease、主 apply identity、主目标或任务 `desired_state/status/last_execution*`，因此可与主 continuous runtime 并行。

resume 前必须确认 committed `next_offset` 仍在 Kafka partition 的保留范围内。若 retention 已删除该位置，任务明确失败并要求人工决定如何处理，不能自动跳到 earliest。PostgreSQL/MySQL/Oracle 目标锁等待必须响应 context 取消；Oracle 使用 `FOR UPDATE NOWAIT` 有界重试，取消事务不得留下业务行或目标 ledger 的半提交状态。数据库 CDC 遇到 schema drift 时，当前 execution 失败且任务进入 `blocked`；业务 Kafka record 路线按显式 `block|dead_letter` 处理确定性记录错误，不复用 CDC 的 schema change request 语义。

continuous worker 按 Transfer 配置管理页面“连续任务策略”中的 diagnostics 采样间隔读取每分区 earliest/latest offset，用目标已成功应用的 committed `next_offset` 计算 lag 和 retention 恢复余量。时间余量使用连续 latest 样本的增长率估算；冷启动、无 committed position 或写入速率为零时显示 unknown。degraded/critical retention 阈值、checkpoint 停滞阈值均由同一配置页统一控制。`transfer.sync_states.position_committed_at` 单独保存真实 position commit 时间；只有存在 source lag 且 commit age 超过配置的 checkpoint 停滞阈值时 checkpoint health 才进入 degraded，lag 为 0 时保持 healthy。所有阈值均不进入任务 JSON。诊断结果写入 `common.task_executions.metadata.continuous.diagnostics`，Monitor 只读取 execution metadata，不直连业务 Kafka 或 Transfer 私有状态表。

continuous worker 重启时不会复用已结束的 execution。正常 worker shutdown 将旧 execution 收敛为 `cancelled + stop_reason=worker_shutdown`；新 worker 在任务 `desired_state=running` 时原子创建 recovery execution，继承上一 execution 的 `trigger_type`，并从 `transfer.sync_states` 已提交位置继续。runtime lease 过期时旧 execution 先以 `failed + stop_reason=lease_expired` 结束，再创建 recovery execution。恢复 execution 在 metadata 记录 `recovery_reason` 和 `recovered_from_execution_id`，每次领取继续递增 fencing token。

普通 execution failure 和 lease expiry 会立即创建唯一 pending recovery execution，但在 metadata 的 `recovery_not_before` 之前不可领取。连续失败按配置页中的指数退避、最大连续失败次数、circuit 冷却时间和稳定运行窗口处理；达到上限后 `recovery_circuit_state=open`，冷却到期被领取时转为 `half_open`。circuit open 不复用 schema drift 的任务 `blocked`；任务仍保持 `desired_state=running`，没有活 lease 时实际 `status=idle`。worker shutdown 不累计失败，schema drift、Pause 和 Stop 不自动恢复。任一目标 position 成功提交，或 session 稳定运行达到配置的稳定运行窗口后再失败，只重置 `recovery_consecutive_failures` 和 circuit 状态；本次 execution 的 `recovery_attempt`、`recovery_not_before` 与 `recovery_backoff_seconds` 作为审计事实保持不变。相关参数属于 Transfer-owned 平台运行配置，不进入任务 JSON。

## 九、数据库 CDC v1 已冻结边界

工作包 3A-3E 已完成第一版的 CDC 路线为：

```text
PostgreSQL/MySQL/Oracle 单表
  -> 对应 Debezium Connector
  -> Infra Kafka
  -> Transfer Continuous Worker
  -> PostgreSQL/MySQL/Oracle 新目标表
```

公开任务只保存 `runtime.boundary=continuous`、`load.mode=incremental`、`load.change_detection.type=cdc`、`bootstrap=initial_snapshot`、源表 locator、目标表和 field mapping。source provider 只能由 locator 对应的 System Engine 解析结果决定，不进入任务 JSON。connector、provider 专属捕获资源、Infra Kafka topic 和 consumer group 都由服务端生成，不进入 System Engine 或 Meta 资源树。

第一版只支持有稳定主键的单表。Debezium `op=r` 作为 snapshot upsert，`op=c|u` 作为 upsert，`op=d` 使用 record key 做物理 delete；目标固定为 `apply_mode=upsert_delete`，可选择声明完整原子应用能力的 PostgreSQL、MySQL 或 Oracle。目标必须是不存在的新表，由 bootstrap 创建；不清空或接管已有目标表。Oracle 目标只开放该 CDC apply 路线，字段不允许独立 `time`，decimal precision 最大 38，geometry 必须冻结为 XY dimension=2；不为这些限制保留兼容映射或旁路。捕获位点由 Kafka Connect 管理，Transfer 只在 `transfer.sync_states` 保存目标已应用的 Infra Kafka `next_offset`，两类位点不能互相代替。

PostgreSQL CDC 在创建 capture 前核对完整源表字段和真实类型，开放 `string|bool|int|bigint|float|double|decimal|date|time|timestamp|json|uuid|geometry`。PostGIS geometry 必须固定 OGC type、正 SRID和 XY/XYZ 维度；Transfer 将 Debezium `{wkb,srid}` 解码为 EWKB，按源空间事实创建目标 geometry 列，不做坐标转换，geometry 也不能作为主键。connector 固定 Decimal 字符串和 Connect 毫秒时间编码；`time` 和无时区 `timestamp` 仅允许精度 `0..3`。`bytea`、数组、geography、未约束 geometry、M/ZM geometry、interval、枚举及其他用户定义类型明确拒绝。

Debezium 的 geometry 属性名是 `wkb`，真实 PostGIS connector 可能在该 base64 属性中发送带 SRID 的 EWKB。Transfer 使用统一 WKB/EWKB 解析器，并校验内嵌 SRID、旁路 `srid` 与 generation 冻结事实一致，再输出 ADDP 标准 EWKB 行值。

MySQL CDC 固定支持 MySQL 8.0、有稳定非空主键的单表、`log_bin=ON`、`binlog_format=ROW`、`binlog_row_image=FULL` 和非零 server id。v1 接受有符号整数、字符/文本、Decimal、浮点、毫秒精度日期时间、JSON 和 binary/BLOB；拒绝 unsigned、`TINYINT(1)`/BOOL、BIT、ENUM/SET、YEAR、空间类型、超过毫秒的时间精度和 zero date。每个 generation 拥有唯一 connector server id、data topic 和 `cleanup.policy=delete + retention.ms=-1` 的 schema-history topic，Stop 时统一清理。

Oracle CDC 固定支持有稳定非空主键的普通单表，并要求独立 `cdc_database_name`、`cdc_user`、`cdc_password`、ARCHIVELOG、FORCE LOGGING、minimal supplemental logging 以及源表 `SUPPLEMENTAL LOG DATA (ALL) COLUMNS`。接受字符、整数、浮点、Decimal、毫秒精度时间戳和 `MDSYS.SDO_GEOMETRY`；`NUMBER` 按十进制字符串严格转换，`DATE/TIMESTAMP(0..3)` 按 Connect 毫秒时间解码。Oracle 原生 `BOOLEAN` 虽可被 Debezium 初始快照读取，但 Debezium 3.6 LogMiner 不能稳定交付包含该列的流式 DML，因此和普通 LOB/binary、JSON/XML、超过毫秒精度的 timestamp 一样在创建 capture 前拒绝，不能以快照成功推断增量可用。由于 LogMiner 不能直接稳定交付 `SDO_GEOMETRY`，Transfer 使用源 schema owner 账号创建 generation-owned 镜像表、行级同步触发器和 DDL guard：XY 转为 WKB BLOB，XYZ 转为保留 Z 值的 GeoJSON CLOB，再由同一 connector/consumer 主路径捕获；运行期间对逻辑源表的 ALTER/DROP/RENAME 会被明确拒绝，Stop 后才允许 DDL。空间事实从 Oracle Catalog Facts 冻结，adapter 严格校验 base64 WKB 或 GeoJSON 的 SRID、拓扑和 XY/XYZ 维度，统一输出标准 EWKB；内部镜像列不等于开放用户 LOB CDC。真实 E2E 已覆盖 PostgreSQL/MySQL/Oracle 三类源到 Oracle 目标的 snapshot/insert/update/delete 与 offset/ledger 收敛；Oracle 源还覆盖跨 consumer batch 的 128 行事务提交、未提交不可见和回滚隔离。Oracle Spatial 的 XY Polygon/MultiPolygon 已覆盖 PostgreSQL/MySQL/Oracle 目标，XYZ 当前只覆盖 PostgreSQL，MySQL 与 Oracle 目标仍明确只支持二维 geometry。每个 generation 拥有 data topic 和独立 schema-history topic；表级 ALL COLUMN LOGGING 是共享 source readiness，Stop 不删除，Spatial 镜像表、行级触发器和 DDL guard 则在 Stop 核对身份后删除。

类型不兼容与字段增删、envelope/source 结构变化一样进入 `schema_change_blocked`，execution metadata 会记录 missing/unexpected/incompatible 字段，当前 Kafka 消息不会被跳过。任务同时进入 `status=blocked`，禁止直接启动、恢复或重试；connector 仍会继续采集，因此 backlog、Kafka retention 和磁盘风险继续存在。

PostgreSQL/MySQL 原 capture generation 的唯一恢复路线是人工确认 additive migration。`GET /task-definitions/:id/schema-change` 返回当前请求与服务端重新检查后的建议字段；只有当前阻塞消息实际包含、源表中仍存在、允许 NULL、不是主键且不是 geometry 的新增字段可审批。`POST /task-definitions/:id/schema-change/approve` 必须逐字段提交 `source`、`target`、`target_type` 和 `nullable=true`，完整覆盖本次新增字段。服务端复用 `PartitionedTableChangeApplyProvider` 幂等新增目标列，追加唯一 field mapping、递增 generation schema revision，并把任务收敛为 `status=idle, desired_state=paused`；审批不会隐式恢复，用户需通过既有 Resume 从原 committed offset 继续。Oracle 第一期的任何 schema drift，以及 PostgreSQL/MySQL 的删除字段、类型或主键变化、非 nullable/geometry 新增和协议变化，均只能永久 Stop 后创建新任务和新目标表。

审批提交后的 Meta deep scan 使用 request 持久化 `pending -> running(token, lease) -> success|failed` claim。并发审批只有一个调用者触发 Meta；进程崩溃留下的过期 `running` 只由相同重复审批 POST 接管，GET 始终只读，旧 token 的迟到结果被拒绝。真实 Meta 调用失败进入 `failed` 且不由重复审批自动重试，用户可在 Meta 手动扫描；claim TTL 由统一的 `TRANSFER_META_SCAN_CLAIM_TTL` 配置，默认 2 分钟。

`pause` 停止目标应用，但 connector 继续把数据库日志变化写入 Infra Kafka 并推进捕获位置。正常 pause 的主要代价是 Kafka backlog、磁盘和 retention 窗口；connector/Kafka 故障时还必须分别观测 PostgreSQL slot/WAL、MySQL binlog 或 Oracle redo/archive log 容量风险。resume 只在 committed position 尚未过期时保证无损。

`stop` 是 CDC task 的不可逆终态：删除 ADDP-owned connector、provider 专属捕获资源、data/schema-history topic、consumer group 和 ACL，任务不得再次 start/resume。重新同步必须创建新任务、新目标表并重新 initial snapshot。服务端 Stop API 必须要求 `confirmed=true` 且 `confirmation_text` 与任务名称完全一致，Console 同时使用 danger 二次确认并要求输入任务名称；stop 不删除目标业务表、目标 ledger、任务定义、execution 或审计记录。

continuous task 初始同样保存 `desired_state=stopped`，所以该字段只表达用户期望状态，不能证明数据库 CDC 已被永久停止。CDC 的不可逆终态以 `transfer.capture_resources.status=stopped` 为事实；尚无 capture generation 的新任务可以首次启动。`cleanup_failed` 表示 Stop 清理尚未完成，只允许重试清理，不允许重新启动或恢复。

完整配置、envelope、schema drift、目标原子应用和资源 owner 约束以 [ADDP 任务体系规范](../../docs/spec/addp任务体系规范.md) 的“Transfer 数据库 CDC v1 契约”为准。普通 Oracle LOB/RAC、ArcGIS SDE、多表、无主键、数据库 CDC replay/DLQ、Schema Registry、Avro、Protobuf、自动 schema evolution 和 truncate 事件均未进入当前实现；Oracle Spatial 已通过 generation-owned WKB 镜像进入现有 CDC 主路径，业务 Kafka 的 DLQ 与 bounded replay已公开。

capture supervisor 已通过 Kafka Connect REST 和 Infra Kafka admin API 管理任务级 generation，状态登记在 `transfer.capture_resources`。continuous worker 从登记的内部 topic/group 消费 Debezium 3.6 schemaless JSON，严格处理 `r/c/u/d`，并在协议或 schema 变化时以 `schema_change_blocked` 阻塞而不推进 offset。人工 additive request 和 revision 审计保存在 `transfer.schema_change_requests`；公开任务配置仍不出现这些内部名称或 schema evolution 开关。

## 十、Checkpoint、进度和重试

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
| resumable | PostgreSQL/MySQL source 的 watermark incremental 通过 `transfer.sync_states` 支持 execution 间 resume，目标按幂等 `table_upsert` capability 选择；snapshot checkpoint 仍仅可观测。 |

## 十一、写后 Meta 扫描

`auto_scan_metadata=true` 时，Transfer 按 runtime boundary 触发 Meta deep scan：

- bounded execution 在成功写入后扫描一次。
- 普通业务 Kafka continuous execution 不等待任务结束；目标 Provider 首次成功建立目标结构后，按 task `apply_identity` 提交一次目标父 catalog 扫描。
- 数据库 CDC connector 把 Debezium `Initial Snapshot` 通知写入当前 generation-owned 单分区 data topic；所有合法通知作为目标 `skip` 推进，只有严格匹配当前 connector 的 `COMPLETED` 所在 offset 在目标 ledger 和 Transfer position 提交后才触发扫描。空源表同样触发，不依赖数据事件或 `source.snapshot=last`。
- continuous 首次扫描使用持久化 claim 防止 worker recovery、resume 或并发实例重复提交；Meta 提交失败不阻断数据面，失败或过期 claim 由后续 runtime session 重新领取。
- 目标 schema 经正式审批发生变化后再次扫描；普通 DML、单条事件和单个 batch 不扫描。持续变化的行数等统计由低频计划扫描或手动刷新维护。

Transfer 不直接推导目标文件 attributes。GeoJSON 导出目标使用独立 `format=geojson` 写出；写后扫描由 Meta 按统一格式探测和 GeoJSON provider 解析目标内容，负责写入 `type_info.table`、`format_info.geojson` 和实际存在的 `capabilities.spatial`。

Transfer 只提交本次实际写出的目标边界；encoded/raw content 目标使用 `ref_groups`，不扩大为父目录扫描：

| 目标类型 | 扫描目标 |
|---|---|
| native table | schema 或 database。 |
| NFS encoded/raw file | 单文件 `ref_groups`。 |
| MinIO / S3 encoded/raw object | 单对象 `ref_groups`。 |
| Shapefile refs | 本次实际生成的 refs group，不补不存在的 sidecar refs。 |

Transfer 不直接写目标 Meta attributes。
