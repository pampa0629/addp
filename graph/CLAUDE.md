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

前端入口、列表和本体定义表单不应静态加载 G6。共享图翻译和轻量工具直接从对应文件导入；本体图形页签按需加载唯一的共享 `OntologyView`，图谱浏览页仍使用现有 G6 画布和布局。Element Plus 按页面实际依赖分块，不将整个组件库聚合为首屏 vendor；颜色、日期、时间及树选择器不得进入入口静态依赖。统一样式与开发预构建继续在启动时准备，避免首次导航重建依赖。`make test-graph-frontend` 的生产构建沿静态 chunk 导入链核对上述边界，Console 既有本体导航及四主题浏览器回归验证按需加载后的页面行为，不增加运行入口或依赖。

GraphCanvas 在布局完成后才执行搜索定位，避免用布局前的坐标启动平移动画，再被最终布局适配覆盖。多个搜索结果适配全体结果的视口；单个定位保留居中行为。GraphCanvas 自动适配只缩小超出视口的图，不自动放大小图；滚轮仍允许用户放大。放大时节点/关系文字最多显示为 14px，并按实际视口倍率补偿描边、虚线、光晕和方向箭头，保持自然线宽；节点位置和半径继续缩放，关系线型及选择颜色保持语义。Console 的 `graph-canvas-zoom.spec.js` 通过真实滚轮及 Canvas 绘制结果验证缩放边界、字号和线宽，不使用产品调试接口。

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
- 审核单项和批量均以 `tenant_id + graph_id + item_id` 限定范围，越界返回 404；已处理单项返回 409。批量先核对全部 ID 归属再执行，任一处理失败须返回失败并保留成功结果；详情见构建规划，不吞错或回滚业务图。

`delegable` 当前统一保守为 `false`，待 Graph Knowledge Service、Agent Tool 和 OAuth Scope 映射阶段逐项评审，不在首批目录中默认开放委托。

独立访问 Graph 根路径时，在 AuthContext 加载后依照 Console 的本体建模、知识图谱、图分析、知识服务顺序进入首个可访问页面；没有可访问页面则显示无权限。独立侧栏使用相同的页面准入条件与顺序，显式页面 URL 保持原样。

Graph 调用 Copilot 图谱抽取时必须从当前构建任务取得 `tenant_id`，通过 `GRAPH_SERVICE_CLIENT_SECRET` 换取短期 Tenant Service Access Token。不得发送共享 `X-Internal-API-Key`、客户端自报 Tenant Header 或厂商模型凭据。

### 图谱构建 execution 语义

- 唯一正式契约为[Graph构建任务](../docs/spec/tasks/模块任务契约.md#graph-构建任务)，构建已采用持久pending、单槽Backend监督器、租约事务和持久取消意图；删除了API启动运行体和进程内取消map。外部调用先登记操作，未知结果阻断后续写入。Cleanup仍待整体迁移，不能将构建门禁当作Cleanup通过。
- API沿正式准入与任务锁协议创建pending并冻结确定输入；Backend内嵌监督器复用Common lease领取。采用E→G→T／B组合事务、跨Backend持久取消意图和外部效果核清，进程内句柄不作为所有权事实。未启动pending直接取消的证明、运行体确认、迟到停止证据和人工重跑条件按正式契约，不增加公共状态或第二套队列。
- 取消／失联不自动重放图写入，不无差别清零已发生效果；材料检查点、生成待审项、计数及摘要受同一lease事务保护。人工审核为独立命令，不要求旧构建仍running；Cleanup关闭准入后必须释放锁，再逐项真实取消／核清，最终PG清理才与回执／子终态同事务。
- Cleanup首次领取按[父P→子C契约](../docs/spec/addp-cleanup体系规范.md#父关闭与子领取协议)与Graph领域锁组合，已领取后C→G→T/B不反向锁P；父永久关闭的未领取拒绝只由Graph Owner把本C收敛为failed，无运行attempt或清理回执，不执行Graph领域清理。该细则已获用户确认、尚未实施，不套用kg_build未启动取消例外。
- [协调关闭安全展示契约](../docs/spec/addp-cleanup体系规范.md#协调关闭与模块未开始的安全展示)已获用户确认、由牵头统一实施：Cleanup读取仍由Graph逐请求Owner范围独立裁决，kg_build与cleanup_executor的Permission不互代。未开始标签只公开已授权的闭合公共概览，不交付Graph私有操作／审核证据或图数据；父可读不授予子权，单凭deadline已到与pending不能证明没有未提交claim。不新增Graph专业端点或写操作，源码及页面尚未实施该投影。
- 已确认的展示细则：未开始仅指当前Cleanup子execution；不代表G关闭／开放、构建或审核停止、业务图为空、远端效果已核清。Graph不为System／Monitor标签新增私表查询接口或输出原始操作证据；未领取拒绝仍不改变领域事实。读取顺序、标签随快照失效及矛盾事实处理沿同一正式契约，实现与验证由牵头统一推进，新的Hosted另行确认。
- 冻结Graph当前本体执行映射、材料实际字节身份和目标配置，不扩展Ontology修订／发布职责；当前Graph版本记录不视为已经具备不可变发布能力。职责、最小验证和未有门禁缺口见[Graph／Cleanup矩阵](../docs/spec/testing/Owner补充门禁.md#graphcleanup-执行管理最小验证矩阵)。
- 自动实体／关系写入、审核项入库或材料处理失败必须向上传播原始错误，失败分块不推进已处理进度。无未决外部效果时继续后续材料；原生效果不确定时阻断后续新增效果。任一材料失败，任务和 execution 最终同为 `failed`。已成功写入的数据、写入／待审统计和逐份材料错误保留，不新增部分成功状态，不回滚业务图。两类引擎共用此流程，最小验证为 Graph Go T1 和既有 Common FalkorDB／Neo4j T2。
- 抽取结果进入写图或审核队列前，实体类型必须在当前本体中存在，并且只能确定一个显式 `Unique` 属性，值必须非空；无键、多键、空值或未知实体／关系类型使当前材料失败。数字零和布尔假不作为空值；不默认按 name 合并，不自动生成 UUID，也不按属性顺序猜合并键。此规则用于构建，不改变现有业务图的浏览和查询。

构建监督器默认每Backend一个槽位；`GRAPH_BUILD_LEASE_SECONDS=120`、`GRAPH_BUILD_HEARTBEAT_SECONDS=20`、`GRAPH_BUILD_POLL_SECONDS=1`控制租约、心跳和发现周期，非法值拒绝启动。旧执行缺少私有冻结输入／控制记录时只保留历史，不由新监督器接管或补造事实。

Graph控制API只接受当前version，reconcile要求`graph.graph.reconcile`，reopen同时要求update与reconcile；该Permission不默认授予角色。现有Provider缺少原物理目标身份和完整失联核清协议，reopen失败关闭，不承诺全部失联场景可以恢复。

本轮最小验证：`make test-module MODULE=graph GO_ONLY=1`、`make test-graph-frontend`、`make test-common-postgres COMMON_POSTGRES_TEST_ARGS="--test graph"`、`make test-common-falkordb`、`make test-common-neo4j`和`make test-authorization`。PG覆盖双实例精确领取、取消竞争、真实G锁等待跨租约、旧运行体拒绝／迟到证据与未决阻断；原生门禁覆盖部分写入及成功后回执丢失。HTTP 202只表示入队／取消受理，运行验收和新的Hosted须另行取得证据。

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

- 模块内 Router 使用 `/ontologies`、`/graphs`、`/analysis`、`/knowledge-service` 等无模块前缀路径；Console 公开 URL 统一加 `/graph` 前缀。发布镜像的 Nginx 为页面深链提供 SPA 回退；缺失的 `/assets/` 资源明确返回 404，不返回入口 HTML。
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

隔离权限 T4 使用 `graph-protection` suite，通过 `ADDP_ONLINE_GRAPH_ENGINE=falkordb|neo4j` 显式选择原生引擎，沿 `online-hosted-security-gate.sh` 和 `hosted-online.sh` 的唯一 Hosted 生命周期，由 `make test-online ONLINE_SUITE=graph-protection` 执行。专用 FalkorDB 复用中央镜像契约，当次 Infra 内部网络、tmpfs 数据、独立 Compose 资源；原有保护断言使用两张各 3 个 Person、2 条 Link 的合成图。普通读取 User 仅获 `graph.graph.read`；准备者独立持有图谱配置、Meta 扫描和纳管所需 Permission，无权对照 User 不含 Graph 读取权。所有账号只存在于 disposable Hosted Tenant，不新增产品角色、源数据 Grant 或业务引擎授权。

上述 T4 从 Gateway 和 Graph 直连验证私有读取 200、无权 403、匿名 401，仅将合成测试图显式公开，先验证匿名读取 200，再经正式 Security 纳管并等待 develop/graph/manager/service/transfer 五 Owner 的当前投影确认后验证保护 403；FalkorDB 的同引擎未纳管对照图仍返回真实数据，Neo4j 的独立实例对照图仍可读。拒绝必须匹配保护原因，不能以普通权限错误或连接失败代替。失败也须关闭公开状态、正式退出测试纳管并等待五 Owner、删除图谱与本体配置；标准 Hosted 清理继续销毁合成图数据库和临时 IAM 所在的完整测试 Infra，并核对容器、网络、卷零残留。现有业务图、Infra 私有图、external 索引和暂缓的 Graph 源数据授权均不参与。确定性回归由 `make test-security-online-runner`、聚合 `make test-online-runner` 及既有 Platform CI 覆盖，真实 T4 结果单独报告。

同一 suite 的 v2 报告还要求 Console 浏览器验收：在合成图保持私有、未纳管时，通过正式登录进入 Graph，核对当前浏览器 User、Tenant 和仅有的 `graph.graph.read` 与 HTTP 验收身份一致，验证浏览快照的 3 节点/2 关系、真实画布、全文搜索及知识服务搜索的三个实体，并确认公开开关不可操作、没有图谱配置写请求。浏览器复用相同合成资源，不生成额外账号、图或权限；报告和两张截图缺失、身份不符、任何非预期 API 失败或浏览器异常都使 suite 失败，随后仍执行同一清理。历史 v1 的纯 HTTP 结果不能作为普通账号 Console 已通过的证据。

v3 在保留上述 Graph 最小权限账号和验收的同时，复用两张合成图增加独立 Develop 普通账号。该账号仅有 `develop.task.read`、`develop.task.execute`、`develop.data_read.execute`、`system.execution_authorization.create`、`meta.catalog.read` 五项已确认的测试 Permission。正式登录工作台并选择 Meta 图资源后，纳管前验证 Cypher 的真实 3 节点/2 关系、两行图结果及表格视图，以及文本和整数参数的一行表格结果；五 Owner 确认后，同一 Develop User 的目标图执行必须以 `develop.query.protection_required` 在 preparation 阶段拒绝且不返回结果，同引擎对照图继续成功。两个阶段报告、四张截图、身份/权限或断言任何一项缺失均使同一 suite 失败；账号、执行记录及原生合成数据随 Hosted Infra 销毁。未扩大 Graph 源授权或增加属性脱敏。

v4 将该 suite 统一切换为正式 Compose 产品镜像：Console、Graph 和 Develop 经生产 Nginx 单入口访问，原有后端与必要 Worker 复用正式服务定义，不再启动 Vite。仅回环诊断端口供既有构建身份和直连拒绝检查使用；业务图通过当次 Infra 网络的容器地址连接，不发布宿主机端口。普通用户权限、两张合成图及保护阶段保持原范围。浏览器额外核对发布资源、Graph 深链刷新、四主题同步及画布颜色，旧 v3 结果不计为生产前端验收；应用、夹具及 Infra 仍在全部退出路径清理并核对所属 project 零残留。

Graph 浏览器 warning/error 必须保留脱敏后的具体消息、资源路径、行列及观察阶段；失败时也保留诊断文件，不记录 URL query、登录凭据或当前 Access Token。任何未豁免诊断仍使验收失败。四主题像素检查从真实绘图画布复制到声明 `willReadFrequently` 的读取画布，避免测试观测自身反复读取绘图 context 产生性能警告；不修改产品画布配置、不替换实际像素，也不将该警告加入过滤规则。

Graph 入口使用现有 `--el-font-family` 与 `--el-font-size-base` 设置文档字体。图标签由共享 `readGraphTheme(container)` 读取容器计算后的字体并显式传入 G6；查询结果、结构图、本体图及 Graph 浏览器复用这条路径，避免 G6 加载时缓存的 `body` 字体与模块 `#app` 字体不一致，也不能依赖 Console 的 iframe 外部继承。Graph 与 Develop 的 T3 覆盖正文及实际节点、关系标签字体；Hosted 浏览器 v3 报告额外要求 `theme_typography=true`，旧 v2 不作为字体一致性验收证据。

FalkorDB 全文索引同步必须在原生 `db.indexes()` 报告全部所需 NODE 全文索引字段匹配且 `OPERATIONAL` 后返回；managed 新建/重建和 external 复用走同一就绪检查，最多等待30秒并遵守调用取消。构建中不能返回成功或空搜索结果；元数据异常、字段冲突、超时和查询错误保留为错误。该边界由 Common Provider 实现，Graph 与知识服务不增加重试或扫描回退。`make test-common-falkordb-unit` 覆盖状态、取消和错误，`make test-common-falkordb` 以独占合成大图验证同步后首次搜索完整返回、external复用及既有Graph/Develop领域契约。

Neo4j 原生 T4 复用同一 Graph／Develop 断言、正式构建与 Nginx 生命周期；使用与既有 Neo4j T2 相同的固定 Community 镜像，目标和对照分别位于两个独占实例，各只含 3 个 Person、2 条 Link。路径为 server/database/graph，不以 label 伪造 graph leaf。现有 Neo4j PreparedQuery 仅证明引擎范围，未提供精确图 ReadSet，因此纳管后验证目标拒绝和另一实例可读，不声明同引擎其他图可读或属性脱敏。FalkorDB 仍验证同引擎独立 graph 对照。唯一入口和浏览器实现覆盖两种原生协议，旧专用 suite／脚本路径删除；手工 Hosted 输入 graph_engine 必须显式选择并写入报告，四主题、字体、图／表结果、类型化参数、五 Owner 回执、发布资源及双层清理缺一不可。


v5 在原有两张保护图之外增加独立人工审核合成图；FalkorDB 使用独立 graph key，Neo4j 使用第三个独占实例。准备账号仅补六项 `graph.build_task.read/create/update/execute/cancel/delete`，独立普通审核账号只持有 `graph.graph.read` 与 `graph.review.read/approve/reject/update`。通过正式任务、材料上传和执行产生待审项，抽取传输端返回受控结果；真实 Console 验证批准、拒绝、修改、关系批准及中间一项原生写入失败后的批量继续、失败提示、刷新和清空选择。随后核对审核人、原生实体/关系标识、最终属性及 404/409/403，删除当次任务/材料/队列/图配置；原生数据和身份随 Hosted 整体销毁。该结果不计真实模型抽取或 Copilot 服务鉴权验收。标准验证继续使用 `make test-graph-online-runner` 和既有 Hosted suite，不接入个人开发部署；完整边界见[数据保护场景](../docs/spec/testing/scenarios/数据保护.md#graph-protection)。Develop 类型参数的浏览器定位按当前 combobox 的 `aria-controls` 选择对应 listbox，不能全局按同名选项点击多个参数下拉框。

审核表格的批量勾选通过可见的 Element Plus `.el-checkbox` 点击，再断言原生 checkbox 已选中；不能直接对隐藏的原生 input 执行 `check()`，也不能强制点击跳过可操作性检查。批量按钮只向既有翻译模板传入选中数量，不再额外拼接计数。`make test-console-frontend` 自动发现 `e2e/graph-review-selection.spec.js`，使用真实 ReviewQueue 和组件样式、HTTP 合成响应验证批量批准和拒绝的勾选顺序、准确计数、失败后的刷新、清空选择及失败提示；该 T3 回归不代替 Hosted 原生写图验收。

稳定视觉补验由 Console T3 与同一原生 Hosted `graph-protection` 套件复用 `e2e/graph-visual.fixture.js` 单一观察实现，生产构建不读取 Vue/G6 调试对象，不增加产品测试接口。搜索从实际 Canvas 的真实图例填色连通核心定位节点，将主题高亮像素按最近核心归属，并要求高亮边界包围填色核心；分别保留两类像素计数（各至少 40）、真实颜色及高亮/填色核心边界，Python 独立核对核心被高亮边界包围。文字穿过高亮圆环及阴影过渡带不要求两色直接连通，不将上下两段圆弧分别当节点或忽略该节点。色差容限、完整视口边界、三节点数量和十连续帧不变。要求三节点全部位于视口内且连续十帧稳定，再逐个鼠标点击详情核对来源 ID 与原生 ID；T3 同时覆盖保留身份和全新身份的搜索替换，并拒绝隐藏 Canvas。审核在原生批量中间失败、继续处理及队列刷新后，核对表头与剩余行原生 checked/indeterminate、组件类、背景与勾号样式，等待真实 CSS 动画结束并连续三帧稳定后截图。搜索原始报告使用 v2，探索原始报告使用 v2，所有节点帧必须携带主色/填色和各自原始像素计数，旧单色帧不接受。Hosted 必须保存同一运行/租户/普通用户/图绑定的 `graph-search-stable-frame.json` 与 `graph-review-stable-selection.json`，Python 严格校验原始边界、点击身份、样式与帧数，缺失或不符即失败；Graph 浏览器报告升级 v7、审核浏览器 v2、整体 v9，不保留旧证据分支。定向 T3 使用 `console/frontend` 下的 `node ../../scripts/test/frontend-browser-gate.mjs e2e/graph-search-visibility.spec.js e2e/graph-review-selection.spec.js`；现有 Console 自动发现、`make test-graph-online-runner` 与 Hosted 双引擎入口覆盖，无需新工作流、服务或权限。T3 仅证明真实组件与合成 HTTP，原生稳定状态以新增 Hosted 运行证据为准。

节点展开与布局切换补验继续复用上述隔离合成图和普通 reader：浏览快照 overview 是聚合节点/聚合边，搜索返回匹配实体而不补关系，均不得作为原生边基准。先由同一最小权限 reader 经既有 Knowledge Service 两跳路径接口独立读取并严格核对 p1→p2→p3 的两个原生关系 ID、端点及类型，再将该基准绑定到浏览器期望和原始探索报告；展开响应是以当前目标为种子的一跳子图，页面以该子图替换当前实体视图，不累积前次结果。从 p1 正式 POST 展开得到 p1/p2 和一条 Link，再从 p2 展开得到 p1/p2/p3 和两条 Link，覆盖保留节点与新增邻居。随后实际操作力导向、分层、环形、辐射四种布局，布局选择本身不得新增 API 请求。通过真实清空搜索和关闭详情控件去除搜索/选中光晕后，每个阶段从筛选图例读取真实 Person 填色，复用同一个 Canvas 连通像素观察算法，要求全部节点完整位于视口内、连续十帧稳定并逐项真实点击核对来源及原生 ID；不读取 Vue/G6 调试对象。每次点击前关闭旧详情，防止旧详情被误记为当前命中。新增 `graph-exploration-stable-frame.json` 必须保留两次请求及返回、逐阶段边界/帧数/命中和四种实际布局，Python 严格拒绝缺阶段、错原生 ID/边端点、错展开深度或仅有声明无原始证据。Graph 浏览器报告升级 v7、整体 v9，审核 v2 原有证据不变。定向 T3 仍由既有两个 spec 和标准入口覆盖，原生结论只以本轮 FalkorDB/Neo4j 同源 Hosted 证据认定；未运行或失败不得算通过。

审核浏览器验收在深链重载后等待真实队列响应，再由该请求创建 API 客户端并核对 User、Tenant 和精确权限集；不保留重载前捕获的会话凭据用于最终结果核对。共享 `browserAPIFromResponse` 的既有回归通过真实 HTTP 测试服务验证旧凭据失效、新请求凭据有效及身份不符时拒绝，纳入 `make test-console-frontend`；不注入准备账号 Token，不放宽 401 断言。

审核列表省略 `status` 时只返回 `pending`。验收的待审读取显式使用该状态；最终审核结果由普通审核账号分别查询既有 `approved/rejected/modified/pending` 状态，逐页核对数量和状态后合并，严格核对七条记录、审核人、原生标识和最终属性。不新增 `status=all` 或改变接口默认值；浏览器与 Python 核验使用相同过滤语义。既有完整流程回归模拟真实的默认 pending 行为，另验证缺页、错误计数和混入其他状态均失败，由 `make test-graph-online-runner` 覆盖。

Develop 保护阶段沿用仅纳管、无保护规则的合成夹具，五 Owner 回执确认不等于规则已激活，资源保持 `enrolling`。具体拒绝诊断严格按现有 Provider 证据核对：FalkorDB 的精确图 ReadSet 命中 enrolling 投影，原因为 `protection_not_ready`；Neo4j 无法证明完整 ReadSet，原因为 `protection_lineage_unverified`。浏览器和报告均保留稳定错误码、`preparation` 阶段及该精确原因，仍要求零结果和独立未纳管对照成功；不接受通用原因或互换引擎原因。确定性回归由既有 `make test-graph-online-runner` 覆盖，不改产品保护规则或授权。

Graph Hosted 的并发组按 `suite + graph_engine` 划分，两种引擎的独占部署不共用 pending 槽位；同一引擎仍保持不取消运行中任务的串行限制。其他 Security suite 的并发组不依赖无关的 graph_engine 输入。排队中被替换的任务不计为执行或通过，必须保留取消回执并在对应引擎组补跑。该约束由既有 `make test-graph-online-runner` 的 Graph Online 登记回归验证。

多标签、密集关系原生补验继续使用同一 Hosted `graph-protection` 生命周期。原有目标/对照/审核断言全部结束后，删除对照图配置，由已验证 Compose project 所属的原生夹具入口仅替换该隔离对照数据库，保留目标与审核数据，复用原实例而不新增服务、身份或权限。新图含 12 节点、四个完整标签集合（Person、Person+Employee、Organization、Organization+Partner 各三个）和 72 条关系（12 节点两两有向连接 66 条，另含一条反向边、四条异类型平行边和一条自环）。新本体与图配置单独创建和清理。普通 reader 独立读取实体及两跳子图作为原生 ID/完整标签集/关系 ID、类型、端点的严格基准，再验证实际 Console 搜索、完整集合筛选、一次两跳实体展开及四布局。多色观察复用唯一 Canvas 观察器：一次采样中识别全部真实图例颜色的核心、按最近核心归属主色像素，全部颜色组同时连续十帧稳定、完整视口内，并逐个真实点击。原始 `graph-dense-stable-frame.json` 必须绑定同 run/Tenant/User/图及原生基准，Python 拒绝缺组、重复/错身份、标签或边、帧数、坐标与请求；整体报告升级 v16，密集原始及阶段报告为 v5，缺多标签密集及逐条关系证据不接受。T3、原生夹具生命周期 T1、Python 证据反例及既有 Graph runner/Hosted 双引擎入口同次覆盖，不恢复定时。

密集关系的分层布局按 `max(1, relationshipCount / max(1, nodeCount))` 归一化节点/层间距，保留稀疏图原间距。真实 Dagre 回归覆盖 12 节点、69 条含回边/平行/自环关系的布局，要求自动适配后仍能看见普通关系且节点不重叠；该调整修复固定大间距使密集图横向展开过宽、适配缩小后落入远景关系隐藏区间的问题，不改变语义缩放阈值或通过放大固定节点绕过。原生补验仍要求每个标签组全部节点的真实像素、点击及十稳定帧。

密集合成图同时核对回边、异类型平行边和自环的原生关系身份。GraphCanvas 统一复用 G6 `processParallelEdges` 分配曲线偏移与自环位置，语义描边保留继承几何的二次/三次曲线路径，箭头沿该路径末端切线绘制；不再用端点直线重建曲线。曲线使用现有关系身份、颜色、虚线和缩放规则，不改变 API 数据。组件回归拒绝重叠偏移、遗漏自环类型、退化路径及错误箭头终点，真实浏览器继续验证布局、节点命中、主题和缩放。

多标签密集原始报告的每个含关系阶段还必须保留 `relationship_pixels`：从实际图例读取关系色，在 Canvas 中排除所有节点边界区域后，每种当前关系类型至少 40 个真实描边像素且连续十帧计数稳定，颜色不得互相重复。搜索无关系阶段该字段必须为空；不能只用 72 条 HTTP 关系数据推断密集关系线已显示。该证据与节点稳定帧、真实命中和原生关系基准同时验证。

密集夹具的独立 KG 基准只保存原生身份、标签和属性，不携带浏览器 `display_name`。该夹具本体明确以 `name` 为显示属性；浏览器观察应从已独立核验的 `properties.name` 构造期望显示名，并核对搜索 DTO 与每次真实点击的详情标题。T3 必须使用同样不含 `display_name` 的原生基准，避免拿浏览器 DTO 代替原生基准而漏掉标题断言错误。

像素观察中，普通节点的观察色与填色相同时，其边界和像素数直接来自圆形填色连通域；只有异色搜索轮廓才按最近核心归属轮廓像素。与节点同色但不相连的关系描边不能扩大节点边界。确定性 Canvas 反例覆盖两个同色节点之间的独立同色线段，实际密集图仍要求全部节点完整可见、十稳定帧及逐项点击，不放宽尺寸、比例或视口条件。

Neo4j managed 全文索引由原生索引定义去重：多个本体搜索定义如果具有相同首标签和相同属性集合，只创建一个当前 Graph 前缀内的物理索引，各定义解析到该实际索引名，并保留各自完整标签过滤。不同属性集合不可合并，其他 Graph 前缀的索引不可删除；external 路径仍只读复用精确匹配索引。Neo4j `CREATE FULLTEXT INDEX IF NOT EXISTS` 对同定义的不同名称也会跳过创建，不能把请求名称直接当作已存在的索引名。

自环路径在 G6 原生路径计算入口始终从当前源节点中心重新生成锚点，不继承布局过程中缓存的无效 start/end/controlPoints；仍使用原生 loop 的完整三次曲线计算。真实回归覆盖无效锚点刷新后全部坐标有限且起终点不同。密集夹具保留原有同标签短平行边，并增加三条跨标签平行边，以足够的异类型关系面积观察完整的多标签图；每类型 40 像素及十稳定帧不变，不把短虚线只有少量实心像素误记为关系数据丢失。

密集阶段复用隔离 Neo4j control 容器时，删除 Graph 配置不会删除原生全文索引。重置前必须传入刚删除并核 404 的旧 control Graph ID，验证原三节点两关系和容器归属，再要求只有该 ID 的 Person/id+name 精确索引，按精确名称删除并核全文索引数为零；异名、异定义、多余或缺失索引均在写入前拒绝。随后才重置节点并生成密集图，不改产品 Graph 删除语义、不清理其他 Graph 前缀，不修改 FalkorDB 的原生准备行为。


逐条关系 Hosted 补验继续使用同一隔离 12/72 密集夹具。每条关系具有独立 `fixture_id`、有向来源/目标键、零起序号、数值评分和布尔属性；普通 reader 的原生子图独立保存关系原生 ID、类型、有向端点和完整属性，不能用浏览器 DTO 构造基准。四种布局均逐条真实点击 72 条关系并读取 NodePanel 的关系类型与全部属性，按唯一夹具键绑定原生 ID，要求各布局同一 72 身份集合，覆盖反向、异类型平行及自环。该身份绑定来自已独立读取的原生基准，不能声称详情面板直接显示了原生 ID。

关系坐标仅通过测试侧 Canvas 绘制调用的实际路径与变换采样，不读取 Vue/G6 对象或调用产品选择方法。候选点必须位于实际已绘制曲线、视口内且避开节点；绘制路径连续十帧稳定后才用真实鼠标点击。原始 `graph-relationship-hit-frame.json`（v2）关系命中报告单独保存每布局的路径、点击像素、端点归属、原生身份映射与 DOM 属性原文，并与密集阶段报告交叉核验。缺关系、重复身份、错方向/类型/属性、旧版本报告、未稳定或不可见 Canvas 都不能通过。既有最小权限、保护、审核、稳定节点、真实请求和清理断言保留；标准 Graph 前端门禁、相关 Console T3、原生夹具/证据反例和既有 Graph runner/Hosted 双引擎同次覆盖，不恢复定时。

关系命中主图形必须直接使用完整二次/自环曲线路径，不能另画禁止捕获的可见关系层而让主图形停留在退化路径；绘制和命中共用一条路径。选择关系时，详情面板的对象类别与原生关系类型分别传递，不能以 `edge` 覆盖展示的关系类型。

二次关系曲线按端点距离增加四分之一的基础弯曲，依据端点 ID 的固定排序确定规范侧；平行/反向关系仍保留 G6 已分配的相对偏移。该几何规则让共线的嵌套长短关系具有独立线段，避免长关系的命中区完整覆盖短关系；不按夹具 ID、关系类型或引擎硬编码。自环继续使用原生 loop 计算。

密集图关系的附加命中宽度为 2（稀疏图仍为 8），与现有密度线宽及缩放一起处理，避免不可见的宽命中区截获相邻关系描边上的真实点击。属性回读与四布局逐条命中是最终约束，不能通过忽略漏项或程序触发选择满足。

关系描边的原生命中区重叠时，GraphCanvas 在同一鼠标点击处理内只比较可见、可捕获且原生 `isInShape` 命中的关系，复用 G6 Path 的 `getPoint` 与实际变换矩阵选择距离鼠标最近的可见曲线。精确交点距离相同时保留原生绘制顺序；关系文字和方向标记继续选中所属关系。测试仍通过真实鼠标事件与完整属性/原生身份核对，不调用选择方法代替用户点击，不按引擎或夹具 ID 分支。

关系类型过滤以真实复选框选择为准：全部取消表示不显示任何关系，不作为“未筛选”显示全部；节点保持可见。隔离 12/72 Hosted 夹具在真实页面依次验证仅平行类型、全部关闭、仅普通类型和全部恢复：当前类型的逐条原生关系命中/属性、关闭类型的零描边十稳定帧，以及全部关闭后逐个点击原 72 个关系命中位置不得选中关系。恢复后四布局逐条身份断言继续执行。新增独立 `graph-relationship-filter-frame.json`（v1），与 dense v5 / 整体 v16 报告严格绑定同运行、租户、普通用户和原生基准；缺过滤证据不能复用旧验收。复用原有 Graph runner、Console T3 自动发现与 Hosted 双引擎入口，不扩权、不恢复定时。

存在节点形状选项时，复选框全部取消表示不显示节点及其关联关系；没有可用形状选项的图沿用既有无标签节点读取行为。关系类型选择保持原值，恢复全部节点形状后重新显示原有节点/关系。空选择不能作为“未筛选”显示全部，非空选择继续沿用现有标签匹配规则。隔离 12/72 Hosted 夹具须在关系类型过滤回路之后，逐项取消四个节点形状，核真实 0 节点/0 关系、可见空态和无加载、全部节点填色/关系描边零像素十稳定帧，再真实点击原 12 节点和 72 关系位置确认没有详情选中；全部恢复后核完整节点属性/原生身份，并继续四布局逐条关系命中。新增独立 `graph-node-filter-frame.json`（v1），与 dense v5 / 整体 v16 报告绑定同运行、租户、普通用户和独立原生基准；保留关系过滤、审核、保护与零残留断言，复用现有标准门禁和 Hosted 双引擎入口，不恢复定时。

逐条关系观察器按真实像素节点轮廓绑定曲线端点，不能用最近节点中心代替：曲线端点位于所属圆形边界时，相邻节点中心可能反而更近。JS 候选识别与 Python 独立校验统一比较端点到可见节点轮廓的距离；既有真实鼠标命中、完整属性、原生 ID/方向/类型、可见描边像素与十稳定帧要求不变。受控 Canvas 与 Python 正反例覆盖相邻中心误归属和端点实际落到其他节点轮廓的拒绝，不借此修改产品布局或放宽逐条验收。

逐条关系回归只优化候选点计算与尝试顺序：使用精确空间最近邻查询计算到其他绘制路径采样点的净空，保持原100像素上限、每个分数和稳定排序；每条路径先尝试最佳点，其余所有候选继续保留给交点及平行边重试。受控Canvas逐点对照穷举距离，并覆盖交点零距离及独立路径100上限。三种ID、四布局、每条原生关系及全部属性、真实鼠标点击、像素十稳定帧、过滤与不可见负例均不得减少；不以提高Job超时或重跑有效Hosted代替Console标准门禁耗时验证。

近邻端点专项使用真实 Graph 页面和隔离 12/72 原生合成图：由真实鼠标拖拽形成固定几何，先停放其他节点，再分别把不同标签的旁观节点放到源/目标端点邻近位置。保留实际 Canvas 轮廓、绘制路径、拖拽前后坐标、十稳定帧和每次关系属性/原生身份回读；必须证明旧最近中心方法会选错节点而当前端点绑定及真实页面点击正确，不能只用手工 Canvas 代替。Hosted 仅选择 `graph_case=endpoint-geometry`，复用现有 `graph-protection` 的独占生命周期、最小权限 reader/preparer、原生基准和零残留清理，不执行仍有效的保护/审核/完整密集图回路；默认 `full` 的验收范围保持不变。此专项不证明旧失败原始几何帧已复原，不接入个人部署、不扩权、不恢复定时。

固定近邻真实页面回归已暴露仅用像素填色包围盒估计圆轮廓仍会误认：填色未覆盖描边，且邻节点遮挡会裁切可见包围盒。唯一端点归属路线改为测试侧实际 Canvas `arc`/`fill` 绘制圆几何；按真实填色和已经过真实点击核对的节点核心绑定原生 ID，保留绘制圆与像素边界共同十稳定帧、完整视口、圆/核心一致性和真实关系鼠标属性回读。Python 严格校验每个原生节点的独立绘制圆、字段类型/颜色/范围及端点到圆轮廓距离；不回退到最近中心或可见包围盒。关系原始证据 v2，密集证据 v5，整体 full v16；既有历史证据保留其原版本，不重复派发本轮无关回路。

端点专项的临时 managed 对照配置必须在删除前，由原有只读 reader 执行一次严格三实体搜索，实际建立本体搜索索引。Graph 的 managed 全文索引在首次非空搜索时同步，创建配置本身不会建立；Neo4j 原生重置仅接受该已删除对照 ID 的唯一精确 Person(id,name) 索引，缺失/额外/异主/异结构均拒绝。专项不能省略索引准备，也不能放宽原生重置所有权检查。此读取仅为现有隔离对照准备，不重跑完整保护/审核验收。

同标签同色近邻专项继续使用原隔离12/72合成图、普通reader及已有原生独占生命周期。分别把Person旁观节点置于源端、Organization+Partner旁观节点置于目标端，覆盖紧邻、部分遮挡和移开恢复；由真实鼠标拖拽形成几何，逐节点点击绑定原生身份，并回读目标关系全部属性。不得以不同色旁观节点或手工Canvas替代；不重跑既有不同色端点、审核、保护和完整密集回路，不恢复定时。

同色深遮挡使用独立 `addp.graph-exposed-pixels/v1` 契约，不放宽完整可见节点的比例或视口要求：实际绘制圆完整位于视口内，暴露核心像素与其他所有圆轮廓至少相隔3 CSS像素，每节点至少40实际填色像素、5个原始RGBA样本、实际点击/原生身份以及十连续稳定帧。局部 `clearRect` 只使相交的绘制圆记录失效，保留未重绘节点；记录矩形clip及实际覆盖交叠采样点的清除/填色轨迹，不能用缓存枚举顺序代替该点上下层。交叠处真实点击必须选中最后覆盖采样点的原生节点，移开后全部节点身份恢复。

两圆边界都接近曲线端点时，端点归属同时要求与圆轮廓距离不超过2 CSS像素，以及路径起始切向离开源圆、结束切向进入目标圆；必须唯一匹配，不能只取最近圆边界。重叠节点点击取证后先通过真实关闭按钮清除选择，再观察关系路径，避免把选中边框引起的合法锚点位移当作基础几何变化。

同色专项选择 `graph_case=endpoint-same-color`，仅执行新七阶段（基线；源端/目标端各紧邻、遮挡、恢复）和20次真实拖拽。整体 `addp.graph-same-color-online/v1`、原始 `graph-same-color-geometry.json` 与七个 `graph-same-color-*-frame.json`（`addp.graph-same-color-frame/v1`）绑定同run/Tenant/User/引擎/图及独立原生12/72基准；保存七张关系属性图和两张重叠节点命中图。JS与Python同时验证完整关系属性、同标签/同色、圆几何、暴露像素、裁剪绘制轨迹、鼠标偏移和恢复，不接受旧不同色证据。现有Graph runner、Console T3自动发现、Hosted同一生命周期/并发组与精确清理保持唯一入口，不增加身份、权限或原生服务。
