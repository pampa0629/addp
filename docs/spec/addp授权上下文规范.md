# ADDP 授权上下文规范

更新日期：2026-10-09

状态：正式规范。本规范定义 ADDP 访问令牌的唯一解析语义和模块消费路径；当前唯一 JSON 契约为 `addp.auth_context/v1`。

机器可校验的唯一 Schema 位于 `common/authorization/schemas/auth-context-v1.schema.json`，共享 Go 类型位于 `common/authorization`，Python 类型位于 `common-python/addp_common/auth.py`。本文不复制完整 Schema，避免形成第二事实源。

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

Monitor 租户任务执行诊断遵循 5.1 的同步 BFF 规则：只能在已验证的当前 User 请求调用栈内转发 Bearer 给任务 Owner 的读取范围接口，不保存、缓存或后台使用该 Token。Owner 重新认证和裁决；读取范围不能由浏览器指定主体或 Tenant。Monitor 的功能 Permission 不替代 Owner 读取 Permission，历史 Execution Authorization 不授予当前诊断读取权。第一版只支持 Owner 明确的 Tenant 级任务历史和本人一次性 execution 范围；无可信资源归属时不推断组织范围权限。平台服务进程日志仍由 System 在 Platform Context 下授权，操作审计保持独立。

Develop 专业执行结果读取与 Monitor 通用历史读取分开裁决。专业列表、详情、轮询、日志和基于原 execution 的导出只允许当前 User 读取本人执行；保存任务或任务定义已删除均不扩大该范围。当前 Tenant、Principal、Tenant Membership、授权版本必须与执行保存的来源事实完全一致，缺失来源事实或版本已变化时拒绝；历史 Execution Authorization 的到期时间不作为已完成结果的 TTL，也不能代替当前授权。当前 `develop.task.read` 是入口条件，query/workflow 专业结果还要求 `develop.data_read.execute`，script 要求 `develop.notebook.read`；无可信组织资源归属时不推断 Department/Project Group 范围。OAuth 和 Delegated User 仍须先经过现有 Client Scope / Tool 路由裁决，Service Principal 不进入专业结果接口。查询导出会话和文件下载必须回到原 execution 复核当前专业读取权；路径绑定 Browser Resource Access Ticket 只在既有下载路由验证后进入该复核，不能用于其他专业接口。Script 新执行从已验证的当前 AuthContext 保存同一来源事实，不补造旧执行主体。源数据和身份授权变化推进授权版本后，专业结果拒绝继续读取；安全历史仍按 Monitor 契约读取。暂不提供结果分享或跨主体重授权。

TaskProvider 的 Service 状态读取使用独立契约：Develop 只允许已认证的 `addp-orchestrator` 读取 `source=orchestrator` 且关联同 Tenant Orchestrator 父 execution 的子执行。共享 TaskProvider 响应只投影状态、安全诊断和 `metadata.outputs` 中的稳定交接输出，不回传 execution_config、原始 metadata/result 或执行授权引用。输出引用不授予对应产物的数据读取权。

SQL、Workflow、Jupyter 以及 Service 查询服务发布前的样例验收等用户计算入口，必须先在当前 User AuthContext 下完成两层判断：一是入口自身的功能 Permission，二是本次执行涉及资源与 `read | write | ddl | external_effect` 效果的 owner 决策。Service 样例验收使用 `service.definition.create + service.data_read.execute`，普通 SQL 的 Execution Authorization audience 为 `service`，联邦 SQL 的 audience 为 `duckdb`。已发布查询服务由 Service 根据不可变服务定义、数据源依赖快照、公开/私有访问策略和当前请求上下文作出 owner 决策。两类入口通过后都由 System 创建绑定唯一 execution 的 Execution Authorization。

Execution Authorization 的来源固定为互斥的 `user` 或 `service_definition`。用户来源包含当前 Principal、Tenant Membership 和 `authorization_version`；其中由 Notebook Session 派生的用户来源还必须保存 `source_notebook_session_authorization_id`，不能退化为不受 Session 和 Token Family 生命周期约束的普通用户来源。服务定义来源包含 owner module、definition ID、definition version/hash 和签发 Service Principal。两者都必须包含 owner audience、execution ID、不可变操作范围、签发时间和到期时间；业务引擎任务逐 Source Engine 保存 Engine Access Scope。每个 scope 由唯一 `engine_id` 和该引擎允许的非空效果集合组成；禁止以独立 `engine_ids` 与 `effects` 集合表达授权，因为两者笛卡尔积会扩大权限。查询服务定义的 version/hash 必须覆盖发布时冻结的 Source Engine ID，Service 不得在请求期按当前 Engine 名称重新绑定数据源。服务定义来源第一阶段只允许 `addp-service` 为本 Tenant 的已发布查询服务签发逐引擎 `read` scope，System 不接受客户端自报其他 owner、效果或 audience。

匹配 audience 的 Runtime Service Principal 使用自身 Service Access Token 消费 Execution Authorization。System 必须同时校验 Service Principal/OAuth Client 与 audience 匹配、Tenant Context 相同、Execution Authorization 未过期或撤销、来源仍有效，并且当前 Engine 的 scope 包含本次所需效果。不得用其他 Engine scope 的效果满足当前 Engine。用户来源继续校验 Principal/Membership/授权版本；服务定义来源校验 owner Service Principal、definition version/hash 和未撤销状态。需要在调用 Runtime 前解析脱敏端点的 owner Runtime Role，可以同时获得 `system.engine_descriptor.read` 与 `system.execution_authorization.execute`；它仍不得获得通用 `system.engine.read`、Tenant 数据 Permission 或用户 Role。

DuckDB 是平台共享的联邦查询 Runtime；`addp-duckdb` 在租户上下文中固定持有 `tenant.duckdb_runtime`，该 Role 只包含 `system.execution_authorization.execute`。DuckDB 从消费 Execution Authorization 的单次响应获得精确 Engine Access，不得持有 `meta.catalog.read`、`system.engine.read` 或其他租户数据权限。

交互式执行以当前 User 为授权主体；异步执行把创建 execution 时的 User、Tenant Membership 和授权版本写为不可变执行来源事实。定时执行绑定任务授权主体：该主体只能由同 Tenant 的当前 User AuthContext 在创建、更新或显式重新授权任务时写入，并必须绑定当前任务定义；任务定义或授权版本发生变化后不得继续沿用旧主体。每次执行开始前必须重新校验 Membership、Role、资源规则和授权版本；显式平台自动任务才使用 Service Principal 自身 Runtime Role。任何路径都不得持久化或代传原始 User Access Token。

跨模块异步调用时，owner Runtime Service Principal 只能为与自身 audience 匹配的子 execution 请求 Execution Authorization。System 必须验证父 execution、子 execution、Tenant、User、Membership、授权版本和 `parent_execution_id` 来源链完全一致，并重新计算当前 Role Permission；调用方提交的主体字段不能单独成为授权事实。静态资源边界只允许子 execution 处于 `pending` 时签发，每个 `audience + execution_id` 只有一份不可变授权；必须在 claim 后解析的动态资源边界，请求必须同时提交正数 `attempt` 和规范 UUID `lease_token`，System 只在数据库中的子 execution 处于 `running`、attempt/token 精确匹配且 `lease_expires_at > NOW()` 时签发。该授权不可变绑定当前 attempt 和 lease token，每个 `audience + execution_id + attempt` 只有一份；消费引擎访问时 System 必须再次校验它仍是当前未过期租约。新 attempt 必须签发新授权，旧 attempt 授权保留审计但不再可消费；两种状态不得使用兼容或降级路径互换。Orchestrator Service Principal 只负责调用 TaskProvider 和传递父 execution 身份，不获得数据效果 Permission，也不能任意指定或替换任务授权主体。

### 5.2.1 内部任务执行授权

内部任务的 Execution Authorization 使用与 Engine Access Scope 互斥的 `internal_task` 范围，空 `accesses` 本身不构成授权。首期只允许当前 Tenant User 为 `audience=ontology` 签发 `task_type=semantic_projection`，必须固定 `resource_id`、十进制字符串 `revision`、SHA-256 `digest` 和 UUID `generation`。System 复核 `ontology.revision.publish` 与既有 pending execution 的 Tenant、actor、任务类型和配置；不读取 Ontology 业务表。内部任务不得由 Notebook、服务定义或 Orchestrator 派生路径签发。

消费方限定为 `addp-ontology` Tenant Runtime；每次消费必须携带精确 execution、attempt、lease_token 和同一 internal_task，System 复核完整授权引用、当前 running 租约、授权期限及用户当前授权版本与 Permission。Ontology 继续负责资源授权、撤回、摘要及激活基线；System 的成功响应不是投影 ready 凭证。内部任务授权不能消费 engine-accesses，也不返回 Infra 凭据。

### 5.2.2 Manager 剖析执行授权

完整配置摘要由 `common/execution.NewManagerProfileReadScope` 唯一计算，输入为持久 execution 不超过 512 KiB 的原始 JSON 对象。规范化只消除键序与空白差异，不经过浮点数反序列化，不承诺不同数字字面量具有相同摘要。Manager 私有配置语义仍由 owner 和 System 可信反查入口分别核验；共享工厂只提供纯值绑定，不作授权判断。

Manager 后台剖析使用物理源读取范围，不属于内部任务。其正式 Execution Authorization 在既有逐引擎 `read` scope 之外，必须不可变绑定完整 `source_read_scope`：已提交的 Manager 一次性执行配置摘要与 Provider 提供的规范完整 ReadSet。首期仅支持普通第一方／OAuth API User、单引擎、手工 `data_profiling`；不接受浏览器自报主体、来源集合或配置，不从当前 Meta 补造依赖。System 从精确 pending execution 可信反查配置及发起人，复核当前 `manager.data_profile.execute` 和 `manager.data_item.read` 的 Tenant Scope 与全部源 Grant／Explicit Deny，签发、封存和审计原子提交。

消费必须是当前 `addp-manager` Tenant Runtime，并绑定同一 running execution、attempt、lease_token、授权引用、完整配置摘要与本次不可变计划的相同 ReadSet。每次重新读取当前 IAM 和全部源规则；权限撤销、Explicit Deny、自然到期、执行配置或租约变化均拒绝。带物理源读取范围的授权禁止通过仅提交 Engine ID 的 engine-accesses 路径消费，不能从 Notebook、服务定义或父 execution 自动派生。正式源范围不是可缓存 Allow，Manager 仍负责同一 Provider 计划和本地 Security 的真实执行、响应与持久化边界。内部签发／消费底座与 HTTP／Manager 采样接通分别验证，未接通前生产采样继续明确拒绝。

固定 HTTP 签发入口为 `POST /api/v1/system/auth/execution-authorizations/manager-profiles`，只提交 `execution_id` 和可选整数秒 `expires_in`（省略为 900，最大 3600）；以两个 Manager 功能 Permission 为 all-of Guard，不开放通用 audience／效果选择，不借此获得通用执行授权创建能力。201 返回不可变授权 `id`、`execution_id`、`tenant_id`、固定 `audience=manager`、`source_read_scope` 和 `expires_at`，不返回用户凭据。Manager 必须校验响应与自己的真实计划及租户一致后才绑定执行。

固定消费入口为 `POST /api/v1/system/execution-authorizations/{id}/manager-profile-accesses`，只接受 `execution_id`、正数 `attempt`、`lease_token` 和相同 `source_read_scope`；以 Service 凭据、Tenant Context、固定 `addp-manager` Client 和 `system.execution_authorization.execute` 为 Guard，主体与租户仅从可信 AuthContext 获取。200 仅返回 `observed_at`，不返回源连接或通用访问租约。两入口拒绝未知字段、额外 JSON、query 参数、非法规范 ID 和超限正文，响应禁止缓存。客户端不跟随重定向、不缓存放行、不自动重试签发或消费，不向 owner 日志透出上游正文或凭据；400／401／403／409 与服务故障分别保留稳定错误语义。共享范围 DTO 与规范校验由 `common/execution` 唯一提供，不复制校验到 IAM、HTTP 与 Client。

### 5.3 Notebook 会话授权

Notebook Interactive Session 不复用 Agent Delegated Access Token。Develop 必须在 Session 创建的同步 BFF 调用栈内，用当前 User Bearer 请求 System 创建 Notebook Session Authorization，随后丢弃 User Bearer。System 保存的授权事实至少绑定唯一 authorization ID、Notebook Session ID、Develop Task ID、Tenant、Principal、Tenant Membership、Token Family、`authorization_version`、固定 audience `develop`、允许操作集合、签发时间、到期时间和撤销时间；不保存 User Token、Service Token、Engine ID 列表或连接信息。

后续操作由 `addp-develop` Tenant Service Principal 使用自身 Service Access Token 和 authorization ID 消费。Engine Catalog 请求在一次调用内完成授权校验和 `EngineCatalogProvider.ListChildren`。每次查询或扫描则提交新的 execution ID、单个 Engine ID、固定 `read` 效果和短 TTL；System 在一个事务内重新校验用户来源并创建绑定该 Notebook Session Authorization 的独立 Execution Authorization，随后在同一控制面请求返回执行期 Engine Access。System 只给该 Runtime Role `system.notebook_session_authorization.execute`，不得授予通用 `system.engine.read`；authorization ID 是引用，不是 Bearer Credential。

授权有效期不晚于 Notebook Session，到期后不能刷新；普通 Access Token 与 Refresh Token 在同一 Token Family 内轮换不影响现有 Session。Token Family 撤销或重用、Membership/Role/资源规则变化、`authorization_version` 变化、Session 显式关闭或到期时必须拒绝后续消费并取消活动查询。Session 或 Token Family 撤销必须联动撤销其派生且仍有效的 Execution Authorization；标准 Engine Access 租约复核也必须沿来源外键回查 Session 与 Token Family，不能只检查 Principal 和 Membership。长扫描只在短租约边界回查 System，不按每个 Arrow batch 往返；租约失效后关闭 Cursor。Develop 重启后本地 Session 与 authorization ID 一并丢失，Kernel 入口先 fail-closed。

### 5.4 Owner Resource Grant 与 Asset 履约

Resource Grant 的权威事实固定保存在业务资源 owner，不进入 System IAM、AuthContext、Catalog 或 Asset 的中央 ACL 表。owner 使用结构化 `ResourceAccessRule` 表达资源、主体、Permission、`allow | deny`、有效期、来源和撤销事实；`allow` 构成 Resource Grant，`deny` 构成 Explicit Deny。运行时必须在当前 Tenant 内同时校验 Role Permission、资源状态和 owner 规则，Deny 优先于 Allow。

物理源数据与专业业务对象必须区分：同一外部表、文件或对象的访问规则，由 System 的引擎访问控制领域唯一维护，各访问模块执行这一权威规则；这不是 System IAM 接管全平台业务 ACL。模型、质量方案、服务、应用等专业对象的授权仍在对应 owner。源数据表级贯通属于已确认、待实现的职责扩展，不能将当前引擎级 Execution Authorization 当作表级授权完成证据。详细规则见 5.5。

Asset 只拥有申请、审批、授权期限和跨 owner 履约状态，不拥有最终资源访问判断。批准申请后，Asset 在自身事务中创建 `pending` 授权履约事实，再由可恢复 reconciler 使用 `addp-asset` Tenant Service Access Token 调用 owner 的幂等 Resource Grant Runtime API；只有 owner 返回并持久化有效规则后，Asset 授权才进入 `effective`。调用失败时保持 `pending` 并重试，不能把审批成功或 Asset 本地记录存在伪装为资源已经可访问。

撤销和过期统一先把 Asset 授权推进为 `revocation_pending`，再幂等撤销 owner 规则；owner 确认撤销后才进入 `revoked`。`pending | effective | revocation_pending | revoked` 是唯一履约状态，不保留 `is_active`、软授权、Credential、专属 Token 或 owner 实时反查 Asset ACL 的并行路线。Asset 的来源授权 ID 是 owner 幂等键；重复创建或撤销必须收敛到同一规则和同一最终状态。

当前第一条正式 owner 适配是 Workbench Data Application：

- `application` 类型 Asset 只能引用唯一一个 `workbench/data_application` CatalogEntry，不接受手工 URL；
- Asset 解析 Catalog 当前来源身份后，向 Workbench 写入绑定当前 User 与 `workbench.data_application.execute` 的 Allow Rule；
- Workbench 创建者继续按所有权运行，其他 User 必须命中当前有效 Grant；
- Workbench Grant 只允许打开应用配置，Application Component 的真实数据查询仍由 Service 以当前 User Bearer 独立执行 `service.data_read.execute` 和 Service Resource Grant / Policy；
- 尚未实现 owner 履约适配的 Asset 类型不得审批为 effective，也不得回退为软授权。

Portal 是同步 BFF 和消费界面，只展示 Asset 返回的履约状态；只有 `effective` 的 Data Application 资产显示规范 `/data-apps/{application_id}` 打开入口。Portal 不保存 owner 资源 ID、Grant、Token 或运行配置。

### 5.5 目录责任、业务授权决定与源数据授权

正式准备与首次受理契约（2026-10-03 已确认）：

- Catalog `POST /entries/:id/sharing_fulfillments` 仅接受当前 Tenant User，要求条目可见、`catalog.entry.read` 与 `system.engine_access_fulfillment.create`。请求只提供 `request_id`、`decision_id`、预期 `requirement_version`；办理人从当前 AuthContext 派生，机器调用主体通过 System 权威 AuthContext 查询取得，目标与共享参数只从 Catalog 不可变决定取得。事务外先观察 System 当前人类资格，事务内复核可见性、期限及原 owner 依据，提交完整 pending 请求后才发送。准备本身不是受理或 Grant。
- Catalog `POST /runtime/sharing-fulfillments/:request_id/basis` 只允许独立 `addp-system` Tenant Service 与不可委托、不可租户定制的 `catalog.sharing_fulfillment.read`。只反查精确已提交且尚待核清的持久请求，匹配完整绑定并复核原来源、业务负责人责任连续性；返回确认人的原身份引用，不返回用途正文或资格缓存。请求体不是证明；System 在本地 IAM、请求、目标锁之前反查，不在锁内跨模块联网。
- System `POST /runtime/engine-access-fulfillments/:request_id/accept` 只允许当前 `addp-catalog` Tenant Service 与 `system.engine_access_fulfillment.execute`。首次受理要求可信持久依据、当前原办理人的严格授权版本及独立办理 Permission、确认人的当前 IAM 资格、接收主体、引擎管理委派和精确目标批准要求版本。全部 Principal 去重按 ID 升序锁定，先于 Membership/Tenant/请求/目标；提交前按数据库墙钟重核自然到期。
- 同参既有结果优先走历史核清，不要求旧业务依据仍有效，不刷新五分钟窗口。首次请求失败或响应丢失不释放 Catalog pending 保护；已有 resolve/close 串行核清才可解除。未配置独立 System 反查凭据只拒绝新受理，历史恢复保持可用。当前回执只表示 `accepted|closed`，没有 Grant 或内容访问结果；实际授权仍须后续独立交付。
- 原办理请求只读找回（2026-10-03 已确认）使用 Catalog 人类 GET，不直接开放 Runtime 给人：仅本人原办理、当前条目仍可见、具备条目读取和独立办理 Permission，并按每个原请求的引擎核验当前管理范围。历史授权版本仅匹配原绑定，不能替代当前资格；同部门、项目组或编目维护权不开放他人请求。分页列表不复制受理状态；详情以持久完整绑定向 System 只读反查，`pending` 只表示权威未找到，依赖失败仍报错。查询不提交、关闭、写核清、延期或授予 Grant；显式 POST 重试另沿用原请求编号及原参数。

本节定义源数据授权的统一职责与消费契约。已实现的办理和读取范围分别以本规范、Catalog 与 Manager owner 文档为准；签发历史不替代当前访问裁决，确定性门禁不替代真实跨模块验收。剩余工作见[企业资源目录能力专题](../next/ADDP企业资源目录能力专题.md)，不保留旧租户级放行作为兼容路径。

#### 5.5.1 事实与职责分开

- Catalog 唯一维护条目的责任部门、业务责任人、数据管理员和技术维护者。它们是资源责任关系，不自动成为 IAM Role、授权办理资格或数据访问 Grant；编目提交人只表达审计行为，不等于技术维护者。
- 首次明确保存经 System 核验有效的 `business_owner` 即视为建立过业务责任，不等待完成编目；只分配责任部门、业务域或其他责任不算。建立依据只由 Catalog 随责任写入同事务维护，撤销编目、移交、失效或恢复不允许清除；历史证据不足时保持未知。建立依据不代表当前责任完整、具备业务确认资格、已经启用 Catalog 批准方式或已有实际访问规则；也不替代 System 的权威批准要求。
- 业务责任人确认资源共享的业务合理性；数据管理员协助编目、界定范围和协调质量；技术维护者校验技术可执行性。部门、项目组、业务域归属和协作集合不自动授予实际数据权限。
- 业务决定必须绑定精确资源、接收主体、动作和期限，记录真实确认人及可核验的责任依据。办理方验证当前资格和决定依据后，在唯一授权维护方写入规则。页面显示“已确认”不得代替“授权已生效”。
- 2026-10-02 已确认：业务决定中的确认人授权版本是当次确认的审计事实，不是永久资格快照。首次受理须实时核验确认人仍是有效 User、仍使用原 Tenant Membership、原业务负责人责任关系仍有效，并具有当前业务确认所需的 `catalog.entry.read` 与 `catalog.sharing_decision.create`；无关角色调整仅使授权版本变化，不要求重新确认。权限撤销或自然到期、账号／成员失效、原责任移交等拒绝新受理，同账号重新接任也不复活旧决定。办理请求的原操作者仍严格匹配其绑定授权版本，不将确认人的规则推广到办理人；关闭未受理请求和历史结果核清不重新要求确认人仍有效。
- 普通只读共享允许同一账号兼任业务确认与授权办理，但两方面的功能、当前责任资格和目标管理范围必须分别满足，两步分别核验并留痕。2026-10-01 已确认：允许有效业务责任人显式确认向自己或自己当前所在的项目组共享普通只读；必须记录自用或项目组受益关系，不因指定责任而自动得权，也不通过没有独立申请单规避记录。此规则不允许敏感原值例外、写入、DDL 等受独立复核约束的动作自批，不放宽既有 Security 原值访问复核、Break-glass 或平台三员分立规则。
- 每次普通只读共享必须显式选择 `expiry_mode=at_time|until_revoked`：指定到期时间时，`expires_at` 必须是未来绝对时间；长期有效（直至撤销）时，`expires_at` 必须省略或为 null。遗漏模式、未知模式或模式与日期矛盾均拒绝，不将漏填解释为永久，不使用远期日期或数据库 infinity 伪装长期有效。指定到期不自动续期，首版不设全平台统一的固定天数上限；长期有效只表示规则没有时间终点，当前身份、组织成员关系、功能权限、撤销、Explicit Deny 和安全条件仍持续约束实际访问。本规则不放宽临时接入只读、引擎管理委派、敏感原值例外、写入或 DDL 的期限，也不把五分钟自动办理窗口当作访问有效期。
- 业务共享确认与源读取授权办理使用独立功能 Permission，不复用编目维护或引擎管理委派 Permission。通过既有 Role Assignment 显式分配，可选择专门的内置职责模板或自定义 Role；不默认授予通用管理员、不随责任指定自动补权，也不为未获角色的账号回填。2026-10-08 的引擎授权管理员模板扩展仅改变其已有显式 Role Assignment 的功能组合，撤销旧会话，不创建内容 Grant。Permission 随真实接口及其守卫、审计、测试和 migration 一起发布，不先启用无人消费的权限；本条不表示已有表级办理入口。
- 2026-10-02 已确认：不同于原确认人的历史读取，授权办理人读取业务决定摘要须同时满足当前 Catalog 条目可见、独立源读取授权办理 Permission 和目标所属 Engine 的当前有效管理委派。独立人类权限使用 `system.engine_access_fulfillment.create`，仅允许显式 Tenant Role Assignment；机器核清的 `.execute` 不满足它。System 的 `GET /engines/:id/access_handling_scope` 以当前 User Token 核验人类资格，仅返回当前身份及引擎范围的观察结果，不产生委派、受理、Grant 或可复用资格凭据。
- Catalog 的 `GET /entries/:id/sharing_decision_candidates` 在上述交集内提供分页候选摘要，只包含决定引用、完整目标、接收主体、动作、有效期和确认信息，不返回业务用途正文或全租户历史。候选以 Catalog 当前来源、原责任关系、条目及期限过滤；确认人的实时 IAM 资格、接收主体和批准要求仍在正式提交时重新核验。选择时的摘要不代表 System 已受理，也不保证后续仍可办理。确认历史复核使用独立确认权限，并按本人／当前业务负责人范围过滤，不要求办理资格。System 的 Catalog 模式入口也不得借机器核验绕过人的条目可见性；明确退出或不使用 Catalog 的独立批准入口不新增 Catalog 可见性前提。
- 2026-10-03 已确认：本条目当前业务负责人具备 `catalog.entry.read` 与独立 `catalog.sharing_decision.create` 时，可通过条目级共享接收方候选选择同 Tenant 的有效账号或项目组，包括本人未加入的项目组。只暴露最小名称、编码、稳定 ID 和状态，不提供成员、角色或源数据；普通条目读取不获得全 Tenant 项目组枚举。办理人若需要接收方名称，只能解析已经有权读取的候选决定指向的主体，不据此取得全 Tenant 候选枚举。组织事实仍归 System，候选读取不创建决定、受理或 Grant。
- Catalog 可提供业务入口，System 引擎管理必须能够独立办理；两个入口使用同一权威写路径。System 不维护可独立编辑的业务责任副本，Catalog 不维护源数据 Grant 副本。
- System 针对精确源数据目标持久维护“源数据授权批准要求”，表达新授权是否需要 Catalog 的业务确认。该事实归引擎访问控制领域，包含目标、当前批准要求、并发版本及变更审计，不包含可编辑的 Catalog 责任人、部门或历史副本。Catalog 唯一维护业务责任、业务决定和当前业务核验；System 唯一保存正式受理回执及实际 Grant，不在 Catalog 另存第二份可编辑接受状态。

#### 5.5.2 接入阶段与 Catalog 独立性

引擎授权管理与委托的契约：

2026-10-08 已确认：授权管理员在当前 Tenant Scope 的有效 `system.engine_access_delegation.create` 范围内可以直接办理，不必向自己创建委托；仍须分别具备本次配置、签发、撤销或业务办理的功能 Permission。该管理员权限目前为租户范围，不能跨租户。受托办理员以目标引擎的有效委托取得管理范围，不能转委托。两种管理资格在同一引擎管理范围核验中裁决，均不产生数据读取 Grant、不代替 Catalog 业务批准。角色、成员或委托到期和撤销在提交前重新核验，不能靠页面加载自动补权。

- 委托管理资格分别使用 `system.engine_access_delegation.create/read/revoke`，只接受当前 Tenant 的 User 和 Tenant Scope。产品提供专职内置 Role `tenant.engine_access_delegation_administrator`（引擎授权管理员），组合委托管理、引擎结构与批准方式读取／首次配置、接收方候选以及普通只读 Grant 签发／读取／撤销权限；通过既有 Role Assignment 显式分配，也允许按既有规则定制 Role。不向其他内置管理员 Role 加入这些权限，不创建新的账号分配。该 Role 不包含业务共享确认、Catalog 办理或 Explicit Deny，也不产生委托记录或源数据读取权。受托办理员不具有转委托能力；“无需委托自己”只取消冗余管理记录，不取消操作 Permission、精确目标和批准依据的校验。
- 每份委派只绑定一个当前 Tenant 的有效 Engine 和一个有效 User Tenant Membership。管理范围为该 Engine，不包含内容读取、写入、DDL 或转委派；后续访问规则仍须逐项选定逻辑资源并验证业务依据。
- 创建必须提供非空原因与未来的 `expires_at`；不使用默认期限、不创建永久委派，期限不得超过目标 Membership 的有效期。尚未确认期限上限，不据此扩展普通读取 Grant 的期限。新建资格只面向具备实时目录能力的引擎，不根据引擎类型猜测能力。
- 唯一管理路径为 `GET/POST /api/v1/system/engines/:id/access_delegations`、`GET /api/v1/system/engines/:id/access_delegations/:delegation_id` 和 `POST .../:delegation_id/revoke`。撤销要求正整数 `version` 与原因，冲突返回 `resource_version_conflict`；无更新、恢复、续期或物理删除路线。
- 持久状态为 `active/revoked`，当前状态还需结合数据库时间、Tenant、Principal、Membership、Engine 生命周期判断；同一 Engine、同一 Membership 不允许重叠委派。撤销后历史只读，续期必须创建新记录。
- 写入在同一事务中复核当前操作账号的有效功能权限、目标身份和范围，保存委派与安全审计并推进接收主体授权版本、撤销旧会话。该接口不连接业务源库，也不向 Catalog 写入副本。
- 本阶段仅落地管理委派，不把存在委派视为任何源数据访问的 Allow；源读取规则及真实消费者的表级校验另行贯通。

- 引擎登记、实时发现、Meta 扫描和内容访问是不同动作。System 的实时 Engine Catalog 允许在 Meta 扫描前选择资源；发现本身也必须受控，不能凭实时接口泄露未授权名称和结构。Meta Worker 的服务身份不能扩张任务授权，读取文件头或内容样本也不得借结构发现权限绕过访问检查。
- 接入 Engine 不构成修改源库的授权。登记、发现、扫描和只读访问不得擅自安装触发器、创建源端角色、写入身份记录或执行其他非只读操作；写入、建表等能力必须有用户针对对应操作及资源范围的明确授权。源数据访问规则保存在 ADDP 的权威方，不因登记连接自动向源库写入规则。
- 接入授权主体必须具备有效授权管理员资格或目标引擎的显式委托，并具备对应功能 Permission；登记人、角色名称或连接成功不能推导初始管理资格。批准依据以 System 对精确源目标的明确安排为准：未配置时由用户在授权表单中显式选择，不以缺少责任或缺少 Catalog 默认直接批准；已要求 Catalog 确认但责任失效时，先修复责任或明确交接，不提供“恢复接入阶段”的旁路。
- 临时接入规则必须填写未来的绝对到期时间，不提供期限默认值或无限期选项，不设全平台统一的固定天数上限。长期独立批准使用 5.5.6 的显式有效期规则，不套用临时接入期限。只有明确启用 Catalog 批准方式后，新共享或扩大权限才必须经当前业务责任人确认；仅保存业务负责人不改变已明确的独立批准安排。
- Catalog 是可选的企业资源关联与责任协同模块，不成为引擎发现、扫描或源数据授权的启动前提。部署、注册、试用 Catalog 或保存业务责任本身均不切换批准方式，不能据此永久阻断整个平台、其他 Tenant 或无关资源的授权。System 已明确独立批准时，不要求 Catalog 在线或责任历史查询；已明确 Catalog 批准时，不因服务失联、停用开关或查询失败自动退回独立批准。已有有效规则继续支持合法访问；需要新鲜业务依据而无法核验的新办理仅在对应范围拒绝，不把 Catalog 可达性加入其他模块 Ready 条件。
- 用户明确决定撤销 Catalog，与模块暂不可用是不同情形。平台必须支持用户停止使用这一可选模块，不得仅因过去部署过而永久影响 Transfer、计算等正常能力；退出不等于清除责任历史、自动授予全租户访问或取消功能权限与精确资源权限检查。2026-10-01 已确认：退出须使用独立的退出／治理功能 Permission，明确精确资源范围、有效的后续批准安排和原因，并由 System 在同一权威事务中变更批准要求及追加审计；不以模块停用开关执行这一动作。具体首次启用资格、可信交接协议及接口仍待落实，本条不表示已有退出接口。未经授权的故障降级不能冒充主动退出。
- 不要求使用者最终部署 Catalog。长期不部署或完成明确退出后，System 的独立批准入口使用独立批准功能 Permission 与当前有效的引擎授权管理委派共同判定资格；委派不代替批准 Permission，两者也不授予内容访问。独立批准入口与 Catalog 办理入口共享同一源数据 Grant 写路径。临时接入只读的期限建议不能直接套用为这些长期部署的全部授权期限。

2026-10-07 已确认：共享历史读取不等于办理资格。具备 `catalog.entry.read` 与独立 `catalog.sharing_decision.create` 且条目当前可见的原确认人，可查看本人确认对应的办理及签发历史；本条目当前有效业务负责人可复核全部历史共享。仅编目权限、部门／项目组关系不产生此资格。Catalog 在人类读取守卫后按持久完整绑定只读查询 System，不能借 Runtime 身份绕过人的范围过滤。查询不签发、不撤销、不核清，不要求复核者同时具有办理 Permission 或引擎管理委派；签发历史不是当前访问 Allow，后者仍由 System 的实时源规则与功能权限、安全约束共同裁决。

#### 5.5.3 责任变化与访问规则生命周期

业务责任人更换或其责任失效后，旧责任立即不再支持新的业务确认。仅变更责任人不批量撤销已生效的访问规则；既有规则继续按其原范围、有效期、当前主体资格和撤销事实判断，不因移交自动扩大、续期或绑定新对象。

内部签发结果找回与签发写入分开：只读查询必须匹配原办理编号和完整不可变绑定，且在返回结果或绑定冲突之前核验当前机器身份、Token 与办理 Permission。未找到、尚未签发或已关闭且无签发记录，仅表示本次读取未观察到签发历史，不关闭请求、不自动签发、不推断当前允许访问。历史读取使用自有只读事务，不暴露调用事务自身未提交的记录，不取请求／精确目标仲裁锁；在当前资格事务开始前结束读取，避免持有资格锁再占用第二条连接。原结果存在时返回原时刻，不复核历史人员、旧窗口或后续批准安排来改写历史。后续新签发仍在写事务内重新核验与查重，不把只读未找到当成签发许可。

2026-10-03 已确认：源数据撤销首版仅收回显式指定的一份 Grant，不按相同目标或接收主体批量撤销其他独立授权。同一账号仍可能通过另一份有效个人或项目组 Grant 访问；撤销个人 Grant 不删除项目组 Grant、不影响其他组员、不结束组织成员关系、不停用账号，也不修改 Catalog 的编目责任。若要求对某主体的精确资源动作无条件禁止访问，应另行显式建立优先于 Allow 的 Explicit Deny；撤销 Grant 不隐含创建 Deny。指定 Grant 撤销及 Deny 创建接口的契约见下文；执行消费者仍待实现，不把规则事实的写入作为完整访问裁决已经生效的证明。

2026-10-04 已确认：源数据 Explicit Deny 由 System 引擎访问控制领域唯一维护。建立和解除分别要求独立功能 Permission，并与当前有效的目标引擎管理委派取交集；两项功能权限不能相互推导，也不能由指定 Grant 撤销权限推导。操作者须为当前 Tenant 的有效 User，账号、租户成员关系、Token 和授权版本均须有效。Permission 按当前 Tenant Scope 与有效时段核验，Department／Project Group Scope 不自动扩大为整租户管理资格。租户通过现有自定义 Role 和 Role Assignment 显式安排人员，不默认赋予内置管理员，不因成为 Catalog 编目维护者、业务负责人、数据管理员或技术负责人而自动取得。

拒绝规则绑定明确主体、精确源数据逻辑目标与动作，不借此停用账号、移除组织成员关系或禁止全租户。对某个人建立 Deny 时，其通过项目组取得的同一目标、同一动作 Allow 也不能放行；解除只移除该份禁止依据，不创建、恢复或续期 Grant，仍须重新判断其余 Deny 和当前有效 Allow。规则及操作审计归 System，不在 Catalog 保存可编辑副本，不依赖 Catalog 在线。期限、建立和解除命令按下述契约落实；执行消费者仍待贯通。真实命令发布时同步登记 Permission、向前迁移和测试，不预先启用无人消费的权限。

2026-10-04 已确认：源数据只读 Explicit Deny 的期限必须显式选择 `expiry_mode=at_time|until_revoked`，沿用精确只读访问规则的期限值契约，不将漏填解释为永久。指定到期时必须提供未来绝对时间；长期有效时不提供日期，不使用远期日期或 infinity。规则自然到期仅结束这份拒绝的时间效力，不生成 Allow、不恢复 Grant，也不延长操作者的管理委派。创建重试保留原期限与原建立时间，已到期的历史不能通过同参数重试重新生效。

创建的唯一命令为 `POST /api/v1/system/engines/{id}/access_denies`，使用独立 `system.engine_access_deny.create` 与有效引擎管理委派。首版只接受精确叶子路径、`read` 动作以及当前 Tenant 的有效 User／Department／Project Group 主体，与既有普通只读共享的主体值契约一致，不支持全租户、递归目录、写入或 DDL。客户端必须提供非零 UUID `deny_id` 作为本次命令编号，并提供规范化原因；Tenant 与操作者来自当前认证上下文。数据库建立时间、不可变规则及高风险审计同事务提交，等待后及提交前复核当前资格、接收主体和未来期限。停用引擎仍允许建立禁止规则，不连接源端，也不依赖 Catalog 或实时目录能力。

同一编号、原操作者／成员关系及全部原参数的重试，在当前操作者资格有效时返回原事实，不重复审计或延长期限；历史接收主体后续失效不改写原规则。相同 Tenant／Engine 内同编号异参返回 409；其他 Tenant／Engine 的编号隐藏为 404，当前资格核验先于历史暴露。建立命令及其历史不代表执行侧已经拒绝源内容读取。

2026-10-04 已确认：唯一解除命令为 `POST /api/v1/system/engines/{id}/access_denies/{deny_id}/release`，正文仅接受必填原因，使用独立 `system.engine_access_deny.release` 与当前有效引擎管理委派。具备此交集资格的当前 Tenant User 可以解除其他人建立的同引擎规则，不要求仍是原建立人；停用引擎或历史接收主体失效不阻止解除，不访问源端或 Catalog。追加唯一不可变解除事实及高风险审计，不修改原规则或任何 Grant。每次先核验当前操作者资格，再暴露历史；跨 Tenant／Engine 或不存在规则返回 404，已解除后的异操作者、异成员关系或异规范化原因重试返回 409 `engine_access_deny_release_conflict`。

`at_time` Deny 自然到期后首次解除返回 409 `engine_access_deny_expired`（拒绝规则已到期，无须解除），不追加解除事实或解除成功审计。服务在目标锁等待后、写入前及审计后提交前按数据库墙钟检查期限，数据库插入触发器拒绝过期插入及回填时间。到期前已经解除的同操作者、同成员关系、同规范化原因重试优先返回原解除记录，即使原期限已到或接收主体失效；当前操作者资格仍须有效。`until_revoked` 无自然到期日，可按相同资格解除。解除历史不等于当前允许访问，原五分钟办理窗口不限制解除。

撤销不得删除或改写原签发历史及受理回执。原办理编号和原参数的签发重试只恢复原签发事实，不清除撤销、不再次授予；签发历史与当前授权是否有效必须分别读取和判断。撤销事实及对应审计应由 System 引擎访问控制领域在同一事务提交，不在 Catalog 保存第二份可编辑源数据授权。操作者资格须与编目责任、业务共享确认和授予新权限区分，随真实撤销接口发布独立 Permission，不预先启用无人消费的权限或默认赋权。

指定 Grant 撤销要求当前 Tenant User、有效账号与租户成员关系、管理资格（授权管理员或有效受托办理人）及独立 `system.engine_access_grant.revoke` Permission 同时成立；不由历史办理人、业务责任人或受益人身份自动取得资格。引擎停用或失去实时目录能力不阻止收回旧 Grant，撤销不访问源端或依赖 Catalog 在线；新授予仍要求引擎启用及原有办理条件。账号、Token、Permission 或管理委派失效时仍拒绝，等待仲裁锁后和提交前重新核验。

唯一撤销命令为 `POST /api/v1/system/engines/{id}/access_grants/{request_id}/revoke`，用当前有效 Grant 的 UUID 定位整条授权关系，正文仅包含必填原因；操作者和 Tenant 来自当前认证上下文。在同一事务收回相同精确目标、接收方和动作的全部有效记录，追加不可变撤销事实、完整收回编号集合与高风险审计，不更新原 Grant。每份 Grant 最多一份撤销事实；同一当前主体、成员关系与相同规范化原因的重试只返回原收回结果，不重复审计或收回后来新授予；不同参数返回 409。个人与组织是不同关系，不能隐式扩大撤销。不存在或跨 Tenant／Engine 的 Grant 返回 404，但必须先通过当前操作者资格核验，不通过错误泄露其他范围的历史。

2026-10-03 已确认：`at_time` Grant 自然到期后不允许首次主动撤销，返回 409 `engine_access_grant_expired`（授权已到期，无须撤销），不追加撤销事实或撤销成功审计。服务在取得目标锁后、写入前及审计后提交前按数据库墙钟检查原授权期限；数据库插入触发器同时拒绝过期插入和回填撤销时间。到期前已成功撤销的同主体、同成员关系、同规范化原因重试，即使原授权后来到期，仍返回原撤销记录，但当前操作者资格必须有效。`until_revoked` 无自然到期日，可按同一资格规则撤销；原五分钟自动办理窗口不作为撤销期限。

新责任人复核既有共享的合理性，必要时由具备办理功能与目标管理范围的主体落实撤销；接任不自动取得办理资格。尚未被正式接受办理的业务决定必须重新核验当前责任，不能使用旧责任资格继续办理。旧责任人作为使用者持有的独立 Grant 不因责任移交当然失效，但账号失效、当前成员关系不再匹配、到期、撤销或 Explicit Deny 仍按相应规则拒绝访问。

责任资格以 Catalog 最终核验当前责任与精确业务决定后、System 持久提交本次受理回执为分界，而不是以业务责任人先前点击确认或 Catalog 本地写入“已接受”为分界。2026-10-01 已确认：Catalog 管业务批准，System 管正式受理与实际授权。接受动作与责任变更须形成可验证的先后顺序：移交先完成的，旧负责人的确认必须由新负责人重新确认；接受先完成的，之后移交不单独阻止同一次、同参数办理继续完成。接受不得支持另一份目标、接收主体、动作或期限，也不支持新共享、扩大范围或续期。

业务确认、正式接受办理与 System 写入规则实际生效是三个不同事实。Catalog 只维护责任、业务决定和当前业务核验，不保存源数据 Grant 或可编辑受理回执副本；System 写规则时仍独立核验当前办理功能、目标管理委派和接收主体有效性。System 回执只保存本次精确办理编号、目标与参数绑定、Catalog 决定引用、批准要求版本、服务端受理时间、原截止时间和审计依据，不复制业务责任或业务决定正文，也不充当访问凭据。Catalog 展示及未知结果恢复读取 System 原回执，不以 Catalog 后续第二次提交建立正式受理事实。接受失败不产生有效依据；接受后写规则失败或响应丢失，只能按同一次办理的幂等契约处理，不得自动扩大或重复授权。

已弃用条目禁止新的业务共享确认与正式接受办理。弃用前已经正式接受的同一次、同参数办理，仍按原自动办理窗口继续，不因弃用刷新依据或延长期限；是否接受在弃用之前，必须由 Catalog 当前事实与 System 受理提交的协调协议证明，不能只比较客户端时间或两个服务的时钟。已有访问规则不因目录弃用自动撤销，必要撤销仍由具备独立办理资格的主体通过 System 权威写路径完成。撤销弃用不自动恢复旧的未接受决定或过期接受依据。

自动办理窗口从 System 正式受理回执的权威时间起为 5 分钟：指定到期时间时，截止时间取“受理时间加 5 分钟”与“本次拟授权的绝对到期时间”的较早者；长期有效时，截止时间仍为受理时间加 5 分钟。正式受理成功以回执事务提交为准。窗口仅约束系统尚未生效的规则写入，不限制人员填写、等待业务确认或审核的时间，也不把接收方访问期限改成 5 分钟。时间由 System 的数据库时间确定，参数重试、网络重发和责任移交都不得刷新窗口、改变有效期模式或顺延拟授权的到期时间。System 必须在权威写入事务中检查截止时间；排队或等待锁期间跨过截止时间也不得写入新规则，不能只在 HTTP 请求到达时检查。

窗口到期且尚未生效的办理，必须重新核验当前责任并重新正式接受，不能延续旧接受依据；当前责任已变更时必须由新负责人重新确认。已经成功写入但响应丢失的同次、同参数请求，重试只查询并返回原办理结果，不再次授权、不延长权限，不因接受窗口到期将已有结果误判为未办理。原规则当前是否仍有效由 System 按到期、撤销和主体资格等事实判断，原办理成功结果不代替当前访问判定。

System 的统一 Grant 保存一次签发的精确目标、接收主体、动作、期限和操作者来源；`request_id` 是签发命令的唯一编号，不要求所有命令均有 Catalog 回执。`approval_mode` 区分批准依据：Catalog 签发必须通过 `catalog_request_id=request_id` 引用已提交的精确 `accepted` 回执，数据库从该不可变回执提取规则参数；独立签发不建立或伪造 Catalog 回执，必须引用显式独立批准要求版本。Grant 是源数据规则的唯一消费事实，读取和撤销不再通过回执拼接规则；回执只证明 Catalog 链路的正式受理与原窗口，不复制 Catalog 责任、业务决定或 IAM Role/Permission。已有签发通过前向迁移保留原编号、签发时间、参数及撤销关系，不重新授予或续期。

Catalog 签发的新写入仍沿用原办理人的不可变来源，核验当前身份及授权版本、独立办理 Permission、引擎管理委派与接收主体；不重新核验业务确认人的责任或读取新的批准要求来否定此前已受理的同次办理。资格锁先于原请求和精确目标锁，锁内再次查询签发结果；原结果已经存在时只恢复历史，不重新签发或延长期限。签发和高风险审计同事务提交；写入时及提交前按数据库墙钟复核原窗口，任何失败都回滚两者。独立签发的公开命令与页面须另行接通，不能把统一存储完成宣称为独立授予已经可用。

该签发底座仅面向 Catalog 链路已经正式受理的请求，不办理独立批准或初始接入授权；不存在回执、未部署 Catalog 或 Catalog 不可用都不能触发自动签发。唯一 HTTP 签发入口为 `POST /api/v1/system/runtime/engine-access-fulfillments/:request_id/grant`，纯只读签发历史查询为同一路径下的 `POST /grant/resolve`。两者复用现有 `system.engine_access_fulfillment.execute`，限定当前 `addp-catalog` Tenant Service，完整请求绑定须匹配原回执和认证调用主体，不接受浏览器身份、客户端租户或自报批准；新签发仍须独立核验原人类办理权限与委派。

签发及同参重试统一返回 200，响应仅含字符串 UUID `request_id` 和数据库时间 `granted_at`；查询返回 `found`，找到时附相同的 `grant`，未找到时省略 `grant`。查询采用只读 POST，是为了在请求体中完整匹配结构化绑定，不能枚举他人的记录；未找到不表示关闭、失败或允许访问，也不创建受理、签发、撤销或业务成功审计。窗口到期且未签发返回 409 `engine_access_grant_window_expired`；原请求已关闭返回 409 `engine_access_fulfillment_closed`；绑定冲突返回 409 `engine_access_fulfillment_binding_conflict`。这些稳定错误码不依赖界面语言；当前身份或权限失效仍优先拒绝，不借冲突响应暴露他人历史。已经签发的原参重试只恢复历史，包括之后已撤销或自然到期的历史，不重新激活授权。

跨模块签发历史 DTO 唯一放在 `common/authorization`，System 从本域权威记录投影，`common/client.SystemFulfillmentClient` 通过现有 Tenant Service Bearer 消费。客户端先校验原编号与完整绑定，响应须明确包含合法签发编号与非零时间；查询只有明确 `found=false` 且省略 `grant` 才表示未找到。缺少字段、结果编号不符、无效响应、401／403／404、绑定冲突或通信错误不得降为未找到，也不得在客户端自动执行签发、关闭或更换参数；错误分类沿用 `TenantAPIError` 的 HTTP 状态及稳定错误码。不依据客户端当前时间否定已提交的历史签发。

2026-10-04 已确认：Catalog 在现有 `fulfillment_checks` 增加一次性 `grant_reconciled_at`，只表示本次自动签发处理已核清。它必须晚于或等于本地受理核清时间 `resolved_at`，不保存签发成功／失败状态、System Grant、受理时间或窗口截止时间；受理核清后仍解除原责任与来源保护，不等待自动签发。两个标记独立且不可清除或改写；旧记录的新标记保持 NULL，由同一恢复流程向 System 查证，不伪造已经处理。

前台受理核清后及后台恢复调用同一个签发续办命令。后台同时扫描未核清受理及受理已核清但签发处理未核清的原记录，保持既有一分钟发送宽限、100 条游标分页与 30 秒单项边界，不门控 Ready。受理未知时沿用明确未找到才关闭的恢复流程；受理已核清后仍查询原 System 回执，不重新接受、不核验为新业务批准。`closed` 可终结自动调度；`accepted` 先查原签发历史，明确未找到才同参签发。原签发历史存在，或 System 签发明确返回原窗口到期／原请求已关闭，才填写自动处理核清时间。丢响应和其他通信、身份、权限、解析或绑定错误保持未核清；重试先查历史，不换编号、不补参数、不刷新窗口。任何远端调用都在 Catalog 事务和行锁之外，本地终结按条目→办理记录短事务幂等写入。

签发记录只证明这次精确规则写入，不证明当前仍可访问；Catalog 受理响应及历史 GET 保持原契约，不新增 Grant 副本或在 GET 中触发续办。Explicit Deny 和执行侧当前主体裁决尚未贯通前，不得把受理成功或签发历史展示为数据访问已生效。

不得把“没有有效业务责任人”设计成“无人能够发现并修复责任”。Catalog 的租户范围修复通道复用显式授予的 `catalog.entry.read`、`catalog.inventory.read` 和 `catalog.entry.update`，不要求修复者仍属于失效的原责任部门，也不将修复者自动登记为业务责任人或授予源数据访问、业务确认及授权办理资格。已认证条目仍须另具认证权限先撤销认证，不能绕过冻结门禁；已弃用条目允许仅移交责任，维持弃用和其他业务事实冻结。撤销弃用另需弃用权限，只回到已编目，不自动恢复认证、数据授权或资产发布。

租户必须显式安排有效治理账号及相应 Role Assignment，不能只定义一个未分配的角色或凭“管理员”名称假定有人可处理。已初始化的有效 Tenant 复用现有保护通道：内置 `tenant.administrator` 明确包含上述修复权限及认证权限，System 对最后一个有效租户管理员的主体、成员关系和角色分配维护已有保护；Catalog 仍逐项检查实际 Permission，不按角色名放行，也不新增兜底角色、自动补权或身份事实副本。该保护不保证每个自定义治理角色都始终有人值守，不保证人类及时处理任务，也不绕过 Tenant 停用或条目冻结状态。

#### 5.5.4 逻辑资源目标与执行约束

源数据授权针对 ADDP 中登记的逻辑资源，而不是源库对象的每一次物理创建。在当前 Tenant 边界内，复用 Engine 身份和完整结构化 EngineCatalogPath 确定精确目标；不能只授权一个 Engine 就允许所有表，也不能只用 CatalogEntry 或 Meta Item ID 代替实际资源范围。外部删除后在同一 Engine、同一路径下重建对象，只要用户没有在 ADDP 中进行相应的资源绑定或授权变更，就仍按同一逻辑资源处理，不自动撤销原规则或要求重新授权；访问仍须满足规则有效期、当前主体、功能权限、撤销、Deny 与安全条件。

保留现有 DataItem 指纹算法及逻辑定位语义，不增加物理创建实例、OID 连续性或源端生命周期配合作为授权前提。ADDP 不承诺识别源库每一次物理替换。外部改名形成不同路径，原规则不自动跟随；Catalog 的业务名称编辑只改变编目信息，不修改来源表名，也不新增来源改名功能。显式改接不同 Engine 或不同路径、Catalog 来源重绑和授权调整必须分别处理，目录身份连续不把原目标规则转移给新目标。同一实际引擎搬迁且经现有规范显式确认后保留 Engine ID 的行为不变，不另设物理实例追踪流程。

目标暂时不存在时，访问失败，不伪装为读取成功；缺失本身不删除逻辑资源规则。该路径重新可用后，按当前仍有效的规则判断访问，而不是因“重新出现”自动授予额外范围、延长期限或恢复已撤销的规则。首次表级规则以明确选中的资源为范围，不从名称相似、目录责任、组织成员资格或连接凭据推导全库访问。

Manager 预览和 SQL 对同一表使用同一授权规则；SQL 必须核验实际完整读取对象集合，读 C 写 D 则分别核验读取和写入对象集合及动作。复用 Provider 的 PreparedQuery／QueryReadSet，不通过正则猜测；解析失败或执行资源范围无法约束时拒绝对应受控操作。不能向用户代码暴露整库宽权限凭据，再依赖页面或一次入口检查。首次 PostgreSQL C/D 闭环只证明实际覆盖的访问路径，不代表 Quality、Transfer、Notebook 或其他引擎已经全部贯通。

System 引擎访问控制领域先提供私有的当前读取规则观察底座：对非空、完整的精确目标集合，以同一条 SQL 的已提交快照和唯一数据库时刻，检查当前 User／Tenant Membership／Tenant／授权版本、启用引擎、直接部门与项目组成员关系，以及尚未到期或撤销的 Grant、尚未到期或解除的 Explicit Deny。任一目标不满足就不满足集合覆盖；Explicit Deny 优先，无 Grant 拒绝。父部门、项目组关联部门、目录责任或管理委派不推导读取许可；原办理人的后续状态、Catalog 在线状态和已经结束的五分钟签发窗口不替代已签发 Grant 的访问期限。

该底座只证明当前源数据规则覆盖，不证明调用凭据真实有效，也不替代功能 Permission、Execution Authorization 范围或 Security 条件。主体引用和完整读取集合须由后续可信消费链路提供，不能从浏览器自报账号或任意路径构造放行请求。观察结果只描述该快照时刻，不是可缓存或跨操作复用的访问令牌；数据库读取失败、无目标、非法目标或结果不完整均不得放行。规则底座本身保持私有，不开放脱离 owner 操作契约的独立裁决 API；正式同步检查契约见下文，不宣称 Manager／Develop 的实际读取已接通。

可信同步用户消费的第一步复用 System IAM 的 `ResolveUserAccessToken`，仅接受第一方或 OAuth User Access Token，在自有只读 Repeatable Read 事务中解析真实凭据；当前 Tenant、Principal、Membership 和授权版本全部从该结果派生，不接收自报身份、外部 AuthContext 或历史 execution 引用。IAM 凭据核验与精确规则读取共用同一已提交快照；规则查询结束还须按数据库墙钟复核 Token 的自然到期，避免只读事务的开始时间延长凭据。凭据不保存、不缓存、不记录，也不返回给调用方。此私有组合仍只证明身份及源规则覆盖，不替代 owner 的功能 Permission／Client Scope、异步执行范围和 Security；Service Token、Resource Ticket 或 Delegated Token 不进入这条普通 User 消费路径，不能降级为机器读取或兼容放行。

Explicit Deny 提交后立即生效，其建立时刻是审计事实，不是预约生效时间；数据库墙钟小幅回拨不能借此忽略已提交且尚未到期、未解除的拒绝规则。

同步消费首个正式检查入口为 System `POST /api/v1/system/engine-access/read-checks/manager-preview`。它只接收非空且至多 200 个完整 EngineCatalogPath；当前凭据只能放在 Bearer 中，不接收 Tenant、账号、AuthContext、execution、Permission 或动作选择。入口固定复核 `manager.data_item.read` 的当前 Tenant Assignment，以及第一方 Web 的 unrestricted API 会话或 OAuth User 的 `addp.api` audience／Scope；组织范围功能分配不能在缺少资源归属证明时升级为租户级功能权限。真实凭据、上述功能／Client 条件和精确源规则使用同一只读快照；结束前复核凭据自然到期。

该固定 Manager 检查另接受真实 IAM 签发、精确绑定 `audience=manager` 与唯一 `data.preview` Scope 的 Delegated Token。它沿 IAM 委托凭据解析核验源会话、当前主体／Membership／授权版本、委托 Client 与调用绑定，使用同一当前 User 的精确源规则；不伪装普通 User，不放开 Resource Ticket 或 Service，不赋予 Tool 凭据其它 System 普通 API 权限。这是同一 Manager 预览的两种可信消费资格，不增加通用跨 Owner 委托检查入口。Tool-only OAuth User 本身仍不能直接调用普通 API 路径，必须先经正式 Tool 委托。

任一条件不满足时整个检查失败，无 Grant／命中 Deny 返回 403，不返回逐项规则或主体详情；成功仅返回 `observed_at`，不生成访问令牌、lease、授权副本或可缓存 Allow，不持久化请求目标或正文。领域检查只读且不追加授权审计；HTTP 入口仍沿用 System 通用请求元信息审计，不记录 Bearer 或请求正文。该入口是 Manager 同步调用链的源规则检查，不是完整执行裁决，也不访问源端或 Security。Manager 仍须先以 Provider 证明本次完整 ReadSet，完成本地 Security 保护，并执行同一 PreparedQuery；未完成实际消费接入时不能宣称预览权限闭环完成。后续不同 owner 操作须明确自己的固定功能契约，不能通过客户端传入权限名复用本入口降低门槛。

Manager 剖析结果读取另使用固定同步入口 `POST /api/v1/system/engine-access/read-checks/manager-profile-result`，与预览共用当前源规则和普通 User 凭据的只读快照实现，但不开放 Delegated Token、Service Token 或 Resource Ticket。固定功能条件同为 `manager.data_item.read`，请求只含 1–200 个完整目标，不接受身份、Permission、execution 或效果选择；成功只返回不可缓存的当次 `observed_at`。Manager 必须提供结果生成时冻结的真实采样完整 ReadSet，不以今天的依赖或所选叶子替代；来源证据缺失或不规范时拒绝派生内容读取，不回填历史、不删除结果。该入口不授予后台剖析执行资格，不代替 Security，也不改变结果共享语义。

#### 5.5.5 批准要求、可选治理与明确交接

账号授权核查合并到当前授权的唯一 `GET /engines/:id/access_grants`：指定 `recipient_type=user` 和 `recipient_id` 时，按表返回该账号的个人及当前有效部门、项目组授权来源；其他筛选仍按直接接收方返回授权关系。操作人仍须当前 Tenant User、`system.engine_access_grant.read` 与本引擎管理资格。选中账号只用于定位同租户对象，不作为可信调用来源；主体、成员关系和授权版本由 System 权威库取得。其他租户或不存在的账号返回 404。不保留独立 inspection HTTP 入口。

核查与实际读取复用唯一源规则查询，在同一个已提交快照和数据库观察时刻展开账号当前有效的个人、部门、项目组来源，排除已撤销和已到期 Grant，并按接收主体合并重复来源，显示有效期及记录数量。每张表携带 `inspection`，包含 `account_id`、规范目标、`observed_at`、`rule_covered`、固定 `reason`（`grant|no_grant|explicit_deny|source_unavailable|target_unavailable`）及 `sources`；每个来源独立携带撤销定位编号、批准方式、期限及记录数量。筛选后按表计数和分页，不能先分页直接授权再补组织来源。拒绝优先，但仍可展示命中的有效 Grant 来源供管理员排查；不返回拒绝规则正文、编号、原因或操作者。名称解析仍须既有独立 IAM 读取权限。

核查不读取源表、不持久化授权、不产生凭据或可缓存 Allow；返回 `Cache-Control: no-store`。`rule_covered=true` 只说明本表源规则覆盖，不能替代接收账号实际会话、功能权限、Client 条件、Provider 完整读取集合或 Security 检查。普通数据使用者不因此取得核查他人的资格。页面条件变化自动查询并隔离旧响应；无匹配记录只表示当前筛选没有授权来源，不是逐表访问裁决。撤销必须定位具体个人／部门／项目组来源，不对账号聚合行整体撤销。授权历史保留全部签发、到期及撤销事实，账号条件只匹配直接个人记录，不按当前组织成员关系改写历史。

本节是 2026-10-01 已确认的架构补充。批准要求的首次初始化、管理读取、显式切换、正式准备与跨模块首次受理已按下述契约实施。退出业务批准或重新启用业务批准通过同一显式切换入口办理；表级 Grant 仍单独签发，不以初始化、切换或受理接口代替实际授权生效。

批准要求针对 `Tenant + Engine + 完整 EngineCatalogPath`，不针对整个平台注册历史，不递归扩展为整库，也不因 CatalogEntry 重绑把旧目标要求搬到新目标。同一目标只能有一份 System 权威批准要求；Catalog 不保存第二份可编辑要求。

| 情形 | 新授权办理 | 既有有效 Grant |
| --- | --- | --- |
| 已有明确的 System 独立批准安排 | 核验独立批准 Permission、有效管理委派及本次精确参数 | 按原范围、期限、主体及安全条件判断 |
| 明确要求 Catalog 业务确认，Catalog 可达 | Catalog 最终核验业务依据，System 持久受理后在原窗口内办理 | 不额外要求 Catalog 在线 |
| 明确要求 Catalog 业务确认，Catalog 失联或停用 | 不自动改为独立批准；对应的新办理无法取得依据时拒绝 | 不因失联自动撤销、扩大或续期 |
| 具备独立退出资格，并已明确范围和有效承接安排 | System 显式交接批准要求；后续新办理使用独立批准入口 | 不因交接自动撤销、扩大或续期 |
| 没有权威批准要求，或存量责任尚未核清 | 不能把“没有记录”当成独立批准许可；先完成明确初始化或有资格的交接 | 已有规则仍按自身有效性判断 |

2026-10-07 已确认：责任配置与批准方式配置分开。保存 Catalog 业务负责人只建立责任关系，不隐式启用业务确认，也不覆盖 System 已明确的独立批准安排。明确启用 Catalog 批准方式后，新办理必须取得有效业务依据；若责任尚未配置或写入失败，应修复责任或办理明确交接，不自动清除批准要求。两个数据库不宣称拥有同一事务，网络超时或响应丢失不能作为放宽条件的依据。

单独初始化批准要求的公开入口为 `POST /api/v1/system/engines/:id/access_approval_requirements`，只接受精确 `catalog_path`、必填 `mode=catalog|independent` 与非空 `reason`，建立版本 1。遗漏或未知模式拒绝，不保留固定 Catalog 模式的旧请求契约。请求不接受版本、租户、操作账号或承接人字段，不递归作用于父路径。当前 Tenant User 必须同时具有 `system.engine_access_approval_requirement.initialize` 与管理资格（当前租户授权管理员或该 Engine 的有效受托办理人）；System 在同一事务内锁定当前身份、读取有效 Tenant Scope Permission、锁定引擎及委派，再进入精确目标边界，等待后按数据库墙钟重新核验期限，并与审计原子提交。首次配置不是已有要求的交接，不指定承接人、不授予独立批准 Permission 或内容访问权。已存在要求均返回 409，不覆盖、不自动增加版本；不得用此入口在两种模式之间切换。

管理读取使用同一路径的分页 GET 和 `/:requirement_id` 详情 GET，要求独立 `system.engine_access_approval_requirement.read` 及当前有效引擎管理资格（授权管理员或受托办理员），不接受 `tenant_id`。它只读取当前 Tenant、当前 Engine 的权威要求，不返回连接信息、责任副本或内容授权。两个 Permission 均仅允许 Tenant Scope、不可委托；通过租户自定义角色、源数据授权办理员或引擎授权管理员模板显式分配。不因普通租户管理员角色名称自动取得资格。不存在批准要求仍不是独立批准许可。上述入口不建立 Catalog 责任、不完成跨数据库纳管协调、不签发 Grant；Catalog 不可达不影响初始化或管理读取。

办理所需的最小观察（2026-10-03 已确认）使用 `POST /api/v1/system/engines/:id/access_handling_requirement`。结构化路径可能超过安全 URL 长度，因此使用只读 POST，正文只接受路径 `version` 和完整 `segments`，Engine ID 唯一从路径参数取得；不接受 Tenant、操作者、批准模式或预期版本。当前 Tenant User 必须同时具备独立 `system.engine_access_fulfillment.create` 和该引擎的有效管理委派，按现有身份→引擎／委派锁及数据库墙钟复核。仅查询一个精确叶子，不枚举引擎要求、不继承父路径、不初始化缺失要求，缺失返回 404。响应仅含当前 `mode` 和无损十进制字符串 `requirement_version`；既有配置读取权限不因该入口放宽。观察不创建业务决定、待核清、受理、Grant 或批准要求变更审计，沿用 HTTP 请求审计但不保存正文；不是资格凭据。正式提交仍核验当前权限、完整目标及原预期版本，冲突不自动换版本重试。

交接必须区分“设置批准安排”和“批准一次授权”。前者只改变后续办理所需依据；后者还要分别核验接收主体、动作、期限、当前批准／办理资格和目标管理范围，才可能产生 Grant。退出操作人可以与承接人同人，但必须分别满足资格；该兼任不额外扩大 5.5.1 已确认的普通只读自用范围，也不绕过既有敏感动作复核。

2026-10-08 已确认的切换入口为 `PUT /api/v1/system/engines/:id/access_approval_requirements/:requirement_id`，严格只接受正整数 `version`、目标 `mode=catalog|independent` 和非空 `reason`。精确目标由本租户、本引擎的原要求记录派生，不接受正文覆盖路径、承接人或身份。当前 Tenant User 必须具有独立 `system.engine_access_approval_requirement.update` 和当前引擎管理资格；此权限默认只组合进引擎授权管理员模板，不组合进源数据授权办理员或其他角色。切到 `independent` 时，操作人还须具有有效的 `system.engine_access_grant.create`，本人作为当次承接人写入审计，不成为永久唯一批准人。切到 `catalog` 是明确启用业务批准，不以 Catalog 在线状态推导或自动切换。两种切换均只影响后续新授权；不修改既有 Grant、撤销记录、期限、业务确认或已正式受理的原办理。

切换复用身份→引擎／管理资格→精确目标的唯一事务边界；按原版本校验，要求与高风险审计原子提交，并在锁等待和审计后按数据库墙钟复核资格。跨租户／引擎记录返回 404；版本冲突返回 409 且无副作用。相同模式不制造新版本或切换审计，仍须匹配原版本和当前资格。页面从所选表的已有批准方式旁提供切换操作，明确填写原因并确认；失败保留输入，不自动改用新版本重试，不在选择、刷新或恢复页面时写入。响应不确定时先只读重载当前要求，不重复切换或签发 Grant。角色模板增加此权限时推进已分配主体授权版本并撤销其旧会话，按现有 IAM 发布机制重新登录生效，不自动分配账号或创建引擎委派。

System 在批准要求变更与新 Grant 写入时使用同一精确目标并发边界。已读取旧要求、但尚未正式接受的请求不能越过交接继续使用旧批准依据；重试不从服务端最新版本静默补齐。2026-10-01 已确认：退出不取消退出前已正式接受的同一次、同参数办理，它仍可在原 5 分钟窗口及原拟授权到期时间内完成；System 继续核验当前办理资格、接收主体和精确参数，不增加范围、不刷新窗口。退出后发起的新办理使用新的批准安排。已成功写入后的同参重试只返回原结果，不再次授权或续期。退出与接受的先后必须由可信事务协议证明，不能比较两个服务自报的时间戳。

2026-10-01 已确认：正式受理回执归 System，与批准要求变更及 Grant 写入使用同一精确目标事务边界；Catalog 仅提供已持久业务决定的当前核验。不得让 System 持锁同步反向调用需要 Catalog 核验锁的接口，形成循环等待。跨模块接口必须遵循下述可信机器调用、真实操作账号、同次参数和未知结果恢复契约；浏览器正文自报的“已批准”、身份或时间不得作为依据。Catalog 本地锁不能跨进程故障持续存在，因此仅“持本地锁调用 System”不构成故障情况下的先后证明；协调协议落实并经过真实并发及故障验证前，不发布实际受理或源数据 Grant 接口。

已确认的故障协调规则：Catalog 必须先在条目／来源核验事务内持久保存同次精确请求的待核清事实，事务提交后才允许发送。请求未核清期间，影响该核验依据的责任变更、弃用及来源变更只能暂缓，人工和后台写路径遵循同一规则；无待核清请求的其他条目、普通业务说明维护、模块 Ready 和已有合法读取不受影响。批次同步回滚受阻批次并保留 checkpoint，不跳过对象。System 必须把同编号受理和关闭串行化；关闭先提交则迟到受理不能成功，受理先提交则关闭返回原回执。查询未找到不等于关闭；超时、进程重启或经过 5 分钟不删除待核清事实。请求编号绑定租户、可信调用主体、精确目标、业务决定引用、批准要求版本及完整参数，不能换参复用。已受理结果和持久关闭结果均不可改写；核清读取 System 原结果，不在 Catalog 保存第二份受理状态。System 明确退出不要求 Catalog 在线，仍按同目标边界阻止旧要求的新受理。

核清查询是独立的只读动作，不以再次受理或执行关闭代替。System 只返回已提交且完整请求绑定一致的原结果；未找到、绑定冲突和数据库错误分别保留，均不产生关闭事实。查询不取受理／目标写锁，不修改批准要求，不追加办理审计或刷新原窗口，也不要求历史结果仍满足新版本批准安排或未过期。原结果只证明历史受理／关闭，不能据此跳过实际 Grant 写入时的当前资格与期限检查。

生产核清使用 `POST /api/v1/system/runtime/engine-access-fulfillments/:request_id/resolve`，仅 `addp-catalog` 的当前 Tenant Service 身份及独立 `system.engine_access_fulfillment.execute` Permission 可调用。沿用权限目录的 `execute` 动作，核清／关闭与下述首次受理使用同一机器 Permission，不能将其授予人类办理角色。正文为原完整绑定，不接受正文 Tenant；调用 Principal 必须与绑定的调用主体相等。查询明确返回 `found=false` 才能调用同编号 `/close`；普通 404、鉴权失败、超时及解析失败均不能解释为未找到。关闭与首次受理沿用同一请求／精确目标锁，返回胜出的原不可变结果，且不能生成 Grant。关闭提交前重新核验调用服务本身的当前 IAM 身份、授权版本、Permission 和 Token 期限；原人员、确认人及接收方后来失效不阻断历史核清。

Catalog 后台仅消费已提交且未核清的原请求，逐项调用该主路，不创建新请求、不重试新受理，也不写第二份 System 结果。沿用来源同步调度周期；至少等待一分钟后开始故障核清，为前台提交后的首次发送留出时间。此等待只是调度延迟，不延长五分钟办理窗口，也不能作为清保护的依据。远端故障延后本项，不能影响模块 Ready 或清除本地保护。

办理请求还必须不可变绑定原操作账号的 Principal、当前 Tenant Membership 和授权版本，机器调用主体不能替代该账号。此最小来源只由 owner 从已验证的 User AuthContext 派生并持久化；结构完整的身份字段本身不是可信来源证明，System 后续仍须通过 owner 的持久事实核验其来源，不能信任浏览器正文自报。首次受理的身份底线在 System 自身事务内检查当前 User、Membership、Tenant、授权版本和成员有效期；按 Principal → Membership → Tenant → 请求／目标的顺序锁定，等待及业务核验后重新检查数据库墙钟，不能借排队越过到期。身份只读核验使用共享行锁，既阻止状态和授权版本在受理提交前被修改，又与审计外键的引用检查兼容；不能在持有目标边界时反向获取身份写锁。关闭未受理请求和只读核清历史不要求原操作人仍有效，否则失效账号会使待核清依据永久无法解除。历史结果查询不恢复该账号的新办理资格。此身份底线不代替独立功能 Permission、有效目标管理委派或 Catalog 当前业务依据；正式首次受理已按下述可信反查契约消费这些核验；实际 Grant 尚未开放，不保存 User Token 或可编辑 Role／责任副本。

首次受理还须在同一 System 事务内核验原操作账号对目标引擎的当前有效管理委派。锁顺序为 Principal → Membership → Tenant → Engine → Delegation → 请求／精确目标；引擎与委派采用共享行锁，使同一引擎不同目标的核验可以并行，同时阻止受理提交前的引擎状态变更和委派撤销。委派仅匹配本租户、本引擎及该账号当前 Membership；等待和业务核验后按数据库墙钟再次检查生效及到期时间。资格失效拒绝新受理，不阻断原回执读取或未受理请求关闭。此目标管理范围检查不授予内容访问、不替代独立办理 Permission、接收主体核验或可信业务决定；实际 Grant 写入时仍须核验当前资格。

首次受理的接收主体核验同样由 System 当前 IAM 事实完成：`user` 必须是有效 User，且具有本 Tenant 的有效 Tenant Membership，成员有效期按数据库墙钟判断；`department` 必须是本 Tenant 的有效 Department；`project_group` 必须是本 Tenant 的有效 Project Group。组织可无成员，不据成员数量推导组织失效，也不把接收组织的每个成员锁定或复制成个人 Grant。办理人与接收 User 的 Principal 先去重并按 ID 升序取得共享锁，再读取成员、Tenant、接收组织、Engine 和 Delegation；不能持有下层锁后才锁接收账号。接收事实保持共享锁至受理事务结束，等待和业务核验后再检查期限；不绑定接收账号的授权版本快照，不因接收账号角色变化永久冻结同一账号的受益身份。失效、跨 Tenant 或不存在的主体不产生新受理，已提交结果的同参核清和未受理关闭不重新要求接收主体有效。此检查不是 Grant，实际规则写入与访问仍须重新裁决当前主体资格。

确认人的 IAM 当前资格由 System 在首次受理事务内核验，原责任关系的连续性仍由 Catalog owner 核验。办理人、原确认人及接收 User 的 Principal 必须先共同去重升序锁定，再取得成员、Tenant、组织、引擎和请求／目标锁。确认权限使用当前有效的 Tenant Scope Role Assignment，事务内读取的权限行不持久化为副本；身份共享锁阻止授撤权写入，提交前数据库墙钟再次检查成员及 Assignment 自然到期。历史授权版本不作相等比较，也不得借此放宽办理人版本检查。该内部资格检查不证明输入来自 Catalog，不替代可信 owner 读取、原责任核验或独立办理 Permission。

重新部署 Catalog、恢复心跳、回放旧来源同步或只保留历史责任记录，都不得自动撤销此前明确完成的退出安排。重新启用业务确认要求必须是新的明确治理操作，使用当前版本并留审计。对账用于发现待修复情况，不取得放宽、重新接管或发放内容访问的权限。

交接时必须核验承接账号及其独立批准功能和目标委派当前有效；不能只指定一个无人拥有的角色。此后承接资格失效，只拒绝无有效办理人的新授权，不把旧责任或模块状态当成自动补权依据。通过现有 IAM 角色分配和引擎管理委派入口显式修复，不能靠停用 Catalog 修复权限。

2026-10-01 已确认：承接账号用于证明交接时确有有效办理人，并保留在该次交接审计中，不成为精确资源永久唯一的批准人。交接后，任何同时具备独立批准 Permission 与该引擎有效管理委派的当前 Tenant User，都可按独立批准契约办理；仍须核验本次接收主体、动作、期限及既有复核条件。System 批准要求不另存审批账号白名单，不能仅因曾被指定为承接人而放行，也不能因未被指定而拒绝另一个当前具备完整资格的账号。

把精确源目标的批准要求首次设为需要 Catalog 业务确认，须同时具有独立治理配置 Permission 和该引擎当前有效的授权管理委派。现有编目维护 Permission 只允许修改企业条目，不隐式授予调整源数据批准安排的资格。2026-10-07 已确认：缺少启用资格仍可按编目权限维护责任，但不得宣称批准方式已经改变；已明确的独立批准安排保持不变。资格与接口、审计、迁移和真实消费者同时发布，不新增未被使用的启用／退出 Permission。该规则不把标准、模型等专业对象的业务责任变成引擎管理操作。

#### 5.5.6 无 Catalog 的独立授权首版产品范围

独立只读授予的正式契约为 `POST /api/v1/system/engines/:id/access_grants`：当前 Tenant User 同时满足 `system.engine_access_grant.create` 和管理资格（当前租户授权管理员或该引擎有效受托办理人）。正文明确提交命令 `request_id`、完整 `catalog_path`、十进制字符串 `requirement_version`、`recipient_type`、十进制字符串 `recipient_id`、`action=read`、显式 `expiry_mode`、可空 `expires_at` 与非空 `reason`；Tenant、操作者、批准来源和签发时刻均由 System 派生，拒绝额外字段或 Query。首次签发通过 Engine Catalog Provider 的 `ResolvePath` 在源端精确确认普通表或集合叶子存在后，重新核验当前 IAM、委派、精确目标的 `independent` 批准方式与原预期版本；不读取样本、不请求企业 Catalog。源端验证期间不持有数据库事实锁，引擎管理员编辑版本变化则拒绝提交并要求重新选择。Grant 与高风险审计同事务落地，返回 201 才表示签发事实已提交。未明确首次初始化时，缺少要求不能签发；已有 Catalog 模式或版本变化均不能独立签发。首次初始化遵循下述原子命令契约。

2026-10-11 已确认：独立读取 Grant 接受精确普通表与 `collection` 叶子，沿用同一签发、批准要求、身份、委派、到期和撤销主路径。MongoDB 集合通过原生 `listCollections` 名称谓词及类型确认，只接受普通 `collection`，不接受视图、时序集合、命名空间或未来新增集合；核验不查询文档、统计、索引或字段。Provider 返回的完整路径须与请求完全一致。该范围首先由 MongoDB Hosted 验收验证；现有 Console 批量选表交互仍保持普通表范围，不把目录分支解释为递归 Grant。

同一个命令 UUID 仅允许原操作者、同引擎、同目标、同接收方、同动作、同期限、同批准版本及同原因重试；当前授予 Permission 和管理委派仍须有效。同参返回原签发事实，不续期、不撤销或恢复，不要求历史接收方或源表仍有效；不同参返回 409，跨 Tenant 或引擎隐藏为 404。历史恢复不以操作者当前授权版本等于签发时版本为条件，但当前请求身份必须有效。首次签发提交前按数据库墙钟复核期限与接收主体。

`GET /api/v1/system/engines/:id/access_grants` 使用独立 `system.engine_access_grant.read` 和管理资格（授权管理员或有效受托办理人），默认第 1 页、20 条、最多 100 条；按同租户、精确目标、接收方类型／编号和动作聚合尚未到期且未撤销的 Grant，返回当前授权关系及其有效记录数量。长期有效优先，否则显示这些记录的最晚到期；返回的 `request_id` 是最新有效记录的撤销定位引用，不是访问凭据。独立批准和 Catalog 来源在同一关系内展示，不按签发者拆成多条。`GET /api/v1/system/engines/:id/access_grants/history` 才按签发时间倒序及 UUID 稳定分页返回不可变签发／撤销历史，两者沿用同一读取权限和存储，不新增 ACL 副本。当前关系仅表示授权来源仍有效，不是实际访问 Allow；失效身份、Deny、功能权限和安全策略仍由读取时裁决。停用引擎允许查看、撤销，不允许新授予。000196 仅登记两项新 Permission，000197 将其组合进专门的源数据授权办理员模板；不自动分配账号，也不扩张其他角色；现有 System T1、IAM PostgreSQL T2、Swagger 与授权清单门禁覆盖该接口切片。

同一授权关系已有有效 Grant 时，新命令返回 409 `engine_access_grant_relation_exists`，不因新的命令 UUID、操作者或批准来源再发一份，也不静默延长或缩短期限；原命令同参重试仍恢复原历史。修改期限须明确收回原关系并按当前批准要求重新授予，业务批准不能改由独立授予绕过。存量重复记录不删除、不自动撤销，当前列表合并展示实际数量。

`POST /access_grants/:request_id/revoke` 收回该定位记录所代表的整条关系：在精确目标锁内，原子撤销同目标、同接收方、同动作的全部仍有效 Grant，并保存本次收回的编号集合及同事务审计。历史和原期限不改写。对原定位编号的同操作者、同原因重试只恢复原撤销结果，不收回后来重新授予的记录。自然到期或已撤销记录不再作为新的收回入口。个人、部门和项目组关系分别处理；收回个人关系不收回组织关系、不建立 Deny，页面须明确提示其他组织来源仍可能允许访问。

2026-10-07 已确认：Catalog 是可选治理模块，缺少 Catalog 不取消源数据权限检查，也不应强制用户为了访问数据先完成企业编目。System 的引擎列表提供与详情并列的“授权”入口，直接批准的首次配置与读取授权合并操作。独立授予和历史读取采用上述正式接口，已有安排的退出／交接仍须独立落实。

2026-10-08 的统一授权交互：先选择数据表或非根命名空间，再核对本次精确表清单、批准方式和接收方。命名空间只是把本次发现的普通表加入选择清单，最多 200 张，用户可逐张移除；不保存递归授权，不包含视图或未来新增表，发现失败不提交部分清单。复用共享资源选择器和 System 实时目录，不读取样本。

直接批准复用唯一 `POST /engines/:id/access_grants`：显式 `initialize_approval=true` 仅允许版本 1 的首次配置，同时额外核验 `system.engine_access_approval_requirement.initialize`。批准要求、只读 Grant 与两项审计在同一事务提交；已有批准要求返回冲突，不覆盖或切换。`initialize_approval=false` 核验已有直接批准版本。初始化意图作为不可变命令参数保存，同参重试只恢复原结果。页面不再提供“先保存直接批准、再授予读取”的两步表单。

批量清单沿同一逐表命令办理，各表独立原子提交并显示成功或失败；没有批量全成功假象。失败／结果未知时保留每表原命令，显式重试不新建命令或重复发放。既有业务批准目标不可被批量直接批准覆盖。业务批准仍由 Catalog 维护业务确认，System 保存其批准方式后提供对应引擎和表名的 Catalog 浏览入口；跳转不冒充确认完成或授权生效。Catalog 未可用时不提供新的业务批准选择，已有业务批准不自动降级。

日常页面在独立批准的配置行提供“授予读取权限”，只绑定该行的精确表与原版本；Catalog 模式不提供独立授予按钮。接收方从当前 System 账号、部门或项目组清单下拉选择，复用已有 IAM 读取权限，不让用户手填身份 ID。缺少候选读取权限给出明确说明，不退回自由输入。期限必须显式选择。确认摘要同时展示表、接收方和期限，提交不自动重试；成功只说明签发已提交，未知结果保留原命令及原参数供同参重试，不生成新的命令。授权历史包括批准来源、接收方、期限与撤销事实，不宣称当前一定可读取；撤销必填原因，权限／账号／引擎上下文变化后清除草稿及迟到响应。该页面不请求 Catalog、Meta 或数据样本，现有 System 前端单元、构建和隔离浏览器门禁自动覆盖。

- 首次配置分别安排引擎授权管理员和源目标的批准方式。管理委派、批准方式配置和实际数据授权是三种不同操作；前两者均不产生内容访问权。配置与批准分别要求实际功能 Permission 和管理资格（当前租户授权管理员或该引擎有效受托办理人），不能从登记人、管理员角色名称、连接成功、模块缺席或记录缺失推导资格。
- 明确采用独立批准的源目标，由具备独立批准功能 Permission 和管理资格（授权管理员或有效受托办理人）的账号办理，不要求 Catalog 条目、盘点、编目或业务共享确认。同一人可以兼任管理员和接收方，但须显式授权并记录真实操作者，不能因兼任自动获得数据权限。
- 日常流程为“选择数据 → 选择接收方 → 明确操作与有效期 → 确认授权”。接收方使用当前 Tenant 的有效账号、部门或项目组；组织授权按当前成员关系消费，不展开为个人授权副本。首版只支持明确选中的数据库表及普通只读，不授予全租户公开、写入、建表或任意其他模块操作；功能权限与安全约束仍独立核验。导出和各执行模块消费须分别落实其操作契约，不能凭只读授权宣称全部消费者已贯通。
- 不默认整库公开。多选只表示本次明确选中多个精确目标，不递归授权父目录、整个引擎或以后新增的表。批量采用上述逐表独立原子提交、逐项结果和原命令重试契约，不把页面多选视为整批事务，也不把部分成功显示为整批成功。
- 数据选择复用引擎实时目录能力，不要求先完成 Meta 扫描；实时发现名称和结构本身须受控。扫描若读取样本或文件内容，另须满足该内容访问授权，不能把发现资格扩张为数据读取资格。
- 每次授权显式选择“指定到期时间”或“长期有效，直至撤销”；遗漏不默认为永久。普通独立只读不沿用临时接入的强制到期规则，也不放宽引擎管理委派的期限。
- 界面收拢内部处理步骤，成功结果只在 System 权威授权事务提交后展示；失败或结果未知不能表示已生效。提供授权列表及明确撤销入口，不通过重复授予实现查询。独立批准和 Catalog 业务确认只改变批准依据，最终使用同一源数据 Grant 权威存储与读取裁决，不伪造 Catalog 决定或受理回执，也不另建独立授权副本。
- Catalog 部署、恢复或来源同步本身不自动接管批准安排；停用或失联不自动退回独立批准。既有有效授权继续按原范围、期限、当前主体资格和安全条件判断。已有 Catalog 批准要求须经明确交接才能改为独立批准，不能用首次配置覆盖。

2026-10-07 已确认：指定业务负责人仅建立责任，不触发批准方式切换。Catalog 业务确认必须由有配置资格的人显式启用；无批准要求、没有 Catalog 或服务不可达都不自动放行。首次独立配置与 Catalog 配置共用 5.5.5 的唯一初始化入口；已有安排的变更须走明确交接，不通过重新初始化覆盖。

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

OAuth 数据模型不复用 `system.api_consumers/api_consumer_credentials`：API Consumer Credential 只识别已登记的数据面 API 消费方，OAuth Client 表达获得用户授权或 Service Principal Token 的客户端软件，两者生命周期和审计语义不同。API Consumer 不生成 AuthContext，也不能访问控制面 API。

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

Service Principal 只能访问显式挂载在 `/api/v1/system/runtime/*` 等 Service Runtime 路由上的机器契约。`/api/v1/system/platform/*` 是平台 User 管理面，即使 Service Principal 所持 Runtime Role 含有同名 Permission，也不得用 Service Token 访问该管理路由。需要平台事实的 Runtime 必须使用单独的最小投影 API，并同时校验 Platform Service Context 与精确 Permission；不得为复用 User 管理 API 而放宽凭据类型守卫。

## 八、禁止事项

- 外部 Agent 使用共享服务密钥或模块 OAuth Client Secret 直接调用 owner API；
- 在 CLI、Agent 或 owner 模块内保存 `JWT_SECRET`；
- 客户端提交 Principal、User、Tenant、Membership、Role 或 `user_type` 并被服务端信任；
- 用 API Key、OAuth Scope 或平台角色模拟 Tenant 业务授权；
- 在 `/api/v1/*` 控制面路由接受 `X-API-Key`，或仅由 Gateway 判断 API Consumer 的最终资源权限；
- 通过 Scope 提升 Role Permission 或跨 Tenant 权限；
- 业务模块从 Token 字符串、日志或前端状态反推授权上下文；
- 恢复 `user_type` 与 Role Assignment 双轨权限判断。

## 九、当前实现与演进约束

当前 IAM Runtime 使用唯一 `auth-context-v1.schema.json` 和共享类型投影第一方 Web、OAuth User、Service Principal、Browser Resource Access Ticket 与 Delegated Access Token。旧 `users.user_type`、`users.tenant_id`、JWT 用户令牌、平铺 Gin Context Key 和 `/users/me` 令牌验证路径已删除。

Resource Ticket 使用所属第一方 Browser Family 的同一身份与授权投影，并用 owner audience 和 `resource:read` 额外收窄；Delegated Token 回溯源 Access Token 与 Family，并用 owner Tool Scope 和审计绑定额外收窄；Service Access Token 只由 Fosite Client Credentials Grant 签发，并固定为一个 Tenant Membership Context 或一个显式 Platform Service Context。调用方不能保留共享 Internal API Key 与 Bearer 双轨。

Execution Authorization 与 Notebook Session Authorization 都不新增 AuthContext Token 类型，也不复用 Agent Delegated Access Token。前者授权绑定 execution 的数据效果；后者绑定 Notebook Session 生命周期，并且只能执行实时 Engine Catalog 或派生新的只读 Execution Authorization。两者不能互换；禁止恢复“Service Principal 直接获得通用 Engine 明文读取权限”和“用户 Token 代传到 Worker/Runtime”两条旧路径。

v1 契约演进必须同步修改 Schema、共享 Go/Python 类型、System 投影、所有消费者和契约测试。ADDP 当前不提供按 Client 协商多个 AuthContext Schema 的兼容机制；需要破坏性变化时先修订本规范，再一次性切换全平台。

最小验证：

```bash
make test-authorization
cd system/backend && go test ./internal/iam/... ./internal/api/... ./internal/middleware/...
cd ../../common-python && .venv/bin/pytest -q
```

## 平台模块运行日志读取

System 实例日志查询只接受 Platform Context 和 `platform.module_log.read`。该 Permission 默认归平台系统管理员，不由模块读取、操作审计或租户管理员权限推导；Runtime Principal 不获得正文读取能力。System 校验登记实例归属后生成受限查询，不接受客户端提供文件路径、原始 LogQL 或目标服务器。运行日志正文必须在生产与采集边界限制敏感内容；读取审计只包含操作者、实例、时间范围及结果，不包含正文或关键字。

## 平台日志链路观测和告警

Monitor 拥有 `monitor.log_pipeline.read/update`、`monitor.log_observation.create` 及 `monitor.log_notification.read/update`，只在 Platform Context 使用。平台系统管理员默认获得查看、处理、规则与通知目标管理能力；观测上报仅允许独立 `addp-log-observer` Service Principal，节点名必须匹配部署绑定，委托凭据和用户凭据不能上报。日志正文仍单独检查 `platform.module_log.read`。平台事件没有 Tenant ID 或任务身份，不进入租户接口；统一操作审计不保存观测载荷、正文或通知 secret。

Monitor 平台告警读取与管理操作通过唯一 System 平台审计追加接口记录安全请求事实。`audit.event.create` 默认只授予 `platform.monitor_runtime`，服务主体与 Platform Context 从凭据派生，模块必须属于调用服务；被验证的用户身份只作为最小操作来源记录，不覆盖审计的服务主体。观测上报不逐次产生操作审计。
