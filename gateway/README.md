# Gateway 网关模块

> 全域数据平台的统一 API 入口和路由服务

## 🎯 核心功能

- **统一入口**: 为所有平台服务提供单一 API 入口点
- **智能路由**: 根据请求路径自动路由到对应的内部服务
- **认证传递**: 统一处理 JWT 认证并透传给后端服务
- **CORS 支持**: 处理跨域请求，支持前端访问
- **健康检查**: 监控后端服务状态

## 🚀 快速开始

### 前置要求

- Go 1.21+
- 至少一个后端服务运行中（System、Manager、Meta 或 Transfer）

### 运行网关

```bash
cd gateway
go run cmd/gateway/main.go
```

访问: http://localhost:8000

### Docker 部署

```bash
docker build -t addp-gateway .
docker run -d -p 8000:8000 addp-gateway
```

## 🔀 路由规则

Gateway 根据 URL 路径前缀自动路由请求：

| 请求路径 | 目标服务 | 服务地址 | 用途 |
|---------|---------|---------|-----|
| `/api/auth/*` | System | http://localhost:8080 | 用户认证 |
| `/api/users/*` | System | http://localhost:8080 | 用户管理 |
| `/api/tenants/*` | System | http://localhost:8080 | 租户管理 |
| `/api/resources/*` | System | http://localhost:8080 | 资源管理 |
| `/api/logs/*` | System | http://localhost:8080 | 日志查询 |
| `/api/datasources/*` | Manager | http://localhost:8081 | 数据源管理 |
| `/api/directories/*` | Manager | http://localhost:8081 | 目录管理 |
| `/api/preview/*` | Manager | http://localhost:8081 | 数据预览 |
| `/api/metadata/*` | Meta | http://localhost:8082 | 元数据查询 |
| `/api/lineage/*` | Meta | http://localhost:8082 | 数据血缘 |
| `/api/transfer/*` | Transfer | http://localhost:8083 | 数据传输 |

### 健康检查

- `GET /health` - 网关健康状态

## ⚙️ 环境配置

```bash
# 网关端口
GATEWAY_PORT=8000

# 后端服务地址
SYSTEM_SERVICE_URL=http://localhost:8080
MANAGER_SERVICE_URL=http://localhost:8081
META_SERVICE_URL=http://localhost:8082
TRANSFER_SERVICE_URL=http://localhost:8083

# CORS 配置
CORS_ALLOWED_ORIGINS=http://localhost:5170,http://localhost:5173,http://localhost:5174
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE,OPTIONS
CORS_ALLOWED_HEADERS=Content-Type,Authorization

# 超时配置
PROXY_TIMEOUT=30s
```

## 🔐 认证流程

Gateway 透明传递认证信息：

1. 前端发送请求到 Gateway，携带 `Authorization: Bearer <token>` 头部
2. Gateway 接收请求并提取所有头部信息
3. Gateway 根据路径规则路由到对应服务
4. Gateway 将原始请求（包括认证头）完整转发给后端服务
5. 后端服务验证 JWT Token 并处理请求
6. Gateway 将响应返回给前端

**注意**: Gateway 本身不验证 Token，由各个后端服务负责验证。

## 🌐 访问方式

### 开发环境

**通过 Gateway 访问** (推荐):
```bash
# 所有服务通过统一入口访问
curl http://localhost:8000/api/auth/login
curl http://localhost:8000/api/datasources
curl http://localhost:8000/api/metadata/tables
```

**直接访问服务**:
```bash
# 也可以直接访问各个服务
curl http://localhost:8080/api/auth/login    # System
curl http://localhost:8081/api/datasources   # Manager
curl http://localhost:8082/api/metadata      # Meta
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
- 服务地址配置是否正确（`SYSTEM_SERVICE_URL` 等）
- 网络连接是否正常

### 3. CORS 错误？

确保：
- 前端域名已添加到 `CORS_ALLOWED_ORIGINS`
- 浏览器控制台显示的域名与配置匹配
- Gateway 日志中 CORS 中间件已加载

### 4. 认证失败？

检查：
- 请求是否携带 `Authorization` 头部
- Token 是否正确且未过期
- 后端服务（System）是否正常运行

### 5. 如何添加新的路由规则？

请参考技术文档 [ARCHITECTURE.md](./ARCHITECTURE.md) 中的"添加新路由"章节。

## 📊 监控和日志

Gateway 会记录：
- 所有请求的路由信息
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

- **快速开始**: [QUICK_START.md](./QUICK_START.md) - 5分钟上手指南
- **技术架构**: [ARCHITECTURE.md](./ARCHITECTURE.md) - 详细的技术实现和开发指南
- **项目总览**: [../CLAUDE.md](../CLAUDE.md) - 完整平台架构

## 📄 License

Copyright © 2025 ADDP Team
