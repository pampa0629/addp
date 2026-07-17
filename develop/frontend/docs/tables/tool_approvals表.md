# tool_approvals 表

`develop.tool_approvals` 是 Develop 对委托 `workflow.run` 的审批事实源。它保存原始执行请求、请求指纹、申请身份、决定和一次性消费结果；Agent 只保存该记录的 Interaction 投影。

## 字段

| 字段 | 类型 | 约束 | 说明 |
| --- | --- | --- | --- |
| `id` | UUID | 主键 | approval ID |
| `user_id` | BIGINT | 非空、索引 | 发起 Tool 的用户 |
| `tenant_id` | BIGINT | 非空、索引 | 发起 Tool 的租户 |
| `agent_run_id` | VARCHAR(100) | 非空、索引 | 原 AgentRun UUID |
| `tool_call_id` | VARCHAR(100) | 非空 | 首次 `workflow.run` ToolCall |
| `tool_name` | VARCHAR(100) | 非空 | 固定为 `workflow.run` |
| `request_fingerprint` | CHAR(64) | 非空、索引 | canonical JSON 的 SHA-256 |
| `request_payload` | JSONB | 非空 | Develop 持有的原执行请求 |
| `request_summary` | JSONB | 非空 | UI 和 Agent 可读取的最小摘要 |
| `status` | VARCHAR(20) | 非空、索引 | `pending/approved/rejected/expired/consumed` |
| `requested_at` | TIMESTAMPTZ | 非空 | 申请时间 |
| `expires_at` | TIMESTAMPTZ | 非空、索引 | 默认 15 分钟过期 |
| `decided_at` | TIMESTAMPTZ | 可空 | 决定时间 |
| `decided_by_user_id` | BIGINT | 可空 | 作出决定的原申请用户 |
| `consumed_at` | TIMESTAMPTZ | 可空 | 创建 execution 时的一次性消费时间 |
| `execution_id` | VARCHAR(100) | 可空 | 消费后创建的 execution ID |

## 状态机

```text
pending -> approved -> consumed
pending -> rejected
pending -> expired
approved -> expired
```

`approved -> consumed` 只能由携带同一用户、租户、AgentRun 和请求指纹的委托 `workflow.run` 完成。审批 API 只接受第一方或 OAuth User Access Token；Delegated Access Token 和内部身份不得作出决定。

## API

| API | 说明 |
| --- | --- |
| `GET /api/v1/develop/approvals/{id}` | 原申请用户读取审批状态和最小摘要 |
| `POST /api/v1/develop/approvals/{id}/decision` | 原申请用户提交 `approved` 或 `rejected` |
| `POST /api/v1/develop/executions` | 委托 Tool 首次创建审批，或批准后原子消费审批并创建 execution |
