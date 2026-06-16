
# ADDP MVT 瓦片生成配置说明

> 状态：当前 PostGIS + MVT 格式实现备查。平台目标概念是“瓦片缓存生成任务”，TaskProvider `task_type=tile_cache_generation`，MVT 只是 `config.tile.format=mvt`。Quick View 能力由 Capability API 动态合成，`manager.quick_view` 只保存用户预览模式偏好，不再承载完整瓦片缓存结果和准备状态事实。3857 快显优化目标由 `quick_view_optimization` 任务创建和管理，瓦片缓存生成任务不再隐式创建物化视图、空间索引或执行准备动作。

## 一、生成工作流

当前 MVT 瓦片缓存生成采用**诊断 → 选择生成目标 → 生成**流程：

```
【步骤1：诊断】瓦片缓存生成任务读取源 item 和快显能力事实
    ↓
  判断源 SRID、几何列、渲染范围、ready 快显性能优化目标和索引状态
    ↓
【步骤2：选择生成目标】优先使用 Manager 登记的 ready 快显性能优化目标
    ↓
  缺少 ready 优化目标时仍使用源表生成，并在 execution metadata 中记录快显性能优化推荐
    ↓
【步骤3：瓦片生成】POST /api/v1/manager/tasks/tile_cache_generation/{id}/execute
    ↓
  生成 MVT 瓦片并保存到任务配置指定的 storage_ref
```

**核心设计**：
- **瓦片缓存任务不准备派生产物**：不再根据 `config.preparation` 隐式创建 3857 物化视图或索引。
- **快显优化目标独立管理**：`quick_view_optimization` 任务负责创建、刷新和删除 Manager 管理的 3857 优化目标。
- **生成目标可诊断**：当次实际使用的 schema、table、geometry column、SRID 和是否为快显性能优化目标写入 execution metadata。

---

## 二、优化配置

### 2.1 默认配置（推荐）

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

### 2.2 激进优化（超大数据集）

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

### 2.3 高质量配置（小数据集）

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

## 三、API 接口

### 3.1 检查优化目标状态（诊断）

准备检查不再作为普通空间预览 API 暴露，也不再作为瓦片缓存生成任务的主路径。是否存在可复用 3857 优化目标由 Quick View Capability 和 `manager.quick_view_optimization` 结果表达。

### 3.2 执行快显性能优化

创建 3857 物化视图、`geom_3857` GiST 索引和 `ANALYZE` 由 `quick_view_optimization` 任务显式执行；瓦片缓存生成任务只消费 ready 快显性能优化目标，缺少目标时走源表生成路径并记录推荐。

### 3.3 启动瓦片缓存生成

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

前置条件：任务配置完整且源 item 可解析。ready 快显性能优化目标不是硬性前置条件；缺少目标时任务仍可执行，但会记录 `optimization_recommended=true`。

### 3.4 获取执行与快显状态

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

## 四、快显性能优化目标

### 4.1 物化视图创建

**触发条件**：用户显式创建并执行 `quick_view_optimization` 任务，且源表 SRID ≠ 3857。

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

### 4.2 空间索引创建

**命名规则**：`idx_{table}_mv3857_geom_3857_gist`

**创建逻辑**：
```sql
CREATE INDEX CONCURRENTLY idx_{table}_mv3857_geom_3857_gist
ON {schema}.{table}_mv3857 USING GIST (geom_3857);
```

### 4.3 统计信息更新

```sql
ANALYZE {schema}.{table}_mv3857;
```

---

## 五、常见问题

### Q1: 为什么需要快显性能优化目标？

- **物化视图**：预计算 ST_Transform，避免每次瓦片生成时重复转换（性能提升10-50倍）
- **空间索引**：加速空间查询，避免全表扫描
- **ANALYZE**：更新统计信息，优化查询计划

### Q2: 瓦片缓存生成如何选择查询目标？

当前瓦片缓存生成时调用快显实时瓦片目标解析：
- 存在 ready 快显性能优化目标且 `geom_3857` 有有效 GiST 索引时，使用该目标。
- 源表本身是 3857 且可索引时，使用源表 3857 路径。
- 缺少 ready 优化目标时，使用源表生成，并在 execution metadata 中记录快显性能优化推荐。

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

## 六、当前收敛点

- 3857 派生目标收敛到 `quick_view_optimization` 任务体系。
- 瓦片缓存任务删除 `config.preparation` 主路径，不再隐式创建物化视图或索引。
- 当次生成目标统一记录到 execution metadata 的 `tile_generation_target`。
