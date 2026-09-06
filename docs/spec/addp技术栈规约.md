## 技术栈

### 后端

- **语言**: Go 1.23+
- **HTTP 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15（所有持久化业务模块使用各自 owner schema 隔离，例如 `system`、`manager`、`meta`、`catalog`、`security`、`transfer`、`orchestrator`、`develop`）
- **缓存/事件**: Redis 7
- **对象存储**: MinIO (兼容 S3)
- **Infra Kafka**: Redpanda v24.3.18，唯一 Kafka API broker 实现
- **Kafka Connect / Debezium**: `quay.io/debezium/connect:3.6.0.Final`，内置 Kafka Connect 4.3.0；PostgreSQL Connector 3.6.0.Final
- **有界执行领取**: PostgreSQL `common.task_executions` claim + lease；Cron 只用于 owner scheduler 计算到期 execution
- **空间计算**: GeoPython Workflow (基于 Python 的空间工作流执行引擎,内存 GeoDataFrame 处理)
- **Spark 工作流运行时**: PySpark 3.5 + OpenJDK 11；Workflow driver 与 Spark Master/Worker 必须使用相同的 JVM 主版本；JDBC 使用 PostgreSQL `42.7.4`、MySQL Connector/J `8.4.0`。分布式模式下，`SPARK_WORKFLOW_SHARED_HOST` 必须是 Workflow driver 自身和 Spark executor 都可访问的地址，用于公布 driver 地址，并替换本地开发中数据引擎连接的 loopback host。

### Go 依赖版本规范

为确保所有模块依赖版本一致，ADDP 平台使用以下统一的 Go 依赖版本（最后更新: 2026-09-06）。本节中反引号包裹的 `Go模块路径@版本` 是依赖版本检查脚本的唯一事实源；同一个 Go 模块路径只能声明一个目标版本：

#### 核心框架

- **Gin 框架**: `github.com/gin-gonic/gin@v1.11.0`
- **GORM**: `gorm.io/gorm@v1.31.2`
- **PostgreSQL 驱动**: `gorm.io/driver/postgres@v1.6.0`
- **PostgreSQL 客户端**: `github.com/lib/pq@v1.10.9`
- **PostgreSQL 连接池**: `github.com/jackc/pgx/v5@v5.7.2`

#### 认证与加密

- **用户令牌**: System 生成随机 opaque Token，只保存 SHA-256 Hash
- **密码学**: `golang.org/x/crypto@v0.47.0`

#### 数据库驱动

- **DuckDB**: `github.com/duckdb/duckdb-go/v2@v2.5.6`（DuckDB 1.4.5 LTS）
  - 原生依赖只允许存在于 `engines/duckdb`。Develop 和 Service 统一通过 `FederatedQueryRuntimeProvider` 调用独立 Runtime，不得链接 DuckDB 驱动。
  - 用户 SQL 执行前必须完成授权引擎挂载和对象路径白名单配置，再关闭 DuckDB 外部访问并锁定安全配置。
- **MySQL**: `github.com/go-sql-driver/mysql@v1.9.3` ⚠️ **必须使用此版本**
  - **Doris 兼容性要求**: v1.8.x 版本无法正常连接 Doris,会返回 "invalid connection" 错误
  - **影响**: 所有需要连接 MySQL/Doris 的模块必须使用 v1.9.3+
  - **相关模块**: System (资源测试), Develop (SQL 工作台)
- **MySQL SQL AST**: `github.com/xwb1989/sqlparser@v0.0.0-20180606152119-120387863bf2`
  - **用途**: Common MySQL Provider 对无普通函数的只读 SELECT/JOIN/派生子查询生成完整 `QueryReadSet`。
  - **边界**: 解析失败、CTE、View、函数或非基础表必须返回 unresolved，不得回退到顶层对象摘要或字符串匹配。

#### 缓存与执行领取

- **Redis 客户端**: `github.com/redis/go-redis/v9@v9.17.2`
- **有界执行队列**: PostgreSQL + GORM，不引入独立消息队列依赖

#### 对象存储

- **MinIO**: `github.com/minio/minio-go/v7@v7.0.97`
- **AWS SDK**: `github.com/aws/aws-sdk-go@v1.45.0`

#### 全文搜索

- **Meilisearch**: `github.com/meilisearch/meilisearch-go@v0.26.0`

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

- **CORS 中间件** (Meta): `github.com/gin-contrib/cors@v1.5.0`
- **Hive 客户端** (Develop): `github.com/beltran/gohive@v1.8.1`
- **SQLite 驱动** (Manager): `gorm.io/driver/sqlite@v1.6.0`
- **MySQL 驱动** (Develop): `gorm.io/driver/mysql@v1.6.0`
- **测试框架** (Transfer): `github.com/stretchr/testify@v1.11.1`

**重要提示**:

- 新模块开发时，请严格遵循上述版本
- 升级依赖前，需在所有模块中统一升级
- 所有版本号最后更新时间记录在文档顶部

### 前端

- **运行时**: Node.js 24（根目录 `.node-version` 是工具链版本的唯一事实源）
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
