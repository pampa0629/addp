# ADDP 登录认证的统一要求

更新日期：2026-08-27

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

内部服务请求的唯一认证主线为：

```text
独立 Confidential OAuth Client
  -> POST /api/v1/system/oauth/token
     (client_credentials + tenant_id | context_type=platform)
  -> System 校验 OAuth Client、Service Principal 与目标 Context
  -> 短期 Service Access Token
  -> Authorization: Bearer <service_access_token>
  -> System AuthContext
  -> owner 模块精确 Permission Guard
```

Service Access Token 有效期最多 5 分钟且不可刷新。每个模块独立持有 Client Secret，
System 只保存 BCrypt Hash；Secret 轮换使旧 Client Credential 立即失效，Service Principal
的 Membership、Role、Tenant 或 `authorization_version` 变化使已签发 Token 立即失效。
owner 路由不得接受共享 Internal API Key、`X-Tenant-ID` 或 User Token 代传来构造服务身份。

### 1.1 执行 audience 与机器身份命名

Execution Audience、OAuth Client ID 和 Service Principal 是三个独立概念，不得因为当前存在一对一映射就混用名称：

- Execution Audience 是执行授权协议中的逻辑消费方标识，固定使用模块或 Runtime 稳定标识，不加 `addp-` 前缀，例如 `model`、`quality`、`develop`、`transfer`、`service`、`duckdb`。
- 机器身份使用的 Confidential OAuth Client ID 和 Service Principal 名称固定使用 `addp-<module_or_runtime>`，例如 `addp-model`、`addp-quality`、`addp-develop`、`addp-service`、`addp-duckdb`。
- System 是 audience 到唯一 OAuth Client ID 的映射事实 owner。消费 Execution Authorization 时必须同时校验 audience 和当前 Service Principal 所属 OAuth Client，不能仅比较字符串是否相等。
- audience 不接受 OAuth Client ID、服务进程名、前端包名或容器名作为别名。积极开发阶段修正命名时直接切换唯一标识，不保留旧 audience 兼容值。

当前标准映射为：

| Execution Audience | OAuth Client ID / Service Principal |
| --- | --- |
| `model` | `addp-model` |
| `quality` | `addp-quality` |
| `develop` | `addp-develop` |
| `transfer` | `addp-transfer` |
| `service` | `addp-service` |
| `duckdb` | `addp-duckdb` |

Tenant Runtime 必须提交正整数 `tenant_id`，并绑定该 Service Principal 的有效 Membership。
平台控制面必须显式提交 `context_type=platform`，只允许平台所有 Service Principal 的专用
Platform Service Role；平台三员 Role 仍只允许实名 User。两种 Context 参数互斥，不能用
空 Tenant、默认 Tenant 或任意 Tenant Membership 模拟平台控制面权限。

每枚 User Access Token 必须绑定且只能绑定一种会话模式：

- `platform`：进入 Platform Realm，不携带当前 Tenant，也不激活 Tenant 权限；
- `tenant`：绑定一个有效 Tenant Membership 和唯一当前 Tenant，不携带平台角色权限。

切换 Tenant 或切换平台/租户模式时必须原子撤销当前 Browser Token Family 并创建绑定目标 Context 的新 Family，不能原地修改 Family，也不能在一个 AuthContext 中合并多个 Tenant 或混合平台与 Tenant 权限。浏览器切换不改变既有 CLI / OAuth Family。

授权事实变化必须递增 Principal `authorization_version`，旧 Access Token、Delegated Access Token 和 Browser Resource Access Ticket 因版本不匹配立即失效；仍有效且未撤销的 Refresh Token Family 可以在下一次 Refresh Token 轮换事务中推进到 Principal 当前授权版本，并按新事实重新签发派生凭据。Principal、凭据、Tenant Membership 或 Tenant 失效，以及 Refresh Token 重用等身份或会话安全事件仍撤销受影响 Family，必须重新登录。不得为自我授权或某个前端建立专用换票接口。

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
- 每个 Owner 使用独立 Path，例如 Develop 为 `/api/v1/develop`、Manager 为 `/api/v1/manager`、Standard 为 `/api/v1/standard`；Develop 仅将该票据用于导出会话文件等浏览器原生 GET/HEAD 资源，不得用于普通 JSON API；
- Owner 只允许明确声明的 GET/HEAD 资源路由消费，普通业务 API 不接受该票据；
- 不进入 URL、浏览器历史、Referrer、日志或前端持久化存储。

### 2.4 认证凭据日志边界

任何访问日志、应用日志、错误日志和审计详情都不得记录认证请求体、认证 Header、Cookie 或凭据明文。禁止记录的值至少包括密码、TOTP/恢复码、MFA Challenge、Context Selection Ticket、OAuth Authorization Code、PKCE Verifier、Client Secret、API Key、User Access Token、Refresh Token、Resource Access Ticket 和 Delegated Access Token。

Gateway AccessLog 只服务于外部 API Key 请求的性能、缓存和限流统计，不记录 Browser Bearer、Cookie 或公开认证请求。Gateway 不采集任何请求体；Query 参数进入 AccessLog 前必须按敏感字段策略脱敏。System AuditLog 记录认证和授权领域事件，但只允许保存稳定事件事实、实体标识、结果和非敏感原因码，不得保存上述凭据。

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

页面启动时可以优先复用其他标签页广播的内存 Access Token，但本地过期时间不能证明该 Token
仍被服务端接受。初始化阶段读取 `/users/me` 或 `/auth/context` 返回 401 时，Browser AuthSession
必须在全局刷新锁内强制刷新并重试初始化请求一次；只有 Refresh 本身返回认证失败，或重试后的
初始化请求仍返回 401，才能把会话判定为失效并进入登录页。

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
- iframe 在认证超时窗口内使用同一个 `requestId` 重发握手消息，收到 Token、Logout 或 Error 后立即停止；Console 必须按幂等请求处理重复消息；
- iframe 等待认证期间保持初始化状态，不跳转到模块登录页；
- 模块作为顶层页面独立运行时，由自身 Browser AuthSession 通过 Cookie 恢复会话。

独立顶层产品界面的正式入口必须与 Console 保持同 origin。当前包括 Portal 的 `/portal/...` 和
Workbench Data Application 的 `/data-apps/:application_id`：生产环境由 Nginx 提供，开发环境由
Console Vite 将同一路径反向代理到对应 owner 前端。Console 只能打开当前 origin 下的正式路径，
不得打开 owner 前端开发端口形成第二个顶层认证 origin。这样所有顶层入口继续使用同一个 Web Lock、
BroadcastChannel 和 Refresh Token Family 协调域，不需要在新窗口 URL、`window.opener` 或持久化
存储中传递 Access Token。

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
- 因 401 发起的强制刷新不得复用其他标签页尚未过期的旧 Access Token，必须在全局刷新锁内使用 Refresh Cookie 复核服务端会话；
- 刷新成功后自动重试原请求；
- Family 授权版本落后于 Principal 当前版本时，刷新事务必须先复核 Principal、当前 Context、Tenant 和 Membership 仍有效，再把 Family 推进到当前版本并轮换全部派生凭据；
- 授权版本推进不改变 Family 的 Principal、Context、Client、认证事实和最终 `expires_at`，并写入审计；
- Principal、凭据、Tenant、Membership 或 Family 已失效时不得推进版本，统一按会话失效处理；
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

### 4.4 普通 User 的 TOTP 登记与会话内 step-up

普通 User 通过唯一的当前用户安全设置接口自助登记 TOTP。开始登记必须同时提交当前本地账号密码；System 在一个事务内锁定 Principal、Local Account 和当前 Browser Token Family，重新验证密码，确认不存在激活的 TOTP Credential，然后创建 5 分钟有效的一次性 Enrollment。Enrollment 绑定 Principal、当前授权版本和源 Token Family，Secret 只在开始响应中以 Base32 和 `otpauth://` URI 返回一次，数据库只保存加密值，日志与审计不得记录 Secret、URI、验证码或 Enrollment Token。

完成登记必须提交 Enrollment Token、TOTP 验证码，并同时携带当前 Access Token 与同一 Family 的 Refresh Token Cookie。System 在一个事务内按 Principal -> Family -> Refresh Token -> Access Token -> Enrollment 的顺序加锁，验证 TOTP 防重放 counter，创建唯一激活 Credential，消费 Enrollment，以固定最终期限创建 `password + totp / aal2` 替换 Family，并撤销源 Family。任一步失败必须整体回滚；验证码连续失败 5 次、过期、授权版本变化、Family 变化或并发消费后均不可重放。

已有激活 TOTP Credential 的 User 可在已登录 Browser Session 内发起 step-up。Challenge 固定 5 分钟有效并绑定 `purpose=step_up`、Principal、当前授权版本和源 Token Family；登录前 Challenge 使用 `purpose=login` 且不绑定 Family。完成 step-up 同样要求当前 Access Token、同一 Family 的 Refresh Token Cookie与 TOTP 验证码，并原子创建 AAL2 替换 Family、撤销源 Family。不得原地修改 Token Family 的认证事实，也不得仅凭旧 Access Token 获得 AAL2。

所有已登录 MFA 写接口必须使用现有按用户限流。单个 Challenge/Enrollment 连续失败 5 次即终止；step-up 达到 5 次失败时还必须在同一事务撤销源 Token Family，要求重新登录。验证码无效、过期、已消费或会话绑定变化统一返回 HTTP 401、稳定 `invalid_mfa_verification` 和不泄露内部状态的用户提示。

step-up 成功响应复用唯一 Browser Access Token 响应结构，服务端同时替换 HttpOnly Refresh Cookie。前端必须原地接收新 Access Token、刷新 AuthContext 并继续被中断的操作，不得要求用户退出后重新登录。AAL2 的 `step_up_expires_at` 不得晚于替换 Family 的固定最终期限；过期后需要再次 step-up，但不主动注销仍可用于低风险操作的 Tenant Session。

### 4.5 普通 User 的受控 TOTP 重置

普通 User 遗失认证器或 TOTP Secret 且无法完成登录前 Challenge 时，由 Platform Context 中持有 `iam.mfa_credential.reset` 的平台安全管理员执行唯一受控重置。请求必须提供非空原因，目标必须是有效 User、具有 active 或 locked 的 Local Account、具有唯一 active TOTP Credential，且不能持有任何当前有效 Platform Role。平台三员不得使用该接口，继续只允许本人维护凭据或通过离线三员整体恢复处理灾难场景。

重置必须在一个事务内按 Principal -> Local Account -> MFA Credential 的顺序加锁，废止旧 Credential、推进 Principal `authorization_version`、撤销全部有效 Token Family、消费未完成的 MFA Challenge、MFA Enrollment 和 Context Selection Ticket，并写入 `iam.mfa.credential_reset` 高风险审计。审计可以记录目标 Principal、原因、授权版本和受影响事实数量，但不得记录旧/新 TOTP Secret、验证码、密文或 nonce。API 只返回重置时间、授权版本和受影响数量，不签发会话，也不修改密码、Tenant Membership 或 Role Assignment。

重置后目标 User 使用现有密码重新登录；由于不再存在 active TOTP Credential，登录只建立 AAL1 认证事实。目标 User 必须立即通过“安全设置”中的现有自助登记路径扫描并验证新的 TOTP，完成后由现有登记事务替换为 `password + totp / aal2` Token Family。不得为重置建立第二套登记端点、临时默认 Secret、数据库直接更新或旧密钥 fallback。

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

### 6.8 租户邀请接受

System 创建 Tenant Invitation 时返回的唯一浏览器入口固定为 Console 同 origin 下的
`/invitations/accept?invitation=<opaque-secret>`。Console 拥有该公开页面和会话接管，System
仍是 Invitation、User、Membership 和新会话的唯一事实 owner。不得把邀请链接实现为
需要先登录的模块 iframe 路由，也不得在 Console 复制 Invitation 或 Membership 业务事实。

接受流程只允许以下两条 System 正式 API 路径：

- 当前浏览器没有有效会话时，用户填写本地账号注册资料，Console 调用
  `POST /api/v1/system/tenant/invitations/registrations`；
- 当前浏览器已有有效 User Session 时，Console 明确展示当前账号并调用
  `POST /api/v1/system/tenant/invitations/acceptances`。用户选择其他账号时必须先通过唯一 Logout
  流程结束当前 Family，再带原邀请路径进入登录页。

两个接口成功时都直接返回新 Tenant Context 的 Browser Session 并由 System 设置 HttpOnly
Refresh Cookie。Console 必须把 Access Token 交给共享 Browser AuthSession 的内存 Token Provider，
随后重读 `/users/me` 和 `/auth/context`；不得把 Token、邀请 Secret 或密码写入
`localStorage`、`sessionStorage`、日志或自建 Cookie。邀请缺失或格式无效时页面
必须 fail-closed，不得回退到普通登录或默认 Tenant。

## 七、前端共享能力要求

所有模块必须使用：

- `createAuthStore()`：内存 Token 和认证状态；
- `createAuthGuard()`：等待 Browser AuthSession 初始化后再决定放行或跳转；
- `createAPIClient()`：动态注入内存 Token、刷新和单次重试；
- `createAuthenticatedFetch()`：SSE、流式请求和无法使用 Axios 的场景；
- 统一运行时 Token Provider：预览、地图、下载等特殊调用读取当前内存 Token。
- 原生资源 URL 直接使用无凭据 URL，由浏览器自动携带 Owner Path 限定的 HttpOnly Resource Access Ticket Cookie。

交互式 Notebook 等需要原生多方法 HTTP 与 WebSocket 协议的短期工具会话，不得扩大 Browser Resource Access Ticket 的只读语义。唯一允许的主线为：浏览器先以 Access Token 调用 owner 的会话创建 API；owner 完成 Permission、Tenant、User、资源归属和 Runtime 能力校验后，签发只绑定单个会话路径的 Browser Session Capability Cookie。该 Cookie 必须是 opaque、HttpOnly、SameSite=Strict、短 TTL，服务端只保存 Hash；不得进入 URL、前端存储或日志。owner 代理必须在每个 HTTP 请求和 WebSocket 握手时重新校验会话状态，并在关闭、过期、Context 切换、登出或 owner 重启后 fail-closed。它不能访问 owner 的其他 API，也不能作为 Access Token、Refresh Token 或 Browser Resource Access Ticket 的兼容替代。

Notebook Kernel 需要发现当前可查询 Engine、实时 Engine Catalog 或读取数据时，只允许由 Develop 在创建同一 Notebook Interactive Session 时签发独立的 Notebook Kernel Capability Token，并通过标准 Script Runtime 会话请求注入隔离 Kernel process。该 Token 必须是 opaque、短 TTL，只绑定一个 Session、Tenant、User 和 Task，Develop 只保存 Hash；它只能调用该 Session 的脱敏 Engine Runtime Descriptor、Engine Catalog 与受控只读数据代理接口，响应不得包含 `connection_info`。Token 不得进入 Notebook 内容、浏览器、公开会话响应、URL 或日志，不得作为 User Access Token、Service Access Token、Execution Authorization、Notebook Session Authorization 或 Browser Session Capability Cookie 的兼容替代；会话关闭、过期或 Develop 重启后必须 fail-closed。创建会话时必须同时校验 Notebook 更新权限和 `system.engine.read`。

Notebook Session Authorization 是 System 保存的用户派生短期授权事实，不是新的 AuthContext Token 类型。唯一签发和消费流程为：

1. Develop 在创建 Session 的同步请求栈内，把已经过自身 AuthContext 中间件验证的 User Bearer 转发给 `POST /api/v1/system/auth/notebook-session-authorizations`；请求只提交 Develop 已生成的 `session_id`、已验证的 `task_id` 和 `expires_in`，不得提交 User、Tenant、Membership、Permission、audience 或 Engine 列表。
2. System 从 User AuthContext 与源 Token Family 派生 Principal、Tenant Membership、`authorization_version` 和 family 绑定，固定 audience 为 `develop`、允许操作为 `catalog.list_children` 与 `execution_engine_access.derive`，并把有效期限制为不晚于 Notebook Session 到期时间和平台上限。响应只返回不可猜测的 authorization ID 与 `expires_at`，不返回 Bearer Token。
3. Develop 只在内存 Session 中保存 authorization ID。Kernel 使用自己的 Notebook Kernel Capability 调用 `POST /api/v1/develop/notebook-kernel-sessions/{session_id}/catalog/children`；Develop 解析 Session 后，以 `addp-develop` Tenant Service Access Token 和 authorization ID 调用 `POST /api/v1/system/notebook-session-authorizations/{id}/catalog/children`。请求必须同时携带绑定的 `session_id`、目标 `engine_id` 和完整 `EngineCatalogListChildrenRequest`。路由中的 `catalog` 由 Notebook Engine 上下文限定，不表示企业 Catalog。
4. System 在同一次请求内校验固定 OAuth Client、Service Principal Permission、Tenant、Session、Principal、Membership、Token Family、授权版本、当前 `system.engine.read`、Engine 归属与状态，再调用唯一 `EngineCatalogProvider.ListChildren`。不得先返回通用“授权通过”结果让 Develop 使用自身身份调用另一条 Engine Catalog 路径。
5. 每次查询或扫描由 Develop 生成新的 execution ID，并以固定 `read` 效果调用 `POST /api/v1/system/notebook-session-authorizations/{id}/execution-engine-accesses`。System 在同一事务内重新校验 Session 来源、当前读取权限和目标 Engine，派生独立 Execution Authorization，并以 `source_notebook_session_authorization_id` 保存其唯一来源；同一响应返回执行授权事实和执行期 Engine Access。Kernel 不接收连接信息。
6. Develop 显式关闭 Session 时，以 Service Access Token 调用 `POST /api/v1/system/notebook-session-authorizations/{id}/revocations` 并提交绑定的 `session_id`；重复撤销必须幂等。System 必须联动撤销该 Session 已派生且仍有效的 Execution Authorization。无论 System 撤销调用是否成功，Develop 都必须先使本地 Kernel Capability、活动查询和 Session 失效；残留授权最晚在原 `expires_at` 失效，不能延长或刷新。

Notebook Session Authorization 不冻结 Engine ID 列表。每次消费都按当前 Tenant、Permission、资源规则、Engine 生命周期和 capabilities 实时判断，因此 Session 存续期间新注册且当前用户可见的 Engine 可以出现，停用、删除或撤权的 Engine 必须立即不可访问。普通 Access Token / Refresh Token 轮换不撤销该授权；退出、Token Family 撤销或重用、Principal/Membership 失效、`authorization_version` 变化、显式 Session 关闭、到期和 Develop 本地 Session 丢失都必须 fail-closed。活动查询的标准 Engine Access 租约复核必须沿 Execution Authorization 的来源关联重新校验 Session 与 Token Family。它不能直接用于数据预览、查询或连接获取；数据读取必须先派生独立 Execution Authorization。

接口契约固定如下，不增加兼容字段或第二组路径：

```http
POST /api/v1/system/auth/notebook-session-authorizations
Authorization: Bearer <current-user-access-token>
Content-Type: application/json

{
  "session_id": "f9233f3f-81cf-4532-b4e4-441281a790ce",
  "task_id": 42,
  "expires_in": 3600
}
```

成功返回 HTTP 201：

```json
{
  "id": "869e5cf8-cd09-40ad-9307-20841381ad51",
  "session_id": "f9233f3f-81cf-4532-b4e4-441281a790ce",
  "task_id": 42,
  "expires_at": "2026-08-03T12:00:00Z"
}
```

签发路由使用 User AuthContext 和 `system.engine.read`；`expires_in` 必须为正数，System 负责收窄，客户端不能提交绝对到期时间。

Kernel 调用 Develop：

```http
POST /api/v1/develop/notebook-kernel-sessions/{session_id}/catalog/children
Authorization: Bearer <addp_nkc_...>
Content-Type: application/json

{
  "engine_id": 2,
  "path": {
    "version": "catalog.path/v1",
    "engine_id": 2,
    "segments": []
  },
  "options": {
    "recursive": false,
    "limit": 100,
    "offset": 0
  }
}
```

Develop 调用 System 时使用唯一消费路由，并在原请求之外补充绑定的 `session_id`；System 响应复用 `EngineCatalogListChildrenResponse`：

```http
POST /api/v1/system/notebook-session-authorizations/{id}/catalog/children
Authorization: Bearer <addp-develop-service-access-token>
Content-Type: application/json

{
  "session_id": "f9233f3f-81cf-4532-b4e4-441281a790ce",
  "engine_id": 2,
  "path": {
    "version": "catalog.path/v1",
    "engine_id": 2,
    "segments": []
  },
  "options": {
    "recursive": false,
    "limit": 100,
    "offset": 0
  }
}
```

`engine_id` 必须与非空 `path.engine_id` 一致；System 返回的每个 `EngineCatalogEntry.Path` 必须保留规范 engine ID 和完整 segments。消费与撤销路由只接受 `addp-develop` Tenant Service Access Token、固定 Client Guard 和 `system.notebook_session_authorization.execute`，该 Runtime Role 不包含 `system.engine.read`。

单次查询或扫描的原子执行访问接口固定为：

```http
POST /api/v1/system/notebook-session-authorizations/{id}/execution-engine-accesses
Authorization: Bearer <addp-develop-service-access-token>
Content-Type: application/json

{
  "session_id": "f9233f3f-81cf-4532-b4e4-441281a790ce",
  "execution_id": "741e64f3-8303-4d93-9c0f-9ce1bfefb35e",
  "engine_id": 2,
  "expires_in": 300
}
```

请求不能提交 audience、effects、Principal、Membership 或授权版本；System 固定 `audience=develop`、`effects=["read"]`。成功响应包含 Execution Authorization ID、execution ID、到期时间与仅供 Develop 受控 Runtime 使用的 Engine Access。每次查询或扫描必须使用新的 execution ID；冲突返回 409，不复用旧授权。

```http
POST /api/v1/system/notebook-session-authorizations/{id}/revocations
Authorization: Bearer <addp-develop-service-access-token>
Content-Type: application/json

{
  "session_id": "f9233f3f-81cf-4532-b4e4-441281a790ce"
}
```

首次和重复撤销都返回 HTTP 204。调用方不得通过错误差异判断其他 Tenant 或其他 Session 的授权 ID 是否存在。

System 对不存在、跨 Tenant、Session 不匹配、已撤销、已到期或用户授权事实失效的 authorization ID 统一返回 403 与稳定内部错误码 `notebook_session_authorization_forbidden`，不能用 404 泄露授权记录。Develop 将该错误映射为面向 Kernel 的 403 `notebook_engine_catalog_forbidden`；System Service Access Token 无效、固定 Client Guard/Runtime Permission 配置错误、响应结构无效或 System 不可达统一映射为 502 `engine_catalog_control_plane_failed`，不能伪装成用户无权限。Engine、EngineCatalogPath 和 Provider 错误继续按引擎体系中的稳定错误表映射。

Console 与 iframe 之间的 `addp-auth/v1` 协议必须保留刷新结果语义：

- Refresh 因会话无效返回 401 时，Console 发送带 `error_code=authentication_required` 的 `addp-auth-logout`；iframe 必须将其还原为认证失败，清理本地会话并由顶层 Console 进入带原路径 `redirect` 的登录页；
- Refresh 因网络、锁竞争或服务故障失败时，Console 发送 `addp-auth-error`，iframe 保留当前会话并进入可重试错误状态；不得发送 `addp-auth-logout` 或伪装成退出。

模块不得：

- 直接读写 `localStorage.token`；
- 自建刷新 Promise、刷新定时器或多标签页协议；
- 在 URL 中拼接用户 Access Token；
- 在 URL 中拼接 Browser Resource Access Ticket；
- 在 iframe 中自行调用 Refresh API，与父 Console 争用 Refresh Token；
- 从 Console 打开 Portal 或 Workbench 前端开发端口，形成无法协调的第二个顶层认证 origin；
- 在生产环境直连 `localhost:8180` 或 `{hostname}:8180`。

## 八、部署与 Cookie 边界

- 生产环境推荐统一 origin，通过 Nginx/Gateway 访问 Console、模块前端和 `/api`。
- 开发环境普通 iframe 模块使用各自端口；Portal `/portal/...` 与 Workbench Data Application `/data-apps/...` 正式入口仍由 Console origin 代理提供，避免一个 Browser Family 被多个顶层 origin 同时轮换。
- System CORS 必须允许已分配的开发 origin，并允许 credentials，供模块独立调试使用。
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
- [ ] Role Assignment 或 Role Permission 变化后旧 Access Token 立即失效，有效 Family 可通过唯一 Refresh 路径推进授权版本并静默恢复。
- [ ] Principal、密码/MFA、Tenant Membership、Tenant 或 Refresh Token 重用等安全事件仍撤销 Family，不能通过授权版本推进恢复。
- [ ] 用户退出会同步到其他标签页和 iframe。
- [ ] 会话撤销后顶层 Console 进入带原路径 `redirect` 的登录页，iframe 不停留空白页。
- [ ] iframe Refresh 的非认证故障不清理会话，不发送 `addp-auth-logout`。
- [ ] Access Token 只绑定 `platform` 或一个当前 Tenant，上下文切换后旧权限不会继续生效。
- [ ] 多 Context 登录在选择前不签发业务 Token，Selection Ticket 过期、重放和并发消费均失败。
- [ ] Browser Context Switch 撤销旧 Web Family，但不改变既有 CLI / OAuth Family。
- [ ] 登录、MFA、Context Selection、OAuth、Token 和 Secret 请求体未进入 Gateway AccessLog、System AuditLog 或应用日志。
- [ ] Console、一个 iframe 模块和一个独立模块完成自动化或在线验收。
