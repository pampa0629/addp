from __future__ import annotations

import json
import math
from dataclasses import dataclass
from io import BytesIO
from typing import Any

import numpy as np
from PIL import Image

from runtime.gdal_env import gdal_config_env, join_gdal_path
from runtime.tile_math import clamp_tile_size, int_param, is_finite_bounds, web_mercator_tile_bounds


DisplayRange = tuple[float, float]
DisplayRanges = list[DisplayRange | None]
_DISPLAY_RANGE_CACHE: dict[str, DisplayRanges] = {}
_DISPLAY_RANGE_CACHE_MAX_SIZE = 64


@dataclass
class RenderedTile:
    data: bytes
    content_type: str
    source: str


class RasterMosaicRuntimeError(Exception):
    def __init__(self, code: str, message: str, status_code: int = 400):
        super().__init__(message)
        self.code = code
        self.status_code = status_code


def render_mosaic_tile(payload: dict[str, Any]) -> RenderedTile:
    dataset = _dict(payload.get("dataset"), "dataset")
    tile = _dict(payload.get("tile"), "tile")
    render = _dict(payload.get("render") or {}, "render")

    dataset_root = str(dataset.get("dataset_root_uri") or "").strip()
    overview_ref = str(dataset.get("overview_ref") or "").strip()
    index_ref = str(dataset.get("index_ref") or "").strip()
    if not dataset_root:
        raise RasterMosaicRuntimeError("invalid_request", "dataset.dataset_root_uri is required")
    if not overview_ref:
        raise RasterMosaicRuntimeError("overview_not_found", "dataset.overview_ref is required")

    z = int_param(tile.get("z"), "tile.z", minimum=0)
    x = int_param(tile.get("x"), "tile.x", minimum=0)
    y = int_param(tile.get("y"), "tile.y", minimum=0)
    tile_size = clamp_tile_size(int_param(tile.get("tile_size"), "tile.tile_size", minimum=1, default=256))
    bounds = web_mercator_tile_bounds(z, x, y)
    if not is_finite_bounds(bounds):
        raise RasterMosaicRuntimeError("invalid_tile", "tile bounds are invalid")

    output_format = _output_format(render.get("format"))
    resampling = str(render.get("resampling") or "bilinear").strip() or "bilinear"
    overview_max_upsample = _positive_float(render.get("overview_max_upsample"), default=1.5)
    display_gamma = _positive_float(render.get("gamma"), default=0.6)
    display_min = _optional_float(render.get("display_min"))
    display_max = _optional_float(render.get("display_max"))
    invert = _bool_param(render.get("invert"), default=False)
    max_leaf_sources = int_param(render.get("max_leaf_sources"), "render.max_leaf_sources", minimum=1, default=128)
    overview_uri = join_gdal_path(dataset_root, overview_ref)

    with gdal_config_env(dataset.get("gdal_env")) as gdal:
        ds = None
        try:
            ds = gdal.Open(overview_uri)
            if ds is None:
                raise RasterMosaicRuntimeError("overview_not_found", "overview COG cannot be opened", 404)
            display_ranges = _override_display_ranges(
                _display_ranges_for_overview(overview_uri, ds),
                display_min,
                display_max,
            )
            if not _overview_has_enough_resolution(ds, bounds, tile_size, overview_max_upsample):
                leaf_uris = _intersecting_leaf_uris(gdal, dataset_root, index_ref, bounds, max_leaf_sources)
                if leaf_uris:
                    return _render_tile_from_source(gdal, leaf_uris, bounds, tile_size, resampling, output_format, "leaf", display_ranges, display_gamma, invert, _effective_dataset_projection(ds))
            return _render_tile_from_source(gdal, ds, bounds, tile_size, resampling, output_format, "overview", display_ranges, display_gamma, invert, _effective_dataset_projection(ds))
        finally:
            ds = None


def render_overview_tile(payload: dict[str, Any]) -> RenderedTile:
    return render_mosaic_tile(payload)


def _dict(value: Any, name: str) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise RasterMosaicRuntimeError("invalid_request", f"{name} must be an object")
    return value


def _output_format(value: Any) -> str:
    fmt = str(value or "webp").strip().lower()
    if fmt in ("jpg", "jpeg"):
        return "jpeg"
    if fmt not in ("png", "webp", "jpeg"):
        raise RasterMosaicRuntimeError("invalid_request", "render.format must be png, webp, or jpeg")
    return fmt


def _positive_float(value: Any, default: float) -> float:
    if value is None:
        return default
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return default
    if not math.isfinite(parsed) or parsed <= 0:
        return default
    return parsed


def _optional_float(value: Any) -> float | None:
    if value is None:
        return None
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return None
    if not math.isfinite(parsed):
        return None
    return parsed


def _bool_param(value: Any, default: bool = False) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    text = str(value).strip().lower()
    if text in ("1", "true", "yes", "y", "on"):
        return True
    if text in ("0", "false", "no", "n", "off"):
        return False
    return default


def _render_tile_from_source(
    gdal,
    source: Any,
    bounds: tuple[float, float, float, float],
    tile_size: int,
    resampling: str,
    output_format: str,
    tile_source: str,
    display_ranges: DisplayRanges | None,
    display_gamma: float,
    invert: bool,
    source_srs: str = "",
) -> RenderedTile:
    warp_options = {
        "format": "MEM",
        "dstSRS": "EPSG:3857",
        "outputBounds": bounds,
        "outputBoundsSRS": "EPSG:3857",
        "width": tile_size,
        "height": tile_size,
        "resampleAlg": resampling,
        "dstAlpha": True,
    }
    if source_srs:
        warp_options["srcSRS"] = source_srs
    warped = gdal.Warp("", source, **warp_options)
    if warped is None:
        raise RasterMosaicRuntimeError("render_failed", "GDAL failed to warp raster mosaic tile", 500)
    array = warped.ReadAsArray()
    if array is None:
        raise RasterMosaicRuntimeError("render_failed", "GDAL returned empty tile data", 500)
    image = _array_to_image(array, warped, display_ranges, display_gamma, invert)
    return _encode_image(image, output_format, tile_source)


def _tile_resolution_3857(bounds: tuple[float, float, float, float], tile_size: int) -> float:
    if tile_size <= 0:
        return 0
    width = float(bounds[2]) - float(bounds[0])
    if not math.isfinite(width) or width <= 0:
        return 0
    return width / float(tile_size)


def _overview_has_enough_resolution(dataset, bounds: tuple[float, float, float, float], tile_size: int, max_upsample: float) -> bool:
    tile_resolution = _tile_resolution_3857(bounds, tile_size)
    overview_resolution = _dataset_resolution_3857(dataset, bounds)
    return _resolution_is_sufficient(overview_resolution, tile_resolution, max_upsample)


def _resolution_is_sufficient(source_resolution: float, tile_resolution: float, max_upsample: float) -> bool:
    if tile_resolution <= 0 or source_resolution <= 0:
        return True
    return (source_resolution / tile_resolution) <= max_upsample


def _dataset_resolution_3857(dataset, bounds: tuple[float, float, float, float]) -> float:
    local_resolution = _dataset_local_resolution_3857(dataset, bounds)
    if local_resolution > 0:
        return local_resolution
    return _dataset_extent_resolution_3857(dataset)


def _dataset_local_resolution_3857(dataset, bounds: tuple[float, float, float, float]) -> float:
    transform = _dataset_transform(dataset)
    if transform is None:
        return 0
    projection = _effective_dataset_projection(dataset)
    to_source = _coordinate_transform("EPSG:3857", projection)
    to_3857 = _coordinate_transform(projection, "EPSG:3857")
    if to_source is None or to_3857 is None:
        return 0
    center_3857 = ((bounds[0] + bounds[2]) / 2.0, (bounds[1] + bounds[3]) / 2.0)
    center_source = _transform_point(to_source, center_3857[0], center_3857[1])
    if center_source is None:
        return 0
    x_neighbor = (center_source[0] + float(transform[1]), center_source[1] + float(transform[4]))
    y_neighbor = (center_source[0] + float(transform[2]), center_source[1] + float(transform[5]))
    center_back = _transform_point(to_3857, center_source[0], center_source[1])
    x_back = _transform_point(to_3857, x_neighbor[0], x_neighbor[1])
    y_back = _transform_point(to_3857, y_neighbor[0], y_neighbor[1])
    if center_back is None or x_back is None or y_back is None:
        return 0
    distances = [_distance(center_back, x_back), _distance(center_back, y_back)]
    distances = [value for value in distances if math.isfinite(value) and value > 0]
    if not distances:
        return 0
    return max(distances)


def _dataset_extent_resolution_3857(dataset) -> float:
    extent = _dataset_extent_3857(dataset)
    if extent is None:
        return 0
    width = max(0.0, extent[2] - extent[0])
    height = max(0.0, extent[3] - extent[1])
    raster_width = float(getattr(dataset, "RasterXSize", 0) or 0)
    raster_height = float(getattr(dataset, "RasterYSize", 0) or 0)
    resolutions = []
    if width > 0 and raster_width > 0:
        resolutions.append(width / raster_width)
    if height > 0 and raster_height > 0:
        resolutions.append(height / raster_height)
    return max(resolutions) if resolutions else 0


def _dataset_extent_3857(dataset) -> tuple[float, float, float, float] | None:
    transform = _dataset_transform(dataset)
    if transform is None:
        return None
    width = float(getattr(dataset, "RasterXSize", 0) or 0)
    height = float(getattr(dataset, "RasterYSize", 0) or 0)
    if width <= 0 or height <= 0:
        return None
    corners = [
        _pixel_to_world(transform, 0, 0),
        _pixel_to_world(transform, width, 0),
        _pixel_to_world(transform, width, height),
        _pixel_to_world(transform, 0, height),
    ]
    return _extent_from_points_3857(corners, _effective_dataset_projection(dataset))


def _dataset_transform(dataset) -> tuple[float, float, float, float, float, float] | None:
    try:
        transform = dataset.GetGeoTransform()
    except Exception:
        return None
    if not transform or len(transform) != 6:
        return None
    return tuple(float(value) for value in transform)


def _dataset_projection(dataset) -> str:
    try:
        return str(dataset.GetProjection() or dataset.GetProjectionRef() or "").strip()
    except Exception:
        return ""


def _effective_dataset_projection(dataset) -> str:
    projection = _dataset_projection(dataset)
    if projection:
        return projection
    extent = _dataset_extent(dataset)
    if extent is not None and _looks_like_geographic_extent(extent):
        return "EPSG:4326"
    return ""


def _dataset_extent(dataset) -> tuple[float, float, float, float] | None:
    transform = _dataset_transform(dataset)
    if transform is None:
        return None
    width = float(getattr(dataset, "RasterXSize", 0) or 0)
    height = float(getattr(dataset, "RasterYSize", 0) or 0)
    if width <= 0 or height <= 0:
        return None
    corners = [
        _pixel_to_world(transform, 0, 0),
        _pixel_to_world(transform, width, 0),
        _pixel_to_world(transform, width, height),
        _pixel_to_world(transform, 0, height),
    ]
    xs = [point[0] for point in corners]
    ys = [point[1] for point in corners]
    extent = (min(xs), min(ys), max(xs), max(ys))
    if not all(math.isfinite(value) for value in extent):
        return None
    return extent


def _looks_like_geographic_extent(extent: tuple[float, float, float, float]) -> bool:
    minx, miny, maxx, maxy = extent
    return -180.0 <= minx < maxx <= 180.0 and -90.0 <= miny < maxy <= 90.0


def _pixel_to_world(transform: tuple[float, float, float, float, float, float], px: float, py: float) -> tuple[float, float]:
    x = transform[0] + px * transform[1] + py * transform[2]
    y = transform[3] + px * transform[4] + py * transform[5]
    return (x, y)


def _coordinate_transform(source_crs: str, target_crs: str):
    source_crs = str(source_crs or "").strip()
    target_crs = str(target_crs or "").strip()
    if not source_crs or not target_crs:
        return None
    try:
        from osgeo import osr

        source = osr.SpatialReference()
        target = osr.SpatialReference()
        if source_crs.upper().startswith("EPSG:"):
            source.ImportFromEPSG(int(source_crs.split(":", 1)[1]))
        else:
            source.ImportFromWkt(source_crs)
        if target_crs.upper().startswith("EPSG:"):
            target.ImportFromEPSG(int(target_crs.split(":", 1)[1]))
        else:
            target.ImportFromWkt(target_crs)
        if hasattr(source, "SetAxisMappingStrategy"):
            source.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
            target.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        return osr.CoordinateTransformation(source, target)
    except Exception:
        return None


def _transform_point(transform, x: float, y: float) -> tuple[float, float] | None:
    try:
        tx, ty, _ = transform.TransformPoint(float(x), float(y))
    except Exception:
        return None
    if not math.isfinite(tx) or not math.isfinite(ty):
        return None
    return (float(tx), float(ty))


def _distance(left: tuple[float, float], right: tuple[float, float]) -> float:
    return math.hypot(float(right[0]) - float(left[0]), float(right[1]) - float(left[1]))


def _intersecting_leaf_uris(gdal, dataset_root: str, index_ref: str, tile_bounds_3857: tuple[float, float, float, float], max_leaf_sources: int) -> list[str]:
    if not index_ref:
        return []
    index = _read_source_index(gdal, join_gdal_path(dataset_root, index_ref))
    leaves = index.get("leaves") if isinstance(index, dict) else None
    if not isinstance(leaves, list):
        return []
    uris: list[str] = []
    for leaf in leaves:
        if not isinstance(leaf, dict):
            continue
        leaf_extent = _leaf_extent_3857(leaf)
        if leaf_extent is None or not _extent_intersects(leaf_extent, tile_bounds_3857):
            continue
        leaf_ref = str(leaf.get("leaf_ref") or leaf.get("path") or "").strip()
        leaf_uri = str(leaf.get("leaf_uri") or "").strip()
        if leaf_uri:
            uris.append(leaf_uri)
        elif leaf_ref:
            uris.append(join_gdal_path(dataset_root, leaf_ref))
        if len(uris) >= max_leaf_sources:
            break
    return uris


def _read_source_index(gdal, index_uri: str) -> dict[str, Any]:
    if not index_uri:
        return {}
    handle = None
    try:
        handle = gdal.VSIFOpenL(index_uri, "rb")
        if handle is None:
            return {}
        payload = gdal.VSIFReadL(1, 32 * 1024 * 1024, handle)
        if isinstance(payload, str):
            payload = payload.encode("utf-8")
        if not payload:
            return {}
        return json.loads(bytes(payload).decode("utf-8"))
    except Exception:
        return {}
    finally:
        if handle is not None:
            try:
                gdal.VSIFCloseL(handle)
            except Exception:
                pass


def _leaf_extent_3857(leaf: dict[str, Any]) -> tuple[float, float, float, float] | None:
    extent = _float_extent(leaf.get("extent"))
    if extent is None:
        return None
    source_crs = str(leaf.get("source_crs") or leaf.get("crs") or "").strip()
    if not source_crs and _looks_like_geographic_extent(extent):
        source_crs = "EPSG:4326"
    return _extent_from_points_3857(
        [(extent[0], extent[1]), (extent[0], extent[3]), (extent[2], extent[1]), (extent[2], extent[3])],
        source_crs,
    )


def _extent_from_points_3857(points: list[tuple[float, float]], source_crs: str) -> tuple[float, float, float, float] | None:
    transform = _coordinate_transform(source_crs, "EPSG:3857")
    if transform is None:
        return None
    transformed = []
    for x, y in points:
        point = _transform_point(transform, x, y)
        if point is not None:
            transformed.append(point)
    if not transformed:
        return None
    xs = [point[0] for point in transformed]
    ys = [point[1] for point in transformed]
    return (min(xs), min(ys), max(xs), max(ys))


def _float_extent(value: Any) -> tuple[float, float, float, float] | None:
    if not isinstance(value, (list, tuple)) or len(value) != 4:
        return None
    try:
        extent = tuple(float(item) for item in value)
    except (TypeError, ValueError):
        return None
    if not all(math.isfinite(item) for item in extent):
        return None
    minx, miny, maxx, maxy = extent
    if maxx <= minx or maxy <= miny:
        return None
    return (minx, miny, maxx, maxy)


def _extent_intersects(left: tuple[float, float, float, float], right: tuple[float, float, float, float]) -> bool:
    return left[0] < right[2] and left[2] > right[0] and left[1] < right[3] and left[3] > right[1]


def _display_ranges_for_overview(overview_uri: str, dataset) -> DisplayRanges:
    cache_key = str(overview_uri or "").strip()
    if cache_key and cache_key in _DISPLAY_RANGE_CACHE:
        return _DISPLAY_RANGE_CACHE[cache_key]
    ranges = _display_ranges_from_dataset(dataset)
    if cache_key:
        if len(_DISPLAY_RANGE_CACHE) >= _DISPLAY_RANGE_CACHE_MAX_SIZE:
            _DISPLAY_RANGE_CACHE.pop(next(iter(_DISPLAY_RANGE_CACHE)))
        _DISPLAY_RANGE_CACHE[cache_key] = ranges
    return ranges


def _display_ranges_from_dataset(dataset, max_pixels: int = 65536) -> DisplayRanges:
    band_count = int(getattr(dataset, "RasterCount", 0) or 0)
    width = int(getattr(dataset, "RasterXSize", 0) or 0)
    height = int(getattr(dataset, "RasterYSize", 0) or 0)
    if band_count <= 0 or width <= 0 or height <= 0:
        return []
    sample_width, sample_height = _sample_size(width, height, max_pixels)
    ranges: DisplayRanges = []
    for band_index in range(1, band_count + 1):
        try:
            band = dataset.GetRasterBand(band_index)
            values = band.ReadAsArray(buf_xsize=sample_width, buf_ysize=sample_height) if band is not None else None
        except Exception:
            values = None
        ranges.append(_display_range_from_samples(values, _band_nodata(dataset, band_index)))
    return ranges


def _override_display_ranges(display_ranges: DisplayRanges, display_min: float | None, display_max: float | None) -> DisplayRanges:
    if display_min is None or display_max is None or display_max <= display_min:
        return display_ranges
    if not display_ranges:
        return [(display_min, display_max)]
    return [(display_min, display_max) if item is not None else item for item in display_ranges]


def _sample_size(width: int, height: int, max_pixels: int) -> tuple[int, int]:
    source_width = max(1, int(width or 1))
    source_height = max(1, int(height or 1))
    source_pixels = source_width * source_height
    if source_pixels <= max_pixels:
        return (source_width, source_height)
    ratio = math.sqrt(float(max_pixels) / float(source_pixels))
    return (max(1, round(source_width * ratio)), max(1, round(source_height * ratio)))


def _display_range_from_samples(values: Any, nodata: float | None = None) -> DisplayRange | None:
    if values is None:
        return None
    arr = np.asarray(values, dtype=np.float64)
    valid = np.isfinite(arr)
    if nodata is not None and math.isfinite(float(nodata)):
        valid &= arr != float(nodata)
    samples = arr[valid]
    if samples.size == 0:
        return None
    min_value = float(np.percentile(samples, 2))
    max_value = float(np.percentile(samples, 98))
    if max_value <= min_value:
        min_value = float(np.min(samples))
        max_value = float(np.max(samples))
    if not math.isfinite(min_value) or not math.isfinite(max_value) or max_value <= min_value:
        return None
    return (min_value, max_value)


def _array_to_image(
    array: np.ndarray,
    dataset,
    display_ranges: DisplayRanges | None = None,
    display_gamma: float = 1.0,
    invert: bool = False,
) -> Image.Image:
    arr = np.asarray(array)
    if arr.ndim == 2:
        gray = _scale_to_uint8(arr, _display_range_for_band(display_ranges, 0), display_gamma, invert)
        alpha = _alpha_from_dataset(arr, dataset)
        return Image.fromarray(np.dstack([gray, gray, gray, alpha]), "RGBA")

    if arr.ndim != 3:
        raise RasterMosaicRuntimeError("render_failed", "unexpected raster tile array shape", 500)

    bands, height, width = arr.shape
    if bands >= 4:
        rgb = np.stack([
            _scale_to_uint8(arr[0], _display_range_for_band(display_ranges, 0), display_gamma, invert),
            _scale_to_uint8(arr[1], _display_range_for_band(display_ranges, 1), display_gamma, invert),
            _scale_to_uint8(arr[2], _display_range_for_band(display_ranges, 2), display_gamma, invert),
        ], axis=-1)
        alpha = _scale_alpha(arr[3])
        return Image.fromarray(np.dstack([rgb, alpha]), "RGBA")
    if bands >= 3:
        rgb = np.stack([
            _scale_to_uint8(arr[0], _display_range_for_band(display_ranges, 0), display_gamma, invert),
            _scale_to_uint8(arr[1], _display_range_for_band(display_ranges, 1), display_gamma, invert),
            _scale_to_uint8(arr[2], _display_range_for_band(display_ranges, 2), display_gamma, invert),
        ], axis=-1)
        alpha = _alpha_from_dataset(arr[:3], dataset)
        return Image.fromarray(np.dstack([rgb, alpha]), "RGBA")
    if bands == 2:
        gray = _scale_to_uint8(arr[0], _display_range_for_band(display_ranges, 0), display_gamma, invert)
        alpha = _scale_alpha(arr[1])
        return Image.fromarray(np.dstack([gray, gray, gray, alpha]), "RGBA")
    if bands == 1:
        gray = _scale_to_uint8(arr[0], _display_range_for_band(display_ranges, 0), display_gamma, invert)
        alpha = _alpha_from_dataset(arr[0], dataset)
        return Image.fromarray(np.dstack([gray, gray, gray, alpha]), "RGBA")
    return Image.fromarray(np.zeros((height, width, 4), dtype=np.uint8), "RGBA")


def _display_range_for_band(display_ranges: DisplayRanges | None, band_index: int) -> DisplayRange | None:
    if not display_ranges or band_index < 0 or band_index >= len(display_ranges):
        return None
    return display_ranges[band_index]


def _scale_to_uint8(
    values: np.ndarray,
    display_range: DisplayRange | None = None,
    display_gamma: float = 1.0,
    invert: bool = False,
) -> np.ndarray:
    arr = np.asarray(values)
    gamma = float(display_gamma) if math.isfinite(float(display_gamma or 1.0)) and float(display_gamma or 1.0) > 0 else 1.0
    if arr.dtype == np.uint8 and display_range is None and gamma == 1.0:
        return arr
    if display_range is not None:
        min_value, max_value = display_range
    else:
        valid = np.isfinite(arr)
        if not np.any(valid):
            return np.zeros(arr.shape, dtype=np.uint8)
        min_value = float(np.nanmin(arr[valid]))
        max_value = float(np.nanmax(arr[valid]))
    if max_value <= min_value:
        return np.zeros(arr.shape, dtype=np.uint8)
    normalized = np.clip((arr.astype(np.float64) - min_value) / (max_value - min_value), 0, 1)
    if gamma != 1.0:
        normalized = np.power(normalized, gamma)
    if invert:
        normalized = 1.0 - normalized
    return np.clip(normalized * 255.0, 0, 255).astype(np.uint8)


def _scale_alpha(values: np.ndarray) -> np.ndarray:
    arr = np.asarray(values)
    if arr.dtype == np.uint8:
        return arr
    return np.where(arr > 0, 255, 0).astype(np.uint8)


def _alpha_from_dataset(values: np.ndarray, dataset) -> np.ndarray:
    arr = np.asarray(values)
    if arr.ndim == 3:
        sample = arr[0]
        valid = np.ones(sample.shape, dtype=bool)
        for index in range(arr.shape[0]):
            nodata = _band_nodata(dataset, index + 1)
            if nodata is not None:
                valid &= arr[index] != nodata
        return np.where(valid, 255, 0).astype(np.uint8)
    nodata = _band_nodata(dataset, 1)
    if nodata is None:
        return np.full(arr.shape, 255, dtype=np.uint8)
    return np.where(arr != nodata, 255, 0).astype(np.uint8)


def _band_nodata(dataset, index: int) -> float | None:
    try:
        band = dataset.GetRasterBand(index)
        if band is None:
            return None
        return band.GetNoDataValue()
    except Exception:
        return None


def _encode_image(image: Image.Image, output_format: str, tile_source: str) -> RenderedTile:
    buffer = BytesIO()
    if output_format == "png":
        image.save(buffer, format="PNG")
        return RenderedTile(buffer.getvalue(), "image/png", tile_source)
    if output_format == "jpeg":
        image.convert("RGB").save(buffer, format="JPEG", quality=88)
        return RenderedTile(buffer.getvalue(), "image/jpeg", tile_source)
    image.save(buffer, format="WEBP", quality=85)
    return RenderedTile(buffer.getvalue(), "image/webp", tile_source)
