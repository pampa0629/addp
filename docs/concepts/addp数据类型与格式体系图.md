# ADDP 数据类型与格式体系图

本文定义 ADDP 对 data item、数据类型、组织方式、文件格式和横切能力的概念模型。配套规范放在 `docs/spec` 下，后续实现应以概念文档和规范文档为准修订。

## 核心结论

ADDP 的数据管理主轴是：

```text
engine -> node -> data item
```

- `engine`：数据来自哪个引擎。
- `node`：引擎内的目录、schema、bucket、prefix 等资源树节点。
- `data item`：平台真正管理、预览、检索、授权、传输的核心数据对象，对应 `meta_item`。

`data item` 是 meta 体系的核心概念，也是 `meta_item` 表的关键语义。`node` 只是资源树组织结构，不能替代 item 边界。

一个 data item 内部可以包含子 item，例如 SQLite 的内部 table、Excel 的 sheet、GeoPackage 的 layer、压缩包中的文件。当前概念上这些子 item 不直接展开为 `meta_item`，而应先在 attributes 中表达；只有当需要独立授权、检索、血缘、传输或生命周期管理时，才讨论是否升格为独立 meta item。

一句话概括：

**engine 管连接，node 管资源树，data item 管平台语义；组织方式管资源如何成为 item，数据类型管用户如何理解 item，文件格式管编码，横切能力管跨类型附加能力。**

## 文档分工

本文件只定义概念边界，不定义扫描流程、落库结构或 provider 接口。数据类型与格式主题按以下事实源维护：

| 问题 | 唯一事实源 |
|---|---|
| engine、node、data item、organization、data type、format、capability 的概念边界 | 本文 |
| 哪些资源组成一个 data item、主资源是什么、claims / exclusive 如何合并 | [ADDP 数据项 detector 规范](../spec/addp数据项detector规范.md) |
| `meta_item.attributes` 如何分区、哪些字段写入哪里 | [ADDP 元数据 attributes 规范](../spec/addp元数据attributes规范.md) |
| format capability、format provider、data type provider 的职责和接口边界 | [ADDP 文件格式能力与 Data Type Provider 规范](../spec/addp文件格式能力与DataTypeProvider规范.md) |
| ResourceRef、ResourceReader、ComponentReader、NativeCursor 的读取边界 | [ADDP 资源读取抽象规范](../spec/addp资源读取抽象规范.md) |
| 数据类型与格式相关的跨模块职责边界 | [ADDP 数据类型与格式模块边界规范](../spec/addp数据类型与格式模块边界规范.md) |
| 首批内置格式的确定性落地规则 | [ADDP 内置数据格式规范](../spec/addp内置数据格式规范.md) |
| 新增或修改格式时的操作步骤和验证清单 | [ADDP 数据格式扩展指南](../spec/addp数据格式扩展指南.md) |

其他文档引用上述主题时，只保留摘要和链接，不重复定义规则。

## 总览

```mermaid
graph LR
    Engine[Engine] --> Node[Node]
    Node --> Item[Data Item / Meta Item]

    Item --> Organization[组织方式]
    Item --> DataType[数据类型]
    Item --> Format[文件格式]
    Item --> TypeInfo[类型信息]
    Item --> FormatInfo[格式信息]
    Item --> Capabilities[横切能力]
    Item --> Children[内部子 item]

    Organization --> O1[single]
    Organization --> O2[multi]
    Organization --> O3[whole]

    DataType --> T1[table]
    DataType --> T2[document]
    DataType --> T3[media]
    DataType --> T4[container]
    DataType --> T5[graph]
    DataType --> T6[unknown]

    Capabilities --> C1[spatial]
    Capabilities --> C2[temporal]
    Capabilities --> C3[profile/statistics]
    Capabilities --> C4[extraction]
    Capabilities --> C5[semantic]
    Capabilities --> C6[partitioning]
    Capabilities --> C7[indexing]
```

## engine / node / data item

### engine

`engine` 表示数据接入和访问能力，例如 PostgreSQL、MySQL、MongoDB、Neo4j、MinIO、S3、NFS、本地文件系统。

engine 只回答：

- 如何连接。
- 如何枚举资源。
- 如何读取内容。
- 能提供哪些存储侧元信息。

engine 不直接决定 data item 的数据类型，也不直接决定一个目录、prefix 或文件组是否构成 item。

### node

`node` 表示 engine 内的资源树节点，例如：

- 数据库 schema。
- bucket。
- prefix。
- 目录。
- catalog 分组节点。

node 的职责是组织资源树，服务浏览、扫描和定位。node 不是 data item，除非某个 detector 或引擎原生边界明确说明“这个范围整体构成一个 data item”。

### data item

`data item` 是 ADDP 的核心数据对象，对应 `meta_item`。平台围绕 data item 做：

- 元数据扫描。
- 目录展示。
- 预览。
- 检索。
- 授权。
- 传输。
- 血缘和资产治理。

data item 的身份由 `meta_item` 表字段承载，例如 `id`、`tenant_id`、`engine_id`、`node_id`、`item_type`、`name`、`full_name`、`fingerprint`。这些字段不应重复写入 attributes。

## 组织方式

组织方式回答：**引擎中的文件、目录、表、prefix 等资源如何组织成一个 data item。**

建议使用“组织方式”替代“组合形态”。它比“组合形态”更贴近 engine 资源到 data item 的归并关系，也更容易跨文件、对象存储和数据库引擎复用。

| 组织方式 | 含义 | 示例 |
|---|---|---|
| `single` | 一对一，一个引擎资源就是一个 data item | 数据库 table、CSV 文件、SQLite 文件、PDF、图片 |
| `multi` | 多对一，多个资源共同组成一个 data item | Shapefile 多文件、一组表共同构成一个业务 item |
| `whole` | 全部对一，整个目录、prefix、schema 或扫描范围构成一个 data item | Iceberg 表目录、OSGB 场景目录、影像镶嵌数据集目录 |

### single

`single` 不等于“单文件”。它表示一个引擎资源和一个 data item 一一对应。

示例：

- PostgreSQL table。
- CSV 文件。
- PDF 文件。
- SQLite 文件。
- ZIP 文件。

SQLite、ZIP、RAR 等虽然内部包含子 item 或子资源，但它们作为外层 data item 仍是 `single`。容器不是组织方式，而是数据类型。

### multi

`multi` 表示多个资源共同组成一个有意义的 data item。

示例：

- Shapefile 的 `.shp/.shx/.dbf/.prj`。
- 未来一组物理表共同构成一个业务逻辑 item。
- 主文件 + 同级索引文件 + 同级元数据文件。

`multi` 只认领明确匹配的组件资源，不独占整个目录或 prefix。

### whole

`whole` 表示整个范围构成一个 data item，不再逐个顾忌范围内的文件和子目录。

示例：

- Iceberg 表目录。
- OSGB 场景目录。
- 明确声明为完整数据集的 prefix。

Parquet 本身不需要被称为“湖表”。一个 Parquet 文件可以是 `single + table + format=parquet`；一组同类 Parquet 文件可以是 `multi + table + format=parquet`；未来 Iceberg 支持后，可以是 `whole + table + format=iceberg`。数据类型仍然是 `table`。

## 数据类型

数据类型回答：**这个 data item 在用户观感和处理方式上是什么。**

数据类型是对一类类似 data item 的抽象。它们通常具备相似的数据特征、预览方式、处理手段和治理方式。

建议第一版数据类型：

| 数据类型 | 含义 | 典型处理方式 |
|---|---|---|
| `table` | 有字段、行列或可推断字段的结构化数据 | 字段预览、表格查询、导入导出、统计分析 |
| `document` | 以阅读、解析和全文提取为主的数据 | 文档预览、文本提取、全文索引、摘要 |
| `media` | 图片、视频、音频等可感知媒体内容 | 缩略图、播放、转码、媒体元信息 |
| `container` | 内部包含子 item 或资源的数据 | 内部对象枚举、解包、选择子对象 |
| `graph` | 节点、边、关系结构的数据 | 图谱预览、关系查询、图算法 |
| `unknown` | 暂未识别 | 基础文件预览或下载 |

## 数据类型与格式目录

本节只给出概念层的格式归类和 ADDP 当前支持状态。具体落地规则见 [ADDP 内置数据格式规范](../spec/addp内置数据格式规范.md)，provider 实现矩阵见 [ADDP 文件格式能力与 Data Type Provider 规范](../spec/addp文件格式能力与DataTypeProvider规范.md)。

支持状态说明：

- 已支持：当前 ADDP 已有识别、扫描或提取链路，可进入标准 meta item / attributes。
- 部分支持：已有枚举、识别、解析或提取能力的一部分，但尚未形成完整 provider / preview / transfer 链路。
- 规划：概念上属于该数据类型，但当前不作为稳定能力声明。

| 数据类型 | 典型格式 / 来源 | 当前 ADDP 支持状态 |
|---|---|---|
| `table` | 数据库表、CSV、TSV、records JSON、JSON Lines、FeatureCollection、Shapefile、Parquet、ORC、Avro、Iceberg | 已支持数据库表、CSV、TSV、records / 空间 JSON、Shapefile、单 Parquet；ORC / Avro 有 single 规则声明但 provider 能力待补；Iceberg 属于 whole 规划 |
| `document` | PDF、TXT、Markdown、DOCX、PPTX、WPS、任意对象 JSON、文档型数据库记录 | 已支持 PDF 元数据 / 提取状态和文档型 JSON 识别；TXT / DOCX / PPTX / WPS 有格式枚举或识别但稳定提取能力待补 |
| `media` | JPEG、PNG、GIF、TIFF / GeoTIFF、视频、音频 | 已支持 JPEG / PNG / GIF / TIFF 图片识别和图片元数据提取，GeoTIFF 可写入可确定空间信息；视频 / 音频有格式枚举但稳定元数据 provider 待补 |
| `container` | Excel、SQLite、GeoPackage、ZIP、RAR、TAR | 已支持 Excel、SQLite、GeoPackage 外层容器和 children 枚举；压缩包类容器为规划能力 |
| `graph` | Neo4j label / relationship、RDF、GraphML、GEXF、图结构 JSON | 已支持引擎原生图 schema 写入 `type_info.graph`；文件型图格式为规划能力 |
| `unknown` | 无法识别或暂未接入格式 | 已支持作为兜底类型 |

注意：

- `format` 是编码或格式族，不等于 `data_type`。例如 GeoJSON 类结构仍表达为 `format=json` + `capabilities.spatial`。
- 数据库表、文档集合、图 label / relationship 等引擎原生 item 可以没有文件格式；它们仍按 `data_type` 进入平台语义。
- 一个格式是否“可识别”、是否“有 FormatCapability 声明”、是否“实现了 provider”是三层不同能力，不能混用。

### table

`table` 是所有表格型 data item 的通用数据类型。

包括：

- 数据库表。
- CSV / TSV。
- records 型 JSON。
- 带空间结构的 JSON FeatureCollection。
- Shapefile。
- Parquet / ORC / Avro。
- Iceberg 等表格式目录。

CSV 和 JSON 虽然文本属性强，但只要平台把它们作为行列数据处理，就应归为 `table`。文本属性属于文件格式或读取方式，不应把 CSV 放进 `document`。

JSON 需要结构识别：

- records array、JSON Lines、带空间结构的 JSON FeatureCollection 可归为 `table`。
- 任意 JSON 对象、配置文件、嵌套文档可归为 `document` 或 `container`，取决于平台消费方式。

### document

`document` 是以阅读、正文提取和片段检索为主的 data item。

典型来源：

- PDF。
- Word / RTF / Markdown / 纯文本。
- 配置文件或嵌套 JSON 文档。
- 文档型数据库记录。

`document` 只说明用户如何理解和消费该 item，不等于后端已经能完整解析正文。文档型格式的能力可以分阶段存在：

- 只识别格式和基础元信息。
- 提供文本片段或全文提取。
- 提供预览材料，例如 Markdown / 纯文本 / HTML / raw binary / URL。
- 由前端专用 renderer 基于 raw binary 或 URL 展示。

例如 WPS 可以表达为 `data_type=document + format=wps`，即使当前后端只提供 raw binary 预览材料、由前端 WPS renderer 展示；不能因为预览材料是二进制，就把 WPS 降级为 `unknown` 或改成 `format=binary`。

### media

`media` 是以视觉、音频或视频预览为主的 data item。

典型来源：

- 图片。
- 视频。
- 音频。

差异可通过 `type_info.media.kind=image|video|audio` 或类似字段表达。

### container

`container` 表示 item 内部包含子 item 或资源。

示例：

- ZIP / RAR / TAR。
- SQLite。
- GeoPackage。
- Excel。

容器类型不说明它如何被 engine 组织成 item。大多数容器文件外层组织方式是 `single`。

## 类型信息

类型信息是某个数据类型天然应该具备的通用元数据。

| 数据类型 | 类型信息示例 |
|---|---|
| `table` | 字段列表、字段类型、主键、索引、行数、采样信息 |
| `document` | 标题、作者、页数、正文摘要、语言 |
| `media` | kind、宽高、时长、编码、采样率、颜色模式 |
| `container` | 内部子 item 列表、默认入口、子资源数量 |
| `graph` | label、relationship、属性结构、节点数、边数 |

这里不再使用 `schema` 作为概念层分区名。这个词容易和数据库 schema、JSON 结构规范、表结构描述混淆。对于 ADDP 概念层，应明确说：

- 表格型数据有 `table info`。
- 媒体型数据有 `media info`。
- 文档型数据有 `document info`。
- 容器型数据有 `container info`。

## 文件格式与格式信息

文件格式回答 item 的编码方式，例如：

- `csv`、`tsv`
- `json`
- `parquet`、`orc`、`avro`
- `shapefile`
- `sqlite`、`geopackage`
- `zip`、`rar`
- `pdf`
- `jpeg`、`png`、`tiff`

格式信息是某个具体格式才有的描述。

示例：

| 格式 | 格式信息示例 |
|---|---|
| `csv` | delimiter、encoding、has_header、quote_char |
| `shapefile` | base_name、component_extensions、has_prj、shape_type、dbf_version |
| `json` | json_type、record_count、has_bbox、crs |
| `sqlite` | sqlite_version、table_count、tables |
| `zip` | compression_method、entry_count、encrypted |

格式信息不应污染数据类型信息。例如 Shapefile 的 `shape_type` 是格式信息；几何字段列表、字段类型属于 table info；哪个字段是空间字段和 SRID/extent 属于 spatial。

## 横切能力

横切能力是不属于单一数据类型、也不属于单一文件格式，但会影响平台处理能力的附加语义。

### spatial

`spatial` 是典型横切能力。它既不属于数据类型，也不属于文件格式。

具体而言：

1. table 上的空间字段，本身应该在 table info 的 field info 中。
2. 哪个字段是空间字段，属于 spatial。
3. SRID、extent、geometry type、空间维度、空间索引等属于 spatial。
4. Shapefile、JSON、GeoPackage 等具体格式仍应保留自己的 format info，和通用 spatial 独立。

示例：

- Shapefile = `data_type=table` + `organization=multi` + `format=shapefile` + `spatial`。
- 带空间结构的 JSON = `data_type=table` + `organization=single` + `format=json` + `spatial`。
- PostGIS 表 = `data_type=table` + `organization=single` + `spatial`。
- GeoTIFF = `data_type=media` + `format=tiff` + `spatial`。

### 其他横切能力

| 横切能力 | 含义 |
|---|---|
| `temporal` | 时间字段、时间范围、时间粒度 |
| `profile` / `statistics` | 采样统计、空值率、min/max、质量画像 |
| `extraction` | OCR、文本提取、摘要、提取状态 |
| `semantic` | embedding、语义索引、向量表示 |
| `partitioning` | 分区字段、分区范围、分区样例 |
| `indexing` | 空间索引、全文索引、向量索引等能力描述 |

这些能力不应变成顶层数据类型，也不应被塞进具体格式信息。它们应该作为横切能力独立表达。

## 去掉 ObjectInfo

概念层不再保留 `ObjectInfo`。

原因是 `ObjectInfo` 容易被误解为“对象存储中的对象信息”，但实际上对象大小、etag、content-type、last_modified 等属于 storage 层信息。

例如 MinIO 中的 CSV 文件同时具备：

- storage info：bucket、path、size、etag、content_type、last_modified。
- table info：字段、行数、表头、采样类型。

因此：

- `TableInfo` 属于 `data_type=table` 的类型信息。
- `MediaInfo` 属于 `data_type=media` 的类型信息。
- `DocumentInfo` 属于 `data_type=document` 的类型信息。
- `ContainerInfo` 属于 `data_type=container` 的类型信息。
- 原 `ObjectInfo` 应拆散并融入 storage info，不作为数据类型模型。

## capability 与 provider

平台需要区分三层能力：

| 层次 | 含义 | 示例 |
|---|---|---|
| engine capability | 引擎插件声明自己能提供什么访问能力 | catalog、content read、range read、batch read、batch write、graph query |
| format capability | 格式插件声明自己能识别、解析、提取或写出什么 | identification、layout、extract、sample、batch read/write、component commit |
| item capabilities | `meta_item.attributes.capabilities` 中的扫描事实 | spatial、temporal、statistics、extraction、partitioning、indexing |

`format capability` 与 `engine capability` 使用同一术语风格，都是插件对平台的能力声明。`item capabilities` 是扫描后的 item 事实结果，不能反向冒充插件能力声明。

上层消费者应尽量面向 data type provider，而不是直接面向具体 engine type 或 format type：

- `TableProvider`：表结构、样本、分页、批量读写、空间列等表语义。
- `DocumentProvider`：文档元数据、文本片段、页数、语言等文档语义。
- `MediaProvider`：媒体元数据、缩略图素材、EXIF 等媒体语义。
- `ContainerProvider`：内部对象枚举、默认入口、子对象摘要等容器语义。
- `GraphProvider`：节点、边、schema、图样本等图语义。
- `SpatialProvider`：作为横切 provider，补充 geometry、SRID、extent 等空间语义。

新增 format 时，如果能落到既有 data type，上层消费者原则上不应感知新格式；新增 data type 时，上层消费者必须显式增加对应 provider、展示和转换能力。

## attributes 概念分层

基于上述概念，data item attributes 应表达：

```json
{
  "storage": {},
  "item": {
    "organization": "single|multi|whole",
    "data_type": "table|document|media|container|graph|unknown",
    "format": "..."
  },
  "type_info": {},
  "format_info": {},
  "capabilities": {}
}
```

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `storage` | 这个 item 在引擎侧的存储和访问属性是什么 | bucket、path、physical_path、size、etag、content_type |
| `item` | 这个 data item 的核心语义是什么 | organization、data_type、format、component_files、scope_exclusive |
| `type_info` | 对应数据类型的通用元数据是什么 | table fields、media width/height、document page_count、container children |
| `format_info` | 对应文件格式的私有信息是什么 | CSV delimiter、Shapefile components、SQLite version |
| `capabilities` | 这个 item 有哪些横切能力 | spatial、temporal、statistics、extraction |

`meta_item` 表字段仍是 item 身份和归属的事实源。attributes 不重复保存 `id`、`tenant_id`、`engine_id`、`node_id`、`name`、`full_name`、`fingerprint` 等表字段。

本文定义目标结构。当前实现中的旧字段名和旧结构不保留兼容语义，应通过重新 meta 扫描和代码修正尽早暴露并解决。

## 设计原则

- ADDP 的核心层次是 `engine -> node -> data item`。
- data item 是平台真正治理、预览、检索和传输的对象。
- 组织方式表达资源如何成为 item，不表达数据是什么。
- 数据类型表达用户如何理解 item，不表达资源如何组织。
- 文件格式表达编码方式，不表达数据类型和组织方式。
- 横切能力表达跨数据类型、跨格式的附加能力。
- 容器是数据类型，不是组织方式。
- “湖表”不是基础概念；Parquet、Iceberg 等都是 `table` 类型在不同组织方式和格式下的实现。
- `ObjectInfo` 不作为概念层模型，存储侧对象信息归入 storage info。
- 一个事实只有一个规范来源和一个规范存储点。
- 上层模块应通过 data type provider 消费能力，避免直接依赖具体 engine type 或 format type。

## 后续阅读

本文只定义概念边界。实现层职责边界见 [ADDP 数据类型与格式模块边界规范](../spec/addp数据类型与格式模块边界规范.md)；新增格式的最小流程见 [ADDP 数据格式扩展指南](../spec/addp数据格式扩展指南.md)；首批格式的落地规则见 [ADDP 内置数据格式规范](../spec/addp内置数据格式规范.md)。
