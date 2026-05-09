# common/format

`common/format` 是 ADDP 后端共享的格式基础包，负责格式识别、格式能力声明、轻量 schema、类型映射和格式 Provider 注册。

它只表达“格式自身能提供什么”，不负责 Meta item 归并、不负责 Manager 预览 DTO、不直接连接 engine，也不承担 Transfer 任务编排。

## 职责边界

`common/format` 负责：

- 根据文件名、MIME、magic bytes 识别 `FormatType`。
- 声明格式 capability，例如 data type、layout、是否可 parse、是否可 preview、是否支持 transfer read/write。
- 提供 `TableInfo`、`FieldInfo`、`Schema`、`Field` 等跨模块可复用的结构化语义模型。
- 注册和获取 data type provider，例如 `TableProvider`、`ComponentTableProvider`、`ScopeTableProvider`。
- 提供跨数据源的 `TypeMapper`，把原生类型映射到 ADDP 通用字段类型。
- 为文件内容提供可选的增强元数据提取器 `FileMetadataExtractor`。

`common/format` 不负责：

- 不决定一个资源是不是一个 Meta item，也不做多文件归并。
- 不接收 engine id，不读取 engine 配置，不创建 engine 连接。
- 不返回 Manager 面向前端的 preview DTO。
- 不决定 Transfer 任务计划、提交边界、批量并发策略。
- 不把 `.geojson` 当作独立顶层格式；它是 `FormatJSON` 的空间结构能力。
- `ExtractInput` 等 provider 输入不携带 `EngineID`，调用方需要的引擎上下文应留在编排层。

上层模块应先基于 engine capability 或本地文件系统能力构造 `common/resource` 读取抽象，再把 `io.Reader`、`ComponentReader` 或 `ResourceReader + scope` 交给 format provider。

## 目录结构

核心文件：

| 文件 | 职责 |
| --- | --- |
| `detection.go` | `FormatType`、扩展名/MIME/magic bytes 检测 |
| `capability_registry.go` | 顶层 format capability facade |
| `capability/registry.go` | capability 注册表实现 |
| `provider.go` | Provider 基础接口和 table provider 注册表 |
| `table_info.go` | `TableInfo`、`FieldInfo` 等 data type 语义模型 |
| `schema.go` | 轻量 schema 与字段类型模型 |
| `type_mapper.go` | 类型映射注册表 |
| `metadata.go` | 对象基础元数据、文件增强元数据提取器注册表 |
| `extension_info.go` | data type 扩展信息接口与常见扩展信息 |

具体格式 codec：

| 目录 | 职责 |
| --- | --- |
| `codecs/csv/` | CSV table provider |
| `codecs/excel/` | Excel 表格解析 |
| `codecs/json/` | JSON/GeoJSON 结构解析；顶层格式仍为 `FormatJSON` |
| `codecs/parquet/` | Parquet table provider 与 scope table 读取 |
| `codecs/shapefile/` | Shapefile 多组件 table provider、读写和类型映射 |
| `codecs/image/` | 图像和 GeoTIFF 解析 |
| `codecs/pdf/` | PDF 文档解析 |
| `codecs/sqlite/` | SQLite 分析能力 |
| `mappers/` | PostgreSQL、MySQL、SpatiaLite 等类型映射 |
| `builtin/` | 内置 provider 注册入口 |

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
- Shapefile 的 `.shp`、`.shx`、`.dbf`、`.prj` 等组件扩展名都识别为 `FormatShapefile`，但组件归并由上层基于 format capability 和资源组织规则完成。
- Parquet 既可以是单文件表，也可以作为目录 scope 下的表文件；`common/format/codecs/parquet` 只提供格式判断和 provider，不表达 lake table item type。

## Capability

`FormatCapability` 用于声明格式在平台中的可消费能力，术语与 engine capability 对齐。

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
    Preview        bool
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
| `Preview` | 是否可为 Manager 预览提供底层数据 |
| `Parse` | 是否具备解析结构化内容的能力 |
| `EngineFamilies` | 常见适配 engine family，不是 provider 直接依赖 |

常用入口：

```go
capability, ok := format.GetFormatCapability(format.FormatParquet)
capabilities := format.ListFormatCapabilities()
formats := format.ListTransferFormatsForEngineFamily(format.EngineFamilyObject)
```

## Table Provider

`TableProvider` 是表格 data type 的格式层入口。它只返回表语义，不返回 Manager preview DTO。

```go
type Provider interface {
    Format() FormatType
    Capabilities() FormatCapability
}

type TableProvider interface {
    Provider
    DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error)
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

provider, err := format.GetTableProvider(format.FormatParquet)
if err != nil {
    return err
}

info, err := provider.DescribeTable(ctx, input, nil)
if err != nil {
    return err
}

rows, err := provider.SampleTable(ctx, input, 0, 50, nil)
```

使用约束：

- 单文件 provider 接收 `io.Reader`。
- 多组件 provider 接收 `resource.ComponentReader`。
- 目录 scope provider 接收 `resource.ResourceReader` 和 `resource.ResourceRef`。
- engine 鉴权、连接池、对象列举、内容打开、重试和审计都在上层或 engine/resource 层完成。
- Manager 负责把 `TableInfo + rows` 组装为预览 DTO。
- Transfer 后续通过 planner 组合 engine capability、format capability 和 data type provider。

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
    Extensions []ExtensionInfo
}
```

扩展信息用于承载格式或 data type 的附加语义，例如：

- `SpatialInfo`：空间字段、几何类型、坐标系、范围等。
- `CSVInfo`：分隔符、编码、是否有表头等。
- `DocCollectionInfo`：文档集合结构信息。

空间判断应基于 `TableInfo.IsSpatial()` 或 `SpatialInfo`，不要通过 `format=geojson` 推断。

## Schema 与类型映射

`Schema` / `Field` 是轻量结构模型，适合跨模块传递字段定义。

```go
schema := &format.Schema{
    Fields: []format.Field{
        {Name: "id", Type: format.FieldTypeInt, Nullable: false},
        {Name: "name", Type: format.FieldTypeString, Size: 100},
    },
    PrimaryKey: []string{"id"},
}

if err := schema.Validate(); err != nil {
    return err
}
```

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

## FileMetadataExtractor

`FileMetadataExtractor` 是文件增强元数据提取器，适合从内容中补充基础元数据、轻量 schema 或格式特有属性。

```go
type FileMetadataExtractor interface {
    SupportedTypes() []string
    Extract(ctx context.Context, input ExtractInput) (*ExtractedMetadata, error)
    Priority() int
}
```

它不是 Meta item detector，也不负责 item 归并。Meta 可以使用它补充属性，但最终 item 识别、组织形态、冲突处理和落库策略仍属于 Meta。

## 新增格式的推荐步骤

1. 在 `detection.go` 增加或确认 `FormatType`、扩展名、MIME 和 magic bytes 规则。
2. 在 `capability/registry.go` 声明 capability，包括 data type、layout、provider hints 和 transfer/preview/parse 能力。
3. 如果格式可提供表语义，实现 `TableProvider`、`ComponentTableProvider` 或 `ScopeTableProvider`。
4. 在 `builtin/init.go` 注册内置 provider。
5. 如果涉及原生类型映射，实现并注册 `TypeMapper`。
6. 为检测、capability、provider 和边界情况补测试。
7. 如果新增 data type 或 provider 形态，先更新 `docs/next` 中的规范草案，再落代码。

## 测试

```bash
go test ./common/format/...
go test ./common/resource
```

涉及 Meta 或 Manager 消费链路时，追加对应模块测试：

```bash
go test ./meta/backend/internal/service ./meta/backend/internal/metaitem ./meta/backend/internal/extractor
go test ./manager/backend/internal/service
```

## 相关文档

- `docs/next/common-format收口与Provider化改造方案.md`
- `docs/next/addp格式Capability与DataTypeProvider接口草案.md`
- `docs/next/addp资源读取抽象规范.md`
- `docs/next/addpManager内容预览插件能力构想.md`
- `docs/next/transfer与FormatProvider整合方案.md`
