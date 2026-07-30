# ADDP 授权上下文规范

更新日期：2026-07-24

状态：正式目标规范。本规范定义 ADDP 访问令牌的唯一解析语义和模块消费路径；目标 JSON 契约固定为 `addp.auth_context/v1`，完整 Schema 和示例见 `docs/next/addp-IAM AuthContext契约设计.md`，现有 `user_type` 响应结构不是目标契约。

## 一、目标

ADDP 只保留一套 IAM 事实：

- System 是 User、Service Principal、Local Account、External Identity、Tenant、Tenant Membership、Department、Project Group、Permission、Role 和 Role Assignment 的唯一逻辑权威；
- OAuth Client 只表达客户端软件，不创建 OAuth 用户、OAuth 租户或独立授权体系；
- Scope 只能缩小令牌能力，不能提升 Principal 已有权限；
- 业务资源、Resource Grant / Policy 和最终资源访问判断归对应 owner 模块；
- 业务模块不自行解析 JWT、OAuth 或受委托令牌，只消费 System 返回的 AuthContext。

稳定概念和授权边界见 `docs/concepts/addp账号与权限体系图.md`。

## 二、唯一调用链

```text
Authorization: Bearer <access_token>
  -> System Token Service
  -> 查询 Token Hash，验证有效期、family 和撤销状态
  -> 回查 Principal、会话模式和当前授权事实
  -> AuthContext
  -> common Go / common-python 认证中间件
  -> owner 模块执行 Permission、Scope、资源、条件和审批校验
```

禁止保留以下平行路径：

- Go 认证中间件通过 `/users/me` 推断令牌身份；
- Agent 或其他 Python 模块自行使用 `JWT_SECRET` 解析用户令牌；
- 各模块自行定义 claims、Scope、`user_type` 或平台管理员跨租户语义；
- 通过 `tenant_id=null`、`tenant_id=0` 或缺失 Tenant 绕过 Tenant 隔离。

`GET /api/v1/system/users/me` 只提供当前用户资料，不是 Token 验证接口。

## 三、AuthContext 语义契约

System 提供唯一解析接口：

```text
GET /api/v1/system/auth/context
Authorization: Bearer <access_token>
```

AuthContext 根对象固定包含：

| 字段 | 必要内容 | 约束 |
| --- | --- | --- |
| `schema_version` | 固定值 `addp.auth_context/v1` | 不按客户端协商或返回双 Schema |
| `principal` | `user | service_principal` 和稳定 Principal ID | 不返回 username、email 或 Local Account |
| `context` | `platform` 或 `tenant` 判别联合 | Tenant 模式必须包含唯一 Tenant 和 Tenant Membership |
| `authentication` | 认证方法、AAL、认证时间和 step-up 有效期 | User 进入 Platform Context 至少为 AAL2 |
| `client` | 显式 Client、audience、`scope_mode` 和 Scope | Scope 只能缩小能力；第一方 Web 固定为 `addp-web` |
| `organization` | 当前 Tenant 的直接 Department / Project Group Membership | Platform Context 为空；Department 祖先不代表默认继承 |
| `authorization` | 授权版本和当前有效 Role Assignment | 每个 Assignment 携带 Permission、Scope、来源和有效期 |
| `token` | Token 类型、签发时间和过期时间 | 使用带时区的 ISO 8601 时间 |
| `delegation` | 委托 Client、AgentRun 和 ToolCall | 只在 Delegated Token 存在，其他 Token 为 `null` |

所有 IAM bigint ID 在 JSON 中使用非零十进制字符串，避免 JavaScript Number 精度丢失。响应对象和嵌套对象拒绝未知字段；数组使用契约规定的稳定排序。

平台管理上下文不得通过空 Tenant 表示“所有 Tenant”。它只激活当前 Principal 被分配的平台角色。Tenant 业务上下文必须绑定一个有效 Tenant Membership，且只投影当前 Tenant 内的角色和组织事实。

第一方 Web、OAuth、Service Principal、Browser Resource Access Ticket 和 Delegated Access Token 都解析为同一 AuthContext 语义。第一方 Web 固定返回 `client_id=addp-web`、`audiences=[addp.api]`、`scope_mode=unrestricted` 和空 Scope；Browser Resource Access Ticket 固定返回 `token.type=resource_access_ticket`、`client_id=addp-web`、`audiences=[owner]`、`scope_mode=restricted`、`scopes=[resource:read]` 和 `delegation=null`，其 Principal、Context、认证事实、组织事实、授权版本和 Role Assignment 必须从所属第一方 Browser Family 的当前事实投影；OAuth Token 返回真实 Client、`addp.api` audience、`scope_mode=restricted` 和批准 Scope；Delegated Access Token 固定返回 `token.type=delegated_access_token`、源 Family 的真实 `client_id`、唯一 owner audience、`scope_mode=restricted`、当前 Tool 的稳定 Scope，以及 `delegated_by_client_id + agent_run_id + tool_call_id` 审计绑定。`delegated_by_client_id` 必须等于 `client_id`，两者都从源 Family 派生，不接受委托请求自报。

## 四、身份和上下文校验

System 生成 AuthContext 前必须校验：

1. opaque Token Hash 存在，Access Token 未过期、未撤销且 family 有效；
2. Principal 存在、类型匹配且当前有效；
3. 会话模式只能是 `platform` 或 `tenant`；
4. `tenant` 模式绑定的 Tenant 和 Tenant Membership 均存在且有效；
5. `platform` 模式不携带当前 Tenant，也不激活任何 Tenant Role、Department、Project Group 或资源授权；
6. Token 的 `issued_authorization_version` 等于 Principal 当前 `authorization_version`，Role Assignment、外部身份状态和组织关系没有失效；
7. OAuth Client、audience、Scope、认证强度和委托绑定满足令牌类型要求。

任一校验失败均不返回部分 AuthContext。Role Assignment、Role Permission 和组织授权关系变化时，在同一授权变更事务中递增 Principal 授权版本，旧派生凭据立即失效；仍有效且未撤销的 Refresh Token Family 只能在唯一 Refresh Token 轮换事务中复核身份、Context、Tenant 和 Membership 后推进到当前版本。User、凭据、Tenant Membership、Tenant 或 Token Family 失效时，在同一安全事务中递增授权版本并撤销受影响 Token Family，不允许通过版本推进恢复。

## 五、权限计算

有效权限是以下条件的交集：

```text
Principal 有效
  ∩ 会话模式匹配
  ∩ Tenant Membership 有效（tenant 模式）
  ∩ Tenant 有效（tenant 模式）
  ∩ audience 允许
  ∩ Scope 允许
  ∩ Role Permission 允许
  ∩ owner Resource Grant / Policy 允许
  ∩ 上下文条件允许
  ∩ 必要审批已完成
  ∩ 不存在显式 Deny
```

规则如下：

- 默认拒绝，显式 Deny 优先于 Allow；
- Scope 只能缩小权限，不能授予 Role Permission 或跨 Tenant 能力；
- 平台角色不自动产生 Tenant 业务权限；跨租户聚合统计只通过独立 Statistics Permission 访问；
- Resource Grant 不批量写入 Token 或 AuthContext，owner 使用 Principal 和授权事实完成最终判断；
- Department 父子授权默认不继承，Project Group 权限不传播到成员所在 Department；
- 临时授权必须携带有效期、来源和撤销事实。

委托令牌使用默认拒绝策略：owner 模块收到 Delegated Access Token 时，只有当前路由与 Tool Manifest 中该 Tool 的 owner、唯一 audience 和 required scopes 精确集合完全匹配，并具有声明的 Role Permission all-of 候选时才可继续；额外 Scope 同样拒绝，委托令牌不能访问同一模块的其他普通 API。普通第一方或 OAuth User Token 继续执行该路由原有 Permission 与资源授权路径，不被 Delegated Route Guard 放宽或替代。写 Tool 的审批事实仍由 owner Handler 判断。

Browser Resource Access Ticket 同样使用默认拒绝策略。System 的 AuthContext 接口是仅供 Owner 解析票据的基础设施例外；System 的其他身份、Tenant、OAuth、引擎和管理 API 必须拒绝该票据。Owner 只在挂载 Resource Ticket Guard 的 GET/HEAD 原生资源路由读取 `addp_resource_access_ticket` Cookie；Guard 挂载本身就是路由白名单，不再接收运行时 matcher。该路由不得同时接受 `Authorization` Header 和 Ticket Cookie，出现 `Authorization` Header 时统一拒绝，避免歧义凭据优先级。Guard 必须校验 `token.type=resource_access_ticket`、唯一 owner audience、`scope_mode=restricted`、唯一 `resource:read` Scope 以及声明的 Role Permission all-of 候选；最终 Assignment Scope、资源归属、Grant、Policy 和 Explicit Deny 仍由 owner Handler 判断。

### 5.1 同步 BFF 授权

Portal 等同步 BFF 不拥有其所展示业务资源的授权事实。BFF 调用 Asset 等 owner 的消费 API 时，
只在当前请求调用栈内转发已经过自身 AuthContext 中间件验证的 User Bearer；owner 必须重新解析
同一 AuthContext，并完成 Permission、资源状态、资源归属、Grant、Policy 和 Explicit Deny 判断。
BFF 不能提交或让 owner 信任 User、Tenant、Membership、Role、申请人或评价人字段，也不能保存、
缓存、记录或异步转发 User Access Token。

BFF 以 Service Principal 调用下游只适用于不代表用户作业务决定的最小聚合能力。该 Service
Principal 必须使用独立 Confidential OAuth Client、当前 Tenant Membership、专用 Runtime Role、
固定 Client Guard 和精确 Permission。Portal 的机器身份不得持有 `asset.*` 业务 Permission；它在
Asset 已按当前 User 确认有效授权后，只能读取 Service 端点投影。真实数据访问仍由 owner Resource
Grant 或 Resource Ticket 独立判断。

### 5.2 计算执行授权

SQL、Workflow、Jupyter 等计算入口必须先在当前 User AuthContext 下完成两层判断：一是入口自身的功能 Permission，二是本次执行涉及资源与 `read | write | ddl | external_effect` 效果的 owner 决策。通过后由 System 创建绑定唯一 execution 的 Execution Authorization。

Execution Authorization 固定包含当前 Principal、Tenant Membership、`authorization_version`、owner audience、execution ID、允许的 Engine Instance、允许效果、签发时间和到期时间。它不是访问令牌，不进入 AuthContext Schema，也不得以源 Access Token 的存活、Refresh Token Family 或 Token ID 作为执行期授权事实；统一执行记录只保存其稳定 ID 和不含密钥的审计摘要。原始 User Access Token、Service Access Token、Engine 明文连接和运行时访问计划不得写入 `execution_config`、任务定义、日志或审计详情。

匹配 audience 的 Runtime Service Principal 使用自身 Service Access Token 消费 Execution Authorization。System 必须同时校验 Service Principal/OAuth Client 与 audience 匹配、Tenant Context 相同、Execution Authorization 未过期或撤销、源 Principal/Membership/授权版本仍有效、Engine 与效果均在授权边界内。Service Principal 的 Runtime Role 只授予消费执行授权所需的内部 Permission，不授予通用 `system.engine.read`、Tenant 数据 Permission 或用户 Role。

交互式执行以当前 User 为授权主体；异步执行把创建 execution 时的 User、Tenant Membership 和授权版本写为不可变执行来源事实。定时执行绑定任务授权主体：该主体只能由同 Tenant 的当前 User AuthContext 在创建、更新或显式重新授权任务时写入，并必须绑定当前任务定义；任务定义或授权版本发生变化后不得继续沿用旧主体。每次执行开始前必须重新校验 Membership、Role、资源规则和授权版本；显式平台自动任务才使用 Service Principal 自身 Runtime Role。任何路径都不得持久化或代传原始 User Access Token。

跨模块异步调用时，owner Runtime Service Principal 只能为与自身 audience 匹配的子 execution 请求 Execution Authorization。System 必须验证父 execution、子 execution、Tenant、User、Membership、授权版本和 `parent_execution_id` 来源链完全一致，并重新计算当前 Role Permission；调用方提交的主体字段不能单独成为授权事实。Orchestrator Service Principal 只负责调用 TaskProvider 和传递父 execution 身份，不获得数据效果 Permission，也不能任意指定或替换任务授权主体。

## 六、共享中间件

### 6.1 Go

`common/middleware/auth` 负责：

- 把 Bearer Token 转发给 System AuthContext API；
- 将不可变 AuthContext 注入 Gin Context；
- 提供 Principal、会话模式、当前 Tenant、client、audience、Scope、认证强度和授权事实的统一 helper；
- 不提供或新增基于 `user_type` 的授权 helper。

第一阶段不跨请求缓存任何 Access Token 或 Resource Ticket AuthContext。System 每次解析都回查 Token/Ticket、Family、Principal、Membership、授权版本和当前有效 Assignment；`common/middleware/auth` 不保留 `CachedSystemAuthMiddleware` 或其他旧缓存旁路。以后只有性能证据证明必要时，才能引入带签发版本校验和可靠失效协议的单一路径缓存。

### 6.2 Python

`common-python/addp_common/auth.py` 负责调用 System AuthContext API 并生成不可变 `AuthorizationContext`。Agent、Copilot 等 Python 模块不保留私有 JWT 解析器、私有用户身份 DTO 或 `user_type` 权限分支。

## 七、OAuth 与 Refresh Token 要求

- CLI 有浏览器时使用 Authorization Code + PKCE；
- 无浏览器或跨设备时使用 Device Authorization Flow；
- 公共客户端不使用可内置的统一 Client Secret；
- Refresh Token 只保存 Hash，每次刷新必须轮换；
- 已轮换 Refresh Token 被重复使用时，撤销整个 Token Family；
- CLI 只把 Refresh Token 保存到 OS Keychain，Access Token 保持短期。

OAuth 数据模型不复用 `system.applications/api_keys`：API Key 表达应用身份，OAuth Client 表达获得用户授权的客户端软件，两者生命周期和审计语义不同。

内部模块使用 Fosite Client Credentials Grant 获取 Service Access Token。每个模块使用
独立 Confidential OAuth Client，Client 必须一对一绑定一个 Service Principal；Client
Secret 只保存 BCrypt Hash。Tenant Runtime 请求中的 `tenant_id` 仅用于从该 Service
Principal 的有效 Tenant Membership 中选择 Context，不能直接成为授权事实；平台控制面
请求必须显式提交 `context_type=platform`，且只允许平台所有 Service Principal 使用专用
Platform Service Role，不允许 Service Principal 持有或借用平台三员 Role。两种请求形态
互斥，缺失或同时提交 Context 判别均拒绝。签发的 Token 固定为
`service_access_token`、`authentication.methods=["service_secret"]`、
`assurance_level=not_applicable`，有效期不超过 5 分钟且不签发 Refresh Token。业务请求只
发送 Bearer Token，禁止同时发送 `X-Internal-API-Key` 或 `X-Tenant-ID`。

## 八、禁止事项

- 外部 Agent 使用 `INTERNAL_API_KEY`；
- 在 CLI、Agent 或 owner 模块内保存 `JWT_SECRET`；
- 客户端提交 Principal、User、Tenant、Membership、Role 或 `user_type` 并被服务端信任；
- 用 API Key、OAuth Scope 或平台角色模拟 Tenant 业务授权；
- 通过 Scope 提升 Role Permission 或跨 Tenant 权限；
- 业务模块从 Token 字符串、日志或前端状态反推授权上下文；
- 在目标实现中保留 `user_type` 与 Role Assignment 双轨权限判断。

## 九、实现切换要求

当前代码和部分 System 表/API文档仍使用 `users.user_type` 与 `users.tenant_id`。它们是待替换的实现差距，不构成兼容契约。实施目标 IAM 时必须同步完成：

1. 将已确认的 v1 JSON Schema 落为 `common/authorization/schemas/auth-context-v1.schema.json`，生成或校验共享 Go/Python/TypeScript 类型；
2. 将所有调用方一次性迁移到新契约；
3. 删除 `user_type` 字段、helper、条件分支和旧文档；
4. 删除 `tenant_id=null` / `tenant_id=0` 的平台全权语义；
5. 完成 System、common、common-python、Gateway 和 owner 模块契约测试后再切换运行路径。

不允许旧 AuthContext 与新 AuthContext 按客户端、路由或配置双轨共存。

当前 IAM Runtime 使用唯一 `auth-context-v1.schema.json` 和共享类型投影第一方 Web、OAuth User、Service Principal、Browser Resource Access Ticket 与 Delegated Access Token。Resource Ticket 使用所属第一方 Browser Family 的同一身份与授权投影，并用 owner audience 和 `resource:read` 额外收窄；Delegated Token 回溯源 Access Token 与 Family，并用 owner Tool Scope 和审计绑定额外收窄；Service Access Token 只由 Fosite Client Credentials Grant 签发，并固定为一个 Tenant Membership Context 或一个显式 Platform Service Context。调用方必须一次性迁移，不能保留共享 Internal API Key 与 Bearer 双轨。

Execution Authorization 不新增 AuthContext Token 类型，也不复用 Agent Delegated Access Token。它是由 User AuthContext 派生、由匹配 Runtime Service Principal 消费的执行期授权事实；调用方迁移后必须删除“Service Principal 直接获得通用 Engine 明文读取权限”和“用户 Token 代传到 Worker/Runtime”两条旧路径。
