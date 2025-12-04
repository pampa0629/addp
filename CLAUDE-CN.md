# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 在本代码仓库中工作时提供指导。

## 仓库结构

**ADDP (All Domain Data Platform / 全域数据平台)** 是一个企业级数据平台，采用微服务架构。每个服务都有独立的目录：

- **common/** - 共享库模块：通用客户端代码、模型、配置加载器和跨服务使用的工具
- **system/** - 核心系统模块：用户认证、日志记录、资源管理 - **已实现** (PostgreSQL system schema)
- **gateway/** - API 网关：处理外部请求并路由到内部服务 - **已实现** (反向代理)
- **manager/** - 数据管理：数据源连接、上传目录组织、数据预览 - **部分实现** (Go 后端结构已创建)
- **meta/** - 元数据服务：数据元数据解析/存储/查询，支持 cron 调度的 schema 扫描 - **已实现** (PostgreSQL metadata schema)
- **transfer/** - 数据传输：数据导入/导出/同步 - *计划中*

所有服务遵循相同的架构模式，使用共享基础设施 (PostgreSQL, Redis, MinIO)。通用代码通过 `common` 模块共享以避免重复。

## 快速开始

### 开发环境 (仅 System 模块)

```bash
# 从 system/ 目录
cd system

# 后端开发 (需要 PostgreSQL)
cd backend && go run cmd/server/main.go

# 前端开发
cd frontend && npm install && npm run dev

# Docker 部署 (仅 System)
make docker-up
```

### 完整平台部署

```bash
# 从项目根目录
make init           # 初始化配置文件
make up             # 仅启动 System 模块
make up-full        # 启动所有服务 (Gateway + 所有模块 + 基础设施)
make status         # 检查服务状态
make logs           # 查看所有日志
```

**详细的 System 模块文档，请参阅 `system/CLAUDE.md`。**

### 开发模式启动 (推荐)

**重要**: 服务必须按正确顺序启动以确保依赖关系得到满足。使用自动化启动脚本避免竞态条件。

```bash
# 从项目根目录

# 1. 按正确顺序启动所有服务 (推荐)
make dev-start
# 或直接运行:
./scripts/dev/start.sh

# 2. 检查服务健康状态
make dev-health

# 3. 停止所有服务
make dev-stop
# 或直接运行:
./scripts/dev/stop.sh
```

**启动顺序**:
```
基础设施 (PostgreSQL, Redis, MinIO)
  ↓
System Backend (认证, 配置中心)
  ↓
Manager Backend + Meta Backend (并行)
  ↓
Gateway (API 路由)
  ↓
前端服务 (可选)
```

**主要特性**:
- ✅ 自动健康检查等待 - 确保每个服务就绪后再启动下一个
- ✅ 进度指示器 - 彩色输出显示启动状态
- ✅ PID 跟踪 - 存储进程 ID 以便优雅关闭
- ✅ 超时处理 - 如果服务在 60 秒内未启动则快速失败
- ✅ 日志文件 - 将输出重定向到 `logs/` 目录便于调试

**启动脚本详情**:
- 位置: `scripts/dev/start.sh`
- 等待 `/health` 端点返回 200 后继续
- 创建 `.dev-pids/` 目录跟踪进程 ID
- 日志存储在 `logs/` 目录 (例如 `logs/system-backend.log`)
- 可选的前端启动 (提示用户)

**服务 URL** (成功启动后):
- Portal: http://localhost:5170
- Gateway: http://localhost:8000
- System Backend: http://localhost:8080
- Manager Backend: http://localhost:8081
- Meta Backend: http://localhost:8082

**故障排除**:
- 如果启动失败，检查 `logs/` 目录中的日志
- 运行 `make dev-health` 验证哪些服务正在运行
- 详细依赖文档请参阅 [docs/STARTUP_ORDER.md](docs/STARTUP_ORDER.md)

## 技术栈

### 后端
- **语言**: Go 1.23+
- **HTTP 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15 (所有模块使用 schema 隔离: system, manager, metadata, transfer)
- **缓存/队列**: Redis 7
- **对象存储**: MinIO (S3 兼容)
- **任务队列**: Asynq (基于 Redis，用于 Transfer 模块), Cron (用于 Meta 模块调度)

### 前端
- **框架**: Vue 3 + Composition API
- **构建工具**: Vite
- **UI 库**: Element Plus
- **状态管理**: Pinia
- **路由**: Vue Router
- **HTTP 客户端**: Axios (带认证拦截器)

### 基础设施
- **容器化**: Docker + Docker Compose
- **反向代理**: Nginx (生产环境), Gateway 服务 (API 路由)
- **数据库 Schema 隔离**: PostgreSQL schemas (manager, metadata, transfer)
- **数据分离**: 系统基础设施 (ADDP 元数据) + 业务基础设施 (用户数据) 独立部署

### 基础设施架构

ADDP 采用**系统与业务数据分离**的架构设计：

```
┌─────────────────────────────────────────────────────────────┐
│  ADDP 系统基础设施 (docker-compose.yml)                    │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ postgres     │  │ redis        │  │ minio-system │     │
│  │ (系统元数据) │  │ (缓存/队列)  │  │ (系统文件)   │     │
│  │ 端口: 5432   │  │ 端口: 6379   │  │ 端口: 9000-1 │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  业务基础设施 (business/docker-compose.yml)                 │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐                        │
│  │ postgres     │  │ minio        │                        │
│  │ (业务数据库) │  │ (业务文件)   │                        │
│  │ 端口: 5433   │  │ 端口: 9002-3 │                        │
│  └──────────────┘  └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

**系统基础设施**（主 docker-compose.yml）:
- `postgres`: 存储 ADDP 系统元数据（用户、资源配置、元数据索引、任务定义等）
- `redis`: 缓存和任务队列
- `minio`: 存储系统文件（用户头像、系统配置等）

**业务基础设施**（business/docker-compose.yml，独立部署）:
- `postgres`: 存储用户通过 ADDP 管理的实际业务数据（用户上传的 PostgreSQL 数据等）
- `minio`: 存储用户上传的业务文件（Shapefile、GeoJSON、图片、视频等）

**分离优势**:
- ✅ 数据隔离: 系统数据与业务数据物理分离
- ✅ 独立扩展: 业务数据量增长时可单独扩展
- ✅ 安全性: 业务数据库可配置更严格的访问控制
- ✅ 可替换性: 业务基础设施可替换为云服务（RDS、OSS）

详见 [business/README.md](business/README.md)

## 关键架构模式

### 分层后端架构 (在 system/backend/ 中)

Go 后端遵循清晰的分层方法：

```
cmd/server/main.go          → 应用入口点
internal/api/               → HTTP 处理器 + 路由
internal/service/           → 业务逻辑层
internal/repository/        → 数据访问层 (GORM)
internal/models/            → 数据库模型 + DTOs
internal/middleware/        → 认证、日志中间件
pkg/utils/                  → 共享工具 (JWT, crypto)
```

**数据流向**: API Handler → Service → Repository → Database

### 前端架构 (Portal + 微服务模式)

**统一入口 + 独立模块前端**:

平台使用基于 **Portal 的架构**，提供统一入口点：

```
portal/frontend/           → 统一 Portal 入口 (开发端口 5170 / 生产端口 8000)
├── src/
│   ├── views/
│   │   ├── Portal.vue    → 主 Portal 页面，包含模块卡片
│   │   └── Login.vue     → 集中式登录
│   ├── api/auth.js       → 通过 System backend 进行认证
│   └── router/           → Portal 路由
│
│   Portal 通过 iframe 嵌入模块前端:
│   - 左侧边栏: 所有模块的统一导航
│   - 主区域: iframe 动态加载模块前端

system/frontend/           → System 模块 (开发端口 5173 / 生产端口 8090)
├── 独立或嵌入 portal
├── 功能: 用户、日志、资源

manager/frontend/          → Manager 模块 (开发端口 5174 / 生产端口 8091)
├── 独立或嵌入 portal
├── 功能: 数据源、目录、预览

meta/frontend/             → Meta 模块 (开发端口 5175 / 生产端口 8092) - 计划中
transfer/frontend/         → Transfer 模块 (开发端口 5176 / 生产端口 8093) - 计划中
```

**两种访问模式**:

1. **统一 Portal 模式** (推荐用户使用):
   - 单一入口: http://localhost:5170 (开发) 或 http://localhost:8000 (生产)
   - 集成导航，包含所有模块
   - 模块前端通过 iframe 加载到 portal 中
   - 一次登录，访问所有模块

2. **独立模块模式** (用于独立部署):
   - 直接访问每个模块前端
   - System: http://localhost:5173, Manager: http://localhost:5174
   - 每个模块有自己的登录
   - 适合单独部署单个模块

**前端关键原则**:
- Portal 提供统一用户体验和一致的导航
- 模块前端保持独立，可单独部署
- 所有前端共享 JWT 认证模式 (token 存储在 localStorage)
- Portal 和模块可以独立认证
- 生产环境中，所有请求通过 Gateway (8000) 路由

### 认证流程

1. 用户向 `POST /api/auth/login` 提交凭据
2. 后端使用 bcrypt 验证，返回 JWT (HS256, 使用 `JWT_SECRET` 签名)
3. 前端将 token 存储在 localStorage (`auth.js` Pinia store)
4. Axios 拦截器 (`api/client.js`) 为所有请求添加 `Authorization: Bearer <token>` 头
5. 后端 `AuthMiddleware` 验证 JWT 并将用户上下文注入 Gin context
6. 受保护路由通过 `c.Get("user_id")` 访问用户

### 数据库架构

所有模块使用 **PostgreSQL 15** 并通过 schema 隔离实现数据分离。

**System 模块 (system schema)**:
- **users** - 用户账户，使用 bcrypt 哈希密码
- **tenants** - 租户信息，用于多租户
- **audit_logs** - 自动记录所有非 GET 操作 (通过 `LoggerMiddleware`)
- **resources** - 灵活的资源配置，使用 JSON connection_info 字段 (加密存储)

**Manager 模块 (manager schema)**:
- **data_sources** - 数据源连接和配置
- **directories** - 文件组织和层级结构
- **permissions** - 数据源和目录的访问控制

**Meta 模块 (metadata schema)** - 已实现:
- **meta_node** - 层级节点 (schemas, prefixes, collections)，支持递归结构，通过 `res_id` 引用 `system.resources`
- **meta_item** - 叶子项 (tables, objects, files)，使用 JSON attributes
- **meta_dictionary** - 节点类型和子节点规则定义，用于验证
- **meta_change_log** - 变更跟踪，用于审计和增量同步 (计划中)

注意: Meta 模块使用**统一层级模型** (resource → node → item) 支持关系数据库和对象存储，替代了旧的类型特定表 (schemas, tables, fields)

**Transfer 模块 (transfer schema)** - 计划中:
- **tasks** - 传输任务定义
- **task_executions** - 任务执行历史
- **data_mappings** - 字段映射配置

GORM AutoMigrate 在启动时自动处理 schema 更新。PostgreSQL schemas 通过 `scripts/init-db.sql` 初始化。

### 配置中心模式

**System 作为唯一真实来源**:

平台实现了集中式配置管理模式，**System 模块作为所有其他模块的配置中心**。

**架构**:
```
┌────────────────────────────────────────────────────┐
│   System 模块 (配置中心)                           │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /internal/config API                   │    │
│   │  返回: JWT_SECRET, DB 连接信息,          │    │
│   │        ENCRYPTION_KEY                   │    │
│   └─────────────────────────────────────────┘    │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /api/resources (业务 DB 配置)           │    │
│   │  管理所有数据源配置                      │    │
│   │  (加密存储)                             │    │
│   └─────────────────────────────────────────┘    │
└────────────────┬───────────────────────────────────┘
                 │
      ┌──────────┼──────────┐
      ▼          ▼          ▼
  ┌────────┐ ┌────────┐ ┌─────────┐
  │ Manager│ │  Meta  │ │Transfer │
  │        │ │        │ │         │
  │ 启动时 │ │ 启动时 │ │ 启动时  │
  │ ↓      │ │ ↓      │ │ ↓       │
  │ 获取   │ │ 获取   │ │ 获取    │
  │ 配置   │ │ 配置   │ │ 配置    │
  └────────┘ └────────┘ └─────────┘
```

**集中管理的内容**:
1. **认证**: `JWT_SECRET` - 确保所有服务使用相同的 JWT 签名密钥
2. **系统数据库**: PostgreSQL 连接信息 - 系统数据的单一来源
3. **业务数据库**: 在 System 的 `resources` 表中管理的资源 - 所有数据源配置
4. **加密密钥**: `ENCRYPTION_KEY` - 跨服务一致的加密

**配置加载流程**:
```
模块启动
   ↓
尝试从 System 获取配置 (/internal/config)
   ↓
   ├─ 成功 ✅
   │  └─ 使用 System 配置 (JWT_SECRET, DB 连接)
   │
   └─ 失败 ⚠️
      └─ 回退到本地 .env 配置
```

**优势**:
- ✅ **唯一真实来源**: 更改数据库密码一次，重启服务即可应用
- ✅ **安全性**: 敏感配置集中管理并加密
- ✅ **灵活性**: 支持集成和独立部署模式
- ✅ **可维护性**: 减少配置重复，更易审计

**SystemClient 使用**:

所有模块使用 `SystemClient` 从 System 获取业务数据库配置：

```go
import (
    commonClient "github.com/addp/common/client"
    commonModels "github.com/addp/common/models"
)

// 使用 JWT token 创建客户端
client := commonClient.NewSystemClient(systemURL, jwtToken)

// 列出所有数据源
resources, err := client.ListResources("postgresql")

// 获取特定数据源
resource, err := client.GetResource(resourceID)

// 构建连接字符串 (密码自动解密)
connStr, err := commonModels.BuildConnectionString(resource)
```

**模块 .env 文件**:

每个模块只需配置模块特定设置：

```bash
# Manager/Meta/Transfer .env
PORT=8081                          # 模块特定端口
DB_SCHEMA=manager                  # 模块特定 schema
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true    # 启用配置中心

# 共享配置 (JWT_SECRET, DB 连接) 从 System 获取
# 回退配置已注释 (仅在禁用集成时使用)
```

**另请参阅**: `docs/CONFIG_CENTER.md` 获取详细的配置中心使用指南。

## Common 模块

`common` 模块提供共享代码，避免 Manager、Meta 和 Transfer 模块之间的重复。

**内容**:
- [client/system.go](common/client/system.go) - 与 System 模块通信的 SystemClient
- [models/resource.go](common/models/resource.go) - 共享 Resource 模型和 BuildConnectionString 工具
- [config/loader.go](common/config/loader.go) - 带回退的集中式配置加载

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

// 使用 SystemClient 获取资源
client := commonClient.NewSystemClient(systemURL, jwtToken)
resources, err := client.ListResources("postgresql")
resource, err := client.GetResource(resourceID)

// 构建连接字符串 (自动解密密码)
connStr, err := commonModels.BuildConnectionString(resource)
```

**关键设计原则**:
- 最小外部依赖 (仅 Go 标准库)
- 所有模块使用相同的 SystemClient 实现
- Resource 模型在所有服务中是规范的
- 对 common 的破坏性更改影响所有模块 - 需要彻底测试

**另请参阅**: [docs/COMMON_MODULE.md](docs/COMMON_MODULE.md)

## 开发工作流

### 添加新 API 端点

遵循整个代码库中使用的分层架构模式：

1. **在 `internal/models/` 中定义数据模型**：
   ```go
   type CreateResourceRequest struct {
       Name           string                 `json:"name" binding:"required"`
       ResourceType   string                 `json:"resource_type" binding:"required"`
       ConnectionInfo map[string]interface{} `json:"connection_info"`
   }
   ```

2. **在 `internal/repository/` 中添加仓库方法**：
   ```go
   func (r *ResourceRepository) Create(resource *models.Resource) error {
       return r.db.Create(resource).Error
   }
   ```

3. **在 `internal/service/` 中实现业务逻辑**：
   ```go
   func (s *ResourceService) CreateResource(req *CreateResourceRequest) (*Resource, error) {
       // 验证、加密、业务规则
       return s.repo.Create(resource)
   }
   ```

4. **在 `internal/api/` 中创建 HTTP 处理器**：
   ```go
   func (h *ResourceHandler) Create(c *gin.Context) {
       var req CreateResourceRequest
       if err := c.ShouldBindJSON(&req); err != nil {
           c.JSON(400, gin.H{"error": err.Error()})
           return
       }
       resource, err := h.service.CreateResource(&req)
       c.JSON(201, resource)
   }
   ```

5. **在 `internal/api/router.go` 中注册路由**：
   ```go
   protected.POST("/resources", resourceHandler.Create)
   ```

**示例 PR**: 参见 system 模块资源管理实现

### 数据库迁移

GORM AutoMigrate 自动处理 schema 变更：

1. **在 `internal/models/` 中修改模型结构**：
   ```go
   type Resource struct {
       ID             uint      `gorm:"primaryKey"`
       Name           string    `gorm:"not null"`
       NewField       string    `gorm:"default:''" json:"new_field"` // 添加新字段
   }
   ```

2. **添加到 `internal/repository/database.go` 的 AutoMigrate**：
   ```go
   db.AutoMigrate(
       &models.Resource{},
       &models.User{},
       // 在此添加新模型
   )
   ```

3. **重启应用** - 迁移在启动时运行

**复杂迁移**:
- 在 `scripts/migrations/` 中创建 SQL 脚本用于数据转换
- 在部署新版本前通过 `make db-migrate` 手动运行
- 在 PR 描述中记录破坏性变更

**Meta 模块特性**:
统一元数据模型 (resource/node/item) 需要协调更新:
- 模型结构在 [meta/backend/internal/models/](meta/backend/internal/models/)
- `meta_dictionary` 表中的字典验证
- 如果结构变更，`attributes` 字段中的 JSON schema 版本
- 可能需要为现有元数据编写数据迁移脚本

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
# 安全配置 (生产环境必须修改)
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

# MinIO - 业务数据 (在 business/docker-compose.yml 中部署)
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9002
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin

# 服务集成
ENABLE_SERVICE_INTEGRATION=true  # 启用跨服务调用
```

### 端口分配

**ADDP 系统服务**:

| 服务 | 开发端口 | Docker 端口 | 描述 |
|------|----------|-------------|------|
| **Portal Frontend** | **5170** | **8000** | **统一入口点** |
| Gateway | 8000 | 8000 | API 网关 (后端路由) |
| System Backend | 8080 | 8080 | 认证、用户、日志 |
| System Frontend | 5173 | 8090 | 独立访问 |
| Manager Backend | 8081 | 8081 | 数据源、文件 |
| Manager Frontend | 5174 | 8091 | 独立访问 |
| Meta Backend | 8082 | 8082 | 元数据、血缘 |
| Meta Frontend | 5175 | 8092 | 独立访问 |
| Transfer Backend | 8083 | 8083 | 导入/导出任务 |
| Transfer Frontend | 5176 | 8093 | 独立访问 |
| PostgreSQL (System) | 5432 | 5432 | ADDP 系统元数据 |
| Redis | 6379 | 6379 | 缓存 & 队列 |
| MinIO System API | 9000 | 9000 | 系统文件存储 |
| MinIO System Console | 9001 | 9001 | 系统 MinIO Web UI |

**业务基础设施服务** (通过 `business/docker-compose.yml` 部署):

| 服务 | Docker 端口 | 描述 |
|------|-------------|------|
| PostgreSQL (Business) | 5433 | 用户业务数据存储 |
| MinIO Business API | 9002 | 用户文件存储 |
| MinIO Business Console | 9003 | 业务 MinIO Web UI |

**推荐访问**: 使用 **http://localhost:5170** 的 Portal 获得统一体验

**业务基础设施设置**:
```bash
cd business
cp .env.example .env
docker-compose up -d
```

## 测试

### 运行测试

```bash
# 测试所有模块 (从项目根目录)
make test

# 测试特定模块
cd system/backend && go test ./...
cd manager/backend && go test ./...
cd meta/backend && go test ./...

# 带覆盖率测试
go test -cover ./...

# 测试特定包
go test ./internal/service/...

# 带详细输出运行测试
go test -v ./...

# 运行特定测试函数
go test -v -run TestFunctionName ./internal/service/
```

### 编写测试

Go 测试应该是表驱动的，放在 `_test.go` 文件中:

```go
// 示例: internal/service/resource_service_test.go
func TestResourceService_Create(t *testing.T) {
    tests := []struct {
        name    string
        input   *models.Resource
        wantErr bool
    }{
        {"valid resource", &models.Resource{...}, false},
        {"invalid type", &models.Resource{...}, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 测试实现
        })
    }
}
```

### 前端测试

前端测试尚未实现。添加 Vue 组件时，在 PR 描述中记录手动测试场景。

## Docker 部署

### 业务基础设施 (首先部署)

业务基础设施需要**先于 ADDP 系统启动**，因为 Manager 和 Meta 模块会连接到业务存储：

```bash
# 1. 首先部署业务基础设施
cd business
cp .env.example .env
# 编辑 .env 配置密码
docker-compose up -d

# 2. 验证业务服务正在运行
docker-compose ps
docker-compose logs -f
```

### 仅 System 模块 (默认)

```bash
# 从项目根目录
make up              # 启动 System backend + frontend
make logs-system     # 查看日志
make down           # 停止服务
```

### 完整平台

```bash
# 从项目根目录 (确保业务基础设施已运行)
make up-full        # 使用 --profile full 启动所有服务
make status         # 检查所有服务状态
make logs           # 查看所有日志
make down           # 停止所有服务

# 单独服务日志
make logs-system
make logs-manager
make logs-gateway
```

### 更改后重建

```bash
make docker-build       # 仅重建 System
make docker-build-all   # 重建所有服务
docker-compose up -d    # 重启
```

**数据持久化**:

**ADDP 系统** (docker-compose.yml):
- PostgreSQL: `postgres_data` 卷 (ADDP 系统元数据)
- Redis: `redis_data` 卷 (缓存和队列)
- MinIO System: `minio_system_data` 卷 (系统文件)

**业务基础设施** (business/docker-compose.yml):
- PostgreSQL: `business_postgres_data` 卷 (用户业务数据)
- MinIO Business: `business_minio_data` 卷 (用户文件)

## API 端点摘要

**公开**:
- `POST /api/auth/login` - 登录
- `POST /api/auth/register` - 注册

**受保护** (需要 JWT):
- `GET /api/users/me` - 当前用户
- `GET /api/users` - 用户列表
- `GET/PUT/DELETE /api/users/:id` - 用户 CRUD
- `GET /api/logs` - 审计日志 (支持 `?user_id=X` 过滤)
- `POST/GET/PUT/DELETE /api/resources` - 资源 CRUD (支持 `?resource_type=X` 过滤)

## 服务架构详情

### Gateway 服务 (已实现)
**目的**: 所有微服务的统一 API 入口

**关键特性**:
- 使用 Gin 的 HTTP 反向代理
- 按 URL 前缀匹配路由 (`/api/auth/*` → System, `/api/datasources/*` → Manager 等)
- 跨域请求的 CORS 中间件
- 透明的请求/响应转发 (保留 headers、body、query params)
- `/health` 健康检查端点

**配置**: 通过环境变量配置服务 URL (`SYSTEM_SERVICE_URL`, `MANAGER_SERVICE_URL` 等)

**架构文件**: 参见 `gateway/ARCHITECTURE.md` 了解详细请求流和路由规则

### Manager 服务 (已实现)
**目的**: 数据源管理、文件组织和数据预览

**已实现功能**:
- **对象存储预览** (MinIO, S3, OSS):
  - 带层级列表的目录/前缀导航
  - 对象内容预览 (文本、JSON、GeoJSON、图片)
  - PDF 预览支持流式传输
  - Office 文档预览 (DOCX, PPTX) 通过转换
  - 元数据显示 (大小、最后修改时间、内容类型)
  - 与 Meta 模块集成以获取扫描的元数据增强
- **预览插件系统** ([manager/backend/internal/service/object_preview.go:1](manager/backend/internal/service/object_preview.go)):
  - 可扩展的预览处理器 (TextPreview, ImagePreview, PDFPreview, DocxPreview, PptxPreview)
  - 内容类型检测和路由
  - 二进制和文本内容处理
- 连接到 System 模块进行资源管理
- 连接信息解密以安全访问

**关键文件**:
- 后端预览服务: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- 前端预览组件: [manager/frontend/src/components/previews/](manager/frontend/src/components/previews/)
- 预览插件注册: [manager/frontend/src/plugins/previews/index.js](manager/frontend/src/plugins/previews/index.js)

**计划功能**:
- 数据库数据预览 (带分页的表记录)
- 视频/音频预览
- 其他 office 格式 (XLS, CSV)
- 基于权限的访问控制 (用户/组级别)
- 文件上传和管理

**数据库**: PostgreSQL `manager` schema

### Meta 服务 (已实现)
**目的**: 元数据管理和数据血缘

**已实现功能**:
- 从 System 模块同步数据源
- **统一层级元数据模型**，适用于所有数据源类型:
  - 关系数据库: resource (数据库) → node (schema) → item (表/视图)
  - 对象存储: resource (bucket) → node (prefix) → item (object)
- 元数据扫描:
  - PostgreSQL、MySQL 和其他 JDBC 兼容数据库
  - 对象存储 (MinIO, S3, OSS) 通过 S3 API
- Schema 级别扫描，带状态跟踪 (未扫描/扫描中/已扫描)
- 表和字段元数据提取 (名称、类型、大小、注释)
- 对象存储元数据提取 (前缀层级、对象类型、大小)
- 使用 cron 表达式的自动和计划扫描 (默认: 每天午夜)
- 多租户元数据隔离
- **基于 JSON 的灵活属性**，带 schema 版本控制

**扫描工作流**:
1. 从 System 模块 `/api/resources` 同步数据源
2. 选择要扫描的数据源和 schemas/prefixes
3. 层级提取元数据:
   - 数据库: system.resources (数据库) → meta_node (schemas) → meta_item (表，字段详情在 JSON 中)
   - 对象存储: system.resources (bucket 范围) → meta_node (prefixes) → meta_item (对象，文件元数据)
4. 存储在 PostgreSQL `metadata` schema 中，带租户隔离
5. 跟踪扫描状态、同步版本和最后扫描时间
6. 支持手动触发和计划自动同步

**架构亮点**:
- **节点类型验证**: `meta_dictionary` 表强制有效的父子关系
- **软删除**: 所有实体使用 `deleted_at` 进行安全删除和恢复
- **路径跟踪**: 节点维护 `depth`、`path` (ID 链) 和 `full_name` 以便高效查询
- **增量同步**: `sync_version` 和 `last_synced_at` 启用变更检测

**计划功能**:
- 数据血缘跟踪 (源 → 转换 → 目标)
- 基于标签的搜索和发现
- 扩展的元数据统计和分析
- `meta_change_log` 用于审计跟踪和回滚

**数据库**: PostgreSQL `metadata` schema (表: meta_node, meta_item, meta_dictionary, meta_change_log)

### Transfer 服务 (计划中)
**目的**: 数据导入/导出和同步

**计划功能**:
- 从外部源导入 (数据库、API、文件)
- 导出到各种目标
- 使用 Cron 表达式的计划任务
- 字段映射和转换
- 带进度跟踪的批处理
- 基于 Asynq 的任务队列用于异步执行
- 失败传输的重试机制

**数据库**: PostgreSQL `transfer` schema (表: tasks, task_executions, data_mappings)

## 服务间通信

**当前模式**: 服务间的 HTTP REST 调用
- 服务通过环境变量发现彼此 (例如 `SYSTEM_SERVICE_URL`)
- Manager/Meta/Transfer 可以调用 System API 进行用户验证
- Manager 在添加新数据源时通知 Meta
- Transfer 向 Manager 查询数据源连接信息

**认证传播**: JWT tokens 通过 `Authorization` headers 传递

**错误处理**: 服务返回标准 HTTP 状态码；调用服务处理重试

## 代码质量原则

### DRY (不要重复自己)

**核心原则**: 避免跨模块的代码重复。将通用功能提取到 `common/` 模块。

**为什么重要**:
- ✅ 单一维护点 - 修复一次 bug，所有模块受益
- ✅ 一致性 - 所有模块使用相同的实现
- ✅ 降低错误风险 - 无需记住更新多个位置
- ✅ 更容易重构 - 在一个地方更改逻辑

**`common/` 中共享代码示例**:
- **`common/config/LoadEnv(levelsUp int)`** - 从项目根目录加载 .env 文件
  ```go
  // 在每个模块的 main.go 中
  commonConfig.LoadEnv(4)  // system/backend/cmd/server (向上 4 级)
  commonConfig.LoadEnv(3)  // gateway/cmd/gateway (向上 3 级)
  ```
- **`common/client/SystemClient`** - 与 System 模块通信
- **`common/models/Resource`** - 共享资源模型
- **`common/config/LoadSharedConfig()`** - 从 System 获取配置

**何时提取到 common/**:
1. 代码出现在 2+ 个模块中，变化很小
2. 逻辑与模块无关 (不特定于一个服务的业务领域)
3. 函数可以参数化以处理差异 (例如路径深度)

**何时不提取**:
- 模块特定的业务逻辑
- 可能在模块间分化的代码
- 没有重用潜力的单次使用函数

**实现模式**:
```go
// 步骤 1: 添加到 common/config/loader.go 或创建新文件
func SharedFunction(param int) {
    // 实现
}

// 步骤 2: 在每个模块中导入
import commonConfig "github.com/addp/common/config"

// 步骤 3: 使用它
commonConfig.SharedFunction(value)
```

**示例 PR**: 参见 .env 加载重构 - 从 4 个重复的 main.go 文件中提取 `godotenv` 逻辑到 `common/config/LoadEnv()`

## 新服务开发指南

实现或扩展服务时:

1. **遵循 System 模块模式**:
   ```
   service/backend/
   ├── cmd/server/main.go       # 入口点
   ├── internal/
   │   ├── api/                 # HTTP 处理器
   │   ├── service/             # 业务逻辑
   │   ├── repository/          # 数据访问
   │   ├── models/              # 数据结构
   │   ├── middleware/          # 认证、日志
   │   └── config/              # 配置
   └── pkg/utils/               # 共享工具
   ```

2. **数据库约定**:
   - 使用 PostgreSQL schema 隔离 (System 使用 SQLite 除外)
   - 使用 GORM 作为 ORM，带 AutoMigrate
   - 将 schemas 添加到 `scripts/init-db.sql`
   - 使用 `updated_at` 触发器进行时间戳跟踪

3. **配置**:
   - 通过 `internal/config/config.go` 从环境变量读取
   - 支持开发和 Docker 部署模式
   - 为缺失的环境变量设置默认值

4. **认证**:
   - 重用 System 模块的 JWT 验证逻辑
   - 从 System 导入认证中间件或创建相同的
   - 从 JWT claims 提取 user_id 并传递到服务层

5. **Docker 集成**:
   - 在服务根目录创建 Dockerfile
   - 使用 `profile: full` 将服务添加到 `docker-compose.yml`
   - 使用健康检查进行依赖管理
   - 连接到 `addp-network` 进行服务间通信

6. **前端集成**:
   - 创建独立的 `<module>/frontend/` 目录
   - 从 `system/frontend/` 复制结构 (Vue 3 + Pinia + Element Plus)
   - 创建指向模块后端的 `api/client.js`
   - 创建指向 System backend (8080) 的 `api/auth.js` 用于认证
   - 从 System 模块复制 auth store 模式 (独立副本，非共享)
   - 在 `vite.config.js` 中设置唯一开发端口 (System: 5173, Manager: 5174 等)
   - 配置路由基础路径 (例如 Manager 模块的 `/manager/`)
   - 为生产部署创建 Dockerfile 和 nginx.conf
   - 使用唯一端口和 `profile: full` 添加到 docker-compose.yml

## 前端开发工作流

### 快速开始: Portal + 所有模块

```bash
# 终端 1: 启动 Portal (统一入口)
cd portal/frontend
npm install
npm run dev
# 访问: http://localhost:5170

# 终端 2: 启动 System frontend
cd system/frontend
npm install
npm run dev

# 终端 3: 启动 Manager frontend
cd manager/frontend
npm install
npm run dev

# 现在访问 http://localhost:5170 获得统一体验
# 所有模块可通过单一 portal 界面访问
```

### 运行单独前端 (独立模式)

```bash
# System frontend (端口 5173)
cd system/frontend
npm run dev
# 访问: http://localhost:5173

# Manager frontend (端口 5174)
cd manager/frontend
npm run dev
# 访问: http://localhost:5174

# Portal (端口 5170)
cd portal/frontend
npm run dev
# 访问: http://localhost:5170
```

### 开发中的前后端连接

**开发模式** (直连后端):
- System frontend → System backend (localhost:8080)
- Manager frontend → Manager backend (localhost:8081)
- Auth requests → System backend (localhost:8080)

**生产模式** (通过 Gateway):
- 所有前端请求 → Gateway (localhost:8000)
- Gateway 路由到适当的后端

### 创建新模块前端

实现新模块 (例如 Meta) 时，按以下步骤:

1. **复制前端结构**:
   ```bash
   cp -r system/frontend meta/frontend
   cd meta/frontend
   ```

2. **更新配置**:
   - `package.json`: 将 name 改为 `meta-frontend`
   - `vite.config.js`: 将端口改为唯一数字 (例如 5175)
   - `index.html`: 更新标题
   - `src/router/index.js`: 设置基础路径为 `/meta/`
   - `src/api/client.js`: 将 baseURL 指向 meta backend (8082)
   - 保持 `src/api/auth.js` 指向 System backend (8080)

3. **更新 views 和 components** 以匹配模块功能

4. **添加 Dockerfile 和 nginx.conf** (从 manager/frontend 复制作为模板)

5. **添加到 docker-compose.yml**:
   ```yaml
   meta-frontend:
     build:
       context: ./meta/frontend
     ports:
       - "8092:80"
     profiles:
       - full
   ```

## 常用 Make 命令 (项目根目录)

```bash
# 初始化
make init                # 创建配置文件和目录
make install-deps        # 安装 Go 和 npm 依赖

# 开发
make dev-start           # 按正确顺序启动所有服务 (推荐)
make dev-stop            # 停止所有开发服务
make dev-health          # 检查所有服务健康状态
make dev-system          # 在开发模式运行 System
make dev-manager         # 运行 Manager backend
make dev-gateway         # 运行 Gateway 服务

# Docker 操作
make up                  # 仅启动 System 模块
make up-full             # 启动所有服务 (完整平台)
make up-infra            # 仅启动 PostgreSQL、Redis、MinIO
make down                # 停止所有服务
make restart             # 重启 System 模块
make restart-full        # 重启所有服务

# 构建
make build               # 构建所有 artifacts 到 dist/
make docker-build        # 构建 System Docker 镜像
make docker-build-all    # 构建所有服务 Docker 镜像

# 监控
make status              # 显示所有服务状态和 URLs
make logs                # 查看所有服务日志
make logs-system         # 仅查看 System 日志
make logs-manager        # 查看 Manager 日志
make health              # 检查所有服务健康状态

# 数据库
make db-shell            # 连接到 PostgreSQL
make db-migrate          # 运行数据库迁移 (init-db.sql)
make redis-cli           # 连接到 Redis
make minio-setup         # 初始化 MinIO buckets
make backup              # 备份 PostgreSQL 数据库
make restore FILE=...    # 从备份恢复数据库

# 测试 & 质量
make test                # 运行所有测试
make test-system         # 运行 System 模块测试
make lint                # 运行代码检查
make fmt                 # 格式化 Go 代码

# 清理
make clean               # 删除构建 artifacts
make clean-all           # 删除所有数据和卷 (破坏性)
```

## 重要文件位置

### 配置
- [`.env`](.env) - 根环境变量 (共享配置)
- [`.env.example`](.env.example) - 包含所有可用选项的模板
- [`docker-compose.yml`](docker-compose.yml) - 服务定义和网络

### 文档
- [`CLAUDE.md`](CLAUDE.md) - 本文件 (平台级架构)
- [`AGENTS.md`](AGENTS.md) - 仓库约定和指南
- [`system/CLAUDE.md`](system/CLAUDE.md) - System 模块详情
- [`gateway/ARCHITECTURE.md`](gateway/ARCHITECTURE.md) - Gateway 路由逻辑
- [`docs/CONFIG_CENTER.md`](docs/CONFIG_CENTER.md) - 配置中心指南
- [`docs/COMMON_MODULE.md`](docs/COMMON_MODULE.md) - Common 模块使用

### 构建 & 部署
- [`Makefile`](Makefile) - 项目级编排命令
- [`scripts/init-db.sql`](scripts/init-db.sql) - PostgreSQL schema 初始化
- [`scripts/dev/start.sh`](scripts/dev/start.sh) - 开发启动脚本 (按顺序启动所有服务)
- [`scripts/dev/stop.sh`](scripts/dev/stop.sh) - 开发停止脚本 (停止所有服务)
- [`scripts/dev/run.sh`](scripts/dev/run.sh) - 本地开发助手 (旧版)

### 关键源文件
- System auth: [system/backend/internal/middleware/auth.go](system/backend/internal/middleware/auth.go)
- Manager preview: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Meta scanning: [meta/backend/internal/service/scan_service.go](meta/backend/internal/service/scan_service.go)
- Common client: [common/client/system.go](common/client/system.go)

## 故障排除

**服务无法启动**:
```bash
make status              # 检查正在运行的内容
docker-compose ps        # 检查容器状态
make logs                # 检查错误
```

**端口冲突**:
```bash
lsof -i :8080            # 检查什么在使用端口 8080
# 终止进程或在 docker-compose.yml 中更改端口
```

**数据库连接问题**:
```bash
docker-compose ps postgres    # 确保 PostgreSQL 正在运行
make db-shell                 # 尝试手动连接
docker-compose restart postgres
```

**无法访问 MinIO**:
```bash
make minio-setup         # 初始化 MinIO buckets
curl http://localhost:9001   # 检查 MinIO console
```

**JWT token 问题**: 确保服务间 `.env` 中的 `JWT_SECRET` 匹配 (System 和 Gateway 需要相同的 secret)

**跨服务调用失败**: 验证 `ENABLE_SERVICE_INTEGRATION=true` 且 docker-compose.yml 中的服务 URLs 正确
