"""
几何处理算子模块

提供几何变换和处理算子：缓冲、相交、合并、质心、差集、简化、凸包、外接矩形
"""

import geopandas as gpd
import numpy as np
import pandas as pd
import shapely
from collections import Counter
from numbers import Real
from typing import List
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, OutputPort, register_operator
)


# ==================== 算子实现 ====================

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
    geom_col = result.geometry.name
    result[geom_col] = result.geometry.buffer(distance, resolution=resolution)
    return result


def intersection(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    几何相交（叠加分析）

    Args:
        input_gdf: 输入图层 A（主图层）
        gdf_b: 输入图层 B（叠加图层）

    Returns:
        GeoDataFrame 交集结果
    """
    return gpd.overlay(input_gdf, gdf_b, how='intersection')


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
    geom_col = result.geometry.name
    result[geom_col] = result.geometry.centroid
    return result


def difference(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    几何差集（A - B）

    Args:
        input_gdf: 被减图层（图层 A）
        gdf_b: 减去的图层（图层 B）

    Returns:
        GeoDataFrame 差集结果
    """
    return gpd.overlay(input_gdf, gdf_b, how='difference')


def simplify(input_gdf: gpd.GeoDataFrame, tolerance: float, preserve_topology: bool = True) -> gpd.GeoDataFrame:
    """
    简化几何（Douglas-Peucker 算法）

    Args:
        input_gdf: 输入 GeoDataFrame
        tolerance: 简化容差
        preserve_topology: 是否保持单个几何的拓扑

    Returns:
        GeoDataFrame 简化结果
    """
    result = input_gdf.copy()
    geom_col = result.geometry.name
    result[geom_col] = result.geometry.simplify(tolerance, preserve_topology=preserve_topology)
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
    geom_col = result.geometry.name
    result[geom_col] = result.geometry.convex_hull
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
    geom_col = result.geometry.name
    result[geom_col] = result.geometry.envelope
    return result


def dissolve(input_gdf: gpd.GeoDataFrame, by: str = None) -> gpd.GeoDataFrame:
    """
    融合几何（按字段分组融合或全局融合）

    Args:
        input_gdf: 输入 GeoDataFrame
        by: 分组字段名，None 表示全局融合为单个几何

    Returns:
        GeoDataFrame 融合结果
    """
    if by is None:
        # 全局融合：所有要素合并为一个几何
        from shapely.ops import unary_union
        merged_geom = unary_union(input_gdf.geometry)
        result = gpd.GeoDataFrame(geometry=[merged_geom], crs=input_gdf.crs)
    else:
        result = input_gdf.dissolve(by=by).reset_index()
    return result


def _active_geometry(input_gdf: gpd.GeoDataFrame, name: str = "input_gdf") -> gpd.GeoSeries:
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise ValueError(f"{name} must be a GeoDataFrame")
    try:
        return input_gdf.geometry
    except AttributeError as exc:
        raise ValueError(f"{name} requires an active geometry column") from exc


def _finite_xy_geometry(input_gdf: gpd.GeoDataFrame, name: str = "input_gdf") -> gpd.GeoSeries:
    geometry = _active_geometry(input_gdf, name)
    if shapely.has_z(geometry.array).any() or shapely.has_m(geometry.array).any():
        raise ValueError(f"{name} requires two-dimensional XY geometries")
    if not np.isfinite(shapely.get_coordinates(geometry.array)).all():
        raise ValueError(f"{name} requires finite XY coordinates")
    return geometry


def check_geometry(
    input_gdf: gpd.GeoDataFrame,
    valid_field: str = "geometry_valid",
    reason_field: str = "geometry_reason",
) -> gpd.GeoDataFrame:
    """Annotate each source feature's planar validity without changing geometry."""
    geometry = _finite_xy_geometry(input_gdf)
    fields = (valid_field, reason_field)
    if any(not isinstance(field, str) or not field or field != field.strip() for field in fields):
        raise ValueError("output field names must be non-empty strings without surrounding whitespace")
    if valid_field == reason_field or any(field in input_gdf.columns for field in fields):
        raise ValueError("output field names must be distinct and must not overwrite source fields")
    result = input_gdf.copy()
    result[valid_field] = shapely.is_valid(geometry.array)
    reasons = shapely.is_valid_reason(geometry.array)
    result[reason_field] = pd.array(
        ["Missing Geometry" if reason is None else reason for reason in reasons], dtype="string",
    )
    return result


def repair_geometry(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Repair invalid XY geometries using linework, retaining collapsed components."""
    geometry = _finite_xy_geometry(input_gdf)
    values = geometry.array.copy()
    invalid = ~shapely.is_missing(values) & ~shapely.is_valid(values)
    values[invalid] = shapely.make_valid(values[invalid], method="linework", keep_collapsed=True)
    if (~shapely.is_missing(values) & ~shapely.is_valid(values)).any():
        raise ValueError("geometry repair did not produce valid geometries")
    result = input_gdf.copy()
    result[result.geometry.name] = values
    return result


def _require_projected_xy_crs(crs) -> None:
    if crs is None or not crs.is_projected:
        raise ValueError("input requires a known projected CRS; reproject explicitly")
    units = [axis.unit_conversion_factor for axis in crs.axis_info[:2]]
    if len(units) != 2 or not all(np.isfinite(unit) and unit > 0 for unit in units) or units[0] != units[1]:
        raise ValueError("projected CRS requires matching known horizontal coordinate units")


def _source_identifiers(input_gdf: gpd.GeoDataFrame, id_field: str) -> pd.Series:
    if (not isinstance(id_field, str) or not id_field or not input_gdf.columns.is_unique
            or id_field not in input_gdf.columns or isinstance(input_gdf[id_field].dtype, gpd.array.GeometryDtype)):
        raise ValueError("id_field must explicitly name one existing non-geometry scalar column")
    ids = input_gdf[id_field]
    if not all(pd.api.types.is_scalar(value) for value in ids):
        raise ValueError("id_field requires non-empty unique scalar identifiers")
    if ids.isna().any() or any(isinstance(value, str) and not value.strip() for value in ids) or ids.duplicated().any():
        raise ValueError("id_field requires non-empty unique scalar identifiers")
    return ids


def check_dangling_points(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report degree-one planar network endpoints with all original source IDs."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    if geometry.isna().any() or not geometry.geom_type.isin(["LineString", "MultiLineString"]).all():
        raise ValueError("input_gdf requires non-null LineString or MultiLineString geometries")
    parts = shapely.get_parts(geometry.array)
    if (shapely.is_empty(geometry.array).any() or shapely.is_empty(parts).any()
            or not shapely.is_valid(geometry.array).all()):
        raise ValueError("input_gdf requires non-empty valid lines; repair explicitly before checking endpoints")

    # Use the same exact noding as polygonize; coincident/reversed edges count
    # once, while the two ends of a closed edge both contribute to its degree.
    network = shapely.node(shapely.multilinestrings(parts))
    degrees = Counter(endpoint for edge in shapely.get_parts(network)
                      for endpoint in (edge.coords[0], edge.coords[-1]))
    tree = shapely.STRtree(geometry.array)
    associations = []
    for coordinate, degree in degrees.items():
        if degree != 1:
            continue
        point = shapely.Point(coordinate)
        sources = tree.query(point, predicate="intersects")
        if not len(sources):
            raise ValueError("dangling endpoint cannot be associated with an original source line")
        associations.extend((int(source), coordinate) for source in sources)
    associations.sort(key=lambda item: (item[0], *item[1]))
    return gpd.GeoDataFrame(
        {"source_id": ids.iloc[[source for source, _ in associations]].reset_index(drop=True)},
        geometry=gpd.GeoSeries([shapely.Point(point) for _, point in associations], crs=geometry.crs),
    )


def check_polygon_overlap(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report each positive-area polygon pair once, retaining explicit source IDs."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    if geometry.isna().any() or not geometry.geom_type.isin(["Polygon", "MultiPolygon"]).all():
        raise ValueError("input_gdf requires non-null Polygon or MultiPolygon geometries")
    if not shapely.is_valid(geometry.array).all():
        raise ValueError("input_gdf requires valid polygons; repair explicitly before checking overlaps")

    left, right, areas, overlaps = [], [], [], []
    tree = shapely.STRtree(geometry.array)
    for a, shape in enumerate(geometry.array):
        candidates = tree.query(shape)
        # Position-based ordering works for arbitrary unique identifiers and
        # duplicate DataFrame indices, independently of STRtree traversal order.
        for b in np.sort(candidates[candidates > a]):
            intersection = shapely.intersection(shape, geometry.array[b])
            polygons, pending = [], [intersection]
            while pending:
                part = pending.pop()
                if part.geom_type == "Polygon" and not part.is_empty:
                    polygons.append(part)
                elif part.geom_type in {"MultiPolygon", "GeometryCollection"}:
                    pending.extend(part.geoms)
            if not polygons:
                continue
            overlap_geometry = shapely.multipolygons(polygons)
            area = shapely.area(overlap_geometry)
            if not np.isfinite(area) or area <= 0:
                raise ValueError("overlap requires a finite positive Float64 area")
            left.append(a)
            right.append(b)
            areas.append(area)
            overlaps.append(overlap_geometry)
    return gpd.GeoDataFrame(
        {"source_id_a": ids.iloc[left].reset_index(drop=True),
         "source_id_b": ids.iloc[right].reset_index(drop=True),
         "overlap_area": pd.Series(areas, dtype="float64")},
        geometry=gpd.GeoSeries(overlaps, crs=geometry.crs),
    )


def check_polygon_gap(input_gdf: gpd.GeoDataFrame, coverage_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Report uncovered polygon components inside an explicit coverage boundary."""
    geometry = _finite_xy_geometry(input_gdf)
    coverage = _finite_xy_geometry(coverage_gdf, "coverage_gdf")
    for name, values in [("input_gdf", geometry), ("coverage_gdf", coverage)]:
        _require_projected_xy_crs(values.crs)
        if values.isna().any() or not values.geom_type.isin(["Polygon", "MultiPolygon"]).all():
            raise ValueError(f"{name} requires non-null Polygon or MultiPolygon geometries")
        if not shapely.is_valid(values.array).all():
            raise ValueError(f"{name} requires valid polygons; repair explicitly before checking gaps")
    if not geometry.crs.equals(coverage.crs):
        raise ValueError("both inputs require the same known projected CRS; reproject explicitly")
    if coverage.empty or coverage.is_empty.all():
        raise ValueError("coverage_gdf requires at least one non-empty polygon")

    # Reuse the existing global dissolve, without per-feature overlay attributes
    # or implicit make_valid. A coverage hole is an explicit excluded region.
    uncovered = shapely.difference(
        dissolve(coverage_gdf).geometry.iloc[0], dissolve(input_gdf).geometry.iloc[0],
    )
    parts = [shapely.normalize(part) for part in shapely.get_parts(uncovered) if not part.is_empty]
    if any(part.geom_type != "Polygon" or not part.is_valid for part in parts):
        raise ValueError("gap difference did not produce valid polygon components")
    parts.sort(key=lambda part: (*part.bounds, shapely.to_wkb(part, output_dimension=2)))
    areas = shapely.area(parts)
    if not np.isfinite(areas).all() or (areas <= 0).any():
        raise ValueError("gap requires a finite positive Float64 area")
    return gpd.GeoDataFrame(
        {"gap_area": pd.Series(areas, dtype="float64")},
        geometry=gpd.GeoSeries(parts, crs=geometry.crs),
    )


def snap_geometry(
    input_gdf: gpd.GeoDataFrame,
    reference_gdf: gpd.GeoDataFrame,
    tolerance: float,
) -> gpd.GeoDataFrame:
    """Snap against a fixed reference vertex set in shared projected CRS units."""
    geometry = _finite_xy_geometry(input_gdf)
    reference = _finite_xy_geometry(reference_gdf, "reference_gdf")
    if isinstance(tolerance, (bool, np.bool_)) or not isinstance(tolerance, Real):
        raise ValueError("tolerance must be a finite non-negative number in CRS units")
    try:
        tolerance = float(tolerance)
    except OverflowError as exc:
        raise ValueError("tolerance must be a finite non-negative number in CRS units") from exc
    if not np.isfinite(tolerance) or tolerance < 0:
        raise ValueError("tolerance must be a finite non-negative number in CRS units")
    crs = geometry.crs
    if crs is None or reference.crs is None or not crs.is_projected or not crs.equals(reference.crs):
        raise ValueError("both inputs require the same known projected CRS; reproject explicitly")
    _require_projected_xy_crs(crs)
    for name, values in [("input_gdf", geometry.array), ("reference_gdf", reference.array)]:
        if (~shapely.is_missing(values) & ~shapely.is_valid(values)).any():
            raise ValueError(f"{name} requires valid geometries; repair explicitly before snapping")
    # Query immutable original coordinates once. GEOS snap can move a vertex
    # repeatedly along reference chains and exceed its original tolerance.
    vertices = np.unique(shapely.get_coordinates(reference.array), axis=0)
    coordinates = shapely.get_coordinates(geometry.array)
    snapped = coordinates.copy()
    if tolerance > 0 and len(vertices) and len(coordinates):
        tree = shapely.STRtree(shapely.points(vertices))
        # Envelope lookup avoids squared-distance overflow/underflow in native
        # nearest queries. Only original XY distances below decide candidates.
        with np.errstate(over="ignore"):
            limit = np.finfo(float).max
            lower = np.maximum(coordinates - tolerance, -limit)
            upper = np.minimum(coordinates + tolerance, limit)
        source_ids, reference_ids = tree.query(shapely.box(
            lower[:, 0], lower[:, 1], upper[:, 0], upper[:, 1],
        ))
        # Independently check original XY distances; index traversal order and
        # inclusive search envelopes must not determine the movement boundary.
        with np.errstate(over="ignore", invalid="ignore"):
            delta = vertices[reference_ids] - coordinates[source_ids]
            distances = np.hypot(delta[:, 0], delta[:, 1])
        within = distances < tolerance
        source_ids, reference_ids, distances = source_ids[within], reference_ids[within], distances[within]
        order = np.lexsort((reference_ids, distances, source_ids))
        source_ids, reference_ids = source_ids[order], reference_ids[order]
        _, first = np.unique(source_ids, return_index=True)
        snapped[source_ids[first]] = vertices[reference_ids[first]]
    values = shapely.set_coordinates(geometry.array.copy(), snapped)
    if (~shapely.is_missing(values) & ~shapely.is_valid(values)).any():
        raise ValueError("snapping produced invalid geometries; reduce tolerance or revise reference")
    result = input_gdf.copy()
    result[result.geometry.name] = values
    return result


def polygonize(input_gdf: gpd.GeoDataFrame) -> dict[str, gpd.GeoDataFrame]:
    """Build planar faces and report unused edges without changing source linework."""
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise ValueError("input_gdf must be a GeoDataFrame")
    try:
        geometry = input_gdf.geometry
    except AttributeError as exc:
        raise ValueError("input_gdf requires an active geometry column") from exc

    if geometry.isna().any() or not geometry.geom_type.isin(["LineString", "MultiLineString"]).all():
        raise ValueError("input_gdf requires non-null LineString or MultiLineString geometries")
    parts = shapely.get_parts(geometry.array)
    if shapely.is_empty(geometry.array).any() or shapely.is_empty(parts).any():
        raise ValueError("input_gdf must not contain empty line geometries")
    if shapely.has_z(parts).any() or shapely.has_m(parts).any():
        raise ValueError("input_gdf requires two-dimensional XY line geometries")
    if not np.isfinite(shapely.get_coordinates(parts)).all() or not shapely.is_valid(parts).all():
        raise ValueError("input_gdf requires valid lines with finite coordinates")

    # Node the complete network once; this inserts real intersections and removes
    # overlapping segments without introducing a snapping distance.
    network = shapely.node(shapely.multilinestrings(parts))
    faces, cuts, dangles, invalid = shapely.polygonize_full(shapely.get_parts(network))
    diagnostic_geometries, issue_types = [], []
    for issue_type, collection in [("dangle", dangles), ("cut_edge", cuts), ("invalid_ring", invalid)]:
        edges = list(shapely.get_parts(collection))
        diagnostic_geometries.extend(edges)
        issue_types.extend([issue_type] * len(edges))

    return {
        "result": gpd.GeoDataFrame(geometry=gpd.GeoSeries(shapely.get_parts(faces), crs=geometry.crs)),
        "diagnostics": gpd.GeoDataFrame(
            {"issue_type": pd.Series(issue_types, dtype="string")},
            geometry=gpd.GeoSeries(diagnostic_geometries, crs=geometry.crs),
        ),
    }


# ==================== 元数据定义 ====================

CHECK_DANGLING_POINTS_METADATA = OperatorMetadata(
    name="check_dangling_points", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="悬点检查", brief_description="报告平面线网中度为一的端点及原始来源，不自动改线",
    execution_modes=["workflow"], effects=["read"],
    overview="按真实 XY 交点打断并去除重合段后计算节点度，将悬挂端点精确关联回原始要素。"
             "合法道路尽头也会报告，不自动判断业务质量或立交层级。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的单个线图层", notes="有效有限二维 LineString/MultiLineString，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何标量列，值非空且唯一；不推断主键或使用行索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id/Point，悬点逐来源输出，保留来源类型和 CRS", is_default=True)],
    use_cases=["定位线网断头及其原始要素", "检查孤立线端点后由业务确认是否应连接"],
    notes=["真实交点相连，重合段不重复计度，闭环端点计两次", "同点逐来源报告，同一源只报告一次；按源行和 XY 稳定排序", "不使用距离容差，不吸附、延伸、删线、重投影或改写源", "不生成 Quality 结论；保存另接 save，线网及索引驻内存，不承诺固定容量"],
    workflow_example={"id": "check_dangles", "operator": "check_dangling_points", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id"}, "depends_on": ["load"]},
)

CHECK_POLYGON_OVERLAP_METADATA = OperatorMetadata(
    name="check_polygon_overlap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="面叠检查", brief_description="逐对提取面图层正面积重叠，保留双方来源标识",
    execution_modes=["workflow"], effects=["read"],
    overview="检查单图层内有效二维面的重叠，每对要素输出一行重叠面、双方来源标识及面积。"
             "包含和完全重复同样报告，共边共点不报告，不修改源图层或自动修复。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的单个面图层", notes="有效有限二维 Polygon/MultiPolygon，已知投影 CRS，空面不产生重叠"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="必须是现有非几何标量列，值非空且唯一；不推断主键或使用行索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id_a/source_id_b/overlap_area/MultiPolygon，源 CRS", is_default=True)],
    use_cases=["查找地块重叠并定位双方来源", "识别完全重复或被其他面包含的图斑"],
    notes=["每对按原始行位置只报告一次，多处重叠保留在同一 MultiPolygon", "面积采用源投影水平单位的平方，不自动换算平方米或忽略微小重叠", "保留来源标识类型和源 CRS，不复制其他业务属性、不修改源", "不检查缺口、不决定重叠归属或生成 Quality 质量结论；保存另接 save", "索引及结果驻内存，不承诺固定容量"],
    workflow_example={"id": "check_overlaps", "operator": "check_polygon_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "parcel_id"}, "depends_on": ["load"]},
)

CHECK_POLYGON_GAP_METADATA = OperatorMetadata(
    name="check_polygon_gap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="面缝检查", brief_description="检查显式覆盖范围内未被面图层覆盖的区域，只报告不填补",
    execution_modes=["workflow"], effects=["read"],
    overview="将覆盖范围与待检查面分别整体融合，计算范围减覆盖，每个独立缺口输出一行面及面积。"
             "范围孔洞表示合法留白，范围外不报告；不修改源或推断相邻要素归属。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的面图层", notes="有效有限二维 Polygon/MultiPolygon；空批或空面按实际覆盖计算"),
        OperatorParam(name="coverage_gdf", type="input", data_type="GeoDataFrame", required=True, description="必须显式提供的应覆盖范围", notes="与输入相同的已知投影 CRS，至少一个非空有效二维面；孔洞为合法留白"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="gap_area/Polygon，每个独立缺口一行，保留源 CRS", is_default=True)],
    use_cases=["检查地块在指定管理范围内的覆盖缺口", "检查边缘缺口并排除明确的合法留白"],
    notes=["所有正面积缺口均报告，不忽略微小缝隙、不自动填补或分配归属", "面积采用源投影水平单位的平方，不自动换算平方米", "范围及待检查面分别整体融合，重叠或重复不重复报缺口", "不复制业务属性、不生成 Quality 质量结论；保存另接 save", "融合及差集驻内存，不承诺固定容量"],
    workflow_example={"id": "check_gaps", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "load"}, "coverage_gdf": {"$ref": "coverage"}}, "depends_on": ["load", "coverage"]},
)

CHECK_GEOMETRY_METADATA = OperatorMetadata(
    name="check_geometry", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="几何检查", brief_description="逐要素检查平面几何有效性，保留原要素并标注原因",
    execution_modes=["workflow"], effects=["read"],
    overview="按 GEOS 平面有效性规则检查每个几何，在原数据副本中增加有效性和原因字段。"
             "保留属性、行顺序、几何及源坐标系，不判断要素间的重叠或缝隙。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的二维矢量要素", notes="要求有限 XY 坐标，不接受 Z/M"),
        OperatorParam(name="valid_field", type="param", data_type="str", required=False, default="geometry_valid", description="有效性结果字段名", notes="不能覆盖已有字段或与原因字段同名"),
        OperatorParam(name="reason_field", type="param", data_type="str", required=False, default="geometry_reason", description="有效性原因字段名", notes="原因保留 GEOS 原始诊断文本"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="原要素及有效性、原因字段", is_default=True)],
    use_cases=["定位自相交几何", "修复前后对比几何有效性"],
    notes=["空几何有效，缺失几何无效；不丢弃任何记录", "仅检查有限 XY 几何；Z/M 和非有限坐标明确拒绝", "输出字段名冲突时失败，不自动改名或覆盖业务属性"],
    workflow_example={"id": "check", "operator": "check_geometry", "params": {"input_gdf": {"$ref": "load"}}, "depends_on": ["load"]},
)

REPAIR_GEOMETRY_METADATA = OperatorMetadata(
    name="repair_geometry", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="几何修复", brief_description="修复单个无效几何，完整保留多部件及退化部分",
    execution_modes=["workflow"], effects=["read"],
    overview="以 linework 方法修复二维无效几何，保留原边线形成的多部件、几何集合和退化线点。"
             "一条输入仍对应一条输出，保留源属性和坐标系，不自动吸附或处理图层间拓扑。",
    params=[OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待修复的二维矢量要素", notes="拒绝非有限 XY 坐标和 Z/M，不静默降维")],
    output_ports=[OutputPort(name="result", type="geodataframe", description="修复后的完整几何及原属性", is_default=True)],
    use_cases=["修复自相交面", "几何检查后修复并复查"],
    notes=["固定 linework 和 keep_collapsed=True，不提供另一套推断修复路线", "合法几何、缺失值和空几何保持原样；缺失值须另行补充或过滤", "可能输出多面、GeometryCollection 或线点，不自动拆行或丢弃低维部分", "不补缝、不吸附、不纠正跨要素重叠；混合几何保存受目标格式约束"],
    workflow_example={"id": "repair", "operator": "repair_geometry", "params": {"input_gdf": {"$ref": "check"}}, "depends_on": ["check"]},
)

SNAP_GEOMETRY_METADATA = OperatorMetadata(
    name="snap_geometry", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="参考吸附", brief_description="按显式容差吸向固定参考顶点，保留输入属性和行数",
    execution_modes=["workflow"], effects=["read"],
    overview="每个输入原始顶点按原始位置一次匹配最近参考顶点，等距按参考 X、再 Y 升序决胜。参考保持固定，保留输入属性、行顺序和坐标系。"
             "两输入须使用相同的已知投影坐标系，容差采用其水平坐标单位。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待吸附的二维矢量图层", notes="仅接受有效且坐标有限的 XY 几何，缺失和空几何保留"),
        OperatorParam(name="reference_gdf", type="input", data_type="GeoDataFrame", required=True, description="保持固定的参考图层", notes="只使用原有顶点，不吸向线段上的任意位置；空参考不移动输入"),
        OperatorParam(name="tolerance", type="param", data_type="float", required=True, description="原始位置吸附容差", notes="有限非负数，单位与投影 CRS 一致；原始距离须严格小于正容差，禁止连锁越界"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="吸附后的输入副本及原属性", is_default=True)],
    use_cases=["将偏移端点吸向已有参考节点", "按固定基准整理矢量顶点后复查几何"],
    notes=["不自动重投影；地理、未知或不匹配坐标系明确拒绝", "只移动原有输入顶点，不插点或删点；参考顶点按 XY 去重排序", "原始位置只匹配一次，不按已移动位置再次吸附；零容差不移动", "无效输入或无效结果整批失败，不自动修复、删行或回退", "不保证消除缝隙或重叠，不提供单图层互吸、端点延伸；参考索引及坐标驻内存，保存另接既有保存节点"],
    workflow_example={"id": "snap", "operator": "snap_geometry", "params": {"input_gdf": {"$ref": "load"}, "reference_gdf": {"$ref": "reference"}, "tolerance": 0.5}, "depends_on": ["load", "reference"]},
)

POLYGONIZE_METADATA = OperatorMetadata(
    name="polygonize",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="拓扑构面",
    brief_description="从二维线网提取闭合面，并输出未参与构面的线诊断",
    execution_modes=["workflow"],
    effects=["read"],
    overview="将多个线要素组成的平面线网按真实交点打断并去除重合线段，提取每个闭合区域。"
             "面结果和余线诊断使用独立端口，保留源坐标系，不修改源数据或继承源线属性。",
    params=[OperatorParam(
        name="input_gdf", type="input", data_type="GeoDataFrame", required=True,
        description="二维 LineString / MultiLineString 线网",
        notes="允许空批；非空批拒绝空值、空几何、Z/M、非有限坐标及无效线",
    )],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="构面结果，仅含面几何", is_default=True),
        OutputPort(name="diagnostics", type="geodataframe", description="余线诊断，含 issue_type 和线几何", is_default=False),
    ],
    use_cases=["将地块边界线网构建为面", "从道路闭合区域生成街区面", "定位未参与构面的余线"],
    notes=[
        "不自动吸附、补缝、延伸或删除短线；无闭合区域时输出空面",
        "保留嵌套闭合区域及孔洞，不继承源线业务属性",
        "诊断类型为 dangle（悬挂或孤立余线）、cut_edge（连接但不构面的边）、invalid_ring（无效环）",
        "诊断对应打断去重后的线网，不提供原始线 ID，不等同完整拓扑质检",
        "两端口可分别连接保存节点；完整线网在内存中计算，几何规模影响内存消耗",
    ],
    workflow_example={
        "id": "build_faces", "operator": "polygonize",
        "params": {"input_gdf": {"$ref": "load_boundaries"}}, "depends_on": ["load_boundaries"],
    },
)

BUFFER_METADATA = OperatorMetadata(
    name="buffer",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="缓冲区分析",
    brief_description="在几何对象周围创建指定距离的缓冲区,常用于影响范围分析和邻域查询",
    execution_modes=["workflow"],
    effects=["read"],

    overview="在输入几何对象周围创建指定距离的缓冲区多边形。支持点、线、面等各类几何类型,缓冲距离可正可负(负值仅对面有效,表示内缩)。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="distance",
            type="param",
            data_type="float",
            required=True,
            description="缓冲距离,单位与坐标系一致",
            notes="投影坐标系单位为米,地理坐标系单位为度(不推荐,建议先转投影坐标系)"
        ),
        OperatorParam(
            name="resolution",
            type="param",
            data_type="integer",
            required=False,
            description="缓冲区圆弧段数",
            notes="值越大缓冲区越平滑但计算越慢,建议范围 8-32"
        )
    ],

    use_cases=[
        "河流污染影响范围: 河流中心线缓冲500米",
        "学校服务范围: 学校点位缓冲1000米",
        "道路噪音影响区: 道路线缓冲200米",
        "保护区核心区: 边界内缩100米(负值缓冲)"
    ],

    notes=[
        "地理坐标系(EPSG:4326)需先用 to_crs 转投影坐标系再缓冲",
        "大数据集建议先用空间过滤减少要素数量再缓冲",
        "负值缓冲仅对面几何有效,点和线无效",
        "缓冲结果可能产生自相交,建议后续用 simplify 简化",
        "重要：多要素(如铁路、道路多条线段)缓冲后各自独立，相邻缓冲区可能重叠。如需后续用 intersection 统计面积，必须先用 dissolve 融合缓冲区，否则重叠区域将被重复计算，导致面积结果偏大"
    ],

    workflow_example={
        'id': 'buffer_rivers',
        'operator': 'buffer',
        'params': {
            'input_gdf': {'$ref': 'load_rivers'},
            'distance': 500,
            'resolution': 16
        },
        'depends_on': ['load_rivers']
    }
)


INTERSECTION_METADATA = OperatorMetadata(
    name="intersection",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_OVERLAY,
    description="几何相交",
    brief_description="计算两个几何图层的空间交集,提取重叠区域,常用于叠加分析",
    execution_modes=["workflow"],
    effects=["read"],

    overview="计算输入图层A与图层B的几何交集,返回两者重叠的部分。结果保留图层A的属性,可选择性保留图层B的属性。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入图层A(被裁剪对象)"
        ),
        OperatorParam(
            name="gdf_b",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入图层B(裁剪对象)"
        )
    ],

    use_cases=[
        "土地利用分析: 提取保护区内的耕地",
        "行政区划统计: 提取某市范围内的河流",
        "灾害风险评估: 提取洪水淹没区内的建筑物",
        "多图层叠加: 提取同时满足多个条件的区域"
    ],

    notes=[
        "两个图层的坐标系必须一致,不一致需先用 to_crs 统一",
        "结果只包含有交集的要素,无交集的要素会被剔除",
        "如果需要保留无交集的要素,使用 spatial_join 代替",
        "大数据集建议先用空间索引优化性能"
    ],

    workflow_example={
        'id': 'intersect_protected_area',
        'operator': 'intersection',
        'params': {
            'input_gdf': {'$ref': 'load_farmland'},
            'gdf_b': {'$ref': 'load_protected_area'}
        },
        'depends_on': ['load_farmland', 'load_protected_area']
    }
)


UNION_METADATA = OperatorMetadata(
    name="union",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_OVERLAY,
    description="几何合并",
    brief_description="将多个图层的几何对象合并为一个图层,常用于数据整合和范围叠加",
    execution_modes=["workflow"],
    effects=["read"],

    overview="将多个GeoDataFrame的所有几何对象合并到一个GeoDataFrame中。所有输入图层的要素会保留在输出中,属性也会保留。适用于多数据源整合、多时相数据合并等场景。",

    params=[
        OperatorParam(
            name="gdf_list",
            type="input",
            data_type="List[GeoDataFrame]",
            required=True,
            description="GeoDataFrame列表,包含需要合并的多个图层",
            notes="所有输入图层应具有相同的坐标系,否则建议先用to_crs统一坐标系"
        )
    ],

    use_cases=[
        "多城市数据整合: 将各市的建筑物数据合并为全省数据",
        "历史数据汇总: 将多年的土地利用变化数据合并为时序数据集",
        "分区数据拼接: 将分区采集的GPS轨迹点合并为完整轨迹",
        "多源数据融合: 将不同部门提供的POI数据整合为统一数据库"
    ],

    notes=[
        "合并后的属性列为所有输入图层属性列的并集,缺失值填充为NaN",
        "如果输入图层坐标系不一致,会导致几何错误,需先统一坐标系",
        "不同于dissolve,union不会融合几何,只是简单拼接",
        "大数据集合并可能消耗大量内存,建议分批处理"
    ],

    workflow_example={
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
)


CENTROID_METADATA = OperatorMetadata(
    name="centroid",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="计算质心",
    brief_description="计算几何对象的质心(中心点),常用于位置标注和点化处理",
    execution_modes=["workflow"],
    effects=["read"],

    overview="计算每个几何对象的质心坐标,将面或线转换为点。质心是几何对象的几何中心,适用于标注位置、聚合分析等场景。",

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
        "行政区标注: 计算省市边界质心用于地图标注",
        "建筑物点化: 将建筑物面转为中心点用于密度分析",
        "河流中心线提取: 计算河流面的中心线用于网络分析",
        "地块代表点: 为每个地块生成代表点用于空间连接"
    ],

    notes=[
        "质心可能位于几何对象外部(如环形面),如需保证点在内部请使用representative_point",
        "线的质心是沿线中点,不一定是线的端点",
        "点的质心就是自身",
        "质心坐标单位与输入坐标系一致"
    ],

    workflow_example={
        'id': 'get_province_centers',
        'operator': 'centroid',
        'params': {
            'input_gdf': {'$ref': 'load_provinces'}
        },
        'depends_on': ['load_provinces']
    }
)


DIFFERENCE_METADATA = OperatorMetadata(
    name="difference",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_OVERLAY,
    description="几何差集",
    brief_description="计算两个图层的几何差集(A-B),保留A中不与B重叠的部分",
    execution_modes=["workflow"],
    effects=["read"],

    overview="从图层A中减去图层B的重叠部分,返回A中独有的几何区域。常用于排除分析、净用地计算等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="被减图层(图层A)"
        ),
        OperatorParam(
            name="gdf_b",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="减去的图层(图层B)",
            notes="需与input_gdf具有相同的坐标系"
        )
    ],

    use_cases=[
        "净用地计算: 建设用地减去道路用地得到净建设用地",
        "排除保护区: 可开发区域减去自然保护区得到可用区域",
        "去除已建区: 规划范围减去已建成区得到待开发区",
        "水体擦除: 行政区边界减去水体得到陆地范围"
    ],

    notes=[
        "结果保留input_gdf的属性,gdf_b的属性不保留",
        "如果A与B完全不相交,返回A的原始几何",
        "如果A完全被B包含,返回空几何",
        "大数据集差集运算较慢,建议先用空间索引过滤"
    ],

    workflow_example={
        'id': 'exclude_protected',
        'operator': 'difference',
        'params': {
            'input_gdf': {'$ref': 'load_land'},
            'gdf_b': {'$ref': 'load_protected'}
        },
        'depends_on': ['load_land', 'load_protected']
    }
)


SIMPLIFY_METADATA = OperatorMetadata(
    name="simplify",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="简化几何",
    brief_description="简化几何对象的顶点数量,减小数据量,常用于地图制图和性能优化",
    execution_modes=["workflow"],
    effects=["read"],

    overview="使用Douglas-Peucker算法简化几何对象,减少顶点数量同时保持形状特征。适用于大比例尺制图、Web地图加载优化等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="tolerance",
            type="param",
            data_type="float",
            required=True,
            description="简化容差,单位与坐标系一致",
            notes="值越大简化越激进。投影坐标系单位为米,建议10-100;地理坐标系单位为度,建议0.0001-0.001"
        ),
        OperatorParam(
            name="preserve_topology",
            type="param",
            data_type="boolean",
            required=False,
            description="是否保持单个几何的拓扑",
            notes="True时保留单个几何拓扑，不修复原有无效几何；False时可能产生无效几何"
        )
    ],

    use_cases=[
        "Web地图优化: 简化省市边界用于在线地图快速加载",
        "制图综合: 将1:1万地图简化为1:50万比例尺",
        "数据压缩: 减少GPS轨迹点数量降低存储空间",
        "性能优化: 简化复杂面几何加速空间运算"
    ],

    notes=[
        "tolerance过大会导致几何变形严重甚至崩溃",
        "地理坐标系(EPSG:4326)简化效果较差,建议先转投影坐标系",
        "preserve_topology=True仅保持单个几何拓扑，不保证邻接面无缝隙或重叠",
        "简化后的几何可能不满足原始精度要求,需权衡数据量和精度"
    ],

    workflow_example={
        'id': 'simplify_boundaries',
        'operator': 'simplify',
        'params': {
            'input_gdf': {'$ref': 'load_provinces'},
            'tolerance': 100,
            'preserve_topology': True
        },
        'depends_on': ['load_provinces']
    }
)


CONVEX_HULL_METADATA = OperatorMetadata(
    name="convex_hull",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="凸包",
    brief_description="计算几何对象的最小凸包(凸多边形),常用于范围分析和形状概括",
    execution_modes=["workflow"],
    effects=["read"],

    overview="计算包含几何对象所有顶点的最小凸多边形。凸包类似用橡皮筋包裹几何对象的外轮廓,适用于范围估算、离群点检测等场景。",

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
        "活动范围估算: 计算GPS轨迹点的凸包估算人员活动范围",
        "建筑群轮廓: 计算建筑物点集的凸包生成建筑群外轮廓",
        "数据质量检查: 检测离群点(凸包面积异常大)",
        "服务范围概算: 用设施点的凸包快速估算覆盖范围"
    ],

    notes=[
        "凸包会忽略凹陷部分,不能精确表示复杂形状",
        "如需精确边界,建议使用alpha_shape或buffer",
        "点的凸包是点本身,线的凸包可能是线或面",
        "凸包面积通常大于原始几何面积"
    ],

    workflow_example={
        'id': 'get_activity_range',
        'operator': 'convex_hull',
        'params': {
            'input_gdf': {'$ref': 'load_gps_points'}
        },
        'depends_on': ['load_gps_points']
    }
)


DISSOLVE_METADATA = OperatorMetadata(
    name="dissolve",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="融合几何",
    brief_description="将多个几何对象融合为一个（或按字段分组融合），消除重叠，常用于缓冲区合并后的去重",
    execution_modes=["workflow"],
    effects=["read"],

    overview="将图层内的所有几何对象融合为单一几何（全局融合），或按指定字段分组融合。是缓冲区分析中消除重叠的关键步骤，确保后续面积统计不重复计算。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="by",
            type="param",
            data_type="string",
            required=False,
            description="分组字段名，不填则全局融合为单个几何",
            notes="全局融合时所有要素合并为一个几何，消除重叠区域"
        )
    ],

    use_cases=[
        "缓冲区合并: 铁路/道路多段缓冲后融合，避免重叠重复计算",
        "行政区合并: 将多个区县融合为市级边界",
        "土地利用汇总: 将同类型地块融合为整体",
        "影响范围合并: 多个设施缓冲区融合为统一服务范围"
    ],

    notes=[
        "全局融合后只剩一条记录，属性字段全部丢失（仅保留几何）",
        "按字段融合时，其他属性列默认取第一条记录的值",
        "融合会消除重叠区域，是面积统计去重的标准做法",
        "缓冲区分析必须在 buffer 后、intersection 前执行 dissolve，否则面积会重复计算"
    ],

    workflow_example={
        'id': 'dissolve_buffer',
        'operator': 'dissolve',
        'params': {
            'input_gdf': {'$ref': 'buffer_railway'}
        },
        'depends_on': ['buffer_railway']
    }
)


ENVELOPE_METADATA = OperatorMetadata(
    name="envelope",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_GEOMETRY,
    description="最小外接矩形",
    brief_description="计算几何对象的最小外接矩形(MBR),常用于快速范围查询和空间索引",
    execution_modes=["workflow"],
    effects=["read"],

    overview="计算包含几何对象的最小轴对齐矩形(Minimum Bounding Rectangle),边平行于坐标轴。常用于空间索引、粗略范围查询等场景。",

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
        "空间索引构建: 为复杂几何创建快速索引矩形",
        "地图瓦片切割: 计算要素范围用于地图瓦片分配",
        "粗略相交判断: 用矩形快速过滤候选对象再精确计算",
        "图框生成: 为地图要素生成标准矩形图框"
    ],

    notes=[
        "矩形边平行于坐标轴,对旋转的狭长几何可能产生很大冗余",
        "如需最小面积外接矩形,使用minimum_rotated_rectangle",
        "envelope面积通常大于原始几何面积",
        "点和线的envelope可能退化为线或点"
    ],

    workflow_example={
        'id': 'get_bounds_rect',
        'operator': 'envelope',
        'params': {
            'input_gdf': {'$ref': 'load_buildings'}
        },
        'depends_on': ['load_buildings']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(BUFFER_METADATA, buffer),
    register_operator(INTERSECTION_METADATA, intersection),
    register_operator(UNION_METADATA, union),
    register_operator(CENTROID_METADATA, centroid),
    register_operator(DIFFERENCE_METADATA, difference),
    register_operator(SIMPLIFY_METADATA, simplify),
    register_operator(CONVEX_HULL_METADATA, convex_hull),
    register_operator(ENVELOPE_METADATA, envelope),
    register_operator(DISSOLVE_METADATA, dissolve),
    register_operator(POLYGONIZE_METADATA, polygonize),
    register_operator(CHECK_GEOMETRY_METADATA, check_geometry),
    register_operator(REPAIR_GEOMETRY_METADATA, repair_geometry),
    register_operator(SNAP_GEOMETRY_METADATA, snap_geometry),
    register_operator(CHECK_POLYGON_OVERLAP_METADATA, check_polygon_overlap),
    register_operator(CHECK_POLYGON_GAP_METADATA, check_polygon_gap),
    register_operator(CHECK_DANGLING_POINTS_METADATA, check_dangling_points),
])
