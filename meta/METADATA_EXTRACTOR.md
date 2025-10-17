# 元数据提取器插件框架

## 概述

Meta 模块实现了一个**可扩展的元数据提取器插件框架**，用于从不同类型的文件中提取深度元数据。该框架采用**接口 + 注册表**模式，支持灵活添加新的文件类型支持。

## 架构设计

### 核心组件

```
meta/backend/internal/scanner/
├── plugin.go              # 提取器接口和数据结构定义
├── registry.go            # 全局注册表和提取器管理
└── extractors/            # 具体提取器实现
    ├── init.go           # 自动注册所有提取器
    ├── geojson_extractor.go
    ├── csv_extractor.go
    ├── image_extractor.go
    ├── pdf_extractor.go
    └── default_extractor.go  # 兜底提取器
```

### 关键接口

#### MetadataExtractor 接口

所有提取器必须实现此接口：

```go
type MetadataExtractor interface {
    // 返回支持的MIME类型列表
    SupportedTypes() []string

    // 从输入中提取元数据
    Extract(ctx context.Context, input ExtractInput) (*Metadata, error)

    // 返回优先级（数字越大优先级越高）
    Priority() int
}
```

#### 数据结构

**ExtractInput** - 提取输入：
- `ResourceID`: 数据源ID
- `ObjectKey`: 对象键/文件路径
- `ContentType`: MIME类型
- `Size`: 文件大小
- `Reader`: 内容读取器
- `Metadata`: 基础元数据（S3/MinIO返回的）
- `LastModified`: 最后修改时间
- `ETag`: 对象版本标识

**Metadata** - 提取结果：
- `BasicInfo`: 基础元数据（文件名、类型、大小等）
- `SchemaInfo`: 结构化数据的schema（列信息、行数等）
- `PreviewData`: 预览数据（可选）
- `CustomAttrs`: 自定义属性（扩展字段）

### 专用元数据类型

框架提供了针对不同数据类型的专用元数据结构：

- **GeoMetadata**: 地理空间数据（几何类型、坐标系、边界框等）
- **ImageMetadata**: 图像数据（宽高、格式、色彩空间等）
- **DocumentMetadata**: 文档数据（标题、作者、页数、字数等）

## 已实现的提取器

### 1. GeoJSONExtractor
- **支持类型**: `application/geo+json`, `application/vnd.geo+json`
- **优先级**: 100
- **功能**:
  - 提取几何类型（Point, LineString, Polygon等）
  - 提取坐标系统（默认EPSG:4326）
  - 提取边界框（bbox）
  - FeatureCollection的schema提取（列名、类型）
  - 前N行样本数据

### 2. CSVExtractor
- **支持类型**: `text/csv`, `application/csv`
- **优先级**: 100
- **功能**:
  - 自动检测分隔符（逗号、制表符、管道符）
  - 智能推断列类型（integer, number, boolean, date, string）
  - 提取表头和列信息
  - 统计总行数
  - 前N行样本数据

### 3. ImageExtractor
- **支持类型**: `image/*`（通配符）
- **优先级**: 50
- **功能**:
  - 提取图像尺寸（宽高）
  - 识别图像格式（JPEG, PNG, GIF等）
  - 推断色彩空间
  - 计算长宽比（识别常见比例如16:9）
  - 图像分类（缩略图、横幅、图标等）

### 4. PDFExtractor
- **支持类型**: `application/pdf`
- **优先级**: 80
- **功能**:
  - 提取PDF版本
  - 提取文档元数据（标题、作者、主题、关键词等）
  - 估算页数
  - 检测是否加密、是否包含表单

### 5. DefaultExtractor（兜底）
- **支持类型**: `*/*`（通配符）
- **优先级**: -100（最低）
- **功能**:
  - 根据文件扩展名推断文件类型
  - 计算MD5校验和（仅对小于100MB的文件）
  - 文件大小分类（tiny, small, medium, large等）
  - 文件分类（document, image, video, audio等）

## 提取器注册机制

### 自动注册

所有提取器在包加载时通过 `init()` 函数自动注册：

```go
// extractors/init.go
func init() {
    scanner.Register(&GeoJSONExtractor{})
    scanner.Register(&CSVExtractor{})
    scanner.Register(&ImageExtractor{})
    scanner.Register(&PDFExtractor{})
    scanner.Register(&DefaultExtractor{})
}
```

### 注册表功能

注册表提供以下API：

- `scanner.Register(extractor)`: 注册提取器
- `scanner.GetExtractor(contentType)`: 根据MIME类型获取最佳提取器
- `scanner.GetAllExtractors(contentType)`: 获取所有匹配的提取器
- `scanner.ListRegisteredTypes()`: 列出所有已注册的MIME类型
- `scanner.Count()`: 返回已注册的提取器总数

### MIME类型匹配规则

注册表支持三种匹配模式（按优先级）：

1. **完全匹配**: `image/jpeg` 完全匹配 `image/jpeg`
2. **主类型通配**: `image/jpeg` 匹配 `image/*`
3. **全通配**: 任何类型匹配 `*/*`

## 与扫描服务集成

### 扫描时标记

在对象存储扫描时，`persistObjectMetas()` 函数会：

1. 根据文件扩展名推断MIME类型
2. 查找匹配的提取器
3. 在元数据attributes中添加标记：
   - `extractor_available`: true
   - `extractor_type`: 提取器类型
   - `content_type`: MIME类型

```go
// scan_service_new.go
func (s *ScanServiceNew) extractEnhancedMetadata(resourceID uint, meta scanner.ObjectMetadata, baseAttrs models.JSONMap) models.JSONMap {
    contentType := mime.TypeByExtension(pathpkg.Ext(meta.Path))
    extractor := scanner.GetExtractor(contentType)

    if extractor != nil {
        baseAttrs["extractor_available"] = true
        baseAttrs["extractor_type"] = fmt.Sprintf("%T", extractor)
        baseAttrs["content_type"] = contentType
    }

    return baseAttrs
}
```

### 按需提取

提供 `ExtractObjectMetadataOnDemand()` 方法，用于Manager预览时按需提取深度元数据：

```go
func (s *ScanServiceNew) ExtractObjectMetadataOnDemand(
    tenantID, resourceID uint,
    objectKey string,
    token string,
    objectReader io.Reader,
) (*scanner.Metadata, error) {
    // 1. 检测内容类型
    contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))

    // 2. 获取提取器
    extractor := scanner.GetExtractor(contentType)

    // 3. 调用提取器
    metadata, err := extractor.Extract(ctx, input)

    // 4. 更新数据库中的attributes
    // ...

    return metadata, nil
}
```

## API接口

### 获取对象元数据

**Endpoint**: `GET /api/meta/metadata/object`

**Query Parameters**:
- `resource_id`: 数据源ID
- `object_key`: 对象键（如 `bucket/path/to/file.geojson`）

**Response**:
```json
{
  "data": {
    "id": 123,
    "name": "file.geojson",
    "item_type": "object",
    "attributes": {
      "bucket": "my-bucket",
      "path": "path/to/file.geojson",
      "extractor_available": true,
      "extractor_type": "*extractors.GeoJSONExtractor",
      "content_type": "application/geo+json",
      "extracted_metadata": {
        "basic_info": { ... },
        "custom_attrs": {
          "geo_metadata": {
            "geometry_type": "Point",
            "coordinate_system": "EPSG:4326",
            "feature_count": 100
          }
        }
      },
      "schema_info": {
        "columns": [
          {"name": "name", "type": "string"},
          {"name": "population", "type": "number"}
        ],
        "row_count": 100
      }
    }
  }
}
```

## 如何添加新的提取器

### 步骤1: 创建提取器文件

在 `meta/backend/internal/scanner/extractors/` 目录下创建新文件，如 `shapefile_extractor.go`:

```go
package extractors

import (
    "context"
    "github.com/addp/meta/internal/scanner"
)

type ShapefileExtractor struct{}

func (e *ShapefileExtractor) SupportedTypes() []string {
    return []string{"application/x-shapefile", "application/octet-stream"}
}

func (e *ShapefileExtractor) Priority() int {
    return 90 // 高优先级
}

func (e *ShapefileExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
    // 实现提取逻辑
    // ...

    return &scanner.Metadata{
        BasicInfo: scanner.BasicMetadata{
            FileName: input.ObjectKey,
            FileType: "Shapefile",
            // ...
        },
        CustomAttrs: map[string]interface{}{
            "geo_metadata": scanner.GeoMetadata{
                // ...
            },
        },
    }, nil
}
```

### 步骤2: 注册提取器

在 `extractors/init.go` 中添加注册：

```go
func init() {
    // ...existing extractors...
    scanner.Register(&ShapefileExtractor{})
}
```

### 步骤3: 编译测试

```bash
cd meta/backend
go build ./cmd/server/main.go
```

## 性能考虑

### 扫描时策略

**当前实现**：扫描时**不下载完整文件**，只标记可用的提取器

- **优点**: 扫描速度快，不占用带宽
- **缺点**: 元数据不完整，需要后续按需提取

### 按需提取策略

Manager模块预览时，可以选择性地调用按需提取：

- **小文件**（< 10MB）: 实时提取元数据
- **大文件**（> 10MB）: 异步提取，显示加载状态
- **已提取**: 直接从数据库读取缓存

### 未来优化方向

1. **智能预提取**: 根据文件类型和大小，在扫描时有选择地提取元数据
2. **增量提取**: 只提取变化的文件
3. **后台任务**: 使用任务队列异步提取大量文件的元数据
4. **缓存策略**: 使用Redis缓存热点文件的元数据

## 扩展建议

### 未来可添加的提取器

1. **ParquetExtractor**: Apache Parquet列式存储格式
2. **ExcelExtractor**: Excel文件（.xlsx, .xls）
3. **WordExtractor**: Word文档（.docx, .doc）
4. **VideoExtractor**: 视频文件（编解码器、分辨率、时长）
5. **AudioExtractor**: 音频文件（采样率、比特率、时长）
6. **ArchiveExtractor**: 压缩包（文件列表、压缩率）
7. **SQLExtractor**: SQL脚本（DDL分析）
8. **JSONExtractor**: 通用JSON（schema推断）
9. **XMLExtractor**: XML文档（schema提取）
10. **NetCDFExtractor**: 科学数据格式

### 元数据版本控制

建议在 `meta_item` 表的 `attributes` 字段中添加版本号：

```json
{
  "meta_schema_version": 2,
  "extracted_metadata": { ... }
}
```

当提取器逻辑升级时，可以批量重新提取旧版本的元数据。

## 相关文件

- **接口定义**: [meta/backend/internal/scanner/plugin.go](meta/backend/internal/scanner/plugin.go)
- **注册表**: [meta/backend/internal/scanner/registry.go](meta/backend/internal/scanner/registry.go)
- **提取器**: [meta/backend/internal/scanner/extractors/](meta/backend/internal/scanner/extractors/)
- **扫描服务集成**: [meta/backend/internal/service/scan_service_new.go:851](meta/backend/internal/service/scan_service_new.go)
- **API Handler**: [meta/backend/internal/api/handler.go:25](meta/backend/internal/api/handler.go)
- **路由配置**: [meta/backend/internal/api/router_new.go:59](meta/backend/internal/api/router_new.go)

## 总结

元数据提取器插件框架实现了：

✅ **可扩展性**: 通过接口+注册表模式，轻松添加新的文件类型支持
✅ **灵活性**: 支持MIME类型通配符匹配，优先级控制
✅ **职责分离**: Meta专注于元数据提取，Manager专注于数据展示
✅ **性能优化**: 扫描时标记+按需提取的混合策略
✅ **类型安全**: 提供专用的元数据结构（Geo, Image, Document等）

该框架为ADDP平台的数据探查和预览功能提供了坚实的元数据基础设施。
