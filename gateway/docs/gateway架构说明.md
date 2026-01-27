# Gateway 架构详解

## 📋 目录

1. [Gateway 是什么](#gateway-是什么)
2. [核心功能](#核心功能)
3. [工作原理](#工作原理)
4. [代码结构](#代码结构)
5. [请求流程](#请求流程)
6. [路由规则](#路由规则)
7. [数据库集成](#数据库集成)
8. [Redis 集成](#redis-集成)
9. [为什么使用专用 SystemClient](#为什么使用专用-systemclient)
10. [监控和调试](#监控和调试)
11. [未来扩展](#未来扩展)

## Gateway 是什么

Gateway（API 网关）是全域数据平台的**统一入口**，所有外部请求都通过它进入系统。

### 为什么需要 Gateway？

在微服务架构中，如果没有 Gateway：

```
客户端 → System (8180)     # 直接访问宿主机端口
客户端 → Manager (8081)
客户端 → Meta (8082)
客户端 → Transfer (8083)
客户端 → Develop (8084)
客户端 → Service (8086)
客户端 → Copilot (8087)
```

**问题**：
- 客户端需要知道每个服务的地址
- 跨域配置分散在各个服务
- 认证和鉴权逻辑重复
- 难以统一管理和监控
- 无法统一限流和访问控制

有了 Gateway：

```
客户端 → Gateway (8000) → System (8180)
                        → Manager (8081)
                        → Meta (8082)
                        → Transfer (8083)
                        → Develop (8084)
                        → Service (8086)
                        → Copilot (8087)
```

**优势**：
- 统一入口，客户端只需要知道 Gateway 地址
- 集中处理跨域、认证、限流等
- 服务对外透明，可以随意调整内部服务
- 便于监控、日志、安全控制
- 支持 API Key 管理和访问审计

## 核心功能

### 1. **API Key 认证** 🔐
基于三层缓存的 API Key 验证机制

```
客户端请求（X-API-Key: xxx）
  ↓
三层缓存验证：
  ├─ 本地缓存（5分钟 TTL，<1ms）
  ├─ Redis 缓存（1小时 TTL，<5ms）
  └─ System API 验证（<20ms）
```

**特点**：
- SHA256 哈希存储，安全可靠
- 三层缓存极大提升性能
- 支持服务级别访问控制
- 支持 API Key 撤销和缓存失效

### 2. **请求路由** 🚦
根据 URL 路径将请求转发到对应的后端服务

```
/api/system/*    → System (认证、用户、租户、引擎、日志)
/api/manager/*   → Manager (数据源、预览、文件上传)
/api/meta/*      → Meta (元数据扫描、对象存储)
/api/transfer/*  → Transfer (数据传输任务)
/api/develop/*   → Develop (SQL 执行、工作流)
/api/service/*   → Service (数据服务、OGC 标准)
/api/copilot/*   → Copilot (AI 助手)
```

### 3. **限流控制** ⏱️
基于 Redis 令牌桶算法的限流机制

```
每个应用独立限流配额
  ↓
Redis 原子操作（Lua 脚本）
  ↓
超限返回 429 Too Many Requests
```

**特点**：
- 按应用 ID 独立限流
- 每分钟请求数可配置
- Redis 保证分布式一致性
- 自动记录限流事件

### 4. **访问日志** 📝
异步记录所有 API 访问到 PostgreSQL

```
每次请求自动记录：
  - 应用 ID 和 API Key 前缀
  - 请求路径、方法、参数
  - 响应状态码和响应时间
  - 缓存命中情况
  - 是否被限流
```

**用途**：
- API 使用统计和分析
- 性能监控和优化
- 缓存命中率分析
- 限流审计

### 5. **请求代理** 🔄
完整转发 HTTP 请求，包括：
- 请求方法（GET, POST, PUT, DELETE）
- 请求头（Headers）
- 请求体（Body）
- 查询参数（Query Parameters）

### 6. **路径重写** 🔀
支持模块化路由映射

```
Manager 模块路径重写：
  请求：GET /api/manager/engines/1
  转发：GET /api/engines/1 (Manager 服务)

Copilot 模块路径重写：
  请求：POST /api/copilot/chat
  转发：POST /chat (Copilot 服务)
```

### 7. **跨域处理** 🌐
统一配置 CORS，允许前端跨域访问

### 8. **健康检查** ❤️
提供 `/health` 端点检查 Gateway 状态

## 工作原理

### 整体架构

```
┌─────────────────────────────────────────────────────┐
│                    客户端/浏览器                       │
└─────────────────┬───────────────────────────────────┘
                  │ HTTP Request (X-API-Key)
                  ▼
         ┌─────────────────┐
         │   Gateway:8000   │
         │                 │
         │  ┌───────────┐  │
         │  │  Router   │  │  路由解析
         │  └─────┬─────┘  │
         │        │        │
         │  ┌─────▼─────┐  │
         │  │Middleware │  │  四层中间件处理
         │  │  Chain    │  │
         │  │           │  │
         │  │ 1. CORS   │  │  跨域处理
         │  │ 2. APIKey │  │  API Key 验证（三层缓存）
         │  │ 3. RateLimit│ │  限流控制（Redis）
         │  │ 4. AccessLog│ │  访问日志（PostgreSQL）
         │  └─────┬─────┘  │
         │        │        │
         │  ┌─────▼─────┐  │
         │  │   Proxy   │  │  HTTP 代理
         │  └─────┬─────┘  │
         └────────┼────────┘
                  │
         ┌────────┼────────┬────────┬────────┬────────┬────────┐
         │        │        │        │        │        │        │
    ┌────▼───┐ ┌─▼────┐ ┌─▼────┐ ┌──▼────┐ ┌─▼─────┐ ┌─▼────┐ ┌─▼─────┐
    │System  │ │Manager│ │Meta  │ │Transfer│ │Develop│ │Service│ │Copilot│
    │  8180  │ │ 8081 │ │ 8082 │ │ 8083  │ │ 8084  │ │ 8086  │ │ 8087  │
    └────────┘ └──────┘ └──────┘ └───────┘ └───────┘ └───────┘ └───────┘
```

### 中间件处理顺序

```
Request
  ↓
[1] CORS Middleware (common)
    - 设置跨域头部
    - 处理 OPTIONS 预检请求
  ↓
[2] API Key Auth Middleware
    - 检查 X-API-Key 头
    - 三层缓存验证（本地 → Redis → System API）
    - 存储 app_id、app_name 到 context
  ↓
[3] Rate Limiter Middleware
    - 检查 context 中的 api_key_info
    - Redis 令牌桶检查
    - 超限返回 429
  ↓
[4] Access Logger Middleware
    - 记录请求开始时间
    - 执行业务处理
    - 异步记录访问日志到 PostgreSQL
  ↓
[5] Route Handler (Proxy)
    - 根据路径规则选择目标服务
    - 转发请求到后端服务
    - 返回响应
  ↓
Response
```

### 核心组件

#### 1. **Config (配置管理)**
文件：[internal/config/config.go](../internal/config/config.go)

```go
type Config struct {
    // Gateway 配置
    Port string  // Gateway 端口（默认：8000）
    Env  string  // 运行环境（development/production）

    // 后端服务地址（7 个服务）
    SystemServiceURL   string  // System 服务（默认：http://localhost:8180）
    ManagerServiceURL  string  // Manager 服务（默认：http://localhost:8081）
    MetaServiceURL     string  // Meta 服务（默认：http://localhost:8082）
    TransferServiceURL string  // Transfer 服务（默认：http://localhost:8083）
    DevelopServiceURL  string  // Develop 服务（默认：http://localhost:8084）
    ServiceServiceURL  string  // Service 服务（默认：http://localhost:8086）
    CopilotServiceURL  string  // Copilot 服务（默认：http://localhost:8087）

    // 数据库配置
    DBHost     string  // PostgreSQL 主机（默认：localhost）
    DBPort     string  // PostgreSQL 端口（默认：5432）
    DBUser     string  // 数据库用户（默认：addp）
    DBPassword string  // 数据库密码（默认：addp_password）
    DBName     string  // 数据库名（默认：addp）
    DBSchema   string  // 数据库 schema（默认：gateway）

    // Redis 配置
    RedisHost     string  // Redis 主机（默认：localhost）
    RedisPort     string  // Redis 端口（默认：6379）
    RedisPassword string  // Redis 密码（默认：addp_redis）
    RedisDB       int     // Redis DB（默认：0）

    // 认证配置
    InternalAPIKey string  // 内部 API 密钥（用于调用 System 模块）
}
```

**作用**：
- 从环境变量读取配置
- 提供默认值
- 集中管理所有服务地址和连接信息

#### 2. **Router (路由配置)**
文件：[internal/router/router.go](../internal/router/router.go)

**作用**：
- 定义路由规则（公开路由 + 受保护路由）
- 创建代理实例（7 个服务）
- 配置中间件链（CORS → API Key → 限流 → 日志）
- 初始化数据库和 Redis 连接

#### 3. **Proxy (HTTP 代理)**
文件：[internal/proxy/proxy.go](../internal/proxy/proxy.go)

```go
type ServiceProxy struct {
    targetURL string      // 目标服务地址
    client    *http.Client // HTTP 客户端
}

// Handle 基础代理处理
func (p *ServiceProxy) Handle(c *gin.Context) {
    // 1. 构建目标 URL（保留查询参数）
    // 2. 读取请求体
    // 3. 创建新请求
    // 4. 复制请求头
    // 5. 发送请求到后端服务
    // 6. 复制响应头和响应体
}

// HandleWithPathRewrite 路径重写代理
func (p *ServiceProxy) HandleWithPathRewrite(prefix string) gin.HandlerFunc {
    // 移除路径前缀后转发
    // 例如：/api/manager/engines → /api/engines
}
```

**作用**：
- 转发 HTTP 请求到后端服务
- 保持请求的完整性（方法、头部、体、查询参数）
- 支持路径重写
- 透明代理，客户端无感知

#### 4. **Middleware (中间件)**

##### APIKeyAuthMiddleware
文件：[internal/middleware/api_key_auth.go](../internal/middleware/api_key_auth.go)

**功能**：
- 检查 `X-API-Key` 请求头
- 计算 API Key 的 SHA256 哈希
- 三层缓存验证（本地 5分钟 → Redis 1小时 → System API）
- 将验证结果存入 gin context

##### RateLimiterMiddleware
文件：[internal/middleware/rate_limiter.go](../internal/middleware/rate_limiter.go)

**功能**：
- 基于 Redis 令牌桶算法
- 按应用 ID 限制每分钟请求数
- 使用 Lua 脚本保证原子性
- 超限返回 429 错误

##### AccessLoggerMiddleware
文件：[internal/middleware/access_logger.go](../internal/middleware/access_logger.go)

**功能**：
- 异步记录所有 API 访问
- 存储到 PostgreSQL `gateway.api_access_logs` 表
- 包含应用 ID、API Key 前缀、服务名、响应状态、响应时间等

#### 5. **SystemClient (专用客户端)**
文件：[pkg/client/system_client.go](../pkg/client/system_client.go)

**功能**：
- `ValidateAPIKey(keyHash)` - 验证 API Key
- `BulkGetAPIKeys()` - 批量获取 API Keys（用于预加载）

**特点**：
- 5 秒超时（快速失败）
- 使用内部密钥认证（`X-Internal-API-Key`）
- 轻量级（100 行代码）
- 专为 API Key 验证优化

#### 6. **LocalCache (本地缓存)**
文件：[internal/cache/local_cache.go](../internal/cache/local_cache.go)

**功能**：
- 存储 API Key 验证结果
- 5 分钟 TTL
- 自动清理过期数据
- 支持主动失效

## 代码结构

```
gateway/
├── cmd/
│   └── gateway/
│       └── main.go              # 入口文件，启动服务
├── internal/
│   ├── config/
│   │   └── config.go            # 配置管理
│   ├── router/
│   │   └── router.go            # 路由配置
│   ├── proxy/
│   │   └── proxy.go             # HTTP 代理逻辑
│   ├── middleware/
│   │   ├── api_key_auth.go      # API Key 认证中间件
│   │   ├── rate_limiter.go      # 限流中间件
│   │   └── access_logger.go     # 访问日志中间件
│   └── cache/
│       └── local_cache.go       # 本地缓存实现
├── pkg/
│   └── client/
│       └── system_client.go     # System 模块 API 客户端
├── docs/
│   └── gateway架构说明.md       # 本文档
├── go.mod                        # Go 模块定义
├── go.sum                        # 依赖校验
├── Dockerfile                    # Docker 镜像构建
└── README.md                     # 说明文档
```

## 请求流程

### 示例 1：带 API Key 的请求流程

```
1. 客户端发起请求
   GET http://localhost:8000/api/manager/engines
   Headers: X-API-Key: sk_live_abc123...

2. 请求到达 Gateway
   ↓
   [Gateway:8000] 接收请求

3. CORS 中间件处理
   ↓
   [Middleware] 添加 CORS 头

4. API Key 认证中间件
   ↓
   [APIKeyAuthMiddleware]
   - 提取 API Key: sk_live_abc123...
   - 计算 SHA256 hash
   - 查询本地缓存 → Miss
   - 查询 Redis 缓存 → Miss
   - 调用 System API /internal/api-keys/validate
   - 返回验证结果: {valid: true, app_id: 123, rate_limit_per_minute: 1000}
   - 写入 Redis 缓存（1小时）
   - 写入本地缓存（5分钟）
   - 设置 context: app_id=123, app_name="My App"

5. 限流中间件
   ↓
   [RateLimiterMiddleware]
   - 读取 app_id: 123
   - Redis 令牌桶检查: ratelimit:app:123
   - 当前请求数: 45 < 1000 (限额)
   - 通过

6. 访问日志中间件
   ↓
   [AccessLoggerMiddleware]
   - 记录请求开始时间
   - 继续处理...

7. 路由匹配
   ↓
   [Router] 匹配规则: /api/manager/* → managerProxy

8. 代理转发（路径重写）
   ↓
   [Proxy] 原始路径: /api/manager/engines
   [Proxy] 重写后: /api/engines
   [Proxy] 目标 URL: http://localhost:8081/api/engines
   [Proxy] 复制请求头: X-API-Key, Content-Type...
   [Proxy] 发送请求

9. Manager 服务处理
   ↓
   GET http://localhost:8081/api/engines
   [Manager] 返回引擎列表

10. Gateway 接收响应
    ↓
    [Proxy] 复制响应状态: 200
    [Proxy] 复制响应头: Content-Type: application/json
    [Proxy] 复制响应体: [{"id": 1, "name": "PostgreSQL"}]

11. 访问日志记录
    ↓
    [AccessLoggerMiddleware]
    - 计算响应时间: 67ms
    - 异步写入 PostgreSQL: gateway.api_access_logs
    - 记录: app_id=123, service_name=manager, response_time_ms=67, cache_hit=false

12. 返回给客户端
    ↓
    客户端收到: [{"id": 1, "name": "PostgreSQL"}]
```

### 示例 2：限流触发流程

```
客户端发起第 1001 次请求（限额 1000/分钟）
  ↓
APIKeyAuthMiddleware（缓存 Hit，<1ms）
  ↓
RateLimiterMiddleware
  ├─ 读取 app_id: 123
  ├─ Redis INCR ratelimit:app:123 → 1001
  └─ 1001 > 1000 (限额) → 返回 429 Too Many Requests
  ↓
AccessLoggerMiddleware
  └─ 记录: rate_limited=true, response_status=429
  ↓
响应给客户端: {"error": "Rate limit exceeded"}
```

### 示例 3：公开路由（登录）

```
客户端发起登录请求
  ↓
POST http://localhost:8000/api/system/login
Body: {"username": "admin", "password": "123456"}
  ↓
CORS 中间件处理
  ↓
路由匹配：/api/system/login（公开路由，无需 API Key）
  ↓
代理转发：http://localhost:8180/api/system/login
  ↓
System 服务验证用户名密码
  ↓
返回 JWT Token
```

### 时序图

```
客户端          Gateway         System API      Redis       PostgreSQL
  │              │               │               │            │
  │─────POST────→│               │               │            │
  │ X-API-Key: xxx               │               │            │
  │              │               │               │            │
  │              │─ValidateKey──→│               │            │
  │              │               │ (首次验证)    │            │
  │              │←────Valid─────│               │            │
  │              │               │               │            │
  │              │──────SET──────┼──────────────→│            │
  │              │         (缓存 1小时)          │            │
  │              │               │               │            │
  │              │─────POST──────┼───────────────┼───────────→│
  │              │ (代理到 Manager)              │            │
  │              │               │               │            │
  │              │←────200───────┼───────────────┼────────────│
  │              │               │               │            │
  │              │──────INSERT───┼───────────────┼───────────→│
  │              │         (异步记录访问日志)    │            │
  │              │               │               │            │
  │←────200──────│               │               │            │
  │              │               │               │            │
```

## 路由规则

### 当前配置的路由

| 路由前缀 | 目标服务 | 端口 | 认证要求 | 路径重写 | 说明 |
|---------|---------|------|---------|---------|-----|
| `POST /api/system/login` | System | 8180 | **公开** | 无 | 用户登录 |
| `POST /api/system/register` | System | 8180 | **公开** | 无 | 用户注册 |
| `/api/system/users/*` | System | 8180 | API Key | 无 | 用户管理 |
| `/api/system/tenants/*` | System | 8180 | API Key | 无 | 租户管理 |
| `/api/system/engines/*` | System | 8180 | API Key | 无 | 引擎管理 |
| `/api/system/logs/*` | System | 8180 | API Key | 无 | 日志查询 |
| `/api/system/applications/*` | System | 8180 | API Key | 无 | 应用管理 |
| `/api/system/admin/*` | System | 8180 | API Key | 无 | 系统管理 |
| `/api/manager/engines/*` | Manager | 8081 | API Key | ✅ 移除 `/manager` | 引擎配置 |
| `/api/manager/preview/*` | Manager | 8081 | API Key | ✅ 移除 `/manager` | 数据预览 |
| `/api/manager/tree/*` | Manager | 8081 | API Key | ✅ 移除 `/manager` | 目录树 |
| `/api/manager/search/*` | Manager | 8081 | API Key | ✅ 移除 `/manager` | 搜索 |
| `/api/manager/mvt/*` | Manager | 8081 | API Key | ✅ 移除 `/manager` | 地图瓦片 |
| `/api/meta/engines/*` | Meta | 8082 | API Key | 无 | 引擎列表 |
| `/api/meta/scan/*` | Meta | 8082 | API Key | 无 | 元数据扫描 |
| `/api/meta/object-storage/*` | Meta | 8082 | API Key | 无 | 对象存储 |
| `/api/transfer/tasks/*` | Transfer | 8083 | API Key | 无 | 传输任务 |
| `/api/transfer/executions/*` | Transfer | 8083 | API Key | 无 | 任务执行 |
| `/api/transfer/connections/*` | Transfer | 8083 | API Key | 无 | 连接管理 |
| `/api/develop/engines/*` | Develop | 8084 | API Key | 无 | 引擎列表 |
| `/api/develop/sql/*` | Develop | 8084 | API Key | 无 | SQL 执行 |
| `/api/develop/workflows/*` | Develop | 8084 | API Key | 无 | 工作流管理 |
| `/api/service/services/*` | Service | 8086 | API Key | 无 | 数据服务 |
| `/api/service/ogc/*` | Service | 8086 | API Key | 无 | OGC 标准 |
| `/api/copilot/*` | Copilot | 8087 | API Key | ✅ 移除 `/api` | AI 助手 |
| `/api/internal/engines/*` | System | 8180 | API Key | 无 | 内部 API |

### 路径重写示例

**Manager 模块**：
```
请求: GET /api/manager/engines/1
  ↓ 路径重写（移除 /manager 前缀）
转发: GET /api/engines/1 (Manager 服务)
```

**Copilot 模块**：
```
请求: POST /api/copilot/chat
  ↓ 路径重写（移除 /api 前缀）
转发: POST /chat (Copilot 服务)
```

### 路由匹配规则

Gateway 使用 **前缀匹配**：

```
请求: GET /api/system/users/123
匹配: /api/system/users/*
代理到: http://localhost:8180/api/system/users/123

请求: POST /api/manager/engines?type=postgresql
匹配: /api/manager/engines/*
路径重写: /api/engines?type=postgresql
代理到: http://localhost:8081/api/engines?type=postgresql

请求: GET /api/develop/sql/execute
匹配: /api/develop/sql/*
代理到: http://localhost:8084/api/develop/sql/execute
```

## 数据库集成

### PostgreSQL Schema: `gateway`

Gateway 使用独立的数据库 schema 存储访问日志。

#### 表结构：api_access_logs

```sql
CREATE TABLE gateway.api_access_logs (
    id                SERIAL PRIMARY KEY,
    application_id    INTEGER,              -- 应用 ID（来自 API Key 验证）
    api_key_prefix    VARCHAR(20),          -- API Key 前缀（sk_live_xxx...）
    service_name      VARCHAR(255),         -- 目标服务名（system, manager, meta 等）
    request_method    VARCHAR(10),          -- HTTP 方法（GET, POST, PUT, DELETE）
    request_path      TEXT,                 -- 请求路径
    request_params    JSONB,                -- 查询参数 + 请求体
    response_status   INTEGER,              -- 响应状态码（200, 404, 500 等）
    response_time_ms  INTEGER,              -- 响应时间（毫秒）
    cache_hit         BOOLEAN DEFAULT FALSE,-- 缓存命中标志
    rate_limited      BOOLEAN DEFAULT FALSE,-- 是否被限流
    accessed_at       TIMESTAMP DEFAULT NOW()-- 访问时间
);

-- 索引（加速查询）
CREATE INDEX idx_accessed_at ON gateway.api_access_logs(accessed_at);
CREATE INDEX idx_application_id ON gateway.api_access_logs(application_id);
CREATE INDEX idx_service_name ON gateway.api_access_logs(service_name);
CREATE INDEX idx_response_status ON gateway.api_access_logs(response_status);
```

#### 字段说明

| 字段名 | 类型 | 说明 | 示例 |
|-------|------|------|------|
| id | INTEGER | 主键 | 1001 |
| application_id | INTEGER | 应用 ID | 123 |
| api_key_prefix | VARCHAR(20) | API Key 前缀 | `sk_live_abc...` |
| service_name | VARCHAR(255) | 目标服务 | `manager` |
| request_method | VARCHAR(10) | HTTP 方法 | `GET` |
| request_path | TEXT | 请求路径 | `/api/manager/engines` |
| request_params | JSONB | 请求参数 | `{"type": "postgresql"}` |
| response_status | INTEGER | 响应状态码 | 200 |
| response_time_ms | INTEGER | 响应时间（毫秒） | 67 |
| cache_hit | BOOLEAN | 缓存命中 | true |
| rate_limited | BOOLEAN | 是否被限流 | false |
| accessed_at | TIMESTAMP | 访问时间 | `2025-01-24 10:30:15` |

#### 用途

1. **API 使用统计**
   ```sql
   -- 按服务统计请求量
   SELECT service_name, COUNT(*) as request_count
   FROM gateway.api_access_logs
   WHERE accessed_at > NOW() - INTERVAL '1 day'
   GROUP BY service_name
   ORDER BY request_count DESC;
   ```

2. **性能分析**
   ```sql
   -- 查询平均响应时间
   SELECT service_name, AVG(response_time_ms) as avg_time
   FROM gateway.api_access_logs
   WHERE accessed_at > NOW() - INTERVAL '1 hour'
   GROUP BY service_name;
   ```

3. **缓存命中率监控**
   ```sql
   -- 计算缓存命中率
   SELECT
       COUNT(*) FILTER (WHERE cache_hit = TRUE) * 100.0 / COUNT(*) as hit_rate
   FROM gateway.api_access_logs
   WHERE accessed_at > NOW() - INTERVAL '1 hour';
   ```

4. **限流审计**
   ```sql
   -- 查询被限流的请求
   SELECT application_id, COUNT(*) as rate_limited_count
   FROM gateway.api_access_logs
   WHERE rate_limited = TRUE
   GROUP BY application_id
   ORDER BY rate_limited_count DESC;
   ```

## Redis 集成

### Redis Key 前缀规范

Gateway 使用 Redis 存储缓存和限流数据。

| Key 模式 | 用途 | TTL | 数据类型 | 示例 |
|---------|------|-----|---------|------|
| `gateway:apikey:{hash}` | API Key 验证缓存 | 1 小时 | String (JSON) | `gateway:apikey:abc123...` |
| `ratelimit:app:{id}` | 限流计数器 | 1 分钟 | Integer | `ratelimit:app:123` |

### API Key 验证缓存

**Key 格式**：`gateway:apikey:{SHA256(api_key)}`

**Value 格式**：
```json
{
  "valid": true,
  "app_id": 123,
  "app_name": "My Application",
  "allowed_services": ["system", "manager", "meta"],
  "rate_limit_per_minute": 1000,
  "expires_at": "2025-12-31T23:59:59Z"
}
```

**TTL**：1 小时

**用途**：
- 避免每次请求都调用 System API 验证
- 极大提升验证性能（从 20ms → <1ms）
- 与本地缓存配合，实现三层缓存

### 限流计数器

**Key 格式**：`ratelimit:app:{app_id}`

**Value 格式**：整数（当前分钟的请求数）

**TTL**：1 分钟（滑动窗口）

**实现**：
- 使用 Redis INCR 原子操作
- Lua 脚本保证原子性
- 每分钟自动重置

**示例**：
```bash
# 第 1 次请求
INCR ratelimit:app:123  # → 1
EXPIRE ratelimit:app:123 60

# 第 2 次请求
INCR ratelimit:app:123  # → 2

# ...

# 第 1001 次请求（限额 1000）
GET ratelimit:app:123  # → 1001
# 超限，返回 429
```

## 为什么使用专用 SystemClient

### 核心问题

Gateway 需要调用 System 模块验证 API Key，为什么不使用 `common/client/system.go` 而是自己实现 `gateway/pkg/client/system_client.go`？

### 决策理由

#### 1. 职责单一

**Gateway 需求**：
- 只需要验证 API Key（2 个方法）
  - `ValidateAPIKey(keyHash)` - 验证单个 API Key
  - `BulkGetAPIKeys()` - 批量获取 API Keys

**Common SystemClient**：
- 提供 30+ 个方法（GetEngine、ListEngines、CreateEngine、RegisterCapability 等）
- Gateway 完全不需要这些功能

**结论**：使用通用客户端违反**最小依赖原则**

#### 2. 性能优化

| 特性 | Gateway 专用 | Common 通用 |
|------|-------------|-------------|
| HTTP 超时 | 5 秒（快速失败） | 30 秒（适应复杂查询） |
| 缓存策略 | 三层缓存（本地 + Redis + API） | 无内置缓存 |
| 响应速度 | <1ms（缓存 Hit）<br>20ms（缓存 Miss） | 20-30ms（每次调用） |

**结论**：Gateway 是高频低延迟服务，5 秒超时专门为 API Key 验证设计

#### 3. 缓存优化

**Gateway 专用客户端**：
```go
// 三层缓存验证
validateWithCache(keyHash)
  ├─ 本地缓存（5分钟 TTL）→ <1ms
  ├─ Redis 缓存（1小时 TTL）→ <5ms
  └─ System API → <20ms
```

**Common 通用客户端**：
```go
// 无缓存，每次都调用 System API
GetEngine(engineID) → 20-30ms（每次）
```

**结论**：Gateway 的三层缓存深度集成在中间件中，无法用通用客户端替代

#### 4. 依赖隔离

**Gateway 专用客户端**：
```go
import (
    "net/http"
    "encoding/json"
)
// 无外部依赖
```

**Common 通用客户端**：
```go
import (
    "github.com/addp/common/models"  // 依赖所有模型
    commonutils "github.com/addp/common/utils"
)
```

**结论**：Gateway 不需要依赖 common/models 的所有数据模型，减少耦合

#### 5. 接口稳定性

- **API Key 验证接口**：变化极少，一年可能 0-1 次修改
- **Common SystemClient**：随着功能增加频繁变更

**结论**：专用客户端更稳定，不受通用客户端变更影响

### 代码对比

**Gateway 专用 SystemClient（100 行）**：
```go
type SystemClient struct {
    baseURL    string
    httpClient *http.Client  // 5秒超时
    internalKey string       // X-Internal-API-Key
}

func (c *SystemClient) ValidateAPIKey(keyHash string) (*APIKeyValidationResponse, error) {
    url := fmt.Sprintf("%s/internal/api-keys/validate?key_hash=%s", c.baseURL, keyHash)
    // 简单的 HTTP GET 请求
    // 返回验证结果
}

func (c *SystemClient) BulkGetAPIKeys() ([]APIKeyValidationResponse, error) {
    // 批量获取 API Keys（用于预加载缓存）
}
```

**Common SystemClient（700+ 行）**：
```go
type SystemClient struct {
    baseURL     string
    httpClient  *http.Client  // 30秒超时
    authToken   string         // JWT Token
    internalKey string         // Internal Key
}

// 30+ 方法
func (c *SystemClient) GetEngine(engineID uint) (*models.Engine, error) { ... }
func (c *SystemClient) ListEngines(engineType string, tenantID uint) ([]models.Engine, error) { ... }
func (c *SystemClient) CreateEngine(payload *models.CreateEngineRequest) (*models.Engine, error) { ... }
func (c *SystemClient) RegisterCapability(req *models.CapabilityRegistrationRequest) error { ... }
func (c *SystemClient) ListCapabilities(filters map[string]string) ([]*models.Capability, error) { ... }
func (c *SystemClient) ListTaskProviders() ([]*models.TaskProvider, error) { ... }
func (c *SystemClient) ListSchemas(engineID uint) ([]SchemaInfo, error) { ... }
func (c *SystemClient) ListTables(engineID uint, schema string) ([]TableInfo, error) { ... }
func (c *SystemClient) CreateAuditLog(log *models.AuditLogCreateRequest) error { ... }
// ... 还有 20+ 个方法
```

### 最佳实践

**模块专用客户端** 适用于：
- ✅ 职责明确、功能单一的场景（如 Gateway API Key 验证）
- ✅ 性能敏感、高频调用的场景
- ✅ 需要深度缓存优化的场景

**通用客户端** 适用于：
- ✅ 需要多种 System API 的模块（如 Manager、Meta、Develop）
- ✅ 调用频率较低的场景
- ✅ 不需要特殊缓存策略的场景

**遵循 ADDP 开发原则**：
- DRY 不是绝对的，职责隔离更重要
- Gateway 是高频低延迟服务，需要极致性能优化

## 监控和调试

### 性能指标

#### 1. API Key 验证性能

**目标**：
- 本地缓存命中率：> 95%
- Redis 缓存命中率：> 90%
- System API 调用延迟：P99 < 50ms

**监控查询**：
```sql
-- 缓存命中率（过去 1 小时）
SELECT
    COUNT(*) FILTER (WHERE cache_hit = TRUE) * 100.0 / COUNT(*) as cache_hit_rate
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour';
```

#### 2. 限流统计

**监控指标**：
- 被限流的请求数
- Top 10 限流应用
- 限流触发时段分布

**监控查询**：
```sql
-- 被限流的应用排行（过去 1 天）
SELECT
    application_id,
    COUNT(*) as rate_limited_count
FROM gateway.api_access_logs
WHERE rate_limited = TRUE
  AND accessed_at > NOW() - INTERVAL '1 day'
GROUP BY application_id
ORDER BY rate_limited_count DESC
LIMIT 10;
```

#### 3. 访问日志分析

**监控指标**：
- 请求量 Top 10 服务
- 平均响应时间 Top 10 慢路由
- 4xx/5xx 错误率

**监控查询**：
```sql
-- 请求量 Top 10 服务（过去 1 小时）
SELECT
    service_name,
    COUNT(*) as request_count,
    AVG(response_time_ms) as avg_time
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour'
GROUP BY service_name
ORDER BY request_count DESC
LIMIT 10;

-- 慢查询 Top 10（过去 1 小时）
SELECT
    request_method,
    request_path,
    AVG(response_time_ms) as avg_time
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour'
GROUP BY request_method, request_path
ORDER BY avg_time DESC
LIMIT 10;

-- 错误率分析（过去 1 小时）
SELECT
    response_status,
    COUNT(*) as count
FROM gateway.api_access_logs
WHERE accessed_at > NOW() - INTERVAL '1 hour'
  AND response_status >= 400
GROUP BY response_status
ORDER BY count DESC;
```

### 调试命令

#### Redis 缓存调试

```bash
# 连接 Redis
docker exec -it addp-infra-redis redis-cli

# 查看所有 API Key 缓存
KEYS gateway:apikey:*

# 查看特定 API Key 缓存
GET gateway:apikey:abc123...

# 查看 TTL
TTL gateway:apikey:abc123...

# 查看限流状态
GET ratelimit:app:123
TTL ratelimit:app:123

# 清理缓存（调试用）
DEL gateway:apikey:abc123...
```

#### PostgreSQL 日志调试

```bash
# 连接 PostgreSQL
psql -h localhost -p 15432 -U addp -d addp

# 切换到 gateway schema
SET search_path TO gateway;

# 查看最近 100 条访问日志
SELECT
    id,
    application_id,
    api_key_prefix,
    service_name,
    request_method,
    request_path,
    response_status,
    response_time_ms,
    cache_hit,
    rate_limited,
    accessed_at
FROM api_access_logs
ORDER BY accessed_at DESC
LIMIT 100;

# 查看特定应用的访问日志
SELECT * FROM api_access_logs
WHERE application_id = 123
ORDER BY accessed_at DESC
LIMIT 50;

# 查看被限流的请求
SELECT * FROM api_access_logs
WHERE rate_limited = TRUE
ORDER BY accessed_at DESC;
```

#### 健康检查

```bash
# Gateway 健康检查
curl http://localhost:8000/health

# 响应示例
{
  "status": "ok",
  "service": "gateway"
}

# Gateway 服务列表
curl http://localhost:8000/

# 响应示例
{
  "message": "全域数据平台 API Gateway",
  "version": "1.0.0",
  "services": {
    "system": "http://localhost:8180",
    "manager": "http://localhost:8081",
    "meta": "http://localhost:8082",
    "transfer": "http://localhost:8083",
    "develop": "http://localhost:8084",
    "service": "http://localhost:8086",
    "copilot": "http://localhost:8087"
  }
}
```

#### 测试 API Key 验证

```bash
# 测试有效的 API Key
curl -X GET http://localhost:8000/api/manager/engines \
  -H "X-API-Key: sk_live_your_api_key_here"

# 测试无效的 API Key
curl -X GET http://localhost:8000/api/manager/engines \
  -H "X-API-Key: invalid_key"
# 响应: 401 Unauthorized

# 测试无 API Key
curl -X GET http://localhost:8000/api/manager/engines
# 响应: 正常处理（跳过 API Key 验证，可能依赖 JWT）
```

#### 测试限流

```bash
# 发送大量请求触发限流（假设限额 1000/分钟）
for i in {1..1100}; do
  curl -X GET http://localhost:8000/api/manager/engines \
    -H "X-API-Key: sk_live_your_api_key_here"
done

# 第 1001 次请求返回：429 Too Many Requests
```

### 日志查看

```bash
# Gateway 日志（开发模式）
# 直接查看控制台输出

# Gateway 日志（Docker 模式）
docker logs -f gateway

# 过滤特定日志
docker logs gateway | grep "API Key validation"
docker logs gateway | grep "Rate limit exceeded"
```

## 未来扩展

### 1. API 版本管理
支持多个 API 版本并存

```
/api/v1/manager/engines  → Manager v1
/api/v2/manager/engines  → Manager v2
```

### 2. GraphQL 网关集成
统一的 GraphQL 入口

```
POST /graphql
{
  "query": "{ engines { id name } }"
}
```

### 3. WebSocket 代理支持
支持 WebSocket 连接代理

```
wss://gateway.example.com/ws/realtime
  → ws://manager:8081/ws/realtime
```

### 4. gRPC 网关支持
支持 gRPC 协议转换

```
gRPC Client → Gateway (gRPC-HTTP Transcoding) → HTTP Backend
```

### 5. 熔断降级
后端服务故障时自动熔断

```
Manager 服务故障
  → 熔断器打开
  → 返回降级响应
```

### 6. 负载均衡
支持多个后端实例的负载均衡

```
Gateway → Manager Instance 1 (Round Robin)
        → Manager Instance 2
        → Manager Instance 3
```

### 7. 智能路由
基于请求特征的智能路由

```
大文件上传 → Manager 高性能实例
普通查询 → Manager 普通实例
```

### 8. API 文档聚合
聚合所有服务的 API 文档

```
GET /api-docs  → Swagger UI（所有服务的 API）
```

## 总结

Gateway 的核心价值：

1. ✅ **统一入口** - 客户端只需要一个地址
2. ✅ **API Key 认证** - 三层缓存，性能极致优化
3. ✅ **限流控制** - Redis 令牌桶，保护后端服务
4. ✅ **访问日志** - PostgreSQL 持久化，支持审计和分析
5. ✅ **透明代理** - 后端服务无感知
6. ✅ **路径重写** - 支持模块化路由映射
7. ✅ **集中管理** - 跨域、认证、日志统一处理
8. ✅ **灵活扩展** - 易于添加新服务
9. ✅ **生产就绪** - 支持监控、健康检查

Gateway 是微服务架构的**门面**，是系统对外的唯一入口！🚪
