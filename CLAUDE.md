# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 提供在本仓库中工作时的指导说明。

## 交流语言

**重要**: 在本项目中,**请使用中文与用户交流**。除非用户明确使用英文提问,否则默认使用中文回复。

## 📚 文档导航地图(给 Claude Code 使用)

**遇到以下场景时,主动阅读对应文档**:


| 场景                 | 必读文档                      | 触发关键词                        |
| -------------------- | ----------------------------- | --------------------------------- |
| 开发原则与编码规范   | docs/addp开发原则.md          | 原则、规范、DRY、向后兼容         |
| 环境配置、密钥      | docs/addp配置介绍.md          | 配置、环境变量、.env              |
| 端口               | docs/addp端口分配.md          | 端口                              |
| Go/前端依赖版本      | docs/addp技术栈规约.md            | 依赖、版本、升级、库              |
| 共享模块使用         | docs/addp共享模块介绍.md          | common、共享代码、复用            |
| 创建新模块           | docs/addp新模块开发指南.md        | 新模块、脚手架、模板              |
| 新增存储引擎/数据库   | docs/addp新增存储引擎指南.md      | 新数据库、存储引擎 |
| 新增数据类型/数据格式  | docs/addp数据类型扩展指南.md     | 新数据类型、新数据格式 |
| 故障和问题修复        | docs/addp常见故障排查.md        | 出错、修复、问题                  |
| Gateway 路由         | gateway/ARCHITECTURE.md       | 路由、转发、API网关               |
| System 模块详情      | system/CLAUDE.md              | 认证、用户、租户、日志            |
| Manager 模块详情     | manager/CLAUDE.md             | 数据预览、MVT、对象存储、插件     |
| Meta 模块详情        | meta/CLAUDE.md                | 元数据扫描、定时调度、向量化      |
| Transfer 模块详情    | transfer/CLAUDE.md            | 数据导入、导出、同步、Asynq       |
| Orchestrator 模块详情| orchestrator/CLAUDE.md        | 工作流编排、DAG、任务调度         |
| Develop 模块详情     | develop/CLAUDE.md             | SQL 执行、工作流、算子        |
| Service 模块详情     | service/CLAUDE.md             | 数据服务、OGC 标准、API 发布      |


**重要**:

- 遇到相关问题时,优先阅读对应文档
- 多个场景可能需要阅读多份文档
- 外链文档包含详细信息,主文档仅提供概览

## 开发原则

**重要**: ADDP 目前处于积极开发阶段。以下原则指导所有开发工作:

### 1. 无需考虑向后兼容

开发阶段优先考虑最佳架构。可自由修改数据库schema结构、API定义和前端调用代码；且无需考虑数据兼容和迁移。

### 2. 保持整洁

临时脚本和文档保存到 /tmp/,保持项目树整洁。你觉得需要保留的文档，需要征求用户的同意，保存到 ./docs 或 对应模块的docs目录下。

### 3. 无需请求权限

在自动编辑模式下,自由执行脚本,无需每次询问用户。

### 4. 全局思考

任何修改都要考虑全局影响,同步更新相关模块。

### 5. 修复根本原因

深入分析根本原因,彻底解决而不是打补丁。

### 6. 大胆删除

删除过时的代码和文件,不要保留"以防万一"的内容。

### 7. 挑战不合理的需求

质疑不合理的需求,平等讨论达成共识。

### 8. DRY (不要重复自己)

将重复代码提取到 common/ 或 common-frontend/ 模块。

详细说明: docs/addp开发原则.md

## 仓库结构

**ADDP (全域数据平台)** 是一个采用微服务架构的企业数据平台。每个服务都有自己的目录:

- **common/** - 后端共享库:所有后端服务使用的通用客户端代码、模型、配置加载器和工具
- **common-frontend/** - 前端共享库:Vue 3 组件、工具和类型定义,供前端复用
  - **basic/** - 基础 UI 组件,无地图依赖(StorageEngineForm、ImagePreview、formatters)
  - **map/** - 地图相关组件,需要 OpenLayers 和高德地图(GeoJsonPreview、ShapefilePreview、TablePreview)
- **portal/** - 统一门户入口,基于 iframe 的模块集成 - **已实现**
- **system/** - 核心系统模块:用户认证、日志、引擎管理 - **已实现** (PostgreSQL system schema)
- **gateway/** - API 网关:处理外部请求并路由到内部服务 - **已实现** (反向代理)
- **manager/** - 数据管理:数据源连接、上传目录组织、数据预览 - **已实现**
- **meta/** - 元数据服务:数据元数据解析/存储/查询,定时扫描 - **已实现** (PostgreSQL metadata schema)
- **transfer/** - 数据传输:数据导入/导出/同步 - **已实现**
- **orchestrator/** - 工作流编排:任务调度和执行 - **已实现**
- **develop/** - 开发工作台:SQL 执行、GIS 工作流管理 - **已实现**
- **service/** - 数据服务模块:外部服务注册、数据查询服务、OGC 标准支持 - **已实现** (PostgreSQL service schema)
- **engines/geopandas/** - 空间计算引擎:基于 Python 的 GIS 工作流执行,提供 21 个空间算子 - **已实现**

所有服务遵循相同的架构模式,使用共享基础设施(PostgreSQL、Redis、MinIO、Meilisearch)。通过 `common` 模块(后端)和 `common-frontend` 模块(前端)共享通用代码,避免重复。

关于 Common 模块和 common-frontend 模块的详细介绍,需要时请阅读: docs/共享模块介绍.md

## 快速启动

### 基础启动（3步）

1. **启动基础设施**: `bash scripts/infra/up.sh`
2. **启动开发环境**: `bash scripts/dev/start.sh`（并行启动优化，25-39 秒）
3. **访问应用**:
   - Portal 统一入口: http://localhost:5170
   - API Gateway: http://localhost:8000
   - System Backend: http://localhost:8080

详细步骤: [docs/addp部署和开发步骤.md](docs/addp部署和开发步骤.md)

### 模块选择启动（推荐用于开发）

**只启动需要的模块,加快开发速度,节省资源**:

```bash
# 只启动 System 模块 (5-8秒,节省85%资源)
bash scripts/dev/start.sh -system

# 启动 Manager 模块 + 依赖 (10-15秒)
bash scripts/dev/start.sh -manager

# 启动 Develop 模块 + GeoPandas (12-18秒)
bash scripts/dev/start.sh -develop

# 查看所有选项
bash scripts/dev/start.sh -h
```

**详细指南**: [docs/模块选择启动指南.md](docs/模块选择启动指南.md)

### 开发工作流（重要）

**如果你修改了某个模块的代码，使用选择性重启快速验证**：

```bash
# 场景 1: 修改了 Manager 模块
bash scripts/dev/restart.sh -manager

# 场景 2: 修改了 Meta 和 Transfer 模块
bash scripts/dev/restart.sh -meta -transfer

# 场景 3: 修改了 System 模块
bash scripts/dev/restart.sh -system

# 场景 4: 修改了多个模块或不确定影响范围
bash scripts/dev/restart.sh -all

# 场景 5: 只重启服务，不重新编译（最快）
bash scripts/dev/restart.sh
```

**支持的模块选项**：

- `-system` - System 模块
- `-manager` - Manager 模块
- `-meta` - Meta 模块
- `-transfer` - Transfer 模块
- `-orchestrator` - Orchestrator 模块
- `-develop` - Develop 模块
- `-service` - Service 模块
- `-gateway` - Gateway 模块
- `-all` - 所有模块

**当前处于开发阶段，优先采用 ./scripts/dev/ 下的脚本，有明确要求时再使用容器方式。**

## 模块端口

**核心端口**(高频使用):

- **Portal**: 5170 (dev) / 80 (prod via Nginx)
- **Gateway**: 8000
- **System Backend**: 8080
- **PostgreSQL**: 15432 (system)
- **Redis**: 16379
- **MinIO**: 19000-19001 (system) / 9002-9003 (business)

完整端口列表: docs/addp端口分配.md

## 技术栈

### 后端

- **语言**: Go 1.23+
- **HTTP 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15 (所有模块使用 schema 隔离: system、manager、metadata、transfer、orchestrator、develop)
- **缓存/队列**: Redis 7
- **对象存储**: MinIO (S3 兼容)
- **任务队列**: Asynq (基于 Redis,用于 Transfer 模块)、Cron (用于 Meta 模块调度)
- **空间计算**: GeoPandas Engine (基于 Python 的空间工作流执行引擎,内存 GeoDataFrame 处理)

### Go 依赖版本规范

为确保所有模块的依赖版本一致性,ADDP 平台使用统一的 Go 依赖版本(最后更新: 2025-12-15)。
需要详细技术栈信息时,请参考 docs/技术栈规约.md 文档。

### 基础设施

- **容器化**: Docker + Docker Compose
- **反向代理**: Nginx (生产环境)、Gateway 服务 (API 路由)
- **数据库 Schema 隔离**: PostgreSQL schemas (manager、metadata、transfer)
- **数据分离**: 系统基础设施 (ADDP 元数据) + 业务数据库 (用户数据) 独立部署

### 基础设施架构

ADDP 采用 **系统与业务数据分离** 架构设计:

**系统基础设施** (docker-compose.infra.yml):

- **Docker Compose 项目名**: `addp-infra`
- **容器命名**: 简单名称 (postgres、redis、minio、meilisearch),通过项目名管理隔离
- **postgres**: 存储 ADDP 系统元数据 (用户、引擎配置、元数据索引、任务定义等)
- **redis**: 缓存和任务队列 (Asynq)
- **minio**: 存储系统文件 (用户头像、系统配置、模块化 buckets)
- **meilisearch**: 全文搜索引擎 (元数据资产搜索、文件索引)

**业务数据库** (business/docker-compose.yml, 独立部署):

- `business-postgres`: 存储用户通过 ADDP 管理的实际业务数据 (用户上传的 PostgreSQL 数据等)
- `business-minio`: 存储用户上传的业务文件 (Shapefile、GeoJSON、图片、视频等)

### 基于模块的资源隔离

ADDP 采用 **模块化资源隔离** 策略,确保模块资源独立管理:
**PostgreSQL Schema 隔离**: 按模块名隔离
**MinIO Bucket 隔离**: 按模块名隔离
**Redis Key 命名规范**: {module}:{middleware}:{function}:{id}
**Asynq Queue 命名规范**: {module}:{priority}
**Meilisearch Index 命名规范**: {module}:{entity_type}

## 关键架构模式

### 分层后端架构 (在 system/backend/)

Go 后端遵循清晰的分层方法:

```
cmd/server/main.go          → 应用入口
internal/api/               → HTTP handlers + 路由
internal/service/           → 业务逻辑层
internal/repository/        → 数据访问层 (GORM)
internal/models/            → 数据库模型 + DTOs
internal/middleware/        → 认证、日志中间件
pkg/utils/                  → 共享工具 (JWT、加密)
```

**数据流**: API Handler → Service → Repository → Database

### 前端架构 (Portal + 微服务模式)

**统一门户 + 独立模块前端**:

平台使用 **基于门户的架构** 提供统一入口:

```
portal/frontend/           → 统一门户入口 (端口 5170 dev / 8000 prod)
├── src/
│   ├── views/
│   │   ├── Portal.vue    → 主门户页面,包含模块卡片
│   │   └── Login.vue     → 集中登录
│   ├── api/auth.js       → 通过 System 后端认证
│   └── router/           → Portal 路由
│
│   Portal 通过 iframe 嵌入模块前端:
│   - 左侧边栏: 所有模块的统一导航
│   - 主区域: iframe 动态加载模块前端

system/frontend/           → System 模块 (端口 5173 dev / 8090 prod)
├── 可独立运行或嵌入 portal
├── 功能: 用户、日志、引擎
其他模块类似 system。
```

**两种访问模式**:

1. **统一门户模式** (推荐给用户):

   - 单一入口: http://localhost:5170 (dev) 或 http://localhost:8000 (prod)
   - 集成所有模块的导航
   - 模块前端加载在 portal 的 iframe 中
   - 一次登录访问所有模块
2. **独立模块模式** (用于独立部署):

   - 直接访问各模块前端
   - System: http://localhost:5173, Manager: http://localhost:5174
   - 每个模块有自己的登录
   - 适合独立部署单个模块

**前端关键原则**:

- Portal 提供统一的用户体验和一致的导航
- 模块前端保持独立,可单独部署
- 所有前端共享 JWT 认证模式 (token 存储在 localStorage)
- Portal 和模块可独立认证
- 生产环境中,所有请求通过 Gateway (8000) 路由

### 认证流程

JWT 认证模式: 用户登录 → 后端验证 → 返回 JWT → 前端存储 token → 请求携带 token → 后端验证 token

### 测试账号

**租户管理员**: `admin` / `123456` (管理默认租户的用户、引擎、数据)
**超级管理员**: `SuperAdmin` / `20251001#SuperAdmin` (系统级管理、租户管理)

详细说明和启用方法: system/CLAUDE.md

### 配置中心模式

需要详细内容时,请阅读 docs/addp配置介绍.md

端口分配

需要了解端口分配时，请阅读 docs/PORTS分配.md

### 新模块开发

开发新模块时,请阅读: docs/新模块开发指南.md

## 重要文件位置

### 配置文件

- [`.env`](.env) - 根环境变量 (共享配置)
- [`.env.example`](.env.example) - 包含所有可用选项的模板
- [`docker-compose.yml`](docker-compose.yml) - 服务定义和网络配置

### 文档

- [`CLAUDE.md`](CLAUDE.md) - 本文件 (平台级架构)
- [`system/CLAUDE.md`](system/CLAUDE.md) - System 模块详情
- [`gateway/ARCHITECTURE.md`](gateway/ARCHITECTURE.md) - Gateway 路由逻辑
- [`docs/CONFIG_CENTER.md`](docs/CONFIG_CENTER.md) - 配置中心指南
- [`docs/COMMON_MODULE.md`](docs/COMMON_MODULE.md) - Common 模块使用
- [`common-frontend/README.md`](common-frontend/README.md) - Common 前端组件指南
- [`common-frontend/ARCHITECTURE.md`](common-frontend/ARCHITECTURE.md) - Common 前端架构
- 各模块目录中的 DATA_STRUCTURES.md

### 构建和部署

需要时请阅读 docs/addp部署和开发步骤.md

### 关键源文件

- System 认证: [system/backend/internal/middleware/auth.go](system/backend/internal/middleware/auth.go)
- Manager 预览: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Meta 扫描: [meta/backend/internal/service/scan_service.go](meta/backend/internal/service/scan_service.go)
- Common 客户端: [common/client/system.go](common/client/system.go)
- Common 前端基础: [common-frontend/basic/src/index.js](common-frontend/basic/src/index.js)
- Common 前端地图: [common-frontend/map/src/index.js](common-frontend/map/src/index.js)

## 数据库插件系统

ADDP 平台采用插件化架构支持多种数据库类型，当前支持 **8 种**数据库/存储引擎（PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、Spark SQL、MinIO、S3）。

**相关文档**：
- **架构说明**：[docs/数据库插件系统.md](docs/数据库插件系统.md) - 了解插件系统的整体架构和设计
- **新增指南**：[docs/addp新增存储引擎指南.md](docs/addp新增存储引擎指南.md) - 如何添加新的数据库/存储引擎类型

## 故障排查

**JWT token 问题**: 确保 `.env` 中的 `JWT_SECRET` 在各服务间匹配 (System 和 Gateway 等所有模块，均需要相同的密钥)
**跨服务调用失败**: 验证 `ENABLE_SERVICE_INTEGRATION=true` 且 docker-compose.yml 中的服务 URL 正确

更多故障排查，可翻阅 [docs/常见故障排查.md](docs/常见故障排查.md) 文档。
如果一个问题需要反复修改才能改好，或者以后可能反复遇到，你就应主动向用户提出把问题根源和修复思路记录到[docs/常见故障排查.md]中。

各模块的日志都统一输出到 addp/logs目录下，按照模块和前后端区分不同的日志文件。

**开发状态，故障修改后，请使用 `./scripts/dev/restart.sh -<模块名>` 重启对应服务来验证确认结果，没有经过这一步，不要告知我已经改好了。**
