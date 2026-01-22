# ADDP MVT 瓦片简化配置说明

## 一、优化方案概述

### 1.1 优化目标

- **减少瓦片大小**：提升前端加载速度，减少网络传输量
- **保证视觉效果**：优先保留重要要素，确保地图清晰可读
- **自动化优化**：用户配置简单，系统自动选择最佳优化策略

### 1.2 优化流程

ADDP 采用**三阶段渐进式优化**策略，按以下顺序执行：

```
生成瓦片 → 检查大小
    ↓
【Step 1】Extent 优化（降低瓦片分辨率）
    - 最快速的优化手段
    - 降低 Extent：4096 → 2048 或 1024
    - 对视觉影响较小（低 zoom 级别）
    ↓
检查大小 < 2MB？Yes → 返回 | No → 继续
    ↓
【Step 2】对象采样（保留重要对象）
    - 保留大地块、主干道等视觉主体
    - 面/线：按面积/长度排序，保留 80% 或 60% 对象
    - 点：随机保留 60%
    ↓
检查大小 < 2MB？Yes → 返回 | 检查 < 5MB？Yes → 跳过简化 | No → 继续
    ↓
【Step 3】几何简化（简化几何形状）
    - 计算开销最大，作为最终兜底
    - 去除重复点 + 几何简化
    - 使用 Visvalingam 或 Douglas-Peucker 算法
    ↓
返回结果
```

**核心原则**：
- 每步检查瓦片大小，满足条件则停止，避免过度优化
- 优先使用低开销、快速的优化手段
- 保证最终输出瓦片可用（即使超过阈值）

---

## 二、配置参数说明

### 2.1 基础配置

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
  - **z0-z8**：仅返回主键（id），减少数据量
  - **z9+**：返回全部属性，便于查看详细信息

**说明**：低 zoom 级别时用户通常查看整体概况，不需要详细属性；高 zoom 级别时用户需要查看具体信息。

---

### 2.2 瓦片大小阈值

#### 不优化阈值

- **no_optimization_mb**：小于此值不进行任何优化（默认 `2.0` MB）
  - 瓦片小于此值时直接返回
  - 避免不必要的优化开销

#### 停止优化阈值

- **stop_optimization_mb**：达到此值后跳过几何优化（默认 `5.0` MB）
  - 瓦片在 2MB-5MB 之间时，完成前两步优化即可
  - 跳过计算开销大的几何简化
  - 平衡性能和质量

---

### 2.3 优化参数（高级）

#### 1. Extent 优化（瓦片分辨率）

**模糊度**（blur_level）：

| 模糊度 | Extent 值 | 说明 | 推荐场景 |
|--------|----------|------|---------|
| **清晰（1）** | 4096 | 边界精细，数据量大 | 数据量小，追求高质量 |
| **适中（2）** | 2048 | 边界较精细，数据量减半 | **推荐**，平衡质量和大小 |
| **模糊（4）** | 1024 | 边界粗糙，数据量减少 75% | 数据量极大，优先速度 |

**说明**：
- Extent 决定 MVT 瓦片的内部分辨率
- 模糊度越高，瓦片越小，但边界越不精细
- 在低 zoom 级别（z0-z5），适中或模糊效果可接受

**计算公式**：
```
Extent = 4096 / blur_level
```

---

#### 2. 对象采样

对象采样在数据密集区域优先保留视觉上重要的要素，过滤掉小对象。

**面/线要素保留策略**：

- **cumulative_size_ratio**：面积/长度占比（范围 0.5-1.0，默认 `0.8`）
  - 保留累计占总面积/长度 80% 的对象
  - 优先保留大地块、主干道
  - 示例：100 个地块，排序后前 20 个占总面积 80%，则只保留这 20 个

- **max_feature_count_ratio**：对象数量占比（范围 0.3-1.0，默认 `0.6`）
  - 最多保留 60% 的对象数量
  - 防止小对象过多导致数据量仍然很大
  - 示例：100 个对象，最多保留 60 个

**采样逻辑**：满足以下**任一条件**的对象将被保留：
- 累计面积/长度 ≤ 总量 × `cumulative_size_ratio`
- 对象数量 ≤ 总数 × `max_feature_count_ratio`

**点要素保留策略**：

- **sample_ratio**：点保留比例（范围 0.3-1.0，默认 `0.6`）
  - 随机保留 60% 的点对象
  - 基于主键哈希的确定性采样（同一点在相同 zoom 下始终显示或不显示）
  - 空间分布均匀

**说明**：对象采样在低 zoom 级别（z0-z5）自动启用，优先保留视觉主体。

---

#### 3. 几何简化

几何简化通过去除冗余节点来减少几何复杂度。

**简化倍数**（tolerance_multiplier）：

| 简化倍数 | 说明 | 节点减少预估 | 推荐场景 |
|---------|------|-------------|---------|
| **2倍简化** | 容错度翻倍，边界较精细 | 20-40% | 追求高质量 |
| **4倍简化** | 容错度 4 倍，效果好 | 40-70% | **推荐**，平衡质量和大小 |
| **8倍简化** | 容错度 8 倍，边界平滑但细节少 | 70-90% | 数据量极大，优先速度 |

**简化算法**（algorithm）：

| 算法 | 说明 | 特点 | 推荐场景 |
|------|------|------|---------|
| **Visvalingam** | 基于三角形面积，保留重要拐点 | 视觉效果好，可能产生自相交 | **推荐**，适合预览 |
| **Douglas-Peucker** | 传统算法，保证拓扑正确 | 保守，不产生自相交 | 追求拓扑正确性 |

**简化流程**：
1. **去除重复点**：移除距离小于 tolerance × 0.1 的连续重复点
2. **几何简化**：使用选定算法简化几何
3. ~~**网格对齐**~~：（可选，当前未启用）将坐标对齐到网格

**容错度计算公式**：
```go
tolerance = 0.0001 × multiplier^(10-z)
```

**示例**（multiplier = 4.0）：
- z0: 0.4096 (~45km)
- z3: 0.0064 (~711m)
- z5: 0.0004 (~44m)
- z9: 0.00002 (~2.2m)

**说明**：几何简化在低 zoom 级别（z0-z9）自动启用，减少节点数量。

---

## 三、配置示例

### 3.1 默认配置（推荐）

```json
{
  "optimization_config": {
    "version": "2.0",
    "attribute_pruning": {
      "enabled": true,
      "zoom_threshold": 8
    },
    "tile_size_thresholds": {
      "no_optimization_mb": 2.0,
      "stop_optimization_mb": 5.0
    },
    "extent_optimization": {
      "blur_level": 2
    },
    "sampling": {
      "polygon_line": {
        "cumulative_size_ratio": 0.8,
        "max_feature_count_ratio": 0.6
      },
      "point": {
        "sample_ratio": 0.6
      }
    },
    "simplification": {
      "tolerance_multiplier": 4.0,
      "algorithm": "visvalingam"
    }
  }
}
```

**适用场景**：
- 一般数据集（1万 - 100万条记录）
- 平衡质量和性能
- 大多数情况下推荐使用

**预期效果**：
- z0-z3 瓦片大小：< 2MB
- z4-z8 瓦片大小：< 2MB
- 视觉效果：保留主要要素，边界平滑

---

### 3.2 激进优化配置

```json
{
  "optimization_config": {
    "version": "2.0",
    "attribute_pruning": {
      "enabled": true,
      "zoom_threshold": 10
    },
    "tile_size_thresholds": {
      "no_optimization_mb": 1.0,
      "stop_optimization_mb": 3.0
    },
    "extent_optimization": {
      "blur_level": 4
    },
    "sampling": {
      "polygon_line": {
        "cumulative_size_ratio": 0.7,
        "max_feature_count_ratio": 0.5
      },
      "point": {
        "sample_ratio": 0.5
      }
    },
    "simplification": {
      "tolerance_multiplier": 8.0,
      "algorithm": "visvalingam"
    }
  }
}
```

**适用场景**：
- 超大数据集（> 100万条记录）
- 网络带宽受限
- 优先速度，可接受质量损失

**预期效果**：
- z0-z3 瓦片大小：< 1MB
- z4-z8 瓦片大小：< 1MB
- 视觉效果：保留主要轮廓，细节较少

---

### 3.3 保守优化配置

```json
{
  "optimization_config": {
    "version": "2.0",
    "attribute_pruning": {
      "enabled": true,
      "zoom_threshold": 6
    },
    "tile_size_thresholds": {
      "no_optimization_mb": 3.0,
      "stop_optimization_mb": 8.0
    },
    "extent_optimization": {
      "blur_level": 1
    },
    "sampling": {
      "polygon_line": {
        "cumulative_size_ratio": 0.9,
        "max_feature_count_ratio": 0.8
      },
      "point": {
        "sample_ratio": 0.8
      }
    },
    "simplification": {
      "tolerance_multiplier": 2.0,
      "algorithm": "douglas_peucker"
    }
  }
}
```

**适用场景**：
- 小数据集（< 1万条记录）
- 追求高质量
- 网络带宽充足

**预期效果**：
- z0-z3 瓦片大小：< 3MB
- z4-z8 瓦片大小：< 3MB
- 视觉效果：保留大部分要素和细节

---

## 四、常见问题

### Q1: 瓦片太大，如何优化？

**症状**：
- 前端加载缓慢
- 浏览器卡顿或崩溃
- 瓦片大小超过 5MB

**解决方案**：按以下顺序调整参数：

1. **降低模糊度**：`blur_level: 2 → 4`
2. **降低面积占比**：`cumulative_size_ratio: 0.8 → 0.7`
3. **降低对象占比**：`max_feature_count_ratio: 0.6 → 0.5`
4. **提高简化倍数**：`tolerance_multiplier: 4 → 8`
5. **降低阈值**：`no_optimization_mb: 2.0 → 1.0`

**示例**：切换到"激进优化配置"（见 3.2）

---

### Q2: 瓦片太小，视觉效果不好？

**症状**：
- 地图显示空洞或缺失要素
- 边界过于粗糙
- 重要地物未显示

**解决方案**：按以下顺序调整参数：

1. **提高模糊度**：`blur_level: 2 → 1`
2. **提高面积占比**：`cumulative_size_ratio: 0.8 → 0.9`
3. **提高对象占比**：`max_feature_count_ratio: 0.6 → 0.8`
4. **降低简化倍数**：`tolerance_multiplier: 4 → 2`
5. **提高阈值**：`no_optimization_mb: 2.0 → 3.0`

**示例**：切换到"保守优化配置"（见 3.3）

---

### Q3: 如何平衡瓦片大小和视觉效果？

**A**: 使用默认配置作为起点，根据实际效果微调：

**评估指标**：
- **瓦片大小目标**：< 2MB（不优化阈值）
- **视觉效果目标**：保留主要要素，边界平滑

**调优流程**：
1. 使用默认配置生成瓦片
2. 在前端查看 z0-z5 的视觉效果
3. 检查瓦片大小统计
4. 根据 Q1/Q2 调整参数
5. 重新生成并验证

**工具**：
- 使用"预缓存统计"查看各 zoom 层级的平均瓦片大小
- 使用浏览器开发者工具查看网络传输大小

---

### Q4: 不同数据类型如何配置？

**A**: 根据数据类型特点调整配置：

**点数据**（POI、传感器）：
- 主要调整 `point.sample_ratio`
- 建议：0.6-0.8（保留更多点）
- Extent 优化影响小，可使用 `blur_level: 2`

**线数据**（道路、河流）：
- 主要调整 `cumulative_size_ratio`（长度占比）
- 建议：0.7-0.8（保留主干）
- 简化倍数可适当提高：4-8

**面数据**（地块、行政区划）：
- 主要调整 `cumulative_size_ratio`（面积占比）
- 建议：0.8-0.9（保留大地块）
- 简化倍数：2-4（保持边界质量）

---

### Q5: PostgreSQL 内存不足导致瓦片生成失败？

**症状**：
- 瓦片生成速度极慢（例如：17 分钟只生成 7 个瓦片）
- 后台日志出现 `ERROR: could not resize shared memory segment` 错误
- 单个瓦片查询耗时超过 10 秒
- 预缓存进度停滞不前

**典型错误日志**：
```
ERROR: could not resize shared memory segment "/PostgreSQL.3377925630" to 12615680 bytes: No space left on device (SQLSTATE 53100)
query_duration_ms: 13474
```

**根本原因**：

MVT 瓦片生成涉及复杂的空间查询操作（ST_AsMVT、ST_Transform、ST_Intersects、ST_SimplifyVW 等），这些操作需要较大的内存空间来处理几何数据。当 PostgreSQL 的 `work_mem` 配置过小时，查询将失败或极其缓慢。

在上述案例中，查询需要 12.6 MB 内存，但 `work_mem` 只配置了 4 MB，导致每个瓦片查询失败，耗时 13+ 秒。

**解决方案**：

需要为业务数据库（`business/docker-compose.yml`）优化 PostgreSQL 内存配置：

1. **修改 `business/docker-compose.yml` 文件**：

```yaml
  postgres:
    image: ${POSTGRES_IMAGE:-imresamu/postgis-arm64:15-3.4}
    container_name: business-postgres
    # 优化 PostgreSQL 内存配置，支持 MVT 瓦片生成
    command:
      - postgres
      - -c
      - shared_buffers=512MB
      - -c
      - work_mem=64MB
      - -c
      - maintenance_work_mem=256MB
      - -c
      - max_connections=100
      - -c
      - effective_cache_size=2GB
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-business}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-business_password}
      POSTGRES_DB: ${POSTGRES_DB:-business}
```

**关键参数说明**：

| 参数 | 建议值 | 说明 |
|-----|-------|------|
| `work_mem` | **64MB** | **最关键**！每个查询操作可用的内存。MVT 查询通常需要 10-30 MB |
| `shared_buffers` | 512MB | 共享缓冲区，缓存数据库页面 |
| `maintenance_work_mem` | 256MB | 维护操作（CREATE INDEX、VACUUM）使用的内存 |
| `effective_cache_size` | 2GB | 操作系统缓存提示，帮助查询规划器优化 |

2. **重启 PostgreSQL 容器**：

```bash
cd business
docker-compose stop postgres
docker-compose rm -f postgres
docker-compose up -d postgres
```

注意：必须完全重建容器，`docker-compose restart` 不会应用新的 command 参数。

3. **验证配置是否生效**：

```bash
docker exec business-postgres psql -U business -d business -c "SHOW work_mem; SHOW shared_buffers; SHOW maintenance_work_mem;"
```

预期输出：
```
 work_mem
----------
 64MB
(1 row)

 shared_buffers
----------------
 512MB
(1 row)

 maintenance_work_mem
----------------------
 256MB
(1 row)
```

**如何诊断内存问题**：

1. **检查 Worker 日志**：
```bash
tail -f logs/manager-worker.log
```

查找包含以下关键字的错误：
- `could not resize shared memory segment`
- `No space left on device`
- `query_duration_ms` 超过 10000 毫秒

2. **检查瓦片生成性能**：

正常情况下，单个瓦片生成时间应该在：
- 无优化：100-500 ms
- Extent 优化：200-800 ms
- 采样优化：500-2000 ms
- 简化优化：1000-5000 ms

如果单个瓦片超过 10 秒，极可能是内存不足。

3. **查看当前 PostgreSQL 内存配置**：
```bash
docker exec business-postgres psql -U business -d business -c "SHOW work_mem;"
```

如果返回值小于 64MB，建议按上述方案优化。

**性能对比**：

| 场景 | work_mem | 单瓦片耗时 | 1778 瓦片预计总时长 |
|-----|---------|----------|-----------------|
| **优化前** | 4MB | 13+ 秒（失败） | > 6 小时 |
| **优化后** | 64MB | 1-3 秒 | 30 分钟 - 1.5 小时 |

**重要提示**：

- 此配置修改仅影响 **业务数据库**（`business/docker-compose.yml`），不影响 ADDP 系统数据库（`docker-compose.infra.yml`）
- 如果服务器内存有限，可适当降低 `shared_buffers` 和 `effective_cache_size`，但 **`work_mem` 必须至少 64MB**
- 对于超大数据集（> 500 万条记录），可能需要进一步提高 `work_mem` 到 128MB

**macOS 特殊优化**：

如果在 macOS 上遇到 "could not resize shared memory segment" 错误（即使 work_mem 已设置为 64MB），建议使用以下更保守的配置：

```yaml
command:
  - postgres
  - -c
  - shared_buffers=256MB      # 降低共享缓冲（macOS 限制）
  - -c
  - work_mem=64MB             # 单个查询操作内存
  - -c
  - maintenance_work_mem=128MB
  - -c
  - max_connections=50         # 限制并发连接
  - -c
  - effective_cache_size=1GB
```

这个配置在 macOS 上的共享内存限制下表现最好，避免了系统级别的内存段分配失败。

**性能预期**（基于实际测试）：

以 1000+ 万条大型空间数据为例，zoom 11 高精度瓦片生成：

| 数据密度 | 瓦片大小 | 单瓦片耗时 | 备注 |
|---------|---------|---------|------|
| 低密度 | 0.4 MB | 3-4 秒 | 数据少，无优化 |
| 中密度 | 0.5-1.2 MB | 6-10 秒 | 正常数据分布 |
| 高密度 | 1.2-2.0 MB | 15-20 秒 | 需要优化 |
| 超高密度 | > 2.0 MB | 30-40 秒 | 需要 Extent 优化 |

**1778 个瓦片预计完成时间**：
- 平均速度：12 秒/瓦片
- 预计总时长：4-6 小时（视数据分布而定）

---

## 五、技术细节

### 5.1 Extent 计算公式

```go
func getExtentFromBlurLevel(blurLevel int) int {
    baseExtent := 4096
    return baseExtent / blurLevel
}
```

**说明**：
- MVT 标准 Extent 为 4096
- 降低 Extent 相当于降低内部分辨率
- Extent = 1024 时，坐标精度降低到 1/4

---

### 5.2 SimplifyTolerance 计算公式

```go
func SimplifyToleranceWithMultiplier(z int, multiplier float64) float64 {
    base := 0.0001  // ~11m at equator in degrees
    pow := 10 - z
    tol := base
    for i := 0; i < pow; i++ {
        tol *= multiplier
    }
    return tol
}
```

**说明**：
- base 是基础容错度（~11m @ 赤道）
- zoom 越低，容错度指数增长
- multiplier 控制增长速度

**示例**（multiplier = 4.0）：

| Zoom | tolerance | 实际距离（赤道） |
|------|----------|---------------|
| 0 | 0.4096 | ~45km |
| 3 | 0.0064 | ~711m |
| 5 | 0.0004 | ~44m |
| 9 | 0.00002 | ~2.2m |

---

### 5.3 对象采样 SQL（面/线）

```sql
WITH ranked_features AS (
  SELECT
    t.*,
    CASE
      WHEN ST_GeometryType(t.geom) IN ('ST_Polygon', 'ST_MultiPolygon')
        THEN ST_Area(t.geom)
      WHEN ST_GeometryType(t.geom) IN ('ST_LineString', 'ST_MultiLineString')
        THEN ST_Length(t.geom)
      ELSE 0
    END AS size_metric,
    SUM(...) OVER (ORDER BY size_metric DESC) AS cumulative_size,
    ROW_NUMBER() OVER (ORDER BY size_metric DESC) AS row_num,
    COUNT(*) OVER () AS total_count
  FROM schema.table AS t, b
  WHERE t.geom && b.g_src
    AND ST_Intersects(t.geom, b.g_src)
),
total_stats AS (
  SELECT
    SUM(size_metric) AS total_size,
    MAX(total_count) AS total_count
  FROM ranked_features
)
SELECT ... FROM ranked_features rf, total_stats ts
WHERE
  rf.cumulative_size <= ts.total_size * 0.8
  OR
  rf.row_num <= ts.total_count * 0.6
```

**核心逻辑**：
1. 计算每个要素的面积/长度（size_metric）
2. 按 size_metric 降序排序
3. 计算累计面积/长度（cumulative_size）
4. 保留满足条件的要素

---

### 5.4 对象采样 SQL（点）

```sql
WHERE ...
  AND (
    ST_GeometryType(t.geom) NOT IN ('ST_Point', 'ST_MultiPoint')
    OR
    (hashtext(t.primary_key::text)::bigint % 100) < 60
  )
```

**核心逻辑**：
- 基于主键哈希值的模运算
- 确定性采样（同一点的哈希值固定）
- 60% 采样率 = 哈希值 % 100 < 60

---

### 5.5 几何简化 SQL

```sql
WITH cleaned AS (
  SELECT ST_RemoveRepeatedPoints(t.geom, tolerance * 0.1) AS clean_geom
  FROM schema.table AS t
),
simplified AS (
  SELECT ST_SimplifyVW(c.clean_geom, tolerance) AS simplified_geom
  FROM cleaned c
)
SELECT
  ST_AsMVTGeom(
    ST_Transform(s.simplified_geom, 3857),
    b.g3857,
    extent, buffer, true
  ) AS geom
FROM simplified s, b
WHERE s.simplified_geom && b.g_src
  AND ST_Intersects(s.simplified_geom, b.g_src)
  AND NOT ST_IsEmpty(s.simplified_geom)
```

**核心逻辑**：
1. **cleaned**：去除重复点（tolerance × 0.1）
2. **simplified**：几何简化（ST_SimplifyVW 或 ST_SimplifyPreserveTopology）
3. **转换**：转换为 MVT 几何

---

## 六、更新日志

### v2.0 (2025-01-21)
- 调整优化顺序：Extent → 采样 → 几何
- 点采样默认改为 60%（原 80%）
- 参数命名用户友好化：模糊度、简化倍数
- 所有参数可配置
- 增加详细配置说明文档

### v1.0 (2024-XX-XX)
- 初版实现
- 基础 MVT 瓦片生成
- 简单的几何简化

---

## 七、相关文档

- [MVT 实现说明](./MVT_IMPLEMENTATION.md)
- [MVT 优化方案](./MVT_OPTIMIZATION_PLAN.md)
- [预缓存说明](./PRE_CACHE.md)

---

## 八、联系与支持

如有问题或建议，请联系开发团队或提交 Issue。
