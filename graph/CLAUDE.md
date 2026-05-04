# Graph 模块说明

## 一、模块定位

Graph 模块是 ADDP 平台的**知识图谱领域模块**，覆盖知识图谱的完整生命周期：
**本体建模 → 图谱构建 → 图谱探索 → 图分析 → 知识服务**

面向企业知识库、实体关系分析等场景，是 ADDP 从"数据管理平台"向"知识管理平台"演进的关键模块。

## 二、端口配置

| 服务 | 开发端口 | Docker 端口 |
|------|---------|------------|
| Backend | 8186 | 8186 |
| Frontend | 5187 | 8117 |

## 三、架构决策

| 决策 | 结论 |
|------|------|
| 模块形态 | 独立 `graph/` 模块，Go 后端 + Vue 前端 |
| 多图谱支持 | 每个图谱对应一个 Neo4j database（engine+database 绑定） |
| 本体存储 | PostgreSQL（graph schema），与图谱数据分离 |
| 本体版本 | 支持多本体模型并行维护，含版本快照管理 |
| LLM 图谱构建 | 调用 Copilot 模块（KG extraction chains），不在 graph 内实现 LLM |
| Agent 集成 | Agent 消费 Knowledge Service API（Graph RAG） |
| 图算法 | 优先 Cypher 封装常用算法，探测 Neo4j GDS 可用性后升级 |
| 图可视化 | G6（蚂蚁开源） |

## 四、数据库设计

**PostgreSQL Schema: `graph`**（由 GORM AutoMigrate 自动创建）

| 表名 | 用途 |
|------|------|
| `ontologies` | 本体模型（一套知识图谱的概念体系） |
| `entity_types` | 实体类型定义，含继承关系（subClassOf） |
| `relation_types` | 关系类型定义，含来源/目标约束 |
| `ontology_versions` | 本体版本快照 |
| `knowledge_graphs` | 知识图谱实例（绑定本体 + Neo4j engine + database） |

## 五、代码结构

```
graph/
├── backend/
│   ├── cmd/server/main.go          # 入口
│   ├── go.mod
│   └── internal/
│       ├── api/
│       │   ├── router.go            # 路由（/api/graph/*）
│       │   ├── ontology_handler.go  # 本体 CRUD handler
│       │   ├── knowledge_graph_handler.go
│       │   └── browse_handler.go    # 图谱浏览 handler（schema/stats/overview/search/expand/path）
│       ├── config/config.go         # 配置（port 8186, schema=graph）
│       ├── models/
│       │   ├── ontology.go          # 数据库模型
│       │   ├── requests.go          # 请求/响应 DTO
│       │   └── browse.go            # 图浏览 DTO（GraphNodeDTO, GraphEdgeDTO, SubgraphResult...）
│       ├── repository/
│       │   ├── database.go          # DB 初始化 + AutoMigrate
│       │   ├── ontology_repository.go
│       │   ├── type_repository.go   # EntityType + RelationType
│       │   └── graph_repository.go  # KnowledgeGraph + OntologyVersion
│       └── service/
│           ├── ontology_service.go
│           ├── knowledge_graph_service.go
│           └── neo4j_service.go     # Neo4j 查询服务（GetSchema/GetStats/GetOverview/SearchNodes/ExpandNode/FindPath）
└── frontend/
    ├── package.json                 # @antv/g6 依赖
    ├── vite.config.js               # port 5187, base=/graph/
    └── src/
        ├── api/
        │   ├── auth.js
        │   ├── client.js
        │   ├── ontology.js          # ontologyAPI + knowledgeGraphAPI
        │   └── browse.js            # browseAPI（getSchema/getStats/getOverview/searchNodes/expandNode/findPath）
        ├── components/
        │   ├── Layout.vue           # 支持双模式（嵌入/独立）
        │   ├── GraphCanvas.vue      # G6 可视化核心组件（力导向/分层/环形/辐射布局）
        │   └── NodePanel.vue        # 节点/关系属性面板（右侧抽屉式）
        ├── router/index.js
        ├── store/auth.js
        └── views/
            ├── Login.vue
            ├── OntologyList.vue
            ├── OntologyForm.vue
            ├── OntologyDetail.vue   # 实体类型/关系类型/版本管理
            ├── KnowledgeGraphList.vue
            └── GraphBrowser.vue     # 交互式图谱浏览器（三栏布局，过滤/搜索/路径查找）
```

## 六、API 路由

所有路由通过 Gateway (`/api/v1`) 转发，需要 JWT 认证。

```
GET    /api/v1/graph/ontologies                          本体列表
POST   /api/v1/graph/ontologies                          创建本体
GET    /api/v1/graph/ontologies/:id                      本体详情（含 entity_types、relation_types）
PUT    /api/v1/graph/ontologies/:id                      更新本体
DELETE /api/v1/graph/ontologies/:id                      删除本体

GET    /api/v1/graph/ontologies/:id/entity-types         实体类型列表
POST   /api/v1/graph/ontologies/:id/entity-types         创建实体类型
PUT    /api/v1/graph/ontologies/:id/entity-types/:eid    更新实体类型
DELETE /api/v1/graph/ontologies/:id/entity-types/:eid    删除实体类型

GET    /api/v1/graph/ontologies/:id/relation-types       关系类型列表
POST   /api/v1/graph/ontologies/:id/relation-types       创建关系类型
PUT    /api/v1/graph/ontologies/:id/relation-types/:rid  更新关系类型
DELETE /api/v1/graph/ontologies/:id/relation-types/:rid  删除关系类型

GET    /api/v1/graph/ontologies/:id/versions             版本列表
POST   /api/v1/graph/ontologies/:id/versions             创建版本快照

GET    /api/v1/graph/graphs                              知识图谱列表
POST   /api/v1/graph/graphs                              创建知识图谱
GET    /api/v1/graph/graphs/:id                          知识图谱详情
PUT    /api/v1/graph/graphs/:id                          更新知识图谱
DELETE /api/v1/graph/graphs/:id                          删除知识图谱

GET    /api/v1/graph/graphs/:id/analysis/capabilities  算法能力探测（GDS/Cypher）
POST   /api/v1/graph/graphs/:id/analysis/run           执行图算法（度中心性/K跳/最短路径/PageRank/Louvain/WCC/介数中心性）

GET    /api/v1/graph/graphs/:id/schema                   图谱 Schema（标签 + 关系类型）
GET    /api/v1/graph/graphs/:id/stats                    图谱统计（节点数/关系数/按标签分组）
GET    /api/v1/graph/graphs/:id/overview                 概览子图（采样100条关系）
POST   /api/v1/graph/graphs/:id/search                   全文搜索节点 (body: {query, limit})
POST   /api/v1/graph/graphs/:id/expand                   展开节点邻居 (body: {node_id, limit})
POST   /api/v1/graph/graphs/:id/path                     最短路径查询 (body: {source_id, target_id})
```

## 七、开发与调试

```bash
# 独立启动
bash scripts/dev/start.sh -graph

# 重启后端
bash scripts/dev/restart.sh -graph

# 访问
# 前端：http://localhost:5187
# API：http://localhost:8186/health
```

日志文件：`logs/graph-backend.log`、`logs/graph-backend-stderr.log`

## 八、实施阶段状态

| 阶段 | 内容 | 状态 |
|------|------|------|
| 阶段一 | 基础架构 + 本体建模 | ✅ 已完成（基础 CRUD） |
| 阶段二 | 图谱构建（LLM 驱动） | 待实施 |
| 阶段三 | 知识探索（图可视化，G6） | ✅ 已完成（图谱浏览器） |
| 阶段四 | 图算法分析 | ✅ 已完成（度中心性/K跳/多路径/PageRank/Louvain/WCC/介数中心性） |
| 阶段五 | 知识服务 API + Agent 集成 | 待实施 |

## 九、相关文档

- [总体规划](docs/graph模块总体规划.md)
- [本体管理改进规划](docs/graph本体管理改进规划.md)
- [图谱构建详细规划](docs/graph阶段2-图谱构建详细规划.md)
- [ADDP 开发原则](../docs/spec/addp开发原则.md)
- [新模块开发指南](../docs/spec/addp新模块开发指南.md)
