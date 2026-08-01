# Model 模块 CLAUDE.md

本文件为 Claude Code 在 `model/` 目录下工作时提供指导。

## 模块概述

**Model 模块** 是 ADDP 平台的数据建模和数仓设计服务，负责：

- 业务实体（Entity）设计与属性定义
- 逻辑表（LogicalTable）设计：支持实体表、事实表、维度表三种建模角色
- 星型建模视图：事实表与维度表的显式关联关系
- 指标血缘：事实表与 Standard 模块指标的关联
- ER 图 Mermaid 导入导出
- DDL 预览（物化前的 SQL 预览）
- 数仓分层（DW Layer）定义

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
│       │   ├── fact_metric_handler.go      # 事实表-指标关联
│       │   └── dw_layer_handler.go
│       ├── config/config.go
│       ├── models/
│       │   ├── entity.go            # Entity, EntityAttribute, EntityRelation
│       │   ├── logical_table.go     # LogicalTable, LogicalField, TableRelation
│       │   ├── fact_metric_mapping.go
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
        │   ├── StarSchemaView.vue   # 星型建模视图
        │   ├── ERDiagramManager.vue # 实体关系图
        │   └── DWLayerList.vue
        └── components/
            └── DDLPreviewDialog.vue
```

## 数据库表结构

### `model.entities` — 业务实体

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int64 PK | 主键 |
| tenant_id | int64 | 租户 ID |
| domain_id | int64? | 所属业务域（引用 standard.domains） |
| name / code | string | 显示名 / 英文标识符 |
| description | text | 描述 |
| status | string | `draft` / `approved` |
| created_by / updated_by | int64 | 操作人 |

### `model.entity_attributes` — 实体属性

| 字段 | 类型 | 说明 |
|------|------|------|
| entity_id | int64 | 所属实体 |
| element_id | int64? | 引用 `standard.elements`（无 DB FK） |
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

### `model.logical_tables` — 逻辑表

| 字段 | 类型 | 说明 |
|------|------|------|
| table_type | string | `entity` / `fact` / `dimension` |
| layer | string | `ods` / `dwd` / `dws` / `ads` |
| status | string | `draft` / `approved` / `materialized` |
| grain_description | text | 粒度声明（仅 fact 表） |
| scd_type | int | 缓慢变化维类型 0=静态/1=覆盖/2=拉链/3=混合（仅 dimension 表） |
| materialization | JSONB | 物化配置（关联到真实物理表） |

### `model.logical_fields` — 逻辑表字段

| 字段 | 类型 | 说明 |
|------|------|------|
| table_id | int64 | 所属逻辑表 |
| element_id | int64? | 引用 `standard.elements`（无 DB FK） |
| field_role | string | `regular` / `measure_additive` / `measure_semi` / `measure_non` / `dimension_fk` / `degenerate_dim` |
| hierarchy_id / hierarchy_level | int64? / int? | 维度层级关联（引用 standard.dimension_hierarchies） |
| is_pk / is_partition | bool | 主键 / 分区字段 |

### `model.table_relations` — 逻辑表间关系

| 字段 | 类型 | 说明 |
|------|------|------|
| source_table / source_field | int64 | 事实表 ID / 事实表字段 ID（外键字段） |
| target_table / target_field | int64 | 维度表 ID / 维度表字段 ID（主键字段） |
| relation_type | string | `fk`（外键关联）/ `join`（宽泛关联） |
| tenant_id | int64 | 租户隔离 |

### `model.fact_metric_mappings` — 事实表与指标关联

| 字段 | 类型 | 说明 |
|------|------|------|
| fact_table_id | int64 | 事实表 ID |
| metric_id | int64 | 引用 `standard.metrics`（无 DB FK） |
| field_id | int64? | 对应的逻辑表字段（可选） |
| note | text | 备注 |

### `model.dw_layers` — 数仓分层

| 字段 | 类型 | 说明 |
|------|------|------|
| layer_code | string | `ods` / `dwd` / `dws` / `ads` |
| layer_name | string | 分层名称 |
| naming_rule | text | 命名规范 |
| quality_sla | JSONB | 质量 SLA 配置 |
| sort_order | int | 显示顺序 |

## API 端点（`/api/model`）

### 业务实体

```
GET    /api/model/entities               # 列表，支持 domain_id/status 过滤
POST   /api/model/entities               # 创建
GET    /api/model/entities/:id           # 详情
PUT    /api/model/entities/:id           # 更新
DELETE /api/model/entities/:id           # 删除
POST   /api/model/entities/:id/approve   # 审批通过（status: draft → approved）
GET/POST/PUT/DELETE .../attributes       # 实体属性 CRUD
POST   /api/model/entities/import-mermaid # 从 Mermaid ER 图导入
GET    /api/model/entities/export-mermaid # 导出 Mermaid ER 图
```

### 实体关系

```
GET/POST /api/model/entity-relations
GET/PUT/DELETE /api/model/entity-relations/:id
```

### 逻辑表

```
GET    /api/model/logical-tables                          # 列表，支持 table_type/status/domain_id 过滤
POST   /api/model/logical-tables                          # 创建
GET/PUT/DELETE /api/model/logical-tables/:id              # 详情/更新/删除
GET/POST/PUT/DELETE /api/model/logical-tables/:id/fields  # 字段 CRUD
POST   /api/model/logical-tables/:id/preview-ddl          # 预览 DDL
GET/POST/DELETE .../metrics                               # 事实表关联指标
GET/POST/DELETE .../dimension-relations                   # 事实表关联维度表（含字段映射）
```

### 代理到 Standard 模块

```
/api/model/domains          → /api/standard/domains
/api/model/elements         → /api/standard/elements
/api/model/dw-layers        # 独立
/api/model/standard-metrics → /api/standard/metrics
/api/model/dimension-hierarchies → /api/standard/dimension-hierarchies
```

## 前端路由

```
/modeling/dw-layers              # 数仓分层
/modeling/entities               # 业务实体列表
/modeling/entities/:id           # 实体详情（属性、关系、Mermaid 图）
/modeling/logical-tables         # 逻辑表列表
/modeling/logical-tables/:id     # 逻辑表详情（字段设计、DDL 预览）
/modeling/er-diagram             # 全局 ER 图视图
/modeling/star-schema            # 星型建模视图（事实表-维度表-指标三维关联）
```

## 模块依赖关系

**依赖**:
- **System 模块**: JWT 认证、用户信息（`SYSTEM_URL`）
- **Standard 模块**:
  - 验证 element_id、hierarchy_id 是否存在（`STANDARD_URL`）
  - 代理域名、数据元、指标、维度层级等查询

**被依赖**:
- 无其他模块调用 Model 的 API（Model 是纯消费端）

## 跨 Schema 关联设计

Model 和 Standard 使用不同的 PostgreSQL Schema，**无数据库外键约束**，通过应用层验证：

| Model 字段 | 引用 Standard |
|-----------|-------------|
| `entities.domain_id` | `standard.domains.id` |
| `entity_attributes.element_id` | `standard.elements.id` |
| `logical_fields.element_id` | `standard.elements.id` |
| `logical_fields.hierarchy_id` | `standard.dimension_hierarchies.id` |
| `fact_metric_mappings.metric_id` | `standard.metrics.id` |

创建/更新时，Service 层通过 HTTP 调用 Standard 模块 API 验证 ID 是否存在。

## IAM Permission 所有权

Model 是 `model.logical_model.*` 第一批 Permission 的唯一 owner，机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Model 服务启动时不向 System 动态注册 Permission。

Entity、EntityRelation、DWLayer 和 LogicalModel 分别使用 `model.entity.*`、`model.entity_relation.*`、`model.dw_layer.*`、`model.logical_model.*`。EntityAttribute 是 Entity 聚合内子资源；LogicalField、TableRelation 和 FactMetricMapping 是 LogicalModel 聚合内子资源，不建立平行宽泛 Permission。Mermaid 导入和导出分别按 Entity 与 EntityRelation 的 create/read 执行 all-of 校验。

## 特殊设计

### Mermaid 解析器

`backend/internal/service/mermaid_parser.go` 实现了 Mermaid ER 图的解析，支持将 ER 图批量导入为实体和关系。适合从已有文档快速初始化数据模型。

### 逻辑表状态机

```
draft → approved → materialized
```

当前仅实现了 Entity 的 `draft → approved`，逻辑表的状态转换暂未实现 API，`materialized` 状态预留给未来物化功能。

### `dimension-relations` 查询返回 JOIN 结果

`GET /api/model/logical-tables/:id/dimension-relations` 返回的是带字段名的详情（通过 Raw SQL JOIN），而非原始 ID，前端可直接展示，无需二次请求：

```json
{
  "id": 1,
  "source_field": 3,
  "source_field_name": "客户ID",
  "target_table": 5,
  "target_table_name": "客户维度表",
  "target_table_code": "dim_customer",
  "target_scd_type": 2,
  "target_field": 8,
  "target_field_name": "客户主键",
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

1. **新增 API**: `models` → `repository` → `service` → `handler` → `router.go` → `main.go`（注入依赖）

2. **重启服务**:
   ```bash
   bash scripts/dev/restart.sh -model
   ```

3. **跨模块验证**: Service 层通过 `cfg.StandardURL` 和 `cfg.InternalAPIKey` 调用 Standard API。如果 Standard 服务未启动，相关创建操作会失败。

4. **代理路由位置**: 代理到 Standard 的路由配置在 `router.go` 末尾，通过 `proxyToStandard()` 函数实现。

5. **前端 API 统一入口**: 所有 API 调用集中在 [frontend/src/api/model.js](frontend/src/api/model.js)，新增接口在此文件追加。

## 前端公开路由

- 模块内 Router 使用 `/dw-layers`、`/entities`、`/logical-tables`、`/er-diagram`、`/star-schema`；Console 模块名为 `modeling`，公开 URL 统一加 `/modeling` 前缀。
- 实体和逻辑表详情使用 `/:id`；实体详情默认 `basic` Tab 省略，`attributes`、`relations` 使用唯一 `tab` query。
- 星型模型当前事实表使用 `table_id`，并响应刷新及浏览器前进/后退；无选择时省略该 query。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`；详情返回明确列表路由。
