# common/format

`common/format` 是 ADDP 后端共享的格式基础包，负责格式身份、格式识别、格式能力声明、轻量 schema、类型映射、info provider 和 content reader 注册。

它只表达“格式自身能提供什么”，不负责 Meta item 归并、不负责 Manager 面向前端的 DTO、不直接连接 engine，也不承担 Transfer 任务编排。

## 职责边界

`common/format` 负责：

- 根据文件名、MIME、magic bytes 识别 `FormatType`。
- 声明格式 capability，例如 data type、layout、info provider、content reader、是否可 parse、是否支持 transfer read/write。
- 提供 `TableInfo`、`FieldInfo`、`FieldType` 等跨模块可复用的结构化语义模型。
- 注册和获取 format plugin、info provider / content reader，例如 `FormatPlugin`、`FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider`、兼容期 `TableProvider`、`ComponentTableProvider`、`ScopeTableProvider`。
- 提供跨数据源的 `TypeMapper`，把原生类型映射到 ADDP 通用字段类型。
- 不再保留 `FileMetadataExtractor` 旁路注册表；新增格式必须通过 FormatPlugin、info provider 和 content reader 进入主线。

`common/format` 不负责：

- 不决定一个资源是不是一个 Meta item，也不做多文件归并。
- 不接收 engine id，不读取 engine 配置，不创建 engine 连接。
- 不定义展示协议，不返回 Manager 面向前端的 DTO，不推荐前端渲染器。
- 不决定 Transfer 任务计划、提交边界、批量并发策略。
- 不把 `.geojson` 当作独立顶层格式；它是 `FormatJSON` 的空间结构能力。
- `ExtractInput` 等 provider 输入不携带 `EngineID`，调用方需要的引擎上下文应留在编排层。

上层模块应先基于 engine capability 或本地文件系统能力构造 `common/resource` 读取抽象，再把 `io.Reader`、`ComponentReader` 或 `ResourceReader + scope` 交给 FormatPlugin、info provider 或 content reader。

## 职责清单与目录归位

`common/format` 的代码按以下职责归位：

| 职责 | 代码位置 | 说明 |
| --- | --- | --- |
| 格式标识常量与 data type/layout/provider/reader 枚举 | `format_type.go`、`capability_registry.go`、`registry/descriptor.go` | `format` 只表示资源编码格式；`table`、`document` 等是 data type，不作为逻辑 format 注册。 |
| 格式身份 descriptor 与能力发现 | `descriptor.go`、`discovery.go`、`registry/` | 运行时注册、查询、冲突诊断和 capability view；内置格式定义由各 `plugins/<format>/Descriptor()` 维护。 |
| 格式检测 | `detection.go`、`detection_mime.go`、`detection_magic.go`、`detection_classification.go` | 基于扩展名、MIME、magic bytes 和 descriptor 识别 format candidate，不决定 data item 边界；根包保留稳定 facade。 |
| FormatPlugin、info provider、content reader 接口 | `provider.go` | 只定义格式层能力接口，不接 engine id，不返回 Manager DTO。 |
| provider / reader 注册表 | `provider_registry.go`、`provider_register*.go`、`provider_get.go`、`provider_list.go`、`provider_constructors.go`、`provider_views.go` | 注册和获取当前进程已加载的 plugin、info provider 和 content reader；`TableProvider`、`DocumentProvider` 仅为兼容期组合接口。 |
| data type 通用 info 模型 | `table_info.go`、`field_type.go`、`container_info.go`、`container_child.go`、`content_index.go` | 表、字段类型、容器、内容索引等跨模块结构。内容样本不进入这些 info。 |
| 格式私有 info 与横切事实候选 | `plugins/<format>/`、`spatial_info.go`、`content_index.go` | 具体格式私有结构留在对应插件目录；`TableInfo` 只提供通用 `FormatInfo`、`SpatialInfo`、`ContentIndex` 承载，由 Meta 映射到 `format_info.*`、`capabilities.*`、`content_index.*`。 |
| 解析选项和 manifest | `options.go`、`manifest.go` | provider / reader 调用选项，以及第三方 descriptor manifest 加载。 |
| 类型映射 | `type_mapper.go`、`mappers/` | 把数据库或格式原生字段类型映射到 ADDP 通用字段类型。 |
| 内置格式加载入口 | `builtin/` | 统一 blank import 内置格式插件和 type mapper。 |
| 具体格式实现 | `plugins/<format>/` | 一个稳定文件格式一个目录；descriptor、provider、reader 和测试尽量在目录内闭合。 |

不属于 `common/format` 的职责：

- Meta item 归并、claims 合并、component_files 写入。
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
- Shapefile 只有主资源 `.shp` 识别为 `FormatShapefile`；`.shx`、`.dbf`、`.prj`、`.cpg` 等组件不单独代表完整 Shapefile，组件归并由上层基于 format capability 和资源组织规则完成。
- Parquet 既可以是单文件表，也可以作为目录 scope 下的表文件；`common/format/plugins/parquet` 只提供格式判断和 provider，不表达 lake table item type。

## Format Identity 与 Detection

`format identity` 定义平台支持的格式是谁，以及它默认属于什么 data type、支持什么 layout、info provider 和 content reader。它由 `FormatPlugin` / `FormatDescriptor` 表达。

`format detection` 是给定文件名、MIME、magic bytes 或内容签名后，判断某个资源看起来是什么格式。Detection 输出 format identity 的引用，不决定 data item 边界。

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
| `Extensions` | 常见文件扩展名，用于识别和展示 |
| `DataType` | 默认可映射的数据类型，例如 `table`、`document`、`media` |
| `Layouts` | 资源组织形态：`single`、`multi`、`whole` |
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

`TableProvider` 是兼容期组合接口，等价于同时具备 `TableInfoProvider` 和 `TableSampleReader`。新实现应优先按 info provider 与 content reader 分开设计。

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

多组件格式使用 `ComponentTableProvider`：

```go
type ComponentTableProvider interface {
    TableProvider
    DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *ParseOptions) (*TableInfo, error)
    SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
```

目录 scope 格式使用 `ScopeTableProvider`：

```go
type ScopeTableProvider interface {
    TableProvider
    DescribeTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, options *ParseOptions) (*TableInfo, error)
    SampleTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
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
- 多组件 provider 接收 `resource.ComponentReader`。
- 目录 scope provider 接收 `resource.ResourceReader` 和 `resource.ResourceRef`。
- `SampleTable` 的 `offset` / `limit` 是逻辑数据行窗口，不是字节偏移。行号从数据区第 0 行开始；CSV 等有表头的格式不把表头计入数据行。
- `SampleTable` 的 `input` 默认表示资源起点。调用方也可以传入从某个 record boundary 开始的局部流，但必须通过 `ParseOptions.TableSample` 提供 `InputStartsAtRow`、`Fields` 等上下文，让格式 plugin 在内部完成剩余行跳过和解析。
- `content_index` 是上层 attributes 中的通用访问索引，可用于帮助 engine range reader 打开局部流；`common/format` 只定义索引结构和 reader 选项，不直接调用 engine。
- engine 鉴权、连接池、对象列举、内容打开、重试和审计都在上层或 engine/resource 层完成。
- Manager 可按自身协议把 `TableInfo + rows` 组装为面向前端的 DTO；该协议不属于 `common/format`。
- Transfer 后续通过 planner 组合 engine capability、format capability、info provider 和 content reader。

## Document Info Provider / Text Reader

文档 data type 的格式层入口拆成 `DocumentInfoProvider` 和 `DocumentTextReader`：前者返回文档元信息，后者返回文本片段。兼容期 `DocumentProvider` 只是二者的组合接口。它们消费调用方传入的 `io.Reader`，不直接读取 engine，也不返回 Manager 面向前端的 DTO。

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

- `FormatInfo`：格式私有事实，例如 CSV 分隔符、Excel sheet、Shapefile 组件、Parquet 文件行数。
- `SpatialInfo`：空间字段、几何类型、坐标系、范围等横切事实。
- `ContentIndex`：读取优化索引，例如 `content_index.table` 的稀疏行索引。

空间判断应基于 `TableInfo.IsSpatial()` 或 `SpatialInfo`，不要通过 `format=geojson` 推断。

## 字段类型与类型映射

字段结构统一承载在 `TableInfo.Fields` / `FieldInfo` 中；根包不再保留并行的 `Schema` / `Field` 模型。

`TypeMapper` 负责原生类型和通用字段类型互转：

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
3. 按需实现 `FormatInfoProvider`、`TableInfoProvider`、`TableSampleReader`、`DocumentInfoProvider`、`DocumentTextReader`、`MediaInfoProvider` 等能力。
4. 在格式包 `init()` 中调用一次 `format.RegisterFormatPlugin(NewPlugin(...))`。注册函数会自动挂接 plugin 已实现的 provider / reader。
5. 如果格式是内置稳定能力，在 `Plugin.Descriptor()` 中维护完整 descriptor；descriptor-only 阶段也应有独立格式包。
6. 在 `builtin/init.go` 空白导入该格式包，触发内置 plugin 注册。
7. 为 descriptor、capability view、provider / reader 和边界情况补测试。

## 测试

```bash
go test ./common/format/...
go test ./common/resource
```

涉及 Meta 或 Manager 消费链路时，追加对应模块测试：

```bash
go test ./meta/backend/internal/service ./meta/backend/internal/metaitem ./meta/backend/internal/extractor
go test ./manager/backend/internal/preview ./manager/backend/internal/objectcontent ./manager/backend/internal/service
```

## 相关文档

- `docs/spec/addp数据类型与格式能力规范.md`
- `docs/spec/addp数据类型与文件格式扩展指南.md`
- `docs/spec/addp资源读取抽象规范.md`
- Manager 内容展示边界文档由 Manager 模块维护，不能反向约束 `common/format`。
