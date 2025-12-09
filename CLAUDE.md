# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 交流语言 / Communication Language

**重要**: 在此项目中,**请尽可能使用中文与用户交流**。除非用户明确使用英文提问,否则默认使用中文回复。

**Important**: In this project, **please communicate with users in Chinese as much as possible**. Use English only when the user explicitly asks questions in English.

## Repository Structure

**ADDP (All Domain Data Platform / 全域数据平台)** is an enterprise data platform structured as microservices. Each service has its own directory:

- **common/** - Shared backend library: common client code, models, config loader, and utilities used across all backend services
- **common-frontend/** - Shared frontend library: Vue 3 components, utilities, and type definitions for frontend reuse
  - **basic/** - Basic UI components without map dependencies (StorageEngineForm, ImagePreview, formatters)
  - **map/** - Map-related components requiring OpenLayers and Gaode Map (GeoJsonPreview, ShapefilePreview, TablePreview)
- **portal/** - Unified portal entry point with iframe-based module integration - **IMPLEMENTED**
- **system/** - Core system module: user authentication, logging, resource management - **IMPLEMENTED** (PostgreSQL system schema)
- **gateway/** - API gateway: handles external requests and routes to internal services - **IMPLEMENTED** (reverse proxy)
- **manager/** - Data management: data source connections, upload directory organization, data preview - **PARTIALLY IMPLEMENTED** (Go backend structure created)
- **meta/** - Metadata service: data metadata parsing/storage/querying, schema scanning with cron scheduling - **IMPLEMENTED** (PostgreSQL metadata schema)
- **transfer/** - Data transfer: data import/export/synchronization - *Planned*

All services follow the same architectural pattern and use shared infrastructure (PostgreSQL, Redis, MinIO). Common code is shared via the `common` module (backend) and `common-frontend` module (frontend) to avoid duplication.

## Quick Start

### Development (System Module Only)

```bash
# From system/ directory
cd system

# Backend development (需要 PostgreSQL)
cd backend && go run cmd/server/main.go

# Frontend development
cd frontend && npm install && npm run dev

# Docker deployment (System only)
make docker-up
```

### Full Platform Deployment

```bash
# From project root
make init           # Initialize config files
make up             # Start System module only
make up-full        # Start all services (Gateway + all modules + infrastructure)
make status         # Check service status
make logs           # View all logs
```

### Production Deployment (Recommended for Production)

**生产环境一键部署**：使用 `scripts/prod/start.sh` 按正确顺序启动完整平台（基础设施 + 后端 + 前端 + Portal）

```bash
# From project root
./scripts/prod/start.sh
```

**部署流程** (自动执行):

1. **启动基础设施** (PostgreSQL, Redis, MinIO, Meilisearch)
2. **启动 System Backend** (其他服务依赖它)
3. **启动业务后端** (Manager, Meta, Transfer, Orchestrator, Develop, Gateway)
4. **启动前端服务** (所有模块前端 + Portal + Nginx)
5. **健康检查** (验证所有服务就绪)

**访问地址** (部署完成后):

- **✨ Portal 统一入口 (推荐)**: http://localhost:80
  - 统一登录，一键访问所有模块
  - 通过 Nginx 反向代理，提供最佳用户体验
- **Portal 独立访问** (开发调试): http://localhost:5170
- **API Gateway**: http://localhost:8000
- **独立模块访问** (如需单独访问):
  - System: http://localhost:8090
  - Manager: http://localhost:8091
  - Meta: http://localhost:8092
  - Transfer: http://localhost:8093
  - Orchestrator: http://localhost:8094
  - Develop: http://localhost:8095

**For detailed System module documentation, see `system/CLAUDE.md`.**

### Development Mode Startup (Recommended)

**Important**: Services must start in the correct order to ensure dependencies are met. Use the automated startup script to avoid race conditions.

```bash
# From project root

# 1. Start all services in correct order (Recommended)
make dev-start
# Or directly:
./scripts/dev/start.sh

# Skip Go dependency check (faster startup, saves 5-10 seconds)
SKIP_MODTIDY=1 ./scripts/dev/start.sh

# 2. Check service health
make dev-health

# 3. Stop all services
make dev-stop
# Or directly:
./scripts/dev/stop.sh
```

**Startup Process** (automatic):

1. **Step 0**: Go 依赖检查 (`go mod tidy`, can skip with `SKIP_MODTIDY=1`)
2. **Step 1**: Infrastructure startup (calls `scripts/infra/up.sh` - PostgreSQL, Redis, MinIO, Meilisearch)
3. **Step 2-7**: Backend services (in dependency order)
   - System Backend (8080) - configuration center, auth service
   - Manager Backend (8081) + Meta Backend (8082) (parallel)
   - Transfer Backend (8083) + Workers
   - Orchestrator Backend (8084)
   - Gateway (8000) - API router
4. **Step 8**: Frontend services (Portal, System, Manager, Meta, Transfer, Orchestrator)

**Startup Order**:

```
Infrastructure (PostgreSQL, Redis, MinIO, Meilisearch) [auto-started by start.sh]
  ↓
System Backend (8080) - auth, config center
  ↓
Manager Backend (8081) + Meta Backend (8082) (parallel)
  ↓
Transfer Backend (8083) + Transfer Worker + Meta Worker + Manager Worker
  ↓
Orchestrator Backend (8084)
  ↓
Gateway (8000) - API router
  ↓
Frontend services (Portal, System, Manager, Meta, Transfer, Orchestrator)
```

**Key Features**:

- ✅ **Single Command**: One script starts everything (infrastructure + backends + frontends)
- ✅ **Automatic dependency checking**: `go mod tidy` before startup (can skip with `SKIP_MODTIDY=1`)
- ✅ **Infrastructure auto-startup**: Calls `scripts/infra/up.sh` automatically
- ✅ **Smart skip**: If infrastructure is already running, skips restart (avoids pgvector recompilation)
- ✅ **Health check waiting**: Ensures each service is ready before starting the next
- ✅ **Progress indicators**: Color-coded output shows startup status
- ✅ **PID tracking**: Stores process IDs for graceful shutdown
- ✅ **Timeout handling**: Fails fast if service doesn't start within 60 seconds
- ✅ **Log files**: Redirects output to `logs/` directory for easy debugging

**Startup Script Details**:

- Location: `scripts/dev/start.sh`
- Waits for `/health` endpoint to return 200 before proceeding
- Creates `.dev-pids/` directory to track process IDs
- Logs stored in `logs/` directory (e.g., `logs/system-backend.log`)
- See `scripts/dev/README.md` for detailed documentation
- Optional frontend startup (prompts user)

**Service URLs** (after successful startup):

- Portal: http://localhost:5170
- Gateway: http://localhost:8000
- System Backend: http://localhost:8080
- Manager Backend: http://localhost:8081
- Meta Backend: http://localhost:8082

**Troubleshooting**:

- If startup fails, check logs in `logs/` directory
- Run `make dev-health` to verify which services are running
- See [docs/STARTUP_ORDER.md](docs/STARTUP_ORDER.md) for detailed dependency documentation

**Important Development Workflow**:

- **After modifying backend code**, always use `./scripts/dev/restart.sh` to restart all services
- This ensures all processes (including workers) are recompiled with the latest changes
- **Note**: `restart.sh` does NOT restart infrastructure containers (PostgreSQL, Redis, MinIO, Meilisearch)
  - Reason: Avoids pgvector extension recompilation (saves 1-2 minutes)
  - If infrastructure restart needed: `bash scripts/infra/down.sh && bash scripts/infra/up.sh`
  - `start.sh` automatically detects running infrastructure and skips startup
- Manual restart of individual services may cause version mismatch issues

## Technology Stack

### Backend

- **Language**: Go 1.23+
- **HTTP Framework**: Gin
- **ORM**: GORM
- **Databases**: PostgreSQL 15 (all modules with schema isolation: system, manager, metadata, transfer)
- **Cache/Queue**: Redis 7
- **Object Storage**: MinIO (S3-compatible)
- **Task Queue**: Asynq (Redis-based, for Transfer module), Cron (for Meta module scheduling)

### Frontend

- **Framework**: Vue 3 + Composition API
- **Build Tool**: Vite
- **UI Library**: Element Plus
- **State Management**: Pinia
- **Router**: Vue Router
- **HTTP Client**: Axios (with interceptors for auth)

### Infrastructure

- **Containerization**: Docker + Docker Compose
- **Reverse Proxy**: Nginx (production), Gateway service (API routing)
- **Database Schema Isolation**: PostgreSQL schemas (manager, metadata, transfer)
- **Data Separation**: System infrastructure (ADDP metadata) + Business infrastructure (user data) independently deployed

### Infrastructure Architecture

ADDP 采用**系统与业务数据分离**的架构设计:

```
┌─────────────────────────────────────────────────────────────┐
│  ADDP System Infrastructure (docker-compose.infra.yml)      │
│  Project Name: addp-infra                                    │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ postgres     │  │ redis        │  │ minio        │     │
│  │ (系统元数据) │  │ (缓存/队列)  │  │ (系统文件)   │     │
│  │ Port: 5433*  │  │ Port: 6379   │  │ Port: 9000-1 │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
│                    ┌──────────────┐                        │
│                    │ meilisearch  │                        │
│                    │ (全文检索)   │                        │
│                    │ Port: 7700   │                        │
│                    └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘
  * 默认 5433 避免与本地 PostgreSQL 冲突，可配置为 5432

┌─────────────────────────────────────────────────────────────┐
│  Business Infrastructure (business/docker-compose.yml)      │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐                        │
│  │ postgres     │  │ minio        │                        │
│  │ (业务数据库) │  │ (业务文件)   │                        │
│  │ Port: 5434   │  │ Port: 9002-3 │                        │
│  └──────────────┘  └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

**系统基础设施** (docker-compose.infra.yml):

- **Docker Compose 项目名**: `addp-infra`
- **容器命名**: 简洁命名 (postgres, redis, minio, meilisearch)，由项目名统一管理隔离
- **postgres**: 存储 ADDP 系统元数据 (用户、资源配置、元数据索引、任务定义等)
- **redis**: 缓存和任务队列 (Asynq)
- **minio**: 存储系统文件 (用户头像、系统配置、模块化 buckets)
- **meilisearch**: 全文检索引擎 (元数据资产搜索、文件索引)

**业务基础设施** (business/docker-compose.yml，独立部署):

- `postgres`: 存储用户通过 ADDP 管理的实际业务数据 (用户上传的 PostgreSQL 数据等)
- `minio`: 存储用户上传的业务文件 (Shapefile、GeoJSON、图片、视频等)

**分离优势**:

- ✅ 数据隔离: 系统数据与业务数据物理分离
- ✅ 独立扩展: 业务数据量增长时可单独扩展
- ✅ 安全性: 业务数据库可配置更严格的访问控制
- ✅ 可替换性: 业务基础设施可替换为云服务 (RDS、OSS)
- ✅ 端口隔离: 避免端口冲突，支持本地服务和容器服务并存

**基础设施管理**:
- 启动: `bash scripts/infra/up.sh` (自动完成所有初始化)
- 状态: `bash scripts/infra/status.sh`
- 停止: `bash scripts/infra/down.sh`
- 详见: [scripts/infra/README.md](scripts/infra/README.md)

详见 [business/README.md](business/README.md)

### Module-Based Resource Isolation

ADDP 采用**模块化资源隔离**策略,确保各模块资源独立管理:

**PostgreSQL Schema 隔离**:
```sql
CREATE SCHEMA IF NOT EXISTS system;       -- System 模块(用户、租户、资源)
CREATE SCHEMA IF NOT EXISTS manager;      -- Manager 模块(数据源、目录)
CREATE SCHEMA IF NOT EXISTS metadata;     -- Meta 模块(元数据、血缘)
CREATE SCHEMA IF NOT EXISTS transfer;     -- Transfer 模块(传输任务)
CREATE SCHEMA IF NOT EXISTS orchestrator; -- Orchestrator 模块(编排)
CREATE SCHEMA IF NOT EXISTS develop;      -- Develop 模块(查询、开发)
```

**MinIO Bucket 隔离** (系统 MinIO, 端口 9000-9001):
```
system/             -- System 模块(用户头像、系统配置)
manager/            -- Manager 模块(预览缓存、MVT 瓦片)
  └── mvt-tiles/    -- MVT 瓦片存储路径
meta/               -- Meta 模块(元数据相关文件)
transfer/           -- Transfer 模块(传输临时文件)
orchestrator/       -- Orchestrator 模块(编排文件)
develop/            -- Develop 模块(查询结果导出)
```

**Redis Key 命名规范**:
```
{module}:{middleware}:{function}:{id}

示例:
- meta:cache:scan_task:{tenant_id}:{resource_id}:{scan_type}
- meta:cache:scan_last_time:{resource_id}
- manager:cache:mvt:spatial:{fingerprint}:{z}:{x}:{y}
```

**Asynq Queue 命名规范**:
```
{module}:{priority}

示例:
- meta:default / meta:critical / meta:low        -- Meta 扫描任务队列
- transfer:default / transfer:critical / transfer:low  -- Transfer 传输任务队列
```

**Meilisearch Index 命名规范**:
```
{module}:{resource_type}

示例:
- meta:assets        -- Meta 模块资产索引
- manager:files      -- Manager 模块文件索引
- develop:results    -- Develop 模块查询结果索引
```

**命名规范优势**:
- ✅ 清晰隔离: 一眼看出资源归属模块
- ✅ 避免冲突: 不同模块资源不会相互干扰
- ✅ 易于管理: 可按模块清理、备份、监控资源
- ✅ 统一规范: 所有基础设施遵循相同命名模式

## Key Architectural Patterns

### Layered Backend Architecture (in system/backend/)

The Go backend follows a clean layered approach:

```
cmd/server/main.go          → Application entry point
internal/api/               → HTTP handlers + routing
internal/service/           → Business logic layer
internal/repository/        → Data access layer (GORM)
internal/models/            → Database models + DTOs
internal/middleware/        → Auth, logging middleware
pkg/utils/                  → Shared utilities (JWT, crypto)
```

**Data flow**: API Handler → Service → Repository → Database

### Frontend Architecture (Portal + Microservice Pattern)

**Unified Portal + Independent Module Frontends**:

The platform uses a **portal-based architecture** with a unified entry point:

```
portal/frontend/           → Unified Portal Entry (port 5170 dev / 8000 prod)
├── src/
│   ├── views/
│   │   ├── Portal.vue    → Main portal page with module cards
│   │   └── Login.vue     → Centralized login
│   ├── api/auth.js       → Authentication via System backend
│   └── router/           → Portal routes
│
│   Portal embeds module frontends via iframe:
│   - Left sidebar: Unified navigation for all modules
│   - Main area: iframe loading module frontends dynamically

system/frontend/           → System module (port 5173 dev / 8090 prod)
├── Standalone or embedded in portal
├── Features: Users, Logs, Resources

manager/frontend/          → Manager module (port 5174 dev / 8091 prod)
├── Standalone or embedded in portal
├── Features: DataSources, Directories, Preview

meta/frontend/             → Meta module (port 5175 dev / 8092 prod) - Planned
transfer/frontend/         → Transfer module (port 5176 dev / 8093 prod) - Planned
```

**Two Access Modes**:

1. **Unified Portal Mode** (Recommended for users):

   - Single entry: http://localhost:5170 (dev) or http://localhost:8000 (prod)
   - Integrated navigation with all modules
   - Module frontends load in iframe within portal
   - One login for all modules
2. **Standalone Module Mode** (For independent deployment):

   - Direct access to each module frontend
   - System: http://localhost:5173, Manager: http://localhost:5174
   - Each module has its own login
   - Suitable for deploying single module independently

**Key Frontend Principles**:

- Portal provides unified UX with consistent navigation
- Module frontends remain independent and can be deployed standalone
- All frontends share JWT auth pattern (token in localStorage)
- Portal and modules can authenticate independently
- In production, all requests route through Gateway (8000)

### Authentication Flow

1. User submits credentials to `POST /api/auth/login`
2. Backend validates with bcrypt, returns JWT (HS256, signed with `JWT_SECRET`)
3. Frontend stores token in localStorage (`auth.js` Pinia store)
4. Axios interceptor (`api/client.js`) adds `Authorization: Bearer <token>` to all requests
5. Backend `AuthMiddleware` validates JWT and injects user context into Gin context
6. Protected routes access user via `c.Get("user_id")`

### Database Architecture

All modules use **PostgreSQL 15** with schema isolation for data separation.

**System Module (system schema)**:

- **users** - User accounts with bcrypt hashed passwords
- **tenants** - Tenant information for multi-tenancy
- **audit_logs** - Automatic logging of all non-GET operations (via `LoggerMiddleware`)
- **resources** - Flexible resource configurations with JSON connection_info field (encrypted)

**Manager Module (manager schema)**:

- **data_sources** - Data source connections and configurations
- **directories** - File organization and hierarchy
- **permissions** - Access control for data sources and directories

**Meta Module (metadata schema)** - IMPLEMENTED:

- **meta_node** - Hierarchical nodes (schemas, prefixes, collections) with recursive structure, referencing `system.resources` by `res_id`
- **meta_item** - Leaf items (tables, objects, files) with JSON attributes
- **meta_dictionary** - Node type and child rule definitions for validation
- **meta_change_log** - Change tracking for audit and incremental sync (planned)

Note: Meta module uses a **unified hierarchical model** (resource → node → item) to support both relational databases and object storage, replacing the old type-specific tables (schemas, tables, fields)

**Transfer Module (transfer schema)** - PLANNED:

- **tasks** - Transfer task definitions
- **task_executions** - Task execution history
- **data_mappings** - Field mapping configurations

GORM AutoMigrate handles schema updates automatically on startup. PostgreSQL schemas initialized via `scripts/init-db.sql`.

### Configuration Center Pattern

**System as the Single Source of Truth**:

The platform implements a centralized configuration management pattern where **System module acts as the configuration center** for all other modules.

**Architecture**:

```
┌────────────────────────────────────────────────────┐
│   System Module (Configuration Center)            │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /internal/config API                   │    │
│   │  Returns: JWT_SECRET, DB connection,    │    │
│   │          ENCRYPTION_KEY                 │    │
│   └─────────────────────────────────────────┘    │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /api/resources (Business DB Config)    │    │
│   │  Manages all data source configs         │    │
│   │  (encrypted storage)                     │    │
│   └─────────────────────────────────────────┘    │
└────────────────┬───────────────────────────────────┘
                 │
      ┌──────────┼──────────┐
      ▼          ▼          ▼
  ┌────────┐ ┌────────┐ ┌─────────┐
  │ Manager│ │  Meta  │ │Transfer │
  │        │ │        │ │         │
  │ At     │ │ At     │ │ At      │
  │ Startup│ │ Startup│ │ Startup │
  │ ↓      │ │ ↓      │ │ ↓       │
  │ Get    │ │ Get    │ │ Get     │
  │ Config │ │ Config │ │ Config  │
  └────────┘ └────────┘ └─────────┘
```

**What is Centralized**:

1. **Authentication**: `JWT_SECRET` - ensures all services use the same JWT signing key
2. **System Database**: PostgreSQL connection info - single source for system data
3. **Business Databases**: Resources managed in System's `resources` table - all data source configs
4. **Encryption Key**: `ENCRYPTION_KEY` - consistent encryption across services

**Configuration Loading Flow**:

```
Module Startup
   ↓
Try to fetch config from System (/internal/config)
   ↓
   ├─ Success ✅
   │  └─ Use System config (JWT_SECRET, DB connection)
   │
   └─ Failure ⚠️
      └─ Fallback to local .env config
```

**Benefits**:

- ✅ **Single Source of Truth**: Change database password once, restart services to apply
- ✅ **Security**: Sensitive configs centrally managed and encrypted
- ✅ **Flexibility**: Supports both integrated and standalone deployment modes
- ✅ **Maintainability**: Reduced config duplication, easier to audit

**SystemClient Usage**:

All modules use `SystemClient` to fetch business database configurations from System:

```go
import (
    commonClient "github.com/addp/common/client"
    commonModels "github.com/addp/common/models"
)

// Create client with JWT token
client := commonClient.NewSystemClient(systemURL, jwtToken)

// List all data sources
resources, err := client.ListResources("postgresql")

// Get specific data source
resource, err := client.GetResource(resourceID)

// Build connection string (password auto-decrypted)
connStr, err := commonModels.BuildConnectionString(resource)
```

**Module .env Files**:

Each module only needs to configure module-specific settings:

```bash
# Manager/Meta/Transfer .env
PORT=8081                          # Module-specific port
DB_SCHEMA=manager                  # Module-specific schema
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true    # Enable config center

# Shared configs (JWT_SECRET, DB connection) fetched from System
# Fallback configs commented out (used only when integration disabled)
```

**See Also**: `docs/CONFIG_CENTER.md` for detailed configuration center usage guide.

## Common Module

The `common` module provides shared code to avoid duplication across Manager, Meta, and Transfer modules.

**Contents**:

- [client/system.go](common/client/system.go) - SystemClient for communicating with System module
- [models/resource.go](common/models/resource.go) - Shared Resource model and BuildConnectionString utility
- [config/loader.go](common/config/loader.go) - Centralized configuration loading with fallback

**Usage Pattern**:

```go
// In module's go.mod
require (github.com/addp/common v0.0.0)
replace github.com/addp/common => ../../common

// Import with alias to avoid conflicts
import (
    commonClient "github.com/addp/common/client"
    commonModels "github.com/addp/common/models"
)

// Use SystemClient to fetch resources
client := commonClient.NewSystemClient(systemURL, jwtToken)
resources, err := client.ListResources("postgresql")
resource, err := client.GetResource(resourceID)

// Build connection string (auto-decrypts password)
connStr, err := commonModels.BuildConnectionString(resource)
```

**Key Design Principles**:

- Minimal external dependencies (only Go stdlib)
- All modules use identical SystemClient implementation
- Resource model is canonical across all services
- Breaking changes to common affect all modules - test thoroughly

**See Also**: [docs/COMMON_MODULE.md](docs/COMMON_MODULE.md)

## Common Frontend

The `common-frontend` module provides shared Vue 3 components, utilities, and type definitions for frontend reuse across modules.

**Architecture**: Split into two sub-modules to avoid unnecessary dependencies:

```
common-frontend/
├── basic/          # Basic UI components (no map dependencies)
│   └── src/
│       ├── components/  - StorageEngineForm, ImagePreview, ExtractedMetadata
│       ├── utils/       - formatters, type utilities
│       ├── types/       - FieldType, FormatType, ResourceType
│       └── index.js
│
└── map/            # Map-related components (requires ol and @amap/amap-jsapi-loader)
    └── src/
        ├── components/  - MapContainer, GeoJsonPreview, ShapefilePreview, TablePreview
        ├── composables/ - useMapConfig, useGaodeMap, useOpenLayersMap
        └── utils/       - geo utilities, formatters
```

**Usage Patterns**:

**For modules WITHOUT map features** (System, Transfer):

```javascript
// vite.config.js
resolve: {
  alias: {
    '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
  }
}

// In components
import { StorageEngineForm, ImagePreview } from '@common-ui'
import { formatFileSize, formatDateTime } from '@common-ui'
```

**For modules WITH map features** (Manager):

```javascript
// vite.config.js
resolve: {
  alias: {
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
  }
}

// package.json dependencies
{
  "ol": "^9.2.4",
  "@amap/amap-jsapi-loader": "^1.0.1"
}

// In components
import { TablePreview, GeoJsonPreview, ShapefilePreview } from '@common-ui-map'
```

**Key Components**:

- **Preview Components**: ShapefilePreview, GeoJsonPreview, TablePreview, ImagePreview
- **Form Components**: StorageEngineForm (PostgreSQL/MinIO/S3 configuration)
- **Map Components**: MapContainer, OpenLayersRenderer, GaodeMapRenderer
- **Utilities**: formatFileSize, formatDateTime, detectFormatByExtension, isGeospatialFormat
- **Types**: FieldType, FormatType, ResourceType (aligned with backend models)

**Benefits**:

- ✅ **Modular Dependencies**: Modules only install what they need
- ✅ **Reduced Bundle Size**: Basic modules save ~2-3MB by excluding map libraries
- ✅ **Type Safety**: Shared type definitions ensure frontend-backend consistency
- ✅ **DRY Compliance**: UI components reused instead of duplicated
- ✅ **Unified Maintenance**: All shared components in one place

**Module Usage**:

- **System Frontend**: Uses `basic` (StorageEngineForm for resource configuration)
- **Manager Frontend**: Uses `map` (GeoJsonPreview, ShapefilePreview, TablePreview for data preview)
- **Meta Frontend**: Uses `basic` (ExtractedMetadata for metadata display)
- **Transfer Frontend**: Uses `basic` (field type utilities for mapping UI)
- **Portal Frontend**: Uses `basic` (common UI elements)

**See Also**: [common-frontend/README.md](common-frontend/README.md), [common-frontend/ARCHITECTURE.md](common-frontend/ARCHITECTURE.md)

## Development Workflows

### Adding New API Endpoints

Follow the layered architecture pattern used throughout the codebase:

1. **Define data models** in `internal/models/`:

   ```go
   type CreateResourceRequest struct {
       Name           string                 `json:"name" binding:"required"`
       ResourceType   string                 `json:"resource_type" binding:"required"`
       ConnectionInfo map[string]interface{} `json:"connection_info"`
   }
   ```
2. **Add repository methods** in `internal/repository/`:

   ```go
   func (r *ResourceRepository) Create(resource *models.Resource) error {
       return r.db.Create(resource).Error
   }
   ```
3. **Implement business logic** in `internal/service/`:

   ```go
   func (s *ResourceService) CreateResource(req *CreateResourceRequest) (*Resource, error) {
       // Validation, encryption, business rules
       return s.repo.Create(resource)
   }
   ```
4. **Create HTTP handler** in `internal/api/`:

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
5. **Register route** in `internal/api/router.go`:

   ```go
   protected.POST("/resources", resourceHandler.Create)
   ```

**Example PR**: See system module resource management implementation

### Database Migrations

GORM AutoMigrate handles schema changes automatically:

1. **Modify model struct** in `internal/models/`:

   ```go
   type Resource struct {
       ID             uint      `gorm:"primaryKey"`
       Name           string    `gorm:"not null"`
       NewField       string    `gorm:"default:''" json:"new_field"` // Add new field
   }
   ```
2. **Add to AutoMigrate** in `internal/repository/database.go`:

   ```go
   db.AutoMigrate(
       &models.Resource{},
       &models.User{},
       // Add new models here
   )
   ```
3. **Restart application** - migration runs on startup

**For Complex Migrations**:

- Create SQL script in `scripts/migrations/` for data transformations
- Run manually via `make db-migrate` before deploying new version
- Document breaking changes in PR description

**Meta Module Specifics**:
The unified metadata model (resource/node/item) requires coordinated updates:

- Model structs in [meta/backend/internal/models/](meta/backend/internal/models/)
- Dictionary validation in `meta_dictionary` table
- JSON schema version in `attributes` field if structure changes
- May require data migration script for existing metadata

### Adding Frontend Pages

**Important**: Add pages to the correct frontend based on functionality:

- System features (users, logs, resources) → `system/frontend/`
- Manager features (data sources, directories) → `manager/frontend/`
- Meta features (metadata, lineage) → `meta/frontend/`
- Transfer features (tasks, executions) → `transfer/frontend/`

Steps for each frontend:

1. Create Vue component in `<module>/frontend/src/views/`
2. Add API functions in `<module>/frontend/src/api/`
3. Register route in `<module>/frontend/src/router/index.js`
4. Add navigation link in `<module>/frontend/src/components/Layout.vue`

## Configuration

### Environment Variables

Root `.env` file (copy from `.env.example`):

```bash
# Security (MUST change for production)
JWT_SECRET=your-super-secret-jwt-key-change-this-in-production

# PostgreSQL - ADDP System Database
POSTGRES_PASSWORD=addp_password
POSTGRES_USER=addp
POSTGRES_DB=addp

# Redis
REDIS_PASSWORD=addp_redis

# MinIO - System Files
MINIO_SYSTEM_ROOT_USER=minioadmin
MINIO_SYSTEM_ROOT_PASSWORD=minioadmin

# MinIO - Business Data (deployed in business/docker-compose.yml)
BUSINESS_MINIO_ENDPOINT=host.docker.internal:9002
BUSINESS_MINIO_ACCESS_KEY=minioadmin
BUSINESS_MINIO_SECRET_KEY=minioadmin

# Service Integration
ENABLE_SERVICE_INTEGRATION=true  # Enable cross-service calls
```

### Port Assignments

**ADDP System Services**:


| Service              | Dev Port | Docker Port | Description                   |
| -------------------- | -------- | ----------- | ----------------------------- |
| **Nginx Gateway**    | **80**   | **80**      | **Unified entry (recommended)** |
| **Portal Frontend**  | **5170** | **5170**    | **Portal UI (via Nginx)**     |
| Gateway              | 8000     | 8000        | API Gateway (backend routing) |
| System Backend       | 8080     | 8080        | Auth, users, logs             |
| System Frontend      | 5173     | 8090        | Standalone access             |
| Manager Backend      | 8081     | 8081        | Data sources, files           |
| Manager Frontend     | 5174     | 8091        | Standalone access             |
| Meta Backend         | 8082     | 8082        | Metadata, lineage             |
| Meta Frontend        | 5175     | 8092        | Standalone access             |
| Transfer Backend     | 8083     | 8083        | Import/export tasks           |
| Transfer Frontend    | 5176     | 8093        | Standalone access             |
| Orchestrator Backend | 8084     | 8084        | Workflow orchestration        |
| Orchestrator Frontend| 5177     | 8094        | Standalone access             |
| Develop Backend      | 8085     | 8085        | Development tools             |
| Develop Frontend     | 5178     | 8095        | Standalone access             |
| PostgreSQL (System)  | 5432     | 5432        | ADDP system metadata          |
| Redis                | 6379     | 6379        | Cache & queue                 |
| MinIO System API     | 9000     | 9000        | System file storage           |
| MinIO System Console | 9001     | 9001        | System MinIO web UI           |
| Meilisearch          | 7700     | 7700        | Full-text search engine       |

**Business Infrastructure Services** (deployed via `business/docker-compose.yml`):


| Service                | Docker Port | Description                |
| ---------------------- | ----------- | -------------------------- |
| PostgreSQL (Business)  | 5433        | User business data storage |
| MinIO Business API     | 9002        | User file storage          |
| MinIO Business Console | 9003        | Business MinIO web UI      |

**Recommended Access**:
- **生产环境**: http://localhost:80 (通过 Nginx 访问 Portal 统一入口)
- **开发环境**: http://localhost:5170 (Portal 独立访问) 或各模块独立端口

**Business Infrastructure Setup**:

```bash
cd business
cp .env.example .env
docker-compose up -d
```

## Testing

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
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "SuperAdmin", "password": "20251001#SuperAdmin"}'

# 使用租户管理员登录
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "123456"}'
```

**初始化位置**: `system/backend/internal/repository/database.go`

### Running Tests

```bash
# Test all modules (from project root)
make test

# Test specific module
cd system/backend && go test ./...
cd manager/backend && go test ./...
cd meta/backend && go test ./...

# Test with coverage
go test -cover ./...

# Test specific package
go test ./internal/service/...

# Run tests with verbose output
go test -v ./...

# Run specific test function
go test -v -run TestFunctionName ./internal/service/
```

### Writing Tests

Go tests should be table-driven and placed in `_test.go` files:

```go
// Example: internal/service/resource_service_test.go
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
            // Test implementation
        })
    }
}
```

### Frontend Testing

Frontend tests are not yet implemented. When adding Vue components, document manual test scenarios in PR descriptions.

## Docker Deployment

### Business Infrastructure (Deploy First)

业务基础设施需要**先于 ADDP 系统启动**，因为 Manager 和 Meta 模块会连接到业务存储：

```bash
# 1. Deploy business infrastructure first
cd business
cp .env.example .env
# Edit .env to configure passwords
docker-compose up -d

# 2. Verify business services are running
docker-compose ps
docker-compose logs -f
```

### System Module Only (Default)

```bash
# From project root
make up              # Start System backend + frontend
make logs-system     # View logs
make down           # Stop services
```

### Full Platform

```bash
# From project root (ensure business infrastructure is running first)
make up-full        # Start all services with --profile full
make status         # Check all service status
make logs           # View all logs
make down           # Stop all services

# Individual service logs
make logs-system
make logs-manager
make logs-gateway
```

### Rebuild After Changes

```bash
make docker-build       # Rebuild System only
make docker-build-all   # Rebuild all services
docker-compose up -d    # Restart
```

### Docker Swarm Mode (High Availability)

For production environments requiring high availability, use Docker Swarm mode instead of standard Compose:

```bash
# 1. Initialize Swarm (one-time setup)
docker swarm init

# 2. Deploy to Swarm
docker stack deploy -c docker-compose.prod.yml addp

# 3. Verify services
docker service ls
docker service ps addp_transfer-worker  # Should show 2 replicas

# 4. View logs
docker service logs -f addp_transfer-worker

# 5. Scale manually (if needed)
docker service scale addp_transfer-worker=3

# 6. Update services (zero downtime)
docker service update --image addp-transfer-worker:v2.0 addp_transfer-worker

# 7. Stop services
docker stack rm addp
```

**Key Benefits of Swarm Mode**:

- ✅ **Auto-recovery**: Crashed containers automatically replaced with new replicas
- ✅ **Load balancing**: Built-in load balancing across replicas
- ✅ **Zero-downtime updates**: Rolling updates without service interruption
- ✅ **Resource management**: CPU and memory limits/reservations

**Transfer Worker High Availability** (configured in docker-compose.prod.yml):

- Default: 2 replicas running simultaneously
- Auto-restart on failure
- CPU limit: 2 cores per worker
- Memory limit: 2GB per worker

See [docs/DOCKER_SWARM.md](docs/DOCKER_SWARM.md) for detailed Swarm deployment guide.

**Data Persistence**:

**ADDP System** (docker-compose.yml):

- PostgreSQL: `postgres_data` volume (ADDP system metadata)
- Redis: `redis_data` volume (cache and queues)
- MinIO System: `minio_system_data` volume (system files)

**Business Infrastructure** (business/docker-compose.yml):

- PostgreSQL: `business_postgres_data` volume (user business data)
- MinIO Business: `business_minio_data` volume (user files)

## API Endpoints Summary

**Public**:

- `POST /api/auth/login` - Login
- `POST /api/auth/register` - Register

**Protected** (require JWT):

- `GET /api/users/me` - Current user
- `GET /api/users` - List users
- `GET/PUT/DELETE /api/users/:id` - User CRUD
- `GET /api/logs` - Audit logs (supports `?user_id=X` filter)
- `POST/GET/PUT/DELETE /api/resources` - Resource CRUD (supports `?resource_type=X` filter)

## Service Architecture Details

### Gateway Service (IMPLEMENTED)

**Purpose**: Unified API entry point for all microservices

**Key Features**:

- HTTP reverse proxy using Gin
- Route matching by URL prefix (`/api/auth/*` → System, `/api/datasources/*` → Manager, etc.)
- CORS middleware for cross-origin requests
- Transparent request/response forwarding (headers, body, query params preserved)
- Health check endpoint at `/health`

**Configuration**: Service URLs configured via environment variables (`SYSTEM_SERVICE_URL`, `MANAGER_SERVICE_URL`, etc.)

**Architecture Files**: See `gateway/ARCHITECTURE.md` for detailed request flow and routing rules

### Manager Service (IMPLEMENTED)

**Purpose**: Data source management, file organization, and data preview

**Implemented Features**:

- **Object storage preview** (MinIO, S3, OSS):
  - Directory/prefix navigation with hierarchical listing
  - Object content preview (text, JSON, GeoJSON, images)
  - PDF preview with streaming support
  - Office document preview (DOCX, PPTX) via conversion
  - Metadata display (size, last modified, content type)
  - Integration with Meta module for scanned metadata enrichment
- **Preview plugin system** ([manager/backend/internal/service/object_preview.go:1](manager/backend/internal/service/object_preview.go)):
  - Extensible preview handlers (TextPreview, ImagePreview, PDFPreview, DocxPreview, PptxPreview)
  - Content type detection and routing
  - Binary and text content handling
- Connection to System module for resource management
- Connection info decryption for secure access

**Key Files**:

- Backend preview service: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Frontend preview components: [manager/frontend/src/components/previews/](manager/frontend/src/components/previews/)
- Preview plugin registry: [manager/frontend/src/plugins/previews/index.js](manager/frontend/src/plugins/previews/index.js)

**Planned Features**:

- Database data preview (table records with pagination)
- Video/audio preview
- Additional office formats (XLS, CSV)
- Permission-based access control (user/group level)
- File upload and management

**Database**: PostgreSQL `manager` schema

### Meta Service (IMPLEMENTED)

**Purpose**: Metadata management and data lineage

**Implemented Features**:

- Data source synchronization from System module
- **Unified hierarchical metadata model** for all data source types:
  - Relational databases: resource (database) → node (schema) → item (table/view)
  - Object storage: resource (bucket) → node (prefix) → item (object)
- Metadata scanning for:
  - PostgreSQL, MySQL, and other JDBC-compatible databases
  - Object storage (MinIO, S3, OSS) via S3 API
- Schema-level scanning with status tracking (未扫描/扫描中/已扫描)
- Table and field metadata extraction (names, types, sizes, comments)
- Object storage metadata extraction (prefix hierarchy, object types, sizes)
- Automatic and scheduled scanning with cron expressions (default: daily at midnight)
- **Event-driven automatic scanning**: System registration triggers Meta scanning (via Redis Pub/Sub)
- Multi-tenant metadata isolation
- **JSON-based flexible attributes** with schema versioning

**Automatic Scanning Trigger**:
When registering a storage engine in System module, you can configure automatic metadata scanning:

- **Immediate**: Scan starts automatically after registration (no manual trigger needed)
- **Daily/Weekly**: Creates scheduled task in Meta module
- **Manual**: No automatic scanning, requires manual trigger in Meta frontend

This is implemented via **event-driven architecture**:

1. System publishes resource change event to Redis when creating/updating resources
2. Meta subscribes to these events and checks `ScanConfig.ScheduleType`
3. If `ScheduleType == "immediate"`, Meta automatically creates and enqueues a scan task
4. No circular dependency: System → Redis Pub/Sub → Meta (one-way communication)

**Scanning Workflow**:

1. Sync data sources from System module `/api/resources`
2. Select data source and schemas/prefixes to scan
3. Extract metadata hierarchically:
   - Database: system.resources (database) → meta_node (schemas) → meta_item (tables with field details in JSON)
   - Object Storage: system.resources (bucket scope) → meta_node (prefixes) → meta_item (objects with file metadata)
4. Store in PostgreSQL `metadata` schema with tenant isolation
5. Track scan status, sync version, and last scan time
6. Support manual triggers and scheduled auto-sync

**Architectural Highlights**:

- **Node type validation**: `meta_dictionary` tables enforce valid parent-child relationships
- **Soft delete**: All entities use `deleted_at` for safe deletion and recovery
- **Path tracking**: Nodes maintain `depth`, `path` (ID chain), and `full_name` for efficient querying
- **Incremental sync**: `sync_version` and `last_synced_at` enable change detection

**Planned Features**:

- Data lineage tracking (source → transformation → target)
- Tag-based search and discovery
- Extended metadata statistics and profiling
- `meta_change_log` for audit trail and rollback

**Database**: PostgreSQL `metadata` schema (tables: meta_node, meta_item, meta_dictionary, meta_change_log)

### Transfer Service (PLANNED)

**Purpose**: Data import/export and synchronization

**Planned Features**:

- Import from external sources (databases, APIs, files)
- Export to various targets
- Scheduled tasks with Cron expressions
- Field mapping and transformations
- Batch processing with progress tracking
- Asynq-based task queue for async execution
- Retry mechanism for failed transfers

**Database**: PostgreSQL `transfer` schema (tables: tasks, task_executions, data_mappings)

**Task Queue Architecture**:

- **Queue Naming**: Uses module-prefixed queues to avoid conflicts with other modules
  - `transfer:critical` - High priority tasks (紧急任务)
  - `transfer:default` - Normal priority tasks (普通任务)
  - `transfer:low` - Low priority tasks (低优先级任务)
- **Redis Storage Structure**:
  ```
  asynq:transfer:default:pending    → 等待处理的任务
  asynq:transfer:default:active     → 正在处理的任务
  asynq:transfer:default:scheduled  → 延迟执行的任务
  asynq:transfer:default:retry      → 失败重试队列
  asynq:transfer:default:archived   → 永久失败的任务 (死信队列)
  ```
- **Multi-Module Isolation**: Each module uses its own queue namespace
  - Transfer: `transfer:*` queues
  - Meta (future): `meta:*` queues
  - Other modules: `{module_name}:*` queues
- **Worker Configuration**: Runs with Docker Swarm for high availability (2 replicas by default)

## Inter-Service Communication

**Current Pattern**: HTTP REST calls between services

- Services discover each other via environment variables (e.g., `SYSTEM_SERVICE_URL`)
- Manager/Meta/Transfer can call System APIs for user validation
- Manager notifies Meta when new data sources are added
- Transfer queries Manager for data source connection info

**Auth Propagation**: JWT tokens passed through in `Authorization` headers

**Error Handling**: Services return standard HTTP status codes; calling services handle retries

## Code Quality Principles

### DRY (Don't Repeat Yourself)

**Core Principle**: Avoid code duplication across modules. Extract common functionality to the `common/` module.

**Why it matters**:

- ✅ Single point of maintenance - fix bugs once, benefit all modules
- ✅ Consistency - all modules use identical implementations
- ✅ Reduces error risk - no need to remember to update multiple locations
- ✅ Easier refactoring - change logic in one place

**Examples of shared code in `common/`**:

- **`common/config/LoadEnv(levelsUp int)`** - Load .env file from project root
  ```go
  // In each module's main.go
  commonConfig.LoadEnv(4)  // system/backend/cmd/server (4 levels up)
  commonConfig.LoadEnv(3)  // gateway/cmd/gateway (3 levels up)
  ```
- **`common/client/SystemClient`** - Communicate with System module
- **`common/models/Resource`** - Shared resource model
- **`common/config/LoadSharedConfig()`** - Fetch config from System

**When to extract to common/**:

1. Code appears in 2+ modules with minimal variation
2. Logic is module-agnostic (not specific to one service's business domain)
3. Function can be parameterized to handle differences (e.g., path depth)

**When NOT to extract**:

- Module-specific business logic
- Code that's likely to diverge between modules
- Single-use functions with no reuse potential

**Implementation pattern**:

```go
// Step 1: Add to common/config/loader.go or create new file
func SharedFunction(param int) {
    // Implementation
}

// Step 2: Import in each module
import commonConfig "github.com/addp/common/config"

// Step 3: Use it
commonConfig.SharedFunction(value)
```

**Example PR**: See .env loading refactor - extracted `godotenv` logic from 4 duplicated main.go files to `common/config/LoadEnv()`

## Development Guidelines for New Services

When implementing or extending services:

1. **Follow System module pattern**:

   ```
   service/backend/
   ├── cmd/server/main.go       # Entry point
   ├── internal/
   │   ├── api/                 # HTTP handlers
   │   ├── service/             # Business logic
   │   ├── repository/          # Data access
   │   ├── models/              # Data structures
   │   ├── middleware/          # Auth, logging
   │   └── config/              # Configuration
   └── pkg/utils/               # Shared utilities
   ```
2. **Database conventions**:

   - Use PostgreSQL schema isolation (except System which uses SQLite)
   - GORM for ORM with AutoMigrate
   - Add schemas to `scripts/init-db.sql`
   - Use `updated_at` triggers for timestamp tracking
3. **Configuration**:

   - Read from environment variables via `internal/config/config.go`
   - Support both development and Docker deployment modes
   - Set defaults for missing env vars
4. **Authentication**:

   - Reuse System module's JWT validation logic
   - Import auth middleware from System or create identical one
   - Extract user_id from JWT claims and pass to service layer
5. **Docker integration**:

   - Create Dockerfile in service root
   - Add service to `docker-compose.yml` with `profile: full`
   - Use health checks for dependency management
   - Connect to `addp-network` for inter-service communication
6. **Frontend integration**:

   - Create independent `<module>/frontend/` directory
   - Copy structure from `system/frontend/` (Vue 3 + Pinia + Element Plus)
   - Create `api/client.js` pointing to module's backend
   - Create `api/auth.js` pointing to System backend (8080) for authentication
   - Copy auth store pattern from System module (independent copy, not shared)
   - Set unique dev port in `vite.config.js` (System: 5173, Manager: 5174, etc.)
   - Configure router base path (e.g., `/manager/` for Manager module)
   - Create Dockerfile and nginx.conf for production deployment
   - Add to docker-compose.yml with unique port and `profile: full`

## Frontend Development Workflow

### Quick Start: Portal + All Modules

```bash
# Terminal 1: Start Portal (unified entry)
cd portal/frontend
npm install
npm run dev
# Access: http://localhost:5170

# Terminal 2: Start System frontend
cd system/frontend
npm install
npm run dev

# Terminal 3: Start Manager frontend
cd manager/frontend
npm install
npm run dev

# Now visit http://localhost:5170 for unified experience
# All modules accessible through single portal interface
```

### Running Individual Frontends (Standalone Mode)

```bash
# System frontend (port 5173)
cd system/frontend
npm run dev
# Access: http://localhost:5173

# Manager frontend (port 5174)
cd manager/frontend
npm run dev
# Access: http://localhost:5174

# Portal (port 5170)
cd portal/frontend
npm run dev
# Access: http://localhost:5170
```

### Frontend-Backend Connection in Development

**Development mode** (direct backend connection):

- System frontend → System backend (localhost:8080)
- Manager frontend → Manager backend (localhost:8081)
- Auth requests → System backend (localhost:8080)

**Production mode** (via Gateway):

- All frontend requests → Gateway (localhost:8000)
- Gateway routes to appropriate backend

### Creating New Module Frontend

When implementing a new module (e.g., Meta), follow these steps:

1. **Copy frontend structure**:

   ```bash
   cp -r system/frontend meta/frontend
   cd meta/frontend
   ```
2. **Update configuration**:

   - `package.json`: Change name to `meta-frontend`
   - `vite.config.js`: Change port to unique number (e.g., 5175)
   - `index.html`: Update title
   - `src/router/index.js`: Set base path to `/meta/`
   - `src/api/client.js`: Point baseURL to meta backend (8082)
   - Keep `src/api/auth.js` pointing to System backend (8080)
3. **Configure common-frontend alias** (choose based on module needs):

   For modules **without map features** (System, Meta, Transfer):
   ```javascript
   // vite.config.js
   resolve: {
     alias: {
       '@': resolve(__dirname, 'src'),
       '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
     }
   }
   ```

   For modules **with map features** (Manager):
   ```javascript
   // vite.config.js
   resolve: {
     alias: {
       '@': resolve(__dirname, 'src'),
       '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
     }
   }

   // package.json - add map dependencies
   {
     "dependencies": {
       "ol": "^9.2.4",
       "@amap/amap-jsapi-loader": "^1.0.1"
     }
   }
   ```
4. **Update views and components** to match module's functionality
5. **Add Dockerfile and nginx.conf** (copy from manager/frontend as template)
6. **Add to docker-compose.yml**:

   ```yaml
   meta-frontend:
     build:
       context: ./meta/frontend
     ports:
       - "8092:80"
     profiles:
       - full
   ```

**Using Common Frontend Components**:

```vue
<script setup>
// For basic modules
import { StorageEngineForm, ImagePreview } from '@common-ui'
import { formatFileSize, FieldType } from '@common-ui'

// For map-enabled modules
import { TablePreview, GeoJsonPreview, ShapefilePreview } from '@common-ui-map'

const resourceForm = ref({
  resource_type: 'postgresql',
  name: '',
  connection_info: {}
})
</script>

<template>
  <StorageEngineForm v-model="resourceForm" />
  <TablePreview :data="tableData" />
</template>
```

## Common Make Commands (Project Root)

```bash
# Initialization
make init                # Create config files and directories
make install-deps        # Install Go and npm dependencies

# Development
make dev-start           # Start all services in correct order (RECOMMENDED)
make dev-stop            # Stop all development services
make dev-health          # Check health of all services
make dev-system          # Run System in development mode
make dev-manager         # Run Manager backend
make dev-gateway         # Run Gateway service

# Docker Operations
make up                  # Start System module only
make up-full             # Start all services (full platform)
make up-infra            # Start only PostgreSQL, Redis, MinIO
make down                # Stop all services
make restart             # Restart System module
make restart-full        # Restart all services

# Building
make build               # Build all artifacts to dist/
make docker-build        # Build System Docker images
make docker-build-all    # Build all service Docker images

# Monitoring
make status              # Show all service status and URLs
make logs                # View all service logs
make logs-system         # View System logs only
make logs-manager        # View Manager logs
make health              # Check health of all services

# Database
make db-shell            # Connect to PostgreSQL
make db-migrate          # Run database migrations (init-db.sql)
make redis-cli           # Connect to Redis
make minio-setup         # Initialize MinIO buckets
make backup              # Backup PostgreSQL database
make restore FILE=...    # Restore database from backup

# Testing & Quality
make test                # Run all tests
make test-system         # Run System module tests
make lint                # Run code linters
make fmt                 # Format Go code

# Cleanup
make clean               # Remove build artifacts
make clean-all           # Remove all data and volumes (DESTRUCTIVE)
```

## Important File Locations

### Configuration

- [`.env`](.env) - Root environment variables (shared config)
- [`.env.example`](.env.example) - Template with all available options
- [`docker-compose.yml`](docker-compose.yml) - Service definitions and networking

### Documentation

- [`CLAUDE.md`](CLAUDE.md) - This file (platform-wide architecture)
- [`AGENTS.md`](AGENTS.md) - Repository conventions and guidelines
- [`system/CLAUDE.md`](system/CLAUDE.md) - System module details
- [`gateway/ARCHITECTURE.md`](gateway/ARCHITECTURE.md) - Gateway routing logic
- [`docs/CONFIG_CENTER.md`](docs/CONFIG_CENTER.md) - Configuration center guide
- [`docs/COMMON_MODULE.md`](docs/COMMON_MODULE.md) - Common module usage
- [`common-frontend/README.md`](common-frontend/README.md) - Common frontend components guide
- [`common-frontend/ARCHITECTURE.md`](common-frontend/ARCHITECTURE.md) - Common frontend architecture

### Build & Deploy

- [`Makefile`](Makefile) - Project-wide orchestration commands
- [`scripts/init-db.sql`](scripts/init-db.sql) - PostgreSQL schema initialization
- [`scripts/dev/start.sh`](scripts/dev/start.sh) - Development startup script (按顺序启动所有服务)
- [`scripts/dev/stop.sh`](scripts/dev/stop.sh) - Development stop script (停止所有服务)
- [`scripts/dev/run.sh`](scripts/dev/run.sh) - Local development helper (legacy)

### Key Source Files

- System auth: [system/backend/internal/middleware/auth.go](system/backend/internal/middleware/auth.go)
- Manager preview: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Meta scanning: [meta/backend/internal/service/scan_service.go](meta/backend/internal/service/scan_service.go)
- Common client: [common/client/system.go](common/client/system.go)
- Common frontend basic: [common-frontend/basic/src/index.js](common-frontend/basic/src/index.js)
- Common frontend map: [common-frontend/map/src/index.js](common-frontend/map/src/index.js)

## Troubleshooting

**Services won't start**:

```bash
make status              # Check what's running
docker-compose ps        # Check container status
make logs                # Check for errors
```

**Port conflicts**:

```bash
lsof -i :8080            # Check what's using port 8080
# Kill process or change port in docker-compose.yml
```

**Database connection issues**:

```bash
docker-compose ps postgres    # Ensure PostgreSQL is running
make db-shell                 # Try connecting manually
docker-compose restart postgres
```

**Cannot access MinIO**:

```bash
make minio-setup         # Initialize MinIO buckets
curl http://localhost:9001   # Check MinIO console
```

**JWT token issues**: Ensure `JWT_SECRET` in `.env` matches between services (System and Gateway need same secret)

**Cross-service calls failing**: Verify `ENABLE_SERVICE_INTEGRATION=true` and service URLs are correct in docker-compose.yml
