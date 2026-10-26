# ADDP Online 场景：Manager与空间成果

[返回测试与验收总入口](../../addp测试与验收规范.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

本页场景：[manager-internal-artifact-lineage](#manager-internal-artifact-lineage)、[manager-hybrid-search](#manager-hybrid-search)、[vector-pmtiles-workflow](#vector-pmtiles-workflow)、[raster-workflow](#raster-workflow)、[raster-contour-persistence](#raster-contour-persistence)、[raster-polygonize-persistence](#raster-polygonize-persistence)、[raster-boundary-clean-persistence](#raster-boundary-clean-persistence)、[raster-thin-persistence](#raster-thin-persistence)。

## manager-internal-artifact-lineage

### 明确的验收 case

2026-10-24 用户批准在同一 suite 内增加独立 `native-raster-media` case，专门验收 IMG 与 JPEG2000；默认 `full` 保留原完整场景、八模型、授权、基础 FBX 及其他已有断言。二者复用同一 Hosted disposable 部署、既有十项普通 User 权限和原生产品 Runtime，不增加服务、权限或 suite，也不通过旧源码或放宽模型源授权运行。

唯一选择项为 `ADDP_ONLINE_MANAGER_CASE=full|native-raster-media|kml-export`，未提供时明确选择 `full`，未知或空值在任何 Hosted 生命周期动作前拒绝。标准业务入口仍为 `ADDP_ONLINE_MANAGER_CASE=native-raster-media make test-online ONLINE_SUITE=manager-internal-artifact-lineage`；人工 Hosted 使用既有 workflow 的 `suite=manager-internal-artifact-lineage` 和明确 `manager_case`。Job 名称、Artifact 名称和 `manager-case.json` 必须显式包含所选 case，不能把任一独立 case 的通过标为完整 Manager 场景通过。KML case 的独立数据、身份、导出与清理边界见下节。

`native-raster-media` 只执行下述既有 IMG/JPEG2000 子场景，沿同一入口验证真实 Meta、原文件、COG、页面、缓存和领域／物理清理；两个子场景任一个失败即失败，不因另一个通过继续宣称整项通过。独立根报告为 `manager-native-raster-media.json`、`schema_version=addp.manager-native-raster-media/v1`，包含 `case=native-raster-media`、完整 `source_formats=[img,jpeg2000]`、普通身份、两个子报告及真实失败状态；总 `online-report.json` 仍由统一分发器生成。未执行的模型、点云、文档、结构化数据及其他格式既不计通过，也不伪装成跳过后完整通过；完整 case 在新模型源授权迁移完成前仍可能受其原有正向断言阻断，须单独验收。成功／失败后的应用、夹具、凭据与 Infra 清理继续使用同一 Hosted 生命周期，不修改开发服务。

case 路由、默认完整场景保留、未知值只读拒绝、子场景失败与清理、case 归档及 workflow 接线由现有 `make test-manager-online-runner` 及 CI 登记检查覆盖；不新增长期脚本或根 Makefile 入口。

### KML 实际页面导出

`kml-export` 是同一 `manager-internal-artifact-lineage` suite 内独立的 Manager 导出 case，仅使用本轮隔离 PostgreSQL/PostGIS 源、Manager→Transfer 标准导出会话和 Infra 暂存区；不运行或放宽原模型／媒体栅格断言。标准入口为 `ADDP_ONLINE_MANAGER_CASE=kml-export make test-online ONLINE_SUITE=manager-internal-artifact-lineage`，既有 Hosted workflow 使用同名 `manager_case`。选择、Job／Artifact 名称和根报告必须明确包含 case，不将本 case 的通过记为完整 Manager suite 通过。

复用仓库 PostgreSQL 产品镜像和既有 disposable 源生命周期，数据库只读角色仅访问合成样例；不连接开发数据库。普通 User 只具备八项既有权限：`manager.data_item.read`、`manager.derived_artifact.create`、`manager.derived_artifact.read`、`meta.catalog.read`、`meta.scan_task.execute`、`meta.scan_task.read`、`monitor.execution.read`、`transfer.task.read`。2026-10-25 用户确认仅为本隔离 case 补充最后一项：Monitor 按 Transfer Owner 裁决读取 `sync`，本人导出执行也须具备该权限，Manager 产物读取权限不能替代；缺少该项须在身份前置检查拒绝，不放宽 Monitor 归属断言。独立源初始化 User 沿既有四项初始化权限和 System 正式命令，为扫描后确认的精确表办理读取 Grant，不给消费者引擎级范围或管理员权限。报告沿正式回执记录 `request_id`、`item_fingerprint`、`boundary_hash`，不假设存在 `id`；本地调用链回归须实际经过精确 Grant 初始化 helper、拒绝→签发→允许检查，再进入浏览器阶段。其他 case 的十项普通权限和模型初始化身份保持原边界。全部参与服务（含 Transfer）的 Build ID 与 ready 前置检查须先通过。

源表分别声明 EPSG:4326 与 EPSG:3857，覆盖七类二维非空几何、内洞和 null 几何，以及 int32、bool、double、中文／XML 特殊字符、空字符串和 null。同一普通用户在真实 Console iframe 查看表，打开共享导出弹窗，选择 KML 并修改文件名，经真实按钮建立导出会话、等待 Transfer 完成和接收浏览器下载；不以 API 下载替代按钮。独立 XML oracle 核对完整 Placemark、几何／坐标、Folder、Schema／标量、null／空字符串及数值布尔，保留文件名、文件哈希、截图和同一用户／无任务定义的执行归属。EPSG:3857 输出须恢复已知 WGS84 坐标，源数据、结构和行数前后不变。原生 LIBKML 跨版本读取仍沿已登记 GeoPython 门禁验证，XML oracle 不冒充产品 KML 读侧增强。

新 case 的报告、异常和下载证据不得泄漏凭据。导出会话沿现有保留与生命周期规则，不新建业务删除 API；成功和失败均销毁本轮源容器／数据、应用、临时凭据及 Infra，并核零残留。业务结果和清理结果独立报告：业务失败且夹具清理成功时仍是 `result=failed / cleanup=passed`；夹具清理实际失败必须保留 `cleanup=failed`，不能由 Infra 零残留替代。部署清理不冒充会话业务 DELETE 或缓存验收。根 `make test-manager-online-runner`、`make test-online-runner`、System Go、共享前端及现有 Platform CI 覆盖新增路由、最小身份、页面脚本、物理 oracle 拒绝样例和失败清理；正式 Hosted 尚未通过前不得回填已验收。此 case 不扩展 bigint／decimal／时序／嵌套字段、Z/M、样式、KMZ 或其他目标格式。

### IMG 原生扫描与 COG 快显

同一 Manager Hosted suite 增加 IMG 子场景，使用既有普通 User、十项权限、Business MinIO、GeoPython 产品 Runtime 与同一部署生命周期，不新增 suite、服务或权限。Runtime 中生成三份自有 256×256 三波段 Byte HFA：WGS84、UTM50N 和无定位影像；全部自包含、同数据类型且无 NoData。真实 Meta 深度扫描须证明 `img/media/single`、HFA 编码、尺寸、波段及明确的源 CRS/范围；无定位样例不得出现推断的空间能力或 COG 生成动作。

两份有定位 IMG 经正式 `generate_raster_cog` action 生成受管 TIFF/COG，核任务 `source_format=img`、原生 Runtime 身份、完整像元/mask/CRS/仿射网格、完整 COG 布局、API/物理哈希和大小、Range 前后缀及越界拒绝。同一普通 User 在真实 Console iframe 显示 WGS84/UTM COG，检查 WebGL 图层的品红/青/黄像素及截图，重开复用同一结果且不产生额外生成请求；原文件下载哈希保持一致。无定位样例核元数据、不开放地图，以及可见的“不支持直接预览”提示；预览区域不得挂载普通图片组件或 `<img>`，不得把 IMG/JP2/J2K 源交给浏览器图片解码。v2 浏览器报告必须保存 `source_image_absent=true` 与 `unsupported_message_visible=true`，缺失或为假均拒绝，不能仅凭无地图／无生成按钮判通过。生成、页面、复用及退出均核源原字节不变；通过正式 Manager API 删除结果和任务，核内容404、物理对象缺失、源预览模式恢复与零残留。失败仍须尝试本轮已知精确资源清理，保留首个业务错误和清理错误。

IMG 与 JPEG2000 子场景复用唯一媒体栅格 runner，分别归档 `manager-img*.json` 和 `manager-jpeg2000*.json`，报告使用 `addp.manager-raster-media/v1` 与 `addp.manager-raster-media-browser/v2`，旧 Manager 场景断言保持。标准确定性入口仍为 `make test-manager-online-runner`，原生物理夹具由 `make test-geopython-workflow` 自动发现，CI 注册检查须确认新子场景、浏览器和两组回归在已有入口中实际接线。仅手工触发既有 Manager Hosted，不增加定时；真实结果出来前不记通过。该证据不外推 IMG 导出、外部 IGE/RRD/依赖文件、复数/异质波段、自定义 WKT、NFS Hosted 或任意影像容量。

JPEG2000 扩展仍使用同一 Manager Hosted、Business MinIO、十项普通 User 权限及产品 Runtime，不新建 suite 或服务。四份 256×256 三波段 Byte 合成源分别为 GeoJP2 WGS84、GMLJP2 UTM50N、无定位 JP2 和原始 J2K；采用可逆无损编码。检查 canonical `jpeg2000/media/single`、JP2OpenJPEG、profile、尺寸和波段，完整下载核源大小/哈希。两份定位源沿同一任务入口生成 COG，核实际 source_format、完整像元、mask、CRS、仿射网格、COG布局、HTTP/物理哈希及Range和416；真实浏览器检查三色像素、截图、重开复用且零新生成请求。两份无定位源无地图、无生成动作，不假定经纬度。每份源 Meta 及字节前后不变，两个 COG 结果/任务经 API 删除后核物理对象缺失、内容404和预览模式恢复，部署与Infra清理仍须通过。此扩展不认证外部定位/PAM、JPX组合、NFS/FTP、JPEG2000写出或任意厂商容量；旧IMG通过证据不代表JPEG2000通过。

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

独立手工 Hosted Linux x86_64 T4，标准入口 `make test-online ONLINE_SUITE=vector-topology-persistence`，复用共享 Hosted 生命周期、NFS export 所有权及正式 GeoPython 产品默认入口。同一成功 execution 的各条分支分别执行线网构面双保存、单要素检查→修复→复查保存、严格参考吸附保存、单图层面叠检查保存、显式覆盖范围的双输入面缝检查保存、关联来源的悬点检查保存及跨要素线叠、线内自叠、不同要素精确点交会检查、交点打断、原始部件连通分组及链路合并双保存。只使用当次 disposable 非默认 Tenant 和同一普通 User，精确 11 项权限：`develop.task.read`、`develop.task.execute`、`develop.data_read.execute`、`develop.data_write.execute`、`system.execution_authorization.create`、`meta.catalog.read`、`meta.scan_task.read`、`meta.scan_task.execute`、`meta.lineage.read`、`monitor.execution.read`、`manager.data_item.read`。Engine 登记身份独立且登记后撤去基础设施 Token；不得扩大业务权限、600 秒预算或复用个人会话。

十五份固定源为 `source.geojson`、`invalid.geojson`、`snap-input.gpkg`、`snap-reference.gpkg`、`overlap-input.gpkg`、`gap-input.gpkg`、`gap-coverage.gpkg`、`dangling-input.gpkg`、`line-overlap-input.gpkg`、`line-self-overlap-input.gpkg`、`line-intersection-input.gpkg`、`line-self-intersection-input.gpkg`、`split-line-input.gpkg`、`line-component-input.gpkg`、`line-chain-input.gpkg`；成功成果严格为 `faces.geojson`、`diagnostics.geojson`、`repaired.geojson`、`snapped.gpkg`、`overlaps.gpkg`、`gaps.gpkg`、`dangling.gpkg`、`line-overlaps.gpkg`、`line-self-overlaps.gpkg`、`line-intersections.gpkg`、`line-self-intersections.gpkg`、`split-lines.gpkg`、`line-components.gpkg`、`line-chains.gpkg`、`line-chain-members.gpkg`。血缘输入按 `input.<读取节点ID>` 与源 locator 精确一一绑定，拒绝缺项、重复、额外输入及端口错绑；数组顺序不作为资源身份，不能要求与源文件清单顺序一致。所有保存均使用 `mode=fail`，十五份稳定 outputs 和 lineage_facts.outputs 必须为 `write_mode=create`。十五成果分别只有一次精确自动扫描，公开 tenant_id/triggered_by 必须为严格正整数且匹配当前 User/Tenant，拒绝私有字段替代、手工补扫、缺失事实或重复扫描。十五份物理与 Meta 大小必须是相等的严格正整数；GPKG 核对文件与唯一子图层的边界。线源→faces/diagnostics、invalid→repaired、两个吸附源→snapped、面源→overlaps、待查面与覆盖范围分别→gaps，悬点线源→dangling，线叠线源→line_overlaps，线内自叠源→line_self_overlaps，线交源→line_intersections，线内自交源→line_self_intersections，打断源→split_lines，分组源→line_components、链路源分别→line_chains/line_chain_members，共十七条当前 execution 的 active derive，Monitor 必须一致。

独立 GDAL oracle 按显式预定义坐标验证四面四 dangle；自交面修复后保留两行各含两三角面的 MultiPolygon、属性及修复前 false/原因与修复后 true/Valid Geometry。GeoJSON 成果保留文件原 CRS84 声明，Meta 必须为 OGC:CRS84、无虚构 EPSG SRID；地图按原经纬坐标验证 direct_renderable/not_transformed，不临时选 CRS。

严格参考吸附的两份单层 GPKG 源为 EPSG:3857、米单位，五条输入线与六条固定参考线，tolerance=.75 核对原始位置上限、严格边界、等距 XY、不插点和不吸向线段内部，源/属性/行序/XY/CRS 不变。另一个 tolerance=1.25 的失败 DAG 必须以固定 `snapping produced invalid geometries` Owner 原因整批拒绝，失败 execution/Monitor 同 User 且无保存、facts、扫描或源变化；只允许有界观察当前 execution 的隔离 Owner 日志，不重试业务。浏览器之前先保存 `topology-snap-failure.json` 的最小公开观察、原因、Monitor 和未变化扫描清单，result=incomplete，不能替代最终成功。

面叠源是十二条有效 EPSG:3857 面，显式 parcel_id 唯一非空。真实 `load → check_polygon_overlap(id_field=parcel_id) → save` 必须输出七对：五对普通重叠/包含/完全重复、一对含两个面部分并排除接触线的多部件交集、一对面积 2^-40（约 9.09e-13）且二进制精确可表示的微小重叠。独立静态坐标和面积 oracle 核对双方标识、行序、每对唯一、Float64 面积和 MultiPolygon；共边、共点、孔洞内部不输出，不调用生产算法生成期望。物理层以 OGR 独立验证原几何/3857/七对及精确面积，预览层验证标准 EWKB、精确源坐标/字段/3857；GPKG 自动生成的存储 FID 按 OGR 实际列名核对，不冒充来源业务标识，不能将缺行、重复、错误来源或忽略小面积视为合格。不扩展 Quality 或自动修复。

面缝两份单层 EPSG:3857 GPKG 源分别为七条待检查面和七条显式覆盖范围面。真实 `load×2 → check_polygon_gap(input_gdf, coverage_gdf) → save` 输出八个独立 Polygon，按稳定位置排序：内部和边缘缺口各一、含两个孔洞的缺口一、待查面孔洞一、点接触和独立多部件三、面积精确为 2^-40 的微小缺口一。范围孔洞是合法留白，完全覆盖的区域、范围外数据不报告；范围重叠及源重复不重复报缺口。独立静态 oracle 逐原始坐标、孔洞、顺序、Float64 gap_area 和 3857 核验，不调用生产融合／差集生成期望，不舍入、不加容差、不自动填补。两个源各有本次 execution 的 active derive 到 gaps，不接受只连一个源或把两源互换。十五成果和十五源前后哈希／大小及 Meta 不变。

悬点源为十二条有效 EPSG:3857 线／多线，显式 route_key 是唯一非空字符串，另有 note 属性。固定场景包含孤立线、连续链、T 形、闭环、反向重复线、内部重合段、多部件和间距精确 2^-40 的微小断口。真实 load→check_dangling_points(id_field=route_key)→save 输出十七行 Point＋source_id；闭环和 T 形连接处不报，重复线同点分别关联两个原始 ID，多部件同源同点只报一次，结果 source_id 与 GPKG 自动 FID 分开验证。独立静态原坐标／来源／行序 oracle 使用 OGR GetPoints/GetX/GetY 核验，不调用生产打断或计度算法生成期望，不舍入、不加容差。悬点源到成果有唯一当前 execution active derive，成果仅一次同主体自动扫描且严格大小匹配 Meta；真实预览标准 EWKB、十五幅地图中的 Point 要素、实际显示坐标及像素必须通过。排序静态回归仅导入 geometry-preview-coordinate.js 中唯一的纯数学投影 oracle，不加载含 OpenLayers 解码器的 geometry-preview-support.js，不要求安装前端 node_modules；真实地图与解码验收仍使用相应前端依赖。地图 Point 按完整 source_id＋显示坐标在同一 Node 环境用确定性身份顺序比对，不以浏览器语言排序推断记录匹配，不去重，保留重复线的两个来源；物理和 API 的原始行序仍须精确核对。点图统计共享蓝色 Point 样式的非透明前景像素（不计底图水域），其余十二幅线面图的绿色像素断言保持；原始图分别归档 point_pixels／green_pixels，不改产品样式。十五源十五成果的前后原字节与源 Meta 不变；不扩展吸附、修复或 Quality 结论。

同一 User 经 Console 检查成功执行、十五幅真实 Manager 地图与塌缩失败页。每次导航读取当前页面自身响应，再通过 browserAPIFromResponse 核同一 User/Tenant/11 权限；不跨导航持旧 Token，不 mock 响应或注入内部图对象。GPKG 预览使用容器唯一 child_name、标准 EWKB，真实 OpenLayers 要素和 EPSG:3857 投影像素同时核验。物理及 API 坐标与源预期精确相等；显示值按共享的独立球面墨卡托数学 oracle 计算 EPSG:3857→4326→3857 后逐值精确比较，不用舍入或容差。面叠／面缝的小面积几何仍须保留，要素数和完整坐标必须正确，像素由实际可见重叠或缺口区域核验。

线叠源为十六条有效 EPSG:3857 线／多线，使用显式字符串 route_key，保留 note 原属性。固定九对结果覆盖部分重合、包含、反向重复及三要素逐对、多部件并排除点接触、精确长度 2^-40、源内折返去重和不同采样顶点；孤立自重合、点接触、交叉和相距 2^-40 的平行线不报告。真实 load→check_line_overlap(id_field=route_key)→save 输出九行 MultiLineString＋source_id_a/source_id_b/overlap_length；长度为原坐标单位，双方来源和顺序独立核对，GPKG FID 不能替代业务 ID。独立手写坐标与长度 oracle 使用原生 OGR GetPoints，逐部件核坐标并以独立线段长度验证，不调用生产相交算法生成期望，不舍入或加容差。原始预览 EWKB、双 ID 和长度、真实 OL MultiLineString 坐标及身份、绿色前景像素和截图必须全部通过。微小线段的原始坐标/长度必须保留，但不宣称其亚像素投影必然可单独目视。线叠源到成果只有一条当前执行 active derive，一次同主体自动扫描；源字节/Meta 和成果末轮均不变。此扩展只检查、不修复，不增加权限或场景预算。

线内自叠源为十五条有效 EPSG:3857 线／多线，显式字符串 route_key 与 note 原属性保持不变。真实 load→check_line_self_overlap(id_field=route_key)→save 输出八行 MultiLineString＋source_id/overlap_length：连续折返、三次经过、不同顶点分段、多部件正长度重合、精确长度 2^-40、夹杂零长度段的折返、同方向重复部件和重复闭环；普通闭环、点自交、近邻线、单纯零长度段、仅端点接触和两个独立但重合的源要素不报告。每个源同一位置只计一次长度，不按经过次数累加。独立手写原坐标／长度 oracle、原生 OGR GetPoints、物理文件篡改负例与实际 EWKB／OpenLayers 来源和几何／绿色像素共同核验，禁止用生产算法生成期望。此源到成果只有一条当前 execution active derive，成果只有一次同主体自动扫描；源 Meta、十五源十五成果原字节前后不变。十五图加成功和塌缩失败页共十七截图；不将亚像素微线宣称为单独目视可见，不扩容差、修复、权限或600秒预算。

新增线交源为十八条有效 EPSG:3857 线／多线，显式 route_key 与 note 保持不变。真实 load→check_line_intersection(id_field=route_key)→save 输出十行 Point＋source_id_a/source_id_b：三线同点三对、T形、端点接触、线点混合中的独立交点、重复多部件的两处点交会以及两个间距2^-40的点；重合段端点、近邻间隔和单要素自交不报告。独立手写原坐标／双方ID／逐对顺序 oracle 与原生OGR读取共同核验，并拒绝丢点、重复、错配、FID冒充来源、微点舍入及CRS篡改。文件级只有线交源→交点成果的一条当前 execution active derive；双方业务ID分别与同一原源文件绑定，不虚构两份文件血缘。成果仅一次同主体自动扫描，物理与Meta大小为相等正整数，实际预览EWKB、双来源字段／FID区分、OpenLayers完整身份及坐标和蓝色Point前景像素／截图均必须通过。亚像素间隔不声称单独目视可分；沿用原11权限、600秒及清理边界，不修改源线或修复。

新增线内自交源为十六条有效 EPSG:3857 线／多线，显式 route_key 与 note 保持不变。真实 load→check_line_self_intersection(id_field=route_key)→save 输出十五行 Point＋source_id：蝴蝶结、重复到访、跨部件接点、重合覆盖以外的支线交会、重合与独立交点并存、两个间距2^-40的点、两个不同源同XY及跨部件闭环四角。正常折点（含中间零段）、同部件闭环首尾、重复分段路径、相邻折返覆盖，以及自叠线内部与端点的支线交会均不报；两个不同源仅互相交叉也不报。期望为手写原坐标／原ID／源顺序及XY排序，原生OGR Point读值不得经默认GeoJSON截断；缺失、额外、重复、全图层去重、FID冒充来源、错误重合覆盖点或微点舍入均拒绝。文件级只有新源→点成果的一条当前 execution active derive，同主体一次精确自动扫描、正整数physical==Meta、预览标准EWKB／实际FID区分、真实OL来源及坐标、蓝色Point像素和截图都必须通过；只检查不修复，无新权限或预算。

交点打断分支显式选择 `scope=all`，同一 `split-line-input.gpkg` 扩展为十七条有效 EPSG:3857 线／多线，显式唯一 route_key、可空 note 和 Int64 grade；不增加源文件、成果端口或权限。真实 load→split_line_intersections(id_field=route_key,scope=all)→save 输出四十三行 LineString，逐行保留全部原属性并追加 source_id、从1开始的 part_no/segment_no。手写原坐标预期在原十字／T形／端点／逆向多部件／正常闭环接缝／精确2^-40微小片段／重复顶点基础上，增加同源蝴蝶结与跨要素穿越同时切分、反向自交闭环原接缝、同XY多次经过的独立路径位置、同源多部件交会及切点重复原顶点。原方向、每个原顶点、源／部件顺序、全部来源属性及片段编号严格相等，禁止全源XY去重漏切或用生产算法生成预期，不舍入或加容差。原生OGR核字段类型、null、物理几何／3857与行序；缺少内部切分、合并多次经过、错来源／FID冒充、属性／编号／方向／微点／字段类型／数量或CRS篡改均拒绝。正长度重合仍整批拒绝，原产品Native负例保持，不在T4增加无关失败DAG。文件级仍只有打断源→成果的一条当前execution active derive、一次同主体精确自动扫描及严格正整数physical==Meta；文件血缘不替代逐片段source_id。浏览器先用真实分页控件选择每页50条，再消费同locator／child／page=1／page_size=50的当前页面响应，核43行完整内容；不注入组件状态或用独立API代替页面。真实容器child预览标准EWKB、FID与业务ID区分、全部原属性和片段号、OpenLayers四十三个LineString的完整属性／方向／坐标、绿色像素和截图必须通过。十五源十五成果、源Meta及浏览器前后原字节保持不变。复用原11权限／600秒、Hosted/NFS/默认入口Runtime和清理，原有全部断言保留；不增加容量或其他suite运行。旧between范围已通过T4只认证原范围，不外推本次all。

新增连通分组源为十一条有效 EPSG:3857 线／多线，显式唯一 route_key、可空 note 及 Int64 grade。真实 load→group_line_components(id_field=route_key)→save 输出十五条原始 LineString 部件、九个稳定连通组；同一源的三个分离部件分属三组，同源微小断口两部件跨组，而同源端点相接两部件同组。后出现的桥接线按传递连通归组，另覆盖T形、十字、反向重合、孤立闭环、源内自交／自叠和重复坐标。按原源行／部件顺序核对全部属性与source_id、Int64 part_no/component_id、原方向／全部顶点／闭环接缝与精确2^-40断口；预期坐标和组号固定手写，不调用生产分组生成预期，不舍入或按同源强行归组。独立原生OGR、真实保存重载及错组／错来源／属性／字段类型／方向／微点等篡改拒绝必须通过；实际预览EWKB／FID区分、真实OL完整属性和原坐标投影／绿色像素及截图均须通过。文件级只有新源→分组成果的一条当前execution active derive，同主体一次精确自动扫描和严格正整数physical==Meta，十五源十五成果前后及源Meta不变。复用原11权限／600秒及清理，不修复、不生成Quality结论，不重跑容量或无关suite；历史v11运行不认证本次连通分组扩展。

新增链路合并源为十六条有效EPSG:3857线／多线、十八原始部件；唯一route_key、可空note、Int64 grade及可空road均保留。真实load→merge_line_chains(id_field=route_key,match_fields=[road,grade])双输出保存为line-chains.gpkg（十二条LineString及共同属性、chain_id/member_count）和line-chain-members.gpkg（十八原部件及全部原属性、chain_id/member_no/source_part_no/Boolean is_reversed）。手写坐标和逐成员关系覆盖中段最早出现／反向拼接、全图三岔先于属性、属性变化断链、空road一致、同源多部件跨链及同链、闭环接缝、内部重复坐标与精确2^-40断口。独立原生OGR核全部坐标、原方向、来源／null及Integer64／Boolean subtype；任何错链／顺序／反向／属性／类型／微点／CRS／数量均拒绝，不调用生产合并生成预期。双成果各自仅新源→本成果的一条当前execution active derive、一次同主体公开整数扫描与严格正整数physical==Meta；文件级血缘不冒充逐成员业务来源。真实预览标准EWKB／FID区分、两图完整属性和精确OL坐标／绿色像素／截图须通过。十五源十五成果共三十原文件及源Meta前后不变，共十五扫描、十七当前执行derive、十五地图加成功失败页十七截图；沿用原11权限／600秒／生命周期清理，不执行容量或无关suite。旧v12通过仅证明原范围。

每个成果端口须在唯一几何类型／精确数量表中登记，API 行数和真实 OL 要素类型数量共用该表；连通分组严格为十五个 LineString，不允许落入其他成果的默认数量。未知端口明确失败；标准 runner 直接执行这段真实地图断言，逐端口拒绝少行、多行和错误类型，并验证表与 API／地图两处消费连接。

唯一报告版本为 addp.vector-topology-persistence/v14、addp.topology-browser/v14、addp.topology-checkpoint/v14，旧 v13 不接受。Owner、浏览器及阶段证据必须显式绑定 split_line_scope=all。等待血缘前归档 topology-before-lineage.json，保留十五份严格大小/公开扫描身份/物理哈希/稳定outputs与facts及原始 Monitor，标记 incomplete。topology-source-lineage-raw.json/v13 保留十五源原 Meta、十五原图和实际预览响应；topology-physical/ 保留全部十五源十五成果合成原文件，逐个核 SHA/大小并拒绝覆盖不同证据，支持下载后独立 GDAL 复核。浏览器前后源及成果均不变；最终 Owner/browser 完整成功、十五图及退出清理均实际通过才认本轮 T4。

2026-10-24，固定源码 `d9a3cec22e12db098182e25bc96a62e0916f5f32`（包含交点打断接入 `90a9eab843ecf38538a601f15dd0add5e9af24a9`）的[普通 User Hosted 40080467804](https://github.com/pampa0629/addp/actions/runs/40080467804/job/120832764807) 已实际通过并完成归档独立复核：User33／Tenant2／精确11权限，十三源十二成果、十二次自动扫描、十四条当前执行 active derive；交点打断十一条源线生成二十条 LineString，来源与全部属性、null、Int64 编号、原方向和顶点、精确 2^-40 片段均保留。成果 GPKG 为 98304 字节，SHA-256 `11939282c30049db0b76856de9ca802334be7f8863a4356a07564eade441c3e9`；二十五份归档原文件及浏览器前后大小／哈希一致。十二份实际 API 和原生 OGR 几何逐行一致，十二幅原始 OL 坐标用独立数学公式复核，交点打断完整属性与方向精确保留；十二幅地图及成功／失败执行页共十四截图已复核。Owner／browser v11 完整成功，checkpoint 的 incomplete 不替代最终报告；预检与业务共 115726 毫秒，塌缩失败无发布／扫描，归属 NFS／部署及 Infra 清理零残留。官方 artifact `12555218473` 的 ZIP SHA-256 为 `53be33fcdc5aa180bf9ebac02b1cebcf4412f0b1051b2e57c1c31e156e43aa90`。

同次发布后继源码 `5a5614564b38115b38f4091fc1742a46da5cae0e` 的[GeoPython CI](https://github.com/pampa0629/addp/actions/runs/40080291973/job/120832242674) 实际 4797 passed／21 warnings，[Console CI](https://github.com/pampa0629/addp/actions/runs/40080291973/job/120832242861) 实际 195 单测／219 浏览器及 build 通过，[Platform consistency](https://github.com/pampa0629/addp/actions/runs/40080291973/job/120832242706) 实际通过。此前固定本地 Console 未修改的公共 HMR 缓存夹具 194 passed／1 failed 保留历史，不倒推为本地通过；不外推当前 main、其他 Job、容量或业务 DELETE，也不宣称亚像素几何可单独目视。

NFS export/owned root、Runtime/registry、部署与 Infra 全部清理并核零残留；失败清理仍使门禁失败。归档合成字节不是业务目录残留；部署销毁不认证 Manager 业务 DELETE。旧构面/修复/吸附/面叠/面缝/悬点/线叠/线内自叠 T4 不外推线交／线内自交检查，代码与确定性回归不记为新真实 T4，不新增 schedule。

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

普通 PNG／JPEG 图片导出沿用本套件、同源产品 Runtime、原有普通 User 权限和清理入口，不另设 suite。完整既有场景之后，经真实 Develop 页面导入双节点工作流、选择编码（JPEG 显式质量 85）、首次保存、重开及执行，再在 Monitor 核对成功及目标资源。物理夹具独立读取 37×23 的 RGBA PNG／RGB JPEG：PNG 核完整像元和透明覆盖，JPEG 完整解码并按平滑合成源核误差（平均绝对误差 <5，最大 <15；非通用质量承诺），同时核原始／解码哈希、无 CRS／网格／GCP／RPC／EXIF／旁文件、源及既有成果不变。必须核精确输出、自动扫描、三方大小、保存任务删除和共享部署／物理对象／Infra 清理；页面生成不等同浏览器下载或 GIS 定位交付。确定性入口 `make test-geopython-workflow`、`make test-module MODULE=develop GO_ONLY=1`、`make test-develop-frontend` 和 `make test-raster-online-runner`；正式结果须另记实际 SHA 与原始产物。

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

独立手工 `raster-flow-direction-d8-persistence`／`make test-raster-flow-direction-d8-online-runner`，600秒业务预算，沿用既有Hosted生命周期、NFS、产品默认入口Runtime及普通User原12权限。三条COG保存链：直接流向、填洼接流向、全无效；四类错误（封闭洼地、非法波段、旋转网格、单位冲突）各按中英文提交，精确匹配当前执行的direction任务与语言原因，并核反向查询不会改写错误。严格核完整像元／mask、方向码、闭式最短平坦路径、无环与最终开放出口、源网格／CRS、COG、artifact／物理／Meta相等正整数大小、精确扫描身份、当前执行实际输入血缘及Monitor；失败无发布／新增扫描血缘，源及先前成果不变，Runtime工作区／NFS／部署／Infra全部清理。本地夹具专项33项、脚本门禁62项及Online登记通过。固定3944846c9的Hosted 39750207818／119728593684已通过且完成原始归档领域终审：225115毫秒，三保存链及八准确双语失败、三方大小、精确扫描／血缘／Monitor和全部清理通过。归档12414557855无TIFF，复核现场全像元worker及原始回执，未离线重算。仅API持久化，不计UI、容量或该394源码Linux全量；4fba的Linux另按精确版本记录，原套件及预算不变。

### D8 汇流累积持久化

独立手工 `raster-flow-accumulation-d8-persistence`／`make test-raster-flow-accumulation-d8-online-runner`，600秒预算，沿用Hosted、NFS、同源默认入口Runtime和原普通User十二权限。四条完整COG链：已登记流向、DEM接流向接累积、填洼接流向接累积、全无效。七类错误（非法方向含UInt64 NoData相邻大整数、内部伪终点、越界指向、无效下游、循环、非法波段、旋转网格）各中英文提交，14条精确accumulation任务失败，反向查询不得改写旧错误。独立逐源路径核完整计数／mask、不计自身与终点总量守恒，严格核网格／CRS、COG及三方大小、精确自动扫描身份、当前执行实际输入血缘与Monitor、源和先前成果不变、失败零发布／零新增扫描血缘及Runtime工作区／NFS／部署／Infra全部清理。原套件不变；本地真实GDAL夹具专项40项、脚本门禁62项及完整Online登记通过，原栅格脚本总门禁退出0；共享重复测试不累加，Go隔离身份T1不计全量IAM。固定4fba03313的Hosted 39761858875／119767722319已通过并完成领域终审：273053毫秒，四保存链／十四准确双语失败和全部严格断言及清理通过。前三成果各35819字节／264714有效像元／2终点，终点含自身总量264714；全无效成果10741字节／0有效。归档12420909006无TIFF，复核现场独立逐源路径全像元worker及原始回执，未离线重算。同精确源码Linux GeoPython 39761773681／119767424921全量2360 passed／21 warnings／1541.12秒，0失败／跳过／筛除；不覆盖后续流长／汇水分区，后续Linux仍等CI会话回传，不计UI、容量、整体Platform或完整水文体系。

### D8 下游流长持久化

独立手工 `raster-flow-length-d8-persistence`／`make test-raster-flow-length-d8-online-runner`，600秒预算，复用Hosted、NFS、同源默认入口Runtime及普通User原12权限。六条COG链覆盖米制／国际英尺／美制测量英尺已登记方向、DEM→流向→流长、填洼→流向→流长及全无效；七类非法方向／内部伪终点／越界下游／无效下游／循环／非法波段／旋转网格各中英文提交，共14条准确length任务失败，查询语言不得改写旧错误。独立逐源路径按横纵对角步数核全像元／mask、米制中心距离和严格终点零，保留原网格／CRS，完整COG验证；严格三方相等正整数大小、精确自动扫描、当前执行实际输入血缘／Monitor、源及先前成果不变、失败零发布／新增扫描血缘、Runtime工作区／NFS／部署／Infra全部清理。原套件不变；本地真实GDAL夹具专项45项、脚本门禁62项与完整Online登记通过。扩展原栅格脚本总门禁未通过：最后215项中的3个未修改原Raster模拟生命周期用例60秒超时，其他16组子门禁通过，不改原限制、不计整套通过；隔离身份Go T1不计全量IAM。固定 `dd04fb0637f381db9dfc3532c70d87135b34d21b` 的 [Hosted 39783255643／119839656541](https://github.com/pampa0629/addp/actions/runs/39783255643/job/119839656541) 已通过领域归档终审：507747毫秒，六条成果链和十四条中英文准确失败全部满足上述严格断言，普通User33／Tenant2原12权限、七Backend与默认入口Runtime同源码，全部清理通过。归档12437638231，原始online-report SHA-256 `c4273d4d51fb53173d63373554653cab8dd1566427213d765c007b3e922f0d18`；现场worker完整像元校验及原始回执已核，归档未保留TIFF，未离线重算。Linux全量等CI会话回传，不计UI或容量，不覆盖上游最长流长或汇水分区。


### D8 汇水分区持久化

独立手工 `raster-drainage-basins-d8-persistence`／`make test-raster-drainage-basins-d8-online-runner`，600秒预算，复用Hosted、NFS、同源默认入口Runtime及普通User原12权限。四条已登记多出口方向／DEM接流向／填洼接流向／全无效COG链，七类非法方向（含64位NoData相邻值）／内部伪终点／越界或无效下游／循环／非法波段／旋转网格各中英文提交，共14条准确失败链。独立逐源路径核全像元／mask、出口行列编号、相邻出口不合并、唯一归属与总数守恒；严格三方大小、COG／网格／CRS、精确扫描、当前执行实际输入血缘／Monitor、源及先前成果不变、失败零发布／新增扫描血缘与全部清理。入口、夹具及登记已实现；本地Native新增夹具专项42项（2676筛除，不是新全量）、脚本门禁62项及完整Online登记、Python CI标准登记通过，记录输入无漂移。固定 `6bf471fc2b6f8ee75fc01e2ce56e317e1375b5dc` 的 [Hosted 39797527146／119888617829](https://github.com/pampa0629/addp/actions/runs/39797527146/job/119888617829) 已通过并完成领域归档终审，297809毫秒，四保存链／十四准确双语失败、严格大小／扫描血缘及全部清理通过；原12权限、600秒和产品逻辑保持。官方归档12441159437，online-report SHA-256 `1589759d6af72b938fab53bf28d171e7fcc8d929be577403cf9368e53cad2c68`；现场全像元及原始回执／日志已核，TIFF未归档、未离线重算，不计UI或容量；Linux全量仍等CI会话回传。

### D8 上游最长流长持久化

独立手工 `raster-flow-length-upstream-d8-persistence`／`make test-raster-flow-length-upstream-d8-online-runner`，600秒预算；复用Hosted、NFS、同源默认入口Runtime、普通User原12权限。米制／国际英尺／美制测量英尺已登记方向、DEM接流向、填洼接流向及全无效六条COG链，七类非法方向码／内部伪终点／越界下游／无效下游／循环／非法波段／旋转网格各中英文提交共14条准确length任务失败。独立逐源路径全像元oracle核横纵对角米制中心距离、无入流零、汇合取最长以及开放终点保留上游长度；完整mask／原网格／CRS／Float64／NaN／COG及金字塔、严格artifact／物理／Meta相等正整数大小、精确扫描、当前执行实际输入血缘／Monitor、源及先前成果不变、失败零发布／新增扫描血缘与Runtime／NFS／部署／Infra清理均须满足。查询语言不改写旧失败；不修改产品算法、原套件、权限或跨模块产品逻辑。入口、夹具、Native recipe与手工Hosted登记已实现。本地标准Native新增夹具专项48项通过（2844项筛除不计通过），标准脚本门禁62项与完整Online登记通过，Python CI登记10项通过；记录输入无漂移。修后唯一固定 `ddd32f9536857cecc4dc40d63d356755c4ca297b`（含夹具修复 `2d6d92780`）的 [Hosted 39876682648／120163869951](https://github.com/pampa0629/addp/actions/runs/39876682648/job/120163869951) 已通过并完成领域归档终审，416469毫秒；六保存链／十四准确双语失败、上述完整像元／最长路径／米制单位／严格大小／精确扫描和当前执行血缘／Monitor／源及先前成果不变／全部清理通过。三方同源码，User33／Tenant2原12权限，600秒预算、样本及产品算法不变；夹具仅共享同几何不可变数学预期，物理成果每轮仍重读。修后标准Native专项51 passed／3032 deselected／36.86秒及脚本62项／23.525秒和登记通过，不冒充Linux全量。官方归档12472970205，online-report SHA-256 `c8c91de85e4ffae418f6dc46278ae91d13c8b4610239bc3ca3ed491114cb057c`；域报告与raw Job深等，固定源码及实际步骤／原始失败日志已核，未归档TIFF、未离线复算，不计UI、容量、加权汇流或业务DELETE。Linux全量仍等CI会话回传。


### raster-stream-order-d8-persistence

独立手工 `raster-stream-order-d8-persistence`／`make test-raster-stream-order-d8-online-runner`，600秒业务预算，复用现有Hosted／NFS、同源默认入口产品Runtime及普通User原12权限。六条COG链：已登记D8＋二值多级河网、汇流累积→重分类接分级、DEM→流向→累积→重分类→分级、填洼接同链、空河网、全无效。八类断流／非二值掩膜／有效覆盖不一致／完整网格不一致／非法D8方向（含UInt64 NoData相邻大整数）／循环／非法方向波段／非法掩膜波段各按中英文提交，共16条grade任务准确失败，反向查询不改写旧错误。

515×519完整网格中的确定性有效条带跨512块，含级别1／2／3、同最高级汇合、低级重复不升级、开放终点保留级别、合法零、原生NoData／mask／alpha／非有限覆盖。独立反向支流栈遍历与单独计数核全部像元与阈值掩膜，整幅逐像元／mask及完整COG／最近邻金字塔验证；严格保留原网格／CRS／Float64／NaN，级别不带距离单位。严格artifact／物理／Meta相等正整数大小，精确扫描父执行／用户／租户／引擎与当前执行所有实际加载输入血缘／Monitor，源和旧成果不变，失败零发布／新增扫描血缘及Runtime工作区／NFS／部署／Infra全部清理。入口／夹具／Native recipe／手工Hosted／输入影响与CI一致性登记已完成，本地Native专项48 passed／3083 deselected／5.33秒（0失败／跳过，不是新全量）、脚本门禁63项／23.076秒与完整登记、Python CI登记10项通过，记录输入无漂移；标准共享栅格脚本总入口完整退出0，20组子门禁（含原215项模拟生命周期）及隔离身份Go T1通过，输入无漂移；不累加共享重复用例、不计全量IAM／数据库集成，实际Hosted已在固定 `bde58e038751d932888bd7696046715e4ba21410` 通过并完成领域终审，Linux全量仍等专门CI会话回传；不改产品、权限或原套件，不计UI或容量。

固定Hosted [39880792263／120177404317](https://github.com/pampa0629/addp/actions/runs/39880792263/job/120177404317) 的六条COG链及十六条中英文准确失败均通过，预检与业务436228毫秒，保持600秒预算。三方大小10788／10936／10860／10845／10715／10741字节；完整像元／mask、Strahler汇合／河网闭包／零背景、精确扫描与当前执行实际输入血缘／Monitor、源及先前成果不变、反向查询不改提交语言、失败零发布扫描血缘和Runtime／NFS／部署／Infra清理均通过。七Backend与默认入口产品Runtime同源码，普通User33／Tenant2原12权限；官方ZIP摘要、解包文件、域报告与原始Job JSON和逐条失败日志已独立核验。online-report SHA-256：`b2b5637e03923dd36238122e18a6ac8c804ff3ba2c6859ddf99163c4987ed1df`。归档无TIFF，未作离线像元复算；不计Linux全量、UI、容量、加权汇流或业务DELETE。

## raster-flow-accumulation-weighted-d8-persistence

独立 `raster-flow-accumulation-weighted-d8-persistence` 已获用户授权：复用Hosted隔离生命周期、NFS、同源默认入口产品Runtime和普通用户原12权限，独立600秒业务预算。六条COG保存链为直接非负权重、全1权重、权重重分类、DEM→D8流向→加权累积、填洼接上述链、全无效；每条都核当前执行两份实际注册输入的血缘。负权重、缺失／非有限权重、网格不一致、非法D8码（含原始UInt64 NoData邻接大整数）、循环、非法方向波段、非法权重波段、累计溢出八类各中英文提交，共16条准确失败。逐源路径oracle核完整权重和／mask、合法零、自身排除、全1等价、出口守恒、单位／网格／CRS、COG／金字塔、artifact／物理／Meta相等正整数大小、精确扫描与父执行／用户／租户／引擎、当前执行两源血缘／Monitor、源及先前成果不变和全部清理。失败不发布、不新增扫描或派生血缘，反向语言查询不改提交错误；不变更原套件／权限／跨模块产品逻辑，不计UI或容量。入口与夹具已实现、本地标准门禁通过，实际Hosted未运行；Linux全量和既有Hosted仍等专门CI会话回传。

标准入口：`make test-online ONLINE_SUITE=raster-flow-accumulation-weighted-d8-persistence`；脚本门禁：`make test-raster-flow-accumulation-weighted-d8-online-runner`。仅在独占Linux Runner经Hosted准入后运行，本地不接管个人部署。夹具仅拥有本轮NFS目录、容器、注册及部署；每次重读所有已发布物理成果和源哈希，退出严格核零残留。现场完整像元检查与归档回执审计分开记账，不将未归档的TIFF称为离线重读。

独立夹具已实现并完成本地验证：标准 `make test-geopython-workflow` 过滤本夹具 **48 passed／3296 deselected／5.05秒**，真实GDAL六条COG保存链、16条中英文准确失败、完整像元／mask、单位／守恒／全1等价及成果篡改拒绝均通过，验证输入无漂移；专项日志SHA-256：`4b64f5e97eaa53a68c43c4ab39a9428faea155ec60285103bea7e2ec81909d37`。这是新增夹具专项，不将筛除计通过或冒充最终固定源码Linux全量。标准 `make test-raster-flow-accumulation-weighted-d8-online-runner` **63项／17.125秒**及完整Online登记通过，`make test-python-ci-registration` 的10项及11模块登记通过；前置34项重复覆盖不累加，组合日志SHA-256：`7150cdca0c3fd053df660539afd31af02d8be7066bdbd9c316d3f7a628510cac`。Node24.11.1下完整 `make test-raster-online-runner` 退出0，21个子门禁及末尾215项共享生命周期模拟／隔离身份Go T1通过，输入无漂移；聚合耗时574.87秒，日志SHA-256：`12705bbf3576b9b6f687dd7024a1bb64faad42d3a21abe995e55b1406bd0632d`。共享子门禁重复计数不累加为唯一测试数，不计全量IAM／数据库集成；产品代码仍复用a53690c9f的Native全量3296项固定证据。实际Hosted尚未运行，Linux全量和Hosted均沿专门CI会话回传后独立归档终审，不设定时或自行轮询。

## raster-watershed-d8-persistence

独立 `raster-watershed-d8-persistence` 已获用户授权：复用Hosted、NFS、同源默认入口产品Runtime、普通用户原12权限，独立600秒业务预算。四条直接指定点／标记重分类／DEM→流向／填洼接流向COG保存链，每条核当前执行两份实际注册输入血缘；嵌套、重复编号、无点、全无效、非法编号（UInt64 NoData相邻大整数）、覆盖不一致、网格不一致、非法方向、循环及方向／标记两个角色非法波段十一类各中英文提交，共22条准确watershed任务失败。515×519完整网格中的有效条带跨512块，包含最大安全编号、中间点、两条支流共用最终开放出口、未选有效零及NoData／mask／alpha覆盖。独立逐源完整路径oracle核全部编号／mask、点自身、原网格／CRS、COG／金字塔及单位清除；严格artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前执行两源血缘／Monitor、源及先前成果不变和全部清理；失败无发布／新增扫描血缘，反向查询不改提交错误。入口与长期夹具已实现，本地门禁通过、实际Hosted未运行；不修改原套件、权限或跨模块产品逻辑，不计UI或容量。Linux及其余Hosted继续等专门CI会话回传，不设定时、不自查或重复触发。

标准入口：`make test-online ONLINE_SUITE=raster-watershed-d8-persistence`；脚本门禁：`make test-raster-watershed-d8-online-runner`。仅经Hosted准入在独占Linux Runner运行，原普通用户权限与NFS归属边界不变；每轮重读全部源哈希与已发布物理成果，只有不可变数学预期可缓存，不缓存物理读取。现场完整像元校验与归档回执终审分开，不将未归档TIFF称为离线重读。

独立夹具标准 `make test-geopython-workflow` 专项 **55 passed／3600 deselected／7.82秒**，0失败／跳过，输入无漂移；真实GDAL四保存链、22条中英准确失败、完整编号／mask与源及旧成果保护、19类物理篡改拒绝通过。专项不计新全量，不与产品原3553项相加。日志SHA-256：`b9969555eb2c6bea5c8c354a103275cbc416fce1ba2de109b431bce15fd8b2cc`。标准 `make test-raster-watershed-d8-online-runner` 的63项脚本回归（unittest报告28.890秒）及完整Online登记通过；`make test-python-ci-registration` 的10项／11模块与前置37项登记回归通过，共享计数不累加，组合日志SHA-256：`2c8f0d45e17a15852b95d5e1e9fa4c31d8b4cea7de75d6b15e123b9c9dbe6945`。实际Node24.11.1下完整 `make test-raster-online-runner` 退出0，22组子门禁及末尾215项共享生命周期模拟／隔离身份Go T1通过，壁钟2672.09秒，输入无漂移；日志SHA-256：`d28506697d40acf7d0c6a26698d1e0071e9711fe02b0c8eeba65f1f71c5fdd03`，不计完整IAM／数据库集成。最终专项与独立脚本绑定隔离候选 `19bb583a21b4ffd5d7ffe1d5099bb57d02d58ab7`，共享总入口绑定较早候选 `3a26d888637ef598e9d8361dbd668622ca710348`。两候选之间仅其他会话矢量拓扑测试／夹具／登记及场景文档发生变化，未修改产品代码或本轮算子／夹具；更新后的共享登记另由最终独立入口复核；Runtime产品代码保持91440f2e1，原Native全量固定证据保留。独立Hosted尚未运行，Linux及其余Hosted继续等专门CI会话回传与领域归档终审；不设定时、不自行轮询或重复触发。

### D8 河段编号独立持久化

手工 `raster-stream-link-d8-persistence` 复用隔离Hosted、NFS、同源默认入口Runtime和普通User原12权限，独立600秒预算。六条COG保存链覆盖已登记双输入、累积阈值重分类、DEM／填洼接流向、空河网及全无效；八类非法输入各中英文提交，共16条准确失败。独立上溯oracle核完整编号／mask、起点行列顺序、汇合点归下游、单像元汇合出口及交叉河流独立，严格COG／网格／CRS、artifact／物理／Meta相等正整数大小、精确扫描／全部实际输入当前血缘／Monitor、源和旧成果不变及零残留；失败零发布／新增扫描血缘、查询不改提交语言。夹具、标准入口与手工CI登记已实现；本地标准Native夹具专项50项／3852筛除、独立脚本63项、Python／Online登记与完整栅格总入口通过，输入无漂移（共享重复计数不累加，筛除不计通过）；实际Hosted尚未运行，Linux由专门CI会话协调回传，不计UI或容量；不改原套件与产品权限。

### Shreve 独立持久化验收

已批准手工 `raster-stream-shreve-d8-persistence`：沿用隔离Hosted／NFS、同源默认入口Runtime、普通User原12权限和600秒预算。六条Shreve保存链加同源默认／显式Strahler两条对照，共八条COG；断流、非二值掩膜、覆盖／网格不一致、精确64位非法方向、循环、双角色非法波段及非法method九类各中英文，共18条准确失败。独立逐源头前向路径oracle核完整量级／mask、出口源头数守恒、非河网供体排除和对角交叉独立，默认／显式Strahler完整物理成果相等且符合独立原规则；严格核COG／网格／CRS、三方相等正整数大小、精确扫描／全部实际输入当前血缘／Monitor、源与先前成果不变及零残留，失败零发布／新增扫描血缘、反向查询不改提交语言。整数越界继续单列Native证据，不模拟Hosted越界或降低产品上限；不改原套件、权限或跨模块产品逻辑，不计UI或容量。本地新夹具与脚本门禁通过，实际Hosted未运行；Linux全量及实际Hosted均等专门CI会话回传，本会话不触发、轮询或设置定时。

本独立夹具本地Native专项53项、标准独立runner65项、Online／Python CI登记及共享栅格总入口24组均通过；原产品Native全量4136项固定证据保留，固定883aed7b5正式Linux全量4189项已通过，实际Hosted仍待专门CI会话回传。整数累计越界仅列Native受控上界回归，Hosted精确64位非法方向码不等同量级累计越界；完整固定证据见[栅格专题](../../../next/栅格算子体系后续专题.md#shreve-独立持久化验收)。

### D8 河网转矢量线独立 Hosted 验收

已批准独立手工 `raster-stream-to-vector-d8-persistence`，复用隔离Hosted／NFS、同源默认入口Runtime、普通User原12权限及600秒预算。三种水平单位的直接双输入，加累积阈值、DEM／填洼完整链及空河网／全无效，共八条GeoPackage保存；孤立单像元（含非河网入流）、混合孤立点、断流、非二值掩膜、覆盖／网格不一致、精确64位非法方向、循环及两个角色非法波段十类各中英，共20条准确失败。独立逐边oracle核每条真实河网边恰好一次，完整中心坐标／方向／ID间隔／零基起止行列／米制长度、汇合端点、单像元终点、跨块及交叉独立。按通用矢量save契约核GeoPackage的完整几何／字段类型／CRS、物理与Meta相等正整数大小、精确扫描与当前执行所有实际输入血缘／Monitor、源与先前成果不变及全部清理；空成果核Schema与CRS。失败零发布／新增扫描血缘，反向查询不改提交语言，不新增RasterArtifact。编号／长度与坐标数值极限继续单列Native，不模拟Hosted越界；不改原套件、权限、算法或跨模块产品逻辑，不计UI或容量。夹具、标准入口和手工CI登记已实现并通过本地准备门禁；实际Hosted尚未运行，仍由专门CI会话协调，本会话不触发、轮询或设置定时。

独立Hosted准备的标准Native专项 `make test-geopython-workflow`（`-k stream_to_vector_d8`）**158 passed／4280 deselected／5.52秒**，0失败／跳过，输入无漂移；其中新增夹具46项，涵盖八份真实GDAL GeoPackage、20条双语准确失败、23类物理篡改拒绝及源／先前成果不变。基于修正提交5419a73ad，候选树 `2290b776308c6e0c86940a4a6f0557a48f598906`，原始日志SHA-256 `89cf9b3e1ae6efbe5dbb9c3cc8c0faeb1d4afc409da51eb8ce7f48b6f6f9157e`；筛除不计通过，不把专项与旧全量相加。三种单位直接样本各21条线／444条边／465个顶点，跳过终点ID2；阈值样本34条线／3542条边，DEM为7条／3612条，填洼链为5条／2582条，空河网与全无效均零要素且保留Schema／CRS。

Node24.11.1下 `make test-raster-stream-to-vector-d8-online-runner` **63项／20.403秒**及完整Online登记通过，原始日志SHA-256 `a5a53ce92de30f6d24e240ea662b64d2e84b181b958ff72c832be4d700c4e6fc`。完整 `make test-raster-online-runner` 退出0、输入无漂移，耗时744.59秒，25组Python子门禁及原215项生命周期、隔离身份夹具Go T1通过（共享回归重复，不求和作唯一测试数）；原始日志SHA-256 `b52e1fa5f190351d166207ca1186873a9322c9a1f1184a243412926ae6c2c496`。这些是本地准备与Native证据，不计正式Linux全量、真实Hosted用户／扫描／当前执行血缘／Monitor及清理通过；数值极限继续单列Native，不模拟Hosted越界。实际Hosted仍等专门CI会话协调和完整原始归档领域审计，无自行触发／轮询／定时。

## raster-snap-pour-points-persistence

已获用户授权新增独立手工Hosted：复用既有NFS、同源默认入口产品Runtime和普通用户原12权限，独立600秒业务预算。8条COG保存链包括米／国际英尺／美制测量英尺直接吸附、零半径、标记重分类、DEM接流向累积吸附、填洼接上述链，以及流向→累积→吸附→汇水区。13类各中英文提交，共26条失败链：碰撞、重复编号、全零无点、全无效、非法计数、非法编号、覆盖不一致、网格不一致、负半径、两个角色非法波段、旋转网格及吸附后嵌套；嵌套须由原汇水区节点失败，吸附节点先成功。整数非法样本包括原UInt64 NoData相邻大整数；非有限半径／单位溢出等极限保留Native证据。

独立逐候选核完整编号／mask、真实CRS米制圆边界、距离／行列决胜、跨分水岭／无效区、零与编号保留；严格COG／原网格CRS／单位清除，artifact／物理／Meta相等正整数大小，精确扫描父执行／用户／租户／引擎、当前执行全部实际输入血缘／Monitor，源与先前成果不变和全部清理。失败无发布／新增扫描／输出血缘，反向查询不改提交语言。入口与夹具已实现，本地最终标准Native专项312项（新增夹具53项，4538筛除）、独立脚本63项与完整Online登记通过；原栅格总入口固定准备候选实跑通过，最终Worker独立性另经Native／独立入口覆盖；实际Hosted未运行，不计UI或容量，不改产品逻辑／权限／原套件，Linux与其余Hosted等专门CI会话回传，不自行轮询或触发。

标准入口：`make test-online ONLINE_SUITE=raster-snap-pour-points-persistence`；脚本门禁：`make test-raster-snap-pour-points-online-runner`。现场重新读取完整物理成果与源哈希，只有不可变数学预期可缓存；现场完整像元校验与原始归档终审分别记账，不把未归档TIFF称为离线重读。


## raster-subcatchments-d8-persistence

已确认新增独立手工Hosted：复用Hosted／NFS／同源默认入口产品Runtime与普通用户原12权限，独立600秒预算。7条COG链：非嵌套增量分区、相同输入的原完整汇水区对照、多层嵌套与并列支流、标记重分类、DEM接流向、填洼接流向，以及流向→累积→真实移动点的吸附→增量分区。两条非嵌套成果须完整像元一致；嵌套按下游第一个指定点归属且点自身归本点，保留用户编号、未选有效零和原无效覆盖。

14类各中英文，共28条增量失败：重复编号、无点、全无效、非法编号、覆盖／网格不一致、非法方向码、内部伪终点、越界／无效下游、循环、两个角色非法波段及旋转网格；再加原完整汇水区对同一嵌套输入的两条中英文准确拒绝，共30条失败。包含原UInt64 NoData相邻非法大整数。独立逐源路径oracle核全部编号／mask、跨块、多波段、首点／自身、唯一归属和像元数守恒；不读取产品拓扑标签。完整COG、最近邻金字塔、原网格／CRS／单位清除、artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前执行所有实际输入血缘／Monitor、源及先前成果不变及零残留均严格核对。失败须准确节点和提交语言、零发布／新增扫描或输出血缘，反向查询不改旧失败。

本地最终标准Native专项 `make test-geopython-workflow`（`-k subcatchments`）**247 passed／4912 deselected／17.46秒**，0失败／跳过，含新增夹具73项；输入无漂移且无新增Python原生崩溃报告。七份真实COG、30条双语准确失败、完整物理篡改拒绝和真实stdin Worker空目录独立运行均已验；专项筛除项未执行，不计新全量、不与原5073项全量累加。候选树 `adad1eebdd2a117ea800cc20bb5a1bb70d70154d`，日志SHA-256 `382fa9c8de7462820a457bb70c30a27f80960b45accf3e37ca46b4e17d092312`。最终 `make test-raster-subcatchments-d8-online-runner` **63项／38.524秒**及完整Online登记通过，同候选树且输入无漂移，日志SHA-256 `9918247fc8fd72fd185cf4b891226cbc96b8855d9297cfde4a52a9522a171089`。

实际Node24.11.1下完整 `make test-raster-online-runner` 退出0，27组Python子门禁及隔离身份夹具Go T1通过，壁钟1058.90秒，输入无漂移；共享计数不累加为唯一测试数，不计全量IAM／数据库集成。固定准备候选 `a437da2bdfcb277c61ed6a1c5234b18c12e4f9a3`，日志SHA-256 `2b606775546f2a666114f2f7901de906c51fbde23d2d4bf4801772177be1f490`。其后仅本套件错误描述／测试命名及Worker独立性登记收紧，由上述最终Native／独立入口复核，原套件断言保持；PythonCI登记另通过42项与10项及11模块检查。新脚本的变更→engines选择、Native recipe、独立根入口和手工Hosted Job已同次登记。

以上为本地准备及Native证据。实际Hosted尚未运行，仍由专门CI会话协调并回传原始归档终审；Linux全量及其余Hosted继续等待回传，不自行触发、轮询或定时。未计平台身份／扫描／血缘／Monitor与真实部署清理通过，也不计UI或容量；数值极限仍单列Native。


## raster-flow-accumulation-area-d8-persistence

用户已批准独立手工D8汇流面积持久化验收：复用Hosted隔离生命周期、NFS、同源默认入口产品Runtime及普通用户原12权限，独立600秒业务预算。八条COG保存链为米制／国际英尺／美制测量英尺方向接area、同一米制方向的默认count／显式count对照、DEM→流向→area、填洼接上述链及全无效area。519×515矩形网格、显式第二数据波段，覆盖跨512块、多出口、孔洞、合法零、部分透明、孤立开放终点及边界非零方向。

非法方向码（含原始UInt64 NoData邻接大整数）、内部伪终点、越界指向、无效下游、循环、非法波段、旋转网格与非法measure八类各中英文提交，共16条准确失败；反向查询不改提交语言。独立逐源路径核完整像元／mask、area包含自身、平方米m2、count不计自身且清空单位、两count对照一致与出口面积守恒；严格核原网格／CRS、Float64／NaN、COG及最近邻金字塔、artifact／物理／Meta相等正整数大小、精确扫描的父执行／用户／租户／引擎、当前执行实际注册输入血缘／Monitor、源与先前成果不变。失败零发布／零新增扫描和派生血缘，全部退出清理和零残留为必需断言。面积溢出／下溢、累计溢出与精确计数上限继续单列Native数值极限证据，不记Hosted已验。

标准业务入口`make test-online ONLINE_SUITE=raster-flow-accumulation-area-d8-persistence`；本地准备门禁`make test-raster-flow-accumulation-area-d8-online-runner`；仅手工Hosted，不参加夜间自动运行，不改原套件、产品逻辑、权限或跨模块实现，不计UI或容量。夹具、标准入口和独立手工CI登记已实现；最终本地Native专项184项（含新增夹具52项、5217项筛除）／18.65秒，独立脚本门禁63项／17.992秒与完整Online登记通过；Node24.11.1下共享栅格脚本总入口实跑通过，具体候选版本与证据见后续专题。实际Hosted未运行，由专门CI会话协调；Linux全量和其余Hosted继续等回传，不计平台扫描／血缘／Monitor、UI或容量通过。数值极限继续单列Native。


## raster-distance-to-stream-d8-persistence

用户已批准独立手工D8到河网沿流距离持久化验收，复用Hosted／NFS、同源默认入口产品Runtime、普通User原12权限，独立600秒预算。米／国际英尺／美制测量英尺方向＋河网、累积阈值、DEM、填洼、空河网及全无效共8条COG保存链；519×515矩形网格与显式第二波段覆盖跨512块、多出口、对角交叉、孔洞及alpha／mask。独立逐源路径核首次命中、河网自身零、未到达NoData及输出有效覆盖，严格核米制单位、完整COG／最近邻金字塔、三方相等正整数大小、精确扫描、当前执行实际输入血缘／Monitor、源旧不变及全部清理。

非法码（含原始UInt64 NoData邻接非法整数）、内部伪终点、图外／无效下游、循环、断流、非二值掩膜、覆盖／网格不一致、旋转网格及两个角色非法波段共12类各中英文，合计24条准确失败；包含河网之外或命中之后的非法路线。失败零发布／新增扫描／派生血缘，反向查询不改提交语言。长路径稳定性、距离溢出及极小步长继续单列Native数值证据，不记Hosted已验。不改产品、权限、其他模块或原套件，不计UI／容量。

夹具、脚本、标准入口及独立手工CI登记已实现；最终本地标准Native专项260项（含新增夹具57项、5434筛除）／10.55秒及独立脚本63项／23.327秒通过，完整Online登记通过；Node24.11.1下共享栅格脚本总入口实跑通过，具体候选与日志见后续专题。产品算子源码保持47a390210。本轮未运行实际Hosted，Linux全量和其他Hosted继续等专门CI会话回传，不计平台扫描／血缘／Monitor、UI或容量；数值极限仍单列Native，不触发、轮询或设置定时。


## raster-hand-d8-persistence

已获用户批准：独立手工raster-hand-d8-persistence，复用Hosted／NFS、同源默认入口Runtime和普通用户原12权限，独立600秒预算。三种高程单位与水平CRS交叉的直接HAND、累积阈值、DEM、填洼、空河网及全无效共8条COG保存链，三输入显式数据波段、519×515矩形网格，覆盖跨512块、多出口、孔洞、平坦零、负高程与alpha／mask。独立逐源路径oracle核首次河网基准／完整高程差及mask、未到达NoData；严格核完整COG／最近邻金字塔、网格／CRS／米制、三方相等正整数大小、精确扫描和当前执行全部实际输入血缘／Monitor、源及先前成果不变与全部清理。

命中前／命中后／空河网逆坡及填洼流向混用原DEM，非法码／内部伪终点／图外与无效指向／循环／断流，非二值河网、两角色覆盖与网格不一致、旋转网格，三个角色非法波段、非法高程单位及单位冲突共21类各按中英文提交，合计42条准确失败。失败零发布／新增扫描／派生血缘，反向查询不改提交语言。十万像元长路径、原始64位高程相邻差值及溢出／下溢继续单列Native，不计Hosted数值极限、UI或容量，不修改产品、原套件、权限或跨模块逻辑。

夹具、标准入口与独立手工CI登记已实现，通过本地Native专项389项（含新夹具76项）、独立脚本63项及登记检查；实际Hosted尚未运行，Linux全量与其余Hosted继续等专门CI会话回传。本会话不手动触发、轮询或设置定时。专项不计新全量，不与原6007项全量或共享测试相加；不计真实普通用户、平台扫描／血缘／Monitor、UI或容量。


### DEM 平均曲率独立 Hosted 验收

用户已确认独立手工`raster-curvature-persistence`，复用Hosted／NFS、同源默认入口Runtime、普通用户原12项权限及独立600秒业务预算。三种高程单位与不同水平CRS交叉的直接曲率、填洼接曲率、重分类接曲率、全无效、无完整有效邻域和小尺寸不足3行／列共8条COG保存链；519×515矩形多波段样本显式第二数据波段，覆盖跨块、倾斜平面、凹凸／鞍面／中心脉冲、合法零、内嵌mask、NoData和透明／部分透明。独立九点设计矩阵最小二乘和解析曲面核完整像元及mask、九点等权／自由常数项、凸正凹负、单位1/metre和一圈无效邻域；金字塔按实际尺寸逐层核验，小图不强制不存在的层级。

非法波段、alpha作高程、复数高程、旋转／南向网格、经纬度CRS、缺CRS、缺定位网格、非法单位、单位冲突、未应用scale、未应用offset共12类，各按中英文提交，共24条准确失败；须在曲率节点以准确领域错误失败，反向查询不改提交语言，失败零发布／新增扫描／派生血缘。严格核COG、原网格／CRS、Float64／NaN、三方相等正整数大小、精确自动扫描、同用户和当前执行实际输入血缘／Monitor、源和先前成果不变及全部清理。原生64位相邻高程、极端数值和不可表示步长继续单列Native；不计UI或容量，不改产品、原套件、权限或跨模块逻辑。

独立夹具、场景、标准入口、变更选择、独立手工Hosted与CI登记均已实现；产品算法、权限和原套件保持。本地标准`make test-geopython-workflow`配合`PYTEST_ADDOPTS='-k curvature --durations=10'`为 **202 passed／6188 deselected／11.99秒**，0失败／跳过，JUnit计数一致；其中151项既有曲率用例与51项新夹具用例包含于同一专项，不相加为全量。八条真实GDAL／COG链、24条中英准确失败、完整像元／mask、九点等权／自由常数项、解析凹凸／鞍面／平面／中心脉冲、三单位与水平CRS交叉、边缘与无效邻域、真实金字塔及其绕过布局检查的错误像元反例、源旧保护、全部失败workspace清理和stdin Worker空目录独立运行均已核。

最终`make test-raster-curvature-online-runner` **64项／29.147秒**与完整Online登记通过，覆盖三方大小、精确扫描父执行／用户／租户／引擎、同次执行实际输入血缘、失败零发布／扫描／血缘、冻结语言、源旧不变及Hosted准入／部分启动／业务失败／收尾失败／清理反例；这些属于确定性回归，不替代真实普通用户／平台链路。`make test-python-ci-registration`通过46项、10项与11模块Python／Online登记，共享计数不累加。

最终Native候选树`939fc7b147e50358c15bffd8338aed93223af30e`，原始日志SHA-256：`5fa8caf58594ec27ce22a10fa9dcdf3cd4ef690200cfd9a7eb5a1e2c9634e50d`。最终独立runner候选树`939fc7b147e50358c15bffd8338aed93223af30e`，原始日志SHA-256：`c4280a2c79fad3ec3f9b60cd676be3743ee0f8877c8e1524c9434efd15e3dbf1`。最终登记候选树`939fc7b147e50358c15bffd8338aed93223af30e`，原始日志SHA-256：`cc00eccd8b0325cc7b9cb725255b5a00896364deee2fd623c5b2bc1bccd42033`。

实际Node24.11.1下完整`make test-raster-online-runner`退出0，31组Python子门禁及既有隔离身份夹具Go T1通过，壁钟883.57秒；共享计数不累加唯一用例数，Go T1不计全量IAM或数据库集成。聚合候选树`be585e3ca47ee9fd34e785a20562570f1285369a`，原始日志SHA-256：`84c6c991339acbc3b6962fe3fb37aa514928c2931db79623dc69dcab1d0a717a`。本会话其后仅收紧DAG中间节点的显式波段／引用断言并新增两项直接金字塔反例，最终Native、独立runner与登记已复核，不把较早聚合候选外推为后续整树复跑。

COG原生新建金字塔按GDAL自定义网格的整数减半规则核精确尺寸，519×515样本为259×257、129×128、64×64，小图为零层；每层再独立核完整最近邻像元及mask，依据GDAL `frmts/gtiff/cogdriver.cpp`及真实文件事实修正了首轮向上取整的错误期望，未降低像元误差阈值或删去布局断言。所有成功门禁跟踪输入无漂移、无新增Python原生崩溃报告。

实际Linux全量及Hosted仍待专门CI会话协调；本轮未触发／轮询／重跑或设置定时，不计真实T4通过、UI、容量或Hosted数值极限。原固定全量与验收证据分别保留。


### DEM 剖面曲率独立 Hosted 验收

独立手工入口`raster-profile-curvature-persistence`已准备，复用既有Hosted／NFS生命周期、同源产品默认入口Runtime、普通User原12权限和独立600秒业务预算；仅手动登记，不加入nightly，不取消排队任务。没有修改产品计算逻辑、权限、其他模块或旧套件。

固定12条COG成功链：直接米制／国际英尺／美制测量英尺高程、填洼接profile、重分类接profile、完全水平平地、非平地零梯度残差、全无效、有有效像元但无完整邻域、小图，以及同源默认mean与显式mean两条对照。519×515、矩形投影网格、显式第二数据波段的样本覆盖跨块、凸凹／鞍面／倾斜面、极小非零坡度、NoData／mask／透明和部分透明；3×3残差与2×7小图不虚构金字塔。独立九点最小二乘与第一／第二基本形式方向商核完整像元／mask，保留`RTOL=1e-10`、`ATOL=1e-11`；平地严格0、其他零梯度严格NaN，mean另用基本形式迹核验，并逐字节比较默认／显式mean。

13类中英文共26次准确失败：非法数据波段、alpha波段、复数、旋转、南向、地理CRS、缺CRS、缺网格、非法单位、单位冲突、未应用scale、未应用offset、非法method。准确匹配同次执行的curvature任务和双语原因；反向查询不得改写提交语言。成功严格核完整COG／最近邻金字塔、原网格／CRS、Float64／NaN与`1/metre`、artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前执行实际输入血缘／Monitor；失败必须零发布／新增扫描／派生血缘。每次核源及先前成果不变，生命周期覆盖所有退出与零残留。

本地标准`make test-geopython-workflow`专项（`PYTEST_ADDOPTS='-k curvature --durations=10'`）**411 passed／6290 deselected／54.66秒**，0失败／跳过；其中本套件58项真实GDAL回归覆盖12条保存链、26次双语拒绝、完整像元／金字塔和错误算法／平地／零梯度／极小坡度／mean混用的污染反例。原353项曲率相关回归保持通过，筛除项不计为全量通过。`make test-raster-profile-curvature-online-runner` **66项／29.659秒**及完整Online登记通过，包含三方大小、精确扫描／实际血缘、冻结语言、源旧保护和隔离生命周期的失败反例。`make test-python-ci-registration`通过46项、10项及11模块Python／Online登记，共享计数不累加。

Native候选树`83c8384f0ae808f1d9947b9126ded49c72289797`，原始日志SHA-256：`996ec5cd96dbec2c3b45c8202e2552ace781fab54dd48a0a103ae06b40364ded`。最终独立runner及登记候选树`6bac9fd9e055f4f71a2984c98f916ebb0f0f0797`，runner日志SHA-256：`c5e811ef319dd9a629c8c869d80023935ea1bdfafb60a57c50ca08c6a041351a`；登记日志SHA-256：`b7052da03034348a428324e59c3e9c538dfa38b7b4bd761f5dc9ab9a48a1ef2d`。

实际Node24.11.1下完整`make test-raster-online-runner`退出0，32组Python子门禁及隔离身份夹具Go T1通过，壁钟1105.79秒；共享计数不累加唯一用例数，Go T1不计全量IAM或数据库集成。聚合候选树`83c8384f0ae808f1d9947b9126ded49c72289797`，原始日志SHA-256：`99efb31e84d2d301a66e561b849913343df26ca141565032043e666bbd8a7250`。聚合证据绑定其固定输入；最终runner与登记另已覆盖同期其他会话提交后的共享注册，Native与栅格标准recipe及本套件数值输入一致，不将较早聚合外推为后续整树复跑。所有成功门禁跟踪输入无漂移、无新增Python原生崩溃报告。

实际Hosted由专门CI会话协调，本轮未手动触发、轮询、重跑、取消或设置定时；新套件的Linux全量与真实普通用户T4尚未回传，不计UI、容量或Hosted数值极限，数值极限继续单列既有Native证据。原产品cb2的Linux／Platform固定证据和本地Parquet失败记录分别保留。
