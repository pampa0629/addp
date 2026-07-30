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
├── authorization/
│   └── permissions.yaml       # Service Permission Manifest，发布期聚合事实源
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

Service 是 `service.definition.*`、`service.endpoint.read` 和 `service.external_registration.*` 的 Permission owner；定义只存在于 `authorization/permissions.yaml`，通过 `common/authorization` 发布期聚合，不在服务启动时动态注册。`service.definition.publish/offline` 是 IAM 目标目录能力，当前独立路由仍待首次 SQL seed 前的覆盖门禁确认。

管理路由前缀：`/api/v1/service`。

- 资产发现使用 `GET /api/v1/service/assets/discoverable`，只接受 `addp-asset` Tenant Service Access Token，并校验 `service.definition.read`；Tenant 只来自 canonical AuthContext。
- 端点投影使用唯一 `GET /api/v1/service/endpoints?ref=`：只接受 `addp-portal` Tenant Service Access Token，并校验 `service.endpoint.read`。它只返回端点元数据，不替代真实服务执行时的用户 Resource Grant；旧 `/internal/endpoints`、内部密钥和 Tenant Header 路径必须删除。
- 查询服务管理：`POST/GET /query`、`GET/PUT/DELETE /query/:id`；公开执行端点：`GET /api/query/:serviceName`。
- 图查询服务管理：`POST/GET /graph`、`GET/PUT/DELETE /graph/:id`；公开执行端点：`POST /api/gquery/:serviceName`。
- 注册服务管理：`POST/GET /registered`、`GET/PUT/DELETE /registered/:id`、`POST /registered/:id/refresh`、`POST /registered/:id/health`；公开代理：`ANY /api/service/registered/proxy/:id/*path`。
- 瓦片服务管理：`POST/GET /tile`、`GET /tile/search`、`GET /tile/by-name/:serviceName`、`GET/PUT/DELETE /tile/:id`、`/tile-layers/:serviceId`。
- 数据查询辅助：`POST /data/query`、`POST /data/aggregate`、`GET /data/structure`，资源输入统一为 `locator`，由后端派生执行所需 `engine_id/schema/table`。
- 资源能力辅助：资源选择、资源树、表级空间元数据统一走 Meta resource-tree / item API；Service 仅保留 `GET /graphs/node-shapes` 和 `POST /sql/output-contract` 等业务能力接口。
- OGC/瓦片公开端点：`/ogc/features/:serviceName/*`、`/tiles/:serviceName/:layerName/:z/:x/*yformat`、`/wmts/:serviceName`、`/ogc/tiles/:serviceName/*`。

## 开发规则

- 存储引擎连接信息必须从 System 获取，Service 不管理连接配置。
- 表结构、空间信息和资源树通过 Meta 共享能力获取；Service 不重复实现资源树、表空间检测或按 `schema/table` 查找资源的代理接口。
- 静态二维瓦片发布只接受 Meta 已识别、位于 Business 存储的 `data_type=media + format=pmtiles + layout=single` item。发布配置保存 ResourceLocator 和 PMTiles v3 依赖快照，运行时通过 System engine provider Range Read，不接受裸路径、URL 或 Manager infra `storage_ref`。
- 三维瓦片不并入二维瓦片服务。3D Tiles / S3M 后续使用独立“三维场景服务”入口和服务类型。
- 公开访问端点要在 Handler 内检查服务的 public/private 权限，避免绕过认证。
- `/api/v1/service` 管理 API 只接受 canonical Bearer Tenant AuthContext；内部 API Key 不得跳过认证或伪造用户。匿名公开端点可选解析 Bearer，但 private 服务必须校验当前 AuthContext Tenant 与服务 Tenant 一致。
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
