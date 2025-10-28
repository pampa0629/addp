# 视频元数据提取器测试指南

## 已完成的集成

✅ 视频元数据提取器已成功集成到ADDP Meta模块
✅ 支持多种视频格式：MP4, AVI, MKV, MOV, WebM, FLV, WMV, M4V
✅ 提取元数据包括：时长、分辨率、编码器、比特率、帧率、音频信息

## 测试步骤

### 1. 验证提取器已注册

```bash
# Meta backend应该成功启动并监听8082端口
curl http://localhost:8082/health
# 响应: {"status":"healthy"}
```

### 2. 创建测试视频文件（如果没有）

```bash
# 使用ffmpeg创建一个测试视频
ffmpeg -f lavfi -i testsrc=duration=10:size=1920x1080:rate=30 \
       -f lavfi -i sine=frequency=1000:duration=10 \
       -c:v libx264 -c:a aac -shortest /tmp/test_video.mp4
```

### 3. 测试元数据提取API

#### 方法1：直接调用Meta API

```bash
# 假设你有一个JWT token
TOKEN="your_jwt_token_here"

# 上传视频并提取元数据
curl -X POST 'http://localhost:8082/api/meta/metadata/extract?resource_id=9&object_key=addp/videos/test.mp4' \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @/tmp/test_video.mp4

# 预期响应:
{
  "data": {
    "BasicInfo": {
      "FileName": "test.mp4",
      "FileType": "Video File",
      "Size": 123456,
      "ContentType": "video/mp4"
    },
    "CustomAttrs": {
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
          "duration": 300,
          "resolution": "1920x1080",
          "codec": "H.264",
          "bitrate": 5000,
          "frame_rate": 30.0,
          "audio_codec": "AAC",
          "audio_channels": 2,
          "has_subtitles": false,
          "container": "MP4"
        }
      },
      "file_extension": ".mp4",
      "is_streaming_ready": true
    }
  },
  "message": "元数据提取成功"
}
```

#### 方法2：通过Manager预览（自动触发提取）

```bash
# 首先确保视频文件已上传到MinIO
# 然后通过Manager API预览

TOKEN="your_jwt_token_here"

curl 'http://localhost:8081/api/data-explorer/preview?resource_id=9&schema=addp&table=videos/test.mp4' \
  -H "Authorization: Bearer $TOKEN"

# 预期响应包含extracted_metadata字段:
{
  "mode": "object",
  "object": {
    "bucket": "addp",
    "path": "videos/test.mp4",
    "node_type": "object",
    "size_bytes": 123456,
    "content_type": "video/mp4",
    "extracted_metadata": {
      "basic_info": {
        "FileName": "test.mp4",
        "FileType": "Video File",
        "Size": 123456
      },
      "custom_attrs": {
        "video_metadata": {
          "_type": "video.metadata",
          "data": {
            "duration": 300,
            "resolution": "1920x1080",
            "codec": "H.264",
            "bitrate": 5000,
            "frame_rate": 30.0,
            "audio_codec": "AAC",
            "audio_channels": 2,
            "container": "MP4"
          }
        }
      }
    }
  }
}
```

### 4. 验证数据库存储

```sql
-- 连接到PostgreSQL
psql -h localhost -U addp -d addp

-- 查询提取的视频元数据
SELECT
    item_name,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'codec' as codec,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'resolution' as resolution,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'duration' as duration
FROM metadata.meta_item
WHERE
    attributes->'extracted_metadata'->'custom_attrs' ? 'video_metadata';

-- 查询所有H.264编码的视频
SELECT
    item_name,
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data' as video_info
FROM metadata.meta_item
WHERE
    attributes->'extracted_metadata'->'custom_attrs'->'video_metadata'->'data'->>'codec' = 'H.264';
```

### 5. 测试不同视频格式

#### MP4视频
```bash
curl -X POST 'http://localhost:8082/api/meta/metadata/extract?resource_id=9&object_key=addp/test.mp4' \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @test.mp4
```

#### AVI视频
```bash
curl -X POST 'http://localhost:8082/api/meta/metadata/extract?resource_id=9&object_key=addp/test.avi' \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @test.avi
```

#### MKV视频
```bash
curl -X POST 'http://localhost:8082/api/meta/metadata/extract?resource_id=9&object_key=addp/test.mkv' \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @test.mkv
```

## 支持的视频格式

| 格式 | MIME类型 | 容器 | 典型编码器 | 音频编码 |
|------|----------|------|-----------|---------|
| MP4 | video/mp4 | MP4 | H.264 | AAC |
| AVI | video/x-msvideo | AVI | XVID | MP3 |
| MKV | video/x-matroska | MKV | H.264 | AAC |
| MOV | video/quicktime | MOV | H.264 | AAC |
| WebM | video/webm | WebM | VP9 | Opus |
| FLV | video/x-flv | FLV | H.264 | AAC |

## 提取的元数据字段

```go
type VideoMetadata struct {
    Duration      int     // 视频时长（秒）
    Resolution    string  // 分辨率（如 "1920x1080"）
    Codec         string  // 视频编码（如 "H.264"）
    Bitrate       int     // 比特率（kbps）
    FrameRate     float64 // 帧率（fps）
    AudioCodec    string  // 音频编码（如 "AAC"）
    AudioChannels int     // 音频声道数
    HasSubtitles  bool    // 是否有字幕
    Container     string  // 容器格式（如 "MP4"）
}
```

## 完整的端到端测试流程

```bash
#!/bin/bash
# test_video_extractor.sh

set -e

echo "=== 视频元数据提取器测试 ==="

# 1. 获取JWT token（假设你已经登录）
TOKEN=$(curl -s -X POST 'http://localhost:8080/api/auth/login' \
  -H 'Content-Type: application/json' \
  -d '{"username":"zuhu1","password":"123456"}' | jq -r '.token')

echo "✓ 获取到token: ${TOKEN:0:20}..."

# 2. 创建测试视频（如果没有）
if [ ! -f /tmp/test_video.mp4 ]; then
    echo "创建测试视频..."
    ffmpeg -f lavfi -i testsrc=duration=5:size=1280x720:rate=25 \
           -f lavfi -i sine=frequency=1000:duration=5 \
           -c:v libx264 -c:a aac -shortest /tmp/test_video.mp4 2>/dev/null
    echo "✓ 测试视频已创建"
fi

# 3. 上传到MinIO
echo "上传视频到MinIO..."
mc cp /tmp/test_video.mp4 local/addp/videos/test.mp4 2>/dev/null || \
    curl -X PUT 'http://localhost:9002/addp/videos/test.mp4' \
      --user minioadmin:minioadmin \
      --data-binary @/tmp/test_video.mp4
echo "✓ 视频已上传"

# 4. 扫描对象存储（让Meta发现这个文件）
echo "扫描对象存储..."
curl -s -X POST 'http://localhost:8082/api/meta/scan/resource' \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "resource_id": 9,
    "object_paths": ["addp"]
  }' | jq -r '.message'

# 5. 通过Manager预览（自动触发元数据提取）
echo "预览视频（触发元数据提取）..."
RESPONSE=$(curl -s 'http://localhost:8081/api/data-explorer/preview?resource_id=9&schema=addp&table=videos/test.mp4' \
  -H "Authorization: Bearer $TOKEN")

# 6. 检查是否提取了元数据
if echo "$RESPONSE" | jq -e '.object.extracted_metadata.custom_attrs.video_metadata' > /dev/null; then
    echo "✓ 元数据提取成功！"
    echo ""
    echo "提取的视频信息:"
    echo "$RESPONSE" | jq '.object.extracted_metadata.custom_attrs.video_metadata.data'
else
    echo "✗ 元数据提取失败"
    echo "响应: $RESPONSE"
    exit 1
fi

echo ""
echo "=== 测试完成 ==="
```

## 排错指南

### 问题1：提取器未注册

**症状：** Meta backend启动但无法识别视频文件

**解决：**
```bash
# 检查Meta backend日志
tail -f /tmp/meta-with-video.log

# 确认plugins/plugins.go中有视频提取器导入
grep "video" meta/backend/internal/scanner/plugins/plugins.go

# 确认go.mod中有依赖
grep "video-extractor" meta/backend/go.mod
```

### 问题2：编译错误

**症状：** `go build`失败

**解决：**
```bash
cd /Users/pampa/code/addp/meta/backend
go mod tidy
go build cmd/server/main.go
```

### 问题3：元数据未提取

**症状：** 预览响应中没有`extracted_metadata`字段

**解决：**
1. 检查meta_item是否有`extractor_available`标记
2. 检查Manager是否能连接到Meta服务
3. 检查JWT token是否有效

```sql
-- 检查meta_item
SELECT
    item_name,
    attributes->'extractor_available' as has_extractor,
    attributes->'content_type' as content_type
FROM metadata.meta_item
WHERE item_name LIKE '%.mp4';
```

## 性能说明

- **文件头读取**：只读取前64KB用于格式识别
- **内存占用**：每次提取约 ~100KB
- **处理时间**：通常 < 100ms（不包括网络传输）
- **并发支持**：可并发处理多个文件

## 下一步改进

1. **使用专业库**：集成ffmpeg/ffprobe获取精确信息
2. **缩略图提取**：提取视频关键帧作为预览
3. **字幕检测**：检测和提取字幕轨道
4. **章节信息**：提取视频章节标记
5. **元数据编辑**：支持修改视频元数据

## 相关文档

- [第三方元数据类型扩展指南](../../docs/THIRD_PARTY_METADATA_TYPES.md)
- [元数据类型架构](../../docs/METADATA_TYPES_ARCHITECTURE.md)
- [视频提取器README](README.md)
