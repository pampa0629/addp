# model3d_tiles 表

`manager.model3d_tiles` 保存 Manager 拥有生命周期的分块三维模型瓦片结果。表名中的 `tiles` 指分块瓦片数据集，不特指 Cesium 3D Tiles；实际格式由 `target_format` 区分。

## 核心字段

| 字段 | 说明 |
| --- | --- |
| `item_fingerprint` / `item_id` / `locator` | 源 data item 身份与生成时指纹 |
| `task_id` / `last_execution_id` | 生成任务和最近 execution |
| `target_format` | `3d_tiles` 或 `s3m` |
| `storage_ref` | Manager infra MinIO 目录前缀 |
| `manifest_ref` | 3D Tiles 为 `tileset.json`；S3M 为 `config/scene.scp` |
| `file_count` / `size_bytes` | 结果目录统计 |
| `status` | `building`、`ready`、`failed`、`stale`、`deleted` |

## 当前结果与消费

同一租户、源指纹和目标格式只保留一条当前结果；数据库约束范围见[索引声明与核验边界](#索引声明与核验边界)。3D Tiles 与 S3M 必须分别登记，某一种格式生成中或失败不影响另一种 ready 结果预览。关联只能读取本表显式字段，不得根据 MinIO 输出目录名称推断。

已有 `building`、`ready`、`failed` 或 `stale` 结果时，重复执行关联任务必须先完成服务端强制的本次覆盖确认；确认后覆盖同一结果记录和受管 MinIO 对象前缀。结果 ID 与任务 ID 保持不变，`last_execution_id`、状态、manifest 和统计更新为最近一次 execution 事实。确认只作用于当前 `target_format`，不得把 3D Tiles 的确认用于 S3M，反之亦然。

ready 结果通过 `GET /api/v1/manager/model3d_tiles/:id/assets/*asset_path` 读取。该接口保留相对路径、Content-Type、Range 和租户鉴权，因此预览不依赖转换引擎在线。

## 索引声明与核验边界

除 `id` 主键外，[Model3DTiles 模型](../../backend/internal/models/model3d_tiles.go)与[初始化 `ensureModel3DTilesSchema`](../../backend/internal/repository/database.go)的声明如下：

| 来源 | 索引 | 字段与用途 |
| --- | --- | --- |
| GORM 普通索引 | `idx_model3d_tiles_tenant_fingerprint_format` | `(tenant_id, item_fingerprint, target_format)`；按租户、源身份与目标格式回查 |
| GORM 普通索引 | `idx_model3d_tiles_item` | `item_id`；按当前 Meta item 行引用辅助回查，不是租户联合索引 |
| GORM 普通索引 | `idx_model3d_tiles_task` | `task_id`；按生成任务回查 |
| GORM 普通索引 | `idx_model3d_tiles_execution` | `last_execution_id`；最近 execution 回溯 |
| GORM 普通索引 | `idx_model3d_tiles_status` | `status`；结果状态过滤 |
| GORM 普通索引 | `idx_model3d_tiles_deleted_at` | `deleted_at`；软删除字段索引 |
| 初始化显式 SQL 部分唯一索引 | `idx_model3d_tiles_current_unique` | `(tenant_id, item_fingerprint, target_format)`，仅覆盖 `deleted_at IS NULL AND status <> 'deleted'` |

模型没有对应的 `uniqueIndex` 声明；初始化先执行模型 `AutoMigrate`，再创建结果部分唯一索引。`building`、`ready`、`failed`、`stale` 均参与占位；3D Tiles 与 S3M 因 `target_format` 不同可以各保留一条当前结果。任务 ID、execution、manifest、内容版本和对象前缀不参与该结果唯一键。谓词没有排除空指纹或空目标格式，字段 `NOT NULL` 不等于业务有效值校验。

[GetCurrentResult](../../backend/internal/repository/model3d_tiles_repository.go)按租户、源指纹与目标格式读取未软删除且状态不是 `deleted` 的当前结果，未将读取限定为 ready。唯一占位不证明 manifest 与全部瓦片完整存在、源授权有效或客户端预览通过；[分块瓦片与 S3M 契约](../三维模型、点云与高斯泼溅预览说明.md#分块瓦片结果与-s3m-预览)定义实际就绪及渲染条件，目录清理仍须履行[删除语义](#删除语义)。

[历史 037 SQL](../../backend/migrations/037_create_model3d_tiles_results.sql)具有同列、同谓词的结果唯一索引，另声明 `chk_model3d_tiles_target_format CHECK (target_format IN ('3d_tiles', 's3m'))`；当前模型没有对应 CHECK 标签，初始化也未显式创建该 CHECK。因此，应用接受的两种格式与实际数据库是否具备该 CHECK 必须分别核验，不能由历史 SQL 存在推定运行库已约束。

[历史 027 SQL](../../backend/migrations/027_create_model3d_tiles_tasks.sql)创建旧 `model3d_tiles_tasks`；当前任务已统一到 `task_definitions`，任务专用表达式索引与通用语义唯一键统一见[任务定义索引说明](task_definitions表.md#索引声明与唯一性边界)。当前初始化还调整字段，对源指纹与目标格式均非空的同租户重复活跃任务，按 `updated_at DESC, id DESC` 保留一条，其余软删除并禁用；这是数据变更，不是只读核验。启动与历史 SQL 的归属见[Schema 收敛方式](../数据库架构.md#schema-收敛方式)，不以重启或重放 SQL 代替实际落库检查。

### 任务与执行引用

`task_id`、`last_execution_id` 在模型中均可空；当前模型、初始化和上述历史 SQL 未声明到任务或 execution 的外键、复合租户／任务类型约束或 `ON DELETE CASCADE`。`model3d_tiles_generation` 的归属由应用维护，普通索引不保证引用存在、类型正确或租户一致。

[`prepareModel3DTilesResult`](../../backend/internal/service/model3d_tiles_task_service.go)按当前源身份与目标格式复用结果，准备生成时更新任务、execution、对象前缀、manifest 引用并置为 `building`。终态经[统一执行生命周期](../../backend/internal/repository/task_execution_lifecycle.go)核验 lease、Tenant、结果 ID 和 `last_execution_id`，与任务摘要及 execution 在同一事务提交；普通索引不承担该 fencing。

任务删除与目录结果清理是独立动作：统一任务删除事务处理定义与资源绑定，不自动删除结果／目录，也不自动清空旧任务引用；失效任务合并不把旧结果改挂给替代任务。任务及绑定的软／硬删除范围见[绑定表说明](task_resource_bindings表.md#投影替换与删除边界)，覆盖确认与按格式版本化读取统一见[已有结果覆盖契约](../三维模型、点云与高斯泼溅预览说明.md#已有结果覆盖与业务派生)。

### 测试与实际数据库边界

| 现有用例 | 断言范围与限制 |
| --- | --- |
| PostgreSQL `TestIntegrationPostgresManagerModel3DTilesConcurrentClaimAndStart` | 同任务并发领取一次成功、一次冲突，pending 开始时间为空及后续领取开始；显式执行模型 `AutoMigrate`，未在该用例显式建立或验证结果部分唯一索引与格式 CHECK |
| SQLite `TestModel3DTilesExecutionRequiresConfirmationAndFencesResult` | 未确认覆盖不创建 execution、活动执行冲突、结果被新 execution 接管时回滚；另一目标格式的任务不复用 S3M 覆盖确认 |
| SQLite `TestModel3DTilesTaskServiceDeletesManagedResultAndArtifact`、`TestModel3DTilesTaskServiceKeepsResultWhenArtifactCleanupFails` | 通过记录式 cleaner 核对精确 `storage_ref`、成功后的 deleted／软删除和清理失败保留记录；不是实际 MinIO 前缀完整清空证据 |

标准回归沿 [Manager 开发与验证](../../CLAUDE.md#开发与验证)的 `make test-manager-backend`、`make test-manager-postgres`，PostgreSQL 用例由现有 `TestIntegrationPostgresManager` 前缀命中。上述断言未直接覆盖结果重复插入的数据库拒绝、各状态占位、软删除后复用、格式 CHECK 的实际拒绝、任务删除后的引用保留及当前部署迁移；用例存在不计为已通过。

实际数据库须只读核对索引所属表、列顺序、唯一性、有效性、完整谓词与 CHECK／引用约束；历史文件或同名索引不足以证明落库结果。文档静态检查不能替代权限、完整 manifest／瓦片读取、真实转换、另一格式结果不受影响及目录物理清理验收。

## 删除语义

通过 `DELETE /api/v1/manager/model3d_tiles/:id` 删除结果时，Manager 必须先删除 `storage_ref` 对应的 infra MinIO 整个瓦片前缀，再把结果状态更新为 `deleted` 并软删除记录。删除结果不删除源 item、任务定义、execution 历史或另一种 `target_format` 的结果；物理清理失败时不得删除结果记录。
