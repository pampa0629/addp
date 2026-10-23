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

加载支持自包含 TIFF、PNG、JPEG 和 ERDAS Imagine HFA IMG（不含 spill、dependent file、RRD 引用列表），声明格式必须与实际 GDAL driver 一致；拒绝外部 sidecar 和伪装成 TIFF 的 VRT。缺少 CRS 时可显式补充 `source_crs`，不能覆盖冲突的已有 CRS。空间操作要求可信 CRS 和 geotransform；按尺寸重采样等像素操作允许缺失定位，并保留缺失事实。

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
| D8 汇流累积 | raster_load → 可选显式填洼／D8流向 → raster_flow_accumulation_d8 → raster_save；沿给定方向、不计自身，非法方向／循环失败 |
| D8 加权汇流累积 | 显式D8方向＋同网格同覆盖权重 → raster_flow_accumulation_weighted_d8 → raster_save；不计自身，权重有限非负，缺失权重／非法路线失败 |
| D8 下游流长 | raster_load → 可选显式填洼／D8流向 → raster_flow_length_d8 → raster_save；沿给定中心间路线，统一米制，开放终点0，非法方向／循环失败 |
| D8 Strahler 河流分级 | 显式D8流向＋同网格0／1河网掩膜 → raster_stream_order_d8 → raster_save；最高同级入流至少两条才升级，断流失败不补线 |
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

DEM 填洼首期已实现并经本地 Native 验证，范围和证据见[后续专题](../next/栅格算子体系后续专题.md)。独立 Hosted 已在固定 `8599f94ed` 通过严格归档审计，Linux 全量仍等 CI 会话回传，不沿用下述原 DEM 基础四项的 T4 作为填洼验收。D8流向首期已在固定3944846c9通过独立Hosted API持久化及领域归档终审；D8无权汇流累积首期已在固定4fba03313通过独立Hosted及精确同源码Linux GeoPython全量2360项。各自严格范围、原始证据与未保留TIFF的离线复核边界见专题；不沿用其他算子证据，不外推UI、容量、后续源码或整个平台。 D8下游流长已在固定dd04fb063通过独立Hosted API持久化及领域归档终审，六条COG链与十四条中英文准确失败、米制中心间路径及开放终点零规则见专题；Linux待CI回传，不沿用其他算子证据，不外推离线重算、UI或容量。 D8汇水分区首期已实现并通过本地标准Native全量，按开放终点行列顺序从1编号，完整图校验及独立逐源路径归属证据见专题；Linux继续等CI会话回传，独立Hosted已获授权并完成入口及本地验证，实际运行／归档终审仍待验，不沿用其他算子证据。 D8上游最长流长首期已实现并通过本地标准Native全量，汇合取最长、无入流0及米制中心间步长证据见专题；Linux等CI回传，独立Hosted入口、夹具及本地48项真实GDAL专项／62项脚本验证已具备，实际运行及归档终审仍待验，不沿用其他流长证据。

D8 Strahler 河流分级首期已实现并通过本地标准Native全量3076项，双输入0／1掩膜、完整图校验、同最高级汇合升级及断流失败规则见算子契约，固定候选源码、专项与完整证据见[后续专题](../next/栅格算子体系后续专题.md#d8-strahler-河流分级首期)。Linux全量等专门CI会话回传，独立Hosted入口及夹具已实现，本地48项真实GDAL专项／63项脚本与完整登记通过，实际运行及归档终审待验，不沿用其他D8算子证据，不计平台扫描／血缘、UI或容量。

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

D8加权汇流累积首期已实现，显式双输入、不计自身、非负有限权重、覆盖完全一致及完整D8路线校验见[算子契约](workflow/栅格算子契约.md)。本地标准 `make test-geopython-workflow` 全量 **3296 passed／21 warnings／710.94秒**，0失败／跳过／筛除，进度计数一致且验证输入无漂移；独立逐源路径核完整像元／mask、全1权重与无权累积一致、出口总量守恒，以及真实COG、双语异步失败和清理。Linux全量继续等CI会话回传，独立Hosted尚未授权／运行，不计平台扫描血缘、UI或容量。
