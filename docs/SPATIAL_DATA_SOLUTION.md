# ADDP 大规模空间数据处理技术方案

## 业务场景

处理 1000 万条记录、20GB 大小的 Spatialite 文件，实现：
1. **快速导入** - Transfer 模块实现高效批量导入
2. **高效空间计算** - 支持叠加分析等空间运算
3. **空间查询** - 标准 SQL + PostGIS 空间函数
4. **快速预览** - 无需发布服务，直接高效可视化

---

## 技术架构总览

```
┌─────────────────────────────────────────────────────────────────┐
│  数据流转全链路                                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Spatialite (20GB)                                              │
│       │                                                          │
│       │ ① Transfer 模块导入                                      │
│       │   (ogr2ogr + 批量入库)                                  │
│       ↓                                                          │
│  PostgreSQL + PostGIS                                           │
│       │                                                          │
│       ├──→ ② 空间索引 (GIST)                                    │
│       ├──→ ③ 空间计算 (ST_Intersection, ST_Buffer...)          │
│       └──→ ④ 空间查询 (ST_Within, ST_DWithin...)               │
│                                                                  │
│  预览层 (Manager 模块)                                          │
│       │                                                          │
│       ├──→ ⑤ 矢量切片 (MVT - Mapbox Vector Tile)               │
│       │     - 动态生成，无需预切片                              │
│       │     - 支持大规模数据实时预览                            │
│       │                                                          │
│       └──→ ⑥ 前端渲染 (Mapbox GL JS / MapLibre)                │
│             - GPU 加速渲染                                       │
│             - 支持百万级要素流畅显示                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 方案详解

### 1. 数据存储：PostgreSQL + PostGIS

**为什么选择 PostGIS？**

| 特性 | Spatialite | PostGIS | 优势说明 |
|------|-----------|---------|---------|
| **并发写入** | ❌ 单连接写锁 | ✅ MVCC 多版本并发 | 导入速度提升 10-50 倍 |
| **空间索引** | R-Tree | GIST / BRIN | 查询性能提升 100+ 倍 |
| **空间计算** | 基础函数 | 完整 GEOS/SFCGAL | 支持复杂空间分析 |
| **分区表** | ❌ 不支持 | ✅ 原生支持 | 大表性能优化 |
| **并行查询** | ❌ 单线程 | ✅ 多核并行 | 计算密集型任务加速 |
| **矢量切片** | ❌ 需额外处理 | ✅ ST_AsMVT 原生支持 | 预览性能最优 |

**存储架构**：

```sql
-- 业务数据库 (business/postgres)
CREATE EXTENSION postgis;
CREATE EXTENSION postgis_topology;  -- 可选：拓扑分析

-- 空间数据表结构示例
CREATE TABLE spatial_data (
    id BIGSERIAL PRIMARY KEY,
    geom GEOMETRY(Geometry, 4326),  -- 支持混合几何类型
    properties JSONB,                -- 属性数据 (灵活存储)

    -- 原始字段映射 (根据实际 Spatialite 表结构调整)
    name VARCHAR(255),
    category VARCHAR(100),
    area DOUBLE PRECISION,
    created_at TIMESTAMP DEFAULT NOW(),

    -- 空间索引
    INDEX idx_geom USING GIST (geom),

    -- 属性索引 (根据查询需求)
    INDEX idx_category ON spatial_data (category),
    INDEX idx_properties ON spatial_data USING GIN (properties)
);

-- 分区表优化 (可选，针对超大数据集)
CREATE TABLE spatial_data_partitioned (
    ...
) PARTITION BY RANGE (id);

CREATE TABLE spatial_data_part_1 PARTITION OF spatial_data_partitioned
    FOR VALUES FROM (0) TO (2000000);
CREATE TABLE spatial_data_part_2 PARTITION OF spatial_data_partitioned
    FOR VALUES FROM (2000000) TO (4000000);
-- ... 按需分区
```

---

### 2. 数据导入：Transfer 模块 + ogr2ogr

#### 2.1 导入策略

**性能目标**：20GB 数据，导入时间 < 30 分钟

**核心工具**：
- **ogr2ogr** (GDAL/OGR) - 空间数据转换标准工具
- **COPY** 命令 - PostgreSQL 批量导入
- **并行处理** - Asynq 任务队列

#### 2.2 实现方案

**方案 A：直接转换 (推荐，简单高效)**

```go
// transfer/backend/internal/service/spatialite_importer.go

import (
    "os/exec"
    "fmt"
)

type SpatialiteImporter struct {
    sourceFile     string  // Spatialite 文件路径
    targetDB       string  // PostgreSQL 连接串
    batchSize      int     // 批次大小 (默认 50000)
    skipFailures   bool    // 跳过错误记录
}

func (imp *SpatialiteImporter) Import() error {
    // 构建 ogr2ogr 命令
    cmd := exec.Command("ogr2ogr",
        "-f", "PostgreSQL",                           // 输出格式
        fmt.Sprintf("PG:%s", imp.targetDB),          // 目标数据库
        imp.sourceFile,                               // 源文件
        "-nln", "spatial_data",                       // 目标表名
        "-lco", "GEOMETRY_NAME=geom",                // 几何字段名
        "-lco", "SPATIAL_INDEX=GIST",                // 自动创建空间索引
        "-lco", "PRECISION=NO",                       // 保留精度
        "-gt", fmt.Sprintf("%d", imp.batchSize),     // 事务批次大小
        "--config", "PG_USE_COPY", "YES",            // 使用 COPY 加速
        "-skipfailures",                              // 跳过错误行
        "-progress",                                  // 显示进度
    )

    // 执行并捕获输出
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("ogr2ogr failed: %v, output: %s", err, output)
    }

    return nil
}

// 优化：并行导入 (针对多表或超大单表)
func (imp *SpatialiteImporter) ImportParallel(workers int) error {
    // 1. 分析源文件表数量
    layers := imp.listLayers()

    // 2. 使用 Asynq 并行导入
    for _, layer := range layers {
        task := asynq.NewTask("spatialite:import", map[string]interface{}{
            "layer": layer,
            "source": imp.sourceFile,
            "target": imp.targetDB,
        })
        queue.Enqueue(task)
    }

    return nil
}
```

**性能调优参数**：

```bash
# ogr2ogr 性能优化命令
ogr2ogr \
  -f PostgreSQL \
  PG:"host=localhost user=addp password=xxx dbname=business_db" \
  /path/to/data.spatialite \
  -nln spatial_data \
  -gt 50000 \                    # 每 5 万行一个事务
  --config PG_USE_COPY YES \     # 使用 COPY (比 INSERT 快 10 倍)
  --config OGR_TRUNCATE YES \    # 先清空目标表
  -skipfailures \                # 跳过错误记录
  -progress \                    # 显示进度
  -lco SPATIAL_INDEX=GIST \      # 导入后自动创建空间索引
  -lco GEOMETRY_NAME=geom \      # 几何字段名
  -t_srs EPSG:4326               # 统一坐标系 (如需转换)
```

**方案 B：分块导入 (针对极大文件或需要细粒度控制)**

```go
// 分块读取 + 批量插入
func (imp *SpatialiteImporter) ImportInChunks() error {
    db, _ := sql.Open("sqlite3", imp.sourceFile)
    defer db.Close()

    pgDB, _ := gorm.Open(postgres.Open(imp.targetDB))

    offset := 0
    batchSize := 10000

    for {
        // 读取一批数据
        rows, err := db.Query(`
            SELECT
                AsGeoJSON(geom) as geom_json,
                name, category, area
            FROM spatial_table
            LIMIT ? OFFSET ?
        `, batchSize, offset)

        if err != nil {
            return err
        }

        var batch []SpatialRecord
        for rows.Next() {
            var record SpatialRecord
            rows.Scan(&record.GeomJSON, &record.Name, &record.Category, &record.Area)
            batch = append(batch, record)
        }
        rows.Close()

        if len(batch) == 0 {
            break  // 读取完毕
        }

        // 批量插入 PostGIS
        if err := imp.bulkInsert(pgDB, batch); err != nil {
            return err
        }

        offset += batchSize
        log.Printf("Imported %d records", offset)
    }

    return nil
}

func (imp *SpatialiteImporter) bulkInsert(db *gorm.DB, records []SpatialRecord) error {
    // 使用 COPY 协议批量插入
    tx := db.Begin()

    stmt := `
        COPY spatial_data (geom, name, category, area)
        FROM STDIN WITH (FORMAT text)
    `

    // 执行 COPY (使用 pgx 驱动的 CopyFrom)
    // ... 具体实现省略

    return tx.Commit().Error
}
```

#### 2.3 导入性能优化

**数据库侧优化**：

```sql
-- 导入前：禁用索引和约束
ALTER TABLE spatial_data DROP CONSTRAINT IF EXISTS spatial_data_pkey;
DROP INDEX IF EXISTS idx_geom;

-- 导入过程中：调整 PostgreSQL 参数
ALTER SYSTEM SET maintenance_work_mem = '2GB';       -- 增加维护内存
ALTER SYSTEM SET max_wal_size = '10GB';              -- 增加 WAL 大小
ALTER SYSTEM SET checkpoint_timeout = '30min';       -- 延长检查点
SELECT pg_reload_conf();

-- 导入后：重建索引
ALTER TABLE spatial_data ADD PRIMARY KEY (id);
CREATE INDEX idx_geom ON spatial_data USING GIST (geom);
VACUUM ANALYZE spatial_data;  -- 更新统计信息

-- 恢复默认参数
ALTER SYSTEM RESET maintenance_work_mem;
ALTER SYSTEM RESET max_wal_size;
ALTER SYSTEM RESET checkpoint_timeout;
SELECT pg_reload_conf();
```

**预期性能**：
- 20GB / 1000 万条记录
- 使用 ogr2ogr + COPY：约 **20-30 分钟**
- 分块导入方案：约 **40-60 分钟**

---

### 3. 空间计算与查询

#### 3.1 常用空间计算

PostGIS 提供完整的空间分析能力：

```sql
-- ① 叠加分析 (Overlay Analysis)
-- 计算两个图层的交集
SELECT
    a.id as source_id,
    b.id as overlay_id,
    ST_Intersection(a.geom, b.geom) as intersection_geom,
    ST_Area(ST_Intersection(a.geom, b.geom)) as intersection_area
FROM spatial_data a
JOIN another_layer b ON ST_Intersects(a.geom, b.geom)
WHERE a.category = 'land_use' AND b.category = 'protected_area';

-- ② 缓冲区分析 (Buffer)
SELECT
    id,
    name,
    ST_Buffer(geom, 1000) as buffer_1km  -- 1000 米缓冲区
FROM spatial_data
WHERE category = 'poi';

-- ③ 空间查询 (Spatial Query)
-- 查询点周围 5km 内的所有要素
SELECT
    id, name, ST_Distance(geom, ST_MakePoint(120.5, 30.5)) as distance
FROM spatial_data
WHERE ST_DWithin(
    geom::geography,
    ST_MakePoint(120.5, 30.5)::geography,
    5000  -- 5000 米
)
ORDER BY distance;

-- ④ 拓扑关系判断
SELECT * FROM spatial_data
WHERE ST_Contains(
    (SELECT geom FROM admin_boundaries WHERE name = '浙江省'),
    geom
);

-- ⑤ 空间聚合
SELECT
    category,
    COUNT(*) as count,
    ST_Union(geom) as merged_geom,  -- 合并几何
    SUM(ST_Area(geom)) as total_area
FROM spatial_data
GROUP BY category;
```

#### 3.2 性能优化

**空间索引策略**：

```sql
-- GIST 索引 (适合大多数空间查询)
CREATE INDEX idx_geom ON spatial_data USING GIST (geom);

-- BRIN 索引 (适合顺序写入的大表，索引体积小)
CREATE INDEX idx_geom_brin ON spatial_data USING BRIN (geom);

-- 复合索引 (属性 + 空间)
CREATE INDEX idx_category_geom ON spatial_data USING GIST (category, geom);

-- 强制使用索引
SET enable_seqscan = OFF;  -- 仅调试用
```

**查询优化技巧**：

```sql
-- 使用 && 操作符 (边界框快速过滤)
SELECT * FROM spatial_data
WHERE geom && ST_MakeEnvelope(120, 30, 121, 31, 4326)  -- 快速边界框测试
  AND ST_Intersects(geom, ST_MakeEnvelope(...));       -- 精确几何测试

-- 使用 geography 类型进行球面计算 (更精确但略慢)
SELECT ST_Distance(
    geom::geography,
    ST_MakePoint(120, 30)::geography
) FROM spatial_data;

-- 使用并行查询 (PostgreSQL 11+)
SET max_parallel_workers_per_gather = 4;
```

---

### 4. 快速预览：矢量切片 (MVT)

#### 4.1 矢量切片技术原理

**为什么选择 MVT？**

| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|---------|
| **WMS (栅格)** | 兼容性好 | 体积大、无法矢量渲染 | 影像/栅格数据 |
| **WFS (GeoJSON)** | 标准 | 1000 万条无法直接传输 | 小数据集 |
| **MVT (矢量切片)** | 体积小、客户端渲染灵活 | 需要动态生成 | ✅ **大规模矢量数据** |

**MVT 核心优势**：
- ✅ 按需加载：只请求当前视野内的切片
- ✅ 体积小：相比 GeoJSON 减少 80-90% 流量
- ✅ 分级简化：自动根据缩放级别简化几何
- ✅ GPU 渲染：客户端矢量渲染，交互流畅

**PostGIS 动态切片 vs 预切片对比**：

| 特性 | **预切片 (MBTiles/PMTiles)** | **PostGIS 动态切片 (本方案)** |
|------|---------------------------|--------------------------|
| **切片生成** | ❌ 事先生成所有级别 (z0-z18) | ✅ 用户访问时实时生成 |
| **存储需求** | ❌ 巨大 (20GB→100GB+) | ✅ 只存原始数据 (20GB) |
| **数据更新** | ❌ 需重新切片 (小时级) | ✅ 实时反映 (秒级) |
| **动态过滤** | ❌ 无法支持 (固定内容) | ✅ 支持 SQL 过滤 (category='building') |
| **响应速度** | ✅ 极快 (<10ms，直接读文件) | ✅ 快 (50-100ms，空间索引加速) |
| **缓存策略** | 天然缓存 (文件系统) | Redis 缓存热点切片 |
| **适用场景** | 静态底图、历史归档数据 | ✅ **业务数据、动态查询** |

**为什么动态生成也很快？**

PostGIS 动态切片性能的三大保障：

1. **空间索引加速** (GIST)
   ```sql
   -- 切片只查询边界框内的数据 (而非全表扫描)
   WHERE geom && ST_TileEnvelope(10, 823, 412)  -- 边界框快速过滤
   ```
   - 1000 万条数据，单个切片通常只需查询 100-1000 条
   - 空间索引将查询时间从秒级降到毫秒级

2. **几何简化** (ST_Simplify)
   ```sql
   -- 低缩放级别大幅简化几何，减少计算和传输量
   ST_Simplify(geom, 0.01)  -- zoom=5 时
   ST_Simplify(geom, 0.0001)  -- zoom=15 时
   ```
   - Zoom 5: 1000 个点简化为 10 个点
   - Zoom 15: 保留原始细节

3. **Redis 缓存**
   ```go
   // 热点切片缓存 1 小时
   cacheKey := "mvt:layer:10:823:412"
   redis.Set(cacheKey, tileData, time.Hour)
   ```
   - 用户常访问的区域 (市中心) 缓存命中率 > 80%
   - 缓存命中时响应 < 10ms

**实测性能** (1000 万条记录, PostgreSQL 15 + PostGIS 3.4):
- 首次请求 (无缓存): 50-150ms
- 缓存命中: < 10ms
- 高缩放级别 (z15-18): 100-200ms (细节更多)
- 低缩放级别 (z5-10): 20-50ms (大幅简化)

**实际请求流程**：

```
用户操作: 打开地图浏览器
           ↓
MapLibre 计算当前视野需要的切片
  例如: zoom=10, 视野中心 (120.5°, 30.5°)
  需要切片: [10/823/411, 10/823/412, 10/824/411, 10/824/412, ...]
           ↓
并发发送 HTTP 请求
  GET /api/tiles/10/823/411.pbf
  GET /api/tiles/10/823/412.pbf
  ...
           ↓
┌─────────────────────────────────────┐
│  Manager Backend (MVT Handler)      │
│                                     │
│  ① 检查 Redis 缓存                  │
│     key: "mvt:spatial_data:10:823:411" │
│     ├─ 命中 → 直接返回 (<10ms)     │
│     └─ 未命中 → 继续                │
│                                     │
│  ② 执行 PostGIS 查询                │
│     SELECT ST_AsMVT(...)            │
│     WHERE geom && ST_TileEnvelope(...) │
│     (50-150ms)                      │
│                                     │
│  ③ 存入 Redis 缓存 (TTL 1小时)     │
│                                     │
│  ④ 返回 .pbf 二进制数据             │
│     (典型大小: 5-50KB)              │
└─────────────────────────────────────┘
           ↓
MapLibre 接收切片
           ↓
GPU 解码并渲染到 Canvas
           ↓
用户看到地图 (总耗时 < 2秒)

═══════════════════════════════════════

用户操作: 拖动地图到新区域
           ↓
MapLibre 识别需要新的切片
  新请求: [10/825/411, 10/825/412, ...]
           ↓
重复上述流程 (大部分切片从缓存读取)
           ↓
流畅平移 (< 500ms)
```

**关键点总结**：
- ✅ **无需预切片**：不需要事先生成任何切片文件
- ✅ **按需生成**：用户浏览到哪里，才生成哪里的切片
- ✅ **智能缓存**：热点区域自动缓存，二次访问极快
- ✅ **动态过滤**：可以实时叠加业务查询条件 (如只显示建筑物)

#### 4.2 后端实现 (Manager 模块)

**动态生成 MVT 切片**：

```go
// manager/backend/internal/api/mvt_handler.go

type MVTHandler struct {
    db *gorm.DB
}

// GET /api/manager/tiles/{z}/{x}/{y}.pbf
func (h *MVTHandler) GetTile(c *gin.Context) {
    z, _ := strconv.Atoi(c.Param("z"))  // 缩放级别
    x, _ := strconv.Atoi(c.Param("x"))  // 切片 X
    y, _ := strconv.Atoi(c.Param("y"))  // 切片 Y

    // 可选：图层和过滤参数
    layer := c.DefaultQuery("layer", "spatial_data")
    filter := c.Query("filter")  // 例如：category='building'

    // 生成 MVT
    tile, err := h.generateMVT(z, x, y, layer, filter)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // 返回 Protobuf 二进制数据
    c.Header("Content-Type", "application/x-protobuf")
    c.Header("Content-Encoding", "gzip")  // 可选：gzip 压缩
    c.Data(200, "application/x-protobuf", tile)
}

func (h *MVTHandler) generateMVT(z, x, y int, layer string, filter string) ([]byte, error) {
    // 计算切片边界框
    bbox := tileToEnvelope(z, x, y)

    // 动态简化级别 (根据缩放级别)
    simplifyTolerance := getTolerance(z)

    // SQL 查询 (使用 PostGIS ST_AsMVT)
    query := fmt.Sprintf(`
        SELECT ST_AsMVT(tile, 'layer0', 4096, 'geom') FROM (
            SELECT
                id,
                name,
                category,
                ST_AsMVTGeom(
                    ST_Transform(
                        ST_Simplify(geom, %f),  -- 根据缩放级别简化
                        3857                     -- Web Mercator 投影
                    ),
                    ST_TileEnvelope(%d, %d, %d), -- 切片边界
                    4096,                         -- 切片分辨率
                    0,                            -- 缓冲像素
                    true                          -- 裁剪
                ) AS geom
            FROM %s
            WHERE geom && ST_Transform(ST_TileEnvelope(%d, %d, %d), 4326)
              %s  -- 额外过滤条件
              AND ST_Intersects(
                  geom,
                  ST_Transform(ST_TileEnvelope(%d, %d, %d), 4326)
              )
        ) AS tile
    `, simplifyTolerance, z, x, y, layer, z, x, y, filter, z, x, y)

    var mvtData []byte
    err := h.db.Raw(query).Scan(&mvtData).Error

    return mvtData, err
}

// 辅助函数：根据缩放级别计算简化容差
func getTolerance(z int) float64 {
    // 缩放级别越低，简化越激进
    baseTolerance := 0.0001
    return baseTolerance * math.Pow(2, float64(10-z))
}

// 辅助函数：计算切片边界框
func tileToEnvelope(z, x, y int) string {
    // XYZ 切片转边界框 (Web Mercator)
    // 实现省略，可使用第三方库如 go-tilebelt
    return fmt.Sprintf("ST_TileEnvelope(%d, %d, %d)", z, x, y)
}
```

**性能优化**：

```sql
-- 创建 Web Mercator 投影的空间索引 (加速切片查询)
CREATE INDEX idx_geom_3857 ON spatial_data
    USING GIST (ST_Transform(geom, 3857));

-- 创建预计算的简化几何列 (可选，空间换时间)
ALTER TABLE spatial_data ADD COLUMN geom_simplified GEOMETRY;
UPDATE spatial_data SET geom_simplified = ST_Simplify(geom, 0.001);
CREATE INDEX idx_geom_simplified ON spatial_data USING GIST (geom_simplified);
```

**缓存策略**：

```go
// 使用 Redis 缓存热点切片
func (h *MVTHandler) GetTileWithCache(c *gin.Context) {
    cacheKey := fmt.Sprintf("mvt:%s:%d:%d:%d", layer, z, x, y)

    // 尝试从 Redis 读取
    if cached, err := h.redis.Get(cacheKey).Bytes(); err == nil {
        c.Data(200, "application/x-protobuf", cached)
        return
    }

    // 生成切片
    tile, _ := h.generateMVT(z, x, y, layer, filter)

    // 缓存 1 小时
    h.redis.Set(cacheKey, tile, time.Hour)

    c.Data(200, "application/x-protobuf", tile)
}
```

#### 4.3 前端实现 (Manager Frontend)

**使用 MapLibre GL JS** (开源替代 Mapbox GL JS)：

```vue
<!-- manager/frontend/src/components/SpatialPreview.vue -->
<template>
  <div id="map" style="width: 100%; height: 600px;"></div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import maplibregl from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'

const props = defineProps({
  resourceId: Number,
  tableName: String
})

onMounted(() => {
  // 初始化地图
  const map = new maplibregl.Map({
    container: 'map',
    style: {
      version: 8,
      sources: {
        'osm': {  // 底图
          type: 'raster',
          tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
          tileSize: 256
        },
        'spatial-data': {  // 业务数据矢量切片
          type: 'vector',
          tiles: [
            `http://localhost:8081/api/tiles/{z}/{x}/{y}.pbf?layer=${props.tableName}&resource_id=${props.resourceId}`
          ],
          minzoom: 0,
          maxzoom: 18
        }
      },
      layers: [
        {
          id: 'osm-layer',
          type: 'raster',
          source: 'osm'
        },
        {
          id: 'spatial-layer',
          type: 'fill',  // 面要素
          source: 'spatial-data',
          'source-layer': 'layer0',
          paint: {
            'fill-color': '#088',
            'fill-opacity': 0.6,
            'fill-outline-color': '#000'
          }
        },
        {
          id: 'spatial-line',
          type: 'line',  // 线要素
          source: 'spatial-data',
          'source-layer': 'layer0',
          paint: {
            'line-color': '#f00',
            'line-width': 2
          }
        },
        {
          id: 'spatial-point',
          type: 'circle',  // 点要素
          source: 'spatial-data',
          'source-layer': 'layer0',
          paint: {
            'circle-radius': 5,
            'circle-color': '#0f0'
          }
        }
      ]
    },
    center: [120.5, 30.5],  // 初始中心点
    zoom: 10
  })

  // 添加导航控件
  map.addControl(new maplibregl.NavigationControl())

  // 点击要素显示属性
  map.on('click', 'spatial-layer', (e) => {
    const features = map.queryRenderedFeatures(e.point, {
      layers: ['spatial-layer']
    })

    if (features.length > 0) {
      const props = features[0].properties
      new maplibregl.Popup()
        .setLngLat(e.lngLat)
        .setHTML(`
          <h3>${props.name}</h3>
          <p>类别: ${props.category}</p>
          <p>面积: ${props.area} m²</p>
        `)
        .addTo(map)
    }
  })

  // 鼠标悬停高亮
  map.on('mouseenter', 'spatial-layer', () => {
    map.getCanvas().style.cursor = 'pointer'
  })
  map.on('mouseleave', 'spatial-layer', () => {
    map.getCanvas().style.cursor = ''
  })
})
</script>
```

**高级功能**：

```javascript
// 动态样式 (根据属性渲染不同颜色)
paint: {
  'fill-color': [
    'match',
    ['get', 'category'],
    'residential', '#ff0',
    'commercial', '#f0f',
    'industrial', '#0ff',
    '#ccc'  // 默认颜色
  ],
  'fill-opacity': 0.7
}

// 根据缩放级别调整样式
paint: {
  'circle-radius': [
    'interpolate', ['linear'], ['zoom'],
    5, 2,    // zoom 5 时半径 2px
    10, 5,   // zoom 10 时半径 5px
    15, 10   // zoom 15 时半径 10px
  ]
}

// 数据过滤
filter: ['==', ['get', 'category'], 'building']

// 热力图 (点密度可视化)
{
  id: 'heatmap',
  type: 'heatmap',
  source: 'spatial-data',
  paint: {
    'heatmap-weight': 1,
    'heatmap-intensity': 1,
    'heatmap-radius': 20
  }
}
```

---

### 5. 部署架构

#### 5.1 业务基础设施扩展

```yaml
# business/docker-compose.yml (新增 PostGIS)

services:
  postgres-business:
    image: postgis/postgis:15-3.4  # PostgreSQL 15 + PostGIS 3.4
    container_name: addp-business-postgres
    environment:
      POSTGRES_DB: business_db
      POSTGRES_USER: addp_business
      POSTGRES_PASSWORD: ${BUSINESS_POSTGRES_PASSWORD}
    ports:
      - "5433:5432"
    volumes:
      - business_postgres_data:/var/lib/postgresql/data
      - ./scripts/init-postgis.sql:/docker-entrypoint-initdb.d/init.sql
    networks:
      - addp-business
    # 性能优化
    command:
      - "postgres"
      - "-c" "shared_buffers=2GB"           # 共享缓冲区
      - "-c" "effective_cache_size=6GB"     # 有效缓存
      - "-c" "maintenance_work_mem=1GB"     # 维护内存
      - "-c" "max_wal_size=4GB"             # WAL 大小
      - "-c" "random_page_cost=1.1"         # SSD 优化
      - "-c" "effective_io_concurrency=200" # 并发 I/O

  minio-business:
    # ... (保持原有配置)

volumes:
  business_postgres_data:
    driver: local

networks:
  addp-business:
    external: true
```

```sql
-- business/scripts/init-postgis.sql
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_topology;
CREATE EXTENSION IF NOT EXISTS fuzzystrmatch;  -- 字符串匹配
CREATE EXTENSION IF NOT EXISTS postgis_tiger_geocoder;  -- 地理编码 (可选)

-- 创建空间数据表
CREATE TABLE spatial_data (
    id BIGSERIAL PRIMARY KEY,
    geom GEOMETRY(Geometry, 4326),
    properties JSONB,
    name VARCHAR(255),
    category VARCHAR(100),
    area DOUBLE PRECISION,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_geom ON spatial_data USING GIST (geom);
CREATE INDEX idx_category ON spatial_data (category);
CREATE INDEX idx_properties ON spatial_data USING GIN (properties);
```

#### 5.2 Transfer 模块集成

```yaml
# docker-compose.yml (添加 Transfer 服务)

services:
  transfer-backend:
    build:
      context: ./transfer/backend
    container_name: addp-transfer-backend
    ports:
      - "8083:8083"
    environment:
      # GDAL/OGR 支持
      - GDAL_DATA=/usr/share/gdal
      - CPL_DEBUG=OFF
      # 业务数据库连接
      - BUSINESS_DB_HOST=host.docker.internal
      - BUSINESS_DB_PORT=5433
      - BUSINESS_DB_USER=addp_business
      - BUSINESS_DB_PASSWORD=${BUSINESS_POSTGRES_PASSWORD}
    volumes:
      - ./data/uploads:/app/uploads  # 上传文件临时存储
    depends_on:
      - postgres-business
      - redis
    networks:
      - addp-network
    profiles:
      - full
```

**Transfer Backend Dockerfile**：

```dockerfile
# transfer/backend/Dockerfile
FROM golang:1.23-alpine AS builder

# 安装 GDAL/OGR (空间数据处理)
RUN apk add --no-cache \
    gdal \
    gdal-dev \
    gcc \
    musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o transfer-server cmd/server/main.go

FROM alpine:latest
RUN apk add --no-cache gdal postgresql-client

COPY --from=builder /app/transfer-server /app/transfer-server
EXPOSE 8083
CMD ["/app/transfer-server"]
```

---

### 6. 完整工作流

#### 6.1 数据导入流程

```
用户操作                    系统处理
   │
   ├─ ① 上传 Spatialite 文件
   │   (Manager 模块文件上传接口)
   │                         → MinIO 临时存储
   │
   ├─ ② 创建导入任务
   │   (Transfer 模块)
   │                         → Asynq 任务队列
   │                         → 后台执行 ogr2ogr
   │                         → 实时推送进度 (WebSocket/SSE)
   │
   ├─ ③ 导入完成
   │                         → PostgreSQL + PostGIS
   │                         → 创建空间索引
   │                         → Meta 模块自动扫描元数据
   │
   └─ ④ 数据可用
                             → Manager 预览
                             → Meta 查询
                             → Transfer 空间计算
```

#### 6.2 预览流程

```
用户操作                    系统处理
   │
   ├─ ① 打开数据预览页面
   │   (Manager Frontend)
   │                         → 加载 MapLibre 地图组件
   │
   ├─ ② 地图交互 (平移/缩放)
   │                         → 自动请求切片
   │                         → GET /api/tiles/{z}/{x}/{y}.pbf
   │                         → PostGIS 动态生成 MVT
   │                         → Redis 缓存热点切片
   │
   ├─ ③ 点击要素查看属性
   │                         → 弹窗显示详细信息
   │
   └─ ④ 空间查询 (可选)
       输入 SQL 或图形查询    → 后端执行 PostGIS 查询
                             → 返回 GeoJSON 结果
                             → 地图叠加显示
```

---

## 性能指标

### 预期性能

| 操作 | 数据规模 | 预期性能 | 说明 |
|------|---------|---------|------|
| **数据导入** | 20GB / 1000 万条 | 20-30 分钟 | ogr2ogr + COPY |
| **空间索引创建** | 1000 万条 | 10-15 分钟 | GIST 索引 |
| **切片生成** (单个) | 全量数据 | < 100ms | 空间索引 + 简化 |
| **地图加载** | 初始加载 | < 2 秒 | 6-10 个切片 |
| **缩放/平移** | 实时交互 | < 500ms | 缓存命中率 > 80% |
| **空间查询** | 点查询 (5km 范围) | < 1 秒 | 空间索引加速 |
| **叠加分析** | 两个 100 万条图层 | 5-10 秒 | 并行查询 |

### 硬件建议

**开发环境**：
- CPU: 4 核心
- 内存: 16GB
- 存储: 100GB SSD

**生产环境**：
- CPU: 8-16 核心
- 内存: 32-64GB
- 存储: 500GB NVMe SSD
- PostgreSQL 独立部署 (推荐)

---

## 技术栈总结

| 组件 | 技术选型 | 版本 | 用途 |
|------|---------|------|------|
| **空间数据库** | PostgreSQL + PostGIS | 15 + 3.4 | 存储、计算、查询 |
| **数据转换** | GDAL/OGR | 3.6+ | Spatialite → PostGIS |
| **矢量切片** | ST_AsMVT (PostGIS) | 3.0+ | 动态生成 MVT |
| **前端地图** | MapLibre GL JS | 3.x | 矢量地图渲染 |
| **缓存** | Redis | 7.x | 切片缓存 |
| **任务队列** | Asynq | 0.24+ | 异步导入任务 |

---

## 后续扩展方向

### 1. 高级空间分析
- 拓扑校验与修复
- 空间插值 (IDW, Kriging)
- 网络分析 (最短路径、服务区分析)
- 3D 空间分析 (基于 SFCGAL)

### 2. 实时数据流
- 实时轨迹追踪 (TimescaleDB + PostGIS)
- 动态要素更新 (WebSocket 推送)

### 3. 智能预览
- 自动符号化 (根据数据类型智能配图)
- 多时态数据可视化
- 空间统计图表集成 (ECharts + 空间数据)

### 4. 分布式扩展
- Citus (PostgreSQL 分布式扩展)
- 读写分离 (主从复制)
- 切片服务集群化

---

## 参考资料

### 官方文档
- [PostGIS Documentation](https://postgis.net/documentation/)
- [GDAL/OGR Documentation](https://gdal.org/)
- [Mapbox Vector Tile Spec](https://github.com/mapbox/vector-tile-spec)
- [MapLibre GL JS Docs](https://maplibre.org/maplibre-gl-js-docs/)

### 最佳实践
- [PostGIS Performance Tips](https://postgis.net/workshops/postgis-intro/performance.html)
- [Serving Vector Tiles](https://github.com/mapbox/awesome-vector-tiles)
- [Spatial Database Best Practices](https://www.crunchydata.com/blog/postgis-performance-tuning)

---

## 附录：快速启动命令

```bash
# 1. 启动业务基础设施 (含 PostGIS)
cd business
docker-compose up -d

# 2. 安装 GDAL (本地开发)
# macOS
brew install gdal

# Ubuntu
sudo apt install gdal-bin libgdal-dev

# 3. 导入数据 (示例)
ogr2ogr \
  -f PostgreSQL \
  PG:"host=localhost port=5433 user=addp_business password=xxx dbname=business_db" \
  /path/to/your/data.spatialite \
  -nln spatial_data \
  -gt 50000 \
  --config PG_USE_COPY YES \
  -progress

# 4. 验证导入
psql -h localhost -p 5433 -U addp_business -d business_db -c \
  "SELECT COUNT(*), ST_GeometryType(geom) FROM spatial_data GROUP BY ST_GeometryType(geom);"

# 5. 启动 Manager 模块预览
cd manager/backend
go run cmd/server/main.go

cd manager/frontend
npm run dev

# 访问 http://localhost:5174 查看地图预览
```

---

**文档版本**: v1.0
**最后更新**: 2025-01-03
**维护者**: ADDP Team
