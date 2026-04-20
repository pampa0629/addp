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
- 以节点为中心的展开探索（点击节点 → 展开邻居）
- 按实体类型/关系类型过滤（本体感知，不是原始 Neo4j 标签）
- 全文实体搜索（输入关键词 → 定位节点）
- 多种布局算法切换（力导向、层级、环形）
- 节点/关系属性面板（点击查看详情）
- 路径高亮显示（两个节点间的关系路径）
- 子图保存与分享

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
