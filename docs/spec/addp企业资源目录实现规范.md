# ADDP 企业资源目录实现规范

版本：v1.13-draft
更新日期：2026-08-28

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
| CatalogComponent → Element | 每个组件最多一个当前 Element |

`curated` 及以上 CatalogEntry 必须有一个有效 primary Domain。Meta DataItem 的 primary Domain 是 Catalog 关联；Model Entity / LogicalTable 和 Standard Metric 的 primary Domain 是 owner 当前 `domain_id`。Catalog 只对自身持有的 Domain、Glossary 和 Element 关联通过 Standard 批量解析接口验证同一 Tenant、对象存在且生命周期允许引用；Standard 名称可进入搜索投影，但不成为 Catalog 权威字段。

Model 自身保存的 Entity / LogicalTable `domain_id`、属性或字段 `element_id`、事实表 `metric_id` 及建模关系是参与审批、设计和物化的专业内生关系，仍由 Model 权威维护，不复制为 Catalog 人工语义关联。Catalog 动态读取并以“Model 声明的专业关系”展示；Model 已声明主业务域时，它构成该 Model 来源条目的有效 primary Domain，Catalog 不接受另一条冲突的人工 primary Domain。修改这类关系必须进入 Model 唯一写路径。

Standard Metric 的定义、公式、类型、专业状态、`domain_id`、分类、计量单位、数据元映射和指标依赖全部由 Standard 权威维护。Catalog 不复制这些专业事实，也不允许为 Metric 保存冲突的人工 primary Domain 或组件 Element；Catalog 只维护辅助业务域、企业术语、责任、可见性和治理状态。修改指标专业事实必须进入 Standard 唯一写路径。

### 3.2 责任关系

`catalog.responsibilities` 只引用 System 主体和组织身份，不复制成员关系：

| 角色 | 主体 | 基数与要求 |
| --- | --- | --- |
| `accountable_department` | Department | 每个条目最多一个；`curated` 及以上必填 |
| `business_owner` | User | 每个条目最多一个；`curated` 及以上必填 |
| `data_steward` | User | 零到多个；`curated` 及以上至少一个 |
| `technical_owner` | User | 零到多个，可选 |

Project Group 不作为长期责任主体。它只在后续协作集合、草稿、评审和治理任务中使用。User 离职或 Department 失效不会自动删除 CatalogEntry；Catalog 将关系标记为待移交并进入治理队列。

System 的 Department / Project Group 管理契约以 `system/docs/IAM数据模型与迁移规范.md` 为准。Catalog 不创建、修改或关闭组织对象，只通过 System 精确解析接口验证责任引用，并在已引用 Department 或 User 变为不可引用时建立本地治理待办。Project Group 只作为后续 Catalog 协作集合的主体，不进入长期责任关系。

责任失效对账使用 System 同一个精确批量解析契约，不新增组织变化副本或第二条事实同步路线。Catalog 按 Tenant 周期性读取当前 `active` / `needs_transfer` 责任，最多 200 个一批进行解析，并在 Catalog 本地事务中执行：

1. `active` 引用变为不存在或不可引用时，将关系标记为 `needs_transfer`，打开一条 `responsibility_transfer` 治理任务，递增 CatalogEntry 聚合版本并写入领域审计和搜索投影任务；
2. 同一条责任重复对账保持同一个 open 任务，不重复递增聚合版本或制造重复审计；
3. 同一 System 身份恢复为可引用时，关系恢复为 `active`，治理任务以 `reference_restored` 自动解决，并递增聚合版本；
4. 治理人员通过唯一的 `PUT /entries/:id` 完整替换责任聚合后，被删除或替换的失效责任任务以 `responsibility_replaced` 在同一事务自动解决；不提供独立“忽略失效责任”或手工关闭任务 API。

`catalog.governance_tasks` 是 Catalog 内派生的治理工作事实，不是责任关系副本。第一阶段字段固定表达 CatalogEntry、任务类型、责任角色、主体类型与 ID、失效原因、已观察主体摘要、`open` / `resolved` 状态、打开/解决时间和解决方式；数据库必须保证同一 Tenant、CatalogEntry、责任角色和主体最多一条 open `responsibility_transfer` 任务。任务不跨 Schema 建外键，System 不保存其反向投影。

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
discovered ⇄ curated ⇄ certified → deprecated
                     ↘ deprecated
```

- `discovered → curated`：业务名称、说明、有效 primary Domain、责任部门、业务责任人和至少一个数据管理员完整；Model 来源的 primary Domain 取 owner 当前声明，不要求也不允许 Catalog primary 副本；
- `curated → discovered`：唯一表示“撤销编目”。它不是普通字段编辑或任意状态回退，而是把误编目或不再具备完整业务治理事实的条目原子恢复为自动发现状态；请求必须同时清空 Catalog 自有业务名称、业务说明、Domain、Glossary、责任、组件 Element 和推荐继任关系，并把可见性恢复为 `inventory`。来源绑定、CatalogEntry 稳定身份、专业 owner 事实、版本和审计历史必须保留；服务端拒绝任何保留人工编目字段的部分撤销；
- `curated → certified`：需要独立认证权限和认证审计；认证只确认当前 CatalogEntry 聚合版本，不允许在同一请求中改变业务名称、说明、语义关联、责任、组件数据元关联或可见性；
- `certified → curated`：唯一表示“撤销认证”。必须具有认证权限并填写原因，只改变治理状态并完整保留当前编目事实；撤销后使用普通编目更新完成修订，再由同一路径重新认证；
- `curated|certified → deprecated`：必须具有弃用权限并填写原因，只改变治理状态和可选推荐继任项，不允许夹带业务编目修改；
- `deprecated → deprecated`：只允许具有弃用权限的用户填写原因并变更或清除推荐继任项；其他编目事实冻结；
- 弃用时可以指定一个推荐继任项；推荐继任项不是必填，没有替代资源时允许为空；
- `deprecated` 不允许恢复；除“撤销编目”和“撤销认证”外不允许任何回退。重新认证不是独立状态、实体或兼容 API，固定由“撤销认证 → 编辑编目 → 认证”构成同一 CatalogEntry 上的可审计闭环。

通用编目更新 `PUT /entries/:id` 只维护 `discovered|curated` 阶段的完整编目聚合和撤销编目，不接受进入、维持或退出 `certified|deprecated` 的请求。认证、撤销认证、弃用和弃用信息维护唯一使用 `PUT /entries/:id/governance`，请求携带当前聚合 `version`、目标 `governance_status`、按转换要求填写的 `reason` 和可选 `recommended_successor_entry_id`。服务端在同一事务中锁定 CatalogEntry、校验状态与版本、更新治理状态或推荐继任项、递增版本并分别写入 `catalog.entry.certified`、`catalog.entry.certification_withdrawn`、`catalog.entry.deprecated` 或 `catalog.entry.deprecation_updated` 审计；该路径不替换编目关联，也不依赖 Standard / System 当前可达。

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

Console 的固定页面标题使用“企业资源目录”，侧边栏入口使用“资源浏览”，两个视图标签分别使用“已治理资源”和“资源盘点”。不得同时使用“目录浏览”“治理目录”“企业目录导航”等相近名称制造另一套目录概念。`discovered` 条目主操作为“开始编目”；`curated` 条目主操作为“编辑编目”，认证和撤销编目放在明确的治理操作中；`certified` 条目只提供“撤销认证”和“弃用资源”，不显示通用编目编辑器；`deprecated` 只提供“维护弃用信息”，不得修改冻结的编目事实。

视图是同一组 CatalogEntry 的权限感知查询，不新增实体、复制条目或维护双轨索引。DataItem 全量自动建档且可在 `inventory` 查询；完成业务编目后，同一 CatalogEntry 自然进入 `governance` 视图。

目录浏览采用“主业务域 + 上下文分面 + 权威分页列表”，不建立持久化企业目录树。Standard Domain 是主业务分类，Accountable Department 是可交叉的组织责任分面，Entry Type 是资源形态分面；三者不能被固化为 Domain 拥有 Department、Department 拥有资源类型的父子事实。前端在同一 `/entries` 路由中按“业务域 → 责任部门 → 资源类型”逐步缩小当前查询，所有选择写入规范 URL，并继续由 `/entries` 返回同一批 CatalogEntry。

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

Model 在聚合根创建、版本推进或删除的同一数据库事务中，通过 PostgreSQL trigger 追加 owner-local、append-only `model.catalog_resource_changes`。EntityAttribute 写入必须推进 Entity 版本，LogicalField、TableRelation、FactMetricMapping 等写入必须推进 LogicalTable 版本，因此只监听两个聚合根即可完整观察专业定义变化，不增加 Service 手工双写。

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

响应 Schema 固定为 `standard.catalog_resource_changes/v1`，变化项固定 `source_type=metric`，`source_identity` 使用公开正整数 ID 的规范十进制字符串，`operation=upsert|missing`。首次迁移必须回填所有现存 Metric；变化历史不设保留窗口，Standard、Model 和 Meta 各自使用独立 Catalog checkpoint，任一来源不可达都不能阻塞其他来源或 Catalog Ready。

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

详情读取时按精确物理表引用动态组合 `configured`、最近 execution 状态、当前有效评分、open Issue 数量和 Quality 详情路径。Quality 不可达时 Catalog 返回 `unavailable`，不回退到旧评分；未配置返回 `not_configured`，不解释为高质量或失败。本阶段只在 Catalog 详情动态展示，不提供质量搜索过滤或排序，避免为了分页投影复制 Quality 事实。

### 5.9 Meta 数据血缘的用户上下文联邦视图

数据血缘节点、边、时态证据和当前投影继续只归 Meta。Catalog 不保存血缘副本，不新增 Catalog Backend 代理接口，也不使用 `addp-catalog` Service Access Token 代替用户查询血缘。

第一阶段仅对当前来源满足 `source_module=meta`、`source_type=data_item`、`source_status=active` 且已观察摘要具有规范正整数 `item_id` 的 active CatalogEntry 展示数据血缘。Catalog Frontend 使用当前 User Access Token 直接调用 Meta 唯一图接口：

```http
GET /api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item_id}&direction=both&depth=3&limit=100
```

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
| `primary_domain` | 全部 active 条目 | Catalog 自有 primary Domain 存在，或 Model / Standard 最近一次最小观察摘要具有 owner `domain_id` |
| `accountable_department` | 全部 active 条目 | 至少存在一个 active 责任部门 |
| `business_owner` | 全部 active 条目 | 至少存在一个 active 业务责任人 |
| `data_steward` | 全部 active 条目 | 至少存在一个 active 数据管理员 |
| `glossary` | 全部 active 条目 | 至少关联一个 Catalog 自有 Glossary Term |
| `component_element` | 至少有一个 active CatalogComponent 的条目 | 该条目的全部 active CatalogComponent 都有 Element 关联 |

责任覆盖率必须使用 `accountable_department`、`business_owner`、`data_steward` 三个原子维度，不保留同时要求三项完整的复合 `accountability` 维度。`curated` 状态仍由 4.1 节聚合写路径同时校验三项责任；治理状态表示整体准入结果，覆盖率维度则负责准确指出需要处置的具体缺口。`glossary` 是观察维度，不作为 `curated` 的必备状态条件；`component_element` 对没有 CatalogComponent 的专业条目标记为不适用，不能用全体条目作为分母制造虚假低覆盖率。覆盖率只说明企业目录治理完整度，不说明底层数据质量、内容授权、Owner 专业模型完整度或资产发布资格。

覆盖率页面必须能够沿同一权威口径下钻到待治理条目，但不得为此新增覆盖率明细表、任务实体或搜索投影字段。`GET /entries` 通过成对参数 `coverage_dimension=<固定维度>&coverage_state=missing` 返回该维度当前适用且未覆盖的 active CatalogEntry；这两个参数只允许与 `view=inventory` 同时出现，缺少任一参数、使用其他状态或在治理目录视图提交均返回 `400`。第一阶段只实现 `missing`，不预建未形成处置价值的 `covered`、`not_applicable` 等并行状态。

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
- Catalog 用自身权威的 `CatalogComponent -> Element` 关联把 Meta 字段连接到稳定 `element_id`，然后通过 Standard `POST /api/v1/standard/runtime/element-revisions/resolve` 在同一 `as_of` 批量解析精确数据元修订及其绑定的码值集修订。
- Standard 运行时请求固定包含 1 到 200 个不重复、规范十进制字符串形式的正整数 `element_ids` 和一个 `as_of`；响应按请求顺序返回 `found`、数据元稳定摘要、精确不可变修订，以及可选的精确码值集修订和码项。跨 Tenant、不存在、已删除或该时点无生效修订统一 `found=false`。该路由 `addp-catalog|addp-model` 和 `standard.element.read` 共同约束；Model 审批冻结也必须复用此唯一契约。
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

## 六、Catalog API 契约

BasePath 固定为 `/api/v1/catalog`。第一阶段公开单一路由集合：

| Method | Path | 语义 |
| --- | --- | --- |
| GET | `/entries` | 权限感知的分页搜索与分面筛选 |
| GET | `/entries/facets` | 返回当前目录视图可见条目中出现的 Domain、Department 和 Engine Instance 候选引用 |
| POST | `/entries/resolve-sources` | 把专业关系节点的精确来源身份批量解析为当前可见 CatalogEntry，不复制 owner 关系 |
| POST | `/entries/batch_governance` | 对显式选择的 CatalogEntry 原子批量分配主业务域或责任部门 |
| GET | `/reference-candidates` | 按名称分页查询当前可建立语义或责任关联的 owner 候选 |
| GET | `/entries/:id` | 读取聚合详情、来源、语义和责任 |
| GET | `/entries/:id/data-dictionary` | 组合 Meta 当前物理字段、Catalog 组件语义关联与 Standard 按时点修订 |
| GET | `/entries/:id/data-dictionary/export` | 重新组合一次联邦数据字典并下载不可变 JSON 快照，不在服务端留存副本 |
| PUT | `/entries/:id` | 使用聚合根 `version` 原子更新 `discovered|curated` 阶段的编目、语义、责任与可见性；`curated → discovered` 只接受完整撤销编目形状，不承担认证或弃用转换 |
| PUT | `/entries/:id/governance` | 使用聚合根 `version` 原子执行认证、撤销认证、弃用或弃用信息维护；只更新治理状态、推荐继任项和领域审计，不替换编目事实 |
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

`PUT /entries/:id` 必须携带完整可编辑聚合和正整数 `version`，其中 `recommended_successor_entry_id` 使用规范 UUID 或 `null`。成功返回新完整资源并递增版本；版本冲突返回 `409` 和 `catalog_entry_version_conflict`，不能自动重试或覆盖。推荐继任目标不满足同 Tenant、状态或来源约束时返回 `409` 和 `catalog_recommended_successor_invalid`。

`POST /entries/batch_governance` 是资源盘点中的显式成员批量命令，只允许同时具有 `catalog.inventory.read` 与 `catalog.entry.update` 的治理人员调用。请求固定包含 1 到 200 个互不重复的 `{id, version}`、单一 `operation=assign_primary_domain|assign_accountable_department` 和 owner 稳定 `reference_id`；不接受筛选条件、查询结果全选或手工输入裸 ID。Catalog 在写事务前只向对应 owner 精确校验一次目标可引用性，在事务中按 CatalogEntry UUID 稳定排序加锁并校验全部成员，再只替换每个条目的主业务域或责任部门这一项关系，保留其他语义与责任事实。任一条目不存在、跨 Tenant、非 active、版本冲突、目标不可引用或不适用时整批回滚；成功后每个条目版本递增并按原请求顺序返回 `{id, version}`。

Model `business_entity|logical_model` 与 Standard `metric` 的主业务域由专业 owner 维护，Catalog 批量命令不得覆盖；包含任一此类条目时整批返回 `409 catalog_batch_governance_unsupported_entry`。每个成功条目必须写入独立审计记录并共享同一个 `batch_id`，同时投递搜索投影任务。显式成员和逐条版本共同构成并发快照，因此本命令不创建 Tenant 级集合 `revision`；前端遇到冲突必须保留选择和输入供用户刷新后重新确认，不能自动覆盖。

列表使用标准 `{data,total,page,page_size,total_pages}` 响应；`view=governance|inventory` 是稳定目录视图，省略时唯一表示 `governance`。`search`、`entry_type`、`source_status`、`governance_status`、`visibility`、`primary_domain_id`、`accountable_department_id`、`source_engine_id`、`coverage_dimension`、`coverage_state` 等过滤参数必须在 Swagger 中逐项声明，排序字段使用白名单。显式请求 `view=inventory` 但缺少 `catalog.inventory.read` 时返回 `403`，不静默降级到治理目录；治理缺口参数组合不满足 5.12 节约束时返回 `400`。

`GET /entries/facets` 接受同样的 `view`，以及可选 `primary_domain_id`、`accountable_department_id`、`entry_type` 上下文参数，并与 `/entries` 共用 Tenant、目录可见性和盘点权限过滤。响应是即时聚合的导航读模型，不是目录树事实：主业务域统计始终覆盖当前视图；责任部门统计受已选业务域约束；资源类型统计受已选业务域和责任部门约束；来源引擎统计受三项选择共同约束。Catalog 从权威库计算当前可见结果中实际出现的稳定 ID 及数量，再使用 `addp-catalog` Tenant Service Token 向 Standard / System 精确批量解析显示名、编码、类型和状态。它不返回 owner 未在当前可见 CatalogEntry 中被引用的对象，不授予额外 owner 管理权限，也不持久化 owner 完整列表。任一 owner 解析失败时，该分面返回 `unavailable` 状态，其他分面仍正常返回；不把动态解析变成 Catalog 启动或 Ready 依赖。

前端以可键盘操作的名称与数量选项呈现 Domain、Department 和 Entry Type 导航；Domain 或 Department 数量过多时在各自区域内部滚动，不把全量 DataItem 改造成节点树。Engine Instance 继续使用可搜索选择器。所有稳定 ID 只用于提交和恢复 URL；裸 ID 输入框与列表中的裸 Engine ID 列都不是正式交互路径。

“待归类”只在资源盘点视图的 Domain 导航中作为治理动作出现，并复用 5.12 节 `primary_domain=missing` 的动态缺口口径，不伪造计数、不创建特殊 Domain，也不增加另一条列表 API。普通 Domain、Department 或 Entry Type 导航选择必须清除既有治理缺口状态，保证同一 URL 只有一种列表语义。

责任部门导航在资源盘点视图提供“待分配部门”虚拟治理入口，唯一映射到 `coverage_dimension=accountable_department&coverage_state=missing`。它不是 System Department，不进入 Department 分面候选，也不使用复合责任完整度代替部门缺失。进入时保留已选 primary Domain，清除名称搜索、责任部门和下游 Entry Type，使治理人员可以处置某业务域内尚未分配组织责任的条目；缺口视图继续沿用 4.3 节的导航隐藏与退出规则。

`GET /reference-candidates` 是 Catalog 编目交互的唯一跨 owner 候选入口，使用 `catalog.entry.update` Permission。请求固定包含 `reference_type=domain|glossary|element|department|user`，可选 `search`，并使用 `page`、`page_size` 分页；`page_size` 最大 50。响应使用标准分页结构，候选 `id` 使用字符串，显示字段只包含 `name`、可选 `code` 和 owner 当前 `status`。该接口只返回当前 Tenant 中允许建立新关联的对象，不返回完整专业 DTO。

候选事实仍由 owner 动态提供：Catalog 使用 `addp-catalog` Tenant Service Token 分别调用 Standard `GET /api/v1/standard/references/candidates` 和 System `GET /api/v1/system/runtime/catalog-references/candidates`。两个 owner 路由均按名称或编码搜索、稳定排序和分页，只允许 `addp-catalog`，并复用建立关联时已经要求的 owner read Permission。Catalog 不保存候选列表、不建立 owner 全表投影，也不把候选响应写入搜索索引；owner 不可达只使当前候选请求返回 `503 catalog_reference_validation_unavailable`，不影响 Catalog 启动、Ready、列表和已保存关联展示。

推荐继任项与治理任务条目筛选属于 Catalog 自有对象选择，复用 `/entries` 的权限感知名称搜索，不另建候选事实源。编辑器加载既有关联时可以使用聚合中已保存的 `observed_snapshot` 作为“最近确认的显示摘要”，但不得把裸 ID 当作名称回退，也不得在动态候选失败时恢复手工 ID 输入。技术来源详情中的 fingerprint、Meta Item ID 等只可出现在明确的技术溯源区域，不能成为业务编目主交互。

所有用户可见错误使用 Catalog i18n；Swagger 使用中文在前、英文在后的双语注解，并为每个公开 Operation 声明 `x-addp-auth-mode` 和精确 Permission。

`GET /governance/tasks` 第一阶段只接受 `status=open|resolved`、可选 `entry_id`、`page` 和 `page_size`，同时使用 `catalog.entry.read` 与 `catalog.entry.update` Permission。返回任务、CatalogEntry 当前显示名和版本，治理人员从任务进入现有条目编目页修复责任；任务列表不新增责任写入或任务关单权限。前端按 CatalogEntry 名称远程搜索并提交 `entry_id`，不提供 UUID 手工输入。

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
- `catalog.entry.certify`：推进到 `certified`；
- `catalog.entry.deprecate`：弃用目录条目；
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
- T4 `enterprise-catalog-publishing` 在真实 System、Gateway 和各 owner 中重复执行 Meta 扫描，验证 fingerprint / CatalogEntry 身份幂等、资源盘点与治理目录视图、治理覆盖率、精确来源身份解析、CatalogEntry 自动建档与编目、AssetComponent 组合与发布、Portal 消费是唯一路线；同一专用 User 还必须通过真实浏览器验收 Console 覆盖率、目录详情和人类可读筛选器，最终通过正式 API 完成临时 Asset 和资产目录零残留清理。

Asset 的删除生命周期固定为：`draft` 或 `offline` 可删除，`published` 必须先下架；不允许跳过下架直接删除已发布资产。
