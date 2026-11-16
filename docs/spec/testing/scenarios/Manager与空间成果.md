# ADDP Online 场景：Manager与空间成果

[返回测试与验收总入口](../../addp测试与验收规范.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

专项导读：[文件读取公共准备与收尾](#文件读取-case-的公共准备与收尾) · [公共前提](#独立持久化专项公共前提) · [Runner／Native 验证入口](#独立持久化专项验证入口) · [水文与分区统计共同要求](#水文与分区统计专项共同要求) · [水文固定结果与待验](#水文专项的固定结果与待验边界) · [邻域与IDW共同要求](#邻域与-idw-专项共同要求)

本页场景导航仅用于定位，适用范围以各场景正文为准：

| 内容 | 场景入口 |
| --- | --- |
| Manager 与文件 | [内部产物与 case](#manager-internal-artifact-lineage) · [混合检索](#manager-hybrid-search) · [KML／GML／TopoJSON S3](#kmlgmltopojson-的-s3-兼容消费) · [设备静态轨迹](#设备静态轨迹范围读取) |
| 表格读取 | [ORC 单文件](#orc-单文件表格-hosted) · [Avro 单文件](#avro-单文件表格读取) · [Feather V2](#feather-v2-基础标量独立-hosted) · [CSV／TSV GB18030 读取设置](#csvtsv-gb18030-独立读取设置-case) |
| 表格历史证据 | [ORC 平坦传输／嵌套只读](#orc-分层验证与历史证据) · [Avro 首轮／复验](#avro-验证与历史证据) · [Feather 分版本](#feather-分版本证据) · [GB18030 本地／Hosted](#gb18030-分层验证与历史证据) |
| 媒体与容器读取 | [NetCDF](#netcdf-单文件只读) · [GRIB2](#grib2-单文件只读) · [ENVI 双文件 COG](#envi-双文件-cog限定-hosted-已验收) · [Surfer GRD](#surfer-grd-读取) |
| 媒体读取历史证据 | [NetCDF 授权契约适配](../evidence/资源读取与预览历史证据.md#netcdf-当前元数据授权契约适配--限定正式-hosted-通过) · [GRIB2 数值与页面](../evidence/资源读取与预览历史证据.md#grib2-单文件读取与切片--数值通过及页面收口) · [ENVI 精确组成](../evidence/资源读取与预览历史证据.md#envi-精确双文件现行权限正式复验通过2026-11-04) · [ENVI MinIO 节点](../evidence/资源读取与预览历史证据.md#envi-minio-持续节点新建文件及到期独立-hosted-通过2026-11-04) · [Surfer 四份 GRD](../evidence/资源读取与预览历史证据.md#surfer-6-ascii-扩展及四份-grd-读取--普通-user-hosted-t4) |
| 矢量与地图 | [拓扑持久化](#拓扑构面面叠面缝悬点与线叠持久化vector-topology-persistence) · [重合规整](#重合规整独立场景) · [PMTiles 工作流](#vector-pmtiles-workflow) |
| 栅格流程与持久化 | [Raster 工作流](#raster-workflow) · [等值线](#raster-contour-persistence) · [分类转面](#raster-polygonize-persistence) · [矢量栅格化](#vector-rasterize-persistence) · [整数分区统计](#raster-zonal-statistics-persistence) · [邻域统计](#raster-focal-statistics-persistence) · [邻域百分位](#raster-focal-percentile-persistence) · [邻域分类种类数](#raster-focal-variety-persistence) · [筛除分类保护](#分类小斑块筛除指定类别保护独立-hosted-验收) · [筛除面积阈值](#分类小斑块筛除平方米面积阈值独立-hosted-验收) · [矢量分区统计](#vector-zonal-statistics-persistence) · [邻近距离](#raster-proximity-persistence) · [细化](#raster-thin-persistence) · [边界清理](#raster-boundary-clean-persistence) |
| 邻域与插值专项 | [三种加权方法共同复核](#三种邻域加权方法的共同复核) · [7×7方窗](#77-像元方窗独立-hosted-验收) · [米制圆窗](#米制圆形邻域统计独立验收) · [多百分位](#邻域多百分位独立保存验收) · [IDW基础](#点数据idw插值独立-hosted-验收) · [最少支撑](#idw最少支撑点数独立-hosted-验收) · [最近N点](#idw最近n个支撑点独立-hosted-验收) · [距离幂次](#idw距离幂次独立-hosted-验收) |
| 点／线密度专项 | [共同要求与数量矩阵](#点线密度专项共同要求) · [点权重](#点密度显式非负权重独立-hosted-验收) · [点核密度](#米制点核密度独立-hosted-验收) · [线核密度](#米制四次线核密度独立-hosted-验收) · [线权重](#线密度显式非负权重独立-hosted-验收) · [uniform线](#米制圆形线密度栅格独立-hosted-验收) · [uniform点](#米制圆形点密度栅格独立-hosted-验收) |
| 容量与磁盘 | [分类蚕食容量](#分类蚕食容量raster-nibble-capacity) · [容量验证入口](#分类蚕食容量的验证入口) · [临时磁盘保护](#raster-disk-pressure) |
| DEM 地形与统计 | [填洼](#raster-fill-depressions-persistence) · [平均曲率](#dem-平均曲率独立-hosted-验收) · [剖面曲率](#dem-剖面曲率独立-hosted-验收) · [切向曲率](#dem-切向曲率独立-hosted-验收) · [平面曲率](#dem-平面曲率独立-hosted-验收) · [双 DEM 填挖方](#双dem填挖方统计独立-hosted-验收) |
| D8 流向与累积 | [流向](#dem-d8-流向持久化) · [汇流累积](#d8-汇流累积持久化) · [加权累积](#raster-flow-accumulation-weighted-d8-persistence) · [下游流长](#d8-下游流长持久化) · [上游最长流长](#d8-上游最长流长持久化) · [汇水分区](#d8-汇水分区持久化) |
| D8 河网与汇水 | [Strahler](#raster-stream-order-d8-persistence) · [Shreve](#shreve-独立持久化验收) · [河段编号](#d8-河段编号独立持久化) · [河网转线](#d8-河网转矢量线独立-hosted-验收) · [指定汇水点](#raster-watershed-d8-persistence) · [汇水点吸附](#raster-snap-pour-points-persistence) · [增量子汇水区](#raster-subcatchments-d8-persistence) |
| D8 面积、距离与高程 | [汇流面积](#raster-flow-accumulation-area-d8-persistence) · [到河网沿流距离](#raster-distance-to-stream-d8-persistence) · [HAND](#raster-hand-d8-persistence) |

## GRIB2 单文件只读

阅读导航：[授权与来源](#grib2-授权与来源) · [逐消息数值](#grib2-逐消息数值) · [页面切片与原件](#grib2-页面切片与原件) · [清理与未覆盖项](#grib2-清理与未覆盖项) · [验证与历史证据](#grib2-验证与历史证据)。

### GRIB2 授权与来源

已批准的首期范围见[GRIB2 格式规范](../../builtin-formats/媒体与地图数据集.md#grib2-单文件只读限定范围已验收)。独立 `native-grib` case 复用既有 Manager Linux x86-64 Hosted 部署、GeoPython、NFS、MinIO 和 NetCDF 的六项普通读取权限，不新增服务、执行权限或精确组成内容 Grant，不运行 Model3D 或 Transfer 业务。

按现行跨引擎元数据规则，复用套件已有、与消费者分离的源初始化 User，为扫描后四个精确文件／对象办理 `metadata_read` Grant；核准前不可见、核准后仅返回结构投影，退出撤销并确认重新不可见。仅元数据边界不得作为内容完整性或内容读取授权。

NFS／MinIO 各两份独立字节编码源文件，每份三条单场消息，覆盖简单打包、IEEE32／64、不同尺寸、相同要素不同预报时刻、两种行扫描方向、合法值 9999 与显式 bitmap 缺失、大于 180° 的原生经度。

### GRIB2 逐消息数值

完整格式参数、版本和数值通过消费者既有 `manager.content.read` 与 Security 约束下的 Manager 预览／切片核对，不从 Meta 结构接口索取内容属性；GRIB 内容源授权迁移仍不在本轮范围。

逐消息核对全部数值、掩膜、单位、时间、独立网格、有界子窗口、错误选择和源变化拒绝。

### GRIB2 页面切片与原件

真实页面在标准1280px及Hosted视口使用完整属性切换消息并清除旧结果，核对读取按钮可点击、切片DOM及首尾单元格滚动后实际可见；每条消息保存一张数值表截图，四份原件另核API／按钮完整字节及页面截图。

### GRIB2 清理与未覆盖项

页面切片不写回元数据，不生成地图、任务或缓存成果；额外核 Native 临时快照清空，精确源及外层收尾沿[公共要求](#文件读取-case-的公共准备与收尾)。

受验消息、网格、压缩和模板范围以格式规范及分层证据为准；产品模板 8 只有 Native 证明，不由 Hosted 模板 0 的结果推定通过。GRIB1、单消息多场、其他网格／压缩／本地扩展、地图／COG、科学计算与导出不在该验收范围；不读外部 idx、不自动转单位或重排经度，SKP 精确组成准入仍保持关闭。

### GRIB2 验证与历史证据

标准门禁复用 `make test-geopython-workflow`（含独立字节夹具经真实 Native Worker）、`make test-common-frontend`、`make test-module MODULE=common GO_ONLY=1`、受影响 Meta／Manager／Transfer Go 门禁、`make test-manager-frontend`、`make test-manager-online-runner` 和 `make test-online-runner`。正式验收沿现有 workflow 选择 `suite=manager-internal-artifact-lineage`、`manager_case=native-grib`，业务入口为 `ADDP_ONLINE_MANAGER_CASE=native-grib make test-online ONLINE_SUITE=manager-internal-artifact-lineage`。

固定源码 `6e7ae87559552bd7d5bad122f4f0259f963d7b46`、Run `41572302717`／Job `125707787735` 的 Owner／Browser v2 覆盖十二张可见切片、四原件、Grant 撤销及清理，详见[分层证据](../evidence/资源读取与预览历史证据.md#grib2-单文件读取与切片--数值通过及页面收口)。首轮 v1 数值／DOM／原件有效但控件被长属性遮挡，元数据截图不证明切片可见；修后首验 `stage=scan / cases=[]` 因缺元数据授权失败，未进数值／页面。GeoPython CI 取消及本机 runner TLS 夹具错误独立保留，不由专项或清理通过推定全量 CI。


## NetCDF 单文件只读

阅读导航：[来源与权限](#netcdf-来源与权限) · [变量与切片](#netcdf-变量与切片) · [页面与原件](#netcdf-页面与原件) · [清理与未覆盖项](#netcdf-清理与未覆盖项) · [验证与历史证据](#netcdf-验证与历史证据)。

### NetCDF 来源与权限

独立 `native-netcdf` 验证 CDF-1/2/5 及 NetCDF-4 根组数值 profile，复用 GeoPython／NFS／MinIO；不启动 Model3D／PointCloud／Document，不执行 Transfer 或任务。部署及未执行组件的判定沿[公共要求](#文件读取-case-的公共准备与收尾)。

一次性普通消费用户严格六项已有权限：manager.content.read、manager.data_item.read、meta.catalog.read、meta.scan_task.execute、meta.scan_task.read、monitor.execution.read；不增加权限，源初始化身份复用现有标准初始化范围，普通原文件读取仍沿现行内容权限，不宣称精确组成内容源Grant已接入。浏览器进程移除初始化凭据。

按现行跨引擎元数据契约，扫描后由分离的初始化User为十四个精确文件／对象签发metadata_read Grant，沿既有共享ExactFileMetadataGrants核准前不可见、核准后可见、退出撤销后不可见及详情404；不授予目录／前缀或内容读取Grant。

保留三个独立编码的经典样例：CDF-1未知坐标、CDF-2显式UTM50N规则网格、CDF-5超2^53的Int64。增加四个独立netCDF-C/HDF5生成的固定NC4原件：Int32压缩、Int64、UInt64近上限、显式UTM50N规则网格；生成器不调用产品读取器，原件随夹具保存，数值/mask/网格由独立明确oracle核对。七样例均含两时间层、缺失值、比例/偏移和360_day时间维。

### NetCDF 变量与切片

公开Meta只核容器结构投影，原始类型、维度、日历、组成profile和源版本由消费者经既有Manager内容入口核对并归档。

两引擎共十四份源上逐文件核规范身份、变量与维序、NC4完整组成profile、原始数值类型、字节大小/SHA、二维完整数值/掩膜、指定小窗口、重复无持久缓存读取、越界/伪子数据集拒绝、经典与NC4源变化409及原件精确恢复。

父容器不得取得统一空间范围，无CRS切片不得定位；显式规则网格只在已证空间profile下显示范围，不据数据名称推4326。

### NetCDF 页面与原件

实际页面逐格核原值和valid状态，另核数值表可视高度及最后数据格能滚动进入视口，防止地图把表格压缩到零高度；有定位样例分别保留数值网格与地图截图。切时间层清掉旧图、下载哈希/截图，归档manager-netcdf.json与manager-netcdf-browser.json。

核对原文件 API 与真实按钮下载的完整字节、大小及 SHA。

Owner 报告使用 `addp.manager-netcdf/v2`，必须包含现行元数据授权证明，拒绝缺少该证明的旧回执；页面报告沿用 `addp.manager-netcdf-browser/v1`，严格核对 schema、逐源数值／掩膜／时间切换、下载及现有截图。

### NetCDF 清理与未覆盖项

按[公共收尾](#文件读取-case-的公共准备与收尾)核源未改写、精确 Grant／NFS 文件及对象清除、外层 identity／engine／deployment／Infra 清理。无任务或业务产物，不登记缓存复用或任务删除。

NC4外部/复杂组成拒绝由GeoPython原生T1及Linux产品构建验证分别取证，不以正常源页面通过代替；NC4嵌套分组、复杂类型、跨文件聚合、输出和任意CF特性不在本轮。

### NetCDF 验证与历史证据

标准入口为 `make test-geopython-workflow`、`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=manager GO_ONLY=1`、`make test-manager-frontend`、`make test-manager-online-runner`；真实业务为 `ADDP_ONLINE_MANAGER_CASE=native-netcdf make test-online ONLINE_SUITE=manager-internal-artifact-lineage`，Hosted 由既有 workflow 选择同名 case。

- [经典 CDF](../evidence/资源读取与预览历史证据.md#netcdf-经典-cdf-单文件只读--限定-hosted-通过)：CDF-1／2／5；首轮页面失败保留，API 数值／清理不替代按钮下载和浏览器后快照。
- [NC4 根组数值](../evidence/资源读取与预览历史证据.md#netcdf-4-根组数值-profile--本地门禁与正式复验)：保留组成拒绝假阳性、ARM64 下载失败、Linux 十项失败及维度引用有／无符号修复。修后 Hosted、Linux 命名回归与完整 GeoPython 超时取消分别认定。
- [现行元数据授权适配](../evidence/资源读取与预览历史证据.md#netcdf-当前元数据授权契约适配--限定正式-hosted-通过)：`5e5d436f8eb433890daa8b04d9c751fe5e7044bc`、Job `125763328404`，十四来源、精确 Grant、结构／内容分工及清理。UInt64 静态截图裁切，首层缺失由 DOM／JSON 证明；范围框不认证外部底图或影像渲染。

详细产物、失败及清理归所属证据；旧回执不替代现行授权，固定版本不证明任意 NC4／CF 或当前所有环境，相关本地失败与 CI 取消仍保留。

## manager-internal-artifact-lineage

### 明确的验收 case

本 suite 的唯一选择参数为 `ADDP_ONLINE_MANAGER_CASE`，未提供时选 `full`；空值或未知值在任何 Hosted 生命周期动作前拒绝。下表与 gate、workflow 的 `manager_case` 和业务分发器对应，各 case 的身份、权限及清理分别按所链正文，不套用 `full` 的十项权限。

| case | 业务覆盖与正文入口 |
| --- | --- |
| `full` | 完整模型、授权、点云、文档等既有断言；[部署、模型转换与通用清理](#部署模型转换与通用清理)。 |
| `native-raster-media` | [IMG／JPEG2000](#img-原生扫描与-cog-快显)、[Surfer GRD](#surfer-grd-读取)、[TIFF 无定位小图](#tiff-无定位小图的-cog-布局回归)；不执行模型、点云、文档、结构化数据业务。 |
| `native-table-orc` | [ORC](#orc-单文件表格-hosted)：嵌套只读与平坦字段 Transfer→Parquet 分别判定。 |
| `native-table-avro` | [Avro](#avro-单文件表格读取)：三 codec、标量／日期／小数／UTC、嵌套／Enum／Map；仅平面批量写 Parquet。 |
| `native-table-feather` | [Feather V2](#feather-v2-基础标量独立-hosted)：六源分页／Transfer→Parquet及各自原件准入。 |
| `delimited-read-settings` | [CSV／TSV GB18030](#csvtsv-gb18030-独立读取设置-case)：编码、表头、分页、重开及原件，无派生产物，不替代 Transfer T4。 |
| `native-netcdf` | [NetCDF](#netcdf-单文件只读)：变量／切片、页面及原件，无任务／持久缓存。 |
| `native-grib` | [GRIB2](#grib2-单文件只读)：逐消息数值、切片、页面及原件，无地图／任务／缓存成果。 |
| `native-envi` | [ENVI 双文件 COG](#envi-双文件-cog限定-hosted-已验收)：精确组成、节点授权、原件／COG／缓存与撤权恢复。 |
| `kml-export` | [KML 页面导出](#kml-实际页面导出)：PostGIS 源、真实按钮下载与独立 XML 核对。 |
| `filegdb-export` | [PostGIS→FileGDB](#filegdb-实际页面导出)：ZIP 回读、跨账号拒绝与排队撤权，独立于 NFS FileGDB 读授权。 |
| `filegdb-mysql-export` | [MySQL→FileGDB](#mysql-filegdb-实际页面导出)：独占 MySQL 源与独立清理，页面及撤权按专节。 |
| `filegdb-authorization` | [NFS FileGDB 整容器授权](授权与共享.md#nfs-filegdb-整容器授权2026-10-29-接入范围)：整项 Grant、子表／ZIP、跨账号／撤权／到期、故障恢复及挂载／临时包清理。 |
| `nfs-upload-initial` | [NFS 上传新项初始授权](授权与共享.md#nfs-上传新项初始授权)：仅此 case 使用自有可写目录；新项授权、覆盖不增权、重放／部分失败及失败收尾，不扩展追加。 |

标准入口示例：`ADDP_ONLINE_MANAGER_CASE=native-raster-media make test-online ONLINE_SUITE=manager-internal-artifact-lineage`。人工 Hosted 必须指定同名 `suite`、`manager_case` 和 `manager_source_commit`：后者为完整40位小写 SHA，检出前拒绝空值、短 SHA、分支名或非法格式；检出后核 HEAD 精确相等且为 dispatch 提交祖先（含自身），才准备工具／部署，不回退 main。日志分别记录源码与 dispatch SHA；应用、Runtime、夹具、断言均来自所选源码，其他 suite 不消费该参数。无此参数的历史 workflow 不替代固定源码入口。

Job、Artifact、`manager-case.json` 均须含所选 case；独立通过不算 full。case 路由、默认值、拒绝、失败收尾及归档由 `make test-manager-online-runner`、既有 CI 登记检查覆盖，不新增入口、suite、服务、权限或定时。

`native-raster-media` 根报告为 `manager-native-raster-media.json`、`schema_version=addp.manager-native-raster-media/v2`，须含 case、`source_formats=[img,jpeg2000,grd,tiff]`、普通身份、四子报告及真实失败状态；任一子场景失败即失败，`online-report.json` 由统一分发器生成。未执行业务不计通过；full 仍须独立验证，可能受新模型源授权正向断言阻断，不用旧源码或放宽授权绕过。

### 文件读取 case 的公共准备与收尾

本节文件读取 case 共用以下要求；`full`、导出／上传与独立持久化按各自正文。部署见[组件分工](#部署模型转换与通用清理)，身份／拓扑与外层清理见[T4 规范](../T4-Online验收规范.md)。

- **来源与授权**：初始化和消费身份分离，浏览器不带办理凭据。精确 `metadata_read` 仅证明元数据可见；内容按各 case 的既定许可读取，不因受阻而扩权。公开 Meta 核身份、格式、布局、已声明 CRS；尺寸、波段、范围等内容事实由同一普通 User 经 Manager 授权预览取回，与物理夹具交叉核验并分开归档。
- **交付**：API、真实页面按钮与独立物理 oracle 分别取证，完整字节／大小／SHA-256 不以截图或 API 成功代替。源字节及Meta保持／变更恢复按各节断言，不以显式刷新或源变化测试推定其他修改获准。无定位不推导 CRS，各格式页面行为按专节。
- **收尾**：只清本轮已知精确资源；源尚在时先核撤权／到期，再删源。逐项核 Grant、任务、成果与物理删除，外层部署／Infra零残留不代替业务清理；无任务／成果者不造删除或缓存证明。执行未终结／归属未确认不报零残留，逻辑任务删除不证明历史执行已物理清除。首败与清理错误分列，清理失败不计通过。
- **验证**：`make test-manager-online-runner`、`make test-online-runner` 核接线；原生媒体夹具经 `make test-geopython-workflow` 自动发现，其他 Native／T1–T3 按 owner 标准入口。固定结果见[资源读取证据](../evidence/资源读取与预览历史证据.md)。本地反例／修复、部署成功、部分 CI 或清理不替代真实 Hosted，不外推当前源码、容量或 full。报告／异常／下载证据不泄漏凭据。

| case | 身份、来源与独有交付／收尾 |
| --- | --- |
| `native-netcdf`／`native-grib` | 消费六项见[NetCDF](#netcdf-来源与权限)；十四／四份精确 metadata Grant，未迁移内容源 Grant；无任务／持久缓存。 |
| `native-table-orc`／`native-table-avro`／`native-table-feather` | 消费十一项见[ORC](#orc-入口与权限)；Avro／Feather 准备十三项、源扫描归初始化用户。各保留 Transfer／只读拒绝；Avro／Feather NFS 原件403 JSON、无附件／按钮，MinIO核 API／按钮完整原件；ORC按自身正文。 |
| `delimited-read-settings` | 消费十项／准备十三项；Common Go reader、初始化 basic 扫描与精确 Grant，无派生产物。 |
| `native-raster-media` | 消费十项；初始化用户仅签本轮 MinIO `raster` 前缀非递归读取，限直属项、退出撤回，不改角色／引擎范围。GRD无产物；TIFF另按专节核初始化扫描权限、整项read、到期及sidecar。 |
| `native-envi` | 消费十项／准备十三项；NFS扫描／变更重扫归初始化，MinIO归消费者，节点与精确组成阶段分离。 |

导出 case 共用真实 Console 入口、独立回读、源前后内容／结构不变及独占源清理。会话按既有保留／生命周期管理，不新增删除 API，不把部署销毁算会话 DELETE／缓存验收；`result=failed / cleanup=passed`仍是业务失败，业务夹具 `cleanup=failed`不能由Infra清理替代。各自授权及回归范围保留如下。

### Surfer GRD 读取

阅读导航：[格式准入](#grd-格式准入) · [四份样例](#grd-四份样例) · [页面与原件](#grd-页面与原件) · [清理与能力边界](#grd-清理与能力边界) · [分版本证据](#grd-分版本证据)。

#### GRD 格式准入

完整准入见[媒体与地图数据集](../../builtin-formats/媒体与地图数据集.md)的[GRD契约](../../builtin-formats/媒体与地图数据集.md#grd--surfer-数值网格)：仅认内容 `DSRB`（Surfer7 v1/v2）、`DSBB`（6 Binary）和`DSAA`（6 ASCII），三者主体≤64 MiB；单波段、完整GRID/DATA／头及全部节点，Surfer7零旋转。拒绝同后缀其他格式、非有限值、断层／尾随及GDAL无法等价表达的v1范围缺失值；不读旁文件、推导CRS或新增导出。

Native另核Binary掩膜与精确NoData等价，ASCII的LF／全缺失／跨块token／无末尾换行及畸形／多余／截断／下溢拒绝；数值／mask不等价即拒绝，源字节不变。此类原生反例不算Hosted故障注入。

#### GRD 四份样例

沿公共十项权限，四份均5×3、14有效节点＋1 NoData，无CRS：

| 样例 | 类型与独有断言 |
| --- | --- |
| Surfer7 v1 | `DSRB/GS7BG/Float64`，经纬度外观仍无CRS，核版本1。 |
| Surfer7 v2 | `DSRB/GS7BG/Float64`，投影外观仍无CRS；大于BlankValue仍为有效值，核版本2。 |
| `grd-surfer6.grd` | `DSBB/GSBG/Float32`、116字节，小数／大整数与固定精确NoData；`profile=surfer6_binary`，无版本字段。 |
| `grd-surfer6-ascii.grd` | `DSAA/GSAG/Float64`，五行头、CRLF、tab／空格、跨行列表、高精度及`2^53−1`、精确NoData；`profile=surfer6_ascii`，无版本字段。 |

独立oracle核全部15个原类型数值、NoData／mask、上下行序、节点原点／间距／形状／范围／半像元网格和完整原字节。Float32／64不混用；Manager核驱动、波段、版本／无版本及网格事实，Meta不发布推断空间能力。

#### GRD 页面与原件

Console iframe逐源核属性／预览／原件按钮、截图与完整下载；无地图、破图或COG入口。ASCII头与精确文本靠原件／oracle，fallback／下载截图不证明地图或全量属性表。

等待当前locator实际`GET /api/v1/manager/preview`，沿该请求Bearer核同一普通User，不等不存在的COG capability。页面报告`addp.manager-raster-media-browser/v4`逐源须有`source_preview_verified=true`，旧v2不替代。归档`manager-grd.json`、`manager-grd-browser.json`、四原件及各Meta快照／截图。

#### GRD 清理与能力边界

按[公共收尾](#文件读取-case-的公共准备与收尾)删精确源；GRD无任务、产物、缓存或快显。原IMG／JPEG2000四COG仍须完整核像元、Range、缓存及领域／物理删除，不能当GRD生成证明。未知CRS不定位；其他GRD变体、任意ASCII编码、写出、旁文件、NFS／FTP专项及容量未验。

#### GRD 分版本证据

| 固定来源／实际Job | 结论及缺口（完整记录见链接） |
| --- | --- |
| `69623c8847c97fae0bb92d6b42fef281702fbd46`／`121439055657` | [首败](../evidence/资源读取与预览历史证据.md#surfer-7-grd-读取首轮--hosted-失败留证)：数值／API原件已核，等不存在的capability超时，属性／按钮／浏览器后Meta未验，整case失败。 |
| `7d8d738c1839c267c4259e2bd713d083110266e1`／`121493786622` | [复验](../evidence/资源读取与预览历史证据.md#surfer-7-grd-读取复验--普通-user-hosted-t4)仅两份Surfer7及原媒体，不含6／NFS／FTP。 |
| `48aae0a4dbf79131edffd143bedb3407d20bfd18`／`121589927858` | [Binary三份](../evidence/资源读取与预览历史证据.md#surfer-6-binary-扩展及三份-grd-读取--普通-user-hosted-t4)；首个Native Job测试未完取消，后续同SHA独立认定。 |
| `ff9e2fb512507d5e4ff1a4b1983be38c142dadfe`／`121910269484` | [ASCII四份](../evidence/资源读取与预览历史证据.md#surfer-6-ascii-扩展及四份-grd-读取--普通-user-hosted-t4)及原媒体；PROJ失败、重复subset主动停止、父Platform取消不计整轮CI通过。 |

### KML 实际页面导出

`kml-export`仅用隔离PostgreSQL/PostGIS、标准Manager→Transfer会话及Infra暂存；产品镜像／disposable源只读角色限合成样例，不连开发库。消费八项：`manager.data_item.read`、`manager.derived_artifact.create`、`manager.derived_artifact.read`、`meta.catalog.read`、`meta.scan_task.execute`、`meta.scan_task.read`、`monitor.execution.read`、`transfer.task.read`。末项是Monitor读取Transfer Owner的`sync`所必需，缺失在身份预检拒绝，不由Manager权限代替。初始化用户保留原四项权限，经System正式命令给已扫描精确表签Grant，无引擎级／管理员范围；本地调用链须经过精确Grant helper的拒绝→签发→允许，再进浏览器。回执读`request_id/item_fingerprint/boundary_hash`，不假设`id`；含Transfer在内的Build ID／ready先通过。

EPSG:4326／3857源含七类二维非空几何、内洞／null几何和int32／bool／double／中文XML特殊字符／空串／null。普通User在真实iframe查看表，经共享弹窗选KML、改名、按钮建会话、等待并接收下载。独立XML oracle核完整Placemark、几何／坐标、Folder、Schema／标量及空值，3857恢复已知WGS84；留文件名／哈希／截图、同用户且无任务定义的执行归属。LIBKML跨版本读取仍归GeoPython原生门禁，XML oracle不证明产品读侧增强。

按公共导出收尾；既有runner、System Go、共享前端和Platform核路由／最小身份／页面／oracle拒绝／失败清理。未实际Hosted不回填通过；不扩展bigint／decimal／时序／嵌套、Z/M、样式、KMZ或其他目标。

### Avro 单文件表格读取

阅读导航：[来源与权限](#avro-来源与权限) · [三种 codec](#avro-三种-codec) · [分页与批量传输](#avro-分页与批量传输) · [清理与未覆盖项](#avro-清理与未覆盖项) · [验证与历史证据](#avro-验证与历史证据)。

#### Avro 来源与权限

`native-table-avro`沿[公共准备](#文件读取-case-的公共准备与收尾)，复用NFS／MinIO、Transfer及GeoPython物理回读。平面／嵌套／Enum／Map各六源（双存储×三codec），共二十四源／任务／精确Grant；只有六平面源可写Parquet，其余十八次须只读拒绝且无输出。Map扩展保留原十八源字节与全部断言。[Owner runner](../../../../scripts/test/manager-avro-online.py)及浏览器回执v6；v2日期小数／v3 UTC／v4嵌套只证当轮，不追认后续。

#### Avro 三种 codec

`null/deflate/snappy`独立编码原件、独立核验，不用产品reader生成期望。

| 来源 | 独有断言 |
| --- | --- |
| 平面 | 每源2503行、多OCF块，标量／nullable、date、bytes／fixed decimal(38,6)：0001／9999、闰日／纪元前、正负大值／尾零／NULL／符号扩展。十三字段含ms/us/ns UTC：零／负计数、ms/us年界、超JS安全整数、ns int64两端及NULL；long独立编码不过浮点／时间对象。 |
| 嵌套 | 每源2503行，根五字段id／detail／items／grid／fixed_values；可空record、record数组、多维数组及具名fixed decimal数组。核NULL对象／全NULL对象、NULL／空数组／NULL元素、大整数／Decimal／日期／三单位UTC／binary及OCF单／多／负块；原平面三文件哈希保持。 |
| Enum | 顶层／嵌套／数组完整枚举名、有序候选、具名引用、null及精确值。 |
| Map | 动态键原文、完整`map_value`树、大整数／小数／UTC纳秒／枚举、NULL／空映射／缺键；真实第二页、结构详情／声明JSON。 |

#### Avro 分页与批量传输

- Meta核schema／date／decimal及UTC的unit=ms/us/ns、timezone=UTC、native_type=long；嵌套用户投影核完整树，不要求公开format_info，原schema保留由Meta原生及原件证明。
- API首页／第二页／末页及真实下一页核总数／全值／缺失；页面UTC固定3／6／9位ISO小数＋Z。嵌套三页／四类结构单元格的只读JSON核完整值，Enum／Map各核声明。原件依公共MinIO允许／NFS拒绝规则；裁切不算全文展开／hover已验。
- 六平面Avro→Parquet跨批核全字段、类型、nullable、精度／scale和值（含大整数／binary／NULL）。writer按字段名排序，不能按OCF列序；PyArrow逐列cast int64核UTC原计数／NULL、物理INT64／TIMESTAMP原单位及isAdjustedToUTC=true，不先转datetime或把最小ns当NaT。
- 十八拒绝逐项绑定execution UUID／Tenant，公开读写0、`error_details.category=execution_failed`，不读取不存在的error_msg或公开原始错误。关联Worker既有Runtime日志单次已保存preview-only拒绝，仅存闭集原因／位置／行哈希；物理空间只含六Parquet＋marker，零计数或缺target_change_possible不证明无副作用。预期拒绝不算整场失败，原因／物理输出不符才失败。

#### Avro 清理与未覆盖项

按[公共收尾](#文件读取-case-的公共准备与收尾)核二十四源／任务／Grant、六成果及marker删除，无十八类拒绝输出。嵌套／Enum／Map批量未开放，local-timestamp／time等未列逻辑类型、whole、写出、所有生产者／压缩变体与容量未验；NFS原件和SKP精确组成准入保持关闭。

#### Avro 验证与历史证据

| 固定源码／作业 | 结论与关键限制 |
| --- | --- |
| `40b1468c5576c3660e1d49c8018f8356f5a959c3`／Job128604699236 | [首败](../evidence/资源读取与预览历史证据.md#avro-单文件标量读取--hosted-验收)：API／原件／Transfer计数后列序oracle失败，未进页面、未归档原Parquet，本地复现不补缺口。 |
| `e706899b69309fa9a7364e4c0f6118b9f5d153dd`／Job128610273652 | [六源三codec](../evidence/资源读取与预览历史证据.md#限定正式复验通过)，六Parquet15018行；ID截图裁切，完整整数由DOM／API／原件／物理证明，相关门禁失败仍保留。 |
| `7af201a28f237a0ef88aa27a8129e54042ff5d8f`，dispatch `1082c0ed0cd2359be69841085cf5dbf02a20467e`；43584189240／132415063748／attempt1 | [UTC v3](../evidence/资源读取与预览历史证据.md#utc-时间戳限定正式-hosted-通过)：六源十三字段／15018行原计数／NULL／UTC注解及清理；前两次失败、六截图长值裁切限制保留。 |
| `c63888fba953df5545a06b11c7641284c2bbe792`；[43668574041／132673333161](https://github.com/pampa0629/addp/actions/runs/43668574041/job/132673333161) | [Enum](../evidence/资源读取与预览历史证据.md#avro-enum-正式-hosted-复验2026-11-15)：十八源、原件、拒绝、六Parquet／清理；首败及视口外声明／过渡帧不作完整视觉证明。 |
| `9cf429c4a9a0ffc84f06e5971bed254c7e935faa`；[43677124474／132699620936](https://github.com/pampa0629/addp/actions/runs/43677124474/job/132699620936) | [Map](../evidence/资源读取与预览历史证据.md#avro-map-限定正式-hosted2026-11-15)：二十四源、键／类型、分页／原件、十八拒绝、六Parquet／清理；NFS403及截图边界不扩大。 |

### ORC 单文件表格 Hosted

阅读导航：[入口与权限](#orc-入口与权限) · [平坦字段与批量传输](#orc-平坦字段与批量传输) · [嵌套对象与数组只读](#orc-嵌套对象与数组只读) · [源变化与读取限制](#orc-源变化与读取限制) · [清理与报告](#orc-清理与报告) · [分层验证与历史证据](#orc-分层验证与历史证据)。产品准入归[ORC正式契约](../../builtin-formats/列式文件与数据容器.md#orc-单文件只读)，公共部署／交付／收尾见[文件读取case](#文件读取-case-的公共准备与收尾)。

#### ORC 入口与权限

`native-table-orc`的入口为`ADDP_ONLINE_MANAGER_CASE=native-table-orc make test-online ONLINE_SUITE=manager-internal-artifact-lineage`，人工Hosted沿既有Manager Job。隔离NFSv3／Business MinIO、GeoPython产品镜像、六Backend及Console，不启本case无关的模型／点云／文档，不改其他case断言。

一次性普通User精确11权限：`manager.content.read`、`manager.data_item.read`、`meta.catalog.read`、`meta.scan_task.execute`、`meta.scan_task.read`、`monitor.execution.read`、`transfer.task.create`、`transfer.task.read`、`transfer.task.execute`、`transfer.task.delete`、`system.execution_authorization.create`。无管理员角色，不改个人／生产身份或产品声明；源／目标以明确locator及source-only plan绑定，不枚举兄弟源。

#### ORC 平坦字段与批量传输

NFS／MinIO两份同物理内容源各2503行，含超JS安全整数、decimal(38,9)、NULL／空值／binary。核深扫、Meta原生类型／footer行数／源版本、三页精确值及尾页、真实下一页／原件；两次真实Transfer各以1000行批次写Parquet，产品镜像内独立PyArrow完整回读所有字段／数值／缺失mask／Decimal精度。

#### ORC 嵌套对象与数组只读

双存储各另加43行struct/list文件，不增身份／引擎／服务／Job。核Meta完整类型树、三页全43行／NULL与空集合、实际下一页／下载、对象与数组详情逐项精确值，保四分页＋两详情截图；源字节／Meta不变。原平面源／下载／两次Transfer全回读保留；嵌套只证结构／分页／详情／原件，同轮平面搬运不认证嵌套批量。

#### ORC 源变化与读取限制

Runtime封闭`ORC_SOURCE_CHANGED`映射Manager `409/preview_source_refresh_required`及既有i18n，不解析本地化异常。Native decoder／Common批量reader仍拒Map／Union；嵌套`OpenTableReader`在任何行读取前拒绝，单选标量列也不绕过。`validation=footer`不证全部数据页，扩展Hosted须按新源码／产物独立认定。

#### ORC 清理与报告

按公共收尾核源／Meta保持、本人任务删除、物理产物及NFS／卷／镜像／部署／Infra清理。归档`manager-orc.json`、`manager-orc-browser.json`、两平面＋两嵌套原件／Meta／截图，与`online-report.json`及源码绑定；缺真实报告不回填Hosted通过。

#### ORC 分层验证与历史证据

GeoPython、六Go owner、Manager／Online runner及既有CI登记分层认定，不增Job／suite：

| 固定源码／实际Job | 结论与限制 |
| --- | --- |
| `c16d563fc2e177521f3924b36b56c9bfc8cce18d`／122074622494 | [平坦读取与搬运](../evidence/资源读取与预览历史证据.md#orc-单文件只读分页与批量读取--nfsminio-普通-user-hosted-t4)：两源2503行／两Parquet任务；前三轮失败、旧预检漏Transfer、本地T2失败与Linux结果分别保留。 |
| `f947636548da8055f7f9cd68c797e6979a1514cd`／122242512428 | [嵌套只读](../evidence/资源读取与预览历史证据.md#orc-对象与数组只读预览--nfsminio-普通-user-hosted-t4)：首轮页面size未初始化失败／准备复验取消保留；详情值由DOM证明，动画帧不是稳定视觉证据。 |

源变化／坏输入／预算归Native，嵌套reader拒绝（含标量投影）归Common T1，不冒充Hosted故障注入／搬运。Map／Union、加密／ACID／whole、ORC写出／上传界面、任意源目标及容量未覆盖；原始产物／清理及失败查所链证据，不外推固定版本。

### FileGDB 实际页面导出

`filegdb-export`用隔离PostGIS、GeoPython产品镜像。初始化用户扫描并签精确读Grant；同一消费用户授予前经Gateway提交每个精确表必须403，且无会话／execution UUID／下载入口。随后真实页面分别导出3857点、4326 MultiLineString／MultiPolygon，三份`.gdb.zip`各仅单图层；核Monitor执行归属／下载字节哈希／截图。同Tenant另一普通用户有导出读取权限仍须拒绝其会话及下载。独立GDAL解包核源CRS、XY、多部件边界、内环／标量／null：线顶点序不变，面允许环起点／方向变化。按公共导出收尾，与NFS FileGDB读授权分开报告。

两种FileGDB case共用执行前撤权断言：仅归属已证的Linux Hosted，用现有进程工具／pidfd暂停唯一Transfer bounded Worker；提交202后Monitor须证pending、attempt=0、未开始。原初始化用户撤销精确Grant并复核未领取，恢复同一进程后须attempt=1在来源授权preparation失败，公开计数不报告内容IO；会话failed、无download_url，文件409、重提403、源不变。异常也恢复原进程，确定性核暂停／恢复／中途异常；不改配置、增权或操作开发进程。该API拒绝不证明浏览器失败页、物理零读或缓存。

Owner v5须含独立`execution_revocation`、每表无Grant拒绝、原三页面／清理及browser v3绑定；旧v3不证撤权。运行中途撤权／自然到期、其他引擎、ZM／多层／复杂字段／容量不因此扩展。

### MySQL FileGDB 实际页面导出

`filegdb-mysql-export`沿上节页面／OpenFileGDB oracle／授权与撤权流程；唯一新增夹具为独占临时MySQL：既有T2固定镜像、随机loopback端口、tmpfs及仅可读三表账号，不连个人Business。初始化13项／消费6项／同Tenant旁观1项权限不变。

三InnoDB普通表分别点3857、复线／含内环复面4326，各三行含中文／空串／null、布尔／小数／空几何；身份为server→database→table，保留数据库轴序／CRS，视图／自定义SQL由guarded reader拒绝。独立注册／快照／运行／清理，不借PostGIS schema或历史通过。

同Owner v5／browser v3绑定case与source_engine_type，旧v4不证MySQL。`business/scripts/online-manager-mysql-fixture.sh`的start／精确快照／stop须核case及容器归属，原Registry／GeoPython及部署清理保留；runner、进程工具／身份夹具、既有登记／workflow核接线，未核正式结果不回填通过。SKP精确组成关闭。

### TIFF 无定位小图的 COG 布局回归

阅读导航：[MinIO 小图](#tiff-minio-无定位小图) · [NFS 自然到期](#tiff-nfs-渲染与自然到期) · [MSK 组成扩展](#tiff-msk-组成扩展) · [清理与共同边界](#tiff-清理与共同边界) · [分版本证据](#tiff-分版本证据)。子报告`addp.manager-raster-media/v2`、页面`addp.manager-raster-media-browser/v4`。以下是验收要求，是否已验按固定记录，不以单文件通过追认MSK／四成员扩展。

#### TIFF MinIO 无定位小图

`native-raster-media`保留IMG／JPEG2000／四种GRD及四份受管COG回归，另加自建240×120 Classic TIFF COG：Float64/NaN、512瓦片、DEFLATE、无CRS／网格／overview。原十项权限及部署／预算不变；不上传个人成果或提交该小图生成，允许既有非定位可选转换按钮，不把无地图等同无按钮。

- 夹具GDAL完整COG验证0错误／0警告；独立公式核28,800值、35 NaN／完整mask、块尺寸／空CRS／空网格／零overview、原大小／SHA。源改写即使重签哈希收据也拒绝。
- 同User经Gateway精确item刷新，核单项同步请求／响应、前后Meta结构／扫描时间及独占源scan execution；`full_name`／指纹／item ID保持。`storage.last_modified_at`按带时区同一时刻比较，其他attributes及源字节不变。Manager受控事实须为`tiff/cog/true/heuristic`、240×120、tiled=true／overview=false且无spatial；与公开Meta分别归档。
- 真实属性／预览核同一源、240×120单页、Range206、全部Float64灰阶／透明NoData、无地图；API及按钮下载同一single TIFF。重开、零生成／业务失败／警告及源／Meta保持分别核对，显式刷新除外。

#### TIFF NFS 渲染与自然到期

`nfs_tiff_authorization`只把同SHA小图复制至本轮只读NFS。仅初始化用户额外拥有`meta.scan_task.execute`、`meta.scan_task.read`完成单目录深扫，消费仍十项；短签／收回精确metadata授权以取得正式DataItem ID，再用同一普通API会话核Range 403→206→403，正向核Content-Range／实际字节。有效时真实Console核同账号、Range及完整灰阶／透明像元；原90秒read自然到期后Meta／preview404，正式历史须证未撤销而已到期。

三态另核：仅metadata时Meta／preview200且仅白名单结构、无内容地址／摘要／统计、Range403；加精确read后完整preview／Range206；撤read后保留metadata白名单，撤metadata后Meta／preview404、Range403。仅增加本轮精确read命令，不增功能权限、目标、服务或预算；根报告核完整状态，异常清理限本轮命令。该有界NFS单文件结果不证BigTIFF、任意布局／容量或Float64地图。

#### TIFF MSK 组成扩展

MSK和四成员复用组成验收实现，保留原格式／四COG／单文件浏览器／自然到期断言，不增权限／服务／预算，均须新增Hosted证明。共同准入为正式Meta DataItem指纹＋System完整boundary hash，不按扩展名／文件数拼授权边界；先验精确扫描身份和成功终态，再签整项read。每成员核授权前403、授权后完整Range206（大小／Content-Range／SHA）、源仍在时撤权后403，跨DataItem借sidecar400；计数相等不替代single／multi完整组成。

| 专项 | 独有组成、扫描与物理断言 |
| --- | --- |
| `nfs_tiff_msk` | 两份字节相同的有定位TIFF，一份带真实`.tif.msk`。独立公式核全部65,536 Int32、NoData／CRS／仿射；无mask仅1无效，有mask257无效。single与multi分离，主image required=true、mask=false。`scan_progress.files_discovered=3`／`directories_discovered=1`，两个total_known=true，`items_planned/items_completed/items_succeeded=2`、`items_failed=0`、`phase=finalizing`，`metadata.items_scanned=2`；原初始化User／成功终态写入`nfs_tiff_msk.scan`，缺计数／额外项／未完成均签发前拒绝。生成额外一COG，核完整数值／mask／空间／布局及删除后对象缺失。 |
| `nfs_tiff_sidecars` | 一TIFF＋同basename HDR／TFW＋完整主名追加PAM，另有相同主字节single对照，不用个人SRTM。主TIFF无定位，TFW给固定网格、PAM给EPSG:4326、HDR给完整可选成员；主image必需，header／world_file／auxiliary_metadata显式可选，single仍无定位。扫描恰5物理文件／1目录／2逻辑项全成功，System事实与Meta完整对应；五成员逐项Range。额外一COG核65,536 Int32、1 NoData、完整mask／TFW网格／PAM CRS／布局；TFW／PAM缺失或变化不能因主图可读而通过，HDR核组成／组件版本／原字节。 |

分别归档正式组成、授权回执、执行／像元及删除证明；四成员Hosted须单独确认，两成员MSK／确定性成功均不能替代。

#### TIFF 组成的浏览器画布断言

MSK／四成员在授权有效且成果未删时各开真实Console快显。夹具独立公式将EPSG:4326网格换算Web Mercator像素，核真实OpenLayers源投影／范围及WebGL postrender RGBA：有效区可见、范围外透明，MSK 16×16无效区中心透明、邻近可见，四成员同位置可见。元数据／原生mask不替代显示；重开复用同成果并再核，实际COG Range206，零生成／业务失败／警告。

`addp.manager-tiff-composition-browser/v1`绑定run／profile／普通身份／源locator／item／成果ID／URL；缺失／错误使组成验收失败，仍收权清理。强制不透明mask、移出预期地图及错误网格／mask报告的本地反例必须拒绝；本地页面／runner不算Hosted，新增运行仍需单独确认。

#### TIFF 清理与共同边界

按[公共收尾](#文件读取-case-的公共准备与收尾)，先核源存在时的撤权／到期再删精确文件，授权／Range／像元／生成阶段独立报告。MSK和四成员各一额外COG，不改变小图禁提交生成；IMG／JPEG2000原四COG仍核缓存、任务／成果／物理删除及模式恢复。

#### TIFF 分版本证据

| 范围 | 最小证据与未覆盖项 |
| --- | --- |
| MinIO小图 | [布局修复与限定Hosted](../evidence/资源读取与预览历史证据.md#tiff-cog-布局误判--有界格式与未知状态修复)，正向与overview反例分开认定。 |
| 刷新／NFS单文件 | [页面、Range与自然到期](../evidence/资源读取与预览历史证据.md#nfs-tiff-渲染授权自然到期与清理--hosted-t4)，首败与最终范围按正文。 |
| MSK本地／准备 | [接入](../evidence/资源读取与预览历史证据.md#msk-纳入-tiff-完整组成2026-11-12) · [本机实操](../evidence/资源读取与预览历史证据.md#msk-singlemulti-本机普通用户实操与清理2026-11-12) · [自动化入口](../evidence/资源读取与预览历史证据.md#msk-自动化验收入口与本地门禁2026-11-12) · [迁移阻断](../evidence/资源读取与预览历史证据.md#tiff-285-已应用迁移被原位修改--system-启动阻断2026-11-12)。 |
| MSK Hosted | [首轮计数失败](../evidence/资源读取与预览历史证据.md#msk-hosted-计数失败与修复后待验2026-11-13) · [修复后待验](../evidence/资源读取与预览历史证据.md#计数修复的自然-ci-与未覆盖范围)：识别不证后续组成／Range／COG与mask／撤权／删除已执行，清理不抵消失败。 |

BigTIFF、任意布局／容量、Float64地图及专业影像生产未覆盖，其他Native／T1／T2／历史截图不能补作Hosted。

### ENVI 双文件 COG（限定 Hosted 已验收）

阅读导航：[身份与来源](#envi-身份与来源) · [精确组成授权](#envi-精确组成授权) · [持续节点授权](#envi-minio-持续节点授权) · [COG 与缓存](#envi-cog-与缓存) · [撤权恢复与组成变化](#envi-撤权恢复与组成变化) · [清理与失败](#envi-清理与失败) · [验证与历史证据](#envi-验证与历史证据)。

#### ENVI 身份与来源

`native-envi`沿[公共准备](#文件读取-case-的公共准备与收尾)，不启动模型／文档／点云。仅源初始化从十一项加`meta.scan_task.execute`、`meta.scan_task.read`为十三项，消费十项不变，不扩大其他case／真实账号／角色／Grant。NFS首次`envi`有界扫及组成／头版本变更重扫归初始化，MinIO归消费者；逐次留存储／范围／阶段／身份／execution，不用已禁Deny。

NFS／MinIO各三份独立构造256×256三波段Byte：BSQ/WGS84、BIL/UTM50N、BIP无定位。核`media/envi/multi`、唯一双ref／完整扫描／原尺寸及两SHA；公开Meta仅结构／CRS，完整组成／尺寸／hash／extent经同User Manager受控入口并独立归档。准入归[ENVI正式规范](../../builtin-formats/媒体与地图数据集.md#envi-双文件只读)。

#### ENVI 精确组成授权

六项只用完整双文件read所含必要metadata，不混入节点Grant。分离办理用户先核System完整组成并批准，再用真实扫描`item_id`的正式locator消费；缺item_id的400不作授权拒绝。未批准／全撤后主项隐藏、详情404；下载未批准拒绝，批准后ZIP两原件逐字节／hash匹配。浏览器单核结构投影／受控内容／ZIP，不从公开Meta读取refs／尺寸／hash／extent。

#### ENVI MinIO 持续节点授权

先独立验证节点metadata_read／read，再进六精确项；同隔离Tenant／用户／办理身份与MinIO，前后有效来源均归零。只加六临时对象：bucket metadata不含内容；prefix非递归覆盖直属及签发后新建首次识别直属，拒绝深层／相邻；递归仅扩后代；bucket两递归范围；撤read后独立metadata可见。拒绝均用真实item_id，允许ZIP核完整成员。

自然到期由正式历史核主体／节点／动作／到期，不重复撤已到期命令；有效命令显式撤。`minio_node_scope`存命令／检查／清理，失败也照清单收尾，核当前关系零／详情隐藏／六副本删除，原输入不变。

#### ENVI COG 与缓存

四定位源经正式action、当前User来源及独立目的EA生成COG，逐份核196608值／mask、CRS／网格／布局、HTTP／物理hash、Range。两无定位BIP只核原件／内容，无地图／直接图片解码／COG动作。真实Console核三色像元／属性／边界／ZIP／截图，重开同缓存、零额外生成。

原件动作与读取共用当前User认证准入；按钮须authenticated-fetch／Blob保存，核同账号、无Cookie／URL凭据、精确locator及完整ZIP，六份逐一`current_user_download_verified=true`。Blob URL不是服务端入口，API不替按钮。

#### ENVI 撤权恢复与组成变化

每源撤read后原件403／Meta404，按同完整清单重批后恢复；COG缓存也须拒绝，重批恢复原成果同Range字节、零新产物，不用旧Deny报告。双存储各BSQ改头命名并重扫，旧清单拒绝，撤旧Grant／批新后可读，最后恢复原组成并重批。同名头内容变化另须拒绝旧COG缓存，恢复原字节并重扫才恢复。

#### ENVI 清理与失败

正式API删成果／任务并核物理对象／精确暂存输入缺失→撤完整read、核六项隐藏／404→删双存储精确源，外层另核。失败先观察终态实际结果，经API删并返回精确清单供物理核对；成果创建前失败记`results_observed=0`，部分成果逐项核删。全部任务后只读查本独占Tenant COG namespace／暂存零残留，记`artifact_residuals`；首错／清理错误分开，不以外层销毁替代。

退出前仅保存本部署Redpanda固定状态及末80行脱敏日志（≤8 KiB），拒读其他Compose；诊断失败`unavailable`，不替业务／清理结论。

#### ENVI 验证与历史证据

Owner `addp.manager-envi/v4`、媒体页面v4；`manager-envi.json`／`manager-envi-browser.json`含结构／内容分离与当前User下载，旧v2／v3不证节点扩展。三交错／双端序／七实数类型、NoData／NaN／mask、漂移／头变更／歧义／外部文件／非法字段／预算拒绝分别经GeoPython及Common／Meta／System／Manager T1/T2，两组runner／登记只核接线。

| 固定来源／作业 | 最小证据与限制 |
| --- | --- |
| 早期／契约适配 | [原件／COG／页面](../evidence/资源读取与预览历史证据.md#envi-双文件读取与-cog-hosted-t4) · [授权首败](../evidence/资源读取与预览历史证据.md#envi-现行授权契约复验--待正式结果)，无效locator400不证拒权，旧通过不追认新契约。 |
| `beb3925600d6480af5722abe1df58103a4328dcb`／126236035325 | [NFS初始化身份失败](../evidence/资源读取与预览历史证据.md#envi-现行-nfs-初始化扫描身份修正2026-11-04正式复验待运行)：首次scan403，MinIO部分／清理不证六项或页面。 |
| `9e5a94b7e48d654cfcb844deec8d2ddb8d0f1557`／126269226975 | [v3精确组成](../evidence/资源读取与预览历史证据.md#envi-精确双文件现行权限正式复验通过2026-11-04)：六项／原件／四COG／缓存／页面／清理，不含节点。 |
| `516822366b3d976a3cb7d59ba742be616fc158fa`；[41741133610／126282637030](https://github.com/pampa0629/addp/actions/runs/41741133610/job/126282637030) | [v4节点及原六项](../evidence/资源读取与预览历史证据.md#envi-minio-持续节点新建文件及到期独立-hosted-通过2026-11-04)：12次内容判定／5撤销／1到期及历史；早期runner不冒充最后清理修正后的全量重跑。 |

只认证所测Byte／MinIO节点；SKP精确组成仍关闭，任意光谱／64bit整数／复数／压缩／外部资源／导出／其他引擎格式／容量／授权表单全流程未验。

### IMG 原生扫描与 COG 快显

阅读导航：[正文分工与来源](#imgjpeg2000-正文分工与来源) · [COG 与缓存](#imgjpeg2000-cog-与缓存) · [页面与原件](#imgjpeg2000-页面与原件) · [清理与验证](#imgjpeg2000-清理与验证) · [历史证据与未覆盖项](#imgjpeg2000-历史证据与未覆盖项)。

#### IMG／JPEG2000 正文分工与来源

同suite／十项消费权限、Business MinIO／GeoPython产品Runtime／唯一媒体runner，部署沿[组件分工](#部署模型转换与通用清理)。真实Meta深扫的结构与受控内容按公共规则分开核。

| 来源 | 固定夹具与独有断言 |
| --- | --- |
| IMG／HFA | 三份256×256三波段Byte：WGS84／UTM50N／无定位；自包含、同类型、无NoData，核`img/media/single`及HFA。准入归[IMG契约](../../builtin-formats/媒体与地图数据集.md#img--erdas-imagine)。 |
| JPEG2000 | 四份同尺寸三波段Byte可逆无损：GeoJP2 WGS84、GMLJP2 UTM50N、无定位JP2及原J2K；核`jpeg2000/media/single`、JP2OpenJPEG／profile／原大小及hash。准入归[JPEG2000契约](../../builtin-formats/媒体与地图数据集.md#jpeg2000)。 |
| TIFF | 布局／小图／刷新／NFS／到期／MSK归[TIFF专题](#tiff-无定位小图的-cog-布局回归)，不沿用下述无定位提示／按钮规则。 |

#### IMG／JPEG2000 COG 与缓存

各两定位源经`generate_raster_cog`共四受管TIFF／COG，逐任务核`source_format`为`img`或`jpeg2000`、原生Runtime身份、完整像元／mask／CRS／仿射、布局、API／物理hash及大小、Range前后缀／越界416。真实iframe核WGS84／UTM WebGL品红／青／黄及截图；重开同成果／execution、零额外生成，源字节／Meta与原格式身份保持。

#### IMG／JPEG2000 页面与原件

无定位IMG／JP2／J2K须显示“不支持直接预览”，无地图／COG动作、普通图片组件或源`<img>`，不交浏览器解码。页面必须`source_image_absent=true`、`unsupported_message_visible=true`，缺失／假即拒绝。原件逐份按公共API／按钮交付核验；TIFF独立判定。

#### IMG／JPEG2000 清理与验证

按[公共收尾](#文件读取-case-的公共准备与收尾)经Manager API删四成果／任务，核内容404／物理缺失／源预览模式恢复；`manager-img*.json`／`manager-jpeg2000*.json`分别归档。现行子报告`addp.manager-raster-media/v2`、页面`addp.manager-raster-media-browser/v4`，历史版本不追认新版；runner／原生门禁及既有登记核子场景／浏览器／回归接线，不增定时。

#### IMG／JPEG2000 历史证据与未覆盖项

| 范围 | 最小证据与限制 |
| --- | --- |
| IMG早期HFA | [原生链路](../evidence/资源读取与预览历史证据.md#manager-内部产物与-minio-模型入口--hosted-t4)，不证明JPEG2000。 |
| JPEG2000首轮 | [外层成功、独立页面未过](../evidence/资源读取与预览历史证据.md#jpeg2000-初轮--hosted-外层成功独立页面复核未通过)，保留破图／旧报告漏检。 |
| 独立媒体case | [正式收口](../evidence/资源读取与预览历史证据.md#imgjpeg2000-独立媒体栅格-case--hosted-t4)仅当轮七源／四COG／页面／清理，不追认当前报告或full；TIFF见[分版本证据](#tiff-分版本证据)。 |

IMG导出／外部IGE或RRD／依赖／复数或异质波段／自定义WKT／NFS Hosted、JPEG2000外部定位或PAM／JPX组合／NFS或FTP／写出，以及任意厂商／容量未验。二者不替GRD／TIFF断言，亦不认证整轮CI／完整Manager或专业影像生产。

### 部署、模型转换与通用清理

`manager-internal-artifact-lineage`使用GitHub Hosted Linux x86_64 disposable，`online-hosted-manager-gate.sh`复用共享生命周期建Infra、最小User／Tenant／Business MinIO Engine；登录凭据限仓库外当次owner-only Secret目录，不由macOS self-hosted分发。身份／原生进程／产品容器归[T4专用环境](../T4-Online验收规范.md#专用环境)，按case选组件，启动不代表业务已验。

| 组件 | 部署及职责 |
| --- | --- |
| 应用 | 标准开发生命周期选择Security／Meta／Manager／Monitor，表格及部分导出另选Transfer；System／Gateway／Console等依赖由入口带起，不在workflow复制。 |
| PointCloud／Document | 对应分支启动原生Python，按PID／回环监听归属管理；准备／版本检查不替COPC／PDF页面验收。 |
| Model3D | 正式构建Linux amd64转换器及Runtime产品镜像，默认入口运行，不用模拟器。 |
| GeoPython | 共享Hosted helper构建／启Runtime产品镜像，NFS case只读挂载当次夹具。 |

Runtime启动前设置`ADDP_ONLINE_TEST_RUN_ID=manager-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>`，运行标签／业务报告同ID；缺Run ID／attempt在变更服务前失败并清凭据。生命周期模拟保suite标签，其他标签不影响suite归属判断。

full的确定性LAS、3页PPTX、PNG贴图DAE／3DS、IFC4米制立方体、内嵌DXT5红贴图的zlib单OSGB经同轮Meta深扫及正式owner action/task生成COPC／PDF／GLB；模型均核`model_3d/single`、自包含非空网格，格式断言见[八模型](#表格json-与八模型固定断言)。Manager／Monitor输入输出血缘一致，真实iframe须GLB请求成功、加载完成并截图；不扩裁剪case。

Chromium显式SwiftShader软件WebGL、真实绘制不占位。共享测试工具仅精确分类`GPU stall due to ReadPixels`，浏览器报告v5单列来源／阶段／次数（含重投影／预览／截图）；预览诊断不归因截图，限流条数不代表GPU调用次数／无读取。其他业务警告、错误、失败请求仍失败；重投影T3核真实像元及无Canvas getImageData，分类不替渲染验证。

文件case沿[公共收尾](#文件读取-case-的公共准备与收尾)；full按捕获ID经正式API删产物／任务，内容／任务各404、源预览模式恢复，不直接改DB／Infra MinIO。十一类领域／物理清理、PDF／COG独立物理核验均保留，结束停应用／销毁本轮Business容器／网络／卷及Infra。确定性为`make test-online-runner`／`make test-manager-online-runner`，真实为`make test-online ONLINE_SUITE=manager-internal-artifact-lineage`，不增入口／workflow／定时。

### 点云与 PPTX

Fixture owner幂等准备确定性LAS／多页PPTX，Gateway真实扫描得locator／item ID／fingerprint后核：

- `point_cloud_copc_generation`由PointCloud从Business读源、发布COPC至Manager Infra MinIO，Manager execution写`addp.lineage-facts/v1`。
- Quick View Capability声明`generate_pptx_pdf`，首次统一action触发`pptx_pdf_generation`，Document Workflow／LibreOffice发多页静态PDF；再读Capability复用同任务／结果、零第二execution。
- Monitor API／真实浏览器须同业务输入与`addp-infra://`输出；Data Explorer翻后续PDF页，跨Engine状态刷新仍保持页。两类结果／任务按正式API和公共清理核不可读／零残留。

### 彩色 FBX 只读预览

官方`vCube.fbx`原样复制至`model3d/fbx/vCube.fbx`，沿原扫描／十项权限，和缺颜色索引`model.fbx`的GLB几何转换并列，不替换。生成入口／缓存两阶段直接加载彩色源：初始红绿蓝均有足够真实像素，旋转／恢复／重开允许RGB／CMY六系中至少两色；原面积／像素阈值／旋转变化及下载SHA保持。

该源零任务／成果，扫描后及清理末尾即使失败也核源字节／空结果／空任务，再随Business生命周期销毁；原十一类清理不变。新增范围最低browser v12／Owner v10，旧版拒绝；完整报告遵循下文现行绑定，实际Hosted前不认证。

<a id="max-模型-hosted-验收"></a>

### MAX 模型 Hosted 验收

Business MinIO原样CC-BY-SA-3.0马模型＋自建4×4 RGB(220,30,60)贴图；页面核默认米／手选毫米／显式bitmap映射，正式生成`source_unit=mm`。核审计、5静态网格／12 primitive／444顶点、米制尺寸及真实内嵌像素，不固定跨Blender版本PNG编码hash。Meta仅格式身份；MAX阶段六模型＋点云／PPTX／栅格核九项输入输出血缘／缓存／清理，实际Hosted前不算T4。

### 表格、JSON 与八模型固定断言

full沿原十项普通User，含`manager.content.read`下载及`manager.data_item.update`设置／刷新；Owner拒缺项／多项或管理员角色，不扩个人／生产身份。报告版本由当前Owner／浏览器及CI登记绑定，历史不替当前；下列固定夹具保留，格式产品规则归权威规范。

| 来源 | 固定断言 |
| --- | --- |
| CSV／TSV GBK | 两源各25行／784字节，真实设置／刷新核三列中文、20＋5／末页禁用、原大小／SHA及无覆盖再次刷新／重开。切“首行为数据”须`has_header=false`、Column1～Column3、26行（原头为首数据）／20＋6、普通刷新继承／重开，恢复头后25行。两阶段Meta实际模式／编码／指纹／内容hash一致，下载不变，GBK不发布字节索引；表头扩展最低suite／browser v14，旧v13不认证。 |
| XLSX | 完整7087字节，Cities25行2列、Readings2行3列、Empty0行0列、HeaderOnly2列0数据。`format=excel`／`data_type=container`／`layout=single`，不同结构、未知总数20＋5／末页禁用、切表重置、刷新保当前页／空表、重开第一页及完整下载；dimension只是估算。 |
| JSON／GeoJSON | 嵌套文档与25行records分别文档／20＋5表格；相同Point／LineString FeatureCollection的`.geojson/.json`均走唯一`preview_type=table`地图＋表格，核完整属性／几何、4326／未变换CRS、真canvas／点击、末页／下载／刷新／重开；普通文档不误入records／空间，不外推轨迹。 |
| 八模型 | DAE／3DS核贴图引用／内嵌PNG；IFC核STEP schema／实体摘要／米制几何；OSGB核内嵌JPEG／UV三角形；SKP核英寸→米／共享双实例／彩色PNG、无虚构解析元数据；MAX按上节；FBX核角点／变换／颜色／12三角面；OBJ核尺寸／UV／PNG像素／不透明度，基础预览仅几何。均核`model_3d/single`／自包含GLB／真实网格；[固定FBX预览未认证边界](../../../../manager/docs/三维模型、点云与高斯泼溅预览说明.md#二数据类型与预览路线)保持，旧SKP套件不证明当前关闭的精确组成授权。 |

**版本绑定索引**：同suite独占NFS私有根的两ASCII源精确深扫后，核版本绑定稀疏索引／两页；宿主同大小改字段与值、显式推进mtime，两页须`409/preview_source_refresh_required`、无列／行／总数、不自动改Meta，User仍可下载实际字节。原item刷新后身份保持／绑定更新／两页新值。成功／失败均先还原字节、刷新、复核原页／下载再删私有导出。写前核Hosted Linux x86_64／私有根／Run owner／无符号链接。

MinIO沿同预览／409／刷新／分页／下载断言，只用既有Business与固定mc：在Run私有根／owner核验后，仅改`index-source-versions/source.csv|tsv`，动作限对应change／restore，拒绝已知原／变更字节之外对象及任意路径／数据。核同大小改写ETag改变，成功／失败均还原／回读，容器销毁零残留。四storage×format案例在`index-source-versions.json`证据v2，Owner v13／browser v13；旧v1不替四案例，未取得新版本完整Hosted前MinIO源变化仍未验，不认证外部S3／一致性快照。

**原件与成果**：生成／缓存两阶段FBX单主文件核SHA；OBJ仅含model.obj(97字节)／model.mtl(45)／texture.png(76)的ZIP，共218字节。API、浏览器捕获、Owner／退出共用Python标准ZIP字节校验器，核包大小／SHA及每成员完整原字节／大小／SHA，拒重复／额外／加密／损坏；DataItem整体与主文件大小分开。两阶段通过且清理后源成员不变；表格／JSON不建派生产物。OBJ修正报告统一v15、拒v14，不新增包／产品接口。

**公共执行事件**：复用十一转换执行，按Monitor事件ID游标到末页核四成功任务`model_3d_glb_generation/pptx_pdf_generation/raster_cog_generation/point_cloud_copc_generation`，不增任务／权限／预算。分页闭合／字段／UUID、首claim attempt=1、ID递增、带时区保留窗口时间及唯一started首／completed末必须正确；progress零至多条、仅整数百分比，不固定数量，拒截断／失败终态。生命周期首末无步骤／计数，过程无参数／样本／Runtime正文／lease／授权。`manager-execution-events.json`（`addp.manager-execution-events/v1`）及完整报告仅存UUID／类型／attempt／事件页数／安全结果，不存完整响应。事件不替结果／血缘／清理，不外推其余八任务、失败／过期／进度写入T4。

固定输入／报告缺失或变化拒绝／生命周期由原runner和Hosted登记核对；实际源码、失败、清理与覆盖范围统一查[证据索引](../验收证据索引.md#manager-预览与私有导入历史证据)。

## 拓扑构面、面叠面缝、悬点与线叠持久化（vector-topology-persistence）

### 拓扑持久化公共要求

本节主场景及多距缓冲、面界提取／诊断、线端修剪、重合规整／组合业务case使用独立手工Hosted Linux x86_64 T4，入口 `make test-online ONLINE_SUITE=vector-topology-persistence`。复用共享生命周期、NFS export所有权和正式GeoPython产品默认入口；当次disposable非默认Tenant、同一普通User、600秒预算及精确11权限不变：`develop.task.read`、`develop.task.execute`、`develop.data_read.execute`、`develop.data_write.execute`、`system.execution_authorization.create`、`meta.catalog.read`、`meta.scan_task.read`、`meta.scan_task.execute`、`meta.lineage.read`、`monitor.execution.read`、`manager.data_item.read`。Engine登记身份独立，登记后撤基础设施Token，不复用个人会话。

算子准入及算法归[矢量算子契约](../../workflow/矢量算子契约.md)，本页只保留固定夹具与验收差异。以下要求按各case明确的源、端口和计数核验，不能把独立case通过当主场景重验：

- **持久事实**：保存均`mode=fail`，稳定outputs／lineage_facts.outputs均`write_mode=create`。每成果恰一次精确自动扫描，公开`tenant_id/triggered_by`为匹配当前User／Tenant的严格正整数；不读私有字段替代、补扫或接受重复／缺失。物理与Meta大小为相等严格正整数，GPKG区分文件与唯一child边界。
- **血缘与来源**：输入以`input.<读取节点ID>`精确绑定locator，拒缺项／重复／额外／端口错绑，不按数组顺序认身份。每个源→成果须有本execution的active derive，Monitor与facts一致；文件血缘不代行级`source_id`／members，业务来源不写入Meta技术字段。
- **独立核验**：固定手写原坐标／属性／类型／行序／关系，经独立原生OGR读取核对，不调用生产算法造期望、不舍入或加容差／精度网格。存储FID与业务ID分开，篡改必须拒绝。每端口在唯一几何类型／数量表登记，API与真实地图共用；未知端口、少／多行或错类型均失败，runner须实际消费该断言。源Meta及源／成果原字节、大小／SHA在浏览器前后保持。
- **真实页面**：逐次导航从页面自身响应经`browserAPIFromResponse`核当前User／Tenant／11权限，不跨导航持旧Token、mock响应或注入图对象。GPKG用唯一`child_name`、标准EWKB；核完整属性／原坐标及实际OpenLayers要素。显示坐标由独立球面墨卡托oracle算3857→4326→3857，逐值精确比，不用容差。主场景三幅Point图核共享蓝色非透明前景、不计底图水域，其余十二幅线面图核绿色；分别存`point_pixels/green_pixels`，不改产品样式。保留微小原始几何，但不宣称亚像素可单独目视或分辨。
- **失败与收尾**：计算失败须阻断下游保存、无发布／新增扫描／输出facts，源及成功成果不变；不承诺保存跨文件事务回滚。阶段`incomplete`不替最终Owner／browser完整成功。领域、NFS export／owned root、Runtime／registry、部署／Infra均清理核零残留，清理失败即门禁失败；归档合成原件不是业务残留，部署销毁不认证Manager业务DELETE。未实际运行不计T4，不扩Quality／修复／容量或定时，不重复旧业务与容量矩阵。

### 主场景的固定输入、输出与扫描血缘

同一成功execution各分支使用以下十五源／十五成果；端口映射及文件名均须精确一致，共十五次自动扫描、十七条当前execution active derive。表内多源均分别连向成果，多输出均分别保留来源：

| 固定源 | 成果／端口 |
| --- | --- |
| `source.geojson` | `faces.geojson`、`diagnostics.geojson` |
| `invalid.geojson` | `repaired.geojson` |
| `snap-input.gpkg`、`snap-reference.gpkg` | `snapped.gpkg` |
| `overlap-input.gpkg` | `overlaps.gpkg` |
| `gap-input.gpkg`、`gap-coverage.gpkg` | `gaps.gpkg` |
| `dangling-input.gpkg` | `dangling.gpkg` |
| `line-overlap-input.gpkg` | `line-overlaps.gpkg`（line_overlaps） |
| `line-self-overlap-input.gpkg` | `line-self-overlaps.gpkg`（line_self_overlaps） |
| `line-intersection-input.gpkg` | `line-intersections.gpkg`（line_intersections） |
| `line-self-intersection-input.gpkg` | `line-self-intersections.gpkg`（line_self_intersections） |
| `split-line-input.gpkg` | `split-lines.gpkg`（split_lines） |
| `line-component-input.gpkg` | `line-components.gpkg`（line_components） |
| `line-chain-input.gpkg` | `line-chains.gpkg`、`line-chain-members.gpkg`（line_chains／line_chain_members） |

### 主场景的独有几何断言

除构面／修复的原CRS84外，下列GPKG均EPSG:3857；线源显式唯一非空字符串`route_key`、面叠显式唯一非空`parcel_id`，`note`等原属性保持。各算子的完整输入拒绝／空批／输出Schema归正式契约，此处固定输入与精确结果如下。

| 分支 | 固定计算与独立断言 |
| --- | --- |
| 构面／修复 | GDAL核四面／四dangle；自交面检查→修复→复查后仍两行MultiPolygon，各含两三角面，原属性不变，修复前false／原因、后true／Valid Geometry。GeoJSON保留CRS84声明，Meta为OGC:CRS84、无虚构EPSG SRID，地图`direct_renderable/not_transformed`，不临时选CRS。 |
| 严格参考吸附 | 五输入线／六参考线、米单位，tolerance=.75核原始位置上限／严格边界／等距XY、不插点或吸向段内部，属性／行序／XY／CRS不变。tolerance=1.25另DAG须以Owner固定`snapping produced invalid geometries`整批失败。 |
| 面叠 `check_polygon_overlap(id_field=parcel_id)` | 十二有效面→七对MultiPolygon／Float64面积：五普通重叠／包含／全重复、一含两面部分且排接触线的多部件交集、一精确2^-40（约9.09e-13）微叠。核双ID／顺序／每对唯一；共边／共点／孔内不输出，不忽略微面积。 |
| 面缝 `check_polygon_gap(input_gdf,coverage_gdf)` | 七待查＋七显式覆盖面→八Polygon稳定排序：内／边缺各一、双孔缺口一、待查孔一、点接触及独立多部件三、2^-40微缺一。范围孔是合法留白，完全覆盖／范围外不报，覆盖重叠及源重复不重复报。核原坐标／孔／顺序／Float64 gap_area；两源血缘不可漏／互换，不自动填补。 |
| 悬点 `check_dangling_points(id_field=route_key)` | 十二有效线／多线→十七Point＋source_id，含孤立／连续链／T形／闭环／反向重复／内部重合／多部件及2^-40断口。闭环／T连接不报；同点重复线保留双来源，同源多部件同点仅一次。OGR GetPoints/GetX/GetY逐坐标／来源／原行序核，不用生产打断／计度造期望。 |
| 线叠 `check_line_overlap(id_field=route_key)` | 十六有效线／多线→九MultiLineString＋source_id_a/source_id_b/overlap_length；覆盖部分／包含／反向重复／三要素逐对、多部件排点接触、2^-40正长、源内折返去重及不同采样顶点。孤立自叠／点接触／交叉／间距2^-40平行线不报。长度按原单位，用手写坐标及独立线段长度核。 |
| 线内自叠 `check_line_self_overlap(id_field=route_key)` | 十五有效线／多线→八MultiLineString＋source_id/overlap_length：折返／三次经过／不同分段／多部件正长叠／2^-40／夹零段折返／同向重复部件／重复闭环。同源同位置只计一次长度；普通闭环／点自交／近邻／单零段／仅端点接触／两个独立重合源不报。 |
| 线交 `check_line_intersection(id_field=route_key)` | 十八有效线／多线→十Point＋双ID：三线同点三对、T形／端点、线点混合中的独立点、重复多部件两交点及间距2^-40双点。重合段端点／近邻／单源自交不报；拒丢点／重复／错配／微点舍入／CRS篡改，双ID仍绑定同一源文件，不虚构双文件血缘。 |
| 线内自交 `check_line_self_intersection(id_field=route_key)` | 十六有效线／多线→十五Point＋source_id：蝴蝶结／重复到访／跨部件接点／重合覆盖外支线点／重合与独立点并存／微距双点／不同源同XY／跨部件闭环四角。正常折点（含中间零段）、同部件闭环首尾、重复分段／相邻折返覆盖、自叠内部及端点支线、两不同源仅互交不报。原生Point不经默认GeoJSON截断；按源序／XY排序，拒全层去重或错误覆盖点。 |

悬点静态排序回归只导入`geometry-preview-coordinate.js`的纯数学投影，不载含OL解码的`geometry-preview-support.js`，不要求node_modules；真实地图／解码仍需前端依赖。地图Point按完整source_id＋显示坐标在同一Node环境确定性身份排序，不用浏览器语言排序，不去重；物理／API原行序仍精确核。面叠／面缝像素取实际可见区域，完整微几何仍须存在。

**交点打断**：`split_line_intersections(id_field=route_key,scope=all)`以十七有效线／多线生成四十三LineString，保留全部原属性、可空note／Int64 grade，追加source_id及从1起part_no/segment_no。手写预期覆盖十字／T／端点／逆向多部件／正常闭环接缝／2^-40片段／重复顶点，以及同源蝴蝶结与跨源同时切分、反向自交闭环原接缝、同XY多次经过的独立路径位置、同源跨部件交会和切点重复原顶点。原方向／每顶点／源部件序／类型／null／编号必须相等，不按全源XY去重漏切；正长重合仍整批拒绝，沿Native负例，不另加T4失败DAG。文件血缘不代片段来源。浏览器须真实控件选每页50，再用同locator／child／page=1／page_size=50页面响应核43行及全部OL属性／方向／坐标，不注状态／另API代页面；旧between T4不认证all。

**连通分组**：`group_line_components(id_field=route_key)`十一有效源线／多线→十五原LineString部件／九稳定组。含同源三分离部件各组、同源微断口跨组、同源端点相接同组、后到桥接线传递连通，另核T／十字／反向叠／孤立环／源内自交自叠／重复坐标。手写组号／原坐标，按源行／部件序核全部属性、source_id、Int64 part_no/component_id、方向／顶点／闭环接缝及2^-40断口，不按同源强归组。独立OGR／真实保存重载及错组／来源／属性／类型／方向／微点篡改拒绝；端口类型数量严格十五LineString，不能落默认计数。历史v11不含此扩展。

**链路合并**：`merge_line_chains(id_field=route_key,match_fields=[road,grade])`十六源要素／十八部件→十二LineString链及十八members。源含唯一route_key、可空note／road、Int64 grade；链保共同属性及chain_id/member_count，成员保全部原列及chain_id/member_no/source_part_no/Boolean is_reversed。手写关系覆盖中段最早出现／反向拼接、全图三岔先于属性、属性断链、NULL road一致、同源部件跨链／同链、闭环接缝、内部重复坐标及2^-40断口；OGR核原方向／全部坐标／null／Integer64／Boolean subtype，拒错链／序／反向／类型／微点／CRS／数量。两个成果各一来源关系，不用文件血缘冒充成员。原v12只证当轮，不认证此后范围。

### 主场景报告、失败证据与历史范围

完整主场景须十五地图＋成功／塌缩失败页共十七截图。报告唯一`addp.vector-topology-persistence/v14`、`addp.topology-browser/v14`、`addp.topology-checkpoint/v14`，拒v13，Owner／browser／阶段证据均绑定`split_line_scope=all`。有界只观察当前失败execution的隔离Owner日志，不重试业务；浏览器前先存`topology-snap-failure.json`的最小公开观察／固定首因／Monitor／未变扫描清单，`result=incomplete`。

等待血缘前存`topology-before-lineage.json`，包含十五严格大小／公开扫描身份／物理hash／稳定outputs与facts／原Monitor，仍为incomplete。`topology-source-lineage-raw.json/v13`保十五源原Meta、十五原图和真实预览响应；`topology-physical/`保十五源／十五成果共三十合成原件，逐个SHA／大小核并拒覆盖不同证据，可离线GDAL复核。最终Owner／browser完整成功、十五图与退出清理实际通过才认本轮T4。

| 固定版本与最小证据 | 已验结论及限制 |
| --- | --- |
| `d9a3cec22e12db098182e25bc96a62e0916f5f32`（含打断接入`90a9eab843ecf38538a601f15dd0add5e9af24a9`）；[Hosted 40080467804／Job120832764807](https://github.com/pampa0629/addp/actions/runs/40080467804/job/120832764807) | 2026-10-24 User33／Tenant2／11权限，v11十三源／十二成果／十二扫描／十四derive；打断仅十一源线→二十LineString，属性／null／Int64编号／方向／顶点／2^-40保持。GPKG98304字节、二十五原件及十二API／OGR／OL几何核对，十二地图＋成功失败页十四截图；115726ms，塌缩无发布／扫描，NFS／部署／Infra清理零残留。artifact12555218473；checkpoint不替最终报告，旧结果不认证当前十五成果／all。 |
| 后继源码`5a5614564b38115b38f4091fc1742a46da5cae0e` | [GeoPython](https://github.com/pampa0629/addp/actions/runs/40080291973/job/120832242674)4797 passed／21 warnings；[Console](https://github.com/pampa0629/addp/actions/runs/40080291973/job/120832242861)195单测／219浏览器／build；[Platform](https://github.com/pampa0629/addp/actions/runs/40080291973/job/120832242706)通过。此前本地未改HMR公共夹具194过／1败仍保留，不倒推本地通过或外推当前main／其他Job。 |

旧构面／修复／吸附／面叠／面缝／悬点／线叠／线内自叠T4不外推新增线交／线内自交；Native／CI不算新真实T4，容量及业务DELETE未由上述清理认证。

### KML／GML／TopoJSON 的 S3 兼容消费

本小节属`manager-internal-artifact-lineage`，同Business MinIO／GeoPython产品Runtime／十项普通User，三份固定hash双图层源；不继承上节拓扑11权限。核验差异：

- scope-table几何为HTTP JSON Base64 EWKB，用同一解码入口核原始／刷新几何、类型／完整坐标／Z，再独立核真实地图／像素，不要求改返GeoJSON。
- 未知CRS TopoJSON须省略`source_srid/source_crs`（omitempty），`transform_status=unknown_crs`且默认暂停；不虚构source_srid=0／4326。临时选择／清除／切换／重开及原始几何分别核。
- 活动控件定位`button.ol-zoom-out`，可访问名是“–”而非提示“Zoom out”；同一中英文T3／Hosted helper真实连续点两次，各等动画并核缩放减1，再验像素／原几何／实例。另核普通刷新／原件下载hash。
- 隔离环境先Security，标准身份夹具等待Manager持久保护投影首同步；预检核健康／源码，启动／初始化失败则拒入场并清理。Owner／browser v13拒旧版，`make test-manager-online-runner`核夹具／拒绝／浏览器语法／登记。

不改血缘实现、服务／权限／workflow；MinIO只证S3兼容，不认证外部云。真实T4按当次源码／报告／清理独立认定，此处不登记通过。

### 设备静态轨迹范围读取

同Manager suite／Business MinIO／十项User，无新增服务／任务／成果／权限。产品字段／预算／保护归[Manager预览规范](../../../../manager/docs/数据预览与资源树实现规范.md#六设备静态轨迹预览)；只读夹具和页面断言为：

- CSV／records JSON／Point GeoJSON各3000合成点，A／B各1500小时；UTC `[2026-01-01T00:00:00Z,2026-01-02T00:00:00Z)`为双设备48／A24点。独立表单核设备／时间／坐标／像素、请求与`trajectory_scope` selection一致、`complete=true/scanned_rows=3000`。
- 整文件拒前2000点绘图并保完整性提示，GeoJSON自动CRS不清提示，重开重验；同User超限API `422/trajectory_point_limit_exceeded`，缩回恢复，不能统一豁免其他业务失败／警告。
- 三源大小／下载SHA不变，正式下载helper不被探针替代；像素只读复制至独立离屏画布，不改地图／过滤警告。当前Owner／browser／CI登记核完整三源、拒缺失／篡改／旧版；历史Owner v9／browser v11只识别当轮。
- 既有生命周期删源，原模型／PPTX／COG／点云领域与物理清理仍保留，Infra不替领域零残留。

源字节／大小／下载登记、两次生成一致／三格式等价及3000／48／24计数／报告拒绝归`make test-manager-online-runner`／`make test-online-runner`；实际范围查[静态轨迹历史证据](../验收证据索引.md#manager-静态轨迹历史证据)，不以runner代手工Hosted。

### 多距缓冲独立场景

`topology_case=multi-distance-buffer`沿[拓扑公共要求](#拓扑持久化公共要求)，仅两个3857点源／一个六行GPKG，full／规整／组合含义不变。成功源含两个业务ID、非标准活动几何列、可空Integer64／Boolean／String；冲突源另有`distance`列。成功`load→batch_buffer(distances=[2,1,2],resolution=1)→save`是完整缓冲非环带，按距离再源行，重复距离保留。

OGR以原点手写六闭合菱形，核精确坐标／方向／序／包络／面积／顶点半径／二维有效／CRS／类型／NULL及Real distance，不跨GEOS版本buffer造期望。两源各一次初扫，成功血缘仅成功源→成果；一成果一次扫描／derive、create、严格正整数大小／Monitor／全原件／前后保持按公共要求。

冲突源另DAG在batch_buffer以准确distance冲突原因失败，零下游文件／扫描／输出facts，两源Meta及成功成果不变。Console成功页／六面图／重名失败页三截图，核EWKB／FID区别／全属性／OL投影数组／像素。报告`addp.topology-buffer/v1`、`addp.topology-buffer-browser/v1`绑定case／run／身份／当前execution／完整预期，保原始预览／血缘图；incomplete不替最终，收尾按公共规则。

既有runner／Native／Console／workflow及mutation回归覆盖接线，无新suite／权限／路线／永久脚本。超JSON安全整数、零负距离、空层及其他参数仅沿已有确定性证据，不认证本case新增T4或容量／受控对照。

### 重合规整独立场景

`topology_case=line-network-normalize`沿[公共要求](#拓扑持久化公共要求)，只一3857 GPKG执行`load→normalize_line_network(id_field=route_key)→result/members各save`，不seed／执行旧十五源成果或塌缩／容量。未知case在副作用前拒绝，workflow输入贯穿owner／物理夹具／浏览器／报告，不算full重验。Playwright按含文件名前缀的完整标题末尾精确选例；runner用真实`--list`证normalize／full各唯一一项，列表不执行业务。平台沿已有前端action装Console／Manager锁定npm依赖，不需浏览器／服务。

五显式字符串业务ID与FID分开；折线原顶点／连续重复坐标、反向部分叠／十字、同源多部件多次经过及不同采样点→手写九唯一LineString／十七经过明细。唯一边仅几何＋Integer64 edge_id/occurrence_count；members保全部业务列／null／Integer64 grade／Boolean enabled及来源／部件／原段／片段号／Boolean is_reversed。精确坐标／方向／序／关系核验；Native保存须原Integer64，不按输入dtype接受float。

两成果各一同主体扫描／derive／create／严格大小，原件一源两成果；真实两地图核全九／十七行属性／EWKB／FID／3857投影／绿色像素，源Meta不变、Monitor一致，members业务来源不塞Meta。失败DAG引用不存在来源列，Develop／Monitor须同`develop.workflow.invalid_params`，定义序第2节点`normalize_line_network`报`operator_input_invalid/input_gdf`，不靠日志自由文本；双save零发布／扫描／facts，源／成功成果不变，不承诺保存事务回滚。

成功／失败页＋两图四截图；最终`addp.topology-normalize/v1`、`addp.topology-normalize-browser/v1`显式case／Run ID／身份／execution／源／双端口预期，保原图／预览／OL／物理证据，incomplete不算passed。清理及接线沿公共要求，无新suite／权限／容量。

### 重合规整与链路合并独立组合

`topology_case=line-network-normalize-chains`沿[公共要求](#拓扑持久化公共要求)，一3857源执行`load→normalize_line_network→merge_line_chains(id_field=edge_id,match_fields=[])`；chains／chain_edges／edge_sources三save均显式依赖合并成功。原full／规整含义不变，无新算子／API／权限，不跑旧整包／容量。

十一要素→手写九链／十五唯一边明细／二十二经过明细，覆盖共享边／四方向／分支／闭环／多部件重复／微断口／零长原段。业务ID为超32位但在JSON安全范围内的2^40 Int64；保NULL／Boolean及原业务chain_id/member_no，Native 2^60不证浏览器超安全整数。按同次edge_id核两级关系／方向异或，唯一边与经过分别计数；原属性不进主链、来源关系不塞Meta，正式关系与字段归[显式几何组合](../../workflow/矢量算子契约.md#重合规整与链路合并的显式几何组合)。

每成果一同主体扫描／derive／严格大小／Monitor，归档一源三成果原件、三原图／源Meta／真实预览。三幅LineString地图核全9/15/22行EWKB／原生类型／FID／原坐标与OL投影／像素；成功／三图／失败页五截图，源及成果前后不变。失败DAG引用不存在合并来源列，Develop／Monitor同`develop.workflow.invalid_params`，定义序第3节点`merge_line_chains`报`operator_input_invalid/input_gdf`；三save零发布／扫描／facts，不承诺保存跨文件事务回滚。Owner／browser绑定case／同run与身份／execution／三精确期望，阶段incomplete不替成功，清理及runner／Native／Console／workflow／mutation接线沿公共要求。

**固定通过**：`afc6a18aa01338850f690083bc4ba8834ae0d1f6`，[2026-10-27 Hosted40501760127／Job122180927931](https://github.com/pampa0629/addp/actions/runs/40501760127/job/122180927931)，User33／Tenant2／11权限，11源要素→9／15／22、三成果各98304字节等于Meta，三扫描／derive及Monitor一致。独立OGR以原生坐标／类型／精确分数核来源／片段／四方向，EWKB／实际OL／三图像素＋五截图已验；缺列负例零发布／扫描／facts、源／成果保持，业务31562ms、全部退出清理零残留。原扫描库存／逐页身份有同轮断言但未单独归档原库存／AuthContext响应，不认证业务DELETE、亚像素肉眼可见或超JSON安全整数。

同SHA [Console](https://github.com/pampa0629/addp/actions/runs/40501463449/job/122179912108)195单测／233浏览器／build、[GeoPython](https://github.com/pampa0629/addp/actions/runs/40501463449/job/122179912720)7622项／24警告通过；[Platform](https://github.com/pampa0629/addp/actions/runs/40501463449/job/122179911829)因Catalog登记在四合成仓库读缺失Makefile失败，原已交公共owner，不算整轮Platform通过。本轮未重验旧业务／容量，组合容量仍未验；本地／代码就绪不算新T4。

### 面界提取独立普通用户场景

`polygon-rings`仅一成功／两失败，沿[拓扑公共要求](#拓扑持久化公共要求)的Hosted／非默认Tenant／普通User11权限／600秒，不跑full／旧业务／容量。三份3857单层GPKG分别成功、ring_id冲突、自交无效面；成功`load→extract_polygon_rings(id_field=parcel_key)→save(mode=fail)`，四源面→八闭合LineString（多部件五环＋相邻／重复／精确微坐标面各一）。保环起点／方向／重复顶点／共有边每次记录、源序／部件／外后内；逐字段核source_id／part_no／ring_no／ring_id／is_hole及全部属性。

parcel_shape活动几何与普通String geometry分开，来源是中文业务键非FID；nullable Int64／Boolean／String／Real及大整数核原生类型／值，不据JS Number猜类型。GPKG物理几何名按save事实，不要求等于内存名。rings.gpkg的stable_outputs／facts均create，一次同主体公开整数tenant_id／triggered_by扫描、严格正整数physical==Meta／单child八行／全字段、一条当前execution active derive及Monitor／原图一致，不给内存环建DataItem。

两负例均带save而无文件／facts／scan／derive，同UUID隔离日志核准确原因，失败页／Monitor同User／failed；三源＋一成果共四扫描库存不变，失败／浏览器后源／成果SHA／大小／源Meta保持。每次导航browserAPIFromResponse核身份；原EWKB／八OL线完整来源编号坐标／真实像素，不能凭像素区分重线或微线。成功／线图／两失败页至少四截图，另核真实血缘tab原图／单边画布；浏览器只读，不补扫／重放／额外写入。

Owner／browser／raw仅case/v1，incomplete不替最终。归档四原GPKG及API／Meta／血缘／Monitor／渲染，独立OGR复核完整坐标／类型／属性／序／3857／SHA与大小，期望不调产品／舍入／加容差。清理及runner／Native／Console／登记／mutation沿公共边界，不认证业务DELETE或新增suite／权限／永久入口。

### 面界提取参数诊断独立实页

`polygon-rings-input-diagnostics`仅ring_id冲突／真实无效面两失败，不跑旧成功／旧四参数场景／容量。沿拓扑11权限／600秒，只两源`rings-conflict.gpkg`／`rings-invalid.gpkg`，保parcel_shape及普通geometry。失败节点是冻结tasks数组首项、在load后调度，save不得执行。

Develop／Monitor同UUID／User／冻结定义，准确v3／execution／INVALID_PARAMS对应`develop.workflow.invalid_params`；冲突仅field=ring_id，无效面仅operator_geometry_invalid且无field。两execution各在Develop／Monitor中英实页刷新同UUID，共八原始响应／截图／真实剪贴板核验；用唯一共享面板与当前请求身份，不跨导航持Token。核原因／人工重命名或先检查后显式修复建议、算子／节点／参数／固定字段／稳定码／UUID，不泄自定义节点名／业务ID值／坐标／路径／GEOS正文。

两源字节／大小／SHA／Meta及两初扫库存保持，零新文件／成果扫描／facts／本次derive。存全原证据／源，最终不由incomplete替代，NFS／ownedroot／Runtime／registry／应用／Infra全清，不算业务DELETE；沿原runner／Native／浏览器／登记，无新suite／权限／API／容量入口。

### 线端修剪独立普通User验收

`line-trimming`仅一成功／八失败，沿拓扑Hosted／非默认Tenant／同User11权限／600秒，不跑full／旧业务／容量。成功双源3857、max_distance=4：15 MultiLineString／16部件＋14参考，手写15线结果／9目标Point。覆盖原两端冻结、累计路径非直距、上限等号／略超、首次交会／共线入口、同点全来源／单参考重复段去重、分支阻止／命中、重复内点／部件／方向、已接触／闭环／非悬端／无目标不动及2^-20微距。

ID是JSON安全Int64而非FID，source_shape／reference_shape与普通geometry并存，nullable Int64／Boolean／String保持。活动几何名只属内存断言，物理／预览按save驱动实际名绑定，不承诺持久列名。Point是切点，原端存original_x/y；明细仅双ID、part_no/end、原端／切点XY／累计distance，不复制任意业务属性。

八失败：非正上限、源ID／参考ID非法、CRS异、单部件自交、源正长重合、原两端计划相遇、首交点不可精确落线但后有可表示点。前七按v3核闭合原因／参数／冻结节点，无field或私值；第八保持EXECUTION_FAILED，同UUID隔离日志核首交点不可精确表示，不作参数错。每DAG含双save，冲突夹具先有可修剪要素仍整批零发布；逐次核Develop／Monitor同身份UUID、零目标／facts／新scan／derive、库存不增、全源／成功成果字节／Meta保持。

双稳定输出／facts=create，两自动扫描公开严格整数tenant_id／triggered_by同主体；每成果双源当前derive，共四边，失败夹具不进成功图。独立OGR逐原坐标／全属性类型与null／序／方向／部件／3857核，不调产品造期望／epsilon／舍入。归档11源＋双成果，physical==Meta严格正整数；真实成功页、线／Point地图属性血缘及八次Develop／Monitor失败页，核EWKB／完整OL／明显切点见证与Point层像素／原图／截图，逐导航用当前请求身份。微距只核物理／API／OL，不证亚像素可目视。

最终Owner／browser／raw同case／run／execution完整passed，incomplete仅阶段；末轮保持及NFS／ownedroot／Runtime／registry／部署／Infra清零。不认证内存索引／中间算子资源血缘／跨文件事务／业务DELETE／空批／英尺／容量。接线沿原runner／Native／Console／登记，最小门禁后才单次手工本case，不据脚本就绪填通过。

## 端点吸附手工容量

既有`make test-vector-topology-capacity`／`vector-topology-capacity.yml`的独立`profile=endpoint-snapping`，五类十五档、每档一次串行，不跑旧矩阵／受控对照／业务T4。3857／容差1／同源码产品镜像Runner，4 CPU／2 GiB／`memory.swap.max=0`、每档全流程180秒；Runtime准备／清理另记，不继承拓扑业务600秒。镜像构建前按根`.node-version`备Node，登记拒缺失／条件跳过／晚准备，不依赖默认版本。

| 类型 | 三档与完整明细 |
| --- | --- |
| 稀疏 | 源／参考各1000／10000／100000；每四源一移动／严格边界／原位接触／无候选，N/4明细。 |
| 密集 | 源／参考各128／512／2048，每待移端点K参考且唯一最近，K明细。 |
| 同点 | 各128／512／2048，保全部S×R＝16384／262144／4194304明细。 |
| 单参考重复部件 | 1000／10000／100000部件，源1条、仅1明细，不因参考重复扩增。 |
| 单源原始部件 | 1000／10000／100000部件、参考1条，K明细，保编号／方向。 |

精确二进制几何／唯一可落线目标，不改吸附。180秒含造双源／load／双源全检／生产snap_line_endpoints／线与Point双save／OGR完整重载／源文件Frame保持，不抽样／截明细／只比摘要，流式预期不调产品。核全坐标／来源／属性null／Int64／Boolean／Float64／序／部件／方向／CRS及原点／目标／距离，物理hash与全行摘要另存。

observer=A不包产品，外部0.25秒采样及cgroup整体memory.peak／CPU／OOM／退出码；阶段缺采null，不把RSS高水位之和／采样峰值称精确阶段峰值。固定源码／实际加载函数／双Frame文件hash、阶段时长、整体资源、双成果数量大小摘要及档清理均绑case／measurement ID。成功／超时／cgroup证OOM／原生崩溃／其他失败分列，不扩预算／重试；只证本档实测。归属容器／镜像／registry／合成数据清零，不认证scan／血缘／页面／普通User授权／跨文件事务，旧endpoint-snapping T4范围不变。

## manager-hybrid-search

Manager 混合检索类 T4 使用同一专用 Business MinIO Fixture 和永久 MinIO Engine Instance，Fixture owner 额外幂等写入仓库内确定性 JPG。专用 Tenant 必须通过正式 Inference 与 Manager 配置 API 预置支持文本和图片、维度为 2560 的 Model Profile 及 `semantic_search_embedding` 场景绑定；Provider Credential 只保存在 Inference 加密存储，Online 环境只声明预期 Model Profile ID。suite 不创建、更新或删除 Provider、Deployment、Model Profile、Credential 和场景绑定，而经 Gateway 触发真实 Meta scan 与 Manager ad-hoc embedding，验证 Inference、pgvector、Meilisearch 和 Manager 对称 RRF 融合的完整链路：同一 `document_id` 的关键词与向量候选只能返回一次，纯向量候选必须在融合后参与统一分页，响应不得包含旧 `vector_hits`。每轮 embedding 结果必须通过 Manager 正式 API 删除并确认零残留；新增 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得加入夜间调度。

## vector-pmtiles-workflow

矢量 PMTiles T4 `vector-pmtiles-workflow` 仅手工在 GitHub Hosted `ubuntu-24.04` x86_64 独占部署运行，复用共享 Hosted 生命周期、标准 GeoPython 产品镜像和独立 source／target tmpfs MinIO。必须先启动 Security，preflight 同提交核验 Security、Develop、Meta、Manager 和 Monitor；身份夹具通过统一 `--wait-protection-owners develop,manager` 在导出凭据前只读观察该租户两个 Owner 的持久首次同步状态，最多等待 60 秒；读取错误或超时立即停止并清理，不写入 checkpoint，不绕过保护门禁或重试业务执行。非默认租户普通 User 经公开 Develop API 新建两点 GeoJSON 的纯几何 PMTiles，再用 50 面 Shapefile 和显式 name／rank 字段替换同一目标；独立 GDAL oracle 核对 PMTiles v3、gzip MVT、层名、业务字段、数量及按瓦片量化误差约束的坐标。重复 create 必须返回受控 target_exists，且文件哈希／大小／ETag／修改时间、源对象、扫描记录、DataItem 详情及当前血缘完全不变。replace 必须保留目标 ID／指纹并更新文件内容、自动扫描和唯一当前 derive 来源，不把当前图当作历史 observation 证明。每次成功均核对物理大小与 DataItem、原生 header_hash 与 Meta 格式事实，并由同一 User 真实登录 Console，检查 min_zoom=0／max_zoom=5、no-store 请求与响应、瓦片内容哈希、页面解码要素数及地图绘制。失败或成功退出均清理当次应用、业务容器、产品 Runtime／镜像、registry、Infra 数据卷和凭据；清理失败使门禁失败。标准离线入口为 `make test-vector-pmtiles-online-runner`，接入 `test-online-runner`；真实 GDAL oracle 用例接入既有 `make test-geopython-workflow`，由现有 Platform CI 调用，suite／CI／身份／清理登记由 Online 注册检查覆盖。提交 `3eed180b927916d76f1385e63cf8b213d32149a2` 的 [Hosted T4 39037436667](https://github.com/pampa0629/addp/actions/runs/39037436667/job/117410000383) 已通过上述完整 create／冲突保护／replace 场景：两轮地图分别显示 2 点与 50 面，目标身份稳定，自动扫描和当前血缘换源。7 个服务预检同提交，Runtime 留存同次源码构建、实际镜像 ID 与默认入口核验记录。清理证据仅覆盖当次隔离部署及 Infra 零残留，不代表 Manager 受管结果 DELETE API；T4 瓦片留痕为 HTTP 200／no-store／内容哈希，未单独采集 S3 HTTP Range 请求日志，Range 边界另有 Manager T1 回归。不登记 schedule，不覆盖新增 NFS／外部云 S3 认证、历史血缘、容量或十类全算子认证。

## raster-workflow

本节保留固定场景与验收差异；产品语义统一见[栅格计算与成果契约](../../addp栅格计算与成果契约规范.md)，算子细节见[栅格算子契约](../../workflow/栅格算子契约.md)。主场景、容量重扫及分支专项的预算、身份与实际结果分别认定。

### 镶嵌 whole 的 basic 重扫收敛

本小节属于既有`raster-workflow-capacity`，不扩suite／功能权限／Runtime／容量组／预算。四个Manager镶嵌成果的自动扫描、物理oracle、Monitor先各自通过，再对各成果自身prefix连续两次`basic / force=false`定向扫；28次自动与8次手动分别计数，不能用重扫补链路。

#### whole 重扫身份与断言

仅当轮Hosted独占`addp_online`、非默认Tenant／普通User、成功Manager父执行及真实whole身份全匹配时，Meta夹具在whole内准备两错误组件项＋一子prefix模拟旧碎片，不删记录／改源对象或Grant，由生产扫描收敛。首扫物理删除含软删残留，次扫内部node／item零；两轮只读快照核item_id／fingerprint／node_id／真实名与全路径／扫描深度稳定、范围外身份不变。正式资源树始终同路径单叶，不暴露内件／同路径普通目录；留扫描UUID／终态／范围、DB快照及原资源树回执。

拒个人环境、其他suite、容器重建／重启、身份／目标漂移，无产品旁路。失败仍保阶段证据、恢复策略并清部署；Runner确定性不替实际Hosted终态。

#### whole 元数据授权与受限快照

System标准独立初始化User为五个已扫源TIFF及尚不可见的精确成果签`metadata_read`，消费者仍12项；`at_time`期限≤原10800秒，退出逐一撤销／核重新隐藏。内容仍由ExecutionAuthorization和正式入口裁决，不把metadata Grant当内容许可。初始化Token不入Runtime／业务环境／浏览器／证据，Security启动依赖保留，成功／失败／未知写入均登记待撤销。

layout只能取System可信事实：普通TIFF严格single／multi，成果严格whole，不按数量／扩展名／默认值猜或把multi降single。观察及签发前重核均留字段白名单脱敏事实（含不完整／错配）、实际与允许layout／阶段／稳定码；未知字段／异常串仅摘要。事实身份、类型化目标、边界、basis_state、摘要／到期全部匹配才签，失败先留证，不被收尾覆盖。

大小／范围／格式以绑定本轮部署、User／Tenant／Engine／成功父执行／原自动扫描／精确成果的有界只读DB快照核；公开Meta仅结构／身份。查询区分`execution/produced_item/whole_state/seed_fragments`操作和归属／执行／返回边界／JSON解码／形态／公开身份阶段；保留原超时／大小／只读限制。仅记稳定码、可精确提取的五位SQLSTATE（否则unavailable）、退出码／字节行数／JSON类型及摘要，不存原SQL／参数／stdout／stderr／连接／凭据或异常正文。

Runner核诊断／脱敏，真实SQL沿`make test-common-postgres COMMON_POSTGRES_TEST_ARGS='--test online-capacity-snapshots'`，会话临时表退出清理；本地仅经核验addp_test，CI用既有Common PostgreSQL Service完整门禁，不增Hosted调度／产品入口。

#### whole 专业 DTO 与血缘投影

内部／公开事实分开核，不能要求公开返回内部加工信息。Develop专业读取只取`metadata.lineage_facts`与有界内部事实比，`result`等不送Monitor校验器；真实Monitor仍完整DTO严格核。两者沿现有资源投影，operations空、schema_snapshot私有。

Manager专业响应先过完整DTO白名单，再按已声明kind／operator／input_ports／output_ports逐项按序比内部操作投影，保Go omitempty；未知字段、field_mappings／schema_snapshot外泄或身份差异即失败。操作匹配后仅在资源比对视图置空operations以复用locator／身份校验，不改原响应／事实、不提前丢字段／重新取数掩差或增接口／权限。

`capacity-snapshots.json`留每次投影结果／闭合原因／字段存在性与类型／摘要／有界差异位置；未知字段名及所有值只存摘要，不存原资源标识／路径／行值／SQL／凭据／异常正文。上述失败均先于授权／环境清理留证，首败与独立清理结果并列。

### 部署与主场景

阅读导航：[运行准入](#栅格主场景运行准入) · [计算矩阵](#栅格主场景计算矩阵) · [元数据授权](#栅格主场景元数据授权与事实取证) · [失败补偿](#栅格主场景失败补偿与退出) · [固定结果](#栅格主场景固定结果与验收边界)。下述规则属`raster-workflow`，后续计算分支沿[共同核验](#栅格主场景计算矩阵)，独立范围另行说明；计算分支见[多来源计算与页面构图](#多来源计算与页面构图)。

#### 栅格主场景运行准入

- **部署**：GitHub Hosted `ubuntu-24.04` x86_64独占，复用`scripts/utils/hosted-online.sh`。`make build-images`构建GeoPython产品镜像，默认入口启`addp-raster-online-runtime`，核实际image ID／command／提交并留Runtime日志，不走开发启动。物理夹具经`ADDP_ONLINE_RASTER_RUNTIME_IMAGE`用同一已核镜像，缺参Docker前拒绝，不回退开发镜像。
- **来源／身份**：Business两套无持久卷／不同凭据MinIO，确定性4326双波段／NoData TIFF；System IAM helper建非默认Tenant／普通User，独立Provisioner正式注册两Engine。API／浏览器同User，凭据仅owner-only runner.temp，不进业务环境／证据。登录等待精确URL／实际Tenant或MFA状态，不靠不存在选择框超时推进，不跳身份／权限核验。
- **预算／范围**：preflight＋业务（含矩阵、页面及原收尾）共1740秒，不改其他场景／夜间预算，首次真实通过前仅手工workflow_dispatch。完整full保71项主场景浏览器观察、DEM、普通图片及分支；min/max阶段48轮或旧扩展计数不替完整矩阵。

#### 栅格主场景计算矩阵

| 路径 | 独有断言 |
| --- | --- |
| 基础保存 | `raster_load→raster_band_math→raster_save(profile=cog)`，create／重复create失败／replace；独立完整像元／CRS／transform／NoData／布局。 |
| 重投影／镶嵌 | 确定性源→3857的1米网格，裁两个重叠区，分别first／last核全部像元／NoData／空间及原成果不变。 |
| 多波段／缺值 | 各波段缺值位置不同，合法零／部分alpha，NaN／有限NoData分别核。有限NoData平均插值保各波段声明／缺值，波段计算保存重读声明NaN／独立有效性；尺寸重采样COG经正式ResourceLocator重读`b1+b2`，核全像元／NoData／alpha／联合有效。 |
| 平均插值 | 非均匀源、逐波段缺值／透明干扰、整倍数及非整数缩放；独立源目标矩形面积交集核边权，不能等权平均或提前合源有效性。保存重读相加各用本波段均值。 |
| 双线性 | 逐波段有效中心／邻权，中心无效不被邻域填补；放大COG的overview与Meta轻探测一致，重读相加保各波段有效性。 |
| min/max聚合 | 第二数据波段、3×5、nearest金字塔的单DAG，核NoData排除／内边不足整组／CRS／完整仿射；保两个新成果／两浏览器场景及原成功失败。 |

**所有后续持久化成果共同核验**：

1. Classic TIFF的`profile=cog`、`is_cloud_optimized=true`、`cog_check_level=heuristic`、`is_tiled=true`，`has_overviews`与独立物理结果一致；overview是独立事实，不推导／否定或补造COG profile。Meta有界提示不替GDAL完整布局／像元。
2. `size_bytes == physical stat size == Meta DataItem size > 0`，三者严格正整数；只由Develop自动提交目标Meta scan、owner自动采血缘，逐成果核当前执行派生关系及Monitor API／Console同身份／源目标，不手扫或collect补链路。
3. 每轮重读源及先前／当前全部成果，核像元／mask／布局／大小／源hash与精确对象集合保持。物理oracle只在同一进程复用已算单波段期望元组，推导平均覆盖alpha／联合及双线性联合结果；不缓存真实对象、GDAL读取、hash或校验结论。

只读分支按其无持久输出约束核验，不造大小／发布／扫描／derive；各分支不放宽Runtime、1740秒总预算、单项／单请求／浏览器期限和既有断言。

#### 栅格主场景元数据授权与事实取证

full三源Engine扫描成功后，独立初始化员为原执行User办理固定源／已确认成果限时精确metadata_read；layout来自System事实，绑定Tenant／User／Engine／完整路径／fingerprint／boundary hash，执行者仍12项功能权限。办理凭据限准备控制器，普通枚举前核可见；未知写入也先列待撤销，退出核重新隐藏，业务与撤销错误分列，不以metadata许可替执行授权。

公开Meta仅安全结构；三方大小／完整TIFF事实及普通图片源前后快照，来自当轮Hosted PostgreSQL固定有界只读查询。绑定`addp_online`容器ID／启动时刻、suite／Run ID、非默认Tenant／真实User、源扫描或当前Develop父执行及精确自动扫描，错容器／身份／目标即失败。父失败但已发布切片只认原夹具白名单及实返扫描绑定；失败prefix完整Meta集合另取同受控快照，不让普通列表过滤掩多登记。

Develop公开不含tenant_id，成果候选前由快照核父Tenant／内部发起者，公开UUID／模块／任务类型／状态／User仍匹配，不恢复内部字段或减物理／血缘／页面／保持／清理断言。

#### 栅格主场景失败补偿与退出

- **已确认发布后的清理故障**：同suite的Business回环代理只针对首／末明确对象，真实MinIO PUT成功、回复Runtime前，以长度／SHA唯一定位私有临时COG，在sidecar路径建目录使真实unlink失败，不改COG字节／大小。代理及事件生产／读取使用相同Runner UID／实际Docker socket GID，socket／owner-only事件仅内部持有，共享生命周期独占建删，无产品故障参数。部分确认时下一片`not_attempted`，全确认不返`failed_tile/publication`，父均failed；逐项核确认产物／大小／新自动扫／旧hash／工作目录清理，未真实运行不算该故障T4。发布事实归[正式失败契约](../../addp栅格计算与成果契约规范.md#43-发布失败事实)。
- **硬超时授权补偿**：不能靠Python finally。停System／Owner前，既有生命周期钩子从当轮metadata日志恢复精确撤销，核suite／Run／原容器与启动时刻／User与Tenant／固定Engine与对象／请求UUID／确认Grant，未知写入也待办。先归档原日志，正式System撤销／核不可见，再逐项回写剩余请求并独存补偿结果。失败／未知不算收权通过，不改原Online失败，不把超时后清理算1740秒内业务通过。
- **退出归属**：各退出路径毁本轮业务容器／临时registry／平台容器及卷／Runtime容器及镜像，核零残留。业务产物、授权撤销与外层Infra清理分别认定，不能替代。

#### 栅格主场景固定结果与验收边界

| 固定源码／作业 | 实际结论与关键缺口 |
| --- | --- |
| `0bee3c249cdc08424f6b97b9aab11d3a1e445655`；[42383359430／128435478324](https://github.com/pampa0629/addp/actions/runs/42383359430/job/128435478324) | full在1740113ms以`online_scenario_timeout`失败，七服务同提交／67页面回执，末metadata为dem-slope.cog.tif；普通图片／末尾分支撤权无完整结果，58 Grant撤销pending。部署／Infra清理单独成立，不能报收权完成。血缘223次HTTP共21.81秒／最长419ms，不据总超时推断接口慢。 |
| `a7895d467ca2d69cee45a578d2a4dcbf0c5c8d89`；[42390011971／128455431586](https://github.com/pampa0629/addp/actions/runs/42390011971/job/128455431586) | full通过，preflight＋业务1489195ms、未扩1740秒。七服务同提交；Tenant2执行者12权限、独立普通分支读者3权限。create／冲突／replace、多源／页面构图、DEM、PNG／JPEG及分支撤权完成；71主页面／52组三方严格大小一致，64主＋7分支Grant全撤不可见、pending空；154次有界取证／策略恢复／部署及Infra清理通过。 |
| 同SHA [GeoPython42389976288／128455317630](https://github.com/pampa0629/addp/actions/runs/42389976288/job/128455317630) | 21113过／145警告、登记及编排门禁通过，不外推其他Job／当前源码。 |

成功full的八张原始图／三前后截图证明：撤A前四产出图各2节点／1本执行直接边；撤后A1／A2各1节点／0边、`permission_filtered=true`且不泄隐藏源名／指纹／execution UUID，B1／B2仍2／1、无隐藏提示。C可有既有隐藏提示，但四产出图及C下游均无本次执行关系。该固定范围不代表当前源码重跑，独立专项通过不替full，后续成功不抹首轮超时／收权缺口。

### 普通 PNG／JPEG 页面导出

属本suite完整场景之后的页面链路，同源产品Runtime／原普通User权限／清理，不是whole容量重扫。真实Develop导入双节点工作流、选编码（JPEG质量85）、首次保存／重开／执行，Monitor核成功／目标。物理独立读37×23 RGBA PNG／RGB JPEG：PNG核全像元／透明，JPEG完整解码对平滑合成源核MAE<5、最大<15，仅该样例不作通用质量承诺。核原始／解码hash，无CRS／网格／GCP／RPC／EXIF／旁文件；源／旧成果保持。

沿共同标准核精确输出／自动扫描／三方大小、删保存任务及部署／物理／Infra清理；页面生成不等浏览器下载或GIS定位。产品规则归[普通图片导出](../../builtin-formats/媒体与地图数据集.md#工作流普通图片导出)，确定性入口为`make test-geopython-workflow`、`make test-module MODULE=develop GO_ONLY=1`、`make test-develop-frontend`、`make test-raster-online-runner`；正式范围按固定SHA／原产物，不能由本地结果补验。

### 多来源计算与页面构图

三链路沿[运行准入](#栅格主场景运行准入)、1740秒及[成果共同核验](#栅格主场景计算矩阵)，分别取证，固定通过查[主场景结果](#栅格主场景固定结果与验收边界)。

#### 多源加权与RGB合成

4326源TIFF＋目标Engine已自动扫的3857 COG，显式参考网格／选波段，分别跨文件加权与RGB合成。Develop／Monitor完整绑定双ResourceLocator，同execution两源DataItem→目标derive，Console按完整输入核卡片／链接／逐源证据。独立公式核全波段／NaN与mask／RGB解释／网格与CRS／nearest金字塔，源／旧成果保持及三方大小按共同标准；RGB生成不认证显示拉伸／最佳波段推荐／Manager RGB渲染。

两个execution各两份来源页面证据，共四份；当前full71项由[独立计数断言](../../../../scripts/test/raster-workflow-online_test.py)核，早期29份只属扩展阶段，不替full。

#### 普通用户页面多选构图

保原API矩阵，真实Console／Develop多选三个独立登记TIFF、配置镶嵌／COG、首次保存／重载／运行。仅本suite补`develop.task.create`／`develop.task.delete`，容量不继承，不增update／引擎管理／管理员，不改产品规则。页面建／执行，不能API代提交或注图对象；读真实定义／回执核Runtime／源序／公开参数／目标／父执行，成果按共同标准核并逐源血缘／同User页面。成功及归属明确的失败删除本轮任务核404；未知创建／执行不重发，零残留与真实用户操作分别验，未真通过不登记完成。

#### 分支血缘与源撤权

手工`raster_case=full|branch-lineage`：默认full末尾执行本项，独立branch-lineage先深扫三源、仅本专项；空／未知在生命周期前拒绝。Job／Artifact／`raster-case.json`标范围，不算full。一次execution从A／B各派两成果，C仅load不产出，四成果自动扫／登记。初始化员给同专用普通读者三源＋四成果限时精确metadata_read，不改执行者权限；读者同会话核图／execution UUID再仅撤A：

| 阶段 | 断言 |
| --- | --- |
| 撤前 | 四产出图仅指定A／B及本execution唯一直接边，证明C未参与；C下游可留full前序合法历史／权限隐藏提示，但无本次关系。 |
| 撤A后 | A两成果只当前节点／零边／隐藏提示，不泄隐藏端；B两成果保原边、无隐藏提示。 |
| 收尾 | 逐图回执／截图；成功失败均撤本轮精确授权并核Meta隐藏，首败与清理分列。 |

确定性`make test-raster-workflow-online-runner`及既有登记只证接线／受控断言，真实跨服务／页面Hosted独立认定。

### 重分类与整数分组聚合

同suite继多源执行，预算／保持／大小／自动扫／derive／Monitor及Console均沿[共同核验](#栅格主场景计算矩阵)。

#### 重分类规则与缺值

正式ResourceLocator双波段TIFF选第二波段；`reclassify-nodata`省unmatched核默认NoData，`reclassify-keep`显式保未中原值。核精确值／合法零／左闭右开／未中间隙／无界端点，独立全像元／NaN／mask／nearest overview；keep不能恢复源NoData。

#### 整数分组与精度

`aggregate-mean`／`aggregate-sum`接重分类后选源第二波段，`factors=[3,5]`得86×52单COG、43×26 nearest金字塔。独立核全值／NoData有效计数及右／底／右下不足整组；仅mean按Float64所需局部容差，sum及旧场景精确比。

#### 扩展阶段计数与验收边界

旧重分类、mean／sum累计清单仅证当轮新增及先前成果保持，不作为当前full计数；完整71项及后续聚合范围查[主场景要求与固定结果](#部署与主场景)。

### 外部裁剪、轮廓与局部更新

同suite／1740秒／单项期限，写出沿[共同核验](#栅格主场景计算矩阵)；只读不造对象／大小／发布证明。

#### 区域外裁剪：持久化COG

聚合后`clip-outside`：正式locator双数据波段TIFF，`raster_clip(mode=outside)`带孔Polygon，保256×256网格，直接存双数据波段＋alpha，不以选波段漏alpha。独立行列公式核外围／孔／区内NaN、源NoData不恢复、255/0 alpha／逐波段mask／色解及128×128 nearest金字塔；各波段53247有效像元。

#### 有效像元轮廓：只读GeoJSON

`footprint-any`／`footprint-all`正式加载双数据波段＋alpha TIFF，省validity核默认并，显式all核交；独立整数像元公式核完整面覆盖／边界网格／面积／逐波段缺失，不用Polygonize或生产算法造期望，不固定合法环起点／方向。Develop／Monitor／Console核同User GeoJSON，无持久输出／derive／目标扫描，源／旧COG保持；不认证全部后续轮廓扩展。

#### 局部更新：持久化COG

外裁剪后正式load基底及双数据波段＋alpha更新源，局裁／显式对齐后`raster_update`存双COG／nearest金字塔。独立行列公式核覆盖、区域外／无效更新保基底、合法零／部分alpha／逐波段有效；双输入各有来源页面证据、自动扫描／双derive不可省，后续只读轮廓仍保护全旧成果。

#### 阶段清单与固定结果

旧阶段计数不当当前full结论，完整范围查[固定结果](#栅格主场景固定结果与验收边界)：

| 固定版本／作业 | 已验范围与限制 |
| --- | --- |
| `89e77da9a1bc72ec6c7e1791f2172edbbfdefb28`；[37741228176](https://github.com/pampa0629/addp/actions/runs/37741228176) | 归档独立核并／交覆盖65532/65524像元、36页面／25 COG及零残留；同SHA Platform取消的GeoPython全量Job不计通过。 |
| `53591eba7e749c84849fe2e1ebff4a8ee7a525e9`；[37795049439](https://github.com/pampa0629/addp/actions/runs/37795049439) | 独立审计更新182565字节、双波段有效65535/65536，38页面／26 COG、双来源derive及零残留。 |
| 同SHA [Platform37794921834](https://github.com/pampa0629/addp/actions/runs/37794921834) | GeoPython全量697过，同提交另一取消运行不计通过。 |

标准`make test-raster-online-runner`、`make test-geopython-workflow`、Platform与手工`online-t4-gates.yml`，不增入口／依赖／suite；固定Hosted／CI／取消分列，不代表当前源码重跑。

## raster-contour-persistence

本专项验证 `raster_load → raster_contour → save` 的NFS单文件GeoPackage持久化，以下部署、数学、扫描与历史结果分别认定。

### 等值线持久化部署与NFS

独立手工Hosted Linux x86_64 T4，复用共享生命周期、GeoPython产品镜像默认入口、普通栅格User原十二项权限及正式引擎登记，不扩权、不重复原 `raster-workflow`；原1740秒预算不变。本专项业务预算 **600秒**，部署不计入。

标准分发为 `make test-online ONLINE_SUITE=raster-contour-persistence`，独占部署脚本为 `bash scripts/test/online-hosted-raster-contour-gate.sh`。独占NFSv3 export仅回环访问、启用 `root_squash`；Runtime按已有mounted-path契约同路径bind mount，Meta另经真实NFS读取。

### 等值线数学与保存

独立生成EPSG:3857、64×48、2×3米像元的Float64倾斜平面DEM；指定200／350／500米与固定间隔100米分别存两份 `.gpkg`。沿现有 `save` 的 `mode=replace`，每次目标均是本轮尚不存在的唯一文件，**不构成create冲突或覆盖替换验收**，不扩展通用保存能力。

独立平面公式逐点核严格二维LineString、完整高程集合、Float64 elevation与源投影，验证单图层、精确文件集合及无sidecar／工作目录残留。物理文件＝Meta大小，均为严格正整数；通用矢量 `save` 返回GeoDataFrame，不伪造raster artifact大小。

第二次保存后重读第一份文件及源DEM的SHA-256／大小，源Meta保持不变。

### 等值线自动扫描与血缘

仅源做一次手工深扫；两目标各由Develop自动提交精确file扫描，不能手扫目标或手工collect血缘。普通发起User读取其Meta扫描执行，扫描安全响应的 `tenant_id`／`triggered_by` 必须是准确Tenant／发起User整数，不读取禁止公开的内部主体字段。

GeoPackage按 `single/container/geopackage` 登记；图层只在 `type_info.container.children`，不向外层复制空间能力或另造图层身份。Monitor成功状态／owner／资源血缘一致，Meta自动形成同一Develop execution从源文件到目标文件的derive边。

### 等值线固定结果与验收边界

`54acffb1c` 的[Hosted 39097413643／Job117586617847](https://github.com/pampa0629/addp/actions/runs/39097413643/job/117586617847)已完整通过并独立审计：两种高程模式的两份成果、严格大小、两个自动扫描、当前执行血缘及清理通过，预检＋业务103001毫秒。完整版本及相邻门禁查[栅格当前验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)；固定结果不表示当前源码已重跑。

所有退出路径清本轮NFS export、已确认归属的临时目录、Runtime／registry／产品镜像及Infra，零残留与失败清理分别验收。环境清理不等于Manager业务DELETE；API链不证明Manager绘制、HTTP Range或真实页面操作。保持手工入口，代码落地、Native物理验证和真实T4分别记账。

## raster-polygonize-persistence

独立手工Hosted Linux x86_64 T4。标准分发 `make test-online ONLINE_SUITE=raster-polygonize-persistence`，隔离部署 `bash scripts/test/online-hosted-raster-polygonize-gate.sh`；复用既有生命周期、产品Runtime默认入口、共享NFS归属检查及普通User原十二项权限，无新产品接口／权限。预检＋业务合计 **600秒**；原Raster T4、1740秒预算和等值线两场景不变。

### 分类转面三条DAG

三条链均由正式源加载，经现有 `save(mode=replace)` 存本轮三个尚不存在的唯一GeoPackage，不据此认证覆盖冲突：

- 四邻接转面：`raster_load → raster_polygonize(connectivity=4) → save`。
- 八邻接转面：`raster_load → raster_polygonize(connectivity=8) → save`。
- 重分类后四邻接：`raster_load → raster_reclassify → raster_polygonize(connectivity=4) → save`。

### 分类夹具与mask／alpha

EPSG:3857、8×6旋转仿射整数TIFF，含负数、合法零、孔洞、对角相连区域、NoData及共享mask。有效性先排除NoData、无效mask和全透明像元，部分透明的有效像元保留；三条链都须核对这些覆盖，不能用重分类恢复无效源像元。

### 分类转面独立oracle

标量flood-fill＋逐像元多边形并集核完整连通区域、分类代码、精确像元覆盖、合法二维MultiPolygon、Int64 `class_value`、孔洞与源CRS；期望不调用产品转面／重分类或GDAL Polygonize。真实文件篡改回归须拒绝错误分类、几何、维度、区域丢失及额外sidecar。

### 分类转面扫描、清理与未验范围

仅手扫源，三目标由当前Develop执行自动扫描；核普通User公开扫描归属、GeoPackage单文件容器／内部图层、物理＝Meta严格正整数大小、Monitor与当前执行active derive。保存API校验和Runtime worker归属检查复用各自共享唯一实现；每次保存重核先前成果SHA／大小、源字节及源Meta不变。

Runtime同路径bind写入与Meta真实NFS读取分别记录。所有退出路径清本轮NFS export、已确认归属目录、Runtime／registry／镜像及Infra；不据此认证NFS客户端写、Manager绘制／业务DELETE、HTTP Range、覆盖冲突或容量。

**真实Hosted T4待验**；登记、代码落地与Native结果均不能记为T4通过。

## 独立持久化专项公共前提

本节仅适用于下列五项：[矢量栅格化](#vector-rasterize-persistence)、[整数栅格分区统计](#raster-zonal-statistics-persistence)、[邻域统计](#raster-focal-statistics-persistence)、[矢量分区统计](#vector-zonal-statistics-persistence)、[邻近距离](#raster-proximity-persistence)。其他套件仍按各自场景声明执行。

五项均使用独占 Hosted 生命周期、普通 User 原 12 项权限、同源七个 Backend 与默认入口 GeoPython 产品 Runtime，以及共享 NFS；不接管个人服务。Runtime 同路径 bind 写入与 Meta 真实 NFS 读取分别记录。各专项独立手工触发，预检与业务执行共用各自的 600 秒预算，不改变已有套件、产品权限或跨模块产品逻辑。

每轮核对精确输出与扫描清单、源及先前成果不变和 Runtime 执行工作目录清理；失败不得发布目标或新增扫描。成功和失败退出均须核对归属 NFS export／目录、部署及 Infra 零残留。各专项另列的产品 Runtime／镜像／registry 清理项仍须逐项核对。

入口登记、确定性门禁与真实 Hosted 分别记账。固定版本结果和未覆盖范围见[验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)，不外推为当前 HEAD 已重跑。

### 独立持久化专项验证入口

以下分层仍仅适用于上述五项。脚本 Runner 验证夹具、场景断言、隔离生命周期与 CI 登记；五个专项 Runner 均由 `make test-raster-online-runner` 聚合。真实 GDAL 物理校验与篡改拒绝统一沿 `make test-geopython-workflow`，各项范围如下：

| 专项 | 脚本 Runner | Native 物理验证范围 |
| --- | --- | --- |
| [矢量栅格化](#vector-rasterize-persistence) | `make test-vector-rasterize-online-runner` | 几何覆盖、完整 COG 与篡改拒绝 |
| [整数栅格分区统计](#raster-zonal-statistics-persistence) | `make test-raster-zonal-statistics-online-runner` | 真实 GDAL／CSV 与篡改拒绝 |
| [邻域统计](#raster-focal-statistics-persistence) | `make test-raster-focal-statistics-online-runner` | 真实 GDAL／COG 与篡改拒绝 |
| [矢量分区统计](#vector-zonal-statistics-persistence) | `make test-vector-zonal-statistics-online-runner` | 真实 GDAL／GeoPackage 与篡改拒绝 |
| [邻近距离](#raster-proximity-persistence) | `make test-raster-proximity-online-runner` | 真实 GDAL／COG，以及错误像元／mask／单位／网格等篡改拒绝 |

真实 Hosted 使用各场景列出的 `make test-online ONLINE_SUITE=...` 标准入口，workflow 仅编排同一入口。Runner、Native 与真实 Hosted 结果分别记账；前两者通过不代替 T4，实际结论仍绑定固定源码、运行与未覆盖范围。

## vector-rasterize-persistence

独立手工Hosted；部署／身份／预算／清理遵循[公共前提](#独立持久化专项公共前提)，Runner与Native分层沿[验证入口](#独立持久化专项验证入口)。标准业务入口为 `make test-online ONLINE_SUITE=vector-rasterize-persistence`。

### 矢量栅格化三条成功DAG

已登记GeoJSON二维面与同CRS参考TIFF分别作为矢量／网格来源，三条链各保存COG：

- 属性值栅格化，按像元中心判定覆盖。
- 常量栅格化，覆盖所有触及像元（all-touched）。
- 参考分类栅格转面，再栅格化回写。

保存沿 `raster_save` 的 `write_mode=create`／`profile=cog`。仅手扫两份精确源，成果各由当前父执行自动扫描；核同User／Tenant、Monitor及所有实际输入的active derive。前两链为两输入，转面回写链仅用参考栅格。

### 非法字段失败

非法字段须使执行失败、无发布／新增扫描；精确输出清单、扫描清单及Runtime工作目录不变，既有成果不受影响。

### 独立像元核验

独立几何oracle逐像元核值／mask、NaN NoData、Float64、旋转网格及完整COG布局；artifact＝物理文件＝Meta大小，均为严格正整数。

### 固定Hosted结果与边界

`764bf53a9` 的[独立Hosted 39207785953／Job117914293518](https://github.com/pampa0629/addp/actions/runs/39207785953/job/117914293518)已通过并复核归档：三份COG严格大小824／819／847字节，预检＋业务176408毫秒，清理及Infra零残留通过。

该固定结果不计入Manager绘制、NFS客户端写、HTTP Range、业务DELETE、覆盖冲突或容量，不表示当前源码已重跑。

## raster-zonal-statistics-persistence

部署／身份／预算／清理沿[公共前提](#独立持久化专项公共前提)，Runner与Native分层沿[验证入口](#独立持久化专项验证入口)。标准业务入口为 `make test-online ONLINE_SUITE=raster-zonal-statistics-persistence`。

### 整数分区统计四条成功DAG

已登记且同CRS／旋转网格的数值TIFF＋分区TIFF，分别执行：

- 显式多波段分类分区统计。
- 重分类后接分区统计。
- 超过50类的全量统计保存及有限预览。
- 全无效分区保存空表，保留固定Schema。

四份结果均经既有 `save` 持久化CSV；响应最多50行、仅作摘要，不新增RasterArtifact。物理文件＝Meta大小且为严格正整数；无数据行不等于无文件或执行失败。

### 非法波段与不同网格失败

两条失败分别验证非法波段、数值与分区网格不同；不得发布目标或新增扫描，不改变精确输出／扫描清单和既有成果。

### 独立CSV核验与扫描血缘

独立标量分组＋标准库统计oracle逐行核分类值、三项计数、五项统计、null、排序和固定Schema；完整CSV与有限预览分别验证，不以预览样本代替全表。

仅手扫两份精确源，四份CSV各自自动扫描；逐项核同User／Tenant、Monitor及当前执行所有实际输入的active derive。每轮核源物理字节／Meta和先前成果不变。

### 固定Hosted结果与范围

`5e49199e8` 的[独立Hosted 39217208101／Job117943758382](https://github.com/pampa0629/addp/actions/runs/39217208101/job/117943758382)已通过并独立复核归档：四CSV为4／3／70／0行，严格物理／Meta大小253／206／1900／73字节，预检＋业务210767毫秒，清理及Infra零残留通过。

Native与真实跨模块结果分别记账；不计Manager绘制、NFS客户端写、HTTP Range、业务DELETE、覆盖冲突或容量，原Raster／Contour／Polygonize／Rasterize T4不变，固定结果不表示当前源码已重跑。

## 邻域与 IDW 专项共同要求

仅下文明示引用的专项适用；原[五项公共前提](#独立持久化专项公共前提)和[水文／分区要求](#水文与分区统计专项共同要求)不扩范围。语义以[邻域契约](../../workflow/栅格算子契约.md#栅格计算契约)、[IDW契约](../../workflow/栅格算子契约.md#点数据idw插值)为准，矩阵、容差和归档清单各自保留。IDW始终核点源＋模板两输入；模板只定义网格／CRS。

- **准备与预算**：独立手工Hosted／NFS、同源默认入口Runtime、普通User原12权限，各600秒。身份／源初始化、首次预期、全部执行查询／物理全检、原件归档及业务缓存清理按原边界计时，部署／外层退出另核。4 CPU／2 GiB／无额外swap仅用于原已声明的专项。不接管个人部署，不改产品、权限、安全投影及原套件，不重试、扩预算或加夜间调度；准入与报告沿[T4规范](../T4-Online验收规范.md)。
- **完整核验**：全部实际像元／mask、Float64／NaN、原六项网格／CRS或缺定位、COG布局及实际nearest概览；小图不虚构概览。artifact＝物理＝Meta正整数大小，自动扫描绑定当前父execution／User／Tenant／Engine，实际输入血缘和Monitor单独核对。内部observer保持原有界SQL、独立凭据与当前身份／DAG／Runtime绑定，公开安全投影不扩大。
- **复核与退出**：源及先前成果原字节／Meta不变，按各节次数真实重读；不得缓存实际成果。只读预期仅按原许可复用，绑定同轮root／run、源hash和夹具源码；原件／清单不覆盖、修补或静默重建，独立oracle不调用产品算法，不放宽精确比较或容差。错误须绑定实际focal／interpolate及原原因／提交语言，前置失败不充数、反向查询不改错。失败零发布／新增扫描／派生血缘；正常、失败、部分启动均清理本轮工作区／缓存、NFS、Runtime／镜像／registry、部署／Infra等已声明资源，首败与清理错误分别保留，清理失败整套失败。
- **分层验证**：Runner沿 `make test-raster-online-runner` 聚合，GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`，输入与workflow接线遵守[CI规范](../CI与交付规范.md)。本地／登记不代替Linux全量或真实Hosted、UI、容量；原始类型、非标准JSON及各节数值极限／混合类型／故障注入继续单列Native，不扩格式支持。

### 邻域参数与专项范围

邻域统一使用非空严格递增的 `percentiles` 列表，单项也传 `[p]`；旧单值 `percentile` 已删除，旧DAG显式修改。原单项、圆窗及variety对照只同步列表与准确诊断，原成功／失败矩阵、全检次数、原件与600秒预算保持，历史固定证据不追认为新版本。多项要求直达[邻域多百分位](#邻域多百分位独立保存验收)，登记不代表已完成真实Hosted。

像元方窗为3／5／7，米制圆窗仍限定最多5×5候选；两者的权重尺寸分别按现行契约核验，不互相扩大。十二种方法、跨双轴块、全1对照及P50／中位数规则按各节独立矩阵验证；[7×7专项](#77-像元方窗独立-hosted-验收)已登记，实际运行另行认定。

### 三种邻域加权方法的共同复核

仅下列明确引用的weighted_sum／weighted_stddev／weighted_mean套件共用本段。各为16条COG成功链、56次准确失败：成功累计136次、失败累计896次，合计1032次完整成果检查；加首次空基线共73次物理调用。逐次核完整像元／mask、缺定位和源编码清除、COG及实际概览，按上方要求核大小、扫描／内外血缘／Monitor、源旧Meta与全部清理。归档16份COG原字节、十份源原件、16份冻结独立预期及大小／SHA256清单，不覆盖／修补旧归档。三种方法各自的求和、均值或总体标准差oracle、全1解析见证与准确拒绝条件仍分别验证。

#### 三种加权方法的矩阵与拒绝条件

以下三suite各16成功COG（14加权＋2原方法对照）／28类各中英共56准确失败，沿[邻域共同要求](#邻域与-idw-专项共同要求)的12权限／各600秒及上方1032全检／73物理轮／十源与16成果16期望归档，不互借验收结果。

成功共同覆盖非对称3／5跨块两链、全1权重3／5＋实际原方法3／5四链、非均匀同比例权重3／5两链、中心权重0／有效支持空、全无效、重分类、旋转、无定位单行、Int64／UInt64原生NoData邻值、第二波段raw scale-offset／mask／alpha；均值另保部分透明见证。非对称／同比例／重分类用519×515跨块，其余核实际概览；矩阵不翻转，中心无效不填洞，正权重有效支持空为NaN，先判原生NoData再按Float64边界，不宣称任意64位精确。方法独有见证如下。

共同28类：18权重缺失／null／数值标量／布尔／字符串／对象／空列表／一维／行数错／列数错／不规则行／全零／负元素／布尔元素／字符串元素／null元素／对象元素／列表元素；原对应sum／stddev／mean带weights、variety带null weights、加权方法带percentiles三类；未知方法／非法窗口／布尔窗口／波段0／越界／选择alpha／复数数据七类。须同execution命中真实focal及准确原因，前置失败不充数，反向语言不改冻结错误；失败零发布／scan／derive，保持及清理沿共同要求。非标准JSON（含NaN／Infinity／超标准数值）、混合复数alpha与各法数值极限仅Native，登记／本地不代Hosted。

### 邻域加权求和独立 Hosted 验收

`raster-focal-weighted-sum-persistence`；`make test-raster-focal-weighted-sum-online-runner`，沿上述矩阵及[完整复核](#三种邻域加权方法的共同复核)。weighted_sum全1与原sum的实际全像元／mask及解析0／4、−4／4、2／2对照须得4／0／4；非均匀权重放大8倍的实际总量严格同比例。独立逐位置2000位Decimal乘积累计、最终Float64舍入，不用生产整数编码／归约，不除权重和／抽样／容差；常量按实际有效正权重质量，单样本保加权值，合法零／负结果保留。真正溢出像元转NaN只沿Native证据；旧加权均值Runner生命周期超时仍独立，不由本套覆盖。

### 邻域加权总体标准差独立 Hosted 验收

`raster-focal-weighted-stddev-persistence`；`make test-raster-focal-weighted-stddev-online-runner`，沿上述矩阵及[完整复核](#三种邻域加权方法的共同复核)。weighted_stddev全1与原stddev实际全像元／mask一致，解析0／4、−4／4、2／2分别2／4／0；非均匀权重用逐位置3000位Decimal偏差公式算总体方差再开方，不用生产一二阶矩／精确平方根，不放容差／抽样。核同比例权重不变、输出非负、常量／单样本0；不承诺任意64位整数差值。TRI仅保原有效证据，不纳本suite Hosted重跑。

### 邻域加权均值独立 Hosted 验收

`raster-focal-weighted-mean-persistence`；`make test-raster-focal-weighted-mean-online-runner`，沿上述矩阵及[完整复核](#三种邻域加权方法的共同复核)。weighted_mean全1与实际mean、同比例放大权重均核完整像元／mask相同，合法零／负值保持。独立逐位置2000位Decimal构造、最终Float64舍入并精确比，不调生产／容差／抽样。原九方法及数值极限另列Native，不以平台前置拒绝代目标诊断。

## raster-focal-statistics-persistence

独立手工专项；标准业务入口 `make test-online ONLINE_SUITE=raster-focal-statistics-persistence`，Runner为 `make test-raster-focal-statistics-online-runner`。准备／成果核验／清理按[邻域共同要求](#邻域与-idw-专项共同要求)；[原五项公共前提](#独立持久化专项公共前提)仍限原范围，Runner／Native分层见[验证入口](#独立持久化专项验证入口)。不登记夜间计划，不改已有栅格T4、权限或跨模块逻辑。

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

## vector-zonal-statistics-persistence

部署、身份、预算及通用清理按[公共前提](#独立持久化专项公共前提)，Runner 与 Native 分层按[验证入口](#独立持久化专项验证入口)。标准入口为 `make test-online ONLINE_SUITE=vector-zonal-statistics-persistence`。

### 三条 GeoPackage DAG

三条链均沿通用 `save` 保存 GeoPackage：

1. 已登记 GeoJSON 面与旋转多波段 TIFF，按中心覆盖统计。
2. 同两源按全部触及统计，使用自定义统计列前缀。
3. TIFF 重分类→分类转面→矢量分区统计，仅记录实际 TIFF 输入；前两条均记录两源。

样本覆盖跨块、重叠、孔洞、多部件、合法零、负值、NoData／mask／透明／非有限值、部分透明、网格外、全无效数值及薄面。保留原始属性与行序，重叠面独立统计；空覆盖和无有效值保留行、统计为 null。

### 独立物理核验

oracle 以像元平面几何运算、标量四邻接遍历与标准库统计核对完整几何、属性、计数／五项统计／null、CRS 和字段类型，不调用产品统计／GDAL Rasterize／Polygonize。通用矢量保存不新增 RasterArtifact；物理／Meta 大小必须为相等正整数。

### 精确扫描与执行血缘

只手工扫描两份精确源，三份成果各自精确自动扫描，核对同租户／同用户、Monitor 和当前执行全部输入的 active derive 血缘。

### 失败、清理与验收边界

非法波段／列名冲突不得发布文件或新增扫描；每轮源物理／Meta 与先前成果不变、退出清理沿公共前提，另须核对 Runtime／镜像／registry。

不认证 Manager 绘制、NFS 客户端写、业务 DELETE、覆盖冲突或容量。固定版本 Hosted 结果与未覆盖范围见[验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)；仅手工触发，入口登记不代表 T4 通过，不外推为当前 HEAD 已重跑。

## raster-proximity-persistence

部署、身份、预算及通用清理按[公共前提](#独立持久化专项公共前提)，Runner 与 Native 分层按[验证入口](#独立持久化专项验证入口)。标准入口为 `make test-online ONLINE_SUITE=raster-proximity-persistence`。

### 单位与网格

已登记投影米制 Int32 多波段及美制测量英尺 UInt32 TIFF，分别执行 `raster_load → raster_proximity → raster_save`。两源均为北向无旋转方形网格；输出保留源网格及 CRS，距离统一为米制 Float64／NaN，沿 COG 保存。美制测量英尺按 `1200/3937` 换算为米。

完整像元核验覆盖显式波段、合法零／负分类／UInt32 最大分类值、跨块、无效目标、部分透明和无效区域可跨越。

### 独立距离核验与扫描血缘

独立 oracle 对每个有效目标中心穷举欧氏距离，不调用产品变换、下包络或 GDAL 距离算法；核对完整像元／mask、单位、网格／CRS、COG／金字塔及 artifact／物理／Meta 严格相等的正整数大小。

只手工扫描两份精确源，两个当前执行各自自动扫描成果；核对同用户／租户、Monitor 及各 DAG 仅自身实际源输入的 active derive 血缘。

### 失败、清理与验收边界

非法波段及无有效目标失败不得发布或新增扫描；每次源物理／Meta 与先前成果不变、退出清理沿公共前提，另须核验产品 Runtime／registry／镜像。

本专项不认证测地距离、成本／路径、旋转／非方形网格、容量或 Manager 绘制。固定版本 Hosted 结果与未覆盖范围见[验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)；仅手工触发，入口登记不代表 T4 通过，不外推为当前 HEAD 已重跑。

## raster-thin-persistence

独立手工 Hosted Linux x86_64，入口 `make test-online ONLINE_SUITE=raster-thin-persistence`，生命周期由 `scripts/test/online-hosted-raster-thin-gate.sh` 管理。复用同源默认入口产品 Runtime、隔离 NFS 和普通 User 原12权限，独立预检与业务预算600秒；不改变产品策略、权限、原套件或算法，仅验 API 持久化。

旋转519×515六波段 TIFF，显式选第二波段的零分类直接细化、第一波段重分类为安全整数上界后细化，以及第六波段全无效输入，三条 COG 保存链。样本覆盖跨510／512块的粗线、T／Y／交叉、对角线、环与孔洞、孤点、图边、合法零／负背景／精确大分类、NoData／mask／部分透明和非有限覆盖；源 scale/offset 不得隐式应用。独立全幅标量 Guo–Hall oracle 固定两阶段顺序直到收敛，不调用产品邻域、块遍历或细化实现。逐成果严格核完整0／1／NaN像元及mask、Float64／NaN、完整网格和CRS、无色表及scale/offset、COG完整布局和overviews、artifact／物理／Meta相等正整数大小；核对精确父执行／用户／租户／引擎扫描、Monitor和当前执行所有源血缘、源和先前成果不变。

非法波段、有效小数分类、越界分类和非法前景参数四条失败链，必须匹配当前 execution 的 `thin` 任务与准确参数错误；失败不得发布文件、输出血缘或新增扫描／派生边。成功、失败、超时及部分启动均执行所属工作目录、NFS、Runtime、应用及Infra清理，清理失败即验收失败。仅认证合成样本API闭环，不含页面、容量、任意影像或完整拓扑数学证明。

真实GDAL／COG及篡改拒绝沿 `make test-geopython-workflow`，编排、严格校验、生命周期和登记沿 `make test-raster-thin-online-runner`，并纳入原 `make test-raster-online-runner`。仅手工登记，不新增定时。隔离候选标准Native专项88 passed／1869 deselected／5.64秒，本套件脚本子门禁56项及完整Online登记通过；较大脚本聚合在范围外矢量分区统计模拟生命周期单测60秒超时停止，仍不计整套通过。

固定 `7d376d998` 的 [Hosted 39698265976](https://github.com/pampa0629/addp/actions/runs/39698265976/job/119560904827) 已通过上述三条真实COG保存链、四条精确参数失败及全部隔离清理。七个Backend和默认入口Runtime同源，User33／Tenant2原12权限；原生GDAL3.10.3／Python3.11的现场全像元／mask与完整COG校验已执行，三方大小严格相等，分别9574／9574／10787字节，有效像元267227／267227／0、骨架像元1529／1529／0，预检与业务150285毫秒。源及先前成果不变、精确扫描父身份、当前执行血缘及Monitor、NFS／Runtime／部署与Infra零残留通过。固定源码、物理回执与原始日志已独立复核；归档未保留原TIFF，未作离线成果重读。不计UI、容量、业务DELETE、任意影像或全栅格认证，同版本Linux全量仍等CI会话回传。最终报告哈希及边界见[栅格专题验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)。

## raster-boundary-clean-persistence

该固定套件覆盖默认一轮。显式两轮单独登记为下述套件。

独立手工 Hosted Linux x86_64，入口 `make test-online ONLINE_SUITE=raster-boundary-clean-persistence`，生命周期由 `scripts/test/online-hosted-raster-boundary-clean-gate.sh` 管理。复用同源默认入口产品 Runtime、隔离 NFS 和普通 User 原 12 权限，独立预检与业务预算 600 秒；原各套验收及权限保持。仅验 API 持久化，不计页面、容量或任意影像效果认证。

同一 519×515、EPSG:4326 旋转六波段 TIFF 显式选择分类波段，执行直接清理、重分类接清理和全无效输入三条 COG 保存链。覆盖合法零、负分类、±(2^53−1) 及相邻大分类、全图类别计数并列、3×3 邻域跨块与边缘，以及 NoData／mask／透明和非有限值；全无效成果必须成功保存完整 NaN／无效 mask。独立全幅 Counter 与不可变两阶段逐像元 oracle 核对完整值及 mask，不调用产品排序、分块或中间文件实现，精确比较不采用近似容差。

逐成果核对原尺寸、CRS 与完整仿射网格、Float64／NaN、完整 COG／金字塔、未继承源 scale／offset／色表，以及 artifact／物理／Meta 相等正整数大小。仅源由普通用户手工精确扫描一次，三个目标必须来自各自父执行的自动扫描；严格核扫描执行、父执行、用户、租户和引擎，Monitor 与当前执行源输入派生血缘一致。非法波段、小数类别和越界类别必须失败，输出、扫描及 active derive 图均不得增加；每步重核源物理／Meta 与先前成果不变。所有退出路径检查工作目录、NFS export／私有目录、Runtime／镜像／registry、部署及 Infra 清理，清理失败使门禁失败。

真实 GDAL／COG 及篡改拒绝沿 `make test-geopython-workflow`；编排、严格校验、隔离生命周期与登记沿 `make test-raster-boundary-clean-online-runner`，并纳入原 `make test-raster-online-runner`。只登记手工 Hosted，不新增定时。固定 `1b5a26bc1` 的 [Hosted 39625363775](https://github.com/pampa0629/addp/actions/runs/39625363775/job/119309061524) 已通过上述 API 场景，预检与业务 194662 毫秒，三份大小 31270／13788／10787 字节，有效像元数 267227／267227／0；现场物理校验回执、失败 Runtime 诊断与完整日志已独立复核。归档未保留原 TIFF，未作离线重读；无页面截图，不计 UI／容量或同版本 Linux 全量通过。最终报告哈希与边界统一维护于[栅格专题验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)。

## raster-boundary-clean-two-rounds-persistence

独立手工 Hosted Linux x86_64，复用同源默认入口 Runtime、隔离 NFS、非默认租户普通 User 原 12 权限及既有退出清理。固定 600 秒业务预算覆盖身份与源初始化、冻结独立预期、全部执行和物理复核、归档与业务内清理。Linux 全量和实际 Hosted 由 CI 会话协调，不重试、不扩预算。

12 条 COG 成功链。三组见证输入分别保存默认一轮、显式一轮和显式两轮，核真实第二轮变化、原始类别优先级冻结、旋转网格上跨双轴块的第二轮变化。三对默认／显式一轮实际成果必须像元及掩膜完全相等。另含重分类接两轮、全无效及无定位薄栅格。按原输入全图独立计数冻结类别优先级，逐像元独立生成每轮完整扩张和收缩，精确校验 NoData、mask、透明和部分透明 alpha、原始第二波段、合法零及负分类。16 类各中英文提交共 32 次准确失败，包括轮数 10、波段 4、分类 2，严格绑定真实清理节点与冻结提交语言。整数值浮点、宽整数 NoData 邻接、混合复数 alpha、数值极限及阶段故障注入单列 Native。

成功后复查全部已有成果，失败后逐次复查全部 12 份，共 462 次成果全检、含首次基线 45 轮物理核验。核完整像元／实际掩膜、六项仿射及 CRS 或明确缺定位、Float64／NaN、COG 布局及全部实际最近邻概览、源编码清除。严格核声明／物理／Meta 正整数大小一致、精确父执行扫描、当前实际输入内外血缘及 Monitor、源和旧成果原字节／Meta 不变、失败零发布／新增扫描及派生血缘和全部退出清理。归档 12 份 COG 原字节、源文件、冻结预期及大小／SHA256 清单。

## 分类蚕食容量（raster-nibble-capacity）

独立手工 Hosted Linux x86_64，入口 `make test-online ONLINE_SUITE=raster-nibble-capacity`，生命周期由 `scripts/test/online-hosted-raster-nibble-capacity-gate.sh` 统一管理。复用同源默认入口产品 Runtime、NFS 与普通 User 原 12 权限；管理员身份只配置本轮资源策略，计算、扫描和血缘均由普通 User 发起与读取。原 Raster T4、众数／蚕食持久化及旧完整场景容量套件保持原范围与预算。

确定性 Int64 分类样本为供体网格，尺寸为 4096²／10980²，供体间距为 128／2 像元，包含合法零、负分类、安全整数边界、NoData 和显式 mask。每种样本按运行上限 1／2 分别提交两个蚕食→COG DAG，共 8 组／16 链；每组一次观测，不推导吞吐稳定性或真实遥感专题效果。独立业务预算 10800 秒；Runtime 固定 4 CPU／2 GiB／无额外 swap，缓存固定 64 MiB，不修改产品默认资源建议。隔离身份夹具使用同名 suite，复用原普通 Raster 用户的 12 项权限和已有容量夹具的 4 小时 Access Token／Family 配置，覆盖业务预算及清理；普通 Raster 仍为原会话期限，产品认证配置不变。策略恢复失败时同时保留首个业务失败和恢复错误，不能用清理错误覆盖首个失败，也不能将恢复失败记为通过。

每组重建同镜像的本轮 Runtime 后测量 cgroup v2 的内存、OOM、CPU 与进程 VmHWM 之和，验证实际串行排队／双任务并发及额度排空。将 TMPDIR 绑定到本轮 NFS 根内的私有目录，使用受限自有路径的宿主机只读采样记录私有文件逻辑／分配大小，避免采样进程计入 Runtime 内存。采样间隔包含 HTTP 观察和采样开销，不宣称捕获连续精确峰值。每像元 32 字节是私有距离／供体中间文件的逻辑上界，源、工作 TIFF、COG 和金字塔另计；运行前按并发与网格保守预留磁盘，空间不足应失败。

使用独立解析最近供体 oracle 分块核对全部像元、mask、CRS／网格和完整 COG 布局；逐项核对 artifact／物理／Meta 相等正整数大小、精确自动扫描、同用户／租户、当前执行血缘、Monitor、源及先前成果不变。保留逐组资源原始观测及进度，即使 OOM 或失败也不丢弃诊断；工作目录、NFS／部署和 Infra 清理仍为通过前提。

### 分类蚕食容量的验证入口

脚本回归使用 `make test-raster-nibble-capacity-online-runner`，由 `make test-raster-online-runner` 聚合；容量脚本入口同时运行隔离身份夹具 Go T1。真实 GDAL 与容量 oracle 由 `make test-geopython-workflow` 覆盖，CI 注册同次核对。不增加夜间调度。

### 分类蚕食容量的固定版本证据

固定源码 `d12960c06e4e41a21c821780ec99f8fd7677c98a` 的[独立 Hosted 39408596156](https://github.com/pampa0629/addp/actions/runs/39408596156/job/118580519411) 已通过并经完整原始归档严格审计：8 组／16 链、1098700928 个像元及对应 mask、COG、三方严格大小、16 个精确扫描、同用户及当前执行血缘、源与先前成果不变、策略恢复和全部零残留通过；预检与业务 3120412 毫秒，小于 10800 秒预算。报告 SHA-256 为 `5a628bc5f9b30275aa26ca36c3a813ec49b22a7889b85096e9762f0584698959`。同源[Linux Native](https://github.com/pampa0629/addp/actions/runs/39408524778/job/118580271250) 的标准 `make test-geopython-workflow` 全量 1666 项通过、21 warnings、757.20 秒，无失败或跳过；同源 Platform 在范围外 Infra 生命周期门禁失败，不计整个平台通过。资源逐组指标及其适用边界见[栅格专题](../../../next/栅格算子体系后续专题.md#分类蚕食容量验收)，不把此次确定性分类样本容量计作 UI、任意影像或稳定吞吐认证。


## raster-disk-pressure

独立手工 Hosted Linux x86_64，标准业务入口为 `make test-online ONLINE_SUITE=raster-disk-pressure`，生命周期脚本为 `scripts/test/online-hosted-raster-disk-pressure-gate.sh`。复用既有产品镜像构建与默认入口、七个 Backend、NFS 和普通 User 原 12 权限；沿现有 `raster-workflow` 身份夹具，不修改 System、产品认证／权限、计算算法、磁盘阈值或旧套件。业务预算独立 600 秒，Runtime 4 CPU／2 GiB／无额外 swap；不作为吞吐或大影像容量认证。

仅在本轮独占 Hosted Runner 上，以实际预分配的 4 GiB 文件建立归属可核对的 loop／ext4 文件系统，绑定到 Runtime 的 `/raster-pressure-tmp`，源、成果与证据位于其外。占位文件调节真实剩余空间；源为 1024² 双波段整数分类影像，显式选择第二波段，包含合法零、NoData 和 mask。1024² 影像显式使用合法 4096 分块，原始网格只需一个块且无 overview，独立物理校验完整 COG 布局、块尺寸及无 overview 事实，不把“必须有金字塔”强加给该样本。

蚕食与邻近距离各运行“正常基线→距离计算前拒绝→编码前拒绝→释放占位空间后重提相同 DAG／目标恢复成功”，共 8 条平台执行。距离阶段保留约 2 GiB + 40 MiB，工作影像预算通过后距离预算不足；编码阶段保留约 2 GiB + 128 MiB，计算预算可通过而 COG 的保守瓦片预算不足。采用静态空间窗口，不向产品注入 hook，不抢占计算中途时序。严格核实际失败阶段、required／available／pending／safety 字节数、任务失败及无半成品、无新增扫描／派生血缘；切换查询语言不得改写已记录失败。源与全部先前成果不可变，同一 Runtime 容器和 master／worker 身份保持，释放空间后重提任务成功，无需重启。

蚕食提交中文、邻近距离提交英文，完整中英文链路是验收要求。当前只读已确认 Common `HTTPExecuteWorkflow` 未转发 `Accept-Language`；Runtime 自身的中英文诊断已有 Native 证据，不能替代英文平台 Hosted。该产品语言传递问题不在本轮测试夹具修复范围，未修复前保留验收缺口，不按中文默认值弱化英文断言或记录整套通过。

四条成功链严格核完整像元／mask、Float64 NaN、单位、原 CRS／网格、COG、artifact／物理／Meta 相等正整数大小、精确父执行／普通用户／租户／引擎自动扫描、当前执行血缘和 Monitor。四条拒绝后均核无新增成果、扫描和派生血缘；独立 oracle、源物理／Meta 与先前成果不变贯穿全部执行。退出路径先停止本轮 Runtime，再核精确挂载源与 loop backing file，卸载、分离设备、删除镜像／占位文件和本轮归属目录，随后核 NFS、产品部署及 Infra 零残留；部分启动失败也必须清理，不允许以跳过或错误阶段充数。

标准确定性入口为 `make test-raster-disk-pressure-online-runner`，由原 `make test-raster-online-runner` 聚合，覆盖隔离挂载归属、失败清理、8 条业务链和 CI 登记；GDAL 物理 oracle 与拒绝／恢复回归走 `make test-geopython-workflow`。本场景不新增并发抢盘或容量不可读取的 Hosted 故障注入，相关 Native 证据独立保留。独立入口与夹具已实现，本地 Native 全量、受影响场景复验及标准脚本聚合门禁通过；目前尚未执行真实 Hosted，仍需先补齐提交语言传递，不计 T4 通过。


## 水文与分区统计专项共同要求

本节只适用于明确引用它的水文与分区统计专项，不扩大[原五项公共前提](#独立持久化专项公共前提)或其他 suite 的适用范围。各节保留自己的输入、链数、错误节点、独立 oracle、字段和归档矩阵。

- **准备与预算**：独立手工 Hosted Linux、既有隔离生命周期、NFS、同源默认入口产品 Runtime、普通 User 原 12 项权限及各自 600 秒业务预算。不接管个人部署，不改产品、权限、安全投影或原套件，不重试、扩预算或加入夜间调度。分区统计明确计入预算的初始化扫描、首次期望生成、全部执行／查询／物理复核、归档及业务工作区／期望缓存清理仍须全部计时；部署与外层退出按原生命周期另行核验。
- **成果与身份**：逐项核完整物理成果、原网格／CRS、mask、类型和单位。COG 核完整布局及实际金字塔，artifact／物理／Meta 大小为相等正整数；普通 CSV／GeoPackage 核物理＝Meta 正整数大小，不虚构 RasterArtifact、COG 或第三方声明大小。自动扫描精确绑定父 execution／User／Tenant／Engine，血缘只认当前 execution 实际加载的注册输入，Monitor 单独核验；各节的源数、端口与受控内部 observer 范围不变，公开安全投影不因此扩大。
- **独立复核**：按各节独立算法验证完整像元或表格／几何；每轮重新读取实际成果与源哈希，只允许复用绑定同轮输入的不可变数学期望。原始源、全部先前成果及各节要求的 Meta 快照不变。分区统计保留各自的全检次数、完整原件和清单，缺失、篡改、额外文件或符号链接均拒绝，不覆盖、重建或静默修复。
- **失败与清理**：准确绑定当前执行、失败节点及各节要求的原因／语言；权限、保护投影、加载或其他节点失败不能冒充目标拒绝。反向语言查询不得改写原提交错误。失败零发布、零新增扫描与派生血缘；正常、失败及部分启动退出都核验本轮 Runtime 工作区、缓存、NFS、部署／Infra，以及各节声明的镜像／registry 等归属资源。保留首个业务失败和清理错误，清理失败整套失败。
- **验证与证据**：各专项 Runner 沿既有 `make test-raster-online-runner` 聚合，真实 GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`；新增输入须同次命中 Native、Online、影响选择与 CI。专项筛除、共享重复计数和隔离身份 Go T1 不算全量或完整 IAM／数据库集成。现场完整物理回执与归档后离线复算分开；无原 TIFF 的记录不能声称离线重读。已验与待验按固定源码分别认定，不外推 UI、容量、业务 DELETE、其他 Owner 或生产。 水文已有结果与缺口见[固定结果与待验边界](#水文专项的固定结果与待验边界)。

## raster-fill-depressions-persistence

`make test-online ONLINE_SUITE=raster-fill-depressions-persistence`；脚本门禁 `make test-raster-fill-depressions-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

北向非方形网格的519×515多波段 DEM，显式选择第二波段；覆盖嵌套洼地、多／对角出口、图像与内部无效边界、合法零和负高程、跨512块、NoData／mask／alpha／非有限。三条保存链为米制填洼、英尺换算米制后填洼、全无效第三波段，独立逐水位连通oracle核全像元／mask；输出Float64米制NaN、原网格／CRS、COG／overview、无scale/offset泄漏。非法波段、旋转网格、单位冲突及未应用scale四类必须精确匹配当前父执行和fill任务的参数失败，不能用权限／保护投影／加载失败充数。

### DEM D8 流向持久化

`make test-online ONLINE_SUITE=raster-flow-direction-d8-persistence`；脚本门禁 `make test-raster-flow-direction-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

三条COG保存链：直接流向、填洼接流向、全无效；四类错误（封闭洼地、非法波段、旋转网格、单位冲突）各按中英文提交，精确匹配当前执行的direction任务与语言原因，并核反向查询不会改写错误。严格核完整像元／mask、方向码、闭式最短平坦路径、无环与最终开放出口。

### D8 汇流累积持久化

`make test-online ONLINE_SUITE=raster-flow-accumulation-d8-persistence`；脚本门禁 `make test-raster-flow-accumulation-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

四条完整COG链：已登记流向、DEM接流向接累积、填洼接流向接累积、全无效。七类错误（非法方向含UInt64 NoData相邻大整数、内部伪终点、越界指向、无效下游、循环、非法波段、旋转网格）各中英文提交，14条精确accumulation任务失败，反向查询不得改写旧错误。独立逐源路径核完整计数／mask、不计自身与终点总量守恒。

### D8 下游流长持久化

`make test-online ONLINE_SUITE=raster-flow-length-d8-persistence`；脚本门禁 `make test-raster-flow-length-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。正式 DAG 必须按[统一流长入口](#d8流长统一入口的既有验收适配)显式指定 `method`；旧版本结果不代替该迁移验收。

六条COG链覆盖米制／国际英尺／美制测量英尺已登记方向、DEM→流向→流长、填洼→流向→流长及全无效；七类非法方向／内部伪终点／越界下游／无效下游／循环／非法波段／旋转网格各中英文提交，共14条准确length任务失败，查询语言不得改写旧错误。独立逐源路径按横纵对角步数核全像元／mask、米制中心距离和严格终点零，保留原网格／CRS，完整COG验证。

### D8 汇水分区持久化

`make test-online ONLINE_SUITE=raster-drainage-basins-d8-persistence`；脚本门禁 `make test-raster-drainage-basins-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

四条已登记多出口方向／DEM接流向／填洼接流向／全无效COG链，七类非法方向（含64位NoData相邻值）／内部伪终点／越界或无效下游／循环／非法波段／旋转网格各中英文提交，共14条准确失败链。独立逐源路径核全像元／mask、出口行列编号、相邻出口不合并、唯一归属与总数守恒。

### D8 上游最长流长持久化

`make test-online ONLINE_SUITE=raster-flow-length-upstream-d8-persistence`；脚本门禁 `make test-raster-flow-length-upstream-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。正式 DAG 必须按[统一流长入口](#d8流长统一入口的既有验收适配)显式指定 `method`；旧版本结果不代替该迁移验收。

米制／国际英尺／美制测量英尺已登记方向、DEM接流向、填洼接流向及全无效六条COG链，七类非法方向码／内部伪终点／越界下游／无效下游／循环／非法波段／旋转网格各中英文提交共14条准确length任务失败。独立逐源路径全像元oracle核横纵对角米制中心距离、无入流零、汇合取最长以及开放终点保留上游长度；输出Float64／NaN；完整mask、原网格／CRS及COG核验沿共同要求。查询语言不改写旧失败。

### raster-stream-order-d8-persistence

`make test-online ONLINE_SUITE=raster-stream-order-d8-persistence`；脚本门禁 `make test-raster-stream-order-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

六条COG链：已登记D8＋二值多级河网、汇流累积→重分类接分级、DEM→流向→累积→重分类→分级、填洼接同链、空河网、全无效。八类断流／非二值掩膜／有效覆盖不一致／完整网格不一致／非法D8方向（含UInt64 NoData相邻大整数）／循环／非法方向波段／非法掩膜波段各按中英文提交，共16条grade任务准确失败，反向查询不改写旧错误。

515×519完整网格中的确定性有效条带跨512块，含级别1／2／3、同最高级汇合、低级重复不升级、开放终点保留级别、合法零、原生NoData／mask／alpha／非有限覆盖。独立反向支流栈遍历与单独计数核全部像元与阈值掩膜，整幅逐像元／mask及完整COG／最近邻金字塔验证；严格保留原网格／CRS／Float64／NaN，级别不带距离单位。严格artifact／物理／Meta相等正整数大小，精确扫描父执行／用户／租户／引擎与当前执行所有实际加载输入血缘／Monitor，源和旧成果不变，失败零发布／新增扫描血缘及Runtime工作区／NFS／部署／Infra全部清理。

## raster-flow-accumulation-weighted-d8-persistence

`make test-online ONLINE_SUITE=raster-flow-accumulation-weighted-d8-persistence`；脚本门禁 `make test-raster-flow-accumulation-weighted-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

六条COG保存链为直接非负权重、全1权重、权重重分类、DEM→D8流向→加权累积、填洼接上述链、全无效；每条都核当前执行两份实际注册输入的血缘。负权重、缺失／非有限权重、网格不一致、非法D8码（含原始UInt64 NoData邻接大整数）、循环、非法方向波段、非法权重波段、累计溢出八类各中英文提交，共16条准确失败。逐源路径oracle核完整权重和／mask、合法零、自身排除、全1等价、出口守恒及单位；其余成果／身份核验沿共同要求。

## raster-watershed-d8-persistence

`make test-online ONLINE_SUITE=raster-watershed-d8-persistence`；脚本门禁 `make test-raster-watershed-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

四条直接指定点／标记重分类／DEM→流向／填洼接流向COG保存链，每条核当前执行两份实际注册输入血缘；嵌套、重复编号、无点、全无效、非法编号（UInt64 NoData相邻大整数）、覆盖不一致、网格不一致、非法方向、循环及方向／标记两个角色非法波段十一类各中英文提交，共22条准确watershed任务失败。515×519完整网格中的有效条带跨512块，包含最大安全编号、中间点、两条支流共用最终开放出口、未选有效零及NoData／mask／alpha覆盖。独立逐源完整路径oracle核全部编号／mask、点自身、原网格／CRS、COG／金字塔及单位清除。

### D8 河段编号独立持久化

`make test-online ONLINE_SUITE=raster-stream-link-d8-persistence`；脚本门禁 `make test-raster-stream-link-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

六条COG保存链覆盖已登记双输入、累积阈值重分类、DEM／填洼接流向、空河网及全无效；八类非法输入各中英文提交，共16条准确失败。独立上溯oracle核完整编号／mask、起点行列顺序、汇合点归下游、单像元汇合出口及交叉河流独立。

### Shreve 独立持久化验收

`make test-online ONLINE_SUITE=raster-stream-shreve-d8-persistence`；脚本门禁 `make test-raster-stream-shreve-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

六条Shreve保存链加同源默认／显式Strahler两条对照，共八条COG；断流、非二值掩膜、覆盖／网格不一致、精确64位非法方向、循环、双角色非法波段及非法method九类各中英文，共18条准确失败。独立逐源头前向路径oracle核完整量级／mask、出口源头数守恒、非河网供体排除和对角交叉独立，默认／显式Strahler完整物理成果相等且符合独立原规则；整数越界继续单列Native证据，不模拟Hosted越界或降低产品上限。

### D8 河网转矢量线独立 Hosted 验收

`make test-online ONLINE_SUITE=raster-stream-to-vector-d8-persistence`；脚本门禁 `make test-raster-stream-to-vector-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

三种水平单位的直接双输入，加累积阈值、DEM／填洼完整链及空河网／全无效，共八条GeoPackage保存；孤立单像元（含非河网入流）、混合孤立点、断流、非二值掩膜、覆盖／网格不一致、精确64位非法方向、循环及两个角色非法波段十类各中英，共20条准确失败。独立逐边oracle核每条真实河网边恰好一次，完整中心坐标／方向／ID间隔／零基起止行列／米制长度、汇合端点、单像元终点、跨块及交叉独立。GeoPackage核完整几何／字段类型／CRS，空成果核Schema与CRS；其余成果／身份核验沿共同要求。编号／长度与坐标数值极限继续单列Native，不模拟Hosted越界；不改原套件、权限、算法或跨模块产品逻辑，不计UI或容量。

## raster-snap-pour-points-persistence

`make test-online ONLINE_SUITE=raster-snap-pour-points-persistence`；脚本门禁 `make test-raster-snap-pour-points-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

8条COG保存链包括米／国际英尺／美制测量英尺直接吸附、零半径、标记重分类、DEM接流向累积吸附、填洼接上述链，以及流向→累积→吸附→汇水区。13类各中英文提交，共26条失败链：碰撞、重复编号、全零无点、全无效、非法计数、非法编号、覆盖不一致、网格不一致、负半径、两个角色非法波段、旋转网格及吸附后嵌套；嵌套须由原汇水区节点失败，吸附节点先成功。整数非法样本包括原UInt64 NoData相邻大整数；非有限半径／单位溢出等极限保留Native证据。

独立逐候选核完整编号／mask、真实CRS米制圆边界、距离／行列决胜、跨分水岭／无效区、零与编号保留；清除输出单位；COG及其余成果／身份核验沿共同要求。

## raster-subcatchments-d8-persistence

`make test-online ONLINE_SUITE=raster-subcatchments-d8-persistence`；脚本门禁 `make test-raster-subcatchments-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

7条COG链：非嵌套增量分区、相同输入的原完整汇水区对照、多层嵌套与并列支流、标记重分类、DEM接流向、填洼接流向，以及流向→累积→真实移动点的吸附→增量分区。两条非嵌套成果须完整像元一致；嵌套按下游第一个指定点归属且点自身归本点，保留用户编号、未选有效零和原无效覆盖。

14类各中英文，共28条增量失败：重复编号、无点、全无效、非法编号、覆盖／网格不一致、非法方向码、内部伪终点、越界／无效下游、循环、两个角色非法波段及旋转网格；再加原完整汇水区对同一嵌套输入的两条中英文准确拒绝，共30条失败。包含原UInt64 NoData相邻非法大整数。独立逐源路径oracle核全部编号／mask、跨块、多波段、首点／自身、唯一归属和像元数守恒；不读取产品拓扑标签。输出清除单位；其余成果、身份、失败及清理核验沿共同要求。

## raster-flow-accumulation-area-d8-persistence

`make test-online ONLINE_SUITE=raster-flow-accumulation-area-d8-persistence`；脚本门禁 `make test-raster-flow-accumulation-area-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

八条COG保存链为米制／国际英尺／美制测量英尺方向接area、同一米制方向的默认count／显式count对照、DEM→流向→area、填洼接上述链及全无效area。519×515矩形网格、显式第二数据波段，覆盖跨512块、多出口、孔洞、合法零、部分透明、孤立开放终点及边界非零方向。

非法方向码（含原始UInt64 NoData邻接大整数）、内部伪终点、越界指向、无效下游、循环、非法波段、旋转网格与非法measure八类各中英文提交，共16条准确失败；反向查询不改提交语言。独立逐源路径核完整像元／mask、area包含自身、平方米m2、count不计自身且清空单位、两count对照一致与出口面积守恒；输出Float64／NaN；其余成果、身份、失败及清理核验沿共同要求。面积溢出／下溢、累计溢出与精确计数上限继续单列Native数值极限证据，不记Hosted已验。

## raster-distance-to-stream-d8-persistence

`make test-online ONLINE_SUITE=raster-distance-to-stream-d8-persistence`；脚本门禁 `make test-raster-distance-to-stream-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

米／国际英尺／美制测量英尺方向＋河网、累积阈值、DEM、填洼、空河网及全无效共8条COG保存链；519×515矩形网格与显式第二波段覆盖跨512块、多出口、对角交叉、孔洞及alpha／mask。独立逐源路径核首次命中、河网自身零、未到达NoData及输出有效覆盖，输出米制单位；其余成果／身份核验沿共同要求。

非法码（含原始UInt64 NoData邻接非法整数）、内部伪终点、图外／无效下游、循环、断流、非二值掩膜、覆盖／网格不一致、旋转网格及两个角色非法波段共12类各中英文，合计24条准确失败；包含河网之外或命中之后的非法路线。长路径稳定性、距离溢出及极小步长继续单列Native数值证据，不记Hosted已验。

## raster-hand-d8-persistence

`make test-online ONLINE_SUITE=raster-hand-d8-persistence`；脚本门禁 `make test-raster-hand-d8-online-runner`。准备、成果核验和清理遵守[专项共同要求](#水文与分区统计专项共同要求)。

三种高程单位与水平CRS交叉的直接HAND、累积阈值、DEM、填洼、空河网及全无效共8条COG保存链，三输入显式数据波段、519×515矩形网格，覆盖跨512块、多出口、孔洞、平坦零、负高程与alpha／mask。独立逐源路径oracle核首次河网基准／完整高程差及mask、未到达NoData；输出米制；其余成果／身份核验沿共同要求。

命中前／命中后／空河网逆坡及填洼流向混用原DEM，非法码／内部伪终点／图外与无效指向／循环／断流，非二值河网、两角色覆盖与网格不一致、旋转网格，三个角色非法波段、非法高程单位及单位冲突共21类各按中英文提交，合计42条准确失败。十万像元长路径、原始64位高程相邻差值及溢出／下溢继续单列Native，不计Hosted数值极限。

### 水文专项的固定结果与待验边界

以下仅收敛已有归档结论，完整物理／扫描／当前执行血缘／Monitor、源旧不变和清理均按各轮实际报告认定；不代表当前 HEAD 已重跑。前七项 Hosted 归档均无原 TIFF，只有现场物理回执，不称为离线像元重算。

| 专项 | 固定证据与长期结论 |
| --- | --- |
| 填洼 | `8599f94ed8aefffd98370ee1c4d19d8d81c158ce`，[Run39727166222／Job119653632678](https://github.com/pampa0629/addp/actions/runs/39727166222/job/119653632678)：3 链／4 类准确失败、三方大小、扫描／血缘／Monitor及清理通过；不证明平台完整双语矩阵或完整水文分析。 |
| D8 流向 | `3944846c9`，Run39750207818／Job119728593684：3 链／8 次双语失败、平坦路径／开放出口／无环及清理通过。归档 `12414557855`，不计该源码 Linux 全量。 |
| 计数累积 | `4fba03313`，Run39761858875／Job119767722319：4 链／14 次双语失败、排除自身与终点总量及清理通过；同源 Linux Run39761773681／Job119767424921 全量 2360 项通过，不证明后继面积或加权模式。 |
| 旧下游流长 | `dd04fb0637f381db9dfc3532c70d87135b34d21b`，[Run39783255643／Job119839656541](https://github.com/pampa0629/addp/actions/runs/39783255643/job/119839656541)：6 链／14 次失败及清理通过。本地栅格聚合曾有 3 项原生命周期模拟超时，不能改计整套通过。 |
| 开放终点分区 | `6bf471fc2b6f8ee75fc01e2ce56e317e1375b5dc`，[Run39797527146／Job119888617829](https://github.com/pampa0629/addp/actions/runs/39797527146/job/119888617829)：4 链／14 次失败、出口编号／归属守恒及清理通过。 |
| 旧上游最长流长 | `ddd32f9536857cecc4dc40d63d356755c4ca297b`（含夹具修复 `2d6d92780`），[Run39876682648／Job120163869951](https://github.com/pampa0629/addp/actions/runs/39876682648/job/120163869951)：6 链／14 次失败及清理通过；仅共享不可变数学期望，物理成果仍逐轮重读，不冒充 Linux 全量。 |
| Strahler | `bde58e038751d932888bd7696046715e4ba21410`，[Run39880792263／Job120177404317](https://github.com/pampa0629/addp/actions/runs/39880792263/job/120177404317)：6 链／16 次失败及清理通过，不证明 Shreve、加权累积或业务 DELETE。 |

上述七项均保持原 600 秒预算、普通 User 原 12 权限和同源产品 Runtime。**后继统一流长**的两模式结果改查[既有验收适配](#d8流长统一入口的既有验收适配)，不把旧无 `method` 的结果当作当前接口验收。

**尚无本文已核验 Hosted 通过结论**：加权累积、指定点汇水区、河段编号、Shreve、河网转线、点吸附、增量子汇水区、汇流面积、到河网距离及 HAND。已记录的本地 GDAL／脚本／登记通过只证明准备；其中指定点汇水区最终候选为 `19bb583a21b4ffd5d7ffe1d5099bb57d02d58ab7`，增量子汇水区最终候选为 `adad1eebdd2a117ea800cc20bb5a1bb70d70154d`，均不能用较早聚合候选外推整树通过。Shreve 的 `883aed7b5` Linux 全量 4189 项是独立 Native 证据。其余实际运行和完整固定版本边界统一见[栅格验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)与[后续工作](../../../next/栅格算子体系后续专题.md#三后续工作)；不因本次整理触发新运行。

### DEM 平均曲率独立 Hosted 验收

用户已确认独立手工`raster-curvature-persistence`，复用Hosted／NFS、同源默认入口Runtime、普通用户原12项权限及独立600秒业务预算。三种高程单位与不同水平CRS交叉的直接曲率、填洼接曲率、重分类接曲率、全无效、无完整有效邻域和小尺寸不足3行／列共8条COG保存链；519×515矩形多波段样本显式第二数据波段，覆盖跨块、倾斜平面、凹凸／鞍面／中心脉冲、合法零、内嵌mask、NoData和透明／部分透明。独立九点设计矩阵最小二乘和解析曲面核完整像元及mask、九点等权／自由常数项、凸正凹负、单位1/metre和一圈无效邻域；金字塔按实际尺寸逐层核验，小图不强制不存在的层级。

非法波段、alpha作高程、复数高程、旋转／南向网格、经纬度CRS、缺CRS、缺定位网格、非法单位、单位冲突、未应用scale、未应用offset共12类，各按中英文提交，共24条准确失败；须在曲率节点以准确领域错误失败，反向查询不改提交语言，失败零发布／新增扫描／派生血缘。严格核COG、原网格／CRS、Float64／NaN、三方相等正整数大小、精确自动扫描、同用户和当前执行实际输入血缘／Monitor、源和先前成果不变及全部清理。原生64位相邻高程、极端数值和不可表示步长继续单列Native；不计UI或容量，不改产品、原套件、权限或跨模块逻辑。

COG原生新建金字塔按GDAL自定义网格的整数减半规则核精确尺寸，519×515样本为259×257、129×128、64×64，小图为零层；每层再独立核完整最近邻像元及mask，依据GDAL `frmts/gtiff/cogdriver.cpp`及真实文件事实修正了首轮向上取整的错误期望，未降低像元误差阈值或删去布局断言。所有成功门禁跟踪输入无漂移、无新增Python原生崩溃报告。

标准入口：`make test-raster-curvature-online-runner`；真实 GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`。专项筛选与本地 Runner 不算全量或真实 T4。

**固定结果**：`05e44e4a0a3cce04c9860765745d074d0ab70e7c` 的 [Hosted Run40339721289／Job121630801003](https://github.com/pampa0629/addp/actions/runs/40339721289/job/121630801003) 在原 600 秒内通过 8 条 COG 链与 24 次双语拒绝；全像元／mask、金字塔、严格大小、精确自动扫描、当前执行血缘／Monitor、源和旧成果不变、工作区／NFS／部署清理与 Infra 零残留均有现场报告。artifact `12659759018` 未保存 COG 原字节，不能声称离线重算。

Native 前置为 `cabfbe79c3d09c8db9211abbda8c848a0beb011f` 的 [Job121497755392](https://github.com/pampa0629/addp/actions/runs/40300756435/job/121497755392)，全量 7298 项通过；它不等于上述 Hosted 源码整树 Native 通过。数值极限仍单列 Native；UI、容量、生产、业务 DELETE、全量 IAM 和旧原生崩溃不由本项证明。未解决问题统一见[栅格后续专题](../../../next/栅格算子体系后续专题.md#原生稳定性)。

### DEM 剖面曲率独立 Hosted 验收

独立手工入口`raster-profile-curvature-persistence`已准备，复用既有Hosted／NFS生命周期、同源产品默认入口Runtime、普通User原12权限和独立600秒业务预算；仅手动登记，不加入nightly，不取消排队任务。没有修改产品计算逻辑、权限、其他模块或旧套件。

固定12条COG成功链：直接米制／国际英尺／美制测量英尺高程、填洼接profile、重分类接profile、完全水平平地、非平地零梯度残差、全无效、有有效像元但无完整邻域、小图，以及同源默认mean与显式mean两条对照。519×515、矩形投影网格、显式第二数据波段的样本覆盖跨块、凸凹／鞍面／倾斜面、极小非零坡度、NoData／mask／透明和部分透明；3×3残差与2×7小图不虚构金字塔。独立九点最小二乘与第一／第二基本形式方向商核完整像元／mask，保留`RTOL=1e-10`、`ATOL=1e-11`；平地严格0、其他零梯度严格NaN，mean另用基本形式迹核验，并逐字节比较默认／显式mean。

13类中英文共26次准确失败：非法数据波段、alpha波段、复数、旋转、南向、地理CRS、缺CRS、缺网格、非法单位、单位冲突、未应用scale、未应用offset、非法method。准确匹配同次执行的curvature任务和双语原因；反向查询不得改写提交语言。成功严格核完整COG／最近邻金字塔、原网格／CRS、Float64／NaN与`1/metre`、artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前执行实际输入血缘／Monitor；失败必须零发布／新增扫描／派生血缘。每次核源及先前成果不变，生命周期覆盖所有退出与零残留。

标准入口：`make test-raster-profile-curvature-online-runner`、`make test-geopython-workflow` 与 `make test-python-ci-registration`，脚本由 `make test-raster-online-runner` 聚合。成功保存后、负例前集中等待每条实际输入到当前成果的 active derive 血缘，禁止手工 collect 或额外重试。阶段进度只保存枚举、序号、状态与相对耗时，不含 SQL、原始响应或凭据；未完成阶段不算通过。同次物理核验可共享只读期望事实，完整实际文件重读、精确求和、污染反例及数值容差保持。

| 结论 | 最小证据与边界 |
| --- | --- |
| 首轮失败 | `06f9caf5637580d72e5ef571a8a14fd91d31cd46`，[Run40265540763／Job121387037149](https://github.com/pampa0629/addp/actions/runs/40265540763/job/121387037149) 在 600 秒超时，无最终报告；请求次数不算成功链数，清理通过仅证明清理。 |
| 等待修正后仍失败 | `909be4e7b1e1269c916bcf431c458f0e1db1dd17`，[Run40272625047／Job121408540481](https://github.com/pampa0629/addp/actions/runs/40272625047/job/121408540481) 完成 12 条链，负例第 23 次开始后超时；剩余 4 次负例及源最终核验未完成。 |
| 修复与 Native | 去除同次 oracle 的重复计算，保持独立预期、12／26 范围及 600 秒。`d8ddc96dc03d862c66de5ee3ca32b1d79f6b07e4` 的 [Linux Job121492608736](https://github.com/pampa0629/addp/actions/runs/40299191579/job/121492608736) 全量 7266 项通过，不外推后续整树或 Platform。 |
| 正式 Hosted | `7157fb44f4b09bb08e097469ecf5360e03c5c0af`，[Run40305988194／Job121515098139](https://github.com/pampa0629/addp/actions/runs/40305988194/job/121515098139) 在 600 秒内通过 12 链／26 次拒绝、完整物理／扫描／血缘／Monitor、冻结语言、源旧不变、全部清理及 Infra 零残留。artifact `12643687867` 无 COG 原字节，不算离线重算。 |

仅证明上述固定 API T4；UI、容量、任意影像、业务 DELETE、全量 IAM 与数值极限未覆盖。旧超时保留为失败；并行镶嵌 double-free 仍未确定根因，旧本地 Parquet 失败未在本节闭合，均不由新全量或本项通过关闭。

### DEM 切向曲率独立 Hosted 验收

独立手工入口`raster-tangential-curvature-persistence`、夹具、标准runner、Native recipe、变更选择和CI登记已准备，复用既有Hosted／NFS、同源默认入口Runtime、普通User原12权限及独立600秒业务预算；manual-only／非nightly，保留排队任务，不修改产品算法、权限、其他模块或旧套件。

固定13条COG成功链：三种高程单位与不同水平CRS交叉的直接tangential、填洼接切向、重分类接切向、完全水平平地、非平地零梯度残差、全无效、无完整有效邻域、小图，以及同源默认mean／显式mean／显式profile三条对照。519×515矩形投影多波段样本显式选择第二数据波段，覆盖跨块、横向凸凹／鞍面／倾斜面、陡坡、小而非零坡度及非零切向曲率、NoData／mask／透明／部分透明；3×3残差和2×7小图按实际零层overview核验。

独立九点最小二乘和第一／第二基本形式方向商核全幅像元／mask，保留`RTOL=1e-10`、`ATOL=1e-11`；切向与平面曲率专用解析样本必须区分，水平平地严格0、其他零梯度NaN。默认／显式mean的数据字节一致；profile另用自身方向oracle。三个实际保存结果仅在非零梯度共同有效处核`profile+tangential=2×mean`，不能用该关系生成切向主预期。完整物理核验与这三项实际数据交叉检查各有独立污染反例。

13类中英文共26次准确失败：非法波段、alpha、复数、旋转、南向、经纬度、缺CRS、缺网格、非法单位、单位冲突、未应用scale、未应用offset、非法method。准确核同次执行的curvature任务和提交语言原因，反向查询不改旧失败；失败零发布／新增扫描／派生血缘。严格核完整COG／最近邻金字塔、原网格／CRS／Float64／NaN／`1/metre`、artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前执行所有实际输入血缘／Monitor、源与先前成果不变、所有退出清理及零残留。

标准入口：`make test-raster-tangential-curvature-online-runner`、`make test-geopython-workflow` 与 `make test-python-ci-registration`。等待顺序、闭集阶段记录和独立物理核验按[剖面专项](#dem-剖面曲率独立-hosted-验收)的相同约束执行，切向仍独立核对 13 链／26 次双语拒绝及三方法对照。

**失败与收口**：`2f704eb174ba00a9f8e54318f22c5264f37c487a` 的 [Run40267387076／Job121392642425](https://github.com/pampa0629/addp/actions/runs/40267387076/job/121392642425) 在 600 秒超时，无最终报告，提交／查询次数不算通过链数；清理独立通过。修后 `909be4e7b1e1269c916bcf431c458f0e1db1dd17` 的 [Run40272637832／Job121408579032](https://github.com/pampa0629/addp/actions/runs/40272637832/job/121408579032) 在原预算内通过 13 链／26 次拒绝，完整物理／扫描／血缘／Monitor、三方法 228530 个共同有效像元关系、默认 mean 对照及清理／Infra 零残留。artifact `12630112467` 无 COG 原字节；已核 Job 元数据及场景原始日志，未补取完整 Job 日志，不宣称离线重算。

`6cdc38dff129492a83f3200b2b27ffc908e8e6ca` 的 [Linux Native Job121379953801](https://github.com/pampa0629/addp/actions/runs/40263251151/job/121379953801) 全量 6918 项通过，仅覆盖该产品源码，不覆盖后来新增夹具或 Hosted 909 整树。Platform 未在本项独立验收；UI、容量、任意影像、全量 IAM、业务 DELETE、平面曲率新能力及 Native 数值极限不外推。旧 double-free 和其他未解决问题见[栅格后续专题](../../../next/栅格算子体系后续专题.md)。

## DEM 专项共同验收边界

以下仅适用于平面曲率、填挖方、TPI、TRI四个独立手工suite：Hosted／独立NFS、同源产品默认入口Runtime、非默认Tenant普通User原12权限，各自600秒业务预算，不并入nightly或改变其他套件。算法准入归[栅格算子契约](../../workflow/栅格算子契约.md)，本页保留固定矩阵、独立期望、证据与缺口；未执行范围不能由本地／部分CI补认。

- 固定519×515矩形多波段样本显式第二高程波段，跨512块；逐项按专节覆盖NoData／mask／alpha、合法零／大基值。独立oracle不调用产品函数；容差按各专项，TPI／TRI及填挖方最终存储值严格比、一ULP篡改拒绝，不能套曲率容差。
- 成功逐份核完整像元／mask或CSV值、单位／原网格／CRS、精确自动扫描父execution／User／Tenant／Engine、当前实际输入血缘及Monitor；COG另核布局／nearest概览和artifact＝物理＝Meta严格正整数大小。CSV只核物理＝Meta，不生成RasterArtifact或补造第三方大小。
- 失败准确绑定当前节点／原因／提交语言，反向查询不改冻结语言；零发布／新增扫描／派生血缘，仍全检源和先前成果。保存后的精确血缘沿普通收集器逐条等待，不主动collect绕过。
- 原件清单以SHA-256核验，实际归档范围按各专项，不补造缺失原件。源／先前成果与Meta保持，退出核Runtime工作区、NFS export／owned root、镜像／部署／Infra及私有空间清理；业务、清理与Infra `zero_residuals`独立认定。封闭进度不存响应／SQL／凭据／错误全文。
- Native数值极限单列；固定Hosted不外推后续HEAD、全部类型／单位组合、UI、容量、任意影像、生产或受管业务DELETE，不以新通过关闭旧超时／double-free。

## DEM 平面曲率独立 Hosted 验收

`raster-plan-curvature-persistence`沿[共同边界](#dem-专项共同验收边界)，15成功COG链：三组高程／水平单位直接plan、填洼／重分类接plan、水平平地／非平地驻点／全无效／无完整邻域／小图、真实存储高程`8z+64`，及默认mean／显式mean／profile／tangential四对照。样本含凹凸／鞍面／倾斜面、圆形等高线、可表示极小非零梯度和部分透明。

主预期为独立九点等权自由常数最小二乘＋等高线单位切向水平法向加速度，`RTOL=1e-10/ATOL=1e-11`，不用产品卷积核／切向结果造plan预期。核圆形`-1/r`、正高程缩放／平移不变、水平平地严格0、其余精确零梯度NoData、无阈值；默认／显式mean保存字节一致。四方法只在非零梯度共同有效处核`profile+tangential=2×mean`及`tangential=plan*g/sqrt(1+g²)`；所有物理／语义断言有污染反例。

13类各中英文，共26准确失败：波段、alpha、复数、旋转、南向、经纬度、缺CRS／网格、非法高程单位／冲突、未应用scale／offset、非法method。绑定curvature任务，完整Float64／NaN／每米单位、按实际尺寸nearest概览及共同扫描／血缘／保持／清理均必验。

标准`make test-raster-plan-curvature-online-runner`、`make test-geopython-workflow`及输入选择／手工workflow登记；本地筛选不算全量。固定`79ebc6ff93f2301db51a34e64f54e7ac0976014b`的[Linux Job121440770332](https://github.com/pampa0629/addp/actions/runs/40283174360/job/121440770332)全量7253过；同源[Hosted40283794715／121442687622](https://github.com/pampa0629/addp/actions/runs/40283794715/job/121442687622)600秒内完成15／26、228529共同有效像元关系及完整清理。artifact12635131765无COG原字节，不算离线重算；父Platform公共crypto依赖冲突不计整套通过，旧剖面超时／原生double-free仍未由本项关闭。

## 双DEM填挖方统计独立 Hosted 验收

`raster-cut-fill-persistence`沿[共同边界](#dem-专项共同验收边界)，30源覆盖双源独立NoData／mask／透明与部分透明、零／负高程；水平网格含米／美制测量英尺。12成功CSV链为混合挖填、前后互换、全不变、纯填／纯挖、三单位组合、填洼／重分类接统计、Int64／UInt64相邻大高程。

18类中英文共36失败，须绑定cut_fill：两角色非法波段、非法单位／冲突、alpha／复数、scale／offset、CRS／尺寸／完整网格不符、旋转／南向／经纬度／缺CRS／定位、一源全无效／有效域不交。独立完整35×35周期闭式计数＋特殊像元修正，用200位Decimal算高差／面积／体积，另以完整标量遍历核周期；面积用实际解码网格／CRS线性换算、保WKT序列化末位事实，不以容差替CSV值。

严格单行13列（6 Int64／7 Float64），核总／有效／排除及挖／填／不变守恒、平方米／立方米、挖填非负／净填减挖／互换关系。每份当前execution双源血缘及Monitor须齐，全部保存后、负例前逐条核完；内部取证见[受控血缘](#填挖方与tpi受控血缘取证修复)。

核验后逐份将原CSV存`cut-fill-csv/`，`cut-fill-csv-manifest.json`记名／大小／SHA；收尾逐字节再比，归档变更／额外文件／软链接均失败。12原件／清单／完整日志／回执须归档，普通表格保存与大小按共同边界。

**固定结果**：`a26d758daab9149e6fdda628aa202957d9245906`，[Hosted40635872131／122642597594](https://github.com/pampa0629/addp/actions/runs/40635872131/job/122642597594)，12成功／36准确失败、266531ms＜600000ms。七服务同源码／干净仓库、Tenant2／User33／12权限、默认`sh container_entrypoint.sh`；12精确扫描／24实际双源Meta关系及Monitor，内部source-port／唯一output.save／create及save／derive与公开隐藏operations分别验证，源／旧成果／工作区／外层cleanup／Infra均通过。

artifact12794083457的12 CSV与清单／hash／Runtime物理base64一致，固定Decimal夹具＋已归档定位事实独立重算单行全13列、计数／面积／净量／互换关系精确通过。源TIFF未归档，不称离线重读DEM。旧`2edf97e60`的Run40485888750与`16452b150`的Run40537589903均首条血缘失败；后者仅证公开operations=null，不补认内部事实。新来源a26不改写旧失败。

## DEM 地形位置指数（TPI）独立 Hosted 验收

`raster-tpi-persistence`沿[共同边界](#dem-专项共同验收边界)。13成功COG：混合凸凹、平地／倾斜平面／对称鞍面三合法零、三组高程与水平单位交叉、填洼／重分类、Int64／UInt64普通大基值原生高差、全无效／无完整九点邻域。保跨块多波段、NoData／mask／透明／非有限覆盖与有效部分透明。

13类中英文共26失败：波段、alpha、复数高程、非法单位／冲突／未知波段单位、scale／offset、经纬度／旋转／南向／缺CRS／定位，逐条定位当前tpi与冻结语言。独立高精度Decimal八邻均值及解析平面／鞍面，精确核最终Float64／NaN全像元／mask、零／正负、原网格／CRS／metre，不调产品函数。COG布局／概览／大小、扫描与实际血缘／Monitor、失败及清理按共同边界。

核后原COG存`tpi-cogs/`及`tpi-cogs-manifest.json`大小／SHA清单，不能静默修复；收尾再核，未知文件／软链接／归档变化均失败。受控血缘见[下节](#填挖方与tpi受控血缘取证修复)，期望缓存与后续固定复验见[计时专题](#tpi验收计时与本轮独立期望值缓存)。

**固定结果**：`a26d758daab9149e6fdda628aa202957d9245906`，[Hosted40635880285／122642624444](https://github.com/pampa0629/addp/actions/runs/40635880285/job/122642624444)，13成功／26失败、535785ms＜600000ms；七服务同源／干净、Tenant2／User33／12权限、默认`sh container_entrypoint.sh`。13扫描／13实际源Meta关系及Monitor齐，内部input.source／唯一output.save／create／raster_save／derive与公开隐藏operations分别核验，源／旧成果／工作区／cleanup／Infra通过。

artifact12794317336的13原COG独立重开核519×515、原CRS／完整仿射、Float64／NaN／metre、全像元／mask、三级nearest概览及COG布局，固定Decimal夹具精确比含大基值／全无效／无邻域；原件／清单／三方大小／闭合血缘一致。源TIFF未归档，仅据固定夹具重建oracle，不称离线重读源或重跑产品／Hosted；Homebrew PROJ_DATA／PROJ_LIB只设审计进程。

旧`63d6299ea`的Run40538763760首条execution血缘失败，未完成13／26／原件归档，完整actual差异未存；a26不能回填旧内部事实。此为一次受控API验收，不认定稳定吞吐。

### 填挖方与TPI受控血缘取证修复

仅两套独立Hosted的受控只读证据，不改公开Develop／Monitor隐藏operations。内部source-port／create／derive逐项核，再沿原普通User12权限核Meta实际关系；数据库补证不算API授权通过，无公开回退。

仅当次Hosted Linux隔离addp_online准备私有归属回执，冻结容器ID／启动时刻／suite／Run ID，读时再核未重启、Tenant／真实User／execution／完全相同DAG。固定有界只读事务，不接受任意SQL、写库／造事实／主动collect；DB凭据不进User请求，只归档闭合字段／大小／hash，拒绝前保公开／内部差异及失败类型，错误仅类型／指纹。原矩阵／预算／数值NULL／原件／扫描／身份／Monitor／保持／清理不减。

标准`make test-raster-cut-fill-online-runner`、`make test-raster-tpi-online-runner`、`make test-python-ci-registration`、`make test-geopython-workflow`须拒公开泄露、内部缺失／错源端口操作、身份／DAG错绑、容器替换、取证／归档篡改。

修复`1a0b3bb27d7ada93404fff7b76fd677d83800122`的[Linux Job122636163645](https://github.com/pampa0629/addp/actions/runs/40633847309/job/122636163645)全量7858过；后续a26两Hosted非Markdown输入相同，结果见[填挖方](#双dem填挖方统计独立-hosted-验收)／[TPI](#dem-地形位置指数tpi独立-hosted-验收)。固定输入等价不等于整树SHA相同，不外推HEAD／其他Job或旧double-free根因。

### TPI验收计时与本轮独立期望值缓存

缓存仅本轮独立Decimal期望，绑定Run／夹具代码／源hash，私有锚点及数组hash每次核，缺失／篡改／跨轮复用／清理失败均失败。首次生成及清理纳600秒，不缓存真实成果、不并发业务或改轮询／资源策略／权限／产品。仍13成功／26双语拒绝、39调用／429次真实成果全检，完整像元／mask／概览／COG／大小／扫描／内部公开血缘／Monitor／保持／清理不减。

`189f5f8ce3219ba6257d055f975f9bd2154c0b1d`的[Hosted40645432302／122673041241](https://github.com/pampa0629/addp/actions/runs/40645432302/job/122673041241)终审通过：259655ms含准备8432ms（期望7703ms）及清理325ms，嵌套时间不重复加；39调用／429全检、13 COG原件／manifest及离线Decimal／mask／三级nearest复核通过，字节与早期a26一致。较535785ms减少276.13秒／约51.5%，只证两固定运行，不作吞吐／容量承诺，数值极限仍Native。

准备`2b7df04aa5fdd097f4a2fe1c11062f69c0c6cae3`的[Linux40644787773／122670954049](https://github.com/pampa0629/addp/actions/runs/40644787773/job/122670954049)标准Native全量7887过／24 warnings／2119.73秒。原ZIP／digest／CRC、日志／步骤、原件与只读回执／矩阵／清理只在固定范围复核，不计重跑或后续源码通过。

## DEM 地形崎岖度（TRI）独立 Hosted 验收

`raster-tri-persistence`沿[共同边界](#dem-专项共同验收边界)，13成功COG／13类中英文共26失败。链含凸凹／平地／倾斜／鞍面、三组单位、填洼／重分类、Int64／UInt64大基值微差、全无效／无完整九点；第二波段／跨块／NoData／mask／alpha／非有限覆盖、零及非负均全检。

独立2500位Decimal Riley平方和开方，不导入产品函数；每成功／负例后重读所有既有成果，39调用／429实际全检，拒一ULP错误。完整mask、metre／原网格／CRS、COG布局／三级nearest、三方大小、精确扫描与当前实际输入内部／公开安全血缘／Meta关系／Monitor、失败零发布／扫描／血缘及清理按共同边界。原件／清单不可静默修复，源／旧成果保持；只读取证限本轮容器／User／Tenant／execution，期望缓存绑定Run／夹具／源hash，不缓存实际成果，准备及收尾均纳600秒。

`db2638ac9d7dc2f11a1fc949d0be77b0a0f238de`的[Hosted40652107491／122694375654](https://github.com/pampa0629/addp/actions/runs/40652107491/job/122694375654)终审通过，368511ms。artifact归档13原COG／清单，固定Decimal夹具独立复算全像元／mask／CRS／米制／三级nearest及布局；源TIFF未归档，不称从归档源重生。官方ZIP4208788字节，digest／CRC／13原件、受控回执／13扫描／Monitor／矩阵及零残留已只读复核，不等于新Hosted。

准备`872375a48da8150a86936f8c6ae67d293b007a62`的[Linux40650267385／122688461218](https://github.com/pampa0629/addp/actions/runs/40650267385/job/122688461218)Native全量8134过／39 warnings／1967.59秒；此前本地231专项／runner／登记仅准备，不与全量累加。额外类型／单位组合与数值极限仍分层，固定范围不作UI／容量／稳定性或后续源码认证。

## D8流长统一入口的既有验收适配

仅保留 `raster_flow_length_d8`，必填method：原下游suite `raster-flow-length-d8-persistence` 提交downstream，原上游 `raster-flow-length-upstream-d8-persistence` 提交upstream_longest。不新增suite／脚本，不自动迁移旧DAG；两套各6成功COG／14准确双语失败及原米制步长、零值／最长汇合断言保持。

固定 `fdbe2d58d55ab2b215a609bfbd58b5c34e7c27ed` 的[下游40657243422／122711172418](https://github.com/pampa0629/addp/actions/runs/40657243422/job/122711172418)、[上游40657252005／122711199682](https://github.com/pampa0629/addp/actions/runs/40657252005/job/122711199682)各完成6成功／14失败、6次扫描／血缘／Monitor与清零，业务543430／414932毫秒。原COG未归档，只认现场物理回执；旧独立ID证据不追认为统一入口，不认证UI／容量／业务DELETE／已有DAG迁移。

[Linux40658271835／122714572426](https://github.com/pampa0629/addp/actions/runs/40658271835/job/122714572426)固定 `093a685b8558b74ecbdccb6b95b026d33af603e9` 的Native全量8413通过：D8另8个可执行输入相同，仅元数据测试矢量目录断言变化且D8 AST一致。fdbe条件跳过的Native不计通过，093也不证明粗糙度或当前HEAD全量。

算法与参数以[栅格算子契约](../../workflow/栅格算子契约.md#栅格计算契约)和[矢量算子契约](../../workflow/矢量算子契约.md)为准，以下仅列验收范围与独立参考。

**以下子专题的共同要求**：仅合并各节已声明的手工隔离Hosted／NFS、同源默认入口Runtime及600秒业务预算，栅格普通User原12权限、矢量拓扑原11权限不互换；来源准备者／metadata_read Grant仍按各节独立限定。不改产品／权限／安全投影／原套件，不重试、扩预算或新增nightly。

- 每次核完整实际成果、源与先前成果原字节／Meta不变、精确父execution／User／Tenant／Engine扫描、当前实际加载输入的内部血缘／公开安全投影／Meta关系及Monitor。COG核原网格／CRS（或明确缺定位）、类型／mask、完整布局及实际概览，artifact＝物理＝Meta正整数大小；CSV／GPKG仅物理＝Meta，不虚构RasterArtifact或把子层重复计大小。
- 独立oracle不调用产品算法，实际成果逐次重读不缓存；允许的不可变预期缓存绑定本轮／夹具／源hash。按各节数量归档原件／清单／冻结预期，缺失／篡改／额外文件／链接拒绝，不静默修复。仅原已声明套件使用有界只读observer／SQL，公开安全投影不扩大。
- 失败须到当前指定业务节点及原因／提交语言，前置失败不充数，反向查询不改原错误；零发布／新增扫描／派生血缘。全部退出清所属缓存／工作区、NFS、Runtime／镜像／registry、部署／Infra及已声明Grant，首败与清理错误分别保留，清理失败整套失败；部署销毁不证明业务DELETE。

标准准备沿各节Runner、`make test-raster-online-runner`／既有拓扑入口，真实GDAL及污染反例沿 `make test-geopython-workflow`，输入／选择／登记同次命中，不累计共享测试为唯一数量。正文只保留代表性证据与未完成项：本地／登记、Linux全量、Hosted API、真实页面各按固定版本认定，不证明当前HEAD、容量、任意影像或未列范围。后续状态统一见[栅格专题](../../../next/栅格算子体系后续专题.md#三后续工作)。

### DEM 地形粗糙度独立 Hosted 验收

`raster-roughness-persistence`；准备 `make test-raster-roughness-online-runner`。13条COG链覆盖中心极大／极小及正负邻居、平地／斜面／鞍面、三组单位、填洼／重分类、Int64／UInt64大基值微差、全无效／无完整九点；13类各中英共26准确失败。独立2500位Decimal九点max−min包含中心，不取半差／去趋势，数值极限仅Native。39次调用／429次成果全检，13原COG归档，预期准备及清理计原600秒。

固定 `60da3691404ee74d3b6789d2948f575be1edef9c` 的[Hosted40700360070／122858214770](https://github.com/pampa0629/addp/actions/runs/40700360070/job/122858214770)完成上述矩阵、13次扫描／内外血缘与清零，业务215112毫秒。13原件已独立Decimal全像元／mask、519×515／米制／三级nearest及COG复核；源TIFF未归档，离线参考来自固定夹具。准备源码ae5的全量因范围外矢量断言失败仍记失败，不由Hosted追改，不计数值极限Hosted或当前HEAD全量。

### DEM 三维表面积统计独立 Hosted 验收

`raster-surface-area-persistence`；准备 `make test-raster-surface-area-online-runner`。12条CSV链覆盖平地、斜面、非共面双对角、跨块覆盖、三种水平／垂直单位、填洼／重分类、Int64／UInt64微差及最小2×2；18类双语共36失败。完整8列按Float64精确核，预览依double_precision=15另核；独立高精度向量叉积双对角平均，不补外缘／跨孔洞，严格计数、平方米／无量纲比值，数值极限仅Native。

成功累计78次、失败432次，48调用／510次CSV全检；12原CSV归档。期望为纯标准库周期计数＋异常面片修正，仍用独立叉积，宿主不新增NumPy／pandas／GDAL。物理＝Meta，不虚构RasterArtifact／第三方大小。准备、业务、收尾计600秒；本地专项／Runner已验证，本文未有已核Linux全量或实际Hosted结论。

### 正交旋转网格通视与可视域独立 Hosted 验收

`raster-sight-orthogonal-persistence`：来源准备User复用既有办理角色及精确13权限（原11＋扫描执行／读取），仅扫固定来源后给业务User精确限时metadata_read，不给业务User引擎管理权。来源扫描／物理Meta快照绑定准备者；16条业务链／自动扫描／血缘绑定原12权限User，两份清单分别核，所有退出撤精确Grant。

8组各一CSV通视＋一COG可视域：北向矩形、一般正交旋转、90°／180°、子像元／格角格边、NoData/mask/alpha与遮挡优先、国际／美制测量英尺跨块；显式第二波段，半径等号／邻接浮点及重合中心，以完整六项仿射＋独立逐格矩形相交核北向／旋转对照。

两算子对剪切／近似正交／双单轴镜像／地理／无CRS／无定位／退化各中英32拒绝，通视重合／半开边界外及可视域零半径／越界再8拒绝，共40准确业务失败。16成功重读136份＋40×16＝640，合计776全检，准备及56现场检查／清理计600秒。归档8CSV／8COG，CSV核列序／唯一行／距离三态及保存声明，COG核像元mask／仿射／CRS／nearest／布局；数值极限／注入仅Native。

派发必填 `sight_orthogonal_source_commit` 为40位小写SHA；当前main供编排，checkout等于所选业务源且为编排祖先，分别记两SHA，不省略回退main，不影响他suite。

| 固定运行 | 首败、修复及未覆盖 |
| --- | --- |
| 业务 `d240f16bc`／编排 `f21728b33`：[42448726028／128633145625](https://github.com/pampa0629/addp/actions/runs/42448726028/job/128633145625) | 检出／祖先及7服务预检通过，真实main缺os导入NameError；无套件报告／8CSV8COG清单，16/40/776未验。wrapper清理／Infra清零不补精确Grant撤销报告。 |
| 修复复验 `aeabceb3d`：[42469659281／128697238003](https://github.com/pampa0629/addp/actions/runs/42469659281/job/128697238003) | 原业务User集中扫源因无引擎管理范围403，矩阵／原件未执行归档。已改上述双身份夹具并经本地场景／隔离身份T1，不计实际通过；新源码Hosted须另确认，两个失败不追改、不扩600秒。 |

### DEM 单观察点可视域独立 Hosted 验收

`raster-viewshed-persistence`：16条COG链覆盖平地／持续贴地、山脊、沿途NoData／遮挡优先、mask／透明部分alpha、半径等号邻接浮点、重合／子像元观察点、半径内无目标、国际／美制英尺、填洼接链及519×515矩形像元跨块；18类双语共36准确拒绝，数值极限／异构复数／注入仅Native。

独立精确矩形裁剪预期，Byte三态0/1/2、NoData255及mask、六项仿射／CRS／nearest／COG精确核。成功136＋失败576＝712次原件全检，预检＋52现场共53物理调用，归档16COG及大小／SHA清单；期望／准备／收尾计600秒。

源初始化器只授本轮精确NFS文件限时metadata_read并全部退出撤回；填洼链仍核原DEM的 `raster_save` 输入。持久大小／内容只在隔离Hosted用绑定当轮User／扫描／执行／目标的有界只读SQL取证，不改公开投影／权限。Linux全量及Hosted待协调，准备不计通过。

### DEM 两点通视独立 Hosted 验收

`raster-line-of-sight-persistence`：16条CSV链覆盖平地／山脊、严格贴地及邻接高程、端点触地／持续贴地、格角仅触及实际穿入、横纵格边、NoData／mask／alpha和遮挡优先、国际／美制英尺、矩形第二波段跨块。18类双语共36准确通视失败，数值极限／注入仅Native。

独立逐格相交冻结期望，136＋576＝712次CSV原字节全检，核唯一行／列序／精确Float64米制距离三态，预览按原序列化另核；归档16CSV及清单，保存目标／类型／写方式与物理＝Meta大小准确。唯一实际DEM到CSV核内部 `save`、公开投影／Meta／Monitor，observer仅扩本套件白名单。Linux全量及Hosted待协调，不由本地准备补通过。

### 地形湿度指数 TWI 独立 Hosted 验收

`raster-twi-persistence`；准备 `make test-raster-twi-online-runner`。14条链覆盖degree/percent等价、两英尺网格、极小非零坡度、重分类、DEM两坡度单位、填洼（平地后全NoData）、Int64／UInt64面积、全无效／不相交／零坡度。519×515方格、第二波段跨512块、各源独立mask/NoData/alpha；负指数／合法零／NaN分别核。

26类双语共52个TWI节点失败：两角色波段、面积单位、坡度声明冲突、零负面积／负坡度90度、九类网格、alpha/复数及scale/offset；非法值在另一源无效区仍拒绝。独立200位Decimal Lambert连分式＋ln，固定A/d、米制／单位1，数值极限／异构复数alpha仅Native。14成功105＋52×14＝728，66调用／833全检，归档14COG；预期准备至清理计600秒。

固定 `128a5330782ec1c0033be2ce14b076392e90f012` 的[Hosted40818720334／123265876421](https://github.com/pampa0629/addp/actions/runs/40818720334/job/123265876421)完成14／52／833、三级nearest、大小／内外血缘／扫描及清零，业务421186毫秒；14COG及血缘原件清单与现场回执匹配。Linux全量仍待回传，隔离身份Go T1不算完整IAM／DB，不计当前HEAD或关闭通用GDAL中止缺陷。

### 水流功率指数 SPI 独立 Hosted 验收

`raster-spi-persistence`；准备 `make test-raster-spi-online-runner`。14条链与TWI相同的单位／源组合，**原始(A/d)×tanβ、单位metre，不取对数**；零坡度／填平洼地有效内部为0，原无效及坡度边缘NoData。519×515第二波段跨512块、独立mask/NoData/alpha／部分透明；数值极限／异构复数仅Native。

26类双语共52个SPI节点失败，包括两角色波段／面积单位／坡度声明、零负面积／负坡度90度、尺寸原点CRS及非方形／旋转／南向／地理／无CRS网格、alpha/复数、scale/offset，另一源无效不掩盖非法值。独立200位Decimal Lambert连分式＋原始乘积；误乘边长、面积计数、取ln、负值、零坡度作NoData、小坡度阈值及percent漏除100均须拒绝。

14成功105＋失败728＝833次全检／66调用，归档14COG，期望首次构建／业务清理计600秒，产品构建／部署／源播种／拆除另记。本地专项及SPI/TWI Runner／登记通过；较早栅格聚合绑定旧快照主动中断，不计全入口通过，最终排序修正按最终专项认定。Linux全量／Hosted待协调，不由本地通过推定平台通过。

### 分类栅格小斑块筛除独立 Hosted 验收

`raster-sieve-persistence`；准备 `make test-raster-sieve-online-runner`。6条COG链：旋转多波段4邻接／8邻接、重分类、min_pixels=1、无供体无定位、全无效。519×515第二波段跨512块，合法零／负／安全大整数、mask/NoData/alpha／非有限／部分透明；独立BFS＋原始邻界搜索验证严格小于阈值才替换、等值保留、原始最大直接供体／并列规则、冻结不传播不迭代、无供体保留，不调用产品游程／GDAL Sieve。

14类双语共28个sieve节点失败：波段、零负小数布尔阈值、非法值小数布尔邻接、小数分类、正越界、Int64负及UInt64越界邻接精确NoData、alpha／复数；异构复数alpha、不可观测同分类供体行列并列及资源极限仅Native。6成功21＋28×6＝168，34调用／189全检，归档6COG；源期望准备及业务清理计600秒。

无定位成果只核Meta的cog/true/heuristic有界提示且无spatial，不造CRS／网格；实际COG须GDAL完整布局／三级nearest。血缘只认当前input.source。Linux／Hosted与本地准备分开，本文无已核实际Hosted结论。

### 算子参数错误独立普通 User 实页

`vector-topology-persistence` 的 `operator-input-diagnostics` 只跑4个预期失败，不跑原成功／地图／容量；普通User精确11权限。多距缓冲属性distance重名、distances=[true]、distance两已知投影CRS不一致、近接零容差，各冻结DAG均带save；至少一份失败节点在tasks首位、depends_on使load先行，节点序号必须来自冻结数组。

Develop／Monitor核 `develop.workflow.invalid_params`、唯一v3 execution阶段与闭合reason/operator，不读错误正文猜原因。同四UUID在两实页经正常菜单切中英并刷新，共16组核原因／算子／节点／可选字段参数／建议／UUID／真实剪贴板及同User/Tenant/11权限，禁mock／绕Worker。仅诊断区与复制内容按白名单，禁原生异常／自定义节点名／源值／私路径，合法locator不当整份响应泄漏。

四失败无成果／facts／自动扫描／当前derive，初始源扫描分开登记；归档4定义、公开响应、源字节／Meta、各语言截图与剪贴板，最终报告与incomplete分开。`distance_to` 活动几何名distance特殊冲突因正式load归一化未覆盖，保留Native/T3，不注内存／改load／造DB；不外推全部11组合或成功计算。

### 近接检查独立双源 Point 成果

`vector-topology-persistence` 的 `line-proximity`，一条双源load→check_line_proximity→save；EPSG:3857的5源线／9参考、容差1米，`proximity-points.gpkg` 为7Point／3悬点XY。业务Int64为JSON安全2^40级、参考String，均非FID，不复制note；手写期望核全部候选、异参考ID同几何、同参考多部件去重／等距XY决胜、任一精确接触全排除、严格边界1不报、2^-20微距、共享悬点分别保留，按原源／参考行序不按ID距离重排。

独立OGR核两源一成果坐标／CRS／字段序类型：source_id/reference_id为Int64/String，distance/nearest_x/nearest_y为Float64，Point为原悬点。一个create／一次同主体自动扫描，两个显式端口locator及两条当前active derive，公开tenant_id／triggered_by核严格整数且同User/Tenant；父文件物理＝Meta、唯一子层7行不重复计大小。初扫另记、不补扫collect。

真实Console成功／属性／Point／血缘四截图；API EWKB及全部OL七Point属性／精确投影坐标一致，保留同位置记录，不以点簇数代要素数。读取可见矢量层实际填色，>30非透明像素、禁面线绿色假设，空／隐藏／颜色错失败；两源一成果及Meta／响应／渲染归档。空结果／英尺／超JSON整数／容量／内存中间血缘未覆盖。

固定 `ffa8aed3de8d3c84e5659c4575b30f6f7e8323e4` 的[Hosted40995207798／123866626980](https://github.com/pampa0629/addp/actions/runs/40995207798/job/123866626980)完成上述矩阵：User33/Tenant2、7Point、98304字节、1扫描／2派生、174填色像素及真实G6三节点两边，业务42859ms；最终Owner/browser v1 passed，incomplete只记阶段，原件／Meta不变及隔离清零通过，不认证业务DELETE。

### 线端延伸独立双源双成果

`vector-topology-persistence` 的 `line-extension`：一成功双源load→extend_line_endpoints→result/details各save，一不可精确表示首次交点失败，不跑full／旧业务／容量。3857的7条MultiLineString／8部件、14参考，max_distance=2，期望7结果／8新增LineString。核首尾重复端点、等号／越界、共线入口、首次而非侧向最近／后方、同点全来源、接触不动、多部件、2^-20、JSON安全非FID业务ID与nullable整型布尔字符串。第三独立参考只造x=4/3首次交点，禁舍入／容差，期望不调产品。

三源两成果归档、独立OGR核坐标原顶点／方向序／字段CRS。两create、2扫描，公开tenant_id／triggered_by严格整数且同主体；每成果2源派生共4边；失败参考不混成功血缘，初始3扫描另记。失败同UUID Develop/Monitor及owner准确原因，双目标／facts／derive／新扫均无。浏览器核成功失败页、EWKB／FID／全部OL属性顶点投影、两线图／属性／G6边标签；须真实新增长段像素，微距仅证物理／API／OL，不称亚像素独立可见。最终Owner/browser/raw同case/run/execution且passed，源／成果／Meta不变及清零。

固定 `46300b7efd9055fe9a40039208a5cf6f9b05017d` 的[Hosted41032821129／123986411278](https://github.com/pampa0629/addp/actions/runs/41032821129/job/123986411278)通过：三源7/14/1行、双成果7/8行、五原GPKG各98304字节、2扫描／4派生、两实际线图新增段及八截图，业务51944ms，准确失败与清零均核。原41029102072首图后Canvas观察器初始化遗漏失败／清零仍保留，`1b6a16f5a` 修复红绿后仅此复验；不认证业务DELETE、空批／英尺／超JS整数／容量／内存中间血缘。

### 端点吸附独立线与Point双成果

`vector-topology-persistence` 的 `endpoint-snapping`：一成功／四失败，3857、容差1，load→snap_line_endpoints→result/details各save。12条MultiLineString／13部件、14参考，手写期望12线／10原悬点Point。核严格边界及略超不动、精确接触／闭环／无候选、首尾／多部件／重复端点、共享悬点全部端部、同点全参考／单参考重复段去重、最近不连锁及2^-20；JSON安全Int64非FID，nullable Int64/Boolean/String留在线，明细保原点／双方ID／部件首尾移动事实，按原行／部件／首尾／参考序，无epsilon或舍入。

一失败源＋四参考触发等距异点、内部覆盖非端部处理、斜线最近点不可精确落线、正长度塌缩；每DAG双save，核同User/UUID失败及owner原因，双目标／facts／派生／新扫均无。沿EXECUTION_FAILED，不外推结构化参数或双文件事务；成功双create／2扫描／4当前派生，公开tenant_id／triggered_by严格整数且同主体，五失败输入不入成功图。

七源＋双成果归档，独立OGR核全坐标／字段空值／部件方向序／CRS和父文件大小，唯一子层不混计；真实Console成功四失败、线／Point地图、属性／G6边标签共11截图，EWKB／全部OL及端点移动／原Point像素核，目标用字段与线对照，微距不称亚像素独立可见。完整报告与incomplete、隔离清零与业务DELETE分别认定。

固定 `182f93649cd1227ef6ff8451fca0902373fbde1b` 的[Hosted41054235377／124057611152](https://github.com/pampa0629/addp/actions/runs/41054235377/job/124057611152)完成上述矩阵：九原GPKG各98304字节、12线/10点、扫描7→9后不变、2扫描／4派生、移动14／Point451像素及11截图，业务69915ms，准确四失败、原件／Meta不变和清零通过；同SHA Console／Platform通过。GeoPython全量9994通过但1个未改TIFF overview负例失败，仍不计全量通过；无容量／跨文件事务／中间血缘／业务DELETE／空批／英尺／超JS整数认证。

### 分类小斑块筛除平方米面积阈值独立 Hosted 验收

`raster-sieve-area-persistence`；准备 `make test-raster-sieve-area-online-runner`。16条COG：米制4/8邻接＋重分类3；两英尺换算／提前舍入反例2；阈值等号邻接浮点3；等价像元数1；省略／空保护2；1000平方米下10米／30米分辨率2；Int64／UInt64原生NoData2；全无效1。519×515第二波段、北向非方形跨512块，合法零负／mask/alpha、不用scale/offset。三基本链核保护小斑块不替换、达标供体可供给、未达标不供给／桥接，原始大小并列冻结不传播；临界斑块直接邻达标供体、供体恰等阈值也核，防无供体保留掩盖错误。

32类双语64准确sieve失败：阈值缺／双传／像元null+面积／反向4；面积null/布尔/零负/串/数组/对象7；地理/无CRS/无定位/双旋转/未知单位/复合CRS7；波段0/alpha/复数及非法小数布尔邻接6；小数/不安全Float64/原生Int64UInt64紧邻NoData越界4；保护null/[{}]/重复/越界4。独立2000位Decimal面积乘积＋BFS原边界，不调产品连通／供体／GDAL Sieve；单位重读原文件，不能用写入前系数。

像元mask精确无容差，16成功136＋64×16＝1024，共1160全检；含源基线81轮／80执行，原600秒含身份源／首次期望／查询／归档与缓存清理，部署／外退另记。归档全部源TIFF、16COG及16冻结NPY和SHA清单；源全波段／原生NoData／类型／单位编码／Meta及旧成果不变，内部raster_save/derive与公开operations:null分核，observer仅本suite。

NaN/Infinity、保护[1.0]、混合复数alpha、退化／溢出下溢／I/O注入仅Native。GDAL矩阵／语义污染／归档拒绝及无仓库依赖stdin worker沿原Native，Online/changed/workflow/删断言拒绝同登记；原像元／保护suite不代面积范围，本地准备不算Hosted／UI／容量／全量IAM。

### 分类小斑块筛除指定类别保护独立 Hosted 验收

`raster-sieve-protected-persistence`；准备 `make test-raster-sieve-protected-online-runner`。JSON重编码会规范[1.0]，其严格拒绝只记Native，Hosted改测[{}]；浮点邻接用真正非整数4.5，不改平台数字传递／产品准入。

16COG：4/8直接保护、重分类、零负保护／供体资格5；省略/空/未命中/正逆序5；全保护/min_pixels=1/无定位无供体/全无效/Int64UInt64安全类6。519×515旋转多波段、第二波段跨512、NoData/mask/alpha及部分透明，不用scale/offset。独立BFS＋原边界核保护小斑块不改、达标供体可供给／未达标不传播、原大小／并列／等阈值／冻结单轮／无供体；省略空未命中合原oracle、顺序两份相等、全保护／阈值1保留有效原像元。

原14类波段／阈值／邻接／分类／alpha复数拒绝，加12类保护null、列表外布尔/整数/串/对象、列表内布尔/对象/小数、正负越界、重复／嵌套，共26类双语52准确失败；不可序列化／极端组合／深嵌套／I/O资源注入仅Native。精确无容差，不调产品游程／并查集／供体／GDAL Sieve；68执行，16成功136＋52×16＝832，共968成果全检，另源基线共69物理轮，基线不计成果数。

原600秒含身份源／首次期望／全部查询全检归档／执行和缓存清理，部署／播种／退出另核。全部源TIFF、16COG、16冻结NPY及大小SHA清单归档；源全波段／类型／原生NoData／编码／Meta与旧成果不变，COG实际nearest及无定位分核，内部raster_save/derive与公开operations:null独立，不扩只读observer。真实GDAL／52失败／语义污染／缓存归档破坏拒绝及stdin worker独立性沿原Native，Online/changed/workflow/删断言拒绝同登记；原Sieve6/28/189不代16/52/968，准备不计真实Hosted。

## 整幅栅格百分位统计独立 Hosted 验收

`raster-statistics-percentile-workflow`为独立手工Hosted只读套件，复用NFS、同源产品默认入口Runtime及普通User原12权限，业务预算固定600秒；不改原Raster/OIF/邻域套件，不计UI、容量或任意影像认证。实现与本地夹具通过不计实际Hosted成功，实际Linux／Hosted仍由CI会话协调。

该既有矩阵已随产品统一接口改为percentiles单元素列表及嵌套结果核验；不新增执行数或预算，不作为多项同次请求的Hosted证据。固定16条成功链：原统计不带百分位1条，0／100端点2条，奇偶总体50百分位2条，25／75插值2条，33.3跨六块及像元逆序2条，重分类接90百分位1条，Int64／UInt64 NoData邻接值2条，显式第二波段的mask／alpha／scale-offset覆盖1条，单有效／全无效2条，缺失定位1条；跨块源保留旋转网格。固定12类非法参数各按中英文提交，共24次准确失败：列表null／布尔项／字符串项／嵌套数组项／对象项／低于0项／高于100项，波段0／越界／布尔／字符串及所选复数数据。新增百分位原因按提交语言准确核验，既有波段／复数原因按当前英文契约核验，不将两种提交冒充全部诊断已翻译；反向查询不能改写冻结失败。NaN／Infinity、复数alpha混合类型、极端插值／原统计矩范围、IO注入及真实低盘边界分别沿Native或既有独立套件记账。

独立oracle按源原生有效性取完整总体，使用Python标量排序与Fraction的确切二进制百分位位置；不调用产品函数或NumPy百分位。原统计全字段、计数／null／合法零、端点及中位数严格核对，其他插值固定rtol=4e-15／atol=0，原mean／stddev采用rtol=1e-10／atol=1e-12；同源带／不带百分位的原字段逐值相等。oracle首次计算计入预算，只缓存独立期望，实际源文件、像元与mask每次重新全读。

源初始化扫描须核当前执行、无父执行、同租户／普通User／引擎；随后40条业务执行全部零资源发布、零新增扫描和零派生血缘，专业详情与Monitor核当前身份、状态和提交DAG／语言快照。共41次完整物理检查，每次覆盖11份源原件／全像元／mask／类型／NoData／网格／CRS／scale-offset及Runtime执行工作区零残留；每步重新读取此前成功结果，共504次先前结果检查。归档11份源原字节、独立期望、80份专业／Monitor终态原始响应及大小／hash清单（响应文件名相对于`statistics-responses/`目录），所有旧原件／响应／期望禁止覆盖、重建或静默修复。业务计时覆盖源初始化、oracle、40执行、查询、原件复核和执行工作区清理；Hosted部署／NFS／Infra退出清理由既有生命周期单独严格核验，清理失败不得通过。

本地入口为`make test-raster-statistics-percentile-online-runner`及标准`make test-geopython-workflow`中的`test_online_statistics_percentile_fixture.py`；已同步Online registry、changed gate、独立workflow与固定矩阵／删除断言变异检查，仅登记不代表已运行Hosted。


## 整幅栅格多百分位独立 Hosted 验收

`raster-statistics-percentiles-workflow`是独立手工只读套件，复用NFS、同源产品默认入口Runtime及普通User原12权限，业务预算固定600秒；保留原单元素套件与产品算法、权限和原统计。实际Linux全量及Hosted由CI会话协调，不触发、重跑或扩预算，不计页面或容量。

固定16成功：原统计不请求列表、同源单元素P50对照各1条；端点／常用分位／小数分位／源像元逆序4条；奇数／偶数总体、常量重分类／原两类重分类4条；原生Int64／UInt64 NoData邻接2条；显式第二波段覆盖、单有效、全无效及缺定位4条。共14条多项请求及2条原字段／单元素对照。完整结果逐项用独立标量排序及Fraction确切二进制位置核验，跨六块含旋转源；P50真实对照、像元逆序及不同百分位合法同值不得被合并。全面逐项与单元素计算的一致性仍由Native覆盖，不虚称Hosted逐项重跑。

固定20类各中英文提交，共40准确失败：列表null／布尔／数值／字符串／对象／空6类，布尔项／字符串项／null项／嵌套列表项／对象项／越下界／越上界7类，重复／降序2类，波段0／越界／布尔／字符串及所选复数5类；须准确到analysis任务，前置失败不能充数。新增列表诊断核提交中英语言；既有波段／复数按原诊断契约精确核验，双语提交不等于全部诊断已翻译。反向查询不改旧失败。非有限JSON、旧单值参数、混合类型复数alpha、数值极限及IO／磁盘注入仍单列Native。

56执行全部零发布、零新增扫描、零派生血缘；初始扫描精确核目标／无父执行／普通用户／租户／引擎，当前DAG／语言快照／Monitor严格核对。共57次完整物理复核、760次先前成功结果复核；每步核源及归档不变，最终核全部源Meta及血缘快照。归档11份源TIFF原字节、112份专业详情／Monitor原始终态响应、独立期望及大小／hash清单，不虚构成果声明大小。物理复核每次重读所有像元／mask／网格／CRS／类型／NoData／scale-offset，不缓存实际结果；只缓存独立期望且首次生成计入预算。源夹具与新矩阵打包为只读stdin Worker，依赖源码hash冻结，源码或原件漂移均失败。所有执行工作区及NFS／容器／镜像／部署／Infra退出清理不弱化；超时或清理失败整套失败。

本地标准入口：`make test-raster-statistics-percentiles-online-runner`；Native夹具加入既有`make test-geopython-workflow`；Online、影响选择、手工workflow和严格矩阵登记同次接入。准备与本地通过不记实际Hosted通过。

## 栅格分区加权总体标准差独立 Hosted 验收

`raster-zonal-weighted-stddev-persistence`；`make test-raster-zonal-weighted-stddev-online-runner`。独立运行、600 秒预算、成果／身份核验及退出清理统一按[专项共同要求](#水文与分区统计专项共同要求)执行。

固定16条CSV成功链：非均匀权重、整体×8、全1三条；默认关闭／显式False的原加权对照及原未加权对照三条；权重／分区重分类两条；全零、混合特殊组、数值全无效、分区全无效四条；显式第二波段／raw scale-offset／mask／部分alpha及非必需域坏权重、同旋转网格、Int64／UInt64原生NoData邻接值四条。13份13列启用表、2份12列原加权表及1份9列原统计表。混合组覆盖单正权重、正权重对应常量、零权重高差值仍计入原统计但不计新标准差、无有效数值与全零权重；主场景79分区完整表，预览仍最多50行。

固定49类各中英文提交，共98次准确失败：原加权40类成对参数、必需域权重、三角色波段／alpha／复数、定位／网格与精确分类准入；新增开关null、0、1、0.5、字符串true／false、列表、对象八类，以及启用却无权重一类。核当前execution、analysis节点、真实准确原因、专业错误详情／Monitor和冻结提交语言，反向查询不改旧失败。提交、加载、其他节点或错误阶段不得充数；原英文诊断不冒称已新增翻译，非标准JSON、混合复数alpha及数值极限仍单列Native。

独立高精度Decimal逐组偏差公式核总体分母及完整σ，不导入产品统计实现、不使用已舍入weighted_mean；原统计按原精度另核并与同源真实对照逐字段相等，全1的加权sum／mean／stddev必须精确等于原字段，不以理想算术值替换原IEEE归约。权重×8保持均值及标准差，前两个和同比变化。全零／无有效数值新标准差null，单正权重或常量0；关闭开关不加新列。CSV原十进制文本直接解析精确核验，公开15位JSON预览单列校验。

每次成功后重读当前1至16份成果，98次失败后各重读16份，共136＋98×16＝1704次CSV全表检查；加首次源全检共115轮物理检查。每轮真实重读所有源像元／原生NoData／mask／alpha／类型／网格／CRS／scale-offset和hash，不缓存实际成果，仅冻结独立期望。归档16份CSV及全部源原字节、大小／SHA256清单、当前执行血缘和原始证据；篡改、缺失、额外文件及符号链接均拒绝，不重建或静默修复。

启用及关闭开关的加权链核值／分区／权重三份实际源，原统计对照只核两份。受控只读observer核完整内部save derive，公开operations:null、Meta关系及Monitor分别校验。标准入口`make test-raster-zonal-weighted-stddev-online-runner`与标准Native显式登记`test_online_zonal_weighted_stddev_fixture.py`，Online／changed gate／独立手工workflow／observer和固定矩阵删断言检查同次覆盖。本节已有本地 Native 专项、独立 Runner 与登记通过记录，筛除不计全量，准备结果不证明 Linux 全量或真实 Hosted。

## 栅格分区加权统计独立 Hosted 验收

`raster-zonal-weighted-persistence`；`make test-raster-zonal-weighted-online-runner`。独立运行、600 秒预算、成果／身份核验及退出清理统一按[专项共同要求](#水文与分区统计专项共同要求)执行。

固定16条CSV成功链：非均匀跨二维读取块及权重×8两条；全1加权和两条原九列实际对照三条；权重／分区重分类两条；全零权重、混合空组、数值全无效、分区全无效四条；显式第二波段／raw scale-offset／mask／部分alpha与非必需域坏权重一条；三源同旋转网格一条；单有效值／合法零负值一条；Int64／UInt64原生NoData邻接数值与权重、精确大分区编号两条。14份12列加权表、2份9列对照；79分区完整CSV，公开预览最多50行。

固定40类各中英文提交，共80次准确失败：权重成对参数／显式null五类及与percentile冲突一类；必需位置负权重／NoData／无效mask／透明alpha／NaN／Infinity六类；值／分区／权重三个角色各波段0／越界／布尔／字符串／alpha／复数数据共18类；分区偏移和权重尺寸／CRS／偏移／旋转／缺定位／奇异网格七类；小数分区／不安全浮点整数／超Int64的UInt64分区三类。核当前执行、`analysis`节点、真实准确原因、专业错误详情、Monitor及冻结提交语言，反向查询不得改写。沿用原诊断的英文原因不冒称已新增翻译。加载、JSON、其他节点或错误阶段不能充数；非标准JSON、混合复数alpha和数值极限另列Native。

逐像元独立Decimal（2000位）计算权重总和／加权和／均值，最终Float64精确比较；不导入产品统计实现。原九列沿既有容差／精确分类计数规则，和同源实际不加权对照逐值相等；全1结果等于原sum／mean，整体×8使两个和×8且均值不变。有有效值但全零权重输出0／0／null，没有有效值的分区三个新增字段全null；分区全无效保留带表头空CSV。完整原CSV不舍入，公开JSON预览的15位投影单列校验。

成功后依次全读1至16份，80次失败后各全读16份，共1416次CSV全表检查；加首次源全检共97轮物理检查。每轮真实重读所有源像元／mask／类型／原生NoData／仿射／CRS／scale-offset及hash，不缓存实际成果。仅冻结独立期望；归档源原字节、16份CSV原字节及大小／SHA256清单和当前执行血缘。原件／manifest／期望变化、缺失、额外文件、符号链接一律失败，不重建或静默修复。

加权链值／分区／权重三份实际输入，原统计对照仅两份。受控只读observer保留完整内部`save` derive与公开`operations:null`分别校验，Meta关系／Monitor独立核实。

标准入口`make test-raster-zonal-weighted-online-runner`与标准Native中的`test_online_zonal_weighted_fixture.py`，同次登记Online、changed gate、手工workflow、observer及固定矩阵／删断言拒绝测试；本节已有本地 Native 专项、独立 Runner 与登记通过记录，筛除不计全量，准备结果不证明 Linux 全量或真实 Hosted。


## 栅格分区百分位独立 Hosted 验收

栅格分区百分位参数已统一为非空严格递增`percentiles`列表，原单值参数及原两列删除，旧DAG显式修改；原九列保持、每区一行，按请求顺序从1追加percentile_i／percentile_value_i列对。原单项套件仅迁移单元素列表与列名、保持16成功／56双语失败／1032次全检和600秒预算，权重及分类频数互斥场景同步迁移，严格断言不减。历史固定版本证据保留；多项独立Hosted范围另议，实际Linux全量／Hosted仍由CI会话协调，不重试、不扩预算。

`raster-zonal-percentile-persistence`；`make test-raster-zonal-percentile-online-runner`。独立运行、600 秒预算、成果／身份核验及退出清理统一按[专项共同要求](#水文与分区统计专项共同要求)执行。

固定16条成功CSV保存链：原九列统计1条；0／100端点、奇偶总体50百分位4条；25／75插值及33.3跨六块／逆序4条；分区重分类1条；79分区完整表／50行预览1条；精确Int64大分区号配Int64／UInt64数值NoData邻接有效值2条；显式第二波段独立mask／alpha及scale-offset1条；无有效数值保留分区、分区全无效空表2条。共同包含负分类、合法零、同分类非连通合组及单有效零值，主网格保留旋转。

固定28类各按中英文提交，共56次准确失败：列表内null／布尔／字符串／嵌套列表／对象／低于0／高于100共7类；两个角色的波段0／越界／布尔／字符串共8类；两个角色的所选复数数据／alpha共4类；小数分类、UInt64超Int64共2类；缺CRS、不同CRS、缺仿射、奇异网格、不同尺寸、原点偏移和旋转不一致共7类。必须核当前执行UUID、`analysis`任务、准确原因、专业详情与Monitor失败及冻结提交语言；反向查询不改错误和快照。新增百分位原因按中英核验，原有英文原因沿当前契约，不把两种提交称为全部诊断已翻译。非有限JSON参数、混合类型复数alpha、数值极限、IO注入及真实低盘另列Native或既有专项。

独立原生有效性与Python标量分组／排序、标准库统计及Fraction位置核完整CSV每行每列、精确Int64编号／计数、顺序、null、合法零和固定Schema；端点／50百分位及min/max精确，其他百分位rtol=4e-15／atol=0，原统计其余值rtol=1e-12／atol=1e-12。同源带／不带百分位的原九列逐值相等，逆序百分位逐组相同。完整CSV不舍入；轻量表预览沿既有`double_precision=15`序列化，大数min/max独立核15位展示值，不能据预览外推原字节精度。

每次成功后重读当前全部CSV、每次失败后重读16份，固定1032次完整CSV检查；另含首次源全检，共73轮物理检查。仅缓存冻结的独立期望，不缓存实际成果；各轮完整重读所有源像元／mask／类型／原生NoData／网格／CRS／scale-offset和字节hash。归档源原字节、16份CSV原字节及大小／SHA256清单、独立期望和16份当前执行血缘取证；旧原件／manifest／期望变化、缺失或额外文件均拒绝，不覆盖、重建或静默修复。

CSV沿普通表保存契约，不虚构RasterArtifact：物理／Meta大小须为相等正整数；目标只由本次父执行自动扫描，核用户／租户／引擎／父执行与当前两角色实际输入血缘。套件仅加入既有执行范围的受控只读内部血缘登记，完整验证内部`save`／两源derive与公开`operations:null`安全投影，不改产品投影或权限；Meta关系及Monitor分别核验。失败零目标／新增扫描／派生血缘，源物理／Meta和先前CSV不变，全部退出保持NFS／Runtime／registry／部署／Infra零残留。

本地入口`make test-raster-zonal-percentile-online-runner`和标准Native内`test_online_zonal_percentile_fixture.py`，同步Online、changed gate、独立workflow、只读observer和矩阵／删断言拒绝检查。准备通过不计Hosted通过。


## 矢量面分区加权总体标准差独立 Hosted 验收

`vector-zonal-weighted-stddev-persistence`；`make test-vector-zonal-weighted-stddev-online-runner`。独立运行、600 秒预算、成果／身份核验及退出清理统一按[专项共同要求](#水文与分区统计专项共同要求)执行。

固定16条GeoPackage成功链：非均匀中心／全部触及、权重×8、全1四条；默认关闭／显式False的原加权对照及原未加权对照三条；权重重分类接自定义前缀、数值重分类、重分类→转面→统计三条；空前缀、全零、数值全无效、带声明字段空面批四条；Int64／UInt64原生NoData邻接值两条。13份追加新标准差、2份原加权字段、1份原八统计。显式False使用sd_前缀保留已有sd_weighted_stddev属性，不能误拒绝或覆盖。主夹具固定旋转网格并跨二维512块，覆盖第二波段／raw scale-offset／mask／alpha、孔洞、多部件、重叠、重复几何／属性、薄面、部分／完全出界；新增小面核单正权重、常量、零权重高差值仍参与原统计但不计新σ。

固定53类各中英文提交，共106次准确失败：原加权43类准入，再加开关null、0、1、0.5、字符串true／false、列表、对象八类，以及启用无权重、新字段与原属性冲突各一类。必需域坏权重取值为合法零的位置也必须拒绝。逐次核当前execution／analysis节点／真实准确原因、专业错误详情／Monitor及冻结提交语言，反向查询不改旧失败；加载、JSON或其他节点提前失败不能充数。活动几何列冲突、重复内存索引、混合复数alpha、非标准JSON及数值极限仍单列Native，不修改产品格式支持或借提前失败替代目标拒绝。

独立像元中心／格网方格与面关系构造覆盖，不调用产品栅格化／统计；孔洞排除、重叠独立，不按面积折算。2000位Decimal用逐组偏差公式核总体σ、不使用舍入weighted_mean；相同Float64数值／权重按出现次数精确合并后计算独立偏差，仍覆盖全部入选像元。全1精确对照该面原sum／mean／stddev和同源实际原统计，原八项按原精度另核，默认／False与启用表原11字段逐值相等；不同前缀按字段语义对应，原属性／几何／行序保持。权重×8使前两个和×8、均值／σ保持；全零或无覆盖／无值σ为null，单正权重／常量0。

每次成功重读当前1至16份成果，106次失败各重读16份，固定136＋106×16＝1832次GeoPackage全检；加首次源核共123轮物理检查。每轮真实核全部源像元／mask／原生NoData／类型／完整网格／CRS／scale-offset、矢量几何／属性及hash，核全部成果几何／属性／字段名序型／统计／null／行序／CRS，不缓存实际成果，仅冻结独立期望。归档16份GeoPackage及全部源原字节、大小／SHA256清单、原始执行和血缘；篡改、缺失、额外文件、符号链接不重建、不覆盖、不静默修复。

直接启用及关闭的加权链核三源，转面链核实际两源，原未加权对照核两源；完整内部save derive、公开operations:null、Meta关系和Monitor分别验证。根Makefile标准Native、独立Runner、Online／changed gate／独立手工workflow／受控observer／固定矩阵删断言检查同次登记。本节已有本地 Native 专项、独立 Runner 与登记通过记录，筛除不计全量，准备结果不证明 Linux 全量或真实 Hosted。

## 矢量面分区加权统计独立 Hosted 验收

`vector-zonal-weighted-persistence`；`make test-vector-zonal-weighted-online-runner`。独立运行、600 秒预算、成果／身份核验及退出清理统一按[专项共同要求](#水文与分区统计专项共同要求)执行。

固定16条成功GeoPackage保存链：非均匀权重中心覆盖／全部触及、权重×8、全1四条；对应中心／全部触及的原统计实际对照两条；权重重分类、数值重分类、重分类→转面→加权三条；自定义／空前缀两条；全零权重、数值全无效、带声明字段的空面批三条；Int64／UInt64原生NoData邻接数值及权重两条。14份加权成果追加原前缀三字段，2份原统计对照保持原八字段。共同覆盖完整旋转网格、跨二维读取块、显式第二波段、raw scale-offset、mask／部分alpha，以及孔洞、多部件、重叠、重复几何／属性、薄面、部分／完全出界、合法零和空组；重复内存索引单列Native，不冒充持久文件身份。

固定43类各按中英文提交，共86次准确失败：权重成对缺失／显式null及percentile冲突6类；必需位置负权重／NoData／无效mask／透明alpha／NaN／Infinity6类；数值／权重各波段0／越界／布尔／字符串／alpha／复数数据12类；权重尺寸／CRS／偏移／旋转／缺CRS／缺仿射／奇异网格7类；矢量CRS不一致及数值缺CRS／缺仿射／奇异网格4类；非法覆盖参数／前缀4类；原统计字段和三个新增字段属性冲突4类。逐次核当前执行、analysis节点、准确真实原因、专业详情／Monitor与冻结提交语言，反向查询不改错误；加载、JSON或其他节点提前失败不能充数。沿用原有英文诊断不冒称新增翻译；数值极限、混合复数alpha、特殊JSON和内存索引保持另列Native。

覆盖期望用独立像元中心／像元方格与面的关系计算，不调用产品栅格化或统计实现；重叠逐面独立，孔洞不贡献，不按交叠面积折算。只有面覆盖且数值有效的位置要求有效有限非负权重，面外或数值无效位置坏权重不误拒绝；合法数值零仍需权重。有有效值但全零权重0／0／null，无覆盖或无值三个null；原行／属性／几何／CRS／行序及空批声明Schema保持。独立2000位Decimal逐像元累计三个字段并核最终Float64精确值；全1按已确认契约精确复用原sum／mean，二者独立核原统计并与同源实际不加权成果逐值相等，不用理想算术均值覆盖原归约的位级结果。原八字段沿原精度与精确计数规则，全部权重×8使两个和×8、均值不变。

每次成功后真实重读当前1至16份成果，每次失败后重读16份，固定136＋86×16＝1512次GeoPackage全检；加首次源检查共103轮物理检查。每轮核完整几何／属性／行序／字段类型、计数／统计／null，所有源原生像元／mask／NoData／仿射／CRS／scale-offset及矢量完整内容和hash；只冻结独立期望，不缓存实际成果。归档16份GeoPackage及全部源原字节、大小／SHA256清单、当前执行血缘和原始执行证据；篡改、缺失、额外文件或符号链接均拒绝，不覆盖、重建或静默修复。

直接加权三源，转面链仅数值和权重两份实际源，两个不加权对照仅面和数值。仅登记执行范围受控只读observer，完整内部save／derive和公开operations:null分别核验，Meta关系／Monitor另核。

标准入口`make test-vector-zonal-weighted-online-runner`和标准Native中的`test_online_vector_zonal_weighted_fixture.py`，同步Online、changed gate、独立手工workflow、observer与固定矩阵／删断言拒绝检查。本节已有本地 Native 专项、独立 Runner 与登记通过记录，筛除不计全量，准备结果不证明 Linux 全量或真实 Hosted。


## 矢量面分区百分位独立 Hosted 验收

`vector-zonal-percentile-persistence`；`make test-vector-zonal-percentile-online-runner`。独立运行、600秒、身份／成果／清理沿[专项共同要求](#水文与分区统计专项共同要求)，算法归[栅格契约](../../workflow/栅格算子契约.md#栅格计算契约)。单项仍显式`percentiles=[p]`、原前缀`percentile_1/percentile_value_1`；64执行及查询纳预算，不以旧结果认证后来多项套件。

| 固定成功链 | 数量与独有断言 |
| --- | --- |
| 原八列／端点／中位 | 不带百分位1链；0／100、奇偶总体P50共4链。 |
| 插值与覆盖 | 25／75、33.3跨块中心／全部触及共4链。 |
| 字段与派生 | 自定义／空前缀2链；重分类→面化→P90共1链。 |
| 原生有效性／空结果 | Int64／UInt64 NoData邻接合法值2链；全无效保面行、空矢量保Schema／CRS共2链。 |

共16 GeoPackage。面含孔／重叠／MultiPolygon／细面／部分及全出界／单有效零／重复几何，以唯一source_row核原行序；栅格旋转、第二波段、独立mask／alpha、raw scale-offset。FID不代用户索引；重复索引／活动几何列冲突／混合复数alpha／数值极限另列Native。

24类各中英文共48准确失败：百分位列表null／布尔／字符串／嵌套数组／对象／＜0／＞100七类；波段0／越界／布尔／字符串四类；所选复数数据／alpha两类；all_touched数值／null、前缀null／布尔四类；原统计列／percentile_1／percentile_value_1冲突三类；异CRS／缺栅格CRS／缺仿射／奇异网格四类。必须到当前execution的analysis任务核准确原因，load／scan失败不充数；新增百分位原因中英核，原英文原因按现行契约，不称全部诊断已翻译。反向查询不改原语言／错误／快照；非法几何／无矢量CRS／非有限JSON／IO磁盘注入沿Native或原专项。

oracle用像元平面Shapely中心包含／正面积交，派生面用独立4邻接＋横条并集，不调产品、GDAL栅格化／面化或NumPy百分位。标量排序／标准库统计／Fraction确切二进制位置核逐面全属性／几何孔洞多部件／Schema／CRS／序、原八列＋新增两列、null／零；带／不带百分位的八列逐值相同。端点／P50与min/max精确，其他百分位rtol=4e-15／atol=0，其余原统计rtol=1e-12／atol=1e-12。公开FeatureCollection核完整属性／身份，完整拓扑／真实字段类型由物理重读核，不增宿主几何依赖。

成功递增全检、每次失败全检16份，共904次GeoPackage检查＋首次源全检计65物理轮。只缓存冻结独立期望；每轮重读14源的原hash、全部像元／mask／类型／原生NoData／仿射／CRS／scale-offset及矢量几何／属性／Schema。归档14源／16成果原字节、大小／SHA256清单、独立期望及16当前执行血缘取证；缺失／变化／额外文件均拒，不覆盖／重建／修复。

普通矢量容器只核物理＝Meta严格正整数大小／唯一子层，不虚构RasterArtifact／COG／第三方大小。扫描精确父执行／User／Tenant／Engine；直接链核面＋数值栅格双源，面化派生仅实际TIFF。observer限本套件，核内部save／derive端口及公开`operations:null`，Meta关系／Monitor另核。失败零目标／扫描／derive，源物理／Meta与旧成果保持，NFS／Runtime／registry／部署／Infra全清。

标准runner、Native的`test_online_vector_zonal_percentile_fixture.py`、Online／changed gate／workflow／observer及矩阵删断言mutation均登记，接线不计Hosted通过。固定`c116e05fd9fb3eb4f2fd49bd1598879f64a14647`的[Linux Job124388227200](https://github.com/pampa0629/addp/actions/runs/41151306867/job/124388227200)全量10587过，无失败／跳过／筛除；Schema污染须无存活Feature时修改、关重开后核字段并拒绝，九种污染及生命周期先红后绿保留。此前段错误仍独立失败，不由新通过关闭，未认证Hosted／页面／容量。

## 矢量面分区面积加权统计独立 Hosted 验收

`vector-zonal-area-weighted-persistence`；`make test-vector-zonal-area-weighted-online-runner`。准备／600秒预算、成果身份与清理沿[水文与分区共同要求](#水文与分区统计专项共同要求)，算法及字段以[栅格计算契约](../../workflow/栅格算子契约.md#栅格计算契约)为准。

16条GPKG链：中心／all_touched、默认／False原八列、国际／美制测量英尺、旋转非方形／反向轴、重分类／转面接统计、空前缀／零负、全无效／带类型空面批、Int64／UInt64原生NoData邻接。14条追加三列、2条原八列不变，False保留前缀下原同名业务属性；519×515多波段第二波段跨二维512块，raw scale-offset／mask/alpha，面含孔洞／多部件／重叠重复／出界／边点零面积／无中心细小正面积／解析三角形。

原八列与关闭对照相等，新增三列不随all_touched改变：覆盖平方米含无效区域，有效面积排无效值，均值按有效正交叠面积、不乘alpha。独立矩形闭式／有理三角裁切、2000位Decimal按实际TIFF单位与精确仿射行列式，累计全部有效像元乘积后舍入；禁止产品函数／GEOS intersection／超采样，不放宽面积／均值比较，重复值权重按精确次数合并。派生分类面以独立BFS／行条核完整归属，原统计按原精度。

38类双语76次准确analysis失败：非布尔开关8、百分位／单或成对外权重／加权stddev互斥8、波段/alpha/复数6、CRS不符／地理复合／缺CRS网格／奇异6、三新列／原列冲突／自交／非面6、all_touched非法2、前缀非法2。须同execution／原因／冻结语言，load前败不充数；活动几何名、重复索引、混合复数alpha、未知单位归一化／塌缩／数值极限仅Native，合法舍入零沿原契约。

成功136＋76×16＝1352次完整GPKG全检，含源基线93物理轮；每轮核源全像元mask／类型NoData／CRS网格编码、面全几何属性，成果全几何／属性／行序／字段名序型／统计null。16原GPKG、全部源／冻结预期及大小／SHA256清单归档；物理＝Meta，归档大小另核，缺失／篡改／额外项／链接拒绝不修复。直接／重分类链两原源、转面链只栅格，共31条当前关系；内部有界血缘／公开operations:null／Monitor分核，源与全部16成果末轮Meta等于初始化／首扫快照，前后快照归档。正式EPSG:2222／2277保留英尺及预览CRS；本地Native／Runner／登记不计Linux全量／实际Hosted，筛除不算全量。

**本块其他专题的正文分工**：邻域按各节引用的[共同要求](#邻域与-idw-专项共同要求)，其余栅格采样／频数／斑块按下述[水文与分区共同要求](#水文与分区统计专项共同要求)，仅收敛其原已声明的原12权限、NFS／同源默认Runtime及600秒；各自矩阵、容差、归档和Native例外不互换。实际结果须固定版本证据，准备／登记不代Linux全量、Hosted、UI或容量，不重试／扩预算／改旧套件。矢量拓扑11权限与机器／初始化身份另列，不套栅格权限。

**四个拓扑case的共用核验**：差集、融合、相交、节点提取沿 `vector-topology-persistence` 独立case，非默认Tenant、同普通User精确11功能权限、隔离Hosted及600秒，不跑full／旧T4／容量。功能权不代内容read，真实派发待共享源／成果授权及必要门禁确认，无管理员／NFS例外／不可见血缘旁路；公共CI超时取消仍失败。精确读写以[正式授权契约](../../addp授权上下文规范.md#524-develop-geopackage-工作流的精确读写)为准，双输出计算case不表示多目标保存已获准。

- 独立OGR核原坐标／几何成员、原生字段名序型／值／NULL、行序／CRS及原件SHA；不用生产算法生成答案，不epsilon／舍入，环向／成员规范化仅按算子契约。业务ID不同FID且JSON安全，普通geometry字段不替活动几何；超大整数／空批等Native范围不外推实页。
- 各成果精确create输出／facts、公开tenant_id／triggered_by为同主体严格整数、父文件物理＝Meta正整数、唯一子层与扫描数按case；当前实际输入到成果的active derive及Monitor精确，容器层／中间成果／成员关系不虚构独立技术血缘。初始源扫描与成果扫描分开，不手工补扫collect。
- 算子负例须到冻结目标节点的v3原因／参数，Develop／Monitor同User／UUID中英提示建议复制均核，无业务字段ID／坐标／路径／原生正文；load拒绝不充数。无失败目标／outputs/facts／新增扫描／失败derive，源与已有成果原字节／Meta逐次不变，不业务重试。
- 浏览器逐导航绑定当前真实响应的User／Tenant／权限，不借旧Token；EWKB／FID与业务ID、全部OL几何属性／独立投影、实际主题像素及血缘画布按各case核，不只看组件。源／成果／API／Meta／原图／页面证据及最终Owner/browser报告归档，incomplete只是阶段；所有退出撤原已声明Grant并核领域、NFS/export/owned root、Runtime/registry、部署/Infra清零，清理不认证业务DELETE。runner／Native／browser／case／CI及mutation同次接入，不新增API／权限或提前派发。

### 几何差集独立普通用户验收

`geometry-difference`：五源basic身份＋精确read Grant由初始化员准备（原14权限），执行者仍11权限、无deep也须读准入。保存原EA／Scope／目标先由批准者在正式审批上下文GET审阅，再POST原单次批准；先发布／basic成果识别／用户read生效，再独立deep核内部预览，手动deep不混自动扫描。证据先归档，再撤源／成果Grant核物化独立性，再清理；不读私表／SQL替审批，metadata_read不升级read。

| 发布／事实／取证 | 不可省略的约束 |
| --- | --- |
| NFS暂存 | 同文件系统显式 `publication_staging_path`、0700、真目录非链接、同实际发布服务UID且发布后空，普通接口不可见。宿主 `NFSExport.owned_root` 先核本轮根／owner标记，容器worker取已证根UID为发布者，不拿观察进程UID代替；Native同规则。禁chmod/chown、改Runtime用户、任意期望UID或放宽权限。 |
| UID证据 | 记录发布根／暂存／观察UID、权限与空状态，错误保观察值；wrong owner／权限扩大／残留／缺失链接仍拒绝。Topology身份差异正反例及GeoPython真实差集／原面邻沿唯一物理路径，输入须命中Native；确定性不补原T4未观察inode。 |
| 机器持久事实 | 差集与面邻共用Manager唯一OAuth取令牌：同Tenant `addp-manager` Service Access Token／内置 `tenant.manager_runtime` 的既有 `meta.catalog.read`，仅GET `/api/v1/meta/items/{item_id}` 读当轮五源一成果，无新账号／Grant／权限/API。先核Client／Service／Token类型／Tenant／资格，再逐item匹配用户已见ID、Engine／指纹／规范路径／扫描；不枚举父或别项，不把机器凭据交浏览器／证据。机器Meta大小／子层行列及前后不变须真持久，缺／错／拒绝／依赖失败即失败，无人员回退／预览重解析／物理替代。共享实现也服务Manager上传，删局部副本，沿Topology／Manager／Execution现有入口登记。 |
| 跨视图 | `attributes.item` 仅比用户公开data_type／format／layout，不全字典比较或要求用户file_count／refs；共用GPKG验证器同样三字段。完整组成由精确授权事实／初始Grant回执另核，机器完整描述／组成统计原样归档并比前后。夹具含真实file_count／refs及三字段视图，核合法差异与错身份／采集版本／组成／统计。 |
| 归档边界 | 机器正文只在身份／扫描全绑定成功后、统计断言前归档；错绑定只留已确认目标ID／阶段／固定原因，无被拒正文／值／私标记。首次提交前存五源／独立oracle／身份范围，execution／审批上下文及每次Develop等待（含异常终态）／Monitor响应在断言／物理前写incomplete，已准入Meta/Grant即时分存。 |
| 物理失败取证 | 只从当轮owner根复制精确 `outputs/difference.gpkg`，每次独立快照／长度SHA，不覆盖旧证据；空缺／非普通如实记，不递归、不以取证成功证有效。保原九行oracle／schema/NULL/CRS/清单暂存断言；每次worker及外runner存完整stdout/stderr/退出码，超时保已捕获原文不截断／摘要替代／重试。仅合成夹具公开响应及无凭据worker输出，无连接／Token／认证头；失败／未知／清理错误分开，原always上传及撤权清理继续，故障注入不代新T4。 |

真实派发必填 `topology_source_commit` 为另经确认且有适用门禁的40位小写SHA；部署前校验、实际HEAD等于输入且为dispatch SHA祖先（含自身），报告两SHA，不能main前移覆盖。其余Topology case无输入仍取dispatch快照；冻结本身不授账号／批准／Grant或运行权。

仅一次新建 `outputs/difference.gpkg` 成功＋四只读负例（两load＋difference）；范围外写拒绝由授权专项另验，不充算子负例。A十二有限有效二维点线面／多部件／GeometryCollection，B三面含重叠重复和末行扩展；整体扣除九非空结果，源内点／边界点／共边线全扣略，保不交／多点部分移除／线切多段／多线退单／面多部件孔洞／多面部分扣／完整集合／仅共边。B全行参与，非首行或逐对拼接；手写几何冻结、不调并差集。

A唯一非空ID非FID；每源≤一行按A顺序，仅A字段类型值NULL／几何CRS，不添source_id／面积长度／B列。B同名属性与空重复a_key不要求标识。nullable Int64/Boolean/String及普通geometry隔离。唯一混合GPKG `mode=fail` 核九行／3857、1自动扫描／双源2派生，五源原Meta与图／文件归档，不造B行级贡献。

四负例：A缺来源字段、A自交、B非面、CRS异，必须第1个difference；对应v3 `operator_input_invalid/input_gdf`（两例）、`operator_mask_invalid/gdf_b`、`operator_crs_invalid/无parameter`，均无field。Develop/Monitor中英精确复制共16观测，同User/UUID；成功混合EWKB／OL集合全成员实际点线面像素、成功／双源血缘／四失败双语页核，正式授权及部署证据未具备前不派发。本地runner／发布安全修正不证明真实批准Grant或T4通过。

### 融合几何独立普通用户验收

`polygon-dissolve`：两成功／八失败，资源授权确认前不派发。10行投影面以 `group_fields=[zone,rank]` 得4组、成员4/2/2/2；`[]` 得全局1组10成员。字符串＋nullable整数保空组，同组含重叠重复消除、跨组独立、多部件不拆行；独立手写矩形并集／成员，不调产品。

result只分组字段／fusion_id/member_count／几何，members保10原属性几何／CRS／顺序并加fusion_id/source_id，每源一次且组计数守恒；nullable Int64/Boolean/String、普通geometry与活动parcel_shape隔离，保存后按GPKG真实几何声明，不假定源列名、不取first／聚合。两端口共四GPKG、mode=fail；融合用精确对称差空＋面积，不承诺共线冗余顶点／环序，成员规范化WKB精确。

4扫描／每执行2源成果边共4边、四原图，无members→result技术派生；第二成功及每次失败重查所有旧成果。八负例：来源字段缺／ID重／无效面、地理CRS、分组非列表／重／缺字段、固定source_id冲突；第1个dissolve的v3 input_invalid/input_gdf、crs_invalid/无parameter、group_fields_invalid/group_fields、dissolve_fields_conflict/无parameter，reason加operator_、均无field。四成果／五输入不变。

四API EWKB与全OL属性／NULL／坐标及四地图实际面像素>30，重复明细用记录不凭像素；四血缘画布、两成功及八失败双Owner双语页归档，五源四成果原件保留。双save不据此获得跨文件事务或多目标授权。

### 相交叠加独立普通用户验收

`intersection-overlay`：一成功／四失败，授权专项和必要门禁未确认前不派发，历史公共CI超时取消不算通过。A17/B18混合要素，显式唯一非FID ID；手写16非空对含九维度组合、共边共点／十字、完整点线面集合、多点、异ID重复几何、面积2^-40微交；孔洞内bbox候选／不相交无结果。每对多部件／集合一行，A/B原序，双方字段nullable Int64/Boolean/String＋source_id_a/source_id_b，普通geometry不替活动几何。

一混合GPKG核全坐标成员／字段／行序／有效XY／3857，不用产品intersection或ExportToJson默认精度生成期望；规范化仅按契约，原EWKB/OL独立投影及真实集合成员点线面像素核，微小成员只证物理/API/OL，不称亚像素可辨。一次扫描／双源2派生／唯一子层非独立主体。

A/B来源字段非法、CRS异、双方业务重名四负例，intersection v3 `operator_input_invalid/input_gdf`、`operator_reference_invalid/gdf_b`、`operator_crs_invalid/无parameter`、`operator_overlay_fields_conflict/无parameter`，均无field；四源／成功成果不变。归档源成果、五执行／扫描／双源图／原Meta／预览／页面，未真实通过不计T4。

### 节点提取独立普通用户验收

`line-nodes`：一成功／九失败。9源／11线部件含闭环支线、分裂三角形、同端异路、同源不连部件、2^-40间隙；手写15节点／22端部明细，坐标序ID／部件／start-end／度数精核，degree总和＝members行数＝2×部件数。result仅几何/node_id/degree，members保全部业务字段nullable类型及普通geometry。双PointGPKG独立OGR，2create／2公开同主体整数扫描／2当前源到成果派生，行级来源仅members，不称中间成果血缘。

九负例：反向重边、同源部分重合、未打断T／十字、内部自交、业务node_id冲突、活动几何degree冲突、重来源ID、地理CRS；冻结节点既有v3及双Owner同UUID双语提示复制，失败源成果不变。两真实Point地图核完整EWKB/OL／主题像素、同点明细不去重；两save无跨文件事务承诺，失败保障只指准入未进入save。

### NFS GeoPackage 血缘权限专项验收

`vector-topology-persistence` 的 `line-nodes-lineage-permissions`，只一次成功节点提取，不跑诊断／地图／发布节点。具引擎管理范围初始化员集中精扫 `nodes-source.gpkg`，生成 `nodes.gpkg`／`node-members.gpkg`，仅授普通读者这三文件metadata_read，两个成果均可见同执行来源边。

同User／Manager页会话先撤源Grant、改深度重查：200、隐藏提示、仅成果节点／零关系，无隐藏源计数／身份／执行证据；再撤当前成果，须 `404 lineage_subject_not_found`，清画布及旧隐藏提示。原页一次资源树顶刷新仅Meta只读回读，两个撤权文件消失、node-members仍可见、无效locator／右侧清空，无整页刷新或扫描。存响应／截图、核物理不变、撤本轮Grant；机器事实或content read不能替代该metadata撤权场景。

### 矢量面分区面积加权总体标准差独立验收

`vector-zonal-area-weighted-stddev-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16成功／50类双语100准确失败／1736全检，117物理调用含首次期望；14开启stddev＋原八列／十一列各一关闭。覆盖孔洞重叠多部件／微交无中心、米／两英尺、旋转反向、重分类转面、零负／常量／单有效／全无效／空批。

独立闭式面积＋Decimal中心偏差先冻结，完整几何属性统计null／字段型；16原GPKG和大小hash归档，物理＝Meta且归档清单另核，不虚构RasterArtifact声明。当前实际源内外血缘／扫描／Monitor及全部成果Meta快照不变；活动几何名冲突／数值极限仅Native。本地准备不算Linux全量／Hosted，不重试扩预算。

### 7×7 像元方窗独立 Hosted 验收

`raster-focal-square-seven-persistence`；`make test-raster-focal-square-seven-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。18COG含12种7×7方法、三全1权重、3×3/5×5同源mean、7×7全无效。369列×367行跨横纵360块界，三非对称权重及min/median/max/多percentile跨轴；独立冻结预期核全波段mask、P0/P50/P100实际对照、三对全1／非加权及权重方向边缘。

30类双语60准确focal失败，共1251全检；18COG／源／冻结预期及SHA归档，六仿射CRS／Float64NaN／布局及实际nearest核。数值极限、读块大小／注入仅Native。

### 米制圆形邻域统计独立验收

`raster-focal-circle-persistence`；`make test-raster-focal-circle-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。18COG涵九非加权方法，3/4米步长半径5及邻接浮点、P50＝median、两英尺／仅中心／重分类／无效／方窗对照；519×515跨轴、薄行列／常量单样本／零负／NoData/mask/alpha、raw scale-offset。

50类双语100准确拒绝；独立Decimal距离／标量统计，不调产品窗归约。1971全检／119物理调用含预期，18COG／源／预期数组原件归档；候选≤5×5、等号纳入、无epsilon，沿原数值容差而分类成员零mask精确，复数／非有限JSON／极限仅Native。

扩加权后原两套圆窗的三旧方法拒绝，改为加权mean尺寸错、weighted stddev全零、weighted sum圆外非零；仍真实focal双语准确，原成功／全检／原件／清理及600秒不变，旧固定证据不追认新版本。原十二方窗方法不改，准备不算Hosted／UI／容量。

### 正交旋转网格米制圆形邻域统计独立验收

`raster-focal-circle-orthogonal-persistence`；`make test-raster-focal-circle-orthogonal-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。19COG含九非加权方法、90°/180°/270°及斜向正交、非方形、跨轴／薄行列／三单位、半径等号邻接浮点；同轴长北向／P50-median／方窗／中心／重分类／全无效实际对照。

53类双语106拒绝，含极小剪切、浮点点积零而精确非零、旋转单轴镜像，行列轴／单像元超限仍拒绝。独立Decimal全仿射距离统计，数值容差沿原圆窗，像元mask／六仿射／CRS／完整COG与实际nearest精核；2204全检／126物理调用含预期，19COG／源／冻结预期及SHA归档，极限／注入仅Native。

### 米制加权圆窗独立 Hosted 验收

`raster-focal-circle-weighted-persistence`；`make test-raster-focal-circle-weighted-online-runner`；沿[邻域共同要求](#邻域与-idw-专项共同要求)。18COG含北向非方形3×3／正交旋转非方形5×5的三非对称方法、三对同源全1／非加权、两英尺、1×1非单位权重、重分类／全无效／半径前一浮点。独立Decimal全仿射距离及逐位置权重，stddev中心偏差、sum绝对权重；全1实际对照精确相等，核半径等号／前浮点边界、跨轴／NoData／显式mask／部分alpha及raw波段。

36类双语72准确focal失败（权重14、方法窗10、网格CRS8、波段4）；1467全检／含基线91物理轮，18COG／源／冻结预期与SHA归档，像元mask／六仿射／CRS／Float64NaN／实际nearest及布局精核；极限／注入仅Native。

### 非方形像元邻近距离独立验收

`raster-proximity-rectangular-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16COG涵横纵比例使最近反转、小数步长、两英尺、raw第二波段、NoData/mask/alpha、零负／UInt32、跨块／单行列／原方形对照；26类双语52准确proximity失败。独立枚举目标预期，968全检，16COG／源／冻结数组及hash归档；米制、网格CRS／mask／完整COG和overview实核，数值极限／私文件破坏仅Native。

正交旋转扩展不改16/52/968／600秒，仅同步实际双语准入：rotated_x/rotated_y是非正交剪切、orientation是单轴镜像，仍须当前节点准确拒绝；本suite只证北向矩形成果，不补认旋转Hosted。Linux／实际运行另行认定。

### 分类栅格连通斑块编号独立验收

`raster-region-group-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16COG含4/8邻接、重分类、跨轴／重复编号一致／union根变、raw第二波段mask/alpha、旋转／地理／无定位、Int64/UInt64／全无效；另2CSV编号接分区统计，核每斑块分类、计数／总量守恒。

18类双语36准确region失败：波段0/越界/布尔/串/null/列表，邻接0/3/布尔/串/null/列表，alpha／小数／正负越界／原生Int64UInt64不安全类；前置加载提交保存不充数，复数alpha／编号上限／I/O仅Native。独立BFS先冻，819全检／含源基线55物理轮；完整编号mask／锚点序／网格CRS、实际nearest及CSV九列，16COG＋2CSV／源／预期原件清单归档。COG三方大小、CSV物理Meta及预览分核，无RasterArtifact；准备不计Linux／Hosted／页面容量。

### 点位栅格采样独立验收

`raster-sample-points-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG涵cell原值三状态、边界邻接浮点、旋转剪切／地理／反向、raw第二波段／NoData/mask/alpha、Int64UInt64、重复点／nullable属性、519×515跨轴512、重分类／表达式接采样、全无效命中／全外／带类型空批；所有二维点几何属性顺序类型保留，FID／内存索引非业务ID。

18类双语36准确sample失败：0负越界布尔串null列表对象波段、CRS异、MultiPoint/LineString/Polygon、新两列冲突、alpha、Int64UInt64命中不可精确值／缺网格；混合复数、非有限JSON坐标、重复索引／活动几何／极限／I/O仅Native。独立高精度半平面判界＋闭式原生值，不调产品逆变换分组块读；712全检／含预期53物理调用，16原GPKG／全部源／预期及hash归档，root/run／源字节／夹具绑定。普通矢量声明与物理Meta分核、两原始输入内外血缘，不虚构RasterArtifact，准备不计Hosted／UI容量。

### 双线性点位栅格采样独立验收

`raster-sample-points-bilinear-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG含省略method／显式cell、bilinear四二一支持、中心邻接浮点／图外边缘不足、旋转剪切／地理反向、raw第二波段／NoData/mask/alpha／Int64UInt64、512跨轴重复点、重分类→表达式→采样、全无效／全外／类型空批。

正权重全支持须有效，零权重无效邻居不参与、精确中心仅自身；无epsilon／补边外推／归一化，无效支持优先整数精度／结果范围问题，保全部点属性／序／CRS。原cell18类＋null/布尔/数/空串/未知/大小写/列表/对象8种method，共26类双语52准确sample失败；复数／非有限／极端仿射／索引几何／I/O仅Native。独立中心四边形高精度面积比＋精确权重原值，不调产品插值；136＋52×16＝968全检／含预期69物理调用，16原GPKG／源预期／清单归档，完整几何属性Schema状态null／CRS沿原容差，不借旧cell证据作Hosted。

### 栅格分区分类频数独立保存验收

`raster-zonal-class-frequency-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16CSV含省略method／显式summary两对照、直接／单类别／第二波段raw、512跨轴多行、类别／分区重分类、旋转非方形／地理／反向、两源有效性／原生整数／混合诊断、分类全无效／分区全无效。

32类双语64准确analysis失败：未知空method、percentile值/null、外权重／仅权重波段／加权stddev／非布尔开关、双角色0越界布尔串波段、alpha／复数、类别小数／不安全Float64／原生64越界、分区小数／不安全／UInt64越界、尺寸位置CRS／缺CRS网格／奇异；混合alpha／计数溢出／注入仅Native。独立逐像元整数计数＋精确比例核区守恒、有效分类分母、零负／诊断null行、七列序／空Schema／最多50行预览，summary两链原九列。

136＋64×16＝1160全CSV检查，16CSV／源／冻结预期及hash归档；声明精确target/file/create，物理Meta正整数相等，归档大小另核不冒产品声明。实际全部输入血缘／前后Meta不变，准备／Native/mock不算Linux全量／Hosted。

<a id="vector-zonal-class-frequency-persistence"></a>
### 矢量面分区分类频数持久化

`vector-zonal-class-frequency-persistence`；入口 `scripts/test/online-hosted-vector-zonal-class-frequency-gate.sh`，沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG含两覆盖、孔洞重叠多部件、矩形旋转地理、NoData/mask/alpha、跨块／全无效无覆盖空批、三种业务fid大小写／原summary。32类双语64准确analysis失败，独立逐面像元计数、有效分母守恒、原属性几何行序；1160全检／81物理调用不含收尾，16原GPKG及hash归档、root/run／源码源hash冻预期。

业务fid可展开重复，存储FID独立唯一，不能互相代替；声明／大小／扫描／全部实际输入内外血缘／Monitor／Meta快照和语言不变按共同要求。底层写入发布／清理故障及竞争仅Native，不伪造Hosted故障；准备状态见栅格后续专题，不计UI／容量／任意影像或实际Hosted通过。

<a id="raster-sample-lines-persistence"></a>
### 沿线栅格采样持久化

`raster-sample-lines-persistence`；入口 `scripts/test/online-hosted-raster-sample-lines-gate.sh`，沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG含cell/bilinear、米／正式国际及美制测量英尺、折线重复内顶点、短／闭合／准确终点、矩形旋转、outside/nodata/alpha、三种业务fid展开。28类双语56准确sample失败，独立Decimal段长＋逐点路径／精确支持权重核全里程坐标数值状态／原属性；单位取CRS事实，不拿独立浮点近似扩容差。

1032全检／73物理调用不含收尾，16GPKG原件及hash归档；两源血缘／公开投影、Meta与旧成果不变沿共同要求。正权无效支持不补值／归一化，闭合起终点按不同里程保留。混合类型／数值极限／底层注入仅Native，不改原点位／算法／权限，未以本地准备记UI／容量或Hosted通过。

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

## 点线密度专项共同要求

仅下方六个明确引用本节的密度suite适用，不扩大邻域／IDW或其他套件的范围。点、线的单位、核函数、完整源校验及数值边界分别以[点密度契约](../../workflow/栅格算子契约.md#米制圆形点密度栅格)、[线密度契约](../../workflow/栅格算子契约.md#米制圆形线密度栅格)为准。

- **运行**：独立手工Hosted／NFS、同源默认入口产品Runtime、普通User原12权限，4 CPU／2 GiB／无额外swap；各600秒包含首次预期、执行查询、全检、归档与业务清理。准入及外层退出沿[T4规范](../T4-Online验收规范.md)。不接管个人部署、不改产品／原套件／权限，不重试、扩预算或加调度。
- **完整核验**：每个终态真实重读全部已有COG，核全部像元／mask／合法零、Float64／NaN定义、方法／字段／单位、原六项网格／CRS、COG布局及全部实际nearest概览。artifact＝物理＝Meta相等正整数大小；自动扫描绑定当前父execution／User／Tenant／Engine，点或线源＋模板两输入的内部事实与公开投影分别校验，Monitor单独核对，源与先前成果字节／Meta不变。
- **独立证据与清理**：每节保留自身oracle，预期一次生成、只读冻结并绑定root／run、全部源hash及夹具源码，不缓存实际成果。按下表归档原COG、全部源、冻结预期、大小／hash清单及Develop／Monitor原始终态响应；数量不替代内容核验。准确核density节点、原始原因和冻结语言，前置失败不充数、反向查询不改旧错。失败零发布／新增扫描／派生血缘；正常、失败和部分启动均核本轮工作区／oracle、NFS、Runtime／镜像／部署及Infra等已声明资源清理，首败与清理结果分别记录。
- **层级**：专项Runner沿 `make test-raster-online-runner` 聚合，GDAL及污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`。这些正文定义验收要求；准备／本地通过不证明Linux全量或真实Hosted、UI、容量、稳定吞吐。各节声明的原始类型、异常格式、数值极限、故障注入仍单列Native，不扩格式支持。后续状态见[栅格后续专题](../../../next/栅格算子体系后续专题.md#三后续工作)。

### 密度专项数量矩阵

下表为每个suite独立要求，**不是已通过结果**。每次成功重读1至N份，失败各重读N份；物理调用含首次源／预期基线，COG归档份数与成功链数一致。

| 专项 | 成功链／原COG | 双语准确失败 | 成果全检（成功＋失败） | 物理调用 | 业务执行／原始终态响应 |
| --- | --- | --- | --- | --- | --- |
| [点显式权重](#点密度显式非负权重独立-hosted-验收) | 24 | 64 | 300＋1536＝1836 | 89 | 88／176 |
| [四次点核](#米制点核密度独立-hosted-验收) | 18 | 52 | 171＋936＝1107 | 71 | 70／140 |
| [四次线核](#米制四次线核密度独立-hosted-验收) | 24 | 60 | 300＋1440＝1740 | 85 | 84／168 |
| [线显式权重](#线密度显式非负权重独立-hosted-验收) | 16 | 40 | 136＋640＝776 | 57 | 56／112 |
| [uniform线密度](#米制圆形线密度栅格独立-hosted-验收) | 16 | 40 | 136＋640＝776 | 57 | 56／112 |
| [uniform点密度](#米制圆形点密度栅格独立-hosted-验收) | 16 | 32 | 136＋512＝648 | 49 | 48／96 |

## 点密度显式非负权重独立 Hosted 验收

独立手工 `raster-point-density-weighted-persistence`；`make test-raster-point-density-weighted-online-runner`。准备、核验、归档数量和清理统一按[密度共同要求与矩阵](#点线密度专项共同要求)。

uniform／quartic各12条成功COG链，共24条：省略权重、显式全1、混合非负权重、混合点序倒置、整数权重、同位置实际重复记录、混合权重乘8、全零、保留所选字段的空批、全无效mask／alpha模板、国际英尺矩形横跨512块、美制测量英尺旋转／剪切纵跨512块。实际文件核全1原数值／mask保持、点序／模板隔离相等、整数重复等价及绝对权重比例。夹具独立覆盖精确圆边界及相邻浮点、中心／模板外支持和局部无支持，uniform边界纳入，quartic边界准确零；不按权重总和归一化。省略字段points/m2，选字段weight/m2。

每方法16类各中英文提交，共64次准确density失败：字段null／空／全空白／布尔／数字／列表／对象／缺失字段／活动几何列9类；权重缺失／负数／字符串及范围外缺失／范围外负数5类；零权重非法几何／CRS冲突2类。必须到达指定算子并核原始双语原因，读取／提交前置失败不得充数，反向查询不改冻结语言。布尔／复数／非有限原始权重、宽整数及数值极限、故障注入单列Native，不扩持久化格式。

独立全点Fraction米制距离及2000位Decimal展开核公式预期，不复用产品索引／距离／核助手。

## 米制点核密度独立 Hosted 验收

独立手工 `raster-point-kernel-density-persistence`；`make test-raster-point-kernel-density-online-runner`。准备、核验、归档数量和清理统一按[密度共同要求与矩阵](#点线密度专项共同要求)。

16条quartic成功COG链：普通点集／逆序／整批三倍重复／中心重合及半径中点剖面4条，半径下邻浮点／等号／上邻浮点3条，模板外正支持／局部无支持／空批3条，全无效模板／mask-alpha-scale-offset忽略2条，非方形横跨512块／旋转兼剪切纵跨512块2条，两种英尺2条；再实际保存默认uniform／显式uniform2条，共18条。中心最大、圆周贡献准确0、无支持为有效0；全部重复XY逐条贡献，不设epsilon、边缘不归一化。实际COG核点序／模板隔离、整批三倍贡献及默认／显式uniform对照；每份全像元仍须精确匹配独立预期。离散中心采样不以整图像元面积加权和等于点数作为断言。

半径null／true／false／0／负数／字符串／列表／对象8类，缺失／空Point／LineString／MultiPoint／范围内三维Point／范围外三维Point6类，CRS不一致／地理CRS2类，method为null／true／false／0／小数1.5／Quartic／gaussian／空字符串／列表／对象10类，共26类×中英文＝52次准确density失败；反向查询不改冻结错误，读取／提交前置失败不得充数。

独立全点Fraction米制距离及2000位Decimal展开四次核oracle，不复用产品索引／距离／核计算。

数值极限、非有限JSON、异常格式、重复索引和故障注入单列Native，不扩大产品格式、权限或UI／容量／吞吐认证。

## 米制四次线核密度独立 Hosted 验收

独立手工 `raster-line-kernel-density-persistence`；`make test-raster-line-kernel-density-online-runner`。准备、核验、归档数量和清理统一按[密度共同要求与矩阵](#点线密度专项共同要求)。

24成功：6条uniform实际对照（无权重默认／显式、混合权重默认／显式、全零、相切及相邻浮点组合）；18条quartic（无权重／全1、混合非负／要素逆序、整数权重／实际重复要素、权重乘8／原线折返乘2、等价多部／拆段反向、全零／保留字段空批、全无效mask-alpha模板／边界组合、国际英尺矩形横跨块／美制测量英尺旋转剪切纵跨块、解析直径／偏心弦）。核实际默认／显式uniform逐值一致、quartic全1／整数重复／顺序／多部／细分／模板隔离等价及倍增；实际跨方法见证必须不同。逐原始线段积分，边界核为0、完整圆核归一化、不按要素权重总和或模板边缘重新归一化；无支持和全零保留有效0。

30类各中英文，共60次准确density失败：原加权20类（9类非法字段、5类缺失／负／字符串及范围外坏权重、6类零权重下非法几何／CRS／半径）；新增method的null／true／false／数字／列表／对象／空串／空白／大小写错误／未知值10类，按原JSON值显式提交，不能以默认值替代。严格核源WKB／属性／Schema／CRS、活动几何列shape和相邻浮点保真，不以加载／扫描／提交前置失败充数；错误节点／原因及冻结语言准确，反向查询不改旧失败。

独立全原始段2000位Decimal的t多项式积分oracle，不复用产品z参数裁圆／根式／权重准入／舍入助手；仅对同一固定精度下的纯标量平方根做有界4096项memo。

另核请求中method省略事实与实际方法／权重／单位，物理／身份及两源血缘沿共同要求。

宽整数、原生布尔／复数／非有限权重、真正数值极限及故障注入继续单列Native；原两套uniform Hosted不被quartic准备记录代验，不改产品、权限或跨模块逻辑。

## 线密度显式非负权重独立 Hosted 验收

独立手工 `raster-line-density-weighted-persistence`；`make test-raster-line-density-weighted-online-runner`。准备、核验、归档数量和清理统一按[密度共同要求与矩阵](#点线密度专项共同要求)。

16条COG成功链：省略字段／全1、混合非负／要素逆序、整数权重／实际重复要素、权重乘8／原线折返、等价多部／拆段反向、全零／保留所选字段空批、全无效mask-alpha模板／相切边界组合、国际英尺矩形横跨块／美制测量英尺旋转剪切纵跨块。实际文件核省略／全1数值及mask相等（单位分别metre/m2与weight/metre）、整数重复等价、顺序／多部／细分／模板隔离相等、×8与折返×2，不归一化。混合线集覆盖重复、部分重叠、闭合自交、折返和模板外圆内段；边界组合含相切／略内／略外／端点接触／无支持／零权重，固定完整圆面积，逐原始段乘原要素权重累计。

每类中英文提交，共20类／40次准确density失败：字段null／空／全空白／布尔／数字／列表／对象／缺失列／活动几何列9类；权重缺失／负数／字符串／范围外缺失／范围外负数5类；零权重Point／整线零长／多部零长分量／范围外Z线／CRS冲突／非法半径6类。先核源WKB／Schema／属性／CRS／实际字段角色与相邻浮点原始值，异常格式归一化或读取／提交前置失败不得充数；准确核节点／原因／冻结提交语言，反向查询不改旧错误。

独立全原始段2000位Decimal二次方程oracle，不复用产品索引／投影裁圆／根式区间／权重准入或舍入助手。

原始布尔／复数／非有限权重、宽整数及数值极限、异常持久化格式和故障注入单列Native。

## 米制圆形线密度栅格独立 Hosted 验收

独立手工 `raster-line-density-persistence`；`make test-raster-line-density-online-runner`。准备、核验、归档数量和清理统一按[密度共同要求与矩阵](#点线密度专项共同要求)。

16条COG成功链：普通／逆序2条，双倍重复／原线折返2条，等价MultiLineString／拆段并反向2条，部分重叠1条，相切／略内／略外／端点接触4条，无支持／合法空集2条，全无效mask-alpha模板1条，国际英尺矩形横跨块／美制测量英尺旋转剪切纵跨块2条。基线含直径、弦、闭合／自交线和模板外圆内段。核实际保存文件的顺序／多部／细分／模板忽略等价，以及重复／折返双倍贡献；每份仍逐像元独立oracle相等。固定完整圆面积，逐原始线段重数累计，相切或无支持为有效0，单位metre/m2。

半径null／true／false／0／负数／字符串／列表／对象8类；缺失几何／空线／Point／Polygon／GeometryCollection／整线零长／多部零长分量／范围内Z线／范围外Z线／范围外零长线10类；不一致／地理CRS2类，共20类×中英文＝40次准确density失败。先完整核源WKB／Schema／属性及CRS事实，异常格式归一化、读取或提交前置失败不得冒充算子拒绝；无法保真须报告，不换负例补数。准确核失败节点／原因／冻结提交语言，反向查询不改旧失败。

独立全线段2000位Decimal二次方程oracle，不复用产品索引／投影裁圆／根式区间或舍入助手。

数值极限、不可JSON数值、M维异常格式、故障注入及已有目标写入保护单列Native。

## 米制圆形点密度栅格独立 Hosted 验收

独立手工 `raster-point-density-persistence`；`make test-raster-point-density-online-runner`。准备、核验、归档数量和清理统一按[密度共同要求与矩阵](#点线密度专项共同要求)。

16条成功COG链：普通点集／倒序／整批三倍重复／像元中心重合点4条，半径略小于／等于／略大于边界3条，模板外支持／局部无支持／空点集3条，全无效模板／mask-alpha及缩放元数据模板2条，非方形横跨512块／旋转兼剪切纵跨512块2条，国际英尺／美制测量英尺2条。所有重复XY逐条计数；像元中心精确米制平方距离小于等于半径平方纳入、不设epsilon，固定完整圆面积分母、不做边缘补偿；模板只给网格和CRS，无支持处有效0、全部成功像元有效。实际保存文件核普通／逆序、普通／全无效模板、普通／mask-alpha相等，以及整批三倍重复与独立计数对应关系；各文件仍须全像元精确oracle相等。

半径null／true／false／0／负数／字符串／列表／对象8类，缺失几何／空Point／LineString／MultiPoint／范围内三维Point／范围外三维Point6类，点模板CRS不一致／地理CRS2类，共16类×中英文＝32次准确density失败。核正确失败节点／原始原因和提交语言冻结，反向查询不改旧错误；读取或提交前置失败不得冒充算子拒绝。

独立逐点Fraction oracle精确核全部距离和计数，以Float64 math.pi的精确有理值乘半径平方作为完整圆面积、密度一次舍入；不复用产品索引／距离助手。

非有限JSON、异常持久化格式、重复索引、数值极限和故障注入单列Native；不扩产品格式。

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

### 邻域多百分位独立保存验收

独立手工 `raster-focal-percentiles-persistence`；`make test-raster-focal-percentiles-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。 Runtime保持4 CPU／2 GiB／无额外swap。

16条COG链覆盖3／5方窗批量及逐项堆叠对照、两种方窗median、米制圆窗批量／堆叠／median、国际英尺／美制测量英尺圆窗、重分类、缺定位Int64／UInt64原生NoData邻接大整数、跨双块轴及全无效。请求列表固定[0,25,33.3,50,75,100]；逐项堆叠沿原空描述契约，批量波段严格核percentile描述和顺序。

28类参数／顺序／未适用列表／波段／窗口／圆窗失败各中英文提交，共56次，准确落在focal及对应原因，反向查询不改提交语言。每次终态重读所有先前成果的全部波段、mask和最近邻概览，共1032次成果全检／73次物理调用；独立逐像元邻居及Fraction线性插值oracle，实际批量与逐项堆叠／P50与median全量比较。定位缺失事实保持，物理／身份、当前实际源血缘／Monitor、源旧Meta及失败／全退出清理按共同要求。归档16份COG、全部源、冻结oracle及大小／hash，144份原始Develop／Monitor终态响应。

只读一次／排序一次、旧单值关键字移除、非有限JSON／元组／混合复数alpha及数值极限继续单列Native证据，不冒充Hosted。登记和本地回归不等于真实Hosted／Linux全量；实际运行由CI会话协调，不计UI、容量或吞吐。

### 栅格分区多百分位独立保存验收

手工套件 `raster-zonal-percentiles-persistence` 使用同源默认入口Runtime、NFS、普通User原12权限及4CPU／2GiB／无额外swap，固定600秒、不重试、不扩预算。16条CSV保存链覆盖原九列、[0,25,33.3,50,75,100]批量、六条逐项实际CSV对照、分区重分类、1025×513跨六块79分区、逆序同总体、原生Int64／UInt64及NoData邻接大整数、显式第二波段及双角色mask／alpha、无有效值、全无效分区。独立逐像元分组和Fraction插值核完整表／列顺序／精确类别及计数，批量与实际单项、原九列及逆序结果逐表对照。

40类参数／列表／波段／分类／网格／权重及频数冲突各中英文提交，共80次准确分析阶段失败，反向查询不改提交语言。每个终态重读全部先前实际CSV，共1416次全表检查／97次物理调用；归档16份CSV原字节、全部源、绑定源码／源／本轮路径的冻结oracle和大小／hash清单、192份Develop／Monitor原始终态响应。严格核声明目标／格式／写入方式、物理＝Meta正整数大小、扫描父执行／用户／租户／引擎、当前两源内部实际血缘与公开投影／Monitor、源及先前成果和Meta不变、失败无发布／新增扫描血缘及部分启动和全部退出清理；归档大小不冒充产品声明size_bytes。

共享一次排序／归并、数值极限、非有限JSON／元组／混合复数alpha单列Native证据。登记和本地回归不等于Linux全量或真实Hosted；实际运行由CI会话协调，不计UI、容量或吞吐。

## 矢量面分区多百分位独立保存验收

独立手工套件vector-zonal-percentiles-persistence复用NFS、同源默认入口Runtime、普通User原12权限和4CPU／2GiB／无额外swap，业务预算600秒、不重试、不扩预算。16条GeoPackage保存链：原八列、批量[0,25,33.3,50,75,100]、六条同总体实际单项对照、全部触及、自定义／空前缀、重分类接面化、Int64／UInt64原生NoData邻接有效值、无有效数值保留行及空矢量批完整Schema。主519×515旋转栅格跨两分块轴，显式第二波段／mask／alpha／原始存储值，面覆盖孔洞／重叠／多部件／细面／出界／重复几何／奇偶总体及单有效零；每面一行、完整属性／几何／行序／CRS、原八列及按请求顺序追加的nullable列对保持。

40类列表／元素／顺序／波段／复数与alpha／覆盖开关／前缀／首项与后项列冲突／CRS与网格／权重与面积及频数冲突各中英文提交，共80次准确analysis失败，反向查询不改提交语言。每个终态重读全部先前实际GeoPackage，共1416次全检／97次物理调用；独立像元面覆盖／标量排序／Fraction插值oracle，实际批量与六份实际单项／原八列逐表比较。归档16份原GeoPackage、全部源、绑定源／本轮／路径／夹具源码的冻结oracle及大小／hash清单、192份Develop／Monitor原始终态响应。

严格核真实矢量声明目标／类型／写入方式、物理＝Meta正整数大小、精确扫描父执行／用户／租户／引擎、当前实际输入内外血缘及Monitor、源与全部先前成果／Meta不变、失败零发布／新增扫描血缘和部分启动／全部退出零残留；存储FID不冒充用户索引，归档大小不冒充产品声明size_bytes。数值极限、重复索引／活动几何名、混合类型复数alpha、内部读取／排序／归并计数及故障注入继续单列Native。仅准备／登记／本地回归，不计实际Hosted或Linux全量、UI、容量；实际运行由CI会话协调。


## 顶点提取独立场景

用户确认新增 `vertex-extraction` case，沿 `vector-topology-persistence` 既有入口、Hosted 隔离生命周期、非默认租户普通 User 精确11功能权限及600秒总业务预算，仅一次成功与七次失败，不执行full、旧业务T4或容量。功能权限不替代正式资源授权；真实派发须等待共享血缘专项具备源／成果正式授权与必要证据，公共门禁仍归对应owner，不为本场景加权限、NFS例外或管理员业务调用。

同一成功DAG三条 `load→extract_vertices(id_field=feature_key)→save(mode=fail)` 分支，分别Point/MultiPoint、LineString/MultiLineString、Polygon/MultiPolygon来源，各输出3／8／20条Point，三个成果各自vertex_id从1连续，不将其当跨成果或跨执行身份。手写逐行XY与来源／部件／环／位置编号，保闭环首尾、连续重复坐标、重复位置、多面孔洞与原始方向；来源ID及全部可空Int64／Boolean／String属性保留，真实source_shape与业务geometry分离。读取本轮原文件用独立OGR原生坐标逐行核Schema/类型/值/CRS，不调用生产提取生成期望，不舍入或去重。

七失败为source_id、part_no、ring_no、vertex_no、vertex_id分别冲突，以及GeometryCollection、缺失CRS；固定v3诊断，冻结算子置tasks首位而load先调度，准确绑定定义节点、当前UUID、同User/Tenant，Develop/Monitor中英文页面与复制内容不泄漏私有字段／值／路径／原生异常；下游save不执行、无成果／扫描／输出facts／本次derive，失败前后物理及Meta／扫描库存不变。

三成果物理与Meta严格正整数大小相等、三自动扫描公开严格整数tenant_id/triggered_by同主体，稳定outputs/facts均create，三条对应来源→成果当前execution active derive及Monitor一致；保存预览原始EWKB／FID与业务ID、完整OL数据与Point蓝色像素、三幅地图及失败页。重合Point的数据行分别存在，不要求重复位置呈现分离符号。保留incomplete检查点与唯一最终报告、原始图／Meta／浏览器响应／原文件及前后SHA大小，最后隔离NFS/export/ownedroot/Runtime/registry/部署/Infra清0，不外推业务DELETE。

### 正交旋转网格邻近距离独立保存

`raster-proximity-orthogonal-persistence`复用Hosted、NFS、同源默认入口Runtime及普通用户原12权限，4CPU／2GiB／无额外swap；固定600秒，不重试、不扩预算。18条COG保存链覆盖原北向三单位对照、90／180／270度与斜向正交方形／非方形、真实最近目标反转、第二波段、零负目标、NoData／mask／alpha与跨块概览；30类各中英文共60次准确拒绝。每次成功复查全部已生成成果、每次失败复查18成果，共1251次。完整仿射／CRS／米制距离与mask由独立世界向量枚举oracle核验；严格核COG、artifact＝物理＝Meta正整数大小、扫描父执行／用户／租户／引擎、当前血缘／Monitor、源旧成果不变与零残留。归档18份原始COG和大小／SHA256清单及冻结预期；失败无发布／新增扫描血缘，反向查询不改提交语言。数值极限、私有故障注入和等距目标身份单列Native，不冒充Hosted。实际Linux／Hosted由CI会话协调，登记与本地通过不计真实验收通过。

### CSV/TSV GB18030 独立读取设置 case

阅读导航：[身份与来源准备](#gb18030-身份与来源准备) · [结构与内容投影](#gb18030-结构与内容投影) · [页面读取与原件](#gb18030-页面读取与原件) · [清理与能力边界](#gb18030-清理与能力边界) · [分层验证与历史证据](#gb18030-分层验证与历史证据)。

`delimited-read-settings` 按[公共准备](#文件读取-case-的公共准备与收尾)复用 NFS／MinIO。Common 唯一 Go CSV／TSV reader 负责产品读取，物理 worker 仅准备／核对／删除本轮源，不增加 Runtime 或解码路线。

#### GB18030 身份与来源准备

普通消费用户保持原十项功能权限，来源初始化用户仅在本 case 使用已批准的十三项，补现有 `meta.scan_task.execute`、`meta.scan_task.read`，不修改个人或生产账号。来源为各25行、含四字节字段名/数据及引号内CRLF的CSV和TSV，在NFS与MinIO各两份；先由来源初始化用户有界basic扫描，再授予已有精确单文件Grant。

#### GB18030 结构与内容投影

公共Meta详情只核对结构并明确不得包含行数、读取设置、源哈希及访问索引等内容事实；完整编码、表头、精确行数、源哈希与无源字节索引改由既有受保护Manager预览的 `data.item_meta` 核对，不扩大目录投影或权限。

#### GB18030 页面读取与原件

实际页面经原读取设置提交 `encoding=gb18030`；验证25行的20＋5分页、无表头26行的20＋6分页、首条记录保留、普通刷新继承、重开和恢复表头。

API与页面原文件按钮分别核对完整字节/大小/SHA256。

#### GB18030 清理与能力边界

沿[公共收尾](#文件读取-case-的公共准备与收尾)撤销 Grant、核元数据不可见及精确源零残留，再清外层部署／Infra；无派生产物，不替代 Transfer 批量 T4。

#### GB18030 分层验证与历史证据

标准门禁为 `make test-manager-online-runner`、受影响System Go T1及既有case选择／登记；不新建入口。

- [本地及Linux补证](../evidence/资源读取与预览历史证据.md#csv--tsv-的-gb18030-显式读取--本地门禁)：T1／T2／受控T3不等于Hosted；既有GBK结果不追认本case。
- [失败与修正](../evidence/资源读取与预览历史证据.md#gb18030-独立-hosted-的失败记录与-meta-完整名修正)：前三轮租户ID类型、公共投影误用、重复完整名，以及第四轮会话轮换后旧API凭据401均保留，修复／清理不改记通过。
- [第五轮正式结果](../evidence/资源读取与预览历史证据.md#gb18030-独立正式-hosted-通过与产物复核)：`b362628e725fc8e2fd95264b7bc06b941ea541c4`、Job `128749505328`，四源页面设置／分页／原件及完整清理；字库缺字、截图浮层及本地失败不删。

产物／各轮覆盖按所链证据认定，不认证编码写出、国家标准版本全符合或更多业务样本，SKP精确组成准入保持关闭。

### 正交旋转矩形DEM坡度独立 Hosted 验收

独立手工`raster-slope-orthogonal-persistence`，同源默认入口产品Runtime、Hosted/NFS、原普通业务User精确12权限，隔离来源准备者复用既有13项准备权限。首次来源扫描及精确限时metadata_read由准备者负责，业务执行及自动成果扫描由普通User负责；两类身份、来源／成果快照及各自完整扫描清单分别严格核验，撤权及全部清理不减。600秒业务预算包含来源扫描、独立参考准备、业务执行、每次全检及业务收尾；不重试、不扩预算。

八组各保存degree／percent两份COG，共16条成功链：北向矩形、一般正交旋转（与北向同轴长／同输入逐位对照）、90°、180°、国际英尺、美制测量英尺、跨512块的第二波段／NoData／内部mask／alpha／非有限值／合法零、全无效输入。独立Horn九点期望固定输入有效性、实际轴长与米制换算、既有GDAL Float32读取／结果舍入，另用解析平面及实际北向成果校验，不调用产品算子生成期望或现场放宽容差。完整COG像元／mask／六项仿射／CRS／单位／布局／实际nearest概览逐项核验；每次成功重检累计成果，18类各中英文提交一次后重检16份成果，共136＋36×16＝712次成果全检，53次物理调用（含首次参考准备，缓存清理另计）。

18类准确失败：剪切、一ULP近似正交、两种单轴镜像、地理坐标、缺CRS、缺定位、退化网格8类；波段0／越界／alpha3类；不支持高程单位／布尔单位／波段单位冲突／未知波段单位4类；scale／offset2类；非法坡度输出单位1类。必须验证实际坡度节点、提交语言及精确错误；反向查询不改写已存错误，拒绝零发布／零新增扫描和血缘，不以选源、准备或前置读取失败充数。复数及混合alpha、最小尺寸、数值极限和故障注入单列Native，不计本Hosted。

严格核artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、本次实际输入血缘及Monitor、源和先前成果原件与Meta不变；归档16份COG原字节、完整大小／SHA-256清单、冻结独立参考及必要终态证据。准备完成后固定完整业务源SHA并独立记录编排SHA／祖先断言；原固定90d63b065首跑42479552549在来源结构元数据读取阶段以AttributeError失败，16／36／712及16份原COG均无完成证据；原ZIP SHA-256 `ebd40c7f07bd9b5282df6654d53f1b1c427e67709d5b791fb8f673d9ce47a0b9`保留。读取器已按现行投影契约处理合法`spatial:null`并继续严格拒绝畸形节点／媒体内容／extent泄漏；缺CRS持久事实来自原始Meta日志，原HTTP体及异常堆栈未归档，根因由固定源码及本地真实读取器红绿复现确认，不伪称原始堆栈。原14个精确Grant已撤销、待收尾项为空；清理不抵消业务失败。修复固定dd201c02f的自然Platform Run42489265940／Job128757843713已独立核标准make test-platform及依赖链，本范围45项组合（含原3个本机超时用例）实际45 tests／6.092秒／OK，随后Go夹具T1通过。原本机隔离3个60秒超时记录保留且不计本机通过，Linux结果不解释其宿主根因；未加时或重复运行。固定 `dd201c02f` 的 [Hosted 复验 42549948422](https://github.com/pampa0629/addp/actions/runs/42549948422/job/128943056914) 在来源根扫描身份校验时以 KeyError 失败。源码检出、祖先校验及七个服务的同源核对通过，业务进度 16.399 秒、阶段为空，参考值和 16／36／712 矩阵均未执行，归档 0 份 COG。19 个精确 Grant 已撤销并核隐藏，无待清理项。原 ZIP SHA-256 为 `4e01c295359446dab418bee63b03a92e5dfe35cac27d4f5facec66bb63860288`。栈、固定源码与回归复现指向根扫描省略父执行字段时的直接索引，原 HTTP 体未归档。共享校验器现将通过校验的无父执行值写为 null，自动成果扫描仍严格校验实际父执行、用户、租户及引擎。固定 `55fb679016e1397045735ff8f3f4078accc42cf4` 的[标准 Platform 门禁](https://github.com/pampa0629/addp/actions/runs/42924607122/job/130207746240)已核原始完整日志与 Make 依赖链，实际执行坡度组合 45 项／6.236 秒／OK，后续隔离身份 Go T1 通过；根扫描父执行字段修复及两项回归文件与 `80637f0a9` 逐字节一致。原日志 SHA-256 为 `329e3bfec7c7f1812ea684eabfd270a15853f9fbdd829b351b0974d29edf64e4`，此证据按该固定源码的 Linux Runner 范围记录。Hosted 仍待验。

### Feather V2 基础标量独立 Hosted

阅读导航：[身份与基础标量](#feather-身份与基础标量) · [时间字段](#feather-时间字段) · [日期与定点小数](#feather-日期与定点小数) · [清理与未覆盖项](#feather-清理与未覆盖项) · [分版本证据](#feather-分版本证据)。各扩展共用 `native-table-feather`，实际通过范围按对应固定版本记录判定。

#### Feather 身份与基础标量

`native-table-feather` 复用 NFS／MinIO、GeoPython 与 Transfer；消费十一项、来源准备十三项，沿[公共准备与身份差异](#文件读取-case-的公共准备与收尾)，不改变个人账号权限。

各存储三种codec（无压缩、LZ4、ZSTD），每源2503行、多个Arrow record batch，覆盖四种有符号整数、bool、float32／64、Unicode文本、binary、nullable及空值差异。先由来源准备者有界deep扫描，再建立既有精确单文件元数据Grant；公共Meta只核结构，受保护Manager预览核编码profile、真实总数、源版本。API页1／2／126及真实页面下一页保留响应和截图。

三份 MinIO 原件仍核对 API 与真实下载按钮的字节。按现行 Manager 源读取契约，三份 NFS 原件逐一核对下载 HTTP 403、JSON 错误且无附件，以及页面下载能力关闭；仍完整执行 NFS 预览和 Transfer。Owner 和浏览器回执升级为 v2，分别记录两种存储的下载结果。

普通User创建实际Transfer源→精确Parquet成果，独立物理reader完整核对15018行、字段类型／nullable／数值，归档六份成果；不由页样本替代连续读取。

#### Feather 时间字段

时间字段沿用该 case、六份来源、既有权限及清理。Meta 核对单位、源时区和可空性，Manager 分页核对完整 ISO 文本，Transfer 的六份物理 Parquet 成果独立回读逻辑类型、原始整数与 NULL。样例覆盖秒时刻、日末纳秒、本地时间戳、纪元前及最小纳秒计数、纽约夏令时回拨时的两个瞬间和固定偏移。秒转毫秒溢出在确定性门禁单独验证。

#### Feather 日期与定点小数

Feather 日期与定点小数扩展复用同一 Hosted 场景和六份来源。Meta 与 Manager 须核对原生日期类型、Decimal128/256 的精度、小数位数及可空性。API 与实际翻页须逐值比较日期及定点文本，含纪元前日期、闰日、空值和尾零。Transfer 产物须由独立 Parquet reader 逐行检查 DATE/DECIMAL 逻辑类型、精度、小数位数及值。

#### Feather 清理与未覆盖项

沿[公共收尾与分层判定](#文件读取-case-的公共准备与收尾)，核六源字节不变、撤 Grant 后不可见、六任务删除／404、六物理源及六成果／目标标记删除；外层单独报告。源变化／不支持类型／原生预算拒绝单列确定性与 Native，不替代跨模块权限或容量。无派生缓存或新增内容授权准入；Feather V1、IPC stream、unsigned／嵌套／dictionary／duration／写出未支持，SKP 精确组成准入保持关闭。

#### Feather 分版本证据

现行断言见以上各主题；固定源码、实际 Job、失败与修复、清理结果及未覆盖项保留在以下证据正文，不在本节重复维护：

- [基础标量 Hosted 证据](../evidence/资源读取与预览历史证据.md#feather-v2-基础标量只读--独立-hosted-t4)：保留首轮正负零 oracle 失败、Transfer preflight 缺口及修复后结果；当时两种来源均下载的记录不替代现行 NFS 下载拒绝断言。
- [日期与定点小数 Hosted 证据](../evidence/资源读取与预览历史证据.md#feather-v2-日期和定点小数)：仅按该轮 DATE／DECIMAL 范围判定，保留相关 CI 失败，不追认时间戳转存。
- [时刻与时间戳 Hosted 证据](../evidence/资源读取与预览历史证据.md#feather-v2-时刻与时间戳--正式-hosted)：区分首轮 NFS 下载 403 后退出与修订下载断言后的正式复验，保留首轮缺少页面和物理 Parquet 回读的边界。

本地确定性、Linux 原生门禁和真实 Hosted 结果分别按证据中的实际覆盖范围使用；清理通过不抵消业务失败，历史基础版本通过不代表后续类型扩展已通过。

### 面邻检查独立普通用户场景（polygon-adjacency）

复用 `vector-topology-persistence` 唯一 Hosted 入口，`topology_case=polygon-adjacency` 仅运行一次成功、四次只读失败及一份线成果，不运行旧整包业务或容量场景。先完成脚本和确定性验证；输入五份 DataItem 可读、唯一 outputs 成果创建批准、源及成果元数据/血缘可见的正式授权与部署证据具备后，才允许单次真实派发。权限最小单元仍为 DataItem，不对内部文件组成另授权限；本场景不增人员权限、不代替专项授权，也不为派发绕过关闭的公共保存准入。

成功源含12条 EPSG:3857 有效面，使用非默认活动几何列 source_shape、未排序整型来源 parcel_key，以及同名业务 geometry 和 nullable note 字段。只输出5对 MultiLineString：普通邻边两对、孔洞共边一对、两部件合并成一条来源对一对、长度严格为 2^-40 的微小共边一对；点接触和微小缝隙不报告。物理 oracle 用手写坐标及 OGR 原生点重载，严格检查 ID 类型、原行顺序、完整部件、长度、CRS，不调用生产算法生成期望，不使用 epsilon 或十进制舍入。成果 adjacency-lines.gpkg 仅含 source_id_a、source_id_b、shared_length 和真实几何；FID 与业务 ID 分开核对。源属性不复制到成果，五源字节及 Meta 前后保持不变。

EMPTY 坏源必须以明确的 OGR 空 Polygon 构造，重载仍严格区分 EMPTY 与 NULL；GeoJSON 空坐标在 CI 的 GDAL 3.8.4 会解析成 NULL，不能据此替代空面或放宽物理断言。

差集与面邻共用精确声明成果的原件捕获：业务异常退出时先捕获再撤权，标准夹具退出在删除 owned root 前再次捕获，两处与物理校验前归档复用同一实现。只读取本轮已核归属 root 内本 case 的唯一目标，不遍历目录、不调用校验算法、补扫、补 Grant 或重算；每份观察独立保存原始字节、大小和 SHA-256，明确区分缺失、空文件、非普通文件及读写失败，不覆盖既有证据。捕获不证明成果有效，不能放宽原物理、Meta、完整组成、血缘、页面或拒绝正文保护断言。首个业务异常始终保留为主要失败，取证和清理错误分别记录；取证失败仍执行原清理，无法确认的取证、停止或删除如实记失败或待处理，不计为零残留。

四份独立坏源分别验证重复 ID、EMPTY 面、地理 CRS、微小正面积重叠，诊断依次为 operator_input_invalid/input_gdf、operator_input_invalid/input_gdf、operator_crs_invalid/无参数、operator_input_overlap/input_gdf，均无 field。失败 DAG 仅 load 与检查，不申请失败保存目标；断言无成果、扫描、产出事实或本次 derive、扫描库存和物理目录不变，并在 Develop/Monitor 中英文实页核对固定原因与复制内容，不泄漏字段、坐标或路径。不把本场景只读失败称作实际执行了下游保存阻断，后者保留既有 Native/DAG 证据。

正式脚本必须与差集共用同一文件产出准备适配：独立初始化员对五源逐个 basic识别→精确read Grant→deep物理检查；普通User扫描库存初始为空。成功提交原execution后，仅经既有审批上下文GET核对精确目标并单次批准，结果未知不重发；自动basic成果扫描和初始read生效先核，后由初始化员deep检查。初始化员11次准备扫描与执行用户1次自动扫描分开核对，撤源Grant后物化成果仍可读，再撤成果Grant。没有独立初始化凭据或case不匹配必须在业务提交前拒绝，不回退旧扫描路径。NFS和Native夹具都准备原0700私有暂存目录，权限扩大或残留仍拒绝。System 专用验收夹具仅对 `geometry-difference` 与 `polygon-adjacency` 选择同一既有源初始化／批准准备策略：原引擎授权管理角色的11项权限，加原 Meta 采集执行／读取和精确保存批准资格，共14项；普通执行者仍为原11项且不含 Grant 创建或保存批准资格。两类 case 均经同一临时账号创建入口和原 AuthContext 严格核验，不新增 Permission、角色、迁移或数据授权边界，无关 case 不获得该准备批准能力，既有独立血缘专项保持原范围。此处接线只完善受控准备策略，标准和 CI 通过不替代真实身份／Grant／审批／发布验收；实际单次面邻 T4 沿来源会话已批准的范围由其审核后执行，不由专项代码交付自动派发。

成功保存采用原协议自动基础扫描及成果初始只读授权；预览所需深扫如适用必须单独保留证据，不手工补扫或发 Grant 冒充自动阶段。严格核对正整数 physical==Meta 大小、公开 tenant_id/triggered_by 同一普通 User/非默认 Tenant、create 事实及 Monitor。面邻双方 ID 来自同一源，持久血缘只应有源 DataItem→线成果一条当前 execution active derive；不是双源血缘。浏览器核5条实际 EWKB/OL线坐标与属性、可见共边像素及截图；微小共边仅保证物理/API/OL数据精度，不要求亚像素边独立可见。读取导航当次实际请求绑定身份，禁写监听，完整失败页与原始 Meta/图/预览归档。最终再核字节、扫描库存及 owned NFS/Runtime/部署/Infra 零残留；阶段 incomplete 不能代替最终通过，清理不认证业务 DELETE。

标准验证沿 `make test-vector-topology-online-runner`、定向 `make test-geopython-workflow GEOPYTHON_WORKFLOW_TEST_ARGS='-n 0 -k adjacency_t4'` 及现有 Console 浏览器发现/映射核验。既有 Platform 登记、Console 与 GeoPython CI 同次登记覆盖；不新增 suite、Job、服务或权限。本段是待验收约定，不代表真实 T4 已通过。
