# ADDP 元数据 attributes 规范

本文定义 `meta_item.attributes` 的标准分区、唯一事实源和扩展命名空间规则。概念边界以 [ADDP 数据项体系图](../concepts/addp数据项体系图.md) 和 [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md) 为准。扫描深度和刷新机制见 [ADDP 元数据扫描机制规范](addp元数据扫描机制规范.md)。

## 目标结构

`meta_item.attributes` 采用“受控核心 + 开放能力”：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "type_info": {},
  "format_info": {},
  "content_index": {},
  "capabilities": {}
}
```

顶层只允许长期保留：

- `schema_version`
- `storage`
- `item`
- `type_info`
- `format_info`
- `content_index`
- `capabilities`

旧 attributes 字段、旧分区和平铺字段不保留兼容读取或兼容写入。旧数据应删除后重新 meta 扫描生成新结构；仍依赖旧结构的代码应尽早暴露并修正。

## 术语统一

attributes 分区统一采用以下概念：

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `storage` | 这个 item 在引擎侧的存储和访问属性是什么 | physical_path、bucket、path、etag、content_type、last_modified_at、total_size |
| `item` | 这个 data item 的核心语义是什么 | layout、data_type、format、refs、file_count、scope_exclusive |
| `type_info` | 对应数据类型的通用元数据是什么 | table fields、media width/height、document page_count、container children |
| `format_info` | 对应文件格式的私有信息是什么 | csv delimiter、shapefile refs、sqlite version |
| `content_index` | 面向内容读取的通用访问索引是什么 | table sparse_row_index |
| `capabilities` | 这个 item 有哪些横切能力 | spatial、temporal、statistics、extraction、semantic、partitioning、indexing |

`schema` 不作为通用分区名。表格型数据的字段、主键、索引、行数应进入 `type_info.table`。

## 唯一事实源

| 信息 | 唯一事实源 / 规范存储点 | 说明 |
|---|---|---|
| item 主键 | `meta_item.id` | 不进入 attributes |
| 租户、引擎、节点归属 | `tenant_id`、`engine_id`、`node_id` | attributes 不重复表达关系归属 |
| item 类型表字段 | `meta_item.item_type` | 引擎 catalog / 路径模型的原生叶子类型，路由基础列，不写入 attributes；不等同于 `item.data_type` |
| 名称和逻辑全名 | `meta_item.name`、`full_name` | 不写入 attributes |
| fingerprint | `meta_item.fingerprint` | 不写入 attributes |
| 大小 | `meta_item.size_bytes` + `attributes.storage.total_size` | 表列用于列表和排序，attributes 保存源存储视角 |
| 修改时间 | `meta_item.data_updated_at` + `attributes.storage.last_modified_at` | `scanned_at` 不进入 attributes |
| data item 核心语义 | `attributes.item` | layout、data_type、format、refs、file_count、scope_exclusive、claim_policy |
| 类型信息 | `attributes.type_info.<data_type>` | table、document、media、container、graph 等通用类型信息 |
| 格式信息 | `attributes.format_info.<format>` | 具体文件格式私有信息 |
| 内容访问索引 | `attributes.content_index.<data_type>` | 面向内容读取优化的索引，例如 table 稀疏行索引 |
| 横切能力 | `attributes.capabilities.<capability>` | spatial、temporal、statistics、extraction、semantic、partitioning、indexing |

同一事实只能有一个规范存储点，不允许双写旧字段和新字段。

`meta_item.item_type` 必须跟随所属引擎的原生叶子术语：对象存储为 `object`，文件系统为 `file`，关系型数据库为 `table` / `view`，MongoDB 为 `collection`，Neo4j 为 `label` / `relationship`。内容可读成表格、文档、媒体或容器时，只更新 `attributes.item.data_type`、`attributes.item.format`、`type_info`、`format_info` 和 `capabilities`，不得反向改写 `meta_item.item_type`。

## 分区职责

| 分区 | 写入来源 | 内容 |
|---|---|---|
| `storage` | 引擎抽象层、catalog、对象枚举 | physical_path、bucket、path、content_type、etag、last_modified_at、total_size |
| `item` | Meta 扫描、Meta item normalizer | layout、data_type、format、refs、file_count、scope_exclusive、claim_policy |
| `type_info` | 数据库 metadata、format info provider、采样器、Meta item normalizer | table fields、primary_key、indexes、row_count；media kind/width/height/duration；document title/page_count；container children |
| `format_info` | format plugin / provider、Meta item normalizer | CSV 分隔符、Shapefile related refs、JSON 结构类型、SQLite 版本等具体格式信息 |
| `content_index` | format plugin / reader、Meta item normalizer | 用于按内容窗口读取的访问索引，例如 table 稀疏行号到字节偏移索引 |
| `capabilities` | format provider、画像任务、Meta item normalizer | spatial、temporal、statistics、extraction、semantic、partitioning、indexing 等横切能力 |

## 写入规则

1. `meta` 在落库前通过统一 normalizer 生成 attributes，并对平台核心字段拥有最终裁决权。
2. 引擎抽象层只提供资源位置、catalog 和基础存储属性，不直接决定 `data_type` 或 `layout`。
3. data item 的 content 内容布局、识别逻辑、claims、exclusive、`refs`、`meta_item.full_name` 决策见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)；本规范只定义这些结果如何进入 `attributes.item` 和相关分区。
4. `common/format` 只提供文件格式枚举、格式识别、类型信息 / 格式信息模型、format plugin、info provider、content reader 和 analyzer 等通用能力，不直接决定 meta item 如何归并，也不绕过 Meta normalizer 写最终 attributes。
5. `common/jsonmap` 只作为 decoded JSON map 的通用读写 helper，不承载 attributes 规范语义；不得再使用 `common/attributes` 作为 attributes 规范包占位。
6. 第三方插件不得直接写入平台保留字段，只能返回候选识别信息和命名空间扩展。
7. 容器内部 table、sheet、layer、文件默认写入 `type_info.container.children`；未形成规范前不得自动展开为独立 meta item。
8. 空间、时间、统计、提取、语义、分区、索引等不应进入 `data_type` 或 `format_info`，应作为横切能力写入 `capabilities`。
9. `meta_item.full_name` 是 data item 在引擎内的唯一逻辑标识和定位事实源。attributes 不再定义通用 `entry_path` 字段。
10. 对 `layout=multi` 的 item，primary content 应直接作为 `meta_item.full_name`，related refs 写入 `item.refs`。
11. 对 `layout=whole` 的 item，whole scope 根范围应直接作为 `meta_item.full_name`，并在 `item.scope_exclusive=true`、`item.claim_policy=whole_scope` 中表达独占语义。
12. `content_index` 是读取优化信息，不是 data type info，也不是 format 私有信息。索引必须能通过源对象大小、etag、mtime 或 fingerprint 等事实判断是否仍适用于当前资源；资源变化后应重建，不得继续复用旧索引。对于 multi-ref 格式，`content_index.source` 允许记录 ref 级事实（如 `refs`、`ref_count`、`index_format`），用于重建、失效判断和调试，但不应把格式私有语义写成新的顶层分区。
13. attributes 写入受 `scan_depth` 约束。`basic` 只写不读取 file/object 内容即可获得的身份、存储和轻量 item 事实；字段、行数、容器 children、`content_index`、需要读取内容的 `format_info` 和横切能力应由 `deep` 写入。

## content_index 结构约定

`content_index` 按数据类型分区。当前标准化 `table` 的稀疏行索引：

```json
{
  "content_index": {
    "table": {
      "kind": "sparse_row_index",
      "data_type": "table",
      "format": "csv",
      "unit": "row",
      "offset_unit": "byte",
      "step": 5000,
      "row_count": 73090,
      "header_bytes": 27,
      "source": {
        "size_bytes": 83886080,
        "etag": "..."
      },
      "anchors": [
        { "row": 0, "byte_offset": 27 },
        { "row": 5000, "byte_offset": 570122 }
      ]
    }
  }
}
```

字段规则：

| 字段 | 规则 |
|---|---|
| `kind` | 索引类型。表格稀疏行索引固定为 `sparse_row_index`。 |
| `unit` | 逻辑定位单位。表格采样固定为 `row`。 |
| `offset_unit` | 物理读取偏移单位。对象存储 range reader 使用 `byte`。 |
| `anchors` | 稀疏锚点。`row` 为数据行号，从 0 开始；`byte_offset` 为该行起始字节偏移。 |
| `header_bytes` | 表头结束后的字节偏移；对 CSV 等有表头格式，通常也是 row 0 的 byte offset。 |
| `source` | 索引绑定的源对象事实。能获取 size、etag、last_modified_at、fingerprint 时应写入。 |

`content_index` 只描述如何更快定位内容窗口，不描述字段、行数等类型事实。字段和总行数仍写入 `type_info.table.fields` 与 `type_info.table.row_count`。

## type_info 结构约定

`type_info` 按数据类型分区：

| 数据类型 | 分区 | 典型字段 |
|---|---|---|
| `table` | `type_info.table` | fields、primary_key、indexes、row_count、sample |
| `document` | `type_info.document` | title、author、page_count、word_count、language、summary |
| `media` | `type_info.media` | kind、width、height、duration、codec、sample_rate、color_mode |
| `container` | `type_info.container` | children、default_child、child_count、resource_count |
| `graph` | `type_info.graph` | labels、relationships、properties、node_count、edge_count |
| `unknown` | `type_info.unknown` | detection_reason、fallback_action |

表字段统一放在 `type_info.table.fields`，不得写入 attributes 顶层。字段不是 data item，字段类型只能使用 `type` 表达 ADDP 标准字段类型，不得在字段对象内写入 `data_type`。原生字段类型如需展示，只能作为只读诊断信息写入 `native_type`，不得参与执行决策；哪个字段是空间字段、SRID、extent 等属于 `capabilities.spatial`。

## format_info 命名空间

格式私有或第三方扩展必须进入合规命名空间，例如：

- `format_info.csv`
- `format_info.shapefile`
- `format_info.json`
- `format_info.sqlite`
- `format_info.com.vendor.plugin_name`

`format_info.unqualified` 是 normalizer 的隔离区，不是业务语义命名空间。正常的新 detector、format provider 或 reader 不应主动写入 `unqualified`。平台级行为不得依赖 `unqualified`。

## capabilities 命名空间

标准横切能力包括：

| 命名空间 | 含义 | 典型字段 |
|---|---|---|
| `spatial` | 空间能力 | geometry_columns、primary_geometry_column、extent、has_spatial_index |
| `temporal` | 时间能力 | time_columns、time_range、granularity、timezone |
| `statistics` | 统计和采样 | sample_size、null_count、min、max、profiled_at |
| `extraction` | 内容提取 | metadata_extracted、extractor_available、text_excerpt、summary、index_ref |
| `semantic` | 语义能力 | embedding_model、vector_index_ref、semantic_tags |
| `partitioning` | 分区能力 | partition_columns、partition_count、partition_sample |
| `indexing` | 索引能力 | spatial_indexes、fulltext_indexes、vector_indexes |

横切能力不应变成顶层数据类型，也不应被塞进具体格式信息。

### capabilities.spatial 最小结构

`capabilities.spatial` 表达跨格式稳定消费的空间能力，不负责完整空间画像。meta 扫描阶段只写能够从字段声明、格式头或轻量元数据中确定的信息，不为了推断实际几何类型而扫描全量数据。

建议最小结构：

```json
{
  "geometry_columns": [
    {
      "name": "geometry",
      "geometry_type": "geometry",
      "srid": 4326,
      "dimension": 2,
      "nullable": false
    }
  ],
  "primary_geometry_column": "geometry",
  "extent": null,
  "has_spatial_index": false
}
```

字段规则：

| 字段 | 规则 |
|---|---|
| `geometry_columns` | 支持多个 Geometry 字段。每个元素描述一个空间字段。 |
| `geometry_type` | 写入声明类型或格式天然可确定的类型。PostGIS 字段声明为 `geometry` 时就写 `geometry`，不得为了得到 Point/Polygon 等具体类型扫描全表。 |
| `srid` | 能确定 EPSG/SRID 编号时写数字，例如 `4326`。 |
| `crs` | 不能确定编号但能获得 CRS 描述时写 `crs`，例如 WKT、PROJJSON、proj4。`srid` 和 `crs` 二选一，不同时写。 |
| `dimension` | 坐标维度，无法确定时可省略。 |
| `nullable` | 字段是否可空，无法确定时可省略。 |
| `primary_geometry_column` | Manager 默认空间预览使用的几何字段。多几何字段时必须明确；单几何字段时建议写入。 |
| `extent` | 空间范围。无法轻量获得时写 `null` 或省略，不得为了 extent 扫描全量数据。 |
| `has_spatial_index` | 是否存在空间索引；无法确定时可省略。 |

`feature_count` 不属于 spatial，表格型行数写入 `type_info.table.row_count`。如果后续画像任务采样得到实际几何类型分布，应进入 `capabilities.statistics` 或后续画像结构，不反向覆盖 meta 扫描阶段的 `geometry_type`。

## 冲突处理

如果多个来源提供同名或同义信息，按以下优先级处理：

1. `meta` normalizer 对平台核心字段拥有最终裁决权。
2. item 识别结果优先于单个 parser 的局部猜测。
3. 显式 parser 输出优先于仅基于扩展名或 MIME 的推断。
4. 第三方私有扩展不能覆盖平台标准字段。
5. 冲突信息可以保留在合规私有命名空间中，但不能影响核心路由，除非经过标准化。

## 消费规则

`manager`、`transfer`、`asset`、`search` 等模块必须遵循：

- 平台级行为只依赖 `storage`、`item`、`type_info`、`format_info` 和平台标准 `capabilities`。
- 私有格式信息默认只用于展示、诊断或插件自身能力。
- Manager 内容路由不得依赖任意 custom key。
- 搜索索引可以选择性索引私有格式信息和横切能力，但应记录来源和字段命名空间。
