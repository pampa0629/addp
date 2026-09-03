# ADDP 元数据 attributes 规范

本文定义 `meta_item.attributes` 和 `meta_node.attributes` 的标准分区、唯一事实源和扩展命名空间规则。概念边界以 [ADDP 数据项体系图](../concepts/addp数据项体系图.md) 和 [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md) 为准。扫描深度和刷新机制见 [ADDP 元数据扫描机制规范](addp元数据扫描机制规范.md)。

## meta_item.attributes 目标结构

`meta_item.attributes` 采用“受控核心 + 开放能力”：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "type_info": {},
  "format_info": {},
  "access_index": {},
  "capabilities": {}
}
```

顶层只允许长期保留：

- `schema_version`
- `storage`
- `item`
- `type_info`
- `format_info`
- `access_index`
- `capabilities`

Manager infra 中的 GLB、3D Tiles、S3M、COPC、KSplat、COG 等快显结果不是 data item，不写入源 item 或虚构目标 item 的 attributes。源 item、源指纹、目标格式、任务、execution 与快显结果的关系由 Manager 对应结果表显式保存；不得通过输出目录名推断。Develop 工作流写入业务存储并经 Meta 扫描形成的目标才是派生 data item，其 attributes 仍只描述该目标 item 自身的标准事实。

Manager infra 中的 PMTiles 快显缓存同样不是 data item。只有通过 `vector_tile_set_generation` 原子写入 Business 存储、校验完成并经 Meta 扫描后，才形成 `data_type=media + format=pmtiles + layout=single` 的业务 data item。

旧 attributes 字段、旧分区和平铺字段不保留兼容读取或兼容写入。旧数据应删除后重新 meta 扫描生成新结构；仍依赖旧结构的代码应尽早暴露并修正。

空分区不落库：`storage`、`type_info`、`format_info`、`access_index`、`capabilities` 等标准分区只有在包含实际事实时才写入。空对象、空字符串和只包含空对象的命名空间应在 normalizer 中删除。空数组可以作为显式事实保留，例如 `type_info.container.children: []` 表示容器已解析但没有子项。

## meta_node.attributes 目标结构

`meta_node.attributes` 只表达 catalog branch 的结构和低成本范围事实，不复用 `meta_item.attributes` 的 item / type_info / format_info / access_index / capabilities 分区：

```json
{
  "schema_version": 1,
  "catalog": {},
  "storage": {}
}
```

顶层只允许长期保留：

- `schema_version`
- `catalog`
- `storage`

分区职责：

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `catalog` | 这个 node 在 catalog 树中的结构事实是什么 | root_term、native_name、display_name_source |
| `storage` | 这个 branch 对应的低成本存储范围是什么 | bucket、path |

`meta_node.attributes` 不表达 data item 语义，不写入 `item`、`type_info`、`format_info`、`access_index` 或 `capabilities`。node 的 `item_count`、`total_size_bytes`、`scan_status`、`scanned_depth`、`scanned_at` 是 `meta_node` 表字段，不写入 attributes。历史裸 `bucket` / `path` 字段不保留兼容读取或兼容写入；旧数据应重新扫描生成新结构。

## 术语统一

attributes 分区统一采用以下概念：

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `storage` | 这个 item 在引擎侧的存储和访问属性是什么 | physical_path、bucket、path、etag、content_type、last_modified_at、total_size |
| `item` | 这个 data item 的核心语义是什么 | layout、data_type、format、refs、file_count、scope_exclusive |
| `type_info` | 对应数据类型的通用元数据是什么 | table fields、media width/height、document page_count、container children、cad layer_count、model_3d mesh_count、point_cloud point_count、gaussian_splat splat_count |
| `format_info` | 对应文件、容器或格式解析层面的私有信息是什么 | csv encoding、shapefile refs、sqlite version |
| `access_index` | 面向内容读取的通用访问索引是什么 | table sparse_row_index |
| `capabilities` | 这个 item 有哪些横切能力 | spatial、temporal、statistics、extraction、semantic、constraints、partitioning、indexing |

`schema` 不作为通用分区名。表格型数据的字段、主键、行数应进入 `type_info.table`；索引摘要进入 `capabilities.indexing`。

## 唯一事实源

| 信息 | 唯一事实源 / 规范存储点 | 说明 |
|---|---|---|
| item 主键 | `meta_item.id` | 不进入 attributes |
| 租户、引擎、节点归属 | `tenant_id`、`engine_id`、`node_id` | attributes 不重复表达关系归属 |
| item 类型表字段 | `meta_item.item_type` | 引擎 catalog / 路径模型的原生叶子类型，路由基础列，不写入 attributes；不等同于 `item.data_type` |
| 名称和逻辑全名 | `meta_item.name`、`full_name` | 不写入 attributes |
| 资源定位符 | `locator` / `ResourceLocator` | 不是 attributes 标准事实。定位符由 `engine_id`、`item_type`、`full_name` 等事实派生，写入搜索索引或 DTO 即可，不作为持久 attributes 存储。 |
| fingerprint | `meta_item.fingerprint` | 不写入 attributes |
| 大小 | `meta_item.size_bytes` + `attributes.storage.total_size` | 表列用于列表和排序，attributes 保存源存储视角 |
| 修改时间 | `meta_item.data_updated_at` + `attributes.storage.last_modified_at` | `scanned_at` 不进入 attributes |
| data item 核心语义 | `attributes.item` | layout、data_type、format、refs、file_count、scope_exclusive、claim_policy |
| 类型信息 | `attributes.type_info.<data_type>` | table、document、media、container、graph、cad、model_3d、point_cloud、gaussian_splat 等通用类型信息 |
| 格式信息 | `attributes.format_info.<format>` | 具体文件格式私有信息 |
| 访问定位索引 | `attributes.access_index.<data_type>` | 面向内容读取优化的索引，例如 table 稀疏行索引 |
| 横切能力 | `attributes.capabilities.<capability>` | spatial、temporal、statistics、extraction、semantic、partitioning、indexing |

同一事实只能有一个规范存储点，不允许双写旧字段和新字段。

`meta_item.item_type` 必须跟随所属引擎的稳定 catalog leaf 术语：对象存储为 `object`，文件系统为 `file`，关系型数据库为 `table` / `view`，MongoDB 为 `collection`，Neo4j 为 `graph`，业务 Kafka 为 `topic`。内容可读成表格、文档、媒体或容器时，只更新 `attributes.item.data_type`、`attributes.item.format`、`type_info`、`format_info` 和 `capabilities`，不得反向改写 `meta_item.item_type`。

Kafka topic 第一版 basic scan 只保存 item identity，`attributes.item.data_type=unknown`；Meta 不读取消息、不采样 JSON schema，也不创建 partition item。partition count、leader、replica、ISR 和 offset range 属于实时 topic facts / runtime diagnostics；在正式定义持久化结构前不得写入 attributes 顶层、`type_info` 或 `capabilities` 兜底字段。

## 分区职责

| 分区 | 写入来源 | 内容 |
|---|---|---|
| `storage` | 引擎抽象层、catalog、对象枚举 | physical_path、bucket、path、content_type、etag、last_modified_at、total_size |
| `item` | Meta 扫描、Meta item normalizer | layout、data_type、format、refs、file_count、scope_exclusive、claim_policy |
| `type_info` | 数据库 metadata、format info provider、采样器、Meta item normalizer | table fields、primary_key、精确 row_count、estimated_row_count；media kind/width/height/duration_ms；document title/page_count；container children；graph shapes；cad 图纸结构摘要；model_3d 结构摘要；point_cloud 点云摘要；gaussian_splat 高斯基元摘要 |
| `format_info` | format plugin / provider、Meta item normalizer | CSV 分隔符、Shapefile related refs、JSON 结构类型、SQLite 版本等具体格式信息 |
| `access_index` | format plugin / reader、Meta item normalizer | 用于按内容窗口读取的访问索引，例如 table 稀疏行号到字节偏移索引 |
| `capabilities` | engine / format provider、扫描采样事实提供方、Meta item normalizer | spatial、temporal、statistics、extraction、semantic、constraints、partitioning、indexing 等横切能力 |

`common/datatype` 是 type info、field type、空间横切事实和访问索引结构的代码事实源；attributes 是这些事实的落库模型。Meta normalizer 负责将 provider 返回的 `datatype.*` 结构写入对应 attributes 分区。

`common/datatype` 只处理自身结构和 JSON payload 的相互转换，不承载 `attributes` 分区路径语义，也不提供从完整 `meta_item.attributes` 读取标准分区的入口。读取或写入 `type_info.*`、`capabilities.*`、`access_index.*` 等路径是 Meta normalizer、MetaClient 消费方或上层模块的职责；它们取出对应分区后，才可调用 `datatype.*FromPayload` 等通用转换函数。

| 输入结构 | attributes 落点 |
|---|---|
| `datatype.TableInfo` | `attributes.type_info.table` |
| `datatype.DocumentInfo` | `attributes.type_info.document` |
| `datatype.MediaInfo` | `attributes.type_info.media` |
| `datatype.ContainerInfo` | `attributes.type_info.container` |
| `datatype.GraphInfo` | `attributes.type_info.graph` |
| `datatype.CADInfo` | `attributes.type_info.cad` |
| `datatype.Model3DInfo` | `attributes.type_info.model_3d` |
| `datatype.PointCloudInfo` | `attributes.type_info.point_cloud` |
| `datatype.GaussianSplatInfo` | `attributes.type_info.gaussian_splat` |
| `datatype.SpatialInfo` | `attributes.capabilities.spatial` |
| `datatype.AccessIndex` | `attributes.access_index.<data_type>` |

`datatype.AccessIndex` 只是当前代码中的共享结构归属，不表示访问索引属于 data type 或 type info。`access_index` 是独立 attributes 分区，服务内容窗口读取、range 读取和索引失效判断；不得写入 `type_info.table`、`format_info` 或 `capabilities.indexing`。

`attributes.type_info.file` 不存在，也不得新增。文件、对象和目录是 Engine Catalog / storage 形态，不是 data type 主事实；对应路径、名称、大小、MIME、etag、hash、last_modified 等事实只能写入 `attributes.storage`，或进入 `EngineCatalogEntry` / Meta item 的标准字段。无法识别内容语义时，`attributes.item.data_type` 必须为 `unknown`。

## 写入规则

1. `meta` 在落库前通过统一 normalizer 生成 attributes，并对平台核心字段拥有最终裁决权。
2. 引擎抽象层只提供资源位置、catalog 和基础存储属性，不直接决定 `data_type` 或 `layout`。
3. data item 的 content 内容布局、识别逻辑、claims、exclusive、`refs`、`meta_item.full_name` 决策见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)；本规范只定义这些结果如何进入 `attributes.item` 和相关分区。
4. `common/format` 只提供文件格式枚举、格式识别、format plugin、info provider、content reader、reader / writer 和 analyzer 等格式能力，不直接决定 meta item 如何归并，也不绕过 Meta normalizer 写最终 attributes。通用 data type / type info / field type 结构归属 `common/datatype`。
5. `common/jsonmap` 只作为 decoded JSON map 的通用读写 helper，不承载 attributes 规范语义；不得再使用 `common/attributes` 作为 attributes 规范包占位。
6. 第三方插件不得直接写入平台保留字段，只能返回候选识别信息和命名空间扩展。
7. 容器内部 table、sheet、layer、文件默认写入 `type_info.container.children`；未形成规范前不得自动展开为独立 meta item。
8. 空间、时间、统计、提取、语义、分区、索引等不应进入 `data_type` 或 `format_info`，应作为横切能力写入 `capabilities`。
9. `meta_item.full_name` 是 data item 在引擎内的唯一逻辑标识和定位事实源。attributes 不再定义通用 `entry_path` 字段。
10. 对 `layout=multi` 的 item，primary content 应直接作为 `meta_item.full_name`，related refs 写入 `item.refs`。
11. 对 `layout=whole` 的 item，whole scope 根范围应直接作为 `meta_item.full_name`，并在 `item.scope_exclusive=true`、`item.claim_policy=whole_scope` 中表达独占语义。
12. `locator` 不作为 attributes 标准分区写入。定位事实由 `meta_item.full_name`、`meta_item.item_type` 和搜索/DTO 层派生，不在 attributes 中重复保存。
13. `access_index` 是读取优化信息，不是 data type info，也不是 format 私有信息。索引必须能通过源对象大小、etag、mtime 或 fingerprint 等事实判断是否仍适用于当前资源；资源变化后应重建，不得继续复用旧索引。对于 multi-ref 格式，`access_index.source` 允许记录 ref 级事实（如 `refs`、`ref_count`、`index_format`），用于重建、失效判断和调试，但不应把格式私有语义写成新的顶层分区。
14. `SpatialInfo`、`AccessIndex`、`format_info` 不是 `TableInfo` 的组成部分。provider 如果一次解析同时得到这些事实，应作为同级结果交给 Meta normalizer，分别写入 `capabilities.spatial`、`access_index.<data_type>` 和 `format_info.<format>`。
15. attributes 写入受 `scan_depth` 约束。`basic` 只写不读取 file/object 内容即可获得的身份、存储和轻量 item 事实；来自 `EngineCatalogEntry` 或只读 database catalog / system table 的低成本 `estimated_row_count`、`size_bytes` 可在 basic 写入。字段、主键、索引、容器 children、`access_index`、需要读取内容的 `format_info`、横切能力以及需要执行 `COUNT(*)`、全量扫描或统计刷新的高成本统计应由 `deep` 写入。

## access_index 结构约定

`access_index` 按数据类型分区。当前标准化 `table` 的稀疏行索引：

```json
{
  "access_index": {
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

`access_index` 只描述如何更快定位内容窗口，不描述字段、行数等类型事实。字段和总行数仍写入 `type_info.table.fields` 与 `type_info.table.row_count`。

## type_info 结构约定

`type_info` 按数据类型分区：

| 数据类型 | 分区 | 典型字段 |
|---|---|---|
| `table` | `type_info.table` | fields、primary_key、row_count、estimated_row_count、size_bytes、native |
| `document` | `type_info.document` | title、language、encoding、page_count、word_count、size_bytes |
| `media` | `type_info.media` | kind、mime_type、width、height、duration_ms、encoding、color_space、size_bytes |
| `container` | `type_info.container` | children、default_child、child_count、resource_count |
| `graph` | `type_info.graph` | model、directed、node_shapes、relationship_shapes、node_count、relationship_count |
| `cad` | `type_info.cad` | drawing_kind、unit、entity_count、layer_count、layout_count、block_definition_count、xref_count、has_model_space、has_paper_space、bounds_2d、bounds_3d、size_bytes |
| `model_3d` | `type_info.model_3d` | model_kind、node_count、mesh_count、vertex_count、triangle_count、material_count、texture_count、animation_count、lod_count、bounds_3d、unit、up_axis |
| `point_cloud` | `type_info.point_cloud` | point_cloud_kind、point_count、point_format、dimension_count、dimensions、bounds_3d、scale、offset、has_color、has_intensity、has_classification |
| `gaussian_splat` | `type_info.gaussian_splat` | representation、splat_count、has_opacity、has_scale、has_rotation、has_spherical_harmonics、sh_degree、bounds_3d、sampled_bounds_3d、sampled_bounds_method、sampled_bounds_sample_count |
| `unknown` | `type_info.unknown` | detection_reason、fallback_action |

`type_info.media` 只承载 `datatype.MediaInfo` 中跨图片、音频、视频稳定通用的字段。EXIF、视频 codec、音频 codec、帧率、采样率、码率、轨道数等细粒度事实暂不作为 media 主事实；如需持久化，应进入受控 `format_info.<format>`、`capabilities.extraction` 或后续另行规范的横切能力命名空间。

`type_info.gaussian_splat.bounds_3d` 表示精确三维范围，只应在低成本元数据或小文件解析时写入。`sampled_bounds_3d` 表示采样得到的近似三维范围，必须同时写入 `sampled_bounds_method` 和 `sampled_bounds_sample_count`，可用于前端初始相机定位，但不得用于空间检索、质量治理或精确范围判断。Meta scan 不得为了 `bounds_3d` 全量扫描大规模高斯泼溅 PLY / SPLAT；需要精确范围时应先定义对应领域的专门分析或派生任务，不得借用表格数据剖析概念。

高斯泼溅格式私有诊断不得写入 `type_info.gaussian_splat`。例如 `.splat` 的 scale 分布、各向异性比例、低透明度计数和推荐渲染模式属于 `format_info.splat`，用于解释前端毛刺、过度模糊等格式渲染现象；这些事实不作为跨格式通用类型字段。

`type_info.document` 只承载文档结构元信息。正文是否已抽取、抽取器、预览文本、截断状态和外部索引引用属于 `capabilities.extraction`，不得在 `type_info.document.text_extracted` 中重复写入。

`type_info.container` 只承载容器结构事实和 child 轻量摘要，例如 `children`、`default_child`、`child_count`、`resource_count`。child 表格摘要中的 `row_count` 与 `estimated_row_count` 遵守和 `type_info.table` 相同的精确/估算语义；例如 Excel worksheet dimension 只能写入 `estimated_row_count`，不得写入 `row_count`。父容器级解析统计、采样上限和截断状态不得写入 `type_info.container.native`，应进入 `format_info.<format>`；child 级 `native` 只保留 child 定位或受控原生摘要，例如 SQLite 表原名、ZIP entry 定位事实。

`type_info.graph` 必须是业务图视图的 JSON payload。引擎插件、扩展、索引或空间能力产生的内部节点和内部关系不得写入 `node_shapes`、`relationship_shapes` 或计数字段；例如 Neo4j Spatial 的 `SpatialLayer` 节点和 `RTREE_*` 关系应在 provider 或 Graph 模块服务层过滤。

`type_info.graph` 使用 `common/datatype.GraphInfo` 的 JSON payload：

| 字段 | 规则 |
|---|---|
| `model` | 图模型，第一版取值为 `property_graph`、`rdf`、`generic`。 |
| `directed` | 是否按有向图描述关系。 |
| `node_count` | 业务图视图中的节点总数，不包含引擎内部节点。 |
| `relationship_count` | 业务图视图中的关系总数，不使用旧字段名 `edge_count`。 |
| `node_shapes` | 节点结构形状。Neo4j 单 label 节点形状使用 `kind=label`，多 label set 使用 `kind=label_set`。 |
| `relationship_shapes` | 关系结构形状，按 relationship type 和属性结构表达。 |
| `relationship_shapes[].patterns` | 关系起点和终点的配对模式，必须保留 from/to 配对关系。不得使用顶层 `from_labels[]` / `to_labels[]` 两个集合替代。 |

label set 必须标准化为去空、去重、排序后的稳定集合；当 node shape 或 endpoint 的 `name` / `shape_name` 为空时，可以由 label set 使用 `+` 连接派生。历史 Meta 数据如果仍使用 `edge_count`、顶层 `from_labels` / `to_labels`、独立 label item 或 relationship item，应删除后重新扫描，不在运行期保留兼容读取。

`type_info.cad` 只承载跨 CAD 格式稳定的图纸事实；当前 deep scan 写入 `drawing_kind=2d` 和文件大小。DWG 的 `AC10xx` 版本、ASCII DXF 可读取的 `$ACADVER` 分别进入 `format_info.dwg`、`format_info.dxf`；预览交互状态、entity 样本和 CAD→GIS 产物不得写入 attributes。Meta deep scan 只读取预算内文件头，不调用工作流 Runtime，也不解析或遍历 Geometry。

`type_info.model_3d` 只承载三维模型跨格式稳定结构摘要。`model_kind` 表达模型子形态，第一版取值为 `mesh_scene`、`photogrammetry_scene`、`bim_model`、`tiled_scene`、`generic`。GLB / glTF、OBJ / STL / FBX、IFC、单 OSGB、OSGB Scene 倾斜摄影、3D Tiles、Revit BIM 都使用 `data_type=model_3d`；不得因为倾斜摄影或 BIM 另行新增 `data_type=osgb`、`data_type=bim` 或平行 type info。单个 `.osgb` 文件的格式私有字段进入 `format_info.osgb`；一套倾斜摄影场景的 `metadata.xml`、`Data/`、SRS 和纹理摘要进入 `format_info.osgb_scene`；`.gltf` manifest 的 asset、scene / buffer / image / accessor 计数、extensions 摘要和外部资源数量进入 `format_info.gltf`，本地资源路径只通过 `item.refs` 表达；OBJ 的 object / group / material library 摘要进入 `format_info.obj`；STL 的 ASCII / binary 编码和三角面摘要进入 `format_info.stl`；FBX 的 binary / ASCII header 编码事实进入 `format_info.fbx`；IFC 的 STEP schema identifiers、schema version、entity count 和 entity type counts 进入 `format_info.ifc`。格式原生字段、构件属性集、tileset 细节、纹理清单、BIM family / level / property set 等进入受控 `format_info.<format>`；空间参考、地理定位和空间范围进入 `capabilities.spatial`。模型原始内容、前端渲染协议、转换产物、缩略图、瓦片或构件查询结果不得写入 `type_info.model_3d`。

S3M 同样使用 `data_type=model_3d`。SCP 的 XML / JSON 编码、S3M 版本、根瓦片数、瓦片扩展名、文件类型和位置进入 `format_info.s3m`；`config/scene.scp` 通过 `item.refs` 以 `role=manifest + primary=true` 表达。S3M 前端 renderer、受控资源 URL、加载队列和浏览器纹理能力不得写入 Meta attributes。

`type_info.point_cloud` 只承载点云跨格式稳定结构摘要。`point_cloud_kind` 表达点云子形态，第一版取值为 `raw_point_cloud`、`tiled_point_cloud`、`scan_collection`、`generic`。LAS / LAZ / COPC、PCD、点云型 PLY、EPT / Potree、E57 等都使用 `data_type=point_cloud`；不得仅因点记录可展开为 x/y/z 等列而归为 `table`。点样本、抽稀结果、前端渲染协议、Potree / EPT / COPC 层级内容、派生瓦片等属于内容读取或 Manager 派生产物，不写入 attributes。CRS、空间定位和空间范围进入 `capabilities.spatial`；分类分布、密度等需要读取点记录的分析结果必须先定义点云领域的独立 owner 和结果结构，不得写入 Meta attributes 或借用表格数据剖析结果。

`type_info.gaussian_splat` 只承载高斯泼溅跨格式稳定结构摘要。`representation` 第一版固定使用 `3d_gaussian_splatting`，`splat_count` 表示高斯基元数量，`has_opacity`、`has_scale`、`has_rotation`、`has_spherical_harmonics` 和 `sh_degree` 表示渲染所需属性能力。3DGS PLY、`.splat`、`.ksplat`、`.spz` 等都使用 `data_type=gaussian_splat`；不得因为 PLY 内部使用 vertex 记录就归为 `point_cloud`，也不得因为它能三维渲染就归为 `model_3d`。原始高斯数据、压缩产物、前端渲染协议、排序结果或派生 splat artifact 不写入 `type_info.gaussian_splat`；格式私有 header、属性列表和 layout 写入 `format_info.<format>`。

表字段统一放在 `type_info.table.fields`，不得写入 attributes 顶层。`type_info.table` 是 `common/datatype.TableInfo` 的直接 JSON payload，`type_info.table.fields[]` 是 `common/datatype.FieldInfo` 的直接 JSON payload。字段不是 data item，字段类型只能使用 `type` 表达 ADDP 标准字段类型，不得在字段对象内写入 `data_type`。Engine / Format Provider 必须在返回 `FieldInfo` 前完成原生类型到 ADDP 标准字段类型的映射；跨过 Provider 边界后，Meta、Manager、Transfer、Quality、Model 等模块只能依据 `type` 做语义判断，不得重新解析 `native_type`。原生字段类型如需展示，只能作为只读诊断信息写入 `native_type`；哪个字段是空间字段、SRID、extent 等属于 `capabilities.spatial`，不得塞回 `type_info.table`。

`FieldInfo.path` 是可选的结构化可查询字段路径，取值为从记录根开始的非空名称段数组。普通关系表字段使用单段路径或省略；MongoDB collection 等动态 schema 记录集合必须为嵌套字段返回完整路径，并同时保留各中间 object / array 字段事实。例如 `members`、`members.userInfo`、`members.userInfo.nickName` 分别返回对应 `path`，其中 `name` 是当前查询语言可直接使用的规范字段表达。数组字段在样本中存在非空元素时还必须通过 `element_type` 返回采样确认的 ADDP 标准元素类型；元素类型不一致时为 `mixed`，无非空元素可供推断时省略，不得从 `native_type` 重新推断。Provider 不得只返回 `members=array` 后丢弃数组元素结构，也不得把原始样本记录或样本值写入字段事实。路径递归必须设置深度、字段数和数组采样上限，超过上限时停止扩展而不是截断名称或编造类型。

字段属性只有能影响扫描、展示、查询建议、质量检测、传输写入、建模标准化或智能生成中的至少一个决策，才进入 ADDP metadata 链路；仅因引擎能查到而没有明确消费方的原生细节，不进入公共模型。

| 字段属性 | 典型来源 | 语义层级 | 主要消费方 | 作用 | 规范归属 |
| --- | --- | --- | --- | --- | --- |
| `native_type` | 各 SQL/文件格式 provider | 通用基础属性 | Meta、Manager、Transfer、Model、Copilot | 保留原生类型，辅助 schema 展示、Provider 边界映射、同源诊断和代码生成；不得作为跨模块执行决策依据 | `datatype.FieldInfo.NativeType` |
| `nullable` | `information_schema.columns`、`system.columns`、文件格式 schema | 通用结构语义 | Meta、Manager、Quality、Transfer | 展示字段约束，推荐非空质量规则，辅助写入校验 | `FieldInfo.Nullable` |
| `primary_key` | PostgreSQL 约束表、MySQL `column_key`、部分引擎原生 metadata | 通用结构语义 | Meta、Manager、Quality、Model、Standard | 唯一性识别、主键规则推荐、模型字段识别 | `FieldInfo.PrimaryKey`；ClickHouse primary key 偏稀疏索引 / 排序表达式语义，暂不映射为 ADDP 通用主键 |
| `comment` | `col_description`、`column_comment`、`system.columns.comment` | 通用描述语义 | Meta、Manager、Model、Standard、Copilot | 字段理解、数据元匹配、智能生成上下文 | `FieldInfo.Comment` |
| `default_expression` | SQL 默认值、ClickHouse `default_expression` | 半通用结构语义 | Manager、Transfer、Model、Copilot | schema 还原、写入避让、生成建表语句 | `FieldInfo.DefaultExpression`；ClickHouse 仅在 `default_kind=DEFAULT` 时写入 |
| `generated_expression` | SQL generated column、ClickHouse `MATERIALIZED` / `ALIAS` | 半通用生成列语义 | Manager、Transfer、Model、Copilot | 避免直接写入生成列，解释字段生成方式 | `FieldInfo.Generated` + `FieldInfo.GenerationExpression`；不保留 `default_kind` 原生枚举 |
| `partition_key` | ClickHouse、Spark/Hive、分区表 metadata | 半通用布局语义 | Manager、Develop、Transfer、Monitor、Copilot | 查询过滤建议、写入分区提示、性能诊断 | 暂不进入 `FieldInfo`；后续如需展示，应先统一跨引擎语义并定义受控 `capabilities.partitioning` 或 native 归属 |
| `sorting_key` | ClickHouse `system.columns` / 表定义 | 引擎原生优化语义 | Develop、Monitor、Copilot | 查询条件和排序建议，解释 ClickHouse 表性能特征 | 暂不进入 `FieldInfo`；需要消费前先定义受控 native 字段归属 |
| `codec` | ClickHouse `compression_codec` | 引擎原生存储语义 | Manager、Monitor | 存储诊断、压缩策略展示 | 暂放受控 native；无明确消费前不展示为通用字段 |
| `ttl` | ClickHouse TTL metadata | 引擎原生生命周期语义 | Manager、Monitor、Governance | 生命周期展示、过期策略诊断 | 暂放受控 native；需确认治理模块消费方式 |

MongoDB collection 等动态 schema 记录集合在当前 ADDP 能力中按记录集合消费，`meta_item.item_type=collection`，`attributes.item.data_type=table`。其 `type_info.table.fields` 是 Provider 对受限样本递归推断得到的字段结构和可查询字段路径，不是强 schema，也不是 Manager 数据剖析结果；Manager 预览、Meta 扫描和 Copilot 资源事实必须消费同一 Provider 字段结构，不得分别从不同数量的展示行重复推断第二套 schema。精确记录数写入 `type_info.table.row_count`，只有估算来源时写入 `type_info.table.estimated_row_count`，不得写入 `type_info.document` 或新增 `type_info.collection`。

`type_info.table.row_count` 只表示精确行数，0 是有效值；`type_info.table.estimated_row_count` 只表示 catalog / system statistics、格式结构元数据或有限分析提供的近似值。两者可以同时存在，但不得用估算值填充 `row_count`。`meta_item.row_count` 是精确 `type_info.table.row_count` 的列表查询投影，不投影估算值。估算值不得用于分页终点、完整性校验、传输完成判断或其他需要精确基数的决策。

索引、采样过程、动态 schema 推断方式等不是 `common/datatype.TableInfo` 当前通用字段，不得写入 `type_info.table`。动态 schema 记录集合、数据库或格式解析得到的索引摘要进入 `capabilities.indexing`；扫描或结构推断过程的采样规模、是否采样、dynamic schema 类型、平均记录大小、索引数量等紧凑过程事实进入 `capabilities.statistics`。字段空值率、基数、值域、分位数、直方图和高频值等数据剖析结果归 Manager，不进入 Meta attributes。

Meta attributes 不维护旧字段兼容层。字段可空性只写 `nullable`，字段主键标记只写 `primary_key`；不得再写 `is_nullable`、`is_primary_key`。历史 Meta 数据如果不符合本规范，应删除后重新扫描，不在运行期做迁移或兼容读取。

`type_info.table.native` 承载表级来源原生事实，例如 CSV 分隔符、Shapefile shape type、Excel 当前 sheet 名称和序号、Parquet 分区列、数据库表引擎或原生表类型。`native` 是单层结构，来源由 item 的 `format` 或 `engine_type` 决定；具体 key 必须由对应 format / engine 白名单约束。文件、容器、资源整体或格式解析层事实仍写入 `format_info.<format>`，例如 Excel 工作簿 sheet 数、默认 sheet、Parquet scope 文件清单、ZIP entry 统计等。

## format_info 命名空间

格式私有或第三方扩展必须进入合规命名空间，例如：

- `format_info.csv`
- `format_info.shapefile`
- `format_info.json`
- `format_info.sqlite`
- `format_info.tiff`
- `format_info.com.vendor.plugin_name`

`format_info.unqualified` 是 normalizer 的隔离区，不是业务语义命名空间。正常的新 detector、format provider 或 reader 不应主动写入 `unqualified`。平台级行为不得依赖 `unqualified`。

### format_info.tiff

`format_info.tiff` 表达 TIFF / GeoTIFF 格式层事实。GeoTIFF 不新增基础 `format`，仍写 `attributes.item.format=tiff`；COG 是 TIFF 的 profile，不写成新的基础格式。

建议最小结构：

```json
{
  "profile": "geotiff",
  "is_cloud_optimized": false,
  "is_tiled": true,
  "has_overviews": true,
  "big_tiff": false,
  "compression": "DEFLATE",
  "photometric_interpretation": "RGB",
  "sample_format": "uint16",
  "nodata": -32768,
  "sample_min": -49,
  "sample_max": 406,
  "display_min": -49,
  "display_max": 406,
  "display_range_method": "metadata_statistics"
}
```

字段规则：

| 字段 | 规则 |
|---|---|
| `profile` | 取值为 `plain_tiff`、`geotiff`、`cog`、`unknown`。只表示格式 profile，不改变 `attributes.item.format=tiff`。 |
| `is_cloud_optimized` | Go / Meta 轻量判断结果。第一阶段只作为 profile hint，不表示完整 COG 合规验证。无法判断时省略，不得硬置为 `false`。 |
| `is_tiled` / `has_overviews` / `big_tiff` | 从 TIFF tag / IFD / overview 信息可轻量读取时写入。 |
| `compression` / `photometric_interpretation` / `sample_format` | 格式私有事实，用于展示和默认渲染判断。 |
| `nodata` | 从 TIFF / GDAL metadata 可轻量读取时写入。 |
| `sample_min` / `sample_max` | 来自 TIFF sample value tag 或已有 metadata 的样本范围。 |
| `display_min` / `display_max` | Manager / Frontend 默认渲染可直接消费的建议显示范围。 |
| `display_range_method` | 显示范围来源，例如 `metadata_statistics`、`sample_value_tags`。Meta scan 不得为了该字段全量扫描大 TIFF 像素；前端运行时采样结果只能作为本次渲染兜底或 Manager COG 结果补充事实，不反写源 item attributes。 |

TIFF / GeoTIFF 的 CRS、extent、transform 等跨格式空间事实写入 `capabilities.spatial`，不写入 `format_info.tiff` 作为唯一消费来源。`format_info.tiff` 可保留原始 tag 摘要，但平台地图定位必须消费 `capabilities.spatial`。

### format_info.raster_mosaic

`format_info.raster_mosaic` 表达栅格镶嵌数据集的格式层事实。Raster mosaic 是 `data_type=media`、`layout=whole`、`format=raster_mosaic` 的 whole-scope 数据集，不是单个 TIFF / COG。

建议最小结构：

```json
{
  "manifest_ref": "mosaic.addp.json",
  "manifest_version": 1,
  "leaf_count": 2360,
  "leaf_storage": "referenced_and_generated_cog",
  "index_ref": "index/source-index.json",
  "overview_ref": "overviews/overview.cog.tif",
  "overview_profile": "cog",
  "tile_cache_ref": "tiles/",
  "default_style_ref": "styles/default.json",
  "stats_ref": "stats/band-1.json",
  "cog_validation": {
    "method": "gdal_cog_layout",
    "validated_count": 2360,
    "generated_count": 480,
    "referenced_count": 1880,
    "unknown_count": 0
  }
}
```

字段规则：

| 字段 | 规则 |
|---|---|
| `manifest_ref` | manifest 在 whole scope 内的相对路径，通常为 `mosaic.addp.json`。 |
| `manifest_version` | ADDP raster mosaic manifest schema 版本。 |
| `leaf_count` | mosaic leaf 数量。leaf 是 index 中可被预览读取的 COG，不等于源 TIFF 文件数。 |
| `leaf_storage` | leaf COG 组织方式，例如 `referenced_cog`、`generated_cog`、`referenced_and_generated_cog`。 |
| `index_ref` | mosaic source index 相对路径。index 记录 leaf COG、源 locator、fingerprint、extent、分辨率、优先级、NoData 和 COG 校验摘要。 |
| `overview_ref` | 全局低分辨率 overview COG 相对路径。overview 必须是 COG，但不是全分辨率全局 COG。 |
| `overview_profile` | overview 格式 profile，当前固定为 `cog`。 |
| `tile_cache_ref` | 可选低层级瓦片缓存根路径。没有预生成瓦片时省略。 |
| `default_style_ref` | 可选默认渲染样式相对路径。 |
| `stats_ref` | 可选统计、直方图、显示范围等摘要路径。 |
| `cog_validation` | COG 内容级校验摘要。不得只根据后缀或文件名判定 COG。 |

Raster mosaic 的 CRS、extent、分辨率范围等跨格式空间事实写入 `capabilities.spatial`。`format_info.raster_mosaic` 只保存 manifest、index、overview、leaf 组织和 COG 校验摘要等格式私有事实。

### format_info.pmtiles

`format_info.pmtiles` 表达 PMTiles v3 单文件归档的格式层事实，受控字段为：

- `spec_version`：固定为 `3`。
- `tile_type`：当前固定为 `mvt`。
- `tile_compression`：当前固定为 `gzip`。
- `internal_compression`：PMTiles 目录和 metadata 的压缩方式。
- `min_zoom` / `max_zoom`：header 声明的可用层级范围。
- `addressed_tiles_count` / `tile_entries_count` / `tile_contents_count`：header 计数事实。
- `clustered`：归档是否按 tile id 聚簇。
- `center`：header 中的 `[longitude, latitude, zoom]`。

WGS84 bounds、SRID 和 CRS 属于 `capabilities.spatial`，不重复写入 `format_info.pmtiles`。不得把 PMTiles 目录项、单瓦片字节、存储凭据、Manager `storage_ref` 或 Service URL 写入 attributes。

## capabilities 命名空间

标准横切能力包括：

| 命名空间 | 含义 | 典型字段 |
|---|---|---|
| `spatial` | 空间能力 | geometry_columns、primary_geometry_column、extent、has_spatial_index |
| `temporal` | 时间能力 | time_columns、time_range、granularity、timezone |
| `statistics` | 扫描统计与采样事实 | sample_size、is_sampled、schema_type、index_count、avg_record_size |
| `extraction` | 内容提取 | extractor_available、text_extracted、status、reason、extractor、text_truncated、summary |
| `semantic` | 语义能力 | embedding_model、vector_index_ref、semantic_tags |
| `constraints` | 关系表命名约束事实 | constraints（primary_key、unique、foreign_key） |
| `partitioning` | 分区事实 | strategy、key_fields、subpartition_strategy、subpartition_key_fields、partition_count |
| `indexing` | 索引能力 | indexes、spatial_indexes、fulltext_indexes、vector_indexes |

横切能力不应变成顶层数据类型，也不应被塞进具体格式信息。

`capabilities.indexing.indexes[]` 使用 `name`、`fields`、`is_unique`、`index_type`；字段顺序必须保持来源索引列顺序。`capabilities.constraints.constraints[]` 使用 `name`、`constraint_type`、`fields`，外键额外使用 `referenced_namespace`、`referenced_table`、`referenced_fields`；`constraint_type` 只允许 `primary_key`、`unique`、`foreign_key`。`capabilities.partitioning` 使用受控的 `strategy`、`key_fields`、`subpartition_strategy`、`subpartition_key_fields`、`partition_count`。这些结构直接来自 `EngineCatalogFacts` 的强类型事实，Meta 不解析供应商原生表达式进行补推。

`capabilities.statistics` 只保存 Meta scan、catalog、system table、格式头或结构推断过程获得的紧凑统计与采样事实。它不保存 Manager data profile，也不得新增字段级 `fields`、`null_count`、`distinct_count`、`min`、`max`、`quantiles`、`histogram`、`top_values`、`profiled_at` 或 `profile_ref` 来旁路承载剖析结果。Manager 数据剖析的当前结果、字段分布和执行历史分别归 Manager 私有结果表与 `common.task_executions`。

`capabilities.extraction` 只记录提取过程状态和外部结果引用，不保存正文、正文预览、OCR 全文、字幕全文或 embedding。字段语义：

| 字段 | 语义 |
|---|---|
| `extractor_available` | 当前 format / item 是否存在可调用的后端提取器或 reader |
| `text_extracted` | 文档正文是否已成功抽取；只用于正文抽取状态 |
| `status` | 提取状态，例如 `completed`、`unsupported`、`failed` |
| `reason` | `unsupported` 或 `failed` 的稳定原因码，例如 `document_text_reader_unavailable` |
| `extractor` | 实际使用的提取器标识，例如 `common_format:docx` |
| `text_truncated` | 正文抽取是否因限制被截断 |

正文不属于技术元数据。`plain_text_preview`、`text_excerpt` 等正文派生字段不得写入 attributes，也不得由 Manager 从历史快照兼容读取。受控样本只能由 Security 对显式纳管的 data item 按 fingerprint 调用 Meta runtime 即时读取，不持久化到 Meta attributes。

`metadata_extracted` 不是标准 attributes 字段。deep scan 是否完成应看 `meta_item.scanned_depth`、`scan_status` 或 scan run 结果；Manager 不应通过 attributes 判断是否需要补齐元数据。

### capabilities.spatial 最小结构

`capabilities.spatial` 表达跨格式稳定消费的空间能力，不负责完整空间分析。meta 扫描阶段只写能够从字段声明、格式头或轻量元数据中确定的信息，不为了推断实际几何类型而扫描全量数据。

字段型空间对象建议最小结构：

```json
{
  "geometry_columns": [
    {
      "name": "geometry",
      "geometry_type": "Geometry",
      "srid": 4326,
      "crs_ref": "EPSG:4326",
      "dimension": 2,
      "nullable": false
    }
  ],
  "crs_definitions": [
    {
      "id": "EPSG:4326",
      "definition_encoding": "wkt",
      "definition": "GEOGCS[...]",
      "source": "postgis_spatial_ref_sys"
    }
  ],
  "primary_geometry_column": "geometry",
  "extent": null,
  "has_spatial_index": false
}
```

非字段型空间对象，例如 GeoTIFF、栅格覆盖、带 GPS 或整体空间参考的媒体对象，不要求写入 `geometry_columns`，可以只写对象整体空间参考：

```json
{
  "srid": 4326,
  "crs_ref": "EPSG:4326",
  "extent": [120.0, 30.0, 121.0, 31.0],
  "has_spatial_index": false
}
```

字段规则：

| 字段 | 规则 |
|---|---|
| `geometry_columns` | 支持多个 Geometry 字段。每个元素描述一个空间字段；非字段型空间对象不得虚构字段名，可省略该字段。 |
| `geometry_type` | 写入标准 `GeometryType` canonical 值，例如 `Geometry`、`Point`、`MultiPolygon`。PostGIS 字段声明为 `geometry` 时就写 `Geometry`，不得为了得到 Point/Polygon 等具体类型扫描全表。 |
| `srid` | 能确定 EPSG/SRID 编号时写数字，例如 `4326`。字段型空间对象优先写在对应 `geometry_columns[]` 内；非字段型空间对象写在 `capabilities.spatial.srid` 顶层。 |
| `crs_ref` | 当前空间对象或几何字段引用的 CRS ID。能确定 EPSG 时写 `EPSG:<code>`；不能确定 EPSG 但能获得定义文本时写 `ADDP:CRS:<sha256>`。字段型空间对象优先写在对应 `geometry_columns[]` 内；非字段型空间对象写在顶层。 |
| `crs_definitions` | CRS 定义集合。只允许写在 `capabilities.spatial` 顶层，字段通过 `crs_ref` 引用，不得在各字段内重复写定义文本。 |
| `crs_definitions[].id` | CRS 定义 ID，必须被 `crs_ref` 引用。能确定 EPSG 时使用 `EPSG:<code>`；否则使用 `ADDP:CRS:<sha256>`。 |
| `crs_definitions[].definition_encoding` | CRS 定义表达方式。只允许 `wkt`、`esri_wkt`、`proj4`、`projjson`。消费者必须按自身能力选择可处理的表达，不得因为前端不能直接注册某种表达而从平台空间事实中删除它。 |
| `crs_definitions[].definition` | CRS 定义文本，例如 PostGIS `spatial_ref_sys.srtext`、Shapefile `.prj`、GeoPackage `gpkg_spatial_ref_sys.definition`、proj4 字符串或 GeoParquet `crs` 的 PROJJSON 文本。 |
| `crs_definitions[].source` | CRS 定义来源枚举，例如 `postgis_spatial_ref_sys`、`mysql_st_spatial_reference_systems`、`sidecar_prj`、`geopackage_srs`、`geotiff_tags`、`geoparquet_metadata`。`crs_normalization_runtime` 只用于 execution-local CRS 定义转换结果，不得反写覆盖 Meta 保存的源定义事实。 |
| `dimension` | 坐标维度，无法确定时可省略。 |
| `nullable` | 字段是否可空，无法确定时可省略。 |
| `primary_geometry_column` | Manager 默认空间预览使用的几何字段。多几何字段时必须明确；单几何字段时建议写入；没有字段列概念的空间媒体应省略。 |
| `extent` | 空间范围，只记录当前空间对象或 primary geometry column 原生 CRS 下的事实。无法轻量获得时写 `null` 或省略，不得为了 extent 扫描全量数据，也不得为了底图、MVT 或普通预览把 extent 转成其他 CRS 后写入 `capabilities.spatial`。 |
| `has_spatial_index` | 是否存在空间索引；无法确定时可省略。 |

`feature_count` 不属于 spatial，表格型行数写入 `type_info.table.row_count`。Manager 数据剖析采样得到的实际几何类型分布只进入 Manager-owned profile result，不反向覆盖 Meta scan 阶段的 `geometry_type`，也不写入 `capabilities.statistics`。

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

空间能力消费规则：

- `capabilities.spatial.srid`、`geometry_columns[].srid`、`crs_ref`、`crs_definitions`、`extent` 是空间事实，不表示 ADDP 核心后端具备通用 CRS transform 能力。
- `capabilities.spatial.extent` 必须与当前空间对象或 primary geometry column 的原生 CRS 一致；平台标准 attributes 不支持在 `capabilities.spatial` 内记录与源 CRS 不一致的派生 extent。
- Manager 普通空间预览只返回源坐标 geometry 表达和 CRS 元数据；不得为了普通预览隐式调用后端 PROJ 或 PostGIS `ST_Transform` 转成 WGS84。
- `srid=0` 且 `crs_ref` / `crs_definitions` 缺失时必须按 `unknown_crs` 处理，不得默认解释为 `EPSG:4326`。如果 `srid=0` 但存在有效 `crs_ref` 和 CRS 定义，表示“无数字 SRID 但 CRS 已知”。
- 如果某条路径已经由具体引擎能力完成转换，例如 MVT / 矢量物化视图，应在该路径响应中明确 `target_srid`、`transform_status=engine_transformed` 和 `transform_engine`；该事实不得反向改写源数据的 `capabilities.spatial`。
