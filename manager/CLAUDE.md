# Manager 模块说明

## 模块定位

Manager 模块负责数据探查、数据预览、混合检索、空间快显和瓦片缓存能力。它不管理存储引擎配置，存储引擎由 System 管理；Manager 通过 System、Meta 和实际数据源完成只读探查与预览。

空间快显与瓦片缓存的目标边界：

- `manager.quick_view`：快显偏好，表达某个 spatial item 的用户预览模式偏好；是否可快显、推荐渲染源和默认瓦片缓存结果由 Quick View Capability API 动态合成。
- `manager.quick_view_optimization`：快显性能优化结果状态，只登记 Manager 创建并拥有生命周期的 3857 快显优化目标；同源 schema 下自动识别的外部 3857 目标只读消费，不进入该表。
- `manager.quick_view_optimization_tasks`：快显性能优化任务定义，TaskProvider `task_type=quick_view_optimization`，当前不声明标准取消和自身定时调度能力。
- `manager.tile_cache`：瓦片缓存结果状态，表达瓦片缓存是否可用、存储引用、格式、范围、层级和最近执行。
- `manager.tile_cache_tasks`：瓦片缓存生成任务定义，TaskProvider `task_type=tile_cache_generation`。MVT 只是 `config.tile.format=mvt`，不是任务类型。
- 当前 PostGIS + MVT 主路径中的 `tile_cache_generation` 由 Manager Backend 内部执行，不保留独立 Manager Worker 空运行时。后续若瓦片缓存生成计算负载转移到 Manager 进程内、需要多执行器横向扩展，或引入专门 GIS 计算引擎，应先统一文档与架构，再把唯一执行运行时切换为 Manager Worker 或 GIS 执行引擎。

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
│   ├── internal/service/      # preview registry、瓦片缓存、Quick View、搜索、缓存
│   ├── internal/mvt/          # 当前 MVT 瓦片生成与预处理实现
│   └── docs/                  # Swagger 产物
├── docs/
│   ├── 数据库架构.md
│   ├── 快显概念说明.md
│   ├── 快显实现规范.md
│   ├── 数据预览与资源树实现规范.md
│   ├── 数据预览语义协议.md
│   ├── 存储流与原始下载语义.md
│   ├── 向量化概念说明.md
│   ├── 向量化能力说明.md
│   └── tables/               # 表级说明：search_history、embeddings、quick_view、tile_cache 等
└── frontend/src/
    ├── views/                 # DataExplorer、DataRetrieval、Preview、SpatialPreview
    ├── components/explorer/
    ├── components/map/
    └── plugins/previews/
```

## 核心 API

路由前缀：`/api/v1/manager`。

- 数据探查：`GET /engines`；资源树事实读取、搜索和刷新统一使用 Meta `/api/v1/meta/resource-tree/:engine_id...`。
- 预览与下载：`GET /preview`、`GET /storage-stream`、`GET /downloads/file?locator={ResourceLocator}`。
- 搜索：`GET /search`、`GET /search/history`、`DELETE /search/history/:id`、`DELETE /search/history`。
- 空间要素辅助：`GET /engines/:id/spatial/features/:feature_id/centroid`、`GET /engines/:id/spatial/features/:feature_id/geometry`。
- Quick View：统一使用 ResourceLocator 入口，`GET /quick-view/capability?locator={ResourceLocator}` 返回快显能力状态，`GET /quick-view/geojson?locator={ResourceLocator}` 返回 GeoJSON，`GET /quick-view/tiles/:z/:x/:y.mvt?locator={ResourceLocator}` 返回 MVT，`PATCH /quick-view/preferred-mode` 更新显示偏好；瓦片缓存生成通过 `tile_cache_generation` 任务执行，产物通过 `/tile_cache` 管理。
- 任务提供者：`GET /tasks`、`GET /tasks/:task_type/:id`、`POST /tasks/:task_type/:id/execute`、`GET /executions/:execution_id`。
- 数据进出与向量化：`POST /uploads`、`POST /imports`、`POST /exports`、`GET /exports/:id/file`、`POST /embedding_executions`、`GET /embeddings`、`GET /items/:item_id/embedding`。

## 开发规则

- 数据源连接信息必须通过 System 获取，不要在 Manager 中保存或硬编码连接配置。
- 元数据树与数据项优先通过 Meta 查询，Manager 只做预览、检索和快显侧的缓存与呈现。
- 预览能力走 `PreviewRegistry` 和 provider，不要为单一数据源在 Handler 中写特殊逻辑。
- 资源树、预览、刷新和跨页面跳转统一使用 ResourceLocator；不得恢复 `engine_id/schema/table` 公共预览入口。
- 预览响应材料必须遵守 `content.kind`、`preview_material`、`frontend_renderer` 三层语义；不得把 `raw_content`、`range_content`、`binary_content` 写入 `preview_material`。
- 存储型 item 原始下载走 `downloads/file` 的 ResourceLocator + DownloadPlan；前端不得从 preview metadata refs 拼接 multi 文件下载。
- 向量化用户界面使用“向量化”，英文 API、表名和 TaskProvider `task_type` 统一使用 `embedding`；不得新增 `vectorization` 双轨路径。
- 向量化对象只能是 data item；资源树 node 只是批量选择范围，不产生 node 向量化结果。
- 资源树 item / node 向量化是 ad-hoc execution，不写入 `manager.embedding_tasks`；只有独立向量化页面创建的配置才是任务定义。
- 空间相关逻辑不得默认几何字段名为 `geom`，应从 Meta、预览检测或请求参数获取。
- 不得把 Quick View 称为任务；瓦片缓存生成任务统一使用 `tile_cache_generation` / `manager.tile_cache_tasks`。
- 快显性能优化任务统一使用 `quick_view_optimization` / `manager.quick_view_optimization_tasks`；结果只登记 Manager 创建并拥有生命周期的 3857 目标。
- 瓦片缓存生成任务不得隐式创建 3857 物化视图、空间索引或执行准备动作；需要性能准备时必须显式执行快显性能优化任务。
- 自动识别的外部 3857 目标只能只读消费，不写入 `manager.quick_view_optimization`，也不获得 Manager 删除、刷新或 stale 生命周期。
- 不得同时保留 Backend 内执行和 Manager Worker 执行两条瓦片缓存生成路径；运行时只能有一条主路径。
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
- `manager/docs/快显概念说明.md`
- `manager/docs/快显实现规范.md`
- `manager/docs/向量化概念说明.md`
- `manager/docs/向量化能力说明.md`
- `manager/docs/数据预览与资源树实现规范.md`
- `manager/docs/数据预览语义协议.md`
- `manager/docs/存储流与原始下载语义.md`
- `manager/docs/tables/quick_view表.md`
- `manager/docs/tables/quick_view_optimization表.md`
- `manager/docs/tables/quick_view_optimization_tasks表.md`
- `manager/docs/tables/tile_cache表.md`
- `manager/docs/tables/tile_cache_tasks表.md`
- `manager/docs/tables/embeddings表.md`
- `manager/docs/tables/embedding_tasks表.md`
- `manager/docs/tables/search_history表.md`
- `common-frontend/CLAUDE.md`
- `meta/CLAUDE.md`
