# Service 模块 API 测试指南

本文档提供 Service 模块实现的核心 API 的测试方法。

## 前置条件

1. **Service 模块已启动**：确保 Service 后端运行在 `http://localhost:8086`
2. **已发布测试服务**：需要先通过前端创建一个包含空间数据的服务（例如：`test-service`，包含图层 `cities`）
3. **启用协议**：确保测试服务已启用对应的协议开关

---

## 1. WFS 2.0 测试

WFS (Web Feature Service) 是 OGC 标准的矢量要素服务，兼容 QGIS、ArcGIS 等 GIS 客户端。

### 1.1 GetCapabilities - 获取服务元数据

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetCapabilities"
```

**预期响应**：完整的 WFS 2.0.0 Capabilities XML 文档

```xml
<?xml version="1.0" encoding="UTF-8"?>
<wfs:WFS_Capabilities version="2.0.0" ...>
  <ows:ServiceIdentification>
    <ows:Title>Test Service</ows:Title>
    ...
  </ows:ServiceIdentification>
  <FeatureTypeList>
    <FeatureType>
      <Name>cities</Name>
      <Title>城市数据</Title>
      <DefaultCRS>urn:ogc:def:crs:EPSG::4326</DefaultCRS>
      <ows:WGS84BoundingBox>
        <ows:LowerCorner>73.5 18.2</ows:LowerCorner>
        <ows:UpperCorner>135.1 53.6</ows:UpperCorner>
      </ows:WGS84BoundingBox>
    </FeatureType>
  </FeatureTypeList>
</wfs:WFS_Capabilities>
```

### 1.2 DescribeFeatureType - 获取图层字段定义

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=DescribeFeatureType&typeName=cities"
```

**预期响应**：XML Schema 定义

```xml
<?xml version="1.0" encoding="UTF-8"?>
<xsd:schema ...>
  <xsd:element name="cities" type="cities:citiesType" substitutionGroup="gml:AbstractFeature"/>
  <xsd:complexType name="citiesType">
    <xsd:complexContent>
      <xsd:extension base="gml:AbstractFeatureType">
        <xsd:sequence>
          <xsd:element name="id" type="xsd:int" minOccurs="1" maxOccurs="1"/>
          <xsd:element name="name" type="xsd:string" minOccurs="0" maxOccurs="1"/>
          <xsd:element name="population" type="xsd:int" minOccurs="0" maxOccurs="1"/>
          <xsd:element name="geom" type="gml:PointPropertyType" minOccurs="0" maxOccurs="1"/>
        </xsd:sequence>
      </xsd:extension>
    </xsd:complexContent>
  </xsd:complexType>
</xsd:schema>
```

### 1.3 GetFeature - 查询要素数据 ⭐

#### 基础查询（GeoJSON 格式）

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&outputFormat=application/json&count=10"
```

**预期响应**：GeoJSON FeatureCollection

```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "id": 1,
      "geometry": {
        "type": "Point",
        "coordinates": [116.4074, 39.9042]
      },
      "properties": {
        "name": "北京",
        "population": 21540000
      }
    }
  ],
  "numberReturned": 10
}
```

#### GML 格式查询

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&outputFormat=application/gml%2Bxml&count=5"
```

**预期响应**：GML 3.2 FeatureCollection

#### 空间范围过滤（BBOX）

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&bbox=100,30,120,40&count=50"
```

#### 属性过滤（CQL）

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&filter=population>1000000&count=50"
```

#### 分页查询

```bash
# 第一页
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&count=10&startIndex=0"

# 第二页
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&count=10&startIndex=10"
```

#### 指定坐标系

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&srsName=EPSG:3857&count=10"
```

#### 排序

```bash
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&sortBy=population+DESC&count=10"
```

---

## 2. OGC API - Features 测试

OGC API - Features 是现代化的 RESTful 矢量要素服务标准。

### 1.1 获取服务元数据（Landing Page）

```bash
curl http://localhost:8086/ogc/api/test-service/
```

**预期响应**：
```json
{
  "title": "Test Service",
  "description": "...",
  "links": [...]
}
```

### 1.2 查看符合性声明（Conformance）

```bash
curl http://localhost:8086/ogc/api/test-service/conformance
```

**预期响应**：
```json
{
  "conformsTo": [
    "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core",
    "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson"
  ]
}
```

### 1.3 获取图层列表（Collections）

```bash
curl http://localhost:8086/ogc/api/test-service/collections
```

**预期响应**：
```json
{
  "links": [...],
  "collections": [
    {
      "id": "cities",
      "title": "城市数据",
      "extent": {
        "spatial": {
          "bbox": [[73.5, 18.2, 135.1, 53.6]],
          "crs": "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
        }
      },
      "links": [...]
    }
  ]
}
```

### 1.4 获取单个图层元数据（Collection）

```bash
curl http://localhost:8086/ogc/api/test-service/collections/cities
```

### 1.5 查询要素（Items - 核心功能）⭐

#### 基础查询（分页）

```bash
curl "http://localhost:8086/ogc/api/test-service/collections/cities/items?limit=10&offset=0"
```

#### 空间范围过滤（BBOX）

```bash
curl "http://localhost:8086/ogc/api/test-service/collections/cities/items?bbox=100,30,120,40&limit=50"
```

**参数说明**：
- `bbox=minLng,minLat,maxLng,maxLat` - WGS84 坐标系下的边界框

#### 属性过滤（CQL Filter）

```bash
curl "http://localhost:8086/ogc/api/test-service/collections/cities/items?filter=population>1000000&limit=50"
```

#### 指定坐标系（CRS）

```bash
curl "http://localhost:8086/ogc/api/test-service/collections/cities/items?crs=3857&limit=10"
```

**预期响应格式**：
```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "id": 1,
      "geometry": {
        "type": "Point",
        "coordinates": [116.4074, 39.9042]
      },
      "properties": {
        "name": "北京",
        "population": 21540000
      }
    }
  ],
  "numberReturned": 10,
  "links": [
    {"rel": "self", "href": "..."},
    {"rel": "next", "href": "..."}
  ]
}
```

### 1.6 获取单个要素（Item）

```bash
curl http://localhost:8086/ogc/api/test-service/collections/cities/items/1
```

**预期响应**：单个 GeoJSON Feature 对象

---

## 3. WMTS MVT 矢量瓦片测试

WMTS 提供高性能的矢量瓦片服务（Mapbox Vector Tile 格式）。

### 2.1 获取 GetCapabilities

```bash
curl http://localhost:8086/ogc/wmts/test-service
```

**预期响应**：WMTS Capabilities XML 文档

### 2.2 获取 MVT 瓦片⭐

#### 基础请求

```bash
# 北京区域 z=10 瓦片
curl http://localhost:8086/ogc/wmts/test-service/tile/cities/10/851/387.mvt -o tile.mvt
```

**参数说明**：
- `z`: 缩放级别（0-22）
- `x`: 瓦片 X 坐标
- `y`: 瓦片 Y 坐标

#### 验证 MVT 格式

使用 `vt2geojson` 工具（需要安装）：

```bash
# 安装工具
npm install -g @mapbox/vt2geojson

# 转换为 GeoJSON 查看
vt2geojson tile.mvt cities > tile.geojson
```

### 2.3 前端集成示例（Mapbox GL JS）

```html
<script src='https://api.mapbox.com/mapbox-gl-js/v2.15.0/mapbox-gl.js'></script>
<link href='https://api.mapbox.com/mapbox-gl-js/v2.15.0/mapbox-gl.css' rel='stylesheet' />

<script>
const map = new mapboxgl.Map({
  container: 'map',
  style: 'mapbox://styles/mapbox/streets-v11',
  center: [116.4074, 39.9042],
  zoom: 10
});

map.on('load', () => {
  // 添加 MVT 矢量源
  map.addSource('cities', {
    type: 'vector',
    tiles: ['http://localhost:8086/ogc/wmts/test-service/tile/cities/{z}/{x}/{y}.mvt']
  });

  // 添加图层（自定义样式）
  map.addLayer({
    id: 'cities-layer',
    type: 'circle',
    source: 'cities',
    'source-layer': 'cities',
    paint: {
      'circle-radius': 8,
      'circle-color': '#FF0000',
      'circle-stroke-width': 2,
      'circle-stroke-color': '#FFFFFF'
    }
  });
});
</script>
```

### 2.4 性能测试

使用 Apache Bench 测试吞吐量：

```bash
# 测试 100 个请求，10 个并发
ab -n 100 -c 10 "http://localhost:8086/ogc/wmts/test-service/tile/cities/10/851/387.mvt"
```

**预期性能**：
- 首次请求：5-20ms
- 缓存命中：<1ms
- 吞吐量：500+ req/s

---

## 4. REST Query API 测试

简化的 REST 查询 API，支持 JSON/CSV/GeoJSON 格式输出。

### 3.1 基础 JSON 查询

```bash
curl "http://localhost:8086/api/query/test-service/cities?page=1&page_size=50"
```

**预期响应**：
```json
{
  "page": 1,
  "page_size": 50,
  "count": 10,
  "data": [
    {"id": 1, "name": "北京", "population": 21540000, ...}
  ]
}
```

### 3.2 过滤查询

```bash
curl "http://localhost:8086/api/query/test-service/cities?filter=population>1000000&page=1&page_size=50"
```

### 3.3 字段选择

```bash
curl "http://localhost:8086/api/query/test-service/cities?fields=id,name,population&page=1"
```

### 3.4 排序

```bash
curl "http://localhost:8086/api/query/test-service/cities?orderBy=population DESC&page=1"
```

### 3.5 CSV 导出

```bash
curl "http://localhost:8086/api/query/test-service/cities?format=csv" -o cities.csv
```

**预期响应**：CSV 格式文件

### 3.6 GeoJSON 格式（空间数据）

```bash
curl "http://localhost:8086/api/query/test-service/cities?format=geojson&page=1"
```

**预期响应**：
```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "id": 1,
      "properties": {"name": "北京", "population": 21540000},
      "geometry": {"type": "Point", "coordinates": [116.4074, 39.9042]}
    }
  ]
}
```

### 3.7 非空间数据查询

```bash
# 假设有非空间数据图层 "statistics"
curl "http://localhost:8086/api/query/test-service/statistics?format=json&page=1"
```

**注意**：非空间数据图层不能使用 `format=geojson`，会返回错误。

---

## 5. 外部服务代理测试

外部服务代理功能允许通过 ADDP 平台访问第三方 WMS/WFS/WMTS 服务，**隐藏 API Key**，确保安全性和租户隔离。

### 5.1 功能说明

**核心价值**：
- ✅ **隐藏 API Key**：第三方服务的认证信息（API Key、Token、用户名密码）存储在服务端，前端无法访问
- ✅ **租户隔离**：用户只能访问自己租户注册的外部服务
- ✅ **统一访问**：所有外部服务通过统一的代理端点访问
- ✅ **访问统计**：记录外部服务的访问日志，可用于计费和审计

**支持的认证类型**：
- `none` - 无需认证
- `basic` - HTTP Basic Authentication (用户名+密码)
- `bearer` - Bearer Token 认证
- `api_key` - API Key 认证（通过自定义 HTTP Header 传递）

### 5.2 前置条件

1. **已注册外部服务**：通过前端"服务注册"功能或 API 创建外部服务记录
2. **配置认证信息**：在服务注册时填写 `auth_type` 和 `auth_config` 字段

### 5.3 代理端点格式

```
GET /api/service/proxy/{serviceId}/*path
```

**参数说明**：
- `{serviceId}` - 外部服务的 ID（在数据库中的主键）
- `*path` - 目标路径（会拼接到外部服务的 base URL 后面）
- 查询参数会原样转发给目标服务

### 5.4 注册外部服务（示例）

#### 无需认证的 WMS 服务

```bash
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "天地图影像",
    "description": "天地图全球影像服务",
    "service_type": "wmts",
    "url": "http://t0.tianditu.gov.cn/img_c/wmts",
    "auth_type": "none"
  }'
```

**响应示例**：
```json
{
  "id": 1,
  "tenant_id": 1,
  "name": "天地图影像",
  "service_type": "wmts",
  "url": "http://t0.tianditu.gov.cn/img_c/wmts",
  "auth_type": "none",
  "status": "active"
}
```

#### 需要 API Key 的服务

```bash
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "MapBox 矢量瓦片",
    "service_type": "wmts",
    "url": "https://api.mapbox.com/v4/mapbox.mapbox-streets-v8",
    "auth_type": "api_key",
    "auth_config": {
      "header": "Authorization",
      "api_key": "pk.eyJ1IjoiZXhhbXBsZSIsImEiOiJja..."
    }
  }'
```

#### Basic 认证的服务

```bash
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "企业内部 WFS",
    "service_type": "wfs",
    "url": "https://internal-gis.company.com/geoserver/wfs",
    "auth_type": "basic",
    "auth_config": {
      "username": "gis_user",
      "password": "secret_password"
    }
  }'
```

#### Bearer Token 认证的服务

```bash
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "高德 Web API",
    "service_type": "rest",
    "url": "https://restapi.amap.com/v3",
    "auth_type": "bearer",
    "auth_config": {
      "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
    }
  }'
```

### 5.5 通过代理访问外部服务

假设上面注册的"天地图影像"服务 ID 为 1，通过代理访问 GetCapabilities：

```bash
curl "http://localhost:8086/api/service/proxy/1/?service=WMTS&request=GetCapabilities" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

**等价于直接访问**（但前端无需知道 API Key）：
```bash
curl "http://t0.tianditu.gov.cn/img_c/wmts?service=WMTS&request=GetCapabilities"
```

### 5.6 完整测试流程

#### Step 1: 注册外部服务（带 API Key）

```bash
SERVICE_RESPONSE=$(curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "GeoServer WMS",
    "service_type": "wms",
    "url": "https://demo.geo-solutions.it/geoserver/wms",
    "auth_type": "api_key",
    "auth_config": {
      "header": "X-API-Key",
      "api_key": "demo_api_key_12345"
    }
  }')

SERVICE_ID=$(echo $SERVICE_RESPONSE | jq -r '.id')
echo "Service ID: $SERVICE_ID"
```

#### Step 2: 通过代理访问 GetCapabilities

```bash
curl "http://localhost:8086/api/service/proxy/$SERVICE_ID/?service=WMS&request=GetCapabilities" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -o capabilities.xml
```

**验证**：
- 响应应为完整的 Capabilities XML
- 前端/客户端无需知道 `demo_api_key_12345`
- 日志记录了访问行为

#### Step 3: 通过代理获取地图

```bash
curl "http://localhost:8086/api/service/proxy/$SERVICE_ID/?service=WMS&request=GetMap&layers=topp:states&bbox=-124.73142200000001,24.955967,-66.969849,49.371735&width=768&height=330&srs=EPSG:4326&format=image/png" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -o map.png
```

### 5.7 前端集成示例

#### OpenLayers 集成（WMS）

```javascript
import TileLayer from 'ol/layer/Tile';
import TileWMS from 'ol/source/TileWMS';

// 获取 JWT Token
const token = localStorage.getItem('authToken');

// 创建通过代理访问的 WMS 图层
const wmsLayer = new TileLayer({
  source: new TileWMS({
    url: `http://localhost:8086/api/service/proxy/${serviceId}/`,  // 代理端点
    params: {
      'LAYERS': 'topp:states',
      'TILED': true
    },
    serverType: 'geoserver',
    // 添加 JWT 认证头
    tileLoadFunction: function(imageTile, src) {
      const xhr = new XMLHttpRequest();
      xhr.open('GET', src);
      xhr.setRequestHeader('Authorization', `Bearer ${token}`);
      xhr.responseType = 'blob';
      xhr.onload = function() {
        const objectURL = URL.createObjectURL(xhr.response);
        imageTile.getImage().src = objectURL;
      };
      xhr.send();
    }
  })
});

map.addLayer(wmsLayer);
```

#### Fetch API 示例（RESTful 服务）

```javascript
async function queryExternalAPI() {
  const serviceId = 1;  // 注册的外部服务 ID
  const token = localStorage.getItem('authToken');

  const response = await fetch(
    `http://localhost:8086/api/service/proxy/${serviceId}/geocode/regeo?location=116.481488,39.990464`,
    {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    }
  );

  const data = await response.json();
  console.log('Geocode result:', data);
}
```

### 5.8 安全性测试

#### 租户隔离验证

```bash
# 用户 A 的 Token
TOKEN_A="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.A..."

# 用户 B 的 Token
TOKEN_B="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.B..."

# 用户 A 注册服务（假设返回 ID=10）
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Authorization: Bearer $TOKEN_A" \
  -d '{...}'

# 用户 B 尝试访问用户 A 的服务 ID=10
curl "http://localhost:8086/api/service/proxy/10/?..." \
  -H "Authorization: Bearer $TOKEN_B"

# 预期：404 Not Found 或 403 Forbidden（租户隔离生效）
```

#### API Key 隐藏验证

1. 在浏览器 DevTools Network 面板查看请求
2. 验证请求头中**没有**外部服务的 API Key
3. 只有 ADDP 平台的 JWT Token

```bash
# 前端发送的请求（无敏感信息）
GET /api/service/proxy/1/?service=WMTS&request=GetCapabilities
Authorization: Bearer <ADDP_JWT_TOKEN>

# 后端转发给外部服务（包含 API Key）
GET https://api.example.com/wmts?service=WMTS&request=GetCapabilities
X-API-Key: secret_api_key_xyz123  # 前端无法看到
```

### 5.9 错误场景测试

#### 服务 ID 不存在

```bash
curl "http://localhost:8086/api/service/proxy/99999/?service=WMS&request=GetCapabilities" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"

# 预期：404 Not Found
# {"error": "service not found"}
```

#### 外部服务不可达

```bash
# 注册一个无效的 URL
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "Invalid Service",
    "service_type": "wms",
    "url": "https://non-existent-domain-xyz123.com/wms",
    "auth_type": "none"
  }'

# 尝试访问
curl "http://localhost:8086/api/service/proxy/{新服务ID}/?..." \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"

# 预期：502 Bad Gateway
# {"error": "failed to connect to external service"}
```

#### 认证失败

```bash
# 注册带错误 API Key 的服务
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "name": "Service with Wrong Key",
    "service_type": "rest",
    "url": "https://api.example.com",
    "auth_type": "api_key",
    "auth_config": {
      "api_key": "wrong_key"
    }
  }'

# 访问代理
curl "http://localhost:8086/api/service/proxy/{新服务ID}/..." \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"

# 预期：根据外部服务的响应，可能是 401 Unauthorized 或 403 Forbidden
```

### 5.10 访问日志和统计

代理访问会记录审计日志，包括：
- 租户 ID
- 用户 ID
- 外部服务 ID
- 访问路径
- 访问时间
- 响应状态码

**查询访问日志**（需要实现）：
```bash
curl "http://localhost:8086/api/service/registry/services/{serviceId}/access-logs?start_date=2025-01-01&end_date=2025-01-31" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

**预期响应**（未来功能）：
```json
{
  "service_id": 1,
  "service_name": "天地图影像",
  "total_requests": 15234,
  "period": "2025-01-01 to 2025-01-31",
  "daily_stats": [
    {"date": "2025-01-01", "requests": 450},
    {"date": "2025-01-02", "requests": 523}
  ]
}
```

---

## 6. 集成测试建议

### 6.1 Postman 集合

创建 Postman 集合包含所有测试用例：

1. **WFS 2.0**
   - GetCapabilities
   - DescribeFeatureType
   - GetFeature (JSON format)
   - GetFeature (GML format)
   - GetFeature (with bbox)
   - GetFeature (with filter)
   - GetFeature (with pagination)

2. **OGC API - Features**
   - Landing Page
   - Conformance
   - Collections
   - Collection (cities)
   - Items (basic)
   - Items (with bbox)
   - Items (with filter)
   - Item (single feature)

2. **WMTS MVT**
   - GetCapabilities
   - GetTile (multiple zoom levels)

3. **REST Query API**
   - JSON format
   - CSV format
   - GeoJSON format
   - With filters
   - With sorting

### 6.2 QGIS 连接测试（WFS）

1. 打开 QGIS 3.28+
2. 图层 → 添加图层 → 添加 WFS 图层
3. 新建连接：
   - 名称：ADDP Test Service (WFS)
   - URL：`http://localhost:8086/ogc/wfs/test-service`
4. 验证能否加载图层列表并显示数据

### 6.3 QGIS 连接测试（OGC API）

1. 打开 QGIS 3.28+
2. 图层 → 添加图层 → 添加 WFS 图层（QGIS 3.28+ 自动支持 OGC API - Features）
3. 新建连接：
   - 名称：ADDP Test Service (OGC API)
   - URL：`http://localhost:8086/ogc/api/test-service`
4. 验证能否加载图层列表并显示数据

### 6.4 前端集成测试

#### OpenLayers 示例

```javascript
import VectorTileLayer from 'ol/layer/VectorTile';
import VectorTileSource from 'ol/source/VectorTile';
import MVT from 'ol/format/MVT';

const layer = new VectorTileLayer({
  source: new VectorTileSource({
    format: new MVT(),
    url: 'http://localhost:8086/ogc/wmts/test-service/tile/cities/{z}/{x}/{y}.mvt'
  })
});

map.addLayer(layer);
```

---

## 7. 错误场景测试

### 7.1 服务不存在

```bash
curl http://localhost:8086/ogc/api/nonexistent-service/collections
# 预期：404 Not Found
```

### 7.2 图层不存在

```bash
curl http://localhost:8086/ogc/api/test-service/collections/nonexistent-layer
# 预期：404 Not Found
```

### 7.3 协议未启用

```bash
# 如果服务未启用 OGC API
curl http://localhost:8086/ogc/api/test-service/collections
# 预期：403 Forbidden, {"error": "OGC API Features is not enabled for this service"}

# 如果服务未启用 WFS
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetCapabilities"
# 预期：403 Forbidden, {"error": "WFS is not enabled for this service"}
```

### 7.4 非空间数据请求 WMTS

```bash
# 假设 "statistics" 图层无几何列
curl http://localhost:8086/ogc/wmts/test-service/tile/statistics/10/851/387.mvt
# 预期：400 Bad Request, {"error": "Layer does not contain spatial data"}
```

### 7.5 参数错误

```bash
# 无效的 BBOX 格式
curl "http://localhost:8086/ogc/api/test-service/collections/cities/items?bbox=invalid"
# 预期：400 Bad Request, {"error": "Invalid bbox parameter"}

# 无效的缩放级别
curl http://localhost:8086/ogc/wmts/test-service/tile/cities/99/0/0.mvt
# 预期：400 Bad Request, {"error": "Zoom level must be between 0 and 22"}

# WFS 缺少必需参数
curl "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature"
# 预期：400 Bad Request, {"error": "Missing required parameter: typeName"}
```

---

## 8. 性能基准测试

### 8.1 WFS GetFeature

```bash
# 目标：100 req/s, 平均延迟 <150ms
ab -n 1000 -c 10 "http://localhost:8086/ogc/wfs/test-service?service=WFS&request=GetFeature&typeName=cities&outputFormat=application/json&count=50"
```

### 8.2 OGC API - Features

```bash
# 目标：100 req/s, 平均延迟 <100ms
ab -n 1000 -c 10 "http://localhost:8086/ogc/api/test-service/collections/cities/items?limit=50"
```

### 8.3 WMTS MVT

```bash
# 目标：500 req/s, 平均延迟 <20ms（缓存命中 <1ms）
ab -n 5000 -c 50 "http://localhost:8086/ogc/wmts/test-service/tile/cities/10/851/387.mvt"
```

### 8.4 REST Query API

```bash
# 目标：200 req/s, 平均延迟 <200ms
ab -n 1000 -c 10 "http://localhost:8086/api/query/test-service/cities?page=1&page_size=50"
```

---

## 9. 故障排查

### 9.1 500 Internal Server Error

**检查日志**：
```bash
tail -f logs/service-backend.log
```

**常见原因**：
- 数据库连接失败
- SQL 语法错误
- 几何数据格式问题

### 9.2 空响应或 204 No Content（WMTS）

**原因**：查询的瓦片区域无数据

**解决**：
- 检查数据范围是否覆盖请求的瓦片区域
- 降低缩放级别（z）尝试

### 9.3 几何数据未正确返回

**检查**：
- `GeometryColumn` 字段是否正确配置
- PostGIS `ST_AsGeoJSON` 函数是否可用
- 坐标系转换是否成功

### 9.4 WFS 客户端连接失败

**常见原因**：
- 服务未启用 WFS 协议开关
- URL 格式错误（应为：`http://localhost:8086/ogc/wfs/服务名`）
- Capabilities 文档 XML 格式错误

**解决**：
- 检查 Service 后端日志：`tail -f logs/service-backend.log`
- 直接访问 GetCapabilities 验证 XML 格式
- 确认图层有 geometry_column 配置

---

## 10. 下一步

P0+P1 功能测试通过后，可以继续实施 P2 功能：

- **P2**: 样式管理系统、WMS 实现、WMTS 栅格瓦片

更多详细信息请参考计划文档：`/Users/pampa/.claude/plans/expressive-herding-cerf.md`
