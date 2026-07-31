# Graph 模块总体规划

## Context

ADDP 已具备完整的 Neo4j 基础设施层（引擎注册、元数据扫描、数据预览、Cypher 查询、图查询服务发布）。
下一步目标是在此基础上构建**完整的知识图谱领域模块**，覆盖知识图谱的完整生命周期：
本体建模 → 图谱构建 → 图谱探索 → 图分析 → 知识服务。

本模块面向企业知识库、实体关系分析等场景，是 ADDP 从"数据管理平台"向"知识管理平台"演进的关键模块。

---

## 架构决策（已确认）

| 决策 | 结论 |
|------|------|
| 模块形态 | 独立的 `graph/` 模块，Go 后端 + Vue 前端 |
| 多图谱支持 | 支持，每个图谱对应一个 Neo4j database |
| 本体存储 | PostgreSQL（graph schema），与图谱数据分离 |
| 本体版本 | 支持多本体模型并行维护，含版本管理 |
| LLM 图谱构建 | 调用 Copilot 模块（新增 KG extraction chains），不在 graph 模块内实现 LLM |
| Agent 集成 | Agent 消费 Knowledge Service API（Graph RAG），Graph 是 Agent 的知识库 |
| 构建策略 | 高置信度自动写入 Neo4j，低置信度进入人工审核队列 |
| 图算法 | 优先 Cypher 封装常用算法，探测 Neo4j GDS 可用性后可升级 |
| 前端图可视化 | G6（蚂蚁开源，已在 common-frontend 中确认可用性） |

---

## 与 ADDP Graph Data Type 的边界

ADDP 平台层的 `graph` data type 表示一个图整体。Neo4j label、relationship type 和 endpoint pattern 是 graph item 的结构事实，进入 Meta 的 `attributes.type_info.graph`，不作为独立 meta item。

Graph 模块负责真正的图领域能力：

- 图谱本体建模。
- 图谱构建和审核。
- 图浏览、路径探索、属性面板。
- 图查询、知识服务和 Graph RAG。
- 图算法和结果可视化。

Manager 只做通用资源浏览和轻量预览，不维护 Graph 模块的领域模型。Graph 模块可以消费 Meta 中的 graph schema 摘要，但本体、执行映射、图算法参数、图可视化状态和知识服务 DTO 都属于 Graph 模块自己的边界，不写回 `common/datatype.GraphInfo`。

### 本体类型与 Neo4j 执行映射

Graph 模块里的本体 `EntityType` 是概念层定义，`EntityType.Name` 是本体概念标识，不应等同为 Neo4j label。Neo4j label 或 label set 是该概念在具体图引擎中的执行映射。

实体类型执行映射规则：

1. `Name`：本体概念标识，用于本体关系、模型导入、LLM schema、前端展示和 API 语义。
2. `NodeLabels`：Neo4j 执行映射，允许单 label 或 label set，例如 `["Person"]`、`["Employee","Person"]`。
3. 未显式设置 `NodeLabels` 时，Graph 模块可按实体类型继承链生成默认 Neo4j labels。
4. 从已有 Neo4j 数据推导本体时，推导结果写入 `EntityType.NodeLabels`，初始概念名可来自 node shape 名；后续用户重命名概念不应改变执行映射。
5. `RelationType.SourceTypeID` / `TargetTypeID` 指向本体实体类型；运行时查询、构建、空间图层和算法过滤需要 Neo4j label 时，必须通过 `NodeLabels` 或默认映射解析。
6. Neo4j 约束同步受 Cypher DDL 限制，只能作用于单个 label。当前策略使用 `NodeLabels[0]`；复合 label set 的约束策略如需增强，应在 Graph 模块单独设计。

实体类型展示与搜索规则：

1. `DisplayProperty` 是节点展示名称的唯一事实源，可引用本实体类型或祖先继承的字符串属性。
2. `DisplayProperty` 自动纳入该实体类型的 Neo4j full-text index；本类型自己定义的展示属性保存时必须规范化为 `searchable=true`，界面中不可取消。
3. 节点标题、搜索结果标题、路径与图分析结果中的节点名称统一使用 `DisplayProperty`。
4. 未配置展示字段或节点缺少对应属性值时，真实节点显示实体 ID；不得按 `name`、`title`、`label` 等字段名猜测展示字段。
5. 其他全文搜索字段仍由属性上的 `searchable=true` 显式声明，避免无边界索引和搜索噪声。

### 空间图层与业务图视图

Neo4j Spatial 图层属于 Graph 模块的本体配置和运行时能力，不进入 `common/datatype.GraphInfo`。空间图层链路必须区分：

| 概念 | 含义 |
|---|---|
| `EntityType.Name` | 本体实体类型概念标识 |
| `EntityType.NodeLabels` | 该实体类型落到 Neo4j 节点时使用的 label set |
| `SpatialLayerConfig.LayerName` | Neo4j spatial layer 标识，用于 spatial procedure 调用 |

空间图层选择、同步和算法执行传递的是 `LayerName`，不能把 `LayerName` 当作 Neo4j label。节点注册到空间图层时，节点匹配必须使用 `NodeLabels` 或继承链默认映射。

Graph 模块的浏览、知识服务、Schema 推导和图算法必须使用同一套业务图视角：Neo4j 插件、扩展或索引生成的内部节点和内部关系不进入业务图 schema、统计、预览、路径和算法投影。当前第一批过滤的 Neo4j Spatial 内部节点为 `SpatialLayer`，内部关系为 `RTREE_METADATA`、`RTREE_REFERENCE`、`RTREE_ROOT`。

---

## 模块功能划分

### 1. Ontology — 本体建模
定义知识图谱的"概念体系"，是整个模块的地基。

**核心能力：**
- 本体模型 CRUD（实体类型、关系类型、属性定义、约束规则）
- 支持多本体模型并行维护（一个租户可有多套本体，对应不同图谱）
- 继承关系建模（subClassOf 类型的层级）
- 从 Model 模块 ER 图导入（实体 → 实体类型，关系 → 关系类型）
- 从已有 Neo4j 图谱反向提炼本体（从现有数据归纳 Schema）
- 本体驱动 Neo4j 约束生成（唯一性约束、索引、属性存在约束）
- 本体版本管理（变更记录、版本间差异对比）
- 可视化本体编辑器（节点/边的拖拽式建模）

**数据模型（graph schema，PostgreSQL）：**
```
ontologies（本体模型）
entity_types（实体类型）
relation_types（关系类型）
attribute_defs（属性定义）
ontology_versions（版本记录）
knowledge_graphs（知识图谱实例，绑定 ontology + neo4j engine + database）
```

---

### 2. Build — 图谱构建
从各类原始材料通过 LLM 驱动构建图谱，用户几乎无感。

**核心能力：**
- 支持多种材料来源：文档（PDF/Word/TXT）、结构化数据（表格、CSV）、网页
- LLM 驱动的抽取流水线（调用 Copilot 模块，新增 KG extraction chains）：
  - 实体识别（对齐本体实体类型）
  - 关系抽取（对齐本体关系类型）
  - 属性抽取
  - 实体消歧/去重
- 置信度评分机制：高置信度自动写入，低置信度进入审核队列
- 人工审核界面（查看待审实体/关系，确认/拒绝/修正）
- 增量构建（向已有图谱追加新材料，不重建）
- 构建任务管理（任务进度、日志、结果统计）
- 构建前本体约束校验

**Copilot 模块扩展（需新增）：**
```
copilot/backend/
├── api/kg_extract_api.py          # 新增 KG 抽取 API 端点
├── chains/
│   ├── entity_extraction_chain.py  # 实体抽取 Chain
│   ├── relation_extraction_chain.py # 关系抽取 Chain
│   └── entity_disambiguation_chain.py # 实体消歧 Chain
└── pipelines/
    └── kg_build_pipeline.py       # KG 构建管道（仿 workflow_pipeline.py 模式）
```

---

### 3. Browse — 知识探索（图谱可视化）
以可视化交互方式探索知识图谱，是用户感知最直接的功能。

**核心能力：**
- G6 驱动的交互式图谱浏览器
- 浏览初始化使用单一 `browse snapshot` 事实源：一次读取完整节点形状和关系模式，派生 Schema、统计和聚合概览；Schema、统计和概览不得分别扫描图谱
- 默认概览按完整节点形状和关系类型聚合，不直接加载全量实体；聚合节点只属于探索视图，不是 Neo4j 实体
- 统一的展开探索：聚合节点展开代表性实体，实体节点按 1～3 跳展开局部子图
- 展开查询同时受节点预算和关系预算约束，关系预算只计算此前未返回的新关系；K-hop 分析复用同一展开语义，不单独枚举路径
- 按实体类型/关系类型过滤（本体感知，不是原始 Neo4j 标签）
- 全文实体搜索只查询本体属性中显式声明 `searchable=true` 的字符串属性以及实体类型的 `DisplayProperty`，并使用 Neo4j full-text index；不保留任意属性全图扫描路线
- 搜索前按当前本体声明校验并同步当前图谱数据库的全文索引，属性声明变化后以本体定义为唯一事实源
- 多种布局算法切换（力导向、层级、环形）
- 增量展开保留已有节点位置，仅对新增节点按 hop 和父节点分簇执行有限迭代的局部布局；样式变化和容器尺寸变化不得触发全图重排
- 按缩放级别控制标签和关系可见性，远景只展示概览语义
- 节点/关系属性面板（点击查看详情）
- 路径高亮显示（两个节点间的关系路径）
- 子图保存与分享

**浏览 API：**
```
GET  /graphs/{id}/browse-snapshot    同一图事实快照中的 Schema、统计和聚合概览
POST /graphs/{id}/search             基于本体 searchable 属性的全文索引搜索
POST /graphs/{id}/expand             聚合桶或实体的统一双预算展开
POST /graphs/{id}/path               两个实体间的最短路径
```

`browse-snapshot` 是浏览初始化唯一入口，不同时保留独立的 `schema`、`stats`、`overview` 路由。Knowledge Service 的统计和本体描述复用同一快照派生结果。

---

### 4. Analytics — 图算法分析
封装高层图分析能力，屏蔽 Cypher 复杂度。

**核心能力（分两级）：**

**基础级（Cypher 实现，不依赖 GDS）：**
- 最短路径（两节点间）
- 邻居分析（N 跳邻居统计）
- 连通分量
- 节点度中心性
- 实体统计（各类型实体/关系数量）

**高级级（依赖 Neo4j GDS，可选）：**
- PageRank（重要性排名）
- Louvain 社区发现
- Betweenness Centrality
- Node Similarity

**交互设计：**
- 算法选择面板（参数填写 → 一键执行）
- 结果可视化（在图浏览器中高亮显示结果节点）
- 分析结果导出（CSV/JSON）

---

### 5. Services — 知识服务 API
语义化、本体感知的知识图谱 API，区别于 Service 模块的通用图查询服务。

**核心端点设计：**
```
GET    /kg/{graphId}/entities/{type}           实体列表（按类型）
GET    /kg/{graphId}/entities/{type}/{id}      实体详情
GET    /kg/{graphId}/entities/{id}/neighbors   邻居节点（可按关系类型过滤、指定跳数）
POST   /kg/{graphId}/paths                     路径查找（A → B 的路径）
POST   /kg/{graphId}/subgraph                  实体中心的子图
GET    /kg/{graphId}/search?q=...              全文实体搜索
GET    /kg/{graphId}/ontology                  图谱本体描述
GET    /kg/{graphId}/stats                     图谱统计（实体数、关系数等）
```

**Graph RAG 集成（供 Agent 使用）：**
- 以上端点注册为 Agent 工具（仿现有 develop_tools.py 模式）
- 支持结构化知识查询："和阿里巴巴有合作关系的上市公司"

**与 Service 模块的边界：**
| 维度 | Service 图查询 | Graph Knowledge Service |
|------|--------------|------------------------|
| 抽象层次 | 低层（Cypher → REST） | 高层（语义、本体感知）|
| 目标用户 | 懂 Cypher 的开发者 | 应用集成者 / Agent |
| 场景 | 灵活自定义查询 | 标准化知识访问 |

---

## 与其他模块的集成关系

```
Model 模块
  ─── 导入 ER 图 ──→ Ontology（本体初始化）

Copilot 模块（扩展）
  ←── KG 构建请求 ─── Build（LLM 实体/关系抽取）

Agent 模块
  ─── 工具调用 ──→ Knowledge Service API（Graph RAG）

Meta 模块（现有）
  ─── 元数据扫描 ──→ 可触发"从 Neo4j 反向提炼本体"

System 模块（现有）
  ─── 引擎注册 ──→ Graph 模块绑定 Neo4j engine

Gateway
  ─── 路由 ──→ /api/graph/* → graph backend
              /kg/* → graph backend（knowledge service）
```

---

## 实施阶段划分

### 阶段一：基础架构 + 本体建模
**目标：** 搭建模块骨架，完成核心本体管理能力
**主要工作：**
- graph 模块脚手架（Go 后端 + Vue 前端，按新模块开发指南）
- PostgreSQL schema 设计和迁移脚本
- 本体模型 CRUD（实体类型、关系类型、属性定义）
- 多本体模型管理
- 本体版本管理
- 从 Model 模块 ER 图导入
- 从已有 Neo4j 反向提炼本体
- 本体驱动 Neo4j 约束生成
- 前端：本体可视化编辑器（G6）
- Gateway 路由注册

**验证方式：**
- 创建本体模型，定义实体类型和关系类型
- 成功将约束写入 Neo4j（验证唯一性约束生效）
- 从 Model 导入 ER 图并生成本体

---

### 阶段二：图谱构建（LLM 驱动）
**目标：** 实现从原始材料构建图谱的完整流水线
**主要工作：**
- Copilot 模块扩展（KG extraction API + chains + pipeline）
- Graph 模块 Build 功能（任务管理、材料上传、流水线调度）
- 置信度评分 + 自动写入逻辑
- 人工审核队列和审核界面
- 增量构建支持
- 构建任务监控（进度、日志）

**验证方式：**
- 上传一份文档，触发构建流水线
- 高置信度实体自动写入 Neo4j
- 低置信度实体出现在审核队列，人工确认后写入

---

### 阶段三：知识探索（图可视化）
**目标：** 交互式图谱浏览器
**主要工作：**
- G6 图谱浏览器组件（可提取到 common-frontend/map）
- 节点展开、过滤、搜索交互
- 本体感知的过滤面板
- 多布局算法
- 节点/关系属性面板

**验证方式：**
- 在浏览器中加载已构建的图谱
- 点击节点展开邻居，按关系类型过滤
- 全文搜索定位实体

---

### 阶段四：图算法分析
**目标：** 封装常用图分析算法
**主要工作：**
- 基础算法封装（最短路径、邻居分析、连通分量）
- 算法执行面板（前端）
- 结果可视化（图高亮）
- GDS 可用性探测 + 高级算法（可选）

**验证方式：**
- 选择两个节点，执行最短路径，图上高亮路径
- 执行连通分量分析，结果按分组着色显示

---

### 阶段五：知识服务 API + Agent 集成
**目标：** 对外暴露语义化知识 API，与 Agent 完成 Graph RAG 集成
**主要工作：**
- Knowledge Service API 端点实现
- API 鉴权（公开/私有）
- Agent 工具注册（KG 查询工具，仿 develop_tools.py 模式）
- Graph RAG 效果验证

**验证方式：**
- 通过 REST API 查询实体邻居，返回正确子图
- 在 Agent 对话中提问，Agent 正确调用 KG 工具并回答

---

## 关键文件（预期）

```
graph/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/           # HTTP handlers
│   │   ├── service/       # 业务逻辑
│   │   ├── repository/    # 数据访问
│   │   └── models/        # 数据库模型
│   └── migrations/        # PostgreSQL 迁移
├── frontend/
│   └── src/
│       ├── views/
│       │   ├── OntologyEditor.vue   # 本体可视化编辑器
│       │   ├── GraphBrowser.vue     # 图谱浏览器
│       │   ├── BuildManager.vue     # 构建任务管理
│       │   ├── ReviewQueue.vue      # 审核队列
│       │   └── Analytics.vue       # 图分析面板
│       └── api/
│           ├── ontology.js
│           ├── graphBuild.js
│           └── knowledgeService.js
└── CLAUDE.md

copilot/backend/（扩展）
├── api/kg_extract_api.py
├── chains/entity_extraction_chain.py
├── chains/relation_extraction_chain.py
└── pipelines/kg_build_pipeline.py

agent/backend/tools/（扩展）
└── kg_tools.py   # Knowledge Service API 工具
```

---

## 下一步

确认本规划后，按阶段逐一进行详细设计文档（保存至 docs/plan/graph-阶段N-详细设计.md），然后实施。
