# search_histories 表结构和 API 说明

## 归属与记录时机

`manager.search_histories` 保存当前租户内当前 User 的个人搜索历史。写入、列表、删除和容量淘汰都使用已认证的 Tenant Context 和 User ID。User 切换租户后使用该租户下自己的列表，切回后恢复原列表。

混合检索的首次请求成功时记录关键词，包含零命中；续页和失败请求不写入。去掉关键词首尾空白后，同一租户和用户的相同词只保存一条，重复搜索只更新时间。每组保留最近 50 条，按更新时间和记录 ID 倒序。

## 表与迁移

| 字段 | 规则 |
| --- | --- |
| `id` | 稳定记录 ID |
| `tenant_id`、`user_id` | 必填的正整数 |
| `query` | 必填，最长 512 字符，与租户和用户组成联合唯一键 |
| `created_at`、`updated_at` | 原记录创建时间和最近记录时间 |

[049 迁移](../../backend/migrations/049_scope_search_history_to_tenant.sql)保留拥有有效租户与用户 ID 的旧行，包括 ID、关键词和时间，按该行已有租户归属保留。旧唯一键按用户及关键词去重，因此同一词在其他租户被覆盖的历史无法恢复。迁移删除租户或用户 ID 缺失、非正数的行，安装联合唯一索引 `idx_search_history_tenant_user_query`。迁移及其校验和同事务提交，后续启动不会重复改动已迁移数据。

MySQL 新装使用原生 InnoDB 表。个人历史的唯一键按大小写和尾随空格精确比较。并发写入同一用户历史时，MySQL 在事务内锁定该租户和用户的归属行，PG 使用已有事务级 advisory lock，随后共享更新与容量淘汰逻辑。真实 MySQL 用例纳入 `platform-manager-storage` 门禁。

## API

| 入口 | 行为 |
| --- | --- |
| `GET /api/v1/manager/search/history` | 返回当前范围的 `data.items`，每项含 ID、搜索词及时间。默认 10 条，最多 50 条，响应使用 `Cache-Control: no-store` |
| `DELETE /api/v1/manager/search/history/:id` | 删除当前范围的一条记录并返回 204。记录不在当前范围时同样返回 204 |
| `DELETE /api/v1/manager/search/history` | 只清空当前范围，返回 204 |

客户端无需提交 Tenant 或 User 选择器。授权主体和租户成员身份由认证中间件校验。

迁移及持久化边界通过 `make test-manager-postgres` 验证，HTTP 合约沿 `make test-manager-backend`。
