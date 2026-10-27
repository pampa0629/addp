# ADDP 脚本：Online成果与栅格

[返回脚本导航](../README.md)

专项入口：[Manager 内部产物与内容边界](#manager-内部产物与内容边界) · [栅格工作流](#栅格工作流) · [完整场景容量校准](#完整场景容量校准) · [分类蚕食容量](#分类蚕食容量) · [D8 河网转矢量线](#d8-河网转矢量线)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

## Manager 内部产物与内容边界

`manager-internal-artifact-lineage` 使用 `scripts/test/online-hosted-manager-gate.sh` 管理 GitHub Hosted Ubuntu x86_64 临时部署：从零建立 Infra、最小权限登录身份与 Business MinIO Engine Instance，启动 System、Gateway、Meta、Manager、Monitor、Console 和 PointCloud、Document、Model3D Runtime；无需永久账号或自备 Runner。Business owner 确定性生成 LAS、带 PNG 贴图的 DAE/3DS，并发布已跟踪的 3 页 PPTX；不依赖本机被忽略的数据目录。suite 经同一次真实 Meta scan，通过 Manager owner 入口生成 COPC、PDF 和 GLB，核对 Manager/Monitor 的 `addp.lineage-facts/v1`、Range 读取、GLB 内嵌纹理与非空网格、Console 模型加载，以及 PPTX 翻页后引擎刷新不重复生成。结果和任务仅按本轮捕获 ID 经 Manager API 删除，预览偏好恢复原值；生命周期在所有退出路径销毁当次业务容器、网络、数据卷和 Infra，校验零残留。确定性测试和真实验收分别使用 `make test-online-runner` 与 `make test-online ONLINE_SUITE=manager-internal-artifact-lineage`；首次 Hosted T4 成功前只允许手工触发，不计为 T4 通过。

该 Hosted suite 同时覆盖 Meta 内容边界：使用 `business/scripts/online-content-boundary-fixture.py` 为 MinIO 与临时 NFS export 生成相同的两组 Shapefile、同名异类文件及范围外记录，分别从目录和显式 refs 入口执行 basic→deep→basic。`content-boundaries.json` 保存执行 ID、item ID/fingerprint、父节点与 refs，并验证再次 basic 保留深度事实、组件不重复登记及范围外记录不变。NFS fixture 由 `business/scripts/online-manager-nfs-fixture.sh` 独占管理，仅允许 GitHub Hosted Ubuntu，使用系统 `nfs-kernel-server` 并将只读 export 限于回环客户端；拒绝既有 export，失败也必须清理本轮 export、临时数据与服务。个人 macOS NFS 不在该脚本操作范围。确定性验证使用 `make test-manager-online-runner`。2026-10-15，提交 `935dabe02` 的 [Hosted T4 38660099411](https://github.com/pampa0629/addp/actions/runs/38660099411) 已真实通过四个边界场景及原有完整 suite；归档的 `content-boundaries.json` 与主报告一致，12 次边界扫描均成功，普通 User 身份和五个服务构建版本一致，生命周期清理通过、Infra 零残留。

组件收敛扩展沿用同一套件、权限和生命周期，保留上述四个空范围场景，再增加 NFS／MinIO 各自的目录与 refs 既有记录场景。每个新范围先用精确 `targets` 登记十条独立 single；后续 basic→deep→basic 均使用 `force=false`，目录仅收敛被两组 Shapefile 认领的六条组件，refs 仅收敛指定组合的三条组件。校验退役详情 404、主项身份与父节点保留、范围外及未认领记录不变，`addp.content-boundaries/v2` 报告记录准备执行、被移除的 ID／fingerprint 和保留记录。共享生成器输出 41 个文件，既有 NFS、MinIO 清理路线不变；标准门禁和 Hosted Job 继续覆盖八个场景。2026-10-15，提交 `468df57af` 的 [Hosted 38676830266](https://github.com/pampa0629/addp/actions/runs/38676830266) 独立边界报告八项 passed，24 次边界扫描和四次准备执行均有 Worker success 终态，18 条被认领组件清理及详情 404、主项身份与无关记录保留断言通过。五个服务构建提交一致，退出清理 passed、Infra zero_residuals；后续 3DS 转换失败使完整 suite 为 failed，主报告没有完整 suite_report，因此本次只计数据项边界领域通过，完整套件仍须对应领域修复后复验。

## 矢量与栅格成果

矢量 PMTiles 工作流验收在 `online-t4-gates.yml` 手工选择 `vector-pmtiles-workflow`；`online-hosted-vector-pmtiles-gate.sh` 复用共享 Hosted 生命周期和真实 GeoPython 产品镜像，Business 夹具仅拥有本轮独立 MinIO。通过普通 User 的 Develop 执行两点 GeoJSON 纯几何 create、重复 create 冲突保护、50 面 Shapefile 精确字段 replace，核对稳定目标身份、自动扫描及当前血缘；真实 Console 核对 min_zoom=0、no-store、原生瓦片哈希、要素数和地图。离线入口 `make test-vector-pmtiles-online-runner` 纳入平台聚合，真实 GDAL oracle 纳入 `make test-geopython-workflow`。成功与失败均须清理，完整验收边界见[测试与验收规范](../../docs/spec/addp测试与验收规范.md)；登记入口不代表 Hosted 结果已经通过。

### 栅格工作流

#### 入口、身份与清理

栅格工作流验收在 `online-t4-gates.yml` 手工选择 `raster-workflow`，由 `online-hosted-raster-gate.sh` 复用 Hosted 生命周期和标准 Runtime 启动入口。此 suite 不登记 schedule，也不得在当前开发部署运行。

Business owner `online-raster-minio-fixture.py` 管理两套 tmpfs MinIO，凭据只存于 owner-only 临时文件；目标 Engine 使用独立普通写入账号，目标侧 root 仅用于夹具管理与物理核验。独立 Provisioner 注册源、目标 Engine 后从业务进程移除 Provisioner Token 与数据库密码。

退出时清理业务容器、Runtime 镜像、registry、Infra 数据卷和凭据，清理失败使门禁失败。`make test-raster-online-runner` 验证场景与生命周期，归入 `make test-online-runner` 和 `make test-platform`；独立物理校验器的真实 GDAL 回归归入既有 `make test-geopython-workflow`。完整场景要求见[正式测试场景](../../docs/spec/testing/scenarios/Manager与空间成果.md#raster-workflow)。

#### 主场景与失败断言

普通 User 通过 Develop 执行栅格工作流，验证 create、重复 create 失败不改目标、replace、原始像元和完整 COG 布局、自动 Meta scan/血缘，以及同一 User 的 Console Monitor 状态与源目标卡片。

批量切片先以 DEFLATE create，再以 NONE replace，保留无关文件；目录拒写场景精确拒绝第二片写入，要求父执行失败，仅首片进入稳定输出、自动扫描与 Meta 目录，且已有成果字节不变。

发布后清理故障复用 Business-owned 存储代理：真实 PUT 成功后按上传哈希唯一定位临时 COG，保留原文件，在私有 sidecar 路径触发删除失败；分别核对首片与四片已确认时的失败清单、稳定输出、新自动扫描、原有成果哈希及工作目录清理。代理及其 Docker socket 仅属于 Hosted 夹具，故障事件保存在不归档的 owner-only 目录，产品无故障开关。

普通成果与切片共用唯一严格大小断言：artifact、物理对象和 DataItem 的大小须为相等的正整数；按执行 ID 和目标文件名归档每次已确认写入的三方大小，保留覆盖前记录，未确认或未发布片不进入大小证据。

#### 固定版本证据

原 suite 的四种单波段聚合、多波段 mean、旋转网格多波段 sum、波段选择、孔洞／分离面片、旋转网格与四邻接／自接触轮廓、批量 create/replace、拒写及两类发布后清理故障已通过[Hosted T4 38589484655](https://github.com/pampa0629/addp/actions/runs/38589484655)，运行版本 `44e246a5a`；最新证据与边界见[栅格算子体系后续专题](../../docs/next/栅格算子体系后续专题.md)。

原成功与失败场景均保留，总计 55 轮浏览器、40 份最终 COG，预算为 1740 秒。报告保存 45 次已确认写入的三方正整数大小、14 次独立切片自动扫描、当前 derive、只读轮廓及清理证据。

以下细项均属于上述固定版本基线：

- 轮廓复用原三波段源，以 `bands=[2]` 验证 65,528 个有效像元及完整覆盖位置，区分默认并集与全部波段交集。
- 内部孔洞场景复用区域外裁剪 COG，核对 53,247 个有效像元、两个面片和一个孔洞，并确认输入成果字节不变。
- 旋转场景使用两个非零旋转项及列行不同分辨率的三波段源，按完整逆变换核对 65,532 个有效像元。
- 四邻接与自接触场景使用同一双波段源，显式选第 1／2 波段，分别核对四个角点相邻面片及一个面片中的两个角点相接孔洞，保留 4／14 个有效零值像元。

### 完整场景容量校准

#### 环境与计算矩阵

完整场景容量校准单独手工选择 `raster-workflow-capacity`，标准业务入口为 `make test-online ONLINE_SUITE=raster-workflow-capacity`，仅允许上述一次性 Hosted 部署运行。`online-hosted-raster-capacity-gate.sh` 复用同一产品镜像启动与身份校验，Runtime 为 4 CPU quota／2 GiB／无额外 swap；Business `online-raster-capacity-fixture.py` 使用当次磁盘 MinIO，下载固定长度与 SHA-256 的完整 Sentinel-2 B02/B03/B04/B08，并完整核对组合源。

管理员正式 API 配置 64／128／256 MiB 缓存与运行额度 1／2，每组两条四波段计算／average／COG 工作流，共两轮、12 组、24 条链；每组新建 Runtime，核对策略应用版本、实际运行及等待数量、cgroup 峰值、OOM 和容器进程 VmHWM 之和。耗时只计提交至父执行终态，不含独立物理 oracle 与 Meta 扫描；顺序在第二轮反转，不清空宿主机文件缓存。

完整成果像元、mask、四层金字塔、COG 布局、源哈希、artifact／对象／DataItem 正整数大小、自动扫描、血缘和 Monitor 全部通过后才计为通过。每组清空目标对象，退出核对全生命周期清理；失败也归档当组采样。

#### 目录镶嵌与通过条件

目录镶嵌负载：同一完整四波段源无损分为四片，通过普通 User 的 Manager `raster_mosaic_generation` 正式创建／执行／读取入口，以 leaf 并发 1／4 各两轮生成四份 COG 和 5490×5490 全局总览；固定 64 MiB 缓存、运行额度 1，每组重建 Runtime。仅容量身份增加 Manager 创建／读取权限；原 Raster 身份不变。

独立 oracle 核对七份完整文件、明确的零字节目录标记、全部四波段像元／mask／金字塔、源哈希，以及 whole 物理总大小／对象 stat 总大小／Meta DataItem 正整数大小相等，28 次自动扫描均须互异。

原 24 条链与新增四次目录任务全部完成、清理通过后才能声明扩展容量验收通过；旧容量证据仅覆盖原计算负载。

#### 门禁与结论边界

Native oracle 自动发现于 `make test-geopython-workflow`，身份夹具与场景回归纳入 `make test-raster-online-runner`，CI 登记同步核对。业务预算 10800 秒，容量专属测试会话 4 小时，原 Raster suite 的 1740 秒与普通 30 分钟测试 Token 均保持原值。

新入口不登记夜间调度，结果只支持该工作负载和实际测量条件，不能直接改变默认资源策略。


### 独立持久化专项

各专项独立手工触发，不改变已有套件、权限或跨模块产品逻辑。场景要求由下列正式场景维护，固定版本结果与未覆盖范围统一见[验收基线](../../docs/next/栅格算子体系后续专题.md#二当前验收基线)；入口登记、早期准备和真实 Hosted 分别记账，不外推为当前 HEAD 通过。

`make test-online ONLINE_SUITE=raster-focal-statistics-persistence` 是邻域统计独立手工 Hosted，复用原普通用户 12 权限、NFS 和同源默认入口产品 Runtime；600 秒业务预算验证六条 COG 保存链、跨块完整像元／mask、严格三方大小、精确自动扫描、当前执行血缘及零残留，非法波段／窗口失败不得发布或新增扫描。确定性脚本入口为 `make test-raster-focal-statistics-online-runner`，并入原栅格 Runner；真实 GDAL 回归沿现有 Native 入口。固定版本 `fe0d7cc8e` 的[独立 Hosted](https://github.com/pampa0629/addp/actions/runs/39228689716/job/117981136802) 已通过并经归档审计，预检／业务 359770 毫秒，完整结果见[邻域统计正式场景](../../docs/spec/testing/scenarios/Manager与空间成果.md#raster-focal-statistics-persistence)；同源 Linux 全量 1251 项及 Platform 均通过。

`make test-online ONLINE_SUITE=raster-zonal-statistics-persistence` 为整数栅格分区统计的独立手工 Hosted T4；600 秒预算，复用 NFS 与普通用户原 12 权限，验证四条完整 CSV 保存链、有限预览、严格物理／Meta 大小、自动扫描、血缘与两条非法参数失败清理。不新增 RasterArtifact；实现与证据边界见 [Manager 与空间成果](../../docs/spec/testing/scenarios/Manager与空间成果.md#raster-zonal-statistics-persistence)。

`make test-online ONLINE_SUITE=vector-rasterize-persistence` 为二维面矢量栅格化的独立手工 Hosted T4；600 秒业务预算，复用 NFS 与普通用户原 12 权限，验证三条 COG 链和非法字段失败清理。实现与证据边界见 [Manager 与空间成果](../../docs/spec/testing/scenarios/Manager与空间成果.md#vector-rasterize-persistence)。

`make test-online ONLINE_SUITE=vector-zonal-statistics-persistence` 为矢量分区统计独立手工 Hosted；原普通用户 12 权限、NFS 与同源默认产品入口、独立 600 秒预检／业务预算，验证中心覆盖、全部触及＋自定义前缀、重分类→转面→统计三条 GeoPackage 保存链和非法波段／列冲突失败清理。严格核对完整几何／属性／字段类型／计数／统计／null、物理与 Meta 大小、扫描归属、当前执行全部实际输入血缘及零残留。脚本回归入口 `make test-vector-zonal-statistics-online-runner` 并入栅格 Runner，真实 GDAL oracle 沿已有 Native 门禁；仅登记入口不代表 T4 通过，实际结果按上述固定版本验收基线核对。场景要求见 [Manager 与空间成果](../../docs/spec/testing/scenarios/Manager与空间成果.md#vector-zonal-statistics-persistence)。

邻近距离的独立手工入口为 `make test-online ONLINE_SUITE=raster-proximity-persistence`：复用 Hosted／NFS／同源产品镜像和原 12 权限，米制多波段与英尺制 UInt32 两条 COG DAG，完整距离／mask／单位／网格、严格三方大小、精确扫描、当前执行单源血缘、源与先前成果不可变及失败零残留；独立 600 秒预算；早期准备记录为 16 项真实 GDAL 夹具、51 项脚本门禁及 CI 登记验证通过，该记录未标注源码版本，不作为当前 HEAD 的通过证据。场景要求见[邻近距离正式场景](../../docs/spec/testing/scenarios/Manager与空间成果.md#raster-proximity-persistence)，后续固定版本 Hosted 结果按上述验收基线核对。

### 分类蚕食容量

独立手工入口为 `make test-online ONLINE_SUITE=raster-nibble-capacity`，仅在专属 GitHub Hosted Linux x86_64 部署运行。[Hosted 生命周期脚本](../test/online-hosted-raster-nibble-capacity-gate.sh)统一管理准入、同源默认入口产品 Runtime、NFS 和资源限制，不在个人开发环境执行。

本地确定性入口为 `make test-raster-nibble-capacity-online-runner` 与 `make test-geopython-workflow`。容量矩阵、资源观测、策略恢复和固定版本证据统一见[分类蚕食容量正式场景](../../docs/spec/testing/scenarios/Manager与空间成果.md#分类蚕食容量raster-nibble-capacity)；工作目录、NFS／部署及 Infra 的零残留清理是通过前提。确定性门禁不替代 Hosted 容量验收，结果不外推为 UI、任意影像或稳定吞吐认证，也不据此改变默认资源策略。

### D8 河网转矢量线

独立手工入口为 `make test-online ONLINE_SUITE=raster-stream-to-vector-d8-persistence`，复用隔离 Hosted、NFS、同源默认入口产品 Runtime 和普通 User 原 12 项权限。Hosted 触发与证据核对由专门 CI 会话协调；入口登记与本地准备结果不等于 Hosted 已执行或通过。

本地准备使用 `make test-raster-stream-to-vector-d8-online-runner`，该入口由 `make test-raster-online-runner` 聚合；真实 GDAL 夹具进入既有 `make test-geopython-workflow`。完整保存链、准确失败、工作目录／NFS／部署／Infra 清理及实际证据统一见[D8 河网正式场景](../../docs/spec/testing/scenarios/Manager与空间成果.md#d8-河网转矢量线独立-hosted-验收)。失败须零发布、零新增扫描血缘，清理仍为验收前提；数值极限继续单列 Native，不计 UI 或容量，未执行、失败或未覆盖项不能计为通过。
