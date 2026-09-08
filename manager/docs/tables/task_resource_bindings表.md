# task_resource_bindings 表结构说明

> 状态：当前实现说明。`manager.task_resource_bindings` 是统一派生任务与外部资源之间的规范化关系投影。

## 一、用途

任务类型的 `config` 结构不同，资源回收不能靠猜测 `source_engine_id`、`target_engine_id`、`locator` 等 JSON 路径。任务创建或更新时，类型适配器必须在同一事务中重建本表绑定；资源回收和引擎生命周期检查只消费本表。

## 二、字段

| 字段 | 语义 |
| --- | --- |
| `task_definition_id` / `tenant_id` | 所属统一任务定义与租户 |
| `role` | `source` 或 `target` |
| `engine_id` | 资源所属 Engine Instance ID |
| `locator` | 规范 ResourceLocator |
| `item_id` / `item_fingerprint` | 可用时保存的 Meta item 身份与稳定指纹 |
| `ordinal` | 同一角色存在多个资源时的稳定序号 |
| `created_at` | 创建时间 |

`(task_definition_id, role, ordinal)` 唯一。任务更新先删除旧投影再写入新投影；任务物理删除必须在同一事务中删除全部绑定。

## 三、回收语义

- `source` 缺失或其引擎不再属于当前租户时，任务成为回收候选。
- `target` 用于引擎删除影响定位，但业务目标不存在不等同于源任务垃圾。
- 逻辑回收禁用任务并清空下次调度；物理回收删除任务定义与绑定。
- 删除任务定义不自动删除 Business/Meta 业务结果。

## 四、相关文档

- [task_definitions 表结构说明](./task_definitions表.md)
- [快显实现规范](../快显实现规范.md)
