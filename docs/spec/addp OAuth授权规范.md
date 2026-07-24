# ADDP OAuth 授权规范

更新日期：2026-07-23

状态：阶段 4.3 正式规范。OAuth 登录、浏览器会话、资源票据和受委托访问令牌均以本文为准；OAuth/OIDC 协议引擎目标路线已确认，运行代码尚待一次性切换。

## 一、统一令牌模型

ADDP 的 Web、CLI 和 OAuth 客户端统一使用随机 opaque 用户令牌。System 是 ADDP 唯一的身份、会话、OAuth Client 和 AuthContext 权威，不拆分第二个 ADDP Auth Server；业务模块不解析令牌，只调用 System AuthContext。接入企业 IdP 时，由 System 将 External Identity 映射为 ADDP User 和 Tenant Membership，并为所选 Platform Realm 或唯一当前 Tenant 建立 opaque 会话；外部 IdP Token 不直接进入业务模块。

- Access Token：`addp_at_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 15 分钟。
- Refresh Token：`addp_rt_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 30 天。
- Authorization Code：`addp_ac_` 前缀，单次使用，默认有效期 5 分钟。
- Authorization Request Secret：`addp_ars_` 前缀，只在 CLI 内存中保存，System 只保存 SHA-256 Hash，随 5 分钟 Authorization Request 一起失效。
- Device Code：`addp_dc_` 前缀，只保存 Hash，默认有效期 10 分钟。
- User Code：8 位易输入字符，服务端只保存规范化值的 Hash。
- Delegated Access Token：`addp_dat_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 2 分钟且不得超过源 Access Token 剩余有效期。
- Context Selection Ticket：`addp_cst_` 前缀，随机部分不少于 32 字节，只保存 SHA-256 Hash，默认有效期 5 分钟且只能成功消费一次；它是第一方登录临时凭据，不是 OAuth Token。

System 不再签发或解析用户 JWT；旧的“允许过期 Access Token 调 `/refresh`”路径删除。

每枚 User Access Token 必须绑定 `platform` 会话模式或一个有效 Tenant Membership 对应的 `tenant` 会话模式。OAuth Client 不得通过 authorize、token 或 delegation 请求提交任意 `tenant_id` 或平台角色；授权结果继承用户在批准时选择的唯一当前上下文。

### 1.1 协议引擎实现约束

System 内嵌 ADDP 受控 Fosite 派生版本，作为 Authorization Code、PKCE、Device Flow、OAuth Refresh、Revocation 和 OIDC 的唯一协议引擎。目标版本、证据和治理要求见 [ADDP IAM OAuth/OIDC 协议引擎 ADR](../next/addp-IAM%20OAuth-OIDC协议引擎ADR.md)。

System 仍是 ADDP 唯一 Auth Server 和 IAM 事实权威，不新增独立认证服务。Fosite 不负责账号、MFA、Tenant Context、Role Permission、owner Resource Grant、页面或外部 IdP 身份映射；这些事实由 System 计算后通过 Authentication / Consent Bridge 写入 Fosite Session。

现有 `request_id` 托管授权交互继续作为浏览器安全边界，但 Client、redirect URI、Scope、PKCE、grant、Code/Device/Refresh 状态和 OAuth error 只能由同一个 Fosite Provider 处理。不得继续保留 `TokenService` 自研协议状态机，不得以正式版缺少 Device Flow 为由按 grant type 分流到自研 Handler。

OIDC ID Token 是发给 OIDC Client 的签名身份断言，不是 ADDP API Access Token。System 不签发用户 JWT 的边界是：ADDP API Access Token 始终为 opaque Token；业务模块不得接受 ID Token 或外部 IdP Token 作为 Bearer Access Token。

## 二、客户端与授权

OAuth Client 独立存储在 `system.oauth_clients`，不复用 `applications` 或 `api_keys`。

第一阶段内置公共客户端：

| client_id | 用途 | redirect URI | Device Flow |
| --- | --- | --- | --- |
| `addp-cli` | ADDP CLI、Codex、Hermes 等本地 Agent | `http://127.0.0.1/callback`（运行时使用随机端口） | 允许 |

公共客户端不配置 Client Secret。Authorization Code Flow 只接受 PKCE `S256`。非 loopback redirect URI 必须与客户端注册值完全一致；原生应用 loopback redirect URI 按 RFC 8252 允许请求在已注册 URI 的 IP 字面量 host 上使用运行时随机端口，但 `scheme`、IP 字面量、path、query 和 fragment 必须与注册值一致。ADDP CLI 固定绑定 `127.0.0.1`，不得使用 `localhost`、非 loopback IP、任意域名、路径前缀或通配符。授权时使用的完整动态 redirect URI 必须原样用于 Authorization Code 兑换。

## 三、Refresh Token Family

一次登录或用户授权创建一个 Refresh Token Family。每次刷新必须在单个数据库事务内：

1. 锁定当前 Refresh Token 和 family；
2. 校验 Principal、当前平台或 Tenant 上下文、客户端、有效期和撤销状态；
3. 将当前 Token 标记为已使用；
4. 创建新的 Refresh Token 和 Access Token；
5. 记录 replaced_by 关系。

已使用 Refresh Token 再次出现时视为重用攻击，立即撤销整个 family 及其全部 Access Token 和 Browser Resource Access Ticket。正常 Web Refresh 轮换也立即撤销上一组资源票据；撤销或轮换时同步删除对应 Redis `auth:context:<token_hash>` 缓存。

## 四、Web 登录与 Cookie

`POST /api/v1/system/login` 返回短期 Access Token，并通过 Cookie 保存 Refresh Token：

- 名称：`addp_refresh_token`
- `HttpOnly=true`
- `SameSite=Lax`
- Path：`/api/v1/system`
- 生产环境 `Secure=true`

`POST /api/v1/system/refresh` 只读取该 Cookie，不接受旧 Access Token。响应返回新的 Access Token，并轮换 Cookie 中的 Refresh Token。

`POST /api/v1/system/logout` 撤销当前 family 并清除 Cookie。

登录认证完成后若存在多个可进入 Context，System 不签发业务 Token，而是返回 Context Selection Ticket 和经 System 计算的 Platform / Tenant 选项。浏览器使用该 Ticket 调用唯一端点 `POST /api/v1/system/auth/context-selections`；客户端只提交 Platform 判别值或 Tenant Membership ID，不能提交 Principal、Tenant、Role 或 Permission。只有一个可用 Context 时可以直接创建 Family，没有可用 Context 时拒绝登录。

浏览器切换 Context 使用 `GET /api/v1/system/auth/context-options` 和 `POST /api/v1/system/auth/context-switches`。切换必须原子撤销当前 Browser Family 并创建绑定目标 Context 的新 Family，不得原地修改 Family；切换只影响当前 Browser Family，不改变既有 CLI / OAuth Family。进入 Platform Context 时必须满足 AAL2 或先完成 step-up。

浏览器 Access Token 只保存在 Browser AuthSession 内存中，不进入 `localStorage`、`sessionStorage`、IndexedDB、iframe URL 或下载 URL。页面启动、刷新和新标签页通过 HttpOnly Refresh Cookie 静默恢复。

第一方 Web 登录和 Web Refresh Token 轮换同时签发短期 Browser Resource Access Ticket。票据使用独立 `addp_rat_` 前缀，只保存 Hash，并以 `addp_resource_access_ticket` HttpOnly Cookie 按 Owner API Path 下发；它不出现在响应 JSON 或资源 URL，只允许 Owner 明确声明的 GET/HEAD 资源路由消费。CLI 和 OAuth Token Endpoint 不签发浏览器资源票据。

Refresh Token 严格轮换要求同 origin 多标签页通过 Web Locks 和 BroadcastChannel 协调，只允许一个页面调用 `/refresh`。Console iframe 模式下只有父 Console 消费 Refresh Cookie，iframe 通过受信任 `postMessage` 获得内存 Access Token。

## 五、Authorization Code + PKCE

CLI 先在 `127.0.0.1` 绑定随机空闲端口，再生成 `code_verifier` 和 S256 `code_challenge`，然后使用表单调用：

```text
POST /api/v1/system/oauth/authorization_requests
```

创建请求携带 `client_id`、完整动态 `redirect_uri`、`scope`、`code_challenge` 和 `code_challenge_method=S256`。System 必须在创建时校验 OAuth Client、redirect URI、Scope 和 PKCE，并返回随机 `request_id`、只向 CLI 返回一次的 `request_secret` 和 `expires_in=300`。System 只保存 `request_secret` 的 SHA-256 Hash。Authorization Request 状态只有 `pending|approved|rejected|cancelled`；超时由 `expires_at` 判定，不保留第二套 expired 状态。

CLI 只以 `request_id` 打开 Console `/oauth/authorize?request_id=...`，浏览器 URL 不再携带 `client_id`、redirect URI、Scope、state 或 PKCE。Console 登录恢复后使用当前 ADDP Access Token 读取：

```text
GET /api/v1/system/oauth/authorization_requests/{request_id}
```

该接口只向已认证用户返回已由 System 校验的客户端名称、`client_id`、Scope 和过期时间；非 pending、已过期或不存在的请求统一视为失效。Console 作出决定时只提交 `request_id` 和 `decision=approved|rejected`：

```text
POST /api/v1/system/oauth/authorizations
```

System 必须在单个数据库事务内锁定 Authorization Request、复核客户端状态与授权边界并完成一次性状态转换。批准时签发 Authorization Code，拒绝时不签发 Code；回跳中的 `state` 固定使用随机 `request_id`，CLI 必须精确比对。Authorization Code 只能使用一次。CLI 再以 `grant_type=authorization_code`、`client_id`、`code`、原完整 `redirect_uri` 和 `code_verifier` 调用 `/oauth/token`。

CLI 在收到回调前退出、取消或超时时，必须使用只在内存持有的 `request_secret` 调用 `DELETE /api/v1/system/oauth/authorization_requests/{request_id}`，并以 `Authorization: Bearer <request_secret>` 传输取消凭据。取消是幂等操作；已批准或已拒绝的请求不能重新变回 cancelled。CLI 进程无法执行取消时，Authorization Request 最多存活 5 分钟。Console 不得继续批准已取消或已过期请求，也不得保留从完整 OAuth query 直接构造授权决定的旧路径。

CLI loopback listener 必须保持运行，直到收到有效 `/callback`、收到授权错误或达到 Authorization Code 的 5 分钟超时；探测请求、favicon 和其他 path 不得提前消费监听器。回调必须严格校验单一 `state`，且只接受单一 `code` 或单一 `error`。收到回调后，浏览器返回不包含 Code、state、OAuth error 详情和 Token 的静态本地结果页；成功页只表示 CLI 已收到授权响应，最终登录结果以 CLI 终端输出为准。结果页必须禁用缓存和 Referrer，并设置限制性 CSP；成功、拒绝、state 不匹配和超时后都必须关闭监听端口。

## 六、Device Authorization Flow

1. CLI 调用 `POST /oauth/device/code` 获取 `device_code`、`user_code`、verification URI、过期时间和轮询间隔。
2. 用户在 Console `/oauth/device` 登录并确认 User Code。
3. Console 调用受 Bearer 保护的 `POST /oauth/device/authorizations`。
4. CLI 按 interval 调用 `/oauth/token`，使用 Device Code grant。
5. pending 返回 `authorization_pending`；过快返回 `slow_down`；批准后只成功兑换一次。

Fosite RFC 8628 Handler 必须注入 ADDP PostgreSQL `DeviceRateLimitStrategy`，不能使用默认的空限速实现。每次合法轮询原子更新下一次允许时间；过快轮询返回 `slow_down`，并把当前及后续轮询 interval 增加 5 秒。该协议限速与 Redis 端点滥用防护同时生效，但使用不同状态和错误语义。

已兑换 Device Code 再次提交时，必须使用原 Device Request ID 撤销其关联 Token Family；不得使用本次 Token 请求新生成的 Request ID 导致撤销落空。

## 七、Token API

`POST /oauth/token` 支持 `authorization_code`、`urn:ietf:params:oauth:grant-type:device_code` 和 `refresh_token` 三种 grant。

OAuth 成功响应包含 `access_token`、`token_type=Bearer`、`expires_in`、`refresh_token` 和 `scope`。CLI 只把 Refresh Token 保存到 OS Keychain；每次命令执行前必须按 ADDP Base URL 获取跨进程互斥锁，再完成读取旧 Refresh Token、调用刷新接口和原子更新新 Refresh Token，避免并行命令把正常竞争误判为重用攻击。

### 7.1 OAuth 安全审计

System 的 `system.audit_logs` 是 OAuth 安全事件的唯一持久化落点，不另建 OAuth 日志表，也不经跨模块审计 API 回写自身。每个 OAuth 非 GET 请求只生成一条结构化审计记录，`event_name` 与 `entity_id` 使用同一个稳定事件名，`entity_type=oauth_security_event`：

- `oauth.authorization_request.created|cancelled|cancel_ignored|failed`；
- `oauth.authorization.approved|rejected|failed`；
- `oauth.device.code.issued|failed`；
- `oauth.device.authorization.approved|rejected|failed`；
- `oauth.token.issued|failed|refresh_reuse_detected`；
- `oauth.token.revoked|revoke_ignored`；
- `oauth.rate_limit.exceeded|unavailable`。

审计详情只允许保存 `client_id`、`grant_type`、`decision`、`scope` 和稳定 `error_code`；事件名、结果与风险等级进入统一列，不在 JSON 中复制。不得保存或部分保留 Access Token、Refresh Token、Authorization Code、Device Code、User Code、PKCE verifier/challenge、state、Cookie、Authorization Header、原始请求体或原始 query。IP、User-Agent、Request ID、HTTP 状态、Principal 和当前平台或 Tenant 上下文继续使用统一审计字段。尚未建立 AuthContext 的 OAuth 失败使用 `context_type=NULL`，不得伪装成 Platform Context。Refresh Token 重用必须记录 `risk_level=high` 事件并保持 HTTP 响应为 `invalid_grant`，不能向客户端泄露 family、用户或攻击判定细节。

OAuth Request、Code、Device Authorization、Token Family 和撤销状态的成功转换，必须与对应安全审计事件在同一个 PostgreSQL 事务提交；状态转换成功但审计丢失，或审计成功但协议状态回滚，均视为事务失败。纯前置校验失败没有待修改的协议事实时，仍写入一条统一审计事件。

首次切换目标 IAM 时必须按统一破坏性迁移规则重建开发环境 `system` Schema；runner 拒绝旧 IAM Schema，不在 System 启动路径读取或清理旧审计列。需要保留历史审计的环境必须先走独立审批的离线脱敏、归档和导入设计，运行时不保留旧日志格式兼容分支。

### 7.2 OAuth 端点限流

OAuth 限流状态统一存放在 Infra Redis，保证多个 System 实例共享同一计数；Redis 不可用时 OAuth 端点失败关闭，不得回退到进程内计数。固定一分钟窗口的默认上限如下：

| 端点 | 默认上限 | 限流键 |
| --- | ---: | --- |
| `/oauth/authorization_requests` 创建与取消、`/oauth/token`、`/oauth/device/code`、`/oauth/revoke` | 60 次/分钟 | endpoint + client IP；请求包含 `client_id` 时同时纳入 |
| `/oauth/authorizations`、`/oauth/device/authorizations` | 30 次/分钟 | endpoint + 当前 `user_id`；请求包含 `client_id` 时同时纳入 |

超限统一返回 HTTP 429、`error=temporarily_unavailable`、`Retry-After` 和标准限流响应头，并写入 `oauth.rate_limit.exceeded`；Redis 计数失败返回 HTTP 503、同一 OAuth error，并写入 `oauth.rate_limit.unavailable`。Device Flow 自身的轮询 `slow_down` 规则继续生效，它与端点级滥用防护是同一路径上的两层约束，不是替代关系。

限流 IP 必须来自 Gin 受信代理配置。System 默认只信任 loopback 代理；容器或生产反向代理网段必须通过 `TRUSTED_PROXIES` 显式声明，禁止信任任意来源提交的转发头。

## 八、AuthContext 映射

第一方 Web Token 在 `addp.auth_context/v1` 中返回 `token.type=first_party_access_token`、`client.client_id=addp-web`、`client.audiences=["addp.api"]`、`client.scope_mode=unrestricted` 和空 scopes。OAuth Token 返回 `token.type=oauth_access_token`、真实 `client_id`、`client.audiences=["addp.api"]`、`client.scope_mode=restricted` 和批准的 scopes。

Scope 仍只能缩小权限；owner 模块对 Delegated Access Token 的 audience、Scope 和路由默认拒绝已在阶段 4.3 第一段完成，写入审批由阶段 4.3 第二段完成。

## 九、受委托访问令牌

Agent 不得把进入 Runtime 的原始 User Access Token 继续转发给 owner 模块。每次 ADDP Tool 调用必须先使用当前 User Access Token 调用：

```text
POST /api/v1/system/auth/delegations
Authorization: Bearer <current_user_access_token>
```

请求只包含目标 Tool 的授权边界和审计绑定：

```json
{
  "audience": "develop",
  "scopes": ["workflow.run"],
  "agent_run_id": "7a9f43a7-81f0-4cb4-b545-6bfef53ed922",
  "tool_call_id": "call_abc123"
}
```

规则如下：

1. `audience` 固定为 Tool Manifest 的 `owner` 模块名；Scope 固定使用稳定 Tool 名，不定义第二套能力名称。
2. System 只允许签发已注册的 `audience + scope` 组合，客户端不能请求任意 Scope。
3. Principal、会话模式、当前 Tenant Membership、Role/Permission 授权事实、`client_id` 和 `delegated_by` 均由源 Access Token 派生，客户端不得提交。
4. OAuth 源令牌的 `delegated_by` 为真实 `client_id`；第一方 Web 源令牌固定为 `addp-web`。该字段不接受 Agent 自报身份。
5. 第一方 Web Token 的空 Scope 表示当前用户会话未被 OAuth Scope 额外收窄；OAuth Token 必须具有 `addp.api` 或覆盖所请求 Tool Scope。
6. Resource Access Ticket 和 Delegated Access Token 不得再次签发委托令牌，禁止委托链。
7. Delegated Access Token 不创建 Refresh Token Family、不可刷新、不可兑换，只用于一次短期 Tool 调用边界。
8. System 解析委托令牌时必须同时校验令牌、源 Access Token、源 Family、Principal、会话模式、当前 Tenant Membership 和授权事实仍然有效。
9. owner 模块对委托令牌默认拒绝，只在 Tool Manifest 对应的精确路由校验 audience、全部 required scopes 和必要审批后放行；审批闭环尚未启用的 write Tool 必须继续返回 403。非委托的第一方 Web 和 OAuth API 调用继续执行原 Principal 的 Role Permission 与 owner 资源授权路径。

成功响应只返回短期委托令牌及其明确边界：

```json
{
  "access_token": "addp_dat_...",
  "token_type": "Bearer",
  "expires_in": 120,
  "audience": "develop",
  "scopes": ["workflow.run"],
  "agent_run_id": "7a9f43a7-81f0-4cb4-b545-6bfef53ed922",
  "tool_call_id": "call_abc123"
}
```

### 9.1 委托写操作审批

委托写操作必须由业务 owner 持有审批事实。当前 `workflow.run` 的 owner 为 Develop，使用同一个 `POST /api/v1/develop/executions` 完成两阶段调用：

1. 首次请求携带 workflow 执行内容，不携带 `approval_id`。Develop 校验委托令牌后创建 pending approval，返回 HTTP 202、`status=approval_required`、`interaction_id`、`open_url` 和 `request_fingerprint`，不得创建 execution。
2. 用户使用第一方或 OAuth User Access Token 在 Develop 审批页作出 `approved|rejected` 决定。Delegated Access Token 和内部身份不得调用 decision API。
3. Agent 使用进入 Runtime 的原始 User Access Token 查询审批状态；客户端提交的 `check` 只触发查询，不能构造批准事实。
4. 审批为 approved 后，Agent 为同一 AgentRun 再次调用 `workflow.run`，请求只携带 `approval_id + request_fingerprint`。
5. Develop 校验原 Principal、当前 Tenant Membership、AgentRun、审批状态、过期时间和 SHA-256 请求指纹，从 owner 表读取原 workflow 请求，并在一次性消费审批后创建 execution。

Agent 不保存完整 workflow 审批 payload。approval 默认 15 分钟过期，只能消费一次；拒绝、过期、已消费、指纹不匹配或身份不匹配均返回稳定错误且不得创建 execution。同一 AgentRun 重复消费已完成审批返回 `approval_already_consumed`；其他 AgentRun 重放该 approval 时必须优先返回 `approval_forbidden`，不得通过错误码泄露审批终态。

## 十、禁止事项

- 保存任何 Token、Authorization Code 或 Device Code 明文。
- 保存 Authorization Request Secret 明文，或把它放入浏览器 URL、日志和审计详情。
- 把 OAuth 表单或 JSON 原始请求体写入通用审计日志，或在安全事件中保存 Code、Token、User Code、PKCE、state、Cookie 和 Authorization Header。
- OAuth 限流在 Redis 不可用时回退为单实例内存计数，或直接信任任意来源的 `X-Forwarded-For`。
- 公共客户端内置 Client Secret。
- 支持 PKCE `plain`。
- redirect URI 前缀匹配、通配符或回退 URI；RFC 8252 规定的 loopback IP 动态端口是唯一例外，不能放宽 scheme、host、path、query 或 fragment。
- Console 在未经 System 校验的情况下直接跳转客户端提交的 redirect URI。
- Console 从浏览器 query 读取 Client、redirect URI、Scope、state 或 PKCE 并直接构造授权决定。
- 同时保留 JWT 刷新和 Refresh Token Family 两条路径。
- 使用 API Key、`INTERNAL_API_KEY` 或 Scope 模拟用户身份提升。
- Agent Tool Client 把原始 User Access Token 直接传给 owner 模块。
- owner 模块仅解析委托字段但不强制 audience、Scope 和路由白名单。
