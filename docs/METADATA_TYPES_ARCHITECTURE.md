# 元数据类型扩展架构

## 概述

ADDP Meta模块使用**开放的类型注册表**架构，允许第三方开发者为任何文件类型定义和注册自定义元数据类型。

## 核心设计原理

### 1. 全局注册表

```go
// meta/sdk/metadata_registry.go
var defaultMetadataRegistry = &MetadataTypeRegistry{
    types: make(map[string]TypedMetadata),
}

func RegisterMetadataType(metadata TypedMetadata) {
    defaultMetadataRegistry.Register(metadata)
}
```

**关键点：**
- ✅ 全局、单例的注册表
- ✅ 线程安全（sync.RWMutex）
- ✅ 任何包都可以注册新类型
- ✅ init()函数自动执行注册

### 2. init()函数的执行顺序

Go语言保证：
1. 所有导入包的init()按依赖顺序执行
2. 同一个包内的init()按出现顺序执行
3. main包的init()最后执行

**在ADDP中的执行流程：**

```
1. SDK包初始化
   ├─ meta/sdk/metadata_registry.go init()
   │  └─ 注册内置类型：geo.spatial, image.metadata, document.metadata

2. 第三方插件初始化（按导入顺序）
   ├─ plugins/shapefile-extractor init()
   │  └─ 注册 geo.shapefile
   ├─ plugins/video-extractor init()
   │  └─ 注册 video.metadata
   └─ plugins/audio-extractor init()
      └─ 注册 audio.metadata

3. ADDP scanner初始化
   ├─ meta/backend/internal/scanner/extractors init()
   │  └─ 注册内置提取器（GeoJSON, CSV, Image, PDF, SQLite）
   └─ meta/backend/internal/scanner/plugins init()
      └─ 注册第三方提取器（Shapefile, Video, Audio）

4. Meta服务启动
   └─ 所有类型和提取器已就绪
```

### 3. 类型安全的元数据

```go
// 第三方定义的类型
type VideoMetadata struct {
    Duration   int     `json:"duration"`
    Resolution string  `json:"resolution"`
    Codec      string  `json:"codec"`
}

// 实现TypedMetadata接口
func (m *VideoMetadata) TypeName() string {
    return "video.metadata"
}

func (m *VideoMetadata) Schema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "duration":   map[string]string{"type": "integer"},
            "resolution": map[string]string{"type": "string"},
            "codec":      map[string]string{"type": "string"},
        },
    }
}

func (m *VideoMetadata) ToMap() map[string]interface{} { /* ... */ }
func (m *VideoMetadata) FromMap(data map[string]interface{}) error { /* ... */ }
```

### 4. 序列化和存储

```go
// 序列化时自动包含类型信息
metadata.AddTypedMetadata("video_metadata", videoMeta)

// 生成的JSON结构
{
  "video_metadata": {
    "_type": "video.metadata",
    "_schema": { /* JSON Schema */ },
    "data": {
      "duration": 3600,
      "resolution": "1920x1080",
      "codec": "H.264"
    }
  }
}
```

## 架构优势

### 1. 完全解耦

```
┌─────────────────────────────────────────────┐
│  第三方开发者                                 │
│  ├─ 只需依赖 SDK                             │
│  ├─ 不需要ADDP源码                           │
│  ├─ 独立开发和测试                           │
│  └─ 通过go.mod replace集成                  │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  SDK (meta/sdk/)                             │
│  ├─ TypedMetadata接口                        │
│  ├─ 全局注册表                               │
│  ├─ 序列化/反序列化                          │
│  └─ 基础工具函数                             │
└─────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────┐
│  ADDP Meta模块                               │
│  ├─ 扫描服务                                 │
│  ├─ 提取器注册                               │
│  ├─ 元数据存储                               │
│  └─ API端点                                  │
└─────────────────────────────────────────────┘
```

### 2. 类型扩展性

```
内置类型（SDK提供）
├─ geo.spatial        - GeoSpatialMetadata
├─ image.metadata     - ImageMetadata
└─ document.metadata  - DocumentMetadata

第三方类型（示例）
├─ video.metadata     - VideoMetadata
├─ audio.metadata     - AudioMetadata
├─ 3d.model.metadata  - Model3DMetadata
├─ cad.drawing        - CADDrawingMetadata
└─ medical.dicom      - DICOMMetadata

用户自定义类型
└─ company.custom.*   - 任何自定义结构
```

### 3. 数据库灵活性

**PostgreSQL JSONB的优势：**

```sql
-- 查询特定类型的元数据
SELECT *
FROM metadata.meta_item
WHERE attributes->'extracted_metadata'->'custom_attrs' ? 'video_metadata';

-- 按类型分组统计
SELECT
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->>'_type' as type,
    COUNT(*) as count
FROM metadata.meta_item
WHERE attributes->'extracted_metadata'->'custom_attrs'->'video_metadata' IS NOT NULL
GROUP BY type;

-- 复杂查询：查找所有H.264编码的高清视频
SELECT
    item_name,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution' as resolution
FROM metadata.meta_item
WHERE
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'codec' = 'H.264'
    AND (
        attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution'
        LIKE '%1080%'
        OR
        attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution'
        LIKE '%4K%'
    );

-- 创建GIN索引加速查询
CREATE INDEX idx_meta_item_extracted_metadata
ON metadata.meta_item
USING GIN (attributes);
```

## 实际案例：医疗影像元数据

### 1. 定义DICOM元数据类型

```go
// plugins/dicom-extractor/dicom_metadata.go
package dicomextractor

import sdk "github.com/addp/meta-extractor-sdk"

type DICOMMetadata struct {
    PatientName    string `json:"patient_name"`
    PatientID      string `json:"patient_id"`
    StudyDate      string `json:"study_date"`
    Modality       string `json:"modality"`        // CT, MRI, X-Ray等
    BodyPart       string `json:"body_part"`
    SliceCount     int    `json:"slice_count"`
    PixelSpacing   string `json:"pixel_spacing"`
    ImageSize      string `json:"image_size"`
    WindowCenter   int    `json:"window_center"`
    WindowWidth    int    `json:"window_width"`
}

func (m *DICOMMetadata) TypeName() string {
    return "medical.dicom"
}

func (m *DICOMMetadata) Schema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "patient_name":  map[string]string{"type": "string"},
            "patient_id":    map[string]string{"type": "string"},
            "study_date":    map[string]string{"type": "string", "format": "date"},
            "modality":      map[string]string{"type": "string", "enum": "CT,MRI,X-Ray,Ultrasound"},
            "body_part":     map[string]string{"type": "string"},
            "slice_count":   map[string]string{"type": "integer"},
            "pixel_spacing": map[string]string{"type": "string"},
            "image_size":    map[string]string{"type": "string"},
            "window_center": map[string]string{"type": "integer"},
            "window_width":  map[string]string{"type": "integer"},
        },
        "required": []string{"patient_id", "modality"},
    }
}

func (m *DICOMMetadata) ToMap() map[string]interface{} { /* ... */ }
func (m *DICOMMetadata) FromMap(data map[string]interface{}) error { /* ... */ }

func init() {
    sdk.RegisterMetadataType(&DICOMMetadata{})
}
```

### 2. 实现DICOM提取器

```go
// plugins/dicom-extractor/dicom_extractor.go
package dicomextractor

import (
    "context"
    sdk "github.com/addp/meta-extractor-sdk"
    "github.com/suyashkumar/dicom" // 使用开源DICOM库
)

type DICOMExtractor struct{}

func (e *DICOMExtractor) SupportedTypes() []string {
    return []string{"application/dicom"}
}

func (e *DICOMExtractor) Priority() int {
    return 85 // 高优先级
}

func (e *DICOMExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    // 解析DICOM文件
    dataset, err := dicom.Parse(input.Reader, input.Size, nil)
    if err != nil {
        return nil, err
    }

    // 提取DICOM标签
    dicomMeta := &DICOMMetadata{
        PatientName:  getString(dataset, dicom.PatientName),
        PatientID:    getString(dataset, dicom.PatientID),
        StudyDate:    getString(dataset, dicom.StudyDate),
        Modality:     getString(dataset, dicom.Modality),
        BodyPart:     getString(dataset, dicom.BodyPartExamined),
        SliceCount:   getInt(dataset, dicom.NumberOfFrames),
        PixelSpacing: getString(dataset, dicom.PixelSpacing),
        ImageSize:    fmt.Sprintf("%dx%d", getInt(dataset, dicom.Rows), getInt(dataset, dicom.Columns)),
        WindowCenter: getInt(dataset, dicom.WindowCenter),
        WindowWidth:  getInt(dataset, dicom.WindowWidth),
    }

    metadata := sdk.NewMetadata(input.ObjectKey, "DICOM Image", input.Size)
    metadata.AddTypedMetadata("dicom_metadata", dicomMeta)

    return metadata, nil
}

func GetExtractor() sdk.MetadataExtractor {
    return &DICOMExtractor{}
}
```

### 3. 集成到ADDP

```go
// meta/backend/internal/scanner/plugins/plugins.go
import (
    dicom "github.com/hospital/addp-dicom-extractor"
)

func init() {
    scanner.RegisterSDKExtractor(dicom.GetExtractor())
}
```

### 4. 医疗影像查询示例

```sql
-- 查询特定患者的所有CT扫描
SELECT
    item_name,
    attributes->'extracted_metadata'->'custom_attrs'->'dicom_metadata'->'data'->>'study_date' as study_date,
    attributes->'extracted_metadata'->'custom_attrs'->'dicom_metadata'->'data'->>'body_part' as body_part
FROM metadata.meta_item
WHERE
    attributes->'extracted_metadata'->'custom_attrs'->'dicom_metadata'->'data'->>'patient_id' = 'P123456'
    AND attributes->'extracted_metadata'->'custom_attrs'->'dicom_metadata'->'data'->>'modality' = 'CT'
ORDER BY study_date DESC;

-- 统计各种检查类型的数量
SELECT
    attributes->'extracted_metadata'->'custom_attrs'->'dicom_metadata'->'data'->>'modality' as modality,
    COUNT(*) as count
FROM metadata.meta_item
WHERE attributes->'extracted_metadata'->'custom_attrs' ? 'dicom_metadata'
GROUP BY modality;
```

## 最佳实践总结

### 1. 命名约定

```
<domain>.<category>.<specifics>

示例：
- geo.spatial          - 地理空间通用
- geo.shapefile        - Shapefile特定
- image.metadata       - 图像通用
- image.raw.metadata   - RAW格式特定
- video.metadata       - 视频通用
- video.h264.metadata  - H.264特定
- document.metadata    - 文档通用
- document.pdf         - PDF特定
- medical.dicom        - 医疗DICOM
- cad.dwg              - CAD图纸
- 3d.model.obj         - 3D模型
```

### 2. Schema设计

```go
func (m *MyMetadata) Schema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "version": "1.0",  // 包含版本信息
        "properties": map[string]interface{}{
            // 必填字段
            "field1": map[string]string{
                "type": "string",
                "description": "字段描述",
            },
            // 可选字段
            "field2": map[string]interface{}{
                "type": "integer",
                "minimum": 0,
                "description": "字段描述",
            },
        },
        "required": []string{"field1"},
    }
}
```

### 3. 性能优化

```go
// 对大文件只读取必要部分
func (e *MyExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    // 只读取文件头（例如前10MB）
    limitReader := io.LimitReader(input.Reader, 10*1024*1024)

    // 或者使用带缓冲的读取
    bufReader := bufio.NewReaderSize(input.Reader, 64*1024)

    // 解析...
}
```

### 4. 错误处理

```go
func (e *MyExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    metadata := sdk.NewMetadata(input.ObjectKey, "My File Type", input.Size)

    // 即使某些字段提取失败，也返回部分元数据
    myMeta := &MyMetadata{}

    // 尝试提取各个字段，记录错误但继续
    if field1, err := extractField1(input.Reader); err != nil {
        metadata.CustomAttrs["extraction_errors"] = append(
            getErrors(metadata),
            fmt.Sprintf("field1: %v", err),
        )
    } else {
        myMeta.Field1 = field1
    }

    metadata.AddTypedMetadata("my_metadata", myMeta)
    return metadata, nil
}
```

### 5. 测试

```go
// my_extractor_test.go
func TestExtractor(t *testing.T) {
    extractor := &MyExtractor{}

    // 测试数据
    testFile := bytes.NewReader([]byte{/* ... */})

    input := sdk.ExtractInput{
        ObjectKey: "test.ext",
        Reader:    testFile,
        Size:      int64(testFile.Len()),
    }

    metadata, err := extractor.Extract(context.Background(), input)
    assert.NoError(t, err)
    assert.NotNil(t, metadata)

    // 验证类型化元数据
    myMeta, err := metadata.GetTypedMetadata("my_metadata")
    assert.NoError(t, err)
    assert.Equal(t, "my.type", myMeta.TypeName())
}
```

## 总结

ADDP的元数据类型扩展架构提供了：

✅ **开放性** - 任何人都可以定义新类型
✅ **类型安全** - 编译时检查和运行时验证
✅ **零侵入** - 不需要修改ADDP源码
✅ **灵活存储** - JSONB支持任意结构
✅ **结构化查询** - PostgreSQL强大的JSON操作
✅ **版本兼容** - Schema支持版本演进

这使得ADDP能够适应各行各业的特定需求，从医疗影像到工业设计，从视频媒体到科学数据！
