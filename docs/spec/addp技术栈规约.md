# ADDP 技术栈规约

## 技术栈

### 后端

- **语言**: Go 1.24.2（工作区基线）
- **HTTP 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15（所有持久化业务模块使用各自 owner schema 隔离，例如 `system`、`manager`、`meta`、`catalog`、`security`、`transfer`、`orchestrator`、`develop`）
- **缓存/事件**: Redis 7
- **对象存储**: MinIO (兼容 S3)
- **Infra Kafka**: Redpanda v24.3.18，唯一 Kafka API broker 实现
- **指标控制面 TLS 转发**: Nginx 1.28.0 Alpine，`scripts/infra/metrics-control.yml` 锁定多架构 digest，正式可选部署与指标 T2 共用模板，仅私有网络两个固定接口。
- **Linux 主机指标来源**: node_exporter v1.12.1，正式部署与独占 T2 共用 `scripts/infra/node-metrics.yml` 固定多架构 digest 和 exporter-toolkit 原生 mTLS；仅部署方明确选择的节点取得宿主只读资源权限，Docker Desktop T2 不计为物理宿主纳管。
- **PostgreSQL 固定指标来源**: SQL Exporter 0.24.11，`scripts/infra/postgres-metrics.yml` 锁定 Linux ARM64/amd64 镜像摘要，固定单查询与单连接；复用 Nginx 受控 mTLS，仅开放指标读取。当前模板与独占 T2 已接入，标准来源生命周期和 Monitor 专业读取尚待实施，未默认部署。
- **Kafka Connect / Debezium**: `quay.io/debezium/connect:3.6.0.Final`，内置 Kafka Connect 4.3.0；PostgreSQL Connector 3.6.0.Final
- **有界执行领取**: PostgreSQL `common.task_executions` claim + lease；Cron 只用于 owner scheduler 计算到期 execution
- **空间计算**: GeoPython Workflow (基于 Python 的空间工作流执行引擎,内存 GeoDataFrame 处理)
- **GeoPython 原生开发运行时**: Python 3.12 独立虚拟环境、匹配原生 GDAL 版本的 Python 绑定、unixODBC 与 MDBTools。macOS 的 GDAL、PROJ 和 pkg-config 固定来自 Homebrew，版本、资源目录与 Python 绑定编译使用同一来源，不随 Conda 的 PATH 切换。同一进程加载 GDAL 与 PyArrow 时，Arrow C++ 必须来自同一套原生库：GDAL 链接系统 Arrow 时，PyArrow 按该 Arrow 的版本从源码构建并动态链接同一来源，不使用自带另一套 Arrow 的 wheel。GDAL 未链接 Arrow 的平台使用声明的 PyArrow 依赖，不能把 macOS 的版本硬编码到 Linux。原生库真实路径、内容身份及构建约束进入准备指纹；依赖准备必须在原有安装锁内完成，成功准入后才能复用记录。准入在新进程中同时加载 GDAL、PyArrow，验证真实文件系统、Parquet 文件写入与重读及精确含空值整型/布尔值，不以 import 或版本号相同代替。准备入口先验证 GDAL Python 绑定的加载与版本；缺失、加载失败或版本不匹配时，在已有依赖安装锁内禁用缓存、从源码重建匹配版本的绑定，不变更其他 Python 依赖。重建后必须重新验证，失败时不启动 Runtime；运行中的 GeoPython 必须先停止，准备入口禁止改写其 venv。必须保留 PGeo 只读、OpenFileGDB 读写及 GTiff/COG 能力，资源目录和 ODBC 配置只影响 Runtime 子进程。
- **Manager 栅格镶嵌原生运行时**: Python 3.12 独立虚拟环境，与 GeoPython 共用原生 GDAL 工具链和失效绑定重建入口；macOS 使用 Homebrew GDAL/PROJ，禁止继承 Anaconda 系统包。仅安装自身 requirements，验证 GTiff/COG、GDAL NumPy 数组与 EPSG 解析，不引入 MDBTools 或 PyArrow 依赖；共享 GDAL 准备变更必须验证该消费者。
- **Spark 工作流运行时**: PySpark 3.5 + OpenJDK 11；Workflow driver 与 Spark Master/Worker 必须使用相同的 JVM 主版本；JDBC 使用 PostgreSQL `42.7.4`、MySQL Connector/J `8.4.0`。分布式模式下，`SPARK_WORKFLOW_SHARED_HOST` 必须是 Workflow driver 自身和 Spark executor 都可访问的地址，用于公布 driver 地址，并替换本地开发中数据引擎连接的 loopback host。
  - Python Sedona 与 Spark 3.5 shaded JAR 统一为 `1.5.3`，GeoTools wrapper 为 `1.5.3-28.2`。Sedona 1.5.1 将可选 `cdm-core` 错置为必需依赖，干净环境无法从 Maven Central 完成解析；该缺陷已在 1.5.2 后修复，见 [官方版本说明](https://sedona.apache.org/1.5.3/setup/maven-coordinates/)。镜像验证必须经生产 SparkConnector 建立本地会话，完成真实聚合和空间函数，覆盖冷启动依赖解析；不能只测试不加载 Sedona 的裸 SparkSession。
  - 本地开发使用 Python 3.11/3.12 虚拟环境与原生 OpenJDK 11；Java 环境仅注入 Spark 子进程，不改变其他开发模块。
  - 产品镜像使用 Python 3.11 / Debian Bookworm，Java 11 JRE 从官方 Eclipse Temurin `11-jre-jammy` 镜像取得；不使用已结束 LTS、软件包源失效的 Bullseye。镜像必须安装同一仓库的 `common-python`，使用根目录构建上下文；构建门禁验证依赖一致性、API 导入及真实 Spark 计算。

### Go 依赖版本规范

检查使用标准入口 `make test-go-dependency-policy`；检查器覆盖、失败语义及辅助脚本的写入差异见[Go 依赖辅助工具](../../scripts/utils/README.md#go-依赖辅助工具)。依赖变更按[开发维护流程](../../scripts/dev/README.md#添加新依赖)执行。

为确保所有模块依赖版本一致，ADDP 平台使用以下统一的 Go 依赖版本（最后更新: 2026-10-05）。本节中反引号包裹的 `Go模块路径@版本` 是依赖版本检查脚本的唯一事实源；同一个 Go 模块路径只能声明一个目标版本：

#### 核心框架

- **Gin 框架**: `github.com/gin-gonic/gin@v1.11.0`
- **GORM**: `gorm.io/gorm@v1.31.2`
- **PostgreSQL 驱动**: `gorm.io/driver/postgres@v1.6.0`
- **PostgreSQL 客户端**: `github.com/lib/pq@v1.10.9`
- **GaussDB 客户端**: `github.com/HuaweiCloudDeveloper/gaussdb-go@v1.0.0-rc1`，独立注册 `gaussdb` driver，仅用于已准入 Kernel 507.0.T1 的非空间只读插件；与认证事实 `scripts/lib/gaussdb-official-media.sh` 的版本和双校验和一致。RC 阶段不宣称商业版、其他内核或写入能力。
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
- **Avro 二进制 codec**: `github.com/linkedin/goavro/v2@v2.15.0`（Apache-2.0，纯 Go）；OCF 边界、解压预算和表格语义由 Common Avro 插件持有，不使用 codec 的无界 OCF 解压入口。

#### 工具库

- **UUID**: `github.com/google/uuid@v1.6.0`
- **环境变量**: `github.com/joho/godotenv@v1.5.1`
- **Cron 调度**: `github.com/robfig/cron/v3@v3.0.1`

#### 自身进程指标

- **Prometheus 指标协议**: `github.com/prometheus/client_golang@v1.23.2`，Common SDK 使用独立 registry，只注册固定的自身进程身份、CPU 累计时间、当前 RSS 和启动时间；不启用默认全局采集器。
- **Linux 自身进程读取**: `github.com/prometheus/procfs@v0.16.1`，只读当前 PID，不枚举其他进程。
- **macOS 自身进程读取**: `github.com/ebitengine/purego@v0.9.0`，调用本机 libproc 的 proc_pidinfo 并校验完整返回长度；CPU 使用 getrusage(RUSAGE_SELF)。采样失败不能产生有效零值，CGO 关闭时仍走同一路线，不使用 peak RSS 或 Go 堆作为替代。

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

### Python HTTP 追踪依赖

`common-python` 同步 HTTP Trace 使用 `opentelemetry-api==1.45.1`、`opentelemetry-sdk==1.45.1` 和 `opentelemetry-proto==1.45.1`，版本由共享 pyproject／uv.lock 固定。显式 SDK 与有界 OTLP/HTTP protobuf 导出只服务正式规范 6.5，不启用全局自动 instrument，不把依赖存在当成 Agent/Copilot 以外的 Python 服务已接入。Agent 与 Copilot 保留各自原 FastAPI／HTTPX 版本，并由共享内核同时验证；遥测依赖准备沿现有 Python owner 门禁和 Trace T2，不在启动时临时安装。

Python HTTP 指标来源使用 `prometheus-client==0.26.0`，证书检查统一使用 `cryptography==50.0.2`，由同一共享 pyproject／uv.lock 固定。该版本必须同时满足 Agent 固定 DeerFlow Harness 的 `cryptography>=50.0.0`；共享依赖变更须验证完整消费者声明在全新环境中的解析、安装与 `pip check`，不能用已有环境或单独覆盖依赖的测试代替。Python 及平台范围沿各 Runtime 原声明，不通过修改消费者依赖、跳过依赖解析或另建兼容约束绕过冲突。

### Python AI 执行依赖

- Agent 运行环境为 Python 3.12（开发、测试 CI 和产品镜像一致）。DeerFlow Harness 与 Extension API 从官方源码提交 `130a9ab9f035a8f319502f8d5e044ed05962b2aa` 的两个 package 子目录安装，不使用浮动分支。
- Agent 固定 `langchain==1.4.3`、`langgraph==1.2.12`。共享 `inference-langchain` extra 固定 `langchain-core==1.6.6`，Agent 与 Copilot 使用同一适配器；Copilot 只消费 Core，不安装未使用的 Agent 框架或 Community 集成。
- Agent HTTP 运行栈使用 `fastapi==0.142.2`，搭配该 DeerFlow 提交已锁定的稳定版 `langgraph-api==0.10.0`、`langgraph-runtime-inmem==0.30.0`，以满足 Starlette 依赖；不采用旧版 HTTP 栈配合预发布 Runtime 的隐式降级组合。
- DeerFlow 只作为 Agent 进程内 SDK 使用；平台授权、业务副作用、交互和语义检查点仍属于 ADDP，不新增 DeerFlow 服务或框架持久化数据库。

### 前端技术栈

- **运行时**: Node.js 稳定主版本以根目录 `.node-version` 为唯一事实源，文档和脚本不另行维护版本号。
- `.node-version` 只声明稳定主版本，开发、测试与构建接受该主版本的不同补丁版本，拒绝其他主版本与预发布版本。`scripts/utils/node-runtime.sh` 独占版本读取和实际 `node --version` 校验，不自动切换、安装或修改 PATH。
- 新机器先按[环境准备流程](../guide/addp部署和开发步骤.md#node-工具链准备与诊断)安装声明版本的 Node 与随附 npm，再运行 `make dev-doctor`。诊断只读当前 PATH 中实际执行的 Node/npm 版本和路径，复用上述校验；不扫描其他安装来隐式切换，不绑定版本管理器，不修改全局 Shell 配置，不下载安装或启停服务。诊断成功只表示 Node/npm 工具链就绪，不代表其他依赖、Infra 或业务服务就绪。
- 开发启动和重启必须在加锁、停止服务、安装依赖及启动进程前完成校验；锁定依赖安装、前端测试与构建入口复用同一校验。停止、服务状态查询与纯 Go 编译不额外要求 Node。版本不符必须返回非零并报告要求、实际版本和解释器位置，不保留跳过校验开关。
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

- **OpenLayers** (Manager/Service/Agent/Workbench): `ol@10.7.0`
- **Giro3D** (Manager COPC): `@giro3d/giro3d@1.0.1`，与 OpenLayers 10.7.0 配套；保留 `geotiff@2.1.3` 和 `three@0.172.0`，不得引入第二份地图或三维运行依赖
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

#### Neo4j 查询范围证明

- 标准语法解析：`github.com/cloudprivacylabs/opencypher@v1.0.0` 的 `parser` 包，来源提交 `b4c3894568190456f8144fbc4b622e1eb4a78163`，仅消费 ANTLR 生成的 openCypher 9 语法树，不集成其解释器。
- 匹配生成器的 Go ANTLR Runtime：`github.com/antlr/antlr4/runtime/Go/antlr@v0.0.0-20210803070921-b358b509191a`。两者由 Common 直接依赖，消费模块通过 Go module 锁定同一版本；升级语法后必须同时运行 Neo4j 真实门禁和拒绝边界回归。


### FalkorDB 业务图引擎

首期认证固定 `falkordb/falkordb:v4.20.6@sha256:217bc520be076ce9e85b0ba426b2379ccbaebd17f2fe6e174288bd1b36d486bb`，官方 Go 结果客户端固定 `github.com/FalkorDB/falkordb-go/v2@v2.1.0`。客户端的内部 Background I/O 必须绑定到 Owner 请求 context，取消关闭本次独占连接，不自动重试。使用已有 go-redis v9.17.2 传输，RESP2；原生参数头编码与结果解码归插件。只读范围证明仅认证原生 module version 42006，其他版本明确拒绝，需补同等认证后再放开，不把版本未知当作已支持。

`make test-common-falkordb-unit` 覆盖共享 openCypher AST 与两种图插件的确定性边界；`make test-common-falkordb` 创建独占、无持久卷实例并销毁，已登记根集成入口与 Release/T2 CI，Owner 自动发现按声明输入计算影响。Ontology Infra 的生命周期与数据不被该门禁使用。

### ORC 原生只读依赖

GeoPython Runtime 内部固定 `pyorc==0.12.0`，不由 Common Go 或业务 Backend 加载原生 ORC 库。开发准备、产品镜像与原生测试使用同一固定版本；macOS/Linux 需分别验证 ORC 与现有 GDAL/Arrow 的实际读写共存。不得为启用 ORC 替换既有同源 PyArrow。PyORC 及其打包依赖的许可证与 notices 随分发保留。
