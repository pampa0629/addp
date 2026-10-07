# ADDP 企业资源目录实现规范

版本：v1.14-draft
更新日期：2026-09-30

本文定义 Catalog 模块的身份、数据、变化、API、权限、搜索和运行契约。概念边界见 [企业资源目录体系图](../concepts/addp企业资源目录体系图.md)。

## 一、适用范围与原则

第一阶段以 Meta DataItem 为自动来源，并开放 Standard Domain、Glossary Term、Element 的基础关联；第二阶段接入 Model Entity 与 LogicalTable；第三阶段接入 Standard Metric；第四阶段接入 Service QueryService；第五阶段接入经过筛选的 Develop 可复用开发成果；第六阶段接入 Workbench 已首次发布的 Data Application。Quality 摘要只通过 owner 公开契约动态读取，不扩展为新的 CatalogEntry 类型或 Catalog 事实副本；统一 Workspace 经跨模块评估确认当前不新增，不预建模块、实体或 `workspace_id`。

实现必须遵守：

1. Catalog 是企业目录身份、来源绑定、业务语义关联、责任和目录可见性的唯一事实源；
2. 专业事实留在 owner 模块，Catalog 只保存最小已观察投影；
3. 跨模块只通过公开 API 和 Tenant Service Access Token，不跨 Schema 查询或建立数据库外键；
4. 除 System 和自身必需 Infra 外，任何业务模块不可达都不影响 Catalog 启动或 Ready；
5. DataItem 变化使用单一、可恢复的游标变化源，不增加 Meta → Catalog 同步回调；
6. 所有处理至少一次投递、幂等应用，不依赖“事件只来一次”；
7. 旧 Asset 自动发现、Meta `AssetRecord` 和含义混杂的搜索路径在相应迁移阶段删除，不保留双写或 fallback。

## 二、身份与聚合边界

### 2.1 CatalogEntry

`CatalogEntry` 是可独立读取和编辑的聚合根，公开 ID 使用 UUID，创建后永久不复用。数据库表 `catalog.entries` 至少包含：

| 字段 | 约束 | 语义 |
| --- | --- | --- |
| `id` | UUID PK | 企业目录稳定身份 |
| `tenant_id` | 非空、索引 | Tenant 隔离事实，不接受调用方提交 |
| `entry_type` | `data_item` / `business_entity` / `logical_model` / `metric` / `data_service` / `development_artifact` / `data_application` | 企业目录对象类型 |
| `entry_status` | `active` / `merged` | 目录记录是否仍为规范身份 |
| `merged_into_entry_id` | nullable UUID | `merged` 墓碑指向的规范条目 |
| `recommended_successor_entry_id` | nullable UUID | 弃用条目可选指向的推荐继任 CatalogEntry |
| `business_name` | nullable | Catalog 拥有的业务名称；空时展示来源投影名 |
| `business_description` | nullable text | 业务说明 |
| `governance_status` | 四值枚举 | `discovered` / `curated` / `certified` / `deprecated` |
| `visibility` | 三值枚举 | `inventory` / `department` / `tenant` |
| `version` | 非空 BIGINT，从 1 开始 | 聚合根乐观并发版本 |
| `created_at` / `updated_at` | UTC | 审计时间 |

`merged` 条目不可再次编目、认证或被新资产选择；详情读取返回 `merged_into_entry_id`，调用方必须解析到规范条目。不得物理删除已经产生业务关系、审计或外部引用的 CatalogEntry。

`recommended_successor_entry_id` 只允许在 `governance_status=deprecated` 的 active 条目上存在。它与当前条目必须不同并属于同一 Tenant；建立时目标必须为 `entry_status=active`、当前来源 `active` 且治理状态为 `curated` 或 `certified`。一个弃用条目最多指定一个推荐继任项，一个条目可以承接多个弃用条目。推荐继任不会合并身份、转移来源或自动跳转，旧条目继续保留完整详情和审计；`merged_into_entry_id` 仍只表达同一企业身份归并，两者不得互换。

### 2.2 SourceBinding

`catalog.source_bindings` 保存来源绑定及历史：

| 字段 | 约束 |
| --- | --- |
| `id` | UUID PK |
| `tenant_id` / `catalog_entry_id` | 非空；只引用 Catalog 自身表 |
| `source_module` | `meta` / `model` / `standard` / `service` / `develop` / `workbench`；后续由规范显式扩展 |
| `source_type` | `data_item` / `entity` / `logical_table` / `metric` / `query_service` / `dev_task` / `data_application` |
| `source_identity` | Meta DataItem fingerprint，Model / Standard / Service / Develop 公开正整数 ID 的规范十进制字符串，或 Workbench Data Application 规范小写 UUID |
| `source_status` | `active` / `missing` |
| `source_version` | owner 变化源给出的可排序单调版本字符串 |
| `bound_at` / `missing_at` | UTC |
| `replaced_binding_id` | nullable UUID，自身历史关联 |
| `missing_reason` | nullable 稳定枚举 |
| `observed_snapshot` | JSONB，最小可重建专业摘要 |
| `observed_at` | UTC |

数据库必须保证：

- 同一 Tenant 的 `{source_module, source_type, source_identity}` 最多一个当前有效绑定；
- 一个 `active` CatalogEntry 最多一个当前 `active` SourceBinding；
- 历史绑定可以同 identity 多条，但只能一条当前有效；
- `observed_snapshot` 只包含列表、搜索、来源失效解释所需的最后观察摘要。Meta 来源可以包含名称、类型、引擎 ID、Meta Item ID、结构摘要、扫描深度和必要展示字段；Model 来源只包含名称、编码、对象种类、专业生命周期、逻辑模型角色、分层和必要的 owner 引用 ID；Standard Metric 来源只包含名称、编码、指标类型、专业状态、生命周期和必要的 Domain、Category、Unit 引用 ID；Service QueryService 来源只包含标题、服务名、服务状态、配置类型、访问模式和必要的 Engine 引用 ID；Develop DevTask 来源只包含名称、说明、开发类型、专业状态和必要的 Engine 引用 ID；Workbench Data Application 来源只包含当前发布 Revision 的名称、说明、专业发布状态、Revision Number 和规范运行路径。不得复制完整 attributes、EntityAttribute、LogicalField、指标定义、公式、派生配置、服务 SQL、协议配置、输出契约、Consumer Descriptor、Develop `content`、查询文本、工作流 DAG、参数、执行配置、Data Application Component、页面布局、参数绑定、Revision 快照、关系、物化配置或内容数据；当前专业详情通过 owner 批量解析 API 动态读取。

`observed_snapshot` 是带 `source_version` 与 `observed_at` 的可重建投影，不是专业事实备份：Catalog 不允许编辑其中字段，owner 当前响应永远权威，且该摘要不能用于恢复 owner 数据。owner 不可达或来源已经删除时，Catalog 可以继续展示最后观察摘要并明确其观察时间，但不得把它表述为当前事实。

### 2.3 CatalogComponent

`catalog.components` 是 CatalogEntry 聚合内的字段或组件：

- `id` 使用 UUID；
- `component_key` 在条目内唯一，第一阶段来自 Meta 结构字段的规范名称；
- `component_status` 使用 `active` / `missing`；
- 组件没有独立 `version`，所有变更使用 CatalogEntry `version`；
- 字段重命名不做模糊跟随；需要保留语义历史时使用显式组件重绑；
- 没有结构字段的 DataItem 不强制创建组件。

组件不是顶级 CatalogEntry，不进入 Asset 选源主列表。

## 三、业务语义与责任模型

### 3.1 Standard 语义关联

语义定义始终由 Standard 拥有。Catalog 只保存软引用、关系角色、验证版本和审计：

| 关联 | 第一阶段基数 |
| --- | --- |
| CatalogEntry → Domain | 最多一个 `primary`，允许零到多个 `secondary` |
| CatalogEntry → Glossary Term | 多对多 |
| CatalogComponent → ElementRevision（StandardMapping） | 每个组件最多一个当前已审核映射；候选可有多条，审核时必须收敛 |

`curated` 及以上 CatalogEntry 通常必须有一个有效 primary Domain。唯一例外是 Standard MetricDefinition 的 `scope_type=platform|tenant_common` 且 owner 未声明 `domain_id`：其“公共且无专属归属域”是明确的专业事实，允许编目，不算待归类。`scope_type=domain` 的指标、Model Entity / LogicalTable 及其他条目仍须有主业务域。Meta DataItem 的 primary Domain 是 Catalog 关联；Model Entity / LogicalTable 和 Standard MetricDefinition 的 primary Domain 是 owner 当前 `domain_id`。Catalog 不因缺少主域而给 owner 管理的对象另建人工主域。Catalog 对自身持有的 Domain、Glossary 和 StandardMapping 通过 Standard owner 契约验证同一 Tenant、对象存在且生命周期允许引用；Standard 名称可进入搜索投影，但不成为 Catalog 权威字段。

StandardMapping 是独立、可审核、可并发编辑的关系事实，不是 CatalogEntry 聚合中的无身份数组项。至少保存 `id + tenant_id + catalog_entry_id + component_id + element_id + element_revision_id + source + confidence + evidence + review_status + version`：

- 新建候选必须明确提交 `element_revision_id`；它必须属于 `element_id`，且在创建时为已发布修订。正式映射不得动态跟随数据元当前修订。仅迁移自无法证明历史采用修订的旧关联可暂时没有 `element_revision_id`，此类 `proposed` 候选只能由人工补选并验证确定修订后进入审核，不能计入覆盖率或数据字典。
- `source` 区分人工与 Copilot 建议；Copilot 只能创建 `proposed` 候选，不能直接写入 `approved`。
- `review_status` 固定为 `proposed|approved|rejected|withdrawn`。只有 `approved` 计入治理覆盖率并进入数据字典。Quality 检查方案独立配置，不以该映射为执行前置。
- 同一组件在任一时点最多一个 `approved` 映射。批准新候选时必须在同一事务撤回旧映射并推进双方版本，不能依赖先查后写。
- `evidence` 保存推荐依据和来源定位；置信度只辅助审核，不代替审核结论。
- CatalogEntry 为 `certified` 时禁止直接创建、编辑、批准、拒绝或撤回映射；必须先走既有撤销认证动作。

CatalogEntry 详情读取 StandardMapping 时，映射身份、审核状态和确定修订 ID 始终来自 Catalog；面向人的数据元名称、编码和修订号只通过 Standard 精确修订批量接口即时解析，作为可丢弃的响应展示摘要，不写入 Catalog 表或搜索投影，也不能按当前修订替换已固定的历史修订。无修订的迁移候选明确显示“待补选修订”；Standard 暂不可达或确定修订已不存在时，详情和映射事实仍可读取，但展示摘要必须标记不可用，不得把数据库修订 ID 伪装成 `R` 修订号或回退为另一修订。候选编辑仍只允许选择当前已发布修订。

旧 `component_element_associations` 仅保存 `element_id` 并随 CatalogEntry 完整更新整体替换，无法冻结修订、承载审核和被 Quality 稳定引用。数据库迁移将无法证明历史采用修订的现有关联转为 `source=legacy`、`element_revision_id=null` 的 `proposed` 候选，保留原 Element 稳定身份与历史观察证据，不用迁移时当前修订伪造已审核历史；迁移事务随后删除旧表，业务 API、请求字段和聚合更新不再提供旧路线。

Model 自身保存的 Entity / LogicalTable `domain_id`、属性或字段冻结的 `element_revision_id`、MetricImplementation 及建模关系是参与审批、设计和物化的专业内生关系，仍由 Model 权威维护，不复制为 Catalog 人工语义关联。Catalog 动态读取并以“Model 声明的专业关系”展示；Model 已声明主业务域时，它构成该 Model 来源条目的有效 primary Domain，Catalog 不接受另一条冲突的人工 primary Domain。修改这类关系必须进入 Model 唯一写路径。

Standard MetricDefinition 的业务定义、统计口径、专业状态、`domain_id`、分类、计量单位和语义依赖全部由 Standard 权威维护；具体粒度、来源、连接、过滤和可执行表达式由 Model MetricImplementation 权威维护。Catalog 不复制这些专业事实，也不允许保存冲突的人工 primary Domain；修改必须进入对应 owner 的唯一写路径。

### 3.2 责任关系

`catalog.responsibilities` 只引用 System 主体和组织身份，不复制成员关系：

| 角色 | 主体 | 基数与要求 |
| --- | --- | --- |
| `accountable_department` | Department | 每个条目最多一个；`curated` 及以上必填 |
| `business_owner` | User | 每个条目最多一个；`curated` 及以上必填 |
| `data_steward` | User | 零到多个；`curated` 及以上至少一个 |
| `technical_owner` | User | 零到多个，可选 |

Project Group 不作为长期责任主体。它只在后续协作集合、草稿、评审和治理任务中使用。User 离职或 Department 失效不会自动删除 CatalogEntry；Catalog 将关系标记为待移交并进入治理队列。

System 的 Department / Project Group 管理契约以 `system/docs/IAM数据模型与迁移规范.md` 为准。Catalog 不创建、修改或关闭组织对象，只通过 System 精确解析接口验证责任引用，并在已引用 Department 或 User 变为不可引用时建立本地责任治理任务。Project Group 只作为后续 Catalog 协作集合的主体，不进入长期责任关系。

责任失效对账使用 System 同一个精确批量解析契约，不新增组织变化副本或第二条事实同步路线。Catalog 按 Tenant 周期性读取当前 `active` / `needs_transfer` 责任，最多 200 个一批进行解析，并在 Catalog 本地事务中执行：

1. `active` 引用变为不存在或不可引用时，将关系标记为 `needs_transfer`，打开一条 `responsibility_transfer` 治理任务，递增 CatalogEntry 聚合版本并写入领域审计和搜索投影任务；
2. 同一条责任重复对账保持同一个 open 任务，不重复递增聚合版本或制造重复审计；
3. 同一 System 身份恢复为可引用时，关系恢复为 `active`，治理任务以 `reference_restored` 自动解决，并递增聚合版本；
4. 未认证的已编目条目通过 `PUT /entries/:id` 完整编目更新替换责任；已弃用条目只通过 `PUT /entries/:id/responsibilities` 完整替换责任子资源。被删除或替换的失效责任任务以 `responsibility_replaced` 在同一事务自动解决；不提供独立“忽略失效责任”或手工关闭任务 API。

`catalog.governance_tasks` 是 Catalog 内派生的治理工作事实，不是责任关系副本。第一阶段字段固定表达 CatalogEntry、任务类型、责任角色、主体类型与 ID、失效原因、已观察主体摘要、`open` / `resolved` 状态、打开/解决时间和解决方式；数据库必须保证同一 Tenant、CatalogEntry、责任角色和主体最多一条 open `responsibility_transfer` 任务。任务不跨 Schema 建外键，System 不保存其反向投影。

#### 3.2.1 责任与实际数据授权

已确认、待贯通实现的目标边界：上述责任不是全平台 IAM Role，不自动授予编目、源数据读取、写入或授权办理权限。业务责任人确认共享的业务合理性，数据管理员协助界定资源范围和编目，技术维护者校验技术可执行性；技术维护者不等于编目提交人。实际访问始终要求功能权限与精确资源授权同时满足。

业务共享确认与源读取授权办理分别使用独立功能 Permission，首版仅通过角色显式分配，不默认加入内置管理员权限，不随编目人、责任人或管理委派自动授予；现有 `catalog.entry.update` 及 `system.engine_access_delegation.create/read/revoke` 均不能替代。具体 Permission 随真实消费入口统一发布，不登记启用的占位权限。

普通只读允许当前有效业务负责人显式确认自用或本人当前项目组受益；每次显式选择“指定到期时间”或“长期有效（直至撤销）”。前者必须填写未来绝对到期时间，无全平台统一天数上限；后者无到期日期。遗漏模式不默认永久，矛盾参数拒绝。保存与重试统一截断为 UTC 微秒精度，不向后延长期限；长期有效仍受当前身份、组织成员关系、功能权限、撤销、Explicit Deny 及安全条件约束。不放宽临时接入、管理委派、敏感原值复核、写入或 DDL。业务确认和 System 正式受理、Grant 生效分别记录，不能以决定存在宣称已经授权。

业务决定生产入口的首条范围是 Meta 已扫描数据库表，只读取 Meta 当前 item／祖先链和 System 当前引擎能力描述，不探测源库、不读取数据值。Meta Item ID 仅用于定位读取；必须核对当前指纹、Engine、完整父节点链及声明的 catalog model，构造保留节点原名的完整 EngineCatalogPath，不能拆 `full_name` 或仅凭已观察快照猜测目标。不支持的目录模型明确拒绝，不回退为整个引擎授权；后续其他类型按其真实路径契约扩展。

`POST /entries/:id/sharing_decisions` 使用 `catalog.entry.read` 和独立 `catalog.sharing_decision.create`，仅接受已验证 Tenant User。请求包含调用方生成的 `decision_id` UUID（同参恢复身份）、条目 `version`、接收主体类型 `user | project_group`、字符串 ID、必填 `expiry_mode=at_time|until_revoked` 和用途原因。`at_time` 必须提供未来绝对 `expires_at`；`until_revoked` 的 `expires_at` 只能省略或为 null，响应统一为 null。固定动作 `read`，不接受自报确认人、源路径、权限或确认时间。当前业务负责人身份与独立功能权限取交集；当前源必须 active、条目不得合并或弃用，不要求先达到 `curated`。

新决定在 Catalog 自有事务按条目→当前来源→责任顺序核对版本与来源依据，写入不可变决定及审计并递增条目 version；依赖模块读取在本地事务前完成，不持本地锁进行网络调用。决定绑定完整源路径、当前来源绑定及版本、负责人关系 ID、确认人的 Principal／Tenant Membership／授权版本、接收主体、用途及期限；不保存 Token、Role 或可编辑责任副本。同编号、同确认人、同业务参数重试返回原决定，不再递增版本或审计；换参冲突。该记录不能替代后续正式受理时的当前责任与接收主体核验。

`GET /entries/:id/sharing_decisions/:decision_id` 使用相同功能权限，返回当前可见条目下的不可变原记录：原确认人可读取本人历史，当前有效业务负责人可复核本条目的全部历史。不进行再次确认、不延期、不返回 System Grant 或受理状态；办理及签发历史通过决定下的专用只读结果列表查询。原确认人的历史读取不要求仍为当前业务负责人；未找到、跨租户、超出复核范围或不可见统一 404。生产入口不创建待核清事实、不冻结责任；待核清只在后续真实办理准备时建立。已提交原请求的 System 核清／关闭、可信正式准备、首次受理和 Catalog 自动签发续办已接通；执行侧读取仍须另行贯通后才能宣称数据访问闭环。

共享接收方选择（2026-10-03 已确认）：`GET /entries/:id/sharing_recipient_candidates` 与业务确认使用相同的两个独立功能权限，并核验当前 Tenant User 是本条目的有效业务负责人；条目须可见、active、未弃用且具有 active Meta DataItem 来源。`recipient_type=user|project_group`、`search`、`page`、`page_size` 使用有界分页（每页最多 50 项）。允许选择本 Tenant 的有效账号或项目组，包括确认人未加入的项目组；只返回类型、稳定字符串 ID、名称、编码和状态，不返回成员、角色或数据。System 通过既有专用 Catalog Runtime 组织候选提供最小事实，Catalog 不复制组织；网络调用不持 Catalog 锁，返回前复核同一来源、同一责任关系、可见性和权限期限。候选不是共享决定、批准依据或数据访问权，正式提交仍须重新核验。通用 `/reference-candidates` 不新增项目组枚举，`/me/project-groups` 仍只表达本人有效成员关系；不得借组织管理权限或手填 ID 绕过这一入口。

授权办理人的候选读取独立使用 `GET /entries/:id/sharing_decision_candidates`，同时要求 `catalog.entry.read` 与 `system.engine_access_fulfillment.create`。只接受 System 已验证的当前 Tenant User，使用本次 User Token 向 System 的 `GET /engines/:id/access_handling_scope` 核验原身份、授权版本、有效功能权限和引擎管理委派；不使用 Catalog 机器身份代查人的资格。网络核验不得置于 Catalog 行锁事务内，返回前再次检查条目可见性、当前来源绑定和版本、原业务负责人关系及有效期。分页只针对本条目当前来源和原责任仍有效的候选，不提供全租户枚举、完整用途正文或既有决定的编辑能力。弃用、失效、移交和换源后的旧决定不进入候选，同账号重新接任也不复活旧决定。候选只是当前观察，正式准备和 System 首次受理仍须重新核验确认人的当前 IAM 资格、接收主体及批准要求；读取不创建待核清记录、不冻结条目、不产生受理或 Grant。System 失联、鉴权拒绝或响应绑定错误不降级放行。

正式办理准备所用的 Catalog 当前依据核验必须读取已持久决定，而非调用方传入的决定正文，并在本模块事务内按条目→当前来源→原责任关系顺序锁定。必须核对同租户、同条目、同一个仍为当前且有效的 Meta 来源绑定及版本，以及同一个仍为 active 的业务负责人关系 ID 和确认账号。移交后重新把同一账号设为负责人形成的新关系，不复活旧关系上的决定。只修改业务名称、说明或其他非批准依据而推进聚合版本，不单凭版本不相等使决定失效；聚合版本倒退则拒绝。到期判断在锁定后使用数据库墙钟，长期有效仍不能绕过当前依据核验。该内部核验不获取远端事实，不创建待核清、受理或 Grant；还须由后续可信消费者另外核验实时源路径、System 当前身份和 Permission、办理委派及接收主体，不能以返回决定对象替代这些门禁。

有效期模式与可空日期共同参与决定及受理的不可变参数绑定。`common/authorization` 仅提供两模块共用的普通只读共享有效期校验、微秒规范化与比较，不保存业务事实或代替权限判断。存量限时历史在模块迁移事务中补为 `at_time`，保留原日期、身份和审计；不改为长期有效，不保留缺模式请求的兼容解释。Catalog 迁移必须在排他表锁下、同一事务内调整不可变触发器并恢复保护，不能在运行时更新历史。

Catalog 拥有当前责任资格及企业关联，System 的引擎访问控制领域拥有物理源数据访问规则；Catalog 可以提供业务入口，但不保存源数据 Grant 副本，也不成为数据访问或 System 启动前提。普通只读共享的同人确认／办理、接入阶段限时只读、责任移交及 Catalog 不可用的唯一目标规则见 [授权上下文规范 5.5](addp授权上下文规范.md#55-目录责任业务授权决定与源数据授权)，不由编目聚合更新隐式完成。

Catalog 的部署或注册不代表具体资源已经建立业务责任，不能把平台曾注册 Catalog 作为永久禁止独立批准的判据。责任历史核验针对精确资源；未知历史与临时不可用不得自动解释成无责任，也不替代 System 的批准要求。用户应能明确停止使用 Catalog，且不因过去部署过而丧失其他模块的正常能力；已明确要求 Catalog 批准的资源需明确退出后的批准依据，不通过停用开关、清空历史或删除来源绑定暗中完成交接。仅建立责任但尚未启用 Catalog 批准的资源，不因此需要退出交接。

2026-10-01 已确认：System 在引擎访问控制领域保存精确源目标的版本化“源数据授权批准要求”，不保存 Catalog 的可编辑责任副本。Catalog 停用或不可达不自动解除要求；有独立退出／治理 Permission 的主体可以明确范围及有效承接安排，由 System 变更批准要求并追加审计，不删除 Catalog 历史、不改动既有 Grant。长期不使用 Catalog 的部署使用 System 独立批准 Permission 与有效引擎管理委派，不被强制套用临时接入的期限建议。首次启用要求独立治理配置 Permission 与有效引擎管理委派，不由编目权限推导；退出前已正式接受的同次、同参办理仍可在原 5 分钟窗口内完成。详细目标规则见授权上下文规范 5.5.5；可信交接事务协议待落实，当前没有可据此调用的退出接口。

责任人更换或失效，不一律撤销已生效源数据规则；旧责任不得支持新的业务确认，新责任人复核既有共享，由有办理资格的主体落实必要撤销。责任治理任务、`needs_transfer`、编目状态和条目信息可见性不是实际数据 Allow，也不得通过在同一请求中把自己设为责任人取得授权批准资格。生产入口已核验当前业务负责人并保存不可变决定，正式受理通过专用 Runtime 反查复核当前责任和持久决定来源，并与 System 本地 IAM、请求及目标仲裁衔接；实际授权规则仍待贯通，不新增通用审批表、源数据授予 API 或可配置审批引擎。

已确认的责任资格分界是 Catalog 最终核验当前责任及精确业务决定后，System 持久提交本次正式受理回执：接受前移交须由新负责人重新确认，接受后移交仅允许已接受的同一次、同参数办理继续完成。Catalog 不独立保存第二份可编辑接受状态；展示及未知结果恢复读取 System 原回执，不以 Catalog 后续第二次提交决定是否受理。正式受理必须与责任变更、弃用、来源重绑和批准要求退出形成可验证顺序，不等于源访问已经生效，也不代表 System 后续办理资格检查可以省略。自动办理窗口使用 System 回执的原受理时间，为 5 分钟；指定到期时间时另受该日期截断，长期有效仍只有 5 分钟；它不限制人员操作时间，不是数据访问期限，重试不得刷新。到期未生效须重新核验并接受，已生效但响应丢失只返回原办理结果；详细规则见授权上下文规范 5.5.3。接口、故障协调和幂等消费仍待实施，不能把现有编目更新、审计或只读查询伪装为已经实现的接受动作。

业务责任建立的分界是首次明确保存经 System 核验有效的 `business_owner`，不等待 `curated`；只保存部门、业务域或其他责任不算。CatalogEntry 内部字段 `business_responsibility_established` 保存不可回退的历史依据：`false` 表示从本条目新建开始确认尚未建立，`true` 表示已建立过，`NULL` 表示旧历史不足、无法判定。它不通过编目请求编辑，不加入用户 API、搜索或专业事实投影，也不是新的治理状态。2026-10-07 已确认：保存负责人仅建立责任，不隐式切换 System 批准方式；明确启用 Catalog 批准后才要求新办理取得业务依据。缺少批准要求仍须显式配置，不因缺少责任或模块不可达放行。

Meta 和专业来源新建条目时显式写入 `false`；完整责任替换路径在保存有效业务负责人时同事务置为 `true`，编目审计同时保存本次完整责任输入。撤销编目、责任移交、来源观测、来源重绑、认证及弃用操作保留依据。数据库拒绝把 `true` 清为 `false/NULL`，也拒绝通过更新把旧历史 `NULL` 改成 `false` 或把已知 `false` 改成未知。旧数据迁移仅以当前业务负责人、已完成编目的当前／审计状态或明确的责任角色审计补为 `true`；只有责任数量、仅有发现记录或没有审计均不足以补为 `false`，不伪造历史操作人、时间或授予权限。迁移与新约束同事务并可重复运行，不递增业务聚合版本或触发搜索投影。

该字段仅证明本企业身份的历史，不能单独证明某个 EngineCatalogPath 从未建立责任；来源重绑与旧绑定历史、当前主体资格、精确目标核验及责任变化和规则写入的并发契约仍须另行贯通。System 不保存可编辑的责任副本，已有合法访问不增加 Catalog 在线依赖。

来源已观察摘要中的数值 Engine ID／Item ID 只能无损解析：小数、非有限值以及超出浮点安全整数范围的浮点输入不得截断或猜测为其他 ID。原生整数保留完整精度；无效输入不能用于调用专业模块查询，并在展示／投影中保持未解析，不改写来源身份，也不因此补成“从未建立责任”。该输入校验不等于完整 EngineCatalogPath 定位或资源授权证明。

已弃用条目禁止新的业务共享确认与正式接受办理；弃用前已接受的同一次、同参数办理仅按原 5 分钟窗口继续。已有访问规则不因目录弃用自动撤销，撤销弃用也不恢复未接受的旧决定或过期依据。接受和弃用的先后须由权威事务确定，不能依据客户端时间或界面状态。

源数据授权核验协调采用授权上下文规范 5.5.5 的持久待核清事实。Catalog 只保存发送前精确请求、受理核清及自动办理核清时间，不保存 System 受理状态、受理时间或 5 分钟截止时间副本。责任保护仅取决于受理核清时间 `resolved_at`；自动办理核清时间 `grant_reconciled_at` 独立用于恢复调度，不延长保护。待核清依据索引按 Tenant 与 CatalogEntry 定位；同一条目锁内登记、核清及依据变更必须串行化。责任替换只在实际角色／主体集合变化时受阻，普通名称和说明维护仍允许；来源同步及责任失效对账不得旁路。正式首次登记及新受理发送由下述人类入口发起；故障恢复独立消费已提交的原请求，不创建新办理。

内部待核清准备只接受决定引用与本次可信消费链提供的调用主体、原操作账号来源、批准要求版本和实时精确路径；接收主体、动作及有效期从持久决定派生，不接受另一份可编辑决定正文。精确路径须与决定完整相同，不能仅比较指纹或 Engine。首次登记在条目→来源→责任锁内核验当前决定依据，并提交不可变请求绑定；调用方必须提交该事务后才能发送。同编号恢复比较完整绑定，返回原待核清记录和原创建时间，不再次核验历史决定为新批准、不延长期限；已核清记录仍只返回历史，不重新打开。换租户、条目、决定、路径、调用主体、操作来源或批准要求版本均冲突。该内部函数不证明输入来源可信、不执行当前 IAM／Permission／委派核验，也不发送、不核清或发放 Grant；正式消费者须按下述契约完成这些核验，不直接暴露内部存储原语。

故障核清消费只读取已提交的原待核清记录，在 Catalog 事务外查询 System 原结果；仅当权威查询明确未找到时，才请求关闭同编号、同参数的原请求。关闭可能返回先提交的受理结果，不能假定它一定关闭成功。超时、拒绝、绑定冲突或数据库错误不等于未找到，不得解除本地保护。收到可信权威结果后，完整核对请求编号、Tenant 和不可变绑定，仅接受 `accepted` 或 `closed`，再按条目→待核清记录顺序使用本地短事务填写一次数据库墙钟核清时间。并发／重复核清保留原时间，不复制 System 受理时间、截止时间或 Grant，不依赖原操作账号、原责任或拟授权期限仍有效；历史核清不恢复新办理资格。

正式准备使用 `POST /entries/:id/sharing_fulfillments`，仅接受请求编号、决定编号及批准要求版本。当前 User 须具备条目可见、`catalog.entry.read`、独立 `system.engine_access_fulfillment.create` 及有效引擎管理委派；操作身份从可信 AuthContext 派生，调用服务身份由当前 Tenant Service AuthContext 核验，不接受正文覆盖。远端资格查询不持 Catalog 行锁，本地条目锁内按数据库墙钟复核权限／可见性并登记完整绑定；必须提交后才发送 System `/runtime/engine-access-fulfillments/:request_id/accept`。通信错误或未知响应保留 `pending`，不回滚已提交保护，也不把错误当作关闭；同参受理／关闭结果按原绑定核清，不在 Catalog 落受理结果副本。

原办理请求只读找回（2026-10-03 已确认）：同一路径的分页 GET 只列当前 Tenant、当前仍可见条目下由本人原先办理、且本人当前仍有对应原引擎管理范围的请求；详情 GET `/entries/:id/sharing_fulfillments/:request_id` 按持久完整绑定向 System 查询原结果。两者均要求当前 Tenant User、`catalog.entry.read` 与独立 `system.engine_access_fulfillment.create`，部门、项目组和编目维护权不能替代。管理范围逐一以原请求的 Engine 为准，不以条目换源后的 Engine 代替；列表在资格过滤后计数、稳定分页，不返回他人请求或机器调用身份／历史授权版本。原操作身份仅用于识别本人的历史与匹配权威查询，不要求历史授权版本等于当前版本，也不能恢复当前资格。

列表只返回原编号、决定引用、批准要求版本、原目标、接收方、动作、期限及创建时间，不从 Catalog 核清时间推断受理。详情结果只读消费 System 原回执，`pending` 表示权威明确尚未找到结果，`accepted`／`closed` 表示不可变历史受理／关闭；依赖失败或绑定错误返回 503，不能伪装为 pending、closed 或数据已授权。即使原窗口到期或条目已弃用，仍可在当前读取资格内查询历史，不重新核验为新的业务批准。所有网络调用在 Catalog 事务外，返回前重新检查权限期限、条目可见性、原绑定和当前原引擎管理资格。查询不提交、不关闭、不填写核清时间、不新增审计或 Grant、不延长窗口；不能复用正式准备 POST 或会关闭未受理请求的后台恢复命令。显式重试另用原编号及原三个参数，不自动生成新编号、补新版本或续期。

可信 owner 依据使用 `POST /runtime/sharing-fulfillments/:request_id/basis`，只允许 `addp-system` 当前 Tenant Service OAuth 身份和独立 `catalog.sharing_fulfillment.read`，不用于人的候选浏览。精确匹配已提交且未核清的完整请求绑定，在条目→来源→原责任关系锁内核验当前决定依据，等待条目锁后再次检查仍待核清；只返回原绑定及确认身份，不返回业务用途正文。System 在自身数据库锁外反查，随后独立核验当前 IAM、权限、委派、接收主体及批准要求。可选 System 反查凭据缺失只阻断新受理，不影响 Ready、历史核清／关闭，不推断 Catalog 退出。受理回执不是 Grant 或内容访问权。

生产恢复使用 `common/client.SystemFulfillmentClient`，复用现有 `addp-catalog` 的独立 Tenant Service Token，消费 System 专用核清与签发守卫及权限。后台复用来源同步间隔和已发现 Tenant，至少等待原请求创建满一分钟后扫描自动办理尚未核清的记录，按创建时间和请求编号做每批 100 条的 keyset 分页；失败行不阻塞后续批次，单项续办有 30 秒 Context 边界。调度等待不是五分钟办理窗口，不以记录年龄判断关闭或删除。通信错误只延后重试，不门控 Ready、不擅自解除未核清受理的依据保护，也不重新冻结已核清的依据；后台不创建准备记录或新受理。这里的 HTTP／数据库测试不替代真实 OAuth 与双服务 Online T4；受理及自动签发已接通，不代表执行侧数据读取已贯通。

普通只读办理的目标值校验与编码统一归 `common/authorization.EncodeSharingTarget`，Catalog 待核清准备／绑定读取和 System 受理／关闭／批准要求边界复用同一实现：必须是带结构根的有效叶子 `EngineCatalogPath`，Engine ID 为正且可精确表示为 int64，最多 64 个 segment（含结构根），编码后的完整 JSON 不超过 16 KiB。该限制沿用 System 内部边界，不改变引擎路径模型、Meta 扫描层级或现有业务确认 API；Catalog 必须在首次登记待核清前拒绝不满足同一边界的目标，不能先冻结依据再等 System 拒绝。编码完整保留节点原名、大小写、空白和分隔符；无效 UTF-8 明确拒绝，不允许 JSON 替换字符悄然合并不同身份。不修剪、不合并、不回退到 fingerprint 或 display path。共享库不核验身份、责任、Permission、批准要求或实际源端存在性。

人类界面（2026-10-03）：数据项详情通过“共享确认与请求” Tab 组合专用接收方候选、显式业务确认、本人原确认查询、正式办理和本人原办理请求查询，不新建独立入口或可编辑决定副本。确认表单要求接收方下拉选择、明确有效期模式和用途；新确认需要当前业务负责人及独立确认 Permission，编目权限不能替代。页面先固定同一次决定编号与参数，再将编号写入公开 URL，随后发送 POST；通信不确定后只允许原参数显式重试。重载按 URL 编号 GET 查询，不从浏览器重构或自动发送决定；本地丢失原参数时不能假造原重试。办理人从本条目分页候选选择决定，以本人 User Token 将原结构化目标交给 System 的 `POST /engines/:id/access_handling_requirement` 只读观察，只有明确的 `catalog` 模式及字符串版本才可显式提交。原请求编号、决定编号及预期版本固定一次，提交前保存原请求编号到公开 URL；重复点击及显式同参重试不换编号、不补新版本，刷新仅查询原请求。批准要求缺失、资格不足或不可达不自动初始化，也不让用户手填 ID 或版本。原请求的列表及结果均只读，查询错误清除旧结果但不伪装为 pending；accepted 不表示实际 Grant 生效。

2026-10-04 自动签发续办：`fulfillment_checks.grant_reconciled_at` 是一次性本地调度终结时间，必须已有 `resolved_at` 且不早于它。两个标记不可清除或改写，原绑定和创建时间始终不可变；新标记不参与责任／来源依据保护，不进入用户 DTO、搜索或授权判定。旧记录新增字段保持 NULL，由恢复消费者查证。前台受理核清后与后台扫描调用同一续办命令；已核清受理但尚未核清自动处理的记录也必须扫描。原回执为 accepted 时先读签发历史，明确未找到才按原参签发；closed、已有签发历史、或签发明确返回原窗口到期／原请求关闭才终结调度。其他失败保留待续办。复用现有一分钟宽限、100 条游标分页、单项 30 秒边界，不刷新原窗口、不保存第二份 Grant、不在历史 GET 中执行续办、不门控 Ready。完整边界见授权上下文规范 5.5.3。

共享历史复核（2026-10-07 已确认）：`GET /entries/:id/sharing_decisions` 提供稳定分页的确认历史，详情仍使用同一路径下的决定编号 GET。两者要求当前 Tenant User、条目可见、`catalog.entry.read` 与 `catalog.sharing_decision.create`；原确认人可读本人记录，当前有效业务负责人可复核本条目的全部历史。仅编目权限、部门或项目组关系不取得该读取资格；移交后的原确认人仍可读本人历史，接任负责人不需要原办理人的功能权限或引擎委派才能复核。过滤在计数和分页之前完成，不以当前来源或原确认期限排除历史。

`GET /entries/:id/sharing_decisions/:decision_id/fulfillments` 在同一复核资格内分页查询该决定对应的办理及签发历史。Catalog 只读本地不可变请求绑定，事务外按完整绑定向 System 查询原受理结果及签发历史，返回前再次核验条目可见性、当前读取权限和责任资格。列表中 `pending` 仅表示 System 明确未找到受理结果，`accepted/closed` 表示历史受理／关闭；可空 `granted_at` 只表示签发历史，不代表当前访问 Allow、未被撤销或安全条件已满足。依赖或绑定失败返回错误，不伪装为未办理、未签发或当前有权限。不创建请求、不关闭、不核清、不签发、不写审计或 Grant 副本。本人原办理结果查询复用同一权威结果解析，并展示签发历史；原办理人的读取守卫不放宽。

共享 Tab 在普通进入条目时加载上述确认列表，选择记录后同步公开 URL 并只读查询原确认及其办理结果；新确认表单与历史记录入口分开。当前业务负责人可以复核他人的历史确认，其他确认人员只看到本人记录。确认、受理和签发分别展示，页面不把历史签发称为当前数据访问已授权。

任务式交互第一步（2026-10-07）：上述 Tab 的产品名称收拢为“共享数据”，在同一页面展示“业务确认 → 授权办理 → 系统签发”和当前记录的下一步提示，不新增权限、工作流实体或 API。没有选择记录时只说明可开展的操作，不把其他历史请求当作本次共享的进度。仅选择确认时，概览使用该确认首页最新一次办理的只读结果；翻阅其他页只展示历史浏览提示，不把分页结果推断为全局授权状态。请求与确认编号同时存在时，结果必须属于所选确认，否则明确提示记录不匹配；没有办理权限的复核者，可使用当前已获准读取的确认结果列表中精确匹配该请求的结果，不要求或绕过办理人的独立查询资格。未找到匹配记录或依赖失败保持未知，不伪装为未办理或办理失败。提交办理时同步记录所选确认及请求编号，防止换选确认后保留另一份确认的 URL。签发始终表达历史事实，不表达当前访问 Allow。

查看一份确认或成功提交新确认后，具有办理功能权限的人员可只读加载现有办理候选；仅当本页候选包含该编号时自动选中并观察原目标的批准要求，未出现则仍由用户按现有分页选择，不用确认详情绕过候选资格核验。加载、选择和刷新不自动初始化批准要求或提交办理；正式办理、首次配置、同参重试仍分别由人明确点击。首次配置与日常办理在页面中分组说明，技术版本不再作为主流程文案展示。切换确认时清除旧办理结果及公开请求编号；未确定的原命令仍禁止切换，避免丢失同参恢复上下文。业务负责人、授权办理人和接收方允许不同；办理人不必成为共享接收方，也不因办理资格获得数据访问。

### 3.3 个人目录视图、收藏与关注

第一阶段不新增 `PersonalWorkspace`。`catalog.entry_marks` 以 `{tenant_id, user_id, catalog_entry_id, mark_type}` 保存当前 User 对 CatalogEntry 的个人关系，`mark_type` 只允许：

- `favorite`：快速回看标记；
- `following`：希望接收该条目后续变化通知的订阅意图。

收藏与关注相互独立，不进入 CatalogEntry 聚合版本，不改变目录可见性、责任、治理状态、搜索排序或底层数据授权。关注在通知能力落地前只保存订阅意图，不能伪装为已经发送通知。User 失去目录可见性后，原标记保留但不再通过“我的目录”返回；重新获得可见性后自然恢复展示。

“我的目录”第一阶段由当前 User 动态查询形成，只提供 `responsible`、`favorite` 和 `following` 三种关系视图。`responsible` 来自 Catalog 责任关系，另外两种来自 `entry_marks`。治理队列继续使用 `/governance/tasks`，最近访问继续由 Console 最近访问事实提供；Catalog 不复制这两类事实。尚未建立独立草稿模型前，“我的草稿”不返回伪造结果，也不新增空壳实体。

个人标记只允许 `principal.type=user` 操作，并复用 `catalog.entry.read`；Service Principal 不得建立个人标记。写入使用完整目标集合 `{favorite, following}` 原子替换当前 User 在该条目上的标记，不接受客户端提交 User ID 或 Tenant ID。

### 3.4 Project Group 目录集合

`catalog.collections` 是 Project Group 范围的独立协作聚合，至少保存 UUID、Tenant、`project_group_id`、名称、说明、正整数 `version`、创建人和时间；`catalog.collection_entries` 保存集合与 CatalogEntry 的成员关系及添加人。数据库必须保证同一 Project Group 内集合名称唯一、同一条目在同一集合中最多出现一次，并只对 Catalog 自身条目建立外键。

集合不是 CatalogEntry、Asset、Workspace 或目录可见性边界。加入集合不改变 CatalogEntry 聚合版本、治理状态和责任，也不向项目组成员授予目录可见性或底层数据访问权；列表必须再次应用 CatalogEntry 权威可见性规则。

集合访问同时满足两层判断：当前 AuthContext 中存在该 Project Group 的有效成员关系，且调用者对精确 Project Group Scope 或 Tenant Scope 拥有对应 `catalog.collection.read` / `catalog.collection.update` Permission。Project Group ID 只来自集合事实或当前 AuthContext，不信任客户端自报成员资格。项目组关闭后不再出现在有效 AuthContext，集合与成员历史保留但不能继续读取或修改；Catalog 不建立 System 组织副本，也不通过项目组关闭删除 CatalogEntry。

集合创建、名称/说明更新、条目完整替换和删除均使用集合 `version` 作为乐观并发边界并写入 Catalog 领域审计。成员替换一次提交完整 CatalogEntry UUID 集合，不能提供逐条追加与完整替换两条并行写入路线。

### 3.5 推荐继任关系

推荐继任是 Catalog 当前唯一自有的跨 CatalogEntry 业务关系。它服务于目录弃用后的治理迁移，不扩展为通用关系表、`RelationType` 配置或任意关系编辑器：

- `recommended_successor_entry_id` 是 CatalogEntry 聚合字段，只随唯一治理子资源 `PUT /entries/:id/governance` 在弃用或维护弃用信息时更新，并使用同一个聚合 `version`；通用编目更新不得读写它，也不新增独立关系写接口；
- 只有持有 `catalog.entry.deprecate` 的用户可以在弃用时设置，或在已弃用条目上变更、清除推荐继任项；
- 目标校验只读取 Catalog 自身同 Tenant 事实，不调用专业模块，不形成新的启动或 Ready 依赖；
- 赋值时目标必须是来源有效的 `curated` 或 `certified` active 条目；目标后续来源缺失或继续弃用时，既有关系作为历史治理事实保留，并按当前目标状态明确展示；
- `depends_on`、`produces`、`serves`、血缘等关系继续归专业 owner；术语同义、首选和替代语义归 Standard；泛化 `related` 不进入数据模型。

## 四、状态与可见性

### 4.1 治理状态转换

允许的单一路线：

```text
discovered ⇄ curated ⇄ certified
               ⇅           ↓
           deprecated ←────┘
```

- `discovered → curated`：业务名称、说明、适用时有效的 primary Domain、责任部门、业务责任人和至少一个数据管理员完整；Model 来源的 primary Domain 取 owner 当前声明，不要求也不允许 Catalog primary 副本；Standard 公共指标无专属域时遵守 3.1 节唯一例外；
- `curated → discovered`：唯一表示“撤销编目”。它不是普通字段编辑或任意状态回退，而是把误编目或不再具备完整业务治理事实的条目原子恢复为自动发现状态；请求必须同时清空 Catalog 自有业务名称、业务说明、Domain、Glossary、责任和推荐继任关系，并把可见性恢复为 `inventory`。来源绑定、CatalogEntry 稳定身份、专业 owner 事实、独立 StandardMapping、版本和审计历史必须保留；StandardMapping 如需撤回，必须走自身审核生命周期，不能隐藏在条目更新中；
- `curated → certified`：需要独立认证权限和认证审计；认证只确认当前 CatalogEntry 聚合版本和当前已审核 StandardMapping 集合，不允许在同一请求中改变业务名称、说明、语义关联、责任、映射或可见性；
- `certified → curated`：唯一表示“撤销认证”。必须具有认证权限并填写原因，只改变治理状态并完整保留当前编目事实；撤销后使用普通编目更新完成修订，再由同一路径重新认证；
- `curated|certified → deprecated`：必须具有弃用权限并填写原因，只改变治理状态和可选推荐继任项，不允许夹带业务编目修改；
- `deprecated → deprecated`：治理接口只允许具有弃用权限的用户填写原因并变更或清除推荐继任项；独立责任子资源允许读取及编目维护权限持有者移交责任，无需弃用权限。业务名称、说明、语义关联、可见范围设置和来源绑定继续冻结；
- 弃用时可以指定一个推荐继任项；推荐继任项不是必填，没有替代资源时允许为空；
- `deprecated → curated`：唯一表示“撤销弃用”，需要读取、编目维护和弃用权限，填写原因并校验当前版本；保留编目事实，清除当前推荐继任项并在审计中保留原关系。不自动回到 `certified`，不恢复源数据访问、源对象或资产发布。除上述明确转换外不允许任意回退。

责任移交只接受 `version`、`reason` 与完整 `responsibilities`，仅适用于 active 的已弃用条目；要求一个有效责任部门、一个有效业务负责人和至少一个有效数据管理员，技术负责人可选，候选由 System 当前事实核验。拒绝夹带其他字段，不依赖 Standard 可达。移交在同一事务锁定条目、递增聚合版本、替换责任、解决旧任务并记录 `catalog.entry.responsibilities_transferred` 审计与搜索投影。部门可见条目更换责任部门时，当前可发现范围随之变化，前端必须提示；不把责任身份变成 IAM 或数据授权。

通用编目更新 `PUT /entries/:id` 只维护 `discovered|curated` 阶段的完整编目聚合和撤销编目，不接受进入、维持或退出 `certified|deprecated` 的请求。认证、撤销认证、弃用、撤销弃用和弃用信息维护唯一使用 `PUT /entries/:id/governance`，请求携带当前聚合 `version`、目标 `governance_status`、按转换要求填写的 `reason` 和可选 `recommended_successor_entry_id`。服务端在同一事务中锁定 CatalogEntry、校验状态与版本、更新治理状态或推荐继任项、递增版本并分别写入 `catalog.entry.certified`、`catalog.entry.certification_withdrawn`、`catalog.entry.deprecated`、`catalog.entry.deprecation_withdrawn` 或 `catalog.entry.deprecation_updated` 审计；该治理写路径不替换编目关联，不重新调用 Standard / System 的专业引用解析服务。HTTP 接口仍遵守 System 统一认证与权限校验要求，不能把“不重新解析引用”理解为 System 不可用时自动放行。所有治理操作和已弃用条目责任移交均校验当前条目可见性。

### 4.2 目录可见性

`visibility` 与治理状态分离：

- `inventory`：只对具有企业资源盘点权限的治理/技术人员可发现；自动建档默认值；
- `department`：责任部门有效成员和具有盘点权限的人员可发现；
- `tenant`：Tenant 内具有目录读取权限的人员可发现。

`discovered` 只能使用 `inventory`。`curated` 可以选择三种可见性；`certified` 必须为 `department` 或 `tenant`。目录可见不授予底层内容读取、预览、下载、查询或资产使用权。

### 4.3 目录视图

Catalog 列表只提供两个相互排他的目录视图：

- `governance`：默认视图，只包含 `curated|certified|deprecated` 条目；调用者拥有 `catalog.inventory.read` 不改变该默认值。
- `inventory`：企业资源盘点视图，包含 `discovered|curated|certified|deprecated` 条目，必须同时具有 `catalog.entry.read` 和 `catalog.inventory.read`。

Console 的固定页面标题使用“企业资源目录”，侧边栏入口使用“资源浏览”，两个视图标签分别使用“已编目资源”和“资源盘点”。“已编目资源”包含 `curated|certified|deprecated`，不表示全部已认证、质量合格或已发布为资产。不得同时使用“目录浏览”“治理目录”“企业目录导航”等相近名称制造另一套目录概念。`discovered` 条目主操作为“开始编目”；`curated` 条目主操作为“编辑编目”，认证和撤销编目放在明确的治理操作中；`certified` 条目只提供“撤销认证”和“弃用资源”，不显示通用编目编辑器；`deprecated` 提供“维护弃用信息”、权限感知的“撤销弃用”与“移交责任”，移交复用既有编辑器的责任部分，不开放其他冻结事实。

视图是同一组 CatalogEntry 的权限感知查询，不新增实体、复制条目或维护双轨索引。DataItem 全量自动建档且可在 `inventory` 查询；完成业务编目后，同一 CatalogEntry 自然进入 `governance` 视图。

目录浏览采用“主业务域 + 上下文分面 + 权威分页列表”，不建立持久化企业目录树。Standard Domain 是主业务分类，Accountable Department 是可交叉的组织责任分面，Entry Type 是资源形态分面；三者不能被固化为 Domain 拥有 Department、Department 拥有资源类型的父子事实。前端只在 `/entries` 路由中按“业务域 → 责任部门 → 资源类型”逐步缩小当前查询，所有选择写入规范 URL，并继续由 `/entries` 返回同一批 CatalogEntry。选中业务域时，同页展示 Standard 当前定义、层级和按资源类型汇总的上下文；汇总只使用相同的 `/entries/facets`，类型入口改变同页筛选，不生成第二套资源列表。业务域之外的分面不因同样可筛选就各自建立概况页。

Catalog `GET /domains` 仅对 `catalog.entry.read` 开放，动态使用 `addp-catalog` Tenant Service Token 读取 Standard 当前业务域树，响应裁剪为稳定 ID、名称、编码、定义、父域 ID 与层级；不返回 Standard 审计字段或专业对象详情，不落库或缓存完整 Domain 树。该接口使 `/entries` 的业务域选择包含当前尚无可见条目的 Standard Domain，而 `/entries/facets` 只统计当前可见 CatalogEntry 引用的域；两者不能互相替代。它不等于授予调用者 `standard.domain.read` 或其他 Standard / Model 读取权限。Standard 暂不可达时只使域定义和完整候选暂不可用，Catalog 启动、Ready、条目列表和现有已引用域分面继续工作。上下文中的类型数量仅使用当前调用者权限、查看范围、所选精确主域和责任部门口径，不包含名称及高级筛选条件；列表结果数量仍以实际查询为准。普通读者不见资源盘点，专业详情仍直接到 owner 按 User Token 判权。

前端把业务域、责任部门和资源类型表达为三个独立、可搜索且带计数的浏览维度，不显示 `1/2/3` 层级编号或父子树外观；已选维度以可独立清除的当前范围展示。名称搜索保持常显，来源状态、治理状态、目录可见性和来源引擎属于高级筛选，默认折叠。详情页优先显示业务可理解的概览和编目信息，来源身份、owner 当前事实、联邦专业读模型、关系与审计分别进入“专业事实”和“关系与历史”；进入编辑模式时只显示完整编目表单，不在同一滚动页面下继续重复只读详情。

资源盘点视图的业务域导航可以提供“待归类”虚拟入口，但它不是 Standard Domain、没有稳定 Domain ID，也不进入 `/entries/facets` 的 Domain 候选。该入口唯一映射到 `view=inventory&coverage_dimension=primary_domain&coverage_state=missing` 的权威治理缺口查询；进入时清除名称搜索、已选业务域及其下游责任部门和资源类型，退出后恢复普通目录导航。缺口视图不得继续把“待归类”表现为导航父节点，避免把治理状态误装成企业分类事实。

## 五、Meta DataItem 可恢复变化契约

### 5.1 唯一传输路线

Meta 在与 DataItem 创建、目录摘要更新或失效相同的数据库事务中追加 `meta.data_item_changes`。Catalog 按 Tenant 使用 `addp-catalog` Service Access Token 拉取：

```http
GET /api/v1/meta/data-items/changes?after_cursor={opaque}&limit=200
```

空 `after_cursor` 表示从该 Tenant 的变化历史起点读取。`limit` 默认 200、最大 500。响应：

```json
{
  "schema_version": "meta.data_item_changes/v1",
  "changes": [
    {
      "change_id": "opaque-id",
      "operation": "upsert",
      "source_identity": "sha256-fingerprint",
      "source_version": "00000000000000000042",
      "observed_at": "2026-08-26T10:00:00Z",
      "snapshot": {
        "name": "orders",
        "item_type": "table",
        "engine_id": 12,
        "item_id": 34,
        "scanned_depth": "deep"
      }
    }
  ],
  "next_cursor": "opaque-cursor",
  "has_more": false
}
```

`operation` 只能是 `upsert` 或 `missing`。fingerprint 作为 `source_identity`；数据库行 ID、路径字符串和扫描 execution ID 不得成为来源身份。

`change_id` 和 cursor 对消费者完全不透明，只能原样保存和回传。`source_version` 则固定为 20 位零填充十进制字符串，消费者只允许按字符串等值和大小比较，用于忽略重复或倒序变化，不得把它解释为 Meta 数据库主键或自行构造。

变化日志第一阶段不设置时间保留窗口。首次迁移必须为已有 DataItem 原子补齐 `upsert` 记录，从而允许新的 Catalog 从空库只通过该变化源重建。

Meta 使用 `meta_item` 表上的 PostgreSQL trigger 作为唯一变化捕获边界：任何 Repository、批量 SQL、软删除、恢复或硬删除只要修改权威表，就必须在同一数据库事务内自动追加变化记录。应用代码不得再增加第二套手工 append、回调或双写逻辑；禁用或绕过该 trigger 写入 `meta_item` 视为缺陷。trigger 只读取 `meta_item` 当前行构造最小快照，不查询 System 或其他模块。

### 5.2 幂等消费与检查点

Catalog 表 `catalog.source_checkpoints` 以 `{tenant_id, source_module, feed_name}` 唯一，保存 opaque cursor。一次批次必须在同一 Catalog 数据库事务中：

1. 按 `source_version` 忽略重复或倒序变化；
2. 幂等创建或更新 CatalogEntry、SourceBinding 和组件；
3. 写入 Catalog 自身投影任务；
4. 推进 checkpoint。

事务失败不得推进 cursor。Catalog 重启后从已提交 cursor 继续；Meta 不可达只记录同步滞后和重试，不影响 Catalog Ready。

来源同步与人工编目、治理、责任维护及来源重绑共用聚合锁顺序：批次先锁 owner checkpoint，再只读定位已有绑定，按 CatalogEntry UUID 升序锁定本批全部既有条目，最后按 SourceBinding UUID 升序锁定仍属于这些条目的 current 绑定。不得按变化事件顺序逐条锁绑定后再更新条目；人工路径不得反向取得 checkpoint。预定位后绑定若移至本批未锁定的条目，整批拒绝并回滚，由原变化源重试，不追到未锁定的新条目继续写入。锁内只应用本批已取得的来源事实，不调用专业模块网络接口；同步不改变弃用状态或责任建立历史。

Catalog 的 reconciliation 仍使用同一变化源和同一幂等处理器：管理员可以把 checkpoint 重置到历史起点后重放，不能另建“全表轮询 + 另一套 upsert 规则”。

Catalog 的平台后台同步通过 `addp-catalog` Platform Service Access Token 访问 `GET /api/v1/system/runtime/tenants`，发现已初始化的 active Tenant。该路由必须同时校验 Platform Service Context 和 `platform.tenant.read`；Catalog 不得使用仅供平台 User 管理的 `/api/v1/system/platform/tenants`，System 也不得为 Service Token 放宽该 User 管理路由。System 不可达只使同步滞后并后台重试，不得阻止 Catalog 进程启动。

### 5.3 其他 owner 读取契约

第一阶段新增或收敛以下精确 ID 批量读取能力，单次最多 200 个引用：

- Meta：按 fingerprint 批量解析当前 DataItem 摘要；
- Standard：按 `{object_type, id}` 批量解析 Domain、Glossary、Element 的存在性、状态、版本和显示摘要；
- System：按 `{subject_type, id}` 批量解析 Department、User、Project Group 的存在性、状态和显示摘要。`user.id` 使用全局稳定 User ID，不保存 Tenant Membership ID；只有该 User 的 Principal 和当前 Tenant Membership 均有效时才允许建立新责任关系。Project Group 只用于目录集合协作范围的当前名称解析，不进入责任候选。

精确 ID 集合使用单个请求体，不接受 Tenant ID。所有接口只信任 Bearer 中的 Tenant Context，并固定校验 `addp-catalog` OAuth Client 与 owner Permission。响应按请求顺序返回，并为每个引用给出 `found`、`referenceable`、owner 状态、版本和最小显示摘要；跨 Tenant 对象与不存在对象都只返回 `found=false`，不得泄露其真实状态。`found=true, referenceable=false` 表示同 Tenant 对象仍存在但已不允许建立新关联，Catalog 可以据此展示和触发责任移交。批量解析用于用户写入时校验、展示修复和失效对账，不替代 DataItem 变化源。

### 5.4 Model 专业资源变化与动态引用

Model Entity 和 LogicalTable 无论处于 `draft` 还是 `approved`，只要已经正式持久化，就分别自动建立 `business_entity` 与 `logical_model` CatalogEntry。`draft` 只表示 Model 专业生命周期，初始目录条目仍使用 `discovered + inventory`；Catalog 不把 Model 审批状态改写为自己的治理状态。

Model 在聚合根创建、版本推进或删除的同一数据库事务中，通过 PostgreSQL trigger 追加 owner-local、append-only `model.catalog_resource_changes`。EntityAttribute 写入必须推进 Entity 版本，LogicalField、TableRelation、DimensionHierarchy、MetricImplementation 等写入必须推进 LogicalTable 版本，因此只监听两个聚合根即可完整观察专业定义变化，不增加 Service 手工双写。

Catalog 使用 `addp-catalog` Tenant Service Access Token 和 `model.catalog.read` 拉取：

```http
GET /api/v1/model/catalog-resources/changes?after_cursor={opaque}&limit=200
```

响应 Schema 固定为 `model.catalog_resource_changes/v1`，变化项包含 `source_type=entity|logical_table`、规范十进制字符串 `source_identity`、`operation=upsert|missing`、20 位 `source_version`、`observed_at` 和最小 `snapshot`。首次迁移必须回填所有现存 Entity 与 LogicalTable；变化历史不设保留窗口，checkpoint、条目写入与投影任务在 Catalog 单事务提交。

当前 Model 摘要通过唯一批量动态解析接口读取：

```http
POST /api/v1/model/runtime/catalog-references/resolve
```

请求包含 1 到 200 个 `{source_type, source_identity}`，不接受 Tenant ID；响应按请求顺序返回 `found`、Model 当前 `status`、资源 `version`、最小摘要和规范详情路径。跨 Tenant与不存在统一 `found=false`。接口只允许 `addp-catalog` Service Client 与 `model.catalog.read`，不接受 User Token 代理、Internal API Key 或 Tenant Header。

Catalog 的来源绑定和最后观察摘要保证企业身份稳定、离线列表与失效解释；Model 完整详情及专业内生关系保持动态引用。Model 不可达只使当前专业详情标记为不可解析并延迟后台同步，不影响 Catalog Alive、Ready、业务编目事实或已建立目录身份。

### 5.5 Standard Metric 专业资源变化与动态引用

Standard Metric 无论处于 `draft`、`approved` 还是 `deprecated`，只要已经正式持久化，就自动建立 `metric` CatalogEntry。Standard 专业状态不改写 Catalog 治理状态，初始条目仍使用 `discovered + inventory`。

Standard 在 Metric 聚合根创建、版本推进或删除的同一数据库事务中，通过 PostgreSQL trigger 追加 owner-local、append-only `standard.catalog_resource_changes`。指标数据元映射和依赖关系写入必须与 Metric 版本推进处于同一事务，因此只监听 Metric 聚合根；不得在 Service 层增加向 Catalog 的同步双写。

Catalog 使用 `addp-catalog` Tenant Service Access Token 和不可委派、不可定制的 `standard.catalog.read` 拉取：

```http
GET /api/v1/standard/catalog-resources/changes?after_cursor={opaque}&limit=200
```

响应 Schema 固定为 `standard.catalog_resource_changes/v1`，变化项固定 `source_type=metric`，`source_identity` 使用公开正整数 ID 的规范十进制字符串，`operation=upsert|missing`。最小观察摘要须包含 `scope_type`；有 owner 主业务域时包含 `domain_id`，无专属主域时不伪造空 ID。首次迁移必须回填所有现存 Metric；此后摘要契约补充字段时，Standard 用同一 append-only 变化源为存量指标补发事件，由 Catalog 依单调事件 ID 正常追赶，不直接修改 Catalog 来源表。变化历史不设保留窗口，Standard、Model 和 Meta 各自使用独立 Catalog checkpoint，任一来源不可达都不能阻塞其他来源或 Catalog Ready。

当前 Metric 摘要通过唯一批量动态解析接口读取：

```http
POST /api/v1/standard/runtime/catalog-references/resolve
```

请求包含 1 到 200 个 `{source_type=metric, source_identity}`，响应按请求顺序返回 `found`、当前专业状态、资源版本、最小摘要和 `/standard/metrics/{id}` 详情路径。跨 Tenant 与不存在统一 `found=false`。接口只允许 `addp-catalog` Service Client 与 `standard.catalog.read`；现有 `/references/resolve` 继续只承担 Domain、Glossary、Element 业务语义关联校验，两类契约不得合并。

Catalog 的动态详情以 Standard 当前响应为权威；Standard 暂不可达或 Metric 已删除时，Catalog 只以 `unavailable` / `missing` 明确展示最后观察摘要，不把投影表述成当前指标事实。

### 5.6 Service QueryService 专业资源变化与动态引用

Service 当前只有 QueryService 同时具备正式发布快照、稳定公开 ID 和 Consumer Descriptor，因此第四阶段只接入 QueryService。GraphQueryService、TileService 与 RegisteredService 尚未形成统一稳定消费契约，不得从管理 DTO 推断企业服务语义；它们必须在 owner 契约成熟后再显式扩展本规范。

QueryService 创建即形成正式持久化服务定义，不存在另一套 draft 聚合。因此 `active`、`inactive`、`error` 都自动建立并保留同一个 `data_service` CatalogEntry，状态仅表示 Service 当前专业状态；只有物理删除才产生 `missing`。Catalog 治理状态和 Service 可消费性保持正交。

Service 通过 PostgreSQL trigger 将 `service.query_services` 新增、更新和删除追加到 owner-local `service.catalog_resource_changes`，首次迁移回填所有现存 QueryService。变化日志 ID 同时作为最小专业摘要的单调版本；Catalog 使用独立 `service/catalog_resource_changes` checkpoint 拉取：

```http
GET /api/v1/service/catalog-resources/changes?after_cursor={opaque}&limit=200
```

当前摘要通过下列唯一批量接口动态读取：

```http
POST /api/v1/service/runtime/catalog-references/resolve
```

请求固定 `source_type=query_service`，响应返回当前 `status`、最新摘要变化版本、最小摘要和 `/service/published-services/{id}` 详情路径；跨 Tenant 与不存在统一 `found=false`。两个接口只允许 `addp-catalog` Service Client 和不可委派、不可定制的 `service.catalog.read`。

QueryService 的 SQL、发布快照、协议配置、输出字段、稳定键、访问端点和 Consumer Descriptor 全部由 Service 权威维护，Catalog 不复制为组件或专业事实。QueryService 当前没有 owner Domain 字段，因此其 primary Domain 仍由 Catalog 维护；辅助 Domain、Glossary、企业责任、可见性和治理状态同样归 Catalog。

### 5.7 Develop 可复用开发成果变化与动态引用

Develop 只将已持久化、可被人员重复编辑或被 Orchestrator 稳定引用的 `dev_tasks.dev_type=query|workflow` 视为可复用开发成果，并自动建立 `development_artifact` CatalogEntry。`active`、`inactive`、`archived` 只表达 Develop 专业状态，不改写 Catalog 治理状态；软删除或物理删除产生 `missing`。

`script` / Notebook 任务当前只有空的闭合执行契约，且包含交互会话与私有文件语义，不自动建档。即时查询、`common.task_executions`、执行历史、运行结果、Notebook Session 和 ToolApproval 都是过程或私有事实，不得伪造 CatalogEntry。

Develop 通过 PostgreSQL trigger 将非删除 `query|workflow` DevTask 的新增、更新、软删除和物理删除追加到 owner-local `develop.catalog_resource_changes`，首次迁移回填现存对象。`script` 始终被 trigger 排除，不为其保留兼容变化路线。变化日志 ID 同时作为单调摘要版本，Catalog 使用独立 `develop/catalog_resource_changes` checkpoint 拉取：

```http
GET /api/v1/develop/catalog-resources/changes?after_cursor={opaque}&limit=200
```

当前摘要通过下列唯一批量接口动态读取：

```http
POST /api/v1/develop/runtime/catalog-references/resolve
```

请求固定 `source_type=dev_task`，响应返回当前专业状态、最新摘要版本、最小摘要以及 `/develop/sql?action=edit&id={id}` 或 `/develop/workflow?action=edit&id={id}` 规范详情路径；跨 Tenant、不存在、已删除和 `script` 统一 `found=false`。两个接口只允许 `addp-catalog` Service Client 和不可委派、不可定制的 `develop.catalog.read`。

DevTask 的 `content`、查询文本、工作流 DAG、公开参数、物化输入、执行配置、Engine 绑定和执行契约全部由 Develop 权威维护。Catalog 只保存可重建的最后观察摘要，不创建 CatalogComponent，不将 Develop 内容备份或投影为可编辑事实。DevTask 当前没有 owner Domain，因此 primary / secondary Domain、Glossary、企业责任、可见性和治理状态归 Catalog。Develop 不可达只使当前专业详情不可解析并延迟后台同步，不影响 Catalog Ready。

### 5.8 Quality 当前摘要动态关联

Quality 评分、当前 Issue 和 execution 历史都是 Quality 专业事实，不是新的 CatalogEntry 类型，Catalog 不保存其副本或反向写回。第一阶段只为 `source_module=meta`、`source_type=data_item`、`item_type=table` 且具有结构化 `engine_id + schema_name + table_name` 的 PostgreSQL DataItem 动态解析 Quality 摘要。

Meta DataItem 变化摘要必须由技术事实直接携带 `schema_name` 和 `table_name`，Catalog 不得拆分 `full_name`、locator 或搜索文本猜测物理定位。Catalog 使用 `addp-catalog` Tenant Service Access Token 和 `quality.catalog.read` 调用：

```http
POST /api/v1/quality/runtime/catalog-summaries/resolve
```

详情读取时按精确物理表引用动态组合 `configured`、最近 execution 状态、当前关联方案的规则通过率、open Issue 数量和 Quality 详情路径。Quality 不可达时 Catalog 返回 `unavailable`，不回退到旧评分；未配置返回 `not_configured`，不解释为高质量或失败。本阶段只在 Catalog 详情动态展示，不提供质量搜索过滤或排序，避免为了分页投影复制 Quality 事实。

### 5.9 Meta 数据血缘的用户上下文联邦视图

数据血缘节点、边、时态证据和当前投影继续只归 Meta。Catalog 不保存血缘副本，不新增 Catalog Backend 代理接口，也不使用 `addp-catalog` Service Access Token 代替用户查询血缘。

第一阶段仅对当前来源满足 `source_module=meta`、`source_type=data_item`、`source_status=active` 且已观察摘要具有规范正整数 `item_id` 的 active CatalogEntry 展示数据血缘。Catalog Frontend 使用当前 User Access Token 直接调用 Meta 唯一图接口：

```http
GET /api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item_id}&direction=both&depth=2&limit=100
```

`depth=2` 是 Catalog 首次展示的层数；用户在共享血缘查看器中切换层数时，Catalog 必须以所选层数重新查询 Meta，而不是仅改变下拉框显示。

Meta 继续执行 `meta.lineage.read`、Tenant 和资源可见性校验。Catalog 前端复用 `common-frontend/graph` 的 `LineageViewer` 和 DTO 标准化能力，并合并共享双语词条；不得复制图组件、解析 locator 或从 `full_name` 猜测 Meta Item 身份。

血缘请求与 CatalogEntry 详情请求相互独立。无权限、Meta 主体不存在和 Meta 暂不可达必须分别表达，任何失败都不能隐藏目录详情、阻止业务编目或进入 Catalog Ready 条件。G6 图依赖必须按需加载，不能进入 Catalog 首屏主包。

本节只完成 Meta 数据血缘的联邦展示，不将其包装成已经完成的通用跨模块关系图。Model 建模关系、Standard 指标依赖等专业关系仍需各 owner 先形成权限感知的公开查询契约；Catalog 自有人工业务关系只有在关系类型和业务用例明确后才建模，且不得与 `derive`、`serve` 等专业关系类型重叠。

### 5.10 Model / Standard 专业关系查询契约

进入企业目录的专业来源由 owner 提供一跳专业关系图，第一批只覆盖 Model Entity、Model LogicalTable 和 Standard Metric：

```http
GET /api/v1/model/entities/{id}/relations?limit=100
GET /api/v1/model/logical-tables/{id}/relations?limit=100
GET /api/v1/standard/metrics/{id}/relations?limit=100
```

`limit` 默认 100、最大 200。响应统一使用 `addp.professional_relations/v1`，至少包含 `subject`、`nodes`、`edges` 和 `truncated`。节点稳定身份由 `{owner_module, resource_type, resource_id}` 构成；边必须保留 namespaced `relation_kind`、权威方向及 owner 能够直接证明的名称、说明、字段端点、权重或备注，不得把不同 owner 的同端点边合并为一种“通用关系”。第一批关系固定为：

- Model Entity：`model.entity.one_to_one|one_to_many|many_to_many`；
- Model LogicalTable：`model.logical_table.entity`、`model.logical_table.fk|join`、`model.logical_table.supports_metric`；
- Standard Metric：`standard.metric.base_metric`、`standard.metric.dependency`。

指标数据元映射、Domain、分类、单位等仍在 owner 当前专业详情中展示；它们不是当前 CatalogEntry 来源类型，因此本阶段不伪造目录节点。后续只有对应对象正式进入企业目录，或明确需要保留非目录专业节点时，才扩展本契约。

这些路由只接受当前 User Access Token，并分别校验 Model Entity / EntityRelation / LogicalModel 或 Standard Metric 的读取权限。Catalog Frontend 直接调用 owner；Catalog Backend 不代理，不使用 `model.catalog.read`、`standard.catalog.read` 等机器权限替代用户权限，也不持久化响应。Owner 不可达、无权限或主体不存在只改变当前关系卡片状态，不影响 Catalog 启动、Ready、条目详情及其他 owner 卡片。

### 5.11 联邦影响分析与来源身份解析

企业目录详情把影响关系组合为一个联邦视图，但不建立统一关系事实表，也不把不同 owner 的边改写成 Catalog 关系：

- Meta 血缘继续由当前 User Token 直接查询 Meta，并保留方向、深度和时态证据；
- Model / Standard 专业关系继续由当前 User Token 直接查询事实 owner，并保留 namespaced `relation_kind`；
- 推荐继任继续直接读取 CatalogEntry 聚合，是 Catalog 当前唯一自有跨条目关系；
- Quality 摘要、Domain、责任等没有明确影响边语义的事实不得为凑图而伪造关系。

Catalog 只为联邦导航提供自己拥有的来源绑定解析：前端把 owner 已返回的 `{owner_module, resource_type, resource_id}` 转成 `{source_module, source_type, source_identity}`，调用 `POST /entries/resolve-sources` 批量解析到当前调用者可见的 CatalogEntry。该接口最多接受 200 个精确来源引用，只查询当前 `SourceBinding` 与 CatalogEntry，不调用 owner、不保存专业节点或边、不根据名称猜测匹配。请求使用 `catalog.entry.read` 并复用条目详情的目录可见性规则；具有 `catalog.inventory.read` 时可以解析盘点条目，否则 `inventory` 条目自然不可见。跨 Tenant、不存在或当前不可见统一返回 `found=false`。

联邦视图必须按 owner 分区表达来源状态。任一 owner 无权限、不可达或主体缺失只使对应分区不可用，其他分区和 Catalog 详情继续工作；不能使用 `addp-catalog` Service Token 扩权代查，也不能把动态响应写入 Catalog 数据库或搜索索引。

### 5.12 治理覆盖率动态聚合

治理覆盖率是 Catalog 自有治理事实的权限感知读模型，不是持久化投影。`GET /governance/coverage` 固定覆盖当前 Tenant 的资源盘点视图，因此同时要求 `catalog.entry.read` 与 `catalog.inventory.read`，并只统计 `entry_status=active` 的 CatalogEntry。聚合必须直接读取当前权威表，不创建覆盖率表、缓存副本或后台同步任务。

第一阶段固定返回治理状态分布与以下七个可独立处置的条目级维度；每个维度都返回 `covered`、`applicable`、`not_covered`、`not_applicable` 和百分比 `coverage_rate`，其中百分比等于 `covered / applicable * 100`，无适用对象时为 `0`：

| 维度 | 适用分母 | 覆盖判定 |
| --- | --- | --- |
| `business_definition` | 全部 active 条目 | 业务名称和业务说明均非空 |
| `primary_domain` | 除 `scope_type=platform|tenant_common` 且无 owner `domain_id` 的 Standard MetricDefinition 外的 active 条目 | Catalog 自有 primary Domain 存在，或 Model / Standard 最近一次最小观察摘要具有 owner `domain_id`；公共指标记为不适用，不进入“待归类”缺口 |
| `accountable_department` | 全部 active 条目 | 至少存在一个 active 责任部门 |
| `business_owner` | 全部 active 条目 | 至少存在一个 active 业务责任人 |
| `data_steward` | 全部 active 条目 | 至少存在一个 active 数据管理员 |
| `glossary` | 全部 active 条目 | 至少关联一个 Catalog 自有 Glossary Term |
| `component_standard_mapping` | 至少有一个 active CatalogComponent 的条目 | 该条目的全部 active CatalogComponent 都有一个已审核 StandardMapping |

责任覆盖率必须使用 `accountable_department`、`business_owner`、`data_steward` 三个原子维度，不保留同时要求三项完整的复合 `accountability` 维度。`curated` 状态仍由 4.1 节聚合写路径同时校验三项责任；治理状态表示整体准入结果，覆盖率维度则负责准确指出需要处置的具体缺口。`glossary` 是观察维度，不作为 `curated` 的必备状态条件；`component_standard_mapping` 对没有 CatalogComponent 的专业条目标记为不适用，不能用全体条目作为分母制造虚假低覆盖率。覆盖率只说明企业目录治理完整度，不说明底层数据质量、内容授权、Owner 专业模型完整度或资产发布资格。

覆盖率页面必须能够沿同一权威口径下钻到待治理条目，但不得为此新增覆盖率明细表、任务实体或搜索投影字段。`GET /entries` 通过成对参数 `coverage_dimension=<固定维度>&coverage_state=missing` 返回该维度当前适用且未覆盖的 active CatalogEntry；这两个参数只允许与 `view=inventory` 同时出现，缺少任一参数、使用其他状态或在治理目录视图提交均返回 `400`。第一阶段只实现 `missing`，不预建未形成处置价值的 `covered`、`not_applicable` 等并行状态。

治理状态分布与七个维度缺口是两种不同的下钻：点击状态数量进入 `view=inventory&governance_status=<该状态>`；其中 `discovered` 表示尚未完成业务编目，进入条目后按单项编目流程处理。点击维度的“未覆盖”数量则进入上述缺口视图，并按维度提示处理入口：具有目录维护权限时，主业务域和责任部门可在适用条目上使用批量分配；业务定义、人员责任和术语进入条目编目且遵守状态门禁；组件标准映射进入数据项的映射审核。专业 owner 自有业务域不能由 Catalog 批量覆盖。分配主业务域或责任部门不推进 `discovered → curated`，任何下钻也不自动创建责任治理任务。

缺口列表的 SQL 判定必须与本节覆盖率聚合复用同一组适用性和覆盖谓词，使列表 `total` 精确等于当前权限与其他结构化筛选共同约束后的缺口数量。名称全文搜索由 Meilisearch 投影负责，治理缺口由 PostgreSQL 当前事实负责；第一阶段二者互斥，避免按搜索分页候选再做数据库过滤导致漏项或虚假总数。前端从覆盖率页进入缺口列表时不得携带名称搜索，并在缺口视图中禁用名称搜索；退出缺口视图后恢复普通目录搜索。

### 5.13 Workbench Data Application 专业资源变化与动态引用

Workbench Data Application 在首次发布不可变 Application Revision 后才建立 `data_application` CatalogEntry。未发布草稿是创建者私有工作成果，不进入企业资源盘点；CatalogEntry 标识稳定的 Data Application 聚合根，不标识单个 Revision。重新发布只推进同一个来源绑定的观察版本，下线只把 Workbench 专业状态改为 `offline`，两者都不改变 Catalog 治理状态、企业身份或来源 `active` 状态。

Workbench 通过 PostgreSQL trigger 在首次发布、重新发布和下线的同一数据库事务中追加 owner-local、append-only `workbench.catalog_resource_changes`。只修改未发布草稿、且没有改变 `current_revision_number` 或 `publication_status` 时不得产生目录变化；首次迁移只回填已经存在 `current_revision_number` 的 Data Application。变化日志 ID 同时作为单调摘要版本，Catalog 使用独立 `workbench/catalog_resource_changes` checkpoint 拉取：

```http
GET /api/v1/workbench/catalog-resources/changes?after_cursor={opaque}&limit=200
```

变化 Schema 固定为 `workbench.catalog_resource_changes/v1`，`source_type=data_application`，`source_identity` 使用 Data Application 规范小写 UUID，`operation` 当前固定为 `upsert`。已经产生 Revision 的 Data Application 不允许物理删除，因此不得预设日常 `missing` 路线；如果未来要引入彻底删除，必须先补齐 Catalog、Asset、Grant 和审计的正式回收状态机。

当前专业摘要通过下列唯一批量接口动态读取：

```http
POST /api/v1/workbench/runtime/catalog-references/resolve
```

请求只接受 1 到 200 个 `{source_type=data_application, source_identity=<uuid>}`，响应按请求顺序返回 `found`、当前 `published|offline` 专业状态、最新目录变化版本、当前 Revision Number、最小摘要以及 `/data-apps/{application_id}` 规范运行路径；跨 Tenant、不存在和从未发布统一 `found=false`。两个接口只允许 `addp-catalog` Service Client 和不可委派、不可定制的 `workbench.catalog.read`。

Data Application 的草稿、Component、页面布局、参数、绑定、Revision 快照和内容哈希全部由 Workbench 权威维护，Catalog 不复制为 CatalogComponent 或可编辑专业事实。Catalog 或 Asset 可见性不授予应用执行权；应用运行仍由 Workbench 校验 `workbench.data_application.execute` 与 owner Resource Grant / Policy，组件查询继续由 Service 独立执行最终数据授权。

Catalog 提供给 Asset 的 `POST /api/v1/catalog/runtime/references/resolve` 除可组合、可发布状态外，必须返回当前 `entry_type` 以及唯一当前来源的 `source_module`、`source_type`、`source_identity`。这些字段是一次动态解析结果，不成为 Asset 的来源绑定副本。`application` 类型 Asset 只接受唯一一个 `entry_type=data_application`、`source_module=workbench`、`source_type=data_application` 的 primary Component；`source_identity` 必须是规范小写 Data Application UUID。Asset 使用该解析结果建立 owner 履约目标，不从展示名称、运行路径或手工 URL 猜测资源。

### 5.14 数据字典联邦读模型

数据字典是当前物理字段事实与指定查询时点标准解释的组合视图，不是 Catalog、Meta 或 Standard 的新持久化实体。第一阶段只适用于当前来源为 `meta/data_item`、来源状态为 `active` 且具有规范正整数 `item_id` 的 CatalogEntry。

Catalog 提供唯一查询路径：

```http
GET /api/v1/catalog/entries/{id}/data-dictionary?as_of={RFC3339}
```

- `as_of` 可选，省略时由 Catalog 在一次请求中固定一个 UTC 服务器时点；显式值必须是带时区的 RFC3339 时间。
- Catalog 先使用现有目录可见性规则校验条目，再使用 `addp-catalog` Tenant Service Access Token 调用 Meta `GET /api/v1/meta/items/{item_id}/fields?include_details=true` 读取当前物理字段。Catalog 不从已观察摘要伪造当前字段，也不解析路径猜测 Meta 身份。
- Catalog 用自身权威且已审核的 StandardMapping 把 Meta 字段连接到确定的 `element_revision_id`，然后通过 Standard 的精确修订批量读取契约解析该不可变数据元修订及其固定引用的码值集修订；不得按稳定 `element_id` 和查询时点重新选择另一修订。
- Standard 提供只读 `POST /api/v1/standard/runtime/element-revisions/resolve-exact`，接受同 Tenant 最多 200 个互异的确定 `element_revision_id`，按请求顺序返回 `revision_id + found + snapshot`；仅 `published|withdrawn` 历史发布修订可返回快照，跨 Tenant、不存在或未发布统一 `found=false`。Catalog 创建/补选候选时还必须校验结果中的 `element_id` 与请求一致、修订当时为 `published`，查询已审核历史映射时可读取后来 `withdrawn` 的确定快照。此精确读取契约与 Model 的按 `element_id + as_of` 生效解析并列，但语义不同，不作彼此的兼容兜底。
- `as_of` 只用于说明映射所指修订在该业务时点是否处于生效区间，不改变 StandardMapping 的修订选择。Model 审批按统一审批时点解析“当前生效修订”，Catalog 对既有映射则按 `element_revision_id` 精确读取，两种契约不可合并或互相兜底。
- 响应按 Meta 字段顺序返回物理名称、原生类型、通用类型、可空、主键、默认表达式、注释等物理事实，并可选组合数据元编码、名称、定义、数据类型、格式、值域、安全等级、生效区间及码项。未关联 Element 的物理字段仍必须返回，其标准解释为 `null`。
- `as_of` 只回溯 Standard 修订语义；Meta 当前没有物理 Schema 时态版本，因此不得把本视图表述为历史物理结构快照。

数据字典导出使用唯一同步路径：

```http
GET /api/v1/catalog/entries/{id}/data-dictionary/export?as_of={RFC3339}
```

- 导出与联邦查询使用完全相同的可见性、适用范围、依赖解析和 `as_of` 规则，不接受客户端提交查询结果，也不从 Catalog 已观察摘要生成字段。
- 导出在请求时重新组合一次联邦数据字典，以 UTF-8 JSON 附件返回 `catalog.data_dictionary/v1` 完整响应；文件中的 `generated_at` 是本次当前物理结构捕获时点，`as_of` 是 Standard 解释时点，两者不得混淆。
- 响应使用 `Content-Disposition: attachment`，并以强 ETag 提供响应字节的 SHA-256 摘要。下载文件一经产生即是不可变快照；Catalog 不保存导出文件、导出任务或第二份数据字典事实，不提供修改、覆盖或服务端重放接口。
- 同步导出只面向单个 DataItem 的有界字段集合。未来若出现批量发布、长期托管、审批或外部分发需求，应另行定义 Asset 发布物及保留策略，不能把本同步下载接口扩展成隐式发布流程。
- Meta 或 Standard 不可达时返回 `503 catalog_data_dictionary_dependency_unavailable`，仅影响本次字典查询，不影响 Catalog 详情、Alive 或 Ready。条目来源不适用时返回 `409 catalog_data_dictionary_not_applicable`，不返回空数据伪装成功。

办理人候选的接收方名称只按本次可读分页中已经绑定的 `recipient_type + recipient_id` 向 System 精确批量解析，返回 `recipient_name`、可选 `recipient_code` 与 `recipient_label_status=resolved|not_found`。它们是当前显示观察，不保存到决定、不返回成员／角色、不授予组织或数据访问权。解析后再以新只读快照核验同一分页的可见性、权限期限与决定依据；分页变化返回冲突供用户重新读取，不把旧名称贴到新决定上。System 无法解析时返回依赖不可用；明确不存在的主体只显示 `not_found`，不得用 ID 或旧快照伪造名称。

## 六、Catalog API 契约

BasePath 固定为 `/api/v1/catalog`。第一阶段公开单一路由集合：

| Method | Path | 语义 |
| --- | --- | --- |
| GET | `/entries` | 权限感知的分页搜索与分面筛选 |
| GET | `/entries/facets` | 返回当前目录视图可见条目中出现的 Domain、Department 和 Engine Instance 候选引用 |
| GET | `/domains` | 动态返回当前 Tenant 的 Standard Domain 最小概况树，供 `catalog.entry.read` 的 `/entries` 完整业务域候选与上下文说明使用；不是独立前端页面 |
| POST | `/entries/resolve-sources` | 把专业关系节点的精确来源身份批量解析为当前可见 CatalogEntry，不复制 owner 关系 |
| POST | `/entries/batch_governance` | 对显式选择的 CatalogEntry 原子批量分配主业务域或责任部门 |
| GET | `/reference-candidates` | 按名称分页查询当前可建立语义或责任关联的 owner 候选 |
| GET | `/entries/:id/sharing_recipient_candidates` | 当前业务负责人按本条目资格选择同 Tenant 的有效账号或项目组最小显示摘要 |
| POST | `/entries/:id/sharing_decisions` | 显式确认普通只读共享，保存不可变业务决定；不授予源访问 |
| GET | `/entries/:id/sharing_decisions` | 原确认人分页读取本人记录；当前业务负责人复核本条目全部历史 |
| GET | `/entries/:id/sharing_decisions/:decision_id` | 在上述复核资格内读取原决定，不重新确认 |
| GET | `/entries/:id/sharing_decisions/:decision_id/fulfillments` | 动态只读查询原办理及签发历史，不表达当前访问 Allow |
| GET | `/entries/:id/sharing_decision_candidates` | 有源办理资格的当前 User 读取仍有效的决定摘要，精确解析其接收方名称 |
| POST | `/entries/:id/sharing_fulfillments` | 根据持久决定和批准要求版本正式准备并触发 System 首次受理；不等于 Grant 已生效 |
| GET | `/entries/:id/sharing_fulfillments` | 按本人当前原引擎管理范围过滤、分页找回本人原办理请求；不提交或核清 |
| GET | `/entries/:id/sharing_fulfillments/:request_id` | 按持久完整绑定只读查询本人原受理／关闭结果；不重试、延期或授予源访问 |
| GET | `/entries/:id` | 读取聚合详情、来源、语义和责任 |
| GET | `/entries/:id/data-dictionary` | 组合 Meta 当前物理字段、Catalog 已审核 StandardMapping 与其冻结的 Standard 修订 |
| GET | `/entries/:id/data-dictionary/export` | 重新组合一次联邦数据字典并下载不可变 JSON 快照，不在服务端留存副本 |
| PUT | `/entries/:id` | 使用聚合根 `version` 原子更新 `discovered|curated` 阶段的编目、语义、责任与可见性；`curated → discovered` 只接受完整撤销编目形状，不承担认证或弃用转换 |
| PUT | `/entries/:id/governance` | 使用聚合根 `version` 原子执行认证、撤销认证、弃用、撤销弃用或弃用信息维护；只更新治理状态、推荐继任项和领域审计，不替换编目事实 |
| PUT | `/entries/:id/responsibilities` | 使用聚合根 `version` 与原因，仅对已弃用条目完整替换责任关系；保持弃用状态和其他事实冻结 |
| GET/POST | `/standard-mappings` | 分页读取或创建字段/组件标准映射候选；创建必须携带确定数据元修订和来源证据 |
| GET | `/standard-mappings/revision-options` | Catalog 使用运行身份动态返回所选数据元可用的已发布修订，供候选映射下拉选择；不复制 Standard 修订事实 |
| GET/PUT/DELETE | `/standard-mappings/:id` | 读取、完整更新或删除仍为 `proposed` 的映射，写操作使用映射自身 `version` |
| POST | `/standard-mappings/:id/approve` | 审核通过候选；同一事务撤回该组件旧 approved 映射并推进版本 |
| POST | `/standard-mappings/:id/reject` | 驳回候选并记录审核意见 |
| POST | `/standard-mappings/:id/withdraw` | 撤回当前 approved 映射并记录原因 |
| POST | `/entries/:id/rebind-source` | 显式把新 DataItem 来源重绑到既有条目 |
| GET | `/entries/:id/history` | 读取该条目的治理和重绑审计 |
| GET | `/governance/tasks` | 分页读取责任失效治理队列；默认只返回 open 任务 |
| GET | `/governance/coverage` | 动态聚合资源盘点范围内 Catalog 自有治理覆盖率 |
| GET | `/me/entries` | 按当前 User 的责任、收藏或关注关系分页读取“我的目录” |
| GET | `/me/entries/:id/marks` | 读取当前 User 对条目的收藏与关注状态 |
| PUT | `/me/entries/:id/marks` | 原子替换当前 User 对条目的收藏与关注状态 |
| GET | `/me/project-groups` | 动态返回当前 User 可读取或维护目录集合的 Project Group 显示摘要与成员角色 |
| GET | `/collections` | 分页读取当前 User 可参与的 Project Group 目录集合 |
| POST | `/collections` | 在当前有效 Project Group Scope 创建目录集合 |
| GET | `/collections/:id` | 读取集合及当前仍可见的目录条目 |
| PUT | `/collections/:id` | 使用集合 `version` 原子更新名称、说明和完整条目集合 |
| DELETE | `/collections/:id` | 使用集合 `version` 删除集合聚合 |
| POST | `/runtime/references/resolve` | Asset 按 CatalogEntry UUID 精确批量校验可组合与可发布状态 |

不存在用户手工创建或删除 DataItem CatalogEntry 的 API。创建只来自变化源，删除以来源 `missing`、治理 `deprecated` 或条目 `merged` 表达。

`PUT /entries/:id` 必须携带完整可编辑编目聚合和正整数 `version`，不接受 `recommended_successor_entry_id`。治理子资源 `PUT /entries/:id/governance` 中的推荐继任字段使用规范 UUID 或 `null`。成功写入返回新完整资源并递增版本；版本冲突返回 `409` 和 `catalog_entry_version_conflict`，不能自动重试或覆盖。推荐继任目标不满足同 Tenant、状态或来源约束时返回 `409` 和 `catalog_recommended_successor_invalid`。

`POST /entries/batch_governance` 是资源盘点中的显式成员批量命令，只允许同时具有 `catalog.inventory.read` 与 `catalog.entry.update` 的治理人员调用。请求固定包含 1 到 200 个互不重复的 `{id, version}`、单一 `operation=assign_primary_domain|assign_accountable_department` 和 owner 稳定 `reference_id`；不接受筛选条件、查询结果全选或手工输入裸 ID。Catalog 在写事务前只向对应 owner 精确校验一次目标可引用性，在事务中按 CatalogEntry UUID 稳定排序加锁并校验全部成员，再只替换每个条目的主业务域或责任部门这一项关系，保留其他语义与责任事实。任一条目不存在、跨 Tenant、非 active、版本冲突、目标不可引用或不适用时整批回滚；成功后每个条目版本递增并按原请求顺序返回 `{id, version}`。

资源盘点中的用户操作称“批量分配”，弹窗明确说明只支持主业务域或责任部门的单项分配。该命令不完成业务编目、不推进认证或弃用状态，也不自动生成责任治理任务；不得用泛化的“批量治理”文案暗示上述能力。

Model `business_entity|logical_model` 与 Standard `metric` 的主业务域由专业 owner 维护，Catalog 批量命令不得覆盖；包含任一此类条目时整批返回 `409 catalog_batch_governance_unsupported_entry`。每个成功条目必须写入独立审计记录并共享同一个 `batch_id`，同时投递搜索投影任务。显式成员和逐条版本共同构成并发快照，因此本命令不创建 Tenant 级集合 `revision`；前端遇到冲突必须保留选择和输入供用户刷新后重新确认，不能自动覆盖。

列表使用标准 `{data,total,page,page_size,total_pages}` 响应；`view=governance|inventory` 是稳定目录视图，省略时唯一表示 `governance`。`search`、`entry_type`、`source_status`、`governance_status`、`visibility`、`primary_domain_id`、`accountable_department_id`、`source_engine_id`、`coverage_dimension`、`coverage_state` 等过滤参数必须在 Swagger 中逐项声明，排序字段使用白名单。显式请求 `view=inventory` 但缺少 `catalog.inventory.read` 时返回 `403`，不静默降级到治理目录；治理缺口参数组合不满足 5.12 节约束时返回 `400`。

`GET /entries/facets` 接受同样的 `view`，以及可选 `primary_domain_id`、`accountable_department_id`、`entry_type` 上下文参数，并与 `/entries` 共用 Tenant、目录可见性和盘点权限过滤。响应是即时聚合的导航读模型，不是目录树事实：主业务域统计始终覆盖当前视图；责任部门统计受已选业务域约束；资源类型统计受已选业务域和责任部门约束；来源引擎统计受三项选择共同约束。Catalog 从权威库计算当前可见结果中实际出现的稳定 ID 及数量，再使用 `addp-catalog` Tenant Service Token 向 Standard / System 精确批量解析显示名、编码、类型和状态。它不返回 owner 未在当前可见 CatalogEntry 中被引用的对象，不授予额外 owner 管理权限，也不持久化 owner 完整列表。任一 owner 解析失败时，该分面返回 `unavailable` 状态，其他分面仍正常返回；不把动态解析变成 Catalog 启动或 Ready 依赖。

主业务域分面通过 Standard 精确解析取得 `domain_path`，只用作当前响应的层级展示，不落库。父域没有可见条目时不为补树而增加可选分面，子域仍保留完整路径。业务域候选、导航与归属编辑统一复用共享 `BusinessDomainSelect`，每层缩进 16px，搜索或缺少父选项时显示完整路径，选中框显示名称。

前端以可键盘操作的名称与数量选项呈现 Domain、Department 和 Entry Type 导航；Domain 或 Department 数量过多时在各自区域内部滚动，不把全量 DataItem 改造成节点树。Engine Instance 继续使用可搜索选择器。所有稳定 ID 只用于提交和恢复 URL；裸 ID 输入框与列表中的裸 Engine ID 列都不是正式交互路径。

“待归类”只在资源盘点视图的 Domain 导航中作为治理动作出现，并复用 5.12 节 `primary_domain=missing` 的动态缺口口径，不伪造计数、不创建特殊 Domain，也不增加另一条列表 API。普通 Domain、Department 或 Entry Type 导航选择必须清除既有治理缺口状态，保证同一 URL 只有一种列表语义。

责任部门导航在资源盘点视图提供“待分配部门”虚拟治理入口，唯一映射到 `coverage_dimension=accountable_department&coverage_state=missing`。它不是 System Department，不进入 Department 分面候选，也不使用复合责任完整度代替部门缺失。进入时保留已选 primary Domain，清除名称搜索、责任部门和下游 Entry Type，使治理人员可以处置某业务域内尚未分配组织责任的条目；缺口视图继续沿用 4.3 节的导航隐藏与退出规则。

`GET /reference-candidates` 是 Catalog 编目交互的唯一跨 owner 候选入口，使用 `catalog.entry.update` Permission。请求固定包含 `reference_type=domain|glossary|element|department|user`，可选 `search`，并使用 `page`、`page_size` 分页；`page_size` 最大 50。响应使用标准分页结构，候选 `id` 使用字符串，显示字段包含 `name`、可选 `code` 和 owner 当前 `status`；业务域候选额外返回 `domain_path`（从根到当前域的名称数组）。该接口只返回当前 Tenant 中允许建立新关联的对象，不返回完整专业 DTO。

候选事实仍由 owner 动态提供：Catalog 使用 `addp-catalog` Tenant Service Token 分别调用 Standard `GET /api/v1/standard/references/candidates` 和 System `GET /api/v1/system/runtime/catalog-references/candidates`。两个 owner 路由均按名称或编码搜索、稳定排序和分页；业务域还支持名称路径搜索，在父域优先的层级顺序上过滤和分页，分页边界不截断 `domain_path`，只允许 `addp-catalog`，并复用建立关联时已经要求的 owner read Permission。Catalog 不保存候选列表、不建立 owner 全表投影，也不把候选响应写入搜索索引；owner 不可达只使当前候选请求返回 `503 catalog_reference_validation_unavailable`，不影响 Catalog 启动、Ready、列表和已保存关联展示。

推荐继任项与治理任务条目筛选属于 Catalog 自有对象选择，复用 `/entries` 的权限感知名称搜索，不另建候选事实源。编辑器加载既有关联时可以使用聚合中已保存的 `observed_snapshot` 作为“最近确认的显示摘要”，但不得把裸 ID 当作名称回退，也不得在动态候选失败时恢复手工 ID 输入。技术来源详情中的 fingerprint、Meta Item ID 等只可出现在明确的技术溯源区域，不能成为业务编目主交互。

所有用户可见错误使用 Catalog i18n；Swagger 使用中文在前、英文在后的双语注解，并为每个公开 Operation 声明 `x-addp-auth-mode` 和精确 Permission。

`GET /governance/tasks` 第一阶段只接受 `status=open|resolved`、可选 `entry_id`、`page` 和 `page_size`，同时使用 `catalog.entry.read` 与 `catalog.entry.update` Permission。任务结果、分页计数和精确 `entry_id` 筛选必须复用条目详情的当前可见性，不能列出调用者无法打开的条目或泄露其任务数量。具有显式 `catalog.inventory.read` 的租户治理人员可发现本租户责任部门已失效的条目及任务，不依赖原部门成员关系；普通部门治理人员仍只看到当前可见条目的任务。返回任务、CatalogEntry 当前显示名和版本，治理人员从任务进入现有条目编目页修复责任；任务列表不新增责任写入或任务关单权限。前端按 CatalogEntry 名称远程搜索并提交 `entry_id`，有盘点权限时使用盘点视图，否则使用已编目资源视图，不提供 UUID 手工输入。

责任修复复用完整责任聚合及版本校验，不自动赋予修复者业务责任、源数据读取或共享批准权。已认证条目须先按 4.1 节撤销认证后修复；已弃用条目通过责任子资源移交，保持弃用及其他业务事实冻结，不必先撤销弃用。租户治理账号必须通过 System 的有效角色分配显式配置；已初始化 Tenant 的最后一个有效 `tenant.administrator` 受到既有 IAM 保护，该角色明确包含读取、盘点、维护和认证 Permission，因此可复用为责任失效后的修复通道。Catalog 不根据角色名放行、不新增自动兜底赋权。

Console 和 Catalog 的菜单、页面均称“责任治理队列”：只有既有关联的 Department 或 User 失效所派生的 `responsibility_transfer` 任务进入此页。未编目、未分配主域或责任等目录治理缺口仍通过治理覆盖率下钻到资源盘点，不自动生成任务；不得将此页标成泛化的“治理待办”。

`GET /me/entries` 必须显式提交 `relation=responsible|favorite|following`，只接受 `page` 和 `page_size`，使用标准目录分页结构并再次应用当前调用者可见性。个人 marks 读写使用 `catalog.entry.read`，目标条目不可见时统一返回 `404`。

集合列表只返回当前 AuthContext 有效 Project Group membership 覆盖的集合；集合更新正文固定携带 `version`、`name`、`description` 和完整 `entry_ids`。创建正文携带当前 membership 中的 `project_group_id` 及同样的业务字段；服务端必须执行精确 Scope Permission、成员资格、条目同 Tenant 与可见性校验。集合创建、更新和删除必须在同一个 Project Group 上同时满足 `catalog.collection.read` 与 `catalog.collection.update`，不能把不同 Scope 上分别命中的 Permission 拼接成写权限。版本冲突返回 `409`，删除正文只携带 `version`，不存在不带版本的旁路写法。

`GET /me/project-groups` 以 AuthContext 中的有效 Project Group membership 和 Catalog Collection Scope Permission 作为唯一 ID 集，再由 Catalog 使用 `addp-catalog` Tenant Service Token 调用 System `POST /api/v1/system/runtime/catalog-references/resolve` 动态解析名称、编码和状态。响应只包含当前 User 可访问的项目组、成员角色及 `can_read`、`can_update`，不得枚举 Tenant 全部 Project Group。Project Group 名称不写入 AuthContext、Access Token、Catalog 表或搜索索引；System 不可达时该请求返回 `503 catalog_reference_validation_unavailable`，Catalog Ready、集合权威事实和其他 API 不受影响。前端不得把稳定 ID 作为名称回退。

### 6.1 Asset 精确引用解析

`POST /runtime/references/resolve` 只接受 1 到 200 个规范 CatalogEntry UUID，不接受 Tenant ID，并必须同时校验 `addp-asset` Service Client 与 `catalog.reference.read` Permission。响应按请求顺序返回：

- `found`：该 UUID 属于当前 Tenant；跨 Tenant 与不存在统一返回 `false`；
- `selectable`：条目为 `active`、治理状态非 `deprecated`，且当前来源为 `active`；
- `publishable`：在 `selectable` 基础上，治理状态必须为 `curated` 或 `certified`；
- 条目状态、治理状态、来源状态、展示名和当前聚合版本。

Asset 创建或编辑组件时必须校验全部引用 `selectable=true`；单条或批量发布前必须在同一请求中重新校验全部引用 `publishable=true`。Asset 不得根据自己保存的名称或历史快照猜测有效性，也不得在 Catalog 不可达时绕过校验。该运行时调用失败只影响当前创建、编辑或发布操作，不进入 Asset 启动和 Ready 条件。

## 七、显式来源重绑状态机

`POST /entries/:id/rebind-source` 第一阶段只接受：

- 目标为一个 `active` CatalogEntry，且其当前绑定已经 `missing`；
- 新 fingerprint 当前绑定到另一个 `active + discovered + inventory` 临时 CatalogEntry；
- 临时条目没有人工业务说明、语义、责任或认证历史；
- 请求同时携带目标条目和临时条目的当前 `version`、重绑原因和人工证据。

事务内固定执行：

1. 锁定两个 CatalogEntry 和相关 SourceBinding；
2. 重新验证版本与前置条件；
3. 把新 active SourceBinding 转移到原 CatalogEntry；
4. 保留原 missing binding 历史并建立替换关系；
5. 把临时 CatalogEntry 标记为 `merged` 并指向原条目；
6. 递增两个聚合版本，写入不可变审计和搜索投影任务。

新来源已经绑定到 `curated`、`certified`、`deprecated` 或具有人工作业的条目时返回 `409`，第一阶段不自动合并两个业务身份。不得使用名称、字段、路径或结构相似度自动执行重绑。

## 八、权限与审计

Catalog 是以下 Permission 的 owner，正式 Key 在 `catalog/authorization/permissions.yaml` 中唯一声明：

- `catalog.entry.read`：读取企业目录的基础权限，所有列表和详情请求均必需；
- `catalog.inventory.read`：在 `catalog.entry.read` 基础上额外查看自动盘点和 `inventory` 条目；不能单独授权读取接口；
- `catalog.entry.update`：编目和普通关系维护；
- `catalog.standard_mapping.review`：审核通过、驳回或撤回字段/组件标准映射；创建、编辑、删除候选仍使用 `catalog.entry.update`。该权限不代替 `catalog.entry.certify`，后者只认证整个目录条目。第一阶段不强制提交者与审核者为不同自然人，但每次操作均独立审计；Copilot Service Principal 不持有审核权限；
- `catalog.entry.certify`：推进到 `certified`；
- `catalog.entry.deprecate`：弃用、撤销弃用及维护推荐继任项，均同时要求读取与编目维护权限；
- `catalog.source.rebind`：显式来源重绑；
- `catalog.audit.read`：读取目录审计。
- `catalog.reference.read`：由 `addp-asset` 精确批量解析可组合和可发布状态；不授权用户列表或盘点视图。
- `catalog.collection.read`：读取当前有效 Project Group membership 覆盖的目录集合；允许 Tenant / Project Group Scope；
- `catalog.collection.update`：创建、更新和删除当前有效 Project Group membership 覆盖的目录集合；允许 Tenant / Project Group Scope。

Catalog 必须从 AuthContext 读取当前 Tenant、User、Department / Project Group membership 和 Permission，不接受调用方提交 Tenant、User、Role 或成员关系。目录写入、状态变更、责任移交、语义关联和来源重绑必须写入 Catalog 自身不可变领域审计，并通过公共审计中间件把平台审计摘要发送到 System；System 不保存 Catalog 业务详情副本。

## 九、搜索投影

PostgreSQL `catalog` Schema 是权威事实源。Meilisearch 索引固定使用 Catalog 专属文档语义和名称 `catalog_entries`，至少包含：

- CatalogEntry ID、业务名称与说明；
- 来源投影名称、类型、引擎和路径摘要；
- primary / secondary Domain、Glossary Term 和 Element 摘要；
- 责任部门与责任人摘要；
- 来源、治理、条目和可见性状态；
- Catalog 自己拥有的可见性过滤 token。

索引更新通过 Catalog 数据库内的投影任务异步完成。数据库事务成功而索引失败时 API 事实仍然有效，投影任务后台重试；管理员可以从 PostgreSQL 全量重建索引。不得把 Meilisearch 命中作为授权结论，返回前仍需应用 Catalog 可见性规则。

Meta 技术树索引、Manager 内容索引、Catalog 企业元数据索引和 Asset 已发布资产索引必须物理或逻辑隔离，不能继续复用 `asset` 文档模型。

Manager 是技术内容全文/向量检索投影的唯一 owner。Meta 在完成 DataItem 扫描后，只通过 Manager 的 Tenant Runtime API 提交以 fingerprint 为 `document_id` 的内容文档；Manager 负责索引名称、字段映射、写入和删除。该调用是软依赖：Manager 不可达不得使 Meta 进程或扫描事实写入失败，但本次扫描必须记录 `index_failed`，后续重扫可按 DataItem 事实完整重建投影。Meta 不得持有 Meilisearch Client、索引名称或直接读写 Manager 索引。

运行时写入契约固定为：

- `PUT /api/v1/manager/runtime/content-documents/{document_id}`：按当前 Tenant 幂等覆盖一个内容文档；
- `DELETE /api/v1/manager/runtime/content-documents?engine_id={engine_id}`：删除当前 Tenant 指定 Engine 的内容投影；可选 `data_item_type`、`schema`、`bucket`、`path_prefix` 只用于 Meta 扫描范围内的精确收敛；
- 两个路由只接受 `addp-meta` Tenant Service Principal，并校验 `manager.content_index.update`；
- Tenant 只来自 canonical AuthContext，请求正文不得携带或覆盖 `tenant_id`；
- `document_id` 必须等于 DataItem fingerprint，删除和覆盖均不得跨 Tenant。

Manager 内容索引固定使用独立配置 `MEILISEARCH_MANAGER_CONTENT_INDEX`，不得继续读取 `MEILISEARCH_ASSET_INDEX`。内容检索返回 `document_id`、`data_item_type` 和 Locator 等技术资源字段，不暴露 `asset_id` / `asset_type` 旧词族。

## 十、运行、配置与基础设施

- 模块目录：`catalog/`；数据库 Schema：`catalog`；
- Backend 开发与容器端口：`8192`；Frontend 开发端口：`5189`；Frontend 容器端口：`8120`；
- BasePath：`/api/v1/catalog`；Console 模块前缀：`/catalog`；
- 必需 Infra：Catalog PostgreSQL Schema；第一阶段企业搜索启用后 Meilisearch 也属于本模块必需 Infra；
- System URL 和 `CATALOG_SERVICE_CLIENT_SECRET` 来自根环境配置；Meta / Standard URL 是运行时软依赖配置，不参与 Ready；
- 进程使用 `/health/live` 和 `/health/ready`，只有自身 Infra 正常且 System 注册成功后 Ready；
- Meta、Standard、Manager、Asset 不可达不得出现在 Ready 必需检查中。

`addp-catalog` 必须作为内置 Tenant Runtime Service Principal / OAuth Client 由 System 唯一供应。Catalog 不接受内部 API Key 或 Tenant Header。

## 十一、数据库迁移与旧路线删除

新增 Schema 和表必须通过 Catalog 自身迁移创建；`scripts/infra/init-postgresql.sql` 只登记 Schema 与注释。跨模块引用均为带 Tenant 的软引用，不建立跨 Schema FK。

阶段性迁移顺序固定为：

1. Meta 建立 DataItem 变化日志并回填现有 DataItem；
2. Catalog 从变化历史自动建档并验证一源一条目；
3. Manager 增加 Catalog 摘要与跳转；
4. Asset 切换为 `AssetComponent.catalog_entry_id` 多对象组合；
5. 删除 Asset 跨 Meta / Standard / Service / Develop 自动发现；
6. 删除 Meta `assets/discoverable`、`AssetRecord` 词族和企业目录含义的旧索引；
7. 删除 `{source_module, source_reference}` 资产来源主路径和所有 fallback。

Asset 的正式组合模型固定为 `asset.asset_components`：`catalog_entry_id` 使用 UUID 软引用，`role` 只允许 `primary` / `supporting`，`sort_order` 表达稳定展示顺序。每个 Asset 至少一个组件且恰好一个 `primary`，同一 CatalogEntry 在同一 Asset 内不得重复。Asset 创建和编辑均使用完整可编辑聚合原子写入，不提供独立组件增删改路由。

迁移时保留现有 Asset 主记录、申请、授权、评价和资产目录归属，但不根据旧 `{source_module, source_reference}` 自动猜测 CatalogEntry。所有旧 `published` 资产在删除旧来源字段前原子转为 `offline`；草稿和下架记录保留，待管理员选择 CatalogEntry 并通过发布校验后再上架。不为旧字段建立读取 fallback、暗中回填或双轨 API。

每一步都必须在同一变更中同步 API、Swagger、前后端调用方、根 Makefile、CI 注册和文档，不能保留双轨等待“以后迁移”。

## 十二、最小验收

实现前必须登记 Catalog 到模块自动发现、Gateway、Console、开发脚本、Docker Compose、Swagger、授权 Manifest、根 Makefile 和 GitHub Actions。最小验收至少覆盖：

- 相同变化重复消费只产生一个 CatalogEntry；
- checkpoint 与业务写入原子提交，失败不越过变化；
- Catalog 在 Meta / Standard 不可达时仍可启动并 Ready，依赖操作明确失败并可恢复；
- `discovered` 默认不可被普通目录读者发现；
- 版本冲突无任何业务或投影副作用；
- 重绑保留历史、临时条目变为 `merged`，不产生两个规范身份；
- 跨 Tenant fingerprint、Standard ID、Department ID 和 User ID 均不可探测；
- 搜索索引可从 PostgreSQL 重建，索引不可用不改变权威事实；
- Swagger 路由覆盖和授权覆盖报告通过；
- 旧发现、旧索引和旧来源字段删除后不存在兼容路由、字段或 fallback query。
- T4 `enterprise-catalog-publishing` 在真实 System、Gateway 和各 owner 中重复执行 Meta 扫描，验证 fingerprint / CatalogEntry 身份幂等、资源盘点与治理目录视图、治理覆盖率、精确来源身份解析、CatalogEntry 自动建档与编目、已弃用责任子资源更新及撤销弃用、AssetComponent 组合与发布、Portal 消费是唯一路线；同一专用 User 还必须通过真实浏览器验收 Console 覆盖率、目录详情和人类可读筛选器，最终通过正式 API 完成临时 Asset 和资产目录零残留清理，并重新读取核对永久 fixture 的完整编目聚合恢复。具体生命周期用例边界以测试与验收规范 5.2 节为准。

Asset 的删除生命周期固定为：`draft` 或 `offline` 可删除，`published` 必须先下架；不允许跳过下架直接删除已发布资产。
