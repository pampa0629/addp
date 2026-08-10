# Standard 模块 CLAUDE.md

本文件为 Claude Code 在 `standard/` 目录下工作时提供指导。

数据元 `quality_rules` 的结构和校验必须遵守 [ADDP 数据质量规范](../docs/spec/addp数据质量规范.md)。Standard 只拥有规则定义，不拥有物理字段应用、规则执行、评分或质量问题。

## 模块概述

**Standard 模块** 是 ADDP 平台的数据标准和治理中心，负责：

- 业务域（Domain）树形组织管理
- 业务术语（Glossary）词典：别名、定义、状态流转、关联数据元
- 数据元（Element）：数据标准的核心原子对象，定义数据规格、质量规则、安全等级
- 码值集（CodeSet）：系统/自定义码值集及码值项
- 计量单位（Unit）：按度量类别组织的计量单位
- 数据分类（Classification）与分级（GradingLevel）
- 指标（Metric）：原子/派生/复合三类指标，支持依赖关系
- 标准文档（Document）：文档上传与多维关联（数据元/术语/指标）
- 维度层级（DimensionHierarchy）：业务上的上下钻路径定义

**端口**:
- 后端: `8110`（环境变量 `STANDARD_BACKEND_PORT`）
- 前端: `5181`（开发环境）

**数据库 Schema**: `standard`

**外部存储**: MinIO（存储标准文档文件，bucket 名为 `standard`）

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
│       │   ├── domain_handler.go
│       │   ├── glossary_handler.go
│       │   ├── element_handler.go
│       │   ├── code_set_handler.go
│       │   ├── unit_handler.go
│       │   ├── classification_handler.go
│       │   ├── metric_handler.go
│       │   ├── document_handler.go
│       │   └── dimension_hierarchy_handler.go
│       ├── config/config.go
│       ├── models/
│       │   ├── domain.go
│       │   ├── glossary.go
│       │   ├── element.go
│       │   ├── code_set.go
│       │   ├── unit.go
│       │   ├── classification.go
│       │   ├── metric.go
│       │   ├── document.go
│       │   ├── dimension_hierarchy.go
│       │   └── common_types.go        # StringArray, Int64Array, JSONB
│       ├── repository/
│       └── service/
└── frontend/
    └── src/
        ├── api/
        │   └── standard.js            # 所有 API 调用
        ├── views/
        │   ├── DomainList.vue
        │   ├── GlossaryList.vue / GlossaryDetail.vue
        │   ├── ElementList.vue / ElementDetail.vue
        │   ├── CodeSetList.vue / CodeSetDetail.vue
        │   ├── UnitList.vue
        │   ├── ClassificationList.vue  # 分类与分级合并页
        │   ├── MetricList.vue / MetricDetail.vue
        │   ├── DocumentList.vue
        │   └── DimensionHierarchyList.vue / DimensionHierarchyDetail.vue
        └── components/
            ├── DocumentPanel.vue       # 通用文档关联面板
            └── DDLPreviewDialog.vue
```

## 数据库表结构

### `standard.domains` — 业务域（树形）

| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点（支持多层级） |
| name / code | string | 显示名 / 英文标识符 |
| icon | string | 图标标识 |
| sort_order | int | 同层排序 |

### `standard.glossaries` — 业务术语

| 字段 | 类型 | 说明 |
|------|------|------|
| domain_id | int64? | 所属业务域 |
| alias | StringArray | 别名列表 |
| definition / example / note | text | 定义、示例、备注 |
| status | string | `draft` / `approved` / `deprecated` |
| steward_id | int64? | 数据责任人 |
| related_ids | StringArray | 关联术语 ID 列表 |
| tags | StringArray | 标签 |

### `standard.glossary_element_mappings` — 术语与数据元映射

| 字段 | 类型 | 说明 |
|------|------|------|
| glossary_id / element_id | int64 PK | 复合主键，多对多关联 |

### `standard.elements` — 数据元（核心对象）

| 字段 | 类型 | 说明 |
|------|------|------|
| domain_id | int64? | 所属业务域 |
| name / code | string | 显示名 / 英文标识符 |
| data_type | string | string/int/bigint/float/decimal/date/datetime/bool/json/text |
| length / precision_num / scale | int? | 长度/精度/小数位 |
| nullable / default_value | bool/string | 可空 / 默认值 |
| format | string | 格式约束（如日期格式） |
| value_range | JSONB | 值域约束 |
| unit_id | int64? | 引用 `standard.units` |
| security_level | string | `L1` / `L2` / `L3` / `L4` |
| classification_id | int64? | 引用 `standard.classifications` |
| code_set_id | int64? | 引用 `standard.code_sets`（码值约束） |
| quality_rules | JSONB | 质量规则定义 |
| status | string | `draft` / `approved` |
| steward_id | int64? | 数据责任人 |
| tags | StringArray | 标签 |

### `standard.code_sets` — 码值集

| 字段 | 类型 | 说明 |
|------|------|------|
| code / name | string | 码值集标识 / 名称 |
| type | string | `system`（系统内置）/ `custom`（用户自定义） |

### `standard.code_items` — 码值项

| 字段 | 类型 | 说明 |
|------|------|------|
| code_set_id | int64 | 所属码值集 |
| code / value | string | 码 / 值 |
| parent_id | int64? | 预留树形（当前平铺使用） |
| sort_order / is_active | int/bool | 排序 / 是否启用 |

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

### `standard.classifications` — 数据分类（树形）

| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点 |
| name / code | string | 显示名 / 标识 |
| sort_order | int | 同层排序 |

### `standard.grading_levels` — 数据分级（固定 L1-L4）

| 字段 | 类型 | 说明 |
|------|------|------|
| level | string | `L1` / `L2` / `L3` / `L4`（固定，不可增删） |
| name / description | string | 自定义名称 / 描述 |
| color | string | 十六进制颜色（如 `#FF0000`） |
| sort_order | int | 显示顺序 |

> **注意**: 分级记录由系统初始化，仅支持 PUT 更新名称/描述/颜色，不支持创建和删除。

### `standard.metric_categories` — 指标分类（树形）

| 字段 | 类型 | 说明 |
|------|------|------|
| parent_id | int64? | 父节点 |
| name / code | string | 类别名 / 标识 |

### `standard.metrics` — 指标定义

| 字段 | 类型 | 说明 |
|------|------|------|
| category_id / domain_id | int64? | 所属分类 / 业务域 |
| name / code | string | 指标名 / 英文标识 |
| type | string | `atomic`（原子）/ `derived`（派生）/ `composite`（复合） |
| definition | text | 定义 |
| formula | text | 计算公式（复合指标） |
| unit_id | int64? | 引用 `standard.units` |
| base_metric_id | int64? | 基础指标（派生指标用） |
| derivation_config | JSONB | 派生配置（聚合方式、过滤条件等） |
| status | string | `draft` / `approved` / `deprecated` |
| steward_id | int64? | 数据责任人 |
| tags | StringArray | 标签 |

### `standard.metric_element_mappings` — 指标与数据元关联

原子指标与数据元之间的映射，记录指标来源于哪个数据元。

### `standard.metric_dependencies` — 复合指标依赖关系

| 字段 | 类型 | 说明 |
|------|------|------|
| from_metric_id / to_metric_id | int64 | 依赖关系方向 |
| coefficient | float? | 权重系数（可选） |

### `standard.documents` — 标准文档

| 字段 | 类型 | 说明 |
|------|------|------|
| name | string | 文档名 |
| doc_type | string | `national` / `industry` / `internal` / `reference` |
| source_org | string | 来源机构 |
| version / publish_date | string | 版本 / 发布日期 |
| file_key | string | MinIO 存储路径（bucket: standard） |
| file_name / file_size | string/int64 | 原始文件名 / 大小 |

### `standard.document_*_mappings` — 文档关联

文档与数据元、术语、指标的多对多关联，每条关联记录 `reference_location`（引用位置）。

### `standard.dimension_hierarchies` — 维度层级

定义业务意义上的上下钻路径（如时间：年→季→月→日）。

### `standard.dimension_hierarchy_levels` — 维度层级的每一层

| 字段 | 类型 | 说明 |
|------|------|------|
| hierarchy_id | int64 | 所属层级 |
| level_num | int | 层次编号（从 1 开始） |
| name | string | 层次名称（如"年"） |
| element_id | int64? | 引用 `standard.elements`（层次对应的数据元） |
| sort_order | int | 显示顺序 |

## API 端点（`/api/standard`）

### 业务域
```
GET/POST /api/standard/domains
GET/PUT/DELETE /api/standard/domains/:id
```

### 业务术语
```
GET/POST /api/standard/glossaries
GET/PUT/DELETE /api/standard/glossaries/:id
POST /api/standard/glossaries/:id/approve     # 草稿→已发布
POST /api/standard/glossaries/:id/deprecate   # 已发布→已弃用
GET/PUT /api/standard/glossaries/:id/elements # 关联数据元
```

### 数据元
```
GET/POST /api/standard/elements
GET/PUT/DELETE /api/standard/elements/:id
POST /api/standard/elements/:id/approve
GET/PUT /api/standard/elements/:id/quality-rules
```

### 码值集
```
GET/POST /api/standard/code-sets
GET/PUT/DELETE /api/standard/code-sets/:id
GET/POST/PUT/DELETE /api/standard/code-sets/:id/items
```

### 计量单位
```
GET/POST /api/standard/measurement-categories
PUT/DELETE /api/standard/measurement-categories/:id
GET/POST /api/standard/units
GET/PUT/DELETE /api/standard/units/:id
```

### 数据分类与分级
```
GET/POST /api/standard/classifications
PUT/DELETE /api/standard/classifications/:id

GET /api/standard/grading-levels           # 始终返回 L1-L4 四条记录
PUT /api/standard/grading-levels/:id       # 更新名称/描述/颜色（不可增删）
```

### 指标
```
GET/POST /api/standard/metric-categories
PUT/DELETE /api/standard/metric-categories/:id

GET/POST /api/standard/metrics
GET/PUT/DELETE /api/standard/metrics/:id
POST /api/standard/metrics/:id/approve
POST /api/standard/metrics/:id/deprecate
```

### 标准文档
```
GET/POST /api/standard/documents
GET/PUT/DELETE /api/standard/documents/:id
POST /api/standard/documents/:id/upload    # 上传文件到 MinIO
GET /api/standard/documents/:id/download   # 从 MinIO 下载
GET/PUT /api/standard/documents/:id/mappings # 多维关联（数据元/术语/指标）
```

### 维度层级
```
GET/POST /api/standard/dimension-hierarchies
GET/PUT/DELETE /api/standard/dimension-hierarchies/:id
GET/POST/PUT/DELETE /api/standard/dimension-hierarchies/:id/levels
```

## 前端路由

```
/standard/domains                    # 业务域（树形管理）
/standard/glossaries                 # 业务术语列表
/standard/glossaries/:id             # 术语详情（属性、关联数据元、文档）
/standard/elements                   # 数据元列表
/standard/elements/:id               # 数据元详情（规格、质量规则、文档）
/standard/code-sets                  # 码值集列表
/standard/code-sets/:id              # 码值集详情（码值项管理）
/standard/units                      # 计量单位（含度量类别）
/standard/classifications            # 数据分类与分级（合并页）
/standard/metrics                    # 指标列表
/standard/metrics/:id                # 指标详情（依赖关系、关联数据元）
/standard/documents                  # 标准文档库
/standard/dimension-hierarchies      # 维度层级列表
/standard/dimension-hierarchies/:id  # 维度层级详情（层次定义）
```

## 模块依赖关系

**依赖**:
- **System 模块**: JWT 认证、用户信息（`SYSTEM_URL`）
- **MinIO**: 标准文档文件存储（bucket: `standard`）

**被依赖**（其他模块调用 Standard 的 API）:
- **Model 模块**: 验证 domain_id、element_id、hierarchy_id、metric_id；代理标准对象查询

资产发现固定使用 `/api/v1/standard/assets/discoverable`，只接受 `addp-asset` Tenant Service Access Token，并校验 `standard.metric.read`；Tenant 只来自 canonical AuthContext。其他 `/api/v1/standard` 路由同样只接受 canonical Bearer Tenant AuthContext；文档下载还可使用 Standard owner 的 Browser Resource Ticket。

## IAM Permission 所有权

Standard 是以下第一批 Permission 的唯一 owner：

- `standard.domain.*`
- `standard.element.*`
- `standard.metric.*`
- `standard.code_set.*`
- `standard.document.*`
- `standard.glossary.*`
- `standard.unit.*`
- `standard.classification.*`
- `standard.dimension_hierarchy.*`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Standard 服务启动时不向 System 动态注册 Permission。

Measurement Category 是 Unit 聚合内子资源，Grading Level 是 Classification 聚合内子资源，Dimension Hierarchy Level 是 Dimension Hierarchy 聚合内子资源。Document 与 Element、Glossary、Metric 的关联操作按涉及资源 Permission 做 all-of 校验，不借用宽泛 Key 或前缀匹配授权。

## 特殊设计

### 指标三种类型的关系

```
atomic（原子指标）
  ├─ 关联一个或多个 elements（数据来源）
  └─ 作为 derived 的 base_metric_id

derived（派生指标）
  ├─ base_metric_id → atomic 指标
  └─ derivation_config（JSONB）存储派生规则

composite（复合指标）
  └─ metric_dependencies 表记录依赖哪些 atomic/derived 指标
```

### 术语/数据元/指标的状态流转

```
draft → approved → deprecated
               ↑
           （可从 deprecated 恢复至 draft）
```

### 文档关联的多维设计

同一篇文档可同时关联：
- 多个数据元（`document_element_mappings`）
- 多个术语（`document_glossary_mappings`）
- 多个指标（`document_metric_mappings`）

每条关联记录 `reference_location`（在文档中的引用位置），便于溯源。

### 数据分级固定四级

分级（GradingLevel）的 L1-L4 记录由系统初始化脚本预置，不支持用户创建/删除，只能修改各级的名称、描述和颜色。避免各租户因随意增删分级导致数据不一致。

### 无跨 Schema 外键

Standard 的 `elements`、`units`、`code_sets` 等被其他 Schema（model、metadata 等）引用时，**没有数据库级外键约束**，通过应用层 HTTP 调用 Standard API 进行 ID 存在性验证。

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

4. **树形结构查询**: Domain、Classification、MetricCategory 均为树形结构，查询时通过递归或应用层组装。

5. **`DocumentPanel` 组件**: 前端复用组件，用于在各详情页嵌入文档关联管理，避免重复实现。

6. **前端 API 统一入口**: 所有 API 调用集中在 [frontend/src/api/standard.js](frontend/src/api/standard.js)。

## 前端公开路由

- 模块内 Router 使用 `/domains`、`/glossaries`、`/elements`、`/code-sets`、`/metrics`、`/dimension-hierarchies` 等无模块前缀路径；Console 公开 URL 统一加 `/standard` 前缀。
- 术语、数据元、码集、指标和维度层级详情使用 `/:id` 表达对象身份；详情返回使用明确列表路由，不依赖 `router.back()`。
- 创建成功进入详情使用 `replace`，列表进入详情和跨标准对象导航使用 `push`。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。
