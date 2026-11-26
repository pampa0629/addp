# ADDP Online 场景：邻域统计与 IDW

[返回测试与验收总入口](../../addp测试与验收规范.md) · [返回空间成果场景](Manager与空间成果.md)

通用准入见[T4 Online 验收规范](../T4-Online验收规范.md)。共有规则仅适用于原文明示引用的专项；窗口、权重、精度、权限、预算及固定证据仍逐节认定，各专项不互借通过结论。

| 阅读主题 | 正文入口 |
| --- | --- |
| 共同规则与加权 | [邻域与 IDW 专项共同要求](#邻域与-idw-专项共同要求) · [邻域参数与专项范围](#邻域参数与专项范围) · [三种邻域加权方法的共同复核](#三种邻域加权方法的共同复核) · [邻域加权求和独立 Hosted 验收](#邻域加权求和独立-hosted-验收) · [邻域加权总体标准差独立 Hosted 验收](#邻域加权总体标准差独立-hosted-验收) · [邻域加权均值独立 Hosted 验收](#邻域加权均值独立-hosted-验收) |
| 方窗与百分位 | [raster-focal-statistics-persistence](#raster-focal-statistics-persistence) · [raster-focal-percentile-persistence](#raster-focal-percentile-persistence) · [邻域多百分位独立保存验收](#邻域多百分位独立保存验收) · [raster-focal-variety-persistence](#raster-focal-variety-persistence) · [7×7 像元方窗独立 Hosted 验收](#77-像元方窗独立-hosted-验收) |
| 米制圆窗 | [米制圆形邻域统计独立验收](#米制圆形邻域统计独立验收) · [正交旋转网格米制圆形邻域统计独立验收](#正交旋转网格米制圆形邻域统计独立验收) · [米制加权圆窗独立 Hosted 验收](#米制加权圆窗独立-hosted-验收) |
| IDW插值 | [点数据IDW插值独立 Hosted 验收](#点数据idw插值独立-hosted-验收) · [IDW最少支撑点数独立 Hosted 验收](#idw最少支撑点数独立-hosted-验收) · [IDW距离幂次独立 Hosted 验收](#idw距离幂次独立-hosted-验收) · [IDW最近N个支撑点独立 Hosted 验收](#idw最近n个支撑点独立-hosted-验收) |

## 邻域与 IDW 专项共同要求

仅下文明示引用的专项适用；原[五项公共前提](Manager与空间成果.md#独立持久化专项公共前提)和[水文／分区要求](Manager与空间成果.md#水文与分区统计专项共同要求)不扩范围。语义以[邻域契约](../../workflow/栅格算子契约.md#栅格计算契约)、[IDW契约](../../workflow/栅格算子契约.md#点数据idw插值)为准，矩阵、容差和归档清单各自保留。IDW始终核点源＋模板两输入；模板只定义网格／CRS。

- **准备与预算**：独立手工Hosted／NFS、同源默认入口Runtime、普通User原12权限，各600秒。身份／源初始化、首次预期、全部执行查询／物理全检、原件归档及业务缓存清理按原边界计时，部署／外层退出另核。4 CPU／2 GiB／无额外swap仅用于原已声明的专项。不接管个人部署，不改产品、权限、安全投影及原套件，不重试、扩预算或加夜间调度；准入与报告沿[T4规范](../T4-Online验收规范.md)。
- **完整核验**：全部实际像元／mask、Float64／NaN、原六项网格／CRS或缺定位、COG布局及实际nearest概览；小图不虚构概览。artifact＝物理＝Meta正整数大小，自动扫描绑定当前父execution／User／Tenant／Engine，实际输入血缘和Monitor单独核对。内部observer保持原有界SQL、独立凭据与当前身份／DAG／Runtime绑定，公开安全投影不扩大。
- **复核与退出**：源及先前成果原字节／Meta不变，按各节次数真实重读；不得缓存实际成果。只读预期仅按原许可复用，绑定同轮root／run、源hash和夹具源码；原件／清单不覆盖、修补或静默重建，独立oracle不调用产品算法，不放宽精确比较或容差。错误须绑定实际focal／interpolate及原原因／提交语言，前置失败不充数、反向查询不改错。失败零发布／新增扫描／派生血缘；正常、失败、部分启动均清理本轮工作区／缓存、NFS、Runtime／镜像／registry、部署／Infra等已声明资源，首败与清理错误分别保留，清理失败整套失败。
- **分层验证**：Runner沿 `make test-raster-online-runner` 聚合，GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`，输入与workflow接线遵守[CI规范](../CI与交付规范.md)。本地／登记不代替Linux全量或真实Hosted、UI、容量；原始类型、非标准JSON及各节数值极限／混合类型／故障注入继续单列Native，不扩格式支持。

### 邻域参数与专项范围

邻域统一使用非空严格递增的 `percentiles` 列表，单项也传 `[p]`；旧单值 `percentile` 已删除，旧DAG显式修改。原单项、圆窗及variety对照只同步列表与准确诊断，原成功／失败矩阵、全检次数、原件与600秒预算保持，历史固定证据不追认为新版本。多项要求直达[邻域多百分位](#邻域多百分位独立保存验收)，登记不代表已完成真实Hosted。

像元方窗为3／5／7，米制圆窗的候选外接窗口最多7×7；两者的权重尺寸分别按现行契约核验，不互相扩大。十二种方法、跨双轴块、全1对照及P50／中位数规则按各节独立矩阵验证；[7×7专项](#77-像元方窗独立-hosted-验收)已登记，实际运行另行认定。

### 三种邻域加权方法的共同复核

仅下列明确引用的weighted_sum／weighted_stddev／weighted_mean套件共用本段。各为16条COG成功链、56次准确失败：成功累计136次、失败累计896次，合计1032次完整成果检查；加首次空基线共73次物理调用。逐次核完整像元／mask、缺定位和源编码清除、COG及实际概览，按上方要求核大小、扫描／内外血缘／Monitor、源旧Meta与全部清理。归档16份COG原字节、十份源原件、16份冻结独立预期及大小／SHA256清单，不覆盖／修补旧归档。三种方法各自的求和、均值或总体标准差oracle、全1解析见证与准确拒绝条件仍分别验证。

#### 三种加权方法的矩阵与拒绝条件

以下三suite各16成功COG（14加权＋2原方法对照）／28类各中英共56准确失败，沿[邻域共同要求](#邻域与-idw-专项共同要求)的12权限／各600秒及上方1032全检／73物理轮／十源与16成果16期望归档，不互借验收结果。

成功共同覆盖非对称3／5跨块两链、全1权重3／5＋实际原方法3／5四链、非均匀同比例权重3／5两链、中心权重0／有效支持空、全无效、重分类、旋转、无定位单行、Int64／UInt64原生NoData邻值、第二波段raw scale-offset／mask／alpha；均值另保部分透明见证。非对称／同比例／重分类用519×515跨块，其余核实际概览；矩阵不翻转，中心无效不填洞，正权重有效支持空为NaN，先判原生NoData再按Float64边界，不宣称任意64位精确。方法独有见证如下。

共同28类：18权重缺失／null／数值标量／布尔／字符串／对象／空列表／一维／行数错／列数错／不规则行／全零／负元素／布尔元素／字符串元素／null元素／对象元素／列表元素；原对应sum／stddev／mean带weights、variety带null weights、加权方法带percentiles三类；未知方法／非法窗口／布尔窗口／波段0／越界／选择alpha／复数数据七类。须同execution命中真实focal及准确原因，前置失败不充数，反向语言不改冻结错误；失败零发布／scan／derive，保持及清理沿共同要求。非标准JSON（含NaN／Infinity／超标准数值）、混合复数alpha与各法数值极限仅Native，登记／本地不代Hosted。

### 邻域加权求和独立 Hosted 验收

`raster-focal-weighted-sum-persistence`；`make test-raster-focal-weighted-sum-online-runner`，沿上述矩阵及[完整复核](#三种邻域加权方法的共同复核)。weighted_sum全1与原sum的实际全像元／mask及解析0／4、−4／4、2／2对照须得4／0／4；非均匀权重放大8倍的实际总量严格同比例。独立逐位置2000位Decimal乘积累计、最终Float64舍入，不用生产整数编码／归约，不除权重和／抽样／容差；常量按实际有效正权重质量，单样本保加权值，合法零／负结果保留。真正溢出像元转NaN只沿Native证据；加权均值 Runner 证据见均值专项。

### 邻域加权总体标准差独立 Hosted 验收

`raster-focal-weighted-stddev-persistence`；`make test-raster-focal-weighted-stddev-online-runner`，沿上述矩阵及[完整复核](#三种邻域加权方法的共同复核)。weighted_stddev全1与原stddev实际全像元／mask一致，解析0／4、−4／4、2／2分别2／4／0；非均匀权重用逐位置3000位Decimal偏差公式算总体方差再开方，不用生产一二阶矩／精确平方根，不放容差／抽样。核同比例权重不变、输出非负、常量／单样本0；不承诺任意64位整数差值。TRI仅保原有效证据，不纳本suite Hosted重跑。

### 邻域加权均值独立 Hosted 验收

`raster-focal-weighted-mean-persistence`；`make test-raster-focal-weighted-mean-online-runner`，沿上述矩阵及[完整复核](#三种邻域加权方法的共同复核)。weighted_mean全1与实际mean、同比例放大权重均核完整像元／mask相同，合法零／负值保持。独立逐位置2000位Decimal构造、最终Float64舍入并精确比，不调生产／容差／抽样。原九方法及数值极限另列Native，不以平台前置拒绝代目标诊断。本地标准 `make test-raster-focal-weighted-mean-online-runner` 退出 0，专项 19 项通过，含清理失败判例。历史 60 秒模拟清理超时仍保留，现行 Runner 未复现。真实 Hosted 待验。

## raster-focal-statistics-persistence

独立手工专项；标准业务入口 `make test-online ONLINE_SUITE=raster-focal-statistics-persistence`，Runner为 `make test-raster-focal-statistics-online-runner`。准备／成果核验／清理按[邻域共同要求](#邻域与-idw-专项共同要求)；[原五项公共前提](Manager与空间成果.md#独立持久化专项公共前提)仍限原范围，Runner／Native分层见[验证入口](Manager与空间成果.md#独立持久化专项验证入口)。不登记夜间计划，不改已有栅格T4、权限或跨模块逻辑。

### 邻域输入与六条成功链

已登记EPSG:4326、519×515多波段旋转仿射TIFF，显式选原始数值波段。源含合法零／负值、NoData／mask、NaN／正负Inf、透明／部分透明alpha，跨块边界布置无效值，并有大于窗口的无效孔洞。保存六份COG：

- 3×3 sum、min两条。
- 5×5 mean、max、stddev三条。
- 重分类后接3×3 stddev一条。

### 非法波段与窗口失败

非法波段和非法窗口各须执行失败，均不得发布目标或新增扫描；已有成果与精确清单不变。

### 独立NumPy核验

完整窗口NumPy oracle不调用产品统计函数，按实际有效邻域核全部输出像元／mask；无效中心保持NoData、边缘不补零，不填无效孔洞。只统计选定波段raw值，scale/offset分开处理，不隐式套用。

核Float64／NaN NoData、完整旋转网格、COG布局及实际金字塔；仅手扫精确源，各成果自动扫描和当前输入active derive按共同要求逐项核验。

### 邻域固定证据与未覆盖范围

`fe0d7cc8e` 的[Hosted Run39228689716／Job117981136802](https://github.com/pampa0629/addp/actions/runs/39228689716/job/117981136802)已核六COG、三方大小、精确扫描／血缘、源及旧成果不变和全部清理；同源Linux全量1251项与Platform检查通过。仅认该固定范围，不推定当前HEAD已重跑。

不认证Manager绘制、NFS客户端写、HTTP Range、业务DELETE、覆盖冲突、所有窗口组合的跨模块覆盖或容量。

## raster-focal-percentile-persistence

独立手工专项，准备／成果核验／清理按[邻域共同要求](#邻域与-idw-专项共同要求)。标准准备入口 `make test-raster-focal-percentile-online-runner`，真实GDAL及篡改拒绝沿 `make test-geopython-workflow`；suite、手工workflow、受控只读血缘suite及路径登记同批核验，不改原套件、安全投影或权限，不登记定时。

### 百分位16条成功链

- 端点对照：3×3第0百分位／min、5×5第100百分位／max，共4条。
- 中位数对照：3／5窗口各执行第50百分位／median，共4条。
- 第25／75／33.3百分位3条，重分类接第90百分位1条。
- Int64／UInt64邻接NoData两条，无定位／全无效两条。

主源519×515，显式第二数据波段；保留双向跨块、小数／零／负值／重复值、奇偶及单有效邻居、NoData／mask／alpha／非有限数据、原始scale/offset和旋转网格。无定位源不得补造CRS或仿射变换。

### 18类输入与36次准确失败

每类均用中文／英文提交，共36次：

- `percentiles`缺失；单元素列表含null、布尔、字符串、嵌套数组、下界越界或上界越界，合计7类。
- 其他方法携带百分位、未知方法，共2类。
- 窗口值、布尔、字符串非法，共3类。
- 波段越界、0、布尔、字符串、alpha，共5类。
- 复数数据1类。

复数alpha单列Native混合类型GDAL数据集证据；不能用全复数TIFF的数据波段前置失败冒充alpha校验，不扩产品格式。NaN／Infinity参数、极大／次正规及极小百分位权重等数值极限仍单列Native。

### Fraction独立期望与精度

标量邻居枚举＋Fraction线性位置oracle逐像元核完整结果／mask，不调用产品排序／插值或NumPy quantile。端点／50与各自控制链逐像元完全一致；其他插值沿Native Float64精度 `rtol=4e-15`、`atol=0`，mask与合法零严格比较。

只允许缓存已核输入／参数的只读期望，首次构建计入预算；实际成果验证不得缓存。其余物理／身份、内部事实与公开投影、Monitor及源旧不变沿共同要求。

### 712次全检与原件保真

每次成功／失败后重读全部既有成果：16轮成功累计136次，36轮失败为576次，合计52次物理调用／712次全检。完整COG及实际3层nearest概览每轮核验，不能用旧校验结论代替。

原16份COG字节及大小／SHA清单只追加核验，不重新编码或静默修复；全部退出保留首个业务失败与清理错误。实际Hosted仅按固定版本及运行范围认定，登记和Native不替代真实验收。

## 邻域多百分位独立保存验收

独立手工 `raster-focal-percentiles-persistence`；`make test-raster-focal-percentiles-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。 Runtime保持4 CPU／2 GiB／无额外swap。

16条COG链覆盖3／5方窗批量及逐项堆叠对照、两种方窗median、米制圆窗批量／堆叠／median、国际英尺／美制测量英尺圆窗、重分类、缺定位Int64／UInt64原生NoData邻接大整数、跨双块轴及全无效。请求列表固定[0,25,33.3,50,75,100]；逐项堆叠沿原空描述契约，批量波段严格核percentile描述和顺序。

28类参数／顺序／未适用列表／波段／窗口／圆窗失败各中英文提交，共56次，准确落在focal及对应原因，反向查询不改提交语言。每次终态重读所有先前成果的全部波段、mask和最近邻概览，共1032次成果全检／73次物理调用；独立逐像元邻居及Fraction线性插值oracle，实际批量与逐项堆叠／P50与median全量比较。定位缺失事实保持，物理／身份、当前实际源血缘／Monitor、源旧Meta及失败／全退出清理按共同要求。归档16份COG、全部源、冻结oracle及大小／hash，144份原始Develop／Monitor终态响应。

只读一次／排序一次、旧单值关键字移除、非有限JSON／元组／混合复数alpha及数值极限继续单列Native证据，不冒充Hosted。登记和本地回归不等于真实Hosted／Linux全量；实际运行由CI会话协调，不计UI、容量或吞吐。

## raster-focal-variety-persistence

独立手工 `raster-focal-variety-persistence`；准备入口 `make test-raster-focal-variety-online-runner`。身份、准备、扫描、大小、源与既有成果不变及残留清理按[共同要求](#邻域与-idw-专项共同要求)。全部44次执行／查询与首次源、预期准备计入原业务预算。

### 种类数成功矩阵与有效性

固定16条成功COG链：直接variety的3／5窗口、重分类接variety、零分类、Int64／UInt64安全整数分类、缺定位519×1单行、全无效，以及同源sum／mean／min／max／stddev／mode／median／percentile50八方法对照。

主样本为519×515旋转网格，显式选第二波段；覆盖跨块、边缘截断、负分类、合法零、孤立有效中心、NoData／mask／透明／非有限覆盖和部分透明。未选第一波段故意含小数；源scale=0、offset=0.5不得隐式应用。

种类数是有效不同分类的正整数，零分类计1；无效中心不填补，50百分位与中位数实际成果相等。Int64／UInt64原生NoData先于Float64转换排除。安全整数邻接极限及复数alpha混合类型单列Native，不以Hosted全复数前置失败代替。

### 28次准确失败与诊断归属

14类非法输入各以中英文提交，共28次：分类小数、正／负超安全整数，波段0／越界／布尔／字符串，alpha、复数分类，窗口非法值／布尔／字符串，非法method及不适用percentiles参数。

逐项定位当前focal任务及精确原因，不接受load／保存／扫描提前失败充数；保留诊断的实际语言契约，反向查询不改提交语言或已保存错误。

### 独立预期与584次全检

标量set／Counter／math／statistics／Fraction oracle构建16份不可变预期，不调用产品统计／排序或NumPy百分位函数，预期不由实际成果生成。variety及除mean／stddev外对照精确相等；mean／stddev保留rtol=1e-12、atol=1e-12。

每次成功后重读所有已有成果，16轮累计136次；每次失败后重读全部16份，28轮累计448次，合计584次完整像元／mask核验；另加首次完整源基线，共45轮物理核对。核验完整COG布局、按实际尺寸的nearest概览、Float64／NaN、原网格／CRS、无伪造定位、编码属性、合法零和源全波段／mask，禁止用结果缓存代替物理重读。

只扫描精确源；内部raster_save／derive、公开operations:null、Meta关系与Monitor分别核验，不能由公开投影推断内部血缘缺失。

### 冻结归档与验收边界

归档10份源TIFF、16份成果COG和16份冻结独立预期NPY原字节及各自大小／SHA256清单，保留当前执行受控只读封闭血缘回执；预期缓存绑定、归档异常拒绝沿共同要求。

真实GDAL、准确失败、像元／概览／缓存／原COG和预期归档篡改拒绝接 `make test-geopython-workflow`；Online／changed gate、独立手工workflow、受控只读observer与CI静态登记沿准备入口核对。入口登记和Native覆盖不等于真实Hosted通过，实际验收只按固定版本及运行范围认定。

## 7×7 像元方窗独立 Hosted 验收

`raster-focal-square-seven-persistence`；`make test-raster-focal-square-seven-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。18COG含12种7×7方法、三全1权重、3×3/5×5同源mean、7×7全无效。369列×367行跨横纵360块界，三非对称权重及min/median/max/多percentile跨轴；独立冻结预期核全波段mask、P0/P50/P100实际对照、三对全1／非加权及权重方向边缘。

30类双语60准确focal失败，共1251全检；18COG／源／冻结预期及SHA归档，六仿射CRS／Float64NaN／布局及实际nearest核。数值极限、读块大小／注入仅Native。

## 米制圆形邻域统计独立验收

`raster-focal-circle-persistence`；`make test-raster-focal-circle-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。18COG涵九非加权方法，3/4米步长半径5及邻接浮点、P50＝median、两英尺／仅中心／重分类／无效／方窗对照；519×515跨轴、薄行列／常量单样本／零负／NoData/mask/alpha、raw scale-offset。

50类双语100准确拒绝；独立Decimal距离／标量统计，不调产品窗归约。1971全检／119物理调用含预期，18COG／源／预期数组原件归档；原成功样例覆盖至5×5、等号纳入、无epsilon，沿原数值容差而分类成员零mask精确，复数／非有限JSON／极限仅Native。

扩加权后原两套圆窗的三旧方法拒绝，改为加权mean尺寸错、weighted stddev全零、weighted sum圆外非零；仍真实focal双语准确，原成功／全检／原件／清理及600秒不变，旧固定证据不追认新版本。原十二方窗方法不改，准备不算Hosted／UI／容量。

## 正交旋转网格米制圆形邻域统计独立验收

`raster-focal-circle-orthogonal-persistence`；`make test-raster-focal-circle-orthogonal-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。19COG含九非加权方法、90°/180°/270°及斜向正交、非方形、跨轴／薄行列／三单位、半径等号邻接浮点；同轴长北向／P50-median／方窗／中心／重分类／全无效实际对照。

53类双语106拒绝，含极小剪切、浮点点积零而精确非零、旋转单轴镜像，行列轴／单像元超限仍拒绝。独立Decimal全仿射距离统计，数值容差沿原圆窗，像元mask／六仿射／CRS／完整COG与实际nearest精核；2204全检／126物理调用含预期，19COG／源／冻结预期及SHA归档，极限／注入仅Native。

## 米制加权圆窗独立 Hosted 验收

`raster-focal-circle-weighted-persistence`；`make test-raster-focal-circle-weighted-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。27COG含北向非方形3×3／正交旋转非方形5×5的三非对称方法、三对同源全1／非加权、两英尺、1×1非单位权重、重分类／全无效／半径前一浮点。独立Decimal全仿射距离及逐位置权重，stddev中心偏差、sum绝对权重；7×7 新增北向加权均值、旋转加权总体标准差、跨块加权和及三对全1／无权实际COG对照。全1实际对照精确相等，核半径等号／前浮点边界、跨轴／NoData／显式mask／部分alpha及raw波段。

36类双语72准确focal失败（权重14、方法窗10、网格CRS8、波段4）；2322全检／含基线100物理轮，27COG／源／冻结预期与SHA归档，像元mask／六仿射／CRS／Float64NaN／实际nearest及布局精核；极限／注入仅Native。

沿用晕渲专项的隔离准备账号。该账号只发起本轮首次精确源扫描，并按实际目录边界发放限时 `metadata_read`，范围限本轮指定的源和已核对自动扫描及当次执行的成果。普通 User 以原 12 项权限执行全部业务，分别核对两账号的扫描库存。公开结构与隔离库内绑定 DataItem、扫描和执行的只读快照分别核对，三方大小及源和旧成果快照保持严格校验。600 秒含预检、授权、业务与收尾，预留撤销和缓存清理时间，逐笔归档撤销回执及撤销后不可见证据。

固定提交 `a552b2136` 的[首跑](https://github.com/pampa0629/addp/actions/runs/45631920757/job/139267131715)在首次源扫描时返回 403，业务矩阵未开始。同一请求在 System 的元数据刷新范围校验返回 403。业务进度记录为 224 毫秒，成功链、双语节点拒绝、成果全检及归档 COG 均为 0。隔离环境清理通过，Infra 零残留。[原始归档](https://github.com/pampa0629/addp/actions/runs/45631920757/artifacts/15035787643)

固定提交 `d6d689292` 在 Ubuntu 24.04 x86_64／GDAL 3.8.4 运行无筛选的 `make test-geopython-workflow`，22,109 项通过，141 条警告，耗时 1574.45 秒，失败、跳过及筛除均为 0。前置 NetCDF 专项另有 109 项通过。[修后 Linux](https://github.com/pampa0629/addp/actions/runs/45648191072/job/139324816601)

[实现提交 Linux](https://github.com/pampa0629/addp/actions/runs/45630922450/job/139263576757)与[验收提交 Linux](https://github.com/pampa0629/addp/actions/runs/45631571311/job/139265868233)各 22,108 项通过、1 项失败，均为元数据测试仍要求 5×5。断言已同步至 7×7，本地专项 9 项通过。

## 点数据IDW插值独立 Hosted 验收

独立手工 `raster-interpolate-idw-persistence`；`make test-raster-interpolate-idw-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。 Runtime保持4 CPU／2 GiB／无额外swap。

- 16条成功保存链：基础／点序反转、精确重合、半径等于／相邻下浮点／相邻上浮点、模板外支持、局部无支持、空点批、全无效模板、mask／alpha忽略、矩形跨512块、旋转、剪切、国际英尺、美制测量英尺；包含合法零与负值。模板只定义网格与CRS，不继承像元、NoData、mask、alpha或scale／offset。
- 18类各中英文提交，共36次准确拒绝：零／负／布尔／字符串半径，字段缺失／选择活动几何字段，空值／字符串／不精确Int64样本，同值／异值／范围外重复XY，空几何、LineString、MultiPoint、三维Point、CRS不一致、地理CRS。必须命中`interpolate`节点的准确原因；源读取或提交前置错误不能代替IDW准入证明，反向语言查询不得改写已存失败。
- 独立有理数oracle逐像元穷举全部点，以原坐标、网格及已知CRS单位判定精确米制圆边界、重合和加权；不调用产品插值、空间索引或GDAL插值。首次计算与清理均计业务预算。只读缓存绑定本轮root／run、全部源hash及夹具源码hash，任何变更或可写缓存均失败。
- 先16成功再36失败，每次终态重读全部已有成果，合计712次现场全像元／mask／合法零检查；按实际尺寸核COG布局、Float64／NaN、网格／CRS和nearest概览，不能虚构小图概览。归档16份COG原字节、全部29份源、冻结预期及大小／hash清单；同时保留52执行的104份Develop／Monitor原始终态响应，供独立复核。
- 当前执行点源＋模板两源的内部事实与公开安全投影分别校验；物理／身份、源旧Meta及部分启动／失败清理均按共同要求。
- 极端浮点、混合类型、重复列、无法可靠持久化的非法网格与故障注入继续单列Native，不为Hosted增加格式支持或修改产品规则。既有套件、权限与预算不变。

## IDW最少支撑点数独立 Hosted 验收

独立手工 `raster-interpolate-idw-min-points-persistence`；`make test-raster-interpolate-idw-min-points-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。 Runtime保持4 CPU／2 GiB／无额外swap。

16条COG成功链：省略门槛／显式1实际对照、支撑数等于3／不足4、超过总数但保留精确重合零值和负值、无精确重合且门槛超过总数的全NoData、半径相邻下浮点／等于／相邻上浮点（模板外两个样本）、反转点序与门槛3实际对照、空点批、全无效模板及mask／alpha／scale-offset忽略、矩形横跨512块、旋转兼剪切纵跨512块、国际英尺及美制测量英尺。保持固定距离平方反比和半径内全部样本，不截取前N点；计数在精确半径过滤后进行，精确重合豁免支撑门槛。

保留旧18类IDW准入回归，新增min_points的null／true／false／零／负数／小数1.5／字符串／列表／对象九类，各中英文提交，共54次准确interpolate失败，反向语言查询不改存储错误。成功终态逐次重读1至16份成果（136次），54失败终态各重读16份（864次），合计1000次实际COG全检／71次物理调用。独立Fraction oracle逐像元穷举全部点及精确CRS单位，禁止复用产品插值或索引；冻结oracle绑定本轮root／run／所有源／夹具源码，缓存只读且不可修复。

默认1及反序对照须同时比较实际保存值；物理／身份、点源＋模板两源血缘、源旧Meta与清理按共同要求。归档16份COG原字节、所有源、冻结预期及大小／hash清单，以及70次执行的140份Develop／Monitor原始终态响应。

1.0与1的原始类型区别、非有限JSON、numpy整数、数值极限、无法可靠持久化的非法网格及故障注入单列Native，不修改产品格式支持。本节尚无已核验的真实Hosted通过记录。

## IDW距离幂次独立 Hosted 验收

独立手工 `raster-interpolate-idw-power-persistence`；`make test-raster-interpolate-idw-power-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。 Runtime保持4 CPU／2 GiB／无额外swap。

24条COG成功链：每种幂次8条。1／3各普通点集与逆序两条，2为省略power与显式2两条（同源／模板／半径／支撑配置）；其余每种幂次各最近N点／距离与X-Y决胜、精确重合零负值及非重合支撑不足、空批、全无效mask-alpha模板、国际英尺矩形横跨512块、美制测量英尺旋转剪切纵跨512块各一条。基础夹具含3／1.8／1的三幂次解析见证、合法抵消零与负值；最近点夹具精确核半径等号及相邻浮点先筛后排、不足NoData、X后Y决胜。实际保存文件核默认2与显式2／原Fraction公式一致、1／3点序和三幂次模板隔离一致。

80次中英文准确失败：power为null／true／false／0／负数／4／1.5／字符串／列表／对象10类各中英20次；每种合法幂次各min_points=0、max_points=0、普通／空批／精确重合三类min-max冲突、范围外重复XY、范围外缺失数值、未选中点缺失数值、非法几何、CRS不一致10类各中英，共60次。必须到达interpolate节点并核准确原始原因，提交／读取前置失败不得充数，反向查询不改冻结语言。原生浮点2.0、非有限JSON、宽整数／复数／数值极限及故障注入继续单列Native，不扩产品格式或共享JSON数值契约。

幂次2用独立Fraction旧公式；1／3用全点精确米制距离与2000位Decimal欧氏距离幂公式，解析／有理见证单独核算，不复用产品索引／根式分组／区间舍入。只读冻结预期绑定root／run、源hash及夹具源码。其余物理／身份、两源实际内外血缘、源旧Meta和清理按共同要求。成功重检300次、失败重检1920次，合计2220次全检；104执行、105物理调用（含基线），归档24原COG、全部源／冻结预期／大小hash及208份Develop／Monitor终态响应。本地准备不计Linux全量、真实Hosted、UI或容量通过。

## IDW最近N个支撑点独立 Hosted 验收

独立手工 `raster-interpolate-idw-max-points-persistence`；`make test-raster-interpolate-idw-max-points-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。 Runtime保持4 CPU／2 GiB／无额外swap。

16条COG成功链：省略上限／N超过总数实际对照、N=1／N=2等距截断（分别核X优先及同X再按Y升序）、N=2点序反转实际对照、半径相邻下浮点／等于／上浮点（模板外支持）、合法大门槛下精确重合零值和负值保留、无重合支撑不足全NoData、空点批、全无效模板及mask／alpha／scale-offset忽略、矩形横跨512块、旋转兼剪切纵跨512块、国际英尺和美制测量英尺。全部按精确圆半径过滤后才排序选点和判断min_points，额外包含非等距选择与全体支撑结果不同的明确见证；不因等距多取、不设epsilon，所有输入先完整校验。

保留原18类IDW和9类min_points准入，新增max_points的null／true／false／零／负数／小数1.5／字符串／列表／对象9类，以及max<min的普通样本／空批／精确重合3类，共39类×中英文=78次准确interpolate失败。旧非法样本及重复XY场景设置小N，含未选中／范围外坏样本不被隐藏；反向语言查询不得改写提交语言与已存错误。成功终态重读1至16份成果（136次），78失败终态各重读16份（1248次），共1384次实际COG全检／95次物理调用。独立逐像元Fraction穷举／完整排序oracle不复用产品、索引或选择堆；只读冻结预期绑定本轮root／run、所有源hash和夹具源码。

默认不限／大N与原序／逆序必须比较实际保存成果；物理／身份、点源＋模板两源血缘、源旧Meta与部分启动／全部退出清理按共同要求。归档16份COG原字节、全部源、冻结预期和大小／hash清单，以及94执行的188份Develop／Monitor原始终态响应。

1.0与1的原始类型区别、非有限JSON、numpy整数、重复索引、数值极限及故障注入继续单列Native，不扩产品格式支持。本节尚无已核验的真实Hosted通过记录。
