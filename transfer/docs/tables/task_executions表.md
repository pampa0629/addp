# common.task_executions 中的 Transfer 执行记录

更新时间：2026-10-31

Transfer 执行记录统一存储在 `common.task_executions`。本文说明保存的关联、计数和 checkpoint 事实；HTTP 公开字段与读取身份统一见[Transfer 专业读取与响应](../../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)。专业 DTO 保留声明的执行身份、时间、计数和安全诊断，不返回原始 checkpoint；持久字段不自动成为响应字段。

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

Transfer 将 checkpoint 观测信息写入 `metadata`。以下仅为持久结构示例，不是公开 DTO 或恢复通过证据：

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

字段口径、batch 成功后更新、provider marker 的生成与解释职责，以及 snapshot 只可观测的边界，统一见[Checkpoint、进度和重试](../transfer-基本概念及配置说明.md#十checkpoint进度和重试)。保存 marker 不表示当前执行可自动续写。

## 四、执行过程事件和错误

bounded 执行的开始、批次计数和终态，以及 continuous 的实际 claim、累计提交计数和终态，写入共享 `common.execution_events`，按 execution + attempt 关联，bounded 在当前有效 execution lease 下写入，continuous 由 Transfer 的 Runtime lease/fencing 同事务裁决；过期终态由 Owner supervisor 收敛，未开始的 pending 取消只保留概览。事件只保存闭合类型和非负整数计数，不保存 source offset、checkpoint marker、自由文本或业务数据。每个 attempt 每 UTC 日最多 1000 条（含截断标记）；接口使用 ID 游标，每页最多 100 条。Monitor 和 Transfer 复用共享运行过程组件，不从日志文字推断后处理结果。

过程事件保留 30 天，由 System 的公共存储维护循环分批清理。概览和步骤暂不自动删除；180 天概览目标仍待 Owner 引用裁决落地。连续执行复用相同事件与保留规则，不允许绕过既有 fencing 写入。错误事实继续位于 `error_details`，Monitor 只提供安全投影，领域诊断由 Transfer 负责。

Backend schema 版本 2 单向删除历史 `metadata.execution_logs` 和 `error_details.logs`，保留其他字段。旧文本不转换为结构化事实，不再保留日志 API 或追加路径。

## 五、恢复语义

恢复分级统一见[配置契约](../transfer-基本概念及配置说明.md#恢复分级)：失败 snapshot retry 创建新的 execution 并从头执行，不携带旧 `checkpoint_state` 继续写入，append 拒绝重试。写入策略沿[策略正文](../transfer-基本概念及配置说明.md#六写入策略)。

watermark incremental 的跨 execution resume 使用独立 `transfer.sync_states` 的 committed position；支持范围、稳定键与目标幂等要求见[增量同步契约](../transfer-基本概念及配置说明.md#七watermark-bounded-incremental)。这不表示 snapshot checkpoint 已支持断点续写。格式级 marker 恢复还须源定位、目标幂等提交和 provider 消费同时满足，条件见[格式恢复契约](../../../docs/spec/addp数据类型与格式能力规范.md#checkpoint--resume-契约)。

失败 execution UUID、重试响应中的新 UUID 与实际观察步骤见[快速开始](../快速开始.md#四checkpoint-和重试)。结构样例、marker 持久化或确定性回归不等于真实恢复、权限及目标数据已验收；标准入口、历史结果和未覆盖项见[模块验证](../../CLAUDE.md#开发与验证)，未运行、失败或跳过不计为通过。

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
