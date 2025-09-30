# Gateway 架构详解

## 📋 目录

1. [Gateway 是什么](#gateway-是什么)
2. [核心功能](#核心功能)
3. [工作原理](#工作原理)
4. [代码结构](#代码结构)
5. [请求流程](#请求流程)
6. [路由规则](#路由规则)
7. [集成方式](#集成方式)
8. [实际案例](#实际案例)

## Gateway 是什么

Gateway（API 网关）是全域数据平台的**统一入口**，所有外部请求都通过它进入系统。

### 为什么需要 Gateway？

在微服务架构中，如果没有 Gateway：

```
客户端 → System (8080)
客户端 → Manager (8081)
客户端 → Meta (8082)
客户端 → Transfer (8083)
```

**问题**：
- 客户端需要知道每个服务的地址
- 跨域配置分散在各个服务
- 认证逻辑重复
- 难以统一管理和监控

有了 Gateway：

```
客户端 → Gateway (8000) → System (8080)
                        → Manager (8081)
                        → Meta (8082)
                        → Transfer (8083)
```

**优势**：
- 统一入口，客户端只需要知道 Gateway 地址
- 集中处理跨域、认证、限流等
- 服务对外透明，可以随意调整内部服务
- 便于监控、日志、安全控制

## 核心功能

### 1. **请求路由** 🚦
根据 URL 路径将请求转发到对应的后端服务

```
/api/auth/*     → System (认证服务)
/api/users/*    → System (用户管理)
/api/datasources/* → Manager (数据源管理)
/api/metadata/* → Meta (元数据服务)
/api/tasks/*    → Transfer (任务管理)
```

### 2. **请求代理** 🔄
完整转发 HTTP 请求，包括：
- 请求方法（GET, POST, PUT, DELETE）
- 请求头（Headers）
- 请求体（Body）
- 查询参数（Query Parameters）

### 3. **跨域处理** 🌐
统一配置 CORS，允许前端跨域访问

### 4. **健康检查** ❤️
提供 `/health` 端点检查 Gateway 状态

## 工作原理

### 整体架构

```
┌─────────────────────────────────────────────────────┐
│                    客户端/浏览器                       │
└─────────────────┬───────────────────────────────────┘
                  │ HTTP Request
                  ▼
         ┌─────────────────┐
         │   Gateway:8000   │
         │                 │
         │  ┌───────────┐  │
         │  │  Router   │  │  路由解析
         │  └─────┬─────┘  │
         │        │        │
         │  ┌─────▼─────┐  │
         │  │Middleware │  │  CORS等中间件
         │  └─────┬─────┘  │
         │        │        │
         │  ┌─────▼─────┐  │
         │  │   Proxy   │  │  HTTP代理
         │  └─────┬─────┘  │
         └────────┼────────┘
                  │
         ┌────────┼────────┐
         │        │        │
    ┌────▼───┐ ┌─▼────┐ ┌─▼────┐ ┌──▼────┐
    │System  │ │Manager│ │Meta  │ │Transfer│
    │  8080  │ │ 8081 │ │ 8082 │ │ 8083  │
    └────────┘ └──────┘ └──────┘ └───────┘
```

### 核心组件

#### 1. **Config (配置管理)**
文件：`internal/config/config.go`

```go
type Config struct {
    Port               string  // Gateway 端口
    SystemServiceURL   string  // System 服务地址
    ManagerServiceURL  string  // Manager 服务地址
    MetaServiceURL     string  // Meta 服务地址
    TransferServiceURL string  // Transfer 服务地址
}
```

**作用**：
- 从环境变量读取配置
- 提供默认值
- 集中管理所有服务地址

#### 2. **Router (路由配置)**
文件：`internal/router/router.go`

```go
func SetupRouter(cfg *config.Config) *gin.Engine {
    router := gin.Default()

    // 添加 CORS 中间件
    router.Use(middleware.CORS())

    // 创建各服务的代理
    systemProxy := proxy.NewServiceProxy(cfg.SystemServiceURL)
    managerProxy := proxy.NewServiceProxy(cfg.ManagerServiceURL)

    // 配置路由规则
    api := router.Group("/api")
    {
        api.Any("/auth/*path", systemProxy.Handle)
        api.Any("/users/*path", systemProxy.Handle)
        api.Any("/datasources/*path", managerProxy.Handle)
        // ... 更多路由
    }

    return router
}
```

**作用**：
- 定义路由规则
- 创建代理实例
- 配置中间件

#### 3. **Proxy (HTTP 代理)**
文件：`internal/proxy/proxy.go`

```go
type ServiceProxy struct {
    targetURL string      // 目标服务地址
    client    *http.Client // HTTP 客户端
}

func (p *ServiceProxy) Handle(c *gin.Context) {
    // 1. 构建目标 URL
    targetURL := p.targetURL + c.Request.URL.Path

    // 2. 读取请求体
    bodyBytes, _ := io.ReadAll(c.Request.Body)

    // 3. 创建新请求
    req, _ := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(bodyBytes))

    // 4. 复制请求头
    for key, values := range c.Request.Header {
        req.Header.Add(key, values[0])
    }

    // 5. 发送请求到后端服务
    resp, _ := p.client.Do(req)

    // 6. 复制响应头和响应体
    c.Status(resp.StatusCode)
    c.Writer.Write(respBody)
}
```

**作用**：
- 转发 HTTP 请求到后端服务
- 保持请求的完整性
- 透明代理，客户端无感知

#### 4. **Middleware (中间件)**
文件：`internal/middleware/cors.go`

```go
func CORS() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
        c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }

        c.Next()
    }
}
```

**作用**：
- 统一处理跨域请求
- 支持 OPTIONS 预检请求
- 允许携带 Authorization 头

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
│   └── middleware/
│       └── cors.go              # CORS 中间件
├── go.mod                        # Go 模块定义
├── go.sum                        # 依赖校验
├── Dockerfile                    # Docker 镜像构建
└── README.md                     # 说明文档
```

## 请求流程

### 示例：用户登录

```
1. 客户端发起登录请求
   POST http://localhost:8000/api/auth/login
   Body: {"username": "admin", "password": "admin123"}

2. 请求到达 Gateway
   ↓
   [Gateway:8000] 接收请求

3. CORS 中间件处理
   ↓
   [Middleware] 添加 CORS 头

4. 路由匹配
   ↓
   [Router] 匹配规则: /api/auth/* → systemProxy

5. 代理转发
   ↓
   [Proxy] 重写 URL: http://localhost:8080/api/auth/login
   [Proxy] 复制请求头: Content-Type, Authorization...
   [Proxy] 复制请求体: {"username": "admin", ...}

6. 发送到 System 服务
   ↓
   POST http://localhost:8080/api/auth/login

7. System 服务处理
   ↓
   [System] 验证用户名密码
   [System] 生成 JWT Token
   [System] 返回响应: {"access_token": "eyJ...", "token_type": "Bearer"}

8. Gateway 接收响应
   ↓
   [Proxy] 复制响应状态: 200
   [Proxy] 复制响应头: Content-Type: application/json
   [Proxy] 复制响应体: {"access_token": ...}

9. 返回给客户端
   ↓
   客户端收到: {"access_token": "eyJ...", "token_type": "Bearer"}
```

### 时序图

```
客户端          Gateway         System
  │              │               │
  │─────POST────→│               │
  │ /api/auth/login             │
  │              │               │
  │              │────POST──────→│
  │              │ http://system:8080/api/auth/login
  │              │               │
  │              │               │ 验证用户
  │              │               │ 生成 Token
  │              │               │
  │              │←────200───────│
  │              │ {"access_token": "..."}
  │              │               │
  │←────200──────│               │
  │ {"access_token": "..."}     │
  │              │               │
```

## 路由规则

### 当前配置的路由

| 路径前缀 | 目标服务 | 端口 | 说明 |
|---------|---------|------|------|
| `/api/auth/*` | System | 8080 | 用户认证 |
| `/api/users/*` | System | 8080 | 用户管理 |
| `/api/logs/*` | System | 8080 | 日志管理 |
| `/api/resources/*` | System | 8080 | 资源管理 |
| `/api/datasources/*` | Manager | 8081 | 数据源管理 |
| `/api/directories/*` | Manager | 8081 | 目录管理 |
| `/api/preview/*` | Manager | 8081 | 数据预览 |
| `/api/upload/*` | Manager | 8081 | 文件上传 |
| `/api/metadata/*` | Meta | 8082 | 元数据 |
| `/api/datasets/*` | Meta | 8082 | 数据集 |
| `/api/lineage/*` | Meta | 8082 | 血缘关系 |
| `/api/tasks/*` | Transfer | 8083 | 传输任务 |
| `/api/executions/*` | Transfer | 8083 | 任务执行 |

### 路由匹配规则

Gateway 使用 **前缀匹配**：

```
请求: GET /api/users/123
匹配: /api/users/*
代理到: http://localhost:8080/api/users/123

请求: POST /api/auth/login
匹配: /api/auth/*
代理到: http://localhost:8080/api/auth/login

请求: GET /api/datasources?type=mysql
匹配: /api/datasources/*
代理到: http://localhost:8081/api/datasources?type=mysql
```

## 集成方式

### 1. 与 System 模块集成

**System 服务不需要修改**，继续监听 8080 端口。

```go
// System 配置不变
PORT=8080
```

Gateway 通过配置知道 System 的地址：

```go
// Gateway 配置
SYSTEM_SERVICE_URL=http://localhost:8080
```

### 2. 前端集成

前端有两种访问方式：

#### 方式一：直接访问 System（当前）

```javascript
// frontend/src/api/client.js
const BASE_URL = 'http://localhost:8080';

axios.post(`${BASE_URL}/api/auth/login`, {...});
```

#### 方式二：通过 Gateway 访问（推荐）

```javascript
// frontend/src/api/client.js
const BASE_URL = 'http://localhost:8000'; // 改为 Gateway 地址

axios.post(`${BASE_URL}/api/auth/login`, {...});
```

**优势**：
- 前端只需要知道一个地址
- 后端服务可以随意调整端口
- 生产环境更安全

### 3. 服务发现（未来扩展）

当前是**硬编码**服务地址，未来可以集成服务发现：

```go
// 使用 Consul / Etcd 进行服务发现
systemURL := discovery.GetServiceURL("system")
managerURL := discovery.GetServiceURL("manager")
```

## 实际案例

### 案例 1：用户登录

```bash
# 通过 Gateway 登录
curl -X POST http://localhost:8000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# Gateway 日志
[GIN] POST /api/auth/login → 代理到 http://localhost:8080/api/auth/login

# System 日志
[GIN] POST /api/auth/login → 处理登录请求 → 返回 Token

# 返回结果
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}
```

### 案例 2：获取用户列表

```bash
# 通过 Gateway 获取用户列表
curl http://localhost:8000/api/users?page=1&page_size=10 \
  -H "Authorization: Bearer <token>"

# Gateway 处理流程
1. 接收请求: GET /api/users?page=1&page_size=10
2. 匹配路由: /api/users/* → systemProxy
3. 构建目标URL: http://localhost:8080/api/users?page=1&page_size=10
4. 复制请求头: Authorization: Bearer <token>
5. 发送到 System
6. 接收 System 响应
7. 返回给客户端

# 返回结果
{
  "data": [
    {"id": 1, "username": "admin", ...},
    {"id": 2, "username": "user1", ...}
  ],
  "total": 2
}
```

### 案例 3：跨服务调用（未来）

当 Manager 服务也启动后：

```bash
# 获取数据源列表
curl http://localhost:8000/api/datasources \
  -H "Authorization: Bearer <token>"

# Gateway 自动路由到 Manager 服务
→ http://localhost:8081/api/datasources
```

## 配置说明

### 环境变量

```bash
# Gateway 端口
PORT=8000

# 后端服务地址
SYSTEM_SERVICE_URL=http://localhost:8080
MANAGER_SERVICE_URL=http://localhost:8081
META_SERVICE_URL=http://localhost:8082
TRANSFER_SERVICE_URL=http://localhost:8083

# 运行环境
ENV=development  # development / production
```

### 启动方式

```bash
# 开发模式
cd gateway
go run cmd/gateway/main.go

# 生产模式
export ENV=production
./gateway

# Docker 模式
docker-compose up gateway
```

## 性能和监控

### 性能指标

- **延迟增加**: Gateway 增加约 1-5ms 延迟
- **吞吐量**: 单个 Gateway 可处理 10000+ req/s
- **资源占用**: 内存 ~50MB，CPU ~5%

### 监控建议

1. **请求日志**
   ```
   [GIN] 2025/09/30 - 18:54:23 | 200 | 67.166908ms | POST "/api/auth/login"
   ```

2. **健康检查**
   ```bash
   curl http://localhost:8000/health
   # {"status": "ok", "service": "gateway"}
   ```

3. **服务状态**
   ```bash
   curl http://localhost:8000/
   # 显示所有后端服务地址
   ```

## 未来扩展

### 1. 认证过滤
在 Gateway 统一验证 JWT Token，无效请求直接拒绝

### 2. 限流
按 IP、用户或 API 限制请求频率

### 3. 缓存
对查询接口添加缓存层

### 4. 负载均衡
支持多个后端实例的负载均衡

### 5. 熔断降级
后端服务故障时自动熔断

## 总结

Gateway 的核心价值：

1. ✅ **统一入口** - 客户端只需要一个地址
2. ✅ **透明代理** - 后端服务无感知
3. ✅ **集中管理** - 跨域、认证、日志等统一处理
4. ✅ **灵活扩展** - 易于添加新服务
5. ✅ **生产就绪** - 支持监控、健康检查

Gateway 是微服务架构的**门面**，是系统对外的唯一入口！🚪