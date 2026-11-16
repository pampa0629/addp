# ADDP Online 场景：Manager与空间成果

[返回测试与验收总入口](../../addp测试与验收规范.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。每项条款仅适用于其声明的 suite 与 profile；代码就绪不表示真实 T4 已通过。

专项导读：[文件读取公共准备与收尾](Manager文件与预览.md#文件读取-case-的公共准备与收尾) · [公共前提](#独立持久化专项公共前提) · [Runner／Native 验证入口](#独立持久化专项验证入口) · [水文与分区统计共同要求](#水文与分区统计专项共同要求) · [水文固定结果与待验](#水文专项的固定结果与待验边界) · [邻域与IDW共同要求](邻域统计与IDW.md#邻域与-idw-专项共同要求)

本页场景导航仅用于定位，适用范围以各场景正文为准：

| 内容 | 场景入口 |
| --- | --- |
| Manager 与文件 | [内部产物与 case](Manager文件与预览.md#manager-internal-artifact-lineage) · [混合检索](#manager-hybrid-search) · [KML／GML／TopoJSON S3](Manager文件与预览.md#kmlgmltopojson-的-s3-兼容消费) · [设备静态轨迹](Manager文件与预览.md#设备静态轨迹范围读取) |
| 表格读取 | [ORC 单文件](Manager文件与预览.md#orc-单文件表格-hosted) · [Avro 单文件](Manager文件与预览.md#avro-单文件表格读取) · [Feather V2](Manager文件与预览.md#feather-v2-基础标量独立-hosted) · [CSV／TSV GB18030 读取设置](Manager文件与预览.md#csvtsv-gb18030-独立读取设置-case) |
| 表格历史证据 | [ORC 平坦传输／嵌套只读](Manager文件与预览.md#orc-分层验证与历史证据) · [Avro 首轮／复验](Manager文件与预览.md#avro-验证与历史证据) · [Feather 分版本](Manager文件与预览.md#feather-分版本证据) · [GB18030 本地／Hosted](Manager文件与预览.md#gb18030-分层验证与历史证据) |
| 媒体与容器读取 | [NetCDF](Manager文件与预览.md#netcdf-单文件只读) · [GRIB2](Manager文件与预览.md#grib2-单文件只读) · [ENVI 双文件 COG](Manager文件与预览.md#envi-双文件-cog限定-hosted-已验收) · [Surfer GRD](Manager文件与预览.md#surfer-grd-读取) |
| 媒体读取历史证据 | [NetCDF 授权契约适配](../evidence/资源读取与预览历史证据.md#netcdf-当前元数据授权契约适配--限定正式-hosted-通过) · [GRIB2 数值与页面](../evidence/资源读取与预览历史证据.md#grib2-单文件读取与切片--数值通过及页面收口) · [ENVI 精确组成](../evidence/资源读取与预览历史证据.md#envi-精确双文件现行权限正式复验通过2026-11-04) · [ENVI MinIO 节点](../evidence/资源读取与预览历史证据.md#envi-minio-持续节点新建文件及到期独立-hosted-通过2026-11-04) · [Surfer 四份 GRD](../evidence/资源读取与预览历史证据.md#surfer-6-ascii-扩展及四份-grd-读取--普通-user-hosted-t4) |
| 矢量与地图 | [拓扑持久化](矢量拓扑持久化.md#拓扑构面面叠面缝悬点与线叠持久化vector-topology-persistence) · [重合规整](矢量拓扑持久化.md#重合规整独立场景) · [PMTiles 工作流](#vector-pmtiles-workflow) |
| 栅格流程与持久化 | [Raster 工作流](栅格工作流.md#raster-workflow) · [等值线](#raster-contour-persistence) · [分类转面](#raster-polygonize-persistence) · [矢量栅格化](#vector-rasterize-persistence) · [整数分区统计](#raster-zonal-statistics-persistence) · [邻域统计](邻域统计与IDW.md#raster-focal-statistics-persistence) · [邻域百分位](邻域统计与IDW.md#raster-focal-percentile-persistence) · [邻域分类种类数](邻域统计与IDW.md#raster-focal-variety-persistence) · [筛除分类保护](#分类小斑块筛除指定类别保护独立-hosted-验收) · [筛除面积阈值](#分类小斑块筛除平方米面积阈值独立-hosted-验收) · [矢量分区统计](#vector-zonal-statistics-persistence) · [邻近距离](#raster-proximity-persistence) · [细化](#raster-thin-persistence) · [边界清理](#raster-boundary-clean-persistence) |
| 邻域与插值专项 | [三种加权方法共同复核](邻域统计与IDW.md#三种邻域加权方法的共同复核) · [7×7方窗](邻域统计与IDW.md#77-像元方窗独立-hosted-验收) · [米制圆窗](邻域统计与IDW.md#米制圆形邻域统计独立验收) · [多百分位](邻域统计与IDW.md#邻域多百分位独立保存验收) · [IDW基础](邻域统计与IDW.md#点数据idw插值独立-hosted-验收) · [最少支撑](邻域统计与IDW.md#idw最少支撑点数独立-hosted-验收) · [最近N点](邻域统计与IDW.md#idw最近n个支撑点独立-hosted-验收) · [距离幂次](邻域统计与IDW.md#idw距离幂次独立-hosted-验收) |
| 点／线密度专项 | [共同要求与数量矩阵](#点线密度专项共同要求) · [点权重](#点密度显式非负权重独立-hosted-验收) · [点核密度](#米制点核密度独立-hosted-验收) · [线核密度](#米制四次线核密度独立-hosted-验收) · [线权重](#线密度显式非负权重独立-hosted-验收) · [uniform线](#米制圆形线密度栅格独立-hosted-验收) · [uniform点](#米制圆形点密度栅格独立-hosted-验收) |
| 容量与磁盘 | [分类蚕食容量](#分类蚕食容量raster-nibble-capacity) · [容量验证入口](#分类蚕食容量的验证入口) · [临时磁盘保护](#raster-disk-pressure) |
| DEM 地形与统计 | [填洼](#raster-fill-depressions-persistence) · [平均曲率](#dem-平均曲率独立-hosted-验收) · [剖面曲率](#dem-剖面曲率独立-hosted-验收) · [切向曲率](#dem-切向曲率独立-hosted-验收) · [平面曲率](#dem-平面曲率独立-hosted-验收) · [双 DEM 填挖方](#双dem填挖方统计独立-hosted-验收) |
| D8 流向与累积 | [流向](#dem-d8-流向持久化) · [汇流累积](#d8-汇流累积持久化) · [加权累积](#raster-flow-accumulation-weighted-d8-persistence) · [下游流长](#d8-下游流长持久化) · [上游最长流长](#d8-上游最长流长持久化) · [汇水分区](#d8-汇水分区持久化) |
| D8 河网与汇水 | [Strahler](#raster-stream-order-d8-persistence) · [Shreve](#shreve-独立持久化验收) · [河段编号](#d8-河段编号独立持久化) · [河网转线](#d8-河网转矢量线独立-hosted-验收) · [指定汇水点](#raster-watershed-d8-persistence) · [汇水点吸附](#raster-snap-pour-points-persistence) · [增量子汇水区](#raster-subcatchments-d8-persistence) |
| D8 面积、距离与高程 | [汇流面积](#raster-flow-accumulation-area-d8-persistence) · [到河网沿流距离](#raster-distance-to-stream-d8-persistence) · [HAND](#raster-hand-d8-persistence) |

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

本节仅适用于下列五项：[矢量栅格化](#vector-rasterize-persistence)、[整数栅格分区统计](#raster-zonal-statistics-persistence)、[邻域统计](邻域统计与IDW.md#raster-focal-statistics-persistence)、[矢量分区统计](#vector-zonal-statistics-persistence)、[邻近距离](#raster-proximity-persistence)。其他套件仍按各自场景声明执行。

五项均使用独占 Hosted 生命周期、普通 User 原 12 项权限、同源七个 Backend 与默认入口 GeoPython 产品 Runtime，以及共享 NFS；不接管个人服务。Runtime 同路径 bind 写入与 Meta 真实 NFS 读取分别记录。各专项独立手工触发，预检与业务执行共用各自的 600 秒预算，不改变已有套件、产品权限或跨模块产品逻辑。

每轮核对精确输出与扫描清单、源及先前成果不变和 Runtime 执行工作目录清理；失败不得发布目标或新增扫描。成功和失败退出均须核对归属 NFS export／目录、部署及 Infra 零残留。各专项另列的产品 Runtime／镜像／registry 清理项仍须逐项核对。

入口登记、确定性门禁与真实 Hosted 分别记账。固定版本结果和未覆盖范围见[验收基线](../../../next/栅格算子体系后续专题.md#二当前验收基线)，不外推为当前 HEAD 已重跑。

### 独立持久化专项验证入口

以下分层仍仅适用于上述五项。脚本 Runner 验证夹具、场景断言、隔离生命周期与 CI 登记；五个专项 Runner 均由 `make test-raster-online-runner` 聚合。真实 GDAL 物理校验与篡改拒绝统一沿 `make test-geopython-workflow`，各项范围如下：

| 专项 | 脚本 Runner | Native 物理验证范围 |
| --- | --- | --- |
| [矢量栅格化](#vector-rasterize-persistence) | `make test-vector-rasterize-online-runner` | 几何覆盖、完整 COG 与篡改拒绝 |
| [整数栅格分区统计](#raster-zonal-statistics-persistence) | `make test-raster-zonal-statistics-online-runner` | 真实 GDAL／CSV 与篡改拒绝 |
| [邻域统计](邻域统计与IDW.md#raster-focal-statistics-persistence) | `make test-raster-focal-statistics-online-runner` | 真实 GDAL／COG 与篡改拒绝 |
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

## 正交旋转网格邻近距离独立保存

`raster-proximity-orthogonal-persistence`：Hosted／NFS、同源默认产品Runtime、普通User原12权限、4CPU／2GiB／无额外swap、600秒，不重试／扩预算。18 COG链覆盖原北向三单位对照、90／180／270度及斜向正交方形／非方形、真实最近目标反转、第二波段／零负目标／NoData／mask／alpha／跨块及概览；30类中英共60准确拒绝，反向查询不改语言，失败零发布／新scan／血缘。

成功递增全检、失败各重读18成果，共1251检查。独立世界向量枚举核完整仿射／CRS／米制距离／mask，逐份核COG／artifact＝物理＝Meta正整数、扫描父执行／User／Tenant／Engine、当前血缘／Monitor、源旧保持及全清理。归档18原COG／SHA256大小清单／冻结期望；数值极限、私有故障注入／等距目标身份只认Native，登记／本地不算Linux全量或真实Hosted。

## 分类栅格小斑块筛除独立 Hosted 验收

`raster-sieve-persistence`；`make test-raster-sieve-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。6 COG：旋转多波段4／8邻接、重分类、min_pixels=1、无供体缺定位、全无效。519×515第二波段跨512，合法零／负／安全大整数、mask/NoData/alpha／非有限／部分透明；独立BFS＋原邻界核严格小于阈值替换、等值保留、原最大直接供体／并列、冻结单轮不传播迭代、无供体保持，不调产品游程／GDAL Sieve。

14类中英28准确sieve失败：波段、零负小数布尔阈值、非法值／小数／布尔邻接、小数分类／正越界／Int64负及UInt64越界邻原生NoData、alpha／复数。异构复数alpha、不可观测同分类供体行列并列／资源极限仅Native；21＋28×6＝189全检／34调用，存6 COG，源／期望／业务清理纳600秒。

无定位仅Meta cog/true/heuristic提示、无spatial，不造网格CRS；真实COG须全GDAL布局／三级nearest。血缘仅当前input.source；本节无已核Hosted证据，不以本地准备补Linux／Hosted。

## 分类小斑块筛除平方米面积阈值独立 Hosted 验收

`raster-sieve-area-persistence`；`make test-raster-sieve-area-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。16 COG：米制4／8邻接＋重分类3、两英尺换算／提前舍入反例2、等号邻浮点3、等价像元数1、省略／空保护2、1000平方米下10米／30米分辨率2、Int64／UInt64原生NoData2、全无效1。519×515北向非方形、第二波段跨512，零负／mask/alpha、raw scale/offset。三基本链核保护小斑块不换、达标供体可给／未达标不供给桥接，原大小并列冻结不传播；临界斑块直邻达标供体、供体恰等阈值也核，防无供体保持掩错。

32类中英64准确sieve失败：阈值缺／双传／像元null＋面积／反向4；面积null／布尔／零负／串／数组／对象7；地理／无CRS／定位／双旋转／未知单位／复合7；波段0／alpha／复数及非法小数布尔邻接6；小数／不安全Float64／原生Int64UInt64邻NoData越界4；保护null／[{}]／重复／越界4。独立2000位Decimal面积乘积＋BFS原边界，不调连通／供体／GDAL Sieve，单位重读文件而非写前系数，像元mask精确无容差。

136＋64×16＝1160全检，源基线计81物理轮／80执行；600秒含身份源／首次期望／查询／归档／缓存清理，部署／外退另记。归档全源TIFF／16 COG／16冻结NPY／SHA，源全波段／原生NoData／类型／单位编码／Meta及旧成果保持；内部raster_save/derive与公开operations:null分核，observer只本suite。

NaN/Infinity、保护[1.0]、混合复数alpha／退化／溢出下溢／I/O仅Native。GDAL矩阵／语义污染／归档拒绝及无仓库依赖stdin worker沿原Native，Online／changed／workflow／删断言mutation同登记；旧像元／保护suite不代面积，本地不证Hosted／UI／容量／全量IAM。

## 分类小斑块筛除指定类别保护独立 Hosted 验收

`raster-sieve-protected-persistence`；`make test-raster-sieve-protected-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。JSON重编码规范[1.0]，严格拒绝仅Native，Hosted改[{}]；浮点邻接真非整数4.5，不改数字传输／产品准入。

16 COG：4／8直接保护／重分类／零负保护与供体资格5，省略／空／未中／正逆序5，全保护／min_pixels=1／无定位无供体／全无效／Int64UInt64安全类6。519×515旋转多波段第二波段跨512，NoData/mask/alpha／部分透明，不用scale/offset。独立BFS＋原边界核保护小斑块不变、达标供体可给／未达标不传、原大小／并列／等阈／冻结单轮／无供体；省略空未中合原oracle、正逆序相等，全保护／阈值1保原有效像元。

原14类波段／阈值／邻接／分类／alpha复数，加12保护null、列表外布尔／整数／串／对象、内布尔／对象／小数、正负越界／重复／嵌套，共26类双语52准确失败；不可序列化／极端／深嵌套／I/O资源仅Native。不调产品游程／并查集／供体／GDAL Sieve、不设容差；68执行、136＋52×16＝968全检，源基线计69物理轮，基线非成果数。

600秒含身份源／期望／全查询全检归档／执行／缓存清理，部署／播种／退出另记。存全源TIFF／16 COG／16冻结NPY与大小SHA，源全波段／类型／原生NoData／编码／Meta及旧成果保持、实际nearest与缺定位分核；内部raster_save/derive与公开operations:null独立，不扩observer。原GDAL／52失败／语义污染／缓存归档破坏拒绝／stdin worker独立性及Online／changed／workflow／mutation接线不变；Sieve6/28/189不代16/52/968，准备不算Hosted。

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

## 水文专项的固定结果与待验边界

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

## 平均、剖面与切向曲率共同验收边界

仅适用于下列三个独立手工 suite；各自沿 Hosted／NFS、同源产品默认入口 Runtime、普通 User 原 12 项权限和独立 600 秒业务预算，不改产品算法、权限、其他模块或旧套件。剖面、切向仅手动登记，不并入 nightly，也不取消排队任务。算法准入见[栅格算子契约](../../workflow/栅格算子契约.md)，各方法的矩阵与固定证据见对应小节。

- **样本与独立预期**：519×515 矩形投影多波段网格，显式第二数据波段、跨 512 块，覆盖米／国际英尺／美制测量英尺高程与不同水平 CRS、凹凸／鞍面／倾斜面、NoData／mask／透明和部分透明；各方法另列零梯度与小图边界。独立九点最小二乘核全幅像元／mask；剖面、切向保留 `RTOL=1e-10`、`ATOL=1e-11`，不以方法间关系代替主预期。
- **保存与身份**：完整 COG／最近邻金字塔、原网格／CRS、Float64／NaN／`1/metre`，artifact／物理／Meta 大小为相等正整数；精确自动扫描父 execution／User／Tenant／Engine，当前执行实际输入血缘及 Monitor，源和先前成果不变。概览按实际尺寸核验，小图不虚构层级；519×515 原生新建 COG 按整数减半为 259×257、129×128、64×64，每层独立核完整像元／mask。
- **拒绝与清理**：非法数据波段、alpha 作高程、复数高程、旋转／南向网格、经纬度或缺 CRS、缺定位网格、非法高程单位／单位冲突、未应用 scale／offset 共 12 类各中英文提交；剖面、切向另加非法 method。失败必须绑定同次 curvature 任务及准确领域原因，反向查询不改冻结提交语言；零发布／新增扫描／派生血缘。所有退出核工作区、NFS、部署与 Infra 零残留，源旧不变仍必验。
- **验证与范围**：各节专用 Runner 外，真实 GDAL／污染反例沿 `make test-geopython-workflow`，登记沿 `make test-python-ci-registration`；专项筛选或本地 Runner 不算全量／真实 T4。原始 64 位相邻高程、极端数值和不可表示步长仍单列 Native；固定 API Hosted 不外推后续整树、UI、容量、任意影像、生产、业务 DELETE 或全量 IAM。三项 Hosted artifact 均无 COG 原字节，不称离线像元重算；旧超时／原生崩溃不会由新通过关闭，未解决项见[栅格后续专题](../../../next/栅格算子体系后续专题.md#原生稳定性)。

## DEM 平均曲率独立 Hosted 验收

`raster-curvature-persistence`；`make test-raster-curvature-online-runner`，沿[三方法共同边界](#平均剖面与切向曲率共同验收边界)。8 条 COG 链：三组高程／水平单位直接曲率、填洼／重分类接曲率、全无效、无完整有效邻域、小于 3 行／列。另覆盖中心脉冲、合法零；独立九点设计矩阵最小二乘与解析曲面核九点等权／自由常数项、凸正凹负和一圈无效邻域。

首轮 COG 期望曾错误地向上取整；依据 GDAL `frmts/gtiff/cogdriver.cpp` 与真实文件事实改为共同边界的整数减半，保留布局和像元误差断言。成功门禁跟踪输入无漂移、无新增 Python 原生崩溃报告；这不关闭旧崩溃。

**固定结果**：`05e44e4a0a3cce04c9860765745d074d0ab70e7c` 的 [Hosted Run40339721289／Job121630801003](https://github.com/pampa0629/addp/actions/runs/40339721289/job/121630801003) 在原 600 秒内通过 8 条 COG 链与 24 次双语拒绝；全像元／mask、金字塔、严格大小、精确自动扫描、当前执行血缘／Monitor、源和旧成果不变、工作区／NFS／部署清理与 Infra 零残留均有现场报告。artifact `12659759018` 未保存 COG 原字节，不能声称离线重算。

Native 前置为 `cabfbe79c3d09c8db9211abbda8c848a0beb011f` 的 [Job121497755392](https://github.com/pampa0629/addp/actions/runs/40300756435/job/121497755392)，全量 7298 项通过；不等于上述 Hosted 源码整树 Native 通过。固定结果与共同边界分别认定，不补认未覆盖项。

## DEM 剖面曲率独立 Hosted 验收

`raster-profile-curvature-persistence`；`make test-raster-profile-curvature-online-runner`（由 `make test-raster-online-runner` 聚合），沿[三方法共同边界](#平均剖面与切向曲率共同验收边界)。12 条 COG 链：三种高程单位直接 profile、填洼／重分类、水平平地、非平地零梯度残差、全无效、无完整邻域、小图及默认／显式 mean 两对照。覆盖极小非零坡度；3×3 残差与 2×7 小图为零层概览。独立九点最小二乘＋第一／第二基本形式方向商，平地严格 0、其他零梯度严格 NaN；mean 另用基本形式迹核验，默认／显式 mean 逐字节一致。13 类共 26 次双语准确拒绝。

保存成功后、负例前，集中逐条等待实际输入至当前成果的 active derive 血缘，不手工 collect 或额外重试。阶段记录仅含枚举、序号、状态与相对耗时，不存 SQL、原始响应或凭据，未完成阶段不算通过。同次物理核验可共享只读期望，实际文件完整重读、精确求和、污染反例及容差保持；切向沿用此等待和记录约束。

| 结论 | 最小证据与边界 |
| --- | --- |
| 首轮失败 | `06f9caf5637580d72e5ef571a8a14fd91d31cd46`，[Run40265540763／Job121387037149](https://github.com/pampa0629/addp/actions/runs/40265540763/job/121387037149) 在 600 秒超时，无最终报告；请求次数不算成功链数，清理通过仅证明清理。 |
| 等待修正后仍失败 | `909be4e7b1e1269c916bcf431c458f0e1db1dd17`，[Run40272625047／Job121408540481](https://github.com/pampa0629/addp/actions/runs/40272625047/job/121408540481) 完成 12 条链，负例第 23 次开始后超时；剩余 4 次负例及源最终核验未完成。 |
| 修复与 Native | 去除同次 oracle 的重复计算，保持独立预期、12／26 范围及 600 秒。`d8ddc96dc03d862c66de5ee3ca32b1d79f6b07e4` 的 [Linux Job121492608736](https://github.com/pampa0629/addp/actions/runs/40299191579/job/121492608736) 全量 7266 项通过，不外推后续整树或 Platform。 |
| 正式 Hosted | `7157fb44f4b09bb08e097469ecf5360e03c5c0af`，[Run40305988194／Job121515098139](https://github.com/pampa0629/addp/actions/runs/40305988194/job/121515098139) 在 600 秒内通过 12 链／26 次拒绝、完整物理／扫描／血缘／Monitor、冻结语言、源旧不变、全部清理及 Infra 零残留。artifact `12643687867` 无 COG 原字节，不算离线重算。 |

旧两轮超时仍为失败；并行镶嵌 double-free 根因未定，旧本地 Parquet 失败未在本节闭合，均不由新全量或本项通过关闭。其余未覆盖范围沿共同边界。

## DEM 切向曲率独立 Hosted 验收

`raster-tangential-curvature-persistence`；`make test-raster-tangential-curvature-online-runner`，夹具、Native recipe、变更选择与 CI 登记已准备，沿[三方法共同边界](#平均剖面与切向曲率共同验收边界)。13 条 COG 链：三组高程／水平单位直接 tangential、填洼／重分类、水平平地、非平地零梯度残差、全无效、无完整邻域、小图及默认 mean／显式 mean／显式 profile 三对照。另覆盖横向凸凹、陡坡、小而非零坡度及非零切向曲率；3×3 残差与 2×7 小图为零层概览。13 类共 26 次双语准确拒绝。

独立九点最小二乘＋第一／第二基本形式方向商，切向与平面曲率解析样本必须区分，水平平地严格 0、其他零梯度 NaN。默认／显式 mean 数据字节一致，profile 用自身方向 oracle；仅在三份实际保存结果的非零梯度共同有效处核 `profile+tangential=2×mean`，不用该关系生成切向主预期。完整物理核验和三方法实际数据交叉检查各有独立污染反例；等待顺序、闭集阶段记录和实际文件重读沿[剖面专项](#dem-剖面曲率独立-hosted-验收)。

**失败与收口**：`2f704eb174ba00a9f8e54318f22c5264f37c487a` 的 [Run40267387076／Job121392642425](https://github.com/pampa0629/addp/actions/runs/40267387076/job/121392642425) 在 600 秒超时，无最终报告，提交／查询次数不算通过链数；清理独立通过。修后 `909be4e7b1e1269c916bcf431c458f0e1db1dd17` 的 [Run40272637832／Job121408579032](https://github.com/pampa0629/addp/actions/runs/40272637832/job/121408579032) 在原预算内通过 13 链／26 次拒绝，完整物理／扫描／血缘／Monitor、三方法 228530 个共同有效像元关系、默认 mean 对照及清理／Infra 零残留。artifact `12630112467` 无 COG 原字节；已核 Job 元数据及场景原始日志，未补取完整 Job 日志，不宣称离线重算。

`6cdc38dff129492a83f3200b2b27ffc908e8e6ca` 的 [Linux Native Job121379953801](https://github.com/pampa0629/addp/actions/runs/40263251151/job/121379953801) 全量 6918 项通过，只覆盖该产品源码，不覆盖后来新增夹具或 Hosted 909 整树；Platform 未在本项独立验收，也不证明平面曲率新能力。旧 double-free 和其他未解决问题见[栅格后续专题](../../../next/栅格算子体系后续专题.md)，其余缺口沿共同边界。

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

## DEM 地形粗糙度独立 Hosted 验收

`raster-roughness-persistence`；`make test-raster-roughness-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。13 COG链含中心极大／极小及正负邻居、平地／斜面／鞍面、三单位、填洼／重分类、Int64／UInt64大基值微差、全无效／无完整九点；13类中英共26准确失败。独立2500位Decimal九点max−min含中心，不取半差／去趋势；39调用／429全检、13原COG，期望准备／清理纳600秒，数值极限仅Native。

`60da3691404ee74d3b6789d2948f575be1edef9c`的[Hosted40700360070／122858214770](https://github.com/pampa0629/addp/actions/runs/40700360070/job/122858214770)完成矩阵／13扫描／内外血缘／全清，215112ms；13原件独立Decimal核全像元／mask、519×515／metre／三级nearest／COG。源TIFF未存，离线参考据固定夹具；ae5准备全量因范围外矢量断言失败仍失败，Hosted不追改或认证当前HEAD全量。

## DEM 三维表面积统计独立 Hosted 验收

`raster-surface-area-persistence`；`make test-raster-surface-area-online-runner`，沿[本组边界](#本组独立专项的核验与证据边界)。12 CSV链：平地／斜面／非共面双对角／跨块、三水平垂直单位、填洼／重分类、Int64／UInt64微差／最小2×2；18类中英共36失败。完整8列Float64精确，预览按double_precision=15另核；独立高精度向量叉积双对角平均，不补外缘／跨孔洞，核计数／平方米／无量纲比，极限仅Native。

成功78＋失败432＝510次CSV全检／48调用，存12原CSV；期望为标准库周期计数＋异常面片修正，保独立叉积，宿主不增NumPy／pandas／GDAL。物理＝Meta，不造第三方大小；准备／业务／收尾纳600秒。已有本地专项／Runner证据，未列已核Linux全量／Hosted结论。

## DEM 单观察点可视域独立 Hosted 验收

`raster-viewshed-persistence`沿[本组边界](#本组独立专项的核验与证据边界)。16 COG链含平地／持续贴地、山脊／沿途NoData与遮挡优先、mask／透明及部分alpha、半径等号邻浮点、重合／子像元观察点／半径内无目标、两英尺、填洼及519×515矩形跨块；18类中英共36拒绝，数值极限／异构复数／注入仅Native。

独立精确矩形裁剪，Byte三态0/1/2、NoData255／mask／六仿射／CRS／nearest／COG精核；136＋576＝712原件全检，预检＋52现场共53物理调用，存16 COG／大小SHA，期望／准备／收尾纳600秒。初始化仅本轮精确NFS文件限时metadata_read并全退出撤，填洼链保原DEM的raster_save输入；大小／内容仅隔离Hosted当轮User／scan／execution／target绑定的有界只读SQL，不改公开投影／权限。准备不计Linux全量／Hosted通过。

## DEM 两点通视独立 Hosted 验收

`raster-line-of-sight-persistence`沿[本组边界](#本组独立专项的核验与证据边界)。16 CSV链含平地／山脊、严格贴地与邻高程／端点触地／持续贴地、格角仅触及或真穿入／横纵边、NoData／mask／alpha与遮挡优先、两英尺／矩形第二波段跨块；18类中英共36通视失败，极限／注入仅Native。

独立逐格相交冻结期望，136＋576＝712次原CSV全检，核唯一行／列序／精确Float64米制距离三态，预览按原序列化另核。存16 CSV／清单，核保存目标／类型／写方式与物理＝Meta；唯一真DEM→CSV内部save／公开投影／Meta／Monitor，observer仅本suite白名单。不以本地准备认证Linux全量／Hosted。

## 正交旋转网格通视与可视域独立 Hosted 验收

`raster-sight-orthogonal-persistence`沿[本组边界](#本组独立专项的核验与证据边界)，准备者复用办理角色13权限（原11＋扫描执行／读取），只扫固定源后给执行User精确限时metadata_read，不给执行者引擎管理。源扫／物理Meta快照绑准备者，16业务链／自动扫／血缘绑原12权限User，两清单分核，全部退出撤Grant。

8组各CSV通视＋COG可视域：北向矩形／一般正交／90°／180°、子像元／格角格边、NoData/mask/alpha与遮挡优先、两英尺跨块；第二波段、半径等号邻浮点／重合中心，以完整六仿射＋独立逐格矩形相交核北向／旋转对照。两算子对剪切／近似正交／双单轴镜像／地理／无CRS／无定位／退化各中英共32拒绝，通视重合／半开界外及可视域零半径／越界另8，共40。16成功136＋40×16＝776全检，准备／56现场／清理纳600秒；存8 CSV／8 COG，分别核列序／唯一行／距离三态及声明、像元mask／网格／布局／nearest，极限／注入仅Native。

派发必填`sight_orthogonal_source_commit`完整40位小写SHA；main仅编排，checkout等于所选业务源且为编排祖先，两SHA分记，不缺参回退main／影响其他suite。

| 固定运行 | 首败与未完成范围 |
| --- | --- |
| 业务d240f16bc／编排f21728b33；[42448726028／128633145625](https://github.com/pampa0629/addp/actions/runs/42448726028/job/128633145625) | 检出／祖先／七服务预检过，真实main缺os导入NameError；无报告／8CSV8COG清单，16/40/776未验。wrapper／Infra清零不补Grant撤销报告。 |
| aeabceb3d；[42469659281／128697238003](https://github.com/pampa0629/addp/actions/runs/42469659281/job/128697238003) | 原执行User集中扫源缺引擎管理范围403，矩阵／原件未执行。双身份修正及本地／隔离身份T1不算Hosted，新源须另确认；旧两失败不追改／扩600秒。 |

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

## D8流长统一入口的既有验收适配

仅保留 `raster_flow_length_d8`，必填method：原下游suite `raster-flow-length-d8-persistence` 提交downstream，原上游 `raster-flow-length-upstream-d8-persistence` 提交upstream_longest。不新增suite／脚本，不自动迁移旧DAG；两套各6成功COG／14准确双语失败及原米制步长、零值／最长汇合断言保持。

固定 `fdbe2d58d55ab2b215a609bfbd58b5c34e7c27ed` 的[下游40657243422／122711172418](https://github.com/pampa0629/addp/actions/runs/40657243422/job/122711172418)、[上游40657252005／122711199682](https://github.com/pampa0629/addp/actions/runs/40657252005/job/122711199682)各完成6成功／14失败、6次扫描／血缘／Monitor与清零，业务543430／414932毫秒。原COG未归档，只认现场物理回执；旧独立ID证据不追认为统一入口，不认证UI／容量／业务DELETE／已有DAG迁移。

[Linux40658271835／122714572426](https://github.com/pampa0629/addp/actions/runs/40658271835/job/122714572426)固定 `093a685b8558b74ecbdccb6b95b026d33af603e9` 的Native全量8413通过：D8另8个可执行输入相同，仅元数据测试矢量目录断言变化且D8 AST一致。fdbe条件跳过的Native不计通过，093也不证明粗糙度或当前HEAD全量。

## 本组独立专项的核验与证据边界

仅本组14个显式引用的专项适用，不扩其他套件。算法归[栅格算子契约](../../workflow/栅格算子契约.md#栅格计算契约)／[矢量算子契约](../../workflow/矢量算子契约.md)，本页保留固定矩阵／独立参考。各沿原手工Hosted／NFS、同源默认Runtime及600秒，栅格User12权限、拓扑User11权限不互换；准备者及metadata_read Grant按专节，不改产品／权限／公开投影／原套件，不重试／扩预算／增nightly。

- 完整实际成果、源旧字节／Meta每次核；自动扫描精确父execution／User／Tenant／Engine，当前真实输入内部／公开安全血缘、Meta关系及Monitor分别核。COG核原网格／CRS或明确缺定位、类型／mask／全布局／实际概览、artifact＝物理＝Meta正整数；CSV／GPKG只物理＝Meta，不造RasterArtifact或重复计child大小。
- oracle不调产品，真实成果逐次重读不缓存；获准不可变期望仅本轮／夹具／源hash绑定。各自原件／清单／期望缺失／篡改／额外文件／链接拒绝，不修复；有界只读observer／SQL仅原声明suite，公开投影不扩大。
- 失败须命中指定业务节点／原因／冻结语言，前置失败不充数、反向查询不改错，零发布／新scan／derive。全部退出清归属缓存／工作区、NFS、Runtime／镜像／registry、部署／Infra及已声明Grant；首败与清理分列，清理失败整套失败，部署销毁不证业务DELETE。

标准沿各Runner、`make test-raster-online-runner`／既有拓扑入口、`make test-geopython-workflow`及选择／登记，不能累加共享测试为唯一数量。本地／登记、Linux全量、Hosted API、真实页面只按固定源码认，不证当前HEAD／容量／任意影像或未列范围；后续事项见[栅格专题](../../../next/栅格算子体系后续专题.md#三后续工作)。

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


### 栅格分区多百分位独立保存验收

手工套件 `raster-zonal-percentiles-persistence` 使用同源默认入口Runtime、NFS、普通User原12权限及4CPU／2GiB／无额外swap，固定600秒、不重试、不扩预算。16条CSV保存链覆盖原九列、[0,25,33.3,50,75,100]批量、六条逐项实际CSV对照、分区重分类、1025×513跨六块79分区、逆序同总体、原生Int64／UInt64及NoData邻接大整数、显式第二波段及双角色mask／alpha、无有效值、全无效分区。独立逐像元分组和Fraction插值核完整表／列顺序／精确类别及计数，批量与实际单项、原九列及逆序结果逐表对照。

40类参数／列表／波段／分类／网格／权重及频数冲突各中英文提交，共80次准确分析阶段失败，反向查询不改提交语言。每个终态重读全部先前实际CSV，共1416次全表检查／97次物理调用；归档16份CSV原字节、全部源、绑定源码／源／本轮路径的冻结oracle和大小／hash清单、192份Develop／Monitor原始终态响应。严格核声明目标／格式／写入方式、物理＝Meta正整数大小、扫描父执行／用户／租户／引擎、当前两源内部实际血缘与公开投影／Monitor、源及先前成果和Meta不变、失败无发布／新增扫描血缘及部分启动和全部退出清理；归档大小不冒充产品声明size_bytes。

共享一次排序／归并、数值极限、非有限JSON／元组／混合复数alpha单列Native证据。登记和本地回归不等于Linux全量或真实Hosted；实际运行由CI会话协调，不计UI、容量或吞吐。

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

后续邻域仍按各节[共同要求](邻域统计与IDW.md#邻域与-idw-专项共同要求)，栅格采样／频数／斑块按[水文与分区要求](#水文与分区统计专项共同要求)，仅沿各自原12权限／NFS／同源默认Runtime／600秒。矩阵、容差、归档及Native例外不互换；固定版本、Linux全量、Hosted、UI／容量分别认定，不重试／扩预算／改旧套件。拓扑授权另见[专项边界](矢量拓扑持久化.md#差集融合叠加与节点提取的专项授权边界)，不套用栅格权限。

## 矢量面分区面积加权总体标准差独立验收

`vector-zonal-area-weighted-stddev-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16成功／50类双语100准确失败／1736全检，117物理调用含首次期望；14开启stddev＋原八列／十一列各一关闭。覆盖孔洞重叠多部件／微交无中心、米／两英尺、旋转反向、重分类转面、零负／常量／单有效／全无效／空批。

独立闭式面积＋Decimal中心偏差先冻结，完整几何属性统计null／字段型；16原GPKG和大小hash归档，物理＝Meta且归档清单另核，不虚构RasterArtifact声明。当前实际源内外血缘／扫描／Monitor及全部成果Meta快照不变；活动几何名冲突／数值极限仅Native。本地准备不算Linux全量／Hosted，不重试扩预算。

## 非方形像元邻近距离独立验收

`raster-proximity-rectangular-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16COG涵横纵比例使最近反转、小数步长、两英尺、raw第二波段、NoData/mask/alpha、零负／UInt32、跨块／单行列／原方形对照；26类双语52准确proximity失败。独立枚举目标预期，968全检，16COG／源／冻结数组及hash归档；米制、网格CRS／mask／完整COG和overview实核，数值极限／私文件破坏仅Native。

正交旋转扩展不改16/52/968／600秒，仅同步实际双语准入：rotated_x/rotated_y是非正交剪切、orientation是单轴镜像，仍须当前节点准确拒绝；本suite只证北向矩形成果，不补认旋转Hosted。Linux／实际运行另行认定。

## 分类栅格连通斑块编号独立验收

`raster-region-group-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16COG含4/8邻接、重分类、跨轴／重复编号一致／union根变、raw第二波段mask/alpha、旋转／地理／无定位、Int64/UInt64／全无效；另2CSV编号接分区统计，核每斑块分类、计数／总量守恒。

18类双语36准确region失败：波段0/越界/布尔/串/null/列表，邻接0/3/布尔/串/null/列表，alpha／小数／正负越界／原生Int64UInt64不安全类；前置加载提交保存不充数，复数alpha／编号上限／I/O仅Native。独立BFS先冻，819全检／含源基线55物理轮；完整编号mask／锚点序／网格CRS、实际nearest及CSV九列，16COG＋2CSV／源／预期原件清单归档。COG三方大小、CSV物理Meta及预览分核，无RasterArtifact；准备不计Linux／Hosted／页面容量。

## 点位栅格采样独立验收

`raster-sample-points-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG涵cell原值三状态、边界邻接浮点、旋转剪切／地理／反向、raw第二波段／NoData/mask/alpha、Int64UInt64、重复点／nullable属性、519×515跨轴512、重分类／表达式接采样、全无效命中／全外／带类型空批；所有二维点几何属性顺序类型保留，FID／内存索引非业务ID。

18类双语36准确sample失败：0负越界布尔串null列表对象波段、CRS异、MultiPoint/LineString/Polygon、新两列冲突、alpha、Int64UInt64命中不可精确值／缺网格；混合复数、非有限JSON坐标、重复索引／活动几何／极限／I/O仅Native。独立高精度半平面判界＋闭式原生值，不调产品逆变换分组块读；712全检／含预期53物理调用，16原GPKG／全部源／预期及hash归档，root/run／源字节／夹具绑定。普通矢量声明与物理Meta分核、两原始输入内外血缘，不虚构RasterArtifact，准备不计Hosted／UI容量。

## 双线性点位栅格采样独立验收

`raster-sample-points-bilinear-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG含省略method／显式cell、bilinear四二一支持、中心邻接浮点／图外边缘不足、旋转剪切／地理反向、raw第二波段／NoData/mask/alpha／Int64UInt64、512跨轴重复点、重分类→表达式→采样、全无效／全外／类型空批。

正权重全支持须有效，零权重无效邻居不参与、精确中心仅自身；无epsilon／补边外推／归一化，无效支持优先整数精度／结果范围问题，保全部点属性／序／CRS。原cell18类＋null/布尔/数/空串/未知/大小写/列表/对象8种method，共26类双语52准确sample失败；复数／非有限／极端仿射／索引几何／I/O仅Native。独立中心四边形高精度面积比＋精确权重原值，不调产品插值；136＋52×16＝968全检／含预期69物理调用，16原GPKG／源预期／清单归档，完整几何属性Schema状态null／CRS沿原容差，不借旧cell证据作Hosted。

## 栅格分区分类频数独立保存验收

`raster-zonal-class-frequency-persistence`；沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16CSV含省略method／显式summary两对照、直接／单类别／第二波段raw、512跨轴多行、类别／分区重分类、旋转非方形／地理／反向、两源有效性／原生整数／混合诊断、分类全无效／分区全无效。

32类双语64准确analysis失败：未知空method、percentile值/null、外权重／仅权重波段／加权stddev／非布尔开关、双角色0越界布尔串波段、alpha／复数、类别小数／不安全Float64／原生64越界、分区小数／不安全／UInt64越界、尺寸位置CRS／缺CRS网格／奇异；混合alpha／计数溢出／注入仅Native。独立逐像元整数计数＋精确比例核区守恒、有效分类分母、零负／诊断null行、七列序／空Schema／最多50行预览，summary两链原九列。

136＋64×16＝1160全CSV检查，16CSV／源／冻结预期及hash归档；声明精确target/file/create，物理Meta正整数相等，归档大小另核不冒产品声明。实际全部输入血缘／前后Meta不变，准备／Native/mock不算Linux全量／Hosted。

<a id="vector-zonal-class-frequency-persistence"></a>
## 矢量面分区分类频数持久化

`vector-zonal-class-frequency-persistence`；入口 `scripts/test/online-hosted-vector-zonal-class-frequency-gate.sh`，沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG含两覆盖、孔洞重叠多部件、矩形旋转地理、NoData/mask/alpha、跨块／全无效无覆盖空批、三种业务fid大小写／原summary。32类双语64准确analysis失败，独立逐面像元计数、有效分母守恒、原属性几何行序；1160全检／81物理调用不含收尾，16原GPKG及hash归档、root/run／源码源hash冻预期。

业务fid可展开重复，存储FID独立唯一，不能互相代替；声明／大小／扫描／全部实际输入内外血缘／Monitor／Meta快照和语言不变按共同要求。底层写入发布／清理故障及竞争仅Native，不伪造Hosted故障；准备状态见栅格后续专题，不计UI／容量／任意影像或实际Hosted通过。

<a id="raster-sample-lines-persistence"></a>
## 沿线栅格采样持久化

`raster-sample-lines-persistence`；入口 `scripts/test/online-hosted-raster-sample-lines-gate.sh`，沿[水文／分区共同要求](#水文与分区统计专项共同要求)。16GPKG含cell/bilinear、米／正式国际及美制测量英尺、折线重复内顶点、短／闭合／准确终点、矩形旋转、outside/nodata/alpha、三种业务fid展开。28类双语56准确sample失败，独立Decimal段长＋逐点路径／精确支持权重核全里程坐标数值状态／原属性；单位取CRS事实，不拿独立浮点近似扩容差。

1032全检／73物理调用不含收尾，16GPKG原件及hash归档；两源血缘／公开投影、Meta与旧成果不变沿共同要求。正权无效支持不补值／归一化，闭合起终点按不同里程保留。混合类型／数值极限／底层注入仅Native，不改原点位／算法／权限，未以本地准备记UI／容量或Hosted通过。

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

## 矢量面分区多百分位独立保存验收

独立手工套件vector-zonal-percentiles-persistence复用NFS、同源默认入口Runtime、普通User原12权限和4CPU／2GiB／无额外swap，业务预算600秒、不重试、不扩预算。16条GeoPackage保存链：原八列、批量[0,25,33.3,50,75,100]、六条同总体实际单项对照、全部触及、自定义／空前缀、重分类接面化、Int64／UInt64原生NoData邻接有效值、无有效数值保留行及空矢量批完整Schema。主519×515旋转栅格跨两分块轴，显式第二波段／mask／alpha／原始存储值，面覆盖孔洞／重叠／多部件／细面／出界／重复几何／奇偶总体及单有效零；每面一行、完整属性／几何／行序／CRS、原八列及按请求顺序追加的nullable列对保持。

40类列表／元素／顺序／波段／复数与alpha／覆盖开关／前缀／首项与后项列冲突／CRS与网格／权重与面积及频数冲突各中英文提交，共80次准确analysis失败，反向查询不改提交语言。每个终态重读全部先前实际GeoPackage，共1416次全检／97次物理调用；独立像元面覆盖／标量排序／Fraction插值oracle，实际批量与六份实际单项／原八列逐表比较。归档16份原GeoPackage、全部源、绑定源／本轮／路径／夹具源码的冻结oracle及大小／hash清单、192份Develop／Monitor原始终态响应。

严格核真实矢量声明目标／类型／写入方式、物理＝Meta正整数大小、精确扫描父执行／用户／租户／引擎、当前实际输入内外血缘及Monitor、源与全部先前成果／Meta不变、失败零发布／新增扫描血缘和部分启动／全部退出零残留；存储FID不冒充用户索引，归档大小不冒充产品声明size_bytes。数值极限、重复索引／活动几何名、混合类型复数alpha、内部读取／排序／归并计数及故障注入继续单列Native。仅准备／登记／本地回归，不计实际Hosted或Linux全量、UI、容量；实际运行由CI会话协调。
