# common.task_executions 中的 Transfer 执行记录

更新时间：2026-11-02

Transfer 执行记录统一存储在 `common.task_executions`。本文说明关联、计数、checkpoint 及恢复的持久事实；HTTP 公开字段与读取身份统一见[Transfer 专业读取与响应](../../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)，持久字段不自动成为响应字段。

阅读导航：[时间与恢复边界](#execution-与-attempt-的恢复分工)、[checkpoint 样例](#三metadata-中的-checkpoint-字段)、[过程事件与错误](#四执行过程事件和错误)、[恢复类型与位置](#恢复类型与位置)、[自动恢复字段与安全 DTO](#自动恢复字段与安全-dto)、[API 与权限](#六api)、[验证边界](#验证边界)。

## 一、关联规则

| 字段 | Transfer 语义 |
|---|---|
| `module` | 执行 Owner 固定为 `transfer`，不随调用来源改变。 |
| `task_type` | 本文传输执行为 `sync`，用户、模块结果与 TaskProvider 入口均核对该归属，不扩展到其他任务类型。 |
| `source` | 本次调用来源。模块一次性执行已用于 Manager／Develop 等调用方，来源由已认证 `addp-<module>` Service Client 推导，不接受请求自报；流程见[执行记录契约](../transfer-基本概念及配置说明.md#12-执行记录)。 |
| `source_task_id` | 有定义执行按十进制字符串软引用 `transfer.transfer_tasks.id`；模块一次性执行为空，不创建临时任务定义。 |
| `tenant_id` | 租户隔离字段。 |
| `status` | 取值及终态语义统一见公共[执行状态枚举](../../../docs/spec/tasks/执行记录与租约.md#执行状态枚举)。 |
| `trigger_type` | `manual` / `scheduled`。只表达手动或定时触发，不表达来源模块、API 通道或重试场景。 |
| `triggered_by` | 经核验的发起 User 事实，不是机器认证主体或执行授权。服务代发导出从 Owner 核验取得用户；普通无用户来源的机器执行不伪造发起人，见[来源核验](../../../docs/spec/addp登录认证的统一要求.md#一认证事实与唯一主路径)。 |

## 二、指标字段

| 字段 | 说明 |
|---|---|
| `records_read` | 读取行数。table Transfer 主指标；raw copy 第一版固定为 `1`。 |
| `records_written` | 写入行数。table Transfer 主指标；raw copy 第一版固定为 `1`。 |
| `bytes_read` | 读取字节数。当前 table Transfer 通常不作为主指标；raw copy 第一版会写入该指标。 |
| `bytes_written` | 写入字节数。raw copy 第一版会写入该指标。 |
| `created_at` | 本 execution 的创建时间，不等于 Worker 开始时间。 |
| `started_at` | 当前 attempt 的真实开始时间，等待领取的 `pending` 为空。 |
| `completed_at` / `execution_time_ms` | 当前 attempt 的完成时间与耗时，不是所有 attempt 的累计耗时；缺少开始事实时不生成耗时。 |
| `attempt` / `max_attempts` | 本 execution 的运行尝试次数与 bounded 自动恢复的 attempt 上限，恢复分工见下文。 |

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

bounded 执行的开始、批次计数和终态，以及 continuous 的实际 claim、累计提交计数和终态，写入共享 `common.execution_events`，按 execution + attempt 关联。bounded 在当前有效 execution lease 下写入，continuous 由 Transfer 的 Runtime lease/fencing 同事务裁决。过期终态由 Owner supervisor 收敛，未开始的 pending 取消只保留概览。

事件闭合结构、敏感字段禁入、每日预算、ID 游标、读取授权和事件留存窗口统一见[通用执行读取与过程事件](../../../docs/spec/tasks/执行监控.md#通用执行读取与过程事件)，continuous 的事务、attempt、终态安全错误及预算耗尽行为见[continuous 过程事件](../../../docs/spec/tasks/执行监控.md#transfer-continuous-过程事件)。Monitor 和 Transfer 复用共享运行过程组件，不从日志文字推断后处理结果。错误事实仍位于 `error_details`，Monitor 只提供安全投影，领域诊断由 Transfer 负责。

System 的公共存储维护循环分批清理过期过程事件，当前概览和步骤不自动删除。概览保留目标、Owner 引用裁决、只读候选评估与物理删除边界统一见[概览保留与清理契约](../../../docs/spec/tasks/执行监控.md#概览保留与清理契约)，不将目标保留期或只读报告计为已完成删除。

Backend schema 版本 2 单向删除历史 `metadata.execution_logs` 和 `error_details.logs`，保留其他字段。旧文本不转换为结构化事实，不再保留日志 API 或追加路径。

## 五、恢复语义

### execution 与 attempt 的恢复分工

| 场景 | 执行身份与时间边界 | 权威正文 |
| --- | --- | --- |
| bounded 自动恢复 | 满足安全重放与 attempt 上限条件后，原未终态 execution 回到 `pending`，UUID 与 `created_at` 不变。恢复时清空开始、完成、耗时与 lease，下一次领取增加 attempt 并写入新开始时间。 | [公共领取与恢复契约](../../../docs/spec/tasks/执行记录与租约.md#bounded-execution-领取租约和恢复契约)、[公共时间写入规则](../../../docs/spec/tasks/执行记录与租约.md#公共写入入口与权威时间) |
| 失败执行的用户／API retry | 创建新 pending execution，通过 `retry_of_execution_id` 关联原失败执行。原记录保留，新执行的创建、领取与时间独立记录，不作为原 execution 的下一次 attempt。 | [公共 retry 规则](../../../docs/spec/tasks/执行记录与租约.md#bounded-execution-领取租约和恢复契约)、[恢复类型与位置](#恢复类型与位置) |
| continuous 显式或自动恢复 | 新建 pending execution，对应新的 runtime session。旧 execution 不重新变回 `running`，自动恢复另记 `recovered_from_execution_id` 与恢复审计。 | [运行控制与执行身份](../../../docs/spec/tasks/Transfer同步执行.md#运行控制与执行身份)、[continuous 自动恢复](../../../docs/spec/tasks/Transfer同步执行.md#continuous-自动恢复退避与-circuit) |

上述时间与计数属于持久事实，完整字段口径由[公共执行记录](../../../docs/spec/tasks/执行记录与租约.md#执行记录规范)维护。它们不直接扩展[安全 DTO 与读取范围](#自动恢复字段与安全-dto)，也不能据此认定业务恢复成功，实际结果沿[验证边界](#验证边界)核对。

### 恢复类型与位置

snapshot 失败重跑与 watermark 跨 execution 续读统一见[Bounded 执行语义](../transfer-任务语义与同步模式.md#四bounded-执行语义)：失败 snapshot 从头执行，不携带旧 `checkpoint_state` 续写，append 拒绝重试；watermark 使用独立 `transfer.sync_states` 的 committed position，仍须满足已支持源、稳定键及目标幂等要求。checkpoint 与同步主状态的区别见[状态所有权](../transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)，写入策略沿[策略正文](../transfer-基本概念及配置说明.md#六写入策略)。

格式级 marker 恢复还须源定位、目标幂等提交和 provider 消费同时满足，条件见[格式恢复契约](../../../docs/spec/addp数据类型与格式能力规范.md#checkpoint--resume-契约)。保存 marker 不表示 snapshot 已支持断点续写。

### 自动恢复字段与安全 DTO

continuous 恢复 metadata 中，本次恢复的审计事实与可变计数、circuit 状态分别维护；字段清单与重置边界统一见[恢复审计与部署配置](../../../docs/spec/tasks/Transfer同步执行.md#恢复审计与部署配置)，新旧 execution、领取条件及退避规则见[自动恢复契约](../../../docs/spec/tasks/Transfer同步执行.md#continuous-自动恢复退避与-circuit)。恢复字段的存在不额外授予读取权限。

用户专业详情及启动、恢复、回放、重试响应使用同一显式安全投影，不直接公开任意 metadata、原始 checkpoint、执行授权引用或 fencing／租约；允许的恢复诊断与其他敏感信息排除统一见[专业响应契约](../../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)。模块结果与 TaskProvider 使用各自入口和投影，身份、权限及来源隔离见[API 归属](#六api)，不能用专业响应可见字段反推原始持久实体可读。

失败 execution UUID、重试响应中的新 UUID 与实际观察步骤见[快速开始](../快速开始.md#四checkpoint-和重试)，结果判定与证据导航统一见[验证边界](#验证边界)。

## 六、API

路由前缀：`/api/v1/transfer`。用户、模块结果与 TaskProvider 的读取范围和 DTO 统一见[Transfer 专业执行读取与响应](../../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)，下表仅定位各入口。

### 用户专业读取与重试

第一方和普通 OAuth User 使用 `transfer.task.read` 读取；重试使用 `transfer.task.execute`，并独立复核当前执行资格。读取同时限定可信 Tenant、`module=transfer` 和 `task_type=sync`：有定义的历史（含已删除定义）按 Owner 任务范围读取，无定义的一次性执行仅发起 User 可读，无主体证据拒绝。列表、总数和统计适用同一范围；父执行可读不授予子执行读取权。

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/executions` | 查询租户下 Transfer 执行记录。 |
| `GET` | `/executions/statistics` | 查询执行统计。 |
| `GET` | `/executions/:execution_id` | 用户专业执行详情入口，按统一 `common.task_executions.execution_id` 查询。 |
| `POST` | `/executions/:execution_id/retry` | 按统一 `common.task_executions.execution_id` 和 restartable 语义重试失败执行。该执行所属的 CDC 任务处于 `status=blocked` 时返回 `409 Conflict`。可审批的 additive 变更须先完成审批，再由用户显式 Resume 任务。 [审批落库与显式恢复](../../../docs/spec/tasks/Transfer数据库CDC.md#审批落库与显式恢复) |
| `GET` | `/executions/:execution_id/progress` | 按统一 `common.task_executions.execution_id` 查询执行进度。 |
| `GET` | `/executions/:execution_id/events` | 按统一 `common.task_executions.execution_id` 查询安全过程事件。 |
| `GET` | `/task-definitions/:id/executions` | 查询某个任务的执行记录。 |

### 模块一次性执行与来源隔离

以下入口使用当前 Tenant 的模块 Service 身份，创建与读取权限互不替代。来源由 Client ID 推导，结果回查还须匹配同租户、`module=transfer`、`task_type=sync`、无任务定义且 `source` 等于调用模块；其他来源按不存在处理。不能用用户任务读取权或 TaskProvider 权限代替模块结果读取权。

| 方法 | 路径 | 权限与职责 |
| --- | --- | --- |
| `POST` | `/executions` | `transfer.execution.create`：创建无任务定义的 bounded `sync` execution，返回统一 execution UUID。 |
| `GET` | `/executions/:execution_id/result` | `transfer.execution.read`：只回查当前模块创建的一次性结果，使用专用安全结果 DTO。 |

服务身份与发起 User 分开核验；导出会话的 UUID、请求摘要和来源事实按[来源核验契约](../../../docs/spec/addp登录认证的统一要求.md#一认证事实与唯一主路径)处理。来源归属不授予数据访问或执行权限，成功取得结果也不替代后续交付所需的保护校验。

### TaskProvider 机器状态读取

`GET /task-provider/executions/:execution_id` 仅供固定 `addp-orchestrator` Service Client 使用 `transfer.task_provider.read` 读取同租户 Transfer `sync` 执行状态；不套用用户本人条件，也不代替用户专业详情或模块导出结果。TaskProvider 的独立任务与执行契约见[模块核心 API](../../CLAUDE.md#核心-api)和[TaskProvider 规范](../../../docs/spec/tasks/TaskProvider接口.md)。

### 验证边界

上述持久字段、来源、路由和守卫的静态核对不代表真实授权或运行通过。User 读取、模块来源隔离与 TaskProvider 机器入口须按各自身份分别验证；snapshot 重跑、watermark 续读和 continuous 恢复按各自契约核对新旧 execution、适用的已提交位置与目标数据，不能由结构样例、marker 或恢复审计推断运行成功。

测试分层遵循[测试与验收规范](../../../docs/spec/addp测试与验收规范.md#三标准入口)，命令、环境前提与各门禁覆盖范围统一见[README 标准门禁](../../README.md#常规模块与专项门禁)；Oracle CDC 专项缺口、测试隔离和通用持久化要求见[数据库验收导航](../数据库架构.md#持久化与恢复验收)。Go 子集、PostgreSQL T2、runner 回归与真实 Online 链路分别认定。

读取隔离、安全字段及双用户范围见[专业执行读取与响应证据](../../CLAUDE.md#专业执行读取与响应收敛)，事件数据面、浏览器及清理范围见[过程事件证据](../../CLAUDE.md#continuous-过程事件与浏览器验收)；其他固定版本、失败与修复、清理及未覆盖项按[分版本证据索引](../../CLAUDE.md#专项约束与分版本证据)查阅。历史结果仅适用于所列源码与运行尝试，不代表当前 HEAD 已重跑；未运行、失败、跳过或未覆盖项不计为通过。
