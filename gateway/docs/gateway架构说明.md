# Gateway 架构说明

## 📋 目录

1. [Gateway 概述](#gateway-概述)
2. [核心功能](#核心功能)
3. [整体架构](#整体架构)
4. [模块注册与动态路由](#模块注册与动态路由)
5. [路由规则](#路由规则)
6. [数据存储设计](#数据存储设计)
7. [代码结构](#代码结构)
8. [请求流程](#请求流程)
9. [架构决策](#架构决策)

## Gateway 概述

Gateway（API 网关）是全域数据平台的**统一入口**，所有外部请求都通过它进入系统。

### 为什么需要 Gateway？

在微服务架构中，如果没有 Gateway：

```
客户端 → System (8180)     # 直接访问宿主机端口
客户端 → Manager (8081)
客户端 → Meta (8082)
客户端 → Transfer (8083)
客户端 → Develop (8185)
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
                        → Develop (8185)
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

### 1. API Key 认证 🔐

基于三层缓存的 API Key 验证机制，确保高性能和安全性。

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
- 三层缓存极大提升性能（缓存命中率 > 95%）
- 支持服务级别访问控制（allowed_services）
- 支持 API Key 撤销和缓存失效

### 2. 请求路由 🚦

根据 URL 路径将请求转发到对应的后端服务。

| 路由前缀 | 目标服务 | 说明 |
|---------|---------|-----|
| `/api/v1/system/*` | System (8180) | 认证、用户、租户、引擎、日志 |
| `/api/v1/manager/*` | Manager (8081) | 数据源、预览、文件上传 |
| `/api/v1/meta/*` | Meta (8082) | 元数据扫描、对象存储 |
| `/api/v1/transfer/*` | Transfer (8083) | 数据传输任务 |
| `/api/v1/orchestrator/*` | Orchestrator (8084) | 任务编排、工作流编排 |
| `/api/v1/develop/*` | Develop (8185) | SQL 执行、工作流 |
| `/api/v1/service/*` | Service (8086) | 数据服务、OGC 标准 |
| `/api/v1/copilot/*` | Copilot (8087) | AI 助手 |

### 3. 限流控制 ⏱️

基于 Redis 令牌桶算法的限流机制，防止恶意攻击和保护后端服务。

```
每个应用独立限流配额
  ↓
Redis 原子操作（Lua 脚本）
  ↓
超限返回 429 Too Many Requests
```

**特点**：
- 按应用 ID 独立限流
- 每分钟请求数可配置（在 System 模块 Application 管理界面设置）
- Redis 保证分布式一致性
- 自动记录限流事件

### 4. 访问日志 📝

异步记录所有 API 访问到 PostgreSQL `gateway.api_access_logs` 表。

**记录内容**：
- 应用 ID 和 API Key 前缀
- 请求路径、方法、参数
- 响应状态码和响应时间
- 缓存命中情况
- 是否被限流

**用途**：
- API 使用统计和分析
- 性能监控和优化
- 缓存命中率分析
- 限流审计

### 5. 其他功能

- **请求代理**：完整转发 HTTP 请求（方法、头部、体、查询参数）
- **编码透明**：代理不自动解压上游 gzip 响应，原样保留 `Content-Encoding` 与响应字节（例如 PMTiles 内的 gzip MVT）
- **透明代理**：按 `/api/v1/:module/*path` 提取模块名，完整保留请求路径、头、体和查询参数
- **跨域处理**：统一配置 CORS，允许前端跨域访问
- **健康检查**：提供 `/health` 端点检查 Gateway 状态

## 整体架构

### 架构图

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
    │  8180  │ │ 8081 │ │ 8082 │ │ 8083  │ │ 8185  │ │ 8086  │ │ 8087  │
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

| 组件 | 文件位置 | 职责 |
|------|---------|------|
| **Config** | `internal/config/config.go` | 配置管理：从环境变量读取配置、提供默认值 |
| **Router** | `internal/router/router.go` | 路由配置：定义路由规则、创建代理实例、配置中间件链 |
| **Proxy** | `internal/proxy/proxy.go` | HTTP 代理：透明转发请求、保持路径、头部、正文和查询参数完整 |
| **APIKeyAuthMiddleware** | `internal/middleware/api_key_auth.go` | 三层缓存验证 API Key |
| **RateLimiterMiddleware** | `internal/middleware/rate_limiter.go` | Redis 令牌桶限流 |
| **AccessLoggerMiddleware** | `internal/middleware/access_logger.go` | 异步记录访问日志 |
| **SystemClient** | `pkg/client/system_client.go` | 专用客户端：验证 API Key（5秒超时） |
| **LocalCache** | `internal/cache/local_cache.go` | 本地缓存：5分钟 TTL、自动清理 |
| **ModuleDiscovery** | `internal/module_discovery.go` | 模块发现：从 System 动态加载模块列表、创建代理 |

## 模块注册与动态路由

Gateway 只保留一条模块路由主路径：System 使用配置中的 `SYSTEM_URL` 作为模块注册表的 bootstrap 目标，所有 `/api/v1/system/**` 请求透明转发到该目标；其他模块统一通过 System 模块注册表动态发现，不保留硬编码模块 fallback。

### 模块注册表（system.module_registry）

**表名**：`system.module_registry`

**用途**：System 模块维护的模块注册中心，记录所有已注册模块的信息，供 Gateway 动态发现和路由。

**核心字段**：

| 字段名 | 类型 | 说明 | 示例 |
|-------|------|------|------|
| module_name | VARCHAR(50) | 模块名称（唯一索引） | `manager` |
| module_url | VARCHAR(255) | 模块 URL | `http://localhost:8081` |
| route_prefix | VARCHAR(50) | 路由前缀 | `/manager` |
| health_check_url | VARCHAR(255) | 健康检查端点 | `http://localhost:8081/health` |
| status | VARCHAR(20) | 状态（up/down） | `up` |
| last_heartbeat | TIMESTAMP | 最后心跳时间 | `2025-01-24 10:30:15` |
| metadata | JSONB | 扩展信息（版本、权重） | `{"version": "0.0.24"}` |

**索引**：
- `idx_module_registry_status` - 状态索引（加速查询活跃模块）
- `idx_module_registry_heartbeat` - 心跳索引（加速超时检测）

### 模块注册流程

```
1. 模块启动
   - 调用 System API: POST /api/v1/internal/modules/register
   - 传入：module_name, module_url, route_prefix, health_check_url
   ↓
2. System 模块写入 module_registry 表（幂等操作）
   - 如果模块已存在，更新 URL 和 metadata
   - 如果模块不存在，创建新记录
   - 初始状态设为 'up'
   ↓
3. 模块定期发送心跳
   - 调用 System API: POST /api/v1/internal/modules/heartbeat
   - System 更新 last_heartbeat 字段
   ↓
4. System 定时任务（每 30 秒）检查超时模块
   - 如果 last_heartbeat 超时（默认 2 分钟）
   - 将 status 从 'up' 改为 'down'
```

### Gateway 动态路由流程

```
1. Gateway 启动
   - 使用 `SYSTEM_URL` 创建 System bootstrap 代理
   - 创建其他模块使用的 ModuleDiscovery 实例
   ↓
2. 初始化模块列表
   - 调用 System API: GET /api/v1/internal/modules?status=up
   - 获取所有活跃模块（status='up'）
   ↓
3. 构建动态路由映射
   - 为每个模块创建 ServiceProxy：map[module_name]*ServiceProxy
   - 存储模块信息：map[module_name]*ModuleInfo
   ↓
4. 启动定时刷新任务
   - 定期刷新模块列表（MODULE_REFRESH_INTERVAL，默认 30 秒）
   - 检测模块变更：
     - 新增模块 → 创建新代理
     - 模块 URL 变更 → 重建代理
     - 模块下线（status='down'） → 删除代理
   ↓
5. 请求路由
   - `/api/v1/system/**` 始终使用 System bootstrap 代理
   - 其他请求示例：GET /api/v1/manager/engines
   - Gateway 提取 module_name = "manager"
   - 从 ModuleDiscovery 获取对应的 ServiceProxy
   - 如果模块存在且 status='up' → 转发请求
   - 如果模块不存在或 status='down' → 返回 503 Service Unavailable
   - 模块不存在、状态为 down 或模块发现暂时不可用时返回 503，不绕过注册表使用硬编码地址
```

### 动态路由的核心价值

| 价值点 | 说明 |
|--------|------|
| **动态扩展** | 新增模块无需修改 Gateway 代码，只需注册到 System 即可自动路由 |
| **自动故障切换** | 模块宕机或心跳超时自动标记为 'down'，Gateway 不再转发请求 |
| **健康监控** | 通过心跳机制实时监控模块状态，及时发现服务故障 |
| **负载均衡基础** | 未来可支持同一模块的多实例（通过 metadata 存储权重信息） |
| **灰度发布基础** | 未来可支持同一模块的多个版本（v1/v2），通过 metadata 控制流量分配 |
| **配置热更新** | 模块 URL 变更无需重启 Gateway，定时刷新自动生效 |

### 配置示例

**System bootstrap 与动态路由**（`.env`）：
```bash
SYSTEM_URL=http://localhost:8180    # System bootstrap 唯一目标
MODULE_REFRESH_INTERVAL=30s         # 刷新间隔（默认 30 秒）
```

## 路由规则

### 路由表

| 路由前缀 | 目标服务 | 端口 | 认证要求 | 转发方式 | 说明 |
|---------|---------|------|---------|---------|-----|
| `/api/v1/system/*` | System bootstrap | 8180 | 由 System 按具体端点认证 | 透明转发 | 登录、会话、OAuth、AuthContext、用户、租户、引擎和日志 |
| `/api/v1/{module}/*` | System 注册表中的活跃模块 | 动态发现 | Bearer Token 或 API Key | 透明转发 | 自动覆盖 Manager、Meta、Transfer、Develop、Service、Copilot 等所有注册模块 |
| `/api/query/*`、`/ogc/*`、`/wmts/*`、`/tiles/*` | Service | 动态发现 | 由 Service 端点决定 | 透明转发 | 数据服务公开协议入口 |

### 透明代理示例

```
请求: GET http://localhost:8000/api/v1/manager/engines
转发: GET http://localhost:8081/api/v1/manager/engines
```

### 路由匹配规则

Gateway 使用 **前缀匹配**，支持通配符和查询参数透传：

```
请求: GET /api/v1/system/users/123
匹配: /api/v1/system/users/*
代理到: http://localhost:8180/api/v1/system/users/123

请求: POST /api/v1/manager/engines?type=postgresql
匹配: /api/v1/manager/engines/*
代理到: http://localhost:8081/api/v1/manager/engines?type=postgresql
```

## 数据存储设计

### PostgreSQL Schema: gateway

**说明**：Gateway 在数据库初始化脚本中预留了独立的 `gateway` schema，用于存储访问日志等数据。

#### 访问日志（api_access_logs）

**实现状态**：⚠️ **当前未实现**

Gateway 有访问日志中间件的代码实现（`internal/middleware/access_logger.go`），设计用于记录 API Key 访问审计和性能监控，但由于未执行 GORM AutoMigrate，`gateway.api_access_logs` 表实际上不存在，访问日志功能未启用。

**设计用途**（未来实现）：
- 记录 API Key 访问信息（application_id, api_key_prefix）
- 监控缓存性能（cache_hit 指标）
- 审计限流事件（rate_limited 指标）
- 分析响应时间（response_time_ms）

**与 System AuditLog 的关系**：
- **System AuditLog**（`system.audit_logs`）：已实现，记录**用户业务操作审计**（谁做了什么操作）
  - 使用范围：Manager、Meta、Transfer、Develop、Service、Orchestrator 模块
  - 记录内容：用户信息、HTTP 请求、资源类型、错误追踪
  - 记录范围：仅非 GET 请求（创建、更新、删除等修改操作）
  - 用途：合规审计、问题追溯
- **Gateway AccessLog**（未实现）：设计用于记录 **API Key 访问审计和性能监控**
  - 使用范围：仅 Gateway 模块
  - 记录内容：API Key 信息、服务路由、缓存命中、限流状态
  - 记录范围：所有请求（包括 GET）
  - 用途：性能分析、限流审计、缓存优化

**两者关系**：互补而非替代。System AuditLog 关注"谁做了什么"，Gateway AccessLog 关注"API Key 访问性能和限流"。

### PostgreSQL Schema: system.module_registry

详见上文 [模块注册与动态路由](#模块注册与动态路由) 章节。

### Redis Key 设计

Gateway 使用 Redis 存储缓存和限流数据。

| Key 模式 | 用途 | TTL | 数据类型 | 示例 Value |
|---------|------|-----|---------|-----------|
| `gateway:apikey:{hash}` | API Key 验证缓存 | 1 小时 | String (JSON) | `{"valid": true, "app_id": 123, ...}` |
| `ratelimit:app:{id}` | 限流计数器 | 1 分钟 | Integer | `45`（当前分钟请求数） |

#### API Key 验证缓存

**Key 格式**：`gateway:apikey:{SHA256(api_key)}`

**Value 格式**：JSON 字符串
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

**用途**：避免每次请求都调用 System API，极大提升验证性能（从 20ms → <1ms）

#### 限流计数器

**Key 格式**：`ratelimit:app:{app_id}`

**Value 格式**：整数（当前分钟的请求数）

**实现**：
- 使用 Redis INCR 原子操作
- Lua 脚本保证原子性
- 每分钟自动重置（TTL 60秒）

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
│   ├── gateway架构说明.md       # 本文档
│   ├── gateway鉴权限流设计（未实现）.md  # 未实现功能设计
│   └── gateway运维手册.md       # 监控和调试指南
├── go.mod                        # Go 模块定义
├── go.sum                        # 依赖校验
└── Dockerfile                    # Docker 镜像构建
```

## 请求流程

### 带 API Key 的请求流程

```
1. 客户端发起请求
   GET http://localhost:8000/api/v1/manager/engines
   Headers: X-API-Key: sk_live_abc123...

2. Gateway 接收 → CORS 处理

3. API Key 认证中间件
   - 提取 API Key 并计算 SHA256 hash
   - 三层缓存验证：本地缓存 → Redis 缓存 → System API
   - 将验证结果存入 gin context

4. 限流中间件
   - 读取 app_id
   - Redis 令牌桶检查
   - 通过或返回 429

5. 访问日志中间件
   - 记录请求开始时间

6. 路由匹配 → 代理转发
   - 匹配规则: /api/v1/manager/* → managerProxy
   - 转发到: http://localhost:8081/api/v1/manager/engines

7. Manager 服务处理并返回

8. Gateway 返回响应
   - 访问日志异步写入 PostgreSQL
   - 返回给客户端
```

### 限流触发流程

```
客户端发起第 1001 次请求（限额 1000/分钟）
  ↓
APIKeyAuthMiddleware（缓存 Hit，<1ms）
  ↓
RateLimiterMiddleware
  ├─ Redis INCR ratelimit:app:123 → 1001
  └─ 1001 > 1000 → 返回 429 Too Many Requests
  ↓
AccessLoggerMiddleware
  └─ 记录: rate_limited=true, response_status=429
  ↓
响应: {"error": "Rate limit exceeded", "limit": 1000}
```

### 公开路由（登录）

```
POST http://localhost:8000/api/v1/system/login
Body: {"username": "admin", "password": "123456"}
  ↓
CORS 中间件处理
  ↓
路由匹配：/api/v1/system/login（公开路由，无需 API Key）
  ↓
代理转发：http://localhost:8180/api/v1/system/login
  ↓
System 服务验证用户名密码
  ↓
返回短期 opaque Access Token，并设置 HttpOnly Refresh Token Cookie
```

## 架构决策

### 为什么使用专用 SystemClient

Gateway 需要调用 System 模块验证 API Key，为什么不使用 `common/client/system.go` 而是自己实现 `gateway/pkg/client/system_client.go`？

#### 1. 职责单一

**Gateway 需求**：
- 只需要 2 个方法：`ValidateAPIKey(keyHash)` 和 `BulkGetAPIKeys()`

**Common SystemClient**：
- 提供 30+ 个方法（GetEngine、ListEngines、CreateEngine 等）
- Gateway 完全不需要这些功能

**结论**：使用通用客户端违反**最小依赖原则**

#### 2. 性能优化

| 特性 | Gateway 专用 | Common 通用 |
|------|-------------|-------------|
| HTTP 超时 | 5 秒（快速失败） | 30 秒（适应复杂查询） |
| 缓存策略 | 三层缓存（本地 + Redis + API） | 无内置缓存 |
| 响应速度 | <1ms（缓存 Hit）<br>20ms（缓存 Miss） | 20-30ms（每次调用） |

**结论**：Gateway 是高频低延迟服务，5 秒超时专门为 API Key 验证设计

#### 3. 缓存深度集成

**Gateway 专用客户端**：
```
validateWithCache(keyHash)
  ├─ 本地缓存（5分钟 TTL）→ <1ms
  ├─ Redis 缓存（1小时 TTL）→ <5ms
  └─ System API → <20ms
```

**Common 通用客户端**：
```
无缓存，每次都调用 System API → 20-30ms
```

**结论**：Gateway 的三层缓存深度集成在中间件中，无法用通用客户端替代

#### 4. 依赖隔离

**Gateway 专用客户端**：
- 无外部依赖，仅使用标准库 `net/http` 和 `encoding/json`
- 代码仅 100 行

**Common 通用客户端**：
- 依赖 `common/models` 的所有数据模型
- 代码 700+ 行

**结论**：Gateway 不需要依赖 common/models 的所有数据模型，减少耦合

#### 5. 接口稳定性

- **API Key 验证接口**：变化极少，一年可能 0-1 次修改
- **Common SystemClient**：随着功能增加频繁变更

**结论**：专用客户端更稳定，不受通用客户端变更影响

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

## 相关文档

### 模块文档

- [gateway鉴权限流设计（未实现）.md](gateway鉴权限流设计（未实现）.md) - Service 数据服务鉴权、服务注册改造、未来扩展功能
- [gateway运维手册.md](gateway运维手册.md) - 性能监控、调试命令、常见问题排查

### 平台文档

- [ADDP 开发原则](../../docs/spec/addp开发原则.md) - 平台级开发原则和规范
- [ADDP 各模块简要介绍](../../docs/concepts/addp各模块功能介绍.md) - 平台核心概念辨析
- [System 模块说明](../../system/CLAUDE.md) - System 模块的架构和功能

---

## 总结

Gateway 的核心价值：

1. ✅ **统一入口** - 客户端只需要一个地址
2. ✅ **API Key 认证** - 三层缓存，性能极致优化（缓存命中率 > 95%）
3. ✅ **限流控制** - Redis 令牌桶，保护后端服务
4. ✅ **访问日志** - PostgreSQL 持久化，支持审计和分析
5. ✅ **透明代理** - 后端服务无感知
6. ✅ **透明代理** - 保持模块 API 契约一致，不在 Gateway 改写模块路径
7. ✅ **集中管理** - 跨域、认证、日志统一处理
8. ✅ **灵活扩展** - 易于添加新服务
9. ✅ **生产就绪** - 支持监控、健康检查

Gateway 是微服务架构的**门面**，是系统对外的唯一入口！🚪
