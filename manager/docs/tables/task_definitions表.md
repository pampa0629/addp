# task_definitions 表结构说明

> 状态：当前实现说明。`manager.task_definitions` 是 Manager 可重复生成任务定义的唯一存储表。

## 一、表定位

该表统一保存“快显生成”和“空间数据生成”两类任务定义。统一的是定义控制面、分页、CRUD 和执行入口；`task_type` 仍选择独立的配置校验、语义身份、执行器和结果生命周期，不允许把不同任务退化成无类型 JSON 作业。

当前纳入的任务类型：

- 快显生成：`vector_materialized_view_generation`、`vector_tile_cache_generation`、`raster_cog_generation`、`model_3d_glb_generation`、`model3d_tiles_generation`、`gaussian_splat_ksplat_generation`、`point_cloud_copc_generation`、`pptx_pdf_generation`。
- 空间数据生成：`vector_tile_set_generation`、`raster_mosaic_generation`。

`embedding` 的定义结构和生命周期不同，不进入本表。按需 PPTX/PDF 预览任务与其他受管快显任务一样进入本表，其 PDF 当前结果仍保存在 `manager.pptx_pdf`。

## 二、核心字段

| 字段 | 语义 |
| --- | --- |
| `id` / `tenant_id` | Manager 内部任务 ID 与租户边界 |
| `task_type` | 稳定的强类型任务标识 |
| `version` | 乐观并发版本；更新必须携带当前版本 |
| `name` / `description` / `enabled` | 通用定义字段 |
| `schedule` / `next_run_at` | owner 调度字段；当前纳入类型统一不启用自身调度 |
| `last_run_at` / `last_execution_id` / `last_execution_status` | 最近执行摘要，不替代 `common.task_executions` |
| `binding_status` / `binding_issue` | 强资源绑定的聚合可用状态与失效原因；`active` 时原因为空，`missing` 时原因为 `missing_engine` 或 `missing_source` |
| `semantic_key` | 由对应任务类型从规范化配置投影出的稳定语义身份 |
| `config` | 任务类型专有配置，只能由对应校验器解释 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 审计与生命周期字段 |

活跃定义按 `(tenant_id, task_type, semantic_key)` 唯一；空 `semantic_key` 不参与该唯一约束。

`binding_status` 与 `last_execution_status` 正交：前者是 Manager cleanup 对当前绑定的持久化结论，后者只保存最近 execution 状态。任务创建或重新保存有效配置时，必须把 `binding_status` 重置为 `active` 并清空 `binding_issue`；逻辑回收不得覆盖已有执行摘要。

受管快显任务只允许对 `binding_status=missing` 的定义执行显式源重绑。重绑请求携带当前 `version` 与新的 ResourceLocator，由 Manager 校验源能力并重建强类型配置；重绑不创建 execution。新语义身份若已存在任务，则返回该规范任务并删除旧失效定义，避免重复语义身份。

## 三、资源与结果边界

源和目标资源必须同步投影到 `manager.task_resource_bindings`。资源回收与引擎删除影响评估只读取该规范化绑定，不解析各任务不同的 `config` 路径。

删除任务定义只删除控制面定义与资源绑定，不级联删除业务派生产物：

- Manager 拥有的快显 artifact 由各结果表和对应删除接口管理。
- Business PMTiles、Raster Mosaic 等业务结果归 Business 存储与 Meta，删除任务不得删除这些结果。

## 四、唯一 API

任务列表通过 `category`、`task_type`、`execution_status`、`binding_status`、`page` 和 `page_size` 组合筛选；`execution_status` 精确匹配最近一次执行状态，例如 `failed`，`binding_status` 只接受 `active` 或 `missing`。批量删除只允许前端显式列举用户已勾选的任务，并逐项调用唯一 DELETE，不提供“删除当前筛选全部”的隐式成员接口。

```text
GET    /api/v1/manager/tasks?category=managed_quick_view|spatial_business&task_type=...
POST   /api/v1/manager/tasks/{task_type}
GET    /api/v1/manager/tasks/{task_type}/{id}
PUT    /api/v1/manager/tasks/{task_type}/{id}
DELETE /api/v1/manager/tasks/{task_type}/{id}
POST   /api/v1/manager/tasks/{task_type}/{id}/execute
```

不得恢复按任务类型分表或 `/*_tasks` 私有 CRUD 路由。

## 五、相关文档

- [task_resource_bindings 表结构说明](./task_resource_bindings表.md)
- [快显实现规范](../快显实现规范.md)
- [数据库架构](../数据库架构.md)
