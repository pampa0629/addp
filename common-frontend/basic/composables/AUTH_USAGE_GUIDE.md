# Common Frontend 浏览器认证使用指南

本文说明 `common-frontend/basic/src` 提供的统一浏览器认证主路径。正式安全要求以 `docs/spec/addp登录认证的统一要求.md` 为准。

## 一、浏览器会话模型

1. Web Refresh Token 只保存在 System 设置的 HttpOnly Cookie 中。
2. Access Token 只保存在 `authSession.js` 的 JavaScript 内存中。
3. 页面启动时调用 `/api/v1/system/refresh` 静默恢复会话。
4. Access Token 到期前由 Browser AuthSession 主动刷新；业务请求遇到 401 时只兜底刷新并重试一次。
5. 同源顶层页面通过 Web Locks、BroadcastChannel 和无 Token 的短期锁租约协调刷新与登出。
6. Console 是 iframe 集成模式下唯一刷新协调者；模块 iframe 通过可信 `postMessage` 获取 Access Token。
7. Portal 新窗口使用 Console 当前 origin 的 `/portal/` 正式入口，与 Console 共享顶层页面刷新锁和 Token 广播。

禁止：

- 在 localStorage、sessionStorage、IndexedDB 或 URL 中持久化 Access Token；
- 从路由 query 读取 Token；
- 在 Console iframe URL 或新窗口 URL 中拼接 `?token=`；
- 从 Console 打开 Portal 前端开发端口，形成无法协调的第二个顶层认证 origin；
- 为旧认证方式保留兼容分支。

## 二、Auth Store

模块使用 `createAuthStore()` 创建 Pinia store：

```javascript
import { defineStore } from 'pinia'
import { createAuthStore } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore(
  'manager-auth',
  createAuthStore('manager-auth', authAPI, {
    persistUser: false
  })
)
```

`persistUser` 只控制非敏感当前用户资料，不影响 Token；Access Token 始终只在内存中。

认证 API 使用 `createAuthAPI()`：

```javascript
import axios from 'axios'
import { createAuthAPI } from '@common-ui'

const systemClient = axios.create({
  baseURL: '/api/v1/system',
  timeout: 10_000
})

export const authAPI = createAuthAPI(systemClient)
```

`login`、`refresh` 和 `logout` 会携带 Cookie 凭据；前端代码不能读取 Refresh Token。

## 三、路由守卫

所有模块使用 `createAuthGuard()`：

```javascript
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

router.beforeEach(createAuthGuard(useAuthStore, {
  loginRouteName: 'Login'
}))
```

守卫会先执行 `initializeSession()`：

- 顶层页面优先请求同源标签页的内存 Token，没有可用 Token 时通过 Cookie 刷新；
- 初始化身份请求遇到 401 时，在全局刷新锁内强制刷新并重试一次，避免把已被服务端轮换撤销的标签页 Token 误判为有效会话；
- iframe 请求 Console 父页面提供 Token；
- iframe 在认证超时窗口内使用同一个请求 ID 重发握手，直到父 Console 返回明确结果；
- 认证失败才进入登录页；
- 网络故障标记会话初始化错误，不把用户错误地当作已退出。

## 四、API 请求

优先通过 `createAPIClient()` 创建 Axios 客户端：

```javascript
import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

export const client = createAPIClient(useAuthStore, {
  moduleName: 'Manager',
  baseURL: '/api/v1/manager'
})
```

SSE、流式上传等 Fetch 场景使用 `createAuthenticatedFetch()`。两者都会在请求时读取当前运行时 Token，并统一处理一次 401 刷新重试。

可控 Fetch、SSE 或第三方加载器允许设置请求 Header 时，只能通过运行时 Token Provider 获取当前 Token，并写入 `Authorization` Header：

```javascript
import { getAccessToken } from '@common-ui'

const token = getAccessToken()
```

原生图片、视频、PDF、下载、新窗口和无法设置 Header 的三维子资源必须直接使用干净 URL。System 在登录和刷新时设置 Owner Path 限定的 `addp_resource_access_ticket` HttpOnly Cookie，浏览器自动携带；前端不得读取票据，也不得把 User Access Token 或 Resource Access Ticket 拼入 URL。

## 五、Console iframe

Console 使用 `createIframeAuthCoordinator()`，并只允许配置中的模块 origin：

```javascript
const coordinator = createIframeAuthCoordinator({
  allowedOrigins,
  getToken: getAccessToken,
  getExpiresAt: getAccessTokenExpiresAt,
  refreshToken: options => authStore.refreshAccessToken(options),
  logout: () => authStore.logout()
})
```

协调器同时校验消息的 `origin` 和 `source` 是否属于当前页面中的 iframe。模块不需要编写 Token query 解析逻辑；统一 AuthSession 会自动向可信父页面请求 Token。

握手消息可能早于 Console 组件的挂载回调，因此模块会在超时窗口内重发同一个幂等请求。Console
不得依赖只接收一次 `addp-auth-ready`，也不得把重复握手视为重复登录或创建新的会话。

## 六、验证

涉及认证改动时至少执行：

```bash
cd agent/frontend
npm test -- --run tests/authSession.test.js tests/authRefresh.test.js tests/portalConfig.test.js
```

同时扫描运行时代码，确保没有重新引入 `localStorage.token`、query Token 或 Console iframe Token URL。
