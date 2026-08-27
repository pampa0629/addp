# Port Allocation (ADDP)

统一定义 ADDP 系统库与业务库的容器端口，避免冲突。

## System (ADDP 基础设施)

- PostgreSQL: `15432`
- Redis: `16379`
- MinIO API: `19000`
- MinIO Console: `19001`
- Meilisearch: `17700`
- Infra Kafka bootstrap: `19092`
- Kafka Connect REST: `18083`

PostgreSQL、Redis、MinIO 和 Meilisearch 当前来源于 `docker-compose.infra.yml`；Infra Kafka/Kafka Connect 端口已由工作包 3A 保留，工作包 3B 才写入 compose 和启动脚本。脚本固定使用这些端口，不会自动改动；若被其他进程占用，`scripts/infra/up.sh` 会给出提示，可能导致启动失败。请使用 `lsof -nP -i :<port>` 查占用并释放，或手动调整 compose 端口映射。

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

来源：`business/docker-compose.yml`，可通过 `business/.env` 覆盖。脚本固定使用这些端口，不会自动改动；若被其他进程占用，启动脚本会给出警告并继续尝试（可能失败）。

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
```

## Reserved Policy（保留规则）

- **System MinIO 使用 19000/19001**，Business 侧不得占用这两个端口。
- **Business MinIO 使用 9002/9003**，System 侧不得占用这两个端口。
- System PostgreSQL 使用 15432；Business PostgreSQL 使用 5433。
- SuperMap SDX+ for PostgreSQL 专用实例使用 5434，且不得安装 PostGIS 或与 5433 的 SuperMap SDX+ for PostGIS 工作区共用数据卷。
- Infra Kafka 使用 19092；Business Kafka 使用 29092。两者必须是独立集群，Business Kafka 才能注册为 System Engine。

脚本约束：

- `scripts/infra/up.sh`：若检测到 `business-minio` 占用了 19000/19001，将报错并退出，提示修改 `business/.env`。
- `business/scripts/start.sh`：若配置了 19000/19001，将报错并退出，提示改为 9002/9003；对 5433 端口仅警告不改动。

## 快速校验

使用命令校验策略是否符合：

```
make ports-validate
```

输出会显示 business/.env 的端口配置、System 默认端口以及当前运行容器的实际映射，帮助定位问题。

如果本地已有其他服务占用 19000/19001，可改用其他未占用端口。

## 使用建议

- 同机运行 System 与 Business：
  - 先启动 Business：`cd business && ./scripts/start.sh`
  - 再启动 System 基础设施：`bash scripts/infra/up.sh` 或 `make infra-up`
  - 注册到 ADDP 的 Business 引擎地址应使用容器可访问地址，例如 `business-postgres:5432`、`business-minio:9000`；不要使用 `localhost`，因为连接测试由 ADDP 容器内服务发起。
- 如遇端口冲突：
  - 参考本文件调整 `business/.env` 或根目录 `.env`
  - 重新启动对应容器：`docker-compose down && docker-compose up -d`


### 端口分配

**ADDP 系统服务**:


| 服务                  | 开发端口 | Docker 端口 | 说明                       |
| --------------------- | -------- | ----------- | -------------------------- |
| **Nginx Gateway**     | **80**   | **80**      | **统一入口 (推荐)**        |
| **Console Frontend**   | **5170** | **5170**    | **Console UI (通过 Nginx)** |
| Gateway               | 8000     | 8000        | API Gateway (后端路由)     |
| System Backend        | 8180     | 8180        | 认证、用户、日志 (统一使用8180避免端口冲突) |
| System Frontend       | 5173     | 8090        | 独立访问                   |
| Manager Backend       | 8081     | 8081        | 数据源、文件               |
| Manager Frontend      | 5174     | 8091        | 独立访问                   |
| Meta Backend          | 8082     | 8082        | 元数据、血缘               |
| Meta Frontend         | 5175     | 8092        | 独立访问                   |
| Transfer Backend      | 8083     | 8083        | 数据同步任务               |
| Transfer Frontend     | 5176     | 8093        | 独立访问                   |
| Orchestrator Backend  | 8084     | 8084        | 工作流编排                 |
| Orchestrator Frontend | 5177     | 8094        | 独立访问                   |
| Develop Backend       | 8185     | 8185        | 开发工具                   |
| Develop Frontend      | 5178     | 8095        | 独立访问                   |
| Service Backend       | 8086     | 8086        | 数据服务、OGC 标准服务     |
| Service Frontend      | 5180     | 8096        | 独立访问                   |
| Monitor Backend       | 8100     | 8100        | 执行监控、统计分析         |
| Monitor Frontend      | 5179     | 5179        | 监控仪表盘                 |
| **Standard Backend**  | **8110** | **8110**    | **数据标准管理（业务域、术语、数据元、码值集）** |
| **Standard Frontend** | **5181** | **8112**    | **标准管理 UI**            |
| **Model Backend**     | **8181** | **8181**    | **数据建模（业务实体、逻辑表、数仓分层）** |
| **Model Frontend**    | **5182** | **8111**    | **建模 UI**                |
| **Quality Backend**   | **8182** | **8182**    | **数据质量检查、评分（质量规则执行层）** |
| **Quality Frontend**  | **5183** | **8113**    | **质量管理 UI**            |
| **Asset Backend**     | **8183** | **8183**    | **数据资产管理（编目、申请、授权）** |
| **Asset Frontend**    | **5184** | **8114**    | **资产管理 UI**            |
| **Portal Backend**    | **8184** | **8184**    | **数据消费者门户 BFF**      |
| **Portal Frontend**   | **5185** | **8115**    | **数据门户 UI**            |
| Copilot Backend       | 8087     | 8087        | AI 助手 (查询/工作流/Notebook 生成) |
| **Agent Backend**     | **8190** | **8190**    | **Agent AI 对话助手后端**  |
| **Agent Frontend**    | **5186** | **8117**    | **Agent 对话界面 UI**      |
| **Graph Backend**     | **8186** | **8186**    | **知识图谱本体建模、图谱管理** |
| **Graph Frontend**    | **5187** | **8118**    | **知识图谱 UI**            |
| **Inference Backend** | **8191** | **8191**    | **统一 AI 推理控制面与数据面** |
| **Inference Frontend** | **5188** | **8119**   | **Provider、模型和 Profile 管理 UI** |
| **Catalog Backend**   | **8192** | **8192**    | **企业数据目录身份、业务语义关联、责任和搜索** |
| **Catalog Frontend**  | **5189** | **8120**    | **企业数据目录管理 UI** |
| **Workbench Backend** | **8193** | **8193**    | **已发布服务消费、动态查询和数据应用创作** |
| **Workbench Frontend** | **5190** | **8121**   | **Workbench 创作端 UI** |
| Math Workflow Engine  | 8089     | 8089        | 数学计算工作流参考实现（自动启动服务、手动注册） |
| Jupyter API Server    | 8097     | 8097        | Jupyter 执行引擎 API       |
| Spark Workflow Engine | 8098     | 8098        | Spark 分布式工作流引擎     |
| GeoPython Workflow     | 8099     | 8099        | 空间计算引擎 (Python)      |
| Model3D Workflow Engine    | 8101     | 8101        | 三维模型转换工作流引擎     |
| PointCloud Workflow Engine | 8102     | 8102        | 点云处理工作流引擎         |
| SuperMap Workflow Engine   | 8103     | 8103        | 超图 iObjects C++ 空间计算工作流引擎 |
| DuckDB Query Runtime       | 8104     | 8104        | 联邦只读查询计算引擎       |
| PostgreSQL (System)   | 15432    | 15432       | ADDP 系统元数据            |
| Redis                 | 16379    | 16379       | 缓存、事件和分布式锁       |
| MinIO System API      | 19000    | 19000       | 系统文件存储               |
| MinIO System Console  | 19001    | 19001       | 系统 MinIO Web UI          |
| Meilisearch           | 17700    | 17700       | 全文检索引擎               |
| Infra Kafka           | 19092    | 9092        | 内部 CDC 总线；不注册为 System Engine |
| Kafka Connect REST    | 18083    | 8083        | Transfer capture supervisor 内部控制面，不经 Gateway 暴露 |
| Business Kafka        | 29092    | 9092        | 业务 Topic；以 `engine_type=kafka` 注册为 System Engine |
| Business Oracle       | 15210    | 1521        | Oracle Free 普通表与 Oracle Spatial 测试源；以 `engine_type=oracle` 注册为 System Engine |

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
  - **8192: Catalog（企业数据目录）**
  - **8193: Workbench（服务消费工作台）**
- **引擎服务**：808x-809x 系列
  - 8089: Math Workflow Engine（参考实现，自动启动服务、手动注册）
  - 8097: Jupyter API Server
  - 8098: Spark Workflow Engine
  - 8099: GeoPython Workflow
  - 8101: Model3D Workflow Engine
  - 8102: PointCloud Workflow Engine
  - 8103: SuperMap Workflow Engine

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
