# ADDP 授权上下文规范

更新日期：2026-07-16

状态：正式规范。本规范定义 ADDP 用户访问令牌的唯一解析结果和模块消费路径；OAuth PKCE、Device Flow 和 Refresh Token Family 见 `docs/spec/addp OAuth授权规范.md`。

## 一、目标

ADDP 只保留一套用户和租户事实：

- `system.users` 是用户身份、激活状态和 `user_type` 的事实源；
- `system.tenants` 是租户身份和激活状态的事实源；
- OAuth Client 只表达客户端软件，不创建 OAuth 用户或 OAuth 租户；
- Scope 只能缩小令牌能力，不能提升用户原有权限。

业务模块不自行解析 JWT、OAuth 或受委托令牌，只消费 System 返回的 AuthContext。

## 二、唯一调用链

```text
Authorization: Bearer <access_token>
  -> System Token Service
  -> 验证签名、有效期和撤销状态
  -> 回查 system.users / system.tenants
  -> AuthContext
  -> common Go / common-python 认证中间件
  -> owner 模块租户、Scope、资源和审批校验
```

以下旧路径在迁移完成后删除，不保留双轨：

- Go 认证中间件通过 `/users/me` 推断令牌身份；
- Agent 或其他 Python 模块自行使用 `JWT_SECRET` 解析用户令牌；
- 各模块自行定义 claims、Scope 或 SuperAdmin 租户语义。

`GET /api/v1/system/users/me` 继续作为当前用户资料资源，不是 Token 验证接口。

## 三、AuthContext 契约

System 提供：

```text
GET /api/v1/system/auth/context
Authorization: Bearer <access_token>
```

成功响应为直接对象：

```json
{
  "subject_type": "user",
  "user_id": 12,
  "username": "alice",
  "user_type": "tenant_admin",
  "tenant_id": 3,
  "auth_type": "first_party_access_token",
  "client_id": null,
  "audiences": [],
  "scopes": [],
  "delegated_by": null,
  "agent_run_id": null,
  "tool_call_id": null,
  "issued_at": "2026-07-16T08:00:00Z",
  "expires_at": "2026-07-16T08:15:00Z"
}
```

字段规则：

| 字段 | 规则 |
| --- | --- |
| `subject_type` | 用户访问令牌固定为 `user`。 |
| `user_id` / `username` / `user_type` | 由当前 `system.users` 记录生成，不信任客户端参数。 |
| `tenant_id` | 普通用户和租户管理员必须为当前用户所属租户；SuperAdmin 为 `null`。 |
| `auth_type` | 识别第一方、OAuth 或受委托访问令牌。 |
| `client_id` | OAuth 或受委托令牌的客户端身份；未绑定客户端时为 `null`。 |
| `audiences` | 令牌允许访问的目标服务。 |
| `scopes` | 令牌权限上限；不取代 `user_type` 和 owner 资源权限。 |
| `delegated_by` | 受委托令牌的委托方，否则为 `null`。 |
| `agent_run_id` / `tool_call_id` | 受委托 Agent Tool 调用的可选审计绑定。 |
| `issued_at` / `expires_at` | 必须为带时区的 ISO 8601 时间。 |

当前第一方 JWT 在 OAuth 签发切换前返回空 `audiences` / `scopes` 和 `client_id=null`；owner 模块不得伪造 Scope。OAuth 令牌实现后这些字段由 System Token Service 权威填充。

## 四、身份和租户校验

System 生成 AuthContext 前必须校验：

1. Token 签名、签名算法和有效期正确。
2. `user_id` 对应用户存在且 `is_active=true`。
3. 令牌中租户与用户当前 `tenant_id` 一致。
4. 非 SuperAdmin 的租户存在且 `is_active=true`。
5. SuperAdmin 必须没有租户；不得把 `tenant_id=0` 解释为 Agent 隐式访问所有租户数据。

任一校验失败均不返回部分 AuthContext。

## 五、权限计算

有效权限是以下条件的交集：

```text
用户有效
  ∩ 租户有效
  ∩ user_type 允许
  ∩ audience 允许
  ∩ scope 允许
  ∩ owner 资源权限允许
  ∩ 必要的服务端审批已完成
```

Scope 只能缩小权限。例如普通用户获得 `workflow.execute` Scope 后，仍只能访问其租户内、owner 模块允许的资源。

## 六、共享中间件

### 6.1 Go

`common/middleware/auth` 负责：

- 把 Bearer Token 转发给 System AuthContext API；
- 将 AuthContext 注入 Gin Context；
- 保留 `user_id`、`username`、`tenant_id` 现有稳定读取 helper；
- 提供 `user_type`、`client_id`、`audiences`、`scopes` 和完整 AuthContext 的统一 helper。

缓存 Key 只使用 Token SHA-256，不保存明文 Token。实际缓存时间不得超过 Access Token 剩余有效期。

### 6.2 Python

`common-python/addp_common/auth.py` 负责调用 System AuthContext API 并生成不可变 `AuthorizationContext`。Agent、Copilot 等 Python 模块不保留私有 JWT 解析器或私有用户身份 DTO。

## 七、OAuth 与 Refresh Token 目标

- CLI 有浏览器时使用 Authorization Code + PKCE。
- 无浏览器或跨设备时使用 Device Authorization Flow。
- 公共客户端不使用可内置的统一 Client Secret。
- Refresh Token 只保存 Hash，每次刷新必须轮换。
- 已轮换 Refresh Token 被重复使用时，撤销整个 Token Family。
- CLI 只把 Refresh Token 保存到 OS Keychain，Access Token 保持短期。

OAuth 数据模型不复用 `system.applications/api_keys`：API Key 表达应用身份，OAuth Client 表达获得用户授权的客户端软件，两者生命周期和审计语义不同。

## 八、禁止事项

- 外部 Agent 使用 `INTERNAL_API_KEY`。
- 在 CLI、Agent 或 owner 模块内保存 `JWT_SECRET`。
- 客户端提交 `user_id`、`tenant_id`、`user_type` 并被服务端信任。
- 用 API Key 模拟用户委托身份。
- 通过 Scope 提升 `user_type` 或跨租户权限。
- 业务模块从 Token 字符串、日志或前端状态反推授权上下文。
