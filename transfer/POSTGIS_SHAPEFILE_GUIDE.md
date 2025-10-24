# PostGIS ⟷ Shapefile 传输与转换指南

## 概述

Transfer 模块现已支持 **PostgreSQL/PostGIS** 和 **Shapefile** 之间的双向数据传输和格式转换。

### ✅ 核心功能

1. **Shapefile → PostGIS** - 将 Shapefile 数据导入PostgreSQL/PostGIS
2. **PostGIS → Shapefile** - 从 PostgreSQL/PostGIS 导出为 Shapefile
3. **Shapefile → Shapefile** - Shapefile 格式转换和投影变换
4. **PostGIS → PostGIS** - PostgreSQL 空间数据迁移

### ✅ 松耦合设计

架构完全松耦合，易于扩展新的空间数据类型：

```
Reader (数据源)        Transform (转换器)        Writer (目标)
├─ JDBCReader          ├─ SpatialTransform       ├─ JDBCWriter
│  (PostGIS, MySQL)    │  (格式转换)             │  (PostGIS, MySQL)
├─ ShapefileReader     ├─ FieldMappingTransform  ├─ ShapefileWriter
├─ GeoPackageReader    ├─ FilterTransform        ├─ GeoPackageWriter
├─ GeoJSONReader       └─ CustomTransform        ├─ GeoJSONWriter
└─ ...                                            └─ ...

                        可任意组合！
```

**新增数据源只需实现接口并注册，立即支持所有现有转换器！**

---

## 支持的空间数据类型

### PostGIS 空间类型

| 类型 | 说明 | 示例 |
|------|------|------|
| `GEOMETRY` | 通用几何类型 | `GEOMETRY(POINT, 4326)` |
| `GEOGRAPHY` | 地理坐标类型 | `GEOGRAPHY(LINESTRING, 4326)` |
| `POINT` | 点 | `ST_Point(116.4, 39.9)` |
| `LINESTRING` | 线 | `ST_LineString(...)` |
| `POLYGON` | 多边形 | `ST_Polygon(...)` |
| `MULTIPOINT` | 多点 | `ST_MultiPoint(...)` |
| `MULTILINESTRING` | 多线 | `ST_MultiLineString(...)` |
| `MULTIPOLYGON` | 多多边形 | `ST_MultiPolygon(...)` |
| `GEOMETRYCOLLECTION` | 几何集合 | `ST_GeomCollection(...)` |

### Shapefile 几何类型

| ShapeType | 说明 | 对应 PostGIS 类型 |
|-----------|------|-------------------|
| `POINT` | 点 | `POINT` |
| `POLYLINE` | 折线 | `LINESTRING` |
| `POLYGON` | 多边形 | `POLYGON` |
| `MULTIPOINT` | 多点 | `MULTIPOINT` |
| `POINTZ` | 3D 点 | `POINT Z` |
| `POLYLINEZ` | 3D 折线 | `LINESTRING Z` |
| `POLYGONZ` | 3D 多边形 | `POLYGON Z` |

---

## 使用场景

### 场景 1: Shapefile → PostGIS (导入)

将 Shapefile 数据导入 PostgreSQL/PostGIS 数据库。

**配置示例**:
```json
{
  "name": "Import Shapefile to PostGIS",
  "type": "import",
  "source_id": null,  // 本地文件，无需 resource_id
  "target_id": 1,     // PostGIS 数据源 ID
  "config": {
    "source": {
      "type": "shapefile",
      "file_path": "/data/gis/cities.shp",
      "encoding": "UTF-8",
      "geometry_field": "geom"
    },
    "target": {
      "type": "postgresql",
      "table": "public.cities",
      "create_table": true,     // 自动创建表
      "srid": 4326              // 坐标系
    },
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["geom"],
        "source_format": "wkb",   // Shapefile Reader 输出 WKB
        "target_format": "wkb",   // PostGIS 接收 WKB
        "source_srid": 4326,
        "target_srid": 4326
      }
    ]
  },
  "batch_size": 1000
}
```

**cURL 调用**:
```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @shapefile-to-postgis.json
```

---

### 场景 2: PostGIS → Shapefile (导出)

从 PostgreSQL/PostGIS 导出数据为 Shapefile。

**配置示例**:
```json
{
  "name": "Export PostGIS to Shapefile",
  "type": "export",
  "source_id": 1,      // PostGIS 数据源 ID
  "target_id": null,   // 本地文件
  "config": {
    "source": {
      "type": "postgresql",
      "query": "SELECT id, name, ST_AsBinary(geom) as geom FROM public.cities WHERE population > 1000000"
    },
    "target": {
      "type": "shapefile",
      "file_path": "/data/exports/major_cities.shp",
      "geometry_field": "geom",
      "shape_type": "POINT"    // 可选，自动推断
    },
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["geom"],
        "source_format": "wkb",
        "target_format": "wkb"
      }
    ]
  }
}
```

**重要提示**: PostGIS 查询必须使用 `ST_AsBinary(geom)` 将几何转为 WKB 格式！

---

### 场景 3: Shapefile 格式转换

将 Shapefile 转换为 GeoJSON 或 WKT 格式。

**配置示例**:
```json
{
  "name": "Shapefile to GeoJSON",
  "type": "export",
  "config": {
    "source": {
      "type": "shapefile",
      "file_path": "/data/roads.shp"
    },
    "target": {
      "type": "file",
      "format": "geojson",
      "file_path": "/data/roads.geojson"
    },
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["geom"],
        "source_format": "wkb",
        "target_format": "geojson"  // 转为 GeoJSON
      }
    ]
  }
}
```

---

### 场景 4: PostGIS 数据迁移

从一个 PostGIS 数据库迁移到另一个，支持投影转换。

**配置示例**:
```json
{
  "name": "PostGIS Migration with Projection",
  "type": "sync",
  "source_id": 1,      // 源 PostGIS
  "target_id": 2,      // 目标 PostGIS
  "config": {
    "source": {
      "query": "SELECT id, name, ST_AsBinary(geom) as geom FROM gis_data"
    },
    "target": {
      "table": "gis_data_web_mercator"
    },
    "transforms": [
      {
        "type": "spatial",
        "geometry_fields": ["geom"],
        "source_format": "wkb",
        "target_format": "wkb",
        "source_srid": 4326,      // WGS84
        "target_srid": 3857       // Web Mercator (未来实现)
      }
    ]
  }
}
```

---

## PostGIS SQL 最佳实践

### 1. 查询时转换几何为 WKB

```sql
-- ✅ 正确：使用 ST_AsBinary
SELECT id, name, ST_AsBinary(geom) as geom
FROM cities;

-- ❌ 错误：直接查询几何（返回 HEXWKB 或文本）
SELECT id, name, geom FROM cities;
```

### 2. 创建带空间索引的表

```sql
-- 创建表
CREATE TABLE cities (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100),
    population INTEGER,
    geom GEOMETRY(POINT, 4326)
);

-- 创建空间索引
CREATE INDEX idx_cities_geom ON cities USING GIST (geom);
```

### 3. 批量插入优化

```sql
-- Transfer 会自动使用批量插入
INSERT INTO cities (id, name, geom)
VALUES
  (1, 'Beijing', ST_GeomFromWKB($1, 4326)),
  (2, 'Shanghai', ST_GeomFromWKB($2, 4326)),
  ...
  (1000, 'City1000', ST_GeomFromWKB($1000, 4326));
```

### 4. 空间查询示例

```sql
-- 查询北京 100km 范围内的城市
SELECT id, name, ST_AsBinary(geom) as geom
FROM cities
WHERE ST_DWithin(
    geom::geography,
    ST_SetSRID(ST_Point(116.4, 39.9), 4326)::geography,
    100000  -- 100km
);
```

---

## Shapefile 文件结构

Shapefile 实际上是一组文件的集合：

```
cities.shp  ← 几何数据 (必需)
cities.shx  ← 索引文件 (必需)
cities.dbf  ← 属性数据 (必需)
cities.prj  ← 投影信息 (可选)
cities.cpg  ← 编码信息 (可选)
cities.sbn  ← 空间索引 (可选)
```

**重要**: Transfer 在配置中只需指定 `.shp` 文件路径，其他文件会自动查找！

---

## 数据流示例

### PostGIS → Shapefile 完整流程

```
1. JDBCReader 读取 PostGIS
   ↓
   SQL: SELECT id, name, ST_AsBinary(geom) FROM cities
   ↓
   DataBatch{
     Rows: [
       {id: 1, name: "Beijing", geom: [0x01, 0x01, ...]}  // WKB bytes
     ]
   }

2. SpatialTransform 转换 (可选)
   ↓
   WKB → WKB (或其他格式)
   ↓
   DataBatch{
     Rows: [
       {id: 1, name: "Beijing", geom: [0x01, 0x01, ...]}
     ]
   }

3. ShapefileWriter 写入
   ↓
   解析 WKB → Shapefile Point
   ↓
   写入 cities.shp + cities.dbf + cities.shx
```

### Shapefile → PostGIS 完整流程

```
1. ShapefileReader 读取 .shp
   ↓
   解析 Shapefile → WKB
   ↓
   DataBatch{
     Rows: [
       {id: 1, name: "Beijing", geom: [0x01, 0x01, ...]}  // WKB bytes
     ]
   }

2. SpatialTransform 转换 (可选)
   ↓
   WKB → WKB (坐标转换等)

3. JDBCWriter 写入 PostGIS
   ↓
   SQL: INSERT INTO cities (id, name, geom)
        VALUES (1, 'Beijing', ST_GeomFromWKB($1, 4326))
```

---

## 性能优化

### 1. 批量大小调优

```json
{
  "batch_size": 5000,  // 空间数据推荐 1000-5000
  "max_parallelism": 4 // 并行处理
}
```

**建议**:
- **小几何对象** (Point): `batch_size = 5000`
- **大几何对象** (Polygon): `batch_size = 500-1000`
- **超大多边形**: `batch_size = 100`

### 2. 空间索引

**PostGIS 端**:
```sql
-- 导入前先删除索引
DROP INDEX IF EXISTS idx_cities_geom;

-- 批量导入数据
-- (Transfer 执行)

-- 导入后重建索引
CREATE INDEX idx_cities_geom ON cities USING GIST (geom);
```

### 3. 使用事务

Transfer 自动使用事务批量提交，每个 batch 一个事务。

---

## 常见问题

### Q1: Shapefile 字段名限制？

**A**: DBF 格式限制字段名最多 10 个字符。Transfer 会自动截断：

```
原字段名: population_density
DBF 字段名: population  (自动截断)
```

**解决方案**: 使用 FieldMappingTransform 重命名字段：
```json
{
  "type": "field_mapping",
  "mappings": [
    {
      "source": "population_density",
      "target": "pop_dens",  // 10 字符以内
      "type": "float"
    }
  ]
}
```

### Q2: 如何处理坐标系？

**A**: 使用 SpatialTransform 的 `source_srid` 和 `target_srid` 参数（未来支持投影转换）。

当前版本可以通过 PostGIS SQL 实现：
```sql
-- 查询时转换坐标系
SELECT id, name,
       ST_AsBinary(ST_Transform(geom, 3857)) as geom  -- WGS84 → Web Mercator
FROM cities;
```

### Q3: Shapefile 不支持 NULL 值？

**A**: Shapefile 的 DBF 格式支持有限的 NULL 表示。Transfer 会将 NULL 转为默认值：
- 字符串: 空字符串 `""`
- 数值: `0`
- 布尔: `false`

### Q4: 如何处理大文件？

**A**: 使用流式处理和批量提交：

```json
{
  "mode": "stream",          // 流式模式
  "batch_size": 1000,        // 每批 1000 条
  "config": {
    "checkpoint_enabled": true,  // 启用断点续传
    "checkpoint_interval": 10000 // 每 10000 条记录一个检查点
  }
}
```

### Q5: 支持 3D 几何吗？

**A**: 当前版本支持 2D 几何，3D 支持计划在下个版本实现。

---

## 扩展新的空间数据类型

### 示例：添加 GeoPackage 支持

```go
// 1. 实现 Reader
type GeoPackageReader struct {
    db *sql.DB
}

func (r *GeoPackageReader) Read(ctx) (*pipeline.DataBatch, error) {
    // 读取 GeoPackage (SQLite + 空间扩展)
    // 输出 DataBatch{Rows: [{geom: wkbBytes}]}
}

// 2. 注册
func init() {
    registry.RegisterReader("geopackage", NewGeoPackageReader)
    registry.RegisterWriter("geopackage", NewGeoPackageWriter)
}

// 3. 立即可用！
{
  "source": {"type": "geopackage", "file_path": "data.gpkg"},
  "target": {"type": "postgresql", "table": "imported_data"},
  "transforms": [{"type": "spatial", ...}]  // 复用现有转换器
}
```

**无需修改**:
- ✅ SpatialTransform (自动支持)
- ✅ FieldMappingTransform (自动支持)
- ✅ Pipeline Engine (自动支持)
- ✅ API 端点 (自动支持)

---

## 测试验证

### 1. 测试 Shapefile Reader

```bash
# 创建测试任务
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "Test Shapefile Reader",
    "type": "import",
    "config": {
      "source": {
        "type": "shapefile",
        "file_path": "/data/test.shp"
      },
      "target": {
        "type": "postgresql",
        "table": "test_import"
      }
    }
  }'
```

### 2. 测试 PostGIS → Shapefile

```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "Test PostGIS Export",
    "type": "export",
    "source_id": 1,
    "config": {
      "source": {
        "query": "SELECT id, ST_AsBinary(geom) as geom FROM test_table"
      },
      "target": {
        "type": "shapefile",
        "file_path": "/data/output.shp"
      }
    }
  }'
```

---

## 未来扩展计划

### 短期 (下一版本)

- [ ] 坐标系投影转换（集成 PROJ 库）
- [ ] 3D 几何支持 (PointZ, PolygonZ)
- [ ] GeoPackage (GPKG) 支持
- [ ] GeoJSON 文件读写

### 中期

- [ ] KML/KMZ 支持
- [ ] GeoTIFF (栅格数据) 支持
- [ ] CAD (DWG/DXF) 支持

### 长期

- [ ] ArcGIS Geodatabase
- [ ] MapInfo TAB
- [ ] Oracle Spatial
- [ ] MongoDB GeoJSON

**所有扩展都遵循松耦合设计，添加新数据源不影响现有代码！**

---

## 总结

### ✅ 已实现

- Shapefile Reader/Writer
- PostGIS 空间类型支持
- 空间数据格式转换
- WKB/WKT/GeoJSON 互转
- 批量处理和流式处理

### ✅ 松耦合设计

- Reader/Transform/Writer 完全独立
- 新增数据源自动支持所有转换器
- 前端自动发现新组件
- 无需修改核心 Pipeline 代码

### ✅ 易于扩展

添加新空间数据类型只需：
1. 实现 Reader/Writer 接口
2. 注册到全局注册表
3. 完成！

---

**相关文档**:
- [ARCHITECTURE_LOOSE_COUPLING_ANALYSIS.md](ARCHITECTURE_LOOSE_COUPLING_ANALYSIS.md) - 松耦合架构分析
- [SPATIAL_TRANSFORM_USAGE.md](SPATIAL_TRANSFORM_USAGE.md) - 空间转换器使用指南
- [SPATIAL_AND_EXTENSIBLE_TRANSFORMS.md](SPATIAL_AND_EXTENSIBLE_TRANSFORMS.md) - 扩展机制详解
