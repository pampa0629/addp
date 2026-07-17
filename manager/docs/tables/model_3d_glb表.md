# model_3d_glb 表结构说明

> 状态：当前实现说明。`manager.model_3d_glb` 表达 Manager 拥有生命周期的单体三维模型 GLB 快显结果状态。

## 一、表定位

`manager.model_3d_glb` 回答：

> 当前 model_3d item 是否存在可复用的 GLB 快显 artifact，该 artifact 存在哪里、是否可用、由哪次任务执行生成。

它不替代：

1. 源业务 data item。
2. `manager.model_3d_glb_tasks` 的任务定义。
3. `manager.preview_state` 的基础预览 / 快显预览模式偏好和三维相机状态。
4. `common.task_executions` 的执行历史。

GLB 快显结果存放在 Manager infra MinIO 中，不自动升格为业务存储中的新 data item。

## 二、核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 结果 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 源 item 指纹 |
| `item_id` | integer | 当前 Meta item 行引用，仅用于回查 |
| `locator` | text | 源 item ResourceLocator |
| `task_id` | bigint | 产生或最近刷新该结果的 `manager.model_3d_glb_tasks.id` |
| `last_execution_id` | varchar | 最近一次 GLB 生成 execution |
| `source_engine_id` | integer | 源存储引擎 ID |
| `source_format` | varchar | 源格式，例如 `gltf`、`fbx`、`obj`、`stl`、`ifc`、`osgb` |
| `source_size_bytes` | bigint | 源文件大小 |
| `storage_ref` | text | Manager infra MinIO 对象引用，前端不得拼接消费 |
| `file_name` | varchar | GLB artifact 文件名 |
| `size_bytes` | bigint | GLB artifact 大小 |
| `content_url` | text | 受控内容读取 URL |
| `status` | varchar | GLB 快显结果状态 |
| `metadata` | jsonb | 转换 operator、源 facts、目标 facts 和诊断摘要 |
| `error_message` | text | 最近错误摘要 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

## 三、状态语义

| 状态 | 含义 |
| --- | --- |
| `building` | GLB artifact 正在生成 |
| `ready` | GLB artifact 可通过 Manager 受控接口读取并用于预览 |
| `failed` | 最近生成失败，结果不可用或不完整 |
| `deleted` | 结果已清理 |

这些状态属于 artifact state，不属于统一 execution status。

## 四、execution fencing 与终态提交

开始生成或刷新结果时，`last_execution_id` 必须写入当前 execution。生成终态只能更新 `last_execution_id` 仍等于当前 execution 的结果行；旧 execution 或失去所有权的执行不得覆盖新结果。

结果终态、`common.task_executions` 终态和 `manager.model_3d_glb_tasks.last_execution_status` 必须在同一 Infra PostgreSQL 事务中提交。任一行的 fencing 条件不成立时整笔事务回滚，不允许出现结果已 `ready`、execution 仍 `running` 等分裂状态。

## 五、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_model_3d_glb_tenant_item_fingerprint` | `tenant_id, item_fingerprint` | 查询某 item 的 GLB 快显结果 |
| `idx_model_3d_glb_tenant_item` | `tenant_id, item_id` | 按当前 Meta item 行引用辅助回查 |
| `idx_model_3d_glb_task` | `task_id` | 查询某任务产生的结果 |
| `idx_model_3d_glb_execution` | `last_execution_id` | execution 回溯 |
| `idx_model_3d_glb_status` | `status` | 按结果状态过滤 |
| `idx_model_3d_glb_current_unique` | `tenant_id, item_fingerprint` | 同一 item 只保留一个当前 GLB 快显结果 |

## 六、删除语义

删除 GLB 快显结果时，Manager 必须先按 `storage_ref` 删除 infra MinIO artifact，再将结果状态标记为 `deleted` 并软删除记录。

不得删除：

1. 源业务文件或源 data item。
2. 对应的 `manager.model_3d_glb_tasks` 任务定义。
3. `common.task_executions` 执行历史。
4. `manager.preview_state` 中的用户预览偏好和三维相机状态。

## 七、相关文档

- [三维模型、点云与高斯泼溅预览说明](../三维模型、点云与高斯泼溅预览说明.md)
- [快显实现规范](../快显实现规范.md)
- [model_3d_glb_tasks 表结构说明](./model_3d_glb_tasks表.md)
- [数据库架构](../数据库架构.md)
