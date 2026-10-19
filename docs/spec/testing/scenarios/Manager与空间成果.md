# ADDP Online 场景：Manager与空间成果

[返回测试与验收总入口](../../addp测试与验收规范.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

本页场景：[manager-internal-artifact-lineage](#manager-internal-artifact-lineage)、[manager-hybrid-search](#manager-hybrid-search)、[vector-pmtiles-workflow](#vector-pmtiles-workflow)、[raster-workflow](#raster-workflow)、[raster-contour-persistence](#raster-contour-persistence)、[raster-polygonize-persistence](#raster-polygonize-persistence)。

## manager-internal-artifact-lineage

### 部署、模型转换与通用清理

Manager 内部产物 T4 使用 `manager-internal-artifact-lineage` 单一 suite 和 GitHub Hosted Linux x86_64 disposable profile，不再由 macOS self-hosted 分发。`online-hosted-manager-gate.sh` 复用共享 Hosted 生命周期，从零建立 Infra、最小权限 User、Tenant 和 Business MinIO Engine；浏览器登录凭据只保存到当次仓库外 owner-only Secret 目录。通过正式构建入口构建 Linux amd64 Model3D 转换器及 Runtime 产品镜像，以镜像默认入口启动 Model3D Runtime；标准开发生命周期启动 System、Gateway、Meta、Manager、Monitor、Console、PointCloud 和 Document；不启动无关模块或模拟转换器。退出时停止应用并销毁 Business MinIO 容器、网络、数据卷及当次 Infra，检查零残留。Business MinIO 的确定性生成 LAS、仓库已跟踪的 3 页 PPTX、带 PNG 贴图的 DAE 与 3DS、仓库自有 IFC4 米制立方体及 zlib 压缩并内嵌 DXT5 红色贴图的单体 OSGB 必须经过同一次真实 Meta 深度扫描，再通过对应 Manager owner action/task 与真实 Runtime 生成 COPC、PDF、GLB。模型断言覆盖四种格式的 `model_3d + single` 分类；DAE/3DS 核对贴图资源引用及 GLB 内嵌 PNG，IFC 核对 STEP schema、实体摘要及米制立方体几何，OSGB 核对 GLB 内嵌 JPEG 与带 UV 的三角形。全部产物核对自包含 GLB 和非空网格、Manager/Monitor 血缘一致，以及 Console iframe 中 GLB 请求成功、模型加载完成和截图。Online Chromium 统一显式使用 SwiftShader 软件 WebGL，在无独立 GPU 的 Hosted Runner 上仍执行真实模型绘制；不屏蔽业务警告或用截图占位。浏览器报告 v5 仅将 Chromium WebGL Driver 的精确 `GPU stall due to ReadPixels` 性能诊断单列，记录来源、观测阶段及各阶段次数（包括栅格重投影/预览与截图），不将预览期间的诊断归因为截图。次数表示浏览器输出的诊断条数，浏览器可能限流，不能推断实际 GPU 读取次数或诊断未再次出现即无读取。共享前端测试工具独占精确消息分类；其余页面警告、错误及失败的业务请求仍使验收失败。重投影 T3 必须核对真实输出像元并断言无 Canvas getImageData 读取，不以诊断分类替代渲染验收。临时产物及任务按本轮捕获 ID 删除，内容和任务分别确认 404，模型源的用户预览模式恢复原值；执行尚未终结或无法确认资源归属时不得报告零残留。确定性脚本测试沿用 `make test-online-runner`，真实运行沿用 `make test-online ONLINE_SUITE=manager-internal-artifact-lineage`；本次模型扩展首次在 Hosted 环境通过前不计为已完成 T4，也不增加定时调度。

Manager Hosted 在启动 Runtime 前明确设置 `ADDP_ONLINE_TEST_RUN_ID=manager-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>`，共享 Runtime 的运行标签和后续业务报告使用同一 ID。生命周期模拟须保留 suite 标签，并忽略其他标签对 suite 归属判断的影响；标准 `make test-manager-online-runner` 和 `make test-online-runner` 覆盖启动、失败退出与清理，不新增入口或 workflow。缺少 GitHub Run ID／attempt 时，生命周期必须在变更服务前明确失败并清理凭据。

### 点云与 PPTX

Manager 点云与 PPTX 专用断言复用同一 Hosted 场景及当次 Business MinIO 与 Engine Instance。Fixture owner 幂等写入仓库内确定性小型 LAS 与多页 PPTX 样本；suite 经 Gateway 触发真实 Meta scan，并使用扫描所得的 ResourceLocator、item ID 和 fingerprint 验证两条正式链路：`point_cloud_copc_generation` 必须由 PointCloud Runtime 从业务对象存储读取源文件、向 Manager infra MinIO 发布 COPC，并由 Manager execution 写入 `addp.lineage-facts/v1`；PPTX 预览必须由 Quick View Capability 声明 `generate_pptx_pdf`，并通过统一 action 入口首次触发 `pptx_pdf_generation`，由 Document Workflow / LibreOffice 发布多页静态 PDF，同源再次读取 Capability 只复用同一任务与结果，不创建第二次 execution。Monitor API 与真实浏览器必须展示同一业务输入和 `addp-infra://` 输出；浏览器还必须在 Data Explorer 翻到后续 PDF 页，跨过 Engine 状态周期刷新后仍保持当前页。退出路径通过 Manager 正式 API 删除两类结果与任务、验证对象不可再读取及临时资源零残留；不得直接操作数据库或 infra MinIO 清理。

### 彩色 FBX 只读预览

初始视角仍要求红、绿、蓝三种主色各有足够的真实像素。旋转、缓存恢复和重新打开后的视角可以显示源模型的黄色、品红、青色面，因此这些阶段按 RGB／CMY 六个明确色系核对至少两种着色，不能把“未同时看到两种 RGB 主色”误判为丢失顶点颜色；面积、像素阈值、旋转前后变化和源哈希断言保持不变。

同一 Manager Hosted suite 增加官方彩色 FBX `vCube.fbx` 的只读基础预览场景：Business 夹具原样复制仓库固定样例至 `model3d/fbx/vCube.fbx`，沿用既有扫描与十项普通 User Permission；与缺少颜色索引的 `model.fbx` 转 GLB 场景并列，不能替换原转换几何断言。在生成入口和缓存阶段分别直接加载彩色源，首轮确认真实截图的三种主色，两轮均核对旋转变化、重开后的着色及下载 SHA-256；不得为该源生成任务或产物。Owner 在扫描后和清理末尾再次核对原文件字节、空成果和空任务，失败也必须执行该检查；源由当次 Business MinIO 生命周期销毁，既有十一类领域／物理清理保持原断言。浏览器报告唯一版本升至 v12，Owner 报告升至 v10；旧版本拒绝。沿用 `make test-manager-online-runner`、`make test-online-runner`、Online CI 登记及既有 Hosted workflow，不增加服务、部署入口或权限；真实运行通过后方能认证新增 T4 范围。

<a id="max-模型-hosted-验收"></a>

### MAX 模型 Hosted 验收

MAX 的 Manager Hosted T4 扩展复用 `manager-internal-artifact-lineage`，从专用 Business MinIO 读取未修改的 CC-BY-SA-3.0 马模型及 ADDP 自建 4 × 4 RGB `(220,30,60)` 贴图。页面验证默认米、手选毫米和显式 bitmap 映射；正式生成声明 `source_unit=mm`，核对转换审计、五个静态网格、十二个 primitive、444 个顶点、米制场景尺寸和实际内嵌贴图像素，不固定跨 Blender 版本的 PNG 编码哈希。Meta 仅识别 MAX 格式身份；六种模型与点云、PPTX、栅格共同核对九项输入／输出血缘、缓存加载和零残留清理。确定性入口仍为 `make test-manager-online-runner` 与 `make test-online-runner`；真实验收仍为现有 Hosted suite，实际运行通过前不计为 T4 通过。

### 表格、JSON 与八模型固定断言

CSV／TSV 索引源版本复验复用同一 Hosted 独占 NFS 导出和原十项普通 User 权限。NFS 生命周期只在本轮私有根目录准备两个专用 ASCII 源；Owner 精确深扫并确认版本绑定的稀疏索引，验证两页内容后，在宿主夹具目录同大小改写字段与数据并显式推进修改时间。两页预览均须返回 `409/preview_source_refresh_required`，不得附带列、行或总数，不得自动改写 Meta 快照；同一用户仍可下载实际源字节。通过 Manager 原 item 刷新入口重建后，身份保持、绑定更新，两页必须显示新字段和新值。成功或失败均还原原字节、刷新并复核原分页与下载，再由既有 NFS 生命周期删除整个私有导出；写入前校验 Hosted Linux x86_64、私有根路径、Run owner 及无符号链接。原模型及领域／物理清理断言保持。本机标准入口 `make test-manager-online-runner` 仅证明夹具、拒绝边界及登记，真实存储及跨模块 T4 必须以对应版本完整 Hosted 通过证明。

MinIO 源变更复验继续使用该 suite 的现有隔离 Business MinIO 和固定 mc 镜像，不新增服务、用户权限或产品写接口。只允许生命周期在已有 Hosted Run 私有根与 owner 标记核验后改写两个精确 `index-source-versions/source.csv|tsv` 对象；命令动作固定为对应格式的 change／restore，拒绝原始字节或已知变更字节之外的对象，不接收任意路径或数据。NFS 与 MinIO 共用同一预览、409 拒绝、刷新、分页和下载验证逻辑；MinIO 还须核对源绑定中的 ETag 在同大小改写后改变，成功或失败均精确还原原字节、刷新并复核原页及下载，原业务容器销毁仍须零残留。四个 storage／format 案例在唯一 `index-source-versions.json` 中登记源绑定与字节哈希，证据版本升 v2，Owner 报告升 v13、浏览器仍 v13；历史 v1 报告不替代四案例的 v2 证据。未取得对应新版本完整 Hosted 通过前，MinIO 源变化仍属未验，不外推外部云 S3 或一致性快照。

同一轮复用既有十一份转换执行，核对 `model_3d_glb_generation`、`pptx_pdf_generation`、`raster_cog_generation` 和 `point_cloud_copc_generation` 四类成功任务的公共过程事件，不新增任务、权限或等待预算。通过 Monitor 原事件接口按 ID 游标分页到末页，严格核对闭合分页／事件字段、实际执行 UUID、首 claim 的 attempt=1、递增事件 ID、带时区的保留窗口内时间，以及唯一 started 在前、唯一 completed 在末；允许零至多条仅含整数百分比的 progress，不要求进度数量，拒绝截断或失败终态。生命周期首末事件不得含步骤或计数，过程事件不得出现参数、样本、Runtime 正文、lease 或授权信息。Owner 在 `manager-execution-events.json`（`addp.manager-execution-events/v1`）及完整报告中只归档执行 UUID、任务类型、attempt、事件／页数及安全检查结果，不保存完整事件响应；事件通过不替代专业结果、血缘或清理，也不外推其余八类任务和失败、过期、进度写入的 T4。

同一 Manager Hosted suite 继续使用当次 Business MinIO、原十项普通 User 权限和生成入口／缓存两阶段浏览器，不新增服务或权限。身份只允许既有 `manager.content.read`（原文件下载）与 `manager.data_item.update`（应用编码和 item 刷新）等原十项权限，Owner 拒绝缺少／多出 Permission 或管理员角色；该限定不扩展个人或生产身份。完整报告版本以当前 Owner、浏览器实现和 Online CI 登记为准，历史版本不替代当前完整证据。

- CSV／TSV：两份固定 GBK 小表，各 25 行、784 字节。真实读取设置和 item 刷新先核对三列中文、20＋5 分页、末页禁用、源大小／SHA-256、无覆盖参数的再次刷新及整页重开。随后实际选择“首行为数据”，核对请求布尔 `has_header=false`、稳定 `Column1`～`Column3`、总数 26、原表头作为首条数据、20＋6 分页、普通刷新继承和重开；最后从同一设置组恢复表头模式并复核原 25 行。两阶段 Meta 的实际模式、编码、源指纹及内容哈希必须一致，下载原字节不变；GBK 不发布源字节索引。新增表头模式证据使用 suite／browser v14，旧 v13 不计该能力通过；该扩展复用原十项权限、业务存储和清理边界。
- XLSX：固定完整工作簿 7087 字节，Cities 25 行两列、Readings 两行三列、Empty 零行零列、HeaderOnly 两列表头零数据。格式事实为 `format=excel`、`data_type=container`、`layout=single`；核对不同列结构、未知总数下 20＋5 分页和末页禁用、切表重置、刷新保留当前页／空表、重开回第一页及完整工作簿下载；worksheet dimension 仅为估算，不写成精确总数。
- JSON：普通嵌套文档与 25 行 records array 分别证明文档展示和 20＋5 表格分页。相同 Point／LineString FeatureCollection 的 `.geojson`、`.json` 两后缀均按唯一 `preview_type=table` 主线验证地图＋属性表、完整属性／几何、EPSG:4326、未变换 CRS、真实 canvas／要素点击、末页、下载／刷新／重开；普通文档不误入 records 或空间分支，不推导设备轨迹规则。
- 八模型：DAE／3DS／IFC／OSGB／SKP／MAX／FBX／OBJ 均核对 `model_3d/single`、自包含 GLB 和真实几何。原 DAE／3DS 贴图、IFC schema／实体、OSGB JPEG／UV 断言保持；SKP 核对英寸到米、共享双实例、内嵌彩色 PNG 且无虚构解析元数据；MAX 核对毫米审计、五网格及真实 RGB 贴图。FBX 核对源角点、变换、颜色与十二个三角面；OBJ 核对尺寸、UV、PNG 像素和不透明度，基础预览只验证几何。固定 FBX 的基础预览未认证边界见[三维预览说明](../../../../manager/docs/三维模型、点云与高斯泼溅预览说明.md#二数据类型与预览路线)。
- 原文件与清理：FBX／OBJ 在生成和缓存两阶段下载主文件核对 SHA-256，清理后源主文件不变。OBJ 主文件 97、MTL 45、PNG 76 字节合计 218 字节；DataItem 整体大小与主文件下载大小分别核对。十一类产物继续验证 Manager／Monitor 血缘、正式结果／任务删除和后续内容／任务 404，PDF／COG 独立物理核验不由 Infra 销毁代替；表格／JSON 源不创建派生产物。

固定源、报告缺失／变更拒绝、场景与生命周期沿用 `make test-manager-online-runner`、`make test-online-runner` 和既有 Hosted Job。实际历史源码与通过边界见[证据索引](../验收证据索引.md#manager-预览与私有导入历史证据)。

## 拓扑构面双输出持久化（vector-topology-persistence）

独立手工 Hosted Linux x86_64 T4，验收 `load → polygonize → save_faces / save_diagnostics`、独立自相交面源的 `load → check_geometry → repair_geometry → check_geometry → save_repaired` 及下述参考吸附分支，三条计算分支在同一成功 execution 中执行，不复用 PMTiles 或等高线 suite 的通过结论。标准入口为 `make test-online ONLINE_SUITE=vector-topology-persistence`；使用共享 Hosted 生命周期和 NFS export 所有权实现，拒绝个人环境，保持产品 GeoPython 镜像默认入口、构建身份、失败清理及零残留约束。首次真实通过前不增加 schedule，不将代码或确定性回归通过记为 T4。

System 测试夹具仅为当次 disposable 非默认 Tenant 创建普通 User，精确授予 11 项权限：`develop.task.read`、`develop.task.execute`、`develop.data_read.execute`、`develop.data_write.execute`、`system.execution_authorization.create`、`meta.catalog.read`、`meta.scan_task.read`、`meta.scan_task.execute`、`meta.lineage.read`、`monitor.execution.read`、`manager.data_item.read`。执行使用既有 ad-hoc API，无任务定义编辑或引擎管理权限；Engine 登记身份与业务 User 分离，登记后移除基础设施 Token。入口和 Console 浏览器均核对同一 User、Tenant 与精确权限集合，拒绝管理员和多余权限。

确定性 NFS 线网声明 EPSG:4326，包含闭合方框、两条贯穿线及重复边。默认输出引用省略 `port`，诊断显式引用 `port=diagnostics`；两个既有保存节点分别写入新 GeoJSON，使用 `mode=fail`。独立物理 oracle 按预定义坐标核对四个面、四条外围 dangle、字段和 CRS，不调用构面算子生成期望；同时核对源 SHA/大小、准确目标清单及无临时旁路文件。三个目标必须各自具备一次精确自动扫描、正式 DataItem、物理与 Meta 相等的正整数大小、同一 actor/Tenant、指向同一 Develop execution 的 derive 及一致 Monitor 事实。不得手动补扫输出伪装自动扫描成功。

自动扫描身份沿 Meta `ScanExecutionResponse` 的公开 `tenant_id` 与 `triggered_by` 核对，两者必须为正整数并分别等于当前 Tenant 和 User Principal；不读取或回退到私有 `actor_principal_id`，不为验收扩展 Meta DTO 或权限。

文件保存继续使用 `mode=fail`，成功后的三份稳定输出及 `lineage_facts.outputs` 必须分别包含标准 `write_mode=create`，不能将缺少该事实或原始值 `fail` 视为合格。血缘仍须匹配同一 Develop execution 的当前 active derive；不修改 Meta 对未知写入模式只保留观察的规则。

GDAL 保存成果中的显式 CRS84 声明必须进入标准空间事实 `crs_ref=OGC:CRS84`，不得补造 EPSG SRID。浏览器核对 `source_crs=OGC:CRS84`、SRID 为零或省略、`not_transformed` / `direct_renderable`，并以原经纬坐标验证四面四余线、修复后两条 MultiPolygon 及三幅真实地图；不得临时选择坐标系或改文件声明来通过验收。

等待血缘前先核对三份成果的严格大小，并归档 `topology-before-lineage.json`：包含本轮身份、公开扫描主体、资源血缘事实、Monitor 响应、物理哈希及三份大小证据，标记 `result=incomplete`。即使后续超时，已验证阶段仍可复核；该文件不能替代最终成功报告、血缘、浏览器或退出清理证据。

同一普通 User 经 Console 查看专业结果与三份 Manager 预览，核对记录、字段、源 CRS 和真实地图像素；浏览器报告须绑定 Run ID、User、Tenant、execution、item 和 locator，禁止 mock 响应或内部图对象注入。缺失输出、错端口、错扫描身份、源或目标内容变化、空白地图、旧报告与清理失败均失败。NFS export/目录、Runtime、registry 与当次 Infra 的退出清理不等同于 Manager 业务 DELETE 验收。

新增 `invalid.geojson` 含两条带业务属性的自相交面；`repaired.geojson` 必须一源要素一行、每行精确两个三角面，保留 source_name/grade，修复前 false 和自交原因、复查后 true/Valid Geometry 均须物理和预览核对。两份源文件的内容与 Meta 事实在浏览器访问前后不变；不增加 User 权限、等待预算或手工补扫。旧双输出版本的真实 T4 证据仍只证明原范围，不外推本次增强。

浏览器按本次导航的真实请求身份读取源 Meta 和当前 active derive 原始图，归档到 `topology-source-lineage-raw.json`，不跨导航持有旧 Token。当前增强版报告版本和新增参考吸附范围见下文。

参考吸附扩展沿用同一 suite、原 11 项权限、独占 Hosted 生命周期和 600 秒业务预算，不增加入口或 schedule。新增 `snap-input.gpkg` 与 `snap-reference.gpkg` 两份单图层 EPSG:3857 米制源；同一成功 execution 追加双 `load → snap_geometry(tolerance=0.75) → save(mode=fail)`，生成第四份成果 `snapped.gpkg`。固定五条输入线和六条参考线，独立坐标期望核对原始位置最近匹配、0→0.5→1 链不越界、恰在容差边界不动、等距按 X/Y 决胜、不向参考线段投影且不插删点；保留 source_name/grade、行序、部件及投影 CRS。GeoPackage 保留原始投影坐标，不借转换到经纬度代替几何核验；Meta 文件须为 single/container/geopackage，图层事实保留在容器内。

第四份成果须严格核对物理与 Meta 正整数大小、一次同 User/Tenant 的精确自动扫描、两个吸附源与本次 execution 的 active derive、Monitor、容器图层 EWKB 原始坐标和真实 OpenLayers 要素／像素／截图；四份源及全部既有成果在浏览器前后不变。另执行 tolerance=1.25 的失败 DAG，末条线双端吸到同一点导致塌缩，必须整批失败且保存未执行；不得出现失败成果、自动扫描或任何源／既有成果变化。失败执行状态与 Monitor 也须绑定同一用户。最终报告唯一版本升为 `addp.vector-topology-persistence/v3`、`addp.topology-browser/v3` 和 `addp.topology-checkpoint/v3`，原始血缘归档覆盖四源、第四份成果的双来源及各次实际预览响应；`topology-physical/` 保留四份合成源和四份已核验成果原文件，按 SHA/大小核对且拒绝覆盖不同证据，支持下载后独立 GDAL 复核。归档不属于已清理的 NFS 业务目录；incomplete 仍不可替代最终通过。此前构面／几何修复 T4 只证明原范围，本扩展在真实 Hosted 通过前不得计入参考吸附 T4。

### KML／GML／TopoJSON 的 S3 兼容消费

容器图层沿既有 scope-table 路线返回 EWKB，HTTP JSON 中的二进制几何为 Base64。浏览器验收须先按该传输契约解码，再逐层核对几何类型、完整源坐标和 Z 维度；地图实例及像素仍独立验证，不能要求产品接口改返 GeoJSON 对象来满足测试。刷新后的响应与报告中的规范化几何使用同一解码入口。

`TablePreview` 的零值 `source_srid` 和空 `source_crs` 在 HTTP JSON 中按 `omitempty` 省略。未知 CRS 的固定 TopoJSON 样例必须核对两字段缺省及 `transform_status=unknown_crs`，不能要求虚构 `source_srid=0` 字段或默认 EPSG:4326；本地页面夹具与 Hosted 使用相同的字段缺省契约，临时 CRS 选择及原始几何检查保持。

地图缩小交互按活动 OpenLayers 控件的 `button.ol-zoom-out` 定位，不能把 `Zoom out` 提示文字当成默认按钮的可访问名称（实际为符号“–”）。本地中英文 T3 与 Hosted 复用同一交互帮助函数，连续两次真实点击后分别核对视图缩放级别减少 1 且动画结束，再检查要素像素；原源几何、地图实例及像素断言保持。

复用同一 Manager Hosted suite、GeoPython 产品 Runtime 和隔离 Business MinIO，三份固定哈希双图层源沿用原十项普通 User 权限。Owner/v13、browser/v13 验证图层与源几何、实际地图要素和像素、TopoJSON 未知 CRS 默认暂停、临时选择清除／切换／重开、普通刷新和原文件下载哈希，拒绝旧报告。隔离环境先启动 Security，再由标准身份夹具等待 Manager 持久保护投影首次同步；预检核对 Security 健康和源码身份，启动／初始化失败须拒绝进入场景并清理。标准 `make test-manager-online-runner` 覆盖夹具、拒绝边界、浏览器语法和 CI 登记。

此场景不增加自定义服务、用户权限或 workflow，不改变血缘产品实现；MinIO 只证明 S3 兼容协议，不认证外部云 S3。真实 T4 须按当次源码、报告和清理证据单独认定，此处不登记通过。

### 设备静态轨迹范围读取

复用 `manager-internal-artifact-lineage`、当次 Business MinIO 和已批准的十项普通 User 权限，不增加服务、任务、产物或权限。产品字段、预算及保护规则以[Manager 预览规范](../../../../manager/docs/数据预览与资源树实现规范.md#六设备静态轨迹预览)为准。

夹具包含 CSV、records JSON、Point GeoJSON 三份确定性 3000 点合成源，每种源含 A／B 两设备、各 1500 小时采样。UTC `[2026-01-01T00:00:00Z,2026-01-02T00:00:00Z)` 应返回两设备 48 点、单设备 A 24 点。真实 Console 表单必须证明：

1. 整文件模式拒绝绘制前 2000 点，保留完整性提示；GeoJSON 自动填入源 CRS 不能清除该提示，重新打开仍须重新校验。
2. 显式设备／时间范围请求与服务端 `trajectory_scope` 的 selection 回显一致，`complete=true`、`scanned_rows=3000`；逐行核对设备、时间、坐标及真实地图像素。
3. 同一普通 User 的超限 API 请求返回 422 与 `trajectory_point_limit_exceeded`，缩回合法范围后恢复；业务失败或警告不因该预期反例而被统一豁免。
4. 范围预览前后的原文件大小和下载 SHA-256 不变，正式下载 helper 不由受控探针替代。
5. 三源证据完整；完整 suite 的报告版本以当前 Owner、浏览器实现和 Online CI 登记为准，拒绝旧版本、缺项或篡改证据。历史 Owner/v9、browser/v11 仅用于识别当次通过报告。
6. 沿既有生命周期销毁本轮源，并保留原模型、PPTX、COG、点云等场景的领域删除和物理清理断言；Infra 销毁不代替领域零残留。

源生成的字节／大小／下载对象登记、两次生成一致、三格式等价、3000／48／24 点计数和报告拒绝归 `make test-manager-online-runner`／`make test-online-runner`。浏览器像素取证只读复制到独立离屏画布，不修改地图或过滤警告。真实运行沿用原手动 Hosted 入口；最终源码、报告与不覆盖范围见[静态轨迹历史证据](../验收证据索引.md#manager-静态轨迹历史证据)。


## manager-hybrid-search

Manager 混合检索类 T4 使用同一专用 Business MinIO Fixture 和永久 MinIO Engine Instance，Fixture owner 额外幂等写入仓库内确定性 JPG。专用 Tenant 必须通过正式 Inference 与 Manager 配置 API 预置支持文本和图片、维度为 2560 的 Model Profile 及 `semantic_search_embedding` 场景绑定；Provider Credential 只保存在 Inference 加密存储，Online 环境只声明预期 Model Profile ID。suite 不创建、更新或删除 Provider、Deployment、Model Profile、Credential 和场景绑定，而经 Gateway 触发真实 Meta scan 与 Manager ad-hoc embedding，验证 Inference、pgvector、Meilisearch 和 Manager 对称 RRF 融合的完整链路：同一 `document_id` 的关键词与向量候选只能返回一次，纯向量候选必须在融合后参与统一分页，响应不得包含旧 `vector_hits`。每轮 embedding 结果必须通过 Manager 正式 API 删除并确认零残留；新增 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得加入夜间调度。

## vector-pmtiles-workflow

矢量 PMTiles T4 `vector-pmtiles-workflow` 仅手工在 GitHub Hosted `ubuntu-24.04` x86_64 独占部署运行，复用共享 Hosted 生命周期、标准 GeoPython 产品镜像和独立 source／target tmpfs MinIO。必须先启动 Security，preflight 同提交核验 Security、Develop、Meta、Manager 和 Monitor；身份夹具通过统一 `--wait-protection-owners develop,manager` 在导出凭据前只读观察该租户两个 Owner 的持久首次同步状态，最多等待 60 秒；读取错误或超时立即停止并清理，不写入 checkpoint，不绕过保护门禁或重试业务执行。非默认租户普通 User 经公开 Develop API 新建两点 GeoJSON 的纯几何 PMTiles，再用 50 面 Shapefile 和显式 name／rank 字段替换同一目标；独立 GDAL oracle 核对 PMTiles v3、gzip MVT、层名、业务字段、数量及按瓦片量化误差约束的坐标。重复 create 必须返回受控 target_exists，且文件哈希／大小／ETag／修改时间、源对象、扫描记录、DataItem 详情及当前血缘完全不变。replace 必须保留目标 ID／指纹并更新文件内容、自动扫描和唯一当前 derive 来源，不把当前图当作历史 observation 证明。每次成功均核对物理大小与 DataItem、原生 header_hash 与 Meta 格式事实，并由同一 User 真实登录 Console，检查 min_zoom=0／max_zoom=5、no-store 请求与响应、瓦片内容哈希、页面解码要素数及地图绘制。失败或成功退出均清理当次应用、业务容器、产品 Runtime／镜像、registry、Infra 数据卷和凭据；清理失败使门禁失败。标准离线入口为 `make test-vector-pmtiles-online-runner`，接入 `test-online-runner`；真实 GDAL oracle 用例接入既有 `make test-geopython-workflow`，由现有 Platform CI 调用，suite／CI／身份／清理登记由 Online 注册检查覆盖。提交 `3eed180b927916d76f1385e63cf8b213d32149a2` 的 [Hosted T4 39037436667](https://github.com/pampa0629/addp/actions/runs/39037436667/job/117410000383) 已通过上述完整 create／冲突保护／replace 场景：两轮地图分别显示 2 点与 50 面，目标身份稳定，自动扫描和当前血缘换源。7 个服务预检同提交，Runtime 留存同次源码构建、实际镜像 ID 与默认入口核验记录。清理证据仅覆盖当次隔离部署及 Infra 零残留，不代表 Manager 受管结果 DELETE API；T4 瓦片留痕为 HTTP 200／no-store／内容哈希，未单独采集 S3 HTTP Range 请求日志，Range 边界另有 Manager T1 回归。不登记 schedule，不覆盖新增 NFS／外部云 S3 认证、历史血缘、容量或十类全算子认证。

## raster-workflow

### 部署与主场景

栅格工作流 T4 `raster-workflow` 只在 GitHub Hosted `ubuntu-24.04` x86_64 独占部署运行，复用 `scripts/utils/hosted-online.sh`，由根 `make build-images` 构建 GeoPython 产品镜像并以默认入口启动独立 `addp-raster-online-runtime`，不调用 GeoPython 开发启动。共享入口核验实际镜像 ID、默认 command 与提交并保留 Runtime 日志，所有退出路径按当次所有权清理容器和镜像。样例生成与成果物理校验通过 `ADDP_ONLINE_RASTER_RUNTIME_IMAGE` 使用入口本次构建并核验的同一个产品镜像；未传入镜像时在启动 Docker 前拒绝执行，不引用开发镜像。手工栅格场景的 preflight 与业务执行总预算为 1740 秒；该场景预算由栅格入口设置，不改变其他场景或夜间调度预算。Business Fixture 管理两套无持久卷、凭据不同的 MinIO，生成含 EPSG:4326、确定性双波段和 NoData 的 TIFF；System-owned IAM helper 创建非默认 Tenant 与普通 User，独立 Provisioner 经正式 API 注册两个业务 Engine。User 经 Develop 提交 `raster_load → raster_band_math → raster_save(profile=cog)`，必须验证 create、重复 create 失败和 replace，并独立读取成果像元、CRS、transform、NoData 与完整 COG 布局。Meta 的轻量 COG profile hint 与完整布局校验分别验证：物理夹具记录成果是否具有 overview，Meta 的 `is_tiled`、`has_overviews` 与 profile hint 须与该真实结构一致；小尺寸、无 overview 的合法 COG 可被轻量探测表达为 `geotiff`，不能据此拒绝已经通过完整布局校验的成果，也不能用 hint 替代完整校验或人为新增 overview 掩盖断言错误。场景还需重投影确定性源到 EPSG:3857 的 1 米网格，裁剪两个重叠区域，分别验证 first/last 镶嵌 COG 的全部像元、NoData、空间属性和原成果未改变；每个成果均验证自动扫描、派生血缘和同一普通 User 的浏览器展示。多波段组合验收需使用各数据波段缺值位置不同、含合法零值与部分 alpha 的确定性 TIFF，并分别覆盖 NaN 与有限 NoData；有限 NoData 的平均插值成果须逐波段保留有限声明与缺值，保存重读的波段计算成果须声明 NaN 并保留独立有效性，尺寸重采样后保存 COG，再经正式 ResourceLocator 重新读取并计算 `b1+b2`；独立物理夹具逐波段验证全部像元、NoData 声明、alpha 覆盖和重读后的联合有效性，同时核对先前全部成果及源对象哈希未改变。平均插值组合还需以非均匀源像元、波段独立缺值和透明干扰值验证有效贡献像元的面积均值，同时覆盖整倍数与非整数缩放比例；非整数网格须以源、目标像元矩形的面积交集独立验证边界贡献权重，不能使用等权均值；保存重读后的相加应复用各波段自己的平均结果，不能提前合并源有效性。双线性插值组合还需按各波段自己的有效中心与邻域权重验证，中心无效时不得用有效邻域填补；放大后 COG 的 overview 事实须与 Meta 轻量探测一致，保存重读后相加仍分别保留波段有效性。目标 DataItem 只来自 Develop 自动提交的 Meta scan，血缘只来自 owner 自动采集，禁止测试手工扫描目标或调用 collect 掩盖自动链路缺陷。Monitor API 与 Console 必须展示同一成功执行及源、目标血缘，浏览器登录身份与 API User 相同。凭据仅存 owner-only runner.temp，不进入业务进程环境或证据；全部退出路径销毁业务容器、临时 registry、平台容器和数据卷并检查零残留。首次真实通过前只登记手工 workflow_dispatch。 发布确认后清理故障复用该 suite：Business-owned 回环存储代理仅针对首片／末片两个明确对象，在真实 MinIO PUT 成功后、回复 Runtime 前，按上传长度与 SHA-256 唯一定位私有临时 COG，在 sidecar 路径创建目录触发真实 unlink 失败；原 COG 字节和大小保持不变。代理以 Runner 的 UID 和实际 Docker socket 所属 GID 运行，使 owner-only 故障事件的生产者与读取者身份一致；由已有 Hosted 生命周期独占创建和销毁，仅内部持有 Docker socket 与故障事件，不新增产品故障参数。部分确认时下一片为 not_attempted，全部确认时不返回 failed_tile/publication；两者父执行仍 failed，逐一核对确认产物、严格整数大小、新自动扫描、先前成果哈希和执行工作目录清理，未真实通过前不计为 T4。 min/max 聚合扩展复用已有选择第 2 数据波段、3×5 分组及 nearest 金字塔的单一 DAG，逐像元独立验证源 NoData 排除、内部和边缘不足整组的数值、CRS／完整仿射网格及金字塔；发布后的 artifact、物理对象、Meta DataItem 大小均为严格正整数且相等。新增两个成果及两个浏览器场景，原成功与失败场景完整保留，48 轮浏览器及业务总预算仍为 1740 秒；扩展真实通过前不计为 T4。

### 多来源计算与页面构图

多来源栅格验收继续使用同一 suite：分别加载源 Engine 的 EPSG:4326 TIFF 和目标 Engine 已自动扫描的 EPSG:3857 COG，显式参考网格对齐、选波段及组合后，验证跨文件加权表达式和 RGB 通道合成。Develop 与 Monitor 输入事实必须绑定完整的两个 ResourceLocator，Meta 必须存在同一执行从两个源 DataItem 到目标的 derive 边；Console 按完整输入集合验证卡片及链接，并分别绑定来源证据。物理夹具以独立像元公式验证所有波段、NaN、mask、Red/Green/Blue 解释、网格、CRS 和 nearest 金字塔，核对源及所有已接受成果哈希、精确对象集合；新成果必须保持 `size_bytes == physical stat size == Meta DataItem size > 0`。两个新执行增加四份浏览器来源证据，总数为 29；总预算扩展不改变 Runtime 执行、物理校验和单次浏览器的超时，也不放宽大小、像元或血缘断言。RGB 通道生成不代表显示拉伸、自动最佳波段推荐或 Manager RGB 渲染已经验收。

多选构图的真实浏览器贯通继续复用同一 Raster Hosted T4：保留全部原 API 场景，新增一条普通 User 经 Console／Develop 页面多选三个独立登记 TIFF、配置镶嵌及 COG 保存、首次保存、重载后运行的场景。仅 `raster-workflow` 身份夹具增加 `develop.task.create` 和 `develop.task.delete`，容量 suite 不继承这两项权限；不增加更新、引擎管理或管理员权限，不改变产品权限规则。真实页面负责创建和执行，测试不得通过 API 或图对象注入代替构图；读取真实保存定义和执行回执，核对运行时、来源顺序、公开参数、目标及父执行绑定。成功及可确认归属的失败路径删除本轮任务并确认 404，未知创建／执行结果不得自动重发。新成果沿既有独立全像元／mask／金字塔／COG、严格三方正整数大小、自动扫描、逐源血缘、同一 User 浏览器及零残留断言验收，原 1740 秒预算保持不变；首次真实通过前不计为用户操作链路已完成。

### 重分类与整数分组聚合

重分类沿用原 suite，在多来源计算后追加 `reclassify-nodata` 和 `reclassify-keep`：经正式源 ResourceLocator 加载确定性双波段 TIFF，选择第二数据波段，分别验证省略 `unmatched` 时的默认 NoData 和显式保留原值。规则必须涵盖精确值、合法零类别、左闭右开区间、未命中间隙和无界端点。独立物理公式验证完整输出像元、NaN、mask 与 nearest overview，且源 NoData 不能因 `keep` 恢复有效；仍核对原全部成果的物理字节和对象集合。两个成果继续采用严格三方大小相等，自动 Meta scan、资源级 derive、Monitor 和同一普通 User 的 Console 全部验证。增加两份浏览器证据，总数为 31，最终保留 22 份 COG；不调整现有 1740 秒 suite 预算或单请求超时，不放宽原 29 份场景的断言。

整数分组聚合继续追加到原 suite 的重分类场景之后：`aggregate-mean` 和 `aggregate-sum` 选择源第二波段，以 `factors=[3,5]` 生成 86×52 单波段 COG，并显式构建 43×26 nearest 金字塔。独立公式验证全部像元、源 NoData 的有效计数，以及右侧、底侧和右下角不足整块的分组；均值仅允许 Float64 运算所需的局部容差，旧场景和求和仍保持原精确断言。继续逐次核对先前 22/23 份成果哈希、全部源字节、精确对象集合、自动 Meta scan、资源级 derive、Monitor 和同一普通 User 的 Console；严格保持三方正大小相等。浏览器总数增至 33，最终 24 份 COG，1740 秒总预算和各单项超时不变。

### 外部裁剪、轮廓与局部更新

区域外裁剪在聚合之后追加 `clip-outside`：正式源 ResourceLocator 加载原双数据波段 TIFF，`raster_clip(mode=outside)` 使用带孔 Polygon，保留完整 256×256 源网格并直接保存双数据波段及 alpha，不通过选波段省略 alpha 验收。独立行列公式检查外围、孔洞、区域内 NaN、源 NoData 不恢复、255/0 alpha、逐波段 mask、颜色解释及 128×128 nearest 金字塔。每个数据波段有效像元为 53247；先前 24 份成果 SHA、全部源字节和精确对象集合继续核对。严格保持 `size_bytes == physical stat size == Meta DataItem size > 0`，沿用自动扫描、资源级 derive、Monitor 和同一普通 User 的 Console。浏览器总数为 34，最终 25 份 COG；原预算、单项超时和旧场景断言不变。新增测试由既有 `make test-raster-online-runner`、`make test-geopython-workflow`、Platform CI 及手工 `online-t4-gates.yml` 自动覆盖，不新增入口、依赖或 suite。

有效像元轮廓在外裁剪之后追加 `footprint-any` 和 `footprint-all` 只读执行：正式加载双数据波段及 alpha 的确定性 TIFF，省略 validity 验证默认并集，显式 all 验证交集。独立整数像元公式核对完整面覆盖、边界网格、面积和波段独立缺值，不依赖 Polygonize 或生产算子生成期望，不限定合法环的起点和方向。Develop/Monitor/Console 展示同一普通 User 的 GeoJSON 预览，不能生成持久输出、derive 或目标扫描；物理夹具继续保护源和先前 25 份 COG，原严格三方大小规则不变。浏览器为 36 份，COG 仍 25 份；预算、超时、原场景门禁及 CI 登记路径不变。该扩展已在提交 `89e77da9a1bc72ec6c7e1791f2172edbbfdefb28` 的 [Hosted T4 37741228176](https://github.com/pampa0629/addp/actions/runs/37741228176) 通过并独立核对归档；并/交集覆盖分别为 65532/65524 像元，36 份浏览器、25 份 COG 及零残留清理通过。该结果不将同提交 Platform CI 中已取消的 GeoPython 全量 Job 计为通过。

局部更新追加到原 suite 的外裁剪之后：正式加载基底与双数据波段及 alpha 的更新源，裁剪更新源局部范围后显式对齐基底，执行 `raster_update` 并保存带 nearest 金字塔的双波段 COG。独立行列公式验证更新覆盖、区域外和无效更新像元保留基底、合法零、部分 alpha、逐数据波段有效性与源不可变；严格三方大小、原前置成果 SHA 和自动扫描/资源级 derive 保持。两个输入分别绑定来源浏览器证据；浏览器为 38 份、COG 为 26 份，后续只读轮廓查询继续保护全部 26 份成果。入口、预算、单项超时和自动登记路径不变。该扩展已在提交 `53591eba7e749c84849fe2e1ebff4a8ee7a525e9` 的 [Hosted T4 37795049439](https://github.com/pampa0629/addp/actions/runs/37795049439) 通过并独立审计；更新成果 182565 字节、两波段有效数 65535/65536，38 份浏览器、26 份 COG、双来源 derive 及零残留清理通过。同提交 [Platform CI 37794921834](https://github.com/pampa0629/addp/actions/runs/37794921834) 实际执行标准 GeoPython 全量 697 项通过；同提交另一取消运行不计通过。


## raster-contour-persistence

独立手工 Hosted Linux x86_64 T4，专门验证 `raster_load → raster_contour → save` 的 NFS 单文件 GeoPackage 持久化。复用共享 Hosted 生命周期、产品 GeoPython 镜像默认入口、现有普通栅格 User 的原十二项权限与正式引擎登记；不扩展产品权限或通用保存能力，不重复已通过的原 `raster-workflow`，其 1740 秒预算保持不变。标准分发为 `make test-online ONLINE_SUITE=raster-contour-persistence`，独占部署入口为 `bash scripts/test/online-hosted-raster-contour-gate.sh`。业务阶段预算为 600 秒，部署不计入该预算。

本轮独占 NFSv3 export 仅供回环访问，使用 `root_squash`，Runtime 以同路径 bind mount 消费已有 mounted-path 契约，Meta 通过真实 NFS 协议读取。独立生成 EPSG:3857、64×48、2×3 米像元的 Float64 倾斜平面 DEM。指定高程 200／350／500 米与固定间隔 100 米分别写出不同 `.gpkg`；复用现有 `save` 的 `mode=replace`，每次使用本轮尚不存在的唯一目标，不将此描述为 create 冲突或覆盖替换验收。

逐点独立平面公式验证每条严格二维 LineString、完整高程集合、Float64 elevation 及源投影；核对单图层 GeoPackage、精确文件集合和无 sidecar／工作目录残留。物理文件与 Meta 大小必须为相等的严格正整数；通用矢量 `save` 返回 GeoDataFrame，不伪造 raster artifact 大小字段。仅对源做一次手工深扫，两个目标必须来自 Develop 自动提交的精确 file 扫描；普通发起用户可读取其扫描执行，公开 `tenant_id` 和 `triggered_by` 必须为准确的租户及发起用户整数；不读取安全响应禁止返回的内部主体字段。GeoPackage 文件按 `single/container/geopackage` 登记，图层只按现有 `type_info.container.children` 表达，不向外层复制图层空间能力、不另造图层资源身份。Monitor 的成功状态、owner 与资源血缘需一致，Meta 必须自动形成同一 Develop execution 的源文件至目标文件 derive 边；不手工扫描目标或手工采集血缘。

第二次保存后重新核对第一份文件 SHA-256／大小以及源 DEM SHA-256／大小，源 Meta 记录保持不变。所有退出路径移除本轮 NFS export、正向确认归属的临时目录、Runtime／registry／产品镜像和当次 Infra，零残留与失败清理分别验收。此清理不等于 Manager 业务 DELETE，API 链路也不代表 Manager 绘制、HTTP Range 或真实页面操作验收。保持手工入口，明确区分代码落地、Native 物理验证和真实 T4 结果。该套件已在 `54acffb1c` 的 [Hosted 39097413643](https://github.com/pampa0629/addp/actions/runs/39097413643/job/117586617847) 完整通过并独立审计；指定高程与固定间隔两份成果、严格大小、两个自动扫描、当前执行血缘及清理均通过，预检与业务为 103001 毫秒。完整版本及相邻门禁见[栅格当前验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)。


## raster-polygonize-persistence

独立手工 Hosted Linux x86_64 T4，标准分发为 `make test-online ONLINE_SUITE=raster-polygonize-persistence`，隔离部署为 `bash scripts/test/online-hosted-raster-polygonize-gate.sh`。复用既有 Hosted 生命周期、产品 Runtime 默认入口、共享 NFS 归属检查和原普通 User 十二项权限；不增加产品接口或权限。业务与预检合计预算为 600 秒；原 Raster T4、1740 秒预算和等值线两场景不变。

夹具为 EPSG:3857、8×6、带旋转仿射的整数分类 TIFF，包含负数、合法零、孔洞、对角相连区域、NoData、共享 mask、全透明和部分透明像元。三条正式 DAG 分别执行四邻接转面、八邻接转面和重分类后四邻接转面，复用现有 `save(mode=replace)` 写入本轮三个尚不存在的唯一 GeoPackage。独立标量 flood-fill 和逐像元多边形并集核对完整连通区域、分类代码、精确像元覆盖、合法二维 MultiPolygon、Int64 `class_value`、孔洞及源 CRS。Oracle 不调用产品转面、重分类或 GDAL Polygonize 计算期望值，并以真实文件篡改回归证明拒绝错误分类、几何、维度、区域丢失和额外 sidecar。

仅源文件手工扫描，三个目标必须由当前 Develop 执行自动扫描；校验原普通用户公开扫描归属、GeoPackage 单文件容器及内部图层、物理与 Meta 大小严格为相等正整数、Monitor 与当前执行 active derive 血缘。保存 API 校验和 Runtime worker 归属检查分别复用共享唯一实现。每轮保存重核先前成果 SHA／大小和源字节，源 Meta 记录不变。所有退出路径清理本轮 NFS export、已确认归属的目录、Runtime／registry／镜像和 Infra。Runtime 同路径 bind 写入与 Meta 真实 NFS 读取分别记录；不据此宣称 NFS 客户端写、Manager 绘制／业务 DELETE、HTTP Range、覆盖冲突或容量验收通过。代码、Native 和真实 Hosted 结果分开记账；本段登记不表示 T4 已通过。

## vector-rasterize-persistence

独立手工 Hosted 入口验证已登记 GeoJSON 二维面与同 CRS 参考 TIFF：属性／中心覆盖、常量／所有触及像元、分类转面再栅格化三条 COG 保存 DAG，以及非法字段导致失败后的无发布、无新增扫描与清理。输入为同一普通 User 的原 12 项权限；复用独占 Hosted 生命周期和共享 NFS，Runtime 同路径挂载写入，Meta 经 NFS 读取，不接管个人服务。

独立几何 oracle 核对每个像元、mask、NaN NoData、Float64、旋转网格与 COG 布局；artifact／物理文件／Meta 大小必须为相等正整数。只手工扫描两份精确源，输出只能由各父执行自动扫描；逐一验证同用户／租户、Monitor 和当前执行所有输入的 active derive 血缘。源与先前成果不可变；非法参数后精确输出清单、扫描清单和 Runtime 工作目录不变。退出核对 NFS export、归属目录、部署和 Infra 零残留。业务预检与执行共用独立 600 秒预算；不改变原 Raster T4、Contour、分类转面套件、产品权限和跨模块产品逻辑。版本 `764bf53a9` 的[独立 Hosted 39207785953](https://github.com/pampa0629/addp/actions/runs/39207785953/job/117914293518) 已通过并经归档复核：三份 COG 严格大小为 824／819／847 字节，预检与业务 176408 毫秒，清理与 Infra 零残留通过；不据此计入 Manager 绘制、NFS 客户端写、HTTP Range、业务 DELETE、覆盖冲突或容量。标准入口 `make test-online ONLINE_SUITE=vector-rasterize-persistence`；Native oracle 沿现有 GeoPython 门禁、脚本回归沿 `make test-raster-online-runner`，Hosted workflow 仅编排上述入口。

## raster-zonal-statistics-persistence

独立手工 Hosted 入口使用普通用户原 12 权限、同源七个 Backend 与默认入口 GeoPython Runtime、共享 NFS 和独立 600 秒预检／业务预算。验证已登记且同 CRS／旋转网格的数值与分区 TIFF：显式多波段分类分区、重分类接统计、超过 50 类的完整 CSV 保存与有限预览、全无效分区空表四条 DAG；非法波段和不同网格两条失败不得发布目标或新增扫描。固定版本 `5e49199e8` 的[独立 Hosted 39217208101](https://github.com/pampa0629/addp/actions/runs/39217208101/job/117943758382) 已通过并经独立归档复核：四份 CSV 为 4／3／70／0 行，严格物理／Meta 大小为 253／206／1900／73 字节；预检与业务 210767 毫秒，清理与 Infra 零残留通过。Native 与真实跨模块结论分别记账。

独立标量分组与标准库统计 oracle 逐行核对分类值、三项计数、五项统计、null、排序和固定 Schema，完整结果必须经既有 `save` 持久化；响应仅为有限表摘要，不新增 RasterArtifact。物理文件／Meta 大小严格为相等正整数；只手工扫描两份精确源，四份 CSV 各自自动扫描，核对同租户／同用户、Monitor 和当前执行所有输入的 active derive 血缘。每轮核对源物理字节／Meta 和先前成果不变、精确输出与扫描清单及执行工作目录清理；退出核对归属 NFS export／目录、部署及 Infra 零残留。Runtime bind 写入与 Meta 真实 NFS 读取分开记录，不计为 Manager 绘制、NFS 客户端写、HTTP Range、业务 DELETE、覆盖冲突或容量；原 Raster／Contour／Polygonize／Rasterize T4 保持。

标准入口 `make test-online ONLINE_SUITE=raster-zonal-statistics-persistence`；真实 GDAL／CSV 与篡改拒绝沿 `make test-geopython-workflow`，夹具、场景断言和生命周期沿 `make test-raster-zonal-statistics-online-runner`，并由原 `make test-raster-online-runner` 聚合；workflow 仅编排同一根入口。


## raster-focal-statistics-persistence

独立手工 Hosted 使用普通用户原 12 权限、同源七个 Backend 与默认入口 GeoPython Runtime、共享 NFS 和独立 600 秒预检／业务预算。固定版本 `fe0d7cc8e` 的[独立 Hosted 39228689716](https://github.com/pampa0629/addp/actions/runs/39228689716/job/117981136802) 已通过并经归档独立审计：六份 COG 严格三方大小为 68844／59439／68325／58704／58995／10767 字节，每份 267227 个有效像元；预检与业务 359770 毫秒，源及先前成果不变，扫描／血缘与全部清理通过。同源 Linux 全量 1251 项、21 warnings（1240.70 秒）及 Platform 检查均通过，确定性 CI 与真实 T4 分别记账。输入为已登记的 EPSG:4326、519×515、多波段及旋转仿射 TIFF；显式选择原始数值波段，源包含合法零、负值、NoData、mask、NaN／正负 Inf、透明及部分透明 alpha，块边界布置无效值，并包含超过窗口的无效孔洞。验证 3×3 sum/min、5×5 mean/max/stddev、重分类接 3×3 stddev 六条 COG 保存链；非法波段与窗口不得发布目标或新增扫描。

独立完整窗口 NumPy oracle 不调用产品统计函数，以实际有效邻域核对所有输出像元和 mask；无效中心保持 NoData、边缘不补零，源 raw 值与 scale/offset 分开处理。核对 Float64／NaN NoData、完整旋转网格、COG 布局和金字塔存在；artifact／物理文件／Meta 大小严格为相等正整数。仅手工扫描精确源，每份成果各自自动扫描，同用户／租户、Monitor 和当前执行源输入的 active derive 血缘逐项核对；源物理字节／Meta 与先前成果不变、输出及扫描清单精确、执行工作目录清理。退出核对归属 NFS export／目录、部署及 Infra 零残留。Runtime 同路径 bind 写与 Meta 真实 NFS 读取分别记录；不认证 Manager 绘制、NFS 客户端写、HTTP Range、业务 DELETE、覆盖冲突、所有窗口组合的跨模块覆盖或容量，不改原五套栅格相关 T4、权限或跨模块产品逻辑。

标准入口 `make test-online ONLINE_SUITE=raster-focal-statistics-persistence`；真实 GDAL／COG 与篡改拒绝沿 `make test-geopython-workflow`，夹具、场景、登记正反例和生命周期沿 `make test-raster-focal-statistics-online-runner`，由原 `make test-raster-online-runner` 聚合。Hosted workflow 只编排上述根入口，不登记夜间计划。

## vector-zonal-statistics-persistence

独立手工 Hosted 使用普通用户原 12 权限、共享 NFS、同源七个 Backend 与默认产品入口 GeoPython Runtime，预检／业务预算独立 600 秒。三条通用 `save` GeoPackage DAG 分别验证已登记 GeoJSON 面与旋转多波段 TIFF 的中心覆盖、全部触及与自定义统计列前缀，以及 TIFF 重分类→分类转面→矢量分区统计；前两条记录两源，第三条仅记录实际 TIFF 输入。样本覆盖跨块、重叠、孔洞、多部件、合法零、负值、NoData／mask／透明／非有限值、部分透明、网格外、全无效数值及薄面。保留原始属性与行序，重叠面独立统计；空覆盖和无有效值保留行、统计为 null。

物理 oracle 独立以像元平面几何运算、标量四邻接遍历与标准库统计核对完整几何、属性、计数／五项统计／null、CRS 和字段类型，不调用产品统计／GDAL Rasterize／Polygonize。通用矢量保存不新增 RasterArtifact；物理／Meta 大小必须为相等正整数。只手工扫描两份精确源，三份成果各自精确自动扫描，核对同租户／同用户、Monitor 和当前执行全部输入的 active derive 血缘。非法波段／列名冲突不得发布文件或新增扫描；每轮检查源物理／Meta 与先前成果不变、精确输出清单和工作区清理。退出核对归属 NFS export／目录、Runtime／镜像／registry、部署与 Infra 零残留。Runtime bind 写入与 Meta NFS 读取分别记账；不认证 Manager 绘制、NFS 客户端写、业务 DELETE、覆盖冲突或容量，不改已有套件、权限或跨模块产品逻辑。

标准入口 `make test-online ONLINE_SUITE=vector-zonal-statistics-persistence`；脚本／生命周期／登记正反例沿 `make test-vector-zonal-statistics-online-runner`，由 `make test-raster-online-runner` 聚合；真实 GDAL 与篡改拒绝由现有 `make test-geopython-workflow` 自动发现。仅手工触发，当前尚未执行真实 Hosted，不能记作 T4 通过。
