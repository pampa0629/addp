## 技术栈

### 后端

- **语言**: Go 1.23+
- **HTTP 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15 (所有模块使用 schema 隔离: system, manager, metadata, transfer, orchestrator, develop)
- **缓存/队列**: Redis 7
- **对象存储**: MinIO (兼容 S3)
- **任务队列**: Asynq (基于 Redis,用于 Transfer 模块), Cron (用于 Meta 模块调度)
- **空间计算**: GeoPandas Engine (基于 Python 的空间工作流执行引擎,内存 GeoDataFrame 处理)

### Go 依赖版本规范

为确保所有模块依赖版本一致，ADDP 平台使用以下统一的 Go 依赖版本（最后更新: 2025-12-17）：

#### 核心框架

- **Gin 框架**: `github.com/gin-gonic/gin@v1.11.0`
- **GORM**: `gorm.io/gorm@v1.25.12`
- **PostgreSQL 驱动**: `gorm.io/driver/postgres@v1.6.0`
- **PostgreSQL 客户端**: `github.com/lib/pq@v1.10.9`
- **PostgreSQL 连接池**: `github.com/jackc/pgx/v5@v5.7.2`

#### 认证与加密

- **JWT**: `github.com/golang-jwt/jwt/v5@v5.3.0`
- **密码学**: `golang.org/x/crypto@v0.43.0`

#### 数据库驱动

- **MySQL**: `github.com/go-sql-driver/mysql@v1.9.3` ⚠️ **必须使用此版本**
  - **Doris 兼容性要求**: v1.8.x 版本无法正常连接 Doris,会返回 "invalid connection" 错误
  - **影响**: 所有需要连接 MySQL/Doris 的模块必须使用 v1.9.3+
  - **相关模块**: System (资源测试), Develop (SQL 工作台)

#### 缓存与队列

- **Redis 客户端**: `github.com/redis/go-redis/v9@v9.17.2`
- **异步任务队列**: `github.com/hibiken/asynq@v0.25.1`

#### 对象存储

- **MinIO**: `github.com/minio/minio-go/v7@v7.0.95`
- **AWS SDK**: `github.com/aws/aws-sdk-go@v1.45.0`

#### 全文搜索

- **Meilisearch**: `github.com/meilisearch/meilisearch-go@v0.26.0`

#### 地理与空间数据

- **几何处理**: `github.com/twpayne/go-geom@v1.6.1`
- **Shapefile**: `github.com/jonas-p/go-shp@v0.1.1`
- **向量数据库**: `github.com/pgvector/pgvector-go@v0.1.0`

#### Excel 与文档处理

- **Excel**: `github.com/xuri/excelize/v2@v2.10.0`

#### 工具库

- **UUID**: `github.com/google/uuid@v1.6.0`
- **环境变量**: `github.com/joho/godotenv@v1.5.1`
- **Cron 调度**: `github.com/robfig/cron/v3@v3.0.1`

#### 模块特定依赖

- **CORS 中间件** (Meta): `github.com/gin-contrib/cors@v1.5.0`
- **Hive 客户端** (Develop): `github.com/beltran/gohive@v1.6.0`
- **SQLite 驱动** (Manager): `gorm.io/driver/sqlite@v1.6.0`
- **MySQL 驱动** (Develop): `gorm.io/driver/mysql@v1.6.0`
- **测试框架** (Transfer): `github.com/stretchr/testify@v1.11.1`

**重要提示**:

- 新模块开发时，请严格遵循上述版本
- 升级依赖前，需在所有模块中统一升级
- 所有版本号最后更新时间记录在文档顶部

### 前端

- **框架**: Vue 3 + Composition API
- **构建工具**: Vite
- **UI 库**: Element Plus
- **状态管理**: Pinia
- **路由**: Vue Router
- **HTTP 客户端**: Axios (带认证拦截器)
