# CLAUDE-CN.md

本文件为 Claude Code (claude.ai/code) 在处理此仓库代码时提供指导。

## 交流语言 / Communication Language

**重要**: 在此项目中,**请尽可能使用中文与用户交流**。除非用户明确使用英文提问,否则默认使用中文回复。

**Important**: In this project, **please communicate with users in Chinese as much as possible**. Use English only when the user explicitly asks questions in English.

## 开发原则

**重要**: ADDP 当前处于活跃开发阶段。以下原则指导所有开发工作:

### 1. 无需考虑向后兼容性

开发阶段优先考虑最佳架构和功能实现,无需考虑向后兼容或数据迁移:

- ✅ 根据需要自由修改数据库结构
- ✅ 改进 API 设计时可修改接口结构
- ✅ 为更好的模式重构代码
- ⚠️ 此原则仅适用于开发阶段

### 2. 保持简洁

除非有明确要求或许可,否则不要在 ADDP 工程目录下保存脚本和文档:

- ✅ 将临时性的计划和脚本放在 `/tmp/` 或你自己管理的目录下
- ✅ 保持项目目录树简洁,专注于生产代码
- ✅ 必要的文档优先保存到docs或者对应的模块目录下
- ✅ 必要的脚本保存到scripts对应的目录下
- ❌ 避免将一次性测试脚本或调试文档添加到仓库中

### 3. 无需征得同意

一旦开启"自动编辑"模式,你可以随时调用任何脚本、命令,无需得到用户同意:

- ✅ 自由运行构建、测试和部署脚本
- ✅ 执行数据库迁移和清理操作
- ✅ 在进行常规更改时信任你的判断

### 4. 全面考虑

对于任何设计和改动,都要考虑周全:

- ✅ 保持各个模块的一致性
- ✅ 同时更新关联功能代码、脚本和文档
- ✅ 考虑上下游影响和依赖关系
- ❌ 绝不孤立地修改某一部分而不考虑相关组件

### 5. 彻底解决根本原因

对于任何问题,要全面分析并解决根本原因:

- ✅ 调查问题背后的根本原因
- ✅ 彻底解决问题,不留隐患
- ❌ 绝不采用只解决表面症状的临时修复
- ❌ 不要头疼医头脚疼医脚,而要理解为什么会出现问题

### 6. 敢于删除

毫不犹豫地删除不需要的文件和代码:

- ✅ 自信地删除过时的代码
- ✅ 移除未使用的文件和依赖
- ❌ 不要因为"以防万一"或"兼容性"而保留
- ⚠️ 保留死代码会造成未来的混淆和维护负担

### 7. 敢于质疑

有权质疑任何不合理的要求:

- ✅ 指出设计或实现方面的顾虑
- ✅ 进行平等和相互尊重的讨论
- ✅ 在开始实施前达成共识
- 💡 我们是合作伙伴,共同构建最佳解决方案

## 仓库结构

**ADDP (All Domain Data Platform / 全域数据平台)** 是一个企业级数据平台,采用微服务架构。每个服务都有自己的目录:

- **common/** - 后端共享库: 通用客户端代码、模型、配置加载器和所有后端服务使用的工具
- **common-frontend/** - 前端共享库: Vue 3 组件、工具和类型定义,供前端复用
  - **basic/** - 基础 UI 组件,无地图依赖 (StorageEngineForm、ImagePreview、格式化器)
  - **map/** - 地图相关组件,需要 OpenLayers 和高德地图 (GeoJsonPreview、ShapefilePreview、TablePreview)
- **portal/** - 统一门户入口,基于 iframe 的模块集成 - **已实现**
- **system/** - 核心系统模块: 用户认证、日志、资源管理 - **已实现** (PostgreSQL system schema)
- **gateway/** - API 网关: 处理外部请求并路由到内部服务 - **已实现** (反向代理)
- **manager/** - 数据管理: 数据源连接、上传目录组织、数据预览 - **已实现**
- **meta/** - 元数据服务: 数据元数据解析/存储/查询,使用 cron 调度的模式扫描 - **已实现** (PostgreSQL metadata schema)
- **transfer/** - 数据传输: 数据导入/导出/同步 - **已实现**
- **orchestrator/** - 工作流编排: 任务调度和执行 - **已实现**
- **develop/** - 开发工作台: SQL 执行、GIS 工作流管理 - **已实现**
- **geopandas-engine/** - 空间计算引擎: 基于 Python 的 GIS 工作流执行,提供 21 个空间算子 - **已实现**

所有服务遵循相同的架构模式,使用共享的基础设施 (PostgreSQL、Redis、MinIO、Meilisearch)。通过 `common` 模块(后端)和 `common-frontend` 模块(前端)共享通用代码,避免重复。

## 快速开始

### 前提条件

**需要 Docker 环境**: ADDP 平台需要安装 Docker 和 Docker Compose。

- 安装 Docker Desktop: https://www.docker.com/products/docker-desktop
- 验证安装: `docker --version` 和 `docker-compose --version`

### 第一步: 启动基础设施 (必须首先执行)

**重要**: 必须先启动基础设施服务 (PostgreSQL、Redis、MinIO、Meilisearch)。

```bash
# 从项目根目录
bash scripts/infra/up.sh
```

此脚本自动完成:
- 启动 PostgreSQL (addp-postgres)、Redis (addp-redis)、MinIO (addp-minio)、Meilisearch (addp-meilisearch) 容器
- 初始化所有模块的 PostgreSQL schemas
- 初始化 MinIO buckets 和 Redis 配置
- 配置 Meilisearch 索引

检查基础设施状态:
```bash
bash scripts/infra/status.sh
```

### 第二步: 开发模式 (推荐用于开发)

**按正确顺序启动所有服务**,使用自动化开发脚本:

```bash
# 从项目根目录
bash scripts/dev/start.sh
```

自动启动以下内容:
1. 基础设施 (如未运行)
2. 所有后端服务 (System、Manager、Meta、Transfer、Orchestrator、Develop、GeoPandas Engine)
3. Gateway 服务
4. 所有前端服务 (可选,提示用户)

停止所有服务:
```bash
bash scripts/dev/stop.sh
```

代码修改后重启:
```bash
bash scripts/dev/restart.sh
```

### 第三步: 构建模式 (用于 Docker 镜像构建)

```bash
# 编译 Go 二进制文件
bash scripts/build/compile.sh

# 构建 Docker 镜像
bash scripts/build/build-images.sh

# 打包并推送镜像 (如需要)
bash scripts/build/package.sh
```

### 第四步: 本地 Docker Compose 模式

```bash
# 通过 Docker Compose 启动完整平台
bash scripts/local/start.sh

# 检查状态
bash scripts/local/status.sh

# 停止所有服务
bash scripts/local/stop.sh
```

### 第五步: 生产模式

**一键生产部署**:

```bash
# 从项目根目录
bash scripts/prod/start.sh
```

**部署流程** (自动执行):

1. **启动基础设施** (PostgreSQL、Redis、MinIO、Meilisearch)
2. **启动 System Backend** (其他服务依赖它)
3. **启动业务后端** (Manager、Meta、Transfer、Orchestrator、Develop、GeoPandas Engine、Gateway)
4. **启动前端服务** (所有模块前端 + Portal + Nginx)
5. **健康检查** (验证所有服务就绪)

**访问地址** (部署完成后):

- **✨ Portal 统一入口 (推荐)**: http://localhost:80
  - 统一登录,一键访问所有模块
  - 通过 Nginx 反向代理,提供最佳用户体验
- **Portal 独立访问** (开发调试): http://localhost:5170
- **API Gateway**: http://localhost:8000
- **独立模块访问** (如需单独访问):
  - System: http://localhost:8090
  - Manager: http://localhost:8091
  - Meta: http://localhost:8092
  - Transfer: http://localhost:8093
  - Orchestrator: http://localhost:8094
  - Develop: http://localhost:8095

**System 模块详细文档请参阅 `system/CLAUDE.md`。**

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

为确保所有模块依赖版本一致，ADDP 平台使用以下统一的 Go 依赖版本（最后更新: 2025-12-15）：

#### 核心框架
- **Gin 框架**: `github.com/gin-gonic/gin@v1.11.0`
- **GORM**: `gorm.io/gorm@v1.31.1`
- **PostgreSQL 驱动**: `gorm.io/driver/postgres@v1.6.0`
- **PostgreSQL 客户端**: `github.com/lib/pq@v1.10.9`
- **PostgreSQL 连接池**: `github.com/jackc/pgx/v5@v5.7.2`

#### 认证与加密
- **JWT**: `github.com/golang-jwt/jwt/v5@v5.3.0`
- **密码学**: `golang.org/x/crypto@v0.43.0`

#### 数据库驱动
- **MySQL**: `github.com/go-sql-driver/mysql@v1.9.3`

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

### 基础设施

- **容器化**: Docker + Docker Compose
- **反向代理**: Nginx (生产环境), Gateway 服务 (API 路由)
- **数据库 Schema 隔离**: PostgreSQL schemas (manager, metadata, transfer)
- **数据分离**: 系统基础设施 (ADDP 元数据) + 业务库 (用户数据) 独立部署

### 基础设施架构

ADDP 采用**系统与业务数据分离**的架构设计:

```
┌─────────────────────────────────────────────────────────────┐
│  ADDP 系统基础设施 (docker-compose.infra.yml)               │
│  项目名: addp-infra                                          │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ postgres     │  │ redis        │  │ minio        │     │
│  │ (系统元数据) │  │ (缓存/队列)  │  │ (系统文件)   │     │
│  │ Port: 5432   │  │ Port: 6379   │  │ Port: 9000-1 │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
│                    ┌──────────────┐                        │
│                    │ meilisearch  │                        │
│                    │ (全文检索)   │                        │
│                    │ Port: 7700   │                        │
│                    └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  ADDP 应用层 (docker-compose.yml)                           │
│  项目名: addp-app                                            │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ system-      │  │ manager-     │  │ meta-        │     │
│  │ backend      │  │ backend      │  │ backend      │     │
│  │ Port: 8080   │  │ Port: 8081   │  │ Port: 8082   │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
│  ... 其他服务 (Transfer, Orchestrator, Develop, 前端等) ... │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  业务库 (business/docker-compose.yml)                       │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐                        │
│  │ postgres     │  │ minio        │                        │
│  │ (业务数据库) │  │ (业务文件)   │                        │
│  │ Port: 5433   │  │ Port: 9002-3 │                        │
│  └──────────────┘  └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

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

**分离优势**:

- ✅ 数据隔离: 系统数据与业务数据物理分离
- ✅ 独立扩展: 业务数据量增长时可单独扩展
- ✅ 安全性: 业务数据库可配置更严格的访问控制
- ✅ 可替换性: 业务库可替换为云服务 (RDS、OSS)
- ✅ 端口隔离: 避免端口冲突,支持本地服务和容器服务并存

**MinIO 端口分配**(严格固定):

- **系统 MinIO**(ADDP 平台系统文件):
  - API 端口: `9000`
  - Console 端口: `9001`
  - 用途: 存储系统文件(用户头像、系统配置、MVT 瓦片、模块化 buckets)
  - 配置来源: `docker-compose.infra.yml`

- **业务 MinIO**(用户业务数据文件):
  - API 端口: `9002`
  - Console 端口: `9003`
  - 用途: 存储用户上传的业务文件(Shapefile、GeoJSON、图片、视频等)
  - 配置来源: `business/docker-compose.yml`

**端口冲突保护**:
- `scripts/infra/up.sh` 会检测 `business-minio` 是否占用了 9000/9001,若占用则报错退出
- 所有脚本和源代码中的硬编码端口均已统一为上述规范
- 详见: [docs/PORTS.md](docs/PORTS.md)

**基础设施管理**:
- 启动: `bash scripts/infra/up.sh` (自动完成所有初始化)
- 状态: `bash scripts/infra/status.sh`
- 停止: `bash scripts/infra/down.sh`
- 详见: [scripts/infra/README.md](scripts/infra/README.md)

详见 [business/README.md](business/README.md)

### 基于模块的资源隔离

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

manager/frontend/          → Manager 模块 (port 5174 dev / 8091 prod)
├── 独立或嵌入在 portal 中
├── 功能: 数据源、目录、预览

meta/frontend/             → Meta 模块 (port 5175 dev / 8092 prod)
├── 独立或嵌入在 portal 中
├── 功能: 元数据扫描、数据血缘

transfer/frontend/         → Transfer 模块 (port 5176 dev / 8093 prod)
├── 独立或嵌入在 portal 中
├── 功能: 数据导入/导出、任务调度

orchestrator/frontend/     → Orchestrator 模块 (port 5177 dev / 8094 prod)
├── 独立或嵌入在 portal 中
├── 功能: 工作流设计、任务编排

develop/frontend/          → Develop 模块 (port 5178 dev / 8095 prod)
├── 独立或嵌入在 portal 中
├── 功能: SQL 工作台、GIS 工作流编辑器
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

### 认证流程

1. 用户提交凭据到 `POST /api/auth/login`
2. 后端使用 bcrypt 验证,返回 JWT (HS256,使用 `JWT_SECRET` 签名)
3. 前端将 token 存储在 localStorage (`auth.js` Pinia store)
4. Axios 拦截器 (`api/client.js`) 在所有请求中添加 `Authorization: Bearer <token>`
5. 后端 `AuthMiddleware` 验证 JWT 并将用户上下文注入 Gin context
6. 受保护的路由通过 `c.Get("user_id")` 访问用户

### 前端认证标准规范

**重要**: 所有模块前端必须遵循以下标准化认证模式,确保一致性并避免常见错误。

#### 1. 认证 API (`api/auth.js`)

**所有模块必须使用独立的 System 客户端进行认证**,而不是模块自己的 API 客户端:

```javascript
import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// ✅ 正确: 创建专用的 System 客户端用于认证
const systemClient = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8080/api' : '/api',
  timeout: 10000
})

// 基础模块 (Meta, Transfer, Orchestrator, Develop)
export const authAPI = createAuthAPI(systemClient)

// 带注册功能的模块 (Manager, System, Portal)
export const authAPI = createAuthAPI(systemClient, {
  includeRegister: true
})

// System 和 Portal (向后兼容)
export const authAPI = {
  ...createAuthAPI(systemClient, {
    includeRegister: true
  }),
  getMe: () => systemClient.get('/users/me')
}
```

**为什么需要独立客户端?**
- 认证请求必须发送到 System 后端 (端口 8080)
- 模块自己的 `client` 指向自己的后端 (例如 Meta → 8082)
- 混用会导致登录时出现 404 错误

**常见错误避免:**
- ❌ `import client from './client'` - 错误! 这指向模块后端
- ❌ 使用模块的 client 进行认证 - 登录会 404 失败
- ✅ 始终创建独立的 `systemClient` 用于认证

#### 2. API 客户端 (`api/client.js`)

**所有模块必须使用 `createAPIClient()` 工厂函数** 以获得一致的拦截器和错误处理:

```javascript
import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

// 标准配置 (Meta, Transfer, Orchestrator)
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Meta'
})

// 自定义超时 (Develop - SQL 查询需要 5 分钟)
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Develop',
  timeout: 300000
})

// 短超时 (Manager, System, Portal)
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Manager',
  timeout: 10000
})

export default client
```

**`createAPIClient()` 提供的功能:**
- 通过请求拦截器自动注入 JWT token
- 401 错误时自动刷新 token
- 自动提取 `response.data`
- 所有模块的错误处理一致
- 开发/生产环境自动处理

**常见错误避免:**
- ❌ 手动创建 axios 实例并自定义拦截器
- ❌ 在各模块中重复拦截器逻辑
- ❌ 忘记提取 `response.data`
- ✅ 始终使用 `createAPIClient()` 工厂函数

#### 3. 认证 Store (`store/auth.js` 或 `stores/auth.js`)

**所有模块必须使用 `createAuthStore()` 工厂函数** 以避免 getter 覆盖 bug:

```javascript
import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

// 标准配置 (所有模块)
export const useAuthStore = defineStore('meta-auth',
  createAuthStore('meta-auth', authAPI, {
    persistUser: true  // 所有模块必须设为 true
  })
)

// 带自定义 getters (Develop 模块示例)
export const useAuthStore = defineStore('develop-auth',
  createAuthStore('develop-auth', authAPI, {
    persistUser: true,
    extraGetters: {
      username: (state) => state.user?.username || ''
    }
  })
)
```

**关键规则:**
- ✅ **必须使用 `persistUser: true`** - 在 localStorage 中缓存用户信息
- ✅ **永远不要手动合并 getters** - 使用 `extraGetters` 选项
- ❌ **永远不要覆盖基础 getters** - 会导致 `isAuthenticated: undefined` bug

**为什么 `persistUser: true` 是必需的:**
- 没有它,页面刷新后用户信息会丢失
- 每次刷新都会额外调用 `/users/me` 请求
- 用户体验差 (加载状态、闪烁)
- 所有模块必须保持一致的行为

**getter 覆盖 bug (永远不要这样做):**
```javascript
// ❌ 错误 - 这会覆盖所有基础 getters 包括 isAuthenticated
export const useAuthStore = defineStore('develop-auth', {
  ...createAuthStoreConfig('develop-auth', authAPI),
  getters: {
    username: (state) => state.user?.username || ''  // 覆盖了 isAuthenticated!
  }
})

// ✅ 正确 - 使用 createAuthStore 和 extraGetters
export const useAuthStore = defineStore('develop-auth',
  createAuthStore('develop-auth', authAPI, {
    persistUser: true,
    extraGetters: {
      username: (state) => state.user?.username || ''
    }
  })
)
```

#### 4. 路由守卫 (`router/index.js`)

**所有模块必须使用 `createAuthGuard()` 工厂函数**:

```javascript
import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/meta/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Meta',
  loginRouteName: 'Login'
}))

export default router
```

**`createAuthGuard()` 处理的内容:**
- Token 验证和用户加载
- 登录页重定向
- Query token 处理 (Portal iframe 模式)
- 开发/生产环境路径规范化

#### 5. 模块命名规范

**Store 名称必须使用模块前缀:**
```javascript
// ✅ 正确的命名
'develop-auth'    // Develop 模块
'meta-auth'       // Meta 模块
'manager-auth'    // Manager 模块
'transfer-auth'   // Transfer 模块
'orchestrator-auth' // Orchestrator 模块
'system-auth'     // System 模块
'portal-auth'     // Portal 模块
```

**为什么这很重要:**
- 防止 Pinia store 名称冲突
- 调试更容易 (清晰的模块归属)
- 与 localStorage key 命名保持一致

#### 6. 总结检查清单

创建或更新模块前端时,确保:

- [ ] `api/auth.js` 使用独立的 `systemClient` (不是模块的 client)
- [ ] `api/client.js` 使用 `createAPIClient()` 工厂函数
- [ ] `store/auth.js` 使用 `createAuthStore()` 工厂函数
- [ ] 所有模块都设置 `persistUser: true`
- [ ] Router 使用 `createAuthGuard()` 工厂函数
- [ ] Store 名称遵循 `{module}-auth` 约定
- [ ] 没有手动的拦截器代码 (使用工厂函数)
- [ ] 没有手动的 getter 合并 (使用 `extraGetters`)

**详细实现请参考:**
- [common-frontend/basic/src/composables/useAuth.js](common-frontend/basic/src/composables/useAuth.js) - 工厂函数
- [develop/auth.md](develop/auth.md) - 常见认证 bug 和解决方案
- 任意模块的 `api/`, `store/`, `router/` 目录作为参考实现

### 数据库架构

所有模块使用 **PostgreSQL 15**,通过 schema 隔离实现数据分离。

**System 模块 (system schema)**:

- **users** - 用户账户,使用 bcrypt 哈希密码
- **tenants** - 多租户的租户信息
- **audit_logs** - 自动记录所有非 GET 操作 (通过 `LoggerMiddleware`)
- **resources** - 灵活的资源配置,带有 JSON connection_info 字段 (加密)

**Manager 模块 (manager schema)**:

- **data_sources** - 数据源连接和配置
- **directories** - 文件组织和层级结构
- **permissions** - 数据源和目录的访问控制

**Meta 模块 (metadata schema)** - 已实现:

- **meta_node** - 层级节点 (schemas, prefixes, collections),递归结构,通过 `res_id` 引用 `system.resources`
- **meta_item** - 叶子项 (tables, objects, files),带有 JSON attributes
- **meta_dictionary** - 节点类型和子规则定义,用于验证
- **meta_change_log** - 变更跟踪,用于审计和增量同步 (规划中)

注意: Meta 模块使用**统一的层级模型** (resource → node → item) 来支持关系数据库和对象存储,替换了旧的特定类型表 (schemas, tables, fields)

**Transfer 模块 (transfer schema)** - 规划中:

- **tasks** - 传输任务定义
- **task_executions** - 任务执行历史
- **data_mappings** - 字段映射配置

GORM AutoMigrate 在启动时自动处理 schema 更新。PostgreSQL schemas 通过 `scripts/init-db.sql` 初始化。

### 配置中心模式

**System 作为单一真实来源**:

平台实现了集中式配置管理模式,其中 **System 模块充当所有其他模块的配置中心**。

**架构**:

```
┌────────────────────────────────────────────────────┐
│   System 模块 (配置中心)                           │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /internal/config API                   │    │
│   │  返回: JWT_SECRET, DB 连接,             │    │
│   │          ENCRYPTION_KEY                 │    │
│   └─────────────────────────────────────────┘    │
│                                                    │
│   ┌─────────────────────────────────────────┐    │
│   │  /api/resources (业务数据库配置)        │    │
│   │  管理所有数据源配置                     │    │
│   │  (加密存储)                              │    │
│   └─────────────────────────────────────────┘    │
└────────────────┬───────────────────────────────────┘
                 │
      ┌──────────┼──────────┼──────────┼──────────┼──────────┼──────────┐
      ▼          ▼          ▼          ▼          ▼          ▼          ▼
  ┌────────┐ ┌────────┐ ┌─────────┐ ┌───────────┐ ┌────────┐ ┌─────────┐
  │ Manager│ │  Meta  │ │Transfer │ │Orchestrator│ │ Develop│ │GeoPandas│
  │        │ │        │ │         │ │           │ │        │ │ Engine  │
  │ 启动时 │ │ 启动时 │ │ 启动时  │ │ 启动时    │ │ 启动时 │ │ 启动时  │
  │ ↓      │ │ ↓      │ │ ↓       │ │ ↓         │ │ ↓      │ │ ↓       │
  │ 获取   │ │ 获取   │ │ 获取    │ │ 获取      │ │ 获取   │ │ 获取    │
  │ 配置   │ │ 配置   │ │ 配置    │ │ 配置      │ │ 配置   │ │ 配置    │
  └────────┘ └────────┘ └─────────┘ └───────────┘ └────────┘ └─────────┘
```

**集中化的内容**:

1. **认证**: `JWT_SECRET` - 确保所有服务使用相同的 JWT 签名密钥
2. **系统数据库**: PostgreSQL 连接信息 - 系统数据的单一来源
3. **业务数据库**: System 的 `resources` 表中管理的资源 - 所有数据源配置
4. **加密密钥**: `ENCRYPTION_KEY` - 跨服务的一致加密

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

- ✅ **单一真实来源**: 修改数据库密码一次,重启服务即可应用
- ✅ **安全性**: 敏感配置集中管理和加密
- ✅ **灵活性**: 支持集成和独立部署模式
- ✅ **可维护性**: 减少配置重复,更易于审计

**SystemClient 使用**:

所有模块使用 `SystemClient` 从 System 获取业务数据库配置:

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

每个模块只需配置模块特定的设置:

```bash
# Manager/Meta/Transfer .env
PORT=8081                          # 模块特定端口
DB_SCHEMA=manager                  # 模块特定 schema
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true    # 启用配置中心

# 共享配置 (JWT_SECRET, DB 连接) 从 System 获取
# 回退配置已注释 (仅在集成禁用时使用)
```

**另请参阅**: `docs/CONFIG_CENTER.md` 获取详细的配置中心使用指南。

## Common 模块

`common` 模块提供共享代码,避免**所有其他后端模块**之间的重复 (Manager、Meta、Transfer、Orchestrator、Develop 和 GeoPandas Engine 集成)。

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
5. **在 `internal/api/router.go` 中注册路由**:

   ```go
   protected.POST("/resources", resourceHandler.Create)
   ```

**示例 PR**: 参见 system 模块资源管理实现

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

### 端口分配

**ADDP 系统服务**:

| 服务              | 开发端口 | Docker 端口 | 说明                   |
| -------------------- | -------- | ----------- | ----------------------------- |
| **Nginx Gateway**    | **80**   | **80**      | **统一入口 (推荐)** |
| **Portal Frontend**  | **5170** | **5170**    | **Portal UI (通过 Nginx)**     |
| Gateway              | 8000     | 8000        | API Gateway (后端路由) |
| System Backend       | 8080     | 8080        | 认证、用户、日志             |
| System Frontend      | 5173     | 8090        | 独立访问             |
| Manager Backend      | 8081     | 8081        | 数据源、文件           |
| Manager Frontend     | 5174     | 8091        | 独立访问             |
| Meta Backend         | 8082     | 8082        | 元数据、血缘             |
| Meta Frontend        | 5175     | 8092        | 独立访问             |
| Transfer Backend     | 8083     | 8083        | 导入/导出任务           |
| Transfer Frontend    | 5176     | 8093        | 独立访问             |
| Orchestrator Backend | 8084     | 8084        | 工作流编排        |
| Orchestrator Frontend| 5177     | 8094        | 独立访问             |
| Develop Backend      | 8085     | 8085        | 开发工具             |
| Develop Frontend     | 5178     | 8095        | 独立访问             |
| GeoPandas Engine     | 8099     | 8099        | 空间计算引擎 (Python) |
| PostgreSQL (System)  | 5432     | 5432        | ADDP 系统元数据          |
| Redis                | 6379     | 6379        | 缓存和队列                 |
| MinIO System API     | 9000     | 9000        | 系统文件存储           |
| MinIO System Console | 9001     | 9001        | 系统 MinIO Web UI           |
| Meilisearch          | 7700     | 7700        | 全文检索引擎       |

**业务库服务** (通过 `business/docker-compose.yml` 部署):

| 服务                | Docker 端口 | 说明                |
| ---------------------- | ----------- | -------------------------- |
| PostgreSQL (Business)  | 5433        | 用户业务数据存储 |
| MinIO Business API     | 9002        | 用户文件存储          |
| MinIO Business Console | 9003        | 业务 MinIO Web UI      |

**推荐访问**:
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
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "SuperAdmin", "password": "20251001#SuperAdmin"}'

# 使用租户管理员登录
curl -X POST http://localhost:8080/api/auth/login \
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

### 编写测试

Go 测试应该是表驱动的,放在 `_test.go` 文件中:

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

前端测试尚未实现。添加 Vue 组件时,在 PR 描述中记录手动测试场景。

## Docker 部署

### 业务库 (先部署)

业务库需要**先于 ADDP 系统启动**,因为 Manager 和 Meta 模块会连接到业务存储:

```bash
# 1. 先部署业务库
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
make up              # 启动 System 后端 + 前端
make logs-system     # 查看日志
make down           # 停止服务
```

### 完整平台

```bash
# 从项目根目录 (确保业务库先运行)
make up-full        # 使用 --profile full 启动所有服务
make status         # 检查所有服务状态
make logs           # 查看所有日志
make down           # 停止所有服务

# 单个服务日志
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

### Docker Swarm 模式 (高可用)

对于需要高可用性的生产环境,使用 Docker Swarm 模式而不是标准 Compose:

```bash
# 1. 初始化 Swarm (一次性设置)
docker swarm init

# 2. 部署到 Swarm
docker stack deploy -c docker-compose.yml addp

# 3. 验证服务
docker service ls
docker service ps addp_transfer-worker  # 应该显示 2 个副本

# 4. 查看日志
docker service logs -f addp_transfer-worker

# 5. 手动扩展 (如需要)
docker service scale addp_transfer-worker=3

# 6. 更新服务 (零停机)
docker service update --image addp-transfer-worker:v2.0 addp_transfer-worker

# 7. 停止服务
docker stack rm addp
```

**Swarm 模式的关键优势**:

- ✅ **自动恢复**: 崩溃的容器自动替换为新副本
- ✅ **负载均衡**: 跨副本的内置负载均衡
- ✅ **零停机更新**: 滚动更新不中断服务
- ✅ **资源管理**: CPU 和内存限制/预留

**Transfer Worker 高可用** (在 docker-compose.yml 中配置):

- 默认: 2 个副本同时运行
- 失败时自动重启
- CPU 限制: 每个 worker 2 核
- 内存限制: 每个 worker 2GB

详细的 Swarm 部署指南参阅 [docs/DOCKER_SWARM.md](docs/DOCKER_SWARM.md)。

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
- `POST/GET/PUT/DELETE /api/resources` - 资源 CRUD (支持 `?resource_type=X` 过滤)

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
- 连接到 System 模块进行资源管理
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

1. System 在创建/更新资源时向 Redis 发布资源变更事件
2. Meta 订阅这些事件并检查 `ScanConfig.ScheduleType`
3. 如果 `ScheduleType == "immediate"`, Meta 自动创建并入队扫描任务
4. 无循环依赖: System → Redis Pub/Sub → Meta (单向通信)

**扫描工作流**:

1. 从 System 模块 `/api/resources` 同步数据源
2. 选择要扫描的数据源和 schemas/prefixes
3. 层级提取元数据:
   - 数据库: system.resources (database) → meta_node (schemas) → meta_item (tables,字段详情在 JSON 中)
   - 对象存储: system.resources (bucket scope) → meta_node (prefixes) → meta_item (objects,文件元数据)
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

### Transfer 服务 (规划中)

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

### GeoPandas Engine (已实现)

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
  - 仅注册引擎本身到 System (`geopandas.engine.default`)
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
- [geopandas-engine/workflow_engine.py](geopandas-engine/workflow_engine.py) - DAG执行引擎
- [geopandas-engine/operators.py](geopandas-engine/operators.py) - 21个空间算子
- [geopandas-engine/api_server.py](geopandas-engine/api_server.py) - Flask REST API
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

## 代码质量原则

### DRY (Don't Repeat Yourself / 不要重复自己)

**核心原则**: 避免跨模块的代码重复。将通用功能提取到 `common/` 模块。

**为什么重要**:

- ✅ 单点维护 - 修复一次错误,所有模块受益
- ✅ 一致性 - 所有模块使用相同的实现
- ✅ 减少错误风险 - 无需记住更新多个位置
- ✅ 更容易重构 - 在一处更改逻辑

**`common/` 中的共享代码示例**:

- **`common/config/LoadEnv(levelsUp int)`** - 从项目根目录加载 .env 文件
  ```go
  // 在每个模块的 main.go 中
  commonConfig.LoadEnv(4)  // system/backend/cmd/server (4 级向上)
  commonConfig.LoadEnv(3)  // gateway/cmd/gateway (3 级向上)
  ```
- **`common/client/SystemClient`** - 与 System 模块通信
- **`common/models/Resource`** - 共享的资源模型
- **`common/config/LoadSharedConfig()`** - 从 System 获取配置

**何时提取到 common/**:

1. 代码出现在 2+ 个模块中,变化很小
2. 逻辑与模块无关 (不特定于某个服务的业务域)
3. 函数可以参数化以处理差异 (例如路径深度)

**何时不提取**:

- 模块特定的业务逻辑
- 在模块间可能分化的代码
- 没有复用潜力的单次使用函数

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

**示例 PR**: 参见 .env 加载重构 - 将 `godotenv` 逻辑从 4 个重复的 main.go 文件提取到 `common/config/LoadEnv()`

## 新服务的开发指南

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

   - 使用 PostgreSQL schema 隔离 (所有模块使用 PostgreSQL 和专用 schemas)
   - 使用 GORM 作为 ORM,带 AutoMigrate
   - 将 schemas 添加到 `scripts/infra/init-postgresql.sql`
   - 使用 `updated_at` 触发器进行时间戳跟踪
3. **配置**:

   - 通过 `internal/config/config.go` 从环境变量读取
   - 支持开发和 Docker 部署模式
   - 为缺失的环境变量设置默认值
4. **认证**:

   - 重用 System 模块的 JWT 验证逻辑
   - 从 System 导入 auth 中间件或创建相同的
   - 从 JWT 声明中提取 user_id 并传递给服务层
5. **Docker 集成**:

   - 在服务根目录创建 Dockerfile
   - 使用 `profile: full` 将服务添加到 `docker-compose.yml`
   - 使用健康检查进行依赖管理
   - 连接到 `addp-network` 进行服务间通信
6. **前端集成**:

   - 创建独立的 `<module>/frontend/` 目录
   - 从 `system/frontend/` 复制结构 (Vue 3 + Pinia + Element Plus)
   - 创建 `api/client.js` 指向模块后端
   - 创建 `api/auth.js` 指向 System 后端 (8080) 进行认证
   - 从 System 模块复制 auth store 模式 (独立副本,非共享)
   - 在 `vite.config.js` 中设置唯一的开发端口 (System: 5173, Manager: 5174 等)
   - 配置路由基础路径 (例如 Manager 模块的 `/manager/`)
   - 创建 Dockerfile 和 nginx.conf 用于生产部署
   - 使用唯一端口和 `profile: full` 添加到 docker-compose.yml

## 前端开发工作流

### 快速开始: Portal + 所有模块

```bash
# 终端 1: 启动 Portal (统一入口)
cd portal/frontend
npm install
npm run dev
# 访问: http://localhost:5170

# 终端 2: 启动 System 前端
cd system/frontend
npm install
npm run dev

# 终端 3: 启动 Manager 前端
cd manager/frontend
npm install
npm run dev

# 现在访问 http://localhost:5170 获得统一体验
# 通过单一 portal 界面访问所有模块
```

### 运行单个前端 (独立模式)

```bash
# System 前端 (端口 5173)
cd system/frontend
npm run dev
# 访问: http://localhost:5173

# Manager 前端 (端口 5174)
cd manager/frontend
npm run dev
# 访问: http://localhost:5174

# Portal (端口 5170)
cd portal/frontend
npm run dev
# 访问: http://localhost:5170
```

### 开发中的前端-后端连接

**开发模式** (直接后端连接):

- System 前端 → System 后端 (localhost:8080)
- Manager 前端 → Manager 后端 (localhost:8081)
- 认证请求 → System 后端 (localhost:8080)

**生产模式** (通过 Gateway):

- 所有前端请求 → Gateway (localhost:8000)
- Gateway 路由到适当的后端

### 创建新模块前端

实现新模块 (例如 Meta) 时,遵循以下步骤:

1. **复制前端结构**:

   ```bash
   cp -r system/frontend meta/frontend
   cd meta/frontend
   ```
2. **更新配置**:

   - `package.json`: 将名称更改为 `meta-frontend`
   - `vite.config.js`: 将端口更改为唯一数字 (例如 5175)
   - `index.html`: 更新标题
   - `src/router/index.js`: 将基础路径设置为 `/meta/`
   - `src/api/client.js`: 将 baseURL 指向 meta 后端 (8082)
   - 保持 `src/api/auth.js` 指向 System 后端 (8080)
3. **配置 common-frontend 别名** (根据模块需求选择):

   对于**无地图功能**的模块 (System, Meta, Transfer):
   ```javascript
   // vite.config.js
   resolve: {
     alias: {
       '@': resolve(__dirname, 'src'),
       '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
     }
   }
   ```

   对于**有地图功能**的模块 (Manager):
   ```javascript
   // vite.config.js
   resolve: {
     alias: {
       '@': resolve(__dirname, 'src'),
       '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
     }
   }

   // package.json - 添加地图依赖
   {
     "dependencies": {
       "ol": "^9.2.4",
       "@amap/amap-jsapi-loader": "^1.0.1"
     }
   }
   ```
4. **更新视图和组件**以匹配模块的功能
5. **添加 Dockerfile 和 nginx.conf** (从 manager/frontend 复制作为模板)
6. **添加到 docker-compose.yml**:

   ```yaml
   meta-frontend:
     build:
       context: ./meta/frontend
     ports:
       - "8092:80"
     profiles:
       - full
   ```

**使用 Common Frontend 组件**:

```vue
<script setup>
// 对于基础模块
import { StorageEngineForm, ImagePreview } from '@common-ui'
import { formatFileSize, FieldType } from '@common-ui'

// 对于启用地图的模块
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

## 常用 Make 命令 (项目根目录)

```bash
# 初始化
make init                # 创建配置文件和目录
make install-deps        # 安装 Go 和 npm 依赖

# 开发
make dev-start           # 按正确顺序启动所有服务 (推荐)
make dev-stop            # 停止所有开发服务
make dev-health          # 检查所有服务健康状态
make dev-system          # 在开发模式下运行 System
make dev-manager         # 运行 Manager 后端
make dev-gateway         # 运行 Gateway 服务

# Docker 操作
make up                  # 仅启动 System 模块
make up-full             # 启动所有服务 (完整平台)
make up-infra            # 仅启动 PostgreSQL, Redis, MinIO
make down                # 停止所有服务
make restart             # 重启 System 模块
make restart-full        # 重启所有服务

# 构建
make build               # 构建所有工件到 dist/
make docker-build        # 构建 System Docker 镜像
make docker-build-all    # 构建所有服务 Docker 镜像

# 监控
make status              # 显示所有服务状态和 URL
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

# 测试和质量
make test                # 运行所有测试
make test-system         # 运行 System 模块测试
make lint                # 运行代码检查器
make fmt                 # 格式化 Go 代码

# 清理
make clean               # 删除构建工件
make clean-all           # 删除所有数据和卷 (破坏性)
```

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

### 构建和部署

- [`Makefile`](Makefile) - 项目范围的编排命令
- [`scripts/`](scripts/) - 所有用于开发、构建和部署的自动化脚本
  - [`scripts/infra/`](scripts/infra/) - 基础设施管理 (PostgreSQL, Redis, MinIO, Meilisearch)
    - `up.sh` - 启动基础设施 + 自动初始化
    - `down.sh` - 停止基础设施
    - `status.sh` - 检查服务健康状态
    - `init-postgresql.sh` - 初始化数据库 schemas
    - `init-redis.sh` - 初始化 Redis 配置
    - `init-minio.sh` - 初始化 MinIO buckets
    - `init-meilisearch.sh` - 初始化 Meilisearch 索引
  - [`scripts/dev/`](scripts/dev/) - 开发模式脚本 (直接 Go/npm 进程)
    - `start.sh` - 启动完整开发环境 (基础设施 + 后端 + 前端)
    - `stop.sh` - 停止所有开发服务
    - `restart.sh` - 重启所有服务
    - `modtidy.sh` - 清理 Go 模块依赖
  - [`scripts/build/`](scripts/build/) - 编译和 Docker 镜像构建
    - `compile.sh` - 编译 Go 二进制文件 (go build)
    - `build-images.sh` - 构建 Docker 镜像 (docker build)
    - `package.sh` - 打包部署工件 (docker save/push)
    - `push-images.sh` - 推送镜像到仓库
    - `push-images-batched.sh` - 批量推送镜像 (并行上传)
  - [`scripts/local/`](scripts/local/) - 本地 Docker Compose 部署
    - `start.sh` - 通过 Docker Compose 启动完整平台
    - `stop.sh` - 停止 Docker 服务
    - `status.sh` - 查看容器状态和资源使用
  - [`scripts/prod/`](scripts/prod/) - 生产部署脚本
    - `start.sh` - 启动生产环境 (分步执行)
    - `stop.sh` - 停止生产服务
    - `health-check.sh` - 健康监控
    - `swarm/` - Docker Swarm 高可用部署
  - 完整脚本文档参阅 [`scripts/README.md`](scripts/README.md)

### 关键源文件

- System 认证: [system/backend/internal/middleware/auth.go](system/backend/internal/middleware/auth.go)
- Manager 预览: [manager/backend/internal/service/object_preview.go](manager/backend/internal/service/object_preview.go)
- Meta 扫描: [meta/backend/internal/service/scan_service.go](meta/backend/internal/service/scan_service.go)
- Common 客户端: [common/client/system.go](common/client/system.go)
- Common frontend basic: [common-frontend/basic/src/index.js](common-frontend/basic/src/index.js)
- Common frontend map: [common-frontend/map/src/index.js](common-frontend/map/src/index.js)

## 故障排除

**服务无法启动**:

```bash
make status              # 检查正在运行的内容
docker-compose ps        # 检查容器状态
make logs                # 检查错误
```

**端口冲突**:

```bash
lsof -i :8080            # 检查谁在使用端口 8080
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
curl http://localhost:9001   # 检查 MinIO 控制台
```

**JWT token 问题**: 确保 `.env` 中的 `JWT_SECRET` 在服务间匹配 (System 和 Gateway 需要相同的密钥)

**跨服务调用失败**: 验证 `ENABLE_SERVICE_INTEGRATION=true` 且 docker-compose.yml 中的服务 URL 正确
