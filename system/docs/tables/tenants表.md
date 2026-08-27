# tenants 表与 Platform Tenant API

Tenant 是 ADDP 的最高业务隔离边界。User 通过 `tenant_memberships` 加入一个或多个 Tenant；Tenant 不保存管理员账号，也不在创建时隐式授予 Role。

## 表结构

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | bigint | PK | Tenant ID |
| `code` | text | NOT NULL, UNIQUE, 小写非空 | 稳定代码 |
| `name` | text | NOT NULL, 非空 | 展示名称 |
| `description` | text | NOT NULL | 描述 |
| `status` | text | `active/suspended/closed` | 生命周期状态 |
| `created_at` | timestamptz | NOT NULL | 创建时间 |
| `updated_at` | timestamptz | NOT NULL | 更新时间 |

Tenant 关闭是终态；暂停和恢复是受控状态转换。状态变化会影响 Membership 和会话可用性，不能通过删除数据库行代替。

## 当前 User 管理 API

| 方法 | 路径 | Permission |
| --- | --- | --- |
| `GET` | `/api/v1/system/platform/tenants` | `platform.tenant.read` |
| `POST` | `/api/v1/system/platform/tenants` | `platform.tenant.create` |
| `GET` | `/api/v1/system/platform/tenants/{id}` | `platform.tenant.read` |
| `PUT` | `/api/v1/system/platform/tenants/{id}` | `platform.tenant.update` |
| `POST` | `/api/v1/system/platform/tenants/{id}/suspend` | `platform.tenant.suspend` |
| `POST` | `/api/v1/system/platform/tenants/{id}/restore` | `platform.tenant.restore` |
| `POST` | `/api/v1/system/platform/tenants/{id}/close` | `platform.tenant.close` |

这些 API 只接受 Platform Context 中的 User First-Party Token，并由平台系统管理员 Role 的精确 Permission 控制。平台角色本身不获得任何 Tenant 业务数据访问权。

## Service Runtime 租户发现 API

| 方法 | 路径 | Permission | 凭据与语义 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/system/runtime/tenants` | `platform.tenant.read` | Platform Service Context；供内置 Runtime 发现已初始化的 active Tenant |

该 Runtime 路由使用与 User 管理面相同的分页响应形状，但只接受 Service Access Token，并必须同时满足 Platform Service Context 和 `platform.tenant.read`。Service Principal 不得改用 `/platform/tenants`；User 也不得访问 Runtime 投影。

## 隔离规则

- 所有 Tenant 业务 API 从规范 AuthContext 读取 `context.tenant_id`，不得信任客户端提交的 Tenant ID。
- Department 和 Project Group 是 Tenant 内 Scope，不替代 Tenant 隔离。
- Owner 模块查询必须携带 Tenant 条件；Platform Context 不得隐式转换为“查看全部 Tenant 数据”。
- 跨 Tenant 统计只能通过单独、可审计的统计投影能力实现，不能复用普通业务 Repository 全表读取。

## 相关文档

- [System 数据库架构](../数据库架构.md)
- [ADDP 账号与权限体系](../../../docs/concepts/addp账号与权限体系图.md)
- [System IAM 数据模型与迁移规范](../IAM数据模型与迁移规范.md)
