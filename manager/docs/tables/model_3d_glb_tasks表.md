# model_3d_glb_tasks 表结构说明

> 状态：当前实现说明。`manager.model_3d_glb_tasks` 表达单体三维模型 GLB 快显生成任务定义，TaskProvider `task_type=model_3d_glb_generation`。

## 一、表定位

`manager.model_3d_glb_tasks` 回答：

> 以后按什么配置为哪个单体三维模型 item 生成或刷新 Manager 受管 GLB 快显 artifact。

它不替代：

1. `manager.model_3d_glb` 的结果状态。
2. `manager.preview_state` 的预览模式偏好和三维相机状态。
3. `common.task_executions` 的执行历史。
4. 业务存储中的源 data item。

IFC 必须通过 `model3d_workflow.ifc_to_glb` 专用 operator 生成 GLB，不复用 glTF / FBX / OBJ / STL 的 mesh converter。

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
| `config` | jsonb | GLB 快显任务私有配置 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

执行生命周期遵守以下唯一语义：

1. 启动请求在任务行锁事务内检查同任务是否已有 `pending` 或 `running` execution；存在时返回 HTTP 409，不创建第二个 execution。
2. 启动成功时原子创建 `common.task_executions.status=pending`，同时把任务摘要推进为 `pending`；此时 `started_at` 必须为空。
3. Manager 后台执行器领取成功后，原子推进 execution 和任务摘要为 `running`，并写入 `started_at` / `last_run_at`。
4. 结果状态、execution 终态和任务摘要在同一 Infra PostgreSQL 事务中提交；任务摘要必须以 `last_execution_id` fencing，不能被旧 execution 回写覆盖。

## 三、config 语义

最小结构：

```json
{
  "source": {
    "item_fingerprint": "string",
    "item_locator": "addp://engine/26/path/models/building.ifc?type=file&item_id=100",
    "source_engine_id": 26,
    "item_id": 100,
    "format": "ifc",
    "size_bytes": 10485760
  },
  "options": {
    "center_model": true
  },
  "result": {
    "target_kind": "infra_minio_object",
    "file_name": "building.glb"
  }
}
```

服务端会从 ResourceLocator 和 Meta item facts 归一化 `item_fingerprint`、源格式、源大小和默认转换参数。执行器按 `source.format` 选择 `osgb_to_glb`、`gltf_to_glb`、`fbx_to_glb`、`obj_to_glb`、`stl_to_glb` 或 `ifc_to_glb` direct operator。调用方不应传入或消费底层 `storage_ref`。

## 四、TaskProvider 入口

标准任务入口：

```text
GET  /api/v1/manager/tasks?task_type=model_3d_glb_generation
GET  /api/v1/manager/tasks/model_3d_glb_generation/{id}
POST /api/v1/manager/tasks/model_3d_glb_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD：

```text
GET    /api/v1/manager/model_3d_glb_tasks
POST   /api/v1/manager/model_3d_glb_tasks
GET    /api/v1/manager/model_3d_glb_tasks/{id}
PUT    /api/v1/manager/model_3d_glb_tasks/{id}
DELETE /api/v1/manager/model_3d_glb_tasks/{id}
```

当前 `supports_schedule=false`、`supports_cancel=false`。需要周期性刷新时，应由 Orchestrator 定时编排间接触发已保存任务定义，并由用户在 Step 参数中显式配置 `existing_result_action=overwrite`。

`POST .../execute` 接受后返回 HTTP 202、`status=pending` 和新 `execution_id`；同任务已有活跃 execution 时返回 HTTP 409。

## 五、相关文档

- [三维模型、点云与高斯泼溅预览说明](../三维模型、点云与高斯泼溅预览说明.md)
- [快显实现规范](../快显实现规范.md)
- [model_3d_glb 表结构说明](./model_3d_glb表.md)
- [数据库架构](../数据库架构.md)
