# Manager 模块说明

KML／GML／TopoJSON 的 S3 兼容消费验收复用既有 Manager Hosted suite、GeoPython 产品 Runtime 和隔离 Business MinIO，增加三份固定哈希双图层源；普通 User 权限仍为已批准的十项。Owner/v11、browser/v13 严格要求图层及源几何、实际地图要素和像素、TopoJSON 未知 CRS 默认暂停、临时选择清除／切换／重开、普通刷新及原文件下载哈希证明，旧报告拒绝。标准 `make test-manager-online-runner` 自动覆盖夹具、拒绝边界、浏览器语法和 CI 登记。隔离环境先启动既有 Security，再由标准身份夹具等待 Manager 持久保护投影初始化；预检核对 Security 健康和源码身份，启动或初始化失败均拒绝进入场景并清理。不新增自定义服务、用户权限或 workflow，不变更血缘产品实现。本轮端点为 Hosted 的 `http://127.0.0.1:59002`，MinIO 结果只认证 S3 兼容协议，不能登记为外部云 S3 认证；正式 T4 须另行核对当次运行、源码及清理证据后记录。

导入弹窗按 Transfer／Meta 的公开执行响应展示进度：终态 `failed`、`timeout`、`cancelled` 均结束轮询并复用共享 `ExecutionFailure`，只消费 `error_details` 与实际执行 UUID，不读取原始运行错误或已删除的 `error_msg`。Transfer 成功后等待其准确的 Meta 子执行，元数据同步失败不能显示整个导入成功；关闭、重试和卸载必须隔离旧请求。数据保护拒绝的共享提示使用适用于数据读取与结果保护的中性文案，不将导入失败解释为 SQL 拼接错误。受影响层级为共享前端 T1、Manager T1／隔离浏览器 T3／构建，以及既有 Console Hosted T4；标准入口 `make test-common-frontend`、`make test-manager-frontend` 和 `make test-transfer-online-runner` 已自动覆盖，无新增服务或 CI Job。

私有导入 Hosted 场景使用 Console 的真实 Manager 导入弹窗上传相同两点 ZIP，核对目标命名空间、CPG 编码、Transfer 执行及 Meta 子扫描等待和资源树刷新；非上传者拒绝后，经普通 User 的 Console Transfer 执行详情核对零读写和共享安全诊断。上传一经接受即记录当次 task／execution 身份，后续页面失败仍交给既有清理；浏览器不复制后台导入 HTTP 路线。独立回执升级为 `addp.manager-private-import/v2`，包含两阶段 `addp.manager-private-import-browser/v1` 证据，完整 suite 为 v12；必须核对当前 User／Tenant／run 及准确执行 ID，缺少任一页面证明不得登记通过。

Console 导入完成后由 NodePanel 先等待原 schema 的祖先事实与直接子资源刷新完成，再按既有桥接导航进入同一执行 UUID 的 Monitor；ImportDialog 只提交准确的完成执行与目标 locator，不能并行发起会卸载 iframe 的导航。验收延迟刷新响应，确认刷新未完成时仍保留 Manager iframe，完成后才进入准确的 Monitor UUID，再返回原 schema 验证目标节点。嵌入回归沿既有 Manager 前端 T3 入口自动发现，不新增导航路线。

导出发起用户来源核验复用 `common/exportartifact`：派发前持久化 Transfer UUID 与完整请求摘要，`POST /runtime/export-sessions/{id}/execution-source` 仅允许 `addp-transfer` 持有 `manager.export_provenance.read` 核验有效待执行会话。只返回发起用户事实，不开放文件下载或数据权限。最小验证入口为 `make test-module MODULE=manager`；Common 变更按 `make test-changed` 扩散验证。

导入暂存来源按上传者私有：`manager.import_sources` 只保存成功上传的 Tenant、Uploader、UUID、准确 source locator 和一次绑定的 Transfer task ID。`POST /runtime/import-sources/verify` 仅接受持有 `manager.import_provenance.read` 的 `addp-transfer`，核对当前执行主体及任务，不返回文件或凭据。未绑定或不匹配拒绝；不从路径推断所有权。验证沿既有 Manager Go／PostgreSQL、Common Client、System IAM 和 Transfer Hosted 门禁。

## 模块定位

Manager 模块负责数据探查、数据预览、表格数据剖析、混合检索、空间快显和瓦片缓存能力。它不管理存储引擎配置，存储引擎由 System 管理；Manager 通过 System、Meta 和实际数据源完成只读探查与预览。Manager 的引擎资源树是技术浏览视图，不是 Enterprise Catalog，也不拥有企业 `CatalogEntry`、业务语义关联或责任事实。Catalog 落地后，Manager 只按需读取企业资源摘要并跳转企业资源目录；Catalog 不可达只影响该摘要和跳转，不得回退到旧 Asset 发现或在 Manager 内复制企业资源目录。

数据剖析已经按确认边界实现：剖析执行和结果归 Manager，Meta 只提供 data item 身份、结构和源版本事实；首期在用户进入“剖析”标签时按需创建 `task_type=data_profiling` 的 ad-hoc execution，不创建持久任务定义、不声明 TaskProvider capability。完整规则见 `manager/docs/数据剖析规范.md`。

PostgreSQL item 预览只执行 Provider 的不可变 PreparedQuery：完整 ReadSet 先用当前 User／固定 `data.preview` 委托 Bearer 调用 System 源检查，再编译本地 Security 保护；检查和执行使用同一计划，不回退 ReadBatch。保护在响应输出前执行并同步删除被抑制列的展示元数据。后台剖析首版仅开放已扫描 PostgreSQL 单表的有界采样；独立 execution 签发、绑定、当前 Tenant Service 消费、逐页及提交前复核均使用同一计划，不借用户预览凭据。视图、物化视图和多来源计划明确不支持。用户决定暂缓源端并发 DDL 改源的特殊情形；本轮不声称原生内容事务内依赖冻结已经完成，也不认证预览已解决该缺口。跟进见专题 §26.99–26.100。

Manager 通过本地保护投影统一约束预览、剖析和全文索引写入，用户请求不调用 Security。`profile=suppress` 在持久化前删除敏感字段及全部祖先容器的字段剖析对象和对应全局观察，防止父级 Top N 携带敏感叶子值；`search_index=mask` 在写入 Meilisearch 前覆盖正文及所有正文派生字符串。投影变化与历史剖析结果、条件值清理及 cursor 保存共用本地安装屏障；启动时对已安装投影重放清理。外部全文索引只走持久投递路线：保护事务内登记清理并隔离出口，事务外提交或核查 Meilisearch 任务；任务入队不算完成。新投递在发送前持久绑定 UUID，并随请求发送 `customMetadata`，关闭 SDK 自动重试；已知 UID 的任务在超时或重启后续查，无 UID 的新提交只能从完整保留的任务历史中唯一认回同一标记、端点、索引和操作种类的原任务，再保存回执并核查终态。没有标记的旧提交不回填证据；零匹配、历史不完整或冲突均保留未决，不重发、不按时间清除。服务器不支持标记、未配置或暂时失联均不使 Manager 初始化失败；重新开放搜索必须先核清历史任务、完成清理。出口隔离且旧读取结束时允许保护回执，但不声称外部清理成功。多 Backend／HA 须独立认证；持久投递与保护回执契约见 `docs/spec/addp数据安全与隐私保护实现规范.md` 第九节。旧 Meilisearch 数据卷须另行确认迁移，不能只改镜像后重启，见 `scripts/infra/README.md`。

Tenant HTTP 请求与有界执行分发先登记在途生命周期，再刷新 Owner 本地持久保护 cursor；登记覆盖响应序列化／流复制或执行结果落库。投影同步的后置屏障等待本进程在途工作及持久有效 lease 下的运行执行结束，再回执 Security；pending 和过期遗留记录不阻塞。此机制不授予数据访问权，也不替代 System 的源授权核验。

剖析采样前通过 Common 捕获持久保护版本，结果提交通过同一 checkpoint 锁核验版本、重新执行保护校验并落库；不接受“先检查、后独立事务写入”。版本变化以 `protection_version_changed` 失败，前端提示显式重新执行。采样期间不持锁，规则更新可以及时安装；Repository 不保留没有保护提交事务的生产写入路径。

表格预览的 `preview=suppress` 必须同时删除行值、输出列和对应列元数据；空结果页也不得重新暴露被抑制列。响应边界复用 Common 的查询结果保护执行器，Manager 只适配预览协议；临时原值授权命中时，列契约与有效允许决策保持一致，不能按默认规则额外删除已授权列。

预览失败展示保留服务端稳定错误码。HTTP 403 且 `error_code=security_protection_required` 表示当前数据保护要求阻止预览，界面应引导核对保护规则、同步状态和有效期；不能将其解释为缺少功能权限或源数据 Grant。其他 403 继续展示读取权限提示，503 继续展示依赖不可用；成功重读或切换资源时必须清除旧失败状态。该展示回归沿既有 `make test-manager-frontend` 和 Manager frontend CI 自动发现，不改后端错误契约。

Manager 的一次性数据库 item 导出通过 Common 强类型 Client 直接创建 Transfer bounded `sync` ad-hoc execution，不创建临时 `transfer.transfer_tasks`。`manager.export_sessions` 只保存 Transfer 的统一 `execution_id` 和短生命周期下载会话事实，不保存 Transfer task ID；状态回查、artifact manifest、发起用户隔离与 infra 暂存清理由 `common/exportartifact` 提供唯一实现，Manager 只负责资源校验和格式能力适配。Manager 导入保留持久 `sync` 任务语义，不与一次性导出混用。Shapefile 导入使用 `runtime.boundary=bounded`、`load.mode=snapshot` 和 `target.policy.apply_mode=replace`；不得提交已删除的根 `mode` 或 `write_mode`。回归必须覆盖暂存上传、按当前 User 身份创建任务及触发执行的实际 HTTP 提交过程。既有 Hosted `transfer-relational-sql-etl` suite 还需验证 Shapefile 实际写入、Meta 自动扫描和当前 User／父 execution 来源，并由 Business fixture 独立核对点坐标与 SRID；入口为 `make test-transfer-online-runner` 和对应 Online T4 workflow。导入场景在该 suite 中前置执行，先经同一 Business fixture 的 `manager-import` 核对实际 PostGIS 行值、坐标及 SRID，再写入 `manager-private-import.json` 独立回执；回执包含真实 source commit、User／Tenant、Transfer／Meta UUID、非上传者零读写拒绝及普通 User 直接 Inspect 拒绝。后续场景失败不得抹去已完成的本领域证据；完整 suite 仍保留全部场景与最终物理校验。

2026-10-17，提交 `68d7cbd03` 的 [Hosted T4 38983723706](https://github.com/pampa0629/addp/actions/runs/38983723706) 已生成 `addp.manager-private-import/v1` 独立通过回执：Tenant 2 的普通 User 33 上传并成功读写两条点数据，自动 Meta 子扫描核对当前 User 和真实父 UUID，Business 独立核对完整行值、坐标及 SRID 4326；非上传者启动同任务时以 `data_protection_required` 在零读写时失败，两名 User 直接调用 Inspect 均被拒绝。八个服务构建提交与回执一致，退出清理 passed、Infra zero_residuals。完整 suite 随后因其他写入失败场景创建任务 HTTP 400 而 failed；此证据仅证明本领域私有导入及 Inspect 边界通过，不计为完整套件通过。

提交 `a238b036b` 的 [Hosted T4 39009814565](https://github.com/pampa0629/addp/actions/runs/39009814565) 已真实通过完整 v12 suite，九个服务（含 Security）的构建提交与回执一致。`addp.manager-private-import/v2` 核对 Tenant 2、普通 User 33 的实际 Console 上传、两条点数据、准确 Transfer／Meta UUID、当前 User／父执行归属、独立坐标与 SRID，以及非上传者零读写拒绝的 Console 安全诊断；两阶段 browser/v1 回执与领域回执一致。16 个临时资源全部删除，领域残留为零，退出清理 passed、Infra zero_residuals。此轮成功不替代延迟刷新响应下的导航顺序回归，该竞态由既有嵌入式 T3 单独验证。

Manager 拥有的成功 execution 必须在 `common.task_executions.metadata.lineage_facts` 写入 `addp.lineage-facts/v1` 事实：输入使用执行时已冻结的 ResourceLocator、item ID 和 fingerprint，业务输出使用目标 ResourceLocator，Manager 私有快显产物使用 `addp-infra://` Locator；触发的 Meta scan execution ID 写入 `meta_scan_refs`。任务服务只提供自身输入输出事实，统一结构由 Manager service 公共构造器生成，不得各自拼装 JSON。清理 execution 不表达数据派生血缘；Manager 调用 Transfer 的导入导出由 Transfer execution 作为唯一血缘 owner，Manager 不重复写入。

`raster_mosaic_generation` 成功必须覆盖成果生成、原精确自动扫描完成及 whole DataItem 正式身份取得。父 execution 在原 lease／超时内等待，通过 Meta 专用 `GET /runtime/mosaic-scans/{execution_id}/result?parent_execution_id=...` 取得闭合的 `output.locator/item_id/fingerprint`；输入 Runtime 返回的 `/vsis3/...` manifest 仅保存为私有物理事实，不能作为公开血缘输出。Manager 保存唯一 whole 输出的 locator、item ID 和 item fingerprint 后才成功，耗时包含扫描等待。失败／超时保留物理成果和子扫描关联，不自动重放、不增加终态后台补写。其他任务不改变成功条件。最小回归为 `make test-manager-backend`、`make test-manager-postgres`；共享 Client 与 Meta 收尾和权限由 `make test-go`、`make test-module MODULE=meta GO_ONLY=1`、`make test-meta-postgres` 覆盖，真实复验由既有栅格容量 suite Owner 执行。

用户 `/executions/:execution_id` 使用 `ManagerExecutionResponse` 显式投影，复用 Common 安全概览与失败分类；专业 metadata 仅保留向量化／瓦片计数、血缘资源和操作、Workflow 类型／算子／模式／耗时、GLB 大小／来源格式及 MAX 转换证据。嵌套结构也必须显式声明；不输出配置、授权引用、访问计划、Runtime command/stdout/stderr/publish、schema snapshot、字段映射或任意附加 metadata，不改写持久历史。专业结果内容仍使用已有结果 API；Monitor 与机器 TaskProvider 不扩展响应。后端 HTTP 及真实 PG 读取隔离矩阵同时核对敏感字段缺席；现有前端轮询继续消费状态及向量化计数，失败使用共享分类词条。标准入口 `make test-manager-backend`、`make test-manager-postgres`、`make test-manager-online-runner` 和模块自动发现／Platform CI 已覆盖新增回归；真实 T4 复用 `manager-internal-artifact-lineage` 核对安全字段、完整资源血缘及 MAX 转换证据，并验证本轮 Meta scan 执行 ID 直读 Manager 返回无执行数据的 404。

`metadata.workflow_runtime.execution_time_ms` 是 Runtime 返回的毫秒耗时，按工作流接口规范使用 JSON number 并保留小数；可选字段缺失或为 null 时不输出，数值 0 必须保留。它与 Common Observation 的 Owner execution 整数毫秒耗时分别投影，不改写持久历史。最小回归应覆盖真实 Runtime 小数输入和两个 execution 查询入口，并保留敏感字段排除断言。

跨模块验收使用唯一 T4 suite `manager-internal-artifact-lineage`：专用 Business MinIO 的 LAS、多页 PPTX、GeoTIFF、带 PNG 漫反射贴图的 DAE 与 3DS、自有 IFC4 米制立方体、zlib/DXT5 压缩的单体 OSGB、自有带内嵌彩色 PNG 的 SKP 双实例模型、声明外部贴图的 MAX 马模型、固定 FBX 盒子及带 MTL／PNG 的 OBJ 三角形经同一次 Meta scan，通过 Manager 生成任务及各自真实 Runtime 发布 infra 产物，并由 Monitor、Data Explorer 和正式清理 API 形成完整证据；不得用数据库夹具、伪 Runtime 或前端 mock 代替该链路。八种模型必须核对 `model_3d/single` 分类、自包含 GLB 与真实几何；DAE/3DS 另核对关联贴图和内嵌 PNG，IFC 核对 schema、实体摘要和米制立方体，OSGB 核对内嵌 JPEG 与 UV；SKP 核对无虚构解析元数据、英寸到米的尺寸换算、两个共享网格实例及内嵌彩色 PNG。MAX 核对用户毫米审计、五个网格与真实 RGB 贴图；FBX 核对源变换与颜色，OBJ 核对基础几何及 GLB 内嵌 PNG。FBX／OBJ 在生成入口和缓存预览两阶段均下载主文件核对 SHA256，清理后再次核对源主文件。十一类产物均核对既有血缘事实、领域清理及删除后的内容 404；GeoTIFF 使用 `raster_cog_generation`，表格／JSON 源只验证预览和下载，不创建派生产物。固定 FBX 的基础预览缺陷仍单列，不计入本轮通过范围。

混合检索跨模块验收使用独立 T4 suite `manager-hybrid-search`：专用 Business MinIO Fixture 幂等发布仓库内确定性 JPG，真实 Meta scan 建立全文索引，Manager 通过预置 `semantic_search_embedding` 场景绑定调用 Inference Model Profile 完成图片和查询文本向量化，并由真实 pgvector 与 Meilisearch 验证统一 RRF 排名、去重和分页。专用 Runner 的 Model Profile 与场景绑定是长期前置事实，suite 不创建或改写 Provider、Deployment、Profile、Credential 或绑定；每轮只创建并经 Manager 正式 API 删除该图片的 embedding 结果。

空间快显与瓦片缓存的目标边界：

快显能力和预览状态只解析 Engine／Meta／受管产物事实，不执行 PreviewProvider 或读取样本行。缺少空间事实时提示刷新，不能隐式采样补齐。渲染目标 SRID 只核验目录类型声明，不以一条几何样本推断；技术能力与内容读取授权保持独立。

PostgreSQL 渲染目标核验只查 geometry 字段的 typmod 声明；旧目标缺少声明时提示用户显式重新生成，不自动修改。生成端声明 EPSG:3857 并保留 2D／Z／M／ZM；未约束源列的全列维度核验仅发生在明确执行的生成任务中，不能移入 capability 链路。此边界不替代实际内容读取的授权核验。

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

文件上传后的扫描提交使用当前请求 User 的同步 BFF 身份，不使用 Meta Service Client。上传入口在写入前同时要求 `manager.data_item.create`、`meta.scan_task.execute` 与 `meta.scan_task.read`；Meta 独立认证并记录发起人，Worker 不接收用户凭据。响应仅提供 `scan_execution_id`，前端按唯一字段回查 Meta 安全执行详情；不返回内部执行配置。完整身份与失败边界见平台《授权上下文规范》5.1。验证由既有 `make test-manager-backend`、`make test-manager-frontend`、`make test-go` 和 Swagger 门禁覆盖。

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
- 用户执行状态读取在 SQL 中限定可信 Tenant、Manager 派生产物或向量化任务；Owner 读取权限包含已删除定义的执行历史，无定义执行仅发起 User 可读，缺少主体拒绝。TaskProvider 独立保留机器认证，不套用本人条件，但同样拒绝跨模块、data_profiling、cleanup 与未声明任务类型。专业血缘和转换事实的显式响应范围待单独确认，本次归属修复不改变该响应契约。权限回归由 `make test-manager-backend` 的 T1 和 `make test-manager-postgres` 的真实 PostgreSQL API 测试验证，模块门禁和 CI 沿用自动发现。
- TaskProvider：`GET /task-provider/tasks`、`GET /task-provider/tasks/:task_type/:id`、`POST /task-provider/tasks/:task_type/:id/execute`、`GET /task-provider/executions/:execution_id`；整组路由只允许 `addp-orchestrator` Service Client，并使用 `manager.task_provider.read|execute`，不得向 Manager 前端暴露。
- 数据进出与向量化：`POST /uploads`、`POST /imports`、`POST /exports`、`GET /exports/:id/file`、`POST /embedding_executions`、`GET /embeddings`、`GET /items/:item_id/embedding`。
- 平台配置管理：`GET /settings/embedding`、`PUT /settings/embedding`；只接受 Platform Context 和 `manager.configuration.read/update`。

## 开发规则

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
- 剖析入队及活跃／最近执行查询只从可信当前 User AuthContext 获取发起人、租户成员关系与授权版本；冻结共享 execution 来源字段，并按这三项隔离执行复用。不保存 User Token，不把来源事实当作源授权；独立 execution 源授权贯通前保持采样拒绝。
- PostgreSQL 剖析准备通过同一表格查询生成器冻结预算内所有分页及完整视图依赖，不调用预览或读取业务行；最多 10000 行、单页 2000 行。v6 入队先保存完整计划，当前 User 请求签发后在行锁事务内绑定；未绑定不得领取，失败或中断须收敛。Worker 核对精确租约、原始配置和重新准备的完整分页计划后，用当前 Tenant Service 消费 System 固定源授权入口；拒绝与不可用不进入采样，不缓存 Allow。逐页读取和提交前复核接通前生产采样仍关闭。完整配置摘要复用 `common/execution.NewManagerProfileReadScope`，只能输入持久执行的原始 JSON，不能经 `JSONMap` 浮点数往返。
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
- 2026-10-16 本地验证：共享 `make test-go`、最终 Meta Go T1、Manager PostgreSQL（21 项）及 Meta PostgreSQL（18 项）通过，两项 PostgreSQL 门禁均 0 失败/0 跳过，使用已核实的 `25432/addp_test`，不新建 database。Manager 当前用户来源与有效 lease 下先保存产物后提交扫描由 HTTP/服务回归验证；临时移除 Meta 的 Manager 身份继承时，原发起人读取明确复现 404，恢复后通过。新增 PG 夹具先前的非 UUID lease token、重复执行授权 ID 已按真实字段与唯一索引修正，不删除约束或将失败计为通过。Swagger 覆盖 Manager 93/Meta 44 个公开路由方法，执行夹具门禁通过；新增回归复用现有 Go 与两 Owner PostgreSQL CI 入口，无新增注册。原栅格容量 T4 等待对应会话在包含修复的 SHA 上复跑，未修改其严格断言或重启个人环境。
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

```bash
bash scripts/dev/start.sh -manager
bash scripts/dev/restart.sh -manager
curl http://localhost:8081/health/ready
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
- `manager/docs/tables/task_definitions表.md`
- `manager/docs/tables/task_resource_bindings表.md`
- `manager/docs/tables/vector_materialized_view表.md`
- `manager/docs/tables/model_3d_glb表.md`
- `manager/docs/tables/model3d_tiles表.md`
- `manager/docs/tables/gaussian_splat_ksplat表.md`
- `manager/docs/tables/point_cloud_copc表.md`
- `manager/docs/tables/raster_cog表.md`
- `manager/docs/tables/vector_tile_cache表.md`
- `manager/docs/tables/embeddings表.md`
- `manager/docs/tables/embedding_tasks表.md`
- `manager/docs/tables/embedding_configuration表.md`
- `manager/docs/tables/search_history表.md`
- `common-frontend/CLAUDE.md`
- `meta/CLAUDE.md`

Manager 内部产物验收 `manager-internal-artifact-lineage` 使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `scripts/test/online-hosted-manager-gate.sh` 从零建立最小权限身份、Business MinIO 与真实 Runtime，复用 `make test-online` 业务断言并在退出时销毁环境；无需永久账号或自备 Runner。首次真实运行成功前只登记手工触发，不计为 T4 通过。

Manager Hosted 在启动 Runtime 前明确设置 `ADDP_ONLINE_TEST_RUN_ID=manager-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>`，共享 Runtime 的运行标签和后续业务报告使用同一 ID。生命周期模拟须保留 suite 标签，并忽略其他标签对 suite 归属判断的影响；标准 `make test-manager-online-runner` 和 `make test-online-runner` 覆盖启动、失败退出与清理，不新增入口或 workflow。

2026-10-12 此启动修正的隔离副本通过 `make test-manager-online-runner`：69 项 Python 回归和 Go 身份工具测试，包含缺少 GitHub 运行 ID／attempt 时显式失败、启动前无服务变更及凭据清理。一次性身份工具同步修正 `suite` 字符串指针比较的编译错误，不修改权限或会话策略。完整 `make test-online-runner` 首轮已通过 HDFS 的 95 项、栅格的 110 项及 Manager 的 68 项，但随后公共入口模拟生命周期超过 20 秒；最终 Manager 专项增加上述负向用例后通过 69 项，完整入口串行复验又在 HDFS 夹具清理模拟超过 15 秒。两次完整入口均不计为通过，不改这些其他 suite 的超时上限；后续整轮由已有 Platform CI 验证。`make test-changed` 因 Common／Manager／System 的 T2 连接参数缺失在预检中止，未执行数据库测试；对应门禁仍由既有 Release/T2 编排验证。此记录不代替仍在执行的 XLSX Hosted 页面、下载及最终清理验收。

CSV / TSV 编码选择和 XLSX 多工作表的 Hosted 验收复用上述 Manager suite、Business MinIO、普通 User 和原有两阶段浏览器入口。Business 夹具生成两份固定 GBK 小表（各 25 行、784 字节）和一份四工作表 XLSX（7087 字节）。普通消费身份使用原八项权限，再补现有 `manager.content.read`（原文件下载）与 `manager.data_item.update`（应用编码和普通 item 刷新），共十项；Owner 验收必须拒绝缺少或多出的权限及管理员角色。2026-10-12 用户已明确允许仅本次一次性验收用户使用十项权限，不修改个人或生产账号、不增加管理员权限；不新增权限定义、服务或 Job。

浏览器在 cached-preview 阶段通过真实编码选择与 item 刷新请求核对 GBK、三列中文结构、20＋5 分页、末页、原文件 SHA256、无编码参数的再次刷新及整页重开；Meta 编码、源指纹和内容哈希须保持一致，GBK 不得发布源字节索引。当前报告只有 browser/v10、Owner/v8 一条规范路径，缺少 CSV / TSV 或 XLSX 任一完整证据必须失败；原八权限基线的 browser/v5、Owner/v3 不计为本扩展通过。沿用 `make test-manager-online-runner`、`make test-online-runner` 和已有 Hosted Job；Manager 专项入口覆盖场景、夹具、CI 登记及一次性用户 Go 工具。

[Hosted 38200854607](https://github.com/pampa0629/addp/actions/runs/38200854607) 使用源码 `c0a28c3a9`、Job `114653726264`、attempt 1，九权限用户在 CSV 应用编码时被现行 `manager.data_item.update` 校验拒绝（403）；CSV 下载与 TSV 验收均未执行。九类产物的结果 DELETE 200、任务 DELETE 204、后续内容和任务 GET 404 均有实际日志，PDF / COG 物理对象删除、源文件不变和零残留亦有独立证据；基础设施清理通过、零残留。这些清理结果不代替 CSV / TSV 的验收通过，十权限修正 `df725a476` 已通过专项 65 项及 Go 用户工具回归，完整 `make test-online-runner` 的 909 项 Python 回归与 CI 登记核对；[Hosted 38216365354](https://github.com/pampa0629/addp/actions/runs/38216365354) 使用源码 `df725a47602a8f7750e20940dd89b7add9891f9d`、Job `114701938855`、attempt 1，已失败：十权限准入及 CSV 应用编码通过，首屏 20 行中文逐字段一致，测试却用 `Next page` 查找实际英文按钮 `Go to next page`，停在翻页；原文件下载、普通刷新及 TSV 验收尚未执行。已下载证据并核对九个结果 DELETE 200、九个任务 DELETE 204、各自后续 GET 404，PDF / COG 对象删除、源文件不变及 Infra 零残留；清理通过不代替格式验收通过。已修正该测试的按钮定位，继续使用现有十项权限与同一 Hosted suite，不修改产品、权限定义或浏览器报告契约。 修正后 `make test-manager-online-runner` 的 65 项回归及 Go 用户工具通过。 隔离副本通过标准 `scripts/test/frontend-browser-gate.mjs` 临时探针，使用 Hosted 修正后的同一按钮定位打开真实 Manager 英文组件，核对 20＋5 翻页及末页禁用；仅 UI 夹具，不代替 Hosted 真实下载。同期 [Release/T2 38216214675](https://github.com/pampa0629/addp/actions/runs/38216214675) 的 Meta 编码失败保留测试误将写入前纳秒级时间与 PostgreSQL 读回微秒级时间比较；现改为比较数据库读回的前后完整快照，保留所有属性、大小、行数与时间断言。标准 `make test-meta-postgres` 完整通过，五类编码场景均执行，无跳过；共享工作区 `make test-module MODULE=meta` 在平台 T0 被其他会话的 `agent/backend/tests/test_transfer_precheck_runtime.py` 来源漂移拦截，未进入该入口后续步骤，不计为整轮通过。 隔离副本以提交 `88329610a6685d68f66b40cf42906c4d76e3f014` 加本轮两处测试修正执行标准 `make test-go`，Meta 的全部 Go 包通过（含 SQLite 编码刷新测试）；整轮在第 13 个 Ontology 模块的同一 Transfer 来源审查失败，不登记 22 模块整体通过。本轮不修改该审查的语义记录。 修正已提交为 `2184a9fd7` 并推送；已触发 [Hosted 38229615395](https://github.com/pampa0629/addp/actions/runs/38229615395)，实际源码 `2184a9fd767c290c0ad38a1b4fb882f82b4baa32`，本次 Hosted 已通过（Job `114745590478`、attempt 1），正式 artifact 已下载并独立核对：外层报告、Owner/v4、browser/v6 及两阶段入口均为 passed；普通 User 恰持已批准的十项权限。CSV / TSV 各 25 行、784 字节，真实 GBK 应用请求、中文三列与全部数据、20＋5 分页和末页禁用、Meta 编码／指纹／源哈希、无编码参数的普通刷新及整页重开均通过；不发布不适用的源字节索引。两次浏览器原文件下载 SHA256 分别为 `b2d4a39906f6b5b37cb28d3a8222e93a67c73b09df8dc7a00b5050ffe50a4728`（CSV）和 `2df6d5a2666d0a8ff2a50b917643a6d7430921fdc4454b925de15736c66798e0`（TSV），与固定源文件一致；两份末页截图的中文、页码及禁用控件已检查。九类产物的结果 DELETE 200、任务 DELETE 204及各自后续 GET 404均有实际日志，报告残留资源为零；PDF / COG 的独立物理检查确认对象删除、源文件不变、零残留，summary 确认清理通过、Infra 零残留。本结果只覆盖所用 CSV / TSV 编码样例，不外推设备轨迹业务语义或全部编码变体。同一提交的 [Release/T2 38229579298](https://github.com/pampa0629/addp/actions/runs/38229579298) 已通过（12 个作业成功、27 个条件跳过；跳过不计为执行通过），Meta PostgreSQL 15 的五类编码刷新场景均实际执行并通过；[Platform CI 38229579199](https://github.com/pampa0629/addp/actions/runs/38229579199) 整轮亦通过，含全仓库 Go、Manager 前端和平台一致性。此前来源审查阻断已由对应 Owner 同步，当前工作区标准 `make test-platform-review` 通过；本轮未修改其他 Owner 的语义记录。


2026-10-12 XLSX Hosted 扩展范围：复用现有 `manager-internal-artifact-lineage`、Business MinIO、普通 User 的十项权限与既有清理主线，新增一个固定四工作表的完整 XLSX 源文件（Cities 25 行两列、Readings 一行三列、Empty 零行零列、HeaderOnly 零行两列）。真实 Console 入口核对工作表切换后页码归一与字段替换、Cities 未知总数的 20＋5 分页及末页禁用、普通深度刷新保留当前工作表和页码、空表刷新、整页重开，以及完整工作簿下载的大小与 SHA256。格式事实沿用 `format=excel`、`data_type=container`、`layout=single`，worksheet dimension 仍仅是估算行数，不写成精确总数。只增加 owner 夹具和验收断言，不修改产品 API、权限定义、服务、Job 或血缘实现。规范报告统一升级为 browser/v7 与 Owner/v5，缺少 XLSX 完整证据必须失败；此前 browser/v6、Owner/v4 的 CSV / TSV 通过记录仍保留历史范围，不计为 XLSX 通过。受影响门禁为既有 `make test-manager-online-runner`、`make test-online-runner`、CI 登记检查及原 Hosted Job；现有入口自动覆盖本轮脚本和测试，无需新增 Makefile 或 workflow 路径。实施前登记范围，本扩展尚未运行真实 Hosted，不提前登记通过。 本轮确定性验证：新增夹具测试先复现源 XLSX 缺失，再独立解包核对四个工作表和全部源值；最终固定文件为 7087 字节，SHA256 `317ad1292f7e21861a85607cb2423b10f554ef418b787b9d2d28222106d9b18b`。隔离副本以基线 `f060635fb2a94d9e33e6a1765cb01d7dcac278fb` 加本项改动执行标准 `make test-go`，22 模块全部通过；其中临时 Go 验证用实际 Excel 插件读取这份同一源文件，核对四个 child 的列数／估算语义、不同表结构及 Cities 21–25 行和 Readings 值，临时验证代码不纳入项目。中英文两项标准隔离浏览器探针通过，复用 Hosted 同一工作簿 helper 和原文件下载校验，实际 Manager 组件在受控 iframe/API 中验证切表、未知总数分页、刷新保留页码和空表、完整附件哈希及重新打开；附件由标准测试服务器的同一路径提供，不调用或重启个人开发服务，夹具不代替真实 Hosted。`make test-changed` 因受影响 T2 的连接参数缺失在预检中止，未连接数据库，不计为通过；相应门禁沿用已有 Platform CI、Release/T2 和 Hosted Job。 本轮最终 `make test-manager-online-runner` 的 68 项 Python 回归及 Go 用户工具通过，包含工作簿证据缺失／变更必须拒绝、失败后九类产物清理和 CI 登记字段缺失回归；隔离副本完整 `make test-online-runner` 的 916 项 Python 回归（13 批）、Go 用户工具及 CI 登记核对全部通过。CSV / TSV 和 XLSX 原文件下载现在复用同一测试校验函数，校验大小和源 SHA256；工作簿另核对 ZIP 签名，无新增产品下载路径。 实现已提交并推送为 `4ad7f5c77`；已触发 [Hosted 38260977042](https://github.com/pampa0629/addp/actions/runs/38260977042)，实际源码 `4ad7f5c772b962ed92e48d905680f4837605dc28`、attempt 1，该次 Hosted 已失败（Job `114851090048`）：生成入口浏览器通过，但 DAE 生成的 GLB 图片仍带 URI，被自包含校验拒绝；尚未进入 cached-preview，因此 XLSX、CSV / TSV 的页面与下载复验均未执行。正式 artifact 已下载：实际创建的四个结果 DELETE 200、四个任务 DELETE 204及各自后续内容／任务 GET 404均有日志；PDF / COG 物理对象删除、源文件不变、零残留与 Infra 零残留已独立核对。清理通过不代替格式验收通过。与此前通过运行的源码、Assimp 5.4.3 包版本及 DAE／3DS 源样例比较，未发现相关代码或样例变化；同一源样例的本地真实转换及 DAE 40 种暂存路径未复现。当前 artifact 没有 Model3D Runtime 日志，继续补充生产转换失败诊断和该 Runtime 的退出日志收集后复验，不放宽 GLB 校验、不增加转换路线。 诊断改动已提交并推送为 `6b33eb9dc`；已启动 [Hosted 38274727956](https://github.com/pampa0629/addp/actions/runs/38274727956)，实际源码 `6b33eb9dcf27e1be1db66b44e260fd859ff9eb51`、Job `114897282774`、attempt 1。该次已通过，正式 artifact 已下载，独立复核结果见下文；沿用普通 User 十项权限及 browser/v7、Owner/v5，不增加转换路线。

2026-10-13 独立复核 [Hosted 38274727956](https://github.com/pampa0629/addp/actions/runs/38274727956)：源码 `6b33eb9dcf27e1be1db66b44e260fd859ff9eb51`、Job `114897282774`、attempt 1，外层报告、Owner/v5、browser/v7 及生成入口／缓存预览两阶段均通过，普通 User 恰持已批准的十项权限。GBK CSV / TSV 各 25 行的中文字段、20＋5 分页、编码应用、普通刷新、重开和浏览器原文件下载均通过，784 字节及 SHA256 与固定源一致。XLSX 的 Cities 显示 21–25 行且总数未知、末页禁用；切换 Readings 后重置页码并替换为三列，Empty 零行零列，HeaderOnly 零数据行但保留两列表头，刷新保留当前页／空表选择，整页重开恢复第一页。完整工作簿下载为 7087 字节，SHA256 `317ad1292f7e21861a85607cb2423b10f554ef418b787b9d2d28222106d9b18b`，四工作表元数据及全部源值一致；四份 XLSX 与两份 GBK 末页截图已检查。九个结果 DELETE 200、九个任务 DELETE 204及各自后续内容／任务 GET 404均有实际日志；PDF / COG 独立物理核对对象删除、源文件不变、零残留，summary 确认清理通过及 Infra 零残留。Model3D Runtime 日志已归档，六种模型调用均返回 200；DAE / 3DS 内嵌贴图和 GLB 校验通过，上次 URI 失败没有复现，根因仍未确定，不将诊断改动登记为根因修复。本结果限定所用样例，不外推全部工作簿、编码或设备轨迹语义。同期 [Release/T2 38274592818](https://github.com/pampa0629/addp/actions/runs/38274592818) 通过，含 Manager 生命周期／内容快照；[Platform CI 38274593300](https://github.com/pampa0629/addp/actions/runs/38274593300) 的 Model3D、产品二进制、Go、Manager 和 Console 前端通过，但平台一致性因共享 Runtime 抽取后 HDFS／ES／栅格 CI 登记测试仍查旧文件而失败（1 项失败、3 项错误），不计整轮通过。对应 Owner 已在 `7c7151019` 同步登记，[Platform CI 复验 38325553201](https://github.com/pampa0629/addp/actions/runs/38325553201) 已整轮通过（实际源码 `7c715101941b75a58e2b929dc9ae84b9b1acd446`），包含原失败的平台一致性；该复验不改变前述 Hosted 实际源码，也不将原失败运行登记为通过。本轮不修改该 Owner 的代码。

本轮只更新上述既有验收文档；隔离副本运行标准 `make test-changed`，识别 engines／manager 后因未提供 `MANAGER_POSTGRES_TEST_DSN` 在 T2 预检中止，未执行数据库测试，不计本地整轮通过。正式 artifact 的独立报告校验、实际 HTTP 清理日志校验及六张截图复核均完成；代码门禁沿用上述同源码 Model3D／Manager 回归、Release/T2 与 Hosted 结果，Platform CI 整轮失败单列保留。

2026-10-13 JSON / GeoJSON Hosted 扩展范围：复用既有 Manager suite、Business MinIO、普通 User 十项权限及清理入口，覆盖普通嵌套 JSON 文档、25 行 records array，以及相同点／折线 FeatureCollection 的 `.geojson` 与 `.json` 两种后缀。核对源格式精化、文档／表格／空间材料、20＋5 分页、刷新和重开、实际地图加载及完整原文件下载哈希；不推导设备轨迹语义，不新增产品 API、权限、服务或转换路线。Owner 报告统一为 v6、browser 报告为 v8，缺少任一新增源或证据必须失败；已有 v5／v7 通过记录保留历史范围。受影响标准入口为 `make test-manager-online-runner`、`make test-online-runner`、CI 登记检查及原 Hosted Job，现有路径自动覆盖，无需新增 workflow 或 Makefile 入口。尚未运行真实 Hosted，不计扩展通过。

本轮确定性验证：工作区与隔离副本的标准 `make test-manager-online-runner` 均通过 73 项 Python 回归及 Go 身份工具；固定源字节独立解析、两次夹具生成一致、四源报告缺失／变更必须失败，以及失败后原九类产物清理均已覆盖。隔离副本以基线 `6210696efe654bb9a7a39431cb7cc43bcc249498` 加本项改动运行 `make test-go`，22 个 Go 模块全部通过；临时测试用真实 JSON／GeoJSON 插件读取同一源字节，确认普通文档结构、25 行两列、末页五行，以及两种后缀的内容识别、两条几何记录与 EPSG:4326，临时测试不纳入仓库。标准隔离浏览器入口复用正式 Hosted 同一 helper，中英文两项探针通过，实际 Manager 组件显示嵌套文档、20＋5 记录分页和点／折线，四次完整下载均核对大小与源 SHA256；底图瓦片和服务响应为受控夹具，不能代替真实 Hosted。探针复现了刷新后立即重载导致响应体读取丢失，验收脚本现等待新预览及已收到响应读取完成，再执行重开；未忽略读取错误。CI 登记核对通过，无新增产品 API、依赖、权限或运行服务。九文件隔离范围的 `make test-changed` 因多个 Owner T2 连接条件缺失在预检中止，未执行数据库测试，不登记本地整轮通过；沿用既有 Release/T2 和 Hosted 门禁。

隔离副本完整 `make test-online-runner` 的 940 项 Python 回归（13 批）、Go 身份工具及 CI 登记核对全部通过；工作区 Manager 专项已按当前共享文件复验通过，其他 Owner 的变更保留。以上仍是确定性门禁，新增四源真实 Hosted 尚未执行，不提前登记通过。

实现已提交并推送为 `dc0c3f687d6436e6da3c0ccd3f3c579d6e953963`；[JSON / GeoJSON Hosted 38328291459](https://github.com/pampa0629/addp/actions/runs/38328291459) 已触发，实际源码为该提交、attempt 1，当前排队中。沿用现有普通 User 十项权限及 browser/v8、Owner/v6；真实四源页面、下载和最终清理须待正式 artifact 返回后独立核对，不提前登记通过。

FBX／OBJ Hosted 扩展复用同一 suite、普通 User 十项权限和两阶段浏览器入口，不新增服务、权限或转换路线。固定 FBX 盒子复用 Model3D 产品构建夹具和原始许可；OBJ 使用自建三角形、MTL 和已有 4 × 4 红色 PNG。Meta 核对 model_3d/single，生成前和清理后使用 Manager 原文件下载接口核对模型字节；GLB 核对 FBX 源角点、节点变换、基础颜色与十二个三角面，以及 OBJ 尺寸、UV、实际内嵌 PNG 像素和不透明度。OBJ 基础预览只验证几何加载，外部材质保真由 GLB 快显验证。固定 FBX 基础预览曾被归因为上游 issue 31247；2026-10-17 实际解析确认它包含 96 个 RGBA 数值，却在 `ByVertice`／`IndexToDirect` 颜色层中缺失 `ColorIndex`，不能视为空颜色层或宣称单靠升级修复；2026-10-13 用户确认本轮先验收 FBX GLB 快显，不将其基础预览记为通过，依赖升级另行评估。Owner/v7、browser/v9 为本扩展唯一契约，八种模型合计十一类产物；缺少 OBJ 基础预览、任一格式原文件下载、缓存加载或源不变证据必须失败。原有血缘断言及清理逻辑沿用，只扩展本轮资源集合，不修改血缘产品实现。确定性入口仍为 make test-manager-online-runner、make test-online-runner，真实结果仍由既有 Hosted Job 验证；未完成实际运行前不记八模型 T4 通过。

本扩展的隔离副本验证：`make test-manager-online-runner` 的 76 项及 Go 身份工具通过，完整 `make test-online-runner` 的 968 项 Python 回归（13 批）及 CI 登记核对通过；最终按已确认的 FBX GLB 范围再次执行 Manager 专项通过。既有 Linux ARM64 最终 Runtime 镜像经正式 operator 转换同一固定源，FBX 的 24 个顶点／源变换／颜色与 OBJ 的三角形／内嵌 PNG 像素通过新断言。标准浏览器入口的临时中英文探针共两项通过，已目视核对 FBX GLB 及 OBJ 基础几何、带贴图 GLB，原文件下载哈希一致；探针使用受控 API，不能代替 Hosted。临时测试与 Vite 文件服务已移除，未升级依赖或重启用户服务；正式八模型 Hosted 尚未执行。

本扩展隔离副本的标准 `make test-changed` 因所选共享 CI 登记影响面所需 T2 数据库连接条件未注入，在统一预检中止；未执行其后续 T0—T3，不记整轮通过。已运行的专项与 Online runner 结果独立有效；完整扩散验证由既有 Platform CI、Release/T2 及后续 Manager Hosted 门禁承接。

扩展实现已提交并推送为 `3a8d809209b3d2661e6f9eadd13b2849470b8501`；合入当前工作区后 Manager 专项 76 项及 Go 身份工具再次通过，CI 登记检查通过。[FBX／OBJ Hosted 38337360185](https://github.com/pampa0629/addp/actions/runs/38337360185) 已触发，实际源码为包含该改动的 `b2e94c79b408a6297461f758f0ac1327d8b568c7`、Job `115100364456`，当前 pending。沿既有套件串行等待 JSON／GeoJSON Run `38328291459`，不取消或重复触发上一轮；两轮的实际浏览器、下载和清理结果分别核验，当前均不提前登记通过。

2026-10-13 正式失败证据收口：JSON／GeoJSON Run `38328291459`、Job `115071985839`、源码 `dc0c3f687d6436e6da3c0ccd3f3c579d6e953963` 为 failure。停在 `features.geojson`：浏览器等待 `data.object.content.frontend_renderer=map`，实际 Manager 日志确认 `builtin:file-table` 与 `preview_type=table`；截图显示 EPSG:4326 点／折线和两条属性记录。现有对象内容规范与实际文件表路由的展示契约存在冲突，已向用户提交“地图＋属性表”与“GeoJSON 文档＋地图”的统一方案确认；确认前不修改依赖该决定的代码或测试，不接受 undefined，也不延长等待。六模型缓存、CSV／TSV GBK 全流程、XLSX 四工作表及下载哈希断言已先于失败点执行完成，普通 JSON 文档和 records array 也已执行至下载、刷新及重开；两种 GeoJSON 的完整下载与刷新尚未执行，没有 cached-preview 成功报告，整轮不计通过。实际 Manager JSONL 独立核对原九个产物 DELETE 200、九个任务 DELETE 204 及各自后续 GET 404；PDF／COG 物理删除报告均确认对象删除、源不变、零残留，外层 summary 确认 cleanup passed、Infra zero_residuals，未把 Infra 销毁替代领域删除证据。后续 FBX／OBJ Run `38337360185` 复用了相同错误断言，在 Job 尚无 steps 时取消，当前 completed/cancelled；保留全部旧失败证据，待统一契约及验证完成后再触发一次新增范围验收。

2026-10-13 用户确认 GeoJSON 的唯一基础预览为“地图＋属性表”。已先修订 Manager 预览语义协议，再将 Hosted GeoJSON 两种后缀的断言统一到实际 `preview_type=table`：核对全部属性／Point 与 LineString 几何及源坐标、EPSG:4326、未变换 CRS 契约、属性表与真实地图 canvas、末页禁用、原文件下载、普通刷新及重开。普通 JSON 文档和 records array 仍各自严格验证。报告统一为 Owner/v8、browser/v10，新增 table_verified、geometry_verified、source_crs 证据，缺少或错误必须失败；不接受旧报告、不放宽警告或失败响应，不改产品路由、权限、血缘实现、Runtime 或等待时间。受影响入口仍为 make test-manager-online-runner、make test-online-runner、Manager 前端浏览器门禁及既有 Hosted Job，原有路径登记已覆盖。修正后的真实 Hosted 尚未执行，不提前登记通过。

本次修正验证：隔离副本的标准 `make test-go` 全部 22 个模块通过，临时 Go 探针经真实 FileTable Provider 读取相同固定 GeoJSON 字节并保存实际表格／源 CRS／几何响应，探针已移除。使用核实的 Node v24.19.0，经标准 frontend-browser-gate 复用最终 Hosted 同一 helper，中英文两项临时探针均通过；`.geojson` 与 `.json` 各自的地图要素点击、属性弹窗、完整下载 SHA256、刷新和重开通过，截图已目视确认点、折线和属性。受控身份及接口不代替 T4。临时 Vite 下载端点、浏览器测试和依赖链接已移除，未改产品路由或启动用户服务。Manager 专项 76 项及 Go 身份工具通过，CI 登记一致；标准 make test-changed 因多 Owner T2 连接条件缺失在预检中止，不记整轮通过，完整扩散验证仍由既有 Platform CI／Release-T2 及本 suite 的 Hosted 验证。

修正已提交并推送为 `cfbb3b5a453b8e2524f2d47b511b3243d892fd77`；合入当前工作区后的 Manager 专项 76 项及 Go 身份工具再次通过，CI 登记检查通过。唯一新增 [Manager Hosted 38341080755](https://github.com/pampa0629/addp/actions/runs/38341080755) 已触发，实际源码为该修正提交、Job `115111976721`，当前 queued。该轮同时保留四种 JSON 源、CSV／TSV／XLSX、八模型及原 PPTX／点云／栅格，Owner/v8 与 browser/v10；最终原文件下载、十一类产物领域删除和基础设施清理仍须以实际报告分别核对，不将本地探针或旧失败轮清理当作本轮 T4 通过。

2026-10-13 Run `38341080755`、Job `115111976721` 已结束为 failure，源码 `cfbb3b5a453b8e2524f2d47b511b3243d892fd77`。失败发生在生成前的 OBJ 夹具核验：误将 DataItem 的整体 `size_bytes` 与原模型下载大小比较。Meta 真实持久日志显示 OBJ 主文件 97 字节、MTL 45 字节和贴图 76 字节合计 218 字节，主文件 SHA-256 与固定源一致。验收必须分别核对数据项总大小和主文件下载大小，不能修改 Meta 的整体大小语义或删除原下载断言；确定性假客户端应复现真实 218 字节，夹具回归同时核对三个文件字节数及合计。Manager 实际日志没有生成请求，故本轮未创建十一类受管产物，也未执行正式浏览器与格式下载验收；不能登记领域删除通过。外层生命周期 cleanup passed、Infra zero_residuals。最终 Linux x86_64 Runtime 构建已完成正式 FBX 静态几何与 OBJ 内嵌 PNG 验证，但不代替页面 T4。修复沿用既有 Manager runner、Platform CI 自动登记和唯一 Hosted suite，不改权限、报告版本、产品 API 或血缘实现。

本项最小回归已先复现旧断言失败，再由标准 `make test-manager-online-runner` 验证修正后的 77 项及 Go 身份工具全部通过；保留源文件下载大小与 SHA-256、两阶段页面和全部原场景的严格断言，Owner/v8、browser/v10 不变。现有 `test-platform → test-online-runner → test-manager-online-runner` 自动覆盖本次脚本和夹具回归，无需新建 CI 入口；`make test-changed` 因共享工作区多 Owner 变更及缺少 T2 连接条件在预检中止，未执行数据库测试，整轮不计通过。Hosted 须在修正提交重新执行；本地回归不替代 T4。

本轮正式验收收口：[Manager Hosted 38377560103](https://github.com/pampa0629/addp/actions/runs/38377560103)、Job `115229667480`、attempt 1、源码 `c1c47c6d8df1f80b63043de1679daaf9188c94da` 已通过。独立下载 artifact 核对外层报告、Owner/v8、browser/v10、生成入口／缓存预览两份原始报告一致；System、Gateway、Meta、Manager、Monitor 五个服务的构建源码均匹配该提交，普通 User 恰持已批准的十项权限。四种 JSON 源均完成真实页面、下载 SHA256、普通刷新和重开；records array 为 20＋5 行且末页禁用，`.geojson` 与 `.json` FeatureCollection 均核对两条 Point／LineString、EPSG:4326、地图＋属性表及实际点选弹窗。GBK CSV／TSV 各 25 行的编码应用、中文三列、分页、末页、刷新、重开和源下载哈希再次通过；XLSX 的 Cities／Readings／Empty／HeaderOnly 四工作表、未知总数、切换重置、刷新保留页／空表和完整工作簿下载再次通过。

八模型 DAE／3DS／IFC／OSGB／SKP／MAX／FBX／OBJ 均通过真实 Runtime 转换与 GLB 缓存预览。FBX 为 24 顶点，保留源节点变换、尺寸及颜色；OBJ 为 3 顶点、2 × 3 三角形，UV、不透明材质和实际内嵌 PNG 像素通过，基础预览核对几何。FBX 主文件 17200 字节与 OBJ 主文件 97 字节在两阶段下载的 SHA256 均与固定源一致，清理后主文件不变；OBJ 数据项整体大小另按主文件＋MTL＋PNG 的 218 字节核对。MAX 使用内部 Blender／固定 io_scene_max 1.9.2，毫米换算、5 网格、12 图元、444 顶点、5 实例和内嵌贴图通过；SKP 的英寸到米尺寸、两个共享实例及彩色 PNG 通过。PPTX 三页 PDF 的缓存复用与刷新保留第 2 页、COG Range 和地图、点云预览等原场景同时通过；不扩展血缘产品实现。

独立解析实际 Manager JSONL，按本轮结果／任务 ID 核对十一项结果 DELETE 200、十一项任务 DELETE 204 及各自后续内容／任务 GET 404，十一类报告残留资源均为零，模型预览模式已恢复。PDF／COG 的独立物理报告确认对象删除、源文件不变、零残留；summary 为 cleanup passed、Infra zero_residuals，未以 Infra 销毁替代领域清理。六张 GeoJSON、XLSX、FBX、OBJ、MAX 截图已目视复核；浏览器业务警告／错误及失败业务响应为零，另按既有协议记录 4 条 Chromium WebGL driver 性能诊断，不视作业务成功证据。此次只认证所用样例和静态主线；SGM 不在本轮 Hosted 内，固定 FBX 的基础预览缺陷、全部厂商版本、动画、复杂材质及设备轨迹业务规则仍未认证。

同源码 [Release/T2 38377388800](https://github.com/pampa0629/addp/actions/runs/38377388800) 通过（21 个作业成功，18 个条件跳过；跳过不计执行通过），Quality frontend smoke 通过。[Platform CI 38377388963](https://github.com/pampa0629/addp/actions/runs/38377388963) 原轮停在 `ontology/backend` 的 go.mod／go.sum tidy 差异，不能登记原轮通过；对应 Owner 已在 `d56c8806d` 修正。包含本轮及该修正的后续 [Platform CI 38396404876](https://github.com/pampa0629/addp/actions/runs/38396404876)、源码 `eef85fca0ccd3937110b0e2eca0482f9f0d84f65` 已整轮通过，含 Go workspace；本轮五个夹具／Owner／浏览器实现文件与该源码一致。此复验不改写 Manager Hosted 的实际源码。本次仅维护既有验收文档，不改产品、依赖、权限或用户服务。

本次验收记录更新的 `git diff --check` 通过；标准 `make test-changed` 再次因共享工作区多 Owner 所需 T2 连接未注入而在预检中止，未执行数据库测试，不计本地整轮通过。此次不变更实现和 CI 编排，代码验证复用上述真实 Hosted、专项回归、Release/T2 及后续 Platform CI 证据。
