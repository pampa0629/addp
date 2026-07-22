# ADDP 授权上下文规范

更新日期：2026-07-22

状态：正式目标规范。本规范定义 ADDP 访问令牌的唯一解析语义和模块消费路径；AuthContext 的具体 JSON Schema 在目标 IAM 数据模型和 Permission 目录确定后补充，现有 `user_type` 响应结构不是目标契约。

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

AuthContext 必须表达以下语义组，具体字段名和 JSON 结构由后续契约设计一次性确定：

| 语义组 | 必要内容 | 约束 |
| --- | --- | --- |
| Principal | Principal 类型和稳定 ID；User 场景包含必要的用户标识投影 | Principal 只能来自 System 当前事实，不能信任客户端参数 |
| Context | `platform` 或 `tenant` 会话模式 | 两种模式互斥，不存在同时激活平台和 Tenant 权限的上下文 |
| Tenant | 当前 Tenant、Tenant Membership 及有效状态 | 只在 `tenant` 模式存在；一次上下文只能有一个当前 Tenant |
| Authorization Facts | 当前模式下必要的 Role Assignment / Permission 投影及授权事实版本 | 不返回其他 Tenant 权限，不包含主体可访问的全部资源列表 |
| Organization Facts | 当前 Tenant 的 Department / Project Group 关系版本或引用 | 具体成员关系由 owner 按需查询，不能跨 Tenant 合并 |
| Authentication | 认证方式、认证时间、MFA 强度和必要的 step-up 事实 | 平台三员和其他高权限操作必须能判断强认证要求 |
| Client Limits | 令牌类型、OAuth Client、audience 和 Scope | audience / Scope 只能缩小能力 |
| Delegation | 委托方、AgentRun、ToolCall 和用途绑定 | 仅受委托令牌存在，不能改变原 Principal 和当前 Tenant |
| Lifetime | 签发时间和过期时间 | 使用带时区的 ISO 8601 时间 |

平台管理上下文不得通过空 Tenant 表示“所有 Tenant”。它只激活当前 Principal 被分配的平台角色。Tenant 业务上下文必须绑定一个有效 Tenant Membership，且只投影当前 Tenant 内的角色和组织事实。

第一方 Web、OAuth 和 Delegated Access Token 都解析为同一 AuthContext 语义。OAuth Token 的客户端、audience 和 Scope 由 System 权威填充；Delegated Access Token 还必须包含唯一 owner audience、稳定 Tool Scope 和 AgentRun / ToolCall 审计绑定。

## 四、身份和上下文校验

System 生成 AuthContext 前必须校验：

1. opaque Token Hash 存在，Access Token 未过期、未撤销且 family 有效；
2. Principal 存在、类型匹配且当前有效；
3. 会话模式只能是 `platform` 或 `tenant`；
4. `tenant` 模式绑定的 Tenant 和 Tenant Membership 均存在且有效；
5. `platform` 模式不携带当前 Tenant，也不激活任何 Tenant Role、Department、Project Group 或资源授权；
6. Role Assignment、外部身份状态、组织关系和授权事实版本没有失效；
7. OAuth Client、audience、Scope、认证强度和委托绑定满足令牌类型要求。

任一校验失败均不返回部分 AuthContext。User、Tenant Membership、Role Assignment 或相关身份事实失效时，必须使已有 AuthContext 缓存失效。

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

委托令牌使用默认拒绝策略：owner 模块收到 Delegated Access Token 时，只有当前路由与 Token Manifest 中该 Tool 的 owner、audience 和 required scopes 完全匹配才可继续；委托令牌不能访问同一模块的其他普通 API。

Browser Resource Access Ticket 同样使用默认拒绝策略。System 的 AuthContext 接口是仅供 Owner 解析票据的基础设施例外；System 的其他身份、Tenant、OAuth、引擎和管理 API 必须拒绝该票据。Owner 只在自身明确声明的 GET/HEAD 原生资源路由接受对应 audience 和 `resource:read` Scope。

## 六、共享中间件

### 6.1 Go

`common/middleware/auth` 负责：

- 把 Bearer Token 转发给 System AuthContext API；
- 将不可变 AuthContext 注入 Gin Context；
- 提供 Principal、会话模式、当前 Tenant、client、audience、Scope、认证强度和授权事实的统一 helper；
- 不提供或新增基于 `user_type` 的授权 helper。

缓存 Key 只使用 Token SHA-256，不保存明文 Token。实际缓存时间不得超过 Access Token 剩余有效期；授权事实版本变化必须主动失效。

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

1. 确定 AuthContext JSON Schema 和共享 Go/Python 类型；
2. 将所有调用方一次性迁移到新契约；
3. 删除 `user_type` 字段、helper、条件分支和旧文档；
4. 删除 `tenant_id=null` / `tenant_id=0` 的平台全权语义；
5. 完成 System、common、common-python、Gateway 和 owner 模块契约测试后再切换运行路径。

不允许旧 AuthContext 与新 AuthContext 按客户端、路由或配置双轨共存。
