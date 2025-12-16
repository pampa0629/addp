# CLAUDE-CN.md

本文件为 Claude Code (claude.ai/code) 在处理此仓库代码时提供指导。

## 交流语言 / Communication Language

**重要**: 在此项目中,**请尽可能使用中文与用户交流**。除非用户明确使用英文提问,否则默认使用中文回复。

**Important**: In this project, **please communicate with users in Chinese as much as possible**. Use English only when the user explicitly asks questions in English.

## 开发原则
详细内容需要时，请阅读 docs/addp开发原则.md 。
**重要**: ADDP 当前处于活跃开发阶段。以下原则指导所有开发工作:
### 1. 无需考虑向后兼容性
### 2. 保持简洁
### 3. 无需征得同意
### 4. 全面考虑
### 5. 彻底解决根本原因
### 6. 敢于删除
### 7. 敢于质疑
### 8. DRY (Don't Repeat Yourself / 不要重复自己)

## 仓库结构

**ADDP (All Domain Data Platform / 全域数据平台)** 是一个企业级数据平台,采用微服务架构。每个服务都有自己的目录:

- **common/** - 后端共享库: 通用客户端代码、模型、配置加载器和所有后端服务使用的工具
- **common-frontend/** - 前端共享库: Vue 3 组件、工具和类型定义,供前端复用
  - **basic/** - 基础 UI 组件,无地图依赖 (StorageEngineForm、ImagePreview、格式化器)
  - **map/** - 地图相关组件,需要 OpenLayers 和高德地图 (GeoJsonPreview、ShapefilePreview、TablePreview)
- **portal/** - 统一门户入口,基于 iframe 的模块集成 - 
- **system/** - 核心系统模块: 用户认证、日志、资源管理 -  (PostgreSQL system schema)
- **gateway/** - API 网关: 处理外部请求并路由到内部服务 -  (反向代理)
- **manager/** - 数据管理: 数据源连接、上传目录组织、数据预览 
- **meta/** - 元数据服务: 数据元数据解析/存储/查询,使用 cron 调度的模式扫描 -  (PostgreSQL metadata schema)
- **transfer/** - 数据传输: 数据导入/导出/同步 
- **orchestrator/** - 工作流编排: 任务调度和执行 
- **develop/** - 开发工作台: SQL 执行、GIS 工作流管理 
- **geopandas-engine/** - 空间计算引擎: 基于 Python 的 GIS 工作流执行,提供 N个空间算子

所有服务遵循相同的架构模式,使用共享的基础设施 (PostgreSQL、Redis、MinIO、Meilisearch)。通过 `common` 模块(后端)和 `common-frontend` 模块(前端)共享通用代码,避免重复。

## 各模块端口
需要时请阅读 docs/addp配置介绍.md 

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
为确保所有模块依赖版本一致，ADDP 平台使用以下统一的 Go 依赖版本（最后更新: 2025-12-15）。
需要涉及技术栈详细信息时，请访问 docs/技术栈规约.md 文档。


### 基础设施

- **容器化**: Docker + Docker Compose
- **反向代理**: Nginx (生产环境), Gateway 服务 (API 路由)
- **数据库 Schema 隔离**: PostgreSQL schemas (manager, metadata, transfer)
- **数据分离**: 系统基础设施 (ADDP 元数据) + 业务库 (用户数据) 独立部署

### 基础设施架构

ADDP 采用**系统与业务数据分离**的架构设计:

**系统基础设施** (docker-compose.infra.yml):

- **Docker Compose 项目名**: `addp-infra`
- **容器命名**: 简洁命名 (postgres, redis, minio, meilisearch),由项目名统一管理隔离
- **postgres**: 存储 ADDP 系统元数据 (用户、资源配置、元数据索引、任务定义等)
- **redis**: 缓存和任务队列 (Asynq)
- **minio**: 存储系统文件 (用户头像、系统配置、模块化 buckets)
- **meilisearch**: 全文检索引擎 (元数据资产搜索、文件索引)

**业务库** (business/docker-compose.yml,独立部署):
- `business-postgres`: 存储用户通过 ADDP 管理的实际业务数据 (用户上传的 PostgreSQL 数据等)
- `business-minio`: 存储用户上传的业务文件 (Shapefile、GeoJSON、图片、视频等)

### 基于模块的资源隔离
ADDP 采用**模块化资源隔离**策略,确保各模块资源独立管理:
**PostgreSQL Schema 隔离**:按照模块名隔离
**MinIO Bucket 隔离** :按照模块名隔离
**Redis Key 命名规范**:{module}:{middleware}:{function}:{id}
**Asynq Queue 命名规范**:{module}:{priority}
**Meilisearch Index 命名规范**:{module}:{resource_type}


## 核心架构模式

### 分层后端架构 (在 system/backend/ 中)
Go 后端遵循清晰的分层方法:
```
cmd/server/main.go          → 应用入口
internal/api/               → HTTP 处理器 + 路由
internal/service/           → 业务逻辑层
internal/repository/        → 数据访问层 (GORM)
internal/models/            → 数据库模型 + DTO
internal/middleware/        → 认证、日志中间件
pkg/utils/                  → 共享工具 (JWT、加密)
```
**数据流**: API Handler → Service → Repository → Database

### 前端架构 (Portal + 微服务模式)

**统一 Portal + 独立模块前端**:

平台使用**基于 portal 的架构**,提供统一入口:

```
portal/frontend/           → 统一 Portal 入口 (port 5170 dev / 8000 prod)
├── src/
│   ├── views/
│   │   ├── Portal.vue    → 主 portal 页面,包含模块卡片
│   │   └── Login.vue     → 集中式登录
│   ├── api/auth.js       → 通过 System backend 认证
│   └── router/           → Portal 路由
│
│   Portal 通过 iframe 嵌入模块前端:
│   - 左侧边栏: 所有模块的统一导航
│   - 主区域: iframe 动态加载模块前端

system/frontend/           → System 模块 (port 5173 dev / 8090 prod)
├── 独立或嵌入在 portal 中
├── 功能: 用户、日志、资源
其它模块类似system。
```

**两种访问模式**:

1. **统一 Portal 模式** (推荐给用户):

   - 单一入口: http://localhost:5170 (dev) 或 http://localhost:8000 (prod)
   - 集成导航,包含所有模块
   - 模块前端在 portal 的 iframe 中加载
   - 一次登录访问所有模块
2. **独立模块模式** (用于独立部署):

   - 直接访问每个模块前端
   - System: http://localhost:5173, Manager: http://localhost:5174
   - 每个模块有自己的登录
   - 适合独立部署单个模块

**前端关键原则**:

- Portal 提供统一的用户体验和一致的导航
- 模块前端保持独立,可以独立部署
- 所有前端共享 JWT 认证模式 (token 存储在 localStorage)
- Portal 和模块可以独立认证
- 在生产环境,所有请求通过 Gateway (8000) 路由


### 测试登录：使用租户管理员登录
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "123456"}'

### 配置中心模式
需要详细内容时，请阅读 docs/addp配置介绍.md

### 共享模块
`common` 模块提供共享代码,避免**所有其他后端模块**之间的重复 (Manager、Meta、Transfer、Orchestrator、Develop 和 GeoPandas Engine 集成)。
`common-frontend` 模块提供共享的 Vue 3 组件、工具和类型定义,供跨模块的前端复用。
对于 ## Common 模块 和 common-frontend 模块的详细介绍，需要时请阅读： docs/共享模块介绍.md

### 新模块开发 
需要开发新模块时，请阅读： docs/新模块开发指南.md


## 重要文件位置

### 配置

- [`.env`](.env) - 根环境变量 (共享配置)
- [`.env.example`](.env.example) - 包含所有可用选项的模板
- [`docker-compose.yml`](docker-compose.yml) - 服务定义和网络

### 文档

- [`CLAUDE.md`](CLAUDE.md) - 本文件 (平台范围架构)
- [`system/CLAUDE.md`](system/CLAUDE.md) - System 模块详情
- [`gateway/ARCHITECTURE.md`](gateway/ARCHITECTURE.md) - Gateway 路由逻辑
- [`docs/CONFIG_CENTER.md`](docs/CONFIG_CENTER.md) - 配置中心指南
- [`docs/COMMON_MODULE.md`](docs/COMMON_MODULE.md) - Common 模块使用
- [`common-frontend/README.md`](common-frontend/README.md) - Common 前端组件指南
- [`common-frontend/ARCHITECTURE.md`](common-frontend/ARCHITECTURE.md) - Common 前端架构
- 各模块目录下的 DATA_STRUCTURES.md 

### 构建和部署
需要时，请阅读 docs/addp部署和开发步骤.md

### 关键源文件

- System 认证: [system/backend/internal/middleware/auth.go](system/backend/internal/middleware/auth.go)
- Manager 预览: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Meta 扫描: [meta/backend/internal/service/scan_service.go](meta/backend/internal/service/scan_service.go)
- Common 客户端: [common/client/system.go](common/client/system.go)
- Common frontend basic: [common-frontend/basic/src/index.js](common-frontend/basic/src/index.js)
- Common frontend map: [common-frontend/map/src/index.js](common-frontend/map/src/index.js)

## 故障排除
**JWT token 问题**: 确保 `.env` 中的 `JWT_SECRET` 在服务间匹配 (System 和 Gateway 需要相同的密钥)
**跨服务调用失败**: 验证 `ENABLE_SERVICE_INTEGRATION=true` 且 docker-compose.yml 中的服务 URL 正确
