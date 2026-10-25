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

加载支持自包含 JPEG2000（JP2/J2K，内部定位）、TIFF、PNG、JPEG 和 ERDAS Imagine HFA IMG（不含 spill、dependent file、RRD 引用列表），声明格式必须与实际 GDAL driver 一致；拒绝外部 sidecar 和伪装成 TIFF 的 VRT。缺少 CRS 时可显式补充 `source_crs`，不能覆盖冲突的已有 CRS。空间操作要求可信 CRS 和 geotransform；按尺寸重采样等像素操作允许缺失定位，并保留缺失事实。

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
| `raster_focal_statistics` | raster、必填数据波段、3／5 像元窗口、sum/mean/min/max/stddev/mode | 首期已实现；同网格单波段 Float64，中心无效保留 NaN，忽略无效邻居，外缘只计实际像元；mode 分类众数扩展已实现并经独立 Hosted 验证，最高频并列优先中心原分类、否则取较小分类；Native 与真实跨模块验收分别记账。 |
| `raster_vector_zonal_statistics` | raster、同 CRS 有效二维面、必填数据波段，可选覆盖规则及列前缀 | 首期已实现；每面独立，重叠分别计入，保留原面／属性／顺序并追加计数与五项统计，复用矢量保存；Native 与真实跨模块验收分别记账。 |
| `raster_proximity` | raster、必填不超过 32 位的整数数据波段、显式目标分类值 | 首期已实现；投影北向方形网格，最近有效目标像元中心的米制 Float64 欧氏距离；源无效位置保留 NoData，不作为障碍，无有效目标失败；Native 与真实链路分开记账。 |
| `raster_recommend_band_combination` | raster、显式 3–16 个候选数据波段 | 首期已实现并通过独立 Hosted；全量共同有效像元的 OIF 三波段组合排名及推荐 object，原始存储值，不自动 RGB 或宣称专题最优。 |
| `raster_nibble` | raster、显式分类数据波段、目标分类列表 | 首期已实现并通过独立 Hosted；全图最近有效非目标供体替换，等距按源行列，保持无效覆盖；限投影北向方形网格，沿既有 COG。 |
| `raster_boundary_clean` | raster、显式分类数据波段 | 原分类有效总数优先、并列较小分类；3×3 八邻域单轮扩张／收缩，同网格 Float64／NaN，保持无效覆盖；沿既有 COG。Native 与真实平台验收分别记账。 |
| `raster_thin` | raster、显式分类波段与单个前景分类 | Guo–Hall 八邻域两子迭代全图同步收敛；同网格 Float64 骨架1／背景0／原无效NaN；复用 COG，Native／Linux／Hosted 分别记账。 |
| `raster_histogram` | raster、band、bins、可选范围 | 按块全量计数，返回 edges/counts、有效/无效和范围外计数。 |
| `raster_slope` | raster、必填高程单位、band、degree/percent | 同网格单波段坡度，默认角度。 |
| `raster_aspect` | raster、必填高程单位、band | 投影网格北起顺时针坡向；平坦区域为 NaN。 |
| `raster_hillshade` | raster、必填高程单位、band、方位角／高度角 | 同网格地形晕渲，常规有效灰度 1–255；不表示三维场景。 |
| `raster_contour` | raster、必填高程单位、band、等高距或指定高程 | 源 CRS 的二维 LineString GeoDataFrame，`elevation` 为米；可接既有矢量保存节点。 |
| `raster_snap_pour_points` | 未加权累积计数、同网格同有效覆盖的唯一正编号标记、两个显式波段及米制半径 | 圆内最大计数、再按距离／行／列决胜；保留编号、碰撞失败，嵌套仍由汇水区检查。Native／Linux／Hosted分别记账。 |
| `raster_stream_to_vector_d8` | 既有D8方向、同网格／同有效覆盖0／1河网、两个显式数据波段 | 源CRS二维LineString与既有河段ID／零基起止行列／米制中心间长度；上游补到汇合中心，孤立单像元失败，终点单像元只作端点，复用矢量save。Native／Linux／Hosted分别记账。 |

除 `raster_to_cog` 外，上表算子只支持 workflow。波段表达式允许 `b1/b2/...`、常量、加减乘除、有限常数幂及 `abs/sqrt/log/minimum/maximum`，拒绝属性访问、索引、导入和任意 Python 执行。

关键语义如下，完整参数以正式契约和算子元数据为准：

- 多来源计算先显式对齐、组合，再进行表达式计算；引用波段任一无效时结果无效，不隐式跳过缺值后归一化权重。
- 局部更新只用更新源的有效像元逐数据波段覆盖基底，无效更新保留基底；不同网格先对齐，多来源顺序串联，后一次有效更新优先。
- 重分类默认 `unmatched=nodata`，可选 `keep`；规则为精确值或互不重叠的左闭右开区间，源无效像元始终无效。
- 聚合按整数列/行倍数从左上角分组，忽略无效值，全无效组写 NaN，保留不足整组的边缘；有定位时按倍数缩放仿射列、行向量。
- 区域内裁剪要求与栅格实际覆盖面存在正面积交集；区域外裁剪保留完整源网格，将边界内设为无效，孔洞保留，不按剩余有效值缩小范围。
- 有效像元轮廓默认取所选数据波段的并集，可选交集；保留孔洞、分离面片、旋转和源 CRS，输出既有 GeoDataFrame，四角 extent 不能替代轮廓。
- DEM 四项要求二维投影北向无旋转网格和明确的米／国际英尺／美制测量英尺高程单位；水平单位来自 CRS。邻域、边缘、平坦坡向和空等值线等规则以[工作流栅格契约](workflow/栅格算子契约.md#栅格计算契约)为准，不依赖 GDAL 默认值推断。

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
| DEM 填洼 | raster_load → raster_fill_depressions → raster_save；八邻接、图边／无效区边界开放，输出米制 |
| DEM D8 流向 | raster_load → 可选显式 raster_fill_depressions → raster_flow_direction_d8 → raster_save；不改高程、平坦最短路无环、封闭洼地失败 |
| D8 汇流累积 | raster_load → 可选显式填洼／D8流向 → raster_flow_accumulation_d8 → raster_save；count不计自身，area计入自身、统一平方米；非法方向／循环失败 |
| D8 加权汇流累积 | 显式D8方向＋同网格同覆盖权重 → raster_flow_accumulation_weighted_d8 → raster_save；不计自身，权重有限非负，缺失权重／非法路线失败 |
| D8 下游流长 | raster_load → 可选显式填洼／D8流向 → raster_flow_length_d8 → raster_save；沿给定中心间路线，统一米制，开放终点0，非法方向／循环失败 |
| D8 Strahler 河流分级 | 显式D8流向＋同网格0／1河网掩膜 → raster_stream_order_d8 → raster_save；最高同级入流至少两条才升级，断流失败不补线 |
| D8 指定汇水点汇水区 | 显式D8方向＋同网格同覆盖的唯一正编号标记 → raster_watershed_d8 → raster_save；保留用户编号、包含点自身，嵌套点失败，未选范围有效0 |
| D8 上游最长流长 | raster_load → 可选显式填洼／D8流向 → raster_flow_length_upstream_d8 → raster_save；汇合取最长中心间路径，统一米制，无入流0，非法方向／循环失败 |
| D8 汇水分区 | raster_load → 可选显式填洼／D8流向 → raster_drainage_basins_d8 → raster_save；每个开放终点独立分区，按出口行列顺序从1编号，非法方向／循环失败 |
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

### 5.1 距离计算的临时磁盘保护

分类蚕食与邻近距离复用唯一距离核心，在创建距离／供体中间文件前检查当前 execution 工作目录所在文件系统的可用空间。分类蚕食无目标命中时只估算输出，不预留未执行的距离变换；无供体及非法分类仍按原计算契约失败。工作影像在创建前另行估算，含上述计算的 execution 后续调用既有栅格保存编码时再次检查；不提前为未知尺寸的整个 DAG 或远端发布目标作容量承诺。

单进程按文件系统共享空间账本，检查与登记在同一互斥区完成。距离私有文件的同时存活逻辑上界为每像元 32 字节；账本扣除其他活动阶段尚未落盘的承诺，已分配的私有文件块不再重复扣减。读取已分配块数后再读取空闲空间，私有文件删除与检查共用互斥区，避免并发增长／删除造成空间重复计算。工作影像及编码使用实际尺寸、数据类型、瓦片补齐、mask、已有／自动 overview、压缩膨胀和 COG 编码中间副本的保守估算；这些阶段不抵扣正在写入的文件，以免误认可回收空间。

受保护编码在当前线程内将 GDAL `CPL_TMPDIR` 固定到执行工作目录，结束或异常时恢复上层设置，不改进程全局配置。这样 COG 的临时金字塔与工作副本也落在已检查的文件系统；不接受外部临时目录将本阶段写入转移到另一文件系统。[GDAL 3.8.4 COG 的目录选择实现](https://github.com/OSGeo/gdal/blob/v3.8.4/frmts/gtiff/cogdriver.cpp#L67)是这一约束的依据。

每个文件系统统一保留 **2 GiB** 安全余量，不按任务重复保留。空间不足或无法读取容量／活动私有文件的分配事实时，当前工作流明确失败；空间不足的错误给出阶段及所需／可用／待写／安全余量字节数，容量不可读时明确报告未知，不暴露宿主路径。不增加磁盘等待队列、自动重试、System 配置、租户配额或新的公开 API。正常结束与异常退出均释放阶段登记，沿原执行生命周期清理文件；已确认发布的此前成果仍按原契约保留。

新增容量错误按提交请求既有 `Accept-Language` 选择中文／英文，默认中文；在执行线程内绑定该语言，后续状态查询不会改写已记录的失败文本。沿用既有 `EXECUTION_FAILED`，不新增错误码或响应字段。

该保护仅协调同一 Runtime 进程的受保护阶段，不是文件系统配额、实际预分配或跨进程锁；其他进程、其他算子和外部写入仍可能消耗空间，不能据预检宣称整个工作流不会遭遇磁盘写入失败。固定版本容量验收与新增保护的验证分别记账。

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
| `make test-online ONLINE_SUITE=raster-proximity-persistence` | 邻近距离米制多波段及英尺制 UInt32 两条 COG 保存链；完整像元／mask、米制单位、源网格／CRS、COG、严格三方大小、精确扫描、同用户／当前执行输入血缘及失败清理；独立手工 Hosted，600 秒预算。首个 `7c2d47ed5` 基线及 `e712b3b13` 共享核心完整 Hosted 回归均通过，见本节验收边界。 |
| `make test-online ONLINE_SUITE=vector-zonal-statistics-persistence` | 矢量分区中心覆盖、全部触及／自定义前缀、重分类→转面→统计三条 GeoPackage 保存链；完整几何、属性、行序、字段类型、计数／统计／null，严格物理／Meta 大小、精确扫描、同用户／当前执行全部输入血缘和失败清理；独立手工 Hosted，600 秒预算。固定 `9b2830a39` 的完整 Hosted 已通过，见本节验收边界。 |
| `make test-online ONLINE_SUITE=raster-band-recommendation-workflow` | 只读 OIF 三波段组合推荐的完整 object、共同有效域、560 个组合、无界／并列、候选顺序、非法输入和精确源身份；仅源扫描，无成果文件／派生血缘；独立手工 Hosted、600 秒预算，固定 `e712b3b13` 已通过完整验收及归档审计。 |
| `make test-online ONLINE_SUITE=raster-nibble-persistence` | 直接蚕食、重分类接蚕食及无目标命中的三条 COG 链；独立最近原供体 oracle、完整精确分类／mask、严格三方大小、扫描、同用户／当前执行血缘、非法输入失败与零残留；独立手工 Hosted、600 秒预算，固定 `a7d627e5a` 已通过完整验收及归档审计。 |
| `make test-online ONLINE_SUITE=raster-boundary-clean-persistence` | 直接清理、重分类接清理及全无效三条 COG 链；独立全图 Counter／两阶段完整像元与 mask、旋转跨块、安全整数并列分类、严格三方大小、精确扫描、当前执行血缘及非法输入零发布／零扫描／零新增血缘；独立手工 Hosted，600 秒预算，固定 `1b5a26bc1` 已通过 API 保存链及归档复核，见 6.12 节。 |
| `make test-online ONLINE_SUITE=raster-focal-mode-persistence` | 分类众数3×3、5×5及重分类接3×3三条COG链；独立Counter核对完整精确整数像元／mask、旋转跨块、COG、严格三方大小、扫描、同用户／当前执行血缘及非法波段／小数／越界分类失败清理；独立手工 Hosted，600 秒业务预算，固定 `a7d627e5a` 已通过完整验收及归档审计。 |
| `make test-online ONLINE_SUITE=raster-focal-statistics-persistence` | 邻域统计六条 COG 保存链（3×3 sum/min、5×5 mean/max/stddev、重分类接 3×3 stddev）及非法波段／窗口失败清理；跨块完整像元／mask、严格三方大小、自动扫描、同用户／当前执行血缘和零残留的独立手工 Hosted，600 秒业务预算；固定版本 `fe0d7cc8e` 的实际结果见下节。 |
| `make test-online ONLINE_SUITE=raster-zonal-statistics-persistence` | 同网格多波段、重分类、超过预览上限及空表的四条 CSV 保存链，非法波段／网格失败清理、严格物理／Meta 大小、精确自动扫描、同用户／当前执行血缘与零残留的独立手工 Hosted；600 秒预算。 |
| `make test-online ONLINE_SUITE=vector-rasterize-persistence` | 已登记 GeoJSON／参考 TIFF 的三条 COG 保存链及非法参数失败清理；严格大小、像元／mask、精确自动扫描、同用户／当前执行血缘与零残留的独立手工 Hosted T4；600 秒预算。 |
| `make test-online ONLINE_SUITE=raster-workflow` | 原 Raster Hosted T4；仅在 GitHub Hosted 独占部署运行，不接管个人开发环境。 |

最小日常计算验证使用 `make test-geopython-workflow`；修改跨模块成果、授权或扫描链路时补充相应 owner 门禁与原 Hosted T4。连接本地 Infra 前先执行 `bash scripts/infra/status.sh`，PostgreSQL 仅使用规范允许的测试库。服务重启由用户按标准生命周期操作。

### 6.2 DEM 验收边界

DEM 填洼首期已实现并经本地 Native 验证，范围和证据见[后续专题](../next/栅格算子体系后续专题.md)。独立 Hosted 已在固定 `8599f94ed` 通过严格归档审计，Linux 全量仍等 CI 会话回传，不沿用下述原 DEM 基础四项的 T4 作为填洼验收。D8流向首期已在固定3944846c9通过独立Hosted API持久化及领域归档终审；D8无权汇流累积首期已在固定4fba03313通过独立Hosted及精确同源码Linux GeoPython全量2360项。各自严格范围、原始证据与未保留TIFF的离线复核边界见专题；不沿用其他算子证据，不外推UI、容量、后续源码或整个平台。 D8下游流长已在固定dd04fb063通过独立Hosted API持久化及领域归档终审，六条COG链与十四条中英文准确失败、米制中心间路径及开放终点零规则见专题；Linux待CI回传，不沿用其他算子证据，不外推离线重算、UI或容量。 D8汇水分区首期已在固定6bf471fc2通过四条COG链／十四条准确双语失败的独立Hosted API持久化及领域终审，按开放终点行列顺序从1编号，完整像元／唯一归属／分区守恒、严格大小／扫描血缘／清理证据见专题；Linux全量继续等CI会话回传，不沿用其他算子证据，不计UI或容量。 D8上游最长流长首期已在固定ddd32f953（含夹具修复2d6d92780）通过六条COG链／十四条准确双语失败的独立Hosted API持久化及领域终审，汇合取最长、源头零、开放终点接收上游及米制步长／严格大小／扫描血缘／清理证据见专题；保持600秒预算，Linux全量继续等CI回传，不沿用其他流长证据，不计UI或容量。

D8 Strahler 河流分级首期已实现并通过本地标准Native全量3076项，双输入0／1掩膜、完整图校验、同最高级汇合升级及断流失败规则见算子契约，固定候选源码、专项与完整证据见[后续专题](../next/栅格算子体系后续专题.md#d8-strahler-河流分级首期)。独立Hosted已在固定 `bde58e038751d932888bd7696046715e4ba21410` 通过六条COG与十六条准确中英文失败，436228毫秒，小于600秒；完整级别／mask、汇合与河网闭包、零背景、COG、三方严格大小、精确扫描、当前执行实际输入血缘／Monitor、源与旧成果不变及全部清理通过，官方归档与原始日志已完成领域终审。Linux全量仍等专门CI会话回传；TIFF未归档，未作离线复算，不计UI、容量、业务DELETE或任意河网认证。

DEM 基础四项已有 Native 和原 Raster Hosted T4 证据，最终版本与归档见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。坡度、坡向和晕渲在 Hosted 中覆盖 COG 保存、自动扫描、资源血缘与页面参数；等值线在原 Hosted 中覆盖计算及 JSON 预览，另由独立 `raster-contour-persistence` 在 `54acffb1c` 完成两种高程模式的 NFS 单文件 GeoPackage 保存、精确自动扫描、普通发起人归属、严格物理／Meta 大小、Monitor、当前执行血缘和零残留清理。Runtime 沿现有挂载路径写入，Meta 经真实 NFS 读取；GeoPackage 内部图层遵守容器边界。该 API 闭环不替代 Manager 绘制、HTTP Range、业务删除、覆盖冲突或容量验收；原 Raster T4 保留原版本和 1740 秒预算。

### 6.3 分类栅格转面验收边界

`raster_polygonize` 已在 `6cd2bb7b0` 的独立 `raster-polygonize-persistence` 完成四邻接、八邻接和重分类接转面三条真实 DAG。完整像元与连通区域、Int64 分类字段、二维 MultiPolygon、源 CRS、严格物理／Meta 大小、原普通用户三个自动扫描、Monitor 与各自当前执行血缘、源及先前成果不变和零残留均通过，最终证据见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。原 Raster T4 与等值线独立基线保持；该结论不替代矢量栅格化、Manager 绘制、业务删除、覆盖冲突或容量验收。

### 6.4 矢量栅格化验收边界

`vector_rasterize` 已在 `764bf53a9` 的独立 `vector-rasterize-persistence` 完成属性／中心覆盖、常量／所有触及像元及分类转面再栅格化三条 COG 保存 DAG。完整像元、mask、NaN NoData、Float64、旋转网格、COG 布局和 artifact／物理／Meta 严格大小、三个精确自动扫描、原普通用户归属、Monitor、当前执行所有输入的血缘、源及先前成果不变、非法字段失败清理和零残留均通过，最终证据见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。Runtime 同路径挂载写入、Meta 经真实 NFS 读取；独立 600 秒预算。原 Raster T4、等值线与分类转面套件保持；本结论不包含点线栅格化、自动范围、插值、完整 Int64 分类码往返、Manager 绘制、业务删除、覆盖冲突或容量验收。

### 6.5 栅格分区统计验收边界

`raster_zonal_statistics` 已在 `5e49199e8` 的独立 `raster-zonal-statistics-persistence` 完成显式多波段分类分区、重分类接统计、超过预览上限及全无效分区四条 CSV 保存 DAG。完整分类值、计数、统计值、null、固定列结构及有限表预览，严格物理／Meta 大小，四个精确自动扫描、原普通用户归属、Monitor、当前执行两源血缘、源及先前成果不变、非法波段／网格失败清理与零残留均通过；最终证据见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。沿用通用 `save`，不新增 RasterArtifact；Runtime 同路径挂载写、Meta 经真实 NFS 读取，独立 600 秒预算。原四套栅格相关 T4 保持；本结论不含矢量分区、分位数、面积加权、自动对齐、Manager 绘制、业务删除、覆盖冲突或容量验收。


### 6.6 栅格邻域统计验收边界

固定版本 `fe0d7cc8e` 的独立 `raster-focal-statistics-persistence` 已完成六条 COG 保存链，覆盖五项统计、两种窗口的代表组合及重分类串联。519×515 多波段旋转输入的完整像元／mask、边缘与跨块、无效中心、Float64／NaN NoData、COG、严格三方相等正整数大小、六个精确自动扫描、同用户／当前执行源输入血缘、Monitor、源及先前成果不可变、非法波段／窗口失败清理与零残留均通过，归档已独立审计。预检及业务 359770 毫秒，小于 600 秒预算；当前最终证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。不认证全部窗口组合的 T4 覆盖、自定义权重、距离半径、分位数、众数、Manager 绘制、NFS 客户端写、业务删除、覆盖冲突或容量；同源 Linux 全量 1251 项及 Platform 通过；最新完整 Native 状态见验收基线；众数扩展另在修复版 `a7d627e5a` 通过独立套件，见 6.9 节。

### 6.7 矢量分区统计验收边界

独立 `vector-zonal-statistics-persistence` 复用 Hosted 一次性部署、NFS、同源产品 Runtime 和普通用户原 12 权限，不修改其他套件、权限或跨模块产品逻辑。已登记 GeoJSON 面与旋转多波段 TIFF 覆盖重叠、孔洞、多部件、网格外及全无效值；前两条 DAG 必须记录两源血缘，重分类→转面→统计仅记录实际 TIFF 输入。非法波段、统计列冲突不得发布文件或新增扫描；保存沿通用 `save`，不新增 RasterArtifact。所有成果、源和先前成果按完整物理 oracle 检查，退出核对工作区、NFS、部署和 Infra 零残留。预算独立 600 秒，不包含矢量重投影、修复、面积加权、分位数、Manager 绘制、覆盖冲突、业务删除或容量认证。固定版本 `9b2830a39` 已通过完整 Hosted 及独立归档审计：三份 GeoPackage 的物理／Meta 大小分别严格相等为 98304／98304／131072 字节，预检及业务 169803 毫秒，Runtime／NFS／部署清理及 Infra 零残留均通过；最终证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。

### 6.8 栅格邻近距离验收边界

邻近距离的首个完整基线为 `7c2d47ed5`；唯一精确距离核心扩展供体身份后，固定 `e712b3b13` 的独立 `raster-proximity-persistence` 已完整复跑原米制多波段与美制测量英尺 UInt32 两条 COG 链。完整像元／mask、米制单位、源网格／CRS、COG、严格 artifact／物理／Meta 大小、两个精确自动扫描、普通发起人和当前执行单源血缘、源及先前成果不变，以及非法波段／无有效目标失败后无发布或新增扫描均通过。两份成果三方大小仍分别为 1025454／2116652 字节，有效像元仍为 265676／267235；预检与业务 130686 毫秒，小于独立 600 秒预算。完整归档经独立审计，Runtime／NFS／部署清理与 Infra 零残留通过；最终证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。本轮为 API 闭环，不计 UI、任意网格、测地／成本距离、路径、覆盖冲突、业务删除或容量保证。

### 6.9 分类众数滤波验收边界

独立 `raster-focal-mode-persistence` 复用既有Hosted生命周期、NFS、同源产品Runtime和普通用户原12权限，覆盖3×3、5×5及重分类接3×3三条COG保存链。五波段旋转跨块输入包括合法零、负分类、±(2^53−1)及相邻大分类、并列优先中心与较小分类、NoData／mask／alpha／非有限值，以及未选择波段中的小数与越界值。完整物理结果使用独立逐窗口Counter与精确数组相等核对，不能用近似比较掩盖整数误差；同时核验严格三方大小、COG及金字塔、精确自动扫描、同用户与当前执行源输入血缘、源及先前成果不变，非法波段／小数分类／越界分类不得发布或新增扫描。独立600秒业务预算，旧各套T4保持。固定 `a7d627e5a` 已通过完整 Hosted 及独立归档审计：三份 artifact／物理／Meta 大小严格相等为 23355／24782／13789 字节，每份 267227 个有效像元；预检与业务 166599 毫秒，Runtime／NFS／部署和 Infra 零残留通过。最终证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)；仅认证 API 保存闭环，不计 UI、全部窗口组合、边界清理或容量。

### 6.10 OIF 推荐只读验收边界

独立 `raster-band-recommendation-workflow` 使用同源产品 Runtime、NFS 和普通用户原 12 权限，独立 600 秒业务预算。跨块旋转多波段与正交样本覆盖完整共同域统计、相关矩阵、560 个组合（有限评分及零方差排除）、候选逆序结果一致，以及零相关无界和并列。真实专业执行返回的完整 object 按独立全量 NumPy／解析 oracle 核对，当前执行源 locator 和参数必须与公开 DAG 快照一致；Monitor 仅核对其安全投影，不要求暴露完整 object。只执行显式源扫描，成功及非法输入失败均不得生成成果文件、派生血缘或新增扫描，核验源物理内容／mask／定位／scale、严格物理／Meta 源大小、源元数据和血缘不变，Runtime／NFS／部署及 Infra 零残留。固定 `e712b3b13` 已通过完整 Hosted 及独立归档审计：16 候选的共同有效像元 267275、无效 10，455 个有限评分与 105 个常量排除完整覆盖 560 个组合，候选逆序结果完全相同；正交样本的无界／并列评分结构正确，五类失败均无发布或新增扫描。预检与业务 32421 毫秒，Runtime／NFS／部署与 Infra 零残留通过。最终证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)；只认证 API 只读建议，不计 UI、影像持久化、RGB 自动应用、专题效果或容量。

### 6.11 分类栅格蚕食验收边界

独立 `raster-nibble-persistence` 复用同源产品 Runtime、NFS、原普通用户 12 权限及既有 COG 保存，独立 600 秒业务预算。519×515 北向方形网格、多波段输入执行直接蚕食、重分类接蚕食、无目标命中等值三条链；原始稀疏供体由全幅整数平方暴力 oracle 按行列顺序裁定，精确核对完整分类／mask、合法零、负值、安全整数边界、等距及跨无效区域，不能用产品 EDT 或近似数组比较验证自身。逐成果验证 COG、网格／CRS、严格 artifact／物理／Meta 相等正整数大小、精确自动扫描、同用户／当前执行源血缘、Monitor、源及先前成果不变；非法波段、越界分类和无供体失败不得发布或新增扫描。固定 `a7d627e5a` 已通过完整 Hosted 及独立归档审计：三份 artifact／物理／Meta 大小严格相等为 27216／27216／13871 字节，每份 265670 个有效像元；预检与业务 160978 毫秒，Runtime／NFS／部署和 Infra 零残留通过。最终证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)；仅认证 API 保存闭环，不含 UI、边界清理、细化、任意网格、业务删除、覆盖冲突或容量保证。

本批 OIF 推荐、分类蚕食与新增三套独立夹具均纳入标准 `make test-geopython-workflow`。固定修复版 `a7d627e5a` 的 Linux 全量实际通过 1652 项（21 条警告，1051.41 秒），无失败或跳过；同源标准 `make test-platform` 通过，完整原始日志已复核。本地 macOS 全量 1652 项（548.33 秒），255 个输入及标准入口前后不变；独立 Ubuntu 24.04 x86_64／NumPy 2.2.6／GDAL 3.8.4 标准专项 233 项（17.91 秒）通过。26 条新回归覆盖 UInt64 精确写入、大整数 NoData 混同及显式 mask，保留原拒绝断言；修复版众数及蚕食 Hosted 已通过严格归档复核，最终固定版本与证据见[验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。既有三个独立 runner、登记一致性和工作流安全检查证据保持；确定性、各套 Hosted、UI 与容量分别记账。

### 6.12 分类栅格边界清理验收边界

独立 `raster-boundary-clean-persistence` 的 [Hosted 39625363775](https://github.com/pampa0629/addp/actions/runs/39625363775/job/119309061524) 在固定源码 `1b5a26bc1a0a18d0e9133a8b4c4544f7faf277dd` 已通过直接清理、重分类接清理和全无效输入三条 COG 保存链。519×515、旋转 EPSG:4326 六波段样本覆盖零、负类别、安全整数边界及相邻大类别、全图计数并列和无效覆盖；现场独立两阶段全幅 oracle 精确校验完整像元／mask、原网格／CRS、COG／金字塔及未继承 scale／offset／色表。三份 artifact／物理／Meta 大小严格相等且为正整数，分别为 31270／13788／10787 字节，有效像元数为 267227／267227／0；精确自动扫描、普通用户与当前父执行、租户／引擎、资源血缘和 Monitor 一致，源及先前成果不变。非法波段、小数和越界类别三次失败均核对真实 Runtime 诊断，零发布、零新增扫描／派生血缘；工作目录、NFS、Runtime、镜像、部署及 Infra 清理通过。预检与业务 194662 毫秒，保持原 12 权限及独立 600 秒预算。

归档回执、固定版本校验源码和原始日志已独立复核；归档未保留 TIFF 原字节，未作离线成果重读。本结论仅认证 API 保存闭环，不计页面／截图、容量、业务 DELETE、覆盖冲突、双向多轮或细化。同版本 Linux 全量／Platform 尚待已审计终态证据，不以本地 Native 或 Hosted 代替。最终链接和报告哈希见[当前验收基线](../next/栅格算子体系后续专题.md#二当前验收基线)。

D8加权汇流累积首期已实现，显式双输入、不计自身、非负有限权重、覆盖完全一致及完整D8路线校验见[算子契约](workflow/栅格算子契约.md)。本地标准 `make test-geopython-workflow` 全量 **3296 passed／21 warnings／710.94秒**，0失败／跳过／筛除，进度计数一致且验证输入无漂移；独立逐源路径核完整像元／mask、全1权重与无权累积一致、出口总量守恒，以及真实COG、双语异步失败和清理。Linux全量继续等CI会话回传，独立Hosted入口及夹具已实现、本地通过，尚未实际运行，不计平台扫描血缘、UI或容量。

独立 `raster-flow-accumulation-weighted-d8-persistence` 已获用户授权：复用Hosted隔离生命周期、NFS、同源默认入口产品Runtime和普通用户原12权限，独立600秒业务预算。六条COG保存链为直接非负权重、全1权重、权重重分类、DEM→D8流向→加权累积、填洼接上述链、全无效；每条都核当前执行两份实际注册输入的血缘。负权重、缺失／非有限权重、网格不一致、非法D8码（含原始UInt64 NoData邻接大整数）、循环、非法方向波段、非法权重波段、累计溢出八类各中英文提交，共16条准确失败。逐源路径oracle核完整权重和／mask、合法零、自身排除、全1等价、出口守恒、单位／网格／CRS、COG／金字塔、artifact／物理／Meta相等正整数大小、精确扫描与父执行／用户／租户／引擎、当前执行两源血缘／Monitor、源及先前成果不变和全部清理。失败不发布、不新增扫描或派生血缘，反向语言查询不改提交错误；不变更原套件／权限／跨模块产品逻辑，不计UI或容量。入口与夹具已实现、本地标准门禁通过，实际Hosted未运行；Linux全量和既有Hosted仍等专门CI会话回传。

D8指定汇水点汇水区首期按已确认范围实施：保留用户唯一正编号，包含点自身的完整上游归属，嵌套点明确失败，未选有效范围零、原无效NaN；完整准入与失败规则见[算子契约](workflow/栅格算子契约.md)，实现及验证状态见[后续专题](../next/栅格算子体系后续专题.md#d8-指定汇水点汇水区首期)。本地标准 `make test-geopython-workflow` 隔离候选全量3553项／21 warnings／735.99秒通过，0失败／跳过／筛除且验证输入无漂移；完整像元／mask独立路径、严格编号／嵌套失败、中英诊断及真实异步COG保存清理已验。Linux与其余Hosted仍等CI会话回传，本算子独立Hosted入口及夹具已实现，实际运行待验，不计平台扫描／血缘、UI或容量。

独立 `raster-watershed-d8-persistence` 已获用户授权：复用Hosted、NFS、同源默认入口产品Runtime、普通用户原12权限，独立600秒业务预算。四条直接指定点／标记重分类／DEM→流向／填洼接流向COG保存链，每条核当前执行两份实际注册输入血缘；嵌套、重复编号、无点、全无效、非法编号（UInt64 NoData相邻大整数）、覆盖不一致、网格不一致、非法方向、循环及方向／标记两个角色非法波段十一类各中英文提交，共22条准确watershed任务失败。515×519完整网格中的有效条带跨512块，包含最大安全编号、中间点、两条支流共用最终开放出口、未选有效零及NoData／mask／alpha覆盖。独立逐源完整路径oracle核全部编号／mask、点自身、原网格／CRS、COG／金字塔及单位清除；严格artifact／物理／Meta相等正整数大小、精确扫描父执行／用户／租户／引擎、当前执行两源血缘／Monitor、源及先前成果不变和全部清理；失败无发布／新增扫描血缘，反向查询不改提交错误。入口与长期夹具已实现，本地门禁通过、实际Hosted未运行；不修改原套件、权限或跨模块产品逻辑，不计UI或容量。Linux及其余Hosted继续等专门CI会话回传，不设定时、不自查或重复触发。

独立夹具标准 `make test-geopython-workflow` 专项 **55 passed／3600 deselected／7.82秒**，0失败／跳过，输入无漂移；真实GDAL四保存链、22条中英准确失败、完整编号／mask与源及旧成果保护、19类物理篡改拒绝通过。专项不计新全量，不与产品原3553项相加。日志SHA-256：`b9969555eb2c6bea5c8c354a103275cbc416fce1ba2de109b431bce15fd8b2cc`。标准 `make test-raster-watershed-d8-online-runner` 的63项脚本回归（unittest报告28.890秒）及完整Online登记通过；`make test-python-ci-registration` 的10项／11模块与前置37项登记回归通过，共享计数不累加，组合日志SHA-256：`2c8f0d45e17a15852b95d5e1e9fa4c31d8b4cea7de75d6b15e123b9c9dbe6945`。实际Node24.11.1下完整 `make test-raster-online-runner` 退出0，22组子门禁及末尾215项共享生命周期模拟／隔离身份Go T1通过，壁钟2672.09秒，输入无漂移；日志SHA-256：`d28506697d40acf7d0c6a26698d1e0071e9711fe02b0c8eeba65f1f71c5fdd03`，不计完整IAM／数据库集成。最终专项与独立脚本绑定隔离候选 `19bb583a21b4ffd5d7ffe1d5099bb57d02d58ab7`，共享总入口绑定较早候选 `3a26d888637ef598e9d8361dbd668622ca710348`。两候选之间仅其他会话矢量拓扑测试／夹具／登记及场景文档发生变化，未修改产品代码或本轮算子／夹具；更新后的共享登记另由最终独立入口复核；Runtime产品代码保持91440f2e1，原Native全量固定证据保留。独立Hosted尚未运行，Linux及其余Hosted继续等专门CI会话回传与领域归档终审；不设定时、不自行轮询或重复触发。

D8河段编号首期按已确认范围实施：显式流向与同网格／同覆盖0／1河网，河源与汇合点开启新段，汇合点归下游段；按起点行列顺序从1编号，完整非法路线或断流严格失败，不补线。输出同网格Float64分类编号，非河网有效零、原无效NaN；复用现有COG保存。完整契约见[算子契约](workflow/栅格算子契约.md)，实现与验证状态见[后续专题](../next/栅格算子体系后续专题.md#d8-河段编号首期)。首期已实现并通过本地标准Native全量（3852项／21 warnings／833.62秒，0失败／跳过／筛除且输入无漂移），完整编号／mask、双语失败、共享Strahler回归及真实异步COG保存清理已验；Linux／Hosted仍等CI会话回传，不计UI或容量。

D8河段编号独立Hosted `raster-stream-link-d8-persistence` 的夹具、标准入口及手工CI登记已实现，按六保存链／十六中英准确失败核验，普通User原12权限及600秒预算保持。完整编号／mask、起点顺序、汇合归属、COG／三方严格大小、精确扫描／全部实际输入当前血缘、源旧成果不变及清理均为必需断言；本地真实GDAL夹具专项50项、独立脚本63项、Python／Online登记与共享总入口通过；专项与原3852项全量分开记账。Linux和实际Hosted待专门CI会话回传，不计UI或容量。

D8河流分级扩展复用 `raster_stream_order_d8`，新增 `method=strahler/shreve`，默认Strahler结果保持。Shreve每个河源贡献1、实际河网汇合求和，输出当前显式河网上游河源量级；统一准入／完整图／断流校验，非河网有效零、原无效NaN，同网格Float64精确整数及COG保存。非法方法、完整路线及超2^53−1失败；无新增算子、依赖、权限或跨模块逻辑。完整契约见[算子契约](workflow/栅格算子契约.md)，本地标准Native全量4136项／21 warnings／1416.45秒通过，0失败／跳过／筛除且输入无漂移；默认及显式Strahler、Shreve完整量级／mask、中英失败和本地异步COG保存清理已验。固定5898d1aa18f9db66ce80b121e0ed2182c0865939已通过正式Linux标准Native全量4136项／21 warnings／1644.76秒，0失败／跳过／筛除、进度点数一致；实际第7步及Job均success，日志hash与版本边界见[专题](../next/栅格算子体系后续专题.md#d8-shreve-河流量级扩展)。上述5898证据不覆盖后续新增夹具；固定883aed7b5已另通过正式Linux全量4189项／21 warnings／1732.13秒，完整日志与版本边界见同一专题，实际Hosted仍待CI回传，不计UI或容量。

### Shreve 独立持久化验收

已批准手工 `raster-stream-shreve-d8-persistence`：沿用隔离Hosted／NFS、同源默认入口Runtime、普通User原12权限和600秒预算。六条Shreve保存链加同源默认／显式Strahler两条对照，共八条COG；断流、非二值掩膜、覆盖／网格不一致、精确64位非法方向、循环、双角色非法波段及非法method九类各中英文，共18条准确失败。独立逐源头前向路径oracle核完整量级／mask、出口源头数守恒、非河网供体排除和对角交叉独立，默认／显式Strahler完整物理成果相等且符合独立原规则；严格核COG／网格／CRS、三方相等正整数大小、精确扫描／全部实际输入当前血缘／Monitor、源与先前成果不变及零残留，失败零发布／新增扫描血缘、反向查询不改提交语言。整数越界继续单列Native证据，不模拟Hosted越界或降低产品上限；不改原套件、权限或跨模块产品逻辑，不计UI或容量。本地新夹具与脚本门禁通过，实际Hosted未运行；固定5898首期Linux全量已通过，固定883aed7b5的Linux全量4189项已通过，实际Hosted仍等专门CI会话回传，本会话不触发、轮询或设置定时。

本独立夹具本地Native专项53项、标准独立runner65项、Online／Python CI登记及共享栅格总入口24组均通过；原产品Native全量4136项固定证据保留，固定883aed7b5正式Linux全量4189项已通过，实际Hosted仍待专门CI会话回传。整数累计越界仅列Native受控上界回归，Hosted精确64位非法方向码不等同量级累计越界；完整固定证据见[栅格专题](../next/栅格算子体系后续专题.md#shreve-独立持久化验收)。

### D8 河网转矢量线首期验收边界

`raster_stream_to_vector_d8` 已实现；唯一河段编号路径生成源CRS二维线，保留每个中心、既有ID、零基起止行列和米制长度。每条实际河网边恰好一次，汇合边归上游；孤立单像元整次失败，单像元汇合终点仅为端点、无零长度线，ID允许间隔。复用既有矢量save／GeoPackage，无新增权限或跨模块逻辑。环境采集修正提交5419a73ad的本地标准Native全量4392项／21 warnings／798.08秒通过，0失败／跳过／筛除，输入无漂移；候选树 `9502d54c6f6b360e74ad4992cd79b6bce82e25aa`，原始日志SHA-256 `6a1ee21d37cf3508e08e3b790ff88b2406e4d1ac2703166df6afbf7f85245503`。含独立完整逐边／几何／字段／单位及真实GeoPackage、异步DAG双语失败和清理，CI登记检查通过；不计Linux正式全量或真实平台扫描／血缘／Monitor、UI、容量。全量期间未新增Python崩溃报告；新增夹具不计入该4392历史全量。产品与独立Hosted夹具已另在固定 `9e4339d5583e0d22b5253e44b716bd6d0d2795ec` 通过正式Linux标准Native全量4527项／21 warnings／1371.94秒，0失败／跳过／筛除、进度点数一致，实际测试步骤及Job均success；完整日志、运行时修复及旧失败覆盖关系见[专题终审](../next/栅格算子体系后续专题.md#geopython-原生进程安全修复与-linux-验收)。真实Hosted仍等专门CI会话回传。完整规则见[工作流栅格契约](workflow/栅格算子契约.md#栅格计算契约)。

### D8 河网转矢量线独立 Hosted 验收

已批准独立手工 `raster-stream-to-vector-d8-persistence`，复用隔离Hosted／NFS、同源默认入口Runtime、普通User原12权限及600秒预算。三种水平单位的直接双输入，加累积阈值、DEM／填洼完整链及空河网／全无效，共八条GeoPackage保存；孤立单像元（含非河网入流）、混合孤立点、断流、非二值掩膜、覆盖／网格不一致、精确64位非法方向、循环及两个角色非法波段十类各中英，共20条准确失败。独立逐边oracle核每条真实河网边恰好一次，完整中心坐标／方向／ID间隔／零基起止行列／米制长度、汇合端点、单像元终点、跨块及交叉独立。按通用矢量save契约核GeoPackage的完整几何／字段类型／CRS、物理与Meta相等正整数大小、精确扫描与当前执行所有实际输入血缘／Monitor、源与先前成果不变及全部清理；空成果核Schema与CRS。失败零发布／新增扫描血缘，反向查询不改提交语言，不新增RasterArtifact。编号／长度与坐标数值极限继续单列Native，不模拟Hosted越界；不改原套件、权限、算法或跨模块产品逻辑，不计UI或容量。夹具、标准入口和手工CI登记已实现并通过本地准备门禁；实际Hosted尚未运行，仍由专门CI会话协调，本会话不触发、轮询或设置定时。

独立Hosted准备的标准Native专项 `make test-geopython-workflow`（`-k stream_to_vector_d8`）**158 passed／4280 deselected／5.52秒**，0失败／跳过，输入无漂移；其中新增夹具46项，涵盖八份真实GDAL GeoPackage、20条双语准确失败、23类物理篡改拒绝及源／先前成果不变。基于修正提交5419a73ad，候选树 `2290b776308c6e0c86940a4a6f0557a48f598906`，原始日志SHA-256 `89cf9b3e1ae6efbe5dbb9c3cc8c0faeb1d4afc409da51eb8ce7f48b6f6f9157e`；筛除不计通过，不把专项与旧全量相加。三种单位直接样本各21条线／444条边／465个顶点，跳过终点ID2；阈值样本34条线／3542条边，DEM为7条／3612条，填洼链为5条／2582条，空河网与全无效均零要素且保留Schema／CRS。

Node24.11.1下 `make test-raster-stream-to-vector-d8-online-runner` **63项／20.403秒**及完整Online登记通过，原始日志SHA-256 `a5a53ce92de30f6d24e240ea662b64d2e84b181b958ff72c832be4d700c4e6fc`。完整 `make test-raster-online-runner` 退出0、输入无漂移，耗时744.59秒，25组Python子门禁及原215项生命周期、隔离身份夹具Go T1通过（共享回归重复，不求和作唯一测试数）；原始日志SHA-256 `b52e1fa5f190351d166207ca1186873a9322c9a1f1184a243412926ae6c2c496`。上述本地记录保持原验证层级；本节夹具已另纳入固定9e4339d55的正式Linux全量，终审证据见专题。真实Hosted用户／扫描／当前执行血缘／Monitor及隔离清理尚待平台执行证明；数值极限继续单列Native，不模拟Hosted越界。实际Hosted仍等专门CI会话协调和完整原始归档领域审计，无自行触发／轮询／定时。

### 汇水点吸附首期验收边界

`raster_snap_pour_points` 已实现：在显式米制圆半径内选最大未加权累积计数，允许跨分水岭，按距离／行列稳定决胜；保留用户唯一编号，碰撞整次失败，嵌套仍由既有汇水区算子拒绝。完整准入与数值规则见[算子契约](workflow/栅格算子契约.md)，实际证据见[后续专题](../next/栅格算子体系后续专题.md#汇水点吸附首期)。本地标准Native全量4786项／21 warnings／1040.36秒通过，0失败／跳过／筛除、输入无漂移；含独立全像元／mask、单位与精确边界、双语失败和真实Runtime异步COG保存清理。Linux与既有Hosted等专门CI会话回传；本算子独立Hosted入口及夹具已实现，实际Hosted未运行；准备证据与边界见[专题](../next/栅格算子体系后续专题.md#汇水点吸附独立-hosted-验收)，不计平台扫描／血缘／Monitor、UI或容量。

### D8 增量子汇水区首期

新增 `raster_subcatchments_d8`，允许嵌套的唯一用户汇水点编号，按下游遇到的第一个指定点划分互不重叠的增量范围，包含点自身；未选有效零、原无效NaN。完整图、网格／有效覆盖及精确编号严格校验，非法路线整次失败；原 `raster_watershed_d8` 的完整上游语义及嵌套拒绝保持。输出沿既有COG保存，无新增权限或跨模块逻辑。完整规则见[算子契约](workflow/栅格算子契约.md)，实现与验证分层记录见[后续专题](../next/栅格算子体系后续专题.md#d8-增量子汇水区首期)。本地标准Native固定候选全量5073项／21 warnings／1536.93秒通过，0失败／跳过／筛除且输入无漂移；已核独立逐路径完整编号／mask、原嵌套拒绝回归、中英失败及真实Runtime异步COG保存清理，原始日志与固定候选树见专题。后续并行测试／登记变更后，本项标准Native专项398项／4688 deselected／25.30秒及更新后的登记入口复核通过，Runtime产品及本项测试未变；专项不计新全量。独立Hosted入口及夹具已实现并通过本地准备门禁；Linux全量及已有Hosted继续等CI会话回传，新算子独立Hosted尚未运行，不计平台扫描／血缘／Monitor、UI或容量。


D8增量子汇水区独立手工Hosted `raster-subcatchments-d8-persistence` 已获授权，按7保存链／30条中英准确失败准备，含非嵌套原完整汇水区等价对照、嵌套原算子拒绝对照及真实吸附接增量。完整物理／COG／严格三方大小／扫描／当前执行实际输入血缘／Monitor／源旧不变与清理为必需断言；普通用户原12权限及600秒预算保持，不改产品逻辑／权限／原套件。夹具、标准入口及手工CI登记已实现；最终Native专项247项（含新增夹具73项、4912筛除）／17.46秒、独立脚本63项和更新后的Online登记通过；Node24.11.1下共享栅格总入口实跑退出0，其后本套件描述与Worker登记收紧经最终专项复核，证据分开记账。实际Hosted尚未运行，由专门CI会话协调，Linux及其余Hosted仍等待回传；范围与证据见[专题](../next/栅格算子体系后续专题.md#d8-增量子汇水区独立-hosted-验收)。

### D8 汇流面积扩展

现有`raster_flow_accumulation_d8`按已确认范围扩展`measure=count/area`，默认count不计自身，area计入自身、统一投影网格平方米（m2），共用完整D8图校验与计数核心。矩形像元及已知线性单位换算、开放终点累入、无效覆盖保留和数值失败规则见[算子契约](workflow/栅格算子契约.md)。不新增算子、权限或跨模块逻辑；已实现并通过本地标准Native全量5264项／21 warnings／1388.75秒，0失败／跳过／筛除且输入无漂移，完整证据与版本边界见[后续专题](../next/栅格算子体系后续专题.md#d8-汇流面积扩展)。Linux继续等CI会话回传；本轮没有新增或运行area独立Hosted，本地异步COG不计平台扫描／血缘、UI或容量，既有count固定证据不作为新模式验收。


### D8 汇流面积独立 Hosted 验收

用户已批准独立手工D8汇流面积持久化验收：复用Hosted隔离生命周期、NFS、同源默认入口产品Runtime及普通用户原12权限，独立600秒业务预算。八条COG保存链为米制／国际英尺／美制测量英尺方向接area、同一米制方向的默认count／显式count对照、DEM→流向→area、填洼接上述链及全无效area。519×515矩形网格、显式第二数据波段，覆盖跨512块、多出口、孔洞、合法零、部分透明、孤立开放终点及边界非零方向。

非法方向码（含原始UInt64 NoData邻接大整数）、内部伪终点、越界指向、无效下游、循环、非法波段、旋转网格与非法measure八类各中英文提交，共16条准确失败；反向查询不改提交语言。独立逐源路径核完整像元／mask、area包含自身、平方米m2、count不计自身且清空单位、两count对照一致与出口面积守恒；严格核原网格／CRS、Float64／NaN、COG及最近邻金字塔、artifact／物理／Meta相等正整数大小、精确扫描的父执行／用户／租户／引擎、当前执行实际注册输入血缘／Monitor、源与先前成果不变。失败零发布／零新增扫描和派生血缘，全部退出清理和零残留为必需断言。面积溢出／下溢、累计溢出与精确计数上限继续单列Native数值极限证据，不记Hosted已验。

标准业务入口`make test-online ONLINE_SUITE=raster-flow-accumulation-area-d8-persistence`；本地准备门禁`make test-raster-flow-accumulation-area-d8-online-runner`；仅手工Hosted，不参加夜间自动运行，不改原套件、产品逻辑、权限或跨模块实现，不计UI或容量。夹具、标准入口和独立手工CI登记已实现；最终本地Native专项184项（含新增夹具52项、5217项筛除）／18.65秒，独立脚本门禁63项／17.992秒与完整Online登记通过；Node24.11.1下共享栅格脚本总入口实跑通过，具体候选版本与证据见后续专题。实际Hosted未运行，由专门CI会话协调；Linux全量和其余Hosted继续等回传，不计平台扫描／血缘／Monitor、UI或容量通过。数值极限继续单列Native。


### D8 到河网沿流距离首期

用户已确认：显式同网格／同有效覆盖D8方向和二值河网，河网自身0，沿既有路线到首次河网中心，未到达河网为NoData；非法完整路线及河网断流明确失败，不补线。仅已知线性单位二维投影北向无旋转，允许矩形像元，横纵及对角中心间步长统一米制；输出同网格／CRS Float64／NaN，复用现有COG。完整输入、数值及清理规则见[算子契约](workflow/栅格算子契约.md)。本轮仅产品实现和现有Native入口回归，不新增权限、依赖、跨模块逻辑或独立Hosted脚本；Linux全量及Hosted继续等CI会话回传，不触发、轮询或设置定时。已实现并通过本地标准Native全量5605项／21 warnings／1187.13秒，0失败／跳过／筛除，输入无漂移；独立逐源路径oracle、首次命中／未到达、完整像元／mask、米与两种英尺、十万像元长路径、完整COG／金字塔、双语失败和异步组合保存／清理均已覆盖。既有Native recipe、自动算子发现及Platform GeoPython Job覆盖本轮产品变更，无新增测试入口；完整固定候选与日志证据见[后续专题](../next/栅格算子体系后续专题.md#d8-到河网沿流距离首期)。Linux／Hosted未取得本轮新证据，不计平台扫描／血缘、UI或容量。


### D8 到河网沿流距离独立 Hosted 验收

用户已批准独立手工D8到河网沿流距离持久化验收，复用Hosted／NFS、同源默认入口产品Runtime、普通User原12权限，独立600秒预算。米／国际英尺／美制测量英尺方向＋河网、累积阈值、DEM、填洼、空河网及全无效共8条COG保存链；519×515矩形网格与显式第二波段覆盖跨512块、多出口、对角交叉、孔洞及alpha／mask。独立逐源路径核首次命中、河网自身零、未到达NoData及输出有效覆盖，严格核米制单位、完整COG／最近邻金字塔、三方相等正整数大小、精确扫描、当前执行实际输入血缘／Monitor、源旧不变及全部清理。

非法码（含原始UInt64 NoData邻接非法整数）、内部伪终点、图外／无效下游、循环、断流、非二值掩膜、覆盖／网格不一致、旋转网格及两个角色非法波段共12类各中英文，合计24条准确失败；包含河网之外或命中之后的非法路线。失败零发布／新增扫描／派生血缘，反向查询不改提交语言。长路径稳定性、距离溢出及极小步长继续单列Native数值证据，不记Hosted已验。不改产品、权限、其他模块或原套件，不计UI／容量。

夹具、脚本、标准入口及独立手工CI登记已实现；最终本地标准Native专项260项（含新增夹具57项、5434筛除）／10.55秒及独立脚本63项／23.327秒通过，完整Online登记通过；Node24.11.1下共享栅格脚本总入口实跑通过，具体候选与日志见后续专题。产品算子源码保持47a390210。本轮未运行实际Hosted，Linux全量和其他Hosted继续等专门CI会话回传，不计平台扫描／血缘／Monitor、UI或容量；数值极限仍单列Native，不触发、轮询或设置定时。

### D8 到河网相对高程（HAND）首期

用户已确认三个显式同网格／同有效覆盖输入：D8方向、二值河网和DEM。全图下游高程不升高、允许平坦，逆坡明确失败；相对下游首次河网高程差统一米制，河网及等高非河网为合法零，未到达及原无效为NoData。完整准入、单位及数值边界以[算子契约](workflow/栅格算子契约.md)为准。已实现并通过本地标准`make test-geopython-workflow`全量 **6007 passed／21 warnings／928.48秒**，0失败／跳过／筛除，JUnit及通过进度点数一致，输入指纹无漂移。新增HAND相关313项包含于全量，不与专项相加；Linux全量和Hosted等待CI会话回传，本会话不手动触发、轮询或设置定时，不计平台扫描／血缘、UI或容量。


### D8 到河网相对高程（HAND）独立 Hosted 验收

已获用户批准：独立手工raster-hand-d8-persistence，复用Hosted／NFS、同源默认入口Runtime和普通用户原12权限，独立600秒预算。三种高程单位与水平CRS交叉的直接HAND、累积阈值、DEM、填洼、空河网及全无效共8条COG保存链，三输入显式数据波段、519×515矩形网格，覆盖跨512块、多出口、孔洞、平坦零、负高程与alpha／mask。独立逐源路径oracle核首次河网基准／完整高程差及mask、未到达NoData；严格核完整COG／最近邻金字塔、网格／CRS／米制、三方相等正整数大小、精确扫描和当前执行全部实际输入血缘／Monitor、源及先前成果不变与全部清理。

命中前／命中后／空河网逆坡及填洼流向混用原DEM，非法码／内部伪终点／图外与无效指向／循环／断流，非二值河网、两角色覆盖与网格不一致、旋转网格，三个角色非法波段、非法高程单位及单位冲突共21类各按中英文提交，合计42条准确失败。失败零发布／新增扫描／派生血缘，反向查询不改提交语言。十万像元长路径、原始64位高程相邻差值及溢出／下溢继续单列Native，不计Hosted数值极限、UI或容量，不修改产品、原套件、权限或跨模块逻辑。

夹具、标准入口与独立手工CI登记已实现，通过本地Native专项389项（含新夹具76项）、独立脚本63项及登记检查；实际Hosted尚未运行，Linux全量与其余Hosted继续等专门CI会话回传。本会话不手动触发、轮询或设置定时。专项不计新全量，不与原6007项全量或共享测试相加；不计真实普通用户、平台扫描／血缘／Monitor、UI或容量。

### DEM 平均曲率首期

已实现`raster_curvature(input_raster, band, elevation_unit, method='mean')`：默认mean保留原平均曲率结果；固定完整有效3×3九点等权二次曲面拟合，保留自由常数项，以曲面平均曲率输出凸正凹负，单位`1/metre`。显式数据波段与三种高程单位，水平单位独立换算；复用DEM准入、原网格／CRS和既有COG。边缘及任一无效邻域NoData，无完整邻域成功全NoData；米制步长不可表示或高程差／导数／结果非有限明确失败。原生64位整数先作精确相对差值，唯一halo路径单次读取不超过512×512；不新增权限、依赖或跨模块逻辑。完整定义见[算子契约](workflow/栅格算子契约.md)。

本地标准`make test-geopython-workflow`全量 **6271 passed／21 warnings／5353.66秒**，0失败／跳过／筛除；含151项平均曲率用例，独立最小二乘／解析曲面、完整像元mask、单位／原生64位、跨块COG及真实异步保存／双语失败／清理均覆盖。固定候选树`b5aab7222f5ce61f83fe60d47101d94befae8284`，原始日志SHA-256：`b1ebc6193e7b96f9aa004b36d967564649c6f6253a3e6b6ad078089aae9e1ca4`。Native输入无漂移、无新增Python原生崩溃报告；本地异步接口不计真实平台Hosted、扫描血缘、普通用户、UI或容量。现有自动发现与Native／CI登记覆盖，无新入口。后继固定`3fe8aa81c`的原平均曲率及独立保存夹具已通过正式Linux Native全量6394项，完整日志审计见专题；其独立Hosted入口与夹具已准备，实际运行仍由CI会话协调。该旧固定证据不外推为下述剖面扩展的Linux或Hosted证明。


### DEM 剖面曲率扩展

既有`raster_curvature`增加`method=profile`，与默认mean共用完整有效3×3九点等权六系数二次曲面拟合和唯一带halo分块／COG路径。profile为沿最大坡降方向的竖直剖面曲率，凸正凹负、单位`1/metre`；九个有效原始高程严格全相等的水平平地为0，其他拟合梯度同时为零的点为NaN NoData，包括山顶／洼底／鞍点及非平地残差。不设置近零阈值，极小非零坡度继续计算；没有可计算点仍成功全NoData。原mean公式与结果保持，方向未定义的NoData不掩盖非有限数值失败。

显式波段及三种高程单位、水平单位独立换算、北向无旋转矩形投影网格、原始64位差值、完整九点有效性、原网格／CRS及源旧保护规则保持。仅新增method枚举与双语诊断，不新增算子、权限、依赖或跨模块逻辑。独立最小二乘／基本形式方向商、解析曲面及Decimal极值验证沿既有Native入口自动发现；本地结果见[专题](../next/栅格算子体系后续专题.md)。Linux全量和Hosted分别等CI会话回传；本轮没有新增或运行剖面独立Hosted，不计真实平台扫描／血缘／Monitor、UI或容量。
