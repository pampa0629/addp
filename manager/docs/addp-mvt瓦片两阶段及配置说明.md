
# ADDP MVT 瓦片生成两阶段配置说明

> 状态：当前 PostGIS + MVT 格式实现备查。平台目标概念是“瓦片缓存生成任务”，TaskProvider `task_type=tile_cache_generation`，MVT 只是 `config.tile.format=mvt`。Quick View 是快显状态，不再承载完整瓦片缓存结果和准备状态事实。

## 一、两阶段工作流

当前 MVT 实现采用**检查 → 准备 → 生成**的三步流程：

```
【步骤1：检查诊断】瓦片缓存生成任务内部执行准备检查
    ↓
  快速检查物化视图、空间索引、统计信息是否就绪
  返回 overall_status: "passed" | "failed"
    ↓
【步骤2：准备工作】由瓦片缓存生成任务根据 config.preparation 执行（可选，仅当检查失败且任务授权时执行）
    ↓
  实际创建物化视图、空间索引、执行 ANALYZE
  保存执行结果到 execution metadata 或后续准备产物摘要
    ↓
【步骤3：瓦片生成】POST /api/v1/manager/tasks/tile_cache_generation/{id}/execute
    ↓
  前置条件：准备检查通过，或任务 config 明确允许所需准备动作
  从 checks 动态推导 QueryInfo（无需预先存储）
  生成 MVT 瓦片并保存到任务配置指定的 storage_ref
```

**核心设计**：
- **检查与执行分离**：检查只诊断，不修改数据库
- **QueryInfo 动态推导**：运行时从 checks 推导，无需冗余存储
- **强制顺序**：必须先完成检查，必要且授权时执行准备动作，再生成瓦片缓存结果

---

## 二、准备状态数据结构

### 2.1 PreparationStatus（当前实现结构，后续不再保存在 quick_view）

目标归属：

1. 当次检查和准备结果进入 `common.task_executions.metadata`。
2. 必要的准备产物摘要可进入后续准备产物专题。
3. `manager.quick_view` 只保留快显状态和推荐瓦片缓存结果。

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
        "target_srid": 3857
      }
    },
    {
      "name": "spatial_index",
      "status": "passed",
      "message": "空间索引已存在",
      "details": {
        "table": "public.dltb_mv3857",
        "column": "geom_3857"
      }
    },
    {
      "name": "analyze",
      "status": "passed",
      "message": "统计信息已更新",
      "details": {
        "last_analyze": "2026-01-25T10:30:10Z"
      }
    }
  ],
  "overall_status": "passed",
  "summary": "准备阶段全部通过，可以开始生成瓦片",
  "completed_at": "2026-01-25T10:30:10Z",
  "query_info": null,
  "execution_info": null
}
```

**字段说明**：
- `checks[]`：包含物化视图、空间索引、统计信息的完整检查结果
- `query_info`：在检查阶段为 `null`，瓦片缓存生成时从 checks 动态推导（包含实际查询表名、几何列名、SRID）
- `execution_info`：仅在实际执行准备工作时填充（执行器 ID、耗时等）

**状态值**：
- `passed`：已就绪（如物化视图已存在）
- `skipped`：不需要（如源表已是3857）
- `failed`：需要处理（如索引不存在）

---

## 三、优化配置

### 3.1 默认配置（推荐）

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
  }
}
```

**关键参数**：
- `zoom_threshold: 8`：z≤8 只返回主键，z>8 返回全部属性
- `max_size_mb: 5.0`：瓦片超过5MB时自动减半 extent
- `max_zoom_extent: 1024`：最高层级使用1024精度
- `base_extent: 512`：其他层级使用512精度

### 3.2 激进优化（超大数据集）

```json
{
  "extent_optimization": {
    "max_zoom_extent": 512,
    "base_extent": 256,
    "min_extent": 256
  },
  "tile_size_thresholds": {
    "max_size_mb": 3.0
  }
}
```

适用场景：>100万条记录，网络受限，优先速度

### 3.3 高质量配置（小数据集）

```json
{
  "extent_optimization": {
    "max_zoom_extent": 2048,
    "base_extent": 1024,
    "min_extent": 512
  },
  "tile_size_thresholds": {
    "max_size_mb": 8.0
  }
}
```

适用场景：<1万条记录，追求高质量，Retina 屏幕用户多

---

## 四、API 接口

### 4.1 检查准备状态（诊断）

准备检查由瓦片缓存生成任务内部执行，不再作为普通空间预览 API 暴露。检查结果进入当次 execution metadata，后续如需长期保存准备产物，再进入独立准备产物专题。

### 4.2 执行准备工作

准备动作不再暴露为普通空间预览 API。瓦片缓存生成任务执行时根据 `config.preparation` 判断是否允许创建物化视图、创建空间索引或执行 `ANALYZE`，相关结果写入 execution metadata 或后续准备产物摘要。

### 4.3 启动瓦片缓存生成

```
POST /api/v1/manager/tasks/tile_cache_generation/{id}/execute
```

通过标准任务入口执行瓦片缓存生成任务。

请求体：
```json
{
  "min_zoom": 5,
  "max_zoom": 16,
  "optimization_config": { ... }
}
```

前置条件：准备检查通过，或任务配置允许执行必要准备动作。

### 4.4 获取执行与快显状态

```
GET /api/v1/manager/executions/:execution_id
GET /api/v1/manager/quick-view/capability?locator={ResourceLocator}
```

响应：
```json
{
  "execution_id": "uuid",
  "status": "success",
  "metadata": {
    "tile_cache_id": 12,
    "total_tiles": 1888,
    "cached_tiles": 1234
  }
}
```

---

## 五、准备阶段详解

### 5.1 物化视图创建

**触发条件**：源表 SRID ≠ 3857

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

### 5.2 空间索引创建

**命名规则**：`idx_{table}_mv3857_geom_3857_gist`

**创建逻辑**：
```sql
CREATE INDEX CONCURRENTLY idx_{table}_mv3857_geom_3857_gist
ON {schema}.{table}_mv3857 USING GIST (geom_3857);
```

### 5.3 统计信息更新

```sql
ANALYZE {schema}.{table}_mv3857;
```

---

## 六、常见问题

### Q1: 为什么需要准备阶段？

- **物化视图**：预计算 ST_Transform，避免每次瓦片生成时重复转换（性能提升10-50倍）
- **空间索引**：加速空间查询，避免全表扫描
- **ANALYZE**：更新统计信息，优化查询计划

### Q2: QueryInfo 如何生成？

无需预先存储。当前瓦片缓存生成时从准备检查结果动态推导：
- 从 `materialized_view` check 判断是否需要物化视图
- 从 `spatial_index` check 提取查询表名和几何列
- SRID 固定为 3857

### Q3: 物化视图占用多少空间？

- 点数据：源表的 50-80%
- 线/面数据：源表的 80-120%

### Q4: PostgreSQL 内存配置

MVT 生成需要足够内存，建议配置（business/docker-compose.yml）：

```yaml
postgres:
  command:
    - postgres
    - -c
    - work_mem=64MB        # 最关键！
    - -c
    - shared_buffers=512MB
    - -c
    - maintenance_work_mem=256MB
```

### Q5: 瓦片太大如何优化？

按顺序调整：
1. 降低阈值：`max_size_mb: 5.0 → 3.0`
2. 降低 Extent：`base_extent: 512 → 256`
3. 提高属性阈值：`zoom_threshold: 8 → 10`

---

## 七、更新日志

### v2.1 (2026-01-27)

**QueryInfo 推导优化**：
- QueryInfo 不再预先生成，改为运行时从 checks 动态推导
- 避免信息冗余，checks 中已包含所有必要数据

**Extent 优化**：
- Max Zoom Extent: 2048 → 1024
- Base Extent: 1024 → 512
- 文件大小减少约 40-50%

### v2.0 (2026-01-26)

- API 统一为 `/quick-view` 路由前缀
- 两阶段工作流：准备 + 生成
- 新增 QueryInfo 和 ExecutionInfo 结构

### v1.0 (2026-01-25)

- 新增准备阶段：物化视图、空间索引、ANALYZE
- 简化配置：移除采样和几何简化
