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
├── authorization/
│   └── permissions.yaml          # Graph owner Permission Manifest
├── backend/
│   ├── cmd/server/main.go          # 入口
│   ├── go.mod
│   └── internal/
│       ├── api/
│       │   ├── router.go            # 路由（/api/v1/graph/*）
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
POST   /api/v1/graph/graphs/:id/analysis/sync-spatial  同步空间图层

GET    /api/v1/graph/graphs/:id/schema                   图谱 Schema（标签 + 关系类型）
GET    /api/v1/graph/graphs/:id/stats                    图谱统计（节点数/关系数/按标签分组）
GET    /api/v1/graph/graphs/:id/overview                 概览子图（采样100条关系）
POST   /api/v1/graph/graphs/:id/search                   全文搜索节点 (body: {query, limit})
POST   /api/v1/graph/graphs/:id/expand                   展开节点邻居 (body: {node_id, limit})
POST   /api/v1/graph/graphs/:id/path                     最短路径查询 (body: {source_id, target_id})

GET/POST   /api/v1/graph/graphs/:id/build/tasks                         构建任务列表/创建
GET/DELETE /api/v1/graph/graphs/:id/build/tasks/:tid                    构建任务详情/删除
POST       /api/v1/graph/graphs/:id/build/tasks/:tid/run                执行构建
POST       /api/v1/graph/graphs/:id/build/tasks/:tid/cancel             取消构建
POST       /api/v1/graph/graphs/:id/build/tasks/:tid/rerun              重新执行
GET/POST   /api/v1/graph/graphs/:id/build/tasks/:tid/materials          列出/上传构建材料
DELETE     /api/v1/graph/graphs/:id/build/tasks/:tid/materials/:mid     删除构建材料

GET  /api/v1/graph/graphs/:id/review                  审核项列表
GET  /api/v1/graph/graphs/:id/review/pending-count    待审数量
POST /api/v1/graph/graphs/:id/review/batch/approve    批量通过
POST /api/v1/graph/graphs/:id/review/batch/reject     批量拒绝
POST /api/v1/graph/graphs/:id/review/:iid/approve     通过审核项
POST /api/v1/graph/graphs/:id/review/:iid/reject      拒绝审核项
PUT  /api/v1/graph/graphs/:id/review/:iid             修改并通过审核项

GET  /api/v1/graph/tasks                              TaskProvider 构建任务列表
GET  /api/v1/graph/tasks/:task_type/:id               TaskProvider 构建任务详情
POST /api/v1/graph/tasks/:task_type/:id/execute       TaskProvider 执行构建任务
GET  /api/v1/graph/executions/:execution_id           Graph 构建 execution 详情

GET/POST /api/v1/graph/kg/:graphId/*                  可选认证的 Knowledge Service 查询
```

## IAM Permission 所有权

Graph 是以下 Permission 的唯一 owner：

- `graph.ontology.*`
- `graph.graph.*`
- `graph.build_task.*`
- `graph.analysis.*`
- `graph.review.*`

机器可读事实源是 [authorization/permissions.yaml](authorization/permissions.yaml)。该 Manifest 由 `common/authorization` 在构建/发布期统一发现、校验和聚合，Graph 服务启动时不向 System 动态注册 Permission。

路由与 Permission 语义映射固定如下：

- Ontology 的实体类、关系类、版本、Model 导入、Schema 推导和约束/空间映射都是 Ontology 聚合内部能力，按操作语义映射到 `graph.ontology.read/create/update/delete`。
- 图谱实例 CRUD 映射到 `graph.graph.*`；Schema、统计、浏览、搜索、展开、路径和私有 Knowledge Service 查询使用 `graph.graph.read`。公开 Knowledge Service 由图谱的显式公开策略决定，不伪造 Principal Permission。
- 构建任务和 Graph TaskProvider 的查询/执行分别使用 `graph.build_task.read/execute`；上传或删除材料使用 `graph.build_task.update`；重跑使用 `graph.build_task.execute`。
- Analysis 能力探测使用 `graph.analysis.read`，算法执行和空间图层同步使用 `graph.analysis.execute`。
- Review 列表/数量使用 `graph.review.read`，通过、拒绝和修改分别使用 `graph.review.approve/reject/update`；批量路由必须按请求 action 校验对应 Permission。

`delegable` 当前统一保守为 `false`，待 Graph Knowledge Service、Agent Tool 和 OAuth Scope 映射阶段逐项评审，不在首批目录中默认开放委托。

### 图谱构建 execution 语义

- 启动时在任务定义行锁保护下检查 active execution，并原子创建 `pending` execution；`pending.started_at` 必须为空。
- worker 接管时在同一事务推进构建任务和 execution 为 `running`，并写入真实 `started_at`；终态同样在一个事务提交。
- 重跑只允许终态任务，材料进度、待审核项、任务摘要重置与新 pending execution claim 必须在同一事务完成。
- 取消只允许当前 Graph Backend 进程持有真实运行句柄时执行；取消请求等待 worker 停止并写入 `cancelled`，无句柄时返回冲突，不修改持久状态。取消中断当前材料时，材料必须恢复为可重跑的 `pending`，不得遗留 `processing` 状态或部分分块进度。

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
| 阶段二 | 图谱构建（LLM 驱动） | ✅ 已完成（构建任务、材料、审核和抽取链路） |
| 阶段三 | 知识探索（图可视化，G6） | ✅ 已完成（图谱浏览器） |
| 阶段四 | 图算法分析 | ✅ 已完成（度中心性/K跳/多路径/PageRank/Louvain/WCC/介数中心性） |
| 阶段五 | 知识服务 API + Agent 集成 | 基础 Knowledge Service API 已完成，Agent 集成待实施 |

## 九、相关文档

- [总体规划](docs/graph模块总体规划.md)
- [本体管理改进规划](docs/graph本体管理改进规划.md)
- [图谱构建详细规划](docs/graph阶段2-图谱构建详细规划.md)
- [ADDP 开发原则](../docs/spec/addp开发原则.md)
- [新模块开发指南](../docs/spec/addp新模块开发指南.md)
