# ADDP 元数据 attributes 规范

本文定义 `meta_item.attributes` 的标准分区、唯一事实源和扩展命名空间规则。概念边界以 [ADDP 数据类型与格式体系图](addp数据类型与格式体系图.md) 为准。

## 目标结构

`meta_item.attributes` 采用“受控核心 + 开放能力”：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "type_info": {},
  "format_info": {},
  "capabilities": {}
}
```

顶层只允许长期保留：

- `schema_version`
- `storage`
- `item`
- `type_info`
- `format_info`
- `capabilities`

旧 attributes 字段、旧分区和平铺字段不保留兼容读取或兼容写入。旧数据应删除后重新 meta 扫描生成新结构；仍依赖旧结构的代码应尽早暴露并修正。

## 术语统一

attributes 分区统一采用以下概念：

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `storage` | 这个 item 在引擎侧的存储和访问属性是什么 | physical_path、bucket、path、etag、content_type、last_modified_at、total_size |
| `item` | 这个 data item 的核心语义是什么 | organization、data_type、format、entry_path、component_files、file_count |
| `type_info` | 对应数据类型的通用元数据是什么 | table fields、media width/height、document page_count、container children |
| `format_info` | 对应文件格式的私有信息是什么 | csv delimiter、shapefile components、sqlite version |
| `capabilities` | 这个 item 有哪些横切能力 | spatial、temporal、statistics、extraction、semantic、partitioning、indexing |

`schema` 不作为通用分区名。表格型数据的字段、主键、索引、行数应进入 `type_info.table`。

## 唯一事实源

| 信息 | 唯一事实源 / 规范存储点 | 说明 |
|---|---|---|
| item 主键 | `meta_item.id` | 不进入 attributes |
| 租户、引擎、节点归属 | `tenant_id`、`engine_id`、`node_id` | attributes 不重复表达关系归属 |
| item 类型表字段 | `meta_item.item_type` | 路由基础列，不写入 attributes；不等同于 `item.data_type` |
| 名称和逻辑全名 | `meta_item.name`、`full_name` | 不写入 attributes |
| fingerprint | `meta_item.fingerprint` | 不写入 attributes |
| 大小 | `meta_item.size_bytes` + `attributes.storage.total_size` | 表列用于列表和排序，attributes 保存源存储视角 |
| 修改时间 | `meta_item.data_updated_at` + `attributes.storage.last_modified_at` | `scanned_at` 不进入 attributes |
| data item 核心语义 | `attributes.item` | organization、data_type、format、entry_path、component_files |
| 类型信息 | `attributes.type_info.<data_type>` | table、document、media、container、graph 等通用类型信息 |
| 格式信息 | `attributes.format_info.<format>` | 具体文件格式私有信息 |
| 横切能力 | `attributes.capabilities.<capability>` | spatial、temporal、statistics、extraction、semantic、partitioning、indexing |

同一事实只能有一个规范存储点，不允许双写旧字段和新字段。

## 分区职责

| 分区 | 写入来源 | 内容 |
|---|---|---|
| `storage` | 引擎抽象层、catalog、对象枚举 | physical_path、bucket、path、content_type、etag、last_modified_at、total_size |
| `item` | `common/dataitem`、meta normalizer | organization、data_type、format、entry_path、component_files、file_count |
| `type_info` | 数据库 metadata、parser、采样器、extractor | table fields、primary_key、indexes、row_count；media kind/width/height/duration；document title/page_count；container children |
| `format_info` | parser、extractor、plugin | CSV 分隔符、Shapefile 组件、GeoJSON 类型、SQLite 版本等具体格式信息 |
| `capabilities` | parser、extractor、plugin、画像任务 | spatial、temporal、statistics、extraction、semantic、partitioning、indexing 等横切能力 |

## 写入规则

1. `meta` 在落库前通过统一 normalizer 生成 attributes。
2. 引擎抽象层只提供存储和 catalog 基础信息，不直接决定 `data_type` 或 `organization`。
3. `common/dataitem` 负责生成 `item` 分区的核心语义。
4. `common/format` 的 parser / extractor 只提供类型信息、格式信息和横切能力，不直接覆盖 `item.format`、`item.data_type`、`item.organization` 等核心字段。
5. 第三方插件不得直接写入平台保留字段，只能返回候选识别信息和命名空间扩展。
6. 容器内部 table、sheet、layer、文件默认写入 `type_info.container.children`；未形成规范前不得自动展开为独立 meta item。
7. 空间、时间、统计、提取、语义、分区、索引等不应进入 `data_type` 或 `format_info`，应作为横切能力写入 `capabilities`。

## type_info 结构约定

`type_info` 按数据类型分区：

| 数据类型 | 分区 | 典型字段 |
|---|---|---|
| `table` | `type_info.table` | fields、primary_key、indexes、row_count、sample |
| `document` | `type_info.document` | title、author、page_count、word_count、language、summary |
| `media` | `type_info.media` | kind、width、height、duration、codec、sample_rate、color_mode |
| `container` | `type_info.container` | children、default_child、child_count、resource_count |
| `graph` | `type_info.graph` | labels、relationships、properties、node_count、edge_count |
| `unknown` | `type_info.unknown` | detection_reason、fallback_preview |

表字段统一放在 `type_info.table.fields`，不得写入 attributes 顶层。字段级空间类型、原始字段类型、nullable 等属于 table field info；哪个字段是空间字段、SRID、extent 等属于 `capabilities.spatial`。

## format_info 命名空间

格式私有或第三方扩展必须进入合规命名空间，例如：

- `format_info.csv`
- `format_info.shapefile`
- `format_info.geojson`
- `format_info.sqlite`
- `format_info.com.vendor.plugin_name`

`format_info.unqualified` 是 normalizer 的隔离区，不是业务语义命名空间。正常的新 detector、parser、extractor 不应主动写入 `unqualified`。平台级行为不得依赖 `unqualified`。

## capabilities 命名空间

标准横切能力包括：

| 命名空间 | 含义 | 典型字段 |
|---|---|---|
| `spatial` | 空间能力 | geometry_column、geometry_type、geometry_types、srid、extent、dimension、has_spatial_index |
| `temporal` | 时间能力 | time_columns、time_range、granularity、timezone |
| `statistics` | 统计和采样 | sample_size、null_count、min、max、profiled_at |
| `extraction` | 内容提取 | metadata_extracted、extractor_available、plain_text_preview、summary、index_ref |
| `semantic` | 语义能力 | embedding_model、vector_index_ref、semantic_tags |
| `partitioning` | 分区能力 | partition_columns、partition_count、partition_sample |
| `indexing` | 索引能力 | spatial_indexes、fulltext_indexes、vector_indexes |

横切能力不应变成顶层数据类型，也不应被塞进具体格式信息。

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
- 预览路由不得依赖任意 custom key。
- 搜索索引可以选择性索引私有格式信息和横切能力，但应记录来源和字段命名空间。
