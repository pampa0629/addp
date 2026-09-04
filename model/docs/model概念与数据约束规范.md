# ADDP Model 概念与数据约束规范

## 一、模块边界

Model 是 Tenant 级数据架构与建模事实的 owner，管理业务实体、实体关系、逻辑模型、数仓分层、公共/一致性维度、维度层级和指标实现。Standard 拥有业务域、数据元、码值集和指标定义等业务语义契约；Model 保存经过 Standard API 验证的长期引用，并在聚合审批或指标实现发布时冻结确定的标准修订，不代理或复制 Standard 资源。

维度层级是模型内部结构，只能引用 Model 的 LogicalTable / LogicalField，并可通过字段冻结的数据元修订获得统一语义。DimensionHierarchy、层级成员、API 和前端编辑入口已整体归入 Model；Standard 旧表、API、权限与前端路线以及 LogicalField 上的 `hierarchy_id + hierarchy_level` 已删除。

指标定义与指标实现必须分离：Standard MetricDefinitionRevision 只描述业务含义、统计口径、单位、非引擎可执行的语义表达，以及修订级指标语义依赖；依赖在草稿中引用指标定义稳定身份，发布时冻结为确定的已发布修订。Model MetricImplementation 同时保存 `metric_definition_id` 并冻结 `metric_definition_revision_id`，拥有粒度、事实来源、维度、连接、过滤和可执行表达式。同一指标定义可存在多个模型实现。FactMetricMapping 和 Standard Metric 的 `derivation_config` 是旧实现，迁移时直接删除，不保留并行路径。

Model 是逻辑表物化的结构控制面 owner。逻辑表的 `materialization` 保存目标父节点 ResourceLocator、目标名称和分区设计；物理 staging 创建、受控 DDL、结构校验、封存、原子发布和回收必须由 Model 根据已审批逻辑模型执行。任何通用 writer 只获得 prepare 稳定输出的 staging ResourceLocator，并通过自身的 Engine 写能力向已存在表写入；Model 不识别、调用或依赖具体 writer 业务模块。Orchestrator 使用 TaskProvider outputs 与显式参数绑定组织准备、计算、封存、质量门禁与发布顺序。

### 企业目录接入边界

Model Entity 与 LogicalTable 是企业目录的专业资源来源。所有已持久化资源，包括 `draft`，都通过 Model owner-local 可恢复变化源自动建立 CatalogEntry；Catalog 不调用 Model 列表 API 轮询全表，也不向 Model 回写 CatalogEntry ID。

Model 继续权威拥有完整 Entity、LogicalTable、属性、字段、物化配置、专业生命周期，以及 `domain_id`、`element_id + element_revision_id`、DimensionHierarchy、MetricImplementation 和建模关系。Catalog 只保存稳定来源引用与名称、编码、对象种类、专业状态等最小最后观察摘要；完整详情通过 Model 的 Catalog 专用批量解析接口动态读取。Catalog 投影不得用于恢复 Model，不得接受编辑，也不得被 Model 反向读取为专业事实。

Model 的 Domain、ElementRevision、MetricDefinitionRevision 和模型关系是专业内生关系，不复制为 Catalog 人工语义关联。Catalog 可以将其展示为 owner 声明关系和搜索分面；修改仍使用 Model 唯一写路径。Catalog 自身继续拥有目录业务名称、补充说明、责任、治理、收藏、集合，以及实际字段/组件到标准修订的落标映射。

Model 面向当前 User AuthContext 提供 `GET /entities/{id}/relations` 与 `GET /logical-tables/{id}/relations` 一跳专业关系图。Entity 路由同时要求 `model.entity.read` 与 `model.entity_relation.read`；LogicalTable 路由要求 `model.logical_model.read`。响应遵循 `addp.professional_relations/v1`，只读取 Model 权威表，不调用 Standard、Catalog 或其他 owner，不保存 CatalogEntry ID，也不把 owner 不可达提升为 Model Ready 条件。

Model 变化捕获固定使用 Entity 与 LogicalTable 聚合根上的 PostgreSQL trigger，在业务写事务中追加 `model.catalog_resource_changes`。聚合子资源写入已经必须推进根版本，因此不得再在各 Service 添加 append 回调或双写。Catalog 以 opaque cursor 拉取并可从历史起点重放；Model 或 Catalog 暂时不可达只造成同步滞后，不参与对方 Ready。

PostgreSQL DDL 预览仍只是设计辅助能力，不改变逻辑模型状态，也不产生物理资源。真实物化使用独立 `MaterializationBatch` 聚合，不把 `materialized` 加入 LogicalTable 生命周期，也不允许其他模块直接读取 Model 私有表、自行拼装 DDL或持有永久数据库权限。

### 逻辑表物化批次

一次完整重算遵循唯一顺序：

```text
Model prepare -> generic writer -> Model seal -> Quality materialization gate -> Model group publish
```

- `prepare` 只接受已审批且配置完整物化目标的 LogicalTable ID。Model 冻结逻辑表版本、物化目标和结构指纹，使用 `audience=model` 的 Execution Authorization 验证目标边界，并创建本批唯一可写 staging。稳定输出为 `batch_id + staging_locator`，不返回 DDL 或凭据。
- 目标表不存在时允许首次发布；目标表已存在时，Model 必须先解析完整管理标记并确认其中的 LogicalTable ID 与当前逻辑表一致。标记中的旧结构指纹可以不同，这表示同一 LogicalTable 的受控结构升级，不表示目标所有权失效；未受 Model 管理、标记损坏、属于其他 LogicalTable 或包含尚未支持分区设计的目标仍必须拒绝自动替换。
- prepare 必须把事务中实际观察到的旧目标完整管理标记持久化为批次前置状态；目标不存在时持久化明确的“不存在”状态。该前置状态与本批新结构指纹含义分离，不能用当前结构指纹反推旧目标状态。
- 通用 writer 任务定义不保存 LogicalTable ID、Model 上下文或 staging。Orchestrator 将 prepare 的 `staging_locator` 绑定到 writer 必填 `target_locator`；writer 使用从父 execution 派生的精确 Engine `read/write` 授权执行，稳定输出 `execution_id + target_locator + row_count`。
- `seal` 接受 prepare 的 `batch_id`、writer 输出的 `execution_id` 和 `target_locator`。Model 必须校验 writer execution 为 `success`，与 prepare/seal 同 Tenant、同父 Orchestrator execution、同 Actor Principal/Tenant Membership/授权版本，且 locator 精确匹配批次 staging；随后校验字段顺序、物理类型、结构指纹和 Model 管理标记，成功后置为 `sealed`。Model 不校验 writer module 名称。
- Quality 如需读取 staging，必须以同一父 Orchestrator execution 和当前 reader execution 向 Model 申请 Materialization Read Context；只能返回 sealed 批次的 staging locator、字段、批次和结构指纹。reader 仍使用自身从父 execution 派生的精确只读授权，不得读取 Model 私有表或借用 Model 的引擎授权。
- `publish` 在同一目标数据库事务内先以 prepare 持久化的前置状态执行 compare-and-swap 校验，再完成旧目标暂存、staging 改名、旧目标删除和管理标记保留。prepare 时目标不存在则 publish 时仍必须不存在；prepare 时目标存在则当前完整标记必须与前置标记逐字一致。任何并发发布、人工改标记或替换都会令整组发布失败。事务失败必须保持原目标可用；重复执行按本批管理标记幂等收敛。
- 同一 Tenant 的同一物理目标同时最多一个 `preparing|prepared|sealed|publishing` 批次。并发重算返回冲突，不建立多批次竞争或“最后完成者覆盖”语义。新 prepare 只能接管父 Orchestrator execution 已为 `failed|timeout|cancelled`、且该父 execution 已无 `pending|running` 子 execution 的旧 `preparing|prepared|sealed` 批次；旧父仍运行、已成功或旧批次处于 `publishing` 时必须继续返回冲突。接管事务将旧批次标记为 `aborted`，新 prepare worker 使用本次精确 DDL 授权，在同一目标数据库事务中先回收该目标历史 `aborted|failed` staging，再创建新 staging。
- 历史 staging 只有在表注释精确匹配该批次的 Model ownership marker 时才能删除；表已不存在按幂等成功处理，marker 不匹配则整个 prepare 失败且不得删除任何历史 staging。回收与新 staging 创建共用一个物理数据库事务；任一步失败必须整体回滚。不新增公开 abort TaskProvider、Orchestrator 专属补偿节点或 writer 回调。
- 批次状态固定为 `preparing|prepared|sealed|publishing|published|failed|aborted`。prepare 失败进入 `failed`；seal 失败不提升批次；publish 失败恢复为 `sealed`，允许在同一 sealed 批次上重新发布；只有物理发布成功后才进入 `published`。

### 物化读上下文

- 唯一接口为 `POST /api/v1/model/materialization-read-contexts`，当前只允许 `addp-quality` 通过 Common Model Client 调用。Develop 的通用关系输入直接消费 Orchestrator 绑定的 ResourceLocator，不调用本接口。
- 请求必须提供 `parent_execution_id + reader_execution_id + reader_attempt + reader_lease_token + logical_table_ids`。Model 必须校验 reader 的 Tenant、模块、Actor 血缘、父 execution、running lease 和调用 Service Principal 一致；不接受浏览器或 Orchestrator 直接调用。
- 每个逻辑表必须在同一父 execution 下存在 sealed 批次。任一缺失、未封存、失效或结构漂移都拒绝整份上下文，不返回部分结果。
- 响应使用 `model.materialization-read-context/v1`，每项只包含 `logical_table_id + batch_id + engine_id + staging_locator + columns + schema_fingerprint`。这是执行期内部事实，不进入任务定义或 Orchestrator Step 参数。
- 本能力是 Quality 物化门禁的受限读语义，不是 writer 目标解析 API；writer 目标只来自 prepare 的 TaskProvider output。

### 物化组与原子发布

- `MaterializationGroup` 是 Model-owned 可变主资源，使用 `tenant_id + code` 作为稳定语义身份，使用正整数 `version` 作为并发版本，持久化非空且不重复的 LogicalTable ID 集合。
- 业务 API 唯一路由为 `GET|POST /api/v1/model/materialization-groups`和 `GET|PUT|DELETE /api/v1/model/materialization-groups/{id}`。`PUT` 必须提交完整可编辑状态和当前 `version`，`code` 创建后不可修改；删除也必须提交 `version`。
- 组内逻辑表必须已审批、配置完整物化目标，且位于同一 PostgreSQL Engine。不允许跨 Engine 发布、空组、重复成员或同一物理目标重复绑定。
- TaskProvider 任务类型为 `materialization_group_publish`，task ID 为 MaterializationGroup ID，仅允许 Orchestrator 以非空父 execution 触发。任务输入必须提交 `expected_group_id + expected_group_version`，不接收动态成员、表名或 locator。
- 发布必须按组定义的全部成员，解析同一父 execution 下的 sealed 批次，并在一个目标 PostgreSQL 事务内交换全部目标。任一成员不就绪、结构漂移或物理标记不匹配时整组拒绝，不部分发布。
- 目标库事务提交后再收敛 Model 控制库状态；如响应丢失或进程崩溃，重试必须通过全部管理标记识别已提交结果并幂等收敛，不再次交换。

### 物化目标退役

- 物化目标退役是 Model owner 的高风险同步命令，不注册 TaskProvider、不进入 Orchestrator，也不允许 Develop、Transfer 或通用 SQL 任务代为执行。
- 唯一路由为 `DELETE /api/v1/model/logical-tables/{id}/materialized-target`。请求必须提交当前 LogicalTable 正整数 `version`，并逐字提交当前 `target_parent_locator + target_name` 作为人机确认快照；不接受 SQL、另一个目标或服务端版本兜底。
- Model 必须在控制库事务中锁定 LogicalTable，校验 Tenant、版本、当前物化配置、该表已不属于任何 MaterializationGroup，且不存在 `preparing|prepared|sealed|publishing` 批次；随后使用当前用户授权访问配置中的精确 Engine 和目标。
- 删除前必须读取物理表管理标记。目标不存在按幂等成功处理；目标存在时标记必须合法且 LogicalTable ID 与当前逻辑表一致，结构指纹和历史 batch ID 只作为该物理产物版本事实，不阻止退役。未标记、标记损坏或属于其他 LogicalTable 的物理表一律拒绝。
- 物理操作只允许对配置解析出的精确限定表执行 `DROP TABLE`，不接受级联删除。物理删除成功后不修改 LogicalTable 配置或并发版本；后续如需删除逻辑模型，必须按既有流程先重新打开为 `draft`，再通过 LogicalTable 删除接口提交当时版本。
- Model 不调用 Catalog、Service、Develop、Quality 或 Orchestrator 检查引用。治理流程必须先由各 owner 删除对该逻辑表或物理目标的配置引用，再移出 MaterializationGroup、退役物理目标，最后删除逻辑模型；模块边界不能由 Model 的 DDL 操作穿透。

### 逻辑表删除闭环

- LogicalTable 删除是 Model 聚合删除，不负责物理 DDL。删除前必须在同一控制库事务中锁定 LogicalTable，并确认资源版本匹配、状态为 `draft`、不属于任何 MaterializationGroup、`materialization` 已通过 LogicalTable 完整更新显式清空，且不存在 `preparing|prepared|sealed|publishing` 非终态 MaterializationBatch。物理目标退役不会代替清空配置，也不会隐式推进 LogicalTable 版本。
- `published|failed|aborted` MaterializationBatch 是依附于 LogicalTable 的终态物化操作状态，不是跨逻辑定义独立保留的审计资源。满足全部删除前置条件后，Model 必须在删除 LogicalTable 的同一事务中先删除该 Tenant、该 LogicalTable 的全部终态批次，再删除逻辑表聚合；任一步失败必须整体回滚。
- `common.task_executions` 是跨模块通用执行审计历史，不属于 LogicalTable 聚合。删除终态 MaterializationBatch 不得删除、级联删除或改写对应 TaskExecution；历史执行中的 `source_task_id`、执行血缘与结果继续保留，并允许其引用已经删除的业务任务定义。
- `materialization_batches.logical_table_id` 继续使用 `ON DELETE RESTRICT`，作为绕过 Service 事务时的数据库保护；不得改为外键级联，也不得新增 LogicalTable “退役”状态或第二条强制删除路径。
- 删除前置条件冲突统一返回 HTTP `409`，物化组成员、配置未清空和非终态批次分别使用稳定错误码 `materialization_group_member_conflict`、`logical_table_materialization_configured`、`logical_table_materialization_batch_active`；不得把数据库外键错误作为正常业务响应。

### TaskProvider 与封存交接

已审批 LogicalTable 是来源驱动、不可变的物化任务定义，同一 LogicalTable ID 分别作为 `materialization_prepare`、`materialization_seal` 与 `materialization_publish` 的 TaskProvider task ID：

- prepare 执行输入契约为空，稳定输出 `batch_id + staging_locator`。
- seal 的 `batch_id + writer_execution_id + target_locator` 是必填运行时输入，`input_defaults` 不伪造空值。seal 稳定输出 `batch_id + staging_locator + schema_fingerprint`。
- publish 不接受 locator、Schema、表名或 DDL，按 `tenant_id + logical_table_id + parent_execution_id` 解析唯一 sealed 批次。
- prepare、writer、seal、Quality 和 publish 子 execution 必须共享同一 `parent_execution_id`。手动全量重算同样由用户启动 Orchestrator 编排，不建立绕过编排的 Model 直执行路径。
- `staging_locator` 可以在父编排内作为短生命周期 ResourceLocator 输出绑定，但不进入任何 writer 任务定义、Model 之外的业务实体或长期配置。
- 动态目标 writer 不进行跨 lease 单步重试。writer 失败后整个父编排失败，下一次重算从新 prepare 开始；Model 回收旧批次，不建立 write-attempt 实体、完成回调、模块白名单或接管协议。

## 二、授权边界

Model 资源当前全部属于 Tenant，不存在 Department 或 Project Group Resource Scope Binding。所有 `model.*` Permission 只允许 Tenant Scope。`tenant.data_architect` 是面向 User Principal 的完整 Model 管理角色；`tenant.graph_runtime` 只保留 Graph 导入所需的 Entity 和 EntityRelation 只读权限。

物化读取精确 Permission 为 `model.materialization_read.execute`，当前仅用于 Quality 读取 sealed 批次上下文；`model.materialization_group.read|create|update|delete` 用于 MaterializationGroup 业务资源；`model.materialized_target.delete` 只用于用户显式退役 LogicalTable 当前登记的物理目标。不存在面向 Transfer/Develop 的 Model 写入 Permission。Model prepare/seal/publish 通过本模块 TaskProvider 执行权限触发，writer 只持有目标 Engine 的精确 write effect。机器身份授权与用户资源管理权限不得互相替代。

`model.catalog.read` 是不可由租户自定义的 Tenant Scope 机器权限，只授予 `tenant.catalog_runtime`，并由 Model 的变化流和 Catalog 批量解析路由同时校验固定 `addp-catalog` OAuth Client。该权限不授予用户读取 Model 管理 API，也不允许 Catalog 写入 Model。

Model 在写入前校验 Standard 引用时，不转发或保存 User Access Token。`addp-model` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.model_runtime`，目标上只读取 Standard 的 Domain、Element/ElementRevision 与 MetricDefinition/MetricDefinitionRevision，不再拥有 `standard.dimension_hierarchy.read`。Standard 协调被 Model 引用资源的删除时，`addp-standard` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.standard_runtime`，该角色只包含 `model.standard_reference.update`。平台控制面的 Runtime Role 不参与 Tenant 业务引用校验或删除协调。

Permission Guard 只判断候选能力，Repository 和 Service 仍必须对每个资源及其子资源执行 Tenant 隔离。任何父子写入、删除和关系创建都必须验证完整归属，不能只依赖请求中的全局 ID。

## 三、聚合与引用

- Entity 聚合包含 EntityAttribute；EntityRelation 是连接两个 Entity 聚合的独立关系事实。
- LogicalTable 聚合包含 LogicalField、TableRelation、DimensionHierarchy 和 MetricImplementation；维度层级成员只能引用同一 LogicalTable 的字段，MetricImplementation 只允许归属事实表。
- LogicalTable 的可选 Entity 引用只表达概念模型来源，不自动同步属性与字段。需要重新生成时必须由显式操作整体替换，不能隐式双向同步。
- DWLayer 是 Tenant 可配置事实。LogicalTable 必须引用已存在的 DWLayer，前端不得维护固定分层枚举作为第二事实源。
- Model 内部引用由数据库外键、唯一约束和 CHECK 约束保证；跨 Standard Schema 的引用先由 Standard HTTP API 验证，再在 Model 写事务中锁定对应的标准引用删除屏障。后台调用、Mermaid 导入和普通 API 写入必须使用同一屏障路径。

### 数据元修订冻结

EntityAttribute 与 LogicalField 在草稿阶段只维护长期引用 `element_id`，`element_revision_id` 必须为空。Entity 或 LogicalTable 审批时，Model 使用同一个审批时点批量解析全部 `element_id` 对应的 Standard 当前生效修订，并在本地审批事务中把结果冻结到各属性或字段的 `element_revision_id`；任一数据元在该时点没有生效修订时，审批整体失败且不产生状态、版本或冻结字段副作用。

审批后的 DDL、物化、质量规则和历史展示必须以被冻结的 `element_revision_id` 为语义事实，不得动态跟随 Standard 后续生效修订。重新打开聚合时，Model 在同一事务中把聚合转回 `draft` 并清空所属属性或字段的 `element_revision_id`；再次审批重新按新的统一审批时点解析。`element_revision_id` 是审批快照，不接受前端写入，也不建立绕过聚合审批的单独更新接口。

引入冻结字段时，历史已审批聚合如果含有 `element_id`，不能仅凭当前 Standard 状态反推当初审批时使用的精确修订。迁移必须将这类聚合转回 `draft` 并推进版本，由用户在确认后显式重新审批；禁止用迁移时的当前修订伪造历史快照。

### Standard 引用删除屏障

Standard 的业务域、数据元和指标定义被 Model 引用时，不使用跨 Schema 外键，也不允许 Standard 直接读取 Model 私有表。Standard 硬删除这些稳定身份必须通过 Model 的标准引用删除屏障完成影响评估；已发布且被模型冻结引用的修订不得硬删除。一次性“查询无引用后直接删除”存在检查与删除之间的竞态，禁止作为正式路径。DimensionHierarchy 迁入 Model 后是本地聚合，不再参与跨 Standard 删除协调。

Model 为 `(tenant_id, resource_type, resource_id)` 维护单行屏障，状态只允许 `open`、`frozen`、`deleted`。任何可能写入 `domain_id`、`element_id` 或 `metric_definition_id` 的事务，必须按稳定顺序创建并锁定对应屏障行，只有 `open` 才允许继续；屏障状态检查、Model 业务写入、资源版本和 Tenant 实体模型集合 `revision` 推进必须处于同一事务。Standard HTTP 校验仍在本地事务前完成，不能持有 Model 行锁等待网络。

Standard 删除遵循唯一顺序：Standard 先持久化删除协调记录并将资源置为 `deleting`，再由同一删除协调流程串行锁定 Standard 资源行，调用 Model 原子冻结屏障并权威扫描当前引用。协调流程必须在释放 Standard 资源行锁前完成冻结、引用分支和本地硬删除；这样用户重试、后台补偿和并发删除不会同时执行本地删除。有引用时必须先让 Model 恢复 `open`，再提交 Standard `active`；任一恢复步骤失败都保留 `deleting` 协调记录，供后台补偿继续处理。无引用时 Standard 硬删除资源并保留协调记录，直到 Model 屏障终止为 `deleted` 成功；因此即使资源已硬删除而终态通知响应丢失，后台补偿仍能完成终态收敛。冻结事务与所有新增引用事务锁定同一屏障行：冻结前完成的写入必然进入权威扫描，冻结后到达的写入必然失败，因此不存在在途请求越过扫描的窗口。

`frozen` 和 `deleted` 都禁止新增或保留目标引用；`deleted` 是不可逆终态，防止删除完成前已通过 Standard 校验、但较晚进入 Model 事务的请求在资源删除后落库。协调调用失败时不得绕过屏障继续删除。资源已进入 `deleting` 时重复删除必须复用同一条协调记录并从冻结和权威扫描继续，不能创建第二条强制删除路径。Model 冻结前或本地删除失败时，只能在 Model 成功 `open` 后再恢复 Standard `active`；如果 `open` 失败，资源保持 `deleting`，后台补偿必须重试。已经完成硬删除但终态通知失败时协调记录和 Model 屏障都保持待收敛状态，后续只允许补做 `deleted` 终态；协调记录不得因本地资源已不存在而丢失。

## 四、生命周期

Entity 和 LogicalTable 当前生命周期统一为 `draft` 与 `approved`。只有 `draft` 可修改；审批前必须完成聚合校验。Entity 必须至少包含一个定义完整的属性且至少一个属性为主键；LogicalTable 必须至少包含一个字段且至少一个字段为主键。审批失败必须通过稳定错误码和本地化消息指出缺少属性、字段或主键等具体前置条件，不能统一降级为通用请求校验失败。已审批资源如需修改，必须通过显式重新打开操作回到 `draft`，不能在 `approved` 状态直接修改子资源。

`materialized` 不属于当前正式状态。租户资源回收的 logical 模式可以将已审批资源重新打开为 `draft`，physical 模式必须在单个数据库事务中按聚合顺序删除。

### 完整更新语义

Model 资源统一使用 `PUT` 表达完整更新，不提供并行的部分更新路径。请求必须携带资源的完整可编辑状态；`domain_id`、`entity_id`、`element_id`、`length` 等可空字段使用 JSON `null` 表示解除引用或清空值。缺失的可空字段与 `null` 含义一致，不能被解释为保留旧值；必填字段缺失或为空必须返回 `400 invalid_request`。维度层级及成员通过 Model 聚合写接口维护，不在 LogicalField 上保留 `hierarchy_id` 或 `hierarchy_level` 双重事实。

前端保存详情和子资源时必须提交完整表单状态。对于当前页面不直接编辑但属于资源状态的字段，例如逻辑表的来源实体 `entity_id` 和属性、字段的 `sort_order`，前端也必须从已加载资源中原样带回，不能依赖后端保留旧值。

### 并发版本与聚合写入

Model 遵循平台 API 规范中的资源并发版本规则。`Entity`、`LogicalTable`、`DWLayer` 和 `EntityRelation` 是独立版本主体，数据库均保存非空 `BIGINT version`，创建时从 `1` 开始。`EntityAttribute` 共用所属 `Entity.version`；`LogicalField`、`TableRelation`、`DimensionHierarchy`、层级成员和 `MetricImplementation` 共用所属 LogicalTable 的 `version`，这些聚合子资源不得再建立自己的并发版本。

| 写入对象 | 并发版本主体 | 事务边界 |
| --- | --- | --- |
| Entity 基本信息、审批、重新打开、删除 | Entity | 按 `tenant_id + id + version` 条件写入并推进 Entity 版本 |
| EntityAttribute 新增、更新、删除 | 所属 Entity | 校验 Entity 为 `draft`、写入属性并推进 Entity 版本 |
| LogicalTable 基本信息、审批、重新打开、删除 | LogicalTable | 按 `tenant_id + id + version` 条件写入并推进 LogicalTable 版本 |
| LogicalField 新增、更新、删除 | 所属 LogicalTable | 校验 LogicalTable 为 `draft`、写入字段并推进 LogicalTable 版本 |
| TableRelation 新增、删除 | 事实侧 LogicalTable | 锁定并校验事实表和维度表均为 `draft`，写入关系并只推进事实表版本 |
| DimensionHierarchy 及层级成员新增、更新、删除 | 所属维度 LogicalTable | 校验维度表为 `draft`、层级字段均属于该表、`level_num` 从 1 开始且不重复，写入并推进 LogicalTable 版本 |
| MetricImplementation 新增、更新、删除 | 所属事实 LogicalTable | 校验事实表为 `draft`、指标定义修订已发布且实现契约完整，写入并推进 LogicalTable 版本 |

MetricImplementation 的 `source_config` 与 `expression_config` 必须是非空 JSON 对象，`dimension_config` 与 `filter_config` 必须是 JSON 对象。当前唯一契约要求 `source_config.field_ids` 是至少包含一个当前事实表字段 ID 的数组，`expression_config.engine` 和 `expression_config.expression` 是非空字符串；扩展配置必须在本规范先定义后实现，不能由前端任意创造第二套结构。`metric_definition_id` 与 `metric_definition_revision_id` 必须经 Standard API 验证属于同一指标定义，且修订状态为 `published`。实现状态固定为 `active|disabled`，它只控制该实现是否可用于后续计算选择，不改变所冻结定义修订的有效性。
| EntityRelation 更新、删除 | EntityRelation | `PUT` 携带完整端点和关系定义；校验关系版本，同时锁定并校验变更前后涉及的全部 Entity 均为 `draft` |
| EntityRelation 创建 | 创建时无关系版本 | 同一事务锁定并校验两端 Entity 均为 `draft`，新关系版本从 `1` 开始 |
| DWLayer 更新、删除 | DWLayer | 更新按版本条件写入；删除在同一事务完成版本校验、LogicalTable 引用检查和删除 |

删除 LogicalTable 若级联移除其他事实表拥有的 TableRelation，必须在同一事务中锁定这些事实表，并将每个幸存事实表的 `version` 推进一次；同一事实表存在多条被级联删除的关系时也只推进一次。

版本校验、生命周期校验、引用锁定、业务写入和版本递增必须在同一数据库事务中完成。Service 先读取 `draft` 状态、Repository 随后无条件写入的做法不成立，因为审批可能在两步之间完成。LogicalTable 审批必须在事务内锁定聚合根并校验字段完整性；Entity 审批同理。版本或状态冲突不得留下属性、字段、关系、指标映射或回收队列副作用。

跨 Standard 的 HTTP 引用校验在进入本地数据库事务前完成，不能持有 Model 行锁等待网络请求。本地资源版本、生命周期、父子归属和外键引用仍必须在事务内重新锁定并校验。

已有资源的更新、删除、审批和重新打开必须在 JSON body 中携带自己的 `version`；聚合子资源写入必须在 JSON body 中携带父资源 `version`。成功的子资源写入至少返回新的父版本，前端后续写请求必须顺序使用该值。`DELETE` 同样只使用 JSON body 传递版本，不接受 query、Header 或服务端当前值兜底。

直接资源更新、审批和重新打开返回更新后的完整资源。聚合子资源新增、更新返回 `{ "resource": ..., "version": n }`，其中 `resource` 使用具体业务字段名 `attribute`、`field`、`relation` 或 `mapping`；聚合子资源删除返回 `{ "version": n }`。删除聚合根或独立关系成功后资源已不存在，只返回删除结果，不返回新版本。

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

Mermaid 导入采用 Tenant 级“实体模型集合”全量替换语义：该集合包含当前 Tenant 的全部 Entity、EntityAttribute 和 EntityRelation。由于它跨越多个独立聚合，不能使用任一 Entity 或 EntityRelation 的 `version` 作为并发边界；Model 必须为每个 Tenant 维护非空 `BIGINT revision`，初始值为 `1`，并由 `model.entity_model_revisions` 的租户单行事实承载。

`revision` 是整个实体模型集合的编辑基线，不只用于串行化两次 Mermaid 导入。任何改变 Entity、EntityAttribute 或 EntityRelation 的创建、更新、删除、审批、重新打开、Mermaid 导入和内部 Cleanup 都必须在同一事务中锁定 Tenant 修订行并推进 `revision`。创建独立资源不要求客户端携带 `revision`，但服务端仍必须推进它；这样导出后的任意普通写入都会使旧 Mermaid 导入产生版本冲突。

导出响应必须同时返回 `mermaid_code` 和当前 `revision`；导入请求必须同时携带 `mermaid_code` 和作为编辑基线的 `revision`。导入先完整解析和校验可逆子集，再在单个事务中锁定 Tenant 修订行、校验修订版本、替换集合并执行 `revision = revision + 1`。修订行必须在 Tenant 首次导出或导入前稳定存在，不能依赖锁定现有 Entity 行，因为空集合没有可锁定成员。修订冲突同样返回 `409 resource_version_conflict`，且集合和修订版本均保持不变。

导入是破坏性聚合写入，要求 Entity 与 EntityRelation 的创建、删除权限；租户存在已审批实体时必须先全部重新打开。任何解析、校验、修订冲突或写入错误都整体回滚，不返回部分成功。导入成功后返回新 `revision`，前端立即替换本地修订基线。

Mermaid 可逆子集必须通过 ADDP 元数据注释完整保存所有可编辑 Model 字段：Entity 的 code、显示名、domain_id、description；EntityAttribute 的 column_name、显示名、element_id、data_type、主键、可空性、description、sort_order；EntityRelation 的两端实体 code、关系类型、name 和 description。导出后不做修改立即导入必须保持上述业务字段不变；数据库身份、资源版本、创建人和时间戳由导入生成新值。子集外语法必须明确拒绝，不能静默丢失。

Cleanup 是内部强制生命周期写入，不从外部请求接收 `version`。它仍必须锁定受影响资源，推进被修改资源的 `version`，并在涉及实体模型集合时推进 Tenant `revision`；physical cleanup 必须在单个事务中完成锁定、删除和修订推进。

PostgreSQL DDL 预览只接受结构化物化配置。物化目标统一使用 `target_parent_locator + target_name`：父定位符必须是标准 ResourceLocator 且指向 `schema` 节点，目标名称是尚未创建或准备替换的物理表名。配置不再接受脱离 Engine Instance 身份的 `schema_name/table_name`，也不构造尚不存在资源的伪 `target_locator`。父定位符与目标名称必须同时为空或同时存在；为空时 DDL 仅按逻辑表编码生成无 Schema 限定的设计预览。Schema、表、字段和分区标识符必须统一校验与引用；分区类型使用固定枚举；不接受任意 SQL 扩展字段。

未分区是物化配置的唯一默认形态，持久化时必须同时省略 `partition_by` 与 `partition_type`，不得使用空字符串或单独的 `partition_type` 表达“未分区”。只有非空 `partition_by` 才表示分区设计，此时 `partition_type` 必须规范化为 `range|list|hash`。当前 DDL 预览可展示该设计，但物化 prepare 与 TaskProvider 不得发布或执行非空分区配置；在 Model 完成受控分区物化前，必须以稳定领域错误明确拒绝。

## 六、完成条件

- 所有写操作具有 Tenant 隔离和领域不变量测试。
- 所有版本主体和实体模型集合具有成功递增、旧版本无副作用、跨 Tenant 不可探测的并发测试。
- 聚合删除、Mermaid 导入和 physical cleanup 使用事务。
- Model Schema 使用版本化 migration，不在服务启动时执行 `AutoMigrate`。
- API 错误包含稳定 `error_code`，Swagger、前端和跨模块客户端保持同步。
