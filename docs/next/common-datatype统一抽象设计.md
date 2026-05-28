# Common Datatype 统一抽象设计

更新时间：2026-05-28

本文保留为本轮 `common/datatype` 统一抽象重构的迁移记录，不再作为正式规范事实源。正式口径以以下文档为准：

- [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)
- [ADDP 元数据体系图](../concepts/addp元数据体系图.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 元数据 attributes 规范](../spec/addp元数据attributes规范.md)
- [ADDP 引擎插件接口规范](../spec/addp引擎插件接口规范.md)

本文记录重构前后的判断、迁移顺序和历史上下文；若与正式 concepts / spec 文档冲突，以正式文档为准。

## 接力状态

截至 2026-05-28，本轮已完成 table、graph、document、media、container 主事实收口：

- table：`format.TableInfo` / `format.FieldInfo` 薄壳已删除，reader / writer / Transfer / engine tabular catalog 统一使用 `datatype.TableInfo` / `datatype.FieldInfo`；`plugin.ItemMetadata.Table *datatype.TableInfo` 是 table item 主事实，`Fields` / `Stats` / `Attributes` 不再作为 table 事实源。
- graph：`datatype.GraphInfo` 已按 node shape、relationship shape、relationship pattern 定义；`plugin.ItemMetadata.Graph *datatype.GraphInfo` 是 graph item 主事实；Neo4j catalog / Meta 扫描 / Manager 预览和属性页 / Graph 模块 / Service 图查询服务已迁移到 graph item + `type_info.graph` 口径。
- graph 业务图视图已明确过滤引擎内部结构：Neo4j Spatial 的 `SpatialLayer` 节点和 `RTREE_METADATA`、`RTREE_REFERENCE`、`RTREE_ROOT` 关系不进入 GraphInfo、计数、样本、Graph Browser、Schema 推导、知识服务或 GDS 投影。
- graph shape facts 已补稳定化：label set 去空、去重、排序，空 shape name 可由 label set 派生；单 label node shape 使用 `kind=label`，多 label node shape 使用 `kind=label_set`；graph sample provider 过滤条件使用 `plugin.GraphSampleFilter` 强类型传递。
- document：`plugin.ItemMetadata.Document *datatype.DocumentInfo` 已落地，作为 document item 主事实字段；`ItemMetadataDocumentInfo()` 是公共消费 helper。Meta single resource deep scan、refresh 和对象按需元数据入口已统一通过 `metaenrich.EnrichResourceAttributes` 消费 `DocumentInfoProvider` 并写入 `type_info.document`。DOCX / PPTX 已有轻量 `DocumentInfoProvider`，DOCX 正文抽取已覆盖正文、页眉、页脚、脚注、尾注和批注文本。
- media：`plugin.ItemMetadata.Media *datatype.MediaInfo` 已落地，作为 media item 主事实字段；`ItemMetadataMediaInfo()` 是公共消费 helper。Meta single resource deep scan、refresh 和对象按需元数据入口已统一通过 `metaenrich.EnrichResourceAttributes` 消费 `MediaInfoProvider` 并写入 `type_info.media`；对象存储扫描中的 inline media extractor 旁路已删除。GeoTIFF 等空间事实继续通过同一次 `MediaDescribeResult` 写入 `capabilities.spatial`，不塞进 `MediaInfo`。
- container：`plugin.ItemMetadata.Container *datatype.ContainerInfo` 已落地，作为 container item 主事实字段；`ItemMetadataContainerInfo()` 是公共消费 helper。Meta container summary、deep children enrich、对象按需元数据入口和已知 item refresh 均通过 `DetectedItem.Container` / `metaenrich.EnrichResourceAttributes` 写入 `type_info.container`，ZIP、Excel、SQLite、GeoPackage 等 child 仍只保存轻量摘要，不展开内容样本或完整字段。
- file：`DataTypeFile` / `FileInfo` 已删除。文件、对象、目录等是 catalog / storage 形态；未识别内容统一使用 `unknown`。
- Manager / common client：对象预览按需元数据触发已改为判断标准 attributes 是否已有 `type_info.*` / `format_info` 主事实，提取结果只合并回 `ObjectPreview.attributes`；`common/client.MetaClient.GetObjectMetadata` 返回标准 attributes；`ObjectPreview.extracted_metadata`、共享前端 `ExtractedMetadata` 旧展示组件、旧 payload 展示入口和未使用的旧 `TryExtractMetadata` helper 已删除。

下一阶段不再推进新的 data type 主事实字段；后续只有出现独立于 storage / format / capabilities 的通用事实和真实消费方时，才重新讨论新增 data type 或横切能力。

## 核心目标

`common/datatype` 要收拢 ADDP 所有 data type 的基础语义，而不限于 table 或字段模型。

它回答：

- ADDP 支持哪些基础 data type。
- 每类 data type 的通用 type info 是什么。
- 字段类型、空间信息、访问索引等横切结构的公共定义在哪里。
- engine、format、Meta、Manager、Transfer 应围绕哪一个事实源理解数据类型。

它不回答：

- 某个资源如何识别成 data item。
- 某个文件是什么 format。
- 某个 engine 如何连接、枚举、读取或写入。
- Meta attributes 如何落库。
- Manager 前端如何展示。

## 背景问题

当前 ADDP 中 data type 基础语义分散在多个包中：

| 位置 | 当前职责 | 问题 |
|---|---|---|
| `common/dataitem` | 定义 item 组织规则、layout、候选归并 | 当前仍依赖 `common/format` 获取格式能力和 layout 常量；长期应拆分 core 与 format 规则适配 |
| `common/format/registry` | 定义 descriptor 的 layout、provider、reader 常量 | registry 不应成为跨 engine / format 的 item layout 事实源 |
| `common/format` | 曾定义 `FieldType`、`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`SpatialInfo`、`AccessIndex` | data type / type info 不应属于 format；`SpatialInfo` 和 `AccessIndex` 是横切结构，不是 format 私有语义，也不是 data type 本体 |
| `common/engine/plugin` | 曾定义 `FieldInfo`、`ColumnInfo`、`TableInfo`、`SchemaInfo`、`CollectionInfo`、图相关 info 和 provider 接口 | engine 侧形成与 format 平行的字段和元数据模型；其中公共 `FieldInfo`、`ColumnInfo`、`TableInfo` 已收敛到 `datatype`，`SchemaInfo` 已更名为 `NamespaceInfo` |
| `common/models` | 定义 Meta API / client DTO，例如 `FieldInfo`、`SpatialMetadata` | API DTO 又定义了一套字段语义 |
| Meta 内部 | 将 engine / format 结果转换为 `meta_item.attributes` | 历史上数据库扫描链路存在 `ColumnInfo` / `FieldInfo` / `format.FieldInfo` 来回转换；当前已先收敛公共 `ColumnInfo` 和 `format.FieldInfo` |
| Manager / Transfer | 消费 Meta attributes、engine `FieldInfo` 或 format `FieldInfo` | 消费方不得不理解多套数据类型和字段模型 |

典型绕路如下：

```text
数据库插件 ListColumns()
  -> datatype.FieldInfo
  -> common/engine/plugin.DescribeTabularItem()
  -> Meta / Manager 直接消费 datatype.FieldInfo
  -> type_info.table.fields attributes
```

这不是单纯字段结构不够丰富的问题，而是 data type 事实源没有统一的问题。

## 核心判断

ADDP 应新增 `common/datatype` 作为更底层的共享抽象：

```text
common/datatype
   ↑          ↑          ↑
common/format common/engine common/dataitem
   ↑          ↑          ↑
格式插件      引擎插件      Meta item 归并
```

`common/datatype` 只定义平台通用的数据类型语义，不定义格式识别、不定义 engine provider、不访问存储、不决定 Meta item 归并。

## 职责边界

| 包 | 应负责 | 不应负责 |
|---|---|---|
| `common/datatype` | data type identity、各类 type info、field type、field info、空间等横切结构、轻量 helper | format detection、engine 连接、provider registry、Meta 落库、Manager DTO、Transfer 计划 |
| `common/format` | format identity、format detection、FormatPlugin、format capability、info provider、content reader、格式私有解析和 native type mapper | 定义通用 data type / field type 事实源 |
| `common/engine/plugin` | 引擎身份、capabilities、catalog provider、query/runtime provider、metadata provider、连接池和引擎原生 metadata 获取 | 定义独立于 datatype 的字段模型或 type info 模型 |
| `common/dataitem` | 候选 content 到 data item 的组织规则、layout、refs、scope 归并辅助 | 作为 data type 常量事实源 |
| Meta | 扫描调度、item 裁决、attributes normalizer、落库、索引 | 自己发明 data type、field type 或重复定义 type info 语义 |

## 目标模型

### DataType

`DataType` 表达 data item 在用户观感和平台处理上的高层类型。

第一版目标取值：

| 值 | 含义 |
|---|---|
| `unknown` | 未识别、暂不接入或扫描事实不足 |
| `table` | 行列、记录集合或可推断字段的结构化数据 |
| `document` | 以阅读、正文提取、全文索引为主的数据 |
| `media` | 图片、视频、音频等可感知媒体 |
| `container` | 内部包含子对象或子资源的数据 |
| `graph` | 节点、边、关系结构数据 |

`file` 不作为基础 data type。文件、对象、目录等是 catalog / storage 形态；当平台尚不能稳定判断内容语义时，data type 统一为 `unknown`。

空间、时间、统计、提取、语义、分区、索引等不新增为基础 data type，而是横切事实。

### TypeInfo

每个 data type 对应一类通用 type info。type info 是该 data type 的结构事实，不是内容数据，也不是 format 私有信息。

| DataType | 通用 info | 典型内容 |
|---|---|---|
| `table` | `TableInfo` | 字段、主键、行数、大小 |
| `document` | `DocumentInfo` | 标题、语言、编码、页数、字数、大小 |
| `media` | `MediaInfo` | media kind、MIME、宽高、时长、编码、颜色空间 |
| `container` | `ContainerInfo` | child 数量、默认 child、child 摘要、child refs |
| `graph` | `GraphInfo` | 节点结构、关系结构、连接模式、属性结构、节点数、关系数 |

目标代码形态使用轻量 `TypeInfo` 接口标记这些结构的 data type 归属，同时保持各 info 为独立结构，不做大 union：

```go
type DataType string

type TypeInfo interface {
    TypeInfoDataType() DataType
}

type TableInfo struct { ... }
type DocumentInfo struct { ... }
type MediaInfo struct { ... }
type ContainerInfo struct { ... }
type GraphInfo struct { ... }
```

`TypeInfo` 只回答“该结构写入哪个 `type_info.<data_type>` 分区”，不承载空间、访问索引、格式私有信息或运行时业务逻辑。

Meta attributes 仍负责将这些结构写入：

```text
type_info.table
type_info.document
type_info.media
type_info.container
type_info.graph
```

### FieldType

`FieldType` 是 ADDP 通用字段类型，属于 `datatype`，不属于 `format`。

第一版候选值沿用当前 `format.FieldType`，但事实源迁到 `common/datatype`：

| 类型族 | 候选值 |
|---|---|
| 基础 | `unknown`、`string`、`bool`、`bytes`、`mixed` |
| 数值 | `int`、`bigint`、`float`、`double`、`decimal` |
| 时间 | `date`、`time`、`timestamp` |
| 半结构化 | `json`、`array` |
| 标识 | `uuid` |
| 空间 | `geometry`、`point`、`linestring`、`polygon`、`multipoint` |

`FieldType` 表达字段语义，不固定行值编码。WKT / WKB / EWKB / GeoJSON 属于读取选项或内容编码事实。

### TableInfo

`TableInfo` 是 table data type 的通用信息，不属于数据库私有模型，也不属于文件格式私有模型。

`TableInfo` 只表达 `attributes.type_info.table` 对应的 table 类型事实。空间能力和访问定位索引虽然常在 table 解析过程中同时得到，但它们不是 table 类型事实本体，不放入 `TableInfo`。

第一版目标字段：

```go
type TableInfo struct {
    Name       string
    Kind       string
    Comment    string
    RowCount   *int64
    SizeBytes  *int64
    CreatedAt  *time.Time
    UpdatedAt  *time.Time

    Fields     []FieldInfo
    PrimaryKey []string
    Native     map[string]interface{}
}
```

索引、唯一约束、外键、分区、排序键等先不塞进 `TableInfo` 顶层。它们需要先确认跨引擎语义和上层消费方式，再作为表级 metadata 单独设计。

`Native` 表达当前 table item 的来源原生表级事实。它用于统一承载 format 和 engine 的表级差异信息，便于上层以一致的 `TableInfo` 视角消费文件类表和数据库类表。

`Native` 的来源由 item 上下文决定：数据库表看 `engine_type`，文件表看 `format`。因此 `Native` 不再使用二级来源结构，不写成 `native.clickhouse.engine` 或 `native.shapefile.shape_type`。各来源能写入哪些 key 必须由对应 engine / format 的规范或白名单定义，不能作为无限制透传容器。

`common/datatype` 只提供通用过滤 helper，用于按白名单清理 `Native`；它不登记具体 engine / format 的 key。具体来源必须在自己的包或规范中声明允许的 table native key，并在写入 `TableInfo.Native` 前过滤。

进入 `Native` 的事实必须满足：

- 是当前 table item 的表级来源原生事实。
- 不能用现有通用字段表达。
- 字段名和语义在对应 engine / format 内有清晰定义。
- 后续能被存储、调试、展示、复现解析或专门消费。

不得进入 `Native` 的事实：

- 已标准化字段，例如 `row_count`、`size_bytes`、`created_at`、`updated_at`、`comment`、`kind`、`fields`、`primary_key`。
- 空间事实，应进入 `SpatialInfo` / `capabilities.spatial`。
- 访问定位索引，应进入 `AccessIndex` / `access_index`。
- storage、path、etag、last_modified 等资源定位或存储事实。
- 容器、文件集合、资源整体事实，例如 ZIP entry 数、SQLite page size、Shapefile sidecar 列表、Parquet scope 文件清单。

典型候选：

| 来源 | 可进入 `Native` 的表级事实 |
|---|---|
| ClickHouse | `engine` |
| MySQL | `engine`；`table_collation`、`create_options` 暂为候选，待来源字段和消费链路确认 |
| Doris | `engine` 等 Doris 表级原生事实暂为候选，待来源字段确认后进入白名单 |
| PostgreSQL | `table_type`、`relkind` |
| CSV / TSV | `delimiter`、`quote_char`、`escape_char`、`has_header` |
| Shapefile | `shape_type`、`dbf_version`、`encoding` |
| Excel sheet | `sheet_name`、`sheet_index` |
| Parquet table | `partition_columns`，但文件清单等 scope 事实不进入 |
| SQLite / GeoPackage | 暂不新增 table native key；表 / 视图分类进入 `Kind`，page size、page count、对象数量等仍属文件或容器事实 |

### 描述结果包

Provider 一次解析可能同时得到 type info、横切事实、访问索引和格式私有事实。描述结果包是 provider / 编排层的返回组合，不是 data type 本体。

进一步结论：`TableDescribeResult` / `MediaDescribeResult` 不属于 `common/datatype`。它们不是平台 data type 事实结构，而是 `common/format` provider 一次解析后返回的事实组合包。

当前原则：

- `common/datatype` 的核心职责是统一 format 和 engine 共同需要表达的 ADDP 通用数据语义结构。
- 描述结果包不应因为“同一次解析顺手得到多类事实”而污染 `TableInfo`、`MediaInfo` 等 type info，也不应进入 `common/datatype`。
- `FormatInfo` 是 format 私有事实，应由 `common/format.FormatInfoProvider` 提供，不进入 `datatype` 结构。
- `SpatialInfo` 是横切空间事实，不是 table 类型事实本体；它可以由 format describe result 同级返回，再由 Meta 写入 `capabilities.spatial`。
- `AccessIndex` 是访问定位索引事实，不是 table 类型事实本体；它可以由 format describe result 同级返回，再由 Meta 写入 `access_index.table`。
- 当前不为拆分 describe result 引入缓存、session 或 resource handle 等复杂机制；保持 format provider 返回组合事实，Meta 负责拆写 attributes。

描述结果中各类事实的映射关系必须清晰：

| 字段 | 规范落点 |
|---|---|
| `Table` | `attributes.type_info.table` |
| `Spatial` | `attributes.capabilities.spatial` |
| `AccessIndex` | `attributes.access_index.table` |
| format 私有事实 | `attributes.format_info.<format>`，由 `FormatInfoProvider` 独立提供 |

`FormatInfo` 的语义是“当前格式的裸格式私有事实”，不是 attributes 中已经命名空间化的 `format_info` 结构。Provider 自身和调用编排层已经知道当前格式，因此 `FormatInfoProvider` 不得再套一层格式名。例如 Shapefile format info 应返回：

```go
map[string]interface{}{
    "base_name": "roads",
    "ref_extensions": []string{".shp", ".dbf", ".shx"},
    "shape_type": "Polygon",
}
```

由 Meta normalizer 或等价编排层负责写入：

```json
{
  "format_info": {
    "shapefile": {
      "base_name": "roads",
      "ref_extensions": [".shp", ".dbf", ".shx"],
      "shape_type": "Polygon"
    }
  }
}
```

如果极少数编排场景确实需要一次聚合多个格式的私有事实，应在上层聚合结构中表达，不污染单个 data type describe result。

目标接口方向：

```go
DescribeTable(...) (*datatype.TableInfo, error)
DescribeMedia(...) (*datatype.MediaInfo, error)
```

横切事实通过独立通道表达：

| 事实 | 目标表达方式 |
|---|---|
| table type info | `datatype.TableInfo` |
| media type info | `datatype.MediaInfo` |
| spatial facts | 独立 `datatype.SpatialInfo`；由专门 provider、读取/写入参数或上层编排携带 |
| access index | 独立 `datatype.AccessIndex`；由访问索引能力或上层编排生成 |
| format private info | `FormatInfoProvider.DescribeFormat` 返回裸 `map[string]interface{}` |

### `format.TableInfo` 删除结论

`format.TableInfo` 薄壳已删除。`common/format` reader / writer / Transfer 操作边界直接使用 `datatype.TableInfo`，避免 format 和 datatype 维护两套 table info 表达。

删除前已经拆掉以下耦合：

- `FormatInfo` 已改为通过 `format.TableDescribeResult.FormatInfo` 或 `FormatInfoProvider` 获取，不再由 table 执行上下文承载。
- `SpatialInfo` 已改为独立参数或上层编排携带；format reader 使用 `TableSpatialInfoProvider`，format writer 使用 `WriteOptions.SpatialInfo`，Transfer / engine 写入链路使用 `BatchData.Spatial`、`TableWriteOptions.SpatialInfo`、`TableWriteSessionOptions.SpatialInfo`。
- `AccessIndex` 已改为独立访问索引事实，不由 table info 承载。

`SpatialInfo` 的拆分需要单独设计，不能像 `FormatInfo` / `AccessIndex` 一样直接从 `TableInfo` 删除。原因是它当前同时承担两类职责：

- 元数据事实：由 provider describe result 返回，再写入 `attributes.capabilities.spatial`。
- 执行期空间参数：连续 reader / writer / Transfer pipeline 用它确定几何字段、几何类型、SRID、维度，并传给 engine table write prepare / session / batch write。

`format.TableInfo.SpatialInfo` 已删除，替代通道如下：

| 场景 | 当前通道 | 后续候选 |
|---|---|---|
| format provider 解析空间事实 | `format.TableDescribeResult.Spatial` | 保持不变 |
| Manager / Meta 展示空间能力 | attributes `capabilities.spatial` | 保持不变 |
| format writer 写出 GeoJSON / Shapefile | `WriteOptions.SpatialInfo` | 已落地 |
| Transfer 批次传递空间事实 | pipeline 单独保存 `SpatialInfo`，再写入 `BatchData.Spatial` | 已落地 |
| engine 写表准备和会话 | `BatchData.Spatial` / `TableWriteOptions.SpatialInfo` | 保持 engine 侧现有独立空间参数 |
| `TableReader` 返回读取字段和空间事实 | `Fields()` + 可选 `TableSpatialInfoProvider.SpatialInfo()` | 已不再用 `Schema() *TableInfo` 暴露完整 table info |

当前已完成 reader / writer / Transfer pipeline 侧收敛：`TableReader` 只暴露实际读取 rows 对应的 `Fields()`，空间读取上下文通过可选 `TableSpatialInfoProvider` 提供，空间写出上下文通过 `WriteOptions.SpatialInfo` 提供，Transfer plan / pipeline 单独携带 `SpatialInfo`。

### Meta attributes 存储口径

Meta attributes 是 item 事实的直接 JSON 投影，不是兼容旧实现的包装层。

- `type_info.table` 直接对应 `datatype.TableInfo` 的 JSON 形态。
- `type_info.table.fields[]` 直接对应 `datatype.FieldInfo` 的 JSON 形态。
- 字段可空性只写 `nullable`，字段主键标记只写 `primary_key`。
- 不再写入或运行期兼容 `is_nullable`、`is_primary_key`、`table_type`、`table_comment` 等旧字段。
- 历史 Meta 数据不做迁移脚本；需要时删除旧 Meta 数据并重新 scan。

### TableInfo.Native 与 format_info 的边界

`TableInfo.Native` 只承载表级来源原生事实；`format_info.<format>` 仍承载文件、容器、资源整体或格式解析层面的私有事实。二者不是同义词。

典型拆分：

| 来源 | `TableInfo.Native` | `format_info.<format>` |
|---|---|---|
| CSV / TSV | `delimiter`、`quote_char`、`escape_char`、`has_header` | 文件编码探测、行尾风格、采样策略摘要 |
| Shapefile | `shape_type`、`dbf_version`、`encoding` | `base_name`、`ref_extensions`、`has_prj`、`has_cpg` |
| Excel | `sheet_name`、`sheet_index` | `sheet_count`、`default_sheet`、workbook 级摘要 |
| Parquet | `partition_columns` | scope 文件清单、row group 摘要、压缩和文件级 schema 版本 |
| SQLite / GeoPackage | 暂无已确认 key；`sqlite_master.type` 统一为 `Kind` | page size、page count、table/view/index 数量 |
| ZIP | 不适用，ZIP 本身不是 table item | entry count、file count、directory count、children truncated |

数据库 engine 的表级原生事实同样进入 `TableInfo.Native`，但数据库、namespace、连接、catalog 层事实不进入 table native。

### format_info 与 capabilities 边界

核心原则：

> `format_info` 回答“这个文件、对象或容器的具体格式实现事实是什么”；`capabilities` 回答“这个 data item 可被平台按什么跨格式能力消费”。

表级来源原生事实优先进入 `TableInfo.Native`。下面的 `format_info.<format>` 例子只表示文件、容器、资源整体或尚未明确归属到表级的格式事实；新增表级私有事实时应优先放入 `TableInfo.Native`，不要再借 `format_info` 承载。

进入 `format_info.<format>` 的事实满足以下任一条件：

- 只对某个具体格式有意义。
- 用于复现、调试或解析这个格式。
- 换成其他格式后，字段名或语义不稳定。
- 描述文件封装、编码、容器、头信息、sidecar/ref 结构、格式版本或格式内部组织。

典型示例：

| 格式 | `format_info.<format>` 示例 |
|---|---|
| CSV / TSV | 文件编码探测、行尾风格、采样策略摘要 |
| Shapefile | `base_name`、`ref_extensions`、`has_prj`、`has_cpg` |
| Excel | `sheet_count`、`default_sheet`、workbook 级摘要 |
| Parquet | row group、压缩、schema 版本、scope 文件清单 |
| SQLite / GeoPackage | SQLite 版本、page size、page count、内部表 / 视图 / 索引摘要 |
| PDF | PDF 版本、author、subject、creator、producer、加密状态、读取限制、格式头或对象结构摘要 |

进入 `capabilities.<capability>` 的事实满足以下任一条件：

- 跨格式、跨引擎有统一消费方式。
- Manager、Transfer、Meta、Service 等上层会按该能力执行通用行为。
- 它描述的不是“格式怎么存”，而是“这个 item 能被当成什么能力处理”。
- 同一能力未来可以来自文件格式、数据库、对象存储或外部服务。

典型示例：

| 能力 | `capabilities.<capability>` 示例 |
|---|---|
| spatial | `geometry_columns`、`primary_geometry_column`、`srid`、`crs`、`extent`、`dimension`、`has_spatial_index` |
| temporal | 时间字段、时间范围、粒度、时区 |
| statistics | 字段分布、null 率、min/max、采样规模、画像时间 |
| indexing | 空间索引、全文索引、向量索引摘要 |
| extraction | 文本提取状态、可用 extractor、提取索引引用 |

容易混淆的字段按以下规则处理：

| 字段 / 事实 | 处理规则 |
|---|---|
| `shape_type` | Shapefile header 原生 shape type 进入 `type_info.table.native.shape_type`；平台统一几何类型进入 `capabilities.spatial.geometry_columns[].geometry_type`。 |
| `has_prj` / `has_cpg` | sidecar 文件存在性进入 `format_info.shapefile`；解析出的 CRS / SRID 进入 `capabilities.spatial`。 |
| `bbox` | GeoJSON 原文显式声明的 bbox 可作为格式事实保留在 `format_info.json.bbox`；插件扫描计算出的 bbox 只进入 `capabilities.spatial.extent`。通用消费只依赖后者。 |
| `feature_count` | 不属于 spatial。表格行数进入 `type_info.table.row_count`；画像或统计任务产生的要素统计进入 `capabilities.statistics`。 |
| `geometry_types` | 插件推导出的几何类型进入 `capabilities.spatial.geometry_columns[].geometry_type`，不在 `format_info.json` 中重复表达。 |
| `has_geometry` | 不作为长期通用判断依据；是否具备空间能力由 `capabilities.spatial` 是否存在表达。 |

一句话规则：

- 格式怎么编码、封装、组织：`format_info`
- 数据能被平台如何通用消费：`capabilities`
- 数据类型本体事实：`type_info`
- 内容定位索引：`access_index`

后续如果 document、media、container、graph 也存在同类“一次解析产出多个事实”的情况，可以继续定义对应 describe result，或者抽出更通用的 `DescribeResult`。但不应为省事把横切事实塞进各自 `TypeInfo`。

### FieldInfo

`FieldInfo` 是 table / graph property / document dynamic schema 等场景可复用的字段或属性模型。它以字段语义为核心，不表达完整数据库列 DDL。

第一版目标字段：

```go
type FieldInfo struct {
    Name                 string
    Type                 FieldType
    NativeType           string
    Nullable             bool
    PrimaryKey           bool
    Comment              string
    Size                 int
    Precision            int
    Scale                int
    OrdinalPosition      int
    DefaultExpression    string
    Generated            bool
    GenerationExpression string
}
```

字段属性进入 common 的原则：

| 层级 | 示例 | 处理原则 |
|---|---|---|
| 通用结构语义 | name、type、native_type、nullable、primary_key、comment、ordinal_position、default_expression、generated、generation_expression | 可进入 `datatype.FieldInfo` |
| 半通用布局 / 优化语义 | partition key、sorting / clustering key、index participation、unique constraint | 先定义消费方和跨引擎语义，再决定是否进入 table 级 info |
| 引擎私有语义 | ClickHouse codec / ttl / low_cardinality，PostgreSQL storage / collation，MySQL charset / collation | 没有明确上层消费方前不进入 common，也不预留自由 attributes 入口 |

### DocumentInfo

`DocumentInfo` 是 document data type 的通用信息。它只描述文档结构元信息，不承载正文内容、提取能力或提取状态。

第一版目标字段：

```go
type DocumentInfo struct {
    Title        string
    Language     string
    Encoding     string
    PageCount    int
    WordCount    int
    SizeBytes    *int64
}
```

文档正文、片段、摘要、OCR 结果、embedding 和提取状态不写入 `DocumentInfo`；它们分别属于 content reader、extraction 结果或语义索引。

### MediaInfo

`MediaInfo` 是 media data type 的通用信息。

第一版目标字段：

```go
type MediaKind string

const (
    MediaKindImage MediaKind = "image"
    MediaKindVideo MediaKind = "video"
    MediaKindAudio MediaKind = "audio"
)

type MediaInfo struct {
    Kind       MediaKind
    MIMEType   string
    Width      int
    Height     int
    DurationMS *int64
    Encoding   string
    ColorSpace string
    SizeBytes  *int64
}
```

GeoTIFF、带 GPS 的图片等空间语义进入 `SpatialInfo`，不新增“空间媒体” data type。

### ContainerInfo

`ContainerInfo` 是 container data type 的通用信息。容器 child 只是内部可寻址对象摘要，不自动等于独立 data item。

第一版目标字段：

```go
type ContainerInfo struct {
    ChildCount    int
    DefaultChild  string
    ResourceCount int
    Children      []ContainerChildInfo
}

type ContainerChildInfo struct {
    Name        string
    ChildKind   string
    DataType    DataType
    Format      string
    RowCount    *int64
    ColumnCount *int
    HasHeader   *bool
    Fields      []FieldInfo
    Refs        []ContainerChildRef
    Native      map[string]interface{}
}

type ContainerChildRef struct {
    Role      string
    Path      string
    Required  bool
    Primary   bool
    Extension string
}
```

`ContainerInfo` 不依赖 `common/format.FormatType`。如果 child 需要表达格式身份，使用字符串字段 `Format`，不让 `datatype` 反向依赖 `format`。

`ContainerChildInfo` 不承载 `layout`。child 只是容器内部可寻址对象摘要，不是已经确认的 data item；zip 内 shapefile 多组件归并等 layout 事实由 dataitem / Manager / Meta 编排层按上下文动态计算，不写入底层 container child info。

父容器级 `Native` 不作为标准落库入口。容器级文件整体事实、解析统计、采样上限和截断状态写入 `format_info.<format>`；child 级 `Native` 只保留 child 定位或受控原生摘要，例如 ZIP entry 定位事实、SQLite child 的原始 table 名等。

### GraphInfo

`GraphInfo` 是 graph data type 的通用信息。它要同时服务文件型图数据和引擎原生图数据。

graph 的核心本体是 node 和 relationship。Neo4j label、RDF class、采样推断出的结构簇等都只是 node shape 的来源或投影视角，不应把 label 作为 `GraphInfo` 的顶层本体。Meta 和 Manager 可以按 label 展示图结构，但 `common/datatype` 应先表达图整体的结构摘要。

第一版目标字段：

```go
type GraphInfo struct {
    Model              string
    Directed           *bool
    NodeCount          *int64
    RelationshipCount  *int64
    NodeShapes         []GraphNodeShapeInfo
    RelationshipShapes []GraphRelationshipShapeInfo
}

type GraphNodeShapeInfo struct {
    Name       string
    Kind       string
    Labels     []string
    Properties []FieldInfo
    Count      *int64
}

type GraphRelationshipShapeInfo struct {
    Type       string
    Properties []FieldInfo
    Patterns   []GraphRelationshipPatternInfo
    Count      *int64
}

type GraphRelationshipPatternInfo struct {
    From  GraphEndpointInfo
    To    GraphEndpointInfo
    Count *int64
}

type GraphEndpointInfo struct {
    ShapeName string
    Labels    []string
}
```

`Model` 第一版使用 `generic`、`property_graph`、`rdf`。`GraphNodeShapeInfo.Kind` 第一版使用 `label`、`label_set`、`class`、`inferred`。

`RelationshipCount` 是标准字段名，不再使用 `edge_count` 作为 `common/datatype` 字段。relationship 的 endpoint 必须用 `Patterns` 保留配对关系，不得只用顶层 `from_labels[]` / `to_labels[]` 两个集合表达。

图查询语言、遍历 API、子图采样、图算法不属于 `datatype`，应留在 engine provider 或 graph 模块能力中。

### FileInfo 删除结论

`FileInfo` 不作为 ADDP data type 主事实结构保留。当前能想到的字段（MIME、编码、大小）要么已有更明确的 storage / document / format 落点，要么缺少独立消费方。为避免 `type_info.file` 与 `storage` 双写同一事实，本轮删除 `DataTypeFile` / `FileInfo`。

后续如果出现明确需求，例如低语义文件的通用安全扫描摘要、可读性状态或通用二进制画像，应先确认这些事实不属于 `storage`、`format_info` 或 `capabilities`，再重新讨论是否新增 data type 或横切能力。

## 横切结构

### SpatialInfo

`SpatialInfo` 描述 table / media / graph 等 data type 上的空间横切事实。

目标字段：

```go
type SpatialInfo struct {
    GeometryColumn  string
    GeometryType    string
    SRID            int
    BoundingBox     *[4]float64
    HasSpatialIndex bool
    IndexName       string
    Dimension       int
}
```

空间不是 data type；它是横切能力事实。

### AccessIndex

`AccessIndex` 描述面向内容读取的通用访问索引，例如大文件稀疏行索引。它不是 data type identity，不是 type info，也不是 format 私有信息。

目标口径：

- 如果 provider 在解析某类 type info 时同时生成访问索引，应通过 describe result 的同级字段返回，再由 Meta 写入 `attributes.access_index`。
- `AccessIndex` 的结构可以放在 `common/datatype`，但不应被理解为“数据类型”本身。
- `AccessIndex` 当前暂不调整。后续是否留在 `common/datatype`，取决于它是否成为 format 和 engine 都需要消费的通用访问索引结构；如果它长期只服务内容读取优化，应考虑移出 `datatype`，但不为此提前新增含糊概念。

## 已识别但暂缓处理的问题

以下问题已经确认存在，但当前阶段不立即改代码；记录在这里，避免后续忘记边界和目标。

| 问题 | 当前状态 | 暂缓原因 | 后续方向 |
|---|---|---|---|
| `format.TableDescribeResult` / `format.MediaDescribeResult` | 已迁出 `common/datatype`，作为 format provider 解析结果包存在 | Provider 一次解析自然可能得到多类事实；保留组合返回可以避免重复读取和过度设计 | 保持在 `common/format` provider 边界内，不进入 `datatype`；如后续确有必要，再按事实拆分 provider |
| `datatype.AccessIndex` | 当前放在 `common/datatype`，被 format、Meta、Manager preview 使用 | 它不是 data type 本体，但当前是跨模块复用结构；贸然移出会引入新包或新概念 | 暂不动。后续结合 engine range reader、format access index、Meta attributes 的消费链路再决定是否移出 |
| `capabilities.spatial` 写入 helper | 已在 Meta 侧新增专门 helper，并完成主要写入链路收敛 | 字段型与非字段型 spatial 的表达不同，不能直接用纯结构投影替代 | 后续只在新增 spatial 写入链路时复用 `metaattr.SpatialInfoAttributes`，避免重新手写 |
| `common/engine/plugin.DatabaseInfo` / `CollectionInfo` | 仍是 engine catalog 层结构 | 它们更接近 catalog hierarchy / namespace 事实，不等同于 data type info | 暂不迁入 `datatype`。后续如有重复，再从 catalog/path/node 语义统一 |
| `MediaInfo` 的音视频细粒度字段 | 当前只定义 `kind/mime_type/width/height/duration_ms/encoding/color_space/size_bytes` | 采样率、声道数、码率、轨道数、视频 codec / 音频 codec 等尚未形成跨格式稳定消费链路 | 暂不加入 `datatype.MediaInfo`；已有提取器事实先留在 `capabilities.extraction` 或后续受控 `format_info.<format>` |
| `format.TableInfo` | 已删除 | 原薄壳只重复 `datatype.TableInfo`，没有独立事实边界 | reader / writer / Transfer 直接使用 `datatype.TableInfo` |
| Manager `Metadata.vue` 历史页面 | 已删除 | 页面未挂路由，且调用的 `managerAPI.scanDataSource/getTables/manageTable/unmanageTable` 已不存在；继续保留会误导为旧 metadata 消费入口 | Manager 元数据展示以 Data Explorer + Meta 标准 attributes 为准；`quick_view` 保留为空间快显 / MVT 预缓存任务表，不承载 table info 事实 |

### `common/engine/plugin.TableInfo` 收敛结论

`plugin.TableInfo` 已不再作为公共结构保留。engine tabular catalog 的 table 列表直接返回 `datatype.TableInfo`，避免 format / engine 继续维护两套 table metadata 事实源。

字段归属结论：

| 当前字段 | 建议归属 | 说明 |
|---|---|---|
| `TableName` | `datatype.TableInfo.Name` | table data type 的名称事实，已统一 |
| `RowCount` | `datatype.TableInfo.RowCount` | table 类型事实，使用 `*int64` 表达未知和 0 的差异 |
| `SizeBytes` | `datatype.TableInfo.SizeBytes` | table 类型事实，使用 `*int64` 表达未知和 0 的差异 |
| `Comment` | `datatype.TableInfo.Comment` | 写入 `type_info.table.comment`，作为通用 table 描述事实 |
| `Kind` | `datatype.TableInfo.Kind` | 平台通用 table 分类，例如 `table`、`view`、`materialized_view`；PostgreSQL `table_type` / `relkind` 等来源原生分类进入 `Native` |
| `Schema` | catalog / storage / path 事实 | 不进入 `datatype.TableInfo` |
| `LastModified` | `datatype.TableInfo.UpdatedAt` | 表源端更新时间事实，用于增量判断和 Meta `data_updated_at` |

同时确认：

- 不新增 `TableCatalogEntry` 等中间公共结构，除非后续出现 `datatype.TableInfo` 无法表达的明确事实。
- namespace 由 `CatalogPath` / `CatalogSegment` / `NamespaceTerm` 表达，不放入 `datatype.TableInfo`。
- `SchemaInfo` 已改名为 `NamespaceInfo`，`ListSchemas` 已改名为 `ListNamespaces`，避免把 schema/database 作为同一个含糊概念继续扩散。
- tabular engine 的 `CatalogProvider.ListChildren` 和 `ItemMetadataProvider.DescribeItem` 必须从同一份 `datatype.TableInfo` 派生表级事实；`Native`、`Comment`、`UpdatedAt`、`SizeBytes` 等不能只在列表入口存在而在详情入口丢失。
- `plugin.ItemMetadata` 已增加 `Table *datatype.TableInfo`。table 型 item 的主事实应进入该字段；`Fields`、`Stats`、`Attributes` 只作为非 table item 通用信息或必要的 catalog 展示属性，不再作为 table 事实源。tabular helper 不再为 table 型 `ItemMetadata` 主动填充 `Fields` / `Stats` 投影，消费方应通过 `ItemMetadataTableInfo()` / `ItemMetadataFields()` 获取 table facts 或字段列表。

## 和现有模型的收拢关系

### 必须迁入或替换

| 现有定义 | 目标 |
|---|---|
| `common/dataitem.DataType` | 已改为 `common/datatype.DataType` 的类型别名，`dataitem` 只消费 |
| `common/format/registry.DataType*` | 已删除；registry descriptor 直接使用 `datatype.DataType` |
| `common/format.FormatDataType*` | 已删除；format 调用方直接使用 `datatype.DataType*` |
| `common/format.FieldType` | 迁到 `common/datatype.FieldType` |
| `common/format.TableInfo` | 已删除，直接使用 `common/datatype.TableInfo` |
| `common/format.FieldInfo` | 迁到 `common/datatype.FieldInfo` |
| `common/format.DocumentInfo` | 迁到 `common/datatype.DocumentInfo` |
| `common/format.MediaInfo` | 迁到 `common/datatype.MediaInfo` |
| `common/format.ContainerInfo` | 已删除，直接使用 `common/datatype.ContainerInfo` |
| `common/format.SpatialInfo` | 迁到 `common/datatype.SpatialInfo` |
| `common/format.AccessIndex*` | 迁到 `common/datatype` 或后续独立 `common/contentindex`，但不能继续属于 format |
| `common/engine/plugin.FieldInfo` | 已删除，改用 `datatype.FieldInfo` |
| `common/engine/plugin.ColumnInfo` | 已删除；provider 对外使用 `datatype.FieldInfo` |
| `common/models.FieldInfo` | 已删除；Meta fields API / common MetaClient 直接返回 `datatype.FieldInfo` |
| Meta 内部重复 `FieldInfo` | 已删除；字段事实从 `attributes.type_info.table.fields[]` 投影为 `datatype.FieldInfo` |

### 不应迁入

| 现有概念 | 原因 |
|---|---|
| FormatPlugin / descriptor / capability | 属于 format identity 和实现能力 |
| Format detection / sniffer | 属于格式识别 |
| Engine provider / catalog / connection | 属于 engine 能力 |
| Manager 前端 DTO | 属于展示边界 |
| Manager preview `ColumnMetadata` | 属于预览响应 DTO，由 `datatype.FieldInfo` 或 spatial metadata 投影而来，不作为字段事实源 |
| Service query 内部 `metadataColumnInfo` / response column DTO | 属于查询执行和响应边界，可从 `datatype.FieldInfo` 投影，不下沉到 `datatype` |
| `common/catalogview.ColumnMetadata` | 属于目录视图模型，只保留面向视图的最小列展示信息 |
| Transfer plan / writer policy | 属于执行规划 |
| Meta attributes schema | 属于落库和查询协议 |

### Layout 的位置

`layout` 回答“资源如何组成 data item”，不是 data type。

它描述的是多个 content 如何组织成一个 data item。这里的 content 可以是文件、object、容器内部子对象，将来也可以是数据库 table、消息 topic、图 label 等。因此 layout 是跨 format / engine 的 item 组织语义，不属于 `common/datatype`，也不应由 `common/format` 或 `common/engine` 独占定义。

需要区分两类事实：

- format / engine 提供 layout 能力声明或候选事实：例如某个 format 支持 `single`、`multi`、`whole`，某个 engine 可把多个 table 组合成一个 item。
- dataitem / Meta item 识别层基于上下文确认最终 item layout，并负责 refs、claims、exclusive、entry path 等 item 边界事实。

当前阶段性处理：

- 对外统一入口放在 `common/format` 顶层：`LayoutSingle`、`LayoutMulti`、`LayoutWhole`，以及 `NormalizeLayout`、`IsKnownLayout`、`NormalizeLayouts`、`ValidateLayouts`、`HasLayout`。
- `common/dataitem.Layout` 复用 `common/format.Layout`，表达已经解析完成的 item layout；format capability 中的 layout 表达某个格式可支持的组织方式。
- `common/dataitem` 当前仍需要询问 format descriptor / capability 来生成规则，因此简单让 `common/format` 反向依赖 `common/dataitem` 会形成循环依赖。
- 为 `single/multi/whole` 三个取值新增独立顶层包过重，除非后续 layout 规则扩展到 engine、topic、graph 等更多 item 组织场景。

长期目标：

- `common/dataitem` core 定义 item layout、candidate、rule、resolver 等纯 item 组织语义。
- `common/dataitem` core 不直接依赖 `common/format` 或 `common/engine`。
- format / engine 只声明自己支持或发现的 layout 能力，不决定最终 item layout。
- Meta / dataitem 编排层基于 format / engine 提供的能力声明和实际 content 上下文，确认最终 data item。

建议迁移路径：

1. 短期保持 `common/format` 顶层作为公开统一入口，不把 layout 放入 `common/datatype`，也暂不新增 `dataitem` 子包或独立 layout 包。
2. format descriptor、format capability 和调用方统一使用 `Layout*` 常量及 helper，减少自由字符串扩散；不再使用 `FormatLayout*` 这类带包名前缀的重复命名。
3. 后续拆分 `common/dataitem/format.go` 中依赖 format 的规则生成逻辑，形成 `dataitem` core + format 规则适配层。
4. 当 engine 侧出现多个 table / topic / graph label 组成 item 的明确需求时，再判断是否需要抽出更薄的 item layout 公共包。

阶段性结论：`layout` 不迁入 `common/datatype`；公开常量和 helper 先统一在 `common/format` 顶层；最终 item layout 由 dataitem / Meta item 识别层确认。

### dataitem、metaitem 与 Manager container preview

`common/dataitem` 是通用 data item 组织识别层。它只负责把一批候选 content 识别为 `ResolvedItem`，并确认 `layout`、`data_type`、`format`、`entry_path`、`refs`、claims 等 item 边界事实。它不负责 Meta 落库、不读取 provider 详情、不生成 Manager 展示 DTO。

`meta/backend/internal/metaitem` 是 Meta 扫描阶段在 `common/dataitem` 之上的业务编排层。它把存储引擎扫描出的文件 / 对象转成 `dataitem.Candidate`，调用 `dataitem.ResolveItems`，再补充 Meta 落库需要的事实，例如 `physical_path`、字段、attributes、multi table 的 format describe 结果等。`metaitem.DetectedItem` 可以内嵌 `dataitem.ResolvedItem`，但它不是新的通用 item 事实源，只是 Meta 扫描计划。

Manager 的 container preview 也复用 `common/dataitem`，但它发生在展示时。container child 本身只是容器内部可寻址对象摘要，`ContainerChildInfo` 不持久化 `layout`。当 Manager 预览 ZIP 等容器内容时，会把 children 转成 `dataitem.Candidate`，以 `ScopeKindContainer` 调用 `dataitem.ResolveItems`，再把识别出的 multi item 临时投影为 preview child。预览结果中出现的 `layout=multi` 来自当次动态归并，不是 `type_info.container.children[]` 的原始存储事实。

因此三者边界如下：

```text
common/dataitem
  -> 通用 item 组织识别，输出 ResolvedItem

meta/internal/metaitem
  -> Meta 扫描编排和 enrichment，输出 DetectedItem / attributes 写入计划

manager/objectcontent
  -> 展示时复用 dataitem 对 container children 临时分组，输出 preview DTO
```

当前约束：

- `layout` 是 item 组织事实，不进入 `common/datatype`。
- `ContainerChildInfo` 不写 `layout`，只保留 child 摘要、格式字符串和 refs 等可复用事实。
- container 内 multi layout 的识别必须使用统一的 `common/dataitem` 规则，避免 Manager、Meta、format 各自硬编码 shapefile / sidecar 归并逻辑。
- 如果未来 engine 侧出现“多个 table / topic / label 组成一个 item”的需求，也应复用或扩展 `common/dataitem` 的组织识别能力，而不是在 engine provider 中新增另一套 layout 事实源。

## Meta attributes 映射

`datatype` 结构不等于 `meta_item.attributes` schema。Meta 仍负责 attributes normalizer 和落库。

目标映射：

```text
datatype.TableInfo
  -> attributes.type_info.table

datatype.DocumentInfo
  -> attributes.type_info.document

datatype.MediaInfo
  -> attributes.type_info.media

datatype.ContainerInfo
  -> attributes.type_info.container

datatype.GraphInfo
  -> attributes.type_info.graph

datatype.SpatialInfo
  -> attributes.capabilities.spatial

datatype.AccessIndex
  -> attributes.access_index.<data_type>
```

转换规则必须集中在 Meta，不允许 Manager / Transfer 自己重复解释字段属性和 type info。通用的 `struct` / `map[string]interface{}` 转换能力放在 `common/jsonmap`，不放入 `common/datatype`；`datatype` 只定义事实结构和必要的语义 helper。

当前转换收敛口径：

- `TableInfo` / `FieldInfo`：由 `common/datatype` 提供专门 helper，内部复用 `common/jsonmap`，并保留 field type 归一化、空字段过滤、row count / size bytes 有效性等 table 语义。
- `AccessIndex`：结构已具备明确 json tag，Meta 写入 `access_index.<data_type>` 时直接复用 `common/jsonmap.MapFromStruct`，不再为每个写入点手写 anchors / source / header bytes 转换。
- `ContainerInfo`：Meta 写入 `type_info.container` 时可以复用 `common/jsonmap` 生成通用字段，但必须保留 container 语义约束：`child_count/resource_count/children` 是明确事实，即使为 0 或空列表也应写入；child 只写轻量摘要，不写 `Fields`；child `Native` 必须过滤 `format/ref_paths/components` 等协议字段。
- `SpatialInfo`：`capabilities.spatial` 已确认支持字段型和非字段型两类表达。字段型空间对象写 `geometry_columns` / `primary_geometry_column`；非字段型空间对象可只写顶层 `srid` / `crs` / `extent`，不得虚构 geometry column。Meta 写入应使用专门 helper，不能在各解析链路重复手写。

`metaattr` 是 Meta attributes 投影层，不是扫描编排层或 engine 适配层。它的函数输入只能是：

- `models.JSONMap` / `map[string]interface{}` 这类 attributes map。
- `common/datatype` 中的通用事实结构，例如 `TableInfo`、`FieldInfo`、`SpatialInfo`。
- 为 attributes 投影定义的轻量输入结构，例如 data item attributes input、document collection attributes input。

`metaattr` 不应接收 `metaitem.DetectedItem`、`plugin.ItemMetadata`、`plugin.IndexInfo`、`models.SpatialMetadata`、Manager DTO 等上层复杂类型。上层模块如果拿到 engine / format / query / 展示模型，应先在本层投影为轻量输入或 `datatype` 事实结构，再调用 `metaattr`。这样可以避免 attributes helper 反向依赖扫描、engine 或展示边界。

## 迁移原则

用户已确认本次重构不以旧实现兼容为目标，干净整洁优先。

因此迁移口径是：

1. 可以分阶段施工，降低单次改动风险。
2. 但每个阶段都必须朝删除旧定义前进。
3. 不为旧模型长期保留 alias、兼容 DTO 或双事实源。
4. 代码与文档冲突时，以审定后的概念和规范为准，直接修正实现。

## 迁移步骤

### 阶段 0：文档审定

1. 审定本文对 `DataType`、`TypeInfo`、`FieldType`、横切结构和 Layout 边界的定义。
2. 同步更新 `docs/concepts/addp数据类型和格式体系图.md`、`docs/spec/addp数据类型与格式能力规范.md`、`docs/spec/addp元数据attributes规范.md`。
3. `file` 已确认不作为长期基础 data type 保留；文件 / 对象形态归 catalog / storage facts。

### 阶段 1：新增 `common/datatype`

1. 新建 `common/datatype`。
2. 定义 `DataType`、`FieldType`、`TableInfo`、`FieldInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`GraphInfo`、`SpatialInfo`、`AccessIndex`。
3. 补类型常量和 helper 测试。

### 阶段 2：format 依赖 datatype

1. `common/format` 删除通用 data type / type info / field type 定义。
2. FormatPlugin、provider、reader 接口直接使用 `datatype.*`。
3. format 只保留 format identity、capability、provider registry、content reader、native type mapper。

当前进展：

- `FormatDescriptor.DataType`、`FormatCapability.DataType`、format discovery view、registry descriptor view、`ContainerChildInfo.DataType`、`RefDescriptor.DataType` 已统一使用 `datatype.DataType`。
- format 对外 JSON 仍输出字符串值；Manager preview hint、API DTO、搜索索引等展示 / 传输边界可继续使用字符串投影。

### 阶段 3：engine 依赖 datatype

1. engine provider 对外 metadata 结构改用 `datatype.*`。
2. 删除 `plugin.FieldInfo`。
3. `ColumnInfo` 不再作为公共模型；SQL 插件直接产出 `datatype.FieldInfo` 或插件内部结构后立即转换。
4. `ItemMetadata`、tabular catalog helper、graph metadata 等统一使用 datatype。

当前进展：

- `plugin.ItemMetadata.Table *datatype.TableInfo` 已落地，table facts 不再由 `Fields` / `Stats` / `Attributes` 拼装。
- `plugin.ItemMetadata.Graph *datatype.GraphInfo` 已落地，graph facts 不再由 label item、relationship item 或扁平 `from_labels` / `to_labels` 拼装。
- `plugin.ItemMetadata.Document *datatype.DocumentInfo` 已落地，document facts 不再需要由 `Attributes` 拼装。
- `plugin.ItemMetadata.Media *datatype.MediaInfo` 已落地，media facts 不再需要由 `Attributes` 拼装。
- `plugin.ItemMetadata.Container *datatype.ContainerInfo` 已落地，container facts 不再需要由 `Attributes` 拼装。
- `ItemMetadataTableInfo()`、`ItemMetadataFields()`、`ItemMetadataDocumentInfo()`、`ItemMetadataMediaInfo()`、`ItemMetadataContainerInfo()`、`ItemMetadataGraphInfo()` 是公共消费 helper，避免消费方直接读 `Attributes`。
- `DocumentMetadataSamplingProvider` 当前仍返回 `*ItemMetadata`。MongoDB collection 动态字段画像继续按既有 table facts 口径保留，是否调整为 document 专用事实需要先统一 collection 的 data type 归属和字段画像消费链路。

### 阶段 4：dataitem 与 Meta 收拢

1. `common/dataitem` 已使用 `datatype.DataType`。
2. Meta normalizer 从 `datatype.*` 生成 `attributes.type_info.*`。
3. 删除 Meta 内部重复字段 DTO 和转换绕路。
4. `common/models` 中重复的 `FieldInfo` 已删除；字段 API 和 `MetaClient.GetItemFields*` 返回 `datatype.FieldInfo`。
5. 字段 API 不混入空间事实；空间列、SRID、几何类型等通过 spatial metadata / `capabilities.spatial` 独立获取。

### 阶段 5：Manager / Transfer / 其他模块清理

1. Manager 只消费标准 attributes 和必要的 `datatype` 派生 DTO，不按 format / engine 重猜字段类型。
2. Transfer 读写规划统一基于 `datatype.TableInfo` 等标准结构。
3. Asset、Search、Quality、Model、Copilot 等模块统一使用 `datatype` 和 Meta attributes，不再引入本地字段模型。

当前进展：

- Manager 已按 `ItemMetadataTableInfo` / `ItemMetadataFields` / `type_info.graph` 消费 table 和 graph facts；graph 预览与属性页只展示轻量结构摘要，不维护 Graph 模块领域模型。
- Transfer pipeline 已迁移到统一 table facts。
- Service 图查询服务已从旧 label item 口径迁移为读取 graph item 的 `type_info.graph.node_shapes`。
- Meta known single media item refresh 已按 `ItemMetadataMediaInfo` / `type_info.media` 消费 media facts；Manager 媒体预览仍基于标准 attributes 与 raw / range content。
- Meta container summary 和 deep children enrich 已按 `ItemMetadataContainerInfo` / `type_info.container` 消费 container facts；Manager 容器预览继续基于标准 attributes 与按需 child resolver。

下一阶段候选：

1. MongoDB collection：确认文档集合的 data type 归属，以及动态字段画像继续走 table facts 还是 document 专用事实。
2. container 后续增强：ZIP、Excel、SQLite、GeoPackage 这类 native child 场景已具备主事实承载点；后续继续核实真实样例和 Manager child resolver 体验，不把 child 样本或完整字段塞回父 `ContainerInfo`。
3. media 后续增强：image/audio/video 当前通用字段已足以作为 `ItemMetadata.Media` 主事实；音视频 codec、bitrate、sample rate 等继续暂留 format/extraction，除非有明确消费方。
4. 文档全文检索：DOCX / PPTX / WPS 的全文不进入 `DocumentInfo`。Meta 深度扫描或 extraction 任务负责调用 `DocumentTextReader` / 外部 extractor 抽取正文并写入 Meilisearch；attributes 只记录 `type_info.document`、`capabilities.extraction` 状态、预览或外部索引引用；Manager 只提供检索入口和结果展示，不解析文档。

## 不做事项

- 不让 `common/engine` 依赖 `common/format`。
- 不让 `common/datatype` 依赖 `common/format` 或 `common/engine`。
- 不把 format detection、provider registry 或 engine provider 下沉到 `common/datatype`。
- 不把 engine 私有字段属性通过自由 `Attributes` 抢先暴露。
- 不在 `common/datatype` 中定义 Manager 前端 DTO。
- 不在本次重构中顺手设计完整索引 / 约束 / 分区 / 统计模型；这些应在消费方明确后单独设计。

## 验收标准

- `common/datatype` 成为 ADDP data type、field type、type info 和横切基础结构的唯一事实源。
- `common/format`、`common/engine`、`common/dataitem` 都依赖 `common/datatype`，而不是互相依赖。
- Meta 数据库扫描不再进行 `FieldInfo -> ColumnInfo -> FieldInfo` 的来回转换。
- `type_info.table/document/media/container/graph/file` 的来源和转换规则集中在 Meta。
- 通用字段属性稳定进入 `type_info.table.fields[]`。
- 私有字段属性不会因有自由扩展字段而被无消费方地写入。
- 旧的重复 `DataType`、`FieldType`、`FieldInfo`、`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo` 定义被删除。
