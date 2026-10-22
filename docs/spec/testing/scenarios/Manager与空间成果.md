# ADDP Online 场景：Manager与空间成果

[返回测试与验收总入口](../../addp测试与验收规范.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

本页场景：[manager-internal-artifact-lineage](#manager-internal-artifact-lineage)、[manager-hybrid-search](#manager-hybrid-search)、[vector-pmtiles-workflow](#vector-pmtiles-workflow)、[raster-workflow](#raster-workflow)、[raster-contour-persistence](#raster-contour-persistence)、[raster-polygonize-persistence](#raster-polygonize-persistence)、[raster-boundary-clean-persistence](#raster-boundary-clean-persistence)、[raster-thin-persistence](#raster-thin-persistence)。

## manager-internal-artifact-lineage

### IMG 原生扫描与 COG 快显

同一 Manager Hosted suite 增加 IMG 子场景，使用既有普通 User、十项权限、Business MinIO、GeoPython 产品 Runtime 与同一部署生命周期，不新增 suite、服务或权限。Runtime 中生成三份自有 256×256 三波段 Byte HFA：WGS84、UTM50N 和无定位影像；全部自包含、同数据类型且无 NoData。真实 Meta 深度扫描须证明 `img/media/single`、HFA 编码、尺寸、波段及明确的源 CRS/范围；无定位样例不得出现推断的空间能力或 COG 生成动作。

两份有定位 IMG 经正式 `generate_raster_cog` action 生成受管 TIFF/COG，核任务 `source_format=img`、原生 Runtime 身份、完整像元/mask/CRS/仿射网格、完整 COG 布局、API/物理哈希和大小、Range 前后缀及越界拒绝。同一普通 User 在真实 Console iframe 显示 WGS84/UTM COG，检查 WebGL 图层的品红/青/黄像素及截图，重开复用同一结果且不产生额外生成请求；原文件下载哈希保持一致。无定位样例只验元数据与不开放地图的边界。生成、页面、复用及退出均核源原字节不变；通过正式 Manager API 删除结果和任务，核内容404、物理对象缺失、源预览模式恢复与零残留。失败仍须尝试本轮已知精确资源清理，保留首个业务错误和清理错误。

IMG 子报告单独使用 `addp.manager-img/v1` 与 `addp.manager-img-browser/v1`，旧 Manager 场景断言保持。标准确定性入口仍为 `make test-manager-online-runner`，原生物理夹具由 `make test-geopython-workflow` 自动发现，CI 注册检查须确认新子场景、浏览器和两组回归在已有入口中实际接线。仅手工触发既有 Manager Hosted，不增加定时；真实结果出来前不记通过。该证据不外推 IMG 导出、外部 IGE/RRD/依赖文件、复数/异质波段、自定义 WKT、NFS Hosted 或任意影像容量。

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
- 原文件与清理：FBX 在生成和缓存两阶段下载单主文件核对 SHA-256；OBJ 按既有逻辑对象下载契约接收 ZIP，严格核对且仅含 model.obj、model.mtl、texture.png，逐成员核大小、原始字节 SHA-256、重复／额外成员、加密及损坏拒绝。浏览器捕获原 ZIP 并记录包大小／SHA-256 和成员事实，Owner 用同一字节校验器复核；两阶段均须通过，清理后源成员不变。OBJ 主文件 97、MTL 45、PNG 76 字节合计 218 字节；DataItem 整体大小与主文件下载大小分别核对。十一类产物继续验证 Manager／Monitor 血缘、正式结果／任务删除和后续内容／任务 404，PDF／COG 独立物理核验不由 Infra 销毁代替；表格／JSON 源不创建派生产物。

OBJ 逻辑下载修正将 Owner／浏览器报告统一升至 v15，拒绝 v14；使用 Python 标准 ZIP 读取器作为 API、浏览器捕获和退出核对的唯一字节校验器，不新增包或产品接口。

固定源、报告缺失／变更拒绝、场景与生命周期沿用 `make test-manager-online-runner`、`make test-online-runner` 和既有 Hosted Job。实际历史源码与通过边界见[证据索引](../验收证据索引.md#manager-预览与私有导入历史证据)。

## 拓扑构面、面叠面缝、悬点与线叠持久化（vector-topology-persistence）

独立手工 Hosted Linux x86_64 T4，标准入口 `make test-online ONLINE_SUITE=vector-topology-persistence`，复用共享 Hosted 生命周期、NFS export 所有权及正式 GeoPython 产品默认入口。同一成功 execution 的七条分支分别执行线网构面双保存、单要素检查→修复→复查保存、严格参考吸附保存、单图层面叠检查保存、显式覆盖范围的双输入面缝检查保存、关联来源的悬点检查保存及跨要素线叠检查保存。只使用当次 disposable 非默认 Tenant 和同一普通 User，精确 11 项权限：`develop.task.read`、`develop.task.execute`、`develop.data_read.execute`、`develop.data_write.execute`、`system.execution_authorization.create`、`meta.catalog.read`、`meta.scan_task.read`、`meta.scan_task.execute`、`meta.lineage.read`、`monitor.execution.read`、`manager.data_item.read`。Engine 登记身份独立且登记后撤去基础设施 Token；不得扩大业务权限、600 秒预算或复用个人会话。

九份固定源为 `source.geojson`、`invalid.geojson`、`snap-input.gpkg`、`snap-reference.gpkg`、`overlap-input.gpkg`、`gap-input.gpkg`、`gap-coverage.gpkg`、`dangling-input.gpkg`、`line-overlap-input.gpkg`；成功成果严格为 `faces.geojson`、`diagnostics.geojson`、`repaired.geojson`、`snapped.gpkg`、`overlaps.gpkg`、`gaps.gpkg`、`dangling.gpkg`、`line-overlaps.gpkg`。血缘输入按 `input.<读取节点ID>` 与源 locator 精确一一绑定，拒绝缺项、重复、额外输入及端口错绑；数组顺序不作为资源身份，不能要求与源文件清单顺序一致。所有保存均使用 `mode=fail`，八份稳定 outputs 和 lineage_facts.outputs 必须为 `write_mode=create`。八成果分别只有一次精确自动扫描，公开 tenant_id/triggered_by 必须为严格正整数且匹配当前 User/Tenant，拒绝私有字段替代、手工补扫、缺失事实或重复扫描。八份物理与 Meta 大小必须是相等的严格正整数；GPKG 核对文件与唯一子图层的边界。线源→faces/diagnostics、invalid→repaired、两个吸附源→snapped、面源→overlaps、待查面与覆盖范围分别→gaps，悬点线源→dangling，线叠线源→line_overlaps，共十条当前 execution 的 active derive，Monitor 必须一致。

独立 GDAL oracle 按显式预定义坐标验证四面四 dangle；自交面修复后保留两行各含两三角面的 MultiPolygon、属性及修复前 false/原因与修复后 true/Valid Geometry。GeoJSON 成果保留文件原 CRS84 声明，Meta 必须为 OGC:CRS84、无虚构 EPSG SRID；地图按原经纬坐标验证 direct_renderable/not_transformed，不临时选 CRS。

严格参考吸附的两份单层 GPKG 源为 EPSG:3857、米单位，五条输入线与六条固定参考线，tolerance=.75 核对原始位置上限、严格边界、等距 XY、不插点和不吸向线段内部，源/属性/行序/XY/CRS 不变。另一个 tolerance=1.25 的失败 DAG 必须以固定 `snapping produced invalid geometries` Owner 原因整批拒绝，失败 execution/Monitor 同 User 且无保存、facts、扫描或源变化；只允许有界观察当前 execution 的隔离 Owner 日志，不重试业务。浏览器之前先保存 `topology-snap-failure.json` 的最小公开观察、原因、Monitor 和未变化扫描清单，result=incomplete，不能替代最终成功。

面叠源是十二条有效 EPSG:3857 面，显式 parcel_id 唯一非空。真实 `load → check_polygon_overlap(id_field=parcel_id) → save` 必须输出七对：五对普通重叠/包含/完全重复、一对含两个面部分并排除接触线的多部件交集、一对面积 2^-40（约 9.09e-13）且二进制精确可表示的微小重叠。独立静态坐标和面积 oracle 核对双方标识、行序、每对唯一、Float64 面积和 MultiPolygon；共边、共点、孔洞内部不输出，不调用生产算法生成期望。物理层以 OGR 独立验证原几何/3857/七对及精确面积，预览层验证标准 EWKB、精确源坐标/字段/3857；GPKG 自动生成的存储 FID 按 OGR 实际列名核对，不冒充来源业务标识，不能将缺行、重复、错误来源或忽略小面积视为合格。不扩展 Quality 或自动修复。

面缝两份单层 EPSG:3857 GPKG 源分别为七条待检查面和七条显式覆盖范围面。真实 `load×2 → check_polygon_gap(input_gdf, coverage_gdf) → save` 输出八个独立 Polygon，按稳定位置排序：内部和边缘缺口各一、含两个孔洞的缺口一、待查面孔洞一、点接触和独立多部件三、面积精确为 2^-40 的微小缺口一。范围孔洞是合法留白，完全覆盖的区域、范围外数据不报告；范围重叠及源重复不重复报缺口。独立静态 oracle 逐原始坐标、孔洞、顺序、Float64 gap_area 和 3857 核验，不调用生产融合／差集生成期望，不舍入、不加容差、不自动填补。两个源各有本次 execution 的 active derive 到 gaps，不接受只连一个源或把两源互换。八成果和九源前后哈希／大小及 Meta 不变。

悬点源为十二条有效 EPSG:3857 线／多线，显式 route_key 是唯一非空字符串，另有 note 属性。固定场景包含孤立线、连续链、T 形、闭环、反向重复线、内部重合段、多部件和间距精确 2^-40 的微小断口。真实 load→check_dangling_points(id_field=route_key)→save 输出十七行 Point＋source_id；闭环和 T 形连接处不报，重复线同点分别关联两个原始 ID，多部件同源同点只报一次，结果 source_id 与 GPKG 自动 FID 分开验证。独立静态原坐标／来源／行序 oracle 使用 OGR GetPoints/GetX/GetY 核验，不调用生产打断或计度算法生成期望，不舍入、不加容差。悬点源到成果有唯一当前 execution active derive，成果仅一次同主体自动扫描且严格大小匹配 Meta；真实预览标准 EWKB、八幅地图中的 Point 要素、实际显示坐标及像素必须通过。排序静态回归仅导入 geometry-preview-coordinate.js 中唯一的纯数学投影 oracle，不加载含 OpenLayers 解码器的 geometry-preview-support.js，不要求安装前端 node_modules；真实地图与解码验收仍使用相应前端依赖。地图 Point 按完整 source_id＋显示坐标在同一 Node 环境用确定性身份顺序比对，不以浏览器语言排序推断记录匹配，不去重，保留重复线的两个来源；物理和 API 的原始行序仍须精确核对。点图统计共享蓝色 Point 样式的非透明前景像素（不计底图水域），其余七幅线面图的绿色像素断言保持；原始图分别归档 point_pixels／green_pixels，不改产品样式。九源八成果的前后原字节与源 Meta 不变；不扩展吸附、修复或 Quality 结论。

同一 User 经 Console 检查成功执行、八幅真实 Manager 地图与塌缩失败页。每次导航读取当前页面自身响应，再通过 browserAPIFromResponse 核同一 User/Tenant/11 权限；不跨导航持旧 Token，不 mock 响应或注入内部图对象。GPKG 预览使用容器唯一 child_name、标准 EWKB，真实 OpenLayers 要素和 EPSG:3857 投影像素同时核验。物理及 API 坐标与源预期精确相等；显示值按共享的独立球面墨卡托数学 oracle 计算 EPSG:3857→4326→3857 后逐值精确比较，不用舍入或容差。面叠／面缝的小面积几何仍须保留，要素数和完整坐标必须正确，像素由实际可见重叠或缺口区域核验。

线叠源为十六条有效 EPSG:3857 线／多线，使用显式字符串 route_key，保留 note 原属性。固定九对结果覆盖部分重合、包含、反向重复及三要素逐对、多部件并排除点接触、精确长度 2^-40、源内折返去重和不同采样顶点；孤立自重合、点接触、交叉和相距 2^-40 的平行线不报告。真实 load→check_line_overlap(id_field=route_key)→save 输出九行 MultiLineString＋source_id_a/source_id_b/overlap_length；长度为原坐标单位，双方来源和顺序独立核对，GPKG FID 不能替代业务 ID。独立手写坐标与长度 oracle 使用原生 OGR GetPoints，逐部件核坐标并以独立线段长度验证，不调用生产相交算法生成期望，不舍入或加容差。原始预览 EWKB、双 ID 和长度、真实 OL MultiLineString 坐标及身份、绿色前景像素和截图必须全部通过。微小线段的原始坐标/长度必须保留，但不宣称其亚像素投影必然可单独目视。线叠源到成果只有一条当前执行 active derive，一次同主体自动扫描；源字节/Meta 和成果末轮均不变。此扩展只检查、不修复，不增加权限或场景预算。

唯一报告版本为 addp.vector-topology-persistence/v7、addp.topology-browser/v7、addp.topology-checkpoint/v7，旧 v6 不接受。等待血缘前归档 topology-before-lineage.json，保留八份严格大小/公开扫描身份/物理哈希/稳定outputs与facts及原始 Monitor，标记 incomplete。topology-source-lineage-raw.json/v6 保留九源原 Meta、八原图和实际预览响应；topology-physical/ 保留全部九源八成果合成原文件，逐个核 SHA/大小并拒绝覆盖不同证据，支持下载后独立 GDAL 复核。浏览器前后源及成果均不变；最终 Owner/browser 完整成功、八图及退出清理均实际通过才认本轮 T4。

NFS export/owned root、Runtime/registry、部署与 Infra 全部清理并核零残留；失败清理仍使门禁失败。归档合成字节不是业务目录残留；部署销毁不认证 Manager 业务 DELETE。旧构面/修复/吸附/面叠/面缝/悬点 T4 不外推线叠，代码与确定性回归不记为新真实 T4，不新增 schedule。

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

标准入口 `make test-online ONLINE_SUITE=vector-zonal-statistics-persistence`；脚本／生命周期／登记正反例沿 `make test-vector-zonal-statistics-online-runner`，由 `make test-raster-online-runner` 聚合；真实 GDAL 与篡改拒绝由现有 `make test-geopython-workflow` 自动发现。仅手工触发，当前真实 Hosted 尚未通过，不能记作 T4 通过。


## raster-proximity-persistence

独立手工 Hosted 复用普通用户原 12 权限、NFS、同源七个 Backend 和默认入口产品 Runtime，独立 600 秒预检／业务预算。已登记投影米制 Int32 多波段及美制测量英尺 UInt32 TIFF 分别执行 `raster_load → raster_proximity → raster_save`；显式波段、合法零／负分类／UInt32 最大分类值、跨块、无效目标、部分透明和无效区域可跨越由完整像元核验。源均为北向无旋转方形网格；输出保留源网格及 CRS，距离统一为米制 Float64／NaN，沿 COG 保存。

独立 oracle 逐个有效目标中心穷举欧氏距离，不调用产品变换、下包络或 GDAL 距离算法。检查完整像元／mask、单位、网格／CRS、COG／金字塔与严格相等正整数的 artifact／物理／Meta 大小；精确两源手工扫描、两个当前执行自动扫描、同用户／租户、Monitor 以及各 DAG 仅自身实际源输入的 active derive 血缘。每次重核源物理／Meta 与先前成果不变；非法波段及无有效目标失败不得发布或新增扫描。退出核验工作目录、归属 NFS export／目录、产品 Runtime／registry／镜像、部署及 Infra 零残留。

标准入口 `make test-online ONLINE_SUITE=raster-proximity-persistence`；真实 GDAL／COG 和错误像元／mask／单位／网格等篡改拒绝沿 `make test-geopython-workflow`，脚本／登记／隔离生命周期沿 `make test-raster-proximity-online-runner`，由原 `make test-raster-online-runner` 聚合。仅手工触发，不改原套件、权限或跨模块产品逻辑；不认证测地距离、成本／路径、旋转／非方形网格、容量或 Manager 绘制。当前尚未执行真实 Hosted，不能记作 T4 通过。

## raster-thin-persistence

独立手工 Hosted Linux x86_64，入口 `make test-online ONLINE_SUITE=raster-thin-persistence`，生命周期由 `scripts/test/online-hosted-raster-thin-gate.sh` 管理。复用同源默认入口产品 Runtime、隔离 NFS 和普通 User 原12权限，独立预检与业务预算600秒；不改变产品策略、权限、原套件或算法，仅验 API 持久化。

旋转519×515六波段 TIFF，显式选第二波段的零分类直接细化、第一波段重分类为安全整数上界后细化，以及第六波段全无效输入，三条 COG 保存链。样本覆盖跨510／512块的粗线、T／Y／交叉、对角线、环与孔洞、孤点、图边、合法零／负背景／精确大分类、NoData／mask／部分透明和非有限覆盖；源 scale/offset 不得隐式应用。独立全幅标量 Guo–Hall oracle 固定两阶段顺序直到收敛，不调用产品邻域、块遍历或细化实现。逐成果严格核完整0／1／NaN像元及mask、Float64／NaN、完整网格和CRS、无色表及scale/offset、COG完整布局和overviews、artifact／物理／Meta相等正整数大小；核对精确父执行／用户／租户／引擎扫描、Monitor和当前执行所有源血缘、源和先前成果不变。

非法波段、有效小数分类、越界分类和非法前景参数四条失败链，必须匹配当前 execution 的 `thin` 任务与准确参数错误；失败不得发布文件、输出血缘或新增扫描／派生边。成功、失败、超时及部分启动均执行所属工作目录、NFS、Runtime、应用及Infra清理，清理失败即验收失败。仅认证合成样本API闭环，不含页面、容量、任意影像或完整拓扑数学证明。

真实GDAL／COG及篡改拒绝沿 `make test-geopython-workflow`，编排、严格校验、生命周期和登记沿 `make test-raster-thin-online-runner`，并纳入原 `make test-raster-online-runner`。仅手工登记，不新增定时。隔离候选标准Native专项88 passed／1869 deselected／5.64秒，本套件脚本子门禁56项及完整Online登记通过；较大脚本聚合在范围外矢量分区统计模拟生命周期单测60秒超时停止，仍不计整套通过。

固定 `7d376d998` 的 [Hosted 39698265976](https://github.com/pampa0629/addp/actions/runs/39698265976/job/119560904827) 已通过上述三条真实COG保存链、四条精确参数失败及全部隔离清理。七个Backend和默认入口Runtime同源，User33／Tenant2原12权限；原生GDAL3.10.3／Python3.11的现场全像元／mask与完整COG校验已执行，三方大小严格相等，分别9574／9574／10787字节，有效像元267227／267227／0、骨架像元1529／1529／0，预检与业务150285毫秒。源及先前成果不变、精确扫描父身份、当前执行血缘及Monitor、NFS／Runtime／部署与Infra零残留通过。固定源码、物理回执与原始日志已独立复核；归档未保留原TIFF，未作离线成果重读。不计UI、容量、业务DELETE、任意影像或全栅格认证，同版本Linux全量仍等CI会话回传。最终报告哈希及边界见[栅格专题验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)。

## raster-boundary-clean-persistence

独立手工 Hosted Linux x86_64，入口 `make test-online ONLINE_SUITE=raster-boundary-clean-persistence`，生命周期由 `scripts/test/online-hosted-raster-boundary-clean-gate.sh` 管理。复用同源默认入口产品 Runtime、隔离 NFS 和普通 User 原 12 权限，独立预检与业务预算 600 秒；原各套验收及权限保持。仅验 API 持久化，不计页面、容量或任意影像效果认证。

同一 519×515、EPSG:4326 旋转六波段 TIFF 显式选择分类波段，执行直接清理、重分类接清理和全无效输入三条 COG 保存链。覆盖合法零、负分类、±(2^53−1) 及相邻大分类、全图类别计数并列、3×3 邻域跨块与边缘，以及 NoData／mask／透明和非有限值；全无效成果必须成功保存完整 NaN／无效 mask。独立全幅 Counter 与不可变两阶段逐像元 oracle 核对完整值及 mask，不调用产品排序、分块或中间文件实现，精确比较不采用近似容差。

逐成果核对原尺寸、CRS 与完整仿射网格、Float64／NaN、完整 COG／金字塔、未继承源 scale／offset／色表，以及 artifact／物理／Meta 相等正整数大小。仅源由普通用户手工精确扫描一次，三个目标必须来自各自父执行的自动扫描；严格核扫描执行、父执行、用户、租户和引擎，Monitor 与当前执行源输入派生血缘一致。非法波段、小数类别和越界类别必须失败，输出、扫描及 active derive 图均不得增加；每步重核源物理／Meta 与先前成果不变。所有退出路径检查工作目录、NFS export／私有目录、Runtime／镜像／registry、部署及 Infra 清理，清理失败使门禁失败。

真实 GDAL／COG 及篡改拒绝沿 `make test-geopython-workflow`；编排、严格校验、隔离生命周期与登记沿 `make test-raster-boundary-clean-online-runner`，并纳入原 `make test-raster-online-runner`。只登记手工 Hosted，不新增定时。固定 `1b5a26bc1` 的 [Hosted 39625363775](https://github.com/pampa0629/addp/actions/runs/39625363775/job/119309061524) 已通过上述 API 场景，预检与业务 194662 毫秒，三份大小 31270／13788／10787 字节，有效像元数 267227／267227／0；现场物理校验回执、失败 Runtime 诊断与完整日志已独立复核。归档未保留原 TIFF，未作离线重读；无页面截图，不计 UI／容量或同版本 Linux 全量通过。最终报告哈希与边界统一维护于[栅格专题验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)。

## 分类蚕食容量（raster-nibble-capacity）

独立手工 Hosted Linux x86_64，入口 `make test-online ONLINE_SUITE=raster-nibble-capacity`，生命周期由 `scripts/test/online-hosted-raster-nibble-capacity-gate.sh` 统一管理。复用同源默认入口产品 Runtime、NFS 与普通 User 原 12 权限；管理员身份只配置本轮资源策略，计算、扫描和血缘均由普通 User 发起与读取。原 Raster T4、众数／蚕食持久化及旧完整场景容量套件保持原范围与预算。

确定性 Int64 分类样本为供体网格，尺寸为 4096²／10980²，供体间距为 128／2 像元，包含合法零、负分类、安全整数边界、NoData 和显式 mask。每种样本按运行上限 1／2 分别提交两个蚕食→COG DAG，共 8 组／16 链；每组一次观测，不推导吞吐稳定性或真实遥感专题效果。独立业务预算 10800 秒；Runtime 固定 4 CPU／2 GiB／无额外 swap，缓存固定 64 MiB，不修改产品默认资源建议。隔离身份夹具使用同名 suite，复用原普通 Raster 用户的 12 项权限和已有容量夹具的 4 小时 Access Token／Family 配置，覆盖业务预算及清理；普通 Raster 仍为原会话期限，产品认证配置不变。策略恢复失败时同时保留首个业务失败和恢复错误，不能用清理错误覆盖首个失败，也不能将恢复失败记为通过。

每组重建同镜像的本轮 Runtime 后测量 cgroup v2 的内存、OOM、CPU 与进程 VmHWM 之和，验证实际串行排队／双任务并发及额度排空。将 TMPDIR 绑定到本轮 NFS 根内的私有目录，使用受限自有路径的宿主机只读采样记录私有文件逻辑／分配大小，避免采样进程计入 Runtime 内存。采样间隔包含 HTTP 观察和采样开销，不宣称捕获连续精确峰值。每像元 32 字节是私有距离／供体中间文件的逻辑上界，源、工作 TIFF、COG 和金字塔另计；运行前按并发与网格保守预留磁盘，空间不足应失败。

使用独立解析最近供体 oracle 分块核对全部像元、mask、CRS／网格和完整 COG 布局；逐项核对 artifact／物理／Meta 相等正整数大小、精确自动扫描、同用户／租户、当前执行血缘、Monitor、源及先前成果不变。保留逐组资源原始观测及进度，即使 OOM 或失败也不丢弃诊断；工作目录、NFS／部署和 Infra 清理仍为通过前提。确定性回归由 `make test-raster-nibble-capacity-online-runner` 与 `make test-geopython-workflow` 覆盖，CI 注册同次验证；容量脚本入口同时运行隔离身份夹具 Go T1。不增加夜间调度。

固定源码 `d12960c06e4e41a21c821780ec99f8fd7677c98a` 的[独立 Hosted 39408596156](https://github.com/pampa0629/addp/actions/runs/39408596156/job/118580519411) 已通过并经完整原始归档严格审计：8 组／16 链、1098700928 个像元及对应 mask、COG、三方严格大小、16 个精确扫描、同用户及当前执行血缘、源与先前成果不变、策略恢复和全部零残留通过；预检与业务 3120412 毫秒，小于 10800 秒预算。报告 SHA-256 为 `5a628bc5f9b30275aa26ca36c3a813ec49b22a7889b85096e9762f0584698959`。同源[Linux Native](https://github.com/pampa0629/addp/actions/runs/39408524778/job/118580271250) 的标准 `make test-geopython-workflow` 全量 1666 项通过、21 warnings、757.20 秒，无失败或跳过；同源 Platform 在范围外 Infra 生命周期门禁失败，不计整个平台通过。资源逐组指标及其适用边界见[栅格专题](../../../next/栅格算子体系后续专题.md#分类蚕食容量验收)，不把此次确定性分类样本容量计作 UI、任意影像或稳定吞吐认证。


## raster-disk-pressure

独立手工 Hosted Linux x86_64，标准业务入口为 `make test-online ONLINE_SUITE=raster-disk-pressure`，生命周期脚本为 `scripts/test/online-hosted-raster-disk-pressure-gate.sh`。复用既有产品镜像构建与默认入口、七个 Backend、NFS 和普通 User 原 12 权限；沿现有 `raster-workflow` 身份夹具，不修改 System、产品认证／权限、计算算法、磁盘阈值或旧套件。业务预算独立 600 秒，Runtime 4 CPU／2 GiB／无额外 swap；不作为吞吐或大影像容量认证。

仅在本轮独占 Hosted Runner 上，以实际预分配的 4 GiB 文件建立归属可核对的 loop／ext4 文件系统，绑定到 Runtime 的 `/raster-pressure-tmp`，源、成果与证据位于其外。占位文件调节真实剩余空间；源为 1024² 双波段整数分类影像，显式选择第二波段，包含合法零、NoData 和 mask。1024² 影像显式使用合法 4096 分块，原始网格只需一个块且无 overview，独立物理校验完整 COG 布局、块尺寸及无 overview 事实，不把“必须有金字塔”强加给该样本。

蚕食与邻近距离各运行“正常基线→距离计算前拒绝→编码前拒绝→释放占位空间后重提相同 DAG／目标恢复成功”，共 8 条平台执行。距离阶段保留约 2 GiB + 40 MiB，工作影像预算通过后距离预算不足；编码阶段保留约 2 GiB + 128 MiB，计算预算可通过而 COG 的保守瓦片预算不足。采用静态空间窗口，不向产品注入 hook，不抢占计算中途时序。严格核实际失败阶段、required／available／pending／safety 字节数、任务失败及无半成品、无新增扫描／派生血缘；切换查询语言不得改写已记录失败。源与全部先前成果不可变，同一 Runtime 容器和 master／worker 身份保持，释放空间后重提任务成功，无需重启。

蚕食提交中文、邻近距离提交英文，完整中英文链路是验收要求。当前只读已确认 Common `HTTPExecuteWorkflow` 未转发 `Accept-Language`；Runtime 自身的中英文诊断已有 Native 证据，不能替代英文平台 Hosted。该产品语言传递问题不在本轮测试夹具修复范围，未修复前保留验收缺口，不按中文默认值弱化英文断言或记录整套通过。

四条成功链严格核完整像元／mask、Float64 NaN、单位、原 CRS／网格、COG、artifact／物理／Meta 相等正整数大小、精确父执行／普通用户／租户／引擎自动扫描、当前执行血缘和 Monitor。四条拒绝后均核无新增成果、扫描和派生血缘；独立 oracle、源物理／Meta 与先前成果不变贯穿全部执行。退出路径先停止本轮 Runtime，再核精确挂载源与 loop backing file，卸载、分离设备、删除镜像／占位文件和本轮归属目录，随后核 NFS、产品部署及 Infra 零残留；部分启动失败也必须清理，不允许以跳过或错误阶段充数。

标准确定性入口为 `make test-raster-disk-pressure-online-runner`，由原 `make test-raster-online-runner` 聚合，覆盖隔离挂载归属、失败清理、8 条业务链和 CI 登记；GDAL 物理 oracle 与拒绝／恢复回归走 `make test-geopython-workflow`。本场景不新增并发抢盘或容量不可读取的 Hosted 故障注入，相关 Native 证据独立保留。独立入口与夹具已实现，本地 Native 全量、受影响场景复验及标准脚本聚合门禁通过；目前尚未执行真实 Hosted，仍需先补齐提交语言传递，不计 T4 通过。


## raster-fill-depressions-persistence

独立手工 Hosted Linux x86_64，标准业务入口 `make test-online ONLINE_SUITE=raster-fill-depressions-persistence`。复用现有 Hosted 生命周期、NFS、同源产品默认入口 Runtime 与普通 User 原十二权限，独立600秒业务预算，不改变原 Raster／分类整理套件、权限或跨模块产品逻辑。

北向非方形网格的519×515多波段 DEM，显式选择第二波段；覆盖嵌套洼地、多／对角出口、图像与内部无效边界、合法零和负高程、跨512块、NoData／mask／alpha／非有限。三条保存链为米制填洼、英尺换算米制后填洼、全无效第三波段，独立逐水位连通oracle核全像元／mask；输出Float64米制NaN、原网格／CRS、COG／overview、无scale/offset泄漏。非法波段、旋转网格、单位冲突及未应用scale四类必须精确匹配当前父执行和fill任务的参数失败，不能用权限／保护投影／加载失败充数。每条只承认实际输入的当前执行血缘；核artifact／物理／Meta相等正整数大小、精确扫描的父执行／用户／租户／引擎与Monitor，失败无发布、无新增扫描或派生血缘，源及先前成果不变。

成功与失败均沿现有隔离生命周期核工作目录、NFS、部署和Infra零残留。Native物理夹具与脚本门禁走标准入口，新增文件同次登记Native、Online、影响路由及CI一致性检查；入口、夹具及 CI 登记已实现并通过本地 Native 专项95项、脚本59项与完整登记检查；详细边界与日志指纹见[栅格专题](../../../next/栅格算子体系后续专题.md#dem-填洼独立持久化验收)。固定 `8599f94ed8aefffd98370ee1c4d19d8d81c158ce` 的 [Hosted 39727166222](https://github.com/pampa0629/addp/actions/runs/39727166222/job/119653632678) 已通过三条真实COG保存链、四类准确任务失败及领域归档复核。三方大小16173／16565／10849字节，有效像元267255／267255／0；米制及英尺链均抬升23815像元，最大29米／8.8392米。普通User33／Tenant2原12权限、七Backend和默认入口Runtime同源，预检与业务198588毫秒；精确扫描身份、当前执行实际输入血缘、Monitor、源及先前成果不变和全部清理通过。归档未保留TIFF，未离线重读成果像元，无页面截图；现场严格物理校验与报告指纹见专题。不认证UI、容量、平台中英文矩阵或完整水文分析，Linux全量继续等CI会话回传。

### DEM D8 流向持久化

独立手工 `raster-flow-direction-d8-persistence`／`make test-raster-flow-direction-d8-online-runner`，600秒业务预算，沿用既有Hosted生命周期、NFS、产品默认入口Runtime及普通User原12权限。三条COG保存链：直接流向、填洼接流向、全无效；四类错误（封闭洼地、非法波段、旋转网格、单位冲突）各按中英文提交，精确匹配当前执行的direction任务与语言原因，并核反向查询不会改写错误。严格核完整像元／mask、方向码、闭式最短平坦路径、无环与最终开放出口、源网格／CRS、COG、artifact／物理／Meta相等正整数大小、精确扫描身份、当前执行实际输入血缘及Monitor；失败无发布／新增扫描血缘，源及先前成果不变，Runtime工作区／NFS／部署／Infra全部清理。本地夹具专项33项、脚本门禁62项及Online登记通过，独立Hosted待运行及归档审计。仅API持久化，不计UI、容量或Linux全量；原套件及预算不变。

### D8 汇流累积持久化

独立手工 `raster-flow-accumulation-d8-persistence`／`make test-raster-flow-accumulation-d8-online-runner`，600秒预算，沿用Hosted、NFS、同源默认入口Runtime和原普通User十二权限。四条完整COG链：已登记流向、DEM接流向接累积、填洼接流向接累积、全无效。七类错误（非法方向含UInt64 NoData相邻大整数、内部伪终点、越界指向、无效下游、循环、非法波段、旋转网格）各中英文提交，14条精确accumulation任务失败，反向查询不得改写旧错误。独立逐源路径核完整计数／mask、不计自身与终点总量守恒，严格核网格／CRS、COG及三方大小、精确自动扫描身份、当前执行实际输入血缘与Monitor、源和先前成果不变、失败零发布／零新增扫描血缘及Runtime工作区／NFS／部署／Infra全部清理。原套件不变；本地真实GDAL夹具专项40项、脚本门禁62项及完整Online登记通过，原栅格脚本总门禁退出0；共享重复测试不累加，Go隔离身份T1不计全量IAM。独立Hosted运行及归档审计待验，Linux全量等CI会话回传，不计UI、容量或完整水文体系。
