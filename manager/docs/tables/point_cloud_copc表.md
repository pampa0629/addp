# point_cloud_copc 表结构说明

> 状态：当前实现说明。`manager.point_cloud_copc` 表达 Manager 拥有生命周期的点云 COPC 快显结果状态。

## 一、表定位

`manager.point_cloud_copc` 回答：

> 当前 LAS / LAZ / E57 / PCD / XYZ 点云 item 是否存在可复用的 COPC 快显 artifact，该 artifact 存在哪里、是否可用、由哪次任务执行生成。

它不替代：

1. 源业务 data item。
2. `manager.task_definitions` 中 `task_type=point_cloud_copc_generation` 的任务定义。
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
| `task_id` | bigint | 产生或最近刷新该结果的 `manager.task_definitions.id` |
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

## 四、execution fencing 与终态提交

开始生成或刷新结果时，`last_execution_id` 必须写入当前 execution。生成终态只能更新 `last_execution_id` 仍等于当前 execution 的结果行；旧 execution 或失去所有权的执行不得覆盖新结果。

结果终态、`common.task_executions` 终态和对应 `manager.task_definitions.last_execution_status` 必须在同一 Infra PostgreSQL 事务中提交。进度事件使用 `status=running` 条件更新，终态提交后的迟到事件必须返回冲突。

<a id="五索引建议"></a>

## 五、索引声明与核验边界

除 `id` 主键外，[PointCloudCOPC 模型](../../backend/internal/models/point_cloud_copc.go)与[初始化 `ensurePointCloudCOPCSchema`](../../backend/internal/repository/database.go)的声明如下：

| 来源 | 索引名 | 字段与用途 |
| --- | --- | --- |
| GORM 普通索引 | `idx_point_cloud_copc_tenant_item_fingerprint` | `(tenant_id, item_fingerprint)`；按源 item 身份回查 |
| GORM 普通索引 | `idx_point_cloud_copc_tenant_item` | `(tenant_id, item_id)`；按当前 Meta item 行引用辅助回查 |
| GORM 普通索引 | `idx_point_cloud_copc_task` | `task_id`；按产生或最近刷新结果的任务回查 |
| GORM 普通索引 | `idx_point_cloud_copc_execution` | `last_execution_id`；execution 回溯 |
| GORM 普通索引 | `idx_point_cloud_copc_status` | `status`；按结果状态过滤 |
| GORM 普通索引 | `idx_point_cloud_copc_deleted_at` | `deleted_at`；软删除字段索引 |
| 初始化显式 SQL 部分唯一索引 | `idx_point_cloud_copc_current_unique` | `(tenant_id, item_fingerprint)`，仅覆盖 `deleted_at IS NULL AND status <> 'deleted'` |

模型没有对应的 `uniqueIndex` 声明；初始化先执行模型 `AutoMigrate`，再创建结果部分唯一索引。`building`、`ready`、`failed` 均参与占位，不能理解为“只有 ready 唯一”。谓词未排除空指纹，字段 `NOT NULL` 也不等于有效业务身份校验。任务 ID、execution、源格式、内容版本及转换参数不参与该结果唯一键。

[GetCurrentByFingerprint](../../backend/internal/repository/point_cloud_copc_repository.go)读取未软删除且状态不是 `deleted` 的当前结果，ready 读取还要求 `status=ready`；索引不证明对象存在、权限满足或前端预览通过。结果退出占位仍须履行[删除语义](#六删除语义)和[单对象清理契约](../quick-view/共用契约.md#单对象结果清理)，不得通过直接改库替代物理清理。

[历史 035 SQL](../../backend/migrations/035_create_point_cloud_copc.sql)含同列、同谓词的结果唯一索引，同时创建旧 `point_cloud_copc_tasks`。当前任务已统一到 `task_definitions`；任务专用表达式索引与通用语义唯一键见[任务定义索引说明](task_definitions表.md#索引声明与唯一性边界)，不能把名称中的 `_tasks` 当成旧表仍在使用的证明。当前初始化还调整字段，并按 `updated_at DESC, id DESC` 保留同租户同源指纹的一条活跃 COPC 任务，其余软删除并禁用；这是数据变更，不是只读检查。启动和历史 SQL 的归属见[Schema 收敛方式](../数据库架构.md#schema-收敛方式)，文件存在或重放 SQL 均不替代实际落库核验。

### 任务与执行引用

`task_id`、`last_execution_id` 在模型中均可空；当前模型、初始化和上述历史 SQL 未声明到任务或 execution 的外键、复合租户／任务类型约束或 `ON DELETE CASCADE`。`point_cloud_copc_generation` 的归属由应用维护，普通索引不保证引用存在、类型正确或租户一致。

[`prepareResult`](../../backend/internal/service/point_cloud_copc_task_service.go)按源指纹复用当前结果，在准备生成时写入本次 `task_id`、`last_execution_id` 并置为 `building`；因此这些引用不是永远冻结的首次成功来源。终态写入通过[统一执行生命周期](../../backend/internal/repository/task_execution_lifecycle.go)复核当前 lease、Tenant、结果 ID 与 execution 归属；原子提交要求见[execution fencing 与终态提交](#四execution-fencing-与终态提交)，不能用普通索引代替。

普通任务删除沿统一定义与绑定事务，不自动删除 COPC 结果／对象，也不自动清空旧任务引用；失效任务合并不把旧结果改挂给替代任务。任务与绑定的软／硬删除范围见[绑定表说明](task_resource_bindings表.md#投影替换与删除边界)，结果独立清理的禁区见[删除语义](#六删除语义)。源 `format=copc` 直接基础预览、不创建本表结果的边界保持不变。

### 测试与实际数据库边界

| 现有用例 | 断言范围与限制 |
| --- | --- |
| PostgreSQL `TestIntegrationPostgresManagerPointCloudCOPCConcurrentClaimAndStart` | 同任务并发领取一次成功、一次冲突，pending 的空开始时间、领取后开始执行及运行中进度更新；显式模型 `AutoMigrate`，未在该用例中显式创建或验证结果部分唯一索引 |
| SQLite `TestPointCloudCOPCExecutionLifecycleIsAtomic` | 重复执行冲突、运行中进度、结果与 execution 成功终态；完成后的迟到进度返回冲突，成功状态与 100% 进度不被改写。不证明 PostgreSQL 索引或真实转换／对象效果 |

标准回归沿 [Manager 开发与验证](../../CLAUDE.md#开发与验证)的 `make test-manager-backend`、`make test-manager-postgres`，PostgreSQL 用例由现有 `TestIntegrationPostgresManager` 前缀命中。上述断言未直接覆盖结果重复插入的数据库拒绝、各状态占位、软删除后复用、任务删除后结果引用保留及实际部署迁移；不据源码或用例名称认定通过。

实际数据库须只读核对索引所属表、字段顺序、唯一性、有效性、完整谓词及引用相关真实约束；同名索引或历史 SQL 存在不足以证明落库结果。文档静态检查不替代权限、实际 COPC 内容读取、转换和失败清理验收。

## 六、删除语义

删除 COPC 快显结果时，Manager 必须先按 `storage_ref` 删除 infra MinIO artifact，再将结果状态标记为 `deleted` 并软删除记录。

不得删除：

1. 源业务文件或源 data item。
2. 对应的 `manager.task_definitions` 任务定义。
3. `common.task_executions` 执行历史。
4. `manager.preview_state` 中的用户预览偏好和三维相机状态。

## 七、相关文档

- [三维模型、点云与高斯泼溅预览说明](../三维模型、点云与高斯泼溅预览说明.md)
- [数据预览语义协议](../数据预览语义协议.md)
- [task_definitions 表结构说明](./task_definitions表.md)
- [数据库架构](../数据库架构.md)
