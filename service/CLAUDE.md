# Service 模块说明

## 模块定位

Service 模块负责数据服务发布与外部服务注册，覆盖查询服务、图查询服务、注册服务代理、瓦片服务、OGC API Features、OGC Tiles、WMTS 和公开访问端点。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8086`，环境变量 `SERVICE_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + OpenLayers，开发端口 `5180`，启动脚本环境变量 `SERVICE_FE_PORT`。
- 数据库：PostgreSQL `service` schema。
- 依赖：System、Manager、Meta、Gateway、Redis、MinIO。

## 重要目录

```text
service/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # query、graph、registered、tile、OGC、resource capability
│   ├── internal/models/       # query、graph、registered、tile
│   ├── internal/repository/
│   ├── internal/service/      # query executor、tile cache/dynamic/static、data query
│   ├── migrations/
│   └── docs/                  # Swagger 产物
├── docs/
│   ├── 数据库架构.md
│   ├── service核心概念和架构设计.md
│   ├── 外部服务架构设计.md
│   ├── 瓦片服务使用指南.md
│   └── tables/
└── frontend/src/
    ├── views/                 # QueryService、GraphQueryService、RegisteredService、TileService
    ├── components/
    └── api/
```

## 核心 API

管理路由前缀：`/api/v1/service`。

- 资产发现与端点：`GET /assets/discoverable`、`GET /endpoints`。
- 查询服务管理：`POST/GET /query`、`GET/PUT/DELETE /query/:id`；公开执行端点：`GET /api/query/:serviceName`。
- 图查询服务管理：`POST/GET /graph`、`GET/PUT/DELETE /graph/:id`；公开执行端点：`POST /api/gquery/:serviceName`。
- 注册服务管理：`POST/GET /registered`、`GET/PUT/DELETE /registered/:id`、`POST /registered/:id/refresh`、`POST /registered/:id/health`；公开代理：`ANY /api/service/registered/proxy/:id/*path`。
- 瓦片服务管理：`POST/GET /tile`、`GET /tile/search`、`GET /tile/by-name/:serviceName`、`GET/PUT/DELETE /tile/:id`、`/tile-layers/:serviceId`。
- 数据查询辅助：`POST /data/query`、`POST /data/aggregate`、`GET /data/structure`，资源输入统一为 `locator`，由后端派生执行所需 `engine_id/schema/table`。
- 资源能力辅助：资源选择、资源树、表级空间元数据统一走 Meta resource-tree / item API；Service 仅保留 `GET /graphs/node-shapes` 和 `POST /sql/spatial-metadata` 等业务能力接口。
- OGC/瓦片公开端点：`/ogc/features/:serviceName/*`、`/tiles/:serviceName/:layerName/:z/:x/*yformat`、`/wmts/:serviceName`、`/ogc/tiles/:serviceName/*`。

## 开发规则

- 存储引擎连接信息必须从 System 获取，Service 不管理连接配置。
- 表结构、空间信息和资源树通过 Meta 共享能力获取；Service 不重复实现资源树、表空间检测或按 `schema/table` 查找资源的代理接口。
- 公开访问端点要在 Handler 内检查服务的 public/private 权限，避免绕过认证。
- 瓦片缓存使用系统 MinIO，路径和缓存策略应保持租户隔离。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh service` 和 `bash scripts/swagger/check-route-coverage.sh service`。

## 开发与验证

```bash
bash scripts/dev/start.sh -service
bash scripts/dev/restart.sh -service
curl http://localhost:8086/health
```

常用日志：

- `logs/service-backend.log`

## 相关文档

- `service/docs/数据库架构.md`
- `service/docs/service核心概念和架构设计.md`
- `service/docs/外部服务架构设计.md`
- `service/docs/瓦片服务使用指南.md`
- `service/docs/API测试指南.md`
- `service/docs/tables/`
- `gateway/docs/gateway架构说明.md`
