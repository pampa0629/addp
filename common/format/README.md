# common/format - 格式识别、Schema 与 Provider 工具

本包提供跨模块共享的数据格式识别、类型转换、Schema 标准化和格式 Provider 注册工具。

---

## 目录

- [概述](#概述)
- [格式识别](#格式识别)
- [Schema模型](#schema模型)
- [类型映射](#类型映射)
- [错误处理](#错误处理)
- [使用示例](#使用示例)

---

## 概述

`common/format` 包旨在解决以下问题：

1. **格式识别统一化** - 各模块使用相同的逻辑识别文件格式
2. **类型映射标准化** - PostgreSQL、MySQL、Shapefile等不同数据源的类型统一转换
3. **Schema定义规范化** - 提供统一的Schema模型供各模块使用
4. **Provider入口统一** - 表格等 data type 读取由 `ProviderRegistry` 暴露，不再通过旧 parser registry 暴露

**设计原则**：
- ✅ `FormatType` 只表达顶层格式事实，不表达业务 item
- ✅ Provider 只返回格式 / data type 语义，不返回 Manager preview DTO
- ✅ 读取资源由上层通过 `common/resource` 适配后传入 Provider
- ✅ `.geojson` 统一识别为 `FormatJSON`，空间语义由 spatial capability / attributes 表达

---

## 格式识别

### FormatType 枚举

```go
type FormatType string

const (
    // 地理空间格式
    FormatShapefile  FormatType = "shapefile"
    FormatGeoPackage FormatType = "geopackage"

    // 表格格式
    FormatCSV   FormatType = "csv"
    FormatExcel FormatType = "excel"

    // 文档格式
    FormatPDF  FormatType = "pdf"
    FormatDOCX FormatType = "docx"

    // 图像格式
    FormatJPEG FormatType = "jpeg"
    FormatPNG  FormatType = "png"

    // 数据库格式
    FormatSQLite FormatType = "sqlite"

    // 数据交换格式
    FormatJSON FormatType = "json"

    // 未知格式
    FormatUnknown FormatType = "unknown"
)
```

### DetectFormat 函数

根据文件名和内容前缀检测格式：

```go
func DetectFormat(filename string, peek []byte) FormatType
```

**检测策略**：
1. 优先根据文件扩展名判断
2. 对于需要验证的格式（PDF、SQLite等），检查Magic Bytes
3. 如果扩展名无法判断，尝试Magic Bytes检测

**示例**：

```go
import "github.com/addp/common/format"

// 读取文件前512字节
f, _ := os.Open("data.shp")
peek := make([]byte, 512)
f.Read(peek)

// 检测格式
formatType := format.DetectFormat("data.shp", peek)
if formatType == format.FormatShapefile {
    // 处理Shapefile
}
```

### MIME类型转换

```go
// MIME类型 → FormatType
func MIMEToFormat(mimeType string) FormatType

// FormatType → MIME类型
func FormatToMIME(format FormatType) string

// 综合检测（结合文件名和内容）
func GuessContentType(filename string, peek []byte) string
```

**示例**：

```go
// MIME → Format
format := format.MIMEToFormat("application/geo+json")
// format == format.FormatJSON

// Format → MIME
mimeType := format.FormatToMIME(format.FormatShapefile)
// mimeType == "application/x-shapefile"

// 综合检测
mimeType := format.GuessContentType("data.csv", firstBytes)
// 结合扩展名和Magic Bytes智能检测
```

### 格式类别判断

```go
// 判断是否为地理空间格式
func IsGeospatialFormat(format FormatType) bool

// 判断是否为文档格式
func IsDocumentFormat(format FormatType) bool

// 判断是否为图像格式
func IsImageFormat(format FormatType) bool

// 判断是否为表格格式
func IsTableFormat(format FormatType) bool
```

---

## Schema模型

### Schema 结构

```go
type Schema struct {
    Fields        []Field  // 字段列表
    PrimaryKey    []string // 主键字段名
    GeometryField *string  // 几何字段名
    GeometryType  *string  // 几何类型
    SpatialRefSys *string  // 空间参考系统
    RecordCount   *int64   // 记录数
    Comment       string   // Schema注释
}
```

### Field 结构

```go
type Field struct {
    Name         string    // 字段名
    Type         FieldType // 字段类型
    Nullable     bool      // 是否允许NULL
    Size         int       // 字符串长度或数值精度
    Precision    int       // 小数位数
    Comment      string    // 字段注释
    DefaultValue *string   // 默认值
    Extra        map[string]interface{} // 扩展属性
}
```

### FieldType 枚举

```go
type FieldType string

const (
    // 基础类型
    FieldTypeString    FieldType = "string"
    FieldTypeInt       FieldType = "int"
    FieldTypeBigInt    FieldType = "bigint"
    FieldTypeFloat     FieldType = "float"
    FieldTypeDecimal   FieldType = "decimal"
    FieldTypeBool      FieldType = "bool"
    FieldTypeDate      FieldType = "date"
    FieldTypeTime      FieldType = "time"
    FieldTypeTimestamp FieldType = "timestamp"

    // 地理空间类型
    FieldTypeGeometry   FieldType = "geometry"
    FieldTypePoint      FieldType = "point"
    FieldTypeLineString FieldType = "linestring"
    FieldTypePolygon    FieldType = "polygon"

    // 复杂类型
    FieldTypeJSON  FieldType = "json"
    FieldTypeArray FieldType = "array"
    FieldTypeUUID  FieldType = "uuid"
)
```

### Schema 方法

```go
// 查找字段
func (s *Schema) GetField(name string) *Field

// 判断字段是否存在
func (s *Schema) HasField(name string) bool

// 判断字段是否为主键
func (s *Schema) IsPrimaryKey(fieldName string) bool

// 判断是否为地理空间数据
func (s *Schema) IsGeospatial() bool

// 返回所有字段名
func (s *Schema) FieldNames() []string

// 验证Schema有效性
func (s *Schema) Validate() error
```

**示例**：

```go
schema := &format.Schema{
    Fields: []format.Field{
        {Name: "id", Type: format.FieldTypeInt, Nullable: false},
        {Name: "name", Type: format.FieldTypeString, Size: 100},
        {Name: "geom", Type: format.FieldTypePoint},
    },
    PrimaryKey:    []string{"id"},
    GeometryField: stringPtr("geom"),
    GeometryType:  stringPtr("Point"),
    SpatialRefSys: stringPtr("EPSG:4326"),
}

// 验证Schema
if err := schema.Validate(); err != nil {
    log.Fatal(err)
}

// 查询字段
if field := schema.GetField("name"); field != nil {
    fmt.Printf("Field: %s, Type: %s\n", field.Name, field.Type)
}

// 判断地理空间数据
if schema.IsGeospatial() {
    fmt.Println("This is a geospatial dataset")
}
```

---

## 类型映射

类型映射统一通过 `TypeMapper` 注册表完成，不再使用旧的 `TypeMapping` facade。

```go
type TypeMapper interface {
    Name() string
    ToCommon(nativeType string) FieldType
    FromCommon(commonType FieldType) (nativeType string, size int, precision int)
}
```

**示例**：

```go
mapper := format.GetTypeMapper("postgresql")
commonType := mapper.ToCommon("varchar(255)")
// commonType == format.FieldTypeString

nativeType, _, _ := mapper.FromCommon(format.FieldTypeFloat)
// nativeType == "REAL"
```

Shapefile DBF 类型同样走注册表：

```go
mapper := format.GetTypeMapper("shapefile")
commonType := mapper.ToCommon("C")
// commonType == format.FieldTypeString

dbfType, size, precision := mapper.FromCommon(format.FieldTypeFloat)
// dbfType == "F"
// size == 13
// precision == 6
```

### 类型判断工具

```go
// 判断是否为数值类型
func IsNumeric(fieldType FieldType) bool

// 判断是否为时间类型
func IsTemporalType(fieldType FieldType) bool

// 判断是否为几何类型
func IsGeometryType(fieldType FieldType) bool
```

---

## 错误处理

### 错误常量

```go
var (
    ErrUnsupportedFormat = errors.New("unsupported format")
    ErrInvalidSchema     = errors.New("invalid schema")
    ErrFormatDetection   = errors.New("format detection failed")
    ErrInvalidMagicBytes = errors.New("invalid magic bytes")
    ErrEmptyFile         = errors.New("empty file")
    ErrCorruptedFile     = errors.New("corrupted file")
)
```

**示例**：

```go
import "errors"

formatType := format.DetectFormat(filename, peek)
if formatType == format.FormatUnknown {
    return format.ErrUnsupportedFormat
}

if err := schema.Validate(); err != nil {
    if errors.Is(err, format.ErrInvalidSchema) {
        // 处理无效Schema
    }
}
```

---

## 使用示例

### 示例1：在Meta模块中使用格式识别

```go
// meta/backend/internal/scanner/extractors/shapefile_extractor.go
package extractors

import (
    "github.com/addp/common/format"
    "github.com/addp/meta/internal/scanner"
)

type ShapefileExtractor struct{}

func (e *ShapefileExtractor) SupportedTypes() []string {
    return []string{
        format.FormatToMIME(format.FormatShapefile),
        "application/x-shapefile",
    }
}

func (e *ShapefileExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
    // 使用common/format识别格式
    formatType := format.DetectFormat(input.ObjectKey, input.PeekBytes)
    if formatType != format.FormatShapefile {
        return nil, format.ErrUnsupportedFormat
    }

    // 提取元数据...
}
```

### 示例2：使用类型映射转换字段类型

```go
mapper := format.GetTypeMapper("shapefile")
dbfType, size, precision := mapper.FromCommon(format.FieldTypeDouble)
// dbfType == "F"
// size == 20
// precision == 8
```

### 示例3：Manager 模块消费 Meta 标准格式

```go
// manager/backend/internal/service/preview_resolver.go
package service

func providerNamesForMeta(req *PreviewResolverRequest, legacyReq *PreviewRequest) []string {
    attrs := req.MetadataAttributes()
    item := section(attrs, "item")
    dataType := stringAttribute(item, "data_type")
    formatName := stringAttribute(item, "format")

    if dataType == "table" && isFileTableFormat(formatName) {
        return []string{"builtin:file-table"}
    }
    return []string{"builtin:object-storage"}
}
```

---

## 性能考虑

### 格式检测性能

- `DetectFormat` 的时间复杂度: **O(1)**
  - 文件扩展名检查: 字符串比较
  - Magic Bytes检查: 前512字节前缀匹配

- **建议**：
  - 如果文件名可信，peek参数可传nil（跳过Magic验证）
  - 对于大批量文件扫描，使用goroutine并发检测

### Schema验证性能

- `Schema.Validate()` 的时间复杂度: **O(n)**，n为字段数
- 仅在必要时调用（如用户输入、API接口）
- 内部数据流转可跳过验证

---

## 扩展指南

### 添加新格式支持

1. 在 `detection.go` 中添加 `FormatType` 常量
2. 更新 `extToFormat` 函数（扩展名映射）
3. 更新 `MIMEToFormat` 和 `FormatToMIME` 函数
4. （可选）添加Magic Bytes检测逻辑
5. 更新 `docs/FORMAT_SUPPORT_MATRIX.md`

### 添加新数据源类型映射

1. 实现 `TypeMapper`:
   ```go
   type OracleTypeMapper struct{}

   func (m *OracleTypeMapper) Name() string { return "oracle" }
   func (m *OracleTypeMapper) ToCommon(nativeType string) format.FieldType
   func (m *OracleTypeMapper) FromCommon(commonType format.FieldType) (string, int, int)
   ```

2. 在 mapper 包的 `init()` 中调用 `format.RegisterTypeMapper(...)`。

---

## 测试

运行单元测试：

```bash
cd common/format
go test -v ./...
```

测试覆盖率：

```bash
go test -cover ./...
```

---

## Provider 模型

`common/format` 当前提供第一版表格 Provider 入口：

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

多组件和 scope 表格来源通过扩展接口表达：

```go
type ComponentTableProvider interface {
    TableProvider
    DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *ParseOptions) (*TableInfo, error)
    SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}

type ScopeTableProvider interface {
    TableProvider
    DescribeTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, options *ParseOptions) (*TableInfo, error)
    SampleTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)
}
```

使用方式：

```go
import _ "github.com/addp/common/format/builtin"

provider, err := format.GetTableProvider(format.FormatParquet)
if err != nil {
    return err
}
info, err := provider.DescribeTable(ctx, input, nil)
```

内置注册包 `common/format/builtin` 会注册 CSV、Excel、JSON 空间表结构、Shapefile、Parquet 等 provider。  
JSON provider 位于 `common/format/json`，它处理 GeoJSON FeatureCollection 这类 JSON 空间表结构，但不会重新引入 `FormatGeoJSON`。

---

## FAQ

### Q1: 为什么不把所有格式解析器都放在这里？

**A**: `common/format` 可以承载跨模块共享的格式 Provider，但 Provider 边界必须克制：
- 不绑定 Manager preview DTO
- 不绑定 Meta item 归并逻辑
- 不直接接 engine id 或 engine 配置
- 不替 Transfer 做任务编排

### Q2: 什么时候应该使用 common/format？

**A**: 建议使用场景：
- ✅ 需要标准化的格式识别逻辑
- ✅ 需要跨数据源的类型映射
- ✅ 需要统一的Schema模型定义

不建议使用场景：
- ❌ 需要决定 item organization / claims
- ❌ 需要组装前端 preview DTO
- ❌ 需要直接连接 engine 或执行 transfer 任务

### Q3: 如何处理格式检测的边界情况？

**A**:
- 对于 `.json` 和 `.geojson` 文件，`DetectFormat` 都返回 `FormatJSON`；空间语义由上层通过 `capabilities.spatial` 表达
- 对于无扩展名的文件，只能依赖Magic Bytes检测
- 如果检测失败，返回 `FormatUnknown`，由调用方决定如何处理

---

## 相关文档

- [数据格式插件化架构设计](../../docs/数据格式插件化架构设计.md)
- [格式支持矩阵](../../docs/FORMAT_SUPPORT_MATRIX.md)
- [插件开发指南](../../docs/PLUGIN_DEVELOPMENT_GUIDE.md)

---

## 变更日志

| 版本 | 日期 | 变更内容 |
|------|------|----------|
| v0.1.0 | 2025-01-26 | 初始版本 |
