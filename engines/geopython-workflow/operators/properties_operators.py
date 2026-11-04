"""
几何属性算子模块

提供计算几何对象属性的算子：面积、长度、边界框
"""

import geopandas as gpd
import numpy as np
import pandas as pd
import shapely
from .geometric_operators import _unary_frame_geometry, _finite_xy_geometry, _require_projected_xy_crs, _finite_real_scalar
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)


# ==================== 算子实现 ====================

class MeasurementInputError(ValueError):
    """Closed planar measurement admissions; numerical faults stay execution failures."""
    _messages = {
        'zh-cn': {
            'source': '量测源须具有唯一列名和唯一活动几何列，几何须有效且为有限二维XY；NULL不允许，合法EMPTY可用并计0。请先显式整理源图层。',
            'crs': '量测要求已知投影坐标系及匹配、有限且为正的水平线性单位；长度沿原单位、面积沿原单位平方，不自动投影或换算。',
            'area': '活动几何列area与面积输出字段冲突；请先手动重命名几何列，不自动改名或覆盖几何。',
            'length': '活动几何列length与长度输出字段冲突；请先手动重命名几何列，不自动改名或覆盖几何。',
            'threshold': '面积阈值threshold必须为有限非负实数，单位为源坐标单位平方；不接受布尔值、字符串、空值、数组或非有限值。',
        },
        'en': {
            'source': 'Measurement requires unique column names, one active geometry column and valid finite XY geometries; NULL is rejected, while valid EMPTY is retained with zero. Prepare the source explicitly first.',
            'crs': 'Measurement requires a known projected CRS with matching finite positive horizontal linear units; length uses the original unit and area its square, without automatic projection or conversion.',
            'area': 'Active geometry column area conflicts with the area output field; rename the geometry column manually first, without automatic renaming or geometry replacement.',
            'length': 'Active geometry column length conflicts with the length output field; rename the geometry column manually first, without automatic renaming or geometry replacement.',
            'threshold': 'Area threshold must be a finite non-negative real scalar in squared source coordinate units; booleans, strings, nulls, arrays and non-finite values are rejected.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _measurement_threshold(value):
    number = _finite_real_scalar(value, MeasurementInputError, 'threshold')
    if number < 0:
        raise MeasurementInputError('threshold')
    return number


def _measure(input_gdf, field):
    _unary_frame_geometry(input_gdf, MeasurementInputError)
    try:
        geometry = _finite_xy_geometry(input_gdf)
        if geometry.isna().any() or not shapely.is_valid(geometry.array).all():
            raise ValueError('measurement requires valid non-missing XY geometry')
    except (ValueError, TypeError) as exc:
        raise MeasurementInputError('source') from exc
    if geometry.name == field:
        raise MeasurementInputError(field)
    try:
        _require_projected_xy_crs(geometry.crs)
    except ValueError as exc:
        raise MeasurementInputError('crs') from exc
    values = np.asarray({'area': shapely.area, 'length': shapely.length}[field](geometry.array), dtype=float)
    if (values.shape != (len(geometry),) or not np.isfinite(values).all() or (values < 0).any()
            or (values[shapely.is_empty(geometry.array)] != 0).any()):
        raise RuntimeError('measurement produced invalid or non-finite scalar results')
    result = input_gdf.copy()
    result[field] = values
    return result


def get_area(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算面积

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 副本，追加或更新 area 列，保留源几何和其他属性
    """
    return _measure(input_gdf, 'area')


def get_length(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算长度/周长

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 副本，追加或更新 length 列，保留源几何和其他属性
    """
    return _measure(input_gdf, 'length')


class BoundsInputError(ValueError):
    """Closed coordinate-bounds admissions, separate from native failures."""
    _messages = {
        'zh-cn': {
            'source': '边界框要求唯一列名及活动几何，几何须有效且为有限二维XY；NULL拒绝，合法EMPTY保行且四值为空。请先显式整理源图层，不接受Z/M。',
            'crs': '边界框要求已知二维地理或投影CRS；未知、地心、三维及复合CRS拒绝。不投影、换单位或交换XY，不计算日期变更线环绕范围。',
            'field': '活动几何列{field}与固定边界框结果列冲突；请先手动重命名几何列，不自动改名或覆盖几何。',
        },
        'en': {
            'source': 'Bounds require unique column names, an active geometry and valid finite XY geometries; NULL is rejected, while valid EMPTY rows retain four null values. Prepare the source explicitly; Z/M is rejected.',
            'crs': 'Bounds require a known two-dimensional geographic or projected CRS; unknown, geocentric, 3D and compound CRS are rejected. No projection, unit conversion, XY swapping or antimeridian wrapping is performed.',
            'field': 'Active geometry column {field} conflicts with a fixed bounds output; rename the geometry column manually first, without automatic renaming or geometry replacement.',
        },
    }

    def __init__(self, code, field=''):
        self.code, self.field = code, field
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code].format(field=self.field)


def get_bounds(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Return per-source original XY bounds, retaining EMPTY as four nulls."""
    try:
        if not isinstance(input_gdf, gpd.GeoDataFrame) or not input_gdf.columns.is_unique:
            raise ValueError('bounds require a GeoDataFrame with unique column names')
        geometry = _finite_xy_geometry(input_gdf)
        if geometry.isna().any() or not shapely.is_valid(geometry.array).all():
            raise ValueError('bounds require valid non-missing geometry')
    except (ValueError, TypeError) as exc:
        raise BoundsInputError('source') from exc
    fields = ('minx', 'miny', 'maxx', 'maxy')
    if geometry.name in fields:
        raise BoundsInputError('field', geometry.name)
    crs = geometry.crs
    if crs is None or crs.is_compound or len(crs.axis_info) != 2 or not (crs.is_geographic or crs.is_projected):
        raise BoundsInputError('crs')
    result = input_gdf.copy()
    if len(geometry) == 0:
        for field in fields:
            result[field] = pd.array([], dtype='Float64')
        return result
    values = np.asarray(shapely.bounds(geometry.array))
    if values.shape != (len(geometry), 4) or values.dtype.kind not in 'fiu':
        raise RuntimeError('bounds produced an invalid scalar matrix')
    values = values.astype(float)
    empty = shapely.is_empty(geometry.array)
    if (not np.isnan(values[empty]).all() or not np.isfinite(values[~empty]).all()
            or (values[~empty, 0] > values[~empty, 2]).any()
            or (values[~empty, 1] > values[~empty, 3]).any()):
        raise RuntimeError('bounds produced invalid or non-finite extrema')
    for i, field in enumerate(fields):
        result[field] = pd.arrays.FloatingArray(values[:, i].copy(), empty.copy())
    return result


# ==================== 元数据定义 ====================

GET_AREA_METADATA = OperatorMetadata(
    name="get_area",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_MEASUREMENT,
    description="计算面积",
    brief_description="计算面几何的面积,常用于土地统计和资源核算",
    execution_modes=["workflow"],
    effects=["read"],

    overview="逐要素计算平面面积，在原GeoDataFrame副本中追加或更新area列，保留源几何和其他属性。适用于土地利用统计、资源清单、规划指标核算等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的面几何数据"
        )
    ],

    use_cases=[
        "土地利用统计: 计算各地类的总面积",
        "规划指标核算: 统计建设用地面积是否超标",
        "生态资源清查: 计算林地、湿地面积",
        "房产测绘: 计算建筑物占地面积"
    ],

    notes=[
        "读取实际活动几何列；若几何列名为area则报错，请先显式重命名几何列",
        "面积为输入坐标单位的平方，不自动换算平方米；投影单位可为米、英尺等；不计算测地面积",
        "只接受已知投影CRS和匹配有限正水平单位；未知/地理/地心CRS拒绝，不自动投影",
        "对于点和线几何,面积为0；面扣除孔洞，多部件/集合逐部件累加，重复贡献不去重",
        "NULL拒绝、合法EMPTY计0保行；有效有限XY，拒绝Z/M；原属性、索引和CRS不变",
        "大范围区域面积计算建议使用等面积投影(如Albers)"
    ],

    workflow_example={
        'id': 'calc_landuse_area',
        'operator': 'get_area',
        'params': {
            'input_gdf': {'$ref': 'load_landuse'}
        },
        'depends_on': ['load_landuse']
    }
)


GET_LENGTH_METADATA = OperatorMetadata(
    name="get_length",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_MEASUREMENT,
    description="计算长度",
    brief_description="计算线几何的长度,常用于道路统计和网络分析",
    execution_modes=["workflow"],
    effects=["read"],

    overview="逐要素计算平面长度或周长，在原GeoDataFrame副本中追加或更新length列，保留源几何和其他属性。适用于道路统计、河流长度计算、管网清单等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的线几何数据"
        )
    ],

    use_cases=[
        "道路里程统计: 计算各级道路总长度",
        "河流长度计算: 统计流域内河流总长",
        "管网清单: 计算供水、燃气管道长度",
        "步行距离: 计算步行路径的实际长度"
    ],

    notes=[
        "读取实际活动几何列；若几何列名为length则报错，请先显式重命名几何列",
        "长度沿输入坐标单位，不自动换算米；投影单位可为米、英尺等；不计算测地长度",
        "只接受已知投影CRS和匹配有限正水平单位；未知/地理/地心CRS拒绝，不自动投影",
        "对于点几何,长度为0;对于面几何,返回外环及所有孔洞环周长；多部件/集合累加全部贡献，不融合",
        "NULL拒绝、合法EMPTY计0保行；有效有限XY，拒绝Z/M；原属性、索引和CRS不变",
        "大范围线要素长度计算建议使用等距投影"
    ],

    workflow_example={
        'id': 'calc_road_length',
        'operator': 'get_length',
        'params': {
            'input_gdf': {'$ref': 'load_roads'}
        },
        'depends_on': ['load_roads']
    }
)


GET_BOUNDS_METADATA = OperatorMetadata(
    name="get_bounds",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_MEASUREMENT,
    description="获取边界框",
    brief_description="获取几何对象的边界框坐标(minx, miny, maxx, maxy),常用于范围查询",
    execution_modes=["workflow"],
    effects=["read"],

    overview="逐要素获取轴对齐边界框，在原GeoDataFrame副本中追加或更新minx、miny、maxx、maxy四列，保留源几何和其他属性。适用于地图范围定位、数据裁剪、空间索引构建等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        )
    ],

    use_cases=[
        "地图定位: 获取要素范围用于地图缩放定位",
        "数据裁剪: 用边界框快速过滤候选数据",
        "瓦片计算: 计算要素所在的地图瓦片编号",
        "空间索引: 构建R-Tree等空间索引的边界矩形"
    ],

    notes=[
        "读取实际活动几何列；几何列名与四个输出名冲突时先显式重命名",
        "返回保留源几何和其他属性的GeoDataFrame副本，追加或更新minx、miny、maxx、maxy四列",
        "边界框是轴对齐矩形,不随几何旋转",
        "已知二维地理或投影CRS，沿原存储XY与单位，不交换轴/投影/日期变更线环绕；未知/地心/三维/复合CRS拒绝",
        "NULL拒绝，合法EMPTY保行且四个Float64结果均为空值；有效有限XY，拒绝Z/M，不以0替代空范围",
        "保留原几何、其它属性和索引；点/水平/垂直线允许对应极值相等，不扩宽或舍入",
        "点的边界框minx=maxx, miny=maxy"
    ],

    workflow_example={
        'id': 'get_feature_bounds',
        'operator': 'get_bounds',
        'params': {
            'input_gdf': {'$ref': 'load_features'}
        },
        'depends_on': ['load_features']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(GET_AREA_METADATA, get_area),
    register_operator(GET_LENGTH_METADATA, get_length),
    register_operator(GET_BOUNDS_METADATA, get_bounds),
])
