# common/format

`common/format` 是 ADDP 后端共享的格式基础包，负责格式身份、格式识别、格式静态声明，以及通过已注册 `FormatPlugin` 暴露 info provider、content reader 和 writer provider。

它只表达“格式自身能提供什么”，不负责 Meta item 归并、不负责 Manager 面向前端的 DTO、不直接连接 engine，也不承担 Transfer 任务编排。

## 职责边界

`common/format` 负责：

- 根据文件名、MIME、magic bytes 识别 `FormatType`。
- 维护 `FormatDescriptor` 静态事实，例如 data type、layout 和识别事实。
- 提供 format identity、FormatPlugin、provider / reader 动态查询；通用 `DataType`、`FieldType`、`TableInfo` 等结构化语义模型归属 `common/datatype`。
- 注册 format plugin，并通过同一个 plugin 的接口断言获取 info provider / content reader，例如 `FormatPlugin`、`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`MultiTableInfoProvider`、`MultiTableSampleReader`、`ScopeTableInfoProvider`、`ScopeTableSampleReader`、`TableReaderProvider`、`MultiTableReaderProvider`、`TableWriterProvider`、`MultiTableWriterProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider`、`ContainerInfoProvider`、`ContainerChildResolver`。
- 提供 `TypeMapper` 注册机制，供 engine / format 在自身边界内把原生类型映射到 ADDP 通用字段类型；上层执行链路不读取原生字段类型。
- 不再保留 `FileMetadataExtractor` 旁路注册表；新增格式必须通过 FormatPlugin、info provider 和 content reader 进入主线。

`common/format` 不负责：

- 不决定哪些 content 组成一个 Meta item，也不做 related refs 归并。
- 不接收 engine id，不读取 engine 配置，不创建 engine 连接。
- 不定义展示协议，不返回 Manager 面向前端的 DTO，不推荐前端渲染器。
- 不决定 Transfer 任务计划、提交边界、批量并发策略。
- `ExtractInput` 等 provider 输入不携带 `EngineID`，调用方需要的引擎上下文应留在编排层。

上层模块应先基于 engine capability 或本地文件系统能力构造 `common/contentio` 内容 I/O 抽象，再把 `io.Reader`、`contentio.Reader`，以及多 content 场景下的 `[]format.RelatedRef` 交给 FormatPlugin、info provider 或 content reader。

## 职责清单与目录归位

`common/format` 的代码按以下职责归位：

| 职责 | 代码位置 | 说明 |
| --- | --- | --- |
| 格式标识常量与 data type/layout 枚举 | `format_type.go`、`layout.go` | `format` 只表示 content 编码格式；`table`、`document` 等是 data type，不作为逻辑 format 注册；layout 对外统一使用 `format.Layout*` 和 helper。 |
| 格式身份 descriptor 与诊断快照 | `descriptor.go`、`discovery.go` | descriptor 注册、查询、冲突诊断和 `FormatCapabilitySnapshot`；内置格式定义由各 `plugins/<format>/Descriptor()` 维护。 |
| 格式检测 | `detection.go`、`detection_mime.go`、`detection_magic.go` | 基于扩展名、MIME、magic bytes 和 descriptor 识别 format candidate，不决定 data item 边界；根包保留稳定 facade。 |
| FormatPlugin、info provider、content reader 接口 | `provider.go` | 只定义格式层能力接口，不接 engine id，不返回 Manager DTO。 |
| plugin 注册与动态能力查询 | `provider_registry.go`、`provider_register.go`、`provider_constructors.go`、`provider_views.go` | 注册当前进程已加载的 plugin；获取具体 provider / reader / writer 时对同一 plugin 做接口断言。 |
| data type 通用 info 模型 | `common/datatype` | `common/format` 只通过 provider 返回 `datatype.TableInfo`、`datatype.DocumentInfo`、`datatype.MediaInfo`、`datatype.ContainerInfo` 等通用事实，不再保留平行结构。内容样本不进入这些 info。 |
| 格式私有 info 与横切事实候选 | `plugins/<format>/`，目标通过 describe result 或等价结构返回 | 具体格式私有结构留在对应插件目录；`SpatialInfo`、`AccessIndex`、`FormatInfo` 与 `TableInfo` 同级返回，由 Meta 映射到 `capabilities.*`、`access_index.*`、`format_info.*`。 |
| 解析选项和 manifest | `options.go`、`manifest.go` | provider / reader 调用选项，以及第三方 descriptor manifest 加载。 |
| 类型映射注册机制 | `type_mapper.go`、`mappers/`、`plugins/<format>/` | 根包只提供注册表和通用接口；数据库 engine 映射位于 `mappers/`，格式私有映射留在对应 `plugins/<format>/` 目录内。 |
| 内置格式加载入口 | `builtin/` | 统一 blank import 内置格式插件和 type mapper。 |
| 具体格式实现 | `plugins/<format>/` | 一个稳定文件格式一个目录；descriptor、provider、reader 和测试尽量在目录内闭合。 |

不属于 `common/format` 的职责：

- Meta item 归并、claims 合并、refs 写入。
- Manager / Frontend 预览 hint、渲染器或 DTO。
- engine 连接、engine reader 构造、连接池、鉴权和审计。
- Transfer planner、任务提交边界、并发策略。
- 泛用字符串 / 配置工具函数；没有明确格式语义的 helper 不应放在本包下。

当前子目录含义：

| 目录 | 职责 |
| --- | --- |
| `plugins/` | 内置文件格式插件。descriptor-only 阶段也必须有独立格式目录。 |
| `mappers/` | PostgreSQL、MySQL、SpatiaLite 等原生类型映射。 |
| `builtin/` | 内置 descriptor、provider / reader 和 type mapper 统一注册入口。 |
| `integration_test/` | 跨包集成验证。 |

## 格式识别

`FormatType` 是格式事实，不是 data item 类型。

```go
formatType := format.DetectFormat("roads.geojson", peek)
// formatType == format.FormatGeoJSON

mimeType := format.FormatToMIME(format.FormatParquet)
formatType = format.MIMEToFormat("application/geo+json")
// formatType == format.FormatGeoJSON
```

检测入口：

```go
func DetectFormat(filename string, peek []byte) FormatType
func NormalizeFormat(value string) FormatType
func MIMEToFormat(mimeType string) FormatType
func FormatToMIME(format FormatType) string
func GuessContentType(filename string, peek []byte) string
```

`NormalizeFormat` 是上层消费 format 字段、扩展名、MIME 或文件名时的统一归一化入口。它只返回已注册或可识别的 canonical format；识别不到时返回 `FormatUnknown`，不得把裸扩展名或任意字符串作为 format 写入 Meta / Manager / Transfer 语义字段。

当前约定：

- `.geojson` 返回 `FormatGeoJSON`；`.json` 默认返回 `FormatJSON`，但 `DetectFormat(filename, peek)` 在内容前缀严格匹配 GeoJSON `FeatureCollection` 时返回 `FormatGeoJSON`。
- `application/geo+json`、`application/vnd.geo+json` 返回 `FormatGeoJSON`；`application/json` 返回 `FormatJSON`。
- Shapefile 只有 primary content `.shp` 识别为 `FormatShapefile`；`.shx`、`.dbf`、`.prj`、`.cpg` 等 related content 不单独代表完整 Shapefile，ref 归并由上层基于 format descriptor、related ref 规则和 item 组织规则完成。
- Parquet 既可以是单文件表，也可以作为目录 scope 下的表文件；`common/format/plugins/parquet` 只提供格式判断和 provider，不表达 lake table item type。
- `IsDocumentFormat`、`IsTableFormat`、`IsImageFormat` 等分类 helper 是 descriptor `DataType` / MIME 事实的派生判断，不维护独立格式分类表；例如 Excel 的默认 data type 是 `container`，不属于 `IsTableFormat`。
- 未知扩展名不能仅凭后缀成为 format。`.yml`、`.conf` 等没有已注册 format identity 的资源，只有在内容前缀可判定为文本时才由 `DetectFormat(filename, peek)` 归为 `FormatText`；没有内容证据时保持 `FormatUnknown`。
- detection fallback 只保留内置 descriptor 尚未加载时的 bootstrap 主扩展名 / 主 MIME 兜底；完整扩展名、MIME 变体和 wildcard 识别必须来自 `FormatDescriptor.Identification`。

## Format Identity 与 Detection

`format identity` 定义平台支持的格式是谁，以及它默认属于什么 data type、支持什么 layout 和识别规则。静态事实由 `FormatDescriptor` 表达；已有代码实现时，`FormatPlugin` 作为格式包入口表达运行时身份。

`format detection` 是给定文件名、MIME、magic bytes 或内容签名后，判断某个 content 看起来是什么格式。Detection 输出 format identity 的引用，不决定 data item 边界。

调用方已经持有一个可能来自用户输入、attributes 或插件返回的 format-like 字符串时，应先调用 `NormalizeFormat`，不能在上层维护扩展名映射表，也不能在识别失败时返回原始字符串。只有 `common/format` 能把扩展名、MIME、文件名或内容探测结果提升为 canonical format。

Shapefile 这类 multi 格式需要特别区分：`.shp/.dbf/.shx` 的识别不等于 item 归并；归并属于 Meta item detector。

## Descriptor 与能力发现

`FormatDescriptor` 是格式静态声明的事实源，描述格式身份、识别规则、默认 data type 和 layout。

当前进程实际加载了哪些 plugin、provider、reader 或 writer，只能通过已注册 `FormatPlugin` 是否实现对应接口动态判断。descriptor 不声明 Go 实现状态，也不声明 raw/range 内容通道能力。

```go
type FormatDescriptor struct {
    ID             string
    Version        string
    Priority       int
    Format         FormatType
    I18nKey        string
    DataType       datatype.DataType
    Layouts        []string
    Identification FormatIdentification
}
```

字段含义：

| 字段 | 含义 |
| --- | --- |
| `Format` | 顶层格式事实，例如 `csv`、`json`、`parquet`、`shapefile` |
| `DataType` | 默认可映射的数据类型，例如 `table`、`document`、`media` |
| `Layouts` | format 可支持的 content layout：`single`、`multi`、`whole`；使用 `NormalizeLayout` / `HasLayout` 等 helper 处理，不手写自由字符串判断 |
| `Identification` | 扩展名、MIME、内容签名等识别事实 |

常用入口：

```go
descriptor, ok := format.GetFormatDescriptor(format.FormatParquet)
hasWhole := format.HasLayout(descriptor.Layouts, format.LayoutWhole)
```

`FormatDescriptor` 是格式静态事实，覆盖识别、默认 data type 和 layout。内置 descriptor 由 `common/format/plugins/<format>/` 中的 `Descriptor()` 维护；`FormatPlugin` 是格式包的代码入口。调用 `RegisterFormatPlugin` 会注册 plugin，若 plugin 实现 `FormatDescriptorProvider`，会校验 `Format()` 与 `Descriptor().Format` 一致并注册 descriptor。descriptor 可以先于完整 provider / reader 存在，但读写可执行性只由当前进程已注册 plugin 的接口实现决定。

内置格式通过统一聚合包加载：

```go
import _ "github.com/addp/common/format/builtin"
```

该入口会导入内置格式包和 type mapper。没有导入它的进程只拥有自己显式注册的 descriptor / plugin。

```go
descriptor, ok := format.GetFormatDescriptor(format.FormatMarkdown)
snapshot, ok := format.GetFormatCapabilitySnapshot(format.FormatMarkdown)
diagnostics := format.ListFormatConflictDiagnostics()
```

`FormatCapabilitySnapshot` 是临时诊断视图，用于展示 format 静态事实以及当前进程已加载的 provider / reader / writer 实现状态；它不是业务事实源。冲突诊断会记录 descriptor 注册中的 format、extension、MIME 冲突；后续第三方 manifest 加载时将复用同一机制。

第三方格式可以先通过 descriptor manifest 注册识别和静态声明：

```go
descriptor, err := format.RegisterFormatPluginManifest("/path/to/plugin.json")
descriptors, err := format.RegisterFormatPluginManifestsFromDir("/path/to/plugins")
```

manifest 当前最小结构：

```json
{
  "descriptor": {
    "id": "plugin-markdown-like",
    "version": "v1",
    "format": "markdown_like",
    "data_type": "document",
    "layouts": ["single"],
    "identification": {
      "extensions": [".mdl"],
      "mime_types": ["text/x-markdown-like"]
    }
  }
}
```

## Info Provider 与 Content Reader

Info provider 只返回元数据，主要服务 Meta 写入 `type_info.*`、`format_info.*` 或 `capabilities.*`。Content reader 读取内容数据，主要服务 Manager、Transfer 或其他上层消费方。

### Provider 选择矩阵

选择 provider 时先看 data type，再看内容布局，最后看消费意图。format 只决定具体解码 / 编码实现，不改变这些接口的基本语义。

| Provider / Reader | Data type | 内容布局 | 输入 / 输出 | 核心能力 | 主要消费者 | 适合的 format |
|---|---|---|---|---|---|---|
| `FormatPlugin` | 任意 | 任意 | 无内容输入 | 声明格式身份；后续 provider / reader / writer 查询对同一 plugin 做接口断言。 | Meta、Manager、Transfer、能力发现 | 所有稳定 format |
| `FormatInfoProvider` | 任意 | 通常 `single`，也可服务 `multi` / `whole` 的格式私有摘要 | `io.Reader` | 返回 `format_info.<format>` 候选事实，不写类型信息。 | Meta | CSV encoding、PDF 版本、图片 EXIF、压缩方式等 |
| `TableInfoProvider` | `table` | `single` | `io.Reader` | 返回字段、行数等 table 类型信息；空间信息、访问索引和格式私有事实作为同级 describe result 候选事实返回。 | Meta、Manager 探查、Transfer 规划 | CSV、JSON/JSONL、Parquet 单文件 |
| `TableSampleReader` | `table` | `single` | `io.Reader` | 按逻辑行窗口读取少量样本。 | Manager 预览、轻量探查 | CSV、JSON/JSONL、Parquet 单文件 |
| `TableReaderProvider` | `table` | `single` | `io.Reader` -> `TableReader` | 打开一次连续读取会话，按批读取全量行。 | Transfer 主链路、批处理导出/导入 | CSV、JSON/JSONL、Parquet 单文件 |
| `TableWriterProvider` | `table` | `single` | `io.Writer` + `TableInfo` -> `TableWriter` | 打开一次连续写出会话，按批编码写入。 | Transfer 写侧 | CSV、JSON/JSONL、Parquet 单文件 |
| `MultiTableInfoProvider` | `table` | `multi` | `contentio.Reader` + `[]RelatedRef` | 多 ref table 类型信息。 | Meta、Manager 探查、Transfer 规划 | Shapefile |
| `MultiTableSampleReader` | `table` | `multi` | `contentio.Reader` + `[]RelatedRef` | 多 ref table 样本读取。 | Manager 预览、轻量探查 | Shapefile |
| `MultiTableReaderProvider` | `table` | `multi` | `contentio.Reader` + `[]RelatedRef` -> `TableReader` | 多 ref table 的连续全量读取会话。 | Transfer 主链路 | Shapefile |
| `MultiTableWriterProvider` | `table` | `multi` | `contentio.Writer` + `[]RelatedRef` + `TableInfo` -> `TableWriter` | 多 ref table 的连续写出会话。 | Transfer 写侧 | Shapefile |
| `ScopeTableInfoProvider` | `table` | `whole` | `contentio.Reader` + scope | 目录 / prefix / scope 级 table 类型信息。 | Meta、Manager 探查、Transfer 规划 | Parquet dataset、未来 lake table |
| `ScopeTableSampleReader` | `table` | `whole` | `contentio.Reader` + scope | 目录 / prefix / scope 级 table 样本读取。 | Manager 预览、轻量探查 | Parquet dataset、未来 lake table |
| `ScopeTableReaderProvider` | `table` | `whole` | `contentio.Reader` + scope -> `TableReader` | 目录 / prefix / scope 级 table 连续全量读取会话。 | Transfer 主链路 | Parquet dataset、未来 lake table |
| `DocumentInfoProvider` | `document` | 通常 `single` | `io.Reader` | 返回文档标题、语言、编码、大小等文档类型信息。 | Meta、Manager、Search | text、markdown、json、pdf、docx、pptx；未来 WPS 解析 |
| `DocumentTextReader` | `document` | 通常 `single` | `io.Reader` | 读取正文片段，可标记 truncated。 | Manager、Search、AI / 摘要 | text、markdown、json、docx、pptx；未来 PDF/WPS 解析 |
| `BinaryContentReader` | `unknown` | 通常 `single` | `io.Reader` | 对已判定为 unknown 且非文本的内容读取原始字节片段，可标记 truncated。 | Manager、Transfer 探查 | unknown |
| `MediaInfoProvider` | `media` | 通常 `single` | `io.Reader` | 返回宽高、时长、编码、MIME、颜色空间、可选空间事实。 | Meta、Manager | image、jpeg、png、gif、tiff |
| `ContainerInfoProvider` | `container` | 通常 `single` | `io.Reader` | 描述容器内部 child 列表和默认入口。 | Meta、Manager | zip、excel、sqlite、geopackage |
| `ContainerChildResolver` | `container` 子内容 | `single` 父容器内部 | parent `contentio.Reader` + parent ref + child locator | 把容器 child 解析成可继续交给 format/provider 的 content。 | Manager、Transfer 后续 child 读取 | zip entry、Excel sheet、SQLite table |
| `RelatedRefSpecProvider` | 任意 multi 格式 | `multi` | 无内容输入 | 声明 related ref 的角色、扩展名、必需性和 primary。 | Meta item detector、Transfer multi reader/writer 构造 | Shapefile 等多 content 格式 |
| `RefDescriptorProvider` | 任意 multi 格式 | `multi` | `[]RelatedRef` | 把 refs 解释成用户可理解的描述。 | Manager、Meta 展示 | Shapefile 相关内容展示 |

Container 通用事实直接使用 `datatype.ContainerInfo` / `datatype.ContainerChildInfo`，`common/format` 不再保留平行结构或别名。child 可以用字符串 `Format` 表达内容格式，用 `Native` 承载受控的 child 定位或原生摘要；不承载 `layout`。父容器级格式统计、采样上限和截断状态由 `FormatInfoProvider` 写入 `format_info.<format>`，不得写入 `type_info.container.native`。容器内多文件归并、refs 展示等 layout 事实由 dataitem / Manager / Meta 编排层动态计算。

### 实现要求

所有 provider / reader 实现必须遵守以下边界：

- 不接收 engine id，不读取 engine 配置，不创建 engine 连接；调用方先用 `common/engine` / `common/engine/contentadapter` / `common/contentio` 打开内容或 ref 集合。
- 不决定 data item 边界；`single` / `multi` / `whole` 的归并由 Meta detector 和 item 组织规则负责。
- 不返回 Manager DTO、前端渲染 hint 或 Transfer 任务结构；只返回通用 info、样本、文本、媒体信息或 reader/writer 会话。
- Info provider 不返回内容样本；content reader 不写 `type_info` / `format_info`。
- `FormatInfoProvider` 只承载格式私有事实；跨格式 type info 使用 `common/datatype` 模型，横切事实和访问索引与 type info 同级返回，再由 Meta 映射到标准 attributes。
- `Sample*` 接口的 offset / limit 是逻辑内容窗口，不是字节范围。
- `*ReaderProvider` / `*WriterProvider` 打开的是一次有状态会话，调用方负责循环读取 / 写入并调用 `Close`。
- multi 格式不得在 provider 内自行猜测相关路径；refs 集合由调用方基于 item detector 或 `RelatedRefSpecs()` 构造后传入。
- `Descriptor()` 只声明格式静态事实；当前 Go 进程是否可调用某项实现，必须通过已注册 plugin 的接口断言判断。

### 命名规则与现状评估

命名采用三段式：`[内容布局前缀][DataType][能力后缀]`。

| 命名片段 | 含义 |
|---|---|
| 无前缀 | single content / single stream 输入，例如 `TableReaderProvider`。 |
| `Multi` | 多 ref 内容布局，例如 Shapefile。 |
| `Scope` | whole scope / 目录 / prefix 内容布局。 |
| `Table`、`Document`、`Media`、`Container` | data type 或容器父类型。 |
| `InfoProvider` | 类型元信息 provider，只返回 info。 |
| `SampleReader` | 读取少量样本或片段，面向预览 / 探查。 |
| `ReaderProvider` | 打开连续全量读取会话。 |
| `WriterProvider` | 打开连续写出会话。 |
| `Resolver` | 把容器内部定位解析成可继续读取的 content。 |
| `RefDescriptorProvider` / `RelatedRefSpecProvider` | 提供 ref 描述或相关 ref 推导规则，不读取内容。 |

治理策略：新代码使用明确的 info、sample、continuous reader、writer 接口；不得新增 `*Provider` 组合接口来同时表达多种消费意图。

```go
type FormatPlugin interface {
    Format() FormatType
}

type TableInfoProvider interface {
    FormatPlugin
    DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*format.TableDescribeResult, error)
}

type TableSampleReader interface {
    FormatPlugin
    SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
```

多 ref table 格式按读取意图拆分接口。info 和 sample 分别注册，面向元数据、预览和少量样本读取：

```go
type MultiTableInfoProvider interface {
    FormatPlugin
    RelatedRefSpecs() []RelatedRefSpec
    DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []RelatedRef, options *ParseOptions) (*format.TableDescribeResult, error)
}

type MultiTableSampleReader interface {
    FormatPlugin
    RelatedRefSpecs() []RelatedRefSpec
    SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []RelatedRef, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
```

全量批处理读取使用 `MultiTableReaderProvider`，打开一次连续读取：

```go
type MultiTableReaderProvider interface {
    FormatPlugin
    RelatedRefSpecs() []RelatedRefSpec
    OpenMultiTableReader(ctx context.Context, reader contentio.Reader, refs []RelatedRef, options *ParseOptions) (TableReader, error)
}

type TableReader interface {
    Fields() []datatype.FieldInfo
    ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error)
    Close(ctx context.Context) error
}

type TableSpatialInfoProvider interface {
    SpatialInfo() *datatype.SpatialInfo
}

type MultiTableWriterProvider interface {
    FormatPlugin
    RelatedRefSpecs() []RelatedRefSpec
    OpenMultiTableWriter(ctx context.Context, writer contentio.Writer, refs []RelatedRef, tableInfo *datatype.TableInfo, options *WriteOptions) (TableWriter, error)
}
```

空间表的 `FieldTypeGeometry` 只表达字段语义，行值编码由 `ParseOptions.GeometryEncoding` 决定。默认编码是 `wkt`，用于 Manager sample、日志和调试；连续读取链路可显式请求 `wkb` 或 `ewkb`，此时行值为 `[]byte`。SRID / CRS 事实以 `datatype.SpatialInfo` 为准，`ewkb` 携带 SRID 只是行值编码能力，不替代空间参考事实。具体格式的 native 几何类型必须在各自 plugin 内转换，不得暴露到 format 根接口或 engine / Transfer 层。

格式写出 CRS 定义时，只能使用定义文本，不能使用 CRS ID 充当定义。Shapefile writer 的 `WriteOptions.ExtraParams["crs_definition"]` 表示 `.prj` 定义文本，例如 WKT、ESRI WKT 或 proj4 文本；不得传入裸 `EPSG:<code>` 或 `URN:OGC:DEF:CRS:EPSG::<code>`。CRS ID 应由 `datatype.SpatialInfo` 的 `crs_ref` 表达，定义文本应由 `crs_definitions[].definition` 表达。

目录 scope 格式同样拆成 info 和 sample：

```go
type ScopeTableInfoProvider interface {
    FormatPlugin
    DescribeTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *ParseOptions) (*format.TableDescribeResult, error)
}

type ScopeTableSampleReader interface {
    FormatPlugin
    SampleTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

type ScopeTableReaderProvider interface {
    FormatPlugin
    OpenTableScopeReader(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *ParseOptions) (TableReader, error)
}
```

调用示例：

```go
import _ "github.com/addp/common/format/builtin"

provider, err := format.GetTableInfoProvider(format.FormatParquet)
if err != nil {
    return err
}

info, err := provider.DescribeTable(ctx, input, nil)
if err != nil {
    return err
}

reader, err := format.GetTableSampleReader(format.FormatParquet)
if err != nil {
    return err
}
rows, err := reader.SampleTable(ctx, input, 0, 50, nil)
```

使用约束：

- 单文件 provider 接收 `io.Reader`。
- multi provider 接收 `contentio.Reader` / `contentio.Writer` 与显式 `[]RelatedRef`；`RelatedRef` 中的 `Ref` 是底层定位器，`Required` / `Primary` 是 ref 集合标注。
- 目录 scope provider 接收 `contentio.Reader` 和 `contentio.Ref`。
- `SampleTable` 的 `offset` / `limit` 是逻辑数据行窗口，不是字节偏移。行号从数据区第 0 行开始；CSV 等有表头的格式不把表头计入数据行。
- `SampleTable` 的 `input` 默认表示 content 起点。调用方也可以传入从某个 record boundary 开始的局部流，但必须通过 `ParseOptions.TableSample` 提供 `InputStartsAtRow`、`Fields` 等上下文，让格式 plugin 在内部完成剩余行跳过和解析。
- `access_index` 是上层 attributes 中的通用访问索引，可用于帮助 engine range reader 打开局部流；`common/format` 只定义索引结构和 reader 选项，不直接调用 engine。
- engine 鉴权、连接池、对象列举、内容打开、重试和审计都在上层或 engine/contentadapter 层完成。
- Manager 可按自身协议把 `TableInfo + rows` 组装为面向前端的 DTO；该协议不属于 `common/format`。
- Transfer 通过 planner 组合 engine capability、format descriptor、已注册 provider / reader / writer。

## Document Info Provider / Text Reader

文档 data type 的格式层入口拆成 `DocumentInfoProvider` 和 `DocumentTextReader`：前者返回文档元信息，后者返回文本片段。它们消费调用方传入的 `io.Reader`，不直接读取 engine，也不返回 Manager 面向前端的 DTO。

```go
type DocumentInfoProvider interface {
    Provider
    DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*datatype.DocumentInfo, error)
}

type DocumentTextReader interface {
    Provider
    ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error)
}
```

当前内置支持情况：

| 格式 | DataType | DocumentInfoProvider | DocumentTextReader | 说明 |
|---|---|---|---|---|
| `text` | `document` | 是 | 是 | `DocumentInfo` 当前只写 `encoding=utf-8`；正文片段由 `DocumentTextReader` 返回。 |
| `markdown` | `document` | 是 | 是 | 复用 text provider，当前不在 `DocumentInfo` 中提取标题或字数。 |
| `json` | `document` 默认，内容严格匹配记录集合时可升级为 `table` | 是 | 是 | `DocumentInfo` 当前只写 `encoding=utf-8`；同一格式也提供 table / spatial provider。 |
| `pdf` | `document` | 是 | 否 | 只提供轻量 metadata，例如 title、page_count、size_bytes；author / creator / producer 等 PDF 原生事实进入 `format_info.pdf`。 |
| `docx` | `document` | 是 | 是 | 轻量读取 `docProps` 中的 title、language、pages、words；正文从 `word/document.xml` 提取，并追加页眉、页脚、脚注、尾注和批注文本；暂不解析修订语义或复杂版面关系。 |
| `pptx` | `document` | 是 | 是 | 轻量读取 `docProps` 中的 title、language、slides、words；正文从 `ppt/slides/slide*.xml` 按页提取，并追加对应备注页与批注文本；暂不解析母版或隐藏页策略。 |
| `wps` | `document` | 否 | 否 | 后端解析边界尚未定义；原始文件预览由 engine / contentio / URL 内容通道提供。 |

`DocumentInfoProvider` 必须返回 `datatype.DocumentInfo`。文档正文、片段、摘要、OCR 结果、embedding 不写入 `DocumentInfo`；正文片段只通过 `DocumentTextReader` 或后续 extraction / semantic 链路表达。

调用示例：

```go
import _ "github.com/addp/common/format/builtin"

reader, err := format.GetDocumentTextReader(format.FormatMarkdown)
if err != nil {
    return err
}

text, truncated, err := reader.ReadDocumentText(ctx, input, 16*1024, nil)
```

WPS 这类复杂且变体较多的文档格式，不应因为当前只能通过 engine / contentio / URL 内容通道读取原始文件就降级为二进制格式；后续如需更完整的全文索引、摘要或服务端转换，再补对应 DocumentInfoProvider / DocumentTextReader 或外部 extraction 服务。

## Binary Content Reader

`unknown` 是一个内置 format identity，默认 data type 为 `unknown`，只注册 `BinaryContentReader`。它服务“不认识且非文本”的内容兜底：调用方已经完成文本判断或格式识别后，才通过 `format.GetBinaryContentReader(format.FormatUnknown)` 获取 reader 读取原始字节片段。

`DetectFormat` 在扩展名、MIME、内容签名、plugin sniffer 和 magic bytes 都无法识别时，会使用 `LooksLikeTextContent` 做轻量文本兜底；可读文本返回 `FormatText`，剩余 unknown 非文本才进入 `BinaryContentReader`。

`BinaryContentReader` 不做文本判断，不把 binary 声明为 data type 或独立 format，也不写 `type_info.binary`。Manager 可以把结果投影为 `preview_material=raw_binary` 的不支持预览提示；其他模块需要原始字节探查时复用同一 reader。

`raw_content` / `range_content` 不进入 `FormatDescriptor`，也不是 format plugin registry 中的可调用 Go reader。它们属于 engine capability、`contentio`、预签名 URL 或模块 fetcher 能提供的内容通道；format 层只负责格式解码、元信息提取和内容语义读取。

## Media Info Provider

`MediaInfoProvider` 是媒体 data type 的格式层入口。它消费调用方传入的 `io.Reader`，返回媒体元信息，不直接读取 engine，也不返回 Manager 面向前端的 DTO。

```go
type MediaInfoProvider interface {
    Provider
    DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*format.MediaDescribeResult, error)
}
```

当前内置最小实现：

- `image`
- `jpeg`
- `png`
- `gif`
- `tiff`

调用示例：

```go
import _ "github.com/addp/common/format/builtin"

provider, err := format.GetMediaInfoProvider(format.FormatPNG)
if err != nil {
    return err
}

info, err := provider.DescribeMedia(ctx, input, nil)
```

图片 MediaInfoProvider 目前返回宽高、编码、MIME、颜色空间，并可通过 `MediaDescribeResult.Spatial` 携带 GeoTIFF 等空间横切事实。缩略图、视频、音频等内容读取能力后续通过独立 content reader 扩展。

## Table Operation Schema

`common/datatype.TableInfo` 是 table 类型信息的通用事实源，对应 `attributes.type_info.table`。`common/format` 不再保留 `TableInfo` 薄壳；reader / writer / Transfer 的 table 执行上下文也直接使用 `datatype.TableInfo`，用来表达执行期所需的字段顺序、写出字段信息和采样上下文。

Provider 对外返回 `format.TableDescribeResult`。这是 format provider 的解析结果包，不是 data type 本体。其中 `Table` 只表达 `attributes.type_info.table`；同次解析得到的补充事实必须作为同级候选事实返回，由 Meta normalizer 写入对应分区：

- `FormatInfo`：格式私有事实，写入 `format_info.<format>`。
- `SpatialInfo`：空间字段、几何类型、坐标系、范围等横切事实，写入 `capabilities.spatial`。
- `AccessIndex`：读取优化索引，例如稀疏行索引，写入 `access_index.table`。

`datatype.TableInfo` 不承载 `FormatInfo`、`AccessIndex` 或 `SpatialInfo`。格式私有事实通过 `format.TableDescribeResult.FormatInfo` 或 `FormatInfoProvider` 返回；访问定位索引通过 `format.TableDescribeResult.AccessIndex` 返回；空间读取上下文通过 `TableSpatialInfoProvider` 返回，空间写出上下文通过 `WriteOptions.SpatialInfo` 传入。

空间判断应基于标准 `SpatialInfo` / `capabilities.spatial`，不要仅通过 `format=geojson` 推断。GeoJSON 是独立格式身份，但空间字段、SRID、extent 等事实仍必须来自内容解析结果。

## 字段类型与类型映射

字段结构统一承载在 `TableInfo.Fields` / `datatype.FieldInfo` 中；根包不再保留并行的 `Schema` / `Field` 模型，也不再保留 `format.FieldInfo`。`FieldType` 归属 `common/datatype`，字段名、类型、主键、精度、默认值等通用字段事实以 `datatype.FieldInfo` 为唯一事实源。

`TypeMapper` 只应在对应 format / engine plugin 内部使用，负责原生类型和 `datatype.FieldType` 互转：

```go
mapper := format.GetTypeMapper("postgresql")
commonType := mapper.ToCommon("varchar(255)")

nativeType, size, precision := mapper.FromCommon(datatype.FieldTypeFloat)
```

新增类型映射时，实现并注册：

```go
type OracleTypeMapper struct{}

func (m *OracleTypeMapper) Name() string { return "oracle" }
func (m *OracleTypeMapper) ToCommon(nativeType string) datatype.FieldType
func (m *OracleTypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int)
```

原生字段类型不得作为执行链路的公共字段语义向外扩散。`TableInfo.Fields` 对外只表达 ADDP 标准字段事实；如 Manager / Meta 需要展示原始字段类型，应由 provider 返回标准 `native_type` 或由 Meta 写入只读 attributes，供查看和诊断使用，不得参与 Transfer、transform 或目标写入决策。

## 旧 FileMetadataExtractor 已删除

早期 `FileMetadataExtractor` 注册表按 MIME 类型提取增强元数据，返回结构混合了 storage info、type info、format info、capabilities 和内容数据。该旁路机制已经删除，不再作为 Meta 按需提取或新增格式扩展入口。

当前写法：

- 确定的类型元数据进入 `TableInfoProvider`、`DocumentInfoProvider`、`MediaInfoProvider`、`ContainerInfoProvider` 等 info provider。
- 格式私有元信息进入 `FormatInfoProvider`。
- 空间等横切事实和访问索引作为同级结果进入 Meta normalizer，不塞进 `TableInfo`。
- 样本、文本片段、缩略图、raw content、range content 等进入独立 content reader。
- Meta 只编排 provider / reader 结果并写入标准 attributes，不通过 MIME extractor 旁路写 attributes。

## 新增格式的推荐步骤

正式实施清单见 `docs/spec/addp数据类型与文件格式扩展指南.md`。代码侧最小步骤是：

1. 在 `common/format/plugins/<format>/` 新增 `plugin.go`，公开主类型命名为 `Plugin`，构造函数命名为 `NewPlugin`。
2. `Plugin` 必须实现 `FormatPlugin`。
3. 按需实现 `FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`TableReaderProvider`、`TableWriterProvider`、`MultiTableReaderProvider`、`MultiTableWriterProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider`、`ContainerInfoProvider` 等能力。
4. 在格式包 `init()` 中调用一次 `format.RegisterFormatPlugin(NewPlugin(...))`。后续 `Get*Provider` / `Get*Reader` / `Get*Writer` 会对该 plugin 做接口断言。
5. 如果格式是内置稳定能力，在 `Plugin.Descriptor()` 中维护完整 descriptor；descriptor-only 阶段也应有独立格式包。
6. 在 `builtin/init.go` 空白导入该格式包，触发内置 plugin 注册。
7. 为 descriptor、`FormatCapabilitySnapshot`、provider / reader 和边界情况补测试。

## 测试

```bash
go test ./common/format/...
go test ./common/contentio ./common/engine/contentadapter
```

涉及 Meta 或 Manager 消费链路时，追加对应模块测试：

```bash
go test ./meta/backend/internal/service ./meta/backend/internal/metaitem
go test ./manager/backend/internal/preview ./manager/backend/internal/objectcontent ./manager/backend/internal/service
```

## 相关文档

- `docs/spec/addp数据类型与格式能力规范.md`
- `docs/spec/addp数据类型与文件格式扩展指南.md`
- `docs/spec/addp内容IO抽象规范.md`
- Manager 内容展示边界文档由 Manager 模块维护，不能反向约束 `common/format`。
