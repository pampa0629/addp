# internal_services 表结构和 API 说明

## 一、表结构概览

`service.internal_services` 表是 Service 模块的内部服务发布表,负责将 ADDP 平台内部的空间数据发布为符合 OGC 标准的服务(WFS、OGC API Features、WMTS、WMS 等)。与 `internal_service_layers` 表是 1:N 关系。

### 核心功能

- **OGC 服务发布**:将 PostgreSQL 中的空间表发布为 WFS、OGC API Features 等标准服务
- **多协议支持**:同一服务可同时支持多种 OGC 协议
- **元数据管理**:维护服务的标题、摘要、关键词、联系方式等元数据
- **服务配置**:支持自定义 SRID、最大要素数等服务参数
- **租户隔离**:每个服务仅对所属租户可见

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 服务唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID(隔离) |
| `service_name` | VARCHAR(255) | NOT NULL, UNIQUE | 服务名称(英文标识符,用于 URL) |
| `title` | VARCHAR(255) | NOT NULL | 服务标题(显示名称) |
| `abstract` | TEXT | | 服务摘要/描述 |
| `keywords` | TEXT[] | | 服务关键词数组 |
| `enabled_wfs` | BOOLEAN | DEFAULT true | 是否启用 WFS 协议 |
| `enabled_ogc_api` | BOOLEAN | DEFAULT true | 是否启用 OGC API Features 协议 |
| `enabled_wmts` | BOOLEAN | DEFAULT true | 是否启用 WMTS 协议 |
| `enabled_wms` | BOOLEAN | DEFAULT false | 是否启用 WMS 协议 |
| `default_srid` | INTEGER | DEFAULT 4326 | 默认空间参考系统 |
| `max_features` | INTEGER | DEFAULT 1000 | 单次查询最大要素数 |
| `provider_name` | VARCHAR(255) | | 服务提供者名称 |
| `provider_site` | VARCHAR(255) | | 服务提供者网站 |
| `contact_person` | VARCHAR(255) | | 联系人 |
| `contact_email` | VARCHAR(255) | | 联系邮箱 |
| `engine_id` | INTEGER | NOT NULL, INDEXED | 关联的存储引擎 ID |
| `status` | VARCHAR(20) | DEFAULT 'active' | 服务状态:'active', 'inactive', 'error' |
| `created_by` | INTEGER | NOT NULL | 创建者用户 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |
| `deleted_at` | TIMESTAMP | | 软删除时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_internal_service_tenant` | tenant_id | 租户级查询 |
| `idx_internal_service_unique` | service_name | 服务名称唯一性 |
| `idx_internal_service_engine` | engine_id | 按存储引擎查询 |

### 2.3 外键约束

| 外键字段 | 引用表 | 引用字段 | 约束 |
|---------|--------|---------|------|
| `engine_id` | `system.storage_engines` | `id` | - |

### 2.4 关联关系

| 关系类型 | 关联表 | 关系说明 |
|---------|--------|---------|
| 1:N | `internal_service_layers` | 一个服务包含多个图层 |

---

## 三、字段详细说明

### 3.1 service_name (服务标识符)

**用途**: 用于构建服务访问 URL

**命名规则**:
- 仅允许字母、数字(a-z, A-Z, 0-9)
- 长度: 1-255 字符
- 示例: `city_poi`, `building_data`, `land_use`

**URL 示例**:
```
WFS: http://api.example.com/ogc/wfs/city_poi?service=WFS&request=GetCapabilities
OGC API: http://api.example.com/ogc/api/city_poi/collections
WMTS: http://api.example.com/ogc/wmts/city_poi?service=WMTS&request=GetCapabilities
```

### 3.2 协议启用标志

| 字段 | 协议 | 说明 | 典型用途 |
|-----|------|------|---------|
| `enabled_wfs` | WFS 2.0 | 矢量要素服务 | 下载矢量数据、空间查询 |
| `enabled_ogc_api` | OGC API Features | 现代 RESTful API | Web 应用集成、JSON 输出 |
| `enabled_wmts` | WMTS 1.0 | 切片地图服务 | 快速地图显示 |
| `enabled_wms` | WMS 1.3 | 地图图像服务 | 动态地图渲染 |

**注意**: 可同时启用多种协议,系统会根据协议自动生成对应端点。

### 3.3 default_srid (默认坐标系统)

**支持的 SRID**:
- `4326` - WGS84 (经纬度)
- `3857` - Web Mercator (Web 地图)
- `4490` - CGCS2000 (中国大地坐标系)
- `2000` - 北京 54
- `4214` - 西安 80

**作用**: 当客户端未指定坐标系时使用此值。

### 3.4 max_features (最大要素数)

**作用**: 限制单次请求返回的要素数量,防止大数据集拖垮系统。

**取值范围**: 1-10000
**默认值**: 1000
**建议值**:
- 小数据集(< 1万条): 5000
- 中等数据集(1-10万条): 1000
- 大数据集(> 10万条): 500

### 3.5 status (服务状态)

| 值 | 说明 | 何时出现 |
|----|------|---------|
| `active` | 正常运行 | 服务正常发布 |
| `inactive` | 已禁用 | 用户手动停用 |
| `error` | 错误状态 | 数据源不可用、配置错误等 |

---

## 四、API 端点

### 4.1 POST /api/service/internal/services - 创建内部服务

**请求体**:
```json
{
  "service_name": "city_poi",
  "title": "城市兴趣点数据服务",
  "abstract": "提供城市兴趣点(POI)数据的 OGC 标准服务",
  "keywords": ["POI", "城市", "兴趣点"],
  "enabled_wfs": true,
  "enabled_ogc_api": true,
  "enabled_wmts": false,
  "enabled_wms": false,
  "default_srid": 4326,
  "max_features": 1000,
  "provider_name": "ADDP 数据平台",
  "contact_email": "support@example.com",
  "engine_id": 1
}
```

**响应**(201 Created):
```json
{
  "code": 200,
  "message": "内部服务创建成功",
  "data": {
    "id": 1,
    "tenant_id": 1,
    "service_name": "city_poi",
    "title": "城市兴趣点数据服务",
    "abstract": "提供城市兴趣点(POI)数据的 OGC 标准服务",
    "keywords": ["POI", "城市", "兴趣点"],
    "enabled_wfs": true,
    "enabled_ogc_api": true,
    "enabled_wmts": false,
    "enabled_wms": false,
    "default_srid": 4326,
    "max_features": 1000,
    "engine_id": 1,
    "status": "active",
    "created_by": 1,
    "created_at": "2026-01-31T10:00:00Z",
    "updated_at": "2026-01-31T10:00:00Z",
    "layers": []
  }
}
```

---

### 4.2 GET /api/service/internal/services/:id - 获取服务详情

**响应**(200 OK):
```json
{
  "code": 200,
  "message": "获取成功",
  "data": {
    "id": 1,
    "service_name": "city_poi",
    "title": "城市兴趣点数据服务",
    "layers": [
      {
        "id": 1,
        "layer_name": "poi_restaurants",
        "title": "餐饮兴趣点",
        "geometry_types": ["Point"],
        "srid": 4326,
        "enabled": true
      }
    ]
  }
}
```

---

### 4.3 GET /api/service/internal/services - 列出服务

**查询参数**:
- `page`: 页码(默认 1)
- `page_size`: 每页大小(默认 20,最大 100)
- `status`: 按状态过滤('active', 'inactive', 'error')

**响应**(200 OK):
```json
{
  "code": 200,
  "message": "查询成功",
  "data": {
    "items": [
      {
        "id": 1,
        "service_name": "city_poi",
        "title": "城市兴趣点数据服务",
        "status": "active",
        "layer_count": 3,
        "created_at": "2026-01-31T10:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 20
  }
}
```

---

### 4.4 PUT /api/service/internal/services/:id - 更新服务

**请求体**(仅更新提供的字段):
```json
{
  "title": "更新后的标题",
  "enabled_wms": true,
  "max_features": 2000
}
```

**响应**(200 OK):
```json
{
  "code": 200,
  "message": "更新成功",
  "data": {
    "id": 1,
    "title": "更新后的标题",
    "enabled_wms": true,
    "max_features": 2000
  }
}
```

---

### 4.5 DELETE /api/service/internal/services/:id - 删除服务

**响应**(204 No Content)

**注意**:软删除,关联的 `internal_service_layers` 记录会级联删除(ON DELETE CASCADE)。

---

## 五、OGC 服务端点

创建内部服务后,系统会自动生成以下 OGC 端点:

### 5.1 WFS (Web Feature Service)

```bash
# GetCapabilities
GET /ogc/wfs/{service_name}?service=WFS&request=GetCapabilities&version=2.0.0

# DescribeFeatureType
GET /ogc/wfs/{service_name}?service=WFS&request=DescribeFeatureType&typeName={layer_name}

# GetFeature
GET /ogc/wfs/{service_name}?service=WFS&request=GetFeature&typeName={layer_name}&count=100
```

### 5.2 OGC API Features

```bash
# Landing Page
GET /ogc/api/{service_name}/

# Collections
GET /ogc/api/{service_name}/collections

# Collection Metadata
GET /ogc/api/{service_name}/collections/{layer_name}

# Items
GET /ogc/api/{service_name}/collections/{layer_name}/items?limit=100
```

### 5.3 WMTS (Web Map Tile Service)

```bash
# GetCapabilities
GET /ogc/wmts/{service_name}?service=WMTS&request=GetCapabilities&version=1.0.0

# GetTile
GET /ogc/wmts/{service_name}?service=WMTS&request=GetTile&layer={layer_name}&tilematrixset=EPSG:3857&tilematrix={z}&tilerow={y}&tilecol={x}
```

---

## 六、业务场景示例

### 场景 1: 发布 POI 数据为 WFS 服务

**步骤**:
1. 创建服务(仅启用 WFS)
2. 添加图层(指向 PostgreSQL 表)
3. 客户端通过 WFS 协议访问

**应用**: GIS 软件(QGIS、ArcGIS)直接加载矢量图层。

---

### 场景 2: 为 Web 地图提供瓦片服务

**步骤**:
1. 创建服务(启用 WMTS)
2. 配置图层样式
3. 前端通过 OpenLayers/Leaflet 加载瓦片

**应用**: Web 地图快速显示大数据集。

---

### 场景 3: 提供 RESTful API 供前端调用

**步骤**:
1. 创建服务(启用 OGC API Features)
2. 前端通过 JSON API 查询要素
3. 支持空间过滤、属性查询

**应用**: 现代 Web 应用集成。

---

## 七、设计决策

### 7.1 为什么使用 TEXT[] 存储 keywords?

**优点**:
- PostgreSQL 原生数组类型,查询高效
- 支持 `@>` 运算符进行数组包含查询
- 无需额外关联表

**示例查询**:
```sql
-- 查找包含 "POI" 关键词的服务
SELECT * FROM internal_services WHERE keywords @> ARRAY['POI'];
```

### 7.2 为什么 service_name 要求字母数字?

**原因**:
- 用于构建 URL,需要 URL 安全
- 避免特殊字符引起路由问题
- 保持简洁和可读性

### 7.3 为什么默认不启用 WMS?

**原因**:
- WMS 需要样式配置(SLD),较复杂
- WMTS 性能更好(预渲染瓦片)
- 用户需要时可手动启用

---

## 八、性能优化建议

### 8.1 合理设置 max_features

**避免**:设置过大(> 5000),可能导致:
- 查询缓慢
- 内存占用高
- 客户端渲染卡顿

**建议**:根据数据量动态调整。

### 8.2 使用空间索引

**前提**:发布的 PostgreSQL 表必须有空间索引:
```sql
CREATE INDEX idx_geom ON your_table USING GIST (geom);
```

---

## 九、安全机制

### 9.1 租户隔离

- 每个服务仅对所属租户可见
- API 自动过滤 `tenant_id`

### 9.2 权限控制

- 只有租户管理员可创建/修改服务
- SuperAdmin 可跨租户管理

### 9.3 数据访问控制

- 服务只能访问其 `engine_id` 对应的存储引擎
- 图层只能引用该引擎下的表

---

## 十、相关文档

- [internal_service_layers表](./internal_service_layers表.md) - 内部服务图层表
- [数据库架构](../数据库架构.md) - Service 模块架构
- [Service 模块说明](../CLAUDE.md) - 模块整体说明
