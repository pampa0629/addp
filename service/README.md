# Service 模块

数据服务和 GIS 空间服务模块，提供统一的数据查询和 OGC 标准服务能力。

## 功能概述

### 1. 外部服务注册管理
- ✅ 支持注册多种类型的外部服务（WMS、WFS、WMTS、OGC API、REST API）
- ✅ 自动获取 OGC 服务元数据（GetCapabilities）
- ✅ 服务健康检查
- ✅ 服务代理（统一对外提供服务）
- ✅ 按租户隔离

### 2. 数据查询服务
- ✅ 数据表查询
- ✅ 聚合查询
- 🚧 完整实现（待集成 Manager 和 Meta 模块）

### 3. OGC 标准支持（规划中）
- ⏳ OGC API - Features (REST 标准)
- ⏳ WMS 1.3.0
- ⏳ WFS 3.0
- ⏳ WMTS

## 技术架构

```
service/
├── backend/
│   ├── cmd/server/          # 主程序入口
│   ├── internal/
│   │   ├── api/             # HTTP API 层
│   │   ├── config/          # 配置管理
│   │   ├── models/          # 数据模型
│   │   ├── repository/      # 数据访问层
│   │   └── service/         # 业务逻辑层
│   │       ├── data/        # 数据查询服务
│   │       └── registry/    # 服务注册管理
│   └── go.mod
└── frontend/                # 前端（待实现）
```

## 数据库 Schema

**Schema**: `service`

### 表结构

#### external_services（外部服务）
- `id` - 主键
- `tenant_id` - 租户 ID（租户隔离）
- `name` - 服务名称
- `service_type` - 服务类型（wms/wfs/wmts/ogc_api/data_api/rest）
- `url` - 服务地址
- `metadata` - 服务元数据（JSONB）
- `auth_type` - 认证类型（none/basic/bearer/api_key）
- `auth_config` - 认证配置（JSONB，加密存储）
- `status` - 状态（active/inactive/error）
- `health_check_url` - 健康检查地址
- `last_checked_at` - 上次检查时间

#### service_layers（服务图层）
- `id` - 主键
- `service_id` - 关联服务 ID
- `layer_name` - 图层名称
- `geometry_type` - 几何类型
- `crs` - 坐标系
- `bbox` - 边界框（JSONB）
- `metadata` - 图层元数据（JSONB）

## API 端点

### 服务注册管理

```bash
# 创建外部服务
POST /api/service/registry/services
{
  "name": "第三方 WMS 服务",
  "service_type": "wms",
  "url": "https://example.com/wms",
  "auth_type": "none"
}

# 列出服务
GET /api/service/registry/services?service_type=wms&page=1&page_size=20

# 获取服务详情
GET /api/service/registry/services/:id

# 更新服务
PUT /api/service/registry/services/:id

# 删除服务
DELETE /api/service/registry/services/:id

# 刷新服务元数据
POST /api/service/registry/services/:id/refresh

# 健康检查
POST /api/service/registry/services/:id/health

# 搜索服务
GET /api/service/registry/search?keyword=地图

# 导出配置
GET /api/service/registry/export
```

### 服务目录

```bash
# 获取服务目录（按类型分类）
GET /api/service/catalog
```

### 服务代理

```bash
# 代理访问外部服务
GET /api/service/proxy/:id/*path
```

### 数据查询服务

```bash
# 查询数据表
POST /api/service/data/query
{
  "resource_id": 1,
  "schema": "public",
  "table": "cities",
  "page": 1,
  "page_size": 20
}

# 聚合查询
POST /api/service/data/aggregate
{
  "resource_id": 1,
  "schema": "public",
  "table": "sales",
  "group_by": ["region"],
  "aggregates": [
    {"column": "amount", "function": "sum", "alias": "total"}
  ]
}
```

## 配置

### 环境变量

```bash
# 服务端口
PORT=8086

# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_USER=addp
DB_PASSWORD=addp_password
DB_NAME=addp
DB_SCHEMA=service

# 模块集成
SYSTEM_SERVICE_URL=http://localhost:8080
MANAGER_SERVICE_URL=http://localhost:8081
META_SERVICE_URL=http://localhost:8082

# Redis 配置
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
```

## 启动服务

### 开发模式

```bash
# 启动所有服务（包括 service 模块）
bash scripts/dev/start.sh

# 单独重启 service 模块
bash scripts/dev/restart.sh -service
```

### 访问

- **Backend**: http://localhost:8086
- **通过 Gateway**: http://localhost:8000/api/service/
- **健康检查**: http://localhost:8086/health

## 开发计划

### Phase 1 ✅（已完成）
- [x] 模块基础架构
- [x] 外部服务注册管理
- [x] 数据查询服务框架
- [x] Gateway 路由集成
- [x] 启动脚本支持

### Phase 2（计划中）
- [ ] 完整的 OGC Capabilities 解析
- [ ] OGC API - Features 实现
- [ ] WFS/WMTS 服务实现
- [ ] 前端管理界面
- [ ] 完整的数据查询实现（集成 Manager/Meta）

### Phase 3（未来）
- [ ] WMS 服务实现
- [ ] 服务性能监控
- [ ] 缓存优化
- [ ] 服务发现和负载均衡

## 注意事项

1. **认证**: 所有 API 端点需要通过 JWT token 认证
2. **租户隔离**: 所有服务按租户 ID 隔离
3. **健康检查**: 支持定时检查外部服务可用性
4. **元数据缓存**: OGC 服务元数据会缓存 1 小时

## 相关文档

- [ADDP 开发原则](../docs/addp开发原则.md)
- [ADDP 配置介绍](../docs/addp配置介绍.md)
- [新模块开发指南](../docs/新模块开发指南.md)
