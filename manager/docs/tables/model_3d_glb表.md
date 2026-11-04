# model_3d_glb 表结构说明

> 状态：当前实现说明。`manager.model_3d_glb` 表达 Manager 拥有生命周期的单体三维模型 GLB 快显结果状态。

## 一、表定位

`manager.model_3d_glb` 回答：

> 当前 model_3d item 是否存在可复用的 GLB 快显 artifact，该 artifact 存在哪里、是否可用、由哪次任务执行生成。

它不替代：

1. 源业务 data item。
2. `manager.task_definitions` 中 `task_type=model_3d_glb_generation` 的任务定义。
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
| `task_id` | bigint | 产生或最近刷新该结果的 `manager.task_definitions.id` |
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

执行隔离输出、授权复核与发布的完整规则统一见[文件模型输入与输出契约](../三维模型、点云与高斯泼溅预览说明.md#ftpnfsminio-转换输入准备)。`last_execution_id` 的普通索引不承担 fencing。已有已发布产物时，文件模型的待发布任务、execution 和对象归属保存在 `metadata.model3d_output_lifecycle.pending`，准备与失败均不改写旧成果的 `task_id`、`last_execution_id` 和对象引用；成功提交时才切换。首次生成与已有结果刷新应分别判断，不能笼统要求准备阶段就将可见结果的 execution 改为本次执行。

结果终态、`common.task_executions` 终态和对应 `manager.task_definitions.last_execution_status` 必须在同一 Infra PostgreSQL 事务中提交。任一行的 fencing 条件不成立时整笔事务回滚，不允许出现结果已 `ready`、execution 仍 `running` 等分裂状态。

<a id="五索引建议"></a>

## 五、索引声明与核验边界

除 `id` 主键外，[Model3DGLB 模型](../../backend/internal/models/model3d_glb.go)与[初始化 `ensureModel3DGLBSchema`](../../backend/internal/repository/database.go)的索引分工如下：

| 来源 | 索引名 | 字段与用途 |
| --- | --- | --- |
| GORM 普通索引 | `idx_model_3d_glb_tenant_item_fingerprint` | `(tenant_id, item_fingerprint)`；按源 item 身份回查 |
| GORM 普通索引 | `idx_model_3d_glb_tenant_item` | `(tenant_id, item_id)`；按当前 Meta item 行引用辅助回查 |
| GORM 普通索引 | `idx_model_3d_glb_task` | `task_id`；按产生或最近刷新结果的任务回查 |
| GORM 普通索引 | `idx_model_3d_glb_execution` | `last_execution_id`；execution 回溯 |
| GORM 普通索引 | `idx_model_3d_glb_status` | `status`；按结果状态过滤 |
| GORM 普通索引 | `idx_model_3d_glb_deleted_at` | `deleted_at`；软删除字段索引 |
| 初始化显式 SQL 部分唯一索引 | `idx_model_3d_glb_current_unique` | `(tenant_id, item_fingerprint)`，仅覆盖 `deleted_at IS NULL AND status <> 'deleted'` |

模型本身未以 `uniqueIndex` 声明结果部分唯一索引；初始化先执行模型 `AutoMigrate`，再运行显式 SQL。`building`、`ready`、`failed` 均参与占位，谓词并未限定为 ready，也没有排除空指纹；字段 `NOT NULL` 不等于有效业务身份校验。任务 ID、execution、源格式、内容版本和对象引用不参与该结果唯一键。

[GetCurrentByFingerprint](../../backend/internal/repository/model3d_glb_repository.go)读取未软删除且状态不是 `deleted` 的当前结果，ready 读取还要求 `status=ready`；唯一占位不证明物理对象存在、源授权有效或前端预览通过。`deleted` 状态及软删除的处理仍须履行[删除语义](#六删除语义)，不得通过直接改库释放占位来替代对象清理。

[历史 028 SQL](../../backend/migrations/028_create_model_3d_glb.sql)具有同列、同谓词的结果唯一索引，同时创建旧 `model_3d_glb_tasks`。当前任务类型已映射到统一 `task_definitions`；任务专用表达式索引与通用语义唯一键见[任务定义索引说明](task_definitions表.md#索引声明与唯一性边界)，不能因索引名含 `_tasks` 推断旧表仍是运行入口。当前初始化还调整字段，并将同租户同源指纹的重复活跃 GLB 任务按 `updated_at DESC, id DESC` 保留一条，其余软删除并禁用；这是数据变更，不是只读检查。初始化与历史 SQL 的归属见[Schema 收敛方式](../数据库架构.md#schema-收敛方式)，不以重启或重放 SQL 证明实际约束。

### 任务与执行引用

`task_id`、`last_execution_id` 在模型中均可空；当前模型、初始化及上述历史 SQL 未声明到任务或 execution 的外键、复合租户／任务类型约束或 `ON DELETE CASCADE`。`model_3d_glb_generation` 归属由应用链路维护，普通索引不保证引用存在、类型正确或租户一致。

完成的任务被删除后，已发布 GLB 结果及旧任务引用仍保留，不自动清空或改挂；同来源新建任务必须显式确认覆盖，发布成功才切换归属。当前文件模型 `config_version` 的 pending／running 执行会阻止任务删除，不能将此条件推广为所有历史配置的数据库约束。实际逻辑见[任务仓储](../../backend/internal/repository/model3d_glb_repository.go)和[隔离输出发布](../../backend/internal/repository/model3d_glb_output.go)；任务与绑定的通用软／硬删除规则见[绑定表说明](task_resource_bindings表.md#投影替换与删除边界)。结果删除不删除任务、源数据、execution 或用户偏好，物理清理仍按各 Owner 的独立契约执行。

### 测试与实际数据库边界

| 现有用例 | 断言范围与限制 |
| --- | --- |
| PostgreSQL `TestIntegrationPostgresManagerModel3DGLBConcurrentClaimAndStart` | 同任务并发领取一次成功、一次冲突，pending／running 摘要及发起人字段；发起人存在不等于源读取已授权 |
| PostgreSQL `TestIntegrationPostgresManagerGLBIsolatedOutputPublication` | IFC／3DS／DAE／OBJ／STL 配置夹具的发布指针切换、精确旧对象清理回调；删除完成任务后保留结果，新任务未确认覆盖或有活动执行时拒绝，失败后保留旧任务归属与 ready 成果。使用合成授权引用和清理回调，不证明真实 System Allow、源读取、转换或对象物理删除 |
| SQLite `TestGLBRecreatedTaskDoesNotTakeOwnershipOfPreviousArtifactBeforeSuccess` | 重建任务在准备阶段不接管旧成果，成功切换与失败保留分支；不证明 PostgreSQL 部分唯一索引 |

上述 PostgreSQL 用例显式执行模型 `AutoMigrate`，未在各自用例中显式创建或直接验证结果部分唯一索引；不能据此认定结果重复插入拒绝、各状态占位、软删除后复用或实际部署迁移已覆盖。标准回归沿 [Manager 开发与验证](../../CLAUDE.md#开发与验证)的 `make test-manager-backend`、`make test-manager-postgres`，PostgreSQL 用例由现有 `TestIntegrationPostgresManager` 前缀命中。

实际数据库仍须只读核对索引所属表、字段顺序、唯一性、有效性、完整谓词及引用相关真实约束；同名索引、历史文件或用例存在不能作为运行通过证据。文档静态检查也不替代权限、对象完整读取、真实转换和失败清理验收。

## 六、删除语义

删除 GLB 快显结果时，Manager 必须先按 `storage_ref` 删除 infra MinIO artifact，再将结果状态标记为 `deleted` 并软删除记录。

不得删除：

1. 源业务文件或源 data item。
2. 对应的 `manager.task_definitions` 任务定义。
3. `common.task_executions` 执行历史。
4. `manager.preview_state` 中的用户预览偏好和三维相机状态。

## 七、相关文档

- [三维模型、点云与高斯泼溅预览说明](../三维模型、点云与高斯泼溅预览说明.md)
- [快显实现规范](../快显实现规范.md)
- [task_definitions 表结构说明](./task_definitions表.md)
- [数据库架构](../数据库架构.md)
