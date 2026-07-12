# transfer.transfer_tasks 表说明

更新时间：2026-07-12

`transfer.transfer_tasks` 存储 Transfer 任务定义。任务的执行历史不在本表中保存，统一写入 `common.task_executions`。

## 一、核心字段

| 字段 | 说明 |
|---|---|
| `id` | 任务 ID。 |
| `tenant_id` | 租户 ID。 |
| `name` | 任务名称。 |
| `description` | 任务描述。 |
| `task_type` | 当前固定为 `sync`；执行主链路以 `config` 为准。 |
| `config` | source / target endpoint JSON。 |
| `schedule` | Cron 表达式；为空表示手动任务。 |
| `batch_size` | 默认批大小。 |
| `enabled` | 定时任务是否启用。 |
| `auto_scan_metadata` | 成功后是否触发 Meta deep scan。 |
| `status` | `idle` 或 `running`。 |
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
| `runtime.boundary` | 执行边界；当前只支持 `bounded`。 |
| `load.mode` | `snapshot` 或 PostgreSQL native table 的 `incremental`。 |
| `load.change_detection` | incremental 第一版只支持复合 watermark。 |
| `target.policy.apply_mode` | `replace` / `append` / `upsert`；按任务组合和目标 Provider 校验。 |
| `target.policy.keys` | upsert 的稳定目标键。 |

## 四、任务状态

| 状态 | 说明 |
|---|---|
| `idle` | 未运行或执行已结束。 |
| `running` | 当前有执行正在运行。 |

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
| `POST` | `/task-definitions/:id/start` | 创建 execution 并入队执行。 |
| `POST` | `/task-definitions/:id/pause` | 关闭 owner schedule；不停止 active execution。 |
| `POST` | `/task-definitions/:id/resume` | 恢复 owner schedule。 |
| `GET` | `/task-definitions/:id/executions` | 查询任务执行记录。 |

`/task-definitions/:id/resume` 是调度控制 API，不是 execution resume 参数。PostgreSQL watermark execution 的 resume 由新 execution 自动读取 `transfer.sync_states` 完成。
