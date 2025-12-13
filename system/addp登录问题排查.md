# 修复 Develop 模块登录状态不稳定问题

## 问题分析

### 现象描述
用户在 Portal 中用 admin 账户登录后，点击"数据开发"的"SQL 工作台"时，**有时会弹出登录页面**，但点击其他页面后再回到 SQL 工作台时又能正常显示。这种不稳定的行为表明存在认证状态的竞态条件（Race Condition）。

### 根本原因（初步分析）

经过深入代码分析，我发现了一个**潜在的竞态条件问题**：

#### 问题 1：路由守卫中的异步操作未完全完成前就跳转

**文件**: `develop/frontend/src/router/index.js`（第 64-114 行）

```javascript
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()
  const queryToken = typeof to.query.token === 'string' ? to.query.token : null

  // 1️⃣ 接收 Portal 传递的 token
  if (queryToken) {
    authStore.setToken(queryToken)  // ← 同步设置 token

    try {
      await authStore.fetchUser()   // ← 异步获取用户信息（需要 HTTP 请求）
    } catch (error) {
      console.error('获取用户信息失败:', error)
      authStore.logout()
      return next({ name: 'Login' })
    }

    // ⚠️ 关键问题：立即清除 URL token 并重新导航
    const { token: _removed, ...restQuery } = to.query
    next({ path: to.path, query: restQuery, replace: true })
    return  // ← 此时 fetchUser() 可能还未完成！
  }

  // 2️⃣ 如果已认证但无用户信息，尝试刷新
  if (authStore.isAuthenticated && !authStore.user) {
    try {
      await authStore.fetchUser()  // ← 再次异步请求
    } catch (error) {
      console.error('刷新用户信息失败:', error)
      authStore.logout()
      return next({ name: 'Login' })
    }
  }

  // 3️⃣ iframe 中已认证 → 直接通过
  const isInIframe = window.self !== window.top
  if (isInIframe && authStore.isAuthenticated) {
    return next()  // ← 可能此时 user 还未加载完成
  }

  // 4️⃣ 正常路由检查
  if (authStore.isAuthenticated) {
    next()
  } else {
    next('/login')
  }
})
```

**竞态场景**：

```
时间线  Portal 操作           Develop iframe 路由守卫                  用户看到的结果
-----  --------------      ---------------------------------     ----------------
T0     点击"SQL 工作台"      →
T1                         接收 URL token (?token=xxx)
                           调用 setToken(queryToken)
                           token 已保存到 localStorage ✅

T2                         调用 await fetchUser()
                           发起 HTTP 请求到 System Backend →

T3                         next({ path, query, replace: true })
                           重新导航，清除 URL token

T4                         ❌ 再次进入路由守卫！
                           此时 queryToken 已清除（因为 URL 已被 replace）
                           检查 isAuthenticated: true ✅
                           检查 authStore.user: null ❌（HTTP 还未返回）

T5                         ↓ 进入步骤 2️⃣
                           if (isAuthenticated && !user)
                           再次调用 fetchUser()
                           发起第二个 HTTP 请求 →

T6                         ↓ 进入步骤 3️⃣
                           isInIframe: true ✅
                           isAuthenticated: true ✅
                           → next() 通过 ✅

T7     iframe 内容显示      第一个 fetchUser() HTTP 返回（user 信息）  显示正常 ✅
```

**问题关键**：
- 在 T3 时刻，`next({ path, query, replace: true })` 会立即触发**第二次路由守卫检查**
- 但此时 `fetchUser()` 的 HTTP 请求可能还未返回（T7 才返回）
- 导致 T4-T6 期间，`authStore.user` 为 `null`，触发步骤 2️⃣ 再次请求用户信息

**为什么"有时正常，有时不正常"**？
- 如果 `fetchUser()` 的 HTTP 请求在 T3 之前返回 → 正常 ✅
- 如果 `fetchUser()` 的 HTTP 请求在 T3 之后返回 → 可能出现登录页 ⚠️
- 取决于网络速度、后端响应时间、浏览器调度等不确定因素

#### 问题 2：Layout 组件加载时机与路由守卫冲突

**文件**: `develop/frontend/src/views/Layout.vue`（第 1-94 行）

```javascript
onMounted(() => {
  isInIframe.value = window.self !== window.top
})
```

- Layout 组件在路由守卫**之后**才挂载
- 如果路由守卫检查通过但 user 信息未加载，Layout 可能渲染出登录页的内容
- 特别是当 Layout 内部有条件渲染依赖 `authStore.user` 时

#### 问题 3：潜在的 localStorage 同步问题

在 iframe 嵌入模式下：
- Portal 和 Develop iframe 共享 `localStorage`（同源策略）
- 但 iframe 的路由守卫**可能在 localStorage 同步之前**就执行
- 导致 `authStore.isAuthenticated` 检查失败

### 为什么"点击其他页面再回来就正常了"？

第一次点击时：
- iframe 加载 → 路由守卫检查 → fetchUser() 未完成 → 可能显示登录页

第二次点击时：
- iframe 已加载 → localStorage 已有 token → user 已缓存在 authStore
- 路由守卫检查 → 直接通过（无需 HTTP 请求）→ 正常显示 ✅

## ⚠️ 修复不完整！需要进一步补充修复

经过实际测试，发现初步修复**不够完善**。问题比预想的更复杂，存在**三层并发竞态**：

### 新发现的问题

**现象**（从用户提供的截图）：
1. 点击"GIS 工作流编辑器"时仍然弹出登录页
2. 控制台显示多个 401 错误：
   - `Failed to load :8080/api/users/me` (401)
   - `Failed to load :5178/api/develop/spatial/operators` (401，多次)

**根本原因**（三层并发）：

```
Layer 1: 路由守卫层
├─ ❌ isLoadingUser 检查时直接放行，没有等待完成
├─ fetchUser() 异步执行，但路由继续转换
└─ 组件开始渲染时 user 可能还未加载

Layer 2: API 客户端层
├─ 请求拦截器从 localStorage 读取 token
├─ 但读取时可能 fetchUser() 还未完成
└─ 导致部分请求没有 Authorization header → 401

Layer 3: 组件层
├─ GISWorkflowEditor onMounted() 立即调用 loadOperators()
├─ 没有检查 authStore.isLoadingUser 或等待 user 加载
└─ API 请求在 token 未就绪时发出 → 401
```

### 关键时序问题

```
时间线 (首次访问 GIS 工作流编辑器):

T1: 路由守卫检测到 isLoadingUser = false
T2: 接收 URL token，调用 setToken()（同步）
T3: 调用 fetchUser()（异步，开始执行）
T4: ❌ 当前修复：检查 isLoadingUser = true → 直接 next() 放行！
T5: 组件开始渲染 → onMounted() 触发
T6: OperatorPalette 调用 loadOperators() → 发送 HTTP 请求
T7: 请求拦截器：localStorage.getItem('token') → 有 token
T8: ⚠️ 但 fetchUser() 可能还在处理中 → 可能导致 401
T9: fetchUser() 完成，user 已设置
```

## 🔍 深度对比分析：为什么其他模块没有这个问题

经过全面对比分析所有 6 个模块（System、Manager、Meta、Transfer、Orchestrator、Develop），我发现了惊人的真相：

### 核心发现

**Develop 不是唯一有问题的模块，而是唯一暴露了问题的模块！**

#### 问题对比表

| 模块 | 路由守卫步骤 0 | 路由守卫步骤 2 | 组件防御 | 表现 | 原因 |
|-----|--------------|--------------|---------|------|------|
| **System** | ❌ 无 | ❌ 无 | ✅ **有** | 正常 | 组件层等待用户加载 |
| **Manager** | ❌ 无 | ✅ **await fetchUser()** | ❌ 无 | 正常 | 步骤 2 阻塞生效 |
| **Meta** | ❌ 无 | ✅ **await fetchUser()** | ❌ 无 | 正常 | 步骤 2 阻塞生效 |
| **Transfer** | ❌ 无 | ✅ **await fetchUser()** | ❌ 无 | 正常 | 步骤 2 阻塞生效 |
| **Orchestrator** | ❌ 无 | ✅ **await fetchUser()** | ❌ 无 | 正常 | 步骤 2 阻塞生效 |
| **Develop** | ⚠️ **有（直接放行）** | ✅ await fetchUser() | ❌ 无 | ❌ **401** | **步骤 0 破坏了步骤 2** |

### 根本原因

**Develop 的步骤 0 破坏了步骤 2 的保护机制！**

**时间线对比**：

```
Manager/Meta/Transfer/Orchestrator (正常):
T1: 路由守卫触发
T2: 步骤 2 检测：isAuthenticated=true, user=null
T3: 开始 fetchUser()，设置 isLoadingUser=true
T4: ⏳ 等待 fetchUser() 完成（await 阻塞）
T5: fetchUser() 完成，user 已加载 ✅
T6: next() 放行
T7: 组件挂载 → onMounted → API 请求 → 200 OK ✅

Develop (401 错误):
T1: 路由守卫触发
T2: 步骤 2 检测：isAuthenticated=true, user=null
T3: 开始 fetchUser()，设置 isLoadingUser=true
T4: fetchUser() 异步执行中...
T5: ⚠️ Vue Router 重新导航（清除 URL token 后）
T6: 步骤 0 检测：isLoadingUser=true
T7: ❌ 直接 next() 放行（不等待！）
T8: 组件挂载 → onMounted → API 请求
T9: fetchUser() 还未完成 → 401 错误 ❌
```

### System 的特殊防御（为何它也正常）

System 模块的组件中有防御性代码：

```javascript
// system/frontend/src/views/Users.vue
onMounted(async () => {
  // ✅ 确保用户信息已加载
  if (!authStore.user) {
    try {
      await authStore.fetchUser()  // 等待
    } catch (error) {
      console.error('Failed to load user:', error)
    }
  }
  loadUsers()  // 然后才加载数据
})
```

**代价**：每个组件都要写防御代码（违反 DRY 原则）

### 关键结论

1. **Develop 的 isLoadingUser 机制方向正确**，但步骤 0 实现错误
2. **其他模块不是没问题**，而是步骤 2 碰巧生效或有组件层防御
3. **需要统一所有模块的实现**

---

## 💡 最佳修复方案（基于最佳实践）

### 方案 A：优化 Develop 的 Auth Store（推荐）

**问题**：当前使用轮询等待，效率低下

**改进**：使用 Promise 链

```javascript
// develop/frontend/src/stores/auth.js
export const useAuthStore = defineStore('develop-auth', {
  state: () => ({
    token: localStorage.getItem('token') || null,
    user: null,
    isLoadingUser: false,
    userLoadPromise: null  // 🆕 存储 Promise
  }),

  actions: {
    setToken(token) {
      this.token = token
      localStorage.setItem('token', token)
    },

    async fetchUser() {
      // 如果已有请求在进行中，返回该 Promise
      if (this.userLoadPromise) {
        return this.userLoadPromise
      }

      this.isLoadingUser = true
      this.userLoadPromise = getCurrentUser()
        .then(response => {
          this.user = response.data
          return response
        })
        .catch(error => {
          console.error('获取用户信息失败:', error)
          throw error
        })
        .finally(() => {
          this.isLoadingUser = false
          this.userLoadPromise = null
        })

      return this.userLoadPromise
    },

    // 🆕 新增：等待用户加载完成
    async waitForUserLoad() {
      if (this.userLoadPromise) {
        return this.userLoadPromise
      }
      if (this.user) {
        return Promise.resolve({ data: this.user })
      }
      throw new Error('No user load in progress')
    },

    logout() {
      this.token = null
      this.user = null
      this.isLoadingUser = false
      this.userLoadPromise = null
      localStorage.removeItem('token')
    }
  }
})
```

### 方案 B：修复 Develop 的路由守卫步骤 0（关键）

**当前错误代码**（第 68-72 行）:
```javascript
// 0️⃣ 如果正在加载用户信息，直接放行（避免重复处理）
if (authStore.isLoadingUser) {
  console.log('[Router] User is loading, allowing navigation')
  return next()  // ❌ 这里是问题！
}
```

**正确修复代码**:
```javascript
// 0️⃣ 如果正在加载用户信息，等待完成后再放行
if (authStore.isLoadingUser) {
  console.log('[Router] User is loading, waiting for completion...')
  try {
    await authStore.waitForUserLoad()  // ✅ 等待已有请求完成
    console.log('[Router] User loaded successfully')
  } catch (error) {
    console.error('[Router] Failed to wait for user load:', error)
    authStore.logout()
    return next({ name: 'Login' })
  }
  return next()
}
```

### 方案 C：API 客户端添加智能等待（防护）

**文件**: `develop/frontend/src/api/client.js`

```javascript
import { useAuthStore } from '../stores/auth'

client.interceptors.request.use(
  async (config) => {
    const authStore = useAuthStore()

    // 如果正在加载用户，等待完成
    if (authStore.isLoadingUser) {
      console.log('[Client] Waiting for user to load before sending request')
      try {
        await authStore.waitForUserLoad()
      } catch (error) {
        console.error('[Client] Failed to wait for user:', error)
      }
    }

    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error)
)
```

---

## 📋 最终修改清单

### 紧急修复（Develop 模块）

| 文件 | 修改内容 | 行数 | 优先级 |
|------|---------|------|--------|
| **stores/auth.js** | 添加 `userLoadPromise` 和 `waitForUserLoad()` | 新增 | 🔴 P0 |
| **stores/auth.js** | 优化 `fetchUser()` 使用 Promise 链 | 34-71 | 🔴 P0 |
| **router/index.js** | 修改步骤 0：从"直接放行"改为"等待完成" | 68-72 | 🔴 P0 |
| **api/client.js** | 请求拦截器添加 `waitForUserLoad()` | 新增 | 🟡 P1 |

### 长期改进（所有模块）

| 任务 | 受影响模块 | 工作量 | 优先级 |
|-----|-----------|-------|--------|
| 统一路由守卫逻辑 | System, Manager, Meta, Transfer, Orchestrator | 4-6小时 | 🟡 P1 |
| 添加 isLoadingUser 机制 | System, Manager, Meta, Transfer, Orchestrator | 4-6小时 | 🟡 P1 |
| 提取到 common-frontend | common-frontend | 12-16小时 | 🟢 P2 |

---

## ✅ 预期效果

修复后：
1. **Develop 模块**：彻底消除 401 错误
2. **路由守卫**：步骤 0 正确等待 fetchUser 完成
3. **API 请求**：拦截器确保 token 就绪
4. **向后兼容**：不影响其他模块的正常运行

---

## 测试验证计划

### 测试场景 1：正常加载（快速网络）
**步骤**：
1. 在 Portal 中登录（admin / 123456）
2. 点击"数据开发" → "SQL 工作台"

**预期结果**：✅ SQL 工作台正常显示，不弹出登录页

**验证点**：
- 控制台日志：
  ```
  [Router] Found token in URL, processing...
  [Router] User fetched successfully, user: admin
  [Router] Removing token from URL
  [Router] User is loading, waiting for completion...  (第二次守卫)
  [Router] User loaded successfully
  ```

---

### 测试场景 2：慢速网络（模拟）
**步骤**：
1. 浏览器开发者工具 → Network → Throttling → Slow 3G
2. 在 Portal 中点击"SQL 工作台"

**预期结果**：✅ 即使网络慢，也不应该出现登录页，最终正常加载

**验证点**：
- `isLoadingUser` 在 `fetchUser()` 期间为 `true`
- 第二次路由守卫等待 `waitForUserLoad()` 完成

---

### 测试场景 3：GIS 工作流编辑器
**步骤**：
1. 在 Portal 中登录
2. 点击"数据开发" → "GIS 工作流编辑器"

**预期结果**：✅ 不出现 401 错误，operators 列表正常加载

**验证点**：
- 控制台无 401 错误
- API 拦截器等待 `waitForUserLoad()` 完成
- operators 数据正常显示

---

### 测试场景 4：快速切换菜单（并发导航）
**步骤**：
1. 在 Portal 中快速连续点击：SQL 工作台 → GIS 任务 → SQL 工作台

**预期结果**：✅ 每次都正常加载，不出现错误

**验证点**：
- Promise 链机制生效
- 不会发起多个并发 `fetchUser()` 请求

---

## 边界情况处理

### ✅ 场景 1：`fetchUser()` 失败（401 / 网络错误）
- **处理**：`authStore.logout()` 清除所有状态 → 跳转登录页
- **结果**：用户被引导到登录页，不会卡在加载状态

### ✅ 场景 2：用户在 `fetchUser()` 过程中刷新页面
- **处理**：`isLoadingUser` 重置为 `false`（内存状态）→ Token 保留在 localStorage → 重新执行步骤 2
- **结果**：刷新后重新加载用户信息，不会死锁

### ✅ 场景 3：iframe 被销毁后重新加载
- **处理**：Portal 重新创建 iframe 时传递 `?token=xxx` → 路由守卫重新执行步骤 1
- **结果**：每次 iframe 重新加载都会正确初始化认证状态

### ✅ 场景 4：网络极慢，`fetchUser()` 耗时 10 秒+
- **处理**：使用 Promise 链，无需超时保护（HTTP 请求本身有超时）
- **结果**：等待 HTTP 请求完成或失败

### ✅ 场景 5：并发多次调用 `fetchUser()`
- **处理**：第一次调用创建 Promise → 后续调用返回同一个 Promise
- **结果**：避免发起多个并发 HTTP 请求，节省资源

---

## 向后兼容性保证

### ✅ 独立访问模式（http://localhost:5178）
- 直接访问不携带 `?token` 参数
- 路由守卫执行步骤 2-4，逻辑不变
- **结论**：完全兼容

### ✅ Portal 嵌入模式（iframe）
- Portal 传递 `?token=xxx`，触发步骤 1
- 第二次守卫等待 `waitForUserLoad()` 完成
- **结论**：修复了竞态条件，完全兼容

### ✅ 其他模块（System, Manager, Meta, Transfer）
- 这些模块有相同的代码结构和潜在问题
- **短期**：先修复 Develop 模块验证有效性
- **长期**：将相同方案推广到其他模块

---

## 风险评估

- **风险等级**：🟢 低风险
- **影响范围**：仅 Develop 模块，不影响其他模块
- **改动集中**：3 个文件，逻辑清晰
- **可回滚**：如果出现问题，可以快速恢复旧代码
- **可测试**：提供了 4 个详细测试场景

---

## 实施建议

### 短期（本次修复）
1. ✅ 修改 `auth.js`、`router/index.js` 和 `client.js`
2. ✅ 在开发环境测试所有场景
3. ✅ 部署到生产环境观察
4. ✅ 收集用户反馈

### 长期（后续优化）
1. 将相同方案应用到 Manager, Meta, Transfer, Orchestrator 模块
2. 考虑将路由守卫逻辑提取到 `common-frontend` 中复用
3. 统一所有模块的认证流程
4. 添加性能监控和错误追踪

---

## 总结

**核心改进**：
1. 使用 Promise 链替代轮询，提升效率
2. 路由守卫步骤 0 等待用户加载完成，而非直接放行
3. API 客户端拦截器智能等待，确保 token 就绪
4. 三层防护机制，彻底解决竞态条件

**优势**：
- ✅ 彻底解决竞态条件
- ✅ 向后完全兼容
- ✅ 错误处理完善
- ✅ 性能优化（Promise 链代替轮询）
- ✅ 可维护性强（清晰的状态管理）

**关键发现**：
- **Develop 不是唯一有问题的模块**，而是唯一暴露了问题的模块
- 其他模块因步骤 2 的 await 阻塞或组件层防御代码而"碰巧"工作正常
- **步骤 0 的直接放行破坏了步骤 2 的保护机制**
- 使用 Promise 链比轮询更高效、更优雅
