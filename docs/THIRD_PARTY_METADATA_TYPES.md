# 第三方元数据类型扩展指南

## 概述

ADDP Meta模块支持第三方开发者定义和注册自己的元数据类型。有两种扩展方式：

## 方式1：定义类型化元数据（推荐）

第三方提取器可以定义自己的类型化元数据结构，并在init()中注册。

### 步骤1：定义元数据结构

在你的提取器包中定义元数据类型：

```go
package myextractor

import (
    sdk "github.com/addp/meta-extractor-sdk"
)

// VideoMetadata 视频文件的类型化元数据
type VideoMetadata struct {
    Duration     int      `json:"duration"`      // 视频时长（秒）
    Resolution   string   `json:"resolution"`    // 分辨率，如 "1920x1080"
    Codec        string   `json:"codec"`         // 编码格式，如 "H.264"
    Bitrate      int      `json:"bitrate"`       // 比特率（kbps）
    FrameRate    float64  `json:"frame_rate"`    // 帧率
    AudioCodec   string   `json:"audio_codec"`   // 音频编码
    AudioChannels int     `json:"audio_channels"` // 音频声道数
    HasSubtitles bool     `json:"has_subtitles"` // 是否有字幕
}

// 实现 TypedMetadata 接口
func (m *VideoMetadata) TypeName() string {
    return "video.metadata"
}

func (m *VideoMetadata) Schema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "duration":       map[string]string{"type": "integer", "description": "Video duration in seconds"},
            "resolution":     map[string]string{"type": "string", "description": "Video resolution (e.g., 1920x1080)"},
            "codec":          map[string]string{"type": "string", "description": "Video codec (e.g., H.264)"},
            "bitrate":        map[string]string{"type": "integer", "description": "Bitrate in kbps"},
            "frame_rate":     map[string]string{"type": "number", "description": "Frame rate"},
            "audio_codec":    map[string]string{"type": "string", "description": "Audio codec"},
            "audio_channels": map[string]string{"type": "integer", "description": "Number of audio channels"},
            "has_subtitles":  map[string]string{"type": "boolean", "description": "Whether subtitles are present"},
        },
        "required": []string{"duration", "resolution", "codec"},
    }
}

func (m *VideoMetadata) ToMap() map[string]interface{} {
    return map[string]interface{}{
        "duration":       m.Duration,
        "resolution":     m.Resolution,
        "codec":          m.Codec,
        "bitrate":        m.Bitrate,
        "frame_rate":     m.FrameRate,
        "audio_codec":    m.AudioCodec,
        "audio_channels": m.AudioChannels,
        "has_subtitles":  m.HasSubtitles,
    }
}

func (m *VideoMetadata) FromMap(data map[string]interface{}) error {
    if v, ok := data["duration"].(float64); ok {
        m.Duration = int(v)
    }
    if v, ok := data["resolution"].(string); ok {
        m.Resolution = v
    }
    if v, ok := data["codec"].(string); ok {
        m.Codec = v
    }
    if v, ok := data["bitrate"].(float64); ok {
        m.Bitrate = int(v)
    }
    if v, ok := data["frame_rate"].(float64); ok {
        m.FrameRate = v
    }
    if v, ok := data["audio_codec"].(string); ok {
        m.AudioCodec = v
    }
    if v, ok := data["audio_channels"].(float64); ok {
        m.AudioChannels = int(v)
    }
    if v, ok := data["has_subtitles"].(bool); ok {
        m.HasSubtitles = v
    }
    return nil
}
```

### 步骤2：在init()中注册

在你的提取器包的init()函数中注册元数据类型：

```go
package myextractor

import (
    sdk "github.com/addp/meta-extractor-sdk"
)

func init() {
    // 注册自定义元数据类型
    sdk.RegisterMetadataType(&VideoMetadata{})
}
```

### 步骤3：在提取器中使用

```go
package myextractor

import (
    "context"
    "fmt"
    sdk "github.com/addp/meta-extractor-sdk"
)

type VideoExtractor struct{}

func (e *VideoExtractor) SupportedTypes() []string {
    return []string{"video/mp4", "video/avi", "video/mkv"}
}

func (e *VideoExtractor) Priority() int {
    return 80
}

func (e *VideoExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    // 1. 创建基础元数据
    metadata := sdk.NewMetadata(
        input.ObjectKey,
        "Video File",
        input.Size,
    )

    // 2. 提取视频信息（使用ffmpeg或其他库）
    videoMeta := &VideoMetadata{
        Duration:     3600,
        Resolution:   "1920x1080",
        Codec:        "H.264",
        Bitrate:      5000,
        FrameRate:    30.0,
        AudioCodec:   "AAC",
        AudioChannels: 2,
        HasSubtitles: true,
    }

    // 3. 添加类型化元数据（自动序列化并包含类型信息）
    metadata.AddTypedMetadata("video_metadata", videoMeta)

    return metadata, nil
}

func GetExtractor() sdk.MetadataExtractor {
    return &VideoExtractor{}
}
```

### 步骤4：ADDP集成

在ADDP的插件加载器中导入你的包：

```go
// meta/backend/internal/scanner/plugins/plugins.go
package plugins

import (
    "github.com/addp/meta/internal/scanner"

    // 内置插件
    shapefile "github.com/addp/plugins/shapefile-extractor"

    // 第三方插件
    video "github.com/example/video-extractor"  // 你的视频提取器
)

func init() {
    // 注册Shapefile提取器
    scanner.RegisterSDKExtractor(shapefile.GetExtractor())

    // 注册视频提取器
    scanner.RegisterSDKExtractor(video.GetExtractor())
}
```

## 方式2：使用通用map结构（简单场景）

对于简单的元数据，可以不定义类型化结构，直接使用map：

```go
func (e *SimpleExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    metadata := sdk.NewMetadata(input.ObjectKey, "Custom File", input.Size)

    // 直接设置CustomAttrs（不使用类型化元数据）
    metadata.CustomAttrs["custom_field1"] = "value1"
    metadata.CustomAttrs["custom_field2"] = 123
    metadata.CustomAttrs["custom_metadata"] = map[string]interface{}{
        "property1": "value",
        "property2": 456,
    }

    return metadata, nil
}
```

## 两种方式对比

| 特性 | 类型化元数据 | 通用map |
|------|------------|---------|
| 类型安全 | ✅ 编译时检查 | ❌ 运行时错误 |
| JSON Schema | ✅ 自动验证 | ❌ 需手动验证 |
| 文档化 | ✅ 自描述 | ❌ 需外部文档 |
| 查询支持 | ✅ 结构化查询 | ⚠️ 通用JSON查询 |
| 开发成本 | 中 | 低 |
| 维护性 | 高 | 低 |

## 数据库存储示例

### 类型化元数据存储

```json
{
  "meta_item": {
    "attributes": {
      "extracted_metadata": {
        "custom_attrs": {
          "video_metadata": {
            "_type": "video.metadata",
            "_schema": { /* JSON Schema */ },
            "data": {
              "duration": 3600,
              "resolution": "1920x1080",
              "codec": "H.264",
              "bitrate": 5000,
              "frame_rate": 30.0,
              "audio_codec": "AAC",
              "audio_channels": 2,
              "has_subtitles": true
            }
          }
        }
      }
    }
  }
}
```

### PostgreSQL查询示例

```sql
-- 查询所有H.264编码的视频
SELECT *
FROM metadata.meta_item
WHERE attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'codec' = 'H.264';

-- 查询时长超过1小时的视频
SELECT *
FROM metadata.meta_item
WHERE (attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'duration')::int > 3600;

-- 查询所有类型化元数据的类型
SELECT DISTINCT
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->>'_type' as metadata_type
FROM metadata.meta_item
WHERE attributes->'extracted_metadata'->'custom_attrs'->'video_metadata' IS NOT NULL;
```

## 最佳实践

1. **优先使用类型化元数据**
   - 对于复杂的、结构化的元数据
   - 需要验证和文档化的场景
   - 多个项目共享的元数据格式

2. **使用通用map的场景**
   - 原型开发和快速验证
   - 一次性的简单元数据
   - 完全非结构化的数据

3. **命名约定**
   - 类型名使用点分隔：`video.metadata`, `audio.metadata`, `3d.model.metadata`
   - 避免与内置类型冲突：`geo.spatial`, `image.metadata`, `document.metadata`

4. **版本管理**
   - 在Schema中包含版本信息
   - 支持多版本共存和向后兼容

## 示例：完整的音频提取器

```go
package audioextractor

import (
    "context"
    sdk "github.com/addp/meta-extractor-sdk"
)

// AudioMetadata 音频元数据
type AudioMetadata struct {
    Duration      int     `json:"duration"`
    SampleRate    int     `json:"sample_rate"`
    Channels      int     `json:"channels"`
    Bitrate       int     `json:"bitrate"`
    Codec         string  `json:"codec"`
    Artist        string  `json:"artist"`
    Album         string  `json:"album"`
    Title         string  `json:"title"`
    Year          int     `json:"year"`
}

func (m *AudioMetadata) TypeName() string {
    return "audio.metadata"
}

func (m *AudioMetadata) Schema() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "duration":    map[string]string{"type": "integer"},
            "sample_rate": map[string]string{"type": "integer"},
            "channels":    map[string]string{"type": "integer"},
            "bitrate":     map[string]string{"type": "integer"},
            "codec":       map[string]string{"type": "string"},
            "artist":      map[string]string{"type": "string"},
            "album":       map[string]string{"type": "string"},
            "title":       map[string]string{"type": "string"},
            "year":        map[string]string{"type": "integer"},
        },
    }
}

func (m *AudioMetadata) ToMap() map[string]interface{} {
    return map[string]interface{}{
        "duration":    m.Duration,
        "sample_rate": m.SampleRate,
        "channels":    m.Channels,
        "bitrate":     m.Bitrate,
        "codec":       m.Codec,
        "artist":      m.Artist,
        "album":       m.Album,
        "title":       m.Title,
        "year":        m.Year,
    }
}

func (m *AudioMetadata) FromMap(data map[string]interface{}) error {
    // ... 实现反序列化
    return nil
}

func init() {
    sdk.RegisterMetadataType(&AudioMetadata{})
}

type AudioExtractor struct{}

func (e *AudioExtractor) SupportedTypes() []string {
    return []string{"audio/mpeg", "audio/wav", "audio/flac"}
}

func (e *AudioExtractor) Priority() int {
    return 80
}

func (e *AudioExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    metadata := sdk.NewMetadata(input.ObjectKey, "Audio File", input.Size)

    audioMeta := &AudioMetadata{
        Duration:   240,
        SampleRate: 44100,
        Channels:   2,
        Bitrate:    320,
        Codec:      "MP3",
        Artist:     "Example Artist",
        Album:      "Example Album",
        Title:      "Example Song",
        Year:       2024,
    }

    metadata.AddTypedMetadata("audio_metadata", audioMeta)
    return metadata, nil
}

func GetExtractor() sdk.MetadataExtractor {
    return &AudioExtractor{}
}
```

## 总结

第三方开发者**完全可以**定义自己的元数据类型，只需要：

1. ✅ 实现`TypedMetadata`接口
2. ✅ 在自己的init()中调用`sdk.RegisterMetadataType()`
3. ✅ 在提取器中使用`metadata.AddTypedMetadata()`

SDK的init()只注册了**内置类型**，但注册表是**全局的、开放的**，任何包都可以注册新类型。这就是Go语言init()函数和全局注册表的优势！
