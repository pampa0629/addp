# Manager 模块说明

本文件维护模块职责、开发约束和阅读入口。详细契约在对应规范中维护；跨模块验收规则和历史证据分别见[场景规范](../docs/spec/testing/scenarios/Manager与空间成果.md)与[验收证据索引](../docs/spec/testing/验收证据索引.md)。

## 模块定位

Manager 模块负责数据探查、数据预览、表格数据剖析、混合检索、空间快显和瓦片缓存能力。它不管理存储引擎配置，存储引擎由 System 管理；Manager 通过 System、Meta 和实际数据源完成只读探查与预览。Manager 的引擎资源树是技术浏览视图，不是 Enterprise Catalog，也不拥有企业 `CatalogEntry`、业务语义关联或责任事实。Catalog 落地后，Manager 只按需读取企业资源摘要并跳转企业资源目录；Catalog 不可达只影响该摘要和跳转，不得回退到旧 Asset 发现或在 Manager 内复制企业资源目录。

数据剖析执行和结果归 Manager，Meta 只提供身份、结构、源版本和扫描事实。剖析按需创建 `data_profiling` ad-hoc execution，不创建持久任务定义、不声明 TaskProvider capability；首版采样只开放已扫描 PostgreSQL 单表，视图、物化视图和多来源计划不支持。独立 execution 源授权、逐页／提交前复核及源端并发 DDL 的未覆盖边界，以[数据剖析规范](docs/数据剖析规范.md#当前授权切换边界)为准。

预览必须使用当前 User 或固定 `data.preview` 委托身份，经同一 Provider PreparedQuery 证明完整 ReadSet、核验 System 源授权并执行本地 Security 保护；不回退 ReadBatch 或借用服务身份。保护必须在响应输出前执行，空页也不能暴露被抑制列；具体规则见[预览实现规范](docs/数据预览与资源树实现规范.md)。

预览、剖析和全文索引只消费本地持久保护投影，Tenant HTTP／有界执行生命周期与 Security 回执屏障保持独立。剖析的保护版本核验与结果落库使用同一 checkpoint 事务；Meilisearch 投递使用持久关联标记和完成回执，未知状态不得重发或提前开放搜索。完整规则及多 Backend／HA 未认证边界见[数据安全规范](../docs/spec/addp数据安全与隐私保护实现规范.md)和[剖析规范](docs/数据剖析规范.md)。旧 Meilisearch 数据卷迁移须按[Infra 说明](../scripts/infra/README.md)另行确认。

Manager 导入使用持久 Transfer `sync` 任务并保持上传者私有来源；来源记录只保存成功上传的 Tenant、Uploader、UUID、精确 source locator 与一次绑定的 Transfer task ID，未绑定或主体／任务不符时拒绝，不从路径推断所有权。一次性导出使用 Common `exportartifact` 与 Transfer bounded `sync` ad-hoc execution，导出会话只保存 execution ID，不保存 Transfer task ID。两类操作不共用状态机；来源核验、进度与扫描完成、导航顺序见[预览与资源动作规范](docs/数据预览与资源树实现规范.md#4-与导入和导出)。

成功执行的资源血缘由真实读写 owner 生成，清理执行不表达派生血缘；Manager 调用 Transfer 的导入导出不重复生成血缘。mosaic 必须在原 lease／超时内取得自动扫描的正式 whole DataItem 身份后才成功，失败保留物理产物和扫描关联，不自动重放或终态补写。执行事实、专业安全 DTO 和 Runtime 耗时投影见[任务体系规范](../docs/spec/addp任务体系规范.md)。

快显能力与预览状态只解析 Engine／Meta／受管产物事实，不读取样本行；SRID 只核验目录类型声明，生成端保留 2D／Z／M／ZM。类型检查不能代替内容授权，未声明旧目标由用户显式重新生成；细则见[快显实现规范](docs/quick-view/共用契约.md#快显能力)。

## 领域事实与任务边界

- `manager.preview_state`：预览状态，表达某个 data item 的用户预览模式偏好与轻量交互设置（包括表格可见字段）；是否可快显、推荐渲染源和默认瓦片缓存结果由 Quick View Capability API 动态合成。
- `manager.task_definitions`：Manager 生成类任务定义的唯一表；`task_type` 选择强类型配置、校验器和执行器，产品分类投影为“快显任务”与“空间数据任务”。向量化任务继续使用 `manager.embedding_tasks`，三类任务只在统一“数据任务”工作台合并呈现，不合并存储和结果生命周期。
- `manager.task_resource_bindings`：任务定义对源/目标资源的规范化绑定，供引擎生命周期检查和资源回收使用；不得再从异构 `config` JSON 猜测资源归属。
- `manager.vector_materialized_view`：矢量物化视图结果状态，只登记 Manager 创建并拥有生命周期的 3857 矢量物化视图目标；同源 schema 下自动识别的外部 3857 目标只读消费，不进入该表。
- `manager.raster_cog`：栅格快显 COG生成结果，只登记 Manager 创建或登记到 infra MinIO 的 COG 副本；源 NFS 或业务 MinIO COG 不直接暴露给前端。
- `manager.vector_tile_cache`：快显缓存结果状态，表达 Manager infra PMTiles artifact 是否可用、存储引用、范围、层级、源版本、生成 profile 和最近执行。
- `manager.point_cloud_copc`：点云 COPC 快显结果，只登记 Manager 生成并拥有生命周期的 infra MinIO COPC artifact；源 `format=copc` item 直接基础预览，不进入该表。
- `manager.embedding_configuration`：Manager 平台向量化业务策略单例，只保存距离阈值、文件大小、并发等 Manager-owned 策略。
- `manager.inference_scenario_bindings`：Manager-owned 的 `semantic_search_embedding` 场景绑定；保存平台默认和租户覆盖的 Inference Model Profile ID，不保存 Provider、端点、模型名或凭据。
- `manager.model3d_tiles`：分块三维模型瓦片快显结果，`target_format=3d_tiles|s3m`；同一源 item 的两种格式分别登记为独立结果并写入 Manager infra MinIO。
- `vector_tile_cache_generation` 与 `vector_tile_set_generation` 统一由 Manager Backend 编排，并按源能力选择执行路径：PostgreSQL/PostGIS 空间表复用 `common/spatial` 和 `ST_AsMVT` 原生 SQL 生成 PMTiles；MySQL、Oracle 等不具备原生 MVT 输出、但具备标准 EWKB 表读取能力的数据库空间表，由 Manager 通过 `TableReadSessionProvider` 流式物化受控临时 FlatGeobuf，再调用 GeoPython Workflow `vector_to_pmtiles` direct operator；NFS、MinIO/S3 文件或对象转换成受控 GDAL 访问计划后调用同一 operator。每类源只有一条执行路径，最终只生成 PMTiles v3，不保留松散 MVT 目录；临时 FlatGeobuf 由本次 execution 管理并清理。

## 技术栈与端口

- 后端：Go + Gin + GORM，默认端口 `8081`，环境变量 `MANAGER_BACKEND_PORT`。
- 前端：Vue 3 + Element Plus + OpenLayers，开发端口 `5174`，启动脚本环境变量 `MANAGER_FE_PORT`。
- 数据库：PostgreSQL `manager` schema。
- 依赖：System、Meta、Security、Inference、Redis、MinIO、Meilisearch。数据读取须先完成当前租户的 Security 保护投影首次同步；未同步时按保护边界拒绝，验收环境也必须启动 Security。

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

数据检索的结果对象仍为 DataItem。字段命中只来自搜索服务实际高亮的已登记事实，页面展示上下文并转义源文本；纯向量命中不声明字段命中。技术／正文快照分别幂等更新、精确删除、结构覆盖范围及历史补录均以 `docs/spec/addp元数据扫描机制规范.md` 的“DataItem 技术元数据搜索投影”和“技术投影的检索消费与覆盖边界”为准，不在本入口另存一份契约。验证沿用 `make test-manager-frontend`、`make test-manager-postgres`、`make test-go` 及 Swagger 标准检查。

路由前缀：`/api/v1/manager`。

- 数据探查：`GET /engines`；资源树事实读取、搜索和刷新统一使用 Meta `/api/v1/meta/resource-tree/:engine_id...`。
- 预览与下载：`GET /preview`、`GET /storage-stream`、`GET /downloads/file?locator={ResourceLocator}`。
- 文件和对象存储中的业务 PMTiles 统一返回带原始 locator 的 `GET /quick-view/tiles/{z}/{x}/{y}.mvt` 模板，不能把整文件 `storage-stream` 地址交给 MVT 加载器。NFS 目录预览与 MinIO 对象预览均须保留同一数据项身份；`make test-manager-backend` 自动覆盖该映射和不读取整份归档的回归，既有 Manager Go CI 沿标准入口发现。
- 数据剖析：`GET /data-profiles/current` 查询当前成功结果、新鲜度和 execution；`POST /data-profile-executions` 创建或复用 `data_profiling` ad-hoc execution。
- 搜索：`GET /search`、`GET /search/history`、`DELETE /search/history/:id`、`DELETE /search/history`；`GET /search` 只负责 DataItem 内容、全文、向量和空间检索，不承担企业业务元数据搜索。Manager 独占 `MEILISEARCH_MANAGER_CONTENT_INDEX`；Meta 只通过 `POST /runtime/content-documents/batch_update` 和 `DELETE /runtime/content-documents` 提交或删除当前 Tenant 投影，不能直接读写该索引。写入载荷必须显式声明 `technical_metadata|extracted_content`；前者不携带数据值且不进入 Security 动作，后者在已纳管时必须本地命中有效 `search_index` 规则并先执行保护。可选 `engine_id` 必须在全文与向量检索 owner 侧同时生效，供查询工作台等调用方把候选严格限定到当前引擎，禁止先全局截断再由调用方过滤。
- 空间要素辅助：`GET /engines/:id/spatial/features/:feature_id/centroid`、`GET /engines/:id/spatial/features/:feature_id/geometry`。
- 预览状态与 Quick View：`GET /preview-state?locator={ResourceLocator}` 返回任意 data item 的用户预览设置，`PATCH /preview-state/view-state` 更新地图视口、三维相机或表格可见字段，`PATCH /preview-state/preferred-mode` 更新空间显示模式；Quick View 统一使用 ResourceLocator 入口，`GET /quick-view/capability?locator={ResourceLocator}` 返回快显能力和受管产物状态，`POST /quick-view/actions` 是 COG、GLB、KSplat、COPC、3D Tiles / S3M 与 PPTX 静态 PDF 的唯一来源驱动生成入口，`GET /quick-view/flatgeobuf?locator={ResourceLocator}` 返回中小规模矢量 FlatGeobuf 快显材料，`GET /quick-view/geojson?locator={ResourceLocator}` 保留为 GeoJSON 调试/人类可读出口，`GET /quick-view/tiles/:z/:x/:y.mvt?locator={ResourceLocator}` 从实时源或 infra PMTiles 返回 MVT，`GET /raster_cog/:id/content` 返回 ready raster COG 内容；业务 PMTiles 通过 `vector_tile_set_generation` 生成或由合格缓存固化。
- 点云快显：`GET /point_cloud_copc/:id/content` 返回 ready COPC 快显内容；LAS / LAZ / E57 / PCD / XYZ 通过 `point_cloud_copc_generation` 生成 Manager 私有 COPC artifact，源 COPC 直接基础预览。
- CAD 预览：`data_type=cad + format=dwg|dxf + layout=single` 的源文件由普通 Preview API 返回受控 `storage-stream` URL；Manager 前端仅在进入 CAD 预览时加载 LibreDWG Worker/WASM 和 WebGL renderer，在浏览器内解析并绘制，不创建 Manager 快显任务或派生产物。
- 分块三维模型瓦片：`GET /model3d_tiles` 查询 Manager 受管结果，`GET /model3d_tiles/:id/assets/*asset_path` 返回 ready 3D Tiles / S3M 目录资源；生成入口统一由 Quick View action 驱动，独立管理页只负责任务、结果、监控与预览入口。
- 用户任务管理：`GET /tasks`、`GET /tasks/:task_type/:id`、`POST /tasks/:task_type/:id/execute`、`POST /tasks/:task_type/:id/rebind`（仅失效受管快显任务的来源驱动重绑，不自动执行）、`GET /executions/:execution_id`，继续使用 `manager.derived_artifact.*`。
- 用户执行状态读取在 SQL 中限定可信 Tenant、Manager 派生产物或向量化任务；Owner 读取权限包含已删除定义的执行历史，无定义执行仅发起 User 可读，缺少主体拒绝。TaskProvider 独立保留机器认证，不套用本人条件，但同样拒绝跨模块、data_profiling、cleanup 与未声明任务类型。专业血缘和转换事实遵循[任务体系规范](../docs/spec/addp任务体系规范.md)中 Manager 显式安全 DTO 的字段范围，不返回任意 metadata。权限回归由 `make test-manager-backend` 的 T1 和 `make test-manager-postgres` 的真实 PostgreSQL API 测试验证，模块门禁和 CI 沿用自动发现。
- TaskProvider：`GET /task-provider/tasks`、`GET /task-provider/tasks/:task_type/:id`、`POST /task-provider/tasks/:task_type/:id/execute`、`GET /task-provider/executions/:execution_id`；整组路由只允许 `addp-orchestrator` Service Client，并使用 `manager.task_provider.read|execute`，不得向 Manager 前端暴露。
- 数据进出与向量化：`POST /uploads`、`POST /imports`、`POST /exports`、`GET /exports/:id/file`、`POST /embedding_executions`、`GET /embeddings`、`GET /items/:item_id/embedding`。
- 平台配置管理：`GET /settings/embedding`、`PUT /settings/embedding`；只接受 Platform Context 和 `manager.configuration.read/update`。

## 开发规则

Manager 有界执行的租户过程事件复用 `common.execution_events`：真实 claim 同事务写入 `started`，原有成功／失败／超时／取消路径写唯一对应终态，过期租约由 Owner 收敛并写 `failed`。执行、任务摘要及当前专业结果保持原事务；事件存储失败必须全部回滚，过期或错配 lease 的运行体不得追加。没有完整 lease 的历史记录只收敛概览，不补造过程；授权附加、心跳、专业回执不产生事件。已有瓦片缓存／业务瓦片集／镶嵌／COPC 的进度更新与仅含百分比的 `progress` 事件同事务提交，镶嵌等待扫描的既有 99% 阶段同样记录；不把自由文本 current_step 当作 step_id，也不复制 Runtime 回调正文。通用 current_step 仅由 Manager 生成受控说明和计数，不能直接回显 Runtime 的 message 或任意 phase；原始回调只保留在 Owner 的专业事实中。本切片不新增 Runtime 节点历史，不复制配置、参数、Runtime 正文、业务样本或授权信息。Monitor 沿既有 Owner 重裁与公共事件读取展示，30 天维护不影响专业结果及操作审计。验证使用 `make test-manager-backend` 和 `make test-manager-postgres`，新增 PostgreSQL 用例必须以 `TestIntegrationPostgresManager` 命名，由现有 CI 自动发现；该门禁按包串行执行共用 Manager schema 的 repository／service／preview，避免测试初始化 DDL 互锁；未运行真实 T4 时不计为通过。

2026-10-19 租户事件有限范围 T4 已核验：[Hosted 39143850265 / Manager Job 117723580769](https://github.com/pampa0629/addp/actions/runs/39143850265/job/117723580769)，精确源码 `830a9dbfda817c0289856582686edacc261d5849`，归档 `online-t4-manager-internal-artifact-lineage-39143850265`（ID `12148787773`）。普通 Tenant 2 / Principal 33 沿原有十项权限，经 Gateway 读取 Manager 专业执行及 Monitor 公共事件；`addp.manager-execution-events/v1` 收据与 Owner v13 报告一致，11 个 UUID 均匹配实际创建的执行。覆盖 `model_3d_glb_generation` 八份模型源，以及 `point_cloud_copc_generation`、`pptx_pdf_generation`、`raster_cog_generation` 各一次成功执行，共 26 条事件、attempt 均为 1；每次恰有一个 started 和一个 completed，COPC 本次另有四条仅含百分比的 progress。实际响应按闭合字段、身份、时间及游标校验，不保存原始事件正文；全部读取均为单页。事件时间落在返回窗口且距读取时不超过 30 天，不等于已经验收自动保留清理。归档退出摘要为 `cleanup=passed`、`infra_cleanup=zero_residuals`。本轮不证明其余八类任务、失败／超时／取消／过期恢复、多页、每日预算截断、完整进度回调场景或生产 T5，也不证明 180 天概览清理；不因 Owner v13 整体通过扩大上述租户事件范围。

文件上传后的扫描提交使用当前请求 User 的同步 BFF 身份，不使用 Meta Service Client。上传入口在写入前同时要求 `manager.data_item.create`、`meta.scan_task.execute` 与 `meta.scan_task.read`；Meta 独立认证并记录发起人，Worker 不接收用户凭据。响应仅提供 `scan_execution_id`，前端按唯一字段回查 Meta 安全执行详情；不返回内部执行配置。完整身份与失败边界见平台《授权上下文规范》5.1。验证由既有 `make test-manager-backend`、`make test-manager-frontend`、`make test-go` 和 Swagger 门禁覆盖。

- 数据源连接信息必须通过 System 获取，不要在 Manager 中保存或硬编码连接配置。
- 元数据树与数据项优先通过 Meta 查询，Manager 只做预览、检索和快显侧的缓存与呈现。
- 预览能力走 `PreviewRegistry` 和 provider，不要为单一数据源在 Handler 中写特殊逻辑。
- Redis 键值数据集通过 `builtin:key-value` 与 `KeyValueReadableProvider` 读取，要求已扫描的 keyspace Meta item 身份，数据类型为 `key_value`。统一 preview API 先返回游标键列表，再用 `key_name` 内容令牌读取选中键；所有请求沿用数据集 locator 与保护门禁。共享 `KeyValuePreview` 是键浏览和原生内容预览的唯一前端实现，字段和值显式标明 UTF-8/Base64，stream 字段对保留顺序和重复名称。首期不提供表剖析、稳定分页、导出或字段保护适配；存在未支持的字段保护规则时拒绝键列表及内容读取。
- 键名前缀筛选由同一 preview API 的 `key_prefix` 传入，按 UTF-8 字面前缀匹配，保留空格并转义 Redis 通配字符。共享组件在应用或清空前缀时重置游标和选中内容，下一批与刷新保留已应用前缀；不在浏览器按当前批次过滤，也不创建前缀目录。
- 键列表在原生类型后展示“值”列，由同一次预览返回有界原生值样本；string 最多 256 字节、集合最多 3 个条目，较大内容仍点击键名查看详情。列表与详情复用 Common 原生读取实现和保护门禁，前端不逐键补请求。
- 资源树、预览、刷新和跨页面跳转统一使用 ResourceLocator；不得恢复 `engine_id/schema/table` 公共预览入口。
- 预览响应材料必须遵守 `content.kind`、`preview_material`、`frontend_renderer` 三层语义；不得把 `raw_content`、`range_content`、`binary_content` 写入 `preview_material`。
- 存储型 item 原始下载走 `downloads/file` 的 ResourceLocator + DownloadPlan；前端不得从 preview metadata refs 拼接 multi 文件下载。
- 向量化用户界面使用“向量化”，英文 API、表名和 TaskProvider `task_type` 统一使用 `embedding`；不得新增 `vectorization` 双轨路径。
- 向量化对象只能是 data item；资源树 node 只是批量选择范围，不产生 node 向量化结果。
- 资源树 item / node 向量化是 ad-hoc execution，不写入 `manager.embedding_tasks`；只有从“数据任务 → 向量化任务”入口创建的配置才是任务定义。
- 资源树行内只保留刷新快捷操作；向量化从当前选中资源的操作区或统一“数据任务”入口发起，不在每个树行展示向量化状态或快捷图标。
- 资源树 node 行内刷新固定执行 Meta `basic + force`，用于发现当前范围的新 node / item；item 刷新固定执行 known-item `deep + force`，只重建该 item 自身的深层元数据。
- node 详情右上角的“深度扫描”通过 Meta 手动扫描接口提交当前节点完整 ResourceLocator，使用 `deep + force` 覆盖该节点及全部下级资源；与行内基础刷新共用执行等待和资源树回读流程，不逐 item 派发刷新、不默认扩大为整个引擎。交互细则见 `docs/数据预览与资源树实现规范.md`，平台契约见 `docs/spec/addp元数据扫描机制规范.md`。
- 表格数据剖析只按 `data_type=table` 和当前内容选择上下文开放；不得按 `item_type`、engine type 或文件扩展名硬编码。首期剖析是 `data_profiling` ad-hoc execution，结果写 Manager 私有表，不写 Meta attributes，不创建 `manager.data_profile_tasks`，也不声明 TaskProvider capability。
- 数据剖析不得使用当前预览页、分页记录或前端数组计算；采样和指标计算必须走统一 Provider 与服务端预算。刷新失败必须保留上一份成功结果。
- 剖析入队及活跃／最近执行查询只从可信当前 User AuthContext 获取发起人、租户成员关系与授权版本；冻结共享 execution 来源字段，并按这三项隔离执行复用。不保存 User Token，不把来源事实当作源授权；独立 execution 源授权缺失或失效时拒绝采样；当前开放范围见[剖析规范](docs/数据剖析规范.md#当前授权切换边界)。
- PostgreSQL 剖析准备通过同一表格查询生成器冻结预算内所有分页及完整源依赖；首版只允许已扫描单表，不调用预览或读取业务行；最多 10000 行、单页 2000 行。v6 入队先保存完整计划，当前 User 请求签发后在行锁事务内绑定；未绑定不得领取，失败或中断须收敛。Worker 核对精确租约、原始配置和重新准备的完整分页计划后，用当前 Tenant Service 消费 System 固定源授权入口；拒绝与不可用不进入采样，不缓存 Allow。每页读取和结果提交前都须重新核验现行源授权与本地保护，不以准备计划或首次 Allow 代替复核。完整配置摘要复用 `common/execution.NewManagerProfileReadScope`，只能输入持久执行的原始 JSON，不能经 `JSONMap` 浮点数往返。
- 剖析入队与后台消费共享当前强类型配置契约；配置摘要覆盖超时。消费只使用入队时冻结的完整预算，并复核来源主体、复用键、引擎及稳定源身份；旧版本、缺失预算、未知字段或摘要不一致均在采样前拒绝，不补默认值。入队时深拷贝条件和资源引用，后续修改请求对象不得改变已有执行。
- 已保存剖析结果必须带实际采样完整 `dependency_snapshot.read_set`；结果内容按当前普通 User Bearer 同步复核 System 固定 `manager-profile-result` 源规则及本地 Security。缺失或不规范的历史来源不从当前依赖补造、不回填、不删除，明确拒绝结果内容；不能用委托预览或机器身份读取。结果仍按资源／配置共享，不改为发起人私有。
- 条件剖析只接受结构化 `data_scope`，条件必须由声明支持的 Provider 在采样前执行并安全绑定参数；全范围和条件范围按 `profile_config_hash` 分别保存。Manager 不接受任意 SQL，也不得退回到采样后过滤。已纳入 Security 保护的 DataItem 在条件值保护契约完成前只允许全范围剖析，条件剖析必须拒绝。
- 空间相关逻辑不得默认几何字段名为 `geom`，应从 Meta、预览检测或请求参数获取。
- 不得把 Quick View 称为任务；瓦片缓存生成任务统一使用 `manager.task_definitions` 中的 `task_type=vector_tile_cache_generation`。
- “快显任务”“空间数据任务”和“向量化任务”是统一“数据任务”工作区中的产品分类，不是单一 `task_type`。三类任务在“数据任务”父菜单下使用独立 canonical path，但复用同一个工作区实现。全部 Manager 生成类任务定义统一写入 `manager.task_definitions`，由 `task_type` 选择强类型配置、执行器和结果策略；向量化任务继续使用 `manager.embedding_tasks` 及其独立调度和结果语义。不得恢复生成类型独立实现或向量化独立工作区。
- Manager 数据任务工作区只展示任务定义、最近执行摘要和领域结果入口；页头统一使用 `MonitorExecutionsButton(module=manager)`，按当前 `task_type` 筛选 Monitor。不得新增 Manager 模块级执行列表、菜单或通用统计页。
- 统一任务表、语义唯一约束、资源绑定、资源回收生命周期及受控分页计划的完整依赖证明，真实 PostgreSQL 验收统一走 `MANAGER_POSTGRES_TEST_DSN=... make test-manager-postgres`；普通 Go 测试不得把该场景降级为内存替身。分页夹具仅在安全测试库创建随机命名 Schema，并在退出时清理和核验零残留。
- 业务矢量瓦片集生成任务统一使用 `manager.task_definitions` 中的 `task_type=vector_tile_set_generation`；结果只写用户选择的 Business 存储并触发 Meta scan，不进入 `manager.vector_tile_cache`。
- “保存为业务瓦片集”必须创建或执行 `vector_tile_set_generation`。ready 缓存仅在源版本和生成 profile 完全一致时作为执行复用候选；复制必须使用临时对象、PMTiles 校验和原子提交，成功后再触发 Meta scan。
- 矢量物化视图任务统一使用 `manager.task_definitions` 中的 `task_type=vector_materialized_view_generation`；结果只登记 Manager 创建并拥有生命周期的 3857 目标。
- 栅格快显 COG生成结果统一使用 `manager.raster_cog`，任务定义使用 `task_type=raster_cog_generation`；不得写入 `vector_tile_cache` 或 `vector_materialized_view_generation`，不得让前端感知 NFS path、业务 MinIO bucket/object 或底层 `storage_ref`。
- 栅格 mosaic 生成任务使用 `task_type=raster_mosaic_generation`；创建入口是资源树 node，目标是用户选择的业务存储，结果是 `data_type=media`、`format=raster_mosaic`、`layout=whole` 的业务 item。mosaic 的 leaf COG、overview COG、index、manifest 和可选 tiles 都不写入 Manager infra MinIO，不进入 `manager.raster_cog`。
- mosaic 普通执行从当前已认证 User 固化 Principal、Membership、授权版本及 triggered_by，不使用任务创建者；机器入口继续沿 Orchestrator 父执行继承。实际产物生成后，先通过有效 lease 保存 manifest 和当前 attempt 的 `metadata.produced_target`，再以 `manager` 来源与直接父 UUID 提交精确 dataset 目录的 Meta deep/force 扫描。Meta 独立校验专用 Manager 服务、有效父执行和持久产物；发起人仍须具备 Meta 读取权限才能读取该一次性扫描。原扫描失败语义不变；不保存用户 Token、不扩大目标范围或回填旧身份。契约见平台任务体系规范，验证沿现有 Manager 后端、Manager/Meta PostgreSQL 及栅格容量 T4 分层完成，T1/T2 不代替真实容量验收。
- 点云 COPC 快显任务使用 `task_type=point_cloud_copc_generation`；源必须是 `data_type=point_cloud + layout=single + format=las|laz|e57|pcd|xyz`，结果写入 `manager.point_cloud_copc` 和 Manager infra MinIO，不自动升格为业务 data item。源 `format=copc` 只走基础预览，不创建二次快显任务；XYZ 第一阶段只支持简单确定性文本 XYZ。
- CAD 基础预览只允许 `data_type=cad + layout=single + format=dwg|dxf`，统一通过受控 `storage-stream` 把源文件交给 `frontend_renderer=cad`。浏览器收到源文件即具备保存其内容的能力，因此该入口必须使用 `manager.data_item.read`，且产品权限语义不得声称“可预览但不可取得源文件”。
- 分块三维模型瓦片任务使用 `task_type=model3d_tiles_generation`，结果统一进入 `manager.model3d_tiles`；`target_format=3d_tiles` 调用 `osgb_scene_to_3dtiles`，`target_format=s3m` 调用 `osgb_scene_to_s3m`。不得恢复写业务存储并触发 Meta scan 的旧 Manager 路径。
- Manager 受管快显任务的语义身份统一为 `tenant_id + item_fingerprint + artifact_variant`。重复创建必须复用原任务 ID，重复执行必须新建 execution 并刷新同一当前结果；`item_id`、`locator`、`source_engine_id` 只作执行与回查事实。派生变体、并发唯一约束和业务派生任务的例外见 `manager/docs/快显实现规范.md`。
- Manager 受管快显是来源驱动的只读任务定义：既可从 Data Explorer 当前 item 的 Quick View action 发起，也可从统一“数据任务”页选择一个源 item 后发起；两处必须调用同一 owner 领域动作，由后端按源事实派生任务定义，不提供允许前端提交私有任务配置的创建或更新 API。管理页的 `task_id` 只打开只读任务定义。
- Manager 受管当前结果任务统一不启动自身定时调度，但允许 Orchestrator 定时 Pipeline 调用。周期性刷新由用户在 Step 参数中显式配置 `existing_result_action=overwrite`；Manager 不得按 scheduled 来源自动补充已有结果动作。Embedding 的逐 item 调度语义独立保留。
- COG 生成只能由 Manager 任务执行器构造标准源/目标访问计划后，通过 `WorkflowRuntimeProvider.InvokeOperator("raster_to_cog")` direct 调用 GeoPython Workflow；不得退回构造单节点 workflow 或直接拼接 GeoPython Workflow 私有 HTTP。
- 瓦片缓存生成任务不得隐式创建 3857 物化视图、空间索引或执行准备动作；需要性能准备时必须显式执行矢量物化视图任务。
- 自动识别的外部 3857 目标只能只读消费，不写入 `manager.vector_materialized_view`，也不获得 Manager 删除、刷新或 stale 生命周期。
- 批量矢量瓦片生成按源能力分流且不得交叉：PostgreSQL/PostGIS 空间表必须由 Manager 复用 `common/spatial` 的 `ST_AsMVT` SQL 生成；MySQL、Oracle 等标准 EWKB 可读、但无原生 MVT 输出的数据库空间表必须先流式物化受控临时 FlatGeobuf，再由 GeoPython Workflow `vector_to_pmtiles` 生成；文件和对象来源必须由受控 GDAL 访问计划调用同一 operator。三类来源统一封装为 PMTiles v3，Manager 内部不得恢复松散 MVT 目录、私有 manifest 或同源备用路线。
- 快显瓦片缓存的默认最大层级必须同时受记录数和累计候选瓦片预算约束；默认预算为 10000 个 WebMercatorQuad 矩形候选瓦片。前端必须显示当前层级的预计候选数，用户可显式提高层级，但不得把 `max_zoom=18` 作为无条件默认值。
- 业务矢量瓦片集页面复用同一候选瓦片估算能力，但不把快显预算作为业务生成硬限制；后端推荐层级与用户当前选择必须分开显示，用户超过推荐层级时提示预计生成规模和成本风险。
- 修改 API 后同步 Swagger：`bash scripts/swagger/gen-swagger.sh manager` 和 `bash scripts/swagger/check-route-coverage.sh manager`。

## 前端公开路由

- Manager 前端遵守 `docs/spec/addp前端路由与可恢复状态规范.md`，模块内公开导航统一通过 `src/utils/moduleNavigation.js`，不得直接形成 iframe 私有历史。
- 独立访问 `/` 或登录后无指定目标时，按数据探查、数据检索、快显任务、空间数据任务、向量化任务、平台向量化配置的顺序，进入当前 AuthContext 可访问的第一个页面；没有可进入的首页时显示无权限页。具体页面准入与 Console 共用 `CONSOLE_ROUTE_ACCESS`，显式业务地址仍单独校验。
- Data Explorer 使用 ResourceLocator 表达当前资源身份，默认 `preview` Tab 从 URL 省略，非默认稳定 Tab 使用 `tab` query。
- “数据任务”是 Manager 唯一任务工作区，在父菜单下固定提供 `/manager/tasks/quick-view`、`/manager/tasks/spatial`、`/manager/tasks/embedding` 三个纵向子入口并复用同一页面组件。分类身份由 path 表达，不得再用 `category` query；任务类型筛选、向量化子视图和当前 `task_id` 保留在 canonical query 中，规范化使用 `replace`，跨页面进入资源或任务使用 `push`。
- Manager 受管快显的 TaskProvider `create_url` 指向 `/manager/tasks/quick-view` 并携带 `task_type` 和 `create=1`；页面只选择源 item，不选择目标。`edit_url` 指向同一路径并携带 `task_type` 和 `task_id`，任务定义保持只读。Embedding 的 `create_url` / `edit_url` 指向 `/manager/tasks/embedding`，不得恢复 `/manager/vectorization-tasks` 或 `/manager/derived-tasks` 路径。

## 开发与验证

Raster Mosaic Runtime 由 Manager 管理，普通 Manager 启停入口包含该进程。开发环境使用不继承系统包的 Python 3.12 venv 和原生 GDAL；停止后自动重建不合格的 venv，运行期间不得修改环境。GDAL 来源、资源目录和失效绑定重建复用 `scripts/dev/gdal-env.sh`，所需驱动与 NumPy 能力由 Manager 自行检查，不引入 GeoPython 的 MDBTools 配置。

`make test-raster-mosaic-runtime` 覆盖瓦片单测和真实 GDAL/COG 渲染，模块门禁自动发现该入口；测试可通过 `RASTER_MOSAIC_RUNTIME_PYTHON` 指定独立的 Python 环境。

启动或重启前遵循根 AGENTS.md：先说明原因与涉及模块，由用户在自己的终端执行。实际端口以标准启动输出和 `.dev-state/ports.env` 为准；测试或构建验证不要求重启。

```bash
bash scripts/dev/start.sh -manager
bash scripts/dev/restart.sh -manager
```

验证沿[测试与验收规范](../docs/spec/addp测试与验收规范.md)的标准入口执行：

```bash
make test-module MODULE=manager
make test-manager-online-runner
```

共享依赖变更按真实影响扩散；确定性 runner 不替代真实 T4。Manager 内部产物、混合检索和 Transfer 私有导入各按已登记 suite 认定，不共用通过结论。仅调整 Markdown 时核对内容、链接和锚点，并执行 `git diff --check`。

常用日志：

- `logs/manager-backend.log`
- `logs/manager-backend-stderr.log`

## 相关文档

- [数据库架构](docs/数据库架构.md)
- [快显概念说明](docs/快显概念说明.md)
- [快显实现规范](docs/快显实现规范.md)
- [向量化概念说明](docs/向量化概念说明.md)
- [向量化能力说明](docs/向量化能力说明.md)
- [数据预览与资源树实现规范](docs/数据预览与资源树实现规范.md)
- [数据预览语义协议](docs/数据预览语义协议.md)
- [数据剖析规范](docs/数据剖析规范.md)
- [三维模型、点云与高斯泼溅预览说明](docs/三维模型、点云与高斯泼溅预览说明.md)
- [存储流与原始下载语义](docs/存储流与原始下载语义.md)
- [preview_state表](docs/tables/preview_state表.md)
- [task_definitions表](docs/tables/task_definitions表.md)
- [task_resource_bindings表](docs/tables/task_resource_bindings表.md)
- [vector_materialized_view表](docs/tables/vector_materialized_view表.md)
- [model_3d_glb表](docs/tables/model_3d_glb表.md)
- [model3d_tiles表](docs/tables/model3d_tiles表.md)
- [gaussian_splat_ksplat表](docs/tables/gaussian_splat_ksplat表.md)
- [point_cloud_copc表](docs/tables/point_cloud_copc表.md)
- [raster_cog表](docs/tables/raster_cog表.md)
- [vector_tile_cache表](docs/tables/vector_tile_cache表.md)
- [embeddings表](docs/tables/embeddings表.md)
- [embedding_tasks表](docs/tables/embedding_tasks表.md)
- [embedding_configuration表](docs/tables/embedding_configuration表.md)
- [search_history表](docs/tables/search_history表.md)
- [共享前端模块说明](../common-frontend/CLAUDE.md)
- [Meta 模块说明](../meta/CLAUDE.md)
- [平台任务与执行规范](../docs/spec/addp任务体系规范.md)
- [数据安全与保护投影](../docs/spec/addp数据安全与隐私保护实现规范.md)
- [Manager Online 场景](../docs/spec/testing/scenarios/Manager与空间成果.md)
- [Transfer 私有导入场景](../docs/spec/testing/scenarios/同步与引擎消费.md#manager-私有导入)
- [验收证据索引](../docs/spec/testing/验收证据索引.md#manager-预览与私有导入历史证据)
