# Gateway 网关模块

> 全域数据平台的统一 API 入口和路由服务

## 🎯 核心功能

- **统一入口**: 为所有平台服务提供单一 API 入口点
- **智能路由**: 根据请求路径自动路由到对应的内部服务
- **API Key 认证**: 基于三层缓存的 API Key 验证机制（本地缓存 → Redis → System API）
- **限流控制**: Redis 令牌桶算法，按应用 ID 独立限流
- **访问日志**: 异步记录已验证的外部 API Key 访问到 PostgreSQL，支持性能、缓存和限流分析
- **CORS 支持**: 处理跨域请求，支持前端访问
- **健康检查**: 监控后端服务状态

## 🚀 快速开始

### 前置要求

- Go 1.21+
- System 后端运行中；其他模块通过 System 注册表发现

### 运行网关

```bash
cd gateway
go run cmd/gateway/main.go
```

访问: http://localhost:8000

### Docker 部署

```bash
make build
make build-images IMAGE_BUILD_ARGS="--services gateway"
```

平台镜像只允许通过仓库根标准入口构建；Gateway 使用 `gateway/Dockerfile.prebuilt`，不维护源码构建 Dockerfile。

## 🔀 路由规则

Gateway 根据 URL 路径前缀自动路由请求：

| 请求路径 | 目标服务 | 服务地址 | 认证要求 | 转发方式 | 用途 |
|---------|---------|---------|---------|---------|-----|
| `/api/v1/system/*` | System | `SYSTEM_URL` | 由 System 端点决定 | 透明转发 | 登录、会话、OAuth、AuthContext 和系统管理 |
| `/api/v1/{module}/*` | 注册模块 | System 模块注册表 | Bearer Token 或 API Key | 动态透明转发 | 在所有有效 Backend 租约间按请求轮询；不自动重试失败请求 |
| `/api/query/*`、`/ogc/*`、`/wmts/*`、`/tiles/*` | Service | System 模块注册表 | 由 Service 端点决定 | 动态透明转发 | 数据服务公开协议入口 |

### 转发说明

Gateway 使用 `/api/v1/:module/*path` 提取模块名，并将完整请求路径透明转发到目标服务：

```
请求: GET http://localhost:8000/api/v1/manager/engines
转发: GET http://localhost:8081/api/v1/manager/engines
```

### 健康检查

- `GET /health` - 网关健康状态

## ⚙️ 环境配置

```bash
# 网关端口
PORT=8000

# System bootstrap 和模块发现
SYSTEM_URL=http://localhost:8180
MODULE_REFRESH_INTERVAL=30s

# 数据库配置（用于访问日志）
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=addp
POSTGRES_PASSWORD=addp_password
POSTGRES_DB=addp
DB_SCHEMA=gateway

# Redis 配置（用于缓存和限流）
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis
REDIS_DB=0

# Gateway Platform Service Principal
GATEWAY_SERVICE_CLIENT_SECRET=your-gateway-service-client-secret

# 运行环境
ENV=development  # development / production
```

## 🔐 认证流程

Gateway 支持两种认证方式：

### 1. API Key 认证（推荐用于外部 API 调用）

```
客户端发送请求携带 X-API-Key 头部
  ↓
Gateway 三层缓存验证：
  ├─ 本地缓存（5分钟 TTL，<1ms）
  ├─ Redis 缓存（1小时 TTL，<5ms）
  └─ System API 验证（<20ms）
  ↓
验证通过，转发到后端服务
```

**特点**：
- SHA256 哈希存储，安全可靠
- 三层缓存极大提升性能
- 支持服务级别访问控制
- 自动限流和访问日志

### 2. Bearer Token 认证

```
前端/模块发送请求携带 Authorization: Bearer <token>
  ↓
Gateway 透明传递认证信息
  ↓
后端服务通过 System AuthContext 验证 opaque Token
```

## 🌐 访问方式

### 开发环境

**通过 Gateway 访问** (推荐):
```bash
# 所有服务通过统一入口访问
curl http://localhost:8000/api/v1/system/login
curl http://localhost:8000/api/v1/manager/engines
curl http://localhost:8000/api/v1/meta/scan/tasks
```

**直接访问服务**:
```bash
# 也可以直接访问各个服务
curl http://localhost:8180/api/v1/system/login    # System
curl http://localhost:8081/api/v1/manager/engines   # Manager
curl http://localhost:8082/api/v1/meta/scan/tasks      # Meta
```

### 生产环境

生产环境建议只暴露 Gateway 端口（8000），隐藏内部服务端口。

## 🔧 功能特性

### 请求透传

Gateway 完整保留并转发：
- ✅ HTTP 方法（GET、POST、PUT、DELETE 等）
- ✅ 请求头部（Authorization、Content-Type 等）
- ✅ 请求体（JSON、表单数据等）
- ✅ 查询参数（?key=value）
- ✅ 响应状态码
- ✅ 响应头部
- ✅ 响应体

### CORS 处理

自动处理跨域请求：
- 预检请求（OPTIONS）自动响应
- 设置正确的 CORS 头部
- 支持多个前端域名

### 错误处理

- 503 Service Unavailable - 后端服务不可达
- 502 Bad Gateway - 后端服务响应错误
- 500 Internal Server Error - 网关内部错误

## 🐛 常见问题

### 1. Gateway 启动后无法访问？

检查：
- Gateway 是否成功启动（查看日志）
- 端口 8000 是否被占用：`lsof -i :8000`
- 防火墙是否开放端口

### 2. 请求返回 503 错误？

检查：
- 目标后端服务是否启动
- System 的 `SYSTEM_URL` 是否正确
- 目标模块是否已注册、心跳是否正常且状态为 `up`
- 网络连接是否正常

### 3. CORS 错误？

确保：
- 前端域名已添加到 `CORS_ALLOWED_ORIGINS`
- 浏览器控制台显示的域名与配置匹配
- Gateway 日志中 CORS 中间件已加载

### 4. API Key 验证失败？

检查：
- 请求是否携带 `X-API-Key` 头部
- API Key 是否有效且未撤销
- System 服务是否正常运行
- Redis 缓存是否正常连接

调试方法：
```bash
# 查看 Redis 缓存
docker exec -it addp-infra-redis redis-cli
KEYS gateway:apikey:*

# 查看 System 日志
tail -f logs/system-backend.log
```

### 5. 限流不生效？

检查：
- Redis 连接是否正常
- API Key 验证是否成功（限流依赖 API Key 信息）
- 查看 Redis 限流计数器：
```bash
docker exec -it addp-infra-redis redis-cli
GET ratelimit:app:123
```

### 6. 访问日志缺失？

检查：
- PostgreSQL 连接是否正常
- gateway schema 是否存在
- 查看数据库日志：
```bash
psql -h localhost -p 15432 -U addp -d addp
SET search_path TO gateway;
SELECT COUNT(*) FROM api_access_logs;
```

### 7. 如何添加新的路由规则？

请参考技术文档 [docs/gateway架构说明.md](./docs/gateway架构说明.md) 中的"添加新路由"章节。

## 📊 监控和日志

Gateway 会记录：
- 已验证 API Key 请求的路由信息（不采集请求体，敏感 Query 值会脱敏）
- 代理错误和异常
- 后端服务响应时间
- CORS 预检请求

查看日志：
```bash
# 开发模式下直接查看控制台输出

# Docker 模式
docker logs -f gateway
```

## 📚 更多文档

- **技术架构**: [docs/gateway架构说明.md](./docs/gateway架构说明.md) - 详细的技术实现和开发指南
- **模块指南**: [CLAUDE.md](./CLAUDE.md) - Gateway 模块开发指导
- **项目总览**: [../CLAUDE.md](../CLAUDE.md) - 完整平台架构

## 📄 License

Copyright © 2025 ADDP Team
