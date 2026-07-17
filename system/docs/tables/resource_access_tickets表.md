# resource_access_tickets 表

## 一、定位

`system.resource_access_tickets` 保存 Browser Resource Access Ticket 的服务端事实。票据只用于浏览器无法设置 `Authorization` Header 的原生 GET/HEAD 资源请求，不替代 User Access Token，也不允许普通 CRUD、搜索、任务或写 API 使用。

System 在第一方 Web 登录和每次 Web Refresh Token 轮换事务中，为允许的 Owner 各签发一张随机 opaque 票据。明文只通过 Owner Path 限定的 HttpOnly Cookie 下发，数据库仅保存 SHA-256 Hash。

## 二、字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | uuid | 主键 | 票据记录 ID |
| `token_hash` | char(64) | 非空、唯一索引 | `addp_rat_` 明文票据的 SHA-256 Hash |
| `family_id` | uuid | 非空、索引 | 所属 `refresh_token_families.id` |
| `owner` | varchar(100) | 非空、索引 | 可消费票据的 Owner 模块，例如 `manager`、`standard` |
| `expires_at` | timestamp | 非空、索引 | 服务端最终失效时间 |
| `revoked_at` | timestamp | 可空、索引 | 轮换、退出或 Family 撤销时间 |
| `created_at` | timestamp | 非空 | 创建时间 |

## 三、生命周期

1. 第一方登录创建 Refresh Token Family、Access Token 和每个 Owner 的 Resource Access Ticket。
2. Web Refresh 在同一事务内撤销旧票据并创建新票据。
3. 票据有效期不超过 `RESOURCE_ACCESS_TICKET_EXPIRE_MINUTES`、当前 Access Token 和 Family 的剩余有效期。
4. 退出、Refresh Token 重用或 Family 撤销时，所有关联票据同步撤销并清除 Redis AuthContext 缓存。
5. OAuth Authorization Code、Device Flow 和 OAuth Refresh Token 不创建浏览器资源票据。

## 四、安全边界

- Cookie 名称固定为 `addp_resource_access_ticket`，每个 Owner 使用独立 Path，例如 `/api/v1/manager`。
- JavaScript 不可读取票据，响应 JSON 不返回票据。
- Owner 先校验 GET/HEAD 和路由白名单，再解析 Cookie；普通 API 不接受票据。
- AuthContext 必须为 `auth_type=resource_access_ticket`，audience 必须包含当前 Owner，scope 必须包含 `resource:read`。
- Owner Handler 仍按 AuthContext 执行租户隔离和资源权限校验。
