"""
数据源和输出算子
扩展空间算子库，支持从数据库加载和保存结果
"""

from typing import Dict, Any, List, Optional
import json


def load_from_geojson_string(geojson_str: str) -> Dict[str, Any]:
    """
    从 GeoJSON 字符串加载几何对象

    Args:
        geojson_str: GeoJSON 字符串

    Returns:
        GeoJSON 字典
    """
    return json.loads(geojson_str)


def load_from_wkt(wkt_text: str) -> Dict[str, Any]:
    """
    从 WKT (Well-Known Text) 加载几何对象

    Args:
        wkt_text: WKT 文本，如 "POINT(116.404 39.915)"

    Returns:
        GeoJSON 字典
    """
    from shapely import wkt
    from shapely.geometry import mapping

    geom = wkt.loads(wkt_text)
    return mapping(geom)


def export_to_geojson_string(input_geom: Dict[str, Any], pretty: bool = False) -> str:
    """
    导出为 GeoJSON 字符串

    Args:
        input_geom: GeoJSON 几何对象
        pretty: 是否格式化输出

    Returns:
        GeoJSON 字符串
    """
    if pretty:
        return json.dumps(input_geom, indent=2, ensure_ascii=False)
    else:
        return json.dumps(input_geom, ensure_ascii=False)


def export_to_wkt(input_geom: Dict[str, Any]) -> str:
    """
    导出为 WKT 格式

    Args:
        input_geom: GeoJSON 几何对象

    Returns:
        WKT 字符串
    """
    from shapely.geometry import shape

    geom = shape(input_geom)
    return geom.wkt


def create_point(lon: float, lat: float) -> Dict[str, Any]:
    """
    创建点几何对象

    Args:
        lon: 经度
        lat: 纬度

    Returns:
        GeoJSON Point
    """
    return {
        "type": "Point",
        "coordinates": [lon, lat]
    }


def create_polygon(coordinates: List[List[List[float]]]) -> Dict[str, Any]:
    """
    创建多边形几何对象

    Args:
        coordinates: 坐标数组 [[[外环坐标]], [[内环坐标]]]

    Returns:
        GeoJSON Polygon
    """
    return {
        "type": "Polygon",
        "coordinates": coordinates
    }


def create_linestring(coordinates: List[List[float]]) -> Dict[str, Any]:
    """
    创建线串几何对象

    Args:
        coordinates: 坐标数组 [[lon, lat], [lon, lat], ...]

    Returns:
        GeoJSON LineString
    """
    return {
        "type": "LineString",
        "coordinates": coordinates
    }


def get_bounds(input_geom: Dict[str, Any]) -> Dict[str, float]:
    """
    获取几何对象的边界框

    Args:
        input_geom: GeoJSON 几何对象

    Returns:
        {"minx": float, "miny": float, "maxx": float, "maxy": float}
    """
    from shapely.geometry import shape

    geom = shape(input_geom)
    bounds = geom.bounds  # (minx, miny, maxx, maxy)

    return {
        "minx": bounds[0],
        "miny": bounds[1],
        "maxx": bounds[2],
        "maxy": bounds[3]
    }


def get_area(input_geom: Dict[str, Any]) -> float:
    """
    计算几何对象的面积

    Args:
        input_geom: GeoJSON 几何对象

    Returns:
        面积（平方度，需乘以 111000^2 转换为平方米）
    """
    from shapely.geometry import shape

    geom = shape(input_geom)
    return geom.area


def get_length(input_geom: Dict[str, Any]) -> float:
    """
    计算几何对象的长度

    Args:
        input_geom: GeoJSON 几何对象

    Returns:
        长度（度，需乘以 111000 转换为米）
    """
    from shapely.geometry import shape

    geom = shape(input_geom)
    return geom.length


def simplify(input_geom: Dict[str, Any], tolerance: float) -> Dict[str, Any]:
    """
    简化几何对象（减少节点数量）

    Args:
        input_geom: GeoJSON 几何对象
        tolerance: 简化容差（度）

    Returns:
        简化后的 GeoJSON 几何对象
    """
    from shapely.geometry import shape, mapping

    geom = shape(input_geom)
    simplified = geom.simplify(tolerance, preserve_topology=True)
    return mapping(simplified)


def convex_hull(input_geom: Dict[str, Any]) -> Dict[str, Any]:
    """
    计算凸包

    Args:
        input_geom: GeoJSON 几何对象

    Returns:
        凸包 GeoJSON 几何对象
    """
    from shapely.geometry import shape, mapping

    geom = shape(input_geom)
    hull = geom.convex_hull
    return mapping(hull)


def envelope(input_geom: Dict[str, Any]) -> Dict[str, Any]:
    """
    计算最小外接矩形

    Args:
        input_geom: GeoJSON 几何对象

    Returns:
        矩形 GeoJSON Polygon
    """
    from shapely.geometry import shape, mapping

    geom = shape(input_geom)
    env = geom.envelope
    return mapping(env)


def difference(geom_a: Dict[str, Any], geom_b: Dict[str, Any]) -> Dict[str, Any]:
    """
    计算几何差集（A - B）

    Args:
        geom_a: GeoJSON 几何对象 A
        geom_b: GeoJSON 几何对象 B

    Returns:
        差集 GeoJSON 几何对象
    """
    from shapely.geometry import shape, mapping

    a = shape(geom_a)
    b = shape(geom_b)
    result = a.difference(b)
    return mapping(result)


def symmetric_difference(geom_a: Dict[str, Any], geom_b: Dict[str, Any]) -> Dict[str, Any]:
    """
    计算几何对称差集（A ⊕ B = (A - B) ∪ (B - A)）

    Args:
        geom_a: GeoJSON 几何对象 A
        geom_b: GeoJSON 几何对象 B

    Returns:
        对称差集 GeoJSON 几何对象
    """
    from shapely.geometry import shape, mapping

    a = shape(geom_a)
    b = shape(geom_b)
    result = a.symmetric_difference(b)
    return mapping(result)


# 批量操作算子

def batch_buffer(geometries: List[Dict[str, Any]], distance: float,
                 segments: int = 8) -> List[Dict[str, Any]]:
    """
    批量创建缓冲区

    Args:
        geometries: GeoJSON 几何对象列表
        distance: 缓冲距离（米）
        segments: 圆弧段数

    Returns:
        缓冲区 GeoJSON 列表
    """
    from spatial.operators import buffer

    results = []
    for geom in geometries:
        buffered = buffer(geom, distance, segments)
        results.append(buffered)

    return results


def batch_centroid(geometries: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """
    批量计算质心

    Args:
        geometries: GeoJSON 几何对象列表

    Returns:
        质心 GeoJSON 列表
    """
    from spatial.operators import centroid

    results = []
    for geom in geometries:
        center = centroid(geom)
        results.append(center)

    return results


# ========================================
# 测试代码
# ========================================

if __name__ == "__main__":
    print("测试数据源和输出算子")
    print("=" * 60)

    # 测试 1: 创建几何对象
    print("\n测试 1: 创建几何对象")
    point = create_point(116.404, 39.915)
    print(f"  Point: {point}")

    polygon = create_polygon([
        [[0, 0], [2, 0], [2, 2], [0, 2], [0, 0]]
    ])
    print(f"  Polygon: {polygon}")

    # 测试 2: WKT 转换
    print("\n测试 2: WKT 转换")
    wkt_text = "POINT (116.404 39.915)"
    geom = load_from_wkt(wkt_text)
    print(f"  WKT → GeoJSON: {geom}")

    wkt_out = export_to_wkt(geom)
    print(f"  GeoJSON → WKT: {wkt_out}")

    # 测试 3: 几何属性
    print("\n测试 3: 几何属性")
    bounds = get_bounds(polygon)
    print(f"  Bounds: {bounds}")

    area = get_area(polygon)
    print(f"  Area: {area} 平方度")

    # 测试 4: 几何操作
    print("\n测试 4: 几何操作")
    from spatial.operators import buffer

    buffered = buffer(point, 0.01, segments=8)
    hull = convex_hull(buffered)
    print(f"  Convex Hull: {hull['type']}")

    env = envelope(buffered)
    print(f"  Envelope: {env['type']}")

    # 测试 5: 批量操作
    print("\n测试 5: 批量操作")
    points = [
        create_point(116.40 + i * 0.01, 39.91 + i * 0.01)
        for i in range(3)
    ]

    buffers = batch_buffer(points, 0.005, segments=8)
    print(f"  批量缓冲区: {len(buffers)} 个")

    centroids = batch_centroid(buffers)
    print(f"  批量质心: {len(centroids)} 个")

    print("\n🎉 测试完成！")
