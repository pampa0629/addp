# ADDP 登录认证技术探讨

## 用户问题

1. Console 登录后，各个模块如何可用？
2. 所有认证是否都在 System 模块进行？
3. 如何实现跨模块自动登录（Console 或任一模块登录后，其他模块自动登录）？

## 探索结果

### 已完成的探索
- ✅ 阅读 Console 登录实现和 token 传递机制
- ✅ 阅读各模块的认证流程
- ✅ 阅读官方文档：`docs/concepts/addp登录认证的原理说明.md` 和 `docs/spec/addp登录认证的统一要求.md`
- ✅ 分析 common-frontend 的认证工具库

## 技术分析

### 问题 1: Console 登录后如何让各模块可用？

**关键机制：URL Query 参数传递 Token**

当用户在 Console 登录后，点击某个模块卡片时：

1. **Console 构建 iframe URL 时附加 token**（`console/frontend/src/views/Portal.vue:472-476`）：
   ```javascript
   const token = authStore.token
   if (token) {
     const separator = url.includes('?') ? '&' : '?'
     url = `${url}${separator}token=${encodeURIComponent(token)}`
   }
   // 结果: http://localhost:5174/data-explorer?token=eyJhbGc...
   ```

2. **子模块路由守卫自动处理**（`common-frontend/basic/src/composables/useAuth.js:62-82`）：
   ```javascript
   router.beforeEach(createAuthGuard(...))

   // createAuthGuard 内部逻辑
   const queryToken = to.query.token
   if (queryToken) {
     // 保存到子模块的 localStorage
     authStore.setToken(queryToken)

     // 验证 token 有效性
     await authStore.fetchUser()

     // 从 URL 移除 token（安全考虑）
     const { token: _removed, ...restQuery } = to.query
     next({ path: to.path, query: restQuery, replace: true })
   }
   ```

3. **localStorage 隔离但 token 共享**：
   - Console (localhost:5170) 和各模块 (localhost:5173-5180) 的 localStorage 是隔离的
   - 但每次 iframe 加载时都会传递最新的 token
   - 各模块保存后可以独立使用

**优势**：
- 用户只需登录一次（在 Console）
- 各模块通过 URL 参数获得认证
- 安全：token 从 URL 立即移除，存入 localStorage

### 问题 2: 所有认证是否都在 System 模块进行？

**是的，完全正确！**

**认证中心化设计**：

1. **统一登录接口**（`system/backend/internal/api/auth_handler.go`）：
   - 所有模块调用 `POST http://localhost:8180/api/auth/login`
   - System 管理唯一的用户表（`system.users`）
   - System 使用统一的 `JWT_SECRET` 生成 token

2. **各模块的认证 API 配置**（文档要求）：
   ```javascript
   // 所有模块都创建独立的 systemClient 指向 System
   const systemClient = axios.create({
     baseURL: 'http://localhost:8180/api'  // 指向 System
   })

   export const authAPI = createAuthAPI(systemClient)
   ```

3. **Token 验证分布式执行**：
   - System 生成 token
   - 各模块后端独立验证 token（使用相同的 `JWT_SECRET`）
   - 验证逻辑：`system/backend/internal/middleware/auth.go`（所有模块复用此代码）

**架构优势**：
- ✅ 单一用户数据源
- ✅ 统一的密码策略和安全规则
- ✅ Token 在所有模块间通用
- ✅ 用户管理集中化

### 问题 3: 如何实现跨模块自动登录？

**当前限制**：

当前架构下，各模块独立访问时需要重新登录，原因是：
- 开发环境使用不同端口（5173-5180）
- 不同端口的 localStorage 无法共享
- 即使 token 相同，各模块无法感知其他模块的登录状态

**可能的技术方案**（不同复杂度和场景）：

#### 方案 1: Cookie 跨域共享（推荐 - 生产环境）

**原理**：使用 Cookie 替代 localStorage 存储 token，设置 `domain` 属性实现子域共享

**技术实现**：
- 前端：使用 `js-cookie` 库存储 token
  ```javascript
  // 设置 cookie 到父域
  Cookies.set('addp_token', token, {
    domain: '.addp.com',  // 所有子域可访问
    path: '/',
    secure: true,  // HTTPS only
    sameSite: 'Lax'
  })
  ```
- 后端：支持从 Cookie 中读取 token（除了 Authorization header）

**适用场景**：
- ✅ 生产环境（统一域名如 `system.addp.com`, `manager.addp.com`）
- ❌ 开发环境（localhost 无法设置子域）

**优势**：
- 原生浏览器支持
- 安全性好（HttpOnly, Secure 标志）
- 无需 iframe 或 postMessage

**劣势**：
- 需要修改后端认证逻辑
- CORS 配置更复杂
- 开发环境无法使用

---

#### 方案 2: SharedWorker + BroadcastChannel（现代浏览器）

**原理**：使用 SharedWorker 作为跨标签页的认证状态共享中心

**技术实现**：
```javascript
// shared-auth-worker.js
const authState = { token: null, user: null }

onconnect = (e) => {
  const port = e.ports[0]

  port.onmessage = (msg) => {
    if (msg.data.type === 'SET_TOKEN') {
      authState.token = msg.data.token
      // 广播到所有连接的标签页
      broadcast({ type: 'TOKEN_UPDATED', token: authState.token })
    }
  }
}

// 各模块监听
const worker = new SharedWorker('/shared-auth-worker.js')
worker.port.onmessage = (msg) => {
  if (msg.data.type === 'TOKEN_UPDATED') {
    authStore.setToken(msg.data.token)
  }
}
```

**适用场景**：
- ✅ 开发环境（localhost 不同端口）
- ✅ 用户在多个标签页同时访问不同模块

**优势**：
- 实时同步（一个标签页登录，其他立即生效）
- 不需要后端改动
- 支持跨端口

**劣势**：
- 浏览器兼容性问题（Safari 不支持 SharedWorker）
- 实现复杂度较高
- 需要额外的 worker 文件

---

#### 方案 3: LocalStorage + Window.postMessage 轮询检测

**原理**：各模块定期检测其他模块的 localStorage 变化

**技术实现**：
```javascript
// 模块 A 登录后
localStorage.setItem('addp_global_token', token)
window.dispatchEvent(new StorageEvent('storage', {
  key: 'addp_global_token',
  newValue: token
}))

// 模块 B 监听
window.addEventListener('storage', (e) => {
  if (e.key === 'addp_global_token' && e.newValue) {
    authStore.setToken(e.newValue)
    authStore.fetchUser()
  }
})
```

**注意**：StorageEvent 只在**不同标签页**触发，同一标签页不触发

**适用场景**：
- ❌ 不适用（localStorage 在不同端口间隔离）

**结论**：此方案在当前架构下不可行

---

#### 方案 4: 统一网关路由（架构调整）

**原理**：通过 Gateway 统一所有模块的访问路径，使用同一域名和端口

**当前架构**：
```
Console:  http://localhost:5170
System:  http://localhost:5173
Manager: http://localhost:5174
```

**调整后架构**：
```
Gateway:  http://localhost:8000
Console:   http://localhost:8000/console/
System:   http://localhost:8000/system/
Manager:  http://localhost:8000/manager/
```

**技术实现**：
- Gateway 配置路径前缀路由
- 所有模块通过 Gateway 访问
- 同一端口下，localStorage 自然共享

**适用场景**：
- ✅ 开发环境和生产环境都适用
- ✅ 已有 Gateway 基础设施

**优势**：
- 最简单的方案
- 符合微服务网关设计模式
- 前端无需特殊处理

**劣势**：
- 需要调整 Gateway 配置
- 开发环境需要运行 Gateway
- 调试时无法直接访问模块端口

---

#### 方案 5: Console 永久嵌入模式（最小改动）

**原理**：强制用户只能通过 Console 访问，禁止直接访问模块

**技术实现**：
- 各模块检测是否在 iframe 中：`window.self !== window.top`
- 如果不在 iframe 且未登录，重定向到 Console：`window.location.href = 'http://localhost:5170'`

**适用场景**：
- ✅ 不需要独立模块访问
- ✅ 希望统一用户入口

**优势**：
- 无需后端改动
- 实现简单（几行代码）
- 保持当前 Console token 传递机制

**劣势**：
- 无法独立访问模块（开发调试不便）
- 用户体验受限

---

## 技术方案对比

| 方案 | 适用场景 | 实现难度 | 后端改动 | 开发环境 | 生产环境 | 推荐度 |
|------|---------|---------|---------|---------|---------|--------|
| Cookie 跨域 | 生产环境 | 中 | 需要 | ❌ | ✅ | ⭐⭐⭐⭐ |
| SharedWorker | 跨标签同步 | 高 | 不需要 | ✅ | ✅ | ⭐⭐⭐ |
| Storage 轮询 | - | 低 | 不需要 | ❌ | ❌ | ❌ 不可行 |
| 统一网关路由 | 全场景 | 中 | 不需要 | ✅ | ✅ | ⭐⭐⭐⭐⭐ |
| Console 强制嵌入 | 单一入口 | 低 | 不需要 | ⭐ | ✅ | ⭐⭐ |

## 需要与用户确认

1. **生产环境部署方式**：
   - 是否使用统一域名（如 `system.addp.com`, `manager.addp.com`）？
   - 还是通过 Gateway 统一路径（如 `addp.com/system`, `addp.com/manager`）？

2. **开发环境需求**：
   - 是否需要支持直接访问模块端口（不通过 Console）？
   - 是否可以接受强制通过 Console 访问？

3. **多标签页场景**：
   - 是否需要支持用户在多个标签页中独立打开不同模块？
   - 一个标签页登录，其他标签页是否需要自动同步？

## 建议

基于当前架构和文档，我推荐：

**短期（开发阶段）**：
- 保持当前 Console iframe 传递 token 的方式
- 开发调试时直接访问模块需要重新登录（可接受的开发成本）

**长期（生产部署）**：
- **方案 4（统一网关路由）** - 最符合微服务架构
- 或 **方案 1（Cookie 跨域）** - 如果采用子域名部署方式

需要用户明确：
- 是否确实需要实现跨模块自动登录？
- 偏好哪种技术方案？
- 生产环境的部署架构是什么？
