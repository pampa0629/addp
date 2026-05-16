# ADDP 格式与数据类型能力消费者调研

更新时间：2026-05-09

本文记录当前代码中 `meta`、`manager`、`transfer` 等模块对格式、数据类型、引擎能力的真实消费方式，用于后续反推统一的 format capability、info provider 和 content reader 体系。

本文只做现状盘点与原则归纳，不定义最终 Go 接口。正式术语以 [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md) 为准：格式主入口是 FormatPlugin；元数据能力称为 info provider；内容读取能力称为 content reader。

## 调研目的

核心问题是：上层消费者到底需要什么能力，才能不再直接依赖具体 engine type 或 format type。

目标不是让上层知道更多格式细节，而是让上层只面向平台 provider：

- `meta` 负责识别 item 和归并范围。
- `manager` 负责预览与展示。
- `transfer` 负责读、写、转换。
- `search` / `asset` / `portal` 负责索引和消费标准 attributes。

## 当前现状

### 1. Meta 侧

Meta 目前已经在做两类工作：

1. **item 归并与识别**
   - `meta/backend/internal/metaitem/resolver.go`
   - `meta/backend/internal/metaitem/shapefile_detector.go`
   - `meta/backend/internal/metaitem/table_file_detector.go`
   - `meta/backend/internal/metaitem/single_resource.go`

   这部分决定：

   - `organization=single|multi|whole`
   - `claimed resources`
   - `exclusive`
   - `component_files`
   - `meta_item.full_name`

   目前这一步是 Meta 自己的职责，`common/format` 并不直接参与 item 归并。

2. **格式信息 / 元数据 / item capability 结果提取**
   - `meta/backend/internal/extractor/metadata_extractor.go`
   - `meta/backend/internal/metaitem/shapefile_detector.go`
   - `meta/backend/internal/metaitem/table_file_detector.go`

   这里会把结果归到：

   - `type_info.table`
   - `type_info.document`
   - `type_info.media`
   - `type_info.container`
   - `format_info.<format>`
   - `capabilities.spatial`
   - `capabilities.extraction`
   - `capabilities.statistics`

### 2. Manager 侧

Manager 现在主要依赖两类信号：

1. **Meta 已入库 attributes**
   - `item.format`
   - `item.data_type`
   - `item.component_files`
   - `meta_item.full_name`
   - `type_info.*`
   - `format_info.*`
   - `capabilities.*`

2. **少量直接引擎能力**
   - `plugin.ContentReadableProvider`
   - `plugin.CatalogProvider`
   - `plugin.GraphQueryProvider`

典型代码：

- `manager/backend/internal/preview/preview_resolver.go`
- `manager/backend/internal/preview/preview_provider_file.go`
- `manager/backend/internal/preview/preview_provider_scope_table.go`
- `manager/backend/internal/preview/preview_provider_graph.go`
- `manager/backend/internal/objectcontent/object_content_plugin.go`

当前问题是，Manager 仍然保留了不少按 `engine.EngineType`、`format`、`item_type` 的分支。

本轮只关注预览相关路径。与预览无关的 engine type 分支，例如空间服务生成 MVT、特定查询执行、非预览下载或服务发布，可先不纳入本轮格式能力清理。

Manager 后端预览的真实需求不是“知道文件后缀”，而是：

- 能从已扫描 item 定位主资源和组件资源。
- 能读取 item 内容流或 item 对应的引擎原生数据。
- 能根据 data type 得到适合组装前端 preview 的数据提取结果。
- 能补充预览所需的 schema、字段、空间列、分页信息、文档/媒体/容器摘要。

### Manager 表预览收口判断

当前代码中表预览至少有三条路径：

- `manager/backend/internal/preview/preview_provider_file.go`：文件表，包含 CSV、Excel、JSON、Shapefile、Parquet 等。
- `manager/backend/internal/preview/preview_provider_scope_table.go`：scope 表，主要处理目录型或单文件 Parquet。
- `manager/backend/internal/preview/preview_provider_database.go`：数据库原生表。

这三条路径的读取方式不同，但 Manager 的最终需求高度一致：

- columns
- column metadata
- rows sample
- total / page / page size
- geometry columns / SRID / extent
- 可选的渲染辅助字段

因此，长期不建议保留 `filetable preview provider`、`laketable preview provider` 两套对外概念。

更合理的口径是：

| 当前路径 | 长期来源形态 | 长期入口 |
|---|---|---|
| 文件表预览 | single / multi 文件表 | `TableProvider.Sample` 经 `ResourceReader` / `ComponentReader` |
| 目录型表格预览 | scope 表 | `TableProvider.Sample` 经 `ResourceReader` + scope list |
| 数据库表预览 | engine-native 表 | `TableProvider.Sample` 经 `NativeCursor` |

Parquet/ORC/Avro 这类表格文件或目录型表格 scope 不应作为独立 item type 暴露给上层消费者。新扫描结果应表达为 `item_type=table`，并通过 `item.format=parquet/orc/avro` 与 `item.organization=single/whole` 区分单文件表和目录型表。

因此 Manager 后端至少需要以下 provider 形态：

| 预览类型 | 输入 | 输出 | 备注 |
|---|---|---|---|
| table preview | item locator、分页、可选字段选择 | columns、rows、total、field metadata、geometry columns | Manager 组装 DTO；底层只提供表格样本 |
| document preview | item locator、范围或页码 | 文档元信息、文本片段、可选原始内容句柄 | Manager 组装 DTO；底层只提供文档提取结果 |
| media preview | item locator | 媒体元信息、缩略图素材或可访问内容引用 | Manager 组装 DTO；底层只提供媒体提取结果 |
| container preview | item locator、内部路径 | children、默认入口、内部对象样本入口 | Manager 组装 DTO；底层只提供容器结构 |
| graph preview | item locator、采样参数 | nodes / relationships 或表格化采样 | Manager 组装 DTO；底层只提供图采样结果 |

文件格式相关数据由 FormatPlugin、info provider 或 content reader 提取；engine-native 数据由 engine provider 提取；Manager 最终通过 service facade 组装面向前端的 DTO，而不是让 `common/format` 直接返回 Manager 专用结构。

### 3. Transfer 侧

Transfer 当前仍存在较多按格式和引擎类型分支的实现：

- `transfer/backend/pkg/plugin_loader/loader.go`
- `transfer/backend/plugins/builtin_registration.go`
- `transfer/backend/plugins/readers/*`
- `transfer/backend/plugins/writers/*`
- `transfer/frontend/src/views/TaskWizard*.vue`

现阶段很多读写器仍以 `shapefile`、`geojson`、`parquet`、`sqlite`、`geopackage` 这样的历史具体格式名命名和注册。

这说明 Transfer 还没有完全切到“只消费标准 info provider / content reader”的层次。

这里需要区分两个含义：

- ADDP 顶层 `format`：已经不再把 `geojson` 作为独立格式，`.geojson` 统一归入 `json + spatial`。
- Transfer 空间编码：批量转换中仍可能需要 `geojson` 表达几何值或目标文件编码，这属于空间编码 / writer mode，不应重新污染顶层 `format`。

Transfer 是后续 provider 体系中最关键的消费者，因为它同时依赖两种能力：

1. **引擎能力**：数据在哪里、如何读取/写入字节流或原生记录。
2. **格式能力**：这些字节流或记录如何编码/解码成平台统一批次。

当前 `pipeline.Reader` / `pipeline.Writer` 已经抽象出批量读写模型：

- `Reader.Open`
- `Reader.Read`
- `Reader.Schema`
- `Reader.SeekTo`
- `Writer.Open`
- `Writer.Write`
- `Writer.Flush`
- `Writer.Close`
- `DataBatch`
- `Schema`

但现有 reader/writer 经常把“存储引擎访问”和“格式编解码”揉在一起。

例如：

- `S3Reader` 同时负责 S3 列举/下载和 JSON/CSV 文件读取。
- `ParquetReader` 同时负责 S3 访问和 Parquet 解码。
- `S3Writer` 同时负责 S3 上传和 CSV/JSON 空间结构/Shapefile/Parquet 写出选择。
- `JDBCReader` / `JDBCWriter` 则是 engine-native table 的读写，不应进入 format 层。

后续更合理的模型是两层组合：

```text
Transfer Source
  -> Engine IO Provider / Storage Endpoint
  -> Format Reader Provider
  -> DataBatch
  -> Transform
  -> Format Writer Provider
  -> Engine IO Provider / Storage Endpoint
  -> Transfer Target
```

### Transfer 双能力组合模型

Transfer 任务应该同时解析 source / target 的两个维度：

| 维度 | 示例 | 说明 |
|---|---|---|
| engine / storage | postgresql、mysql、minio、s3、nfs、local file | 负责连接、枚举、读取对象流、写入对象流、执行 SQL |
| format | 空、csv、json、shapefile、parquet、sqlite、geopackage | 负责把内容编码/解码为平台数据批次 |

典型组合：

| 场景 | engine 能力 | format 能力 | 组合结果 |
|---|---|---|---|
| PostgreSQL table -> PostgreSQL table | SQL read / SQL write | 无文件格式 | engine-native table provider |
| PostgreSQL table -> S3 Parquet | SQL read | Parquet write + S3 object write | 读原生表，写 Parquet 字节流到对象存储 |
| S3 CSV -> PostgreSQL table | S3 object read | CSV read + SQL write | 读对象流，解码 CSV，写原生表 |
| S3 Shapefile -> GeoPackage | S3 object read / component read | Shapefile read + GeoPackage write | 读取组件文件，写容器文件 |
| NFS Parquet dir -> MySQL | NFS catalog/read | Parquet multi-file read + SQL write | 枚举目录下 Parquet，批量写 MySQL |

因此 Transfer 不应只按 `connector_type=parquet` 或 `connector_type=s3` 做单维路由。它需要的是真正的计划：

```text
source:
  engine: s3
  format: parquet
  data_type: table
  read_mode: batch

target:
  engine: postgresql
  format: none
  data_type: table
  write_mode: batch
```

### Transfer provider 设计方向

后续可从现有 pipeline 抽象反推这些 provider，而不是马上替换全部 reader/writer：

| Provider | 职责 | 来源 |
|---|---|---|
| StorageReadProvider | 打开对象流、列举对象、读取组件资源 | engine provider |
| StorageWriteProvider | 写对象流、提交组件资源、生成最终路径 | engine provider |
| NativeTableReadProvider | 从引擎原生表读取 DataBatch | engine provider |
| NativeTableWriteProvider | 向引擎原生表写入 DataBatch | engine provider |
| FormatBatchReader | 从外部提供的一个或多个资源流解码 DataBatch | FormatPlugin / content reader |
| FormatBatchWriter | 将 DataBatch 编码为一个或多个资源流 | FormatPlugin / writer |
| TransferPlanner | 根据 source/target 的 engine + format + data_type 组合 provider | transfer 编排层 |

`pipeline.Reader` / `pipeline.Writer` 可以保留为运行期执行接口，但创建它们的方式应从“按 connector type 工厂”逐步演进为“由 TransferPlanner 组合 provider”。

### 现有代码的具体改造点

#### Manager 侧

1. `manager/backend/internal/preview/preview_resolver.go`
   - 继续拆分职责。
   - 只负责 Meta 标准结果到 preview 上下文的转换，不再继续累积路由规则。

2. `manager/backend/internal/preview/preview_provider_file.go`
   - 拆成资源读取、格式提取、table 组装三层。
   - 现有的 S3/文件/格式混合逻辑应下沉到资源抽象或格式 provider。

3. `manager/backend/internal/preview/preview_provider_scope_table.go`
   - 长期应并入统一的 `TableProvider` 路由。

4. `manager/backend/internal/preview/preview_provider_database.go`
   - 作为原生表的 `TableProvider` 适配器保留即可，不必独立定义一套表预览语义。

5. `manager/backend/internal/preview/preview_provider_doc_collection.go`
   - 收敛为 `DocumentInfoProvider` / `DocumentTextReader` 或 Manager 内容适配层。

6. `manager/backend/internal/objectcontent/object_content_plugin.go`
   - 这里面大量 image/pdf/json/excel/sqlite/parquet/shapefile 的处理，本质是格式内容提取。
   - 后续应尽量下沉到 FormatPlugin、info provider 或 content reader 的中间层。

7. `manager/backend/internal/objectcontent/object_content_plugin_loader.go`
   - 保留 Manager 面向前端的 DTO 组装。
   - 不再继续承担格式识别和内容提取的核心逻辑。

#### Transfer 侧

1. `transfer/backend/internal/service/execution_engine_service.go`
   - `inferConnectorType()` 和 `resourceToConnectorConfig()` 是历史入口。
   - 后续主路径应改成 `TransferPlan`，由 engine capability + format capability + info provider / content reader 组合出来。
   - 新路径稳定后应删除旧入口，而不是长期兼容。

2. `transfer/backend/pkg/pipeline/registry.go`
   - 作为执行层工厂保留即可。
   - 不再作为上层语义调度的唯一入口。

3. `transfer/backend/plugins/readers/*`
   - 需要拆出 engine IO provider 和 format reader 的职责边界。
   - 现在很多 reader 同时做了对象存储访问和格式解码。

4. `transfer/backend/plugins/writers/*`
   - 需要拆出 engine write 和 format writer 的职责边界。
   - 现在很多 writer 同时做了目标存储提交和格式生成。

5. `transfer/backend/pkg/pipeline/interfaces.go`
   - `Reader` / `Writer` / `DataBatch` 可以继续作为执行层模型。
   - 但它们不应直接成为平台级 provider 的命名来源。

6. `transfer/backend/pkg/pipeline/transform_registry.go`
   - `TransformCapability` 是很好的能力声明雏形。
   - 后续可作为格式/数据类型能力声明的参考模板。

### 批量读写的关键约束

Transfer 的批量读写不能只考虑格式，还要考虑引擎能力：

1. **输入侧是否支持列表 / 分区 / 多组件**
   - Parquet 目录需要 catalog/list 能力。
   - Shapefile 需要组件文件读取能力。
   - 单 CSV 只需要一个对象流。

2. **输入侧是否支持 seek / checkpoint**
   - 数据库可通过主键或游标。
   - 部分文件格式可按 row group 或文件边界恢复。
   - 普通文本文件 seek 可能只能粗粒度处理。

3. **输出侧是否支持原子提交**
   - 数据库事务。
   - 对象存储临时对象 + rename/copy/commit。
   - Shapefile 这类多组件格式需要整体提交。

4. **输出侧是否支持并行写**
   - 数据库可多 writer 连接。
   - 单文件格式通常不能简单多 writer 写同一文件。
   - Parquet 可以按多文件分片写，但需要 manifest / 命名规则。

5. **schema 与空间字段如何传递**
   - Schema 应来自 Meta 标准 attributes 或 source provider。
   - 空间字段不应由前端或 writer 猜字段名。
   - 写出 JSON 空间结构 / Shapefile / GeoPackage 需要明确 geometry column、geometry encoding、SRID。

6. **format write 与 engine write 的提交边界**
   - Format writer 负责产生文件或组件。
   - Engine writer 负责把文件或组件提交到目标存储。
   - 两者需要一个明确的 commit 协议，尤其是 multi 文件格式。

### Transfer 优雅方案

建议目标模型如下：

```text
TransferPlan
  SourceEndpoint
    EngineProvider
    FormatProvider(optional)
    InfoProvider / ContentReader
  TargetEndpoint
    EngineProvider
    FormatProvider(optional)
    InfoProvider / ContentReader
  BatchPipeline
    Schema
    Transforms
    CommitPolicy
```

执行时：

1. Transfer 从任务配置或 Meta item 得到 source / target 的 engine、format、data_type。
2. Transfer 查询 engine capability，确认能否读取或写入对应资源。
3. Transfer 查询 format capability，确认是否支持基于外部读取抽象的 batch read / batch write / component read / component write。
4. Transfer 根据 info provider / content reader 补齐 schema、children、spatial 等平台语义。
5. Planner 组合成运行期 `Reader` / `Writer`。
6. Pipeline 只处理 `DataBatch`，不关心底层是 PostgreSQL、S3、CSV 还是 Parquet。

这样可以保持现有 pipeline 的优点，同时逐步消除具体格式与具体引擎在 reader/writer 中的耦合。

### 4. Search / Asset / Portal 侧

这几块更多消费标准 attributes 和预览结果，尤其是：

- `type_info.media`
- `type_info.document`
- `type_info.table`
- `capabilities.spatial`

它们不太需要了解具体 format 实现，但前提是 Meta 已经把事实整理好。

## 现有问题

### 问题 1：format 能力和 item capabilities 混在一起

当前既有：

- `common/format` 根包 capability registry
- `attributes.capabilities`

但两者不是一个层级的东西。

前者是格式声明，后者是 item 落库事实。术语需要在后续规范里强行分清。

### 问题 2：Meta 仍然直接知道部分具体格式

例如：

- Shapefile detector
- Lake table detector
- single resource 内置规则

这些是合理的起点，但后续不应继续把更多格式分支写进 Meta。

Meta 应只负责调度已注册的 resolver，并根据返回的 `organization`、`claims`、`exclusive` 决定后续动作。

### 问题 3：Manager 仍在按 engine type / format type 做路由

典型表现：

- 选择预览 provider 时直接看 `engine.EngineType`
- 某些 provider 直接判断 `item_type`
- 某些 path 仍通过 format 白名单判断

这会让 Manager 继续膨胀成第二套格式识别逻辑。

### 问题 4：Transfer 仍把具体格式写进读写器注册

目前 Transfer 的 reader/writer/plugin loader 仍以具体格式名为入口。

这是可接受的过渡状态，但最终应该收敛成：

- info provider / content reader
- format capability
- 平台统一读写能力

而不是上层业务直接依赖每个格式包。

## 反推原则

后续 provider 体系应遵循以下原则：

1. **Meta 只做框架，不写死每个格式**
   - Meta 负责询问注册表。
   - Meta 不应该知道每种格式的组件规则细节。

2. **format capability 统一对齐 engine capability 的术语**
   - 格式声明自己能提供什么。
   - item 的 `attributes.capabilities` 是扫描结果，不是插件声明。

3. **统一通过 info provider / content reader 面向消费者**
   - `table`
   - `document`
   - `media`
   - `container`
   - `graph`
   - 以后如新增 `raster`、`point_cloud`，再新增新的 provider 类型

4. **上层不直接调用 engine native 能力**
   - 上层不关心 engine type
   - 上层不关心 format type
   - 上层只关心：这个 item 是什么 data type，能调用哪个 provider

5. **新增 data type 必然影响上层**
   - 如果只是新增 format，只要它能落到既有 info provider / content reader，上层不需要改
   - 如果新增 data type，上层必须新增相应 provider 和展示/转换能力

## 当前消费到的能力类型

从现有代码看，至少已经存在这些消费面：

| 消费面 | 真实需要的能力 |
|---|---|
| Meta item 识别 | format 识别、组织方式规则、claims、exclusive |
| Meta 元数据提取 | table schema、document info、media info、container info、spatial info |
| Manager 表格预览 | table schema、preview |
| Manager 文档预览 | document info、text extraction |
| Manager 媒体预览 | media info、EXIF / 地理扩展信息 |
| Manager 容器预览 | container children |
| Manager 图预览 | graph provider |
| Transfer 读 | table / document / media / spatial 读取能力 |
| Transfer 写 | 格式写出能力 |
| Transfer 转换 | 格式转换能力 |
| Search / Asset | 标准 attributes + 预览摘要 + 空间 / 统计 / 文本能力 |

## 后续接口设计输入

这份调研的结论会直接约束后续接口设计：

1. 先收 `common/format`，把底层能力声明和旧 parser 边界先整理清楚。
2. 再定义 `format capability`、info provider 和 content reader 的术语边界。
3. 再定义每个 data type 的 provider / reader 需要什么方法。
4. 再决定哪些 format 需要实现哪些 provider / reader。
5. 最后再改上层 Manager / Transfer 的调用方式。

## 暂不做的事

- 暂不定义最终 Go 接口。
- 暂不重构 `common/format` 目录。
- 暂不改 `meta` / `manager` / `transfer` 的调用代码。
- 暂不把所有具体格式迁移成统一 provider。
