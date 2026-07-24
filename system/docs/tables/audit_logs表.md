# audit_logs 表结构说明

更新日期：2026-07-24

状态：IAM 目标表契约；运行时查询 API 与中间件尚待随 IAM Runtime 一次性切换，不保留旧 `user_id / username / request_body / query_params` 兼容字段。

## 一、定位

`system.audit_logs` 是 ADDP 的统一追加式审计事实表，也是 IAM 与 OAuth 安全事件的唯一持久化落点。它同时容纳：

- 身份、Membership、Role、授权版本、Token 撤销和 Bootstrap 等领域安全事件；
- OAuth 协议状态转换与安全失败；
- 各模块经过脱敏的通用 HTTP 操作事件。

不建立 IAM 专用表或 OAuth 专用表。访问日志、应用调试日志和错误堆栈不属于该表。

## 二、目标字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | bigint identity | PK | 事件 ID |
| `principal_id` | bigint | nullable, FK -> `principals.id` | 行为 Principal；匿名或未建立身份时为空 |
| `principal_type` | text | nullable, `user \| service_principal` | Principal 类型快照，非空时必须与 Principal 一致 |
| `context_type` | text | nullable, `platform \| tenant` | 已建立的 AuthContext；空不是 Platform Context |
| `tenant_id` | bigint | nullable, FK -> `tenants.id` | Tenant Context 必填，其他情况为空 |
| `event_name` | text | not null | 稳定事件名 |
| `result` | text | not null | `succeeded \| failed \| denied \| ignored` |
| `risk_level` | text | not null | `low \| medium \| high \| critical` |
| `module_name` | text | not null | 事件 owner 模块 |
| `http_method` | text | nullable | HTTP 方法，非 HTTP 事件为空 |
| `resource_path` | text | nullable | 不含 query 的路由路径 |
| `http_status` | integer | nullable | `100..599` |
| `request_id` | text | nullable | 请求追踪 ID |
| `ip_address` | inet | nullable | 客户端 IP |
| `user_agent` | text | nullable | 客户端 User-Agent |
| `entity_type` | text | nullable | 业务实体类型 |
| `entity_id` | text | nullable | 业务实体稳定 ID |
| `details` | jsonb | not null, default `{}` | 结构化、脱敏且可供审计管理员读取的对象 |
| `created_at` | timestamptz | not null | 数据库生成的事件时间 |

约束关系：

```text
platform -> tenant_id is null
tenant   -> tenant_id is not null
null     -> tenant_id is null

principal_id is null -> principal_type is null
principal_id is set  -> principal_type is set and matches principals.principal_type
```

`context_type=NULL` 用于登录失败、OAuth 前置失败或纯内部安全事件，表示事件发生时不存在已建立的授权上下文。事件来源由 `module_name` 和 `event_name` 表达，不新增 `anonymous` 或 `internal` 会话模式。

## 三、写入与事务

身份、Membership、Role、授权版本、Token Family 撤销、Bootstrap 和 OAuth 状态转换必须在修改权威事实的同一个 PostgreSQL 事务内 INSERT 审计事件。审计写入失败时整个业务事务回滚。

通用 HTTP 操作事件可以在请求完成后写入，但不得替代领域安全事件。同一安全事实只产生一条领域事件，避免中间件和 Service 双写。

表只允许追加：目标 DDL 无条件拒绝 UPDATE / DELETE / TRUNCATE。归档可以先只读导出；需要物理保留清理时，必须先完成独立的分区、归档校验和数据库职责设计，不能给普通运行时路径预留开关。生产环境最终拆分 migration owner、runtime writer、audit reader 和 maintenance 角色。

## 四、脱敏白名单

`details` 采用白名单构造，不接受整段请求体、query 或错误对象。任何位置都禁止保存：

- 密码、密码 Hash、MFA Secret、Client Secret；
- Access Token、Refresh Token、Delegated Token、Resource Ticket；
- Authorization Code、Device Code、User Code；
- PKCE verifier/challenge、OAuth state；
- Cookie、Authorization Header；
- 原始 OAuth 请求体、原始 query、可能包含安全材料的错误堆栈。

OAuth 事件的 `details` 仅允许 `client_id`、`grant_type`、`decision`、`scope` 和稳定 `error_code`。事件名、结果和风险等级使用独立列。

## 五、索引

| 索引 | 字段与谓词 | 用途 |
| --- | --- | --- |
| `idx_audit_logs_created_at` | `(created_at DESC, id DESC)` | 全局时间线与稳定分页 |
| `idx_audit_logs_tenant_created_at` | `(tenant_id, created_at DESC, id DESC) WHERE tenant_id IS NOT NULL` | Tenant 审计时间线 |
| `idx_audit_logs_principal_created_at` | `(principal_id, created_at DESC, id DESC) WHERE principal_id IS NOT NULL` | Principal 行为追溯和 FK |
| `idx_audit_logs_event_created_at` | `(event_name, created_at DESC, id DESC)` | 安全事件检索 |
| `idx_audit_logs_request_id` | `(request_id) WHERE request_id IS NOT NULL` | 请求链路定位 |
| `idx_audit_logs_entity` | `(entity_type, entity_id, created_at DESC) WHERE entity_type IS NOT NULL AND entity_id IS NOT NULL` | 实体历史追溯 |
| `idx_audit_logs_high_risk_created_at` | `(risk_level, created_at DESC, id DESC) WHERE risk_level IN ('high', 'critical')` | 高风险事件时间线与告警扫描 |

## 六、查询授权

- 平台审计管理员：按独立 Permission 查询、导出平台和跨 Tenant 审计事实；只读，不能修改原始记录。
- Tenant 审计角色：只读取当前 Tenant 范围，必须使用 Tenant AuthContext。
- 平台系统管理员和平台安全管理员：不因管理职责自动获得原始审计内容读取权。
- 普通 User、Service Principal 和 owner 模块：仅能通过受控写入接口追加其职责内事件，不能直接枚举审计事实。

查询范围由 `principal_id + context_type + tenant_id` 和 Permission 决定，不再使用旧 `user_type` 或 `tenant_id=NULL` 推断全局权限。

## 七、相关规范

- [IAM 目标数据模型设计](../../../docs/next/addp-IAM目标数据模型设计.md)
- [ADDP OAuth 授权规范](../../../docs/spec/addp%20OAuth授权规范.md)
- [ADDP 授权上下文规范](../../../docs/spec/addp授权上下文规范.md)
