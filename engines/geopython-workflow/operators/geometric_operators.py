"""
几何处理算子模块

提供几何变换和处理算子：缓冲、相交、合并、质心、差集、简化、凸包、外接矩形
"""

import geopandas as gpd
import numpy as np
import pandas as pd
import shapely
from collections import Counter
from fractions import Fraction
from itertools import pairwise
from math import fsum, hypot, nextafter
from numbers import Real, Integral
from typing import List
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, OutputPort, register_operator
)


# ==================== 算子实现 ====================

class BufferInputError(ValueError):
    """Proven buffer admission facts shared by single and multiple distances."""
    _messages = {
        'zh-cn': {
            'source': '缓冲输入须具有唯一列名及唯一活动几何列，几何须有效、非空、非缺失且为有限二维 XY；请先显式整理或修复源图层。',
            'crs': '缓冲运算要求已知投影坐标系和匹配、有限且为正的水平线性单位；距离沿原坐标单位，请先显式重投影。',
            'distance': '缓冲距离必须为有限实数，可为正数、零或负数；不接受布尔值、字符串、空值、数组或非有限值。',
            'resolution': '缓冲区每四分之一圆的线段数必须为正整数，不接受布尔值。',
        },
        'en': {
            'source': 'Buffer input requires unique column names, one active geometry column and valid non-empty non-missing finite XY geometries; prepare or explicitly repair the source layer first.',
            'crs': 'Buffering requires a known projected CRS with matching finite positive horizontal units; distances use the original coordinate units. Reproject explicitly first.',
            'distance': 'Buffer distance must be a finite real scalar and may be positive, zero or negative; booleans, strings, nulls, arrays and non-finite values are rejected.',
            'resolution': 'The number of line segments per quarter circle must be a positive integer; booleans are rejected.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _unary_frame_geometry(input_gdf, error_type):
    if (not isinstance(input_gdf, gpd.GeoDataFrame) or not input_gdf.columns.is_unique
            or sum(isinstance(dtype, gpd.array.GeometryDtype) for dtype in input_gdf.dtypes) != 1):
        raise error_type('source')
    try:
        return _active_geometry(input_gdf)
    except ValueError as exc:
        raise error_type('source') from exc


def _prepare_projected_xy_source(input_gdf, error_type):
    # The public callers have already admitted the frame schema.
    try:
        geometry = _finite_xy_geometry(input_gdf)
        if geometry.isna().any() or geometry.is_empty.any() or not shapely.is_valid(geometry.array).all():
            raise ValueError('invalid projected XY source geometry')
    except (ValueError, TypeError) as exc:
        raise error_type('source') from exc
    try:
        _require_projected_xy_crs(geometry.crs)
    except ValueError as exc:
        raise error_type('crs') from exc
    return geometry


def _finite_real_scalar(value, error_type, code):
    if isinstance(value, bool) or not isinstance(value, Real):
        raise error_type(code)
    try:
        number = float(value)
    except (ValueError, OverflowError) as exc:
        raise error_type(code) from exc
    if not np.isfinite(number):
        raise error_type(code)
    return number


def _buffer_distance(value):
    return _finite_real_scalar(value, BufferInputError, 'distance')


def _buffer_resolution(value, error_type=BufferInputError):
    if isinstance(value, bool) or not isinstance(value, Integral) or value <= 0:
        raise error_type('resolution')
    return int(value)


def _compute_buffer(input_gdf, geometry, distance, resolution):
    """One full-buffer primitive; valid empty polygons retain their source row."""
    values = geometry.buffer(distance, resolution=resolution)
    if (len(values) != len(geometry) or not values.index.equals(geometry.index)
            or values.isna().any() or not values.geom_type.isin(['Polygon', 'MultiPolygon']).all()
            or (distance > 0 and values.is_empty.any())
            or not shapely.is_valid(values.array).all() or shapely.has_z(values.array).any()
            or shapely.has_m(values.array).any() or not np.isfinite(shapely.get_coordinates(values.array)).all()):
        raise RuntimeError('buffer calculation produced an invalid or non-finite XY polygon result')
    result = input_gdf.copy()
    result[geometry.name] = values
    return result


def buffer(input_gdf: gpd.GeoDataFrame, distance: float, resolution: int = 16) -> gpd.GeoDataFrame:
    """Full per-source planar buffers in projected CRS units, retaining empty rows."""
    _unary_frame_geometry(input_gdf, BufferInputError)
    geometry = _prepare_projected_xy_source(input_gdf, BufferInputError)
    return _compute_buffer(input_gdf, geometry, _buffer_distance(distance), _buffer_resolution(resolution))


class IntersectionInputError(ValueError):
    """Known overlay admission facts, without source data in public messages."""

    _messages = {
        'zh-cn': {
            'source': '相交叠加的图层 A 须具有唯一几何列、有效非空有限二维几何，以及显式唯一非空来源标识。',
            'reference': '相交叠加的图层 B 须具有唯一几何列、有效非空有限二维几何，以及显式唯一非空来源标识。',
            'crs': '相交叠加要求两个输入使用相同的已知投影坐标系和有限正水平线性单位；请先显式重投影。',
            'fields': '相交叠加的业务字段、固定来源字段或结果几何列发生重名；请先在上游手动改名，不会自动加后缀或覆盖。',
        },
        'en': {
            'source': 'Overlay input A requires one geometry column, valid non-empty finite XY geometries and an explicit unique non-null source identifier.',
            'reference': 'Overlay input B requires one geometry column, valid non-empty finite XY geometries and an explicit unique non-null source identifier.',
            'crs': 'Intersection requires the same known projected CRS and finite positive horizontal linear units on both inputs; reproject explicitly first.',
            'fields': 'Overlay business fields conflict with each other, fixed source fields or the result geometry column; rename them upstream explicitly, without automatic suffixes or replacement.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _binary_overlay_inputs(input_gdf, gdf_b, id_field, reference_id_field, error_type, polygons_only=False):
    """One admission route for explicit binary overlay identities and attributes."""
    geometries, identifiers = [], []
    for frame, field, code in ((input_gdf, id_field, 'source'), (gdf_b, reference_id_field, 'reference')):
        try:
            if not isinstance(frame, gpd.GeoDataFrame) or not frame.columns.is_unique:
                raise ValueError('overlay requires unique columns')
            geometry = _finite_xy_geometry(frame)
            if (sum(isinstance(dtype, gpd.array.GeometryDtype) for dtype in frame.dtypes) != 1
                    or geometry.isna().any() or geometry.is_empty.any()
                    or not shapely.is_valid(geometry.array).all()
                    or (polygons_only and not geometry.geom_type.isin(['Polygon', 'MultiPolygon']).all())):
                raise ValueError('invalid overlay input')
            ids = _source_identifiers(frame, field)
        except ValueError as exc:
            raise error_type(code) from exc
        geometries.append(geometry)
        identifiers.append(ids)
    left, right = geometries
    try:
        _require_projected_xy_crs(left.crs)
        _require_projected_xy_crs(right.crs)
        if left.crs != right.crs:
            raise ValueError('overlay CRS mismatch')
    except ValueError as exc:
        raise error_type('crs') from exc
    left_fields = [name for name in input_gdf.columns if name != left.name]
    right_fields = [name for name in gdf_b.columns if name != right.name]
    if (any(name in frame.columns for frame in (input_gdf, gdf_b) for name in ('source_id_a', 'source_id_b'))
            or set(left_fields).intersection(right_fields) or left.name in right_fields):
        raise error_type('fields')

    return left, right, identifiers, left_fields, right_fields


def intersection(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame,
                 id_field: str, reference_id_field: str) -> gpd.GeoDataFrame:
    """One complete exact intersection per original A/B feature pair."""
    left, right, identifiers, left_fields, right_fields = _binary_overlay_inputs(
        input_gdf, gdf_b, id_field, reference_id_field, IntersectionInputError,
    )
    tree = shapely.STRtree(right.array)
    left_rows, right_rows, values = [], [], []
    for a, geometry in enumerate(left.array):
        for b in sorted(tree.query(geometry).tolist()):
            value = shapely.intersection(geometry, right.iloc[b])
            if value.is_empty:
                continue
            if not value.is_valid or not np.isfinite(shapely.get_coordinates(value)).all():
                raise RuntimeError('intersection produced invalid or non-finite geometry')
            left_rows.append(a)
            right_rows.append(b)
            values.append(value)
    result = input_gdf.iloc[left_rows].copy().reset_index(drop=True)
    result[left.name] = gpd.GeoSeries(values, crs=left.crs)
    for name in right_fields:
        result[name] = gdf_b[name].iloc[right_rows].reset_index(drop=True)
    result['source_id_a'] = identifiers[0].iloc[left_rows].reset_index(drop=True)
    result['source_id_b'] = identifiers[1].iloc[right_rows].reset_index(drop=True)
    return result


class UnionInputError(ValueError):
    """Proven polygon union admissions without source values in diagnostics."""
    _messages = {
        'zh-cn': {
            'source': '联合叠加的 A 须具有唯一几何列、有效非空有限二维面及显式唯一非空来源标识。',
            'reference': '联合叠加的 B 须具有唯一几何列、有效非空有限二维面及显式唯一非空来源标识。',
            'crs': '联合叠加要求双方使用相同已知投影坐标系及匹配的有限正水平单位；请先显式重投影。',
            'fields': '联合叠加的双方业务字段、固定来源字段或结果几何列发生重名；请先在上游手动改名。',
            'source_overlap': '联合叠加的 A 图层内部存在正面积重叠、包含或重复面；请先面叠检查并显式处理。',
            'reference_overlap': '联合叠加的 B 图层内部存在正面积重叠、包含或重复面；请先面叠检查并显式处理。',
        },
        'en': {
            'source': 'Union A requires one geometry column, valid non-empty finite XY polygons and an explicit unique non-null source identifier.',
            'reference': 'Union B requires one geometry column, valid non-empty finite XY polygons and an explicit unique non-null source identifier.',
            'crs': 'Union requires the same known projected CRS and matching finite positive horizontal units; reproject explicitly first.',
            'fields': 'Union business fields conflict with each other, fixed source fields or the result geometry column; rename them upstream explicitly.',
            'source_overlap': 'Union A contains positive-area overlaps, contained or duplicate polygons; check polygon overlaps and resolve them explicitly first.',
            'reference_overlap': 'Union B contains positive-area overlaps, contained or duplicate polygons; check polygon overlaps and resolve them explicitly first.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _union_layer_tree(geometry, code):
    tree = shapely.STRtree(geometry.array)
    for i, shape in enumerate(geometry.array):
        for j in sorted(tree.query(shape).tolist()):
            # Interiors intersect in dimension 2: no area threshold or rounding.
            if j > i and shapely.relate_pattern(shape, geometry.iloc[j], '2********'):
                raise UnionInputError(code)
    return tree


def _union_polygon_result(value):
    _require_valid_mask_result(value)
    if value.is_empty:
        return None
    if value.geom_type in ('Polygon', 'MultiPolygon'):
        return value
    polygons, pending = [], [value]
    while pending:
        part = pending.pop()
        if part.geom_type == 'Polygon' and not part.is_empty:
            polygons.append(part)
        elif part.geom_type in ('MultiPolygon', 'GeometryCollection'):
            pending.extend(reversed(part.geoms))
    if not polygons:
        return None
    result = polygons[0] if len(polygons) == 1 else shapely.multipolygons(polygons)
    _require_valid_mask_result(result)
    return result


def _union_attribute_rows(series, rows):
    # Establish a nullable schema before introducing absent-side rows. Never
    # round wide integers through float64, or infer types from business values.
    dtype = series.dtype
    if isinstance(dtype, np.dtype):
        if dtype.kind in ('i', 'u'):
            dtype = ('UInt' if dtype.kind == 'u' else 'Int') + str(dtype.itemsize * 8)
        elif dtype.kind == 'b':
            dtype = 'boolean'
    original = series.astype(dtype, copy=False).reset_index(drop=True)
    return original.reindex([-1 if i is None else i for i in rows]).reset_index(drop=True)


def union(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame,
          id_field: str, reference_id_field: str) -> gpd.GeoDataFrame:
    """Partition two non-overlapping polygon layers with complete provenance."""
    left, right, identifiers, left_fields, right_fields = _binary_overlay_inputs(
        input_gdf, gdf_b, id_field, reference_id_field, UnionInputError, polygons_only=True,
    )
    _union_layer_tree(left, 'source_overlap')
    tree = _union_layer_tree(right, 'reference_overlap')
    left_rows, right_rows, values = [], [], []

    def append(value, a, b):
        shape = _union_polygon_result(value)
        if shape is not None:
            left_rows.append(a)
            right_rows.append(b)
            values.append(shape)

    for a, geometry in enumerate(left.array):
        for b in sorted(tree.query(geometry).tolist()):
            append(shapely.intersection(geometry, right.iloc[b]), a, b)
    mask_a = _combined_polygon_mask(left) if not left.empty else None
    mask_b = _combined_polygon_mask(right) if not right.empty else None
    for a, geometry in enumerate(left.array):
        append(geometry if mask_b is None else shapely.difference(geometry, mask_b), a, None)
    for b, geometry in enumerate(right.array):
        append(geometry if mask_a is None else shapely.difference(geometry, mask_a), None, b)

    columns = {name: (gpd.GeoSeries(values, crs=left.crs) if name == left.name else
                      _union_attribute_rows(input_gdf[name], left_rows)) for name in input_gdf.columns}
    columns.update({name: _union_attribute_rows(gdf_b[name], right_rows) for name in right_fields})
    columns['source_id_a'] = _union_attribute_rows(identifiers[0], left_rows)
    columns['source_id_b'] = _union_attribute_rows(identifiers[1], right_rows)
    return gpd.GeoDataFrame(columns, geometry=left.name, crs=left.crs)


def centroid(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Per-source planar centroid with native weights; exterior points remain valid."""
    return _construct_unary_geometry(input_gdf, shapely.centroid, (0,))


class DifferenceInputError(ValueError):
    """Proven difference admissions, excluding arbitrary native exceptions."""
    _messages = {
        'zh-cn': {
            'source': '几何差集的图层 A 须具有唯一几何列、有效非空有限二维几何，以及显式唯一非空来源标识。',
            'mask': '几何差集的扣除范围 B 须具有唯一几何列和有效非空有限二维面或多面几何；不要求业务来源标识。',
            'crs': '几何差集要求双方使用相同的已知投影坐标系及有限正且匹配的水平线性单位；请先显式重投影。',
        },
        'en': {
            'source': 'Difference input A requires one geometry column, valid non-empty finite XY geometries and an explicit unique non-null source identifier.',
            'mask': 'Difference mask B requires one geometry column and valid non-empty finite XY Polygon or MultiPolygon geometries; no business identifier is required.',
            'crs': 'Difference requires the same known projected CRS and finite positive matching horizontal linear units; reproject explicitly first.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _polygon_mask_inputs(input_gdf, mask_gdf, id_field, error_type, *, source_types=None):
    """Single admission route for whole-polygon clipping and difference."""
    geometries = []
    for frame, code in ((input_gdf, 'source'), (mask_gdf, 'mask')):
        try:
            if not isinstance(frame, gpd.GeoDataFrame) or not frame.columns.is_unique:
                raise ValueError('polygon-mask inputs require unique columns')
            geometry = _finite_xy_geometry(frame)
            allowed = (['Polygon', 'MultiPolygon'] if code == 'mask' else source_types if source_types is not None else
                       ['Point', 'LineString', 'Polygon', 'MultiPoint', 'MultiLineString', 'MultiPolygon', 'GeometryCollection'])
            if (not frame.columns.is_unique
                    or sum(isinstance(dtype, gpd.array.GeometryDtype) for dtype in frame.dtypes) != 1
                    or geometry.isna().any() or geometry.is_empty.any()
                    or not geometry.geom_type.isin(allowed).all()
                    or not shapely.is_valid(geometry.array).all()):
                raise ValueError('invalid polygon-mask input')
            if code == 'source':
                _source_identifiers(frame, id_field)
        except (ValueError, TypeError) as exc:
            raise error_type(code) from exc
        geometries.append(geometry)
    left, right = geometries
    try:
        _require_projected_xy_crs(left.crs)
        _require_projected_xy_crs(right.crs)
        if left.crs != right.crs:
            raise ValueError('polygon-mask CRS mismatch')
    except ValueError as exc:
        raise error_type('crs') from exc
    return left, right


def _require_valid_mask_result(value):
    if (not value.is_valid or shapely.has_z(value) or shapely.has_m(value)
            or not np.isfinite(shapely.get_coordinates(value)).all()):
        raise RuntimeError('polygon-mask calculation produced invalid or non-finite XY geometry')


def _combined_polygon_mask(geometry):
    value = shapely.union_all(geometry.array)
    if value.is_empty or value.geom_type not in ('Polygon', 'MultiPolygon'):
        raise RuntimeError('polygon mask union did not produce a non-empty polygon range')
    _require_valid_mask_result(value)
    return value


def difference(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame,
               id_field: str) -> gpd.GeoDataFrame:
    """One complete non-empty result per original A, subtracting all B coverage."""
    left, right = _polygon_mask_inputs(input_gdf, gdf_b, id_field, DifferenceInputError)
    if left.empty or right.empty:
        return input_gdf.copy().reset_index(drop=True)

    mask = _combined_polygon_mask(right)
    rows, values = [], []
    for index, geometry in enumerate(left.array):
        value = shapely.difference(geometry, mask)
        if value.is_empty:
            continue
        _require_valid_mask_result(value)
        rows.append(index)
        values.append(value)
    result = input_gdf.iloc[rows].copy().reset_index(drop=True)
    result[left.name] = gpd.GeoSeries(values, crs=left.crs)
    return result


class SimplifyInputError(ValueError):
    """Closed simplification admission facts without source values."""
    _messages = {
        'zh-cn': {
            'source': '简化源图层须具有唯一列名和唯一活动几何列，源几何须有效、非空、非缺失且为有限二维XY；请先显式整理或修复源图层。',
            'crs': '简化要求已知投影坐标系和匹配、有限且为正的水平线性单位；容差沿原坐标单位，请先显式重投影。',
            'tolerance': '简化容差必须为有限非负实数标量，零合法；不接受布尔值、字符串、空值、数组或非有限值。',
            'preserve_topology': '保留单几何拓扑参数必须为布尔值True或False；不接受整数、字符串或空值。',
        },
        'en': {
            'source': 'Simplification requires unique column names, one active geometry column and valid non-empty non-missing finite XY source geometries; prepare or explicitly repair the source first.',
            'crs': 'Simplification requires a known projected CRS with matching finite positive horizontal units; tolerance uses the original coordinate units. Reproject explicitly first.',
            'tolerance': 'Simplification tolerance must be a finite non-negative real scalar, including zero; booleans, strings, nulls, arrays and non-finite values are rejected.',
            'preserve_topology': 'Preserve single-geometry topology must be a boolean True or False; integers, strings and nulls are rejected.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def simplify(input_gdf: gpd.GeoDataFrame, tolerance: float, preserve_topology: bool = True) -> gpd.GeoDataFrame:
    """Simplify each valid projected XY source, retaining attributes and empty rows."""
    _unary_frame_geometry(input_gdf, SimplifyInputError)
    geometry = _prepare_projected_xy_source(input_gdf, SimplifyInputError)
    tolerance = _finite_real_scalar(tolerance, SimplifyInputError, 'tolerance')
    if tolerance < 0:
        raise SimplifyInputError('tolerance')
    if not isinstance(preserve_topology, bool):
        raise SimplifyInputError('preserve_topology')
    values = geometry.simplify(tolerance, preserve_topology=preserve_topology)
    if (len(values) != len(geometry) or not values.index.equals(geometry.index)
            or values.isna().any() or shapely.has_z(values.array).any() or shapely.has_m(values.array).any()
            or not np.isfinite(shapely.get_coordinates(values.array)).all()
            or (preserve_topology and not shapely.is_valid(values.array).all())):
        raise RuntimeError('simplification calculation produced an invalid or non-finite XY result')
    result = input_gdf.copy()
    result[geometry.name] = values
    return result


class UnaryConstructionInputError(ValueError):
    """Closed single-source projected geometry construction admission facts."""
    _messages = {
        'zh-cn': {
            'source': '几何构造源图层须具有唯一列名及唯一活动几何列，源几何须有效、非空、非缺失且为有限二维XY；请先显式整理或修复源图层。',
            'crs': '几何构造要求已知投影坐标系和匹配、有限且为正的水平线性单位；请先显式重投影，不自动换算单位。',
        },
        'en': {
            'source': 'Geometry construction requires unique column names, one active geometry column and valid non-empty non-missing finite XY source geometries; prepare or explicitly repair the source first.',
            'crs': 'Geometry construction requires a known projected CRS with matching finite positive horizontal linear units; reproject explicitly first, without automatic unit conversion.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _validated_unary_result(values, size, allowed_types):
    values = np.asarray(values, dtype=object)
    if (values.shape != (size,) or shapely.is_missing(values).any()
            or shapely.is_empty(values).any() or not shapely.is_valid(values).all()
            or not np.isin(shapely.get_type_id(values), allowed_types).all()
            or shapely.has_z(values).any() or shapely.has_m(values).any()
            or not np.isfinite(shapely.get_coordinates(values)).all()):
        raise RuntimeError('geometry construction produced an invalid or non-finite XY result')
    return values


def _construct_unary_geometry(input_gdf, kernel, allowed_types):
    _unary_frame_geometry(input_gdf, UnaryConstructionInputError)
    geometry = _prepare_projected_xy_source(input_gdf, UnaryConstructionInputError)
    values = _validated_unary_result(kernel(geometry.array), len(geometry), allowed_types)
    result = input_gdf.copy()
    result[geometry.name] = gpd.GeoSeries(values, index=geometry.index, crs=geometry.crs)
    return result


class InteriorPointInputError(ValueError):
    """Typed polygon-only source admission; candidate failures are computation errors."""
    _messages = {
        'zh-cn': {
            'source': '面内点仅接受有效、非空、非缺失、有限二维XY的Polygon或MultiPolygon，且列名和实际活动几何须唯一；请先显式整理或修复面图层。',
            'crs': '面内点要求已知投影坐标系和匹配、有限且为正的水平线性单位；请先显式重投影，不自动换算单位。',
        },
        'en': {
            'source': 'Interior point requires valid non-empty non-missing finite XY Polygon or MultiPolygon sources with unique column names and one active geometry column; prepare or explicitly repair the polygon layer first.',
            'crs': 'Interior point requires a known projected CRS with matching finite positive horizontal linear units; reproject explicitly first, without automatic unit conversion.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def representative_point(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Strict interior point of each source's first maximum-net-area polygon part."""
    _unary_frame_geometry(input_gdf, InteriorPointInputError)
    geometry = _prepare_projected_xy_source(input_gdf, InteriorPointInputError)
    if not geometry.geom_type.isin(['Polygon', 'MultiPolygon']).all():
        raise InteriorPointInputError('source')
    parts, owners = shapely.get_parts(geometry.array, return_index=True)
    areas = np.asarray(shapely.area(parts), dtype=float)
    if areas.shape != (len(parts),) or not np.isfinite(areas).all() or not (areas > 0).all():
        raise RuntimeError('interior point could not measure finite positive polygon part areas')
    selected = np.empty(len(geometry), dtype=object)
    largest = np.full(len(geometry), -np.inf)
    for part, owner, area in zip(parts, owners, areas):
        if area > largest[owner]:
            largest[owner] = area
            selected[owner] = part
    values = _validated_unary_result(shapely.point_on_surface(selected), len(geometry), (0,))
    if not shapely.contains(selected, values).all() or not shapely.contains(geometry.array, values).all():
        raise RuntimeError('interior point calculation did not produce a strict interior point in the selected polygon')
    result = input_gdf.copy()
    result[geometry.name] = gpd.GeoSeries(values, index=geometry.index, crs=geometry.crs)
    return result


def convex_hull(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Per-source convex hull, retaining valid Point and LineString degeneracies."""
    return _construct_unary_geometry(input_gdf, shapely.convex_hull, (0, 1, 3))


def minimum_rotated_rectangle(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Per-source minimum-area oriented rectangle, retaining point/line results."""
    return _construct_unary_geometry(input_gdf, shapely.minimum_rotated_rectangle, (0, 1, 3))


def _axis_aligned_envelope(geometry):
    bounds = np.asarray(shapely.bounds(geometry), dtype=float)
    if (bounds.shape != (len(geometry), 4) or not np.isfinite(bounds).all()
            or (bounds[:, :2] > bounds[:, 2:]).any()):
        raise RuntimeError('envelope calculation produced invalid or non-finite bounds')
    same_x = bounds[:, 0] == bounds[:, 2]
    same_y = bounds[:, 1] == bounds[:, 3]
    points = same_x & same_y
    lines = same_x ^ same_y
    polygons = ~(same_x | same_y)
    values = np.empty(len(geometry), dtype=object)
    if points.any():
        values[points] = shapely.points(bounds[points, :2])
    if lines.any():
        values[lines] = shapely.linestrings(np.stack((bounds[lines, :2], bounds[lines, 2:]), axis=1))
    if polygons.any():
        values[polygons] = shapely.box(*bounds[polygons].T)
    expected_types = np.where(points, 0, np.where(lines, 1, 3))
    if (not np.array_equal(shapely.get_type_id(values), expected_types)
            or not np.array_equal(shapely.bounds(values), bounds)):
        raise RuntimeError('envelope result differs from its exact axis-aligned source bounds')
    return values


def envelope(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Per-source exact XY envelope, retaining valid axis-degenerate points/lines."""
    return _construct_unary_geometry(input_gdf, _axis_aligned_envelope, (0, 1, 3))


class DissolveInputError(ValueError):
    _messages = {
        'zh-cn': {'source':'融合几何要求唯一活动几何、有效非空有限二维面及显式唯一非空来源标识。', 'crs':'融合几何要求已知投影坐标系及匹配的有限正水平单位；请先显式重投影。', 'groups':'请显式指定无重复的现有非几何标量分组字段列表，[]表示全局融合；空值保留成组。', 'fields':'融合固定字段fusion_id/member_count/source_id与输入列重名；请在上游手动改名。'},
        'en': {'source':'Dissolve requires one active geometry, valid non-empty finite XY polygons and an explicit unique non-null source identifier.', 'crs':'Dissolve requires a known projected CRS and matching finite positive horizontal units; reproject explicitly first.', 'groups':'Explicitly list distinct existing non-geometry scalar group fields; [] means global dissolve and null groups are retained.', 'fields':'Dissolve fields fusion_id/member_count/source_id conflict with input columns; rename them upstream explicitly.'},
    }
    def __init__(self, code):
        self.code=code
        super().__init__(self.message('zh-cn'))
    def message(self, language):
        return self._messages['en' if language=='en' else 'zh-cn'][self.code]


def _dissolve_geometry(values):
    value=shapely.union_all(values)
    _require_valid_mask_result(value)
    return value


def dissolve(input_gdf: gpd.GeoDataFrame, id_field: str, group_fields: list) -> dict:
    """Exact per-group polygon union plus every original source occurrence."""
    try:
        if not isinstance(input_gdf,gpd.GeoDataFrame) or not input_gdf.columns.is_unique:
            raise ValueError('unique columns required')
        geometry=_finite_xy_geometry(input_gdf)
        ids=_source_identifiers(input_gdf,id_field)
        if (sum(isinstance(d,gpd.array.GeometryDtype)for d in input_gdf.dtypes)!=1
                or geometry.isna().any() or geometry.is_empty.any()
                or not geometry.geom_type.isin(['Polygon','MultiPolygon']).all()
                or not shapely.is_valid(geometry.array).all()):
            raise ValueError('valid polygons required')
    except ValueError as exc:
        raise DissolveInputError('source')from exc
    try:
        _require_projected_xy_crs(geometry.crs)
    except ValueError as exc:
        raise DissolveInputError('crs')from exc
    if any(name in input_gdf.columns for name in ('fusion_id','member_count','source_id')):
        raise DissolveInputError('fields')
    if (not isinstance(group_fields,list) or any(not isinstance(f,str) or f not in input_gdf.columns or f==geometry.name for f in group_fields)
            or len(set(group_fields))!=len(group_fields)):
        raise DissolveInputError('groups')
    missing=object();groups={};rows=[];representatives=[];membership=[]
    for i in range(len(input_gdf)):
        values=tuple(input_gdf[f].iloc[i]for f in group_fields)
        key=[]
        for value in values:
            if not pd.api.types.is_scalar(value):raise DissolveInputError('groups')
            token=missing if pd.isna(value) else (type(value),value)
            try:hash(token)
            except (TypeError,ValueError)as exc:raise DissolveInputError('groups')from exc
            key.append(token)
        key=tuple(key)
        if key not in groups:
            groups[key]=len(rows);rows.append([]);representatives.append(i)
        group=groups[key];rows[group].append(i);membership.append(group+1)
    shapes=[]
    for positions in rows:
        value=_dissolve_geometry(geometry.iloc[positions].array)
        if value.is_empty or value.geom_type not in ('Polygon','MultiPolygon'):
            raise RuntimeError('dissolve did not produce a non-empty polygon group')
        shapes.append(value)
    result=input_gdf.iloc[representatives][group_fields].copy().reset_index(drop=True)
    result['fusion_id']=pd.Series(range(1,len(rows)+1),dtype='int64')
    result['member_count']=pd.Series([len(x)for x in rows],dtype='int64')
    result[geometry.name]=gpd.GeoSeries(shapes,crs=geometry.crs)
    result=gpd.GeoDataFrame(result,geometry=geometry.name,crs=geometry.crs)
    members=input_gdf.copy().reset_index(drop=True)
    members['fusion_id']=pd.Series(membership,dtype='int64')
    members['source_id']=ids.reset_index(drop=True)
    return {'result':result,'members':members}


class GeometryValidityInputError(ValueError):
    """Only intentional geometry-check/repair admissions, never GEOS text."""
    _messages = {
        'zh-cn': {
            'source': '几何检查和几何修复要求GeoDataFrame及实际活动几何，坐标须为有限二维XY，不接受Z/M；NULL和EMPTY按原规则保留，不自动投影或修复输入准入。',
            'fields': '几何检查的valid_field和reason_field须为非空、无首尾空白且彼此不同的字符串，并且不能与源列重名；请显式修改字段配置，不自动改名或覆盖。',
        },
        'en': {
            'source': 'Geometry checking and repair require a GeoDataFrame with an active geometry and finite XY coordinates, without Z/M. NULL and EMPTY retain their existing rules; no automatic projection or admission repair is performed.',
            'fields': 'Geometry checking requires valid_field and reason_field to be non-empty strings without surrounding whitespace, distinct from each other and every source column. Change the field configuration explicitly; no automatic renaming or replacement is performed.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    @classmethod
    def source(cls, _message):
        return cls('source')

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _active_geometry(input_gdf: gpd.GeoDataFrame, name: str = "input_gdf", *, input_error=ValueError) -> gpd.GeoSeries:
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise input_error(f"{name} must be a GeoDataFrame")
    try:
        return input_gdf.geometry
    except AttributeError as exc:
        raise input_error(f"{name} requires an active geometry column") from exc


def _geometry_for_output(input_gdf: gpd.GeoDataFrame, fields) -> gpd.GeoSeries:
    """Resolve the active geometry before fixed scalar outputs can replace it."""
    geometry = _active_geometry(input_gdf)
    if geometry.name in fields:
        raise ValueError("active geometry column conflicts with output fields; rename the geometry column explicitly")
    return geometry


def _finite_xy_geometry(input_gdf: gpd.GeoDataFrame, name: str = "input_gdf", *, input_error=ValueError) -> gpd.GeoSeries:
    geometry = _active_geometry(input_gdf, name, input_error=input_error)
    if shapely.has_z(geometry.array).any() or shapely.has_m(geometry.array).any():
        raise input_error(f"{name} requires two-dimensional XY geometries")
    if not np.isfinite(shapely.get_coordinates(geometry.array)).all():
        raise input_error(f"{name} requires finite XY coordinates")
    return geometry


def check_geometry(
    input_gdf: gpd.GeoDataFrame,
    valid_field: str = "geometry_valid",
    reason_field: str = "geometry_reason",
) -> gpd.GeoDataFrame:
    """Annotate each source feature's planar validity without changing geometry."""
    geometry = _finite_xy_geometry(input_gdf, input_error=GeometryValidityInputError.source)
    fields = (valid_field, reason_field)
    if any(not isinstance(field, str) or not field or field != field.strip() for field in fields):
        raise GeometryValidityInputError('fields')
    if valid_field == reason_field or any(field in input_gdf.columns for field in fields):
        raise GeometryValidityInputError('fields')
    result = input_gdf.copy()
    result[valid_field] = shapely.is_valid(geometry.array)
    reasons = shapely.is_valid_reason(geometry.array)
    result[reason_field] = pd.array(
        ["Missing Geometry" if reason is None else reason for reason in reasons], dtype="string",
    )
    return result


def repair_geometry(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Repair invalid XY geometries using linework, retaining collapsed components."""
    geometry = _finite_xy_geometry(input_gdf, input_error=GeometryValidityInputError.source)
    values = geometry.array.copy()
    invalid = ~shapely.is_missing(values) & ~shapely.is_valid(values)
    values[invalid] = shapely.make_valid(values[invalid], method="linework", keep_collapsed=True)
    if (~shapely.is_missing(values) & ~shapely.is_valid(values)).any():
        raise ValueError("geometry repair did not produce valid geometries")
    result = input_gdf.copy()
    result[result.geometry.name] = values
    return result


def _require_projected_xy_crs(crs, *, input_error=ValueError) -> None:
    if crs is None or not crs.is_projected:
        raise input_error("input requires a known projected CRS; reproject explicitly")
    units = [axis.unit_conversion_factor for axis in crs.axis_info[:2]]
    if len(units) != 2 or not all(np.isfinite(unit) and unit > 0 for unit in units) or units[0] != units[1]:
        raise input_error("projected CRS requires matching known horizontal coordinate units")


class MultipartInputError(ValueError):
    """Fixed multipart admissions; native failures stay execution failures."""
    _messages = {
        'zh-cn': {
            'source': '多部拆分要求唯一列名、明确活动几何及显式id_field唯一非空标量来源列；仅接受有效有限二维XY的点、线、面及对应Multi类型。GeometryCollection、NULL、EMPTY、空部件和Z/M整批拒绝。',
            'crs': '多部拆分要求已知二维地理或投影CRS；未知、地心、三维及复合CRS拒绝，不自动投影、换单位或交换XY。',
            'field': '多部拆分固定输出字段{field}与输入字段冲突；请先手动重命名，不覆盖或自动改名。',
        },
        'en': {
            'source': 'Multipart split requires unique column names, an active geometry and an explicit id_field containing unique non-null scalar source identifiers. Only valid finite XY Point, LineString, Polygon and their Multi types are accepted; GeometryCollection, NULL, EMPTY, empty parts and Z/M are rejected for the whole batch.',
            'crs': 'Multipart split requires a known two-dimensional geographic or projected CRS; unknown, geocentric, 3D and compound CRS are rejected, without projection, unit conversion or XY swapping.',
            'field': 'Fixed multipart output field {field} conflicts with an input field; rename it manually first, without replacement or automatic renaming.',
        },
    }

    def __init__(self, code, field=''):
        self.code, self.field = code, field
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code].format(field=self.field)


class VertexInputError(MultipartInputError):
    """Vertex extraction admissions share original-part facts, not free text."""
    _messages = {
        'zh-cn': {
            'source': '顶点提取要求唯一列名、活动几何及显式id_field唯一非空标量来源；只接受有效有限二维XY点/线/面及Multi类型。GeometryCollection、NULL、EMPTY、空部件/环和Z/M整批拒绝，闭合首尾及重复坐标保留。',
            'crs': '顶点提取要求已知二维地理或投影CRS；未知、地心、三维及复合CRS拒绝，不自动投影、换单位或交换XY。',
            'field': '顶点提取固定输出字段{field}与输入字段冲突；请先手动重命名，不覆盖或自动改名。',
        },
        'en': {
            'source': 'Vertex extraction requires unique column names, an active geometry and explicit unique non-null scalar id_field identifiers. Only valid finite XY Point/LineString/Polygon and their Multi types are accepted; GeometryCollection, NULL, EMPTY, empty parts/rings and Z/M are rejected for the whole batch. Closing and repeated coordinate occurrences are retained.',
            'crs': 'Vertex extraction requires a known two-dimensional geographic or projected CRS; unknown, geocentric, 3D and compound CRS are rejected, without projection, unit conversion or XY swapping.',
            'field': 'Fixed vertex output field {field} conflicts with an input field; rename it manually first, without replacement or automatic renaming.',
        },
    }


def _original_geometry_parts(input_gdf, id_field, fields, error_type):
    """One original-part source map for mechanical multipart/vertex extraction."""
    try:
        if not isinstance(input_gdf, gpd.GeoDataFrame) or not input_gdf.columns.is_unique:
            raise ValueError('original parts require unique columns')
        geometry = _finite_xy_geometry(input_gdf)
        allowed = {'Point', 'MultiPoint', 'LineString', 'MultiLineString', 'Polygon', 'MultiPolygon'}
        if (geometry.isna().any() or shapely.is_empty(geometry.array).any()
                or not geometry.geom_type.isin(allowed).all() or not shapely.is_valid(geometry.array).all()):
            raise ValueError('original parts require valid non-empty single or Multi geometry')
        ids = _source_identifiers(input_gdf, id_field)
    except (ValueError, TypeError) as exc:
        raise error_type('source') from exc
    for field in fields:
        if field in input_gdf.columns:
            raise error_type('field', field)
    crs = geometry.crs
    if crs is None or crs.is_compound or len(crs.axis_info) != 2 or not (crs.is_geographic or crs.is_projected):
        raise error_type('crs')
    parts, source_rows = shapely.get_parts(geometry.array, return_index=True)
    parts, source_rows = np.asarray(parts), np.asarray(source_rows)
    counts = np.asarray(shapely.get_num_geometries(geometry.array))
    if (parts.ndim != 1 or source_rows.shape != parts.shape or source_rows.dtype.kind not in 'iu'
            or counts.shape != (len(geometry),) or counts.dtype.kind not in 'iu'
            or (counts < 1).any() or counts.sum(dtype=np.int64) != len(parts)):
        raise RuntimeError('original parts produced invalid part counts or source mapping')
    counts = counts.astype(np.int64)
    if not np.array_equal(source_rows, np.repeat(np.arange(len(geometry), dtype=np.int64), counts)):
        raise RuntimeError('original parts changed original source order')
    if shapely.is_empty(parts).any():
        raise error_type('source')
    if (shapely.is_missing(parts).any() or not np.isin(shapely.get_type_id(parts), [0, 1, 3]).all()
            or not shapely.is_valid(parts).all() or shapely.has_z(parts).any() or shapely.has_m(parts).any()
            or not np.isfinite(shapely.get_coordinates(parts)).all()):
        raise RuntimeError('original parts produced invalid single parts')
    part_numbers = np.arange(len(parts), dtype=np.int64) - np.repeat(np.cumsum(counts) - counts, counts) + 1
    return geometry, ids, parts, source_rows, part_numbers


def split_multipart(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Split original single-level parts, preserving every source attribute."""
    geometry, ids, parts, source_rows, part_numbers = _original_geometry_parts(
        input_gdf, id_field, ('source_id', 'part_no'), MultipartInputError)
    result = input_gdf.iloc[source_rows].copy().reset_index(drop=True)
    result[geometry.name] = gpd.GeoSeries(parts, index=result.index, crs=geometry.crs)
    result['source_id'] = ids.iloc[source_rows].reset_index(drop=True)
    result['part_no'] = part_numbers
    return result


def extract_vertices(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Emit each stored coordinate occurrence, including closing duplicates."""
    fields = ('source_id', 'part_no', 'ring_no', 'vertex_no', 'vertex_id')
    geometry, ids, parts, source_rows, part_numbers = _original_geometry_parts(
        input_gdf, id_field, fields, VertexInputError)
    coordinates, rows, numbers, rings, vertices = [], [], [], [], []
    for part, source_row, part_no in zip(parts, source_rows, part_numbers):
        paths = enumerate((part.exterior, *part.interiors), 1) if part.geom_type == 'Polygon' else [(0, part)]
        for ring_no, path in paths:
            values = np.asarray(path.coords)
            if values.ndim != 2 or values.shape[1] != 2 or not len(values) or values.dtype.kind not in 'fiu' or not np.isfinite(values).all():
                raise RuntimeError('vertex extraction produced an invalid coordinate sequence')
            count = len(values)
            coordinates.append(values)
            rows.append(np.full(count, source_row, dtype=np.int64))
            numbers.append(np.full(count, part_no, dtype=np.int64))
            rings.append(np.full(count, ring_no, dtype=np.int64))
            vertices.append(np.arange(1, count + 1, dtype=np.int64))
    xy = np.concatenate(coordinates) if coordinates else np.empty((0, 2), dtype=float)
    source_positions = np.concatenate(rows) if rows else np.empty(0, dtype=np.int64)
    points = np.asarray(shapely.points(xy))
    if (points.shape != (len(xy),) or shapely.is_missing(points).any() or shapely.is_empty(points).any()
            or not (shapely.get_type_id(points) == 0).all() or not shapely.is_valid(points).all()
            or shapely.has_z(points).any() or shapely.has_m(points).any()
            or not np.array_equal(shapely.get_coordinates(points), xy)):
        raise RuntimeError('vertex extraction did not preserve original finite XY coordinates')
    result = input_gdf.iloc[source_positions].copy().reset_index(drop=True)
    result[geometry.name] = gpd.GeoSeries(points, index=result.index, crs=geometry.crs)
    result['source_id'] = ids.iloc[source_positions].reset_index(drop=True)
    for name, chunks in [('part_no', numbers), ('ring_no', rings), ('vertex_no', vertices)]:
        result[name] = np.concatenate(chunks) if chunks else np.empty(0, dtype=np.int64)
    result['vertex_id'] = np.arange(1, len(points) + 1, dtype=np.int64)
    return result


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


class _EndpointNetworkError(ValueError):
    """Proven non-endpoint contact or overlapping/self-intersecting source parts."""


def _endpoint_line_network(geometry):
    """One exact, unmodified original-part endpoint graph for extraction and merging."""
    parts, sources = shapely.get_parts(geometry.array, return_index=True)
    if not shapely.is_simple(parts).all():
        raise _EndpointNetworkError("internal point intersection or self-overlap is not supported; resolve explicitly before merging")
    vertices = [list(part.coords) for part in parts]
    ends, part_numbers = {}, []
    source_counts = np.zeros(len(geometry), dtype=np.int64)
    for index, row in enumerate(sources):
        source_counts[row] += 1
        part_numbers.append(source_counts[row])
        for side in (0, 1):
            ends.setdefault(vertices[index][0 if side == 0 else -1], []).append((index, side))
    tree = shapely.STRtree(parts)
    for a, part in enumerate(parts):
        candidates = tree.query(part)
        for b in np.sort(candidates[candidates > a]):
            for piece in _line_intersection_parts(shapely.intersection(part, parts[b])):
                if piece.geom_type != "Point":
                    raise _EndpointNetworkError("positive-length overlap is not supported; resolve overlap explicitly before merging")
                point = (piece.x, piece.y)
                if point not in (vertices[a][0], vertices[a][-1]) or point not in (vertices[b][0], vertices[b][-1]):
                    raise _EndpointNetworkError("non-endpoint intersection is not supported; split intersections explicitly before merging")

    return parts, sources, vertices, ends, part_numbers


class LineNodeInputError(ValueError):
    """Closed admission facts; numerical/native failures keep their original classification."""
    _messages = {
        'zh-cn': {
            'source': '节点提取要求有效有限二维线图层及唯一非空来源标识。',
            'crs': '节点提取要求已知投影坐标系和相同有效水平单位；请先显式重投影。',
            'network': '节点提取要求部件仅在双方端点相接；请先显式打断交会并处理内部自交或正长度重合。',
            'field_conflict': '节点提取的固定输出字段与源字段重名；请先显式重命名。',
        },
        'en': {
            'source': 'Node extraction requires valid finite XY lines and unique non-null source identifiers.',
            'crs': 'Node extraction requires a known projected CRS with matching valid horizontal units; reproject explicitly first.',
            'network': 'Node extraction requires parts to meet only at both endpoints; explicitly split intersections and resolve internal self-intersections or positive-length overlaps first.',
            'field_conflict': 'A fixed node extraction output field conflicts with a source field; rename it explicitly first.',
        },
    }

    def __init__(self, code, field=''):
        self.code, self.field = code, field
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def extract_line_nodes(input_gdf: gpd.GeoDataFrame, id_field: str) -> dict[str, gpd.GeoDataFrame]:
    """Extract exact original-part endpoints; loops contribute start and end separately."""
    try:
        geometry = _finite_xy_geometry(input_gdf)
        _require_nonempty_valid_lines(geometry)
    except ValueError as exc:
        raise LineNodeInputError('source') from exc
    for field in ('node_id', 'degree', 'source_id', 'source_part_no', 'end'):
        if field in input_gdf.columns:
            raise LineNodeInputError('field_conflict', field)
    try:
        ids = _source_identifiers(input_gdf, id_field)
    except ValueError as exc:
        raise LineNodeInputError('source') from exc
    try:
        _require_projected_xy_crs(geometry.crs)
    except ValueError as exc:
        raise LineNodeInputError('crs') from exc
    try:
        parts, sources, vertices, ends, part_numbers = _endpoint_line_network(geometry)
    except _EndpointNetworkError as exc:
        raise LineNodeInputError('network') from exc

    nodes = {coordinate: index for index, coordinate in enumerate(ends, 1)}
    point_geometry = gpd.GeoSeries([shapely.Point(coordinate) for coordinate in nodes], crs=geometry.crs)
    result = gpd.GeoDataFrame({geometry.name: point_geometry,
        'node_id': pd.Series(list(nodes.values()), dtype='int64'),
        'degree': pd.Series([len(incident) for incident in ends.values()], dtype='int64')}, geometry=geometry.name, crs=geometry.crs)
    member_rows, member_points, node_ids, source_parts, sides = [], [], [], [], []
    for index, row in enumerate(sources):
        for side, coordinate in (('start', vertices[index][0]), ('end', vertices[index][-1])):
            member_rows.append(row)
            member_points.append(shapely.Point(coordinate))
            node_ids.append(nodes[coordinate])
            source_parts.append(part_numbers[index])
            sides.append(side)
    members = input_gdf.iloc[member_rows].copy().reset_index(drop=True)
    members[geometry.name] = gpd.GeoSeries(member_points, crs=geometry.crs)
    members['node_id'] = pd.Series(node_ids, dtype='int64')
    members['source_id'] = ids.iloc[member_rows].reset_index(drop=True)
    members['source_part_no'] = pd.Series(source_parts, dtype='int64')
    members['end'] = pd.Series(sides, dtype='string')
    return {'result': result, 'members': members}


def merge_line_chains(input_gdf: gpd.GeoDataFrame, id_field: str, match_fields: list) -> dict:
    """Join exact non-branching paths, with lossless original-part membership."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    reserved = {"chain_id", "member_count", "member_no", "source_part_no", "is_reversed"}
    if reserved.intersection(input_gdf.columns):
        raise ValueError("chain output columns must not overwrite source fields")
    if (not isinstance(match_fields, list) or
            any(not isinstance(field, str) or field not in input_gdf.columns or
                field == id_field or isinstance(input_gdf[field].dtype, gpd.array.GeometryDtype)
                for field in match_fields) or len(set(match_fields)) != len(match_fields)):
        raise ValueError("match_fields must explicitly list distinct non-geometry business columns excluding id_field")
    attributes = list(input_gdf[match_fields].itertuples(index=False, name=None))
    if any(not pd.api.types.is_scalar(value) for row in attributes for value in row):
        raise ValueError("match_fields requires scalar business values")

    def matches(a, b):
        if not match_fields:
            return True
        for left, right in zip(attributes[a], attributes[b]):
            missing_left, missing_right = pd.isna(left), pd.isna(right)
            if missing_left or missing_right:
                if missing_left and missing_right:
                    continue
                return False
            if type(left) is not type(right) or left != right:
                return False
        return True

    parts, sources, vertices, ends, part_numbers = _endpoint_line_network(geometry)

    # Full network degree is fixed before comparing business attributes. A
    # branch can never disappear merely because another arm differs in value.
    links = {}
    for incident in ends.values():
        if len(incident) == 2:
            left, right = incident
            if left[0] != right[0] and matches(sources[left[0]], sources[right[0]]):
                links[left], links[right] = right, left

    visited, chain_shapes, representatives, member_sources = set(), [], [], []
    chain_ids, ordinals, source_parts, reversed_parts, member_shapes = [], [], [], [], []
    member_counts = []
    for seed in range(len(parts)):
        if seed in visited:
            continue
        # Walk backward from the earliest original part. Open chains preserve
        # that part's direction; cycles start at its original seam/direction.
        start, side = seed, 0
        while (start, side) in links:
            previous, entry = links[(start, side)]
            start, side = previous, 1 - entry
            if start == seed:
                start, side = seed, 0
                break
        chain_id, count, coordinates = len(chain_shapes) + 1, 0, []
        current = start
        while current not in visited:
            visited.add(current)
            reverse = side == 1
            points = vertices[current][::-1] if reverse else vertices[current]
            coordinates.extend(points if not coordinates else points[1:])
            count += 1
            member_sources.append(sources[current])
            member_shapes.append(parts[current])
            chain_ids.append(chain_id)
            ordinals.append(count)
            source_parts.append(part_numbers[current])
            reversed_parts.append(reverse)
            following = links.get((current, 1 - side))
            if following is None:
                break
            current, side = following
        chain_shapes.append(shapely.LineString(coordinates))
        representatives.append(sources[seed])
        member_counts.append(count)

    result = input_gdf.iloc[representatives][match_fields + [geometry.name]].copy().reset_index(drop=True)
    result[geometry.name] = gpd.GeoSeries(chain_shapes, crs=geometry.crs)
    result["chain_id"] = pd.Series(range(1, len(chain_shapes) + 1), dtype="int64")
    result["member_count"] = pd.Series(member_counts, dtype="int64")
    members = input_gdf.iloc[member_sources].copy().reset_index(drop=True)
    members[geometry.name] = gpd.GeoSeries(member_shapes, crs=geometry.crs)
    members["chain_id"] = pd.Series(chain_ids, dtype="int64")
    members["member_no"] = pd.Series(ordinals, dtype="int64")
    members["source_part_no"] = pd.Series(source_parts, dtype="int64")
    members["is_reversed"] = pd.Series(reversed_parts, dtype="bool")
    _finite_xy_geometry(result, "result")
    _require_nonempty_valid_lines(result.geometry)
    return {"result": result, "members": members}


class PolygonRingInputError(ValueError):
    """Confirmed ring admission facts; the adapter owns public diagnostics."""
    def __init__(self, code, field=""):
        self.code, self.field = code, field
        messages = {
            'field_conflict': 'output columns source_id, part_no, ring_no, ring_id and is_hole must not overwrite source fields',
            'geometry_invalid': 'input_gdf requires non-empty valid polygons; repair explicitly before extraction',
        }
        super().__init__(messages[code])


def extract_polygon_rings(input_gdf: gpd.GeoDataFrame, id_field: str) -> gpd.GeoDataFrame:
    """Extract original exterior/interior rings with their unmerged source attributes."""
    geometry = _finite_xy_geometry(input_gdf)
    ids = _source_identifiers(input_gdf, id_field)
    reserved = ("source_id", "part_no", "ring_no", "ring_id", "is_hole")
    for field in reserved:
        if field in input_gdf.columns:
            raise PolygonRingInputError("field_conflict", field)
    if geometry.isna().any() or not geometry.geom_type.isin(["Polygon", "MultiPolygon"]).all():
        raise ValueError("input_gdf requires non-null Polygon or MultiPolygon geometries")
    parts, source_rows = shapely.get_parts(geometry.array, return_index=True)
    if shapely.is_empty(geometry.array).any() or shapely.is_empty(parts).any():
        raise ValueError("input_gdf requires non-empty valid polygons; repair explicitly before extraction")
    if not shapely.is_valid(geometry.array).all():
        raise PolygonRingInputError("geometry_invalid")

    rows, lines, part_numbers, ring_numbers, holes = [], [], [], [], []
    source_part_counts = np.zeros(len(input_gdf), dtype=np.int64)
    for part, source_row in zip(parts, source_rows):
        source_part_counts[source_row] += 1
        # Walk original rings directly: preserve seams, directions and repeated
        # vertices, even for shared edges or duplicate polygons in other rows.
        for ring_no, ring in enumerate((part.exterior, *part.interiors), 1):
            if ring.is_empty:
                raise ValueError("input_gdf requires non-empty polygon rings")
            rows.append(source_row)
            lines.append(shapely.LineString(ring))
            part_numbers.append(source_part_counts[source_row])
            ring_numbers.append(ring_no)
            holes.append(ring_no != 1)

    result = input_gdf.iloc[rows].copy().reset_index(drop=True)
    result[geometry.name] = gpd.GeoSeries(lines, crs=geometry.crs)
    result["source_id"] = ids.iloc[rows].reset_index(drop=True)
    result["part_no"] = pd.Series(part_numbers, dtype="int64")
    result["ring_no"] = pd.Series(ring_numbers, dtype="int64")
    result["ring_id"] = np.arange(1, len(rows) + 1, dtype=np.int64)
    result["is_hole"] = pd.Series(holes, dtype="bool")
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


def _ordered_segment_points(start, end, cuts):
    """Order shared exact cuts on the original segment's dominant coordinate."""
    axis = 0 if abs(end[0] - start[0]) >= abs(end[1] - start[1]) else 1
    interior = sorted(cuts - {start, end}, key=lambda point: point[axis], reverse=start[axis] > end[axis])
    return [start, *interior, end]


def normalize_line_network(input_gdf: gpd.GeoDataFrame, id_field: str) -> dict:
    """Node original segment occurrences and share exact edges without choosing attributes."""
    geometry = _finite_xy_geometry(input_gdf)
    _require_projected_xy_crs(geometry.crs)
    ids = _source_identifiers(input_gdf, id_field)
    _require_nonempty_valid_lines(geometry)
    reserved = {"edge_id", "occurrence_count", "source_id", "source_part_no", "source_segment_no", "piece_no", "is_reversed"}
    if reserved.intersection(input_gdf.columns):
        raise ValueError("network output columns must not overwrite source fields")

    segments, references, cuts = [], [], []
    for row, shape in enumerate(geometry.array):
        for part_no, part in enumerate(shapely.get_parts(shape), 1):
            for segment_no, (start, end) in enumerate(pairwise(part.coords), 1):
                if start != end:
                    segments.append(shapely.LineString([start, end]))
                    references.append((row, part_no, segment_no))
                    cuts.append({start, end})
    tree = shapely.STRtree(segments)
    for a, segment in enumerate(segments):
        candidates = tree.query(segment)
        for b in np.sort(candidates[candidates > a]):
            # Associate each shared cut directly with both original occurrences;
            # post-union predicates cannot recover all repeated source visits.
            for piece in _line_intersection_parts(shapely.intersection(segment, segments[b])):
                points = {(piece.x, piece.y)} if piece.geom_type == "Point" else set(piece.coords)
                cuts[a].update(points)
                cuts[b].update(points)

    edge_lookup, edges, counts = {}, [], []
    source_rows, source_parts, source_segments, ordinals = [], [], [], []
    edge_ids, directions, shapes = [], [], []
    for segment, (row, part_no, segment_no), cut in zip(segments, references, cuts):
        start, end = segment.coords
        points = _ordered_segment_points(start, end, cut)
        for piece_no, (left, right) in enumerate(pairwise(points), 1):
            key = (left, right) if left < right else (right, left)
            edge_id = edge_lookup.get(key)
            if edge_id is None:
                edge_id = len(edges) + 1
                edge_lookup[key] = edge_id
                edges.append(shapely.LineString(key))
                counts.append(0)
            counts[edge_id - 1] += 1
            source_rows.append(row)
            source_parts.append(part_no)
            source_segments.append(segment_no)
            ordinals.append(piece_no)
            edge_ids.append(edge_id)
            directions.append(left != key[0])
            shapes.append(shapely.LineString([left, right]))

    result = gpd.GeoDataFrame({geometry.name: gpd.GeoSeries(edges, crs=geometry.crs)},
                              geometry=geometry.name, crs=geometry.crs)
    result["edge_id"] = pd.Series(range(1, len(edges) + 1), dtype="int64")
    result["occurrence_count"] = pd.Series(counts, dtype="int64")
    members = input_gdf.iloc[source_rows].copy().reset_index(drop=True)
    members[geometry.name] = gpd.GeoSeries(shapes, crs=geometry.crs)
    members["edge_id"] = pd.Series(edge_ids, dtype="int64")
    members["source_id"] = ids.iloc[source_rows].reset_index(drop=True)
    members["source_part_no"] = pd.Series(source_parts, dtype="int64")
    members["source_segment_no"] = pd.Series(source_segments, dtype="int64")
    members["piece_no"] = pd.Series(ordinals, dtype="int64")
    members["is_reversed"] = pd.Series(directions, dtype="bool")
    _finite_xy_geometry(result, "result")
    _require_nonempty_valid_lines(result.geometry)
    return {"result": result, "members": members}


def split_line_intersections(input_gdf: gpd.GeoDataFrame, id_field: str, scope: str = "between") -> gpd.GeoDataFrame:
    """Split exact point meetings at each original path occurrence, preserving provenance."""
    if not isinstance(scope, str) or scope not in {"between", "all"}:
        raise ValueError("scope must be 'between' or 'all'")
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
                if adjacent and points.issubset(shared):
                    continue
                if scope == "between":
                    raise ValueError("internal point intersection is not supported; resolve it explicitly before splitting")
            # Each original edge occurrence owns its cuts. An XY visited more
            # than once must split at every visit, without merging its paths.
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
            for point in _ordered_segment_points(start, end, edge_cuts)[1:]:
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


class _LineReferenceError(ValueError):
    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages[language][self.code]


class NearLineInputError(_LineReferenceError):
    """Near-line diagnostics localized by the existing execution adapter."""

    _messages = {
        'zh-cn': {
            'tolerance': '近接检查的容差必须是以坐标系水平单位表示的有限正数。',
            'source': '近接检查的源图层须具有有效有限二维线、已知投影坐标系及非空唯一标识；不接受缺失或空几何，允许空批。',
            'reference': '近接检查的参考图层须具有有效有限二维线、已知投影坐标系及非空唯一标识；不接受缺失或空几何，允许空批。',
            'crs': '近接检查要求两个图层使用相同的已知投影坐标系和匹配的水平单位；请先显式重投影。',
            'calculation': '近接检查无法可靠计算有限的最近位置和距离，未生成成果。',
        },
        'en': {
            'tolerance': 'Line proximity tolerance must be a finite positive number in horizontal CRS units.',
            'source': 'Line proximity requires valid finite XY source lines, a known projected CRS and unique non-null IDs; '
                      'missing or empty geometries are rejected, but empty batches are allowed.',
            'reference': 'Line proximity requires valid finite XY reference lines, a known projected CRS and unique non-null IDs; '
                         'missing or empty geometries are rejected, but empty batches are allowed.',
            'crs': 'Line proximity requires the same known projected CRS and matching horizontal units; reproject explicitly first.',
            'calculation': 'Line proximity cannot reliably compute finite nearest coordinates and distance; no result was produced.',
        },
    }



class EndpointSnapError(_LineReferenceError):
    """Endpoint moves fail as one calculation, without partial repair."""

    _messages = {
        'zh-cn': {
            'tolerance': '端点吸附的容差必须是以坐标系水平单位表示的有限正数。',
            'source': '端点吸附的源图层须具有有效有限二维线、已知投影坐标系及非空唯一标识；不接受缺失或空几何，允许空批。',
            'reference': '端点吸附的参考图层须具有有效有限二维线、已知投影坐标系及非空唯一标识；不接受缺失或空几何，允许空批。',
            'crs': '端点吸附要求两个图层使用相同的已知投影坐标系和匹配的水平单位；请先显式重投影。',
            'ambiguous': '端点吸附存在等距但位置不同的最近候选，未生成任何修复结果。',
            'endpoint': '端点吸附无法仅移动原始开口部件的端点完整处理该悬点；请先显式规整源线。',
            'landing': '端点吸附的最近位置不能精确落在原始参考线上，未生成任何修复结果。',
            'invalid': '端点吸附会造成原始正长度线段塌缩或产生无效几何，未生成任何修复结果。',
            'calculation': '端点吸附无法可靠计算有限的最近位置和距离，未生成任何修复结果。',
        },
        'en': {
            'tolerance': 'Endpoint snapping tolerance must be a finite positive number in horizontal CRS units.',
            'source': 'Endpoint snapping requires valid finite XY source lines, a known projected CRS and unique non-null IDs; missing or empty geometries are rejected, but empty batches are allowed.',
            'reference': 'Endpoint snapping requires valid finite XY reference lines, a known projected CRS and unique non-null IDs; missing or empty geometries are rejected, but empty batches are allowed.',
            'crs': 'Endpoint snapping requires the same known projected CRS and matching horizontal units; reproject explicitly first.',
            'ambiguous': 'Endpoint snapping has equally near candidates at different positions; no repair result was produced.',
            'endpoint': 'Endpoint snapping cannot completely handle this dangle by moving only original open-part endpoints; normalize the source lines explicitly first.',
            'landing': 'Endpoint snapping cannot place the nearest position exactly on the original reference lines; no repair result was produced.',
            'invalid': 'Endpoint snapping would collapse an original positive-length segment or produce invalid geometry; no repair result was produced.',
            'calculation': 'Endpoint snapping cannot reliably compute finite nearest coordinates and distance; no repair result was produced.',
        },
    }


class LineExtensionError(_LineReferenceError):
    """Exact bounded extension fails without selecting a fallback intersection."""

    _messages = {
        'zh-cn': {
            'tolerance': '线端延伸的最大延伸距离必须是以坐标系水平单位表示的有限正数。',
            'source': '线端延伸的源图层须具有有效有限二维线、已知投影坐标系及非空唯一标识；不接受缺失或空几何，允许空批。',
            'reference': '线端延伸的参考图层须具有有效有限二维线、已知投影坐标系及非空唯一标识；不接受缺失或空几何，允许空批。',
            'crs': '线端延伸要求两个图层使用相同的已知投影坐标系和匹配的水平单位；请先显式重投影。',
            'endpoint': '线端延伸无法将该悬点完整表达为原始开口部件的端点；请先显式规整源线。',
            'direction': '线端延伸的同一悬点具有不同的朝外方向，未生成任何延伸结果。',
            'landing': '线端延伸的首次交点不能精确沿原方向落在参考线上，未生成任何延伸结果。',
            'invalid': '线端延伸产生无效或非有限几何，未生成任何延伸结果。',
            'calculation': '线端延伸无法可靠计算有限的首次交点和延伸长度，未生成任何延伸结果。',
        },
        'en': {
            'tolerance': 'The maximum extension distance must be a finite positive number in horizontal CRS units.',
            'source': 'Line extension requires valid finite XY source lines, a known projected CRS and unique non-null IDs; missing or empty geometries are rejected, but empty batches are allowed.',
            'reference': 'Line extension requires valid finite XY reference lines, a known projected CRS and unique non-null IDs; missing or empty geometries are rejected, but empty batches are allowed.',
            'crs': 'Line extension requires the same known projected CRS and matching horizontal units; reproject explicitly first.',
            'endpoint': 'Line extension cannot completely represent this dangle as original open-part endpoints; normalize the source lines explicitly first.',
            'direction': 'Line extension has different outward directions at the same dangle; no extension result was produced.',
            'landing': 'Line extension cannot place the first intersection exactly along the original direction on the reference lines; no extension result was produced.',
            'invalid': 'Line extension produced invalid or non-finite geometry; no extension result was produced.',
            'calculation': 'Line extension cannot reliably compute a finite first intersection and extension length; no extension result was produced.',
        },
    }


def _open_endpoint_occurrence(part, coords, coordinate, error_type):
    """Locate a complete terminal run, rejecting any remaining interior coverage."""
    if part.is_closed:
        raise error_type('endpoint')
    if coords[0] == coordinate:
        stop = 1
        while coords[stop] == coordinate:
            stop += 1
        end, span, remainder = 'start', (0, stop), coords[stop:]
    elif coords[-1] == coordinate:
        start = len(coords)-2
        while coords[start] == coordinate:
            start -= 1
        end, span, remainder = 'end', (start+1, len(coords)), coords[:start+1]
    else:
        raise error_type('endpoint')
    if len(remainder) > 1 and shapely.intersects(shapely.LineString(remainder), shapely.Point(coordinate)):
        raise error_type('endpoint')
    return end, span, remainder


def _line_reference_inputs(input_gdf, reference_gdf, id_field, reference_id_field, tolerance, error_type):
    if isinstance(tolerance, (bool, np.bool_)) or not isinstance(tolerance, Real):
        raise error_type('tolerance')
    try:
        tolerance = float(tolerance)
    except OverflowError as exc:
        raise error_type('tolerance') from exc
    if not np.isfinite(tolerance) or tolerance <= 0:
        raise error_type('tolerance')
    try:
        dangling = check_dangling_points(input_gdf, id_field)
    except ValueError as exc:
        raise error_type('source') from exc
    try:
        reference = _finite_xy_geometry(reference_gdf, 'reference_gdf')
        _require_projected_xy_crs(reference.crs)
        reference_ids = _source_identifiers(reference_gdf, reference_id_field)
        _require_nonempty_valid_lines(reference)
    except ValueError as exc:
        raise error_type('reference') from exc
    if not dangling.crs.equals(reference.crs):
        raise error_type('crs')
    return dangling, reference, reference_ids, tolerance


def _reference_line_segments(reference):
    segments, owners = [], []
    for owner, shape in enumerate(reference.array):
        for part in shapely.get_parts(shape):
            for start, end in pairwise(part.coords):
                if start != end:
                    # A fixed direction makes projection independent of source
                    # orientation. Neither input geometry is modified.
                    segments.append(shapely.LineString(sorted((start, end))))
                    owners.append(owner)
    return segments, owners, shapely.STRtree(segments)


def _line_projection_candidates(point, segments, owners, tree, tolerance, error_type):
    if len(tree.query(point, predicate='intersects')):
        return
    x, y = point.x, point.y
    limit = float(np.finfo(float).max)
    envelope = shapely.box(
        max(-limit, nextafter(x - tolerance, -float('inf'))),
        max(-limit, nextafter(y - tolerance, -float('inf'))),
        min(limit, nextafter(x + tolerance, float('inf'))),
        min(limit, nextafter(y + tolerance, float('inf'))),
    )
    for segment_row in tree.query(envelope):
        try:
            connection = shapely.shortest_line(point, segments[segment_row])
        except shapely.errors.GEOSException as exc:
            raise error_type('calculation') from exc
        if connection.is_empty:
            raise error_type('calculation')
        nx, ny = connection.coords[-1]
        distance = hypot(nx - x, ny - y)
        if not np.isfinite([nx, ny, distance]).all():
            raise error_type('calculation')
        yield owners[segment_row], distance, nx, ny


def check_line_proximity(
    input_gdf: gpd.GeoDataFrame, reference_gdf: gpd.GeoDataFrame,
    id_field: str, reference_id_field: str, tolerance: float,
) -> gpd.GeoDataFrame:
    """Report every near reference feature for each unconnected source dangle."""
    dangling, reference, reference_ids, tolerance = _line_reference_inputs(
        input_gdf, reference_gdf, id_field, reference_id_field, tolerance, NearLineInputError)
    segments, owners, tree = _reference_line_segments(reference)
    source_rows, reference_rows, distances, nearest_x, nearest_y = [], [], [], [], []
    for row, point in enumerate(dangling.geometry):
        best = {}
        for owner, distance, nx, ny in _line_projection_candidates(
                point, segments, owners, tree, tolerance, NearLineInputError):
            candidate = (distance, nx, ny)
            if owner not in best or candidate < best[owner]:
                best[owner] = candidate
        for owner in sorted(best):
            distance, nx, ny = best[owner]
            if 0 < distance < tolerance:
                source_rows.append(row)
                reference_rows.append(owner)
                distances.append(distance)
                nearest_x.append(nx)
                nearest_y.append(ny)
    return gpd.GeoDataFrame(
        {'source_id': dangling['source_id'].iloc[source_rows].reset_index(drop=True),
         'reference_id': reference_ids.iloc[reference_rows].reset_index(drop=True),
         'distance': pd.Series(distances, dtype='float64'),
         'nearest_x': pd.Series(nearest_x, dtype='float64'),
         'nearest_y': pd.Series(nearest_y, dtype='float64')},
        geometry=gpd.GeoSeries(dangling.geometry.iloc[source_rows].reset_index(drop=True), crs=dangling.crs),
    )


def snap_line_endpoints(
    input_gdf: gpd.GeoDataFrame, reference_gdf: gpd.GeoDataFrame,
    id_field: str, reference_id_field: str, tolerance: float,
) -> dict[str, gpd.GeoDataFrame]:
    """Move complete original endpoint occurrences once to a unique exact target."""
    try:
        return _snap_line_endpoints(input_gdf, reference_gdf, id_field, reference_id_field, tolerance)
    except shapely.errors.GEOSException as exc:
        raise EndpointSnapError('calculation') from exc


def _snap_line_endpoints(input_gdf, reference_gdf, id_field, reference_id_field, tolerance):
    dangling, reference, reference_ids, tolerance = _line_reference_inputs(
        input_gdf, reference_gdf, id_field, reference_id_field, tolerance, EndpointSnapError)
    segments, owners, tree = _reference_line_segments(reference)
    parts = [(row, number, part, list(part.coords)) for row, shape in enumerate(input_gdf.geometry.array)
             for number, part in enumerate(shapely.get_parts(shape), 1)]
    part_tree = shapely.STRtree([part for _, _, part, _ in parts])
    plans = {}
    coordinates = dict.fromkeys(tuple(point.coords[0]) for point in dangling.geometry)
    for coordinate in coordinates:
        point = shapely.Point(coordinate)
        nearest, positions, selected = None, set(), set()
        for owner, distance, nx, ny in _line_projection_candidates(
                point, segments, owners, tree, tolerance, EndpointSnapError):
            if distance == 0:
                # The original point did not intersect any reference segment.
                # A computed zero is therefore unreliable, not a new contact.
                raise EndpointSnapError('calculation')
            if distance >= tolerance:
                continue
            if nearest is None or distance < nearest:
                nearest, positions, selected = distance, {(nx, ny)}, {owner}
            elif distance == nearest:
                positions.add((nx, ny))
                selected.add(owner)
        if nearest is None:
            continue
        if len(positions) != 1:
            raise EndpointSnapError('ambiguous')
        target = next(iter(positions))
        references = sorted({owners[i] for i in tree.query(shapely.Point(target), predicate='intersects')})
        if not selected <= set(references):
            raise EndpointSnapError('landing')
        # Every source occurrence of this network dangle must be movable at an
        # open part's terminal coordinate run. Interior coverage cannot be left
        # behind just because another source has an endpoint at the same XY.
        for index in sorted(part_tree.query(point, predicate='intersects')):
            _, _, part, coords = parts[index]
            end, span, _ = _open_endpoint_occurrence(part, coords, coordinate, EndpointSnapError)
            plans[index, end] = (span, coordinate, target, nearest, references)

    output = input_gdf.geometry.array.copy()
    rebuilt = {}
    source_rows, reference_rows, numbers, ends, originals, targets, distances = [], [], [], [], [], [], []
    for index, (row, number, part, coords) in enumerate(parts):
        values = coords.copy()
        moved = False
        for end in ('start', 'end'):
            if (index, end) not in plans:
                continue
            span, original, target, distance, references = plans[index, end]
            values[span[0]:span[1]] = [target] * (span[1]-span[0])
            moved = True
            for owner in references:
                source_rows.append(row); reference_rows.append(owner); numbers.append(number); ends.append(end)
                originals.append(original); targets.append(target); distances.append(distance)
        if moved and any(a != b and c == d for (a,b),(c,d) in zip(pairwise(coords),pairwise(values))):
            raise EndpointSnapError('invalid')
        rebuilt.setdefault(row, []).append(shapely.LineString(values) if moved else part)
    for row, row_parts in rebuilt.items():
        output[row] = row_parts[0] if input_gdf.geometry.iloc[row].geom_type == 'LineString' else shapely.MultiLineString(row_parts)
    result = input_gdf.copy()
    result[result.geometry.name] = gpd.GeoSeries(output, index=input_gdf.index, crs=input_gdf.crs)
    try:
        _finite_xy_geometry(result, 'result')
        _require_nonempty_valid_lines(result.geometry)
    except ValueError as exc:
        raise EndpointSnapError('invalid') from exc
    details = gpd.GeoDataFrame({
        'source_id': input_gdf[id_field].iloc[source_rows].reset_index(drop=True),
        'reference_id': reference_ids.iloc[reference_rows].reset_index(drop=True),
        'part_no': pd.Series(numbers, dtype='int64'), 'end': pd.Series(ends, dtype='string'),
        'original_x': pd.Series([p[0] for p in originals], dtype='float64'),
        'original_y': pd.Series([p[1] for p in originals], dtype='float64'),
        'target_x': pd.Series([p[0] for p in targets], dtype='float64'),
        'target_y': pd.Series([p[1] for p in targets], dtype='float64'),
        'distance': pd.Series(distances, dtype='float64'),
    }, geometry=gpd.GeoSeries([shapely.Point(p) for p in originals], crs=input_gdf.crs))
    return {'result': result, 'details': details}


def _cross_xy(a, b):
    return a[0]*b[1] - a[1]*b[0]


def _ray_segment_entry(origin, direction, segment):
    """First ray parameter on a closed segment, using exact original binary XY."""
    a, b = (tuple(map(Fraction, xy)) for xy in segment.coords)
    edge = (b[0]-a[0], b[1]-a[1])
    delta = (a[0]-origin[0], a[1]-origin[1])
    denominator = _cross_xy(direction, edge)
    if denominator:
        t = _cross_xy(delta, edge) / denominator
        u = _cross_xy(delta, direction) / denominator
        return t if t > 0 and 0 <= u <= 1 else None
    if _cross_xy(delta, direction):
        return None
    axis = 0 if direction[0] else 1
    first, last = sorted(((a[axis]-origin[axis])/direction[axis],
                          (b[axis]-origin[axis])/direction[axis]))
    if last <= 0:
        return None
    # An interval containing the origin was already excluded as exact contact.
    # A disagreement between exact arithmetic and GEOS cannot become a repair.
    if first <= 0:
        raise LineExtensionError('landing')
    return first


def extend_line_endpoints(
    input_gdf: gpd.GeoDataFrame, reference_gdf: gpd.GeoDataFrame,
    id_field: str, reference_id_field: str, max_distance: float,
) -> dict[str, gpd.GeoDataFrame]:
    """Extend frozen original dangles to the first exact forward reference hit."""
    try:
        return _extend_line_endpoints(input_gdf, reference_gdf, id_field, reference_id_field, max_distance)
    except (shapely.errors.GEOSException, OverflowError, ZeroDivisionError) as exc:
        raise LineExtensionError('calculation') from exc


def _extend_line_endpoints(input_gdf, reference_gdf, id_field, reference_id_field, max_distance):
    dangling, reference, reference_ids, maximum = _line_reference_inputs(
        input_gdf, reference_gdf, id_field, reference_id_field, max_distance, LineExtensionError)
    segments, owners, tree = _reference_line_segments(reference)
    parts = [(row, number, part, list(part.coords)) for row, shape in enumerate(input_gdf.geometry.array)
             for number, part in enumerate(shapely.get_parts(shape), 1)]
    part_tree = shapely.STRtree([part for _, _, part, _ in parts])
    plans = {}
    maximum_squared = Fraction(maximum)**2
    limit = float(np.finfo(float).max)
    for coordinate in dict.fromkeys(tuple(p.coords[0]) for p in dangling.geometry):
        point = shapely.Point(coordinate)
        if not segments or len(tree.query(point, predicate='intersects')):
            continue
        origin = tuple(map(Fraction, coordinate))
        occurrences, direction = [], None
        for index in sorted(part_tree.query(point, predicate='intersects')):
            _, _, part, coords = parts[index]
            end, _, remainder = _open_endpoint_occurrence(part, coords, coordinate, LineExtensionError)
            inner = remainder[0] if end == 'start' else remainder[-1]
            outward = tuple(origin[i]-Fraction(inner[i]) for i in (0,1))
            if direction is not None and (_cross_xy(direction, outward) or
                    sum(a*b for a,b in zip(direction, outward)) <= 0):
                raise LineExtensionError('direction')
            direction = outward if direction is None else direction
            occurrences.append((index, end))
        if direction is None:
            raise LineExtensionError('endpoint')
        x, y = coordinate
        envelope = shapely.box(
            max(-limit, nextafter(x-maximum, -float('inf'))),
            max(-limit, nextafter(y-maximum, -float('inf'))),
            min(limit, nextafter(x+maximum, float('inf'))),
            min(limit, nextafter(y+maximum, float('inf'))))
        squared_direction = sum(value*value for value in direction)
        first, selected = None, set()
        for segment_row in tree.query(envelope):
            t = _ray_segment_entry(origin, direction, segments[segment_row])
            if t is None or t*t*squared_direction > maximum_squared:
                continue
            if first is None or t < first:
                first, selected = t, {owners[segment_row]}
            elif t == first:
                selected.add(owners[segment_row])
        if first is None:
            continue
        exact_target = tuple(origin[i]+first*direction[i] for i in (0,1))
        target = tuple(map(float, exact_target))
        if any(Fraction(value) != exact for value, exact in zip(target, exact_target)):
            raise LineExtensionError('landing')
        covered = {owners[i] for i in tree.query(shapely.Point(target), predicate='intersects')}
        if not selected <= covered:
            raise LineExtensionError('landing')
        distance = hypot(*(float(first*value) for value in direction))
        if not np.isfinite(distance) or not 0 < distance <= maximum:
            raise LineExtensionError('calculation')
        for occurrence in occurrences:
            plans[occurrence] = (coordinate, target, distance, sorted(selected))

    rebuilt = {}
    rows, references, numbers, ends, originals, targets, distances = [], [], [], [], [], [], []
    for index, (row, number, part, coords) in enumerate(parts):
        values = coords.copy()
        for end in ('start','end'):
            if (index,end) not in plans:
                continue
            original, target, distance, selected = plans[index,end]
            if end == 'start':
                values.insert(0,target)
            else:
                values.append(target)
            for owner in selected:
                rows.append(row); references.append(owner); numbers.append(number); ends.append(end)
                originals.append(original); targets.append(target); distances.append(distance)
        rebuilt.setdefault(row,[]).append(shapely.LineString(values) if values != coords else part)
    output = input_gdf.geometry.array.copy()
    for row, row_parts in rebuilt.items():
        output[row] = row_parts[0] if input_gdf.geometry.iloc[row].geom_type == 'LineString' else shapely.MultiLineString(row_parts)
    result = input_gdf.copy()
    result[result.geometry.name] = gpd.GeoSeries(output,index=input_gdf.index,crs=input_gdf.crs)
    try:
        _finite_xy_geometry(result,'result')
        _require_nonempty_valid_lines(result.geometry)
    except ValueError as exc:
        raise LineExtensionError('invalid') from exc
    details = gpd.GeoDataFrame({
        'source_id': input_gdf[id_field].iloc[rows].reset_index(drop=True),
        'reference_id': reference_ids.iloc[references].reset_index(drop=True),
        'part_no': pd.Series(numbers,dtype='int64'), 'end': pd.Series(ends,dtype='string'),
        'original_x': pd.Series([p[0] for p in originals],dtype='float64'),
        'original_y': pd.Series([p[1] for p in originals],dtype='float64'),
        'target_x': pd.Series([p[0] for p in targets],dtype='float64'),
        'target_y': pd.Series([p[1] for p in targets],dtype='float64'),
        'distance': pd.Series(distances,dtype='float64'),
    },geometry=gpd.GeoSeries([shapely.LineString([a,b]) for a,b in zip(originals,targets)],crs=input_gdf.crs))
    return {'result':result,'details':details}


class LineTrimError(_LineReferenceError):
    """Typed admission facts are distinguished from numerical calculation failures."""

    _messages = {
        'zh-cn': {
            'tolerance': '线端修剪的最大路径距离必须是以坐标系水平单位表示的有限正数。',
            'source': '线端修剪的源图层须具有有效有限二维线、已知投影坐标系及非空唯一标识。',
            'reference': '线端修剪的参考图层须具有有效有限二维线、已知投影坐标系及非空唯一标识。',
            'crs': '线端修剪要求两个图层使用相同的已知投影坐标系和匹配的水平单位。',
            'topology': '线端修剪不接受源部件内部自交或源线正长度重合；请先显式检查规整。',
            'conflict': '线端修剪的两端计划交错或相遇，会使原部件消失；未生成任何成果。',
            'landing': '线端修剪的首个交点不能精确落在原线及参考线上；未生成任何成果。',
            'invalid': '线端修剪产生塌缩、无效或非有限几何；未生成任何成果。',
            'calculation': '线端修剪无法可靠计算有限的首个交点及路径长度；未生成任何成果。',
        },
        'en': {
            'tolerance': 'The maximum trim path distance must be a finite positive number in horizontal CRS units.',
            'source': 'Line trimming requires valid finite XY source lines, a known projected CRS and unique non-null IDs.',
            'reference': 'Line trimming requires valid finite XY reference lines, a known projected CRS and unique non-null IDs.',
            'crs': 'Line trimming requires the same known projected CRS and matching horizontal units.',
            'topology': 'Line trimming rejects internal self-intersections or positive-length source overlaps; check and normalize explicitly first.',
            'conflict': 'The two trim plans cross or meet and would remove an original part; no result was produced.',
            'landing': 'The first trim intersection cannot lie exactly on both the original source and reference lines; no result was produced.',
            'invalid': 'Line trimming produced collapsed, invalid or non-finite geometry; no result was produced.',
            'calculation': 'Line trimming cannot reliably compute a finite first intersection and path length; no result was produced.',
        },
    }


def _trim_segment_contact(start, end, other):
    """Return the exact closed-segment entry and exit parameters (binary XY)."""
    origin, finish = tuple(map(Fraction, start)), tuple(map(Fraction, end))
    a, b = (tuple(map(Fraction, xy)) for xy in other.coords)
    direction = tuple(finish[i] - origin[i] for i in (0, 1))
    edge = tuple(b[i] - a[i] for i in (0, 1))
    delta = tuple(a[i] - origin[i] for i in (0, 1))
    denominator = _cross_xy(direction, edge)
    if denominator:
        t, u = _cross_xy(delta, edge) / denominator, _cross_xy(delta, direction) / denominator
        return (t, t) if 0 <= t <= 1 and 0 <= u <= 1 else None
    if _cross_xy(delta, direction):
        return None
    axis = 0 if direction[0] else 1
    low, high = sorted(((a[axis] - origin[axis]) / direction[axis],
                        (b[axis] - origin[axis]) / direction[axis]))
    low, high = max(Fraction(0), low), min(Fraction(1), high)
    return (low, high) if low <= high else None


def _trim_source_network(parts):
    """Reject overlaps and count exact incident half-edges without rounded noding."""
    segments, incidence, entries = [], {}, {}
    for index, (_, _, part, coords) in enumerate(parts):
        if not part.is_simple:
            raise LineTrimError('topology')
        for offset, (a, b) in enumerate(pairwise(coords)):
            if a == b:
                continue
            segment = shapely.LineString([a, b])
            number = len(segments)
            segments.append(segment)
            entries[index, offset] = number
            for xy in (a, b):
                incidence.setdefault(tuple(map(Fraction, xy)), set()).add(number)
    tree = shapely.STRtree(segments)
    for left, segment in enumerate(segments):
        start, end = segment.coords
        for right in tree.query(segment):
            if right <= left:
                continue
            contact = _trim_segment_contact(start, end, segments[right])
            if contact is None:
                continue
            low, high = contact
            if low < high:
                raise LineTrimError('topology')
            point = tuple(Fraction(start[i]) + low * (Fraction(end[i]) - Fraction(start[i])) for i in (0, 1))
            incidence.setdefault(point, set()).update((left, int(right)))
    degrees = {}
    branches = {}
    for point, members in incidence.items():
        degree = sum(1 if point in {tuple(map(Fraction, xy)) for xy in segments[i].coords} else 2 for i in members)
        degrees[point] = degree
        if degree >= 3:
            for i in members:
                branches.setdefault(i, []).append(point)
    return entries, degrees, branches


def _trim_endpoint_plan(coords, end, index, entries, branches, segments, owners, tree, maximum):
    """Walk the original path once, bounded by the first branch and reference hit."""
    oriented = coords if end == 'start' else coords[::-1]
    lengths = []
    for offset, (a, b) in enumerate(pairwise(oriented)):
        if a == b:
            continue
        source_offset = offset if end == 'start' else len(coords) - 2 - offset
        segment_no = entries[index, source_offset]
        axis = 0 if a[0] != b[0] else 1
        delta = Fraction(b[axis]) - Fraction(a[axis])
        branch = min(((point[axis] - Fraction(a[axis])) / delta
                      for point in branches.get(segment_no, [])), default=None)
        segment = shapely.LineString([a, b])
        first, selected = None, set()
        for ref_index in tree.query(segment):
            contact = _trim_segment_contact(a, b, segments[ref_index])
            if contact is None:
                continue
            t = contact[0]
            if first is None or t < first:
                first, selected = t, {owners[ref_index]}
            elif t == first:
                selected.add(owners[ref_index])
        length = hypot(b[0] - a[0], b[1] - a[1])
        if not np.isfinite(length):
            raise LineTrimError('calculation')
        if first is not None and (branch is None or first <= branch):
            partial = hypot(*(float(first * (Fraction(b[i]) - Fraction(a[i]))) for i in (0, 1)))
            distance = fsum([*lengths, partial])
            if not np.isfinite(distance):
                raise LineTrimError('calculation')
            if distance == 0:
                if not lengths and first == 0:
                    # Already touching a reference at the original endpoint.
                    return None
                raise LineTrimError('calculation')
            if distance > maximum:
                return None
            exact = tuple(Fraction(a[i]) + first * (Fraction(b[i]) - Fraction(a[i])) for i in (0, 1))
            target = tuple(map(float, exact))
            if any(Fraction(value) != rational for value, rational in zip(target, exact)):
                raise LineTrimError('landing')
            point = shapely.Point(target)
            covered = {owners[i] for i in tree.query(point, predicate='intersects')}
            if not selected <= covered or not segment.covers(point):
                raise LineTrimError('landing')
            position = Fraction(offset) + first
            if end == 'end':
                position = len(coords) - 1 - position
            return position, target, distance, sorted(selected)
        if branch is not None:
            return None
        lengths.append(length)
        if fsum(lengths) > maximum:
            return None
    return None


def trim_line_endpoints(
    input_gdf: gpd.GeoDataFrame, reference_gdf: gpd.GeoDataFrame,
    id_field: str, reference_id_field: str, max_distance: float,
) -> dict[str, gpd.GeoDataFrame]:
    """Trim frozen original dangles along their path, preserving all target owners."""
    try:
        return _trim_line_endpoints(input_gdf, reference_gdf, id_field, reference_id_field, max_distance)
    except (shapely.errors.GEOSException, OverflowError, ZeroDivisionError) as exc:
        raise LineTrimError('calculation') from exc


def _trim_line_endpoints(input_gdf, reference_gdf, id_field, reference_id_field, max_distance):
    _, reference, reference_ids, maximum = _line_reference_inputs(
        input_gdf, reference_gdf, id_field, reference_id_field, max_distance, LineTrimError)
    parts = [(row, number, part, list(part.coords)) for row, shape in enumerate(input_gdf.geometry.array)
             for number, part in enumerate(shapely.get_parts(shape), 1)]
    entries, degrees, branches = _trim_source_network(parts)
    segments, owners, tree = _reference_line_segments(reference)
    rebuilt = {}
    rows, references, numbers, ends, originals, targets, distances = [], [], [], [], [], [], []
    for index, (row, number, part, coords) in enumerate(parts):
        plans = {}
        if not part.is_closed:
            for end, coordinate in [('start', coords[0]), ('end', coords[-1])]:
                if degrees[tuple(map(Fraction, coordinate))] != 1:
                    continue
                plan = _trim_endpoint_plan(coords, end, index, entries, branches,
                                           segments, owners, tree, maximum)
                if plan is not None:
                    plans[end] = plan
        start = plans['start'][0] if 'start' in plans else Fraction(0)
        stop = plans['end'][0] if 'end' in plans else Fraction(len(coords) - 1)
        if start >= stop:
            raise LineTrimError('conflict')
        values = [xy for i, xy in enumerate(coords) if start <= i <= stop]
        if start.denominator != 1:
            values.insert(0, plans['start'][1])
        if stop.denominator != 1:
            values.append(plans['end'][1])
        if len(values) < 2 or not any(xy != values[0] for xy in values[1:]):
            raise LineTrimError('conflict')
        rebuilt.setdefault(row, []).append(part if not plans else shapely.LineString(values))
        for end in ('start', 'end'):
            if end not in plans:
                continue
            _, target, distance, selected = plans[end]
            for owner in selected:
                rows.append(row); references.append(owner); numbers.append(number); ends.append(end)
                originals.append(coords[0] if end == 'start' else coords[-1])
                targets.append(target); distances.append(distance)
    output = input_gdf.geometry.array.copy()
    for row, row_parts in rebuilt.items():
        output[row] = row_parts[0] if input_gdf.geometry.iloc[row].geom_type == 'LineString' else shapely.MultiLineString(row_parts)
    result = input_gdf.copy()
    result[result.geometry.name] = gpd.GeoSeries(output, index=input_gdf.index, crs=input_gdf.crs)
    try:
        _finite_xy_geometry(result, 'result')
        _require_nonempty_valid_lines(result.geometry)
    except ValueError as exc:
        raise LineTrimError('invalid') from exc
    details = gpd.GeoDataFrame({
        'source_id': input_gdf[id_field].iloc[rows].reset_index(drop=True),
        'reference_id': reference_ids.iloc[references].reset_index(drop=True),
        'part_no': pd.Series(numbers, dtype='int64'), 'end': pd.Series(ends, dtype='string'),
        'original_x': pd.Series([xy[0] for xy in originals], dtype='float64'),
        'original_y': pd.Series([xy[1] for xy in originals], dtype='float64'),
        'target_x': pd.Series([xy[0] for xy in targets], dtype='float64'),
        'target_y': pd.Series([xy[1] for xy in targets], dtype='float64'),
        'distance': pd.Series(distances, dtype='float64'),
    }, geometry=gpd.GeoSeries([shapely.Point(xy) for xy in targets], crs=input_gdf.crs))
    return {'result': result, 'details': details}


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

    # Reuse the geometry-only kernel, without per-feature overlay attributes
    # or implicit make_valid. A coverage hole is an explicit excluded region.
    uncovered = shapely.difference(
        _dissolve_geometry(coverage.array), _dissolve_geometry(geometry.array),
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


class ReferenceSnapInputError(ValueError):
    """Explicit reference-snap admission facts; native failures are not wrapped."""
    _messages = {
        'zh-cn': {
            'source': '参考吸附源输入须为具有活动几何的GeoDataFrame，使用有效有限二维XY，不接受Z/M；NULL/EMPTY保留原样，请显式修复无效输入。',
            'reference': '参考吸附的参考输入须为具有活动几何的GeoDataFrame，使用有效有限二维XY，不接受Z/M；NULL/EMPTY不提供参考顶点，空参考不移动输入。',
            'crs': '参考吸附的双方须使用相同已知投影CRS，两个水平轴单位须匹配且换算因子有限为正；请显式重投影，容差沿原单位。',
            'tolerance': '参考吸附的tolerance须为有限非负数，不能为布尔值，单位沿投影CRS；零容差不移动，正容差仅移动原始距离严格小于上限的顶点。',
        },
        'en': {
            'source': 'Reference snapping requires a source GeoDataFrame with an active geometry and valid finite XY coordinates, without Z/M. NULL/EMPTY remain unchanged; repair invalid input explicitly.',
            'reference': 'Reference snapping requires a reference GeoDataFrame with an active geometry and valid finite XY coordinates, without Z/M. NULL/EMPTY provide no vertices; an empty reference leaves the input unchanged.',
            'crs': 'Reference snapping requires the same known projected CRS on both inputs and matching horizontal units with finite positive conversion factors. Reproject explicitly; tolerance uses the original CRS units.',
            'tolerance': 'Reference snapping requires tolerance to be a finite non-negative number, not a boolean, in the projected CRS units. Zero does not move vertices; positive tolerance requires a strictly smaller original distance.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    @classmethod
    def source(cls, _message):
        return cls('source')

    @classmethod
    def reference(cls, _message):
        return cls('reference')

    @classmethod
    def crs(cls, _message):
        return cls('crs')

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def snap_geometry(
    input_gdf: gpd.GeoDataFrame,
    reference_gdf: gpd.GeoDataFrame,
    tolerance: float,
) -> gpd.GeoDataFrame:
    """Snap against a fixed reference vertex set in shared projected CRS units."""
    geometry = _finite_xy_geometry(input_gdf, input_error=ReferenceSnapInputError.source)
    reference = _finite_xy_geometry(reference_gdf, "reference_gdf", input_error=ReferenceSnapInputError.reference)
    if isinstance(tolerance, (bool, np.bool_)) or not isinstance(tolerance, Real):
        raise ReferenceSnapInputError('tolerance')
    try:
        tolerance = float(tolerance)
    except OverflowError as exc:
        raise ReferenceSnapInputError('tolerance') from exc
    if not np.isfinite(tolerance) or tolerance < 0:
        raise ReferenceSnapInputError('tolerance')
    crs = geometry.crs
    if crs is None or reference.crs is None or not crs.is_projected or not crs.equals(reference.crs):
        raise ReferenceSnapInputError('crs')
    _require_projected_xy_crs(crs, input_error=ReferenceSnapInputError.crs)
    for name, values in [("input_gdf", geometry.array), ("reference_gdf", reference.array)]:
        if (~shapely.is_missing(values) & ~shapely.is_valid(values)).any():
            raise ReferenceSnapInputError('source' if name == 'input_gdf' else 'reference')
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


class PolygonizeInputError(ValueError):
    """An explicit rejection of the input linework."""


def polygonize(input_gdf: gpd.GeoDataFrame) -> dict[str, gpd.GeoDataFrame]:
    """Build planar faces and report unused edges without changing source linework."""
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise PolygonizeInputError("input_gdf must be a GeoDataFrame")
    try:
        geometry = input_gdf.geometry
    except AttributeError as exc:
        raise PolygonizeInputError("input_gdf requires an active geometry column") from exc

    if geometry.isna().any() or not geometry.geom_type.isin(["LineString", "MultiLineString"]).all():
        raise PolygonizeInputError("input_gdf requires non-null LineString or MultiLineString geometries")
    parts = shapely.get_parts(geometry.array)
    if shapely.is_empty(geometry.array).any() or shapely.is_empty(parts).any():
        raise PolygonizeInputError("input_gdf must not contain empty line geometries")
    if shapely.has_z(parts).any() or shapely.has_m(parts).any():
        raise PolygonizeInputError("input_gdf requires two-dimensional XY line geometries")
    if not np.isfinite(shapely.get_coordinates(parts)).all() or not shapely.is_valid(parts).all():
        raise PolygonizeInputError("input_gdf requires valid lines with finite coordinates")

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

NORMALIZE_LINE_NETWORK_METADATA = OperatorMetadata(
    name="normalize_line_network", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_NETWORK,
    description="重合规整", brief_description="精确细分并去重线网，共享边与完整来源经过双输出",
    execution_modes=["workflow"], effects=["read"],
    overview="按全部原顶点、精确点交会和重合端点细分，将正反等价直线段归为唯一边。"
             "唯一边不选择业务属性；明细保留各次经过、全部原属性、原段位置和方向关系。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待规整的单个二维投影线图层", notes="有效有限XY LineString/MultiLineString，拒绝缺失和空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何唯一非空标量列，不使用索引或存储FID替代"),
    ],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="每唯一边一条LineString，仅几何及edge_id/occurrence_count，不含业务属性", is_default=True),
        OutputPort(name="members", type="geodataframe", description="逐次片段及全部原列，追加edge_id/source_id/source_part_no/source_segment_no/piece_no/is_reversed", is_default=False),
    ],
    use_cases=["将内部及跨要素重合线规整为唯一边", "保留反向或多次经过的完整来源供业务决定属性归属", "为后续连通或链路处理建立精确线段"],
    notes=["边方向按端点XY字典序；边号按原路径首次出现，只关联本次双成果", "occurrence_count统计经过次数，同源重复经过分别记录；业务属性只在members保留，不取首值", "原顶点均分段，不合并链路；零长度原段不生成明细，原段号保留其位置间隔", "固定输出字段与源列冲突即整批失败；保留属性类型、原CRS及原始经过方向，源不变", "无容差、精度网格、吸附、补缝或桥隧判断；保存分别接现有save", "索引、切点和双成果驻内存，高密度候选和明细可增长，不承诺固定容量"],
    workflow_example={"id":"normalize_lines", "operator":"normalize_line_network", "params":{"input_gdf":{"$ref":"load"},"id_field":"line_id"},"depends_on":["load"]},
)


EXTRACT_LINE_NODES_METADATA = OperatorMetadata(
    name="extract_line_nodes", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_NETWORK,
    description="节点提取", brief_description="按原始部件端点提取节点及度数，双输出保留每次端部来源和全部属性",
    execution_modes=["workflow"], effects=["read"],
    overview="每原始LineString部件为边，精确XY端点合并为节点；闭环首尾计两次，同源多部件各计。"
             "内部折点不提取，不自动打断、去重、吸附或修复。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True,
            description="已在交会处打断且无重合的有效有限二维投影线图层"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True,
            description="现有非几何唯一非空来源标识字段，不推断索引或存储FID"),
    ],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="每节点一个Point，仅几何与node_id/degree", is_default=True),
        OutputPort(name="members", type="geodataframe", description="每部件起终端各一Point，保原列及node_id/source_id/source_part_no/end", is_default=False),
    ],
    use_cases=["提取已规整线网的端点和分支节点", "按端部逐次追溯全部来源及业务属性"],
    notes=["闭环首尾同点度为2，明细保留start和end两行；同源多部件不去重", "未打断交会、内部自交与任何正长度重合整批拒绝；相同端点的不同非重合路径分别计边", "普通内部折点不提节点，连续重复坐标不增度；节点集合依赖实际部件分段", "完整明细指当前输入每次端部及全部属性，不复制原线或猜上游来源", "node_id/degree/source_id/source_part_no/end冲突按固定顺序报错，用户显式重命名", "源不变，两Point端口分别接save；节点ID仅关联本次结果，不承诺固定容量"],
    workflow_example={"id":"nodes", "operator":"extract_line_nodes", "params":{"input_gdf":{"$ref":"load"},"id_field":"line_id"}, "depends_on":["load"]},
)


MERGE_LINE_CHAINS_METADATA = OperatorMetadata(
    name="merge_line_chains", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_NETWORK,
    description="链路合并", brief_description="按精确端点和显式一致字段串联非分支线，双输出保留完整来源明细",
    execution_modes=["workflow"], effects=["read"],
    overview="全图度二且显式字段同值时串联，分支或属性变化处停止，不选择主路。"
             "主结果保存共同属性，独立明细保留每个原始部件、全部原属性和沿链顺序及反向标记。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="已在交点打断的有效二维投影线图层"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="现有唯一非空原始行标识列，不使用存储FID推断"),
        OperatorParam(name="match_fields", type="param", data_type="list[str]", required=True, description="须精确一致的业务字段；显式空数组表示仅按连接合并", notes="不能含来源ID或几何列；缺失与缺失一致，不取首值或聚合其他属性"),
    ],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="每链一条LineString，保留显式共同字段及chain_id/member_count", is_default=True),
        OutputPort(name="members", type="geodataframe", description="原始部件和全部原列，追加chain_id/member_no/source_part_no/is_reversed", is_default=False),
    ],
    use_cases=["按相同名称和等级串联已打断道路", "整理非分支管线并逐部件追溯原始属性", "在分支及属性变化处保留链路边界"],
    notes=["全图度先于属性；三岔或更高分支全部停止，不按角度或属性选择主路", "未打断交会、内部自交及任何正长度重合整批拒绝；不吸附、打断、去重或修复", "允许反转部件以连接，明细几何保原方向及全部顶点，记录沿链顺序和反向标记", "链号只关联本次双成果；输出字段冲突明确失败，不覆盖或自动改名", "两个端口分别接save；索引及成果驻内存，不承诺固定容量"],
    workflow_example={"id":"merge_chains", "operator":"merge_line_chains", "params":{"input_gdf":{"$ref":"load"},"id_field":"segment_id","match_fields":["name","grade"]},"depends_on":["load"]},
)


EXTRACT_POLYGON_RINGS_METADATA = OperatorMetadata(
    name="extract_polygon_rings", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_NETWORK,
    description="面界提取", brief_description="逐原始内外环生成闭合线，保留来源、属性、方向及共有边的各次记录",
    execution_modes=["workflow"], effects=["read"],
    overview="按源行、面部件、外环和原顺序内环逐环输出LineString。"
             "不合并共有边或重复面，不推断边界归属，保留原始顶点、起点与方向。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="有效有限二维面图层", notes="Polygon/MultiPolygon；拒绝缺失、空几何、空部件/空环、Z/M和无效面，允许空批；原CRS不变"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源面要素标识字段", notes="显式引用现有非几何唯一非空标量列，不使用索引或存储FID推断"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="每环一条闭合LineString，保留全部原列并追加source_id/part_no/ring_no/ring_id/is_hole", is_default=True)],
    use_cases=["提取地块外环和孔洞，保留每条边界的原始来源", "使用唯一环ID衔接线网加工并保留上游面属性"],
    notes=["部件号在源内从1开始；环号在每部件内从1开始，1为外环、2起为内环，is_hole只表达原始角色", "ring_id按结果顺序从1连续编号，只在本次结果内唯一，不是持久身份", "source_id/part_no/ring_no/ring_id/is_hole冲突时整批失败，不覆盖或自动改名", "后续线网算子以ring_id作为本级来源；同名追加字段先用rename_fields显式保留为上游名称", "保留原始CRS，不计算距离或面积；后续投影要求仍须显式重投影", "无去重、打断、吸附或隐式修复；源不变，保存另接save，成果随环和顶点数增长"],
    workflow_example={"id":"polygon_rings", "operator":"extract_polygon_rings", "params":{"input_gdf":{"$ref":"load"},"id_field":"parcel_id"}, "depends_on":["load"]},
)


GROUP_LINE_COMPONENTS_METADATA = OperatorMetadata(
    name="group_line_components", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_NETWORK,
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
    name="split_line_intersections", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_NETWORK,
    description="交点打断", brief_description="按显式范围切分跨要素或全部精确点交会，保留来源、属性和原路径顺序",
    execution_modes=["workflow"], effects=["read"],
    overview="十字交叉切分双方，T形切分被接入的线，端点接触不生成零长度段。"
             "scope=between处理跨要素点交会，scope=all同时处理要素内部点交会。"
             "任何正长度重合均整批拒绝，不去重或自动修复。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待切分的单个线图层", notes="有效有限二维线，已知投影 CRS；拒绝缺失及空几何，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="来源要素标识字段", notes="现有非几何唯一非空标量列；不推断主键或使用索引"),
        OperatorParam(name="scope", type="param", data_type="str", required=False, default="between", enum=["between", "all"], description="点交会作用范围", notes="between：仅跨要素，内部点交会拒绝；all：要素内部及跨要素全部点交会。两者均拒绝正长度重合"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="每行一个LineString，保留原列并追加source_id/part_no/segment_no", is_default=True)],
    use_cases=["将真实十字或T形交会切分为可独立加工的线段", "保留原属性和来源生成线网片段"],
    notes=["部件号和片段号从1开始，片段号在部件内递增；保留方向、顶点和闭环原始接缝", "source_id/part_no/segment_no与源列冲突时失败，不覆盖或自动改名", "all按原路径每次经过分别切分；正常相邻折点和闭环首尾不额外制造切口", "正长度重合包括相邻折返、源内重复走线、同源多部件及跨源重合，均整批拒绝", "无容差、精度网格、吸附、延伸或业务质量判定；源不变，保存另接save", "索引、交点和成果驻内存，不承诺固定内存或容量"],
    workflow_example={"id": "split_crossings", "operator": "split_line_intersections", "params": {"input_gdf": {"$ref": "load"}, "id_field": "line_id", "scope": "all"}, "depends_on": ["load"]},
)

CHECK_LINE_SELF_OVERLAP_METADATA = OperatorMetadata(
    name="check_line_self_overlap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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
    name="check_line_overlap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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
    name="check_line_self_intersection", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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
    name="check_line_intersection", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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
    name="check_dangling_points", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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

CHECK_LINE_PROXIMITY_METADATA = OperatorMetadata(
    name="check_line_proximity", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
    description="近接检查", brief_description="报告未精确接上的源悬点与容差内全部参考线，只检查不移动几何",
    execution_modes=["workflow"], effects=["read"],
    overview="复用源线网悬点提取；已精确落在任一参考线上的点不再报告。"
             "其余悬点逐参考要素保留严格容差内的最近位置、双方来源和原单位距离，不替用户选择连接对象。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="源线图层", notes="有效有限二维线与已知投影 CRS；复用悬点检查，允许空批，拒绝缺失或空几何"),
        OperatorParam(name="reference_gdf", type="input", data_type="GeoDataFrame", required=True, description="参考线图层", notes="有效有限二维线，与源使用相同已知投影 CRS 和水平单位；参考图层不动"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="源线要素标识字段", notes="现有非几何标量列，值非空且唯一；不使用行索引或 FID"),
        OperatorParam(name="reference_id_field", type="param", data_type="str", required=True, description="参考线要素标识字段", notes="参考图层现有非几何标量列，值非空且唯一"),
        OperatorParam(name="tolerance", type="param", data_type="float", required=True, description="有限正容差，单位沿源投影坐标系", notes="仅报告0＜距离＜容差，边界不报，不自动换算米"),
    ],
    output_ports=[OutputPort(name="result", type="geodataframe", description="source_id/reference_id/distance/nearest_x/nearest_y/原悬点Point，保留双方标识类型和源CRS", is_default=True)],
    use_cases=["定位差一点接上参考道路的悬点及全部候选", "检查小断口后由用户决定连接对象"],
    notes=["精确落在任一参考线上的悬点整体排除；其余保留全部严格容差内参考要素，不只取全局最近一条", "每个悬点与参考要素对输出一次；等距最近位置按X/Y稳定决胜，不按部件或索引遍历顺序", "按源行/悬点XY/参考行排序，输出原悬点及最近坐标，不复制业务属性", "同图层接到两个输入时，自身覆盖端点按精确相接规则排除，不增加自动排除自身模式", "不重投影、吸附、延伸或修复，不生成Quality结论；保存另接save", "线网、参考段索引和成果驻内存，不承诺固定容量"],
    workflow_example={"id": "check_near", "operator": "check_line_proximity", "params": {"input_gdf": {"$ref": "load"}, "reference_gdf": {"$ref": "reference"}, "id_field": "line_id", "reference_id_field": "ref_id", "tolerance": 1.0}, "depends_on": ["load", "reference"]},
)

SNAP_LINE_ENDPOINTS_METADATA = OperatorMetadata(
    name="snap_line_endpoints", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
    description="端点吸附", brief_description="将可完整移动的原始悬点端部一次吸到唯一精确参考位置，保留原属性和全部来源",
    execution_modes=["workflow"], effects=["read"],
    overview="沿源线网真实悬点查找固定参考线的全局唯一最近位置，仅移动原始开口部件的首尾。"
             "同时返回保留原业务属性的线和逐端点／参考来源的移动明细，不隐式规整或修复其他问题。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待移动端点的源线图层", notes="有效有限二维线，使用实际活动几何列；允许空批，拒绝缺失或空几何"),
        OperatorParam(name="reference_gdf", type="input", data_type="GeoDataFrame", required=True, description="固定参考线图层", notes="与源使用相同的已知投影CRS，参考线不动"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="源线唯一非空标识字段", notes="显式非几何标量列，不用索引或FID替代"),
        OperatorParam(name="reference_id_field", type="param", data_type="str", required=True, description="参考线唯一非空标识字段", notes="每个实际覆盖目标的参考要素分别保留来源"),
        OperatorParam(name="tolerance", type="param", data_type="float", required=True, description="原始位置的有限正容差", notes="单位沿源投影水平单位，只允许0＜距离＜容差，不连锁移动"),
    ],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="保留源属性、行序、索引、部件和CRS的线图层", is_default=True),
        OutputPort(name="details", type="geodataframe", description="原端点Point及双方来源、part_no/end、original_x/y、target_x/y、distance", is_default=False),
    ],
    use_cases=["按固定参考线显式连接近接悬点", "审计每个实际移动端点和全部参考来源"],
    notes=["已精确接上参考线的悬点不动，其他目标只按冻结原始位置查询一次", "等距异点、不能仅移动端点完整处理或目标不能精确落线时整批失败，不按XY或行序选择路径", "端部连续重复坐标一并移动，保留顶点数；原始正长度段塌缩或无效结果整批失败", "不移动内部顶点、闭环或参考，不吸附到已移动源，不保证全部拓扑问题消除", "result与details分别接现有save，不承诺跨文件事务或固定规模容量"],
    workflow_example={"id":"snap_ends", "operator":"snap_line_endpoints", "params":{"input_gdf":{"$ref":"source"},"reference_gdf":{"$ref":"reference"},"id_field":"line_id","reference_id_field":"ref_id","tolerance":1.0},"depends_on":["source","reference"]},
)

EXTEND_LINE_ENDPOINTS_METADATA = OperatorMetadata(
    name="extend_line_endpoints", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
    description="线端延伸", brief_description="沿原悬点朝外方向延伸到上限内首次参考交点，保留原属性和全部来源",
    execution_modes=["workflow"], effects=["read"],
    overview="沿原始开口部件方向延伸悬点，上限包含等号，共线取重合入口。返回原属性线副本与逐来源新增段明细，不移动参考、不连锁或隐式修复。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待延伸的源线图层", notes="有效有限二维线、已知投影CRS，使用实际活动几何列；允许空批"),
        OperatorParam(name="reference_gdf", type="input", data_type="GeoDataFrame", required=True, description="固定参考线图层", notes="相同已知投影CRS，参考线不动"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="源线唯一非空标识字段"),
        OperatorParam(name="reference_id_field", type="param", data_type="str", required=True, description="参考线唯一非空标识字段", notes="首次目标的全部参考要素分别保留来源"),
        OperatorParam(name="max_distance", type="param", data_type="float", required=True, description="最大延伸距离", notes="有限正数，单位沿投影CRS；0＜延伸长度≤上限，无目标保持原线"),
    ],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="保留源属性、行序、索引、原顶点、方向、部件和CRS的延伸线", is_default=True),
        OutputPort(name="details", type="geodataframe", description="原端点至目标的新增线段及全部source_id/reference_id、part_no/end、坐标和distance", is_default=False),
    ],
    use_cases=["沿端部原方向连接参考线断口", "审计新增延伸段和全部参考来源"],
    notes=["只处理全图悬点；两源冻结，一次计算，不连锁", "首次参考点命中或共线区间入口即停止，不能越过首个不可精确表示的交点", "同点全部参考来源保留，同一参考重复段去重；朝外方向不一致或原始内部覆盖整批失败", "不使用epsilon、网格或舍入；不插入参考节点、不打断新增交叉、不合并链路", "两输出分别连接既有save，不提供跨文件事务或固定规模容量"],
    workflow_example={"id":"extend", "operator":"extend_line_endpoints", "params":{"input_gdf":{"$ref":"source"},"reference_gdf":{"$ref":"reference"},"id_field":"line_id","reference_id_field":"ref_id","max_distance":2.0},"depends_on":["source","reference"]},
)

TRIM_LINE_ENDPOINTS_METADATA = OperatorMetadata(
    name="trim_line_endpoints", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
    description="线端修剪", brief_description="沿原悬端向内修剪到距离上限内首次参考交会，分支处停止并保留全部来源",
    execution_modes=["workflow"], effects=["read"],
    overview="按原路径累计长度搜索首次精确参考交会，共线取入口，上限含等号，不跨部件或越过分支。两端冻结同时计划，返回保留属性的线与目标Point明细。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="待修剪源线图层", notes="有效有限二维线、已知投影CRS、实际活动几何列；允许空批，内部自交或源线正长度重合拒绝"),
        OperatorParam(name="reference_gdf", type="input", data_type="GeoDataFrame", required=True, description="固定参考线图层", notes="相同已知投影CRS，参考线不动"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="源线唯一非空标识字段"),
        OperatorParam(name="reference_id_field", type="param", data_type="str", required=True, description="参考线唯一非空标识字段"),
        OperatorParam(name="max_distance", type="param", data_type="float", required=True, description="每端最大修剪路径长度", notes="有限正数，沿投影水平单位；0＜原路径累计长度≤上限，包含等号"),
    ],
    output_ports=[
        OutputPort(name="result", type="geodataframe", description="保留来源属性、行序、索引、部件、方向、活动几何列及CRS的线", is_default=True),
        OutputPort(name="details", type="geodataframe", description="目标Point及全部source_id/reference_id、part_no/end、original_x/y、target_x/y、distance路径长度", is_default=False),
    ],
    use_cases=["按固定参考线显式修剪悬端过冲", "审计各端修剪长度和全部参考来源"],
    notes=["只处理原全图悬端；已在参考上、无目标或分支先到时保持原状，参考命中分支可剪到该点", "原始路径首交点不能精确落线、两端计划冲突／整部件消失或无效时整批失败，不舍入或选更晚交点", "不吸附、补缝、跨部件追线或隐式修复；不承诺跨文件事务或固定容量", "result和details分别接既有save；已知输入及计划冲突沿统一参数诊断，数值计算失败不误作参数错误"],
    workflow_example={"id":"trim", "operator":"trim_line_endpoints", "params":{"input_gdf":{"$ref":"source"},"reference_gdf":{"$ref":"reference"},"id_field":"line_id","reference_id_field":"ref_id","max_distance":2.0},"depends_on":["source","reference"]},
)

CHECK_POLYGON_OVERLAP_METADATA = OperatorMetadata(
    name="check_polygon_overlap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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
    name="check_polygon_gap", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_TOPOLOGY_CHECK,
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
    name="check_geometry", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
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
    name="repair_geometry", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
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
    name="snap_geometry", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
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
    category=OperatorCategory.VECTOR_NETWORK,
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
    name="buffer", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="缓冲区分析", brief_description="逐源生成投影原单位的完整缓冲，保留属性和空结果行",
    execution_modes=["workflow"], effects=["read"],
    overview="对已知投影CRS的每个有效有限二维源几何独立生成完整 Polygon/MultiPolygon 缓冲，距离可正可零可负；收缩或点线的零负缓冲成为空面时仍保留原行、属性、索引及CRS，不自动融合或修复。",
    params=[
        OperatorParam(name="input_gdf",type="input",data_type="GeoDataFrame",required=True,description="源图层",notes="唯一实际活动几何和唯一列名；有效非空有限XY，允许空批；已知投影CRS"),
        OperatorParam(name="distance",type="param",data_type="float",required=True,description="缓冲距离，沿投影CRS原单位",notes="有限实数，可正、零、负；英尺不换算为米，不接受布尔、字符串、数组或非有限值"),
        OperatorParam(name="resolution",type="param",data_type="int",required=False,default=16,description="每四分之一圆的线段数",notes="正整数，默认16；值越大通常越平滑，也会增加计算和内存消耗"),
    ],
    use_cases=["投影坐标系下逐道路生成影响范围", "逐设施生成服务缓冲面", "面向内收缩且保留消失行的来源属性"],
    notes=[
        "地理、地心或未知CRS拒绝；先显式重投影，不能将坐标单位默认解释为米",
        "源缺失/空/无效或Z/M几何明确拒绝；应先几何检查并显式修复，不用简化或零缓冲替代修复",
        "点线零或负缓冲可为空；面负缓冲会内缩，过大时也可为空，全部仍保行",
        "不同源缓冲独立，可能重叠；若需整体覆盖面积，应显式融合后计量，不自动融合",
        "不新增距离属性列，源distance业务字段或活动几何列均可保留；输入不变，不承诺跨GEOS重新计算的WKB相同",
    ],
    workflow_example={'id':'buffer_roads','operator':'buffer','params':{'input_gdf':{'$ref':'load_roads'},'distance':500,'resolution':16},'depends_on':['load_roads']},
)

INTERSECTION_METADATA = OperatorMetadata(
    name="intersection", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_OVERLAY,
    description="相交叠加", brief_description="逐对保留两个图层的完整精确交集、双方来源和业务属性",
    execution_modes=["workflow"], effects=["read"],
    overview="按 A 原行、B 原行顺序，每个非空交集输出一行；点、线、面及 GeometryCollection 全部保留，多部件不拆行。保留双方业务属性和原类型来源，输入不变。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="图层 A", notes="唯一活动几何；有效非空有限二维 XY，允许空批；相同已知投影 CRS"),
        OperatorParam(name="gdf_b", type="input", data_type="GeoDataFrame", required=True, description="图层 B", notes="准入同 A，使用本侧实际活动几何列"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="A 的唯一来源标识列", notes="显式非几何唯一非空标量列，不推断索引或 FID"),
        OperatorParam(name="reference_id_field", type="param", data_type="str", required=True, description="B 的唯一来源标识列", notes="标识类型和值保持不变"),
    ],
    use_cases=["土地利用与保护区逐对叠加", "河流与行政区完整几何交集", "含接触线点的多维度交集追溯"],
    notes=[
        "保留 A 的活动几何列名、双方属性及 source_id_a/source_id_b，重建 RangeIndex；每对至多一行，不按维度过滤或拆分 GeometryCollection",
        "同名业务字段、固定来源名和 B 属性占用结果几何名时明确报错；请先上游手动改名，不自动加后缀或覆盖",
        "无效、缺失、空、Z/M、非有限几何和不匹配投影 CRS 拒绝；不自动修复、吸附、重投影或舍入",
        "目标格式须能表达实际混合几何；纯计算不保存扫描，空间索引不代表固定内存或容量认证",
    ],
    workflow_example={
        'id': 'intersect_area', 'operator': 'intersection',
        'params': {'input_gdf': {'$ref': 'load_source'}, 'gdf_b': {'$ref': 'load_reference'},
                   'id_field': 'source_key', 'reference_id_field': 'reference_key'},
        'depends_on': ['load_source', 'load_reference'],
    },
)

UNION_METADATA = OperatorMetadata(
    name="union", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_OVERLAY,
    description="联合叠加", brief_description="两个面图层联合分区，保留双方来源与属性，缺侧为空",
    execution_modes=["workflow"], effects=["read"],
    overview="生成双方交叠、仅 A、仅 B 三类面结果；每对或每源的多部件保留一行。输入图层内部的正面积重叠、包含和重复面明确拒绝。不是列表拼接或全局融合。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="面图层 A", notes="有效非空有限二维面或多面，允许空批，同层不可正面积重叠"),
        OperatorParam(name="gdf_b", type="input", data_type="GeoDataFrame", required=True, description="面图层 B", notes="与 A 相同已知投影 CRS，合法面或多面，同层不可正面积重叠，允许空批"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="A 的唯一来源标识列", notes="显式非几何唯一非空标量列，不推断索引或 FID"),
        OperatorParam(name="reference_id_field", type="param", data_type="str", required=True, description="B 的唯一来源标识列", notes="显式非几何唯一非空标量列，不推断索引或 FID"),
    ],
    use_cases=["两套地块边界联合分区", "土地利用与行政区面叠加，保留独有范围及双方属性"],
    notes=[
        "交叠按 A/B 原位置排序，之后仅 A、仅 B 各按来源位置排序；共边和共点不单独输出，不拆多部件",
        "保留双方业务字段及原来源列，追加 source_id_a/source_id_b；缺侧为真实空值，整数与布尔按原位宽使用可空表示，不经浮点转换",
        "结果沿 A 的活动几何名及 CRS，重建 RangeIndex；字段冲突须上游手动改名，不自动加后缀或覆盖",
        "一侧为空保留另一侧及完整 Schema；双方为空输出空结果；不吸附、修复、重投影、舍入或增加面积阈值",
        "纯计算不保存或扫描，资源级血缘保留 A/B 两来源；不是单图层融合或多图层列表拼接",
    ],
    workflow_example={'id':'union_regions','operator':'union','params':{'input_gdf':{'$ref':'load_a'},'gdf_b':{'$ref':'load_b'},'id_field':'parcel_key','reference_id_field':'zone_key'},'depends_on':['load_a','load_b']},
)

CENTROID_METADATA = OperatorMetadata(
    name="centroid", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="计算质心", brief_description="逐源按原生几何权重计算平面质心，面外或线外仍为正常结果",
    execution_modes=["workflow"], effects=["read"],
    overview="逐源计算已知投影CRS下的真实平面几何质心：多点等权、线按原段长度、面按扣除孔洞后的面积计权；混合GeometryCollection只由最高维度参与。面外、孔内、部件间空隙或线外Point均正常保留，不替换为面内点。",
    params=[OperatorParam(name="input_gdf",type="input",data_type="GeoDataFrame",required=True,
        description="有效非空有限二维投影源图层",notes="唯一实际活动几何及列名；已知投影CRS和匹配有限正水平线性单位，空批仍验证准入")],
    use_cases=["按几何权重概括多部件要素的位置", "将面或线逐源点化并保留原属性", "分析真实质心与面内代表位置的差别"],
    notes=["Point位置不变，MultiPoint重复点分别贡献；线按线段长度而非顶点数或部件数平均",
        "多面按净面积贡献，孔洞扣除；集合有面计面、否则有线计线、仅点时等权",
        "合法重复经过和重复部件保留原生权重贡献，不先去重或融合，不新增业务属性weight参数",
        "质心不保证落在面内或线上，不是沿线半程点、中心线或面内标注点，不自动替换结果",
        "保原行数、属性类型/null、索引/顺序、实际几何列和CRS；源不变，不跨源汇总",
        "已知投影原单位，不默认米、不自动重投影或修复；缺失/空/无效/Z/M/非有限源拒绝"],
    workflow_example={'id':'centers','operator':'centroid','params':{'input_gdf':{'$ref':'load_projected_shapes'}},'depends_on':['load_projected_shapes']},
)


DIFFERENCE_METADATA = OperatorMetadata(
    name="difference", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_OVERLAY,
    description="几何差集", brief_description="逐源扣除全部面范围，保留 A 的完整剩余几何、来源和业务属性",
    execution_modes=["workflow"], effects=["read"],
    overview="将所有 B 面视为整体扣除范围，逐 A 输出至多一条非空完整差集；点、线、面、多部件及 GeometryCollection 不拆行。保留 A 的来源字段原名、类型、属性、活动几何名及 CRS，输入不变。",
    params=[
        OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True, description="图层 A", notes="唯一活动几何；有效非空有限二维 XY，允许空批"),
        OperatorParam(name="gdf_b", type="input", data_type="GeoDataFrame", required=True, description="扣除范围 B", notes="仅 Polygon/MultiPolygon；允许空批，不要求来源 ID；与 A 相同已知投影 CRS"),
        OperatorParam(name="id_field", type="param", data_type="str", required=True, description="A 的唯一来源标识列", notes="显式非几何唯一非空标量列，保持原名和值，不推断索引或 FID"),
    ],
    use_cases=["地块扣除多个重叠保护区", "道路排除禁行面范围", "点位排除不适用区域"],
    notes=[
        "B 整体范围只扣除一次；完全扣除行省略，每源剩余多部件或集合仍为一行，保持 A 相对行序并重建 RangeIndex",
        "A 全部业务字段及可空类型保持；B 属性不复制，双方同名业务字段不会冲突，不增加同义来源列或量测列",
        "不自动修复、吸附、重投影、舍入或过滤低维结果；几何运算可改变类型、顶点顺序和环方向",
        "纯计算不保存或扫描；资源级血缘保留实际 A/B 输入，不提供 B 的逐行贡献明细；目标格式须支持实际混合几何",
    ],
    workflow_example={
        'id': 'exclude_protected', 'operator': 'difference',
        'params': {'input_gdf': {'$ref': 'load_source'}, 'gdf_b': {'$ref': 'load_mask'}, 'id_field': 'source_key'},
        'depends_on': ['load_source', 'load_mask'],
    },
)

SIMPLIFY_METADATA = OperatorMetadata(
    name="simplify",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_PREPARATION,
    description="简化几何",
    brief_description="简化几何对象的顶点数量,减小数据量,常用于地图制图和性能优化",
    execution_modes=["workflow"],
    effects=["read"],

    overview="对已知投影CRS的有效非空有限二维源几何逐行简化，容差为原单位的有限非负实数；默认保持单个几何拓扑，显式关闭后塌缩为空仍保行，不自动修复、重投影或维持跨要素共享边。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="有效非空有限二维的投影源图层",
            notes="唯一实际活动几何与列名；已知投影CRS，空批仍验证全部参数"
        ),
        OperatorParam(
            name="tolerance",
            type="param",
            data_type="float",
            required=True,
            description="简化容差,单位与坐标系一致",
            notes="有限非负实数，零合法且允许去除严格共线顶点；容差沿源CRS的坐标单位，投影坐标单位不固定为米，不自动换算单位或重投影"
        ),
        OperatorParam(
            name="preserve_topology",
            type="param",
            data_type="bool",
            required=False,
            default=True,
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
        "容差过大会明显改变形状；零容差也允许移除严格共线的冗余顶点，不承诺原始WKB不变",
        "未知或地理CRS、源缺失/空/无效/Z/M/非有限几何拒绝；先显式整理、修复或重投影",
        "preserve_topology=True仅保持单个几何拓扑，不保证邻接面无缝隙或重叠",
        "保留原属性、类型、索引/行序及CRS；False允许空或无效结果，空结果仍保行，不自动修复",
        "preserve_topology严格为布尔值且默认True，不转换整数、字符串或空值；输入不变"
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


REPRESENTATIVE_POINT_METADATA = OperatorMetadata(
    name="representative_point", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="面内点", brief_description="逐源在最大净面积面部件内生成严格内点，失败不挪点或换部件",
    execution_modes=["workflow"], effects=["read"],
    overview="对已知投影CRS的有效非空有限二维Polygon/MultiPolygon，逐源选择最大净面积部件，精确等面积按原部件顺序决胜；原生求点后严格验证位于选中部件及原源内部。边界、孔洞或面外结果整批计算失败。",
    params=[OperatorParam(name="input_gdf",type="input",data_type="GeoDataFrame",required=True,
        description="有效非空有限二维投影面图层",notes="仅Polygon/MultiPolygon、唯一实际活动几何与列名；已知投影及匹配有限正水平单位，空批仍验准入")],
    use_cases=["在多部件地块的最大净面积部件内生成一个点", "为带孔或凹面生成严格面内位置"],
    notes=["每源一条Point，保原属性类型/null、索引/顺序、实际几何列及CRS；源不变，不跨源汇总，不加部件编号字段",
        "净面积扣除孔洞，不是最宽截段或最大外包矩形；精确相等选择原始顺序第一部件，无epsilon或最小面积阈值",
        "点须严格位于选中部件和原源内，不含外环、内环边界或孔内；不得用covers/intersects放宽",
        "极窄面或原生精度限制可能失败，不自动微移、吸附、换次优、改精度网格或替换质心",
        "不承诺最远边界/最佳标注或最大内切圆中心，不固定坐标或跨GEOS版本WKB恒等",
        "不自动修复或投影；Point/线/GeometryCollection拒绝，仅登记representative_point，无point_on_surface别名"],
    workflow_example={'id':'inside','operator':'representative_point','params':{'input_gdf':{'$ref':'load_projected_polygons'}},'depends_on':['load_projected_polygons']},
)


CONVEX_HULL_METADATA = OperatorMetadata(
    name="convex_hull", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="凸包", brief_description="逐源生成凸包，合法退化保留点或线，不跨行汇总",
    execution_modes=["workflow"], effects=["read"],
    overview="在已知投影CRS下，逐源包含全部部件坐标的最小凸几何：同位点为Point、精确共线为LineString、非共线为凸Polygon。多部件及GeometryCollection每行共同参与，不将不同源点行自动汇总成面。",
    params=[OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True,
        description="有效非空有限二维投影源图层", notes="唯一实际活动几何和列名；已知投影及匹配有限正水平单位，空批仍验证准入")],
    use_cases=["概括单个多部件要素的凸范围", "对上游显式构造的MultiPoint逐源计算凸包"],
    notes=["保原行数、索引/顺序、属性类型/null、实际几何列及CRS；仅替换几何，输入不变",
        "合法重复坐标不改变源；无效零面积面、零长线及缺失/空/Z/M/非有限源拒绝，不自动修复",
        "凹陷、孔洞及部件间空隙可被覆盖，不代表精确源边界；不自动融合、投影或添加面积字段",
        "沿投影原单位，不加epsilon、缓冲或舍入压维；不承诺跨GEOS重新计算WKB相同"],
    workflow_example={'id':'hull','operator':'convex_hull','params':{'input_gdf':{'$ref':'load_multipoints'}},'depends_on':['load_multipoints']},
)


MINIMUM_ROTATED_RECTANGLE_METADATA = OperatorMetadata(
    name="minimum_rotated_rectangle", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="最小外接矩形", brief_description="逐源生成可旋转的最小面积外接矩形，合法退化保留点或线",
    execution_modes=["workflow"], effects=["read"],
    overview="在已知投影CRS下，逐源包含全部部件坐标并允许旋转，以面积最小为目标；同位点为Point、精确共线为LineString、非共线为矩形Polygon。不同于轴对齐envelope，不跨行汇总。",
    params=[OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True,
        description="有效非空有限二维投影源图层", notes="唯一实际活动几何和列名；已知投影及匹配有限正水平单位，空批仍验证准入")],
    use_cases=["概括单个旋转建筑的外接范围", "逐多部件要素生成紧凑的矩形范围"],
    notes=["保原行数、索引/顺序、全部属性类型/null、实际几何列和CRS，输入不变；不新增角度或宽高字段",
        "合法点或共线退化直接保留，不扩宽成面；缺失/空/无效/Z/M/非有限源拒绝，不自动修复",
        "以最小面积而非最小周长为目标；等面积最优方向可能不唯一，不固定环起点或顶点顺序",
        "多部件/GeometryCollection每行合算，不跨行融合；不自动重投影、舍入或设置精度网格"],
    workflow_example={'id':'rectangle','operator':'minimum_rotated_rectangle','params':{'input_gdf':{'$ref':'load_shapes'}},'depends_on':['load_shapes']},
)


EXTRACT_VERTICES_METADATA = OperatorMetadata(
    name="extract_vertices", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
    description="顶点提取", brief_description="逐原始坐标位置输出Point，保留闭环首尾、重复坐标、全部来源与属性",
    execution_modes=["workflow"], effects=["read"],
    overview="按源行、原部件、外环后内环及原顶点顺序提取，不计算新交点或网络度数；闭环首尾和同位坐标分别保留，追加本次成果内唯一vertex_id。",
    params=[OperatorParam(name="input_gdf",type="input",data_type="GeoDataFrame",required=True,description="有效非空有限二维源图层",notes="Point/LineString/Polygon及对应Multi，可跨行混合；实际活动几何，拒绝GeometryCollection、NULL/EMPTY、空部件/环、Z/M；允许空表"),
            OperatorParam(name="id_field",type="param",data_type="str",required=True,description="唯一非空来源标识字段",notes="显式非几何标量列，保留来源类型，不推断索引或FID")],
    output_ports=[OutputPort(name="result",type="geodataframe",description="全部源属性及Point、source_id/part_no/ring_no/vertex_no/vertex_id",is_default=True)],
    use_cases=["逐原始顶点检查线和面边界", "提取面外环与孔洞点并保留原顺序", "按本次vertex_id继续点分析而不丢来源"],
    notes=["已知二维地理或投影CRS，保原XY/单位，不投影/交换轴；源不变，重建RangeIndex", "part_no每源1起始；ring_no点/线0、面外环1/内环2起；vertex_no每线/环1起始，点为1", "vertex_id按本次成果顺序1至N，仅本次结果内唯一，不是永久资源身份", "闭环末尾重复点独立编号；连续重复坐标、不同部件同位点均保留，不排序/去重/修复", "source_id/part_no/ring_no/vertex_no/vertex_id重名须上游手动改名；空表也拒绝，不覆盖", "下游保存沿现有save/格式能力；不新增X/Y字段或容量承诺"],
    workflow_example={'id':'vertices','operator':'extract_vertices','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_id'},'depends_on':['load']},
)


SPLIT_MULTIPART_METADATA = OperatorMetadata(
    name="split_multipart", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
    description="多部拆分", brief_description="按原始部件顺序拆成单部件，完整保留来源属性及部件编号",
    execution_modes=["workflow"], effects=["read"],
    overview="单部件保留一行，Multi点/线/面逐原始部件输出；面孔洞随面保留。原列和来源类型不变，追加source_id/part_no，不去重、融合、修复或递归展开集合。",
    params=[OperatorParam(name="input_gdf",type="input",data_type="GeoDataFrame",required=True,description="有效非空有限二维源图层",notes="实际活动几何，Point/LineString/Polygon及对应Multi，可跨行混合；拒绝GeometryCollection、NULL/EMPTY、空部件、Z/M；允许空表"),
            OperatorParam(name="id_field",type="param",data_type="str",required=True,description="唯一非空来源标识字段",notes="显式非几何标量列，保留类型；不推断索引/FID")],
    output_ports=[OutputPort(name="result",type="geodataframe",description="完整源属性及单部件几何、原类型source_id和每源1起始int64 part_no",is_default=True)],
    use_cases=["多面逐面加工并保留孔洞和原属性", "多线分部件保存而不打断交点", "重复多点按原始位置编号追溯"],
    notes=["已知二维地理或投影CRS，不投影、换单位或交换XY；无CRS/地心/三维/复合拒绝", "按源行/原部件顺序输出，重建RangeIndex；原坐标、方向、重复坐标和部件保留，源不变", "source_id/part_no重名须上游手动改名，空表也拒绝，不覆盖或自动改名", "NULL/EMPTY/GeometryCollection整批拒绝，不静默删行；0行空表仍验证结构/来源/CRS", "混合几何或多几何列保存由现有save与目标格式裁决，不扩格式能力；无跨文件事务或固定容量承诺"],
    workflow_example={'id':'parts','operator':'split_multipart','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_id'},'depends_on':['load']},
)


DISSOLVE_METADATA = OperatorMetadata(
    name="dissolve", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_PREPARATION,
    description="融合几何", brief_description="按显式字段分组求面并集，输出融合成果和完整来源明细",
    execution_modes=["workflow"], effects=["read"],
    overview="每组完整面并集一条，空值保留成组；result仅保分组字段及fusion_id/member_count，members保留全部来源属性和原始几何。不默认取首值或数值聚合。",
    params=[OperatorParam(name="input_gdf",type="input",data_type="GeoDataFrame",required=True,description="有效有限二维投影面图层",notes="面或多面，允许空批，真实活动几何"),OperatorParam(name="id_field",type="param",data_type="str",required=True,description="唯一非空来源标识字段"),OperatorParam(name="group_fields",type="param",data_type="list[str]",required=True,description="显式分组字段列表",notes="[]表示全局；只接受无重复的非几何标量列，保留空值组")],
    output_ports=[OutputPort(name="result",type="geodataframe",description="每组完整融合面及分组字段、fusion_id/member_count",is_default=True),OutputPort(name="members",type="geodataframe",description="原始属性与几何及fusion_id/source_id，一源一条",is_default=False)],
    use_cases=["按行政区融合地块并保留来源", "全局融合缓冲面并保留成员明细"],
    notes=["组内允许重叠、包含和重复面，不跨组融合；每组多部件不拆行", "固定输出列重名须上游手动改名；不自动修复、吸附、重投影或舍入", "纯计算不保存扫描，双端口分别显式接save；不承诺容量"],
    workflow_example={'id':'fuse','operator':'dissolve','params':{'input_gdf':{'$ref':'load'},'id_field':'parcel_id','group_fields':[]},'depends_on':['load']},
)


ENVELOPE_METADATA = OperatorMetadata(
    name="envelope", type=OperatorType.SPATIAL, category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="外包矩形", brief_description="逐源取轴对齐范围，同位输出点、单轴跨度输出线、双轴跨度输出面",
    execution_modes=["workflow"], effects=["read"],
    overview="逐源包含全部部件及集合的XY极值，不跨行汇总、不旋转；双轴同位为Point、单轴同位为水平/垂直LineString，两个轴均有跨度为Polygon，斜向共线仍是面。",
    params=[OperatorParam(name="input_gdf", type="input", data_type="GeoDataFrame", required=True,
        description="有效非空有限二维投影几何", notes="唯一列名及实际活动几何；已知投影及匹配有限正水平线性单位；空批仍验结构和CRS")],
    use_cases=["逐源计算空间检索候选范围", "对多部件整体生成轴对齐外包几何"],
    notes=["仅按原始XY极值精确相等分型，不按面积0、epsilon或舍入判断退化；微小双轴跨度仍为Polygon",
        "合法Point或同位MultiPoint保留Point；水平/垂直线保LineString；无效零长线、零面积面不能借退化规则放行",
        "保属性原名/类型/null、原索引/顺序、实际几何列及CRS，逐源一行；不加来源ID或宽高/面积字段",
        "不扩宽、不缓冲、不自动修复或投影；仅已知投影CRS与有效非空有限XY，拒绝Z/M",
        "凹陷、孔洞和部件间空隙可能被覆盖；不等于精确源边界或可旋转的minimum_rotated_rectangle",
        "范围或结果故障整批计算失败，无兼容路径；混合点线面保存由save校验目标能力，不强转面或删行"],
    workflow_example={'id':'extent','operator':'envelope','params':{'input_gdf':{'$ref':'source'}},'depends_on':['source']},
)


OPERATORS = dict([
    register_operator(BUFFER_METADATA, buffer),
    register_operator(INTERSECTION_METADATA, intersection),
    register_operator(UNION_METADATA, union),
    register_operator(CENTROID_METADATA, centroid),
    register_operator(REPRESENTATIVE_POINT_METADATA, representative_point),
    register_operator(DIFFERENCE_METADATA, difference),
    register_operator(SIMPLIFY_METADATA, simplify),
    register_operator(CONVEX_HULL_METADATA, convex_hull),
    register_operator(MINIMUM_ROTATED_RECTANGLE_METADATA, minimum_rotated_rectangle),
    register_operator(ENVELOPE_METADATA, envelope),
    register_operator(DISSOLVE_METADATA, dissolve),
    register_operator(SPLIT_MULTIPART_METADATA, split_multipart),
    register_operator(EXTRACT_VERTICES_METADATA, extract_vertices),
    register_operator(POLYGONIZE_METADATA, polygonize),
    register_operator(CHECK_GEOMETRY_METADATA, check_geometry),
    register_operator(REPAIR_GEOMETRY_METADATA, repair_geometry),
    register_operator(SNAP_GEOMETRY_METADATA, snap_geometry),
    register_operator(SNAP_LINE_ENDPOINTS_METADATA, snap_line_endpoints),
    register_operator(EXTEND_LINE_ENDPOINTS_METADATA, extend_line_endpoints),
    register_operator(TRIM_LINE_ENDPOINTS_METADATA, trim_line_endpoints),
    register_operator(CHECK_POLYGON_OVERLAP_METADATA, check_polygon_overlap),
    register_operator(CHECK_POLYGON_GAP_METADATA, check_polygon_gap),
    register_operator(CHECK_DANGLING_POINTS_METADATA, check_dangling_points),
    register_operator(CHECK_LINE_PROXIMITY_METADATA, check_line_proximity),
    register_operator(CHECK_LINE_OVERLAP_METADATA, check_line_overlap),
    register_operator(CHECK_LINE_SELF_OVERLAP_METADATA, check_line_self_overlap),
    register_operator(CHECK_LINE_INTERSECTION_METADATA, check_line_intersection),
    register_operator(CHECK_LINE_SELF_INTERSECTION_METADATA, check_line_self_intersection),
    register_operator(SPLIT_LINE_INTERSECTIONS_METADATA, split_line_intersections),
    register_operator(EXTRACT_POLYGON_RINGS_METADATA, extract_polygon_rings),
    register_operator(GROUP_LINE_COMPONENTS_METADATA, group_line_components),
    register_operator(MERGE_LINE_CHAINS_METADATA, merge_line_chains),
    register_operator(EXTRACT_LINE_NODES_METADATA, extract_line_nodes),
    register_operator(NORMALIZE_LINE_NETWORK_METADATA, normalize_line_network),
])
