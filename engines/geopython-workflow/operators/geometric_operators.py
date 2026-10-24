"""
几何处理算子模块

提供几何变换和处理算子：缓冲、相交、合并、质心、差集、简化、凸包、外接矩形
"""

import geopandas as gpd
import numpy as np
import pandas as pd
import shapely
from collections import Counter
from itertools import pairwise
from math import fsum, hypot
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


def _require_nonempty_valid_lines(geometry: gpd.GeoSeries) -> None:
    if geometry.isna().any() or not geometry.geom_type.isin(["LineString", "MultiLineString"]).all():
        raise ValueError("input_gdf requires non-null LineString or MultiLineString geometries")
    parts = shapely.get_parts(geometry.array)
    if (shapely.is_empty(geometry.array).any() or shapely.is_empty(parts).any()
            or not shapely.is_valid(geometry.array).all()):
        raise ValueError("input_gdf requires non-empty valid lines; repair explicitly before checking")


def _normalized_line_overlap(intersections):
    """Keep only shared line positions and measure their union once."""
    lines = []
    for intersection in intersections:
        pending = [intersection]
        while pending:
            part = pending.pop()
            if part.geom_type == "LineString" and not part.is_empty:
                lines.append(part)
            elif part.geom_type in {"MultiLineString", "GeometryCollection"}:
                pending.extend(part.geoms)
    if not lines:
        return None
    shared = shapely.union_all(lines)
    geometry = shapely.normalize(shapely.multilinestrings(shapely.get_parts(shared)))
    # Hypot avoids squared-distance under/overflow; fsum keeps the metric
    # independent of multipart summation order. Repeated positions count once.
    try:
        length = fsum(hypot(end[0] - start[0], end[1] - start[1])
                      for line in geometry.geoms for start, end in pairwise(line.coords))
    except OverflowError as exc:
        raise ValueError("overlap requires valid lines with finite positive Float64 length") from exc
    if not np.isfinite(length) or length <= 0 or not shapely.is_valid(geometry):
        raise ValueError("overlap requires valid lines with finite positive Float64 length")
    return geometry, length



_SELF_OVERLAP_PAIR_LIMIT = 2048


def _self_overlap_candidate_batches(segments):
    """Bound original candidate pairs without changing their occurrence order."""
    tree = shapely.STRtree(segments)
    left, right = [], []
    for a, segment in enumerate(segments):
        candidates = tree.query(segment)
        for b in np.sort(candidates[candidates > a]):
            left.append(a)
            right.append(b)
            if len(left) == _SELF_OVERLAP_PAIR_LIMIT:
                yield np.asarray(left, dtype=np.intp), np.asarray(right, dtype=np.intp)
                left, right = [], []
    if left:
        yield np.asarray(left, dtype=np.intp), np.asarray(right, dtype=np.intp)


def _self_overlap_intersections(segments):
    """Compare every original occurrence in bounded batches; dedup only results."""
    segments = np.asarray(segments, dtype=object)
    seen = set()
    for left, right in _self_overlap_candidate_batches(segments):
        for intersection in shapely.intersection(segments[left], segments[right]):
            pending = [intersection]
            while pending:
                part = pending.pop()
                if part.geom_type == "LineString" and not part.is_empty:
                    coordinates = tuple(part.coords)
                    key = min(coordinates, coordinates[::-1])
                    if key not in seen:
                        seen.add(key)
                        yield part
                elif part.geom_type in {"MultiLineString", "GeometryCollection"}:
                    pending.extend(part.geoms)


def check_line_self_overlap(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report positions traversed more than once within each original feature."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    sources, lengths, overlaps = [], [], []
    for row, shape in enumerate(geometry.array):
        # Preserve every occurrence until AFTER comparing segments. Adjacent
        # segments may retrace each other; identical segments must not be deduped.
        segments = [shapely.LineString([start, end])
                    for part in shapely.get_parts(shape)
                    for start, end in pairwise(part.coords) if start != end]
        shared = _normalized_line_overlap(_self_overlap_intersections(segments))
        if shared is not None:
            overlap_geometry, length = shared
            sources.append(row)
            lengths.append(length)
            overlaps.append(overlap_geometry)
    return gpd.GeoDataFrame(
        {"source_id": ids.iloc[sources].reset_index(drop=True),
         "overlap_length": pd.Series(lengths, dtype="float64")},
        geometry=gpd.GeoSeries(overlaps, crs=geometry.crs),
    )


def check_line_overlap(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report positive-length intersections of distinct feature pairs, without editing sources."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    left, right, lengths, overlaps = [], [], [], []
    tree = shapely.STRtree(geometry.array)
    for a, shape in enumerate(geometry.array):
        candidates = tree.query(shape)
        for b in np.sort(candidates[candidates > a]):
            shared = _normalized_line_overlap([shapely.intersection(shape, geometry.array[b])])
            if shared is None:
                continue
            overlap_geometry, length = shared
            left.append(a)
            right.append(b)
            lengths.append(length)
            overlaps.append(overlap_geometry)
    return gpd.GeoDataFrame(
        {"source_id_a": ids.iloc[left].reset_index(drop=True),
         "source_id_b": ids.iloc[right].reset_index(drop=True),
         "overlap_length": pd.Series(lengths, dtype="float64")},
        geometry=gpd.GeoSeries(overlaps, crs=geometry.crs),
    )


def _line_intersection_parts(intersection):
    """Extract actual point/line leaves without turning line vertices into points."""
    pending = [intersection]
    while pending:
        part = pending.pop()
        if part.is_empty:
            continue
        if part.geom_type in {"Point", "LineString"}:
            yield part
        elif part.geom_type in {"MultiPoint", "MultiLineString", "GeometryCollection"}:
            pending.extend(part.geoms)


def check_line_self_intersection(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report isolated internal point meetings, excluding every self-overlap position."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    sources, points = [], []
    for row, shape in enumerate(geometry.array):
        segments, adjacent = [], set()
        for part in shapely.get_parts(shape):
            offset = len(segments)
            segments.extend(shapely.LineString([start, end]) for start, end in pairwise(part.coords) if start != end)
            count = len(segments) - offset
            adjacent.update((index, index + 1) for index in range(offset, offset + count - 1))
            if count > 1 and part.coords[0] == part.coords[-1]:
                adjacent.add((offset, offset + count - 1))
        tree = shapely.STRtree(segments)
        coordinates, seen_lines, lines = set(), set(), []
        for a, segment in enumerate(segments):
            candidates = tree.query(segment)
            for b in np.sort(candidates[candidates > a]):
                for part in _line_intersection_parts(shapely.intersection(segment, segments[b])):
                    if part.geom_type == "Point":
                        if (a, b) not in adjacent:
                            coordinates.add((part.x, part.y))
                    else:
                        # Adjacent retracing also contributes overlap coverage.
                        vertices = tuple(part.coords)
                        key = min(vertices, vertices[::-1])
                        if key not in seen_lines:
                            seen_lines.add(key)
                            lines.append(part)
        shared = shapely.union_all(lines) if lines else None
        for coordinate in sorted(coordinates):
            point = shapely.Point(coordinate)
            if shared is not None and shapely.intersects(shared, point):
                continue
            sources.append(row)
            points.append(point)
    result = gpd.GeoDataFrame(
        {"source_id": ids.iloc[sources].reset_index(drop=True)},
        geometry=gpd.GeoSeries(points, crs=geometry.crs),
    )
    _finite_xy_geometry(result, "result")
    return result


def group_line_components(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Label exact planar connectivity while retaining every original line part."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    if any(field in input_gdf.columns for field in ("source_id", "part_no", "component_id")):
        raise ValueError("output columns source_id, part_no and component_id must not overwrite source fields")

    parts, source_rows = shapely.get_parts(geometry.array, return_index=True)
    parents = list(range(len(parts)))

    def root(index):
        while parents[index] != index:
            parents[index] = parents[parents[index]]
            index = parents[index]
        return index

    # Query one original part at a time; exact intersection includes contacts,
    # crossings and overlaps without noding or changing any source coordinates.
    tree = shapely.STRtree(parts)
    for a, part in enumerate(parts):
        for b in tree.query(part, predicate="intersects"):
            if b > a:
                left, right = root(a), root(int(b))
                if left != right:
                    parents[max(left, right)] = min(left, right)

    components, part_numbers, component_numbers = {}, [], []
    source_part_counts = np.zeros(len(input_gdf), dtype=np.int64)
    for index, source_row in enumerate(source_rows):
        source_part_counts[source_row] += 1
        part_numbers.append(source_part_counts[source_row])
        component = root(index)
        if component not in components:
            components[component] = len(components) + 1
        component_numbers.append(components[component])

    result = input_gdf.iloc[source_rows].copy().reset_index(drop=True)
    result[geometry.name] = gpd.GeoSeries(parts, crs=geometry.crs)
    result["source_id"] = ids.iloc[source_rows].reset_index(drop=True)
    result["part_no"] = pd.Series(part_numbers, dtype="int64")
    result["component_id"] = pd.Series(component_numbers, dtype="int64")
    return result


def split_line_intersections(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Split original paths at cross-feature point meetings, preserving provenance."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    if any(field in input_gdf.columns for field in ("source_id", "part_no", "segment_no")):
        raise ValueError("output columns source_id, part_no and segment_no must not overwrite source fields")

    parts, segments, references = [], [], []
    for row, shape in enumerate(geometry.array):
        for part_no, part in enumerate(shapely.get_parts(shape), 1):
            vertices = list(part.coords)
            part_index = len(parts)
            count = 0
            for edge, (start, end) in enumerate(pairwise(vertices)):
                if start != end:
                    segments.append(shapely.LineString([start, end]))
                    references.append((part_index, edge, count))
                    count += 1
            parts.append((row, part_no, vertices, count, {}))

    tree = shapely.STRtree(segments)
    for a, segment in enumerate(segments):
        part_a, edge_a, ordinal_a = references[a]
        row_a, _, vertices_a, count_a, cuts_a = parts[part_a]
        candidates = tree.query(segment)
        for b in np.sort(candidates[candidates > a]):
            intersection = shapely.intersection(segment, segments[b])
            if intersection.is_empty:
                continue
            part_b, edge_b, ordinal_b = references[b]
            row_b, _, vertices_b, _, cuts_b = parts[part_b]
            pieces = list(_line_intersection_parts(intersection))
            if any(piece.geom_type != "Point" for piece in pieces):
                raise ValueError("positive-length overlap is not supported; resolve overlap explicitly before splitting")
            points = {(point.x, point.y) for point in pieces}
            if row_a == row_b:
                adjacent = part_a == part_b and (
                    ordinal_b == ordinal_a + 1 or
                    (ordinal_a == 0 and ordinal_b == count_a - 1 and vertices_a[0] == vertices_a[-1])
                )
                shared = set(vertices_a[edge_a:edge_a + 2]) & set(vertices_b[edge_b:edge_b + 2])
                if not adjacent or not points.issubset(shared):
                    raise ValueError("internal point intersection is not supported; resolve it explicitly before splitting")
                continue
            cuts_a.setdefault(edge_a, set()).update(points)
            cuts_b.setdefault(edge_b, set()).update(points)

    sources, part_numbers, segment_numbers, output = [], [], [], []
    for row, part_no, vertices, _, cuts in parts:
        current = [vertices[0]]
        segment_no = 0
        last_edge = max(edge for edge, (start, end) in enumerate(pairwise(vertices)) if start != end)
        for edge, (start, end) in enumerate(pairwise(vertices)):
            edge_cuts = cuts.get(edge, set())
            # The dominant axis retains ordering even when the minor axis of
            # a steep segment rounds several distinct intersections equally.
            # No cumulative distance, division, or on-line lookup is required.
            axis = 0 if abs(end[0] - start[0]) >= abs(end[1] - start[1]) else 1
            interior = sorted(edge_cuts - {start, end}, key=lambda point: point[axis], reverse=start[axis] > end[axis])
            for point in [*interior, end]:
                current.append(point)
                # Retain trailing repeated source coordinates in the last
                # positive piece instead of flushing it at its first endpoint.
                at_final_end = edge == last_edge and point == end
                if point in edge_cuts and not at_final_end and any(vertex != current[0] for vertex in current[1:]):
                    segment_no += 1
                    sources.append(row)
                    part_numbers.append(part_no)
                    segment_numbers.append(segment_no)
                    output.append(shapely.LineString(current))
                    current = [point]
        if any(vertex != current[0] for vertex in current[1:]):
            sources.append(row)
            part_numbers.append(part_no)
            segment_numbers.append(segment_no + 1)
            output.append(shapely.LineString(current))

    result = input_gdf.iloc[sources].copy().reset_index(drop=True)
    result[geometry.name] = gpd.GeoSeries(output, crs=geometry.crs)
    result["source_id"] = ids.iloc[sources].reset_index(drop=True)
    result["part_no"] = pd.Series(part_numbers, dtype="int64")
    result["segment_no"] = pd.Series(segment_numbers, dtype="int64")
    _finite_xy_geometry(result, "result")
    _require_nonempty_valid_lines(result.geometry)
    return result


def check_line_intersection(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report exact isolated point intersections for distinct original feature pairs."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    left, right, points = [], [], []
    tree = shapely.STRtree(geometry.array)
    for a, shape in enumerate(geometry.array):
        candidates = tree.query(shape)
        for b in np.sort(candidates[candidates > a]):
            # Intersect whole features, so shared line vertices never become
            # artificial point diagnostics through segment-by-segment pairing.
            coordinates = {(part.x, part.y)
                           for part in _line_intersection_parts(shapely.intersection(shape, geometry.array[b]))
                           if part.geom_type == "Point"}
            for coordinate in sorted(coordinates):
                left.append(a)
                right.append(b)
                points.append(shapely.Point(coordinate))
    result = gpd.GeoDataFrame(
        {"source_id_a": ids.iloc[left].reset_index(drop=True),
         "source_id_b": ids.iloc[right].reset_index(drop=True)},
        geometry=gpd.GeoSeries(points, crs=geometry.crs),
    )
    _finite_xy_geometry(result, "result")
    return result


def check_dangling_points(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Report degree-one planar network endpoints with all original source IDs."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    parts = shapely.get_parts(geometry.array)

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

GROUP_LINE_COMPONENTS_METADATA = OperatorMetadata(
    name="group_line_components", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="连通分组", brief_description="按原始线部件的精确相交关系归组，保留来源、全部属性和原路径",
    execution_modes=["workflow"], effects=["read"],
    overview="端点、T形、十字及重合均按精确XY连通，传递相连部件归入同组。"
             "逐原始部件输出，不因同源而连接，不打断、合并或修复。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待分组的单个线图层", notes="有效有限二维LineString/MultiLineString，已知投影CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何唯一非空标量列；不推断主键或使用索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="每行一个原始LineString部件，保留原列并追加source_id/part_no/component_id", is_default=True)],
    use_cases=["识别线网的独立连通组和孤立闭环", "保留来源及业务属性，为后续分组加工提供标识"],
    notes=["同源多个部件可以分属不同组；保留源行和部件顺序、原方向、全部顶点和闭环接缝", "部件号在源内从1开始；组号按首次出现从1开始，仅是本次计算标识", "source_id/part_no/component_id冲突时失败，不覆盖或自动改名", "精确XY不判断桥隧或质量，不设容差，不吸附、打断、去重、删组或修复", "源不变，保存另接save；索引和成果驻内存，高密度候选可能平方增长，不承诺固定容量"],
    workflow_example={"id": "group_lines", "operator": "group_line_components", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id"}, "depends_on": ["load"]},
)

SPLIT_LINE_INTERSECTIONS_METADATA = OperatorMetadata(
    name="split_line_intersections", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="交点打断", brief_description="按不同要素的精确点交会切分线，保留来源、属性和原路径顺序",
    execution_modes=["workflow"], effects=["read"],
    overview="十字交叉切分双方，T形切分被接入的线，端点接触不生成零长度段。"
             "内部自交、自叠及跨要素正长度重合整批拒绝，不去重或自动修复。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待切分的单个线图层", notes="有效有限二维线，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何唯一非空标量列；不推断主键或使用索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="每行一个LineString，保留原列并追加source_id/part_no/segment_no", is_default=True)],
    use_cases=["将真实十字或T形交会切分为可独立加工的线段", "保留原属性和来源生成线网片段"],
    notes=["部件号和片段号从1开始，片段号在部件内递增；保留方向、顶点和闭环原始接缝", "source_id/part_no/segment_no与源列冲突时失败，不覆盖或自动改名", "内部自交包括不同部件的点接触，正常相邻折点和闭环首尾除外", "无容差、精度网格、吸附、延伸或业务质量判定；源不变，保存另接save", "索引、交点和成果驻内存，不承诺固定内存或容量"],
    workflow_example={"id": "split_crossings", "operator": "split_line_intersections", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id"}, "depends_on": ["load"]},
)

CHECK_LINE_SELF_OVERLAP_METADATA = OperatorMetadata(
    name="check_line_self_overlap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="线内自叠", brief_description="检查单个线要素内部重复经过的线段，每源汇总去重，只检查不修复",
    execution_modes=["workflow"], effects=["read"],
    overview="提取原始路径折返、重复走线和同一多部件要素内部的正长度重合。"
             "每个有自叠的来源输出一行，重复次数不增加重合长度，不改变源线。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的单个线图层", notes="有效有限二维 LineString/MultiLineString，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何标量列，值非空且唯一；不推断主键或使用行索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id/overlap_length/MultiLineString，每源一行，保留来源类型和 CRS", is_default=True)],
    use_cases=["定位同一条线路的折返与重复走线", "检查同一多部件要素内部重复覆盖的线段"],
    notes=["每源所有重合位置去重后只计一次长度，不统计经过次数或内部段编号", "只检查同一要素内部；跨要素重合使用线叠检查", "点自交、普通闭环接点和零长度段不报，不设容差或忽略微小重合", "长度采用源投影水平单位，不换算米，不自动删线或生成质量结论", "保存另接 save；每源索引及候选驻内存，极端重复路径候选可平方增长，不承诺固定容量"],
    workflow_example={"id": "self_overlaps", "operator": "check_line_self_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_id"}, "depends_on": ["load"]},
)

CHECK_LINE_OVERLAP_METADATA = OperatorMetadata(
    name="check_line_overlap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="线叠检查", brief_description="逐对提取不同线要素的精确重合段，保留双方来源和长度",
    execution_modes=["workflow"], effects=["read"],
    overview="检查单个线图层不同要素之间的正长度交集；包含、反向重复和不同顶点分段同样报告。"
             "只报告几何事实，不判断业务错误或修改源线。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的单个线图层", notes="有效有限二维 LineString/MultiLineString，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何标量列，值非空且唯一；不推断主键或使用行索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id_a/source_id_b/overlap_length/MultiLineString，保留来源类型和源 CRS", is_default=True)],
    use_cases=["定位部分重合或完全重复的线及双方来源", "检查反向重复线和共线段后由业务确认"],
    notes=["每对按源行位置只输出一次，多段归入一行，每对同一位置长度只计一次", "三条共同重合的线逐对输出三行，长度总和不是全图层去重长度", "长度采用源投影水平单位，不换算米、不忽略微小重合", "点接触不报；不设容差、不检查单要素内部重复、不自动修复或改写源", "不扩展 Quality 规则，保存另接 save；索引及结果驻内存，不承诺固定容量"],
    workflow_example={"id": "check_line_overlaps", "operator": "check_line_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id"}, "depends_on": ["load"]},
)

CHECK_LINE_SELF_INTERSECTION_METADATA = OperatorMetadata(
    name="check_line_self_intersection", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="线内自交", brief_description="检查单个线要素内部孤立点交会，每源按 XY 去重，只检查不修复",
    execution_modes=["workflow"], effects=["read"],
    overview="报告非相邻段和不同部件的孤立点交会。排除正常折点、闭环首尾和正长度自叠线上的点，"
             "重合位置即使另有支线交会也不另报，不改变源线。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的单个线图层", notes="有效有限二维 LineString/MultiLineString，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何标量列，值非空且唯一；不推断主键或使用行索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id/Point，每源逐点去重，保留来源类型和源 CRS", is_default=True)],
    use_cases=["定位线路内部的孤立自交点与重复到访点", "核对同一多部件线内部点接触的来源"],
    notes=["相邻正长度段的正常接点与闭环首尾不报；零长度段不打断相邻关系", "不同部件端点接触可报；不将部件间接点当成同部件正常折点", "自叠线及端点上的点统一排除，交给线内自叠，不生成重合线端点诊断", "按源行位置、XY排序；不跨源比较、不输出次数/段号或质量结论", "无容差、精度网格或舍入；不打断、吸附或修复，保存另接 save", "每源原始段、索引、唯一交集及成果驻内存，候选可平方增长，不承诺固定容量"],
    workflow_example={"id": "check_self_crossings", "operator": "check_line_self_intersection", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id"}, "depends_on": ["load"]},
)

CHECK_LINE_INTERSECTION_METADATA = OperatorMetadata(
    name="check_line_intersection", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_GEOMETRY,
    description="线交检查", brief_description="逐对报告不同线要素的精确点交会，保留双方来源，只检查不改线",
    execution_modes=["workflow"], effects=["read"],
    overview="提取不同线要素间的十字、T形和端点接触。每个要素对的同一 XY 仅输出一次，"
             "重合线段两端不另报点，不自动判断业务质量或桥隧层级。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待检查的单个线图层", notes="有效有限二维 LineString/MultiLineString，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何标量列，值非空且唯一；不推断主键或使用行索引"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id_a/source_id_b/Point，每对逐点去重，保留来源类型和源 CRS", is_default=True)],
    use_cases=["定位线路交会及双方来源", "核对十字、T形与端点相接的平面事实"],
    notes=["按源 A/B 行位置、X、Y 排序；三个要素同点相交可输出三对，不按全图层坐标去重", "线点混合交集只取点部分，不把重合线段端点另报为交点", "无容差、精度网格或舍入；不检查单要素内部自交，不生成交会类型或质量结论", "保留源几何、属性和 CRS，不打断、吸附或自动修复；保存另接 save", "候选及成果驻内存，极端配对可平方增长，不承诺固定容量"],
    workflow_example={"id": "check_crossings", "operator": "check_line_intersection", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id"}, "depends_on": ["load"]},
)

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
    register_operator(CHECK_LINE_OVERLAP_METADATA, check_line_overlap),
    register_operator(CHECK_LINE_SELF_OVERLAP_METADATA, check_line_self_overlap),
    register_operator(CHECK_LINE_INTERSECTION_METADATA, check_line_intersection),
    register_operator(CHECK_LINE_SELF_INTERSECTION_METADATA, check_line_self_intersection),
    register_operator(SPLIT_LINE_INTERSECTIONS_METADATA, split_line_intersections),
    register_operator(GROUP_LINE_COMPONENTS_METADATA, group_line_components),
])
