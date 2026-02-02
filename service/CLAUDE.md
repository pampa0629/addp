# Service 模块说明

## 核心职责

Service 模块是 ADDP 平台的**数据服务发布中心**，负责以下核心功能：

1. **外部服务注册** - 管理第三方数据服务的注册和配置（OGC WMS/WFS、REST API 等）
2. **数据查询服务** - 提供统一的数据查询 API，支持跨数据源查询
3. **OGC 标准支持** - 实现 OGC WMS、WFS、WMTS 等地理空间服务标准（规划中）
4. **服务健康检查** - 定时检查注册服务的可用性，自动更新服务状态
5. **服务元数据管理** - 存储和查询服务的元数据信息（能力文档、图层列表等）

## 关键架构

### 服务注册架构

```
外部数据服务
  ↓
Service Registry API（服务注册）
  ├─ 填写服务基本信息（名称、URL、类型）
  ├─ 自动探测服务能力（GetCapabilities）
  ├─ 解析服务元数据（图层列表、坐标系统）
  └─ 存储到 service.external_services 表
  ↓
定时健康检查（Scheduler）
  ├─ 每小时检查所有活跃服务
  ├─ 更新服务状态（active/inactive）
  └─ 记录健康检查日志
```

### 数据查询服务

```
前端请求
  ↓
Data Service API（统一查询接口）
  ├─ 数据库查询 → System Client（获取引擎连接）
  │  └─ 执行 SQL 查询
  ├─ 对象存储查询 → Meta Client（获取对象元数据）
  │  └─ 返回文件列表或内容
  └─ 外部服务查询 → HTTP 请求转发
     └─ 调用已注册的外部服务
  ↓
返回统一格式的查询结果（JSON/GeoJSON）
```

### OGC 服务支持（规划中）

- **WMS (Web Map Service)** - 地图图片服务
- **WFS (Web Feature Service)** - 矢量要素服务
- **WMTS (Web Map Tile Service)** - 瓦片地图服务
- **WCS (Web Coverage Service)** - 栅格覆盖服务

## 数据库文档

**遇到以下场景时,主动阅读对应文档**:

| 场景 | 必读文档 | 触发关键词 |
|------|---------|----------|
| 数据库表结构查询 | 对应单表文档 | 字段定义、索引、约束 |
| 表之间关系 | 数据库架构.md | 外键、关联、数据流 |
| API端点详情 | 对应单表文档 | API、接口、请求响应 |
| 服务注册管理 | external_services表 | WMS、WFS、服务注册、认证 |
| 图层管理 | external_service_layers表 | 图层列表、几何类型、坐标系统 |

### 架构说明
- [数据库架构](docs/数据库架构.md) - 表关系、数据流向、设计决策

### 单表文档

详细的表结构和API说明文档：

- [external_services表](docs/tables/external_services表.md) - 外部服务注册表,支持多种服务类型和认证方式
- [external_service_layers表](docs/tables/external_service_layers表.md) - 外部服务图层表,存储外部服务的图层/要素类信息

**重要**：修改表结构或API时，必须同步更新对应的单表文档。

## 重要文件位置

### 核心服务文件

- [service/registry/external_service_service.go](internal/service/registry/external_service_service.go) - **外部服务注册管理**
- [service/data/query_service.go](internal/service/data/query_service.go) - **统一数据查询服务**

### API 路由文件

- [internal/api/router.go](internal/api/router.go) - HTTP 路由定义
- [internal/api/service_registry_handler.go](internal/api/service_registry_handler.go) - 服务注册 API
- [internal/api/data_service_handler.go](internal/api/data_service_handler.go) - 数据查询 API

### 数据模型文件

- [internal/models/external_service.go](internal/models/external_service.go) - 外部服务模型

### 前端视图文件

- [frontend/src/views/ServiceRegistry.vue](frontend/src/views/ServiceRegistry.vue) - 服务注册界面
- [frontend/src/views/ServiceList.vue](frontend/src/views/ServiceList.vue) - 服务列表查看
- [frontend/src/views/DataQuery.vue](frontend/src/views/DataQuery.vue) - 数据查询界面

### 配置文件

- [internal/config/config.go](internal/config/config.go) - 配置加载逻辑
- [.env](../.env) - 环境变量（`SERVICE_*` 前缀）

## 常见开发场景

### 场景 1：注册外部 OGC 服务

```bash
# 1. 注册 WMS 服务
curl -X POST http://localhost:8085/api/v1/services \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "天地图WMS服务",
    "service_type": "wms",
    "base_url": "http://t0.tianditu.gov.cn/vec_c/wmts",
    "version": "1.0.0",
    "description": "天地图矢量底图",
    "is_active": true
  }'

# 2. 查看服务列表
curl -H "Authorization: Bearer <token>" \
  http://localhost:8085/api/v1/services

# 3. 查看服务详情（包含元数据）
curl -H "Authorization: Bearer <token>" \
  http://localhost:8085/api/v1/services/123
```

### 场景 2：查询数据服务

```bash
# 1. 查询数据库表
curl -X POST http://localhost:8085/api/v1/data/query \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "source_type": "database",
    "engine_id": 1,
    "query": "SELECT * FROM public.cities LIMIT 10"
  }'

# 2. 查询对象存储
curl -X POST http://localhost:8085/api/v1/data/query \
  -H "Authorization: Bearer <token>" \
  -d '{
    "source_type": "object_storage",
    "engine_id": 2,
    "bucket": "gis-data",
    "prefix": "shapefiles/"
  }'
```

### 场景 3：调试服务健康检查

```bash
# 1. 手动触发健康检查
curl -X POST http://localhost:8085/api/v1/services/123/health-check \
  -H "Authorization: Bearer <token>"

# 2. 查看健康检查日志
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8085/api/v1/services/123/health-logs?limit=20"

# 3. 查看 Service 后端日志
tail -f logs/service-backend.log | grep "health-check"
```

## 注意事项

### 1. 外部服务安全

注册外部服务时需注意：

- ✅ **HTTPS 优先** - 优先使用 HTTPS 协议避免中间人攻击
- ✅ **API Key 加密** - 外部服务的 API Key 应加密存储
- ❌ **不做权限代理** - Service 模块不代理用户权限到外部服务

### 2. 服务元数据缓存

服务元数据（如 GetCapabilities 响应）会缓存在数据库中：

- **缓存时间** - 默认 24 小时
- **手动刷新** - 通过 API 手动触发元数据刷新
- **自动刷新** - 定时任务每天凌晨刷新所有服务元数据

### 3. OGC 服务兼容性

不同 OGC 服务提供商的实现可能有差异：

- **版本差异** - WMS 1.1.1 vs 1.3.0
- **坐标系统** - EPSG:4326 vs EPSG:3857
- **参数格式** - 大小写敏感性

建议在注册服务后进行测试，确保兼容性。

### 4. 与其他模块的交互

- **System 模块** - 获取数据库连接信息（用于数据查询服务）
- **Meta 模块** - 获取对象存储元数据（用于文件查询）
- **Manager 模块** - 可能复用 Manager 的数据预览能力

### 5. 性能优化建议

- **查询结果缓存** - 对频繁查询的数据进行缓存（Redis）
- **分页查询** - 限制单次查询返回的数据量
- **异步处理** - 大数据量查询使用异步任务队列

## 典型开发工作流

### 修改 Service 后端代码后

```bash
# 1. 重启 Service 后端服务
bash scripts/dev/restart.sh -service

# 2. 查看启动日志
tail -f logs/service-backend.log

# 3. 测试 API
curl -H "Authorization: Bearer <token>" \
  http://localhost:8085/health
```

### 添加新的 OGC 服务类型支持

```bash
# 1. 在 internal/service/ogc/ 创建新的服务实现
# 2. 实现 GetCapabilities 解析逻辑
# 3. 注册到 ServiceRegistry
# 4. 重启服务
bash scripts/dev/restart.sh -service

# 5. 测试新服务类型注册
curl -X POST http://localhost:8085/api/v1/services \
  -H "Authorization: Bearer <token>" \
  -d '{"service_type": "wcs", ...}'
```

## 相关文档

- **OGC 标准文档** - https://www.ogc.org/standards/
- **System 模块说明** - [system/CLAUDE.md](../system/CLAUDE.md)
- **Meta 模块说明** - [meta/CLAUDE.md](../meta/CLAUDE.md)
