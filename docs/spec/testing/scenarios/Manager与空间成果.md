# ADDP Online 场景：Manager与空间成果

[返回测试与验收总入口](../../addp测试与验收规范.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

专项导读：[文件读取公共准备与收尾](Manager文件与预览.md#文件读取-case-的公共准备与收尾) · [公共前提](#独立持久化专项公共前提) · [Runner／Native 验证入口](#独立持久化专项验证入口) · [水文与分区统计共同要求](#水文与分区统计专项共同要求) · [水文固定结果与待验](D8水文分析.md#水文专项的固定结果与待验边界) · [邻域与IDW共同要求](邻域统计与IDW.md#邻域与-idw-专项共同要求)

本页场景导航仅用于定位，适用范围以各场景正文为准：

| 内容 | 场景入口 |
| --- | --- |
| Manager 与文件 | [内部产物与 case](Manager文件与预览.md#manager-internal-artifact-lineage) · [混合检索](#manager-hybrid-search) · [KML／GML／TopoJSON S3](Manager文件与预览.md#kmlgmltopojson-的-s3-兼容消费) · [设备静态轨迹](Manager文件与预览.md#设备静态轨迹范围读取) |
| 表格读取 | [ORC 单文件](Manager文件与预览.md#orc-单文件表格-hosted) · [Avro 单文件](Manager文件与预览.md#avro-单文件表格读取) · [Feather V2](Manager文件与预览.md#feather-v2-基础标量独立-hosted) · [CSV／TSV GB18030 读取设置](Manager文件与预览.md#csvtsv-gb18030-独立读取设置-case) |
| 表格历史证据 | [ORC 平坦传输／嵌套只读](Manager文件与预览.md#orc-分层验证与历史证据) · [Avro 首轮／复验](Manager文件与预览.md#avro-验证与历史证据) · [Feather 分版本](Manager文件与预览.md#feather-分版本证据) · [GB18030 本地／Hosted](Manager文件与预览.md#gb18030-分层验证与历史证据) |
| 媒体与容器读取 | [NetCDF](Manager文件与预览.md#netcdf-单文件只读) · [GRIB2](Manager文件与预览.md#grib2-单文件只读) · [ENVI 双文件 COG](Manager文件与预览.md#envi-双文件-cog限定-hosted-已验收) · [Surfer GRD](Manager文件与预览.md#surfer-grd-读取) |
| 媒体读取历史证据 | [NetCDF 授权契约适配](../evidence/资源读取与预览历史证据.md#netcdf-当前元数据授权契约适配--限定正式-hosted-通过) · [GRIB2 数值与页面](../evidence/资源读取与预览历史证据.md#grib2-单文件读取与切片--数值通过及页面收口) · [ENVI 精确组成](../evidence/资源读取与预览历史证据.md#envi-精确双文件现行权限正式复验通过2026-11-04) · [ENVI MinIO 节点](../evidence/资源读取与预览历史证据.md#envi-minio-持续节点新建文件及到期独立-hosted-通过2026-11-04) · [Surfer 四份 GRD](../evidence/资源读取与预览历史证据.md#surfer-6-ascii-扩展及四份-grd-读取--普通-user-hosted-t4) |
| 矢量与地图 | [拓扑持久化](矢量拓扑持久化.md#拓扑构面面叠面缝悬点与线叠持久化vector-topology-persistence) · [重合规整](矢量拓扑持久化.md#重合规整独立场景) · [PMTiles 工作流](#vector-pmtiles-workflow) |
| 栅格流程与持久化 | [分类栅格与形态处理](分类栅格与形态处理.md) · [Raster 工作流](栅格工作流.md#raster-workflow) · [等值线](#raster-contour-persistence) · [分类转面](#raster-polygonize-persistence) · [矢量栅格化](#vector-rasterize-persistence) · [整数分区统计](分区统计.md#raster-zonal-statistics-persistence) · [邻域统计](邻域统计与IDW.md#raster-focal-statistics-persistence) · [邻域百分位](邻域统计与IDW.md#raster-focal-percentile-persistence) · [邻域分类种类数](邻域统计与IDW.md#raster-focal-variety-persistence) · [筛除分类保护](分类栅格与形态处理.md#分类小斑块筛除指定类别保护独立-hosted-验收) · [筛除面积阈值](分类栅格与形态处理.md#分类小斑块筛除平方米面积阈值独立-hosted-验收) · [矢量分区统计](分区统计.md#vector-zonal-statistics-persistence) · [邻近距离](邻近距离.md#raster-proximity-persistence) · [细化](分类栅格与形态处理.md#raster-thin-persistence) · [边界清理](分类栅格与形态处理.md#raster-boundary-clean-persistence) |
| 邻域与插值专项 | [三种加权方法共同复核](邻域统计与IDW.md#三种邻域加权方法的共同复核) · [7×7方窗](邻域统计与IDW.md#77-像元方窗独立-hosted-验收) · [米制圆窗](邻域统计与IDW.md#米制圆形邻域统计独立验收) · [多百分位](邻域统计与IDW.md#邻域多百分位独立保存验收) · [IDW基础](邻域统计与IDW.md#点数据idw插值独立-hosted-验收) · [最少支撑](邻域统计与IDW.md#idw最少支撑点数独立-hosted-验收) · [最近N点](邻域统计与IDW.md#idw最近n个支撑点独立-hosted-验收) · [距离幂次](邻域统计与IDW.md#idw距离幂次独立-hosted-验收) |
| 邻近距离专项 | [基础方形](邻近距离.md#raster-proximity-persistence) · [北向非方形](邻近距离.md#非方形像元邻近距离独立验收) · [正交旋转](邻近距离.md#正交旋转网格邻近距离独立保存) |
| 整幅只读统计 | [单元素百分位](整幅栅格百分位统计.md#整幅栅格百分位统计独立-hosted-验收) · [多百分位](整幅栅格百分位统计.md#整幅栅格多百分位独立-hosted-验收) |
| 栅格采样 | [点位原值](栅格采样.md#点位栅格采样独立验收) · [双线性点位](栅格采样.md#双线性点位栅格采样独立验收) · [沿线采样](栅格采样.md#沿线栅格采样持久化) |
| 点／线密度专项 | [共同要求与数量矩阵](点线密度.md#点线密度专项共同要求) · [点权重](点线密度.md#点密度显式非负权重独立-hosted-验收) · [点核密度](点线密度.md#米制点核密度独立-hosted-验收) · [线核密度](点线密度.md#米制四次线核密度独立-hosted-验收) · [线权重](点线密度.md#线密度显式非负权重独立-hosted-验收) · [uniform线](点线密度.md#米制圆形线密度栅格独立-hosted-验收) · [uniform点](点线密度.md#米制圆形点密度栅格独立-hosted-验收) |
| 容量与磁盘 | [分类蚕食容量](#分类蚕食容量raster-nibble-capacity) · [容量验证入口](#分类蚕食容量的验证入口) · [临时磁盘保护](#raster-disk-pressure) |
| DEM 通视与可视域 | [两点通视](DEM通视与可视域.md#dem-两点通视独立-hosted-验收) · [单观察点可视域](DEM通视与可视域.md#dem-单观察点可视域独立-hosted-验收) · [正交旋转](DEM通视与可视域.md#正交旋转网格通视与可视域独立-hosted-验收) |
| DEM 地形与统计 | [填洼](D8水文分析.md#raster-fill-depressions-persistence) · [平均曲率](DEM曲率与地形指标.md#dem-平均曲率独立-hosted-验收) · [剖面曲率](DEM曲率与地形指标.md#dem-剖面曲率独立-hosted-验收) · [切向曲率](DEM曲率与地形指标.md#dem-切向曲率独立-hosted-验收) · [平面曲率](DEM曲率与地形指标.md#dem-平面曲率独立-hosted-验收) · [双 DEM 填挖方](#双dem填挖方统计独立-hosted-验收) · [TPI](DEM曲率与地形指标.md#dem-地形位置指数tpi独立-hosted-验收) · [TRI](DEM曲率与地形指标.md#dem-地形崎岖度tri独立-hosted-验收) · [粗糙度](DEM曲率与地形指标.md#dem-地形粗糙度独立-hosted-验收) |
| D8 流向与累积 | [流向](D8水文分析.md#dem-d8-流向持久化) · [汇流累积](D8水文分析.md#d8-汇流累积持久化) · [加权累积](D8水文分析.md#raster-flow-accumulation-weighted-d8-persistence) · [下游流长](D8水文分析.md#d8-下游流长持久化) · [上游最长流长](D8水文分析.md#d8-上游最长流长持久化) · [汇水分区](D8水文分析.md#d8-汇水分区持久化) |
| D8 河网与汇水 | [Strahler](D8水文分析.md#raster-stream-order-d8-persistence) · [Shreve](D8水文分析.md#shreve-独立持久化验收) · [河段编号](D8水文分析.md#d8-河段编号独立持久化) · [河网转线](D8水文分析.md#d8-河网转矢量线独立-hosted-验收) · [指定汇水点](D8水文分析.md#raster-watershed-d8-persistence) · [汇水点吸附](D8水文分析.md#raster-snap-pour-points-persistence) · [增量子汇水区](D8水文分析.md#raster-subcatchments-d8-persistence) |
| D8 面积、距离与高程 | [汇流面积](D8水文分析.md#raster-flow-accumulation-area-d8-persistence) · [到河网沿流距离](D8水文分析.md#raster-distance-to-stream-d8-persistence) · [HAND](D8水文分析.md#raster-hand-d8-persistence) |

## Manager 文件与预览

现行正文见[Manager 文件与预览专题](Manager文件与预览.md)。以下保留原链接定位；身份、权限、固定断言、历史证据与未覆盖范围均由专题正文维护。

- <a id="grib2-单文件只读"></a>[GRIB2 单文件只读](Manager文件与预览.md#grib2-单文件只读)
- <a id="grib2-授权与来源"></a>[GRIB2 授权与来源](Manager文件与预览.md#grib2-授权与来源)
- <a id="grib2-逐消息数值"></a>[GRIB2 逐消息数值](Manager文件与预览.md#grib2-逐消息数值)
- <a id="grib2-页面切片与原件"></a>[GRIB2 页面切片与原件](Manager文件与预览.md#grib2-页面切片与原件)
- <a id="grib2-清理与未覆盖项"></a>[GRIB2 清理与未覆盖项](Manager文件与预览.md#grib2-清理与未覆盖项)
- <a id="grib2-验证与历史证据"></a>[GRIB2 验证与历史证据](Manager文件与预览.md#grib2-验证与历史证据)
- <a id="netcdf-单文件只读"></a>[NetCDF 单文件只读](Manager文件与预览.md#netcdf-单文件只读)
- <a id="netcdf-来源与权限"></a>[NetCDF 来源与权限](Manager文件与预览.md#netcdf-来源与权限)
- <a id="netcdf-变量与切片"></a>[NetCDF 变量与切片](Manager文件与预览.md#netcdf-变量与切片)
- <a id="netcdf-页面与原件"></a>[NetCDF 页面与原件](Manager文件与预览.md#netcdf-页面与原件)
- <a id="netcdf-清理与未覆盖项"></a>[NetCDF 清理与未覆盖项](Manager文件与预览.md#netcdf-清理与未覆盖项)
- <a id="netcdf-验证与历史证据"></a>[NetCDF 验证与历史证据](Manager文件与预览.md#netcdf-验证与历史证据)
- <a id="manager-internal-artifact-lineage"></a>[manager-internal-artifact-lineage](Manager文件与预览.md#manager-internal-artifact-lineage)
- <a id="明确的验收-case"></a>[明确的验收 case](Manager文件与预览.md#明确的验收-case)
- <a id="文件读取-case-的公共准备与收尾"></a>[文件读取 case 的公共准备与收尾](Manager文件与预览.md#文件读取-case-的公共准备与收尾)
- <a id="surfer-grd-读取"></a>[Surfer GRD 读取](Manager文件与预览.md#surfer-grd-读取)
- <a id="grd-格式准入"></a>[GRD 格式准入](Manager文件与预览.md#grd-格式准入)
- <a id="grd-四份样例"></a>[GRD 四份样例](Manager文件与预览.md#grd-四份样例)
- <a id="grd-页面与原件"></a>[GRD 页面与原件](Manager文件与预览.md#grd-页面与原件)
- <a id="grd-清理与能力边界"></a>[GRD 清理与能力边界](Manager文件与预览.md#grd-清理与能力边界)
- <a id="grd-分版本证据"></a>[GRD 分版本证据](Manager文件与预览.md#grd-分版本证据)
- <a id="kml-实际页面导出"></a>[KML 实际页面导出](Manager文件与预览.md#kml-实际页面导出)
- <a id="avro-单文件表格读取"></a>[Avro 单文件表格读取](Manager文件与预览.md#avro-单文件表格读取)
- <a id="avro-来源与权限"></a>[Avro 来源与权限](Manager文件与预览.md#avro-来源与权限)
- <a id="avro-三种-codec"></a>[Avro 三种 codec](Manager文件与预览.md#avro-三种-codec)
- <a id="avro-分页与批量传输"></a>[Avro 分页与批量传输](Manager文件与预览.md#avro-分页与批量传输)
- <a id="avro-清理与未覆盖项"></a>[Avro 清理与未覆盖项](Manager文件与预览.md#avro-清理与未覆盖项)
- <a id="avro-验证与历史证据"></a>[Avro 验证与历史证据](Manager文件与预览.md#avro-验证与历史证据)
- <a id="orc-单文件表格-hosted"></a>[ORC 单文件表格 Hosted](Manager文件与预览.md#orc-单文件表格-hosted)
- <a id="orc-入口与权限"></a>[ORC 入口与权限](Manager文件与预览.md#orc-入口与权限)
- <a id="orc-平坦字段与批量传输"></a>[ORC 平坦字段与批量传输](Manager文件与预览.md#orc-平坦字段与批量传输)
- <a id="orc-嵌套对象与数组只读"></a>[ORC 嵌套对象与数组只读](Manager文件与预览.md#orc-嵌套对象与数组只读)
- <a id="orc-源变化与读取限制"></a>[ORC 源变化与读取限制](Manager文件与预览.md#orc-源变化与读取限制)
- <a id="orc-清理与报告"></a>[ORC 清理与报告](Manager文件与预览.md#orc-清理与报告)
- <a id="orc-分层验证与历史证据"></a>[ORC 分层验证与历史证据](Manager文件与预览.md#orc-分层验证与历史证据)
- <a id="feather-v2-基础标量独立-hosted"></a>[Feather V2 基础标量独立 Hosted](Manager文件与预览.md#feather-v2-基础标量独立-hosted)
- <a id="feather-身份与基础标量"></a>[Feather 身份与基础标量](Manager文件与预览.md#feather-身份与基础标量)
- <a id="feather-时间字段"></a>[Feather 时间字段](Manager文件与预览.md#feather-时间字段)
- <a id="feather-日期与定点小数"></a>[Feather 日期与定点小数](Manager文件与预览.md#feather-日期与定点小数)
- <a id="feather-清理与未覆盖项"></a>[Feather 清理与未覆盖项](Manager文件与预览.md#feather-清理与未覆盖项)
- <a id="feather-分版本证据"></a>[Feather 分版本证据](Manager文件与预览.md#feather-分版本证据)
- <a id="csvtsv-gb18030-独立读取设置-case"></a>[CSV/TSV GB18030 独立读取设置 case](Manager文件与预览.md#csvtsv-gb18030-独立读取设置-case)
- <a id="gb18030-身份与来源准备"></a>[GB18030 身份与来源准备](Manager文件与预览.md#gb18030-身份与来源准备)
- <a id="gb18030-结构与内容投影"></a>[GB18030 结构与内容投影](Manager文件与预览.md#gb18030-结构与内容投影)
- <a id="gb18030-页面读取与原件"></a>[GB18030 页面读取与原件](Manager文件与预览.md#gb18030-页面读取与原件)
- <a id="gb18030-清理与能力边界"></a>[GB18030 清理与能力边界](Manager文件与预览.md#gb18030-清理与能力边界)
- <a id="gb18030-分层验证与历史证据"></a>[GB18030 分层验证与历史证据](Manager文件与预览.md#gb18030-分层验证与历史证据)
- <a id="filegdb-实际页面导出"></a>[FileGDB 实际页面导出](Manager文件与预览.md#filegdb-实际页面导出)
- <a id="mysql-filegdb-实际页面导出"></a>[MySQL FileGDB 实际页面导出](Manager文件与预览.md#mysql-filegdb-实际页面导出)
- <a id="tiff-无定位小图的-cog-布局回归"></a>[TIFF 无定位小图的 COG 布局回归](Manager文件与预览.md#tiff-无定位小图的-cog-布局回归)
- <a id="tiff-minio-无定位小图"></a>[TIFF MinIO 无定位小图](Manager文件与预览.md#tiff-minio-无定位小图)
- <a id="tiff-nfs-渲染与自然到期"></a>[TIFF NFS 渲染与自然到期](Manager文件与预览.md#tiff-nfs-渲染与自然到期)
- <a id="tiff-msk-组成扩展"></a>[TIFF MSK 组成扩展](Manager文件与预览.md#tiff-msk-组成扩展)
- <a id="tiff-组成的浏览器画布断言"></a>[TIFF 组成的浏览器画布断言](Manager文件与预览.md#tiff-组成的浏览器画布断言)
- <a id="tiff-清理与共同边界"></a>[TIFF 清理与共同边界](Manager文件与预览.md#tiff-清理与共同边界)
- <a id="tiff-分版本证据"></a>[TIFF 分版本证据](Manager文件与预览.md#tiff-分版本证据)
- <a id="envi-双文件-cog限定-hosted-已验收"></a>[ENVI 双文件 COG（限定 Hosted 已验收）](Manager文件与预览.md#envi-双文件-cog限定-hosted-已验收)
- <a id="envi-身份与来源"></a>[ENVI 身份与来源](Manager文件与预览.md#envi-身份与来源)
- <a id="envi-精确组成授权"></a>[ENVI 精确组成授权](Manager文件与预览.md#envi-精确组成授权)
- <a id="envi-minio-持续节点授权"></a>[ENVI MinIO 持续节点授权](Manager文件与预览.md#envi-minio-持续节点授权)
- <a id="envi-cog-与缓存"></a>[ENVI COG 与缓存](Manager文件与预览.md#envi-cog-与缓存)
- <a id="envi-撤权恢复与组成变化"></a>[ENVI 撤权恢复与组成变化](Manager文件与预览.md#envi-撤权恢复与组成变化)
- <a id="envi-清理与失败"></a>[ENVI 清理与失败](Manager文件与预览.md#envi-清理与失败)
- <a id="envi-验证与历史证据"></a>[ENVI 验证与历史证据](Manager文件与预览.md#envi-验证与历史证据)
- <a id="img-原生扫描与-cog-快显"></a>[IMG 原生扫描与 COG 快显](Manager文件与预览.md#img-原生扫描与-cog-快显)
- <a id="imgjpeg2000-正文分工与来源"></a>[IMG／JPEG2000 正文分工与来源](Manager文件与预览.md#imgjpeg2000-正文分工与来源)
- <a id="imgjpeg2000-cog-与缓存"></a>[IMG／JPEG2000 COG 与缓存](Manager文件与预览.md#imgjpeg2000-cog-与缓存)
- <a id="imgjpeg2000-页面与原件"></a>[IMG／JPEG2000 页面与原件](Manager文件与预览.md#imgjpeg2000-页面与原件)
- <a id="imgjpeg2000-清理与验证"></a>[IMG／JPEG2000 清理与验证](Manager文件与预览.md#imgjpeg2000-清理与验证)
- <a id="imgjpeg2000-历史证据与未覆盖项"></a>[IMG／JPEG2000 历史证据与未覆盖项](Manager文件与预览.md#imgjpeg2000-历史证据与未覆盖项)
- <a id="部署模型转换与通用清理"></a>[部署、模型转换与通用清理](Manager文件与预览.md#部署模型转换与通用清理)
- <a id="点云与-pptx"></a>[点云与 PPTX](Manager文件与预览.md#点云与-pptx)
- <a id="彩色-fbx-只读预览"></a>[彩色 FBX 只读预览](Manager文件与预览.md#彩色-fbx-只读预览)
- <a id="max-模型-hosted-验收"></a>[MAX 模型 Hosted 验收](Manager文件与预览.md#max-模型-hosted-验收)
- <a id="kmlgmltopojson-的-s3-兼容消费"></a>[KML／GML／TopoJSON 的 S3 兼容消费](Manager文件与预览.md#kmlgmltopojson-的-s3-兼容消费)
- <a id="设备静态轨迹范围读取"></a>[设备静态轨迹范围读取](Manager文件与预览.md#设备静态轨迹范围读取)
- <a id="表格json-与八模型固定断言"></a>[表格、JSON 与八模型固定断言](Manager文件与预览.md#表格json-与八模型固定断言)

## 矢量拓扑持久化

现行正文见[矢量拓扑持久化专题](矢量拓扑持久化.md)。以下保留旧链接定位，主场景、独立 case、权限、清理与验收边界均由专题正文维护。

- <a id="拓扑构面面叠面缝悬点与线叠持久化vector-topology-persistence"></a>[拓扑构面、面叠面缝、悬点与线叠持久化（vector-topology-persistence）](矢量拓扑持久化.md#拓扑构面面叠面缝悬点与线叠持久化vector-topology-persistence)
- <a id="拓扑持久化公共要求"></a>[拓扑持久化公共要求](矢量拓扑持久化.md#拓扑持久化公共要求)
- <a id="主场景的固定输入输出与扫描血缘"></a>[主场景的固定输入、输出与扫描血缘](矢量拓扑持久化.md#主场景的固定输入输出与扫描血缘)
- <a id="主场景的独有几何断言"></a>[主场景的独有几何断言](矢量拓扑持久化.md#主场景的独有几何断言)
- <a id="主场景报告失败证据与历史范围"></a>[主场景报告、失败证据与历史范围](矢量拓扑持久化.md#主场景报告失败证据与历史范围)
- <a id="多距缓冲独立场景"></a>[多距缓冲独立场景](矢量拓扑持久化.md#多距缓冲独立场景)
- <a id="重合规整独立场景"></a>[重合规整独立场景](矢量拓扑持久化.md#重合规整独立场景)
- <a id="重合规整与链路合并独立组合"></a>[重合规整与链路合并独立组合](矢量拓扑持久化.md#重合规整与链路合并独立组合)
- <a id="面界提取独立普通用户场景"></a>[面界提取独立普通用户场景](矢量拓扑持久化.md#面界提取独立普通用户场景)
- <a id="面界提取参数诊断独立实页"></a>[面界提取参数诊断独立实页](矢量拓扑持久化.md#面界提取参数诊断独立实页)
- <a id="线端修剪独立普通user验收"></a>[线端修剪独立普通User验收](矢量拓扑持久化.md#线端修剪独立普通user验收)
- <a id="算子参数错误独立普通-user-实页"></a>[算子参数错误独立普通 User 实页](矢量拓扑持久化.md#算子参数错误独立普通-user-实页)
- <a id="近接检查独立双源-point-成果"></a>[近接检查独立双源 Point 成果](矢量拓扑持久化.md#近接检查独立双源-point-成果)
- <a id="线端延伸独立双源双成果"></a>[线端延伸独立双源双成果](矢量拓扑持久化.md#线端延伸独立双源双成果)
- <a id="端点吸附独立线与point双成果"></a>[端点吸附独立线与Point双成果](矢量拓扑持久化.md#端点吸附独立线与point双成果)
- <a id="差集融合叠加与节点提取的专项授权边界"></a>[差集、融合、叠加与节点提取的专项授权边界](矢量拓扑持久化.md#差集融合叠加与节点提取的专项授权边界)
- <a id="几何差集独立普通用户验收"></a>[几何差集独立普通用户验收](矢量拓扑持久化.md#几何差集独立普通用户验收)
- <a id="融合几何独立普通用户验收"></a>[融合几何独立普通用户验收](矢量拓扑持久化.md#融合几何独立普通用户验收)
- <a id="相交叠加独立普通用户验收"></a>[相交叠加独立普通用户验收](矢量拓扑持久化.md#相交叠加独立普通用户验收)
- <a id="节点提取独立普通用户验收"></a>[节点提取独立普通用户验收](矢量拓扑持久化.md#节点提取独立普通用户验收)
- <a id="nfs-geopackage-血缘权限专项验收"></a>[NFS GeoPackage 血缘权限专项验收](矢量拓扑持久化.md#nfs-geopackage-血缘权限专项验收)
- <a id="顶点提取独立场景"></a>[顶点提取独立场景](矢量拓扑持久化.md#顶点提取独立场景)
- <a id="面邻检查独立普通用户场景polygon-adjacency"></a>[面邻检查独立普通用户场景（polygon-adjacency）](矢量拓扑持久化.md#面邻检查独立普通用户场景polygon-adjacency)
- <a id="面邻准备批准与持久事实"></a>[面邻准备、批准与持久事实](矢量拓扑持久化.md#面邻准备批准与持久事实)
- <a id="面邻只读负例与失败原件"></a>[面邻只读负例与失败原件](矢量拓扑持久化.md#面邻只读负例与失败原件)

## 端点吸附手工容量

既有`make test-vector-topology-capacity`／`vector-topology-capacity.yml`的独立`profile=endpoint-snapping`，五类十五档、每档一次串行，不跑旧矩阵／受控对照／业务T4。3857／容差1／同源码产品镜像Runner，4 CPU／2 GiB／`memory.swap.max=0`、每档全流程180秒；Runtime准备／清理另记，不继承[拓扑业务600秒](矢量拓扑持久化.md#拓扑持久化公共要求)。镜像构建前按根`.node-version`备Node，登记拒缺失／条件跳过／晚准备，不依赖默认版本。

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

## 栅格工作流

现行正文见[栅格工作流专题](栅格工作流.md)。以下保留旧链接定位；镶嵌重扫、主场景、图片导出与计算分支的身份、预算、授权补偿和验收范围均由专题正文维护。

- <a id="raster-workflow"></a>[raster-workflow](栅格工作流.md#raster-workflow)
- <a id="镶嵌-whole-的-basic-重扫收敛"></a>[镶嵌 whole 的 basic 重扫收敛](栅格工作流.md#镶嵌-whole-的-basic-重扫收敛)
- <a id="whole-重扫身份与断言"></a>[whole 重扫身份与断言](栅格工作流.md#whole-重扫身份与断言)
- <a id="whole-元数据授权与受限快照"></a>[whole 元数据授权与受限快照](栅格工作流.md#whole-元数据授权与受限快照)
- <a id="whole-专业-dto-与血缘投影"></a>[whole 专业 DTO 与血缘投影](栅格工作流.md#whole-专业-dto-与血缘投影)
- <a id="部署与主场景"></a>[部署与主场景](栅格工作流.md#部署与主场景)
- <a id="栅格主场景运行准入"></a>[栅格主场景运行准入](栅格工作流.md#栅格主场景运行准入)
- <a id="栅格主场景计算矩阵"></a>[栅格主场景计算矩阵](栅格工作流.md#栅格主场景计算矩阵)
- <a id="栅格主场景元数据授权与事实取证"></a>[栅格主场景元数据授权与事实取证](栅格工作流.md#栅格主场景元数据授权与事实取证)
- <a id="栅格主场景失败补偿与退出"></a>[栅格主场景失败补偿与退出](栅格工作流.md#栅格主场景失败补偿与退出)
- <a id="栅格主场景固定结果与验收边界"></a>[栅格主场景固定结果与验收边界](栅格工作流.md#栅格主场景固定结果与验收边界)
- <a id="普通-pngjpeg-页面导出"></a>[普通 PNG／JPEG 页面导出](栅格工作流.md#普通-pngjpeg-页面导出)
- <a id="多来源计算与页面构图"></a>[多来源计算与页面构图](栅格工作流.md#多来源计算与页面构图)
- <a id="多源加权与rgb合成"></a>[多源加权与RGB合成](栅格工作流.md#多源加权与rgb合成)
- <a id="普通用户页面多选构图"></a>[普通用户页面多选构图](栅格工作流.md#普通用户页面多选构图)
- <a id="分支血缘与源撤权"></a>[分支血缘与源撤权](栅格工作流.md#分支血缘与源撤权)
- <a id="重分类与整数分组聚合"></a>[重分类与整数分组聚合](栅格工作流.md#重分类与整数分组聚合)
- <a id="重分类规则与缺值"></a>[重分类规则与缺值](栅格工作流.md#重分类规则与缺值)
- <a id="整数分组与精度"></a>[整数分组与精度](栅格工作流.md#整数分组与精度)
- <a id="扩展阶段计数与验收边界"></a>[扩展阶段计数与验收边界](栅格工作流.md#扩展阶段计数与验收边界)
- <a id="外部裁剪轮廓与局部更新"></a>[外部裁剪、轮廓与局部更新](栅格工作流.md#外部裁剪轮廓与局部更新)
- <a id="区域外裁剪持久化cog"></a>[区域外裁剪：持久化COG](栅格工作流.md#区域外裁剪持久化cog)
- <a id="有效像元轮廓只读geojson"></a>[有效像元轮廓：只读GeoJSON](栅格工作流.md#有效像元轮廓只读geojson)
- <a id="局部更新持久化cog"></a>[局部更新：持久化COG](栅格工作流.md#局部更新持久化cog)
- <a id="阶段清单与固定结果"></a>[阶段清单与固定结果](栅格工作流.md#阶段清单与固定结果)

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

本节仅适用于下列五项：[矢量栅格化](#vector-rasterize-persistence)、[整数栅格分区统计](分区统计.md#raster-zonal-statistics-persistence)、[邻域统计](邻域统计与IDW.md#raster-focal-statistics-persistence)、[矢量分区统计](分区统计.md#vector-zonal-statistics-persistence)、[邻近距离](邻近距离.md#raster-proximity-persistence)。其他套件仍按各自场景声明执行。

五项均使用独占 Hosted 生命周期、普通 User 原 12 项权限、同源七个 Backend 与默认入口 GeoPython 产品 Runtime，以及共享 NFS；不接管个人服务。Runtime 同路径 bind 写入与 Meta 真实 NFS 读取分别记录。各专项独立手工触发，预检与业务执行共用各自的 600 秒预算，不改变已有套件、产品权限或跨模块产品逻辑。

每轮核对精确输出与扫描清单、源及先前成果不变和 Runtime 执行工作目录清理；失败不得发布目标或新增扫描。成功和失败退出均须核对归属 NFS export／目录、部署及 Infra 零残留。各专项另列的产品 Runtime／镜像／registry 清理项仍须逐项核对。

入口登记、确定性门禁与真实 Hosted 分别记账。固定版本结果和未覆盖范围见[验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)，不外推为当前 HEAD 已重跑。

### 独立持久化专项验证入口

以下分层仍仅适用于上述五项。脚本 Runner 验证夹具、场景断言、隔离生命周期与 CI 登记；五个专项 Runner 均由 `make test-raster-online-runner` 聚合。真实 GDAL 物理校验与篡改拒绝统一沿 `make test-geopython-workflow`，各项范围如下：

| 专项 | 脚本 Runner | Native 物理验证范围 |
| --- | --- | --- |
| [矢量栅格化](#vector-rasterize-persistence) | `make test-vector-rasterize-online-runner` | 几何覆盖、完整 COG 与篡改拒绝 |
| [整数栅格分区统计](分区统计.md#raster-zonal-statistics-persistence) | `make test-raster-zonal-statistics-online-runner` | 真实 GDAL／CSV 与篡改拒绝 |
| [邻域统计](邻域统计与IDW.md#raster-focal-statistics-persistence) | `make test-raster-focal-statistics-online-runner` | 真实 GDAL／COG 与篡改拒绝 |
| [矢量分区统计](分区统计.md#vector-zonal-statistics-persistence) | `make test-vector-zonal-statistics-online-runner` | 真实 GDAL／GeoPackage 与篡改拒绝 |
| [邻近距离](邻近距离.md#raster-proximity-persistence) | `make test-raster-proximity-online-runner` | 真实 GDAL／COG，以及错误像元／mask／单位／网格等篡改拒绝 |

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

## 分区统计

现行正文见[分区统计专题](分区统计.md)。以下保留旧链接定位，栅格／矢量面的基础、加权、百分位与分类频数矩阵及验收边界均由专题正文维护。

- <a id="raster-zonal-statistics-persistence"></a>[raster-zonal-statistics-persistence](分区统计.md#raster-zonal-statistics-persistence)
- <a id="整数分区统计四条成功dag"></a>[整数分区统计四条成功DAG](分区统计.md#整数分区统计四条成功dag)
- <a id="非法波段与不同网格失败"></a>[非法波段与不同网格失败](分区统计.md#非法波段与不同网格失败)
- <a id="独立csv核验与扫描血缘"></a>[独立CSV核验与扫描血缘](分区统计.md#独立csv核验与扫描血缘)
- <a id="固定hosted结果与范围"></a>[固定Hosted结果与范围](分区统计.md#固定hosted结果与范围)
- <a id="栅格分区加权统计独立-hosted-验收"></a>[栅格分区加权统计独立 Hosted 验收](分区统计.md#栅格分区加权统计独立-hosted-验收)
- <a id="栅格分区加权总体标准差独立-hosted-验收"></a>[栅格分区加权总体标准差独立 Hosted 验收](分区统计.md#栅格分区加权总体标准差独立-hosted-验收)
- <a id="栅格分区百分位独立-hosted-验收"></a>[栅格分区百分位独立 Hosted 验收](分区统计.md#栅格分区百分位独立-hosted-验收)
- <a id="栅格分区多百分位独立保存验收"></a>[栅格分区多百分位独立保存验收](分区统计.md#栅格分区多百分位独立保存验收)
- <a id="栅格分区分类频数独立保存验收"></a>[栅格分区分类频数独立保存验收](分区统计.md#栅格分区分类频数独立保存验收)
- <a id="vector-zonal-statistics-persistence"></a>[vector-zonal-statistics-persistence](分区统计.md#vector-zonal-statistics-persistence)
- <a id="三条-geopackage-dag"></a>[三条 GeoPackage DAG](分区统计.md#三条-geopackage-dag)
- <a id="独立物理核验"></a>[独立物理核验](分区统计.md#独立物理核验)
- <a id="精确扫描与执行血缘"></a>[精确扫描与执行血缘](分区统计.md#精确扫描与执行血缘)
### 失败、清理与验收边界

[矢量分区统计的对应正文](分区统计.md#失败清理与验收边界)

- <a id="矢量面分区加权统计独立-hosted-验收"></a>[矢量面分区加权统计独立 Hosted 验收](分区统计.md#矢量面分区加权统计独立-hosted-验收)
- <a id="矢量面分区加权总体标准差独立-hosted-验收"></a>[矢量面分区加权总体标准差独立 Hosted 验收](分区统计.md#矢量面分区加权总体标准差独立-hosted-验收)
- <a id="矢量面分区面积加权统计独立-hosted-验收"></a>[矢量面分区面积加权统计独立 Hosted 验收](分区统计.md#矢量面分区面积加权统计独立-hosted-验收)
- <a id="矢量面分区面积加权总体标准差独立验收"></a>[矢量面分区面积加权总体标准差独立验收](分区统计.md#矢量面分区面积加权总体标准差独立验收)
- <a id="矢量面分区百分位独立-hosted-验收"></a>[矢量面分区百分位独立 Hosted 验收](分区统计.md#矢量面分区百分位独立-hosted-验收)
- <a id="矢量面分区多百分位独立保存验收"></a>[矢量面分区多百分位独立保存验收](分区统计.md#矢量面分区多百分位独立保存验收)
- <a id="矢量面分区分类频数持久化"></a>[矢量面分区分类频数持久化](分区统计.md#矢量面分区分类频数持久化)
- <a id="vector-zonal-class-frequency-persistence"></a>[vector-zonal-class-frequency-persistence](分区统计.md#vector-zonal-class-frequency-persistence)

## 邻域统计与 IDW

现行正文见[邻域统计与 IDW 专题](邻域统计与IDW.md)。以下保留旧链接定位；共同规则、各专项矩阵、独立预期、权限、清理与验收边界均由专题正文维护。

- <a id="邻域与-idw-专项共同要求"></a>[邻域与 IDW 专项共同要求](邻域统计与IDW.md#邻域与-idw-专项共同要求)
- <a id="邻域参数与专项范围"></a>[邻域参数与专项范围](邻域统计与IDW.md#邻域参数与专项范围)
- <a id="三种邻域加权方法的共同复核"></a>[三种邻域加权方法的共同复核](邻域统计与IDW.md#三种邻域加权方法的共同复核)
- <a id="三种加权方法的矩阵与拒绝条件"></a>[三种加权方法的矩阵与拒绝条件](邻域统计与IDW.md#三种加权方法的矩阵与拒绝条件)
- <a id="邻域加权求和独立-hosted-验收"></a>[邻域加权求和独立 Hosted 验收](邻域统计与IDW.md#邻域加权求和独立-hosted-验收)
- <a id="邻域加权总体标准差独立-hosted-验收"></a>[邻域加权总体标准差独立 Hosted 验收](邻域统计与IDW.md#邻域加权总体标准差独立-hosted-验收)
- <a id="邻域加权均值独立-hosted-验收"></a>[邻域加权均值独立 Hosted 验收](邻域统计与IDW.md#邻域加权均值独立-hosted-验收)
- <a id="raster-focal-statistics-persistence"></a>[raster-focal-statistics-persistence](邻域统计与IDW.md#raster-focal-statistics-persistence)
- <a id="邻域输入与六条成功链"></a>[邻域输入与六条成功链](邻域统计与IDW.md#邻域输入与六条成功链)
- <a id="非法波段与窗口失败"></a>[非法波段与窗口失败](邻域统计与IDW.md#非法波段与窗口失败)
- <a id="独立numpy核验"></a>[独立NumPy核验](邻域统计与IDW.md#独立numpy核验)
- <a id="邻域固定证据与未覆盖范围"></a>[邻域固定证据与未覆盖范围](邻域统计与IDW.md#邻域固定证据与未覆盖范围)
- <a id="raster-focal-percentile-persistence"></a>[raster-focal-percentile-persistence](邻域统计与IDW.md#raster-focal-percentile-persistence)
- <a id="百分位16条成功链"></a>[百分位16条成功链](邻域统计与IDW.md#百分位16条成功链)
- <a id="18类输入与36次准确失败"></a>[18类输入与36次准确失败](邻域统计与IDW.md#18类输入与36次准确失败)
- <a id="fraction独立期望与精度"></a>[Fraction独立期望与精度](邻域统计与IDW.md#fraction独立期望与精度)
- <a id="712次全检与原件保真"></a>[712次全检与原件保真](邻域统计与IDW.md#712次全检与原件保真)
- <a id="raster-focal-variety-persistence"></a>[raster-focal-variety-persistence](邻域统计与IDW.md#raster-focal-variety-persistence)
- <a id="种类数成功矩阵与有效性"></a>[种类数成功矩阵与有效性](邻域统计与IDW.md#种类数成功矩阵与有效性)
- <a id="28次准确失败与诊断归属"></a>[28次准确失败与诊断归属](邻域统计与IDW.md#28次准确失败与诊断归属)
- <a id="独立预期与584次全检"></a>[独立预期与584次全检](邻域统计与IDW.md#独立预期与584次全检)
- <a id="冻结归档与验收边界"></a>[冻结归档与验收边界](邻域统计与IDW.md#冻结归档与验收边界)
- <a id="77-像元方窗独立-hosted-验收"></a>[7×7 像元方窗独立 Hosted 验收](邻域统计与IDW.md#77-像元方窗独立-hosted-验收)
- <a id="米制圆形邻域统计独立验收"></a>[米制圆形邻域统计独立验收](邻域统计与IDW.md#米制圆形邻域统计独立验收)
- <a id="正交旋转网格米制圆形邻域统计独立验收"></a>[正交旋转网格米制圆形邻域统计独立验收](邻域统计与IDW.md#正交旋转网格米制圆形邻域统计独立验收)
- <a id="米制加权圆窗独立-hosted-验收"></a>[米制加权圆窗独立 Hosted 验收](邻域统计与IDW.md#米制加权圆窗独立-hosted-验收)
- <a id="点数据idw插值独立-hosted-验收"></a>[点数据IDW插值独立 Hosted 验收](邻域统计与IDW.md#点数据idw插值独立-hosted-验收)
- <a id="idw最少支撑点数独立-hosted-验收"></a>[IDW最少支撑点数独立 Hosted 验收](邻域统计与IDW.md#idw最少支撑点数独立-hosted-验收)
- <a id="idw距离幂次独立-hosted-验收"></a>[IDW距离幂次独立 Hosted 验收](邻域统计与IDW.md#idw距离幂次独立-hosted-验收)
- <a id="idw最近n个支撑点独立-hosted-验收"></a>[IDW最近N个支撑点独立 Hosted 验收](邻域统计与IDW.md#idw最近n个支撑点独立-hosted-验收)
- <a id="邻域多百分位独立保存验收"></a>[邻域多百分位独立保存验收](邻域统计与IDW.md#邻域多百分位独立保存验收)

## 邻近距离

三套网格场景、单位、独立核验与各自验收范围见[邻近距离专题](邻近距离.md)。原定位保留如下。

- <a id="raster-proximity-persistence"></a>[raster-proximity-persistence](邻近距离.md#raster-proximity-persistence)
- <a id="单位与网格"></a>[单位与网格](邻近距离.md#单位与网格)
- <a id="独立距离核验与扫描血缘"></a>[独立距离核验与扫描血缘](邻近距离.md#独立距离核验与扫描血缘)
- <a id="失败清理与验收边界-1"></a>[失败、清理与验收边界](邻近距离.md#失败清理与验收边界)
- <a id="非方形像元邻近距离独立验收"></a>[非方形像元邻近距离独立验收](邻近距离.md#非方形像元邻近距离独立验收)
- <a id="正交旋转网格邻近距离独立保存"></a>[正交旋转网格邻近距离独立保存](邻近距离.md#正交旋转网格邻近距离独立保存)

## 分类栅格与形态处理

七项场景的矩阵、独立核验、固定证据与未覆盖项见[分类栅格与形态处理专题](分类栅格与形态处理.md)。原有定位保留如下。

- <a id="分类栅格小斑块筛除独立-hosted-验收"></a>[分类栅格小斑块筛除独立 Hosted 验收](分类栅格与形态处理.md#分类栅格小斑块筛除独立-hosted-验收)
- <a id="分类小斑块筛除平方米面积阈值独立-hosted-验收"></a>[分类小斑块筛除平方米面积阈值独立 Hosted 验收](分类栅格与形态处理.md#分类小斑块筛除平方米面积阈值独立-hosted-验收)
- <a id="分类小斑块筛除指定类别保护独立-hosted-验收"></a>[分类小斑块筛除指定类别保护独立 Hosted 验收](分类栅格与形态处理.md#分类小斑块筛除指定类别保护独立-hosted-验收)
- <a id="raster-thin-persistence"></a>[raster-thin-persistence](分类栅格与形态处理.md#raster-thin-persistence)
- <a id="raster-boundary-clean-persistence"></a>[raster-boundary-clean-persistence](分类栅格与形态处理.md#raster-boundary-clean-persistence)
- <a id="raster-boundary-clean-two-rounds-persistence"></a>[raster-boundary-clean-two-rounds-persistence](分类栅格与形态处理.md#raster-boundary-clean-two-rounds-persistence)
- <a id="分类栅格连通斑块编号独立验收"></a>[分类栅格连通斑块编号独立验收](分类栅格与形态处理.md#分类栅格连通斑块编号独立验收)

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
- **验证与证据**：各专项 Runner 沿既有 `make test-raster-online-runner` 聚合，真实 GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`；新增输入须同次命中 Native、Online、影响选择与 CI。专项筛除、共享重复计数和隔离身份 Go T1 不算全量或完整 IAM／数据库集成。现场完整物理回执与归档后离线复算分开；无原 TIFF 的记录不能声称离线重读。已验与待验按固定源码分别认定，不外推 UI、容量、业务 DELETE、其他 Owner 或生产。 水文已有结果与缺口见[固定结果与待验边界](D8水文分析.md#水文专项的固定结果与待验边界)。

## D8 水文分析

场景矩阵、独立核验、固定结果及未覆盖项见[D8 水文分析专题](D8水文分析.md)。公共要求继续由上节维护，以下保留原定位。

- <a id="水文专项的固定结果与待验边界"></a>[水文专项的固定结果与待验边界](D8水文分析.md#水文专项的固定结果与待验边界)
- <a id="raster-fill-depressions-persistence"></a>[raster-fill-depressions-persistence](D8水文分析.md#raster-fill-depressions-persistence)
- <a id="dem-d8-流向持久化"></a>[DEM D8 流向持久化](D8水文分析.md#dem-d8-流向持久化)
- <a id="d8-汇流累积持久化"></a>[D8 汇流累积持久化](D8水文分析.md#d8-汇流累积持久化)
- <a id="d8-下游流长持久化"></a>[D8 下游流长持久化](D8水文分析.md#d8-下游流长持久化)
- <a id="d8-汇水分区持久化"></a>[D8 汇水分区持久化](D8水文分析.md#d8-汇水分区持久化)
- <a id="d8-上游最长流长持久化"></a>[D8 上游最长流长持久化](D8水文分析.md#d8-上游最长流长持久化)
- <a id="raster-stream-order-d8-persistence"></a>[raster-stream-order-d8-persistence](D8水文分析.md#raster-stream-order-d8-persistence)
- <a id="raster-flow-accumulation-weighted-d8-persistence"></a>[raster-flow-accumulation-weighted-d8-persistence](D8水文分析.md#raster-flow-accumulation-weighted-d8-persistence)
- <a id="raster-watershed-d8-persistence"></a>[raster-watershed-d8-persistence](D8水文分析.md#raster-watershed-d8-persistence)
- <a id="d8-河段编号独立持久化"></a>[D8 河段编号独立持久化](D8水文分析.md#d8-河段编号独立持久化)
- <a id="shreve-独立持久化验收"></a>[Shreve 独立持久化验收](D8水文分析.md#shreve-独立持久化验收)
- <a id="d8-河网转矢量线独立-hosted-验收"></a>[D8 河网转矢量线独立 Hosted 验收](D8水文分析.md#d8-河网转矢量线独立-hosted-验收)
- <a id="raster-snap-pour-points-persistence"></a>[raster-snap-pour-points-persistence](D8水文分析.md#raster-snap-pour-points-persistence)
- <a id="raster-subcatchments-d8-persistence"></a>[raster-subcatchments-d8-persistence](D8水文分析.md#raster-subcatchments-d8-persistence)
- <a id="raster-flow-accumulation-area-d8-persistence"></a>[raster-flow-accumulation-area-d8-persistence](D8水文分析.md#raster-flow-accumulation-area-d8-persistence)
- <a id="raster-distance-to-stream-d8-persistence"></a>[raster-distance-to-stream-d8-persistence](D8水文分析.md#raster-distance-to-stream-d8-persistence)
- <a id="raster-hand-d8-persistence"></a>[raster-hand-d8-persistence](D8水文分析.md#raster-hand-d8-persistence)
- <a id="d8流长统一入口的既有验收适配"></a>[D8流长统一入口的既有验收适配](D8水文分析.md#d8流长统一入口的既有验收适配)

## DEM 曲率与地形指标

现行矩阵、精度要求、固定证据与未覆盖项见[DEM 曲率与地形指标专题](DEM曲率与地形指标.md)。以下保留原定位。

- <a id="平均剖面与切向曲率共同验收边界"></a>[平均、剖面与切向曲率共同验收边界](DEM曲率与地形指标.md#平均剖面与切向曲率共同验收边界)
- <a id="dem-平均曲率独立-hosted-验收"></a>[DEM 平均曲率独立 Hosted 验收](DEM曲率与地形指标.md#dem-平均曲率独立-hosted-验收)
- <a id="dem-剖面曲率独立-hosted-验收"></a>[DEM 剖面曲率独立 Hosted 验收](DEM曲率与地形指标.md#dem-剖面曲率独立-hosted-验收)
- <a id="dem-切向曲率独立-hosted-验收"></a>[DEM 切向曲率独立 Hosted 验收](DEM曲率与地形指标.md#dem-切向曲率独立-hosted-验收)
- <a id="dem-平面曲率独立-hosted-验收"></a>[DEM 平面曲率独立 Hosted 验收](DEM曲率与地形指标.md#dem-平面曲率独立-hosted-验收)
- <a id="dem-地形位置指数tpi独立-hosted-验收"></a>[DEM 地形位置指数（TPI）独立 Hosted 验收](DEM曲率与地形指标.md#dem-地形位置指数tpi独立-hosted-验收)
- <a id="填挖方与tpi受控血缘取证修复"></a>[填挖方与TPI受控血缘取证修复](DEM曲率与地形指标.md#填挖方与tpi受控血缘取证修复)
- <a id="tpi验收计时与本轮独立期望值缓存"></a>[TPI验收计时与本轮独立期望值缓存](DEM曲率与地形指标.md#tpi验收计时与本轮独立期望值缓存)
- <a id="dem-地形崎岖度tri独立-hosted-验收"></a>[DEM 地形崎岖度（TRI）独立 Hosted 验收](DEM曲率与地形指标.md#dem-地形崎岖度tri独立-hosted-验收)
- <a id="dem-地形粗糙度独立-hosted-验收"></a>[DEM 地形粗糙度独立 Hosted 验收](DEM曲率与地形指标.md#dem-地形粗糙度独立-hosted-验收)

## DEM 专项共同验收边界

以下仅适用于平面曲率、填挖方、TPI、TRI四个独立手工suite：Hosted／独立NFS、同源产品默认入口Runtime、非默认Tenant普通User原12权限，各自600秒业务预算，不并入nightly或改变其他套件。算法准入归[栅格算子契约](../../workflow/栅格算子契约.md)，各专项保留固定矩阵、独立期望、证据与缺口；未执行范围不能由本地／部分CI补认。

- 固定519×515矩形多波段样本显式第二高程波段，跨512块；逐项按专节覆盖NoData／mask／alpha、合法零／大基值。独立oracle不调用产品函数；容差按各专项，TPI／TRI及填挖方最终存储值严格比、一ULP篡改拒绝，不能套曲率容差。
- 成功逐份核完整像元／mask或CSV值、单位／原网格／CRS、精确自动扫描父execution／User／Tenant／Engine、当前实际输入血缘及Monitor；COG另核布局／nearest概览和artifact＝物理＝Meta严格正整数大小。CSV只核物理＝Meta，不生成RasterArtifact或补造第三方大小。
- 失败准确绑定当前节点／原因／提交语言，反向查询不改冻结语言；零发布／新增扫描／派生血缘，仍全检源和先前成果。保存后的精确血缘沿普通收集器逐条等待，不主动collect绕过。
- 原件清单以SHA-256核验，实际归档范围按各专项，不补造缺失原件。源／先前成果与Meta保持，退出核Runtime工作区、NFS export／owned root、镜像／部署／Infra及私有空间清理；业务、清理与Infra `zero_residuals`独立认定。封闭进度不存响应／SQL／凭据／错误全文。
- Native数值极限单列；固定Hosted不外推后续HEAD、全部类型／单位组合、UI、容量、任意影像、生产或受管业务DELETE，不以新通过关闭旧超时／double-free。

## 双DEM填挖方统计独立 Hosted 验收

`raster-cut-fill-persistence`沿[共同边界](#dem-专项共同验收边界)，30源覆盖双源独立NoData／mask／透明与部分透明、零／负高程；水平网格含米／美制测量英尺。12成功CSV链为混合挖填、前后互换、全不变、纯填／纯挖、三单位组合、填洼／重分类接统计、Int64／UInt64相邻大高程。

18类中英文共36失败，须绑定cut_fill：两角色非法波段、非法单位／冲突、alpha／复数、scale／offset、CRS／尺寸／完整网格不符、旋转／南向／经纬度／缺CRS／定位、一源全无效／有效域不交。独立完整35×35周期闭式计数＋特殊像元修正，用200位Decimal算高差／面积／体积，另以完整标量遍历核周期；面积用实际解码网格／CRS线性换算、保WKT序列化末位事实，不以容差替CSV值。

严格单行13列（6 Int64／7 Float64），核总／有效／排除及挖／填／不变守恒、平方米／立方米、挖填非负／净填减挖／互换关系。每份当前execution双源血缘及Monitor须齐，全部保存后、负例前逐条核完；内部取证见[受控血缘](DEM曲率与地形指标.md#填挖方与tpi受控血缘取证修复)。

核验后逐份将原CSV存`cut-fill-csv/`，`cut-fill-csv-manifest.json`记名／大小／SHA；收尾逐字节再比，归档变更／额外文件／软链接均失败。12原件／清单／完整日志／回执须归档，普通表格保存与大小按共同边界。

**固定结果**：`a26d758daab9149e6fdda628aa202957d9245906`，[Hosted40635872131／122642597594](https://github.com/pampa0629/addp/actions/runs/40635872131/job/122642597594)，12成功／36准确失败、266531ms＜600000ms。七服务同源码／干净仓库、Tenant2／User33／12权限、默认`sh container_entrypoint.sh`；12精确扫描／24实际双源Meta关系及Monitor，内部source-port／唯一output.save／create及save／derive与公开隐藏operations分别验证，源／旧成果／工作区／外层cleanup／Infra均通过。

artifact12794083457的12 CSV与清单／hash／Runtime物理base64一致，固定Decimal夹具＋已归档定位事实独立重算单行全13列、计数／面积／净量／互换关系精确通过。源TIFF未归档，不称离线重读DEM。旧`2edf97e60`的Run40485888750与`16452b150`的Run40537589903均首条血缘失败；后者仅证公开operations=null，不补认内部事实。新来源a26不改写旧失败。

## DEM 三维表面积统计独立 Hosted 验收

`raster-surface-area-persistence`；`make test-raster-surface-area-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。12 CSV链：平地／斜面／非共面双对角／跨块、三水平垂直单位、填洼／重分类、Int64／UInt64微差／最小2×2；18类中英共36失败。完整8列Float64精确，预览按double_precision=15另核；独立高精度向量叉积双对角平均，不补外缘／跨孔洞，核计数／平方米／无量纲比，极限仅Native。

成功78＋失败432＝510次CSV全检／48调用，存12原CSV；期望为标准库周期计数＋异常面片修正，保独立叉积，宿主不增NumPy／pandas／GDAL。物理＝Meta，不造第三方大小；准备／业务／收尾纳600秒。已有本地专项／Runner证据，未列已核Linux全量／Hosted结论。

## DEM 通视与可视域

三套场景的矩阵、独立核验、双身份与未完成记录见[DEM 通视与可视域专题](DEM通视与可视域.md)。原定位保留如下。

- <a id="dem-两点通视独立-hosted-验收"></a>[DEM 两点通视独立 Hosted 验收](DEM通视与可视域.md#dem-两点通视独立-hosted-验收)
- <a id="dem-单观察点可视域独立-hosted-验收"></a>[DEM 单观察点可视域独立 Hosted 验收](DEM通视与可视域.md#dem-单观察点可视域独立-hosted-验收)
- <a id="正交旋转网格通视与可视域独立-hosted-验收"></a>[正交旋转网格通视与可视域独立 Hosted 验收](DEM通视与可视域.md#正交旋转网格通视与可视域独立-hosted-验收)

## 地形湿度指数 TWI 独立 Hosted 验收

`raster-twi-persistence`；`make test-raster-twi-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。14链含degree/percent等价、两英尺、极小非零坡度／重分类／DEM两坡度单位／填洼后平地全NoData、Int64／UInt64面积、全无效／不交／零坡度。519×515方格第二波段跨512，双源独立mask/NoData/alpha；负指数／零／NaN分核。

26类中英共52准确TWI失败：双角色波段／面积单位／坡度冲突、零负面积／负坡度90度、九类网格、alpha／复数／scale/offset；另一源无效仍拒非法值。独立200位Decimal Lambert连分式＋ln、固定A/d／米制／单位1；极限／异构复数alpha仅Native。105＋52×14＝833全检／66调用，存14 COG，期望／准备／收尾纳600秒。

`128a5330782ec1c0033be2ce14b076392e90f012`的[Hosted40818720334／123265876421](https://github.com/pampa0629/addp/actions/runs/40818720334/job/123265876421)完成14／52／833、三级nearest／大小／内外血缘／扫描／清零，421186ms；原件血缘清单与现场匹配。未列Linux全量证据，隔离身份Go T1不算完整IAM／DB，不认证HEAD或关闭通用GDAL中止缺陷。

## 水流功率指数 SPI 独立 Hosted 验收

`raster-spi-persistence`；`make test-raster-spi-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。14链保TWI单位／源组合，原始(A/d)×tanβ、单位metre、不取ln；零坡度／填平洼地有效内部0，原无效／坡度边缘NoData。519×515第二波段跨512，独立mask/NoData/alpha及部分透明，极限／异构复数仅Native。

26类中英52准确SPI失败：双角色波段／面积单位／坡度声明、零负面积／负坡度90度、尺寸原点CRS／非方形／旋转／南向／地理／无CRS网格、alpha／复数／scale/offset，另一源无效不遮非法值。独立200位Decimal Lambert连分式＋原乘积；误乘边长／面积计数／取ln／负值／零坡度作NoData／小坡度阈值／percent漏除100均须拒。

105＋728＝833全检／66调用，存14 COG，首次期望／业务清理纳600秒，构建／部署／源播种／拆除另记。本地专项／SPI-TWI runner／登记通过；旧聚合绑定旧快照主动中断不计全入口通过，最终排序修正仅按专项认。Linux全量／Hosted不能由本地推定。

## 正交旋转矩形DEM坡度独立 Hosted 验收

`raster-slope-orthogonal-persistence`：Hosted／NFS、同源默认产品Runtime、普通执行User12权限，隔离准备者沿13项，分别核身份／源成果快照／各自完整扫描库存。准备者首次源扫／精确限时metadata_read，执行User业务／自动成果扫；撤权／全清理不减。600秒含源扫描、独立参考、执行／每次全检／业务收尾，不重试／扩预算。

八组degree／percent双COG共16链：北向矩形、一般正交旋转（同北向轴长／输入逐位对照）、90°／180°、国际／美制测量英尺、跨512块第二波段／NoData／内部mask／alpha／非有限／零、全无效。独立Horn九点按原有效性／实际轴长／米制换算及GDAL Float32读取与结果舍入，另用解析平面／真实北向成果核，不调产品或临时放容差。全COG像元／mask／六仿射／CRS／单位／布局／实际nearest概览必验。

18类各中英共36准确失败：剪切／一ULP近似正交／两单轴镜像／地理／缺CRS／定位／退化网格八类，波段0／越界／alpha三类，高程单位不支持／布尔／冲突／未知四类，scale／offset两类，非法坡度单位一类。须命中坡度节点／精确原因及冻结语言，不以前置选源／准备／读取失败充数；失败零发布／scan／血缘。复数／混合alpha／最小尺寸／数值极限／故障注入仅Native。

成功累计136次＋36×16＝712成果全检、53物理调用含首次参考，缓存清理另计。三方严格正整数大小、精确扫描身份／当前输入血缘／Monitor、源旧字节及Meta保持；归档16原COG／SHA-256大小清单／冻结参考／终态，末尾撤权／全清理。准备后固定完整业务SHA并记编排SHA／祖先，所列历史结果分别使用：

| 固定版本／实际运行 | 首败、修复与验收边界 |
| --- | --- |
| `90d63b065`／Hosted42479552549 | 来源结构读取AttributeError，16／36／712与16 COG均无完成证据；14精确Grant已撤、待办空。原Meta日志证明缺CRS持久事实，原HTTP／堆栈未归档；固定源码与本地真实读取器红绿支持根因，修正接受合法`spatial:null`、仍拒畸形节点／媒体内容／extent泄漏。不伪称原始栈或用清理抵失败。 |
| `dd201c02f`／Platform42489265940／Job128757843713 | 原完整日志／标准make test-platform依赖链核45组合（含原3本机超时例）／6.092秒／OK，后续Go夹具T1通过。本机3个60秒超时仍失败，Linux不解释宿主根因，无加时／重跑。 |
| `dd201c02f`／[Hosted42549948422／128943056914](https://github.com/pampa0629/addp/actions/runs/42549948422/job/128943056914) | 根扫描身份KeyError；检出／祖先／七服务同源通过，业务16.399秒、阶段空，参考与16／36／712均未执行、0 COG。19精确Grant全撤／隐藏、无待办。栈／源码／复现指向根扫描省略父执行字段时直接索引，原HTTP未存；共享校验器将已验证无父执行值写null，自动成果仍严核父执行／User／Tenant／Engine。 |
| `55fb679016e1397045735ff8f3f4078accc42cf4`／[Platform42924607122／130207746240](https://github.com/pampa0629/addp/actions/runs/42924607122/job/130207746240) | 原完整日志／Make依赖链核45坡度组合／6.236秒／OK、后续隔离身份Go T1过；父字段修复及两回归文件与`80637f0a9`逐字节一致，仅证该固定Linux范围。 |

上述固定记录未有Hosted完整通过，修复／Platform不补认业务矩阵或原COG归档；本轮文档归整不运行新验收。

## 本组独立专项的核验与证据边界

仅本组14个显式引用的专项适用，不扩其他套件。算法归[栅格算子契约](../../workflow/栅格算子契约.md#栅格计算契约)／[矢量算子契约](../../workflow/矢量算子契约.md)，本页保留固定矩阵／独立参考。各沿原手工Hosted／NFS、同源默认Runtime及600秒，栅格User12权限、拓扑User11权限不互换；准备者及metadata_read Grant按专节，不改产品／权限／公开投影／原套件，不重试／扩预算／增nightly。

- 完整实际成果、源旧字节／Meta每次核；自动扫描精确父execution／User／Tenant／Engine，当前真实输入内部／公开安全血缘、Meta关系及Monitor分别核。COG核原网格／CRS或明确缺定位、类型／mask／全布局／实际概览、artifact＝物理＝Meta正整数；CSV／GPKG只物理＝Meta，不造RasterArtifact或重复计child大小。
- oracle不调产品，真实成果逐次重读不缓存；获准不可变期望仅本轮／夹具／源hash绑定。各自原件／清单／期望缺失／篡改／额外文件／链接拒绝，不修复；有界只读observer／SQL仅原声明suite，公开投影不扩大。
- 失败须命中指定业务节点／原因／冻结语言，前置失败不充数、反向查询不改错，零发布／新scan／derive。全部退出清归属缓存／工作区、NFS、Runtime／镜像／registry、部署／Infra及已声明Grant；首败与清理分列，清理失败整套失败，部署销毁不证业务DELETE。

标准沿各Runner、`make test-raster-online-runner`／既有拓扑入口、`make test-geopython-workflow`及选择／登记，不能累加共享测试为唯一数量。本地／登记、Linux全量、Hosted API、真实页面只按固定源码认，不证当前HEAD／容量／任意影像或未列范围；后续事项见[栅格专题](../../../next/栅格算子体系后续专题.md#三后续工作)。

## 整幅栅格百分位统计

单元素与多百分位的矩阵、精度和验收边界见[整幅栅格百分位统计专题](整幅栅格百分位统计.md)。原定位保留如下。

- <a id="整幅栅格百分位统计独立-hosted-验收"></a>[整幅栅格百分位统计独立 Hosted 验收](整幅栅格百分位统计.md#整幅栅格百分位统计独立-hosted-验收)
- <a id="整幅栅格多百分位独立-hosted-验收"></a>[整幅栅格多百分位独立 Hosted 验收](整幅栅格百分位统计.md#整幅栅格多百分位独立-hosted-验收)

## 栅格采样

三项采样的独立核验、精度及验收边界见[栅格采样专题](栅格采样.md)。原定位保留如下。

- <a id="点位栅格采样独立验收"></a>[点位栅格采样独立验收](栅格采样.md#点位栅格采样独立验收)
- <a id="双线性点位栅格采样独立验收"></a>[双线性点位栅格采样独立验收](栅格采样.md#双线性点位栅格采样独立验收)
- <a id="沿线栅格采样持久化"></a>[沿线栅格采样持久化](栅格采样.md#沿线栅格采样持久化)
- <a id="raster-sample-lines-persistence"></a>[沿线采样 suite 定位](栅格采样.md#raster-sample-lines-persistence)

## 点线密度

公共要求、数量矩阵与六项场景统一见[点线密度专题](点线密度.md)。下列旧定位继续有效，具体约束与验收范围由专题维护。

- <a id="点线密度专项共同要求"></a>[点线密度专项共同要求](点线密度.md#点线密度专项共同要求)
- <a id="密度专项数量矩阵"></a>[密度专项数量矩阵](点线密度.md#密度专项数量矩阵)
- <a id="点密度显式非负权重独立-hosted-验收"></a>[点密度显式非负权重独立 Hosted 验收](点线密度.md#点密度显式非负权重独立-hosted-验收)
- <a id="米制点核密度独立-hosted-验收"></a>[米制点核密度独立 Hosted 验收](点线密度.md#米制点核密度独立-hosted-验收)
- <a id="米制四次线核密度独立-hosted-验收"></a>[米制四次线核密度独立 Hosted 验收](点线密度.md#米制四次线核密度独立-hosted-验收)
- <a id="线密度显式非负权重独立-hosted-验收"></a>[线密度显式非负权重独立 Hosted 验收](点线密度.md#线密度显式非负权重独立-hosted-验收)
- <a id="米制圆形线密度栅格独立-hosted-验收"></a>[米制圆形线密度栅格独立 Hosted 验收](点线密度.md#米制圆形线密度栅格独立-hosted-验收)
- <a id="米制圆形点密度栅格独立-hosted-验收"></a>[米制圆形点密度栅格独立 Hosted 验收](点线密度.md#米制圆形点密度栅格独立-hosted-验收)
