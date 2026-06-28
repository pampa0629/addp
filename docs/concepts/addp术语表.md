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
| model_3d | 三维模型数据 | 以三维空间对象、场景、网格、构件或三维可视化结构为核心消费对象的数据类型。 | 覆盖 GLB / glTF、单 OSGB、OSGB Scene 倾斜摄影、3D Tiles 场景、IFC / Revit BIM 等；具体子形态由 `type_info.model_3d.model_kind`、format、layout 和 capabilities 表达。 |
| point_cloud | 点云数据 | 以三维点集合、点属性、空间范围和抽样 / LOD 预览为核心消费对象的数据类型。 | 覆盖 LAS / LAZ / COPC、PCD、点云型 PLY、EPT / Potree 等；点属性不是普通表字段，不能仅因可列化而归为 `table`。 |
| detector | 探测器 / 探测 | 从资源候选集合中识别数据项边界、数据类型和文件格式的过程或组件。 | 归属 Meta 模块。 |
| format identity | 格式身份 | 平台静态注册的格式定义。 | 回答“这个格式是谁、如何识别、默认属于什么 data type 和 layout”。 |
| format detection | 格式探测 | 对给定资源动态判断其文件格式的过程。 | 回答“当前资源像什么格式”。 |
| FormatDescriptor | 格式描述符 | 一个 format 的静态事实源。 | 只描述格式身份、识别规则、默认 data type 和 layout；不声明 Go provider / reader / writer 可用性。 |
| FormatPlugin | 格式插件 | 一个文件格式包在 `common/format` 中的最小代码身份入口。 | 至少实现 `Format()`；可按需同时实现 `FormatDescriptorProvider`、info provider、content reader 或 writer provider。 |
| FormatCapabilitySnapshot | 格式能力诊断快照 | 由 `FormatDescriptor` 和当前进程已注册 plugin 的接口断言临时派生的诊断视图。 | 仅用于展示或测试诊断，不是事实源；业务调用方应直接查询具体 provider / reader。 |

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
| scan target | 扫描目标 | 本次扫描作用的对象范围。 | 身份型目标只抽象为 engine、node、item；`catalog_paths`、`ref_groups` 是 selector/scope 输入形态。 |
| trigger type | 触发方式 | 扫描由手动还是定时触发。 | 概念层只区分 `manual` / `scheduled`。 |
| scan source | 扫描来源 | 触发扫描的模块标记。 | 例如 `meta`、`manager`、`system`、`transfer`；不进入 `trigger_type` 枚举，也不表达调度器或前后端通道。 |
| ScanSelector | 扫描选择器 | API 层或模块调用方提交的扫描选择信息。 | 可包含 `engine_id`、`node_id`、`item_id`、`targets`、`catalog_paths`、`ref_groups` 等输入形态。 |
| ScanScope | 扫描范围 | Meta 内部扫描主链路消费的唯一范围模型。 | 所有扫描选择器进入主链路前必须先解析为 ScanScope。 |
| ScanTask | 扫描任务定义 | Meta 中“未来应该按什么计划扫描什么范围”的定义态。 | 保存 scope、schedule、enabled、owner、最近执行摘要等；不保存每次执行历史。 |
| scan schedule inheritance | 扫描调度继承 | Meta 中粗粒度 ScanTask 为下级范围提供默认定时计划的关系。 | engine 级 ScanTask 可以作为默认计划；下级范围没有独立调度时继承该计划。 |
| independent scan schedule | 独立扫描调度 | 更具体扫描范围上单独启用的 ScanTask。 | node / catalog path 等下级范围启用独立调度后，应从上级定时扫描范围中排除。 |
| TaskExecution | 执行记录 | 某一次任务实际执行的运行态记录。 | 统一存储在 `common.task_executions`；Meta 扫描执行通过 `source_task_id` 关联 ScanTask。 |
| task owner module | 任务绑定模块 | 任务绑定对象所属的模块。 | 字段建议为 `owner_module`；不同于 execution `source`。 |
| owner ref | 任务绑定引用 | 任务定义在绑定模块中的稳定引用。 | 例如 System engine 自动扫描任务可使用 `owner_ref=engine:{engine_id}`。 |
| planned run time | 计划触发时间 | scheduled execution 对应的计划触发时间点。 | 字段建议为 `planned_run_at`；用于 `task_id + planned_run_at` 幂等触发。 |
| ref group | 内容引用组 | 一组共同参与 data item 识别的内容引用边界。 | 用于表达 Shapefile 等 multi content 的本次可见 refs；不绑定 ADDP locator，不是 `catalog_paths`。 |

## 任务与执行

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| Task | 任务 | 可被执行的业务能力抽象。 | Task 是抽象概念，不是统一任务总表；任务定义归 owner 模块私有表。 |
| task definition | 任务定义 | “未来应该按什么策略处理什么对象”的定义态。 | 例如 `meta.scan_tasks`、`transfer.transfer_tasks`、`manager.tile_cache_tasks`。 |
| owner task schedule | 任务自身调度 | owner 模块任务定义上保存并由 owner scheduler 触发的独立定时计划。 | 只决定该任务作为独立任务何时自动执行。 |
| orchestration schedule | 编排调度 | Orchestrator 编排定义上保存的定时计划。 | 只决定编排 run 何时启动；不继承、不覆盖其中 Step 引用任务的自身调度。 |
| task type | 任务类型 | owner 模块内稳定的任务类型标识。 | 例如 `scan`、`tile_cache_generation`、`embedding`；由 TaskProvider capabilities 声明。 |
| TaskProvider | 任务提供者 | 模块对外声明可编排任务能力的角色。 | 按模块注册，不按任务类型注册；一个 provider 在 `task_capabilities[]` 中声明多个任务类型能力。 |
| source task id | 来源任务 ID | execution 关联的 owner 模块任务定义 ID。 | 在 `common.task_executions.source_task_id` 中保存；查询时必须结合 `module + task_type`。 |
| parent execution id | 父执行 ID | 当前 execution 的父级 execution UUID。 | 用于 Orchestrator 子步骤追踪父编排。 |
| ad-hoc execution | 一次性执行 | 不依赖持久任务定义、直接按本次配置创建的 execution。 | 可以没有 `source_task_id`，但必须在 `execution_config` 保存完整执行配置。 |
| artifact state | 产物状态 | 描述派生产物当前是否可用、在哪里、由什么配置生成的状态对象。 | 例如瓦片缓存产物、embedding vectors；不是 execution。 |

## Cleanup 与生命周期

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| cleanup | 系统级资源回收 | ADDP 中面向源事实、派生产物、物理产物、运行时缓存和任务定义残留的系统级资源回收与生命周期治理体系。 | 不是单一模块的失效数据删除；规范见 `docs/spec/addp-cleanup体系规范.md`。 |
| cleanup coordinator | 资源回收协调方 | 发起 cleanup request（资源回收请求）、记录任务元信息、汇总模块结果并展示审计的角色。 | 当前由 System 承担；不执行模块私有资源回收。 |
| cleanup executor | 资源回收执行方 | 评估和回收本模块 owner 范围内资源，并写回 cleanup result（资源回收结果）的角色。 | Meta、Manager、Transfer 等模块按 owner 范围分别承担。 |
| cleanup request | 资源回收请求 | 资源回收协调方发布的中性资源回收请求。 | 不携带模块私有表结构、bucket prefix 或物理删除规则。 |
| cleanup result | 资源回收结果 | 资源回收执行方写回的模块级资源回收结果。 | 包含通用摘要和模块私有统计。 |
| owner module | 归属模块 | 某个事实、产物、任务定义或物理资源生命周期归属的模块。 | cleanup 责任跟随 owner module，不跟随触发事件来源。 |
| physical artifact | 物理产物 | 可删除的实际存储资源。 | 例如对象存储 key、PG 派生对象、向量行、缓存 key。 |
| lifecycle event | 生命周期事件 | 表示 engine、tenant、item 或配置发生生命周期变化的中性事件。 | 各模块独立消费并处理自身 owner 范围内资源。 |

## 能力与读取

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| info provider | 信息提供者 | 读取 data type info 或 format info 的能力。 | 只提供元数据，不提供内容数据。 |
| content reader | 内容读取器 | 按数据类型或格式读取内容数据的能力。 | 例如表格样本、文档文本片段、缩略图、原始内容。 |
| full-text index | 全文索引 | 面向关键词检索的外部搜索索引。 | 例如 Meilisearch 中的资产记录；与 `access_index` 不同，不用于 range read 或表格分页定位。 |
| index ref | 索引引用 | attributes 中指向外部索引记录的引用。 | 文档正文抽取后的全文索引引用写入 `capabilities.extraction.index_ref`，例如 `meilisearch:assets:<item_fingerprint>`；引用的是 item 指纹对应记录，不是 `content_hash`。 |
| capability | 能力 | 引擎、当前进程格式实现或数据项呈现的能力。 | engine capability、format descriptor / provider status、item capability 含义不同。 |
| spatial | 空间能力 | 描述空间字段、CRS、范围、几何类型、空间索引等横切语义。 | 是横切能力，不是 data type。 |
| quick view | 快显 | Manager 空间预览中的高性能地图浏览模式。 | 快显是 UI 能力，不是任务，也不是瓦片缓存产物。 |
| quick view preference | 快显偏好 | Manager 中记录某个空间 item 的用户预览模式偏好。 | 目标落点为 `manager.quick_view`；快显能力、推荐结果和不可用原因由能力 API 动态合成。 |
| tile cache | 瓦片缓存 | 面向地图浏览生成并保存的一组瓦片数据。 | 格式可以是 MVT、栅格瓦片、预渲染图片瓦片或后续扩展格式。 |
| tile cache result | 瓦片缓存结果 | 某个 data item 生成后的瓦片缓存结果状态。 | 目标落点为 `manager.tile_cache`；属于结果状态，不是 execution。 |
| tile cache generation task | 瓦片缓存生成任务 | Manager 中可执行、可调度、可编排的瓦片缓存生成任务定义。 | 后续目标落点为 `manager.tile_cache_tasks`，TaskProvider `task_type=tile_cache_generation`。 |
| storage ref | 存储引用 | 指向外部或内部存储位置的稳定引用。 | 上层逻辑消费存储引用，不应硬编码 bucket、prefix 或对象路径规则。 |
| contentio.Ref | 内容引用 | 一个已确定 content 的定位器，不携带凭据。 | 需要多个 content 时使用 refs 数组。 |
| contentio.Reader | 内容读取器 | 按内容引用打开单个 content 并读取轻量状态的统一抽象。 | 由编排层基于 engine capability 构造。 |
| contentio.Lister | 内容列举器 | 按 scope 引用列举子 content 的可选抽象。 | scope / 目录型格式按需使用。 |
| contentio.Stat | 内容状态 | 单 content 的轻量状态。 | 用于快速判断可读性、大小、修改时间等基础事实。 |
| format.RelatedRef | 相关引用 | 多 content 格式中“内容引用 + 集合标注”的单项。 | `Ref` 负责定位；`Required`、`Primary` 等描述它在集合中的约束和主次。 |
| []format.RelatedRef | 相关引用集合 | 多 content 格式的显式相关引用列表。 | 例如 Shapefile 的 `.shp/.shx/.dbf/.prj`；不是独立 reader。 |
| NativeCursor | 原生游标 | 面向数据库表、动态 schema 记录集合、图查询等引擎原生批量读取的抽象。 | 通常不经过文件格式解码。 |

## 检索与向量化

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| vectorization | 向量化 | 对可支持的数据项生成向量表示的派生能力。 | 服务 Manager 数据检索；资源树触发的是一次性 execution，独立页面创建的配置才是任务定义。 |
| embedding result | 向量化结果 | 某个 data item 当前留下的向量表示及其状态。 | 当前实现对应 `manager.embeddings`；属于 artifact state，不是 execution。 |
| embedding task | 向量化任务 | Manager 中可重复执行、可调度、可编排的向量化任务定义。 | 当前实现对应 `manager.embedding_tasks`，TaskProvider `task_type=embedding`。 |

## 命名约定

1. 概念层统一使用“数据项”表达平台核心对象；只有讨论落库模型、Meta 代码或历史实现时才使用 `meta item`。
2. `data type` 翻译为“数据类型”，`format` 翻译为“文件格式”或“格式”。当上下文已经明确在文件格式体系中，可简称“格式”。
3. `detector` 翻译为“探测器”或“探测”。规范文件名使用“数据项探测器规范”。
4. `provider` 用于 info provider；`reader` 用于 content reader。新文档和新接口不再把内容读取能力统称为 provider。
5. `spatial`、`temporal`、`statistics`、`extraction`、`semantic`、`partitioning`、`indexing` 等是横切能力，不新增为基础数据类型。
6. 扫描深度统一使用 `scan_depth`，已完成深度统一使用 `scanned_depth`。不再使用 `scan_level`、`deep state`、`refresh_policy`、`if_stale` 等额外术语。
7. 面向最终用户的 UI 使用引擎自己的原生术语，例如 `Schema`、数据库、`Bucket`、目录、`Collection`；不得展示 `catalog root`、`catalog node`、`meta node`、`meta item` 等内部术语。
8. 靠近 Engine / Plugin / Manager 探查入口的内部契约使用 `catalog` 体系，例如 `CatalogRoot`、`CatalogEntry`、`CatalogPath`、`catalog_paths`。`catalog` 表达引擎原生目录和可枚举层级，不应被泛化为 `resource`。
9. 靠近 Meta 存储和扫描结果的内部契约使用 `node` / `item` 体系，例如 `meta_node`、`meta_item`、`node_id`、`item_id`。`node` 表达 Meta 树结构，`item` 表达已识别数据项，不应与 engine-side `catalog entry` 混用。
10. `resource` 只作为 UI 或资源树展示语境中的宽泛称呼使用。Meta 模块、Engine 插件接口和跨模块 API 不应新增 `resource_*` 字段来替代已有 `catalog_*`、`node_*` 或 `item_*` 术语。
