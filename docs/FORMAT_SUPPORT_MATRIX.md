# 格式支持矩阵

本文档列出 ADDP 平台各模块对不同数据格式的支持情况。

**图例**：
- ✅ 已实现
- 🔵 计划中
- ⚪ 暂无计划
- ➖ 不适用

---

## 地理空间格式

| 格式 | Meta (元数据扫描) | Manager (数据预览) | Transfer (数据传输) | Common (解析器) | 备注 |
|------|------------------|-------------------|---------------------|----------------|------|
| **Shapefile** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | Meta: ShapefileExtractor<br>Manager: shapefilePreviewProvider<br>Transfer: Shapefile R/W<br>common/geo/shapefile |
| **GeoJSON** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ⚪ | 各模块独立实现 |
| **GeoPackage** | 🔵 计划中 | 🔵 计划中 | ✅ 已实现 | ⚪ | 基于SQLite |
| **KML** | 🔵 计划中 | 🔵 计划中 | 🔵 计划中 | ⚪ | Google Earth格式 |
| **KMZ** | 🔵 计划中 | 🔵 计划中 | 🔵 计划中 | ⚪ | KML压缩包 |

---

## 表格格式

| 格式 | Meta | Manager | Transfer | Common | 备注 |
|------|------|---------|----------|--------|------|
| **CSV** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ⚪ | Meta: CSVExtractor<br>Manager: csvPreviewProvider<br>Transfer: CSV R/W |
| **TSV** | 🔵 计划中 | ✅ 已实现 | ✅ 已实现 | ⚪ | Manager/Transfer支持(与CSV同一实现) |
| **Excel (.xlsx)** | 🔵 计划中 | 🔵 计划中 | 🔵 计划中 | ⚪ | Office文档 |
| **Excel (.xls)** | ⚪ | ⚪ | ⚪ | ⚪ | 旧格式，低优先级 |

---

## 文档格式

| 格式 | Meta | Manager | Transfer | Common | 备注 |
|------|------|---------|----------|--------|------|
| **PDF** | ✅ 已实现 | ✅ 已实现 | ➖ 不适用 | ⚪ | Meta: PDFExtractor<br>Manager: 流式预览 |
| **DOCX** | 🔵 计划中 | ✅ 已实现 | ➖ 不适用 | ⚪ | Manager: 转HTML预览 |
| **PPTX** | 🔵 计划中 | ✅ 已实现 | ➖ 不适用 | ⚪ | Manager: 转HTML预览 |
| **WPS** | ⚪ | ⚪ | ➖ 不适用 | ⚪ | 国产Office格式 |
| **TXT** | ✅ 已实现 | ✅ 已实现 | ➖ 不适用 | ➖ | DefaultExtractor处理 |

---

## 图像格式

| 格式 | Meta | Manager | Transfer | Common | 备注 |
|------|------|---------|----------|--------|------|
| **JPEG** | ✅ 已实现 | ✅ 已实现 | ➖ 不适用 | ⚪ | Meta: ImageExtractor<br>Manager: 缩略图 |
| **PNG** | ✅ 已实现 | ✅ 已实现 | ➖ 不适用 | ⚪ | 同上 |
| **GIF** | ✅ 已实现 | ✅ 已实现 | ➖ 不适用 | ⚪ | 同上 |
| **TIFF** | 🔵 计划中 | 🔵 计划中 | ➖ 不适用 | ⚪ | 地理空间影像 |

---

## 数据库格式

| 格式 | Meta | Manager | Transfer | Common | 备注 |
|------|------|---------|----------|--------|------|
| **PostgreSQL** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ➖ | 通过JDBC/Driver连接 |
| **MySQL** | ✅ 已实现 | 🔵 计划中 | ✅ 已实现 | ➖ | 同上 |
| **SQLite** | ✅ 已实现 | 🔵 计划中 | 🔵 计划中 | ⚪ | Meta: SQLiteExtractor |
| **GeoPackage** | 🔵 计划中 | 🔵 计划中 | ✅ 已实现 | ⚪ | 特殊的SQLite |

---

## 数据交换格式

| 格式 | Meta | Manager | Transfer | Common | 备注 |
|------|------|---------|----------|--------|------|
| **JSON** | ✅ 已实现 | ✅ 已实现 | 🔵 计划中 | ➖ | 标准库支持 |
| **XML** | 🔵 计划中 | 🔵 计划中 | 🔵 计划中 | ⚪ | 低优先级 |
| **Parquet** | 🔵 计划中 | 🔵 计划中 | 🔵 计划中 | 🔵 计划中 | 大数据格式 |
| **Avro** | ⚪ | ⚪ | ⚪ | ⚪ | 低优先级 |

---

## 多媒体格式

| 格式 | Meta | Manager | Transfer | Common | 备注 |
|------|------|---------|----------|--------|------|
| **视频 (MP4/AVI/MOV)** | 🔵 计划中 | 🔵 计划中 | ➖ 不适用 | ⚪ | Meta: 提取时长/分辨率<br>Manager: 视频播放器 |
| **音频 (MP3/WAV)** | 🔵 计划中 | 🔵 计划中 | ➖ 不适用 | ⚪ | Meta: 提取时长/比特率<br>Manager: 音频播放器 |

---

## 对象存储

| 存储类型 | Meta | Manager | Transfer | Common | 备注 |
|---------|------|---------|----------|--------|------|
| **MinIO** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ➖ | S3兼容接口 |
| **AWS S3** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ➖ | 标准S3 API |
| **阿里云OSS** | ✅ 已实现 | ✅ 已实现 | ✅ 已实现 | ➖ | S3兼容模式 |

---

## 各模块功能对比

### Meta 模块 (元数据扫描)

**需求特点**：
- 快速扫描大量文件
- 提取元数据信息（字段、类型、统计）
- 不需要完整读取数据内容
- 支持增量扫描

**已实现的提取器**：
- ✅ GeoJSONExtractor - 地理空间元数据
- ✅ CSVExtractor - 表格元数据
- ✅ PDFExtractor - PDF文档元数据
- ✅ ImageExtractor - 图像EXIF元数据
- ✅ SQLiteExtractor - SQLite数据库元数据
- ✅ DefaultExtractor - 兜底处理器

**注册位置**: `meta/backend/internal/scanner/extractors/`

### Manager 模块 (数据预览)

**需求特点**：
- 快速响应用户预览请求
- 加载部分数据（前N条记录）
- 渲染友好的展示格式
- 支持缩略图生成

**已实现的预览提供者**：
- ✅ ObjectStoragePreviewProvider - 对象存储预览
  - 支持文本、JSON、GeoJSON、图片、PDF、Office文档
- ✅ PostgresPreviewProvider - PostgreSQL数据库表预览
- ✅ NodePreviewProvider - 目录/Schema节点预览

**注册位置**: `manager/backend/internal/service/`

### Transfer 模块 (数据传输)

**需求特点**：
- 完整读写数据
- 批量处理和流式传输
- 高吞吐量
- 支持字段映射和转换

**已实现的连接器**：
- ✅ ShapefileReader/Writer
- ✅ GeoJSONReader/Writer
- ✅ GeoPackageReader/Writer
- ✅ JDBCReader/Writer
- ✅ S3Reader/Writer
- ✅ FileReader/Writer

**注册位置**: `transfer/backend/internal/connector/`

---

## 更新记录

| 日期 | 更新内容 | 更新人 |
|------|---------|--------|
| 2025-01-26 | 初始版本，梳理现有格式支持 | Claude |

---

## 如何更新此文档

当添加新格式支持时，请按以下步骤更新此文档：

1. 在对应的格式类别表格中添加新行
2. 标注每个模块的支持状态（✅/🔵/⚪/➖）
3. 在"备注"列说明实现类名或特殊说明
4. 在"更新记录"表格中添加一行
5. 提交PR时在描述中引用此文档

---

## 相关文档

- [数据格式插件化架构设计](./数据格式插件化架构设计.md) - 总体架构设计
- [插件开发指南](./PLUGIN_DEVELOPMENT_GUIDE.md) - 如何添加新格式支持
- [common/format API文档](../common/format/README.md) - 格式识别和Schema工具
