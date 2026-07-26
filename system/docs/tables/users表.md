# users 表与 User API

`system.users` 只保存全局自然人资料。授权主体事实位于 `system.principals`，本地登录凭据位于 `system.local_accounts`，Tenant 关系位于 `system.tenant_memberships`，Role 位于 `system.role_assignments`。这些事实不得重新合并进 User 表。

## 表结构

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | bigint | PK, FK → `system.principals.id` | 与 Principal 共用身份 ID |
| `display_name` | text | NOT NULL, 非空 | 展示名称 |
| `primary_email` | text | NULLABLE | 主邮箱 |
| `locale` | text | NULLABLE | 用户语言偏好 |
| `created_at` | timestamptz | NOT NULL | 创建时间 |
| `updated_at` | timestamptz | NOT NULL | 更新时间 |

`system.local_accounts` 包含 `user_id`、`username`、`normalized_username`、`password_hash`、账号状态、锁定时间和密码变更时间。密码 Hash 不进入 User DTO、AuthContext 或审计详情。

## 当前 API

| 方法 | 路径 | 授权模式 | Permission |
| --- | --- | --- | --- |
| `GET` | `/api/v1/system/users/me` | `self` | 无业务 Permission |
| `PUT` | `/api/v1/system/users/me/password` | `self` | 无业务 Permission |
| `GET` | `/api/v1/system/platform/users` | `permission` | `iam.user.read` |
| `POST` | `/api/v1/system/platform/users` | `permission` | `iam.user.create` |
| `GET` | `/api/v1/system/platform/users/{id}` | `permission` | `iam.user.read` |
| `PUT` | `/api/v1/system/platform/users/{id}` | `permission` | `iam.user.update` |
| `POST` | `/api/v1/system/platform/users/{id}/suspend` | `permission` | `iam.user.suspend` |
| `POST` | `/api/v1/system/platform/users/{id}/reactivate` | `permission` | `iam.user.reactivate` |

`GET /users/me` 只返回 User/Profile 和可空的 Local Account 展示用户名，不返回 Principal 状态、Tenant Membership、Role 或 Permission。当前上下文必须调用 `/auth/context`。

平台 User 管理只处理全局身份生命周期，不自动创建 Tenant Membership 或授予业务 Role。需要 Tenant 访问时必须走独立 Membership 和 Role Assignment 流程。

## 登录与安全

- 本地登录使用 `POST /api/v1/system/login`，认证成功后进入 Context Selection。
- 不开放匿名注册；Tenant Invitation Enrollment 是唯一受控自助建号路径。
- 当前用户改密要求验证现密码，成功后递增授权版本并撤销既有 Token Family。
- 平台身份停用属于三员治理动作，必须通过 `privileged_change_requests` 审批执行。
- 平台三员只能通过一次性离线 Bootstrap 首次建立，角色相互冲突。

## 相关文档

- [System 数据库架构](../数据库架构.md)
- [ADDP IAM 目标数据模型设计](../../../docs/next/addp-IAM目标数据模型设计.md)
- [ADDP 授权上下文规范](../../../docs/spec/addp授权上下文规范.md)
