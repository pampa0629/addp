# ADDP OAuth 授权规范

更新日期：2026-07-17

状态：阶段 4.3 正式规范。OAuth 登录、浏览器会话、资源票据和受委托访问令牌均以本文为准。

## 一、统一令牌模型

ADDP 的 Web、CLI 和 OAuth 客户端统一使用随机 opaque 用户令牌。业务模块不解析令牌，只调用 System AuthContext。

- Access Token：`addp_at_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 15 分钟。
- Refresh Token：`addp_rt_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 30 天。
- Authorization Code：`addp_ac_` 前缀，单次使用，默认有效期 5 分钟。
- Device Code：`addp_dc_` 前缀，只保存 Hash，默认有效期 10 分钟。
- User Code：8 位易输入字符，服务端只保存规范化值的 Hash。
- Delegated Access Token：`addp_dat_` 前缀，32 字节随机值，只保存 SHA-256 Hash，默认有效期 2 分钟且不得超过源 Access Token 剩余有效期。

System 不再签发或解析用户 JWT；旧的“允许过期 Access Token 调 `/refresh`”路径删除。

## 二、客户端与授权

OAuth Client 独立存储在 `system.oauth_clients`，不复用 `applications` 或 `api_keys`。

第一阶段内置公共客户端：

| client_id | 用途 | redirect URI | Device Flow |
| --- | --- | --- | --- |
| `addp-cli` | ADDP CLI、Codex、Hermes 等本地 Agent | `http://127.0.0.1:8765/callback` | 允许 |

公共客户端不配置 Client Secret。Authorization Code Flow 只接受 PKCE `S256`，redirect URI 必须与客户端注册值完全一致。

## 三、Refresh Token Family

一次登录或用户授权创建一个 Refresh Token Family。每次刷新必须在单个数据库事务内：

1. 锁定当前 Refresh Token 和 family；
2. 校验用户、租户、客户端、有效期和撤销状态；
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

浏览器 Access Token 只保存在 Browser AuthSession 内存中，不进入 `localStorage`、`sessionStorage`、IndexedDB、iframe URL 或下载 URL。页面启动、刷新和新标签页通过 HttpOnly Refresh Cookie 静默恢复。

第一方 Web 登录和 Web Refresh Token 轮换同时签发短期 Browser Resource Access Ticket。票据使用独立 `addp_rat_` 前缀，只保存 Hash，并以 `addp_resource_access_ticket` HttpOnly Cookie 按 Owner API Path 下发；它不出现在响应 JSON 或资源 URL，只允许 Owner 明确声明的 GET/HEAD 资源路由消费。CLI 和 OAuth Token Endpoint 不签发浏览器资源票据。

Refresh Token 严格轮换要求同 origin 多标签页通过 Web Locks 和 BroadcastChannel 协调，只允许一个页面调用 `/refresh`。Console iframe 模式下只有父 Console 消费 Refresh Cookie，iframe 通过受信任 `postMessage` 获得内存 Access Token。

## 五、Authorization Code + PKCE

CLI 生成 `code_verifier`、S256 `code_challenge` 和随机 `state`，打开 Console `/oauth/authorize`。Console 使用当前 ADDP Access Token 调用：

```text
POST /api/v1/system/oauth/authorizations
```

System 校验用户、客户端、redirect URI、Scope 和 PKCE 后返回重定向 URL。Authorization Code 只能使用一次。CLI 再以 `grant_type=authorization_code`、`client_id`、`code`、`redirect_uri` 和 `code_verifier` 调用 `/oauth/token`。

## 六、Device Authorization Flow

1. CLI 调用 `POST /oauth/device/code` 获取 `device_code`、`user_code`、verification URI、过期时间和轮询间隔。
2. 用户在 Console `/oauth/device` 登录并确认 User Code。
3. Console 调用受 Bearer 保护的 `POST /oauth/device/authorizations`。
4. CLI 按 interval 调用 `/oauth/token`，使用 Device Code grant。
5. pending 返回 `authorization_pending`；过快返回 `slow_down`；批准后只成功兑换一次。

## 七、Token API

`POST /oauth/token` 支持 `authorization_code`、`urn:ietf:params:oauth:grant-type:device_code` 和 `refresh_token` 三种 grant。

OAuth 成功响应包含 `access_token`、`token_type=Bearer`、`expires_in`、`refresh_token` 和 `scope`。CLI 只把 Refresh Token 保存到 OS Keychain；每次命令执行前刷新并原子更新轮换后的 Refresh Token。

## 八、AuthContext 映射

第一方 Web Token返回 `auth_type=first_party_access_token`、`client_id=null`、空 audiences/scopes。OAuth Token 返回 `auth_type=oauth_access_token`、真实 `client_id`、`audiences=["addp-api"]` 和批准的 scopes。

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
3. `user_id`、`tenant_id`、`user_type`、`client_id` 和 `delegated_by` 均由源 Access Token 派生，客户端不得提交。
4. OAuth 源令牌的 `delegated_by` 为真实 `client_id`；第一方 Web 源令牌固定为 `addp-web`。该字段不接受 Agent 自报身份。
5. 第一方 Web Token 的空 Scope 表示当前用户会话未被 OAuth Scope 额外收窄；OAuth Token 必须具有 `addp.api` 或覆盖所请求 Tool Scope。
6. Resource Access Ticket 和 Delegated Access Token 不得再次签发委托令牌，禁止委托链。
7. Delegated Access Token 不创建 Refresh Token Family、不可刷新、不可兑换，只用于一次短期 Tool 调用边界。
8. System 解析委托令牌时必须同时校验令牌、源 Access Token、源 Family、用户和租户仍然有效。
9. owner 模块对委托令牌默认拒绝，只在 Tool Manifest 对应的精确路由校验 audience、全部 required scopes 和必要审批后放行；审批闭环尚未启用的 write Tool 必须继续返回 403。非委托的第一方 Web 和 OAuth API 调用继续执行原用户权限路径。

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
5. Develop 校验原用户、原租户、AgentRun、审批状态、过期时间和 SHA-256 请求指纹，从 owner 表读取原 workflow 请求，并在一次性消费审批后创建 execution。

Agent 不保存完整 workflow 审批 payload。approval 默认 15 分钟过期，只能消费一次；拒绝、过期、已消费、指纹不匹配或身份不匹配均返回稳定错误且不得创建 execution。同一 AgentRun 重复消费已完成审批返回 `approval_already_consumed`；其他 AgentRun 重放该 approval 时必须优先返回 `approval_forbidden`，不得通过错误码泄露审批终态。

## 十、禁止事项

- 保存任何 Token、Authorization Code 或 Device Code 明文。
- 公共客户端内置 Client Secret。
- 支持 PKCE `plain`。
- redirect URI 前缀匹配、通配符或回退 URI。
- 同时保留 JWT 刷新和 Refresh Token Family 两条路径。
- 使用 API Key、`INTERNAL_API_KEY` 或 Scope 模拟用户身份提升。
- Agent Tool Client 把原始 User Access Token 直接传给 owner 模块。
- owner 模块仅解析委托字段但不强制 audience、Scope 和路由白名单。
