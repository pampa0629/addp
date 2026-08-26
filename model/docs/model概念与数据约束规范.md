# ADDP Model 概念与数据约束规范

## 一、模块边界

Model 是 Tenant 级数据架构与建模事实的 owner，管理业务实体、实体关系、逻辑模型、数仓分层以及逻辑模型到 Standard 指标的引用。Standard 继续拥有业务域、数据元、维度层级和指标；Model 只保存经过 Standard API 验证的引用，不代理或复制 Standard 资源。

Model 是逻辑表物化的结构控制面 owner。逻辑表的 `materialization` 保存目标父节点 ResourceLocator、目标名称和分区设计；物理表准备、受控 DDL、结构校验、原子发布和 staging 回收必须由 Model 根据已审批逻辑模型执行，不得交给 Develop 的普通 SQL 任务。Develop 只负责计算数据并写入 Model 为单次物化批次签发的受限 staging 目标；Orchestrator 只编排准备、计算、质量门禁与发布顺序。

PostgreSQL DDL 预览仍只是设计辅助能力，不改变逻辑模型状态，也不产生物理资源。真实物化使用独立 `MaterializationBatch` 聚合，不把 `materialized` 加入 LogicalTable 生命周期，也不允许其他模块直接读取 Model 私有表、自行拼装 DDL或持有永久数据库权限。

### 逻辑表物化批次

一次完整重算遵循唯一顺序：

```text
Model prepare -> Develop query compute -> Quality check -> Model publish
```

- `prepare` 只接受已审批且配置完整物化目标的 LogicalTable ID。Model 冻结逻辑表版本、物化目标和结构指纹，生成不可由调用方指定的 staging 名称，并使用 `audience=model` 的 Execution Authorization 执行受控 PostgreSQL/PostGIS DDL。
- 目标表不存在时允许首次发布；目标表已存在时，只有 Model 管理标记中的结构指纹与当前已审批逻辑表一致才允许重算。结构不一致、未受 Model 管理或包含尚未支持的分区设计时拒绝自动替换，不能把破坏性 Schema 迁移隐藏在重算中。
- Develop 任务不得接收或拼装 Schema、表名、DDL。它保存目标 `logical_table_id`，执行时通过 Model Client 按自身 execution 解析执行域物化上下文，再使用自身用户派生 Execution Authorization 向该批次 staging 写数据。
- Quality 如需检查 staging，使用同一执行域解析边界和自身只读 Execution Authorization；不得读取 Model 私有表或借用 Model 的引擎授权。
- `publish` 在同一目标数据库事务内完成旧目标暂存、staging 改名、旧目标删除和管理标记保留。事务失败必须保持原目标可用；重复执行按批次管理标记幂等收敛。
- 同一 Tenant 的同一 LogicalTable 同时最多一个 `preparing|prepared|publishing` 批次。并发重算返回冲突，不建立多 staging 竞争或“最后完成者覆盖”语义。
- 批次状态固定为 `preparing|prepared|publishing|published|failed|aborted`。prepare 失败进入 `failed`；publish 失败恢复为 `prepared`，允许在同一批次上重新发布；只有物理发布成功后才进入 `published`。

### TaskProvider 与执行域解析

已审批 LogicalTable 是来源驱动、不可变的物化任务定义，同一 LogicalTable ID 分别作为 `materialization_prepare` 与 `materialization_publish` 的 TaskProvider task ID。两个任务的执行输入契约均为空，不把动态 `batch_id` 设为必填运行参数：

- Orchestrator 调用时，prepare、Develop、Quality 和 publish 子 execution 共享同一个 `parent_execution_id`。Model 按 `tenant_id + logical_table_id + parent_execution_id` 解析唯一 prepared 批次。
- 手动全量重算同样由用户手动启动 Orchestrator 编排，不建立绕过编排的 Model 直执行路径；因此物化 TaskProvider execution 必须携带父编排 execution。
- `batch_id` 仍是 Model 内部聚合身份、审计事实和 prepare 的稳定 execution 输出，但不是调用方选择物理目标的入口。
- 执行域写入解析固定使用 `POST /api/v1/model/materialization-write-contexts/resolve`，请求只包含 `parent_execution_id + logical_table_id`。Model 必须按当前 Tenant 验证唯一 `prepared` 批次与 prepare execution 的父血缘，响应只返回 `batch_id + engine_id + staging_locator + write_columns`；不返回 DDL、数据库凭据、最终目标或完整逻辑模型。
- 该接口仅面向具有明确消费者的固定 Service Principal，通过 `common/client` 的 Model Client 调用。Develop 使用 `addp-develop`，Quality 接入前不得提前授予其权限；不得发布泛化只读“物化契约快照”接口，也不得让 Orchestrator 直接消费写入上下文。
- `staging_locator` 是执行期内部事实，不进入 Orchestrator Step 参数和任务定义。`write_columns` 严格按已审批 LogicalField 顺序生成，供受控写入编译使用，不构成第二份逻辑模型定义。

## 二、授权边界

Model 资源当前全部属于 Tenant，不存在 Department 或 Project Group Resource Scope Binding。所有 `model.*` Permission 只允许 Tenant Scope。`tenant.data_architect` 是面向 User Principal 的完整 Model 管理角色；`tenant.graph_runtime` 只保留 Graph 导入所需的 Entity 和 EntityRelation 只读权限。

Model 在写入前校验 Standard 引用时，不转发或保存 User Access Token。`addp-model` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.model_runtime`，且该角色只包含 `standard.domain.read`、`standard.element.read`、`standard.dimension_hierarchy.read` 与 `standard.metric.read`。Standard 协调被 Model 引用资源的删除时，`addp-standard` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.standard_runtime`，该角色只包含 `model.standard_reference.update`。平台控制面的 Runtime Role 不参与 Tenant 业务引用校验或删除协调。

Permission Guard 只判断候选能力，Repository 和 Service 仍必须对每个资源及其子资源执行 Tenant 隔离。任何父子写入、删除和关系创建都必须验证完整归属，不能只依赖请求中的全局 ID。

## 三、聚合与引用

- Entity 聚合包含 EntityAttribute；EntityRelation 是连接两个 Entity 聚合的独立关系事实。
- LogicalTable 聚合包含 LogicalField、TableRelation 和 FactMetricMapping。
- LogicalTable 的可选 Entity 引用只表达概念模型来源，不自动同步属性与字段。需要重新生成时必须由显式操作整体替换，不能隐式双向同步。
- DWLayer 是 Tenant 可配置事实。LogicalTable 必须引用已存在的 DWLayer，前端不得维护固定分层枚举作为第二事实源。
- Model 内部引用由数据库外键、唯一约束和 CHECK 约束保证；跨 Standard Schema 的引用先由 Standard HTTP API 验证，再在 Model 写事务中锁定对应的标准引用删除屏障。后台调用、Mermaid 导入和普通 API 写入必须使用同一屏障路径。

### Standard 引用删除屏障

Standard 的业务域、数据元、维度层级和指标被 Model 引用时，不使用跨 Schema 外键，也不允许 Standard 直接读取 Model 私有表。Standard 硬删除这些资源必须通过 Model 的标准引用删除屏障完成影响评估；一次性“查询无引用后直接删除”存在检查与删除之间的竞态，禁止作为正式路径。

Model 为 `(tenant_id, resource_type, resource_id)` 维护单行屏障，状态只允许 `open`、`frozen`、`deleted`。任何可能写入 `domain_id`、`element_id`、`hierarchy_id` 或 `metric_id` 的事务，必须按稳定顺序创建并锁定对应屏障行，只有 `open` 才允许继续；屏障状态检查、Model 业务写入、资源版本和 Tenant 实体模型集合 `revision` 推进必须处于同一事务。Standard HTTP 校验仍在本地事务前完成，不能持有 Model 行锁等待网络。

Standard 删除遵循唯一顺序：Standard 先持久化删除协调记录并将资源置为 `deleting`，再由同一删除协调流程串行锁定 Standard 资源行，调用 Model 原子冻结屏障并权威扫描当前引用。协调流程必须在释放 Standard 资源行锁前完成冻结、引用分支和本地硬删除；这样用户重试、后台补偿和并发删除不会同时执行本地删除。有引用时必须先让 Model 恢复 `open`，再提交 Standard `active`；任一恢复步骤失败都保留 `deleting` 协调记录，供后台补偿继续处理。无引用时 Standard 硬删除资源并保留协调记录，直到 Model 屏障终止为 `deleted` 成功；因此即使资源已硬删除而终态通知响应丢失，后台补偿仍能完成终态收敛。冻结事务与所有新增引用事务锁定同一屏障行：冻结前完成的写入必然进入权威扫描，冻结后到达的写入必然失败，因此不存在在途请求越过扫描的窗口。

`frozen` 和 `deleted` 都禁止新增或保留目标引用；`deleted` 是不可逆终态，防止删除完成前已通过 Standard 校验、但较晚进入 Model 事务的请求在资源删除后落库。协调调用失败时不得绕过屏障继续删除。资源已进入 `deleting` 时重复删除必须复用同一条协调记录并从冻结和权威扫描继续，不能创建第二条强制删除路径。Model 冻结前或本地删除失败时，只能在 Model 成功 `open` 后再恢复 Standard `active`；如果 `open` 失败，资源保持 `deleting`，后台补偿必须重试。已经完成硬删除但终态通知失败时协调记录和 Model 屏障都保持待收敛状态，后续只允许补做 `deleted` 终态；协调记录不得因本地资源已不存在而丢失。

## 四、生命周期

Entity 和 LogicalTable 当前生命周期统一为 `draft` 与 `approved`。只有 `draft` 可修改；审批前必须完成聚合校验。Entity 必须至少包含一个定义完整的属性且至少一个属性为主键；LogicalTable 必须至少包含一个字段且至少一个字段为主键。审批失败必须通过稳定错误码和本地化消息指出缺少属性、字段或主键等具体前置条件，不能统一降级为通用请求校验失败。已审批资源如需修改，必须通过显式重新打开操作回到 `draft`，不能在 `approved` 状态直接修改子资源。

`materialized` 不属于当前正式状态。租户资源回收的 logical 模式可以将已审批资源重新打开为 `draft`，physical 模式必须在单个数据库事务中按聚合顺序删除。

### 完整更新语义

Model 资源统一使用 `PUT` 表达完整更新，不提供并行的部分更新路径。请求必须携带资源的完整可编辑状态；`domain_id`、`entity_id`、`element_id`、`hierarchy_id`、`hierarchy_level`、`length` 等可空字段使用 JSON `null` 表示解除引用或清空值。缺失的可空字段与 `null` 含义一致，不能被解释为保留旧值；必填字段缺失或为空必须返回 `400 invalid_request`。

前端保存详情和子资源时必须提交完整表单状态。对于当前页面不直接编辑但属于资源状态的字段，例如逻辑表的来源实体 `entity_id` 和属性、字段的 `sort_order`，前端也必须从已加载资源中原样带回，不能依赖后端保留旧值。

### 并发版本与聚合写入

Model 遵循平台 API 规范中的资源并发版本规则。`Entity`、`LogicalTable`、`DWLayer` 和 `EntityRelation` 是独立版本主体，数据库均保存非空 `BIGINT version`，创建时从 `1` 开始。`EntityAttribute` 共用所属 `Entity.version`；`LogicalField`、`TableRelation` 和 `FactMetricMapping` 共用所属事实侧 `LogicalTable.version`，这些聚合子资源不得再建立自己的并发版本。

| 写入对象 | 并发版本主体 | 事务边界 |
| --- | --- | --- |
| Entity 基本信息、审批、重新打开、删除 | Entity | 按 `tenant_id + id + version` 条件写入并推进 Entity 版本 |
| EntityAttribute 新增、更新、删除 | 所属 Entity | 校验 Entity 为 `draft`、写入属性并推进 Entity 版本 |
| LogicalTable 基本信息、审批、重新打开、删除 | LogicalTable | 按 `tenant_id + id + version` 条件写入并推进 LogicalTable 版本 |
| LogicalField 新增、更新、删除 | 所属 LogicalTable | 校验 LogicalTable 为 `draft`、写入字段并推进 LogicalTable 版本 |
| TableRelation 新增、删除 | 事实侧 LogicalTable | 锁定并校验事实表和维度表均为 `draft`，写入关系并只推进事实表版本 |
| FactMetricMapping 新增、删除 | 事实侧 LogicalTable | 校验事实表为 `draft`、写入指标映射并推进事实表版本 |
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

Model 的跨资源 ID 必须是正整数；可选引用只允许 `null` 或正整数，不允许使用 `0`、负数或无效字符串表达“未选择”。`sort_order` 和 `hierarchy_level` 必须大于等于 0，逻辑字段 `length` 非空时必须大于 0。`hierarchy_id` 与 `hierarchy_level` 必须同时为空或同时存在，不能保存不完整的维度层级引用。名称、编码、列名、分层编码和关系名称必须在数据库字段定义的字符长度内；Mermaid 导入遵守同一限制，不能把超长输入推迟到数据库报错。上述规则由 HTTP 请求绑定、Service 领域校验和数据库约束共同保证，后台调用不得绕过 Service 校验。

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

## 六、完成条件

- 所有写操作具有 Tenant 隔离和领域不变量测试。
- 所有版本主体和实体模型集合具有成功递增、旧版本无副作用、跨 Tenant 不可探测的并发测试。
- 聚合删除、Mermaid 导入和 physical cleanup 使用事务。
- Model Schema 使用版本化 migration，不在服务启动时执行 `AutoMigrate`。
- API 错误包含稳定 `error_code`，Swagger、前端和跨模块客户端保持同步。
