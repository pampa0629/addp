"""
空间算子实现
使用 GeoPandas 和 Shapely 实现空间算子 + I/O 算子
"""

import geopandas as gpd
from shapely.geometry import shape, mapping, Point, LineString, Polygon
from shapely.ops import unary_union
from typing import Dict, Any, List, Union
import json
import requests
import os
from sqlalchemy import create_engine


# ========================================
# I/O 算子 (2个) - 数据加载和保存
# ========================================

def load(params: Dict[str, Any]) -> gpd.GeoDataFrame:
    """
    通用数据加载算子

    接收 DataLocation 信息，根据类型加载数据：
    - source_type: "table" | "file" | "geojson" | "reference"
    - resource_id: 存储引擎资源 ID
    - 其他参数: table/path、schema/format 等

    注意：此算子不直接执行 SQL，而是根据 DataLocation 信息
    通过 System API 获取资源连接信息后加载数据
    """
    source_type = params.get('source_type')
    resource_id = params.get('resource_id')

    if source_type == 'table':
        # 1. 从 System API 获取资源连接信息
        system_url = os.getenv('SYSTEM_BACKEND_URL', 'http://localhost:8080')
        response = requests.get(f'{system_url}/api/resources/{resource_id}')

        if response.status_code != 200:
            raise ValueError(f"Failed to get resource {resource_id}: {response.text}")

        resource = response.json()

        # 2. 根据资源类型构建连接字符串
        resource_type = resource['resource_type']
        conn_info = resource['connection_info']

        table = params.get('table')
        schema = params.get('schema', 'public')

        # 3. 根据不同数据库类型加载
        if resource_type in ['postgresql', 'PostgreSQL']:
            conn_str = f"postgresql://{conn_info['user']}:{conn_info['password']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            engine = create_engine(conn_str)

            # 读取空间数据（假设几何列名为 geom）
            sql = f'SELECT * FROM {schema}.{table}'
            gdf = gpd.read_postgis(sql, engine, geom_col='geom')

        elif resource_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            password = conn_info.get('password', '')
            if password:
                conn_str = f"mysql+pymysql://{conn_info['user']}:{password}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            else:
                conn_str = f"mysql+pymysql://{conn_info['user']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"

            engine = create_engine(conn_str)

            # MySQL/Doris 空间数据读取
            sql = f'SELECT * FROM {table}'
            gdf = gpd.read_postgis(sql, engine, geom_col='geom')

        else:
            raise ValueError(f"Unsupported resource type for table: {resource_type}")

        return gdf

    elif source_type == 'file':
        # 从对象存储加载文件
        system_url = os.getenv('SYSTEM_BACKEND_URL', 'http://localhost:8080')
        response = requests.get(f'{system_url}/api/resources/{resource_id}')

        if response.status_code != 200:
            raise ValueError(f"Failed to get resource {resource_id}: {response.text}")

        resource = response.json()

        path = params.get('path')
        format_type = params.get('format', 'geojson')

        # TODO: 实现从 MinIO/S3 下载文件逻辑
        # local_file = download_from_minio(resource, path)

        # 临时实现：假设文件已在本地
        if format_type in ['geojson', 'shapefile']:
            gdf = gpd.read_file(path)
        else:
            raise ValueError(f"Unsupported format: {format_type}")

        return gdf

    elif source_type == 'geojson':
        # 直接解析 GeoJSON 对象
        geojson_obj = params.get('geojson')
        gdf = gpd.GeoDataFrame.from_features(geojson_obj['features'])
        return gdf

    elif source_type == 'reference':
        # 引用其他任务的输出（已在内存中的 GeoDataFrame）
        # 这种情况下不需要加载，直接返回引用
        raise NotImplementedError("Reference type should be handled by workflow engine")

    else:
        raise ValueError(f"Unsupported source_type: {source_type}")


def save(data: gpd.GeoDataFrame, params: Dict[str, Any]) -> Dict[str, Any]:
    """
    通用数据保存算子

    根据目标类型保存数据：
    - target_type: "table" | "file"
    - resource_id: 存储引擎资源 ID
    """
    target_type = params.get('target_type')
    resource_id = params.get('resource_id')

    if target_type == 'table':
        # 1. 从 System API 获取资源连接信息
        system_url = os.getenv('SYSTEM_BACKEND_URL', 'http://localhost:8080')
        response = requests.get(f'{system_url}/api/resources/{resource_id}')

        if response.status_code != 200:
            raise ValueError(f"Failed to get resource {resource_id}: {response.text}")

        resource = response.json()

        # 2. 构建连接
        resource_type = resource['resource_type']
        conn_info = resource['connection_info']

        table = params.get('table')
        schema = params.get('schema', 'public')
        mode = params.get('mode', 'overwrite')

        # 3. 根据数据库类型保存
        if resource_type in ['postgresql', 'PostgreSQL']:
            conn_str = f"postgresql://{conn_info['user']}:{conn_info['password']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            engine = create_engine(conn_str)

            if_exists = 'replace' if mode == 'overwrite' else 'append'
            data.to_postgis(table, engine, schema=schema, if_exists=if_exists, index=False)

        elif resource_type in ['mysql', 'MySQL', 'doris', 'Doris']:
            password = conn_info.get('password', '')
            if password:
                conn_str = f"mysql+pymysql://{conn_info['user']}:{password}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"
            else:
                conn_str = f"mysql+pymysql://{conn_info['user']}@{conn_info['host']}:{conn_info['port']}/{conn_info['database']}"

            engine = create_engine(conn_str)

            if_exists = 'replace' if mode == 'overwrite' else 'append'
            data.to_sql(table, engine, if_exists=if_exists, index=False)

        else:
            raise ValueError(f"Unsupported resource type for table: {resource_type}")

        return {
            "output_type": "table",
            "output_table": f"{schema}.{table}",
            "resource_id": resource_id,
            "row_count": len(data)
        }

    elif target_type == 'file':
        # TODO: 实现保存到对象存储逻辑
        path = params.get('path')
        format_type = params.get('format', 'geojson')

        # 临时实现：保存到本地文件
        if format_type == 'geojson':
            data.to_file(path, driver='GeoJSON')
        elif format_type == 'shapefile':
            data.to_file(path, driver='ESRI Shapefile')
        else:
            raise ValueError(f"Unsupported format: {format_type}")

        return {
            "output_type": "file",
            "output_file": path,
            "format": format_type
        }

    else:
        # 标量或其他类型
        return {
            "output_type": "scalar",
            "value": str(data)
        }


# ========================================
# 几何处理算子 (8个)
# ========================================

def buffer(input_gdf: gpd.GeoDataFrame, distance: float, resolution: int = 16) -> gpd.GeoDataFrame:
    """
    缓冲区分析

    Args:
        input_gdf: 输入 GeoDataFrame
        distance: 缓冲距离（米）
        resolution: 圆弧段数

    Returns:
        GeoDataFrame 缓冲区结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].buffer(distance, resolution=resolution)
    return result


def intersection(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    几何相交（叠加分析）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame 交集结果
    """
    return gpd.overlay(gdf_a, gdf_b, how='intersection')


def union(gdf_list: List[gpd.GeoDataFrame]) -> gpd.GeoDataFrame:
    """
    几何合并（联合多个 GeoDataFrame）

    Args:
        gdf_list: GeoDataFrame 列表

    Returns:
        GeoDataFrame 合并结果
    """
    if len(gdf_list) == 0:
        raise ValueError("gdf_list cannot be empty")

    result = gdf_list[0].copy()
    for gdf in gdf_list[1:]:
        result = gpd.overlay(result, gdf, how='union')

    return result


def centroid(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算质心

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 质心点结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].centroid
    return result


def difference(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    几何差集（A - B）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame 差集结果
    """
    return gpd.overlay(gdf_a, gdf_b, how='difference')


def simplify(input_gdf: gpd.GeoDataFrame, tolerance: float, preserve_topology: bool = True) -> gpd.GeoDataFrame:
    """
    简化几何（Douglas-Peucker 算法）

    Args:
        input_gdf: 输入 GeoDataFrame
        tolerance: 简化容差
        preserve_topology: 是否保持拓扑

    Returns:
        GeoDataFrame 简化结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].simplify(tolerance, preserve_topology=preserve_topology)
    return result


def convex_hull(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算凸包

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 凸包结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].convex_hull
    return result


def envelope(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算最小外接矩形（MBR）

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 外接矩形结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].envelope
    return result


# ========================================
# 空间关系算子 (3个)
# ========================================

def contains(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    包含关系判断（返回 A 中包含 B 的记录）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame A 中包含 B 的记录
    """
    result = gdf_a.copy()
    result['contains'] = result['geometry'].apply(
        lambda geom: any(geom.contains(g) for g in gdf_b['geometry'])
    )
    return result[result['contains']]


def intersects(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    相交关系判断（返回 A 中与 B 相交的记录）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame A 中与 B 相交的记录
    """
    result = gdf_a.copy()
    result['intersects'] = result['geometry'].apply(
        lambda geom: any(geom.intersects(g) for g in gdf_b['geometry'])
    )
    return result[result['intersects']]


def distance_to(input_gdf: gpd.GeoDataFrame, target_geom: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    距离计算（计算每个几何到目标几何的最短距离）

    Args:
        input_gdf: 输入 GeoDataFrame
        target_geom: 目标几何 GeoDataFrame

    Returns:
        GeoDataFrame 添加 distance 列
    """
    result = input_gdf.copy()

    # 计算到目标几何集合中最近点的距离
    target_union = target_geom['geometry'].unary_union
    result['distance'] = result['geometry'].apply(lambda g: g.distance(target_union))

    return result


# ========================================
# 几何属性算子 (3个)
# ========================================

def get_area(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算面积

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 添加 area 列
    """
    result = input_gdf.copy()
    result['area'] = result['geometry'].area
    return result


def get_length(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算长度/周长

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 添加 length 列
    """
    result = input_gdf.copy()
    result['length'] = result['geometry'].length
    return result


def get_bounds(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    获取边界框（minx, miny, maxx, maxy）

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 添加 bounds 列
    """
    result = input_gdf.copy()
    bounds = result['geometry'].bounds
    result['minx'] = bounds['minx']
    result['miny'] = bounds['miny']
    result['maxx'] = bounds['maxx']
    result['maxy'] = bounds['maxy']
    return result


# ========================================
# 格式转换算子 (2个)
# ========================================

def load_from_wkt(wkt_list: List[str], properties: List[Dict] = None, crs: str = "EPSG:4326") -> gpd.GeoDataFrame:
    """
    从 WKT 格式加载几何

    Args:
        wkt_list: WKT 字符串列表
        properties: 属性字典列表（可选）
        crs: 坐标系统（默认 EPSG:4326）

    Returns:
        GeoDataFrame
    """
    from shapely import wkt

    geometries = [wkt.loads(w) for w in wkt_list]

    if properties is None:
        properties = [{}] * len(geometries)

    gdf = gpd.GeoDataFrame(properties, geometry=geometries, crs=crs)
    return gdf


def export_to_wkt(input_gdf: gpd.GeoDataFrame) -> List[str]:
    """
    导出为 WKT 格式

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        WKT 字符串列表
    """
    return input_gdf['geometry'].apply(lambda g: g.wkt).tolist()


# ========================================
# 批处理算子 (2个)
# ========================================

def batch_buffer(input_gdf: gpd.GeoDataFrame, distances: List[float], resolution: int = 16) -> List[gpd.GeoDataFrame]:
    """
    批量缓冲区分析（多个距离）

    Args:
        input_gdf: 输入 GeoDataFrame
        distances: 距离列表
        resolution: 圆弧段数

    Returns:
        GeoDataFrame 列表
    """
    return [buffer(input_gdf, dist, resolution) for dist in distances]


def batch_centroid(gdf_list: List[gpd.GeoDataFrame]) -> List[gpd.GeoDataFrame]:
    """
    批量计算质心

    Args:
        gdf_list: GeoDataFrame 列表

    Returns:
        质心 GeoDataFrame 列表
    """
    return [centroid(gdf) for gdf in gdf_list]


# ========================================
# 高级算子 (3个)
# ========================================

def dissolve(input_gdf: gpd.GeoDataFrame, by: Union[str, List[str]] = None) -> gpd.GeoDataFrame:
    """
    融合（dissolve）几何，按字段分组

    Args:
        input_gdf: 输入 GeoDataFrame
        by: 分组字段（可选）

    Returns:
        GeoDataFrame 融合结果
    """
    if by is None:
        # 全部融合为一个几何
        result = gpd.GeoDataFrame(
            {'geometry': [input_gdf['geometry'].unary_union]},
            crs=input_gdf.crs
        )
    else:
        # 按字段分组融合
        result = input_gdf.dissolve(by=by, as_index=False)

    return result


def clip(input_gdf: gpd.GeoDataFrame, mask_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    裁剪几何（使用 mask 裁剪 input）

    Args:
        input_gdf: 输入 GeoDataFrame
        mask_gdf: 裁剪 mask GeoDataFrame

    Returns:
        GeoDataFrame 裁剪结果
    """
    return gpd.clip(input_gdf, mask_gdf)


def voronoi(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    泰森多边形（Voronoi 图）

    Args:
        input_gdf: 输入点 GeoDataFrame

    Returns:
        GeoDataFrame Voronoi 多边形结果
    """
    from shapely.ops import voronoi_diagram

    # 合并所有点
    points = input_gdf['geometry'].unary_union

    # 计算 Voronoi 图
    voronoi_polys = voronoi_diagram(points)

    # 转换为 GeoDataFrame
    result = gpd.GeoDataFrame(
        {'geometry': list(voronoi_polys.geoms)},
        crs=input_gdf.crs
    )

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
    result = input_gdf.copy()
    result['area'] = result['geometry'].area

    large = result[result['area'] > threshold].copy()
    small = result[result['area'] <= threshold].copy()

    # 返回命名输出端口
    return {
        "large": large,
        "small": small
    }


# ========================================
# 算子注册表（供 API 使用）
# ========================================

OPERATORS = {
    # 几何处理 (8个)
    'buffer': {
        'function': buffer,
        'params': {'distance': 'float', 'resolution': 'int'},
        'category': '几何处理',
        'description': '缓冲区分析'
    },
    'intersection': {
        'function': intersection,
        'params': {'gdf_b': 'geodataframe'},
        'category': '几何处理',
        'description': '几何相交'
    },
    'union': {
        'function': union,
        'params': {'gdf_list': 'list[geodataframe]'},
        'category': '几何处理',
        'description': '几何合并'
    },
    'centroid': {
        'function': centroid,
        'params': {},
        'category': '几何处理',
        'description': '计算质心'
    },
    'difference': {
        'function': difference,
        'params': {'gdf_b': 'geodataframe'},
        'category': '几何处理',
        'description': '几何差集'
    },
    'simplify': {
        'function': simplify,
        'params': {'tolerance': 'float', 'preserve_topology': 'bool'},
        'category': '几何处理',
        'description': '简化几何'
    },
    'convex_hull': {
        'function': convex_hull,
        'params': {},
        'category': '几何处理',
        'description': '凸包'
    },
    'envelope': {
        'function': envelope,
        'params': {},
        'category': '几何处理',
        'description': '最小外接矩形'
    },

    # 空间关系 (3个)
    'contains': {
        'function': contains,
        'params': {'gdf_b': 'geodataframe'},
        'category': '空间关系',
        'description': '包含判断'
    },
    'intersects': {
        'function': intersects,
        'params': {'gdf_b': 'geodataframe'},
        'category': '空间关系',
        'description': '相交判断'
    },
    'distance_to': {
        'function': distance_to,
        'params': {'target_geom': 'geodataframe'},
        'category': '空间关系',
        'description': '距离计算'
    },

    # 几何属性 (3个)
    'get_area': {
        'function': get_area,
        'params': {},
        'category': '几何属性',
        'description': '计算面积'
    },
    'get_length': {
        'function': get_length,
        'params': {},
        'category': '几何属性',
        'description': '计算长度'
    },
    'get_bounds': {
        'function': get_bounds,
        'params': {},
        'category': '几何属性',
        'description': '获取边界框'
    },

    # 格式转换 (2个)
    'load_from_wkt': {
        'function': load_from_wkt,
        'params': {'wkt_list': 'list[str]', 'properties': 'list[dict]', 'crs': 'str'},
        'category': '格式转换',
        'description': 'WKT → GeoDataFrame'
    },
    'export_to_wkt': {
        'function': export_to_wkt,
        'params': {},
        'category': '格式转换',
        'description': 'GeoDataFrame → WKT'
    },

    # 批处理 (2个)
    'batch_buffer': {
        'function': batch_buffer,
        'params': {'distances': 'list[float]', 'resolution': 'int'},
        'category': '批处理',
        'description': '批量缓冲'
    },
    'batch_centroid': {
        'function': batch_centroid,
        'params': {'gdf_list': 'list[geodataframe]'},
        'category': '批处理',
        'description': '批量质心'
    },

    # 高级算子 (3个)
    'dissolve': {
        'function': dissolve,
        'params': {'by': 'str or list[str]'},
        'category': '高级算子',
        'description': '融合几何'
    },
    'clip': {
        'function': clip,
        'params': {'mask_gdf': 'geodataframe'},
        'category': '高级算子',
        'description': '裁剪几何'
    },
    'voronoi': {
        'function': voronoi,
        'params': {},
        'category': '高级算子',
        'description': '泰森多边形'
    },
    'split_by_area': {
        'function': split_by_area,
        'params': {'threshold': 'float'},
        'category': '高级算子',
        'description': '按面积分割',
        'output_ports': [
            {'name': 'large', 'type': 'geodataframe', 'description': '面积大于阈值的要素', 'is_default': False},
            {'name': 'small', 'type': 'geodataframe', 'description': '面积小于等于阈值的要素', 'is_default': False}
        ]
    },

    # I/O 算子 (2个)
    'load': {
        'function': load,
        'params': {
            'source_type': 'str',
            'resource_id': 'int',
            'table': 'str',
            'schema': 'str',
            'path': 'str',
            'format': 'str',
            'geojson': 'dict'
        },
        'category': 'I/O',
        'description': '数据加载',
        'param_schema': [
            {
                'name': 'source_type',
                'type': 'string',
                'required': True,
                'description': '数据来源类型',
                'enum': ['table', 'file', 'geojson'],
                'default': 'table'
            },
            {
                'name': 'resource_id',
                'type': 'integer',
                'required': True,
                'description': '存储引擎资源ID',
                'depends_on': 'source_type',
                'ui_type': 'resource_select',  # 自定义UI类型,前端渲染为下拉列表
                'resource_types': ['postgresql', 'mysql', 'doris']  # 限定资源类型
            },
            {
                'name': 'schema',
                'type': 'string',
                'required': False,
                'description': '数据库schema',
                'default': 'public',
                'depends_on': 'resource_id',
                'ui_type': 'schema_select'  # 自定义UI类型
            },
            {
                'name': 'table',
                'type': 'string',
                'required': True,
                'description': '表名',
                'depends_on': 'schema',
                'ui_type': 'table_select'  # 自定义UI类型
            },
            {
                'name': 'path',
                'type': 'string',
                'required': False,
                'description': '文件路径 (source_type=file时使用)',
                'depends_on': 'source_type'
            },
            {
                'name': 'format',
                'type': 'string',
                'required': False,
                'description': '文件格式',
                'enum': ['geojson', 'shapefile'],
                'default': 'geojson',
                'depends_on': 'source_type'
            },
            {
                'name': 'geojson',
                'type': 'object',
                'required': False,
                'description': 'GeoJSON对象 (source_type=geojson时使用)',
                'depends_on': 'source_type'
            }
        ]
    },
    'save': {
        'function': save,
        'params': {
            'data': 'geodataframe',
            'target_type': 'str',
            'resource_id': 'int',
            'table': 'str',
            'schema': 'str',
            'mode': 'str',
            'path': 'str',
            'format': 'str'
        },
        'category': 'I/O',
        'description': '数据保存'
    }
}


def get_operator(operator_name: str):
    """获取算子函数"""
    if operator_name not in OPERATORS:
        raise ValueError(f"Unknown operator: {operator_name}")
    return OPERATORS[operator_name]['function']


def list_operators():
    """列出所有算子 - 返回符合统一标准的算子元数据列表"""
    operators_list = []

    # Python类型到标准类型的映射
    type_mapping = {
        'float': 'float',
        'int': 'integer',
        'bool': 'boolean',
        'str': 'string',
        'geodataframe': 'object',
        'list[float]': 'array',
        'list[str]': 'array',
    }

    for name, meta in OPERATORS.items():
        # 优先使用新的 param_schema,否则使用旧的 params 转换
        if 'param_schema' in meta:
            # 使用新的结构化参数定义
            parameters = meta['param_schema']
        else:
            # 转换旧的参数格式(向后兼容)
            parameters = []
            for param_name, param_type in meta['params'].items():
                param_meta = {
                    "name": param_name,
                    "type": type_mapping.get(param_type, 'string'),
                    "required": True if param_name == "input_gdf" else False,
                    "description": f"{param_name}参数"
                }

                # 数组类型需要指定item_type
                if param_type in ['list[float]', 'list[str]']:
                    param_meta["item_type"] = "float" if param_type == 'list[float]' else "string"

                parameters.append(param_meta)

        # 处理输出端口
        if 'output_ports' in meta:
            # 多输出算子：使用自定义端口定义
            output_ports = meta['output_ports']
        else:
            # 单输出算子：自动生成 default 端口
            output_ports = [{
                "name": "default",
                "type": "geodataframe",
                "description": f"{meta['description']}结果",
                "is_default": True
            }]

        # 构建标准化的算子元数据
        operator = {
            "id": name,
            "name": name,
            "display_name": meta['description'],
            "type": "spatial",
            "category": meta['category'],
            "description": meta['description'],
            "module": "geopandas",
            "parameters": parameters,
            "inputs": ["geodataframe"],
            "output_ports": output_ports  # 使用 output_ports 替代 outputs
        }
        operators_list.append(operator)

    return operators_list
