# delegated_access_tokens 表

> 本文记录已确认的目标表语义；当前运行代码将在 IAM 一次性切换时替换，不保留旧字段兼容读取。

## 一、定位

`system.delegated_access_tokens` 保存 Delegated Access Token 的服务端事实。每张票据只代表当前用户的一次 ADDP Tool 调用边界，不创建 Refresh Token Family，不可刷新，也不替代第一方 Web 或 OAuth User Access Token。

Agent 使用当前 User Access Token 调用 `POST /api/v1/system/auth/delegations`。System 从源令牌派生用户、租户、客户端和委托方，只接受 Tool Manifest 已注册的 owner audience 与稳定 Tool 名 Scope，并把 AgentRun / ToolCall 作为必填审计绑定。明文 `addp_dat_` 只在签发响应中返回一次，数据库仅保存 SHA-256 Hash。

## 二、字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | bigint identity | 主键 | 委托令牌记录 ID |
| `token_hash` | char(64) | 非空、唯一索引 | `addp_dat_` 明文令牌的 SHA-256 Hash |
| `source_access_token_id` | bigint | 非空、索引、FK | 签发该委托令牌的 `access_tokens.id` |
| `audience` | text | 非空、索引 | 唯一 owner 模块名 |
| `scopes` | text[] | 非空 | Tool Manifest 的稳定 Tool 名 Scope |
| `agent_run_id` | text | 非空、索引 | Agent Runtime 的逻辑 Run 标识 |
| `tool_call_id` | text | 非空、索引 | 当前 Tool Call 标识；与 AgentRun 组合唯一 |
| `expires_at` | timestamptz | 非空、索引 | 不晚于源 Access Token 和 Family 的最终失效时间 |
| `revoked_at` | timestamptz | 可空、索引 | 源 Access Token 或 Family 撤销时的撤销时间 |
| `created_at` | timestamptz | 非空 | 创建时间 |

Principal、Tenant Membership、Client、`delegated_by_client_id` 和授权版本不在本表重复保存，统一通过 Source Access Token -> Token Family 派生。

## 三、生命周期

1. System 只接受有效的第一方或 OAuth User Access Token 作为源令牌；Resource Access Ticket 和 Delegated Access Token 不能继续委托。
2. System 校验 audience + Scope 已注册、源 OAuth Scope 未被扩大、AgentRun / ToolCall 非空后创建记录。
3. 有效期不超过 `DELEGATED_ACCESS_TOKEN_EXPIRE_MINUTES` 和源 Access Token 剩余有效期。
4. AuthContext 解析同时校验委托令牌、源 Access Token、源 Family、Principal、唯一 Context 和授权版本。
5. 退出、Refresh Token 重用或 Family 撤销时，关联委托令牌同步撤销，并删除 Redis `auth:context:<token_hash>` 缓存。

## 四、安全边界

- Owner 收到 `auth_type=delegated_access_token` 时默认拒绝。
- 只有 Tool Manifest 对应的 method + route 可校验 owner audience 和全部 required scopes 后放行。
- Scope 只缩小权限，Owner 仍必须执行 `user_type`、租户、资源权限和审批校验。
- Principal、Tenant、Client、`delegated_by_client_id` 不接受客户端提交，也不在本表建立可漂移副本。
- Owner 日志和 AgentRunStep 可使用 `agent_run_id` / `tool_call_id` 关联同一次 Tool 调用，但不得记录明文 Token。
