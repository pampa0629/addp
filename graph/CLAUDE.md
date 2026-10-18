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
| 多图谱支持 | 每个图谱绑定业务图 Engine + 显式 catalog_path；支持 Neo4j 与 FalkorDB |
| 本体存储 | PostgreSQL（graph schema），与图谱数据分离 |
| 本体版本 | 支持多本体模型并行维护，含版本快照管理 |
| AI 图谱构建 | Graph 使用 `addp-graph` Tenant Service Access Token 调用 Copilot `knowledge_graph_extraction` 场景；Graph 不直连 Inference 或厂商模型 |
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
| `knowledge_graphs` | 知识图谱实例（绑定本体 + 业务图 engine + catalog_path + index_management） |

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
│       │   └── browse_handler.go    # 图谱浏览 handler（browse-snapshot/search/expand/path）
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
│           ├── graph_data_service.go # 图谱领域查询与写入组合；原生差异由 Common Provider 承担
│           └── ontology_semantics.go # 本体执行映射和全文索引定义
└── frontend/
    ├── package.json                 # @antv/g6 依赖
    ├── vite.config.js               # port 5187, base=/graph/
    └── src/
        ├── api/
        │   ├── auth.js
        │   ├── client.js
        │   ├── ontology.js          # ontologyAPI + knowledgeGraphAPI
        │   └── browse.js            # browseAPI（getBrowseSnapshot/searchNodes/expandTarget/findPath）
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

所有路由通过 Gateway (`/api/v1`) 转发。管理接口和私有知识服务使用 System User Access Token 与权威 AuthContext；仅显式公开图谱的知识服务允许匿名访问，数据保护门禁仍然生效。

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

GET    /api/v1/graph/graphs/:id/browse-snapshot          浏览快照（同一事实源派生 Schema、统计和聚合概览）
POST   /api/v1/graph/graphs/:id/search                   全文搜索节点 (body: {query, limit})
POST   /api/v1/graph/graphs/:id/expand                   展开目标 (body: {target, depth, node_limit, relationship_limit})
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

GET  /api/v1/graph/task-provider/tasks                              TaskProvider 构建任务列表
GET  /api/v1/graph/task-provider/tasks/:task_type/:id               TaskProvider 构建任务详情
POST /api/v1/graph/task-provider/tasks/:task_type/:id/execute       TaskProvider 执行构建任务
GET  /api/v1/graph/task-provider/executions/:execution_id           TaskProvider 构建执行状态
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
- 从业务图引擎的显式 graph leaf 推导本体时，选择器使用 System 的 `system.engine_catalog.read` 窄目录；预览与应用 API 在本体读取或更新权限之外还要求该目录权限。Graph 不返回完整引擎对象供前端选择。
- 图谱实例 CRUD 映射到 `graph.graph.*`；Schema 预览、统计、浏览、搜索、展开、路径和私有 Knowledge Service 查询使用 `graph.graph.read`。从图谱推导 Schema 并写入本体时同时要求 `graph.graph.read` 与 `graph.ontology.update`，不能用图谱更新权代替本体更新权。公开 Knowledge Service 由图谱的显式公开策略决定，不伪造 Principal Permission。
- 公开状态使用唯一的 `PUT /graphs/{id}` 更新契约和 `graph.graph.update`：可选 `is_public` 省略时不修改，显式 `false` 关闭公开；返回已持久化的完整图谱，前端使用响应更新状态并对无更新权限者禁用开关。认证请求仍按当前 Tenant 解析图谱，私有图谱须通过 `graph.graph.read`；匿名仅可访问明确公开的图谱，携带无效凭据不能降级为匿名。公开与私有读取随后均执行 Security 目标图门禁。验证复用 `make test-module MODULE=graph`、Graph Swagger 生成与 `make test-authorization`，不新增身份或源数据授权。
- 用户构建任务使用 `graph.build_task.*`；Graph TaskProvider 只允许 `addp-orchestrator` Service Client，并使用 `graph.task_provider.read|execute`。上传或删除材料使用 `graph.build_task.update`；重跑使用 `graph.build_task.execute`。
- Analysis 能力探测使用 `graph.analysis.read`，算法执行和空间图层同步使用 `graph.analysis.execute`。
- Review 列表/数量使用 `graph.review.read`，通过、拒绝和修改分别使用 `graph.review.approve/reject/update`；批量路由必须按请求 action 校验对应 Permission。

`delegable` 当前统一保守为 `false`，待 Graph Knowledge Service、Agent Tool 和 OAuth Scope 映射阶段逐项评审，不在首批目录中默认开放委托。

独立访问 Graph 根路径时，在 AuthContext 加载后依照 Console 的本体建模、知识图谱、图分析、知识服务顺序进入首个可访问页面；没有可访问页面则显示无权限。独立侧栏使用相同的页面准入条件与顺序，显式页面 URL 保持原样。

Graph 调用 Copilot 图谱抽取时必须从当前构建任务取得 `tenant_id`，通过 `GRAPH_SERVICE_CLIENT_SECRET` 换取短期 Tenant Service Access Token。不得发送共享 `X-Internal-API-Key`、客户端自报 Tenant Header 或厂商模型凭据。

### 图谱构建 execution 语义

- 启动时在任务定义行锁保护下检查 active execution，并原子创建 `pending` execution；`pending.started_at` 必须为空。
- worker 接管时在同一事务推进构建任务和 execution 为 `running`，并写入真实 `started_at`；终态同样在一个事务提交。
- 重跑只允许终态任务，材料进度、待审核项、任务摘要重置与新 pending execution claim 必须在同一事务完成。
- 取消只允许当前 Graph Backend 进程持有真实运行句柄时执行；取消请求等待 worker 停止并写入 `cancelled`，无句柄时返回冲突，不修改持久状态。取消中断当前材料时，材料必须恢复为可重跑的 `pending`，不得遗留 `processing` 状态或部分分块进度。
- 自动实体／关系写入、审核项入库或材料处理失败必须向上传播原始错误，失败分块不推进已处理进度。继续处理后续材料；任一材料失败，任务和 execution 最终同为 `failed`。已成功写入的数据、写入／待审统计和逐份材料错误保留，不新增部分成功状态，不回滚业务图。两类引擎共用此流程，最小验证为 Graph Go T1 和既有 Common FalkorDB／Neo4j T2。
- 抽取结果进入写图或审核队列前，实体类型必须在当前本体中存在，并且只能确定一个显式 `Unique` 属性，值必须非空；无键、多键、空值或未知实体／关系类型使当前材料失败。数字零和布尔假不作为空值；不默认按 name 合并，不自动生成 UUID，也不按属性顺序猜合并键。此规则用于构建，不改变现有业务图的浏览和查询。

## 七、开发与调试

```bash
# 独立启动
bash scripts/dev/start.sh -graph

# 重启后端
bash scripts/dev/restart.sh -graph

# 访问
# 前端：http://localhost:5187
# API：http://localhost:8186/health/ready
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

## 十、前端公开路由

- 模块内 Router 使用 `/ontologies`、`/graphs`、`/analysis`、`/knowledge-service` 等无模块前缀路径；Console 公开 URL 统一加 `/graph` 前缀。
- 本体、图谱和构建任务使用 path parameter 表达对象身份；创建成功后用 `replace` 进入详情，列表进入详情使用 `push`。
- 本体详情默认 `entities` Tab 省略，其他稳定 Tab 使用 `tab`；审核队列默认 `entity` 省略；知识服务当前图谱使用 `graph_id`。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`，不得直接操作父窗口或在页面中调用 `router.push/replace/back`。
- 图构建页只保留当前图谱的任务定义、材料、审核和单任务领域上下文；模块级与单任务执行历史都通过共享 `MonitorExecutionsButton(module=graph, task_type=kg_build)` 进入 Monitor，不新增 Graph 通用执行列表。


## 业务图引擎与索引管理

Graph HTTP Tenant 与 User 身份只从权威 AuthContext 提取，缺失返回零值并由既有 Guard 拒绝，不保留默认租户或旧 Gin key 回退。

Graph 是 Security 的固定保护消费者，使用本地 `common/dataprotection/projectionstore` 和显式目标图的原生读取门禁；首次投影同步完成前拒绝读取。浏览、搜索、展开、路径、推导、分析及公开/私有知识服务均检查同一目标，命中纳管时拒绝，不提供属性脱敏或原值豁免。领域操作的在途生命周期覆盖完整的服务组合，结束前不得向 Security 确认新投影已安装。System 源数据授权暂未接入，不以本门禁替代。验证复用 Graph/Security/System 模块门禁、Security PostgreSQL 以及 Common Neo4j/FalkorDB 独占实例门禁；新增服务测试须被现有门禁筛选命中。

GraphDataService 是浏览、构建审核、Schema 推导和知识服务的唯一图谱数据组合入口。目标使用 EngineCatalogPath，原生 ID 表达式、全文索引和唯一约束由 Common PropertyGraphProvider 提供，不修改引擎连接的 database 配置。Neo4j 路径为 server/database/graph，FalkorDB 为 server/graph，原生 graph key 作为完整一段保存。

创建图谱必须明确选择 `index_management=managed|external`，同一租户与 Engine 的同一 graph 只能绑定一个实例。managed 按本体维护索引；external 只复用已有完全匹配索引，缺失或冲突返回 409，绝不自动修改。FalkorDB 同 label 多定义取字段并集创建物理索引，搜索仍按每个定义的字段和标签执行。基础度中心性与 K 跳已验证；FalkorDB 不声明 GDS、Spatial 或多路最短路径。单条无向最短路径采用共享逐层遍历，最多 10 跳、每层最多 10000 行、最多访问 10000 节点；超出预算返回 400，不作为无路径结果。

两类引擎的同一领域 T2 实际执行 K 跳分析，分别核对一跳、两跳的中心节点、邻居身份和关系数量；不能仅凭能力声明或展开接口测试认定算法已通过。目标路径使用共享图路径校验，结构根不带业务名称，畸形路径必须在连接或查询准备前拒绝，不能静默规范化成另一条路径。

FalkorDB 现有独占 T2 同时提供明文和 TLS 测试端口，复用同一固定镜像及模块配置。测试证书和私钥仅在仓库外临时目录生成并随门禁清理，验证显式 CA 信任、主机名匹配和真实 PreparedQuery；缺少信任或主机名不符必须保留证书错误，不能降级连接。该结果只证明插件 TLS 连接能力，不声明 Business/Infra 已启用 TLS 或生产 HA/TLS 已通过发布认证。

数据库结构统一由 MigrateGraphTables 原子迁移并删除退役 database/neo4j_id 列。不自动猜测已有旧图谱实例的目录路径或索引管理范围；非空旧绑定无法满足新结构时，迁移失败并完整回滚，需显式重新配置；不删除业务图数据。

本体删除沿已有 `graph.ontology.delete` 聚合契约，在同一事务锁定当前租户的本体，删除其关系类型、实体类型和版本后删除本体；其他租户及本体不受影响。已有图谱绑定仍由外键保护，删除失败时所有定义和版本完整回滚，不级联删除图谱或业务图数据。真实外键、绑定拒绝、回滚与租户隔离回归由既有 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"` 自动发现。

验证入口：`make test-module MODULE=graph GO_ONLY=1`、`make test-graph-frontend`、`make test-common-falkordb`、`make test-common-neo4j`、`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"`。PostgreSQL 分组验证目标路径保真、目标唯一性、旧字段移除、失败回滚和构建 execution 并发；默认 Common PostgreSQL 完整门禁包含该分组。Graph 路径已在三项 Common T2 的输入元数据登记，因此 Graph 后端变更会自动选中 Common owner 的既有 CI Job。Swagger 使用标准 graph 生成与覆盖入口。

隔离权限 T4 使用 `graph-falkordb-protection` suite，沿 `online-hosted-security-gate.sh` 和 `hosted-online.sh` 的唯一 Hosted 生命周期，由 `make test-online ONLINE_SUITE=graph-falkordb-protection` 执行。专用 FalkorDB 复用中央镜像契约，回环随机端口、tmpfs 数据、独立 Compose 资源；仅含两张各 3 个 Person、2 条 Link 的合成图。普通读取 User 仅获 `graph.graph.read`；准备者独立持有图谱配置、Meta 扫描和纳管所需 Permission，无权对照 User 不含 Graph 读取权。所有账号只存在于 disposable Hosted Tenant，不新增产品角色、源数据 Grant 或业务引擎授权。

上述 T4 从 Gateway 和 Graph 直连验证私有读取 200、无权 403、匿名 401，仅将合成测试图显式公开，先验证匿名读取 200，再经正式 Security 纳管并等待 develop/graph/manager/service/transfer 五 Owner 的当前投影确认后验证保护 403；同引擎未纳管对照图仍返回真实数据。拒绝必须匹配保护原因，不能以普通权限错误或连接失败代替。失败也须关闭公开状态、正式退出测试纳管并等待五 Owner、删除图谱与本体配置；标准 Hosted 清理继续销毁合成图数据库和临时 IAM 所在的完整测试 Infra，并核对容器、网络、卷零残留。现有业务图、Infra 私有图、external 索引和暂缓的 Graph 源数据授权均不参与。确定性回归由 `make test-security-online-runner`、聚合 `make test-online-runner` 及既有 Platform CI 覆盖，真实 T4 结果单独报告。

同一 suite 的 v2 报告还要求 Console 浏览器验收：在合成图保持私有、未纳管时，通过正式登录进入 Graph，核对当前浏览器 User、Tenant 和仅有的 `graph.graph.read` 与 HTTP 验收身份一致，验证浏览快照的 3 节点/2 关系、真实画布、全文搜索及知识服务搜索的三个实体，并确认公开开关不可操作、没有图谱配置写请求。浏览器复用相同合成资源，不生成额外账号、图或权限；报告和两张截图缺失、身份不符、任何非预期 API 失败或浏览器异常都使 suite 失败，随后仍执行同一清理。历史 v1 的纯 HTTP 结果不能作为普通账号 Console 已通过的证据。

v3 在保留上述 Graph 最小权限账号和验收的同时，复用两张合成图增加独立 Develop 普通账号。该账号仅有 `develop.task.read`、`develop.task.execute`、`develop.data_read.execute`、`system.execution_authorization.create`、`meta.catalog.read` 五项已确认的测试 Permission。正式登录工作台并选择 Meta 图资源后，纳管前验证 Cypher 的真实 3 节点/2 关系、两行图结果及表格视图，以及文本和整数参数的一行表格结果；五 Owner 确认后，同一 Develop User 的目标图执行必须以 `develop.query.protection_required` 在 preparation 阶段拒绝且不返回结果，同引擎对照图继续成功。两个阶段报告、四张截图、身份/权限或断言任何一项缺失均使同一 suite 失败；账号、执行记录及原生合成数据随 Hosted Infra 销毁。未扩大 Graph 源授权或增加属性脱敏。

FalkorDB 全文索引同步必须在原生 `db.indexes()` 报告全部所需 NODE 全文索引字段匹配且 `OPERATIONAL` 后返回；managed 新建/重建和 external 复用走同一就绪检查，最多等待30秒并遵守调用取消。构建中不能返回成功或空搜索结果；元数据异常、字段冲突、超时和查询错误保留为错误。该边界由 Common Provider 实现，Graph 与知识服务不增加重试或扫描回退。`make test-common-falkordb-unit` 覆盖状态、取消和错误，`make test-common-falkordb` 以独占合成大图验证同步后首次搜索完整返回、external复用及既有Graph/Develop领域契约。
