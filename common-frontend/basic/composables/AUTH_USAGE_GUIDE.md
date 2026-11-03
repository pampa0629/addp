# Common Frontend 浏览器认证使用指南

本文维护 `common-frontend/basic/src` 的浏览器认证接入步骤与调用约束；安全规则以[登录认证规范](../../../docs/spec/addp登录认证的统一要求.md)为准。

[返回共享前端总入口](../../README.md) · [认证组件索引](../../docs/components/基础控件与工具.md#认证组件-composables) · [公共导出](../src/index.js)。

本页内容：[浏览器会话模型](#一浏览器会话模型)、[Auth Store](#二auth-store)、[路由守卫](#三路由守卫)、[登录落点与安全回跳](#32-登录落点与安全回跳)、[API 请求](#四api-请求)、[Console iframe](#五console-iframe)、[验证](#六验证)。

## 一、浏览器会话模型

令牌存储、静默恢复与单次 401 重试、多标签页协调和可信 iframe 握手统一遵循[令牌存储](../../../docs/spec/addp登录认证的统一要求.md#二浏览器令牌存储)、[Browser AuthSession](../../../docs/spec/addp登录认证的统一要求.md#三browser-authsession)、[多标签页协调](../../../docs/spec/addp登录认证的统一要求.md#四多标签页协调)及 [Console 与 iframe](../../../docs/spec/addp登录认证的统一要求.md#五console-与-iframe)。模块不得复制刷新流程或保留旧认证路径。

### 1.1 开发入口

模块独立页面和 iframe 均使用 Console 当前 origin 的 `/module-ui/{frontend}/` 正式入口；Portal 使用 `/portal/`，Workbench Data Application 使用 `/data-apps/`。同一浏览器配置的同源顶层页面共享刷新锁和 Token 广播，独立模块仍显示自己的界面。

模块 Vite 配置使用 `withModuleFrontend()`，Console 使用 `createModuleFrontendProxies()`；开发端口顶层页面在脚本加载前跳转，模块 HTML、资源及 HMR 均经 Console 代理。测试夹具可以显式覆盖 `--base`，入口回归使用正式路径。端口与入口的部署边界见[正式部署要求](../../../docs/spec/addp登录认证的统一要求.md#八部署与-cookie-边界)。

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

AuthStore 统一维护 Role Assignment 的到期检查：到最近的 `valid_until` 时更新候选权限并重读 AuthContext，标签页恢复前台时再次检查。角色自然到期不刷新或撤销有效的 Browser Session，也不移除其他有效角色的权限；读取失败不恢复已到期的候选权限。模块只消费 Store 的 `permissions` / `hasPermission`，不得自行维护到期计时或刷新流程。Store 销毁时应调用 Pinia `$dispose()`，共享层会同时清理到期计时与监听。

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

所有模块使用 `createAuthGuard()`。以下以 Manager 为例，传入该模块的 Router 实例与 `moduleName`，由共享守卫等待当前 AuthContext 并校验模块页面访问；具体接入可核对 [Manager 路由登记](../../../manager/frontend/src/router/index.js)：

```javascript
import { createAuthGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'

router.beforeEach(createAuthGuard(useAuthStore, {
  router,
  moduleName: 'Manager',
  loginRouteName: 'Login'
}))
```

### 3.1 会话初始化与权限状态

守卫会先执行 `initializeSession()`：

- 顶层页面优先请求同源标签页的内存 Token，没有可用 Token 时通过 Cookie 刷新；
- 初始化身份请求遇到 401 时，在全局刷新锁内强制刷新并重试一次，避免把已被服务端轮换撤销的标签页 Token 误判为有效会话；
- iframe 的父页面握手与同参重试见[Console iframe](#五console-iframe)；
- 认证失败才进入登录页；
- 网络故障标记会话初始化错误，不把用户错误地当作已退出。

已初始化会话的 Token 更新也会重新读取 AuthContext。共享守卫在页面权限判断前等待当前读取，若请求被替换则继续等待最新请求；网络故障或事实尚未取得时取消导航，不把加载状态判为权限拒绝。页面根据权限调整标签或 query 时，应在 AuthContext 存在且无读取任务后执行，并在读取完成后重新判断。不得通过保留旧候选权限解决刷新竞态。

### 3.2 登录落点与安全回跳

`createAuthGuard` 的 `homeRoute` 默认 `/`；公开路径带模块前缀的独立应用显式指定自身根路径，已认证用户访问登录页时返回该根路径。

登录完成后的目标统一使用 `resolveLoginRedirect()`，再交给各入口既有导航能力。只接受单一站内绝对路径，保留合法的 path、query 和 hash；外部地址、协议相对地址、反斜线、重复 query 形成的数组等歧义输入回到入口默认首页。登录页自身及 Vue Router 默认匹配规则下等价的大小写、末尾斜线形式均不得作为登录后目标；检查发生在 URL 路径规范化之后。Agent 模块首页 `/` 与 `/sessions/:session_id` 深链保持独立，不从会话列表推断目标。

## 四、API 请求

### 4.1 共享请求客户端

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

`createAuthInterceptor(authStoreOrGetter, moduleName)` 仅在请求配置中装配当前内存 Token，无 Token 时移除 `Authorization`；它本身不负责会话初始化、等待、刷新或重试。业务请求使用上述完整客户端，低层装配范围可核对[共享认证实现](../src/composables/useAuth.js)。

### 4.2 共享资源树会话绑定

共享资源树 API 在内部通过 `getBoundAuthStore()` 读取当前页面已绑定的 Store，并复用 `createAPIClient()`；业务页面不传 Token、不新建刷新会话。正常路由守卫的 `initializeSession()` 已完成绑定，独立挂载共享组件的宿主也必须先完成会话初始化。未绑定 Store 时请求明确失败。共享客户端与模块客户端的并发 401 按 Store 合并刷新，iframe 仍经父 Console 获取更新后的 Token。

### 4.3 请求头与原生资源

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

握手消息可能早于 Console 组件的挂载回调，因此模块会在超时窗口内重发同一个幂等请求。Console 不得依赖只接收一次 `addp-auth-ready`，也不得把重复握手视为重复登录或创建新的会话。

## 六、验证

认证实现改动至少执行以下既有门禁；共享验证与消费方组合的职责见[共享前端测试说明](../../README.md#测试)，夹具隔离见[浏览器测试隔离](../../README.md#浏览器测试隔离)：

```bash
make test-common-frontend
make test-console-frontend
make test-quality-frontend
make test-agent-frontend
```

Console 合同测试验证共享调用边界及路由语义，Agent 组件测试验证实际登录完成后使用同一解析器，不再要求私有源码中重复出现安全检查。

同时扫描运行时代码，确保没有重新引入 `localStorage.token`、query Token 或 Console iframe Token URL。构建、确定性组件／浏览器回归和真实 System／Gateway 会话验收分别记录；未运行、失败或跳过项不计为通过，完整验收范围见[认证完成检查](../../../docs/spec/addp登录认证的统一要求.md#九完成检查)与[测试分层](../../../docs/spec/addp测试与验收规范.md#二测试分层)。
