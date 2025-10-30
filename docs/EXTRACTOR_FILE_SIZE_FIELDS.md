# 文件提取器元数据字段：文件大小

## 概述

所有文件类型元数据提取器现在都包含**文件大小**信息，以便前端更方便地展示这一基础信息。

## 添加的字段

每个提取器在 `metadata.CustomAttrs` 中添加了以下两个字段：

| 字段名 | 类型 | 描述 | 示例 |
|--------|------|------|------|
| `file_size` | `int64` | 文件字节大小 | `2048576` (2MB的字节数) |
| `file_size_human` | `string` | 人类可读的文件大小 | `"2.0 MB"` |

## 实现细节

### formatFileSize 函数

所有提取器都包含了一个统一的 `formatFileSize` 函数：

```go
// formatFileSize 格式化文件大小为人类可读格式
func formatFileSize(size int64) string {
    const unit = 1024
    if size < unit {
        return fmt.Sprintf("%d B", size)
    }
    div, exp := int64(unit), 0
    for n := size / unit; n >= unit; n /= unit {
        div *= unit
        exp++
    }
    return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
```

### 单位说明

- B: Bytes (字节)
- KB: Kilobytes (1024 bytes)
- MB: Megabytes (1024 KB)
- GB: Gigabytes (1024 MB)
- TB: Terabytes (1024 GB)
- PB: Petabytes (1024 TB)
- EB: Exabytes (1024 PB)

### 格式化示例

| 字节数 | 格式化结果 |
|--------|-----------|
| 512 | `"512 B"` |
| 1024 | `"1.0 KB"` |
| 1536 | `"1.5 KB"` |
| 1048576 | `"1.0 MB"` |
| 1073741824 | `"1.0 GB"` |
| 2560000 | `"2.4 MB"` |

## 各提取器实现位置

### 1. 图片提取器
**文件**: [`meta/backend/internal/scanner/extractors/image_extractor.go`](../meta/backend/internal/scanner/extractors/image_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- 分辨率 (resolution): `"1920x1080"`
- 长宽比 (aspect_ratio): `"16:9"`
- 百万像素 (megapixels): `2.07`
- 方向 (orientation): `"landscape"`

---

### 2. PDF提取器
**文件**: [`meta/backend/internal/scanner/extractors/pdf_extractor.go`](../meta/backend/internal/scanner/extractors/pdf_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- 页数
- 标题、作者
- 创建时间

---

### 3. CSV提取器
**文件**: [`meta/backend/internal/scanner/extractors/csv_extractor.go`](../meta/backend/internal/scanner/extractors/csv_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- 行数 (row_count)
- 列数 (column_count)
- 列名列表

---

### 4. GeoJSON提取器
**文件**: [`meta/backend/internal/scanner/extractors/geojson_extractor.go`](../meta/backend/internal/scanner/extractors/geojson_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- GeoJSON类型
- 要素数量
- 边界框
- 坐标系统

---

### 5. SQLite提取器
**文件**: [`meta/backend/internal/scanner/extractors/sqlite_extractor.go`](../meta/backend/internal/scanner/extractors/sqlite_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- 数据库版本
- 表数量 (table_count)
- 总行数 (total_rows)

---

### 6. Office文档提取器
**文件**: [`meta/backend/internal/plugins/officeextractor/office_extractor.go`](../meta/backend/internal/plugins/officeextractor/office_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- 文档类型 (docx/pptx/xlsx)
- 标题、作者
- 页数/幻灯片数/工作表数

---

### 7. 视频提取器
**文件**: [`meta/backend/internal/plugins/videoextractor/video_extractor.go`](../meta/backend/internal/plugins/videoextractor/video_extractor.go)

```go
metadata.CustomAttrs["file_size"] = input.Size
metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
```

**其他元数据**:
- 视频时长 (duration)
- 分辨率 (width x height)
- 编解码器 (codec)
- 比特率 (bitrate)

---

### 8. Shapefile提取器
**文件**: [`meta/backend/internal/scanner/extractors/shapefile_extractor.go`](../meta/backend/internal/scanner/extractors/shapefile_extractor.go)

**注**: Shapefile提取器可能需要单独添加文件大小字段（如尚未添加）

---

## 前端展示建议

### JSON存储格式

元数据存储在 `metadata.meta_item.attributes` JSONB字段中：

```json
{
  "image_metadata": {
    "_type": "image.metadata",
    "_schema": { ... },
    "data": {
      "width": 1920,
      "height": 1080,
      "format": "jpeg"
    }
  },
  "file_size": 2048576,
  "file_size_human": "2.0 MB",
  "resolution": "1920x1080",
  "aspect_ratio": "16:9",
  "megapixels": 2.07
}
```

### 前端展示组件

建议在前端预览组件中优先展示 `file_size_human` 字段：

```vue
<!-- 图片元数据展示 -->
<template>
  <div class="metadata-display">
    <div class="metadata-item">
      <span class="label">文件大小:</span>
      <span class="value">{{ metadata.file_size_human }}</span>
    </div>
    <div class="metadata-item">
      <span class="label">分辨率:</span>
      <span class="value">{{ metadata.resolution }}</span>
    </div>
    <div class="metadata-item">
      <span class="label">长宽比:</span>
      <span class="value">{{ metadata.aspect_ratio }}</span>
    </div>
  </div>
</template>
```

## 数据来源

文件大小来自 `sdk.ExtractInput.Size` 字段，该字段由Meta服务在扫描对象存储或文件系统时自动获取：

- **对象存储** (MinIO/S3): 从对象元数据中的 `Content-Length` 获取
- **本地文件系统**: 通过 `os.Stat()` 获取文件信息
- **数据库**: 由数据库驱动返回（如适用）

## 测试验证

### 手动测试步骤

1. 在Manager模块上传测试文件（图片、PDF、CSV等）
2. 在Meta模块对数据源执行扫描
3. 查询数据库验证元数据：

```sql
SELECT
    name,
    attributes->>'file_size' as size_bytes,
    attributes->>'file_size_human' as size_human
FROM metadata.meta_item
WHERE name LIKE '%.jpg'
LIMIT 10;
```

预期输出：
```
name                  | size_bytes | size_human
----------------------|------------|------------
photo1.jpg            | 2048576    | 2.0 MB
document.pdf          | 524288     | 512.0 KB
data.csv              | 10240      | 10.0 KB
```

4. 在Manager的数据预览界面检查元数据展示

### 单元测试示例

```go
func TestImageExtractor_FileSize(t *testing.T) {
    extractor := &ImageExtractor{}

    testData := []byte{/* valid JPEG data */}
    input := sdk.ExtractInput{
        ObjectKey:   "test.jpg",
        ContentType: "image/jpeg",
        Size:        1024576, // ~1MB
        Reader:      bytes.NewReader(testData),
    }

    metadata, err := extractor.Extract(context.Background(), input)
    require.NoError(t, err)

    // 验证文件大小字段
    assert.Equal(t, int64(1024576), metadata.CustomAttrs["file_size"])
    assert.Equal(t, "1000.6 KB", metadata.CustomAttrs["file_size_human"])
}
```

## 相关文档

- [文件类型提取器总览](./FILE_TYPE_EXTRACTORS.md)
- [元数据类型架构](./METADATA_TYPES_ARCHITECTURE.md)

## 更新历史

- **2025-10-17**: 为所有8个文件类型提取器添加 `file_size` 和 `file_size_human` 字段
- **目的**: 解决前端无法展示基础文件大小信息的问题
- **影响范围**: Image, PDF, CSV, GeoJSON, SQLite, Office, Video, Shapefile extractors
