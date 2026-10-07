## 技术栈

### 后端

- **语言**: Go 1.23+
- **HTTP 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15（所有持久化业务模块使用各自 owner schema 隔离，例如 `system`、`manager`、`meta`、`catalog`、`security`、`transfer`、`orchestrator`、`develop`）
- **缓存/事件**: Redis 7
- **对象存储**: MinIO (兼容 S3)
- **Infra Kafka**: Redpanda v24.3.18，唯一 Kafka API broker 实现
- **指标控制面 TLS 转发**: Nginx 1.28.0 Alpine，`scripts/infra/metrics-control.yml` 锁定多架构 digest，正式可选部署与指标 T2 共用模板，仅私有网络两个固定接口。
- **Linux 主机指标来源**: node_exporter v1.12.1，正式部署与独占 T2 共用 `scripts/infra/node-metrics.yml` 固定多架构 digest 和 exporter-toolkit 原生 mTLS；仅部署方明确选择的节点取得宿主只读资源权限，Docker Desktop T2 不计为物理宿主纳管。
- **Kafka Connect / Debezium**: `quay.io/debezium/connect:3.6.0.Final`，内置 Kafka Connect 4.3.0；PostgreSQL Connector 3.6.0.Final
- **有界执行领取**: PostgreSQL `common.task_executions` claim + lease；Cron 只用于 owner scheduler 计算到期 execution
- **空间计算**: GeoPython Workflow (基于 Python 的空间工作流执行引擎,内存 GeoDataFrame 处理)
- **GeoPython 原生开发运行时**: Python 3.12 独立虚拟环境、匹配原生 GDAL 版本的 Python 绑定、unixODBC 与 MDBTools。macOS 的 GDAL、PROJ 和 pkg-config 固定来自 Homebrew，版本、资源目录与 Python 绑定编译使用同一来源，不随 Conda 的 PATH 切换。准备入口先验证 GDAL Python 绑定的加载与版本；缺失、加载失败或版本不匹配时，在已有依赖安装锁内禁用缓存、从源码重建匹配版本的绑定，不变更其他 Python 依赖。重建后必须重新验证，失败时在停止旧服务前退出。必须保留 PGeo 只读、OpenFileGDB 读写及 GTiff/COG 能力，资源目录和 ODBC 配置只影响 Runtime 子进程。
- **Manager 栅格镶嵌原生运行时**: Python 3.12 独立虚拟环境，与 GeoPython 共用原生 GDAL 工具链和失效绑定重建入口；macOS 使用 Homebrew GDAL/PROJ，禁止继承 Anaconda 系统包。仅安装自身 requirements，验证 GTiff/COG、GDAL NumPy 数组与 EPSG 解析，不引入 MDBTools 依赖。
- **Spark 工作流运行时**: PySpark 3.5 + OpenJDK 11；Workflow driver 与 Spark Master/Worker 必须使用相同的 JVM 主版本；JDBC 使用 PostgreSQL `42.7.4`、MySQL Connector/J `8.4.0`。分布式模式下，`SPARK_WORKFLOW_SHARED_HOST` 必须是 Workflow driver 自身和 Spark executor 都可访问的地址，用于公布 driver 地址，并替换本地开发中数据引擎连接的 loopback host。
  - Python Sedona 与 Spark 3.5 shaded JAR 统一为 `1.5.3`，GeoTools wrapper 为 `1.5.3-28.2`。Sedona 1.5.1 将可选 `cdm-core` 错置为必需依赖，干净环境无法从 Maven Central 完成解析；该缺陷已在 1.5.2 后修复，见 [官方版本说明](https://sedona.apache.org/1.5.3/setup/maven-coordinates/)。镜像验证必须经生产 SparkConnector 建立本地会话，完成真实聚合和空间函数，覆盖冷启动依赖解析；不能只测试不加载 Sedona 的裸 SparkSession。
  - 本地开发使用 Python 3.11/3.12 虚拟环境与原生 OpenJDK 11；Java 环境仅注入 Spark 子进程，不改变其他开发模块。
  - 产品镜像使用 Python 3.11 / Debian Bookworm，Java 11 JRE 从官方 Eclipse Temurin `11-jre-jammy` 镜像取得；不使用已结束 LTS、软件包源失效的 Bullseye。镜像必须安装同一仓库的 `common-python`，使用根目录构建上下文；构建门禁验证依赖一致性、API 导入及真实 Spark 计算。

### Go 依赖版本规范

为确保所有模块依赖版本一致，ADDP 平台使用以下统一的 Go 依赖版本（最后更新: 2026-10-05）。本节中反引号包裹的 `Go模块路径@版本` 是依赖版本检查脚本的唯一事实源；同一个 Go 模块路径只能声明一个目标版本：

#### 核心框架

- **Gin 框架**: `github.com/gin-gonic/gin@v1.11.0`
- **GORM**: `gorm.io/gorm@v1.31.2`
- **PostgreSQL 驱动**: `gorm.io/driver/postgres@v1.6.0`
- **PostgreSQL 客户端**: `github.com/lib/pq@v1.10.9`
- **PostgreSQL 连接池**: `github.com/jackc/pgx/v5@v5.7.2`

#### 认证与加密

- **用户令牌**: System 生成随机 opaque Token，只保存 SHA-256 Hash
- **密码学**: `golang.org/x/crypto@v0.47.0`
- **SM3**: `github.com/emmansun/gmsm@v0.34.1`，仅 Common 保护执行器使用 SM3，不改变 Go 或现有密码学依赖版本。

#### 数据库驱动

- **DuckDB**: `github.com/duckdb/duckdb-go/v2@v2.5.6`（DuckDB 1.4.5 LTS）
  - 原生依赖只允许存在于 `engines/duckdb`。Develop 和 Service 统一通过 `FederatedQueryRuntimeProvider` 调用独立 Runtime，不得链接 DuckDB 驱动。
  - 用户 SQL 执行前必须完成授权引擎挂载和对象路径白名单配置，再关闭 DuckDB 外部访问并锁定安全配置。
- **MySQL**: `github.com/go-sql-driver/mysql@v1.9.3` ⚠️ **必须使用此版本**
  - **Doris 兼容性要求**: v1.8.x 版本无法正常连接 Doris,会返回 "invalid connection" 错误
  - **影响**: 所有需要连接 MySQL/Doris 的模块必须使用 v1.9.3+
  - **相关模块**: System (资源测试), Develop (SQL 工作台)
- **MySQL SQL AST**: `github.com/dolthub/vitess@v0.0.0-20250512224608-8fb9c6ea092c`
  - **用途**: Common MySQL Provider 使用单一 AST 路径分析 SELECT、JOIN、派生查询、非递归 CTE 和 UNION ALL；删除旧解析器依赖。
  - **边界**: 只接受显式审查的无外部读取内置函数；递归 CTE、View、存储函数、可执行注释、行锁及无法闭合的来源必须返回 unresolved，不得回退到字符串匹配或直接执行。CTE 按词法作用域解析，不作为物理表；计算字段不得伪装成源字段直接投影。

#### 缓存与执行领取

- **Redis 客户端**: `github.com/redis/go-redis/v9@v9.17.2`
- **有界执行队列**: PostgreSQL + GORM，不引入独立消息队列依赖

#### 对象存储

- **MinIO**: `github.com/minio/minio-go/v7@v7.0.97`
- **AWS SDK**: `github.com/aws/aws-sdk-go@v1.45.0`

#### 全文搜索

- **Meilisearch**: `github.com/meilisearch/meilisearch-go@v0.36.3`（Manager、Catalog、Asset 统一；正式服务固定 `getmeili/meilisearch:v1.54.3`，新文档投递使用任务 `customMetadata`，关闭 SDK 自动重试）

#### 地理与空间数据

- **几何处理**: `github.com/twpayne/go-geom@v1.6.1`
- **Shapefile**: `github.com/jonas-p/go-shp@v0.1.1`
- **CRS 定义与坐标转换 Runtime**: GeoPython Workflow `pyproj==3.7.2`；只使用镜像内本地 PROJ database，固定 `PROJ_NETWORK=OFF`
- **向量数据库**: PostgreSQL `pgvector` 扩展；业务模块通过各自 owner 表和 repository 查询，不引入独立 `pgvector-go` 存储客户端。

#### Excel 与文档处理

- **Excel**: `github.com/xuri/excelize/v2@v2.10.0`

#### 工具库

- **UUID**: `github.com/google/uuid@v1.6.0`
- **环境变量**: `github.com/joho/godotenv@v1.5.1`
- **Cron 调度**: `github.com/robfig/cron/v3@v3.0.1`

#### API 文档

- **Swagger**: `github.com/swaggo/swag@v1.16.6`
- **Gin Swagger**: `github.com/swaggo/gin-swagger@v1.6.1`
- **Swagger Files**: `github.com/swaggo/files@v1.0.1`

#### 模块特定依赖

- **有限分类规则** (Ontology): `cel.dev/cel-go@v0.32.0`；仅允许经过内核 AST 白名单检查的布尔/字符串规则，不开放完整 CEL 函数集。
- **语义图投影适配** (Ontology): 复用上述 go-redis 版本直接发送固定 FalkorDB 命令，不引入 falkordb-go SDK 或多驱动回退；FalkorDB 4.20.6 的 Infra 单机部署与独占 disposable T2 共用 `scripts/infra/falkordb.yml` 固定镜像 digest，不表示 Ontology 发布执行器或生产 HA/TLS 已上线。
- **CORS 中间件** (Meta): `github.com/gin-contrib/cors@v1.5.0`
- **Hive 客户端** (Develop): `github.com/beltran/gohive@v1.8.1`
- **SQLite 驱动** (Manager): `gorm.io/driver/sqlite@v1.6.0`
- **MySQL 驱动** (Develop): `gorm.io/driver/mysql@v1.6.0`
- **测试框架** (Transfer): `github.com/stretchr/testify@v1.11.1`

**重要提示**:

- 新模块开发时，请严格遵循上述版本
- 升级依赖前，需在所有模块中统一升级
- 所有版本号最后更新时间记录在文档顶部

### Python AI 执行依赖

- Agent 运行环境为 Python 3.12（开发、测试 CI 和产品镜像一致）。DeerFlow Harness 与 Extension API 从官方源码提交 `130a9ab9f035a8f319502f8d5e044ed05962b2aa` 的两个 package 子目录安装，不使用浮动分支。
- Agent 固定 `langchain==1.4.3`、`langgraph==1.2.12`。共享 `inference-langchain` extra 固定 `langchain-core==1.6.6`，Agent 与 Copilot 使用同一适配器；Copilot 只消费 Core，不安装未使用的 Agent 框架或 Community 集成。
- Agent HTTP 运行栈使用 `fastapi==0.142.2`，搭配该 DeerFlow 提交已锁定的稳定版 `langgraph-api==0.10.0`、`langgraph-runtime-inmem==0.30.0`，以满足 Starlette 依赖；不采用旧版 HTTP 栈配合预发布 Runtime 的隐式降级组合。
- DeerFlow 只作为 Agent 进程内 SDK 使用；平台授权、业务副作用、交互和语义检查点仍属于 ADDP，不新增 DeerFlow 服务或框架持久化数据库。

### 前端技术栈

- **运行时**: Node.js 24（根目录 `.node-version` 是工具链版本的唯一事实源）
- 前端产品镜像同样读取根 `.node-version`，由唯一镜像构建入口传入 `NODE_VERSION`；Dockerfile 不维护另一个 Node 版本或默认值。
- 前端产品镜像必须复制 `package.json` 与 `package-lock.json` 后执行 `npm ci`。包括 Rollup 平台包在内的依赖均由锁文件确定，不允许构建期间执行 `npm install` 或绕过锁文件。
- **框架**: Vue 3 + Composition API
- **构建工具**: Vite
- **UI 库**: Element Plus
- **状态管理**: Pinia
- **路由**: Vue Router
- **HTTP 客户端**: Axios (带认证拦截器)

### 前端依赖版本规范

为确保所有前端模块的依赖版本一致，ADDP 平台使用以下统一的前端依赖版本（最后更新: 2026-02-15）：

#### 核心框架

- **Vue**: `vue@3.5.13`
- **Vue Router**: `vue-router@4.5.0`
- **Vite**: `vite@6.0.5`
- **@vitejs/plugin-vue**: `@vitejs/plugin-vue@5.2.1`

#### UI 组件库

- **Element Plus**: `element-plus@2.8.8` ⚠️ **必须使用此版本**
  - **版本选择**: 2.8.8 是最后一个无弃用警告的 2.x 稳定版本
  - **稳定性**: 包含所有必需功能，无控制台警告
  - **影响**: 所有前端模块必须统一使用此版本
  - **重要**: 使用新的 API 规范，避免使用已弃用的属性
    - ✅ **正确**: `<el-button text>文本按钮</el-button>` (使用 `text` 属性)
    - ❌ **错误**: `<el-button type="text">文本按钮</el-button>` (旧的 API，虽然在 2.8.8 中仍可用，但为了代码一致性应统一使用新 API)
    - 同理，链接按钮使用 `<el-button link>`
- **@element-plus/icons-vue**: `@element-plus/icons-vue@2.3.2`

#### 状态管理与 HTTP

- **Pinia**: `pinia@2.3.0`
- **Axios**: `axios@1.7.9`

#### 地图与可视化

- **OpenLayers** (Manager/Service): `ol@9.2.4`
- **ECharts** (Monitor): `echarts@5.5.1`

#### 编辑器

- **Monaco Editor** (Develop): `monaco-editor@0.45.0`
- **SQL Formatter** (Develop): `sql-formatter@15.4.7`

**重要提示**:

- 新模块开发时，请严格遵循上述版本
- 升级依赖前，需在所有模块中统一升级
- Element Plus 2.8.8 是当前推荐的稳定版本，避免升级到 2.9+ 产生警告

**Vue 版本统一要求**:

- 所有模块必须共享**单一 Vue 实例**，避免多实例导致生命周期钩子失效
- `common-frontend` 作为共享库使用 `peerDependencies`，**不得有 node_modules 目录**
- 各前端模块的 `package.json` 必须添加 `overrides` 配置强制统一 Vue 版本，示例：
  ```json
  "overrides": {
    "vue": "3.5.13"
  }
  ```
- 安装依赖前需确保删除 `common-frontend/node_modules` 和 `common-frontend/package-lock.json`

### Business Redis 运行基线

Business Redis 使用 `public.ecr.aws/docker/library/redis:7.2.13@sha256:37aa82f9fdff30517603b2e2c5376b34b106d353c2b508262260d3bb0d2c21ba`，由 Docker 官方账号发布，固定多架构 manifest（AMD64、ARM64）。现有 Infra Redis 版本不在该变更范围内；平台 Redis 业务引擎首版通过现有 `go-redis/v9@v9.17.2` 支持单端点 ACL 连接登记、逻辑数据库选择和 TLS 验证；不声明 Cluster、Sentinel 或 key 级数据访问能力。

### Elasticsearch 接入依赖

- **Elasticsearch Go 客户端**：`github.com/elastic/go-elasticsearch/v9@v9.3.0`，使用官方低层 HTTP 客户端；该版本要求 Go 1.24，保持当前 Common 的 Go 1.24.2 基线。
- **Elasticsearch Business/T2**：官方 `docker.elastic.co/elasticsearch/elasticsearch:9.5.4@sha256:82ac14f43fe701992e601f4cc81e1c0d7dbc5a2576d8cd736006452925df4026`，同一 manifest 支持 linux/amd64 与 linux/arm64。首版仅认证这组服务端与客户端，不启用旧主版本兼容路线。

Model3D Workflow Runtime 使用 `Pillow==12.3.0` 校验生成 GLB 中的 PNG/JPEG 编码和实际解码，Python 版本遵循统一 Python runtime 规约。
