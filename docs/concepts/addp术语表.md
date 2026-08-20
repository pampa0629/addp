# ADDP 术语表

本文统一 ADDP 文档中的基础术语。概念层文档优先使用中文术语，必要时在首次出现时标注英文。

## 资源与数据项

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| engine | 引擎 | ADDP 连接和访问外部数据系统的能力入口。 | 例如 PostgreSQL、MinIO、NFS、Neo4j。 |
| Oracle Engine | Oracle 引擎 | 通过 `engine_type=oracle` 登记的 Oracle 数据库 Engine Instance；普通表 Catalog/查询/读取与基础 Oracle Spatial（`MDSYS.SDO_GEOMETRY`、SpatialInfo、EWKB）能力以 `service_name` 所指服务为连接边界，以 schema/table 为业务路径。 | Oracle CDC 和 ArcGIS SDE 逻辑变化源分别扩展，不因共用 Oracle 连接而合并为同一能力。 |
| File Geodatabase | 文件地理数据库 | ArcGIS `.gdb` 目录承载的多图层矢量容器格式；ADDP 使用 `format=filegdb + layout=whole + data_type=container` 表达，feature class / table 是容器 child。 | 内置开源数据面使用 GDAL OpenFileGDB；普通图层读写不等同 Enterprise Geodatabase、SDE 注册、拓扑或版本化支持。 |
| Microsoft Access Database | Microsoft Access 数据库 | Microsoft Jet / Access `.mdb` 承载的通用数据库容器格式；ADDP 使用 `format=access + layout=single + data_type=container` 表达。 | `.mdb` 后缀和 `application/x-msaccess` MIME 只证明 Access 容器候选，不能证明它是 ArcGIS Personal Geodatabase。 |
| Personal Geodatabase | 个人地理数据库 | Microsoft Access `.mdb` 承载、且经 ArcGIS PGeo 驱动确定性识别的旧 ArcGIS 地理数据库容器格式；ADDP 使用 `format=pgeo + layout=single + data_type=container` 表达。 | Meta 深度扫描从 `access` 候选精化为 `pgeo`；内置开源数据面只允许作为只读 source，通过 GDAL PGeo + unixODBC / MDB Tools 抽取，不提供 `.mdb` 写回。 |
| Engine Instance | 引擎实例 | System 中一条绑定到确定物理端点的引擎登记事实。 | `engine_id` 只标识该实例；物理端点身份不可原地改变，端点变化必须创建新的 Engine Instance。 |
| Engine Runtime Descriptor | 引擎运行时描述 | System 面向受信 Runtime Service Principal 提供的脱敏 Engine Instance 控制面投影。 | 只包含实例身份、生命周期、能力声明和工作流/脚本运行时的 `protocol/host/port`；不包含数据引擎凭据、数据库连接参数或可直接读取业务数据的明文连接。 |
| engine lifecycle state | 引擎生命周期状态 | Engine Instance 当前能否被正常消费或正在退出平台的状态。 | 统一使用 `active`、`disabled`、`deleting`；`deleting` 保留连接只用于删除前 cleanup，不进入正常业务选择。 |
| engine connectivity observation | 引擎连通性观测 | System 对 Engine Instance 最近一次连接检测得到的运行时观测结果。 | 统一使用 `online`、`offline`、`unknown`、`checking`；它是带检测时间和消息的缓存，不改变生命周期，也不等同于持续保持的物理连接。 |
| storage engine binding | 存储引擎绑定 | owner 任务或配置通过标准 ResourceLocator 对某个存储 Engine Instance 的显式引用集合。 | Engine 删除后绑定保持原 ID 并变为不可执行；重绑定由 owner 在用户确认后原子改写 Locator，不按名称或连接信息自动匹配。 |
| external artifact abandonment | 外部产物放弃 | 当外部引擎不可达时，管理员明确接受平台不再删除某个 owner 已登记外部产物，并把后续处置交给外部系统管理员。 | 必须保留对象身份、最后错误、放弃时间和审计；不得伪装成物理删除成功。 |
| node | 资源节点 | 引擎内用于组织资源树的节点。 | 例如目录、bucket、prefix、schema。node 不等同于 data item。 |
| resource tree | 资源树 | 以树形方式展示 engine 内 node 和 data item 的视图。 | 用于浏览、展开、刷新和定位；不是新的身份层。 |
| resource tree search | 资源树搜索 | 在资源树视图内按名称、路径或轻量展示信息定位 node / data item 的浏览辅助能力。 | 不等同于全文检索或语义检索。 |
| resource | 资源 | 引擎 catalog 或资源树语境下的外部对象统称。 | 当讨论内容读写边界时优先使用 content / ref，避免把 engine 资源模型带入 format。 |
| resource concurrency version | 资源并发版本 | 可变持久化主资源用于乐观并发控制的单调递增正整数。 | API 字段统一为 `version`，数据库统一使用非空 `BIGINT` 并从 `1` 开始；只判断客户端编辑基线是否仍然有效，不表达业务版次、发布版本或内容来源版本。聚合子资源共用聚合根版本。 |
| collection revision | 集合修订版本 | 对一次跨多个独立聚合的集合级替换提供并发边界的单调递增正整数。 | 仅在操作没有单一聚合根时使用；API 字段统一为 `revision`。它不是集合成员数量，也不能用任一成员的资源并发版本替代。 |
| CatalogPath | 引擎目录路径 | 引擎 catalog 内的路径坐标。 | 不等同于 ResourceLocator，也不等同于 Meta `full_name`。 |
| CatalogEntry | 引擎目录条目 | `CatalogProvider.ListChildren` 返回的 catalog 列表项，不是“入口”。 | 回答“当前位置下面有什么、结构上怎么走”；通过 `role=branch/leaf` 表达结构角色；列表摘要使用 `Table`、`Storage`、`LeafCount` 等显式字段，不保留兜底 attributes / stats。 |
| CatalogRoot | 引擎目录根 | 每个引擎 catalog 的显性结构根。 | 通常投影为 root `meta_node`；`full_name=""`，ResourceLocator path 为空，但仍可通过 `node_id` 定位。面向用户展示时使用引擎实例名称，不显示内部术语。 |
| CatalogBranch | 引擎目录分支 | 可继续列举子条目的 catalog entry。 | 例如 schema、database、bucket、prefix、directory；通常由 Meta 投影为 `meta_node`。 |
| CatalogLeaf | 引擎目录叶子 | 引擎 catalog 模型中的终点 entry。 | 例如 table、view、object、file、collection、graph、topic；是 data item 候选，但不等于 data item。 |
| topic | 消息主题 | Kafka catalog 中用户可选择的稳定消息资源。 | 业务 Kafka 使用 `service(cluster) -> topic`；partition 是执行分片和运行诊断，不是 catalog 子节点或用户 locator。 |
| partition | 消息分区 | Topic 内维护有序 offset 序列的执行分片。 | Transfer 按 partition 保存 committed position；用户选择 topic，不绑定固定 partition。 |
| CatalogFacts | 引擎目录事实 | Engine 对 catalog entry 直接知道的结构、存储、索引和原生事实详情。 | 回答“这个条目自身有哪些事实”；详情事实必须落在 `Table`、`Graph`、`Storage`、`Indexes` 等显式字段；不保留兜底 attributes / stats。 |
| LeafCount | 叶子数量摘要 | catalog branch 下直接 leaf 条目的数量摘要。 | 只用于低成本列表展示和扫描计划提示；例如 schema 下表数量、database 下 collection 数量。不是递归总量，也不是 Meta item 计数。 |
| content | 内容 | 可被按流读取、写入或 range 读取的底层内容对象。 | 例如文件、对象存储 object、容器 entry；由 `contentio.Ref` 定位。 |
| CAD data item | CAD 数据项 | 保留图层、块、布局、标注等 CAD 原生组织语义的设计图纸数据项。 | 当前内置二维 `dwg`、`dxf`，统一使用 `data_type=cad`；entity-as-row 不改变源 item 类型，CAD→GIS 输出是新的 table item。 |
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
| vector tile set | 矢量瓦片集 | 以 PMTiles v3 单文件封装、可作为业务数据长期保存和跨平台交换的二维矢量瓦片 data item。 | 统一使用 `data_type=media`、`format=pmtiles`、`layout=single`；当前瓦片编码固定为 MVT。 |
| PMTiles | PMTiles | 面向 HTTP Range Read 的单文件瓦片归档格式。 | ADDP 业务矢量瓦片集固定使用 PMTiles v3；PMTiles 是归档格式，MVT 是归档内单瓦片编码。 |
| model_3d | 三维模型数据 | 以三维空间对象、场景、网格、构件或三维可视化结构为核心消费对象的数据类型。 | 覆盖 GLB / glTF、单 OSGB、OSGB Scene 倾斜摄影、S3M、3D Tiles 场景、IFC / Revit BIM 等；具体子形态由 `type_info.model_3d.model_kind`、format、layout 和 capabilities 表达。 |
| point_cloud | 点云数据 | 以三维点集合、点属性、空间范围和抽样 / LOD 预览为核心消费对象的数据类型。 | 覆盖 LAS / LAZ / COPC、PCD、点云型 PLY、EPT / Potree 等；点属性不是普通表字段，不能仅因可列化而归为 `table`。 |
| gaussian_splat | 高斯泼溅数据 | 以三维高斯基元、尺度、旋转、不透明度和视角相关颜色为核心消费对象的数据类型。 | 覆盖 3D Gaussian Splatting PLY 以及后续 `.splat`、`.ksplat`、`.spz` 等格式；不是普通点云，也不走传统 mesh / GLB 模型路线。 |
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
| item fingerprint | 数据项指纹 | 由 `engine_id + full_name` 计算的稳定 data item 身份。 | 同一数据项内容变化时指纹不变；不是内容哈希，也不是源版本。`item_id` 只是当前 Meta 行引用。 |
| source version | 源版本 | 表达某个稳定数据项的当前内容版本事实。 | 可由 `content_hash`、`data_updated_at`、`last_modified_at` 或格式专用版本事实组成；用于判断派生结果是否过期，不替代 item fingerprint。 |
| dependency snapshot | 依赖快照 | 业务定义在创建或显式刷新时，从上游当前事实中选择其执行和对外契约真正依赖的部分并冻结保存。 | 不是完整 Meta item 副本。Meta 提供源事实和源时间；业务模块记录采集时间并对依赖投影计算 hash。差异用于提示重新发布，不自动改变既有业务契约。 |
| lineage | 数据血缘 | 数据项之间的来源、派生和服务依赖关系的关系视图。 | 归 Meta 管理关系事实和查询投影；不是新的数据资产实体或独立图数据库。完整边界见 [数据血缘能力规范](../spec/addp数据血缘能力规范.md)。 |
| lineage observation | 血缘关系证据 | Meta 根据执行事实或服务发布事实解析出的不可变关系证据。 | 保存来源快照、采集方式、执行 / 发布引用和时间；证据清理后仍可审计。 |
| lineage projection | 血缘当前投影 | 从关系证据维护的当前有效上游、下游和服务依赖关系。 | 支持 replace、append、upsert 等写入语义和时态查询；不是事实源。 |
| lineage facts | 血缘执行事实 | 真实读写 owner 在统一 execution 结果中写入的版本化输入、输出和操作事实。 | 使用 ResourceLocator 和 item fingerprint；Runtime 不构造 ADDP 资源身份。 |
| lineage collector | 血缘采集器 | Meta 消费 owner execution / publication fact，解析资源身份并写入关系证据和当前投影的单一路径。 | 立即通知与周期漏采 / 重试都调用同一个 `LineageService.CollectExecution`；不反向解析模块私有 metadata。 |
| published service | 已发布服务版本 | Service 一次通过验证并对外生效的不可变服务发布主体。 | 身份为 `service_id + published_revision`；不是 data item，但可作为血缘图主体。 |
| service dependency | 服务依赖 | 已发布服务读取、发布或暴露某个 data item 的来源事实。 | 在血缘中表现为 `data item --serve--> published service`；`dependency_hash` 只是快照版本摘要，不是具体血缘边。 |
| field ref | 字段引用 | 绑定到 data item 及其 schema snapshot 的字段级引用。 | 作为字段级血缘预留主体；字段默认不是独立 data item。 |
| queryable field path | 可查询字段路径 | 从记录根到具体值字段的结构化路径事实，用于动态 schema 记录集合的字段发现、查询生成和校验。 | MongoDB 示例为 `path=["members","userInfo","nickName"]`，MQL 投影为 `members.userInfo.nickName`；路径各层的 object / array 类型由同一组字段事实表达，不传递原始样本值。 |
| output contract snapshot | 输出契约快照 | 对没有单一 Meta item 身份的查询或计算结果，保存其已检测输出字段、主键、空间信息等契约事实。 | SQL 查询服务使用该快照；查询结果未物化并经 Meta 扫描前，不创建或伪造 Meta item。 |
| query service | 查询服务 | Service 将一个受治理的数据源或固定查询发布为稳定数据 API 的业务定义。 | 表、固定 SQL 和联邦 SQL 是来源表达；REST Query、OGC API Features、WFS 是协议投影，不是不同的查询执行路径。 |
| query service revision | 查询服务发布版本 | 查询服务一次通过验证并发布的不可变执行与输出契约。 | 包含来源绑定、输出契约、稳定排序键、查询策略、资源限制、执行绑定和依赖快照；修改定义必须产生新版本并原子切换，不原地改变已发布契约。 |
| structured query request | 结构化查询请求 | 消费者按字段、类型化过滤、排序和分页对象表达的查询请求。 | 不接受 SQL、WHERE/ORDER BY 片段或其他引擎原生表达式；协议适配层将 REST、OGC 参数编译为同一结构。 |
| stable order key | 稳定排序键 | 查询服务发布时声明的非空唯一字段序列，用于确定结果的全序和 Feature ID。 | 支持复合键；调用方排序不是全序时必须追加该键。没有稳定排序键的结果不得发布为可分页查询服务或 OGC Features。 |
| query cursor | 查询游标 | 由 Service 签发、绑定发布版本、查询指纹、有效排序和最后一行排序值的 opaque 分页位置。 | 游标不承载 SQL、凭据或授权；已发布数据查询使用 keyset/cursor 分页，不以 OFFSET 作为主路径。 |
| access index | 访问定位索引 | 为高效读取内容窗口而生成的定位索引。 | 标准落点为 `attributes.access_index.<data_type>`；例如 CSV / JSONL 表格的稀疏行号到字节偏移索引。它不是全文索引。 |
| content hash | 内容哈希 | 对原始内容二进制流计算得到的哈希值。 | 标准落点为 `attributes.storage.content_hash`，用于判断内容是否变化；不用于定位外部全文索引记录。 |
| basic scan | 基础扫描 | 快速发现资源树和 data item 身份的低成本扫描。 | 原则上不读取 file/object 内容。 |
| deep scan | 深度扫描 | 补充类型信息、格式信息、访问索引和横切能力的扫描。 | 可以读取内容，但必须遵守 provider / reader 边界和成本控制。 |
| scan depth | 扫描深度 | 本次扫描要达到的深度。 | 请求字段为 `scan_depth`，只允许 `basic` / `deep`。 |
| scanned depth | 已扫深度 | 当前 `meta_node` / `meta_item` 已达到的元数据完整度。 | 落库字段为 `scanned_depth`，取值为 `none` / `basic` / `deep`。 |
| scan status | 扫描状态 | 扫描任务或最近扫描过程的运行状态。 | 表达 `pending`、`running`、`completed`、`failed` 等过程状态，不表达扫描深度。 |
| force scan | 强制扫描 | 不管已有元数据和低成本过时判断，重新扫描并覆盖本次深度对应的元数据。 | 请求字段为 `force`。 |
| exact row count | 精确行数 | 对 data item 当前内容执行精确计数得到的总行数。 | 公共字段固定为 `row_count`；SQL 表通常需要显式 `COUNT(*)`，只允许在 deep scan 或调用方显式请求统计时获取。0 是有效精确值。 |
| estimated row count | 估算行数 | 引擎 catalog、system table、格式结构元数据或有限分析低成本提供的近似行数。 | 公共字段固定为 `estimated_row_count`；可用于列表提示和低成本变化判断，不得用于分页边界、完整性校验或冒充 `row_count`。 |
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

## 数据探查与剖析

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| data profiling | 数据剖析 | 通过受控采样或显式全量执行，分析表格型 data item 的完整性、基数、值域和分布特征的过程。 | 归 Manager；不是 Meta scan、BI 聚合分析或 Quality 质量检查。 |
| data profile | 数据剖析结果 | Manager 对某个稳定 data item、源版本和剖析配置保存的当前成功分析结果。 | 归 Manager 私有结果表；不写入 Meta attributes，不是 data item、任务定义或 execution。 |
| profiling execution | 剖析执行 | 实际读取源数据并生成或刷新 data profile 的一次有界执行。 | 首期统一为 `task_type=data_profiling` 的 ad-hoc execution，`source_task_id` 为空。 |
| profile mode | 剖析模式 | 一次剖析执行读取源数据的范围策略。 | 取值预留 `sample` / `full`；首期只开放 `sample`。它不是 task type，也不决定是否存在任务定义。 |
| profile data scope | 剖析数据范围 | 一次剖析执行所分析的行集合。 | 固定为 `all` 或结构化 `condition`；条件范围参与 `profile_config_hash`，不得用 SQL 文本表达。 |
| conditional profiling | 条件剖析 | 先按结构化字段条件限定数据范围，再在命中行集合上采样并计算数据剖析结果。 | 归 Manager；不是预览页过滤，也不是 Develop SQL 查询。条件必须由 Provider 在采样前执行。 |
| profile observation | 剖析观察 | 根据字段级统计派生的描述性数据特征提示。 | 例如高缺失、常量、高基数和分布偏斜；不是质量问题或质量评分。 |

## 数据质量

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| quality rule | 质量规则 | Standard 数据元上使用版本化契约定义的、可复用的字段质量约束。 | 规则只描述语义，不绑定物理字段，也不包含自定义 SQL。 |
| rule key | 规则身份 | Standard 为每条质量规则持有的稳定 UUID，API 字段为 `rule_key`。 | 新规则创建时生成并在编辑时保留；Quality 只能继承该身份，不得按规则应用或物理目标生成第二套身份。 |
| RuleApplication | 规则应用 | Quality 将一份数据元质量规则快照绑定到确定 Engine Instance、schema、table 和 column 的持久事实。 | Standard 规则变化不静默改写已有快照。 |
| quality check | 质量检查 | Quality 在一次持久 execution 中对确定表的全部有效规则应用进行完整求值的过程。 | v1 只支持 PostgreSQL；任一规则执行错误时整次 execution 失败。 |
| quality score | 质量分 | 一次成功质量检查中各规则通过率的算术平均。 | 不按 severity 或行数加权；无有效规则不能产生质量分。 |
| quality issue | 质量问题 | 某条规则应用中的某条规则当前仍存在未通过事实的可治理状态。 | 稳定身份为 Tenant + RuleApplication + rule key；历史发生记录保留在 execution 结果中。 |

## 任务与执行

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| Task | 任务 | 可被执行的业务能力抽象。 | Task 是抽象概念，不是统一任务总表；任务定义归 owner 模块私有表。 |
| spatial task | 空间任务 | Manager 中按空间数据处理目的组织业务任务的导航与能力分类。 | 不是统一任务表或单一 `task_type`；当前包含“矢量瓦片”，后续空间业务能力按各自任务类型扩展。 |
| task definition | 任务定义 | “未来应该按什么策略处理什么对象”的定义态。 | 例如 `meta.scan_tasks`、`transfer.transfer_tasks`、`manager.vector_tile_cache_tasks`。 |
| task semantic identity | 任务语义身份 | owner 模块用于判定两次创建是否表达同一个持久任务定义的规范化键。 | 不等于任务 ID，也不由 execution ID 构成。受管派生任务通常由租户、稳定源身份和派生变体构成。 |
| artifact variant | 派生变体 | 在同一稳定源上区分不同当前派生目标的规范化配置投影。 | 例如 `target_format=3d_tiles|s3m`、`tile_format=mvt`、几何列加目标 SRID。不能用输出目录名推断。 |
| owner task schedule | 任务自身调度 | owner 模块任务定义上保存并由 owner scheduler 触发的独立定时计划。 | 只决定该任务作为独立任务何时自动执行。 |
| orchestration schedule | 编排调度 | Orchestrator 编排定义上保存的定时计划。 | 只决定编排 run 何时启动；不继承、不覆盖其中 Step 引用任务的自身调度。 |
| task type | 任务类型 | owner 模块内稳定的业务执行类型标识。 | 例如 `scan`、`vector_tile_cache_generation`、`embedding`；只有存在持久任务定义并允许编排时才由 TaskProvider capabilities 声明，ad-hoc-only execution type 不因此自动成为 TaskProvider 类型。 |
| TaskProvider | 任务提供者 | 模块对外声明可编排任务能力的角色。 | 按模块注册，不按任务类型注册；一个 provider 在 `task_capabilities[]` 中声明多个任务类型能力。 |
| execution worker | 执行工作器 | 执行 owner 消费已创建 execution、推进真实运行体并写入 execution 终态的独立后台进程角色。 | Quality `check`、Meta `scan` 和 Transfer bounded `sync` 统一从 PostgreSQL claim `common.task_executions`；Backend 不执行这些 bounded execution。 |
| owner scheduler | Owner 调度器 | 按任务定义中的 schedule 发现到期任务并创建 execution 的 owner Backend 组件。 | 调度器负责“何时创建 execution”，不等待 Worker、也不执行业务逻辑；Worker 不可用时仍可留下 durable `pending` execution。 |
| runtime queue | 运行时队列 | execution 从创建到被执行 worker 领取之间的持久领取机制。 | bounded execution 的唯一主路线是 PostgreSQL execution claim；continuous runtime 使用专用 runtime lease。Redis/Asynq 和进程内 channel 不作为 bounded execution 路线。 |
| execution attempt | 执行尝试 | 同一未终态 execution 在一次合法 claim 下的实际运行尝试。 | 每次 claim 原子递增 `attempt` 并生成新的 `lease_token`；用户 retry 不是新 attempt，而是创建新的 execution。 |
| execution lease | 执行租约 | bounded execution worker 对当前 execution attempt 的限时运行所有权。 | 由不可复用 `lease_token`、观测用 `lease_owner` 和 `lease_expires_at` 构成；heartbeat、进度和终态写入必须匹配当前 attempt 与 token。 |
| dispatcher | 投递器 | 从 owner outbox 或 delivery 队列领取待发送事项并调用外部接收方的后台角色。 | Monitor Webhook/邮件 dispatcher 不执行业务任务，不创建业务 execution；投递记录和重试状态归 Monitor outbox。 |
| maintenance loop | 维护循环 | 处理固定系统维护、清理、注册同步或观测采集的后台循环。 | 不等于 execution worker；只有演进为可持久执行、可审计的任务定义后，才进入 owner scheduler + execution worker 体系。 |
| source task id | 来源任务 ID | execution 关联的 owner 模块任务定义 ID。 | 在 `common.task_executions.source_task_id` 中保存；查询时必须结合 `module + task_type`。 |
| parent execution id | 父执行 ID | 当前 execution 的父级 execution UUID。 | 用于 Orchestrator 子步骤追踪父编排。 |
| ad-hoc execution | 一次性执行 | 不依赖持久任务定义、直接按本次配置创建的 execution。 | 可以没有 `source_task_id`，但必须在 `execution_config` 保存完整执行配置。 |
| artifact state | 产物状态 | 描述派生产物当前是否可用、在哪里、由什么配置生成的状态对象。 | 例如瓦片缓存产物、embedding vectors；不是 execution。 |
| existing result action | 已有结果动作 | 调用方在执行会刷新 owner 受管当前结果时显式声明的动作；当前只允许 `overwrite`。 | TaskProvider 请求参数为 `parameters.existing_result_action=overwrite`。前端人工执行时先二次确认再提交；Orchestrator 可将该动作保存为 Step 参数并在定时 Pipeline 中逐次提交。没有当前结果时可省略；业务派生数据不适用。 |
| quick view task | 快显任务 | 为源 data item 生成 Manager 受管快显结果的任务定义。 | 任务归 Manager 私有表；重型转换由对应 Workflow Runtime direct 算子执行。 |
| quick view result | 快显结果 | Manager 为提升交互预览效率而生成并维护生命周期的 infra artifact。 | 不是业务 data item，不进入 Meta；同一源 item 可以按目标格式关联多个独立结果。 |
| derived data | 派生数据 | 通过计算或转换从源 data item 生成、写入业务存储并形成独立 Meta item 的数据。 | Develop 工作流输出属于派生数据；Manager infra 快显结果不属于派生 data item。 |
| execution boundary | 执行边界 | 一次 execution 是否具有确定结束条件。 | `bounded` 表示处理到本次冻结上界后结束；`continuous` 表示持续等待变化直到被真实停止、失败或失联。 |
| load mode | 装载方式 | Transfer 从源端读取完整范围还是已提交位置之后的变化。 | 只允许 `snapshot` / `incremental`；它与触发方式和目标应用方式正交。 |
| watermark | 水位游标 | 以源表中可稳定排序的业务字段识别 insert/update 变化的批增量位置。 | 必须使用 `(watermark_field, tie_breaker...)` 复合游标并冻结每次 bounded execution 的上界；普通 watermark 不发现物理删除，不等同于 CDC。 |
| CDC | 数据库变更捕获 | 从数据库事务日志持续捕获已提交的 insert、update 和 delete，并按确定的初始化与恢复协议交给下游应用。 | CDC 不等于按 `updated_at` 轮询；PostgreSQL 第一版由 Debezium 读取 logical replication slot，经 Infra Kafka 交给 Transfer。 |
| Oracle CDC | Oracle 数据库变更捕获 | Transfer 通过独立 Oracle common user 和 Debezium LogMiner 从 redo 捕获普通关系字段；遇到 `MDSYS.SDO_GEOMETRY` 时，由同一 capture generation 在源 schema 内维护 ADDP-owned WKB 镜像表、行级触发器和 DDL guard，再交给统一 continuous worker。 | 支持 CDB/PDB 下有稳定主键、已启用表级 `ALL COLUMN LOGGING` 的普通字段与 Oracle Spatial 单表，固定 `initial_snapshot` 和严格 schema drift；Spatial capture 运行时拒绝源表 DDL，镜像对象由 Transfer 创建并在 Stop 删除。RAC 和普通业务 LOB 仍未开放；长事务仅提供源端压力观测，不改变已提交事务 CDC 语义。不能由 Oracle Engine 普通读取能力自动推断，也不等同 ArcGIS SDE 逻辑变化源。 |
| ArcGIS SDE logical change source | ArcGIS SDE 逻辑变化源 | 按 ArcGIS enterprise geodatabase 的版本模型、业务事务和 delta table 语义识别的要素变化源。 | 当前完成 Oracle 实例级 workspace 正式核心表组合探测，并冻结 workspace-scoped Provider 契约；首个数据面只允许 traditional versioning，branch versioning 不复用 delta table 路线。Provider 原生位置与 Transfer 提交的 Kafka offset 分离；尚未在没有真实 Enterprise Geodatabase 的情况下声明可运行能力。即使底层使用 Oracle，也不等同 Oracle redo 中的普通表 CDC。 |
| CDC bootstrap | CDC 初始化 | 在一个无空洞的日志衔接点上建立一致性初始快照，并继续消费快照期间和之后产生的日志变化。 | PostgreSQL 第一版固定使用 Debezium `initial` snapshot；Transfer 不自行拼接“先全量、后开 CDC”两条路径。 |
| apply mode | 目标应用方式 | Transfer 将本次读取结果应用到目标的策略。 | 稳定取值为 `replace`、`append`、`upsert`、`upsert_delete`；目标 Provider 必须声明并真实实现对应能力。 |
| sync state | 同步主状态 | Transfer 为增量任务保存的已提交源位置。 | 存储于 `transfer.sync_states`；与任务定义、execution checkpoint 分离，只能在目标提交成功后通过 CAS/fencing 推进。 |
| capture position | 捕获位点 | 捕获组件已经从源数据库事务日志可靠读取并写入 Infra Kafka 的源日志位置。 | PostgreSQL CDC 对应 LSN，MySQL 对应 binlog position，Oracle 对应 SCN；均由 Debezium/Kafka Connect connector offset 管理，Transfer 只读观测而不复制维护。 |
| source recovery window | 源恢复窗口 | 当前仍可供 capture position 连续恢复的源数据库事务日志范围。 | 与 Infra Kafka retention 正交；Oracle 由当前可用 redo/archive 最早 SCN、时间窗口和可选 FRA 容量事实表达，不从 SCN 差值伪造时间。 |
| source transaction pressure | 源事务压力 | capture 数据库当前未提交事务形成的运行压力事实。 | 与源连通状态、源恢复窗口和已提交事件 lag 正交；Oracle 由活跃事务数、最老事务起始 SCN/持续秒数和 Undo blocks/records 表达，不按平台硬编码阈值派生健康状态。 |
| committed position | 已提交位置 | 目标端已可靠应用完成后允许继续消费的源位置。 | Kafka position v1 按 partition 保存 `next_offset`；worker 必须从该 offset seek，不能依赖 consumer auto commit 作为事实源。 |
| runtime session | 运行时会话 | continuous execution 在某个 Transfer continuous worker 中的一次实际长驻运行。 | 一个 task 同一时刻只有一个合法 session owner；session 结束后 execution 随之结束，恢复必须创建新 execution。 |
| runtime lease | 运行时租约 | continuous worker 对 runtime session 的限时所有权。 | 保存 owner instance、lease deadline、heartbeat 和 fencing token；与业务 committed position 分离。 |
| apply identity | 应用身份 | Transfer 为一个任务生成、跨 execution 保持不变的全局 UUID，用于标识该任务对外部目标的应用主线。 | 由服务端生成且不可修改，不进入任务 config；业务目标 PostgreSQL 的 apply ledger 使用它隔离任务并校验 source/target identity。 |
| target apply ledger | 目标应用账本 | 与业务数据位于同一目标数据库、记录各 source partition 已原子应用位置的技术账本。 | continuous PostgreSQL v1 固定为 `addp_transfer.apply_positions`；数据 upsert 与 `next_offset` 推进必须在同一事务，不能用 Infra PostgreSQL 中的 `transfer.sync_states` 替代。 |
| observation signal | 观测信号 | 由 owner 写入的运行诊断事实无状态派生出的当前风险提示。 | 不是任务状态、execution 状态或持久化告警；例如 retention critical、checkpoint stalled。 |
| alert incident | 告警事件 | Monitor 将持续存在的观测信号物化出的可确认、可抑制、可恢复的运维事件。 | 告警不反向修改 owner 状态；同一任务同一信号同时最多一个未恢复事件。 |
| alert event | 告警生命周期事件 | 告警事件发生打开、严重级别升级或恢复时，由 Monitor 在同一事务中写入的不可变事件。 | 稳定类型为 `opened`、`escalated`、`resolved`；是通知 outbox 的事实输入。 |
| alert notification | 告警通知 | 告警事件打开、升级或恢复时向外部渠道发送的消息。 | 通知是告警生命周期的消费结果，不是新的健康事实源。 |
| alert rule | 告警规则 | Monitor 对一个稳定 owner 任务定义配置的告警判定策略。 | 第一版精确绑定 `module + task_type + source_task_id`，规则不进入 owner 任务 config，也不读取 owner 私有表。 |
| generic execution alert | 通用执行告警 | Monitor 根据统一 execution 终态派生的任务级告警。 | 第一版包括最近失败、最近超时和连续失败；ad-hoc execution、子 execution 与 Transfer continuous session 不进入通用规则。 |
| notification route | 通知路由 | 一条租户告警规则到一个 Webhook 或邮件目标的显式投递绑定。 | 无路由时仍保存 incident/event，但不生成外部 delivery；路由只影响后续生命周期事件。 |
| webhook destination | Webhook 目标 | 租户在 Monitor 中配置的告警通知 HTTP 接收端。 | 保存名称、URL、订阅事件、启用状态和加密签名密钥；不属于 System Engine 或 TaskProvider。 |
| webhook delivery | Webhook 投递 | 一个告警生命周期事件面向一个 Webhook 目标生成的可靠投递记录。 | 采用至少一次投递；接收方必须使用 `delivery_id` 幂等。 |
| webhook test delivery | Webhook 测试投递 | 用户主动验证某个 Webhook 目标连通性、签名和接收行为的一次同步测试请求。 | 使用独立 `monitor.webhook.test/v1` schema，不创建告警生命周期事件或正式 delivery outbox；操作本身进入 System 审计日志。 |
| webhook manual retry | Webhook 手动重投 | 用户把已经进入 `dead` 终态的 Webhook delivery 重新放回投递队列。 | 复用原 `delivery_id` 和 payload，使用目标当前 URL/secret，并以新的最大尝试周期继续投递；不得生成新 delivery 身份。 |
| email destination | 邮件目标 | 租户在 Monitor 中配置的告警邮件收件目标。 | 只保存名称、收件地址、订阅事件和启用状态；SMTP Relay、发件身份和凭据属于平台部署配置。 |
| email delivery | 邮件投递 | 一个告警生命周期事件面向一个邮件目标生成的可靠投递记录。 | 冻结收件人、主题和正文，采用至少一次投递；`delivery_id` 同时作为稳定投递身份和邮件 Message-ID 的组成部分。 |
| email test delivery | 邮件测试投递 | 用户主动验证邮件目标和平台 SMTP Relay 的一次同步测试发送。 | 使用独立测试内容，不创建告警生命周期事件或正式 delivery outbox；操作本身进入 System 审计日志。 |
| email manual retry | 邮件手动重投 | 用户把已经进入 `dead` 终态的邮件 delivery 重新放回投递队列。 | 复用原 `delivery_id`、主题和正文，使用目标当前收件地址，并以新的最大尝试周期继续投递；不得生成新 delivery 身份。 |
| ChangeRecord | 变化原始记录 | Change stream Provider 从外部消息系统读取的原生记录。 | Kafka record 包含 topic、partition、offset、timestamp、headers、key/value 原始字节；不等于归一化 ChangeEvent。 |
| ChangeEvent | 统一变化事件 | Transfer source adapter 将 Kafka record 或 CDC envelope 归一化后的内部变化对象。 | 业务 Kafka record v1 归一化为 `operation=upsert`；PostgreSQL、MySQL、Oracle CDC 归一化 snapshot/upsert/delete。Kafka、Debezium 和数据库日志协议细节不得进入目标 writer。 |
| business Kafka Engine | 业务 Kafka 引擎 | 用户在 System 注册、按租户授权并显式选择 topic 的外部 Kafka。 | 进入 System engines、Catalog 和 ResourceLocator；ADDP 不默认创建或删除用户 topic。 |
| Infra Kafka | 基础设施 Kafka | ADDP 内部为 Debezium CDC 中转、缓冲和 replay 使用的基础设施。 | 不进入 System engines、资源树或用户任务 endpoint；与业务 Kafka 即使物理共用也必须使用独立凭据、ACL 和 topic namespace。 |
| resume | 恢复执行 | 新 execution 从任务同步主状态的 committed position 继续处理。 | 第一版 watermark 增量仅支持 resume；已结束 execution 不复用。 |
| dead-letter record | 死信记录 | continuous runtime 按任务显式策略把无法归一化或映射的原始业务 Kafka record 持久化为可审计跳过事实。 | DLQ 写入、控制索引落库和目标 `skip` ledger 提交全部成功后才能推进主 position；PostgreSQL CDC schema/protocol 漂移不进入 DLQ。 |
| replay | 历史重放 | 按显式 source partition/offset 历史范围创建独立 bounded execution，且不读取或推进同步主状态。 | v1 只允许业务 Kafka 重放到不存在的新 PostgreSQL 隔离目标；不是新的 `task_type`，也不是把 DLQ 单条消息补写回原目标。 |

## 工作流与算子

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| ADDP Operator | ADDP 算子 | ADDP 平台统一的可编排能力定义，包含稳定 ID、参数、输入输出端口、执行模式和展示元数据。 | 面向 Develop 前端、工作流定义、参数校验、执行历史和血缘；不承载具体引擎私有类名。 |
| Workflow Runtime | 工作流运行时 | 按 `addp.workflow/v1` 协议接收算子列表查询和 workflow_def 执行请求的独立执行服务。 | 例如 GeoPython Workflow、Spark Workflow、Model3D Workflow、PointCloud Workflow、SuperMap Workflow。 |
| GeoPython Workflow | GeoPython Workflow 运行时 | 基于 Python 地理计算生态实现的 Workflow Runtime，提供 Pandas、GeoPandas、GDAL/OGR 等算子能力。 | 用户可见名称、System Engine Instance 名称和文档专名统一使用 `GeoPython Workflow`；`engine_type` 固定为 `geopython_workflow`，不得使用其他历史别名。 |
| Federated Query Runtime | 联邦查询运行时 | 通过统一查询协议执行 SQL，并按本次执行授权动态挂载多个 Source Engine 或对象表的独立计算服务。 | DuckDB Runtime 是首个实现；Runtime Engine 只表示计算端点，SQL 引用的数据源仍分别保持自己的 Engine Instance 身份。 |
| Runtime Engine | 运行时引擎 | System 中代表独立计算 Runtime 物理端点的 Engine Instance。 | Develop 任务和 Service 定义绑定 Runtime Engine；它不等于查询涉及的 Source Engine，也不因此获得 Source Engine 数据权限。 |
| Source Engine | 数据源引擎 | 一次查询、工作流或服务执行实际读取或写入数据的 Engine Instance。 | Source Engine 必须逐个进入 Execution Authorization；不能用 Runtime Engine ID 替代或隐式扩大数据源范围。 |
| Public Operator Spec | 公开算子规范 | 面向用户、前端、AI 和 Develop 任务定义的算子公开契约，声明公开参数、资源选择方式和校验规则。 | 资源身份使用 `locator` 或 `target_parent_locator + target_name`；不得暴露 `connection_info` 等内部连接参数。 |
| execution input contract | 执行输入契约 | 某个具体任务定义允许调用方在单次 execution 中覆盖的公开输入 Schema、默认值和 UI 语义。 | 保存的任务定义必须始终可直接执行；未提交的输入使用任务保存值，提交值只影响本次 execution。工作流中未被内部连线占用的可序列化 Public Operator 参数默认进入该契约。 |
| execution output contract | 执行输出契约 | 某个具体任务定义对外承诺的、可被 Orchestrator 后续 Step 引用的稳定执行结果 Schema。 | 只允许声明可跨任务边界传递的持久结果或稳定引用，例如 ResourceLocator；不得暴露 DataFrame、GeoDataFrame 或运行时私有内存句柄。 |
| execution parameter override | 执行参数覆盖 | 调用方按执行输入契约为某一次 execution 提交的部分参数值。 | 未提交字段保留任务默认值；覆盖不得修改任务定义，不得改变 DAG 结构，也不得绕过最终资源校验和 Execution Authorization。 |
| query parameter definition | 查询参数定义 | 查询任务为一个命名值声明的公开类型、默认值、标题和说明。 | 第一版只允许 `string`、`integer`、`number`、`boolean` 四种标量类型；保存的任务必须为每个参数提供默认值，并由定义派生任务级执行输入契约。 |
| query parameter binding | 查询参数绑定 | Query Runtime Provider 把本次 execution 的类型化参数值绑定到查询语言参数位置的过程。 | SQL 使用 `:name` 命名参数并编译为驱动占位符，Cypher 使用 `$name` 原生参数，MQL 使用 `{\"$param\":\"name\"}` 结构化参数节点；禁止字符串插值，参数不得替代标识符、关键字或查询片段。 |
| Develop Adapter Spec | Develop 适配规范 | Develop Backend 按工作流引擎类型和算子 ID 选择的显式执行前转换契约，声明公开资源参数如何派生为运行时参数。 | 负责查询 System Engine Instance、派生 `connection_info/schema/table/path` 并移除公开资源参数；不得按参数名隐式触发。 |
| Runtime Operator Spec | 运行时算子规范 | Workflow Runtime 实际执行算子时消费的内部契约，只声明运行时真实需要的参数、输入输出端口和执行行为。 | 不解析 ADDP `ResourceLocator`，不承载资源树 UI 配置；`connection_info/schema/table/path` 属于适配层到运行时的内部参数。 |
| Workflow Access Plan | 工作流访问计划 | Develop、Manager 等调用方把已解析的存储资源转换为 Workflow Runtime 可执行读写计划的内部契约。 | 当前版本为 `addp.workflow.access-plan/v1`；只在执行期携带 `mounted_path` 或 `object_store` 访问参数，不作为用户任务定义、资源身份或长期事实源。 |
| Execution Effect | 执行效果 | 一次计算对数据或外部系统可能产生的效果分类。 | 固定为 `read`、`write`、`ddl`、`external_effect`；工作流按全部算子的最高效果收窄授权，不能由客户端自报后直接信任。 |
| Execution Authorization | 执行授权 | System 基于当前 User AuthContext 或已发布服务定义来源，绑定唯一 execution、Tenant、owner audience、Source Engine、允许效果、来源版本和有效期的短期授权事实。 | 两种来源互斥；Notebook 派生的用户来源还绑定其 Notebook Session Authorization，并继承 Session 与 Token Family 生命周期。服务定义来源只允许 owner Service Principal 为自己的已发布定义签发只读授权。它不是 Role、OAuth Scope 或第二种 Tenant Membership，只允许匹配 audience 的 Runtime Service Principal 消费。 |
| Task Authorization Subject | 任务授权主体 | 持久任务定义为定时或延迟执行绑定的 User、Tenant Membership 和授权版本事实。 | 只能由同 Tenant 的当前 User AuthContext 在创建、更新或显式重新授权任务时写入；不保存 Access Token。任务定义变化或授权版本变化后必须重新授权，执行开始时仍需重新校验 Membership、Role、资源规则和授权版本。 |
| Managed Compute Session | 受控计算会话 | Develop 按 Execution Authorization 创建并管理的 SQL、Workflow 或 Jupyter 执行会话。 | Runtime 只获得本次执行所需的短期访问能力；Jupyter 不再直接获得长期 Engine 凭据或共享 Lab 的无限制数据访问。 |
| Notebook Interactive Session | Notebook 交互会话 | Develop 为一个 Tenant、User、Notebook Task 和 Script Engine 临时创建的隔离 JupyterLab 会话。 | 由已鉴权 API 创建，浏览器只访问 Develop 同源代理；会话关闭、过期或 Develop 重启后失效，Runtime 在清理前把 Notebook 保存回 owner 路径。它不是共享 Lab，也不是任务执行记录。 |
| Notebook Native Engine Facade | Notebook 原生引擎门面 | `common-python` 面向 Notebook 使用者提供、按具体 Engine 原生术语组织的只读 Python 客户端。 | 例如 PostgreSQL 的 `schemas()` / `tables(schema=...)`、MongoDB 的 `databases()` / `collections(database=...)`。它只把用户表达编译为统一 Catalog 请求，不新增引擎专用后端契约，不模拟完整原生驱动。 |
| workflow_def | 工作流定义 | ADDP 工作流运行时协议中的 DAG 定义结构。 | 由 ADDP 前端和后端消费；不得直接等同于某个引擎的私有 DAG JSON。 |
| SuperMap iObjects C++ | SuperMap iObjects C++ | SuperMap 提供的 C++ 数据访问、空间分析、CAD 渲染和三维转换 SDK。 | 作为 `supermap_workflow` 的运行时内部依赖，不直接暴露给 ADDP 前端；完整 SDK 母版不进入 ADDP 仓库或最终运行镜像。 |
| supermap_workflow | SuperMap 工作流运行时 | ADDP 工作流运行时类型，对外实现 `addp.workflow/v1`，对内使用 SuperMap iObjects C++ API 和类型化内存句柄执行 DAG。 | 第一阶段只支持普通 DAG，不实现条件、循环或子工作流；实现与部署见 `engines/supermap-workflow/README.md`。 |
| SuperMap SDX+ for PostGIS | SuperMap SDX+ for PostGIS | 基于 PostgreSQL/PostGIS geometry 的 SuperMap 空间工作区，稳定 workspace 身份为 `supermap/sdx_postgis`。 | geometry 使用 PostGIS 原生编码；与 SuperMap SDX+ for PostgreSQL 不得存在于同一 PostgreSQL 实例。 |
| SuperMap SDX+ for PostgreSQL | SuperMap SDX+ for PostgreSQL | 基于 PostgreSQL、由 SuperMap 私有 geometry 编码承载的空间工作区，稳定 workspace 身份为 `supermap/sdx_postgresql`。 | 表结构、记录数、Bounds 和空间索引由 SuperMap iObjects C++ SDK 维护；不得把私有 geometry Blob 暴露给 Transfer 或 Common Spatial。 |

## 智能体能力与交互

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| Agent Runtime | 智能体运行时 | 负责多轮对话、上下文管理、Skill 加载、规划、Tool 调用循环和 run 生命周期的智能体执行环境。 | ADDP 内置 Agent、Codex、Hermes Agent 都可以是 Agent Runtime；Agent Runtime 不拥有 ADDP 业务能力。 |
| AgentRun | 智能体运行 | Agent Runtime 为完成一次用户目标而持有的可暂停、恢复和审计的逻辑运行。 | 可以跨多个 AG-UI 请求或连接存在；不等同于 owner 模块 execution，也不以某个内存中的 LLM 调用为生命周期边界。 |
| AgentCheckpoint | 智能体检查点 | AgentRun 在可恢复边界保存的语义状态快照。 | 只保存 owner Tool 已观察事实、用户已确认选择、待处理 Interaction 和恢复所需控制状态；不保存模型隐藏推理、完整大结果或框架私有内存对象。 |
| AgentRunStep | 智能体运行步骤 | AgentRun 中一次可审计的 Tool 调用或 Runtime 控制动作记录。 | 保存稳定 Tool 名称、输入、状态、受限输出摘要、事实投影和时间；不是 owner 模块 execution step。 |
| AgentRunEvent | 智能体运行事件 | AgentRun 向客户端输出的、可按序重放的安全协议事件记录。 | 以 run 内单调 sequence 排序；仅保存文本、状态、Tool 进度和 Presentation 等可重建投影，不保存 Tool 原始参数、原始结果、模型隐藏推理或框架私有状态。 |
| Agent Evaluation Scenario | 智能体评测场景 | 以版本化输入、运行轨迹和结构化断言验证 Agent Runtime 行为的可重复评测契约。 | 黄金场景位于 `evals/agent-scenarios/`；断言 Skill、Tool、Interaction、稳定错误、AgentRun/owner 副作用和数据安全边界，不按自然语言逐字匹配。 |
| Agent Evaluation Report | 智能体评测报告 | 统一门禁或报告比较生成的版本化、安全审计输出。 | 只保存稳定状态、源码身份、契约/证据审计绑定和受限差异，不复制在线 trace、审批上下文、原始 Tool 结果或 Token。 |
| Agent Release Evaluation Baseline | 智能体发布评测基线 | 外部发布系统接受的、用于后续正式发布比较的 clean `online_required` 智能体评测报告。 | 必须 `status=passed`、`worktree_dirty=false` 且 release-ready 比较通过；Agent 模块不持有 baseline 指针或发布接受事实。 |
| ADDP Skill | ADDP 技能 | 面向一类可复用任务的知识与工作方法包，包含触发条件、步骤、反模式和所需 Tool。 | 不绑定单次业务案例、固定数据集或固定参数；`workflow-analysis` 是 Skill，铁路占耕地面积计算是评测场景。 |
| ADDP Tool | ADDP 工具 | 面向智能体暴露的稳定、受控操作能力。 | Tool 是 AI 能力契约，不等同于任意 HTTP endpoint；业务执行仍归 owner 模块正式 API。 |
| Tool Manifest | 工具清单 | 声明 ADDP Tool 名称、版本、输入输出 Schema、owner、权限、风险、错误和审计约束的机器可读契约。 | 是 AI Tool 契约事实源；不替代 Swagger，也不自动开放全部 API。 |
| Tool Adapter | 工具适配器 | 把同一 Tool Manifest 和 Python SDK 暴露给特定 Agent 宿主的薄协议层。 | ADDP Agent Tool Provider、`addp` CLI 和后续 MCP Server 都是 Adapter；不得包含分叉业务逻辑或第二套 API Client。 |
| ResultRef | 结果引用 | Agent 消息对 owner 模块业务结果的稳定引用。 | 不建立全局 Artifact 实体，不复制 workflow、execution、数据项或其他 owner 事实。 |
| Interaction | 交互请求 | 等待用户完成澄清、审批、表单或资源选择的有状态请求。 | 必须有稳定 ID、owner 和状态；写入审批由业务 owner 模块持有，客户端确认不是权威事实。 |
| Presentation | 表现描述 | 描述 ResultRef 或 Interaction 如何在客户端显示的可重建投影。 | A2UI Surface、文本摘要和 `open_url` 都属于表现方式，不是业务事实源。 |
| AG-UI | Agent 用户交互协议 | Agent Runtime 与 Web 前端之间传递 run 生命周期、文本、Tool、状态和 Activity 的标准事件协议。 | ADDP Agent 正式切换后不再使用 `0:`、`dag:` 等私有流前缀。 |
| A2UI | Agent 到用户界面协议 | 通过版本化 Catalog 和声明式组件消息描述 Agent 界面的表现协议。 | 只允许客户端预注册组件和函数；负责 Presentation，不替代 ResultRef、Interaction、权限或服务端校验。 |
| A2UI Surface | A2UI 界面单元 | A2UI 中由组件树、数据模型和 action 组成的独立渲染单元。 | 通过 `surface_id` 引用；完整组件树不应作为大段 Tool Result 进入 LLM 上下文。 |

## AI 推理

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| configuration owner | 配置所有者 | 对某项运行策略的语义、存储和生效负责的业务模块。 | System 只登记配置管理入口，不理解业务配置项。 |
| query policy | 查询策略 | Develop 对查询超时和结果预览规模的强类型运行约束。 | 平台定义上限，租户可以覆盖默认查询超时。 |
| quick view policy | 快显策略 | Manager 对空间数据预览中间结果规模、超时和重试的运行约束。 | 归 Manager 所有，当前为平台级。 |
| AI Inference Runtime | AI 推理运行时 | 对 ADDP 调用方提供统一 `addp.inference/v1` 数据面的推理服务。 | Runtime 按网络区域、安全域、GPU 集群、故障域或 SLA 增长，不按模型厂商、账号或模型数量增长。 |
| AI Inference Runtime Engine Instance | AI 推理运行时引擎实例 | System 中登记一个确定 AI Inference Runtime 端点的 Engine Instance。 | `engine_type=inference_runtime`；默认平台级，只登记 Runtime 端点、生命周期和 `compute.inference` 能力，不保存上游 Provider、模型或 API Key。 |
| Provider Template | 模型服务模板 | Inference owner 提供的只读接入模板，预置在线厂商或本地推理运行时的协议适配器、默认 Endpoint、凭据要求、模型发现方式和建议模型。 | 只用于创建 Provider Connection、Model Deployment 和 Model Profile，不是新的运行时资源，不持有凭据，也不参与推理解析。 |
| Provider Connection | 模型提供方连接 | Inference owner 保存的一个确定在线厂商账号端点或内网推理服务端点。 | 是强类型资源，不是普通键值配置；可为平台级或 Tenant 级，凭据使用专用加密字段。 |
| Model Deployment | 模型部署 | Provider Connection 下一个可调用的具体模型或部署单元。 | 保存上游模型标识、能力、限制和状态；继承 Provider 的范围，不在 System 中展开为 Engine Instance。 |
| Model Profile | 模型档案 | 面向调用方的稳定逻辑能力名称及其当前明确 Model Deployment 绑定。 | 例如 `general-chat`、`reasoning`、`text-embedding`、`multimodal-embedding`、`rerank`；第一版只绑定一个 Deployment，不包含隐藏 fallback。 |
| Scenario Binding | 场景绑定 | 业务 owner 将本模块的稳定 AI 场景显式绑定到 Model Profile 或特定 Model Deployment 的事实。 | 归 Agent、Copilot、Manager 等调用模块保存；有效值按 Tenant 显式绑定、平台默认绑定、明确未配置错误解析。 |
| Input Resource Resolution | 输入资源解析与确认 | 将自然语言中的输入数据意图提取为候选资源，经过 owner 校验并形成可供领域生成器消费的 `ResourceFact` 的共享能力。 | 与 Query、Workflow、Notebook、Transfer 等领域生成场景正交；各场景通过策略声明引擎范围、资源类型、数量和 Session 候选边界，不重复实现发现与确认流程。 |
| inference credential | 推理凭据 | Provider Connection 用于访问上游模型服务的 API Key 或等价认证材料。 | 由 Inference 使用部署级 `ENCRYPTION_KEY` 加密；API 只返回 `configured` 和 `version`，不返回明文、掩码或可复用引用。 |

## 身份与授权

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| IAM | 身份与访问管理 | 管理主体身份、认证方式、成员关系、角色、权限、会话和访问治理的统一体系。 | System 是 ADDP 唯一 IAM 逻辑权威；业务资源授权和最终判断仍归对应 owner 模块。 |
| Platform Realm | 平台管理身份域 | 承载 ADDP 平台运维、安全管理、审计和 Tenant 生命周期管理的身份域。 | 不属于任意 Tenant；平台管理权不自动产生 Tenant 业务数据权限。 |
| Tenant | 租户 | ADDP 中企业、学校、研究机构或独立业务主体的数据与权限最高隔离边界。 | 不等于 Department、Project Group 或外部 IdP 组织。 |
| User | 用户 | ADDP 内部的自然人身份，是审计、授权和资源归属的稳定主体。 | User 不等于登录账号；同一 User 可以拥有多个 Tenant Membership。 |
| Local Account | 本地账号 | 由 ADDP 管理的登录标识和本地凭据。 | 是 User 的一种登录账号，不是 User 本身。 |
| External Identity | 外部身份 | 外部 IdP 中通过 `issuer + subject` 唯一标识、并映射到 ADDP User 的身份。 | 邮箱不是外部身份的永久唯一键；外部 Token 不直接进入业务模块。 |
| Tenant Membership | 租户成员关系 | User 或 Service Principal 进入某个 Tenant 的有效关系。 | 一个主体可有多个 Membership，但一次业务会话只能选择一个当前 Tenant。 |
| Principal | 授权主体 | 一次请求中接受授权判断的主体，可以是 User 或 Service Principal。 | 不等于 OAuth Client、Department、Project Group 或 Role。 |
| Service Principal | 服务主体 | 应用、自动化任务或工作负载使用的非人 Principal。 | 不伪装成 User，不使用用户密码，也不得持有平台三员角色。 |
| Authentication Method | 认证方式 | 主体证明身份的方法，例如本地密码、Passkey、MFA、外部 IdP 或工作负载认证。 | CLI Authorization Code + PKCE 和 Device Flow 是登录交互通道，不是独立用户体系。 |
| Permission | 权限 | ADDP 产品定义的稳定、最小功能动作。 | Tenant 可以组合 Permission 创建 Role，但不能创造任意 Permission 字符串。 |
| Role | 角色 | Permission 的命名集合。 | Role 本身不表达业务资源实例；具体作用范围由 Role Assignment 和 owner Resource Grant / Policy 决定。 |
| Data Architect | 数据架构师 | 负责维护 Tenant 全局数据架构与建模约束的内置业务角色。 | 当前使用 `tenant.data_architect`，只允许 Tenant Scope 和 User Principal，负责业务实体、实体关系、逻辑模型、数仓分层、命名规范与质量 SLA。 |
| Role Assignment | 角色分配 | 将 Role 赋予 Principal，并声明 Platform、Tenant、Department 或 Project Group Scope 的授权事实。 | 不使用 `user_type` 同时表达身份类别和完整权限。 |
| Department | 部门 | Tenant 内表达稳定组织归属的层级组织单元。 | 一个 User 可有一个主部门和多个附加部门；父子部门权限默认不继承。 |
| Project Group | 项目组 | Tenant 内面向跨部门协作的成员集合。 | 严格属于单个 Tenant，第一阶段不嵌套，不改变成员的 Department 归属。 |
| Resource Grant | 资源授权 | owner 模块将特定资源动作显式授予 User、Department、Project Group、Role 主体集合或 Service Principal 的事实。 | 最终资源访问判断仍由 owner 执行；Asset 的授权记录可以是授权来源。 |
| Resource Scope Binding | 资源作用域绑定 | owner 模块将资源实例显式关联到 Department 或 Project Group Scope 的事实。 | 只用于判断 scoped Role Assignment 是否覆盖资源；不直接授予 Permission 或 Resource Grant。 |
| Resource Policy | 资源策略 | owner 模块基于资源生命周期、归属、可见级别、密级和业务条件执行的版本化授权规则。 | 第一阶段使用 owner 代码和结构化字段，不引入任意表达式 DSL 或中央策略引擎。 |
| Resource Access Rule | 资源访问规则 | owner 本地保存的结构化 Allow 或 Explicit Deny 记录，绑定资源、主体选择器、Permission、有效期和来源。 | `effect=allow` 时构成 Resource Grant，`effect=deny` 时构成 Explicit Deny；不进入 System IAM 中央表。 |
| Explicit Deny | 显式拒绝 | 对特定主体、动作、资源或条件明确拒绝的授权规则。 | 优先于 Allow，用于密级数据和例外隔离。 |
| Platform Three Administrators | 平台三员 | Platform System Administrator、Platform Security Administrator、Platform Audit Administrator 三个内置、互斥的平台管理角色。 | 替代永久 `super_admin`；不存在可合并三种职责的全权角色。 |
| Break-glass Grant | 紧急访问授权 | 在紧急处置中经双人批准产生的限定动作、限定时长且全程审计的临时授权。 | 不是常驻 root，不能删除审计记录或静默修改平台三员规则。 |
| Platform Statistics Viewer | 平台统计查看者 | 读取已发布跨租户聚合指标的独立平台只读角色。 | 不自动包含在平台三员角色中，不授予 Tenant 业务明细访问权。 |
| AuthContext | 授权上下文 | System 对访问令牌完成验证并基于当前 Principal、会话模式、Tenant Membership、Role Assignment 和客户端约束生成的权威身份与授权投影。 | 是 Go/Python 模块消费主体事实的唯一契约；不包含主体可访问的全部资源列表，`/users/me` 不是 Token 验证接口。 |
| OAuth Client | OAuth 客户端 | 代表 `addp-cli`、Codex 或 Hermes 等请求用户授权的客户端软件。 | 不是 ADDP 用户或租户；公共客户端使用 PKCE / Device Flow，不内置 Client Secret。 |
| OAuth Authorization Request | OAuth 授权请求 | OAuth Client 在打开浏览器前向 System 创建的短期、一次性授权上下文，持有已校验的 Client、redirect URI、Scope 和 PKCE challenge。 | 浏览器只携带随机 `request_id`；取消凭据只在客户端内存保存，System 只保存其 Hash。它不是 Authorization Code 或用户会话。 |
| OAuth Scope | OAuth 授权范围 | 一枚访问令牌被允许执行的最大能力集合。 | 只能缩小权限，不取代 Tenant Membership、Role Permission、owner 资源权限或审批。 |
| User Access Token | 用户访问令牌 | 以当前 ADDP 用户为主体、用于访问业务 API 的短期 Bearer Token。 | 通过 AuthContext 解析；不将客户端参数视为用户或租户事实。 |
| Refresh Token Family | 刷新令牌族 | 一次用户授权会话中经轮换先后产生的 Refresh Token 链。 | 旧 Refresh Token 重复使用视为泄露信号，必须撤销整个 family。 |
| Browser AuthSession | 浏览器认证会话 | 浏览器顶层页面持有的前端会话协调器，负责以内存保存 Access Token、通过 HttpOnly Refresh Cookie 静默恢复、跨标签页互斥刷新和 iframe Token 投递。 | Console 模式由 Console 持有；模块独立运行时由模块顶层页面持有。不得把 Access Token 持久化到浏览器存储。 |
| Browser Resource Access Ticket | 浏览器资源访问票据 | System 基于当前第一方浏览器会话签发、供原生图片、媒体、下载和三维资源请求使用的短期 opaque 凭据。 | 只保存 SHA-256 Hash，通过 HttpOnly、Owner Path 限定 Cookie 传输；只允许对应 Owner 明确声明的 GET/HEAD 资源路由消费，不进入 URL。 |
| Browser Session Capability Cookie | 浏览器会话能力 Cookie | 业务 owner 在 Bearer 已鉴权的会话创建请求成功后，为单个短期交互会话签发的 opaque HttpOnly Cookie。 | 只绑定一个会话 ID、owner 路径、Tenant、User 和到期时间；可代理该会话协议所需的方法与 WebSocket，但不能访问其他业务 API，不能替代 Browser Access Token 或只读 Resource Access Ticket。服务端只保存 Hash，关闭、过期、Context 变更或 owner 重启即失效。 |
| Notebook Kernel Capability Token | Notebook Kernel 能力令牌 | Develop 为单个 Notebook Interactive Session 签发并注入其隔离 Kernel process 的短期 opaque Bearer Capability。 | 只允许调用该会话的脱敏 Engine Runtime Descriptor、实时 Catalog 和受控只读数据代理；服务端只保存 SHA-256 Hash，不能访问其他 Develop API，也不能替代 User Access Token、Service Access Token、Execution Authorization、Notebook Session Authorization 或浏览器会话 Cookie。关闭、过期或 Develop 重启后即失效。 |
| Notebook Session Authorization | Notebook 会话授权 | System 从创建 Notebook Interactive Session 的当前 User AuthContext 派生并保存、绑定唯一 Session、Tenant、User、Task、Token Family、授权版本和有效期的短期授权事实。 | 允许 `addp-develop` 代表该 Session 执行实时 Catalog 发现，或为每次只读查询/扫描派生独立 Execution Authorization；不冻结 Engine 列表、不包含连接信息，本身不是 Token、Execution Authorization 或 Agent Delegated Access Token。 |
| Notebook Table Scan | Notebook 表扫描 | Notebook Native Engine Facade 对一个已由实时 Catalog 解析的表发起的流式只读执行。 | 每次扫描使用独立 execution、服务端 Cursor 和 Arrow IPC 流；返回扫描开始时的一致快照，不设隐式总行数上限，当前不支持断点续读。 |
| Delegated Access Token | 受委托访问令牌 | System 为 Agent 代表当前用户调用特定 owner 能力签发的短期、限 audience 和 Scope 令牌。 | 不改变原用户和租户；可绑定 AgentRun / ToolCall 用于审计。 |
| Runtime Service Principal | 运行时服务主体 | Develop、DuckDB Runtime、Workflow Runtime、Jupyter 等工作负载用于 Client Credentials 和控制面识别的 Service Principal。 | 只证明机器身份并消费与自身 audience 匹配的 Execution Authorization 或 Notebook Session Authorization；不继承发起用户、服务创建人、引擎创建人或 Tenant 全量数据权限。 |

## 配置管理

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| deployment configuration | 部署配置 | 模块连接自身持久化存储之前就必须可用的进程、网络和基础设施参数。 | 由根 `.env`、容器 environment 或部署系统注入；不是平台或 Tenant 普通运行配置。 |
| secret | 密钥配置 | 密码、API Key、Token pepper、加密密钥等不得以明文进入普通配置存储、响应、日志或审计详情的敏感材料。 | 由 Secret Manager、受控环境注入或专用加密凭据实体管理；只可暴露是否设置、版本或引用。 |
| platform configuration | 平台配置 | 在 Platform Realm 中管理、对整个 ADDP 部署或某个 owner 模块统一生效的普通运行配置。 | 平台级不等于 System-owned；配置语义、校验、存储和生效仍归 owner 模块。 |
| tenant configuration | 租户配置 | 在单一 Tenant Context 中管理、只对当前 Tenant 生效的普通运行配置。 | 必须绑定 AuthContext 中的 `tenant_id`；平台管理员不能在 Platform Realm 中直接读取或修改。 |
| configuration definition | 配置定义 | owner 模块对稳定配置 key、范围、类型、默认值、校验、敏感级别、Permission 和生效方式的声明。 | 代码默认值属于定义，不是与持久化值并行的第二事实源。 |
| effective configuration | 有效配置 | owner 按配置定义和范围规则解析后，供当前请求、任务创建或 execution 快照消费的唯一配置值。 | Tenant 可覆盖场景固定按 Tenant 显式值、平台显式默认值、定义默认值解析；不得追加环境变量 fallback。 |
| configuration management entry | 配置管理入口 | owner 模块通过 `addp.configuration-management/v1` 向 System 模块目录发布的模块级配置管理 UI 能力。 | 每个模块一个一级入口；模块内部的多个配置域由 Tab 或分组组织。只包含 entry id、owner、scope、owner 页面或 `/configuration/{owner}/...` Console 组合路由和 Permission；不包含配置键、当前值、Secret 或私有表结构。 |
| configuration snapshot | 配置快照 | 任务或 execution 在确定行为时固化的完整有效配置及版本。 | 平台或 Tenant 默认值后续变化不能改写历史快照；运行中的 execution 不热切换配置。 |

## Cleanup 与生命周期

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| cleanup | 系统级资源回收 | ADDP 中面向源事实、派生产物、物理产物、运行时缓存和任务定义残留的系统级资源回收与生命周期治理体系。 | 不是单一模块的失效数据删除；规范见 `docs/spec/addp-cleanup体系规范.md`。 |
| cleanup coordinator | 资源回收协调方 | 发起 cleanup request（资源回收请求）、记录任务元信息、汇总模块结果并展示审计的角色。 | 当前由 System 承担；不执行模块私有资源回收。 |
| cleanup executor | 资源回收执行方 | 评估和回收本模块 owner 范围内资源，并写回 cleanup result（资源回收结果）的角色。 | Meta、Manager、Transfer 等模块按 owner 范围分别承担。 |
| cleanup request | 资源回收请求 | 资源回收协调方发布的中性资源回收请求。 | 不携带模块私有表结构、bucket prefix 或物理删除规则。 |
| cleanup result | 资源回收结果 | 资源回收执行方写回的模块级资源回收结果。 | 包含通用摘要和模块私有统计。 |
| engine deletion impact assessment | 引擎删除影响评估 | Engine 删除前由 System 协调、各 owner 模块自治执行的无副作用 scan。 | 报告 `rebindable`、`will_disable`、`will_delete`、`running`、`external_artifact` 等影响；System 不读取业务模块私有表。 |
| impact digest | 影响摘要指纹 | owner 模块根据稳定资源 ID 和处理分类计算的确定性摘要。 | 用于比较只读预评估与 `deleting` 后权威复扫，摘要变化时必须重新确认。 |
| standard reference deletion guard | 标准引用删除屏障 | Model 为某个 Tenant 下的 Standard 资源引用键维护的本地串行化状态，用于协调 Standard 硬删除与 Model 新引用写入。 | 状态统一为 `open`、`frozen`、`deleted`；它是 Model 对 Standard 生命周期的安全投影，不复制 Standard 资源，也不替代 Standard 的事实所有权。 |
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
| CRS definition conversion | CRS 定义转换 | 在不改变几何坐标和 CRS 身份的前提下，把同一 CRS 的定义在 WKT、ESRI WKT、Proj4、PROJJSON 等表达之间转换。 | 不等于坐标重投影；当前由 GeoPython Workflow `crs_to_projjson` direct 算子执行。 |
| quick view | 快显 | Manager 空间预览中的高性能地图浏览模式。 | 快显是 UI 能力，不是任务，也不是瓦片缓存产物。 |
| preview state | 预览状态 | Manager 中记录某个 data item 的用户预览模式偏好和交互视角状态。 | 目标落点为 `manager.preview_state`；不依赖快显产物是否存在。快显能力、推荐结果和不可用原因由能力 API 动态合成。 |
| vector tile cache | 矢量瓦片缓存 | 为快显生成、由 Manager 管理生命周期的 infra PMTiles artifact。 | 不是 data item，不进入业务资源树，不允许被 Service 直接依赖。 |
| vector tile cache result | 矢量瓦片缓存结果 | 某个源 data item 当前可复用的 infra PMTiles artifact 状态。 | 目标落点为 `manager.vector_tile_cache`；属于 artifact state，不是 execution。 |
| vector tile cache generation task | 矢量瓦片缓存生成任务 | 为快显生成 infra PMTiles 的任务定义；当前不支持任务自身定时调度。 | 目标落点为 `manager.vector_tile_cache_tasks`，TaskProvider `task_type=vector_tile_cache_generation`。 |
| vector tile set generation task | 矢量瓦片集生成任务 | 把源空间 data item 生成或把合格缓存 artifact 固化为 Business PMTiles data item 的业务派生任务。 | 目标落点为 `manager.vector_tile_set_tasks`，TaskProvider `task_type=vector_tile_set_generation`；结果只存在于业务存储与 Meta，不设 Manager 结果表。 |
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

## 知识图谱

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| ontology entity type | 本体实体类型 | Graph 模块中对一类实体的概念定义，包含属性、约束、继承关系和图引擎执行映射。 | 不等同于 Neo4j label；具体执行映射由 `NodeLabels` 表达。 |
| node display property | 节点展示字段 | 本体实体类型指定的字符串属性，用作图谱节点标题、搜索结果标题和图分析节点名称。 | 可引用本类型或继承属性，并自动纳入该实体类型的全文搜索索引。 |

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
11. Notebook Native Engine Facade 的公开方法、参数和返回对象使用具体引擎原生术语；`CatalogPath`、`CatalogEntry` 和 `CatalogFacts` 只作为其内部实现契约，不要求 Notebook 使用者理解。
