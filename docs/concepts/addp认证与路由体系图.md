# ADDP 认证与路由体系图

本文档展示 ADDP 平台的 JWT 认证流程、Gateway 路由机制和 Portal 架构。

---

## 目录

1. [JWT 认证流程](#jwt-认证流程)
2. [Gateway 路由机制](#gateway-路由机制)
3. [Portal 架构](#portal-架构)
4. [Backend/Worker 分离架构](#backendworker-分离架构)

---

## JWT 认证流程

ADDP 使用 **JWT (JSON Web Token)** 实现无状态认证。

```mermaid
sequenceDiagram
    participant User as 用户
    participant Frontend as 前端
    participant Gateway as Gateway
    participant System as System Backend
    participant DB as PostgreSQL

    Note over User,DB: === 登录阶段 ===

    User->>Frontend: 1. 输入用户名/密码
    Frontend->>Gateway: 2. POST /api/system/login
    Gateway->>System: 3. 转发登录请求
    System->>DB: 4. 查询用户<br/>(users 表)
    DB-->>System: 5. 返回用户信息<br/>(含 password_hash, tenant_id)
    System->>System: 6. 验证密码<br/>(bcrypt)
    System->>System: 7. 生成 JWT Token<br/>payload: {user_id, tenant_id, role}
    System-->>Gateway: 8. 返回 {token, user_info}
    Gateway-->>Frontend: 9. 返回登录成功
    Frontend->>Frontend: 10. 存储 token<br/>(localStorage)

    Note over User,DB: === 访问资源阶段 ===

    User->>Frontend: 11. 访问受保护资源
    Frontend->>Gateway: 12. GET /api/manager/data<br/>Header: Authorization: Bearer {token}
    Gateway->>Gateway: 13. 验证 JWT Token<br/>提取 tenant_id
    Gateway->>System: 14. 转发请求<br/>(附带 tenant_id)
    System->>DB: 15. 查询数据<br/>(WHERE tenant_id = ?)
    DB-->>System: 16. 返回租户数据
    System-->>Gateway: 17. 返回结果
    Gateway-->>Frontend: 18. 返回数据
    Frontend-->>User: 19. 展示数据
```

### JWT Token 结构

```json
{
  "header": {
    "alg": "HS256",
    "typ": "JWT"
  },
  "payload": {
    "user_id": 123,
    "username": "admin",
    "tenant_id": 1,
    "tenant_name": "默认租户",
    "role": "admin",
    "exp": 1708099200,
    "iat": 1708012800
  },
  "signature": "..."
}
```

---

## Gateway 路由机制

Gateway 作为 ADDP 的统一入口,负责请求路由和转发。

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

    classDef client fill:#69db7c,stroke:#2f9e44
    classDef gateway fill:#fff9c4,stroke:#f57f17
    classDef backend fill:#e1f5ff,stroke:#01579b

    class Client client
    class Gateway gateway
    class System,Manager,Meta,Transfer,Orchestrator,Develop,Service backend
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

---

## Portal 架构

Portal 是 ADDP 的统一入口,提供两种访问模式。

```mermaid
graph TB
    subgraph "统一门户模式 (推荐)"
        Portal[Portal<br/>:5170 dev / :8000 prod]

        Portal --> Sidebar[左侧边栏<br/>统一导航]
        Portal --> IframeArea[主区域<br/>iframe 动态加载]

        IframeArea --> SystemFE[System Frontend<br/>:5173]
        IframeArea --> ManagerFE[Manager Frontend<br/>:5174]
        IframeArea --> MetaFE[Meta Frontend<br/>:5175]
        IframeArea --> OtherFE[其他模块前端...]
    end

    subgraph "独立模块模式 (独立部署)"
        Standalone[直接访问模块前端]

        Standalone --> StandaloneSystem[System: :5173]
        Standalone --> StandaloneManager[Manager: :5174]
        Standalone --> StandaloneMeta[Meta: :5175]
    end

    classDef portal fill:#fff9c4,stroke:#f57f17
    classDef component fill:#e1f5ff,stroke:#01579b
    classDef frontend fill:#e8f5e9,stroke:#1b5e20
    classDef standalone fill:#f3e5f5,stroke:#4a148c

    class Portal portal
    class Sidebar,IframeArea component
    class SystemFE,ManagerFE,MetaFE,OtherFE frontend
    class Standalone,StandaloneSystem,StandaloneManager,StandaloneMeta standalone
```

### Portal 两种模式

**1. 统一门户模式** (推荐):
- **单一入口**: `http://localhost:5170` (dev) 或 `http://localhost:8000` (prod)
- **集成导航**: 左侧边栏显示所有模块的导航菜单
- **模块加载**: 主区域通过 iframe 动态加载模块前端
- **一次登录**: 访问所有模块,JWT Token 共享
- **适用场景**: 生产环境,提供完整的用户体验

**2. 独立模块模式**:
- **直接访问**: 各模块前端独立访问 (如 `http://localhost:5173`)
- **独立登录**: 每个模块有自己的登录页面
- **独立部署**: 适合单个模块独立部署的场景
- **适用场景**: 开发调试,模块独立交付

---

## Backend/Worker 分离架构

部分模块采用 Backend/Worker 分离架构,提高系统性能和可扩展性。

```mermaid
graph TB
    subgraph "客户端"
        Client[前端/API 客户端]
    end

    subgraph "Backend (API 服务)"
        Backend[Backend<br/>处理 HTTP 请求]

        Backend --> API[业务逻辑]
        Backend --> CreateTask[创建后台任务]
    end

    subgraph "任务队列"
        Queue[(Redis/Asynq)]
    end

    subgraph "Worker (后台任务)"
        Worker[Worker<br/>执行后台任务]

        Worker --> ScanTask[元数据扫描]
        Worker --> TransferTask[数据传输]
        Worker --> ComputeTask[数据计算]
    end

    Client --> Backend
    Backend --> Queue
    Queue --> Worker

    classDef client fill:#69db7c,stroke:#2f9e44
    classDef backend fill:#e1f5ff,stroke:#01579b
    classDef queue fill:#fff9c4,stroke:#f57f17
    classDef worker fill:#e8f5e9,stroke:#1b5e20

    class Client client
    class Backend,API,CreateTask backend
    class Queue queue
    class Worker,ScanTask,TransferTask,ComputeTask worker
```

### Backend/Worker 分离说明

**Backend**:
- 处理 HTTP API 请求
- 执行业务逻辑
- 返回即时响应
- 创建后台任务

**Worker**:
- 执行后台任务 (扫描、传输、计算等)
- 基于 Asynq 任务队列
- 异步处理,不阻塞请求
- 支持任务重试和并发控制

**采用分离架构的模块**:
- **Meta**: Backend (API) + Worker (元数据扫描)
- **Transfer**: Backend (API) + Worker (数据导入/导出/同步)

---

## 相关文档

- [返回核心概念关系图](../addp核心概念关系图.md)
- [ADDP 账号与权限体系图](addp账号与权限体系图.md)
- [Gateway 架构说明](../../gateway/doc/gateway架构说明.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
