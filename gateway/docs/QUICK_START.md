# Gateway 快速理解指南

## 🎯 Gateway 做了什么？

Gateway 就像一个**智能前台**：

```
你去公司办事 → 前台接待（Gateway） → 转到对应部门
客户端请求   → Gateway:8000      → 路由到后端服务
```

## 📊 对比测试

### 测试 1: 直接访问 System 服务

```bash
curl http://localhost:8180/
```

返回：
```json
{
  "message": "全域数据平台",
  "name_en": "All Domain Data Platform"
}
```

### 测试 2: 通过 Gateway 访问

```bash
curl http://localhost:8000/
```

返回：
```json
{
  "message": "全域数据平台 API Gateway",
  "services": {
    "system": "http://localhost:8180",
    "manager": "http://localhost:8081",
    "meta": "http://localhost:8082",
    "transfer": "http://localhost:8083"
  },
  "version": "1.0.0"
}
```

### 测试 3: Gateway 代理登录请求

```bash
# 通过 Gateway 登录
curl -X POST http://localhost:8000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
```

**发生了什么？**

1. 请求到达 Gateway:8000
2. Gateway 看到路径是 `/api/auth/login`
3. 根据路由规则：`/api/auth/*` → System 服务
4. Gateway 转发请求到 `http://system-backend:8180/api/auth/login`
5. System 处理登录，返回 Token
6. Gateway 把响应原样返回给客户端

返回：
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}
```

## 🔍 查看日志证明

### Gateway 日志
```
[GIN] POST "/api/auth/login" | 200 | 68.549263ms
```
Gateway 接收到请求，耗时 68ms（包括转发时间）

### System 日志
```
[GIN] POST "/api/auth/login" | 200 | 67.471691ms
```
System 处理请求，耗时 67ms

**结论**：Gateway 增加了约 1ms 的代理延迟

## 🎨 Gateway 的核心价值

### 1. 统一入口

**没有 Gateway**：
```
前端需要配置多个地址：
- System:  http://localhost:8180
- Manager: http://localhost:8081
- Meta:    http://localhost:8082
- Transfer:http://localhost:8083
```

**有了 Gateway**：
```
前端只需要一个地址：
- Gateway: http://localhost:8000

Gateway 自动路由到正确的服务
```

### 2. 透明代理

客户端完全不知道后端有多少服务，Gateway 自动处理：

```
客户端视角：
POST /api/auth/login      → 登录
GET  /api/users          → 获取用户
POST /api/datasources    → 创建数据源
GET  /api/metadata       → 查询元数据

实际路由：
POST /api/auth/login      → System:8180
GET  /api/users          → System:8180
POST /api/datasources    → Manager:8081
GET  /api/metadata       → Meta:8082
```

### 3. 灵活扩展

添加新服务只需要在 Gateway 配置路由：

```go
// 添加新服务很简单
newServiceProxy := proxy.NewServiceProxy("http://localhost:8084")
api.Any("/api/newservice/*path", newServiceProxy.Handle)
```

客户端代码**完全不需要修改**！

## 🛠️ 技术实现

### 核心代码（简化版）

```go
// 1. 配置服务地址
type Config struct {
    SystemURL  string  // http://system-backend:8180
    ManagerURL string  // http://localhost:8081
}

// 2. 创建代理
systemProxy := NewProxy(config.SystemURL)

// 3. 配置路由
router.Any("/api/auth/*path", func(c *gin.Context) {
    // 获取原始请求: POST /api/auth/login
    targetURL := config.SystemURL + c.Request.URL.Path
    // 构建目标: http://system-backend:8180/api/auth/login

    // 转发请求（包含所有 Header、Body）
    resp := http.Post(targetURL, body, headers)

    // 返回响应
    c.JSON(resp.StatusCode, resp.Body)
})
```

### 请求转发过程

```
1. 客户端 → Gateway
   POST /api/auth/login
   Header: Content-Type: application/json
   Body: {"username":"admin","password":"admin123"}

2. Gateway 解析
   路径: /api/auth/login
   匹配: /api/auth/* → systemProxy
   目标: http://system-backend:8180/api/auth/login

3. Gateway → System
   POST http://system-backend:8180/api/auth/login
   Header: Content-Type: application/json (复制)
   Body: {"username":"admin","password":"admin123"} (复制)

4. System → Gateway
   Status: 200
   Header: Content-Type: application/json
   Body: {"access_token":"...","token_type":"Bearer"}

5. Gateway → 客户端
   Status: 200 (复制)
   Header: Content-Type: application/json (复制)
   Body: {"access_token":"...","token_type":"Bearer"} (复制)
```

## 📁 文件结构

```
gateway/
├── cmd/gateway/main.go          # 入口：启动 Gateway
├── internal/
│   ├── config/config.go         # 配置：读取服务地址
│   ├── router/router.go         # 路由：定义 URL → 服务映射
│   ├── proxy/proxy.go           # 代理：转发 HTTP 请求
│   └── middleware/cors.go       # 中间件：处理跨域
└── go.mod                        # 依赖管理
```

每个文件只有 50-100 行代码，非常简洁！

## 🚀 实际使用场景

### 场景 1: 开发阶段

现在只有 System 服务：
```
Gateway:8000 → System:8180 ✅
             → Manager:8081 ❌ (服务不存在，返回 502)
             → Meta:8082 ❌
             → Transfer:8083 ❌
```

### 场景 2: Manager 服务开发完成

启动 Manager 后：
```
Gateway:8000 → System:8180 ✅
             → Manager:8081 ✅ (新服务自动可用)
             → Meta:8082 ❌
             → Transfer:8083 ❌
```

**Gateway 代码不需要修改**，只要 Manager 监听 8081 端口即可！

### 场景 3: 生产环境

所有服务部署后：
```
Gateway:8000 → System:8180 ✅
             → Manager:8081 ✅
             → Meta:8082 ✅
             → Transfer:8083 ✅
```

前端只需要知道 Gateway 地址：`https://api.addp.com`

## 🎓 总结

**Gateway 的作用**：
1. ✅ 统一入口 - 一个地址访问所有服务
2. ✅ 自动路由 - 根据 URL 转发到正确的服务
3. ✅ 透明代理 - 完整转发请求和响应
4. ✅ 解耦前后端 - 后端服务地址可以随意变化

**Gateway 不做的事**：
1. ❌ 不修改请求内容
2. ❌ 不处理业务逻辑
3. ❌ 不存储数据

Gateway 就是一个**智能路由器**，仅此而已！

## 📖 深入阅读

- 完整架构文档：[ARCHITECTURE.md](./ARCHITECTURE.md)
- Gateway README：[README.md](./README.md)
- 根目录文档：[../README.md](../README.md)