# ADDP Online 场景：Manager 文件与预览

[返回测试与验收总入口](../../addp测试与验收规范.md) · [返回空间成果场景](Manager与空间成果.md)

共同前置条件见[T4 Online 验收规范](../T4-Online验收规范.md)。各 case 的身份、权限、部署、清理及证据范围分别认定；代码就绪不表示真实 T4 已通过。

导航：[case 选择](#明确的验收-case) · [公共准备与收尾](#文件读取-case-的公共准备与收尾) · [GRIB2](科学容器只读验收.md#grib2-单文件只读) · [NetCDF](科学容器只读验收.md#netcdf-单文件只读) · [其他格式读取](#surfer-grd-读取) · [模型与完整场景](#部署模型转换与通用清理)。

## manager-internal-artifact-lineage

### 明确的验收 case

唯一选择参数为 `ADDP_ONLINE_MANAGER_CASE`，省略用 `full`，空值或未知值在 Hosted 生命周期动作前拒绝。下表对应 gate、workflow 的 `manager_case` 和业务分发器。身份及收尾沿[文件读取公共要求](#文件读取-case-的公共准备与收尾)和各 case 正文，`full` 身份见[部署与清理](#部署模型转换与通用清理)。

| case | 核对范围 |
| --- | --- |
| `full` | 模型、授权、点云、文档及[固定断言](#表格json-与八模型固定断言)。 |
| `native-raster-media` | [IMG／JPEG2000](#img-原生扫描与-cog-快显)、[Surfer GRD](#surfer-grd-读取)、[TIFF 无定位小图](#tiff-无定位小图的-cog-布局回归)。 |
| `native-table-orc` | [ORC](#orc-单文件表格-hosted)的嵌套只读和平坦字段批量传输。 |
| `native-table-avro` | [Avro](#avro-单文件表格读取)三种 codec、分页和仅平面批量写 Parquet。 |
| `native-table-feather` | [Feather V2](#feather-v2-基础标量独立-hosted)六源分页、Parquet 与原件。 |
| `delimited-read-settings` | [CSV／TSV GB18030](#csvtsv-gb18030-独立读取设置-case)编码、表头和分页，无派生产物。 |
| `native-netcdf` | [NetCDF](科学容器只读验收.md#netcdf-单文件只读)变量和切片。 |
| `native-grib` | [GRIB2](科学容器只读验收.md#grib2-单文件只读)逐消息数值与切片，[GRIB1](科学容器只读验收.md#grib1-扩展验收限定正式-hosted通过)、[5.3 二阶差分](科学容器只读验收.md#grib2-模板53二阶空间差分扩展限定正式-hosted通过)、[ECMWF CCSDS](科学容器只读验收.md#ecmwf-grib2-ccsds-扩展限定正式-hosted-已通过)、[IFS 累计降水](科学容器只读验收.md#ecmwf-ifs-累计降水扩展限定正式-hosted-已通过)、[IFS 0h 基线](科学容器只读验收.md#ecmwf-ifs-0h-累计降水基线限定正式-hosted-已通过)按各自 profile 认定。 |
| `native-envi` | [ENVI 双文件 COG](#envi-双文件-cog限定-hosted-已验收)的精确组成、节点授权、COG／缓存及恢复。 |
| `kml-export` | [KML 导出](#kml-实际页面导出)的 PostGIS 源、页面按钮及 XML 回读。 |
| `filegdb-export` | [PostGIS→FileGDB](#filegdb-实际页面导出)的 ZIP 回读、跨账号与排队撤权。 |
| `filegdb-mysql-export` | [MySQL→FileGDB](#mysql-filegdb-实际页面导出)的独占 MySQL 源、页面及撤权。 |
| `filegdb-authorization` | [NFS FileGDB 整容器授权](授权与共享.md#nfs-filegdb-整容器授权2026-10-29-接入范围)的子表／ZIP 读取、到期和故障恢复。 |
| `nfs-upload-initial` | [NFS 上传新项授权](授权与共享.md#nfs-上传新项初始授权)。独有可写目录，核新建、覆盖、重放和部分失败，无追加。 |
| `minio-node-rescan` | 独占 Business MinIO 中登记两条目标 prefix 内的对象及一条同桶对照对象。真实删除第一条后 basic 重扫，再删除最后一条并 deep 重扫。每次核原生对象清单、Meta Worker 成功回执、普通 User 资源树及 Manager 搜索精确指纹集合。保留项 ID 与指纹保持一致，已删详情返回 404。退出核撤销当轮节点 Grant 和隔离资源清零。 |

入口示例：`ADDP_ONLINE_MANAGER_CASE=native-raster-media make test-online ONLINE_SUITE=manager-internal-artifact-lineage`。人工 Hosted 指定同名 `suite`、`manager_case`、`manager_source_commit`。源码参数须为完整 40 位小写 SHA，检出后 HEAD 必须精确相等且属于 dispatch 提交的祖先或自身，才准备工具和部署。日志分列源码及 dispatch SHA，应用、Runtime、夹具和断言取所选源码。其他 suite 不消费此参数，旧 workflow 不作固定源码入口。

Job、Artifact、`manager-case.json` 均记录所选 case。独立通过不计 full，full 的模型源授权正向断言受阻仍按失败认定。case 分发、版本拒绝、退出及归档沿 `make test-manager-online-runner` 与现有 CI 登记。

`native-raster-media` 的 `manager-native-raster-media.json` 使用 `addp.manager-native-raster-media/v2`，须有普通身份、case、`source_formats=[img,jpeg2000,grd,tiff]`、四份子报告和实际失败状态，任一子场景失败即失败。`online-report.json` 归统一分发器。该 case 仅准备基础服务、GeoPython／GDAL、物理校验器及 Console，保留 Infra、Business MinIO／NFS、临时 registry、原样本和 NFS 读取方式。无 Model3D 构建／启动，无 PointCloud、Document 或结构化数据业务，按实际组件清理。生命周期门禁另核失败收尾及 full 保留的运行时。

### 文件读取 case 的公共准备与收尾

下表适用于文件读取 case。full、导出／上传及独立持久化沿各自正文。组件与 full 身份见[部署](#部署模型转换与通用清理)，拓扑和外层退出见[T4 规范](../T4-Online验收规范.md)。

| 共同要求 | 验收方式 |
| --- | --- |
| 身份与投影 | 初始化、消费分离，浏览器不持办理凭据。`metadata_read` 只证明元数据可见。Meta 核身份、格式、布局和声明 CRS。尺寸、波段、范围等由同一普通 User 经 Manager 受保护内容入口与物理 oracle 对照，分开归档。无定位不推 CRS，受阻不扩权。 |
| 页面与原件 | API、真实按钮和独立 oracle 分别核完整字节、大小及 SHA-256。下载许可按 case、存储和固定版本认定。源变化后的恢复仅适用于已获准操作。 |
| 清理 | 删除源前核撤权或到期。Grant、任务、成果及精确物理删除逐项取证，外层 Infra 清零不代领域清理。无任务／缓存的 case 无需伪造删除或复用。未终结或归属未明不报零残留，删定义不证执行史清理。首败与清理错误分列，任一失败整套失败。 |
| 证据 | Native、T1–T3、部分 CI 与实际 Hosted 分开。报告、异常和下载材料不含凭据。SKP 精确组成准入仍关闭。固定版本、已验范围与未覆盖项归[资源读取证据](../evidence/资源读取与预览历史证据.md)。 |

| case | 独有身份与边界 |
| --- | --- |
| NetCDF／GRIB | [六项消费权限](科学容器只读验收.md#netcdf-来源与权限)，各十四份精确 metadata Grant，未迁精确内容源授权。无任务或持久缓存，GRIB 无地图。[GRIB1 初始化及 NFS 原件拒绝](科学容器只读验收.md#grib1-扩展验收限定正式-hosted通过)按自身版本。 |
| ORC／Avro／Feather | [十一项消费权限](#orc-入口与权限)，Avro／Feather 准备十三项、初始化负责扫描。Avro／Feather 核 MinIO API／按钮原件及 NFS 403 JSON、无附件或按钮。Transfer 与只读拒绝沿各格式正文。 |
| GB18030 | 消费十／准备十三项。Common Go reader，初始化 basic 与精确 Grant，无派生，Transfer T4 独立认定。 |
| 栅格媒体 | 消费十项，初始化仅签当轮 MinIO `raster` 前缀直属非递归 read，退出撤回。GRD 无产物，TIFF 扫描资格、整项 read、到期和 sidecar 另验。 |
| ENVI | 消费十／准备十三项。NFS 首扫及变更重扫归初始化，MinIO 归消费者，节点与精确组成分阶段。 |

`make test-manager-online-runner`、`make test-online-runner` 核 case、报告及失败退出。原生媒体夹具由 `make test-geopython-workflow` 自动发现，各格式门禁沿现有 owner。

导出共用真实 Console、独立回读、源内容／结构不变和独占源收尾。会话沿既有生命周期，设施销毁不算会话 DELETE 或缓存验收。

### GRIB2 单文件只读

授权、数值、页面、原件、清理与固定版本证据见[科学容器专题](科学容器只读验收.md#grib2-单文件只读)。

- <a id="grib2-授权与来源"></a>[GRIB2 授权与来源](科学容器只读验收.md#grib2-授权与来源)
- <a id="grib2-逐消息数值"></a>[GRIB2 逐消息数值](科学容器只读验收.md#grib2-逐消息数值)
- <a id="grib2-页面切片与原件"></a>[GRIB2 页面切片与原件](科学容器只读验收.md#grib2-页面切片与原件)
- <a id="grib2-清理与未覆盖项"></a>[GRIB2 清理与未覆盖项](科学容器只读验收.md#grib2-清理与未覆盖项)
- <a id="grib2-验证与历史证据"></a>[GRIB2 验证与历史证据](科学容器只读验收.md#grib2-验证与历史证据)
- <a id="grib1-扩展验收待正式执行"></a><a id="grib1-扩展验收限定正式-hosted通过"></a>[GRIB1 扩展验收（限定正式 Hosted通过）](科学容器只读验收.md#grib1-扩展验收限定正式-hosted通过)

### NetCDF 单文件只读

授权、数值、页面、原件、清理与固定版本证据见[科学容器专题](科学容器只读验收.md#netcdf-单文件只读)。

- <a id="netcdf-来源与权限"></a>[NetCDF 来源与权限](科学容器只读验收.md#netcdf-来源与权限)
- <a id="netcdf-变量与切片"></a>[NetCDF 变量与切片](科学容器只读验收.md#netcdf-变量与切片)
- <a id="netcdf-页面与原件"></a>[NetCDF 页面与原件](科学容器只读验收.md#netcdf-页面与原件)
- <a id="netcdf-清理与未覆盖项"></a>[NetCDF 清理与未覆盖项](科学容器只读验收.md#netcdf-清理与未覆盖项)
- <a id="netcdf-验证与历史证据"></a>[NetCDF 验证与历史证据](科学容器只读验收.md#netcdf-验证与历史证据)

### Surfer GRD 读取

阅读导航：[格式准入](#grd-格式准入) · [四份样例](#grd-四份样例) · [页面与原件](#grd-页面与原件) · [清理与能力边界](#grd-清理与能力边界) · [分版本证据](#grd-分版本证据)。

#### GRD 格式准入

`DSRB`／`DSBB`／`DSAA`内容签名／驱动、≤64MiB 主文件、完整头／网格、精确 NoData／mask、Surfer7 版本差异、ASCII 词法及旁文件拒绝统一沿[媒体与地图](../../builtin-formats/媒体与地图数据集.md)的[GRD 契约](../../builtin-formats/媒体与地图数据集.md#grd--surfer-数值网格)，不复制解析规则。

Native 须逐块证 Binary／ASCII 数值与原生 mask 等价，覆盖 LF／全缺失／跨块 token／无末尾换行及畸形／多余／截断／下溢拒绝，源字节保持。不等价即拒绝，不修补 mask 或推 CRS；原生反例不当 Hosted 故障注入。

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

GRD 只清精确源，无任务／产物／缓存／快显；原 IMG／JPEG2000 四 COG 的全像元、Range、缓存及领域／物理删除仍按各自正文，不记 GRD 生成。未知CRS不定位；其他GRD变体、任意ASCII编码、写出、旁文件、NFS／FTP专项及容量未验。

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

按[公共收尾](#文件读取-case-的公共准备与收尾)核三十源／任务／Grant、六成果及marker删除，无二十四次拒绝输出。嵌套／Enum／Map／普通fixed批量未开放，local-timestamp／time等未列逻辑类型、whole、写出、所有生产者／压缩变体与容量未验；NFS 原件仍拒绝。

#### Avro 验证与历史证据

各成功轮分别核其来源／原件、允许或拒绝的 Transfer、六 Parquet 及清理；不跨轮扩展类型／存储策略。截图裁切／视口外声明／动画帧不代完整视觉证明，精确值按 DOM／API／原件／物理回读认定。

| 固定源码／作业 | 分版本结论与首败 |
| --- | --- |
| `40b1468c5576c3660e1d49c8018f8356f5a959c3`／Job128604699236 | [首败](../evidence/资源读取与预览历史证据.md#avro-单文件标量读取--hosted-验收)：列序 oracle 失败，未进页面、未归档 Parquet；此前 API／原件／计数和本地复现不补缺口。 |
| `e706899b69309fa9a7364e4c0f6118b9f5d153dd`／Job128610273652 | [六源三 codec](../evidence/资源读取与预览历史证据.md#限定正式复验通过)，15018 行；ID 截图裁切、相关门禁失败另认。 |
| `7af201a28f237a0ef88aa27a8129e54042ff5d8f`，dispatch `1082c0ed0cd2359be69841085cf5dbf02a20467e`；43584189240／132415063748／attempt1 | [UTC v3](../evidence/资源读取与预览历史证据.md#utc-时间戳限定正式-hosted-通过)：六源十三字段／15018 行原计数、NULL、UTC 注解；前两败及六截图长值裁切保留。 |
| `c63888fba953df5545a06b11c7641284c2bbe792`；[43668574041／132673333161](https://github.com/pampa0629/addp/actions/runs/43668574041/job/132673333161) | [Enum](../evidence/资源读取与预览历史证据.md#avro-enum-正式-hosted-复验2026-11-15)：十八源；首败、视口外声明／过渡帧保留。 |
| `9cf429c4a9a0ffc84f06e5971bed254c7e935faa`；[43677124474／132699620936](https://github.com/pampa0629/addp/actions/runs/43677124474/job/132699620936) | [Map](../evidence/资源读取与预览历史证据.md#avro-map-限定正式-hosted2026-11-15)：二十四源、真实分页／键树、十八拒绝，NFS 仍 403。 |
| `57d7fa52a5dd122e167d8e8ae4a7df9716f9bdae`；[43885241459／133361505614](https://github.com/pampa0629/addp/actions/runs/43885241459/job/133361505614) | [普通 fixed](../evidence/资源读取与预览历史证据.md#avro-普通-fixed-限定正式-hosted2026-11-16)：三十源、90 页 1290 行、完整具名／精确字节，二十四拒绝，NFS 仍 403。 |

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

沿公共收尾核本人任务／物理产物、源／Meta 保持及设施清理。归档`manager-orc.json`、`manager-orc-browser.json`、两平面＋两嵌套原件／Meta／截图，与`online-report.json`及源码绑定；缺真实报告不回填Hosted通过。

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

沿[公共收尾](#文件读取-case-的公共准备与收尾)核六源字节保持、撤权隐藏、六任务 404、六源／六成果／目标 marker 删除；无派生缓存／新内容授权准入。源变化／不支持类型／预算拒绝分列确定性与Native，不替跨模块权限／容量。Feather V1／IPC stream／unsigned／嵌套／dictionary／duration／写出未支持。

#### Feather 分版本证据

- [基础标量](../evidence/资源读取与预览历史证据.md#feather-v2-基础标量只读--独立-hosted-t4)：首轮正负零oracle失败、Transfer preflight缺口及修复结果保留；旧双存储均下载不替现行NFS拒绝。
- [日期／定点小数](../evidence/资源读取与预览历史证据.md#feather-v2-日期和定点小数)：只证当轮DATE／DECIMAL，保相关CI失败，不追认时间戳转存。
- [时刻／时间戳](../evidence/资源读取与预览历史证据.md#feather-v2-时刻与时间戳--正式-hosted)：首轮NFS403后退出与修订正式复验分开，首轮未进页面／物理Parquet回读。

各类型按固定版本分别认定，不由基础轮追认后继扩展。

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

按[公共收尾](#文件读取-case-的公共准备与收尾)撤 Grant 并核隐藏、精确源及设施清理；无派生，不代 Transfer 批量 T4。

#### GB18030 分层验证与历史证据

公共 Runner、受影响 System Go T1 及原 case 登记覆盖；分层证据：

| 范围 | 结论与限制 |
| --- | --- |
| [本地／Linux补证](../evidence/资源读取与预览历史证据.md#csv--tsv-的-gb18030-显式读取--本地门禁) | T1／T2／受控T3不代Hosted，旧GBK不追认本case。 |
| [失败与修正](../evidence/资源读取与预览历史证据.md#gb18030-独立-hosted-的失败记录与-meta-完整名修正) | 前三轮Tenant ID类型／误用公开投影／重复全名，第四轮会话轮换旧API凭据401保留，修复／清理不改通过。 |
| [第五轮正式结果](../evidence/资源读取与预览历史证据.md#gb18030-独立正式-hosted-通过与产物复核) | `b362628e725fc8e2fd95264b7bc06b941ea541c4`／Job128749505328：四源设置／分页／原件／全清理，字库缺字／截图浮层／本地失败保留。 |

不认证编码写出、国家标准版本全符合或更多样本；各轮产物／范围按证据正文。

### FileGDB 实际页面导出

`filegdb-export`用隔离PostGIS、GeoPython产品镜像。初始化用户扫描并签精确读Grant；同一消费用户授予前经Gateway提交每个精确表必须403，且无会话／execution UUID／下载入口。随后真实页面分别导出3857点、4326 MultiLineString／MultiPolygon，三份`.gdb.zip`各仅单图层；核Monitor执行归属／下载字节哈希／截图。同Tenant另一普通用户有导出读取权限仍须拒绝其会话及下载。独立GDAL解包核源CRS、XY、多部件边界、内环／标量／null：线顶点序不变，面允许环起点／方向变化。按公共导出收尾，与NFS FileGDB读授权分开报告。

两种FileGDB case共用执行前撤权断言：仅归属已证的Linux Hosted，用现有进程工具／pidfd暂停唯一Transfer bounded Worker；提交202后Monitor须证pending、attempt=0、未开始。原初始化用户撤销精确Grant并复核未领取，恢复同一进程后须attempt=1在来源授权preparation失败，公开计数不报告内容IO；会话failed、无download_url，文件409、重提403、源不变。异常也恢复原进程，确定性核暂停／恢复／中途异常；不改配置、增权或操作开发进程。该API拒绝不证明浏览器失败页、物理零读或缓存。

Owner v5须含独立`execution_revocation`、每表无Grant拒绝、原三页面／清理及browser v3绑定；旧v3不证撤权。运行中途撤权／自然到期、其他引擎、ZM／多层／复杂字段／容量不因此扩展。

### MySQL FileGDB 实际页面导出

`filegdb-mysql-export`沿上节页面／OpenFileGDB oracle／授权与撤权流程；唯一新增夹具为独占临时MySQL：既有T2固定镜像、随机loopback端口、tmpfs及仅可读三表账号，不连个人Business。初始化13项／消费6项／同Tenant旁观1项权限不变。

三InnoDB普通表分别点3857、复线／含内环复面4326，各三行含中文／空串／null、布尔／小数／空几何；身份为server→database→table，保留数据库轴序／CRS，视图／自定义SQL由guarded reader拒绝。独立注册／快照／运行／清理，不借PostGIS schema或历史通过。

同Owner v5／browser v3绑定case与source_engine_type，旧v4不证MySQL。`business/scripts/online-manager-mysql-fixture.sh`的start／精确快照／stop须核case及容器归属，保留 Registry／GeoPython 及部署清理，原进程工具／身份／Runner／登记核接线。

### TIFF 无定位小图的 COG 布局回归

导读：[MinIO 小图](#tiff-minio-无定位小图) · [NFS 自然到期](#tiff-nfs-渲染与自然到期) · [MSK 组成](#tiff-msk-组成扩展) · [清理与边界](#tiff-清理与共同边界) · [证据](#tiff-分版本证据)。

`native-raster-media`保持原十项消费权限、预算及IMG／JPEG2000／四种GRD／原四COG；Hosted 服务范围按前述 case 准备要求限定。子报告`addp.manager-raster-media/v2`、页面`addp.manager-raster-media-browser/v4`；COG规则见[TIFF布局提示](../../builtin-formats/媒体与地图数据集.md#tiff-云优化布局提示)。

#### TIFF MinIO 无定位小图

自建240×120 Classic TIFF COG，Float64/NaN、512瓦片、DEFLATE、无CRS／网格／overview。不上传个人成果或提交该小图生成；保留既有非定位可选转换按钮，无地图不等于无按钮。

- **物理**：GDAL完整COG验证0错误／0警告；独立公式核28,800值、35 NaN／完整mask、块尺寸／空CRS／空网格／零overview及原大小／SHA，源改写后重签哈希收据仍拒绝。
- **刷新**：同User经Gateway精确item同步刷新，核请求／响应、前后Meta结构／扫描时间／独占源scan execution，`full_name`／指纹／item ID不变。`storage.last_modified_at`按带时区同一时刻比，其他attributes／源字节不变。Manager受控事实为`tiff/cog/true/heuristic`、240×120、tiled=true／overview=false、无spatial，与公开Meta分存。
- **页面／原件**：真实属性／预览核同源240×120单页、Range206、完整Float64灰阶／透明NoData、无地图；API／按钮交付同一single TIFF。重开仍核零生成／业务失败／警告及源／Meta不变，显式刷新除外。

#### TIFF NFS 渲染与自然到期

`nfs_tiff_authorization`将同SHA小图复制至本轮只读NFS；仅初始化用户增加`meta.scan_task.execute`／`meta.scan_task.read`单目录深扫。短签／收回精确metadata授权取得正式DataItem ID，消费仍十项、同一普通API会话：

- **授权与投影**：仅metadata时Meta／preview200、结构白名单、无内容地址／摘要／统计、Range403；加精确read后完整preview／Range206；撤read保metadata白名单，撤metadata后Meta／preview404、Range403。只增本轮精确read命令，不增目标；根报告核全状态，异常仅清本轮命令。
- **独立到期**：沿原90秒read核Range 403→206→403，正向核Content-Range／实际字节；有效时真实Console核同账号、Range与完整灰阶／透明像元。自然到期后Meta／preview404，正式历史须证已到期、无撤销记录，不以手动撤权代替。
- **capability边界**：上述五阶段沿原普通User核capability状态及no-store；不可见404、metadata_only最小投影无内容／视图／产物事实，read核完整raster事实。无空间范围仍unavailable，不据此声称地图就绪。
- **真实阶段页面**：仅元数据、撤read保元数据、全撤及自然到期分别在状态切换后打开真实Console，逐项核中／英文首次打开和重新加载。前两者明确metadata_only且属性single，无影像、下载、统计及快显；后两者须实际观察精确目标祖先链404，持久显示资源不可访问说明，无Axios英文弹窗、请选择数据空态或旧内容。页面重新打开证明不外推为原标签页无刷新自动收权。原read阶段继续独立核240×120完整像元。
- **报告与预期拒绝**：阶段页面使用 `addp.manager-tiff-permission-browser/v1`，每阶段四次观察须有语言、打开方式、同普通User身份、精确请求／状态、DOM断言及PNG截图；缺任一阶段证据整套失败。只将该阶段精确祖先链GET404及由它产生的限定错误诊断计为预期，其余HTTP／页面异常仍失败，不全局忽略404或console.error。沿既有授权阶段取证，不新增Grant、权限、服务、suite或预算；新报告仅是验收要求，不表示已执行Hosted。
- **隐藏引擎定位**：全撤／到期后目标引擎从可见列表消失，仍须通过正式 locator 的 Meta 祖先事实进入持久提示。Console 组件回归同时覆盖其他引擎可见和列表为空，保留目标引擎仍可见但祖先／预览404的原用例；Hosted observer 的精确响应匹配、状态码与报告要求不放宽。


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

按[公共收尾](#文件读取-case-的公共准备与收尾)，授权／Range／像元／生成独立报告。MSK／四成员各一额外COG，不改变小图禁提交生成；IMG／JPEG2000原四COG仍核缓存、任务／成果／物理删除及模式恢复。

本地页面／Runner／Native／T1／T2／历史截图不替Hosted，单文件／两成员MSK不替四成员；MSK／四成员各须新增Hosted证明，运行单独确认。BigTIFF、任意布局／容量、Float64地图及专业影像生产未覆盖。

#### TIFF 分版本证据

| 范围 | 最小证据与未覆盖项 |
| --- | --- |
| MinIO小图 | [布局修复与限定Hosted](../evidence/资源读取与预览历史证据.md#tiff-cog-布局误判--有界格式与未知状态修复)，正向与overview反例分开认定。 |
| 刷新／NFS单文件 | [页面、Range与自然到期](../evidence/资源读取与预览历史证据.md#nfs-tiff-渲染授权自然到期与清理--hosted-t4)，首败与最终范围按正文。 |
| MSK本地／准备 | [接入](../evidence/资源读取与预览历史证据.md#msk-纳入-tiff-完整组成2026-11-12) · [本机实操](../evidence/资源读取与预览历史证据.md#msk-singlemulti-本机普通用户实操与清理2026-11-12) · [自动化入口](../evidence/资源读取与预览历史证据.md#msk-自动化验收入口与本地门禁2026-11-12) · [迁移阻断](../evidence/资源读取与预览历史证据.md#tiff-285-已应用迁移被原位修改--system-启动阻断2026-11-12)。 |
| MSK／四成员 Hosted | 固定源码 `ec48ca2d7114363093a767429ee66fd77543860f` 的组成授权、成员 Range、MSK 透明掩膜、四成员 COG 定位、首次显示／重开及撤权清理均通过。[本轮验收证据](../evidence/资源读取与预览历史证据.md#tiff-hidden-engine-hosted-20261118) · [历史首轮计数失败](../evidence/资源读取与预览历史证据.md#msk-hosted-计数失败与修复后待验2026-11-13)。 |

### ENVI 双文件 COG（限定 Hosted 已验收）

导航：[身份与来源](#envi-身份与来源) · [精确组成](#envi-精确组成授权) · [节点授权](#envi-minio-持续节点授权) · [COG／缓存](#envi-cog-与缓存) · [撤权／组成变化](#envi-撤权恢复与组成变化) · [清理](#envi-清理与失败) · [现行入口与历史证据](#envi-验证与历史证据)。

#### ENVI 身份与来源

`native-envi`沿[公共准备](#文件读取-case-的公共准备与收尾)，不启动模型／文档／点云。消费十权限，源初始化十一项加`meta.scan_task.execute`／`meta.scan_task.read`为十三项，不扩其他case／真实账号／角色／Grant。NFS首次`envi`有界扫及组成／头版本变更重扫归初始化，MinIO归消费者；逐次记存储／范围／阶段／身份／execution，不用已禁Deny。

NFS／MinIO各三份独立256×256三波段Byte：BSQ/WGS84、BIL/UTM50N、BIP无定位。核`media/envi/multi`、唯一双ref／完整扫描／原尺寸／两SHA；Meta仅结构／CRS，refs／尺寸／hash／extent由同User经Manager受控取回并分存。准入见[ENVI正式规范](../../builtin-formats/媒体与地图数据集.md#envi-双文件只读)。

#### ENVI 精确组成授权

六精确项仅用完整双文件read及必要metadata、不混节点Grant。分离办理用户核System完整组成、批准后，以真实扫描`item_id`的正式locator消费；缺item_id的400不算拒权。未批／全撤后主项隐藏、详情404，原件下载拒绝；批准后ZIP两原件逐字节／hash匹配。浏览器分别核结构投影／受控内容／ZIP，不向公开Meta索取refs／尺寸／hash／extent。

#### ENVI MinIO 持续节点授权

节点／service 根／授权页的阶段、范围和版本统一沿[MinIO 授权专项](授权与共享.md#minio-持续-bucketprefix-授权2026-11-04)，不在格式 case 复述矩阵。v4 原 `minio_node_scope`在六精确项前，复用同 Tenant／消费／办理和 MinIO，仅六临时副本；核 metadata 无内容、本级／递归／未来项／相邻拒绝、撤 read 保独立元数据、真实 item_id 及完整 ZIP。

`minio_node_scope`保命令／检查／清理；有效命令显式撤，到期只核主体／节点／动作／历史，不补造撤销。失败也核所有来源 0、详情隐藏、六副本删除及原输入不变；根／页面的新阶段不替原节点六项。

#### ENVI COG 与缓存

四定位源经正式action、当前User来源与独立目的EA生成COG，逐份核196608值／mask、CRS／网格／布局、HTTP／物理hash及Range。两无定位BIP仅原件／内容，无地图／直接图片解码／COG动作。真实Console核三色像元／属性／边界／ZIP／截图，重开同缓存、零额外生成。

原件按钮沿当前User认证读取，authenticated-fetch／Blob保存；逐项核同账号、无Cookie／URL凭据、精确locator／完整ZIP与`current_user_download_verified=true`。Blob URL不是服务端入口，API不代按钮。

#### ENVI 撤权恢复与组成变化

每源撤read须原件403／Meta404／COG缓存拒绝；按同完整清单重批，恢复原成果同Range字节、零新产物，不用旧Deny报告。双存储各BSQ改头命名并重扫，旧清单拒绝，撤旧Grant／批新后可读，最后恢复原组成并重批。同名头内容变化另拒旧COG缓存，恢复原字节并重扫才恢复。

#### ENVI 清理与失败

沿[公共收尾](#文件读取-case-的公共准备与收尾)，ENVI按序：正式API删成果／任务并核物理对象／精确暂存输入缺失→撤完整read、核六项隐藏／404→删双存储精确源。失败先查终态实际结果，API删除后返回精确清单供物理核；创建成果前失败记`results_observed=0`，部分成果逐项核删。全部任务后只读核独占Tenant COG namespace／暂存零残留，记`artifact_residuals`。

退出诊断仅本部署Redpanda固定状态／末80行脱敏日志（≤8 KiB），拒其他Compose；失败记`unavailable`，不替业务／清理结论。

#### ENVI 验证与历史证据

现行[Owner Runner](../../../../scripts/test/manager-envi-online.py) `addp.manager-envi/v6`、媒体页 v4；`manager-envi.json`／`manager-envi-browser.json`绑定 case、结构／内容及当前 User 下载，v2／v3 不证节点，v3／v4 不证后增根／授权工作区／NFS 刷新。

GeoPython 及 Common／Meta／System／Manager T1/T2 另核三交错／双端序／七实数类型、NoData／NaN／mask、漂移／改头／歧义／外部文件／非法字段／预算拒绝；两 Runner／登记只证接线，不补新 Hosted。

| 固定来源／作业 | 最小结论与版本边界 |
| --- | --- |
| [早期原件／COG／页面](../evidence/资源读取与预览历史证据.md#envi-双文件读取与-cog-hosted-t4)／[授权首败](../evidence/资源读取与预览历史证据.md#envi-现行授权契约复验--待正式结果) | 无效 locator400 不证拒权，旧版不追认新契约。 |
| `beb3925600d6480af5722abe1df58103a4328dcb`／126236035325 | [NFS 初始化 scan403](../evidence/资源读取与预览历史证据.md#envi-现行-nfs-初始化扫描身份修正2026-11-04正式复验待运行)，MinIO 局部／清理不证六项／页面。 |
| `9e5a94b7e48d654cfcb844deec8d2ddb8d0f1557`／126269226975 | [v3 精确组成](../evidence/资源读取与预览历史证据.md#envi-精确双文件现行权限正式复验通过2026-11-04)：六项、原件、四 COG／缓存／页面／清理，不含节点。 |
| `516822366b3d976a3cb7d59ba742be616fc158fa`；[41741133610／126282637030](https://github.com/pampa0629/addp/actions/runs/41741133610/job/126282637030) | [v4 节点＋原六项](../evidence/资源读取与预览历史证据.md#envi-minio-持续节点新建文件及到期独立-hosted-通过2026-11-04)：12 内容判定、5 撤回／1 到期；早期 runner 不代末次清理修正后完整重跑。 |
| `f8da295bbdce679ef0170abe3cb452727c420bed`；[根／授权页正式证据](../evidence/数据保护与执行血缘历史证据.md#minio-service-根导航完整-hosted2026-11-15) | 后继根 API 六项、节点六项、System 授权页七项及原 ENVI／COG／浏览器／NFS 定向刷新／领域和设施清理通过；只证授权页＋消费 API，不新增消费者导航页交互，也未新增实故障注入。 |

Byte 及所列 MinIO 节点／根／授权页分别认定；不能笼统记“全部表单未验”，也不推广任意表单、其他引擎／格式、光谱／64bit 整数／复数／压缩／外部资源／导出／容量。

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

公共收尾另核四成果／任务正式删除、内容 404／物理缺失及源预览模式恢复；`manager-img*.json`／`manager-jpeg2000*.json`分别归档。现行子报告`addp.manager-raster-media/v2`、页面`addp.manager-raster-media-browser/v4`，历史版本不追认新版；runner／原生门禁及既有登记核子场景／浏览器／回归接线，不增定时。

#### IMG／JPEG2000 历史证据与未覆盖项

| 范围 | 最小证据与限制 |
| --- | --- |
| IMG早期HFA | [原生链路](../evidence/资源读取与预览历史证据.md#manager-内部产物与-minio-模型入口--hosted-t4)，不证明JPEG2000。 |
| JPEG2000首轮 | [外层成功、独立页面未过](../evidence/资源读取与预览历史证据.md#jpeg2000-初轮--hosted-外层成功独立页面复核未通过)，保留破图／旧报告漏检。 |
| 独立媒体case | [正式收口](../evidence/资源读取与预览历史证据.md#imgjpeg2000-独立媒体栅格-case--hosted-t4)仅当轮七源／四COG／页面／清理，不追认当前报告或full；TIFF见[分版本证据](#tiff-分版本证据)。 |

IMG导出／外部IGE或RRD／依赖／复数或异质波段／自定义WKT／NFS Hosted、JPEG2000外部定位或PAM／JPX组合／NFS或FTP／写出，以及任意厂商／容量未验。二者不替GRD／TIFF断言，亦不认证整轮CI／完整Manager或专业影像生产。

### 部署、模型转换与通用清理

`online-hosted-manager-gate.sh` 使用 GitHub Hosted Linux x86_64 disposable，按 case 准备 Infra、独占 Tenant／最小 User 与 Business MinIO Engine。凭据只在仓外当次 owner-only Secret 目录，无 macOS self-hosted 分发。组件启动与业务验收分开。[T4 专用环境](../T4-Online验收规范.md#专用环境)

| 组件 | 部署归属 |
| --- | --- |
| 应用 | 标准开发生命周期选 Security／Meta／Manager／Monitor，表格及部分导出另选 Transfer，依赖 System／Gateway／Console 由入口启动。 |
| PointCloud／Document | 对应分支启动原生 Python，以 PID 和回环监听核归属。版本检查不代 COPC／PDF 页面。 |
| Model3D | 正式 Linux amd64 转换器与 Runtime 产品镜像，默认入口。 |
| GeoPython | 共享 Hosted helper 构建并启动产品镜像，NFS case 只读挂载当次夹具。 |

先起 Security，标准身份夹具等待 Manager 保护投影首同步。参与服务 Build ID／Ready／源码一致才进入业务，前置失败按生命周期退出。Runtime 启动前设置 `ADDP_ONLINE_TEST_RUN_ID=manager-<GITHUB_RUN_ID>-<GITHUB_RUN_ATTEMPT>`，报告与标签同 ID，缺任一输入在变更前拒绝并清凭据。生命周期模拟保留 suite 标签。

**full 共同要求。** 原普通 User 十项权限含 `manager.content.read` 下载及 `manager.data_item.update` 设置／刷新。Owner 拒权限缺项、多项或管理员角色。确定性 LAS、3 页 PPTX、PNG 贴图 DAE／3DS、IFC4 米制立方体及内嵌 DXT5 红贴图的 zlib 单 OSGB 同轮 Meta 深扫，通过正式 owner action／task 生成 COPC／PDF／GLB。模型网格、原件和报告版本见[固定断言](#表格json-与八模型固定断言)。Manager／Monitor 核同一输入输出血缘，真实 iframe 核 GLB 请求、加载与截图。

Chromium 显式 SwiftShader 软件 WebGL 和实际绘制。只分类 `GPU stall due to ReadPixels`，诊断浏览器报告 v5 单列来源、阶段及次数，含重投影、预览和截图，不能以预览归因截图，限流条数不证明 GPU 调用或无读取。其他业务警告、错误与失败请求仍失败。重投影 T3 核像元及无 Canvas getImageData。

文件 case 沿[公共收尾](#文件读取-case-的公共准备与收尾)。full 按捕获 ID 经正式 API 清产物／任务，内容和任务各 404，恢复源预览模式。禁止直接改 DB 或 Infra MinIO。十一类领域／物理清理、独立 PDF／COG 物理核验保留，最后销毁当轮应用、Business 容器、网络、卷和 Infra。确定性入口为 `make test-online-runner`／`make test-manager-online-runner`，真实入口为 `make test-online ONLINE_SUITE=manager-internal-artifact-lineage`。

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
- 保护投影和构建准入沿[部署前置](#部署模型转换与通用清理)。扩展最低Owner／browser v13、拒旧版；现行版本按[full固定断言](#表格json-与八模型固定断言)。

`make test-manager-online-runner`核夹具／拒绝／浏览器语法／登记。MinIO只证S3兼容，外部云未验；不登记本项T4通过。

### 设备静态轨迹范围读取

同`full`／Business MinIO／十项User，无新增服务／任务／成果／权限；字段／预算／保护见[Manager预览规范](../../../../manager/docs/数据预览与资源树实现规范.md#六设备静态轨迹预览)。独有断言：

- CSV／records JSON／Point GeoJSON各3000合成点，A／B各1500小时；UTC `[2026-01-01T00:00:00Z,2026-01-02T00:00:00Z)`为双设备48／A24点。独立表单核设备／时间／坐标／像素、请求与`trajectory_scope` selection一致、`complete=true/scanned_rows=3000`。
- 整文件拒前2000点绘图、保完整性提示，GeoJSON自动CRS不清提示，重开重验。超限API `422/trajectory_point_limit_exceeded`，缩回恢复，不豁免其他业务失败／警告。
- 三源大小／下载SHA不变，正式下载helper不被探针替代；像素只读复制至独立离屏画布，不改地图／过滤警告。Owner／browser／CI核完整三源、拒缺失／篡改／旧版；历史Owner v9／browser v11仅证当轮。

源删除及领域／物理收尾沿[full](#部署模型转换与通用清理)，Infra不替领域零残留。`make test-manager-online-runner`／`make test-online-runner`核源字节／大小／下载登记、两次生成一致、三格式等价、上述计数及报告拒绝；[历史证据](../验收证据索引.md#manager-静态轨迹历史证据)分本机／Hosted／CI，不以Runner代Hosted。

### 表格、JSON 与八模型固定断言

身份与退出沿[full 共同要求](#部署模型转换与通用清理)。格式契约归正式规范，本节留固定夹具、变化拒绝和报告版本。

| 来源 | 固定断言 |
| --- | --- |
| CSV／TSV GBK | 两源各 25 行／784 字节、三列中文、20＋5 和末页禁用。真实设置、刷新、无覆盖再刷新、重开及下载核原大小／SHA。切首行为数据时 `has_header=false`、Column1～Column3、原表头作首数据、26 行／20＋6，刷新与重开保设置，恢复表头后 25 行。两阶段 Meta 模式、编码、指纹和内容 hash 一致。GBK 无字节索引。 |
| XLSX | 7087 字节。Cities 25×2、Readings 2×3、Empty 0×0、HeaderOnly 0 行 2 列。`format=excel`／`data_type=container`／`layout=single`，未知总数 20＋5、末页禁用、切表重置、刷新保当前页／空表、重开首页及完整下载。dimension 仅作估算。 |
| JSON／GeoJSON | 嵌套文档走文档，25 行 records 走 20＋5 表格。相同 Point／LineString FeatureCollection 的 `.geojson/.json` 同走 `preview_type=table` 地图＋表格，核属性／几何、4326／未变换 CRS、Canvas／点击、末页、下载、刷新和重开。普通文档不进入 records 或空间路径，轨迹另验。 |
| 八模型 | 均核 `model_3d/single`、自包含 GLB 和真实网格。DAE／3DS 核贴图引用与内嵌 PNG，IFC 核 STEP schema、实体摘要及米制几何，OSGB 核内嵌 JPEG 和 UV 三角形，SKP 核英寸转米、共享双实例、彩色 PNG，禁止虚构解析元数据。MAX 沿上节。FBX 核角点、变换、颜色及 12 三角面。OBJ 核尺寸、UV、PNG 像素和不透明度，基础预览仅几何。旧 SKP 套件不补当前关闭的精确组成授权。[固定 FBX 预览边界](../../../../manager/docs/三维模型、点云与高斯泼溅预览说明.md#二数据类型与预览路线) |

**版本绑定索引。** NFS、MinIO 各有 ASCII CSV／TSV 两源，精确深扫后验证绑定索引与两页。确认 Hosted Linux x86_64、私有根及 Run owner 后等长改字段和值，NFS 拒符号链接并推进 mtime，MinIO 用固定 mc 仅处理 `index-source-versions/source.csv|tsv` 的 change／restore，核 ETag 改变，拒任意路径、数据或未知原／变更字节。两页必须 `409/preview_source_refresh_required`，无列／行／总数且不自动改 Meta，同 User 仍可下载实际字节。刷新原 item 保身份、更新绑定并返回两页新值。成败均先还原、刷新及复核原页／下载，再删当轮私有资源。

**模型原件。** 生成与缓存两阶段均核 FBX 单主文件 SHA。OBJ ZIP 恰含 model.obj（97 字节）、model.mtl（45）、texture.png（76），原成员合计 218 字节，DataItem 整体与主文件大小分别核对。API、浏览器捕获、Owner 和退出用同一 Python ZIP 校验器核包大小／SHA 与完整原成员字节／大小／SHA，拒重复、额外、加密或损坏成员。两阶段及清理后核原成员不变，表格／JSON 无派生产物。

**执行事件。** 复用十一转换执行，按 Monitor 事件 ID 游标到末页，核四类成功任务 `model_3d_glb_generation/pptx_pdf_generation/raster_cog_generation/point_cloud_copc_generation`。须分页闭合、字段精确、UUID 正确、首 claim `attempt=1`、ID 递增、带时区保留窗口时间、唯一 started 首与 completed 末。零至多条 progress 仅整数百分比，拒截断／失败终态。首末无步骤或计数，过程无参数、样本、Runtime 正文、lease 或授权。报告仅保 UUID、类型、attempt、页数及安全结果。执行事件的结果独立于物理成果、血缘和清理，失败、过期及进度写入 T4 另验。

| 报告 | 版本和实际边界 |
| --- | --- |
| GBK 表头扩展 | 最低 Owner／browser v14，v13 不认证该扩展。 |
| `index-source-versions.json` | v2 覆盖四个存储×格式案例，Owner／browser v13 是该批绑定。v1 不补四案例，新版本完整 Hosted 前 MinIO 源变化仍未验。外部 S3／一致性快照另验。 |
| full 与 OBJ 修正 | 现行 Owner／browser v15，拒 v14。 |
| `manager-execution-events.json` | `addp.manager-execution-events/v1`，完整报告与分项共用安全摘要。 |

固定输入或报告缺失／变化由原 runner 和 Hosted 登记拒绝，实际源码、失败、清理与覆盖范围见[证据索引](../验收证据索引.md#manager-预览与私有导入历史证据)。
