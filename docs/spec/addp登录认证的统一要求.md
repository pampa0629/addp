# ADDP 登录认证的统一要求

更新日期：2026-07-22

## 一、认证事实与唯一主路径

- System IAM 是 User、账号、外部身份、Tenant Membership、Role 和会话状态的唯一逻辑事实源。
- System 只签发随机 opaque 用户访问令牌，不签发或解析用户 JWT。
- `GET /api/v1/system/auth/context` 是用户访问令牌到权威 AuthContext 的唯一接口。
- 业务模块不解析令牌，不从 `/users/me` 推断身份，只消费 AuthContext。
- Web、CLI 和外部 Agent 的令牌模型见 `docs/spec/addp OAuth授权规范.md`。

浏览器请求的唯一认证主线为：

```text
HttpOnly Refresh Token Cookie
  -> Browser AuthSession 静默恢复或轮换
  -> 内存 User Access Token
  -> Authorization: Bearer <access_token>
  -> System AuthContext
  -> owner 模块权限校验
```

禁止保留 JWT、本地持久化 Access Token或 URL query Token 等平行路径。

每枚 User Access Token 必须绑定且只能绑定一种会话模式：

- `platform`：进入 Platform Realm，不携带当前 Tenant，也不激活 Tenant 权限；
- `tenant`：绑定一个有效 Tenant Membership 和唯一当前 Tenant，不携带平台角色权限。

切换 Tenant 或切换平台/租户模式时必须原子撤销当前 Browser Token Family 并创建绑定目标 Context 的新 Family，不能原地修改 Family，也不能在一个 AuthContext 中合并多个 Tenant 或混合平台与 Tenant 权限。浏览器切换不改变既有 CLI / OAuth Family；授权事实版本变化仍会使相关 Principal 的旧 Family 失效。

## 二、浏览器令牌存储

### 2.1 Refresh Token

Web Refresh Token 只能存放在 System 设置的 HttpOnly Cookie 中：

- JavaScript 不可读取；
- `SameSite=Lax`；
- Path 为 `/api/v1/system`；
- 生产环境 `Secure=true`；
- 每次刷新都轮换；
- 重用旧 Refresh Token 时撤销整个 Refresh Token Family。

### 2.2 Access Token

Access Token 只能保存在当前 JavaScript 运行时内存中：

- 不写入 `localStorage`、`sessionStorage`、IndexedDB 或持久化 Pinia 插件；
- 不进入 iframe URL、下载 URL、浏览器历史、Referrer 或日志；
- 页面刷新或新开标签页后，通过 Browser AuthSession 静默恢复；
- 用户资料可以按现有规则缓存，但缓存资料不代表已认证状态。

### 2.3 Browser Resource Access Ticket

无法设置 Authorization Header 的原生图片、媒体、下载和三维资源请求统一使用 Browser Resource Access Ticket：

- System 在第一方 Web 登录和 Refresh Token 轮换事务中签发；
- 使用 `addp_rat_` 前缀的随机 opaque 值，数据库只保存 SHA-256 Hash；
- 与当前 Refresh Token Family 关联，退出、Family 撤销或旧 Refresh Token 重用时同步撤销；
- 有效期不超过当前 User Access Token，默认 15 分钟；
- 通过 `addp_resource_access_ticket` HttpOnly Cookie 传输，不返回给 JavaScript；
- 每个 Owner 使用独立 Path，例如 Manager 为 `/api/v1/manager`、Standard 为 `/api/v1/standard`；
- Owner 只允许明确声明的 GET/HEAD 资源路由消费，普通业务 API 不接受该票据；
- 不进入 URL、浏览器历史、Referrer、日志或前端持久化存储。

## 三、Browser AuthSession

`common-frontend` 提供唯一 Browser AuthSession 实现。Console 和所有模块前端必须通过共享能力完成：

1. 应用启动时恢复现有 Cookie 会话；
2. 将 Access Token 仅保存在内存；
3. 根据 `expires_in` 在到期前主动刷新；
4. 401 时只执行一次兜底刷新和请求重试；
5. 多个并发请求共享同一个刷新 Promise；
6. 同 origin 多标签页只允许一个刷新请求；
7. 广播 Token 更新、会话失效和退出事件；
8. Refresh 失败时区分无会话、网络故障和服务不可用，不因瞬时网络错误直接清除会话。

顶层页面通过 HttpOnly Cookie 调用：

```text
POST /api/v1/system/refresh
credentials: include
```

生产环境必须经当前 origin 的 Gateway 路径访问，不直接拼接 System `8180` 端口。

## 四、多标签页协调

Refresh Token Family 采用严格轮换。两个标签页同时提交同一旧 Refresh Token 会触发重用检测，因此跨标签页刷新互斥是安全要求，不是可选优化。

同 origin 页面统一使用：

- Web Locks API：锁名固定为 `addp-auth-refresh`，保证单一刷新者；
- Context Switch 使用独立 Web Lock `addp-auth-context-switch`，并与 refresh 遵守统一锁顺序；
- BroadcastChannel：频道固定为 `addp-auth-session`，广播 Access Token、过期时间、退出和失效事件；
- 不支持 Web Locks 时，在 BroadcastChannel 上使用带租约和超时的刷新主节点协议。

收到其他标签页的新 Access Token 后，本标签页只更新内存状态，不再次调用 Refresh API。

用户主动退出时，当前页面调用 System `/logout`，随后广播退出事件。其他标签页必须立即清空内存状态并进入登录页。

## 五、Console 与 iframe

Console 是 iframe 模式下唯一的浏览器会话协调者。

```text
Console Browser AuthSession
  -> iframe 加载，不携带 Token
  -> iframe 发送 addp-auth-ready
  -> Console 校验 event.origin 和 event.source
  -> Console 发送 addp-auth-token
  -> iframe 只在内存中保存 Token
```

约束：

- 开发环境和生产环境使用同一套 `postMessage` 协议；
- Console 根据模块配置生成允许的 origin，不使用 `*`；
- iframe 必须验证父窗口 origin 和消息来源；
- Console 刷新 Access Token 后向当前受信任 iframe 推送更新；
- iframe 不把父级 Token 写入任何持久存储；
- iframe 中的模块刷新页面后重新发起握手；
- iframe 等待认证期间保持初始化状态，不跳转到模块登录页；
- 模块作为顶层页面独立运行时，由自身 Browser AuthSession 通过 Cookie 恢复会话。

旧的 `?token=` iframe 参数和路由守卫 query Token 解析必须删除。

## 六、登录、恢复和退出体验

### 6.1 登录

1. 用户提交用户名和密码到 System `/login`；
2. System 验证 User、Local Account、认证策略和必要 MFA；
3. System 计算可进入的 Platform Realm 和有效 Tenant Membership；
4. 没有可用 Context 时拒绝登录；只有一个选项时可以直接创建绑定该 Context 的 Browser Token Family；
5. 多于一个选项时返回 5 分钟、单次消费的 `addp_cst_` Context Selection Ticket，不签发业务 Access Token 或 Refresh Token；
6. 浏览器只在内存保存 Ticket，并调用 `POST /api/v1/system/auth/context-selections` 提交 Platform 判别值或 Tenant Membership ID；
7. System 重新校验 Principal、认证强度和目标 Context，在单个事务中消费 Ticket 并创建 Browser Token Family；
8. System 返回短期 Access Token 并设置 Refresh Cookie，Browser AuthSession 只把 Access Token 保存到内存；
9. 前端读取 `/users/me` 作为当前用户资料，Console 进入所选 Context 中的原计划页面。

Context Selection Ticket 只保存 Hash，不能进入 Cookie、URL、浏览器历史、持久化存储或日志。客户端不得提交或决定 Principal、Tenant、Role、Permission、授权版本。平台选项不能静默转换为任一 Tenant，Tenant 选项也不能自动激活平台权限。

### 6.2 启动恢复

页面启动时先进入 `initializing` 状态：

- 内存中已有 Token：直接验证或加载用户资料；
- 顶层页面无 Token：通过 Refresh Cookie 静默恢复；
- iframe 无 Token：等待父 Console 握手；
- 无有效 Cookie：进入匿名状态并跳转登录；
- 临时网络故障：显示可重试的加载或错误状态，不伪装成退出登录。

### 6.3 静默刷新

- 正常路径在 Access Token 到期前刷新；
- 401 只作为兜底；
- 刷新成功后自动重试原请求；
- 整个过程不打断用户当前操作；
- Refresh Token Family 默认固定 30 天，达到最终有效期后必须重新登录。

### 6.4 上下文切换

第一方浏览器使用唯一目标端点：

```text
GET  /api/v1/system/auth/context-options
POST /api/v1/system/auth/context-switches
```

两个端点都要求当前第一方 Web Access Token；切换还要求同一 Family 的 Refresh Token Cookie。进入 Platform Context 时 User 认证强度至少为 AAL2，否则先返回 `step_up_required`。

切换事务必须：锁定当前 Browser Family，复核目标 Context 和授权版本，撤销旧 Family 及派生 Resource Access Ticket，创建新 Family 和新票据，写入旧 / 新 Context 审计，最后返回新内存 Access Token。事务失败时旧 Family 保持有效。切换成功后通过 `addp-auth-session` 广播 `context_changed`；其他标签页和 iframe 必须清空旧 Context 的业务缓存并采用新 Token。

CLI / OAuth Client 不能调用 Context Switch 修改既有 Family。它们需要另一个 Context 时必须重新执行用户授权，并永久绑定批准时选择的 Context。

## 七、前端共享能力要求

所有模块必须使用：

- `createAuthStore()`：内存 Token 和认证状态；
- `createAuthGuard()`：等待 Browser AuthSession 初始化后再决定放行或跳转；
- `createAPIClient()`：动态注入内存 Token、刷新和单次重试；
- `createAuthenticatedFetch()`：SSE、流式请求和无法使用 Axios 的场景；
- 统一运行时 Token Provider：预览、地图、下载等特殊调用读取当前内存 Token。
- 原生资源 URL 直接使用无凭据 URL，由浏览器自动携带 Owner Path 限定的 HttpOnly Resource Access Ticket Cookie。

模块不得：

- 直接读写 `localStorage.token`；
- 自建刷新 Promise、刷新定时器或多标签页协议；
- 在 URL 中拼接用户 Access Token；
- 在 URL 中拼接 Browser Resource Access Ticket；
- 在 iframe 中自行调用 Refresh API，与父 Console 争用 Refresh Token；
- 在生产环境直连 `localhost:8180` 或 `{hostname}:8180`。

## 八、部署与 Cookie 边界

- 生产环境推荐统一 origin，通过 Nginx/Gateway 访问 Console、模块前端和 `/api`。
- 开发环境各前端端口不同，但 hostname 相同；System CORS 必须允许已分配的开发 origin，并允许 credentials。
- Cookie 不按端口隔离，但按 hostname 隔离；`localhost` 与 `127.0.0.1` 不共享会话。
- 不同域名、浏览器 Profile、设备和无痕会话默认独立登录。
- 不通过扩大 Cookie Domain 来模拟跨部署 SSO；需要跨域 SSO 时另行设计正式身份协议。

## 九、完成检查

- [ ] Access Token 未写入任何浏览器持久化存储。
- [ ] Console iframe URL 和新窗口 URL 不包含用户 Access Token。
- [ ] 图片、媒体、下载和三维资源 URL 不包含 User Access Token 或 Resource Access Ticket。
- [ ] Resource Access Ticket 只在 Owner 明确允许的 GET/HEAD 资源路由生效。
- [ ] iframe 使用受信任 origin 的认证握手。
- [ ] 页面刷新、新标签页和浏览器重启可在有效 Family 内静默恢复。
- [ ] 多标签页同时到期不会触发 Refresh Token 重用撤销。
- [ ] 401 只重试一次，不形成刷新循环。
- [ ] 用户退出会同步到其他标签页和 iframe。
- [ ] Access Token 只绑定 `platform` 或一个当前 Tenant，上下文切换后旧权限不会继续生效。
- [ ] 多 Context 登录在选择前不签发业务 Token，Selection Ticket 过期、重放和并发消费均失败。
- [ ] Browser Context Switch 撤销旧 Web Family，但不改变既有 CLI / OAuth Family。
- [ ] Console、一个 iframe 模块和一个独立模块完成自动化或在线验收。
