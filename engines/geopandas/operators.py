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
        'description': '缓冲区分析',

        # 简要描述
        'brief_description': '在几何对象周围创建指定距离的缓冲区,常用于影响范围分析和邻域查询',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '在输入几何对象周围创建指定距离的缓冲区多边形。支持点、线、面等各类几何类型,缓冲距离可正可负(负值仅对面有效,表示内缩)。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                },
                {
                    'name': 'distance',
                    'type': 'float',
                    'required': True,
                    'description': '缓冲距离,单位与坐标系一致',
                    'notes': '投影坐标系单位为米,地理坐标系单位为度(不推荐,建议先转投影坐标系)'
                },
                {
                    'name': 'resolution',
                    'type': 'int',
                    'required': False,
                    'default': 16,
                    'description': '缓冲区圆弧段数',
                    'notes': '值越大缓冲区越平滑但计算越慢,建议范围 8-32'
                }
            ],
            'use_cases': [
                '河流污染影响范围: 河流中心线缓冲500米',
                '学校服务范围: 学校点位缓冲1000米',
                '道路噪音影响区: 道路线缓冲200米',
                '保护区核心区: 边界内缩100米(负值缓冲)'
            ],
            'notes': [
                '地理坐标系(EPSG:4326)需先用 to_crs 转投影坐标系再缓冲',
                '大数据集建议先用空间过滤减少要素数量再缓冲',
                '负值缓冲仅对面几何有效,点和线无效',
                '缓冲结果可能产生自相交,建议后续用 simplify 简化'
            ],
            'input': 'GeoDataFrame (点/线/面)',
            'output': 'GeoDataFrame (Polygon,保留原属性)',

            # 工作流示例 (与 Develop DAG 格式匹配)
            'workflow_example': {
                'id': 'buffer_rivers',
                'operator': 'buffer',
                'params': {
                    'input_gdf': {'$ref': 'load_rivers'},
                    'distance': 500,
                    'resolution': 16
                },
                'depends_on': ['load_rivers']
            }
        }
    },
    'intersection': {
        'function': intersection,
        'params': {'gdf_b': 'geodataframe'},
        'category': '几何处理',
        'description': '几何相交',

        # 简要描述
        'brief_description': '计算两个几何图层的空间交集,提取重叠区域,常用于叠加分析',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算输入图层A与图层B的几何交集,返回两者重叠的部分。结果保留图层A的属性,可选择性保留图层B的属性。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入图层A(被裁剪对象)'
                },
                {
                    'name': 'gdf_b',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入图层B(裁剪对象)'
                }
            ],
            'use_cases': [
                '土地利用分析: 提取保护区内的耕地',
                '行政区划统计: 提取某市范围内的河流',
                '灾害风险评估: 提取洪水淹没区内的建筑物',
                '多图层叠加: 提取同时满足多个条件的区域'
            ],
            'notes': [
                '两个图层的坐标系必须一致,不一致需先用 to_crs 统一',
                '结果只包含有交集的要素,无交集的要素会被剔除',
                '如果需要保留无交集的要素,使用 spatial_join 代替',
                '大数据集建议先用空间索引优化性能'
            ],
            'input': 'GeoDataFrame A + GeoDataFrame B',
            'output': 'GeoDataFrame (几何类型可能变化,保留A的属性)',

            # 工作流示例
            'workflow_example': {
                'id': 'intersect_protected_area',
                'operator': 'intersection',
                'params': {
                    'input_gdf': {'$ref': 'load_farmland'},
                    'gdf_b': {'$ref': 'load_protected_area'}
                },
                'depends_on': ['load_farmland', 'load_protected_area']
            }
        }
    },
    'union': {
        'function': union,
        'params': {'gdf_list': 'list[geodataframe]'},
        'category': '几何处理',
        'description': '几何合并',

        # 简要描述
        'brief_description': '将多个图层的几何对象合并为一个图层,常用于数据整合和范围叠加',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '将多个GeoDataFrame的所有几何对象合并到一个GeoDataFrame中。所有输入图层的要素会保留在输出中,属性也会保留。适用于多数据源整合、多时相数据合并等场景。',
            'parameters': [
                {
                    'name': 'gdf_list',
                    'type': 'list[geodataframe]',
                    'required': True,
                    'description': 'GeoDataFrame列表,包含需要合并的多个图层',
                    'notes': '所有输入图层应具有相同的坐标系,否则建议先用to_crs统一坐标系'
                }
            ],
            'use_cases': [
                '多城市数据整合: 将各市的建筑物数据合并为全省数据',
                '历史数据汇总: 将多年的土地利用变化数据合并为时序数据集',
                '分区数据拼接: 将分区采集的GPS轨迹点合并为完整轨迹',
                '多源数据融合: 将不同部门提供的POI数据整合为统一数据库'
            ],
            'notes': [
                '合并后的属性列为所有输入图层属性列的并集,缺失值填充为NaN',
                '如果输入图层坐标系不一致,会导致几何错误,需先统一坐标系',
                '不同于dissolve,union不会融合几何,只是简单拼接',
                '大数据集合并可能消耗大量内存,建议分批处理'
            ],
            'input': 'list[GeoDataFrame] (多个图层)',
            'output': 'GeoDataFrame (包含所有输入要素)',

            # 工作流示例
            'workflow_example': {
                'id': 'union_cities',
                'operator': 'union',
                'params': {
                    'gdf_list': [
                        {'$ref': 'load_city_a'},
                        {'$ref': 'load_city_b'},
                        {'$ref': 'load_city_c'}
                    ]
                },
                'depends_on': ['load_city_a', 'load_city_b', 'load_city_c']
            }
        }
    },
    'centroid': {
        'function': centroid,
        'params': {},
        'category': '几何处理',
        'description': '计算质心',

        # 简要描述
        'brief_description': '计算几何对象的质心(中心点),常用于位置标注和点化处理',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算每个几何对象的质心坐标,将面或线转换为点。质心是几何对象的几何中心,适用于标注位置、聚合分析等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                }
            ],
            'use_cases': [
                '行政区标注: 计算省市边界质心用于地图标注',
                '建筑物点化: 将建筑物面转为中心点用于密度分析',
                '河流中心线提取: 计算河流面的中心线用于网络分析',
                '地块代表点: 为每个地块生成代表点用于空间连接'
            ],
            'notes': [
                '质心可能位于几何对象外部(如环形面),如需保证点在内部请使用representative_point',
                '线的质心是沿线中点,不一定是线的端点',
                '点的质心就是自身',
                '质心坐标单位与输入坐标系一致'
            ],
            'input': 'GeoDataFrame (点/线/面)',
            'output': 'GeoDataFrame (Point,保留原属性)',

            # 工作流示例
            'workflow_example': {
                'id': 'get_province_centers',
                'operator': 'centroid',
                'params': {
                    'input_gdf': {'$ref': 'load_provinces'}
                },
                'depends_on': ['load_provinces']
            }
        }
    },
    'difference': {
        'function': difference,
        'params': {'gdf_b': 'geodataframe'},
        'category': '几何处理',
        'description': '几何差集',

        # 简要描述
        'brief_description': '计算两个图层的几何差集(A-B),保留A中不与B重叠的部分',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '从图层A中减去图层B的重叠部分,返回A中独有的几何区域。常用于排除分析、净用地计算等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '被减图层(图层A)'
                },
                {
                    'name': 'gdf_b',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '减去的图层(图层B)',
                    'notes': '需与input_gdf具有相同的坐标系'
                }
            ],
            'use_cases': [
                '净用地计算: 建设用地减去道路用地得到净建设用地',
                '排除保护区: 可开发区域减去自然保护区得到可用区域',
                '去除已建区: 规划范围减去已建成区得到待开发区',
                '水体擦除: 行政区边界减去水体得到陆地范围'
            ],
            'notes': [
                '结果保留input_gdf的属性,gdf_b的属性不保留',
                '如果A与B完全不相交,返回A的原始几何',
                '如果A完全被B包含,返回空几何',
                '大数据集差集运算较慢,建议先用空间索引过滤'
            ],
            'input': 'GeoDataFrame (input_gdf和gdf_b,需同坐标系)',
            'output': 'GeoDataFrame (与input_gdf相同的要素数,保留原属性)',

            # 工作流示例
            'workflow_example': {
                'id': 'exclude_protected',
                'operator': 'difference',
                'params': {
                    'input_gdf': {'$ref': 'load_land'},
                    'gdf_b': {'$ref': 'load_protected'}
                },
                'depends_on': ['load_land', 'load_protected']
            }
        }
    },
    'simplify': {
        'function': simplify,
        'params': {'tolerance': 'float', 'preserve_topology': 'bool'},
        'category': '几何处理',
        'description': '简化几何',

        # 简要描述
        'brief_description': '简化几何对象的顶点数量,减小数据量,常用于地图制图和性能优化',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '使用Douglas-Peucker算法简化几何对象,减少顶点数量同时保持形状特征。适用于大比例尺制图、Web地图加载优化等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                },
                {
                    'name': 'tolerance',
                    'type': 'float',
                    'required': True,
                    'description': '简化容差,单位与坐标系一致',
                    'notes': '值越大简化越激进。投影坐标系单位为米,建议10-100;地理坐标系单位为度,建议0.0001-0.001'
                },
                {
                    'name': 'preserve_topology',
                    'type': 'bool',
                    'required': False,
                    'default': True,
                    'description': '是否保持拓扑一致性',
                    'notes': 'True时保证简化后几何不自交,但速度较慢;False时速度快但可能产生自交'
                }
            ],
            'use_cases': [
                'Web地图优化: 简化省市边界用于在线地图快速加载',
                '制图综合: 将1:1万地图简化为1:50万比例尺',
                '数据压缩: 减少GPS轨迹点数量降低存储空间',
                '性能优化: 简化复杂面几何加速空间运算'
            ],
            'notes': [
                'tolerance过大会导致几何变形严重甚至崩溃',
                '地理坐标系(EPSG:4326)简化效果较差,建议先转投影坐标系',
                'preserve_topology=True时邻接面不会产生缝隙',
                '简化后的几何可能不满足原始精度要求,需权衡数据量和精度'
            ],
            'input': 'GeoDataFrame (线/面)',
            'output': 'GeoDataFrame (保留原属性,几何顶点减少)',

            # 工作流示例
            'workflow_example': {
                'id': 'simplify_boundaries',
                'operator': 'simplify',
                'params': {
                    'input_gdf': {'$ref': 'load_provinces'},
                    'tolerance': 100,
                    'preserve_topology': True
                },
                'depends_on': ['load_provinces']
            }
        }
    },
    'convex_hull': {
        'function': convex_hull,
        'params': {},
        'category': '几何处理',
        'description': '凸包',

        # 简要描述
        'brief_description': '计算几何对象的最小凸包(凸多边形),常用于范围分析和形状概括',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算包含几何对象所有顶点的最小凸多边形。凸包类似用橡皮筋包裹几何对象的外轮廓,适用于范围估算、离群点检测等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                }
            ],
            'use_cases': [
                '活动范围估算: 计算GPS轨迹点的凸包估算人员活动范围',
                '建筑群轮廓: 计算建筑物点集的凸包生成建筑群外轮廓',
                '数据质量检查: 检测离群点(凸包面积异常大)',
                '服务范围概算: 用设施点的凸包快速估算覆盖范围'
            ],
            'notes': [
                '凸包会忽略凹陷部分,不能精确表示复杂形状',
                '如需精确边界,建议使用alpha_shape或buffer',
                '点的凸包是点本身,线的凸包可能是线或面',
                '凸包面积通常大于原始几何面积'
            ],
            'input': 'GeoDataFrame (点/线/面)',
            'output': 'GeoDataFrame (Polygon,保留原属性)',

            # 工作流示例
            'workflow_example': {
                'id': 'get_activity_range',
                'operator': 'convex_hull',
                'params': {
                    'input_gdf': {'$ref': 'load_gps_points'}
                },
                'depends_on': ['load_gps_points']
            }
        }
    },
    'envelope': {
        'function': envelope,
        'params': {},
        'category': '几何处理',
        'description': '最小外接矩形',

        # 简要描述
        'brief_description': '计算几何对象的最小外接矩形(MBR),常用于快速范围查询和空间索引',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算包含几何对象的最小轴对齐矩形(Minimum Bounding Rectangle),边平行于坐标轴。常用于空间索引、粗略范围查询等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                }
            ],
            'use_cases': [
                '空间索引构建: 为复杂几何创建快速索引矩形',
                '地图瓦片切割: 计算要素范围用于地图瓦片分配',
                '粗略相交判断: 用矩形快速过滤候选对象再精确计算',
                '图框生成: 为地图要素生成标准矩形图框'
            ],
            'notes': [
                '矩形边平行于坐标轴,对旋转的狭长几何可能产生很大冗余',
                '如需最小面积外接矩形,使用minimum_rotated_rectangle',
                'envelope面积通常大于原始几何面积',
                '点和线的envelope可能退化为线或点'
            ],
            'input': 'GeoDataFrame (点/线/面)',
            'output': 'GeoDataFrame (Polygon,保留原属性)',

            # 工作流示例
            'workflow_example': {
                'id': 'get_bounds_rect',
                'operator': 'envelope',
                'params': {
                    'input_gdf': {'$ref': 'load_buildings'}
                },
                'depends_on': ['load_buildings']
            }
        }
    },

    # 空间关系 (3个)
    'contains': {
        'function': contains,
        'params': {'gdf_b': 'geodataframe'},
        'category': '空间关系',
        'description': '包含判断',

        # 简要描述
        'brief_description': '判断图层A的几何是否包含图层B的几何,常用于点在面内判断',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '对图层A和图层B进行空间包含关系判断,返回布尔值表示A是否完全包含B。常用于点在面内查询、边界检查等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '容器图层(图层A,通常是面)'
                },
                {
                    'name': 'gdf_b',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '被包含图层(图层B,通常是点)',
                    'notes': '需与input_gdf具有相同的坐标系'
                }
            ],
            'use_cases': [
                '点在面内查询: 判断POI点是否在行政区内',
                '建筑物归属: 判断建筑物是否完全在地块内',
                '数据质量检查: 检查子区域是否完全在父区域内',
                '权限验证: 判断用户操作范围是否在授权区域内'
            ],
            'notes': [
                '包含关系要求B完全在A内部,边界接触不算包含',
                '如需包含边界接触情况,使用covers关系',
                '大数据集判断建议先用空间索引过滤',
                '返回布尔Series,可用于过滤数据'
            ],
            'input': 'GeoDataFrame (input_gdf和gdf_b,需同坐标系)',
            'output': 'Series[bool] (与input_gdf相同长度的布尔值)',

            # 工作流示例
            'workflow_example': {
                'id': 'check_points_in_area',
                'operator': 'contains',
                'params': {
                    'input_gdf': {'$ref': 'load_districts'},
                    'gdf_b': {'$ref': 'load_poi_points'}
                },
                'depends_on': ['load_districts', 'load_poi_points']
            }
        }
    },
    'intersects': {
        'function': intersects,
        'params': {'gdf_b': 'geodataframe'},
        'category': '空间关系',
        'description': '相交判断',

        # 简要描述
        'brief_description': '判断两个图层的几何是否相交(包括接触),常用于空间关系过滤',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '判断图层A和图层B的几何对象是否存在空间相交关系(包括重叠、接触、包含等)。返回布尔值,是最常用的空间关系判断算子。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '图层A'
                },
                {
                    'name': 'gdf_b',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '图层B',
                    'notes': '需与input_gdf具有相同的坐标系'
                }
            ],
            'use_cases': [
                '灾害影响分析: 判断洪水淹没区与建筑物是否相交',
                '规划冲突检测: 检查新规划地块是否与现状用地冲突',
                '设施服务范围: 判断服务设施缓冲区与居民区是否相交',
                '交通可达性: 判断道路网络与目标区域是否连通'
            ],
            'notes': [
                'intersects包含所有非分离情况:重叠、接触、包含、被包含',
                '边界接触也算相交,与contains不同',
                '是最宽松的空间关系判断,适用于初步筛选',
                '大数据集建议先用空间索引过滤候选对象'
            ],
            'input': 'GeoDataFrame (input_gdf和gdf_b,需同坐标系)',
            'output': 'Series[bool] (与input_gdf相同长度的布尔值)',

            # 工作流示例
            'workflow_example': {
                'id': 'check_flood_impact',
                'operator': 'intersects',
                'params': {
                    'input_gdf': {'$ref': 'load_flood_zone'},
                    'gdf_b': {'$ref': 'load_buildings'}
                },
                'depends_on': ['load_flood_zone', 'load_buildings']
            }
        }
    },
    'distance_to': {
        'function': distance_to,
        'params': {'target_geom': 'geodataframe'},
        'category': '空间关系',
        'description': '距离计算',

        # 简要描述
        'brief_description': '计算几何对象到目标几何的最短距离,常用于邻近度分析和缓冲区计算',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算图层A中每个几何对象到图层B中最近几何对象的欧氏距离。返回距离值,适用于可达性分析、服务范围评估等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '源图层'
                },
                {
                    'name': 'target_geom',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '目标图层',
                    'notes': '需与input_gdf具有相同的坐标系,且建议使用投影坐标系以获得准确距离'
                }
            ],
            'use_cases': [
                '设施可达性: 计算居民点到最近医院的距离',
                '噪音影响评估: 计算建筑物到道路的距离',
                '生态廊道: 计算栖息地斑块之间的距离',
                '配送范围: 计算客户位置到配送中心的距离'
            ],
            'notes': [
                '距离单位与坐标系一致:投影坐标系为米,地理坐标系为度',
                '地理坐标系(EPSG:4326)的度单位不直观,建议先转投影坐标系',
                '返回的是最近距离,如需到所有目标的距离需遍历',
                '大数据集建议先用空间索引加速最近邻查询'
            ],
            'input': 'GeoDataFrame (input_gdf和target_geom,需同坐标系)',
            'output': 'Series[float] (与input_gdf相同长度的距离值)',

            # 工作流示例
            'workflow_example': {
                'id': 'calc_distance_to_hospital',
                'operator': 'distance_to',
                'params': {
                    'input_gdf': {'$ref': 'load_communities'},
                    'target_geom': {'$ref': 'load_hospitals'}
                },
                'depends_on': ['load_communities', 'load_hospitals']
            }
        }
    },

    # 几何属性 (3个)
    'get_area': {
        'function': get_area,
        'params': {},
        'category': '几何属性',
        'description': '计算面积',

        # 简要描述
        'brief_description': '计算面几何的面积,常用于土地统计和资源核算',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算GeoDataFrame中每个面几何的面积,返回数值。适用于土地利用统计、资源清单、规划指标核算等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的面几何数据'
                }
            ],
            'use_cases': [
                '土地利用统计: 计算各地类的总面积',
                '规划指标核算: 统计建设用地面积是否超标',
                '生态资源清查: 计算林地、湿地面积',
                '房产测绘: 计算建筑物占地面积'
            ],
            'notes': [
                '面积单位与坐标系一致:投影坐标系为平方米,地理坐标系为平方度',
                '地理坐标系(EPSG:4326)的平方度不直观,建议先转投影坐标系(如UTM)',
                '对于点和线几何,面积为0',
                '大范围区域面积计算建议使用等面积投影(如Albers)'
            ],
            'input': 'GeoDataFrame (Polygon/MultiPolygon)',
            'output': 'Series[float] (与input_gdf相同长度的面积值)',

            # 工作流示例
            'workflow_example': {
                'id': 'calc_landuse_area',
                'operator': 'get_area',
                'params': {
                    'input_gdf': {'$ref': 'load_landuse'}
                },
                'depends_on': ['load_landuse']
            }
        }
    },
    'get_length': {
        'function': get_length,
        'params': {},
        'category': '几何属性',
        'description': '计算长度',

        # 简要描述
        'brief_description': '计算线几何的长度,常用于道路统计和网络分析',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '计算GeoDataFrame中每个线几何的长度,返回数值。适用于道路统计、河流长度计算、管网清单等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的线几何数据'
                }
            ],
            'use_cases': [
                '道路里程统计: 计算各级道路总长度',
                '河流长度计算: 统计流域内河流总长',
                '管网清单: 计算供水、燃气管道长度',
                '步行距离: 计算步行路径的实际长度'
            ],
            'notes': [
                '长度单位与坐标系一致:投影坐标系为米,地理坐标系为度',
                '地理坐标系(EPSG:4326)的度单位不直观,建议先转投影坐标系',
                '对于点几何,长度为0;对于面几何,返回周长',
                '大范围线要素长度计算建议使用等距投影'
            ],
            'input': 'GeoDataFrame (LineString/MultiLineString)',
            'output': 'Series[float] (与input_gdf相同长度的长度值)',

            # 工作流示例
            'workflow_example': {
                'id': 'calc_road_length',
                'operator': 'get_length',
                'params': {
                    'input_gdf': {'$ref': 'load_roads'}
                },
                'depends_on': ['load_roads']
            }
        }
    },
    'get_bounds': {
        'function': get_bounds,
        'params': {},
        'category': '几何属性',
        'description': '获取边界框',

        # 简要描述
        'brief_description': '获取几何对象的边界框坐标(minx, miny, maxx, maxy),常用于范围查询',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '返回每个几何对象的边界框(Bounding Box)坐标,包含minx、miny、maxx、maxy四个值。适用于地图范围定位、数据裁剪、空间索引构建等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                }
            ],
            'use_cases': [
                '地图定位: 获取要素范围用于地图缩放定位',
                '数据裁剪: 用边界框快速过滤候选数据',
                '瓦片计算: 计算要素所在的地图瓦片编号',
                '空间索引: 构建R-Tree等空间索引的边界矩形'
            ],
            'notes': [
                '返回DataFrame包含minx、miny、maxx、maxy四列',
                '边界框是轴对齐矩形,不随几何旋转',
                '坐标单位与输入坐标系一致',
                '点的边界框minx=maxx, miny=maxy'
            ],
            'input': 'GeoDataFrame (点/线/面)',
            'output': 'DataFrame (包含minx、miny、maxx、maxy四列)',

            # 工作流示例
            'workflow_example': {
                'id': 'get_feature_bounds',
                'operator': 'get_bounds',
                'params': {
                    'input_gdf': {'$ref': 'load_features'}
                },
                'depends_on': ['load_features']
            }
        }
    },

    # 格式转换 (2个)
    'load_from_wkt': {
        'function': load_from_wkt,
        'params': {'wkt_list': 'list[str]', 'properties': 'list[dict]', 'crs': 'str'},
        'category': '格式转换',
        'description': 'WKT → GeoDataFrame',

        # 简要描述
        'brief_description': '从WKT文本列表创建GeoDataFrame,常用于文本格式几何数据导入',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '将Well-Known Text (WKT)格式的几何文本列表转换为GeoDataFrame。WKT是标准的几何文本表示格式,常用于数据交换和存储。',
            'parameters': [
                {
                    'name': 'wkt_list',
                    'type': 'list[str]',
                    'required': True,
                    'description': 'WKT文本字符串列表',
                    'notes': '每个字符串表示一个几何对象,如 "POINT (120.5 30.2)"'
                },
                {
                    'name': 'properties',
                    'type': 'list[dict]',
                    'required': False,
                    'description': '属性字典列表,每个字典对应一个几何的属性',
                    'notes': '长度必须与wkt_list相同'
                },
                {
                    'name': 'crs',
                    'type': 'str',
                    'required': False,
                    'default': 'EPSG:4326',
                    'description': '坐标参考系统',
                    'notes': '如 "EPSG:4326" (WGS84) 或 "EPSG:3857" (Web墨卡托)'
                }
            ],
            'use_cases': [
                'API数据导入: 从REST API返回的WKT文本创建图层',
                '数据库导入: 读取数据库中WKT字段创建空间数据',
                '文本解析: 解析配置文件或日志中的几何文本',
                '格式转换: 将其他系统导出的WKT转为GeoDataFrame'
            ],
            'notes': [
                'WKT格式必须符合OGC标准,否则解析失败',
                '常见WKT类型: POINT、LINESTRING、POLYGON、MULTIPOINT等',
                'properties列表长度必须与wkt_list一致',
                '默认坐标系为WGS84 (EPSG:4326)'
            ],
            'input': 'list[str] (WKT文本) + list[dict] (属性,可选)',
            'output': 'GeoDataFrame',

            # 工作流示例
            'workflow_example': {
                'id': 'load_wkt_data',
                'operator': 'load_from_wkt',
                'params': {
                    'wkt_list': [
                        'POINT (120.5 30.2)',
                        'POINT (121.0 31.0)'
                    ],
                    'properties': [
                        {'name': '杭州', 'population': 1000000},
                        {'name': '上海', 'population': 2500000}
                    ],
                    'crs': 'EPSG:4326'
                },
                'depends_on': []
            }
        }
    },
    'export_to_wkt': {
        'function': export_to_wkt,
        'params': {},
        'category': '格式转换',
        'description': 'GeoDataFrame → WKT',

        # 简要描述
        'brief_description': '将GeoDataFrame的几何导出为WKT文本列表,常用于数据交换',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '将GeoDataFrame中的几何对象转换为Well-Known Text (WKT)格式的文本列表。WKT是标准的几何文本表示,便于存储到数据库文本字段或传输给其他系统。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                }
            ],
            'use_cases': [
                '数据库存储: 将几何导出为WKT存入数据库文本字段',
                'API响应: 将空间数据以WKT格式返回给客户端',
                '日志记录: 记录几何对象的文本表示到日志',
                '格式转换: 导出为其他GIS系统可识别的WKT格式'
            ],
            'notes': [
                '返回list[str],每个元素是一个WKT文本',
                'WKT文本不包含坐标系信息,需单独记录CRS',
                '复杂几何的WKT文本可能很长',
                '属性信息不包含在WKT中,需单独处理'
            ],
            'input': 'GeoDataFrame',
            'output': 'list[str] (WKT文本列表)',

            # 工作流示例
            'workflow_example': {
                'id': 'export_wkt',
                'operator': 'export_to_wkt',
                'params': {
                    'input_gdf': {'$ref': 'load_features'}
                },
                'depends_on': ['load_features']
            }
        }
    },

    # 批处理 (2个)
    'batch_buffer': {
        'function': batch_buffer,
        'params': {'distances': 'list[float]', 'resolution': 'int'},
        'category': '批处理',
        'description': '批量缓冲',

        # 简要描述
        'brief_description': '对同一图层使用多个缓冲距离批量生成缓冲区,常用于多级影响范围分析',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '对输入GeoDataFrame使用多个缓冲距离批量生成缓冲区,返回包含多个缓冲结果的列表。适用于多级影响范围、服务等级划分等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                },
                {
                    'name': 'distances',
                    'type': 'list[float]',
                    'required': True,
                    'description': '缓冲距离列表',
                    'notes': '如 [100, 300, 500] 表示生成100米、300米、500米三级缓冲区'
                },
                {
                    'name': 'resolution',
                    'type': 'int',
                    'required': False,
                    'default': 16,
                    'description': '缓冲区圆弧段数',
                    'notes': '值越大越平滑,建议8-32'
                }
            ],
            'use_cases': [
                '多级影响范围: 分析设施500米、1000米、2000米三级服务范围',
                '噪音等级划分: 道路50米、100米、200米三级噪音影响区',
                '辐射范围分析: 基站200米、500米、1000米覆盖范围',
                '风险分区: 危化品仓库100米、300米、500米风险等级区'
            ],
            'notes': [
                '返回list[GeoDataFrame],每个元素对应一个缓冲距离的结果',
                '距离单位与坐标系一致',
                '地理坐标系建议先转投影坐标系',
                '批量处理比逐个调用buffer更高效'
            ],
            'input': 'GeoDataFrame + list[float]',
            'output': 'list[GeoDataFrame] (与distances长度相同)',

            # 工作流示例
            'workflow_example': {
                'id': 'multi_level_buffer',
                'operator': 'batch_buffer',
                'params': {
                    'input_gdf': {'$ref': 'load_facilities'},
                    'distances': [500, 1000, 2000],
                    'resolution': 16
                },
                'depends_on': ['load_facilities']
            }
        }
    },
    'batch_centroid': {
        'function': batch_centroid,
        'params': {'gdf_list': 'list[geodataframe]'},
        'category': '批处理',
        'description': '批量质心',

        # 简要描述
        'brief_description': '批量计算多个图层的质心,常用于多数据源的中心点提取',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '对多个GeoDataFrame批量计算质心,返回质心结果列表。适用于多时相数据处理、多区域分析等场景,比逐个调用centroid更高效。',
            'parameters': [
                {
                    'name': 'gdf_list',
                    'type': 'list[geodataframe]',
                    'required': True,
                    'description': 'GeoDataFrame列表,包含需要计算质心的多个图层',
                    'notes': '所有图层建议具有相同的坐标系'
                }
            ],
            'use_cases': [
                '多时相中心点: 批量计算历年建成区的质心变化',
                '多区域标注: 为多个行政区批量生成标注点',
                '多数据源整合: 批量将多个面图层转为点图层用于聚合',
                '变化监测: 对比不同时期同一要素的质心位移'
            ],
            'notes': [
                '返回list[GeoDataFrame],每个元素对应一个输入图层的质心结果',
                '质心可能位于几何对象外部(如环形面)',
                '批量处理比逐个调用centroid节省内存和时间',
                '输出结果顺序与输入gdf_list一致'
            ],
            'input': 'list[GeoDataFrame]',
            'output': 'list[GeoDataFrame] (与gdf_list长度相同,几何为Point)',

            # 工作流示例
            'workflow_example': {
                'id': 'batch_calc_centroids',
                'operator': 'batch_centroid',
                'params': {
                    'gdf_list': [
                        {'$ref': 'load_area_2020'},
                        {'$ref': 'load_area_2021'},
                        {'$ref': 'load_area_2022'}
                    ]
                },
                'depends_on': ['load_area_2020', 'load_area_2021', 'load_area_2022']
            }
        }
    },

    # 高级算子 (3个)
    'dissolve': {
        'function': dissolve,
        'params': {'by': 'str or list[str]'},
        'category': '高级算子',
        'description': '融合几何',

        # 简要描述
        'brief_description': '按属性字段分组并融合几何,常用于边界合并和统计汇总',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '按指定的属性字段对几何对象进行分组,并将同组的几何对象融合为一个。可用于行政区划合并、地类汇总等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的地理数据'
                },
                {
                    'name': 'by',
                    'type': 'string or list',
                    'required': True,
                    'description': '分组字段名或字段名列表',
                    'notes': '单个字段用字符串,多个字段用列表,如 "province" 或 ["province", "city"]'
                }
            ],
            'use_cases': [
                '省级边界合并: 将市级行政区按省份字段融合为省级边界',
                '土地利用汇总: 将地块按地类字段融合为地类统计面',
                '流域边界生成: 将子流域按流域代码融合为完整流域',
                '人口统计区聚合: 将人口普查小区按街道融合'
            ],
            'notes': [
                '融合后每组只保留一条记录,数值字段会被求和,其他字段取第一个值',
                '如需自定义聚合函数,建议先分组再用其他方法处理',
                '分组字段必须存在于输入数据中,否则报错',
                '大数据集融合可能较慢,建议先过滤不需要的数据'
            ],
            'input': 'GeoDataFrame',
            'output': 'GeoDataFrame (按分组字段聚合后的结果)',

            # 工作流示例
            'workflow_example': {
                'id': 'dissolve_by_province',
                'operator': 'dissolve',
                'params': {
                    'input_gdf': {'$ref': 'load_cities'},
                    'by': 'province'
                },
                'depends_on': ['load_cities']
            }
        }
    },
    'clip': {
        'function': clip,
        'params': {'mask_gdf': 'geodataframe'},
        'category': '高级算子',
        'description': '裁剪几何',

        # 简要描述
        'brief_description': '用掩膜图层裁剪输入图层,保留掩膜范围内的部分,常用于研究区提取',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '使用掩膜图层(mask)裁剪输入图层,只保留落在掩膜范围内的几何部分。类似GIS中的Clip工具,常用于提取研究区数据、行政区数据裁剪等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '被裁剪的输入图层'
                },
                {
                    'name': 'mask_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '掩膜图层,定义裁剪范围',
                    'notes': '需与input_gdf具有相同的坐标系,通常是面图层'
                }
            ],
            'use_cases': [
                '研究区提取: 用行政区边界裁剪全国数据得到省级数据',
                '项目范围裁剪: 用项目红线裁剪地形数据',
                '海陆分离: 用陆地边界裁剪数据去除海洋部分',
                '缓冲区内裁剪: 用缓冲区裁剪要素得到影响范围内的数据'
            ],
            'notes': [
                '裁剪后保留input_gdf的属性,mask_gdf只提供范围',
                '与intersection的区别:clip会裁切几何,intersection保留完整几何',
                '掩膜图层如有多个面,会按union后的总范围裁剪',
                '大数据集裁剪建议先用空间索引过滤候选对象'
            ],
            'input': 'GeoDataFrame (input_gdf和mask_gdf,需同坐标系)',
            'output': 'GeoDataFrame (保留input_gdf属性,几何被裁剪)',

            # 工作流示例
            'workflow_example': {
                'id': 'clip_by_province',
                'operator': 'clip',
                'params': {
                    'input_gdf': {'$ref': 'load_national_roads'},
                    'mask_gdf': {'$ref': 'load_province_boundary'}
                },
                'depends_on': ['load_national_roads', 'load_province_boundary']
            }
        }
    },
    'voronoi': {
        'function': voronoi,
        'params': {},
        'category': '高级算子',
        'description': '泰森多边形',

        # 简要描述
        'brief_description': '生成点集的泰森多边形(Voronoi图),常用于服务范围划分和最近邻分析',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '为输入的点集生成泰森多边形(Voronoi Diagram)。每个多边形内的所有位置到对应点的距离都比到其他点更近。适用于服务范围划分、影响区域分析、最近设施查询等场景。',
            'parameters': [
                {
                    'name': 'input_gdf',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '输入的点图层'
                }
            ],
            'use_cases': [
                '服务范围划分: 为医院、学校等设施生成服务范围多边形',
                '势力范围分析: 分析各门店的潜在影响区域',
                '最近设施查询: 快速定位距离最近的服务点',
                '气象插值: 为气象站点生成影响区域用于空间插值'
            ],
            'notes': [
                '输入必须是点几何,线和面会报错',
                '泰森多边形会覆盖整个凸包范围,边界多边形会很大',
                '如需限定范围,建议后续用clip裁剪到研究区',
                '点位重复会导致结果异常,建议先去重'
            ],
            'input': 'GeoDataFrame (Point/MultiPoint)',
            'output': 'GeoDataFrame (Polygon,保留原属性)',

            # 工作流示例
            'workflow_example': {
                'id': 'gen_service_areas',
                'operator': 'voronoi',
                'params': {
                    'input_gdf': {'$ref': 'load_hospitals'}
                },
                'depends_on': ['load_hospitals']
            }
        }
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

        # 简要描述
        'brief_description': '从数据库表、文件或GeoJSON对象加载空间数据,支持多种数据源',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '通用数据加载算子,支持从数据库表(PostgreSQL/MySQL/Doris)、文件(Shapefile/GeoJSON)或内存GeoJSON对象加载空间数据。根据 source_type 参数自动选择加载方式。',
            'parameters': [
                {
                    'name': 'source_type',
                    'type': 'string',
                    'required': True,
                    'description': '数据来源类型',
                    'notes': '可选值: table(数据库表), file(文件), geojson(GeoJSON对象)',
                    'default': 'table'
                },
                {
                    'name': 'resource_id',
                    'type': 'integer',
                    'required': True,
                    'description': '存储引擎资源ID',
                    'notes': '仅 source_type=table 时有效,对应 System 模块中的资源ID'
                },
                {
                    'name': 'schema',
                    'type': 'string',
                    'required': False,
                    'default': 'public',
                    'description': '数据库schema名称',
                    'notes': '仅 source_type=table 时有效'
                },
                {
                    'name': 'table',
                    'type': 'string',
                    'required': False,
                    'description': '数据库表名',
                    'notes': '仅 source_type=table 时必填'
                },
                {
                    'name': 'path',
                    'type': 'string',
                    'required': False,
                    'description': '文件路径',
                    'notes': '仅 source_type=file 时必填,支持绝对路径或相对路径'
                },
                {
                    'name': 'format',
                    'type': 'string',
                    'required': False,
                    'default': 'geojson',
                    'description': '文件格式',
                    'notes': '仅 source_type=file 时有效,可选值: geojson, shapefile'
                },
                {
                    'name': 'geojson',
                    'type': 'object',
                    'required': False,
                    'description': 'GeoJSON对象',
                    'notes': '仅 source_type=geojson 时必填,必须是有效的 GeoJSON FeatureCollection'
                }
            ],
            'use_cases': [
                '从业务数据库加载河流数据: source_type=table, resource_id=1, table=rivers',
                '从文件加载行政区划: source_type=file, path=/data/admin.shp, format=shapefile',
                '从内存GeoJSON加载临时数据: source_type=geojson, geojson={...}',
                '工作流起始节点: 作为数据输入的第一步'
            ],
            'notes': [
                '数据库表必须包含几何字段(geometry列),否则加载失败',
                '文件路径支持 MinIO 路径(minio://bucket/path)和本地路径',
                'Shapefile 会自动处理编码问题,默认尝试 utf-8 和 gb2312',
                '加载后的 GeoDataFrame 会保留所有属性字段'
            ],
            'input': '无(数据源算子)',
            'output': 'GeoDataFrame',

            # 工作流示例
            'workflow_example': {
                'id': 'load_rivers',
                'operator': 'load',
                'params': {
                    'source_type': 'table',
                    'resource_id': 1,
                    'schema': 'public',
                    'table': 'rivers'
                },
                'depends_on': []
            }
        },

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
        'description': '数据保存',

        # 简要描述
        'brief_description': '将空间数据保存到数据库表或文件,支持多种输出格式',

        # 详细描述 (JSON 结构化)
        'detailed_description': {
            'overview': '通用数据保存算子,支持将 GeoDataFrame 保存到数据库表(PostgreSQL/MySQL/Doris)或文件(Shapefile/GeoJSON)。根据 target_type 参数自动选择保存方式。',
            'parameters': [
                {
                    'name': 'data',
                    'type': 'geodataframe',
                    'required': True,
                    'description': '要保存的空间数据'
                },
                {
                    'name': 'target_type',
                    'type': 'string',
                    'required': True,
                    'description': '保存目标类型',
                    'notes': '可选值: table(数据库表), file(文件)',
                    'default': 'table'
                },
                {
                    'name': 'resource_id',
                    'type': 'integer',
                    'required': False,
                    'description': '存储引擎资源ID',
                    'notes': '仅 target_type=table 时必填,对应 System 模块中的资源ID'
                },
                {
                    'name': 'schema',
                    'type': 'string',
                    'required': False,
                    'default': 'public',
                    'description': '数据库schema名称',
                    'notes': '仅 target_type=table 时有效'
                },
                {
                    'name': 'table',
                    'type': 'string',
                    'required': False,
                    'description': '数据库表名',
                    'notes': '仅 target_type=table 时必填'
                },
                {
                    'name': 'mode',
                    'type': 'string',
                    'required': False,
                    'default': 'replace',
                    'description': '表已存在时的处理方式',
                    'notes': '可选值: replace(替换), append(追加), fail(失败),仅 target_type=table 时有效'
                },
                {
                    'name': 'path',
                    'type': 'string',
                    'required': False,
                    'description': '文件保存路径',
                    'notes': '仅 target_type=file 时必填,支持绝对路径或相对路径'
                },
                {
                    'name': 'format',
                    'type': 'string',
                    'required': False,
                    'default': 'geojson',
                    'description': '文件格式',
                    'notes': '仅 target_type=file 时有效,可选值: geojson, shapefile'
                }
            ],
            'use_cases': [
                '保存分析结果到数据库: target_type=table, resource_id=1, table=result, mode=replace',
                '导出到GeoJSON文件: target_type=file, path=/output/result.geojson, format=geojson',
                '导出到Shapefile: target_type=file, path=/output/result.shp, format=shapefile',
                '工作流结束节点: 作为数据输出的最后一步'
            ],
            'notes': [
                '保存到数据库时会自动创建几何索引提升查询性能',
                '保存到Shapefile时字段名会被截断为10个字符',
                'GeoJSON 格式保留所有字段名和数据精度',
                'mode=append 时要求表结构与数据结构一致'
            ],
            'input': 'GeoDataFrame',
            'output': '无(输出算子,返回成功/失败状态)',

            # 工作流示例
            'workflow_example': {
                'id': 'save_result',
                'operator': 'save',
                'params': {
                    'data': {'$ref': 'buffer_rivers'},
                    'target_type': 'table',
                    'resource_id': 1,
                    'schema': 'public',
                    'table': 'river_buffer_result',
                    'mode': 'replace'
                },
                'depends_on': ['buffer_rivers']
            }
        }
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

        # 添加简要描述 (如果存在)
        if 'brief_description' in meta:
            operator['brief_description'] = meta['brief_description']

        # 添加详细描述 (如果存在)
        if 'detailed_description' in meta:
            operator['detailed_description'] = meta['detailed_description']

        operators_list.append(operator)

    return operators_list
