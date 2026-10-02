# common.task_executions 中的 Transfer 执行记录

更新时间：2026-05-31

Transfer 执行记录统一存储在 `common.task_executions`。Transfer API 会将统一执行记录投影为模块 DTO，因此接口响应中仍可看到 `task_id`、`start_time`、`checkpoint_offset`、`checkpoint_state` 等 Transfer 视图字段。

## 一、关联规则

| 字段 | Transfer 语义 |
|---|---|
| `module` | 固定为 `transfer`。 |
| `source` | 默认 `transfer`。如果未来由 Manager 或其他模块直接触发 Transfer execution，应写触发模块。 |
| `source_task_id` | 对应 `transfer.transfer_tasks.id`，按十进制字符串软引用写入。 |
| `tenant_id` | 租户隔离字段。 |
| `status` | `pending`、`running`、`success`、`failed`。 |
| `trigger_type` | `manual` / `scheduled`。只表达手动或定时触发，不表达来源模块、API 通道或重试场景。 |
| `triggered_by` | 触发用户 ID。 |

## 二、指标字段

| 字段 | 说明 |
|---|---|
| `records_read` | 读取行数。table Transfer 主指标；raw copy 第一版固定为 `1`。 |
| `records_written` | 写入行数。table Transfer 主指标；raw copy 第一版固定为 `1`。 |
| `bytes_read` | 读取字节数。当前 table Transfer 通常不作为主指标；raw copy 第一版会写入该指标。 |
| `bytes_written` | 写入字节数。raw copy 第一版会写入该指标。 |
| `started_at` / `completed_at` | 执行开始和完成时间。 |

## 三、metadata 中的 checkpoint 字段

Transfer 将 checkpoint 观测信息写入 `metadata`：

```json
{
  "checkpoint_offset": 20000,
  "checkpoint_state": {
    "version": "v1",
    "batch_index": 2,
    "source_offset": 10000,
    "records_read": 20000,
    "records_written": 20000,
    "target_committed": true,
    "resume_marker": {
      "version": "resume.marker/v1",
      "provider": "parquet.scope_table_reader",
      "position_unit": "ref_row",
      "read_position": {
        "ref": "dataset/part-001.parquet",
        "ref_index": 1,
        "row_offset": 10000,
        "rows_read": 20000
      }
    },
    "commit_marker": {
      "version": "resume.marker/v1",
      "provider": "postgresql.table_write_session",
      "position_unit": "session_commit",
      "commit_position": {
        "rows_committed": 20000,
        "batches_committed": 2
      }
    }
  }
}
```

规则：

1. checkpoint 只在目标 batch 写入成功后更新。
2. `checkpoint_offset` 当前等于累计 `records_read`；raw copy 第一版完成后为 `1`。
3. `checkpoint_state` 用于进度展示、故障定位和 provider marker 持久化。
4. Transfer 只保存 `resume_marker` / `commit_marker`，不解析 marker 内部位置字段。
5. 保存 marker 不表示当前执行可从 checkpoint 后自动恢复。

## 四、执行过程事件和错误

bounded 执行的开始、批次计数和终态写入共享 `common.execution_events`，按 execution + attempt 关联，并在当前有效租约下写入。事件只保存闭合类型和非负整数计数，不保存 source offset、checkpoint marker、自由文本或业务数据。每个 attempt 每 UTC 日最多 1000 条（含截断标记）；接口使用 ID 游标，每页最多 100 条。Monitor 和 Transfer 复用共享运行过程组件，不从日志文字推断后处理结果。

过程事件保留 30 天，由 System 的公共存储维护循环分批清理。概览和步骤暂不自动删除；180 天概览目标仍待 Owner 引用裁决落地。连续执行暂未接入事件，不允许绕过既有 fencing 写入。错误事实继续位于 `error_details`，Monitor 只提供安全投影，领域诊断由 Transfer 负责。

Backend schema 版本 2 单向删除历史 `metadata.execution_logs` 和 `error_details.logs`，保留其他字段。旧文本不转换为结构化事实，不再保留日志 API 或追加路径。

## 五、恢复语义

Transfer 当前恢复能力分三档：

| 等级 | 当前状态 | 说明 |
|---|---|---|
| observable | 已支持 | checkpoint 用于进度展示和故障定位。 |
| restartable | 已支持 | 失败执行 retry 创建新 execution 并从头执行。 |
| resumable | 未进入主链路 | 需要 source seek、target 幂等提交和 provider marker 消费同时满足。 |

`POST /api/v1/transfer/executions/:execution_id/retry` 当前语义：

- 仅重试失败 execution。
- 新建一条 execution。
- 不携带旧 execution 的 checkpoint_state 继续写。
- overwrite / 默认模式可以重试。
- append 模式拒绝重试，避免重复写入。

因此，文档和 UI 不应宣称 Transfer 已支持“从中断点继续写入”。正确说法是：当前已支持 checkpoint 观测和 restartable retry；checkpoint resumable 尚未进入主链路。

## 六、API

路由前缀：`/api/v1/transfer`。

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/executions` | 查询租户下 Transfer 执行记录。 |
| `GET` | `/executions/statistics` | 查询执行统计。 |
| `GET` | `/executions/:execution_id` | TaskProvider 标准执行详情入口，按统一 `common.task_executions.execution_id` 查询。 |
| `POST` | `/executions/:execution_id/retry` | 按统一 `common.task_executions.execution_id` 和 restartable 语义重试失败执行。 |
| `GET` | `/executions/:execution_id/progress` | 按统一 `common.task_executions.execution_id` 查询执行进度。 |
| `GET` | `/executions/:execution_id/events` | 按统一 `common.task_executions.execution_id` 查询安全过程事件。 |
| `GET` | `/task-definitions/:id/executions` | 查询某个任务的执行记录。 |
