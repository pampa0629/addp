# ADDP Online 场景：DEM 曲率与地形指标

[返回测试与验收总入口](../../addp测试与验收规范.md) · [返回空间成果场景](Manager与空间成果.md)

| 主题 | 正文入口 |
| --- | --- |
| 曲率 | [平均](#dem-平均曲率独立-hosted-验收) · [剖面](#dem-剖面曲率独立-hosted-验收) · [切向](#dem-切向曲率独立-hosted-验收) · [平面](#dem-平面曲率独立-hosted-验收) |
| TPI／TRI | [TPI](#dem-地形位置指数tpi独立-hosted-验收) · [TRI](#dem-地形崎岖度tri独立-hosted-验收) · [受控血缘](#填挖方与tpi受控血缘取证修复) · [期望缓存](#tpi验收计时与本轮独立期望值缓存) |
| 粗糙度 | [矩阵、独立参考与固定结果](#dem-地形粗糙度独立-hosted-验收) |

平均／剖面／切向曲率沿[本页共同边界](#平均剖面与切向曲率共同验收边界)；平面曲率／TPI／TRI沿原页[DEM 四专项边界](Manager与空间成果.md#dem-专项共同验收边界)，该正文也供填挖方引用；粗糙度沿原页[本组专项边界](Manager与空间成果.md#本组独立专项的核验与证据边界)。各组权限、容差、原件归档和验收范围按原声明分别认定。

## 曲率

### 平均、剖面与切向曲率共同验收边界

仅适用于平均、剖面与切向三个独立手工 suite；各自沿 Hosted／NFS、同源产品默认入口 Runtime、普通 User 原 12 项权限和独立 600 秒业务预算，不改产品算法、权限、其他模块或旧套件。剖面、切向仅手动登记，不并入 nightly，也不取消排队任务。算法准入见[栅格算子契约](../../workflow/栅格算子契约.md)，各方法的矩阵与固定证据见对应小节。

- **样本与独立预期**：519×515 矩形投影多波段网格，显式第二数据波段、跨 512 块，覆盖米／国际英尺／美制测量英尺高程与不同水平 CRS、凹凸／鞍面／倾斜面、NoData／mask／透明和部分透明；各方法另列零梯度与小图边界。独立九点最小二乘核全幅像元／mask；剖面、切向保留 `RTOL=1e-10`、`ATOL=1e-11`，不以方法间关系代替主预期。
- **保存与身份**：完整 COG／最近邻金字塔、原网格／CRS、Float64／NaN／`1/metre`，artifact／物理／Meta 大小为相等正整数；精确自动扫描父 execution／User／Tenant／Engine，当前执行实际输入血缘及 Monitor，源和先前成果不变。概览按实际尺寸核验，小图不虚构层级；519×515 原生新建 COG 按整数减半为 259×257、129×128、64×64，每层独立核完整像元／mask。
- **拒绝与清理**：非法数据波段、alpha 作高程、复数高程、旋转／南向网格、经纬度或缺 CRS、缺定位网格、非法高程单位／单位冲突、未应用 scale／offset 共 12 类各中英文提交；剖面、切向另加非法 method。失败必须绑定同次 curvature 任务及准确领域原因，反向查询不改冻结提交语言；零发布／新增扫描／派生血缘。所有退出核工作区、NFS、部署与 Infra 零残留，源旧不变仍必验。
- **验证与范围**：各节专用 Runner 外，真实 GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`；专项筛选或本地 Runner 不算全量／真实 T4。原始 64 位相邻高程、极端数值和不可表示步长仍单列 Native；固定 API Hosted 不外推后续整树、UI、容量、任意影像、生产、业务 DELETE 或全量 IAM。三项 Hosted artifact 均无 COG 原字节，不称离线像元重算；旧超时／原生崩溃不会由新通过关闭，未解决项见[栅格后续专题](../../../next/栅格算子体系后续专题.md#原生稳定性)。

### DEM 平均曲率独立 Hosted 验收

`raster-curvature-persistence`；`make test-raster-curvature-online-runner`，沿[三方法共同边界](#平均剖面与切向曲率共同验收边界)。8 条 COG 链：三组高程／水平单位直接曲率、填洼／重分类接曲率、全无效、无完整有效邻域、小于 3 行／列。另覆盖中心脉冲、合法零；独立九点设计矩阵最小二乘与解析曲面核九点等权／自由常数项、凸正凹负和一圈无效邻域。

首轮 COG 期望曾错误地向上取整；依据 GDAL `frmts/gtiff/cogdriver.cpp` 与真实文件事实改为共同边界的整数减半，保留布局和像元误差断言。成功门禁跟踪输入无漂移、无新增 Python 原生崩溃报告；这不关闭旧崩溃。

**固定结果**：`05e44e4a0a3cce04c9860765745d074d0ab70e7c` 的 [Hosted Run40339721289／Job121630801003](https://github.com/pampa0629/addp/actions/runs/40339721289/job/121630801003) 在原 600 秒内通过 8 条 COG 链与 24 次双语拒绝；全像元／mask、金字塔、严格大小、精确自动扫描、当前执行血缘／Monitor、源和旧成果不变、工作区／NFS／部署清理与 Infra 零残留均有现场报告。artifact `12659759018` 未保存 COG 原字节，不能声称离线重算。

Native 前置为 `cabfbe79c3d09c8db9211abbda8c848a0beb011f` 的 [Job121497755392](https://github.com/pampa0629/addp/actions/runs/40300756435/job/121497755392)，全量 7298 项通过；不等于上述 Hosted 源码整树 Native 通过。固定结果与共同边界分别认定，不补认未覆盖项。

### DEM 剖面曲率独立 Hosted 验收

`raster-profile-curvature-persistence`；`make test-raster-profile-curvature-online-runner`（由 `make test-raster-online-runner` 聚合），沿[三方法共同边界](#平均剖面与切向曲率共同验收边界)。12 条 COG 链：三种高程单位直接 profile、填洼／重分类、水平平地、非平地零梯度残差、全无效、无完整邻域、小图及默认／显式 mean 两对照。覆盖极小非零坡度；3×3 残差与 2×7 小图为零层概览。独立九点最小二乘＋第一／第二基本形式方向商，平地严格 0、其他零梯度严格 NaN；mean 另用基本形式迹核验，默认／显式 mean 逐字节一致。13 类共 26 次双语准确拒绝。

保存成功后、负例前，集中逐条等待实际输入至当前成果的 active derive 血缘，不手工 collect 或额外重试。阶段记录仅含枚举、序号、状态与相对耗时，不存 SQL、原始响应或凭据，未完成阶段不算通过。同次物理核验可共享只读期望，实际文件完整重读、精确求和、污染反例及容差保持；切向沿用此等待和记录约束。

| 结论 | 最小证据与边界 |
| --- | --- |
| 首轮失败 | `06f9caf5637580d72e5ef571a8a14fd91d31cd46`，[Run40265540763／Job121387037149](https://github.com/pampa0629/addp/actions/runs/40265540763/job/121387037149) 在 600 秒超时，无最终报告；请求次数不算成功链数，清理通过仅证明清理。 |
| 等待修正后仍失败 | `909be4e7b1e1269c916bcf431c458f0e1db1dd17`，[Run40272625047／Job121408540481](https://github.com/pampa0629/addp/actions/runs/40272625047/job/121408540481) 完成 12 条链，负例第 23 次开始后超时；剩余 4 次负例及源最终核验未完成。 |
| 修复与 Native | 去除同次 oracle 的重复计算，保持独立预期、12／26 范围及 600 秒。`d8ddc96dc03d862c66de5ee3ca32b1d79f6b07e4` 的 [Linux Job121492608736](https://github.com/pampa0629/addp/actions/runs/40299191579/job/121492608736) 全量 7266 项通过，不外推后续整树或 Platform。 |
| 正式 Hosted | `7157fb44f4b09bb08e097469ecf5360e03c5c0af`，[Run40305988194／Job121515098139](https://github.com/pampa0629/addp/actions/runs/40305988194/job/121515098139) 在 600 秒内通过 12 链／26 次拒绝、完整物理／扫描／血缘／Monitor、冻结语言、源旧不变、全部清理及 Infra 零残留。artifact `12643687867` 无 COG 原字节，不算离线重算。 |

旧两轮超时仍为失败；并行镶嵌 double-free 根因未定，旧本地 Parquet 失败未在本节闭合，均不由新全量或本项通过关闭。其余未覆盖范围沿共同边界。

### DEM 切向曲率独立 Hosted 验收

`raster-tangential-curvature-persistence`；`make test-raster-tangential-curvature-online-runner`，夹具、Native recipe、变更选择与 CI 登记已准备，沿[三方法共同边界](#平均剖面与切向曲率共同验收边界)。13 条 COG 链：三组高程／水平单位直接 tangential、填洼／重分类、水平平地、非平地零梯度残差、全无效、无完整邻域、小图及默认 mean／显式 mean／显式 profile 三对照。另覆盖横向凸凹、陡坡、小而非零坡度及非零切向曲率；3×3 残差与 2×7 小图为零层概览。13 类共 26 次双语准确拒绝。

独立九点最小二乘＋第一／第二基本形式方向商，切向与平面曲率解析样本必须区分，水平平地严格 0、其他零梯度 NaN。默认／显式 mean 数据字节一致，profile 用自身方向 oracle；仅在三份实际保存结果的非零梯度共同有效处核 `profile+tangential=2×mean`，不用该关系生成切向主预期。完整物理核验和三方法实际数据交叉检查各有独立污染反例；等待顺序、闭集阶段记录和实际文件重读沿[剖面专项](#dem-剖面曲率独立-hosted-验收)。

**失败与收口**：`2f704eb174ba00a9f8e54318f22c5264f37c487a` 的 [Run40267387076／Job121392642425](https://github.com/pampa0629/addp/actions/runs/40267387076/job/121392642425) 在 600 秒超时，无最终报告，提交／查询次数不算通过链数；清理独立通过。修后 `909be4e7b1e1269c916bcf431c458f0e1db1dd17` 的 [Run40272637832／Job121408579032](https://github.com/pampa0629/addp/actions/runs/40272637832/job/121408579032) 在原预算内通过 13 链／26 次拒绝，完整物理／扫描／血缘／Monitor、三方法 228530 个共同有效像元关系、默认 mean 对照及清理／Infra 零残留。artifact `12630112467` 无 COG 原字节；已核 Job 元数据及场景原始日志，未补取完整 Job 日志，不宣称离线重算。

`6cdc38dff129492a83f3200b2b27ffc908e8e6ca` 的 [Linux Native Job121379953801](https://github.com/pampa0629/addp/actions/runs/40263251151/job/121379953801) 全量 6918 项通过，只覆盖该产品源码，不覆盖后来新增夹具或 Hosted 909 整树；Platform 未在本项独立验收，也不证明平面曲率新能力。旧 double-free 和其他未解决问题见[栅格后续专题](../../../next/栅格算子体系后续专题.md)，其余缺口沿共同边界。

### DEM 平面曲率独立 Hosted 验收

`raster-plan-curvature-persistence`沿[共同边界](Manager与空间成果.md#dem-专项共同验收边界)，15成功COG链：三组高程／水平单位直接plan、填洼／重分类接plan、水平平地／非平地驻点／全无效／无完整邻域／小图、真实存储高程`8z+64`，及默认mean／显式mean／profile／tangential四对照。样本含凹凸／鞍面／倾斜面、圆形等高线、可表示极小非零梯度和部分透明。

主预期为独立九点等权自由常数最小二乘＋等高线单位切向水平法向加速度，`RTOL=1e-10/ATOL=1e-11`，不用产品卷积核／切向结果造plan预期。核圆形`-1/r`、正高程缩放／平移不变、水平平地严格0、其余精确零梯度NoData、无阈值；默认／显式mean保存字节一致。四方法只在非零梯度共同有效处核`profile+tangential=2×mean`及`tangential=plan*g/sqrt(1+g²)`；所有物理／语义断言有污染反例。

13类各中英文，共26准确失败：波段、alpha、复数、旋转、南向、经纬度、缺CRS／网格、非法高程单位／冲突、未应用scale／offset、非法method。绑定curvature任务，保留Float64／NaN／每米单位，其余核验沿共同边界。

标准`make test-raster-plan-curvature-online-runner`、`make test-geopython-workflow`及输入选择／手工workflow登记；本地筛选不算全量。固定`79ebc6ff93f2301db51a34e64f54e7ac0976014b`的[Linux Job121440770332](https://github.com/pampa0629/addp/actions/runs/40283174360/job/121440770332)全量7253过；同源[Hosted40283794715／121442687622](https://github.com/pampa0629/addp/actions/runs/40283794715/job/121442687622)600秒内完成15／26、228529共同有效像元关系及完整清理。artifact12635131765无COG原字节，不算离线重算；父Platform公共crypto依赖冲突不计整套通过，旧剖面超时／原生double-free仍未由本项关闭。

## TPI／TRI

### DEM 地形位置指数（TPI）独立 Hosted 验收

`raster-tpi-persistence`沿[共同边界](Manager与空间成果.md#dem-专项共同验收边界)。13成功COG：混合凸凹、平地／倾斜平面／对称鞍面三合法零、三组高程与水平单位交叉、填洼／重分类、Int64／UInt64普通大基值原生高差、全无效／无完整九点邻域。保跨块多波段、NoData／mask／透明／非有限覆盖与有效部分透明。

13类中英文共26失败：波段、alpha、复数高程、非法单位／冲突／未知波段单位、scale／offset、经纬度／旋转／南向／缺CRS／定位，逐条定位当前tpi与冻结语言。独立高精度Decimal八邻均值及解析平面／鞍面，精确核最终Float64／NaN全像元／mask、零／正负、原网格／CRS／metre，不调产品函数。

核后原COG存`tpi-cogs/`及`tpi-cogs-manifest.json`大小／SHA清单，不能静默修复；收尾再核，未知文件／软链接／归档变化均失败。受控血缘见[下节](#填挖方与tpi受控血缘取证修复)，期望缓存与后续固定复验见[计时专题](#tpi验收计时与本轮独立期望值缓存)。

**固定结果**：`a26d758daab9149e6fdda628aa202957d9245906`，[Hosted40635880285／122642624444](https://github.com/pampa0629/addp/actions/runs/40635880285/job/122642624444)，13成功／26失败、535785ms＜600000ms；七服务同源／干净、Tenant2／User33／12权限、默认`sh container_entrypoint.sh`。13扫描／13实际源Meta关系及Monitor齐，内部input.source／唯一output.save／create／raster_save／derive与公开隐藏operations分别核验，源／旧成果／工作区／cleanup／Infra通过。

artifact12794317336的13原COG独立重开核519×515、原CRS／完整仿射、Float64／NaN／metre、全像元／mask、三级nearest概览及COG布局，固定Decimal夹具精确比含大基值／全无效／无邻域；原件／清单／三方大小／闭合血缘一致。源TIFF未归档，仅据固定夹具重建oracle，不称离线重读源或重跑产品／Hosted；Homebrew PROJ_DATA／PROJ_LIB只设审计进程。

旧`63d6299ea`的Run40538763760首条execution血缘失败，未完成13／26／原件归档，完整actual差异未存；a26不能回填旧内部事实。此为一次受控API验收，不认定稳定吞吐。

#### 填挖方与TPI受控血缘取证修复

仅两套独立Hosted的受控只读证据，不改公开Develop／Monitor隐藏operations。内部source-port／create／derive逐项核，再沿原普通User12权限核Meta实际关系；数据库补证不算API授权通过，无公开回退。

仅当次Hosted Linux隔离addp_online准备私有归属回执，冻结容器ID／启动时刻／suite／Run ID，读时再核未重启、Tenant／真实User／execution／完全相同DAG。固定有界只读事务，不接受任意SQL、写库／造事实／主动collect；DB凭据不进User请求，只归档闭合字段／大小／hash，拒绝前保公开／内部差异及失败类型，错误仅类型／指纹。原矩阵／预算／数值NULL／原件／扫描／身份／Monitor／保持／清理不减。

标准`make test-raster-cut-fill-online-runner`、`make test-raster-tpi-online-runner`、`make test-python-ci-registration`、`make test-geopython-workflow`须拒公开泄露、内部缺失／错源端口操作、身份／DAG错绑、容器替换、取证／归档篡改。

修复`1a0b3bb27d7ada93404fff7b76fd677d83800122`的[Linux Job122636163645](https://github.com/pampa0629/addp/actions/runs/40633847309/job/122636163645)全量7858过；后续a26两Hosted非Markdown输入相同，结果见[填挖方](Manager与空间成果.md#双dem填挖方统计独立-hosted-验收)／[TPI](#dem-地形位置指数tpi独立-hosted-验收)。固定输入等价不等于整树SHA相同，不外推HEAD／其他Job或旧double-free根因。

#### TPI验收计时与本轮独立期望值缓存

缓存仅本轮独立Decimal期望，绑定Run／夹具代码／源hash，私有锚点及数组hash每次核，缺失／篡改／跨轮复用／清理失败均失败。首次生成及清理纳600秒，不缓存真实成果、不并发业务或改轮询／资源策略／权限／产品。仍13成功／26双语拒绝、39调用／429次真实成果全检，完整像元／mask／概览／COG／大小／扫描／内部公开血缘／Monitor／保持／清理不减。

`189f5f8ce3219ba6257d055f975f9bd2154c0b1d`的[Hosted40645432302／122673041241](https://github.com/pampa0629/addp/actions/runs/40645432302/job/122673041241)终审通过：259655ms含准备8432ms（期望7703ms）及清理325ms，嵌套时间不重复加；39调用／429全检、13 COG原件／manifest及离线Decimal／mask／三级nearest复核通过，字节与早期a26一致。较535785ms减少276.13秒／约51.5%，只证两固定运行，不作吞吐／容量承诺，数值极限仍Native。

准备`2b7df04aa5fdd097f4a2fe1c11062f69c0c6cae3`的[Linux40644787773／122670954049](https://github.com/pampa0629/addp/actions/runs/40644787773/job/122670954049)标准Native全量7887过／24 warnings／2119.73秒。原ZIP／digest／CRC、日志／步骤、原件与只读回执／矩阵／清理只在固定范围复核，不计重跑或后续源码通过。

### DEM 地形崎岖度（TRI）独立 Hosted 验收

`raster-tri-persistence`沿[共同边界](Manager与空间成果.md#dem-专项共同验收边界)，13成功COG／13类中英文共26失败。链含凸凹／平地／倾斜／鞍面、三组单位、填洼／重分类、Int64／UInt64大基值微差、全无效／无完整九点；第二波段／跨块／NoData／mask／alpha／非有限覆盖、零及非负均全检。

独立2500位Decimal Riley平方和开方，不导入产品函数；每成功／负例后重读所有既有成果，39调用／429实际全检，拒一ULP错误。metre及三级nearest、当前实际输入内部／公开安全血缘及Meta关系仍必验，其余物理／扫描／失败清理沿共同边界。原件／清单不可静默修复，源／旧成果保持；只读取证限本轮容器／User／Tenant／execution，期望缓存绑定Run／夹具／源hash，不缓存实际成果，准备及收尾均纳600秒。

`db2638ac9d7dc2f11a1fc949d0be77b0a0f238de`的[Hosted40652107491／122694375654](https://github.com/pampa0629/addp/actions/runs/40652107491/job/122694375654)终审通过，368511ms。artifact归档13原COG／清单，固定Decimal夹具独立复算全像元／mask／CRS／米制／三级nearest及布局；源TIFF未归档，不称从归档源重生。官方ZIP4208788字节，digest／CRC／13原件、受控回执／13扫描／Monitor／矩阵及零残留已只读复核，不等于新Hosted。

准备`872375a48da8150a86936f8c6ae67d293b007a62`的[Linux40650267385／122688461218](https://github.com/pampa0629/addp/actions/runs/40650267385/job/122688461218)Native全量8134过／39 warnings／1967.59秒；此前本地231专项／runner／登记仅准备，不与全量累加。额外类型／单位组合与数值极限仍分层，固定范围不作UI／容量／稳定性或后续源码认证。

## 粗糙度

### DEM 地形粗糙度独立 Hosted 验收

`raster-roughness-persistence`；`make test-raster-roughness-online-runner`，沿[本组边界](Manager与空间成果.md#本组独立专项的核验与证据边界)。13 COG链含中心极大／极小及正负邻居、平地／斜面／鞍面、三单位、填洼／重分类、Int64／UInt64大基值微差、全无效／无完整九点；13类中英共26准确失败。独立2500位Decimal九点max−min含中心，不取半差／去趋势；39调用／429全检、13原COG，期望准备／清理纳600秒，数值极限仅Native。

`60da3691404ee74d3b6789d2948f575be1edef9c`的[Hosted40700360070／122858214770](https://github.com/pampa0629/addp/actions/runs/40700360070/job/122858214770)完成矩阵／13扫描／内外血缘／全清，215112ms；13原件独立Decimal核全像元／mask、519×515／metre／三级nearest／COG。源TIFF未存，离线参考据固定夹具；ae5准备全量因范围外矢量断言失败仍失败，Hosted不追改或认证当前HEAD全量。
