# Manager 统一快显 API 设计

## 一、目标

Manager 快显 API 必须以 ADDP ResourceLocator 作为统一资源身份，不再把 PostgreSQL 的 `engine_id + schema + table` 暴露为快显公共 API 主路径。

本设计遵循三个原则：

1. 名副其实：接口名为 GeoJSON 时，响应必须是标准 GeoJSON，不能返回 `rows + WKT` 之类的表格预览结构。
2. 外部统一：PostGIS、NFS Shapefile、对象存储 GeoJSON、GeoPackage 等空间 item 都走同一套快显 API。
3. 内部分派：后端可以按引擎、格式和能力选择不同实现，但分派细节不进入前端和公共 API。

## 二、统一资源身份

所有快显 API 使用 `locator` 查询参数：

```text
addp://engine/{engine_id}/path/{resource_path}?type={item_type}&item_id={item_id}
```

`locator` 只负责定位和回跳。稳定去重身份由后端解析 Meta item 后计算：

```text
tenant_id + item_fingerprint
```

前端不得再根据 engine type、schema、table、bucket、path、name 自行拼接快显身份。

## 三、API 清单

### 3.1 快显能力查询

```http
GET /api/v1/manager/quick-view/capability?locator={resource_locator}
```

用途：返回某个 data item 的快显能力、推荐模式、当前偏好、可用渲染出口和空间元数据。

响应示例：

```json
{
  "locator": "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99",
  "item_fingerprint": "sha256...",
  "item": {
    "engine_id": 26,
    "item_id": 99,
    "item_type": "file",
    "data_type": "table",
    "format": "shapefile",
    "full_name": "shp/farmland.shp"
  },
  "spatial": {
    "geometry_columns": ["geometry"],
    "default_geometry_column": "geometry",
    "source_srid": 4326,
    "source_crs": "EPSG:4326",
    "source_crs_definition": null,
    "extent": [120.1, 30.1, 121.1, 31.1],
    "extent_srid": 4326,
    "record_count": 127
  },
  "quick_view": {
    "can_use": true,
    "recommended_mode": "quick_view",
    "active_mode": "quick_view",
    "render_source": "direct_geojson",
    "geojson_url": "/api/v1/manager/quick-view/geojson?locator=...",
    "tile_url_template": null,
    "default_tile_cache_id": null
  },
  "cached_tile": {
    "can_generate": false,
    "ready_count": 0
  }
}
```

第一阶段可以继续返回现有字段，例如 `can_use_quick_view`、`can_generate_tile_cache`、`render_source`、`preferred_mode`，但语义必须与上面的统一契约一致。

### 3.2 快显偏好设置

```http
PATCH /api/v1/manager/quick-view/preferred-mode
Content-Type: application/json
```

请求：

```json
{
  "locator": "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99",
  "preferred_mode": "quick_view"
}
```

`preferred_mode` 允许值：

```text
table_geojson
quick_view
```

偏好保存到 `manager.quick_view`，绑定 `tenant_id + item_fingerprint`。

### 3.3 统一 GeoJSON 快显数据

```http
GET /api/v1/manager/quick-view/geojson?locator={resource_locator}&page=1&page_size=2000&geometry_column={name}
```

响应必须是 GeoJSON FeatureCollection。CRS、分页和 item 信息作为 GeoJSON foreign members 返回。

```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Polygon",
        "coordinates": []
      },
      "properties": {
        "id": 1,
        "name": "farmland"
      }
    }
  ],
  "metadata": {
    "locator": "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99",
    "item_fingerprint": "sha256...",
    "geometry_column": "geometry",
    "source_srid": 4326,
    "source_crs": "EPSG:4326",
    "source_crs_definition": null,
    "transform_status": "not_transformed"
  },
  "pagination": {
    "page": 1,
    "page_size": 127,
    "total": 127
  }
}
```

内部实现示例：

| item 来源 | 内部实现 |
| --- | --- |
| PostGIS table/view | PostGIS SQL + `ST_AsGeoJSON` |
| NFS / MinIO / S3 Shapefile | 读取 multi refs，后端转换为 GeoJSON FeatureCollection |
| GeoJSON 文件 | 读取并规范化为 FeatureCollection |
| GeoPackage | 读取目标 layer，后端转换为 FeatureCollection |

### 3.4 统一 MVT 瓦片快显

```http
GET /api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator={resource_locator}&geometry_column={name}
```

响应：

```http
Content-Type: application/vnd.mapbox-vector-tile
```

建议响应头：

```text
X-ADDP-Render-Source: cached_tile | realtime_tile
X-ADDP-Tile-Cache: HIT | MISS | BYPASS
X-ADDP-Tile-Cache-ID: 123
```

第一阶段动态 MVT 可以只支持 PostGIS table/view。其他引擎如果没有动态 MVT 能力，`capability` 不应推荐 `realtime_tile`；当它们生成了瓦片缓存后，仍通过同一 tile API 读取。

## 四、瓦片缓存任务目标

瓦片缓存任务也必须使用 locator 作为目标，不再暴露 PG 专属字段。

```http
POST /api/v1/manager/tile-cache/tasks
```

请求：

```json
{
  "name": "farmland 瓦片缓存",
  "target": {
    "locator": "addp://engine/26/path/shp/farmland.shp?type=file&item_id=99",
    "geometry_column": "geometry"
  },
  "tile": {
    "format": "mvt",
    "tile_matrix_set": "WebMercatorQuad",
    "target_srid": 3857,
    "min_zoom": 0,
    "max_zoom": 12
  },
  "schedule": ""
}
```

后端根据 locator 解析 item 身份、引擎类型、读取方式和空间元数据。

## 五、公共 API 收敛

快显公共 API 只保留以下 ResourceLocator 入口：

```http
GET /api/v1/manager/quick-view/capability?locator=...
GET /api/v1/manager/quick-view/geojson?locator=...
GET /api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator=...
```

任何需要 `engine_id + schema + table` 作为公共快显身份的 PG 专属入口都必须删除，不得以兼容路由、兼容 query 或前端旁路形式保留。

## 六、实现约束

1. `/quick-view/geojson` 不得返回表格预览结构。
2. `/quick-view/tiles` 不得要求调用方传 `schema/table`。
3. `locator` 必须由后端规范化为 canonical locator；不符合规范的记录按 clean break 删除或拒绝。
4. 后端内部可以复用 preview provider、PostGIS SQL、tile cache repository，但公共 API 只能暴露 locator。
5. Swagger 必须与真实路由同步，旧 path 不得残留。
