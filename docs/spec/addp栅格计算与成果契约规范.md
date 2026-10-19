# ADDP 栅格计算与成果契约规范

本文维护已实现的 GeoPython 栅格对象、通用算子、成果发布与资源准入约定。尚未完成的展示／规模证据与后续工作见[后续专题](../next/栅格算子体系后续专题.md)。具体公开参数由正式工作流规范和算子元数据维护，不能把验收样例当默认值。

## 一、适用范围

适用于已发布的 GeoPython 通用栅格算子、Develop 工作流成果与引擎资源策略。DEM 基础地形分析纳入通用算子；Manager 快显与 Service 地图发布按各自 owner 契约维护。

## 二、资源与执行模型

### 2.1 公开资源和内部对象

公开源使用 ResourceLocator；公开目标使用父目录 ResourceLocator、目标名和 `create/replace` 模式。Develop 在执行期分别解析、授权源与目标，派生唯一的 `addp.workflow.access-plan/v1`。任务定义和公开契约不保存凭据、明文连接、GDAL URI 或访问计划。`raster_load` 为 source-only，`raster_save` 为 target-only，`raster_to_cog` 使用两端计划。

| 对象 | 用途与边界 |
| --- | --- |
| RasterDataset | 当前 execution 内由私有文件支撑的 raster 对象，只通过当前 DAG 的 `$ref` 流转；加载创建快照，计算生成新文件，不修改上游。 |
| RasterTiles | 私有源快照与切片网格描述，通过 raster_tiles 端口惰性流转；不是公开 JSON 数组、whole scope 数据项或通用 foreach。 |
| RasterRef | 只投影尺寸、波段、CRS、transform、四角 extent、NoData 和 overview 等摘要，不建立第二种公开资源身份。 |
| RasterArtifact | 已持久化栅格的成果事实；Develop 根据授权目标构造稳定 locator，不信任 Runtime 返回的路径或 locator。 |

加载支持自包含 TIFF、PNG、JPEG，声明格式必须与实际 GDAL driver 一致；拒绝外部 sidecar 和伪装成 TIFF 的 VRT。缺少 CRS 时可显式补充 `source_crs`，不能覆盖冲突的已有 CRS。空间操作要求可信 CRS 和 geotransform；按尺寸重采样等像素操作允许缺失定位，并保留缺失事实。

栅格文件输出限定 `.tif/.tiff`，GeoTIFF 与 COG 是布局 profile。COG 编码复用唯一实现，压缩支持 DEFLATE/LZW/ZSTD/NONE，块大小为 128–4096 内的 16 倍数。目录全局 overview 及其内部金字塔从完整全局网格生成，不复用 leaf 的局部抽样金字塔。

### 2.2 生命周期与有效性

合法异步工作流由 `POST /api/workflow` 返回 202，沿标准 execution 接口查询。部署使用单 worker、多线程，保证进程内状态可查询；目录镶嵌的 GDAL 全局配置操作串行化。成功和失败均清理执行工作目录；当前公共协议没有主动取消接口。

数值分析按最多 512×512 分块，统一排除 NoData、无效 mask、非有限值及非有限或不大于零的 alpha。合法零及部分透明的有效数据正常参与计算，不按透明度缩放原数值；各数据波段独立判断有效性。数值结果使用 Float64，不承诺大整数逐位精度。

## 三、已实现能力

### 3.1 通用算子

| 算子 | 输入及配置 | 结果与约束 |
| --- | --- | --- |
| `raster_load` | source-only 访问计划、可选 `source_crs` | 内部 raster 源快照。 |
| `raster_save` | raster、target-only 计划、GeoTIFF/COG profile | 持久化成果摘要；COG 发布前做结构校验，转换失败不覆盖目标。 |
| `raster_tile` | raster、单片像素宽高 | 执行内惰性切片集合；右侧和底部保留实际尺寸，不补像元。 |
| `raster_save_tiles` | raster_tiles、target-only 子目录计划、GeoTIFF/COG profile | 按行列顺序逐片发布，稳定片名；失败保留已确认写入事实，每片独立登记资源。 |
| `raster_info` | raster | 尺寸、band dtype/nodata、CRS、transform、四角范围及 overview。 |
| `validate_cog` | raster | GDAL 完整布局校验的 valid、warnings、errors；不靠扩展名或 tiled 标记判断。 |
| `raster_to_cog` | source/target 计划、转换选项 | 唯一通用 COG 转换入口，支持 workflow/direct；旧 TIFF 专用入口删除。 |
| `raster_build_overviews` | raster、递增倍率、算法 | 新栅格金字塔，不改写源。 |
| `raster_reproject` | raster、目标 CRS、可选分辨率、算法 | 新栅格；源 CRS 与空间定位必须有效。 |
| `raster_resample` | raster、尺寸或分辨率、算法 | 两种目标网格配置互斥。 |
| `raster_clip` | raster、bbox 或 GeoJSON Polygon/MultiPolygon、边界 CRS、mode=inside/outside | 默认保留区域内；区域外模式保留源完整网格，将区域内设为 NaN，保留孔洞中的原有效值；Float64 与 alpha 表达有效性，拒绝复数波段。 |
| `raster_mosaic` | 有序 `input_rasters`、目标 CRS/分辨率、first/last、算法 | 至少两个输入，在共同网格完成一次镶嵌；Float64 输出，空白区域为 NaN；拒绝复数波段。输入顺序是计算契约，不由集合排序重排。 |
| `raster_align` | raster、参考 raster、算法 | 输出参考栅格的完整网格，保留旋转；参考像元值不参与计算，源范围外为 NaN。 |
| `raster_update` | 基底及同网格更新源 | 有效更新像元逐数据波段覆盖，无效更新保留基底；保持基底网格，多来源顺序串联。 |
| `raster_stack` | 两个同网格 raster | 按输入顺序追加全部数据波段；网格不一致时拒绝，多个输入通过成对节点组合。 |
| `raster_select_bands` | raster、波段序号数组、multispectral/rgb | 可重排和重复；RGB 必须恰好三个数据波段，不自动推荐或拉伸。 |
| `raster_band_math` | raster、受限表达式 | Float64 单波段；每块最多 512×512，传播 mask、nodata 和无效计算。 |
| `raster_reclassify` | raster、规则数组、band、unmatched=nodata/keep | 精确值或左闭右开区间重分类；同网格 Float64 单波段，源无效像元始终为 NaN。 |
| `raster_aggregate` | raster、整数列/行倍数、sum/mean/min/max | 从左上角分组，忽略无效像元，保留边缘不足整组的像元；无需空间定位，输出 Float64 数据波段。 |
| `raster_polygonize` | raster、必填 band、connectivity=4/8 | 相同整数分类值的连通区域；每区一条 Int64 class_value 与有效二维 MultiPolygon，保留源 CRS、孔洞和旋转，复用矢量保存。 |
| `vector_rasterize` | 有效二维面、同 CRS 参考网格、数值属性／常量二选一、可选所有触及像元 | 输出单波段 Float64／NaN NoData；按当前行序后者覆盖，保留参考完整旋转网格，不继承参考有效性；整数限于 ±(2^53−1)。 |
| `raster_footprint` | `raster` → `GeoDataFrame` | 所选数据波段有效范围的并集/交集，保留源 CRS、孔洞与分离面片。 |
| `raster_statistics` | raster、从 1 开始的 band | 按块全量累计 min/max/mean/总体标准差、有效/无效计数与比例；全无效时统计值为 null。 |
| `raster_zonal_statistics` | 同网格数值／整数分区 raster、必填数值／分区波段 | 同类非连通区域合并，分区无效像元排除；输出每类像元计数及 sum/min/max/mean/总体标准差普通表，全无效数值为 null，沿既有保存。Native 与真实跨模块验收分别记账。 |
| `raster_focal_statistics` | raster、必填数据波段、3／5 像元窗口、sum/mean/min/max/stddev | 首期已实现；同网格单波段 Float64，中心无效保留 NaN，忽略无效邻居，外缘只计实际像元；Native 与真实跨模块验收分别记账。 |
| `raster_vector_zonal_statistics` | raster、同 CRS 有效二维面、必填数据波段，可选覆盖规则及列前缀 | 首期已实现；每面独立，重叠分别计入，保留原面／属性／顺序并追加计数与五项统计，复用矢量保存；Native 与真实跨模块验收分别记账。 |
| `raster_histogram` | raster、band、bins、可选范围 | 按块全量计数，返回 edges/counts、有效/无效和范围外计数。 |
| `raster_slope` | raster、必填高程单位、band、degree/percent | 同网格单波段坡度，默认角度。 |
| `raster_aspect` | raster、必填高程单位、band | 投影网格北起顺时针坡向；平坦区域为 NaN。 |
| `raster_hillshade` | raster、必填高程单位、band、方位角／高度角 | 同网格地形晕渲，常规有效灰度 1–255；不表示三维场景。 |
| `raster_contour` | raster、必填高程单位、band、等高距或指定高程 | 源 CRS 的二维 LineString GeoDataFrame，`elevation` 为米；可接既有矢量保存节点。 |

除 `raster_to_cog` 外，上表算子只支持 workflow。波段表达式允许 `b1/b2/...`、常量、加减乘除、有限常数幂及 `abs/sqrt/log/minimum/maximum`，拒绝属性访问、索引、导入和任意 Python 执行。

关键语义如下，完整参数以正式契约和算子元数据为准：

- 多来源计算先显式对齐、组合，再进行表达式计算；引用波段任一无效时结果无效，不隐式跳过缺值后归一化权重。
- 局部更新只用更新源的有效像元逐数据波段覆盖基底，无效更新保留基底；不同网格先对齐，多来源顺序串联，后一次有效更新优先。
- 重分类默认 `unmatched=nodata`，可选 `keep`；规则为精确值或互不重叠的左闭右开区间，源无效像元始终无效。
- 聚合按整数列/行倍数从左上角分组，忽略无效值，全无效组写 NaN，保留不足整组的边缘；有定位时按倍数缩放仿射列、行向量。
- 区域内裁剪要求与栅格实际覆盖面存在正面积交集；区域外裁剪保留完整源网格，将边界内设为无效，孔洞保留，不按剩余有效值缩小范围。
- 有效像元轮廓默认取所选数据波段的并集，可选交集；保留孔洞、分离面片、旋转和源 CRS，输出既有 GeoDataFrame，四角 extent 不能替代轮廓。
- DEM 四项要求二维投影北向无旋转网格和明确的米／国际英尺／美制测量英尺高程单位；水平单位来自 CRS。邻域、边缘、平坦坡向和空等值线等规则以[工作流栅格契约](addp工作流计算引擎接口规范.md#46-栅格计算契约)为准，不依赖 GDAL 默认值推断。

## 四、工作流使用与批量切片

### 4.1 常用组合

在 Develop 选择 GeoPython Workflow，从资源树选择源文件，连接计算与保存节点。表格、矢量、普通 object、raster 和 raster_tiles 遵循各自端口类型；内部对象不作为公开调度输入。

| 用途 | 节点组合 |
| --- | --- |
| 单幅计算保存 | raster_load → 重投影／重采样／裁剪／波段计算 → raster_save |
| 跨文件加权 | 两个 raster_load → 显式 raster_align → raster_stack → raster_band_math → raster_save |
| 多文件拼接 | 多个 raster_load → raster_mosaic（有序 input_rasters）→ raster_save；在输入连接区增删或上下调整来源顺序。 |
| RGB 通道生成 | 成对 raster_stack → raster_select_bands（恰好三个数据波段，rgb）→ raster_save |
| 局部更新 | 基底与更新源分别加载 → 必要时对齐／选波段 → raster_update → raster_save |
| 自动批量切片 | raster_load → raster_tile → raster_save_tiles |
| 有效轮廓 | raster_load → raster_footprint → 既有矢量计算／保存节点或 JSON 预览 |
| DEM 地形栅格 | raster_load → raster_slope／raster_aspect／raster_hillshade（三选一）→ raster_save |
| DEM 等值线 | raster_load → raster_contour → 既有矢量保存节点或 JSON 预览 |

`raster_info`、统计、直方图及只读轮廓结果可在执行详情查看，不虚构持久成果或目标扫描。Manager 普通查看不隐式触发计算和业务保存，私有 COG 仍由显式快显任务生成；相关规则见 `manager/docs/快显实现规范.md`。

### 4.2 自动批量切片约定

正式名称为“栅格切片” `raster_tile` 和“栅格切片批量保存” `raster_save_tiles`。“语义分割”“实例分割”等名称保留给 AI 图像识别；这里不生成在线地图瓦片。

- 用户设置单片像素宽高，系统自动划分整幅源栅格；从左上角按行、列递增，无重叠、不重采样、不补像元，保留边缘实际尺寸及全无效片。
- 保留源数据类型、原始像元、NoData、mask、alpha、颜色解释和色表；有定位时平移完整仿射网格原点，不复制源整幅金字塔作为切片金字塔。
- 保存目标为已授权父目录下的单段子目录名；片键固定为 `r<行偏移>_c<列偏移>`，文件名为 `<片键>.tif`。逐片物化、编码与发布，不同时加载整批中间成果。
- `create` 要求目标子目录为空；`replace` 仅替换当前网格生成的同名切片，保留其他文件。单幅与批量目标重叠的 DAG 在派发前拒绝。
- 每片具有独立 file/object ResourceLocator，稳定输出为 `outputs.<保存节点>.tiles.<片键>.resource`。Meta 只消费固定层级的已确认资源叶子，沿用父执行、租户和发起人校验，不递归搜索 JSON 或扫描整个 prefix。
- 动态成果采用命名映射 Schema；Orchestrator 尚不支持按任意动态片键绑定调度输入。

### 4.3 发布失败事实

对象存储逐片发布没有整批事务。执行失败时保留此前已确认写入的片及其稳定输出和自动扫描；写入调用失败的当前片标为发布结果未知，不能计入确认清单，也不能断言远端一定没有写入。

发布已确认后发生临时文件清理错误，不撤销确认事实：仍有未发布片时，下一片为 `not_attempted`；全部已确认时保留完整清单，execution 仍为 `failed`。后续节点失败时，也保留此前已完成节点的确认产物。

失败执行的自动扫描沿用已冻结的确认产物；顶层 `lineage_facts` 仅在成功终态写入，不能要求失败 execution 生成成功 derive。上述失败语义已通过确定性回归及原 Hosted T4 的第二片拒写、首片发布后清理失败、全部发布后清理失败三个真实场景，最终验收见后续专题。

## 五、引擎资源配置

配置使用既有 Console 配置管理入口。进入 System 配置页的“引擎配置”后，先选择已注册且有可管理配置的引擎实例，再查看该实例下的“栅格引擎资源策略”；不保留独立栅格菜单或旧页面路由。引擎选择和列表加载由组合组件唯一负责，栅格编辑器只接收选中的实例 ID。

| 配置 | 初始默认值 | 修改范围与约束 |
| --- | ---: | --- |
| 进程栅格运行上限 | 2 | 平台管理员；所有租户的 workflow/direct 合计共享。 |
| 进程工作流等待上限 | 2 | 平台管理员；有界等待，满额明确拒绝。 |
| 进程 GDAL 块缓存预算 | 256 MiB | 平台管理员；全进程共享，租户不可覆盖。 |
| 租户运行额度 | 继承平台默认 2 | 当前租户的有权限管理员，可在平台上限内覆盖或恢复继承。 |
| 租户等待额度 | 继承平台默认 2 | 当前租户的有权限管理员，可在平台上限内覆盖或恢复继承。 |

租户额度不代表独享资源，所有租户仍受进程总额约束。配置属于引擎实例控制面，不进入任务参数、connection_info 或静态 capabilities；普通消费者不取得管理权限。

含栅格工作流按 execution 只占一次运行额度；direct 同步调用共享额度，立即准入或返回繁忙。资源需求通过显式 resource_groups 声明，目录镶嵌同样计入，不能仅按算子名前缀或分类推断。等待期间不提前下载、创建栅格工作目录或打开 GDAL 对象。

运行／等待额度采用版本化热更新；缩容保留已受理工作，超过新等待上限时拒绝新入队。页面区分已保存平台版本与 Runtime 应用版本，显示运行／等待数量、实际缓存及待重启状态。新进程在首个栅格请求到来时读取权威策略并启动轮询；健康探针报告状态，不触发策略初始化。缓存只在进程首次读取权威策略时应用，修改标记为待重启，不在 execution 间切换；不可用策略使栅格准入暂停。

资源建议结合有效 CPU、CPU quota/cpuset 和内存限额，显示采样时间与推导依据，由管理员手动采用并保存；采样失败不伪造建议。GDAL 缓存不等于 RSS 或进程总内存上限，建议也不保证所有算子内存安全；当前不随负载自动调节容量。

## 六、验收入口与有效结论

### 6.1 标准入口

变更前按 owner 和共享消费者确认门禁，使用根 Makefile 及已登记 CI。已有自动发现覆盖时不另建入口；共享执行核心变化须按规范扩散到其他 Runtime。

| 入口 | 覆盖 |
| --- | --- |
| `make test-changed` | 当前工作区受影响 owner 的 T0–T3，缺少 T2 条件时预检失败关闭。 |
| `make test-geopython-workflow` | 真实 GDAL 像元、有效性、网格、COG、内部对象、异步 API、清理及既有工作流回归；Platform CI 的 GeoPython Job 使用同一入口。 |
| `make test-raster-online-runner` | 场景编排、严格成果校验、自动扫描／血缘、浏览器及清理的确定性回归。 |
| `make test-python-ci-registration` | Python owner 和 CI 登记一致性。 |
| `make test-module MODULE=develop` | Develop 授权、Adapter、稳定输出及相应 T0–T3。 |
| `make test-meta-postgres` | Meta 自动扫描身份与固定切片资源叶子的真实 PostgreSQL 回归。 |
| `make test-system-iam-postgres` | System 资源策略、额度、版本及管理员身份的 PostgreSQL 回归。 |
| `make test-common-python` | 公共执行与准入核心；改动时同时验证受影响 Runtime。 |
| `make test-system-frontend` / `make test-console-frontend` | 配置入口、权限、引擎层级及页面交互。 |
| `make test-common-frontend` / `make test-develop-frontend` | 共享资源选择与批量构图；跨引擎／搜索勾选同步、顺序、取消、整批撤销／重做、保存重载及窄屏操作。 |
| `make test-online ONLINE_SUITE=raster-workflow-capacity` | 完整四波段受限容量矩阵；仅手工 GitHub Hosted 独占部署，不计为原 Raster T4 或默认配置安全保证。 |
| `make test-online ONLINE_SUITE=raster-contour-persistence` | 等值线 NFS 单文件 GeoPackage 保存、精确自动扫描、普通发起人、严格物理／Meta 大小、资源血缘及清理的独立手工 Hosted T4；600 秒业务预算。 |
| `make test-online ONLINE_SUITE=raster-polygonize-persistence` | 分类栅格四／八邻接及重分类接转面，NFS 单文件 GeoPackage 保存、完整区域与像元、严格物理／Meta 大小、自动扫描、同用户与当前执行血缘及清理的独立手工 Hosted T4；600 秒预算。 |
| `make test-online ONLINE_SUITE=raster-focal-statistics-persistence` | 邻域统计六条 COG 保存链（3×3 sum/min、5×5 mean/max/stddev、重分类接 3×3 stddev）及非法波段／窗口失败清理；跨块完整像元／mask、严格三方大小、自动扫描、同用户／当前执行血缘和零残留的独立手工 Hosted，600 秒业务预算；尚未实际验收。 |
| `make test-online ONLINE_SUITE=raster-zonal-statistics-persistence` | 同网格多波段、重分类、超过预览上限及空表的四条 CSV 保存链，非法波段／网格失败清理、严格物理／Meta 大小、精确自动扫描、同用户／当前执行血缘与零残留的独立手工 Hosted；600 秒预算。 |
| `make test-online ONLINE_SUITE=vector-rasterize-persistence` | 已登记 GeoJSON／参考 TIFF 的三条 COG 保存链及非法参数失败清理；严格大小、像元／mask、精确自动扫描、同用户／当前执行血缘与零残留的独立手工 Hosted T4；600 秒预算。 |
| `make test-online ONLINE_SUITE=raster-workflow` | 原 Raster Hosted T4；仅在 GitHub Hosted 独占部署运行，不接管个人开发环境。 |

最小日常计算验证使用 `make test-geopython-workflow`；修改跨模块成果、授权或扫描链路时补充相应 owner 门禁与原 Hosted T4。连接本地 Infra 前先执行 `bash scripts/infra/status.sh`，PostgreSQL 仅使用规范允许的测试库。服务重启由用户按标准生命周期操作。

### 6.2 DEM 验收边界

DEM 基础四项已有 Native 和原 Raster Hosted T4 证据，最终版本与归档见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。坡度、坡向和晕渲在 Hosted 中覆盖 COG 保存、自动扫描、资源血缘与页面参数；等值线在原 Hosted 中覆盖计算及 JSON 预览，另由独立 `raster-contour-persistence` 在 `54acffb1c` 完成两种高程模式的 NFS 单文件 GeoPackage 保存、精确自动扫描、普通发起人归属、严格物理／Meta 大小、Monitor、当前执行血缘和零残留清理。Runtime 沿现有挂载路径写入，Meta 经真实 NFS 读取；GeoPackage 内部图层遵守容器边界。该 API 闭环不替代 Manager 绘制、HTTP Range、业务删除、覆盖冲突或容量验收；原 Raster T4 保留原版本和 1740 秒预算。

### 6.3 分类栅格转面验收边界

`raster_polygonize` 已在 `6cd2bb7b0` 的独立 `raster-polygonize-persistence` 完成四邻接、八邻接和重分类接转面三条真实 DAG。完整像元与连通区域、Int64 分类字段、二维 MultiPolygon、源 CRS、严格物理／Meta 大小、原普通用户三个自动扫描、Monitor 与各自当前执行血缘、源及先前成果不变和零残留均通过，最终证据见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。原 Raster T4 与等值线独立基线保持；该结论不替代矢量栅格化、Manager 绘制、业务删除、覆盖冲突或容量验收。

### 6.4 矢量栅格化验收边界

`vector_rasterize` 已在 `764bf53a9` 的独立 `vector-rasterize-persistence` 完成属性／中心覆盖、常量／所有触及像元及分类转面再栅格化三条 COG 保存 DAG。完整像元、mask、NaN NoData、Float64、旋转网格、COG 布局和 artifact／物理／Meta 严格大小、三个精确自动扫描、原普通用户归属、Monitor、当前执行所有输入的血缘、源及先前成果不变、非法字段失败清理和零残留均通过，最终证据见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。Runtime 同路径挂载写入、Meta 经真实 NFS 读取；独立 600 秒预算。原 Raster T4、等值线与分类转面套件保持；本结论不包含点线栅格化、自动范围、插值、完整 Int64 分类码往返、Manager 绘制、业务删除、覆盖冲突或容量验收。

### 6.5 栅格分区统计验收边界

`raster_zonal_statistics` 已在 `5e49199e8` 的独立 `raster-zonal-statistics-persistence` 完成显式多波段分类分区、重分类接统计、超过预览上限及全无效分区四条 CSV 保存 DAG。完整分类值、计数、统计值、null、固定列结构及有限表预览，严格物理／Meta 大小，四个精确自动扫描、原普通用户归属、Monitor、当前执行两源血缘、源及先前成果不变、非法波段／网格失败清理与零残留均通过；最终证据见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。沿用通用 `save`，不新增 RasterArtifact；Runtime 同路径挂载写、Meta 经真实 NFS 读取，独立 600 秒预算。原四套栅格相关 T4 保持；本结论不含矢量分区、分位数、面积加权、自动对齐、Manager 绘制、业务删除、覆盖冲突或容量验收。
