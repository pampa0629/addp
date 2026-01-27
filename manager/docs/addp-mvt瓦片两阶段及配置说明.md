
# ADDP MVT 瓦片预缓存两阶段及配置说明

## 一、优化方案概述

### 1.1 优化目标

- **减少瓦片大小**：提升前端加载速度，减少网络传输量
- **保证视觉效果**：通过合理的 Extent 和属性配置确保地图清晰可读
- **自动化优化**：用户配置简单，系统自动选择最佳优化策略
- **数据安全性**：强制两阶段流程，防止数据库性能瓶颈

### 1.2 两阶段工作流（v2.0）

ADDP 采用**准备阶段 + 生成过程**的两个独立任务：

```
【阶段一：准备工作】(POST /quick-view/prepare)
    ↓
  1. 建立 3857 物化视图（如果源表不是 3857）
    ↓
  2. 建立空间索引（在物化视图的 3857 空间字段上）
    ↓
  3. 检查是否需要 ANALYZE（统计信息过期检查）
    ↓
  4. 保存准备状态到快显表（preparation_status 字段）
     - 包含每个检查的结果（passed/skipped/failed）
     - 所有诊断信息存储在 checks 数组
     - query_info = null（动态推导，不在诊断阶段生成）
     - execution_info = null（仅在实际执行准备工作时才填充）
    ↓
  5. 返回 overall_status
     - "passed" → 所有检查都成功，可开始预缓存
     - "failed" → 有检查项失败，提示用户
    ↓
状态转换: none → preparing → prepared
    ↓
【阶段二：预缓存生成】(POST /quick-view/pre-cache)
    ↓
  1. **强制检查**：preparation_status 必须为 "passed"
    ↓
  2. 推导 QueryInfo（从 checks 数组动态推导）
     - 从 materialized_view check 判断是否需要物化视图
     - 从 spatial_index check 获取实际查询表名和几何列
     - SRID 固定为 3857
    ↓
  3. 根据 zoom 级别应用属性优化（z<=8 只返回主键）
    ↓
  4. 使用分层 Extent 策略（max zoom: 1024, 其他: 512）
    ↓
  5. 生成瓦片并检查大小
     - 大小 <= 5MB → 保存瓦片
     - 大小 > 5MB → Extent 减半（1024→512→256），重新生成
    ↓
  6. **自动启用 MVT 模式**：设置 preferred_mode = "mvt"
    ↓
返回结果，状态转换: prepared → generating → ready
```

**核心原则**：
- 准备阶段（诊断）和执行阶段分离：CheckPreparation 只做诊断，不执行实际操作
- 准备阶段确保数据库性能最优（物化视图 + 索引 + 统计信息）
- 生成过程从 checks 动态推导 QueryInfo，避免冗余存储
- 强制顺序流程：必须完成准备才能执行预缓存
- 字段保护机制：preparation_status 不会被覆盖

---

## 二、准备阶段配置

### 2.1 物化视图自动创建

**触发条件**：源表的 SRID ≠ 3857

**命名规则**：`{schema}.{table}_mv3857`

**创建逻辑**：
```sql
CREATE MATERIALIZED VIEW {schema}.{table}_mv3857 AS
SELECT
  {primary_key},
  ST_Transform({geom_column}, 3857) AS geom_3857,
  {other_columns}
FROM {schema}.{table};
```

**刷新策略**：
- 物化视图创建后不自动刷新
- 用户可手动刷新（如果源表数据更新）
- 刷新命令：`REFRESH MATERIALIZED VIEW {schema}.{table}_mv3857;`

**注意事项**：
- 物化视图名称避免与现有表冲突
- 如果物化视图已存在，则检查是否需要重建（比对源表记录数）

---

### 2.2 空间索引自动创建

**触发条件**：物化视图的 `geom_3857` 字段没有空间索引

**索引命名**：`idx_{table}_mv3857_geom_3857_gist`

**创建逻辑**：
```sql
CREATE INDEX CONCURRENTLY idx_{table}_mv3857_geom_3857_gist
ON {schema}.{table}_mv3857 USING GIST (geom_3857);
```

**检查方法**：
```sql
SELECT COUNT(*)
FROM pg_indexes
WHERE schemaname = '{schema}'
  AND tablename = '{table}_mv3857'
  AND indexname LIKE '%geom_3857%gist%';
```

**注意事项**：
- 使用 `CONCURRENTLY` 避免锁表
- 创建索引可能耗时较长（取决于数据量）
- 如果索引创建失败，需提示用户手动处理

---

### 2.3 ANALYZE 检查

**触发条件**：物化视图从未执行过 ANALYZE

**检查逻辑**：
```sql
SELECT last_analyze, last_autoanalyze
FROM pg_stat_user_tables
WHERE schemaname = '{schema}'
  AND relname = '{table}_mv3857';
```

**执行条件**：
- `last_analyze` 和 `last_autoanalyze` 都为 NULL（从未执行过）
- 或者物化视图刚创建（刚创建的物化视图需要立即执行 ANALYZE）

**执行命令**：
```sql
ANALYZE {schema}.{table}_mv3857;
```

**注意事项**：
- ANALYZE 通常很快完成（几秒到几十秒）
- 如果执行失败，记录错误但不阻止生成过程
- **不使用时间阈值**：即使 ANALYZE 执行过很久，也不会重复执行（避免不必要的开销）

---

### 2.4 准备状态保存

**数据结构**（保存到 `quick_view.preparation_status` 字段）：

CheckPreparation 诊断阶段保存检查结果到 `preparation_status`。**重要**：诊断阶段仅保存检查结果（checks 数组）和执行元数据（execution_info），`query_info` 保持为 `null`。QueryInfo 不需要在诊断阶段生成，因为预缓存生成阶段可以从 checks 数组动态推导出来，避免冗余存储。

```json
{
  "version": "1.0",
  "checks": [
    {
      "name": "materialized_view",
      "status": "passed" | "failed" | "skipped",
      "message": "物化视图 public.dltb_mv3857 已存在",
      "details": {
        "view_name": "dltb_mv3857",
        "source_srid": 2360,
        "target_srid": 3857,
        "row_count": 1234567,
        "action_required": false
      },
      "checked_at": "2026-01-25T10:30:00Z"
    },
    {
      "name": "spatial_index",
      "status": "passed" | "failed" | "skipped",
      "message": "空间索引 public.dltb_mv3857 (geom_3857) 已存在",
      "details": {
        "index_name": "idx_dltb_mv3857_geom_3857_gist",
        "table": "public.dltb_mv3857",
        "column": "geom_3857",
        "source_srid": 2360,
        "action_required": false
      },
      "checked_at": "2026-01-25T10:30:05Z"
    },
    {
      "name": "analyze",
      "status": "passed" | "failed" | "skipped",
      "message": "统计信息已更新",
      "details": {
        "last_analyze": "2026-01-25T10:30:10Z",
        "last_autoanalyze": null,
        "action_required": false
      },
      "checked_at": "2026-01-25T10:30:10Z"
    }
  ],
  "overall_status": "passed" | "failed",
  "summary": "准备阶段全部通过，可以开始生成瓦片",
  "completed_at": "2026-01-25T10:30:10Z",
  "query_info": null,
  "execution_info": null
}
```

**重要说明**：
- `checks` 数组包含了所有准备工作的诊断信息，包括物化视图、空间索引、统计信息的完整状态
- `query_info` 在诊断阶段为 `null`，**由后续预缓存生成阶段在 Worker 处理 QuickViewTask 时从 checks 动态推导**
  - 这样设计的原因：checks 中已包含所有必要信息，无需冗余存储 query_info
  - 从 materialized_view check 可推导是否需要物化视图
  - 从 spatial_index check 可提取查询表名和几何列名
  - SRID 固定为 3857
- `execution_info` 在诊断阶段为 `null`，仅在实际执行准备工作（HandlePrepareForCreateMVTTask）时才会填充

**状态说明**：
- `passed`：检查通过（例如：物化视图已存在，索引已存在，统计信息已更新）
- `failed`：检查失败（例如：物化视图不存在，索引不存在，统计信息缺失）
- `skipped`：不需要执行（例如：源表已经是 3857，无需物化视图）

**overall_status**：
- `passed`：所有检查都是 `passed` 或 `skipped`，可以开始预缓存生成（QueryInfo 会自动推导）
- `failed`：至少有一个检查是 `failed`，需要先执行准备工作（PrepareForCreateMVT）或手动处理

---

### 2.5 QueryInfo 的推导（查询参数生成）

**核心概念**：QueryInfo 不是在诊断阶段生成并保存的，而是在预缓存生成阶段从 `preparation_status.checks` 动态推导。这样设计的优点是避免信息冗余，因为所有必要数据都已经在 checks 数组中。

**推导时机**：
- 当 Worker 处理 QuickViewTask（预缓存生成任务）时
- 检查 `preparation_status.query_info` 是否为 null
- 如果为 null，则从 checks 数组动态推导

**推导逻辑**：

从 checks 数组中提取关键信息：

1. **从 `materialized_view` check 判断是否存在物化视图**：
   - status = `skipped` → 源表已是 3857，无需物化视图，`materialized_view_exists = false`
   - status = `passed` → 物化视图存在，`materialized_view_exists = true`
   - status = `failed` → 物化视图不存在，overall_status 应为 failed，不会推导

2. **从 `spatial_index` check 获取实际查询表和几何列**：
   - `details.table` - 完整的表名（含 schema），例如 `"public.dltb_mv3857"` 或 `"public.dltb"`
   - `details.column` - 几何列名，例如 `"geom_3857"` 或 `"geom"`
   - **重要**：需要从 `details.table` 中解析出表名部分（不含 schema）

3. **SRID 固定为 3857**（所有坐标转换的最终目标）

**推导结果（PreparedQueryInfo 结构）**：

| 字段 | 说明 | 来源 |
|------|------|------|
| `materialized_view_exists` | 是否存在物化视图（boolean） | `materialized_view` check 的 status |
| `query_table` | 瓦片生成时实际查询的表名（仅表名，不含 schema） | 从 `spatial_index` check 的 `details.table` 提取表名部分 |
| `query_geom_column` | 实际查询的几何列名 | `spatial_index` check 的 `details.column` |
| `query_srid` | 实际查询的 SRID（固定为 3857） | 常数 3857 |

**三种情形的推导示例**：

**情形 1：源表已是 3857（materialized_view status = skipped）**

checks 中的相关信息：
```json
{
  "name": "materialized_view",
  "status": "skipped",
  "message": "源表已经是 3857 坐标系，无需物化视图",
  "details": {
    "source_srid": 3857,
    "target_srid": 3857,
    "action_required": false
  }
}
```

对应的 spatial_index check：
```json
{
  "name": "spatial_index",
  "status": "passed",
  "message": "空间索引 public.dltb (geom) 已存在",
  "details": {
    "table": "public.dltb",
    "column": "geom",
    "source_srid": 3857
  }
}
```

推导结果：
```json
{
  "materialized_view_exists": false,
  "query_table": "dltb",
  "query_geom_column": "geom",
  "query_srid": 3857
}
```

**情形 2：物化视图已存在（materialized_view status = passed）**

checks 中的相关信息：
```json
{
  "name": "materialized_view",
  "status": "passed",
  "message": "物化视图 public.dltb_mv3857 已存在",
  "details": {
    "view_name": "dltb_mv3857",
    "source_srid": 2360,
    "target_srid": 3857,
    "action_required": false
  }
}
```

对应的 spatial_index check：
```json
{
  "name": "spatial_index",
  "status": "passed",
  "message": "空间索引 public.dltb_mv3857 (geom_3857) 已存在",
  "details": {
    "table": "public.dltb_mv3857",
    "column": "geom_3857",
    "source_srid": 2360
  }
}
```

推导结果：
```json
{
  "materialized_view_exists": true,
  "query_table": "dltb_mv3857",
  "query_geom_column": "geom_3857",
  "query_srid": 3857
}
```

**情形 3：物化视图需要创建（materialized_view status = failed）**

此情况下 `overall_status` 为 `failed`，不满足预缓存前置条件，不会进行推导，用户需要先执行 PrepareForCreateMVT 创建物化视图。

**Go 代码实现示例**：

```go
// deriveQueryInfoFromChecks 从 preparation_status.checks 推导 QueryInfo
func deriveQueryInfoFromChecks(checks []models.PreparationCheck, schema string) *models.PreparedQueryInfo {
    queryInfo := &models.PreparedQueryInfo{
        QuerySRID: 3857, // 固定为 3857
    }

    // 遍历 checks 数组
    for _, check := range checks {
        switch check.Name {
        case "materialized_view":
            // 判断是否存在物化视图
            if check.Status == "skipped" {
                queryInfo.MaterializedViewExists = false
            } else if check.Status == "passed" {
                queryInfo.MaterializedViewExists = true
            }

        case "spatial_index":
            // 从 spatial_index check 提取表名和几何列
            if check.Status == "passed" || check.Status == "skipped" {
                if tableVal, ok := check.Details["table"]; ok {
                    if table, ok := tableVal.(string); ok {
                        // 从 "public.dltb_mv3857" 中提取 "dltb_mv3857"
                        parts := strings.Split(table, ".")
                        if len(parts) >= 2 {
                            queryInfo.QueryTable = parts[len(parts)-1]
                        } else {
                            queryInfo.QueryTable = table
                        }
                    }
                }

                if columnVal, ok := check.Details["column"]; ok {
                    if column, ok := columnVal.(string); ok {
                        queryInfo.QueryGeomColumn = column
                    }
                }
            }
        }
    }

    return queryInfo
}
```

**关键要点**：
- 推导是幂等的：同一份 checks 总是推导出相同的 QueryInfo
- 推导不修改数据库：只从内存中的 checks 提取信息
- 解析表名时需要处理 "schema.table" 格式，避免重复的 schema 前缀
- SRID 固定为 3857，这是所有瓦片生成的目标坐标系

---

### 2.6 ExecutionInfo（执行信息）

**用途**：记录准备任务的执行元数据，便于故障排查和性能分析

**字段说明**：

| 字段 | 说明 | 示例 |
|------|------|------|
| `started_at` | 准备任务开始时间 | `2026-01-25T10:30:00Z` |
| `completed_at` | 准备任务完成时间 | `2026-01-25T10:30:10Z` |
| `duration_sec` | 准备任务耗时（秒） | `10.5` |
| `worker_id` | 执行该任务的 Worker ID | `worker-12345` |
| `task_id` | Asynq 任务 ID | `prepare_for_create_mvt` |
| `retry_count` | 重试次数 | `0` |

**应用场景**：
- 性能分析：查看准备阶段的耗时
- 故障排查：知道哪个 Worker 执行了任务，便于查看相应的日志
- 任务追踪：通过 task_id 和 retry_count 追踪任务执行历史

---

## 三、生成过程配置

### 3.1 基础配置

#### 缩放层级范围

- **min_zoom**：最小缩放层级（默认根据数据范围自动计算）
  - 计算公式：根据数据地理范围计算合适的最小层级
  - 用户可手动调整

- **max_zoom**：最大缩放层级（默认根据数据量自动计算）
  - 计算公式：根据记录数确保每瓦片平均记录数 ≤ 3000
  - 用户可手动调整，最大 18

#### 属性优化

- **enabled**：是否启用属性优化（默认 `true`）

- **zoom_threshold**：属性分界层级（默认 `8`）
  - **z <= zoom_threshold（且非 max zoom）**：仅返回主键（id），减少数据量
  - **z > zoom_threshold 或 z = max zoom**：返回全部属性，便于查看详细信息

**特殊规则**：
- **Max zoom 总是返回全部属性**，即使 max zoom <= zoom_threshold
- 例如：zoom_threshold = 8，max_zoom = 6
  - z0-z5：仅返回主键
  - z6（max zoom）：返回全部属性

**说明**：低 zoom 级别时用户通常查看整体概况，不需要详细属性；高 zoom 级别和 max zoom 时用户需要查看具体信息。

---

### 3.2 Extent 分层策略

**Extent 决定 MVT 瓦片的内部分辨率**：

| Zoom 级别 | Extent 值 | 说明 |
|----------|----------|------|
| **Max zoom** | 1024 | 最高精度，适合高 zoom 级别 |
| **其他 zoom** | 512 | 标准精度，适合中低 zoom 级别 |

**说明**：
- Max zoom 层（通常 z16-z18）需要最高精度，使用 1024
- 其他层级（z0-z15）使用 512 即可满足视觉需求
- Extent 值越高，瓦片越大，但边界越精细
- 配合 tile_size=512，在普通屏幕上达到 1:1 或 2:1 的精度比，避免浪费

---

### 3.3 动态减半策略

**触发条件**：生成的瓦片大小超过阈值（默认 5MB）

**减半流程**：
1. 初始 Extent：1024（max zoom）或 512（其他 zoom）
2. 生成瓦片并检查大小
3. 如果大小 > 5MB：
   - Extent 减半：1024 → 512 → 256，或 512 → 256
   - 重新生成瓦片
4. 重复步骤 2-3，直到：
   - 瓦片大小 ≤ 5MB，或
   - Extent 已达到最小值 256

**最小 Extent**：256（不再继续减半）

**示例流程**：
```
生成瓦片（Extent = 1024）→ 8MB → 超过阈值
  ↓
Extent 减半为 512 → 重新生成 → 4.5MB → 通过
  ↓
保存瓦片
```

---

### 3.4 瓦片大小阈值

- **max_size_mb**：瓦片大小限制（默认 `5.0` MB）
  - 超过此值触发 Extent 减半
  - 用户可配置（建议范围：3-10 MB）

---

## 四、配置示例

### 4.1 默认配置（推荐）

```json
{
  "optimization_config": {
    "version": "4.0",
    "attribute_pruning": {
      "enabled": true,
      "zoom_threshold": 8
    },
    "tile_size_thresholds": {
      "max_size_mb": 5.0
    },
    "extent_optimization": {
      "max_zoom_extent": 1024,
      "base_extent": 512,
      "min_extent": 256
    }
  },
  "preparation_config": {
    "auto_create_materialized_view": true,
    "auto_create_spatial_index": true,
    "auto_analyze": true
  }
}
```

**适用场景**：
- 一般数据集（1万 - 100万条记录）
- 平衡质量和性能
- 大多数情况下推荐使用
- **优化 extent 与 tile_size 匹配，避免精度浪费**

**预期效果**：
- z0-z3 瓦片大小：< 1MB
- z4-z8 瓦片大小：< 2MB
- z9-z16 瓦片大小：< 3MB
- 视觉效果：保留所有要素，边界清晰
- **文件大小比旧配置（2048/1024）减少约 50%**

---

### 4.2 激进优化配置

```json
{
  "optimization_config": {
    "version": "4.0",
    "attribute_pruning": {
      "enabled": true,
      "zoom_threshold": 10
    },
    "tile_size_thresholds": {
      "max_size_mb": 3.0
    },
    "extent_optimization": {
      "max_zoom_extent": 512,
      "base_extent": 256,
      "min_extent": 256
    }
  },
  "preparation_config": {
    "auto_create_materialized_view": true,
    "auto_create_spatial_index": true,
    "auto_analyze": true
  }
}
```

**适用场景**：
- 超大数据集（> 100万条记录）
- 网络带宽受限
- 优先速度，可接受质量损失

**预期效果**：
- z0-z3 瓦片大小：< 0.5MB
- z4-z8 瓦片大小：< 1MB
- z9-z16 瓦片大小：< 2MB
- 视觉效果：保留所有要素，边界略粗糙
- **极致优化，文件大小最小化**

---

### 4.3 保守优化配置（高质量）

```json
{
  "optimization_config": {
    "version": "4.0",
    "attribute_pruning": {
      "enabled": true,
      "zoom_threshold": 6
    },
    "tile_size_thresholds": {
      "max_size_mb": 8.0
    },
    "extent_optimization": {
      "max_zoom_extent": 2048,
      "base_extent": 1024,
      "min_extent": 512
    }
  },
  "preparation_config": {
    "auto_create_materialized_view": true,
    "auto_create_spatial_index": true,
    "auto_analyze": true
  }
}
```

**适用场景**：
- 小数据集（< 1万条记录）
- 追求高质量
- 网络带宽充足
- **Retina 屏幕用户多**

**预期效果**：
- z0-z3 瓦片大小：< 3MB
- z4-z8 瓦片大小：< 5MB
- z9-z16 瓦片大小：< 8MB
- 视觉效果：保留所有要素和细节
- **高清晰度，适合 Retina 屏幕**

---

## 五、API 接口

### 5.1 准备阶段 API

**启动准备工作**

```
POST /api/manager/engines/:id/spatial/:schema/:table/quick-view/prepare
```

**请求体**：无（使用 schema、table、geom_column 自动探测）

**响应**：
```json
{
  "id": 123,
  "status": "preparing",
  "preparation_status": {
    "version": "1.0",
    "checks": [...],
    "overall_status": "passed",
    "query_info": null,
    "execution_info": null
  }
}
```

**说明**：
- `query_info` 在诊断阶段为 `null`
- 当预缓存生成时，Worker 会从 checks 数组动态推导 QueryInfo（预缓存用户无需关心此细节）

**状态码**：
- `200`：准备工作启动成功
- `400`：参数错误（例如：表不存在）
- `500`：数据库错误

---

**检查准备状态（诊断）**

```
GET /api/manager/engines/:id/spatial/:schema/:table/quick-view/check-preparation
```

**响应**：
```json
{
  "preparation_status": {
    "version": "1.0",
    "checks": [...],
    "overall_status": "passed",
    "query_info": null,
    "execution_info": null
  },
  "can_proceed": true  // true 表示可以开始预缓存（overall_status = "passed"）
}
```

**说明**：
- 诊断只读取 checks 数组和 overall_status，检查是否满足预缓存前置条件
- `query_info` 为 null，会在预缓存生成时从 checks 动态推导
- `can_proceed = true` 表示可以调用预缓存 API

---

### 5.2 预缓存生成 API

**启动预缓存生成**

```
POST /api/manager/engines/:id/spatial/:schema/:table/quick-view/pre-cache
```

**请求体**：
```json
{
  "min_zoom": 5,
  "max_zoom": 16,
  "concurrency": 4,
  "optimization_config": {...}
}
```

**响应**：
```json
{
  "id": 123,
  "status": "generating",
  "fingerprint": "abc123def456"
}
```

**前置条件检查**：
- `preparation_status` 必须存在且 `overall_status` = "passed"
- 如果 `query_info` 为 null，Worker 会自动从 checks 数组推导

**错误情况**：
- `400`：preparation_status 未完成或状态不是 "passed"
- `409`：已有其他生成任务在运行

---

**获取预缓存状态**

```
GET /api/manager/engines/:id/spatial/:schema/:table/quick-view/status
```

**响应**：
```json
{
  "status": "ready" | "generating" | "failed" | "cancelled",
  "progress": {
    "progress_percent": 65.5,
    "tiles_processed": 1234,
    "tiles_total_estimate": 1888,
    "elapsed_seconds": 45,
    "estimated_remaining_seconds": 20
  },
  "preferred_mode": "mvt"
}
```

---

**更新显示模式偏好**

```
PATCH /api/manager/engines/:id/spatial/:schema/:table/quick-view/preferred-mode
```

**请求体**：
```json
{
  "preferred_mode": "mvt" | "geojson"
}
```

**响应**：
```json
{
  "preferred_mode": "mvt"
}
```

---

### 5.3 任务管理 API

**取消预缓存生成**

```
POST /api/manager/engines/:id/spatial/:schema/:table/quick-view/cancel
```

**响应**：
```json
{
  "status": "cancelled"
}
```

---

**恢复预缓存生成**

```
POST /api/manager/engines/:id/spatial/:schema/:table/quick-view/resume
```

**响应**：
```json
{
  "status": "generating"
}
```

---

**清除预缓存**

```
DELETE /api/manager/engines/:id/spatial/:schema/:table/quick-view
```

**响应**：
```json
{
  "message": "预缓存已清除"
}
```

---

**列出所有快显任务（全局）**

```
GET /api/manager/quick-view/tasks?engine_id=1&status=ready&page=1&page_size=10
```

**响应**：
```json
{
  "data": [
    {
      "id": 123,
      "engine_id": 1,
      "schema": "public",
      "table": "dltb",
      "status": "ready",
      "preferred_mode": "mvt",
      "total_tiles": 5678,
      "cached_tiles": 5678,
      "created_at": "2026-01-25T10:00:00Z"
    }
  ],
  "total": 42,
  "page": 1,
  "page_size": 10
}
```

---

**获取快显统计（全局）**

```
GET /api/manager/quick-view/statistics
```

**响应**：
```json
{
  "total": 50,
  "generating": 2,
  "ready": 45,
  "failed": 3
}
```

---

## 六、前端交互流程

### 6.1 三阶段用户体验

**阶段 1：准备工作（自动或手动）**

用户点击"🚀 启用预缓存"按钮 → 触发准备工作：
1. 后端执行 `POST /quick-view/prepare`
2. 前端显示"正在准备..."，禁用其他操作
3. 准备完成后，根据结果：
   - ✅ **全部通过**：自动进入阶段 2（预缓存生成）
   - ❌ **有失败项**：显示失败原因，用户可手动处理后点击"重新检查"

**阶段 2：预缓存生成（自动）**

准备完成后自动触发 `POST /quick-view/pre-cache`：
1. 前端显示进度条（已生成瓦片数 / 预估总瓦片数）
2. 用户可点击"⏸️ 取消"中止生成
3. 生成完成后，前端显示"✅ 已预缓存"

**阶段 3：显示模式选择（用户手动）**

预缓存完成后，用户可切换显示模式：
- 默认：**⚡ MVT 模式**（高性能，推荐）
- 可选：🔵 **GeoJSON 模式**（轻量级，适合小数据集）

---

### 6.2 准备阶段失败提示

**示例失败提示框**：

```
⚠️ 准备阶段检查失败

以下项目需要手动处理：

❌ 物化视图创建失败
   原因：磁盘空间不足
   建议：
   1. 清理磁盘空间（至少需要 2GB）
   2. 点击"重新检查"重试
   3. 如果问题持续，请联系管理员

✅ 空间索引已存在

✅ 统计信息已更新

请在后台处理后，点击"重新检查"继续。

[重新检查]  [取消]
```

---

### 6.3 生成过程日志

**前端显示的进度信息**：

```
🚀 快显预缓存生成中...

进度：
  已生成：1234 / 1888 瓦片（65.5%）
  已用时：45 秒
  预计剩余：20 秒
  当前处理：z10 级别

[⏸️ 取消]
```

---

## 七、常见问题

### Q1: 为什么需要准备阶段？

**A**: 准备阶段确保数据库性能最优，避免瓦片生成过程中的性能瓶颈：

1. **物化视图**：预计算 ST_Transform，避免每次生成瓦片时重复转换（性能提升 10-50 倍）
2. **空间索引**：加速空间查询（ST_Intersects、&&），避免全表扫描
3. **ANALYZE**：更新统计信息，帮助查询优化器选择最佳执行计划

### Q2: 准备工作和预缓存生成可以分别执行吗？

**A**: 可以。两个任务是独立的：
- 用户可以先执行准备工作（`/quick-view/prepare`），稍后再启动预缓存（`/quick-view/pre-cache`）
- 这样可以在确保数据库准备好后，再选择合适的时间启动预缓存生成
- 支持幂等性：准备工作可以安全地重试

### Q3: 物化视图会占用多少磁盘空间？

**A**: 物化视图的大小取决于源表的记录数和几何复杂度：

- **点数据**：约为源表的 50-80%（只存储转换后的坐标）
- **线/面数据**：约为源表的 80-120%（几何数据量大）

**示例**：
- 源表 100 万条记录，大小 2GB
- 物化视图预计大小：1.6-2.4 GB

**优化建议**：
- 物化视图只包含必要字段（主键 + geom_3857 + 常用属性）
- 不包含大字段（TEXT、JSON 等）

### Q4: 准备阶段需要多长时间？

**A**: 取决于数据量和数据库性能：

| 记录数 | 物化视图创建 | 空间索引创建 | ANALYZE | 总耗时 |
|-------|------------|------------|---------|--------|
| 1 万 | 1-2 秒 | 2-5 秒 | < 1 秒 | 5-10 秒 |
| 10 万 | 5-10 秒 | 10-20 秒 | 1-2 秒 | 20-30 秒 |
| 100 万 | 30-60 秒 | 1-3 分钟 | 5-10 秒 | 2-4 分钟 |
| 1000 万 | 5-10 分钟 | 10-30 分钟 | 30-60 秒 | 15-40 分钟 |

**注意**：
- 使用 `CREATE INDEX CONCURRENTLY` 不阻塞表访问
- 物化视图创建期间会占用较多 CPU 和内存

### Q7: 为什么 v4.0 修改了默认 extent 值？

**变更**：
- Max Zoom Extent: 2048 → **1024**
- Base Extent: 1024 → **512**

**原因**：

1. **精度与渲染匹配**：
   - 大多数地图库使用 tile_size = 512px
   - 旧配置（2048/1024）精度比 = 4:1 或 2:1，存在浪费
   - 新配置（1024/512）精度比 = 2:1 或 1:1，更合理

2. **文件大小优化**：
   - extent 降低 50%，瓦片大小减少约 40-50%
   - 加载速度提升明显，节省带宽

3. **普通屏幕优先**：
   - 大多数用户使用普通屏幕（devicePixelRatio = 1）
   - 新配置在普通屏上无精度浪费
   - Retina 屏幕仍有足够精度（2:1 比例可接受）

4. **动态减半机制**：
   - 超过 5MB 自动减半到 512/256
   - 最小值仍为 256，保障视觉质量

**如果需要更高精度**（Retina 屏多、overzooming 频繁），使用"保守优化配置"（见 4.3）。

---

### Q5: 瓦片太大，如何优化？

**症状**：
- 前端加载缓慢
- 浏览器卡顿或崩溃
- 瓦片大小超过 5MB

**解决方案**：按以下顺序调整参数：

1. **降低阈值**：`max_size_mb: 5.0 → 3.0`
2. **降低基础 Extent**：`base_extent: 512 → 256`
3. **降低 Max Zoom Extent**：`max_zoom_extent: 1024 → 512`
4. **提高属性阈值**：`zoom_threshold: 8 → 10`（更多层级只返回主键）

**示例**：切换到"激进优化配置"（见 4.2）

**注意**：v4.0 默认配置已优化，extent 与 tile_size 匹配（512/1024），相比旧配置（1024/2048）文件大小减少约 50%

### Q6: PostgreSQL 内存不足导致瓦片生成失败？

**症状**：
- 瓦片生成速度极慢（例如：17 分钟只生成 7 个瓦片）
- 后台日志出现 `ERROR: could not resize shared memory segment` 错误
- 单个瓦片查询耗时超过 10 秒

**根本原因**：

MVT 瓦片生成涉及复杂的空间查询操作（ST_AsMVT、ST_Transform、ST_Intersects 等），这些操作需要较大的内存空间。

**解决方案**：

需要为业务数据库（`business/docker-compose.yml`）优化 PostgreSQL 内存配置：

```yaml
postgres:
  image: ${POSTGRES_IMAGE:-imresamu/postgis-arm64:15-3.4}
  container_name: business-postgres
  command:
    - postgres
    - -c
    - shared_buffers=512MB
    - -c
    - work_mem=64MB         # 最关键！每个查询操作可用的内存
    - -c
    - maintenance_work_mem=256MB
    - -c
    - max_connections=100
    - -c
    - effective_cache_size=2GB
```

**关键参数说明**：

| 参数 | 建议值 | 说明 |
|-----|-------|------|
| `work_mem` | **64MB** | **最关键**！MVT 查询通常需要 10-30 MB |
| `shared_buffers` | 512MB | 共享缓冲区，缓存数据库页面 |
| `maintenance_work_mem` | 256MB | 维护操作（CREATE INDEX、VACUUM）使用的内存 |
| `effective_cache_size` | 2GB | 操作系统缓存提示 |

---

---

## 七、技术细节

### 7.1 物化视图 SQL

```sql
-- 创建物化视图
CREATE MATERIALIZED VIEW {schema}.{table}_mv3857 AS
SELECT
  {primary_key},
  ST_Transform({geom_column}, 3857) AS geom_3857
  -- 不包含其他属性，减少存储空间
FROM {schema}.{table};

-- 创建空间索引
CREATE INDEX CONCURRENTLY idx_{table}_mv3857_geom_3857_gist
ON {schema}.{table}_mv3857 USING GIST (geom_3857);

-- 更新统计信息
ANALYZE {schema}.{table}_mv3857;
```

### 7.2 MVT 查询 SQL（简化版）

```sql
WITH b AS (
  SELECT
    ST_TileEnvelope($1, $2, $3) AS g3857
)
SELECT ST_AsMVT(m, $4, $5, 'geom')
FROM (
  SELECT
    ST_AsMVTGeom(
      t.geom_3857,  -- 直接使用物化视图的 3857 字段，无需 ST_Transform
      b.g3857,
      $5,  -- extent
      $6,  -- buffer
      true
    ) AS geom,
    t.{primary_key}  -- 属性优化：只返回主键（z<=8）或全部属性（z>8）
  FROM {schema}.{table}_mv3857 AS t, b
  WHERE t.geom_3857 && b.g3857
    AND ST_Intersects(t.geom_3857, b.g3857)
) AS m
```

**关键优化**：
1. 使用物化视图，避免 `ST_Transform(geom, 3857)`
2. 空间索引加速 `&&` 和 `ST_Intersects`
3. 属性优化减少数据传输量

### 7.3 Extent 动态减半逻辑

```go
func generateTileWithDynamicExtent(ctx context.Context, params TileGenerationParams) ([]byte, error) {
    // 初始 Extent
    currentExtent := params.OptimizationConfig.ExtentOptimization.GetExtentForZoom(params.Z, params.MaxZoom)
    minExtent := params.OptimizationConfig.ExtentOptimization.MinExtent
    maxSizeMB := params.OptimizationConfig.TileSizeThresholds.MaxSizeMB

    for {
        // 生成瓦片
        tileData, err := generateTileWithExtent(ctx, params, currentExtent)
        if err != nil {
            return nil, err
        }

        // 检查瓦片大小
        tileSizeMB := float64(len(tileData)) / (1024 * 1024)

        if tileSizeMB <= maxSizeMB || currentExtent <= minExtent {
            // 瓦片大小符合要求，或已达到最小 Extent
            return tileData, nil
        }

        // Extent 减半，重新生成
        currentExtent = currentExtent / 2
        logger.L().Warn("瓦片过大，Extent 减半重试",
            "z", params.Z, "x", params.X, "y", params.Y,
            "size_mb", tileSizeMB,
            "new_extent", currentExtent,
        )
    }
}
```

### 7.4 字段保护机制

**问题**：全量 Save() 操作会覆盖 preparation_status 字段

**解决方案**：使用 GORM 的 `Updates(map[string]interface{})` 方法，只更新必要字段

```go
// ❌ 错误做法（会覆盖 preparation_status）
h.db.Save(&qv)

// ✅ 正确做法（只更新必要字段）
h.db.Model(&qv).Updates(map[string]interface{}{
    "status":              "generating",
    "started_at":          now,
    "min_zoom":            params.MinZoom,
    "max_zoom":            params.MaxZoom,
    "optimization_config": params.OptimizationConfig,
})

// ✅ 更好做法（使用 Repository 层的专用方法）
h.repo.UpdateGenerationResultWithPreferredMode(qv.ID, updateParams)
```

---

## 八、更新日志

### v2.1 (2026-01-27) ⭐ 最新版本

**QueryInfo 推导优化**：
- **改变架构**：QueryInfo 不在诊断阶段生成，而是在预缓存生成阶段从 checks 动态推导
  - 诊断阶段（CheckPreparation）：仅保存 checks 数组和 overall_status，`query_info = null`
  - 生成阶段（Worker.HandleQuickViewTask）：从 checks 自动推导 QueryInfo
  - 优势：避免信息冗余，checks 中已包含所有必要数据

**实现细节**：
- 新增 `deriveQueryInfoFromChecks()` 方法，从 checks 数组动态推导 QueryInfo
  - 从 `materialized_view` check 判断是否存在物化视图
  - 从 `spatial_index` check 提取查询表名（须解析 "schema.table" 格式）和几何列名
  - SRID 固定为 3857
- Worker 改进：如果 `query_info` 为 null，自动调用推导方法

**文档更新**：
- 详细说明 QueryInfo 推导逻辑和三种情形
- 提供 Go 代码实现示例
- 更新 API 文档说明

**向后兼容**：
- 已有的包含 query_info 的快显记录继续使用现有数据
- 新的快显记录使用推导方式
- 推导是幂等的，多次调用结果一致

**Extent 优化调整**：
- **降低默认 Extent 值，优化精度与渲染匹配**
  - Max Zoom Extent: 2048 → **1024**（减少 50%）
  - Base Extent: 1024 → **512**（减少 50%）
  - Min Extent: 256（保持不变）

**优化效果**：
- **文件大小减少约 40-50%**，加载速度显著提升
- 精度比更合理：普通屏幕 1:1 或 2:1，Retina 屏幕 2:1 或 4:1
- 避免精度浪费，节省带宽和存储空间
- 动态减半策略保持不变（超过 5MB 自动减半到 256）

**影响范围**：
- 后端：handler.go、preparation_service.go、common/models/optimization.go
- 前端：TablePreview.vue
- 文档：本文档全面更新

**向后兼容**：
- 已生成的瓦片不受影响，继续使用
- 新生成的瓦片使用新配置
- 用户可通过"保守优化配置"恢复高精度（2048/1024）

---

### v2.0 (2026-01-26)

**核心改进**：
- **API 重构**：统一为 `/quick-view` 路由前缀
  - `POST /quick-view/prepare` - 启动准备工作
  - `POST /quick-view/pre-cache` - 启动预缓存生成
  - `GET /quick-view/status` - 获取状态
  - `PATCH /quick-view/preferred-mode` - 更新显示模式
  - 其他辅助 API（cancel、resume、clear 等）

- **数据结构增强**：
  - 新增 `QueryInfo` - 供预缓存复用，包含实际查询表、几何列、SRID
  - 新增 `ExecutionInfo` - 记录执行元数据（Worker ID、耗时、重试次数等）
  - 优化了 `PreparationStatus` 结构，支持完整的执行追踪

- **两阶段工作流改进**：
  - 准备工作和预缓存生成变为独立的两个任务
  - 强制流程：必须完成准备（status = "passed"）才能执行预缓存
  - 支持幂等性：准备工作可以安全地重试
  - 字段保护机制：preparation_status 不会被预缓存流程覆盖

- **Worker 层改进**：
  - `HandlePrepareForCreateMVTTask`：完整的准备逻辑，包含结果保存
  - `HandleQuickViewTask`：强制检查 preparation_status，复用 QueryInfo
  - 自动启用 MVT 模式：预缓存完成后自动设置 preferred_mode = "mvt"

- **Service 层改进**：
  - `TriggerQuickView`：完整的检查流程（preparation + generating）
  - `UpdatePreferredMode`：独立的显示模式更新方法
  - 完整的参数验证和字段保护

- **Repository 层**：
  - `UpdatePreparationStatusAtomic` - 原子性更新准备状态
  - `UpdateGenerationResultWithPreferredMode` - 预缓存完成后自动设置 preferred_mode
  - `IsPreparationCompleted` - 检查准备是否完成

**前端改进**：
- 更新所有 API 调用路径为新的 `/quick-view` 前缀
- 识别新增的 "prepared" 状态
- 改进的状态流程显示

**迁移**：
- 新增索引 `idx_quick_view_preparation_status` 优化查询性能

---

### v1.0 (2026-01-25)
- **新增准备阶段**：自动创建物化视图、空间索引、ANALYZE
- **删除采样优化**：移除对象采样相关配置和代码
- **删除几何简化**：移除 ST_SimplifyVW、RemoveRepeatedPoints 等代码
- **简化配置**：只保留属性优化和 Extent 优化
- **前端增强**：增加准备阶段状态显示和"完成准备"按钮

### v3.0 (2025-01-21)
- 调整优化顺序：Extent → 采样 → 几何
- 参数命名用户友好化

### v2.0 (2025-01-15)
- 增加采样和简化优化
- 详细配置说明文档

### v0.1 (2024-12-01)
- 初版实现
- 基础 MVT 瓦片生成

---

## 九、相关文档

- [Manager 模块说明](../CLAUDE.md)
- [开发原则](../../docs/addp开发原则.md)
- [API 设计规范](../../docs/addp-api设计规范.md)