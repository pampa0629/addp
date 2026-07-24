# ADDP 登录认证的统一要求

更新日期：2026-07-24

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

Refresh Token Family 采用严格轮换。跨标签页刷新互斥仍是安全要求，不是可选优化；但服务端必须区分正常并发竞争和已经完成轮换后的历史 Token 重用：

- 请求开始时读取到当前、未使用的 Refresh Token，但进入事务并等待锁后发现它已被另一事务轮换，属于正常并发竞争；本次请求返回可重试冲突，不撤销 Family，也不记录高风险重用事件；
- 请求开始时已经读取到 `used_at` 和已建立的 replacement 链，属于历史 Refresh Token 重用；本次请求返回未授权，并在同一事务设置 `reuse_detected_at`、撤销整个 Family 及全部派生票据、写入高风险审计；
- 无效、过期、已撤销 Token 一律返回未授权，不得通过错误内容泄露 Token 是否存在或 Family 状态。

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

本地用户名密码登录由单一 Browser Login Runtime 编排：先调用 Local Account 认证，成功事实固定为 `methods=["password"]`、`assurance_level=aal1`，再调用 Context Selection Service。HTTP Handler 不得绕过该编排直接创建 Token Family，也不得根据旧 `user_type` 或 `users.tenant_id` 选择 Context。

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

`context-options` 返回 Principal 当前全部有效 Platform / Tenant Context，并显式标记当前 Context。具备有效平台角色但当前认证强度不足时，Platform 选项仍返回并标记 `requires_step_up=true`；该标记只用于前端发起增强认证，不能绕过切换 Service 的 AAL2/AAL3 校验。Tenant 选项按 Tenant Code、Tenant ID 稳定排序，Platform 固定在最前。

切换事务必须：锁定当前 Browser Family，复核目标 Context 和授权版本，撤销旧 Family 及派生 Resource Access Ticket，创建新 Family 和新票据，写入旧 / 新 Context 审计，最后返回新内存 Access Token。目标 Context 必须与当前 Context 不同；新 Family 继承原 Family 的认证事实和固定最终 `expires_at`，切换不得重新起算 30 天期限。事务失败时旧 Family 保持有效。切换成功后通过 `addp-auth-session` 广播 `context_changed`；其他标签页和 iframe 必须清空旧 Context 的业务缓存并采用新 Token。

refresh 与 context switch 同时针对同一 Family 时，按 `Principal -> 目标 Membership / Tenant（切换到 Tenant 时）-> Family -> Refresh Token -> Access Token -> Resource Ticket` 的共同相对锁序，只能一个事务完成状态转换。等待锁后发现 Access Token、Refresh Token 或 Family 已被另一事务转换的请求返回可重试冲突或统一未授权，不得创建第二个 Family、不得把正常竞争误记为 Refresh Token 重用。

CLI / OAuth Client 不能调用 Context Switch 修改既有 Family。它们需要另一个 Context 时必须重新执行用户授权，并永久绑定批准时选择的 Context。

### 6.5 主动退出

第一方浏览器主动退出必须同时提交当前内存 Access Token 和同一 Family 的 Refresh Token Cookie。System 只接受 `auth_type=first_party`、`client_id=addp-web`，且 Refresh Token 的 `issued_access_token_id` 指向所提交 Access Token 的当前有效凭据对；不得只凭 Access Token、只凭 Cookie，或跨 Family 撤销会话。

退出事务按 `Principal -> Family -> Refresh Token -> Access Token -> Resource Ticket` 加锁，原子撤销整个 Family 及全部派生 Access Token、Refresh Token、Delegated Access Token 和 Resource Access Ticket，并写入唯一领域事件 `iam.browser_session.logged_out`。审计写入失败时全部撤销回滚。

logout 与 refresh / context switch 同时针对同一 Family 时，只允许一个事务完成状态转换。先完成 logout 时，等待者统一返回未授权；先完成 refresh 时，使用旧凭据的 logout 返回未授权；先完成 context switch 时，旧凭据的 logout 返回未授权。重复 logout 也统一返回未授权，不识别已撤销 Token 形成幂等旁路，且不得重复写审计。无论服务端返回成功还是未授权，浏览器都必须清除本地内存状态和相关 Cookie，并广播退出事件。

### 6.6 HTTP 契约

`POST /login` 使用判别字段返回且只返回一种结果：

```json
{"next_action":"session_issued","session":{"access_token":"addp_at_...","token_type":"Bearer","expires_in":900}}
```

```json
{"next_action":"select_context","selection":{"selection_ticket":"addp_cst_...","expires_at":"2026-07-24T12:00:00Z","contexts":[]}}
```

`POST /auth/context-selections`、`POST /auth/context-switches` 和 `POST /refresh` 成功时统一直接返回 `session` 对象的 Access Token 结构，并同时设置新的 Refresh Token 与各 Owner Resource Access Ticket Cookie。Selection Ticket 结果不得设置任何会话 Cookie。

Context 选项统一包含 `type`、`current`、`requires_step_up`；Tenant 选项还包含十进制字符串 `tenant_id`、`tenant_membership_id`、`tenant_code`、`tenant_name`，Platform 选项不得伪造 Tenant 字段。Context Selection / Switch 请求只接受 `context_type` 和可选的十进制字符串 `tenant_membership_id`。

错误响应遵循 `{ "error": "..." }`；需要增强认证时额外返回稳定 `error_code=step_up_required` 和 HTTP 403。非法参数返回 400，无效会话或无效凭据返回 401，无权进入 Context 返回 403，并发状态冲突返回 409，其他错误返回 500，且不得暴露内部错误细节。

Refresh 缺少 Cookie 或 Runtime 明确返回未授权时清除全部会话 Cookie；409 锁竞争和 500 服务故障不得清 Cookie。Logout 无论 Runtime 成功、未授权或失败都必须清除全部会话 Cookie；只有首次成功撤销返回 204，其他结果按上述状态码返回。

### 6.7 当前用户自服务

`GET /users/me` 只返回当前全局 User/Profile：十进制字符串 `id`、`display_name`、可空 `primary_email`、可空 `locale`、`created_at`、`updated_at`，以及可空 `local_account`。`local_account` 仅包含展示用户名 `username`；纯外部 IdP User 没有 Local Account 时必须返回 `null`。该响应不得包含旧 `user_type`、单一 `tenant_id`、Principal 状态、授权版本、Role、Permission 或 Membership；当前 Context 和权限只能从 `/auth/context` 获取。

`PUT /users/me/password` 只接受 `current_password` 和 `new_password`，两者都不得为空且必须不同。基础 IAM Runtime 不继承旧 DTO 的 `min=6`，后续密码强度统一由 Authentication Policy 决定。请求必须使用当前有效第一方 Browser Access Token；Runtime 在 Principal 锁内重新校验 Token 后修改 Local Account 密码、递增授权版本、撤销全部有效 Token Family 并写 `iam.password.rotated`。

当前密码错误返回 HTTP 400 和稳定 `error_code=invalid_current_password`，不得返回会触发 Browser AuthSession 刷新的 Token 401；新旧密码相同返回 400 和 `error_code=password_unchanged`。成功返回 `changed_at` 与 `revoked_family_count`，同时清除当前浏览器全部会话 Cookie，前端清空内存 Token、广播退出并跳转登录。

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
- [ ] 多标签页同时到期时最多一个刷新成功，其余请求返回可重试冲突，且不会触发 Refresh Token 重用撤销。
- [ ] 401 只重试一次，不形成刷新循环。
- [ ] 用户退出会同步到其他标签页和 iframe。
- [ ] Access Token 只绑定 `platform` 或一个当前 Tenant，上下文切换后旧权限不会继续生效。
- [ ] 多 Context 登录在选择前不签发业务 Token，Selection Ticket 过期、重放和并发消费均失败。
- [ ] Browser Context Switch 撤销旧 Web Family，但不改变既有 CLI / OAuth Family。
- [ ] Console、一个 iframe 模块和一个独立模块完成自动化或在线验收。
