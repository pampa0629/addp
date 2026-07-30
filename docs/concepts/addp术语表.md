# ADDP 术语表

本文统一 ADDP 文档中的基础术语。概念层文档优先使用中文术语，必要时在首次出现时标注英文。

## 资源与数据项

| 英文术语 | 中文术语 | 定义 | 备注 |
|---|---|---|---|
| engine | 引擎 | ADDP 连接和访问外部数据系统的能力入口。 | 例如 PostgreSQL、MinIO、NFS、Neo4j。 |
| Engine Instance | 引擎实例 | System 中一条绑定到确定物理端点的引擎登记事实。 | `engine_id` 只标识该实例；物理端点身份不可原地改变，端点变化必须创建新的 Engine Instance。 |
| Engine Runtime Descriptor | 引擎运行时描述 | System 面向受信 Runtime Service Principal 提供的脱敏 Engine Instance 控制面投影。 | 只包含实例身份、生命周期、能力声明和工作流/脚本运行时的 `protocol/host/port`；不包含数据引擎凭据、数据库连接参数或可直接读取业务数据的明文连接。 |
| engine lifecycle state | 引擎生命周期状态 | Engine Instance 当前能否被正常消费或正在退出平台的状态。 | 统一使用 `active`、`disabled`、`deleting`；`deleting` 保留连接只用于删除前 cleanup，不进入正常业务选择。 |
| external artifact abandonment | 外部产物放弃 | 当外部引擎不可达时，管理员明确接受平台不再删除某个 owner 已登记外部产物，并把后续处置交给外部系统管理员。 | 必须保留对象身份、最后错误、放弃时间和审计；不得伪装成物理删除成功。 |
| node | 资源节点 | 引擎内用于组织资源树的节点。 | 例如目录、bucket、prefix、schema。node 不等同于 data item。 |
| resource tree | 资源树 | 以树形方式展示 engine 内 node 和 data item 的视图。 | 用于浏览、展开、刷新和定位；不是新的身份层。 |
| resource tree search | 资源树搜索 | 在资源树视图内按名称、路径或轻量展示信息定位 node / data item 的浏览辅助能力。 | 不等同于全文检索或语义检索。 |
| resource | 资源 | 引擎 catalog 或资源树语境下的外部对象统称。 | 当讨论内容读写边界时优先使用 content / ref，避免把 engine 资源模型带入 format。 |
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
| output contract snapshot | 输出契约快照 | 对没有单一 Meta item 身份的查询或计算结果，保存其已检测输出字段、主键、空间信息等契约事实。 | SQL 查询服务使用该快照；查询结果未物化并经 Meta 扫描前，不创建或伪造 Meta item。 |
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
| profile observation | 剖析观察 | 根据字段级统计派生的描述性数据特征提示。 | 例如高缺失、常量、高基数和分布偏斜；不是质量问题或质量评分。 |

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
| CDC bootstrap | CDC 初始化 | 在一个无空洞的日志衔接点上建立一致性初始快照，并继续消费快照期间和之后产生的日志变化。 | PostgreSQL 第一版固定使用 Debezium `initial` snapshot；Transfer 不自行拼接“先全量、后开 CDC”两条路径。 |
| apply mode | 目标应用方式 | Transfer 将本次读取结果应用到目标的策略。 | 稳定取值为 `replace`、`append`、`upsert`、`upsert_delete`；目标 Provider 必须声明并真实实现对应能力。 |
| sync state | 同步主状态 | Transfer 为增量任务保存的已提交源位置。 | 存储于 `transfer.sync_states`；与任务定义、execution checkpoint 分离，只能在目标提交成功后通过 CAS/fencing 推进。 |
| capture position | 捕获位点 | 捕获组件已经从源数据库事务日志可靠读取并写入 Infra Kafka 的源日志位置。 | PostgreSQL CDC 对应 Debezium/Kafka Connect 管理的 LSN 与 connector offset；Transfer 不复制维护该位点。 |
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
| ChangeEvent | 统一变化事件 | Transfer source adapter 将 Kafka record 或 CDC envelope 归一化后的内部变化对象。 | 业务 Kafka record v1 归一化为 `operation=upsert`；PostgreSQL CDC v1 契约归一化 snapshot/upsert/delete。Kafka、Debezium 和数据库日志协议细节不得进入目标 writer。 |
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
| Public Operator Spec | 公开算子规范 | 面向用户、前端、AI 和 Develop 任务定义的算子公开契约，声明公开参数、资源选择方式和校验规则。 | 资源身份使用 `locator` 或 `target_parent_locator + target_name`；不得暴露 `connection_info` 等内部连接参数。 |
| Develop Adapter Spec | Develop 适配规范 | Develop Backend 按工作流引擎类型和算子 ID 选择的显式执行前转换契约，声明公开资源参数如何派生为运行时参数。 | 负责查询 System Engine Instance、派生 `connection_info/schema/table/path` 并移除公开资源参数；不得按参数名隐式触发。 |
| Runtime Operator Spec | 运行时算子规范 | Workflow Runtime 实际执行算子时消费的内部契约，只声明运行时真实需要的参数、输入输出端口和执行行为。 | 不解析 ADDP `ResourceLocator`，不承载资源树 UI 配置；`connection_info/schema/table/path` 属于适配层到运行时的内部参数。 |
| Workflow Access Plan | 工作流访问计划 | Develop、Manager 等调用方把已解析的存储资源转换为 Workflow Runtime 可执行读写计划的内部契约。 | 当前版本为 `addp.workflow.access-plan/v1`；只在执行期携带 `mounted_path` 或 `object_store` 访问参数，不作为用户任务定义、资源身份或长期事实源。 |
| Execution Effect | 执行效果 | 一次计算对数据或外部系统可能产生的效果分类。 | 固定为 `read`、`write`、`ddl`、`external_effect`；工作流按全部算子的最高效果收窄授权，不能由客户端自报后直接信任。 |
| Execution Authorization | 执行授权 | System 从当前 User AuthContext 派生、绑定唯一 execution、Tenant、owner audience、Engine Instance、允许效果、授权版本和有效期的短期授权事实。 | 不是 Role、Service Principal Permission、OAuth Scope 或第二种 Tenant Membership；只允许匹配 audience 的 Service Principal 消费，不保存或转发原始 User Access Token。 |
| Task Authorization Subject | 任务授权主体 | 持久任务定义为定时或延迟执行绑定的 User、Tenant Membership 和授权版本事实。 | 只能由同 Tenant 的当前 User AuthContext 在创建、更新或显式重新授权任务时写入；不保存 Access Token。任务定义变化或授权版本变化后必须重新授权，执行开始时仍需重新校验 Membership、Role、资源规则和授权版本。 |
| Managed Compute Session | 受控计算会话 | Develop 按 Execution Authorization 创建并管理的 SQL、Workflow 或 Jupyter 执行会话。 | Runtime 只获得本次执行所需的短期访问能力；Jupyter 不再直接获得长期 Engine 凭据或共享 Lab 的无限制数据访问。 |
| workflow_def | 工作流定义 | ADDP 工作流运行时协议中的 DAG 定义结构。 | 由 ADDP 前端和后端消费；不得直接等同于某个引擎的私有 DAG JSON。 |
| SuperMap SPS | SuperMap SPS | SuperMap GPA / 处理自动化模型的 Java 扩展与执行框架。 | 当前按 `sps-core`、`IWorkflow`、`IProcess`、`IDataItem`、`WorkflowExecutor` 等已验证能力使用；不在 ADDP 文档中硬展开 SPS 缩写。 |
| SPS Process | SPS 处理节点 | SuperMap SPS 工作流中的可执行节点，实现或等价实现 SPS `IProcess`，声明 input / output 并由 `WorkflowExecutor` 调度。 | 在 `supermap_workflow` runtime 内部使用；不是独立 OS 进程、独立 HTTP 服务或独立容器。 |
| SuperMap Algorithm | SuperMap 算法 | SuperMap iObjects Java / native / iObjectSpy 等实际完成空间分析、数据处理或格式转换的底层能力。 | 通常由 SPS Process 的 `execute()` 调用；不直接暴露给 ADDP 前端。 |
| supermap_workflow | SuperMap 工作流运行时 | ADDP 工作流运行时类型，对外实现 `addp.workflow/v1`，对内使用 Java SuperMap SPS 执行 DAG。 | 设计与验证记录见 `docs/next/SuperMap工作流运行时设计.md`。 |

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
| Delegated Access Token | 受委托访问令牌 | System 为 Agent 代表当前用户调用特定 owner 能力签发的短期、限 audience 和 Scope 令牌。 | 不改变原用户和租户；可绑定 AgentRun / ToolCall 用于审计。 |
| Runtime Service Principal | 运行时服务主体 | Develop、Workflow Runtime、Jupyter 等工作负载用于 Client Credentials 和控制面识别的 Service Principal。 | 只证明机器身份并消费与自身 audience 匹配的 Execution Authorization；不继承发起用户、引擎创建人或 Tenant 全量数据权限。 |

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
