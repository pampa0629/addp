# 认证问题排查记录

## 问题现象

**时间**: 2025-12-16

**症状**:
- 用户输入用户名和密码，点击登录
- 提示"登录成功"
- 但界面仍然停留在登录页面，无法跳转到目标页面

**日志输出**:
```
[Login] Starting login process...
[develop-auth] Starting fetchUser
[develop-auth] fetchUser completed successfully
[Login] Login successful, token saved
[Login] Auth state: {isAuthenticated: undefined, hasUser: true, username: 'admin'}
[Login] Redirecting to: /sql
[Login] ⏸️  等待 5 秒后跳转，方便查看日志...
```

## 问题根源

### 核心原因：`isAuthenticated` getter 被覆盖

在 `develop/frontend/src/stores/auth.js` 中，原始代码如下：

```javascript
export const useAuthStore = defineStore('develop-auth', {
  ...createAuthStoreConfig('develop-auth', authAPI, {
    persistUser: true
  }),

  getters: {
    // 扩展：保留 username getter
    username: (state) => state.user?.username || ''
  }
})
```

**问题所在**：

1. `createAuthStoreConfig()` 返回的配置对象中包含：
   - `state`
   - `getters: { isAuthenticated: (state) => !!state.token }`
   - `actions`

2. 使用 `...createAuthStoreConfig(...)` 展开时，会得到这些属性

3. **关键问题**：后面又定义了 `getters: { username: ... }`

4. 在 JavaScript 对象字面量中，**后定义的同名属性会覆盖前面的属性**

5. 因此，最终的 `getters` 对象只包含 `username`，**`isAuthenticated` 被完全覆盖丢失**

### 验证方式

从日志中可以看到：
```javascript
{isAuthenticated: undefined, hasUser: true, username: 'admin'}
```

- `isAuthenticated: undefined` - getter 不存在，返回 undefined
- `hasUser: true` - 用户信息加载成功
- `username: 'admin'` - 自定义的 username getter 正常工作

### 连锁反应

1. 路由守卫 `createAuthGuard` 检查 `authStore.isAuthenticated`
2. 因为 `isAuthenticated === undefined`（falsy 值），被判定为未登录
3. 路由守卫拒绝跳转，或重定向回登录页
4. 导致用户看似登录成功，实则无法进入系统

## 解决方案

### 修复代码

```javascript
import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

const baseConfig = createAuthStoreConfig('develop-auth', authAPI, {
  persistUser: true  // 启用用户信息持久化
})

export const useAuthStore = defineStore('develop-auth', {
  ...baseConfig,

  getters: {
    // ✅ 继承基础 getters（包括 isAuthenticated）
    ...baseConfig.getters,
    // 扩展：保留 username getter
    username: (state) => state.user?.username || ''
  }
})
```

### 修复原理

1. 先将 `createAuthStoreConfig()` 的结果保存到 `baseConfig` 变量
2. 展开 `baseConfig` 获取 state、actions 等
3. 在 `getters` 中：
   - 先展开 `baseConfig.getters`（包含 `isAuthenticated`）
   - 再添加自定义的 `username` getter
4. 这样既保留了基础 getters，又扩展了新功能

### 验证修复

修复后的日志应该显示：
```javascript
{isAuthenticated: true, hasUser: true, username: 'admin'}
```

## 教训总结

### 1. JavaScript 对象展开的陷阱

**错误示例**：
```javascript
const obj = {
  ...{ foo: 1, bar: 2 },
  foo: 3  // ❌ 覆盖了前面的 foo: 1
}
// 结果: { foo: 3, bar: 2 }
```

**正确示例**：
```javascript
const base = { foo: 1, bar: 2 }
const obj = {
  ...base,
  nested: {
    ...base.nested,  // ✅ 继承嵌套对象
    baz: 3           // 扩展新属性
  }
}
```

### 2. Pinia Store 扩展模式

当使用工厂函数创建 Store 配置时，扩展 getters/actions 的正确方式：

```javascript
// ✅ 正确：先保存配置，再展开嵌套属性
const baseConfig = createStoreConfig()
export const useStore = defineStore('name', {
  ...baseConfig,
  getters: {
    ...baseConfig.getters,  // 继承
    custom: () => {}        // 扩展
  }
})

// ❌ 错误：直接覆盖
export const useStore = defineStore('name', {
  ...createStoreConfig(),
  getters: {
    custom: () => {}  // 覆盖了所有基础 getters！
  }
})
```

### 3. 调试技巧

当遇到认证问题时，优先检查：

1. **Token 是否正确保存**：
   ```javascript
   console.log('Token:', authStore.token)
   console.log('LocalStorage Token:', localStorage.getItem('token'))
   ```

2. **User 信息是否加载**：
   ```javascript
   console.log('User:', authStore.user)
   console.log('LocalStorage User:', localStorage.getItem('user'))
   ```

3. **关键 Getters 是否存在**：
   ```javascript
   console.log('isAuthenticated:', authStore.isAuthenticated)
   console.log('isAuthenticated type:', typeof authStore.isAuthenticated)
   ```

4. **检查 getter 定义**：
   ```javascript
   // 在浏览器控制台
   const store = useAuthStore()
   console.log(Object.keys(store.$options.getters))  // 查看所有 getters
   ```

## 影响范围

### 其他模块检查

需要检查是否有其他模块存在同样的问题：

- [x] **Develop 模块** - 已修复（本次问题）
- [ ] **Meta 模块** - 需要检查 `meta/frontend/src/store/auth.js`
- [ ] **Manager 模块** - 需要检查 `manager/frontend/src/store/auth.js`
- [ ] **Transfer 模块** - 需要检查 `transfer/frontend/src/store/auth.js`
- [ ] **Orchestrator 模块** - 需要检查 `orchestrator/frontend/src/store/auth.js`
- [ ] **System 模块** - 需要检查 `system/frontend/src/stores/auth.js`

### 检查脚本

```bash
# 查找所有使用 createAuthStoreConfig 的文件
grep -r "createAuthStoreConfig" */frontend/src/store*/auth.js

# 检查是否存在覆盖问题
grep -A 5 "createAuthStoreConfig" */frontend/src/store*/auth.js | grep -B 3 "getters:"
```

## 预防措施

### 1. 代码规范

在 `common-frontend/README.md` 中添加使用说明：

```markdown
### ⚠️ 扩展 Auth Store 的正确方式

如果需要添加自定义 getters 或 actions：

\`\`\`javascript
const baseConfig = createAuthStoreConfig('module-auth', authAPI)

export const useAuthStore = defineStore('module-auth', {
  ...baseConfig,
  getters: {
    ...baseConfig.getters,  // ✅ 必须继承基础 getters
    custom: () => {}
  },
  actions: {
    ...baseConfig.actions,  // ✅ 必须继承基础 actions
    customAction() {}
  }
})
\`\`\`
```

### 2. TypeScript 类型检查

如果项目迁移到 TypeScript，可以通过类型系统强制检查：

```typescript
interface AuthStore {
  isAuthenticated: boolean  // 必需
  user: User | null
  token: string | null
  // ...其他属性
}
```

### 3. 单元测试

为 auth store 添加测试，确保关键 getters 存在：

```javascript
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './auth'

describe('AuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('should have isAuthenticated getter', () => {
    const store = useAuthStore()
    expect(store.isAuthenticated).toBeDefined()
    expect(typeof store.isAuthenticated).toBe('boolean')
  })
})
```

## 参考文件

- 问题文件：`develop/frontend/src/stores/auth.js`
- 修复提交：[待添加 commit hash]
- 相关文档：`common-frontend/README.md`
- 路由守卫：`common-frontend/basic/src/composables/useAuth.js`
