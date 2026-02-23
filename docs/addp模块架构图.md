# ADDP 模块架构图

本文档使用可视化图表展示 ADDP 平台的模块组成、层次关系和依赖关系。

---

## 目录

1. [模块总览](#模块总览)
2. [模块详细说明](#模块详细说明)
3. [共享模块](#共享模块)
4. [Worker 运行时](#worker-运行时)
5. [Monitor 模块](#monitor-模块)
6. [Gateway 路由机制](#gateway-路由机制)
7. [ADDP 两种使用方式](#addp-两种使用方式)
8. [计算引擎与任务编排](#计算引擎与任务编排)
9. [模块启动顺序](#模块启动顺序)

---

## 模块总览

ADDP 平台采用微服务架构,由以下模块组成:

```mermaid
graph TB
    subgraph "前端层"
        Console[Console<br/>控制台<br/>:5170]
        SystemFE[System Frontend<br/>:5173]
        ManagerFE[Manager Frontend<br/>:5174]
        MetaFE[Meta Frontend<br/>:5175]
        TransferFE[Transfer Frontend<br/>:5176]
        OrchestratorFE[Orchestrator Frontend<br/>:5177]
        DevelopFE[Develop Frontend<br/>:5178]
        ServiceFE[Service Frontend<br/>:5179]
        MonitorFE[Monitor Frontend<br/>:5179]
    end

    subgraph "网关层"
        Gateway[Gateway<br/>API网关<br/>:8000]
    end

    subgraph "服务层"
        System[System Backend<br/>核心系统<br/>:8180]
        Manager[Manager Backend<br/>数据管理<br/>:8081]
        Meta[Meta Backend<br/>元数据服务<br/>:8082]
        Transfer[Transfer Backend<br/>数据传输<br/>:8083]
        Orchestrator[Orchestrator Backend<br/>任务编排<br/>:8084]
        Develop[Develop Backend<br/>数据开发<br/>:8085]
        Service[Service Backend<br/>数据服务<br/>:8086]
        Monitor[Monitor Backend<br/>执行监控<br/>:8100]
    end

    subgraph "Worker运行时"
        TransferWorker[Transfer Worker<br/>异步任务处理]
        MetaWorker[Meta Worker<br/>扫描任务处理]
        ManagerWorker[Manager Worker<br/>瓦片缓存生成]
    end

    subgraph "共享模块"
        Common[common<br/>后端共享库]
        CommonFE[common-frontend<br/>前端共享库]
    end

    subgraph "计算引擎"
        PyWorkflow[python_workflow<br/>Python工作流引擎]
        SparkWorkflow[spark_workflow<br/>Spark工作流引擎]
        Jupyter[jupyter<br/>Notebook引擎]
    end

    subgraph "基础设施层"
        PostgreSQL[(PostgreSQL<br/>系统数据库<br/>:15432)]
        Redis[(Redis<br/>缓存/队列<br/>:16379)]
        MinIO[(MinIO<br/>对象存储<br/>:19000)]
        Meilisearch[(Meilisearch<br/>全文搜索<br/>:17700)]
    end

    Console --> Gateway
    SystemFE -.-> Console
    ManagerFE -.-> Console
    MetaFE -.-> Console
    TransferFE -.-> Console
    OrchestratorFE -.-> Console
    DevelopFE -.-> Console
    ServiceFE -.-> Console
    MonitorFE -.-> Console

    Gateway --> System
    Gateway --> Manager
    Gateway --> Meta
    Gateway --> Transfer
    Gateway --> Orchestrator
    Gateway --> Develop
    Gateway --> Service
    Gateway --> Monitor

    System --> Common
    Manager --> Common
    Meta --> Common
    Transfer --> Common
    Orchestrator --> Common
    Develop --> Common
    Service --> Common
    Monitor --> Common

    SystemFE --> CommonFE
    ManagerFE --> CommonFE
    MetaFE --> CommonFE
    TransferFE --> CommonFE
    OrchestratorFE --> CommonFE
    DevelopFE --> CommonFE
    ServiceFE --> CommonFE

    Common --> PostgreSQL
    Common --> Redis
    Common --> MinIO
    Common --> Meilisearch

    Transfer --> TransferWorker
    Meta --> MetaWorker
    Manager --> ManagerWorker
    TransferWorker --> Redis
    MetaWorker --> Redis
    ManagerWorker --> Redis

    Develop --> PyWorkflow
    Develop --> SparkWorkflow
    Develop --> Jupyter

    classDef frontend fill:#e1f5ff,stroke:#01579b
    classDef gateway fill:#fff3e0,stroke:#e65100
    classDef backend fill:#f3e5f5,stroke:#4a148c
    classDef worker fill:#ffe0b2,stroke:#e65100
    classDef shared fill:#e8f5e9,stroke:#1b5e20
    classDef engine fill:#fff9c4,stroke:#f57f17
    classDef infra fill:#fce4ec,stroke:#880e4f

    class Console,SystemFE,ManagerFE,MetaFE,TransferFE,OrchestratorFE,DevelopFE,ServiceFE,MonitorFE frontend
    class Gateway gateway
    class System,Manager,Meta,Transfer,Orchestrator,Develop,Service,Monitor backend
    class TransferWorker,MetaWorker,ManagerWorker worker
    class Common,CommonFE shared
    class PyWorkflow,SparkWorkflow,Jupyter engine
    class PostgreSQL,Redis,MinIO,Meilisearch infra
```

**说明**:
- **前端层**: 各模块的独立前端应用,Console 通过 iframe 集成所有模块前端
- **网关层**: Gateway 统一处理外部请求并路由到对应的后端服务
- **服务层**: 各业务模块的后端服务,提供 RESTful API
- **Worker运行时**: 独立的后台任务处理进程
  - **Transfer Worker**: 基于 Asynq 的异步任务队列,处理数据导入/导出/同步任务
  - **Meta Worker**: 基于 Asynq 的扫描任务处理,执行元数据扫描和索引
  - **Manager Worker**: 基于 Asynq 的瓦片缓存生成,批量生成空间数据 Quick View 缓存
- **共享模块**: common 和 common-frontend 提供可复用的代码和组件
- **计算引擎**: engines 目录下的内置计算引擎,由 Develop 模块调用
- **基础设施层**: 共享的数据库、缓存、对象存储和搜索引擎

---

## 模块详细说明

| 模块 | 职责 | 端口 (开发/生产) | 主要技术栈 |
|------|------|-----------------|-----------|
| **Console** | 控制台入口,集成所有模块功能 | 5170 / 80 | Vue 3, Vue Router |
| **System** | 核心系统服务:用户认证、引擎管理、日志 | 8180 / 8180 | Go, Gin, GORM, JWT |
| **Gateway** | API 网关,请求路由和转发 | 8000 / 8000 | Go, Gin |
| **Manager** | 数据管理:数据存储目录展示、数据预览、MVT瓦片 | 8081 / 8081 | Go, Gin, OpenLayers |
| **Manager Worker** | Manager 瓦片缓存生成器 | - | Go, Asynq Worker |
| **Meta** | 元数据服务:扫描、索引、搜索 | 8082 / 8082 | Go, Gin, Meilisearch, Cron |
| **Meta Worker** | Meta 扫描任务处理器 | - | Go, Asynq Worker |
| **Transfer** | 数据传输:导入、导出、同步任务 | 8083 / 8083 | Go, Gin, Asynq |
| **Transfer Worker** | Transfer 后台任务处理器 | - | Go, Asynq Worker |
| **Orchestrator** | 任务编排:跨模块任务编排调度 | 8084 / 8084 | Go, Gin, Cron |
| **Develop** | 数据开发:查询执行、工作流、Notebook 开发 | 8085 / 8085 | Go, Gin, Monaco Editor |
| **Service** | 数据服务:服务发布(空间OGC标准与非空间)、外部服务注册 | 8086 / 8086 | Go, Gin, OGC 标准 |
| **Monitor** | 执行监控:统一监控所有模块的任务执行记录、统计分析 | 8100 / 8100 | Go, Gin, PostgreSQL |
| **Copilot** | AI 辅助助手：SQL 智能生成、工作流智能生成 | 8087 / 8087 | Python, FastAPI, LangChain |


---

## 共享模块

### common (后端共享库)

位于 `common/` 目录,所有后端服务依赖的共享代码:

```mermaid
graph LR
    Common[common 共享模块]

    Common --> Client[client/<br/>数据库客户端]
    Common --> Engine[engine/<br/>引擎插件系统]
    Common --> Models[models/<br/>数据模型]
    Common --> Config[config/<br/>配置加载器]
    Common --> Utils[utils/<br/>工具函数]

    Client --> PG[PostgreSQL]
    Client --> MySQL[MySQL]
    Client --> Mongo[MongoDB]
    Client --> MinIOClient[MinIO/S3]

    Engine --> Plugins[plugins/<br/>引擎插件实现]
    Engine --> Interfaces[interfaces.go<br/>插件接口定义]

    Models --> User[用户模型]
    Models --> EngineModel[引擎模型]
    Models --> Task[任务模型]

    Utils --> JWT[JWT工具]
    Utils --> Crypto[加密工具]
    Utils --> Logger[日志工具]

    classDef mainNode fill:#e8f5e9,stroke:#1b5e20,stroke-width:2px
    classDef subNode fill:#c8e6c9,stroke:#2e7d32

    class Common mainNode
    class Client,Engine,Models,Config,Utils subNode
```

**主要内容**:
- **client/**: 数据库客户端(PostgreSQL、MySQL、MongoDB、MinIO 等)
- **engine/**: 引擎插件系统(接口定义、插件实现、自动注册)
- **models/**: 通用数据模型(用户、引擎、任务等)
- **config/**: 配置加载器(环境变量、.env 文件)
- **utils/**: 工具函数(JWT、加密、日志等)

### common-frontend (前端共享库)

位于 `common-frontend/` 目录,前端模块复用的组件和工具:

```mermaid
graph LR
    CommonFE[common-frontend<br/>前端共享库]

    CommonFE --> Basic[basic/<br/>基础组件]
    CommonFE --> Map[map/<br/>地图组件]

    Basic --> Storage[StorageEngineForm<br/>数据源表单]
    Basic --> Image[ImagePreview<br/>图片预览]
    Basic --> Format[formatters<br/>格式化工具]
    Basic --> BasicOther[其他基础组件...]

    Map --> GeoJSON[GeoJsonPreview<br/>GeoJSON预览]
    Map --> Shapefile[ShapefilePreview<br/>Shapefile预览]
    Map --> Table[TablePreview<br/>表格预览]
    Map --> MapOther[依赖OpenLayers<br/>高德地图]

    classDef mainNode fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    classDef subNode fill:#b3e5fc,stroke:#0277bd

    class CommonFE mainNode
    class Basic,Map subNode
```

**模块划分**:
- **basic/**: 基础 UI 组件,无地图依赖
  - `StorageEngineForm`: 数据源表单组件
  - `ImagePreview`: 图片预览组件
  - `formatters`: 数据格式化工具
  - 其他通用 UI 组件
- **map/**: 地图相关组件,依赖 OpenLayers 和高德地图
  - `GeoJsonPreview`: GeoJSON 数据预览组件
  - `ShapefilePreview`: Shapefile 数据预览组件
  - `TablePreview`: 表格数据预览组件(带空间可视化)

**使用原则**:
- 各模块通过 `npm install` 引入 common-frontend
- common-frontend 自身不能有 `node_modules`(避免依赖冲突)
- 各模块需在 `package.json` 中添加 `overrides` 强制统一 Vue 版本

---

## Worker 运行时

ADDP 平台的部分模块拥有独立的 Worker 运行时进程,用于处理异步任务和后台作业:

```mermaid
graph TB
    subgraph "Transfer 模块"
        TB[Transfer Backend<br/>:8083]
        TW[Transfer Worker<br/>异步任务处理器]
        TB -.入队.-> Redis
        Redis -.消费.-> TW
    end

    subgraph "Meta 模块"
        MB[Meta Backend<br/>:8082]
        MW[Meta Worker<br/>扫描任务处理器]
        MB -.入队.-> Redis2[Redis]
        Redis2 -.消费.-> MW
    end

    subgraph "Manager 模块"
        MB2[Manager Backend<br/>:8081]
        MW2[Manager Worker<br/>瓦片缓存生成器]
        MB2 -.入队.-> Redis3[Redis]
        Redis3 -.消费.-> MW2
    end

    subgraph "任务队列 (Asynq)"
        Redis[(Redis<br/>任务队列)]
        Redis2[(Redis<br/>任务队列)]
        Redis3[(Redis<br/>任务队列)]
    end

    TB --> PostgreSQL[(PostgreSQL)]
    TW --> PostgreSQL
    MB --> PostgreSQL2[(PostgreSQL)]
    MW --> PostgreSQL2
    MB2 --> PostgreSQL3[(PostgreSQL)]
    MW2 --> PostgreSQL3

    classDef backend fill:#f3e5f5,stroke:#4a148c
    classDef worker fill:#ffe0b2,stroke:#e65100,stroke-width:2px
    classDef infra fill:#fce4ec,stroke:#880e4f

    class TB,MB,MB2 backend
    class TW,MW,MW2 worker
    class Redis,Redis2,Redis3,PostgreSQL,PostgreSQL2,PostgreSQL3 infra
```

**Worker 运行时说明**:

| Worker | 所属模块 | 职责 | 技术栈 |
|--------|---------|------|-------|
| **Transfer Worker** | Transfer | 异步处理数据导入/导出/同步任务,支持重试和并发控制 | Go, Asynq, Redis |
| **Meta Worker** | Meta | 异步处理元数据扫描和索引任务,支持定时调度 | Go, Asynq, Redis |
| **Manager Worker** | Manager | 异步处理 Quick View 瓦片缓存批量生成,支持大规模空间数据预缓存 | Go, Asynq, Redis |

**Asynq 任务队列架构**:
- **队列优先级**: 每个 Worker 支持多优先级队列 (critical/default/low)
- **重试机制**: 任务失败自动重试,支持指数退避
- **并发控制**: 可配置 Worker 并发数,避免资源耗尽
- **进度追踪**: 任务执行状态实时更新到 PostgreSQL

---

## Monitor 模块

Monitor 模块是 ADDP 平台的**统一执行监控中心**,负责监控所有模块的任务执行记录。

```mermaid
graph TB
    subgraph "Monitor 模块"
        MFE[Monitor Frontend<br/>监控仪表盘<br/>:5179]
        MBE[Monitor Backend<br/>监控 API<br/>:8100]
        MFE --> MBE
    end

    subgraph "被监控模块"
        Transfer[Transfer<br/>数据传输]
        Develop[Develop<br/>数据开发]
        Orchestrator[Orchestrator<br/>任务编排]
    end

    subgraph "统一执行表"
        TaskExec[(common.task_executions<br/>统一执行记录表)]
    end

    Transfer -.写入执行记录.-> TaskExec
    Develop -.写入执行记录.-> TaskExec
    Orchestrator -.写入执行记录.-> TaskExec
    MBE -.查询执行记录.-> TaskExec

    Gateway[Gateway<br/>:8000] --> MBE
    Console[Console<br/>:5170] -.嵌入.-> MFE

    classDef monitor fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
    classDef module fill:#f3e5f5,stroke:#4a148c
    classDef infra fill:#fce4ec,stroke:#880e4f

    class MFE,MBE monitor
    class Transfer,Develop,Orchestrator,Gateway,Console module
    class TaskExec infra
```

**Monitor 模块功能**:

| 功能 | 说明 |
|------|------|
| **执行记录查询** | 分页查询所有模块的任务执行记录,支持模块/状态/类型过滤 |
| **统计分析** | 计算成功率、平均执行时间、失败原因分布等统计指标 |
| **趋势分析** | 按天聚合执行数据,生成趋势图表 |
| **模块健康检查** | 检查各模块的任务执行状态,识别异常模块 |

**统一执行表 (common.task_executions)**:
- **跨模块统一**: Transfer、Develop、Orchestrator 三个模块的执行记录统一存储
- **灵活字段**: 使用 JSONB 字段存储模块特有数据 (execution_config, result, error_details)
- **性能优化**: 多层索引 (租户+状态、模块+类型、时间降序、JSONB GIN)
- **数据血缘**: 通过 module + source_task_id 关联原始任务定义

---

## Gateway 路由机制

Gateway 作为 ADDP 的统一入口，负责请求路由和转发。ADDP 采用 **动态模块注册 + 动态路由发现** 机制，支持模块的自动上线/下线和故障转移。

### 模块注册与路由发现流程

```mermaid
graph TB
    subgraph "1. 模块启动注册"
        Manager[Manager<br/>Backend] -.3秒后注册.-> SystemReg[System<br/>模块注册表]
        Meta[Meta<br/>Backend] -.3秒后注册.-> SystemReg
        Transfer[Transfer<br/>Backend] -.3秒后注册.-> SystemReg
        Other[其他模块...] -.3秒后注册.-> SystemReg
    end

    subgraph "2. System 模块注册中心"
        SystemReg --> RegTable[(module_registry 表<br/>存储模块URL和状态)]
        RegTable --> RegAPI[注册 API<br/>/api/internal/modules/*]
    end

    subgraph "3. Gateway 动态发现"
        Gateway[Gateway<br/>:8000] -.30秒刷新.-> RegAPI
        Gateway --> Discovery[ModuleDiscovery<br/>模块发现管理器]
        Discovery --> Proxies[动态代理映射<br/>module -> ServiceProxy]
    end

    subgraph "4. 请求路由"
        Client[客户端请求<br/>/api/:module/*] --> Gateway
        Gateway --> DynamicRoute{动态路由查找}
        DynamicRoute -->|找到| Forward[转发到模块代理]
        DynamicRoute -->|未找到| Fallback[Fallback 硬编码路由]
    end

    classDef backend fill:#e1f5ff,stroke:#01579b
    classDef system fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef gateway fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    classDef client fill:#69db7c,stroke:#2f9e44
    classDef infra fill:#fce4ec,stroke:#880e4f

    class Manager,Meta,Transfer,Other backend
    class SystemReg,RegTable,RegAPI system
    class Gateway,Discovery,Proxies,DynamicRoute,Forward,Fallback gateway
    class Client client
```

### 核心机制说明

**1. 模块自动注册**:
- 各模块启动后 **3 秒**自动向 System 模块注册表（`module_registry` 表）注册
- 注册信息包括：模块名、服务 URL、路由前缀、健康检查 URL、状态等
- 注册操作是 **幂等的**（多次注册自动更新，不会产生重复记录）

**2. 周期心跳机制**:
- 模块每 **10 秒**发送一次心跳到 System
- System 每 **60 秒**清理超过 **30 秒**未心跳的模块（标记为 `status='down'`）

**3. Gateway 动态发现**:
- Gateway 启动时从 System 获取模块列表
- 每 **30 秒**定期刷新模块列表（可配置 `MODULE_REFRESH_INTERVAL`）
- 根据模块状态（`status='up'`）自动创建/更新/删除 HTTP 代理

**4. 双层路由机制**:
- **优先**：动态路由（从模块发现获取代理）
- **备选**：硬编码路由（环境变量 `*_SERVICE_URL` 配置）
- 当模块发现失败或模块下线时，自动 Fallback 到硬编码路由

### 路由请求流程

```mermaid
graph LR
    Client[客户端] --> Gateway[Gateway<br/>:8000]

    Gateway --> |/api/system/*| System[System Backend<br/>:8180]
    Gateway --> |/api/manager/*| Manager[Manager Backend<br/>:8081]
    Gateway --> |/api/meta/*| Meta[Meta Backend<br/>:8082]
    Gateway --> |/api/transfer/*| Transfer[Transfer Backend<br/>:8083]
    Gateway --> |/api/orchestrator/*| Orchestrator[Orchestrator Backend<br/>:8084]
    Gateway --> |/api/develop/*| Develop[Develop Backend<br/>:8085]
    Gateway --> |/api/service/*| Service[Service Backend<br/>:8086]
    Gateway --> |/api/monitor/*| Monitor[Monitor Backend<br/>:8100]

    classDef client fill:#69db7c,stroke:#2f9e44
    classDef gateway fill:#fff9c4,stroke:#f57f17
    classDef backend fill:#e1f5ff,stroke:#01579b

    class Client client
    class Gateway gateway
    class System,Manager,Meta,Transfer,Orchestrator,Develop,Service,Monitor backend
```

### 路由规则

| 路径前缀 | 目标服务 | 端口 | 说明 |
|---------|---------|------|------|
| `/api/system/*` | System Backend | 8180 | 用户认证、引擎管理、日志 |
| `/api/manager/*` | Manager Backend | 8081 | 数据管理、预览、MVT 瓦片 |
| `/api/meta/*` | Meta Backend | 8082 | 元数据扫描、索引、搜索 |
| `/api/transfer/*` | Transfer Backend | 8083 | 数据导入、导出、同步 |
| `/api/orchestrator/*` | Orchestrator Backend | 8084 | 任务编排、调度 |
| `/api/develop/*` | Develop Backend | 8085 | 查询、工作流、Notebook |
| `/api/service/*` | Service Backend | 8086 | 数据服务发布、OGC 标准 |
| `/api/monitor/*` | Monitor Backend | 8100 | 执行监控、统计分析 |

### 配置环境变量

#### Gateway 模块配置

```bash
# 启用模块发现（推荐）
MODULE_REGISTRY_ENABLED=true                 # 启用动态路由发现
MODULE_REFRESH_INTERVAL=30s                  # 模块列表刷新间隔

# System 模块配置（用于获取注册信息）
SYSTEM_SERVICE_URL=http://system-backend:8180
INTERNAL_API_KEY=your_internal_api_key_here  # 服务间调用认证

# Fallback 硬编码路由（模块发现失败时使用）
MANAGER_SERVICE_URL=http://manager-backend:8081
META_SERVICE_URL=http://meta-backend:8082
# ... 其他模块
```

#### 各模块配置

```bash
# 启用与 System 集成
ENABLE_INTEGRATION=true
SYSTEM_SERVICE_URL=http://system-backend:8180
INTERNAL_API_KEY=your_internal_api_key_here
```

### 关键文件位置

| 组件 | 文件路径 | 说明 |
|------|---------|------|
| **System 注册表** | [system/backend/internal/models/module_registry.go](../system/backend/internal/models/module_registry.go) | 模块注册表数据模型 |
| **System API** | [system/backend/internal/api/module_registry_handler.go](../system/backend/internal/api/module_registry_handler.go) | 注册/心跳/查询 API |
| **Gateway 发现** | [gateway/internal/module_discovery.go](../gateway/internal/module_discovery.go) | 模块发现管理器（定期刷新） |
| **Gateway 路由** | [gateway/internal/router/router.go](../gateway/internal/router/router.go) | 动态路由配置 |
| **Common 客户端** | [common/client/system.go](../common/client/system.go) | 模块注册客户端（RegisterModule、SendHeartbeat） |

### 优势与特性

- ✅ **动态上线/下线**：模块启动/停止无需重启 Gateway
- ✅ **故障自动恢复**：模块重启后自动重新注册为 `up` 状态
- ✅ **健康监控**：通过心跳机制实时监控模块状态
- ✅ **双层防护**：动态路由失败时自动 Fallback 到硬编码路由
- ✅ **可观测性**：所有模块状态实时可查（`GET /api/internal/modules`）

---

## ADDP 两种使用方式

ADDP 支持两种使用方式：通过 Console 控制台访问，或各模块独立部署使用。

```mermaid
graph TB
    subgraph "方式一：通过 Console 控制台（推荐）"
        Console[Console<br/>:5170 dev / :8000 prod]

        Console --> Sidebar[左侧边栏<br/>统一导航]
        Console --> IframeArea[主区域<br/>iframe 动态加载]

        IframeArea --> SystemFE[System Frontend<br/>:5173]
        IframeArea --> ManagerFE[Manager Frontend<br/>:5174]
        IframeArea --> MetaFE[Meta Frontend<br/>:5175]
        IframeArea --> OtherFE[其他模块前端...]
    end

    subgraph "方式二：各模块独立使用"
        Standalone[直接访问模块前端]

        Standalone --> StandaloneSystem[System: :5173]
        Standalone --> StandaloneManager[Manager: :5174]
        Standalone --> StandaloneMeta[Meta: :5175]
    end

    classDef console fill:#fff9c4,stroke:#f57f17
    classDef component fill:#e1f5ff,stroke:#01579b
    classDef frontend fill:#e8f5e9,stroke:#1b5e20
    classDef standalone fill:#f3e5f5,stroke:#4a148c

    class Console console
    class Sidebar,IframeArea component
    class SystemFE,ManagerFE,MetaFE,OtherFE frontend
    class Standalone,StandaloneSystem,StandaloneManager,StandaloneMeta standalone
```

### 两种使用方式说明

**方式一：通过 Console 控制台**（推荐）：
- **单一入口**：`http://localhost:5170`（dev）或 `http://localhost:8000`（prod）
- **集成导航**：左侧边栏显示所有模块的导航菜单
- **模块加载**：主区域通过 iframe 动态加载各模块前端
- **一次登录**：访问所有模块，JWT Token 共享传递
- **适用场景**：生产环境，提供完整的用户体验

**方式二：各模块独立使用**：
- **直接访问**：各模块前端独立访问（如 `http://localhost:5173`）
- **独立登录**：每个模块有自己的登录页面
- **独立部署**：适合单个模块独立部署的场景
- **适用场景**：开发调试，模块独立交付

> 认证细节（iframe Token 传递、Token 刷新机制等）见：[ADDP 登录认证原理说明](concepts/addp登录认证的原理说明.md)

---

## 计算引擎与任务编排

### 计算引擎概述

位于 `engines/` 目录，ADDP 平台内置的计算引擎：

```mermaid
graph TB
    subgraph "engines 目录"
        PyWorkflow[python_workflow<br/>Python 工作流引擎]
        SparkWorkflow[spark_workflow<br/>Spark 工作流引擎]
        Jupyter[jupyter<br/>Notebook 引擎]
    end

    subgraph "特性"
        PyWorkflow --> PyFeature[单节点内存计算<br/>适合中小规模数据<br/>空间与非空间算子]
        SparkWorkflow --> SparkFeature[分布式计算<br/>大规模数据处理<br/>空间与非空间算子]
        Jupyter --> JupyterFeature[交互式开发<br/>Python/Shell<br/>变量传递]
    end

    subgraph "系统注册"
        PyWorkflow -.内置引擎.-> System[System 模块]
        SparkWorkflow -.内置引擎.-> System
        Jupyter -.内置引擎.-> System
    end

    Develop[Develop 模块] --> PyWorkflow
    Develop --> SparkWorkflow
    Develop --> Jupyter

    classDef engine fill:#fff9c4,stroke:#f57f17,stroke-width:2px
    classDef feature fill:#fff3e0,stroke:#e65100
    classDef module fill:#f3e5f5,stroke:#4a148c

    class PyWorkflow,SparkWorkflow,Jupyter engine
    class PyFeature,SparkFeature,JupyterFeature feature
    class Develop,System module
```

**引擎说明**：

| 引擎 | 类型 | 适用场景 | 主要能力 |
|------|------|---------|---------|
| **python_workflow** | 单节点内存计算 | 数据量 < 100 万行 | 快速执行，空间与非空间算子 |
| **spark_workflow** | 分布式计算 | 数据量 > 100 万行 | 大规模数据处理，空间与非空间算子 |
| **jupyter** | 交互式开发 | Notebook 开发 | Python/Shell，变量传递 |

**注册机制**：
- 系统启动时自动注册为**内置引擎** (`is_builtin = true`)
- 全局可见，不属于任何租户 (`tenant_id = null`)
- 通过 `unique_identifier` 全局唯一标识（如 `python_workflow`）

---

### 计算引擎与任务编排的架构关系

#### 一、计算引擎层：System 注册 → Develop 调用

System 是计算引擎的**注册中心**，Develop 是计算引擎的**唯一调用方**。

```mermaid
graph LR
    subgraph System["System（注册中心）"]
        EngineDB[(引擎注册表)]
        BuiltIn["内置引擎声明<br/>python_workflow<br/>spark_workflow<br/>jupyter"]
        External["外部引擎注册<br/>（用户通过 UI 添加）"]
        BuiltIn --> EngineDB
        External --> EngineDB
    end

    subgraph Develop["Develop（调用方）"]
        DevAPI["开发工作台 API"]
        DevAPI -->|"查询可用引擎列表"| EngineDB
        DevAPI -->|"按引擎类型路由执行"| EngineRouter["引擎路由器"]
    end

    subgraph Engines["计算引擎（执行层）"]
        PyWF["python_workflow<br/>内存/单机计算"]
        SparkWF["spark_workflow<br/>分布式计算"]
        Jupyter["jupyter<br/>交互式 Notebook"]
    end

    EngineRouter -->|"HTTP / gRPC"| PyWF
    EngineRouter -->|"HTTP / gRPC"| SparkWF
    EngineRouter -->|"WebSocket"| Jupyter

    classDef system fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef develop fill:#e3f2fd,stroke:#0d47a1,stroke-width:2px
    classDef engine fill:#f1f8e9,stroke:#33691e,stroke-width:2px

    class System,EngineDB,BuiltIn,External system
    class Develop,DevAPI,EngineRouter develop
    class PyWF,SparkWF,Jupyter engine
```

**要点**：
- System 仅维护引擎元数据（连接信息、类型、状态），不参与执行
- Develop 运行时从 System 查询引擎配置，按用户选择路由到对应引擎
- 计算引擎是纯执行层，无任务定义概念，只接受并执行代码/作业

---

#### 二、任务编排层：各模块注册 → Orchestrator 编排

各业务模块向 System 注册自己提供的**可编排任务类型**，Orchestrator 读取注册表后动态调用。

```mermaid
graph TB
    subgraph System["System（任务注册中心）"]
        TaskRegistry[(任务提供者注册表<br/>task_providers)]
    end

    subgraph Providers["任务提供者（各业务模块）"]
        ManagerT["Manager<br/>数据扫描、元数据刷新"]
        MetaT["Meta<br/>元数据解析、向量化"]
        TransferT["Transfer<br/>数据导入、导出、同步"]
        DevelopT["Develop<br/>SQL执行、工作流运行<br/>Notebook 运行"]

        ManagerT -->|"启动时注册任务类型"| TaskRegistry
        MetaT   -->|"启动时注册任务类型"| TaskRegistry
        TransferT -->|"启动时注册任务类型"| TaskRegistry
        DevelopT -->|"启动时注册任务类型"| TaskRegistry
    end

    subgraph Orchestrator["Orchestrator（编排调度）"]
        DAGEngine["DAG 调度引擎"]
        DAGEngine -->|"① 拉取任务类型定义"| TaskRegistry
        DAGEngine -->|"② 按 DAG 顺序调用各模块 API"| ManagerT
        DAGEngine -->|"② 按 DAG 顺序调用各模块 API"| MetaT
        DAGEngine -->|"② 按 DAG 顺序调用各模块 API"| TransferT
        DAGEngine -->|"② 按 DAG 顺序调用各模块 API"| DevelopT
    end

    classDef system fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef provider fill:#f3e5f5,stroke:#4a148c
    classDef orch fill:#e8eaf6,stroke:#283593,stroke-width:2px

    class System,TaskRegistry system
    class ManagerT,MetaT,TransferT,DevelopT provider
    class Orchestrator,DAGEngine orch
```

**要点**：
- 各模块启动时向 System 注册任务类型（任务名、参数 schema、回调 API 地址）
- Orchestrator 不硬编码对任何模块的依赖，完全由注册信息驱动
- DAG 步骤间通过 `{{stepID.field}}` 语法传递上游结果

---

#### 三、两层之间的关联：Develop 是枢纽

```mermaid
graph LR
    subgraph "引擎层"
        System_E["System 引擎注册表"]
        Engines["计算引擎集群<br/>python / spark / jupyter"]
        System_E -->|"引擎配置"| Develop
        Develop -->|"实际执行"| Engines
    end

    subgraph "编排层"
        System_T["System 任务注册表"]
        Orchestrator["Orchestrator DAG 调度"]
        Develop -->|"注册任务类型"| System_T
        Orchestrator -->|"调用 Develop 任务 API"| Develop
    end

    Develop["Develop<br/>（引擎调用方 + 任务提供者）"]

    classDef system fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef develop fill:#e3f2fd,stroke:#0d47a1,stroke-width:3px
    classDef engine fill:#f1f8e9,stroke:#33691e
    classDef orch fill:#e8eaf6,stroke:#283593

    class System_E,System_T system
    class Develop develop
    class Engines engine
    class Orchestrator orch
```

**Develop 在两层中扮演不同角色**：
- 对**引擎层**：是消费者，从 System 获取引擎配置，向计算引擎派发执行任务
- 对**编排层**：是提供者，将自身能力（SQL、工作流、Notebook）封装为可编排任务类型注册到 System，供 Orchestrator 调度

这意味着一个复杂的数据处理流水线可以是：
> Transfer 导入数据 → Develop 执行 SQL/Python 加工 → Meta 更新元数据 → Manager 刷新目录
>
> 全部由 Orchestrator 统一编排，其中 Develop 步骤内部再调用计算引擎执行。

---

## 模块启动顺序

ADDP 平台各模块存在依赖关系，必须按以下顺序启动：

```
1. 基础设施层（必须最先就绪）
   ├─ PostgreSQL
   ├─ Redis
   ├─ MinIO
   └─ Meilisearch

2. 核心系统层
   └─ System Backend（认证中心、模块注册中心、引擎注册表）

3. Go 业务模块层（可并行启动）
   ├─ Manager Backend + Worker
   ├─ Meta Backend + Worker
   ├─ Transfer Backend + Worker
   ├─ Orchestrator Backend
   ├─ Develop Backend
   ├─ Service Backend
   └─ Monitor Backend

4. 计算引擎层（可并行启动，Python 运行时）
   ├─ Python Workflow Engine
   ├─ Math Workflow Engine
   ├─ Spark Workflow Engine
   └─ Jupyter Engine

5. Copilot（Python 应用层，独立启动）
   └─ Copilot Backend（启动时仅向 System 注册，运行时才调用 Meta/Develop）

6. 网关层
   └─ Gateway（从 System 获取已注册的模块路由信息，建立动态路由）

7. 前端层（可并行启动）
   ├─ Console Frontend
   ├─ System Frontend
   ├─ Manager Frontend
   ├─ Meta Frontend
   ├─ Transfer Frontend
   ├─ Orchestrator Frontend
   ├─ Develop Frontend
   ├─ Service Frontend
   └─ Monitor Frontend
```

**关键顺序约束说明**：

| 约束 | 说明 |
|------|------|
| **System → Go 业务模块** | 业务模块依赖 System 提供的认证和注册服务 |
| **Go 业务模块 → Gateway** | 业务模块启动后 3 秒自动向 System 注册；Gateway 启动时从 System 获取已注册模块列表建立路由，必须在业务模块之后启动 |
| **Copilot 独立于 Go 业务模块层** | Copilot 是 Python 应用服务，语言运行时与 Go 模块不同，不适合混入 Go 三段式（编译→启动→健康检查）流程；但启动时依赖相同（仅 System），运行时才调用 Meta/Develop |
| **前端无严格顺序约束** | Console 通过 iframe 动态加载各模块前端（用户访问时才加载），各前端可完全并行启动 |

---

## 相关文档

- [ADDP 各模块简要介绍](concepts/addp各模块功能介绍.md)
- [ADDP 核心概念关系图](addp核心概念关系图.md)
- [ADDP 登录认证原理说明](concepts/addp登录认证的原理说明.md)
- [ADDP 开发原则](spec/addp开发原则.md)
- [ADDP 共享模块介绍](concepts/addp共享模块介绍.md)
- [ADDP 新模块开发指南](spec/addp新模块开发指南.md)
- [Monitor 模块实施报告](../monitor/docs/Monitor模块实施报告.md)

---

**文档版本**: v1.1
**创建日期**: 2026-02-16
**更新日期**: 2026-02-16
**作者**: ADDP 开发团队
