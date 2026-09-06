# transfer.transfer_tasks 表说明

更新时间：2026-08-02

`transfer.transfer_tasks` 存储 Transfer 任务定义。任务的执行历史不在本表中保存，统一写入 `common.task_executions`。

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
| `auto_scan_metadata` | 是否自动触发 Meta deep scan。bounded 在成功后触发；continuous 在目标结构首次建立后触发。 |
| `initial_metadata_scan_status` | continuous 首次目标扫描私有状态：空值、`running`、`success` 或 `failed`。 |
| `initial_metadata_scan_claim_token` / `initial_metadata_scan_lease_until` | continuous 首次扫描的 fencing claim；仅 `running` 时有效。 |
| `initial_metadata_scan_attempt` | continuous 首次扫描领取次数。 |
| `initial_metadata_scan_execution_id` / `initial_metadata_scan_error` | Meta execution UUID 或最近一次提交错误。 |
| `status` | `idle`、`running` 或 `blocked`；`blocked` 表示数据库 CDC 当前 generation 被 schema drift 阻塞。只有专用 additive 审批成功后可回到 `idle`，其他变化只允许 Stop。 |
| `desired_state` | continuous runtime 用户期望状态：`running` / `paused` / `stopped`，默认 `stopped`；bounded task 不消费。该字段不能单独判定数据库 CDC 是否永久停止，终态以 `capture_resources.status=stopped` 为准。 |
| `progress` | 当前任务进度百分比。 |
| `created_by` | 创建人 ID。 |
| `last_execution_id` | 最近一次执行 ID。 |
| `last_execution_status` | 最近一次执行状态。 |
| `last_run_at` | 最近运行时间。 |
| `next_run_at` | 下次计划运行时间。 |
| `created_at` / `updated_at` / `deleted_at` | 审计字段。 |

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
| `load.mode` | `snapshot` 或 PostgreSQL/MySQL/OceanBase 非空间 native table 的 `incremental`。 |
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

路由前缀：`/api/v1/transfer`。

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/tasks` | TaskProvider 任务列表，支持 `task_type=sync`。 |
| `GET` | `/tasks/{task_type}/{id}` | TaskProvider 标准任务详情，`task_type` 当前仅支持 `sync`。 |
| `POST` | `/tasks/{task_type}/{id}/execute` | TaskProvider 标准任务执行入口，`task_type` 当前仅支持 `sync`。 |
| `POST` | `/task-definitions` | 创建任务定义。 |
| `GET` | `/task-definitions/statistics` | 查询任务统计。 |
| `GET` | `/task-definitions/:id` | 查询任务定义详情。 |
| `PUT` | `/task-definitions/:id` | 更新任务定义。 |
| `DELETE` | `/task-definitions/:id` | 删除任务定义。 |
| `POST` | `/task-definitions/:id/start` | bounded 创建 execution 并入队；continuous 原子设置 running 并创建 pending execution。 |
| `POST` | `/task-definitions/:id/pause` | bounded 关闭 owner schedule；continuous 设置 paused 并通知 runtime 取消 execution。 |
| `POST` | `/task-definitions/:id/resume` | bounded 恢复 owner schedule；未阻塞的 continuous 设置 running 并创建新 pending execution；CDC `status=blocked` 返回 409。 |
| `POST` | `/task-definitions/:id/stop` | 仅 continuous：设置 stopped 并通知 runtime 取消 execution。 |
| `GET` | `/task-definitions/:id/schema-change` | 查询当前数据库 CDC schema change request 与可审批建议。 |
| `POST` | `/task-definitions/:id/schema-change/approve` | 审批当前 additive request；成功后任务进入 paused，不隐式 Resume。 |
| `GET` | `/task-definitions/:id/executions` | 查询任务执行记录。 |

bounded 的 `/task-definitions/:id/resume` 是调度控制 API；PostgreSQL/MySQL/OceanBase 非空间 watermark execution 的 resume 由新 execution 自动读取 `transfer.sync_states` 完成。普通 continuous resume 总是创建新 execution，不复用已取消 execution；数据库 CDC blocked generation 必须先完成专用 additive 审批，其他 schema drift 不支持 resume 或 retry。

TaskProvider 声明的任务发现地址是唯一标准 `/tasks`。服务端在请求带 `task_type=sync` 时强制过滤为 bounded，避免 Orchestrator v1 选择 continuous task；Console 不带 `task_type` 时仍通过同一路由查询全部任务。不保留 `/provider-tasks` 私有路由。
