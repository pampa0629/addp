# point_cloud_copc_tasks 表结构说明

> 状态：当前实现说明。`manager.point_cloud_copc_tasks` 表达点云 COPC 快显生成任务定义，TaskProvider `task_type=point_cloud_copc_generation`。

## 一、表定位

`manager.point_cloud_copc_tasks` 回答：

> 以后按什么配置为哪个 LAS / LAZ / E57 / PCD / XYZ 点云 item 生成或刷新 Manager 受管 COPC 快显 artifact。

它不替代：

1. `manager.point_cloud_copc` 的结果状态。
2. `manager.preview_state` 的基础预览 / 快显预览模式偏好和三维相机状态。
3. `common.task_executions` 的执行历史。
4. 业务存储中的源 data item 或业务 COPC 数据集。

源文件已经是 `format=copc` 时不创建本任务，直接由基础预览使用源文件内容 URL。

## 二、核心字段

公共字段遵守 `docs/spec/addp任务体系规范.md`。

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | Manager 内部任务定义 ID |
| `tenant_id` | integer | 租户 ID |
| `name` | varchar | 任务名称 |
| `description` | text | 任务描述 |
| `enabled` | boolean | 是否启用 |
| `schedule` | varchar | 调度表达式；当前不声明任务自身调度能力 |
| `next_run_at` | timestamp | 下一次计划运行时间；当前只作为公共字段保留 |
| `last_run_at` | timestamp | 最近运行时间 |
| `last_execution_id` | varchar | 最近一次 `common.task_executions.execution_id` |
| `last_execution_status` | varchar | 最近一次执行状态，使用统一 execution status |
| `config` | jsonb | 点云 COPC 快显任务私有配置 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

执行生命周期遵守以下唯一语义：

1. 启动请求在任务行锁事务内检查同任务是否已有 `pending` 或 `running` execution；存在时返回 HTTP 409。
2. 启动成功时原子创建 `pending` execution 并推进任务摘要；后台执行器领取后原子进入 `running`。
3. direct operator 的进度事件只允许更新当前 `running` 的 `point_cloud_copc_generation` execution；`pending`、终态、其他任务类型和其他租户的事件均拒绝。
4. COPC 结果、execution 终态和任务摘要使用 `last_execution_id` fencing，并在同一 Infra PostgreSQL 事务提交。终态提交后，迟到进度事件不能覆盖终态。

## 三、config 语义

最小结构：

```json
{
  "source": {
    "item_fingerprint": "string",
    "item_locator": "addp://engine/26/path/pointcloud/sample.laz?type=file&item_id=100",
    "source_engine_id": 26,
    "item_id": 100,
    "format": "laz",
    "size_bytes": 10485760
  },
  "options": {},
  "result": {
    "target_kind": "infra_minio_object",
    "file_name": "sample.copc.laz"
  }
}
```

服务端会从 ResourceLocator 和 Meta item facts 归一化 `item_fingerprint`、源格式、源大小和默认 artifact 路径。执行器按 `source.format` 选择 `las_to_copc`、`laz_to_copc`、`e57_to_copc`、`pcd_to_copc` 或 `xyz_to_copc` direct operator。调用方不应传入或消费底层 `storage_ref`。

## 四、TaskProvider 入口

标准任务入口：

```text
GET  /api/v1/manager/tasks?task_type=point_cloud_copc_generation
GET  /api/v1/manager/tasks/point_cloud_copc_generation/{id}
POST /api/v1/manager/tasks/point_cloud_copc_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD：

```text
GET    /api/v1/manager/point_cloud_copc_tasks
POST   /api/v1/manager/point_cloud_copc_tasks
GET    /api/v1/manager/point_cloud_copc_tasks/{id}
PUT    /api/v1/manager/point_cloud_copc_tasks/{id}
DELETE /api/v1/manager/point_cloud_copc_tasks/{id}
```

当前 `supports_schedule=false`、`supports_cancel=false`。需要周期性刷新时，应由 Orchestrator 定时编排间接触发已保存任务定义，并由用户在 Step 参数中显式配置 `existing_result_action=overwrite`。

`POST .../execute` 接受后返回 HTTP 202、`status=pending` 和新 `execution_id`；同任务已有活跃 execution 时返回 HTTP 409。

## 五、相关文档

- [三维模型、点云与高斯泼溅预览说明](../三维模型、点云与高斯泼溅预览说明.md)
- [数据预览语义协议](../数据预览语义协议.md)
- [point_cloud_copc 表结构说明](./point_cloud_copc表.md)
- [数据库架构](../数据库架构.md)
