# Port Allocation (ADDP)

统一定义 ADDP 系统库与业务库的容器端口。下列 System 宿主机端口是本地开发首选值；容器内部端口保持固定。

## System (ADDP 基础设施)

- PostgreSQL: `15432`
- Redis: `16379`
- FalkorDB: `16479`（仅绑定 `127.0.0.1`；Ontology 私有 Infra，不注册为业务 Engine）
- MinIO API: `19000`
- MinIO Console: `19001`
- Meilisearch: `17700`
- Infra Kafka bootstrap: `19092`
- Kafka Connect REST: `18083`
- 辅助 macOS CI disposable MySQL: `13306`（仅 `make local-ci` 运行期间占用）
- 辅助 macOS CI disposable OceanBase: `12881`（仅 `make local-ci` 运行期间占用）

PostgreSQL、Redis、FalkorDB、MinIO、Meilisearch、Infra Kafka 和 Kafka Connect 由 `docker-compose.infra.yml` 管理。本地 `scripts/infra/up.sh` 优先使用根 `.env` 中的宿主机端口；若被其他进程占用，自动选取空闲端口，并在输出中报告。已经运行的本工作区容器保持现有映射，重启不随意换端口。开发进程从 Compose 查询实际映射，再构造数据库、缓存、存储、搜索和 Kafka 地址；容器间继续使用固定的服务名和内部端口。端口监听本身不能作为 ADDP 容器就绪或所有权的证据，必须核对 Compose 项目和健康状态。

自动选端口只适用于本地基础设施宿主机映射。生产部署与 `addp-online` 专用 Runner 按其显式环境配置运行。Docker 最终绑定仍可能遇到检查后的并发抢占；启动失败时报告实际冲突，不连接占用该端口的其他服务。

根 `docker-compose.yml` 的容器部署只向宿主机发布 Nginx 统一入口 `${NGINX_PORT:-80}:80`；Console、模块 Frontend、Gateway、Backend 和内置 Workflow Runtime 仅在应用 Docker 网络中使用稳定服务名和固定容器端口。Nginx 按路径转发前端与 `/api/`，Gateway 从 System 的 Backend 实例注册事实发现业务路由；Frontend 端口不注册到 System。宿主机入口端口由部署配置明确指定，冲突时启动失败，不在生产环境自动漂移。容器部署的对外 origin 由 `ADDP_PUBLIC_ORIGIN` 指定；未设置时按 `http://localhost:${NGINX_PORT:-80}` 构造，System 的 `PUBLIC_API_URL`、`CONSOLE_URL` 和 Monitor 的告警链接均使用此 origin。经域名、TLS 或上级反向代理发布时必须显式指定用户实际访问的 origin。模块 standalone 模式仍可单独部署，但不通过根 Compose 再发布一组固定的宿主机端口。

本地开发的 Gateway、各模块 Backend/Frontend 与工作流 Runtime 同样以本表开发端口为首选值。`scripts/dev/start.sh` 在 Infra 就绪后统一检查监听者；首选端口被其他服务占用时选取空闲端口，将最终端口注入模块进程、Gateway URL、Console 代理、iframe 地址、CORS 来源及 Vite HMR。运行中的本工作区服务沿用原端口；解析结果保存在忽略版本控制的 `.dev-state/ports.env`，仅作为本地运行状态，不改写根 `.env`。显式 `addp-online` Runner 仍使用配置端口并在冲突时失败。端口检查与监听之间仍可能出现并发抢占，实际绑定失败必须明确报错。

容器化工作流 Runtime 仍在内部固定端口监听。开发模式的独立 Runtime 向 System 自注册时使用实际宿主机映射端口，开发启动通过 `RUNTIME_PUBLIC_PORT` 向 GeoPython、PointCloud、Document Runtime 传递该端口；根 Compose 部署则使用 Docker 服务名和固定容器端口。SuperMap Runtime 不自注册，需要按相应部署模式的实际可达地址登记。

`13306` 和 `12881` 由 `scripts/test/docker-compose.local-macos-ci.yml` 中的固定 digest MySQL 8 与 OceanBase CE 4.4.2 LTS 使用，不属于 System 或 Business 长期基础设施。两个服务都无数据卷，只绑定 `127.0.0.1`，在本地巡检的确定性门禁和编译开始前完成健康检查，并在巡检的统一退出清理中删除。

## Business (业务库)

- PostgreSQL: `5433`
- Oracle Free: `15210`（容器端口 `1521`，service name `FREEPDB1`）
- SuperMap SDX+ for PostgreSQL 专用 PostgreSQL: `5434`
- MinIO API: `9002`
- MinIO Console: `9003`
- Neo4j Browser: `7474`
- Neo4j Bolt: `7687`
- Spark Master: `7077`
- Spark Master UI: `18088`
- Spark Thrift Server: `11000`
- Business Kafka bootstrap: `29092`
- OceanBase SQL: `2881`
- TiDB SQL: `4000`
- openGauss SQL: `5435`（容器端口 `5432`）
- KingbaseES SQL: `5436`（容器端口 `54321`，仅 owner-managed disposable Business 样例）
- 达梦 DM8 SQL: `5236`（容器端口 `5236`，ARM64 官方介质 disposable 数据库，以 `engine_type=dameng` 登记）

来源：`business/docker-compose.yml`。常规 Business 服务可通过 `business/.env` 指定首选宿主机端口；KingbaseES 与 DM8 技术夹具不进入该文件，只分别接受调用独立 profile 时当前 shell 中的 `KINGBASE_PORT`、`DAMENG_PORT`。

本地 `business/scripts/start.sh` 对 Business PostgreSQL、MySQL 和 MinIO 的首次启动检查实际监听端口，首选值被占用时选择空闲宿主机端口。成功启动后把实际 Compose 映射保存到忽略版本控制的 `business/.business-state/ports.env`。再次启动和重启必须沿用该端口；已保存端口被其他服务占用时直接失败，不自动漂移已登记的 Engine Instance 物理端点。运行中的本工作区容器以实际 Compose 映射为准，容器内部端口始终不变。其他 Business 服务当前仍使用显式配置端口，冲突时不能视为已自动适配。

本机开发进程连接 Business Engine 时应登记 `127.0.0.1` 和该服务的实际宿主机端口；同一 Docker 网络内的 ADDP 进程应登记 `business-*` 服务名和固定内部端口。同一实际 Business Engine 搬迁到新宿主机端口时，有权限的用户在 System 引擎编辑页确认后更新地址，保留原 `engine_id` 和引用；不同实际引擎仍须创建新实例并显式迁移绑定。

Business 本地网络由启动脚本创建为显式网段的 Docker bridge，并保留为外部网络 `business_business-network`；普通服务继续通过 Docker 服务名互访，不登记容器 IP。OceanBase 单节点会把自身内网 IP 写入持久化集群数据，因此启动脚本从只读的 OBD 集群配置读取既有 IP，在容器启动前把该 IP 重新分配给 OceanBase；首次部署成功后也固定其实际 IP。网段记录在忽略版本控制的 `business/.business-state/network.env`。若旧网段不可恢复、地址不在网段内或已被其他容器占用，启动必须在接触 OceanBase 数据前失败，不重新初始化旧库，也不改写 System 的 `engine_id`。笔记本局域网 IP 变化不影响本机回环地址与 Docker 服务名。

OceanBase 的数据卷只保存持久数据与集群配置；`/root/ob/observer/run` 由容器 tmpfs 承载 PID 和 Unix socket 等运行时文件，容器重建时清空，避免旧 PID 阻止 Observer 启动。

```bash
BUSINESS_POSTGRES_PORT=5433
SUPERMAP_POSTGRESQL_PORT=5434
BUSINESS_MINIO_API_PORT=9002
BUSINESS_MINIO_CONSOLE_PORT=9003
NEO4J_HTTP_PORT=7474
NEO4J_BOLT_PORT=7687
SPARK_MASTER_PORT=7077
SPARK_MASTER_UI=18088
SPARK_THRIFT_PORT=11000
BUSINESS_KAFKA_PORT=29092
OCEANBASE_PORT=2881
TIDB_PORT=4000
OPENGAUSS_PORT=5435
KINGBASE_PORT=5436
DAMENG_PORT=5236
```

## Reserved Policy（保留规则）

Ontology Backend 使用 `8195`（开发/容器内部一致），经 Gateway `/api/v1/ontology` 访问。Ontology Frontend 开发首选端口为 `5192`，根 Compose 内部监听 `80`，经 Nginx `/ontology/` 访问，不单独发布宿主机端口；Console 入口 `/ontology/ontologies`。确定性浏览器测试独占回环 `4192`，不复用开发服务。

- **System MinIO 首选 19000/19001**，Business 侧不得主动配置为这两个首选端口。
- **Business MinIO 首选 9002/9003**，System 侧不得占用这两个首选端口。
- System PostgreSQL 首选 15432；Business PostgreSQL 使用 5433。
- SuperMap SDX+ for PostgreSQL 专用实例使用 5434，且不得安装 PostGIS 或与 5433 的 SuperMap SDX+ for PostGIS 工作区共用数据卷。
- Infra Kafka 首选 19092；Business Kafka 使用 29092。两者必须是独立集群，Business Kafka 才能注册为 System Engine。

脚本约束：

- `scripts/infra/up.sh`：若首选端口被其他容器占用，选择空闲宿主机端口并报告；不得复用其他容器的服务。
- `business/scripts/start.sh`：Business MinIO 首选端口不得配置为 System MinIO 的 19000/19001；PostgreSQL、MySQL、MinIO 的首次端口冲突按上述本地规则解析，已持久化端口冲突则失败。

## 快速校验

使用命令校验策略是否符合：

```
make ports-validate
```

输出会显示 business/.env 的端口配置、System 默认端口以及当前运行容器的实际映射，帮助定位问题。

如果本地已有其他服务占用 System 首选端口，`scripts/infra/up.sh` 自动选择其他空闲端口；`scripts/dev/start.sh` 读取实际映射。

## 使用建议

- 同机运行 System 与 Business：
  - 先启动 Business：`cd business && ./scripts/start.sh`
  - 再启动 System 基础设施：`bash scripts/infra/up.sh` 或 `make infra-up`
  - 注册到 ADDP 的 Business 引擎地址应使用容器可访问地址，例如 `business-postgres:5432`、`business-minio:9000`；不要使用 `localhost`，因为连接测试由 ADDP 容器内服务发起。
- 如遇 Business 已持久化端口被其他服务占用，先释放该端口；若同一实际引擎必须搬迁，先通过标准 Business 生命周期脚本启动，再由有权限的用户在 System 编辑页确认并更新其连接地址。


### 端口分配

**ADDP 系统服务**:

开发端口是首选值，实际端口由开发启动脚本解析；容器内端口供 Docker 网络使用。根 Compose 仅发布 Nginx 的宿主机端口，基础设施宿主机映射由 `docker-compose.infra.yml` 单独管理。

| 服务                  | 开发首选端口 | 容器内端口 | 说明                       |
| --------------------- | -------- | ----------- | -------------------------- |
| **Nginx Gateway**     | **80**   | **80**      | **统一入口 (推荐)**        |
| **Console Frontend**   | **5170** | 80 | **经 Nginx 路径访问** |
| Gateway               | 8000     | 8000        | API Gateway (后端路由)     |
| System Backend        | 8180     | 8180        | 认证、用户、日志 (统一使用8180避免端口冲突) |
| System Frontend       | 5173     | 80 | 经 Nginx 路径访问                   |
| Manager Backend       | 8081     | 8081        | 数据源、文件               |
| Manager Frontend      | 5174     | 80 | 经 Nginx 路径访问                   |
| Meta Backend          | 8082     | 8082        | 元数据、血缘               |
| Meta Frontend         | 5175     | 80 | 经 Nginx 路径访问                   |
| Transfer Backend      | 8083     | 8083        | 数据同步任务               |
| Transfer Frontend     | 5176     | 80 | 经 Nginx 路径访问                   |
| Orchestrator Backend  | 8084     | 8084        | 工作流编排                 |
| Orchestrator Frontend | 5177     | 80 | 经 Nginx 路径访问                   |
| Develop Backend       | 8185     | 8185        | 开发工具                   |
| Develop Frontend      | 5178     | 80 | 经 Nginx 路径访问                   |
| Service Backend       | 8086     | 8086        | 数据服务、OGC 标准服务     |
| Service Frontend      | 5180     | 80 | 经 Nginx 路径访问                   |
| Monitor Backend       | 8100     | 8100        | 执行监控、统计分析         |
| Monitor Frontend      | 5179     | 80 | 监控仪表盘                 |
| **Standard Backend**  | **8110** | **8110**    | **数据标准管理（业务域、术语、数据元、码值集）** |
| **Standard Frontend** | **5181** | 80 | **标准管理 UI**            |
| **Model Backend**     | **8181** | **8181**    | **数据建模（业务实体、逻辑表、数仓分层）** |
| **Model Frontend**    | **5182** | 80 | **建模 UI**                |
| **Quality Backend**   | **8182** | **8182**    | **数据质量检查、评分（质量规则执行层）** |
| **Quality Frontend**  | **5183** | 80 | **质量管理 UI**            |
| **Asset Backend**     | **8183** | **8183**    | **数据资产管理（编目、申请、授权）** |
| **Asset Frontend**    | **5184** | 80 | **资产管理 UI**            |
| **Portal Backend**    | **8184** | **8184**    | **数据消费者门户 BFF**      |
| **Portal Frontend**   | **5185** | 80 | **数据门户 UI**            |
| Copilot Backend       | 8087     | 8087        | AI 助手 (查询/工作流/Notebook 生成) |
| **Agent Backend**     | **8190** | **8190**    | **Agent AI 对话助手后端**  |
| **Agent Frontend**    | **5186** | 80 | **Agent 对话界面 UI**      |
| **Graph Backend**     | **8186** | **8186**    | **知识图谱本体建模、图谱管理** |
| **Graph Frontend**    | **5187** | 80 | **知识图谱 UI**            |
| **Inference Backend** | **8191** | **8191**    | **统一 AI 推理控制面与数据面** |
| **Inference Frontend** | **5188** | 80 | **Provider、模型和 Profile 管理 UI** |
| **Catalog Backend**   | **8192** | **8192**    | **企业资源目录身份、业务语义关联、责任和搜索** |
| **Catalog Frontend**  | **5189** | 80 | **企业资源目录管理 UI** |
| **Workbench Backend** | **8193** | **8193**    | **已发布服务消费、动态查询和数据应用创作** |
| **Workbench Frontend** | **5190** | 80 | **Workbench 创作端 UI** |
| **Security Backend** | **8194** | **8194**    | **数据安全分类分级、敏感发现、资源评估、保护策略和投影** |
| **Security Frontend** | **5191** | 80 | **数据安全与隐私保护 UI** |
| **Ontology Backend** | **8195** | **8195** | **原生领域本体、修订与投影运行时** |
| **Ontology Frontend** | **5192** | 80 | **领域本体建模与修订管理 UI** |
| Math Workflow Engine  | 8089     | 8089        | 数学计算工作流参考实现（自动启动服务、手动注册） |
| Jupyter API Server    | 8097     | 8097        | Jupyter 执行引擎 API       |
| Spark Workflow Engine | 8098     | 8098        | Spark 分布式工作流引擎     |
| GeoPython Workflow     | 8099     | 8099        | 空间计算引擎 (Python)      |
| Model3D Workflow Engine    | 8101     | 8101        | 三维模型转换工作流引擎     |
| PointCloud Workflow Engine | 8102     | 8102        | 点云处理工作流引擎         |
| SuperMap Workflow Engine   | 8103     | 8103        | 超图 iObjects C++ 空间计算工作流引擎 |
| DuckDB Query Runtime       | 8104     | 8104        | 联邦只读查询计算引擎       |
| Document Workflow Engine   | 8105     | 8105        | 文档转换工作流引擎         |
| PostgreSQL (System)   | 15432    | 5432 | ADDP 系统元数据            |
| Redis                 | 16379    | 6379 | 缓存、事件和分布式锁       |
| FalkorDB              | 16479    | 6379        | Ontology Infra，仅绑定回环，不注册为业务 Engine |
| MinIO System API      | 19000    | 9000 | 系统文件存储               |
| MinIO System Console  | 19001    | 9001 | 系统 MinIO Web UI          |
| Meilisearch           | 17700    | 7700 | 全文检索引擎               |
| Infra Kafka           | 19092    | 9092        | 内部 CDC 总线；不注册为 System Engine |
| Kafka Connect REST    | 18083    | 8083        | Transfer capture supervisor 内部控制面，不经 Gateway 暴露 |
| Business Kafka        | 29092    | 9092        | 业务 Topic；以 `engine_type=kafka` 注册为 System Engine |
| Business Oracle       | 15210    | 1521        | Oracle Free 普通表与 Oracle Spatial 测试源；以 `engine_type=oracle` 注册为 System Engine |
| Business OceanBase    | 2881     | 2881        | OceanBase CE MySQL 模式测试源；以 `engine_type=oceanbase` 注册为 System Engine |
| Business openGauss    | 5435     | 5432        | openGauss 6.0.6 PG 兼容模式测试源；以 `engine_type=opengauss` 注册为 System Engine |
| Business KingbaseES   | 5436     | 54321       | KingbaseES V9R1C10 PG 模式 owner-managed disposable 测试源；以 `engine_type=kingbase` 注册为 System Engine |
| Business 达梦 DM8     | 5236     | 5236        | ARM64 官方介质 disposable 数据库；以 `engine_type=dameng` 注册为 System Engine |

## 端口分配规则

### 后端端口规则
- **核心模块**：808x 系列（8081-8087）
  - 8081: Manager
  - 8082: Meta
  - 8083: Transfer
  - 8084: Orchestrator
  - 8185: Develop
  - 8086: Service
  - 8087: Copilot
- **特殊模块**：
  - 8180: System（避免与企业微信等应用的 8080 端口冲突）
  - 8100: Monitor
  - **8110: Standard（数据标准管理）**
  - **8181: Model（数据建模）**
  - **8182: Quality（数据质量）**
  - **8183: Asset（数据资产管理）**
  - **8184: Portal（数据消费者门户）**
  - **8191: Inference（统一 AI 推理）**
  - **8192: Catalog（企业资源目录）**
  - **8193: Workbench（服务消费工作台）**
  - **8194: Security（数据安全）**
- **引擎服务**：808x-809x 系列
  - 8089: Math Workflow Engine（参考实现，自动启动服务、手动注册）
  - 8097: Jupyter API Server
  - 8098: Spark Workflow Engine
  - 8099: GeoPython Workflow
  - 8101: Model3D Workflow Engine
  - 8102: PointCloud Workflow Engine
  - 8103: SuperMap Workflow Engine
  - 8105: Document Workflow Engine

### 前端开发端口规则
- **Console**：5170（控制台入口）
- **核心模块**：517x 系列（5173-5180）
  - 5173: System
  - 5174: Manager
  - 5175: Meta
  - 5176: Transfer
  - 5177: Orchestrator
  - 5178: Develop
  - 5179: Monitor
  - 5180: Service
- **新模块**：518x 系列
  - **5181: Standard**
  - **5182: Model**
  - **5183: Quality**
  - **5184: Asset**
  - **5185: Portal**
  - **5186: Agent**
  - **5187: Graph**
  - **5188: Inference**
  - **5189: Catalog**
  - **5190: Workbench**
  - **5191: Security**
  - **5192: Ontology**

### 前端 Docker 端口规则
- **核心模块**：809x 系列（8090-8096）
  - 8090: System
  - 8091: Manager
  - 8092: Meta
  - 8093: Transfer
  - 8094: Orchestrator
  - 8095: Develop
  - 8096: Service
- **特殊模块**：
  - 5179: Monitor（开发和 Docker 端口一致）
  - **8111: Model（811x 系列起始）**
  - **8112: Standard（811x 系列）**
  - **8113: Quality（811x 系列）**
  - **8114: Asset**
  - **8115: Portal**
  - **8116: Monitor**
  - **8117: Agent**
  - **8118: Graph**
  - **8119: Inference**
  - **8120: Catalog**
  - **8121: Workbench**
  - **8122: Security**
  - **8123: Ontology**

## Standard 和 Model 模块配置要求

### Standard 模块（数据标准管理）

**功能**：管理业务域、业务术语、数据元、码值集等数据标准。

**端口配置**：
- Backend 开发：`8110`
- Backend Docker：`8110`
- Frontend 开发：`5181`
- Frontend Docker：`8112`

**配置文件**：
- `.env`: `STANDARD_BACKEND_PORT=8110`, `STANDARD_FRONTEND_PORT=5181`
- `standard/frontend/vite.config.js`: `port: 5181`
- `docker-compose.yml`: `"8110:8110"` (backend), `"8112:80"` (frontend, 待添加)

### Model 模块（数据建模）

**功能**：管理业务实体、逻辑表、数仓分层等数据模型。

**端口配置**：
- Backend 开发：`8181`
- Backend Docker：`8181`
- Frontend 开发：`5182`
- Frontend Docker：`8111`

**配置文件**：
- `.env`: `MODEL_BACKEND_PORT=8181`, `MODEL_FRONTEND_PORT=5182`
- `model/frontend/vite.config.js`: `port: 5182`
- `docker-compose.yml`: `"8181:8181"` (backend), `"8111:80"` (frontend)

**⚠️ 注意事项**：
1. **Standard Backend (8110)** 和 **Model Backend (8181)** 端口不得冲突
2. **Standard Frontend (5181)** 和 **Model Frontend (5182)** 开发端口不得冲突
3. 所有端口配置必须在 `.env`、`vite.config.js`、`docker-compose.yml` 中保持一致
4. Gateway 需要正确配置 Standard 和 Model 服务的路由映射
