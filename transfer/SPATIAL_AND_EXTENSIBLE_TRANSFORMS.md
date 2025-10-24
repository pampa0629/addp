# Transfer 模块空间数据类型转换与通用扩展机制设计

## 1. 概述

本文档描述如何在 transfer 模块中实现:
1. **空间数据类型转换** - 支持 PostGIS、MySQL Spatial、GPKG、Shapefile 等空间数据的转换
2. **通用扩展机制** - 支持任意类型（图片、视频、专业格式）的数据转换插件化开发

## 2. 当前架构分析

### 2.1 现有 Pipeline 架构

```
┌─────────────────────────────────────────────────────┐
│            Pipeline 核心接口                         │
├─────────────────────────────────────────────────────┤
│ Reader (数据读取)                                    │
│   - Open() / Read() / Schema() / SeekTo() / Close() │
│                                                     │
│ Transform (数据转换)                                 │
│   - Apply(batch) -> batch                           │
│   - Name()                                          │
│                                                     │
│ Writer (数据写入)                                    │
│   - Open() / Write() / Flush() / Close()            │
└─────────────────────────────────────────────────────┘
              ↓ 注册和管理
┌─────────────────────────────────────────────────────┐
│         ConnectorRegistry (连接器注册表)             │
├─────────────────────────────────────────────────────┤
│ - RegisterReader(type, factory)                     │
│ - RegisterWriter(type, factory)                     │
│ - NewReader(config) / NewWriter(config)             │
└─────────────────────────────────────────────────────┘
```

### 2.2 现有类型转换能力

当前 `transform.go` 中的 `convertType()` 函数只支持:
- `string` - 字符串
- `int` - 整数
- `float` - 浮点数
- `bool` - 布尔值
- `datetime` - 日期时间

**限制**:
- 缺少空间数据类型（geometry、geography、point、polygon 等）
- 缺少二进制数据类型（image、video、blob）
- 缺少专业格式（CAD、GeoJSON、KML、XML）
- 类型转换逻辑硬编码，无法动态扩展

---

## 3. 空间数据类型转换实现

### 3.1 空间数据类型定义

**扩展 `pipeline/interfaces.go` 的 Field.Type**:

```go
// Field 字段定义
type Field struct {
    Name         string
    Type         string      // 扩展支持空间类型
    SpatialType  string      // geometry, geography, point, linestring, polygon, multipoint, etc.
    SRID         int         // 空间参考系统 ID (如 4326 for WGS84)
    Dimension    string      // 2D, 3D, 4D (Z, M, ZM)
    Nullable     bool
    Description  string
    DefaultValue interface{}
    Format       string
    // 新增：扩展属性
    ExtendedAttributes map[string]interface{} // 支持自定义属性
}
```

### 3.2 空间数据转换器架构

```
┌──────────────────────────────────────────────────────┐
│          SpatialTransform (空间转换器)                │
├──────────────────────────────────────────────────────┤
│ - sourceFormat: "wkb" / "wkt" / "geojson" / "ewkb"   │
│ - targetFormat: "wkb" / "wkt" / "geojson" / "ewkb"   │
│ - sourceSRID: 4326                                   │
│ - targetSRID: 3857                                   │
│ - geometryField: "geom"                              │
│ - preserveAttributes: true                           │
│                                                      │
│ Apply(batch) -> batch:                               │
│   1. 识别空间字段                                     │
│   2. 解析源格式 (WKB/WKT/GeoJSON -> internal)        │
│   3. 坐标转换 (投影变换)                              │
│   4. 序列化目标格式 (internal -> WKB/WKT/GeoJSON)     │
└──────────────────────────────────────────────────────┘
```

### 3.3 实现示例

**文件**: `transfer/backend/pkg/pipeline/spatial_transform.go`

```go
package pipeline

import (
    "context"
    "fmt"

    "github.com/twpayne/go-geom"
    "github.com/twpayne/go-geom/encoding/geojson"
    "github.com/twpayne/go-geom/encoding/wkb"
    "github.com/twpayne/go-geom/encoding/wkt"
    "github.com/twpayne/go-proj/v4"
)

// SpatialFormat 空间数据格式
type SpatialFormat string

const (
    FormatWKB     SpatialFormat = "wkb"      // Well-Known Binary
    FormatWKT     SpatialFormat = "wkt"      // Well-Known Text
    FormatEWKB    SpatialFormat = "ewkb"     // Extended WKB (PostGIS)
    FormatEWKT    SpatialFormat = "ewkt"     // Extended WKT
    FormatGeoJSON SpatialFormat = "geojson"  // GeoJSON
)

// SpatialTransformConfig 空间转换配置
type SpatialTransformConfig struct {
    GeometryFields []string      `json:"geometry_fields"` // 空间字段名列表
    SourceFormat   SpatialFormat `json:"source_format"`   // 源格式
    TargetFormat   SpatialFormat `json:"target_format"`   // 目标格式
    SourceSRID     int           `json:"source_srid"`     // 源坐标系 (如 4326)
    TargetSRID     int           `json:"target_srid"`     // 目标坐标系 (如 3857)
    SimplifyTolerance float64    `json:"simplify_tolerance"` // 简化容差
}

// SpatialTransform 空间数据转换器
type SpatialTransform struct {
    config    SpatialTransformConfig
    projector *proj.Transformer // 坐标投影转换器
}

// NewSpatialTransform 创建空间转换器
func NewSpatialTransform(config SpatialTransformConfig) (*SpatialTransform, error) {
    var projector *proj.Transformer

    // 如果需要坐标转换，初始化投影转换器
    if config.SourceSRID != 0 && config.TargetSRID != 0 && config.SourceSRID != config.TargetSRID {
        proj, err := proj.NewTransformer(
            fmt.Sprintf("EPSG:%d", config.SourceSRID),
            fmt.Sprintf("EPSG:%d", config.TargetSRID),
        )
        if err != nil {
            return nil, fmt.Errorf("failed to create projector: %w", err)
        }
        projector = proj
    }

    return &SpatialTransform{
        config:    config,
        projector: projector,
    }, nil
}

// Apply 应用空间数据转换
func (t *SpatialTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
    if len(t.config.GeometryFields) == 0 {
        return batch, nil
    }

    for _, row := range batch.Rows {
        for _, geomField := range t.config.GeometryFields {
            value, exists := row[geomField]
            if !exists || value == nil {
                continue
            }

            // 解析几何对象
            geometry, err := t.parseGeometry(value)
            if err != nil {
                return nil, fmt.Errorf("failed to parse geometry in field %s: %w", geomField, err)
            }

            // 投影转换
            if t.projector != nil {
                geometry, err = t.transformCoordinates(geometry)
                if err != nil {
                    return nil, fmt.Errorf("failed to transform coordinates: %w", err)
                }
            }

            // 几何简化（可选）
            if t.config.SimplifyTolerance > 0 {
                geometry = t.simplifyGeometry(geometry, t.config.SimplifyTolerance)
            }

            // 序列化为目标格式
            converted, err := t.serializeGeometry(geometry)
            if err != nil {
                return nil, fmt.Errorf("failed to serialize geometry: %w", err)
            }

            row[geomField] = converted
        }
    }

    return batch, nil
}

// parseGeometry 解析几何对象
func (t *SpatialTransform) parseGeometry(value interface{}) (geom.T, error) {
    switch t.config.SourceFormat {
    case FormatWKB, FormatEWKB:
        // 处理 []byte 或 hex string
        var data []byte
        switch v := value.(type) {
        case []byte:
            data = v
        case string:
            // 可能是 hex encoded
            data = []byte(v)
        default:
            return nil, fmt.Errorf("unsupported WKB type: %T", value)
        }
        return wkb.Unmarshal(data)

    case FormatWKT, FormatEWKT:
        str, ok := value.(string)
        if !ok {
            return nil, fmt.Errorf("WKT must be string, got %T", value)
        }
        return wkt.Unmarshal(str)

    case FormatGeoJSON:
        // GeoJSON 可以是 string 或 map
        var jsonStr string
        switch v := value.(type) {
        case string:
            jsonStr = v
        case map[string]interface{}:
            // 将 map 转为 JSON string
            bytes, _ := json.Marshal(v)
            jsonStr = string(bytes)
        default:
            return nil, fmt.Errorf("unsupported GeoJSON type: %T", value)
        }
        return geojson.Unmarshal([]byte(jsonStr))

    default:
        return nil, fmt.Errorf("unsupported source format: %s", t.config.SourceFormat)
    }
}

// transformCoordinates 坐标投影转换
func (t *SpatialTransform) transformCoordinates(geometry geom.T) (geom.T, error) {
    // 使用 go-proj 进行坐标转换
    // 遍历所有坐标点，应用投影转换
    // (简化实现，实际需要递归处理各种几何类型)

    switch g := geometry.(type) {
    case *geom.Point:
        coords := g.Coords()
        newCoords, err := t.projector.Transform(coords[0], coords[1])
        if err != nil {
            return nil, err
        }
        return geom.NewPoint(g.Layout()).MustSetCoords([]float64{newCoords[0], newCoords[1]}), nil

    case *geom.LineString:
        // 处理 LineString
        // ...

    case *geom.Polygon:
        // 处理 Polygon
        // ...

    // ... 其他几何类型
    }

    return geometry, nil
}

// serializeGeometry 序列化几何对象
func (t *SpatialTransform) serializeGeometry(geometry geom.T) (interface{}, error) {
    switch t.config.TargetFormat {
    case FormatWKB, FormatEWKB:
        data, err := wkb.Marshal(geometry)
        return data, err

    case FormatWKT, FormatEWKT:
        return wkt.Marshal(geometry), nil

    case FormatGeoJSON:
        data, err := geojson.Marshal(geometry)
        if err != nil {
            return nil, err
        }
        // 返回 map 或 string
        var result map[string]interface{}
        json.Unmarshal(data, &result)
        return result, nil

    default:
        return nil, fmt.Errorf("unsupported target format: %s", t.config.TargetFormat)
    }
}

// simplifyGeometry 几何简化（Douglas-Peucker 算法）
func (t *SpatialTransform) simplifyGeometry(geometry geom.T, tolerance float64) geom.T {
    // 实现几何简化算法
    // 可使用第三方库如 github.com/paulmach/orb
    return geometry
}

// Name 返回转换器名称
func (t *SpatialTransform) Name() string {
    return "SpatialTransform"
}
```

### 3.4 空间数据 Reader/Writer 扩展

**扩展 JDBC Reader** (`connector/jdbc_reader.go`):

```go
// mapDatabaseType 映射数据库类型到统一类型
func (r *JDBCReader) mapDatabaseType(dbType string) string {
    switch dbType {
    // 原有类型...

    // PostGIS 空间类型
    case "GEOMETRY", "GEOGRAPHY", "POINT", "LINESTRING", "POLYGON",
         "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION":
        return "geometry"

    // MySQL Spatial 类型
    case "GEOMETRY", "POINT", "LINESTRING", "POLYGON":
        return "geometry"

    default:
        return "string"
    }
}

// scanRow 扫描行数据 - 增强版
func (r *JDBCReader) scanRow(rows *sql.Rows) (map[string]interface{}, error) {
    // ... 原有代码

    for i, col := range r.columns {
        val := values[i]

        // 处理空间数据类型
        if columnTypes[i].DatabaseTypeName() == "GEOMETRY" {
            // PostGIS 返回 WKB 格式的 []byte
            if b, ok := val.([]byte); ok {
                row[col] = b // 保持 WKB 格式
                continue
            }
        }

        // ... 其他类型处理
    }

    return row, nil
}
```

### 3.5 使用示例

```go
// 创建空间数据转换任务
taskConfig := models.CreateTaskRequest{
    Name: "PostGIS to MySQL Spatial",
    Type: models.TaskTypeSync,
    SourceID: &postgisResourceID,
    TargetID: &mysqlResourceID,
    Config: map[string]interface{}{
        "source": map[string]interface{}{
            "query": "SELECT id, name, ST_AsBinary(geom) as geom FROM cities",
        },
        "target": map[string]interface{}{
            "table": "cities",
        },
        "transforms": []map[string]interface{}{
            {
                "type": "spatial",
                "geometry_fields": []string{"geom"},
                "source_format": "wkb",
                "target_format": "wkb",
                "source_srid": 4326,  // WGS84
                "target_srid": 3857,  // Web Mercator
            },
        },
    },
}
```

---

## 4. 通用扩展机制设计

### 4.1 插件化架构

```
┌────────────────────────────────────────────────────────┐
│            TransformRegistry (转换器注册表)             │
├────────────────────────────────────────────────────────┤
│ - RegisterTransform(name, factory)                     │
│ - NewTransform(name, config) -> Transform              │
│ - ListTransforms() -> []string                         │
└────────────────────────────────────────────────────────┘
                      ↓ 管理
        ┌─────────────────────────────────┐
        │       TransformFactory          │
        │  func(config) -> Transform      │
        └─────────────────────────────────┘
                      ↓ 创建
        ┌─────────────────────────────────┐
        │         Transform 接口           │
        ├─────────────────────────────────┤
        │ - Apply(batch) -> batch         │
        │ - Name() -> string              │
        │ - Validate() -> error           │
        │ - Capabilities() -> Capability  │
        └─────────────────────────────────┘
              ↓ 实现
┌─────────────┬─────────────┬─────────────┬─────────────┐
│  Spatial    │  Image      │  Video      │  Custom     │
│  Transform  │  Transform  │  Transform  │  Transform  │
└─────────────┴─────────────┴─────────────┴─────────────┘
```

### 4.2 转换器注册表实现

**文件**: `transfer/backend/pkg/pipeline/transform_registry.go`

```go
package pipeline

import (
    "fmt"
    "sync"
)

// TransformFactory Transform 工厂函数
type TransformFactory func(config map[string]interface{}) (Transform, error)

// TransformCapability 转换器能力描述
type TransformCapability struct {
    Name         string                 `json:"name"`
    Description  string                 `json:"description"`
    SupportedTypes []string             `json:"supported_types"` // 支持的字段类型
    ConfigSchema map[string]interface{} `json:"config_schema"`   // JSON Schema
}

// TransformRegistry 转换器注册表
type TransformRegistry struct {
    factories    map[string]TransformFactory
    capabilities map[string]TransformCapability
    mu           sync.RWMutex
}

// NewTransformRegistry 创建转换器注册表
func NewTransformRegistry() *TransformRegistry {
    return &TransformRegistry{
        factories:    make(map[string]TransformFactory),
        capabilities: make(map[string]TransformCapability),
    }
}

// RegisterTransform 注册转换器
func (r *TransformRegistry) RegisterTransform(
    name string,
    factory TransformFactory,
    capability TransformCapability,
) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.factories[name]; exists {
        return fmt.Errorf("transform already registered: %s", name)
    }

    r.factories[name] = factory
    r.capabilities[name] = capability
    return nil
}

// NewTransform 创建转换器实例
func (r *TransformRegistry) NewTransform(name string, config map[string]interface{}) (Transform, error) {
    r.mu.RLock()
    factory, exists := r.factories[name]
    r.mu.RUnlock()

    if !exists {
        return nil, fmt.Errorf("transform not registered: %s", name)
    }

    return factory(config)
}

// ListTransforms 列出所有已注册的转换器
func (r *TransformRegistry) ListTransforms() []TransformCapability {
    r.mu.RLock()
    defer r.mu.RUnlock()

    list := make([]TransformCapability, 0, len(r.capabilities))
    for _, cap := range r.capabilities {
        list = append(list, cap)
    }
    return list
}

// GetCapability 获取转换器能力描述
func (r *TransformRegistry) GetCapability(name string) (TransformCapability, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    cap, exists := r.capabilities[name]
    return cap, exists
}

// 全局注册表实例
var defaultTransformRegistry = NewTransformRegistry()

// RegisterTransform 注册到全局注册表
func RegisterTransform(name string, factory TransformFactory, capability TransformCapability) error {
    return defaultTransformRegistry.RegisterTransform(name, factory, capability)
}

// NewTransformByName 从全局注册表创建转换器
func NewTransformByName(name string, config map[string]interface{}) (Transform, error) {
    return defaultTransformRegistry.NewTransform(name, config)
}

// ListAllTransforms 列出全局注册表中的所有转换器
func ListAllTransforms() []TransformCapability {
    return defaultTransformRegistry.ListTransforms()
}
```

### 4.3 图片转换器示例

**文件**: `transfer/backend/pkg/pipeline/image_transform.go`

```go
package pipeline

import (
    "bytes"
    "context"
    "fmt"
    "image"
    "image/jpeg"
    "image/png"

    "github.com/nfnt/resize"
)

// ImageTransformConfig 图片转换配置
type ImageTransformConfig struct {
    ImageFields   []string `json:"image_fields"`   // 图片字段名
    TargetFormat  string   `json:"target_format"`  // 目标格式: jpeg, png, webp
    Quality       int      `json:"quality"`        // JPEG 质量 (1-100)
    MaxWidth      uint     `json:"max_width"`      // 最大宽度（像素）
    MaxHeight     uint     `json:"max_height"`     // 最大高度
    Thumbnail     bool     `json:"thumbnail"`      // 是否生成缩略图
    ThumbnailSize uint     `json:"thumbnail_size"` // 缩略图尺寸
}

// ImageTransform 图片转换器
type ImageTransform struct {
    config ImageTransformConfig
}

// NewImageTransform 创建图片转换器
func NewImageTransform(config map[string]interface{}) (Transform, error) {
    var cfg ImageTransformConfig
    if err := mapToStruct(config, &cfg); err != nil {
        return nil, err
    }

    // 默认值
    if cfg.Quality == 0 {
        cfg.Quality = 85
    }
    if cfg.TargetFormat == "" {
        cfg.TargetFormat = "jpeg"
    }

    return &ImageTransform{config: cfg}, nil
}

// Apply 应用图片转换
func (t *ImageTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
    for _, row := range batch.Rows {
        for _, field := range t.config.ImageFields {
            value, exists := row[field]
            if !exists || value == nil {
                continue
            }

            // 获取图片数据（支持 []byte 或 base64 string）
            imgData, err := t.getImageData(value)
            if err != nil {
                return nil, fmt.Errorf("failed to get image data for field %s: %w", field, err)
            }

            // 解码图片
            img, format, err := image.Decode(bytes.NewReader(imgData))
            if err != nil {
                return nil, fmt.Errorf("failed to decode image: %w", err)
            }

            // 调整尺寸
            if t.config.MaxWidth > 0 || t.config.MaxHeight > 0 {
                img = resize.Thumbnail(t.config.MaxWidth, t.config.MaxHeight, img, resize.Lanczos3)
            }

            // 编码为目标格式
            var buf bytes.Buffer
            switch t.config.TargetFormat {
            case "jpeg", "jpg":
                err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: t.config.Quality})
            case "png":
                err = png.Encode(&buf, img)
            default:
                return nil, fmt.Errorf("unsupported target format: %s", t.config.TargetFormat)
            }

            if err != nil {
                return nil, fmt.Errorf("failed to encode image: %w", err)
            }

            // 更新字段值
            row[field] = buf.Bytes()

            // 生成缩略图（可选）
            if t.config.Thumbnail && t.config.ThumbnailSize > 0 {
                thumbnail := resize.Thumbnail(t.config.ThumbnailSize, t.config.ThumbnailSize, img, resize.Lanczos3)
                var thumbBuf bytes.Buffer
                jpeg.Encode(&thumbBuf, thumbnail, &jpeg.Options{Quality: 80})
                row[field+"_thumbnail"] = thumbBuf.Bytes()
            }
        }
    }

    return batch, nil
}

func (t *ImageTransform) getImageData(value interface{}) ([]byte, error) {
    switch v := value.(type) {
    case []byte:
        return v, nil
    case string:
        // 可能是 base64 encoded
        // decoded, err := base64.StdEncoding.DecodeString(v)
        // return decoded, err
        return []byte(v), nil
    default:
        return nil, fmt.Errorf("unsupported image data type: %T", value)
    }
}

func (t *ImageTransform) Name() string {
    return "ImageTransform"
}

// 注册图片转换器
func init() {
    RegisterTransform("image", NewImageTransform, TransformCapability{
        Name:        "image",
        Description: "Transform image data (resize, format conversion, thumbnail generation)",
        SupportedTypes: []string{"blob", "binary", "bytea"},
        ConfigSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "image_fields":   map[string]string{"type": "array"},
                "target_format":  map[string]string{"type": "string", "enum": "jpeg,png,webp"},
                "quality":        map[string]string{"type": "integer", "min": "1", "max": "100"},
                "max_width":      map[string]string{"type": "integer"},
                "max_height":     map[string]string{"type": "integer"},
                "thumbnail":      map[string]string{"type": "boolean"},
                "thumbnail_size": map[string]string{"type": "integer"},
            },
        },
    })
}
```

### 4.4 视频转换器示例

**文件**: `transfer/backend/pkg/pipeline/video_transform.go`

```go
package pipeline

import (
    "context"
    "fmt"
    "os"
    "os/exec"
)

// VideoTransformConfig 视频转换配置
type VideoTransformConfig struct {
    VideoFields    []string `json:"video_fields"`
    TargetCodec    string   `json:"target_codec"`    // h264, h265, vp9
    TargetFormat   string   `json:"target_format"`   // mp4, webm, mkv
    Resolution     string   `json:"resolution"`      // 1920x1080, 1280x720
    Bitrate        string   `json:"bitrate"`         // 2M, 5M
    FrameRate      int      `json:"frame_rate"`      // 30, 60
    ExtractThumbnail bool   `json:"extract_thumbnail"`
    ThumbnailTime  string   `json:"thumbnail_time"`  // 00:00:01
}

// VideoTransform 视频转换器
// 依赖 ffmpeg 命令行工具
type VideoTransform struct {
    config VideoTransformConfig
}

func NewVideoTransform(config map[string]interface{}) (Transform, error) {
    var cfg VideoTransformConfig
    if err := mapToStruct(config, &cfg); err != nil {
        return nil, err
    }

    // 检查 ffmpeg 是否安装
    if _, err := exec.LookPath("ffmpeg"); err != nil {
        return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
    }

    return &VideoTransform{config: cfg}, nil
}

func (t *VideoTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
    for _, row := range batch.Rows {
        for _, field := range t.config.VideoFields {
            value, exists := row[field]
            if !exists || value == nil {
                continue
            }

            // 视频数据通常很大，这里假设 value 是文件路径或 URL
            inputPath, ok := value.(string)
            if !ok {
                return nil, fmt.Errorf("video field must be a file path string")
            }

            // 创建临时输出文件
            outputPath := fmt.Sprintf("/tmp/video_output_%s.%s", field, t.config.TargetFormat)

            // 构建 ffmpeg 命令
            args := []string{
                "-i", inputPath,
                "-c:v", t.config.TargetCodec,
                "-b:v", t.config.Bitrate,
            }

            if t.config.Resolution != "" {
                args = append(args, "-s", t.config.Resolution)
            }

            if t.config.FrameRate > 0 {
                args = append(args, "-r", fmt.Sprintf("%d", t.config.FrameRate))
            }

            args = append(args, outputPath)

            // 执行转换
            cmd := exec.CommandContext(ctx, "ffmpeg", args...)
            if err := cmd.Run(); err != nil {
                return nil, fmt.Errorf("ffmpeg conversion failed: %w", err)
            }

            // 更新字段值为新文件路径
            row[field] = outputPath

            // 提取缩略图（可选）
            if t.config.ExtractThumbnail {
                thumbnailPath := fmt.Sprintf("/tmp/video_thumb_%s.jpg", field)
                thumbCmd := exec.CommandContext(ctx, "ffmpeg",
                    "-i", inputPath,
                    "-ss", t.config.ThumbnailTime,
                    "-vframes", "1",
                    thumbnailPath,
                )
                if err := thumbCmd.Run(); err == nil {
                    row[field+"_thumbnail"] = thumbnailPath
                }
            }
        }
    }

    return batch, nil
}

func (t *VideoTransform) Name() string {
    return "VideoTransform"
}

// 注册视频转换器
func init() {
    RegisterTransform("video", NewVideoTransform, TransformCapability{
        Name:        "video",
        Description: "Transform video data (codec conversion, resolution scaling, frame rate adjustment)",
        SupportedTypes: []string{"blob", "binary", "file_path"},
        ConfigSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "video_fields":     map[string]string{"type": "array"},
                "target_codec":     map[string]string{"type": "string", "enum": "h264,h265,vp9"},
                "target_format":    map[string]string{"type": "string", "enum": "mp4,webm,mkv"},
                "resolution":       map[string]string{"type": "string"},
                "bitrate":          map[string]string{"type": "string"},
                "frame_rate":       map[string]string{"type": "integer"},
                "extract_thumbnail": map[string]string{"type": "boolean"},
            },
        },
    })
}
```

### 4.5 自定义专业格式转换器（以 CAD 为例）

**文件**: `transfer/backend/pkg/pipeline/cad_transform.go`

```go
package pipeline

import (
    "context"
    "fmt"
)

// CADTransformConfig CAD 转换配置
type CADTransformConfig struct {
    CADFields      []string `json:"cad_fields"`
    SourceFormat   string   `json:"source_format"`   // dwg, dxf, dgn
    TargetFormat   string   `json:"target_format"`   // dxf, pdf, svg
    ExtractLayers  []string `json:"extract_layers"`  // 提取指定图层
    Scale          float64  `json:"scale"`           // 缩放比例
}

// CADTransform CAD 转换器
// 可集成 Open Design Alliance (ODA) 或 LibreDWG
type CADTransform struct {
    config CADTransformConfig
}

func NewCADTransform(config map[string]interface{}) (Transform, error) {
    var cfg CADTransformConfig
    if err := mapToStruct(config, &cfg); err != nil {
        return nil, err
    }

    return &CADTransform{config: cfg}, nil
}

func (t *CADTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
    // 实现 CAD 文件转换逻辑
    // 1. 调用 LibreDWG 或商业库读取 DWG/DXF
    // 2. 提取几何实体、图层、属性
    // 3. 转换为目标格式（DXF/PDF/SVG）
    // 4. 可选：提取空间数据导出为 Shapefile/GeoJSON

    return batch, nil
}

func (t *CADTransform) Name() string {
    return "CADTransform"
}

// 注册 CAD 转换器
func init() {
    RegisterTransform("cad", NewCADTransform, TransformCapability{
        Name:        "cad",
        Description: "Transform CAD files (DWG, DXF to PDF, SVG, Shapefile)",
        SupportedTypes: []string{"blob", "file_path"},
        ConfigSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "cad_fields":     map[string]string{"type": "array"},
                "source_format":  map[string]string{"type": "string"},
                "target_format":  map[string]string{"type": "string"},
                "extract_layers": map[string]string{"type": "array"},
            },
        },
    })
}
```

---

## 5. 转换器发现和配置 API

### 5.1 API 端点

**文件**: `transfer/backend/internal/api/transform_handler.go`

```go
package api

import (
    "net/http"

    "github.com/addp/transfer/pkg/pipeline"
    "github.com/gin-gonic/gin"
)

// TransformHandler 转换器处理器
type TransformHandler struct{}

// ListTransforms 列出所有可用的转换器
// GET /api/transforms
func (h *TransformHandler) ListTransforms(c *gin.Context) {
    transforms := pipeline.ListAllTransforms()
    c.JSON(http.StatusOK, gin.H{
        "transforms": transforms,
    })
}

// GetTransformCapability 获取转换器能力描述
// GET /api/transforms/:name
func (h *TransformHandler) GetTransformCapability(c *gin.Context) {
    name := c.Param("name")

    cap, exists := pipeline.GetCapability(name)
    if !exists {
        c.JSON(http.StatusNotFound, gin.H{"error": "transform not found"})
        return
    }

    c.JSON(http.StatusOK, cap)
}

// ValidateTransformConfig 验证转换器配置
// POST /api/transforms/:name/validate
func (h *TransformHandler) ValidateTransformConfig(c *gin.Context) {
    name := c.Param("name")

    var config map[string]interface{}
    if err := c.ShouldBindJSON(&config); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // 尝试创建转换器实例以验证配置
    _, err := pipeline.NewTransformByName(name, config)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "valid": false,
            "error": err.Error(),
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{"valid": true})
}
```

### 5.2 前端集成示例

```javascript
// 获取可用转换器列表
async function fetchAvailableTransforms() {
  const response = await axios.get('/api/transforms');
  return response.data.transforms;
  // 返回:
  // [
  //   { name: "spatial", description: "...", supported_types: [...] },
  //   { name: "image", description: "...", supported_types: [...] },
  //   { name: "video", description: "...", supported_types: [...] }
  // ]
}

// 动态渲染转换器配置表单
function renderTransformConfig(transformName) {
  const capability = await axios.get(`/api/transforms/${transformName}`);
  const schema = capability.data.config_schema;

  // 根据 JSON Schema 动态生成表单
  // 可使用 vue-form-generator 或 react-jsonschema-form
}

// 创建任务时指定转换链
const taskConfig = {
  name: "Multi-format data transfer",
  transforms: [
    {
      type: "spatial",
      geometry_fields: ["geom"],
      source_srid: 4326,
      target_srid: 3857
    },
    {
      type: "image",
      image_fields: ["photo"],
      max_width: 1024,
      thumbnail: true
    }
  ]
};
```

---

## 6. 最佳实践

### 6.1 性能优化

1. **流式处理大文件**:
   ```go
   // 不要一次性加载整个文件到内存
   // 使用 io.Reader 流式处理
   func (t *VideoTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
       // 使用流式 API
       reader := batch.GetStreamReader(field)
       writer := batch.GetStreamWriter(field)
       // 边读边转换边写
   }
   ```

2. **并行处理批次数据**:
   ```go
   // 使用 goroutine pool 并行处理批次中的多行
   func (t *ImageTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
       var wg sync.WaitGroup
       sem := make(chan struct{}, 10) // 限制并发数

       for i, row := range batch.Rows {
           wg.Add(1)
           go func(index int, r map[string]interface{}) {
               defer wg.Done()
               sem <- struct{}{}
               defer func() { <-sem }()
               // 处理单行
               t.processRow(ctx, r)
           }(i, row)
       }
       wg.Wait()
       return batch, nil
   }
   ```

3. **缓存和复用资源**:
   ```go
   type SpatialTransform struct {
       projector *proj.Transformer // 复用投影转换器
       wkbCache  *sync.Pool         // WKB 解析缓存
   }
   ```

### 6.2 错误处理

```go
// 支持部分失败模式
type TransformResult struct {
    Batch       *DataBatch
    Errors      []RowError
    FailedCount int
}

type RowError struct {
    RowIndex int
    Field    string
    Error    error
}

func (t *ImageTransform) ApplyWithErrors(ctx context.Context, batch *DataBatch) (*TransformResult, error) {
    result := &TransformResult{Batch: batch}

    for i, row := range batch.Rows {
        if err := t.processRow(ctx, row); err != nil {
            result.Errors = append(result.Errors, RowError{
                RowIndex: i,
                Error:    err,
            })
            result.FailedCount++
        }
    }

    return result, nil
}
```

### 6.3 可观测性

```go
// 添加 metrics 和 tracing
func (t *SpatialTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
    start := time.Now()
    defer func() {
        metrics.RecordTransformDuration("spatial", time.Since(start))
    }()

    span, ctx := tracing.StartSpan(ctx, "SpatialTransform.Apply")
    defer span.Finish()

    // 处理逻辑...

    metrics.IncrementCounter("spatial_transform_rows", len(batch.Rows))
    return batch, nil
}
```

---

## 7. 总结

### 7.1 空间数据转换

- ✅ 通过 `SpatialTransform` 支持 WKB/WKT/GeoJSON/EWKB 格式互转
- ✅ 支持坐标投影转换（SRID 转换）
- ✅ 支持几何简化和空间操作
- ✅ 无缝集成到现有 Pipeline 架构

### 7.2 通用扩展机制

- ✅ **插件化架构**: `TransformRegistry` + `TransformFactory`
- ✅ **自描述能力**: `TransformCapability` 提供 JSON Schema
- ✅ **动态发现**: API 端点暴露可用转换器
- ✅ **易于扩展**: 实现 `Transform` 接口 + `init()` 注册即可

### 7.3 支持的转换类型

| 转换器类型 | 用途 | 依赖库 |
|-----------|------|--------|
| `spatial` | 空间数据格式转换、投影变换 | go-geom, go-proj |
| `image` | 图片格式转换、缩放、缩略图 | resize, imaging |
| `video` | 视频编码转换、分辨率调整 | ffmpeg (CLI) |
| `cad` | CAD 文件转换（DWG/DXF） | LibreDWG, ODA |
| `pdf` | PDF 文档处理 | pdfcpu, gotenberg |
| `office` | Office 文档转换 | unoconv, LibreOffice |

### 7.4 下一步实现建议

1. **优先级 1** (核心功能):
   - 实现 `SpatialTransform` 支持 PostGIS/MySQL Spatial
   - 实现 `TransformRegistry` 注册机制
   - 添加 Transform API 端点

2. **优先级 2** (常用场景):
   - 实现 `ImageTransform` 支持图片处理
   - 添加性能监控和错误处理
   - 前端动态表单生成

3. **优先级 3** (扩展能力):
   - 实现 `VideoTransform` 和 `CADTransform`
   - 提供插件开发文档和示例
   - 支持用户自定义转换器（Go Plugin）

---

## 附录: 依赖库推荐

### Go 空间数据处理库

```bash
go get github.com/twpayne/go-geom           # 几何对象库
go get github.com/twpayne/go-geom/encoding  # WKB/WKT/GeoJSON
go get github.com/go-spatial/proj           # 投影转换
go get github.com/paulmach/orb              # 2D 几何库
```

### Go 图片处理库

```bash
go get github.com/nfnt/resize               # 图片缩放
go get github.com/disintegration/imaging   # 图片处理
go get github.com/h2non/bimg                # libvips 封装（高性能）
```

### 其他格式处理

```bash
# PDF
go get github.com/pdfcpu/pdfcpu

# Office 文档（需要 LibreOffice）
# 使用 exec.Command 调用 unoconv

# CAD 文件
# 使用 CGO 集成 LibreDWG 或商业 SDK
```
