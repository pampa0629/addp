# Model 模块 CLAUDE.md

本文件为 Claude Code 在 `model/` 目录下工作时提供指导。

## 模块概述

正式边界、聚合、生命周期和数据约束以 [Model 概念与数据约束规范](docs/model概念与数据约束规范.md) 为准。

**Model 模块** 是 ADDP 平台的数据建模和数仓设计服务，负责：

- 业务实体（Entity）设计与属性定义
- 逻辑表（LogicalTable）设计：支持实体表、事实表、维度表三种建模角色
- 维度建模：事实表与维度表的显式关联关系
- 概念实现映射：业务实体/属性/关系到逻辑表/字段/维度关联的显式追溯，不创建第二套逻辑表
- 公共/一致性维度与维度层级：由 Model 独占定义，层级成员关联本模型字段
- 指标实现：冻结 Standard 指标定义修订并维护粒度、来源、连接、过滤和可执行表达式
- `count_distinct` 可显式开启 `include_details`，汇总与明细共享构建路径并在同一修订冻结；明细按主体、bucket 和 member 去重，不补零，通过既有 plan 接口的 `result_kind=details` 选择。模型双数据库金样同时验证去重与期间对账。
- 指标主体当前显示名称通过可选 `subject_label` 显式引用同一主体维度的 string 字段，作为聚合后的名称输出；继续由 Model 冻结中立计划、Service 执行、Workbench 展示，不引入数据库方言或前端目录补全。
- ER 图 Mermaid 导入导出
- 建表语句预览（创建目标物理表前的 SQL 预览）
- 数仓分层（DW Layer）定义

Model Entity / LogicalTable 是企业 Catalog 的专业资源来源。Model 权威拥有完整对象、属性、字段、Domain / ElementRevision / MetricDefinitionRevision 引用、维度层级、指标实现和建模关系；Catalog 只通过 owner-local 变化源自动建立企业身份，并动态读取当前专业摘要，不保存或编辑这些专业事实的副本。变化捕获、动态解析 API、`model.catalog.read` 权限和软依赖边界以 [Model 概念与数据约束规范](docs/model概念与数据约束规范.md) 为准。

DimensionHierarchy 已整体迁入 Model，`logical_fields.hierarchy_id + hierarchy_level` 以及对 `standard.dimension_hierarchies` 的依赖已经删除。旧 `fact_metric_mappings` 已由 Model-owned MetricImplementation 整体替换，不保留兼容路线。

Model Entity 与 LogicalTable 的专业关系由概念实现映射提供，并通过当前 User Token 读取 `/:id/relations` 一跳图；它与只供 `addp-catalog` 机器同步使用的变化流、批量摘要解析严格分离。该查询只读 Model 本地事实，不调用 Catalog 或 Standard，不保存 CatalogEntry 反向引用。旧 `logical_tables.entity_id` 单值指针已删除，不保留兼容字段。

Model 负责已审批逻辑表的目标物理表创建、结构校验与显式删除；Develop 负责计算并写入已存在表，Quality 负责目标表数据校验，Orchestrator 负责依赖、调度和完整流程。建表不计算数据，数据刷新不依赖发布组或暂存批次。

Model 声明 `logical_table_materialization` TaskProvider；已持久化的 LogicalTable 是任务定义事实源，任务 ID 等于逻辑表 ID，不新增或复制建表配置。仅已审批且配置目标的逻辑表可执行。手动入口和 Orchestrator 入口复用同一执行服务，写入 `common.task_executions`；执行冻结逻辑表版本，运行时必须匹配该版本。成功输出 `execution_id + target_locator`。

**端口**:
- 后端: `8181`（环境变量 `MODEL_BACKEND_PORT`）
- 前端: `5182`（开发环境）

**数据库 Schema**: `model`

独立前端根路径在 AuthContext 加载后，按 Console 菜单顺序 `/entities`、`/er-diagram`、`/dw-layers`、`/logical-tables`、`/metric-implementations`、`/star-schema` 选择第一个符合页面准入条件的地址；没有可访问页面时进入 `/forbidden`。登录后的默认返回地址为根路径。显式业务地址仍只按自身权限判断，不静默改跳其他页面。

## 目录结构

主要实现入口如下；指标职责与修订约束见[指标实现独立修订](docs/model概念与数据约束规范.md#指标实现独立修订)，访问与定位规则见[前端路由](#前端路由)，测试覆盖及实际验收边界见[开发与验证](#开发与验证)。

```
model/
├── authorization/
│   └── permissions.yaml              # Model owner Permission Manifest
├── backend/
│   ├── cmd/server/main.go           # 应用入口
│   ├── go.mod                       # github.com/addp/model
│   └── internal/
│       ├── api/
│       │   ├── router.go            # 路由配置 + SetupRouter
│       │   ├── entity_handler.go
│       │   ├── entity_relation_handler.go
│       │   ├── logical_table_handler.go
│       │   ├── table_relation_handler.go   # 事实表-维度表关联
│       │   ├── metric_implementation_handler.go # 指标实现与修订 API
│       │   └── dw_layer_handler.go
│       ├── config/config.go
│       ├── models/
│       │   ├── entity.go            # Entity, EntityAttribute, EntityRelation
│       │   ├── logical_table.go     # LogicalTable, LogicalField, TableRelation
│       │   ├── metric_implementation.go # 指标稳定身份与不可变修订
│       │   ├── dw_layer.go
│       │   └── common_types.go      # JSONB 等共用类型
│       ├── repository/              # 数据访问层
│       │   ├── metric_implementation_repository.go # 指标实现与修订的租户隔离读取、持久化
│       │   └── ...
│       └── service/
│           ├── mermaid_parser.go    # Mermaid ER 图解析器
│           ├── metric_implementation_service.go # 指标生命周期、依赖复核与计划构建
│           └── ...
└── frontend/
    └── src/
        ├── api/
        │   ├── auth.js              # System 认证 API 适配
        │   ├── client.js            # Model 客户端与 403 授权刷新
        │   └── model.js             # Model API 及 Standard、Service 跨模块调用
        ├── store/auth.js            # 接入共享认证 Store
        ├── router/index.js          # 模块路由、根落点与共享认证守卫接入
        ├── composables/useUnsavedChanges.js # 未保存状态与离页确认
        ├── utils/
        │   ├── moduleNavigation.js  # 接入共享 Console 模块导航
        │   └── routeState.js        # 列表筛选等可恢复 URL 状态解析与构造
        ├── views/
        │   ├── EntityList.vue
        │   ├── EntityDetail.vue
        │   ├── LogicalTableList.vue
        │   ├── LogicalTableDetail.vue
        │   ├── StarSchemaView.vue   # 维度建模
        │   ├── ERDiagramManager.vue # 实体关系图
        │   ├── DWLayerList.vue
        │   └── MetricImplementationWorkspace.vue # 指标列表、修订编辑与发布、Service 绑定交互
        └── components/
            ├── DDLPreviewDialog.vue
            ├── MetricImplementationLinks.vue # 逻辑表详情中的指标列表与工作区导航
            ├── MetricRevisionServices.vue # 确定修订的引用服务只读弹窗
            └── MetricSourceSummary.vue # 已保存修订的冻结来源摘要
```

## 当前数据库表结构

本节记录当前实现；聚合与边界以 [Model 概念与数据约束规范](docs/model概念与数据约束规范.md) 为准。

### `model.entities` — 业务实体

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 主键 |
| tenant_id | int64 | 租户 ID |
| domain_id | int64? | 所属业务域（引用 standard.domains） |
| name / code | string | 显示名 / 英文标识符 |
| description | text | 描述 |
| status | string | `draft` / `approved` |
| version | int64 | Entity 聚合的资源并发版本，从 1 开始 |
| created_by / updated_by | int64 | 操作人 |

### `model.entity_attributes` — 实体属性

| 字段 | 类型 | 说明 |
|------|------|------|
| entity_id | int64 | 所属实体 |
| element_id | int64? | 引用 `standard.elements`（无 DB FK） |
| element_revision_id | int64? | Entity 审批时冻结的数据元修订；草稿必须为空 |
| name / column_name | string | 属性名 / 物理列名 |
| data_type | string | string/int/bigint/float/date/datetime/bool 等 |
| is_pk / nullable | bool | 主键 / 可空 |
| sort_order | int | 显示顺序 |

### `model.entity_relations` — 实体关系

| 字段 | 类型 | 说明 |
|------|------|------|
| source_entity / target_entity | int64 | 源/目标实体 |
| relation_type | string | `one_to_one` / `one_to_many` / `many_to_many` |
| name | string | 关系名称 |
| version | int64 | 独立关系事实的资源并发版本，从 1 开始 |

### `model.logical_tables` — 逻辑表

| 字段 | 类型 | 说明 |
|------|------|------|
| table_type | string | `entity` / `fact` / `dimension` |
| layer | string | 当前 Tenant 中已存在的 DWLayer 编码 |
| status | string | `draft` / `approved` |
| grain_description | text | 粒度声明（仅 fact 表） |
| scd_type | int | 维度历史策略：0=保留原值、1=覆盖当前值、2=历史行、3=历史属性列；仅设计声明，加工由 Develop 实现（仅 dimension 表） |
| materialization | JSONB | 物理目标配置，仅包含 `target_parent_locator + target_name` |
| version | int64 | LogicalTable 聚合的资源并发版本，从 1 开始 |

事实表和维度表本身就是 LogicalTable，不要求从 Entity 先生成一套 `table_type=entity` 的中间表。`entity` 角色仅用于业务明确需要的规范化核心逻辑层。

### `model.logical_fields` — 逻辑表字段

| 字段 | 类型 | 说明 |
|------|------|------|
| table_id | int64 | 所属逻辑表 |
| element_id | int64? | 引用 `standard.elements`（无 DB FK） |
| element_revision_id | int64? | LogicalTable 审批时冻结的数据元修订；草稿必须为空 |
| field_role | string | `regular` / `measure_additive` / `measure_semi` / `measure_non` / `dimension_fk` / `degenerate_dim` |
| is_pk | bool | 是否主键 |

### `model.dimension_hierarchies` / `model.dimension_hierarchy_levels` — 维度层级聚合

维度层级归属一个 `table_type=dimension` 的 LogicalTable；层级成员通过 `field_id` 引用同一逻辑表的 LogicalField，`level_num` 从 1 开始且在层级内唯一。层级及成员不建立独立并发版本，所有写操作校验并推进父 LogicalTable 的 `version`。

### `model.table_relations` — 逻辑表间关系

| 字段 | 类型 | 说明 |
|------|------|------|
| source_table / source_field | int64 | 事实表 ID / 事实表字段 ID（外键字段） |
| target_table / target_field | int64 | 维度表 ID / 维度表字段 ID（主键字段） |
| relation_type | string | `fk`（外键关联）/ `join`（宽泛关联） |
| tenant_id | int64 | 租户隔离 |

### 概念实现映射

三类映射通过 `tenant_id + table_id` 归属 LogicalTable 聚合，共用父 LogicalTable 的 `version`；字段定义见[concept_mapping.go](backend/internal/models/concept_mapping.go)。

| 表 | 引用与冻结字段 |
| --- | --- |
| `model.logical_table_entity_mappings` | `entity_id`、`mapping_role`；`entity_version` 冻结来源 Entity 版本 |
| `model.logical_field_attribute_mappings` | `field_id` 对应 `entity_id + entity_attribute_id`；`mapping_role` |
| `model.table_relation_entity_relation_mappings` | `table_relation_id` 对应 `entity_relation_id`；`orientation` 表示方向，`entity_relation_version` 冻结来源 EntityRelation 版本 |

唯一完整替换、来源冻结与审批复核统一见[聚合与引用](docs/model概念与数据约束规范.md#三聚合与引用)；父版本推进及事务约束见[并发版本与聚合写入](docs/model概念与数据约束规范.md#并发版本与聚合写入)。权限和资源归属见[授权边界](docs/model概念与数据约束规范.md#二授权边界)，实际验收范围见[开发与验证](#开发与验证)。

### `model.metric_implementations` / `model.metric_implementation_revisions`

稳定身份保存 `fact_table_id`、`metric_definition_id`、名称和独立 `version`；修订保存 `revision_no`、`metric_definition_revision_id`、类型化 `contract`、`dependency_snapshot`、`dependency_hash` 与 `status`。

事实表仅作来源引用，修订写入使用实现版本。修订状态、草稿唯一性及发布／撤回／删除规则统一见[指标实现独立修订](docs/model概念与数据约束规范.md#指标实现独立修订)；权限见[IAM Permission 所有权](#iam-permission-所有权)，实际验收范围见[开发与验证](#开发与验证)。

### `model.dw_layers` — 数仓分层

| 字段 | 类型 | 说明 |
|------|------|------|
| layer_code | string | Tenant 内唯一的自定义分层编码 |
| layer_name | string | 分层名称 |
| naming_rule | text | 命名规范 |
| sort_order | int | 显示顺序 |
| version | int64 | DWLayer 的资源并发版本，从 1 开始 |

### `model.entity_model_revisions` — Mermaid 导入预览基线

| 字段 | 类型 | 说明 |
|------|------|------|
| tenant_id | int64 PK | Tenant 唯一集合修订行 |
| revision | int64 | Mermaid 导入预览到确认提交之间的 Tenant 集合基线，从 1 开始 |

## API 端点（`/api/v1/model`）

### 业务实体

实体及属性入口按[当前路由登记](backend/internal/api/router.go)区分集合、单项与生命周期动作：

```text
GET    /api/v1/model/entities                       # 列表，支持 domain_id/status 过滤
POST   /api/v1/model/entities                       # 创建
GET    /api/v1/model/entities/standard_references   # 业务域标准引用查询
GET    /api/v1/model/entities/:id                   # 详情
PUT    /api/v1/model/entities/:id                   # 更新
DELETE /api/v1/model/entities/:id                   # 删除
GET    /api/v1/model/entities/:id/relations         # 只读一跳专业关系
POST   /api/v1/model/entities/:id/approve           # 审批通过（status: draft → approved）
POST   /api/v1/model/entities/:id/reopen            # 显式退回草稿
GET/POST /api/v1/model/entities/:id/attributes       # 属性列表/创建
PUT/DELETE /api/v1/model/entities/:id/attributes/:aid # 更新/删除确定属性
POST   /api/v1/model/entities/import-mermaid/preview # 预览 Markdown Mermaid 增量导入
POST   /api/v1/model/entities/import-mermaid         # 确认 Markdown Mermaid 增量导入
GET    /api/v1/model/entities/export-mermaid         # 按可选 domain_id 导出 Markdown Mermaid
```

退回草稿需要 `model.entity.update`，审批需要 `model.entity.approve`；属性各动作的精确 Permission 以路由登记和[权限清单](authorization/permissions.yaml)为准，Tenant 及父子资源归属校验见[授权边界](docs/model概念与数据约束规范.md#二授权边界)。

属性写入使用父 Entity 的 `version`；草稿与版本校验、属性写入及父版本递增须在同一事务中完成，具体约束及各聚合的版本归属见[并发版本与聚合写入](docs/model概念与数据约束规范.md#并发版本与聚合写入)。JSON body 传递及新版本返回规则见[API 字段契约](../docs/spec/api/资源版本与并发更新.md#字段与-api-契约)。

退回草稿时冻结修订的清理及再次审批的解析规则见[数据元修订冻结](docs/model概念与数据约束规范.md#数据元修订冻结)。端点登记与静态契约核对不代表真实运行通过，实际验收范围见[开发与验证](#开发与验证)。

Mermaid 的 `addp.model.er/v2` Markdown 格式、Standard 稳定 `domain_code / element_code`、业务域／全域导出边界、完整可编辑元数据及非破坏性增量规则统一见[Mermaid 交换规范](docs/model概念与数据约束规范.md#五mermaid-与-ddl)。导出响应示例：`{ "markdown": "...", "scope": "domain", "domain_code": "outdoor" }`。

预览提交 `{ "markdown": "..." }`，核对当前 Tenant 精确解析后的业务域摘要及创建、未变更、冲突计划，再以同一 Markdown 和预览 `revision` 确认。Tenant 集合 `revision` 的推进、确认时的锁定校验、事务内重新计划及失败回滚均沿用上述规范；联合权限与生命周期边界见[IAM Permission 所有权](#iam-permission-所有权)，实际覆盖见[开发与验证](#开发与验证)。

### 实体关系

```
GET/POST /api/v1/model/entity-relations
GET/PUT/DELETE /api/v1/model/entity-relations/:id
```

### 逻辑表

逻辑表及其子资源按[当前路由登记](backend/internal/api/router.go)区分集合、单项与动作入口：

```text
GET    /api/v1/model/logical-tables                                              # 列表，支持 table_type/status/domain_id 过滤
POST   /api/v1/model/logical-tables                                              # 创建
GET/PUT/DELETE /api/v1/model/logical-tables/:id                                   # 详情/更新/删除
GET    /api/v1/model/logical-tables/standard_references                          # 业务域标准引用查询
POST   /api/v1/model/logical-tables/:id/approve                                  # 审批
POST   /api/v1/model/logical-tables/:id/reopen                                   # 退回草稿
GET    /api/v1/model/logical-tables/:id/relations                                # 只读一跳专业关系
POST/DELETE /api/v1/model/logical-tables/:id/materialized-target                 # 创建或校验/显式删除目标物理表
GET/POST /api/v1/model/logical-tables/:id/fields                                 # 字段列表/创建
PUT/DELETE /api/v1/model/logical-tables/:id/fields/:fid                           # 更新/删除确定字段
POST   /api/v1/model/logical-tables/:id/preview-ddl                              # 预览建表语句
GET/POST /api/v1/model/logical-tables/:id/dimension-relations                    # 事实表维度关联列表/创建（含字段映射）
PUT/DELETE /api/v1/model/logical-tables/:id/dimension-relations/:rid              # 更新/删除确定关联
GET/POST /api/v1/model/logical-tables/:id/dimension-hierarchies                  # 维度表层级列表/创建（含成员）
PUT/DELETE /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid            # 更新/删除确定层级
POST   /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid/levels         # 创建层级成员
PUT/DELETE /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid/levels/:lid # 更新/删除确定成员
GET/PUT /api/v1/model/logical-tables/:id/concept-mappings                        # 概念实现映射读取/完整替换
```

字段、维度关联、层级及成员、概念实现映射共用所属 LogicalTable 的 `version`；JSON body 传递、事务版本推进与各写入对象的草稿条件见[并发版本与聚合写入](docs/model概念与数据约束规范.md#并发版本与聚合写入)，完整替换规则见[完整更新语义](docs/model概念与数据约束规范.md#完整更新语义)。请求仍须通过 Tenant、各路由的精确 Permission 与父子资源归属校验，见[授权边界](docs/model概念与数据约束规范.md#二授权边界)。

目标物理表操作的独立授权、执行与删除边界见[目标物理表建表](docs/model概念与数据约束规范.md#目标物理表建表)；业务域引用查询见[业务域总览与五类标准接入](docs/model概念与数据约束规范.md#业务域总览与五类标准接入)。端点与静态契约核对不证明真实建表或业务查询成功，实际验收范围见[开发与验证](#开发与验证)。

### 指标实现与修订

指标实现是独立资源，以下路径与[当前路由登记](backend/internal/api/router.go)对应：

```text
GET    /api/v1/model/metric-implementations                                      # 列表，可按 fact_table_id 筛选
POST   /api/v1/model/metric-implementations                                      # 创建
GET    /api/v1/model/metric-implementations/standard_references                  # 业务域标准引用查询
GET    /api/v1/model/metric-implementations/:id                                  # 实现详情及修订
DELETE /api/v1/model/metric-implementations/:id                                  # 删除未发布身份
PUT    /api/v1/model/metric-implementations/:id/draft                            # 保存草稿
POST   /api/v1/model/metric-implementations/:id/revisions/:revision_id/publish   # 发布确定修订
POST   /api/v1/model/metric-implementations/:id/revisions/:revision_id/withdraw  # 撤回确定修订
POST   /api/v1/model/metric-implementations/:id/revisions/:revision_id/plan      # 读取确定发布修订的计划包
```

各入口经过统一认证与 Tenant 上下文校验，精确 Permission 以路由登记和 [Model 权限清单](authorization/permissions.yaml)为准；资源归属见[授权边界](docs/model概念与数据约束规范.md#二授权边界)。已有资源写入的实现 `version`、修订生命周期与计划依赖复核统一见[指标实现独立修订](docs/model概念与数据约束规范.md#指标实现独立修订)；路径中的 `revision_id` 是实现修订 ID，绑定确定修订，不自动选择最新版。业务域引用查询见[业务域总览与五类标准接入](docs/model概念与数据约束规范.md#业务域总览与五类标准接入)。

端点登记及契约核对不代表真实指标计算或跨模块消费已验收；实际范围见[开发与验证](#开发与验证)。

## 前端路由

以下为 Console 业务入口，对应 [Model 路由登记](frontend/src/router/index.js)中的模块内子路由；Console 地址与独立模块加载地址的分工见[公开路由事实源](../docs/spec/addp前端路由与可恢复状态规范.md#二公开路由事实源)。登录与根路径的落点规则见[模块概述](#模块概述)。

```text
/modeling/dw-layers                        # 数仓分层
/modeling/entities                         # 业务实体列表
/modeling/entities/:id                     # 实体详情（属性、关系、Mermaid 图）
/modeling/logical-tables                   # 逻辑表列表
/modeling/logical-tables/:id               # 逻辑表详情（字段、维度层级、物理目标、建表语句预览）
/modeling/er-diagram                       # 按业务域查看 ER 图；domain_id=all 显式全域
/modeling/star-schema                      # 维度建模（事实表-维度表-指标三维关联）
/modeling/metric-implementations/:id?      # 指标实现工作区：列表、创建或详情
```

指标实现入口省略 `:id` 时为列表，`new` 为创建入口，具体 ID 为实现详情；详情用 `?revision_id=<修订ID>` 定位该实现的确定修订。实现 ID 与修订 ID 分别传递，指定的修订不属于当前实现时显示不可用，见[工作区加载逻辑](frontend/src/views/MetricImplementationWorkspace.vue)。列表筛选与其他页面的可恢复状态统一见[Canonical URL](../docs/spec/addp前端路由与可恢复状态规范.md#五canonical-url)。

页面准入按共享 [Console 路由权限登记](../common-frontend/basic/src/authorization/consoleRouteAccess.js)复核当前 Tenant 上下文及所需权限；创建、修改、发布等动作仍遵循[Model 授权边界](docs/model概念与数据约束规范.md#二授权边界)，由后端复核权限与资源归属。路由登记及修订参数核对属于静态验证，前端回归与真实跨模块验收按[开发与验证](#开发与验证)分别认定。

## 模块依赖关系

**依赖**:
- **System 模块**: 统一用户／服务身份认证及权威 AuthContext（`SYSTEM_URL`）。用户身份使用 User Access Token，服务身份通过独立 OAuth Client 获取 Service Access Token；Bearer 请求与解析主线统一见[登录认证要求](../docs/spec/addp登录认证的统一要求.md#一认证事实与唯一主路径)和[AuthContext 唯一调用链](../docs/spec/addp授权上下文规范.md#二唯一调用链)。Model 继续按 Tenant、精确 Permission 和资源归属裁决，用户与机器入口的完整边界见[Model 授权规范](docs/model概念与数据约束规范.md#二授权边界)；实际运行覆盖按[开发与验证](#开发与验证)分层认定。
- **Meta 模块**: 提供指标计划所需的已扫描物理结构（`META_URL`，配置默认 `http://localhost:8082`）；Model Runtime 使用已有 `meta.catalog.read`，不新增 System 结构查询权限。字段投影与依赖冻结见[指标计划与数据库金样](#指标计划与数据库金样)。
- **Standard 模块**:
  - **服务端引用校验**：Service 层通过 `cfg.StandardURL`（`STANDARD_URL`），以当前 Tenant 的 `addp-model` Service Access Token 校验 domain_id、element_id / element_revision_id、metric_definition_revision_id；不转发或保存 User Access Token。维度层级只校验 Model 本地事实；服务身份的读取范围见[Model 授权规范](docs/model概念与数据约束规范.md#二授权边界)。
  - **前端引用读取**：[Model 前端](frontend/src/api/model.js)通过当前用户会话的共享客户端，以 User Access Token 调用 Standard 唯一公开 API，由 Standard 校验该用户的权限与资源归属。Model 不提供 Standard 代理路径；服务端引用校验成功不代表用户取得 Standard 读取权限。身份主线见[登录认证要求](../docs/spec/addp登录认证的统一要求.md#一认证事实与唯一主路径)，实际覆盖按[开发与验证](#开发与验证)分层认定。

**被依赖**:
- **Graph 模块**: 使用 `tenant.graph_runtime` 读取 Entity、EntityAttribute 和 EntityRelation，生成本体导入预览。

## 跨 Schema 关联设计

Model 和 Standard 使用不同的 PostgreSQL Schema，**无数据库外键约束**，通过应用层验证：

| Model 字段 | 引用 Standard |
|-----------|-------------|
| `entities.domain_id` | `standard.domains.id` |
| `entity_attributes.element_id` | `standard.elements.id` |
| `logical_fields.element_id` | `standard.elements.id` |
| `metric_implementation_revisions.metric_definition_revision_id` | `standard.metric_definition_revisions.id` |

创建/更新时，Service 层通过 HTTP 校验 Standard 引用；前端读取与服务端校验的身份、权限分工统一见[模块依赖关系](#模块依赖关系)。

## IAM Permission 所有权

Model 是 `model.logical_model.*` 第一批 Permission 的唯一 owner，机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Model 服务启动时不向 System 动态注册 Permission。

Entity、EntityRelation、DWLayer 和 LogicalModel 分别使用 `model.entity.*`、`model.entity_relation.*`、`model.dw_layer.*`、`model.logical_model.*`。EntityAttribute 是 Entity 聚合内子资源；LogicalField、TableRelation、DimensionHierarchy 是 LogicalModel 聚合内子资源；MetricImplementation 是独立聚合，使用 model.metric_implementation 精确权限。Mermaid 导入预览和确认均按 Entity 与 EntityRelation 的 create 执行 all-of 校验；导出按两者的 read 执行 all-of 校验。导入只创建缺失成员，不要求全租户已审批实体退回草稿；新关系仍不得绕过两端实体的生命周期约束。

各聚合的版本主体、同事务校验与递增、EntityRelation 完整 `PUT` 及变更前后端点锁定统一见[并发版本与聚合写入](docs/model概念与数据约束规范.md#并发版本与聚合写入)和[完整更新语义](docs/model概念与数据约束规范.md#完整更新语义)；`409 resource_version_conflict` 后保留未保存内容及禁止自动重试的规则见[冲突响应与前端体验](../docs/spec/api/资源版本与并发更新.md#冲突响应与前端体验)。

Cleanup 内部强制写入的资源锁定、资源 `version` 与 Tenant 实体模型集合 `revision` 推进，以及物理清理的单事务边界，统一见[Model Cleanup 约束](docs/model概念与数据约束规范.md#五mermaid-与-ddl)。权限及写入契约不代表运行验收通过，实际覆盖见[开发与验证](#开发与验证)。

## 特殊设计

### Mermaid 解析器

[mermaid_parser.go](backend/internal/service/mermaid_parser.go) 负责交换文档解析；[EntityService](backend/internal/service/entity_service.go) 负责 Standard 稳定编码解析、预览计划与确认导入。协议、集合修订及事务正文统一见[Mermaid 交换规范](docs/model概念与数据约束规范.md#五mermaid-与-ddl)。

### Entity 与逻辑表状态机

`draft` 与 `approved` 的显式审批、退回草稿及审批条件统一见[正式生命周期规范](docs/model概念与数据约束规范.md#四生命周期)；`materialized` 不属于生命周期状态。详情、引用回显及 DDL 查看／预览的只读边界、未保存状态和操作后的重载要求也以该节为准。

操作须按[授权边界](docs/model概念与数据约束规范.md#二授权边界)和[当前路由登记](backend/internal/api/router.go)校验 Tenant、精确 Permission 与资源归属。状态及只读契约不代表运行验收通过，实际覆盖见[开发与验证](#开发与验证)。

### `dimension-relations` 查询返回 JOIN 结果

查询方向、事实表详情的唯一编辑入口（`?tab=relations&relation_id=<id>`）及维度建模只导航的规则统一见[建模导航规范](docs/model概念与数据约束规范.md#建模导航与物理目标操作入口)；事实侧父版本、草稿及已审批维度约束见[并发版本与聚合写入](docs/model概念与数据约束规范.md#并发版本与聚合写入)。权限及资源归属见[授权边界](docs/model概念与数据约束规范.md#二授权边界)，实际验收范围见[开发与验证](#开发与验证)。

`GET /api/v1/model/logical-tables/:id/dimension-relations` 通过 Raw SQL JOIN 返回 [TableRelationDetail](backend/internal/models/logical_table.go) 列表，已包含两端表名、编码、字段名和列名，展示这些字段无需额外查询。以下为单个列表元素示例：

```json
{
  "id": 1,
  "source_table": 2,
  "source_table_name": "订单事实",
  "source_table_code": "dwd_order",
  "source_field": 3,
  "source_field_name": "客户ID",
  "source_field_code": "customer_id",
  "target_table": 5,
  "target_table_name": "客户维度表",
  "target_table_code": "dim_customer",
  "target_scd_type": 2,
  "target_field": 8,
  "target_field_name": "客户主键",
  "target_field_code": "id",
  "relation_type": "fk"
}
```

### Mermaid 渲染

前端使用 `import mermaid from 'mermaid'`（npm 包），**不是** `window.mermaid`。渲染时必须：
1. `mermaidEl.removeAttribute('data-processed')` — 清除已渲染标记
2. `mermaidEl.textContent = code` — 重置源码
3. `await mermaid.run({ nodes: [mermaidEl] })` — 渲染

参考 [EntityDetail.vue](frontend/src/views/EntityDetail.vue) 和 [ERDiagramManager.vue](frontend/src/views/ERDiagramManager.vue)。

## 开发注意事项

质量相关边界：LogicalTable 详情通过只读 `structural_constraints` 汇总主键组合、必填字段和出向外键，唯一事实源仍是字段和表关系。DWLayer 不再保存 `quality_sla`；检查方案、阈值和执行策略属于 Quality。详见 [Model 概念与数据约束规范](docs/model概念与数据约束规范.md#与数据质量的边界)。

1. **新增 API**: `models` → `repository` → `service` → `handler` → `router.go` → `main.go`（注入依赖）

2. **重启服务**:
   ```bash
   bash scripts/dev/restart.sh -model
   ```

3. **跨模块验证**: 调用身份与权限见[模块依赖关系](#模块依赖关系)。如果 Standard 服务未启动，相关创建操作会失败。

4. **前端 API 职责**:

   - [client.js](frontend/src/api/client.js) 使用共享 `createAPIClient` 创建 Model 业务客户端，并提供跨客户端复用的 403 授权刷新；刷新后仍将原始拒绝交给调用方，不自动再次查询。
   - [auth.js](frontend/src/api/auth.js) 适配 System 认证 API；[store/auth.js](frontend/src/store/auth.js) 接入共享 `createAuthStore`，认证与会话主线统一见[登录认证要求](../docs/spec/addp登录认证的统一要求.md#一认证事实与唯一主路径)。
   - [model.js](frontend/src/api/model.js) 组织 Model 业务 API、Standard 引用读取及 Service 查询定义的引用查询、创建与重绑。跨模块客户端均使用当前用户会话；Standard 身份分工见[模块依赖关系](#模块依赖关系)，Service 操作由 Service 校验当前用户权限与资源归属。授权变化、令牌续期及失败后的结果处理见[引用服务弹窗与授权交互](#引用服务弹窗与授权交互)；入口存在不代表当前版本已通过真实跨模块验收。

   实体属性与逻辑字段的数据元选择只消费 Standard `current_revision`；冻结名称通过 `elements/:id/revisions/:revision_id` 读取，不能取 `draft_revision`、旧顶层字段或用当前名称替代历史名称。两个页面共用 `modelDetailState.js` 的引用加载与展示逻辑。实体审批／退回草稿后重载属性和修订引用。对应选择、不可用、历史修订及生命周期交互由既有 `make test-model-frontend` 覆盖。

   冻结修订链接由 `FrozenElementLink` 统一组合共享 Standard route builder 与 Console 导航；无 `standard.element.read` 时禁用，不在 Model 增加标准详情。跨模块地址与无权限浏览器回归由既有前端门禁覆盖。

## 开发与验证

篇内导航：[标准入口与数据库前提](#标准入口与数据库前提)、[指标计划与数据库金样](#指标计划与数据库金样)、[维度关联回归](#维度关联回归)、[浏览器失败产物](#浏览器失败产物)、[引用服务弹窗与授权交互](#引用服务弹窗与授权交互)。以下为现有入口与覆盖说明，不表示当前版本已运行通过；未执行、失败和跳过项分别认定。

### 标准入口与数据库前提

执行与层级沿[测试与验收规范](../docs/spec/addp测试与验收规范.md#三标准入口)，从仓库根目录使用：

```bash
# 仅 Go T1，不计为完整模块门禁
make test-module MODULE=model GO_ONLY=1

# 配置测试 DSN 后运行 PostgreSQL T2
make test-model-postgres
```

PostgreSQL 门禁必须提供 `ADDP_TEST_MODEL_POSTGRES_DSN`，缺失时入口直接失败；本地先用 `bash scripts/infra/status.sh` 核实实际端口与实例归属，再配置指向共享测试库 `addp_test` 的 DSN，连接规则见[端口规范](../docs/spec/addp端口分配.md)。不按首选端口推断运行连接，也不自行创建临时 database。

直接运行的 PostgreSQL 领域用例可能因缺少 DSN 跳过，标准门禁拒绝缺失 DSN 或用例跳过。并发、事务和迁移相关改动必须走 `make test-model-postgres`，Go T1 或跳过项不计为 PostgreSQL 验收通过。

### 指标计划与数据库金样

指标计算采用“Model 构建中立计划 → 引擎独立编译器 → 唯一 PreparedQuery”，完整契约见 [Model 约束](docs/model概念与数据约束规范.md#数据库无关计划与修订) 和 [引擎插件规范](../docs/spec/addp引擎插件接口规范.md#数据库无关分析计算契约)。

Meta 物理结构只投影参与计算的字段事实到冻结包，扫描时间与无关显示字段不进入计算依赖；已扫描结构读取、来源绑定、依赖冻结和执行期漂移复核统一见[数据库无关计划与修订](docs/model概念与数据约束规范.md#数据库无关计划与修订)。结构校验、数据库金样与真实业务链路的验收范围分别认定。

元数据生命周期及 PG 金样沿 `make test-model-postgres`；MySQL 真实计算沿 `ADDP_TEST_MYSQL_PASSWORD=<disposable-password> make test-model-mysql`，自动清理 addp_model_mysql_it_* 并拒绝跳过，两者已登记 T2 并验证同一中立计划。TiDB 复用同一金样，经 `make test-common-tidb` 的隔离实例执行；测试来源路径从 Provider 的目录模型派生，不按引擎名判断 namespace。

Service 负责在线指标查询，Develop 负责持久表加工；页面物理绑定与建表扩展另行跟踪。

### 维度关联回归

维度关联改动沿用现有自动发现门禁：`make test-module MODULE=model` 覆盖平台一致性、Go 单元、前端路由及交互、PostgreSQL 事务测试；其中数据库测试需配置上述测试 DSN。`make test-model-frontend` 包含关系入口唯一所有权、URL 恢复、审批只读、原位更新和冲突保留测试；CI 继续使用已登记的 Model 前端与 PostgreSQL 作业。

### 浏览器失败产物

Model 确定性浏览器门禁只在失败时保留截图与 Playwright trace；trace 可用于查看页面快照、请求响应及浏览器错误。产物写入操作系统临时目录（CI 通过 `TMPDIR=RUNNER_TEMP` 指向 Runner 目录）下当轮独立的 `addp-model-playwright-results-*/`，不进入仓库。

Model 前端 CI Job 在失败时上传该目录，保留 14 天；每轮使用独立目录，重跑不覆盖此前失败产物；成功退出时删除当轮目录。

排查使用失败输出中的 `playwright show-trace` 命令，不通过增加重试或放宽断言超时掩盖偶发失败。

### 引用服务弹窗与授权交互

指标修订的引用服务弹窗和撤回确认弹窗只在打开、翻页或显式刷新时查询引用服务。403 后重载授权不得自动再次查询，避免拒绝请求与令牌轮换形成循环；失去读取权限时立即清除已有结果，未完成请求也不得写入无权限界面。

令牌续期中的授权重载不取消原有查询；引用查询入口在返回成功结果前等待正在进行的 AuthContext 加载，两个弹窗再按最终读取权限展示，不能因查询响应先于授权响应到达而丢失结果。该行为由 `make test-model-frontend` 的可控授权重载与续期用例覆盖。

## 前端公开路由

- 模块内 Router 使用 `/dw-layers`、`/entities`、`/logical-tables`、`/er-diagram`、`/star-schema`；Console 模块名为 `modeling`，公开 URL 统一加 `/modeling` 前缀。
- 实体和逻辑表详情使用 `/:id`；实体详情默认 `basic` Tab 省略，`attributes`、`relations` 使用唯一 `tab` query。
- 维度建模业务域使用 `domain_id`（省略表示全部），当前事实表使用 `table_id`，并响应刷新及浏览器前进/后退；无选择时省略该 query。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`；详情返回明确列表路由。
- 逻辑表详情的“物理目标”页签集中提供目标引擎、目标位置与目标表名展示，以及建表语句预览、创建/校验、执行记录和删除目标表操作；完整数据流程从 Orchestrator 进入。

- Model 导航依次为业务实体、实体关系图、数仓分层、逻辑表设计、维度建模。
