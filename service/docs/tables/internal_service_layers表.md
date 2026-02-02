# internal_service_layers 表结构和 API 说明

## 一、表结构概览

`service.internal_service_layers` 表是内部服务的图层配置表，定义了每个内部服务包含的数据图层。每个图层对应 PostgreSQL 中的一张空间表，通过 OGC 标准协议对外发布。与 `internal_services` 表是 N:1 关系。

### 核心功能

- **图层映射**：将 PostgreSQL 空间表映射为 OGC 图层
- **元数据管理**：维护图层标题、摘要、关键词等描述信息
- **空间元数据**：记录几何类型、SRID、范围等空间属性
- **查询配置**：支持可查询性、最大要素数、可过滤字段等配置
- **样式管理**：支持自定义图层样式配置
- **显示控制**：支持图层启用/禁用、显示顺序等

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 图层唯一标识 |
| `service_id` | INTEGER | NOT NULL, INDEXED | 所属服务 ID（外键） |
| `layer_name` | VARCHAR(255) | NOT NULL | 图层名称（英文标识符，用于 URL） |
| `title` | VARCHAR(255) | NOT NULL | 图层标题（显示名称） |
| `abstract` | TEXT | | 图层摘要/描述 |
| `keywords` | TEXT[] | | 图层关键词数组 |
| `meta_item_id` | INTEGER | INDEXED | 关联的元数据项 ID（可选） |
| `schema_name` | VARCHAR(255) | NOT NULL, INDEXED | 数据库 schema 名称 |
| `table_name` | VARCHAR(255) | NOT NULL, INDEXED | 数据库表名称（对应 db_table_name） |
| `geometry_column` | VARCHAR(255) | NOT NULL | 几何字段名称 |
| `srid` | INTEGER | NOT NULL | 空间参考系统 ID |
| `extent_4326` | JSONB | | WGS84 范围 [minLng, minLat, maxLng, maxLat] |
| `geometry_types` | TEXT[] | | 几何类型数组（Point, LineString, Polygon 等） |
| `queryable` | BOOLEAN | DEFAULT true | 是否可查询 |
| `max_features` | INTEGER | | 单次查询最大要素数（覆盖服务级配置） |
| `filter_columns` | TEXT[] | | 允许过滤的字段列表 |
| `default_style` | JSONB | | 默认样式配置（JSON 格式） |
| `display_order` | INTEGER | DEFAULT 0 | 显示顺序（数值越小越靠前） |
| `enabled` | BOOLEAN | DEFAULT true | 是否启用 |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_internal_service_layer_service` | service_id | 按服务查询图层 |
| `idx_internal_service_layer_meta_item` | meta_item_id | 按元数据项查询 |
| `idx_internal_service_layer_table` | schema_name, table_name | 快速定位数据表 |

### 2.3 外键约束

| 外键字段 | 引用表 | 引用字段 | 约束 |
|---------|--------|---------|------|
| `service_id` | `internal_services` | `id` | ON DELETE CASCADE |

### 2.4 关联关系

| 关系类型 | 关联表 | 关系说明 |
|---------|--------|---------|
| N:1 | `internal_services` | 多个图层属于一个服务 |
| N:1 | `meta.items` (可选) | 图层可关联元数据项 |

---

## 三、字段详细说明

### 3.1 layer_name (图层标识符)

**用途**：用于构建 OGC 服务 URL 和请求参数

**命名规则**：
- 建议使用英文、数字、下划线
- 长度：1-255 字符
- 示例：`poi_restaurants`, `buildings`, `land_use_2024`

**URL 示例**：
```
WFS GetFeature:
http://api.example.com/ogc/wfs/city_poi?service=WFS&request=GetFeature&typeName=poi_restaurants

OGC API Features Items:
http://api.example.com/ogc/api/city_poi/collections/poi_restaurants/items
```

### 3.2 数据源字段 (schema_name, table_name, geometry_column)

这三个字段定义了图层的数据来源：

| 字段 | 说明 | 示例 |
|-----|------|------|
| `schema_name` | PostgreSQL schema 名称 | `public`, `gis_data`, `city_assets` |
| `table_name` | 表名（注意：字段名为 db_table_name） | `poi_points`, `road_network`, `parcels` |
| `geometry_column` | 几何字段名 | `geom`, `geometry`, `the_geom`, `shape` |

**重要**：
- 表必须存在于 `engine_id` 对应的存储引擎中
- 几何字段必须是 PostGIS 几何类型（geometry 或 geography）
- 建议在几何字段上创建空间索引：`CREATE INDEX idx_geom ON table USING GIST (geom);`

### 3.3 空间元数据

#### srid (空间参考系统)

**常用 SRID**：
- `4326` - WGS84 (GPS 坐标)
- `3857` - Web Mercator (Web 地图)
- `4490` - CGCS2000 (中国国家坐标系)
- `2000` - 北京 54
- `4214` - 西安 80

**作用**：确定数据的坐标系统，用于坐标转换和空间查询。

#### extent_4326 (范围)

**格式**：JSON 数组 `[minLng, minLat, maxLng, maxLat]`

**示例**：
```json
[116.3, 39.9, 116.5, 40.0]  // 北京市某区域
```

**作用**：
- WFS GetCapabilities 中的 `<ows:WGS84BoundingBox>`
- OGC API Collections 中的 `extent.spatial.bbox`
- 前端地图初始范围定位

#### geometry_types (几何类型)

**可能的值**：`Point`, `LineString`, `Polygon`, `MultiPoint`, `MultiLineString`, `MultiPolygon`

**示例**：
```json
["Point"]                          // 纯点数据
["Polygon", "MultiPolygon"]        // 多边形数据（可能混合）
["LineString", "MultiLineString"]  // 线数据
```

**用途**：
- 客户端选择合适的渲染方式
- WFS DescribeFeatureType 响应
- 样式配置参考

### 3.4 查询配置

#### queryable (可查询性)

- `true`：支持属性查询和空间查询（默认）
- `false`：仅用于展示，不支持 WFS GetFeature 查询

**适用场景**：
- `true`：需要下载数据或属性查询的图层（POI、建筑物）
- `false`：仅作为底图的栅格化矢量图层

#### max_features (最大要素数)

**优先级**：图层级 `max_features` > 服务级 `max_features`

**示例**：
- 服务级设置：1000
- 图层级设置：500（该图层仅返回 500 条）
- 图层级未设置：使用服务级 1000

**建议值**：
- 小数据集（< 5000 条）：无需设置（使用服务级）
- 大数据集（> 10 万条）：500-1000
- 超大数据集（> 100 万条）：100-500

#### filter_columns (可过滤字段)

**格式**：字符串数组

**示例**：
```json
["name", "category", "status", "created_at"]
```

**作用**：
- 限制客户端可用于 WHERE 条件的字段
- 防止查询敏感字段
- 为空时允许所有字段过滤

**WFS 查询示例**：
```xml
<ogc:Filter>
  <ogc:PropertyIsEqualTo>
    <ogc:PropertyName>category</ogc:PropertyName>
    <ogc:Literal>restaurant</ogc:Literal>
  </ogc:PropertyIsEqualTo>
</ogc:Filter>
```

### 3.5 样式配置 (default_style)

**格式**：JSONB（支持多种样式规范）

**示例 1 - Simple Style**：
```json
{
  "type": "simple",
  "point": {
    "color": "#FF5733",
    "size": 8,
    "symbol": "circle"
  },
  "line": {
    "color": "#3498db",
    "width": 2,
    "dashArray": [5, 5]
  },
  "polygon": {
    "fillColor": "#2ecc71",
    "fillOpacity": 0.5,
    "strokeColor": "#27ae60",
    "strokeWidth": 2
  }
}
```

**示例 2 - Categorized Style**：
```json
{
  "type": "categorized",
  "field": "category",
  "categories": [
    {
      "value": "restaurant",
      "color": "#e74c3c",
      "label": "餐饮"
    },
    {
      "value": "hotel",
      "color": "#3498db",
      "label": "酒店"
    }
  ]
}
```

**示例 3 - SLD (Styled Layer Descriptor)**：
```json
{
  "type": "sld",
  "version": "1.1.0",
  "sld_body": "<StyledLayerDescriptor>...</StyledLayerDescriptor>"
}
```

### 3.6 显示控制

#### display_order (显示顺序)

**规则**：数值越小越靠前

**示例**：
- 底图图层：100
- 建筑物图层：50
- POI 图层：10

**作用**：
- WFS GetCapabilities 中的图层列表顺序
- 前端图层树的默认顺序

#### enabled (启用状态)

- `true`：图层在 OGC 服务中可见和可查询
- `false`：图层被隐藏，不出现在 GetCapabilities 中

**使用场景**：
- 临时停用某个图层而不删除配置
- 数据更新期间暂时禁用

### 3.7 元数据关联 (meta_item_id)

**可选字段**：关联 `meta.items` 表的元数据项

**作用**：
- 自动同步元数据的标题、摘要、关键词
- 追溯数据来源和更新历史
- 统一管理数据和服务元数据

**示例场景**：
1. Meta 模块扫描到新表 → 创建 meta_item
2. 用户在 Service 模块发布该表 → 设置 `meta_item_id`
3. Meta 模块更新元数据 → Service 图层自动获取最新信息

---

## 四、API 端点

### 4.1 POST /api/service/internal/services/:service_id/layers - 添加图层

**请求体**：
```json
{
  "layer_name": "poi_restaurants",
  "title": "餐饮兴趣点",
  "abstract": "城市餐饮类POI数据",
  "keywords": ["餐饮", "POI", "美食"],
  "meta_item_id": 123,
  "schema_name": "public",
  "table_name": "poi_data",
  "geometry_column": "geom",
  "srid": 4326,
  "extent_4326": [116.3, 39.9, 116.5, 40.0],
  "geometry_types": ["Point"],
  "queryable": true,
  "max_features": 500,
  "filter_columns": ["name", "category", "rating"],
  "default_style": {
    "type": "simple",
    "point": {
      "color": "#e74c3c",
      "size": 8,
      "symbol": "circle"
    }
  },
  "display_order": 10
}
```

**响应** (201 Created)：
```json
{
  "code": 200,
  "message": "图层添加成功",
  "data": {
    "id": 1,
    "service_id": 1,
    "layer_name": "poi_restaurants",
    "title": "餐饮兴趣点",
    "enabled": true,
    "created_at": "2026-01-31T10:00:00Z",
    "updated_at": "2026-01-31T10:00:00Z"
  }
}
```

---

### 4.2 GET /api/service/internal/services/:service_id/layers - 获取服务的所有图层

**响应** (200 OK)：
```json
{
  "code": 200,
  "message": "查询成功",
  "data": [
    {
      "id": 1,
      "layer_name": "poi_restaurants",
      "title": "餐饮兴趣点",
      "geometry_types": ["Point"],
      "srid": 4326,
      "enabled": true,
      "display_order": 10
    },
    {
      "id": 2,
      "layer_name": "poi_hotels",
      "title": "酒店",
      "geometry_types": ["Point"],
      "srid": 4326,
      "enabled": true,
      "display_order": 20
    }
  ]
}
```

---

### 4.3 GET /api/service/internal/layers/:id - 获取图层详情

**响应** (200 OK)：
```json
{
  "code": 200,
  "message": "获取成功",
  "data": {
    "id": 1,
    "service_id": 1,
    "layer_name": "poi_restaurants",
    "title": "餐饮兴趣点",
    "abstract": "城市餐饮类POI数据",
    "keywords": ["餐饮", "POI", "美食"],
    "schema_name": "public",
    "table_name": "poi_data",
    "geometry_column": "geom",
    "srid": 4326,
    "extent_4326": [116.3, 39.9, 116.5, 40.0],
    "geometry_types": ["Point"],
    "queryable": true,
    "max_features": 500,
    "filter_columns": ["name", "category", "rating"],
    "default_style": {
      "type": "simple",
      "point": {"color": "#e74c3c", "size": 8}
    },
    "display_order": 10,
    "enabled": true,
    "created_at": "2026-01-31T10:00:00Z",
    "updated_at": "2026-01-31T10:00:00Z"
  }
}
```

---

### 4.4 PUT /api/service/internal/layers/:id - 更新图层

**请求体** (仅更新提供的字段)：
```json
{
  "title": "餐饮POI（更新）",
  "max_features": 1000,
  "enabled": false,
  "default_style": {
    "type": "simple",
    "point": {"color": "#3498db", "size": 10}
  }
}
```

**响应** (200 OK)：
```json
{
  "code": 200,
  "message": "更新成功",
  "data": {
    "id": 1,
    "title": "餐饮POI（更新）",
    "max_features": 1000,
    "enabled": false,
    "updated_at": "2026-01-31T11:00:00Z"
  }
}
```

---

### 4.5 DELETE /api/service/internal/layers/:id - 删除图层

**响应** (204 No Content)

**注意**：直接删除（硬删除），不使用软删除。

---

## 五、OGC 服务中的图层表现

添加图层后，图层会自动出现在对应 OGC 服务的各个端点中：

### 5.1 WFS GetCapabilities

```xml
<wfs:WFS_Capabilities>
  <FeatureTypeList>
    <FeatureType>
      <Name>city_poi:poi_restaurants</Name>
      <Title>餐饮兴趣点</Title>
      <Abstract>城市餐饮类POI数据</Abstract>
      <Keywords>
        <Keyword>餐饮</Keyword>
        <Keyword>POI</Keyword>
      </Keywords>
      <DefaultSRS>EPSG:4326</DefaultSRS>
      <ows:WGS84BoundingBox>
        <ows:LowerCorner>116.3 39.9</ows:LowerCorner>
        <ows:UpperCorner>116.5 40.0</ows:UpperCorner>
      </ows:WGS84BoundingBox>
    </FeatureType>
  </FeatureTypeList>
</wfs:WFS_Capabilities>
```

### 5.2 OGC API Features - Collections

```json
{
  "collections": [
    {
      "id": "poi_restaurants",
      "title": "餐饮兴趣点",
      "description": "城市餐饮类POI数据",
      "extent": {
        "spatial": {
          "bbox": [[116.3, 39.9, 116.5, 40.0]],
          "crs": "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
        }
      },
      "links": [
        {
          "rel": "items",
          "href": "/ogc/api/city_poi/collections/poi_restaurants/items"
        }
      ]
    }
  ]
}
```

### 5.3 WMTS GetCapabilities

```xml
<Capabilities>
  <Contents>
    <Layer>
      <ows:Title>餐饮兴趣点</ows:Title>
      <ows:Identifier>poi_restaurants</ows:Identifier>
      <ows:WGS84BoundingBox>
        <ows:LowerCorner>116.3 39.9</ows:LowerCorner>
        <ows:UpperCorner>116.5 40.0</ows:UpperCorner>
      </ows:WGS84BoundingBox>
      <Style isDefault="true">
        <ows:Identifier>default</ows:Identifier>
      </Style>
      <TileMatrixSetLink>
        <TileMatrixSet>EPSG:3857</TileMatrixSet>
      </TileMatrixSetLink>
    </Layer>
  </Contents>
</Capabilities>
```

---

## 六、业务场景示例

### 场景 1：发布 POI 数据的多个分类图层

**目标**：将 POI 表按类别拆分为多个图层

**数据表**：`public.poi_data`（包含 category 字段）

**图层配置**：
```json
// 图层 1: 餐饮
{
  "layer_name": "poi_restaurants",
  "title": "餐饮",
  "schema_name": "public",
  "table_name": "poi_data",
  "geometry_column": "geom",
  "filter_columns": ["category"],
  "default_style": {
    "type": "categorized",
    "field": "category",
    "filter": "category = 'restaurant'"
  }
}

// 图层 2: 酒店
{
  "layer_name": "poi_hotels",
  "title": "酒店",
  "schema_name": "public",
  "table_name": "poi_data",
  "filter_columns": ["category"],
  "default_style": {
    "filter": "category = 'hotel'"
  }
}
```

**效果**：同一张表发布为多个逻辑图层，客户端可按需加载。

---

### 场景 2：大数据集的性能优化

**问题**：道路网络表有 50 万条数据，全量查询很慢

**优化方案**：
```json
{
  "layer_name": "road_network",
  "title": "道路网络",
  "max_features": 500,           // 限制单次返回
  "filter_columns": ["road_type", "district"],  // 强制过滤
  "queryable": true
}
```

**建议**：
- 引导用户使用空间过滤（bbox）
- 提供 `road_type` 等属性过滤选项
- 配合 WMTS 用于全局浏览

---

### 场景 3：动态数据的定期更新

**场景**：实时监测数据每小时更新

**配置**：
```json
{
  "layer_name": "iot_sensors",
  "title": "传感器监测点",
  "schema_name": "public",
  "table_name": "sensor_data_latest",  // 视图，只包含最新数据
  "geometry_column": "location",
  "filter_columns": ["sensor_type", "status", "last_update"],
  "max_features": 1000
}
```

**技巧**：
- 使用数据库视图（`sensor_data_latest`）自动过滤最新数据
- 添加 `last_update` 过滤字段，客户端可查询特定时间范围

---

### 场景 4：多坐标系支持

**需求**：数据存储为 CGCS2000，但需要同时支持 WGS84 和 Web Mercator

**方案 1 - 服务级坐标转换**（推荐）：
```json
{
  "layer_name": "parcels",
  "srid": 4490,  // 原始坐标系
  "default_style": {...}
}
```

服务端在 WFS/OGC API 响应时自动转换坐标系（根据客户端请求的 SRID）。

**方案 2 - 多图层发布**（不推荐，数据冗余）：
- `parcels_4490` (CGCS2000)
- `parcels_4326` (WGS84)
- `parcels_3857` (Web Mercator)

---

## 七、设计决策

### 7.1 为什么冗余存储数据源信息？

**字段**：`schema_name`, `table_name`, `geometry_column`

**原因**：
- 快速构建 SQL 查询，无需关联 `meta.items` 表
- 即使不关联元数据项也能发布服务
- 减少服务响应时的表关联次数

**trade-off**：可能与 `meta.items` 不一致，但通过 `meta_item_id` 关联可同步更新。

### 7.2 为什么需要 display_order？

**原因**：
- WFS GetCapabilities 的图层列表顺序影响客户端显示
- 重要图层（如底图）应排在前面
- 用户可能有特定的图层组织逻辑

### 7.3 为什么 max_features 可以覆盖服务级配置？

**原因**：
- 不同图层的数据量差异大
- 小图层（如行政区划）可返回更多数据
- 大图层（如传感器点）需要更严格限制

**优先级**：图层级 > 服务级 > 系统默认(1000)

### 7.4 为什么 extent_4326 使用 JSONB？

**原因**：
- 灵活存储 `[minX, minY, maxX, maxY]` 数组
- 支持 JSON 查询和索引（如 GiST 索引）
- 与 GeoJSON 规范一致

**替代方案**：使用 4 个独立字段（min_lng, min_lat, max_lng, max_lat），但不够简洁。

---

## 八、性能优化建议

### 8.1 确保空间索引存在

**检查索引**：
```sql
SELECT
    schemaname,
    tablename,
    indexname
FROM pg_indexes
WHERE indexname LIKE '%gist%'
AND tablename = 'your_table';
```

**创建索引**：
```sql
CREATE INDEX idx_your_table_geom
ON your_schema.your_table
USING GIST (geom);
```

### 8.2 合理设置 max_features

| 数据量 | 建议值 |
|--------|--------|
| < 1000 条 | 无需设置（使用服务级） |
| 1000-10 万条 | 500-1000 |
| 10 万-100 万条 | 100-500 |
| > 100 万条 | 50-200 + 强制空间过滤 |

### 8.3 使用数据库视图优化复杂查询

**示例**：属性过滤 + 几何简化
```sql
CREATE VIEW simplified_buildings AS
SELECT
    id,
    name,
    ST_Simplify(geom, 0.0001) AS geom,  -- 简化几何
    category
FROM buildings
WHERE category IN ('residential', 'commercial');  -- 预过滤
```

然后发布视图而不是原始表：
```json
{
  "schema_name": "public",
  "table_name": "simplified_buildings"
}
```

### 8.4 启用 filter_columns 限制查询

**问题**：允许所有字段过滤可能导致性能问题

**解决**：
```json
{
  "filter_columns": ["status", "category", "district"]  // 仅允许有索引的字段
}
```

配合数据库索引：
```sql
CREATE INDEX idx_buildings_status ON buildings(status);
CREATE INDEX idx_buildings_category ON buildings(category);
```

---

## 九、安全机制

### 9.1 租户隔离

- 图层通过 `service_id` 关联到服务
- 服务通过 `tenant_id` 隔离
- API 自动过滤当前租户的服务和图层

### 9.2 数据源访问控制

- 图层只能访问其服务的 `engine_id` 对应的存储引擎
- 后端验证 `schema_name.table_name` 是否存在于引擎中
- 防止跨引擎数据访问

### 9.3 SQL 注入防护

- `filter_columns` 白名单机制
- 后端使用参数化查询
- 严格验证字段名和表名（仅允许字母、数字、下划线）

---

## 十、常见问题

### Q1：如何发布多个 schema 的表？

**答**：每个图层独立指定 `schema_name`，只要在同一个 `engine_id` 下即可：
```json
// 图层 1
{"schema_name": "public", "table_name": "poi_data"}

// 图层 2
{"schema_name": "gis_data", "table_name": "buildings"}
```

### Q2：几何字段名必须是 "geom" 吗？

**答**：不必须，`geometry_column` 可以是任何名称（geom, geometry, shape, the_geom 等）。

### Q3：如何处理混合几何类型的表？

**答**：设置多个几何类型：
```json
{
  "geometry_types": ["Polygon", "MultiPolygon"]
}
```

客户端会据此选择合适的渲染方式。

### Q4：图层删除后数据表会被删除吗？

**答**：不会。删除图层仅删除服务配置，不影响原始数据表。

### Q5：如何实现图层的分组管理？

**答**：使用 `display_order` + 命名约定：
```json
// 底图组 (100-199)
{"layer_name": "basemap_satellite", "display_order": 100}
{"layer_name": "basemap_vector", "display_order": 101}

// 业务数据组 (200-299)
{"layer_name": "business_poi", "display_order": 200}
{"layer_name": "business_parcels", "display_order": 201}
```

前端根据 `display_order` 范围分组显示。

---

## 十一、相关文档

- [internal_services表](./internal_services表.md) - 内部服务主表
- [external_service_layers表](./external_service_layers表.md) - 外部服务图层表
- [数据库架构](../数据库架构.md) - Service 模块架构
- [Service 模块说明](../CLAUDE.md) - 模块整体说明

---

## 十二、最佳实践总结

1. **命名规范**：使用语义化的 `layer_name`（如 `poi_restaurants` 而非 `layer1`）
2. **空间索引**：确保所有几何字段有 GIST 索引
3. **性能优化**：合理设置 `max_features`，启用 `filter_columns` 白名单
4. **元数据关联**：有条件时设置 `meta_item_id`，便于追溯和同步
5. **样式管理**：为常用图层配置 `default_style`，提升用户体验
6. **显示控制**：使用 `display_order` 和 `enabled` 灵活管理图层可见性
7. **数据视图**：对复杂查询使用数据库视图，而非直接发布原始表
8. **文档完善**：填写 `title`, `abstract`, `keywords`，方便搜索和发现
