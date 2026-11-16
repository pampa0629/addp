# ADDP Online 场景：Manager 文件与预览

[返回测试与验收总入口](../../addp测试与验收规范.md) · [返回空间成果场景](Manager与空间成果.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。各 case 的身份、权限、部署、清理及证据范围分别认定；代码就绪不表示真实 T4 已通过。

导航：[case 选择](#明确的验收-case) · [公共准备与收尾](#文件读取-case-的公共准备与收尾) · [GRIB2](#grib2-单文件只读) · [NetCDF](#netcdf-单文件只读) · [其他格式读取](#surfer-grd-读取) · [模型与完整场景](#部署模型转换与通用清理)。

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

### GRIB2 单文件只读

阅读导航：[授权与来源](#grib2-授权与来源) · [逐消息数值](#grib2-逐消息数值) · [页面切片与原件](#grib2-页面切片与原件) · [清理与未覆盖项](#grib2-清理与未覆盖项) · [验证与历史证据](#grib2-验证与历史证据)。

#### GRIB2 授权与来源

已批准的首期范围见[GRIB2 格式规范](../../builtin-formats/媒体与地图数据集.md#grib2-单文件只读限定范围已验收)。独立 `native-grib` case 复用既有 Manager Linux x86-64 Hosted 部署、GeoPython、NFS、MinIO 和 NetCDF 的六项普通读取权限，不新增服务、执行权限或精确组成内容 Grant，不运行 Model3D 或 Transfer 业务。

按现行跨引擎元数据规则，复用套件已有、与消费者分离的源初始化 User，为扫描后四个精确文件／对象办理 `metadata_read` Grant；核准前不可见、核准后仅返回结构投影，退出撤销并确认重新不可见。仅元数据边界不得作为内容完整性或内容读取授权。

NFS／MinIO 各两份独立字节编码源文件，每份三条单场消息，覆盖简单打包、IEEE32／64、不同尺寸、相同要素不同预报时刻、两种行扫描方向、合法值 9999 与显式 bitmap 缺失、大于 180° 的原生经度。

#### GRIB2 逐消息数值

完整格式参数、版本和数值通过消费者既有 `manager.content.read` 与 Security 约束下的 Manager 预览／切片核对，不从 Meta 结构接口索取内容属性；GRIB 内容源授权迁移仍不在本轮范围。

逐消息核对全部数值、掩膜、单位、时间、独立网格、有界子窗口、错误选择和源变化拒绝。

#### GRIB2 页面切片与原件

真实页面在标准1280px及Hosted视口使用完整属性切换消息并清除旧结果，核对读取按钮可点击、切片DOM及首尾单元格滚动后实际可见；每条消息保存一张数值表截图，四份原件另核API／按钮完整字节及页面截图。

#### GRIB2 清理与未覆盖项

页面切片不写回元数据，不生成地图、任务或缓存成果；额外核 Native 临时快照清空，精确源及外层收尾沿[公共要求](#文件读取-case-的公共准备与收尾)。

受验消息、网格、压缩和模板范围以格式规范及分层证据为准；产品模板 8 只有 Native 证明，不由 Hosted 模板 0 的结果推定通过。GRIB1、单消息多场、其他网格／压缩／本地扩展、地图／COG、科学计算与导出不在该验收范围；不读外部 idx、不自动转单位或重排经度，SKP 精确组成准入仍保持关闭。

#### GRIB2 验证与历史证据

标准门禁复用 `make test-geopython-workflow`（含独立字节夹具经真实 Native Worker）、`make test-common-frontend`、`make test-module MODULE=common GO_ONLY=1`、受影响 Meta／Manager／Transfer Go 门禁、`make test-manager-frontend`、`make test-manager-online-runner` 和 `make test-online-runner`。正式验收沿现有 workflow 选择 `suite=manager-internal-artifact-lineage`、`manager_case=native-grib`，业务入口为 `ADDP_ONLINE_MANAGER_CASE=native-grib make test-online ONLINE_SUITE=manager-internal-artifact-lineage`。

固定源码 `6e7ae87559552bd7d5bad122f4f0259f963d7b46`、Run `41572302717`／Job `125707787735` 的 Owner／Browser v2 覆盖十二张可见切片、四原件、Grant 撤销及清理，详见[分层证据](../evidence/资源读取与预览历史证据.md#grib2-单文件读取与切片--数值通过及页面收口)。首轮 v1 数值／DOM／原件有效但控件被长属性遮挡，元数据截图不证明切片可见；修后首验 `stage=scan / cases=[]` 因缺元数据授权失败，未进数值／页面。GeoPython CI 取消及本机 runner TLS 夹具错误独立保留，不由专项或清理通过推定全量 CI。


### NetCDF 单文件只读

阅读导航：[来源与权限](#netcdf-来源与权限) · [变量与切片](#netcdf-变量与切片) · [页面与原件](#netcdf-页面与原件) · [清理与未覆盖项](#netcdf-清理与未覆盖项) · [验证与历史证据](#netcdf-验证与历史证据)。

#### NetCDF 来源与权限

独立 `native-netcdf` 验证 CDF-1/2/5 及 NetCDF-4 根组数值 profile，复用 GeoPython／NFS／MinIO；不启动 Model3D／PointCloud／Document，不执行 Transfer 或任务。部署及未执行组件的判定沿[公共要求](#文件读取-case-的公共准备与收尾)。

一次性普通消费用户严格六项已有权限：manager.content.read、manager.data_item.read、meta.catalog.read、meta.scan_task.execute、meta.scan_task.read、monitor.execution.read；不增加权限，源初始化身份复用现有标准初始化范围，普通原文件读取仍沿现行内容权限，不宣称精确组成内容源Grant已接入。浏览器进程移除初始化凭据。

按现行跨引擎元数据契约，扫描后由分离的初始化User为十四个精确文件／对象签发metadata_read Grant，沿既有共享ExactFileMetadataGrants核准前不可见、核准后可见、退出撤销后不可见及详情404；不授予目录／前缀或内容读取Grant。

保留三个独立编码的经典样例：CDF-1未知坐标、CDF-2显式UTM50N规则网格、CDF-5超2^53的Int64。增加四个独立netCDF-C/HDF5生成的固定NC4原件：Int32压缩、Int64、UInt64近上限、显式UTM50N规则网格；生成器不调用产品读取器，原件随夹具保存，数值/mask/网格由独立明确oracle核对。七样例均含两时间层、缺失值、比例/偏移和360_day时间维。

#### NetCDF 变量与切片

公开Meta只核容器结构投影，原始类型、维度、日历、组成profile和源版本由消费者经既有Manager内容入口核对并归档。

两引擎共十四份源上逐文件核规范身份、变量与维序、NC4完整组成profile、原始数值类型、字节大小/SHA、二维完整数值/掩膜、指定小窗口、重复无持久缓存读取、越界/伪子数据集拒绝、经典与NC4源变化409及原件精确恢复。

父容器不得取得统一空间范围，无CRS切片不得定位；显式规则网格只在已证空间profile下显示范围，不据数据名称推4326。

#### NetCDF 页面与原件

实际页面逐格核原值和valid状态，另核数值表可视高度及最后数据格能滚动进入视口，防止地图把表格压缩到零高度；有定位样例分别保留数值网格与地图截图。切时间层清掉旧图、下载哈希/截图，归档manager-netcdf.json与manager-netcdf-browser.json。

核对原文件 API 与真实按钮下载的完整字节、大小及 SHA。

Owner 报告使用 `addp.manager-netcdf/v2`，必须包含现行元数据授权证明，拒绝缺少该证明的旧回执；页面报告沿用 `addp.manager-netcdf-browser/v1`，严格核对 schema、逐源数值／掩膜／时间切换、下载及现有截图。

#### NetCDF 清理与未覆盖项

按[公共收尾](#文件读取-case-的公共准备与收尾)核源未改写、精确 Grant／NFS 文件及对象清除、外层 identity／engine／deployment／Infra 清理。无任务或业务产物，不登记缓存复用或任务删除。

NC4外部/复杂组成拒绝由GeoPython原生T1及Linux产品构建验证分别取证，不以正常源页面通过代替；NC4嵌套分组、复杂类型、跨文件聚合、输出和任意CF特性不在本轮。

#### NetCDF 验证与历史证据

标准入口为 `make test-geopython-workflow`、`make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=manager GO_ONLY=1`、`make test-manager-frontend`、`make test-manager-online-runner`；真实业务为 `ADDP_ONLINE_MANAGER_CASE=native-netcdf make test-online ONLINE_SUITE=manager-internal-artifact-lineage`，Hosted 由既有 workflow 选择同名 case。

- [经典 CDF](../evidence/资源读取与预览历史证据.md#netcdf-经典-cdf-单文件只读--限定-hosted-通过)：CDF-1／2／5；首轮页面失败保留，API 数值／清理不替代按钮下载和浏览器后快照。
- [NC4 根组数值](../evidence/资源读取与预览历史证据.md#netcdf-4-根组数值-profile--本地门禁与正式复验)：保留组成拒绝假阳性、ARM64 下载失败、Linux 十项失败及维度引用有／无符号修复。修后 Hosted、Linux 命名回归与完整 GeoPython 超时取消分别认定。
- [现行元数据授权适配](../evidence/资源读取与预览历史证据.md#netcdf-当前元数据授权契约适配--限定正式-hosted-通过)：`5e5d436f8eb433890daa8b04d9c751fe5e7044bc`、Job `125763328404`，十四来源、精确 Grant、结构／内容分工及清理。UInt64 静态截图裁切，首层缺失由 DOM／JSON 证明；范围框不认证外部底图或影像渲染。

详细产物、失败及清理归所属证据；旧回执不替代现行授权，固定版本不证明任意 NC4／CF 或当前所有环境，相关本地失败与 CI 取消仍保留。

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

`native-table-avro`沿[公共准备](#文件读取-case-的公共准备与收尾)，复用NFS／MinIO、Transfer及GeoPython物理回读。平面／嵌套／Enum／Map／普通fixed各六源（双存储×三codec），共三十源／任务／精确Grant；只有六平面源可写Parquet，其余二十四次须只读拒绝且无输出。普通fixed扩展保留原二十四源字节与全部断言。[Owner runner](../../../../scripts/test/manager-avro-online.py)及浏览器回执v7；v2日期小数／v3 UTC／v4嵌套只证当轮，不追认后续。

#### Avro 三种 codec

`null/deflate/snappy`独立编码原件、独立核验，不用产品reader生成期望。

| 来源 | 独有断言 |
| --- | --- |
| 平面 | 每源2503行、多OCF块，标量／nullable、date、bytes／fixed decimal(38,6)：0001／9999、闰日／纪元前、正负大值／尾零／NULL／符号扩展。十三字段含ms/us/ns UTC：零／负计数、ms/us年界、超JS安全整数、ns int64两端及NULL；long独立编码不过浮点／时间对象。 |
| 嵌套 | 每源2503行，根五字段id／detail／items／grid／fixed_values；可空record、record数组、多维数组及具名fixed decimal数组。核NULL对象／全NULL对象、NULL／空数组／NULL元素、大整数／Decimal／日期／三单位UTC／binary及OCF单／多／负块；原平面三文件哈希保持。 |
| Enum | 顶层／嵌套／数组完整枚举名、有序候选、具名引用、null及精确值。 |
| 普通fixed | 完整具名类型、精确1／4／16／32字节、此前声明引用、同宽异名、NULL／全零／首尾零／高位字节；record／array／map内逐值Base64还原，实际第二页和声明／结构详情。 |
| Map | 动态键原文、完整`map_value`树、大整数／小数／UTC纳秒／枚举、NULL／空映射／缺键；真实第二页、结构详情／声明JSON。 |

#### Avro 分页与批量传输

- Meta核schema／date／decimal及UTC的unit=ms/us/ns、timezone=UTC、native_type=long；嵌套用户投影核完整树，不要求公开format_info，原schema保留由Meta原生及原件证明。
- API首页／第二页／末页及真实下一页核总数／全值／缺失；页面UTC固定3／6／9位ISO小数＋Z。嵌套三页／四类结构单元格的只读JSON核完整值，Enum／Map／普通fixed各核声明。原件依公共MinIO允许／NFS拒绝规则；裁切不算全文展开／hover已验。
- 六平面Avro→Parquet跨批核全字段、类型、nullable、精度／scale和值（含大整数／binary／NULL）。writer按字段名排序，不能按OCF列序；PyArrow逐列cast int64核UTC原计数／NULL、物理INT64／TIMESTAMP原单位及isAdjustedToUTC=true，不先转datetime或把最小ns当NaT。
- 二十四拒绝逐项绑定execution UUID／Tenant，公开读写0、`error_details.category=execution_failed`，不读取不存在的error_msg或公开原始错误。关联Worker既有Runtime日志单次已保存preview-only拒绝，仅存闭集原因／位置／行哈希；物理空间只含六Parquet＋marker，零计数或缺target_change_possible不证明无副作用。预期拒绝不算整场失败，原因／物理输出不符才失败。

#### Avro 清理与未覆盖项

按[公共收尾](#文件读取-case-的公共准备与收尾)核三十源／任务／Grant、六成果及marker删除，无二十四次拒绝输出。嵌套／Enum／Map／普通fixed批量未开放，local-timestamp／time等未列逻辑类型、whole、写出、所有生产者／压缩变体与容量未验；NFS原件和SKP精确组成准入保持关闭。

#### Avro 验证与历史证据

| 固定源码／作业 | 结论与关键限制 |
| --- | --- |
| `40b1468c5576c3660e1d49c8018f8356f5a959c3`／Job128604699236 | [首败](../evidence/资源读取与预览历史证据.md#avro-单文件标量读取--hosted-验收)：API／原件／Transfer计数后列序oracle失败，未进页面、未归档原Parquet，本地复现不补缺口。 |
| `e706899b69309fa9a7364e4c0f6118b9f5d153dd`／Job128610273652 | [六源三codec](../evidence/资源读取与预览历史证据.md#限定正式复验通过)，六Parquet15018行；ID截图裁切，完整整数由DOM／API／原件／物理证明，相关门禁失败仍保留。 |
| `7af201a28f237a0ef88aa27a8129e54042ff5d8f`，dispatch `1082c0ed0cd2359be69841085cf5dbf02a20467e`；43584189240／132415063748／attempt1 | [UTC v3](../evidence/资源读取与预览历史证据.md#utc-时间戳限定正式-hosted-通过)：六源十三字段／15018行原计数／NULL／UTC注解及清理；前两次失败、六截图长值裁切限制保留。 |
| `c63888fba953df5545a06b11c7641284c2bbe792`；[43668574041／132673333161](https://github.com/pampa0629/addp/actions/runs/43668574041/job/132673333161) | [Enum](../evidence/资源读取与预览历史证据.md#avro-enum-正式-hosted-复验2026-11-15)：十八源、原件、拒绝、六Parquet／清理；首败及视口外声明／过渡帧不作完整视觉证明。 |
| `9cf429c4a9a0ffc84f06e5971bed254c7e935faa`；[43677124474／132699620936](https://github.com/pampa0629/addp/actions/runs/43677124474/job/132699620936) | [Map](../evidence/资源读取与预览历史证据.md#avro-map-限定正式-hosted2026-11-15)：二十四源、键／类型、分页／原件、十八拒绝、六Parquet／清理；NFS403及截图边界不扩大。 |
| `57d7fa52a5dd122e167d8e8ae4a7df9716f9bdae`；[43885241459／133361505614](https://github.com/pampa0629/addp/actions/runs/43885241459/job/133361505614) | [普通fixed](../evidence/资源读取与预览历史证据.md#avro-普通-fixed-限定正式-hosted2026-11-16)：三十源、90页1290行、完整名称／精确长度／字节、二十四只读拒绝、六Parquet及清理；NFS403和截图视口边界保留。 |

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

### Feather V2 基础标量独立 Hosted

阅读导航：[身份与基础标量](#feather-身份与基础标量) · [时间字段](#feather-时间字段) · [日期与定点小数](#feather-日期与定点小数) · [清理与未覆盖项](#feather-清理与未覆盖项) · [分版本证据](#feather-分版本证据)。产品规则归[Feather正式契约](../../builtin-formats/列式文件与数据容器.md#feather-v2-单文件只读)，各扩展同`native-table-feather`，固定版本分别认定。

#### Feather 身份与基础标量

沿[公共准备与身份差异](#文件读取-case-的公共准备与收尾)复用NFS／MinIO／GeoPython／Transfer，消费十一项／准备十三项，不改个人身份。各存储三codec（无压缩／LZ4／ZSTD），每源2503行、多Arrow record batch，含四种有符号整数、bool、float32/64、Unicode／binary／nullable与空值区别。初始化员有界deep后签精确单文件metadata Grant；公开Meta只结构，Manager受保护预览核编码profile／真总数／源版本，API页1／2／126及真实下一页留响应／截图。

原件按公共存储差异逐源核：三MinIO API／真实按钮完整字节；三NFS下载403 JSON、无附件／能力关闭，仍核完整预览／Transfer。Owner／browser v2分存两类结果。普通User实际Transfer→精确Parquet，独立物理reader全核六成果15018行／类型／nullable／值，不以页样本代连续读取。

#### Feather 时间字段

同六源／权限，Meta核单位／源时区／可空性，Manager核完整ISO，六Parquet独立核逻辑类型／原整数／NULL。样例含秒时刻、日末纳秒、本地时间戳、纪元前／最小纳秒计数、纽约夏令时回拨双瞬间及固定偏移；秒→毫秒溢出仅确定性验证。

#### Feather 日期与定点小数

同六源，Meta／Manager核原生日期、Decimal128/256精度／scale／nullable；API及实翻页逐值比纪元前／闰日／null／尾零。Parquet独立逐行核DATE／DECIMAL逻辑类型／精度／scale／值。

#### Feather 清理与未覆盖项

沿[公共收尾](#文件读取-case-的公共准备与收尾)，六源字节保持，撤Grant隐藏，六任务删／404，六物理源／六成果／目标标记删除，外层另报；无派生缓存或新内容授权准入。源变化／不支持类型／预算拒绝分列确定性与Native，不替跨模块权限／容量。Feather V1／IPC stream／unsigned／嵌套／dictionary／duration／写出未支持，SKP精确组成关闭。

#### Feather 分版本证据

- [基础标量](../evidence/资源读取与预览历史证据.md#feather-v2-基础标量只读--独立-hosted-t4)：首轮正负零oracle失败、Transfer preflight缺口及修复结果保留；旧双存储均下载不替现行NFS拒绝。
- [日期／定点小数](../evidence/资源读取与预览历史证据.md#feather-v2-日期和定点小数)：只证当轮DATE／DECIMAL，保相关CI失败，不追认时间戳转存。
- [时刻／时间戳](../evidence/资源读取与预览历史证据.md#feather-v2-时刻与时间戳--正式-hosted)：首轮NFS403后退出与修订正式复验分开，首轮未进页面／物理Parquet回读。

本地／Linux Native／Hosted按实际证据各认，清理不抵业务失败，基础通过不追认后续类型。

### CSV/TSV GB18030 独立读取设置 case

阅读导航：[身份与来源准备](#gb18030-身份与来源准备) · [结构与内容投影](#gb18030-结构与内容投影) · [页面读取与原件](#gb18030-页面读取与原件) · [清理与能力边界](#gb18030-清理与能力边界) · [分层验证与历史证据](#gb18030-分层验证与历史证据)。

`delimited-read-settings`沿[公共准备](#文件读取-case-的公共准备与收尾)使用NFS／MinIO，产品仅Common Go CSV／TSV reader，物理worker只备／核／删本轮源，无新Runtime或解码路线；格式规则归[表格与矢量交换](../../builtin-formats/表格与矢量交换.md)。

#### GB18030 身份与来源准备

消费仍十项，初始化仅此case十三项，含既有`meta.scan_task.execute`、`meta.scan_task.read`，不改个人／生产账号。两存储各CSV／TSV两份，每源25行、四字节字段名／数据与引号内CRLF；初始化有界basic后签既有精确单文件Grant。

#### GB18030 结构与内容投影

公开Meta只结构，不含行数／读取设置／源hash／访问索引。完整编码／表头／精确行数／源hash及无源字节索引，由受保护Manager `data.item_meta`核，不扩目录投影／权限。

#### GB18030 页面读取与原件

真实设置提交`encoding=gb18030`，核25行20＋5、无头26行20＋6／保首记录、普通刷新继承／重开／恢复头。API与真实原件按钮分别核完整字节／大小／SHA256。

#### GB18030 清理与能力边界

按[公共收尾](#文件读取-case-的公共准备与收尾)撤Grant／核Meta隐藏与精确源零残留，再清部署／Infra；无派生，不代Transfer批量T4。

#### GB18030 分层验证与历史证据

`make test-manager-online-runner`、受影响System Go T1及既有case登记，不增入口。最小证据：

| 范围 | 结论与限制 |
| --- | --- |
| [本地／Linux补证](../evidence/资源读取与预览历史证据.md#csv--tsv-的-gb18030-显式读取--本地门禁) | T1／T2／受控T3不代Hosted，旧GBK不追认本case。 |
| [失败与修正](../evidence/资源读取与预览历史证据.md#gb18030-独立-hosted-的失败记录与-meta-完整名修正) | 前三轮Tenant ID类型／误用公开投影／重复全名，第四轮会话轮换旧API凭据401保留，修复／清理不改通过。 |
| [第五轮正式结果](../evidence/资源读取与预览历史证据.md#gb18030-独立正式-hosted-通过与产物复核) | `b362628e725fc8e2fd95264b7bc06b941ea541c4`／Job128749505328：四源设置／分页／原件／全清理，字库缺字／截图浮层／本地失败保留。 |

不认证编码写出、国家标准版本全符合或更多样本，SKP精确组成仍关闭；各轮产物／范围按证据正文。

### FileGDB 实际页面导出

`filegdb-export`用隔离PostGIS、GeoPython产品镜像。初始化用户扫描并签精确读Grant；同一消费用户授予前经Gateway提交每个精确表必须403，且无会话／execution UUID／下载入口。随后真实页面分别导出3857点、4326 MultiLineString／MultiPolygon，三份`.gdb.zip`各仅单图层；核Monitor执行归属／下载字节哈希／截图。同Tenant另一普通用户有导出读取权限仍须拒绝其会话及下载。独立GDAL解包核源CRS、XY、多部件边界、内环／标量／null：线顶点序不变，面允许环起点／方向变化。按公共导出收尾，与NFS FileGDB读授权分开报告。

两种FileGDB case共用执行前撤权断言：仅归属已证的Linux Hosted，用现有进程工具／pidfd暂停唯一Transfer bounded Worker；提交202后Monitor须证pending、attempt=0、未开始。原初始化用户撤销精确Grant并复核未领取，恢复同一进程后须attempt=1在来源授权preparation失败，公开计数不报告内容IO；会话failed、无download_url，文件409、重提403、源不变。异常也恢复原进程，确定性核暂停／恢复／中途异常；不改配置、增权或操作开发进程。该API拒绝不证明浏览器失败页、物理零读或缓存。

Owner v5须含独立`execution_revocation`、每表无Grant拒绝、原三页面／清理及browser v3绑定；旧v3不证撤权。运行中途撤权／自然到期、其他引擎、ZM／多层／复杂字段／容量不因此扩展。

### MySQL FileGDB 实际页面导出

`filegdb-mysql-export`沿上节页面／OpenFileGDB oracle／授权与撤权流程；唯一新增夹具为独占临时MySQL：既有T2固定镜像、随机loopback端口、tmpfs及仅可读三表账号，不连个人Business。初始化13项／消费6项／同Tenant旁观1项权限不变。

三InnoDB普通表分别点3857、复线／含内环复面4326，各三行含中文／空串／null、布尔／小数／空几何；身份为server→database→table，保留数据库轴序／CRS，视图／自定义SQL由guarded reader拒绝。独立注册／快照／运行／清理，不借PostGIS schema或历史通过。

同Owner v5／browser v3绑定case与source_engine_type，旧v4不证MySQL。`business/scripts/online-manager-mysql-fixture.sh`的start／精确快照／stop须核case及容器归属，原Registry／GeoPython及部署清理保留；runner、进程工具／身份夹具、既有登记／workflow核接线，未核正式结果不回填通过。SKP精确组成关闭。

### TIFF 无定位小图的 COG 布局回归

导读：[MinIO 小图](#tiff-minio-无定位小图) · [NFS 自然到期](#tiff-nfs-渲染与自然到期) · [MSK 组成](#tiff-msk-组成扩展) · [清理与边界](#tiff-清理与共同边界) · [证据](#tiff-分版本证据)。

`native-raster-media`保持原十项消费权限、部署／服务／预算及IMG／JPEG2000／四种GRD／原四COG。子报告`addp.manager-raster-media/v2`、页面`addp.manager-raster-media-browser/v4`；COG规则见[TIFF布局提示](../../builtin-formats/媒体与地图数据集.md#tiff-云优化布局提示)。

#### TIFF MinIO 无定位小图

自建240×120 Classic TIFF COG，Float64/NaN、512瓦片、DEFLATE、无CRS／网格／overview。不上传个人成果或提交该小图生成；保留既有非定位可选转换按钮，无地图不等于无按钮。

- **物理**：GDAL完整COG验证0错误／0警告；独立公式核28,800值、35 NaN／完整mask、块尺寸／空CRS／空网格／零overview及原大小／SHA，源改写后重签哈希收据仍拒绝。
- **刷新**：同User经Gateway精确item同步刷新，核请求／响应、前后Meta结构／扫描时间／独占源scan execution，`full_name`／指纹／item ID不变。`storage.last_modified_at`按带时区同一时刻比，其他attributes／源字节不变。Manager受控事实为`tiff/cog/true/heuristic`、240×120、tiled=true／overview=false、无spatial，与公开Meta分存。
- **页面／原件**：真实属性／预览核同源240×120单页、Range206、完整Float64灰阶／透明NoData、无地图；API／按钮交付同一single TIFF。重开仍核零生成／业务失败／警告及源／Meta不变，显式刷新除外。

#### TIFF NFS 渲染与自然到期

`nfs_tiff_authorization`将同SHA小图复制至本轮只读NFS；仅初始化用户增加`meta.scan_task.execute`／`meta.scan_task.read`单目录深扫。短签／收回精确metadata授权取得正式DataItem ID，消费仍十项、同一普通API会话：

- **授权与投影**：仅metadata时Meta／preview200、结构白名单、无内容地址／摘要／统计、Range403；加精确read后完整preview／Range206；撤read保metadata白名单，撤metadata后Meta／preview404、Range403。只增本轮精确read命令，不增目标；根报告核全状态，异常仅清本轮命令。
- **独立到期**：沿原90秒read核Range 403→206→403，正向核Content-Range／实际字节；有效时真实Console核同账号、Range与完整灰阶／透明像元。自然到期后Meta／preview404，正式历史须证已到期、无撤销记录，不以手动撤权代替。

#### TIFF MSK 组成扩展

MSK／四成员复用组成验收，正式Meta DataItem指纹＋System完整boundary hash为准入依据，不按扩展名／文件数拼授权边界；核精确扫描身份／成功终态后才签整项read。每成员依次核授权前403、授权后完整Range206（大小／Content-Range／SHA）、源仍在时撤权后403；跨DataItem借sidecar400，计数相等不替single／multi完整组成。

| 专项 | 独有组成、扫描与物理断言 |
| --- | --- |
| `nfs_tiff_msk` | 两份字节相同的有定位TIFF，一份带真实`.tif.msk`。独立公式核全部65,536 Int32、NoData／CRS／仿射；无mask仅1无效，有mask257无效。single与multi分离，主image required=true、mask=false。`scan_progress.files_discovered=3`／`directories_discovered=1`，两个total_known=true，`items_planned/items_completed/items_succeeded=2`、`items_failed=0`、`phase=finalizing`，`metadata.items_scanned=2`；原初始化User／成功终态写入`nfs_tiff_msk.scan`，缺计数／额外项／未完成均签发前拒绝。生成额外一COG，核完整数值／mask／空间／布局及删除后对象缺失。 |
| `nfs_tiff_sidecars` | 一TIFF＋同basename HDR／TFW＋完整主名追加PAM，另有相同主字节single对照，不用个人SRTM。主TIFF无定位，TFW给固定网格、PAM给EPSG:4326、HDR给完整可选成员；主image必需，header／world_file／auxiliary_metadata显式可选，single仍无定位。扫描恰5物理文件／1目录／2逻辑项全成功，System事实与Meta完整对应；五成员逐项Range。额外一COG核65,536 Int32、1 NoData、完整mask／TFW网格／PAM CRS／布局；TFW／PAM缺失或变化不能因主图可读而通过，HDR核组成／组件版本／原字节。 |

正式组成、授权回执、执行／像元和删除证明分别归档。

#### TIFF 组成的浏览器画布断言

MSK／四成员均在授权有效、成果未删时开真实Console快显。独立公式将EPSG:4326网格转Web Mercator像素，核真实OpenLayers源投影／范围及WebGL postrender RGBA：有效区可见、范围外透明；MSK 16×16无效区中心透明、邻近可见，四成员同位置可见。重开复用同成果再核、实际COG Range206、零生成／业务失败／警告，元数据／原生mask不代显示。

`addp.manager-tiff-composition-browser/v1`绑定run／profile／普通身份／源locator／item／成果ID／URL，缺失／错误即失败并收权清理。本地反例须拒强制不透明mask、移出预期地图、错误网格／mask报告。

#### TIFF 清理与共同边界

沿[公共收尾](#文件读取-case-的公共准备与收尾)先核源在时撤权／到期，再删精确文件；授权／Range／像元／生成独立报告。MSK／四成员各一额外COG，不改变小图禁提交生成；IMG／JPEG2000原四COG仍核缓存、任务／成果／物理删除及模式恢复。

本地页面／Runner／Native／T1／T2／历史截图不替Hosted，单文件／两成员MSK不替四成员；MSK／四成员各须新增Hosted证明，运行单独确认。BigTIFF、任意布局／容量、Float64地图及专业影像生产未覆盖。

#### TIFF 分版本证据

| 范围 | 最小证据与未覆盖项 |
| --- | --- |
| MinIO小图 | [布局修复与限定Hosted](../evidence/资源读取与预览历史证据.md#tiff-cog-布局误判--有界格式与未知状态修复)，正向与overview反例分开认定。 |
| 刷新／NFS单文件 | [页面、Range与自然到期](../evidence/资源读取与预览历史证据.md#nfs-tiff-渲染授权自然到期与清理--hosted-t4)，首败与最终范围按正文。 |
| MSK本地／准备 | [接入](../evidence/资源读取与预览历史证据.md#msk-纳入-tiff-完整组成2026-11-12) · [本机实操](../evidence/资源读取与预览历史证据.md#msk-singlemulti-本机普通用户实操与清理2026-11-12) · [自动化入口](../evidence/资源读取与预览历史证据.md#msk-自动化验收入口与本地门禁2026-11-12) · [迁移阻断](../evidence/资源读取与预览历史证据.md#tiff-285-已应用迁移被原位修改--system-启动阻断2026-11-12)。 |
| MSK Hosted | [首轮计数失败](../evidence/资源读取与预览历史证据.md#msk-hosted-计数失败与修复后待验2026-11-13) · [修复后待验](../evidence/资源读取与预览历史证据.md#计数修复的自然-ci-与未覆盖范围)：识别不证后续组成／Range／COG与mask／撤权／删除已执行，清理不抵消失败。 |

### ENVI 双文件 COG（限定 Hosted 已验收）

导航：[身份与来源](#envi-身份与来源) · [精确组成](#envi-精确组成授权) · [节点授权](#envi-minio-持续节点授权) · [COG／缓存](#envi-cog-与缓存) · [撤权／组成变化](#envi-撤权恢复与组成变化) · [清理](#envi-清理与失败) · [现行入口与历史证据](#envi-验证与历史证据)。

#### ENVI 身份与来源

`native-envi`沿[公共准备](#文件读取-case-的公共准备与收尾)，不启动模型／文档／点云。消费十权限，源初始化十一项加`meta.scan_task.execute`／`meta.scan_task.read`为十三项，不扩其他case／真实账号／角色／Grant。NFS首次`envi`有界扫及组成／头版本变更重扫归初始化，MinIO归消费者；逐次记存储／范围／阶段／身份／execution，不用已禁Deny。

NFS／MinIO各三份独立256×256三波段Byte：BSQ/WGS84、BIL/UTM50N、BIP无定位。核`media/envi/multi`、唯一双ref／完整扫描／原尺寸／两SHA；Meta仅结构／CRS，refs／尺寸／hash／extent由同User经Manager受控取回并分存。准入见[ENVI正式规范](../../builtin-formats/媒体与地图数据集.md#envi-双文件只读)。

#### ENVI 精确组成授权

六精确项仅用完整双文件read及必要metadata、不混节点Grant。分离办理用户核System完整组成、批准后，以真实扫描`item_id`的正式locator消费；缺item_id的400不算拒权。未批／全撤后主项隐藏、详情404，原件下载拒绝；批准后ZIP两原件逐字节／hash匹配。浏览器分别核结构投影／受控内容／ZIP，不向公开Meta索取refs／尺寸／hash／extent。

#### ENVI MinIO 持续节点授权

v4的bucket／prefix矩阵先节点metadata_read／read，清零后六精确项；同隔离Tenant／用户／办理身份与MinIO，`minio_node_scope`仅加六临时对象：bucket metadata无内容；prefix非递归覆盖直属及签发后新建首次识别直属、拒深层／相邻，递归仅扩后代；bucket核两递归范围。撤read仍有独立metadata可见，拒绝须真实item_id、允许ZIP核完整成员。

到期历史核主体／节点／动作／到期，不重复撤到期命令；有效命令显式撤。`minio_node_scope`存命令／检查／清理，失败仍核当前关系零／详情隐藏／六副本删除、原输入不变。

#### ENVI COG 与缓存

四定位源经正式action、当前User来源与独立目的EA生成COG，逐份核196608值／mask、CRS／网格／布局、HTTP／物理hash及Range。两无定位BIP仅原件／内容，无地图／直接图片解码／COG动作。真实Console核三色像元／属性／边界／ZIP／截图，重开同缓存、零额外生成。

原件按钮沿当前User认证读取，authenticated-fetch／Blob保存；逐项核同账号、无Cookie／URL凭据、精确locator／完整ZIP与`current_user_download_verified=true`。Blob URL不是服务端入口，API不代按钮。

#### ENVI 撤权恢复与组成变化

每源撤read须原件403／Meta404／COG缓存拒绝；按同完整清单重批，恢复原成果同Range字节、零新产物，不用旧Deny报告。双存储各BSQ改头命名并重扫，旧清单拒绝，撤旧Grant／批新后可读，最后恢复原组成并重批。同名头内容变化另拒旧COG缓存，恢复原字节并重扫才恢复。

#### ENVI 清理与失败

沿[公共收尾](#文件读取-case-的公共准备与收尾)，ENVI按序：正式API删成果／任务并核物理对象／精确暂存输入缺失→撤完整read、核六项隐藏／404→删双存储精确源。失败先查终态实际结果，API删除后返回精确清单供物理核；创建成果前失败记`results_observed=0`，部分成果逐项核删。全部任务后只读核独占Tenant COG namespace／暂存零残留，记`artifact_residuals`。

退出诊断仅本部署Redpanda固定状态／末80行脱敏日志（≤8 KiB），拒其他Compose；失败记`unavailable`，不替业务／清理结论。

#### ENVI 验证与历史证据

现行[Owner Runner](../../../../scripts/test/manager-envi-online.py)为`addp.manager-envi/v6`、媒体页面v4。v6新增MinIO根范围／授权工作区及NFS定向刷新，下表v3／v4不覆盖这些阶段。`manager-envi.json`／`manager-envi-browser.json`仍核结构／内容分离和当前User下载，v2／v3不证节点。

三交错／双端序／七实数类型、NoData／NaN／mask及漂移／头变更／歧义／外部文件／非法字段／预算拒绝，由GeoPython和Common／Meta／System／Manager T1/T2分层认定；两组Runner／登记仅核接线，不补认新Hosted。

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

### KML／GML／TopoJSON 的 S3 兼容消费

`full`沿[部署与清理](#部署模型转换与通用清理)，Business MinIO／GeoPython产品Runtime、普通User十权限，三份固定hash双图层源；区别于拓扑十一权限。核验差异：

- scope-table为HTTP JSON Base64 EWKB，同一解码入口核原始／刷新几何、类型／完整坐标／Z及独立真实地图／像素，不要求GeoJSON。
- 未知CRS TopoJSON省略`source_srid/source_crs`（omitempty），`transform_status=unknown_crs`且默认暂停，不虚构SRID 0／4326；核临时选择／清除／切换／重开及原始几何。
- 活动控件`button.ol-zoom-out`的可访问名“–”而非提示“Zoom out”；中英文T3／Hosted helper各连续点击两次，每次等动画、核缩放减1，再核像素／原几何／实例；刷新与原件下载hash另核。
- 先Security，标准身份夹具等Manager持久保护投影首同步，预检健康／源码；启动／初始化失败拒入场并清理。扩展最低Owner／browser v13、拒旧版；现行版本按[full固定断言](#表格json-与八模型固定断言)。

`make test-manager-online-runner`核夹具／拒绝／浏览器语法／登记。MinIO只证S3兼容，外部云未验；不登记本项T4通过。

### 设备静态轨迹范围读取

同`full`／Business MinIO／十项User，无新增服务／任务／成果／权限；字段／预算／保护见[Manager预览规范](../../../../manager/docs/数据预览与资源树实现规范.md#六设备静态轨迹预览)。独有断言：

- CSV／records JSON／Point GeoJSON各3000合成点，A／B各1500小时；UTC `[2026-01-01T00:00:00Z,2026-01-02T00:00:00Z)`为双设备48／A24点。独立表单核设备／时间／坐标／像素、请求与`trajectory_scope` selection一致、`complete=true/scanned_rows=3000`。
- 整文件拒前2000点绘图、保完整性提示，GeoJSON自动CRS不清提示，重开重验。超限API `422/trajectory_point_limit_exceeded`，缩回恢复，不豁免其他业务失败／警告。
- 三源大小／下载SHA不变，正式下载helper不被探针替代；像素只读复制至独立离屏画布，不改地图／过滤警告。Owner／browser／CI核完整三源、拒缺失／篡改／旧版；历史Owner v9／browser v11仅证当轮。

源删除及领域／物理收尾沿[full](#部署模型转换与通用清理)，Infra不替领域零残留。`make test-manager-online-runner`／`make test-online-runner`核源字节／大小／下载登记、两次生成一致、三格式等价、上述计数及报告拒绝；[历史证据](../验收证据索引.md#manager-静态轨迹历史证据)分本机／Hosted／CI，不以Runner代Hosted。

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
