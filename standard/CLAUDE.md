# Standard 模块 CLAUDE.md

本文件为 Claude Code 在 `standard/` 目录下工作时提供指导。

数据元 `quality_rules` 的结构和校验必须遵守 [ADDP 数据质量规范](../docs/spec/addp数据质量规范.md)。每条规则必须有由 Standard 首次创建时生成且编辑过程中保持不变的 `rule_key`；Standard 只拥有规则定义，不拥有物理字段应用、规则执行、评分或质量问题。

Standard 定义 Domain、Glossary、Element、MetricDefinition、CodeSet、Unit 和标准来源文档等可复用业务语义，但不拥有这些语义与具体 DataItem、CatalogEntry 或 CatalogComponent 的应用关系。具体字段/组件到标准修订的映射只由 Catalog 保存；规则应用、执行、符合性结果和问题只由 Quality 保存。Standard 不依赖 Meta 或 Catalog，不保存 `catalog_entry_id`、反向资源列表或质量执行事实。安全分类、安全等级、敏感类型和保护基线统一属于 Security，Standard 不保存第二份安全事实。

Metric 的指标依赖与基准指标关系通过当前 User Token 读取 `GET /metrics/:id/relations` 一跳图；它要求 `standard.metric.read`，只读 Standard 本地事实，不调用 Catalog 或 Model，也不使用 `standard.catalog.read` 机器权限替代用户权限。数据元、Domain、指标分类和单位继续留在 Metric 专业详情中，本阶段不伪造为企业目录节点。

## 模块概述

**Standard 模块** 是 ADDP 平台的数据标准和治理中心，负责：

- 业务域（Domain）树形组织管理
- 标准集（StandardCollection）：跨业务域成员快照、对象级职责、审核发布和不可变治理事件
- 业务术语（Glossary）：以稳定身份和不可变修订统一业务语言、别名、定义、生效区间与关联数据元
- 数据元（Element）：数据标准的核心原子对象，定义数据规格和质量规则
- 码值集（CodeSet）：系统/自定义码值集及码值项
- 计量单位（Unit）：按度量类别组织的计量单位
- 指标定义（MetricDefinition）：指标业务含义、统计口径、单位、责任归属和适用范围；不保存模型粒度、连接、过滤或可执行表达式
- 标准文档（Document）：标准来源及其不可变文档修订、提取证据和审核关系

维度层级、公共/一致性维度和指标实现统一属于 Model。`standard.dimension_hierarchies` 旧表、API、权限和前端路由已经删除；Metric 中的实现型字段仍是待拆分旧实现，不得据此继续扩展 Standard。

**端口**:
- 后端: `8110`（环境变量 `STANDARD_BACKEND_PORT`）
- 前端: `5181`（开发环境）

**数据库 Schema**: `standard`

**外部存储**: MinIO（存储标准文档文件，bucket 名为 `standard`）

## 已确认的目标边界与迁移顺序

- 业务域只表达业务语义与治理责任，不表达可见范围、审核容器或目录分类。
- 发布型标准采用“稳定身份 + 不可变修订”；统一状态为 `draft → in_review → published → withdrawn`，按半开生效区间动态解析当前修订。
- 标准对象显式保存 `scope_type=platform|tenant_common|domain`；仅 `domain` 必须指定 `owner_domain_id`。码值集不得再以“租户自定义”为由强制归属业务域。
- 范围模型只保留 `scope_type + owner_domain_id`；启动迁移遇到历史 `domain_id` 时必须一次性回填归属和范围并删除旧列，即使新旧列曾同时存在，也不得保留双轨字段。
- StandardCollection 采用“稳定身份 + 治理配置修订 + 成员快照 + 对象级职责分配”。集合修订只审核名称、说明和成员清单，不替代成员对象自身的修订发布；StandardCategory 只承担浏览导航，两者均不得替代业务域和适用范围。
- StandardCollectionAssignment 只绑定当前租户的 User Principal，角色固定为 `owner|maintainer|reviewer`。模块 Permission 是粗粒度门禁，Assignment 是集合对象级门禁；Owner 可管理职责分配并维护草稿，Maintainer 可编辑和提交，Reviewer 可退回和发布，且发布者不得是提交者。
- Copilot 只生成标准候选，必须保留文档修订、页码/章节/行号/文本片段等来源证据。Standard 保存提炼批次、候选和人工处置事实；`retained` 只表示保留为后续建标输入，人工创建并审核发布后才成为正式标准修订。
- Element、CodeSet、MetricDefinition、Glossary、Document 的 Scope、归属域和修订生命周期统一；StandardCollection 已按上述单一路径实现；DimensionHierarchy 已整体迁入 Model。下一批补齐 Catalog → Quality 落标闭环。

文档文件采用“新对象上传、数据库切换引用、旧对象补偿清理”的顺序。失效对象记录在 `standard.document_file_cleanups`，该表仅用于物理清理重试，不作为文档当前文件引用。

文档上传大小和 MinIO 操作超时由部署配置 `STANDARD_DOCUMENT_MAX_FILE_SIZE`、`STANDARD_DOCUMENT_STORAGE_TIMEOUT` 控制。

## 目录结构

```
standard/
├── authorization/
│   └── permissions.yaml              # Standard owner Permission Manifest
├── backend/
│   ├── cmd/server/main.go             # 应用入口
│   ├── go.mod                         # github.com/addp/standard
│   └── internal/
│       ├── api/
│       │   ├── router.go              # 路由配置
│       │   ├── standard_collection_handler.go
│       │   ├── domain_handler.go
│       │   ├── glossary_handler.go
│       │   ├── element_handler.go
│       │   ├── code_set_handler.go
│       │   ├── unit_handler.go
│       │   ├── classification_handler.go
│       │   ├── metric_handler.go
│       │   └── document_handler.go
│       ├── config/config.go
│       ├── models/
│       │   ├── standard_collection.go
│       │   ├── domain.go
│       │   ├── glossary.go
│       │   ├── element.go
│       │   ├── code_set.go
│       │   ├── unit.go
│       │   ├── classification.go
│       │   ├── metric.go
│       │   ├── document.go
│       │   └── common_types.go        # StringArray, Int64Array, JSONB
│       ├── repository/
│       └── service/
└── frontend/
    └── src/
        ├── api/
        │   └── standard.js            # 所有 API 调用
        ├── views/
        │   ├── DomainList.vue
        │   ├── StandardCollectionList.vue / StandardCollectionDetail.vue
        │   ├── GlossaryList.vue / GlossaryDetail.vue
        │   ├── ElementList.vue / ElementDetail.vue
        │   ├── CodeSetList.vue / CodeSetDetail.vue
        │   ├── UnitList.vue
        │   ├── MetricList.vue / MetricDetail.vue
        │   └── DocumentList.vue
        └── components/
            ├── DocumentPanel.vue       # 通用文档关联面板
            └── DDLPreviewDialog.vue
```

## 当前数据库表结构

本节记录当前实现。目标模型以平台术语表和核心对象 ER 图为准；改造时直接替换旧字段与旧资源，不增加兼容字段或并行 API。

### `standard.domains` — 业务域（树形）

| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点（支持多层级） |
| name / code | string | 显示名 / 英文标识符 |
| icon | string | 图标标识 |
| sort_order | int | 同层排序 |

### `standard.standard_collections` 与治理子表 — 标准集

| 表 | 核心事实 |
|------|------|
| `standard_collections` | 租户内唯一且不可变的 `code`、当前草稿指针、并发 `version` |
| `standard_collection_revisions` | 名称、说明、成员清单的修订身份和 `draft → in_review → published → withdrawn` 状态 |
| `standard_collection_members` | 修订内冻结的 `member_type + member_id`；只引用标准对象稳定身份 |
| `standard_collection_assignments` | 当前租户 User Principal 的 `owner / maintainer / reviewer` 对象级职责 |
| `standard_collection_events` | 创建、草稿更新、提交、退回、发布和职责替换的不可变事件 |

标准集不保存 `domain_id` 或 `scope_type`，允许跨业务域组织成员。发布新集合修订会在同一事务中撤回上一已发布修订。已发布标准集不能删除；职责人员后来停用或移除时，历史职责与事件仍可读取并明确显示为不可用，但不能再被新增到职责分配。

### `standard.glossaries` — 业务术语稳定身份

| 字段 | 类型 | 说明 |
|------|------|------|
| code | string | Tenant 内唯一且不可变的术语编码 |
| scope_type | string | `platform` / `tenant_common` / `domain`；租户公开写接口只允许后两者 |
| owner_domain_id | int64? | 归属业务域；仅 `scope_type=domain` 时必填 |
| steward_id | int64? | 数据责任人 |
| tags | StringArray | 标签 |
| draft_revision_id | int64? | 当前唯一草稿或审核中修订指针 |
| version | int64 | 稳定身份乐观锁版本，不是业务版次 |

读取聚合额外返回派生事实 `has_publication_history`。只有该值为 `false` 的从未发布术语才允许删除；Service/Repository 仍在同一事务内复核并删除身份及其草稿修订，存在 `published` 或 `withdrawn` 历史时返回 409。

### `standard.glossary_revisions` — 业务术语修订

| 字段 | 类型 | 说明 |
|------|------|------|
| glossary_id / revision_no | int64 | 术语身份 / 业务修订号，组合唯一 |
| status | string | `draft` / `in_review` / `published` / `withdrawn` |
| name / alias | string / StringArray | 规范名称与别名列表 |
| definition / example / note | text | 定义、示例、备注 |
| related_ids | Int64Array | 关联术语稳定身份 ID 列表 |
| change_summary | text | 本次修订说明 |
| effective_from / effective_to | timestamp? | 半开生效区间 `[from,to)` |

### `standard.glossary_element_mappings` — 术语与数据元映射

| 字段 | 类型 | 说明 |
|------|------|------|
| glossary_id / element_id | int64 PK | 复合主键，多对多关联 |

### `standard.elements` — 数据元稳定身份

| 字段 | 类型 | 说明 |
|------|------|------|
| code | string | Tenant 内唯一且不可变的标准编码 |
| scope_type | string | `platform` / `tenant_common` / `domain`；租户公开写接口只允许后两者 |
| owner_domain_id | int64? | 归属业务域；仅 `scope_type=domain` 时必填 |
| steward_id | int64? | 数据责任人 |
| tags | StringArray | 标签 |
| draft_revision_id | int64? | 当前唯一可编辑草稿 |
| version | int64 | 资源并发版本，不是业务版次 |
| lifecycle_state | string | `active` / `deleting` |

### `standard.element_revisions` — 数据元修订

| 字段 | 类型 | 说明 |
|------|------|------|
| element_id / revision_no | int64 | 数据元身份 / 业务修订号 |
| status | string | `draft` / `in_review` / `published` / `withdrawn` |
| name / definition | string/text | 本修订的标准名称与定义 |
| data_type | string | `string` / `int` / `bigint` / `float` / `decimal` / `date` / `datetime` / `bool` / `json` / `text` |
| length / precision_num / scale | int? | 与数据类型相容的表示约束 |
| nullable / default_value / format | bool/string | 可空、默认值和格式约束 |
| value_domain_kind | string | `unrestricted` / `range` / `enumeration` |
| range_constraint | JSONB | 连续值域结构；仅 `range` 使用 |
| code_set_revision_id | int64? | 枚举值域固定引用；仅 `enumeration` 使用 |
| unit_id | int64? | 计量单位引用 |
| extra_quality_rules | JSONB | 不能由标准语义推导的附加质量规则 |
| compiled_quality_rules | JSONB | 发布时从语义约束和附加规则编译的不可变规则快照 |
| change_summary | text | 本次业务变更说明 |
| effective_from / effective_to | time? | 半开生效区间 `[effective_from, effective_to)`；发布时 `effective_from` 不能为空 |
| submitted_by/at / published_by/at | mixed | 审核发布审计字段 |

### `standard.code_sets` — 码值集稳定身份

| 字段 | 类型 | 说明 |
|------|------|------|
| code | string | Tenant 内唯一且不可变的标准编码 |
| scope_type | string | `platform` / `tenant_common` / `domain`；Tenant 来源只能为后两者 |
| owner_domain_id | int64? | 归属业务域；仅 `scope_type=domain` 时必填 |
| origin | string | `platform` / `tenant`，只能由服务端决定 |
| steward_id | int64? | 数据责任人 |
| draft_revision_id | int64? | 当前唯一草稿 |
| version | int64 | 资源并发版本 |

### `standard.code_set_revisions` — 码值集修订

| 字段 | 类型 | 说明 |
|------|------|------|
| code_set_id / revision_no | int64 | 码值集身份 / 业务修订号 |
| status | string | 统一标准修订状态 |
| name / description | string/text | 本修订名称和定义 |
| value_type | string | 码值的表示类型，首期为 `string` / `int` / `bigint` |
| change_summary | text | 本次业务变更说明 |
| effective_from / effective_to | time? | 半开生效区间 `[effective_from, effective_to)`；发布时 `effective_from` 不能为空 |

### `standard.code_set_revision_items` — 码值修订项

| 字段 | 类型 | 说明 |
|------|------|------|
| code_set_revision_id | int64 | 所属码值集修订 |
| code / label | string | 机器编码 / 显示名称 |
| definition | text | 业务含义 |
| sort_order | int | 排序 |
| status | string | `active` / `deprecated` |
| replacement_item_id | int64? | 当前修订内推荐替代码值 |

### `standard.measurement_categories` — 度量类别

| 字段 | 类型 | 说明 |
|------|------|------|
| name / code | string | 类别名 / 标识 |
| is_system | bool | 是否系统内置 |

### `standard.units` — 计量单位

| 字段 | 类型 | 说明 |
|------|------|------|
| category_id | int64 | 所属度量类别 |
| name / symbol | string | 单位名 / 符号（如 kg、℃） |
| is_system | bool | 是否系统内置 |


> **注意**: 分级记录由系统初始化，仅支持 PUT 更新名称/描述/颜色，不支持创建和删除。

### `standard.metric_categories` — 指标分类（树形）

| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点 |
| name / code | string | 类别名 / 标识 |

### `standard.metric_definitions` / `standard.metric_definition_revisions` — 指标定义

指标定义使用“稳定身份 + 不可变修订”模型。稳定身份保存 `code`、`scope_type`、`owner_domain_id`、分类、责任人、标签和并发 `version`；修订保存 `metric_type`、名称、业务定义、统计口径、非引擎可执行的语义公式、单位、生效区间与统一发布状态。

`standard.metric_definition_revision_dependencies` 保存修订级语义依赖。草稿依赖 `dependency_definition_id`，发布时必须解析并冻结 `dependency_revision_id`；`atomic` 不允许依赖，`derived` 必须且只能有一个 `base`，`composite` 必须至少有一个 `component`。该表不保存模型表、字段、连接、过滤或可执行表达式。

公开路径继续以稳定产品资源名 `/metrics` 表示 MetricDefinition；修订使用 `/metrics/:id/revisions`，审核动作统一为 `submit`、`return`、`publish`、`withdraw`。旧 `approve`、`deprecate` 路径和扁平字段不保留。

| 字段 | 类型 | 说明 |
|------|------|------|
| `metric_definitions.category_id` | int64? | 所属指标分类 |
| `metric_definitions.scope_type / owner_domain_id` | string / int64? | 适用范围与归属业务域 |
| `metric_definitions.code` | string | 稳定英文标识 |
| `metric_definitions.steward_id / tags` | int64? / StringArray | 责任人与标签 |
| `metric_definitions.draft_revision_id / version` | int64? / int64 | 工作修订指针与聚合并发版本 |
| `metric_definition_revisions.metric_type` | string | `atomic` / `derived` / `composite` |
| `metric_definition_revisions.name / definition / statistical_caliber` | string / text | 名称、业务定义与统计口径 |
| `metric_definition_revisions.semantic_formula` | text | 非引擎可执行的语义公式 |
| `metric_definition_revisions.unit_id` | int64? | 引用 `standard.units` |
| `metric_definition_revisions.status` | string | `draft` / `in_review` / `published` / `withdrawn` |
| `metric_definition_revisions.effective_from / effective_to` | timestamp? | 发布生效区间，数据库保证同一定义的已发布区间不重叠 |
| `metric_definition_revision_dependencies.dependency_definition_id` | int64 | 依赖指标稳定身份 |
| `metric_definition_revision_dependencies.dependency_revision_id` | int64? | 发布时冻结的依赖修订 |
| `metric_definition_revision_dependencies.relation_kind` | string | `base` / `component` |
| from_metric_id / to_metric_id | int64 | 依赖关系方向 |
| coefficient | float? | 权重系数（可选） |

### `standard.documents` — 标准文档稳定身份

| 字段 | 类型 | 说明 |
|------|------|------|
| code | string | Tenant 内唯一且不可变的文档编码 |
| scope_type | string | `platform` / `tenant_common` / `domain`；租户公开写接口只允许后两者 |
| owner_domain_id | int64? | 归属业务域；仅 `scope_type=domain` 时必填 |
| doc_type | string | `national` / `industry` / `internal` / `reference` |
| source_org | string | 来源机构 |
| draft_revision_id | int64? | 当前唯一草稿或审核中修订指针 |
| version | int64 | 稳定身份乐观锁版本，不是文档业务版次 |
| lifecycle_state | string | `active` / `deleting` |

### `standard.document_revisions` — 标准文档修订

| 字段 | 类型 | 说明 |
|------|------|------|
| document_id / revision_no | int64 | 文档稳定身份 / 业务修订号，组合唯一 |
| status | string | `draft` / `in_review` / `published` / `withdrawn` |
| name / version_label | string | 本修订文档名 / 来源文档标注版次 |
| publish_date / description | date? / text | 来源发布日期与说明 |
| file_key / file_name / file_size | mixed | 本修订不可变文件快照；对象存储 bucket 为 `standard` |
| media_type / content_sha256 | string | 文件媒体类型与内容摘要 |
| change_summary | text | 本次修订说明 |
| effective_from / effective_to | timestamp? | 半开生效区间 `[from,to)` |

### `standard.document_extractions`、`standard.document_extraction_candidates` 与 `standard.document_extraction_evidences`

- 提炼批次固定引用一个带 Markdown 文件的 `document_revision_id`；重复提炼新建批次，不覆盖历史。
- Copilot 仅返回 `glossary`、`element`、`code_set`、`metric` 候选及证据坐标；Standard 验证证据属于输入修订后持久化。
- Copilot 与 Standard 共用唯一候选数据类型词汇。数据元候选的 `data_type` 只允许 `string|int|bigint|float|decimal|date|datetime|bool|json|text`，码值集候选只允许 `string|int|bigint`，术语和指标候选必须为 `null`；`identifier` 属于业务语义，`numeric`、`date_or_datetime` 等模糊上位提示不是合法标准数据类型。数据元候选的 `value_domain_kind` 只允许 `unrestricted|range|enumeration`，其他候选必须为 `null`。Copilot 输出 Schema 先约束，Standard 在持久化前再次按候选类型拒绝非法字段；历史提炼批次保持不可变，契约修正后通过新提炼批次表达新结果。
- 候选状态固定为 `pending`、`retained`、`rejected`；处置使用候选自己的并发 `version`，`retained` 不会自动创建或发布正式标准。
- 每条证据保存章节、起止行、原文摘录与 SHA-256，始终引用确定的文档修订。
- `GET /documents/:id/extractions` 读取时动态返回候选比对投影，不将易失的匹配结果写入提炼历史。同类型、同编码是唯一确定匹配键；同名不同编码不自动判重。比对结果固定为 `new`、`exact`、`content_conflict`、`scope_conflict`；每项差异同时返回字段、候选值和当前标准值，供治理人员直接核对。
- 比对修订按“稳定身份当前草稿/审核中修订 → 当前生效已发布修订 → 最新历史修订”选择；只比较候选明确给出的字段，缺失字段不构造差异。范围先比较 `scope_type + owner_domain_id`，范围不一致统一为 `scope_conflict`。
- 指标候选中的聚合方式、维度等执行建模提示由 Model/Develop 消费，不属于 Standard 指标定义字段，因此不参与内容冲突判定。
- 候选比对只用于评审提示和打开现有标准，不自动建立文档关联、不创建标准稳定身份或修订，也不改变 `pending / retained / rejected` 人工处置主线。

### `standard.document_*_mappings` — 文档关联

文档与数据元、术语、指标的多对多关联，每条关联记录 `reference_location`（引用位置）。

## API 端点（`/api/v1/standard`）

### 业务域
```
GET/POST /api/v1/standard/domains
GET/PUT/DELETE /api/v1/standard/domains/:id
```

### 标准集
```
GET/POST /api/v1/standard/collections
GET/DELETE /api/v1/standard/collections/:id
GET/POST /api/v1/standard/collections/:id/revisions
GET /api/v1/standard/collections/:id/events
PUT /api/v1/standard/collections/:id/revisions/:revision_id
POST /api/v1/standard/collections/:id/revisions/:revision_id/submit
POST /api/v1/standard/collections/:id/revisions/:revision_id/return
POST /api/v1/standard/collections/:id/revisions/:revision_id/publish
PUT /api/v1/standard/collections/:id/assignments
GET /api/v1/standard/collection-user-candidates
```

### 业务术语
```
GET/POST /api/v1/standard/glossaries                 # 创建稳定身份时同时创建首个草稿
GET/PUT/DELETE /api/v1/standard/glossaries/:id       # PUT 只更新适用范围、归属域、责任人和标签
GET/POST /api/v1/standard/glossaries/:id/revisions
GET/PUT /api/v1/standard/glossaries/:id/revisions/:revision_id
POST /api/v1/standard/glossaries/:id/revisions/:revision_id/submit
POST /api/v1/standard/glossaries/:id/revisions/:revision_id/return
POST /api/v1/standard/glossaries/:id/revisions/:revision_id/publish
POST /api/v1/standard/glossaries/:id/revisions/:revision_id/withdraw
GET/PUT /api/v1/standard/glossaries/:id/elements      # 关联数据元稳定身份
```

### 数据元
```
GET/POST /api/v1/standard/elements             # 创建稳定身份时同时创建首个草稿
GET/PUT/DELETE /api/v1/standard/elements/:id   # PUT 只更新归属域、责任人和标签
GET/POST /api/v1/standard/elements/:id/revisions
GET/PUT /api/v1/standard/elements/:id/revisions/:revision_id
POST /api/v1/standard/elements/:id/revisions/:revision_id/submit
POST /api/v1/standard/elements/:id/revisions/:revision_id/publish
POST /api/v1/standard/elements/:id/revisions/:revision_id/withdraw
GET /api/v1/standard/elements/:id/quality-rules # 只返回当前发布修订的编译规则
```

### 码值集
```
GET/POST /api/v1/standard/code-sets
GET/PUT/DELETE /api/v1/standard/code-sets/:id   # PUT 只更新归属域和责任人
GET/POST /api/v1/standard/code-sets/:id/revisions
GET/PUT /api/v1/standard/code-sets/:id/revisions/:revision_id
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/submit
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/publish
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/withdraw
GET/POST /api/v1/standard/code-sets/:id/revisions/:revision_id/items
PUT/DELETE /api/v1/standard/code-sets/:id/revisions/:revision_id/items/:item_id
```

### 计量单位
```
GET/POST /api/v1/standard/measurement-categories
PUT/DELETE /api/v1/standard/measurement-categories/:id
GET/POST /api/v1/standard/units
GET/PUT/DELETE /api/v1/standard/units/:id
```

### 指标
```
GET/POST /api/v1/standard/metric-categories
PUT/DELETE /api/v1/standard/metric-categories/:id

GET/POST /api/v1/standard/metrics
GET/PUT/DELETE /api/v1/standard/metrics/:id
GET/POST /api/v1/standard/metrics/:id/revisions
GET/PUT /api/v1/standard/metrics/:id/revisions/:revision_id
POST /api/v1/standard/metrics/:id/revisions/:revision_id/submit
POST /api/v1/standard/metrics/:id/revisions/:revision_id/return
POST /api/v1/standard/metrics/:id/revisions/:revision_id/publish
POST /api/v1/standard/metrics/:id/revisions/:revision_id/withdraw

GET /api/v1/standard/catalog-resources/changes
POST /api/v1/standard/runtime/catalog-references/resolve
```

### 标准文档
```
GET/POST /api/v1/standard/documents
GET/PUT/DELETE /api/v1/standard/documents/:id # PUT 只更新适用范围、归属域、类型和来源机构
GET/POST /api/v1/standard/documents/:id/revisions
GET/PUT /api/v1/standard/documents/:id/revisions/:revision_id
POST /api/v1/standard/documents/:id/revisions/:revision_id/submit
POST /api/v1/standard/documents/:id/revisions/:revision_id/return
POST /api/v1/standard/documents/:id/revisions/:revision_id/publish
POST /api/v1/standard/documents/:id/revisions/:revision_id/withdraw
POST /api/v1/standard/documents/:id/revisions/:revision_id/file # 上传或替换草稿修订文件
GET /api/v1/standard/documents/:id/revisions/:revision_id/file # 下载确定修订文件
POST /api/v1/standard/documents/:id/revisions/:revision_id/extractions # Copilot 提炼
GET /api/v1/standard/documents/:id/extractions # 候选附带当前 Standard 确定性比对投影
PUT /api/v1/standard/document-extraction-candidates/:candidate_id # retained/rejected 人工处置
GET/PUT /api/v1/standard/documents/:id/mappings # 多维关联（数据元/术语/指标）
```

## 前端路由

```
/standard/domains                    # 业务域（树形管理）
/standard/collections                # 标准集列表
/standard/collections/:id            # 标准集配置、职责、修订与审核事件
/standard/glossaries                 # 业务术语列表
/standard/glossaries/:id             # 术语详情（属性、关联数据元、文档）
/standard/elements                   # 数据元列表
/standard/elements/:id               # 数据元详情（规格、质量规则、文档）
/standard/code-sets                  # 码值集列表
/standard/code-sets/:id              # 码值集详情（码值项管理）
/standard/units                      # 计量单位（含度量类别）
/standard/metrics                    # 指标列表
/standard/metrics/:id                # 指标详情（依赖关系、关联数据元）
/standard/documents                  # 标准文档库
```

## 模块依赖关系

**依赖**:
- **System 模块**: JWT 认证；标准集职责候选与人员状态通过 `tenant.standard_runtime` 的 `iam.tenant_membership.read` 服务身份解析（`SYSTEM_URL`）
- **Model 模块**: 删除业务域、数据元和指标定义前冻结 Model 标准引用删除屏障并执行权威影响扫描（`MODEL_URL`）；维度层级是 Model 本地聚合
- **Copilot 模块**: Standard 使用当前 Tenant 的 `addp-standard` Service Access Token 调用候选提炼端点（`COPILOT_URL`）；Copilot 不回写 Standard 数据库
- **MinIO**: 标准文档文件存储（bucket: `standard`）

**被依赖**（其他模块调用 Standard 的 API）:
- **Model 模块**: 验证 domain_id、element_id / element_revision_id、metric_definition_revision_id；维度层级只校验 Model 本地事实

`/api/v1/standard` 路由只接受 canonical Bearer Tenant AuthContext；文档下载还可使用 Standard owner 的 Browser Resource Ticket。Catalog 对 Domain、Glossary、Element 语义引用的校验只走 `/api/v1/standard/references/resolve`；Metric 目录来源只走 `/catalog-resources/changes` 与 `/runtime/catalog-references/resolve`，两类契约不可混用，也不提供面向 Asset 的自动发现接口。

## IAM Permission 所有权

Standard 是以下第一批 Permission 的唯一 owner：

- `standard.domain.*`
- `standard.collection.*`
- `standard.collection_assignment.update`
- `standard.element.*`
- `standard.metric.*`
- `standard.catalog.read`（仅 `addp-catalog` 的 `tenant.catalog_runtime`）
- `standard.code_set.*`
- `standard.document.*`
- `standard.document_extraction.create`
- `standard.glossary.*`
- `standard.unit.*`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Standard 服务启动时不向 System 动态注册 Permission。

Measurement Category 是 Unit 聚合内子资源。DimensionHierarchy 与层级成员由 Model 独占拥有。Document 与 Element、Glossary、Metric 的关联操作按涉及资源 Permission 做 all-of 校验，不借用宽泛 Key 或前缀匹配授权。

## 特殊设计

### 指标三种类型关系

```
atomic（原子指标）
  └─ 不包含语义依赖；物理来源由 Model MetricImplementation 定义

derived（派生指标）
  └─ 修订中必须且只能包含一个 base 依赖，发布时冻结其已发布修订

composite（复合指标）
  └─ 修订中包含一个或多个 component 依赖，发布时冻结各依赖修订
```

### 业务术语、数据元、码值集、指标定义与标准文档的修订状态流转

```
draft → in_review → published → withdrawn
   ↑         │
   └─────────┘
```

- `draft` 是唯一可编辑状态；提交审核后不可继续修改，退回时恢复为同一草稿。
- `published` 表示审核通过，业务定义不可修改；后续变更从最新修订复制为下一草稿。
- 当前生效修订不是持久化指针。Standard 按 `[effective_from, effective_to)` 和请求的 `as_of` 动态解析，未传 `as_of` 时使用服务端当前时间。
- 同一稳定身份的已发布修订生效区间不得重叠。发布新修订时，服务端可以在同一事务中把前一条开放区间的 `effective_to` 收口到新修订的 `effective_from`；除此之外不得修改已发布定义。
- `withdrawn` 用于撤回错误发布，不代表创建新版本；稳定身份仍保留历史。
- 业务术语、数据元、码值集、指标定义与标准文档稳定身份各自最多持有一个草稿，可以有多个区间不重叠的已发布修订。

### 标准集审核与职责边界

- 创建者自动成为首位 `owner`；职责替换必须至少保留一名 `owner`。
- `owner` 可维护草稿并管理职责，`maintainer` 可维护和提交，`reviewer` 可退回和发布；提交人与发布人必须不同。
- 提交前必须至少有一个成员，并配置一名不同于提交人的审核人。
- 集合修订只冻结集合名称、说明和成员稳定身份，不冻结或代替成员对象自己的发布修订。
- 状态变化与职责替换在业务事务中追加 `standard_collection_events`，不以可变状态字段冒充审核历史。

### 值域与质量规则的单一事实源

数据元修订必须在 `unrestricted`、`range`、`enumeration` 中三选一。`range` 只使用结构化 `range_constraint`；`enumeration` 必须绑定具体 `code_set_revision_id`。两者互斥，且绑定的码值集修订必须已经发布、值类型必须与数据元类型相容，码值集修订生效区间必须覆盖数据元修订生效区间。

发布数据元修订时，Standard 从 `nullable`、长度、格式、连续值域和固定码值集修订确定性编译质量规则，再合并不重复的 `extra_quality_rules`。下游 Quality 消费指定时点生效修订的 `compiled_quality_rules` 快照，不允许调用方再维护第二份 `allowed_values`。

### 数据字典边界

数据字典不是 Standard 内的持久化主资源。它由 Meta 的物理结构事实、Catalog 的语义关联和 Standard 在查询时点生效的数据元/码值解释组合形成。Standard 只提供标准数据元目录和按时点解析已发布修订的能力。

Standard 通过唯一 `POST /api/v1/standard/runtime/element-revisions/resolve` 契约按同一 `as_of` 批量返回精确数据元修订和可选的精确码值集修订；Catalog 数据字典与 Model 审批冻结必须共用该契约，不得分别通过列表接口猜测当前修订。

### 文档关联的多维设计

同一篇文档可同时关联：
- 多个数据元（`document_element_mappings`）
- 多个术语（`document_glossary_mappings`）
- 多个指标（`document_metric_mappings`）

每条关联记录 `reference_location`（在文档中的引用位置），便于溯源。

### 无跨 Schema 外键

Standard 的 `elements`、`units`、`code_sets` 等被其他 Schema（model、metadata 等）引用时，**没有数据库级外键约束**，通过应用层 HTTP 调用 Standard API 进行 ID 存在性验证。

Model 可引用的 Domain、Element 和 Metric 使用独立 `lifecycle_state=active|deleting`。硬删除时 Standard 先写入删除协调记录并进入 `deleting`，再由删除协调流程串行锁定 Standard 资源行，调用 Model 冻结 `(tenant_id, resource_type, resource_id)` 屏障并权威扫描引用；有引用时必须先恢复 Model `open`，再恢复 Standard `active` 并返回 `409 standard_resource_referenced`；无引用时才硬删除资源，协调记录保留到 Model 屏障终止为 `deleted` 成功。资源行锁覆盖冻结、扫描和本地删除，用户重试与后台补偿复用同一协调记录；后台定期补偿 `deleting` 或资源已删除但终态未收敛的协调记录。普通 check-then-delete、自动清空 Model 引用、跨 Schema 查询和绕过屏障的强制删除都不是合法路径。

### 数据库约束与启动收敛

Standard 当前使用单一启动迁移入口 `repository.Migrate`：在同一个 PostgreSQL advisory transaction lock 内，先由 GORM `AutoMigrate` 维护表和字段，再幂等收紧唯一索引与 CHECK 约束。约束冲突必须阻止服务启动并暴露具体失败语句，不允许自动删除、合并或改写存量业务数据。

租户资源的编码唯一性以 `(tenant_id, code)` 为准；聚合子资源和映射表使用完整业务键去重。Standard Schema 内部引用可以使用数据库约束，跨 Schema 引用仍只允许通过事实 owner 的 API 校验。

## 开发注意事项

1. **新增资源**: `models` → `repository` → `service` → `handler` → `router.go` → `main.go`

2. **文档上传**: 文件写入 MinIO 的 `standard` bucket，`file_key` 格式为 `documents/{id}/{filename}`，下载时通过 `file_key` 从 MinIO 读取流。

3. **重启服务**:
   ```bash
   bash scripts/dev/restart.sh -standard
   ```

4. **树形结构查询**: Domain 和 MetricCategory 均为树形结构，查询时通过递归或应用层组装。

5. **`DocumentPanel` 组件**: 前端复用组件，用于在各详情页嵌入文档关联管理，避免重复实现。

6. **前端 API 统一入口**: 所有 API 调用集中在 [frontend/src/api/standard.js](frontend/src/api/standard.js)。

## 前端公开路由

- 模块内 Router 使用 `/domains`、`/glossaries`、`/elements`、`/code-sets`、`/metrics` 等无模块前缀路径；Console 公开 URL 统一加 `/standard` 前缀。
- 术语、数据元、码集和指标详情使用 `/:id` 表达对象身份；详情返回使用明确列表路由，不依赖 `router.back()`。
- 创建成功进入详情使用 `replace`，列表进入详情和跨标准对象导航使用 `push`。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。
