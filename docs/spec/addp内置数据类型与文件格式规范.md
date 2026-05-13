# ADDP 内置数据类型与文件格式规范

本文定义 ADDP 首批内置数据类型与文件格式的确定性落地规则。概念边界见 [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)，item 识别规则见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)，attributes 写入规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

本文只记录已经形成规范共识的格式。尚未定稿的格式、插件 manifest、whole scope explain / confidence 等问题记录在 [ADDP 数据类型与文件格式待规范事项](../next/addp数据类型与文件格式待规范事项.md)。

## 编写模板

新增内置格式时，按以下结构补充：

| 小节 | 必须说明 |
|---|---|
| 识别与组织 | `organization`、`data_type`、`format`、主资源或 whole scope、组件规则 |
| attributes 写入 | `storage`、`item`、`type_info`、`format_info`、`capabilities` 的事实归属 |
| 消费要求 | Manager、Transfer、Search 等模块应如何消费已入库 meta item |
| 格式约束 | 不得重复推断、不得写入错误分区、不得保留旧字段等约束 |

格式私有字段只进入 `format_info.<format>`；跨格式能力只进入 `capabilities.<capability>`；字段、行数、页数、宽高、子对象等类型信息只进入 `type_info.<data_type>`。

## 总览

| 格式 / 场景 | `organization` | `data_type` | `format` | 说明 |
|---|---|---|---|---|
| CSV | `single` | `table` | `csv` | 单资源表格文件 |
| TSV | `single` | `table` | `tsv` | 单资源表格文件 |
| Excel | `single` | `container` | `excel` | 外层工作簿先作为容器 item |
| records JSON / JSON Lines | `single` | `table` | `json` | 行列结构 JSON |
| GeoJSON / FeatureCollection | `single` | `table` | `json` | JSON 格式 + spatial 横切能力 |
| 任意对象 JSON / 配置 JSON | `single` | `document` 或 `container` | `json` | 按平台消费方式判断 |
| Shapefile | `multi` | `table` | `shapefile` | 同目录或同 prefix 的同 basename 组件 |
| 单个 Parquet | `single` | `table` | `parquet` | 单文件表 |
| sibling Parquet 文件组 | `multi` | `table` | `parquet` | 仅在有明确组件或 manifest 规则时成立 |
| ORC / Avro 单文件 | `single` | `table` | `orc` / `avro` | 单文件表 |
| Iceberg 表目录 | `whole` | `table` | `iceberg` | 整体表目录，需 whole scope 规则 |
| SQLite | `single` | `container` | `sqlite` | 内部表先写入 `type_info.container.children` |
| GeoPackage | `single` | `container` | `geopackage` | 内部 layer 先写入 `type_info.container.children` |
| 图片 | `single` | `media` | `jpeg` / `png` / `gif` / `tiff` / `image` | GPS 或 GeoTIFF 空间语义进入 spatial |
| 视频 | `single` | `media` | `mp4` / `mov` / `mkv` / `avi` / `webm` / `video` | 第一阶段以元信息和 range / stream 播放为主 |
| 音频 | `single` | `media` | `mp3` / `wav` / `flac` / `aac` / `ogg` / `audio` | 第一阶段以元信息和 range / stream 播放为主 |
| PDF | `single` | `document` | `pdf` | 文档元信息和提取状态分区写入 |

## CSV / TSV

### 识别与组织

| 维度 | CSV | TSV |
|---|---|---|
| `organization` | `single` | `single` |
| `data_type` | `table` | `table` |
| `format` | `csv` | `tsv` |
| 主资源 | `meta_item.full_name` 指向文件资源 | `meta_item.full_name` 指向文件资源 |

CSV / TSV 是单资源表格文件。字段名来自表头；无表头时由 parser 生成稳定列名。字段类型来自采样推断。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.table` | `fields`、`row_count`、`primary_key`、采样信息 |
| `format_info.csv` / `format_info.tsv` | `encoding`、`delimiter`、`has_header`、`quote_char` 等格式私有信息 |
| `capabilities.statistics` | 采样统计、画像摘要、空值率等可选统计能力 |

### 消费要求

Manager 和 Transfer 必须使用 Meta 已入库的 `format`、`type_info.table` 和 `format_info.csv|tsv`。不得按扩展名二次猜测分隔符、编码或表头。

### 格式约束

- 不得把 CSV / TSV 放入 `document`，除非明确按文档而不是表格消费。
- 不得把 parser 采样字段写入 attributes 顶层。
- 不得在 Manager 中重新实现 CSV / TSV 格式判断。

## Excel

### 识别与组织

| 维度 | 取值 |
|---|---|
| `organization` | `single` |
| `data_type` | `container` |
| `format` | `excel` |
| 主资源 | `meta_item.full_name` 指向工作簿文件 |

当前阶段 Excel 文件先作为一个容器 item。内部 sheet 是内部子 item，不自动展开为独立 `meta_item`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.container` | `children`、`default_child`、`child_count`、sheet 摘要 |
| `format_info.excel` | 工作簿版本、sheet 数量、默认 sheet、表头判断、采样策略等 |

### 消费要求

Manager 可以基于 `type_info.container.children` 展示 sheet 列表；进入某个 sheet 的表格内容读取时，应由容器读取能力定位内部对象，再交给 `TableInfoProvider` / `TableSampleReader` 归一为表语义。

### 格式约束

- 未形成子 item 规范前，不得按 sheet 自动生成独立 `meta_item`。
- 不得把所有 sheet 字段合并成外层工作簿的 `type_info.table.fields`。
- 不得改变 Manager / Transfer 的外层 item 路由语义。

## JSON

### 识别与组织

| 场景 | `organization` | `data_type` | `format` |
|---|---|---|---|
| records array | `single` | `table` | `json` |
| JSON Lines | `single` | `table` | `json` |
| FeatureCollection / GeoJSON 类空间结构 | `single` | `table` | `json` |
| 任意对象、配置文件、嵌套文档 | `single` | `document` 或 `container` | `json` |

`.json` 后缀不能直接等同于空间格式，也不能直接等同于表格。必须验证内容结构，并按平台消费方式确定 `data_type`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.table` | records / JSON Lines / FeatureCollection 的字段、行数、采样信息 |
| `type_info.document` | 文档型 JSON 的标题、摘要、语言、文本片段等可选信息 |
| `type_info.container` | 容器型 JSON 的内部对象摘要、默认入口、子对象数量 |
| `format_info.json` | `json_type`、结构特征、编码、对象层级摘要等格式私有信息 |
| `capabilities.spatial` | 仅空间结构 JSON 写入几何字段、SRID / CRS、extent 等空间能力 |

### 消费要求

Manager 和 Transfer 面向 `data_type` 选择 provider。带空间结构的 JSON 仍是 `format=json`，空间语义由 `capabilities.spatial` 表达。

### 格式约束

- 不得引入独立顶层 `format=geojson`；GeoJSON 类结构应表达为 `format=json` + `capabilities.spatial`。
- 不得只按扩展名把 JSON 判为 `table` 或 `spatial`。
- 不得把 JSON 私有结构字段写入 `capabilities.spatial`。

## Shapefile

### 识别与组织

| 维度 | 取值 |
|---|---|
| `organization` | `multi` |
| `data_type` | `table` |
| `format` | `shapefile` |
| 主资源 | `.shp`，即 `meta_item.full_name` |
| 必需组件 | `.shp`、`.shx`、`.dbf` |
| 可选组件 | `.prj`、`.cpg`、`.sbn`、`.sbx` |

Shapefile 是空间矢量表，不是单个 `.shp` 文件。组件匹配规则是同目录或同 prefix 下相同 basename；不得跨目录递归匹配；不独占目录。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization=multi`、`data_type=table`、`format=shapefile`、`component_files`、`file_count` |
| `type_info.table` | `.dbf` 非空间字段、平台统一几何字段、`row_count`、`primary_key` |
| `format_info.shapefile` | `base_name`、`component_extensions`、`has_prj`、`has_cpg`、`shape_type`、DBF 私有信息 |
| `capabilities.spatial` | `geometry_columns`、`primary_geometry_column`、`srid` 或 `crs`、`extent`、`has_spatial_index` |

字段规则：

- `.dbf` 提供非空间字段。
- `.shp` 提供平台统一几何字段。
- 字段类型映射为 ADDP 通用字段类型，原始 DBF 类型保留在字段 `original_type`。
- 记录数来自真实 Shapefile 记录数，不写固定占位值。

### 标准写入示例

```json
{
  "schema_version": 1,
  "storage": {
    "physical_path": "/shp/",
    "total_size": 3069403
  },
  "item": {
    "organization": "multi",
    "data_type": "table",
    "format": "shapefile",
    "component_files": [
      "/shp/farmland.dbf",
      "/shp/farmland.prj",
      "/shp/farmland.shp",
      "/shp/farmland.shx"
    ],
    "file_count": 4
  },
  "type_info": {
    "table": {
      "fields": [
        {
          "name": "geometry",
          "type": "geometry",
          "original_type": "Polygon",
          "nullable": false
        }
      ],
      "row_count": 1234,
      "primary_key": []
    }
  },
  "format_info": {
    "shapefile": {
      "base_name": "farmland",
      "component_extensions": ["dbf", "prj", "shp", "shx"],
      "has_prj": true,
      "has_cpg": false,
      "shape_type": "Polygon"
    }
  },
  "capabilities": {
    "spatial": {
      "geometry_columns": [
        {
          "name": "geometry",
          "geometry_type": "Polygon",
          "srid": 0,
          "dimension": 2,
          "nullable": false
        }
      ],
      "primary_geometry_column": "geometry",
      "extent": null,
      "has_spatial_index": false
    }
  }
}
```

### 消费要求

Manager 内容读取必须使用 `meta_item.full_name` 作为主文件路径，并使用 `attributes.item.component_files` 读取组件文件。Transfer 写出 Shapefile 时必须明确组件提交边界，不能只写 `.shp`。

### 格式约束

- 不得把 `.shp` 单独作为完整 Shapefile item。
- 不得把 Shapefile 作为 whole scope detector。
- 不得把 `base_name`、`component_extensions`、`has_prj`、`has_cpg` 写入 attributes 顶层或长期写入 `format_info.unqualified`。
- 不得让 Manager 重新枚举 sibling 后猜测组件。

## Parquet / ORC / Avro / Iceberg

### 识别与组织

| 场景 | `organization` | `data_type` | `format` |
|---|---|---|---|
| 单个 Parquet 文件 | `single` | `table` | `parquet` |
| 一组明确归并的 sibling Parquet 文件 | `multi` | `table` | `parquet` |
| 单个 ORC 文件 | `single` | `table` | `orc` |
| 单个 Avro 文件 | `single` | `table` | `avro` |
| Iceberg 表目录 | `whole` | `table` | `iceberg` |

Parquet、ORC、Avro 是表格型数据的文件格式，不应直接称为“湖表”。一组同类 Parquet 文件只有在有明确组件规则或 manifest 规则时才能归并为 `multi` item。Iceberg 等表格式目录由规范声明后可作为 `organization=whole` 的 table item。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type=table`、`format`、可选 `component_files`、whole scope 的 `scope_exclusive` 和 `claim_policy` |
| `type_info.table` | 字段、原始字段类型、行数或估算行数、采样信息 |
| `format_info.<format>` | 文件 footer、编码、压缩、row group、schema 版本、manifest 摘要等格式私有信息 |
| `capabilities.partitioning` | 分区字段、分区数量、分区样例 |
| `capabilities.statistics` | 可轻量获得的列统计、采样统计 |

`whole` item 的范围由 `meta_item.full_name` 表达，`item.scope_exclusive=true`、`item.claim_policy=whole_scope` 表达独占语义。`component_files` 只包含规范认定的数据文件或 manifest 关键资源，不包含 `_SUCCESS`、`_metadata`、`_common_metadata`、CRC 等辅助文件，除非具体格式规范另有说明。

### 消费要求

上层统一按 `data_type=table` 消费。单文件表、multi 文件表、scope 表和引擎原生表的读取差异由 resource 抽象和 `TableInfoProvider` / `TableSampleReader` 收口，不向 Manager / Transfer 暴露 `filetable` / `laketable` 两套业务概念。

### 格式约束

- 不得把多个独立 sibling Parquet 文件误合成一个 `whole` item。
- 不得把 Parquet 直接叫作湖表。
- 不得在 Manager 中按目录临时拼装 scope 表；whole scope 必须由 Meta 已入库 item 表达。

## SQLite / GeoPackage

### 识别与组织

| 格式 | `organization` | `data_type` | `format` | 主资源 |
|---|---|---|---|---|
| SQLite | `single` | `container` | `sqlite` | SQLite 文件 |
| GeoPackage | `single` | `container` | `geopackage` | GeoPackage 文件 |

容器文件本身先作为一个 item；`meta_item.full_name` 指向容器文件。内部表、view、图层等先写入 `type_info.container.children`。

### attributes 写入

| 分区 | SQLite | GeoPackage |
|---|---|---|
| `item` | `organization`、`data_type`、`format` | `organization`、`data_type`、`format` |
| `type_info.container` | 内部表、view、默认入口、对象数量 | 内部 layer、表、默认入口、对象数量 |
| `format_info.sqlite` | SQLite 版本、内部表数量、表清单、pragma 摘要 | 不适用 |
| `format_info.geopackage` | 不适用 | gpkg 元数据、layer 清单、扩展信息 |
| `capabilities.spatial` | 仅 SpatiaLite 等可确认空间能力时写入 | layer 空间字段、SRID / CRS、extent 等空间能力 |

### 消费要求

Manager 展示容器 children 时消费 `type_info.container`；进入内部表或 layer 预览时，由容器读取能力定位内部对象，再交给 table info / sample reader 和 spatial 横切能力处理。

### 格式约束

- 未形成子 item 规范前，不得自动展开内部表或 layer 为独立 `meta_item`。
- 不得把容器内所有表字段合并成外层 item 的 `type_info.table.fields`。
- 不得把 GeoPackage 的格式私有元数据混入 `capabilities.spatial`。

## 图片

### 识别与组织

| 维度 | 取值 |
|---|---|
| `organization` | `single` |
| `data_type` | `media` |
| `format` | `jpeg`、`png`、`gif`、`tiff`、`image` |
| 主资源 | `meta_item.full_name` 指向图片文件 |

`image` 是图片兜底格式，只在无法稳定识别具体图片格式时使用。JPEG、PNG、GIF、TIFF 等具体格式应优先写入具体 `format`。GeoTIFF 不新增独立基础格式，表达为 `format=tiff + capabilities.spatial`。

WebP、BMP、SVG、AVIF、HEIC / HEIF 进入内置主线前，应先明确 descriptor、MIME、预览方式和后端解析边界；在仅能 raw / range 预览时，不应标记为后端已经具备完整 `MediaInfoProvider`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.media` | `kind=image`、宽高、颜色模式、方向、编码、帧数或页数等媒体信息 |
| `format_info.<format>` | EXIF、TIFF tag、压缩方式等格式私有信息 |
| `capabilities.spatial` | 图片 GPS 或 GeoTIFF 可确定空间信息 |

### 消费要求

图片预览面向 `data_type=media`。如果图片包含 GPS 或 GeoTIFF 空间信息，可以额外启用空间能力展示，但图片本身仍是 `media` 类型。

GIF、WebP、TIFF 等多帧或多页图片仍表达为 `kind=image`。动图播放、帧数、页数、首帧缩略图等属于媒体信息或内容读取能力，不应改写为 `kind=video`。

大图、GeoTIFF、多页 TIFF 等不应依赖全量 base64 作为首屏预览。Manager 应优先使用 raw / range URL、缩略图、降采样或切片能力；后端是否生成缩略图由 `MediaThumbnailReader` 或后续媒体读取能力声明。

### 格式约束

- 不得给图片写入 `type_info.table.fields`。
- 不得把所有图片都视为空间数据。
- 不得把 EXIF 私有字段直接铺到 attributes 顶层。
- 不得把 GeoTIFF 表达为新的基础数据类型。
- 不得为了前端展示把大图全量内容写入 attributes。

## 视频

### 识别与组织

| 维度 | 取值 |
|---|---|
| `organization` | `single` |
| `data_type` | `media` |
| `format` | `mp4`、`mov`、`mkv`、`avi`、`webm`、`video` |
| 主资源 | `meta_item.full_name` 指向视频文件 |

`format` 表达视频文件或容器格式。H.264、H.265、AV1、VP9、AAC、Opus 等编码不作为基础 `format`，应进入 `type_info.media` 或 `format_info.<format>`。

`video` 是兜底格式，只在无法稳定识别具体视频容器时使用。第一阶段视频格式目标是稳定识别、记录轻量元信息，并支持 raw / range / stream 播放链路，不要求后端转码。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.media` | `kind=video`、宽高、时长、视频编码、音频编码、帧率、码率、轨道数等媒体信息 |
| `format_info.<format>` | 容器版本、轨道摘要、metadata atom、字幕轨、封面帧等格式私有信息 |
| `capabilities.extraction` | 仅在已有明确抽帧、OCR、语音转写或字幕提取任务状态时写入 |

### 消费要求

视频预览面向 `data_type=media + type_info.media.kind=video`。Manager 应优先使用 range / stream URL 播放，不应通过后端全量 base64 返回视频内容。转码、抽帧、封面图、字幕提取和语音转写属于后续媒体处理能力，不是格式识别的前置条件。

Search 或语义索引可消费 `capabilities.extraction` 或外部索引引用，但不应把抽帧结果、完整字幕或语音转写全文直接塞入 `attributes`。

### 格式约束

- 不得把视频编码当作基础 `format`。
- 不得把视频写入 `type_info.document` 或 `type_info.table`。
- 不得因为视频包含音轨就拆成多个基础 data item。
- 不得为了预览全量读取大视频文件。

## 音频

### 识别与组织

| 维度 | 取值 |
|---|---|
| `organization` | `single` |
| `data_type` | `media` |
| `format` | `mp3`、`wav`、`flac`、`aac`、`ogg`、`audio` |
| 主资源 | `meta_item.full_name` 指向音频文件 |

`audio` 是兜底格式，只在无法稳定识别具体音频格式时使用。第一阶段音频格式目标是稳定识别、记录轻量元信息，并支持 raw / range / stream 播放链路。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.media` | `kind=audio`、时长、编码、采样率、声道数、码率等媒体信息 |
| `format_info.<format>` | ID3 / Vorbis comment / RIFF chunk / 封面图等格式私有信息 |
| `capabilities.extraction` | 仅在已有明确语音转写、音乐识别或摘要任务状态时写入 |

### 消费要求

音频预览面向 `data_type=media + type_info.media.kind=audio`。Manager 应优先使用 range / stream URL 播放；语音转写、摘要、声纹、音乐识别等属于后续提取或语义能力，不是音频格式识别的前置条件。

### 格式约束

- 不得把音频写入 `type_info.document`。
- 不得把歌词、转写全文或封面大图直接塞入 attributes。
- 不得为了预览全量读取大音频文件。

## PDF

### 识别与组织

| 维度 | 取值 |
|---|---|
| `organization` | `single` |
| `data_type` | `document` |
| `format` | `pdf` |
| 主资源 | `meta_item.full_name` 指向 PDF 文件 |

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `organization`、`data_type`、`format` |
| `type_info.document` | 页数、标题、作者、语言、加密状态、摘要等文档元信息 |
| `format_info.pdf` | PDF 版本、producer、字体、页面结构等格式私有信息 |
| `capabilities.extraction` | 文本提取状态、OCR 状态、文本片段、摘要、外部索引引用 |

### 消费要求

Manager 文档内容读取消费 `type_info.document` 和 `capabilities.extraction`。全文索引或大文本内容应通过外部索引引用或提取任务管理，不直接塞入 attributes。

### 格式约束

- 不得给 PDF 写入 `type_info.table.fields`。
- 不得把大文本全文直接写入 attributes。
- 不得把 PDF 文档提取状态写入 `format_info.pdf`。
