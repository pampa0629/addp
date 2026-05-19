# common/format

`common/format` 是 ADDP 后端共享的格式基础包，负责格式身份、格式识别、格式能力声明、轻量 schema、类型映射、info provider 和 content reader 注册。

它只表达“格式自身能提供什么”，不负责 Meta item 归并、不负责 Manager 面向前端的 DTO、不直接连接 engine，也不承担 Transfer 任务编排。

## 职责边界

`common/format` 负责：

- 根据文件名、MIME、magic bytes 识别 `FormatType`。
- 声明格式 capability，例如 data type、layout、info provider、content reader、是否可 parse、是否支持 transfer read/write。
- 提供 `TableInfo`、`FieldInfo`、`FieldType` 等跨模块可复用的结构化语义模型。
- 注册和获取 format plugin、info provider / content reader，例如 `FormatPlugin`、`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`TableReaderProvider`、`MultiTableReaderProvider`、`TableWriterProvider`、`MultiTableWriterProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider`、`ContainerInfoProvider`、`ContainerChildResolver`，以及历史组合接口 `TableProvider`、`DocumentProvider`、`MultiTableProvider`、`ScopeTableProvider`。
- 提供跨数据源的 `TypeMapper`，把原生类型映射到 ADDP 通用字段类型。
- 不再保留 `FileMetadataExtractor` 旁路注册表；新增格式必须通过 FormatPlugin、info provider 和 content reader 进入主线。

`common/format` 不负责：

- 不决定哪些 content 组成一个 Meta item，也不做 related refs 归并。
- 不接收 engine id，不读取 engine 配置，不创建 engine 连接。
- 不定义展示协议，不返回 Manager 面向前端的 DTO，不推荐前端渲染器。
- 不决定 Transfer 任务计划、提交边界、批量并发策略。
- 不把 `.geojson` 当作独立顶层格式；它是 `FormatJSON` 的空间结构能力。
- `ExtractInput` 等 provider 输入不携带 `EngineID`，调用方需要的引擎上下文应留在编排层。

上层模块应先基于 engine capability 或本地文件系统能力构造 `common/contentio` 内容 I/O 抽象，再把 `io.Reader`、`contentio.Reader`，以及多 content 场景下的 `[]format.RelatedRef` 交给 FormatPlugin、info provider 或 content reader。

## 职责清单与目录归位

`common/format` 的代码按以下职责归位：

| 职责 | 代码位置 | 说明 |
| --- | --- | --- |
| 格式标识常量与 data type/layout/provider/reader 枚举 | `format_type.go`、`capability_registry.go`、`registry/descriptor.go` | `format` 只表示 content 编码格式；`table`、`document` 等是 data type，不作为逻辑 format 注册。 |
| 格式身份 descriptor 与能力发现 | `descriptor.go`、`discovery.go`、`registry/` | 运行时注册、查询、冲突诊断和 capability view；内置格式定义由各 `plugins/<format>/Descriptor()` 维护。 |
| 格式检测 | `detection.go`、`detection_mime.go`、`detection_magic.go` | 基于扩展名、MIME、magic bytes 和 descriptor 识别 format candidate，不决定 data item 边界；根包保留稳定 facade。 |
| FormatPlugin、info provider、content reader 接口 | `provider.go` | 只定义格式层能力接口，不接 engine id，不返回 Manager DTO。 |
| provider / reader 注册表 | `provider_registry.go`、`provider_register*.go`、`provider_constructors.go`、`provider_views.go` | 注册和获取当前进程已加载的 plugin、info provider、content reader 和 writer；`TableProvider`、`DocumentProvider`、`MultiTableProvider`、`ScopeTableProvider` 是历史组合接口。 |
| data type 通用 info 模型 | `data_info.go`、`field_type.go`、`container_info.go`、`container_child.go` | 表、字段类型、容器、内容索引等跨模块结构。内容样本不进入这些 info。 |
| 格式私有 info 与横切事实候选 | `plugins/<format>/`、`data_info.go` | 具体格式私有结构留在对应插件目录；`TableInfo` 只提供通用 `FormatInfo`、`SpatialInfo`、`ContentIndex` 承载，由 Meta 映射到 `format_info.*`、`capabilities.*`、`content_index.*`。 |
| 解析选项和 manifest | `options.go`、`manifest.go` | provider / reader 调用选项，以及第三方 descriptor manifest 加载。 |
| 类型映射 | `type_mapper.go`、`mappers/` | 把数据库或格式原生字段类型映射到 ADDP 通用字段类型。 |
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
| `registry/` | format descriptor 运行时注册、查询、能力发现和冲突诊断。 |
| `plugins/` | 内置文件格式插件。descriptor-only 阶段也必须有独立格式目录。 |
| `mappers/` | PostgreSQL、MySQL、SpatiaLite 等原生类型映射。 |
| `builtin/` | 内置 descriptor、provider / reader 和 type mapper 统一注册入口。 |
| `integration_test/` | 跨包集成验证。 |

## 格式识别

`FormatType` 是格式事实，不是 data item 类型。

```go
formatType := format.DetectFormat("roads.geojson", peek)
// formatType == format.FormatJSON

mimeType := format.FormatToMIME(format.FormatParquet)
formatType = format.MIMEToFormat("application/geo+json")
// formatType == format.FormatJSON
```

检测入口：

```go
func DetectFormat(filename string, peek []byte) FormatType
func MIMEToFormat(mimeType string) FormatType
func FormatToMIME(format FormatType) string
func GuessContentType(filename string, peek []byte) string
```

当前约定：

- `.json` 和 `.geojson` 都返回 `FormatJSON`。
- `application/json`、`application/geo+json`、`application/vnd.geo+json` 都返回 `FormatJSON`。
- Shapefile 只有 primary content `.shp` 识别为 `FormatShapefile`；`.shx`、`.dbf`、`.prj`、`.cpg` 等 related content 不单独代表完整 Shapefile，ref 归并由上层基于 format capability 和 item 组织规则完成。
- Parquet 既可以是单文件表，也可以作为目录 scope 下的表文件；`common/format/plugins/parquet` 只提供格式判断和 provider，不表达 lake table item type。

## Format Identity 与 Detection

`format identity` 定义平台支持的格式是谁，以及它默认属于什么 data type、支持什么 layout、info provider 和 content reader。它由 `FormatPlugin` / `FormatDescriptor` 表达。

`format detection` 是给定文件名、MIME、magic bytes 或内容签名后，判断某个 content 看起来是什么格式。Detection 输出 format identity 的引用，不决定 data item 边界。

Shapefile 这类 multi 格式需要特别区分：`.shp/.dbf/.shx` 的识别不等于 item 归并；归并属于 Meta item detector。

## Capability

`FormatCapability` 用于声明格式在平台中的可消费能力，术语与 engine capability 对齐。

内置 capability 由各格式包自己的 `Descriptor()` 派生。`common/format/registry` 只保存当前进程已注册的 descriptor，负责查询、冲突诊断和能力发现视图；它不是内置格式定义清单。调用方通常通过 `common/format` 顶层 facade 使用能力，不需要直接依赖 `registry` 子包。

```go
type FormatCapability struct {
    Format         FormatType
    I18nKey        string
    Extensions     []string
    DataType       string
    Layouts        []string
    ProviderHints  []string
    Spatial        bool
    TransferRead   bool
    TransferWrite  bool
    Parse          bool
    EngineFamilies []string
}
```

字段含义：

| 字段 | 含义 |
| --- | --- |
| `Format` | 顶层格式事实，例如 `csv`、`json`、`parquet`、`shapefile` |
| `I18nKey` | 展示层可使用的国际化 key |
| `Extensions` | 常见 content 扩展名，用于识别和展示 |
| `DataType` | 默认可映射的数据类型，例如 `table`、`document`、`media` |
| `Layouts` | format 可支持的 content layout：`single`、`multi`、`whole`；data item 落库时同一组值写入 `organization` |
| `ProviderHints` | 可用 provider 类型提示，例如 `table`、`spatial` |
| `Spatial` | 是否天然包含空间语义 |
| `TransferRead` | 是否适合作为 Transfer 读取格式 |
| `TransferWrite` | 是否适合作为 Transfer 写出格式 |
| `Parse` | 是否具备解析结构化内容的能力 |
| `EngineFamilies` | 常见适配 engine family，不是 provider 直接依赖 |

常用入口：

```go
capability, ok := format.GetFormatCapability(format.FormatParquet)
capabilities := format.ListFormatCapabilities()
formats := format.ListTransferFormatsForEngineFamily(format.EngineFamilyObject)
```

## Descriptor 与能力发现

`FormatDescriptor` 是格式能力的静态事实，覆盖识别、默认 data type、layout、provider hints、content readers 和 transfer 能力。内置 descriptor 由 `common/format/plugins/<format>/` 中的 `Descriptor()` 维护；`FormatPlugin` 是格式包的代码入口。调用 `RegisterFormatPlugin` 会注册 plugin，并按它实际实现的接口自动挂接对应 info provider / content reader。descriptor 可以先于完整 provider / reader 存在，用于表达目标能力和检测规则。

内置格式通过统一聚合包加载：

```go
import _ "github.com/addp/common/format/builtin"
```

该入口会导入内置格式包和 type mapper。没有导入它的进程只拥有自己显式注册的 descriptor / provider / reader。

```go
descriptor, ok := format.GetFormatDescriptor(format.FormatMarkdown)
views := format.ListFormatCapabilityViews()
diagnostics := format.ListFormatConflictDiagnostics()
```

当前能力发现视图是运行时只读视图，用于展示 format、provider、content reader、transfer 状态。冲突诊断会记录 descriptor 注册中的 format、extension、MIME 冲突；后续第三方 manifest 加载时将复用同一机制。

第三方格式可以先通过 descriptor manifest 注册识别和 capability 声明：

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
    },
    "content_readers": ["document_text", "raw_content"]
  }
}
```

## Info Provider 与 Content Reader

Info provider 只返回元数据，主要服务 Meta 写入 `type_info.*`、`format_info.*` 或 `capabilities.*`。Content reader 读取内容数据，主要服务 Manager、Transfer 或其他上层消费方。

`TableProvider` 是历史组合接口，等价于同时具备 `TableInfoProvider` 和 `TableSampleReader`。新实现应优先按 info provider 与 content reader 分开设计。

### Provider 选择矩阵

选择 provider 时先看 data type，再看组织方式，最后看消费意图。format 只决定具体解码 / 编码实现，不改变这些接口的基本语义。

| Provider / Reader | Data type | 组织方式 | 输入 / 输出 | 核心能力 | 主要消费者 | 适合的 format |
|---|---|---|---|---|---|---|
| `FormatPlugin` | 任意 | 任意 | 无内容输入 | 声明格式身份、descriptor、capability；自动注册已实现的 provider / reader。 | Meta、Manager、Transfer、能力发现 | 所有稳定 format |
| `FormatInfoProvider` | 任意 | 通常 `single`，也可服务 `multi` / `whole` 的格式私有摘要 | `io.Reader` | 返回 `format_info.<format>` 候选事实，不写类型信息。 | Meta | CSV delimiter、PDF 版本、图片 EXIF、压缩方式等 |
| `TableInfoProvider` | `table` | `single` | `io.Reader` | 返回字段、行数、空间信息、内容索引等 table 类型信息。 | Meta、Manager、Transfer 探查 | CSV、JSON/JSONL、Parquet 单文件 |
| `TableSampleReader` | `table` | `single` | `io.Reader` | 按逻辑行窗口读取少量样本。 | Manager 预览、Transfer 探查 | CSV、JSON/JSONL、Parquet 单文件 |
| `TableReaderProvider` | `table` | `single` | `io.Reader` -> `TableReader` | 打开一次连续读取会话，按批读取全量行。 | Transfer 主链路、批处理导出/导入 | CSV、JSON/JSONL、Parquet 单文件 |
| `TableWriterProvider` | `table` | `single` | `io.Writer` + `TableInfo` -> `TableWriter` | 打开一次连续写出会话，按批编码写入。 | Transfer 写侧 | CSV、JSON/JSONL、Parquet 单文件 |
| `MultiTableProvider` | `table` | `multi` | `contentio.Reader` + `[]RelatedRef` | 多 ref table 的 info / sample 能力。 | Meta、Manager、Transfer 探查兜底 | Shapefile |
| `MultiTableReaderProvider` | `table` | `multi` | `contentio.Reader` + `[]RelatedRef` -> `TableReader` | 多 ref table 的连续全量读取会话。 | Transfer 主链路 | Shapefile |
| `MultiTableWriterProvider` | `table` | `multi` | `contentio.Writer` + `[]RelatedRef` + `TableInfo` -> `TableWriter` | 多 ref table 的连续写出会话。 | Transfer 写侧 | Shapefile |
| `ScopeTableProvider` | `table` | `whole` | `contentio.Reader` + scope | 目录 / prefix / scope 级 table 的 info / sample 能力。 | Meta、Manager、Transfer 探查 | Parquet dataset、未来 lake table |
| `DocumentInfoProvider` | `document` | 通常 `single` | `io.Reader` | 返回文档标题、语言、编码、大小等文档类型信息。 | Meta、Manager、Search | text、markdown、未来 PDF/DOCX 解析 |
| `DocumentTextReader` | `document` | 通常 `single` | `io.Reader` | 读取正文片段，可标记 truncated。 | Manager、Search、AI / 摘要 | text、markdown、未来 PDF/DOCX 解析 |
| `MediaInfoProvider` | `media` | 通常 `single` | `io.Reader` | 返回宽高、时长、编码、MIME、颜色空间、可选空间事实。 | Meta、Manager | image、jpeg、png、gif、tiff |
| `ContainerInfoProvider` | `container` | 通常 `single` | `io.Reader` | 描述容器内部 child 列表和默认入口。 | Meta、Manager | zip、excel、sqlite、geopackage |
| `ContainerChildResolver` | `container` 子内容 | `single` 父容器内部 | parent `contentio.Reader` + parent ref + child locator | 把容器 child 解析成可继续交给 format/provider 的 content。 | Manager、Transfer 后续 child 读取 | zip entry、Excel sheet、SQLite table |
| `RelatedRefSpecProvider` | 任意 multi 格式 | `multi` | 无内容输入 | 声明 related ref 的角色、扩展名、必需性和 primary。 | Meta item detector、Transfer multi reader/writer 构造 | Shapefile 等多 content 格式 |
| `RefDescriptorProvider` | 任意 multi 格式 | `multi` | `[]RelatedRef` | 把 refs 解释成用户可理解的描述。 | Manager、Meta 展示 | Shapefile 相关内容展示 |

### 实现要求

所有 provider / reader 实现必须遵守以下边界：

- 不接收 engine id，不读取 engine 配置，不创建 engine 连接；调用方先用 `common/engine` / `common/engine/contentadapter` / `common/contentio` 打开内容或 ref 集合。
- 不决定 data item 边界；`single` / `multi` / `whole` 的归并由 Meta detector 和 item 组织规则负责。
- 不返回 Manager DTO、前端渲染 hint 或 Transfer 任务结构；只返回通用 info、样本、文本、媒体信息或 reader/writer 会话。
- Info provider 不返回内容样本；content reader 不写 `type_info` / `format_info`。
- `FormatInfoProvider` 只承载格式私有事实；跨格式事实进入 `TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo` 等类型模型，再由 Meta 映射到标准 attributes。
- `Sample*` 接口的 offset / limit 是逻辑内容窗口，不是字节范围。
- `*ReaderProvider` / `*WriterProvider` 打开的是一次有状态会话，调用方负责循环读取 / 写入并调用 `Close`。
- multi 格式不得在 provider 内自行猜测相关路径；refs 集合由调用方基于 item detector 或 `RelatedRefSpecs()` 构造后传入。
- `Capabilities()` 与 `Descriptor()` 必须与实际实现一致；声明了能力，就应能通过注册表拿到对应 provider / reader。

### 命名规则与现状评估

命名采用三段式：`[组织方式前缀][DataType][能力后缀]`。

| 命名片段 | 含义 |
|---|---|
| 无前缀 | single content / single stream 输入，例如 `TableReaderProvider`。 |
| `Multi` | 多 ref 组织方式，例如 Shapefile。 |
| `Scope` | whole scope / 目录 / prefix 组织方式。 |
| `Table`、`Document`、`Media`、`Container` | data type 或容器父类型。 |
| `InfoProvider` | 类型元信息 provider，只返回 info。 |
| `SampleReader` | 读取少量样本或片段，面向预览 / 探查。 |
| `ReaderProvider` | 打开连续全量读取会话。 |
| `WriterProvider` | 打开连续写出会话。 |
| `Resolver` | 把容器内部定位解析成可继续读取的 content。 |
| `RefDescriptorProvider` / `RelatedRefSpecProvider` | 提供 ref 描述或相关 ref 推导规则，不读取内容。 |

当前命名基本可继续使用，但有三个历史兼容项容易混淆：

| 名称 | 问题 | 规则 |
|---|---|---|
| `TableProvider` | 名称过泛，实际只是 `TableInfoProvider + TableSampleReader`。 | 仅作为兼容组合接口；新实现优先分别实现 `TableInfoProvider`、`TableSampleReader`、`TableReaderProvider`。 |
| `DocumentProvider` | 名称过泛，实际只是 `DocumentInfoProvider + DocumentTextReader`。 | 仅作为兼容组合接口；新实现优先分别实现 `DocumentInfoProvider`、`DocumentTextReader`。 |
| `TableSampleProvider` / `MediaProvider` | 旧命名别名，`Provider` 后缀和当前 `Reader` / `InfoProvider` 规则不一致。 | 新代码使用 `TableSampleReader`、`MediaInfoProvider`。 |

`MultiTableProvider` 和 `ScopeTableProvider` 也是历史组合接口：它们表达 info / sample，不表达连续全量读写。为避免继续扩大歧义，新增全量能力必须使用 `MultiTableReaderProvider`、`MultiTableWriterProvider` 或后续明确的 `ScopeTableReaderProvider` / `ScopeTableWriterProvider`，不要把更多职责塞进组合接口。

治理策略：新代码优先使用更窄的 info、sample、continuous reader、writer 接口；历史组合接口只作为已有实现的聚合形态，不继续扩展职责。后续如果需要进一步收窄，可把 `MultiTableProvider` 拆成明确的 multi table info / sample reader 接口。

```go
type Provider interface {
    Format() FormatType
    Capabilities() FormatCapability
}

type TableInfoProvider interface {
    Provider
    DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error)
}

type TableSampleReader interface {
    Provider
    SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
```

多 ref table 格式按读取意图拆分接口。`MultiTableProvider` 提供 info / sample 能力，面向元数据、预览和少量样本读取：

```go
type MultiTableProvider interface {
    TableProvider
    DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []RelatedRef, options *ParseOptions) (*TableInfo, error)
    SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []RelatedRef, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
```

全量批处理读取使用 `MultiTableReaderProvider`，打开一次连续读取：

```go
type MultiTableReaderProvider interface {
    Provider
    RelatedRefSpecs() []RelatedRefSpec
    OpenMultiTableReader(ctx context.Context, reader contentio.Reader, refs []RelatedRef, options *ParseOptions) (TableReader, error)
}

type MultiTableWriterProvider interface {
    Provider
    RelatedRefSpecs() []RelatedRefSpec
    OpenMultiTableWriter(ctx context.Context, writer contentio.Writer, refs []RelatedRef, schema *TableInfo, options *WriteOptions) (TableWriter, error)
}
```

目录 scope 格式使用 `ScopeTableProvider`：

```go
type ScopeTableProvider interface {
    TableProvider
    DescribeTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, options *ParseOptions) (*TableInfo, error)
    SampleTableScope(ctx context.Context, reader contentio.Reader, scope contentio.Ref, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
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

reader, err := format.GetTableSampleProvider(format.FormatParquet)
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
- `content_index` 是上层 attributes 中的通用访问索引，可用于帮助 engine range reader 打开局部流；`common/format` 只定义索引结构和 reader 选项，不直接调用 engine。
- engine 鉴权、连接池、对象列举、内容打开、重试和审计都在上层或 engine/contentadapter 层完成。
- Manager 可按自身协议把 `TableInfo + rows` 组装为面向前端的 DTO；该协议不属于 `common/format`。
- Transfer 后续通过 planner 组合 engine capability、format capability、info provider 和 content reader。

## Document Info Provider / Text Reader

文档 data type 的格式层入口拆成 `DocumentInfoProvider` 和 `DocumentTextReader`：前者返回文档元信息，后者返回文本片段。历史组合接口 `DocumentProvider` 只是二者的组合接口。它们消费调用方传入的 `io.Reader`，不直接读取 engine，也不返回 Manager 面向前端的 DTO。

```go
type DocumentInfoProvider interface {
    Provider
    DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error)
}

type DocumentTextReader interface {
    Provider
    ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error)
}
```

当前内置最小实现：

- `text`
- `markdown`

调用示例：

```go
import _ "github.com/addp/common/format/builtin"

reader, err := format.GetDocumentTextReader(format.FormatMarkdown)
if err != nil {
    return err
}

text, truncated, err := reader.ReadDocumentText(ctx, input, 16*1024, nil)
```

WPS、DOCX、PPTX 这类后端不适合解析的格式，不应因为只能提供 raw / range content reader 就降级为二进制格式；后续如需全文索引、摘要或服务端转换，再补对应 DocumentInfoProvider / DocumentTextReader。

## Media Info Provider

`MediaInfoProvider` 是媒体 data type 的格式层入口。它消费调用方传入的 `io.Reader`，返回媒体元信息，不直接读取 engine，也不返回 Manager 面向前端的 DTO。

```go
type MediaInfoProvider interface {
    Provider
    DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error)
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

图片 MediaInfoProvider 目前返回宽高、编码、MIME、颜色空间和可选 GeoTIFF 空间属性。缩略图、视频、音频等内容读取能力后续通过独立 content reader 扩展。

## TableInfo

`TableInfo` 是 provider 返回的表语义模型。

```go
type TableInfo struct {
    Name       string
    RowCount   *int64
    SizeBytes  *int64
    CreatedAt  *time.Time
    UpdatedAt  *time.Time
    Fields     []FieldInfo
    PrimaryKey []string
    FormatInfo   map[string]interface{}
    SpatialInfo  *SpatialInfo
    ContentIndex *ContentIndexInfo
}
```

`TableInfo` 的补充事实必须按归属写入明确字段，不再通过开放式 extension 机制承载：

- `FormatInfo`：格式私有事实，例如 CSV 分隔符、Excel sheet、Shapefile refs、Parquet 文件行数。
- `SpatialInfo`：空间字段、几何类型、坐标系、范围等横切事实。
- `ContentIndex`：读取优化索引，例如 `content_index.table` 的稀疏行索引。

空间判断应基于 `TableInfo.IsSpatial()` 或 `SpatialInfo`，不要通过 `format=geojson` 推断。

## 字段类型与类型映射

字段结构统一承载在 `TableInfo.Fields` / `FieldInfo` 中；根包不再保留并行的 `Schema` / `Field` 模型。

`TypeMapper` 只应在对应 format / engine plugin 内部使用，负责原生类型和通用字段类型互转：

```go
mapper := format.GetTypeMapper("postgresql")
commonType := mapper.ToCommon("varchar(255)")

nativeType, size, precision := mapper.FromCommon(format.FieldTypeFloat)
```

新增类型映射时，实现并注册：

```go
type OracleTypeMapper struct{}

func (m *OracleTypeMapper) Name() string { return "oracle" }
func (m *OracleTypeMapper) ToCommon(nativeType string) format.FieldType
func (m *OracleTypeMapper) FromCommon(commonType format.FieldType) (string, int, int)
```

原生字段类型不得作为执行链路的公共 schema 语义向外扩散。`TableInfo.Fields` 对外只表达 ADDP 标准字段事实；如 Manager / Meta 需要展示原始字段类型，应由对应插件写入只读 attributes，供查看和诊断使用，不得参与 Transfer、transform 或目标写入决策。

## 旧 FileMetadataExtractor 已删除

早期 `FileMetadataExtractor` 注册表按 MIME 类型提取增强元数据，返回结构混合了 storage info、type info、format info、capabilities 和内容数据。该旁路机制已经删除，不再作为 Meta 按需提取或新增格式扩展入口。

当前写法：

- 确定的类型元数据进入 `TableInfoProvider`、`DocumentInfoProvider`、`MediaInfoProvider`、`ContainerInfoProvider` 等 info provider。
- 格式私有元信息进入 `FormatInfoProvider`。
- 样本、文本片段、缩略图、raw content、range content 等进入独立 content reader。
- Meta 只编排 provider / reader 结果并写入标准 attributes，不通过 MIME extractor 旁路写 attributes。

## 新增格式的推荐步骤

正式实施清单见 `docs/spec/addp数据类型与文件格式扩展指南.md`。代码侧最小步骤是：

1. 在 `common/format/plugins/<format>/` 新增 `plugin.go`，公开主类型命名为 `Plugin`，构造函数命名为 `NewPlugin`。
2. `Plugin` 必须实现 `FormatPlugin`。
3. 按需实现 `FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`TableReaderProvider`、`TableWriterProvider`、`MultiTableReaderProvider`、`MultiTableWriterProvider`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider`、`ContainerInfoProvider` 等能力。
4. 在格式包 `init()` 中调用一次 `format.RegisterFormatPlugin(NewPlugin(...))`。注册函数会自动挂接 plugin 已实现的 provider / reader。
5. 如果格式是内置稳定能力，在 `Plugin.Descriptor()` 中维护完整 descriptor；descriptor-only 阶段也应有独立格式包。
6. 在 `builtin/init.go` 空白导入该格式包，触发内置 plugin 注册。
7. 为 descriptor、capability view、provider / reader 和边界情况补测试。

## 测试

```bash
go test ./common/format/...
go test ./common/contentio ./common/engine/contentadapter
```

涉及 Meta 或 Manager 消费链路时，追加对应模块测试：

```bash
go test ./meta/backend/internal/service ./meta/backend/internal/metaitem ./meta/backend/internal/extractor
go test ./manager/backend/internal/preview ./manager/backend/internal/objectcontent ./manager/backend/internal/service
```

## 相关文档

- `docs/spec/addp数据类型与格式能力规范.md`
- `docs/spec/addp数据类型与文件格式扩展指南.md`
- `docs/spec/addp内容IO抽象规范.md`
- Manager 内容展示边界文档由 Manager 模块维护，不能反向约束 `common/format`。
