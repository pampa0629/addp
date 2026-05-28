# Manager 模块说明

## 模块定位

Manager 模块负责数据探查、数据预览、混合检索、空间瓦片和 Quick View 快显能力。它不管理存储引擎配置，存储引擎由 System 管理；Manager 通过 System、Meta 和实际数据源完成只读探查与预览。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8081`，环境变量 `MANAGER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + OpenLayers，开发端口 `5174`，启动脚本环境变量 `MANAGER_FE_PORT`。
- 数据库：PostgreSQL `manager` schema。
- 依赖：System、Meta、Redis、MinIO、Meilisearch，可选向量化服务。

## 重要目录

```text
manager/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/api/          # explorer、search、quick-view、tiles、import、embedding
│   ├── internal/service/      # preview registry、MVT、Quick View、搜索、缓存
│   ├── internal/mvt/          # 瓦片生成与预处理
│   └── docs/                  # Swagger 产物
├── docs/
│   ├── 数据库架构.md
│   ├── addp-mvt瓦片两阶段及配置说明.md
│   └── tables/
└── frontend/src/
    ├── views/                 # DataExplorer、DataRetrieval、Preview、SpatialPreview
    ├── components/explorer/
    ├── components/map/
    └── plugins/previews/
```

## 核心 API

路由前缀：`/api/v1/manager`。

- 数据探查：`GET /engines`、`GET /tree/:engine_id`、`GET /tree/:engine_id/node`、`GET /tree/:engine_id/search`、`POST /tree/:engine_id/refresh`。
- 预览：`GET /preview`、`GET /video-stream`、`GET /graph-schema/:engine_id`。
- 搜索：`GET /search`、`GET /search/history`、`DELETE /search/history/:id`、`DELETE /search/history`。
- 空间与瓦片：`GET /engines/:id/spatial/features/:feature_id/centroid`、`GET /engines/:id/spatial/features/:feature_id/geometry`、`GET /engines/:id/spatial/:schema/:table/geojson`、`GET /engines/:id/spatial/tiles/:schema/:table/:z/:x/:y`。
- Quick View：`POST /engines/:id/spatial/:schema/:table/quick-view/prepare`、`POST /engines/:id/spatial/:schema/:table/quick-view/pre-cache`、`GET /engines/:id/spatial/:schema/:table/quick-view/status`。
- 任务提供者：`GET /tasks`、`GET /tasks/:task_type/:id`、`POST /tasks/:task_type/:id/execute`、`GET /executions/:execution_id`。
- 数据导入与向量化：`POST /import`、`POST /embedding`、`GET /embedding/tasks/:task_id`。

## 开发规则

- 数据源连接信息必须通过 System 获取，不要在 Manager 中保存或硬编码连接配置。
- 元数据树与数据项优先通过 Meta 查询，Manager 只做预览、检索和快显侧的缓存与呈现。
- 预览能力走 `PreviewRegistry` 和 provider，不要为单一数据源在 Handler 中写特殊逻辑。
- 空间相关逻辑不得默认几何字段名为 `geom`，应从 Meta、预览检测或请求参数获取。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh manager` 和 `bash scripts/swagger/check-route-coverage.sh manager`。

## 开发与验证

```bash
bash scripts/dev/start.sh -manager
bash scripts/dev/restart.sh -manager
curl http://localhost:8081/health
```

常用日志：

- `logs/manager-backend.log`
- `logs/manager-backend-stderr.log`

## 相关文档

- `manager/docs/数据库架构.md`
- `manager/docs/addp-mvt瓦片两阶段及配置说明.md`
- `manager/docs/数据预览API重构方案.md`
- `manager/docs/数据预览语义协议.md`
- `manager/docs/tables/quick_view表.md`
- `manager/docs/tables/search_history表.md`
- `common-frontend/CLAUDE.md`
- `meta/CLAUDE.md`
