# Standard 模块 CLAUDE.md

本文件为 Claude Code 在 `standard/` 目录下工作时提供指导。

## 阅读导航

| 阅读目的 | 现行正文 |
| --- | --- |
| 模块职责与事实归属 | [模块概述](#模块概述)、[边界与读取契约](#边界与读取契约) |
| API 与机器读取入口 | [API 端点](#api-端点apiv1standard)、[企业目录机器读取](#企业目录机器读取)、[数据字典边界](#数据字典边界) |
| 权限与跨模块调用 | [IAM Permission 所有权](#iam-permission-所有权)、[辅助引用与权限隔离](#辅助引用与权限隔离)、[模块依赖关系](#模块依赖关系) |
| 数据结构与修订规则 | [当前数据库表结构](#当前数据库表结构)、[稳定身份与修订规则](#稳定身份与修订规则)、[指标三种类型关系](#指标三种类型关系) |
| 前端路径与页面状态 | [公开路径清单](#前端路由)、[导航与页面交互](#前端公开路由) |
| 迁移与文件生命周期 | [迁移与跨模块边界](#迁移与跨模块边界)、[数据库约束与启动收敛](#数据库约束与启动收敛)、[文档文件生命周期](#文档文件生命周期) |
| 测试入口与证明范围 | [标准测试入口](#标准测试入口与范围)、[文档候选治理验证](#文档候选治理验证范围)、[文件生命周期验证](#文档文件验证范围) |
| 企业目录接入 | [业务域总览与五类标准接入](#业务域总览与五类标准接入) |

导航仅定位正文；权限、独有约束与已验收范围分别按所引章节认定。

## 边界与读取契约

篇内导航：[事实归属与质量规则](#事实归属与质量规则)、[数据元修订与机器读取](#数据元修订与机器读取)、[辅助引用与权限隔离](#辅助引用与权限隔离)、[指标关系读取](#指标关系读取)。

### 事实归属与质量规则

Standard 定义 Domain、Glossary、Element、MetricDefinition、CodeSet、Unit 和标准来源文档等可复用业务语义，但不拥有这些语义与具体 DataItem、CatalogEntry 或 CatalogComponent 的应用关系。具体字段/组件到标准修订的映射只由 Catalog 保存；检查方案、规则来源快照、执行结果和问题只由 Quality 保存。Standard 不依赖 Meta 或 Catalog，不保存 `catalog_entry_id`、反向资源列表或质量执行事实。安全分类、安全等级、敏感类型和保护基线统一属于 Security，Standard 不保存第二份安全事实。

安全定义的现行对象与版本约束见[Security 业务对象](../docs/spec/addp数据安全与隐私保护实现规范.md#四security-业务对象)，变更及引用删除限制见[保护定义变化的影响传播](../docs/spec/addp数据安全与隐私保护实现规范.md#73-保护定义变化的影响传播)；旧 Standard 结构与 ID 的处理统一见[迁移与旧路径删除](../docs/spec/addp数据安全与隐私保护实现规范.md#十五迁移与旧路径删除)。

数据元修订 `compiled_quality_rules` 的结构和校验必须遵守 [ADDP 数据质量规范](../docs/spec/addp数据质量规范.md)。规则仅由值约束编译，`rule_key` 由 Standard 按数据元稳定 ID 与约束类型生成并跨修订保持；Standard 不拥有物理字段应用、规则执行、评分或质量问题。

### 数据元修订与机器读取

数据元精确修订 GET 返回 ElementRevisionDetail：在原修订内容上提供稳定 element_code 和按 code_set_revision_id 解析的码值集完整只读快照。历史已撤回码值集仍按确定 ID 展示，不改用当前生效修订；权限仍为 standard.element.read，跨租户及不匹配的数据元/修订拒绝。精确读取失败显示错误且不替换为默认修订。Quality 管理界面可读取该投影追溯来源，worker 不依赖它。URL 定位、导航与未保存保护见[前端公开路由](#前端公开路由)。

数据元详情无论默认打开还是通过 `revision_id` 定位，均读取选中修订的精确详情。关联码值集的名称、编码、版次和状态来自该修订快照，不以可选候选反推历史引用；绑定快照缺失或身份不匹配时明确报错。只读状态不请求码值集候选，查看历史不额外要求 `standard.code_set.read`；可编辑草稿仍按类型与生效时间提供已发布候选，已绑定但不再可选的修订仅保留显示，不允许重新选入。选择其他候选后不显示旧快照的状态。

Catalog 映射的修订冻结与历史读取以[StandardMapping 正式契约](../docs/spec/addp企业资源目录实现规范.md#standardmapping-展示与精确读取)为准；Standard 的两类机器批量入口、调用权限与验证边界统一见[数据字典边界](#数据字典边界)。

### 辅助引用与权限隔离

#### 单位选项

数据元详情的单位选项仅在当前用户具有 `standard.unit.read` 时读取，不把单位读取权限加入数据元页面准入条件。缺少权限时不请求单位列表、不展示缓存的单位名称或裸 ID，不提供单位选择或清除操作；已有绑定显示权限提示，未绑定显示空值。查看修订和保存其他字段仍保留原 `unit_id`。辅助选项加载跟随当前授权上下文，权限变化立即清空旧选项，迟到响应和离页后的响应不得恢复旧数据；服务器实际返回的权限错误继续沿用统一错误处理，不以静默吞掉 403 代替请求前判权。

数据元和指标的单位选择复用 Standard `UnitSelect`，以 Element Plus 树形选择器按计量单位自带的度量类别组织。默认收拢分类，已选单位所在分类自动展开；分类只负责展开和收拢，只有单位叶节点可以选中。搜索支持分类名、单位名称和符号，并自动展开匹配结果；清空选择保存为 `null`。交互测试纳入已有 `make test-standard-frontend` 与平台 CI 的 Standard 前端门禁。

#### 平台与租户单位维护

平台目录由具备 `standard.unit.*（Platform Scope）` 权限的 Platform User 在计量单位页面维护，所有 Tenant 直接只读引用，不复制默认项。

API 使用现有 `/measurement-categories`、`/units` 资源路径，Platform 与 Tenant Context 分别按对应 Permission 访问；Tenant 写入只允许自己的自定义资源，不能修改平台项。平台写入只允许平台资源。平台译名管理要求默认中文名称与英文名称，语言切换无需刷新或重启。

#### 关联文档读取

数据元、术语和指标详情共用 `DocumentPanel`。关联文档读取按后端 all-of 契约同时检查当前对象读取权限与 `standard.document.read`，不将文档权限加入专业详情页面准入条件。缺少读取权限时不发关联列表或文档候选请求，显示无权查看；加载失败显示错误并提供只读重试，不伪装成暂无关联文档。对象或授权上下文变化时立即清空旧列表与候选，关闭旧关联弹窗；过期响应及离页后的响应不得恢复内容。只有读取成功且列表为空才显示空态。文档创建、关联、上传和解除仍遵循各自既有写权限契约。

### 指标关系读取

Metric 的指标依赖与基准指标关系通过当前 User Token 读取 `GET /metrics/:id/relations` 一跳图；它要求 `standard.metric.read`，只读 Standard 本地事实，不调用 Catalog 或 Model，也不使用 `standard.catalog.read` 机器权限替代用户权限。数据元、Domain、指标分类和单位继续留在 Metric 专业详情中，本阶段不伪造为企业目录节点。实际路由与修订入口见[指标 API 清单](#指标)。边类型的规范与显示差异沿[企业目录与标准映射剩余工作](../docs/next/ADDP企业资源目录能力专题.md#31-企业目录与标准映射)维护，命名统一须先确认。

## 模块概述

**Standard 模块** 是 ADDP 平台的数据标准和治理中心，负责：

- 业务域（Domain）树形组织管理
- 业务术语（Glossary）：以稳定身份和不可变修订统一业务语言、别名、定义、生效区间与关联数据元
- 数据元（Element）：数据标准的核心原子对象，定义数据规格和质量规则
- 码值集（CodeSet）：系统/自定义码值集及码值项
- 计量单位（Unit）：按度量类别组织的计量单位
- 指标定义（MetricDefinition）：指标业务含义、统计口径、单位、责任归属和适用范围；不保存模型粒度、连接、过滤或可执行表达式
- 标准文档（Document）：标准来源及其不可变文档修订、提取证据和审核关系

维度层级、公共/一致性维度和指标实现统一属于 Model。`standard.dimension_hierarchies` 旧表、API、权限和前端路由已经删除；Standard 指标以稳定身份和不可变语义修订维护，具体字段见[当前数据库表结构](#当前数据库表结构)，旧实现字段由[启动迁移](#数据库约束与启动收敛)清理，不得据此继续扩展 Standard。

**端口**:
- 后端: `8110`（环境变量 `STANDARD_BACKEND_PORT`）
- 前端: `5181`（开发环境）

**数据库 Schema**: `standard`

**外部存储**: MinIO（存储标准文档文件，bucket 名为 `standard`）

独立前端根路径在 AuthContext 加载后，按 Console 菜单顺序 `/domains`、`/glossaries`、`/elements`、`/code-sets`、`/units`、`/metrics`、`/documents` 选择第一个符合页面准入条件的地址；没有可访问页面时进入 `/forbidden`。登录后的默认返回地址为根路径。显式列表或详情地址继续只按自身权限判断，不改跳其他标准页面。

## 标准规则与迁移约束

篇内导航：[稳定身份与修订规则](#稳定身份与修订规则)、[迁移与跨模块边界](#迁移与跨模块边界)、[文档文件生命周期](#文档文件生命周期)。

### 稳定身份与修订规则

- 业务域只表达业务语义与治理责任，不表达可见范围、审核容器或目录分类。标准对象独立维护修订和发布流程，不再建立标准成员清单的独立治理容器。
- 业务域、业务术语、数据元、码值集、指标定义、标准文档和标准分类的稳定编码统一使用小写 `snake_case`，在对应 Tenant 和类型内唯一且创建后不可变；创建入口不自动转换非法编码。归属域和适用范围可以独立调整，所以业务域编码不是正式标准编码的强制前缀；该前缀只约束 Copilot 新候选。
- 上述稳定身份和分类的创建 API 必须在 Repository 写入前校验编码；不合规时统一返回 HTTP 400、`error_code=invalid_standard_code`。候选正式化同样先校验历史候选编码，禁止绕过公开创建约束。
- 手工新建业务术语、数据元、码值集、指标定义和标准文档（含新建并关联文档）不提交 `change_summary`，后端按请求语言生成并保存“初始创建 / Initial creation”作为 R1 的说明；新建表单不展示该字段，后续修订仍必填变更说明。候选正式化继续保留其来源与处置说明。
- 发布型标准采用“稳定身份 + 不可变修订”；统一状态为 `draft → in_review → published → withdrawn`，按半开生效区间动态解析当前修订。
- 标准对象显式保存 `scope_type=platform|tenant_common|domain`；仅 `domain` 必须指定 `owner_domain_id`。码值集不得再以“租户自定义”为由强制归属业务域。
- Copilot 只生成标准候选，必须保留文档修订、页码/章节/行号/文本片段等来源证据。Standard 保存提炼批次、候选、人工处置和候选正式化事实；`retained` 只表示保留为后续建标输入。正式化由人工另行发起，只能创建 R1 草稿、为既有身份创建新草稿，或关联内容一致的既有修订，绝不能提交审核或发布。

### 迁移与跨模块边界

- 范围模型只保留 `scope_type + owner_domain_id`；启动迁移遇到历史 `domain_id` 时必须一次性回填归属和范围并删除旧列，即使新旧列曾同时存在，也不得保留双轨字段。
- Element、CodeSet、MetricDefinition、Glossary、Document 的 Scope、归属域和修订生命周期统一；DimensionHierarchy 已整体迁入 Model。

映射审核以[StandardMapping 契约](../docs/spec/addp企业资源目录实现规范.md#standardmapping-候选与审核)为准；规则、方案和执行归属沿[Quality 目标与边界](../docs/spec/addp数据质量规范.md#1-目标与边界)，目录中的质量结果展示沿[Quality 当前摘要动态关联](../docs/spec/addp企业资源目录实现规范.md#58-quality-当前摘要动态关联)。未验收的人工候选、审核、字典与认证版本证据仍沿[企业目录与标准映射剩余工作](../docs/next/ADDP企业资源目录能力专题.md#31-企业目录与标准映射)维护，实际覆盖范围沿[Catalog 验证导航](../catalog/CLAUDE.md#验证导航)分别认定。

### 文档文件生命周期

文档文件采用“新对象上传、数据库切换引用、旧对象补偿清理”的顺序。失效对象记录在 `standard.document_file_cleanups`，该表仅用于物理清理重试，不作为文档当前文件引用。

文件写入 MinIO 的 `standard` bucket，`file_key` 格式为 `tenant_{tenant_id}/documents/{id}/revisions/{revision_id}/{uuid}{extension}`，下载时按确定修订的 `file_key` 从 MinIO 读取流。

上传要求 `standard.document.update`，以 multipart 提交 `file` 和当前文档并发 `version`，只允许替换草稿修订文件；修订 ID 与文档版本分别表达目标和写入并发条件。下载要求 `standard.document.read`，按同租户的文档与确定修订读取文件；浏览器原生资源票据沿[统一认证规范](../docs/spec/addp登录认证的统一要求.md#23-browser-resource-access-ticket)执行，不替代上传或普通业务 API 的认证。

文档上传大小和 MinIO 操作超时由部署配置 `STANDARD_DOCUMENT_MAX_FILE_SIZE`、`STANDARD_DOCUMENT_STORAGE_TIMEOUT` 控制。路径见[标准文档 API 清单](#标准文档)，用例与未覆盖范围见[文档文件验证范围](#文档文件验证范围)。

## 目录结构

以下仅列关键职责入口；表与接口契约分别见[当前数据库表结构](#当前数据库表结构)、[API 端点](#api-端点apiv1standard)，权限所有权见[IAM Permission 所有权](#iam-permission-所有权)。测试文件的存在不代表已执行通过，标准入口、分层覆盖与未覆盖范围见[标准测试入口与范围](#标准测试入口与范围)。

```
standard/
├── authorization/
│   └── permissions.yaml              # Standard owner Permission Manifest
├── backend/
│   ├── cmd/server/main.go             # 配置、迁移、依赖组装与服务入口
│   ├── go.mod                         # github.com/addp/standard
│   └── internal/
│       ├── api/
│       │   ├── router.go              # 路由与 Permission 中间件挂载
│       │   ├── domain_handler.go
│       │   ├── glossary_handler.go
│       │   ├── element_handler.go
│       │   ├── code_set_handler.go
│       │   ├── unit_handler.go
│       │   ├── metric_handler.go
│       │   ├── document_handler.go
│       │   ├── catalog_resource_handler.go             # 目录专业来源读取
│       │   ├── domain_standard_reference_handler.go    # 域级标准引用
│       │   └── element_revision_resolution_handler.go # 数据元修订批量解析
│       ├── authorization/permissions_generated.go     # 由 Manifest 生成，不手工修改
│       ├── candidate/semantic.go      # 候选语义指纹与族快照令牌
│       ├── config/config.go
│       ├── models/
│       │   ├── domain.go
│       │   ├── glossary.go            # 另含 StringArray、Int64Array
│       │   ├── element.go             # 另含 JSONB
│       │   ├── code_set.go
│       │   ├── unit.go
│       │   ├── metric.go
│       │   ├── document.go
│       │   ├── catalog_resource.go    # 目录来源投影
│       │   └── professional_relation.go # 专业关系投影
│       ├── repository/                # 持久化、事务与约束
│       │   ├── schema.go              # 当前模块迁移入口
│       │   ├── temporal_revision.go   # 生效区间与管理历史读取
│       │   └── document_candidate_formalization_repository.go # 候选正式化事务
│       └── service/                   # 业务校验与用例编排
│           ├── document_service.go    # 文档、修订、提炼与候选处置
│           ├── document_candidate_group_service.go # 候选聚合、族分页与裁决历史
│           ├── document_storage.go    # 文档对象存储接口与 MinIO 适配
│           ├── element_revision_resolution_service.go # 数据元修订解析
│           └── professional_relation_service.go       # 一跳专业关系读取
└── frontend/
    ├── src/
    │   ├── api/
    │   │   ├── standard.js            # Standard 业务 API 调用
    │   │   ├── client.js              # 共享业务客户端接入与 Blob 错误处理
    │   │   └── auth.js                # System 认证 API 适配
    │   ├── router/index.js            # 页面路由与共享认证、准入接入
    │   ├── store/auth.js              # 共享认证 Store 接入
    │   ├── views/
    │   │   ├── DomainList.vue
    │   │   ├── GlossaryList.vue
    │   │   ├── GlossaryDetail.vue
    │   │   ├── ElementList.vue
    │   │   ├── ElementDetail.vue
    │   │   ├── CodeSetList.vue
    │   │   ├── CodeSetDetail.vue
    │   │   ├── UnitList.vue
    │   │   ├── MetricList.vue
    │   │   ├── MetricDetail.vue
    │   │   ├── DocumentList.vue
    │   │   ├── DocumentDetail.vue      # 文档修订、提炼与候选治理
    │   │   └── Login.vue
    │   ├── components/
    │   │   ├── Layout.vue             # 模块布局与导航
    │   │   ├── DocumentPanel.vue      # 通用文档关联面板
    │   │   ├── DDLPreviewDialog.vue
    │   │   ├── ElementDataTypeSelect.vue # 数据元类型选择
    │   │   └── UnitSelect.vue          # 单位引用候选选择
    │   ├── composables/
    │   │   ├── useStandardPermissions.js # 页面动作 Permission 投影
    │   │   └── useUnsavedChanges.js    # 未保存状态与离开确认
    │   └── utils/
    │       ├── standardRevision.js    # 管理展示与显式修订读取
    │       └── moduleNavigation.js    # Console 模块导航适配
    ├── tests/                         # 前端确定性测试
    └── e2e/standard-smoke.spec.js      # 受控 API 浏览器场景
```

## 当前数据库表结构

本节记录当前实现。目标模型以平台术语表和核心对象 ER 图为准；改造时直接替换旧字段与旧资源，不增加兼容字段或并行 API。

术语、数据元、码值集、指标定义和标准文档的管理聚合统一返回 `latest_revision`：最高修订号的完整内容投影，不受修订状态或生效区间限制；码值集包含码值项，指标包含修订依赖。`current_revision` 仍只表示指定时点实际生效的已发布修订，运行时解析及可选发布引用继续使用生效契约。页面选择与展示见[标准修订展示](#标准修订展示)。

### `standard.domains` — 业务域（树形）

业务域树与引用候选复用同一层级构建，按 `sort_order, id` 排列同级节点；引用解析和分页候选的 `domain_path` 是根到当前域的名称数组。候选在父域优先顺序上搜索和分页，完整路径不受分页边界影响。路径不持久化，损坏的父子关系不得静默提升为根节点。


| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点（支持多层级） |
| name / code | string | 显示名 / 英文标识符 |
| icon | string | 图标标识 |
| sort_order | int | 同层排序 |

### `standard.glossaries` — 业务术语稳定身份

| 字段 | 类型 | 说明 |
|------|------|------|
| code | string | Tenant 内唯一且不可变的术语编码 |
| scope_type | string | `platform` / `tenant_common` / `domain`；租户公开写接口只允许后两者 |
| owner_domain_id | int64? | 归属业务域；仅 `scope_type=domain` 时必填 |
| tags | StringArray | 标签 |
| draft_revision_id | int64? | 当前唯一草稿或审核中修订指针 |
| version | int64 | 稳定身份乐观锁版本，不是业务版次 |

读取聚合额外返回派生事实 `has_publication_history`。只有该值为 `false` 的从未发布术语才允许删除；Service/Repository 仍在同一事务内复核并删除身份及其草稿修订，存在 `published` 或 `withdrawn` 历史时返回 409。

删除按钮与提示见[术语删除提示](#术语删除提示)。

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

关联绑定稳定身份，不绑定展示修订。`GET /glossaries/:id/elements` 返回完整关联列表 `GlossaryElementReference[]`，不分页、不按发布状态、生效区间或身份生命周期隐藏既有关系；同租户隔离仍在查询中校验。名称、版次和修订状态取当前生效修订，无当前生效修订时取最高版次；无修订时名称为稳定编码、修订字段为空。`is_effective` 独立表达所展示修订是否当前生效，不与 `status=published` 混同，也不代表身份生命周期。PUT 继续只接受同租户 active 身份，使用术语聚合版本整体替换；失效身份必须显式解除，不能靠 GET 隐藏。删除旧 `PublishedElementReference` 消费路径，但跨模块已发布标准解析仍使用原有发布投影，不放宽其规则。

| 字段 | 类型 | 说明 |
|------|------|------|
| glossary_id / element_id | int64 PK | 复合主键，多对多关联 |

### `standard.elements` — 数据元稳定身份

| 字段 | 类型 | 说明 |
|------|------|------|
| code | string | Tenant 内唯一且不可变的标准编码 |
| scope_type | string | `platform` / `tenant_common` / `domain`；租户公开写接口只允许后两者 |
| owner_domain_id | int64? | 归属业务域；仅 `scope_type=domain` 时必填 |
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
| data_type | string | `string` / `int` / `bigint` / `float` / `decimal` / `date` / `datetime` / `bool` / `json` |
| length / precision_num / scale | int? | 与数据类型相容的表示约束 |
| nullable / default_value / format | bool/string | 可空、默认值和格式约束 |
| value_domain_kind | string | `unrestricted` / `range` / `enumeration` |
| range_constraint | JSONB | 连续值域结构；仅 `range` 使用 |
| code_set_revision_id | int64? | 枚举值域固定引用；仅 `enumeration` 使用 |
| unit_id | int64? | 计量单位引用 |
| compiled_quality_rules | JSONB | 发布时仅从数据元值约束编译的不可变规则快照 |
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
| tenant_id | int64? | 空为平台所有，非空为所属 Tenant |
| translations | JSON | 平台目录的多语种名称与描述（单位含符号） |

度量类别和单位采用平台目录与 Tenant 自定义数据的一条资源模型：`tenant_id IS NULL` 表示平台所有，正整数表示 Tenant 所有；不使用 Tenant=0。平台类别与单位以稳定 code 标识，译名保存在数据库 `translations`，按请求语言投影名称、描述和符号。Tenant 自定义名称保留原始输入。维护权限与页面行为见[平台与租户单位维护](#平台与租户单位维护)。

部署迁移在同一 schema lock 与事务内导入外部 JSON 初始目录，并使用已有 `standard.data_migrations` 登记一次性数据迁移；后续启动不覆盖平台用户修改，也不重新创建已删除的目录项。列表读取始终只读。历史 `is_system` 项收敛为平台共享身份，仍与初始目录完全一致的重复项合并并迁移数据元/指标修订的单位引用；已编辑的原内置项保留为 Tenant 自定义数据，自定义项与其他 Tenant 数据保持隔离。删除被引用的单位或类别由外键拒绝，不改变既有引用含义。

分层验证范围见[度量目录验证范围](#度量目录验证范围)。

### `standard.units` — 计量单位

| 字段 | 类型 | 说明 |
|------|------|------|
| category_id | int64 | 所属度量类别 |
| name / symbol | string | 单位名 / 符号（如 kg、℃） |
| tenant_id | int64? | 空为平台所有，非空为所属 Tenant |
| translations | JSON | 平台目录的多语种名称与描述（单位含符号） |

### `standard.metric_categories` — 指标分类（树形）

| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点 |
| name / code | string | 类别名 / 标识 |

### `standard.metric_definitions` / `standard.metric_definition_revisions` — 指标定义

指标定义使用“稳定身份 + 不可变修订”模型。稳定身份保存 `code`、`scope_type`、`owner_domain_id`、分类、标签和并发 `version`；修订保存 `metric_type`、名称、业务定义、统计口径、非引擎可执行的语义公式、单位、生效区间与统一发布状态。

`standard.metric_definition_revision_dependencies` 保存修订级语义依赖。草稿依赖 `dependency_definition_id`，发布时必须解析并冻结 `dependency_revision_id`；依赖类型与基数见[指标三种类型关系](#指标三种类型关系)。该表不保存模型表、字段、连接、过滤或可执行表达式。

公开路径继续以稳定产品资源名 `/metrics` 表示 MetricDefinition；修订使用 `/metrics/:id/revisions`，审核动作统一为 `submit`、`return`、`publish`、`withdraw`。旧 `approve`、`deprecate` 路径和扁平字段不保留。

| 字段 | 类型 | 说明 |
|------|------|------|
| `metric_definitions.category_id` | int64? | 所属指标分类 |
| `metric_definitions.scope_type / owner_domain_id` | string / int64? | 适用范围与归属业务域 |
| `metric_definitions.code` | string | 稳定英文标识 |
| `metric_definitions.tags` | StringArray | 标签 |
| `metric_definitions.draft_revision_id / version` | int64? / int64 | 工作修订指针与聚合并发版本 |
| `metric_definition_revisions.metric_definition_id / revision_no` | int64 / int | 所属指标稳定身份与业务修订号；不是聚合并发 `version` |
| `metric_definition_revisions.metric_type` | string | `atomic` / `derived` / `composite` |
| `metric_definition_revisions.name / definition / statistical_caliber` | string / text | 名称、业务定义与统计口径 |
| `metric_definition_revisions.semantic_formula` | text | 非引擎可执行的语义公式 |
| `metric_definition_revisions.unit_id` | int64? | 引用 `standard.units` |
| `metric_definition_revisions.status` | string | `draft` / `in_review` / `published` / `withdrawn` |
| `metric_definition_revisions.effective_from / effective_to` | timestamp? | 发布生效区间，数据库保证同一定义的已发布区间不重叠 |
| `metric_definition_revision_dependencies.metric_definition_revision_id` | int64 | 来源指标修订，依赖边的起点 |
| `metric_definition_revision_dependencies.dependency_definition_id` | int64 | 被依赖指标的稳定身份 |
| `metric_definition_revision_dependencies.dependency_revision_id` | int64? | 发布时冻结的依赖修订 |
| `metric_definition_revision_dependencies.relation_kind` | string | `base` / `component` |
| `metric_definition_revision_dependencies.coefficient` | float? | 权重系数（可选） |
| `metric_definition_revision_dependencies.note` | text | 语义依赖备注 |

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
| file_key / file_name / file_size | mixed | 本修订文件快照，草稿可替换，发布后保留历史内容；对象存储 bucket 为 `standard` |
| media_type / content_sha256 | string | 文件媒体类型与内容摘要 |
| change_summary | text | 本次修订说明 |
| effective_from / effective_to | timestamp? | 半开生效区间 `[from,to)` |

### `standard.document_extractions`、`standard.document_extraction_candidates`、`standard.document_extraction_evidences`、`standard.document_candidate_family_decisions` 与 `standard.document_candidate_formalizations`

生成与读取：[提炼批次与来源证据](#提炼批次与来源证据)、[候选编码与输入契约](#候选编码与输入契约)、[候选聚合与族分页](#候选聚合与族分页)、[实时比对与筛选口径](#实时比对与筛选口径)。

处置与正式化：[单项处置与代表版本](#单项处置与代表版本)、[候选族快照与胜出裁决](#候选族快照与胜出裁决)、[正式化、权限与历史保护](#正式化权限与历史保护)。

各主题用例与证明范围见[文档候选治理验证范围](#文档候选治理验证范围)；本节契约不代表当前版本、真实 Copilot 或完整浏览器链路已验收。

#### 提炼批次与来源证据

- 提炼批次固定引用一个带 Markdown 文件的 `document_revision_id`；重复提炼新建批次，不覆盖历史。
- Copilot 仅返回 `glossary`、`element`、`code_set`、`metric` 候选及证据坐标；Standard 验证证据属于输入修订后持久化。
- 每条证据保存章节、起止行、原文摘录与 SHA-256，始终引用确定的文档修订。

#### 候选编码与输入契约

- `domain` 范围文档提炼时，Standard 必须按 `owner_domain_id` 读取当前租户权威业务域编码并作为 `code_namespace` 传给 Copilot；业务域编码必须符合小写 `snake_case`，新候选的 `code` 以及枚举引用的 `code_set_code` 必须属于 `<code_namespace>_` 前缀。`tenant_common` 与 `platform` 文档不附加领域前缀。这是候选生成约束，不是正式标准稳定编码的业务域前缀约束。Standard 不自动转换不合规业务域编码，也不从文档名称猜测命名空间。
- Standard 同时从该文档稳定身份的全部历史提炼中构造最多 200 条 `known_candidates`，每条只包含 `candidate_type + code + name + definition`。仅编码符合当前命名空间的历史候选可以进入提示，按类型与编码去重，并依次优先选择已正式化、已保留、待裁决、已驳回候选中的最近出现内容；该读取使用轻量身份投影，不加载证据原文或完整候选载荷。Copilot 对同一概念必须优先复用已知编码。该列表只是生成约束上下文，不是新的持久化事实，也不得用于模型相似度合并。Copilot 在返回前校验命名空间，Standard 在持久化边界再次校验；任一候选越界时整批拒绝，禁止静默丢弃越界项。
- Copilot 与 Standard 共用唯一候选数据类型词汇。数据元候选的 `data_type` 只允许 `string|int|bigint|float|decimal|date|datetime|bool|json`，码值集候选只允许 `string|int|bigint`，术语和指标候选必须为 `null`；`identifier` 属于业务语义，`numeric`、`date_or_datetime` 等模糊上位提示不是合法标准数据类型。
- 数据元候选的 `value_domain_kind` 只允许 `unrestricted|range|enumeration`，其他候选必须为 `null`。
- 枚举数据元候选必须通过 `code_set_code` 引用同一提炼批次中唯一的码值集候选；非枚举数据元以及其他候选不得携带该字段。
- 候选引用只使用稳定编码，正式数据元修订仍由 Standard 按生效时点选择并冻结具体 `code_set_revision_id`。
- Copilot 输出 Schema 先约束，Standard 在持久化前再次校验字段适用性和批次内引用闭包；历史提炼批次保持不可变，契约修正后通过新提炼批次表达新结果。

#### 候选聚合与族分页

- 文档候选治理页面只消费跨提炼批次的候选聚合视图，不再逐批平铺原始候选。聚合项以 `candidate_type + code + normalized(name, definition, payload)` 的 SHA-256 指纹确定；字符串折叠空白，维度去重排序，码值项按编码、名称、定义排序。同类型、同编码但规范化内容不同的候选必须分组展示，禁止用模型相似度自动合并。
- 聚合视图不持久化，也不是新的聚合根。每个聚合项返回一个代表候选和按时间倒序排列的全部出现记录；出现记录保留候选 ID、提炼批次、文档修订、状态、版本、证据和正式化事实。存在正式化事实时聚合状态为 `formalized`；否则由最近一次已人工裁决的同义候选决定 `retained|rejected`；从未裁决时为 `pending`。人工动作只作用于代表候选，不批量回写其他原始候选。
- 候选治理公开读取在聚合项之上按 `candidate_type + code` 形成候选族。候选族只是可折叠的只读组织投影，不合并语义变体、不持久化，也不改变单个候选的身份；族内每个聚合项继续以自己的语义指纹和代表候选为裁决成员。分页必须在候选族形成后执行，禁止前端对单页聚合项做不完整分组。

#### 实时比对与筛选口径

- `GET /documents/:id/extraction-candidate-families` 是唯一候选治理读取入口，支持按聚合状态、候选类型、关键词和实时比对结果分页筛选；旧的 `/extraction-candidate-groups` 不再公开。
- `keyword` 对每个语义变体代表候选的编码或名称执行大小写不敏感、空白规范化后的子串匹配，`comparison_result` 固定为 `new|exact|content_conflict|scope_conflict`，全部筛选都在候选族形成和分页前作用于语义变体，族内只返回命中变体。
- `total` 表示筛选后的候选族总数，`variant_total` 表示筛选后的语义变体总数；
- `variant_status_counts` 始终统计文档全部语义变体、不受任何筛选影响；`family_comparison_counts` 按聚合状态、候选类型和关键词过滤后的候选族统计 `all` 及四类实时比对结果，但不应用当前 `comparison_result`。
- 同一候选族可因不同变体同时计入多个比对分类，各分类不要求相加等于 `all`。
- 标准比对必须按当前标准修订动态计算。响应中的族内变体继续返回代表候选、全部出现记录和动态标准比对。
- 旧的逐批平铺 `GET /documents/:id/extractions` 不再公开。
- 同类型、同编码是标准比对的唯一匹配键；同名不同编码不自动判重。每项差异同时返回字段、候选值和当前标准值，供治理人员直接核对。
- 比对修订按“稳定身份当前草稿/审核中修订 → 当前生效已发布修订 → 最新历史修订”选择；只比较候选明确给出的字段，缺失字段不构造差异。范围先比较 `scope_type + owner_domain_id`，范围不一致统一为 `scope_conflict`。
- 枚举数据元候选以 `code_set_code` 与现有数据元修订冻结的码值集修订所属稳定身份编码比较；候选比对不暴露或猜测数据库修订 ID。
- 指标候选中的聚合方式、维度等执行建模提示由 Model/Develop 消费，不属于 Standard 指标定义字段，因此不参与内容冲突判定。
- 候选比对只用于评审提示和打开现有标准，不自动建立文档关联、不创建标准稳定身份或修订，也不改变 `pending / retained / rejected` 人工处置主线。

#### 单项处置与代表版本

- 候选状态固定为 `pending`、`retained`、`rejected`；处置使用候选自己的并发 `version`，`retained` 不会自动创建或发布正式标准。单项处置只适用于当前文档、候选类型与稳定编码下恰好一个语义变体的候选族，并且请求候选必须是该语义变体按统一确定性规则选出的当前代表；历史非代表出现记录返回 409 `candidate_representative_stale`。一旦存在两个及以上语义变体，单项接口必须返回 409 `candidate_family_decision_required`，只能通过候选族胜出裁决完成处置，禁止绕过理由审计或留下同族互相矛盾的状态。

#### 候选族快照与胜出裁决

- 候选族胜出裁决是多语义变体候选族的唯一处置路线，使用服务端候选族快照令牌执行原子命令，不把候选族升级为聚合根。
- 候选族读取必须对未受筛选影响的当前全部代表候选按稳定顺序冻结 `candidate_id + version + semantic_fingerprint`，连同候选类型和编码计算 SHA-256 `snapshot_token`；请求只提交该令牌、唯一胜出代表候选 ID，以及去除首尾空白后 1–1000 字符的人工理由，不再由客户端重复提交无上限的成员数组。
- Standard 先锁定来源文档，再按候选 ID 稳定加锁，在同一事务中重算全部当前代表候选与快照令牌；令牌变化返回 409 `candidate_family_snapshot_stale`，胜出 ID 不是当前代表、候选族不足两个语义变体、候选已正式化或理由无效时整批拒绝。
- 校验通过后把胜出候选置为 `retained`、其余当前代表置为 `rejected`、递增各自版本并追加一条 `document_candidate_family_decisions` 不可变治理事件。
- 事件冻结候选类型、编码、胜出候选、理由、操作者、时间和全部成员的候选 ID、语义指纹、名称、裁决后版本及结果；成员数量只要求至少两个，不设置会造成候选族治理死锁的固定上限。
- 后续重新裁决只追加新事件，不覆盖既有事件；原始内容、提炼批次与证据不改写，后续新提炼仍可产生新的待裁决变体。
- 候选族读取同时返回未受筛选影响的 `total_variant_count` 和该族 `decision_count`，前端只有在当前返回完整候选族时才提供胜出裁决入口，并且不得同时提供单项处置入口。
- 裁决历史通过独立分页读取接口按事件 ID 倒序返回，禁止把无界事件历史嵌入候选族列表。

#### 正式化、权限与历史保护

- `standard.document_candidate_formalizations` 为候选的一对一不可变正式化事实，保存服务器判定的 `created_identity|created_revision|linked_existing`、目标稳定身份/修订及操作者。正式化要求候选为 `retained` 且仍是其语义变体的当前代表，使用候选 `version` 并在同一事务中重新计算代表、创建目标草稿或确认既有修订、写入正式化事实、递增候选版本；历史非代表候选返回 409 `candidate_representative_stale`，重复正式化返回 409。正式化后候选继续保持 `retained` 且不再允许重新裁决，避免来源事实与目标修订失配。
- 正式化不接受客户端指定目标或动作。无同编码身份时创建 R1 草稿；同编码同范围且无工作修订时以最新修订为基线叠加候选明确字段创建新草稿；与现有 `draft|in_review|published` 修订内容一致时只关联该修订。`scope_conflict`、已有不同内容工作修订、无法唯一解析的计量单位或枚举码值集当前生效已发布修订均返回明确冲突。新指标候选必须由人工在请求中选择 `metric_type`；其他候选不得携带该字段。
- 正式化入口固定需要 `standard.document.update`，并由服务器根据最终动作条件校验对应类型的 `*.create` 或 `*.update` Permission；无法分类时默认拒绝。创建的草稿仍需人工补齐生效时间、范围约束、指标依赖等治理字段，并沿正式修订审核发布路径处理。
- 只要文档已有候选正式化事实，即使文档和目标标准都尚未发布，也不得删除来源文档；目标标准后续删除不会删除该事实，正式化记录保留目标编码、稳定身份 ID、修订 ID 和当时状态作为历史快照。

### `standard.document_*_mappings` — 文档关联

文档与数据元、术语、指标的多对多关联，每条关联记录 `reference_location`（引用位置）。

## API 端点（`/api/v1/standard`）

以下清单对应当前 [router.go](backend/internal/api/router.go) 的模块业务路由；Permission 事实源见 [Manifest](authorization/permissions.yaml)。普通资源列表／详情使用对应 `read`，创建身份使用 `create`，更新身份或草稿、创建修订及提交审核使用 `update`，退回／发布／撤回使用 `publish`，删除身份使用 `delete`；多资源、机器及专用动作按下文独立约束校验，不从 HTTP 方法推导权限。

本节仅说明登记与调用边界；路由及 Swagger 一致不证明真实认证、持久化或完整消费链路通过，运行验证仍按[标准测试入口与范围](#标准测试入口与范围)分别记录。

### 业务域
```
GET/POST /api/v1/standard/domains
GET/PUT/DELETE /api/v1/standard/domains/:id
```

### 业务术语
```
GET/POST /api/v1/standard/glossaries                 # 创建稳定身份时同时创建首个草稿
GET /api/v1/standard/glossaries/standard_references # 域级标准引用，要求 standard.glossary.read
GET/PUT/DELETE /api/v1/standard/glossaries/:id       # PUT 只更新适用范围、归属域和标签
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
GET /api/v1/standard/elements/standard_references # 域级标准引用，要求 standard.element.read
GET/PUT/DELETE /api/v1/standard/elements/:id   # PUT 只更新归属域和标签
GET/POST /api/v1/standard/elements/:id/revisions
GET/PUT /api/v1/standard/elements/:id/revisions/:revision_id
POST /api/v1/standard/elements/:id/revisions/:revision_id/submit
POST /api/v1/standard/elements/:id/revisions/:revision_id/return
POST /api/v1/standard/elements/:id/revisions/:revision_id/publish
POST /api/v1/standard/elements/:id/revisions/:revision_id/withdraw
GET /api/v1/standard/elements/:id/quality-rules # 只返回当前发布修订的编译规则
```

机器批量修订解析另见[数据字典边界](#数据字典边界)，不复用上述用户详情读取资格。

### 码值集
```
GET/POST /api/v1/standard/code-sets
GET/PUT/DELETE /api/v1/standard/code-sets/:id   # PUT 只更新归属域和标签
GET/POST /api/v1/standard/code-sets/:id/revisions
GET/PUT /api/v1/standard/code-sets/:id/revisions/:revision_id # GET 包含该修订的 items
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/submit
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/return
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/publish
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/withdraw
POST /api/v1/standard/code-sets/:id/revisions/:revision_id/items
PUT/DELETE /api/v1/standard/code-sets/:id/revisions/:revision_id/items/:item_id
```

码值项通过指定修订读取，新增、更新与删除均要求 `standard.code_set.update`。

### 计量单位

度量类别复用 `standard.unit.*`；平台与租户 Context 的读写范围统一见[平台与租户单位维护](#平台与租户单位维护)。

```
GET/POST /api/v1/standard/measurement-categories
PUT/DELETE /api/v1/standard/measurement-categories/:id
GET/POST /api/v1/standard/units
GET/PUT/DELETE /api/v1/standard/units/:id
```

### 指标

指标分类复用 `standard.metric.*`，专业关系与域级标准引用的职责分别见下文导航。

```
GET/POST /api/v1/standard/metric-categories
PUT/DELETE /api/v1/standard/metric-categories/:id

GET/POST /api/v1/standard/metrics
GET /api/v1/standard/metrics/standard_references
GET/PUT/DELETE /api/v1/standard/metrics/:id
GET /api/v1/standard/metrics/:id/relations
GET/POST /api/v1/standard/metrics/:id/revisions
GET/PUT /api/v1/standard/metrics/:id/revisions/:revision_id
GET /api/v1/standard/metrics/:id/revisions/:revision_id/published-reference
POST /api/v1/standard/metrics/:id/revisions/:revision_id/submit
POST /api/v1/standard/metrics/:id/revisions/:revision_id/return
POST /api/v1/standard/metrics/:id/revisions/:revision_id/publish
POST /api/v1/standard/metrics/:id/revisions/:revision_id/withdraw
```

稳定身份、修订号和依赖字段见[指标定义表说明](#standardmetric_definitions--standardmetric_definition_revisions--指标定义)；审核动作沿[修订状态流转](#业务术语数据元码值集指标定义与标准文档的修订状态流转)，关系图读取的权限与事实来源沿[指标关系读取](#指标关系读取)。域级标准引用沿[业务域总览契约](../docs/spec/addp企业资源目录实现规范.md#44-业务域总览与资源浏览)，不以关系图或机器摘要接口代替。

`published-reference` 只返回指定 `published` 修订的发布引用，不据此认定该修订当前生效，也不替代精确历史修订读取。

### 企业目录机器读取

以下入口服务于五类标准的目录接入与最小摘要，不限于指标。均要求当前 `addp-catalog` Tenant Service 身份及 `standard.catalog.read`，与上述用户读取资格分别核验；来源版本、摘要与专业详情的分工统一见[五类专业资源变化与动态引用](../docs/spec/addp企业资源目录实现规范.md#55-standard-五类专业资源变化与动态引用)。

```
GET /api/v1/standard/catalog-resources/changes
POST /api/v1/standard/runtime/catalog-references/resolve
```

### 标准引用与修订机器读取

以下为 Tenant Service 专用入口，与用户详情、域级标准引用及目录变化摘要分别授权：

```
POST /api/v1/standard/references/resolve
GET /api/v1/standard/references/candidates
POST /api/v1/standard/runtime/element-revisions/resolve
POST /api/v1/standard/runtime/element-revisions/resolve-exact
```

`references/resolve` 仅允许 `addp-catalog`、`addp-model`，路由要求 `standard.domain.read` 与 `standard.element.read`；请求含 Glossary 时还要求 `standard.glossary.read`，且 Glossary 仅允许 `addp-catalog`。每项只能提交 `id` 或 `code`，批量上限 200；精确身份范围沿[模块间读取契约](#模块依赖关系)。

`references/candidates` 仅允许 `addp-catalog`，同时要求上述三个读取 Permission，返回 Domain、Glossary 或 Element 的当前可引用候选最小摘要，不替代精确解析。

两类数据元修订解析的调用方差异、`standard.element.read`、时点选版与精确历史修订读取，统一见[数据字典边界](#数据字典边界)；其受控测试不证明真实 OAuth 或联邦链路通过。

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
POST /api/v1/standard/documents/:id/revisions/:revision_id/extractions # Copilot 提炼，要求 standard.document_extraction.create
GET /api/v1/standard/documents/:id/extraction-candidate-families # 按同类型同编码组织跨批次候选语义变体、出现记录与动态 Standard 比对
GET /api/v1/standard/documents/:id/extraction-candidate-family-decisions # 按候选类型和编码分页读取追加式裁决历史
POST /api/v1/standard/documents/:id/extraction-candidates/batch_decide # 显式列出同族代表候选并原子选择一个胜出变体
PUT /api/v1/standard/document-extraction-candidates/:candidate_id # 仅单语义变体候选族的 retained/rejected 人工处置
POST /api/v1/standard/document-extraction-candidates/:candidate_id/formalization # retained 候选创建受控草稿或关联一致修订
GET/PUT /api/v1/standard/documents/:id/mappings # 多维关联（数据元/术语/指标）
```

文件上传、确定修订下载及对象补偿沿[文档文件生命周期](#文档文件生命周期)，测试覆盖与隔离清理沿[文档文件验证范围](#文档文件验证范围)。

候选治理规则按[提炼与证据](#提炼批次与来源证据)、[读取与比对](#实时比对与筛选口径)、[单项处置](#单项处置与代表版本)、[候选族裁决](#候选族快照与胜出裁决)、[正式化](#正式化权限与历史保护)定位。现有用例与未证明范围见[文档候选治理验证范围](#文档候选治理验证范围)。

### 对象侧文档关联

```
GET/POST /api/v1/standard/glossaries/:id/documents
POST /api/v1/standard/glossaries/:id/documents/link
DELETE /api/v1/standard/glossaries/:id/documents/:doc_id
GET/POST /api/v1/standard/elements/:id/documents
POST /api/v1/standard/elements/:id/documents/link
DELETE /api/v1/standard/elements/:id/documents/:doc_id
GET/POST /api/v1/standard/metrics/:id/documents
POST /api/v1/standard/metrics/:id/documents/link
DELETE /api/v1/standard/metrics/:id/documents/:doc_id
```

下表的“对象”分别对应 `standard.glossary`、`standard.element`、`standard.metric`，所列权限均为 all-of：

| 动作 | 对象 Permission | 文档 Permission |
| --- | --- | --- |
| GET 关联列表 | `read` | `standard.document.read` |
| POST 新建并关联 | `update` | `standard.document.create` |
| POST 关联已有文档、DELETE 解除关联 | `update` | `standard.document.update` |

DELETE 只解除关联，不删除文档。关联目标与稳定身份约束见[文档关联的多维设计](#文档关联的多维设计)，页面请求资格、空态与失败提示见[关联文档读取](#关联文档读取)；文档侧 `documents/:id/mappings` 仍按其独立路由与业务校验处理，不套用本表权限组合。

### 执行读取与回收结果

```
GET /api/v1/standard/execution-read-scope
GET /api/v1/standard/cleanup-executions/:execution_id/result
```

读取范围端点由共享 Owner 裁决接受第一方、`unrestricted` 的 Tenant User；只有具备 `system.cleanup.read` 才返回 `cleanup_executor` 的任务历史与本人一次性执行 grant，合法身份缺少该权限时返回空 grants，不授予专业结果读取权。公共读取规则见[执行监控规范](../docs/spec/tasks/执行监控.md#通用执行读取与过程事件)。

专业结果同时要求 `standard.cleanup_result.read` 与 `system.cleanup.read`，并校验相同身份限制、同 Tenant、本人子执行及当前 attempt 的回执／请求绑定；不返回 SQL、凭据、对象路径或原始错误。结果未就绪、不可见与不可用分别按实际响应认定，不能以范围端点成功代替专业结果或真实回收完成；完整契约与验收边界见[Owner 专业结果读取](../docs/spec/addp-cleanup体系规范.md#owner-专业结果读取)。

## 前端路由

下列为 Console 公开路径清单；模块内路径、修订定位与页面状态保护见[前端公开路由](#前端公开路由)。

```
/standard/domains                    # 业务域（树形管理）
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
- **System 模块**: 统一身份认证、模块注册与心跳（`SYSTEM_URL`）
- **Model 模块**: 删除业务域、数据元和指标定义前冻结 Model 标准引用删除屏障并执行权威影响扫描（`MODEL_URL`）；维度层级是 Model 本地聚合
- **Quality 模块**: 删除业务域时还须冻结 Quality 的规则/方案归属引用屏障（`QUALITY_URL`，本地默认 `http://localhost:8182`）；任一消费者不可用时保留协调记录等待补偿，不跳过引用检查。Quality 使用自身服务身份校验归属域，worker 不调用域服务。
- **Copilot 模块**: Standard 使用当前 Tenant 的 `addp-standard` Service Access Token 调用候选提炼端点（`COPILOT_URL`）；Copilot 不回写 Standard 数据库
- **MinIO**: 标准文档文件存储（bucket: `standard`）

**被依赖**（其他模块调用 Standard 的 API）:
- **Model 模块**: 验证 domain_id、element_id / element_revision_id、metric_definition_revision_id；Mermaid 交换文档通过稳定 `domain_code / element_code` 精确解析当前 Tenant 运行时 ID；维度层级只校验 Model 本地事实

`/api/v1/standard` 路由只接受 canonical Bearer Tenant AuthContext；文档下载还可使用 Standard owner 的 Browser Resource Ticket。Catalog 与 Model 对 Domain、Glossary、Element 稳定身份的精确校验只走 `/api/v1/standard/references/resolve`，每项必须且只能提交 Tenant 内代理 `id` 或不可变 `code`；`addp-model` 只解析 Domain 与 Element，Glossary 仍只允许 `addp-catalog`。Metric 目录来源只走 `/catalog-resources/changes` 与 `/runtime/catalog-references/resolve`，两类契约不可混用，也不提供面向 Asset 的自动发现接口。

## IAM Permission 所有权

Standard 是以下第一批 Permission 的唯一 owner：

- `standard.domain.*`
- `standard.element.*`
- `standard.metric.*`
- `standard.catalog.read`（仅 `addp-catalog` 的 `tenant.catalog_runtime`）
- `standard.code_set.*`
- `standard.document.*`
- `standard.document_extraction.create`
- `standard.cleanup_result.read`（与 `system.cleanup.read` 共同校验，见[回收结果入口](#执行读取与回收结果)）
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

稳定身份、业务修订号、并发版本与依赖字段见[指标定义表说明](#standardmetric_definitions--standardmetric_definition_revisions--指标定义)；图中的依赖是 Standard 语义关系，不包含 Model 的物理执行实现。

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

### 值域与质量规则的单一事实源

数据元修订必须在 `unrestricted`、`range`、`enumeration` 中三选一。`range` 只使用结构化 `range_constraint`；`enumeration` 必须绑定具体 `code_set_revision_id`。两者互斥，且绑定的码值集修订必须已经发布、值类型必须与数据元类型相容，码值集修订生效区间必须覆盖数据元修订生效区间。

发布数据元修订时，Standard 只从 `nullable`、长度、格式、连续值域和固定码值集修订确定性编译质量规则。`nullable=false` 表示该数据元在所有适用场景中的非空要求；某张表的局部必填要求由 Model 字段表达。唯一性依赖表粒度，不能配置在数据元上；`extra_quality_rules` 列、请求字段和编辑入口删除。下游消费确定修订的 `compiled_quality_rules`，不维护第二份 `allowed_values`。

数据元详情只读展示已发布修订的编译规则。草稿通过语义字段编辑，发布时生成快照；规则身份由数据元 ID 与约束类型确定，参数变化不改变身份。迁移不改写已发布修订的编译快照或 Quality 的历史执行；含旧唯一性规则的已发布修订须通过正常新修订发布替换，新修订不会继承附加规则。

`nullable` 必须原样持久化，ORM 不得用 `default:true` 覆盖显式 `false`。删除附加规则时，受影响的工作修订推进聚合版本，审核中的修订退回草稿并清空提交审计，要求重新确认；已发布快照保持不变。历史错误保存的可空性无法从当前值反推，不自动回填。

### 数据字典边界

数据字典的组合、查询时态、同步导出和验收边界统一见[Catalog 联邦读模型](../docs/spec/addp企业资源目录实现规范.md#514-数据字典联邦读模型)。Standard 提供数据元及其固定码值集修订快照，不保存数据字典主资源或 Catalog 映射；Catalog 按已审核映射读取固定修订，不能按查询时点另选版本，也不能把 Meta 当前字段表述为历史物理结构。

机器批量入口分别遵循下表，两者均要求当前 Tenant Service 身份及 `standard.element.read`，不以 `standard.catalog.read` 或用户页面读取资格代替。

| 路由（前缀 `/api/v1/standard`） | 输入与选版语义 | 当前允许调用方 |
| --- | --- | --- |
| `POST /runtime/element-revisions/resolve` | `element_ids + as_of`：按统一时点解析生效修订，供 Model 审批冻结；不从列表猜测版本 | `addp-model`、`addp-catalog` |
| `POST /runtime/element-revisions/resolve-exact` | `revision_ids`：读取已固定的 `published` 或 `withdrawn` 历史发布修订与固定码值集快照，不按 `as_of` 重选 | 仅 `addp-catalog` |

精确读取的批量范围、`found=false` 语义及候选必须选择已发布修订的规则，统一见[StandardMapping 展示与精确读取](../docs/spec/addp企业资源目录实现规范.md#standardmapping-展示与精确读取)；历史快照可读不代表可重新选择或发布，也不授予目录或源数据访问权。

现有[路由资格与参数测试](backend/internal/api/element_revision_resolution_handler_test.go)使用 SQLite 和受控 System AuthContext，覆盖两类调用方守卫、缺少权限／身份的拒绝、跨 Tenant／不存在结果及精确 ID 参数校验；不证明真实 OAuth、PostgreSQL 并发或 Catalog 联邦链路。执行入口与层级范围统一见[标准测试入口与范围](#标准测试入口与范围)。

### 文档关联的多维设计

文档关联与术语的数据元映射指向同租户、`lifecycle_state=active` 的数据元稳定身份，允许尚未发布的草稿；不得把身份存在性误用为“当前已发布修订存在性”。下游运行时解析和正式修订引用仍必须按各自发布契约校验。

同一篇文档可同时关联：
- 多个数据元（`document_element_mappings`）
- 多个术语（`document_glossary_mappings`）
- 多个指标（`document_metric_mappings`）

每条关联记录 `reference_location`（在文档中的引用位置），便于溯源。读取权限、空态与加载失败统一见[关联文档读取](#关联文档读取)。

### 无跨 Schema 外键

Standard 的 `elements`、`units`、`code_sets` 等被其他 Schema（model、metadata 等）引用时，**没有数据库级外键约束**，通过应用层 HTTP 调用 Standard API 进行 ID 存在性验证。

Model 可引用的 Domain、Element 和 Metric 使用独立 `lifecycle_state=active|deleting`。硬删除时 Standard 先写入删除协调记录并进入 `deleting`，再由删除协调流程串行锁定 Standard 资源行，调用 Model 冻结 `(tenant_id, resource_type, resource_id)` 屏障并权威扫描引用；有引用时必须先恢复 Model `open`，再恢复 Standard `active` 并返回 `409 standard_resource_referenced`；无引用时才硬删除资源，协调记录保留到 Model 屏障终止为 `deleted` 成功。资源行锁覆盖冻结、扫描和本地删除，用户重试与后台补偿复用同一协调记录；后台定期补偿 `deleting` 或资源已删除但终态未收敛的协调记录。普通 check-then-delete、自动清空 Model 引用、跨 Schema 查询和绕过屏障的强制删除都不是合法路径。

### 数据库约束与启动收敛

Standard 当前使用单一启动迁移入口 `repository.Migrate`：在同一个 PostgreSQL advisory transaction lock 内，先删除退出能力的五张成员清单表，再由 GORM `AutoMigrate` 维护保留能力的表和字段，再幂等收紧唯一索引与 CHECK 约束。约束冲突必须阻止服务启动并暴露具体失败语句，不允许自动删除、合并或改写存量业务数据。

租户资源的编码唯一性以 `(tenant_id, code)` 为准；聚合子资源和映射表使用完整业务键去重。Standard Schema 内部引用可以使用数据库约束，跨 Schema 引用仍只允许通过事实 owner 的 API 校验。

## 开发注意事项

1. **新增资源**: `models` → `repository` → `service` → `handler` → `router.go` → `main.go`

2. **文档上传与下载**: 文件身份、存储路径、权限及替换补偿统一见[文档文件生命周期](#文档文件生命周期)，验证沿[标准测试入口与范围](#标准测试入口与范围)。

3. **重启服务**:
   ```bash
   bash scripts/dev/restart.sh -standard
   ```

4. **树形结构查询**: Domain 和 MetricCategory 均为树形结构，查询时通过递归或应用层组装。

5. **`DocumentPanel` 组件**: 前端复用组件，用于在各详情页嵌入文档关联管理，避免重复实现。

6. **前端 API 统一入口**: 所有 API 调用集中在 [frontend/src/api/standard.js](frontend/src/api/standard.js)。

## 前端公开路由

公开路径清单见[前端路由](#前端路由)。篇内导航：[标准修订展示](#标准修订展示)、[路由与修订定位](#路由与修订定位)、[未保存保护与分区保存](#未保存保护与分区保存)、[术语删除提示](#术语删除提示)、[术语关联保存与候选](#术语关联保存与候选)；类型迁移和测试隔离见[开发迁移与验证](#开发迁移与验证)。

### 标准修订展示

列表、默认详情和关联面板统一按“工作修订 → 当前生效修订 → 最新历史修订”选择展示内容，保留撤回、未来生效或已过期内容的名称与状态。显式指定修订仍按确定修订 ID 读取。前端选择规则只有一个模块内规范实现，不通过历史数组顺序另选默认修订。

### 路由与修订定位

- 模块内 Router 使用 `/domains`、`/glossaries`、`/elements`、`/code-sets`、`/metrics` 等无模块前缀路径；Console 公开 URL 统一加 `/standard` 前缀。
- 术语、数据元、码集和指标详情使用 `/:id` 表达对象身份；详情返回使用明确列表路由，不依赖 `router.back()`。
- 创建成功进入详情使用 `replace`，列表进入详情和跨标准对象导航使用 `push`。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。
- 数据元详情以 `revision_id` 唯一 query 定位确定修订；省略时选择草稿、当前生效修订、最新历史修订中的首个。修订内容、精确读取权限与失败边界沿[数据元修订与机器读取](#数据元修订与机器读取)。历史选择以 replace 同步 URL，刷新与同组件 query 切换恢复同一修订。跨模块入口共用 `common-frontend` 的 `buildStandardElementRevisionRoute`，owner 内导航共用其 location builder。
- 术语详情同样以 `revision_id` 定位确定修订；省略时依次选择草稿、当前生效、最新历史修订。历史选择通过模块导航 replace，同一修订重复点击不替换本地输入；切换前使用共享未保存确认，取消时 URL 与表单均不变。精确读取失败显示错误，不回退其他修订；加载成功才建立新保存基线，过期响应不得覆盖新选择。详情保留术语列表筛选，返回列表移除 `revision_id`。新建草稿成功后进入该草稿 URL。

### 未保存保护与分区保存

数据元与术语编辑页的离页确认、浏览器卸载提醒和 Console 修改状态同步统一消费 `common-frontend` 的 `useUnsavedChangesGuard`。Standard 的 `useUnsavedChanges` 仅维护命名保存分区的基线；两类对象的治理信息与修订内容分别确认保存，保存其中一部分不得覆盖另一部分的输入或解除其保护。同身份的 `revision_id` 切换同样受保护，取消时 URL 和输入保持；状态动作前要求先保存修改。保存失败保留输入与基线，不新增自动保存或本地存储草稿。

- 术语治理信息和修订内容独立保存，删除组合保存路径。治理卡片的保存仅更新适用范围、归属域和标签，不依赖当前修订是否为草稿；顶部保存仅更新可编辑草稿。两者分别校验、调用已有 API 并更新对应分区基线，不提交或覆盖另一分区输入。任一保存成功都推进聚合版本；失败保留输入和保护。保存开始时固定对象、修订和内容，离开页面不取消已提交操作；迟到响应不覆盖新选择。两种保存互斥，保存期间禁用表单及另一保存入口，成功后原位更新而不重载其他编辑状态。

### 术语删除提示

术语列表与详情直接说明删除规则；具有删除权限但存在发布历史时，列表保留禁用的删除按钮，并提示“发布过的术语需保留修订历史，撤回后仍不能删除”。后端删除资格与历史保护见[术语稳定身份](#standardglossaries--业务术语稳定身份)。

### 术语关联保存与候选

- 术语关联数据元保存仅提交关联身份集合；成功更新聚合版本并只重读关联列表，不调用整页加载、不覆盖治理或修订输入，也不重置其保存基线。关联写入与两种表单保存互斥；写入失败保留弹窗选择，写入成功但列表读取失败时明确提示已保存并提供只读刷新，不重发 PUT。切换详情关闭关联弹窗，过期保存、列表或候选搜索响应不得覆盖新页面。
- 术语关联选择器以已有映射初始化已选项名称与编码，不依赖其出现在当前 50 条候选中；搜索替换结果或失败时保留已选项的展示信息。候选仅合并当前已选项与最新搜索结果，按数据元身份去重，不累计未选中的历史结果；取消后重新打开从已保存映射恢复，不保留取消的选择。名称仅用于显示，保存仍只提交身份集合，不增加逐项读取或新接口。

## 开发迁移与验证

### 标准测试入口与范围

执行与层级规则统一见[测试与验收规范](../docs/spec/addp测试与验收规范.md#三标准入口)。下表仅定位已有门禁，不表示当前版本已运行通过；未执行、失败和跳过分别记录，单项结果不替代完整模块或 T4 验收。

| 验证目的 | 标准入口 | 范围与正文归属 |
| --- | --- | --- |
| Standard 模块聚合 | `make test-module MODULE=standard` | 按标准自动发现 owner 的 T0–T3 门禁，实际执行与跳过项单列 |
| Go T1 定位 | `make test-module MODULE=standard GO_ONLY=1` | 仅 Go T1，不计为完整模块门禁 |
| 前端验证 | `make test-standard-frontend` | 确定性测试、受控浏览器 T3 与构建；隔离约束见[浏览器与文档测试隔离](#浏览器与文档测试隔离) |
| PostgreSQL／MinIO T2 | `make test-standard-postgres` | 迁移及服务集成，由[现行标准脚本](../scripts/test/standard-postgres-gate.sh)选择用例；文档文件生命周期与清理约束见[测试隔离](#浏览器与文档测试隔离) |

### 度量目录验证范围

验证覆盖 T0 Permission/Swagger 与页面准入、T1 多语种投影及读写边界、T2 新部署导入与幂等/引用迁移/平台扩展、T3 平台新增后租户只读使用与语言切换；沿用既有 Standard 门禁及 CI 入口。层级入口见[标准测试入口与范围](#标准测试入口与范围)，此处不作为当前版本的通过记录。

### 文档候选治理验证范围

以下为现有断言索引，不是运行结果；按[标准测试入口与范围](#标准测试入口与范围)分别执行 Go T1、PostgreSQL T2 与前端验证。[PostgreSQL 标准脚本](../scripts/test/standard-postgres-gate.sh)已选择候选聚合、比对、族裁决和正式化用例，并拒绝跳过；缺少依赖的单独用例跳过不能计为通过。

| 主题 | 现有用例 | 断言与证明边界 |
| --- | --- | --- |
| [读取与比对](#实时比对与筛选口径) | [聚合 T1](backend/internal/service/document_candidate_group_service_test.go)、[出现记录与筛选 T2](backend/internal/service/document_candidate_group_postgres_integration_test.go)、[标准比对 T2](backend/internal/repository/document_candidate_comparison_postgres_integration_test.go) | 语义归一、出现记录、候选族、筛选计数与动态标准比对；数据库夹具不证明真实提炼 |
| [代表](#单项处置与代表版本)与[族裁决](#候选族快照与胜出裁决) | [服务 T1](backend/internal/service/document_candidate_family_decision_service_test.go)、[仓储 T2](backend/internal/repository/document_candidate_family_decision_postgres_integration_test.go) | 当前代表、快照过期、拒绝无副作用、候选版本和裁决历史；不外推完整 HTTP 权限矩阵 |
| [正式化](#正式化权限与历史保护) | [服务 T1](backend/internal/service/document_candidate_formalization_service_test.go)、[仓储 T2](backend/internal/repository/document_candidate_formalization_postgres_integration_test.go) | 受控权限条件、创建／关联分支、代表与目标并发冲突、版本及历史约束；按具体候选类型和动作认定覆盖 |
| [前端契约与页面](#候选聚合与族分页) | [契约 T1](frontend/tests/documentCandidateContract.test.js)、[变体差异 T1](frontend/tests/documentCandidateVariants.test.js)、[浏览器用例](frontend/e2e/standard-smoke.spec.js) | T1 包含源码契约断言和差异计算；T3 使用受控 API，页面覆盖按具体场景认定，不代替真实后端与权限裁决 |

保留候选不等于正式化，正式化不等于审核发布；上述测试不自动证明真实 Copilot、OAuth 和完整浏览器链路。实际验收须以固定版本的真实请求、产物、失败与清理证据分别认定，不把契约描述、夹具或某层结果计为全部通过。

### 文本类型与责任字段迁移

文本类型统一使用 `string`，界面显示“文本”，可选最大长度与格式约束；数据元类型选择统一采用“类型名称＋例如”两行选项，选中后显示取值范围或精度说明，使用同一个 `ElementDataTypeSelect` 组件覆盖新建和详情；不要求用户选择数据库存储类型。迁移将已有数据元修订的 `text` 类型编码原位规范化为 `string`，保留修订身份、状态和业务约束；这是类型词汇调整，不创建业务修订，受影响数据元的聚合版本推进一次。历史文档提炼批次保持不可变，旧类型候选须重新提炼后正式化。Standard 的五类标准稳定身份整体移除 `steward_id`，保留归属域、标签及审计操作人。

文本类型与责任人收敛沿[Standard 标准测试入口](#标准测试入口与范围)选择相应层级；PostgreSQL 门禁覆盖字段删除、类型规范化、约束与历史快照保持及迁移幂等性。Model 对应使用 `make test-model-frontend`、Model Go 测试和 `make test-model-postgres`，Copilot 使用 `make test-copilot`。上述入口已登记到现有 CI 模块门禁。

### 浏览器与文档测试隔离

Standard 浏览器门禁使用独立的 Vite 依赖缓存，测试模式关闭 HMR，测试端口由共享启动器按轮次分配，不占用或连接开发端口 5181。日常开发模式继续保留热更新；验证沿用[Standard 前端门禁](#标准测试入口与范围)。

#### 文档文件验证范围

[存储服务单测](backend/internal/service/document_service_test.go)使用 SQLite 与受控对象存储验证草稿替换、文件大小拒绝及清理失败保留重试；不计为真实 MinIO 验收。

文档文件 T2 沿[PostgreSQL／MinIO 标准入口](#标准测试入口与范围)执行，由[现有生命周期用例](backend/internal/service/document_service_integration_test.go)直接调用服务层：复用 Infra 的 MinIO Dockerfile 构建独立临时实例，验证真实上传、替换、下载、草稿删除，以及发布后撤回仍可按历史修订下载。测试对自身对象执行清理并验证空前缀，数据库夹具随事务回滚且检查零残留；门禁在正常、失败及中断退出时仅销毁自己的 Compose 项目、临时卷与构建镜像，不操作现有 Infra / Business MinIO。

该 T2 核对服务层下载字节，不证明真实 HTTP 身份与 Permission、资源票据、multipart 请求、响应头、代理传输或浏览器下载。真实入口验收还须核对认证、当前权限、客户端响应完整性和中断结果；HTTP 成功状态不能代替这些断言。

## 业务域总览与五类标准接入

Standard 五类标准统一按稳定身份进入企业目录，专业定义和修订唯一归 Standard，Catalog 不维护可编辑名称/定义副本。来源接入与动态引用按[五类专业资源变化与动态引用](../docs/spec/addp企业资源目录实现规范.md#55-standard-五类专业资源变化与动态引用)执行。

[Catalog 业务域总览](../docs/spec/addp企业资源目录实现规范.md#44-业务域总览与资源浏览)独立于资源浏览；Catalog、Model、Standard 的域级标准引用只读接口使用各自本地事实，精确域、一跳、按目标去重并保留证据；当前 User 权限判定，不复制专业关系或改写冻结修订。

场景方法、固定版本证据、失败与未覆盖项沿[Catalog 验证导航](../catalog/CLAUDE.md#验证导航)分别认定，不以本节能力说明代替当前版本验收。
