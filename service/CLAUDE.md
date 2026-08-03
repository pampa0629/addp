# Service 模块说明

## 模块定位

Service 模块负责数据服务发布与外部服务注册，覆盖查询服务、图查询服务、注册服务代理、瓦片服务、OGC API Features、OGC Tiles、WMTS 和公开访问端点。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8086`，环境变量 `SERVICE_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + OpenLayers，开发端口 `5180`，启动脚本环境变量 `SERVICE_FE_PORT`。
- 数据库：PostgreSQL `service` schema。
- 依赖：System、Manager、Meta、DuckDB Federated Query Runtime、Gateway、Redis、MinIO。

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
- 查询服务管理：`POST/GET /query`、`GET/PUT/DELETE /query/:id`；公开执行端点：`POST /api/query/:serviceName/query`。
- 图查询服务管理：`POST/GET /graph`、`GET/PUT/DELETE /graph/:id`；公开执行端点：`POST /api/gquery/:serviceName`。
- 注册服务管理：`POST/GET /registered`、`GET/PUT/DELETE /registered/:id`、`POST /registered/:id/refresh`、`POST /registered/:id/health`；公开代理：`ANY /api/service/registered/proxy/:id/*path`。
- 瓦片服务管理：`POST/GET /tile`、`GET /tile/search`、`GET /tile/by-name/:serviceName`、`GET/PUT/DELETE /tile/:id`、`/tile-layers/:serviceId`。
- 数据查询辅助：`POST /data/query`、`POST /data/aggregate`、`GET /data/structure`，资源输入统一为 `locator`，由后端派生执行所需 `engine_id/schema/table`。
- 资源能力辅助：资源选择、资源树、表级空间元数据统一走 Meta resource-tree / item API；Service 仅保留 `GET /graphs/node-shapes` 和 `POST /sql/output-contract` 等业务能力接口。
- OGC/瓦片公开端点：`/ogc/features/:serviceName/*`、`/tiles/:serviceName/:layerName/:z/:x/*yformat`、`/wmts/:serviceName`、`/ogc/tiles/:serviceName/*`。

## 开发规则

- 存储引擎连接信息必须从 System 获取，Service 不管理连接配置。
- 查询服务执行目标必须显式且互斥：普通 SQL 和关系表只使用 `engine_id`；联邦 SQL 只使用 `runtime_engine_id`；Parquet 对象表同时保存 Source `engine_id` 与 DuckDB `runtime_engine_id`。不得通过 `engine_id IS NULL` 或 SQL 内容猜测执行模式。
- 查询服务 SQL 样例按 Engine capability 发现，不按 `engine_type` 固定列表。样例必须从当前业务 Catalog 构造，并在当前用户的 `service.definition.create + service.data_read.execute` 边界内以最多 10 行真实执行且返回非空数据后才能展示；展示给发布表单的是不含 `LIMIT/OFFSET` 的基础 SQL，由查询服务执行层统一分页，不得回退到 `SELECT 1`、硬编码业务表或在样例 SQL 内固化分页。
- 表、固定 SQL 和联邦 SQL 只表达查询服务的来源与执行绑定。REST Query、OGC API Features 和 WFS 必须共用唯一结构化查询内核；协议层不得拼接 SQL。发布契约必须包含非空唯一稳定排序键；业务数据查询统一使用 cursor/keyset 分页、读取 `limit + 1` 行判断下一页，默认不执行 `COUNT(*)`，不得保留 `page/offset`、原始 `filter/orderBy` 或兼容双轨。
- 联邦 SQL 发布时冻结实际引用的 Source Engine ID 并纳入 `dependency_hash`。每次请求由 Service 基于发布快照签发 `service_definition` Execution Authorization，独立 DuckDB Runtime 消费授权并取得连接；Service 不链接 DuckDB 原生库。
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

## 前端公开路由

- 模块内 Router 使用 `/query-services`、`/registered-services`、`/published-services`、`/tile`、`/graph-services` 等无模块前缀路径；Console 公开 URL 统一加 `/service` 前缀。
- 资源身份和创建、编辑、详情、测试职责使用 path 表达；创建成功后用 `replace` 进入详情，其余列表到详情使用 `push`。
- 服务目录默认 `all` Tab 省略，其他稳定类型使用唯一 `tab` query。
- 业务导航统一调用 `frontend/src/utils/moduleNavigation.js`。
