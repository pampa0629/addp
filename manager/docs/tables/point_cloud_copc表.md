# point_cloud_copc 表结构说明

> 状态：当前实现说明。`manager.point_cloud_copc` 表达 Manager 拥有生命周期的点云 COPC 快显结果状态。

## 一、表定位

`manager.point_cloud_copc` 回答：

> 当前 LAS / LAZ / E57 / PCD / XYZ 点云 item 是否存在可复用的 COPC 快显 artifact，该 artifact 存在哪里、是否可用、由哪次任务执行生成。

它不替代：

1. 源业务 data item。
2. `manager.point_cloud_copc_tasks` 的任务定义。
3. `manager.preview_state` 的基础预览 / 快显预览模式偏好和三维相机状态。
4. `common.task_executions` 的执行历史。

`format=copc` 源文件本身已经是前端目标浏览格式，不创建 COPC 快显任务，也不登记为本表结果。

## 二、核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 结果 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 源 item 指纹 |
| `item_id` | integer | 当前 Meta item 行引用，仅用于回查 |
| `locator` | text | 源 item ResourceLocator |
| `task_id` | bigint | 产生或最近刷新该结果的 `manager.point_cloud_copc_tasks.id` |
| `last_execution_id` | varchar | 最近一次 COPC 生成 execution |
| `source_engine_id` | integer | 源存储引擎 ID |
| `source_format` | varchar | 源格式，当前为 `las`、`laz`、`e57`、`pcd` 或 `xyz` |
| `source_size_bytes` | bigint | 源文件大小 |
| `storage_ref` | text | Manager infra MinIO 对象引用，前端不得拼接消费 |
| `file_name` | varchar | COPC artifact 文件名 |
| `size_bytes` | bigint | COPC artifact 大小 |
| `content_url` | text | 受控内容读取 URL |
| `status` | varchar | COPC 快显结果状态 |
| `metadata` | jsonb | 转换参数、源 facts、目标 facts 和诊断摘要 |
| `error_message` | text | 最近错误摘要 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

## 三、状态语义

| 状态 | 含义 |
| --- | --- |
| `building` | COPC artifact 正在生成 |
| `ready` | COPC artifact 可通过 Manager 受控接口读取并用于预览 |
| `failed` | 最近生成失败，结果不可用或不完整 |
| `deleted` | 结果已清理 |

这些状态属于 artifact state，不属于统一 execution status。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_point_cloud_copc_tenant_item_fingerprint` | `tenant_id, item_fingerprint` | 查询某 item 的 COPC 快显结果 |
| `idx_point_cloud_copc_tenant_item` | `tenant_id, item_id` | 按当前 Meta item 行引用辅助回查 |
| `idx_point_cloud_copc_task` | `task_id` | 查询某任务产生的结果 |
| `idx_point_cloud_copc_execution` | `last_execution_id` | execution 回溯 |
| `idx_point_cloud_copc_status` | `status` | 按结果状态过滤 |
| `idx_point_cloud_copc_current_unique` | `tenant_id, item_fingerprint` | 同一 item 只保留一个当前 COPC 快显结果 |

## 五、删除语义

删除 COPC 快显结果时，Manager 必须先按 `storage_ref` 删除 infra MinIO artifact，再将结果状态标记为 `deleted` 并软删除记录。

不得删除：

1. 源业务文件或源 data item。
2. 对应的 `manager.point_cloud_copc_tasks` 任务定义。
3. `common.task_executions` 执行历史。
4. `manager.preview_state` 中的用户预览偏好和三维相机状态。

## 六、相关文档

- [三维模型、点云与高斯泼溅预览说明](../三维模型、点云与高斯泼溅预览说明.md)
- [数据预览语义协议](../数据预览语义协议.md)
- [point_cloud_copc_tasks 表结构说明](./point_cloud_copc_tasks表.md)
- [数据库架构](../数据库架构.md)
