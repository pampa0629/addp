# ADDP 登录认证原理说明

## 一、认证流程概述

ADDP 平台使用 **JWT (JSON Web Token)** 进行用户认证，流程简单清晰：

```
用户登录 → System 后端验证 → 返回 JWT Token → 前端存储 Token → 后续请求携带 Token → 后端验证 Token
```

### 核心组件

**后端 (System 模块)**：
- `/api/auth/login` - 登录接口，返回 JWT Token
- `/api/auth/refresh` - 刷新过期的 Token
- `AuthMiddleware` - 验证每个请求的 Token

**前端 (各模块)**：
- `authAPI` - 调用登录/用户信息接口
- `authStore` - Pinia 状态管理，存储 token 和 user
- `createAuthGuard` - 路由守卫，保护需要登录的页面
- `createAuthInterceptor` - HTTP 拦截器，自动添加 Token 头

### JWT 认证流程总览

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

---

## 二、登录认证完整流程（以 Meta 模块为例）

### 场景 1：Meta 模块独立登录

用户访问 `http://localhost:5175/meta/scan`

```
┌─────────────────────────────────────────────────────────────────┐
│ 第 1 步：用户访问受保护页面                                       │
└─────────────────────────────────────────────────────────────────┘

浏览器打开 http://localhost:5175/meta/scan
    ↓
Vue Router 触发
    ↓
【路由守卫检查】createAuthGuard()
    ├─ 读取 localStorage.getItem('token')
    ├─ 发现 token 不存在
    └─ 重定向到 /meta/login?redirect=/meta/scan


┌─────────────────────────────────────────────────────────────────┐
│ 第 2 步：用户登录                                                │
└─────────────────────────────────────────────────────────────────┘

用户在 Meta Login.vue 页面输入用户名/密码
    ↓
点击"登录"按钮
    ↓
【前端】authStore.login(username, password)
    ↓
【前端】authAPI.login(username, password)
    └─→ POST http://localhost:8180/api/auth/login
        Body: {
          "username": "admin",
          "password": "123456"
        }
    ↓
【后端 - System】AuthHandler.Login()
    ├─ 从数据库查询用户
    ├─ 验证密码（bcrypt 哈希比对）
    ├─ 生成 JWT Token (HS256 签名)
    │   Token 包含：
    │   {
    │     "user_id": 2,
    │     "username": "admin",
    │     "tenant_id": 1,
    │     "exp": 1765630000,  // 过期时间（默认 30 分钟后）
    │     "iat": 1765628200   // 签发时间
    │   }
    └─ 返回响应：
        {
          "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoyLCJ1c2VybmFtZSI6ImFkbWluIiwidGVuYW50X2lkIjoxLCJleHAiOjE3NjU2MzAwMDAsImlhdCI6MTc2NTYyODIwMH0.xxx",
          "token_type": "Bearer"
        }
    ↓
【前端】authStore.setToken(access_token)
    └─ localStorage.setItem('token', access_token)
    ↓
【前端】authStore.fetchUser()
    └─→ GET http://localhost:8180/api/users/me
        Headers: {
          "Authorization": "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
        }
    ↓
【后端 - System】AuthMiddleware 验证 Token
    ├─ 从 Authorization 头提取 Token
    ├─ 使用 JWT_SECRET 验证签名
    ├─ 验证过期时间（exp）
    ├─ 验证签名算法（必须是 HS256）
    └─ 提取用户信息存入 Gin context:
        c.Set("user_id", 2)
        c.Set("username", "admin")
        c.Set("tenant_id", 1)
    ↓
【后端 - System】UserHandler.GetMe()
    └─ 返回用户信息：
        {
          "id": 2,
          "username": "admin",
          "email": "admin@addp.com",
          "user_type": "tenant_admin",
          "tenant_id": 1
        }
    ↓
【前端】authStore.user = response.data
    ├─ localStorage.setItem('user', JSON.stringify(user))  // Meta 不持久化，但示例展示机制
    └─ 路由守卫放行
    ↓
用户进入 /meta/scan 页面


┌─────────────────────────────────────────────────────────────────┐
│ 第 3 步：后续 API 请求自动携带 Token                             │
└─────────────────────────────────────────────────────────────────┘

用户在 Meta 模块发起任何 API 请求（例如：获取扫描任务列表）
    ↓
【前端】client.get('/meta/scan/tasks')
    ↓
【HTTP 拦截器】createAuthInterceptor()
    ├─ 读取 authStore.token 或 localStorage.getItem('token')
    └─ 添加到请求头：
        config.headers.Authorization = `Bearer ${token}`
    ↓
【后端 - Meta】接收请求
    ↓
【后端 - Meta】AuthMiddleware 验证 Token
    ├─ 验证签名和过期时间
    └─ 提取用户信息到 context
    ↓
【后端 - Meta】ScanTaskHandler.ListTasks()
    ├─ 从 context 获取 tenant_id
    └─ 只返回该租户的扫描任务
    ↓
返回数据给前端


┌─────────────────────────────────────────────────────────────────┐
│ 第 4 步：Token 过期自动刷新                                      │
└─────────────────────────────────────────────────────────────────┘

（30 分钟后，Token 过期）

用户继续操作，发起 API 请求
    ↓
【后端】AuthMiddleware 验证 Token
    └─ 发现 Token 已过期
    └─ 返回 401 Unauthorized
    ↓
【前端】HTTP 响应拦截器 createRefreshInterceptor()
    ├─ 检测到 401 状态码
    ├─ 标记 isRefreshing = true
    └─→ POST http://localhost:8180/api/auth/refresh
        Headers: {
          "Authorization": "Bearer <old_expired_token>"
        }
    ↓
【后端 - System】AuthHandler.Refresh()
    ├─ 使用 ParseTokenAllowExpired() 允许过期的 token
    │   （但仍验证签名和算法，防止伪造）
    ├─ 提取原 token 中的用户信息
    └─ 生成新的 JWT Token
    ↓
【前端】收到新 token
    ├─ authStore.setToken(newToken)
    ├─ 通知所有等待的请求使用新 token
    └─ 重试原请求（自动附加新 token）
    ↓
【后端】验证新 token，处理原请求
    ↓
返回数据，用户无感知刷新成功
```

---

## 三、不同登录场景对比

### 场景 2：Develop 模块独立登录

与 Meta 模块流程**完全相同**，只是：

- 访问地址：`http://localhost:5178/develop/editor`
- 登录页面：`/develop/login`
- 后端 API：依然是 **System 后端** `http://localhost:8180/api/auth/login`
- 业务 API：Develop 后端 `http://localhost:8085/api/develop/*`

**关键点**：
- ✅ **所有模块的认证都通过 System 模块完成**（统一的用户表和 JWT Secret）
- ✅ 各模块只负责业务逻辑，认证中间件验证 token 后获取用户信息
- ✅ Meta 和 Develop 是独立的前端应用，各自维护自己的 authStore

---

### 场景 3：Portal 统一登录（推荐方式）

Portal 通过 iframe 集成所有模块前端，用户只需登录一次，Token 自动传递给各子模块：

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

    classDef portal fill:#fff9c4,stroke:#f57f17
    classDef component fill:#e1f5ff,stroke:#01579b
    classDef frontend fill:#e8f5e9,stroke:#1b5e20

    class Portal portal
    class Sidebar,IframeArea component
    class SystemFE,ManagerFE,MetaFE,OtherFE frontend
```

用户访问 `http://localhost:5170` (Portal 统一入口)

```
┌─────────────────────────────────────────────────────────────────┐
│ 第 1 步：Portal 统一登录                                         │
└─────────────────────────────────────────────────────────────────┘

用户打开 http://localhost:5170
    ↓
【Portal】路由守卫检查
    ├─ 发现未登录
    └─ 重定向到 /login
    ↓
用户在 Portal Login.vue 输入用户名/密码
    ↓
【Portal】authStore.login(username, password)
    └─→ POST http://localhost:8180/api/auth/login
    ↓
【System 后端】返回 JWT Token
    ↓
【Portal】存储 token
    ├─ localStorage.setItem('token', token)
    └─ authStore.fetchUser()
    ↓
【Portal】登录成功，进入首页


┌─────────────────────────────────────────────────────────────────┐
│ 第 2 步：Portal 通过 iframe 加载子模块（传递 Token）             │
└─────────────────────────────────────────────────────────────────┘

用户在 Portal 点击"元数据管理"卡片
    ↓
【Portal】Portal.vue 创建 iframe:
    <iframe src="http://localhost:5175/meta?token=eyJhbGciOiJI..."></iframe>
                                           ↑
                          关键：将 token 作为 query 参数传递给子模块
    ↓
【Meta 前端】Vue Router 初始化
    ↓
【Meta 路由守卫】createAuthGuard() 检查
    ├─ 发现 to.query.token 存在
    ├─ authStore.setToken(query.token)  // 保存 Portal 传来的 token
    ├─ authStore.fetchUser()            // 使用 token 获取用户信息
    └─ 从 URL 移除 token 参数（安全考虑）
        最终 URL: http://localhost:5175/meta/scan
                                              ↑
                                   没有 ?token= 暴露在 URL 中
    ↓
【Meta】加载用户界面（已认证状态）

此时 Meta 模块和 Portal 使用同一个 token，用户信息共享


┌─────────────────────────────────────────────────────────────────┐
│ 第 3 步：用户在 Portal 中切换到 Develop 模块                     │
└─────────────────────────────────────────────────────────────────┘

用户点击"GIS 开发"卡片
    ↓
【Portal】创建新 iframe:
    <iframe src="http://localhost:5178/develop?token=eyJhbGciOiJI..."></iframe>
    ↓
【Develop 前端】路由守卫检查
    ├─ 发现 to.query.token 存在
    ├─ authStore.setToken(query.token)  // 同样的 token
    └─ 移除 URL token 参数
    ↓
【Develop】加载界面（已认证）

✅ 用户在 Portal 只登录一次，所有子模块自动获得认证
```

---

## 四、三种登录方式对比

| 特性 | Meta 独立登录 | Develop 独立登录 | Portal 统一登录 |
|------|-------------|----------------|----------------|
| **登录入口** | http://localhost:5175/meta/login | http://localhost:5178/develop/login | http://localhost:5170/login |
| **认证接口** | System `/api/auth/login` | System `/api/auth/login` | System `/api/auth/login` |
| **Token 存储** | Meta localStorage | Develop localStorage | Portal localStorage |
| **Token 传递** | - | - | ✅ 通过 URL query 传递到子模块 |
| **用户体验** | 需要单独登录 Meta | 需要单独登录 Develop | **一次登录，访问所有模块** |
| **适用场景** | Meta 独立部署 | Develop 独立部署 | **生产环境推荐** |
| **Token 持久化** | ✅ localStorage | ✅ localStorage | ✅ localStorage |
| **User 持久化** | ❌ 每次刷新重新加载 | ❌ 每次刷新重新加载 | ✅ 刷新后无需重新登录 |

---

## 五、关键技术点

### 1. JWT Token 结构

> Gateway 路由规则和 Portal 架构总览见：[ADDP 模块架构图](../addp模块架构图.md)

```json
{
  "header": {
    "alg": "HS256",           // 签名算法
    "typ": "JWT"              // Token 类型
  },
  "payload": {
    "user_id": 2,             // 用户 ID
    "username": "admin",      // 用户名
    "tenant_id": 1,           // 租户 ID（多租户隔离）
    "exp": 1765630000,        // 过期时间（Unix 时间戳）
    "iat": 1765628200         // 签发时间
  },
  "signature": "xxx"          // HS256(base64(header) + "." + base64(payload), JWT_SECRET)
}
```

### 2. 认证中间件工作原理

```go
// 后端每个受保护的 API 都会经过这个中间件
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. 提取 Token
        authHeader := c.GetHeader("Authorization")  // "Bearer eyJhbGci..."
        parts := strings.SplitN(authHeader, " ", 2)
        tokenString := parts[1]

        // 2. 验证 Token
        claims, err := utils.ParseToken(tokenString, cfg.JWTSecret)
        // - 验证签名算法（必须是 HS256，防止攻击）
        // - 验证签名正确性（使用 JWT_SECRET）
        // - 验证过期时间（exp）

        // 3. 提取用户信息到请求上下文
        c.Set("user_id", claims.UserID)
        c.Set("username", claims.Username)
        c.Set("tenant_id", claims.TenantID)

        // 4. 继续处理请求
        c.Next()
    }
}
```

### 3. 前端自动刷新机制

```javascript
// HTTP 响应拦截器
client.interceptors.response.use(
    response => response,
    async error => {
        // 检测 401 状态码
        if (error.response?.status === 401) {
            // 已重试过则放弃
            if (error.config._retry) {
                authStore.logout()
                return Promise.reject(error)
            }

            // 标记重试
            error.config._retry = true

            // 调用刷新接口
            const response = await fetch('/api/auth/refresh', {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${oldToken}` }
            })

            const { access_token } = await response.json()

            // 更新 token
            authStore.setToken(access_token)

            // 重试原请求（自动附加新 token）
            return client(error.config)
        }
        return Promise.reject(error)
    }
)
```

### 4. Portal iframe Token 传递

```vue
<!-- Portal.vue -->
<template>
  <iframe
    :src="`${moduleUrl}?token=${authStore.token}`"
    @load="onIframeLoad"
  ></iframe>
</template>

<script setup>
// Meta 模块路由守卫
router.beforeEach((to, from, next) => {
    // 检测 Portal 传来的 token
    if (to.query.token) {
        // 保存 token
        authStore.setToken(to.query.token)

        // 加载用户信息
        await authStore.fetchUser()

        // 移除 URL 中的 token（防止暴露）
        const { token, ...restQuery } = to.query
        next({
            path: to.path,
            query: restQuery,
            replace: true  // 替换历史记录，用户无法后退看到 token
        })
        return
    }

    next()
})
</script>
```

### 5. iframe 中的 API 请求路径

**关键知识点**：iframe 内的 API 请求使用相对路径时，相对于 **iframe 的 origin**，而不是父窗口。

```
Portal (localhost:5170)
  └─ iframe src="http://localhost:5175/meta"
       └─ Meta 发起 API 请求: /api/meta/scan
          └─ 浏览器解析为 http://localhost:5175/api/meta/scan
             └─ 命中 Meta 的 Vite 代理 /api → Gateway (8000)
             ✅ 正确路由，无需特殊处理
```

**所有模块的 `vite.config.js` 配置相同的代理**：

```javascript
proxy: {
  '/api': {
    target: 'http://localhost:8000',  // Gateway
    changeOrigin: true
  }
}
```

**结论**：在 Portal iframe 中运行时，各模块无需修改 API 请求的 baseURL。使用统一的相对路径 `/api`，由各模块的 Vite 代理转发到 Gateway，和独立运行时行为完全一致。

- ❌ 错误做法：检测 `window.self !== window.top` 并切换到绝对 URL
- ✅ 正确做法：始终使用 `createAPIClient()` 的默认相对路径配置

---

## 六、安全特性

### 1. 密码安全

```go
// 注册时加密密码
passwordHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

// 登录时验证密码
err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
```

- ✅ 使用 **bcrypt** 不可逆哈希（cost factor 10）
- ✅ 自动加盐，防止彩虹表攻击
- ✅ 计算耗时，防止暴力破解

### 2. Token 签名安全

```go
// 验证签名算法（防止 "none" 算法攻击）
if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
    return nil, errors.New("invalid signing method")
}

// 验证具体算法必须是 HS256
if token.Method.Alg() != "HS256" {
    return nil, errors.New("unexpected algorithm")
}
```

- ✅ 强制验证签名算法，防止 CVE-2015-9235 攻击
- ✅ 拒绝 `"none"` 算法
- ✅ 验证 Token 过期时间

### 3. URL Token 保护

```javascript
// Portal iframe 传递 token 后立即移除
const { token, ...restQuery } = to.query
next({ path: to.path, query: restQuery, replace: true })
```

- ✅ Token 传递后立即从 URL 移除
- ✅ 使用 `replace: true` 防止用户后退看到 token
- ✅ Token 存储在 localStorage，不暴露在 URL

### 4. 多租户隔离

```go
// API 自动过滤租户数据
tenantID := c.GetInt("tenant_id")
return db.Where("tenant_id = ?", tenantID).Find(&tasks).Error
```

- ✅ Token 包含 tenant_id
- ✅ 所有数据查询自动过滤租户
- ✅ 租户管理员只能访问自己租户的数据

---

## 七、常见问题

### Q1: 为什么所有模块都调用 System 的登录接口？

**A**: System 是认证中心，负责：
- 管理用户表（users）
- 生成和验证 JWT Token
- 使用统一的 JWT_SECRET

所有模块使用相同的 Token，用户在任何模块登录后都可以访问其他模块。

### Q2: Meta 和 Develop 独立登录，token 能互通吗？

**A**: 不能。虽然都调用 System 登录接口，但：
- Meta 的 token 存储在 Meta 的 localStorage
- Develop 的 token 存储在 Develop 的 localStorage
- 两个模块的 localStorage 相互隔离

如果需要互通，使用 **Portal 统一登录**。

### Q3: Portal iframe 模式下，子模块刷新页面会丢失 token 吗？

**A**: 会。因为：
- Token 通过 URL query 传递是一次性的
- 刷新页面后 URL 中没有 token 参数
- 解决方案：
  - Portal 使用 `persistUser: true`，刷新 Portal 后重新加载 iframe 并传递 token
  - 或者子模块也持久化 token（但需要考虑安全性）

### Q4: Token 过期后用户需要重新登录吗？

**A**: 不需要。前端自动调用 `/api/auth/refresh` 刷新 token，用户无感知。

### Q5: 如何测试 Token 刷新机制？

**A**:
```bash
# 1. 修改 .env 中的 TOKEN_EXPIRE_MINUTES 为 1 分钟
TOKEN_EXPIRE_MINUTES=1

# 2. 重启 System 后端
cd system/backend && go run cmd/server/main.go

# 3. 登录后等待 1 分钟，继续操作
# 4. 打开浏览器 Network 面板，观察是否自动调用 /api/auth/refresh
```

---

## 八、总结

ADDP 认证系统的核心设计思想：

1. **统一认证中心**：所有模块通过 System 登录，使用统一的 JWT Secret
2. **Token 自动刷新**：前端拦截器自动处理 401，无需用户手动重新登录
3. **iframe Token 传递**：Portal 统一登录后，通过 URL query 传递 token 到子模块
4. **多租户隔离**：Token 包含 tenant_id，后端自动过滤数据
5. **安全优先**：bcrypt 密码加密、签名算法验证、URL token 移除

**推荐使用方式**：
- ✅ 生产环境：**Portal 统一登录** (一次登录，访问所有模块)
- ✅ 开发调试：各模块独立登录 (方便单独测试)

---

## 相关文档

- [ADDP 模块架构图（含 Gateway 路由 / Portal 架构）](../addp模块架构图.md)
- [ADDP 账号与权限体系图](addp账号与权限体系图.md)
- [ADDP 核心概念关系图](../addp核心概念关系图.md)
- [Gateway 架构说明](../../gateway/doc/gateway架构说明.md)
