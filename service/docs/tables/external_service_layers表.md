# external_service_layers 表结构和 API 说明

## 一、表结构概览

`service.external_service_layers` 表是 Service 模块的外部服务图层表，存储外部服务的图层/要素类信息。与 `external_services` 表是 1:N 关系，记录每个服务包含的地理数据图层。

### 核心功能

- **图层元数据存储**：记录图层名称、几何类型、坐标系统、边界框等信息
- **图层启用控制**：支持单独启用/禁用图层
- **空间范围管理**：存储图层的地理边界框（BBox）
- **自动解析**：创建服务时自动从 GetCapabilities 解析图层信息

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 图层唯一标识 |
| `service_id` | INTEGER | NOT NULL, INDEXED, FK | 关联的服务 ID |
| `layer_name` | VARCHAR(255) | NOT NULL | 图层名称（服务中的标识符） |
| `display_name` | VARCHAR(255) | | 显示名称（友好名称） |
| `geometry_type` | VARCHAR(50) | | 几何类型：'Point'、'LineString'、'Polygon'、'MultiPolygon' 等 |
| `crs` | VARCHAR(50) | | 坐标参考系统：'EPSG:4326'、'EPSG:3857' 等 |
| `bbox` | JSONB | | 边界框（地理范围） |
| `metadata` | JSONB | | 图层元数据（样式、属性字段等） |
| `enabled` | BOOLEAN | DEFAULT true | 是否启用 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_external_service_layer_service` | service_id | 按服务查询图层 |

### 2.3 外键约束

| 外键字段 | 引用表 | 引用字段 | 约束 |
|---------|--------|---------|------|
| `service_id` | `external_services` | `id` | ON DELETE CASCADE |

**说明**：删除服务时，所有关联的图层记录会自动删除。

---

## 三、GeometryType 说明

| 值 | 含义 | 说明 |
|---|------|------|
| `Point` | 点 | 单个点（如城市位置） |
| `MultiPoint` | 多点 | 多个点集合 |
| `LineString` | 线 | 单条线（如道路） |
| `MultiLineString` | 多线 | 多条线集合 |
| `Polygon` | 面 | 单个多边形（如行政区） |
| `MultiPolygon` | 多面 | 多个多边形集合 |
| `GeometryCollection` | 几何集合 | 混合几何类型 |
| `null` | 无几何 | 属性表（无空间字段） |

---

## 四、JSON 字段详细结构

### 4.1 BBox 字段（边界框）

**WGS84 坐标**（EPSG:4326）：
```json
{
  "type": "BBox",
  "crs": "EPSG:4326",
  "coordinates": [-180, -90, 180, 90]
}
```

**Web 墨卡托**（EPSG:3857）：
```json
{
  "type": "BBox",
  "crs": "EPSG:3857",
  "coordinates": [-20037508.34, -20037508.34, 20037508.34, 20037508.34]
}
```

### 4.2 Metadata 字段（图层元数据）

**WMS 图层**：
```json
{
  "title": "城市点图层",
  "abstract": "包含全国主要城市的位置信息",
  "queryable": true,
  "styles": [
    {
      "name": "default",
      "title": "默认样式",
      "legend_url": "https://gis.example.com/legend.png"
    }
  ],
  "dimensions": {
    "time": ["2020-01-01", "2024-12-31"]
  }
}
```

**WFS 要素类**：
```json
{
  "title": "城市要素",
  "abstract": "矢量城市数据",
  "properties": [
    {"name": "id", "type": "integer"},
    {"name": "name", "type": "string"},
    {"name": "population", "type": "integer"}
  ],
  "output_formats": ["GeoJSON", "GML", "KML"]
}
```

---

## 五、API 端点说明

**注意**：图层主要通过服务 API 间接管理，很少有单独的图层 API。

### 5.1 GET /api/service/registry/services/:id - 获取服务详情（包含图层）

**响应**：

```json
{
  "id": 1,
  "name": "某市 WMS 服务",
  "service_type": "wms",
  "layers": [
    {
      "id": 10,
      "service_id": 1,
      "layer_name": "cities",
      "display_name": "城市点图层",
      "geometry_type": "Point",
      "crs": "EPSG:4326",
      "bbox": {
        "crs": "EPSG:4326",
        "coordinates": [73.5, 3.8, 135.1, 53.6]
      },
      "enabled": true,
      "metadata": {...}
    },
    {
      "id": 11,
      "layer_name": "roads",
      "display_name": "道路网",
      "geometry_type": "LineString",
      "enabled": false
    }
  ]
}
```

---

### 5.2 POST /api/service/registry/services/:id/refresh - 刷新图层列表

**功能**：重新解析 GetCapabilities，更新图层列表。

**响应**：

```json
{
  "message": "图层已更新",
  "layers_added": 2,
  "layers_updated": 3,
  "layers_removed": 1
}
```

**逻辑**：
1. 调用服务的 GetCapabilities
2. 解析图层列表
3. 新增不存在的图层
4. 更新已存在图层的元数据
5. 删除服务中不再存在的图层

---

### 5.3 PUT /api/service/registry/services/:service_id/layers/:layer_id - 更新图层

**请求体**（部分更新）：

```json
{
  "display_name": "城市点图层（更新）",
  "enabled": false
}
```

**响应**：返回更新后的图层对象

---

### 5.4 DELETE /api/service/registry/services/:service_id/layers/:layer_id - 删除图层

**响应**（204 No Content）

**注意**：通常不需要手动删除，刷新服务元数据时会自动清理。

---

## 六、图层自动解析

### 6.1 WMS 图层解析

**GetCapabilities 示例**：
```xml
<Layer>
  <Name>cities</Name>
  <Title>城市点图层</Title>
  <CRS>EPSG:4326</CRS>
  <CRS>EPSG:3857</CRS>
  <EX_GeographicBoundingBox>
    <westBoundLongitude>73.5</westBoundLongitude>
    <eastBoundLongitude>135.1</eastBoundLongitude>
    <southBoundLatitude>3.8</southBoundLatitude>
    <northBoundLatitude>53.6</northBoundLatitude>
  </EX_GeographicBoundingBox>
</Layer>
```

**解析为**：
```go
ServiceLayer{
    LayerName:    "cities",
    DisplayName:  "城市点图层",
    GeometryType: "Point",  // 从服务描述推断
    CRS:          "EPSG:4326",
    BBox: map[string]interface{}{
        "crs": "EPSG:4326",
        "coordinates": []float64{73.5, 3.8, 135.1, 53.6},
    },
    Enabled: true,
}
```

### 6.2 WFS 要素类解析

**DescribeFeatureType 解析**：
- 解析属性字段列表（name、type）
- 识别几何字段类型（gml:Point、gml:LineString 等）
- 存储到 metadata.properties

---

## 七、使用示例

### 示例 1：查询服务的所有图层

```bash
# 查询服务详情（包含图层）
curl -X GET http://localhost:8086/api/service/registry/services/1 \
  -H "Authorization: Bearer $TOKEN" \
  | jq '.layers'
```

### 示例 2：启用/禁用图层

```bash
# 禁用某个图层
curl -X PUT http://localhost:8086/api/service/registry/services/1/layers/10 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": false
  }'
```

### 示例 3：刷新服务图层列表

```bash
# 刷新服务元数据（包括图层）
curl -X POST http://localhost:8086/api/service/registry/services/1/refresh \
  -H "Authorization: Bearer $TOKEN"
```

---

## 八、重要说明

### 8.1 图层名称规范

**layer_name** vs **display_name**：
- `layer_name`：服务中的标识符，用于 API 调用（如 WMS GetMap 的 LAYERS 参数）
- `display_name`：用户友好的显示名称，可自定义

**示例**：
```
layer_name: "province_boundary_2024"
display_name: "2024年省级行政区划"
```

### 8.2 多坐标系统支持

一个图层可能支持多个 CRS（存储在 metadata 中）：
```json
{
  "supported_crs": ["EPSG:4326", "EPSG:3857", "EPSG:4549"]
}
```

`crs` 字段存储**默认/推荐**的坐标系统。

### 8.3 图层启用状态

**enabled = false** 的用途：
- 临时禁用某个图层（不删除记录）
- 服务目录查询时默认过滤禁用图层
- 可用于灰度发布（先禁用，测试后启用）

### 8.4 级联删除注意事项

删除服务时，所有关联图层会自动删除（ON DELETE CASCADE），需谨慎操作。

---

## 九、性能优化

### 9.1 图层列表缓存

**问题**：每次查询服务都会联表查询 layers

**优化**：
```go
// 使用 GORM Preload 一次性加载
db.Preload("Layers").Find(&services)
```

### 9.2 大量图层处理

**场景**：某些 WMS 服务包含数百个图层

**优化策略**：
- 分页加载图层列表
- 只加载 `enabled=true` 的图层
- 前端按需加载详细元数据

---

## 十、相关文档

- [external_services表](./external_services表.md) - 外部服务表
- [数据库架构](../数据库架构.md) - Service 模块架构
