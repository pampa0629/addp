# Manager 模块说明

## 模块定位

Manager 模块负责数据探查、数据预览、表格数据剖析、混合检索、空间快显和瓦片缓存能力。它不管理存储引擎配置，存储引擎由 System 管理；Manager 通过 System、Meta 和实际数据源完成只读探查与预览。

数据剖析已经按确认边界实现：剖析执行和结果归 Manager，Meta 只提供 data item 身份、结构和源版本事实；首期在用户进入“剖析”标签时按需创建 `task_type=data_profiling` 的 ad-hoc execution，不创建持久任务定义、不声明 TaskProvider capability。完整规则见 `manager/docs/数据剖析规范.md`。

空间快显与瓦片缓存的目标边界：

- `manager.preview_state`：预览状态，表达某个 data item 的用户预览模式偏好与轻量交互设置（包括表格可见字段）；是否可快显、推荐渲染源和默认瓦片缓存结果由 Quick View Capability API 动态合成。
- `manager.vector_materialized_view`：矢量物化视图结果状态，只登记 Manager 创建并拥有生命周期的 3857 矢量物化视图目标；同源 schema 下自动识别的外部 3857 目标只读消费，不进入该表。
- `manager.vector_materialized_view_tasks`：矢量物化视图任务定义，TaskProvider `task_type=vector_materialized_view_generation`，当前不声明标准取消和自身定时调度能力。
- `manager.raster_cog`：栅格快显 COG生成结果，只登记 Manager 创建或登记到 infra MinIO 的 COG 副本；源 NFS 或业务 MinIO COG 不直接暴露给前端。
- `manager.raster_cog_tasks`：栅格快显 COG生成任务定义，TaskProvider `task_type=raster_cog_generation`，当前不声明标准取消和自身定时调度能力。
- `manager.raster_mosaic_tasks`：栅格 mosaic 生成任务定义，TaskProvider `task_type=raster_mosaic_generation`，从资源树 node 创建，结果写入用户选择的业务存储并形成 `raster_mosaic` 业务 item；Manager 不登记或拥有 mosaic 长期产物。
- `manager.vector_tile_cache`：快显缓存结果状态，表达 Manager infra PMTiles artifact 是否可用、存储引用、范围、层级、源版本、生成 profile 和最近执行。
- `manager.vector_tile_cache_tasks`：快显缓存生成任务定义，TaskProvider `task_type=vector_tile_cache_generation`，结果固定为 Manager infra PMTiles，当前不声明标准取消和自身定时调度能力。
- `manager.vector_tile_set_tasks`：业务矢量瓦片集生成任务定义，TaskProvider `task_type=vector_tile_set_generation`，结果固定为 Business PMTiles + Meta item；Manager 不建立业务结果表。
- `manager.point_cloud_copc`：点云 COPC 快显结果，只登记 Manager 生成并拥有生命周期的 infra MinIO COPC artifact；源 `format=copc` item 直接基础预览，不进入该表。
- `manager.point_cloud_copc_tasks`：点云 COPC 快显任务定义，TaskProvider `task_type=point_cloud_copc_generation`，源必须是 `format=las|laz|e57|pcd|xyz` 的 point_cloud item。
- `manager.cad_previews`：二维 DWG / DXF 的受管栅格瓦片预览结果，记录 manifest、thumbnail、瓦片范围和 Manager infra MinIO 引用。
- `manager.cad_preview_tasks`：CAD 栅格预览任务定义，TaskProvider `task_type=cad_preview_generation`，源必须是 `data_type=cad + format=dwg|dxf + layout=single`。
- `manager.embedding_configuration`：Manager 平台向量化业务策略单例，只保存距离阈值、文件大小、并发等 Manager-owned 策略。
- `manager.inference_scenario_bindings`：Manager-owned 的 `semantic_search_embedding` 场景绑定；保存平台默认和租户覆盖的 Inference Model Profile ID，不保存 Provider、端点、模型名或凭据。
- `manager.model3d_tiles`：分块三维模型瓦片快显结果，`target_format=3d_tiles|s3m`；同一源 item 的两种格式分别登记为独立结果并写入 Manager infra MinIO。
- `manager.model3d_tiles_tasks`：分块三维模型瓦片任务定义，TaskProvider `task_type=model3d_tiles_generation`；当前源为 `format=osgb_scene + layout=whole`。
- `vector_tile_cache_generation` 与 `vector_tile_set_generation` 统一由 Manager Backend 编排，并按源能力选择执行路径：PostgreSQL/PostGIS 空间表复用 `common/spatial` 和 `ST_AsMVT` 原生 SQL 生成 PMTiles；MySQL、Oracle 等不具备原生 MVT 输出、但具备标准 EWKB 表读取能力的数据库空间表，由 Manager 通过 `TableReadSessionProvider` 流式物化受控临时 FlatGeobuf，再调用 GeoPython Workflow `vector_to_pmtiles` direct operator；NFS、MinIO/S3 文件或对象转换成受控 GDAL 访问计划后调用同一 operator。每类源只有一条执行路径，最终只生成 PMTiles v3，不保留松散 MVT 目录；临时 FlatGeobuf 由本次 execution 管理并清理。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8081`，环境变量 `MANAGER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + OpenLayers，开发端口 `5174`，启动脚本环境变量 `MANAGER_FE_PORT`。
- 数据库：PostgreSQL `manager` schema。
- 依赖：System、Meta、Inference、Redis、MinIO、Meilisearch。

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
│   ├── 数据剖析规范.md
│   ├── 三维模型、点云与高斯泼溅预览说明.md
│   ├── 存储流与原始下载语义.md
│   ├── 向量化概念说明.md
│   ├── 向量化能力说明.md
│   └── tables/               # 表级说明：search_history、embeddings、preview_state、vector_tile_cache 等
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
- 数据剖析：`GET /data-profiles/current` 查询当前成功结果、新鲜度和 execution；`POST /data-profile-executions` 创建或复用 `data_profiling` ad-hoc execution。
- 搜索：`GET /search`、`GET /search/history`、`DELETE /search/history/:id`、`DELETE /search/history`；`GET /search` 的可选 `engine_id` 必须在全文与向量检索 owner 侧同时生效，供查询工作台等调用方把候选严格限定到当前引擎，禁止先全局截断再由调用方过滤。
- 空间要素辅助：`GET /engines/:id/spatial/features/:feature_id/centroid`、`GET /engines/:id/spatial/features/:feature_id/geometry`。
- 预览状态与 Quick View：`GET /preview-state?locator={ResourceLocator}` 返回任意 data item 的用户预览设置，`PATCH /preview-state/view-state` 更新地图视口、三维相机或表格可见字段，`PATCH /preview-state/preferred-mode` 更新空间显示模式；Quick View 统一使用 ResourceLocator 入口，`GET /quick-view/capability?locator={ResourceLocator}` 返回快显能力状态，`GET /quick-view/flatgeobuf?locator={ResourceLocator}` 返回中小规模矢量 FlatGeobuf 快显材料，`GET /quick-view/geojson?locator={ResourceLocator}` 保留为 GeoJSON 调试/人类可读出口，`GET /quick-view/tiles/:z/:x/:y.mvt?locator={ResourceLocator}` 从实时源或 infra PMTiles 返回 MVT，`GET /raster_cog/:id/content` 返回 ready raster COG 内容；业务 PMTiles 通过 `vector_tile_set_generation` 生成或由合格缓存固化。
- 点云快显：`GET /point_cloud_copc/:id/content` 返回 ready COPC 快显内容；LAS / LAZ / E57 / PCD / XYZ 通过 `point_cloud_copc_generation` 生成 Manager 私有 COPC artifact，源 COPC 直接基础预览。
- CAD 预览：`GET /cad-previews/:id/manifest` 返回 ready manifest，`GET /cad-previews/:id/tiles/:z/:x/:y` 返回 WebP 瓦片；DWG / DXF 通过 `cad_preview_generation` 调用 SuperMap 直接渲染 Dataset，前端不读取源 CAD 文件、不重画 entity。
- 分块三维模型瓦片：`GET /model3d_tiles` 查询 Manager 受管结果，`GET /model3d_tiles/:id/assets/*asset_path` 返回 ready 3D Tiles / S3M 目录资源；生成入口统一由 Quick View action 驱动，独立管理页只负责任务、结果、监控与预览入口。
- 任务提供者：`GET /tasks`、`GET /tasks/:task_type/:id`、`POST /tasks/:task_type/:id/execute`、`GET /executions/:execution_id`。
- 数据进出与向量化：`POST /uploads`、`POST /imports`、`POST /exports`、`GET /exports/:id/file`、`POST /embedding_executions`、`GET /embeddings`、`GET /items/:item_id/embedding`。
- 平台配置管理：`GET /settings/embedding`、`PUT /settings/embedding`；只接受 Platform Context 和 `manager.configuration.read/update`。

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
- 表格数据剖析只按 `data_type=table` 和当前内容选择上下文开放；不得按 `item_type`、engine type 或文件扩展名硬编码。首期剖析是 `data_profiling` ad-hoc execution，结果写 Manager 私有表，不写 Meta attributes，不创建 `manager.data_profile_tasks`，也不声明 TaskProvider capability。
- 数据剖析不得使用当前预览页、分页记录或前端数组计算；采样和指标计算必须走统一 Provider 与服务端预算。刷新失败必须保留上一份成功结果。
- 条件剖析只接受结构化 `data_scope`，条件必须由声明支持的 Provider 在采样前执行并安全绑定参数；全范围和条件范围按 `profile_config_hash` 分别保存。Manager 不接受任意 SQL，也不得退回到采样后过滤。
- 空间相关逻辑不得默认几何字段名为 `geom`，应从 Meta、预览检测或请求参数获取。
- 不得把 Quick View 称为任务；瓦片缓存生成任务统一使用 `vector_tile_cache_generation` / `manager.vector_tile_cache_tasks`。
- “空间任务”是 Manager 中空间业务任务的导航与能力分类，不是统一任务表或单一 `task_type`；当前“矢量瓦片”对应 `vector_tile_set_generation`。
- 业务矢量瓦片集生成任务统一使用 `vector_tile_set_generation` / `manager.vector_tile_set_tasks`；结果只写用户选择的 Business 存储并触发 Meta scan，不进入 `manager.vector_tile_cache`。
- “保存为业务瓦片集”必须创建或执行 `vector_tile_set_generation`。ready 缓存仅在源版本和生成 profile 完全一致时作为执行复用候选；复制必须使用临时对象、PMTiles 校验和原子提交，成功后再触发 Meta scan。
- 矢量物化视图任务统一使用 `vector_materialized_view_generation` / `manager.vector_materialized_view_tasks`；结果只登记 Manager 创建并拥有生命周期的 3857 目标。
- 栅格快显 COG生成结果统一使用 `manager.raster_cog`，任务定义统一使用 `manager.raster_cog_tasks` / `raster_cog_generation`；不得写入 `vector_tile_cache` 或 `vector_materialized_view_generation`，不得让前端感知 NFS path、业务 MinIO bucket/object 或底层 `storage_ref`。
- 栅格 mosaic 生成任务统一使用 `manager.raster_mosaic_tasks` / `raster_mosaic_generation`；创建入口是资源树 node，目标是用户选择的业务存储，结果是 `data_type=media`、`format=raster_mosaic`、`layout=whole` 的业务 item。mosaic 的 leaf COG、overview COG、index、manifest 和可选 tiles 都不写入 Manager infra MinIO，不进入 `manager.raster_cog`。
- 点云 COPC 快显任务统一使用 `manager.point_cloud_copc_tasks` / `point_cloud_copc_generation`；源必须是 `data_type=point_cloud + layout=single + format=las|laz|e57|pcd|xyz`，结果写入 `manager.point_cloud_copc` 和 Manager infra MinIO，不自动升格为业务 data item。源 `format=copc` 只走基础预览，不创建二次快显任务；XYZ 第一阶段只支持简单确定性文本 XYZ。
- CAD 栅格预览任务统一使用 `manager.cad_preview_tasks` / `cad_preview_generation`；源必须是 `data_type=cad + layout=single + format=dwg|dxf`。后端只把 ready artifact 的受控 manifest URL 交给 `frontend_renderer=cad`，不得把源 CAD `storage-stream` URL 交给 CAD renderer。
- 分块三维模型瓦片任务统一使用 `manager.model3d_tiles_tasks` / `model3d_tiles_generation`，结果统一进入 `manager.model3d_tiles`；`target_format=3d_tiles` 调用 `osgb_scene_to_3dtiles`，`target_format=s3m` 调用 `osgb_scene_to_s3m`。不得恢复写业务存储并触发 Meta scan 的旧 Manager 路径。
- Manager 受管快显任务的语义身份统一为 `tenant_id + item_fingerprint + artifact_variant`。重复创建必须复用原任务 ID，重复执行必须新建 execution 并刷新同一当前结果；`item_id`、`locator`、`source_engine_id` 只作执行与回查事实。派生变体、并发唯一约束和业务派生任务的例外见 `manager/docs/快显实现规范.md`。
- Raster COG 与 Model3D Tiles 是来源驱动的只读任务定义：只允许 Data Explorer 的 Quick View action 按源事实派生，不提供直接创建或更新任务配置的 API。管理页任务 Tab 的 `task_id` 打开只读任务定义，结果 Tab 的 `task_id` 只筛选该任务结果。
- Manager 受管当前结果任务统一不启动自身定时调度，但允许 Orchestrator 定时 Pipeline 调用。周期性刷新由用户在 Step 参数中显式配置 `existing_result_action=overwrite`；Manager 不得按 scheduled 来源自动补充已有结果动作。Embedding 的逐 item 调度语义独立保留。
- COG 生成只能由 Manager 任务执行器派生 GDAL 参数后，通过 `WorkflowRuntimeProvider.InvokeOperator("tiff_to_cog")` direct 调用 GeoPython Workflow；不得退回构造单节点 workflow 或直接拼接 GeoPython Workflow 私有 HTTP。
- 瓦片缓存生成任务不得隐式创建 3857 物化视图、空间索引或执行准备动作；需要性能准备时必须显式执行矢量物化视图任务。
- 自动识别的外部 3857 目标只能只读消费，不写入 `manager.vector_materialized_view`，也不获得 Manager 删除、刷新或 stale 生命周期。
- 批量矢量瓦片生成按源能力分流且不得交叉：PostgreSQL/PostGIS 空间表必须由 Manager 复用 `common/spatial` 的 `ST_AsMVT` SQL 生成；MySQL、Oracle 等标准 EWKB 可读、但无原生 MVT 输出的数据库空间表必须先流式物化受控临时 FlatGeobuf，再由 GeoPython Workflow `vector_to_pmtiles` 生成；文件和对象来源必须由受控 GDAL 访问计划调用同一 operator。三类来源统一封装为 PMTiles v3，Manager 内部不得恢复松散 MVT 目录、私有 manifest 或同源备用路线。
- 快显瓦片缓存的默认最大层级必须同时受记录数和累计候选瓦片预算约束；默认预算为 10000 个 WebMercatorQuad 矩形候选瓦片。前端必须显示当前层级的预计候选数，用户可显式提高层级，但不得把 `max_zoom=18` 作为无条件默认值。
- 业务矢量瓦片集页面复用同一候选瓦片估算能力，但不把快显预算作为业务生成硬限制；后端推荐层级与用户当前选择必须分开显示，用户超过推荐层级时提示预计生成规模和成本风险。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh manager` 和 `bash scripts/swagger/check-route-coverage.sh manager`。

## 前端公开路由

- Manager 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`，不得直接形成 iframe 私有历史。
- Data Explorer 使用 ResourceLocator 表达当前资源身份，默认 `preview` Tab 从 URL 省略，非默认稳定 Tab 使用 `tab` query。
- 快显与空间任务管理页的稳定 Tab、当前任务筛选和创建来源参数必须保留在 canonical query 中；Tab、筛选和参数规范化使用 `replace`，跨页面进入资源或任务使用 `push`。
- Raster COG 与 Model3D Tiles 的创建入口固定为 Data Explorer；TaskProvider `edit_url` 分别指向对应管理页的 `?task_id=:id` 只读任务定义，不得指向结果筛选或无任务身份的通用页面。

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
- `manager/docs/数据剖析规范.md`
- `manager/docs/三维模型、点云与高斯泼溅预览说明.md`
- `manager/docs/存储流与原始下载语义.md`
- `manager/docs/tables/preview_state表.md`
- `manager/docs/tables/vector_materialized_view表.md`
- `manager/docs/tables/vector_materialized_view_tasks表.md`
- `manager/docs/tables/model_3d_glb表.md`
- `manager/docs/tables/model_3d_glb_tasks表.md`
- `manager/docs/tables/model3d_tiles表.md`
- `manager/docs/tables/model3d_tiles_tasks表.md`
- `manager/docs/tables/gaussian_splat_ksplat表.md`
- `manager/docs/tables/gaussian_splat_ksplat_tasks表.md`
- `manager/docs/tables/point_cloud_copc表.md`
- `manager/docs/tables/point_cloud_copc_tasks表.md`
- `manager/docs/tables/raster_cog表.md`
- `manager/docs/tables/raster_cog_tasks表.md`
- `manager/docs/tables/raster_mosaic_tasks表.md`
- `manager/docs/tables/vector_tile_cache表.md`
- `manager/docs/tables/vector_tile_cache_tasks表.md`
- `manager/docs/tables/embeddings表.md`
- `manager/docs/tables/embedding_tasks表.md`
- `manager/docs/tables/embedding_configuration表.md`
- `manager/docs/tables/search_history表.md`
- `common-frontend/CLAUDE.md`
- `meta/CLAUDE.md`
