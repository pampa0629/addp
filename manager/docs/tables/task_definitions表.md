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

语义唯一的适用范围、模型索引与初始化 SQL 声明统一见[索引声明与唯一性边界](#索引声明与唯一性边界)。

`binding_status` 与 `last_execution_status` 正交：前者是 Manager cleanup 对当前绑定的持久化结论，后者只保存最近 execution 状态。任务创建或重新保存有效配置时，必须把 `binding_status` 重置为 `active` 并清空 `binding_issue`；逻辑回收不得覆盖已有执行摘要。

受管快显任务只允许对 `binding_status=missing` 的定义执行显式源重绑。重绑请求携带当前 `version` 与新的 ResourceLocator，由 Manager 校验源能力并重建强类型配置；重绑不创建 execution。新语义身份若已存在任务，则返回该规范任务并删除旧失效定义，避免重复语义身份。

### 索引声明与唯一性边界

#### 模型索引与通用 SQL 约束

[TaskDefinition 模型](../../backend/internal/models/task_definition.go)声明查询索引；[InitDatabase／ensureTaskDefinitionSchema](../../backend/internal/repository/database.go)在 `AutoMigrate` 后创建绑定查询索引和通用部分唯一索引：

| 索引名 | 字段 | 声明来源与语义 |
| --- | --- | --- |
| `idx_manager_task_definitions_tenant_type` | `tenant_id, task_type` | 模型普通索引，按租户与任务类型查询。 |
| `idx_manager_task_definitions_last_execution` | `last_execution_id` | 模型普通索引，最近执行回溯。 |
| `idx_manager_task_definitions_deleted_at` | `deleted_at` | 模型普通索引，软删除字段索引声明。 |
| `idx_manager_task_definitions_tenant_binding_status` | `tenant_id, binding_status` | 初始化 SQL 的普通索引，按绑定状态查询。 |
| `uq_manager_task_definition_semantic` | `tenant_id, task_type, semantic_key` | 初始化 SQL 的部分唯一索引，谓词为 `deleted_at IS NULL AND semantic_key <> ''`。 |

通用唯一约束不按 `enabled`、`binding_status` 或 `last_execution_status` 过滤，停用或绑定失效本身不释放占位。该谓词不将 NULL 或空字符串纳入通用唯一范围；模型也未为 `semantic_key` 声明 `NOT NULL`，不能据此认定语义身份已经有效。各类型的 key 构造、规范任务复用与业务结果边界分别沿[受管快显语义身份](../quick-view/共用契约.md#受管快显任务语义身份)、[业务派生语义身份](../quick-view/共用契约.md#业务派生任务语义身份)，不以底层唯一拒绝替代各类型的产品创建契约。

#### 类型专用表达式索引

当前 Go 初始化还在同一 `manager.task_definitions` 上创建以下部分唯一索引。它们均限定 `deleted_at IS NULL` 和对应 `task_type`；名称包含 `_tasks` 不表示保留独立任务表。精确表达式以初始化源码为准：

| 索引名 | 限定任务类型 | 配置身份与额外谓词摘要 |
| --- | --- | --- |
| `idx_vector_tile_set_tasks_semantic_unique` | `vector_tile_set_generation` | `config.semantic_hash`。 |
| `idx_vector_tile_cache_tasks_source_profile_unique` | `vector_tile_cache_generation` | `config.target.item_fingerprint` 与 `config.profile_hash`。 |
| `idx_raster_cog_tasks_source_unique` | `raster_cog_generation` | `config.target.item_fingerprint`。 |
| `idx_model3d_tiles_tasks_source_format_unique` | `model3d_tiles_generation` | `config.source.item_fingerprint` 与 `config.target_format`；两者各要求 `COALESCE(..., '') <> ''`。 |
| `idx_model_3d_glb_tasks_source_unique` | `model_3d_glb_generation` | `config.source.item_fingerprint`，并要求 `COALESCE(..., '') <> ''`。 |
| `idx_gaussian_splat_ksplat_tasks_source_unique` | `gaussian_splat_ksplat_generation` | `config.source.item_fingerprint`，并要求 `COALESCE(..., '') <> ''`。 |
| `idx_point_cloud_copc_tasks_source_unique` | `point_cloud_copc_generation` | `config.source.item_fingerprint`，并要求 `COALESCE(..., '') <> ''`。 |

服务按 PostgreSQL 的 `23505`、Manager 任务定义表及准确约束名识别当前任务类型的语义冲突，回查并复用规范任务。两项索引同时冲突时，不能依赖数据库先报告哪一项。

因此，空 `semantic_key` 不参与通用索引，不等于绕过全部数据库唯一约束；类型专用索引仍按配置表达式独立判断。索引保护也不替代各类型配置校验、版本校验或资源授权。

#### 初始化、测试与实际约束

当前 Go 初始化在检测到旧任务表时包含结果关联清理、Manager 派生执行清理与旧表删除，再维护统一表和索引；它不是只读核验入口。[历史 046 SQL](../../backend/migrations/046_add_task_binding_status.sql)仅对应绑定字段、状态归一化及绑定查询索引，不能据该文件存在认定所有语义索引已生效。

[ManagedTaskSemanticIdentityIndexes 用例](../../backend/internal/repository/tile_cache_repository_postgres_integration_test.go)对瓦片缓存与 COG 分别在回滚事务内暂时移除另一约束，验证通用语义索引和类型专用索引各自以准确约束名拒绝重复，同时只留下一个定义；[UnifiedTaskDefinitionLifecycle 用例](../../backend/internal/repository/task_definition_postgres_integration_test.go)包含统一初始化、重复业务任务拒绝、绑定状态归一化与清理。它们不自动扩展为所有任务类型、通用 key 与配置表达式的一致性、空／NULL key、软删除后复用或当前运行库约束的独立验收证据。

2026-11-11 的 [首失败 Run 42936636174／Job 130253833699](https://github.com/pampa0629/addp/actions/runs/42936636174/job/130253833699)已由专用索引拒绝重复插入，但旧测试只接受通用索引名。未改 Manager 后端的[后继 Run 42937944396／Job 130254246607](https://github.com/pampa0629/addp/actions/runs/42937944396/job/130254246607)同名用例及完整门禁通过，两份证据各自保留。原归档未保存索引 OID，不能还原具体哪次 DDL 改变了检查顺序。修订后的回归在事务内分别隔离两个约束，精确核验 SQLSTATE、约束名和存量定义数，退出回滚 DDL 与夹具。本地使用干净隔离检出，通过 `make test-manager-backend` 和 `make test-manager-postgres MANAGER_POSTGRES_TEST_ARGS="--source-root <隔离检出>"`，含四项准确约束反例，零跳过。

实际数据库须分别核对表、字段／表达式、唯一性、有效状态及各自谓词，不能只看索引名称。标准入口为 `make test-manager-postgres`，上述用例由现有命名筛选命中；门禁与分版本证据见[Manager 开发与验证](../../CLAUDE.md#开发与验证)。源码和用例存在不计为数据库或运行验收通过，不以重启初始化或重放历史 SQL 代替只读核验。

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
