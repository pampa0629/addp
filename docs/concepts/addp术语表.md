# ADDP 术语表

本文统一 ADDP 文档中的基础术语。概念层文档优先使用中文术语，必要时在首次出现时标注英文。

## 资源与数据项

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| engine | 引擎 | ADDP 连接和访问外部数据系统的能力入口。 | 例如 PostgreSQL、MinIO、NFS、Neo4j。 |
| node | 资源节点 | 引擎内用于组织资源树的节点。 | 例如目录、bucket、prefix、schema。node 不等同于 data item。 |
| resource tree | 资源树 | 以树形方式展示 engine 内 node 和 data item 的视图。 | 用于浏览、展开、刷新和定位；不是新的身份层。 |
| resource tree search | 资源树搜索 | 在资源树视图内按名称、路径或轻量展示信息定位 node / data item 的浏览辅助能力。 | 不等同于全文检索或语义检索。 |
| resource | 资源 | 引擎 catalog 或资源树语境下的外部对象统称。 | 当讨论内容读写边界时优先使用 content / ref，避免把 engine 资源模型带入 format。 |
| CatalogPath | 引擎目录路径 | 引擎 catalog 内的路径坐标。 | 不等同于 ResourceLocator，也不等同于 Meta `full_name`。 |
| CatalogEntry | 引擎目录条目 | `CatalogProvider.ListChildren` 返回的 catalog 列表项，不是“入口”。 | 回答“当前位置下面有什么、结构上怎么走”；通过 `role=branch/leaf` 表达结构角色；列表摘要使用 `Table`、`Storage`、`LeafCount` 等显式字段，不保留兜底 attributes / stats。 |
| CatalogRoot | 引擎目录根 | 每个引擎 catalog 的显性结构根。 | 通常投影为 root `meta_node`；`full_name=""`，ResourceLocator path 为空，但仍可通过 `node_id` 定位。面向用户展示时使用引擎实例名称，不显示内部术语。 |
| CatalogBranch | 引擎目录分支 | 可继续列举子条目的 catalog entry。 | 例如 schema、database、bucket、prefix、directory；通常由 Meta 投影为 `meta_node`。 |
| CatalogLeaf | 引擎目录叶子 | 引擎 catalog 模型中的终点 entry。 | 例如 table、view、object、file、collection、graph；是 data item 候选，但不等于 data item。 |
| CatalogFacts | 引擎目录事实 | Engine 对 catalog entry 直接知道的结构、存储、索引和原生事实详情。 | 回答“这个条目自身有哪些事实”；详情事实必须落在 `Table`、`Graph`、`Storage`、`Indexes` 等显式字段；不保留兜底 attributes / stats。 |
| LeafCount | 叶子数量摘要 | catalog branch 下直接 leaf 条目的数量摘要。 | 只用于低成本列表展示和扫描计划提示；例如 schema 下表数量、database 下 collection 数量。不是递归总量，也不是 Meta item 计数。 |
| content | 内容 | 可被按流读取、写入或 range 读取的底层内容对象。 | 例如文件、对象存储 object、容器 entry；由 `contentio.Ref` 定位。 |
| data item | 数据项 | ADDP 管理、扫描、预览、检索、授权和传输的核心数据对象。 | 概念层统一称为数据项。 |
| meta item | 元数据项 | data item 在元数据模块和数据库中的实现称呼。 | 与 data item 等价；落库实体通常是 `meta_item`。 |
| item_type | 项类型 / 叶子术语 | data item 在所属引擎 catalog / 路径模型中的原生叶子术语。 | 例如 MinIO 为 `object`，NFS 为 `file`，PostgreSQL 为 `table`。它决定资源树路由和展示，不表示内容语义。 |
| full_name | 逻辑全名 / 语义路径 | data item 在引擎内的稳定逻辑路径。 | 例如 `addp/image/开会.jpg`、`public.users`、`neo4j.graph`。它是定位和指纹的基础，但不是 URI。 |
| ResourceLocator | 资源定位符 | 平台统一的资源 URI 定位形式。 | 形如 `addp://engine/{engine_id}/path/{resource_path}?type={type}&node_id={node_id}` 或 `...&item_id={item_id}`；`type` 表达 catalog 术语，`node_id` / `item_id` 表达真实 Meta 身份。 |
| locator | 定位符 | ResourceLocator URI 的简称。 | 检索结果和前端跳转只消费 locator，不再自行拼接。 |
| data retrieval | 数据检索 | 面向 data item 的关键词、全文或语义检索能力。 | 检索命中以 data item 为结果对象；需要回到资源树时通过 locator 定位。 |

## 数据类型与格式

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| data type | 数据类型 | 用户和平台理解 data item 的高层语义类型。 | 比文件格式更高层，例如 `table`、`document`、`media`。 |
| format | 文件格式 | data item 或 content 的编码方式或格式族。 | 例如 `csv`、`parquet`、`pdf`、`shapefile`。 |
| layout | 内容布局 | content 如何组成 data item 的布局维度。 | `format.layouts` 表示格式支持的布局列表，`attributes.item.layout` 表示已识别 item 的布局结果；取值为 `single`、`multi`、`whole`。 |
| detector | 探测器 / 探测 | 从资源候选集合中识别数据项边界、数据类型和文件格式的过程或组件。 | 归属 Meta 模块。 |
| format identity | 格式身份 | 平台静态注册的格式定义。 | 回答“这个格式是谁、能做什么”。 |
| format detection | 格式探测 | 对给定资源动态判断其文件格式的过程。 | 回答“当前资源像什么格式”。 |
| FormatDescriptor | 格式描述符 | 一个 format 的静态事实源。 | 描述格式身份、识别规则、默认 data type、layout、provider hints 和 content readers。 |
| FormatPlugin | 格式插件 | 一个文件格式包在 `common/format` 中的代码入口。 | 承载 descriptor，并可同时实现 info provider、content reader 或 writer provider；不独立声明 capability。 |
| FormatSupportView | 格式支持视图 | 由 `FormatDescriptor` 和当前进程 provider / reader / writer 注册状态派生的只读视图。 | 用于能力发现；不是独立事实源。 |

## 元数据与扫描

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| attributes | 元数据属性 | `meta_item.attributes` 中保存的结构化扩展事实。 | 包含 `storage`、`item`、`type_info`、`format_info`、`access_index`、`capabilities`。 |
| access index | 访问定位索引 | 为高效读取内容窗口而生成的定位索引。 | 标准落点为 `attributes.access_index.<data_type>`；例如 CSV / JSONL 表格的稀疏行号到字节偏移索引。它不是全文索引。 |
| content hash | 内容哈希 | 对原始内容二进制流计算得到的哈希值。 | 标准落点为 `attributes.storage.content_hash`，用于判断内容是否变化；不用于定位外部全文索引记录。 |
| basic scan | 基础扫描 | 快速发现资源树和 data item 身份的低成本扫描。 | 原则上不读取 file/object 内容。 |
| deep scan | 深度扫描 | 补充类型信息、格式信息、访问索引和横切能力的扫描。 | 可以读取内容，但必须遵守 provider / reader 边界和成本控制。 |
| scan depth | 扫描深度 | 本次扫描要达到的深度。 | 请求字段为 `scan_depth`，只允许 `basic` / `deep`。 |
| scanned depth | 已扫深度 | 当前 `meta_node` / `meta_item` 已达到的元数据完整度。 | 落库字段为 `scanned_depth`，取值为 `none` / `basic` / `deep`。 |
| scan status | 扫描状态 | 扫描任务或最近扫描过程的运行状态。 | 表达 `pending`、`running`、`completed`、`failed` 等过程状态，不表达扫描深度。 |
| force scan | 强制扫描 | 不管已有元数据和低成本过时判断，重新扫描并覆盖本次深度对应的元数据。 | 请求字段为 `force`。 |
| scan target | 扫描目标 | 本次扫描作用的对象范围。 | 只抽象为 engine、node、item。 |
| trigger type | 触发方式 | 扫描由手动还是定时触发。 | 概念层只区分 `manual` / `scheduled`。 |

## 能力与读取

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| info provider | 信息提供者 | 读取 data type info 或 format info 的能力。 | 只提供元数据，不提供内容数据。 |
| content reader | 内容读取器 | 按数据类型或格式读取内容数据的能力。 | 例如表格样本、文档文本片段、缩略图、原始内容。 |
| full-text index | 全文索引 | 面向关键词检索的外部搜索索引。 | 例如 Meilisearch 中的资产记录；与 `access_index` 不同，不用于 range read 或表格分页定位。 |
| index ref | 索引引用 | attributes 中指向外部索引记录的引用。 | 文档正文抽取后的全文索引引用写入 `capabilities.extraction.index_ref`，例如 `meilisearch:assets:<item_fingerprint>`；引用的是 item 指纹对应记录，不是 `content_hash`。 |
| capability | 能力 | 引擎、当前进程格式实现或数据项呈现的能力。 | engine capability、format descriptor / provider status、item capability 含义不同。 |
| spatial | 空间能力 | 描述空间字段、CRS、范围、几何类型、空间索引等横切语义。 | 是横切能力，不是 data type。 |
| contentio.Ref | 内容引用 | 一个已确定 content 的定位器，不携带凭据。 | 需要多个 content 时使用 refs 数组。 |
| contentio.Reader | 内容读取器 | 按内容引用打开单个 content 并读取轻量状态的统一抽象。 | 由编排层基于 engine capability 构造。 |
| contentio.Lister | 内容列举器 | 按 scope 引用列举子 content 的可选抽象。 | scope / 目录型格式按需使用。 |
| contentio.Stat | 内容状态 | 单 content 的轻量状态。 | 用于快速判断可读性、大小、修改时间等基础事实。 |
| format.RelatedRef | 相关引用 | 多 content 格式中“内容引用 + 集合标注”的单项。 | `Ref` 负责定位；`Required`、`Primary` 等描述它在集合中的约束和主次。 |
| []format.RelatedRef | 相关引用集合 | 多 content 格式的显式相关引用列表。 | 例如 Shapefile 的 `.shp/.shx/.dbf/.prj`；不是独立 reader。 |
| NativeCursor | 原生游标 | 面向数据库表、动态 schema 记录集合、图查询等引擎原生批量读取的抽象。 | 通常不经过文件格式解码。 |

## 命名约定

1. 概念层统一使用“数据项”表达平台核心对象；只有讨论落库模型、Meta 代码或历史实现时才使用 `meta item`。
2. `data type` 翻译为“数据类型”，`format` 翻译为“文件格式”或“格式”。当上下文已经明确在文件格式体系中，可简称“格式”。
3. `detector` 翻译为“探测器”或“探测”。规范文件名使用“数据项探测器规范”。
4. `provider` 用于 info provider；`reader` 用于 content reader。新文档和新接口不再把内容读取能力统称为 provider。
5. `spatial`、`temporal`、`statistics`、`extraction`、`semantic`、`partitioning`、`indexing` 等是横切能力，不新增为基础数据类型。
6. 扫描深度统一使用 `scan_depth`，已完成深度统一使用 `scanned_depth`。不再使用 `scan_level`、`deep state`、`refresh_policy`、`if_stale` 等额外术语。
