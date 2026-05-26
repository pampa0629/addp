# Graph 模块本体管理改进规划

## Context

当前 Graph 模块本体管理（Ontology）已完成基础 CRUD，但存在以下核心缺陷，导致其在实际使用中几乎不可用：

- 本体数据结构（属性 JSONB、约束 JSONB）虽已在后端定义，但前端完全没有对应的编辑界面
- 本体只能以枯燥的表格形式查看，无法可视化实体与关系的图谱结构
- 创建的约束也无法同步到 Neo4j
- 缺乏从已有资产（Model 模块的数据模型、Neo4j 已有数据）快速生成本体的能力

本次改进旨在将本体管理从"有 CRUD 的框架"升级为"可实际投入使用的本体建模工具"。

---

## 功能改进清单（按优先级）

### P0 — 补全缺失的基础 UI

#### ✅ F1. 属性管理面板（纯前端，零后端改动）

**问题**：`entity_types` 和 `relation_types` 表中已有 `properties JSONB`，`PropertyDefinition` 结构完整（name/label/data_type/required/unique/description），但 `OntologyDetail.vue` 的编辑对话框中完全没有属性字段的输入入口。

**方案**：扩大对话框宽度至 750px，在"描述"字段下增加"属性定义"可编辑行内表格：

```
属性定义
┌──────────────────────────────────────────────────────┐
│ 字段名(name)  显示名  数据类型      必填  唯一  操作 │
│ name          姓名    string         ✓           [删] │
│ age           年龄    integer                    [删] │
│ [+ 添加属性]                                         │
└──────────────────────────────────────────────────────┘
```

数据类型下拉：`string / integer / float / boolean / date / datetime`

提交时将 `properties` 数组传入已有的创建/更新接口（后端接口已支持，只是前端未使用）。

**改动文件**：`graph/frontend/src/views/OntologyDetail.vue`

---

#### ✅ F2. 本体可视化编辑器（新增 Tab）

**问题**：OntologyDetail 仅有表格视图，无法直观展示实体与关系的拓扑结构。

**方案**：直接采用 `common-frontend/graph/` 中的 G6 版本（`@antv/g6@^4.8.24`）。在 `common-frontend/graph/` 中新增 `OntologyView.vue` 组件，`graph/frontend` 统一使用此组件，原 `GraphCanvas.vue` 也同步迁移为使用 common-frontend 的 G6。



**第一步：在 `common-frontend/graph/` 新增 `OntologyView.vue` 组件**

组件职责：纯展示，接受本体数据，通过 G6 v4 渲染节点-边图，不包含业务逻辑（CRUD 对话框由 graph/frontend 负责）。

```javascript
// Props
props: {
  entityTypes: Array,   // [{id, name, label, color, parent_id}]
  relationTypes: Array, // [{id, name, label, source_type_id, target_type_id, directed, color}]
  readonly: Boolean,    // true=只读模式（默认false）
}

// Events
emits: ['node-click', 'edge-click', 'canvas-click', 'edge-create']
// edge-create: 用户拖拽创建边时触发 { sourceId, targetId }
```

数据转换规则：
```javascript
nodes: entityTypes.map(et => ({
  id: String(et.id),
  label: et.label || et.name,
  style: { fill: et.color || '#6366f1', stroke: '#fff' }
}))
edges: [
  // 关系类型：实线有向边
  ...relationTypes.filter(rt => rt.source_type_id && rt.target_type_id).map(rt => ({
    id: `rel_${rt.id}`, source: String(rt.source_type_id), target: String(rt.target_type_id),
    label: rt.label || rt.name
  })),
  // 继承关系：虚线边
  ...entityTypes.filter(et => et.parent_id).map(et => ({
    id: `inherit_${et.id}`, source: String(et.parent_id), target: String(et.id),
    label: '继承', style: { lineDash: [4, 4], stroke: '#aaa' }
  }))
]
```

布局：`dagre`（分层），适合本体层次结构展示。

在 `common-frontend/graph/src/index.js` 中导出：
```javascript
export { default as OntologyView } from './OntologyView.vue'
```

**第二步：在 `graph/frontend` 中使用**

在 `OntologyDetail.vue` 的 el-tabs 中新增第四个 Tab **"图形视图"**，嵌入 OntologyView 组件，并提供操作面板：

```
┌────────────────────────────────────────────────────────────┐
│ [实体类型] [关系类型] [版本历史] [图形视图]                   │
├──────────────────────────────────────────────────────────── │
│                    本体图 (OntologyView)                     │
│   节点=EntityType(带颜色)                                    │
│   实线有向边=RelationType(带标签)                            │
│   虚线边=继承关系(subClassOf)                               │
│   点击节点→右侧显示属性列表                                  │
└────────────────────────────────────────────────────────────┘
```

**迁移 GraphCanvas.vue**：将现有图谱浏览器中的 GraphCanvas.vue 替换为使用 common-frontend/graph 中的 GraphResultView（功能等价），统一使用 common-frontend 的 G6 版本，graph/frontend 的 package.json 中移除直接 G6 依赖。

**改动文件**：
- 新建：`common-frontend/graph/src/OntologyView.vue`
- 修改：`common-frontend/graph/src/index.js`（新增导出）
- 修改：`graph/frontend/src/views/OntologyDetail.vue`（新增 Tab，引用 OntologyView）
- 修改：`graph/frontend/src/components/GraphCanvas.vue`（迁移至 GraphResultView）
- 修改：`graph/frontend/package.json`（调整 G6 版本为 v4，与 common-frontend 一致）

---

#### ✅ F3. 约束管理界面 + Neo4j 约束同步（前后端均需改动）

**问题**：用户无法为实体类型的属性设置唯一性约束，也无法将约束同步到关联的 Neo4j 实例。

**前端方案**（`OntologyDetail.vue` 属性对话框）：
- 属性行增加"唯一"复选框（映射到已有 `PropertyDefinition.unique`）
- 对话框底部增加"同步约束到 Neo4j"按钮（选择关联图谱后执行）

**后端新增 API**：
```
POST /api/v1/graph/ontologies/:id/entity-types/:eid/sync-constraints
Body: { "graph_id": 1 }

GET  /api/v1/graph/graphs/:id/constraints   // 查询当前 Neo4j 已有约束
```

**后端实现**（`neo4j_service.go` 新增）：
```go
func (s *Neo4jService) SyncConstraints(ctx context.Context, graphID int64,
    tenantID int64, entityTypeName string, props []models.PropertyDefinition) error {
    for _, prop := range props {
        if !prop.Unique { continue }
        constraintName := fmt.Sprintf("graph_%d_%s_%s_unique",
            graphID, entityTypeName, prop.Name)
        cypher := fmt.Sprintf(
            "CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.%s IS UNIQUE",
            constraintName, entityTypeName, prop.Name)
        // 执行 Cypher（注意：DDL 需确认 dbbridge 是否支持，如不支持需走 Neo4j driver 直接调用）
    }
}
```

约束命名规则：`graph_{graphID}_{entityTypeName}_{fieldName}_unique`（幂等，使用 `IF NOT EXISTS`）

**注意**：唯一性约束 Neo4j 社区版支持；存在性约束（NOT NULL）需企业版，UI 中需标注区分。

**改动文件**：
- `graph/backend/internal/service/neo4j_service.go`（SyncConstraints/ShowConstraints）
- `graph/backend/internal/api/ontology_handler.go`（SyncConstraints handler）
- `graph/backend/internal/api/browse_handler.go`（GetConstraints handler）
- `graph/backend/internal/api/router.go`（注册新路由）
- `graph/frontend/src/views/OntologyDetail.vue`（约束 UI）
- `graph/frontend/src/api/ontology.js`（新增 API 函数）

---

### P1 — 跨模块集成

#### ⬜ F4. 从 Model 模块导入本体

**问题**：用户已在 Model 模块中建好了数据模型（实体、属性、关系），重新在 Graph 中定义本体是重复劳动。

**后端：在 common/client/ 新增 model.go**

遵循 `common/client/meta.go` 的模式，新建 `common/client/model.go`：

```go
package client

type ModelClient struct {
    baseURL     string
    httpClient  *http.Client
    internalKey string
    tenantID    *uint
}

func NewModelClient(baseURL, authToken string) *ModelClient { ... }
func NewModelClientWithInternalKey(baseURL, internalKey string) *ModelClient { ... }

// 核心方法（对应 model 模块 API）
func (c *ModelClient) ListEntities(tenantID uint) ([]ModelEntity, error)
func (c *ModelClient) GetEntity(id uint) (*ModelEntity, error)           // 含属性
func (c *ModelClient) ListEntityRelations(tenantID uint) ([]ModelEntityRelation, error)

// DTO 定义
type ModelEntity struct {
    ID          uint                    `json:"id"`
    Name        string                  `json:"name"`
    Code        string                  `json:"code"`
    Description string                  `json:"description"`
    Attributes  []ModelEntityAttribute  `json:"attributes"`
}
type ModelEntityAttribute struct {
    Name       string `json:"name"`
    DataType   string `json:"data_type"`
    Nullable   bool   `json:"nullable"`
}
type ModelEntityRelation struct {
    ID           uint   `json:"id"`
    Name         string `json:"name"`
    SourceEntity uint   `json:"source_entity"`
    TargetEntity uint   `json:"target_entity"`
    RelationType string `json:"relation_type"` // one_to_one/one_to_many/many_to_many
}
```

**后端新增 API**（graph 模块）：
```
GET  /api/v1/graph/ontologies/import-preview/from-model
POST /api/v1/graph/ontologies/:id/import-from-model
Body: {
  "entity_ids": [1,2,3],
  "relation_ids": [1,2],
  "conflict": "skip"   // "skip" | "overwrite"
}
```

**字段映射规则**：
| Model 字段 | Graph 字段 |
|-----------|-----------|
| `Entity.code` | `EntityType.name` |
| `Entity.name` | `EntityType.label` |
| `EntityAttribute.name` | `PropertyDefinition.name` |
| `EntityAttribute.data_type` | `PropertyDefinition.data_type` |
| `EntityAttribute.nullable=false` | `PropertyDefinition.required=true` |
| `EntityRelation.name` | `RelationType.name` |
| `EntityRelation.relation_type=one_to_many` | `RelationType.directed=true` |

**配置**（`graph/backend/internal/config/config.go` 新增）：
```go
ModelServiceURL string `mapstructure:"MODEL_URL"` // 默认 http://localhost:8181
```

**前端**（两步向导对话框，800px）：
```
步骤 1/2：选择实体
☑ Person (人物) - 3个属性   ⚠ 已存在同名，[覆盖|跳过]
☑ Company (公司) - 5个属性

步骤 2/2：选择关系
☑ Person -[WORKS_AT]→ Company（一对多，有向）
☑ Person -[KNOWS]→ Person

[取消] [上一步] [导入 (2实体 + 2关系)]
```

**改动文件**：
- 新建：`common/client/model.go`（ModelClient）
- 新建：`graph/backend/internal/service/model_import_service.go`
- 修改：`graph/backend/internal/config/config.go`（添加 ModelServiceURL）
- 修改：`graph/backend/internal/api/ontology_handler.go`（新增 handler）
- 修改：`graph/backend/cmd/server/main.go`（依赖注入 ModelClient）
- 修改：`graph/backend/internal/api/router.go`
- 新建：`graph/frontend/src/components/ImportFromModelDialog.vue`
- 修改：`graph/frontend/src/views/OntologyDetail.vue`（触发按钮）
- 修改：`graph/frontend/src/api/ontology.js`

---

#### ✅ F5. 从 Neo4j 图库推导本体

**问题**：对已有 Neo4j 数据库（非通过 ADDP 构建的），用户需要手动逐一定义本体，效率极低。

**实现了两条推导路径**：

**F5a（从已有图谱推导）**：在 `KnowledgeGraphList.vue` 中对每个图谱提供"推导本体"操作，通过 `GET /graphs/:id/infer-schema` 预览、`POST /graphs/:id/infer-schema/apply` 应用。

**F5b（从 Neo4j 引擎直接推导，无需图谱）**：本体建模可完全脱离图谱，直接选择 system 已注册的 Neo4j 引擎进行推导。入口位于 `OntologyDetail.vue` 页面的"从 Neo4j 推导"按钮。
```
GET  /api/v1/graph/ontologies/neo4j-engines                          // 列出 system 注册的 Neo4j 引擎
GET  /api/v1/graph/ontologies/infer-schema/from-engine?engine_id=X  // 推导预览（不写库）
POST /api/v1/graph/ontologies/:id/infer-schema/from-engine/apply    // 应用到本体
Body: {
  "engine_id": 1,
  "entity_type_names": ["Person","Company"],
  "relation_type_keys": ["WORKS_AT|Person|Company"],
  "conflict": "skip"
}
```

**推导 Cypher**（`schema_inference_service.go` 的 `inferWithEngine`）：
```cypher
-- 获取所有节点形状
MATCH (n) RETURN labels(n) AS labels, count(n) AS cnt ORDER BY cnt DESC LIMIT 500

-- 对每个节点形状采样属性 key
MATCH (n:`Person`) UNWIND keys(n) AS k RETURN DISTINCT k LIMIT 1000

-- 提取关系模式（来源节点形状 + 关系类型 + 目标节点形状）
MATCH (a)-[r]->(b)
RETURN labels(a) AS src, type(r) AS rel, labels(b) AS tgt, count(r) AS cnt
LIMIT 500
```

**前端两步流程**（`InferFromEngineDialog.vue`）：
```
第一步：选择 Neo4j 引擎
[Engine A ▼]  [开始推导]

第二步：推导结果预览
发现 5 个节点形状，12 个关系模式
┌──────────────────────────────────────────────────────┐
│ 节点形状：☑ Person (新增) count=1203 props: name/age │
│          ☐ Company (已存在)                          │
│ 关系模式：☑ WORKS_AT Person→Company (新增)           │
└──────────────────────────────────────────────────────┘
冲突策略：○ 跳过已存在  ○ 覆盖已存在
[应用选中项 (1实体 + 1关系)]
```

**改动文件**：
- 修改：`graph/backend/internal/service/schema_inference_service.go`（重构，新增 `ListNeo4jEngines`/`InferSchemaFromEngine`/`inferWithEngine`/`ApplyInferredSchemaFromEngine`/`applyPreview`）
- 修改：`graph/backend/internal/api/ontology_handler.go`（新增 `ListNeo4jEngines`/`InferSchemaFromEngine`/`ApplyInferredSchemaFromEngine` handler）
- 修改：`graph/backend/internal/api/browse_handler.go`（F5a 的 `InferSchema`/`ApplyInferredSchema` handler）
- 修改：`graph/backend/internal/api/router.go`（F5a + F5b 路由，静态路径置于 `/:id` 之前）
- 新建：`graph/frontend/src/components/InferFromEngineDialog.vue`（F5b 对话框）
- 新建：`graph/frontend/src/components/SchemaInferenceDialog.vue`（F5a 对话框）
- 修改：`graph/frontend/src/views/OntologyDetail.vue`（"从 Neo4j 推导"按钮，F5b 入口）
- 修改：`graph/frontend/src/views/KnowledgeGraphList.vue`（F5a 入口）
- 修改：`graph/frontend/src/api/ontology.js`（新增 `listNeo4jEngines`/`inferSchemaFromEngine`/`applyInferredSchemaFromEngine`）
- 修改：`graph/frontend/src/api/browse.js`（F5a 相关 API）

---

### P2 — 专业性补充

#### ⬜ F6. 本体 JSON 导出

```
GET /api/v1/graph/ontologies/:id/export    // 导出为 JSON 文件下载
```

用途：本体备份、跨环境迁移。导出内容：ontology 基本信息 + 所有 entity_types（含 properties、constraints）+ 所有 relation_types。

#### ⬜ F7. 本体版本 Diff 视图

在版本历史 Tab 中，选择两个版本后展示差异：新增/删除/修改的实体类型、关系类型和属性，以高亮的 before/after 对比形式呈现。

---

## 数据模型变更

**无需修改数据库 schema。**

仅修改 `graph/backend/internal/models/requests.go`，将约束结构化：

```go
// ConstraintDefinition 将 constraints 从 map[string]interface{} 结构化
type ConstraintDefinition struct {
    Type  string `json:"type"`   // "unique" | "not_null"（not_null 需 Neo4j 企业版）
    Field string `json:"field"`  // 属性字段名
    Name  string `json:"name"`   // 约束名（空则自动生成）
}

// 替代 CreateEntityTypeRequest / UpdateEntityTypeRequest 中的
// Constraints map[string]interface{} → Constraints []ConstraintDefinition
```

---

## 实施顺序

| 阶段 | 功能 | 状态 | 工作量 | 主要改动位置 |
|------|------|------|--------|-------------|
| 1 | F1 属性管理 UI | ✅ 已完成 | 0.5天，纯前端 | OntologyDetail.vue |
| 2 | F2 本体可视化（基于 common-frontend/graph） | ✅ 已完成 | 2天，主要前端 | common-frontend/graph + OntologyDetail |
| 3 | F3 约束管理 + Neo4j 同步 | ✅ 已完成 | 2天，前后端 | neo4j_service.go + OntologyDetail |
| 4 | F4 从 model 导入 | ✅ 已完成 | 2-3天，主要后端 | common/client/model.go + graph 后端 |
| 5 | F5 Neo4j 推导本体（F5a 从图谱 + F5b 从引擎） | ✅ 已完成 | 2天，主要后端 | schema_inference_service.go + InferFromEngineDialog.vue |
| 6 | F6/F7 导出/Diff | ⬜ 待实施 | 1-2天 | 后端导出 API + 前端 diff 组件 |

---

## 关键风险与注意事项

1. **Neo4j 约束 DDL**：`CREATE CONSTRAINT` 为 DDL，需确认 `dbbridge.ExecuteGraphQuery` 是否支持 DDL 语句。如不支持，需在 Neo4jService 中增加直接通过 Neo4j Go driver 执行 DDL 的方法。

2. **model.go 跨服务调用**：认证使用 `X-Internal-API-Key` 头（与现有 meta.go 模式完全一致），配置项通过 `MODEL_URL` + `INTERNAL_API_KEY`（已有环境变量，无需新增 key）。

3. **Schema 推导性能**：大型图库的属性 key 提取可能耗时，设置 30s 超时，前端展示加载状态。优先尝试 APOC `apoc.meta.schema()`，检测不可用时降级为逐标签 Cypher 查询。

---

## 验收标准

1. 创建实体类型时可添加多个属性（字段名/类型/必填/唯一），保存后刷新属性仍在
2. 在"图形视图"Tab 中可看到实体类型节点（带颜色）、关系类型有向边和继承虚线边
3. 为属性设置唯一后点击"同步约束"，在 Neo4j 执行 `SHOW CONSTRAINTS` 可见对应约束（格式：`graph_{id}_{type}_{field}_unique`）
4. 从 Model 模块选择 2 个实体导入，本体中出现对应实体类型（含属性映射正确）
5. 对有数据的 Neo4j 图谱执行推导（F5a），展示节点形状和关系模式预览，选择后应用成功
6. 在本体详情页点击"从 Neo4j 推导"（F5b），直接选择已注册的 Neo4j 引擎，无需图谱即可完成本体推导和应用
