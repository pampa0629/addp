# Gateway 模块说明

本文件为 Claude Code 提供 Gateway 模块的开发指导。

## 📋 核心职责

Gateway 是 ADDP 平台的**统一 API 入口**，负责：

1. **API Key 认证** - 基于三层缓存的验证机制（本地 5分钟 → Redis 1小时 → System API）
2. **请求路由** - 按模块前缀转发到 7 个后端服务（System、Manager、Meta、Transfer、Develop、Service、Copilot）
3. **限流控制** - 基于 Redis 令牌桶算法，按应用 ID 独立限流
4. **访问日志** - 异步记录所有 API 访问到 PostgreSQL
5. **路径重写** - 支持模块化路由映射（Manager、Copilot）
6. **跨域处理** - 统一 CORS 配置

## 🏗️ 关键架构

### API Key 验证流程

```
客户端请求（X-API-Key: xxx）
  ↓
APIKeyAuthMiddleware.Handler()
  ↓
1. 计算 SHA256 hash
   ↓
2. 查询本地缓存（<1ms）
   ├─ Hit → 返回结果
   └─ Miss ↓
   ↓
3. 查询 Redis 缓存（<5ms）
   ├─ Hit → 写入本地缓存 → 返回结果
   └─ Miss ↓
   ↓
4. 调用 System API（/internal/api-keys/validate）
   ↓
5. 反向传播：写入 Redis → 写入本地缓存
   ↓
6. 设置 context: api_key_info, app_id, app_name
```

### 中间件处理链

```
Request
  ↓
[1] CORS Middleware
    ├─ 设置跨域头部
    └─ 处理 OPTIONS 预检
  ↓
[2] API Key Auth Middleware
    ├─ 三层缓存验证
    └─ 设置 context
  ↓
[3] Rate Limiter Middleware
    ├─ Redis 令牌桶检查
    └─ 超限返回 429
  ↓
[4] Access Logger Middleware
    ├─ 记录开始时间
    └─ 异步写入 PostgreSQL
  ↓
[5] Service Proxy
    └─ 转发到后端服务
```

### 为什么使用专用 SystemClient？

Gateway 不使用 `common/client/system.go`，原因：

| 特性 | Gateway 专用 | Common 通用 |
|------|-------------|-------------|
| **功能** | 2 个方法（ValidateAPIKey、BulkGetAPIKeys） | 30+ 个方法 |
| **超时** | 5 秒（快速失败） | 30 秒 |
| **缓存** | 三层缓存（深度集成） | 无内置缓存 |
| **依赖** | 无外部依赖 | 依赖 common/models |
| **代码量** | 100 行 | 700+ 行 |

**核心原因**：
1. **职责单一** - Gateway 只需要 API Key 验证，不需要其他 30+ 个方法
2. **性能优化** - 5 秒超时专门为高频低延迟场景设计
3. **缓存优化** - 三层缓存深度集成在中间件中，无法用通用客户端替代
4. **依赖隔离** - 不依赖 common/models 的所有数据模型

**遵循 ADDP 开发原则**：DRY 不是绝对的，职责隔离更重要

## 📂 代码结构

```
gateway/
├── cmd/gateway/main.go          # 入口文件
├── internal/
│   ├── config/config.go         # 配置管理（7 个服务地址）
│   ├── router/router.go         # 路由配置和中间件链
│   ├── proxy/proxy.go           # HTTP 代理和路径重写
│   ├── middleware/
│   │   ├── api_key_auth.go      # API Key 三层缓存验证
│   │   ├── rate_limiter.go      # Redis 令牌桶限流
│   │   └── access_logger.go     # PostgreSQL 访问日志
│   └── cache/local_cache.go     # 本地缓存（5分钟 TTL）
├── pkg/client/system_client.go  # System API Key 验证客户端
└── docs/gateway架构说明.md      # 详细架构文档
```

## 🗄️ 数据库设计

### PostgreSQL Schema: `gateway`

| 表名 | 文档链接 | 用途 |
|------|---------|------|
| api_access_logs | [gateway架构说明.md#数据库集成](docs/gateway架构说明.md#数据库集成) | API 使用统计、性能分析、缓存命中率、限流审计 |

**关键字段**：
- `application_id` - 应用 ID
- `service_name` - 目标服务（system、manager、meta 等）
- `response_time_ms` - 响应时间（性能监控）
- `cache_hit` - 缓存命中标志
- `rate_limited` - 是否被限流

### Redis Key 规范

| Key 模式 | 用途 | TTL |
|---------|------|-----|
| `gateway:apikey:{hash}` | API Key 验证缓存 | 1 小时 |
| `ratelimit:app:{id}` | 限流计数器 | 1 分钟 |

## 📍 重要文件位置

### 核心中间件
- [api_key_auth.go](internal/middleware/api_key_auth.go) - API Key 三层缓存验证
- [rate_limiter.go](internal/middleware/rate_limiter.go) - Redis 令牌桶限流
- [access_logger.go](internal/middleware/access_logger.go) - 异步访问日志

### 路由和代理
- [router.go](internal/router/router.go) - 路由配置，7 个服务映射，中间件链
- [proxy.go](internal/proxy/proxy.go) - HTTP 代理和路径重写

### 专用客户端
- [system_client.go](pkg/client/system_client.go) - System API Key 验证客户端（100 行）

### 配置管理
- [config.go](internal/config/config.go) - 环境变量加载，7 个服务地址 + DB + Redis

## 🔧 常见开发场景

### 场景 1：添加新的后端服务路由

**需求**：添加 Analytics 模块路由

**步骤**：

1. **在 config.go 添加服务 URL**：
   ```go
   type Config struct {
       // ... 现有配置
       AnalyticsServiceURL string // 新增
   }

   func Load() *Config {
       return &Config{
           // ... 现有配置
           AnalyticsServiceURL: getEnv("ANALYTICS_SERVICE_URL", "http://localhost:8088"),
       }
   }
   ```

2. **在 router.go 创建代理和路由**：
   ```go
   // 创建代理
   analyticsProxy := proxy.NewServiceProxy(cfg.AnalyticsServiceURL)

   // 配置路由
   analyticsGroup := api.Group("/analytics")
   {
       analyticsGroup.Any("/reports", analyticsProxy.Handle)
       analyticsGroup.Any("/reports/*path", analyticsProxy.Handle)
       analyticsGroup.Any("/dashboards", analyticsProxy.Handle)
       analyticsGroup.Any("/dashboards/*path", analyticsProxy.Handle)
   }
   ```

3. **更新文档**：
   - [docs/gateway架构说明.md](docs/gateway架构说明.md) - 路由规则表
   - [README.md](README.md) - 路由规则表

### 场景 2：调试 API Key 验证失败

**问题**：请求返回 401 Unauthorized

**调试步骤**：

1. **查看 Gateway 日志**：
   ```bash
   tail -f logs/gateway.log
   # 查找 "API Key validation failed"
   ```

2. **检查本地缓存**（无法直接查看，检查日志）

3. **检查 Redis 缓存**：
   ```bash
   docker exec -it addp-infra-redis redis-cli
   # 计算 API Key 的 SHA256 hash
   echo -n "sk_live_abc123..." | sha256sum

   # 查看缓存
   GET gateway:apikey:计算的hash值

   # 查看 TTL
   TTL gateway:apikey:计算的hash值
   ```

4. **测试 System API**：
   ```bash
   curl -H "X-Internal-API-Key: your-internal-key" \
     "http://localhost:8180/internal/api-keys/validate?key_hash=计算的hash值"
   ```

5. **清理缓存重试**（如果怀疑缓存问题）：
   ```bash
   docker exec -it addp-infra-redis redis-cli
   DEL gateway:apikey:计算的hash值
   ```

### 场景 3：调试限流问题

**问题**：请求返回 429 Too Many Requests

**调试步骤**：

1. **查看 Redis 限流计数器**：
   ```bash
   docker exec -it addp-infra-redis redis-cli

   # 查看特定应用的计数器
   GET ratelimit:app:123

   # 查看 TTL（应该在 1 分钟内）
   TTL ratelimit:app:123
   ```

2. **查看访问日志**：
   ```sql
   psql -h localhost -p 15432 -U addp -d addp

   SET search_path TO gateway;

   -- 查看被限流的请求
   SELECT application_id, COUNT(*) as rate_limited_count
   FROM api_access_logs
   WHERE rate_limited = TRUE
     AND accessed_at > NOW() - INTERVAL '1 hour'
   GROUP BY application_id;
   ```

3. **临时提高限额**（调试用）：
   - 修改 System 模块中的应用配置
   - 或等待 1 分钟让计数器自动重置

### 场景 4：添加路径重写

**需求**：新模块 Analytics 需要路径重写

**步骤**：

```go
// 在 router.go 中使用 HandleWithPathRewrite
analyticsGroup := api.Group("/analytics")
{
    // 移除 /analytics 前缀
    analyticsGroup.Any("/reports", analyticsProxy.HandleWithPathRewrite("/analytics"))
    analyticsGroup.Any("/reports/*path", analyticsProxy.HandleWithPathRewrite("/analytics"))
}

// 效果：
// 请求: GET /api/analytics/reports/1
// 转发: GET /api/reports/1 (Analytics 服务)
```

## ⚠️ 注意事项

### 1. 不要修改专用 SystemClient
- 保持轻量级（100 行）
- 避免与 Common Client 耦合
- 只保留 API Key 验证功能

### 2. 访问日志异步写入
- 不要阻塞请求流程
- AccessLoggerMiddleware 使用 goroutine 异步写入
- 如果 PostgreSQL 连接失败，只记录错误，不影响请求

### 3. 限流配置审慎
- 限额过低会误伤合法用户
- 限额过高会失去保护作用
- 建议：根据实际流量设置，预留 20% 余量

### 4. 缓存失效
- API Key 撤销后需要主动清理缓存
- 使用 `InvalidateCache(keyHash)` 方法
- 否则缓存会在 TTL 过期前仍然有效（最长 1 小时）

### 5. 路径重写顺序
- 路径重写发生在代理转发之前
- 确保 `HandleWithPathRewrite` 的前缀参数正确
- 测试所有路径重写路由

## 🔗 依赖的其他模块

### System 模块
- **API**: `/internal/api-keys/validate` - API Key 验证
- **认证**: `X-Internal-API-Key` 头部
- **用途**: 验证外部 API Key，返回应用信息和限流配额

### Redis（addp-infra-redis）
- **缓存**: API Key 验证结果（1 小时 TTL）
- **限流**: 令牌桶计数器（1 分钟 TTL）
- **Key 前缀**: `gateway:*`、`ratelimit:*`

### PostgreSQL（addp schema: gateway）
- **访问日志**: `api_access_logs` 表
- **用途**: API 使用统计、性能分析、缓存命中率、限流审计

## 📖 相关文档

- [docs/gateway架构说明.md](docs/gateway架构说明.md) - 完整架构文档（1194 行）
- [README.md](README.md) - 快速开始和路由规则
- [../CLAUDE.md](../CLAUDE.md) - ADDP 平台总览
- [../system/CLAUDE.md](../system/CLAUDE.md) - System 模块（API Key 管理）

## 🚀 性能优化建议

### 1. 缓存命中率优化
- **目标**: 本地缓存命中率 > 95%，Redis 缓存命中率 > 90%
- **监控**: 查询 `api_access_logs` 表的 `cache_hit` 字段
- **优化**: 适当延长本地缓存 TTL（当前 5 分钟）

### 2. 限流算法优化
- **当前**: 简单令牌桶（每分钟固定配额）
- **优化**: 滑动窗口（更平滑的限流）

### 3. 访问日志批量写入
- **当前**: 每次请求单独写入
- **优化**: 批量缓冲写入（降低数据库压力）

### 4. 连接池配置
- **PostgreSQL**: 适当增加连接池大小（高并发场景）
- **Redis**: 使用连接池，避免频繁建立连接

## 🧪 测试建议

### 单元测试
- API Key 验证逻辑（三层缓存）
- 限流算法（令牌桶）
- 路径重写逻辑

### 集成测试
- 完整请求流程（中间件链）
- 多服务路由
- 错误处理

### 性能测试
- 缓存命中率
- 并发请求处理能力
- 限流触发准确性
