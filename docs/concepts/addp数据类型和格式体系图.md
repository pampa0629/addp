# ADDP 数据类型和格式体系图

本文从概念层说明 ADDP 的数据类型、文件格式、格式插件和横切能力。数据项、资源链条和模块职责边界见 [ADDP 数据项体系图](addp数据项体系图.md)。

## 核心结论

数据类型比文件格式更高层：

- `data type` 回答“这个 data item 在用户观感和平台处理上是什么”。
- `format` 回答“这个 data item 或资源使用什么格式编码”。

二者是多对多语义中的常见一对多落点：

- 一个数据类型可以由多种格式承载，例如 `table` 可以来自 CSV、TSV、Parquet、Shapefile、数据库表、JSON FeatureCollection。
- 一个格式也可能根据内容结构落到不同数据类型，例如 JSON 可以是 table、document 或 container。
- 数据库表、文档集合、graph 等引擎原生 item 可以没有文件格式，但仍必须有数据类型。

## 数据类型

数据类型是对一类 data item 的高层抽象。它们通常具备相似的数据特征、内容读取方式、处理手段和治理方式。

`common/datatype` 是 ADDP data type 与 type info 的代码事实源。各模块不得在 `common/format`、`common/engine`、Meta、Manager 或 Transfer 中重新定义平行的 data type / type info 公共模型。

第一版基础数据类型如下：

| 数据类型 | 含义 | 典型处理方式 |
|---|---|---|
| `table` | 有字段、行列或可推断字段的结构化数据 | 字段信息、表格样本、批量读写、统计分析 |
| `document` | 以阅读、解析和全文提取为主的数据 | 文档信息、文本片段、全文索引、摘要 |
| `media` | 图片、视频、音频等可感知媒体内容 | 媒体信息、原始内容、缩略图、播放、转码 |
| `container` | 内部包含子对象或子资源的数据 | 内部对象枚举、默认入口、子对象读取 |
| `graph` | 节点、边、关系结构的数据 | 图结构信息、关系查询、子图样本 |
| `unknown` | 暂未识别或暂不接入的数据 | 基础存储信息、原始内容或下载 |

### table

`table` 是所有表格型 data item 的通用数据类型。

典型来源：

- 数据库表。
- CSV / TSV。
- 严格记录集合型 JSON，例如顶层对象数组。
- JSON FeatureCollection。
- Shapefile。
- Parquet / ORC / Avro。
- Iceberg 等目录型表格式。
- MongoDB collection 等动态 schema 的 JSON/BSON 文档集合。

CSV 和 JSON 虽然有文本属性，但只要平台把它们作为行列数据处理，就应归为 `table`。文本属性属于文件格式或读取方式，不应把 CSV 放进 `document`。

MongoDB collection 的 `meta_item.item_type` 仍是 `collection`，表示引擎原生 catalog 叶子术语；在 ADDP 当前能力中，它作为动态 schema 记录集合消费，`attributes.item.data_type` 固定为 `table`。`type_info.table.fields` 表示采样推断出的字段画像，不是关系型数据库强 schema；采样规模、动态 schema 类型、平均文档大小等画像事实进入 `capabilities.statistics`，索引摘要进入 `capabilities.indexing`。collection 不写 `type_info.document`，也不新增独立 `document_collection` data type。

JSON 默认按 `document` 兜底；只有内容事实能严格证明它是记录集合时才升级为 `table`。当前明确支持两类 JSON table：顶层对象数组和 GeoJSON `FeatureCollection.features`。`{"data":[...]}`、`{"rows":[...]}`、NDJSON 等结构是否作为 table，需要先补规范再实现，不能用字段名或习惯做隐式猜测。

JSON / GeoJSON 也不默认具备空间能力。只有实际记录里发现 GeoJSON geometry 结构，或字段值可被严格解析为 WKB / EWKB 几何时，才写入 `capabilities.spatial`。后端只表达 `table + spatial` 这样的横切能力组合，不新增“空间表”数据类型；Manager 前端可以据此选择“表格 + 空间”的渲染方式。

### document

`document` 是以阅读、正文提取和片段检索为主的 data item。

典型来源：

- PDF。
- Word / WPS / RTF / Markdown / 纯文本。
- 配置文件或嵌套 JSON 文档。
- 文档型数据库记录。

`document` 只说明用户如何理解和消费该 item，不等于后端已经能完整解析正文。WPS 可以表达为 `data_type=document + format=wps`，即使当前后端只提供 raw content 或 range content。

### media

`media` 是以视觉、音频或视频消费为主的 data item。

典型来源：

- 图片：JPEG / PNG / GIF / TIFF / WebP / BMP / SVG / AVIF / HEIC / GeoTIFF。
- 视频：MP4 / MOV / MKV / AVI / WebM 等容器格式。
- 音频：MP3 / WAV / FLAC / AAC / OGG 等音频格式。

图片、视频、音频之间的差异可通过 `type_info.media.kind=image|video|audio` 或类似字段表达，不新增基础数据类型。

媒体文件的容器格式、编码格式和横切能力应分层表达：`format` 优先表达文件或容器格式，`type_info.media` 只承载跨图片、音频、视频稳定通用且已有消费方的字段。视频编码、音频编码、帧率、采样率、码率、轨道数等细粒度事实暂不作为 `MediaInfo` 主事实；需要保留时进入受控 `format_info.<format>` 或 `capabilities.extraction`。GeoTIFF、带 GPS 的图片等空间语义进入 `capabilities.spatial`，不新增“空间图片”或“视频数据”数据类型。

### container

`container` 表示 item 内部包含子对象或子资源。

典型来源：

- Excel。
- SQLite。
- GeoPackage。
- ZIP / RAR / TAR。

容器是数据类型，不是内容布局。大多数容器文件外层仍是 `layout=single`，内部对象默认写入 `type_info.container.children`。

### graph

`graph` 表示节点、边和关系结构数据。

典型来源：

- Neo4j graph item。
- RDF。
- GraphML / GEXF。
- 图结构 JSON。

图结构既可以来自引擎原生查询，也可以来自文件格式。引擎原生图数据通常不经过文件格式解码。

graph 的核心是节点和关系。Neo4j label、relationship type、RDF class 或采样推断出的结构簇，都是图结构的分组、分类或投影视角，不是比 graph 更底层的独立 data type。平台可以在 Manager 或 Graph 模块中按这些视角展示和筛选，但 graph item 本身仍表示一个可被查询、采样、预览和治理的图整体。

通用 graph 类型信息只描述结构摘要，例如节点形状、关系形状、连接模式、属性结构和计数。实际节点样本、路径探索结果、图算法结果和前端图组件数据属于读取、查询或 Graph 模块能力，不进入 graph 的通用类型信息。

### unknown

`unknown` 用于暂未识别或暂不接入的数据。

`unknown` 不是失败状态，而是平台对资源保持可管理、可检索、可下载的兜底语义。后续探测能力增强后，可以重新扫描并升级为更具体的数据类型。

`file` 不是基础数据类型。文件、对象、目录、bucket、prefix、root 等只表示 catalog / storage 形态；路径、名称、大小、MIME、etag、hash、last_modified 等事实属于 storage 或 catalog 标准字段。无法判断内容语义时，data item 使用 `data_type=unknown`，不得新增 `data_type=file` 或 `type_info.file`。

## 文件格式

文件格式回答 item 或资源的编码方式，例如：

- `csv`、`tsv`
- `json`
- `parquet`、`orc`、`avro`
- `shapefile`
- `sqlite`、`geopackage`
- `excel`
- `zip`、`rar`
- `pdf`、`wps`
- `jpeg`、`png`、`tiff`

文件格式不等于数据类型，也不等于内容布局：

- Shapefile = `data_type=table` + `layout=multi` + `format=shapefile` + `spatial`。
- GeoJSON = `data_type=table` + `layout=single` + `format=json`，当 feature 实际包含 geometry 时再附加 `spatial`。
- GeoTIFF = `data_type=media` + `layout=single` + `format=tiff` + `spatial`。
- Excel = `data_type=container` + `layout=single` + `format=excel`。
- Iceberg = `data_type=table` + `layout=whole` + `format=iceberg`。

## 类型信息与格式信息

`xxx info` 是对应数据类型的通用元数据，代码结构定义在 `common/datatype`。每个 data type 只有一类通用 type info，Meta 写入 `attributes.type_info.<data_type>`：

| 数据类型 | 类型信息示例 |
|---|---|
| `table` / `datatype.TableInfo` | 字段列表、字段类型、主键、行数、大小、表级 native 事实 |
| `document` / `datatype.DocumentInfo` | 标题、语言、编码、页数、字数、大小 |
| `media` / `datatype.MediaInfo` | kind、MIME、宽高、时长、编码、颜色空间 |
| `container` / `datatype.ContainerInfo` | child 数量、默认 child、child 轻量摘要、child refs |
| `graph` / `datatype.GraphInfo` | node shapes、relationship shapes、连接模式、属性结构、节点数、关系数 |

这些 type info 是结构事实，不是内容数据，也不是格式私有信息。文档正文、表格样本、图片缩略图、原始二进制、视频流、图节点样本等必须通过 content reader、sample reader、query provider 或业务模块结果表达，不写入 `type_info`。

每个 data type 只有一类通用 info。格式实现只负责在已确定的 `data_type + format` 下提取这类 info；Meta 负责把它写入 `meta_item.attributes.type_info`。

格式信息是某个具体文件格式才有的描述：

| 文件格式 | 格式信息示例 |
|---|---|
| `csv` | delimiter、encoding、has_header、quote_char |
| `shapefile` | base_name、ref_extensions、has_prj、shape_type、dbf_version |
| `json` | structure、feature_count、properties、geometry_types、bbox、crs |
| `sqlite` | sqlite_version、table_count、tables |
| `zip` | compression_method、entry_count、encrypted |

类型信息不等于内容数据。`table info` 描述字段、行数、主键等元数据；表格样本、文档原文片段、图片缩略图、原始二进制内容等属于内容读取能力。

空间、时间、统计、提取、语义、分区、索引等是横切事实，不新增为基础 data type，也不塞进某个 type info。典型落点是 `attributes.capabilities.*` 或 `attributes.access_index.*`。

`AccessIndex` 虽然当前代码结构暂居 `common/datatype`，但它不是 data type、本体类型信息或格式私有信息。它描述内容读取访问索引，例如表格稀疏行索引；规范落点始终是 `attributes.access_index.<data_type>`。

## FormatPlugin

`FormatPlugin` 是一个文件格式在 `common/format` 中的主入口。它承载格式身份、能力声明和具体实现。

FormatPlugin 可以声明或提供：

- 格式身份：稳定格式 ID、名称、默认数据类型。
- 格式探测：扩展名、MIME、magic bytes、内容签名。
- 布局能力：single / multi / whole、primary ref、related refs 规则、manifest 规则。
- info provider：数据类型信息和格式信息。
- content reader：样本、文本片段、缩略图、原始内容、范围内容。
- 横切能力事实：spatial、temporal、statistics、extraction 等候选事实。
- transfer 相关能力：批量读写、related refs 写入、提交边界。

FormatPlugin 不负责：

- 构造 engine reader。
- 接收 `engine_id` 后反向访问存储。
- 最终决定 data item 边界。
- 直接写 `meta_item.attributes`。
- 返回 Manager 或 Frontend 专用展示协议。

## Format Identity 与 Format Detection

`format identity` 定义“平台支持的这个格式是谁”。它是静态注册事实，通常由 `FormatDescriptor` 或 `FormatPlugin` 表达。

`format detection` 是“给定一个 content，判断它像哪个格式”的动态过程。它输入文件名、MIME、magic bytes、内容签名或 ref 上下文，输出指向某个 format identity 的识别结果。

`format normalization` 是消费已有 format-like 字符串时的归一化过程。上层模块必须通过 `common/format.NormalizeFormat` 把 attributes、扩展名、MIME 或文件名转换为 canonical format；识别不到就是 `unknown`，不能把裸后缀或未知字符串当作 format 写入系统语义字段。

| 维度 | Format Identity | Format Detection |
|---|---|---|
| 回答的问题 | 平台支持哪些格式以及这些格式能做什么 | 当前 content 看起来是什么格式 |
| 性质 | 静态注册事实 | 动态识别过程 |
| 输入 | plugin / descriptor 注册信息 | 文件名、MIME、magic bytes、内容片段、ref 上下文 |
| 输出 | format descriptor / capability | detection result，指向某个 format |
| 是否决定 item | 不决定 | 不最终决定，只给 Meta detector 提供格式候选 |

Shapefile 这类 multi 格式尤其要区分：单个 `.shp/.dbf/.shx` 的识别不等于 data item 归并；最终 item 边界由 Meta detector 根据 format layout 和候选 content 上下文决定。

未知扩展名文本文件不需要预先注册一个具体 format。`DetectFormat(filename, peek)` 在扩展名、MIME、内容签名、sniffer 和 magic bytes 都失败后，可以根据内容前缀的 UTF-8 文本特征返回 `format=text`；没有内容证据时保持 `unknown`。剩余 unknown 非文本内容由 `BinaryContentReader` 提供 raw binary 兜底，不引入 `binary` data type 或 `binary` format。

## Provider 与 Reader 矩阵

`provider` / `reader` 的命名跟随消费意图：info provider 提供元信息，sample / text reader 提供轻量内容，reader provider 打开连续读取会话，writer provider 打开连续写出会话。它们不应混用。

| 数据类型 / 内容布局 | info provider | sample / text reader | continuous reader / writer |
|---|---|---|---|
| `table` + `single` | `TableInfoProvider` | `TableSampleReader` | `TableReaderProvider`、`TableWriterProvider` |
| `table` + `multi` | `MultiTableInfoProvider` | `MultiTableSampleReader` | `MultiTableReaderProvider`、`MultiTableWriterProvider` |
| `table` + `whole` | `ScopeTableInfoProvider` | `ScopeTableSampleReader` | 后续按需补 `ScopeTableReaderProvider` / `ScopeTableWriterProvider` |
| `document` | `DocumentInfoProvider` | `DocumentTextReader` | raw / range content 由 `contentio` 或后续 reader 表达 |
| `unknown` | 无 | `BinaryContentReader` | 仅用于 unknown 非文本内容的 raw binary 兜底，不引入 binary data type |
| `media` | `MediaInfoProvider` | 后续 `MediaThumbnailReader` | raw / range content 由 `contentio` 或后续 reader 表达 |
| `container` | `ContainerInfoProvider` | `ContainerChildResolver`、内部对象读取 | child 解析后继续进入对应 data type provider |
| `graph` | `GraphMetadataProvider` / `datatype.GraphInfo` | `GraphSampleProvider` | 图查询读取由 graph / engine 能力表达 |
| 横切能力 | spatial 等横切事实进入 `capabilities.*` | 不替代 data type reader | 不替代 data type reader / writer |

新实现应按 info、sample、continuous reader、writer 拆开设计，不新增同时表达多种消费意图的组合 provider。

## 横切能力

横切能力是不属于单一数据类型、也不属于单一文件格式，但会影响平台处理能力的附加语义。

| 横切能力 | 含义 |
|---|---|
| `spatial` | 空间字段、CRS、extent、geometry type、空间索引 |
| `temporal` | 时间字段、时间范围、时间粒度 |
| `statistics` | 采样统计、空值率、min/max、质量画像 |
| `extraction` | OCR、文本提取、摘要、提取状态 |
| `semantic` | embedding、语义索引、向量表示 |
| `partitioning` | 分区字段、分区范围、分区样例 |
| `indexing` | 空间索引、全文索引、向量索引等能力描述 |

`spatial` 是典型横切能力：

- PostGIS 表 = `data_type=table` + `spatial`。
- Shapefile = `data_type=table` + `format=shapefile` + `spatial`。
- GeoTIFF = `data_type=media` + `format=tiff` + `spatial`。

空间能力不应新增为 data type，也不应塞进某个格式私有字段。

## attributes 分层

基于上述概念，data item attributes 应表达：

```json
{
  "storage": {},
  "item": {
    "layout": "single|multi|whole",
    "data_type": "table|document|media|container|graph|unknown",
    "format": "..."
  },
  "type_info": {},
  "format_info": {},
  "access_index": {},
  "capabilities": {}
}
```

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `storage` | 这个 item 在引擎侧的存储和访问属性是什么 | bucket、path、physical_path、size、etag、content_type |
| `item` | 这个 data item 的核心语义是什么 | layout、data_type、format、refs、scope_exclusive |
| `type_info` | 对应数据类型的通用元数据是什么 | table fields、media width/height、document page_count、container children |
| `format_info` | 对应文件、容器或格式解析层面的私有信息是什么 | CSV encoding、Shapefile refs、SQLite version |
| `access_index` | 面向内容读取的通用访问索引是什么 | table sparse_row_index |
| `capabilities` | 这个 item 有哪些横切能力 | spatial、temporal、statistics、extraction |

`meta_item` 表字段仍是 item 身份和归属的事实源。attributes 不重复保存 `id`、`tenant_id`、`engine_id`、`node_id`、`name`、`full_name`、`fingerprint` 等表字段。

`attributes.type_info.file` 不存在。文件 / 对象 / 目录的基础事实应写入 `storage`，内容语义写入 `item.data_type` 与对应 `type_info.<data_type>`。

## 后续阅读

- [ADDP 术语表](addp术语表.md)
- [ADDP 数据项体系图](addp数据项体系图.md)
- [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 内置数据类型与文件格式规范](../spec/addp内置数据类型与文件格式规范.md)
- [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)
