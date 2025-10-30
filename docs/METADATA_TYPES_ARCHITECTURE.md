# 元数据类型扩展架构

## 概述

ADDP Meta模块使用灵活的元数据存储架构，支持不同文件类型的专用元数据字段。

## 核心设计原理

### 1. JSONB存储

所有提取的元数据存储在PostgreSQL的JSONB字段中：

```sql
-- meta_item表
CREATE TABLE meta_item (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255),
    item_type VARCHAR(50),
    attributes JSONB,  -- 存储所有元数据
    ...
);
```

### 2. 元数据结构

每个文件的元数据包含：

```json
{
  "basic_info": {
    "file_name": "example.mp4",
    "file_type": "Video",
    "size": 1024000,
    "content_type": "video/mp4",
    "last_modified": "2024-01-01T00:00:00Z"
  },
  "schema_info": {
    "columns": [...],
    "row_count": 100
  },
  "custom_attrs": {
    "video_metadata": {
      "duration": 3600,
      "resolution": "1920x1080",
      "codec": "H.264"
    },
    "geo_metadata": {
      "geometry_type": "Polygon",
      "coordinate_system": "EPSG:4326",
      "bounding_box": [-122.5, 37.7, -122.3, 37.9]
    }
  }
}
```

### 3. 提取器接口

所有提取器实现统一的接口：

```go
type MetadataExtractor interface {
    // SupportedTypes 返回支持的MIME类型
    SupportedTypes() []string

    // Extract 从输入中提取元数据
    Extract(ctx context.Context, input ExtractInput) (*Metadata, error)

    // Priority 返回提取器优先级（数字越大优先级越高）
    Priority() int
}
```

## 添加新的文件类型支持

### 1. 创建提取器

```go
package extractors

import (
    "context"
    "path/filepath"
    "github.com/addp/meta/internal/scanner"
)

type MyTypeExtractor struct{}

func (e *MyTypeExtractor) SupportedTypes() []string {
    return []string{"application/x-mytype"}
}

func (e *MyTypeExtractor) Priority() int {
    return 50
}

func (e *MyTypeExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
    metadata := &scanner.Metadata{
        BasicInfo: scanner.BasicMetadata{
            FileName:     filepath.Base(input.ObjectKey),
            FileType:     "My Type",
            Size:         input.Size,
            ContentType:  input.ContentType,
            LastModified: input.LastModified,
        },
        CustomAttrs: make(map[string]interface{}),
    }

    // 提取专用元数据
    metadata.CustomAttrs["my_metadata"] = map[string]interface{}{
        "field1": "value1",
        "field2": 42,
    }

    return metadata, nil
}
```

### 2. 注册提取器

在 `meta/backend/plugins/extractors/register.go` 中：

```go
func init() {
    RegisterExtractor("mytype", &MyTypeExtractor{})
}
```

## 查询元数据

### PostgreSQL JSONB查询

```sql
-- 查询特定几何类型的文件
SELECT name
FROM metadata.meta_item
WHERE attributes->'custom_attrs'->'geo_metadata'->>'geometry_type' = 'Polygon';

-- 查询时长超过1小时的视频
SELECT name
FROM metadata.meta_item
WHERE (attributes->'custom_attrs'->'video_metadata'->>'duration')::int > 3600;

-- 查询特定坐标系的数据
SELECT name
FROM metadata.meta_item
WHERE attributes->'custom_attrs'->'geo_metadata'->>'coordinate_system' = 'EPSG:4326';
```

### 创建索引提高查询性能

```sql
-- 为常用查询字段创建GIN索引
CREATE INDEX idx_meta_item_attrs ON metadata.meta_item USING GIN (attributes);

-- 为特定路径创建表达式索引
CREATE INDEX idx_video_duration ON metadata.meta_item
  ((attributes->'custom_attrs'->'video_metadata'->>'duration'));
```

## 已实现的文件类型

| 文件类型 | 专用元数据字段 |
|---------|--------------|
| Shapefile | 几何类型、坐标系、边界框、要素数量 |
| GeoJSON | 几何类型、坐标系、边界框、要素数量 |
| 视频 | 时长、分辨率、编解码器、帧率、比特率 |
| 图像 | 宽度、高度、色彩空间、位深度、DPI |
| PDF | 页数、作者、标题、创建时间、关键词 |
| Office | 页数/幻灯片数/工作表数、作者、修改时间 |
| CSV | 行数、列数、列名、分隔符、编码 |
| SQLite | 表数量、表信息、行数、索引信息 |

## 架构优势

### 灵活性
- ✅ 新增文件类型无需修改数据库schema
- ✅ 每种文件类型可定义独特的元数据字段
- ✅ 支持嵌套的复杂数据结构

### 可查询性
- ✅ PostgreSQL JSONB支持索引
- ✅ 支持复杂的SQL查询和聚合
- ✅ 可以跨文件类型查询

### 可扩展性
- ✅ 插件式架构，易于添加新类型
- ✅ 提取器之间互不影响
- ✅ 支持优先级控制

## 相关文档

- [文件类型提取器](./FILE_TYPE_EXTRACTORS.md)
- [视频元数据提取](./VIDEO_METADATA_EXTRACTION_DONE.md)
- [插件架构重构](../PLUGIN_ARCHITECTURE_REFACTORING.md)
