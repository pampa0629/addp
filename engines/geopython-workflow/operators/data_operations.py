"""
数据操作算子模块

提供高级数据操作算子：裁剪、泰森多边形、按面积分割、多距缓冲
"""

import geopandas as gpd
import pandas as pd
import shapely
from typing import Dict, List, Any
from .base import (
    OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, register_operator, OutputPort
)
from .geometric_operators import (BufferInputError, _unary_frame_geometry, _prepare_projected_xy_source,
                                  _buffer_distance, _buffer_resolution, _compute_buffer, _geometry_for_output, _polygon_mask_inputs,
                                  _combined_polygon_mask, _require_valid_mask_result, _union_polygon_result)


# ==================== 算子实现 ====================

class ClipInputError(ValueError):
    """Known clipping admissions with no source values in public messages."""
    _messages = {
        'zh-cn': {
            'source': '裁剪几何的源图层须具有唯一几何列、有效非空有限二维几何及显式唯一非空来源标识。',
            'mask': '裁剪掩膜须具有唯一几何列和有效非空有限二维面或多面几何；不要求业务来源标识。',
            'crs': '裁剪几何要求两输入使用相同的已知投影坐标系及匹配的有限正水平线性单位；请先显式重投影。',
        },
        'en': {
            'source': 'Clip source requires one geometry column, valid non-empty finite XY geometries and an explicit unique non-null source identifier.',
            'mask': 'Clip mask requires one geometry column and valid non-empty finite XY Polygon or MultiPolygon geometries; no business identifier is required.',
            'crs': 'Clip requires the same known projected CRS and matching finite positive horizontal linear units; reproject explicitly first.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def clip(input_gdf: gpd.GeoDataFrame, mask_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Keep each source's complete intersection with the whole polygon range."""
    left, right = _polygon_mask_inputs(input_gdf, mask_gdf, id_field, ClipInputError)
    if left.empty or right.empty:
        return input_gdf.iloc[:0].copy().reset_index(drop=True)
    mask = _combined_polygon_mask(right)
    rows, values = [], []
    for index, geometry in enumerate(left.array):
        value = shapely.intersection(geometry, mask)
        if value.is_empty:
            continue
        _require_valid_mask_result(value)
        rows.append(index)
        values.append(value)
    result = input_gdf.iloc[rows].copy().reset_index(drop=True)
    result[left.name] = gpd.GeoSeries(values, crs=left.crs)
    return result


class VoronoiInputError(ValueError):
    """Known point, range and CRS admissions, without source values."""

    _messages = {
        'zh-cn': {
            'source': '泰森多边形的源图层须为有效非空有限二维 Point，具有唯一活动几何列、唯一列名及显式唯一非空来源标识；原始 XY 不得重复，请先手动整理点位和标识。',
            'mask': '泰森多边形须显式提供有效非空有限二维面或多面覆盖范围，且活动几何列和列名唯一；请先整理覆盖范围。',
            'crs': '泰森多边形要求源点和覆盖范围使用相同的已知投影坐标系及匹配、有限且为正的水平线性单位；请先显式重投影。',
        },
        'en': {
            'source': 'Voronoi source requires valid non-empty finite XY Points, unique columns, one active geometry column and an explicit unique non-null source identifier; original XY positions must be distinct. Prepare the station positions and identifiers explicitly.',
            'mask': 'Voronoi requires an explicit valid non-empty finite XY Polygon or MultiPolygon coverage layer with unique columns and one active geometry column; prepare the coverage first.',
            'crs': 'Voronoi requires the same known projected CRS with matching finite positive horizontal units on source points and coverage; reproject explicitly first.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def voronoi(input_gdf: gpd.GeoDataFrame, mask_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Clip exact nearest-station regions, binding original stations before clipping."""
    points, coverage = _polygon_mask_inputs(
        input_gdf, mask_gdf, id_field, VoronoiInputError, source_types=('Point',),
    )
    if len({(point.x, point.y) for point in points.array}) != len(points):
        raise VoronoiInputError('source')
    if points.empty or coverage.empty:
        return input_gdf.iloc[:0].copy().reset_index(drop=True)
    mask = _combined_polygon_mask(coverage)
    if len(points) == 1:
        rows, values = [0], [mask]
    else:
        diagram = shapely.voronoi_polygons(shapely.MultiPoint(points.array), tolerance=0.0, extend_to=mask)
        if diagram.geom_type != 'GeometryCollection' or len(diagram.geoms) != len(points):
            raise RuntimeError('voronoi calculation did not produce one region per station')
        cells = list(diagram.geoms)
        for cell in cells:
            _require_valid_mask_result(cell)
            if cell.is_empty or cell.geom_type != 'Polygon':
                raise RuntimeError('voronoi calculation produced an invalid region')
        tree = shapely.STRtree(cells)
        used, rows, values = set(), [], []
        for position, point in enumerate(points.array):
            matches = [index for index in tree.query(point) if shapely.covers(cells[index], point)]
            if len(matches) != 1 or matches[0] in used:
                raise RuntimeError('voronoi regions cannot be bound uniquely to original stations')
            cell_index = matches[0]
            used.add(cell_index)
            value = _union_polygon_result(shapely.intersection(cells[cell_index], mask))
            if value is not None:
                rows.append(position)
                values.append(value)
    result = input_gdf.iloc[rows].copy().reset_index(drop=True)
    result[points.name] = gpd.GeoSeries(values, crs=points.crs)
    return result


def split_by_area(input_gdf: gpd.GeoDataFrame, threshold: float) -> Dict[str, gpd.GeoDataFrame]:
    """
    按面积阈值分割数据（多输出算子示例）

    Args:
        input_gdf: 输入 GeoDataFrame
        threshold: 面积阈值（平方单位，取决于 CRS）

    Returns:
        Dict[str, GeoDataFrame]: 包含两个输出端口
            - "large": 面积大于阈值的要素
            - "small": 面积小于等于阈值的要素
    """
    geometry = _geometry_for_output(input_gdf, ('area',))
    result = input_gdf.copy()
    result['area'] = geometry.area

    large = result[result['area'] > threshold].copy()
    small = result[result['area'] <= threshold].copy()

    # 返回命名输出端口
    return {
        "large": large,
        "small": small
    }


class MultiBufferInputError(ValueError):
    """Multi-distance buffer admission errors localized by the execution adapter."""

    _messages = {
        'zh-cn': {
            'source': BufferInputError._messages['zh-cn']['source'],
            'crs': BufferInputError._messages['zh-cn']['crs'],
            'distance_conflict': '源图层的列名 distance 与多距缓冲输出列冲突；请先手动重命名源列，再执行多距缓冲。',
            'distances': '多距缓冲的距离列表必须是有限数值数组，不接受布尔值、字符串或空值。',
            'resolution': '多距缓冲的圆弧段数必须是正整数。',
        },
        'en': {
            'source': BufferInputError._messages['en']['source'],
            'crs': BufferInputError._messages['en']['crs'],
            'distance_conflict': 'Source column distance conflicts with the multi-distance buffer output column; '
                                 'rename the source column before running multi-distance buffer.',
            'distances': 'Multi-distance buffer distances must be an array of finite numbers; '
                         'booleans, strings and null values are rejected.',
            'resolution': 'Multi-distance buffer resolution must be a positive integer.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages[language][self.code]


def batch_buffer(input_gdf: gpd.GeoDataFrame, distances: List[float], resolution: int = 16) -> gpd.GeoDataFrame:
    """Expand full buffers into one layer, ordered by distance and original row."""
    _unary_frame_geometry(input_gdf, MultiBufferInputError)
    if 'distance' in input_gdf.columns:
        raise MultiBufferInputError('distance_conflict')
    if not isinstance(distances, list):
        raise MultiBufferInputError('distances')
    normalized = []
    for value in distances:
        try:
            normalized.append(_buffer_distance(value))
        except BufferInputError as exc:
            raise MultiBufferInputError('distances') from exc
    resolution = _buffer_resolution(resolution, MultiBufferInputError)
    geometry = _prepare_projected_xy_source(input_gdf, MultiBufferInputError)

    if not normalized or input_gdf.empty:
        result = input_gdf.iloc[:0].copy().reset_index(drop=True)
        result['distance'] = pd.Series(dtype='float64')
        return result

    buffers = []
    for distance in normalized:
        result = _compute_buffer(input_gdf, geometry, distance, resolution)
        result['distance'] = distance
        buffers.append(result)
    return pd.concat(buffers, ignore_index=True)


# ==================== 元数据定义 ====================

CLIP_METADATA = OperatorMetadata(
    name="clip", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_OVERLAY,
    description="裁剪几何", brief_description="按整体面掩膜逐源裁剪，保留完整交集、来源和业务属性",
    execution_modes=["workflow"], effects=["read"],
    overview="源图层与全部掩膜面的整体范围求交，每源最多一条非空完整结果；共边和共点保留线与点，多部件及 GeometryCollection 不拆行。只保留源属性和显式来源 ID，掩膜不要求业务标识，输入不变。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="源图层", notes="有效非空有限二维点、线、面、多部件或集合；唯一活动几何，允许空批"),
        OperatorParam(name="mask_gdf", type="input", data_type="GeoDataFrame", required=True, description="裁剪掩膜", notes="仅有效有限二维 Polygon/MultiPolygon；相同已知投影 CRS；允许空批，不要求业务来源 ID"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="源图层唯一来源标识列", notes="显式非几何唯一非空标量列，保留原名和值，不推断索引或 FID"),
    ],
    use_cases=["按研究区裁剪完整道路交集", "保留面范围及边界上的点位", "逐源提取掩膜内的多部件几何"],
    notes=[
        "全部掩膜构成整体范围，重复重叠掩膜不重复输出；孔洞内部留白、孔环边界仍参与求交",
        "保留源属性、可空类型、来源 ID、活动几何名及 CRS，重建 RangeIndex；不复制掩膜属性或增加同义 ID 列",
        "不按输入维度丢弃线或点，不自动修复、吸附、重投影、舍入或拆行；不承诺保留原顶点顺序及环方向",
        "空源或空掩膜返回保完整 Schema 的空结果；纯计算不保存扫描，保存格式须能表达实际混合几何",
    ],
    workflow_example={'id':'clip_source','operator':'clip','params':{'input_gdf':{'$ref':'load_source'},'mask_gdf':{'$ref':'load_mask'},'id_field':'source_key'},'depends_on':['load_source','load_mask']},
)

VORONOI_METADATA = OperatorMetadata(
    name="voronoi", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="泰森多边形", brief_description="按源点生成覆盖范围内的最近站点分区，保留来源和属性",
    execution_modes=["workflow"], effects=["read"],
    overview="全部二维源点参与平面最近站点分区，再裁到显式覆盖范围；范围外和孔洞内的点仍参与竞争。每源最多一条完整 Polygon/MultiPolygon，保留源点 ID、全部业务属性和源行序，不拆部件、不复制覆盖面属性。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="源点图层", notes="仅有效非空有限二维 Point；原始 XY 不得重复，允许空批，不自动去重或吸附"),
        OperatorParam(name="mask_gdf", type="input", data_type="GeoDataFrame", required=True, description="必填覆盖面范围", notes="有效非空有限二维 Polygon/MultiPolygon；全部面求并，保留孔洞和不连通范围，不要求范围业务 ID"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="源点唯一来源标识列", notes="显式唯一非空非几何标量列；保留原名、类型和值，不推断索引或存储 FID"),
    ],
    use_cases=["研究区内最近设施分区", "气象站点影响区域划分", "保留设施属性的服务范围分析"],
    notes=[
        "两输入须为相同已知投影CRS及匹配有限正水平线性单位；不按经纬度或球面距离计算，不隐式重投影",
        "单点独占完整范围，共线不同点合法；重复XY或来源ID拒绝，不聚合属性或选择第一条",
        "先按原始未裁剪分区精确绑定源点，再裁剪；不依赖原生分区顺序或按最近面猜来源",
        "只保留面部分，不输出仅接触的线点，不设最小面积阈值；空源或空范围保完整源Schema",
        "保留源活动几何名、全部业务字段及CRS，重建RangeIndex；输入不变，纯计算不保存扫描",
    ],
    workflow_example={'id':'station_regions','operator':'voronoi','params':{'input_gdf':{'$ref':'load_stations'},'mask_gdf':{'$ref':'load_coverage'},'id_field':'station_id'},'depends_on':['load_stations','load_coverage']},
)

SPLIT_BY_AREA_METADATA = OperatorMetadata(
    name="split_by_area",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_SELECTION,
    description="按面积分割",
    brief_description="按面积阈值将数据分割为大小两组,常用于数据分类和质量检查",
    execution_modes=["workflow"],
    effects=["read"],

    overview="根据面积阈值将输入要素分为两组:大于阈值的要素和小于等于阈值的要素。这是一个多输出算子示例,返回两个独立的输出端口。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的面几何数据"
        ),
        OperatorParam(
            name="threshold",
            type="param",
            data_type="float",
            required=True,
            description="面积阈值,单位与坐标系一致",
            notes="投影坐标系单位为平方米,地理坐标系单位为平方度"
        )
    ],

    use_cases=[
        "地块筛选: 分离大地块和小地块用于不同规划策略",
        "建筑物分类: 区分大型建筑和小型建筑",
        "数据质量检查: 识别面积异常的要素",
        "图斑分级: 按面积阈值对图斑进行分级统计"
    ],

    notes=[
        "读取实际活动几何列；几何列名为area时先显式重命名，不覆盖几何",
        "返回两个输出端口: large(大于阈值) 和 small(小于等于阈值)",
        "面积单位与坐标系一致,地理坐标系建议先转投影坐标系",
        "对于点和线几何,面积为0,全部归入small组",
        "多输出算子需要在工作流中正确连接不同的输出端口"
    ],

    workflow_example={
        'id': 'split_parcels',
        'operator': 'split_by_area',
        'params': {
            'input_gdf': {'$ref': 'load_parcels'},
            'threshold': 10000
        },
        'depends_on': ['load_parcels']
    },

    output_ports=[
        OutputPort(
            name="large",
            type="geodataframe",
            description="面积大于阈值的要素",
            is_default=False
        ),
        OutputPort(
            name="small",
            type="geodataframe",
            description="面积小于等于阈值的要素",
            is_default=False
        )
    ]
)



BATCH_BUFFER_METADATA = OperatorMetadata(
    name="batch_buffer",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="多距缓冲",
    brief_description="对同一图层生成多个距离的完整缓冲区,汇总为带 distance 字段的单图层",
    execution_modes=["workflow"],
    effects=["read"],

    overview="对有效有限二维且具有已知投影CRS的源图层，按距离数组顺序生成独立完整缓冲区；正零负距离导致空面时保留原行与属性，以单个GeoDataFrame追加distance列返回，不自动修复、投影或融合。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="投影坐标系的源图层", notes="有效非空有限二维几何、唯一实际活动几何及列名，允许空批；地理或未知CRS拒绝"
        ),
        OperatorParam(
            name="distances",
            type="param",
            data_type="list[float]",
            required=True,
            description="缓冲距离列表",
            notes="如 [100, 300, 500]；单位沿用源CRS,不自动换算为米。保留顺序和重复距离,允许零、负值及空数组。"
        ),
        OperatorParam(
            name="resolution",
            type="param",
            data_type="int",
            required=False,
            default=16,
            description="每四分之一圆的线段数",
            notes="正整数，默认16；值越大越平滑，也增加计算和内存消耗"
        )
    ],

    use_cases=[
        "多级影响范围: 分析设施500米、1000米、2000米三级服务范围",
        "噪音等级划分: 道路50米、100米、200米三级噪音影响区",
        "辐射范围分析: 基站200米、500米、1000米覆盖范围",
        "风险分区: 危化品仓库100米、300米、500米风险等级区"
    ],

    notes=[
        "返回一个GeoDataFrame,依距离顺序再依源行顺序展开,保留原属性、类型、活动几何列和CRS",
        "追加float64 distance列；任意源列重名时明确报错,须先在上游手动改名",
        "生成完整缓冲区而非环带,不融合不同源要素；零和负距离沿用普通缓冲语义",
        "距离沿已知投影CRS原单位，不自动换算或重投影；源缺失/空/无效及Z/M几何拒绝，空批和空距离数组仍先验证准入"
    ],

    output_ports=[OutputPort(name='default', type='geodataframe', description='带distance字段的单图层完整缓冲区', is_default=True)],

    workflow_example={
        'id': 'multi_level_buffer',
        'operator': 'batch_buffer',
        'params': {
            'input_gdf': {'$ref': 'load_facilities'},
            'distances': [500, 1000, 2000],
            'resolution': 16
        },
        'depends_on': ['load_facilities']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(CLIP_METADATA, clip),
    register_operator(VORONOI_METADATA, voronoi),
    register_operator(SPLIT_BY_AREA_METADATA, split_by_area),
    register_operator(BATCH_BUFFER_METADATA, batch_buffer),
])
