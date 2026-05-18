# Transfer 现状与 Provider 化改造调研

更新时间：2026-05-09

本文汇总 Transfer 模块当前与 engine、format、data type、resource 抽象相关的真实实现状态，用于后续把 Transfer 接入统一 Provider 体系。

## 结论

Transfer 当前已经有可用的批处理执行骨架，但 reader / writer 的创建方式仍然是历史 connector type 路由。

现状可以概括为：

1. `pipeline.Reader` / `pipeline.Writer` / `DataBatch` 是值得保留的运行期执行接口。
2. `ConnectorRegistry` 仍按 `postgresql`、`s3`、`parquet`、`shapefile`、`geojson` 等具体类型注册读写器。
3. 多个 reader / writer 同时承担了 engine 访问、资源枚举、格式解码、格式写出和提交职责。
4. Transfer 配置中仍把 engine、format、data type、spatial encoding 混在一个 `connector_type` 或 `output_format` 里。
5. 批量读写的性能优化能力已经存在雏形，但没有进入统一 planner 与 capability 模型。

因此，Transfer 后续不应继续新增具体格式 connector，而应先引入 `TransferPlan`：由 Transfer 编排层组合 engine capability、resource reader/writer、FormatPlugin、info provider 和 content reader，再生成运行期 `pipeline.Reader` / `pipeline.Writer`。

## 当前核心抽象

`transfer/backend/pkg/pipeline/interfaces.go` 定义了执行层接口：

| 抽象 | 当前价值 | 后续定位 |
|---|---|---|
| `Reader` | 批量读取、schema、seek、mode | 保留为运行期读取接口 |
| `Writer` | 批量写入、flush、close | 保留为运行期写入接口 |
| `DataBatch` | 行数据、schema、offset、分区、序列号 | 保留为 Transfer 内部批次模型，后续与 common table batch 对齐 |
| `Schema` / `Field` | 字段、类型、空间字段属性 | 后续从 Meta attributes / info provider 生成 |
| `Transform` | 对 `DataBatch` 做字段映射、过滤、空间转换 | 继续作为 Transfer 自身能力 |

这层接口的问题不大，问题主要在“谁来创建 reader / writer”。

当前 `transfer/backend/pkg/pipeline/registry.go` 中的 `ConnectorRegistry` 按单一字符串创建读写器：

```text
ConnectorConfig.Type -> ReaderFactory / WriterFactory
```

这会把组合问题压扁成一个维度。例如：

- `s3` 既可能表示对象存储读写，也可能表示 S3 上的 CSV / JSON / Shapefile 写出。
- `parquet` 既表示 Parquet 格式，也暗含当前实现从 S3 / MinIO 读取。
- `geojson` 在 Transfer 中表达空间编码或目标文件结构，但不能重新成为 ADDP 顶层 `format=geojson`。

## 当前注册情况

`transfer/backend/plugins/builtin_registration.go` 当前注册的主类型包括：

| 当前 connector | 真实含义 | 问题 |
|---|---|---|
| `jdbc` / `postgresql` / `mysql` | 数据库原生表读写 | 属于 engine-native table，不应进入 format 层 |
| `postgres_copy` | PostgreSQL 高性能写入 | 应成为 PostgreSQL engine write strategy |
| `csv` | 本地 CSV 文件读写 | format 与本地文件访问混在一起 |
| `s3` / `minio` | 对象存储读写 | 内部又按 file type 处理 CSV / JSON / Shapefile / Parquet |
| `nfs` | NFS 写入 | engine write 与格式输出混合 |
| `parquet` | Parquet 读写 | 当前实现绑定 S3 / MinIO 配置 |
| `shapefile` | Shapefile 读写 | 多refs格式，应走 multi reader / writer |
| `s3_shapefile` | S3 上的 Shapefile 读取 | 典型的 engine + format 组合被做成单独 connector |
| `geopackage` | GeoPackage 读写 | container / table 语义需要和 FormatPlugin / provider / reader 对齐 |
| `geojson` | JSON 空间结构读写 | 顶层 format 应是 `json + spatial` |
| `spatialite` / `sqlite` | SQLite/SpatiaLite 表读取 | 更像 container/native table 混合，需要拆清 |
| `spatialite_parallel` | SpatiaLite 并行读取 | 应成为 source read strategy，不是独立格式 |

这些注册说明 Transfer 的扩展性曾经靠“继续新增 connector type”实现。短期可用，但长期会导致每增加一种 engine 和 format 组合都要新增读写器。

## 典型耦合点

### S3Reader

`transfer/backend/plugins/readers/s3_reader.go` 当前同时做了：

- 创建 S3 client。
- 列举 bucket / prefix。
- 下载对象到临时文件。
- 根据 `file_type` 选择 JSON / JSONL / CSV。
- 委托 `FileReader` 解码。

合理拆分后应是：

```text
S3 engine provider -> contentio.Reader/List/Open
CSV/JSON FormatPlugin / content reader -> ReadBatch
Transfer adapter -> pipeline.Reader
```

### ParquetReader

`transfer/backend/plugins/readers/parquet_reader.go` 当前同时做了：

- 读取 S3 / MinIO 连接配置。
- 列举 prefix 下 `.parquet` 文件。
- 下载对象到内存。
- 用 Parquet 库解码 row group。
- 生成 `pipeline.DataBatch`。

合理拆分后应是：

```text
contentio.Reader.List(scope)
  -> contentio.Reader.Open(parquet object)
  -> format/parquet ScopeTableProvider 或 BatchReader
  -> pipeline.DataBatch
```

这样 Parquet provider 不需要知道对象来自 S3、MinIO、NFS 还是本地文件。

### S3Writer

`transfer/backend/plugins/writers/s3_writer.go` 当前同时做了：

- 创建 S3 client。
- 标准化 `file_type`。
- 选择 CSV / JSON / GeoJSON / Shapefile / Parquet writer。
- 创建本地临时文件或临时目录。
- 对 Shapefile 做 zip 打包。
- 上传对象。

合理拆分后应是：

```text
FormatBatchWriter / MultiBatchWriter
  -> 产生单资源或ref 资源
  -> StorageWriteProvider 提交到 S3 / MinIO
```

Shapefile 的 `.shp/.shx/.dbf/.prj` ref 集合必须整体提交，不能让 S3Writer 通过 file type 分支隐式决定。

### ExecutionEngineService

`transfer/backend/internal/service/execution_engine_service.go` 当前有几个历史入口：

- `resolveConnectorConfig()`：把 System / local engine 配置和任务配置合并成 connector config。
- `resourceToConnectorConfig()`：把 engine 配置转成 Transfer 自有 connector config。
- `inferConnectorType()`：根据 `engine_type`、`type`、`bucket`、`path` 等字段推断 connector。
- 自动把 PostgreSQL JDBC target 改成 `postgres_copy`。
- target 为 `s3` 时自动注入空间转换。

这些逻辑未来应迁移到 `TransferPlanner`：

- engine 解析：确认 source / target engine capability。
- resource 适配：构造 `contentio.Reader` / `contentio.Writer` / `NativeCursor`。
- format 解析：确认 batch read / batch write / multi read / multi write。
- data type 解析：确认 table schema、字段映射、空间字段、SRID。
- strategy 选择：COPY、并行读取、分区读取、对象提交策略。

## 配置模型问题

当前任务配置示例中常见字段包括：

- `scope`
- `engine_id`
- `connector_type`
- `query_type`
- `schema`
- `table`
- `output_format`
- `output_path`
- `csv_delimiter`
- `geometry_field`

问题是 `connector_type` 和 `output_format` 同时承载了多个概念：

| 字段 | 当前可能表达 | 应拆成 |
|---|---|---|
| `connector_type=s3` | S3 engine、对象存储目标、文件写出入口 | `engine=s3` + `data_type=table` + `format=csv/json/parquet/...` |
| `connector_type=parquet` | Parquet 格式、S3 上的 Parquet reader | `engine=s3/minio/nfs` + `format=parquet` |
| `output_format=geojson` | JSON 文件结构、空间编码、前端显示口径 | `format=json` + `spatial.encoding=geojson` |
| `geometry_field=geom` | 空间字段名 | 来自 Meta attributes 或 source schema，不应前端猜测 |

后续任务配置应显式区分：

```text
endpoint.engine
endpoint.resource
endpoint.data_type
endpoint.format
endpoint.format_options
endpoint.spatial
endpoint.write_policy
```

## 高性能能力现状

现有 `transfer/docs/transfer高性能分析.md` 中提到的能力值得保留，但需要换入口：

| 当前能力 | 当前入口 | 后续入口 |
|---|---|---|
| SpatiaLite 并行分区读取 | `spatialite_parallel` connector | source read strategy，由 planner 基于 engine / source 表能力选择 |
| PostgreSQL COPY 写入 | `postgres_copy` connector | PostgreSQL engine 的 batch write strategy |
| Reader / Writer 解耦流水线 | `ParallelExecutionEngine` | Transfer execution strategy |
| checkpoint | `Reader.SeekTo` / state manager | TransferPlan 中声明恢复粒度 |
| 多 writer 并行 | parallel config | target write strategy，受 format commit policy 约束 |

关键判断：性能优化不能继续作为新的 connector type 泄漏到用户配置中。它应成为 planner 在满足 capability 条件后选择的执行策略。

## 与 Meta attributes 的关系

Transfer 应消费 Meta 已经确认的事实，而不是重复推断：

- `attributes.item.organization`
- `attributes.item.data_type`
- `attributes.item.format`
- `attributes.item.refs`
- `attributes.storage.physical_path`
- `attributes.type_info.table.fields`
- `attributes.capabilities.spatial`
- `attributes.format_info.<format>`

尤其是：

- 不再在 Transfer 中猜 Shapefile ref 集合。
- 不再在 Transfer 中猜空间字段名一定是 `geom`。
- 不再把 `.geojson` 识别成独立顶层 format。
- 不再把 Parquet 目录称为 lake table item type。

## 需要保留的边界

Transfer 仍然应该拥有：

- 任务生命周期。
- 字段映射。
- Transform 串联。
- 批大小、并行度、checkpoint、重试。
- 读写策略选择。
- 执行指标。
- 任务执行日志。

Transfer 不应该拥有：

- engine native 访问细节。
- 文件格式编解码主实现。
- item 归并规则。
- Manager 面向前端的 DTO。
- Meta detector 规则。

## 当前待确认问题

1. `pipeline.DataBatch` 是否直接升级为 common table batch，还是先保持 Transfer 内部模型并做 adapter。
2. `Reader.SeekTo(offset)` 是否足够表达文件 row group、目录多文件、数据库游标和分区 checkpoint。
3. `Writer.Flush()` 与 format commit / engine commit 的边界如何拆。
4. 多 ref 写出时，format writer 产物应该落本地临时目录，还是通过 resource writer 提供的 staging sink。
5. 并行写出时，单文件格式、目录型格式和原生表的提交策略应如何统一表达。
