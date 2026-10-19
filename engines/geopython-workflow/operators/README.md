# GeoPython Workflow 算子模块

GeoPython Workflow 的空间算子模块，采用模块化架构设计，底层使用 GeoPandas / Shapely 与 GDAL 提供矢量和栅格分析能力。

## 栅格计算

`raster_compute.py` 提供 32 个通用栅格算子：加载、保存、自动切片与批量保存、信息、COG 校验与转换、金字塔、重投影、重采样、裁剪、计算镶嵌、网格对齐、局部更新、波段组合、波段选择、波段计算、重分类、聚合、有效像元轮廓、分类转面、矢量栅格化、坡度、坡向、地形晕渲、等值线、整幅统计、分区统计、邻域统计、矢量分区统计、邻近距离及直方图。栅格使用专用 `raster` 端口和 execution 内部文件对象，公开资源统一使用 ResourceLocator，由 Develop 派生执行期访问计划；不能用原始路径或普通 JSON 代替栅格端口。

跨文件计算先用 `raster_align` 按参考栅格精确对齐，再用 `raster_stack` 成对追加数据波段，最后以 `raster_band_math` 计算加权求和等表达式。`raster_select_bands` 可重排波段，选择三个波段并设置 `color_model=rgb` 可生成 RGB 通道标记；不自动选择最佳波段或拉伸像元。三者统一将源 mask、NoData 和 alpha 覆盖转换为各数据波段独立的 NaN，不保留透明度大小。

`raster_mosaic(input_rasters, target_crs, resolution, overlap)` 将至少两个有序 raster 输入在共同网格中完成一次镶嵌；`first/last` 按数组顺序决定覆盖优先级。源逐一通过既有加载节点授权，电脑文件先经 Manager 上传及扫描登记，不接受任意本地路径或自动展开目录型数据集。显式源 mask 在分块有效性计算后转为各波段 NoData，避免 GDAL 同时遇到 mask 和 NoData 时忽略 mask；原始源保持不变。旧双输入参数不再接受。

`raster_tile(input_raster, tile_size=[宽,高])` 划分整幅源栅格，输出当前执行内的 `raster_tiles` 集合，交给 `raster_save_tiles` 在目标子目录中逐片保存。按左上到右下的行优先顺序命名为 `r<像素行偏移>_c<像素列偏移>.tif`，边缘保留实际宽高，不补齐、不重采样；保留源 dtype、CRS、仿射网格、NoData、mask、alpha 和色表。每次只物化与编码一片，随后清除该片中间文件；源快照和逐片摘要保留至执行结束，摘要内存随片数增长。`create` 要求目标范围为空，`replace` 仅替换本次同名切片，不删除其他文件，目标需要读和写权限。失败保留确认写入的切片并使整批失败；写入调用报错的当前片标记 `publication=unknown`，不能计为已确认成果。名称“栅格切片”不占用 AI 语义分割、实例分割或地图瓦片的概念。

`raster_update(input_raster, update_raster)` 保持基底网格，逐数据波段用更新源有效像元覆盖，更新源无效时保留基底原值，二者均无效时输出 NaN。输入须同宽高、同六参数仿射网格、等价 CRS 和等数据波段数；先用 `raster_align` 对齐、用 `raster_select_bands` 重排，多个来源通过节点顺序串联，后一次有效更新优先。结果为新的 Float64 数据波段，不改写源，不保留 alpha 大小或颜色模型；合法零和部分透明有效值按原数值更新。只支持 workflow，输出继续由 `raster_save` 持久化。

`raster_reclassify` 对 `band`（默认 1）应用非空 JSON 规则数组，例如 `[{"min":0,"max":10,"class":1},{"value":10,"class":2}]`。区间左闭右开，`null` 端点表示无界，规则重叠或重复时拒绝执行。未命中的有效像元默认设为 NaN，可设置 `unmatched=keep` 保留原值；源无效像元始终无效。输出同网格 Float64 单波段，不复制分类色表，不将 alpha 当作分类数据。无需空间定位，保留源定位缺失事实；在现有参数面板中填写 JSON 数组即可。

`raster_proximity(input_raster, band, target_values)` 计算每个有效像元中心到最近有效目标分类像元中心的平面欧氏距离，输出同网格、同 CRS 的米制 Float64 单波段。显式选择不超过 32 位的整数数据波段及非空、不重复的目标整数数组，允许合法零，使用原始分类值。源须为已知线性单位的二维投影、北向无旋转且方形像元；无效像元保留 NaN，距离可跨越无效区域，没有有效目标则失败。通过一维抛物线下包络分离计算精确距离，私有文件承载整幅中间数据，内存随行列长度及固定块增长；不承诺任意规模容量。复用原 COG 保存，不含测地、成本或路径计算。

`raster_aggregate` 使用必填 `factors=[列倍数,行倍数]` 和 `method=sum/mean/min/max`，按整数像元组逐数据波段计算。忽略无效像元，全无效组输出 NaN；保留边缘不足整块的组，按已有像元计算。有定位时按倍数缩放仿射列、行向量，因此边缘像元的完整空间大小可能使输出范围超出源右侧或底侧。保留定位缺失事实，不插值、不自动添加 alpha 或颜色模型。读取和输出累积均分块，均值通过两遍扫描避免直接求和溢出及微小值直接除以数量下溢。

`raster_clip` 的 `mode=inside` 默认保留区域内数据，`outside` 保留源完整网格、将指定区域内设为 NaN，保留区域外和多边形孔洞中的原有效像元。两种模式均使用互斥的 bbox/GeoJSON 面和明确的边界 CRS；区域外模式不插值，保留旋转，允许不相交或完全覆盖。区域外结果含 Float64 数据波段和 255/0 alpha，独立源无效值继续无效，部分透明的有效源值不缩放；不保留源透明度大小、色表或颜色模型。沿用同一个算子、raster 端口和受控保存节点，不修改源文件。

`raster_footprint` 提取真实有效像元轮廓。省略 `bands` 使用所有数据波段，`validity=any` 默认取并集，可选择 `all` 交集；排除 NoData、mask、非有限值和透明区域，保留合法零、部分透明有效数据。源须有 CRS 与可逆仿射网格；按四邻接、完整像元边界保留孔洞和多块面，输出源 CRS 的 GeoDataFrame，非空一条 MultiPolygon，全无效为空批。源逐块逐波段生成文件 Byte mask，再文件矢量化；不填孔、不简化、不改写源，不输出临时路径。读取分块不代表轮廓几何规模固定。

`raster_polygonize(input_raster, band, connectivity=4)` 按必选数据波段的相同整数分类值提取连通区域。四邻接或八邻接由参数选择，输出每区域一行 Int64 `class_value` 及有效二维 MultiPolygon；保留源 CRS、像元边界、旋转和孔洞，不按分类值跨区域合并。复用统一 NoData／mask／alpha 有效性，保留合法零和负分类值；原整数波段保留精度，Float64 重分类的精确整数可参与，连续小数或超范围值明确拒绝。结果接既有矢量 `save`，没有新保存或扫描入口。分块读取不代表几何结果固定内存；它与线网 `polygonize`、有效轮廓和 AI 分割不同。

`vector_rasterize(input_gdf, reference_raster, field=None, burn_value=None, all_touched=False)` 将有效二维面按数值字段或常量（严格二选一）烧写到同 CRS 的参考网格。输出单波段 Float64／NaN NoData，整数限制为 ±(2^53−1)，按当前行位置顺序后者覆盖；参考只提供 CRS、宽高与完整仿射变换，保留旋转，不继承像元值或有效性。默认 GDAL 中心覆盖，可选所有触及像元；保留孔洞，拒绝缺失／空／无效或 Z/M 几何及非有限数值，空批输出全 NoData。复用 `raster_save` 与 COG，不隐式重投影或支持点线、插值及完整 Int64 分类码往返。

`raster_to_cog` 是唯一通用 COG 转换入口，支持受控 direct 调用；其余通用栅格算子在工作流中运行。`raster_operators.py` 的 `build_raster_mosaic` 继续服务 Manager 目录型业务数据集，两者复用同一 COG 编码实现。算子契约与使用边界见 [工作流规范](../../../docs/spec/addp工作流计算引擎接口规范.md) 和 [栅格计算与成果契约规范](../../../docs/spec/addp栅格计算与成果契约规范.md)。

运行 `make test-geopython-workflow` 验证真实 GDAL 栅格、既有矢量、多端口及 API 契约。

`raster_zonal_statistics(input_raster, zones_raster, band, zones_band)` 显式选择同网格数值与整数分区波段，按分类值合并非连通区域，输出 `zone_value`、总／有效／无效像元数和 sum/min/max/mean/总体标准差普通表。保留零及负分类；分区无效像元排除，数值全无效的分类保留 null。原生分类保留 Int64，浮点分类仅允许安全范围内的精确整数；最多 512×512 分块，内存随分类数增长。DAG 内完整表可接既有 `save`，公开响应仅含 schema、行数与最多 50 行预览。无隐式对齐、矢量分区、面积加权或分位数；详见[工作流规范](../../../docs/spec/addp工作流计算引擎接口规范.md)。

`raster_focal_statistics` 显式选择数据波段、3×3／5×5 像元窗口及 sum/mean/min/max/总体标准差，输出同网格单波段 Float64。中心无效保留 NaN，忽略无效邻居，图像边缘只统计实际有效像元；带邻域分块读取并保留旋转和定位缺失事实，沿既有 COG 保存。首期不含权重、距离半径、分位数或分类众数滤波；完整规则见[工作流栅格契约](../../../docs/spec/addp工作流计算引擎接口规范.md#46-栅格计算契约)。

`raster_vector_zonal_statistics` 按每个有效二维面独立统计同 CRS 栅格的所选数据波段，重叠像元分别计入各面。默认像元中心覆盖，可选全部触及；保留面、属性、行序、索引和 active geometry，追加三项计数与 sum/min/max/mean/总体标准差，默认 `zonal_` 前缀可配置、列名冲突报错。空覆盖或全无效数值保留原面并输出 null 统计；沿既有矢量保存，不自动重投影、修复或面积加权。Native 与真实跨模块验收分别记账。

## 目录结构

```
operators/
├── base.py                     # Pydantic 基础模型和工具函数
├── __init__.py                 # 统一导出接口
├── io_operators.py             # 数据 I/O
├── geometric_operators.py      # 几何处理
├── spatial_relations.py        # 空间关系
├── properties_operators.py     # 几何属性
├── format_operators.py         # 格式转换
└── data_operations.py          # 数据操作
```

## 算子分类

分类由 Runtime 的 `OperatorCategory` 声明。工作流面板只显示支持 `workflow` 的 81 个算子：通用处理 18 个、矢量处理 30 个、栅格处理 33 个。所有分组名称为四个字，`category` 为末级组名，`category_path` 为完整两级目录；同级顺序由 Runtime 分类声明决定。

| 一级分组 | 二级分组 | 数量 | 算子 |
|---|---|---:|---|
| 通用处理 | 数据读写 | 2 | `load`、`save` |
| 通用处理 | 字段处理 | 5 | `add_field`、`calculate_field`、`rename_fields`、`drop_fields`、`type_cast` |
| 通用处理 | 数据清洗 | 2 | `fill_null`、`drop_duplicates` |
| 通用处理 | 数据筛选 | 5 | `filter_by_attribute`、`sort_by_field`、`select_top_n`、`sample`、`random_split` |
| 通用处理 | 数值统计 | 4 | `normalize_field`、`encode_categorical`、`bin_field`、`aggregate` |
| 矢量处理 | 几何处理 | 13 | `buffer`、`centroid`、`simplify`、`convex_hull`、`envelope`、`voronoi`、`batch_buffer`、`batch_centroid`、`dissolve`、`polygonize`、`check_geometry`、`repair_geometry`、`snap_geometry` |
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
| 栅格处理 | 像元计算 | 7 | `raster_band_math`、`raster_reclassify`、`raster_proximity`、`raster_aggregate`、`raster_slope`、`raster_aspect`、`raster_hillshade` |
| 栅格处理 | 栅格统计 | 5 | `raster_statistics`、`raster_zonal_statistics`、`raster_focal_statistics`、`raster_vector_zonal_statistics`、`raster_histogram` |
| 栅格处理 | 栅格检查 | 2 | `raster_info`、`validate_cog` |
| 栅格处理 | 成果生成 | 3 | `raster_to_cog`、`raster_build_overviews`、`build_raster_mosaic` |

`crs_to_projjson` 和 GDAL vector dataset 批协议算子仅供受控 direct 调用，不进入工作流面板计数。分类调整不改变算子身份、参数、端口和已有工作流定义。

## 拓扑构面

`polygonize(input_gdf)` 接受二维 LineString / MultiLineString 线网，先按真实交点打断、去除重合段，再提取闭合区域。默认 `result` 端口每行一个 Polygon，仅含几何；`diagnostics` 端口含 `issue_type + geometry`，区分 `dangle`、`cut_edge`、`invalid_ring`。两端口均保留源 CRS，可分别连接 `save`；空批和无闭合区域返回同 Schema 的空面结果。

不自动吸附、补缝、延伸或删除短线，不改写源，也不继承源线业务属性。拒绝空值、空几何、Z/M、非有限坐标及无效线。嵌套闭合区域保留孔洞及独立内面；诊断来自打断去重后的线网，不含原始线 ID，不等同完整拓扑质检。完整线网在内存中计算，不承诺固定内存或大规模容量。详细契约见[工作流规范](../../../docs/spec/addp工作流计算引擎接口规范.md#拓扑构面契约)。

## 几何检查与修复

`check_geometry(input_gdf, valid_field="geometry_valid", reason_field="geometry_reason")` 保留全部原要素和属性，追加平面有效性和 GEOS 原因；空几何有效，缺失值无效。字段名可配置，冲突明确失败，不覆盖业务属性。两算子仅接受有限二维 XY，拒绝 Z/M 和非有限坐标，不静默降维。

`repair_geometry(input_gdf)` 固定使用 `make_valid(method="linework", keep_collapsed=True)`，仅修复无效几何，保留合法、空及缺失几何。要求有限二维 XY；不丢弃修复产生的线点或 GeometryCollection、不拆行，保留源属性、索引、active geometry 名和 CRS。缺失几何不会被凭空补造，可接现有过滤算子；纯面目标须显式处理混合类型。复查请设置新的结果字段名，保留修复前诊断。

两者均在“矢量处理 / 几何处理”中，仅支持 workflow/read，输出接既有 `save`。不检查或修复跨要素重叠、缝隙，不吸附或补缝；单要素有效性与完整拓扑质量不同。详细边界见[工作流规范](../../../docs/spec/addp工作流计算引擎接口规范.md#几何检查与修复契约)。

## 参考吸附

`snap_geometry(input_gdf, reference_gdf, tolerance)` 对每个输入原始顶点按原始位置一次匹配固定参考图层的最近原有顶点，保留属性、行数、顺序、索引、active geometry、几何部件结构和 CRS，两份源数据均不改写。等距时按参考 X、再 Y 的升序决胜，与行序及空间索引遍历顺序无关。不使用已移动位置再匹配，不插点、删点或创建参考交点。空输入保留 Schema，缺失和空几何保留；空参考不移动输入。

两输入必须使用相同的已知投影 CRS，容差是有限非负数，采用该 CRS 的水平单位（可能为米或英尺）。地理坐标须先显式重投影；只接纳原始距离严格小于正容差的候选，零容差不移动。例如原点与参考点 0.5、1 米共线、容差 0.75 米时只能移动到 0.5 米。仅接受有限二维 XY 和有效几何，无效输出整批失败，不自动修复或回退。不吸向参考线段任意位置，不提供单图层相互吸附或完整补缝；参考索引和坐标驻内存，不承诺固定容量。位于“矢量处理 / 几何处理”，只支持 workflow/read，保存另接既有 `save`；[完整契约](../../../docs/spec/addp工作流计算引擎接口规范.md#参考吸附契约)。

## 使用方式

### 基本使用

```python
from operators import OPERATORS, list_operators, get_operator

# 获取所有算子元数据
operators_list = list_operators()

# 获取特定算子函数
buffer_func = get_operator('buffer')

# 执行算子
result = buffer_func(gdf, distance=100, resolution=16)
```

### 在工作流中使用

```python
from workflow_engine import execute_workflow

workflow = {
    "tasks": [
        {
            "id": "load_data",
            "operator": "load",
            "params": {
                "connection_info": {"engine_type": "postgresql"},
                "schema": "public",
                "table": "cities"
            }
        },
        {
            "id": "buffer_analysis",
            "operator": "buffer",
            "params": {
                "input_gdf": {"$ref": "load_data"},
                "distance": 500
            },
            "depends_on": ["load_data"]
        }
    ]
}

result = execute_workflow(workflow)
```

## 新增算子开发指南

### 1. 选择合适的模块文件

根据算子功能选择对应的文件：
- 数据 I/O → `io_operators.py`
- 几何处理 → `geometric_operators.py`
- 空间关系 → `spatial_relations.py`
- 几何属性 → `properties_operators.py`
- 格式转换 → `format_operators.py`
- 数据操作 → `data_operations.py`

### 2. 实现算子函数

```python
def my_operator(input_gdf: gpd.GeoDataFrame, param1: float) -> gpd.GeoDataFrame:
    """
    算子功能说明

    Args:
        input_gdf: 输入 GeoDataFrame
        param1: 参数说明

    Returns:
        处理后的 GeoDataFrame
    """
    # 实现代码
    result = input_gdf.copy()
    # ... 处理逻辑
    return result
```

### 3. 定义元数据

```python
from .base import OperatorMetadata, OperatorParam, OperatorCategory, register_operator

MY_OPERATOR_METADATA = OperatorMetadata(
    name="my_operator",
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="简短描述",
    brief_description="一句话说明，常用于XX场景",

    overview="详细的功能概述，50-100字",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="param1",
            type="param",
            data_type="float",
            required=True,
            description="参数说明"
        )
    ],

    use_cases=[
        "场景1: 具体数字和业务背景",
        "场景2: ...",
        "场景3: ...",
        "场景4: ..."
    ],

    notes=[
        "注意事项1",
        "优化建议2",
        "常见问题3",
        "最佳实践4"
    ],

    workflow_example={
        'id': 'example_node',
        'operator': 'my_operator',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'param1': 100
        },
        'depends_on': ['load_data']
    }
)
```

### 4. 注册算子

```python
OPERATORS = dict([
    register_operator(MY_OPERATOR_METADATA, my_operator),
    # ... 其他算子
])
```

### 5. 更新 __init__.py

在 `operators/__init__.py` 中添加导入和导出：

```python
# 导入算子实现
from .your_module import my_operator

# 在 __all__ 中添加
__all__ = [
    # ... 其他导出
    'my_operator',
]
```

## 元数据规范

### OperatorParam 的 type 字段

- `"input"` - 输入数据（通常是 GeoDataFrame）
- `"output"` - 输出数据
- `"param"` - 参数（数值、字符串等）

### data_type 字段

支持的数据类型：
- `"GeoDataFrame"` - 地理数据框
- `"float"` - 浮点数
- `"int"` - 整数
- `"str"` - 字符串
- `"bool"` - 布尔值
- `"list[float]"` - 浮点数列表
- `"list[str]"` - 字符串列表
- `"list[dict]"` - 字典列表

### OperatorCategory 枚举

枚举值为完整的两级分类元组，例如：

- `COMMON_IO`：`("通用处理", "数据读写")`。
- `VECTOR_GEOMETRY`：`("矢量处理", "几何处理")`。
- `RASTER_IO`：`("栅格处理", "数据读写")`。

注册时选择唯一枚举，序列化时由该枚举同时生成末级 `category` 和完整 `category_path`。完整分类与算子归属见前文“算子分类”，不再维护旧的平铺分类表。

## 技术特性

### Pydantic 类型安全

使用 Pydantic BaseModel 提供运行时类型验证：

```python
# 自动验证参数类型
metadata = OperatorMetadata(
    name="buffer",
    params=[
        OperatorParam(name="distance", type="param", data_type="float", ...)
    ]
)

# 如果类型错误，会抛出 ValidationError
```

### 运行时注册格式

通过 `to_runtime_dict()` 方法生成运行时算子注册结构，并由 `list_operators()` 转换为 `addp.workflow/v1` 的算子元数据：

```python
runtime_dict = metadata.to_runtime_dict()
```

### 多输出端口支持

对于有多个输出的算子（如 `split_by_area`），使用 `output_ports`：

```python
from .base import OutputPort

metadata = OperatorMetadata(
    # ... 其他字段
    output_ports=[
        OutputPort(
            name="larger",
            type="geodataframe",
            description="面积大于阈值的几何",
            is_default=True
        ),
        OutputPort(
            name="smaller",
            type="geodataframe",
            description="面积小于阈值的几何",
            is_default=False
        )
    ]
)
```

## 测试

### 单元测试

在仓库根目录使用 `make test-geopython-workflow` 验证本 Runtime；完整 owner 门禁为 `make test-module MODULE=engines`。下方代码仅说明如何编写用例，不替代标准入口。

```python
# 测试算子功能
def test_buffer():
    import geopandas as gpd
    from operators import get_operator

    # 创建测试数据
    gdf = gpd.GeoDataFrame(...)

    # 执行算子
    buffer_func = get_operator('buffer')
    result = buffer_func(gdf, distance=100)

    # 验证结果
    assert result is not None
    assert len(result) == len(gdf)
```

### 导入测试

```python
from operators import OPERATORS, list_operators

# 验证注册表非空，不固定随算子扩展变化的数量
assert OPERATORS

# 验证公开元数据与当前注册表一致
operators_list = list_operators()
assert {op['name'] for op in operators_list} == set(OPERATORS)
```

## 故障排查

### 导入错误

```bash
# 检查 Python 语法
python -m py_compile operators/*.py

# 测试导入
python -c "from operators import OPERATORS; print(len(OPERATORS))"
```

### API 返回错误

以下地址使用 GeoPython 的首选端口；实际地址以标准启动输出及 `.dev-state/ports.env` 为准。

```bash
# 启动服务
python api_server.py

# 测试端点
curl http://localhost:8099/api/operators
```

### 元数据不完整

检查 Pydantic 模型是否包含所有必需字段：
- name, category, description, brief_description
- overview, params, use_cases, notes, workflow_example

## 重构历史

模块化重构将算子实现和元数据放在同一职责模块中，通过分类 `OPERATORS` 与统一公开出口聚合。新增算子后，以当前注册表与 `GET /api/operators` 公开结果的对应关系，验证元数据能被 `WorkflowRuntimeProvider` 动态发现。

## 参考资料

- [Pydantic 文档](https://docs.pydantic.dev/)
- [GeoPandas 文档](https://geopandas.org/)
- [Shapely 文档](https://shapely.readthedocs.io/)
- [ADDP 开发原则](../../../docs/spec/addp开发原则.md)
