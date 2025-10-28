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
                     │  - PostgreSQL, MySQL           │
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
│ 注册表:            │   │ 注册表:         │   │ 注册表:          │
│ MetadataExtractor  │   │ PreviewProvider │   │ Reader/Writer    │
│                    │   │                 │   │                  │
│ 已实现插件:        │   │ 已实现插件:     │   │ 已实现插件:      │
│ ✓ GeoJSONExtractor │   │ ✓ ObjectStorage │   │ ✓ Shapefile R/W  │
│ ✓ CSVExtractor     │   │ ✓ PostgresPrevi │   │ ✓ GeoJSON R/W    │
│ ✓ PDFExtractor     │   │ ✓ NodePreview   │   │ ✓ GeoPackage R/W │
│ ✓ ImageExtractor   │   │                 │   │ ✓ JDBC R/W       │
│ ✓ SQLiteExtractor  │   │                 │   │ ✓ S3 R/W         │
└────────────────────┘   └─────────────────┘   └──────────────────┘


## 格式支持示例：Shapefile

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
    └─────────────┘  └─────────────┘  └─────────────┘


## 插件注册机制对比

┌─────────────────────────────────────────────────────────────────┐
│                        Meta 模块                                │
├─────────────────────────────────────────────────────────────────┤
│ 1. 实现接口:                                                    │
│    type MetadataExtractor interface {                          │
│        SupportedTypes() []string                               │
│        Priority() int                                          │
│        Extract(ctx, input) (*Metadata, error)                  │
│    }                                                            │
│                                                                 │
│ 2. 注册方式:                                                    │
│    func init() {                                               │
│        scanner.Register(&GeoJSONExtractor{})                   │
│    }                                                            │
│                                                                 │
│ 3. 使用方式:                                                    │
│    extractor := scanner.GetExtractor("application/geo+json")   │
│    metadata, err := extractor.Extract(ctx, input)              │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      Manager 模块                               │
├─────────────────────────────────────────────────────────────────┤
│ 1. 实现接口:                                                    │
│    type PreviewProvider interface {                            │
│        Name() string                                           │
│        Priority() int                                          │
│        Supports(*PreviewRequest) bool                          │
│        Preview(ctx, *PreviewRequest) (*TablePreview, error)    │
│    }                                                            │
│                                                                 │
│ 2. 注册方式:                                                    │
│    registry := NewPreviewRegistry()                            │
│    registry.Register(newObjectStoragePreviewProvider())        │
│                                                                 │
│ 3. 使用方式:                                                    │
│    provider, err := registry.Resolve(req)                      │
│    preview, err := provider.Preview(ctx, req)                  │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                     Transfer 模块                               │
├─────────────────────────────────────────────────────────────────┤
│ 1. 实现接口:                                                    │
│    type Reader interface {                                     │
│        Open(ctx, config) error                                 │
│        Read(ctx) (*DataBatch, error)                           │
│        Schema() (*Schema, error)                               │
│        Close() error                                           │
│    }                                                            │
│                                                                 │
│ 2. 注册方式:                                                    │
│    func init() {                                               │
│        MustRegisterConnector("shapefile",                      │
│            NewShapefileReader, NewShapefileWriter)             │
│    }                                                            │
│                                                                 │
│ 3. 使用方式:                                                    │
│    factory := registry.GetReaderFactory("shapefile")           │
│    reader, err := factory(config)                              │
│    batch, err := reader.Read(ctx)                              │
└─────────────────────────────────────────────────────────────────┘


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
  mapper := &format.TypeMapping{}
  commonType := mapper.PostgreSQLToCommon("integer")  // FieldTypeInt
  dbfType, size, prec := mapper.CommonToShapefileDBF(commonType)  // 'N', 18, 0


## 添加新格式支持流程

┌──────────────────────────────────────────────────────────────────┐
│                    添加 Parquet 支持示例                         │
└──────────────────────────────────────────────────────────────────┘

Step 1: 在 common/format 定义格式类型
┌────────────────────────────────────────────┐
│ const FormatParquet FormatType = "parquet" │
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
└────────────────────────────────────────────────┘
                    │
        ┌───────────┴───────────┬───────────┐
        ▼                       ▼           ▼
┌───────────────┐      ┌────────────────┐  ┌────────────────┐
│ Meta Module   │      │ Manager Module │  │Transfer Module │
└───────────────┘      └────────────────┘  └────────────────┘
        │                       │                   │
        ▼                       ▼                   ▼
Step 3: 实现插件    Step 3: 实现插件       Step 3: 实现插件
┌───────────────┐      ┌────────────────┐  ┌────────────────┐
│ParquetExtractor│     │ParquetPreview  │  │ParquetReader   │
│implements      │     │Provider        │  │implements      │
│MetadataExtractor│    │implements      │  │pipeline.Reader │
└───────────────┘      │PreviewProvider │  └────────────────┘
        │              └────────────────┘           │
        ▼                       ▼                   ▼
Step 4: 注册        Step 4: 注册            Step 4: 注册
┌───────────────┐      ┌────────────────┐  ┌────────────────┐
│scanner.Register│     │registry.Register│ │MustRegister    │
│(&ParquetExt{}) │     │(newParquet())   │ │Connector(...)  │
└───────────────┘      └────────────────┘  └────────────────┘
        │                       │                   │
        └───────────────────────┴───────────────────┘
                                │
                                ▼
                Step 5: 更新文档
        ┌────────────────────────────────────┐
        │ docs/FORMAT_SUPPORT_MATRIX.md     │
        │ | Parquet | ✅ | ✅ | ✅ | ✅ |    │
        └────────────────────────────────────┘


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
│  🎯 解决方案: "统一但不强制"                                     │
│     - common/format 提供可选的共享工具                          │
│     - 各模块自主决定是否使用                                     │
│     - 通过文档和规范保证一致性                                   │
└──────────────────────────────────────────────────────────────────┘


## 性能特征对比

┌─────────────────────────────────────────────────────────────────────┐
│                        性能需求对比                                 │
├─────────────────┬──────────────┬──────────────┬────────────────────┤
│      模块       │   Meta       │   Manager    │    Transfer        │
├─────────────────┼──────────────┼──────────────┼────────────────────┤
│ 数据加载量      │ 文件头部     │ 前100条记录  │ 完整数据           │
│ 响应时间要求    │ < 1秒        │ < 2秒        │ 不限 (批处理)      │
│ 并发处理        │ 高并发扫描   │ 单用户请求   │ 高吞吐量队列       │
│ 内存占用        │ 最小化       │ 中等         │ 可控的流式处理     │
│ 缓存策略        │ Redis缓存    │ 内存缓存     │ 磁盘缓冲           │
│ 错误处理        │ 跳过失败文件 │ 返回错误提示 │ 重试机制           │
└─────────────────┴──────────────┴──────────────┴────────────────────┘

结论: 无法用统一的实现满足所有模块的性能需求
