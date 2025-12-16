
**两种访问模式**:

1. **统一 Portal 模式** (推荐给用户):

   - 单一入口: http://localhost:5170 (dev) 或 http://localhost:8000 (prod)
   - 集成导航,包含所有模块
   - 模块前端在 portal 的 iframe 中加载
   - 一次登录访问所有模块
2. **独立模块模式** (用于独立部署):

   - 直接访问每个模块前端
   - System: http://localhost:5173, Manager: http://localhost:5174
   - 每个模块有自己的登录
   - 适合独立部署单个模块

**前端关键原则**:

- Portal 提供统一的用户体验和一致的导航
- 模块前端保持独立,可以独立部署
- 所有前端共享 JWT 认证模式 (token 存储在 localStorage)
- Portal 和模块可以独立认证
- 在生产环境,所有请求通过 Gateway (8000) 路由

### 认证流程

1. 用户提交凭据到 `POST /api/auth/login`
2. 后端使用 bcrypt 验证,返回 JWT (HS256,使用 `JWT_SECRET` 签名)
3. 前端将 token 存储在 localStorage (`auth.js` Pinia store)
4. Axios 拦截器 (`api/client.js`) 在所有请求中添加 `Authorization: Bearer <token>`
5. 后端 `AuthMiddleware` 验证 JWT 并将用户上下文注入 Gin context
6. 受保护的路由通过 `c.Get("user_id")` 访问用户

### 前端认证标准规范

**重要**: 所有模块前端必须遵循以下标准化认证模式,确保一致性并避免常见错误。

#### 1. 认证 API (`api/auth.js`)

**所有模块必须使用独立的 System 客户端进行认证**,而不是模块自己的 API 客户端:

```javascript
import axios from 'axios'
import { createAuthAPI } from '@common-ui'

// ✅ 正确: 创建专用的 System 客户端用于认证
const systemClient = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8080/api' : '/api',
  timeout: 10000
})

// 基础模块 (Meta, Transfer, Orchestrator, Develop)
export const authAPI = createAuthAPI(systemClient)

// 带注册功能的模块 (Manager, System, Portal)
export const authAPI = createAuthAPI(systemClient, {
  includeRegister: true
})

// System 和 Portal (向后兼容)
export const authAPI = {
  ...createAuthAPI(systemClient, {
    includeRegister: true
  }),
  getMe: () => systemClient.get('/users/me')
}
```

**为什么需要独立客户端?**
- 认证请求必须发送到 System 后端 (端口 8080)
- 模块自己的 `client` 指向自己的后端 (例如 Meta → 8082)
- 混用会导致登录时出现 404 错误

**常见错误避免:**
- ❌ `import client from './client'` - 错误! 这指向模块后端
- ❌ 使用模块的 client 进行认证 - 登录会 404 失败
- ✅ 始终创建独立的 `systemClient` 用于认证

#### 2. API 客户端 (`api/client.js`)

**所有模块必须使用 `createAPIClient()` 工厂函数** 以获得一致的拦截器和错误处理:

```javascript
import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

// 标准配置 (Meta, Transfer, Orchestrator)
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Meta'
})

// 自定义超时 (Develop - SQL 查询需要 5 分钟)
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Develop',
  timeout: 300000
})

// 短超时 (Manager, System, Portal)
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Manager',
  timeout: 10000
})

export default client
```

**`createAPIClient()` 提供的功能:**
- 通过请求拦截器自动注入 JWT token
- 401 错误时自动刷新 token
- 自动提取 `response.data`
- 所有模块的错误处理一致
- 开发/生产环境自动处理

**常见错误避免:**
- ❌ 手动创建 axios 实例并自定义拦截器
- ❌ 在各模块中重复拦截器逻辑
- ❌ 忘记提取 `response.data`
- ✅ 始终使用 `createAPIClient()` 工厂函数

#### 3. 认证 Store (`store/auth.js` 或 `stores/auth.js`)

**所有模块必须使用 `createAuthStore()` 工厂函数** 以避免 getter 覆盖 bug:

```javascript
import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

// 标准配置 (所有模块)
export const useAuthStore = defineStore('meta-auth',
  createAuthStore('meta-auth', authAPI, {
    persistUser: true  // 所有模块必须设为 true
  })
)

// 带自定义 getters (Develop 模块示例)
export const useAuthStore = defineStore('develop-auth',
  createAuthStore('develop-auth', authAPI, {
    persistUser: true,
    extraGetters: {
      username: (state) => state.user?.username || ''
    }
  })
)
```

**关键规则:**
- ✅ **必须使用 `persistUser: true`** - 在 localStorage 中缓存用户信息
- ✅ **永远不要手动合并 getters** - 使用 `extraGetters` 选项
- ❌ **永远不要覆盖基础 getters** - 会导致 `isAuthenticated: undefined` bug

**为什么 `persistUser: true` 是必需的:**
- 没有它,页面刷新后用户信息会丢失
- 每次刷新都会额外调用 `/users/me` 请求
- 用户体验差 (加载状态、闪烁)
- 所有模块必须保持一致的行为

**getter 覆盖 bug (永远不要这样做):**
```javascript
// ❌ 错误 - 这会覆盖所有基础 getters 包括 isAuthenticated
export const useAuthStore = defineStore('develop-auth', {
  ...createAuthStoreConfig('develop-auth', authAPI),
  getters: {
    username: (state) => state.user?.username || ''  // 覆盖了 isAuthenticated!
  }
})

// ✅ 正确 - 使用 createAuthStore 和 extraGetters
export const useAuthStore = defineStore('develop-auth',
  createAuthStore('develop-auth', authAPI, {
    persistUser: true,
    extraGetters: {
      username: (state) => state.user?.username || ''
    }
  })
)
```

#### 4. 路由守卫 (`router/index.js`)

**所有模块必须使用 `createAuthGuard()` 工厂函数**:

```javascript
import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/meta/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Meta',
  loginRouteName: 'Login'
}))

export default router
```

**`createAuthGuard()` 处理的内容:**
- Token 验证和用户加载
- 登录页重定向
- Query token 处理 (Portal iframe 模式)
- 开发/生产环境路径规范化

#### 5. 模块命名规范

**Store 名称必须使用模块前缀:**
```javascript
// ✅ 正确的命名
'develop-auth'    // Develop 模块
'meta-auth'       // Meta 模块
'manager-auth'    // Manager 模块
'transfer-auth'   // Transfer 模块
'orchestrator-auth' // Orchestrator 模块
'system-auth'     // System 模块
'portal-auth'     // Portal 模块
```

**为什么这很重要:**
- 防止 Pinia store 名称冲突
- 调试更容易 (清晰的模块归属)
- 与 localStorage key 命名保持一致

#### 6. 总结检查清单

创建或更新模块前端时,确保:

- [ ] `api/auth.js` 使用独立的 `systemClient` (不是模块的 client)
- [ ] `api/client.js` 使用 `createAPIClient()` 工厂函数
- [ ] `store/auth.js` 使用 `createAuthStore()` 工厂函数
- [ ] 所有模块都设置 `persistUser: true`
- [ ] Router 使用 `createAuthGuard()` 工厂函数
- [ ] Store 名称遵循 `{module}-auth` 约定
- [ ] 没有手动的拦截器代码 (使用工厂函数)
- [ ] 没有手动的 getter 合并 (使用 `extraGetters`)

**详细实现请参考:**
- [common-frontend/basic/src/composables/useAuth.js](common-frontend/basic/src/composables/useAuth.js) - 工厂函数
- [develop/auth.md](develop/auth.md) - 常见认证 bug 和解决方案
- 任意模块的 `api/`, `store/`, `router/` 目录作为参考实现
