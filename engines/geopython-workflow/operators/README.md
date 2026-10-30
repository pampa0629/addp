# GeoPython Workflow 算子模块

GeoPython Workflow 的空间算子模块，采用模块化架构设计，底层使用 GeoPandas / Shapely 与 GDAL 提供矢量和栅格分析能力。

## 栅格计算

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
| [gdal_vector_dataset.py](gdal_vector_dataset.py) | GDAL 矢量数据集批协议 |

## 算子分类

`load` 对空间文件、Parquet 与 Feather 按源 Schema 保留可空整型与布尔型，避免空值导致整型提升为浮点并损失精度；统一使用 Arrow→pandas 的选择性类型映射，其他字段不按取值重新推断。边界与验证见[文件字段类型契约](../../../docs/spec/workflow/矢量算子契约.md#load-文件字段类型契约)。

分类由 Runtime 的 `OperatorCategory` 声明。工作流面板只显示支持 `workflow` 的算子，实际数量以当前注册表为准。所有分组名称为四个字，`category` 为末级组名，`category_path` 为完整两级目录；同级顺序由 Runtime 分类声明决定。

| 一级分组 | 二级分组 | 数量 | 算子 |
|---|---|---:|---|
| 通用处理 | 数据读写 | 2 | `load`、`save` |
| 通用处理 | 字段处理 | 5 | `add_field`、`calculate_field`、`rename_fields`、`drop_fields`、`type_cast` |
| 通用处理 | 数据清洗 | 2 | `fill_null`、`drop_duplicates` |
| 通用处理 | 数据筛选 | 5 | `filter_by_attribute`、`sort_by_field`、`select_top_n`、`sample`、`random_split` |
| 通用处理 | 数值统计 | 4 | `normalize_field`、`encode_categorical`、`bin_field`、`aggregate` |
| 矢量处理 | 几何构造 | 6 | `buffer`、`centroid`、`convex_hull`、`envelope`、`voronoi`、`batch_buffer` |
| 矢量处理 | 几何整理 | 7 | `simplify`、`dissolve`、`check_geometry`、`repair_geometry`、`snap_geometry`、`snap_line_endpoints`、`extend_line_endpoints` |
| 矢量处理 | 拓扑检查 | 8 | `check_polygon_overlap`、`check_polygon_gap`、`check_dangling_points`、`check_line_proximity`、`check_line_overlap`、`check_line_self_overlap`、`check_line_intersection`、`check_line_self_intersection` |
| 矢量处理 | 线网处理 | 5 | `polygonize`、`split_line_intersections`、`group_line_components`、`merge_line_chains`、`normalize_line_network` |
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
| 栅格处理 | 基础计算 | 3 | `raster_band_math`、`raster_aggregate`、`raster_proximity` |
| 栅格处理 | 分类处理 | 5 | `raster_reclassify`、`raster_nibble`、`raster_boundary_clean`、`raster_sieve`、`raster_thin` |
| 栅格处理 | 地形分析 | 10 | `raster_slope`、`raster_aspect`、`raster_hillshade`、`raster_curvature`、`raster_tpi`、`raster_tri`、`raster_roughness`、`raster_twi`、`raster_spi`、`raster_contour` |
| 栅格处理 | 水流分析 | 5 | `raster_fill_depressions`、`raster_flow_direction_d8`、`raster_flow_accumulation_d8`、`raster_flow_accumulation_weighted_d8`、`raster_flow_length_d8` |
| 栅格处理 | 汇水分析 | 4 | `raster_drainage_basins_d8`、`raster_snap_pour_points`、`raster_watershed_d8`、`raster_subcatchments_d8` |
| 栅格处理 | 河网分析 | 5 | `raster_stream_order_d8`、`raster_stream_link_d8`、`raster_stream_to_vector_d8`、`raster_distance_to_stream_d8`、`raster_hand_d8` |
| 栅格处理 | 格式转换 | 2 | `raster_polygonize`、`vector_rasterize` |
| 栅格处理 | 栅格统计 | 8 | `raster_statistics`、`raster_cut_fill`、`raster_surface_area`、`raster_zonal_statistics`、`raster_focal_statistics`、`raster_vector_zonal_statistics`、`raster_recommend_band_combination`、`raster_histogram` |
| 栅格处理 | 栅格检查 | 2 | `raster_info`、`validate_cog` |
| 栅格处理 | 成果生成 | 3 | `raster_to_cog`、`raster_build_overviews`、`build_raster_mosaic` |

`raster_inspect`、`crs_to_projjson` 和 GDAL vector dataset 批协议算子仅供受控 direct 调用，不进入工作流面板计数。分类调整不改变算子身份、参数、端口和已有工作流定义；不增加第三级目录或前端名称映射。原像元计算拆为基础计算、分类处理、地形分析、水流分析、汇水分析和河网分析；转面／栅格化归格式转换，等值线归地形分析，河网转线归河网分析。

D8流长只保留`raster_flow_length_d8`一个入口，必填`method=downstream`／`upstream_longest`分别计算下游／上游最长流长；原独立上游ID删除，旧DAG需显式改写，不保留兼容别名。两种模式的米制、中心步长、开放边界、无效覆盖和严格图校验保持。

“多距缓冲”（`batch_buffer`）把同一图层的多距离完整缓冲展开为一个图层，保留源属性和 CRS 并追加 `distance` 列，源列重名时报错；可直接接现有保存节点。删除“多图质心”（`batch_centroid`）的函数与入口，多个图层分别连接普通 `centroid`，旧 ID 不保留别名。普通缓冲和质心均已对单图层逐要素执行。“外包矩形”（`envelope`）为轴对齐包络。输入输出与验证边界见[多距缓冲与质心入口契约](../../../docs/spec/workflow/矢量算子契约.md#多距缓冲与质心入口契约)。

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

`raster_sieve`显式波段／min_pixels／4或8邻接，按原始连通斑块大小一次冻结，只合并到相邻达标供体，大小并列按较小分类／最前行列决定；无供体保留、不跨无效区、不填洞。原边界清理／蚕食结果保持。

`raster_focal_statistics`的`method=median`复用原邻域统计入口，固定3×3／5×5、包含中心、实际有效邻居排序，偶数取两中值平均；允许小数，中心无效保留、边缘不补零，沿原网格／Float64／COG。原sum／mean／min／max／stddev／mode语义保持，不新增面板入口。

同一入口的`method=percentile`必须显式指定单个`percentile`（有限实数0–100，非布尔），固定线性位置`h=(n−1)p/100`在相邻有效排序值间插值；50与median一致，0／100为端点。沿原3×3／5×5、包含中心、有效性、同网格Float64与COG主路径；其他方法不接受percentile参数，原七方法保持，不新增面板入口。

`raster_statistics`可附加单个`percentile`（0–100有限实数），使用同一固定线性插值并返回`percentile/percentile_value`，未提供时保持原统计对象字段。全量分块读取与执行内有界归并排序，不采样、不作直方图近似；全无效时百分位值为null，原生NoData先判定，随后沿Float64数值边界。临时排序沿原磁盘账本保留2GiB、成功失败均清理；空间不足／不可读或统计范围失败明确报错。仅只读对象，不生成持久化成果，详细边界见栅格算子契约。

`raster_zonal_statistics`可附加同一单个`percentile`，在原九列后追加`percentile/percentile_value`；按精确整数分区的全部有效数值独立计算，空组为null，分区全无效保留空表Schema。原统计口径、同网格双输入、普通表／CSV保存及最多50行预览保持；私有有界分块归并按分区与数值排序，复用磁盘账本及2GiB安全余量，成功失败均清理，不为每区保留全部像元或打开文件。

`raster_vector_zonal_statistics`可附加同一单个`percentile`，逐原始面全量固定线性插值，在原八个统计字段后按原前缀追加`percentile/percentile_value`，同名冲突失败。原中心／全部触及覆盖、孔洞、重叠分别计入及属性／几何／行序／索引保持；无覆盖／无有效数值保留行且值null，原生NoData先判定，逐面私有外排序后释放，沿磁盘账本保留2GiB。结果仍走原矢量保存路径，不增加面板入口、权限或自动重投影。

## 线端延伸

`extend_line_endpoints` 沿原始开口部件朝外方向，将悬点延伸到显式距离上限内首次参考交点；上限包含等号，共线取入口，同点参考来源全部保留。原顶点、方向及业务属性不变，`details`为原端点指向目标的新增段；方向不一致、不能精确连接或结果无效整批失败。无目标不变，不连锁或隐式修复。详见[线端延伸契约](../../../docs/spec/workflow/矢量算子契约.md#线端延伸契约)。
