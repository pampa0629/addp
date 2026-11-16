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


### 多距缓冲独立场景

用户确认新增 `topology_case=multi-distance-buffer`，复用 `vector-topology-persistence` 原Hosted生命周期、非默认Tenant普通User精确11权限及600秒预算。仅生成两个EPSG:3857点图层：成功源含两个不同业务ID、非标准活动几何列及可空Integer64/Boolean/String；冲突源另含 `distance` 属性列。成功DAG只执行 `load → batch_buffer(distances=[2,1,2], resolution=1) → save(mode=fail)`，保存单个六行GPKG，完整缓冲而非环带，顺序按距离再源行，重复距离保留。原 full、重合规整和组合case不变，不运行旧业务T4、容量矩阵或受控对照。

独立OGR按原生点核六个闭合菱形的精确坐标/方向/顺序、包络、面积、顶点半径、二维有效性、源CRS、Integer64/Boolean/NULL与Real distance；不调用不同版本GEOS重新buffer构造期望，不增加epsilon、舍入或精度网格。原始源及成果字节全量归档，物理大小与Meta为相等严格正整数。成功成果恰有一次同User/Tenant自动扫描、源到成果的一条当前execution active derive，outputs/facts均create且Monitor一致。两源各做一次初始扫描，不把冲突源写入成功执行输入血缘。

同User另运行冲突源的DAG，必须在batch_buffer节点以准确distance字段冲突原因失败，下游无文件、无新增扫描或输出facts；两源Meta、源文件和成功成果均不变。不宣称保存跨文件事务。真实Console核成功页、六面地图、重名失败页共三截图，标准EWKB/业务ID与FID区别/全部属性/OL投影数组/像素均须通过；每次导航读取页面自身响应绑定原User/Tenant/11权限，不沿用旧Token。Owner/browser报告为 `addp.topology-buffer/v1` / `addp.topology-buffer-browser/v1`，绑定case/run/身份/当前执行/完整预期；原始预览和血缘图保留，incomplete阶段不代替通过。最终源及成果未变、领域/NFS/export/owned root/Runtime/registry/部署/Infra完整清理零残留；清理不认证业务DELETE。

同次沿既有标准runner、Native、Console和workflow登记及mutation回归覆盖新增case，无新suite、权限、产品路径或永久脚本。Native/CI通过不替代这一新增普通用户T4；超JSON安全整数、零负距离、空图层与其他参数边界复用已有确定性证据，不外推为本case新增真实认证。


### 重合规整独立场景

用户已确认 P0＋P1：`vector-topology-persistence` 新增显式 `topology_case=line-network-normalize`，保留原 `full` 的原业务含义。新场景只生成一个 EPSG:3857 GPKG 源，执行 `load → normalize_line_network(id_field=route_key) → result/members 各自 save(mode=fail)`；不 seed 或执行旧十五源／十五成果场景，不调用旧塌缩负例或容量矩阵。未知 case 在副作用前拒绝；现有根入口、Hosted 生命周期、同源默认入口 Runtime、非默认 Tenant、普通 User 精确11权限与600秒预算不变。Case 由 workflow 输入贯穿 owner、物理夹具、浏览器和报告，不能把本场景成功记为旧整包已重验。Playwright 筛选针对包含文件名前缀的完整标题，按完整用例名的末尾精确匹配；标准 runner 使用真实 `--list` 分别证明 normalize／full 各只选中目标一项，列表验证不执行业务；平台 CI 通过现有前端准备 action 安装 Console／Manager 的已锁定 npm 依赖，不需要安装浏览器或启动服务。

源的五个业务 ID 为显式字符串，与存储 FID 区分；覆盖折线原顶点／连续重复坐标、反向部分重合、十字交会、同源多部件多次经过和不同采样顶点。固定手写预期为九条唯一 LineString、十七条经过明细。唯一边只含几何和 Integer64 edge_id／occurrence_count；明细保留全部业务列及 null、Integer64 grade、Boolean enabled，追加来源／部件／原段／片段序号及 Boolean is_reversed。按精确原坐标、方向、行序与关联逐项核验，不用生产算法生成预期、舍入或添加容差。旧 Native 保存断言必须收紧为原 Integer64 物理类型，不再按输入 dtype 接受浮点。

双保存均使用既有授权与成果主线，稳定 outputs 和 facts 各两份 write_mode=create；各成果恰有同一普通 User／Tenant 的一次精确自动扫描、正整数 physical==Meta 大小以及源到该成果的一条当前 execution active derive。独立原生 OGR 读原始几何／字段类型／CRS，归档一个源与两成果原字节并核浏览器前后不变。两幅真实预览／OpenLayers 地图核全部九／十七行属性、标准 EWKB、FID 区分、EPSG:3857 原坐标的显示投影、要素数、绿色像素与截图。源 Meta 保持不变，Monitor 与执行 facts 一致；业务来源只由 members 表达，不塞入 Meta 技术血缘。

同源另提交一次引用不存在来源列的失败 DAG，Develop 与 Monitor 必须公开一致的 `develop.workflow.invalid_params`，诊断绑定定义顺序第 2 个节点 `normalize_line_network` 的 `operator_input_invalid/input_gdf`，不依赖运行日志的自由文本。两个下游 save 都不发布、无新增扫描或输出 facts，原源／成功成果不变；不承诺保存阶段跨文件事务回滚。浏览器核同 User 的成功／失败执行页与两幅地图，共四截图。阶段证据 result=incomplete 不替代最终 `addp.topology-normalize/v1` 和 `addp.topology-normalize-browser/v1` 的 passed；两者显式绑定 case、Run ID、身份、当前执行、源和两端口预期。原始图／预览／OL 与完整物理证据须保留；领域、NFS export／owned root、Runtime／registry、部署／Infra 全部清理零残留，清理不认证业务 DELETE。新场景沿现有标准 runner、Native、Console 及 workflow 登记检查，不新增 suite、权限或容量运行。


### 重合规整与链路合并独立组合

用户确认新增 `topology_case=line-network-normalize-chains`，沿现有 vector-topology-persistence 标准入口与 Hosted 生命周期，只运行一个 EPSG:3857 GPKG 源的 `load → normalize_line_network → merge_line_chains(id_field=edge_id, match_fields=[])`；三份既有 save 分别保存 chains、chain_edges、edge_sources，全部显式依赖合并成功。原 full 与 line-network-normalize 含义不变，不运行旧整包或容量。非默认 Tenant、普通 User 精确11权限和600秒预算不变，无新算子/API/权限。

夹具11条源要素，独立手写期望为9条链、15条唯一边明细、22条原始经过明细，覆盖共享边、四种方向、分支、闭环、多部件重复、微小断口及零长度原段。业务来源ID用超过32位且在JSON安全整数范围内的2^40级Int64，NULL/Boolean、原业务chain_id/member_no必须保留；Native既有2^60级回归不外推为浏览器超安全整数认证。按同次edge_id核两级关系及方向异或，计数按唯一边与原经过分别核；原始字段不放到主链，业务来源关系不塞进Meta技术血缘。

每成果必须有一次同User/Tenant自动扫描、严格正整数physical==Meta大小、源到成果的一条当前execution active derive和一致Monitor；三幅真实LineString地图须全量核9/15/22条标准EWKB、字段/原生类型、FID区别、原坐标/OL投影及像素。归档一源三成果原始字节和三份原图/源Meta/原始预览。成功页、三图及失败页共五截图；浏览器前后源文件、源Meta和成果都不变。

同源另提交一次引用不存在的合并来源列的失败 DAG。Develop 与 Monitor 必须公开一致的 `develop.workflow.invalid_params`，绑定定义顺序第 3 个节点 `merge_line_chains` 的 `operator_input_invalid/input_gdf`。三个下游 save 无发布、无新增扫描和输出 facts；不承诺保存阶段跨文件事务回滚。最终Owner/browser报告绑定独立case、同run/身份/执行/三份精确期望；incomplete仅阶段证据。领域、NFS/export/owned root、Runtime/registry、部署/Infra完整清理零残留，不认证业务DELETE。新增case同次接入既有runner、Native、Console、workflow登记及mutation回归；代码或本地通过不计真实T4通过。

2026-10-27，提交 `afc6a18aa01338850f690083bc4ba8834ae0d1f6` 的 [独立组合 Hosted T4 40501760127](https://github.com/pampa0629/addp/actions/runs/40501760127/job/122180927931) 已真实通过：User33／Tenant2／精确11权限，11源要素得到9链／15唯一边／22原始经过；三份98304字节成果的物理大小与Meta一致，三次公开同主体自动扫描、三条当前execution active derive及Monitor一致。归档一源三成果原始字节，独立OGR按原生坐标／类型与精确分数重建来源、片段及四种方向组合；原始EWKB、实际OL数组、三图像素和成功／失败页五截图均核对，合并缺失字段负例无发布／扫描／facts，源和成果前后不变。业务门禁31562ms，NFS/export/owned root、Runtime/registry、部署及Infra清理零残留；不认证业务DELETE，扫描库存和逐页身份为同轮实际断言，原始库存/AuthContext响应未单独归档，不宣称亚像素断口肉眼可见或超JSON安全整数保真。精确同提交 [Console CI](https://github.com/pampa0629/addp/actions/runs/40501463449/job/122179912108) 195单测／233浏览器／build通过，[GeoPython CI](https://github.com/pampa0629/addp/actions/runs/40501463449/job/122179912720) 7622项／24条警告通过；[平台一致性](https://github.com/pampa0629/addp/actions/runs/40501463449/job/122179911829) 因既有Catalog登记函数在四个合成仓库读取缺失Makefile失败，已交公共owner，不能记为整个Platform通过。本轮未重复旧业务T4或容量，组合容量仍未验。

## manager-hybrid-search

Manager 混合检索类 T4 使用同一专用 Business MinIO Fixture 和永久 MinIO Engine Instance，Fixture owner 额外幂等写入仓库内确定性 JPG。专用 Tenant 必须通过正式 Inference 与 Manager 配置 API 预置支持文本和图片、维度为 2560 的 Model Profile 及 `semantic_search_embedding` 场景绑定；Provider Credential 只保存在 Inference 加密存储，Online 环境只声明预期 Model Profile ID。suite 不创建、更新或删除 Provider、Deployment、Model Profile、Credential 和场景绑定，而经 Gateway 触发真实 Meta scan 与 Manager ad-hoc embedding，验证 Inference、pgvector、Meilisearch 和 Manager 对称 RRF 融合的完整链路：同一 `document_id` 的关键词与向量候选只能返回一次，纯向量候选必须在融合后参与统一分页，响应不得包含旧 `vector_hits`。每轮 embedding 结果必须通过 Manager 正式 API 删除并确认零残留；新增 suite 只登记手工 `workflow_dispatch`，首次真实通过前不得加入夜间调度。

## vector-pmtiles-workflow

矢量 PMTiles T4 `vector-pmtiles-workflow` 仅手工在 GitHub Hosted `ubuntu-24.04` x86_64 独占部署运行，复用共享 Hosted 生命周期、标准 GeoPython 产品镜像和独立 source／target tmpfs MinIO。必须先启动 Security，preflight 同提交核验 Security、Develop、Meta、Manager 和 Monitor；身份夹具通过统一 `--wait-protection-owners develop,manager` 在导出凭据前只读观察该租户两个 Owner 的持久首次同步状态，最多等待 60 秒；读取错误或超时立即停止并清理，不写入 checkpoint，不绕过保护门禁或重试业务执行。非默认租户普通 User 经公开 Develop API 新建两点 GeoJSON 的纯几何 PMTiles，再用 50 面 Shapefile 和显式 name／rank 字段替换同一目标；独立 GDAL oracle 核对 PMTiles v3、gzip MVT、层名、业务字段、数量及按瓦片量化误差约束的坐标。重复 create 必须返回受控 target_exists，且文件哈希／大小／ETag／修改时间、源对象、扫描记录、DataItem 详情及当前血缘完全不变。replace 必须保留目标 ID／指纹并更新文件内容、自动扫描和唯一当前 derive 来源，不把当前图当作历史 observation 证明。每次成功均核对物理大小与 DataItem、原生 header_hash 与 Meta 格式事实，并由同一 User 真实登录 Console，检查 min_zoom=0／max_zoom=5、no-store 请求与响应、瓦片内容哈希、页面解码要素数及地图绘制。失败或成功退出均清理当次应用、业务容器、产品 Runtime／镜像、registry、Infra 数据卷和凭据；清理失败使门禁失败。标准离线入口为 `make test-vector-pmtiles-online-runner`，接入 `test-online-runner`；真实 GDAL oracle 用例接入既有 `make test-geopython-workflow`，由现有 Platform CI 调用，suite／CI／身份／清理登记由 Online 注册检查覆盖。提交 `3eed180b927916d76f1385e63cf8b213d32149a2` 的 [Hosted T4 39037436667](https://github.com/pampa0629/addp/actions/runs/39037436667/job/117410000383) 已通过上述完整 create／冲突保护／replace 场景：两轮地图分别显示 2 点与 50 面，目标身份稳定，自动扫描和当前血缘换源。7 个服务预检同提交，Runtime 留存同次源码构建、实际镜像 ID 与默认入口核验记录。清理证据仅覆盖当次隔离部署及 Infra 零残留，不代表 Manager 受管结果 DELETE API；T4 瓦片留痕为 HTTP 200／no-store／内容哈希，未单独采集 S3 HTTP Range 请求日志，Range 边界另有 Manager T1 回归。不登记 schedule，不覆盖新增 NFS／外部云 S3 认证、历史血缘、容量或十类全算子认证。

## raster-workflow

### 镶嵌 whole 的 basic 重扫收敛

既有 `raster-workflow-capacity` Hosted T4 在四个 Manager 镶嵌成果的自动扫描、物理 oracle 和 Monitor 校验分别通过后，对每个成果自身 prefix 连续执行两次 `basic / force=false` 定向扫描；不新增 suite、消费者功能权限、Runtime、容量组或预算。自动扫描 28 次与新增手工重扫 8 次分别计数，不用手工重扫补救自动链路。

Meta 夹具仅在当轮 Hosted 独占 `addp_online`、同一非默认 Tenant／普通 User 的成功 Manager 父执行及真实 whole 身份均匹配时，向该 whole 内部准备两个错误组件项和一个子 prefix，模拟旧扫描碎片。夹具不删除记录、不修改源对象或 Grant；生产扫描负责清理。首轮须物理删除这些记录（含软删残留），第二轮须保持内部节点／数据项为零。两轮均以只读数据库快照核对 whole 的 item_id、fingerprint、node_id、真实名称／完整路径和扫描深度稳定，范围外原身份不变；同一普通 User 的正式资源树 API 须始终将 whole 展示为同一路径上的单一叶子，不暴露内部组件或同路径普通目录。保存扫描 UUID、终态／范围、数据库快照和资源树原始回执；失败时保留已有阶段证据，原有策略恢复与部署清理继续执行。夹具拒绝个人环境、其他 suite、容器重建／重启、身份或目标漂移，不提供产品旁路。该扩展须经既有 Runner 确定性门禁和一次实际 Hosted 终态验证，代码与本地通过不计为 T4 通过。2026-11-04 确认前置适配：复用 System 标准独立授权初始化 User，消费者仍保留原精确 12 项功能权限；仅为本轮五个已扫描源 TIFF 及尚不可见的精确成果签发 metadata_read，采用 at_time，期限不超过原 10800 秒业务预算，退出逐一撤销并核重新不可见。元数据 Grant 不充当内容读取许可，执行仍由既有 ExecutionAuthorization 与产品入口约束。旧大小／范围／格式断言转为绑定本轮成功父执行、原自动扫描、Tenant／User／Engine／精确成果的有界数据库只读快照；公开 Meta 仅核身份与结构投影，不恢复内容字段。原始内部／公开血缘分别核验，内部加工事实不要求公开接口返回。源初始化 Token 不传给 Runtime／业务服务或浏览器，不写入证据；补齐既有 Security 启动依赖，所有成功、失败和未知 Grant 写入结果均进入原退出清理。 授权夹具必须从 System 可信授权事实读取真实 layout，普通 TIFF 严格核对 single／multi，镶嵌成果严格核对 whole；不按文件数量、扩展名或默认值推断，不将 multi 改写为 single。观察与签发前重核均保留有界、按字段白名单脱敏的授权事实（包括不完整和不匹配响应）、实际／允许 layout、校验阶段与稳定错误码；未知字段或异常字符串仅留摘要，不归档连接信息、凭据或原始异常文本。事实身份、类型化目标、边界、basis_state、摘要及到期约束仍须全部匹配后才签发 metadata_read。失败证据先于退出清理保存，清理不得覆盖首个业务失败；未知写入结果仍保留待撤销命令，清理失败单列。


受限快照查询分别标识 execution、produced_item、whole_state、seed_fragments 操作，以及归属核验、执行、返回边界、JSON 解码、形态和公开身份校验阶段。仅留稳定错误码、精确提取的五位 SQLSTATE（无法取得时明确 unavailable）、退出码、返回字节数／行数／JSON 类型和摘要，不归档原始 SQL、参数、stdout／stderr、连接或凭据；失败证据先于授权和环境清理保存。查询仍固定绑定本轮部署、User／Tenant／父执行／扫描／成果身份，保留原超时、大小和只读边界。确定性 Runner 门禁覆盖诊断与脱敏；真实 SQL 通过既有 `make test-common-postgres COMMON_POSTGRES_TEST_ARGS='--test online-capacity-snapshots'` 分组复验，使用会话临时表和退出自动清理。本地仅接入经核验的 addp_test；CI 沿既有 Common PostgreSQL Service 和默认完整门禁运行，不新增 Hosted 调度或产品入口。

执行快照的血缘比对须适配专业执行 DTO：只取其 metadata.lineage_facts 与本轮有界内部事实比较，Develop 专业读取允许的 result 等字段不传入 Monitor DTO 校验器；真实 Monitor 响应仍按完整观察 DTO 严格校验。Develop 专业响应及真实 Monitor 沿现有资源投影约定核对，operations 保持空值，schema_snapshot 不公开。Manager 专业响应先复用现有完整 DTO 白名单校验，按其已声明的 kind／operator／input_ports／output_ports 与内部操作事实投影逐项、按序比较，保留 Go omitempty 语义；未知操作字段、内部 field_mappings／schema_snapshot 及资源身份差异均须失败。Manager 操作匹配后，仅在资源比对视图中置空 operations 以复用现有资源身份／locator 校验；不修改原响应或内部事实，不在操作匹配前丢弃字段，不新增公开接口或权限，也不重新取数掩盖差异。capacity-snapshots.json 同时保留每次投影核验的结果、闭合原因码、字段存在性／类型／摘要及有界差异位置；未知字段名和任何字段值仅留摘要，禁止留原始资源标识、路径、行值、SQL、凭据或异常正文。失败证据须先于原授权与环境清理保存，保持首个业务失败及独立清理结果。

普通 PNG／JPEG 图片导出沿用本套件、同源产品 Runtime、原有普通 User 权限和清理入口，不另设 suite。完整既有场景之后，经真实 Develop 页面导入双节点工作流、选择编码（JPEG 显式质量 85）、首次保存、重开及执行，再在 Monitor 核对成功及目标资源。物理夹具独立读取 37×23 的 RGBA PNG／RGB JPEG：PNG 核完整像元和透明覆盖，JPEG 完整解码并按平滑合成源核误差（平均绝对误差 <5，最大 <15；非通用质量承诺），同时核原始／解码哈希、无 CRS／网格／GCP／RPC／EXIF／旁文件、源及既有成果不变。必须核精确输出、自动扫描、三方大小、保存任务删除和共享部署／物理对象／Infra 清理；页面生成不等同浏览器下载或 GIS 定位交付。确定性入口 `make test-geopython-workflow`、`make test-module MODULE=develop GO_ONLY=1`、`make test-develop-frontend` 和 `make test-raster-online-runner`；正式结果须另记实际 SHA 与原始产物。

### 部署与主场景

阅读导航：[运行准入](#栅格主场景运行准入) · [计算矩阵](#栅格主场景计算矩阵) · [元数据授权](#栅格主场景元数据授权与事实取证) · [失败补偿](#栅格主场景失败补偿与退出) · [固定结果](#栅格主场景固定结果与验收边界)。本节定义 `raster-workflow` 主场景；独立分支与页面构图仍见[下节](#多来源计算与页面构图)。

#### 栅格主场景运行准入

- **环境与产品身份**：仅 GitHub Hosted `ubuntu-24.04` x86_64 独占部署，复用 `scripts/utils/hosted-online.sh`。根 `make build-images` 构建 GeoPython 产品镜像，默认入口启动独立 `addp-raster-online-runtime`；核实际镜像 ID、默认 command／提交并留 Runtime 日志，不走开发启动。样例生成和物理校验通过 `ADDP_ONLINE_RASTER_RUNTIME_IMAGE` 使用同一已核产品镜像；缺参数须在 Docker 前拒绝，不回退开发镜像。
- **夹具与身份**：Business 管理两套无持久卷、凭据不同的 MinIO，生成 EPSG:4326、确定性双波段／NoData TIFF；System-owned IAM helper 建非默认 Tenant／普通 User，独立 Provisioner 经正式 API 注册两个业务 Engine。浏览器与 API 必须是同一普通 User，凭据只在 owner-only runner.temp，不进业务进程环境或证据。
- **唯一预算**：入口设置 preflight＋业务总计 **1740 秒**，涵盖下列矩阵、页面和原有业务收尾；不改变其他场景或夜间预算。首次真实通过前只登记手工 workflow_dispatch。当前完整 `full` 保留全部 **71 项**主场景浏览器观察、DEM、普通图片及分支阶段；min/max 扩展阶段的48轮记录不替代完整矩阵。
- **登录等待**：依据精确目标 URL、实际租户选择或 MFA 状态，不以等待不存在选择框超时推进，不跳过当前身份／权限核验。

#### 栅格主场景计算矩阵

| 路径 | 独有断言 |
| --- | --- |
| 基础保存 | 普通 User 经 Develop 提交 `raster_load → raster_band_math → raster_save(profile=cog)`，验证 create、重复create失败及replace；独立重读完整像元、CRS、transform、NoData与COG布局。 |
| 重投影与镶嵌 | 确定性源重投影到EPSG:3857的1米网格，裁剪两个重叠区，分别验证first/last镶嵌的全部像元、NoData、空间属性及原成果不变。 |
| 多波段／缺值 | 各波段缺值位置不同，含合法零及部分alpha，分别覆盖NaN／有限NoData。有限NoData的平均插值保留各波段原声明和缺值；波段计算保存重读声明NaN并保持独立有效性。尺寸重采样后存COG，经正式ResourceLocator重读计算 `b1+b2`，逐波段核全部像元、NoData、alpha及联合有效性。 |
| 平均插值 | 非均匀源像元、波段独立缺值及透明干扰值，覆盖整倍数／非整数缩放；以源目标像元矩形面积交集独立核边界权重，不能用等权均值。保存重读相加使用各波段自己的均值，不提前合并源有效性。 |
| 双线性插值 | 按各波段自己的有效中心／邻域权重核验，中心无效不能由邻域填补；放大COG的overview与Meta轻量探测一致，保存重读相加仍保持各波段有效性。 |
| min/max聚合 | 复用第2数据波段、3×5分组及nearest金字塔的单一DAG，逐像元核NoData排除、内部／边缘不足整组的数值、CRS／完整仿射及金字塔；artifact＝物理对象＝Meta大小，均为严格正整数。两个新增成果／两个浏览器场景及原成功失败场景均保留；实际通过范围按固定版本认定。 |

所有成果共同核验：

- Classic TIFF COG 的 `profile=cog`、`is_cloud_optimized=true`、`cog_check_level=heuristic`、`is_tiled=true`，`has_overviews`须与独立物理检查一致。overview是独立结构事实，不能推导或否定COG profile，更不能补造；Meta有界提示不替代GDAL完整布局／像元检查。
- 目标 DataItem 仅由 Develop 自动提交 Meta scan，血缘仅由 owner 自动采集，不手扫目标或调用collect掩盖链路缺陷。逐成果核自动扫描／派生血缘，Monitor API与Console显示同一成功执行和源目标关系。
- 每轮重读源及全部先前／当前成果，核像元、掩膜、布局、大小与源对象哈希不变。独立物理oracle只在同一进程复用已算单波段期望元组，推导平均插值覆盖alpha／联合结果及双线性联合结果；不缓存实际对象、GDAL读取、哈希或校验结论。

#### 栅格主场景元数据授权与事实取证

完整 `full` 的三个源Engine扫描成功后，由独立授权初始化员为原执行User办理固定源和已确认成果的限时精确 `metadata_read`。单体布局取System已核事实，Grant绑定同一Tenant／User／Engine、完整路径、fingerprint和boundary hash；执行者仍精确十二项功能权限，办理凭据仅留准备控制器。每次普通枚举前核当前可见性；未知授权写入也先登记待撤销，退出后核重新不可见，业务与撤销错误分列。

公开Meta继续核安全结构投影；三方正整数大小、完整TIFF属性和普通图片源前后快照由当轮Hosted PostgreSQL固定有界只读查询取证。绑定 `addp_online` 容器ID／启动时刻、suite／Run ID、非默认Tenant／真实User、源扫描或当前Develop父执行及精确自动扫描；容器、身份、目标错配即失败。已发布但父执行失败的切片只认原夹具白名单及实际返回扫描绑定，失败前缀完整Meta对象集合另从同一受控快照核对，不能让普通列表权限过滤掩盖额外登记。

Develop公开投影不返回 `tenant_id`：成果候选登记前由同一快照核父Tenant／内部发起主体，公开UUID／模块／任务类型／状态／发起User仍匹配，不要求恢复内部字段。上述取证不扩大公开投影，也不减像元／COG、源及既有成果不变、当前血缘／页面或清理断言。

#### 栅格主场景失败补偿与退出

**发布已确认后的清理故障**复用同一suite：Business-owned回环代理仅针对首片／末片明确对象，在真实MinIO PUT成功、回复Runtime前按上传长度／SHA-256唯一定位私有临时COG，在sidecar路径建目录触发真实unlink失败；原COG字节／大小不变。代理以Runner UID及实际Docker socket GID运行，生产／读取owner-only故障事件的身份一致；仅内部持socket／事件，由现有Hosted生命周期独占创建／销毁，不新增产品故障参数。

部分确认时下一片为 `not_attempted`；全部确认时不返回 `failed_tile/publication`，两者父执行仍failed。逐一核确认产物、严格整数大小、新自动扫描、先前成果哈希及执行工作目录清理；未真实运行不计该故障T4通过。

**硬超时后的授权补偿**不能依赖Python finally。停止System／各Owner前，既有生命周期钩子从当轮元数据日志恢复精确撤销：绑定suite／Run、原容器／启动时刻、普通User／Tenant、固定Engine／对象、请求UUID及已确认Grant回执，未知写入同样留待办。先归档原日志，经正式System接口撤销并核普通User不可见，再逐项回写剩余请求、另存补偿结果；失败／未知不记撤销通过，不改原Online失败报告，不将超时后清理计入1740秒内业务通过。

所有退出路径按归属销毁本轮业务容器、临时registry、平台容器／数据卷及产品Runtime容器／镜像，并核零残留。外层Infra销毁、业务产物核验、授权撤销分别认定，不能互相替代。

#### 栅格主场景固定结果与验收边界

| 固定版本／运行 | 结论与保留边界 |
| --- | --- |
| `0bee3c249cdc08424f6b97b9aab11d3a1e445655`，[Hosted42383359430／Job128435478324](https://github.com/pampa0629/addp/actions/runs/42383359430/job/128435478324) | 1740113毫秒触发 `online_scenario_timeout`，full失败。七服务同提交，67份页面通过回执、最后元数据为 `dem-slope.cog.tif`；普通图片及末尾分支撤权无完整结果。58份metadata Grant撤销仍pending，不能报收权完成；部署清理／Infra零残留单独成立。223次血缘HTTP合计21.81秒、最长419毫秒，不据总超时推断接口变慢。 |
| `a7895d467ca2d69cee45a578d2a4dcbf0c5c8d89`，[Hosted42390011971／Job128455431586](https://github.com/pampa0629/addp/actions/runs/42390011971/job/128455431586) | full实际通过，预检＋业务1489195毫秒，未扩1740秒。七服务同提交；执行者Tenant2／精确12权限，分支读者独立普通User／精确3权限。create／冲突／replace、多来源／页面构图、DEM、PNG／JPEG及分支撤权完成；71份主场景页面回执、52组三方严格正整数大小相等。64份主场景＋7份分支Grant全撤且不可见，pending空；154次有界取证、资源策略恢复、部署／Infra清理通过。 |
| 同源码[GeoPython Linux CI42389976288／Job128455317630](https://github.com/pampa0629/addp/actions/runs/42389976288/job/128455317630) | 实际21113项通过／145警告，登记及编排门禁通过；不外推其他CI Job。 |

成功full的权限血缘证据独立保留：撤A前四图各2节点／1条当前执行直接边；撤后A1／A2各1节点／0边、`permission_filtered=true`，隐藏源名／指纹／execution UUID均不泄露；B1／B2仍各2节点／1边且无隐藏提示。C下游可有既有隐藏提示，但无本次执行关系，四张授权前产出图也不含C；已核八张原始图及三张前后对照截图。原专项通过不替代full，后续成功不覆盖首轮超时、收权缺口及其实际未验范围；以上固定结果不代表当前源码已重跑。

### 多来源计算与页面构图

三条链路复用 `raster-workflow` [运行准入](#栅格主场景运行准入)和1740秒总预算，分别取证。计算／页面构图的[成果共同核验](#栅格主场景计算矩阵)不放宽Runtime、物理或单次浏览器期限；分支专项仅按下述范围执行。固定通过范围查[主场景结果](#栅格主场景固定结果与验收边界)。

#### 多源加权与RGB合成

源Engine的EPSG:4326 TIFF与目标Engine已自动扫描的EPSG:3857 COG，显式参考网格对齐、选波段及组合，分别执行跨文件加权表达式和RGB通道合成。Develop／Monitor绑定完整两个ResourceLocator；Meta登记同一execution从两个源DataItem到目标的derive边，Console按完整输入集合核卡片／链接及逐源证据。

独立像元公式核全部波段、NaN／mask、Red/Green/Blue解释、网格／CRS及nearest金字塔；源和全部已接受成果哈希、精确对象集合不变，新成果严格满足 `size_bytes == physical stat size == Meta DataItem size > 0`。RGB生成不证明显示拉伸、自动最佳波段推荐或Manager RGB渲染。

两个执行各两份浏览器来源证据，共四份；早期累计“29”仅属该扩展阶段，不能作为当前full完整数量。当前主场景71项按[独立计数断言](../../../../scripts/test/raster-workflow-online_test.py)核验，不能用早期计数或本专项通过代替。

#### 普通用户页面多选构图

保留原API矩阵，普通User在真实Console／Develop多选三个独立登记TIFF，配置镶嵌／COG保存、首次保存、重载后运行。仅本suite夹具补 `develop.task.create`／`develop.task.delete`；容量suite不继承，不增加更新、引擎管理或管理员权限，不改产品规则。

页面负责创建／执行，不能用API提交或图对象注入代替；读回真实定义／回执，核Runtime、来源顺序、公开参数、目标及父执行绑定。新成果按上方共同标准核全像元／mask／金字塔／COG、三方正整数大小、自动扫描、逐源血缘及同一User页面。成功和归属可确认的失败均删除本轮任务并核404，未知创建／执行结果不自动重发；零残留与实际用户操作分别验收，未真实通过不登记该链路完成。

#### 分支血缘与源撤权

手工 `raster_case=full|branch-lineage`：默认full在末尾执行本项；独立branch-lineage先深扫三个源、仅执行该专项。未知／空值在生命周期动作前拒绝，Job／Artifact／`raster-case.json`记录范围，不将独立专项计为完整套件。

一次执行由A、B各派生两份成果，C仅加载且未参与产出；四成果须由该执行自动扫描／登记。独立初始化员向同一专用普通读者签发三个源＋四成果的限时精确 `metadata_read`，不改变执行者权限。读者在同一会话逐图核直接边和execution UUID，再仅撤A源授权：

| 阶段 | 精确断言 |
| --- | --- |
| 撤权前 | 四张完整可见产出图各只含指定A／B源与本次执行唯一直接边；共同证明C未参与。C下游图不得有本次执行关系，但full前序合法使用C产生的历史关系／权限隐藏提示不算本项失败。 |
| 撤A后 | A两成果各留当前节点、零关系及权限隐藏提示，响应不暴露隐藏端；B两成果仍保留原直接边且无隐藏提示。 |
| 收尾 | 留逐图回执／截图；成功失败均清本轮精确授权并复核元数据不可见，原业务失败和清理结果分别保存。 |

确定性沿 `make test-raster-workflow-online-runner` 与既有Online CI登记；接线、受控断言和真实跨服务／页面Hosted分别认定，不因本次文档整理增加验收结论。

### 重分类与整数分组聚合

同一suite继多来源计算执行，复用[成果共同核验](#栅格主场景计算矩阵)：源字节／先前成果哈希及精确对象集合不变，三方严格正整数大小相等；逐项核自动Meta scan、资源级derive、Monitor和同一普通User的Console。1740秒总预算及各单项／单请求超时、先前场景断言均不变。

#### 重分类规则与缺值

正式源ResourceLocator加载确定性双波段TIFF，选择第二数据波段：`reclassify-nodata`省略 `unmatched`，核默认NoData；`reclassify-keep`显式保留未命中原值。规则覆盖精确值、合法零类别、左闭右开区间、未命中间隙及无界端点。独立公式核全部像元、NaN、mask和nearest overview；源NoData不能因 `keep` 恢复有效。

#### 整数分组与精度

`aggregate-mean`／`aggregate-sum`接在重分类之后，选源第二波段，`factors=[3,5]`生成86×52单波段COG，显式构建43×26 nearest金字塔。独立公式核所有像元、源NoData有效计数，以及右侧／底侧／右下角不足整块的分组；均值仅允许Float64运算所需局部容差，求和和旧场景仍精确比较。

#### 扩展阶段计数与验收边界

重分类早期在原29份浏览器证据上新增两份，累计31份、22份COG；mean／sum原轮逐次核先前22/23份成果，累计33份浏览器证据、24份COG。以上仅为阶段清单；当前full的71项观察及后续聚合范围查[主场景要求与固定结果](#部署与主场景)，不由旧计数推定全量通过。

### 外部裁剪、轮廓与局部更新

原suite、1740秒总预算及各单项期限不变。写出路径复用[成果共同核验](#栅格主场景计算矩阵)：`size_bytes == physical stat size == Meta DataItem size > 0`、自动扫描／资源级derive、Monitor与同一普通User的Console。逐次核源字节、既有成果SHA及精确对象集合；只读路径不造新对象／大小／发布证明。

#### 区域外裁剪：持久化COG

聚合后执行 `clip-outside`：正式源ResourceLocator加载双数据波段TIFF，`raster_clip(mode=outside)`使用带孔Polygon，保留完整256×256网格，直接保存双数据波段及alpha，不能选波段省略alpha验收。独立行列公式核外围／孔洞／区域内NaN、源NoData不恢复、255/0 alpha、逐波段mask、颜色解释和128×128 nearest金字塔；每数据波段53247个有效像元。

#### 有效像元轮廓：只读GeoJSON

`footprint-any`／`footprint-all`正式加载双数据波段及alpha的确定性TIFF，省略validity核默认并集，显式all核交集。独立整数像元公式验证完整面覆盖、边界网格／面积及波段独立缺值，不用Polygonize或生产算子生成期望，不限定合法环的起点／方向。

Develop／Monitor／Console核同一普通User的GeoJSON预览；不得生成持久输出、derive或目标扫描。源及已有COG不变，既有成果三方大小规则保持；两项不代表全部后续轮廓扩展。

#### 局部更新：持久化COG

外裁剪后正式加载基底和带双数据波段／alpha的更新源，局部裁剪更新源、显式对齐基底，经 `raster_update` 保存双波段COG及nearest金字塔。独立行列公式核更新覆盖、区域外／无效更新像元保留基底、合法零、部分alpha及逐数据波段有效性；源不可变，两个输入分别绑定来源浏览器证据，自动扫描与双来源derive不能省略。后续只读轮廓继续保护全部既有成果。

#### 阶段清单与固定结果

外裁剪早期为34份浏览器／25份COG，核先前24份；其后轮廓不增产物，更新后查询保护26份。下表36／25、38／26也仅属扩展阶段，当前full另查[固定结果](#栅格主场景固定结果与验收边界)。

| 固定版本与证据 | 已验范围及CI边界 |
| --- | --- |
| `89e77da9a1bc72ec6c7e1791f2172edbbfdefb28`，[Hosted T4 37741228176](https://github.com/pampa0629/addp/actions/runs/37741228176) | 已独立核归档：并／交覆盖65532/65524像元、36份浏览器、25份COG及零残留清理。同提交Platform CI中取消的GeoPython全量Job仍不计通过。 |
| `53591eba7e749c84849fe2e1ebff4a8ee7a525e9`，[Hosted T4 37795049439](https://github.com/pampa0629/addp/actions/runs/37795049439) | 已独立审计：更新成果182565字节、两波段有效数65535/65536，38份浏览器、26份COG、双来源derive及零残留清理。 |
| 同上源码[Platform CI 37794921834](https://github.com/pampa0629/addp/actions/runs/37794921834) | 标准GeoPython全量实际697项通过；同提交另一取消运行不计通过。 |

验证沿 `make test-raster-online-runner`、`make test-geopython-workflow`、Platform CI与手工 `online-t4-gates.yml`，无新入口／依赖／suite。固定Hosted、CI与取消分列，不代表当前源码已重跑。

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

## DEM 平面曲率独立 Hosted 验收

独立手工入口 `raster-plan-curvature-persistence` 已闭合本轮固定范围，普通 User 原12权限、非默认 Tenant、同源产品默认入口 Runtime、独立 NFS 与600秒业务预算。仅新增本套件测试、登记和既有文档，不修改产品曲率、权限或其他模块逻辑；实际 Linux／Hosted 由专门CI会话协调，本会话仅对其回传结果独立审计，不触发、轮询或设置定时。

15条成功COG链：三种高程／水平单位组合的直接plan，填洼接plan、重分类接plan，完全水平平地、非平地驻点、全无效、无完整有效邻域、小图，实际有效存储高程 `8z+64` 接plan，以及默认mean／显式mean／profile／tangential四条对照。519×515多波段跨512块样本显式选择第二波段，包含凹凸／鞍面／倾斜面、圆形等高线、普通可表示极小非零梯度、合法零、NoData、mask与部分透明。

完整像元主预期由独立九点等权自由常数最小二乘及等高线单位切向的水平法向加速度计算，保留 `RTOL=1e-10`、`ATOL=1e-11`；不用产品卷积核或切向结果生成plan主预期。另核圆形等高线 `-1/r`、正高程缩放平移不变性、水平平地严格0、其他精确零梯度NoData及无阈值。默认／显式mean保存数据字节一致；实际保存四方法仅在非零梯度共同有效处核 `profile+tangential=2×mean` 及 `tangential=plan*g/sqrt(1+g²)`。每种物理／语义断言均有污染反例。

13类非法输入各以中英文提交，共26次准确失败：非法波段、alpha、复数、旋转、南向、经纬度、缺CRS、缺网格、非法高程单位、单位冲突、未应用scale、未应用offset、非法method。核当前执行及curvature任务准确原因、提交语言冻结／反向查询不改写、Monitor失败、无发布／新增扫描／派生血缘。所有失败继续全量核源及先前成果、文件清理。

每条成功严格核完整像元／mask、COG完整布局、按实际尺寸的最近邻金字塔、Float64／NaN／每米单位、源网格／CRS、artifact／物理／Meta相等正整数大小、自动扫描精确父执行／用户／租户／引擎及当前执行实际输入血缘／Monitor。成功保存后批量逐条等待精确血缘，不绕过普通收集器；保留不含响应、SQL、凭据或错误全文的封闭阶段进度，所有退出清理独立NFS／镜像／部署／Infra及私有工作区。

标准入口：`make test-raster-plan-curvature-online-runner`；真实 GDAL 与污染反例进入 `make test-geopython-workflow`，输入选择及手工 workflow 同次登记。本地筛选不计全量；数值极限单列 Native。

**固定结果**：`79ebc6ff93f2301db51a34e64f54e7ac0976014b` 的 [Linux Native Job121440770332](https://github.com/pampa0629/addp/actions/runs/40283174360/job/121440770332) 全量 7253 项通过；同源 [Hosted Run40283794715／Job121442687622](https://github.com/pampa0629/addp/actions/runs/40283794715/job/121442687622) 在 600 秒内通过 15 链／26 次双语拒绝、228529 个共同有效像元的方法关系、完整物理／扫描／血缘／Monitor、源旧不变及清理／Infra 零残留。artifact `12635131765` 无 COG 原字节，不算离线重算。父 Platform 仍有公共 crypto 依赖冲突，不计整套通过；UI、容量、任意影像、业务 DELETE、旧剖面超时及原生 double-free 不由本项关闭。

### ORC 单文件表格 Hosted

阅读导航：[入口与权限](#orc-入口与权限) · [平坦字段与批量传输](#orc-平坦字段与批量传输) · [嵌套对象与数组只读](#orc-嵌套对象与数组只读) · [源变化与读取限制](#orc-源变化与读取限制) · [清理与报告](#orc-清理与报告) · [分层验证与历史证据](#orc-分层验证与历史证据)。

#### ORC 入口与权限

2026-10-27 用户批准 `native-table-orc` 独立 case，标准业务入口为 `ADDP_ONLINE_MANAGER_CASE=native-table-orc make test-online ONLINE_SUITE=manager-internal-artifact-lineage`。人工 Hosted 沿原 Manager Job，复用隔离 NFSv3、Business MinIO、现有 GeoPython 产品镜像、六个业务 Backend 与 Console；不构建或启动本 case 不使用的模型、点云或文档 Runtime，不改变其他 case 的断言及部署。

一次性普通 User 仅使用已批准 11 项现有权限：`manager.content.read`、`manager.data_item.read`、`meta.catalog.read`、`meta.scan_task.execute`、`meta.scan_task.read`、`monitor.execution.read`、`transfer.task.create`、`transfer.task.read`、`transfer.task.execute`、`transfer.task.delete`、`system.execution_authorization.create`。不授予管理员角色，不修改个人／生产账号或产品权限声明。源和目标由明确 locator 及 source-only plan 绑定，不枚举同目录兄弟源。

#### ORC 平坦字段与批量传输

两份相同物理内容的 NFS／MinIO 单文件各含 2503 行，覆盖超过 JavaScript 安全范围的大整数、decimal(38,9)、NULL／空值及二进制。验收深度扫描、Meta 原生字段与 footer 行数／源版本、三页精确值与尾页、真实页面下一页和原文件下载；真实 Transfer 以 1000 行批次写 Parquet，由产品镜像内独立 PyArrow 完整回读所有字段、数值、缺失掩膜和 Decimal 精度。

#### ORC 嵌套对象与数组只读

ORC 嵌套扩展复用同一 `native-table-orc` case、原 11 项权限与精确两引擎夹具范围；不增服务／Job／权限。保留原平面 2503 行、原始下载、两次真实 Transfer→Parquet 全值回读和完整清理断言，另在两源各加入 43 行 struct/list 组合文件：Meta 完整类型树，三页全部 43 行的结构与精确值、NULL／空集合区别，实际页面下一页与下载、打开对象／数组详情逐项核对精确内容、四张分页截图及两张结构详情截图、源字节和元数据不变。

同一 case 的两个 Transfer 任务读取原平坦源；新增嵌套源只验收元数据、分页、结构详情和原件，不据同轮保留的 Transfer 成功推定嵌套源可批量传输。

#### ORC 源变化与读取限制

ORC 源变化在 Runtime 使用封闭 `ORC_SOURCE_CHANGED`，Manager 复用 `preview_source_refresh_required` 409 和既有国际化提示；不通过解析本地化异常文本识别变化。

原生 decoder、Common 批量 reader 继续拒绝 Map／Union；嵌套源的 `OpenTableReader` 在任何行内容读取前拒绝，即使请求仅标量列也不能绕过。该只读扩展不计为嵌套批量传输通过；正式 Hosted 结果按固定源码与新产物单独登记。

#### ORC 清理与报告

按[公共收尾](#文件读取-case-的公共准备与收尾)分别核源字节／元数据不变、本人任务删除、物理夹具产物清理，以及外层 NFS／卷／镜像／部署／Infra 零残留。

报告为 `manager-orc.json`、`manager-orc-browser.json`；原件归档包括两份平坦源和新增两份嵌套源，以及各自 Meta 与截图，并与 `online-report.json` 和运行 SHA 一并复核；没有真实报告时不得回填 Hosted 通过。

#### ORC 分层验证与历史证据

T0/T1沿 `make test-geopython-workflow`、六个受影响Go owner、两组Manager／Online runner与既有CI登记，不新建Job／suite。两类Hosted独立认定：

- [平坦读取与搬运](../evidence/资源读取与预览历史证据.md#orc-单文件只读分页与批量读取--nfsminio-普通-user-hosted-t4)：`c16d563fc2e177521f3924b36b56c9bfc8cce18d`、Job `122074622494`；两源2503行、两Parquet任务。前三轮失败、旧预检漏Transfer、本地T2失败与Linux结果分别保留。
- [嵌套只读](../evidence/资源读取与预览历史证据.md#orc-对象与数组只读预览--nfsminio-普通-user-hosted-t4)：`f947636548da8055f7f9cd68c797e6979a1514cd`、Job `122242512428`；首轮页面size未初始化失败、准备复验取消仍保留。详情精确值由DOM证明，动画帧截图不算稳定视觉证据。

源变化／坏输入／预算属Native，嵌套reader拒绝（含仅标量投影）属Common T1，均不冒充Hosted搬运或故障注入。`validation=footer` 不证明全数据页；两类结果均不认证Map／Union、加密／ACID／whole、ORC写出、上传界面、任意来源目标或容量。原始产物、清理及分层失败统一查所链证据，不扩展固定版本范围。

### 双DEM填挖方统计独立 Hosted 验收

已按确认范围通过独立手工`raster-cut-fill-persistence`，复用Hosted／NFS、同源默认入口产品Runtime、普通User原12权限和独立600秒业务预算；不加入nightly，不改产品算法、权限或其他套件。519×515矩形多波段样本显式选择第二高程波段，30份源覆盖两源各自NoData／mask／透明／部分透明、合法零／负高程及跨512块。12条成功CSV链固定为米制混合挖填、前后互换、完全不变、纯填、纯挖、三组不同高程单位组合、填洼接统计、重分类接统计、Int64和UInt64相邻大高程；水平网格覆盖米与美制测量英尺。18类非法参数／数据各按中英文提交，共36条准确失败，须定位到cut_fill节点：两角色非法波段、非法声明单位／单位冲突、alpha／复数、scale／offset、CRS／尺寸／完整网格不符、旋转／南向／经纬度／缺CRS／缺定位，以及一源全无效和有效区互不相交。反向查询语言不改既有失败。

独立oracle按完整35×35周期的闭式计数及所有特殊像元修正，以200位Decimal计算高差／面积／体积，不导入产品计算函数；完整标量遍历另核周期计数。水平面积使用源文件实际解码的网格和CRS线性换算事实，保留WKT序列化末位事实，CSV物理值仍逐项精确比较，一ULP篡改反例不得通过。严格核单行13列、6个Int64／7个Float64逻辑Schema、总／有效／排除及挖／填／不变计数守恒、平方米／立方米、挖填非负、净填减挖及前后互换关系；普通表格仅沿既有save链路，不生成RasterArtifact。物理／Meta大小必须为相等正整数，不补造第三方大小字段。

每次完整物理核验后，将已成功CSV原字节保存到`cut-fill-csv/`，清单`cut-fill-csv-manifest.json`记录文件名、大小及SHA-256；收尾再次逐字节比对，之前归档发生变化或出现额外／软链接文件均失败。12份CSV、清单、完整日志和回执由独立Job归档。每份成果核精确扫描父执行／用户／租户／引擎身份、当前执行两份实际源血缘与Monitor；全部保存后、负例前逐条核齐血缘，保留普通采集机制。失败无发布／新增扫描／派生血缘；源、先前CSV和Meta事实保持不变，Runtime工作区、NFS export／归属目录、部署及Infra清理不弱化。

固定实际源码`a26d758daab9149e6fdda628aa202957d9245906`的[填挖方Hosted Run40635872131／Job122642597594](https://github.com/pampa0629/addp/actions/runs/40635872131/job/122642597594)已通过完整原始归档领域审计：**12成功／36次准确双语失败，业务266531毫秒，小于600000毫秒预算**。七服务同源码、干净仓库、Tenant2普通User33原12权限；Runtime默认入口`sh container_entrypoint.sh`，镜像`sha256:6dc1f8cdf4657a93c05c3b510a48a50ffa48262ef60a2835e891c07abc16bf33`。12次自动扫描逐一绑定父执行／租户／用户／引擎，Monitor及24条当前执行实际双源Meta关系齐全。12份真实隔离库只读内部血缘与公开投影证据核source-port、唯一output.save／create及save／derive，公开operations仍隐藏；未扩大权限或安全投影。源与先前成果不变、Runtime工作区清理、外层cleanup passed与Infra zero_residuals分别通过。

官方artifact`12794083457`，2188521字节，ZIP SHA-256：`0bf9d93db818b6b7be93620f609716340955d7eecac77406114541945833dde4`。12份CSV原字节与清单／大小／hash／Runtime物理base64逐份一致；本会话基于固定提交的独立Decimal夹具及已归档源定位事实，精确重算并比对全部单行13列，无容差替代最终存储值，同时核计数／面积／净量守恒及前后互换。原始源TIFF未随归档保存，因此不宣称离线重读了源DEM；物理／Meta相等正整数大小保持，不补造不存在的第三方大小。

较早`2edf97e60`的Run40485888750与`16452b150`的Run40537589903均曾在首条血缘校验失败，旧归档与失败结论保留；后者仅证明公开operations为null，不能补认旧内部持久化。其旧ZIP SHA-256分别为`56713f32bed14e38301b3ab0479b55d518d6f8d24aafa259740085303275c56a`、`a1a7584038fd4b320c91168cf2c98a52bb60f2b7b02e0234a3d76f416a1fefe7`。本次通过属于a26新来源复验，不改写旧运行。数值极限仍单列Native，不计UI、容量、生产或受管业务DELETE。

### DEM 地形位置指数（TPI）独立 Hosted 验收

按用户批准通过独立手工`raster-tpi-persistence`，复用Hosted／NFS、同源默认入口产品Runtime、普通User原12权限和600秒业务预算，不加入nightly或修改原套件。固定13条COG保存链：米制混合凸凹、水平平地／倾斜平面／对称鞍面三种合法零、三组高程及水平单位交叉、填洼接TPI、重分类接TPI、Int64／UInt64普通大基值原生高差、全无效及有有效像元却无完整九点邻域。519×515矩形多波段样本显式选择第二高程波段，覆盖跨块、NoData／mask／透明／非有限覆盖与有效部分透明。13类非法输入各中英文提交，共26次准确失败：波段、alpha、复数高程、非法声明单位、单位冲突、未知波段单位、scale、offset、经纬度、旋转、南向、缺CRS、缺定位；逐条定位当前tpi节点及提交语言，反向查询不得改写原失败。

独立高精度Decimal八邻居均值与解析平面／鞍面校验完整Float64／NaN像元、mask、合法零、正负号、原网格／CRS及米制单位，不导入产品计算函数；严格比较最终存储值，一ULP错误也拒绝。完整COG布局、金字塔像元／mask、artifact／物理文件／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前实际源血缘／Monitor、源和先前成果不变及所有清理保持。失败不得发布、新增扫描或派生血缘。每次核验后归档已成功COG原字节到`tpi-cogs/`及`tpi-cogs-manifest.json`大小／SHA-256清单，已有成果与清单不可被静默修复，收尾再次核清单／hash／大小；未知文件、软链接或归档变更严格失败，支持回传离线重读。

固定实际源码`a26d758daab9149e6fdda628aa202957d9245906`的[TPI Hosted Run40635880285／Job122642624444](https://github.com/pampa0629/addp/actions/runs/40635880285/job/122642624444)已通过完整原始归档领域审计：**13成功／26次准确双语失败，业务535785毫秒，小于600000毫秒预算**。七服务同源码、干净仓库、Tenant2普通User33原12权限；默认产品入口`sh container_entrypoint.sh`，镜像`sha256:68b9db492be628355208c8cb0d54266c470e79f76733730ff129311bd1f2f307`。13份真实隔离库只读内部与公开血缘证据均核准input.source、唯一output.save／create及raster_save／derive，公开operations继续隐藏；13次精确父执行／租户／用户／引擎扫描、13条当前执行来源Meta关系及Monitor齐全。源与先前成果不变、Runtime工作区、外层cleanup passed及Infra zero_residuals分别通过。

官方artifact`12794317336`，2948581字节，ZIP SHA-256：`01998be5bb1ce2cfdaa4075116c77be6602c685f5fe345aaa29d5df8513129d5`。本会话逐份重新打开13份原始COG，核519×515、原CRS／完整仿射、Float64／NaN／metre、完整像元及mask、三级nearest概览与COG布局；以实际提交中的独立Decimal确定性夹具oracle逐像元精确比对，通过合法零／原生大基值／全无效及无完整邻域情况。全部原字节、大小／hash清单、artifact／物理／Meta三方大小与封闭血缘回执一致。离线GDAL读取仅在审计进程设置既有Homebrew PROJ_DATA／PROJ_LIB，不改系统环境、服务或产品；原始源TIFF未归档，oracle据固定确定性夹具重建，不宣称离线重读源TIFF，也不重跑产品或Hosted。

早期固定`63d6299ea`的Run40538763760曾在首条execution血缘校验失败，未完成13／26及原件归档，完整actual差异亦未保存；旧ZIP SHA-256：`e6f4a4ef0416d5aacc2263a03116d25fb2515f5e0f46996c5c39bce908ac4281`。旧失败原件与结论保留，不能由a26复验回填旧内部事实。数值极限、额外类型组合继续单列Native，不计全部单位组合、UI、容量、生产或受管业务DELETE。本次仅一次受控API验收，不认定稳定吞吐。

### 填挖方与TPI受控血缘取证修复

用户已确认仅对填挖方／TPI两套Hosted增加受控只读取证。公开Develop／Monitor响应仍须隐藏operations；完整内部source-port／create／derive逐项严格核验，再沿原普通User十二权限核Meta当前执行实际来源关系。仅当次Hosted Linux隔离addp_online容器可准备私有归属回执，冻结容器ID／启动时刻／suite／Run ID；读取时重新核归属与未重启、非默认Tenant／真实User／当前execution及完全相同DAG，固定有界只读事务，不接受任意SQL，不写库／补造事实或主动collect。数据库凭据不传给业务User请求，公开接口无回退路线，补充数据库证据不计API授权通过；仅封闭字段与大小／hash归档，拒绝前保留公开／内部差异及取证失败类型。旧矩阵、预算、原字节、数值／NULL、扫描／身份／Monitor、源与先前成果不变及所有清理保持。产品安全投影、权限与其他套件不变，本轮实际Hosted复验已由CI会话协调完成。


标准回归沿 `make test-raster-cut-fill-online-runner`、`make test-raster-tpi-online-runner`、`make test-python-ci-registration` 与 `make test-geopython-workflow`。必须拒绝公开 operations 泄露、内部事实缺失／错源／错端口／错操作、身份或 DAG 绑定错误、容器更换、取证或归档篡改；错误只留类型与指纹。公开 API 授权和受控数据库证据分别认定。

固定修复 `1a0b3bb27d7ada93404fff7b76fd677d83800122` 的 [Linux Job122636163645](https://github.com/pampa0629/addp/actions/runs/40633847309/job/122636163645) 全量 7858 项通过。后续 `a26d758daab9149e6fdda628aa202957d9245906` 两项 Hosted 的非 Markdown 输入与修复相同，实际结果分别见[双 DEM 填挖方](#双dem填挖方统计独立-hosted-验收)和 [TPI](#dem-地形位置指数tpi独立-hosted-验收)：12 CSV／13 COG 原件可独立复核。固定版本等价不等于整树 SHA 相同，不外推后续 HEAD、其他 Job 或旧 double-free 根因。

### TPI验收计时与本轮独立期望值缓存

缓存仅保存本轮独立Decimal期望数组，绑定Run／夹具代码／源hash，私有锚点及数组hash每次必验，缺失／篡改／跨轮复用或清理失败均失败。首次生成和清理在原600秒业务内；不缓存实际成果、不并发业务、不改轮询／资源策略、产品、权限或其他套件。13成功／26准确中英失败、39调用／429次实际成果全检，以及完整像元／mask／概览／COG／三方大小／扫描／内部及公开血缘／Monitor／源旧不变／零残留保持。

固定`189f5f8ce3219ba6257d055f975f9bd2154c0b1d`的[Hosted40645432302／122673041241](https://github.com/pampa0629/addp/actions/runs/40645432302/job/122673041241)已终审通过，业务259655毫秒，包含期望生成7703毫秒、整个准备阶段8432毫秒及清理325毫秒，嵌套计时不重复相加；39次现场物理调用／429次实际成果检查、13份COG原字节／manifest及独立离线Decimal全像元／mask／3级nearest概览复核通过，原件与较早a26一致。与a26单次535785毫秒相比减少276.13秒（约51.5%），仅为两个固定运行的观测，不承诺稳定吞吐或容量。数值极限继续单列Native，不计UI或任意影像。

准备提交`2b7df04aa5fdd097f4a2fe1c11062f69c0c6cae3`的[Linux40644787773／122670954049](https://github.com/pampa0629/addp/actions/runs/40644787773/job/122670954049)实际标准`make test-geopython-workflow`全量7887通过／24 warnings／2119.73秒。原始ZIP、日志与实际步骤由CI会话核验，领域会话只读复核官方digest／CRC、原件／只读血缘回执字节、矩阵和清理记录；不重跑计算、不查询或派发CI，不外推后续源码通过。


### DEM 地形崎岖度（TRI）独立 Hosted 验收

独立手工`raster-tri-persistence`复用Hosted／NFS、同源默认入口Runtime、普通User原12权限及600秒预算。固定13条COG成功链覆盖凸凹、平地、倾斜平面、对称鞍面、三组单位交叉、填洼／重分类、Int64／UInt64大基值微差、全无效和无完整九点；13类错误各中英文共26次。显式第二波段、跨块／多波段／NoData／mask／alpha／非有限覆盖、合法零和非负均按完整像元核验；数值极限仅Native。

独立2500位Decimal Riley平方和开方oracle不导入产品函数；每条成功和负例后重读全部已有成果，39调用／429次实际全检，保持一ULP拒绝、完整mask、米制／原网格／CRS、COG布局／3级最近邻概览、artifact／物理／Meta相等正整数大小、精确扫描身份、当前实际输入完整内部血缘／公开安全投影／Meta关系和Monitor。原件和清单不可静默修复，源／旧成果不变、失败零发布／新增扫描血缘及全部清理均为必要条件。只读取证限定本轮容器／用户／租户／执行；期望缓存绑定Run／夹具／源hash，不缓存实际成果，准备与收尾均纳入600秒。

固定`db2638ac9d7dc2f11a1fc949d0be77b0a0f238de`的[Hosted40652107491／122694375654](https://github.com/pampa0629/addp/actions/runs/40652107491/job/122694375654)已按上述完整范围终审通过，实际业务368511毫秒。归档13份COG原字节及清单，CI会话以固定提交的确定性夹具独立Decimal复算全像元／mask／CRS／米制／3级nearest概览并核COG布局；源TIFF未归档，不称为从归档源重新生成。官方ZIP为4208788字节、SHA-256 `2bb8b8981c390d1b6e3636f4bfc1c2850e8eda9a0fbe3d3770a7d2737f9740bb`。领域会话只读复核digest／CRC、13原件和受控内部血缘回执字节、13扫描／Monitor、完整矩阵及零残留，不重跑Hosted。

对应准备提交`872375a48da8150a86936f8c6ae67d293b007a62`的[Linux40650267385／122688461218](https://github.com/pampa0629/addp/actions/runs/40650267385/job/122688461218)实际标准Native全量8134通过／39 warnings／1967.59秒。此前本地231项专项及runner／登记仅为准备证据，不与全量累加。固定证据不覆盖后续源码、UI、容量、任意影像或Hosted数值极限；本会话不触发／轮询／重跑／取消或设置定时，不操作个人服务／Infra。

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

`vector-zonal-percentile-persistence`；`make test-vector-zonal-percentile-online-runner`。独立运行、600 秒预算、成果／身份核验及退出清理统一按[专项共同要求](#水文与分区统计专项共同要求)执行。 当前仍显式传 `percentiles=[p]`，使用原前缀的 percentile_1／percentile_value_1，保持原矩阵及预算；64 条执行与查询计入预算，不将历史固定结果外推为后来多项套件通过。

固定16条成功GeoPackage保存链：不带百分位的原八列1条；0／100端点、奇偶总体50百分位4条；25／75插值、33.3跨块中心／全部触及4条；自定义／空前缀2条；栅格重分类→面化→90百分位1条；Int64／UInt64数值NoData邻接有效值2条；全无效数值保留面行与空矢量批次保留Schema／CRS2条。常规面共同覆盖孔洞、重叠、MultiPolygon、细面、部分／完全出界、单有效零和重复几何，以显式唯一`source_row`核行序；主栅格保留旋转网格、显式第二波段、独立mask／alpha和未应用的scale-offset。GeoPackage的FID不冒充用户索引；重复索引、活动几何列名冲突、混合类型复数alpha及数值极限继续单列Native。

固定24类各按中英文提交，共48次准确失败：百分位列表null／布尔项／字符串项／嵌套数组项／对象项／低于0项／高于100项共7类；波段0／越界／布尔／字符串4类；所选复数数据与alpha2类；all_touched数值／null、前缀null／布尔4类；原统计列／percentile_1／percentile_value_1冲突3类；不同CRS、栅格缺CRS、缺仿射及奇异网格4类。必须到达当前执行的`analysis`统计任务并准确核原因，前置加载或扫描失败不能充数；新增百分位原因按中英核验，原有英文原因沿当前契约，不把两种语言提交称为全部诊断已翻译。反向查询不改写提交语言、原错误或执行快照。非法几何／无矢量CRS等文件准入边界、非有限JSON参数与IO／磁盘注入仍沿Native或既有专项记账。

独立oracle用像元平面的Shapely中心包含或正面积相交计算覆盖，不调用产品函数、GDAL栅格化／面化或NumPy百分位；派生面由独立4邻接遍历与横向条带并集计算。Python标量排序、标准库统计及Fraction的确切二进制位置核完整逐面属性、几何／孔洞／多部件、Schema／CRS／顺序、原八列和新增两列、null与合法零。同源带／不带百分位的原八列逐值相同，端点／50百分位与min/max精确，其他百分位固定rtol=4e-15／atol=0，原统计其余值rtol=1e-12／atol=1e-12。GeoDataFrame公开FeatureCollection核完整属性和身份；所有完整拓扑几何及真实字段类型均在物理文件重读时核验，不新增宿主机几何依赖。

每次成功后重读当前全部成果，每次失败后重读16份，固定904次完整GeoPackage检查；加首次源全检共65轮物理检查。仅缓存冻结的独立期望，实际成果和14份源的原件hash、完整栅格像元／mask／类型／原生NoData／仿射／CRS／scale-offset、矢量几何／属性／Schema每轮重读。归档14份源原字节、16份GeoPackage原字节及大小／SHA256清单、独立期望和16份当前执行血缘取证；原件／manifest／期望变化、缺失和额外文件均拒绝，不覆盖、重建或静默修复。

GeoPackage沿普通矢量容器保存契约，不虚构RasterArtifact／COG或第三方大小：物理／Meta大小为相等正整数，唯一图层事实保留容器边界。精确自动扫描核父执行／普通User／租户／引擎；直接链核矢量面与数值栅格两源，面化派生链只核实际TIFF源，不虚构第二来源。现有受控只读observer仅登记本套件，完整核内部`save`／derive端口及公开`operations:null`安全投影，Meta关系和Monitor分别核验。失败零目标／新增扫描／派生血缘，源物理／Meta及先前成果不变；全部退出保持NFS／Runtime／registry／部署／Infra零残留。

本地标准入口为`make test-vector-zonal-percentile-online-runner`及标准Native中的`test_online_vector_zonal_percentile_fixture.py`；Online／changed gate／独立workflow、observer和固定矩阵／删断言变异检查同步登记，实际Hosted由CI会话唯一协调运行。

**固定 Native 证据**：`c116e05fd9fb3eb4f2fd49bd1598879f64a14647` 的 [Linux Job124388227200](https://github.com/pampa0629/addp/actions/runs/41151306867/job/124388227200) 全量 10587 项通过，无失败／跳过／筛除。Schema 篡改须在无存活 Feature 时修改，关闭重开后核完整字段并由原 oracle 拒绝；九种物理污染和生命周期先红后绿回归保留。此前段错误仍为独立失败，其他原生故障不由新通过关闭；不计 Hosted、页面或容量。

### 端点吸附手工容量

用户确认在既有 `make test-vector-topology-capacity`／`vector-topology-capacity.yml` 增加独立 `profile=endpoint-snapping`，仅五类十五档、每档一次、串行，不运行旧矩阵／受控对照或业务T4。EPSG:3857、容差1、同源码／产品镜像／Runner，原4 CPU／2 GiB／memory.swap.max=0和每档全流程180秒不变，Runtime准备与清理另记。镜像构建前须按根`.node-version`准备Node，不依赖Runner默认版本；登记检查拒绝缺失、条件跳过或晚于门禁的准备步骤。observer=A不包装产品函数，外部0.25秒采样与cgroup整体memory.peak、CPU用量、OOM、退出码保留；分阶段采样缺失为null，不把RSS高水位之和或采样峰值说成精确分阶段峰值。

五类为：稀疏源／参考各1000、10000、100000要素（每四源一移动、一严格边界、一原位接触、一无候选，明细N/4）；密集源／参考各128、512、2048（每个待移端点K个参考候选、唯一最近目标，明细K）；同点源／参考各128、512、2048（完整保留全部S×R明细16384／262144／4194304行）；单参考要素1000／10000／100000重复部件（源1条、仅1条明细，不因参考重复扩增）；单源要素1000／10000／100000原始部件（参考1条、明细K，保部件编号与方向）。几何全部采用精确二进制坐标及唯一可落线目标，不改产品吸附语义。

180秒包含生成双源、load、双源全量核验、生产snap_line_endpoints计算、线与Point双save和独立OGR完整重载、源文件／Frame不变核验；不得抽样、截断明细或只比较摘要，流式独立预期不调用生产算法。核所有坐标、来源、属性／null／Int64／Boolean／Float64、原行序／部件／方向／CRS，明细保原点、目标及距离，物理哈希与完整行摘要另存。固定产品源码／实际加载函数、两源Frame与文件哈希、各阶段耗时、整体峰值内存／CPU、两成果数量／大小／摘要和当档清理均绑定case及measurement ID。成功、超时、cgroup证实OOM、原生崩溃／其他失败分别保留，不自动调预算或重试；超限仅说明该场景的实测边界，归属容器／镜像／registry／合成数据清0。该容量不认证扫描／血缘／页面／普通User授权或跨文件事务；已通过的endpoint-snapping T4保持原范围。


### 面界提取独立普通用户场景

用户确认新增 `polygon-rings` 独立case，复用 `vector-topology-persistence` 原入口、Hosted生命周期、非默认租户普通User精确11权限与600秒业务预算，只运行一成功和两失败，不运行full、旧业务T4或容量。三份独立3857单层GPKG分别为成功源、ring_id字段冲突源和自交无效面源。成功DAG为 `load → extract_polygon_rings(id_field=parcel_key) → save(mode=fail)`，四源要素输出八条闭合LineString：多部件面含五环、相邻面一环、重复面一环、含精确微坐标的面一环。保留原环起点／方向／重复顶点／共有边各次记录，原源顺序／部件／外环后内环；逐字段核source_id、part_no、ring_no、ring_id、is_hole及全部业务属性。源物理几何列parcel_shape与普通字符串geometry严格区分，来源用中文字符串业务主键而非FID；可空Int64／Boolean／String／Real及大整数按原生字段与原值核，不靠JS数值推断类型。文件写出的物理几何名遵循现有GPKG写出事实，不要求与内存角色名相同。

唯一成功成果rings.gpkg的stable_outputs和facts均为create；一次成果自动扫描公开整数tenant_id／triggered_by与本普通User同主体，严格正整数physical==Meta大小、单子层行数8与完整字段数；一源到一成果恰好一条本execution active derive，Monitor与原图一致，不为内存环登记DataItem。两失败各携带下游save，但不得发布文件／输出事实／扫描／derive；同UUID原始运行日志核准确拒绝原因，失败页及Monitor核同User和failed。三源与成功成果SHA／大小／源Meta在失败和浏览器后均不变，扫描库存三源加一成果恰好四次。

实际浏览器复用每次导航后的browserAPIFromResponse身份绑定；核原EWKB、8条OL线要素的来源／编号／完整坐标和真实线像素，不以像素区分重复线或证明微小线段独立可见。成功页、线地图和两失败页至少四截图，另核真实血缘tab原图及画布单边；浏览器只读，无补扫、重放执行或额外业务写入。Owner／browser／raw报告仅此case/v1，阶段检查点incomplete不得替代最终通过。归档四份合成原GPKG及原始API／Meta／血缘／Monitor／渲染证据，下载后独立OGR重核全部原坐标／类型／属性／行序／3857和SHA／大小，期望不调用生产算法生成、不舍入或增容差。最后核NFS/export/ownedroot、Runtime/registry/部署/Infra全清零；不认证业务DELETE。现有runner、Native与Console、Online CI登记及mutation检查同次覆盖独立case，无新suite、权限或永久入口。


### 邻域加权求和独立 Hosted 验收

独立手工 `raster-focal-weighted-sum-persistence`；`make test-raster-focal-weighted-sum-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。

固定16条COG成功链（14条weighted_sum＋2条原sum对照）：非对称3／5跨块两条、全1权重3／5及实际sum3／5四条、非均匀权重整体放大8倍3／5两条、中心零权重兼常量／单有效支持／有效支持空、全无效、重分类接加权、旋转网格、缺定位单行、Int64／UInt64原生NoData邻接合法整数、显式第二波段原始scale-offset及mask／alpha。非对称、放大权重及重分类使用519×515跨块样本，其余保持实际概览。全1对照使用解析0／4、−4／4、2／2隔离像元对，求和分别为4／0／4；独立逐位置2000位Decimal乘积累计后最终Float64舍入，不调用生产整数编码或归约实现，不除以权重和，不抽样或放宽容差。全1与原sum实际成果完整像元／mask相等，放大8倍与原权重的实际成果总量严格成比例，常量按实际有效正权重质量计算、单样本保留其加权值；中心无效不填洞、支持空为NaN，合法零／负结果保留，矩阵不翻转。原生NoData先判定，再沿既有Float64数值边界，不宣称任意64位整数精确。

固定28类各中英文一次共56准确失败：18类权重缺失／null／数值标量／布尔／字符串／对象／空列表／一维／行数错误／列数错误／不规则行／全零／负元素／布尔元素／字符串元素／null元素／对象元素／列表元素；原sum带权重、variety带null权重、weighted_sum带percentiles三类；未知方法／非法窗口／布尔窗口／波段0／越界波段／选择alpha／复数数据七类。须绑定同execution真实focal节点及准确原因，反向语言查询不改提交时错误；失败零发布／新增扫描／派生血缘。数值极限、真正溢出像元转NaN、非标准JSON数值及混合类型复数alpha单列Native，不以前置失败替代目标诊断；原加权均值Runner生命周期超时独立记账，不由本套结果覆盖。

完整复核次数、十源／16成果／16预期归档、当前执行observer约束及清理统一见[三种加权方法共同复核](#三种邻域加权方法的共同复核)。

### 邻域加权总体标准差独立 Hosted 验收

独立手工 `raster-focal-weighted-stddev-persistence`；`make test-raster-focal-weighted-stddev-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。

固定16条COG成功链（14条weighted_stddev＋2条原stddev对照）：非对称3／5跨块两条、全1权重3／5及实际stddev3／5四条、非均匀权重同比例放大3／5两条、中心零权重兼常量／单样本／有效支持空、全无效、重分类接加权、旋转网格、缺定位单行、Int64／UInt64原生NoData邻接合法整数、显式第二波段原始scale-offset及mask／alpha。非对称、同比例和重分类为519×515跨块样本，其余保持实际概览。全1对照源为解析0／4、−4／4、2／2隔离像元对，分别精确得到2／4／0，原stddev与加权结果逐像元／mask及独立解析值全部相等；非均匀权重用独立逐位置3000位Decimal偏差公式求总体方差再开方，不调用生产一阶／二阶矩或精确平方根实现，不放宽容差或抽样。完整输出严格非负，中心无效不填洞、支持空为NaN、常量／单样本为0，权重方向不翻转；原生NoData先判定，随后沿既有Float64边界，不宣称保留任意64位整数差值。

固定28类各中英文一次共56准确失败：18类权重缺失／null／数值标量／布尔／字符串／对象／空列表／一维／行数错误／列数错误／不规则行／全零／负元素／布尔元素／字符串元素／null元素／对象元素／列表元素；原stddev带权重、variety显式带null权重、weighted_stddev带percentiles三类；未知方法／非法窗口／布尔窗口／波段0／越界波段／选择alpha／复数数据七类。必须绑定同execution真实focal阶段与准确原因，反向语言查询不改提交时错误；失败零发布／新增扫描／派生血缘。数值极限、非标准JSON数值和复数alpha混合类型单列Native；TRI只保留原有效证据，不纳入本套Hosted重跑。

完整复核次数、十源／16成果／16预期归档、当前执行observer约束及清理统一见[三种加权方法共同复核](#三种邻域加权方法的共同复核)。

### 邻域加权均值独立 Hosted 验收

独立手工 `raster-focal-weighted-mean-persistence`；`make test-raster-focal-weighted-mean-online-runner`。准备、成果核验与清理按[共同要求](#邻域与-idw-专项共同要求)。

固定16条COG成功链（14条weighted_mean＋2条mean对照）：非对称3／5各一、全1权重3／5与同源mean3／5共四、非均匀权重同比例放大3／5两条、中心权重零且局部有效支持空、全无效、重分类接加权、旋转网格、缺定位单行、Int64／UInt64原生NoData邻接合法整数、显式第二波段原始scale-offset与mask／部分透明alpha。非对称和同比例／重分类使用519×515跨块样本，其他用例保留实际概览。矩阵行列直接对应不翻转；中心无效不填洞，有效正权重为空输出NaN，合法零和负值保留。全1与mean、同比例权重均核实际成果完整像元／mask一致。预期只用独立逐位置Decimal（2000位）构建、最终Float64舍入，完整像元精确比较，不调用生产算法、不用容差或抽样；Native另覆盖原九方法及数值极限。

固定28类各中英文提交共56次准确失败：权重缺失／null／数值标量／布尔／字符串／对象／空列表／一维列表／行列数错误／不规则行／全零／负元素／布尔元素／字符串元素／null元素／对象元素／列表元素共18类；mean带权重、variety带null权重、weighted_mean带percentiles三类；未知方法、非法窗口、布尔窗口、波段0、越界波段、选择alpha、复数数据七类。必须同execution精确命中focal阶段及真实原因；反向语言查询不改提交语言，失败零发布、零新增扫描／派生血缘。NaN／Infinity及超标准JSON数值、混合类型复数alpha和数值极限单列Native，不用平台前置拒绝充数。

完整复核次数、十源／16成果／16预期归档、当前执行observer约束及清理统一见[三种加权方法共同复核](#三种邻域加权方法的共同复核)。

### 面界提取参数诊断独立实页

`vector-topology-persistence` 新增 `polygon-rings-input-diagnostics` case，仅执行ring_id固定字段重名与实际无效面的两次真实失败，不执行旧成功链、旧参数诊断四场景或容量。复用原Hosted生命周期、普通User精确11权限、非默认Tenant和600秒业务预算；只准备rings-conflict.gpkg与rings-invalid.gpkg两源，保留实际parcel_shape及普通geometry业务列。失败节点置于冻结tasks数组首位，在load之后调度，下游save必须未执行。两次Develop及Monitor响应均须绑定当前UUID/同一User/冻结定义，准确返回v3、execution、INVALID_PARAMS对应develop.workflow.invalid_params；重名仅field=ring_id，无效面仅operator_geometry_invalid且无field。

两执行分别在Develop/Monitor中英文实页刷新同一UUID，共8份原始响应、截图及真实剪贴板核验；复用现有唯一共享面板和浏览器当前请求身份绑定，不持有跨导航旧Token。核准确原因、人工重命名或先几何检查后显式修复建议、算子/节点/参数/固定字段、稳定码和UUID；不得包含自定义节点名、业务ID/值、坐标、路径或GEOS正文。两源物理字节/大小/SHA及Meta始终不变；两初始扫描库存不变、无新增文件/成果扫描/输出facts/本次derive。保存全部原始证据和源文件，最终报告不能由incomplete checkpoint替代；退出NFS/export/ownedroot/Runtime/registry/应用/Infra全部清0，不把清理当业务DELETE验收。该case更新原runner/Native/浏览器和CI登记，不新增suite、权限、产品API或容量入口。


### 线端修剪独立普通User验收

`vector-topology-persistence` 新增显式 `line-trimming` case，仅一成功与八失败，不执行full、旧业务T4或容量。复用Hosted隔离生命周期、同一非默认Tenant普通User精确11权限和600秒总业务预算。成功双源EPSG:3857、max_distance=4，源15条MultiLineString/16部件及14条参考要素；手写15条线结果与9条目标Point明细，覆盖原始两端冻结、累计路径而非直线距离、上限等号/略超界、首次交会/共线入口、同点全部来源/单参考重复段去重、分支阻止/命中分支、重复内点/部件/方向、已接触/闭环/非悬端/无目标不动及2^-20精确微距。业务ID为JSON安全Int64且不同于FID，真实source_shape/reference_shape与普通geometry业务列共存；可空Int64/Boolean/String属性保持。活动几何列名保留属于算子内存结果断言；GPKG物理几何名沿既有save驱动声明，文件及预览按实际声明绑定，不新增持久列名承诺。Point位于切点，原端位置保存在original_x/y，明细仅保双方ID、part_no/end、原端/切点XY及累计distance，不复制任意业务属性。

八次独立失败为非正上限、源ID非法、参考ID非法、CRS不一致、单部件自交、源正长度重合、原始两端计划相遇、首交点不可精确表示且更晚有可表示交点。前七次按既有v3闭合参数诊断核精确原因/参数/冻结节点，无field或私有值；第八次保持EXECUTION_FAILED，并以同UUID隔离owner日志核首交点不能精确落线，不冒充参数错误。每DAG均含两个下游save；冲突夹具先有可修剪要素，仍须整批无发布。每次核Develop/Monitor同User/UUID失败、失败目标/输出事实/自动扫描/本次derive不存在、扫描库存不增、全部源与成功成果原字节/Meta不变。

成功两稳定输出及facts均create，恰两成果自动扫描公开严格整数tenant_id/triggered_by同主体；每成果两源本execution active derive，共四边，失败夹具不进入成功图。逐原坐标/全部属性类型与空值/行序/方向/部件/3857独立OGR重载，不调用生产算法生成期望、不epsilon或舍入。11源与双成果原文件保存供归档审计，物理与Meta大小严格为相等正整数。真实Console核成功执行、线与Point两地图/属性/血缘页及八次Develop/Monitor失败页；原EWKB、完整OL坐标/属性、明显切点见证像素/Point图层像素、原图和截图归档，每次导航绑定当前浏览器请求身份。微距仅核物理/API/OL，不宣称亚像素单独可见；不认证内存索引、中间算子资源血缘、跨文件事务、业务DELETE、空批/英尺或容量。最终Owner/browser/raw报告同case/run/execution完整passed，incomplete仅阶段意义；源及成果末轮不变、NFS/export/ownedroot/Runtime/registry/部署/Infra清零。现有runner/Native/Console/CI登记同次适配，通过最小标准门禁后单次手工派发此case。

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
