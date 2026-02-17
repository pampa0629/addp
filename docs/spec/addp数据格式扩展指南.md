# 数据格式插件化架构示意图

## 整体架构

┌──────────────────────────────────────────────────────────────────────────────┐
│                          ADDP 数据格式插件化架构                             │
└──────────────────────────────────────────────────────────────────────────────┘

                     ┌─────────────────────────────────┐
                     │    common/format (共享工具)     │
                     │                                 │
                     │  ✓ 格式识别 (DetectFormat)      │
                     │  ✓ MIME转换 (MIMEToFormat)      │
                     │  ✓ Schema模型 (Field/Schema)    │
                     │  ✓ 类型映射 (TypeMapping)       │
                     │                                 │
                     │  支持25+种格式:                 │
                     │  - Shapefile, GeoJSON, CSV     │
                     │  - PDF, Image, SQLite          │
                     │  - PostgreSQL, MySQL, MongoDB  │
                     └─────────────────────────────────┘
                               ▲   ▲   ▲
                    可选使用   │   │   │   可选使用
                 ┌─────────────┘   │   └─────────────┐
                 │                 │                 │
┌────────────────▼───┐   ┌─────────▼──────┐   ┌─────▼────────────┐
│   Meta Module      │   │ Manager Module │   │ Transfer Module  │
│   (元数据扫描)      │   │  (数据预览)     │   │  (数据传输)      │
└────────────────────┘   └────────────────┘   └──────────────────┘
│ 核心需求:          │   │ 核心需求:       │   │ 核心需求:        │
│ - 快速扫描         │   │ - 用户预览      │   │ - 完整读写       │
│ - 提取元数据       │   │ - 快速响应      │   │ - 批量处理       │
│ - 构建索引         │   │ - 部分加载      │   │ - 流式传输       │
│                    │   │                 │   │                  │
│ 使用的 Parser:     │   │ 使用的 Parser:  │   │ 使用的 Parser:   │
│ ✓ FileTableParser  │   │ ✓ FileTableParser│  │ ✓ FileTableParser│
│ ✓ DBTableParser    │   │ ✓ DBTableParser  │  │ ✓ DBTableParser  │
│ ✓ DocCollectionParser⭐│ │ ✗ DocCollectionParser│ │                  │
│                    │   │   ⭐ 暂未实现    │  │  (暂未支持)      │
│ ✓ ObjectInfoParser │   │ ✓ ObjectInfoParser│ │                  │
└────────────────────┘   └─────────────────┘   └──────────────────┘


## 四种核心 Parser 接口 ⭐

ADDP 平台提供 **4 种** Parser 接口,覆盖不同数据源的元数据提取需求:

┌────────────────────────────────────────────────────────────────┐
│ 1️⃣  FileTableParser（文件表解析器）                           │
├────────────────────────────────────────────────────────────────┤
│ 用途: 从文件中提取表格结构元数据                              │
│ 支持格式: CSV、Shapefile、GeoJSON、Excel                     │
│ 核心方法:                                                      │
│   - ParseTableInfo(ctx, input, options) → TableInfo          │
│   - ReadPreview(ctx, input, offset, limit, options) → []map  │
│   - SupportedFormats() → []FormatType                        │
│                                                                │
│ 文件路径: common/format/interface.go (第 37-63 行)           │
│ 实现示例: common/format/csv/parser.go                        │
│          common/format/shapefile/parser.go                   │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ 2️⃣  DBTableParser（关系型数据库表解析器）                     │
├────────────────────────────────────────────────────────────────┤
│ 用途: 从关系型数据库表提取元数据                              │
│ 支持引擎: PostgreSQL、MySQL、ClickHouse、Doris               │
│ 核心方法:                                                      │
│   - ParseTableInfo(ctx, db, enginePlugin, schema, table)     │
│     → TableInfo                                               │
│   - SupportedEngineTypes() → []string                        │
│                                                                │
│ 文件路径: common/format/interface.go (第 11-35 行)           │
│ 实现示例: common/format/db/postgresql_parser.go              │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ 3️⃣  DocCollectionParser（文档型数据库集合解析器）⭐           │
├────────────────────────────────────────────────────────────────┤
│ 用途: 从 MongoDB、CouchDB 等文档数据库采样推断 Schema        │
│ 支持引擎: MongoDB                                             │
│ 核心方法:                                                      │
│   - ParseTableInfo(ctx, client, database, collection,        │
│                     options) → TableInfo                      │
│   - ReadPreview(ctx, client, database, collection,           │
│                  offset, limit, options) → []map             │
│   - SupportedEngineTypes() → []string                        │
│                                                                │
│ 关键特性:                                                      │
│   - 采样文档推断 Schema (默认采样 100 条)                    │
│   - 支持混合类型字段 (FieldTypeMixed)                        │
│   - 字段出现率统计 (OccurrenceRate)                          │
│   - 返回 DocCollectionInfo 扩展信息                          │
│                                                                │
│ 文件路径: common/format/interface.go (第 92-123 行)          │
│ 实现示例: common/format/document/mongodb_parser.go           │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ 4️⃣  ObjectInfoParser（对象信息解析器）                        │
├────────────────────────────────────────────────────────────────┤
│ 用途: 从对象存储文件提取扩展信息 (图片、视频、PDF)           │
│ 支持类型: Image (JPEG、PNG、GIF、TIFF)                       │
│          Video (MP4、AVI、MKV)                                │
│          Document (PDF)                                       │
│ 核心方法:                                                      │
│   - ParseObjectInfo(ctx, input, basicInfo) → ObjectInfo      │
│   - SupportedContentTypes() → []string                       │
│                                                                │
│ 文件路径: common/format/interface.go (第 65-81 行)           │
│ 实现示例: common/format/image/parser.go                      │
│          common/format/pdf/parser.go                         │
└────────────────────────────────────────────────────────────────┘


## 格式支持示例对比

### 示例 1: Shapefile（文件格式）

┌──────────────────────────────────────────────────────────────────────┐
│                    Shapefile 格式支持链路                            │
└──────────────────────────────────────────────────────────────────────┘

     用户上传 sample.shp 文件
              │
              ├────────────┬────────────┬────────────┐
              ▼            ▼            ▼            ▼
         ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐
         │  Meta   │  │ Manager │  │Transfer │  │ common  │
         │  扫描   │  │  预览   │  │  传输   │  │  工具   │
         └─────────┘  └─────────┘  └─────────┘  └─────────┘
              │            │            │            │
              ▼            ▼            ▼            ▼
    ┌─────────────────────────────────────────────────────┐
    │ 1. 格式识别 (common/format/DetectFormat)           │
    │    - 扩展名: .shp → FormatShapefile                │
    │    - Magic Bytes: 0x0000270a (验证)               │
    └─────────────────────────────────────────────────────┘
              │            │            │
              ▼            ▼            ▼
    ┌─────────────────────────────────────────────────────┐
    │ 2. 解析器 (common/geo/shapefile)                   │
    │    - 使用: FileTableParser 接口                    │
    │    - 读取 .shp (几何数据)                          │
    │    - 读取 .dbf (属性数据)                          │
    │    - 读取 .prj (坐标系)                            │
    └─────────────────────────────────────────────────────┘
              │            │            │
              ▼            ▼            ▼
    ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
    │Meta提取元数据│  │Manager预览  │  │Transfer读写 │
    │- 字段列表    │  │- 前100条记录│  │- 批量读取   │
    │- 记录数      │  │- 渲染地图   │  │- 字段映射   │
    │- 边界框      │  │- 属性表格   │  │- 格式转换   │
    │- SpatialInfo │  │             │  │             │
    └─────────────┘  └─────────────┘  └─────────────┘


### 示例 2: MongoDB Collection（文档数据库）⭐

┌──────────────────────────────────────────────────────────────────────┐
│                    MongoDB Collection 支持链路                       │
└──────────────────────────────────────────────────────────────────────┘

     用户创建 MongoDB 引擎 (mongodb://host:27017/mydb)
              │
              ├────────────┬────────────┬────────────┐
              ▼            ▼            ▼            ▼
         ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐
         │  Meta   │  │ Manager │  │ common  │  │  引擎   │
         │  扫描   │  │  预览   │  │  解析器 │  │  插件   │
         └─────────┘  └─────────┘  └─────────┘  └─────────┘
              │            │            │            │
              ▼            ▼            ▼            ▼
    ┌─────────────────────────────────────────────────────┐
    │ 1. 引擎识别 (common/engine/plugin)                 │
    │    - 引擎类型: "mongodb"                           │
    │    - 实现接口: NoSQLPlugin                         │
    │    - 创建客户端: mongo.Connect(ctx, options)       │
    └─────────────────────────────────────────────────────┘
              │            │            │
              ▼            ▼            ▼
    ┌─────────────────────────────────────────────────────┐
    │ 2. 解析器 (common/format/document/mongodb_parser)  │
    │    - 使用: DocCollectionParser 接口 ⭐             │
    │    - 采样文档 (默认 100 条)                        │
    │    - 推断字段类型 (支持混合类型)                   │
    │    - 统计字段出现率                                │
    │    - 提取索引信息                                  │
    └─────────────────────────────────────────────────────┘
              │            │            │
              ▼            ▼            ▼
    ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
    │Meta提取元数据│  │Manager预览  │  │ 扩展信息    │
    │- 字段列表    │  │- 前100条文档│  │DocCollection│
    │- 文档数      │  │- JSON渲染   │  │Info:        │
    │- 推断类型    │  │- 属性表格   │  │- IsSampled  │
    │- 出现率      │  │- 混合类型高亮│ │- SampleSize │
    │              │  │             │  │- SchemaType │
    │              │  │             │  │- Indexes    │
    └─────────────┘  └─────────────┘  └─────────────┘


## 统一的数据结构

### TableInfo（表元数据）

**用途**: 统一描述关系型表、文件表、文档集合的元数据

**适用范围**:
- 关系型数据库表 (PostgreSQL、MySQL)
- 文件表 (CSV、Shapefile、GeoJSON、Excel)
- 文档数据库集合 (MongoDB) ⭐

**核心字段**:
```
TableInfo {
    Name       string           // 表名/集合名/文件名
    RowCount   *int64           // 记录数/文档数
    Fields     []FieldInfo      // 字段列表
    PrimaryKey []string         // 主键字段 (MongoDB 为 ["_id"])
    Extensions []ExtensionInfo  // 扩展信息 (SpatialInfo、DocCollectionInfo 等)
}
```

**文件路径**: [common/format/info.go](../common/format/info.go)


### FieldInfo（字段信息）

**核心字段**:
```
FieldInfo {
    Name           string    // 字段名
    Type           FieldType // 标准化类型 (如 FieldTypeString、FieldTypeMixed ⭐)
    OriginalType   string    // 原生类型 (如 "varchar(255)"、"objectid")
    Nullable       bool      // 是否允许 NULL
    IsPrimaryKey   bool      // 是否为主键
    OccurrenceRate float64   // 出现率 (0.0-1.0，用于 MongoDB 灵活 Schema) ⭐
}
```


### FieldType 类型系统

**基础类型**:
- `string`, `int`, `bigint`, `float`, `double`, `decimal`, `bool`
- `date`, `time`, `timestamp`, `bytes`

**地理空间类型**:
- `geometry`, `point`, `linestring`, `polygon`, `multipoint`

**复杂类型**:
- `json`, `array`, `uuid`

**文档数据库特有类型** ⭐:
- `mixed` - 混合类型（MongoDB 中同一字段有多种类型）

**文件路径**: [common/format/schema.go](../common/format/schema.go) (第 10-40 行)


### ExtensionInfo 机制

**用途**: 为不同数据源提供特定的扩展信息

**已实现的扩展**:

| 扩展类型 | 适用范围 | 文件路径 |
|---------|---------|---------|
| **SpatialInfo** | Shapefile、GeoJSON、PostGIS 表 | common/format/spatial_info.go |
| **CSVInfo** | CSV 文件 | common/format/format_info.go |
| **ShapefileInfo** | Shapefile 文件 | common/format/format_info.go |
| **GeoJSONInfo** | GeoJSON 文件 | common/format/format_info.go |
| **ExcelInfo** | Excel 文件 | common/format/format_info.go |
| **ImageInfo** | 图片文件 (JPEG、PNG、TIFF) | common/format/media_info.go |
| **VideoInfo** | 视频文件 (MP4、AVI) | common/format/media_info.go |
| **PDFInfo** | PDF 文件 | common/format/media_info.go |
| **DocCollectionInfo** ⭐ | MongoDB Collection | common/format/document_info.go |

**DocCollectionInfo 详细说明** ⭐:
```
DocCollectionInfo {
    IsSampled      bool                // 是否为采样推断 (true: Schema 不完整)
    SampleSize     int                 // 采样大小 (采样的文档数量)
    SchemaType     string              // Schema 类型: "dynamic" (MongoDB)
    Indexes        []plugin.IndexInfo  // 索引列表
    TotalDocuments int64               // 总文档数
}
```

**使用示例**:
```go
// 提取 MongoDB Collection 的扩展信息
for _, ext := range tableInfo.Extensions {
    if docInfo, ok := ext.(*format.DocCollectionInfo); ok {
        fmt.Printf("采样大小: %d / 总文档数: %d\n",
                   docInfo.SampleSize, docInfo.TotalDocuments)
        fmt.Printf("索引数量: %d\n", len(docInfo.Indexes))
    }
}
```


## 插件注册机制对比

┌─────────────────────────────────────────────────────────────────┐
│                        Meta 模块                                │
├─────────────────────────────────────────────────────────────────┤
│ 使用的 Parser:                                                  │
│   ✓ FileTableParser（扫描对象存储中的文件）                   │
│   ✓ DBTableParser（扫描关系型数据库表）                       │
│   ✓ DocCollectionParser（扫描 MongoDB Collection）⭐           │
│   ✓ ObjectInfoParser（提取图片、视频、PDF 元数据）            │
│                                                                 │
│ 注册方式:                                                       │
│   format.RegisterFileTableParser(&ShapefileParser{})           │
│   format.RegisterDBTableParser(&PostgreSQLTableParser{})       │
│   format.RegisterDocCollectionParser(&MongoDBParser{}) ⭐       │
│   format.RegisterObjectInfoParser(&ImageParser{})              │
│                                                                 │
│ 注意: builtin/init.go 仅自动注册 FileTableParser 和            │
│   ObjectInfoParser；MongoDB DocCollectionParser 需在模块中     │
│   额外手动导入 "github.com/addp/common/format/document"        │
│                                                                 │
│ 使用方式:                                                       │
│   parser := format.GetFileTableParser(FormatShapefile)         │
│   tableInfo, err := parser.ParseTableInfo(ctx, input, opts)    │
│                                                                 │
│   mongoParser := format.GetDocCollectionParser("mongodb") ⭐    │
│   tableInfo, err := mongoParser.ParseTableInfo(ctx, client,    │
│                                  "mydb", "mycoll", opts)        │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      Manager 模块                               │
├─────────────────────────────────────────────────────────────────┤
│ 使用的 Parser:                                                  │
│   ✓ FileTableParser（预览文件数据）                           │
│   ✓ DBTableParser（预览数据库表）                             │
│   ✗ DocCollectionParser（预览 MongoDB Collection）⭐ 暂未实现  │
│                                                                 │
│ 注册方式:（同 Meta 模块，共享注册表）                          │
│                                                                 │
│ 使用方式:                                                       │
│   parser := format.GetFileTableParser(FormatCSV)               │
│   records, err := parser.ReadPreview(ctx, input, 0, 100, opts) │
│                                                                 │
│   mongoParser := format.GetDocCollectionParser("mongodb") ⭐    │
│   records, err := mongoParser.ReadPreview(ctx, client,         │
│                                  "mydb", "mycoll", 0, 100, opts)│
│   （以上 MongoDB 预览用法暂未实现）                             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                     Transfer 模块                               │
├─────────────────────────────────────────────────────────────────┤
│ 使用的 Parser:                                                  │
│   ✓ FileTableParser（读取源文件 Schema）                      │
│   ✓ DBTableParser（读取数据库表 Schema）                      │
│                                                                 │
│ 注册方式:（使用自己的 Reader/Writer 注册表）                   │
│   registry.MustRegisterConnector("shapefile",                  │
│       NewShapefileReader, NewShapefileWriter)                  │
│                                                                 │
│ 使用方式:                                                       │
│   factory := registry.GetReaderFactory("shapefile")            │
│   reader, err := factory(config)                               │
│   batch, err := reader.Read(ctx)                               │
└─────────────────────────────────────────────────────────────────┘


## TypeMapper（类型映射器）

**用途**: 在原生类型和通用类型之间进行双向转换

**支持的数据源**:

| 数据源 | Mapper 名称 | 文件路径 |
|--------|-----------|---------|
| PostgreSQL | `postgresql` | common/format/mappers/postgresql/type_mapper.go |
| MySQL | `mysql` | common/format/mappers/mysql/type_mapper.go |
| Shapefile DBF | `shapefile` | common/format/shapefile/type_mapper.go |
| SpatiaLite | `spatialite` | common/format/mappers/spatialite/type_mapper.go |
| MongoDB | `mongodb` | **暂未实现** ⭐ |

**MongoDB 类型映射示例** ⭐ (**暂未实现，仅文档说明**):

```
MongoDB BSON 类型   →   FieldType（通用类型）
──────────────────     ─────────────────────
string              →   FieldTypeString
int64               →   FieldTypeInt
float64             →   FieldTypeFloat
bool                →   FieldTypeBool
objectid            →   FieldTypeString (转换为 Hex 字符串)
datetime            →   FieldTypeTimestamp
array               →   FieldTypeArray
object              →   FieldTypeJSON
binary              →   FieldTypeBytes
decimal128          →   FieldTypeDecimal
mixed               →   FieldTypeMixed ⭐ (字段类型不一致)
```

**使用方式**（MongoDB TypeMapper **暂未实现**）:

```go
// 获取 MongoDB 类型映射器（暂未实现，以下为规划中的接口形式）
mapper := format.GetTypeMapper("mongodb")

// BSON 类型 → 通用类型
commonType := mapper.ToCommon("objectid")  // → FieldTypeString

// 通用类型 → BSON 类型
nativeType, size, precision := mapper.FromCommon(FieldTypeTimestamp)
// → ("datetime", 0, 0)
```

**文件路径**: [common/format/type_mapper.go](../common/format/type_mapper.go)


## 类型转换流程示例

┌──────────────────────────────────────────────────────────────────┐
│            PostgreSQL → Shapefile 类型转换                       │
└──────────────────────────────────────────────────────────────────┘

  PostgreSQL表                    通用类型                 Shapefile DBF
┌──────────────┐              ┌──────────────┐          ┌──────────────┐
│ id: integer  │──TypeMapping─→│ FieldTypeInt │─Mapping─→│ N(18,0)      │
│ name: varchar│──────────────→│ FieldTypeStr │─────────→│ C(254)       │
│ price: numeric│─────────────→│ FieldTypeDec │─────────→│ F(20,8)      │
│ active: bool │──────────────→│ FieldTypeBool│─────────→│ L(1)         │
│ created: date│──────────────→│ FieldTypeDate│─────────→│ D(8)         │
│ geom: point  │──────────────→│ FieldTypePoint│────────→│ GEOMETRY     │
└──────────────┘              └──────────────┘          └──────────────┘

  使用方法:
  mapper := format.GetTypeMapper("postgresql")
  commonType := mapper.ToCommon("integer")  // FieldTypeInt

  shpMapper := format.GetTypeMapper("shapefile")
  dbfType, size, prec := shpMapper.FromCommon(commonType)  // 'N', 18, 0


┌──────────────────────────────────────────────────────────────────┐
│            MongoDB → PostgreSQL 类型转换 ⭐                      │
└──────────────────────────────────────────────────────────────────┘

  MongoDB Collection              通用类型                 PostgreSQL表
┌──────────────┐              ┌──────────────┐          ┌──────────────┐
│ _id: objectid│──TypeMapping─→│ FieldTypeStr │─Mapping─→│ VARCHAR(24)  │
│ name: string │──────────────→│ FieldTypeStr │─────────→│ TEXT         │
│ age: int64   │──────────────→│ FieldTypeInt │─────────→│ INTEGER      │
│ price: decimal│─────────────→│ FieldTypeDec │─────────→│ NUMERIC      │
│ active: bool │──────────────→│ FieldTypeBool│─────────→│ BOOLEAN      │
│ created: date│──────────────→│ FieldTypeDate│─────────→│ DATE         │
│ data: object │──────────────→│ FieldTypeJSON│─────────→│ JSONB        │
│ tags: array  │──────────────→│ FieldTypeArray│────────→│ JSONB        │
│ value: mixed │──────────────→│ FieldTypeMixed│────────→│ JSONB ⭐     │
└──────────────┘              └──────────────┘          └──────────────┘

  使用方法（MongoDB TypeMapper **暂未实现**）:
  mongoMapper := format.GetTypeMapper("mongodb")
  commonType := mongoMapper.ToCommon("objectid")  // FieldTypeString

  pgMapper := format.GetTypeMapper("postgresql")
  pgType, _, _ := pgMapper.FromCommon(commonType)  // "VARCHAR(24)"


## 添加新格式支持流程

┌──────────────────────────────────────────────────────────────────┐
│                    添加新格式支持的决策树                        │
└──────────────────────────────────────────────────────────────────┘

                    我要添加新的数据格式支持
                              │
                              ▼
          ┌───────────────────┴───────────────────┐
          │  这是什么类型的数据源？               │
          └───────┬───────────────────────────────┘
                  │
        ┌─────────┼─────────┬─────────┬─────────┐
        ▼         ▼         ▼         ▼         ▼
    ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐
    │ 文件 │  │关系型│  │文档型│  │对象  │  │其他  │
    │ 格式 │  │数据库│  │数据库│  │存储  │  │      │
    └──┬───┘  └──┬───┘  └──┬───┘  └──┬───┘  └──────┘
       │         │         │         │
       ▼         ▼         ▼         ▼
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│实现接口: │ │实现接口: │ │实现接口: │ │实现接口: │
│FileTable │ │DBTable   │ │DocCollect│ │ObjectInfo│
│Parser    │ │Parser    │ │ionParser │ │Parser    │
└──────────┘ └──────────┘ └──────────┘ └──────────┘
       │         │         │⭐        │
       └─────────┴─────────┴──────────┘
                  │
                  ▼
         在 common/format/ 中实现解析器
                  │
                  ▼
         调用 format.Register*Parser()
                  │
                  ▼
         Meta/Manager 模块自动支持


### 示例 1: 添加 Parquet 文件支持

┌──────────────────────────────────────────────────────────────────┐
│                    添加 Parquet 支持示例                         │
└──────────────────────────────────────────────────────────────────┘

Step 1: 在 common/format 定义格式类型
┌────────────────────────────────────────────┐
│ const FormatParquet FormatType = "parquet" │
│                                            │
│ 文件: common/format/format_types.go       │
└────────────────────────────────────────────┘
                    │
                    ▼
Step 2: 更新格式识别逻辑
┌────────────────────────────────────────────────┐
│ func extToFormat(ext string) FormatType {     │
│     case ".parquet": return FormatParquet     │
│ }                                              │
│                                                │
│ func MIMEToFormat(mimeType string) FormatType │
│     case "application/x-parquet":             │
│         return FormatParquet                  │
│ }                                              │
│                                                │
│ 文件: common/format/detection.go              │
└────────────────────────────────────────────────┘
                    │
                    ▼
Step 3: 实现 FileTableParser 接口
┌────────────────────────────────────────────────┐
│ type ParquetParser struct{}                   │
│                                                │
│ func (p *ParquetParser) ParseTableInfo(       │
│     ctx, input, options) (*TableInfo, error)  │
│                                                │
│ func (p *ParquetParser) ReadPreview(          │
│     ctx, input, offset, limit, opts) (...)    │
│                                                │
│ func (p *ParquetParser) SupportedFormats()    │
│     []FormatType {                             │
│     return []FormatType{FormatParquet}        │
│ }                                              │
│                                                │
│ 文件: common/format/builtin/parquet_parser.go │
└────────────────────────────────────────────────┘
                    │
                    ▼
Step 4: 注册解析器
┌────────────────────────────────────────────────┐
│ func init() {                                  │
│     format.RegisterFileTableParser(           │
│         &ParquetParser{})                      │
│ }                                              │
│                                                │
│ 文件: common/format/builtin/parquet_parser.go │
└────────────────────────────────────────────────┘
                    │
                    ▼
        Meta/Manager 模块自动支持 Parquet


### 示例 2: 添加 CouchDB 支持 ⭐

┌──────────────────────────────────────────────────────────────────┐
│                    添加 CouchDB 支持示例                         │
└──────────────────────────────────────────────────────────────────┘

Step 1: 实现引擎插件
┌────────────────────────────────────────────────┐
│ type CouchDBPlugin struct{}                   │
│                                                │
│ 实现接口:                                      │
│   - EnginePlugin（基础接口）                  │
│   - StoragePlugin（存储引擎标记）             │
│   - NoSQLPlugin（NoSQL 专用接口）             │
│                                                │
│ 文件: common/engine/plugins/couchdb/plugin.go │
└────────────────────────────────────────────────┘
                    │
                    ▼
Step 2: 实现 DocCollectionParser 接口 ⭐
┌────────────────────────────────────────────────┐
│ type CouchDBCollectionParser struct{}         │
│                                                │
│ func (p *CouchDBCollectionParser)             │
│     ParseTableInfo(ctx, client, database,     │
│         collection, options) (*TableInfo,     │
│         error)                                 │
│                                                │
│ func (p *CouchDBCollectionParser)             │
│     ReadPreview(ctx, client, database,        │
│         collection, offset, limit, opts)      │
│         ([]map[string]interface{}, error)     │
│                                                │
│ func (p *CouchDBCollectionParser)             │
│     SupportedEngineTypes() []string {         │
│     return []string{"couchdb"}                │
│ }                                              │
│                                                │
│ 文件: common/format/document/couchdb_parser.go│
└────────────────────────────────────────────────┘
                    │
                    ▼
Step 3: 注册
┌────────────────────────────────────────────────┐
│ // 引擎插件注册                                │
│ func init() {                                  │
│     plugin.Register(&CouchDBPlugin{})         │
│ }                                              │
│                                                │
│ // Parser 注册                                 │
│ func init() {                                  │
│     format.RegisterDocCollectionParser(       │
│         &CouchDBCollectionParser{})           │
│ }                                              │
└────────────────────────────────────────────────┘
                    │
                    ▼
        Meta/Manager 模块自动支持 CouchDB


## 关键设计决策

┌──────────────────────────────────────────────────────────────────┐
│              为什么不统一注册表?                                 │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ❌ 统一注册表的问题:                                            │
│     1. Meta/Manager/Transfer 接口签名完全不同                   │
│     2. 性能优化策略差异巨大                                      │
│     3. 为了统一而引入过度抽象                                    │
│     4. 增加模块间耦合                                            │
│                                                                  │
│  ✅ 独立注册表的优势:                                            │
│     1. 各模块接口专注自己的需求                                  │
│     2. 可以针对模块特点优化性能                                  │
│     3. 模块间低耦合，易于独立部署                                │
│     4. 不需要为统一而妥协                                        │
│                                                                  │
│  🎯 解决方案: \"统一但不强制\"                                     │
│     - common/format 提供可选的共享工具                          │
│     - 各模块自主决定是否使用                                     │
│     - 通过文档和规范保证一致性                                   │
└──────────────────────────────────────────────────────────────────┘


┌──────────────────────────────────────────────────────────────────┐
│              为什么需要 4 种 Parser?                             │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1️⃣  FileTableParser: 文件格式解析                              │
│      - CSV、Shapefile、GeoJSON 等文件有固定 Schema             │
│      - 可以完整提取字段定义                                      │
│      - 适用于静态表结构                                          │
│                                                                  │
│  2️⃣  DBTableParser: 关系型数据库解析                            │
│      - 依赖 GORM 和引擎插件查询元数据                           │
│      - 使用 information_schema 或类似机制                       │
│      - 适用于结构化表                                            │
│                                                                  │
│  3️⃣  DocCollectionParser: 文档数据库解析 ⭐                      │
│      - MongoDB、CouchDB 等 Schema 灵活                          │
│      - 需要采样文档推断类型                                      │
│      - 支持混合类型字段 (FieldTypeMixed)                        │
│      - 统计字段出现率 (OccurrenceRate)                          │
│                                                                  │
│  4️⃣  ObjectInfoParser: 对象元数据解析                           │
│      - 图片、视频、PDF 等二进制文件                             │
│      - 提取特定扩展信息 (分辨率、时长、页数)                    │
│      - 不需要完整 Schema，只需扩展信息                          │
│                                                                  │
│  🎯 设计原则:                                                    │
│     - 接口分离：各 Parser 专注自己的场景                        │
│     - 统一输出：都返回 TableInfo 或 ObjectInfo                  │
│     - 扩展友好：通过 ExtensionInfo 机制支持任意扩展             │
└──────────────────────────────────────────────────────────────────┘


## 性能特征对比

┌─────────────────────────────────────────────────────────────────────┐
│                        性能需求对比                                 │
├─────────────────┬──────────────┬──────────────┬────────────────────┤
│      模块       │   Meta       │   Manager    │    Transfer        │
├─────────────────┼──────────────┼──────────────┼────────────────────┤
│ 数据加载量      │ 文件头部     │ 前100条记录  │ 完整数据           │
│                 │ 采样100文档⭐│              │                    │
│ 响应时间要求    │ < 1秒        │ < 2秒        │ 不限 (批处理)      │
│ 并发处理        │ 高并发扫描   │ 单用户请求   │ 高吞吐量队列       │
│ 内存占用        │ 最小化       │ 中等         │ 可控的流式处理     │
│ 缓存策略        │ Redis缓存    │ 内存缓存     │ 磁盘缓冲           │
│ 错误处理        │ 跳过失败文件 │ 返回错误提示 │ 重试机制           │
│ MongoDB 支持⭐  │ 采样推断Schema│预览100条文档│ (暂不支持)         │
└─────────────────┴──────────────┴──────────────┴────────────────────┘

结论: 无法用统一的实现满足所有模块的性能需求


## 核心文件路径参考

### 接口定义
```
common/format/
├── interface.go              # 4 种 Parser 接口定义 ⭐
├── info.go                   # TableInfo、ObjectInfo
├── schema.go                 # FieldType、Field、Schema 定义 ⭐
├── extension_info.go         # ExtensionInfo 标记接口
├── spatial_info.go           # SpatialInfo 扩展
├── format_info.go            # CSVInfo、ShapefileInfo、GeoJSONInfo 等
├── media_info.go             # ImageInfo、VideoInfo、PDFInfo
├── document_info.go          # DocCollectionInfo ⭐
├── type_mapper.go            # TypeMapper 注册表和实现 ⭐
├── registry.go               # Parser 注册表
├── detection.go              # 格式检测
└── builtin/init.go           # 自动注册所有内置解析器
```

### Parser 实现
```
common/format/csv/
└── parser.go                 # CSV FileTableParser

common/format/shapefile/
├── parser.go                 # Shapefile FileTableParser
└── type_mapper.go            # Shapefile TypeMapper

common/format/geojson/
└── parser.go                 # GeoJSON FileTableParser

common/format/excel/
└── parser.go                 # Excel FileTableParser

common/format/image/
└── parser.go                 # Image ObjectInfoParser

common/format/pdf/
└── parser.go                 # PDF ObjectInfoParser

common/format/db/
├── postgresql_parser.go      # PostgreSQL DBTableParser
└── mysql_parser.go           # MySQL DBTableParser

common/format/mappers/
├── postgresql/type_mapper.go # PostgreSQL TypeMapper
├── mysql/type_mapper.go      # MySQL TypeMapper
└── spatialite/type_mapper.go # SpatiaLite TypeMapper

common/format/document/
└── mongodb_parser.go         # MongoDB DocCollectionParser ⭐
```

### 模块集成
```
meta/backend/internal/service/
├── scan_database_service.go    # 使用 DBTableParser
├── scan_object_storage_service.go  # 使用 FileTableParser + ObjectInfoParser
└── scan_nosql_service.go       # 使用 DocCollectionParser ⭐

manager/backend/internal/service/
├── preview_provider_database.go   # 使用 DBTableParser
├── preview_provider_file.go       # 使用 FileTableParser
└── preview_provider_nosql.go      # 使用 DocCollectionParser ⭐ 暂未实现

transfer/backend/plugins/readers/
├── csv_reader.go               # 使用 FileTableParser
├── shapefile_reader.go         # 使用 FileTableParser
└── jdbc_reader.go              # 使用 DBTableParser
```


## 扩展阅读

- **引擎插件系统**: [docs/addp数据引擎扩展指南.md](addp数据引擎扩展指南.md)
- **各模块简要介绍**: [docs/concepts/addp各模块功能介绍.md](../concepts/addp各模块功能介绍.md)
- **共享模块介绍**: [docs/addp共享模块介绍.md](addp共享模块介绍.md)

---

**最后更新**: 2026-01-06
**版本**: v0.0.20
**维护者**: ADDP 开发团队
