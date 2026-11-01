# transfer.transfer_tasks 表说明

更新时间：2026-11-02

`transfer.transfer_tasks` 存储 Transfer 任务定义。任务的执行历史不在本表中保存，统一写入 `common.task_executions`。

阅读导航：[首次扫描](#首次扫描的提交与结果核对)、[配置结构](#二config-字段结构)、[任务状态](#四任务状态)、[用户入口](#用户任务定义与控制)、[TaskProvider 入口](#taskprovider-机器发现与执行)、[执行、恢复与验收](#执行恢复与验收边界)。

## 一、核心字段

| 字段 | 说明 |
|---|---|
| `id` | 任务 ID。 |
| `apply_identity` | 服务端生成且不可修改的全局 UUID；不进入任务 config 或公开请求。continuous PostgreSQL 使用它标识业务目标库 apply ledger 主线。 |
| `tenant_id` | 租户 ID。 |
| `name` | 任务名称。 |
| `description` | 任务描述。 |
| `task_type` | 当前固定为 `sync`；执行主链路以 `config` 为准。 |
| `config` | source / target endpoint JSON。 |
| `schedule` | Cron 表达式；为空表示手动任务。 |
| `batch_size` | 默认批大小。 |
| `enabled` | 定时任务是否启用。 |
| `auto_scan_metadata` | 是否自动触发 Meta deep scan；bounded、业务 Kafka 与数据库 CDC 的触发时机和精确范围统一见[写后扫描契约](../transfer-基本概念及配置说明.md#十一写后-meta-扫描)。 |
| `initial_metadata_scan_status` | continuous 首次目标扫描的私有提交状态：空值、`running`、`success` 或 `failed`；`success` 表示提交成功，结果核对见[首次扫描导航](#首次扫描的提交与结果核对)。 |
| `initial_metadata_scan_claim_token` / `initial_metadata_scan_lease_until` | continuous 首次扫描的 fencing claim；仅 `running` 时有效，领取、接管与 TTL 见[claim 契约](../../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)。 |
| `initial_metadata_scan_attempt` | continuous 首次扫描领取次数。 |
| `initial_metadata_scan_execution_id` / `initial_metadata_scan_error` | 已提交的 Meta execution UUID 或最近一次提交错误；提交反馈与失败边界见[持久化说明](../数据库架构.md#九写后-meta-扫描)。 |
| `status` | `idle`、`running` 或 `blocked`；`blocked` 表示数据库 CDC 当前 generation 被 schema drift 阻塞。只有专用 additive 审批成功后可回到 `idle`，其他变化只允许 Stop。 |
| `desired_state` | continuous runtime 用户期望状态：`running` / `paused` / `stopped`，默认 `stopped`；bounded task 不消费。该字段不能单独判定数据库 CDC 是否永久停止，终态以 `capture_resources.status=stopped` 为准。 |
| `progress` | 当前任务进度百分比。 |
| `created_by` | 创建人 ID。 |
| `last_execution_id` | 最近一次执行 ID。 |
| `last_execution_status` | 最近一次执行状态。 |
| `last_run_at` | 最近运行时间。 |
| `next_run_at` | 下次计划运行时间。 |
| `created_at` / `updated_at` / `deleted_at` | 审计字段。 |

### 首次扫描的提交与结果核对

触发条件、普通 DML／batch 不触发、原生表完整 `targets` 和 encoded 目标 `ref_groups` 的精确范围统一见[写后扫描契约](../transfer-基本概念及配置说明.md#十一写后-meta-扫描)。claim 防重复、失败或过期后的接管及 TTL 要求沿[同步执行规范](../../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)；Meta API 提交失败与仓储／fencing 错误的不同处理见[数据库架构](../数据库架构.md#九写后-meta-扫描)。

上述 `initial_metadata_scan_*` 是私有持久字段，不直接序列化为公开 DTO，也不授予 Meta 读取或执行权限。`success` 或保存的 Meta execution UUID 仅记录提交结果；是否完成、目标 attributes 是否正确，须通过已授权的 Meta 入口核对对应 execution 与目标事实，不能以任务数据面继续运行代替。

bounded 与 continuous 扫描的固定版本、失败和清理结果分别见[SQL ETL 与 bounded 写后扫描](../../CLAUDE.md#sql-etl-与-bounded-写后扫描)、[continuous 精确扫描与审批](../../CLAUDE.md#continuous-精确扫描与审批归属)；标准入口和通用判定见[本表验收导航](#执行恢复与验收边界)，字段、状态或静态接线不作为扫描通过证据。

## 二、config 字段结构

`config` 必须使用新 endpoint 结构：

```json
{
  "runtime": {"boundary": "bounded"},
  "load": {"mode": "snapshot"},
  "source": {
    "locator": "addp://engine/1/path/public/source_roads?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/exports?type=directory",
    "name": "roads.parquet",
    "data_type": "table",
    "representation": "encoded",
    "format": "parquet",
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [
    {
      "type": "field_mapping",
      "version": "v1",
      "mode": "project",
      "fields": [
        {"source": "name", "target": "road_name", "target_type": "string"}
      ]
    }
  ],
  "batch_size": 10000
}
```

旧字段不再兼容：

| 旧字段 | 当前替代 |
|---|---|
| `connector_type` | 通过 endpoint `locator` 解析 System engine。 |
| `source_config` / `target_config` | `source` / `target` endpoint。 |
| `output_format` / `file_type` | encoded endpoint 的 `format`。 |
| 旧 endpoint `engine_id` | endpoint `locator` 中的 engine id。 |
| 任务外层 `mappings` | `config.transforms[type=field_mapping]`。 |
| 顶层 `mode=batch` | `runtime.boundary=bounded` + `load.mode=snapshot`。 |
| `target.policy.write_mode` | `target.policy.apply_mode`；`overwrite` 收敛为 `replace`。 |

## 三、endpoint 规则

| 字段 | 说明 |
|---|---|
| `locator` | source 使用的 ResourceLocator URI，指向已存在资源。 |
| `parent_locator` | target 父 node 的 ResourceLocator URI。 |
| `name` | target 父 node 下待创建或待覆盖的资源名。 |
| `data_type` | 当前稳定主链路为 `table`。 |
| `representation` | `native` 或 `encoded`。 |
| `format` | encoded endpoint 必填。 |
| `options` | 格式读写选项。 |
| `runtime.boundary` | 执行边界：`bounded` 或 `continuous`；continuous 业务 Kafka keyed JSON 契约支持 PostgreSQL/MySQL upsert。 |
| `load.mode` | `snapshot` 或 PostgreSQL/MySQL/OceanBase/openGauss/TiDB native table 的 `incremental`；OceanBase/openGauss/TiDB 限非空间表。 |
| `load.change_detection` | incremental 第一版只支持复合 watermark。 |
| `target.policy.apply_mode` | `replace` / `append` / `upsert`；按任务组合和目标 Provider 校验。 |
| `target.policy.keys` | upsert 的稳定目标键。 |

## 四、任务状态

| 状态 | 说明 |
|---|---|
| `idle` | 未运行或执行已结束。 |
| `running` | 当前有执行正在运行。 |
| `blocked` | 数据库 CDC schema drift 已阻塞当前消息和 offset；禁止直接 start/resume/retry。 |

任务是否成功、失败或重试，应查询 `common.task_executions` 对应记录。

## 五、API

路由前缀：`/api/v1/transfer`。用户任务定义与 TaskProvider 使用独立入口；标准后缀、响应与执行参数统一见 [TaskProvider 契约](../../../docs/spec/tasks/TaskProvider接口.md#标准-endpoint)，任务与执行入口总览见[模块核心 API](../../CLAUDE.md#核心-api)。

### 用户任务定义与控制

Console 与 Transfer 页面使用 `/task-definitions`。每次请求独立校验身份、当前 Tenant 和精确功能 Permission；读取、创建、修改、执行与删除不互相隐含，创建成功不代表获准启动。租户共享与页面动作边界统一见[任务授权](../transfer-基本概念及配置说明.md#13-租户阶段的任务授权)，Permission 声明以 [Manifest](../../authorization/permissions.yaml) 为准。

下表 Permission 均省略 `transfer.task.` 前缀。

| 方法 | 路径 | Permission | 说明 |
|---|---|---|---|
| `GET` | `/task-definitions` | `read` | 当前 Tenant 的用户任务列表，与机器发现入口分离。 |
| `POST` | `/task-definitions` | `create` | 创建任务定义，不等于启动执行。 |
| `GET` | `/task-definitions/statistics` | `read` | 查询任务统计。 |
| `GET` | `/task-definitions/:id` | `read` | 查询任务定义详情。 |
| `PUT` | `/task-definitions/:id` | `update` | 更新任务定义。 |
| `DELETE` | `/task-definitions/:id` | `delete` | 删除任务定义；运行态与私有状态清理前提见[任务删除](../transfer-任务语义与同步模式.md#51-私有状态与任务删除)。 |
| `POST` | `/task-definitions/:id/start` | `execute` | bounded 创建 execution 并入队；continuous 原子设置 running 并创建 pending execution。 |
| `POST` | `/task-definitions/:id/replay` | `execute` | 业务 Kafka 的独立 bounded replay；范围与主状态隔离见[Checkpoint 与 Replay](../transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)。 |
| `POST` | `/task-definitions/:id/pause` | `update` | bounded 关闭 owner schedule；continuous 设置 paused 并通知 runtime 取消 execution。 |
| `POST` | `/task-definitions/:id/resume` | `update` | bounded 恢复 owner schedule；未阻塞的 continuous 设置 running 并创建新 pending execution；CDC `status=blocked` 返回 409。 |
| `POST` | `/task-definitions/:id/stop` | `update` | 仅 continuous：设置 stopped 并通知 runtime 取消 execution。 |
| `GET` | `/task-definitions/:id/schema-change` | `read` | 查询当前数据库 CDC schema change request 与可审批建议。 |
| `POST` | `/task-definitions/:id/schema-change/approve` | `update` | 审批当前 additive request；成功后任务进入 paused，不隐式 Resume。 |
| `GET` | `/task-definitions/:id/dead-letters` | `read` | 查询当前任务的 DLQ 安全控制索引。 |
| `GET` | `/task-definitions/:id/dead-letters/:identity` | `read` | 查询单条控制索引，不返回原始消息或 payload reference。 |
| `GET` | `/task-definitions/:id/executions` | `read` | 查询任务执行记录；专业读取范围见[执行记录表](task_executions表.md)。 |

Agent 委托只沿上述创建入口，必须满足精确 scope/audience 与当前 Tenant 创建权限，并限制为禁用、无调度、无自动扫描的 bounded snapshot native table 任务；它不取得运行授权，也不创建 execution。委托与模块一次性执行的服务身份边界见[模块核心 API](../../CLAUDE.md#核心-api)。

### TaskProvider 机器发现与执行

以下端点只接受固定 `addp-orchestrator` Service Client 与当前 Tenant 上下文，分别要求不可委派的 `transfer.task_provider.read` 或 `transfer.task_provider.execute`；用户的 `transfer.task.*` 不能替代它们。路径占位符采用 TaskProvider 声明格式，`task_type` 当前仅支持 `sync`。

下表 Permission 均省略 `transfer.task_provider.` 前缀。

| 方法 | 路径 | Permission | 说明 |
|---|---|---|---|
| `GET` | `/task-provider/tasks` | `read` | 标准任务发现；无论是否带 `task_type=sync`，均强制只返回 bounded 任务。 |
| `GET` | `/task-provider/tasks/{task_type}/{id}` | `read` | 标准任务详情与 execution contract。 |
| `POST` | `/task-provider/tasks/{task_type}/{id}/execute` | `execute` | 标准执行入口；要求 `source=orchestrator` 和有效 `parent_execution_id`，拒绝 continuous 任务。 |
| `GET` | `/task-provider/executions/{execution_id}` | `read` | 标准 execution 安全状态投影，不公开持久执行实体或私有运行态。 |

### 执行、恢复与验收边界

bounded 的 `/task-definitions/:id/resume` 是调度控制 API；PostgreSQL/MySQL/OceanBase/openGauss/TiDB watermark execution 的 resume 由新 execution 自动读取 `transfer.sync_states` 完成，OceanBase/openGauss/TiDB 限非空间表。普通 continuous resume 总是创建新 execution，不复用已取消 execution；数据库 CDC blocked generation 必须先完成专用 additive 审批，其他 schema drift 不支持 resume 或 retry。

恢复分级与 snapshot 重跑条件统一见[恢复契约](../transfer-基本概念及配置说明.md#恢复分级)，continuous 的真实停止与恢复闭环见[生命周期](../transfer-任务语义与同步模式.md#64-生命周期)。bounded pause/resume 不中断 active execution；continuous stop 不等于整个 `sync` task type 已支持标准取消。当前 TaskProvider `supports_cancel=false`，不提供标准 execution cancel 入口，`transfer.task.cancel` 仍为 disabled。

此清单仅对应当前路由与权限声明；静态核对不等于用户页面或 Orchestrator 经实际入口、认证与 Tenant 授权到 Worker 的完整执行通过。任务创建、执行入队、运行成功、断点恢复和真实停止应分别核对。

测试分层遵循[测试与验收规范](../../../docs/spec/addp测试与验收规范.md#三标准入口)，具体命令与环境前提见[README 标准门禁](../../README.md#常规模块与专项门禁)；Oracle CDC 专项缺口、测试隔离和通用持久化要求统一见[数据库验收导航](../数据库架构.md#持久化与恢复验收)，不在本表重复命令清单。

固定版本、有效证据、失败与修复、清理及未覆盖项统一见[分版本证据索引](../../CLAUDE.md#专项约束与分版本证据)；真实 Cron 与子扫描隔离另见[bounded 定时授权证据](../../CLAUDE.md#bounded-定时授权验收)。各轮结果仅适用于所列源码与运行尝试，不代表当前 HEAD 已重跑；未运行、失败、跳过或未覆盖项不计为通过。
