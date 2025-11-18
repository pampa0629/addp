"""
空间算子实现
使用 Shapely 库实现基础空间算子
"""

from typing import Dict, Any, List, Union
from shapely.geometry import shape, mapping
from shapely.ops import unary_union
import json


def buffer(input_geom: Dict[str, Any], distance: float, segments: int = 8) -> Dict[str, Any]:
    """
    缓冲区分析

    Args:
        input_geom: GeoJSON 格式的几何对象
        distance: 缓冲距离（米）
        segments: 圆弧段数

    Returns:
        GeoJSON 格式的缓冲区几何
    """
    geom = shape(input_geom)
    buffered = geom.buffer(distance, resolution=segments)
    return mapping(buffered)


def intersection(geom_a: Dict[str, Any], geom_b: Dict[str, Any]) -> Dict[str, Any]:
    """
    几何相交

    Args:
        geom_a: GeoJSON 格式的几何对象 A
        geom_b: GeoJSON 格式的几何对象 B

    Returns:
        GeoJSON 格式的交集几何
    """
    a = shape(geom_a)
    b = shape(geom_b)
    result = a.intersection(b)
    return mapping(result)


def union(geometries: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    几何合并

    Args:
        geometries: GeoJSON 格式的几何对象列表

    Returns:
        GeoJSON 格式的合并几何
    """
    shapes = [shape(g) for g in geometries]
    result = unary_union(shapes)
    return mapping(result)


def centroid(input_geom: Dict[str, Any]) -> Dict[str, Any]:
    """
    计算质心

    Args:
        input_geom: GeoJSON 格式的几何对象

    Returns:
        GeoJSON 格式的质心点
    """
    geom = shape(input_geom)
    center = geom.centroid
    return mapping(center)


def contains(geom_a: Dict[str, Any], geom_b: Dict[str, Any]) -> bool:
    """
    包含关系判断

    Args:
        geom_a: GeoJSON 格式的几何对象 A
        geom_b: GeoJSON 格式的几何对象 B

    Returns:
        A 是否包含 B
    """
    a = shape(geom_a)
    b = shape(geom_b)
    return a.contains(b)


def intersects(geom_a: Dict[str, Any], geom_b: Dict[str, Any]) -> bool:
    """
    相交关系判断

    Args:
        geom_a: GeoJSON 格式的几何对象 A
        geom_b: GeoJSON 格式的几何对象 B

    Returns:
        A 和 B 是否相交
    """
    a = shape(geom_a)
    b = shape(geom_b)
    return a.intersects(b)


def distance(geom_a: Dict[str, Any], geom_b: Dict[str, Any]) -> float:
    """
    距离计算

    Args:
        geom_a: GeoJSON 格式的几何对象 A
        geom_b: GeoJSON 格式的几何对象 B

    Returns:
        最短距离（米）
    """
    a = shape(geom_a)
    b = shape(geom_b)
    return a.distance(b)


# ========================================
# 测试函数
# ========================================

if __name__ == "__main__":
    # 测试缓冲区
    point = {
        "type": "Point",
        "coordinates": [116.404, 39.915]
    }

    buffered = buffer(point, 100.0, segments=16)
    print("Buffer result:")
    print(json.dumps(buffered, indent=2))

    # 测试交集
    poly_a = {
        "type": "Polygon",
        "coordinates": [[
            [0, 0], [2, 0], [2, 2], [0, 2], [0, 0]
        ]]
    }
    poly_b = {
        "type": "Polygon",
        "coordinates": [[
            [1, 1], [3, 1], [3, 3], [1, 3], [1, 1]
        ]]
    }

    result = intersection(poly_a, poly_b)
    print("\nIntersection result:")
    print(json.dumps(result, indent=2))