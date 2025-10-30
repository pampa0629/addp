# 文件类型元数据提取器插件

本文档列出了ADDP系统支持的所有文件类型元数据提取器插件。所有插件实现统一的 `MetadataExtractor` 接口。

## 架构概述

```
Meta Service (元数据服务)
  ↓
Plugin Registry (meta/backend/plugins/registry.go)
  ↓
Extractors (元数据提取器)
  └── Built-in Extractors (meta/backend/plugins/extractors/*.go)
```

## 已实现的文件类型提取器

### 1. Shapefile提取器 (shapefile-extractor)

**位置**: `meta/backend/plugins/extractors/shapefile_extractor.go`

**支持格式**:
- `application/x-shapefile` (.shp)

**提取元数据**:
- 几何类型 (点、线、面等)
- 要素数量
- 坐标系统 (WGS84, EPSG代码)
- 边界框 (BoundingBox)
- 属性字段列表

**优先级**: 90

### 2. 视频提取器 (video-extractor)

**位置**: `meta/backend/plugins/extractors/video_extractor.go`

**支持格式**:
- `video/mp4` (.mp4)
- `video/quicktime` (.mov)
- `video/x-msvideo` (.avi)
- `video/x-matroska` (.mkv)

**提取元数据**:
- 视频时长 (Duration)
- 分辨率 (Width x Height)
- 编解码器 (Codec)
- 比特率 (Bitrate)
- 帧率 (FrameRate)
- 音频流信息

**技术实现**:
- 使用FFmpeg/ffprobe进行精确元数据提取
- 支持MP4 box结构解析作为备用方案

**优先级**: 80

### 3. PDF提取器 (pdf-extractor)

**位置**: `meta/backend/plugins/extractors/pdf_extractor.go`

**支持格式**:
- `application/pdf` (.pdf)

**提取元数据**:
- PDF版本
- 页数 (PageCount)
- 标题、作者、主题
- 关键词 (Keywords)
- 创建者、生产者
- 创建时间、修改时间
- 是否加密
- 是否包含表单

**技术实现**:
- 解析PDF Info字典
- 解析PDF Catalog页面树

**优先级**: 75

### 4. 图像提取器 (image-extractor)

**位置**: `meta/backend/plugins/extractors/image_extractor.go`

**支持格式**:
- `image/jpeg` (.jpg, .jpeg)
- `image/png` (.png)
- `image/gif` (.gif)
- `image/bmp` (.bmp)
- `image/webp` (.webp)
- `image/tiff` (.tiff)

**提取元数据**:
- 图像宽度和高度
- 图像格式
- 色彩空间 (RGB, RGBA, Grayscale等)
- 位深度 (BitDepth)
- 是否有Alpha通道
- 分辨率字符串
- 宽高比

**技术实现**:
- 使用Go标准库 `image.DecodeConfig`
- 无需完整解码图像，快速提取配置信息

**优先级**: 70

### 5. CSV提取器 (csv-extractor)

**位置**: `meta/backend/plugins/extractors/csv_extractor.go`

**支持格式**:
- `text/csv` (.csv)
- `application/csv`
- `text/comma-separated-values`

**提取元数据**:
- 行数 (RowCount)
- 列数 (ColumnCount)
- 列名列表 (Columns)
- 分隔符 (Delimiter)
- 是否有表头 (HasHeader)
- 字符编码 (Encoding)

**技术实现**:
- 使用Go标准库 `encoding/csv`
- 启发式检测表头 (非空、非纯数字)
- 限制最多读取1000行以提高性能

**优先级**: 60

### 6. GeoJSON提取器 (geojson-extractor)

**位置**: `meta/backend/plugins/extractors/geojson_extractor.go`

**支持格式**:
- `application/geo+json` (.geojson)
- `application/vnd.geo+json`
- `application/json` (如果是GeoJSON格式)

**提取元数据**:
- GeoJSON类型 (FeatureCollection, Feature, Geometry等)
- 几何类型 (Point, LineString, Polygon等)
- 要素数量 (FeatureCount)
- 坐标系统 (默认EPSG:4326 WGS84)
- 边界框 (BoundingBox)
- 属性列表 (Attributes)
- 维度 (2D/3D)

**技术实现**:
- JSON解析和GeoJSON规范验证
- 递归计算边界框
- 支持多种GeoJSON对象类型

**优先级**: 70

### 7. SQLite提取器 (sqlite-extractor)

**位置**: `meta/backend/plugins/extractors/sqlite_extractor.go`

**支持格式**:
- `application/vnd.sqlite3` (.db, .sqlite, .sqlite3)
- `application/x-sqlite3`
- `application/octet-stream` (SQLite文件)

**提取元数据**:
- SQLite版本
- 表数量 (TableCount)
- 表信息列表 (Tables)
  - 表名
  - 行数
  - 列信息 (名称、类型、约束)
  - 主键列表
  - 索引列表
- 总行数 (TotalRows)
- 数据库文件大小
- 页大小 (PageSize)
- 字符编码

**技术实现**:
- 使用 `github.com/mattn/go-sqlite3` SQLite驱动
- 通过PRAGMA命令查询表结构
- 创建临时文件以供SQLite访问

**优先级**: 65

### 8. Office文档提取器 (office-extractor)

**位置**: `meta/backend/plugins/extractors/office_extractor.go`

**支持格式**:
- `application/vnd.openxmlformats-officedocument.wordprocessingml.document` (.docx)
- `application/vnd.openxmlformats-officedocument.presentationml.presentation` (.pptx)
- `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` (.xlsx)
- `application/vnd.ms-word.document.macroEnabled.12` (.docm)
- `application/vnd.ms-powerpoint.presentation.macroEnabled.12` (.pptm)
- `application/vnd.ms-excel.sheet.macroEnabled.12` (.xlsm)

**提取元数据**:
- 文档类型 (docx/pptx/xlsx)
- 标题、作者、主题
- 关键词 (Keywords)
- 描述信息
- 创建者、最后修改者
- 创建时间、修改时间
- 修订版本号
- **DOCX特有**: 页数、字数、字符数
- **PPTX特有**: 幻灯片数量
- **XLSX特有**: 工作表数量

**技术实现**:
- Office文档是ZIP格式，使用 `archive/zip` 解压
- 解析 `docProps/core.xml` (核心属性)
- 解析 `docProps/app.xml` (应用程序属性)
- 解析 `xl/workbook.xml` (Excel工作表信息)

**优先级**: 55

## 插件注册

所有插件在Meta服务启动时通过 [`meta/backend/plugins/extractors/register.go`](../meta/backend/plugins/extractors/register.go) 自动注册。

```go
func init() {
    RegisterExtractor("shapefile", &ShapefileExtractor{})
    RegisterExtractor("video", &VideoExtractor{})
    RegisterExtractor("pdf", &PDFExtractor{})
    RegisterExtractor("image", &ImageExtractor{})
    RegisterExtractor("csv", &CSVExtractor{})
    RegisterExtractor("geojson", &GeoJSONExtractor{})
    RegisterExtractor("sqlite", &SQLiteExtractor{})
    RegisterExtractor("office", &OfficeExtractor{})
}
```

## 元数据存储

所有提取的元数据存储在PostgreSQL `metadata` schema的 `meta_item` 表中，`attributes` 字段为JSONB类型，结构如下：

```json
{
  "video_metadata": {
    "_type": "video.metadata",
    "_schema": { ... },
    "data": {
      "duration": 48.0,
      "width": 1920,
      "height": 1080,
      "codec": "h264",
      "bitrate": 5000000,
      "frame_rate": 30.0
    }
  },
  "custom_field": "value"
}
```

## 前端展示

Manager模块的数据预览界面读取 `meta_item.attributes` 字段，并通过类型化组件展示不同类型的元数据：

- **视频**: [`VideoPreview.vue`](../manager/frontend/src/components/previews/VideoPreview.vue)
- **PDF**: `PDFPreview.vue`
- **图像**: [`ImagePreview.vue`](../manager/frontend/src/components/previews/ImagePreview.vue)
- **Office**: `OfficePreview.vue`
- **通用**: [`ExtractedMetadata.vue`](../manager/frontend/src/components/previews/ExtractedMetadata.vue)

## 添加新的文件类型提取器

如需添加新的文件类型支持，请遵循以下步骤：

### 1. 创建提取器文件

```bash
cd meta/backend/plugins/extractors
touch mytype_extractor.go
```

### 2. 实现提取器接口

在 `mytype_extractor.go` 中实现 `scanner.MetadataExtractor` 接口：

```go
package extractors

import (
    "context"
    "path/filepath"

    "github.com/addp/meta/internal/scanner"
)

// MyTypeExtractor 自定义类型提取器
type MyTypeExtractor struct{}

func (e *MyTypeExtractor) SupportedTypes() []string {
    return []string{"application/x-mytype"}
}

func (e *MyTypeExtractor) Priority() int {
    return 50
}

func (e *MyTypeExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
    // 创建基础元数据
    metadata := &scanner.Metadata{
        BasicInfo: scanner.BasicMetadata{
            FileName:     filepath.Base(input.ObjectKey),
            FileType:     "My Type File",
            Size:         input.Size,
            ContentType:  input.ContentType,
            LastModified: input.LastModified,
            ETag:         input.ETag,
        },
        CustomAttrs: make(map[string]interface{}),
    }

    // 提取特定元数据
    metadata.CustomAttrs["field1"] = "value1"
    metadata.CustomAttrs["field2"] = 42

    return metadata, nil
}
```

### 3. 注册提取器

在 `meta/backend/plugins/extractors/register.go` 中注册：

```go
func init() {
    // ... 其他提取器
    RegisterExtractor("mytype", &MyTypeExtractor{})
}
```

### 4. 重启服务

```bash
cd /Users/pampa/code/addp
make dev-restart
```

## 优先级说明

当多个提取器支持同一MIME类型时，优先级高的提取器会被优先使用。优先级范围：0-100，数值越大优先级越高。

当前优先级分配：
- Shapefile: 90 (地理空间专用格式)
- Video: 80 (视频文件)
- PDF: 75 (文档类)
- GeoJSON: 70 (地理空间JSON)
- Image: 70 (图像类)
- SQLite: 65 (数据库文件)
- CSV: 60 (表格数据)
- Office: 55 (Office文档)

## 测试

所有插件都应该包含单元测试。测试示例：

```go
package extractors

import (
    "context"
    "strings"
    "testing"

    "github.com/addp/meta/internal/scanner"
)

func TestMyTypeExtractor_Extract(t *testing.T) {
    extractor := &MyTypeExtractor{}

    input := scanner.ExtractInput{
        ObjectKey:   "test.mytype",
        ContentType: "application/x-mytype",
        Size:        1024,
        Reader:      strings.NewReader("test content"),
    }

    metadata, err := extractor.Extract(context.Background(), input)
    if err != nil {
        t.Fatalf("Extract failed: %v", err)
    }

    // 验证元数据
    if metadata.BasicInfo.FileName != "test.mytype" {
        t.Errorf("Expected filename test.mytype, got %s", metadata.BasicInfo.FileName)
    }
}
```

## 相关文档

- [元数据类型架构](./METADATA_TYPES_ARCHITECTURE.md)
- [视频元数据提取](./VIDEO_METADATA_EXTRACTION_DONE.md)
- [插件架构重构](../PLUGIN_ARCHITECTURE_REFACTORING.md)
