# common.task_executions 中的 Transfer 执行记录

更新时间：2026-05-31

Transfer 执行记录统一存储在 `common.task_executions`。Transfer API 会将统一执行记录投影为模块 DTO，因此接口响应中仍可看到 `task_id`、`start_time`、`checkpoint_offset`、`checkpoint_state` 等 Transfer 视图字段。

## 一、关联规则

| 字段 | Transfer 语义 |
|---|---|
| `module` | 固定为 `transfer`。 |
| `source` | 默认 `transfer`。如果未来由 Manager 或其他模块直接触发 Transfer execution，应写触发模块。 |
| `source_task_id` | 对应 `transfer.transfer_tasks.id`。 |
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

## 四、error_details 中的日志和错误

Transfer 将失败信息和简短执行日志写入 `error_details`：

| 字段 | 说明 |
|---|---|
| `message` | 失败错误消息。 |
| `logs` | 执行过程中的简短日志。 |

如果后续日志量增大，应拆到独立日志表或对象存储；当前表内日志只作为执行详情辅助信息。

## 五、恢复语义

Transfer 当前恢复能力分三档：

| 等级 | 当前状态 | 说明 |
|---|---|---|
| observable | 已支持 | checkpoint 用于进度展示和故障定位。 |
| restartable | 已支持 | 失败执行 retry 创建新 execution 并从头执行。 |
| resumable | 未进入主链路 | 需要 source seek、target 幂等提交和 provider marker 消费同时满足。 |

`POST /api/v1/transfer/executions/:id/retry` 当前语义：

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
| `GET` | `/executions/:id` | 查询执行详情。 |
| `POST` | `/executions/:id/cancel` | 取消执行。 |
| `POST` | `/executions/:id/retry` | 按 restartable 语义重试失败执行。 |
| `GET` | `/executions/:id/progress` | 查询执行进度。 |
| `GET` | `/executions/:id/logs` | 查询执行日志。 |
| `GET` | `/tasks/:id/executions` | 查询某个任务的执行记录。 |
