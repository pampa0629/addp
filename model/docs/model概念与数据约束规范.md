# ADDP Model 概念与数据约束规范

## 一、模块边界

Model 是 Tenant 级数据架构与建模事实的 owner，管理业务实体、实体关系、逻辑模型、数仓分层以及逻辑模型到 Standard 指标的引用。Standard 继续拥有业务域、数据元、维度层级和指标；Model 只保存经过 Standard API 验证的引用，不代理或复制 Standard 资源。

Model 当前不拥有物理表创建和多引擎物化。DDL 预览是 PostgreSQL 方言的设计辅助能力，不改变逻辑模型状态，也不产生物理资源。未来物化必须先形成独立规范，明确目标引擎、执行授权、任务与回收语义后再实现。

## 二、授权边界

Model 资源当前全部属于 Tenant，不存在 Department 或 Project Group Resource Scope Binding。所有 `model.*` Permission 只允许 Tenant Scope。`tenant.data_architect` 是面向 User Principal 的完整 Model 管理角色；`tenant.graph_runtime` 只保留 Graph 导入所需的 Entity 和 EntityRelation 只读权限。

Model 在写入前校验 Standard 引用时，不转发或保存 User Access Token。`addp-model` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.model_runtime`，且该角色只包含 `standard.domain.read`、`standard.element.read`、`standard.dimension_hierarchy.read` 与 `standard.metric.read`。平台控制面的 `platform.model_runtime` 不参与 Tenant 业务引用校验。

Permission Guard 只判断候选能力，Repository 和 Service 仍必须对每个资源及其子资源执行 Tenant 隔离。任何父子写入、删除和关系创建都必须验证完整归属，不能只依赖请求中的全局 ID。

## 三、聚合与引用

- Entity 聚合包含 EntityAttribute；EntityRelation 是连接两个 Entity 聚合的独立关系事实。
- LogicalTable 聚合包含 LogicalField、TableRelation 和 FactMetricMapping。
- LogicalTable 的可选 Entity 引用只表达概念模型来源，不自动同步属性与字段。需要重新生成时必须由显式操作整体替换，不能隐式双向同步。
- DWLayer 是 Tenant 可配置事实。LogicalTable 必须引用已存在的 DWLayer，前端不得维护固定分层枚举作为第二事实源。
- Model 内部引用由数据库外键、唯一约束和 CHECK 约束保证；跨 Standard Schema 的引用由 Standard HTTP API 在写入前验证。

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
| EntityRelation 更新、删除 | EntityRelation | 校验关系版本，同时锁定并校验变更前后涉及的全部 Entity 均为 `draft` |
| EntityRelation 创建 | 创建时无关系版本 | 同一事务锁定并校验两端 Entity 均为 `draft`，新关系版本从 `1` 开始 |
| DWLayer 更新、删除 | DWLayer | 更新按版本条件写入；删除在同一事务完成版本校验、LogicalTable 引用检查和删除 |

版本校验、生命周期校验、引用锁定、业务写入和版本递增必须在同一数据库事务中完成。Service 先读取 `draft` 状态、Repository 随后无条件写入的做法不成立，因为审批可能在两步之间完成。LogicalTable 审批必须在事务内锁定聚合根并校验字段完整性；Entity 审批同理。版本或状态冲突不得留下属性、字段、关系、指标映射或回收队列副作用。

已有资源的更新、删除、审批和重新打开必须在 JSON body 中携带自己的 `version`；聚合子资源写入必须在 JSON body 中携带父资源 `version`。成功的子资源写入至少返回新的父版本，前端后续写请求必须顺序使用该值。`DELETE` 同样只使用 JSON body 传递版本，不接受 query、Header 或服务端当前值兜底。

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
| `409` | 资源版本、唯一约束、生命周期或聚合关系状态冲突 | `resource_version_conflict`、`entity_code_conflict`、`entity_state_conflict`、`entity_relation_conflict` |
| `503` | Standard 引用校验不可完成，包括服务不可达、上游 `5xx`、服务身份/权限错误、令牌获取或响应解码失败 | `standard_service_unavailable` |

Standard 引用校验只有明确的 `404` 或跨 Tenant 隐藏结果映射为引用 `404`；其他失败不能伪装成引用不存在，也不能降级为 Model 通用 `500`。所有 Permission 路由的 Swagger 必须声明 `401/403`；带请求参数的接口必须声明 `400`；带资源路径参数的接口必须声明 `404`。该契约由自动化测试校验。

## 五、Mermaid 与 DDL

Mermaid 导入采用 Tenant 级“实体模型集合”全量替换语义：该集合包含当前 Tenant 的全部 Entity、EntityAttribute 和 EntityRelation。由于它跨越多个独立聚合，不能使用任一 Entity 或 EntityRelation 的 `version` 作为并发边界；Model 必须为每个 Tenant 维护非空 `BIGINT revision`，初始值为 `1`，并由 `model.entity_model_revisions` 的租户单行事实承载。

导出响应必须同时返回 `mermaid_code` 和当前 `revision`；导入请求必须同时携带 `mermaid_code` 和作为编辑基线的 `revision`。导入先完整解析和校验可逆子集，再在单个事务中锁定 Tenant 修订行、校验修订版本、替换集合并执行 `revision = revision + 1`。修订行必须在 Tenant 首次导出或导入前稳定存在，不能依赖锁定现有 Entity 行，因为空集合没有可锁定成员。修订冲突同样返回 `409 resource_version_conflict`，且集合和修订版本均保持不变。

导入是破坏性聚合写入，要求 Entity 与 EntityRelation 的创建、删除权限；租户存在已审批实体时必须先全部重新打开。任何解析、校验、修订冲突或写入错误都整体回滚，不返回部分成功。导入成功后返回新 `revision`，前端立即替换本地修订基线。

Mermaid 可逆子集必须保存实体 code、显示名、属性 code、显示名、数据类型、主键、可空性和关系类型。子集外语法必须明确拒绝，不能静默丢失。

PostgreSQL DDL 预览只接受结构化物化配置。Schema、表、字段和分区标识符必须统一校验与引用；分区类型使用固定枚举；不接受任意 SQL 扩展字段。

## 六、完成条件

- 所有写操作具有 Tenant 隔离和领域不变量测试。
- 所有版本主体和实体模型集合具有成功递增、旧版本无副作用、跨 Tenant 不可探测的并发测试。
- 聚合删除、Mermaid 导入和 physical cleanup 使用事务。
- Model Schema 使用版本化 migration，不在服务启动时执行 `AutoMigrate`。
- API 错误包含稳定 `error_code`，Swagger、前端和跨模块客户端保持同步。
