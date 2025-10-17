# 视频元数据提取现状说明

## 🎯 您的问题

> 对于视频数据，是否提取了视频时长、视频大小等元数据，这些数据是否存储到"数据库: PostgreSQL addp 数据库，Schema: metadata schema，表: meta_item 表，字段: attributes 列（JSONB类型）"这里了？

## ✅ 已完成的部分

### 1. 视频元数据提取器（插件）

**位置**: [plugins/video-extractor/video_extractor.go](../plugins/video-extractor/video_extractor.go)

**功能**：可以提取以下元数据
- ✅ `duration`（视频时长，秒）
- ✅ `resolution`（分辨率，如"1920x1080"）
- ✅ `codec`（视频编码，如"H.264"）
- ✅ `bitrate`（比特率，bps）
- ✅ `frame_rate`（帧率，如29.97）
- ✅ `audio_codec`（音频编码，如"AAC"）
- ✅ `audio_channels`（音频声道数，如2）
- ✅ `has_subtitles`（是否有字幕）
- ✅ `container`（容器格式，如"MP4"）

**支持的格式**：
- MP4, MOV, M4V（MPEG-4容器）
- AVI（RIFF容器）
- MKV, WebM（Matroska/EBML容器）
- FLV（Flash Video）
- WMV（Windows Media Video）

**提取方式**：
- 基于文件头（header）解析
- 读取前64KB用于格式识别
- 针对不同容器格式使用不同的解析逻辑

### 2. SDK类型系统

**位置**: [meta/sdk/extractor_sdk.go](../meta/sdk/extractor_sdk.go)

- ✅ `TypedMetadata` 接口定义
- ✅ `VideoMetadata` 结构体（在插件中）
- ✅ 自动序列化为JSON格式（`_type`, `_schema`, `data`）
- ✅ 类型注册机制（`RegisterMetadataType()`）

### 3. Meta Backend集成

**位置**: [meta/backend/internal/scanner/plugins/plugins.go](../meta/backend/internal/scanner/plugins/plugins.go)

- ✅ 视频提取器已注册到Meta Backend
- ✅ 使用SDK适配器桥接插件和内部类型
- ✅ Meta Backend启动时自动加载视频提取器

### 4. 数据库表结构

**表**: `metadata.meta_item`

- ✅ `attributes` 字段（JSONB类型）已就绪
- ✅ 可以存储任意JSON结构的元数据
- ✅ 支持高效的JSONB查询和索引

## ❌ 缺失的部分

### **关键问题：S3扫描器没有调用元数据提取器**

**现状**：
- S3扫描器（[meta/backend/internal/scanner/s3_scanner.go](../meta/backend/internal/scanner/s3_scanner.go)）在扫描MinIO/S3对象时，**只提取了基本信息**：
  ```go
  ObjectMetadata{
      Bucket:       "addp",
      Path:         "addp/videos/demo.mp4",
      RelativePath: "videos/demo.mp4",
      NodeType:     "object",
      FileType:     "mp4",
      SizeBytes:    123456789,      // ✅ 文件大小（字节）
      ObjectCount:  1,
      LastModified: &time.Time{},   // ✅ 最后修改时间
  }
  ```

- **没有调用**视频提取器来获取详细元数据（duration, resolution, codec等）

**结果**：
- 当前存储在 `metadata.meta_item.attributes` 的数据**只有**：
  ```json
  {
    "path": "addp/videos/demo.mp4",
    "bucket": "addp",
    "file_type": "mp4",
    "relative_path": "videos/demo.mp4",
    "last_modified_at": "2025-10-17T08:09:15.802Z"
  }
  ```

- **缺失**的视频元数据（应该有但没有）：
  ```json
  {
    "video_metadata": {
      "_type": "video.basic",
      "data": {
        "duration": 3600,          // ❌ 缺失
        "resolution": "1920x1080", // ❌ 缺失
        "codec": "H.264",          // ❌ 缺失
        "bitrate": 5000000,        // ❌ 缺失
        "frame_rate": 29.97        // ❌ 缺失
        // ... 其他字段
      }
    }
  }
  ```

## 🔧 需要完成的工作

### 步骤1：修改S3扫描器，集成元数据提取

**文件**: `meta/backend/internal/scanner/s3_scanner.go`

**需要添加的逻辑**：
```go
// 在 scanBucket 或 ScanPath 函数中，当发现对象时：

// 1. 根据Content-Type或文件扩展名获取对应的提取器
extractor := registry.GetExtractor(contentType)  // 需要实现

// 2. 从MinIO读取对象内容
ctx := context.Background()
obj, err := s.client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
if err != nil {
    // 处理错误
}
defer obj.Close()

// 3. 调用提取器
extractInput := scanner.ExtractInput{
    ResourceID:   resID,
    ObjectKey:    objectKey,
    ContentType:  contentType,
    Size:         objectSize,
    Reader:       obj,
    LastModified: lastModified,
}

metadata, err := extractor.Extract(ctx, extractInput)
if err != nil {
    // 提取失败，只存储基本信息
}

// 4. 将提取的元数据存储到 attributes 字段
if metadata != nil {
    // metadata.CustomAttrs 包含了 video_metadata 等类型化元数据
    item.Attributes = models.JSONMap(metadata.CustomAttrs)
}
```

### 步骤2：实现提取器注册表查询

**文件**: `meta/backend/internal/scanner/registry.go`

需要实现：
```go
// GetExtractorByContentType 根据Content-Type获取合适的提取器
func GetExtractorByContentType(contentType string) MetadataExtractor {
    // 遍历已注册的提取器，匹配SupportedTypes()
    // 支持通配符匹配（如 "video/*"）
    // 按优先级排序
}

// GetExtractorByExtension 根据文件扩展名获取提取器
func GetExtractorByExtension(ext string) MetadataExtractor {
    // 作为Content-Type的后备方案
}
```

### 步骤3：优化性能（可选）

**考虑因素**：
- 视频文件可能很大（GB级别）
- 全量下载会消耗大量带宽和时间
- 视频提取器只需要前64KB用于格式识别

**优化方案**：
```go
// 使用 Range 请求只下载文件头部
opts := minio.GetObjectOptions{}
opts.SetRange(0, 65535)  // 只读取前64KB
obj, err := s.client.GetObject(ctx, bucket, objectKey, opts)
```

### 步骤4：测试和验证

1. 上传测试视频到MinIO
2. 触发Meta扫描
3. 查询数据库验证元数据是否存储：
   ```sql
   SELECT
       name,
       jsonb_pretty(attributes)
   FROM metadata.meta_item
   WHERE file_type = 'mp4';
   ```

## 📊 当前数据库中的实际数据

**查询示例**：
```bash
docker exec addp-postgres psql -U addp -d addp -c \
  "SELECT name, attributes->'video_metadata' as video_meta \
   FROM metadata.meta_item \
   WHERE attributes->'file_type' = '\"mp4\"';"
```

**预期结果**（当前）：
```
 name        | video_meta
-------------+------------
 demo.mp4    | (null)       ← ❌ 没有视频元数据
```

**期望结果**（完成后）：
```
 name        | video_meta
-------------+--------------------------------------------------------------
 demo.mp4    | {"_type": "video.basic", "data": {"duration": 3600, ...}}  ← ✅
```

## 🎯 总结

### 当前状态

| 组件 | 状态 | 说明 |
|------|------|------|
| 视频提取器插件 | ✅ 完成 | 可以提取duration、resolution等元数据 |
| SDK类型系统 | ✅ 完成 | TypedMetadata接口和序列化 |
| Meta Backend集成 | ✅ 完成 | 提取器已注册 |
| 数据库表结构 | ✅ 就绪 | attributes JSONB字段可用 |
| **S3扫描器调用** | ❌ **缺失** | **未调用提取器，是关键缺失环节** |
| 元数据存储 | ❌ 缺失 | 因此视频元数据未存储到数据库 |

### 回答您的问题

**问题1**: 是否提取了视频时长、视频大小等元数据？
- **答案**: ❌ **没有**。虽然提取器已实现，但S3扫描器未调用它。
- **文件大小**: ✅ 已提取（基本信息，`size_bytes` 字段）
- **视频时长**: ❌ 未提取（需要调用视频提取器）
- **分辨率、编码等**: ❌ 未提取

**问题2**: 这些数据是否存储到 `metadata.meta_item.attributes`？
- **答案**: ❌ **没有**。只存储了基本的path、bucket、file_type等信息，没有存储视频特定的元数据（duration、resolution等）。

### 优先级

**高优先级**：
1. 在S3扫描器中集成元数据提取器调用
2. 实现提取器查询和匹配逻辑

**中优先级**：
3. 优化大文件的提取性能（Range请求）
4. 添加提取失败的错误处理

**低优先级**：
5. 为其他文件类型添加更多提取器（音频、3D模型等）

## 📚 相关文档

- [METADATA_STORAGE.md](./METADATA_STORAGE.md) - 元数据存储架构详解
- [THIRD_PARTY_METADATA_TYPES.md](./THIRD_PARTY_METADATA_TYPES.md) - 第三方元数据类型扩展
- [plugins/video-extractor/README.md](../plugins/video-extractor/README.md) - 视频提取器使用文档
- [plugins/video-extractor/TEST.md](../plugins/video-extractor/TEST.md) - 测试指南

## 🚀 下一步行动

1. **立即可做**：查询数据库确认当前存储的数据
2. **核心任务**：修改S3扫描器，集成元数据提取器
3. **验证**：上传测试视频并验证元数据提取
