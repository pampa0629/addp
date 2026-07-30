"""
向量瓦片算子模块

Manager 负责把 ADDP locator、engine、源路径和目标对象存储解析为 GDAL
可访问的 access_plan；GeoPython Workflow 只负责读取物理源、生成 MVT、
写入 PMTiles v3 单文件并上报进度。
"""

import json
import hashlib
import logging
import math
import os
import posixpath
import re
import shutil
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from pathlib import Path
from time import perf_counter
from typing import Any, Callable, Dict

from pyproj import Transformer
from pmtiles.tile import Compression, TileType, zxy_to_tileid
from pmtiles.writer import Writer

from .base import (
    OperatorType,
    OperatorMetadata,
    OperatorParam,
    OperatorCategory,
    register_operator,
)
from .raster_operators import (
    _import_gdal,
    _prepare_target_path,
    _ensure_gdal_dir,
    _gdal_config_env,
    _is_gdal_virtual_path,
    _path_parent,
    _progress_reporter,
    _set_path_specific_gdal_options,
)

logger = logging.getLogger(__name__)

DEFAULT_MVT_EXTENT = 8192
DEFAULT_MVT_MAX_SIZE_BYTES = 5_000_000
DEFAULT_MVT_MAX_FEATURES = 1_000_000
DEFAULT_MVT_PUBLISH_CONCURRENCY = 8
DEFAULT_MVT_GDAL_NUM_THREADS = "ALL_CPUS"
MVT_GENERATE_PROGRESS_START = 1.0
MVT_GENERATE_PROGRESS_SPAN = 39.0
MVT_PUBLISH_PROGRESS_START = 40.0
MVT_PUBLISH_PROGRESS_SPAN = 59.0


def _tile_bounds(extent: list[float], zoom: int) -> tuple[int, int, int, int]:
    min_lon, min_lat, max_lon, max_lat = extent
    min_lon = max(-180.0, min(180.0, min_lon))
    max_lon = max(-180.0, min(180.0, max_lon))
    min_lat = max(-85.05112878, min(85.05112878, min_lat))
    max_lat = max(-85.05112878, min(85.05112878, max_lat))

    def lon_to_x(lon: float) -> int:
        return int(math.floor((lon + 180.0) / 360.0 * (1 << zoom)))

    def lat_to_y(lat: float) -> int:
        lat_rad = math.radians(lat)
        return int(math.floor((1.0 - math.log(math.tan(lat_rad) + 1.0 / math.cos(lat_rad)) / math.pi) / 2.0 * (1 << zoom)))

    max_index = (1 << zoom) - 1
    min_x = max(0, min(max_index, lon_to_x(min_lon)))
    max_x = max(0, min(max_index, lon_to_x(max_lon)))
    min_y = max(0, min(max_index, lat_to_y(max_lat)))
    max_y = max(0, min(max_index, lat_to_y(min_lat)))
    if max_x < min_x:
        min_x, max_x = max_x, min_x
    if max_y < min_y:
        min_y, max_y = max_y, min_y
    return min_x, min_y, max_x, max_y


def _normalize_extent(value: Any) -> list[float]:
    if not isinstance(value, list) or len(value) != 4:
        raise ValueError("tile.extent must be [minLon, minLat, maxLon, maxLat]")
    extent = [float(v) for v in value]
    if extent[0] >= extent[2] or extent[1] >= extent[3]:
        raise ValueError("tile.extent is invalid")
    return extent


def _extent_to_wgs84(extent: list[float], extent_srid: int, source_srs: str = "") -> list[float]:
    if extent_srid == 4326:
        return list(extent)
    source_crs = f"EPSG:{extent_srid}" if extent_srid > 0 else str(source_srs or "").strip()
    if not source_crs:
        raise ValueError("tile.extent_srid or tile.source_srs is required for non-WGS84 extent")

    transformer = Transformer.from_crs(source_crs, "EPSG:4326", always_xy=True)
    min_x, min_y, max_x, max_y = extent
    points = [
        transformer.transform(min_x, min_y),
        transformer.transform(min_x, max_y),
        transformer.transform(max_x, min_y),
        transformer.transform(max_x, max_y),
    ]
    lon_values = [float(point[0]) for point in points if math.isfinite(point[0]) and math.isfinite(point[1])]
    lat_values = [float(point[1]) for point in points if math.isfinite(point[0]) and math.isfinite(point[1])]
    if not lon_values or not lat_values:
        raise ValueError("tile.extent cannot be transformed to EPSG:4326")
    return [min(lon_values), min(lat_values), max(lon_values), max(lat_values)]


def _copy_tile_payload(source_tile: Path, target_uri: str, target_env: Dict[str, Any], use_configured_path_options: bool = False) -> int:
    data = source_tile.read_bytes()
    if use_configured_path_options:
        _write_binary_with_configured_path_options(target_uri, data)
    else:
        _write_binary(target_uri, data, target_env)
    return len(data)


def _write_binary(target_uri: str, data: bytes, target_env: Dict[str, Any]) -> None:
    if _is_gdal_virtual_path(target_uri):
        with _gdal_config_env(target_env) as gdal:
            handle = gdal.VSIFOpenL(target_uri, "wb")
            if handle is None:
                raise RuntimeError(f"GDAL cannot open target for writing: {target_uri}")
            try:
                written = gdal.VSIFWriteL(data, 1, len(data), handle)
            finally:
                close_result = gdal.VSIFCloseL(handle)
            if written != len(data):
                raise RuntimeError(f"GDAL wrote incomplete payload to {target_uri}: {written}/{len(data)} bytes")
            if close_result != 0:
                raise RuntimeError(f"GDAL failed to close target after writing: {target_uri}")
        return
    target_path = Path(target_uri)
    target_path.parent.mkdir(parents=True, exist_ok=True)
    target_path.write_bytes(data)


def _write_binary_with_configured_path_options(target_uri: str, data: bytes) -> None:
    if _is_gdal_virtual_path(target_uri):
        gdal = _import_gdal()
        handle = gdal.VSIFOpenL(target_uri, "wb")
        if handle is None:
            raise RuntimeError(f"GDAL cannot open target for writing: {target_uri}")
        try:
            written = gdal.VSIFWriteL(data, 1, len(data), handle)
        finally:
            close_result = gdal.VSIFCloseL(handle)
        if written != len(data):
            raise RuntimeError(f"GDAL wrote incomplete payload to {target_uri}: {written}/{len(data)} bytes")
        if close_result != 0:
            raise RuntimeError(f"GDAL failed to close target after writing: {target_uri}")
        return
    target_path = Path(target_uri)
    target_path.parent.mkdir(parents=True, exist_ok=True)
    target_path.write_bytes(data)


def _positive_int(value: Any, default: int, minimum: int = 1, maximum: int | None = None) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = int(default)
    if parsed < minimum:
        parsed = minimum
    if maximum is not None and parsed > maximum:
        parsed = maximum
    return parsed


def _optional_float(value: Any, default: float | None = None) -> float | None:
    if value is None:
        return default
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return default
    if not math.isfinite(parsed):
        return default
    return parsed


def _ogr_sql_identifier(value: str) -> str:
    return '"' + str(value or "").replace('"', '""') + '"'


def _source_layer_name(source_plan: Dict[str, Any], source_uri: str) -> str:
    metadata = source_plan.get("metadata") if isinstance(source_plan.get("metadata"), dict) else {}
    explicit = str(source_plan.get("layer_name") or metadata.get("layer_name") or "").strip()
    if explicit:
        return explicit
    candidates = [
        source_plan.get("full_name"),
        source_uri,
    ]
    for candidate in candidates:
        text = str(candidate or "").strip().rstrip("/")
        if not text:
            continue
        text = text.split("?", 1)[0]
        name = Path(text).stem
        if name:
            return name
    return "layer"


def _build_quick_view_ogr_sql(source_layer: str, primary_key: str = "") -> str:
    fields = ["OGR_GEOMETRY"]
    primary_key = str(primary_key or "").strip()
    if primary_key:
        fields.append(_ogr_sql_identifier(primary_key))
    return f"SELECT {', '.join(fields)} FROM {_ogr_sql_identifier(source_layer)}"


def _mvt_quality_options(tile: Dict[str, Any], options: Dict[str, Any]) -> Dict[str, Any]:
    extent_units = _positive_int(
        options.get("mvt_extent", tile.get("mvt_extent", DEFAULT_MVT_EXTENT)),
        DEFAULT_MVT_EXTENT,
        minimum=256,
        maximum=65536,
    )
    default_buffer = max(1, int(round(extent_units * 80.0 / 4096.0)))
    buffer_units = _positive_int(
        options.get("mvt_buffer", tile.get("mvt_buffer", default_buffer)),
        default_buffer,
        minimum=0,
        maximum=extent_units,
    )
    return {
        "extent": extent_units,
        "buffer": buffer_units,
        "max_size": _positive_int(
            options.get("max_tile_size_bytes", tile.get("max_tile_size_bytes", DEFAULT_MVT_MAX_SIZE_BYTES)),
            DEFAULT_MVT_MAX_SIZE_BYTES,
            minimum=100,
        ),
        "max_features": _positive_int(
            options.get("max_features", tile.get("max_features", DEFAULT_MVT_MAX_FEATURES)),
            DEFAULT_MVT_MAX_FEATURES,
            minimum=1,
        ),
        "simplification": _optional_float(options.get("simplification", tile.get("simplification")), 0.0),
        "simplification_max_zoom": _optional_float(
            options.get("simplification_max_zoom", tile.get("simplification_max_zoom")),
            0.0,
        ),
        "num_threads": str(options.get("num_threads") or tile.get("num_threads") or DEFAULT_MVT_GDAL_NUM_THREADS).strip(),
        "publish_concurrency": _positive_int(
            options.get("publish_concurrency", tile.get("publish_concurrency", DEFAULT_MVT_PUBLISH_CONCURRENCY)),
            DEFAULT_MVT_PUBLISH_CONCURRENCY,
            minimum=1,
            maximum=64,
        ),
    }


def _publish_tile_jobs(tile_jobs: list[Dict[str, Any]], target_env: Dict[str, Any], publish_concurrency: int) -> list[Dict[str, Any]]:
    if not tile_jobs:
        return []

    def publish(job: Dict[str, Any]) -> Dict[str, Any]:
        size = _copy_tile_payload(Path(job["source"]), str(job["target_uri"]), target_env, use_configured_path_options=True)
        return {"zoom": int(job["zoom"]), "size": int(size)}

    if publish_concurrency <= 1 or len(tile_jobs) == 1:
        return [publish(job) for job in tile_jobs]

    results: list[Dict[str, Any]] = []
    with ThreadPoolExecutor(max_workers=min(publish_concurrency, len(tile_jobs))) as executor:
        futures = [executor.submit(publish, job) for job in tile_jobs]
        for future in as_completed(futures):
            results.append(future.result())
    return results


def _write_json(target_uri: str, payload: Dict[str, Any], target_env: Dict[str, Any]) -> None:
    data = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    _write_binary(target_uri, data, target_env)


def _find_generated_tile(tile_root: Path, z: int, x: int, y: int) -> Path | None:
    candidates = [
        tile_root / str(z) / str(x) / f"{y}.pbf",
        tile_root / str(z) / str(x) / f"{y}.mvt",
        tile_root / str(z) / f"{x}" / f"{y}.pbf",
        tile_root / str(z) / f"{x}" / f"{y}.mvt",
    ]
    for candidate in candidates:
        if candidate.exists() and candidate.is_file():
            return candidate
    for suffix in ("*.pbf", "*.mvt"):
        for candidate in tile_root.glob(f"**/{suffix}"):
            parts = candidate.relative_to(tile_root).parts
            if len(parts) >= 3 and parts[-3] == str(z) and parts[-2] == str(x) and Path(parts[-1]).stem == str(y):
                return candidate
    return None


def _index_generated_tiles(tile_root: Path) -> Dict[tuple[int, int, int], Path]:
    tiles: Dict[tuple[int, int, int], Path] = {}
    if not tile_root.exists():
        return tiles
    for candidate in tile_root.rglob("*"):
        if not candidate.is_file() or candidate.suffix.lower() not in (".pbf", ".mvt"):
            continue
        parts = candidate.relative_to(tile_root).parts
        if len(parts) < 3:
            continue
        try:
            z = int(parts[-3])
            x = int(parts[-2])
            y = int(candidate.stem)
        except ValueError:
            continue
        tiles.setdefault((z, x, y), candidate)
    return tiles


def _weighted_progress(start: float, span: float, percent: float) -> float:
    percent = max(0.0, min(100.0, float(percent)))
    return max(0.0, min(100.0, start + span * percent / 100.0))


def _extract_gdal_progress_values(text: str) -> list[int]:
    values: list[int] = []
    for match in re.finditer(r"(?<!\d)(100|[1-9]?\d)(?=(?:\.\.\.| - done|%))", text or ""):
        value = int(match.group(1))
        if 0 <= value <= 100:
            values.append(value)
    return values


def _run_command_with_gdal_progress(
    args: list[str],
    extra_env: Dict[str, Any] | None,
    emit: Callable[[Dict[str, Any]], None],
    min_zoom: int,
    max_zoom: int,
) -> subprocess.CompletedProcess:
    logger.info("执行 MVT 命令: %s", " ".join(args))
    env = os.environ.copy()
    for key in ("PROJ_LIB", "PROJ_DATA"):
        if not extra_env or key not in extra_env:
            env.pop(key, None)
    if extra_env:
        for key, value in extra_env.items():
            if key and value is not None:
                env[str(key)] = str(value)

    process = subprocess.Popen(
        args,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        env=env,
        bufsize=0,
    )
    output_parts: list[str] = []
    scan_text = ""
    last_progress = -1
    if process.stdout is not None:
        while True:
            chunk = process.stdout.read(1)
            if chunk == "" and process.poll() is not None:
                break
            if not chunk:
                continue
            output_parts.append(chunk)
            scan_text += chunk
            if len(scan_text) > 256:
                scan_text = scan_text[-256:]
            for progress in _extract_gdal_progress_values(scan_text):
                if progress <= last_progress:
                    continue
                last_progress = progress
                _emit_progress(
                    emit,
                    "generate",
                    progress,
                    100,
                    min_zoom,
                    max_zoom,
                    overall_progress=_weighted_progress(
                        MVT_GENERATE_PROGRESS_START,
                        MVT_GENERATE_PROGRESS_SPAN,
                        progress,
                    ),
                )

    returncode = process.wait()
    output = "".join(output_parts)
    if returncode != 0:
        message = f"command failed with exit code {returncode}: {' '.join(args)}"
        if output.strip():
            message = f"{message}; output: {output.strip()}"
        raise RuntimeError(message)
    return subprocess.CompletedProcess(args=args, returncode=returncode, stdout=output, stderr="")


def _emit_progress(
    emit,
    phase: str,
    processed: int,
    total: int,
    current_zoom: int,
    max_zoom: int,
    overall_progress: float | None = None,
) -> None:
    percent = float(processed) / float(total) * 100.0 if total > 0 else 0.0
    overall = percent if overall_progress is None else float(overall_progress)
    emit({
        "phase": phase,
        "event": "progress",
        "message": "生成矢量瓦片缓存",
        "current_zoom": current_zoom,
        "max_zoom": max_zoom,
        "tiles_processed": processed,
        "tiles_total_estimate": total,
        "progress_percent": percent,
        "overall_progress": overall,
    })


def _publish_pmtiles_archive(source_path: Path, target_uri: str, target_env: Dict[str, Any]) -> None:
    if _is_gdal_virtual_path(target_uri):
        with _gdal_config_env(target_env) as gdal:
            target = gdal.VSIFOpenL(target_uri, "wb")
            if target is None:
                raise RuntimeError(f"GDAL cannot open PMTiles target for writing: {target_uri}")
            try:
                with source_path.open("rb") as source:
                    while True:
                        chunk = source.read(8 * 1024 * 1024)
                        if not chunk:
                            break
                        if gdal.VSIFWriteL(chunk, 1, len(chunk), target) != len(chunk):
                            raise RuntimeError(f"GDAL wrote incomplete PMTiles payload to {target_uri}")
            finally:
                close_result = gdal.VSIFCloseL(target)
            if close_result != 0:
                raise RuntimeError(f"GDAL failed to close PMTiles target: {target_uri}")
        return

    target_path = Path(target_uri)
    target_path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path = target_path.with_name(f".{target_path.name}.{os.getpid()}.partial")
    try:
        shutil.copyfile(source_path, temporary_path)
        os.replace(temporary_path, target_path)
    finally:
        temporary_path.unlink(missing_ok=True)


def vector_to_pmtiles(
    access_plan: Dict[str, Any],
    tile: Dict[str, Any],
    options: Dict[str, Any] | None = None,
    **kwargs,
) -> Dict[str, Any]:
    """
    生成 ADDP PMTiles v3 矢量瓦片归档。

    access_plan 只包含物理 URI 和对象存储发布信息；本算子不识别 ADDP locator。
    """
    if not isinstance(access_plan, dict):
        raise ValueError("access_plan is required and must be an object")
    if not isinstance(tile, dict):
        raise ValueError("tile is required and must be an object")
    options = dict(options or {})
    source_plan = access_plan.get("source")
    target_plan = access_plan.get("target")
    if not isinstance(source_plan, dict):
        raise ValueError("access_plan.source is required and must be an object")
    if not isinstance(target_plan, dict):
        raise ValueError("access_plan.target is required and must be an object")

    source_uri = str(source_plan.get("root_uri") or "").strip()
    if not source_uri:
        raise ValueError("access_plan.source.root_uri is required")
    target_uri = str(target_plan.get("archive_uri") or "").strip()
    if not target_uri:
        raise ValueError("access_plan.target.archive_uri is required")
    target_env = target_plan.get("gdal_env") if isinstance(target_plan.get("gdal_env"), dict) else {}
    source_env = source_plan.get("gdal_env") if isinstance(source_plan.get("gdal_env"), dict) else {}
    primary_key = str(options.get("primary_key") or "").strip()
    source_layer = _source_layer_name(source_plan, source_uri)
    layer_name = str(options.get("layer_name") or source_layer or "layer").strip() or "layer"
    min_zoom = int(tile.get("min_zoom") or 0)
    max_zoom = int(tile.get("max_zoom") or min_zoom)
    if max_zoom < min_zoom:
        raise ValueError("tile.max_zoom must be greater than or equal to tile.min_zoom")
    extent = _normalize_extent(tile.get("extent"))
    extent_srid = int(tile.get("extent_srid") or 0)
    source_srs = str(tile.get("source_srs") or "").strip()
    render_extent = _extent_to_wgs84(extent, extent_srid, source_srs)
    quality_options = _mvt_quality_options(tile, options)

    progress_plan = access_plan.get("progress_callback") if isinstance(access_plan.get("progress_callback"), dict) else {}
    emit = _progress_reporter(progress_plan)
    cleanup_path_options = _set_path_specific_gdal_options([
        (source_uri, source_env),
        (target_uri, target_env),
    ])
    started_at = perf_counter()
    work_dir = Path(tempfile.mkdtemp(prefix="addp-pmtiles-"))
    tile_dir = work_dir / "tiles"
    archive_path = work_dir / "result.pmtiles"
    try:
        _emit_progress(emit, "prepare", 0, 1, min_zoom, max_zoom)
        ogr_cmd = [
            "ogr2ogr",
            "-progress",
            "-f",
            "MVT",
            str(tile_dir),
            source_uri,
            "-dsco",
            f"MINZOOM={min_zoom}",
            "-dsco",
            f"MAXZOOM={max_zoom}",
            "-dsco",
            f"NAME={layer_name}",
            "-dsco",
            "COMPRESS=YES",
            "-dsco",
            f"EXTENT={quality_options['extent']}",
            "-dsco",
            f"BUFFER={quality_options['buffer']}",
            "-dsco",
            f"MAX_SIZE={quality_options['max_size']}",
            "-dsco",
            f"MAX_FEATURES={quality_options['max_features']}",
            "-sql",
            _build_quick_view_ogr_sql(source_layer, primary_key),
            "-dialect",
            "OGRSQL",
        ]
        if quality_options["simplification"] is not None:
            ogr_cmd.extend(["-dsco", f"SIMPLIFICATION={quality_options['simplification']}"])
        if quality_options["simplification_max_zoom"] is not None:
            ogr_cmd.extend(["-dsco", f"SIMPLIFICATION_MAX_ZOOM={quality_options['simplification_max_zoom']}"])
        if source_srs:
            ogr_cmd.extend(["-s_srs", source_srs])
        ogr_cmd.extend(["-t_srs", "EPSG:3857"])
        ogr_env = dict(source_env or {})
        if quality_options["num_threads"] and "GDAL_NUM_THREADS" not in ogr_env:
            ogr_env["GDAL_NUM_THREADS"] = quality_options["num_threads"]
        _run_command_with_gdal_progress(ogr_cmd, ogr_env, emit, min_zoom, max_zoom)
        generated_tile_index = _index_generated_tiles(tile_dir)

        tile_ranges = []
        total_estimate = 0
        for z in range(min_zoom, max_zoom + 1):
            min_x, min_y, max_x, max_y = _tile_bounds(render_extent, z)
            count = (max_x - min_x + 1) * (max_y - min_y + 1)
            tile_ranges.append((z, min_x, min_y, max_x, max_y, count))
            total_estimate += count

        processed = 0
        generated = 0
        empty = 0
        total_size = 0
        max_size = 0
        min_size = 0
        zoom_levels: Dict[str, Any] = {}
        tile_entries = []
        for z, min_x, min_y, max_x, max_y, total_tiles in tile_ranges:
            zoom_generated = 0
            zoom_empty = 0
            for x in range(min_x, max_x + 1):
                for y in range(min_y, max_y + 1):
                    source_tile = generated_tile_index.get((z, x, y))
                    if source_tile is None:
                        empty += 1
                        zoom_empty += 1
                    else:
                        tile_data = source_tile.read_bytes()
                        if not tile_data.startswith(b"\x1f\x8b"):
                            raise RuntimeError(f"GDAL MVT tile is not gzip-compressed: {source_tile}")
                        generated += 1
                        zoom_generated += 1
                        size = len(tile_data)
                        tile_entries.append((zxy_to_tileid(z, x, y), z, tile_data))
                        total_size += size
                        max_size = max(max_size, size)
                        if min_size == 0 or size < min_size:
                            min_size = size
                    processed += 1
            zoom_levels[str(z)] = {
                "zoom": z,
                "total_tiles": total_tiles,
                "generated_tiles": zoom_generated,
                "empty_tiles": zoom_empty,
                "skipped_tiles": 0,
                "oversized_tiles": 0,
                "failed_tiles": 0,
                "avg_gen_time_ms": 0,
                "avg_size_kb": 0.0,
                "total_size_bytes": 0,
                "max_size_bytes": 0,
                "min_size_bytes": 0,
            }
        if not tile_entries:
            raise RuntimeError("vector source produced no MVT tiles")
        tile_entries.sort(key=lambda item: item[0])
        for _, z, tile_data in tile_entries:
            zoom_stats = zoom_levels[str(z)]
            size = len(tile_data)
            zoom_stats["total_size_bytes"] += size
            zoom_stats["max_size_bytes"] = max(int(zoom_stats["max_size_bytes"]), size)
            if int(zoom_stats["min_size_bytes"]) == 0 or size < int(zoom_stats["min_size_bytes"]):
                zoom_stats["min_size_bytes"] = size
        for zoom_stats in zoom_levels.values():
            generated_count = int(zoom_stats["generated_tiles"])
            if generated_count > 0:
                zoom_stats["avg_size_kb"] = float(zoom_stats["total_size_bytes"]) / float(generated_count) / 1024.0

        actual_max_zoom = max((int(z) for z, stats in zoom_levels.items() if stats["generated_tiles"] > 0), default=min_zoom)
        metadata = {
            "name": layer_name,
            "format": "pbf",
            "type": "overlay",
            "version": "2",
            "vector_layers": [{"id": layer_name, "fields": {}}],
            "generator": "ADDP GeoPython Workflow",
        }
        header = {
            "tile_compression": Compression.GZIP,
            "tile_type": TileType.MVT,
            "min_zoom": min_zoom,
            "max_zoom": actual_max_zoom,
            "min_lon_e7": round(render_extent[0] * 10_000_000),
            "min_lat_e7": round(render_extent[1] * 10_000_000),
            "max_lon_e7": round(render_extent[2] * 10_000_000),
            "max_lat_e7": round(render_extent[3] * 10_000_000),
            "center_zoom": min_zoom,
            "center_lon_e7": round((render_extent[0] + render_extent[2]) / 2 * 10_000_000),
            "center_lat_e7": round((render_extent[1] + render_extent[3]) / 2 * 10_000_000),
        }
        with archive_path.open("wb") as output:
            writer = Writer(output)
            for tile_id, _, tile_data in tile_entries:
                writer.write_tile(tile_id, tile_data)
            writer.finalize(header, metadata)
        archive_size = archive_path.stat().st_size
        with archive_path.open("rb") as archive_input:
            header_hash = hashlib.sha256(archive_input.read(127)).hexdigest()
        _emit_progress(emit, "publish", generated, generated, actual_max_zoom, max_zoom, overall_progress=95)
        _publish_pmtiles_archive(archive_path, target_uri, target_env)
        processed = total_estimate
        duration = perf_counter() - started_at
        result = {
            "archive_format": "pmtiles",
            "spec_version": 3,
            "tile_format": "mvt",
            "tile_compression": "gzip",
            "header_hash": header_hash,
            "archive_size_bytes": archive_size,
            "extent": render_extent,
            "extent_srid": 4326,
            "min_zoom": min_zoom,
            "max_zoom": max_zoom,
            "actual_max_zoom": actual_max_zoom,
            "tiles_total_estimate": total_estimate,
            "tiles_processed": processed,
            "total_tiles": processed,
            "cached_tiles": generated,
            "generated_tiles": generated,
            "empty_tiles": empty,
            "skipped_tiles": 0,
            "oversized_skipped_tiles": 0,
            "failed_tiles": 0,
            "total_size_bytes": archive_size,
            "max_tile_size_bytes": max_size,
            "min_tile_size_bytes": min_size,
            "zoom_levels": zoom_levels,
            "stop_reason": "workflow_ogr2ogr_pmtiles",
            "generation_seconds": duration,
            "mvt_options": {
                "extent": quality_options["extent"],
                "buffer": quality_options["buffer"],
                "max_size": quality_options["max_size"],
                "max_features": quality_options["max_features"],
                "simplification": quality_options["simplification"],
                "simplification_max_zoom": quality_options["simplification_max_zoom"],
                "num_threads": quality_options["num_threads"],
            },
        }
        _emit_progress(emit, "complete", processed, total_estimate, actual_max_zoom, max_zoom, overall_progress=100)
        return result
    finally:
        cleanup_path_options()
        shutil.rmtree(work_dir, ignore_errors=True)


VECTOR_TO_PMTILES_METADATA = OperatorMetadata(
    name="vector_to_pmtiles",
    type=OperatorType.GENERAL,
    category=OperatorCategory.FORMAT_CONVERSION,
    description="PMTiles v3 矢量瓦片集生成",
    brief_description="从空间数据生成单文件 PMTiles v3 矢量瓦片集",
    execution_modes=["workflow", "direct"],
    effects=["read", "write"],
    overview="面向 Manager vector_tile_cache_generation 和 vector_tile_set_generation 的文件、对象来源 PMTiles 生成算子。Manager 负责 locator、源访问计划、目标存储和任务状态；GeoPython Workflow 负责 MVT 生成、PMTiles v3 封装和原子发布。PostgreSQL/PostGIS 表由 Manager 原生生成器处理。",
    params=[
        OperatorParam(
            name="access_plan",
            type="param",
            data_type="object",
            required=True,
            description="Manager 解析后的向量瓦片访问计划",
            notes="包含 source.root_uri/source.gdal_env、target.archive_uri/target.gdal_env、progress_callback 等技术访问参数。原始 ADDP locator 不由 Python 解析。",
        ),
        OperatorParam(
            name="tile",
            type="param",
            data_type="object",
            required=True,
            description="瓦片生成配置",
            notes="包含 min_zoom、max_zoom、extent、extent_srid、source_srs 等。",
        ),
        OperatorParam(
            name="options",
            type="param",
            data_type="object",
            required=False,
            description="生成选项",
            notes="包含 geometry_column、layer_name 等。",
        ),
    ],
    use_cases=[
        "Shapefile、GeoPackage 或 FlatGeobuf 文件生成统一 PMTiles 快显缓存。",
        "空间 data item 生成可长期治理和跨平台交换的 Business PMTiles。",
    ],
    notes=[
        "该算子不识别 ADDP locator，只处理 Manager 传入的物理访问计划。",
        "输出是一个 PMTiles v3 文件，不生成 ADDP 私有 sidecar manifest。",
        "MVT 生成依赖 GDAL/OGR 的 MVT driver。",
        "tile.extent 可使用源 CRS；算子按 tile.extent_srid/source_srs 转为 EPSG:4326 后计算瓦片范围。",
    ],
    workflow_example={
        "id": "vector_to_pmtiles",
        "operator": "vector_to_pmtiles",
        "params": {
            "access_plan": {
                "source": {
                    "root_uri": "/mnt/data/shp/farmland.shp",
                    "gdal_env": {},
                    "metadata": {"full_name": "shp/farmland.shp"},
                },
                "target": {
                    "archive_uri": "/vsis3/manager/tenant_7/vector-tile-cache/fp.pmtiles",
                    "gdal_env": {},
                },
                "progress_callback": {
                    "endpoint": "http://manager:8081/api/v1/manager/internal/executions/{execution_id}/events",
                    "tenant_id": 7,
                    "execution_id": "execution-uuid",
                },
            },
            "tile": {
                "min_zoom": 0,
                "max_zoom": 12,
                "extent": [110.0, 20.0, 120.0, 30.0],
                "extent_srid": 4326,
                "source_srs": "EPSG:4326",
            },
            "options": {"geometry_column": "geometry", "layer_name": "geometry"},
        },
    },
)


OPERATORS = dict([
    register_operator(VECTOR_TO_PMTILES_METADATA, vector_to_pmtiles),
])
