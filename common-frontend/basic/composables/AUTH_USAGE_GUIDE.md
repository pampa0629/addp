# Common Frontend 认证模块使用指南

本文档介绍如何使用 `common-frontend/basic/composables/useAuth.js` 提供的通用认证功能。

## 📦 导出的功能

### 1. `createAuthGuard(authStore, config)` - 路由守卫工厂

创建标准化的 Vue Router `beforeEach` 守卫,处理所有认证相关逻辑:

- ✅ **Step 0**: 智能等待用户加载完成 (Promise 链)
- ✅ **Step 1**: 处理 Portal iframe 传递的 query token
- ✅ **Step 2**: 刷新页面后自动恢复用户信息
- ✅ **Step 3**: iframe 环境自动放行
- ✅ **Step 4**: 标准路由守卫 (检查认证状态)

### 2. `createAuthInterceptor(authStore, moduleName)` - Axios 拦截器

创建智能等待的请求拦截器:

- ✅ 检测 `isLoadingUser` 状态并等待完成
- ✅ 自动添加 `Authorization` 头
- ✅ 详细的日志输出

### 3. `createAuthStoreConfig(storeName, authAPI, options)` - Auth Store 配置

生成标准化的 Pinia auth store 配置:

- ✅ Promise 链机制 (`userLoadPromise`)
- ✅ `waitForUserLoad()` 方法
- ✅ 可选的用户信息持久化
- ✅ 标准的 login/logout/fetchUser 方法

---

## 🚀 快速开始

### 步骤 1: 配置 Vite Alias

在模块的 `vite.config.js` 中添加 `@common-ui` alias:

```javascript
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
    }
  }
})
```

### 步骤 2: 创建 Auth Store (简化版)

**原来的方式** (每个模块重复实现):

```javascript
// manager/frontend/src/store/auth.js
import { defineStore } from 'pinia'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('manager-auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: null,
    isLoadingUser: false,
    userLoadPromise: null
  }),

  getters: {
    isAuthenticated: (state) => !!state.token
  },

  actions: {
    async fetchUser() {
      // ... 100+ 行重复代码
    },
    async waitForUserLoad() {
      // ... 重复逻辑
    }
    // ... 更多重复方法
  }
})
```

**新的方式** (使用 common-frontend):

```javascript
// manager/frontend/src/store/auth.js
import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('manager-auth', {
  ...createAuthStoreConfig('manager-auth', authAPI, {
    persistUser: false  // Manager 不持久化 user
  })
})
```

**对于需要持久化 user 的模块** (Meta, Transfer, Orchestrator):

```javascript
export const useAuthStore = defineStore('meta-auth', {
  ...createAuthStoreConfig('meta-auth', authAPI, {
    persistUser: true  // 持久化到 localStorage
  })
})
```

### 步骤 3: 配置路由守卫 (一行代码)

**原来的方式** (每个模块重复 100+ 行):

```javascript
// manager/frontend/src/router/index.js
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()
  const queryToken = typeof to.query.token === 'string' ? to.query.token : null

  // 0️⃣ Step 0 逻辑 (20 行)
  if (authStore.isLoadingUser) {
    // ...
  }

  // 1️⃣ Step 1 逻辑 (30 行)
  if (queryToken) {
    // ...
  }

  // 2️⃣ Step 2 逻辑 (20 行)
  // 3️⃣ Step 3 逻辑 (10 行)
  // 4️⃣ Step 4 逻辑 (20 行)
})
```

**新的方式**:

```javascript
// manager/frontend/src/router/index.js
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

router.beforeEach(createAuthGuard(useAuthStore(), {
  moduleName: 'Manager',
  loginRouteName: 'Login'
}))
```

**对于需要重定向规范化的模块** (Meta, Transfer):

```javascript
// meta/frontend/src/router/index.js
const normalizeRedirect = (path) => {
  const normalized = path.replace(/^\/meta\//, '/')
  return normalized === '/' ? '/assets' : normalized
}

router.beforeEach(createAuthGuard(useAuthStore(), {
  moduleName: 'Meta',
  loginRouteName: 'Login',
  normalizeRedirect  // 传入自定义规范化函数
}))
```

### 步骤 4: 配置 Axios 拦截器 (一行代码)

**原来的方式**:

```javascript
// manager/frontend/src/api/client.js
import { useAuthStore } from '../store/auth'

client.interceptors.request.use(
  async config => {
    const authStore = useAuthStore()

    // 如果正在加载用户,等待完成
    if (authStore.isLoadingUser) {
      console.log('[Manager Client] Waiting for user to load before sending request')
      try {
        await authStore.waitForUserLoad()
      } catch (error) {
        console.error('[Manager Client] Failed to wait for user:', error)
      }
    }

    if (authStore.token) {
      config.headers.Authorization = `Bearer ${authStore.token}`
    }
    return config
  }
)
```

**新的方式**:

```javascript
// manager/frontend/src/api/client.js
import { createAuthInterceptor } from '@common-ui'
import { useAuthStore } from '../store/auth'

client.interceptors.request.use(
  createAuthInterceptor(useAuthStore(), 'Manager')
)
```

---

## 📊 代码减少对比

| 文件 | 原来代码行数 | 使用 common-frontend 后 | 减少 |
|------|-------------|------------------------|------|
| `store/auth.js` | ~120 行 | ~10 行 | **-91%** |
| `router/index.js` | ~100 行守卫逻辑 | ~10 行 | **-90%** |
| `api/client.js` | ~20 行拦截器 | ~3 行 | **-85%** |
| **每个模块总计** | **~240 行** | **~23 行** | **-90%** |
| **6 个模块总计** | **~1440 行** | **~138 行** | **-90%** |

**节省维护成本**: 1300+ 行重复代码消除! 🎉

---

## 🎯 完整迁移示例

### Manager 模块迁移

#### 1. `manager/frontend/src/store/auth.js` (从 120 行 → 10 行)

```javascript
import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import authAPI from '../api/auth'

export const useAuthStore = defineStore('manager-auth', {
  ...createAuthStoreConfig('manager-auth', authAPI, {
    persistUser: false
  })
})
```

#### 2. `manager/frontend/src/router/index.js` (从 100 行 → 10 行)

```javascript
import { createRouter, createWebHistory } from 'vue-router'
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

const routes = [
  // ... 路由定义
]

const router = createRouter({
  history: createWebHistory('/manager/'),
  routes
})

router.beforeEach(createAuthGuard(useAuthStore(), {
  moduleName: 'Manager',
  loginRouteName: 'Login'
}))

export default router
```

#### 3. `manager/frontend/src/api/client.js` (从 20 行 → 3 行)

```javascript
import axios from 'axios'
import { createAuthInterceptor } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = axios.create({
  baseURL: import.meta.env.PROD
    ? `${window.location.protocol}//${window.location.hostname}:8081/api`
    : 'http://localhost:8081/api',
  timeout: 10000
})

// 一行配置请求拦截器
client.interceptors.request.use(
  createAuthInterceptor(useAuthStore(), 'Manager')
)

// 响应拦截器保持不变 (模块特定逻辑)
client.interceptors.response.use(
  response => response,
  error => {
    // ... 模块特定错误处理
    return Promise.reject(error)
  }
)

export default client
```

---

## ⚙️ API 参考

### `createAuthGuard(authStore, config)`

**参数**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `authStore` | Object | ✅ | - | Pinia auth store 实例 (调用 `useAuthStore()`) |
| `config.moduleName` | String | ❌ | `'Module'` | 模块名称 (用于日志输出) |
| `config.loginRouteName` | String | ❌ | `'Login'` | 登录路由名称 |
| `config.normalizeRedirect` | Function | ❌ | `(path) => path` | 重定向路径规范化函数 |

**返回**: `Function` - Vue Router beforeEach 守卫函数

---

### `createAuthInterceptor(authStore, moduleName)`

**参数**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `authStore` | Object | ✅ | - | Pinia auth store 实例 |
| `moduleName` | String | ❌ | `'Module'` | 模块名称 (用于日志输出) |

**返回**: `Function` - Axios request interceptor

---

### `createAuthStoreConfig(storeName, authAPI, options)`

**参数**:

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `storeName` | String | ✅ | - | Store 名称 (建议: `'{module}-auth'`) |
| `authAPI` | Object | ✅ | - | 认证 API 对象 (需提供 `login()` 和 `getUser()` 方法) |
| `options.persistUser` | Boolean | ❌ | `true` | 是否持久化 user 到 localStorage |

**authAPI 对象要求**:

```javascript
{
  login: async (username, password) => {
    // 返回: { data: { access_token: '...' } }
  },
  getUser: async (token) => {
    // 返回: { data: { id, username, ... } }
  }
}
```

**返回**: `Object` - Pinia store 配置对象 (需与 `defineStore()` 配合使用)

---

## 🔧 高级用法

### 自定义 Auth API 适配

如果模块的 API 方法名不同,可以创建适配器:

```javascript
// system/frontend/src/store/auth.js
import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI as systemAuthAPI } from '../api/auth'

// 适配器 (System 使用 getMe() 而非 getCurrentUser())
const authAPI = {
  login: systemAuthAPI.login,
  getUser: (token) => systemAuthAPI.getMe()  // 适配方法名
}

export const useAuthStore = defineStore('system-auth', {
  ...createAuthStoreConfig('system-auth', authAPI, {
    persistUser: false
  })
})
```

### 扩展 Auth Store

如果需要添加模块特定的 state 或 actions:

```javascript
import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('manager-auth', {
  ...createAuthStoreConfig('manager-auth', authAPI),

  // 扩展 state
  state: () => ({
    ...createAuthStoreConfig('manager-auth', authAPI).state(),
    customField: null  // 模块特定字段
  }),

  // 扩展 actions
  actions: {
    ...createAuthStoreConfig('manager-auth', authAPI).actions,

    customAction() {
      // 模块特定方法
      console.log('Custom action')
    }
  }
})
```

---

## 📝 迁移清单

为每个模块执行以下步骤:

- [ ] **步骤 1**: 在 `vite.config.js` 添加 `@common-ui` alias
- [ ] **步骤 2**: 简化 `store/auth.js` 使用 `createAuthStoreConfig()`
- [ ] **步骤 3**: 简化 `router/index.js` 使用 `createAuthGuard()`
- [ ] **步骤 4**: 简化 `api/client.js` 使用 `createAuthInterceptor()`
- [ ] **步骤 5**: 测试模块功能 (登录、刷新、iframe 集成)
- [ ] **步骤 6**: 删除旧的重复代码和注释

---

## ✅ 收益总结

1. **代码减少 90%**: 每个模块从 ~240 行认证代码减少到 ~23 行
2. **统一实现**: 所有模块使用完全相同的认证逻辑 (无差异风险)
3. **易于维护**: 修复 bug 或添加功能只需修改 `common-frontend/basic/composables/useAuth.js`
4. **类型安全**: 单一实现源保证逻辑一致性
5. **文档清晰**: 所有模块遵循相同模式,新人上手更快

---

## 🐛 故障排除

### 问题 1: `@common-ui` 导入失败

**错误**: `Cannot find module '@common-ui'`

**解决**:
1. 检查 `vite.config.js` 中的 alias 配置
2. 确保路径正确: `resolve(__dirname, '../../common-frontend/basic/src')`
3. 重启 Vite 开发服务器

### 问题 2: `authAPI.getUser is not a function`

**错误**: 传入的 authAPI 对象缺少 `getUser()` 方法

**解决**:
- 检查传入的 authAPI 对象是否包含 `login()` 和 `getUser()` 方法
- 如果方法名不同 (如 `getMe()`),创建适配器 (见"高级用法"章节)

### 问题 3: Store 名称冲突

**错误**: 多个模块使用相同的 store 名称 (如 `'auth'`)

**解决**:
- 统一使用 `{module}-auth` 格式: `'manager-auth'`, `'meta-auth'` 等
- 每个模块的 store 名称必须唯一

---

## 📚 相关文档

- [common-frontend/README.md](../README.md) - Common Frontend 总体介绍
- [common-frontend/ARCHITECTURE.md](../ARCHITECTURE.md) - 架构设计文档
- [system/addp登录问题排查.md](../../system/addp登录问题排查.md) - Promise 链方案原始分析
