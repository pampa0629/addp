# Service 模块核心概念和架构设计

## 一、模块定位

Service 模块是 ADDP 全域数据平台的**数据服务发布模块**，负责将内部数据和外部服务以标准化、可访问的方式发布为 API 服务。

**核心功能**：
- **内部数据发布**：将数据库表或 SQL 查询结果发布为 REST API、OGC API(Features Tiles),即查询服务和地图（瓦片）服务
- **外部服务注册**：集成第三方 OGC 服务（WMS、WFS、WMTS）、XYZ 瓦片服务、REST API
- **多协议支持**：同时支持现代 REST API 和 OGC 标准协议（Features、Tiles）
- **统一访问入口**：通过 Gateway 提供统一的服务访问端点，支持公开访问和认证访问

---

## 二、服务分类

### 2.1 服务分类总览

Service 模块将服务分为三大类：

| 服务类型 | 英文名称 | 用途 | 数据源 | 协议支持 |
|---------|---------|------|--------|---------|
| **查询服务** | Query Service | 数据查询、导出、API发布 | 表/SQL | REST API、OGC Features |
| **瓦片服务** | Tile Service | 地图可视化、瓦片服务 | 表/静态瓦片 | XYZ、OGC Tiles、TMS |
| **注册服务** | Registered Service | 集成外部服务 | 外部URL | 代理转发 |

**术语说明**：
- 后端/数据库使用：`tile_services`（准确描述技术实现）
- 前端界面显示：**地图服务**（用户友好）
- 本文档统一使用：**瓦片服务**（技术规范）

### 2.2 查询服务 (Query Service)

#### 定位
针对**单数据源**（一张表或一条 SQL），提供数据查询和导出服务。

#### 两种配置方式（互斥）

**方式一：界面配置（Table 模式）**
- 通过资源树选择器选择资源
- 自动检测空间字段（调用 Meta resource-tree / item 能力）
- 支持配置默认返回字段和可过滤字段
- 对外只接受结构化字段选择、类型化过滤、排序和游标分页，不接受 SQL 片段

**方式二：SQL 配置（SQL 模式）**
- 编写自定义 SQL 查询语句
- 新建服务选择查询引擎时，默认 SQL 从该引擎当前业务 Catalog 中选择有数据的真实表生成；返回表单前必须通过同一只读执行链路以最多 10 行完成验证
- 默认 SQL 是不含 `LIMIT/OFFSET` 的基础查询，发布后由 Service 在外层统一追加分页；工作台样例中的展示行数限制不属于查询服务定义
- 手动指定空间字段配置
- 固定 SQL 只作为发布来源；对外查询仍通过输出契约上的结构化查询能力执行
- 发布前必须通过所选查询引擎真实检测输出契约；DuckDB 联邦 SQL 在完成授权和挂载后使用 Runtime describe 能力取得字段，发布者从实际输出字段中声明非空唯一稳定键
- 不支持客户端提交 SQL、WHERE、ORDER BY 或其他原生表达式

两种来源必须形成同一个发布契约：输出字段、空间信息、非空唯一稳定排序键、查询字段策略、资源限制、执行绑定和依赖快照。发布版本不可变；修改契约生成新版本并原子切换。

#### 协议支持

**REST API（默认启用）**
- 端点：`POST /api/query/{serviceName}/query`
- 请求体：`select`、结构化 `filter`、`order_by`、`page.limit`、`page.cursor`、`format`
- 输出格式：JSON、CSV、GeoJSON（有空间字段时）

**OGC API Features（自动启用）**
- 启用条件：检测到空间字段
- 端点：`/ogc/features/{serviceName}/collections/{collectionId}/items`
- 符合 OGC API Features 1.0 标准

### 2.3 瓦片服务 (Tile Service)

#### 定位
针对**多图层地图**，提供矢量瓦片和栅格瓦片服务。

#### 图层类型

**动态图层 (layer_type='dynamic')**
- 数据源：通过 ResourceLocator 选择的空间表；`engine_id/schema/table` 只作为后端解析 locator 后形成的执行快照
- 工作原理：实时查询数据库生成瓦片
- 支持格式：MVT（矢量）
- 缓存策略：可选启用瓦片缓存到 MinIO

**静态图层 (layer_type='static')**
- 数据源：通过资源树选择 Business 存储中 `data_type=media + format=pmtiles + layout=single` 的业务 item
- 工作原理：发布时由 Meta 校验 item 并冻结 PMTiles v3 header 摘要、层级、范围和源指纹快照；请求时通过 System engine provider Range Read 业务 PMTiles
- 支持格式：PMTiles v3 + gzip MVT；由 Meta 自动识别，不在表单中手工指定
- 地图初始视角：必须优先按发布快照中的 `capabilities.spatial.extent` 全幅显示数据，并使用 PMTiles 层级范围约束交互；范围不可用时才回退到 `center`，不得硬编码某个地区
- 空瓦片：合法坐标在 PMTiles 中没有目录项时返回 gzip 编码的空 MVT 和 HTTP 200；存储读取或归档校验失败仍返回错误
- 禁止输入裸存储路径、URL 模板或 Manager infra `storage_ref`

#### 协议支持

| 协议 | 端点格式 | 说明 |
|------|---------|------|
| **XYZ Tiles** | `/tiles/{serviceName}/{layerName}/{z}/{x}/{y}.{format}` | 最常用的瓦片规范 |
| **OGC Tiles API** | `/ogc/tiles/{serviceName}/...` | OGC 标准瓦片API |
| **TMS** | `/tms/{serviceName}/{layerName}/{z}/{x}/{y}.{format}` | OSGeo标准，Y轴相反 |

所有协议可同时启用，支持多种格式（mvt/png/jpeg）。

#### 瓦片缓存策略

**动态图层缓存**：
1. 勾选 `tile_cache_enabled`，设置 `tile_cache_ttl`
2. 首次请求：查询数据库 → 生成瓦片 → 保存到 MinIO → 返回
3. 后续请求：检查缓存 → 未过期直接返回，过期则重新生成
4. 缓存路径：`service/{service_id}/{layer_id}/{z}/{x}/{y}.{format}`
5. 支持手动清理：`DELETE /api/service/tile/{serviceId}/layers/{layerId}/cache`

### 2.4 注册服务 (Registered Service)

#### 定位
集成外部第三方服务，提供统一的访问入口。

#### 支持的服务类型

| 服务类型 | 说明 | 图层支持 |
|---------|------|---------|
| **WMS** | Web Map Service | 多图层 |
| **WFS** | Web Feature Service | 多图层 |
| **WMTS** | Web Map Tile Service | 多图层 |
| **OGC API** | OGC API Features/Tiles | 多图层 |
| **XYZ Tiles** | 通用XYZ瓦片服务 | 单图层 |
| **REST API** | 自定义REST API | 单端点 |

#### 注册流程

**OGC 服务（WMS/WFS/WMTS）**：
1. 用户输入服务端点 URL
2. 系统自动发送 GetCapabilities 请求
3. 解析响应，提取服务元数据和图层列表
4. 保存到 `registered_services` 和 `registered_service_layers` 表
5. 定期健康检查

**XYZ 瓦片服务**：
1. 用户输入瓦片 URL 模板（如：`http://example.com/{z}/{x}/{y}.png`）
2. 用户提供 TileJSON 元数据（缩放级别、边界框等）
3. 测试瓦片可访问性

#### 代理转发

**端点**：`GET /api/service/proxy/:serviceId/*path`

**作用**：
- 统一认证：平台统一管理外部服务认证
- 跨域解决：避免前端跨域问题
- 访问控制：在代理层面控制访问权限
- 日志记录：记录外部服务调用日志

---

## 三、数据库设计

### 3.1 表结构总览

| 表名 | 说明 | Schema |
|------|------|--------|
| `query_services` | 查询服务 | service |
| `tile_services` | 瓦片服务 | service |
| `tile_service_layers` | 瓦片服务图层 | service |
| `registered_services` | 注册服务 | service |
| `registered_service_layers` | 注册服务图层 | service |

### 3.2 查询服务表 (query_services)

```sql
CREATE TABLE service.query_services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES system.tenants(id) ON DELETE CASCADE,
    service_name VARCHAR(255) NOT NULL,          -- 服务唯一标识
    title VARCHAR(255) NOT NULL,                  -- 服务标题
    description TEXT,
    keywords VARCHAR(255)[],

    -- 配置方式（互斥）
    config_type VARCHAR(50) NOT NULL CHECK (config_type IN ('table', 'sql')),

    -- Source Engine 与联邦查询 Runtime（按 config_type 和源类型显式互斥）
    engine_id BIGINT REFERENCES system.engines(id) ON DELETE RESTRICT,
    runtime_engine_id BIGINT REFERENCES system.engines(id) ON DELETE RESTRICT,

    -- Table 模式字段
    schema_name VARCHAR(255),
    table_name VARCHAR(255),

    -- SQL 模式字段
    sql_query TEXT,

    -- 数据配置（JSONB）
    data_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    /* data_config 结构：
    {
      "source_snapshot": {                        // Service 执行依赖快照
        "source": {
          "item_id": 33,
          "item_fingerprint": "...",
          "scanned_at": "2026-07-14T08:00:00Z",
          "data_updated_at": "2026-07-14T07:30:00Z"
        },
        "captured_at": "2026-07-14T08:05:00Z",
        "dependency_hash": "...",
		"verification_status": "verified",
        "table": {"fields": [], "primary_key": []},
        "spatial": {
          "geometry_columns": [{"name": "geom", "geometry_type": "Point", "srid": 4326, "crs_ref": "EPSG:4326"}],
          "primary_geometry_column": "geom",
          "crs_definitions": []
		},
		"federated_object_tables": {
		  "lake": {"public.sales": "bucket/public/sales.parquet"}
		},
		"federated_source_engine_ids": [9]
      },
      "default_fields": ["id", "name", "geom"],  // 默认返回字段（Table模式）
      "filterable_fields": ["name", "category"]  // 可过滤字段（Table模式）
    }
    */

    -- 协议配置
    protocols JSONB NOT NULL DEFAULT '{
        "rest_api": {"enabled": true, "formats": ["json", "csv", "geojson"]},
        "ogc_features": {"enabled": false, "version": "1.0"}
    }'::jsonb,

    -- 访问控制
    public_access BOOLEAN DEFAULT FALSE,
    max_features INTEGER DEFAULT 1000,

    -- 状态
    status VARCHAR(50) DEFAULT 'active',
    error_message TEXT,

    -- 审计字段
    created_by UUID NOT NULL REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_query_service_name UNIQUE (tenant_id, service_name)
);
```

**核心字段说明**：
- `config_type`: 'table' 或 'sql'，创建后不可修改
- `engine_id`: 普通 SQL 或表服务的 Source Engine；联邦 SQL 必须为空。
- `runtime_engine_id`: 联邦 SQL 的 DuckDB Runtime Engine；Parquet 对象表同时保存 Source `engine_id` 和该字段；关系表必须为空。
- `data_config`: JSONB 字段，存储 locator、Service 依赖快照、默认字段和可过滤字段。依赖快照只保存执行所需事实，不复制完整 Meta attributes。
- `dependency_hash`: 只计算字段、主键、空间字段/CRS、对象表执行描述符、联邦 Source Engine ID 和 SQL query hash；排除行数、大小、扫描时间、数据更新时间、extent 和空间索引状态。
- `verification_status`: 新发布或显式刷新的快照为 `verified`；一次性迁移的历史快照为 `unverifiable`，需通过管理动作重新检查或刷新。
- DuckDB 联邦 SQL 在发布时冻结查询实际引用的 Source Engine ID，并只保存实际引用的对象表映射；两者都纳入 `dependency_hash`。执行时按冻结 ID 从 System 获取当前连接配置，不在请求期按名称重新绑定 Engine，也不再调用 Meta；数据源绑定或对象表映射变化需要重新发布查询服务。
- 历史表模式记录只有在 locator 能定位同租户且具有 fingerprint 的 Meta item 时才迁移；无法建立可靠源身份的旧记录在迁移时删除，需重新创建。
- 历史 DuckDB 联邦 SQL 无法仅靠旧几何配置节点还原对象表依赖，迁移时直接删除，需重新创建；运行时不为旧记录恢复 Meta 动态解析分支。
- `protocols`: 协议配置，默认启用 REST API
- `public_access`: 公开访问开关；关闭时要求同租户 User Access Token，Service 通过 System AuthContext 校验，不自行解析 Token

### 3.3 瓦片服务表 (tile_services)

```sql
CREATE TABLE service.tile_services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES system.tenants(id) ON DELETE CASCADE,
    service_name VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    keywords VARCHAR(255)[],

    -- 瓦片配置
    default_srid INTEGER DEFAULT 3857,            -- Web Mercator
    extent JSONB,                                 -- 所有图层的联合边界

    -- 协议配置
    protocols JSONB NOT NULL DEFAULT '{
        "xyz": {"enabled": true, "formats": ["mvt"]},
        "ogc_tiles": {"enabled": true, "version": "1.0"},
        "tms": {"enabled": false}
    }'::jsonb,

    -- 访问控制
    public_access BOOLEAN DEFAULT FALSE,

    -- 状态
    status VARCHAR(50) DEFAULT 'active',
    error_message TEXT,

    -- 审计字段
    created_by UUID NOT NULL REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_tile_service_name UNIQUE (tenant_id, service_name)
);
```

### 3.4 瓦片服务图层表 (tile_service_layers)

```sql
CREATE TABLE service.tile_service_layers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id UUID NOT NULL REFERENCES service.tile_services(id) ON DELETE CASCADE,
    layer_name VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,

    -- 图层类型
    layer_type VARCHAR(50) NOT NULL CHECK (layer_type IN ('dynamic', 'static')),

    -- 图层配置（JSONB）
    layer_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    /* layer_config 结构（动态图层）：
    {
      "source": {
        "engine_id": "uuid",
        "schema": "public",
        "table": "beijing_roads",
        "geometry_column": "geom",
        "srid": 4326
      },
      "mvt": {
        "buffer": 0,
        "extent": 4096,
        "simplify_tolerance": 1.0
      },
      "cache": {
        "enabled": true,
        "ttl": 3600
      },
      "style": {...}
    }

    layer_config 结构（静态图层）：
    {
      "source": {
        "locator": "addp://engine/9/path/addp/tiles/roads?type=object&item_id=101",
        "engine_id": 9,
        "item_id": 101
      },
      "source_snapshot": {
        "fingerprint": "...",
        "scope_path": "addp/tiles/roads.pmtiles",
        "archive_format": "pmtiles",
        "spec_version": 3,
        "header_hash": "...",
        "tile_format": "mvt",
        "tile_compression": "gzip",
        "min_zoom": 4,
        "max_zoom": 18,
        "center": [116.4, 39.9, 8],
        "spatial": {
          "srid": 4326,
          "crs_ref": "EPSG:4326",
          "extent": [116.1, 39.7, 116.7, 40.1]
        }
      }
    }
    */

    -- 显示配置
    display_order INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT TRUE,

    -- 审计字段
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_tile_layer_name UNIQUE (service_id, layer_name)
);
```

**核心字段说明**：
- `layer_type`: 'dynamic'（动态表）或 'static'（静态瓦片）
- `layer_config`: JSONB 字段，根据图层类型存储不同配置

### 3.5 注册服务表 (registered_services)

```sql
CREATE TABLE service.registered_services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES system.tenants(id) ON DELETE CASCADE,
    service_name VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    keywords VARCHAR(255)[],

    -- 服务类型
    service_type VARCHAR(50) NOT NULL CHECK (service_type IN
        ('wms', 'wfs', 'wmts', 'ogc_api', 'xyz', 'rest')),

    -- 服务端点
    endpoint_url TEXT NOT NULL,

    -- 元数据（GetCapabilities 解析结果）
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 认证配置
    auth_type VARCHAR(50) DEFAULT 'none' CHECK (auth_type IN
        ('none', 'basic', 'bearer', 'api_key')),
    auth_config JSONB DEFAULT '{}'::jsonb,

    -- 健康检查
    health_check_url TEXT,
    last_checked_at TIMESTAMP WITH TIME ZONE,

    -- 状态
    status VARCHAR(50) DEFAULT 'active',
    error_message TEXT,

    -- 审计字段
    created_by UUID NOT NULL REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_registered_service_name UNIQUE (tenant_id, service_name)
);
```

### 3.6 注册服务图层表 (registered_service_layers)

```sql
CREATE TABLE service.registered_service_layers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id UUID NOT NULL REFERENCES service.registered_services(id) ON DELETE CASCADE,
    layer_name VARCHAR(255) NOT NULL,            -- 从 GetCapabilities 解析
    display_name VARCHAR(255),
    description TEXT,

    -- 几何信息
    geometry_type VARCHAR(50),
    crs VARCHAR(50),
    bbox JSONB,

    -- 图层元数据
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 控制
    enabled BOOLEAN DEFAULT TRUE,

    -- 审计字段
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_registered_layer_name UNIQUE (service_id, layer_name)
);
```

---

## 四、API 端点设计

### 4.1 API 路由总览

```
# 查询服务端点
POST /api/query/:serviceName/query                      → REST 查询 API
GET /ogc/features/:serviceName/collections/:collectionId/items  → OGC Features

# 瓦片服务端点
GET /tiles/:serviceName/:layerName/:z/:x/:y.:format      → XYZ Tiles
GET /tms/:serviceName/:layerName/:z/:x/:y.:format        → TMS
GET /ogc/tiles/:serviceName/:layerName/:z/:x/:y         → OGC Tiles

# 查询服务管理
POST   /api/v1/service/query                             → 创建查询服务
GET    /api/v1/service/query                             → 列出查询服务
GET    /api/v1/service/query/:id                         → 获取查询服务详情
PUT    /api/v1/service/query/:id                         → 更新查询服务
DELETE /api/v1/service/query/:id                         → 删除查询服务

# 瓦片服务管理
POST   /api/service/tile                                 → 创建瓦片服务
GET    /api/service/tile                                 → 列出瓦片服务
GET    /api/service/tile/:id                             → 获取瓦片服务详情
PUT    /api/service/tile/:id                             → 更新瓦片服务
DELETE /api/service/tile/:id                             → 删除瓦片服务
POST   /api/service/tile/:id/layers                      → 添加图层
PUT    /api/service/tile/:id/layers/:layerId             → 更新图层
DELETE /api/service/tile/:id/layers/:layerId             → 删除图层
DELETE /api/service/tile/:id/layers/:layerId/cache       → 清理图层缓存

# 注册服务管理
POST   /api/service/registry                             → 注册外部服务
GET    /api/service/registry                             → 列出注册服务
GET    /api/service/registry/:id                         → 获取注册服务详情
PUT    /api/service/registry/:id                         → 更新注册服务
DELETE /api/service/registry/:id                         → 删除注册服务
POST   /api/service/registry/:id/refresh                 → 刷新服务元数据
GET    /api/service/proxy/:id/*path                      → 代理转发
```

### 4.2 查询服务 API 示例

#### 创建查询服务（Table 模式）

**请求**：
```http
POST /api/service/query
Content-Type: application/json

{
  "service_name": "beijing_poi",
  "title": "北京POI数据查询",
  "description": "北京市POI数据查询服务",
  "keywords": ["POI", "北京"],
  "config_type": "table",
  "engine_id": "uuid",
  "data_config": {
	"locator": "addp://engine/9/path/public/beijing_poi?type=table&item_id=33",
    "default_fields": ["id", "name", "category", "geom"],
    "filterable_fields": ["name", "category"]
  },
  "protocols": {
    "rest_api": {"enabled": true},
    "ogc_features": {"enabled": true}
  },
  "public_access": false,
  "max_features": 1000
}
```

**响应**：
```json
{
  "id": "uuid",
  "service_name": "beijing_poi",
  "title": "北京POI数据查询",
  "config_type": "table",
  "data_config": {
	"locator": "addp://engine/9/path/public/beijing_poi?type=table&item_id=33",
	"source_snapshot": {
	  "source": {"item_id": 33, "item_fingerprint": "..."},
	  "captured_at": "2026-07-14T08:05:00Z",
	  "dependency_hash": "...",
	  "verification_status": "verified",
	  "table": {"fields": [], "primary_key": ["id"]},
	  "spatial": {
		"geometry_columns": [{"name": "geom", "geometry_type": "Point", "srid": 4326, "crs_ref": "EPSG:4326"}],
		"primary_geometry_column": "geom"
	  }
	},
    "stable_key": ["id"],
    "default_fields": ["id", "name", "category", "geom"]
  },
  "endpoints": {
    "rest_api": "http://localhost:8000/api/query/beijing_poi/query",
    "ogc_features": "http://localhost:8000/ogc/features/beijing_poi"
  }
}
```

#### REST 查询 API

**请求**：
```http
POST /api/query/beijing_poi/query
Content-Type: application/json

{
  "select": ["id", "name", "geom"],
  "filter": {"field": "category", "op": "eq", "value": "餐饮"},
  "order_by": [{"field": "name", "direction": "asc"}],
  "page": {"limit": 50},
  "format": "json"
}
```

**响应**：
```json
{
  "data": [
    {"id": 1, "name": "北京烤鸭", "geom": "{\"type\":\"Point\",\"coordinates\":[116.31,39.99]}"}
  ],
  "page": {"limit": 50, "has_more": true, "next_cursor": "opaque-cursor"},
  "service_version": "dependency-hash"
}
```

### 4.3 瓦片服务 API 示例

#### 创建瓦片服务

**请求**：
```http
POST /api/service/tile
Content-Type: application/json

{
  "service_name": "beijing_map",
  "title": "北京地图服务",
  "default_srid": 3857,
  "protocols": {
    "xyz": {"enabled": true, "formats": ["mvt"]},
    "ogc_tiles": {"enabled": true}
  },
  "public_access": true,
  "first_layer": {
    "layer_name": "road",
    "title": "道路图层",
    "layer_type": "dynamic",
    "layer_config": {
      "source": {
        "engine_id": "uuid",
        "schema": "public",
        "table": "beijing_roads",
        "geometry_column": "geom",
        "srid": 4326
      },
      "cache": {
        "enabled": true,
        "ttl": 3600
      }
    }
  }
}
```

**响应**：
```json
{
  "id": "uuid",
  "service_name": "beijing_map",
  "title": "北京地图服务",
  "layers": [
    {
      "id": "uuid",
      "layer_name": "road",
      "layer_type": "dynamic"
    }
  ],
  "endpoints": {
    "xyz": "http://localhost:8000/tiles/beijing_map/{layerName}/{z}/{x}/{y}.{format}",
    "ogc_tiles": "http://localhost:8000/ogc/tiles/beijing_map"
  }
}
```

#### 瓦片请求示例

```http
# XYZ Tiles（MVT矢量）
GET /tiles/beijing_map/road/12/3421/1532.mvt

# XYZ Tiles（栅格）
GET /tiles/beijing_map/basemap/12/3421/1532.png

# TMS（Y轴相反）
GET /tms/beijing_map/road/12/3421/1532.mvt

# OGC Tiles API
GET /ogc/tiles/beijing_map
GET /ogc/tiles/beijing_map/road/12/3421/1532
```

---

## 五、前端用户流程

### 5.1 服务管理入口

用户进入 Service 模块，看到 3 个创建按钮：

```
┌────────────────────────────────────────┐
│  [ 创建查询服务 ] [ 创建地图服务 ] [ 服务注册 ]  │
│                                        │
│  已创建的服务列表：                     │
│  - 北京POI查询 (查询服务)               │
│  - 北京地图 (地图服务)                  │
│  - 外部WMS服务 (注册服务)               │
└────────────────────────────────────────┘
```

### 5.2 查询服务创建流程（3步）

**Step 1**: 选择配置方式
- ○ 界面配置（推荐）
- ○ SQL配置（高级）

**Step 2**: 配置数据源
- **界面配置**：选择引擎 → Schema → Table，自动检测空间字段，配置默认字段和可过滤字段
- **SQL配置**：选择引擎，编写SQL，手动指定空间字段

**Step 3**: 配置服务信息
- 服务名称、标题、描述、关键词
- 协议支持：REST API、OGC Features
- 访问控制：公开访问、最大返回数

### 5.3 地图服务创建流程（2步）

**Step 1**: 添加第一个图层
- 选择图层类型：动态图层 / 静态图层
- 动态图层：选择引擎、Schema、Table，自动检测空间字段
- 静态图层：从资源树选择 `pmtiles` item，格式、范围和缩放层级由 Meta 自动识别
- 配置缓存策略

**Step 2**: 配置地图服务
- 服务名称、标题、描述
- 默认坐标系（通常 EPSG:3857）
- 瓦片协议：XYZ Tiles、OGC Tiles、TMS
- 支持格式：MVT、PNG、JPEG
- 访问控制：公开访问

### 5.4 注册服务流程

**OGC 服务（WMS/WFS/WMTS）**：
1. 输入服务端点 URL
2. 系统自动获取 GetCapabilities
3. 解析服务元数据和图层列表
4. 配置认证信息（可选）
5. 保存服务

**XYZ 瓦片服务**：
1. 输入瓦片 URL 模板
2. 提供 TileJSON 元数据
3. 测试瓦片可访问性
4. 保存服务

---

## 六、核心设计决策

### 6.1 查询服务统一执行契约

**决策**：表、固定 SQL 和联邦 SQL 只区分来源与 Runtime，REST Query、OGC API Features 和 WFS 统一编译为结构化查询计划

**理由**：
- 避免 SQL 注入和协议层重复构造 SQL
- 统一字段策略、稳定排序键、游标、授权、配额和审计
- 让 OGC bbox、Feature ID 和 REST 过滤共享同一类型化谓词
- 查询默认读取 `limit + 1` 行判断下一页，不执行精确 `COUNT(*)`

### 6.2 空间字段自动检测

**决策**：Table 模式完全自动检测，REST API + OGC Features 协议共存，自动启用

**理由**：
- 简化用户操作
- 智能化检测
- 提供最大灵活性
- 用户可手动禁用不需要的协议

### 6.3 瓦片格式 vs 协议

**决策**：
- 格式：MVT（矢量）、PNG/JPEG（栅格）
- 协议：XYZ Tiles、OGC Tiles API、TMS
- 第一阶段：动态图层支持实时 MVT；静态图层支持 PMTiles v3 中的 gzip MVT

**理由**：
- MVT 是格式不是协议
- 满足主流需求和 OGC 标准
- 栅格动态渲染需要额外开发

### 6.4 JSONB 字段精简设计

**决策**：使用 `data_config` 和 `layer_config` JSONB 字段替代大量独立字段

**理由**：
- 减少字段冗余
- 提高灵活性
- 便于扩展新配置
- 简化数据库 schema

### 6.5 注册服务图层表

**决策**：保留 `registered_service_layers` 表

**理由**：
- OGC 服务包含多个图层
- 需要单独管理图层的启用/禁用
- 支持图层级别的访问控制
- 提供图层列表供前端选择

---

## 七、技术架构

### 7.1 后端分层架构

```
internal/api/               → HTTP handlers + 路由
    query_handler.go        → 查询服务管理 + REST查询API
    tile_handler.go         → 瓦片服务管理API
    tile_endpoint_handler.go → 瓦片端点API
    registry_handler.go     → 注册服务管理 + 代理API
    ogc_features_handler.go → OGC Features 协议实现
    router.go               → 路由配置

internal/service/           → 业务逻辑层
    query_service_service.go → 查询服务业务逻辑
    tile_service_service.go  → 瓦片服务业务逻辑
    tile_generator_service.go → 瓦片生成逻辑
    registered_service_service.go → 注册服务业务逻辑

internal/repository/        → 数据访问层
    query_service_repo.go   → 查询服务数据访问
    tile_service_repo.go    → 瓦片服务数据访问
    registered_service_repo.go → 注册服务数据访问

internal/models/            → 数据模型
    query_service.go        → 查询服务模型
    tile_service.go         → 瓦片服务模型
    registered_service.go   → 注册服务模型
```

### 7.2 前端架构

```
src/api/                    → API 客户端
    queryService.js         → 查询服务 API
    tileService.js          → 瓦片服务 API
    registryService.js      → 注册服务 API

src/views/                  → 页面组件
    QueryServiceForm.vue    → 查询服务创建向导
    QueryServiceList.vue    → 查询服务列表
    QueryServiceDetail.vue  → 查询服务详情
    TileServiceForm.vue     → 地图服务创建向导
    TileServiceList.vue     → 地图服务列表
    TileServiceDetail.vue   → 地图服务详情
    RegistryServiceForm.vue → 注册服务表单
    RegistryServiceList.vue → 注册服务列表
    RegistryServiceDetail.vue → 注册服务详情

src/router/index.js         → 路由配置
```

---

## 八、实施计划

### 8.1 实施批次

**第1批：查询服务**（最简单）
1. 数据库表创建（query_services）
2. Repository + Service + Handler
3. 前端表单、列表、详情页
4. 测试：Table 模式和 SQL 模式

**第2批：注册服务**（依赖少）
1. 数据库表创建（registered_services + layers）
2. Repository + Service + Handler
3. 前端表单、列表、详情页
4. 测试：WMS/WFS 服务注册

**第3批：瓦片服务**（最复杂）
1. 数据库表创建（tile_services + layers）
2. Repository + Service（图层管理）
3. TileGeneratorService（瓦片生成、缓存）
4. Handler（管理 API + 瓦片端点）
5. 前端表单、列表、详情页
6. 测试：动态图层和静态图层

**第4批：清理与优化**
1. 数据迁移脚本
2. 删除旧表和旧代码
3. 更新文档
4. 性能优化

### 8.2 数据迁移

从旧表迁移到新表：
- `internal_services` (service_type='table') → `query_services`
- `internal_services` (service_type='spatial') → `tile_services` + `tile_service_layers`
- `external_services` → `registered_services` + `registered_service_layers`

---

## 九、附录

### 9.1 关键文件清单

**数据库迁移**：
- `service/backend/migrations/20260204_refactor_service_model.sql`

**后端核心文件**：
- Models: `query_service.go`, `tile_service.go`, `registered_service.go`
- Repositories: `query_service_repo.go`, `tile_service_repo.go`, `registered_service_repo.go`
- Services: `query_service_service.go`, `tile_service_service.go`, `tile_generator_service.go`, `registered_service_service.go`
- Handlers: `query_handler.go`, `tile_handler.go`, `tile_endpoint_handler.go`, `registry_handler.go`

**前端核心文件**：
- API: `queryService.js`, `tileService.js`, `registryService.js`
- Views: `QueryService*.vue`, `TileService*.vue`, `RegistryService*.vue`

### 9.2 协议与格式对照

| 协议 | 支持格式 | 适用服务 |
|------|---------|---------|
| REST API | JSON, CSV, GeoJSON | 查询服务 |
| OGC API Features | GeoJSON | 查询服务（有空间字段） |
| XYZ Tiles | MVT, PNG, JPEG | 瓦片服务 |
| OGC Tiles API | MVT, PNG, JPEG | 瓦片服务 |
| TMS | MVT, PNG, JPEG | 瓦片服务 |

### 9.3 缓存路径规范

**动态图层缓存**：
```
MinIO Bucket: service
Path: service/{service_id}/{layer_id}/{z}/{x}/{y}.{format}
```

**静态图层（外部）**：
```
Storage Engine Path: {tile_base_path}/{z}/{x}/{y}.{format}
```

**静态图层（内部）**：
```
MinIO Bucket: service
Path: service/{service_id}/{layer_id}/{z}/{x}/{y}.{format}
```

---

## 十、总结

Service 模块重构设计的核心改进：

1. **清晰的服务分类**：查询服务、瓦片服务、注册服务，各司其职
2. **灵活的配置方式**：界面配置 vs SQL 配置，满足不同需求
3. **自动化的协议启用**：智能检测空间字段，自动启用相应协议
4. **现代化的技术栈**：支持主流协议（XYZ Tiles）和 OGC 标准
5. **精简的数据库设计**：使用 JSONB 字段减少冗余，提高灵活性
6. **用户友好的界面**：前端使用易理解的术语（地图服务）

该设计确保了 Service 模块作为 ADDP 全域数据平台的数据服务发布中心，提供统一、标准、灵活的数据服务能力。

---

**文档结束**
