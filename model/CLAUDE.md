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

## 目录结构

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
│       │   ├── metric_implementation_handler.go # 指标实现
│       │   └── dw_layer_handler.go
│       ├── config/config.go
│       ├── models/
│       │   ├── entity.go            # Entity, EntityAttribute, EntityRelation
│       │   ├── logical_table.go     # LogicalTable, LogicalField, TableRelation
│       │   ├── metric_implementation.go
│       │   ├── dw_layer.go
│       │   └── common_types.go      # JSONB 等共用类型
│       ├── repository/              # 数据访问层
│       └── service/
│           ├── mermaid_parser.go    # Mermaid ER 图解析器
│           └── ...
└── frontend/
    └── src/
        ├── api/
        │   ├── client.js
        │   └── model.js             # 所有 API 调用
        ├── views/
        │   ├── EntityList.vue
        │   ├── EntityDetail.vue
        │   ├── LogicalTableList.vue
        │   ├── LogicalTableDetail.vue
        │   ├── StarSchemaView.vue   # 维度建模
        │   ├── ERDiagramManager.vue # 实体关系图
        │   └── DWLayerList.vue
        └── components/
            └── DDLPreviewDialog.vue
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
| scd_type | int | 缓慢变化维类型 0=静态/1=覆盖/2=拉链/3=混合（仅 dimension 表） |
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

`model.logical_table_entity_mappings`、`model.logical_field_attribute_mappings` 和 `model.table_relation_entity_relation_mappings` 分别表达表、字段和关系如何实现概念模型。三类映射均属于 LogicalTable 聚合，通过同一个完整替换 API 写入并推进父版本；表映射冻结 Entity 版本，关系映射冻结 EntityRelation 版本，审批时拒绝来源草稿或版本漂移。

### `model.metric_implementations` / `model.metric_implementation_revisions`

指标实现是独立版本主体；稳定身份保存 `fact_table_id`、`metric_definition_id`、名称和 `version`。修订保存 `revision_no`、`metric_definition_revision_id`、类型化 `contract`、依赖快照与 hash，以及 `draft|published|withdrawn` 状态。同一实现最多一个草稿，发布内容不可变；事实表只是来源引用，不拥有实现生命周期。API 与完整契约以正式规范为准。

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

```
GET    /api/v1/model/entities               # 列表，支持 domain_id/status 过滤
POST   /api/v1/model/entities               # 创建
GET    /api/v1/model/entities/:id           # 详情
PUT    /api/v1/model/entities/:id           # 更新
DELETE /api/v1/model/entities/:id           # 删除
POST   /api/v1/model/entities/:id/approve   # 审批通过（status: draft → approved）
GET/POST/PUT/DELETE .../attributes       # 实体属性 CRUD
POST   /api/v1/model/entities/import-mermaid/preview # 预览 Markdown Mermaid 增量导入
POST   /api/v1/model/entities/import-mermaid         # 确认 Markdown Mermaid 增量导入
GET    /api/v1/model/entities/export-mermaid         # 按可选 domain_id 导出 Markdown Mermaid
```

Entity、LogicalTable、DWLayer 和 EntityRelation 是独立并发版本主体。EntityAttribute 使用父 Entity 版本；LogicalField、TableRelation、DimensionHierarchy 使用父 LogicalTable 版本；MetricImplementation 使用独立版本，其修订使用实现版本。已有资源及聚合子资源的所有写操作都在 JSON body 中携带对应 `version`，成功后返回新版本；不接受 query、Header 或服务端版本兜底。

Mermaid 导出仍以可选 `domain_id` query 选择当前运行时资源，但交换响应和 Markdown 使用 Standard 稳定编码，例如 `{ "markdown": "...", "scope": "domain", "domain_code": "outdoor" }`；省略 query 表示全域。交换格式唯一为 `addp.model.er/v2`，文档及实体使用 `domain_code`，属性使用 `element_code`，不输出或兼容解析 `domain_id / element_id`。预览提交 `{ "markdown": "..." }`，先按当前 Tenant 经 Standard API 精确解析编码，再返回解析后的业务域显示摘要、创建、未变更、冲突计划与 `revision`；确认导入提交同一 Markdown 和预览 `revision`。文件缺失成员不表示删除，同编码不同定义明确冲突，不保留全量替换或自动 upsert 路线。

Tenant 实体模型集合的 `revision` 由所有 Entity、EntityAttribute、EntityRelation 写入推进，确保预览后发生的普通写入会使确认导入返回版本冲突。Mermaid ADDP 元数据必须完整保存这些资源的可编辑业务字段。

### 实体关系

```
GET/POST /api/v1/model/entity-relations
GET/PUT/DELETE /api/v1/model/entity-relations/:id
```

### 逻辑表

```
GET    /api/v1/model/logical-tables                          # 列表，支持 table_type/status/domain_id 过滤
POST   /api/v1/model/logical-tables                          # 创建
GET/PUT/DELETE /api/v1/model/logical-tables/:id              # 详情/更新/删除
GET/POST/PUT/DELETE /api/v1/model/logical-tables/:id/fields  # 字段 CRUD
POST   /api/v1/model/logical-tables/:id/preview-ddl          # 预览建表语句
GET/POST /metric-implementations                         # 独立指标实现及修订，详见正式规范
GET/POST/PUT/DELETE .../dimension-relations                   # 事实表关联维度表（含字段映射）
GET/POST/PUT/DELETE .../dimension-hierarchies             # 维度表聚合内层级及成员
GET/PUT .../concept-mappings                              # 概念实现映射读取/完整替换
```

## 前端路由

```
/modeling/dw-layers              # 数仓分层
/modeling/entities               # 业务实体列表
/modeling/entities/:id           # 实体详情（属性、关系、Mermaid 图）
/modeling/logical-tables         # 逻辑表列表
/modeling/logical-tables/:id     # 逻辑表详情（字段、维度层级、物理目标、建表语句预览）
/modeling/er-diagram             # 按业务域查看 ER 图；domain_id=all 显式全域
/modeling/star-schema            # 维度建模（事实表-维度表-指标三维关联）
```

## 模块依赖关系

**依赖**:
- **System 模块**: JWT 认证、用户信息（`SYSTEM_URL`）
- **Standard 模块**:
  - 验证 domain_id、element_id / element_revision_id、metric_definition_revision_id 是否存在（`STANDARD_URL`）；维度层级只校验 Model 本地事实
  - Standard 前端直接调用 Standard 唯一 API，Model 不提供代理路径

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

创建/更新时，Service 层通过 HTTP 调用 Standard 模块 API 验证 ID 是否存在。前端直接调用 Standard 的唯一公开 API，Model 不提供 Standard 代理路径。

## IAM Permission 所有权

Model 是 `model.logical_model.*` 第一批 Permission 的唯一 owner，机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Model 服务启动时不向 System 动态注册 Permission。

Entity、EntityRelation、DWLayer 和 LogicalModel 分别使用 `model.entity.*`、`model.entity_relation.*`、`model.dw_layer.*`、`model.logical_model.*`。EntityAttribute 是 Entity 聚合内子资源；LogicalField、TableRelation、DimensionHierarchy 是 LogicalModel 聚合内子资源；MetricImplementation 是独立聚合，使用 model.metric_implementation 精确权限。Mermaid 导入预览和确认均按 Entity 与 EntityRelation 的 create 执行 all-of 校验；导出按两者的 read 执行 all-of 校验。导入只创建缺失成员，不要求全租户已审批实体退回草稿；新关系仍不得绕过两端实体的生命周期约束。

并发契约以 [Model 概念与数据约束规范](docs/model概念与数据约束规范.md) 为事实源。后端必须把版本校验、生命周期校验、聚合写入和版本递增放在同一事务中；前端收到 `409 resource_version_conflict` 后保留本地未保存状态，不自动重试。

EntityRelation 使用完整 `PUT`：请求包含变更后的 source_entity、target_entity、relation_type、name、description 和 version，并在事务内锁定变更前后涉及的全部 Entity。Cleanup 属于内部强制写入，同样必须推进资源版本和实体模型集合 revision。

## 特殊设计

### Mermaid 解析器

`backend/internal/service/mermaid_parser.go` 实现了 `addp.model.er/v2` ADDP Markdown Mermaid ER 文档的解析，支持预览后将缺失的实体和关系批量创建。交换文档只携带稳定 `domain_code / element_code`，Service 通过 Standard API 在当前 Tenant 精确解析为运行时 ID；它不覆盖或删除现有模型。

### Entity 与逻辑表状态机

```
draft ⇄ approved
```

只有 `draft` 可修改；审批和退回草稿必须使用显式状态转换。`materialized` 不属于当前正式状态，建表语句预览不改变状态。

### `dimension-relations` 查询返回 JOIN 结果

事实表查询出向关联，维度表查询被哪些事实表引用；详情包含两端表名、编码、字段名和列名。关系唯一编辑入口为事实表详情 `?tab=relations&relation_id=<id>`；维度建模只导航。新增、更新、删除只要求事实表为草稿，引用已审批维度不解除其审批或冻结修订。

`GET /api/v1/model/logical-tables/:id/dimension-relations` 返回的是带字段名的详情（通过 Raw SQL JOIN），而非原始 ID，前端可直接展示，无需二次请求：

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

3. **跨模块验证**: Service 层通过 `cfg.StandardURL` 和 `addp-model` Tenant Service Access Token 调用 Standard API。如果 Standard 服务未启动，相关创建操作会失败。

4. **前端 API 统一入口**: Model API 调用集中在 [frontend/src/api/model.js](frontend/src/api/model.js)；Standard API 使用 Standard 的唯一公开路径。

5. **后端测试**:

   ```bash
   cd model/backend
   go test ./...
   ADDP_TEST_MODEL_POSTGRES_DSN='postgres://addp:addp_password@localhost:15432/addp_test?sslmode=disable' \
     make -C ../.. test-model-postgres
   ```
   PostgreSQL 集成测试未设置 `ADDP_TEST_MODEL_POSTGRES_DSN` 时会跳过；并发、事务和迁移相关改动必须通过根 Makefile 的第二条标准门禁执行，不能直接创建临时 database。

指标计算已确认采用“Model 构建中立计划 → 引擎独立编译器 → 唯一 PreparedQuery”目标设计，完整契约见 [Model 约束](docs/model概念与数据约束规范.md#数据库无关计划与修订已确认设计待代码替换) 和 [引擎插件规范](../docs/spec/addp引擎插件接口规范.md#数据库无关分析计算契约)。当前封闭方言表达器与指标 SQL 拼接属于待替换阶段代码。元数据生命周期及 PG 金样沿 `make test-model-postgres`；MySQL 真实计算沿 `ADDP_TEST_MYSQL_PASSWORD=<disposable-password> make test-model-mysql`，自动清理 addp_model_mysql_it_* 并拒绝跳过，两者已登记 T2。新代码需把双方言金样改为验证同一中立计划，不另建并行编译路线；页面物理绑定与建表扩展另行跟踪。

维度关联改动沿用现有自动发现门禁：`make test-module MODULE=model` 覆盖平台一致性、Go 单元、前端路由及交互、PostgreSQL 事务测试；其中数据库测试需配置上述测试 DSN。`make test-model-frontend` 包含关系入口唯一所有权、URL 恢复、审批只读、原位更新和冲突保留测试；CI 继续使用已登记的 Model 前端与 PostgreSQL 作业。

## 前端公开路由

- 模块内 Router 使用 `/dw-layers`、`/entities`、`/logical-tables`、`/er-diagram`、`/star-schema`；Console 模块名为 `modeling`，公开 URL 统一加 `/modeling` 前缀。
- 实体和逻辑表详情使用 `/:id`；实体详情默认 `basic` Tab 省略，`attributes`、`relations` 使用唯一 `tab` query。
- 维度建模业务域使用 `domain_id`（省略表示全部），当前事实表使用 `table_id`，并响应刷新及浏览器前进/后退；无选择时省略该 query。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`；详情返回明确列表路由。
- 逻辑表详情的“物理目标”页签集中提供目标引擎、目标位置与目标表名展示，以及建表语句预览、创建/校验、执行记录和删除目标表操作；完整数据流程从 Orchestrator 进入。

- Model 导航依次为业务实体、实体关系图、数仓分层、逻辑表设计、维度建模。

指标计划所需物理结构由 `META_URL` 指定的 Meta 提供（默认 `http://localhost:8082`）；Model Runtime 使用已有 `meta.catalog.read`，不新增 System 结构查询权限。只有参与计算的字段事实进入冻结包，Meta 扫描时间与无关显示字段不进入计算依赖。
