# Video Metadata Extractor Plugin

这是一个示例第三方插件，展示如何为ADDP Meta模块创建自定义元数据提取器。

## 特性

- ✅ 定义自定义元数据类型 `VideoMetadata`
- ✅ 支持多种视频格式：MP4, AVI, MKV, MOV, WebM
- ✅ 提取视频元数据：时长、分辨率、编码器、比特率、帧率等
- ✅ 类型安全和JSON Schema验证
- ✅ 独立开发，不依赖ADDP源码

## 目录结构

```
video-extractor/
├── go.mod                  # 模块依赖（仅依赖SDK）
├── video_extractor.go      # 提取器实现
└── README.md               # 本文件
```

## 快速开始

### 1. 编译验证

```bash
cd plugins/video-extractor
go build
```

### 2. 集成到ADDP

在 `meta/backend/internal/scanner/plugins/plugins.go` 中导入：

```go
package plugins

import (
    "github.com/addp/meta/internal/scanner"

    shapefile "github.com/addp/plugins/shapefile-extractor"
    video "github.com/example/addp-video-extractor"  // 添加这行
)

func init() {
    scanner.RegisterSDKExtractor(shapefile.GetExtractor())
    scanner.RegisterSDKExtractor(video.GetExtractor())  // 添加这行
}
```

### 3. 更新依赖

在 `meta/backend/go.mod` 中添加：

```go
require (
    // ...
    github.com/example/addp-video-extractor v0.0.0
)

replace (
    // ...
    github.com/example/addp-video-extractor => ../../plugins/video-extractor
)
```

### 4. 重新编译Meta模块

```bash
cd meta/backend
go mod tidy
go build cmd/server/main.go
```

## 工作原理

### 元数据类型注册

在 `init()` 函数中，插件注册自定义元数据类型：

```go
func init() {
    sdk.RegisterMetadataType(&VideoMetadata{})
}
```

这会将 `video.metadata` 类型添加到全局注册表，使得：
- 元数据可以被序列化时包含类型信息
- 支持JSON Schema验证
- 可以在数据库中结构化查询

### 元数据提取流程

```
1. Meta扫描检测到 .mp4 文件
     ↓
2. 根据MIME类型匹配提取器（video/mp4）
     ↓
3. 调用 VideoExtractor.Extract()
     ↓
4. 提取器读取视频文件头
     ↓
5. 创建 VideoMetadata 结构
     ↓
6. 调用 metadata.AddTypedMetadata("video_metadata", videoMeta)
     ↓
7. SDK自动序列化为：
    {
      "_type": "video.metadata",
      "_schema": { /* JSON Schema */ },
      "data": { /* 实际数据 */ }
    }
     ↓
8. 存储到 meta_item.attributes.extracted_metadata
```

### 数据库存储示例

```json
{
  "meta_item": {
    "name": "demo.mp4",
    "attributes": {
      "extractor_available": true,
      "extractor_type": "*videoextractor.VideoExtractor",
      "content_type": "video/mp4",
      "extracted_metadata": {
        "basic_info": {
          "FileName": "demo.mp4",
          "FileType": "Video File",
          "Size": 52428800,
          "ContentType": "video/mp4"
        },
        "custom_attrs": {
          "video_metadata": {
            "_type": "video.metadata",
            "_schema": {
              "type": "object",
              "properties": {
                "duration": {"type": "integer"},
                "resolution": {"type": "string"},
                "codec": {"type": "string"}
              }
            },
            "data": {
              "duration": 3600,
              "resolution": "1920x1080",
              "codec": "H.264",
              "bitrate": 5000,
              "frame_rate": 30.0,
              "audio_codec": "AAC",
              "audio_channels": 2,
              "has_subtitles": true,
              "container": "MP4"
            }
          },
          "file_extension": ".mp4",
          "is_streaming_ready": true
        }
      }
    }
  }
}
```

## 数据库查询

### 查询所有H.264编码的视频

```sql
SELECT
    item_name,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution' as resolution,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'duration' as duration
FROM metadata.meta_item
WHERE
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'codec' = 'H.264';
```

### 查询时长超过1小时的视频

```sql
SELECT *
FROM metadata.meta_item
WHERE
    (attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'duration')::int > 3600;
```

### 查询所有高清视频（1080p及以上）

```sql
SELECT *
FROM metadata.meta_item
WHERE
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution'
    LIKE '%1080%'
    OR attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution'
    LIKE '%4K%';
```

## API使用示例

### 触发视频元数据提取

```bash
# 上传视频内容到Meta进行提取
curl -X POST 'http://localhost:8082/api/meta/metadata/extract?resource_id=9&object_key=addp/videos/demo.mp4' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/octet-stream' \
  --data-binary @demo.mp4

# 响应
{
  "data": {
    "BasicInfo": {
      "FileName": "demo.mp4",
      "FileType": "Video File",
      "Size": 52428800,
      "ContentType": "video/mp4"
    },
    "CustomAttrs": {
      "video_metadata": {
        "_type": "video.metadata",
        "data": {
          "duration": 3600,
          "resolution": "1920x1080",
          "codec": "H.264"
        }
      }
    }
  },
  "message": "元数据提取成功"
}
```

### 通过Manager预览（自动提取）

```bash
# Manager会自动调用Meta提取元数据
curl 'http://localhost:8081/api/data-explorer/preview?resource_id=9&schema=addp&table=videos/demo.mp4' \
  -H 'Authorization: Bearer <TOKEN>'

# 响应包含extracted_metadata
{
  "mode": "object",
  "object": {
    "path": "videos/demo.mp4",
    "content_type": "video/mp4",
    "extracted_metadata": {
      "custom_attrs": {
        "video_metadata": {
          "_type": "video.metadata",
          "data": {
            "duration": 3600,
            "resolution": "1920x1080"
          }
        }
      }
    }
  }
}
```

## 生产环境增强

当前实现是简化版本，生产环境建议：

### 1. 使用专业视频处理库

```bash
go get github.com/giorgisio/goav
# 或
go get github.com/3d0c/gmf
```

### 2. 实现完整的视频解析

```go
import "github.com/giorgisio/goav/avformat"

func (e *VideoExtractor) extractVideoInfo(input sdk.ExtractInput) *VideoMetadata {
    // 保存到临时文件
    tmpFile, _ := os.CreateTemp("", "video-*.mp4")
    io.Copy(tmpFile, input.Reader)
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    // 使用ffmpeg解析
    ctx := avformat.AvformatAllocContext()
    if avformat.AvformatOpenInput(&ctx, tmpFile.Name(), nil, nil) != 0 {
        return nil
    }
    defer avformat.AvformatCloseInput(ctx)

    // 提取流信息
    if avformat.AvformatFindStreamInfo(ctx, nil) < 0 {
        return nil
    }

    videoMeta := &VideoMetadata{
        Duration:   int(ctx.Duration() / 1000000), // 转换为秒
        Bitrate:    int(ctx.BitRate() / 1000),     // 转换为kbps
        Container:  strings.ToUpper(ctx.Iformat().Name()),
    }

    // 遍历流获取详细信息
    for i := 0; i < int(ctx.NbStreams()); i++ {
        stream := ctx.Streams()[i]
        codec := stream.CodecParameters()

        if codec.CodecType() == avformat.AVMEDIA_TYPE_VIDEO {
            videoMeta.Resolution = fmt.Sprintf("%dx%d", codec.Width(), codec.Height())
            videoMeta.Codec = avformat.AvcodecGetName(codec.CodecId())
            videoMeta.FrameRate = float64(stream.RFrameRate().Num()) / float64(stream.RFrameRate().Den())
        } else if codec.CodecType() == avformat.AVMEDIA_TYPE_AUDIO {
            videoMeta.AudioCodec = avformat.AvcodecGetName(codec.CodecId())
            videoMeta.AudioChannels = codec.Channels()
        }
    }

    return videoMeta
}
```

### 3. 添加缩略图提取

```go
// 在VideoMetadata中添加
type VideoMetadata struct {
    // ...
    ThumbnailBase64 string `json:"thumbnail_base64"` // 缩略图（base64编码）
}

// 提取视频第1秒的帧作为缩略图
func extractThumbnail(videoPath string) string {
    // 使用ffmpeg提取帧
    // ffmpeg -i input.mp4 -ss 00:00:01 -vframes 1 -f image2pipe -
}
```

### 4. 支持流式处理大文件

```go
// 不要一次性加载整个文件到内存
// 使用HTTP Range请求只读取文件头
func (e *VideoExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    // 只读取前10MB进行解析
    limitReader := io.LimitReader(input.Reader, 10*1024*1024)
    // ...
}
```

## 测试

```bash
# 单元测试
go test -v

# 集成测试（需要先启动ADDP）
./test_integration.sh
```

## 许可证

MIT License

## 贡献

欢迎提交Issue和Pull Request！
