# common/format - 格式识别和Schema工具

本包提供跨模块共享的数据格式识别、类型转换和Schema标准化工具。

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

**设计原则**：
- ✅ 提供**辅助工具**，不强制使用
- ✅ 各模块可选择使用或自行实现
- ✅ 最小化外部依赖（仅依赖Go标准库）

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

### TypeMapping 结构

```go
type TypeMapping struct{}
```

### PostgreSQL 类型转换

```go
// PostgreSQL → 通用类型
func (m *TypeMapping) PostgreSQLToCommon(pgType string) FieldType

// 通用类型 → PostgreSQL
func (m *TypeMapping) CommonToPostgreSQL(commonType FieldType) string
```

**示例**：

```go
mapper := &format.TypeMapping{}

// PostgreSQL → 通用类型
commonType := mapper.PostgreSQLToCommon("varchar(255)")
// commonType == format.FieldTypeString

commonType = mapper.PostgreSQLToCommon("geometry(Point, 4326)")
// commonType == format.FieldTypeGeometry

// 通用类型 → PostgreSQL
pgType := mapper.CommonToPostgreSQL(format.FieldTypeFloat)
// pgType == "DOUBLE PRECISION"
```

### MySQL 类型转换

```go
// MySQL → 通用类型
func (m *TypeMapping) MySQLToCommon(mysqlType string) FieldType
```

**示例**：

```go
mapper := &format.TypeMapping{}

commonType := mapper.MySQLToCommon("datetime")
// commonType == format.FieldTypeTimestamp

commonType = mapper.MySQLToCommon("tinyint(1)")
// commonType == format.FieldTypeBool (MySQL的布尔类型)
```

### Shapefile DBF 类型转换

```go
// Shapefile DBF → 通用类型
func (m *TypeMapping) ShapefileDBFToCommon(dbfType byte) FieldType

// 通用类型 → Shapefile DBF
func (m *TypeMapping) CommonToShapefileDBF(commonType FieldType) (dbfType byte, size uint8, precision uint8)
```

**示例**：

```go
mapper := &format.TypeMapping{}

// DBF → 通用类型
commonType := mapper.ShapefileDBFToCommon('C')
// commonType == format.FieldTypeString

// 通用类型 → DBF
dbfType, size, precision := mapper.CommonToShapefileDBF(format.FieldTypeFloat)
// dbfType == 'F' (Float)
// size == 20 (总长度)
// precision == 8 (小数位数)
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

### 示例2：在Transfer模块中使用Schema转换

```go
// transfer/backend/internal/connector/shapefile_writer.go
package connector

import (
    "github.com/addp/common/format"
    "github.com/addp/transfer/pkg/pipeline"
)

func (w *ShapefileWriter) convertSchema(inputSchema *pipeline.Schema) error {
    mapper := &format.TypeMapping{}

    for _, field := range inputSchema.Fields {
        // 将通用类型转换为Shapefile DBF类型
        dbfType, size, precision := mapper.CommonToShapefileDBF(field.Type)

        // 创建DBF字段...
    }

    return nil
}
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

1. 在 `TypeMapping` 结构中添加新方法:
   ```go
   func (m *TypeMapping) OracleToCommon(oracleType string) FieldType
   func (m *TypeMapping) CommonToOracle(commonType FieldType) string
   ```

2. 参考现有的 `PostgreSQLToCommon` 实现

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

## FAQ

### Q1: 为什么不把所有格式解析器都放在这里？

**A**: `common/format` 只提供**格式识别和类型转换**工具，不包含具体的解析逻辑。原因：
- 各模块性能需求差异大（Meta需要快速扫描，Transfer需要高吞吐量）
- 解析器依赖第三方库，会增加common的依赖复杂度
- 各模块可根据需求选择最优实现

### Q2: 什么时候应该使用 common/format？

**A**: 建议使用场景：
- ✅ 需要标准化的格式识别逻辑
- ✅ 需要跨数据源的类型映射
- ✅ 需要统一的Schema模型定义

不建议使用场景：
- ❌ 模块有特殊的性能优化需求
- ❌ 格式检测需要读取大量文件内容（超过512字节）
- ❌ 需要格式特定的复杂验证逻辑

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
