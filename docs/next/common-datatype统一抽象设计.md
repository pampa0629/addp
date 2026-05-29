# Common Datatype 统一抽象设计

更新时间：2026-05-29

本文是 `common/datatype` 统一抽象重构的接力索引，不再作为正式规范事实源。正式口径以 `docs/concepts/`、`docs/spec/` 和对应模块文档为准；若本文与正式文档冲突，以正式文档为准。

## 正式文档承接位置

| 主题 | 正式文档 |
|---|---|
| 数据类型、文件格式、横切能力的概念边界 | [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md) |
| 数据项、资源、layout、refs、scope 的概念边界 | [ADDP 数据项体系图](../concepts/addp数据项体系图.md) |
| Meta 体系中的事实来源、扫描、attributes 流转 | [ADDP 元数据体系图](../concepts/addp元数据体系图.md) |
| 稳定术语和命名约定 | [ADDP 术语表](../concepts/addp术语表.md) |
| provider / reader 矩阵、FormatPlugin、数据类型与格式能力 | [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md) |
| `attributes.type_info`、`format_info`、`capabilities`、`access_index` 落点 | [ADDP 元数据 attributes 规范](../spec/addp元数据attributes规范.md) |
| 引擎插件 provider 边界、`ItemMetadata`、MongoDB / Graph provider | [ADDP 引擎插件接口规范](../spec/addp引擎插件接口规范.md) |
| 存储引擎 item_type、data_type 与路径模型 | [ADDP 存储引擎路径体系规范](../spec/addp存储引擎路径体系规范.md) |
| 数据项识别、detector、layout 分类 | [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md) |
| 新增数据引擎时的接口选择和验证 | [ADDP 数据引擎扩展指南](../spec/addp数据引擎扩展指南.md) |
| Meta 模块内 attributes helper 和输入边界 | [Meta 模块说明](../../meta/CLAUDE.md) |

## 核心决策

`common/datatype` 是 ADDP data type、field type、type info 和横切基础结构的统一事实源。

它负责：

- `DataType`：`unknown`、`table`、`document`、`media`、`container`、`graph`。
- 通用 type info：`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo`、`GraphInfo`。
- `FieldType`、`FieldInfo` 等跨 engine / format 共享字段结构。
- `SpatialInfo`、`AccessIndex` 等横切基础结构的公共定义。

它不负责：

- format detection、FormatPlugin registry、content reader 注册。
- engine 连接、catalog 枚举、provider 实现。
- Meta item 识别、attributes 落库、Manager DTO 或 Transfer 计划。

`file` 不是基础 data type。file、object、directory、bucket、prefix、root 等只表达 catalog / storage 形态；内容语义无法识别时使用 `unknown`。

空间、时间、统计、提取、语义、分区、索引等不新增为基础 data type，应作为横切事实进入正式规范定义的命名空间。

## MongoDB Collection 口径

MongoDB collection 的结论已经收口：

- `meta_item.item_type=collection`，保留 MongoDB 原生 catalog 叶子术语。
- `attributes.item.data_type=table`，按动态 schema 记录集合消费。
- `type_info.table.fields` 表达采样推断字段画像，不是强 schema。
- 记录数写入 `type_info.table.row_count`。
- 采样画像进入 `capabilities.statistics`。
- 索引摘要进入 `capabilities.indexing`。
- 不写 `type_info.document`。
- 不新增 `type_info.collection` 或 `document_collection` data type。

命名约定：

- engine 侧采样 provider：`DynamicSchemaSamplingProvider` / `SampleDynamicSchema`。
- Meta attributes helper：`BuildDynamicSchemaAttributes` / `ApplyDynamicSchemaStatistics`。

## 已完成状态

- table：`format.TableInfo` / `format.FieldInfo` 薄壳已删除，reader / writer / Transfer / engine tabular catalog 统一使用 `datatype.TableInfo` / `datatype.FieldInfo`；`plugin.ItemMetadata.Table *datatype.TableInfo` 是 table item 主事实。
- graph：`plugin.ItemMetadata.Graph *datatype.GraphInfo` 已落地；Neo4j catalog、Meta 扫描、Manager 预览和属性页、Graph 模块、Service 图查询服务已迁移到 graph item + `type_info.graph` 口径。
- graph 内部结构过滤：Neo4j Spatial 的 `SpatialLayer` 节点和 `RTREE_*` 关系不进入 GraphInfo、计数、样本、Graph Browser、Schema 推导、知识服务或 GDS 投影。
- document：`plugin.ItemMetadata.Document *datatype.DocumentInfo` 已落地；Meta single resource deep scan、refresh 和对象按需元数据入口统一通过 `DocumentInfoProvider` 写入 `type_info.document`。
- media：`plugin.ItemMetadata.Media *datatype.MediaInfo` 已落地；Meta single resource deep scan、refresh 和对象按需元数据入口统一通过 `MediaInfoProvider` 写入 `type_info.media`；对象存储扫描中的 inline media extractor 旁路已删除。
- container：`plugin.ItemMetadata.Container *datatype.ContainerInfo` 已落地；Meta container summary、deep children enrich、对象按需元数据入口和已知 item refresh 写入 `type_info.container`。
- file：`DataTypeFile` / `FileInfo` 已删除。
- Manager / common client：对象预览按需元数据触发已改为判断标准 attributes 是否已有 `type_info.*` / `format_info` 主事实；旧 `ObjectPreview.extracted_metadata`、共享前端 `ExtractedMetadata` 旧展示组件、旧 payload 展示入口和未使用的旧 `TryExtractMetadata` helper 已删除。
- MongoDB：已收口为动态 schema 记录集合型引擎，`engine_family=dynamic_schema`；catalog helper 使用 `DynamicSchemaCatalog*`，能力 helper 使用 `NewDynamicSchemaCapabilities`，Manager 预览 provider 使用 `builtin:dynamic-schema-collection`；`avg_doc_size` 已替换为 `avg_record_size`。
- 文档整合：概念层、规范层和 Meta 模块文档已承接本轮稳定结论；本文已瘦身为接力索引。

## 阶段性总结

本轮 common-datatype / MongoDB 收口已完成以下阶段目标：

- 正式文档体系已完成整合：概念层只保留数据类型、格式、item、Meta 事实来源等边界；规范层承接 attributes、provider / reader、engine capabilities、路径和 detector 约束；Meta 模块文档记录模块内 helper 输入边界。
- `docs/next/common-datatype统一抽象设计.md` 已从长篇设计稿瘦身为接力索引。
- MongoDB collection 已从文档型旧口径收敛为动态 schema 记录集合：`item_type=collection`、`data_type=table`、`type_info.table.fields`、`capabilities.statistics`、`capabilities.indexing` 是唯一主路径。
- 旧命名和旧字段已清理：`DocumentCatalog*`、`NewDocumentCapabilities`、`doc-collection`、`avg_doc_size` 不再作为主路径存在。

## 后续 backlog

1. container 体验核实：ZIP、Excel、SQLite、GeoPackage 这类 native child 场景继续核实真实样例和 Manager child resolver 体验；不得把 child 样本或完整字段塞回父 `ContainerInfo`。
2. media 增强评估：image/audio/video 当前通用字段作为 `ItemMetadata.Media` 主事实；音视频 codec、bitrate、sample rate 等继续暂留 format / extraction，除非出现明确消费方。
3. 文档全文检索：DOCX / PPTX / WPS 的全文不进入 `DocumentInfo`。Meta 深度扫描或 extraction 任务负责调用 `DocumentTextReader` / 外部 extractor 抽取正文并写入搜索索引；attributes 只记录 `type_info.document`、`capabilities.extraction` 状态、预览或外部索引引用。

## 不做事项

- 不让 `common/engine` 依赖 `common/format`。
- 不让 `common/datatype` 依赖 `common/format` 或 `common/engine`。
- 不把 format detection、provider registry 或 engine provider 下沉到 `common/datatype`。
- 不把 engine 私有字段属性通过自由 `Attributes` 抢先暴露。
- 不在 `common/datatype` 中定义 Manager 前端 DTO。
- 不在没有真实消费方时新增索引、约束、分区、统计等完整模型。
- 不为旧 Meta attributes、旧字段名或旧 data type 保留运行期兼容读取。

## 验收标准

- `common/datatype` 是 ADDP data type、field type、type info 和横切基础结构的唯一事实源。
- `common/format`、`common/engine`、`common/dataitem` 都依赖 `common/datatype`，而不是互相依赖。
- Meta 数据库扫描不再进行 `FieldInfo -> ColumnInfo -> FieldInfo` 的来回转换。
- `type_info.table/document/media/container/graph` 的来源和转换规则集中在 Meta attributes helper。
- 通用字段属性稳定进入 `type_info.table.fields[]`。
- 私有字段属性不会因有自由扩展字段而被无消费方地写入。
- 旧的重复 `DataType`、`FieldType`、`FieldInfo`、`TableInfo`、`DocumentInfo`、`MediaInfo`、`ContainerInfo` 定义被删除。

## 验证命令

文档整合后至少执行：

```bash
git diff --check -- docs/README.md docs/concepts/addp数据项体系图.md docs/concepts/addp数据类型和格式体系图.md docs/concepts/addp元数据体系图.md docs/concepts/addp术语表.md docs/spec/addp数据项探测器规范.md docs/spec/addp数据类型与格式能力规范.md docs/spec/addp数据引擎扩展指南.md docs/spec/addp元数据attributes规范.md docs/spec/addp存储引擎路径体系规范.md meta/CLAUDE.md docs/next/common-datatype统一抽象设计.md
rg -n "DocumentMetadataSamplingProvider|SampleDocumentMetadata|BuildDocumentCollection|DocumentCollectionAttributesInput|type_info\\.collection" docs meta/CLAUDE.md
rg -n "DocumentCatalogModel|DocumentCatalogCallbacks|NewDocumentCapabilities|doc-collection|avg_doc_size" docs common meta manager system develop common-frontend
```
