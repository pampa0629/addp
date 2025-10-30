# 元数据存储架构说明

## 概述

针对不同类型文件的元数据信息（例如shapefile的空间范围、视频的时长和分辨率等）存储在 PostgreSQL 数据库的 **`metadata` schema** 中。

## 核心存储表

### 1. `metadata.meta_item` 表

这是存储所有文件级元数据的**主表**。

**表结构**：
```sql
Table "metadata.meta_item"
       Column        |           Type           | Description
---------------------+--------------------------+------------------------------------------
 id                  | bigint                   | 主键，自增ID
 tenant_id           | bigint                   | 租户ID（多租户隔离）
 res_id              | bigint                   | 资源ID（关联 system.resources 表）
 node_id             | bigint                   | 节点ID（关联 metadata.meta_node 表）
 item_type           | varchar(64)              | 项目类型（object, table, file等）
 name                | varchar(255)             | 文件名或对象名
 full_name           | text                     | 完整路径
 status              | varchar(32)              | 状态（active, deleted等）
 meta_schema_version | integer                  | 元数据架构版本
 row_count           | bigint                   | 行数（用于表）
 size_bytes          | bigint                   | 大小（字节）
 object_size_bytes   | bigint                   | 对象大小（对象存储）
 last_modified_at    | timestamptz              | 最后修改时间
 attributes          | jsonb                    | **核心字段：存储所有类型特定的元数据**
 sync_version        | bigint                   | 同步版本号
 source              | varchar(64)              | 数据源标识
 created_at          | timestamptz              | 创建时间
 updated_at          | timestamptz              | 更新时间
 deleted_at          | timestamptz              | 软删除时间
```

**关键点**：
- **`attributes` 字段（JSONB类型）** 是存储所有类型特定元数据的核心字段
- 使用 PostgreSQL JSONB 类型，支持高效的JSON查询和索引
- 默认值为 `{}`（空JSON对象）

## 元数据存储格式

### 标准存储格式（Typed Metadata）

对于通过SDK提取的类型化元数据（如GeoSpatial、Image、Document、Video等），`attributes` 字段采用以下结构：

```json
{
  "geo_spatial": {
    "_type": "geo.spatial",
    "_schema": {
      "$schema": "http://json-schema.org/draft-07/schema#",
      "type": "object",
      "properties": {
        "geometry_type": {"type": "string"},
        "coordinate_system": {"type": "string"},
        "bounding_box": {"type": "array", "items": {"type": "number"}},
        "feature_count": {"type": "integer"},
        "dimensions": {"type": "integer"},
        "spatial_index": {"type": "boolean"},
        "attributes": {"type": "array", "items": {"type": "string"}}
      }
    },
    "data": {
      "geometry_type": "Polygon",
      "coordinate_system": "EPSG:4326",
      "bounding_box": [73.5, 18.2, 135.1, 53.5],
      "feature_count": 34,
      "dimensions": 2,
      "spatial_index": false,
      "attributes": ["name", "population", "area"]
    }
  },
  "video_metadata": {
    "_type": "video.basic",
    "_schema": {...},
    "data": {
      "duration": 3600,
      "resolution": "1920x1080",
      "codec": "H.264",
      "bitrate": 5000000,
      "frame_rate": 29.97,
      "audio_codec": "AAC",
      "audio_channels": 2,
      "container": "MP4"
    }
  },
  "path": "addp/videos/sample.mp4",
  "bucket": "addp",
  "file_type": "mp4",
  "relative_path": "videos/sample.mp4"
}
```

**结构说明**：
- **顶层键**：每个键代表一类元数据（如 `geo_spatial`、`video_metadata`）
- **`_type`**：类型标识符（如 `"geo.spatial"`、`"video.basic"`）
- **`_schema`**：JSON Schema定义，用于验证和文档化
- **`data`**：实际的元数据内容（类型特定字段）

### 实际存储示例

#### 1. GeoJSON文件的元数据
```json
{
  "path": "addp/json/中国.geoJson",
  "bucket": "addp",
  "file_type": "geojson",
  "object_count": 1,
  "relative_path": "json/中国.geoJson",
  "last_modified_at": "2025-10-08T12:59:43.185Z",
  "geo_spatial": {
    "_type": "geo.spatial",
    "data": {
      "geometry_type": "MultiPolygon",
      "coordinate_system": "EPSG:4326",
      "bounding_box": [73.5, 18.2, 135.1, 53.5],
      "feature_count": 34,
      "dimensions": 2
    }
  }
}
```

#### 2. Shapefile的元数据
```json
{
  "path": "addp/shapes/roads.shp",
  "bucket": "addp",
  "file_type": "shapefile",
  "geo_spatial": {
    "_type": "geo.spatial",
    "data": {
      "geometry_type": "LineString",
      "coordinate_system": "Unknown (check .prj file)",
      "bounding_box": [116.3, 39.9, 116.5, 40.1],
      "feature_count": 0,
      "dimensions": 2,
      "spatial_index": false
    }
  },
  "prj_file_required": true,
  "associated_files_required": ["roads.dbf", "roads.shx", "roads.prj"],
  "note": "Full metadata extraction requires access to all shapefile components"
}
```

#### 3. 视频文件的元数据
```json
{
  "path": "addp/videos/demo.mp4",
  "bucket": "addp",
  "file_type": "mp4",
  "video_metadata": {
    "_type": "video.basic",
    "data": {
      "duration": 3600,
      "resolution": "1920x1080",
      "codec": "H.264",
      "bitrate": 5000000,
      "frame_rate": 29.97,
      "audio_codec": "AAC",
      "audio_channels": 2,
      "has_subtitles": false,
      "container": "MP4"
    }
  }
}
```

#### 4. 图片文件的元数据
```json
{
  "path": "addp/WechatIMG46.jpg",
  "bucket": "addp",
  "file_type": "jpg",
  "image_metadata": {
    "_type": "image.basic",
    "data": {
      "width": 1920,
      "height": 1080,
      "color_space": "RGB",
      "bit_depth": 8,
      "has_alpha": false,
      "compression": "JPEG",
      "dpi": 300
    }
  }
}
```

#### 5. 文档文件的元数据
```json
{
  "path": "addp/DataOps Cookbook（第三版）-20231219.docx",
  "bucket": "addp",
  "file_type": "docx",
  "document_metadata": {
    "_type": "document.basic",
    "data": {
      "title": "DataOps Cookbook",
      "author": "Unknown",
      "page_count": 350,
      "word_count": 85000,
      "language": "zh-CN",
      "creation_date": "2023-12-19T00:00:00Z"
    }
  }
}
```

## 查询元数据

### 1. 查询所有包含地理空间元数据的文件
```sql
SELECT
    id,
    name,
    attributes->'geo_spatial'->'data'->>'geometry_type' AS geometry_type,
    attributes->'geo_spatial'->'data'->'bounding_box' AS bounding_box
FROM metadata.meta_item
WHERE attributes ? 'geo_spatial';
```

### 2. 查询特定空间范围内的文件
```sql
SELECT
    id,
    name,
    attributes->'geo_spatial'->'data'->'bounding_box' AS bbox
FROM metadata.meta_item
WHERE
    attributes ? 'geo_spatial'
    AND (attributes->'geo_spatial'->'data'->'bounding_box'->>0)::float >= 73.0
    AND (attributes->'geo_spatial'->'data'->'bounding_box'->>2)::float <= 136.0;
```

### 3. 查询所有视频文件及其时长
```sql
SELECT
    id,
    name,
    attributes->'video_metadata'->'data'->>'duration' AS duration_seconds,
    attributes->'video_metadata'->'data'->>'resolution' AS resolution,
    attributes->'video_metadata'->'data'->>'codec' AS codec
FROM metadata.meta_item
WHERE attributes ? 'video_metadata';
```

### 4. 查询高分辨率视频（1080p及以上）
```sql
SELECT
    id,
    name,
    attributes->'video_metadata'->'data'->>'resolution' AS resolution
FROM metadata.meta_item
WHERE
    attributes ? 'video_metadata'
    AND attributes->'video_metadata'->'data'->>'resolution' ~ '(1920|2560|3840)x';
```

### 5. 查询所有图片文件及其尺寸
```sql
SELECT
    id,
    name,
    attributes->'image_metadata'->'data'->>'width' AS width,
    attributes->'image_metadata'->'data'->>'height' AS height,
    attributes->'image_metadata'->'data'->>'color_space' AS color_space
FROM metadata.meta_item
WHERE attributes ? 'image_metadata';
```

### 6. 查询特定类型的所有元数据类型
```sql
SELECT
    id,
    name,
    jsonb_object_keys(attributes) AS metadata_types
FROM metadata.meta_item
WHERE attributes != '{}'::jsonb;
```

### 7. 查询包含多种元数据类型的文件
```sql
SELECT
    id,
    name,
    jsonb_object_keys(attributes) AS metadata_types
FROM metadata.meta_item
WHERE
    attributes ? 'geo_spatial'
    AND attributes ? 'image_metadata';
```

## 索引优化

为了提高查询性能，可以在 `attributes` 字段上创建GIN索引：

```sql
-- 创建GIN索引（支持JSONB查询）
CREATE INDEX idx_meta_item_attributes_gin
ON metadata.meta_item
USING gin (attributes);

-- 创建表达式索引（优化特定键的查询）
CREATE INDEX idx_meta_item_geo_spatial
ON metadata.meta_item ((attributes->'geo_spatial'))
WHERE attributes ? 'geo_spatial';

CREATE INDEX idx_meta_item_video_metadata
ON metadata.meta_item ((attributes->'video_metadata'))
WHERE attributes ? 'video_metadata';
```

## 数据流程

### 1. 元数据提取流程
```
文件上传到MinIO
    ↓
Meta扫描服务发现新文件
    ↓
根据Content-Type选择Extractor
    ↓
Extractor提取元数据（调用SDK）
    ↓
SDK将TypedMetadata序列化为JSON
    ↓
存储到meta_item.attributes（JSONB）
```

### 2. 代码示例

**Extractor端（插件）**：
```go
// 在视频extractor中
videoMeta := &VideoMetadata{
    Duration:   3600,
    Resolution: "1920x1080",
    Codec:      "H.264",
}

metadata.AddTypedMetadata("video_metadata", videoMeta)
```

**SDK内部处理**：
```go
// sdk/metadata_registry.go
func (m *Metadata) AddTypedMetadata(key string, typedMeta TypedMetadata) {
    m.CustomAttrs[key] = SerializeTypedMetadata(typedMeta)
}

func SerializeTypedMetadata(metadata TypedMetadata) map[string]interface{} {
    return map[string]interface{}{
        "_type":   metadata.TypeName(),      // "video.basic"
        "_schema": metadata.Schema(),        // JSON Schema定义
        "data":    metadata.ToMap(),         // 实际数据
    }
}
```

**存储端（Meta Backend）**：
```go
// 转换SDK Metadata到内部Metadata
metadata := &Metadata{
    CustomAttrs: sdkMetadata.CustomAttrs,  // 包含序列化后的TypedMetadata
}

// 存储到数据库
item := &models.MetaItem{
    Name:       "demo.mp4",
    ItemType:   "object",
    Attributes: models.JSONMap(metadata.CustomAttrs),  // 直接存储到JSONB
}
db.Create(&item)
```

## 扩展性

### 添加新的元数据类型

1. **定义TypedMetadata结构**（在插件中）：
```go
type AudioMetadata struct {
    Duration     int     `json:"duration"`
    SampleRate   int     `json:"sample_rate"`
    Channels     int     `json:"channels"`
    Codec        string  `json:"codec"`
    Bitrate      int     `json:"bitrate"`
}

func (a *AudioMetadata) TypeName() string {
    return "audio.basic"
}

func (a *AudioMetadata) ToMap() map[string]interface{} {
    return map[string]interface{}{
        "duration":    a.Duration,
        "sample_rate": a.SampleRate,
        "channels":    a.Channels,
        "codec":       a.Codec,
        "bitrate":     a.Bitrate,
    }
}

// 实现其他必需方法...
```

2. **注册类型**（在插件的init()函数中）：
```go
func init() {
    sdk.RegisterMetadataType(&AudioMetadata{})
}
```

3. **在Extractor中使用**：
```go
audioMeta := &AudioMetadata{
    Duration:   180,
    SampleRate: 44100,
    Channels:   2,
    Codec:      "AAC",
}
metadata.AddTypedMetadata("audio_metadata", audioMeta)
```

4. **自动存储到数据库**：
   - 无需修改数据库schema
   - 无需修改Meta Backend代码
   - 数据自动存储到 `meta_item.attributes` JSONB字段

## 总结

### 核心要点

1. **存储位置**：
   - 数据库：PostgreSQL `addp` 数据库
   - Schema：`metadata` schema
   - 表：`meta_item` 表
   - 字段：`attributes` 列（JSONB类型）

2. **存储格式**：
   - TypedMetadata序列化为JSON
   - 包含 `_type`、`_schema`、`data` 三部分
   - 支持多种元数据类型共存

3. **查询能力**：
   - 使用JSONB操作符（`->`、`->>`、`?`等）
   - 支持复杂条件过滤
   - 支持GIN索引加速

4. **扩展性**：
   - 插件化架构，第三方可轻松扩展
   - 无需修改数据库schema
   - 类型自动注册和验证

### 相关文档

- [THIRD_PARTY_METADATA_TYPES.md](./THIRD_PARTY_METADATA_TYPES.md) - 第三方元数据类型扩展指南
- [METADATA_TYPES_ARCHITECTURE.md](./METADATA_TYPES_ARCHITECTURE.md) - 元数据类型架构详解
- [meta/backend/internal/plugins/videoextractor/](../meta/backend/internal/plugins/videoextractor/) - 视频元数据提取器示例
- [meta/backend/internal/scanner/extractors/shapefile_extractor.go](../meta/backend/internal/scanner/extractors/shapefile_extractor.go) - Shapefile元数据提取器示例
