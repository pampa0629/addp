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

`raster_to_cog` 是唯一通用 COG 转换入口，支持受控 direct 调用；其余通用栅格算子在工作流中运行。`raster_operators.py` 的 `build_raster_mosaic` 继续服务 Manager 目录型业务数据集，两者复用同一 COG 编码实现。

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

分类由 Runtime 的 `OperatorCategory` 声明。工作流面板只显示支持 `workflow` 的 86 个算子：通用处理 18 个、矢量处理 33 个、栅格处理 35 个。所有分组名称为四个字，`category` 为末级组名，`category_path` 为完整两级目录；同级顺序由 Runtime 分类声明决定。

| 一级分组 | 二级分组 | 数量 | 算子 |
|---|---|---:|---|
| 通用处理 | 数据读写 | 2 | `load`、`save` |
| 通用处理 | 字段处理 | 5 | `add_field`、`calculate_field`、`rename_fields`、`drop_fields`、`type_cast` |
| 通用处理 | 数据清洗 | 2 | `fill_null`、`drop_duplicates` |
| 通用处理 | 数据筛选 | 5 | `filter_by_attribute`、`sort_by_field`、`select_top_n`、`sample`、`random_split` |
| 通用处理 | 数值统计 | 4 | `normalize_field`、`encode_categorical`、`bin_field`、`aggregate` |
| 矢量处理 | 几何处理 | 16 | `buffer`、`centroid`、`simplify`、`convex_hull`、`envelope`、`voronoi`、`batch_buffer`、`batch_centroid`、`dissolve`、`polygonize`、`check_geometry`、`repair_geometry`、`snap_geometry`、`check_polygon_overlap`、`check_polygon_gap`、`check_dangling_points` |
| 矢量处理 | 叠加分析 | 4 | `intersection`、`union`、`difference`、`clip` |
| 矢量处理 | 空间关系 | 3 | `contains`、`intersects`、`distance_to` |
| 矢量处理 | 几何量测 | 3 | `get_area`、`get_length`、`get_bounds` |
| 矢量处理 | 要素筛选 | 3 | `filter_by_geometry_type`、`drop_null_geometry`、`split_by_area` |
| 矢量处理 | 坐标转换 | 1 | `vector_reproject` |
| 矢量处理 | 格式转换 | 2 | `load_from_wkt`、`export_to_wkt` |
| 矢量处理 | 成果生成 | 1 | `vector_to_pmtiles` |
| 栅格处理 | 数据读写 | 3 | `raster_load`、`raster_save`、`raster_save_tiles` |
| 栅格处理 | 网格处理 | 3 | `raster_reproject`、`raster_resample`、`raster_align` |
| 栅格处理 | 范围处理 | 8 | `raster_clip`、`raster_mosaic`、`raster_update`、`raster_tile`、`raster_footprint`、`raster_polygonize`、`vector_rasterize`、`raster_contour` |
| 栅格处理 | 波段处理 | 2 | `raster_stack`、`raster_select_bands` |
| 栅格处理 | 像元计算 | 10 | `raster_band_math`、`raster_reclassify`、`raster_proximity`、`raster_nibble`、`raster_boundary_clean`、`raster_thin`、`raster_aggregate`、`raster_slope`、`raster_aspect`、`raster_hillshade` |
| 栅格处理 | 栅格统计 | 6 | `raster_statistics`、`raster_zonal_statistics`、`raster_focal_statistics`、`raster_vector_zonal_statistics`、`raster_recommend_band_combination`、`raster_histogram` |
| 栅格处理 | 栅格检查 | 2 | `raster_info`、`validate_cog` |
| 栅格处理 | 成果生成 | 3 | `raster_to_cog`、`raster_build_overviews`、`build_raster_mosaic` |

`crs_to_projjson` 和 GDAL vector dataset 批协议算子仅供受控 direct 调用，不进入工作流面板计数。分类调整不改变算子身份、参数、端口和已有工作流定义。

## 拓扑构面

`polygonize` 从线网提取闭合面，面结果 `result` 与余线诊断 `diagnostics` 可分别连接 `save`。输入限制、交点打断与去重、孔洞、诊断含义和内存边界统一见[拓扑构面契约](../../../docs/spec/workflow/矢量算子契约.md#拓扑构面契约)。

## 悬点检查

`check_dangling_points` 报告去重平面线网中度为一的端点，并逐条关联原始来源 ID。它不修改线、吸附或判断质量，连接语义与构面余线的区别见[悬点检查契约](../../../docs/spec/workflow/矢量算子契约.md#悬点检查契约)。

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
