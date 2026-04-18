# Service 模块指导说明

## 一、模块定位与价值

### 1.1 核心定位

Service 模块是 ADDP 平台的**统一数据服务门户**，承担以下核心职责：

- **内部数据服务发布中心**：将 ADDP 平台管理的数据资源（数据库表、对象存储文件等）发布为符合国际标准的数据服务；简称服务发布。
- **外部服务注册与代理中心**：注册和管理第三方数据服务，提供统一的访问入口和代理转发能力，简称服务注册。
- **平台级服务目录**：提供统一的服务发现、浏览和查询机制，是平台服务治理的基础设施；简称服务目录。

### 1.2 核心价值

**统一性**
- 统一的服务管理界面：内部服务和外部服务在同一平台管理
- 统一的服务访问入口：通过 ADDP Gateway 提供一致的服务访问体验
- 统一的服务元数据模型：标准化的服务描述和图层信息

**标准化**
- 遵循 OGC 国际标准（WFS、WMTS、OGC API Features 等），确保与第三方 GIS 软件的互操作性
- 支持 RESTful API 设计规范，提供现代化的数据服务接口
- 标准化的元数据描述，便于服务发现和自动化集成

**安全性**
- 集中式认证和授权：统一的 JWT 认证机制
- 敏感信息加密存储：外部服务的认证信息（密码、API Key）采用 AES 加密
- 服务代理白名单机制：防止 SSRF 攻击，保护内网安全
- 租户级数据隔离：确保多租户环境下的数据安全

**便捷性**
- 自动元数据解析：注册外部 OGC 服务时自动获取 GetCapabilities 并解析图层信息
- 一键服务发布：选择数据源即可快速发布为标准服务，无需复杂配置
- 健康监控：自动检查外部服务可用性，及时发现服务故障
- 服务测试工具：内置服务测试功能，便于验证服务配置正确性

---

## 二、核心概念

### 2.1 内部服务 (Internal Service)，又称：服务发布

**定义**
内部服务是指将 ADDP 平台所管理的数据资源（存储引擎中的数据）发布为对外可访问的标准化数据服务。

**数据来源**
- PostgreSQL、MySQL、Doris、ClickHouse 等数据库中的表
- MinIO、S3 等对象存储中的文件（Shapefile、GeoJSON、GeoTIFF 等）
- 未来可扩展：Spark、MongoDB 等其他存储引擎

**服务类型 (Service Type)**

ADDP 平台根据数据特征和使用场景,将内部服务分为两种类型:

1. **空间服务 (spatial)**
   - **适用数据**: 包含几何列的数据表
   - **协议支持**: WFS、WMTS、OGC API Features、REST Query
   - **图层模式**: 支持多图层发布（允许后续添加更多图层）
   - **典型场景**: 地图展示、GIS 分析、空间数据共享
   - **目标客户端**: QGIS、ArcGIS、Web 地图应用

2. **数据表服务 (table)**
   - **适用数据**: 普通数据表或不需要发布空间能力的表
   - **协议支持**: REST Query API（仅支持）
   - **图层模式**: 单图层锁定（不允许添加更多图层）
   - **典型场景**: 业务数据查询、报表集成、数据导出
   - **目标客户端**: Web 应用、移动应用、数据分析工具

**核心特征**
- 数据完全在 ADDP 控制范围内，Service 模块可直接访问底层存储引擎
- **服务类型不可变**: 创建服务时确定类型，之后不可更改
- **协议配置灵活**: 使用 JSONB 配置结构，支持动态协议启用/禁用
- 细粒度配置：可配置坐标系统、最大要素数、空间范围等参数
- 租户隔离：每个租户只能发布和访问自己的数据服务

**配置结构 (Config JSONB)**

服务的协议配置存储在 `config` JSONB 字段中，支持灵活的协议管理:

- **空间服务配置示例**:
```json
{
  "protocols": {
    "wfs": {"enabled": true, "version": "2.0.0"},
    "wmts": {"enabled": true, "version": "1.0.0"},
    "ogc_api": {"enabled": true, "version": "1.0"},
    "rest_query": {"enabled": true}
  },
  "allow_multiple_layers": true
}
```

- **数据表服务配置示例**:
```json
{
  "protocols": {
    "rest_query": {
      "enabled": true,
      "pagination": {
        "default_limit": 20,
        "max_limit": 1000
      },
      "export_formats": ["json", "csv"]
    }
  },
  "allow_multiple_layers": false
}
```

**输出形式**
- **空间服务**: WFS、WMTS、OGC API Features、REST Query（可选择性启用）
- **数据表服务**: REST Query API（固定启用）

### 2.2 外部服务 (External Service)，又称：服务注册

**定义**
外部服务是指第三方提供的数据服务，通过注册到 ADDP 平台后，由 Service 模块进行元数据管理和服务代理。

**服务来源**
- 第三方 OGC 服务：天地图、高德地图、自然资源部门发布的 WMS/WFS 服务
- 其他机构的 REST API：气象数据 API、统计数据 API 等
- 合作伙伴的数据服务

**核心特征**
- 数据由外部系统管理，Service 模块不存储实际数据，仅管理服务元数据（URL、认证信息、图层列表等）
- 自动元数据解析：注册 OGC 服务时自动调用 GetCapabilities 获取服务能力和图层信息
- 服务代理：提供统一的代理访问入口，前端无需直接调用外部服务 URL
- 健康监控：定时检查外部服务可用性，更新服务状态

**支持的服务类型**
- `wms` - Web Map Service（地图图片服务）
- `wfs` - Web Feature Service（矢量要素服务）
- `wmts` - Web Map Tile Service（瓦片地图服务）
- `ogc_api` - OGC API Features（新一代 RESTful 空间 API）
- `data_api` - 通用 RESTful 数据 API
- `rest` - 其他 REST 风格的 API

**服务代理范围**
- **仅支持 HTTP/HTTPS 协议**：当前版本仅支持基于 HTTP 的服务代理
- **服务聚合功能**：未来规划支持将多个外部服务聚合为一个虚拟服务，当前待定

### 2.3 服务目录 (Service Catalog)

**定义**
服务目录是 ADDP 平台的统一服务发现和浏览入口，提供内部服务和外部服务的集中展示、搜索和查询功能。

**核心功能**
- **服务发现**：浏览所有可用的数据服务（内部 + 外部）
- **元数据查询**：查看服务的详细信息（标题、摘要、关键词、提供者、图层列表等）
- **空间范围可视化**：在地图上展示服务的空间覆盖范围
- **服务搜索**：按关键词、服务类型、空间范围等条件搜索服务
- **服务测试**：提供可视化的服务测试工具，验证服务端点可访问性

**访问范围**
- **租户级隔离**：服务目录仅展示当前租户创建或有权访问的服务
- **公开服务**：服务配置 `public_access=true` 时，可在不登录的情况下被访问（具体策略待定）
- **跨租户访问**：未来可能支持服务共享机制，当前不支持

**未来规划**
- **服务分类/标签体系**：待定，便于组织和管理大量服务
- **服务评分/评论**：待定，社区化的服务质量反馈机制

### 2.4 服务图层 (Service Layer)

**定义**
服务图层是服务中的具体数据集或要素类型，一个服务可以包含多个图层。

**内部服务图层**
- 对应于底层数据源的具体表或文件
- 配置项：schema_name、table_name、geometry_column、SRID、空间范围等
- 支持独立的样式配置（SLD 样式，规划中）

**外部服务图层**
- 从外部服务的 GetCapabilities 响应中解析得到
- 存储图层的标识符、显示名称、几何类型、坐标系统、边界框等元数据
- 可以选择性启用/禁用某些图层

---

## 三、功能架构

### 3.1 内部服务发布

ADDP 采用**以数据为中心的统一发布流程**，从数据表选择开始，系统自动检测数据特征（是否包含几何列），引导用户选择合适的服务类型，无需提前区分空间与非空间数据。

#### 3.1.1 空间服务 (Spatial Service)

空间服务是为**包含几何列的数据表**提供的标准化空间数据访问能力，遵循 OGC 国际标准，确保与主流 GIS 软件的互操作性。

**WFS (Web Feature Service) 2.0**
- **用途**：矢量要素查询服务，客户端可查询和下载矢量数据（点、线、面）
- **支持的操作**：
  - GetCapabilities：获取服务能力文档
  - DescribeFeatureType：描述要素类型（字段定义、几何类型）
  - GetFeature：查询要素数据，支持空间过滤（BBox）、属性过滤（CQL）、分页等
- **输出格式**：GeoJSON、GML
- **典型客户端**：QGIS、ArcGIS、OpenLayers、Leaflet
- **实现状态**：✅ 已完整实现

**WMTS (Web Map Tile Service) 1.0**
- **用途**：矢量瓦片地图服务，提供预切分的地图瓦片，适用于高性能地图展示
- **瓦片格式**：MVT (Mapbox Vector Tiles)
- **支持的操作**：
  - GetCapabilities：获取服务能力文档
  - GetTile：获取指定瓦片（z/x/y 坐标）
- **配置项**：瓦片缓冲区、瓦片范围、几何简化容差
- **典型客户端**：Mapbox GL、OpenLayers、Leaflet
- **实现状态**：✅ 已完整实现

**OGC API Features 1.0**
- **用途**：新一代 RESTful 空间数据 API，更加现代化和易用
- **设计理念**：基于 OpenAPI 规范，JSON 优先，符合 Web 开发习惯
- **支持的端点**：
  - Landing Page：服务入口，提供服务元数据和链接
  - Conformance：声明符合的 OGC 标准
  - Collections：图层/集合列表
  - Items：要素查询（支持分页、过滤、BBox）
  - Feature：单个要素查询
- **输出格式**：GeoJSON
- **实现状态**：✅ 已完整实现

**WMS (Web Map Service) 1.3**
- **用途**：地图图片服务，返回渲染好的地图图片（PNG、JPEG）
- **实现状态**：⚠️ 规划中，需要 SLD 样式配置支持

**WCS (Web Coverage Service) 2.0**
- **用途**：栅格数据访问服务，提供原始栅格数据（如 DEM、影像）的查询和下载
- **实现状态**：⚠️ 规划中

#### 3.1.2 数据表服务 (Table Service)

数据表服务是为**普通数据表或不需要空间能力的表**提供的简化 RESTful 查询接口，专注于业务数据查询和集成。

**REST Query API**
- **用途**：提供简化的 RESTful 查询接口，便于 Web 应用和移动应用集成
- **功能特性**：
  - 灵活的列选择：返回全部列或指定列
  - 分页查询：支持 page/limit 分页
  - 条件过滤：支持 WHERE 子句
  - 聚合查询：支持 COUNT、SUM、AVG 等聚合函数（规划中）
  - 排序功能：支持 ORDER BY 子句
  - 导出功能：支持 JSON、CSV 格式导出
- **输出格式**：JSON（固定）
- **图层限制**：单图层服务，不允许添加更多图层
- **实现状态**：✅ 已完整实现

**典型应用场景**
- 业务数据查询：员工表、订单表、产品表等
- 报表数据接口：为报表系统提供数据源
- 移动应用后端：为移动应用提供数据 API
- 数据导出：将数据表导出为 JSON/CSV 格式

#### 3.1.3 发布流程

ADDP 采用**统一的服务发布流程**，通过 3 步向导引导用户完成服务发布，系统自动检测数据特征并推荐合适的服务类型。

**步骤 1: 选择数据表**
```
用户进入"发布服务"页面
  ↓
选择存储引擎
  ├─ 从 System 模块获取当前租户的存储引擎列表
  └─ 支持 PostgreSQL、MySQL、Doris、ClickHouse 等
  ↓
浏览数据表（树形结构）
  ├─ 调用 Manager 模块的 Tree API 获取 schema 和 table 列表
  ├─ 支持懒加载：展开节点时按需加载子节点
  └─ 显示空间标签：标记包含几何列的表
  ↓
选择目标表
  ├─ 调用 Manager 模块的 Preview API 获取表结构
  ├─ 自动检测几何列（geometry/geography 类型）
  ├─ 获取 SRID、几何类型等元数据
  └─ 显示检测结果
```

**步骤 2: 确认服务类型**
```
系统展示检测结果
  ↓
如果检测到几何列：
  ├─ 用户可选择：
  │  ├─ ☑ 空间服务：启用 OGC 协议，支持多图层
  │  └─ ☐ 数据表服务：仅启用 REST Query，单图层
  └─ 默认推荐：空间服务
  ↓
如果未检测到几何列：
  ├─ 自动选择：数据表服务
  └─ 不提供空间服务选项
```

**步骤 3: 配置服务信息**
```
填写服务基本信息
  ├─ 服务名称（唯一标识，英文、数字、下划线）
  ├─ 标题、摘要、关键词
  └─ 公开访问开关（是否需要 JWT 认证）
  ↓
空间服务专属配置：
  ├─ 默认坐标系统（EPSG:4326/3857/4490）
  ├─ 最大要素数限制（防止大数据量查询）
  └─ 协议选择：
     ├─ ☑ WFS 2.0：矢量要素查询服务
     ├─ ☑ WMTS 1.0：矢量瓦片地图服务
     ├─ ☑ OGC API Features：RESTful 空间 API
     └─ ☑ REST Query：简化查询接口（推荐）
  ↓
数据表服务专属配置：
  ├─ 默认分页大小（10-100）
  ├─ 最大分页大小（防止大数据量查询）
  └─ 导出格式：JSON、CSV、Excel（可选）
  ↓
自动创建第一个图层
  ├─ 图层名称：默认使用表名
  ├─ 数据源：schema_name、table_name
  ├─ 几何配置：geometry_column、SRID、几何类型
  └─ 启用状态：默认启用
  ↓
提交并创建服务
  ├─ 后端验证服务类型和配置
  ├─ 构建 JSONB config 结构
  ├─ 创建 internal_services 记录
  ├─ 创建 internal_service_layers 记录
  └─ 返回服务详情
```

**发布后管理**

- **空间服务**：
  - 可以通过"添加图层"按钮添加更多图层
  - 每个图层对应一个数据表
  - 所有图层共享服务级的协议配置

- **数据表服务**：
  - 单图层锁定，不允许添加更多图层
  - 专注于单一数据表的查询和导出
  - 界面显示提示："数据表服务仅支持单图层发布"

**系统自动生成服务端点**

- **空间服务端点**（根据启用的协议）：
  - WFS: `http://host:port/ogc/wfs/{service_name}`
  - WMTS: `http://host:port/ogc/wmts/{service_name}`
  - OGC API: `http://host:port/ogc/api/{service_name}/`
  - REST Query: `http://host:port/api/query/{service_name}/{layer_name}`

- **数据表服务端点**：
  - REST Query: `http://host:port/api/query/{service_name}/{layer_name}`

**数据源连接信息来源**
- 存储引擎的连接信息（数据库地址、端口、用户名、密码等）**直接从 System 模块读取**
- Service 模块不管理存储引擎的连接配置，仅使用 engine_id 引用
- 调用 System API: `GET /api/system/engines/{engine_id}` 获取完整配置

### 3.2 外部服务注册与代理

#### 3.2.1 服务注册

外部服务注册是指将第三方数据服务的 URL 和元数据录入 ADDP 平台的过程。

**注册流程**
```
1. 填写服务基本信息
   - 服务名称、描述
   - 服务类型（wms/wfs/wmts/ogc_api/data_api/rest）
   - 服务 URL（GetCapabilities 或 API 端点）
   ↓
2. 配置认证信息（可选）
   - 认证类型：none（无需认证）/basic（用户名密码）/bearer（Token）/api_key
   - 认证配置：根据类型填写对应的认证信息
   - 敏感信息加密存储（AES-256-GCM）
   ↓
3. 自动元数据解析（针对 OGC 服务）
   - 发送 HTTP 请求到服务 URL（携带认证信息）
   - 调用 GetCapabilities 获取服务能力文档（XML/JSON）
   - 解析服务元数据：版本、支持的操作、坐标系统等
   - 提取图层列表：图层标识符、显示名称、几何类型、边界框等
   - 存储到 metadata JSONB 字段
   ↓
4. 批量创建图层记录
   - 为每个图层创建 external_service_layers 记录
   - 存储图层的详细元数据（CRS、BBox、几何类型等）
   ↓
5. 注册完成
   - 服务状态设置为 active
   - 加入定时健康检查队列
   - 可通过服务目录查看和访问
```

**元数据刷新**
- 外部服务的元数据可能会变化（新增图层、修改坐标系统等）
- 支持手动刷新：通过 API 触发重新获取 GetCapabilities
- 自动刷新：定时任务每天凌晨刷新所有服务元数据（可配置）

#### 3.2.2 服务代理

服务代理是指 ADDP 提供统一的外部服务访问入口，自动处理认证信息，前端无需直接调用外部服务 URL。

**代理流程**
```
前端请求
  ↓
GET /api/service/proxy/{service_id}/path/to/endpoint
  ↓
Service 模块验证
  ├─ 验证服务是否已注册（白名单机制）
  ├─ 验证用户是否有权访问该服务（租户隔离）
  └─ 防 SSRF 检查（禁止访问内网地址）
  ↓
构建外部请求
  ├─ 根据 auth_config 自动添加认证信息
  │  ├─ Basic Auth: 添加 Authorization: Basic <base64>
  │  ├─ Bearer Token: 添加 Authorization: Bearer <token>
  │  └─ API Key: 添加到 URL 参数或请求头
  ├─ 转发原始请求的查询参数和请求体
  └─ 设置合理的超时时间
  ↓
发送 HTTP 请求到外部服务
  ↓
返回响应给前端
  ├─ 透传响应状态码
  ├─ 透传响应头（Content-Type、Cache-Control 等）
  └─ 透传响应体
```

**安全机制**
- **白名单机制**：只能代理已注册的服务，防止被滥用为通用 HTTP 代理
- **防 SSRF 攻击**：验证目标 URL 不是内网地址（127.0.0.1、10.x、172.16.x、192.168.x）
- **认证信息隔离**：前端无需知道外部服务的认证信息，降低泄露风险

**协议支持**
- **当前版本**：仅支持 HTTP/HTTPS 协议的服务代理
- **未来规划**：
  - 数据库连接代理（需要更复杂的安全机制）
  - WebSocket 代理（实时数据流）

**服务聚合**
- **定义**：将多个外部服务的图层合并为一个虚拟服务，提供统一的访问接口
- **应用场景**：整合多个机构的数据服务、创建主题地图服务
- **实现状态**：⚠️ 待定，需要设计图层冲突解决、坐标系统统一等机制

#### 3.2.3 健康监控

健康监控是指定期检查外部服务的可用性，及时发现服务故障或性能问题。

**监控机制**
```
定时任务触发（Cron 表达式，默认每小时）
  ↓
查询所有状态为 active 的外部服务
  ↓
并发健康检查（goroutine pool，最多 10 并发）
  ├─ 对每个服务发送 HTTP 请求
  │  ├─ 使用 health_check_url（如果配置）
  │  └─ 否则使用服务 base_url
  ├─ 设置 5 秒超时限制
  └─ 记录响应状态和响应时间
  ↓
根据响应更新服务状态
  ├─ HTTP 200-299 → status='active'
  ├─ HTTP 400-599 → status='error'
  └─ 超时或网络错误 → status='inactive'
  ↓
更新数据库
  ├─ status 字段
  ├─ last_checked_at 时间戳
  └─ 错误信息（如有）
```

**状态定义**
- `active`：服务正常可用
- `inactive`：服务暂时不可用（超时、网络错误）
- `error`：服务返回错误（HTTP 4xx/5xx）

**故障告警**
- **当前版本**：仅更新服务状态，不发送告警
- **未来规划**：
  - 服务状态变化时发送邮件/短信通知
  - 连续多次失败时触发告警
  - 集成到统一的监控告警平台

### 3.3 统一服务目录

#### 3.3.1 服务发现

服务目录提供可视化的服务浏览界面，用户可以方便地查找和了解平台上的所有数据服务。

**浏览方式**
- **列表视图**：表格形式展示服务列表，支持分页
- **卡片视图**：以卡片形式展示服务，显示服务图标、标题、摘要
- **地图视图**（规划中）：在地图上展示服务的空间覆盖范围，点击查看详情

**展示信息**
- 服务基本信息：名称、标题、摘要、关键词
- 服务类型：内部服务/外部服务、协议类型（WFS/WMTS/REST 等）
- 服务状态：active/inactive/error（外部服务）
- 图层数量：包含多少个图层
- 空间范围：WGS84 边界框（可在地图上可视化）
- 提供者信息：服务提供机构、联系方式
- 创建时间、更新时间

**搜索功能**
- **关键词搜索**：在服务名称、标题、摘要、关键词中搜索
- **类型过滤**：按服务类型（内部/外部）、协议类型（WFS/WMS/REST）筛选
- **空间范围过滤**（规划中）：按空间范围筛选服务（如：查找覆盖北京的服务）
- **状态过滤**：按服务状态筛选（仅显示可用服务）

**访问范围**
- **当前版本**：服务目录仅展示当前租户创建或有权访问的服务
- **租户隔离**：用户无法查看其他租户的私有服务
- **公开服务**：配置为 public_access=true 的服务，可在不登录的情况下浏览（具体策略待定）

**未来规划**
- **服务分类/标签体系**：待定，便于组织和管理大量服务（如：按行业、地域、主题分类）
- **服务评分/评论**：待定，社区化的服务质量反馈机制
- **服务使用统计**：展示服务的访问次数、热度排名
- **跨租户服务共享**：待定，支持租户之间共享服务

#### 3.3.2 目录管理

**服务元数据标准**
- 遵循 ISO 19115（地理信息元数据标准）
- 关键字段：标题、摘要、关键词、空间范围、时间范围、提供者、联系方式、许可证

**服务分组**（规划中）
- 按项目分组：将相关的服务组织在一起
- 按主题分组：如"环境监测"、"城市规划"、"应急管理"

---

## 四、服务类型与标准

### 4.1 服务类型对比

ADDP 平台根据数据特征和使用场景,将内部服务分为两种类型,以下是详细对比:

| 特性 | 空间服务 (spatial) | 数据表服务 (table) |
|------|------------------|------------------|
| **适用数据** | 包含几何列的数据表 | 普通数据表或不需要空间能力的表 |
| **图层数量** | 多图层（允许后续添加） | 单图层（锁定） |
| **协议支持** | WFS、WMTS、OGC API Features、REST Query | REST Query（仅支持） |
| **典型场景** | 地图展示、GIS 分析、空间数据共享 | 业务数据查询、报表集成、数据导出 |
| **目标客户端** | QGIS、ArcGIS、Web 地图应用 | Web 应用、移动应用、数据分析工具 |
| **坐标系统** | 支持多种 EPSG（4326/3857/4490 等） | 不涉及 |
| **空间查询** | 支持 BBox、空间过滤 | 不支持 |
| **格式输出** | GeoJSON、GML、MVT、WKT | JSON、CSV（规划 Excel） |
| **创建后可变** | ❌ 服务类型不可变 | ❌ 服务类型不可变 |
| **URL 示例** | `/ogc/wfs/city_data` | `/api/query/employees/list` |

**设计原则**

1. **以数据为中心**: 从数据表选择开始,让数据特征决定服务形态
2. **服务类型不可变**: 创建时确定类型,避免复杂的数据迁移和配置冲突
3. **协议灵活配置**: 使用 JSONB config 结构,支持动态启用/禁用协议
4. **不偏向空间数据**: 数据表服务与空间服务同等重要,满足全域数据平台需求

### 4.2 OGC 标准服务

| 标准 | 版本 | 用途 | 数据类型 | 实现状态 |
|------|------|------|---------|---------|
| **WFS** | 2.0 | 矢量要素查询服务 | 点、线、面 | ✅ 已实现 |
| **OGC API Features** | 1.0 | RESTful 空间 API | 点、线、面 | ✅ 已实现 |
| **WMTS** | 1.0 | 矢量瓦片地图服务 | MVT 瓦片 | ✅ 已实现 |
| **WMS** | 1.3 | 地图图片服务 | PNG/JPEG 图片 | ⚠️ 规划中 |
| **WCS** | 2.0 | 栅格数据访问服务 | DEM/影像 | ⚠️ 规划中 |

**标准优势**
- **互操作性**：符合国际标准，可被 QGIS、ArcGIS、GeoServer 等主流 GIS 软件直接访问
- **生态丰富**：大量开源库和工具支持（OpenLayers、Leaflet、GDAL、GeoTools 等）
- **文档完善**：OGC 提供详细的标准文档和最佳实践

### 4.3 非 OGC 服务

**REST API**
- 通用的 RESTful 接口，适用于 Web 应用集成
- 支持 JSON 格式输出
- 灵活的查询参数（分页、过滤、排序）

**Data API**
- 自定义的数据 API，不限于地理空间数据
- 支持业务数据查询（如：统计数据、IoT 传感器数据）

**GraphQL API**（规划中）
- 灵活的查询语言，客户端按需获取数据
- 减少网络传输，提高性能

### 4.4 坐标参考系统

Service 模块支持多种坐标参考系统（Coordinate Reference System, CRS），确保与不同地域和行业的数据兼容。

| EPSG 代码 | 名称 | 说明 | 应用场景 |
|----------|------|------|---------|
| **4326** | WGS84 | 全球通用坐标系 | GPS、全球地图、OGC 默认 |
| **3857** | Web Mercator | Web 地图投影 | 在线地图（Google、OSM） |
| **4490** | CGCS2000 | 中国大地坐标系 2000 | 中国官方标准（2008 年后） |
| **2000** | 北京 54 | 中国旧坐标系 | 历史数据兼容 |
| **4214** | 西安 80 | 中国旧坐标系 | 历史数据兼容 |

**坐标转换**（规划中）
- 自动坐标转换：客户端请求不同坐标系时自动转换
- 转换精度保证：使用 PostGIS/GDAL 进行高精度转换

---

## 五、架构设计原则

### 5.1 统一的服务模型设计

**服务类型统一管理**
- 使用 `service_type` 字段统一标识服务类型（'spatial' | 'table'）
- 服务类型在创建时确定，之后不可更改，避免复杂的数据迁移
- 两种服务类型共享相同的数据表结构，通过配置区分行为

**JSONB 配置灵活性**
- 使用 `config` JSONB 字段存储协议配置，避免为每个协议添加独立字段
- 支持动态协议启用/禁用，无需修改数据库结构
- 便于未来扩展新的协议类型和配置选项

**单一发布流程**
- 从数据表选择开始，系统自动检测数据特征（几何列）
- 根据检测结果引导用户选择合适的服务类型
- 统一的前端组件和 API 接口，降低学习成本

### 5.2 模块化与解耦

**内部服务与外部服务分离**
- 两类服务使用独立的数据表（internal_services、external_services）
- 业务逻辑分离，便于独立演进和维护

**服务发布与数据存储解耦**
- Service 模块不直接管理数据，通过 System 模块获取存储引擎连接信息
- 数据源可以是任何支持的存储引擎（PostgreSQL、MinIO、MongoDB 等）

### 5.3 标准优先

**遵循国际标准**
- OGC 标准：WFS、WMTS、OGC API Features
- ISO 标准：ISO 19115（元数据）、ISO 19128（WMS）
- RESTful API 设计规范：符合 REST 架构风格

**保证互操作性**
- 发布的服务可被第三方 GIS 软件直接访问
- 元数据格式兼容主流元数据目录（如 GeoNetwork、CSW）

### 5.4 租户隔离

**租户隔离**
- 所有服务和图层记录包含 tenant_id 字段
- 查询时自动过滤，用户只能看到自己租户的数据
- SuperAdmin 也不可查看其他租户的服务

### 5.5 性能优先

**元数据缓存**
- 外部服务的 GetCapabilities 响应缓存在数据库（metadata JSONB 字段）
- 减少对外部服务的重复请求，提高响应速度

**瓦片缓存**
- WMTS 瓦片支持 HTTP Cache-Control 头
- 可配置瓦片缓存策略（如：缓存 7 天）

**数据库查询优化**
- 关键字段建立索引（tenant_id、service_type、service_name 等）
- 使用 GORM Preload 减少 N+1 查询问题
- 分页查询限制结果集大小

**并发优化**
- 健康检查使用 goroutine pool，控制并发数量
- 避免同时向大量外部服务发起请求导致网络拥塞

### 5.6 安全第一

**敏感信息加密**
- 外部服务的认证信息（密码、Token、API Key）使用 AES-256-GCM 加密存储
- 加密密钥从环境变量读取，不硬编码在代码中

**服务代理安全**
- 白名单机制：只能代理已注册的服务
- 防 SSRF 攻击：验证目标 URL 不是内网地址
- 认证信息隔离：前端无需知道外部服务的认证信息

**访问控制**
- JWT 认证：所有 API 端点（除公开端点外）需要 JWT token
- 租户隔离：自动过滤 tenant_id，用户只能访问自己租户的数据
- 服务级权限：支持配置 public_access（公开访问）或私有访问

---

## 六、与其他模块的协作

### 6.1 System 模块

**用户认证和授权**
- Service 模块的所有 API 端点使用 System 模块的 JWT 认证机制
- 通过 JWT token 获取用户身份（user_id、tenant_id）

**存储引擎配置获取**
- **重要**：数据源连接信息（数据库地址、端口、用户名、密码）**直接从 System 模块读取**
- Service 模块通过 engine_id 引用存储引擎，调用 System API 获取连接配置
- Service 模块不管理存储引擎的配置，避免职责重叠

**审计日志记录**
- 服务创建、修改、删除等操作记录到 System 模块的审计日志
- 便于安全审计和问题追溯

### 6.2 Meta 模块

**数据元数据获取**
- 内部服务发布时，可关联 Meta 模块的元数据项（meta_item_id）
- 自动获取数据的空间范围、几何类型、字段定义等信息
- 减少用户手动配置，提高发布效率

**对象存储访问**
- 查询 MinIO 中的文件列表和元数据
- 支持将对象存储中的空间数据文件（Shapefile、GeoJSON）发布为服务

### 6.3 Gateway 模块

**API 路由和转发**
- Service 模块向 Gateway 注册路由前缀 `/service`
- Gateway 将 `/api/service/*` 的请求转发到 Service 模块
- 统一的入口管理，便于负载均衡和流量控制

**内部 API Key 调用**
- Service 模块支持通过内部 API Key 调用 Gateway 的管理接口
- 跨模块调用时使用内部认证，无需用户 JWT token

### 6.4 Develop 模块（湖表服务发布，规划中）

**SQL 查询结果发布**
- 用户在 Develop 模块的 SQL 工作台中编写查询
- 一键将查询结果发布为数据服务（REST API 或 OGC 服务）
- 动态服务：每次请求时执行 SQL，返回最新数据

**湖表服务发布（中期规划）**

当前 Service 模块的数据源模型以存储引擎为中心（PostgreSQL/MySQL/MinIO），不支持 Parquet lake_table 的直接发布。中期规划引入新的服务类型：

| 服务类型 | 数据源 | 执行引擎 | 协议 |
|---|---|---|---|
| `spatial` | 关系型表（含几何列） | 直连 DB | WFS/WMTS/OGC API/REST |
| `table` | 关系型表 | 直连 DB | REST Query |
| `lake`（规划） | Parquet on MinIO/S3 | DuckDB（内嵌） | REST Query |
| `query`（规划） | 任意（SQL 定义） | DuckDB（内嵌） | REST Query |

**架构决策**：
- DuckDB 是嵌入式库，Service 模块直接内嵌，不依赖 Develop 模块的 DuckDB API（避免跨模块 HTTP 调用）
- DuckDB 的挂载逻辑提取到 `common/duckdb/`，Service 和 Develop 共享
- `lake` 类型：用户选择 MinIO/S3 引擎下的 lake_table，Service 用 DuckDB 读 Parquet 执行查询
- `query` 类型：用户写一个跨源 SQL，Service 每次请求时用 DuckDB 执行并返回最新数据

**实现状态**：⚠️ 规划中，待湖仓一体化第三阶段实施

---

## 七、服务类型统一架构重构 (2026-02)

### 7.1 重构背景

**问题识别**
- 原有设计使用多个 boolean 字段（enabled_wfs、enabled_wmts、enabled_ogc_api 等）管理协议启用状态，扩展性差
- 空间服务和非空间服务的发布流程分离，用户需要提前判断数据类型，增加学习成本
- 过度偏向空间数据，忽视了非空间业务数据服务的重要性
- 缺乏明确的服务类型概念，导致多图层/单图层逻辑混乱

**设计目标**
- 统一服务发布流程，从数据表选择开始，自动检测数据特征
- 引入明确的服务类型概念（spatial vs table），规范服务行为
- 使用 JSONB 配置结构，提升协议管理的灵活性
- 平衡空间与非空间数据服务，满足全域数据平台需求

### 7.2 核心变更

#### 7.2.1 数据库模型变更

**删除字段**（激进策略，无向后兼容）
```sql
ALTER TABLE service.internal_services
DROP COLUMN IF EXISTS enabled_wfs,
DROP COLUMN IF EXISTS enabled_wmts,
DROP COLUMN IF EXISTS enabled_ogc_api,
DROP COLUMN IF EXISTS enabled_wms,
DROP COLUMN IF EXISTS enabled_rest_query;
```

**新增字段**
```sql
ALTER TABLE service.internal_services
ADD COLUMN service_type VARCHAR(50) NOT NULL DEFAULT 'spatial',
ADD COLUMN config JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE service.internal_services
ADD CONSTRAINT check_service_type
CHECK (service_type IN ('spatial', 'table'));
```

**JSONB Config 结构示例**

空间服务配置:
```json
{
  "protocols": {
    "wfs": {"enabled": true, "version": "2.0.0"},
    "wmts": {"enabled": true, "version": "1.0.0"},
    "ogc_api": {"enabled": true, "version": "1.0"},
    "rest_query": {"enabled": true}
  },
  "allow_multiple_layers": true
}
```

数据表服务配置:
```json
{
  "protocols": {
    "rest_query": {
      "enabled": true,
      "pagination": {
        "default_limit": 20,
        "max_limit": 1000
      },
      "export_formats": ["json", "csv"]
    }
  },
  "allow_multiple_layers": false
}
```

#### 7.2.2 后端模型调整

**InternalService 模型新增辅助方法**

```go
// 检查协议是否启用
func (s *InternalService) IsProtocolEnabled(protocol string) bool {
    config := s.GetProtocolConfig(protocol)
    if config == nil {
        return false
    }
    enabled, ok := config["enabled"].(bool)
    return enabled
}

// 是否允许多图层
func (s *InternalService) AllowMultipleLayers() bool {
    if s.ServiceType == "table" {
        return false
    }
    if s.Config != nil {
        if allow, ok := s.Config["allow_multiple_layers"].(bool); ok {
            return allow
        }
    }
    return s.ServiceType == "spatial"
}

// 是否为空间服务
func (s *InternalService) IsSpatialService() bool {
    return s.ServiceType == "spatial"
}
```

**业务逻辑验证规则**

创建服务时的验证逻辑:
- 空间服务必须指定 `default_srid`
- 空间服务的图层必须有 `geometry_column`
- 数据表服务的图层不能有 `geometry_column`
- 数据表服务只能有 1 个图层（单图层锁定）

#### 7.2.3 前端实现

**新增组件: TableSelector.vue**
- 集成 Manager 模块的 Tree API，支持树形浏览 schema 和 table
- 懒加载优化：展开节点时按需加载子节点，提升大数据量性能
- 自动几何列检测：调用 Manager 的 Preview API 获取表结构，检测几何列
- 空间标签显示：标记包含几何列的表，辅助用户选择

**重构组件: PublishedServiceForm.vue**
- 改为 3 步向导模式：选择表 → 确认类型 → 配置服务
- 根据几何列检测结果动态显示服务类型选项
- 空间服务专属配置：坐标系统、协议选择
- 数据表服务专属配置：分页大小、导出格式

**更新组件: PublishedServiceDetail.vue**
- 根据 `service_type` 条件渲染不同的端点卡片
- 空间服务显示 OGC 端点（WFS/WMTS/OGC API）
- 数据表服务显示 REST Query API 端点和使用说明
- 图层列表区分：空间服务显示"添加图层"按钮，数据表服务显示单图层提示

#### 7.2.4 OGC 协议处理器调整

**WFS/WMTS/OGC API Handler**
- 增加服务类型验证：只有 `service_type='spatial'` 的服务才能访问
- 返回 HTTP 400 错误：如果数据表服务尝试访问 OGC 端点

**REST Query Handler**
- 支持所有服务类型
- 空间服务：支持几何字段格式转换（GeoJSON/WKT）和空间过滤（BBox）
- 数据表服务：简化响应格式，不包含几何字段的特殊处理

### 7.3 实施策略

**无向后兼容（激进策略）**
- 直接删除废弃字段，清空现有服务数据
- 不保留兼容性代码，简化实现
- 目标：在开发阶段快速迭代，避免技术债务累积

**迁移步骤**
1. Phase 1: 数据库变更 - 执行迁移脚本清理数据和变更表结构
2. Phase 2: 后端实现 - 更新模型、业务逻辑、API 处理器、OGC 协议处理器
3. Phase 3: 前端实现 - 创建 TableSelector 组件，重构发布表单和详情页
4. Phase 4: 测试验证 - 验证空间服务和数据表服务的完整发布流程
5. Phase 5: 文档更新 - 更新 service/CLAUDE.md 和相关文档

**迁移脚本**
- 文件位置: `/service/backend/migrations/20260202_refactor_service_model.sql`
- 内容: 清空数据 → 删除旧字段 → 添加新字段 → 添加约束

### 7.4 设计决策

**关键决策记录**

| 决策 | 理由 | 影响 |
|------|------|------|
| 服务类型不可变 | 避免复杂的配置迁移和数据转换 | 用户需在创建时明确服务定位 |
| JSONB 配置结构 | 协议配置灵活，易于扩展 | 提升协议管理的可维护性 |
| 单一发布流程 | 降低学习成本，提升用户体验 | 前端逻辑更复杂，但用户体验更好 |
| 激进的无兼容策略 | 开发阶段快速迭代，避免技术债务 | 现有数据需清空，适合开发阶段 |
| 单图层 vs 多图层 | 空间服务常需主题服务（多图层），数据表服务专注单表查询 | 符合实际使用场景 |

**未来扩展方向**
- 支持更多数据类型服务（时序数据、图数据、向量数据等）
- `service_type` 字段可扩展为 'spatial'/'table'/'timeseries'/'graph' 等
- JSONB config 结构支持各类服务的差异化配置

### 7.5 相关文件清单

**后端核心文件**
- `backend/migrations/20260202_refactor_service_model.sql` - 数据库迁移脚本
- `backend/internal/models/internal_service.go` - 服务模型和辅助方法
- `backend/internal/service/internal_service_service.go` - 业务逻辑和验证
- `backend/internal/api/internal_service_handler.go` - API 处理器
- `backend/internal/api/wfs_handler.go` - WFS 协议处理器
- `backend/internal/api/wmts_handler.go` - WMTS 协议处理器
- `backend/internal/api/ogc_api_handler.go` - OGC API 协议处理器
- `backend/internal/api/rest_query_handler.go` - REST Query 处理器

**前端核心文件**
- `frontend/src/components/TableSelector.vue` - 数据表选择器（新增）
- `frontend/src/views/PublishedServiceForm.vue` - 服务发布表单（重构）
- `frontend/src/views/PublishedServiceDetail.vue` - 服务详情页（更新）
- `frontend/src/api/publishedService.js` - API 客户端（无需变更）

---

## 八、相关文档

### 8.1 模块文档

- [数据库架构](docs/数据库架构.md) - Service 模块的数据表结构和关系
- [外部服务架构设计](docs/外部服务架构设计.md) - 外部服务注册和代理的详细设计
- [API 测试指南](docs/API测试指南.md) - Service 模块 API 的测试方法和示例

### 8.2 数据表文档

- [external_services 表](docs/tables/external_services表.md) - 外部服务注册表
- [external_service_layers 表](docs/tables/external_service_layers表.md) - 外部服务图层表
- [internal_services 表](docs/tables/internal_services表.md) - 内部服务发布表
- [internal_service_layers 表](docs/tables/internal_service_layers表.md) - 内部服务图层表

### 8.3 平台文档

- [ADDP 开发原则](../docs/addp开发原则.md) - 平台级开发原则和规范
- [ADDP 各模块简要介绍](../docs/concepts/addp各模块功能介绍.md) - 平台核心概念辨析
- [System 模块说明](../system/CLAUDE.md) - System 模块的架构和功能
- [Meta 模块说明](../meta/CLAUDE.md) - Meta 模块的架构和功能
- [Gateway 架构说明](../gateway/docs/gateway架构说明.md) - Gateway 模块的路由和转发机制

### 8.4 外部标准文档

- [OGC 标准](https://www.ogc.org/standards/) - OGC 官方标准文档
- [WFS 2.0 规范](https://www.ogc.org/standards/wfs) - Web Feature Service 标准
- [WMTS 1.0 规范](https://www.ogc.org/standards/wmts) - Web Map Tile Service 标准
- [OGC API Features](https://ogcapi.ogc.org/features/) - OGC API Features 标准
- [ISO 19115](https://www.iso.org/standard/53798.html) - 地理信息元数据标准

---

## 附录：重要文件位置

### 核心业务逻辑
- [internal/service/registry/external_service_service.go](backend/internal/service/registry/external_service_service.go) - 外部服务管理
- [internal/service/internal_service_service.go](backend/internal/service/internal_service_service.go) - 内部服务管理
- [internal/service/data/query_service.go](backend/internal/service/data/query_service.go) - 数据查询服务

### API 处理器
- [internal/api/router.go](backend/internal/api/router.go) - 路由定义
- [internal/api/service_registry_handler.go](backend/internal/api/service_registry_handler.go) - 外部服务 API
- [internal/api/internal_service_handler.go](backend/internal/api/internal_service_handler.go) - 内部服务 API
- [internal/api/wfs_handler.go](backend/internal/api/wfs_handler.go) - WFS 请求处理
- [internal/api/wmts_handler.go](backend/internal/api/wmts_handler.go) - WMTS 请求处理
- [internal/api/ogc_api_handler.go](backend/internal/api/ogc_api_handler.go) - OGC API Features 处理

### 数据访问层
- [internal/repository/external_service_repository.go](backend/internal/repository/external_service_repository.go) - 外部服务数据访问
- [internal/repository/internal_service_repository.go](backend/internal/repository/internal_service_repository.go) - 内部服务数据访问

### 数据模型
- [internal/models/external_service.go](backend/internal/models/external_service.go) - 外部服务模型
- [internal/models/internal_service.go](backend/internal/models/internal_service.go) - 内部服务模型

### 前端页面
- [frontend/src/views/ServiceManagement.vue](frontend/src/views/ServiceManagement.vue) - 外部服务管理
- [frontend/src/views/PublishedServiceList.vue](frontend/src/views/PublishedServiceList.vue) - 内部服务列表
- [frontend/src/views/ServiceCatalog.vue](frontend/src/views/ServiceCatalog.vue) - 服务目录
