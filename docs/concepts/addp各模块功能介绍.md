## Common 模块

`common` 模块提供共享代码,避免**所有其他后端模块**之间的重复 (Manager、Meta、Transfer、Orchestrator、Develop 和 Python Workflow Engine 集成)。

**内容**:

- [client/system.go](common/client/system.go) - SystemClient 用于与 System 模块通信
- [models/resource.go](common/models/resource.go) - 共享的 Resource 模型和 BuildConnectionString 工具
- [config/loader.go](common/config/loader.go) - 集中式配置加载,带回退

**使用模式**:

```go
// 在模块的 go.mod 中
require (github.com/addp/common v0.0.0)
replace github.com/addp/common => ../../common

// 使用别名导入以避免冲突
import (
    commonClient "github.com/addp/common/client"
    commonModels "github.com/addp/common/models"
)

// 使用 SystemClient 获取引擎
client := commonClient.NewSystemClient(systemURL, jwtToken)
engines, err := client.ListEngines("postgresql")
engine, err := client.GetEngine(engineID)

// 构建连接字符串 (自动解密密码)
connStr, err := commonModels.BuildConnectionString(engine)
```

**关键设计原则**:

- 最小外部依赖 (仅 Go 标准库)
- 所有模块使用相同的 SystemClient 实现
- Resource 模型在所有服务中是规范的
- common 的破坏性更改会影响所有模块 - 彻底测试

**另请参阅**: [docs/COMMON_MODULE.md](docs/COMMON_MODULE.md)

## Common Frontend

`common-frontend` 模块提供共享的 Vue 3 组件、工具和类型定义,供跨模块的前端复用。

**架构**: 分为两个子模块以避免不必要的依赖:

```
common-frontend/
├── basic/          # 基础 UI 组件 (无地图依赖)
│   └── src/
│       ├── components/  - StorageEngineForm, ImagePreview, ExtractedMetadata
│       ├── utils/       - 格式化器, 类型工具
│       ├── types/       - FieldType, FormatType, ResourceType
│       └── index.js
│
└── map/            # 地图相关组件 (需要 ol 和 @amap/amap-jsapi-loader)
    └── src/
        ├── components/  - MapContainer, GeoJsonPreview, ShapefilePreview, TablePreview
        ├── composables/ - useMapConfig, useGaodeMap, useOpenLayersMap
        └── utils/       - 地理工具, 格式化器
```

**使用模式**:

**对于无地图功能的模块** (System, Transfer):

```javascript
// vite.config.js
resolve: {
  alias: {
    '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
  }
}

// 在组件中
import { StorageEngineForm, ImagePreview } from '@common-ui'
import { formatFileSize, formatDateTime } from '@common-ui'
```

**对于有地图功能的模块** (Manager):

```javascript
// vite.config.js
resolve: {
  alias: {
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
  }
}

// package.json 依赖
{
  "ol": "^9.2.4",
  "@amap/amap-jsapi-loader": "^1.0.1"
}

// 在组件中
import { TablePreview, GeoJsonPreview, ShapefilePreview } from '@common-ui-map'
```

**关键组件**:

- **预览组件**: ShapefilePreview, GeoJsonPreview, TablePreview, ImagePreview
- **表单组件**: StorageEngineForm (PostgreSQL/MinIO/S3 配置)
- **地图组件**: MapContainer, OpenLayersRenderer, GaodeMapRenderer
- **工具**: formatFileSize, formatDateTime, detectFormatByExtension, isGeospatialFormat
- **类型**: FieldType, FormatType, ResourceType (与后端模型对齐)

**优势**:

- ✅ **模块化依赖**: 模块只安装需要的内容
- ✅ **减小打包体积**: 基础模块通过排除地图库节省约 2-3MB
- ✅ **类型安全**: 共享的类型定义确保前后端一致性
- ✅ **DRY 合规**: UI 组件复用而非复制
- ✅ **统一维护**: 所有共享组件集中在一处

**模块使用**:

- **System Frontend**: 使用 `basic` (资源配置的 StorageEngineForm)
- **Manager Frontend**: 使用 `map` (数据预览的 GeoJsonPreview, ShapefilePreview, TablePreview)
- **Meta Frontend**: 使用 `basic` (元数据显示的 ExtractedMetadata)
- **Transfer Frontend**: 使用 `basic` (映射 UI 的字段类型工具)
- **Portal Frontend**: 使用 `basic` (通用 UI 元素)

**另请参阅**: [common-frontend/README.md](common-frontend/README.md), [common-frontend/ARCHITECTURE.md](common-frontend/ARCHITECTURE.md)

## 开发工作流

### 添加新的 API 端点

遵循代码库中使用的分层架构模式:

1. **在 `internal/models/` 中定义数据模型**:

   ```go
   type CreateResourceRequest struct {
       Name           string                 `json:"name" binding:"required"`
       ResourceType   string                 `json:"resource_type" binding:"required"`
       ConnectionInfo map[string]interface{} `json:"connection_info"`
   }
   ```
2. **在 `internal/repository/` 中添加仓库方法**:

   ```go
   func (r *ResourceRepository) Create(resource *models.Resource) error {
       return r.db.Create(resource).Error
   }
   ```
3. **在 `internal/service/` 中实现业务逻辑**:

   ```go
   func (s *ResourceService) CreateResource(req *CreateResourceRequest) (*Resource, error) {
       // 验证、加密、业务规则
       return s.repo.Create(resource)
   }
   ```
4. **在 `internal/api/` 中创建 HTTP 处理器**:

   ```go
   func (h *EngineHandler) Create(c *gin.Context) {
       var req CreateEngineRequest
       if err := c.ShouldBindJSON(&req); err != nil {
           c.JSON(400, gin.H{"error": err.Error()})
           return
       }
       engine, err := h.service.CreateEngine(&req)
       c.JSON(201, engine)
   }
   ```
5. **在 `internal/api/router.go` 中注册路由**:

   ```go
   protected.POST("/engines", engineHandler.Create)
   ```

**示例 PR**: 参见 system 模块引擎管理实现

### 数据库迁移

GORM AutoMigrate 自动处理 schema 更改:

1. **在 `internal/models/` 中修改模型结构**:

   ```go
   type Resource struct {
       ID             uint      `gorm:"primaryKey"`
       Name           string    `gorm:"not null"`
       NewField       string    `gorm:"default:''" json:"new_field"` // 添加新字段
   }
   ```
2. **在 `internal/repository/database.go` 中添加到 AutoMigrate**:

   ```go
   db.AutoMigrate(
       &models.Resource{},
       &models.User{},
       // 在此添加新模型
   )
   ```
3. **重启应用** - 迁移在启动时运行

**对于复杂迁移**:

- 在 `scripts/migrations/` 中创建 SQL 脚本用于数据转换
- 在部署新版本前通过 `make db-migrate` 手动运行
- 在 PR 描述中记录破坏性更改

**Meta 模块特殊性**:
统一的元数据模型 (resource/node/item) 需要协调更新:

- [meta/backend/internal/models/](meta/backend/internal/models/) 中的模型结构
- `meta_dictionary` 表中的字典验证
- 如果结构更改,`attributes` 字段中的 JSON schema 版本
- 可能需要现有元数据的数据迁移脚本

### 添加前端页面

**重要**: 根据功能将页面添加到正确的前端:

- System 功能 (用户、日志、资源) → `system/frontend/`
- Manager 功能 (数据源、目录) → `manager/frontend/`
- Meta 功能 (元数据、血缘) → `meta/frontend/`
- Transfer 功能 (任务、执行) → `transfer/frontend/`

每个前端的步骤:

1. 在 `<module>/frontend/src/views/` 中创建 Vue 组件
2. 在 `<module>/frontend/src/api/` 中添加 API 函数
3. 在 `<module>/frontend/src/router/index.js` 中注册路由
4. 在 `<module>/frontend/src/components/Layout.vue` 中添加导航链接

## 配置

### 环境变量

根目录 `.env` 文件 (从 `.env.example` 复制):

```bash
# 安全性 (生产环境必须更改)
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production

# PostgreSQL - ADDP 系统数据库
POSTGRES_PASSWORD=addp_password
POSTGRES_USER=addp
POSTGRES_DB=addp

# Redis
REDIS_PASSWORD=addp_redis

# MinIO - 系统文件
MINIO_SYSTEM_ROOT_USER=minioadmin
MINIO_SYSTEM_ROOT_PASSWORD=minioadmin

# MinIO - 业务数据 (部署在 business/docker-compose.yml)
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9002
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin

# 服务集成
ENABLE_SERVICE_INTEGRATION=true  # 启用跨服务调用
```

推荐访问**:

- **生产环境**: http://localhost:80 (通过 Nginx 访问 Portal 统一入口)
- **开发环境**: http://localhost:5170 (Portal 独立访问) 或各模块独立端口

**业务库设置**:

```bash
cd business
cp .env.example .env
docker-compose up -d
```

## 测试

### 默认测试账户 (仅用于开发和测试环境)

ADDP 系统在首次启动时可自动创建测试账户,方便开发调试。

**重要**: 此功能默认**禁用**,需要在 `.env` 文件中显式启用,且在生产环境强制禁止使用。

#### 超级管理员账户

- **用户名**: `SuperAdmin`
- **密码**: `20251001#SuperAdmin`
- **用户类型**: `super_admin` (超级管理员)
- **租户**: 无 (跨租户管理)
- **权限**: 管理租户、查看系统级日志、不能直接操作业务数据
- **用途**: 系统级管理、租户管理
- **创建方式**: 应用启动时自动创建 (总是启用)

#### 默认租户管理员账户

- **租户**: 默认租户
- **用户名**: `admin`
- **密码**: `123456`
- **用户类型**: `tenant_admin` (租户管理员)
- **权限**: 管理默认租户下的用户、资源、数据
- **用途**: 日常开发调试、演示使用
- **创建方式**: 需要在 `.env` 中设置 `ENABLE_DEFAULT_TENANT=true` 启用

#### 启用默认租户账户

在 `.env` 文件中添加以下配置:

```bash
# 启用默认租户和租户管理员账户创建
ENABLE_DEFAULT_TENANT=true

# 可选: 自定义默认账户信息
DEFAULT_TENANT_NAME=默认租户
DEFAULT_ADMIN_USERNAME=admin
DEFAULT_ADMIN_PASSWORD=123456
DEFAULT_ADMIN_EMAIL=admin@addp.com
```

#### 安全提示

- ⚠️ **仅用于开发和测试环境** - 这些账户密码较弱,不应在生产环境使用
- ⚠️ **生产环境强制禁用** - 即使设置 `ENABLE_DEFAULT_TENANT=true`,在 `ENV=production` 时也不会创建
- ⚠️ **默认禁用** - 未设置 `ENABLE_DEFAULT_TENANT=true` 时不会创建默认租户账户
- 💡 可通过环境变量自定义账户信息 (用户名、密码、邮箱等)
- 💡 账户创建是幂等的,重复启动不会重复创建

#### 登录测试

使用默认账户登录:

```bash
# 使用超级管理员登录
curl -X POST http://localhost:8180/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "SuperAdmin", "password": "20251001#SuperAdmin"}'

# 使用租户管理员登录
curl -X POST http://localhost:8180/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "123456"}'
```

**初始化位置**: `system/backend/internal/repository/database.go`

### 运行测试

```bash
# 测试所有模块 (从项目根目录)
make test

# 测试特定模块
cd system/backend && go test ./...
cd manager/backend && go test ./...
cd meta/backend && go test ./...

# 带覆盖率的测试
go test -cover ./...

# 测试特定包
go test ./internal/service/...

# 使用详细输出运行测试
go test -v ./...

# 运行特定测试函数
go test -v -run TestFunctionName ./internal/service/
```

**数据持久化**:

**ADDP 系统** (docker-compose.infra.yml):

- PostgreSQL: `postgres_data` 卷 (ADDP 系统元数据)
- Redis: `redis_data` 卷 (缓存和队列)
- MinIO System: `minio_data` 卷 (系统文件)
- Meilisearch: `meilisearch_data` 卷 (搜索索引)

**业务库** (business/docker-compose.yml):

- PostgreSQL: `business_postgres_data` 卷 (用户业务数据)
- MinIO Business: `business_minio_data` 卷 (用户文件)

## API 端点摘要

**公开**:

- `POST /api/auth/login` - 登录
- `POST /api/auth/register` - 注册

**受保护** (需要 JWT):

- `GET /api/users/me` - 当前用户
- `GET /api/users` - 列出用户
- `GET/PUT/DELETE /api/users/:id` - 用户 CRUD
- `GET /api/logs` - 审计日志 (支持 `?user_id=X` 过滤)
- `POST/GET/PUT/DELETE /api/engines` - 引擎 CRUD (支持 `?engine_type=X` 过滤)

## 服务架构详情

### Gateway 服务 (已实现)

**目的**: 所有微服务的统一 API 入口

**关键特性**:

- 使用 Gin 的 HTTP 反向代理
- 通过 URL 前缀匹配路由 (`/api/auth/*` → System, `/api/datasources/*` → Manager 等)
- 用于跨域请求的 CORS 中间件
- 透明的请求/响应转发 (保留头、正文、查询参数)
- `/health` 的健康检查端点

**配置**: 服务 URL 通过环境变量配置 (`SYSTEM_SERVICE_URL`, `MANAGER_SERVICE_URL` 等)

**架构文件**: 详细的请求流和路由规则参阅 `gateway/ARCHITECTURE.md`

### Manager 服务 (已实现)

**目的**: 数据源管理、文件组织和数据预览

**已实现功能**:

- **对象存储预览** (MinIO, S3, OSS):
  - 带层级列表的目录/前缀导航
  - 对象内容预览 (文本、JSON、GeoJSON、图片)
  - 带流支持的 PDF 预览
  - Office 文档预览 (DOCX, PPTX) 通过转换
  - 元数据显示 (大小、最后修改时间、内容类型)
  - 与 Meta 模块集成用于扫描的元数据增强
- **预览插件系统** ([manager/backend/internal/service/object_preview.go:1](manager/backend/internal/service/object_preview.go)):
  - 可扩展的预览处理器 (TextPreview, ImagePreview, PDFPreview, DocxPreview, PptxPreview)
  - 内容类型检测和路由
  - 二进制和文本内容处理
- 连接到 System 模块进行引擎管理
- 连接信息解密以安全访问

**关键文件**:

- 后端预览服务: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- 前端预览组件: [manager/frontend/src/components/previews/](manager/frontend/src/components/previews/)
- 预览插件注册表: [manager/frontend/src/plugins/previews/index.js](manager/frontend/src/plugins/previews/index.js)

**规划功能**:

- 数据库数据预览 (带分页的表记录)
- 视频/音频预览
- 额外的 office 格式 (XLS, CSV)
- 基于权限的访问控制 (用户/组级别)
- 文件上传和管理

**数据库**: PostgreSQL `manager` schema

### Meta 服务 (已实现)

**目的**: 元数据管理和数据血缘

**已实现功能**:

- 从 System 模块同步数据源
- **统一的层级元数据模型**,适用于所有数据源类型:
  - 关系数据库: resource (database) → node (schema) → item (table/view)
  - 对象存储: resource (bucket) → node (prefix) → item (object)
- 元数据扫描:
  - PostgreSQL、MySQL 和其他兼容 JDBC 的数据库
  - 通过 S3 API 的对象存储 (MinIO, S3, OSS)
- 带状态跟踪的 schema 级扫描 (未扫描/扫描中/已扫描)
- 表和字段元数据提取 (名称、类型、大小、注释)
- 对象存储元数据提取 (前缀层次结构、对象类型、大小)
- 使用 cron 表达式的自动和计划扫描 (默认: 每日午夜)
- **事件驱动的自动扫描**: System 注册触发 Meta 扫描 (通过 Redis Pub/Sub)
- 多租户元数据隔离
- **基于 JSON 的灵活属性**,带 schema 版本控制

**自动扫描触发**:
在 System 模块中注册存储引擎时,可以配置自动元数据扫描:

- **Immediate**: 注册后自动开始扫描 (无需手动触发)
- **Daily/Weekly**: 在 Meta 模块中创建计划任务
- **Manual**: 无自动扫描,需要在 Meta 前端手动触发

这通过**事件驱动架构**实现:

1. System 在创建/更新引擎时向 Redis 发布引擎变更事件
2. Meta 订阅这些事件并检查 `ScanConfig.ScheduleType`
3. 如果 `ScheduleType == "immediate"`, Meta 自动创建并入队扫描任务
4. 无循环依赖: System → Redis Pub/Sub → Meta (单向通信)

**扫描工作流**:

1. 从 System 模块 `/api/engines` 同步数据源
2. 选择要扫描的数据源和 schemas/prefixes
3. 层级提取元数据:
   - 数据库: system.engines (database) → meta_node (schemas) → meta_item (tables,字段详情在 JSON 中)
   - 对象存储: system.engines (bucket scope) → meta_node (prefixes) → meta_item (objects,文件元数据)
4. 存储在 PostgreSQL `metadata` schema 中,带租户隔离
5. 跟踪扫描状态、同步版本和最后扫描时间
6. 支持手动触发和计划自动同步

**架构亮点**:

- **节点类型验证**: `meta_dictionary` 表强制有效的父子关系
- **软删除**: 所有实体使用 `deleted_at` 进行安全删除和恢复
- **路径跟踪**: 节点维护 `depth`、`path` (ID 链) 和 `full_name` 以高效查询
- **增量同步**: `sync_version` 和 `last_synced_at` 启用变更检测

**规划功能**:

- 数据血缘跟踪 (source → transformation → target)
- 基于标签的搜索和发现
- 扩展的元数据统计和分析
- `meta_change_log` 用于审计跟踪和回滚

**数据库**: PostgreSQL `metadata` schema (表: meta_node, meta_item, meta_dictionary, meta_change_log)

### Transfer 服务 (已实现)

**目的**: 数据导入/导出和同步

**规划功能**:

- 从外部源导入 (数据库、API、文件)
- 导出到各种目标
- 使用 Cron 表达式的计划任务
- 字段映射和转换
- 带进度跟踪的批处理
- 基于 Asynq 的异步执行任务队列
- 失败传输的重试机制

**数据库**: PostgreSQL `transfer` schema (表: tasks, task_executions, data_mappings)

**任务队列架构**:

- **队列命名**: 使用模块前缀队列以避免与其他模块冲突
  - `transfer:critical` - 高优先级任务 (紧急任务)
  - `transfer:default` - 正常优先级任务 (普通任务)
  - `transfer:low` - 低优先级任务 (低优先级任务)
- **Redis 存储结构**:
  ```
  asynq:transfer:default:pending    → 等待处理的任务
  asynq:transfer:default:active     → 正在处理的任务
  asynq:transfer:default:scheduled  → 延迟执行的任务
  asynq:transfer:default:retry      → 失败重试队列
  asynq:transfer:default:archived   → 永久失败的任务 (死信队列)
  ```
- **多模块隔离**: 每个模块使用自己的队列命名空间
  - Transfer: `transfer:*` 队列
  - Meta (未来): `meta:*` 队列
  - 其他模块: `{module_name}:*` 队列
- **Worker 配置**: 使用 Docker Swarm 运行以实现高可用 (默认 2 个副本)

### Python Workflow Engine (已实现)

**目的**: 基于 Python 的空间计算引擎,提供 GIS 工作流执行能力

**架构**: HTTP Sidecar 模式 (独立的 Flask 微服务)

**关键功能** (已实现):

- **21 个空间算子**,分为 5 类:

  - 几何处理 (8): buffer, centroid, convex_hull, simplify, dissolve, envelope, boundary, representative_point
  - 空间关系 (3): intersection, union, difference
  - 几何属性 (3): area, length, distance
  - 格式转换 (2): to_crs, explode
  - 批处理 (2): clip, spatial_join
  - 高级算子 (3): voronoi, delaunay_triangulation, minimum_rotated_rectangle
- **内存高效的工作流执行**:

  - GeoDataFrame 全程内存传递(避免中间序列化)
  - DAG 拓扑排序(Kahn 算法)
  - 支持 `{"$ref": "taskID"}` 引用上游结果
  - 最终结果写入 PostGIS GEOMETRY 字段
- **双执行模式**:

  - **即时执行**: Develop 模块中直接调用工作流 API (`POST /api/spatial/workflow`)
  - **任务保存**: 保存为 GIS 任务(存储到 `develop.spatial_tasks`),供 Orchestrator 编排
- **引擎注册**:

  - 仅注册引擎本身到 System (`python-workflow.engine.default`)
  - 不注册具体算子 (Transfer 模式)
  - 任务动态发现: `GET /api/spatial/tasks`

**API 端点**:

```
GET  /health                          - 健康检查
GET  /api/spatial/operators           - 获取算子列表(21个)
POST /api/spatial/workflow            - 即时执行工作流
POST /api/spatial/operators/:name/execute - 执行单个算子
GET  /api/spatial/tasks               - 列出保存的任务
POST /api/spatial/tasks               - 创建任务
POST /api/spatial/tasks/:id/execute   - 执行任务
GET  /api/spatial/executions/:id      - 查询执行状态
```

**数据库**: PostgreSQL `develop` schema

- `develop.spatial_tasks` - GIS任务定义(workflow_def JSONB, input_schema JSONB, schedule VARCHAR)
- `develop.spatial_execution_results` - 执行结果(geom GEOMETRY(GEOMETRY, 4326), properties JSONB)

**Orchestrator 集成** (已实现):

- 支持参数模板化: `{{stepID.field.nestedField}}`
- 跨步骤数据传递: SQL → GIS → Transfer
- 示例: `{"poi_location": "{{sql_extract.geojson}}"}`

**技术栈**:

- **语言**: Python 3.11
- **框架**: Flask + CORS
- **库**: GeoPandas 0.14.1, Shapely 2.0.2, NumPy<2.0
- **数据库**: SQLAlchemy 2.0.23 + GeoAlchemy2 0.14.3
- **部署**: Docker + Gunicorn (4 workers, 600s timeout)

**端口**: 8090 (开发和生产)

**关键文件**:

- [python-workflow/workflow_engine.py](python-workflow/workflow_engine.py) - DAG执行引擎
- [python-workflow/operators.py](python-workflow/operators.py) - 21个空间算子
- [python-workflow/api_server.py](python-workflow/api_server.py) - Flask REST API
- [develop/backend/internal/service/spatial_workflow_service.go](develop/backend/internal/service/spatial_workflow_service.go) - Develop集成
- [develop/frontend/src/views/SpatialTasks.vue](develop/frontend/src/views/SpatialTasks.vue) - 任务管理UI
- [orchestrator/backend/internal/service/executor.go](orchestrator/backend/internal/service/executor.go) - 参数模板化实现
- [orchestrator/docs/PARAMETER_TEMPLATING.md](orchestrator/docs/PARAMETER_TEMPLATING.md) - 参数模板化文档

**设计原则**:

- ✅ **仅引擎注册**: 只注册引擎,不注册算子(减少System表膨胀)
- ✅ **内存效率**: GeoDataFrame内存传递,避免反复序列化
- ✅ **PostGIS存储**: 结果存储为GEOMETRY类型(支持空间索引和查询)
- ✅ **独立实现**: 完全独立的空间计算引擎,可复用于多场景

## 服务间通信

**当前模式**: 服务间的 HTTP REST 调用

- 服务通过环境变量相互发现 (例如 `SYSTEM_SERVICE_URL`)
- Manager/Meta/Transfer 可以调用 System API 进行用户验证
- Manager 在添加新数据源时通知 Meta
- Transfer 查询 Manager 获取数据源连接信息

**认证传播**: JWT token 通过 `Authorization` 头传递

**错误处理**: 服务返回标准 HTTP 状态码;调用服务处理重试
