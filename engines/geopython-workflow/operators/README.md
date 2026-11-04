# GeoPython Workflow 算子模块

GeoPython Workflow 的空间算子模块，采用模块化架构设计，底层使用 GeoPandas / Shapely 与 GDAL 提供矢量和栅格分析能力。

## 栅格计算

`raster_interpolate_idw`以二维Point、显式数值字段和同CRS模板网格生成连续面：幂次power支持1／2／3、默认2保持原结果，显式米制半径、等号纳入、默认全体支持、精确重合取原值；可选正整数`min_points`默认1，`max_points`省略不限、提供时须为正整数且不小于最少点数；精确半径过滤后按距离平方／X／Y取最近N点，非重合位置支撑不足为NaN，精确重合仍保留样本值；重复XY／非法数值拒绝。模板只提供网格与CRS，不读取／继承NoData、mask或alpha，支持有限可逆旋转／矩形／剪切网格；结果单波段Float64，同网格／CRS，沿原COG保存及清理。归“基础计算”，无新权限／依赖；[完整IDW契约](../../../docs/spec/workflow/栅格算子契约.md#点数据idw插值)。

`raster_point_density`统一提供`method=uniform/quartic`，默认uniform按圆内点数除完整圆面积，保留原结果；quartic固定二维四次核，中心贡献最大、半径边界准确0。可选weight_field显式选有限非负权重，两方法均乘权重且不按总权重归一化；省略保持原结果，全1数值／mask一致，缺失／负权重整次失败，原生整数精确累计。选字段单位weight/m2，否则points/m2。两者均显式米制半径、重复XY逐条贡献，无支持／空批有效0；模板仅提供网格／CRS，允许矩形／旋转／剪切。共用精确圆筛选、分块及COG保存／清理，无新增入口、权限或依赖；[完整点密度契约](../../../docs/spec/workflow/栅格算子契约.md#米制圆形点密度栅格)。

`raster_line_density`以像元中心圆内米制线长除完整圆面积，重复／重叠／折返逐原始段累计，相切或无支持有效0；二维LineString／MultiLineString先全量严格校验，同投影CRS／显式米制半径，模板仅网格与CRS，允许旋转／剪切／矩形。解析裁圆与有理区间最终舍入，不用buffer近似或epsilon；Float64单位metre/m2，沿原分块／COG及失败清理，可选weight_field为有限非负线长乘数，不归一化，省略保持原结果／单位，全1数值与mask一致，缺失／负权重包括范围外均失败；选字段单位weight/metre，全部多部分量沿原要素权重累计。同一入口method=uniform/quartic，默认uniform保持原结果；quartic固定二维四次核，沿原始线段解析积分，半径边界贡献0、不离散成点或重新归一化，单位与权重规则保持。无新依赖或权限。[完整线密度契约](../../../docs/spec/workflow/栅格算子契约.md#米制圆形线密度栅格)。

矢量面分区统计沿原入口使用非空严格递增`percentiles`列表：原八列保持，每面一行，按请求顺序追加带原前缀的percentile_i／percentile_value_i列对；覆盖、孔洞、重叠面独立、原属性／几何／索引及CRS保持，每面共享一次覆盖读取／排序／归并。旧单值参数及列删除，旧DAG须显式改写；原单项套件只迁移单元素列表／列名，既有矩阵／预算保持。完整边界见栅格算子契约，Linux全量与实际Hosted分别记账。

通用算子由 [raster_compute.py](raster_compute.py) 聚合，分类与算子归属见[算子分类](#算子分类)。栅格通过当前 execution 的 `raster` / `raster_tiles` 私有对象在 DAG 中流转；公开资源使用 ResourceLocator，由 Develop 派生访问计划，不能用原始路径或普通 JSON 代替栅格端口。

| 阅读目的 | 正式契约 |
| --- | --- |
| 公开资源、内部对象、生命周期与有效像元 | [资源与执行模型](../../../docs/spec/addp栅格计算与成果契约规范.md#二资源与执行模型) |
| 逐项参数、网格与数值规则；裁剪、重分类、聚合、转面、栅格化及各类统计、邻近距离、OIF 与蚕食 | [栅格算子契约](../../../docs/spec/workflow/栅格算子契约.md#栅格计算契约) |
| 跨文件加权、RGB 通道、多文件拼接与局部更新的节点组合 | [常用组合](../../../docs/spec/addp栅格计算与成果契约规范.md#41-常用组合) |
| 批量切片的网格、命名、目标范围与逐片身份 | [自动批量切片约定](../../../docs/spec/addp栅格计算与成果契约规范.md#42-自动批量切片约定) |
| 部分发布、发布未知与清理失败时的确认事实 | [发布失败事实](../../../docs/spec/addp栅格计算与成果契约规范.md#43-发布失败事实) |
| 运行／等待额度与进程缓存 | [资源准入](../../../docs/spec/workflow/栅格算子契约.md#geopython-栅格资源准入)、[引擎资源配置](../../../docs/spec/addp栅格计算与成果契约规范.md#五引擎资源配置) |
| 标准门禁、Native 与各项真实链路的验收范围 | [验收入口与有效结论](../../../docs/spec/addp栅格计算与成果契约规范.md#六验收入口与有效结论) |

`raster_proximity`支持已知线性单位二维投影下的北向无旋转矩形像元，按横纵实际米制距离选择最近有效目标，保持原网格／Float64／NaN及米制单位；目标为0，源无效不作障碍。方形输入结果逐像元保持，蚕食仍要求方形网格；共享精确下包络与原临时磁盘保护／COG／失败清理，不新增算子或参数。旋转、测地和成本距离不在本扩展内，详细数值与准入见栅格算子契约。

### 本模块实现注意

- `raster_mosaic`：显式源 mask 在分块有效性计算后转为各波段 NoData，避免 GDAL 同时遇到 mask 和 NoData 时忽略 mask；原始源保持不变。
- `raster_save_tiles`：每次只物化与编码一片，随后清除该片中间文件；源快照和逐片摘要保留至执行结束，摘要内存随片数增长。

`raster_to_cog` 是唯一通用 COG 转换入口，支持受控 direct 调用；`raster_inspect` 是只读媒体元数据 direct 入口，不进入工作流面板；其余通用栅格算子在工作流中运行。`raster_operators.py` 的 `build_raster_mosaic` 继续服务 Manager 目录型业务数据集，两者复用同一 COG 编码实现。

D8汇流累积`raster_flow_accumulation_d8`仅有一个`measure=count/area`选择器，默认count沿原图不计自身；area包含自身并按投影网格及CRS线性单位换算平方米（波段单位m2）。共用完整图校验与计数核心，矩形像元允许，面积数值无法有限正表示或累积溢出失败；不表示椭球面积或径流，汇水点吸附仍须显式提供未加权计数。详见[栅格算子契约](../../../docs/spec/workflow/栅格算子契约.md)。

## 目录结构

| 文件 | 职责 |
| --- | --- |
| [base.py](base.py) / [__init__.py](__init__.py) | 内部元数据模型、分类与注册工具；聚合算子并生成标准公开元数据 |
| [io_operators.py](io_operators.py) | 数据加载与保存 |
| [geometric_operators.py](geometric_operators.py) / [spatial_relations.py](spatial_relations.py) / [properties_operators.py](properties_operators.py) | 几何处理、空间关系与几何属性 |
| [data_operations.py](data_operations.py) / [attribute_operators.py](attribute_operators.py) / [filter_operators.py](filter_operators.py) | 数据操作、属性计算与筛选 |
| [format_operators.py](format_operators.py) / [spatial_transform_operators.py](spatial_transform_operators.py) | WKT 格式与坐标参考转换 |
| [raster_compute.py](raster_compute.py) / [raster_band_recommendation.py](raster_band_recommendation.py) | 通用栅格计算与波段组合建议 |
| [raster_operators.py](raster_operators.py) / [vector_tile_operators.py](vector_tile_operators.py) | Manager 栅格镶嵌与矢量瓦片成果生成 |
| [netcdf.py](netcdf.py) / [grib.py](grib.py) | 单文件数值容器的只读目录及有界切片；复用共享预览协议，分别声明原存储值和原单位解码值 |
| [gdal_vector_dataset.py](gdal_vector_dataset.py) | GDAL 矢量数据集批协议 |

## 算子分类

`load` 对空间文件、Parquet 与 Feather 按源 Schema 保留可空整型与布尔型，避免空值导致整型提升为浮点并损失精度；统一使用 Arrow→pandas 的选择性类型映射，其他字段不按取值重新推断。空间文件按 Arrow 几何声明解码并保留真实几何列，不固定写入 `geometry` 或覆盖同名业务列；无物理名时可显式绑定无冲突的逻辑几何名。边界与验证见[文件字段类型契约](../../../docs/spec/workflow/矢量算子契约.md#load-文件字段类型契约)。

分类由 Runtime 的 `OperatorCategory` 声明。工作流面板只显示支持 `workflow` 的算子，实际数量以当前注册表为准。所有分组名称为四个字，`category` 为末级组名，`category_path` 为完整两级目录；同级顺序由 Runtime 分类声明决定。

| 一级分组 | 二级分组 | 数量 | 算子 |
|---|---|---:|---|
| 通用处理 | 数据读写 | 2 | `load`、`save` |
| 通用处理 | 字段处理 | 5 | `add_field`、`calculate_field`、`rename_fields`、`drop_fields`、`type_cast` |
| 通用处理 | 数据清洗 | 2 | `fill_null`、`drop_duplicates` |
| 通用处理 | 数据筛选 | 5 | `filter_by_attribute`、`sort_by_field`、`select_top_n`、`sample`、`random_split` |
| 通用处理 | 数值统计 | 4 | `normalize_field`、`encode_categorical`、`bin_field`、`aggregate` |
| 矢量处理 | 几何构造 | 8 | `buffer`、`centroid`、`representative_point`、`convex_hull`、`minimum_rotated_rectangle`、`envelope`、`voronoi`、`batch_buffer` |
| 矢量处理 | 几何整理 | 10 | `simplify`、`dissolve`、`check_geometry`、`repair_geometry`、`snap_geometry`、`snap_line_endpoints`、`extend_line_endpoints`、`trim_line_endpoints`、`split_multipart`、`extract_vertices` |
| 矢量处理 | 拓扑检查 | 8 | `check_polygon_overlap`、`check_polygon_gap`、`check_dangling_points`、`check_line_proximity`、`check_line_overlap`、`check_line_self_overlap`、`check_line_intersection`、`check_line_self_intersection` |
| 矢量处理 | 线网处理 | 7 | `polygonize`、`extract_polygon_rings`、`split_line_intersections`、`group_line_components`、`merge_line_chains`、`normalize_line_network`、`extract_line_nodes` |
| 矢量处理 | 叠加分析 | 4 | `intersection`、`union`、`difference`、`clip` |
| 矢量处理 | 空间关系 | 3 | `contains`、`intersects`、`distance_to` |
| 矢量处理 | 几何量测 | 3 | `get_area`、`get_length`、`get_bounds` |
| 矢量处理 | 要素筛选 | 3 | `filter_by_geometry_type`、`drop_null_geometry`、`split_by_area` |
| 矢量处理 | 坐标转换 | 1 | `vector_reproject` |
| 矢量处理 | 格式转换 | 2 | `load_from_wkt`、`export_to_wkt` |
| 矢量处理 | 成果生成 | 1 | `vector_to_pmtiles` |
| 栅格处理 | 数据读写 | 3 | `raster_load`、`raster_save`、`raster_save_tiles` |
| 栅格处理 | 网格处理 | 3 | `raster_reproject`、`raster_resample`、`raster_align` |
| 栅格处理 | 范围处理 | 5 | `raster_clip`、`raster_mosaic`、`raster_update`、`raster_tile`、`raster_footprint` |
| 栅格处理 | 波段处理 | 2 | `raster_stack`、`raster_select_bands` |
| 栅格处理 | 基础计算 | 6 | `raster_band_math`、`raster_aggregate`、`raster_proximity`、`raster_interpolate_idw`、`raster_point_density`、`raster_line_density` |
| 栅格处理 | 分类处理 | 6 | `raster_reclassify`、`raster_nibble`、`raster_boundary_clean`、`raster_sieve`、`raster_region_group`、`raster_thin` |
| 栅格处理 | 地形分析 | 10 | `raster_slope`、`raster_aspect`、`raster_hillshade`、`raster_curvature`、`raster_tpi`、`raster_tri`、`raster_roughness`、`raster_twi`、`raster_spi`、`raster_contour` |
| 栅格处理 | 水流分析 | 5 | `raster_fill_depressions`、`raster_flow_direction_d8`、`raster_flow_accumulation_d8`、`raster_flow_accumulation_weighted_d8`、`raster_flow_length_d8` |
| 栅格处理 | 汇水分析 | 4 | `raster_drainage_basins_d8`、`raster_snap_pour_points`、`raster_watershed_d8`、`raster_subcatchments_d8` |
| 栅格处理 | 河网分析 | 5 | `raster_stream_order_d8`、`raster_stream_link_d8`、`raster_stream_to_vector_d8`、`raster_distance_to_stream_d8`、`raster_hand_d8` |
| 栅格处理 | 格式转换 | 2 | `raster_polygonize`、`vector_rasterize` |
| 栅格处理 | 栅格统计 | 10 | `raster_statistics`、`raster_cut_fill`、`raster_surface_area`、`raster_zonal_statistics`、`raster_focal_statistics`、`raster_vector_zonal_statistics`、`raster_sample_points`、`raster_sample_lines`、`raster_recommend_band_combination`、`raster_histogram` |
| 栅格处理 | 栅格检查 | 2 | `raster_info`、`validate_cog` |
| 栅格处理 | 成果生成 | 3 | `raster_to_cog`、`raster_build_overviews`、`build_raster_mosaic` |

`raster_inspect`、`crs_to_projjson` 和 GDAL vector dataset 批协议算子仅供受控 direct 调用，不进入工作流面板计数。分类调整不改变算子身份、参数、端口和已有工作流定义；不增加第三级目录或前端名称映射。原像元计算拆为基础计算、分类处理、地形分析、水流分析、汇水分析和河网分析；转面／栅格化归格式转换，等值线归地形分析，河网转线归河网分析。

D8流长只保留`raster_flow_length_d8`一个入口，必填`method=downstream`／`upstream_longest`分别计算下游／上游最长流长；原独立上游ID删除，旧DAG需显式改写，不保留兼容别名。两种模式的米制、中心步长、开放边界、无效覆盖和严格图校验保持。

“多距缓冲”（`batch_buffer`）把同一图层的多距离完整缓冲展开为一个图层，保留源属性和 CRS 并追加 `distance` 列，源列重名时报错；可直接接现有保存节点。删除“多图质心”（`batch_centroid`）的函数与入口，多个图层分别连接普通 `centroid`，旧 ID 不保留别名。普通缓冲和质心均已对单图层逐要素执行。“外包矩形”（`envelope`）为轴对齐包络。输入输出与验证边界见[多距缓冲与质心入口契约](../../../docs/spec/workflow/矢量算子契约.md#多距缓冲与质心入口契约)。

## 融合几何

`dissolve(input_gdf,id_field,group_fields)` 按显式字段组合求面并集，[]为全局，空值保留成组。result保分组字段、fusion_id/member_count，members保全部来源属性和原始几何；不取首值、不隐式sum/mean、不保留by旧路线。见[融合几何契约](../../../docs/spec/workflow/矢量算子契约.md#融合几何契约)。

## 联合叠加

`union(input_gdf, gdf_b, id_field, reference_id_field)` 只接受两个有效二维面图层及显式来源 ID，同层正面积重叠拒绝。输出交叠、仅 A、仅 B 三类完整面结果，保留双方属性与来源，缺侧为真实空值；不额外报共边／共点、不拆多部件、不自动改名或修复。旧 gdf_list 多层路线删除；边界见[联合叠加契约](../../../docs/spec/workflow/矢量算子契约.md#联合叠加契约)。

## 裁剪几何

`clip(input_gdf, mask_gdf, id_field)` 按整体面掩膜逐 A 求完整交集，保留 A 的来源与全部业务属性；共边、共点和 GeometryCollection 不按维度丢弃，多部件不拆行。空掩膜返回保完整 Schema 的空结果。与逐对保留 A/B 属性的 intersection 及整体范围扣除的 difference 边界见[裁剪几何契约](../../../docs/spec/workflow/矢量算子契约.md#裁剪几何契约)。

## 拓扑构面

`polygonize` 从线网提取闭合面，面结果 `result` 与余线诊断 `diagnostics` 可分别连接 `save`。输入限制、交点打断与去重、孔洞、诊断含义和内存边界统一见[拓扑构面契约](../../../docs/spec/workflow/矢量算子契约.md#拓扑构面契约)。

## 重合规整

`normalize_line_network` 按原始顶点、精确交会和重合端点细分线网，默认输出唯一边，`members` 保留每次原始经过及全部业务属性。共享边不选主来源、不吸附或补缝、不自动合并链路。双端口、方向、编号与零长度段的边界见[重合规整契约](../../../docs/spec/workflow/矢量算子契约.md#重合规整契约)。

## 链路合并

`merge_line_chains` 在全图非分支端点处按显式业务字段精确同值串联。默认 `result` 保存链路和共同属性，`members` 保存每个原始部件、全部原属性及沿链顺序／反向标记；两个端口分别接既有 save。输入需先处理未打断交会、内部自交和重合；完整边界与稳定编号见[链路合并契约](../../../docs/spec/workflow/矢量算子契约.md#链路合并契约)。不选择主路、不吸附或修复。

## 连通分组

`group_line_components` 按原始线部件的精确 XY 相交关系传递归组，每部件保留来源、全部属性和原几何；同源不相连部件可以分属不同组。稳定编号、空批及保存边界见[连通分组契约](../../../docs/spec/workflow/矢量算子契约.md#连通分组契约)。只分组，不打断、吸附、合并或修复。

## 交点打断

`split_line_intersections` 以`scope=between`处理跨要素点交会；显式`scope=all`统一处理要素内部及跨要素的全部精确点交会，共用原始段切分主路径。每片保留原属性、来源、原方向和部件／片段编号，同一XY的多次经过分别切分，任何正长度重合均整批拒绝。闭环接缝、重复坐标、范围拒绝及保存规则见[交点打断契约](../../../docs/spec/workflow/矢量算子契约.md#交点打断契约)。

## 线内自交

`check_line_self_intersection` 报告同一线要素内部的孤立点交会，每源按 XY 去重，保留来源，不改线。相邻段、闭环首尾、跨部件接触与自叠位置排除规则见[线内自交契约](../../../docs/spec/workflow/矢量算子契约.md#线内自交契约)。

## 线交检查

`check_line_intersection` 逐对报告不同线要素的精确点交会，每对按 XY 去重，保留双方来源，只检查不改线。十字、T形、端点接触及重合不造点等边界见[线交检查契约](../../../docs/spec/workflow/矢量算子契约.md#线交检查契约)。

## 线内自叠

`check_line_self_overlap` 检查同一个线要素内部的重复走线和部件间重合，每源汇总去重后报告来源、线和长度，只检查不修复。重复次数、点交集和保存边界见[线内自叠契约](../../../docs/spec/workflow/矢量算子契约.md#线内自叠契约)。

## 面界提取

`extract_polygon_rings` 按原始面部件的外环、内环逐环生成闭合线，保留来源、全部属性、原始坐标、起点及方向，共有边和重复面分别保留。`ring_id`为本次结果内唯一环号，`source_id/part_no/ring_no/is_hole`记录原始面及环角色；不计算度量、去重或修复。来源字段冲突及后续显式重命名规则见[面界提取契约](../../../docs/spec/workflow/矢量算子契约.md#面界提取契约)。

## 线叠检查

`check_line_overlap` 逐对报告不同线要素的精确重合段、双方来源与长度，只检查不改线。点接触、单要素内部重复、长度单位与逐对计数边界见[线叠检查契约](../../../docs/spec/workflow/矢量算子契约.md#线叠检查契约)。

## 悬点检查

`check_dangling_points` 报告去重平面线网中度为一的端点，并逐条关联原始来源 ID。它不修改线、吸附或判断质量，连接语义与构面余线的区别见[悬点检查契约](../../../docs/spec/workflow/矢量算子契约.md#悬点检查契约)。

## 近接检查

`check_line_proximity` 复用源线悬点提取；精确落在任一参考线上的点整体排除，其余逐参考要素报告严格容差内的原悬点、双方来源、距离和最近位置。只检查不改线，详见[近接检查契约](../../../docs/spec/workflow/矢量算子契约.md#近接检查契约)。

## 端点吸附

`snap_line_endpoints` 将可仅移动原始开口部件端部完整处理的悬点，一次移到严格容差内参考线的唯一精确最近位置。默认`result`保留原业务属性与部件，`details`保留原端点Point、双方来源和全部移动事实；等距异点、内部／闭环覆盖、不能精确落线或塌缩／无效时整批失败。复用近接候选内核，不改变近接检查的诊断语义，详见[端点吸附契约](../../../docs/spec/workflow/矢量算子契约.md#端点吸附契约)。

## 几何检查与修复

`check_geometry` 追加单要素有效性诊断，`repair_geometry` 修复无效几何并保留退化结果；它们不替代跨要素拓扑检查。二维输入、字段冲突、修复方法、属性与空缺失值保留规则见[几何检查与修复契约](../../../docs/spec/workflow/矢量算子契约.md#几何检查与修复契约)。

复查请设置新的结果字段名，保留修复前诊断。

## 面叠检查

`check_polygon_overlap` 检查单个面图层内要素对的正面积交集。来源标识、稳定去重、面积单位、接触与空结果、容量及保存边界统一见[面叠检查契约](../../../docs/spec/workflow/矢量算子契约.md#面叠检查契约)。

## 面缝检查

`check_polygon_gap` 检查显式覆盖范围减去待检查面整体覆盖后的缺口，只报告面几何和面积，不自动填补。范围孔洞、空输入、结果顺序、面积单位及保存边界统一见[面缝检查契约](../../../docs/spec/workflow/矢量算子契约.md#面缝检查契约)。

## 参考吸附

`snap_geometry` 将输入原始顶点一次匹配到固定参考图层的原有顶点。投影 CRS、容差单位与严格边界、等距决胜、无效输出拒绝及容量限制统一见[参考吸附契约](../../../docs/spec/workflow/矢量算子契约.md#参考吸附契约)。

## 使用方式

工作流定义、公开资源参数和执行期访问计划遵循 [Develop 资源与执行规范](../../../docs/spec/workflow/Develop资源与执行.md#算子契约分层)。用户与 AI 侧保存公开参数，由后端完成资源适配并注入可信租户和执行授权；不得把内部连接示例当作完整可执行请求。HTTP 封装、授权与错误响应统一见 [HTTP 协议](../../../docs/spec/workflow/HTTP协议.md)。

包内 `OPERATORS` 保存注册结果，`get_operator(name)` 获取实现函数，`list_operators()` 生成 `addp.workflow/v1` 标准元数据。服务通过 `GET /api/operators` 发布发现结果，业务模块通过 `WorkflowRuntimeProvider` 消费。DAG、引用解析和端口适配见 [workflow_engine.py](../workflow_engine.py)与[公共执行核心](../../../common-python/addp_common/workflow_runtime/)。

## 新增算子开发指南

1. 按上面的目录职责选择实现文件，在同一模块定义函数和元数据。命名、active geometry 访问和函数参数要求遵循[算子实现规范](../../../docs/spec/workflow/算子元数据与实现.md#算子实现规范)。
2. 使用 [base.py](base.py) 的 `OperatorMetadata`、`OperatorParam` 和 `OutputPort`，显式声明执行模式、effects 与端口；必填字段、类型与多端口规则统一见[算子元数据规范](../../../docs/spec/workflow/算子元数据与实现.md#算子描述结构)，不在此另维护模型模板。
3. 通过 `register_operator(metadata, func)` 写入分类模块的 `OPERATORS`，再由 [__init__.py](__init__.py) 汇总并维护必要导出。`OperatorMetadata.to_runtime_dict()` 生成内部注册结构，`list_operators()` 统一转成协议字段；不另维护第二份 API 元数据。
4. 补充算子行为和注册发现验证，通过下面的标准入口检查实际元数据、分类、执行模式、effects 和端口；需要公开资源适配时同步遵循 Develop 分层契约。

内部 `OperatorParam.type` 表示 input/output/param 角色，`data_type` 表示内部数据类型；公开 `parameters[]` 由统一出口转换，不能把内部模型字段原样当作 HTTP 契约。分类由 `OperatorCategory` 的两级元组声明，序列化时生成末级 `category` 和完整 `category_path`；同级顺序由枚举声明决定，完整算子归属见[算子分类](#算子分类)。

## 测试

在仓库根目录使用标准入口：

```bash
make test-geopython-workflow
```

该入口准备本 Runtime 的原生依赖环境，覆盖真实 GDAL 栅格、矢量、多端口、元数据及 API 契约；元数据与类别回归见 [test_operator_metadata.py](../test_operator_metadata.py)和 [test_non_spatial_operators.py](../test_non_spatial_operators.py)。算子发现验证以当前注册表与 `GET /api/operators` 的对应关系为准，不在测试样例中固定可能随扩展变化的数量。

完整 owner 门禁为 `make test-module MODULE=engines`。协议元数据的独立校验方法见[公共校验入口](../../docs/README.md#元数据校验)，测试层级与真实运行验收分别见[测试与验收规范](../../../docs/spec/addp测试与验收规范.md)及[运行说明](../docs/geopython-workflow容器管理指南.md#验证)。确定性回归不替代跨模块或产品镜像验收。

## 故障排查

| 问题 | 核对入口 |
| --- | --- |
| 导入或原生库错误 | 使用 [GeoPython 标准运行环境](../docs/geopython-workflow容器管理指南.md#本地开发)，检查依赖准备结果，避免在宿主机全局 Python 中另装依赖或绕过标准启动脚本 |
| 算子缺失或分类不符 | 检查分类模块注册、`__init__.py` 聚合和 `execution_modes`；仅供 direct 的算子不进入 workflow 面板 |
| 元数据不完整或端口不匹配 | 按[正式元数据规范](../../../docs/spec/workflow/算子元数据与实现.md)核对实际 `list_operators()` 输出，并使用[公共校验器](../../docs/README.md#元数据校验)检查协议结果 |
| 工作流参数或资源访问失败 | 区分公开参数与 Runtime 执行参数，按 [Develop 资源适配规范](../../../docs/spec/workflow/Develop资源与执行.md)核对 locator 派生、访问计划及授权，不把原始路径写入公开参数 |

## 参考资料

- [工作流计算引擎接口规范](../../../docs/spec/addp工作流计算引擎接口规范.md)
- [矢量算子契约](../../../docs/spec/workflow/矢量算子契约.md)
- [栅格算子契约](../../../docs/spec/workflow/栅格算子契约.md)
- [ADDP 开发原则](../../../docs/spec/addp开发原则.md)

`raster_hand_d8` 提供显式D8方向／河网／DEM的到河网相对高程：全图下游高程不升高、允许平坦，逆坡失败；相对首次河网高程差统一米制，河网与等高非河网0、未到达NoData。三波段及高程单位必填，复用原COG主路径；完整输入／数值／失败边界见平台栅格算子契约。

`raster_curvature`：DEM曲率，`method=mean/profile/tangential/plan`默认mean保留平均曲率；同一完整有效3×3九点等权二次曲面，凸正凹负，单位`1/metre`。profile沿最大坡降方向，tangential沿等高线切向计算法向曲率；plan为等高线水平投影的几何曲率；三种方向方法九点严格等高平地为0、其他零梯度点NoData；无完整邻域NoData。显式波段与高程单位，复用原COG主路径，详见平台栅格算子契约。

`raster_tpi`计算完整有效3×3窗口的中心高程减周围8点等权均值，统一米制，合法零不排除；复用DEM准入、带halo分块、已有raster端口及COG保存。边缘与无效邻域为NaN；原生高差与抵消保留至最终Float64转换，溢出／非零下溢严格失败。详见[栅格算子契约](../../../docs/spec/workflow/栅格算子契约.md#dem-地形位置指数tpi)。

`raster_tri`提供固定3×3 Riley DEM地形崎岖度：中心与8邻居高差平方和开方、统一米制，完整九点有效才计算；水平平地合法0，倾斜平面通常为正。沿用DEM准入、带halo分块、原网格和COG保存，原始高差及平方和保留至最终开方舍入；范围和数值边界见[栅格算子契约](../../../docs/spec/workflow/栅格算子契约.md#dem-地形崎岖度tri)。

`raster_roughness`计算完整有效3×3九点的最大原始高程减最小原始高程，包含中心、统一米制。水平平地合法0、倾斜平面通常为正；保留原始64位高差及最终Float64范围校验，沿用DEM准入、带halo分块、原网格和COG保存。原TPI／TRI／曲率保持，详见[栅格算子契约](../../../docs/spec/workflow/栅格算子契约.md#dem-地形粗糙度roughness)。

TWI只有`raster_twi`一个工作流入口：显式面积m2与degree/percent坡度两源，固定A/方形像元米制边长，零坡度NoData、非零无阈值；既有坡度与D8面积可直接连接，结果是潜在湿润倾向指标，不代表含水量。

SPI只有`raster_spi`一个入口：显式m2汇流面积与degree/percent坡度，固定(A/米制方形像元边长)×tanβ，零坡度0，非零无阈值，输出metre；不取对数或表示实际侵蚀量，原TWI结果保持。

`raster_region_group`对相同有效分类的4／8邻接斑块分别编号，按最前原始行列从1排序；保留合法零／负分类、无效覆盖、网格／旋转和CRS／缺定位事实。仅输出同网格Float64／NaN标签，可接现有分区统计和COG保存；编号是本幅局部标签。与筛除共用唯一游程／连通标记和临时文件生命周期，不改变原筛除规则，不新增映射表、权限或依赖；完整数值、清理及验收边界见栅格算子契约。

`raster_sieve`显式波段／4或8邻接，min_pixels与min_area_m2必须且只能提供一个；后者使用已知线性单位的北向无旋转投影网格平面平方米，允许非方形像元，精确比较、等于阈值保留；原像元模式仍支持旋转及缺定位。按原始连通斑块大小一次冻结，只合并到相邻达标供体，大小并列按较小分类／最前行列决定；无供体保留、不跨无效区、不填洞。可选protected_values保护整类有效像元免于替换，供体仍须原始大小达标；空列表／省略保持原结果，未命中分类允许，非法／重复保护值明确失败。原边界清理／蚕食结果保持。

`raster_focal_statistics`增加`method=weighted_mean`：显式同尺寸3×3／5×5有限非负二维权重，至少一个正值，行列直接对应不翻转；按实际有效正权重位置重新归一化，中心无效或有效权重和零输出NaN。全1复用mean，原九方法保持；其他方法不得传weights。统一先按原生类型判定NoData再转Float64，修正均值等数值方法在原生64位NoData邻接值上的错误无效覆盖；不改共享读取器或其他算子。沿原网格／CRS／有界halo／COG及清理，详见栅格算子契约。

`raster_focal_statistics`的`method=median`复用原邻域统计入口，固定3×3／5×5、包含中心、实际有效邻居排序，偶数取两中值平均；允许小数，中心无效保留、边缘不补零，沿原网格／Float64／COG。原sum／mean／min／max／stddev／mode语义保持，不新增面板入口。

同一入口的`method=percentile`统一显式提供非空、严格递增且不重复的`percentiles`列表，各项为0–100有限非布尔实数；旧单值参数删除，单项也用列表，旧DAG须显式修改。方窗3／5及米制圆窗沿原成员／有效性规则，按同一线性位置`h=(n−1)p/100`计算，P50与median一致。每块共享一次读取与排序，逐项写同网格Float64／NaN波段，顺序对应列表、描述为`percentile=<Float64 repr>`，COG保存后保持。其他十一种方法与原结果保持，其他方法禁止提供列表（含null）；成功失败清理及原套件场景／预算不变。独立多项Hosted另议，不新增面板入口、权限或跨模块逻辑。

同一邻域入口新增`method=variety`，固定3×3／5×5含中心，计有效邻居中不同分类编码的数量；沿mode精确安全整数准入，合法零／负分类保留，不截断或舍入。中心无效仍为NoData，无效邻居排除、边缘不补零，有效结果1–9／25；保留网格与CRS，沿有界读取／排序和原COG保存清理。原八方法不变，不新增面板入口、权限或依赖。

`raster_statistics`可附加非空、严格递增且不重复的`percentiles`列表，各项为0–100有限非布尔实数；追加有序`percentiles=[{"percentile":p,"value":v},...]`，未提供时保持原统计对象字段且不排序。原单值percentile参数及percentile_value结果已删除，旧DAG须显式改成列表，不自动迁移。共享一次全量分块读取、私有排序与最终归并，各项固定线性插值、50为中位数；全无效时各value为null。原生NoData先判定，随后沿Float64数值边界，保留原统计／有效性／2GiB磁盘余量及成功失败清理；不采样、不近似、不生成持久化成果，不扩展邻域或分区接口，详见栅格算子契约。

`raster_zonal_statistics`可显式成对提供`weights_raster/weights_band`，与percentiles互斥，在原九列后追加weight_sum／weighted_sum／weighted_mean；原统计不改写，全1复用原sum／mean。有效数值位置的权重须有效有限非负，缺失不补零；全零权重为0／0／null，无有效数值组新增三项null。三输入同网格／CRS、原生NoData先判定，按块精确累计并最终舍入，真实溢出明确失败；保留普通表／CSV、50行预览及清理，不增加面板入口或权限，详见栅格算子契约。

`raster_zonal_statistics`可显式`include_weighted_stddev=True`追加nullable `weighted_stddev`，须提供原成对权重输入；总体分母Σw，全1与原stddev精确一致，全零权重为null。默认关闭，原统计、百分位和三项加权Schema保持；不扩展矢量面入口。

`raster_zonal_statistics`可附加非空严格递增、不重复的`percentiles`列表，原单值参数删除、旧DAG须显式修改；每分区仍一行，在原九列后按请求顺序从1追加`percentile_1/percentile_value_1`等Float64列对；按精确整数分区的全部有效数值独立计算，空组为null，分区全无效保留空表Schema。原统计口径、同网格双输入、普通表／CSV保存及最多50行预览保持；一次双源读取与私有有界分块排序／最终归并共享给所有百分位，不按项重做；复用磁盘账本及2GiB安全余量，成功失败均清理，不为每区保留全部像元或打开文件。

`raster_vector_zonal_statistics`可成对显式提供`weights_raster/weights_band`，与percentile互斥；同网格权重按原覆盖逐面统计，重叠面分别计入，不按交叠面积分摊。原八字段保持，追加同前缀weight_sum／weighted_sum／weighted_mean，全1复用原sum／mean；必需位置缺失或负权重严格失败。全零权重0／0／null，无值三项null，空批Schema及几何／属性／索引保持；共用原生NoData、分块精确累计与既有矢量保存／清理，无新入口或权限，详见栅格算子契约。

`raster_vector_zonal_statistics`可显式`include_weighted_stddev=True`追加前缀weighted_stddev，采用总体分母，全1精确沿用原stddev，全零／空组null；默认False保留原Schema，沿原面覆盖／重叠独立规则及GeoPackage路径，启用列冲突严格拒绝。

`raster_vector_zonal_statistics`可附加单个`percentile`，逐原始面全量固定线性插值，在原八个统计字段后按原前缀追加`percentile/percentile_value`，同名冲突失败。原中心／全部触及覆盖、孔洞、重叠分别计入及属性／几何／行序／索引保持；无覆盖／无有效数值保留行且值null，原生NoData先判定，逐面私有外排序后释放，沿磁盘账本保留2GiB。结果仍走原矢量保存路径，不增加面板入口、权限或自动重投影。

## 线端延伸

`extend_line_endpoints` 沿原始开口部件朝外方向，将悬点延伸到显式距离上限内首次参考交点；上限包含等号，共线取入口，同点参考来源全部保留。原顶点、方向及业务属性不变，`details`为原端点指向目标的新增段；方向不一致、不能精确连接或结果无效整批失败。无目标不变，不连锁或隐式修复。详见[线端延伸契约](../../../docs/spec/workflow/矢量算子契约.md#线端延伸契约)。

`raster_focal_statistics`扩展`weighted_sum`邻域加权求和：固定3／5非负矩阵，按有效正权重位置求Σ(w×x)，不归一化；保留权重绝对大小，全1与原sum逐像元／mask一致。中心无效／有效支持空为NaN，合法零和负结果保留；非全1使用精确乘积累计最终舍入，真正超出Float64范围的像元沿原sum写NaN。原十一方法、有界halo、原网格／COG及清理保持；不含负权重卷积、距离核或新权限，详见栅格算子契约。Linux／Hosted仍分别验收。

`raster_focal_statistics`扩展`weighted_stddev`加权总体标准差：固定3／5窗口，复用非负二维权重与有效位置重新归一化；中心无效／无有效正权重为NaN，常量或单样本为0，全1与原stddev完全一致。非均匀权重精确一阶／二阶矩及共享平方根最终舍入，原十方法保持；不含样本标准差或新权限，详见栅格算子契约。Native、Linux与Hosted分别记账。

## 线端修剪

`trim_line_endpoints` 沿原始悬端向内按原路径搜索首个精确参考交会，共线取入口，累计长度上限含等号，不跨部件或越过原线网分支。保留源属性、方向、行与部件；result为修剪线，details为目标Point及逐端点全部参考来源明细。内部自交／源正长度重合、两端冲突／整段消失或不能精确落线整批拒绝。复用统一参数诊断，详见[线端修剪契约](../../../docs/spec/workflow/矢量算子契约.md#线端修剪契约)。不自动补缝、吸附或修复。

`raster_vector_zonal_statistics`可显式`include_area_weighted=True`追加覆盖平方米／有效覆盖平方米／面积加权均值；原八统计及all_touched规则保持，新三列独立按实际正交叠面积计算。孔洞扣除、重叠独立，已知线性单位二维投影支持原旋转仿射网格；与权重栅格、百分位及外部权重标准差互斥。可在面积模式显式include_area_weighted_stddev=True追加面积加权总体标准差，采用总体分母、单位沿数值，常量0、无有效面积null，原十一列保持；无有效面积均值null，字段冲突与数值范围失败严格拒绝；不增加面板入口，详见栅格算子契约。

## 节点提取

`extract_line_nodes`对已打断且无重合线网，按原始部件首尾提取Point节点、度数及完整端部来源Point明细。闭环首尾计两次，同源多部件分别计，拒绝未打断交会／自交／正长度重合，不去重或修复。两端口及原属性、来源与诊断边界见[节点提取契约](../../../docs/spec/workflow/矢量算子契约.md#节点提取契约)。

`raster_focal_statistics`通过`window_type=circle_metres`及显式正`radius_metres`支持米制圆窗，按实际中心距离精确纳入边界，候选外接窗口最多5×5；九种非加权方法共用原halo／归约／输出。仅北向无旋转、已知线性单位二维投影，允许矩形像元，半径与window_size互斥；默认square_pixels保持原十二方法、3／5窗口及原结果。完整数值、有效性与失败边界见栅格算子契约，Linux／Hosted分别验收。


`raster_sample_points`以同CRS二维Point命中的原始像元读取显式波段，保留全部点、属性、顺序、索引和active geometry，追加nullable Float64 `sample_value`及`sample_status=ok/outside/nodata`。支持旋转网格，以原存储坐标／仿射精确判半开像元边界，不插值或隐式应用scale／offset；原生NoData／mask／alpha先判定，不精确可表达的有效命中值明确拒绝。输出沿现有GeoDataFrame／GeoPackage保存，不新增权限、配置或Hosted脚本；详见[点位栅格采样契约](../../../docs/spec/workflow/栅格算子契约.md#点位栅格采样)。

`raster_sample_points`沿同一入口显式支持`method=cell/bilinear`，默认cell取所在像元原值；bilinear在原仿射行列坐标按像元中心插值，只有正权重支持参与，全部有效才计算，不补边／外推或重新归一化。精确中心只需一格有效；范围外仍outside，范围内支持缺失为nodata，保留全部点、原属性与几何及nullable值／状态字段。原生NoData先判断，整数沿原Float64精确准入，插值精确累计最后舍入，数值极限严格失败；源／旧成果及清理保持。复用原读块／GeoPackage，不增加面板入口、权限或依赖；独立Hosted另议，原cell固定证据不替代新方法。

`raster_zonal_statistics`以同一入口的`method=summary/class_frequency`区分原数值统计与分类频数，默认summary原九列、百分位及加权结果保持。分类模式输出按分区／类别升序的长表，比例以分区内有效分类数为分母；全无效分区保留null类别、0计数、null比例诊断行，空分区返回完整Schema。原生分区保持Int64，类别限精确安全整数；同编码非连通区域合并，原生NoData／mask／全部alpha先判定，不应用scale/offset，不自动对齐。复用分块读取及CSV／50行轻量预览，内存随实际分区—类别组合增长；首期与百分位／外部权重／加权标准差互斥，不新增入口、权限或容量承诺，独立Hosted另议。


栅格矢量分区统计同一入口新增`method=class_frequency`，默认summary原八项、百分位、外权重与面积模式保持。频数模式显式指定唯一非空`id_field`，按原面行序／类别升序展开原属性和完整面几何，以有效分类数为比例分母，无有效分类保留null诊断行；孔洞、覆盖、重叠独立及原生有效性保持，不与权重／百分位／面积模式混用。

`raster_sample_lines`按显式米制间距沿二维LineString生成里程Point，保留起终点、源编号与全部业务属性，再调用唯一点位取样核心提供cell／bilinear值及outside／nodata状态。要求同一已知线性单位二维投影CRS、有效正长度及显式唯一非空来源ID；不自动投影、修复、插补或截断样本数，源值不转高程单位。两项沿既有GeoPackage保存和严格清理，Native／Linux／Hosted／UI／容量分别记账，独立Hosted另议。


## 包含与相交筛选

`contains(input_gdf, gdf_b)` 和 `intersects(input_gdf, gdf_b)` 逐 A 判断与任一 B 的精确关系，仅返回命中的 A 原始行，完整保留几何、属性、类型、索引及行序，不新增或覆盖同名布尔字段，不按匹配数复制 A，不附加 B 属性或要求来源 ID。包含不等于 covers/contains_properly；相交含共点共边。有效有限二维混合几何及空批沿[包含与相交筛选契约](../../../docs/spec/workflow/矢量算子契约.md#包含与相交筛选契约)准入；相同已知二维地理或投影 CRS 均可，地理坐标仍为平面判断，不隐式重投影。空间索引仅选候选，错误接入既有闭合参数诊断。


## 泰森多边形

`voronoi(input_gdf, mask_gdf, id_field)` 必须显式提供源点、覆盖面范围及唯一非空来源标识，仅接受同已知投影CRS的有限有效二维Point和面范围。重复原始XY整批拒绝，不自动去重；单点覆盖整个范围，共线点合法，范围外点也参与竞争。先按原点精确绑定未裁剪分区，再裁覆盖范围，逐源最多一条完整Polygon/MultiPolygon，保留来源和全部业务属性、活动几何名及CRS，不复制范围属性。仅线点接触不输出，空批保Schema，不设置面积阈值；旧无来源单参路线删除。详见[泰森多边形契约](../../../docs/spec/workflow/矢量算子契约.md#泰森多边形契约)。

## 普通与多距缓冲统一

`buffer` 与 `batch_buffer` 统一接收已知投影 CRS 的有效非空有限二维几何，距离沿原单位，不默认为米，不自动投影或修复。距离可正、零、负；resolution 是每四分之一圆的正整数线段数，默认16。普通缓冲保原索引/行数及全部属性，空面仍保行，不新增 distance 字段；多距按数组顺序展开，保重复距离，新增 float64 distance 并保留原有重名保护。两者共用一次源准入及同一几何内核，不自动融合。标量距离、数组、精度、源和 CRS 沿现有 v3 参数诊断闭合登记，详见[统一契约](../../../docs/spec/workflow/矢量算子契约.md#普通缓冲与多距缓冲统一契约)。

简化几何的 preserve_topology 发现类型为 boolean、默认 True，与实际函数一致；仅保单个几何拓扑。量测仍沿原平面坐标单位：面积为单位平方，长度为单位，bounds 为四个坐标列；返回保源几何的 GeoDataFrame 副本，不自动换算米制。简化准入见下文统一契约；量测的几何/CRS/空值政策保持既有范围。

简化几何 `simplify` 的严格准入、原单位非负容差、默认单几何保拓扑、零容差可去共线顶点及空结果保行遵循[矢量算子契约](../../../docs/spec/workflow/矢量算子契约.md#简化几何统一契约)。仅检查输入，不自动修复、投影或维持跨要素共享边；参数诊断与计算失败分开。

`convex_hull`（凸包）及`minimum_rotated_rectangle`（最小外接矩形）逐源保行，合法单点/同位点保Point、精确共线保LineString、非共线输出面；单行多部件共同计算，跨行不隐式汇总。唯一实际几何、有效非空有限XY与已知投影准入、属性/index/CRS保护及闭合诊断遵循[统一契约](../../../docs/spec/workflow/矢量算子契约.md#凸包与最小外接矩形统一契约)。`envelope`按[轴对齐契约](../../../docs/spec/workflow/矢量算子契约.md#轴对齐外包矩形统一契约)取逐源全部件极值：同位Point、单轴同位LineString、双轴跨度Polygon；统一已知投影/有限有效XY准入，不扩宽或自动修复。

`centroid`（计算质心）逐源保行，沿投影平面原单位计算原生权重：多点等权、线按原段长、面按扣孔后的面积；混合集合只计最高维度，重复贡献保留。面外/孔内/线外Point正常成功，不当作沿线中点、中心线或面内代表点。唯一实际几何/有效非空有限XY/已知投影、属性与index保护及闭合诊断见[统一契约](../../../docs/spec/workflow/矢量算子契约.md#计算质心统一契约)。不新增weight参数或batch_centroid入口，不自动修复/投影/融合。

`representative_point`（面内点）只接投影CRS下有效非空有限XY Polygon/MultiPolygon，逐源取最大净面积部件，精确等面积原部件顺序；原生求点后必须严格位于选中部件和原源内部，否则整批计算失败，不挪点/换次优。保属性类型/null/index/CRS及实际几何列，唯一Point图层输出；无数值参数或point_on_surface别名。详见[面内点契约](../../../docs/spec/workflow/矢量算子契约.md#面内点统一契约)。

`get_area`、`get_length`及`split_by_area`遵循[统一量测契约](../../../docs/spec/workflow/矢量算子契约.md#面积长度与面积分流统一契约)：已知投影原单位、有效有限XY，NULL拒绝、EMPTY0保行；面积扣孔洞、周长计全部环，集合/重复部件按原生贡献累加不融合。普通area/length列更新，活动几何重名报错；面积分流复用唯一面积计算，有限非负threshold，large严格大于、small小于等于，每源恰入一端口。

`distance_to`按[距离契约](../../../docs/spec/workflow/矢量算子契约.md#distance_to-活动几何列)逐源计算到目标整体的最近平面距离：双方有效非NULL/整体非EMPTY有限XY及相同已知投影，空目标拒绝，空源仍验全部输入后返回空结果；原单位、源字段（包括已有非活动几何列）及distance更新/几何重名规则保留，不输出最近目标ID或边界距离，不自动修复/投影或补无效距离。

获取边界框 `get_bounds` 只追加原XY四极值，可用已知二维地理/投影CRS；NULL拒绝，合法EMPTY四值为空，活动几何与minx/miny/maxx/maxy重名拒绝，具体以[矢量算子契约](../../../docs/spec/workflow/矢量算子契约.md#获取边界框统一契约)为准。

## 多部拆分

`split_multipart(input_gdf,id_field)`按源行/原部件顺序生成单部件，完整保留来源属性并追加source_id/part_no；单部件保一行，面孔洞不拆出，重复部件不去重。GeometryCollection、NULL/EMPTY及固定字段重名明确拒绝，已知二维地理/投影CRS沿原坐标不变。边界以[多部拆分契约](../../../docs/spec/workflow/矢量算子契约.md#多部拆分契约)为准。

## 顶点提取

`extract_vertices(input_gdf,id_field)`逐原始坐标位置输出Point，保闭环首尾、重复坐标/部件/孔洞及全部来源属性，追加source_id/part_no/ring_no/vertex_no/vertex_id；不去重、不补交点或计算网络度数。部件和源准入复用多部拆分，原XY/CRS不变；GeometryCollection/NULL/EMPTY及固定字段重名整批拒绝。完整规则见[顶点提取契约](../../../docs/spec/workflow/矢量算子契约.md#顶点提取契约)。
