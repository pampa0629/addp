# Transfer 与格式能力整合方案

更新时间：2026-05-09

本文定义 Transfer 后续如何组合 engine capability、`common/contentio`、FormatPlugin、info provider 和 content reader。它是目标设计文档，不描述具体迁移进度。

> 注：本文早期使用过旧的 provider 统称。后续阅读时统一按正式规范理解为 FormatPlugin、info provider 和 content reader。

## 核心原则

1. Transfer 是批量读写和任务编排层，不是格式实现层。
2. FormatPlugin、info provider、content reader 不接 `engine_id`，也不自己构造 engine reader。
3. Transfer 外部先基于 engine capability 组装资源读取 / 写入抽象，再交给格式与数据类型能力实现。
4. 上层任务面向 `data_type`，新增 format 时尽量不改 Transfer 主流程。
5. 性能策略由 planner 根据 capability 自动选择，不再作为用户必须理解的 connector type。

## 总体链路

读取链路：

```text
TransferTask
  -> TransferPlanner
  -> SourceEndpoint
      -> EngineProvider / contentio.Reader / NativeCursor
      -> FormatProvider(optional)
      -> info provider / content reader
  -> pipeline.Reader
  -> Transform[]
```

写入链路：

```text
Transform[]
  -> pipeline.Writer
  -> TargetEndpoint
      -> info provider / content reader
      -> FormatProvider(optional)
      -> contentio.Writer / EngineProvider / NativeBatchWriter
  -> CommitPolicy
```

Transfer 运行期仍可以继续使用 `pipeline.Reader`、`pipeline.Writer` 和 `DataBatch`，但它们由 planner 生成，不再由 `connector_type` 直接创建。

## Endpoint 模型

Transfer source / target 应拆成稳定的 endpoint：

```text
TransferEndpoint
  Direction: source | target
  EngineRef
  contentio.Ref | NativeObjectRef
  DataType
  Format(optional)
  Organization(optional)
  Schema(optional)
  Spatial(optional)
  FormatOptions
  ReadPolicy | WritePolicy
```

### EngineRef

说明数据在哪里、怎么访问。

典型字段：

- `scope`：system / local。
- `engine_id`：引擎注册 ID。
- `engine_type`：由 System / local engine 解析得到。
- `capabilities`：engine capability 查询结果。

Transfer 不应把 `engine_type` 直接当作 reader / writer 类型。它只用于选择 engine provider 和资源适配器。

### contentio.Ref / NativeObjectRef

说明读写对象是什么。

文件 / 对象存储：

- path
- bucket / prefix
- refs
- scope path
- physical path

数据库 / 原生表：

- database
- schema
- table
- query
- collection
- graph query

### DataType

说明 Transfer 要处理的平台语义。

当前优先支持：

- `table`
- `document`
- `media`
- `container`
- `graph`

第一阶段只建议做 `table`，因为 Transfer 批量读写主路径本质上以表批次为核心。

### Format

说明文件或对象内容如何编码。

示例：

- `csv`
- `json`
- `excel`
- `shapefile`
- `parquet`
- `sqlite`
- `geopackage`

数据库原生表通常没有文件 format，直接走 engine-native table provider。

`geojson` 不作为顶层 format；Transfer 如果需要 GeoJSON 输出，应表达为：

```text
format=json
spatial.encoding=geojson
```

## TransferPlan

`TransferPlan` 是任务配置到运行期执行的中间结果。

建议结构：

```text
TransferPlan
  Source: PlannedEndpoint
  Target: PlannedEndpoint
  Batch:
    batch_size
    schema
    field_mapping
    transforms
  Execution:
    mode
    parallelism
    checkpoint_policy
    retry_policy
  Commit:
    policy
    staging
    cleanup
```

它的价值是把“能不能执行、怎么执行、如何提交、如何恢复”在执行前说清楚。

### PlannedEndpoint

`PlannedEndpoint` 应包含：

- 已选定的 engine provider。
- 已构造的 `contentio.Reader` / `contentio.Writer` / `NativeCursor`。
- 已选定的 FormatPlugin。
- 已确认的 info provider / content reader。
- 已确认的 schema 和 spatial facts。
- 已确认的并行 / checkpoint / commit 限制。

## 读取能力组合

读取侧需要区分三类来源：

| 来源形态 | 示例 | engine 责任 | format 责任 | 输出 |
|---|---|---|---|---|
| engine-native table | PostgreSQL table、MySQL table | SQL / cursor / batch read | 无 | DataBatch |
| single / multi 文件表 | CSV、JSON、Shapefile、单 Parquet | 打开资源或refs | 解码为表批次 | DataBatch |
| scope 表 | Parquet 目录、未来 Iceberg/Delta/Hudi | list scope、打开 manifest / data files | 解码 scope 表 | DataBatch |

### 文件读取

```text
engine capability
  -> contentio.Reader.Open / List
  -> format.TableProvider / BatchReader
  -> TransferDataBatchAdapter
```

必要能力：

- engine 支持 content read。
- 如果是 scope，engine 支持 list。
- 如果是 multi，Meta 或 format layout 能提供 refs。
- format 支持 sample / describe / batch read。

### 原生表读取

```text
engine capability
  -> NativeTableReadProvider
  -> TransferDataBatchAdapter
```

这类读取通常不经过 FormatPlugin。

## 写入能力组合

写入侧也分三类：

| 目标形态 | 示例 | format 责任 | engine 责任 | 提交 |
|---|---|---|---|---|
| engine-native table | PostgreSQL / MySQL table | 无 | batch insert / copy / transaction | engine transaction |
| single 文件 | CSV、JSON、Parquet 单文件 | 编码单个资源 | 写对象 / 文件 | 单资源提交 |
| multi / scope | Shapefile、Parquet 分片目录 | 编码refs或分片 | 提交ref 集合 / scope | refs整体提交 |

### 文件写出

```text
DataBatch
  -> format BatchWriter / TableWriter
  -> contentio.Writer + []format.RelatedRef
  -> CommitPolicy
```

format writer 只负责把批次编码成资源产物：

- 单文件产物。
- 多refs产物。
- scope 分片产物。
- manifest 或辅助资源。

engine writer 负责：

- 创建 staging。
- 写入对象 / 文件。
- 提交或回滚。
- 清理临时资源。

### 原生表写入

```text
DataBatch
  -> NativeTableWriteProvider
  -> engine transaction / copy / batch insert
```

PostgreSQL COPY 不应再是用户选择的目标 connector，而应是 PostgreSQL writer 在满足条件时选择的 strategy。

## 批量读写的双重要求

Transfer planner 必须同时校验 engine 与 format。

### 读取侧校验

| 校验项 | engine 要求 | format 要求 |
|---|---|---|
| 单文件读取 | content read | batch read |
| 多 ref 读取 | ref open 或多个 resource open | multi read |
| scope 读取 | list + open | scope read / manifest read |
| checkpoint | seek / cursor / range / list checkpoint | row group / record offset / file boundary 恢复 |
| schema | catalog / source schema 可选 | describe table |

### 写入侧校验

| 校验项 | engine 要求 | format 要求 |
|---|---|---|
| 单文件写出 | content write / staging | batch write |
| 多 ref 写出 | ref commit / atomic group | multi write |
| scope 写出 | list path / partition path / manifest commit | partition write / manifest write |
| 并行写 | 多连接 / 多对象写 | 是否允许多 writer |
| 回滚 | transaction / delete staging | 可丢弃未提交产物 |

## CommitPolicy

提交策略是 Transfer 与 format / engine 的关键结合点。

建议第一版至少表达：

| 策略 | 适用场景 | 说明 |
|---|---|---|
| `single_object` | CSV、JSON、单 Parquet | format 生成一个对象，engine 提交一个对象 |
| `ref_group` | Shapefile | format 生成多个refs，engine 整体提交 |
| `scope_replace` | Parquet 目录、未来 lakehouse scope | staging 目录完成后整体替换 |
| `native_transaction` | 数据库原生表 | engine 事务提交 |
| `native_bulk_load` | PostgreSQL COPY、Doris load 等 | engine 原生批量加载 |

Shapefile 不能只提交 `.shp`。Parquet 目录也不能把每个文件当作互不相关的单文件写出。

## CheckpointPolicy

checkpoint 不能只靠一个整数 offset 长期支撑所有场景。

建议按来源形态表达恢复点：

| 来源 | 恢复点 |
|---|---|
| 数据库表 | cursor、primary key、offset、snapshot token |
| CSV / JSONL | byte offset 或 record offset |
| Parquet | file key + row group + row offset |
| 多文件目录 | file index / file key + file 内 offset |
| Shapefile | record index + ref version |
| scope 表 | manifest version + data file + row group |

第一阶段可继续适配到 `Reader.SeekTo(offset)`，但 plan 中应保留更丰富的 checkpoint state，避免以后再破坏任务表结构。

## Spatial 处理

空间不是独立 data type，而是横切能力。

Transfer 应从以下来源获得空间事实：

1. Meta attributes 中的 `capabilities.spatial`。
2. source schema / info provider / content reader。
3. 用户在高级配置中明确覆盖。

Transfer 不应默认：

- 空间字段名一定是 `geom`。
- SRID 一定是 4326。
- JSON 输出一定是 GeoJSON。

目标格式需要空间编码时，由 planner 生成明确参数：

```text
spatial:
  geometry_columns
  srid
  source_encoding
  target_encoding
```

## 与现有 pipeline 的关系

现有 pipeline 不需要一开始推倒。

建议新增 adapter：

| Adapter | 输入 | 输出 |
|---|---|---|
| `NativeTableReaderAdapter` | engine native table reader | `pipeline.Reader` |
| `FormatBatchReaderAdapter` | contentio.Reader + format batch reader | `pipeline.Reader` |
| `NativeTableWriterAdapter` | engine native table writer | `pipeline.Writer` |
| `FormatBatchWriterAdapter` | format writer + contentio.Writer | `pipeline.Writer` |

这样主执行引擎仍然只看 `Reader -> Transform -> Writer`。

## 与 Manager Preview 的差异

Manager preview 和 Transfer 都消费 provider，但目标不同：

| 模块 | 目标 | 输出 |
|---|---|---|
| Manager | 展示样本和结构 | 面向前端的 DTO |
| Transfer | 稳定批量读写 | DataBatch / commit result |

因此：

- FormatPlugin、info provider、content reader 不返回 Manager 面向前端的 DTO。
- Transfer 不复用 Manager 面向前端的 DTO 做批量读取。
- 二者可以共享 `contentio.Reader`、`[]format.RelatedRef`、`TableProvider.Describe/Sample`。
- Transfer 需要额外的 `ReadBatch/WriteBatch/CommitPolicy`。

## 目标任务配置方向

长期配置应从：

```json
{
  "source": {
    "connector_type": "parquet",
    "engine_id": 3
  },
  "target": {
    "connector_type": "s3",
    "output_format": "geojson"
  }
}
```

收敛为：

```json
{
  "source": {
    "engine": {"scope": "system", "engine_id": 3},
    "resource": {"kind": "scope", "path": "datasets/poi/"},
    "data_type": "table",
    "format": "parquet"
  },
  "target": {
    "engine": {"scope": "system", "engine_id": 4},
    "resource": {"kind": "object", "path": "exports/poi.geojson"},
    "data_type": "table",
    "format": "json",
    "spatial": {"target_encoding": "geojson"}
  }
}
```

这只是目标形态，不要求第一阶段一次性改完前端和数据库。

## 第一阶段优先场景

建议先覆盖 table 型主路径：

1. PostgreSQL / MySQL 原生表互传。
2. PostgreSQL / MySQL -> S3 / MinIO / NFS CSV。
3. S3 / MinIO / NFS CSV -> PostgreSQL / MySQL。
4. S3 / MinIO / NFS Parquet 单文件 / 目录 -> PostgreSQL。
5. PostgreSQL -> Parquet 单文件 / 目录。
6. Shapefile -> PostgreSQL。
7. PostgreSQL -> Shapefile ref 集合。

完成这些后，再处理 GeoPackage、SQLite、Excel、document / media / graph 等非主线场景。
