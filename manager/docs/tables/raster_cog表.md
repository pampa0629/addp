# raster_cog 表

## 定位

`manager.raster_cog` 是 Manager 拥有生命周期的栅格快显 COG 生成结果表。

源数据仍是原始 TIFF/GeoTIFF/COG item，可能位于 NFS、业务 MinIO 或其他存储引擎。`raster_cog` 只登记前端可通过 Manager 受控接口消费的 infra MinIO COG 副本，不把 COG 变成新的基础 `format`，也不进入 `vector_tile_cache` 或 `vector_materialized_view_generation`。

raster COG 的生成任务定义写入 `manager.task_definitions`，TaskProvider `task_type=raster_cog_generation`。本表只表达结果状态。

## 核心字段

| 字段 | 说明 |
| --- | --- |
| `tenant_id` | 租户。 |
| `item_fingerprint` | 源 item 当前身份指纹。当前结果身份按 `tenant_id + item_fingerprint` 收敛，约束范围见[索引声明与核验边界](#索引声明与核验边界)。 |
| `locator` | 源 item ResourceLocator，用于回跳和判断 stale。 |
| `source_engine_id` | 生成 COG 结果时的源引擎事实，用于判断 stale。 |
| `task_id` | 产生或最近刷新该 COG 结果的 `manager.task_definitions.id`。 |
| `source_profile` | 源 TIFF profile，如 `geotiff` 或 `cog`。 |
| `target_kind` | 当前固定为 `infra_minio_object`。 |
| `storage_ref` | Manager infra MinIO 对象引用，结构化 JSON，不暴露给前端拼接 URL。 |
| `width` / `height` / `band_count` | 栅格基础事实。 |
| `source_srid` / `source_crs` | 源坐标参考事实。 |
| `extent` / `extent_srid` | 可渲染范围事实。 |
| `status` | COG 结果状态，取值见下文。 |
| `metadata` | 转换过程的补充事实、渲染统计和 direct 调用审计信息。 |
| `last_execution_id` | 最近一次生成执行。 |

## 索引声明与核验边界

除 `id` 主键外，[RasterCOG 模型](../../backend/internal/models/raster_cog.go)与[初始化 `ensureRasterCOGSchema`](../../backend/internal/repository/database.go)的索引声明如下：

| 来源 | 索引 | 字段与用途 |
| --- | --- | --- |
| GORM 普通索引 | `idx_raster_cog_tenant_item_fingerprint` | `(tenant_id, item_fingerprint)`；按源 item 身份回查 |
| GORM 普通索引 | `idx_raster_cog_tenant_item` | `(tenant_id, item_id)`；按当前 Meta item 行引用回查 |
| GORM 普通索引 | `idx_raster_cog_task` | `task_id`；按产生或最近刷新结果的任务回查 |
| GORM 普通索引 | `idx_raster_cog_execution` | `last_execution_id`；回查最近一次生成执行 |
| GORM 普通索引 | `idx_raster_cog_status` | `status`；结果状态过滤 |
| GORM 普通索引 | `idx_raster_cog_deleted_at` | `deleted_at`；软删除字段索引 |
| 初始化显式 SQL 部分唯一索引 | `idx_raster_cog_current_unique` | `(tenant_id, item_fingerprint)`，仅覆盖 `deleted_at IS NULL AND status <> 'deleted'` |

模型未以 `uniqueIndex` 声明该部分唯一索引；初始化先执行模型 `AutoMigrate`，再运行显式 SQL。`building`、`ready`、`failed` 和 `stale` 均在占位范围内，不能将其理解为“仅 ready 结果唯一”。SQL 没有排除空指纹；字段 `NOT NULL` 也不等于非空业务身份校验。源引擎、任务、execution、内容版本及生成参数均不参与该结果唯一键。

[GetCurrentByFingerprint](../../backend/internal/repository/raster_cog_repository.go)读取未软删除且状态不是 `deleted` 的当前结果；`GetLatestReadyByFingerprint` 进一步要求 `ready`。唯一占位不证明 COG 对象存在、来源仍有效或当前用户有权读取，消费条件仍见[消费规则](#消费规则)及[内容与任务接口](../quick-view/TIFF与COG.md#内容与任务接口)。结果删除仍须履行[删除语义](#删除语义)，不能为释放占位单独改状态或删除数据库记录。

[历史 022 SQL](../../backend/migrations/022_create_raster_cog.sql)包含同列、同谓词的结果唯一索引，但还创建旧 `raster_cog_tasks`；[024 SQL](../../backend/migrations/024_normalize_raster_cog_task_result_config.sql)也操作旧任务表，[025 SQL](../../backend/migrations/025_drop_raster_cog_source_locator.sql)只删除重复的 `source_locator` 字段。这些文件不证明当前部署已迁移到统一任务表。初始化还更新任务配置和字段，不是只读检查入口；启动与历史 SQL 的归属统一见[Schema 收敛方式](../数据库架构.md#schema-收敛方式)。

### 任务与执行引用

`task_id`、`last_execution_id` 在模型中均可空，分别是统一任务与执行事实的应用引用；普通索引不保证被引用对象存在，当前模型与上述 SQL 未声明外键、复合租户／任务类型约束或 `ON DELETE CASCADE`。任务类型固定为 `raster_cog_generation` 的约束由应用链路维护，不是结果唯一索引的能力。

[`prepareResult`](../../backend/internal/service/raster_cog_task_service.go)生成或刷新当前结果时写入本次任务 ID 和 execution ID；刷新复用当前结果行，不按 execution 新建一份唯一身份。完成时的结果写入还受 Tenant、结果 ID、`last_execution_id` 和当前 lease 核验约束，完整事务与 fencing 见[执行与租约](../quick-view/TIFF与COG.md#执行与租约)，不将普通索引当成执行归属证明。

任务删除与结果删除是独立动作：统一任务删除事务处理定义与资源绑定，不自动删除 COG 结果／对象，也不自动清空结果中的旧任务引用；结果删除不删除任务或 execution 历史。失效任务合并不将旧结果改挂给替代任务。任务及绑定的软／硬删除范围分别见[任务资源与结果边界](task_definitions表.md#三资源与结果边界)、[投影替换与删除边界](task_resource_bindings表.md#投影替换与删除边界)，单对象清理与失败保留规则见[共用清理契约](../quick-view/共用契约.md#单对象结果清理)。

### 测试与实际数据库边界

- PostgreSQL `TestIntegrationPostgresManagerRasterCOGConcurrentClaimAndStart` 显式执行模型 `AutoMigrate`，断言同任务两次并发领取只有一次成功，以及 pending／running 与任务摘要；该用例没有显式创建或核验结果部分唯一索引。
- `TestIntegrationPostgresManagerManagedTaskSemanticIdentityIndexes` 调用 COG 初始化，但重复拒绝断言针对任务定义的通用语义索引与 COG 专用表达式索引，不证明 COG 结果唯一性。任务专用表达式索引与通用任务唯一键统一见[任务定义索引说明](task_definitions表.md#索引声明与唯一性边界)。
- SQLite `TestRasterCOGPrepareResultReturnsUpdatedExistingResult` 验证已有 stale 结果更新后返回新的存储引用、栅格事实及 building 状态；它不构成 PostgreSQL 约束或真实对象交付证据。

标准回归沿 [Manager 开发与验证](../../CLAUDE.md#开发与验证)的 `make test-manager-backend`、`make test-manager-postgres`。上述断言不覆盖结果重复插入的数据库拒绝、各状态占位、软删除后复用及任务删除后结果引用保留的完整矩阵；不从用例名称或源码存在推定通过。

实际部署须只读核对索引所属表、列顺序、唯一性、有效性、完整谓词，以及引用相关真实约束与数据；同名索引或历史 SQL 存在不足以证明落库结果。文档静态检查不替代当前数据库、权限、COG 内容读取及真实生成／清理验收。

## metadata 审计结构

`raster_cog` 通过 GeoPython Workflow 的 `raster_to_cog` direct 算子生成。Direct 调用不进入 Orchestrator，也不由 Monitor 统一监控，因此 Manager 必须在 COG 结果状态中保存最小审计信息：

```json
{
  "source": {
    "access": {
      "engine_type": "nfs",
      "access_method": "mounted_path"
    }
  },
  "workflow_runtime": {
    "engine_id": 99,
    "engine_name": "GeoPython Workflow",
    "engine_type": "geopython_workflow",
    "execution_id": "py-1",
    "operator": "raster_to_cog",
    "mode": "direct",
    "execution_time_ms": 45.5
  },
  "raster_facts": {
    "profile": "cog",
    "width": 256,
    "height": 128,
    "band_count": 3,
    "nodata": -32768,
    "display_min": -49,
    "display_max": 406,
    "display_range_method": "metadata_statistics",
    "extent": [110, 20, 120, 30],
    "extent_srid": 4326
  }
}
```

`storage_ref`、`locator`、`item_fingerprint`、`last_execution_id` 和 `status` 是一等字段；`metadata` 只保存补充事实和运行时审计，不作为 COG 结果主身份。

## 状态

| 状态 | 语义 |
| --- | --- |
| `building` | 正在生成。 |
| `ready` | 可被 `/api/v1/manager/raster_cog/{id}/content` 读取。 |
| `failed` | 生成失败。 |
| `stale` | COG 结果与当前 item facts 不匹配，不能用于渲染。 |
| `deleted` | 已删除。 |

这些状态是 COG 结果状态，不是统一 execution status。执行过程记录在 `common.task_executions`。

## 消费规则

Quick View Capability 查询时：

1. 先根据 `tenant_id + item_fingerprint` 查最新 ready raster COG。
2. 校验 `source_engine_id`、`locator`、宽高、SRID 等关键 facts。
3. 校验通过返回 `render_source=client_cog_render`，`quick_view.preview_url=/api/v1/manager/raster_cog/{id}/content`。
4. 校验失败将 COG 结果标记为 `stale`，继续按当前源 TIFF facts 返回生成建议。
5. 若 COG 结果 metadata 或源 item attributes 中存在 `nodata`、`display_min`、`display_max` 等渲染统计，Quick View capability 应透传到 `raster` 字段；前端仍可在缺失时读取 COG 内部 GDAL metadata 作为运行时兜底。
6. ready COG 必须具备标准 GeoTIFF CRS 标识。对于 WGS84 经纬度 COG，应能被常见 GeoTIFF 工具识别为 `GCS_WGS_84` / `EPSG:4326`，不能只写成 `User-Defined` GeoKey；否则必须视为不可可靠渲染并重新生成。

前端只消费 `preview_url`，不得读取或解析 `storage_ref`。

## 删除语义

通过 `DELETE /api/v1/manager/raster_cog/{id}` 删除 COG 生成结果时，Manager 必须先按 `storage_ref` 删除 infra MinIO 对象，再将结果状态标记为 `deleted` 并软删除记录。缺少物理清理器时不得静默删除状态，避免形成孤儿对象。
