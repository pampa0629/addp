# notebook_session_authorizations 表

> 本文记录当前正式表语义；Notebook 会话的目录发现和只读执行授权派生只使用这一授权事实，不保存 Token，也不保留其他委托路径。

## 一、定位

`system.notebook_session_authorizations` 保存 Notebook Session Authorization。Develop 在创建隔离 Notebook Session 时，使用当前 Tenant User Access Token 向 System 派生一条绑定 Session、Task、Tenant、User、Membership、Token Family、授权版本、允许操作和有效期的短期授权事实。

Kernel 只持有 Develop 签发的 `addp_nkc_` Notebook Kernel Capability；Develop 只在内存 Session 中保存本表 ID，并由固定 `addp-develop` Tenant Service Principal 代表该 Session 消费。表中不保存 User Access Token、Service Access Token、Kernel Capability、Engine 列表或连接信息。

## 二、字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | uuid | 主键 | Notebook Session Authorization ID |
| `session_id` | uuid | 非空、唯一 | 唯一 Notebook Interactive Session |
| `task_id` | bigint | 非空、正整数 | Session 所属 Notebook DevTask |
| `actor_principal_id` | bigint | 非空、索引、FK | 创建 Session 的 User Principal |
| `tenant_id` | bigint | 非空、索引、FK | Session 的 Tenant 隔离边界 |
| `tenant_membership_id` | bigint | 非空、索引、FK | 签发时选择的 Tenant Membership |
| `token_family_id` | bigint | 非空、索引、FK | 派生授权的 Refresh Token Family |
| `issued_authorization_version` | bigint | 非空、正整数 | 签发时 User Principal 授权版本 |
| `audience` | text | 固定 `develop` | 唯一消费方模块 |
| `operations` | text[] | 固定有序集合 | `catalog.list_children`、`execution_engine_access.derive` |
| `expires_at` | timestamptz | 非空、索引 | 最长一小时，且不晚于 Token Family |
| `revoked_at` | timestamptz | 可空、索引 | Session 关闭或 Family 撤销时间 |
| `revoked_reason` | text | 与 `revoked_at` 同时为空或非空 | 稳定撤销原因 |
| `created_at` | timestamptz | 非空 | 创建时间 |

授权身份、Session 边界和有效期创建后不可修改；撤销事实一旦写入也不可修改。记录禁止物理删除。

## 三、生命周期

1. Develop 打开 Runtime 后，使用创建 Session 的 User Access Token 调用 `POST /api/v1/system/auth/notebook-session-authorizations`。System 在事务内复核 User、Tenant Context、Membership、Token Family、授权版本和 `system.engine.read`，然后创建授权和审计事件。
2. 签发成功后，Develop 仅保存授权 ID；签发失败时回收刚创建的 Runtime，不留下无授权 Session。
3. 每次 Catalog 请求由 Kernel Capability 到达 Develop，再由 `addp-develop` Tenant Service Access Token 调用 `POST /api/v1/system/notebook-session-authorizations/{id}/catalog/children`。
4. 每次查询或扫描使用新的 execution ID 调用 `POST /api/v1/system/notebook-session-authorizations/{id}/execution-engine-accesses`；System 原子创建独立只读 Execution Authorization，以 `execution_authorizations.source_notebook_session_authorization_id` 记录本表 ID，并返回执行期 Engine Access。
5. System 每次实时复核授权事实、Session、Tenant、User、Membership、Token Family、授权版本、当前读取 Permission、Engine 归属和状态。
6. 普通 Access/Refresh Token 轮换不撤销本授权；退出、Refresh Token Family 撤销、User 或 Tenant 停用、Membership 失效、权限变化、授权版本变化、Session 关闭或到期都会 fail-closed。Family 或 Session 撤销会联动撤销本 Session 已派生且仍有效的 Execution Authorization，后续标准 Engine Access 租约复核也会沿来源外键重新校验本表和 Token Family。
7. Develop 关闭、回收或停止 Session 时，先移除本地 Session 并取消活动查询，再幂等调用撤销接口并关闭 Runtime。

## 四、安全边界

- `system.notebook_session_authorization.execute` 只授予 `tenant.develop_runtime`，消费 API 同时要求 Service Principal、Tenant Context 和固定 `addp-develop` OAuth Client。
- Develop Runtime Role 不获得通用 `system.engine.read`；用户权限通过本表的派生授权逐次复核。
- 授权不冻结 Engine 列表。跨 Tenant、错误 Session、未知授权或已失效授权统一拒绝，不暴露记录是否存在。
- Catalog 请求只返回统一 Catalog 契约下的脱敏目录事实。执行期连接只返回给 Develop 受控 Runtime，不返回 Kernel、Notebook 内容或浏览器。
- 每次签发、消费和撤销均写入 IAM 审计；明文 Token 和 Kernel Capability 不进入审计详情。
