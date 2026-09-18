# ADDP Model 概念与数据约束规范

## 一、模块边界

文本逻辑类型统一为 `string`，不另设 `text`；PostgreSQL 物理映射为有最大长度时 `VARCHAR(n)`、未限定长度时 `TEXT`。已有 `text` 属性和逻辑字段迁移为无长度约束的 `string`，保持原来的物理文本语义，并推进受影响聚合版本。历史执行与指标依赖快照不改写；版本或依赖发生漂移时按现有流程重新审批或发布。

Model 是 Tenant 级数据架构与建模事实的 owner，管理业务实体、实体关系、逻辑模型、数仓分层、公共/一致性维度、维度层级和指标实现。Standard 拥有业务域、数据元、码值集和指标定义等业务语义契约；Model 保存经过 Standard API 验证的长期引用，并在聚合审批或指标实现发布时冻结确定的标准修订，不代理或复制 Standard 资源。

维度层级是模型内部结构，只能引用 Model 的 LogicalTable / LogicalField，并可通过字段冻结的数据元修订获得统一语义。DimensionHierarchy、层级成员、API 和前端编辑入口已整体归入 Model；Standard 旧表、API、权限与前端路线以及 LogicalField 上的 `hierarchy_id + hierarchy_level` 已删除。

指标定义与指标实现必须分离：Standard MetricDefinitionRevision 只描述业务含义、统计口径、单位、非引擎可执行的语义表达，以及修订级指标语义依赖；依赖在草稿中引用指标定义稳定身份，发布时冻结为确定的已发布修订。Model MetricImplementation 同时保存 `metric_definition_id` 并冻结 `metric_definition_revision_id`，拥有粒度、事实来源、维度、连接、过滤和可执行表达式。同一指标定义可存在多个模型实现。FactMetricMapping 和 Standard Metric 的 `derivation_config` 是旧实现，迁移时直接删除，不保留并行路径。

Model 负责已审批逻辑表的目标物理表创建、结构校验与显式删除；Develop 负责计算并写入已存在表，Quality 负责目标表数据校验，Orchestrator 负责依赖、调度和完整流程。建表不计算数据，数据刷新不依赖发布组或暂存批次。

### 企业目录接入边界

Model Entity 与 LogicalTable 是企业目录的专业资源来源。所有已持久化资源，包括 `draft`，都通过 Model owner-local 可恢复变化源自动建立 CatalogEntry；Catalog 不调用 Model 列表 API 轮询全表，也不向 Model 回写 CatalogEntry ID。

Model 继续权威拥有完整 Entity、LogicalTable、属性、字段、物理目标配置、专业生命周期，以及 `domain_id`、`element_id + element_revision_id`、DimensionHierarchy、MetricImplementation 和建模关系。Catalog 只保存稳定来源引用与名称、编码、对象种类、专业状态等最小最后观察摘要；完整详情通过 Model 的 Catalog 专用批量解析接口动态读取。Catalog 投影不得用于恢复 Model，不得接受编辑，也不得被 Model 反向读取为专业事实。

Model 的 Domain、ElementRevision、MetricDefinitionRevision 和模型关系是专业内生关系，不复制为 Catalog 人工语义关联。Catalog 可以将其展示为 owner 声明关系和搜索分面；修改仍使用 Model 唯一写路径。Catalog 自身继续拥有目录业务名称、补充说明、责任、治理、收藏、集合，以及实际字段/组件到标准修订的落标映射。

Model 面向当前 User AuthContext 提供 `GET /entities/{id}/relations` 与 `GET /logical-tables/{id}/relations` 一跳专业关系图。Entity 路由同时要求 `model.entity.read` 与 `model.entity_relation.read`；LogicalTable 路由要求 `model.logical_model.read`。响应遵循 `addp.professional_relations/v1`，只读取 Model 权威表，不调用 Standard、Catalog 或其他 owner，不保存 CatalogEntry ID，也不把 owner 不可达提升为 Model Ready 条件。

Model 变化捕获固定使用 Entity 与 LogicalTable 聚合根上的 PostgreSQL trigger，在业务写事务中追加 `model.catalog_resource_changes`。聚合子资源写入已经必须推进根版本，因此不得再在各 Service 添加 append 回调或双写。Catalog 以 opaque cursor 拉取并可从历史起点重放；Model 或 Catalog 暂时不可达只造成同步滞后，不参与对方 Ready。

PostgreSQL 建表语句预览只读，不改变生命周期或创建物理资源。真实创建使用独立结构操作，不增加 LogicalTable 状态。

### 目标物理表建表

Model 声明 `logical_table_materialization` TaskProvider；已持久化的 LogicalTable 是任务定义事实源，任务 ID 等于逻辑表 ID，不新增或复制建表配置。仅已审批且配置目标的逻辑表可执行。手动入口和 Orchestrator 入口复用同一执行服务，写入 `common.task_executions`；执行冻结逻辑表版本，运行时必须匹配该版本。成功输出 `execution_id + target_locator`。

建表使用公共 bounded execution 领取和租约协议；先签发精确引擎 read+ddl 授权再领取，执行上限 45 秒、租约 90 秒。过期租约收敛失败，不自动重放；用户重试产生新 execution。后台退出停止领取并取消在途执行。任务版本改变、草稿或结构冲突明确失败；不自动 ALTER。任务发现和状态协议沿用 TaskProvider，人工查看执行记录使用 Monitor。


Model 只负责由已审批 LogicalTable 创建或校验目标物理表，以及显式删除仍由该逻辑表管理的目标表；不拥有数据刷新、暂存批次、封存或原子发布组。删除 MaterializationGroup、MaterializationBatch、物化读上下文和 prepare/seal/publish TaskProvider，不保留兼容入口。

`POST /logical-tables/{id}/materialized-target` 使用当前用户授权及逻辑表 `version`，返回 202 和统一执行标识。TaskProvider 执行入口仅接受 addp-orchestrator 与有效父 execution，二者调用同一服务，交由 Model Backend 内嵌执行方领取后在目标 PostgreSQL 事务中建表。目标不存在时创建；已存在时必须归属本逻辑表且结构完全一致，幂等成功并保留记录。结构不一致明确拒绝，不自动丢弃或替换表。结构升级需要单独明确设计，当前不猜测 ALTER 或破坏性重建。

`DELETE /logical-tables/{id}/materialized-target` 要求精确目标确认和版本，只删除属于当前逻辑表的目标物理表。创建、校验及删除在控制库锁定 LogicalTable，在目标库对目标串行化；不改写物理目标配置。物理结构操作不执行数据加工或质量检查。

Develop 查询任务通过 ResourceLocator 指定已存在的正式输出表；在同一目标事务内执行覆盖或追加，不负责模型 DDL。Orchestrator 按任务依赖调度可选建表步骤、计算和 Quality 数据校验；任何步骤失败不回滚其他已提交步骤。Quality 校验读取正式表，不引用 Model 私有资源，也不执行发布。

逻辑表删除仍需草稿、版本匹配且显式移除物理目标配置；不再检查已删除的组或批次。历史 common.task_executions 保留原始审计事实，不作为可执行旧任务定义。

## 二、授权边界

Model 资源当前全部属于 Tenant，不存在 Department 或 Project Group Resource Scope Binding。所有 `model.*` Permission 只允许 Tenant Scope。`tenant.data_architect` 是面向 User Principal 的完整 Model 管理角色；`tenant.graph_runtime` 只保留 Graph 导入所需的 Entity 和 EntityRelation 只读权限。

创建或校验目标表使用 `model.materialization.execute`；删除目标表使用 `model.materialized_target.delete`。两者均由用户身份派生目标引擎 read + ddl 授权。

`model.catalog.read` 是不可由租户自定义的 Tenant Scope 机器权限，只授予 `tenant.catalog_runtime`，并由 Model 的变化流和 Catalog 批量解析路由同时校验固定 `addp-catalog` OAuth Client。该权限不授予用户读取 Model 管理 API，也不允许 Catalog 写入 Model。

Model 在写入前校验 Standard 引用时，不转发或保存 User Access Token。`addp-model` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.model_runtime`，目标上只读取 Standard 的 Domain、Element/ElementRevision 与 MetricDefinition/MetricDefinitionRevision，不再拥有 `standard.dimension_hierarchy.read`。Standard 协调被 Model 引用资源的删除时，`addp-standard` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.standard_runtime`，该角色只包含 `model.standard_reference.update`。平台控制面的 Runtime Role 不参与 Tenant 业务引用校验或删除协调。

Permission Guard 只判断候选能力，Repository 和 Service 仍必须对每个资源及其子资源执行 Tenant 隔离。任何父子写入、删除和关系创建都必须验证完整归属，不能只依赖请求中的全局 ID。

## 三、聚合与引用

- Entity 聚合包含 EntityAttribute；EntityRelation 是连接两个 Entity 聚合的独立关系事实。
- LogicalTable 聚合包含 LogicalField、TableRelation、DimensionHierarchy 和概念实现映射；维度层级成员只能引用同一 LogicalTable 的字段。MetricImplementation 是独立聚合，首期声明一个来源事实表并显式引用其维度关系。
- Entity / EntityAttribute / EntityRelation 属于概念层；`fact|dimension` LogicalTable 直接组成维度逻辑模型。概念模型不要求先生成一套 `table_type=entity` 的中间逻辑表；只有业务明确需要独立规范化核心层时才使用 `entity` 角色，并由 Develop 的确定加工任务派生下游事实/维度表。
- 概念实现映射是 Model 内唯一跨层语义事实：LogicalTable 到 Entity 使用 `represents|derives_from`，LogicalField 到 EntityAttribute 使用 `direct|derived`，TableRelation 到 EntityRelation 使用 `same|inverse` 表达实现方向。一个逻辑对象可以引用多个概念来源；旧的 `logical_tables.entity_id` 单值来源指针删除，不保留并行路线。
- 概念实现映射只表达设计来源、语义追溯和一致性约束，不复制字段定义、不执行数据加工、不根据上游变化隐式改写下游。映射通过逻辑表的 `GET/PUT /concept-mappings` 读取和完整替换，所有写入校验并推进父 LogicalTable `version`；仅草稿逻辑表可修改。
- 保存映射时只允许引用当前 Tenant 中已审批 Entity、属于该 Entity 的 EntityAttribute，以及两端实体均已审批的 EntityRelation。映射冻结对应 Entity / EntityRelation 的当前并发版本；LogicalTable 审批时必须拒绝来源退回草稿、版本漂移、字段归属错误或关系端点与表映射不闭合。重新保存完整映射是数据架构师确认上游变化并建立新基线的唯一动作。
- DWLayer 是 Tenant 可配置事实。LogicalTable 必须引用已存在的 DWLayer，前端不得维护固定分层枚举作为第二事实源。
- Model 内部引用由数据库外键、唯一约束和 CHECK 约束保证；跨 Standard Schema 的引用先由 Standard HTTP API 验证，再在 Model 写事务中锁定对应的标准引用删除屏障。后台调用、Mermaid 导入和普通 API 写入必须使用同一屏障路径。

### 与数据质量的边界

Model 拥有结构约束，Standard 拥有可复用值约束，Quality 拥有检查方案、阈值、执行和问题治理。DWLayer 只定义分层与命名规范，删除无执行语义的 `quality_sla` 列及请求字段，不在 Model 预置另一套质量策略。

EntityAttribute 和 LogicalField 的 `nullable` 必须原样保存，ORM 默认值不得把显式 `false` 改成 `true`。已有数据缺乏原始输入证据时不猜测修正。主键固有的非空语义在结构约束投影中始终成立。

LogicalTable 详情返回只读 `structural_constraints`，从同一数据库快照中的 LogicalField 与出向 TableRelation 派生，不增加规则表或编辑入口：`primary_key` 是全部主键字段组成的一个组合键；`required_fields` 包括主键字段和显式 `nullable=false` 字段，两者成员均为 `{field_id, column_name}`；`foreign_keys` 复用 TableRelationDetail，仅包含 `relation_type=fk` 的出向关系，普通 `join` 和入向关系不作为本表外键。空集合使用空数组。该投影随逻辑表定义读取，草稿只表示设计，不能当作已执行检查结果。

`grain_description` 保留业务粒度说明，不从自由文本猜测唯一键。机器可读的主键组合复用字段 `is_pk`，不建立重复的 grain/key 配置。逻辑表详情集中展示结构约束；修改仍通过字段和维度关联的既有唯一入口完成。数据元修订引用继续由审批冻结。下游如何发现、应用这些事实留待 Catalog 与 Quality 契约讨论，本阶段不建立跨模块执行依赖。

### 数据元修订冻结

EntityAttribute 与 LogicalField 在草稿阶段只维护长期引用 `element_id`，`element_revision_id` 必须为空。Entity 或 LogicalTable 审批时，Model 使用同一个审批时点批量解析全部 `element_id` 对应的 Standard 当前生效修订，并在本地审批事务中把结果冻结到各属性或字段的 `element_revision_id`；任一数据元在该时点没有生效修订时，审批整体失败且不产生状态、版本或冻结字段副作用。

审批后的 DDL、目标物理表、质量规则和历史展示必须以被冻结的 `element_revision_id` 为语义事实，不得动态跟随 Standard 后续生效修订。退回草稿聚合时，Model 在同一事务中把聚合转回 `draft` 并清空所属属性或字段的 `element_revision_id`；再次审批重新按新的统一审批时点解析。`element_revision_id` 是审批快照，不接受前端写入，也不建立绕过聚合审批的单独更新接口。

引入冻结字段时，历史已审批聚合如果含有 `element_id`，不能仅凭当前 Standard 状态反推当初审批时使用的精确修订。迁移必须将这类聚合转回 `draft` 并推进版本，由用户在确认后显式重新审批；禁止用迁移时的当前修订伪造历史快照。

### Standard 引用删除屏障

Standard 的业务域、数据元和指标定义被 Model 引用时，不使用跨 Schema 外键，也不允许 Standard 直接读取 Model 私有表。Standard 硬删除这些稳定身份必须通过 Model 的标准引用删除屏障完成影响评估；已发布且被模型冻结引用的修订不得硬删除。一次性“查询无引用后直接删除”存在检查与删除之间的竞态，禁止作为正式路径。DimensionHierarchy 迁入 Model 后是本地聚合，不再参与跨 Standard 删除协调。

Model 为 `(tenant_id, resource_type, resource_id)` 维护单行屏障，状态只允许 `open`、`frozen`、`deleted`。任何可能写入 `domain_id`、`element_id` 或 `metric_definition_id` 的事务，必须按稳定顺序创建并锁定对应屏障行，只有 `open` 才允许继续；屏障状态检查、Model 业务写入、资源版本和 Tenant 实体模型集合 `revision` 推进必须处于同一事务。Standard HTTP 校验仍在本地事务前完成，不能持有 Model 行锁等待网络。

Standard 删除遵循唯一顺序：Standard 先持久化删除协调记录并将资源置为 `deleting`，再由同一删除协调流程串行锁定 Standard 资源行，调用 Model 原子冻结屏障并权威扫描当前引用。协调流程必须在释放 Standard 资源行锁前完成冻结、引用分支和本地硬删除；这样用户重试、后台补偿和并发删除不会同时执行本地删除。有引用时必须先让 Model 恢复 `open`，再提交 Standard `active`；任一恢复步骤失败都保留 `deleting` 协调记录，供后台补偿继续处理。无引用时 Standard 硬删除资源并保留协调记录，直到 Model 屏障终止为 `deleted` 成功；因此即使资源已硬删除而终态通知响应丢失，后台补偿仍能完成终态收敛。冻结事务与所有新增引用事务锁定同一屏障行：冻结前完成的写入必然进入权威扫描，冻结后到达的写入必然失败，因此不存在在途请求越过扫描的窗口。

`frozen` 和 `deleted` 都禁止新增或保留目标引用；`deleted` 是不可逆终态，防止删除完成前已通过 Standard 校验、但较晚进入 Model 事务的请求在资源删除后落库。协调调用失败时不得绕过屏障继续删除。资源已进入 `deleting` 时重复删除必须复用同一条协调记录并从冻结和权威扫描继续，不能创建第二条强制删除路径。Model 冻结前或本地删除失败时，只能在 Model 成功 `open` 后再恢复 Standard `active`；如果 `open` 失败，资源保持 `deleting`，后台补偿必须重试。已经完成硬删除但终态通知失败时协调记录和 Model 屏障都保持待收敛状态，后续只允许补做 `deleted` 终态；协调记录不得因本地资源已不存在而丢失。

## 四、生命周期

Entity 和 LogicalTable 当前生命周期统一为 `draft` 与 `approved`。只有 `draft` 可修改；审批前必须完成聚合校验。Entity 必须至少包含一个定义完整的属性且至少一个属性为主键；LogicalTable 必须至少包含一个字段且至少一个字段为主键。审批失败必须通过稳定错误码和本地化消息指出缺少属性、字段或主键等具体前置条件，不能统一降级为通用请求校验失败。已审批资源如需修改，必须通过显式退回草稿操作回到 `draft`，不能在 `approved` 状态直接修改子资源。

详情页打开、引用回显、查看或复制 DDL 均为只读行为，不改变模型，也不得产生未保存状态。已审批或无更新权限时，物化目标只读展示已有 ResourceLocator；草稿编辑中的资源选择器状态不是模型配置的第二事实源，初始化回显失败或未选中不得清空已保存目标。只有用户显式选择新目标或清空配置才能修改物化目标。

未保存状态按可提交的页面数据与保存基线、弹窗数据与打开时基线分别比较；打开或关闭未修改的弹窗、异步引用加载不视为编辑。弹窗真实修改后的关闭须确认放弃，成功提交只清除对应编辑范围的脏状态。审批或退回草稿成功后必须同步重新读取字段等聚合内容，展示与后端冻结修订一致。

`materialized` 不属于当前正式状态。租户资源回收的 logical 模式可以将已审批资源退回草稿为 `draft`，physical 模式必须在单个数据库事务中按聚合顺序删除。

### 完整更新语义

Model 资源统一使用 `PUT` 表达完整更新，不提供并行的部分更新路径。请求必须携带资源的完整可编辑状态；`domain_id`、`element_id`、`length` 等可空字段使用 JSON `null` 表示解除引用或清空值。缺失的可空字段与 `null` 含义一致，不能被解释为保留旧值；必填字段缺失或为空必须返回 `400 invalid_request`。维度层级及成员通过 Model 聚合写接口维护，不在 LogicalField 上保留 `hierarchy_id` 或 `hierarchy_level` 双重事实。

前端保存详情和子资源时必须提交完整表单状态。对于当前页面不直接编辑但属于资源状态的字段，例如属性、字段的 `sort_order`，前端也必须从已加载资源中原样带回，不能依赖后端保留旧值。概念实现映射不混入 LogicalTable 基本信息 PUT，只能经唯一的完整替换接口写入。

### 并发版本与聚合写入

Model 遵循平台 API 规范中的资源并发版本规则。`Entity`、`LogicalTable`、`DWLayer` 和 `EntityRelation` 是独立版本主体，数据库均保存非空 `BIGINT version`，创建时从 `1` 开始。`EntityAttribute` 共用所属 `Entity.version`；`LogicalField`、`TableRelation`、`DimensionHierarchy`、层级成员和概念实现映射共用所属 LogicalTable 的 `version`，这些聚合子资源不得再建立自己的并发版本。

| 写入对象 | 并发版本主体 | 事务边界 |
| --- | --- | --- |
| Entity 基本信息、审批、退回草稿、删除 | Entity | 按 `tenant_id + id + version` 条件写入并推进 Entity 版本 |
| EntityAttribute 新增、更新、删除 | 所属 Entity | 校验 Entity 为 `draft`、写入属性并推进 Entity 版本 |
| LogicalTable 基本信息、审批、退回草稿、删除 | LogicalTable | 按 `tenant_id + id + version` 条件写入并推进 LogicalTable 版本 |
| LogicalField 新增、更新、删除 | 所属 LogicalTable | 校验 LogicalTable 为 `draft`、写入字段并推进 LogicalTable 版本 |
| TableRelation 新增、更新、删除 | 事实侧 LogicalTable | 写入时锁定并校验事实表为 `draft`；新增和更新同时锁定目标维度并校验同租户、表类型与字段，允许引用已审批维度。只推进事实表版本，不改变维度审批及冻结修订 |
| DimensionHierarchy 及层级成员新增、更新、删除 | 所属维度 LogicalTable | 校验维度表为 `draft`、层级字段均属于该表、`level_num` 从 1 开始且不重复，写入并推进 LogicalTable 版本 |
| 概念实现映射完整替换 | 所属 LogicalTable | 校验逻辑表为 `draft`、概念来源已审批、字段与关系归属闭合，替换三类映射并仅推进一次 LogicalTable 版本 |
| MetricImplementation 新增、草稿修改、发布、撤回、删除 | MetricImplementation | 使用独立版本；来源必须为已审批事实表；冻结发布的指标定义修订并校验依赖，不修改事实表状态或版本 |

| EntityRelation 更新、删除 | EntityRelation | `PUT` 携带完整端点和关系定义；校验关系版本，同时锁定并校验变更前后涉及的全部 Entity 均为 `draft` |
| EntityRelation 创建 | 创建时无关系版本 | 同一事务锁定并校验两端 Entity 均为 `draft`，新关系版本从 `1` 开始 |
| DWLayer 更新、删除 | DWLayer | 更新按版本条件写入；删除在同一事务完成版本校验、LogicalTable 引用检查和删除 |

### 指标实现独立修订

MetricImplementation 使用稳定身份与不可变修订，稳定身份保存来源事实表、指标定义身份、名称和自身 `version`；修订保存指标定义发布修订、结构化 `contract`、依赖快照及 hash。修订状态为 `draft|published|withdrawn`，同一实现最多一个草稿。已发布内容不可修改；撤回后拒绝新执行。未发布身份可删除，曾发布身份保留。旧 `source_config/dimension_config/filter_config/expression_config` 和 `active|disabled` 写路径删除。

首期 `contract.operation=count_distinct`；`subject/distinct/time` 使用 `{field_id, relation_id}` 引用，`relation_id=0` 只表示来源事实自身。`subject_relation_id` 指向主体维度唯一主键，用于区分不存在主体与零活动。`filters` 只接受 `{field,value:boolean}` 固定条件，不接受 SQL。时间字段必须为 DATE。输出为 `subject_id,bucket,value`；查询参数为 `subject_id,start_date,end_date,grain`，粒度仅 `month|total`，时间左闭右开，最多 120 个相交月份。月查询对已存在主体补零，不存在主体返回空结果。编译目标由来源引擎的 AnalyticalCompilerProvider 决定，缺少能力时明确拒绝。

`contract.operation=directional_overlap` 复用同一事实来源、主体字段、主体维度、集合成员去重字段与 DATE 字段；两个人员角色共享字段映射，角色由必填 `subject_id` 和 `comparison_id` 明确绑定。此操作不接受额外固定过滤，避免把主领队过滤或未定义作用范围的条件带入参加活动集合。额外必填参数 `directions=forward|both` 选择单方向或交换角色的双向组合；两者共用一次 PreparedQuery 执行和同一快照。任一人员不存在时整次结果为空；人员存在但集合为空时对应方向为 0；同人非空为 1。

双向结果每方向每个时间桶一行，输出 `direction,subject_id,comparison_id,bucket,value,subject_count,comparison_count,shared_count`；`direction=forward|reverse` 区分有序角色，即使两个人相同也保持唯一行键 `direction,bucket`。`value` 为未按展示精度提前舍入的 decimal 比例，三个 count 为解释字段，不另建指标。月份比例和全期比例各自从范围内集合计算。先分别去重形成双方完整集合，再计算交集和分母，禁止从内连接结果统计分母。质量检查覆盖两个人员。

Model 计划包包含参数声明、输出字段和稳定键，由 Service 冻结消费并只读投影；Service 不按操作名称拼装参数、类型或公式。既有 count_distinct 的四个参数和输出保持其定义，新增操作不为计数查询引入对比人员参数。

指标实现详情以只读“数据来源”展示所选已保存修订的引擎身份、参与计算的逻辑表及物理表，绑定事实只取该修订的依赖快照，不以逻辑表当前物理目标替换历史来源。逻辑表名称和引擎名称可作为当前显示信息补充；无引擎目录读取权限或名称查询失败时仍展示冻结的引擎 ID。逻辑表入口导航到其当前“物理目标”页签。尚未保存或正在编辑时明确区分已保存来源与待保存配置；缺少来源快照时提示不可用，不推测绑定。详情与“发布查询服务”弹窗共用 Model 的 `MetricSourceSummary` 领域组件；弹窗同时展示实现名称、所选修订和来源，禁用来源导航，新建服务与替换来源提交的修订必须与摘要一致。此展示复用现有详情、Meta 引擎目录和共享定位符能力，验证沿用 `make test-model-frontend` 及已登记的 Model 前端 CI 门禁。

指标实现详情提供“引用此修订的服务”只读入口，按当前已保存的实现 ID 与修订 ID 查询 Service 的现有管理列表。Service 是引用关系的唯一事实源，Model 不保存反向引用。仅持有 `service.definition.read` 的用户可查询；列表包含当前租户内所有状态的精确绑定服务，支持分页、刷新与跳转详情，不自动重绑。权限不足、查询失败和无引用必须区分。

撤回已发布修订前必须显示确认弹窗，按同一精确绑定查询的 `total` 展示当前引用服务数量（包含已停用服务），明确撤回后引用服务的新查询将被拒绝。数量是查询时的提示，不是锁定引用关系的事务性保证；每次打开或刷新重新查询，不复用列表缓存。查询期间禁止确认；缺少服务读取权限或查询失败时明确数量无法确认，不得显示为零，也不新增撤回权限条件。确认仍使用 `model.metric_implementation.offline` 与实现版本，取消不写入；切换实现、修订、编辑态或实现版本变化时关闭弹窗，避免对其他目标执行撤回。

#### 数据库无关计划与修订

指标实现可选声明 `subject_label` 字段引用，必须来自 `subject_relation_id` 指向的同一主体维度，且类型为 `string`。它表示查询时的当前显示名称，不是历史时点属性、指标分组键或静态字典；省略时不产生名称列。配置后计数结果追加 `subject_label`，双向重叠结果追加 `subject_label,comparison_label`，名称随结果行中的主体/比较方身份变化，原始 ID、计数、比例与稳定分页键不变。名称为 null 时保留结果行并返回 null，禁止用名称替代 ID 或猜测其他字段。

Model 将名称字段及关联纳入既有依赖快照与数据库无关计划，在指标聚合完成后使用主体维度的唯一键左连接名称；已有主体唯一性断言继续阻止重复维度放大结果。Service 通过现有 Model Client SDK 获取并冻结确定修订的计划包，执行、鉴权、输出血缘与 Consumer Descriptor 均沿既有路径。Service 不修改包、不增加逐行查询或前端补全接口。新增名称输出必须显式发布实现修订、重绑 Service，再更新并发布消费应用，不能修改旧修订。

验证沿用 Model 前端门禁、Go 单元测试、`make test-model-postgres` 与 `make test-model-mysql` 的同一套真实引擎样例，覆盖名称方向、空名称、零活动、重复主体和原指标值不变。无需增加 Provider、数据库分支、持久化表或 CI 服务依赖。

2026-09-17 本地验证已通过上述两个数据库门禁、Model Service/API 单元测试和 `make test-model-frontend`（38 项确定性测试、30 项浏览器用例及构建）。修订生命周期测试另验证名称字段的物理列变化会阻断旧发布包；Swagger 已重新生成并通过 68 个公开路由方法覆盖检查。真实指标的新修订与服务重绑尚待后端重启后执行。

参数展示信息使用 `parameter_presentation`，按参数名保存共享 `ParameterPresentation {labels, descriptions}`。新发布修订由 Model 根据统计主体字段及固定参数职责生成双语展示；中文主体名称来自逻辑字段名，英文采用通用的 Subject 名称，不在 Service 或引擎中推断“人员”。展示信息随 DependencySnapshot 冻结，读取已发布修订只能读取冻结值，不根据当前词条重新生成。它不参与计算依赖 hash；由 Service 消费契约指纹覆盖其变化。缺省表示该修订未声明展示信息，不自动追补旧修订。

指标定义和 `MetricContract` 不包含数据库方言。Model 以同一套 `buildMetricPlan` 构建去重、连接、聚合、时间桶和补零逻辑；各引擎只消费通用计划。完整结构、接口和语义以 [引擎插件接口规范](../../docs/spec/addp引擎插件接口规范.md#数据库无关分析计算契约) 为唯一事实源，Model 不重复定义计划节点或编译器接口。

`metric_plan_neutral.go` 从现有指标契约构建逻辑计划，`metric_plan.go` 校验请求参数，`metric_source_metadata.go` 读取 Meta 并绑定物理来源；`metric_implementation_service.go` 继续负责来源模型、定义修订、审批、并发与发布。Model 表／字段／关联 ID 映射为计划内 SourceID／ColumnID；物理来源放入独立 SourceBinding，以完整 EngineCatalogPath leaf 表达。禁止在指标编译器中假设一个 namespace 加表名，禁止接收 SQL 字符串或数据库原生函数参数。

草稿保存：校验 Standard 发布定义、已审批事实／维度及关联，生成确定的 Plan 与 Sources，调用当前引擎编译器 Check/Compile 验证可表达性，保存中立计划包及 owner 依赖。编译是结构与支持性验证，不执行业务数据查询，页面必须继续区分结构校验和真实数据验收。远程 Standard 与 System 请求在本地行锁之前完成，事务内重新核对本地版本、引用和来源引擎身份。

物理字段结构统一读取 Meta 已扫描的 `type_info.table`，通过完整 Engine Catalog 路径定位来源；只投影参与计算字段的名称／路径、类型／NativeType、长度／精度及可空性到 SourceBinding，不从逻辑类型猜测原生声明。来源未扫描或必要事实不完整时，明确要求先扫描／刷新。Meta 请求同样在本地行锁前完成，事务内核对对应模型来源与字段映射。Model 不新增实时引擎结构读取通道或 System 结构权限；执行 Provider 仍在计算事务内复核真实结构，Meta 快照不替代执行期漂移检查。

发布：重新构建并比较计划包和依赖，确认当前编译器身份、实例能力及定义发布状态；变化要求先保存草稿再发布。冻结内容包括业务契约、确定定义修订、分析计划包和 owner dependency_hash；原生查询不成为第二份持久指标定义。计划包中的 schema、语义配置、绑定与编译器实现版本均参与计算依赖。

`MetricCompiledPlan` 与 `common/client.ModelMetricPlan` 的公开计划响应使用 owner 外壳（实现身份、实现修订、定义身份／修订、dependency_hash）和唯一 `execution_plan`（AnalyticalPlanPackage），删除 `sql` 及其消费校验。`parameter_labels` 仅冻结参数枚举值对应的双语显示标签，值域以 Plan 为准并校验一一对应；外壳不重复保存 engine_id、参数类型、字段和稳定键事实，这些由计划包投影。现有 `POST /metric-implementations/:id/revisions/:revision_id/plan` 继续是唯一入口；输入验证、授权及发布状态边界保留，不新增 v2 路由或兼容响应字段。

取计划与执行：返回明确修订的冻结包，并根据当前模型与引擎验证依赖。描述变化不修改计算结果，但参数显示名、选项标签等消费契约仍按既有 owner 规则版本化。换物理字段、连接、来源类型、编译器实现或语义必须发布新实现修订、重绑服务并更新应用，不自动改写历史快照。客户端只能提交声明的指标参数，不能修改通用计划、引擎、结果类型或原生查询。

闭合 `AnalyticalDialect`、Model SQL 拼接与 sql_dialect 依赖已删除；旧发布包不兼容读取。部署前应盘点受影响实现／服务／应用并安排正式新修订切换，不在迁移 SQL 中伪造发布审批或把旧 SQL 自动改写为新计划。

首期仍限定同一引擎内的表格计算；指标业务语义、参数及结果保持前文所述。PG/MySQL 的物理表绑定入口和建表、退役能力另行跟踪。本轮计算契约既不授权 Model 跨模块管理数据，也不自动扩展物化 DDL；MySQL 测试夹具的 SourceBinding 不代表页面来源配置链路已经交付。

`/metric-implementations` 是唯一管理资源：GET 列表（可用 `fact_table_id` 筛选）、POST 创建；`/{id}` GET/DELETE；`/{id}/draft` PUT；`/{id}/revisions/{revision_id}/publish|withdraw` POST；`/{id}/revisions/{revision_id}/plan` POST（请求体可携带类型化 `input`） 读取确定发布修订。写入已有资源携带实现 `version`，不保留逻辑表嵌套写路由。使用 `model.metric_implementation.read/create/update/delete/publish/offline` 精确权限。

草稿保存验证字段、类型、单键维度关联和同引擎来源，生成确定性计划及依赖快照；发布再次校验依赖与 Standard 修订。结构校验不等于真实数据验证，页面必须区分两者。查询前重新校验发布状态及依赖 hash；物理字段、连接或被冻结标准语义变化必须拒绝旧计划。名称等展示属性不参与依赖签名。服务必须绑定确定修订，不自动选择最新版。

删除 LogicalTable 若级联移除其他事实表拥有的 TableRelation，必须在同一事务中锁定这些事实表，并将每个幸存事实表的 `version` 推进一次；同一事实表存在多条被级联删除的关系时也只推进一次。

版本校验、生命周期校验、引用锁定、业务写入和版本递增必须在同一数据库事务中完成。Service 先读取 `draft` 状态、Repository 随后无条件写入的做法不成立，因为审批可能在两步之间完成。LogicalTable 审批必须在事务内锁定聚合根并校验字段完整性；Entity 审批同理。版本或状态冲突不得留下属性、字段、关系、指标映射或回收队列副作用。

跨 Standard 的 HTTP 引用校验和 System 引擎运行描述读取在进入本地数据库事务前完成，不能持有 Model 行锁等待网络请求。指标计划在事务内重新核对来源引擎身份，并将完整来源绑定和编译器身份冻结入依赖。本地资源版本、生命周期、父子归属和外键引用仍必须在事务内重新锁定并校验。

已有资源的更新、删除、审批和退回草稿必须在 JSON body 中携带自己的 `version`；聚合子资源写入必须在 JSON body 中携带父资源 `version`。成功的子资源写入至少返回新的父版本，前端后续写请求必须顺序使用该值。`DELETE` 同样只使用 JSON body 传递版本，不接受 query、Header 或服务端当前值兜底。

直接资源更新、审批和退回草稿返回更新后的完整资源。聚合子资源新增、更新返回 `{ "resource": ..., "version": n }`，其中 `resource` 使用具体业务字段名 `attribute`、`field`、`relation` 或 `mapping`；聚合子资源删除返回 `{ "version": n }`。删除聚合根或独立关系成功后资源已不存在，只返回删除结果，不返回新版本。

并发版本不替代生命周期冲突。请求版本过期统一返回 `409 resource_version_conflict`；版本仍有效但资源状态不允许操作时，继续返回具体的 `entity_state_conflict`、`logical_table_state_conflict` 等领域错误。前端收到版本冲突后必须保留弹窗、表单、未保存内容和脏状态，由用户主动刷新后再建立新基线，不得自动换用最新版本重试。

### 请求与过滤参数约束

Model 的跨资源 ID 必须是正整数；可选引用只允许 `null` 或正整数，不允许使用 `0`、负数或无效字符串表达“未选择”。`sort_order` 必须大于等于 0，维度层级 `level_num` 必须从 1 开始，逻辑字段 `length` 非空时必须大于 0。名称、编码、列名、分层编码和关系名称必须在数据库字段定义的字符长度内；Mermaid 导入遵守同一限制，不能把超长输入推迟到数据库报错。上述规则由 HTTP 请求绑定、Service 领域校验和数据库约束共同保证，后台调用不得绕过 Service 校验。

Entity 和 LogicalTable 列表的 `status` 只允许 `draft`、`approved`；LogicalTable 的 `table_type` 只允许 `entity`、`fact`、`dimension`。`layer` 不是固定枚举，必须按当前 Tenant 的 DWLayer 事实校验；未知过滤值返回 `400 invalid_request`，不能静默返回空列表。单值过滤参数重复提交同样视为无效请求。

数据库唯一约束冲突必须由 Repository 翻译为统一冲突错误，再由 Service 映射为具体资源的稳定错误码；不能把 PostgreSQL 驱动错误透传或降级为 `500`。实体属性列名、逻辑字段列名和实体关系身份冲突分别使用 `entity_attribute_column_conflict`、`logical_field_column_conflict` 和 `entity_relation_conflict`，HTTP 状态统一为 `409`。

### API 异常矩阵

Model API 使用 HTTP 状态码和稳定 `error_code` 表达失败，前端不得解析本地化 `error` 文案做业务分支：

| HTTP 状态 | Model 语义 | 典型稳定错误码 |
| --- | --- | --- |
| `400` | 请求格式、ID、过滤条件、字段定义或审批前置条件无效 | `invalid_request`、`invalid_id`、`ddl_preview_invalid`、`entity_approval_attributes_required`、`entity_approval_primary_key_required`、`logical_table_approval_fields_required`、`logical_table_approval_primary_key_required` |
| `401` | 当前请求没有有效认证上下文 | `authentication_required` |
| `403` | 已认证，但缺少路由要求的 Permission | `permission_denied` |
| `404` | 当前 Tenant 中资源或引用不存在；跨 Tenant 资源同样隐藏为不存在 | `entity_not_found`、`logical_table_not_found`、`domain_not_found` |
| `409` | 资源版本、唯一约束、生命周期、标准引用删除屏障或聚合关系状态冲突 | `resource_version_conflict`、`entity_code_conflict`、`entity_state_conflict`、`entity_relation_conflict`、`standard_reference_deleting` |
| `503` | Standard 引用校验不可完成，包括服务不可达、上游 `5xx`、服务身份/权限错误、令牌获取或响应解码失败 | `standard_service_unavailable` |

Standard 引用校验只有明确的 `404` 或跨 Tenant 隐藏结果映射为引用 `404`；其他失败不能伪装成引用不存在，也不能降级为 Model 通用 `500`。所有 Permission 路由的 Swagger 必须声明 `401/403`；带请求参数的接口必须声明 `400`；带资源路径参数的接口必须声明 `404`。该契约由自动化测试校验。

## 五、Mermaid 与 DDL

Mermaid 交换文档的唯一格式是 Markdown：文件后缀为 `.md`，且只包含一个 `mermaid` fenced code block。代码块内使用 `addp.model.er/v2` 文档元数据注释声明 `all|domain` 范围；不再输出裸 `.mmd`，也不接受只改后缀、没有 Markdown 围栏的文件。`v1` 数字 ID 格式已删除，不保留兼容解析路线。

导出与 ER 图当前业务域上下文一致：正整数 `domain_id` 导出该业务域归属的 Entity、EntityAttribute，以及两端都在该域内的 EntityRelation；省略 `domain_id` 才表示显式全域导出。“展开跨域关联”只是查看上下文，不改变导出边界，避免将外域实体误表达为本域交换成员。

Mermaid 导入是非破坏性的显式成员批量创建，不是 Tenant 实体模型集合替换。导入文档中缺失的现有 Entity、EntityAttribute 或 EntityRelation 始终保留，不得推导为删除。以 Entity `code` 作为稳定匹配键：不存在时创建草稿实体及属性；已存在且全部可编辑业务字段相同时记为未变更；同编码定义不同时记为冲突，不自动更新或覆盖。EntityRelation 以两端实体编码、关系类型和名称作为稳定匹配键，同键描述不同同样是冲突。导入不保留另一条全量替换、自动 upsert 或删除重建路线。

前端必须先请求导入预览，展示将新建、未变更和冲突的实体与关系数量；存在冲突时不允许提交。`model.entity_model_revisions` 继续作为 Tenant 实体模型集合的非空 `BIGINT revision`，但只用于将预览结果绑定到确认提交时的集合基线。任何 Entity、EntityAttribute、EntityRelation 或 Cleanup 写入仍在事务中推进它；确认导入在同一事务中锁定并校验预览返回的 `revision`，过期返回 `409 resource_version_conflict`。预览本身不写业务资源；确认导入在单个事务中重新计划并创建全部新成员，任何冲突或写入错误整体回滚。

Mermaid 可逆子集必须通过 ADDP 元数据注释完整保存所有可编辑 Model 字段：Entity 的 code、显示名、`domain_code`、description；EntityAttribute 的 column_name、显示名、`element_code`、data_type、主键、可空性、description、sort_order；EntityRelation 的两端实体 code、关系类型、name 和 description。交换文档只使用当前 Tenant 内不可变且唯一的 Standard 稳定编码，不写入 Model 数据库中的 `domain_id` 或 `element_id` 代理键。

业务域文档的 `addp:document.domain_code` 必填，且所有 Entity 的 `domain_code` 必须与文档范围一致；全域文档的文档级 `domain_code` 必须为空，Entity 可分别声明不同业务域编码或不归属业务域。属性未绑定数据元时 `element_code` 为空。导入预览在进入 Model 本地事务前，通过 Standard 唯一 API 按当前 Tenant 精确解析全部非空 `domain_code` 和 `element_code`，不按显示名猜测、不使用当前页面业务域兜底，也不接受模糊匹配。任一编码不存在、不可引用或属于其他 Tenant 时，整次预览以对应 `domain_not_found` 或 `element_not_found` 失败；Standard 不可用时返回 `standard_service_unavailable`。预览响应必须返回已解析业务域的稳定编码和显示名，使用户能够确认落入哪个当前 Tenant 业务域。子集外语法必须明确拒绝，不能静默丢失。

Cleanup 是内部强制生命周期写入，不从外部请求接收 `version`。它仍必须锁定受影响资源，推进被修改资源的 `version`，并在涉及实体模型集合时推进 Tenant `revision`；physical cleanup 必须在单个事务中完成锁定、删除和修订推进。

PostgreSQL 建表语句预览只接受结构化物理目标配置。物理目标统一使用 `target_parent_locator + target_name`：父定位符必须是标准 ResourceLocator 且指向目标引擎中的父命名空间，目标名称是准备创建或校验的物理表名。当前可执行实现只支持 PostgreSQL/PostGIS 引擎的 `schema` 父节点，但用户界面统一称为“目标位置”，不得把 PostgreSQL 专属术语当作平台级概念。配置不再接受脱离 Engine Instance 身份的 `schema_name/table_name`，也不构造尚不存在资源的伪 `target_locator`。父定位符与目标名称必须同时为空或同时存在；为空时预览仅按逻辑表编码生成无父位置限定的设计语句。位置、表和字段标识符必须统一校验与引用。

物理目标配置只允许 `target_parent_locator` 与 `target_name`，不接受 `partition_by`、`partition_type` 或任意 SQL 扩展字段。当前建表能力不支持受控分区表，前后端不得展示或保存不可执行的分区设计；将来只有在引擎能力契约、建表执行和结构校验同时支持后，才能新增单一正式路径。

## 六、完成条件

- 所有写操作具有 Tenant 隔离和领域不变量测试。
- 所有版本主体和实体模型集合具有成功递增、旧版本无副作用、跨 Tenant 不可探测的并发测试。
- 聚合删除、Mermaid 导入和 physical cleanup 使用事务。
- Model Schema 使用版本化 migration，不在服务启动时执行 `AutoMigrate`。
- API 错误包含稳定 `error_code`，Swagger、前端和跨模块客户端保持同步。

### 生命周期操作与编排入口

- 用户界面统一将 `approved → draft` 称为“退回草稿”（Return to draft）；现有 `/reopen` 状态转换 API 保持唯一入口。执行前明确确认将解除审批及数据元修订冻结，不改变已发布物理表。
- 退回草稿不再受组成员限制；既有物理表及数据不因模型编辑而变化。
- 逻辑表详情直接创建物理表，不嵌入完整编排。

### 建模导航与物理目标操作入口

- 导航固定为业务实体、实体关系图、数仓分层、逻辑表设计、维度建模；默认进入业务实体。
- 维度关联唯一编辑入口是事实表详情的“维度关联”页签；维度建模只展示和导航，删除该页原有编辑对话框。维度表详情的“被引用关系”页签只读展示入向关系，编辑跳转回来源事实表。所有关系均显示两端表名、表编码、字段名、列名和类型。
- 逻辑表详情默认模型定义页签省略 `tab`；物理目标页签使用 `tab=physical-target`，集中展示目标引擎、目标位置、目标表名、配置状态、建表语句预览、创建/校验、执行记录和删除目标表操作；关联页签使用 `tab=relations`，`relation_id` 只在该页签表示定位关系。查看关联打开来源事实表及指定关系；查看维度表打开目标表定义，两者不得混用。刷新、前进后退恢复页签与关系定位，关系不存在时明确提示。
- 逻辑表详情使用唯一 `tab=concept-mappings` 维护概念实现映射；不在业务实体详情、ER 图或维度建模页提供第二个编辑入口。表、字段和维度关系映射一次完整提交并共享 LogicalTable 版本；已审批逻辑表只读展示，退回草稿后方可确认上游变化并重建映射基线。
- `GET /logical-tables/:id/dimension-relations` 对事实表返回出向关系，对维度表返回入向关系；仍只读 Model 本地事实。`PUT /logical-tables/:id/dimension-relations/:rid` 完整提交目标维度、两端字段、关系类型及事实表版本，保持关系 ID；与新增、删除共用父版本与事务校验。禁止通过删除后重建模拟编辑。
- 维度建模以事实表为中心展示维度关联、度量与指标实现，图标题统一为“模型关系图”。业务域复用 Standard 的 Domain 和 LogicalTable.domain_id；筛选仅约束左侧事实表，已选事实表的跨域维度关系及可关联维度不受该筛选裁剪。事实表详情展示归属业务域。
- 维度建模沿用唯一公开路由 `/modeling/star-schema`；`domain_id` 正整数表示指定业务域，省略表示全部业务域（包含未归属表），`table_id` 表示当前事实表。切换域时清除不属于新域的当前事实表；刷新及浏览器前进/后退恢复同一筛选和选择，不自动选择其他事实表。
- 维度建模前端变更复用 `make test-model-frontend` 和 `make test-console-frontend`，由现有前端 CI 自动发现执行，不新增测试入口。
- ER 图无业务域上下文时先选域；`domain_id=all` 显式进入全域总览，正整数表示指定域，省略表示未选择。`related=1` 仅在指定域时展开一跳跨域关系，两端实体必须存在；外域实体标注业务域。实体列表进入 ER 图保留当前域。Mermaid 导出复用当前业务域或显式全域选择；导入从 Markdown 文档元数据读取范围，先预览再增量创建，不覆盖或删除现有模型。
- 编排流程统一从 Orchestrator 进入；逻辑表详情物理目标页签的执行记录按钮只展示该表的建表执行。
- 已删除任务的历史执行事实仅保留审计，不提供再次执行入口。
- 当前不提供发布组或多表原子切换；仅在未来明确出现共同可见需求时重新设计。
- 验证复用 `make test-model-frontend`、`make test-model-postgres`、`make test-authorization`、`make test-platform` 和 System IAM PostgreSQL migration 门禁。现有 CI 自动命中新物化任务生命周期、路由权限和迁移测试；TaskProvider 新增 API 同步 Swagger 与权限声明，不新增测试入口。


指标编译计划的命名参数通过 `options` 声明有限允许值及完整 `zh-cn/en` 名称。内置名称取自 Model 国际化资源；允许值同时用于 Model 输入校验，不能在编译签名和校验中分别维护。Service 原样冻结，不把编译器元数据变成第二套指标定义。

#### 指标明细结果（2026-09-18）

`MetricContract.include_details` 显式开启去重计数的明细发布能力，只允许 `count_distinct`。Model 的同一构建主路径复用主体、维度质量断言、时间与固定过滤，分别冻结汇总计划和明细计划；`detail_execution_plan` 进入同一依赖快照及 hash。明细一行是 `subject_id + bucket + member`，同一对象在该期间出现多次时仅一行；首期仅返回主体、期间和去重对象，不拼接未声明的活动名称或日期字段；名称沿用主体维度。明细不补零，稳定键包含全部三个粒度字段。

既有 plan 请求增加可选 `result_kind=details`；省略表示汇总。返回仍是唯一 `execution_plan`，同时声明所选 `result_kind`。未开启明细、重叠率或非法种类明确拒绝。两种结果共同校验同一修订、依赖和冻结包，不能执行时动态猜测或改写汇总计划。

Service 的指标来源引用同样允许 `result_kind=details`，一个 Query Service 固定消费一个结果种类，复用既有创建／重绑、Consumer Descriptor、PreparedQuery、权限、保护及游标分页。Workbench 用独立明细 Table Component 和已有 Selection Binding，把点击行的主体与 bucket 映射到明细参数；日期与粒度复用同页查询条件，bucket 作为结果等值筛选，不把月初当作用户开始日期。这样月度明细保留原日期区间交集。名称和日期展示不参与去重。汇总和明细是两次查询，只有底层数据未变化且权限允许完整明细时才对账；不声称跨请求一致性快照。

验证：既有 Model PostgreSQL/MySQL T2 金样覆盖重复、零值、期间边界及汇总明细对账；Service T1/T2 覆盖种类冻结、依赖拒绝、明细权限及游标；Model/Service 前端标准门禁覆盖显式选择。Workbench 使用既有选择绑定及运行页在线验收，不新增查询代理或引擎分支。全部入口已由根 Makefile 和 CI 模块自动发现登记，无新依赖。
